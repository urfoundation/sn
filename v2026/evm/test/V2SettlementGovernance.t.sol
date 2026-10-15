// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {IERC1967} from "@openzeppelin/contracts/interfaces/IERC1967.sol";
import {ERC1967Utils} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Utils.sol";

import {V2SettlementBase} from "./utils/V2SettlementBase.sol";
import {STSettlement2} from "../src/STSettlement2.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";

/// @dev STSettlement2 initialization, the auditor set, upgrades and size
///      (VERSION2-FINAL.md §7.2, §15.4: S-1, S-2, S-9, S-SIZE, S-EV).
contract V2SettlementGovernanceTest is V2SettlementBase {
    function _nextAddress() internal view returns (address) {
        return vm.computeCreateAddress(address(this), vm.getNonce(address(this)));
    }

    // S-1
    function test_initializeStoresTheConfiguration() public view {
        assertEq(settlement.owner(), owner);
        assertEq(settlement.v1Coordinator(), address(coordinator));
        assertEq(settlement.vault(), address(vault2));
        assertEq(settlement.auditors(), _list(auditor1));
        assertEq(settlement.quorum(), 1);
        assertEq(settlement.auditorSetVersion(), 1);
    }

    // S-1, S-EV: the proxy installs the implementation, then initialize sets
    // the owner and auditor set version 1.
    function test_initializeEmitsUpgradeOwnerAuditorsAndVersion() public {
        address[] memory list = _list(auditor1, auditor2);
        address proxy = _nextAddress();
        vm.expectEmit(true, true, true, true, proxy);
        emit IERC1967.Upgraded(address(settlementImplementation));
        vm.expectEmit(true, true, true, true, proxy);
        emit OwnableUpgradeable.OwnershipTransferred(address(0), owner);
        vm.expectEmit(true, true, true, true, proxy);
        emit ISTSettlement2.AuditorsSet(1, list, 2);
        vm.expectEmit(true, true, true, true, proxy);
        emit Initializable.Initialized(1);
        STSettlement2 deployed = _deploy(owner, address(coordinator), address(vault2), list, 2);
        assertEq(address(deployed), proxy);
        assertEq(deployed.auditors(), list);
        assertEq(deployed.quorum(), 2);
        assertEq(deployed.auditorSetVersion(), 1);
    }

    // S-1
    function test_initializeRunsOnce() public {
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        settlement.initialize(owner, address(coordinator), address(vault2), _list(auditor2), 1);
    }

    // S-1, S-EV
    function test_implementationInitializersAreDisabled() public {
        vm.expectEmit(true, true, true, true, _nextAddress());
        emit Initializable.Initialized(type(uint64).max);
        STSettlement2 implementation_ = new STSettlement2();

        vm.expectRevert(Initializable.InvalidInitialization.selector);
        implementation_.initialize(owner, address(coordinator), address(vault2), _list(auditor1), 1);
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        settlementImplementation.initialize(owner, address(coordinator), address(vault2), _list(auditor1), 1);
    }

    // S-1
    function test_initializeRefusesAZeroOwner() public {
        vm.expectRevert(ISTSettlement2.InvalidConfiguration.selector);
        _deploy(address(0), address(coordinator), address(vault2), _list(auditor1), 1);
    }

    // S-1
    function test_initializeRefusesAZeroV1Coordinator() public {
        vm.expectRevert(ISTSettlement2.InvalidConfiguration.selector);
        _deploy(owner, address(0), address(vault2), _list(auditor1), 1);
    }

    // S-1
    function test_initializeRefusesAV1CoordinatorWithoutCode() public {
        vm.expectRevert(ISTSettlement2.InvalidConfiguration.selector);
        _deploy(owner, stranger, address(vault2), _list(auditor1), 1);
    }

    // S-1
    function test_initializeRefusesAZeroVault() public {
        vm.expectRevert(ISTSettlement2.InvalidConfiguration.selector);
        _deploy(owner, address(coordinator), address(0), _list(auditor1), 1);
    }

    // S-1
    function test_initializeRefusesAVaultWithoutCode() public {
        vm.expectRevert(ISTSettlement2.InvalidConfiguration.selector);
        _deploy(owner, address(coordinator), stranger, _list(auditor1), 1);
    }

    // S-1: the initial auditor set is checked as setAuditors checks it.
    function test_initializeRefusesAnInvalidAuditorSet() public {
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        _deploy(owner, address(coordinator), address(vault2), _list(auditor1), 2);
    }

    // S-2
    function test_setAuditorsIsOwnerOnly() public {
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        settlement.setAuditors(_list(auditor2), 1);

        vm.prank(auditor1);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, auditor1)
        );
        settlement.setAuditors(_list(auditor2), 1);
    }

    // S-2, S-EV
    function test_setAuditorsReplacesTheSetAndBumpsTheVersion() public {
        address[] memory list = _list(auditor2, auditor3);
        vm.expectEmit(true, true, true, true, address(settlement));
        emit ISTSettlement2.AuditorsSet(2, list, 2);
        _setAuditors(list, 2);
        assertEq(settlement.auditors(), list);
        assertEq(settlement.quorum(), 2);
        assertEq(settlement.auditorSetVersion(), 2);

        vm.expectEmit(true, true, true, true, address(settlement));
        emit ISTSettlement2.AuditorsSet(3, list, 1);
        _setAuditors(list, 1);
        assertEq(settlement.quorum(), 1);
        assertEq(settlement.auditorSetVersion(), 3);
    }

    // S-2
    function test_setAuditorsRefusesAnEmptySet() public {
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        settlement.setAuditors(new address[](0), 1);
    }

    // S-2
    function test_setAuditorsAcceptsSixteenAuditors() public {
        address[] memory list = _numbered(16);
        _setAuditors(list, 16);
        assertEq(settlement.auditors(), list);
        assertEq(settlement.quorum(), 16);
    }

    // S-2
    function test_setAuditorsRefusesSeventeenAuditors() public {
        address[] memory list = _numbered(17);
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        settlement.setAuditors(list, 1);
    }

    // S-2
    function test_setAuditorsRefusesAZeroAddress() public {
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        settlement.setAuditors(_list(auditor1, address(0)), 1);
    }

    // S-2: adjacent and separated duplicates.
    function test_setAuditorsRefusesADuplicate() public {
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        settlement.setAuditors(_list(auditor1, auditor1), 1);

        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        settlement.setAuditors(_list(auditor1, auditor2, auditor1), 1);
    }

    // S-2
    function test_setAuditorsRefusesAZeroQuorum() public {
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        settlement.setAuditors(_list(auditor1, auditor2), 0);
    }

    // S-2
    function test_setAuditorsRefusesAQuorumAboveTheSet() public {
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidAuditors.selector);
        settlement.setAuditors(_list(auditor1, auditor2), 3);
    }

    // S-9
    function test_upgradeIsOwnerOnly() public {
        STSettlement2 next = new STSettlement2();
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        settlement.upgradeToAndCall(address(next), "");
    }

    // S-9, S-EV: an owner upgrade keeps every stored value, including approvals
    // of the pending root. The storage layout itself is pinned by
    // evm/layout/STSettlement2.json, which scripts/test-solidity-static.sh
    // compares with `forge inspect STSettlement2 storageLayout`.
    function test_ownerUpgradeKeepsEveryStoredValue() public {
        _registerPool1();
        _setAuditors(_list(auditor1, auditor2), 2);
        uint64 seq = _commit(FIRST_EPOCH, ROOT);
        _approve(auditor1, seq, ROOT);

        STSettlement2 next = new STSettlement2();
        vm.expectEmit(true, true, true, true, address(settlement));
        emit IERC1967.Upgraded(address(next));
        vm.prank(owner);
        settlement.upgradeToAndCall(address(next), "");

        bytes32 implementationSlot = vm.load(address(settlement), ERC1967Utils.IMPLEMENTATION_SLOT);
        assertEq(address(uint160(uint256(implementationSlot))), address(next));
        assertEq(settlement.owner(), owner);
        assertEq(settlement.v1Coordinator(), address(coordinator));
        assertEq(settlement.vault(), address(vault2));
        assertEq(settlement.auditors(), _list(auditor1, auditor2));
        assertEq(settlement.quorum(), 2);
        assertEq(settlement.auditorSetVersion(), 2);
        assertEq(settlement.approvalCount(NO1), 1);
        assertTrue(settlement.hasApproved(NO1, auditor1));

        _approve(auditor2, seq, ROOT);
        assertEq(settlement.finalizeDistribution(NO1), 0);
    }

    // S-SIZE: within EIP-170 and EIP-3860. The release build's sizes and margins
    // are recorded from `forge build --sizes`.
    function test_implementationFitsTheCodeSizeLimits() public view {
        assertLt(address(settlementImplementation).code.length, 24_576);
        assertLt(type(STSettlement2).creationCode.length, 49_152);
    }

    function _numbered(uint256 count) internal pure returns (address[] memory list) {
        list = new address[](count);
        for (uint256 i; i < count; i++) {
            list[i] = vm.addr(0xA0D17 + i);
        }
    }
}
