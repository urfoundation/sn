// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

import {ReleaseBase} from "./ReleaseBase.sol";
import {STCoordinator} from "../../src/STCoordinator.sol";
import {STSettlement2} from "../../src/STSettlement2.sol";
import {ISTSettlementVault2} from "../../src/interfaces/ISTSettlementVault2.sol";
import {MockSettlementVault2} from "../mocks/MockSettlementVault2.sol";

/// @dev STSettlement2 behind its proxy, reading ReleaseBase's real v1
///      coordinator for operators, windows and pause. Epoch 0 keeps
///      ReleaseBase's 100-block policy; mainnet's cadence (50,400-block epochs,
///      a 1,200-block root commit window) applies from epoch 1. vault2 is
///      MockSettlementVault2. Pool NO1 moves to v2 at FIRST_EPOCH once
///      _registerPool1 runs; NO2 stays on v1.
abstract contract V2SettlementBase is ReleaseBase {
    uint64 internal constant V2_EPOCH_BLOCKS = 50_400;
    uint64 internal constant V2_ROOT_WINDOW = 1_200;
    uint64 internal constant FIRST_EPOCH = 2;
    uint64 internal constant V2_BURN_LIMIT = 1_000_000_000;
    bytes32 internal constant V2_POOL1 = keccak256("v2-pool-1");
    bytes32 internal constant ROOT = keccak256("v2-root");
    bytes32 internal constant LEDGER = keccak256("v2-ledger-header");

    address internal auditor1 = makeAddr("auditor-1");
    address internal auditor2 = makeAddr("auditor-2");
    address internal auditor3 = makeAddr("auditor-3");

    STSettlement2 internal settlementImplementation;
    STSettlement2 internal settlement;
    MockSettlementVault2 internal vault2;

    function setUp() public virtual override {
        super.setUp();
        _schedulePolicy(1, V2_EPOCH_BLOCKS);
        vault2 = new MockSettlementVault2();
        settlementImplementation = new STSettlement2();
        settlement = _deploy(owner, address(coordinator), address(vault2), _list(auditor1), 1);
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

    function _deploy(
        address owner_,
        address v1Coordinator_,
        address vault_,
        address[] memory auditors_,
        uint256 quorum_
    ) internal returns (STSettlement2) {
        ERC1967Proxy proxy = new ERC1967Proxy(
            address(settlementImplementation),
            abi.encodeCall(STSettlement2.initialize, (owner_, v1Coordinator_, vault_, auditors_, quorum_))
        );
        return STSettlement2(payable(address(proxy)));
    }

    function _list(address a) internal pure returns (address[] memory list) {
        list = new address[](1);
        list[0] = a;
    }

    function _list(address a, address b) internal pure returns (address[] memory list) {
        list = new address[](2);
        list[0] = a;
        list[1] = b;
    }

    function _list(address a, address b, address c) internal pure returns (address[] memory list) {
        list = new address[](3);
        list[0] = a;
        list[1] = b;
        list[2] = c;
    }

    function _setAuditors(address[] memory auditors_, uint256 quorum_) internal {
        vm.prank(owner);
        settlement.setAuditors(auditors_, quorum_);
    }

    function _registerPool1() internal {
        vm.prank(owner);
        settlement.registerOperatorPool(NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
    }

    function _commitment(bytes32 root) internal pure returns (ISTSettlementVault2.Commitment memory) {
        return ISTSettlementVault2.Commitment({root: root, total: 5_000, leafCount: 3, ledgerHash: LEDGER});
    }

    /// @dev Commits NO1's root for `epoch_` at end(epoch_) as its root signer.
    function _commit(uint256 epoch_, bytes32 root) internal returns (uint64 seq) {
        vm.roll(_end(epoch_));
        vm.prank(rootSigner1);
        seq = settlement.commitDistribution(epoch_, NO1, 0, _commitment(root));
    }

    function _approve(address auditor, uint64 seq, bytes32 root) internal {
        vm.prank(auditor);
        settlement.approveDistribution(NO1, seq, root, LEDGER);
    }

    function _pause() internal {
        vm.prank(guardian);
        coordinator.setPaused(true);
    }

    function _assertVaultCall(uint256 index, uint256 value, bytes memory data) internal view {
        MockSettlementVault2.Call memory call = vault2.callAt(index);
        assertEq(call.sender, address(settlement));
        assertEq(call.value, value);
        assertEq(call.data, data);
    }
}
