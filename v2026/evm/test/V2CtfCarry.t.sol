// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2CtfBase} from "./utils/V2CtfBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STCtfCarry} from "../src/STCtfCarry.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {IStaking, ISTAKING_ADDRESS} from "../src/interfaces/stakingV2.sol";
import {IStakingAccountSettings} from "../src/interfaces/stakingAccountSettings.sol";
import {LedgerTree} from "../src/LedgerTree.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";
import {MockAddressMapping} from "./mocks/MockAddressMapping.sol";
import {MockStakingV2} from "./mocks/PrecompileMocks.sol";
import {MockStakingRuntime486} from "./mocks/MockStakingRuntime486.sol";

/// @dev The capture the flag carry contract (CAPTURETHEFLAG.md §4.6) against
///      the real vault2 and STSettlement2Ctf on the etched precompile mocks,
///      the runtime's 0x080c address mapping included, with runtime 486's
///      StakingHotkeys rules (MockStakingRuntime486). NO1's CTF pool is
///      registered from CTF_FIRST_EPOCH and its carry contract created with
///      the least seed. CC-1 covers creation, CC-2 and CC-3 the cycle, CC-4
///      claims made by others, CC-5 to CC-7 the refusals, CC-8 rounding, CC-9
///      donations, CC-10 the epoch a return is recorded for, CC-11 that it
///      cannot be drained or redirected, CC-12 a collateral lock on vault2's
///      position, and CC-13 the runtime's refusals of the carry's own moves.
///      The residual: CC-14 a full griefing attempt on the carry coldkey's
///      list, which now fails, CC-15 coldkey swaps into it, CC-16 root's sweep
///      of the residual and its restoration, CC-17 a residual the runtime
///      lowered, CC-18 seed, CC-19 swap destination reservations, CC-20
///      vault2's own list (the relay's retry and absorbStake), and CC-21 a
///      list past 256 entries, which the pinned pool hotkey makes harmless.
contract V2CtfCarryTest is V2CtfBase {
    bytes32 internal constant ELSEWHERE = keccak256("another hotkey");
    bytes32 internal constant GRIEFER = keccak256("griefer-coldkey");
    // The least seed: 0.1 TAO, staked 1:1 by the mock.
    uint256 internal constant SEED_RAO = 100_000_000;
    uint256 internal constant SEED_WEI = SEED_RAO * 1e9;
    // The runtime refuses a move or transfer worth less than 100,000 rao of
    // TAO (AmountTooLow); the suite scales that floor down to 100 rao of alpha.
    uint256 internal constant FLOOR = 100;

    MockStakingRuntime486 internal runtime = MockStakingRuntime486(ISTAKING_ADDRESS);
    STCtfCarry internal carry;
    bytes32 internal carryColdkey;
    bytes32 internal ctfHotkey;
    address internal griefer = makeAddr("griefer");
    uint256 internal fills;

    function setUp() public override {
        super.setUp();
        vm.etch(ISTAKING_ADDRESS, address(new MockStakingRuntime486()).code);
        vm.etch(address(0x080c), address(new MockAddressMapping()).code);
        staking.setColdkey(griefer, GRIEFER);
        staking.setMinimumMoveAmount(FLOOR);
        staking.setMinimumTransferAmount(FLOOR);
        ctfHotkey = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        carry = _createCarry(SEED_WEI);
        carryColdkey = carry.coldkey();
    }

    /// @dev This test contract creates a carry for CTF1 with `value` wei.
    function _createCarry(uint256 value) internal returns (STCtfCarry) {
        vm.deal(address(this), address(this).balance + value);
        return new STCtfCarry{value: value}(ISTSettlementVault2(address(vault2)), CTF1);
    }

    /// @dev The address and coldkey of the next contract this test creates.
    function _nextCreation() internal view returns (address next, bytes32 nextColdkey) {
        next = vm.computeCreateAddress(address(this), vm.getNonce(address(this)));
        nextColdkey = Blake2b.hash256(abi.encodePacked(bytes4("evm:"), next));
    }

    /// @dev Epoch CTF_FIRST_EPOCH's root: ALICE won 300 and the carry account
    ///      holds the 700 left, account 1.
    function _firstRoot() internal returns (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) {
        return _publishCtf(CTF_FIRST_EPOCH, ctfHotkey, _keys(ALICE, carryColdkey), _values(300, 700));
    }

    function _carryAccount(uint256 cumulative) internal view returns (LedgerTree.Account memory) {
        return _account(1, carryColdkey, cumulative);
    }

    function _noAccount() internal pure returns (LedgerTree.Account memory) {
        return LedgerTree.Account({index: 0, coldkey: bytes32(0), cumulative: 0});
    }

    function _recycle(
        uint256 rootIndex,
        LedgerTree.Account memory account,
        LedgerTree.ProofNode[] memory proof
    ) internal returns (uint256 epoch_, uint256 amount) {
        vm.prank(stranger);
        return carry.recycle(rootIndex, account, proof);
    }

    /// @dev The griefer adds `entries` new hotkeys to `coldkey`'s list, each by
    ///      a transfer at the floor from its own coldkey, and returns the last.
    function _fill(bytes32 coldkey, uint256 entries) internal returns (bytes32 hotkey) {
        for (uint256 i = 0; i < entries; i++) {
            hotkey = keccak256(abi.encode("griefer hotkey", fills++));
            staking.setStake(hotkey, GRIEFER, FLOOR);
            vm.prank(griefer);
            staking.transferStake(coldkey, hotkey, NETUID, NETUID, FLOOR);
        }
    }

    /// @dev One more griefer transfer to `coldkey` on a new hotkey, refused.
    function _fillRefused(bytes32 coldkey, string memory reason) internal {
        bytes32 hotkey = keccak256(abi.encode("griefer hotkey", fills++));
        staking.setStake(hotkey, GRIEFER, FLOOR);
        vm.prank(griefer);
        vm.expectRevert(bytes(reason));
        staking.transferStake(coldkey, hotkey, NETUID, NETUID, FLOOR);
    }

    // ------------------------------------------------------------------
    // CC-1: creation
    // ------------------------------------------------------------------

    // CC-1: the carry reads vault2's netuid, escrow hotkey and coldkey and the
    // CTF pool's hotkey at creation, and its own coldkey is its address's
    // runtime mapping, blake2b-256("evm:" || address). It stakes the TAO it is
    // created with on the escrow hotkey by its own addStake, which is its
    // residual, and sets the runtime's opt-outs of third-party list growth and
    // locked alpha. It refuses a vault2 without code, a pool vault2 does not
    // hold, a seed below the least, and a runtime that does not keep its flags.
    function test_CC1_creationReadsVault2SeedsTheResidualAndOptsOut() public {
        assertEq(address(carry.vault()), address(vault2));
        assertEq(carry.poolId(), CTF1);
        assertEq(carry.netuid(), NETUID);
        assertEq(carry.escrowHotkey(), V2_ESCROW);
        assertEq(carry.vaultColdkey(), V2_SELF);
        assertEq(carry.poolHotkey(), ctfHotkey);
        assertEq(carryColdkey, Blake2b.hash256(abi.encodePacked(bytes4("evm:"), address(carry))));
        assertTrue(carryColdkey != V2_SELF);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH), 0);

        // The residual: the seed staked on the escrow hotkey, which is on the
        // carry coldkey's list with the pool hotkey, its auto-stake
        // destination, and nothing else is; the TAO is spent.
        assertEq(carry.residual(), SEED_RAO);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO);
        assertEq(runtime.stakingHotkeyCount(carryColdkey), 2);
        assertTrue(runtime.isStakingHotkey(carryColdkey, V2_ESCROW));
        assertTrue(runtime.isStakingHotkey(carryColdkey, ctfHotkey));
        (bool pinned, bytes32 destination) = runtime.getAutoStakeDestination(carryColdkey, NETUID);
        assertTrue(pinned);
        assertEq(destination, ctfHotkey);
        assertEq(address(carry).balance, 0);
        assertTrue(runtime.getRejectStakingHotkeyGrowth(carryColdkey));
        assertTrue(runtime.getRejectLockedAlpha(carryColdkey));
        assertEq(carry.MINIMUM_SEED_RAO(), SEED_RAO);
        assertEq(carry.seedMinimum(), SEED_RAO, "the mock's nominator minimum is far below the floor");

        // Creation emits the seed.
        (address next,) = _nextCreation();
        vm.expectEmit(true, true, true, true, next);
        emit STCtfCarry.Seeded(SEED_RAO + 5, SEED_RAO + 5, SEED_RAO + 5);
        _createCarry(SEED_WEI + 5e9);

        vm.expectRevert(STCtfCarry.InvalidConfiguration.selector);
        new STCtfCarry{value: 0}(ISTSettlementVault2(makeAddr("no code")), CTF1);
        vm.deal(address(this), SEED_WEI);
        vm.expectRevert(STCtfCarry.InvalidConfiguration.selector);
        new STCtfCarry{value: SEED_WEI}(ISTSettlementVault2(address(vault2)), CTF2);
        // A wei short of the least seed's last rao.
        vm.expectRevert(STCtfCarry.SeedTooSmall.selector);
        new STCtfCarry{value: SEED_WEI - 1}(ISTSettlementVault2(address(vault2)), CTF1);
        // A runtime that refuses the flags, or does not keep them.
        runtime.setFailFlags(true);
        vm.expectRevert("mock: flags down");
        new STCtfCarry{value: SEED_WEI}(ISTSettlementVault2(address(vault2)), CTF1);
        runtime.setFailFlags(false);
        vm.mockCall(
            ISTAKING_ADDRESS,
            abi.encodeWithSelector(IStakingAccountSettings.getRejectStakingHotkeyGrowth.selector),
            abi.encode(false)
        );
        vm.expectRevert(STCtfCarry.InvalidConfiguration.selector);
        new STCtfCarry{value: SEED_WEI}(ISTSettlementVault2(address(vault2)), CTF1);
        vm.clearMockedCalls();
        vm.mockCall(
            ISTAKING_ADDRESS,
            abi.encodeWithSelector(IStakingAccountSettings.getAutoStakeDestination.selector),
            abi.encode(true, V2_ESCROW)
        );
        vm.expectRevert(STCtfCarry.InvalidConfiguration.selector);
        new STCtfCarry{value: SEED_WEI}(ISTSettlementVault2(address(vault2)), CTF1);
        vm.clearMockedCalls();
        // The pool hotkey must hold a UID to be the auto-stake destination.
        neuron.clearUid(NETUID, ctfHotkey);
        vm.expectRevert("mock: HotKeyNotRegisteredInSubNet");
        new STCtfCarry{value: SEED_WEI}(ISTSettlementVault2(address(vault2)), CTF1);
        neuron.setUid(NETUID, ctfHotkey, CTF1_UID);

        // TAO already on the carry's address is staked with the call value.
        (address preFunded, bytes32 preFundedColdkey) = _nextCreation();
        vm.deal(preFunded, SEED_WEI);
        STCtfCarry funded = new STCtfCarry{value: 0}(ISTSettlementVault2(address(vault2)), CTF1);
        assertEq(address(funded), preFunded);
        assertEq(funded.residual(), SEED_RAO);
        assertEq(staking.stakes(V2_ESCROW, preFundedColdkey), SEED_RAO);

        uint256 runtimeBytes = address(carry).code.length;
        uint256 creation = type(STCtfCarry).creationCode.length;
        assertLt(runtimeBytes, 24_576);
        assertLt(creation, 49_152);
        emit log_named_uint("STCtfCarry runtime bytes", runtimeBytes);
        emit log_named_uint("STCtfCarry creation bytes", creation);
    }

    // ------------------------------------------------------------------
    // CC-2 and CC-3: the cycle
    // ------------------------------------------------------------------

    // CC-2, §4.6: anyone recycles. The carry claims its account, which vault2
    // pays on its escrow hotkey, moves that onto the CTF pool hotkey and
    // transfers it to vault2's position there, recorded for the pool's next
    // epoch. It keeps only its residual after, so the escrow hotkey stays on
    // its list, and the next close captures the returned stake with that
    // epoch's emission. Conservation holds throughout.
    function test_CC2_recycleReturnsTheCarryToThePoolsNextCapture() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 0, "the close moved everything");

        vm.expectEmit(true, true, true, true, address(vault2));
        emit ISTSettlementVault2.Claimed(CTF1, rootIndex, 1, carryColdkey, 700, 700, address(carry));
        vm.expectEmit(true, true, true, true, address(carry));
        emit STCtfCarry.Recycled(CTF_FIRST_EPOCH + 1, 700);
        (uint256 epoch_, uint256 amount) =
            _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        uint256 gas = vm.lastCallGas().gasTotalUsed;

        assertEq(epoch_, CTF_FIRST_EPOCH + 1);
        assertEq(amount, 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 700);
        assertEq(vault2.claimed(CTF1, 1), 700);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO, "only the residual stays");
        assertEq(staking.stakes(ctfHotkey, carryColdkey), 0);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 700);
        assertTrue(runtime.isStakingHotkey(carryColdkey, V2_ESCROW));
        assertTrue(runtime.isStakingHotkey(carryColdkey, ctfHotkey), "the pool hotkey, its destination, stays listed");
        assertEq(carry.residual(), SEED_RAO);
        assertTrue(vault2.conservationHolds());

        _accrueV2(ctfHotkey, 50);
        assertEq(_closeV2(CTF_FIRST_EPOCH + 1, CTF1), 750);
        assertEq(vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 1), 1_750);
        assertTrue(vault2.conservationHolds());
        emit log_named_uint("recycle with a claim gas", gas);
    }

    // CC-3, §4.5: the carry account is cumulative. The next root keeps it and
    // adds the pots left after its epoch; the next recycle claims only the
    // increment, recorded for the epoch after.
    function test_CC3_eachRootsCarryIncrementCyclesAgain() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        _accrueV2(ctfHotkey, 50);
        // Epoch k + 1 funds 750 more: BOB wins 500 and 250 carries.
        levels = V2TreeHelper.build(_keys(ALICE, carryColdkey, BOB), _values(300, 950, 500));
        _closeV2(CTF_FIRST_EPOCH + 1, CTF1);
        _commitV2(CTF_FIRST_EPOCH + 1, CTF1, rootSigner1, levels, 3);
        _approveV2(CTF1, auditor1);
        rootIndex = _finalizeV2(CTF1);
        assertEq(rootIndex, 1);

        (uint256 epoch_, uint256 amount) =
            _recycle(rootIndex, _carryAccount(950), V2TreeHelper.proof(levels, 1));
        assertEq(epoch_, CTF_FIRST_EPOCH + 2);
        assertEq(amount, 250);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 2), 250);
        assertEq(vault2.claimed(CTF1, 1), 950);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 250);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO);
        assertTrue(vault2.conservationHolds());
    }

    // ------------------------------------------------------------------
    // CC-4: claims made by others
    // ------------------------------------------------------------------

    // CC-4: vault2's claims are anyone's, and always pay the leaf's coldkey.
    // A carry account someone else claimed is not claimed again: recycle with
    // the account, or with a zero coldkey, moves what the carry holds above
    // its residual. With nothing above it there is nothing to recycle.
    function test_CC4_recycleMovesWhatOthersClaimedForIt() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.expectRevert(STCtfCarry.NothingToRecycle.selector);
        _recycle(rootIndex, _noAccount(), proof);

        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);
        assertEq(
            staking.stakes(V2_ESCROW, carryColdkey),
            SEED_RAO + 700,
            "held between calls only after another's claim"
        );
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), proof);
        assertEq(amount, 700);
        assertEq(vault2.claimed(CTF1, 1), 700);
        vm.expectRevert(STCtfCarry.NothingToRecycle.selector);
        _recycle(rootIndex, _carryAccount(700), proof);

        // A zero coldkey names no account: its other fields are ignored.
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + 180);
        (, amount) =
            _recycle(rootIndex, LedgerTree.Account({index: 9, coldkey: bytes32(0), cumulative: 1}), proof);
        assertEq(amount, 180);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 880);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO);
    }

    // ------------------------------------------------------------------
    // CC-5 to CC-7: refusals
    // ------------------------------------------------------------------

    // CC-5: only the carry's own account can be named, and vault2's refusals
    // of its claim, a wrong proof or an unknown root, propagate.
    function test_CC5_recycleRefusesAnotherAccountAndABadClaim() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        vm.expectRevert(STCtfCarry.InvalidColdkey.selector);
        _recycle(rootIndex, _account(0, ALICE, 300), V2TreeHelper.proof(levels, 0));
        vm.expectRevert(ISTSettlementVault2.InvalidProof.selector);
        _recycle(rootIndex, _carryAccount(701), V2TreeHelper.proof(levels, 1));
        vm.expectRevert(ISTSettlementVault2.UnknownRoot.selector);
        _recycle(rootIndex + 1, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(vault2.claimed(CTF1, 0), 0);
        assertEq(vault2.claimed(CTF1, 1), 0);
    }

    // CC-6: a runtime refusal of either move reverts the whole recycle, the
    // claim included, which stays claimable; so does an amount of 2^64 - 1 or
    // more above the residual, which the precompile would read as the whole
    // position.
    function test_CC6_aRuntimeRefusalRevertsTheWholeRecycle() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        staking.setFailMoveStake(true);
        vm.expectRevert("mock: moveStake down");
        _recycle(rootIndex, _carryAccount(700), proof);
        staking.setFailMoveStake(false);
        // vault2's claim is a transfer too: with transfers refused it fails first.
        staking.setFailTransferStake(true);
        vm.expectRevert("mock: transferStake down");
        _recycle(rootIndex, _carryAccount(700), proof);
        staking.setFailTransferStake(false);
        assertEq(vault2.claimed(CTF1, 1), 0);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO);

        // Claimed by another, then the carry's own transfer is refused.
        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);
        staking.setFailTransferStake(true);
        vm.expectRevert("mock: transferStake down");
        _recycle(rootIndex, _noAccount(), proof);
        staking.setFailTransferStake(false);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO + 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 0);

        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + uint256(type(uint64).max));
        vm.expectRevert(STCtfCarry.AmountTooLarge.selector);
        _recycle(rootIndex, _noAccount(), proof);
    }

    // CC-7: as vault2 requires of its own moves, each move must debit its
    // source exactly and credit its destination the amount or one rao less;
    // anything else reverts. The guard refuses a re-entered recycle.
    function test_CC7_recycleChecksEveryMoveAndRefusesReentry() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);

        staking.setMoveStakeSourceResidue(1);
        vm.expectRevert(STCtfCarry.RuntimeAccountingMismatch.selector);
        _recycle(rootIndex, _noAccount(), proof);
        staking.setMoveStakeSourceResidue(0);
        staking.setMoveStakeShortfall(2);
        vm.expectRevert(STCtfCarry.RuntimeAccountingMismatch.selector);
        _recycle(rootIndex, _noAccount(), proof);
        staking.setMoveStakeShortfall(0);
        staking.setTransferStakeShortfall(2);
        vm.expectRevert(STCtfCarry.RuntimeAccountingMismatch.selector);
        _recycle(rootIndex, _noAccount(), proof);
        staking.setTransferStakeShortfall(0);
        staking.setTransferStakeSourceResidue(1);
        vm.expectRevert(STCtfCarry.RuntimeAccountingMismatch.selector);
        _recycle(rootIndex, _noAccount(), proof);
        staking.setTransferStakeSourceResidue(0);
        // The runtime credits the carry more during its move.
        staking.setReentry(
            address(staking),
            abi.encodeCall(
                MockStakingV2.setStake, (ctfHotkey, carryColdkey, staking.stakes(ctfHotkey, carryColdkey) + 5)
            )
        );
        vm.expectRevert(STCtfCarry.RuntimeAccountingMismatch.selector);
        _recycle(rootIndex, _noAccount(), proof);
        staking.clearReentry();
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO + 700);

        staking.setReentry(
            address(carry), abi.encodeCall(STCtfCarry.recycle, (rootIndex, _noAccount(), proof))
        );
        (, uint256 amount) = _recycle(rootIndex, _noAccount(), proof);
        assertFalse(staking.reentrySucceeded());
        assertEq(staking.reentryFailureSelector(), STCtfCarry.Reentrancy.selector);
        assertEq(amount, 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 700);

        // seed shares the guard: re-entered from a recycle's move, it is refused.
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + 105);
        staking.setReentry(address(carry), abi.encodeCall(STCtfCarry.seed, ()));
        (, amount) = _recycle(rootIndex, _noAccount(), proof);
        assertFalse(staking.reentrySucceeded());
        assertEq(staking.reentryFailureSelector(), STCtfCarry.Reentrancy.selector);
        assertEq(amount, 105);
        staking.clearReentry();
    }

    // ------------------------------------------------------------------
    // CC-8 to CC-10: amounts and epochs
    // ------------------------------------------------------------------

    // CC-8, §4.6: each stake move may land one rao short. The claim, the move,
    // the transfer and the next capture lose at most four rao a cycle, and the
    // carry records exactly what vault2's position gained. The residual keeps
    // none of it.
    function test_CC8_roundingLosesAtMostFourRaoACycle() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        staking.setMoveStakeShortfall(1);
        staking.setTransferStakeShortfall(1);
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 697, "the claim, the move and the transfer each landed one rao short");
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 697);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 697);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO);
        assertEq(staking.stakes(ctfHotkey, carryColdkey), 0);
        assertEq(_closeV2(CTF_FIRST_EPOCH + 1, CTF1), 696);
        assertEq(
            vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 1) - vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH),
            700 - 4
        );
        assertTrue(vault2.conservationHolds());
    }

    // CC-9, §4.6: stake donated to the carry, on the escrow hotkey or on the
    // pool hotkey, goes round with the carry and is recorded with it; the
    // ledger counts what exceeds the carry owed as fresh funding. A donation
    // on the pool hotkey alone waits for a recycle with stake above the
    // residual on the escrow hotkey. Stake on any other hotkey stays where it
    // is. (setStake is a protocol credit here, which no list rule limits.)
    function test_CC9_donationsGoRoundWithTheCarry() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        staking.setStake(ctfHotkey, carryColdkey, 40);
        staking.setStake(ELSEWHERE, carryColdkey, 9);
        vm.expectRevert(STCtfCarry.NothingToRecycle.selector);
        _recycle(rootIndex, _noAccount(), V2TreeHelper.proof(levels, 1));
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + 3);
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 743);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 743);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 743);
        assertEq(staking.stakes(ctfHotkey, carryColdkey), 0);
        assertEq(staking.stakes(ELSEWHERE, carryColdkey), 9);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO);
    }

    // CC-10, §4.6: a return is recorded for the pool's next unfunded epoch,
    // the epoch whose close captures it: recycles before a close and after it
    // land in consecutive epochs, and two in one epoch add up.
    function test_CC10_aReturnIsRecordedForTheEpochThatCapturesIt() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + 300);
        (uint256 epoch_,) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(epoch_, CTF_FIRST_EPOCH + 1);
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + 400);
        (epoch_,) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(epoch_, CTF_FIRST_EPOCH + 1);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 700);

        assertEq(_closeV2(CTF_FIRST_EPOCH + 1, CTF1), 700);
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + 125);
        (epoch_,) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(epoch_, CTF_FIRST_EPOCH + 2);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 2), 125);
        // A missed close defers the epoch; the next close captures the return.
        _closeV2(CTF_FIRST_EPOCH + 3, CTF1);
        assertEq(
            vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 2), vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 1)
        );
        assertEq(
            vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 3),
            vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 1) + 125
        );
    }

    // CC-12, §7.3: with registration collateral locked on vault2's CTF hotkey
    // position, the carry records the whole position's growth, which is
    // exactly what the next close can move: the lock stays.
    function test_CC12_aCollateralLockOnVault2sPositionStays() public {
        staking.setStake(ctfHotkey, V2_SELF, 500);
        staking.setCollateral(ctfHotkey, V2_SELF, 500);
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 500, "the close left the lock");
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 700);
        _accrueV2(ctfHotkey, 40);
        assertEq(_closeV2(CTF_FIRST_EPOCH + 1, CTF1), 740);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 500);
        assertTrue(vault2.conservationHolds());
    }

    // CC-13, §7.3: the runtime's refusals of the carry's own moves: below the
    // transfer floor, and a refusal that consumes all the gas forwarded and
    // returns no data, as the runtime refuses a precompile call. Each reverts
    // the whole recycle, and the carry keeps what it held for a later one.
    function test_CC13_theRuntimesRefusalsOfTheCarrysMoves() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);

        staking.setMinimumMoveAmount(701);
        vm.expectRevert("mock: move amount too low");
        _recycle(rootIndex, _noAccount(), proof);
        staking.setMinimumMoveAmount(FLOOR);
        staking.setMinimumTransferAmount(701);
        vm.expectRevert("mock: transfer amount too low");
        _recycle(rootIndex, _noAccount(), proof);
        staking.setMinimumTransferAmount(FLOOR);
        staking.setExhaustTransferTo(V2_SELF, true);
        vm.expectRevert();
        _recycle(rootIndex, _noAccount(), proof);
        staking.setExhaustTransferTo(V2_SELF, false);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO + 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 0);

        (, uint256 amount) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(amount, 700);
    }

    // ------------------------------------------------------------------
    // CC-11: nothing else goes out
    // ------------------------------------------------------------------

    // CC-11: the carry takes native value only through seed, which stakes it
    // on the escrow hotkey as its own, and has no other entry point; whoever
    // calls recycle or seed, the stake goes only to vault2's position on the
    // CTF pool hotkey or into the residual, and the caller gains nothing.
    function test_CC11_theCarryCannotBeDrainedOrRedirected() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        vm.deal(stranger, 1 ether + SEED_WEI);
        vm.prank(stranger);
        (bool sent,) = address(carry).call{value: 1}("");
        assertFalse(sent, "no receive");
        vm.prank(stranger);
        (sent,) = address(carry).call(abi.encodeWithSignature("withdraw(bytes32,uint256)", ALICE, 700));
        assertFalse(sent, "no fallback");

        bytes32 strangerColdkey = keccak256("stranger-coldkey");
        staking.setColdkey(stranger, strangerColdkey);
        uint256 vaultBefore = staking.stakes(ctfHotkey, V2_SELF);
        _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        vm.prank(stranger);
        carry.seed{value: SEED_WEI}();
        assertEq(staking.stakes(ctfHotkey, V2_SELF) - vaultBefore, 700);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 2 * SEED_RAO);
        assertEq(staking.stakes(V2_ESCROW, strangerColdkey), 0);
        assertEq(staking.stakes(ctfHotkey, strangerColdkey), 0);
        assertEq(stranger.balance, 1 ether);
        assertEq(address(carry).balance, 0);
    }

    // ------------------------------------------------------------------
    // CC-14 and CC-15: the staking-hotkey list
    // ------------------------------------------------------------------

    // CC-14, §4.6: a full griefing attempt on the carry coldkey's list, which
    // without the residual blocks its claims for good, now fails.
    //  1. The attack works against a coldkey without a residual: third parties
    //     fill BOB's list to 128, and vault2's claim of BOB's account is
    //     refused (TooManyStakingHotkeys) for as long as they hold it.
    //  2. The carry's address is known before it exists, so a griefer fills
    //     its coldkey's list to 128 first. Its creation still seeds the
    //     residual, by its own addStake, which the 256-entry cap limits.
    //  3. After creation no third party can add an entry, on any hotkey but the
    //     escrow hotkey, where a transfer is a donation the carry returns.
    //  4. Root after root, vault2's claim of the carry account pays, and the
    //     escrow hotkey stays on the list.
    function test_CC14_aGriefingAttemptOnTheCarrysListNowFails() public {
        // 1. Without a residual: BOB holds the winner's account.
        _fill(BOB, 128);
        _fillRefused(BOB, "mock: TooManyStakingHotkeys");
        (address filledCarry, bytes32 filledColdkey) = _nextCreation();
        _fill(filledColdkey, 128);
        _fillRefused(filledColdkey, "mock: TooManyStakingHotkeys");
        assertEq(runtime.stakingHotkeyCount(filledColdkey), 128);
        assertFalse(runtime.isStakingHotkey(filledColdkey, V2_ESCROW));

        // 2. The carry is created over the full list.
        STCtfCarry filled = _createCarry(SEED_WEI);
        assertEq(address(filled), filledCarry);
        assertEq(filled.coldkey(), filledColdkey);
        assertEq(runtime.stakingHotkeyCount(filledColdkey), 130);
        assertTrue(runtime.isStakingHotkey(filledColdkey, V2_ESCROW));
        assertTrue(runtime.isStakingHotkey(filledColdkey, ctfHotkey));
        assertEq(filled.residual(), SEED_RAO);
        assertTrue(runtime.getRejectStakingHotkeyGrowth(filledColdkey));

        // 3. On the carry with a short list too, no new entry; a donation on
        // the escrow hotkey is accepted.
        _fillRefused(filledColdkey, "mock: AccountRejectsStakingHotkeyGrowth");
        _fillRefused(carryColdkey, "mock: AccountRejectsStakingHotkeyGrowth");
        assertEq(runtime.stakingHotkeyCount(carryColdkey), 2);
        staking.setStake(V2_ESCROW, GRIEFER, FLOOR);
        vm.prank(griefer);
        staking.transferStake(filledColdkey, V2_ESCROW, NETUID, NETUID, FLOOR);

        // The pool's first root pays BOB and the filled carry's account.
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) =
            _publishCtf(CTF_FIRST_EPOCH, ctfHotkey, _keys(BOB, filledColdkey), _values(300, 700));
        (uint256 bobIndex, uint256 carryIndex) = (0, 1);
        vm.prank(stranger);
        vm.expectRevert("mock: TooManyStakingHotkeys");
        vault2.claim(CTF1, rootIndex, _account(bobIndex, BOB, 300), V2TreeHelper.proof(levels, bobIndex));

        // 4. The filled carry's claim pays, with the donation, and again at the
        // next root.
        vm.prank(stranger);
        (uint256 epoch_, uint256 amount) = filled.recycle(
            rootIndex, _account(carryIndex, filledColdkey, 700), V2TreeHelper.proof(levels, carryIndex)
        );
        assertEq(epoch_, CTF_FIRST_EPOCH + 1);
        assertEq(amount, 700 + FLOOR);
        assertEq(vault2.claimed(CTF1, carryIndex), 700);
        assertEq(staking.stakes(V2_ESCROW, filledColdkey), SEED_RAO);
        assertTrue(runtime.isStakingHotkey(filledColdkey, V2_ESCROW));
        assertEq(runtime.stakingHotkeyCount(filledColdkey), 130);

        _accrueV2(ctfHotkey, 50);
        levels = V2TreeHelper.build(_keys(BOB, filledColdkey), _values(300, 1_550));
        _closeV2(CTF_FIRST_EPOCH + 1, CTF1);
        _commitV2(CTF_FIRST_EPOCH + 1, CTF1, rootSigner1, levels, 2);
        _approveV2(CTF1, auditor1);
        rootIndex = _finalizeV2(CTF1);
        vm.prank(stranger);
        (, amount) = filled.recycle(
            rootIndex, _account(carryIndex, filledColdkey, 1_550), V2TreeHelper.proof(levels, carryIndex)
        );
        assertEq(amount, 850);
        assertEq(vault2.claimed(CTF1, carryIndex), 1_550);
        assertTrue(runtime.isStakingHotkey(filledColdkey, V2_ESCROW));
        assertTrue(vault2.conservationHolds());
    }

    // CC-15, §4.6: a coldkey swap into the carry's coldkey. On the runtime the
    // guardian makes that coldkey a hotkey account, which refuses every such
    // swap (NewColdKeyIsHotkey, R-9); the mock leaves it out, so this checks
    // the opt-out alone. A source with any hotkey not on the carry's list is
    // refused, so no merge can lengthen it; one with only the escrow hotkey
    // merges, a donation the next recycle returns. Without the opt-out a merge
    // could fill a list to 256 entries, after which the coldkey's own move to a
    // new hotkey, as the carry's move onto the pool hotkey is, would be refused.
    function test_CC15_aColdkeySwapCannotLengthenTheCarrysList() public {
        bytes32 source = keccak256("swap source");
        staking.setStake(ELSEWHERE, source, 5);
        staking.setStake(V2_ESCROW, source, 7);
        assertFalse(runtime.coldkeySwapAdmitted(source, carryColdkey));
        vm.expectRevert("mock: AccountRejectsStakingHotkeyGrowth");
        runtime.swapColdkeyInto(source, carryColdkey);

        staking.setStake(ELSEWHERE, source, 0);
        assertTrue(runtime.coldkeySwapAdmitted(source, carryColdkey));
        runtime.swapColdkeyInto(source, carryColdkey);
        assertEq(runtime.stakingHotkeyCount(carryColdkey), 2);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO + 7);
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 707);

        // Without the opt-out: CAROL keeps a residual on the escrow hotkey, and
        // a merge of 255 new hotkeys leaves her list at 256.
        address carolAccount = makeAddr("carol");
        staking.setColdkey(carolAccount, CAROL);
        staking.setStake(V2_ESCROW, CAROL, 2 * FLOOR);
        bytes32 wide = keccak256("wide source");
        for (uint256 i = 0; i < 255; i++) {
            staking.setStake(keccak256(abi.encode("wide", i)), wide, 1);
        }
        runtime.swapColdkeyInto(wide, CAROL);
        assertEq(runtime.stakingHotkeyCount(CAROL), 256);
        vm.prank(carolAccount);
        vm.expectRevert("mock: TooManyStakingHotkeys");
        staking.moveStake(V2_ESCROW, ctfHotkey, NETUID, NETUID, FLOOR);
    }

    // ------------------------------------------------------------------
    // CC-16 to CC-18: the residual
    // ------------------------------------------------------------------

    // CC-16, §4.6: root raises the nominator minimum above the residual, and
    // its sweep unstakes the residual to the carry's TAO: the escrow hotkey
    // leaves the list, and vault2's claim to the carry is refused, which the
    // carry reports as ResidualMissing. The account stays claimable. Anyone
    // then seeds again, with at least the new least seed, and the claim pays.
    function test_CC16_aSweptResidualIsRestoredBySeed() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        runtime.sweepSmallNomination(V2_ESCROW, carryColdkey, SEED_RAO + 1);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 0);
        assertFalse(runtime.isStakingHotkey(carryColdkey, V2_ESCROW));
        assertEq(address(carry).balance, SEED_WEI, "the sweep returned the TAO");

        vm.expectRevert(STCtfCarry.ResidualMissing.selector);
        _recycle(rootIndex, _carryAccount(700), proof);
        vm.prank(stranger);
        vm.expectRevert("mock: AccountRejectsStakingHotkeyGrowth");
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);
        assertEq(vault2.claimed(CTF1, 1), 0);
        assertEq(carry.residual(), SEED_RAO, "a refused recycle lowers nothing");

        // The new least seed: ten times the raised nominator minimum, more
        // than the swept TAO.
        staking.setNominatorMinimum(SEED_RAO / 5);
        assertEq(carry.seedMinimum(), 2 * SEED_RAO);
        vm.prank(stranger);
        vm.expectRevert(STCtfCarry.SeedTooSmall.selector);
        carry.seed();

        vm.deal(stranger, SEED_WEI);
        vm.expectEmit(true, true, true, true, address(carry));
        emit STCtfCarry.ResidualLowered(SEED_RAO, 0);
        vm.expectEmit(true, true, true, true, address(carry));
        emit STCtfCarry.Seeded(2 * SEED_RAO, 2 * SEED_RAO, 2 * SEED_RAO);
        vm.prank(stranger);
        assertEq(carry.seed{value: SEED_WEI}(), 2 * SEED_RAO);
        assertEq(carry.residual(), 2 * SEED_RAO);
        assertTrue(runtime.isStakingHotkey(carryColdkey, V2_ESCROW));

        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), proof);
        assertEq(amount, 700);
        assertEq(vault2.claimed(CTF1, 1), 700);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 2 * SEED_RAO);
    }

    // CC-17, §4.6: a residual the runtime lowered, as rounding could, is
    // followed down and never refilled from the pool's stake: the recycle
    // returns all of the claim. Stake above the lowered residual is returned
    // too, once it reaches the transfer floor, so the residual never rises
    // again but by a seed.
    function test_CC17_aLoweredResidualIsNeverRefilledFromThePool() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO - 1);
        vm.expectEmit(true, true, true, true, address(carry));
        emit STCtfCarry.ResidualLowered(SEED_RAO, SEED_RAO - 1);
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 700);
        assertEq(carry.residual(), SEED_RAO - 1);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO - 1);

        // A fragment above it waits until it reaches the transfer floor.
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO);
        vm.expectRevert("mock: move amount too low");
        _recycle(rootIndex, _noAccount(), V2TreeHelper.proof(levels, 1));
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO - 1 + FLOOR);
        (, amount) = _recycle(rootIndex, _noAccount(), V2TreeHelper.proof(levels, 1));
        assertEq(amount, FLOOR);
        assertEq(carry.residual(), SEED_RAO - 1);
    }

    // CC-18: anyone seeds, with value or TAO already on the carry's address,
    // at least seedMinimum(): ten times the runtime's nominator minimum, or
    // the 0.1 TAO floor, which a runtime that does not answer leaves. The
    // residual grows by what the position gained and is never recycled.
    function test_CC18_seedAddsToTheResidual() public {
        vm.deal(stranger, 3 * SEED_WEI);
        vm.prank(stranger);
        vm.expectRevert(STCtfCarry.SeedTooSmall.selector);
        carry.seed{value: SEED_WEI - 1e9}();
        vm.prank(stranger);
        assertEq(carry.seed{value: SEED_WEI}(), SEED_RAO);
        assertEq(carry.residual(), 2 * SEED_RAO);
        assertEq(runtime.stakingHotkeyCount(carryColdkey), 2);

        staking.setNominatorMinimum(SEED_RAO);
        assertEq(carry.seedMinimum(), 10 * SEED_RAO);
        staking.setNominatorMinimum(SEED_RAO / 10 - 1);
        assertEq(carry.seedMinimum(), SEED_RAO);
        staking.setNominatorMinimum(SEED_RAO);
        vm.mockCallRevert(
            ISTAKING_ADDRESS, abi.encodeWithSelector(IStaking.getNominatorMinRequiredStake.selector), ""
        );
        assertEq(carry.seedMinimum(), SEED_RAO);
        vm.clearMockedCalls();
        staking.setNominatorMinimum(1_000);

        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 700);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 2 * SEED_RAO);
        vm.expectRevert(STCtfCarry.NothingToRecycle.selector);
        _recycle(rootIndex, _noAccount(), V2TreeHelper.proof(levels, 1));
    }

    // ------------------------------------------------------------------
    // CC-19 and CC-20: refusals the contract cannot clear
    // ------------------------------------------------------------------

    // CC-19, §4.6: a coldkey swap announced with the carry's coldkey, or
    // vault2's, as its destination reserves it. The guardian makes both hotkey
    // accounts, which the runtime refuses to reserve (NewColdKeyIsHotkey,
    // R-9); this is what a reservation would do without that. While it lasts,
    // the runtime refuses every transfer to the coldkey from another one:
    // vault2's claim to the carry, or the carry's return to vault2. No contract
    // can release it. The recycle reverts, the account stays claimable, and
    // the next recycle after the reservation pays.
    function test_CC19_aSwapReservationDelaysTheCarry() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        runtime.setReserved(carryColdkey, true);
        vm.expectRevert("mock: ColdkeySwapDestinationReserved");
        _recycle(rootIndex, _carryAccount(700), proof);
        runtime.setReserved(carryColdkey, false);
        runtime.setReserved(V2_SELF, true);
        vm.expectRevert("mock: ColdkeySwapDestinationReserved");
        _recycle(rootIndex, _carryAccount(700), proof);
        assertEq(vault2.claimed(CTF1, 1), 0);
        runtime.setReserved(V2_SELF, false);
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), proof);
        assertEq(amount, 700);
    }

    // CC-21, §4.6: without the guardian's hotkey account on the carry's
    // coldkey, which refuses every coldkey swap into it, a swap could give the
    // carry hotkeys, since the opt-out does not count owned hotkeys
    // (swap/swap_coldkey.rs:365-390, 1143-1162), and the runtime credits their
    // emission to the carry's list with no cap. This checks the pin alone.
    // Past 256 entries the carry's claim still lands on its residual's escrow
    // hotkey and its move on the pool hotkey, its auto-stake destination, both
    // on the list. A coldkey without the pin, CAROL, whose list reaches 256
    // the same way, is refused her own move onto a new hotkey.
    function test_CC21_aListPast256EntriesLeavesTheCarryWorking() public {
        for (uint256 i = 0; i < 256; i++) {
            bytes32 gifted = keccak256(abi.encode("gifted hotkey", i));
            runtime.setHotkeyOwner(gifted, carryColdkey);
            staking.setStake(gifted, carryColdkey, 1);
        }
        assertEq(runtime.stakingHotkeyCount(carryColdkey), 258);
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        (uint256 epoch_, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(epoch_, CTF_FIRST_EPOCH + 1);
        assertEq(amount, 700);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), SEED_RAO);
        assertTrue(runtime.isStakingHotkey(carryColdkey, ctfHotkey));
        assertTrue(vault2.conservationHolds());

        address carolAccount = makeAddr("carol");
        staking.setColdkey(carolAccount, CAROL);
        staking.setStake(V2_ESCROW, CAROL, 2 * FLOOR);
        for (uint256 i = 0; i < 255; i++) {
            staking.setStake(keccak256(abi.encode("carol's gift", i)), CAROL, 1);
        }
        assertEq(runtime.stakingHotkeyCount(CAROL), 256);
        vm.prank(carolAccount);
        vm.expectRevert("mock: TooManyStakingHotkeys");
        staking.moveStake(V2_ESCROW, ctfHotkey, NETUID, NETUID, FLOOR);
    }

    // CC-20, §4.6: the carry's return re-adds the CTF pool hotkey to vault2's
    // list when vault2 holds nothing there, as after a capture. With third
    // parties holding vault2's list at 128 the recycle reverts
    // TooManyStakingHotkeys. The relay frees an entry with vault2's
    // permissionless absorbStake, which takes the third party's stake as
    // surplus, and the recycle pays; an emission credit to the pool hotkey
    // re-adds the entry too.
    function test_CC20_vault2sFullListStallsTheReturnUntilAnEntryIsFreed() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        assertFalse(runtime.isStakingHotkey(V2_SELF, ctfHotkey));
        bytes32 foreign = _fill(V2_SELF, 128 - runtime.stakingHotkeyCount(V2_SELF));
        assertEq(runtime.stakingHotkeyCount(V2_SELF), 128);

        vm.expectRevert("mock: TooManyStakingHotkeys");
        _recycle(rootIndex, _carryAccount(700), proof);
        assertEq(vault2.claimed(CTF1, 1), 0);

        vm.prank(stranger);
        assertEq(vault2.absorbStake(foreign), FLOOR);
        assertEq(runtime.stakingHotkeyCount(V2_SELF), 127);
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), proof);
        assertEq(amount, 700);
        assertEq(runtime.stakingHotkeyCount(V2_SELF), 128);

        // After the next close vault2's list is full without the pool hotkey
        // again; an emission credit to the pool hotkey re-adds it.
        _closeV2(CTF_FIRST_EPOCH + 1, CTF1);
        _fill(V2_SELF, 128 - runtime.stakingHotkeyCount(V2_SELF));
        staking.setStake(V2_ESCROW, carryColdkey, SEED_RAO + 130);
        vm.expectRevert("mock: TooManyStakingHotkeys");
        _recycle(rootIndex, _noAccount(), proof);
        _accrueV2(ctfHotkey, 1);
        assertTrue(runtime.isStakingHotkey(V2_SELF, ctfHotkey));
        (, amount) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(amount, 130);
        assertTrue(vault2.conservationHolds());
    }
}
