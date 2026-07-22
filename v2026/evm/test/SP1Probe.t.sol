// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";

import {STSubnetProbe} from "../src/probe/STSubnetProbe.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";
import {ISTAKING_ADDRESS} from "../src/interfaces/stakingV2.sol";
import {IMetagraph_ADDRESS} from "../src/interfaces/metagraph.sol";
import {IED25519VERIFY_ADDRESS} from "../src/interfaces/ed25519Verify.sol";
import {MockStakingV2, MockMetagraph, MockEd25519} from "./mocks/PrecompileMocks.sol";

/// @dev CI proof that the SP-1 probe's battery LOGIC is correct — so the
///      harness is itself verified before it ever touches mainnet (a
///      conformance tool you can't test is just another unverified assumption).
///      Drives STSubnetProbe against the etched precompile mocks and asserts
///      every readBattery field plus the value-bearing checks (custody,
///      slippage-free move, transferOut, the dividend two-step). On mainnet the
///      same calls run against the real runtime via `cast` (docs/LAUNCH.md B1).
contract SP1ProbeTest is Test {
    // KAT constants mirrored from STSubnetProbe (documented in docs/LAUNCH.md)
    bytes32 constant MIRROR_KAT = 0x32f955c958e51189a4921aed41ef00818f7368dfaec8d9969f091006f8066228;
    bytes32 constant ED_MSG = 0xca6dd518081710a6081369b7d2eb0cf32396bf77c9f091be21e6d4c8ed37a6cb;
    bytes32 constant ED_PK = 0x3f0d9ad990f7706d891de2dd0a52cc68a6cc631683a31977bb38b9f189d26de1;
    bytes32 constant ED_R = 0x2e530da93345ff099a7c46cb9aab8d964a7a016852b567e074f64f9cf1d5cf30;
    bytes32 constant ED_S = 0x35a13c64140c12e523a8e5fec6541fa846be95974aa399f81fc907d020955f0e;

    uint16 constant NETUID = 964;
    bytes32 constant SAMPLE_HOTKEY = keccak256("uid0-hotkey");
    bytes32 constant SAMPLE_COLDKEY = keccak256("uid0-coldkey");
    bytes32 constant HOTKEY_A = keccak256("hotkey-a");
    bytes32 constant HOTKEY_B = keccak256("hotkey-b");

    MockStakingV2 staking = MockStakingV2(ISTAKING_ADDRESS);
    MockMetagraph metagraph = MockMetagraph(IMetagraph_ADDRESS);
    MockEd25519 ed = MockEd25519(IED25519VERIFY_ADDRESS);

    STSubnetProbe probe;
    bytes32 probeColdkey;

    address deployer = makeAddr("sp1-deployer");

    function setUp() public {
        vm.etch(ISTAKING_ADDRESS, address(new MockStakingV2()).code);
        vm.etch(IMetagraph_ADDRESS, address(new MockMetagraph()).code);
        vm.etch(IED25519VERIFY_ADDRESS, address(new MockEd25519()).code);

        vm.prank(deployer);
        probe = new STSubnetProbe(NETUID);
        probeColdkey = Blake2b.mirror(address(probe));

        // the pallet sees the probe CONTRACT's coldkey as mirror(this)
        staking.setColdkey(address(probe), probeColdkey);

        // a live uid on the netuid + a modelled correct 0x402 (rejects the
        // tampered KAT the probe flips)
        metagraph.setNeuron(0, SAMPLE_HOTKEY, SAMPLE_COLDKEY);
        ed.setBad(ED_MSG, ED_PK, ED_R, ED_S ^ bytes32(uint256(1)), true);
    }

    function test_readBattery_allAssumptionsHold() public {
        // give the probe some stake to read at the sample hotkey
        staking.setStake(SAMPLE_HOTKEY, probeColdkey, 42);

        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY);

        // 0x09 blake2f — the custody-model KAT
        assertTrue(b.blakeOk, "blake2f callable");
        assertEq(b.mirrorKat, MIRROR_KAT, "mirror KAT");
        assertTrue(b.blakeKatMatch, "mirror KAT matches");
        assertEq(b.selfColdkey, probeColdkey, "self coldkey = mirror(probe)");

        // 0x402 ed25519 — good verifies, tampered is rejected
        assertTrue(b.edOk, "ed25519 callable");
        assertTrue(b.edVerifyGood, "KAT sig verifies");
        assertTrue(b.edVerifyBad, "tampered sig rejected");

        // 0x802 metagraph
        assertTrue(b.mgOk, "metagraph callable");
        assertEq(b.uidCount, 1, "uid count");
        assertEq(b.uid0Hotkey, SAMPLE_HOTKEY);
        assertEq(b.uid0Coldkey, SAMPLE_COLDKEY);

        // 0x805 staking view — the probe's own (contract-coldkey) stake
        assertTrue(b.stakeViewOk, "getStake callable");
        assertEq(b.sampleSelfStake, 42, "reads the probe's own stake");
    }

    function test_seedFromTao_custodyIsContractColdkey() public {
        vm.prank(deployer);
        probe.seedFromTao(HOTKEY_A, 1_000);
        // the α landed under the CONTRACT's coldkey, not the deployer's
        assertEq(probe.selfStake(HOTKEY_A), 1_000);
        assertEq(staking.stakes(HOTKEY_A, probeColdkey), 1_000);
        assertEq(staking.stakes(HOTKEY_A, Blake2b.mirror(deployer)), 0);
    }

    function test_moveRoundTrip_slippageFreeAndAttributed() public {
        vm.startPrank(deployer);
        probe.seedFromTao(HOTKEY_A, 1_000);
        (uint256 fromBefore, uint256 toBefore, uint256 fromAfter, uint256 toAfter) =
            probe.moveRoundTrip(HOTKEY_A, HOTKEY_B, 400);
        vm.stopPrank();

        assertEq(fromBefore, 1_000);
        assertEq(toBefore, 0);
        // slippage-free within-netuid: out-delta == in-delta == amount
        assertEq(fromBefore - fromAfter, 400, "moved out exactly");
        assertEq(toAfter - toBefore, 400, "moved in exactly");
        assertEq(fromAfter, 600);
        assertEq(toAfter, 400);
    }

    function test_transferOut_recoversDustFromContract() public {
        bytes32 dest = keccak256("recover-coldkey");
        vm.startPrank(deployer);
        probe.seedFromTao(HOTKEY_A, 1_000);
        probe.transferOut(dest, HOTKEY_A, 250);
        vm.stopPrank();
        assertEq(staking.stakes(HOTKEY_A, dest), 250);
        assertEq(probe.selfStake(HOTKEY_A), 750);
    }

    function test_dividendTwoStep_detectsCompounding() public {
        vm.startPrank(deployer);
        probe.seedFromTao(SAMPLE_HOTKEY, 1_000);
        probe.snapshot(SAMPLE_HOTKEY);
        vm.stopPrank();

        // model a tempo's dividend auto-restaking onto (hotkey, mirror(probe))
        staking.setStake(SAMPLE_HOTKEY, probeColdkey, 1_050);

        (uint256 baseline, uint256 current, uint64 sinceBlock) = probe.dividendDelta(SAMPLE_HOTKEY);
        assertEq(baseline, 1_000);
        assertEq(current, 1_050, "dividends auto-compounded, no action taken");
        assertEq(sinceBlock, uint64(block.number));
    }

    function test_valueBearingChecks_areOwnerGated() public {
        vm.startPrank(makeAddr("intruder"));
        vm.expectRevert("probe: not owner");
        probe.seedFromTao(HOTKEY_A, 1);
        vm.expectRevert("probe: not owner");
        probe.moveRoundTrip(HOTKEY_A, HOTKEY_B, 1);
        vm.expectRevert("probe: not owner");
        probe.transferOut(bytes32(0), HOTKEY_A, 1);
        vm.expectRevert("probe: not owner");
        probe.snapshot(HOTKEY_A);
        vm.stopPrank();
    }

    /// @dev A precompile that reverts (missing on the runtime) must surface as
    ///      a clean `false` in the matrix, never a whole-battery revert — the
    ///      point of the per-check try/catch.
    function test_readBattery_missingPrecompile_failsClosedNotReverts() public {
        // wipe the metagraph precompile: calls now revert
        vm.etch(IMetagraph_ADDRESS, hex"fe"); // INVALID opcode
        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY);
        assertFalse(b.mgOk, "missing metagraph -> false, not a revert");
        // the other precompiles still report
        assertTrue(b.blakeOk);
        assertTrue(b.edOk);
    }
}
