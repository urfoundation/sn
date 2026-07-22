// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Script} from "forge-std/Script.sol";
import {console2} from "forge-std/console2.sol";

import {STSubnetProbe} from "../src/probe/STSubnetProbe.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";

/// @title SP1Conformance — deploy the SP-1 probe + print the on-node battery.
///
/// @notice The SP-1 gate of `docs/LAUNCH.md` Phase B: verify every subtensor
///         precompile assumption STSubnet depends on, against the LIVE mainnet
///         runtime, BEFORE the subnet exists. (PLAN.md §3 SP-1 — the top
///         pre-genesis artifact.)
///
/// @dev Two truths shape this script:
///      1. Only **blake2f (0x09)** is a standard EVM precompile forge can
///         simulate locally. The subtensor precompiles (0x402 ed25519, 0x802
///         metagraph, 0x805 staking) are runtime-only — forge's local EVM
///         reverts on them, so this script does NOT call them in-body. It runs
///         the local blake2f known-answer sanity (the mirror the whole custody
///         model rests on) and then prints the exact `cast` commands that run
///         the real battery ON the node against the deployed probe.
///      2. The custody assumption is contract-scoped, so the battery must run
///         from the deployed `STSubnetProbe`, not from an EOA.
///
/// Usage:
///   # 1. local blake2f sanity + the playbook (no chain writes, no key needed):
///   SP1_NETUID=<existing_netuid> forge script script/SP1Conformance.s.sol --rpc-url mainnet
///
///   # 2. deploy the throwaway probe (broadcast):
///   SP1_NETUID=<existing_netuid> forge script script/SP1Conformance.s.sol \
///       --sig "deploy()" --rpc-url mainnet --broadcast --private-key $DEPLOYER_KEY
///
///   # 3. run the read battery ON THE NODE (free; substitute the probe addr +
///   #    any live hotkey on the netuid, e.g. uid0's from the metagraph):
///   cast call <probe> "readBattery(bytes32)((bool,bytes32,bool,bytes32,bool,bool,bool,bool,uint16,bytes32,bytes32,bool,uint256))" \
///       <sampleHotkey> --rpc-url mainnet
///
///   Value-bearing checks (dust α; see docs/LAUNCH.md B1 for the full sequence
///   incl. the dividend two-step): `cast send <probe> "seedFromTao(bytes32,uint256)" ...`,
///   `"moveRoundTrip(bytes32,bytes32,uint256)"`, `"snapshot(bytes32)"`,
///   `"dividendDelta(bytes32)"`, `"transferOut(bytes32,bytes32,uint256)"`.
contract SP1Conformance is Script {
    function run() external view {
        console2.log("=== SP-1 conformance (local blake2f sanity + on-node playbook) ===");

        // --- 0x09 blake2f: the H160 -> ss58 mirror, faithfully simulable ---
        bytes32 got = Blake2b.mirror(0x1111111111111111111111111111111111111111);
        bytes32 want = 0x32f955c958e51189a4921aed41ef00818f7368dfaec8d9969f091006f8066228;
        console2.log("blake2f (0x09) mirror KAT:", got == want ? "PASS (lib)" : "FAIL");
        console2.logBytes32(got);
        console2.log(
            "  ^ local forge blake2f. Confirm the NODE's 0x09 with: cast call <probe> \"mirrorExt(address)(bytes32)\" 0x1111111111111111111111111111111111111111 --rpc-url mainnet"
        );

        // --- ed25519 (0x402) KAT constants, for a direct node check ---
        console2.log("ed25519 (0x402) KAT (verify TRUE on the node):");
        console2.log(
            "  cast call 0x0000000000000000000000000000000000000402 \"verify(bytes32,bytes32,bytes32,bytes32)(bool)\" \\"
        );
        console2.log(
            "    0xca6dd518081710a6081369b7d2eb0cf32396bf77c9f091be21e6d4c8ed37a6cb 0x3f0d9ad990f7706d891de2dd0a52cc68a6cc631683a31977bb38b9f189d26de1 \\"
        );
        console2.log(
            "    0x2e530da93345ff099a7c46cb9aab8d964a7a016852b567e074f64f9cf1d5cf30 0x35a13c64140c12e523a8e5fec6541fa846be95974aa399f81fc907d020955f0e --rpc-url mainnet"
        );

        console2.log("");
        console2.log("Next: deploy the probe (--sig deploy() --broadcast), then run readBattery on-node.");
        console2.log("Full battery + value-bearing sequence: docs/LAUNCH.md Phase B.");
    }

    function deploy() external returns (address probe) {
        uint16 netuid = uint16(vm.envUint("SP1_NETUID"));
        vm.startBroadcast();
        STSubnetProbe p = new STSubnetProbe(netuid);
        vm.stopBroadcast();
        probe = address(p);
        console2.log("STSubnetProbe deployed:", probe);
        console2.log("  netuid:", netuid);
        console2.log("  probe coldkey mirror(this):");
        console2.logBytes32(Blake2b.mirror(probe));
        console2.log("Fund that ss58 mirror with dust TAO to run the value-bearing checks.");
        console2.log("Read battery (free, on-node):");
        console2.log(
            "  cast call", probe, "\"readBattery(bytes32)((bool,bytes32,bool,bytes32,bool,bool,bool,bool,uint16,bytes32,bytes32,bool,uint256))\" <sampleHotkey> --rpc-url mainnet"
        );
    }
}
