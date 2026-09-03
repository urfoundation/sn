// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";
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

    function loadTestnetConfig() external view returns (Config memory) {
        return _loadConfig(false);
    }

    function minimumClaimTTLBlocks(uint64 epochBlocks, uint64 claimTTLEpochs) external pure returns (uint64) {
        return _minimumClaimTTLBlocks(epochBlocks, claimTTLEpochs);
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

    function test_deploymentConfigBoundsAllNarrowValues() external {
        _setValidTestnetEnvironment();
        Deploy.Config memory cfg = deploy.loadTestnetConfig();
        assertEq(cfg.netuid, 521);
        assertEq(cfg.policy.epochBlocks, 300);
        assertEq(cfg.policy.claimTTLEpochs, 8);
        assertEq(deploy.minimumClaimTTLBlocks(cfg.policy.epochBlocks, cfg.policy.claimTTLEpochs), 2_400);

        uint256 overflowingNetuid = uint256(type(uint16).max) + 1;
        vm.setEnv("ST_NETUID", vm.toString(overflowingNetuid));
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 16, overflowingNetuid)
        );
        deploy.loadTestnetConfig();
        vm.setEnv("ST_NETUID", "521");

        string[10] memory names = [
            string("ST_REGISTRATION_BURN_LIMIT_RAO"),
            "ST_MINIMUM_TRANSFER_TAO_RAO",
            "ST_EPOCH_BLOCKS",
            "ST_ROOT_COMMIT_WINDOW_BLOCKS",
            "ST_FINALIZE_OFFSET_BLOCKS",
            "ST_CLOSE_GRACE_BLOCKS",
            "ST_CLAIM_TTL_EPOCHS",
            "ST_CLAIM_GRACE_EPOCHS",
            "ST_MAX_BINDING_VALIDITY_EPOCHS",
            "ST_COMMITMENT_MAX_AGE_BLOCKS"
        ];
        uint256 overflowingUint64 = uint256(type(uint64).max) + 1;
        for (uint256 i = 0; i < names.length; i++) {
            vm.setEnv(names[i], vm.toString(overflowingUint64));
            vm.expectRevert(
                abi.encodeWithSelector(
                    SafeCast.SafeCastOverflowedUintDowncast.selector, 64, overflowingUint64
                )
            );
            deploy.loadTestnetConfig();
            vm.setEnv(names[i], "1");
        }

        uint256 overflowingTTL = uint256(type(uint64).max) * 2;
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, overflowingTTL)
        );
        deploy.minimumClaimTTLBlocks(type(uint64).max, 2);
    }

    function _setValidTestnetEnvironment() private {
        vm.setEnv("ST_NETUID", "521");
        vm.setEnv("ST_DEPLOYER", "0x0000000000000000000000000000000000001001");
        vm.setEnv("ST_OWNER", "0x0000000000000000000000000000000000001002");
        vm.setEnv("ST_GUARDIAN", "0x0000000000000000000000000000000000001003");
        vm.setEnv("ST_COMMITMENT_ORACLE", "0x0000000000000000000000000000000000001004");
        vm.setEnv("ST_RESERVE_HOTKEY", "0x1111111111111111111111111111111111111111111111111111111111111111");
        vm.setEnv("ST_ESCROW_HOTKEY", "0x2222222222222222222222222222222222222222222222222222222222222222");
        vm.setEnv("ST_POLICY_HASH", "0x3333333333333333333333333333333333333333333333333333333333333333");
        vm.setEnv("ST_REGISTRATION_BURN_LIMIT_RAO", "1000000");
        vm.setEnv("ST_MINIMUM_TRANSFER_TAO_RAO", "100000");
        vm.setEnv("ST_EPOCH_BLOCKS", "300");
        vm.setEnv("ST_ROOT_COMMIT_WINDOW_BLOCKS", "50");
        vm.setEnv("ST_FINALIZE_OFFSET_BLOCKS", "150");
        vm.setEnv("ST_CLOSE_GRACE_BLOCKS", "5");
        vm.setEnv("ST_CLAIM_TTL_EPOCHS", "8");
        vm.setEnv("ST_CLAIM_GRACE_EPOCHS", "1");
        vm.setEnv("ST_MAX_BINDING_VALIDITY_EPOCHS", "32");
        vm.setEnv("ST_COMMITMENT_MAX_AGE_BLOCKS", "600");
        vm.setEnv("ST_EPOCH_DEPOSIT_CAP_RAO", "1000000000");
        vm.setEnv("ST_CAMPAIGN_DEPOSIT_CAP_RAO", "10000000000");
    }

    function _owners(address a, address b, address c) internal pure returns (address[] memory value) {
        value = new address[](3);
        value[0] = a;
        value[1] = b;
        value[2] = c;
    }
}
