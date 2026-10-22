// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {STSettlementVault2} from "../src/STSettlementVault2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev Accepts no native transfer, so a registration refund to it fails.
contract V2RefundRejecter {
    function registerEscrow(STSettlementVault2 target, uint64 maximumBurnRao) external payable {
        target.registerEscrow{value: msg.value}(maximumBurnRao);
    }

    function registerPoolUid(STSettlementVault2 target, uint256 noId, uint64 maximumBurnRao)
        external
        payable
    {
        target.registerPoolUid{value: msg.value}(noId, maximumBurnRao);
    }
}

/// @dev vault2 installation and pool registration (VERSION2-FINAL.md §15.3
///      V-1, V-2, V-3, V-23).
contract V2VaultRegistrationTest is V2VaultBase {
    bytes32 internal constant FRESH_ESCROW = keccak256("v2-fresh-escrow-hotkey");
    uint16 internal constant FRESH_ESCROW_UID = 20;
    uint16 internal constant REREGISTERED_UID = 42;

    // A vault with no escrow and no coordinator, bootstrapped by `bootstrap_`.
    function _fresh(address bootstrap_) internal returns (STSettlementVault2) {
        return new STSettlementVault2(NETUID, FRESH_ESCROW, SELF, bootstrap_, DEMAND_FEE);
    }

    function _prunePool1() internal {
        neuron.clearUid(NETUID, POOL1);
    }

    // ------------------------------------------------------------------
    // V-1: constructor, setCoordinatorOnce, registerEscrow
    // ------------------------------------------------------------------

    // V-1
    function test_V1_constructorKeepsItsConfiguration() public view {
        assertEq(vault.CHALLENGE_BLOCKS(), 14_400);
        assertEq(vault.netuid(), NETUID);
        assertEq(vault.escrowHotkey(), ESCROW);
        assertEq(vault.selfColdkey(), SELF);
        assertEq(vault.bootstrap(), address(this));
        assertEq(vault.demandFeeWei(), DEMAND_FEE);
        assertEq(vault.coordinator(), address(this));
        assertTrue(vault.escrowRegistered());
    }

    // V-1
    function test_V1_constructorRefusesZeroNetuid() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        new STSettlementVault2(0, ESCROW, SELF, address(this), DEMAND_FEE);
    }

    // V-1
    function test_V1_constructorRefusesZeroEscrowHotkey() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        new STSettlementVault2(NETUID, bytes32(0), SELF, address(this), DEMAND_FEE);
    }

    // V-1
    function test_V1_constructorRefusesZeroSelfColdkey() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        new STSettlementVault2(NETUID, ESCROW, bytes32(0), address(this), DEMAND_FEE);
    }

    // V-1
    function test_V1_constructorRefusesZeroBootstrap() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        new STSettlementVault2(NETUID, ESCROW, SELF, address(0), DEMAND_FEE);
    }

    // V-1
    function test_V1_constructorRefusesZeroDemandFee() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        new STSettlementVault2(NETUID, ESCROW, SELF, address(this), 0);
    }

    // V-1 (A5): the runtime truncates native value below a rao, so the fee is
    // a whole number of rao.
    function test_V1_constructorRefusesADemandFeeThatIsNotWholeRao() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        new STSettlementVault2(NETUID, ESCROW, SELF, address(this), 1e9 + 1);
    }

    // V-1 (A5)
    function test_V1_constructorAcceptsADemandFeeOfOneRao() public {
        assertEq(new STSettlementVault2(NETUID, ESCROW, SELF, address(this), 1e9).demandFeeWei(), 1e9);
    }

    // V-1
    function test_V1_bootstrapFixesTheCoordinator() public {
        STSettlementVault2 fresh = _fresh(address(this));
        address coordinator = address(new V2RefundRejecter());
        vm.expectEmit(address(fresh));
        emit ISTSettlementVault2.CoordinatorFixed(coordinator);
        fresh.setCoordinatorOnce(coordinator);
        assertEq(fresh.coordinator(), coordinator);
    }

    // V-1
    function test_V1_setCoordinatorOnceRefusesASecondCoordinator() public {
        address second = address(new V2RefundRejecter());
        vm.expectRevert(ISTSettlementVault2.AlreadyInitialized.selector);
        vault.setCoordinatorOnce(second);
    }

    // V-1
    function test_V1_setCoordinatorOnceRefusesZero() public {
        STSettlementVault2 fresh = _fresh(address(this));
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        fresh.setCoordinatorOnce(address(0));
    }

    // V-1
    function test_V1_setCoordinatorOnceRefusesACodelessAddress() public {
        STSettlementVault2 fresh = _fresh(address(this));
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        fresh.setCoordinatorOnce(makeAddr("codeless-coordinator"));
    }

    // V-1
    function test_V1_registerEscrowRegistersAndRefundsTheSurplus() public {
        STSettlementVault2 fresh = _fresh(address(this));
        neuron.setUid(NETUID, FRESH_ESCROW, FRESH_ESCROW_UID);
        neuron.setRegistrationBurn(0.25 ether);
        vm.deal(address(this), 1 ether);

        vm.expectEmit(address(fresh));
        emit ISTSettlementVault2.EscrowRegistered(FRESH_ESCROW, FRESH_ESCROW_UID);
        uint16 uid = fresh.registerEscrow{value: 1 ether}(BURN_LIMIT);

        assertEq(uid, FRESH_ESCROW_UID);
        assertTrue(fresh.escrowRegistered());
        assertEq(address(this).balance, 0.75 ether, "the bootstrap pays only the burn");
        assertEq(address(fresh).balance, 0);
        assertEq(neuron.registrants(NETUID, FRESH_ESCROW), address(fresh));
        assertEq(neuron.lastLimitPrice(), BURN_LIMIT);
        assertEq(neuron.lastRegistrationCallValue(), 0, "msg.value funds the mirror, not the precompile");
    }

    // V-1
    function test_V1_registerEscrowRefusesASecondRegistration() public {
        vm.expectRevert(ISTSettlementVault2.AlreadyInitialized.selector);
        vault.registerEscrow(BURN_LIMIT);
    }

    // V-1
    function test_V1_registerEscrowRefusesAZeroBurnLimit() public {
        STSettlementVault2 fresh = _fresh(address(this));
        neuron.setUid(NETUID, FRESH_ESCROW, FRESH_ESCROW_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        fresh.registerEscrow(0);
    }

    // V-1
    function test_V1_registerEscrowRefusesAMissingUid() public {
        STSettlementVault2 fresh = _fresh(address(this));
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        fresh.registerEscrow(BURN_LIMIT);
        assertFalse(fresh.escrowRegistered());
    }

    // V-1
    function test_V1_registerEscrowRefusesAFailedRefund() public {
        V2RefundRejecter rejecter = new V2RefundRejecter();
        STSettlementVault2 fresh = _fresh(address(rejecter));
        neuron.setUid(NETUID, FRESH_ESCROW, FRESH_ESCROW_UID);
        vm.deal(address(this), 1 ether);
        vm.expectRevert(ISTSettlementVault2.NativeRefundFailed.selector);
        rejecter.registerEscrow{value: 1 ether}(fresh, BURN_LIMIT);
    }

    // V-1
    function test_V1_registerEscrowRefusesToSpendTheVaultsBalance() public {
        STSettlementVault2 fresh = _fresh(address(this));
        neuron.setUid(NETUID, FRESH_ESCROW, FRESH_ESCROW_UID);
        vm.deal(address(fresh), 1 ether);
        neuron.setRegistrationBurn(0.5 ether);
        vm.deal(address(this), 0.1 ether);
        vm.expectRevert(ISTSettlementVault2.NativeBalanceDecreased.selector);
        fresh.registerEscrow{value: 0.1 ether}(BURN_LIMIT);
    }

    // V-1
    function test_V1_registerEscrowPropagatesARuntimeRefusal() public {
        STSettlementVault2 fresh = _fresh(address(this));
        neuron.setUid(NETUID, FRESH_ESCROW, FRESH_ESCROW_UID);
        neuron.setFailRegistration(true);
        vm.expectRevert(bytes("mock: registration refused"));
        fresh.registerEscrow(BURN_LIMIT);
    }

    // ------------------------------------------------------------------
    // V-2: registerPool
    // ------------------------------------------------------------------

    // V-2
    function test_V2_registerPoolRecordsThePoolAndRefunds() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        neuron.setRegistrationBurn(0.25 ether);
        vm.deal(address(this), 1 ether);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PoolRegistered(NO2, POOL2, POOL2_UID, 9);
        uint16 uid = vault.registerPool{value: 1 ether}(NO2, POOL2, BURN_LIMIT, 9);

        assertEq(uid, POOL2_UID);
        ISTSettlementVault2.Pool memory pool = vault.pools(NO2);
        assertEq(pool.hotkey, POOL2);
        assertEq(pool.firstEpoch, 9);
        assertTrue(pool.registered);
        assertTrue(vault.poolHotkeyUsed(POOL2));
        assertEq(vault.lastFundedEpoch(NO2), 8, "lastFundedEpoch = firstEpoch - 1");
        assertEq(vault.fundedThrough(NO2, 8), 0);
        assertEq(address(this).balance, 0.75 ether, "the coordinator pays only the burn");
        assertEq(address(vault).balance, 0);
        assertEq(neuron.registrants(NETUID, POOL2), address(vault));
        assertEq(neuron.lastLimitPrice(), BURN_LIMIT);
        assertEq(neuron.lastRegistrationCallValue(), 0);
    }

    // V-2: a pool reads empty until it is registered.
    function test_V2_anUnregisteredPoolReadsEmpty() public view {
        ISTSettlementVault2.Pool memory pool = vault.pools(NO2);
        assertEq(pool.hotkey, bytes32(0));
        assertEq(pool.firstEpoch, 0);
        assertFalse(pool.registered);
        assertEq(vault.lastFundedEpoch(NO2), 0);
        assertEq(vault.fundedThrough(NO2, 0), 0);
        assertEq(vault.capturedTotal(NO2), 0);
        assertEq(vault.claimedTotal(NO2), 0);
        assertEq(vault.rootCount(NO2), 0);
        assertEq(vault.finalizedTotal(NO2), 0);
        assertEq(vault.commitSeq(NO2), 0);
        assertEq(vault.lastCommitBlock(NO2), 0);
        assertEq(vault.pending(NO2).seq, 0);
        assertEq(vault.claimed(NO2, 0), 0);
    }

    // V-2
    function test_V2_registerPoolAcceptsTheEdges() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vault.registerPool(type(uint64).max, POOL2, 1, 1);
        assertEq(vault.lastFundedEpoch(type(uint64).max), 0);
        assertEq(vault.pools(type(uint64).max).firstEpoch, 1);
    }

    // V-2
    function test_V2_registerPoolRefusesNoIdZero() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(0, POOL2, BURN_LIMIT, FIRST_EPOCH);
    }

    // V-2
    function test_V2_registerPoolRefusesNoIdOf2To64() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(uint256(type(uint64).max) + 1, POOL2, BURN_LIMIT, FIRST_EPOCH);
    }

    // V-2
    function test_V2_registerPoolRefusesAZeroHotkey() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(NO2, bytes32(0), BURN_LIMIT, FIRST_EPOCH);
    }

    // V-2
    function test_V2_registerPoolRefusesADuplicateHotkey() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(NO2, POOL1, BURN_LIMIT, FIRST_EPOCH);
    }

    // V-2
    function test_V2_registerPoolRefusesTheEscrowHotkey() public {
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(NO2, ESCROW, BURN_LIMIT, FIRST_EPOCH);
    }

    // V-2
    function test_V2_registerPoolRefusesAZeroBurnLimit() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(NO2, POOL2, 0, FIRST_EPOCH);
    }

    // V-2
    function test_V2_registerPoolRefusesFirstEpochZero() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(NO2, POOL2, BURN_LIMIT, 0);
    }

    // V-2
    function test_V2_registerPoolRefusesARegisteredNoId() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPool(NO1, POOL2, BURN_LIMIT, FIRST_EPOCH);
    }

    // V-2 (review L1): no pool before the escrow. Had someone else registered
    // the escrow hotkey first (A7), its owner could swap it away with every
    // position on it, so the installation stops at registerEscrow.
    function test_V2_registerPoolRefusesBeforeTheEscrowIsRegistered() public {
        STSettlementVault2 fresh = _fresh(address(this));
        fresh.setCoordinatorOnce(address(this));
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        fresh.registerPool(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH);
        assertFalse(fresh.pools(NO2).registered);

        neuron.setUid(NETUID, FRESH_ESCROW, FRESH_ESCROW_UID);
        fresh.registerEscrow(BURN_LIMIT);
        assertEq(fresh.registerPool(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH), POOL2_UID);
    }

    // V-2
    function test_V2_registerPoolRefusesAMissingUid() public {
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        vault.registerPool(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH);
        assertFalse(vault.pools(NO2).registered);
        assertFalse(vault.poolHotkeyUsed(POOL2));
    }

    // V-2
    function test_V2_registerPoolRefusesToSpendTheVaultsBalance() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.deal(address(vault), 1 ether);
        neuron.setRegistrationBurn(0.5 ether);
        vm.deal(address(this), 0.1 ether);
        vm.expectRevert(ISTSettlementVault2.NativeBalanceDecreased.selector);
        vault.registerPool{value: 0.1 ether}(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH);
    }

    // V-2
    function test_V2_registerPoolPropagatesARuntimeRefusal() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        neuron.setFailRegistration(true);
        vm.expectRevert(bytes("mock: registration refused"));
        vault.registerPool(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH);
    }

    // ------------------------------------------------------------------
    // V-3: registerPoolUid
    // ------------------------------------------------------------------

    // V-3
    function test_V3_anyoneReRegistersAPrunedPoolHotkeyAndPaysTheBurn() public {
        _prunePool1();
        _uidOnRegistration(POOL1, REREGISTERED_UID);
        neuron.setRegistrationBurn(0.3 ether);
        vm.deal(address(vault), 0.2 ether); // demand fees already held
        vm.deal(stranger, 1 ether);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PoolUidRegistered(NO1, POOL1, REREGISTERED_UID, stranger);
        vm.prank(stranger);
        uint16 uid = vault.registerPoolUid{value: 1 ether}(NO1, BURN_LIMIT);

        assertEq(uid, REREGISTERED_UID);
        assertEq(stranger.balance, 0.7 ether, "the caller pays exactly the burn");
        assertEq(address(vault).balance, 0.2 ether, "the vault's own balance is untouched");
        assertEq(neuron.lastHotkey(), POOL1);
        assertEq(neuron.lastRegistrant(), address(vault));
        assertEq(neuron.lastLimitPrice(), BURN_LIMIT);
        assertEq(neuron.lastRegistrationCallValue(), 0);
    }

    // V-3
    function test_V3_registerPoolUidPaysAnExactBurnWithoutARefund() public {
        _prunePool1();
        _uidOnRegistration(POOL1, REREGISTERED_UID);
        neuron.setRegistrationBurn(0.3 ether);
        vm.deal(stranger, 0.3 ether);
        vm.prank(stranger);
        vault.registerPoolUid{value: 0.3 ether}(NO1, BURN_LIMIT);
        assertEq(stranger.balance, 0);
        assertEq(address(vault).balance, 0);
    }

    // V-3
    function test_V3_registerPoolUidRefusesAnUnknownPool() public {
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        vault.registerPoolUid(NO2, BURN_LIMIT);
    }

    // V-3
    function test_V3_registerPoolUidRefusesWhileTheUidExists() public {
        vm.expectRevert(ISTSettlementVault2.UidExists.selector);
        vault.registerPoolUid(NO1, BURN_LIMIT);
    }

    // V-3
    function test_V3_registerPoolUidRefusesAZeroBurnLimit() public {
        _prunePool1();
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        vault.registerPoolUid(NO1, 0);
    }

    // V-3
    function test_V3_registerPoolUidRefusesAMissingUidAfterRegistration() public {
        _prunePool1();
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        vault.registerPoolUid(NO1, BURN_LIMIT);
    }

    // V-3
    function test_V3_registerPoolUidRefusesToSpendTheVaultsBalance() public {
        _prunePool1();
        _uidOnRegistration(POOL1, REREGISTERED_UID);
        vm.deal(address(vault), 1 ether);
        neuron.setRegistrationBurn(0.5 ether);
        vm.deal(stranger, 0.1 ether);
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.NativeBalanceDecreased.selector);
        vault.registerPoolUid{value: 0.1 ether}(NO1, BURN_LIMIT);
    }

    // V-3
    function test_V3_registerPoolUidRefusesAFailedRefund() public {
        _prunePool1();
        _uidOnRegistration(POOL1, REREGISTERED_UID);
        V2RefundRejecter rejecter = new V2RefundRejecter();
        vm.deal(address(this), 1 ether);
        vm.expectRevert(ISTSettlementVault2.NativeRefundFailed.selector);
        rejecter.registerPoolUid{value: 1 ether}(vault, NO1, BURN_LIMIT);
    }

    // V-3
    function test_V3_registerPoolUidPropagatesARuntimeRefusal() public {
        _prunePool1();
        neuron.setFailRegistration(true);
        vm.expectRevert(bytes("mock: registration refused"));
        vault.registerPoolUid(NO1, BURN_LIMIT);
    }

    // ------------------------------------------------------------------
    // V-23: coordinator-only and bootstrap-only functions
    // ------------------------------------------------------------------

    // V-23
    function test_V23_setCoordinatorOnceRefusesAnyoneButTheBootstrap() public {
        STSettlementVault2 fresh = _fresh(address(this));
        address coordinator = address(new V2RefundRejecter());
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        fresh.setCoordinatorOnce(coordinator);
    }

    // V-23
    function test_V23_registerEscrowRefusesAnyoneButTheBootstrap() public {
        STSettlementVault2 fresh = _fresh(address(this));
        neuron.setUid(NETUID, FRESH_ESCROW, FRESH_ESCROW_UID);
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        fresh.registerEscrow(BURN_LIMIT);
    }

    // V-23
    function test_V23_registerPoolRefusesAnyoneButTheCoordinator() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        vault.registerPool(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH);
    }

    // V-23
    function test_V23_captureEmissionRefusesAnyoneButTheCoordinator() public {
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-23
    function test_V23_deferEmissionRefusesAnyoneButTheCoordinator() public {
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        vault.deferEmission(FIRST_EPOCH, NO1);
    }

    // V-23
    function test_V23_commitPendingRefusesAnyoneButTheCoordinator() public {
        _capture(NO1, 100);
        ISTSettlementVault2.Commitment memory commitment = ISTSettlementVault2.Commitment({
            root: keccak256("root"), total: 100, leafCount: 1, ledgerHash: keccak256("ledger")
        });
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 0, commitment);
    }

    // V-23
    function test_V23_finalizePendingRefusesAnyoneButTheCoordinator() public {
        _capture(NO1, 100);
        vault.commitPending(
            FIRST_EPOCH,
            NO1,
            0,
            ISTSettlementVault2.Commitment({
                root: keccak256("root"), total: 100, leafCount: 1, ledgerHash: keccak256("ledger")
            })
        );
        vm.roll(block.number + WINDOW);
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        vault.finalizePending(NO1);
    }

    // V-23
    function test_V23_assignSurplusRefusesAnyoneButTheCoordinator() public {
        staking.setStake(ESCROW, SELF, 100);
        vm.prank(stranger);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        vault.assignSurplus(NO1, 100);
    }
}
