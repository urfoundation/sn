// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {STSettlementVault2} from "../../src/STSettlementVault2.sol";
import {ISTSettlementVault2} from "../../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../../src/LedgerTree.sol";
import {ISTAKING_ADDRESS} from "../../src/interfaces/stakingV2.sol";
import {INeuron_ADDRESS} from "../../src/interfaces/neuron.sol";
import {MockStakingV2, MockNeuron} from "../mocks/PrecompileMocks.sol";
import {V2TreeHelper} from "./V2TreeHelper.sol";

/// @dev vault2 alone on the etched precompile mocks (ReleaseBase.sol:65-72),
///      in the standalone pattern of ReleaseSettlementMinimums.t.sol: this
///      test contract is both the bootstrap and the coordinator. Pool NO1 is
///      registered from FIRST_EPOCH and nothing is funded yet.
abstract contract V2VaultBase is Test {
    uint16 internal constant NETUID = 77;
    uint64 internal constant START_BLOCK = 1_000;
    uint64 internal constant BURN_LIMIT = 100_000_000;
    uint256 internal constant DEMAND_FEE = 0.01 ether;
    uint64 internal constant WINDOW = 14_400;
    uint64 internal constant FIRST_EPOCH = 5;
    uint256 internal constant NO1 = 1;
    uint256 internal constant NO2 = 2;
    uint16 internal constant ESCROW_UID = 10;
    uint16 internal constant POOL1_UID = 11;
    uint16 internal constant POOL2_UID = 12;
    bytes32 internal constant ESCROW = keccak256("v2-escrow-hotkey");
    bytes32 internal constant SELF = keccak256("v2-vault-coldkey");
    bytes32 internal constant POOL1 = keccak256("v2-pool-1");
    bytes32 internal constant POOL2 = keccak256("v2-pool-2");
    bytes32 internal constant ALICE = keccak256("alice-coldkey");
    bytes32 internal constant BOB = keccak256("bob-coldkey");
    bytes32 internal constant CAROL = keccak256("carol-coldkey");
    bytes32 internal constant DAVE = keccak256("dave-coldkey");
    bytes32 internal constant ERIN = keccak256("erin-coldkey");
    bytes32 internal constant FRANK = keccak256("frank-coldkey");
    bytes32 internal constant MALLORY = keccak256("mallory-coldkey");

    address internal stranger = makeAddr("stranger");

    STSettlementVault2 internal vault;
    MockStakingV2 internal staking = MockStakingV2(ISTAKING_ADDRESS);
    MockNeuron internal neuron = MockNeuron(INeuron_ADDRESS);

    function setUp() public virtual {
        vm.roll(START_BLOCK);
        vm.etch(ISTAKING_ADDRESS, address(new MockStakingV2()).code);
        vm.etch(INeuron_ADDRESS, address(new MockNeuron()).code);
        vault = new STSettlementVault2(NETUID, ESCROW, SELF, address(this), DEMAND_FEE);
        staking.setColdkey(address(vault), SELF);
        neuron.setUid(NETUID, ESCROW, ESCROW_UID);
        vault.registerEscrow(BURN_LIMIT);
        vault.setCoordinatorOnce(address(this));
        neuron.setUid(NETUID, POOL1, POOL1_UID);
        vault.registerPool(NO1, POOL1, BURN_LIMIT, FIRST_EPOCH);
    }

    // Registration refunds return here: this contract is bootstrap and coordinator.
    receive() external payable {}

    function _registerPool2() internal {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vault.registerPool(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH);
    }

    // The runtime gives a hotkey its UID while it registers; the neuron mock's
    // hook stands in for that.
    function _uidOnRegistration(bytes32 hotkey, uint16 uid) internal {
        neuron.setReentry(address(neuron), abi.encodeCall(MockNeuron.setUid, (NETUID, hotkey, uid)));
    }

    // A runtime that credits `coldkey` on `hotkey` with `extra` while it moves
    // or transfers stake: the staking mock's hook rewrites the position mid-call.
    function _creditDuringNextStakeCall(bytes32 hotkey, bytes32 coldkey, uint256 extra) internal {
        staking.setReentry(
            address(staking),
            abi.encodeCall(MockStakingV2.setStake, (hotkey, coldkey, staking.stakes(hotkey, coldkey) + extra))
        );
    }

    /// @dev Adds `amount` to the vault's stake on the pool hotkey, then
    ///      captures the pool's next epoch.
    function _capture(uint256 noId, uint256 amount) internal returns (uint256 epoch) {
        epoch = vault.lastFundedEpoch(noId) + 1;
        bytes32 hotkey = vault.pools(noId).hotkey;
        staking.setStake(hotkey, SELF, staking.stakes(hotkey, SELF) + amount);
        vault.captureEmission(epoch, noId);
    }

    function _defer(uint256 noId) internal returns (uint256 epoch) {
        epoch = vault.lastFundedEpoch(noId) + 1;
        vault.deferEmission(epoch, noId);
    }

    function _ledgerHash(bytes32 root) internal pure returns (bytes32) {
        return keccak256(abi.encode("ledger-header", root));
    }

    function _commitment(V2TreeHelper.Node[][] memory levels, uint256 leafCount)
        internal
        pure
        returns (ISTSettlementVault2.Commitment memory)
    {
        V2TreeHelper.Node memory top = V2TreeHelper.root(levels);
        return ISTSettlementVault2.Commitment({
            root: top.hash,
            total: top.sum,
            leafCount: SafeCast.toUint64(leafCount),
            ledgerHash: _ledgerHash(top.hash)
        });
    }

    /// @dev Commits the tree `levels` of `leafCount` accounts for the pool's
    ///      last funded epoch.
    function _commit(uint256 noId, V2TreeHelper.Node[][] memory levels, uint256 leafCount)
        internal
        returns (uint64 seq)
    {
        return vault.commitPending(
            vault.lastFundedEpoch(noId), noId, vault.rootCount(noId), _commitment(levels, leafCount)
        );
    }

    /// @dev Lets the pending root's challenge window pass, then finalizes it.
    function _finalize(uint256 noId) internal returns (uint256 rootIndex) {
        vm.roll(vault.pending(noId).committedBlock + WINDOW);
        return vault.finalizePending(noId);
    }

    /// @dev Funds the pool's next epoch with what `values` add to its captured
    ///      total, then commits and finalizes the honest tree of the accounts.
    function _publish(uint256 noId, bytes32[] memory coldkeys, uint256[] memory values)
        internal
        returns (V2TreeHelper.Node[][] memory levels)
    {
        _capture(noId, _sum(values) - vault.capturedTotal(noId));
        levels = V2TreeHelper.build(coldkeys, values);
        _commit(noId, levels, coldkeys.length);
        _finalize(noId);
    }

    /// @dev Funds the pool's next epoch like _publish and commits the tree,
    ///      leaving it pending.
    function _propose(uint256 noId, V2TreeHelper.Node[][] memory levels, uint256 leafCount)
        internal
        returns (uint64 seq)
    {
        _capture(noId, V2TreeHelper.root(levels).sum - vault.capturedTotal(noId));
        return _commit(noId, levels, leafCount);
    }

    function _claim(
        uint256 noId,
        uint256 rootIndex,
        V2TreeHelper.Node[][] memory levels,
        uint256 index,
        bytes32 coldkey,
        uint256 cumulative
    ) internal returns (uint256) {
        return vault.claim(
            noId, rootIndex, _account(index, coldkey, cumulative), V2TreeHelper.proof(levels, index)
        );
    }

    function _account(uint256 index, bytes32 coldkey, uint256 cumulative)
        internal
        pure
        returns (LedgerTree.Account memory)
    {
        return LedgerTree.Account({index: index, coldkey: coldkey, cumulative: cumulative});
    }

    function _sum(uint256[] memory values) internal pure returns (uint256 total) {
        for (uint256 i; i < values.length; i++) {
            total += values[i];
        }
    }

    function _keys(bytes32 a) internal pure returns (bytes32[] memory keys) {
        keys = new bytes32[](1);
        keys[0] = a;
    }

    function _keys(bytes32 a, bytes32 b) internal pure returns (bytes32[] memory keys) {
        keys = new bytes32[](2);
        (keys[0], keys[1]) = (a, b);
    }

    function _keys(bytes32 a, bytes32 b, bytes32 c) internal pure returns (bytes32[] memory keys) {
        keys = new bytes32[](3);
        (keys[0], keys[1], keys[2]) = (a, b, c);
    }

    function _keys(bytes32 a, bytes32 b, bytes32 c, bytes32 d, bytes32 e, bytes32 f)
        internal
        pure
        returns (bytes32[] memory keys)
    {
        keys = new bytes32[](6);
        (keys[0], keys[1], keys[2], keys[3], keys[4], keys[5]) = (a, b, c, d, e, f);
    }

    function _values(uint256 a) internal pure returns (uint256[] memory values) {
        values = new uint256[](1);
        values[0] = a;
    }

    function _values(uint256 a, uint256 b) internal pure returns (uint256[] memory values) {
        values = new uint256[](2);
        (values[0], values[1]) = (a, b);
    }

    function _values(uint256 a, uint256 b, uint256 c) internal pure returns (uint256[] memory values) {
        values = new uint256[](3);
        (values[0], values[1], values[2]) = (a, b, c);
    }

    function _values(uint256 a, uint256 b, uint256 c, uint256 d, uint256 e, uint256 f)
        internal
        pure
        returns (uint256[] memory values)
    {
        values = new uint256[](6);
        (values[0], values[1], values[2], values[3], values[4], values[5]) = (a, b, c, d, e, f);
    }
}
