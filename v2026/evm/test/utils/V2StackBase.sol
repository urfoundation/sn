// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {ReleaseBase} from "./ReleaseBase.sol";
import {V2TreeHelper} from "./V2TreeHelper.sol";
import {STCoordinator} from "../../src/STCoordinator.sol";
import {STSettlement2} from "../../src/STSettlement2.sol";
import {STSettlementVault2} from "../../src/STSettlementVault2.sol";
import {ISTSettlementVault2} from "../../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../../src/LedgerTree.sol";

/// @dev The whole settlement v2 stack on the etched precompile mocks:
///      ReleaseBase's real v1 coordinator at mainnet cadence from epoch 1
///      (50,400-block epochs, a 1,200-block root commit window), the real
///      vault2, and STSettlement2 behind its proxy as vault2's coordinator,
///      set once. This test contract is vault2's bootstrap. The auditor set is
///      the launch one: auditor1 with quorum 1. No v2 pool is registered yet.
abstract contract V2StackBase is ReleaseBase {
    uint64 internal constant V2_EPOCH_BLOCKS = 50_400;
    uint64 internal constant V2_ROOT_WINDOW = 1_200;
    uint64 internal constant WINDOW = 14_400;
    uint64 internal constant FIRST_EPOCH = 2;
    uint64 internal constant V2_BURN_LIMIT = 1_000_000_000;
    uint256 internal constant DEMAND_FEE = 0.01 ether;
    uint16 internal constant V2_ESCROW_UID = 20;
    uint16 internal constant V2_POOL1_UID = 21;
    uint16 internal constant V2_POOL2_UID = 22;
    bytes32 internal constant V2_ESCROW = keccak256("v2-escrow-hotkey");
    bytes32 internal constant V2_SELF = keccak256("v2-vault-coldkey");
    bytes32 internal constant V2_POOL1 = keccak256("v2-pool-1");
    bytes32 internal constant V2_POOL2 = keccak256("v2-pool-2");
    bytes32 internal constant ALICE = keccak256("alice-coldkey");
    bytes32 internal constant BOB = keccak256("bob-coldkey");
    bytes32 internal constant CAROL = keccak256("carol-coldkey");
    bytes32 internal constant MALLORY = keccak256("mallory-coldkey");

    address internal auditor1 = makeAddr("auditor-1");

    STSettlementVault2 internal vault2;
    STSettlement2 internal settlement;

    function setUp() public virtual override {
        super.setUp();
        _schedulePolicy(1, V2_EPOCH_BLOCKS);
        // A7: vault2's creation and its escrow registration go back to back.
        vault2 = new STSettlementVault2(NETUID, V2_ESCROW, V2_SELF, address(this), DEMAND_FEE);
        staking.setColdkey(address(vault2), V2_SELF);
        neuron.setUid(NETUID, V2_ESCROW, V2_ESCROW_UID);
        vault2.registerEscrow(V2_BURN_LIMIT);
        address[] memory auditors = new address[](1);
        auditors[0] = auditor1;
        ERC1967Proxy proxy = new ERC1967Proxy(
            address(new STSettlement2()),
            abi.encodeCall(
                STSettlement2.initialize, (owner, address(coordinator), address(vault2), auditors, 1)
            )
        );
        settlement = STSettlement2(payable(address(proxy)));
        vault2.setCoordinatorOnce(address(settlement));
    }

    /// @dev Schedules a v1 policy with mainnet's windows and the given cadence.
    function _schedulePolicy(uint64 effectiveEpoch, uint64 epochBlocks) internal {
        STCoordinator.PolicySnapshot memory next = _policy();
        next.effectiveEpoch = effectiveEpoch;
        next.epochBlocks = epochBlocks;
        next.rootCommitWindowBlocks = V2_ROOT_WINDOW;
        next.finalizeOffsetBlocks = 14_400;
        next.closeGraceBlocks = 120;
        next.commitmentMaxAgeBlocks = 100_800;
        vm.prank(owner);
        coordinator.schedulePolicy(next);
    }

    /// @dev Moves v1 pool `noId` to v2 at FIRST_EPOCH under `hotkey`.
    function _registerV2Pool(uint256 noId, bytes32 hotkey, uint16 uid) internal {
        neuron.setUid(NETUID, hotkey, uid);
        vm.prank(owner);
        settlement.registerOperatorPool(noId, hotkey, V2_BURN_LIMIT, FIRST_EPOCH);
    }

    /// @dev Emission arriving on a v2 pool hotkey, owned by vault2's coldkey.
    function _accrueV2(bytes32 hotkey, uint256 amount) internal {
        staking.setStake(hotkey, V2_SELF, staking.stakes(hotkey, V2_SELF) + amount);
    }

    /// @dev Closes `epoch_` at its end, as anyone may.
    function _closeV2(uint256 epoch_, uint256 noId) internal returns (uint256) {
        vm.roll(_end(epoch_));
        vm.prank(stranger);
        return settlement.closeOperatorEpoch(epoch_, noId);
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
            ledgerHash: keccak256(abi.encode("ledger-header", top.hash))
        });
    }

    /// @dev Commits the tree `levels` of `leafCount` accounts for `epoch_` at
    ///      the current block, as the epoch's root signer.
    function _commitV2(
        uint256 epoch_,
        uint256 noId,
        address signer,
        V2TreeHelper.Node[][] memory levels,
        uint256 leafCount
    ) internal returns (uint64 seq) {
        ISTSettlementVault2.Commitment memory commitment = _commitment(levels, leafCount);
        uint256 rootCount = vault2.rootCount(noId);
        vm.prank(signer);
        seq = settlement.commitDistribution(epoch_, noId, rootCount, commitment);
    }

    /// @dev `auditor` approves the pool's pending root exactly as committed.
    function _approveV2(uint256 noId, address auditor) internal {
        ISTSettlementVault2.Pending memory next = vault2.pending(noId);
        vm.prank(auditor);
        settlement.approveDistribution(noId, next.seq, next.root, next.ledgerHash);
    }

    /// @dev Lets the pending root's challenge window pass, then finalizes it.
    function _finalizeV2(uint256 noId) internal returns (uint256 rootIndex) {
        vm.roll(vault2.pending(noId).committedBlock + WINDOW);
        vm.prank(stranger);
        return settlement.finalizeDistribution(noId);
    }

    /// @dev Funds `epoch_` of pool NO1 with what `values` add to its captured
    ///      total, then closes, commits, approves and finalizes the honest tree.
    function _publishV2(uint256 epoch_, bytes32[] memory coldkeys, uint256[] memory values)
        internal
        returns (V2TreeHelper.Node[][] memory levels)
    {
        levels = V2TreeHelper.build(coldkeys, values);
        _accrueV2(V2_POOL1, V2TreeHelper.root(levels).sum - vault2.capturedTotal(NO1));
        _closeV2(epoch_, NO1);
        _commitV2(epoch_, NO1, rootSigner1, levels, coldkeys.length);
        _approveV2(NO1, auditor1);
        _finalizeV2(NO1);
    }

    /// @dev A stranger pays the fee and demands the opening of `account`,
    ///      proven against the finalized root `rootIndex`, the tree `levels`.
    function _demandV2(
        uint256 noId,
        uint256 rootIndex,
        V2TreeHelper.Node[][] memory levels,
        LedgerTree.Account memory account
    ) internal {
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, account.index);
        vm.deal(stranger, DEMAND_FEE);
        vm.prank(stranger);
        vault2.demandPath{value: DEMAND_FEE}(noId, rootIndex, account, proof);
    }

    function _account(uint256 index, bytes32 coldkey, uint256 cumulative)
        internal
        pure
        returns (LedgerTree.Account memory)
    {
        return LedgerTree.Account({index: index, coldkey: coldkey, cumulative: cumulative});
    }

    function _keys(bytes32 a, bytes32 b) internal pure returns (bytes32[] memory keys) {
        keys = new bytes32[](2);
        (keys[0], keys[1]) = (a, b);
    }

    function _keys(bytes32 a, bytes32 b, bytes32 c) internal pure returns (bytes32[] memory keys) {
        keys = new bytes32[](3);
        (keys[0], keys[1], keys[2]) = (a, b, c);
    }

    function _values(uint256 a, uint256 b) internal pure returns (uint256[] memory values) {
        values = new uint256[](2);
        (values[0], values[1]) = (a, b);
    }

    function _values(uint256 a, uint256 b, uint256 c) internal pure returns (uint256[] memory values) {
        values = new uint256[](3);
        (values[0], values[1], values[2]) = (a, b, c);
    }
}
