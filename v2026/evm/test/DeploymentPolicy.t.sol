// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {Deploy} from "../script/Deploy.s.sol";

contract SafeShapeMock {
    uint256 internal immutable threshold;
    address[] internal owners;

    constructor(uint256 threshold_, address[] memory owners_) {
        threshold = threshold_;
        owners = owners_;
    }

    function getThreshold() external view returns (uint256) {
        return threshold;
    }

    function getOwners() external view returns (address[] memory) {
        return owners;
    }
}

contract MissingSafeInterface {}

contract DeployHarness is Deploy {
    function requireMainnetSafe(address owner_) external view {
        _requireMainnetSafe(owner_);
    }
}

contract DeploymentPolicyTest is Test {
    DeployHarness internal deploy;

    function setUp() external {
        deploy = new DeployHarness();
    }

    function test_mainnetRequiresExactTwoOfThreeSafeShape() external {
        address[] memory owners = _owners(address(0xA1), address(0xA2), address(0xA3));
        deploy.requireMainnetSafe(address(new SafeShapeMock(2, owners)));
    }

    function test_mainnetRejectsEOAAndMissingSafeInterface() external {
        MissingSafeInterface missing = new MissingSafeInterface();
        vm.expectRevert("Deploy: mainnet owner must be Safe");
        deploy.requireMainnetSafe(address(0xA1));

        vm.expectRevert("Deploy: Safe threshold unavailable");
        deploy.requireMainnetSafe(address(missing));
    }

    function test_mainnetRejectsWrongThresholdOrOwnerCount() external {
        SafeShapeMock wrongThreshold = new SafeShapeMock(1, _owners(address(1), address(2), address(3)));
        vm.expectRevert("Deploy: mainnet owner must be 2-of-3 Safe");
        deploy.requireMainnetSafe(address(wrongThreshold));

        address[] memory twoOwners = new address[](2);
        twoOwners[0] = address(1);
        twoOwners[1] = address(2);
        SafeShapeMock wrongOwnerCount = new SafeShapeMock(2, twoOwners);
        vm.expectRevert("Deploy: mainnet owner must be 2-of-3 Safe");
        deploy.requireMainnetSafe(address(wrongOwnerCount));
    }

    function test_mainnetRejectsZeroOrDuplicateOwner() external {
        SafeShapeMock zeroOwner = new SafeShapeMock(2, _owners(address(1), address(0), address(3)));
        vm.expectRevert("Deploy: Safe owner zero");
        deploy.requireMainnetSafe(address(zeroOwner));

        SafeShapeMock duplicateOwner = new SafeShapeMock(2, _owners(address(1), address(1), address(3)));
        vm.expectRevert("Deploy: Safe owners not distinct");
        deploy.requireMainnetSafe(address(duplicateOwner));
    }

    function _owners(address a, address b, address c) internal pure returns (address[] memory value) {
        value = new address[](3);
        value[0] = a;
        value[1] = b;
        value[2] = c;
    }
}
