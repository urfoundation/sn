// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Vm, VmSafe} from "forge-std/Vm.sol";
import {stdError} from "forge-std/StdError.sol";

import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STClaimBatcher} from "../src/STClaimBatcher.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev Stands in for vault2 to measure the batcher: it records the gas left
///      at the start of each call it receives and pays 1.
contract V2GasProbeVault {
    uint256[] private _received;

    function received() external view returns (uint256[] memory) {
        return _received;
    }

    fallback() external {
        _received.push(gasleft());
        assembly {
            mstore(0, 1)
            return(0, 32)
        }
    }
}

/// @dev The claim batcher (VERSION2-FINAL.md §10.2; §15.10 CB-1 to CB-6,
///      CB-EV, CB-SIZE) against vault2 on the etched precompile mocks. Root 0
///      of pool NO1 holds Alice 500, Bob 600, Carol 700, Dave 50, Erin 800 and
///      Frank 900.
contract V2ClaimBatcherTest is V2VaultBase {
    uint256 internal constant GAS_PER_CLAIM = 1_000_000;
    bytes4 internal constant ERROR_STRING = bytes4(keccak256("Error(string)"));

    STClaimBatcher internal batcher;
    bytes32[] internal coldkeys;
    uint256[] internal values;

    function setUp() public override {
        super.setUp();
        batcher = new STClaimBatcher();
        coldkeys = _keys(ALICE, BOB, CAROL, DAVE, ERIN, FRANK);
        values = _values(500, 600, 700, 50, 800, 900);
    }

    function _root0() internal returns (V2TreeHelper.Node[][] memory) {
        return _publish(NO1, coldkeys, values);
    }

    // The gas an item needs when the batcher checks it.
    function _needed() internal view returns (uint256) {
        return GAS_PER_CLAIM + GAS_PER_CLAIM / 63 + batcher.BOOKKEEPING_GAS();
    }

    function _item(V2TreeHelper.Node[][] memory levels, uint256 index)
        internal
        view
        returns (STClaimBatcher.Claim memory)
    {
        return STClaimBatcher.Claim({
            noId: NO1,
            rootIndex: 0,
            account: _account(index, coldkeys[index], values[index]),
            proof: V2TreeHelper.proof(levels, index)
        });
    }

    function _items(V2TreeHelper.Node[][] memory levels, uint256[] memory indexes)
        internal
        view
        returns (STClaimBatcher.Claim[] memory items)
    {
        items = new STClaimBatcher.Claim[](indexes.length);
        for (uint256 i; i < indexes.length; i++) {
            items[i] = _item(levels, indexes[i]);
        }
    }

    function _all(V2TreeHelper.Node[][] memory levels) internal view returns (STClaimBatcher.Claim[] memory) {
        return _items(levels, _values(0, 1, 2, 3, 4, 5));
    }

    // The exact log of each event, as the recorded logs hold it.
    function _paid(uint256 item, uint256 amount) internal view returns (Vm.Log memory log) {
        log.topics = new bytes32[](1);
        log.topics[0] = STClaimBatcher.ClaimPaid.selector;
        log.data = abi.encode(item, amount);
        log.emitter = address(batcher);
    }

    function _refused(uint256 item, bytes4 selector) internal view returns (Vm.Log memory log) {
        log.topics = new bytes32[](1);
        log.topics[0] = STClaimBatcher.ClaimRefused.selector;
        log.data = abi.encode(item, selector);
        log.emitter = address(batcher);
    }

    function _notAttempted(uint256 item) internal view returns (Vm.Log memory log) {
        log.topics = new bytes32[](1);
        log.topics[0] = STClaimBatcher.ClaimsNotAttempted.selector;
        log.data = abi.encode(item);
        log.emitter = address(batcher);
    }

    // vault2's Claimed for account `index` against root `rootIndex`.
    function _claimed(uint256 rootIndex, uint256 index, uint256 cumulative, uint256 amount)
        internal
        view
        returns (Vm.Log memory log)
    {
        log.topics = new bytes32[](4);
        log.topics[0] = ISTSettlementVault2.Claimed.selector;
        log.topics[1] = bytes32(NO1);
        log.topics[2] = bytes32(index);
        log.topics[3] = coldkeys[index];
        log.data = abi.encode(rootIndex, cumulative, amount, address(batcher));
        log.emitter = address(vault);
    }

    function _assertLogs(Vm.Log[] memory expected) internal view {
        Vm.Log[] memory logs = vm.getRecordedLogs();
        assertEq(logs.length, expected.length, "log count");
        for (uint256 i; i < logs.length; i++) {
            assertEq(logs[i].emitter, expected[i].emitter, "log emitter");
            assertEq(logs[i].topics, expected[i].topics, "log topics");
            assertEq(logs[i].data, expected[i].data, "log data");
        }
    }

    function _assertConserved() internal view {
        assertTrue(vault.conservationHolds(), "conservation");
        assertEq(vault.totalCaptured(), vault.totalPaid() + vault.escrowAccounted());
    }

    // ------------------------------------------------------------------
    // CB-1: every item pays
    // ------------------------------------------------------------------

    // CB-1, CB-EV: each item pays its account's claimable to the leaf's
    // coldkey; vault2's Claimed names the batcher as caller, and the batcher
    // reports each payment after it.
    function test_CB1_everyItemPaysItsAccountsClaimable() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        vm.recordLogs();
        assertEq(batcher.claimMany(vault, _all(levels), GAS_PER_CLAIM), 6);

        Vm.Log[] memory expected = new Vm.Log[](12);
        for (uint256 i; i < 6; i++) {
            expected[2 * i] = _claimed(0, i, values[i], values[i]);
            expected[2 * i + 1] = _paid(i, values[i]);
            assertEq(staking.stakes(ESCROW, coldkeys[i]), values[i], "paid to the leaf's coldkey");
            assertEq(vault.claimed(NO1, i), values[i], "claimed watermark");
        }
        _assertLogs(expected);
        assertEq(vault.claimedTotal(NO1), 3550);
        assertEq(vault.liveEscrowStake(), 0);
        _assertConserved();
    }

    // CB-1, CB-EV: after a newer root, a batch pays each account only its
    // increment.
    function test_CB1_aBatchAfterANewerRootPaysOnlyEachIncrement() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        batcher.claimMany(vault, _items(levels, _values(0, 1)), GAS_PER_CLAIM);
        values = _values(1500, 600, 700, 50, 800, 1900);
        levels = _publish(NO1, coldkeys, values);
        STClaimBatcher.Claim[] memory items = _items(levels, _values(0, 5));
        (items[0].rootIndex, items[1].rootIndex) = (1, 1);

        vm.recordLogs();
        assertEq(batcher.claimMany(vault, items, GAS_PER_CLAIM), 2);
        Vm.Log[] memory expected = new Vm.Log[](4);
        expected[0] = _claimed(1, 0, 1500, 1000);
        expected[1] = _paid(0, 1000);
        expected[2] = _claimed(1, 5, 1900, 1900);
        expected[3] = _paid(1, 1900);
        _assertLogs(expected);
        assertEq(staking.stakes(ESCROW, ALICE), 1500);
        assertEq(staking.stakes(ESCROW, FRANK), 1900);
        _assertConserved();
    }

    // ------------------------------------------------------------------
    // CB-2: refusals
    // ------------------------------------------------------------------

    // CB-2, CB-EV: a claim a third party already made (NothingToClaim), an
    // invalid proof, the runtime's floor refusal and a refusal that consumes
    // the item's gas each report their selector and leave the account as it
    // was; the other items pay.
    function test_CB2_aMixPaysTheRestAndReportsEachRefusal() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        vm.prank(stranger);
        _claim(NO1, 0, levels, 1, BOB, 600);
        staking.setMinimumTransferAmount(100);
        staking.setExhaustTransferTo(ERIN, true);
        STClaimBatcher.Claim[] memory items = _all(levels);
        items[2].proof = V2TreeHelper.proof(levels, 3);

        vm.recordLogs();
        assertEq(batcher.claimMany(vault, items, GAS_PER_CLAIM), 6);
        Vm.Log[] memory expected = new Vm.Log[](8);
        expected[0] = _claimed(0, 0, 500, 500);
        expected[1] = _paid(0, 500);
        expected[2] = _refused(1, ISTSettlementVault2.NothingToClaim.selector);
        expected[3] = _refused(2, ISTSettlementVault2.InvalidProof.selector);
        expected[4] = _refused(3, ERROR_STRING);
        expected[5] = _refused(4, bytes4(0));
        expected[6] = _claimed(0, 5, 900, 900);
        expected[7] = _paid(5, 900);
        _assertLogs(expected);

        assertEq(staking.stakes(ESCROW, ALICE), 500);
        assertEq(staking.stakes(ESCROW, BOB), 600);
        assertEq(staking.stakes(ESCROW, FRANK), 900);
        for (uint256 i = 2; i < 5; i++) {
            assertEq(vault.claimed(NO1, i), 0, "a refused account stays claimable");
            assertEq(staking.stakes(ESCROW, coldkeys[i]), 0);
        }
        _assertConserved();

        // Once the runtime accepts them, the same accounts pay.
        staking.setMinimumTransferAmount(0);
        staking.setExhaustTransferTo(ERIN, false);
        assertEq(batcher.claimMany(vault, _items(levels, _values(2, 3, 4)), GAS_PER_CLAIM), 3);
        for (uint256 i = 2; i < 5; i++) {
            assertEq(staking.stakes(ESCROW, coldkeys[i]), values[i]);
        }
        assertEq(vault.liveEscrowStake(), 0);
        _assertConserved();
    }

    // CB-2, CB-EV: an unknown root index reports UnknownRoot.
    function test_CB2_anUnknownRootReportsItsSelector() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        STClaimBatcher.Claim[] memory items = _items(levels, _values(0));
        items[0].rootIndex = 1;
        vm.recordLogs();
        assertEq(batcher.claimMany(vault, items, GAS_PER_CLAIM), 1);
        Vm.Log[] memory expected = new Vm.Log[](1);
        expected[0] = _refused(0, ISTSettlementVault2.UnknownRoot.selector);
        _assertLogs(expected);
        assertEq(vault.claimed(NO1, 0), 0);
    }

    // ------------------------------------------------------------------
    // CB-3: the per-item gas cap
    // ------------------------------------------------------------------

    // CB-3: a refusal that consumes all the gas forwarded to the precompile
    // consumes nearly all of its item's cap and no more.
    function test_CB3_aRefusalThatConsumesItsGasUsesOnlyItsCap() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setExhaustTransferTo(BOB, true);
        vm.recordLogs();
        assertEq(batcher.claimMany(vault, _items(levels, _values(1)), GAS_PER_CLAIM), 1);
        uint256 used = vm.lastCallGas().gasTotalUsed;
        Vm.Log[] memory expected = new Vm.Log[](1);
        expected[0] = _refused(0, bytes4(0));
        _assertLogs(expected);
        assertGt(used, GAS_PER_CLAIM * 62 / 64, "the refusal consumed its cap");
        assertLt(used, GAS_PER_CLAIM + 15_000, "the refusal consumed only its cap");
        assertEq(vault.claimed(NO1, 1), 0);
    }

    // CB-3: with the gas a batch of its items needs, a refusal that consumes
    // its gas does not starve the next item. Uncapped, the refusal would have
    // consumed all but 1/64 of the gas left, too little for the next item.
    function test_CB3_aRefusalThatConsumesItsGasDoesNotStarveLaterItems() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setExhaustTransferTo(BOB, true);
        vm.recordLogs();
        assertEq(
            batcher.claimMany{gas: 3 * _needed()}(vault, _items(levels, _values(0, 1, 2)), GAS_PER_CLAIM), 3
        );
        Vm.Log[] memory expected = new Vm.Log[](5);
        expected[0] = _claimed(0, 0, 500, 500);
        expected[1] = _paid(0, 500);
        expected[2] = _refused(1, bytes4(0));
        expected[3] = _claimed(0, 2, 700, 700);
        expected[4] = _paid(2, 700);
        _assertLogs(expected);
        assertEq(staking.stakes(ESCROW, CAROL), 700);
        assertEq(vault.claimed(NO1, 1), 0);
        _assertConserved();
    }

    // CB-3: an item whose claim needs more gas than the cap runs out of it and
    // is refused, without data; the batch goes on.
    function test_CB3_aClaimThatNeedsMoreThanItsCapIsRefusedNotTheBatch() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        vm.recordLogs();
        assertEq(batcher.claimMany(vault, _items(levels, _values(0, 1)), 20_000), 2);
        Vm.Log[] memory expected = new Vm.Log[](2);
        expected[0] = _refused(0, bytes4(0));
        expected[1] = _refused(1, bytes4(0));
        _assertLogs(expected);
        assertEq(vault.claimed(NO1, 0), 0);
        assertEq(vault.claimed(NO1, 1), 0);
        _assertConserved();
    }

    // ------------------------------------------------------------------
    // CB-4: items the gas left cannot cover
    // ------------------------------------------------------------------

    // CB-4, CB-EV: with gas for one item, the first pays and the rest are
    // reported, once, as not attempted.
    function test_CB4_itemsTheGasLeftCannotCoverAreNotAttempted() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        vm.recordLogs();
        assertEq(
            batcher.claimMany{gas: _needed() + 10_000}(
                vault, _items(levels, _values(0, 1, 2)), GAS_PER_CLAIM
            ),
            1
        );
        Vm.Log[] memory expected = new Vm.Log[](3);
        expected[0] = _claimed(0, 0, 500, 500);
        expected[1] = _paid(0, 500);
        expected[2] = _notAttempted(1);
        _assertLogs(expected);
        assertEq(vault.claimed(NO1, 1), 0);
        assertEq(vault.claimed(NO1, 2), 0);
        _assertConserved();
    }

    // CB-4, CB-EV: without gas for the first item nothing is attempted.
    function test_CB4_withoutGasForTheFirstItemNothingIsAttempted() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        STClaimBatcher.Claim[] memory items = _all(levels);
        uint256 needed = _needed();
        vm.recordLogs();
        assertEq(batcher.claimMany{gas: needed}(vault, items, GAS_PER_CLAIM), 0);
        Vm.Log[] memory expected = new Vm.Log[](1);
        expected[0] = _notAttempted(0);
        _assertLogs(expected);
        assertEq(vault.claimedTotal(NO1), 0);
    }

    // CB-4: whatever gas the batch gets, each attempted item receives its
    // whole cap and no more, the items it attempts come first, and the batch
    // itself never runs out of gas.
    function testFuzz_CB4_anAttemptedItemReceivesExactlyItsCap(uint256 limit) public {
        V2GasProbeVault probe = new V2GasProbeVault();
        limit = bound(limit, 30_000, 5 * _needed());
        STClaimBatcher.Claim[] memory items = new STClaimBatcher.Claim[](5);
        vm.recordLogs();
        (bool ok, bytes memory result) = address(batcher).call{gas: limit}(
            abi.encodeCall(
                STClaimBatcher.claimMany, (ISTSettlementVault2(address(probe)), items, GAS_PER_CLAIM)
            )
        );
        assertTrue(ok, "the batch completed");
        uint256 attempted = abi.decode(result, (uint256));
        uint256[] memory received = probe.received();
        assertEq(received.length, attempted);
        for (uint256 i; i < attempted; i++) {
            assertLe(received[i], GAS_PER_CLAIM, "no more than the cap");
            assertGe(received[i], GAS_PER_CLAIM - 2_000, "the whole cap");
        }
        Vm.Log[] memory logs = vm.getRecordedLogs();
        assertEq(logs.length, attempted < 5 ? attempted + 1 : 5);
        if (attempted < 5) {
            assertEq(logs[attempted].topics[0], STClaimBatcher.ClaimsNotAttempted.selector);
            assertEq(logs[attempted].data, abi.encode(attempted));
        }
    }

    // CB-4: an item is attempted from the gas its cap needs at the call, with
    // the 1/64 the call keeps (EIP-150), plus the bookkeeping, and not below.
    function test_CB4_theFirstItemIsAttemptedFromItsRequirement() public {
        V2GasProbeVault probe = new V2GasProbeVault();
        STClaimBatcher.Claim[] memory items = new STClaimBatcher.Claim[](1);
        uint256 needed = _needed();
        uint256 low = 30_000;
        uint256 high = 2 * needed;
        while (high - low > 1) {
            uint256 middle = (low + high) / 2;
            if (
                batcher.claimMany{gas: middle}(ISTSettlementVault2(address(probe)), items, GAS_PER_CLAIM) == 1
            ) {
                high = middle;
            } else {
                low = middle;
            }
        }
        // The call's entry and the check itself cost a little more.
        assertGe(high, needed, "attempted below the requirement");
        assertLt(high, needed + 2_000, "not attempted at the requirement");
    }

    // ------------------------------------------------------------------
    // CB-5: destination, funds and storage
    // ------------------------------------------------------------------

    // CB-5: whoever calls, each payment goes to its leaf's coldkey; the
    // escrow loses exactly what the coldkeys gain, and neither the caller nor
    // the batcher gains stake.
    function test_CB5_paymentsGoOnlyToEachLeafsColdkey() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        bytes32 strangerColdkey = keccak256("stranger-coldkey");
        bytes32 batcherColdkey = keccak256("batcher-coldkey");
        staking.setColdkey(stranger, strangerColdkey);
        staking.setColdkey(address(batcher), batcherColdkey);
        uint256 escrow = staking.stakes(ESCROW, SELF);
        STClaimBatcher.Claim[] memory items = _all(levels);
        vm.prank(stranger);
        batcher.claimMany(vault, items, GAS_PER_CLAIM);
        for (uint256 i; i < 6; i++) {
            assertEq(staking.stakes(ESCROW, coldkeys[i]), values[i]);
        }
        assertEq(escrow - staking.stakes(ESCROW, SELF), 3550);
        assertEq(staking.stakes(ESCROW, strangerColdkey), 0);
        assertEq(staking.stakes(ESCROW, batcherColdkey), 0);
        _assertConserved();
    }

    // CB-5: the batcher touches no storage of its own and holds no funds:
    // claimMany takes no value and nothing else receives any.
    function test_CB5_theBatcherHoldsNoFundsAndNoStorage() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        STClaimBatcher.Claim[] memory items = _all(levels);
        vm.record();
        batcher.claimMany(vault, items, GAS_PER_CLAIM);
        (bytes32[] memory reads, bytes32[] memory writes) = vm.accesses(address(batcher));
        assertEq(reads.length, 0, "storage reads");
        assertEq(writes.length, 0, "storage writes");

        vm.deal(address(this), 1 ether);
        (bool ok,) = address(batcher).call{value: 1}("");
        assertFalse(ok, "plain value accepted");
        (ok,) = address(batcher).call{value: 1}(
            abi.encodeCall(STClaimBatcher.claimMany, (vault, new STClaimBatcher.Claim[](0), GAS_PER_CLAIM))
        );
        assertFalse(ok, "claimMany accepted value");
        assertEq(address(batcher).balance, 0);
    }

    // CB-5: the batcher's code has no storage opcode, persistent or transient
    // (SLOAD, SSTORE, TLOAD, TSTORE). Runtime 475 keeps a transient write made
    // in a call that reverts (rehearsal/transient_test.go), so a batcher that
    // catches reverts must not rely on one. The scan skips push data and stops
    // at the CBOR metadata, whose length the last two bytes give.
    function test_CB5_theBatcherCodeHasNoStorageOpcode() public view {
        bytes memory code = address(batcher).code;
        uint256 end =
            code.length - 2 - ((uint256(uint8(code[code.length - 2])) << 8) | uint8(code[code.length - 1]));
        uint256 calls;
        for (uint256 i; i < end; i++) {
            uint8 op = uint8(code[i]);
            assertTrue(op != 0x54 && op != 0x55 && op != 0x5c && op != 0x5d, "storage opcode");
            if (op == 0xf1) calls++;
            if (op >= 0x60 && op <= 0x7f) i += op - 0x5f;
        }
        assertGt(calls, 0, "the scan found no CALL");
    }

    // ------------------------------------------------------------------
    // CB-6: an empty batch and an unusable cap
    // ------------------------------------------------------------------

    // CB-6: an empty batch calls nothing and reports nothing.
    function test_CB6_anEmptyBatchDoesNothing() public {
        _root0();
        vm.recordLogs();
        assertEq(batcher.claimMany(vault, new STClaimBatcher.Claim[](0), GAS_PER_CLAIM), 0);
        _assertLogs(new Vm.Log[](0));
    }

    // CB-6: a cap whose requirement overflows reverts the batch.
    function test_CB6_aCapWhoseRequirementOverflowsReverts() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        STClaimBatcher.Claim[] memory items = _all(levels);
        vm.expectRevert(stdError.arithmeticError);
        batcher.claimMany(vault, items, type(uint256).max);
    }

    // ------------------------------------------------------------------
    // CB-SIZE
    // ------------------------------------------------------------------

    // CB-SIZE: sizes from the default profile, which is the release build
    // (`forge build --sizes`). A coverage build is unoptimized, so it is skipped.
    function test_CBSIZE_theBatcherFitsTheContractSizeLimits() public {
        vm.skip(vm.isContext(VmSafe.ForgeContext.Coverage));
        uint256 runtime = address(batcher).code.length;
        uint256 creation = type(STClaimBatcher).creationCode.length;
        emit log_named_uint("batcher runtime bytes", runtime);
        emit log_named_uint("batcher creation bytes", creation);
        assertLt(runtime, 24_576);
        assertLt(creation, 49_152);
    }
}
