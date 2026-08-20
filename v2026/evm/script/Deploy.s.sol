// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Script} from "forge-std/Script.sol";
import {console2} from "forge-std/console2.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

import {Blake2b} from "../src/lib/Blake2b.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STReserveSink} from "../src/STReserveSink.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";

interface ISafeGovernance {
    function getThreshold() external view returns (uint256);
    function getOwners() external view returns (address[] memory);
}

/// @notice Installs the release-1.0 immutable reserve/vault and UUPS
/// coordinator. Chain IDs other than Bittensor testnet/mainnet are rejected.
contract Deploy is Script {
    uint256 internal constant TESTNET_CHAIN_ID = 945;
    uint256 internal constant MAINNET_CHAIN_ID = 964;

    struct Deployment {
        address reserveSink;
        address settlementVault;
        address coordinatorImplementation;
        address coordinatorProxy;
    }

    struct Config {
        uint16 netuid;
        address deployer;
        address owner;
        address guardian;
        address commitmentOracle;
        bytes32 reserveHotkey;
        bytes32 escrowHotkey;
        uint256 registrationBurnWei;
        STCoordinator.PolicySnapshot policy;
    }

    function run() external returns (Deployment memory deployed) {
        if (block.chainid != TESTNET_CHAIN_ID && block.chainid != MAINNET_CHAIN_ID) {
            revert("Deploy: unsupported chain");
        }
        Config memory cfg = _loadConfig(block.chainid == MAINNET_CHAIN_ID);

        // Deployment addresses are needed before construction because their
        // Substrate mirrors are immutable custody identities.
        uint64 nonce = vm.getNonce(cfg.deployer);
        deployed.reserveSink = vm.computeCreateAddress(cfg.deployer, nonce);
        deployed.settlementVault = vm.computeCreateAddress(cfg.deployer, nonce + 1);
        deployed.coordinatorImplementation = vm.computeCreateAddress(cfg.deployer, nonce + 2);
        // The vault's one-shot escrow registration consumes nonce + 3 before
        // the proxy CREATE, so its address is deterministically nonce + 4.
        deployed.coordinatorProxy = vm.computeCreateAddress(cfg.deployer, nonce + 4);

        vm.startBroadcast();
        STReserveSink reserve = new STReserveSink(
            cfg.netuid, cfg.reserveHotkey, Blake2b.mirror(deployed.reserveSink), cfg.deployer
        );
        STSettlementVault vault = new STSettlementVault(
            cfg.netuid,
            cfg.escrowHotkey,
            Blake2b.mirror(deployed.settlementVault),
            cfg.policy.epochBlocks * cfg.policy.claimTTLEpochs,
            cfg.deployer
        );
        STCoordinator implementation = new STCoordinator();
        vault.registerEscrow{value: cfg.registrationBurnWei}();
        ERC1967Proxy proxy = new ERC1967Proxy(
            address(implementation),
            abi.encodeCall(
                STCoordinator.initialize,
                (
                    cfg.netuid,
                    cfg.owner,
                    cfg.guardian,
                    Blake2b.mirror(deployed.coordinatorProxy),
                    vault,
                    reserve,
                    cfg.commitmentOracle,
                    cfg.policy
                )
            )
        );
        reserve.setRecorderOnce(address(proxy));
        vault.setCoordinatorOnce(address(proxy));
        vm.stopBroadcast();

        require(address(reserve) == deployed.reserveSink, "Deploy: reserve address drift");
        require(address(vault) == deployed.settlementVault, "Deploy: vault address drift");
        require(
            address(implementation) == deployed.coordinatorImplementation,
            "Deploy: implementation address drift"
        );
        require(address(proxy) == deployed.coordinatorProxy, "Deploy: proxy address drift");

        console2.log("STReserveSink:               ", deployed.reserveSink);
        console2.log("STSettlementVault:           ", deployed.settlementVault);
        console2.log("STCoordinator implementation:", deployed.coordinatorImplementation);
        console2.log("STCoordinator proxy:         ", deployed.coordinatorProxy);
    }

    /// Required: ST_NETUID, ST_DEPLOYER, ST_OWNER, ST_GUARDIAN,
    /// ST_COMMITMENT_ORACLE, ST_RESERVE_HOTKEY, ST_ESCROW_HOTKEY,
    /// ST_POLICY_HASH, ST_REGISTRATION_BURN_WEI, ST_EPOCH_DEPOSIT_CAP_RAO and
    /// ST_CAMPAIGN_DEPOSIT_CAP_RAO. Window fields have profile defaults.
    function _loadConfig(bool mainnet) internal view returns (Config memory cfg) {
        cfg.netuid = uint16(vm.envUint("ST_NETUID"));
        cfg.deployer = vm.envAddress("ST_DEPLOYER");
        cfg.owner = vm.envAddress("ST_OWNER");
        cfg.guardian = vm.envAddress("ST_GUARDIAN");
        cfg.commitmentOracle = vm.envAddress("ST_COMMITMENT_ORACLE");
        cfg.reserveHotkey = vm.envBytes32("ST_RESERVE_HOTKEY");
        cfg.escrowHotkey = vm.envBytes32("ST_ESCROW_HOTKEY");
        cfg.registrationBurnWei = vm.envUint("ST_REGISTRATION_BURN_WEI");
        bytes32 policyHash = vm.envBytes32("ST_POLICY_HASH");

        require(cfg.netuid != 0 && cfg.deployer != address(0) && cfg.owner != address(0), "Deploy: identity");
        require(cfg.guardian != address(0) && cfg.commitmentOracle != address(0), "Deploy: roles");
        require(
            cfg.deployer != cfg.owner && cfg.deployer != cfg.guardian && cfg.deployer != cfg.commitmentOracle
                && cfg.owner != cfg.guardian && cfg.owner != cfg.commitmentOracle
                && cfg.guardian != cfg.commitmentOracle,
            "Deploy: roles must be distinct"
        );
        require(cfg.reserveHotkey != bytes32(0) && cfg.escrowHotkey != bytes32(0), "Deploy: hotkeys");
        require(cfg.registrationBurnWei != 0, "Deploy: registration burn");
        require(cfg.reserveHotkey != cfg.escrowHotkey && policyHash != bytes32(0), "Deploy: policy");
        if (mainnet) {
            _requireMainnetSafe(cfg.owner);
        } else {
            require(cfg.owner.code.length == 0, "Deploy: testnet owner must be EOA");
        }

        uint64 epochBlocks = uint64(vm.envOr("ST_EPOCH_BLOCKS", uint256(mainnet ? 50_400 : 300)));
        cfg.policy = STCoordinator.PolicySnapshot({
            policyHash: policyHash,
            effectiveEpoch: 0,
            effectiveBlock: 0,
            epochBlocks: epochBlocks,
            rootCommitWindowBlocks: uint64(
                vm.envOr("ST_ROOT_COMMIT_WINDOW_BLOCKS", uint256(mainnet ? 1_200 : 50))
            ),
            finalizeOffsetBlocks: uint64(
                vm.envOr("ST_FINALIZE_OFFSET_BLOCKS", uint256(mainnet ? 14_400 : 150))
            ),
            closeGraceBlocks: uint64(vm.envOr("ST_CLOSE_GRACE_BLOCKS", uint256(mainnet ? 120 : 5))),
            claimTTLEpochs: uint64(vm.envOr("ST_CLAIM_TTL_EPOCHS", uint256(8))),
            claimGraceEpochs: uint64(vm.envOr("ST_CLAIM_GRACE_EPOCHS", uint256(1))),
            maximumBindingValidityEpochs: uint64(vm.envOr("ST_MAX_BINDING_VALIDITY_EPOCHS", uint256(32))),
            commitmentMaxAgeBlocks: uint64(
                vm.envOr("ST_COMMITMENT_MAX_AGE_BLOCKS", uint256(epochBlocks * 2))
            ),
            epochDepositCapRao: vm.envUint("ST_EPOCH_DEPOSIT_CAP_RAO"),
            campaignDepositCapRao: vm.envUint("ST_CAMPAIGN_DEPOSIT_CAP_RAO")
        });
    }

    /// @dev A generic contract address is not a mainnet governance policy.
    /// Require the standard Safe read interface and the exact release-1.0
    /// threshold/owner shape before any broadcast can start.
    function _requireMainnetSafe(address owner_) internal view {
        require(owner_.code.length != 0, "Deploy: mainnet owner must be Safe");
        (bool thresholdOK, bytes memory thresholdData) =
            owner_.staticcall(abi.encodeCall(ISafeGovernance.getThreshold, ()));
        require(thresholdOK && thresholdData.length >= 32, "Deploy: Safe threshold unavailable");
        uint256 threshold = abi.decode(thresholdData, (uint256));

        (bool ownersOK, bytes memory ownersData) =
            owner_.staticcall(abi.encodeCall(ISafeGovernance.getOwners, ()));
        require(ownersOK && ownersData.length >= 64, "Deploy: Safe owners unavailable");
        address[] memory owners = abi.decode(ownersData, (address[]));
        require(threshold == 2 && owners.length == 3, "Deploy: mainnet owner must be 2-of-3 Safe");
        for (uint256 i = 0; i < owners.length; i++) {
            require(owners[i] != address(0), "Deploy: Safe owner zero");
            for (uint256 j = 0; j < i; j++) {
                require(owners[i] != owners[j], "Deploy: Safe owners not distinct");
            }
        }
    }
}
