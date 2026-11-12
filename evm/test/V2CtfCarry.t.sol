// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2CtfBase} from "./utils/V2CtfBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STCtfCarry} from "../src/STCtfCarry.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";
import {MockAddressMapping} from "./mocks/MockAddressMapping.sol";
import {MockStakingV2} from "./mocks/PrecompileMocks.sol";

/// @dev The capture the flag carry contract (CAPTURETHEFLAG.md §4.6) against
///      the real vault2 and STSettlement2Ctf on the etched precompile mocks,
///      the runtime's 0x080c address mapping included. NO1's CTF pool is
///      registered from CTF_FIRST_EPOCH and its carry contract created. CC-1
///      covers creation, CC-2 and CC-3 the cycle, CC-4 claims made by others,
///      CC-5 to CC-7 the refusals, CC-8 rounding, CC-9 donations, CC-10 the
///      epoch a return is recorded for, CC-11 that it cannot be drained or
///      redirected, CC-12 a collateral lock on vault2's position, and CC-13
///      the runtime's refusals of the carry's own moves.
contract V2CtfCarryTest is V2CtfBase {
    bytes32 internal constant ELSEWHERE = keccak256("another hotkey");

    STCtfCarry internal carry;
    bytes32 internal carryColdkey;
    bytes32 internal ctfHotkey;

    function setUp() public override {
        super.setUp();
        vm.etch(address(0x080c), address(new MockAddressMapping()).code);
        ctfHotkey = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        carry = new STCtfCarry(ISTSettlementVault2(address(vault2)), CTF1);
        carryColdkey = carry.coldkey();
        staking.setColdkey(address(carry), carryColdkey);
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

    // ------------------------------------------------------------------
    // CC-1: creation
    // ------------------------------------------------------------------

    // CC-1: the carry reads vault2's netuid, escrow hotkey and coldkey and the
    // CTF pool's hotkey at creation, and its own coldkey is its address's
    // runtime mapping, blake2b-256("evm:" || address). It refuses a vault2
    // without code and a pool vault2 does not hold.
    function test_CC1_creationReadsVault2AndMapsItsOwnColdkey() public {
        assertEq(address(carry.vault()), address(vault2));
        assertEq(carry.poolId(), CTF1);
        assertEq(carry.netuid(), NETUID);
        assertEq(carry.escrowHotkey(), V2_ESCROW);
        assertEq(carry.vaultColdkey(), V2_SELF);
        assertEq(carry.poolHotkey(), ctfHotkey);
        assertEq(carryColdkey, Blake2b.hash256(abi.encodePacked(bytes4("evm:"), address(carry))));
        assertTrue(carryColdkey != V2_SELF);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH), 0);

        vm.expectRevert(STCtfCarry.InvalidConfiguration.selector);
        new STCtfCarry(ISTSettlementVault2(makeAddr("no code")), CTF1);
        vm.expectRevert(STCtfCarry.InvalidConfiguration.selector);
        new STCtfCarry(ISTSettlementVault2(address(vault2)), CTF2);

        uint256 runtime = address(carry).code.length;
        uint256 creation = type(STCtfCarry).creationCode.length;
        assertLt(runtime, 24_576);
        assertLt(creation, 49_152);
        emit log_named_uint("STCtfCarry runtime bytes", runtime);
        emit log_named_uint("STCtfCarry creation bytes", creation);
    }

    // ------------------------------------------------------------------
    // CC-2 and CC-3: the cycle
    // ------------------------------------------------------------------

    // CC-2, §4.6: anyone recycles. The carry claims its account, which vault2
    // pays on its escrow hotkey, moves that onto the CTF pool hotkey and
    // transfers it to vault2's position there, recorded for the pool's next
    // epoch. It holds nothing after, and the next close captures the returned
    // stake with that epoch's emission. Conservation holds throughout.
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
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 0);
        assertEq(staking.stakes(ctfHotkey, carryColdkey), 0);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 700);
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
        assertTrue(vault2.conservationHolds());
    }

    // ------------------------------------------------------------------
    // CC-4: claims made by others
    // ------------------------------------------------------------------

    // CC-4: vault2's claims are anyone's, and always pay the leaf's coldkey.
    // A carry account someone else claimed is not claimed again: recycle with
    // the account, or with a zero coldkey, moves what the carry holds. With
    // nothing held there is nothing to recycle.
    function test_CC4_recycleMovesWhatOthersClaimedForIt() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.expectRevert(STCtfCarry.NothingToRecycle.selector);
        _recycle(rootIndex, _noAccount(), proof);

        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);
        assertEq(
            staking.stakes(V2_ESCROW, carryColdkey), 700, "held between calls only after another's claim"
        );
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), proof);
        assertEq(amount, 700);
        assertEq(vault2.claimed(CTF1, 1), 700);
        vm.expectRevert(STCtfCarry.NothingToRecycle.selector);
        _recycle(rootIndex, _carryAccount(700), proof);

        // A zero coldkey names no account: its other fields are ignored.
        staking.setStake(V2_ESCROW, carryColdkey, 80);
        (, amount) =
            _recycle(rootIndex, LedgerTree.Account({index: 9, coldkey: bytes32(0), cumulative: 1}), proof);
        assertEq(amount, 80);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 780);
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
    // claim included, which stays claimable; so does a position of 2^64 - 1 or
    // more, which the precompile would read as the whole position.
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
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 0);

        // Claimed by another, then the carry's own transfer is refused.
        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);
        staking.setFailTransferStake(true);
        vm.expectRevert("mock: transferStake down");
        _recycle(rootIndex, _noAccount(), proof);
        staking.setFailTransferStake(false);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 0);

        staking.setStake(V2_ESCROW, carryColdkey, uint256(type(uint64).max));
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
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 700);

        staking.setReentry(
            address(carry), abi.encodeCall(STCtfCarry.recycle, (rootIndex, _noAccount(), proof))
        );
        (, uint256 amount) = _recycle(rootIndex, _noAccount(), proof);
        assertFalse(staking.reentrySucceeded());
        assertEq(staking.reentryFailureSelector(), STCtfCarry.Reentrancy.selector);
        assertEq(amount, 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 700);
    }

    // ------------------------------------------------------------------
    // CC-8 to CC-10: amounts and epochs
    // ------------------------------------------------------------------

    // CC-8, §4.6: each stake move may land one rao short. The claim, the move,
    // the transfer and the next capture lose at most four rao a cycle, and the
    // carry records exactly what vault2's position gained.
    function test_CC8_roundingLosesAtMostFourRaoACycle() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        staking.setMoveStakeShortfall(1);
        staking.setTransferStakeShortfall(1);
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 697, "the claim, the move and the transfer each landed one rao short");
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 697);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 697);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 0);
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
    // on the pool hotkey alone waits for a recycle with stake on the escrow
    // hotkey. Stake on any other hotkey stays where it is.
    function test_CC9_donationsGoRoundWithTheCarry() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        staking.setStake(ctfHotkey, carryColdkey, 40);
        staking.setStake(ELSEWHERE, carryColdkey, 9);
        vm.expectRevert(STCtfCarry.NothingToRecycle.selector);
        _recycle(rootIndex, _noAccount(), V2TreeHelper.proof(levels, 1));
        staking.setStake(V2_ESCROW, carryColdkey, 3);
        (, uint256 amount) = _recycle(rootIndex, _carryAccount(700), V2TreeHelper.proof(levels, 1));
        assertEq(amount, 743);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 743);
        assertEq(staking.stakes(ctfHotkey, V2_SELF), 743);
        assertEq(staking.stakes(ctfHotkey, carryColdkey), 0);
        assertEq(staking.stakes(ELSEWHERE, carryColdkey), 9);
    }

    // CC-10, §4.6: a return is recorded for the pool's next unfunded epoch,
    // the epoch whose close captures it: recycles before a close and after it
    // land in consecutive epochs, and two in one epoch add up.
    function test_CC10_aReturnIsRecordedForTheEpochThatCapturesIt() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.prank(stranger);
        vault2.claim(CTF1, rootIndex, _carryAccount(700), proof);
        staking.setStake(V2_ESCROW, carryColdkey, 300);
        (uint256 epoch_,) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(epoch_, CTF_FIRST_EPOCH + 1);
        staking.setStake(V2_ESCROW, carryColdkey, 400);
        (epoch_,) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(epoch_, CTF_FIRST_EPOCH + 1);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 700);

        assertEq(_closeV2(CTF_FIRST_EPOCH + 1, CTF1), 700);
        staking.setStake(V2_ESCROW, carryColdkey, 25);
        (epoch_,) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(epoch_, CTF_FIRST_EPOCH + 2);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 2), 25);
        // A missed close defers the epoch; the next close captures the return.
        _closeV2(CTF_FIRST_EPOCH + 3, CTF1);
        assertEq(
            vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 2), vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 1)
        );
        assertEq(
            vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 3),
            vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH + 1) + 25
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
        staking.setMinimumMoveAmount(0);
        staking.setMinimumTransferAmount(701);
        vm.expectRevert("mock: transfer amount too low");
        _recycle(rootIndex, _noAccount(), proof);
        staking.setMinimumTransferAmount(0);
        staking.setExhaustTransferTo(V2_SELF, true);
        vm.expectRevert();
        _recycle(rootIndex, _noAccount(), proof);
        staking.setExhaustTransferTo(V2_SELF, false);
        assertEq(staking.stakes(V2_ESCROW, carryColdkey), 700);
        assertEq(carry.recycledFor(CTF_FIRST_EPOCH + 1), 0);

        (, uint256 amount) = _recycle(rootIndex, _noAccount(), proof);
        assertEq(amount, 700);
    }

    // ------------------------------------------------------------------
    // CC-11: nothing else goes out
    // ------------------------------------------------------------------

    // CC-11: the carry takes no native value and has no other entry point;
    // whoever calls recycle, the stake goes only to vault2's position on the
    // CTF pool hotkey, and the caller gains nothing.
    function test_CC11_theCarryCannotBeDrainedOrRedirected() public {
        (V2TreeHelper.Node[][] memory levels, uint256 rootIndex) = _firstRoot();
        vm.deal(stranger, 1 ether);
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
        assertEq(staking.stakes(ctfHotkey, V2_SELF) - vaultBefore, 700);
        assertEq(staking.stakes(V2_ESCROW, strangerColdkey), 0);
        assertEq(staking.stakes(ctfHotkey, strangerColdkey), 0);
        assertEq(stranger.balance, 1 ether);
        assertEq(address(carry).balance, 0);
    }
}
