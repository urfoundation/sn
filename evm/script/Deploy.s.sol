// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Script} from "forge-std/Script.sol";
import {console2} from "forge-std/console2.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";
import {STSubnet} from "../src/STSubnet.sol";

/// @title Deploy — UUPS impl + ERC1967 proxy for STSubnet.
///
/// Profiles (PLAN.md §3.1, live-checked 2026-07-01):
///   testnet: chain id 945, rpc https://test.chain.opentensor.ai
///   mainnet: chain id 964, rpc https://lite.chain.opentensor.ai
/// Any other chain id (e.g. anvil 31337, SP-3 localnet) uses testnet defaults.
///
/// Required env:
///   ST_NETUID            subnet netuid (uint16)
///   ST_OWNER             owner (proxy admin authority; multisig once available, D-12)
///   ST_TREASURY_HOTKEY   bytes32 claims-escrow hotkey
///   ST_RESERVE_HOTKEY    bytes32 buyback-reserve hotkey (§7.4/D23): the
///                        owner-validator hotkey every deposit is staked to,
///                        set ONCE here (no setter). Must differ from the
///                        treasury hotkey. Run its delegate take at 0.
/// Optional env (profile defaults otherwise):
///   ST_GUARDIAN          pause-only guardian (default: 0 = none)
///   ST_T_EPOCH           epoch length in blocks   (mainnet 50_400 ~7d; testnet 300)
///   ST_COMMIT_WINDOW     blocks                   (mainnet 1_200 ~4h;  testnet 50)
///   ST_TRAILS_WINDOW     blocks — RESERVED dial for the bounty phase; gates
///                        nothing in v1              (mainnet 7_200; testnet 100)
///   ST_FINALIZE_OFFSET   blocks                   (mainnet 14_400 ~48h; testnet 150)
///   ST_SELF_COLDKEY      bytes32 mirror(proxy) override; 0 = compute on-chain
///                        via blake2f 0x09 (pass explicitly if 0x09 is missing
///                        on the runtime — SP-1)
///
/// Usage:
///   forge script script/Deploy.s.sol --rpc-url testnet --broadcast \
///     --private-key $DEPLOYER_KEY
contract Deploy is Script {
    uint256 internal constant TESTNET_CHAIN_ID = 945;
    uint256 internal constant MAINNET_CHAIN_ID = 964;

    function run() external returns (address proxy, address implementation) {
        bool mainnet = block.chainid == MAINNET_CHAIN_ID;
        if (!mainnet && block.chainid != TESTNET_CHAIN_ID) {
            console2.log("WARNING: unrecognized chain id (using testnet defaults):", block.chainid);
        }

        uint16 netuid = uint16(vm.envUint("ST_NETUID"));
        address owner_ = vm.envAddress("ST_OWNER");
        bytes32 treasuryHotkey = vm.envBytes32("ST_TREASURY_HOTKEY");
        bytes32 reserveHotkey = vm.envBytes32("ST_RESERVE_HOTKEY");
        address guardian = vm.envOr("ST_GUARDIAN", address(0));

        uint64 tEpoch = uint64(vm.envOr("ST_T_EPOCH", uint256(mainnet ? 50_400 : 300)));
        uint64 commitWindow = uint64(vm.envOr("ST_COMMIT_WINDOW", uint256(mainnet ? 1_200 : 50)));
        uint64 trailsWindow = uint64(vm.envOr("ST_TRAILS_WINDOW", uint256(mainnet ? 7_200 : 100)));
        uint64 finalizeOffset =
            uint64(vm.envOr("ST_FINALIZE_OFFSET", uint256(mainnet ? 14_400 : 150)));
        bytes32 selfColdkey = vm.envOr("ST_SELF_COLDKEY", bytes32(0));

        vm.startBroadcast();
        STSubnet impl = new STSubnet();
        ERC1967Proxy p = new ERC1967Proxy(
            address(impl),
            abi.encodeCall(
                STSubnet.initialize,
                (
                    netuid,
                    owner_,
                    guardian,
                    treasuryHotkey,
                    reserveHotkey,
                    tEpoch,
                    commitWindow,
                    trailsWindow,
                    finalizeOffset,
                    selfColdkey
                )
            )
        );
        vm.stopBroadcast();

        proxy = address(p);
        implementation = address(impl);
        console2.log("STSubnet implementation:", implementation);
        console2.log("STSubnet proxy:         ", proxy);
        console2.log("selfColdkey (mirror):");
        console2.logBytes32(STSubnet(payable(proxy)).selfColdkey());
    }
}
