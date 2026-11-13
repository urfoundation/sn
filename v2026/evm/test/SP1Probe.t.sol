// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {STSubnetProbe} from "../src/probe/STSubnetProbe.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";
import {ISTAKING_ADDRESS} from "../src/interfaces/stakingV2.sol";
import {IMetagraph_ADDRESS} from "../src/interfaces/metagraph.sol";
import {IED25519VERIFY_ADDRESS} from "../src/interfaces/ed25519Verify.sol";
import {ISR25519VERIFY_ADDRESS} from "../src/interfaces/sr25519Verify.sol";
import {INeuron_ADDRESS} from "../src/interfaces/neuron.sol";
import {MockAddressMapping} from "./mocks/MockAddressMapping.sol";
import {
    MockStakingV2,
    MockMetagraph,
    MockEd25519,
    MockSr25519,
    MockNeuron
} from "./mocks/PrecompileMocks.sol";

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
    bytes32 constant SR_MSG = 0x0de356fd56fc28d72efe5724a81b2462a7f2bb3f041f48128e2d511b0ae05ba7;
    bytes32 constant SR_PK = 0x94ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b47;
    bytes32 constant SR_R = 0xf4edfe605b1a20514ce7cd0323e32eee364d10b706292028f234d3edde2b5527;
    bytes32 constant SR_S = 0xc93b12e32a60f8f531875060d67b9feca33c9a42bf8bb20debef3aab4b4bf087;

    uint16 constant NETUID = 964;
    bytes32 constant SAMPLE_HOTKEY = keccak256("uid0-hotkey");
    bytes32 constant SAMPLE_COLDKEY = keccak256("uid0-coldkey");
    bytes32 constant HOTKEY_A = keccak256("hotkey-a");
    bytes32 constant HOTKEY_B = keccak256("hotkey-b");
    bytes32 constant ABSENT_HOTKEY = keccak256("absent-hotkey");

    MockStakingV2 staking = MockStakingV2(ISTAKING_ADDRESS);
    MockMetagraph metagraph = MockMetagraph(IMetagraph_ADDRESS);
    MockEd25519 ed = MockEd25519(IED25519VERIFY_ADDRESS);
    MockSr25519 sr = MockSr25519(ISR25519VERIFY_ADDRESS);
    MockNeuron neuron = MockNeuron(INeuron_ADDRESS);

    STSubnetProbe probe;
    bytes32 probeColdkey;

    address deployer = makeAddr("sp1-deployer");

    function setUp() public {
        vm.etch(ISTAKING_ADDRESS, address(new MockStakingV2()).code);
        vm.etch(IMetagraph_ADDRESS, address(new MockMetagraph()).code);
        vm.etch(IED25519VERIFY_ADDRESS, address(new MockEd25519()).code);
        vm.etch(ISR25519VERIFY_ADDRESS, address(new MockSr25519()).code);
        vm.etch(INeuron_ADDRESS, address(new MockNeuron()).code);
        vm.etch(address(0x080c), address(new MockAddressMapping()).code);

        vm.deal(deployer, 1 ether);
        vm.prank(deployer);
        probe = new STSubnetProbe(NETUID);
        probeColdkey = Blake2b.mirror(address(probe));

        // the pallet sees the probe CONTRACT's coldkey as mirror(this)
        staking.setColdkey(address(probe), probeColdkey);
        staking.setNominatorMinimum(1_000);

        // a live uid on the netuid + a modelled correct 0x402 (rejects the
        // tampered KAT the probe flips)
        metagraph.setNeuron(0, SAMPLE_HOTKEY, SAMPLE_COLDKEY);
        neuron.setUid(NETUID, SAMPLE_HOTKEY, 0);
        ed.setBad(ED_MSG, ED_PK, ED_R, ED_S ^ bytes32(uint256(1)), true);
        sr.setBad(SR_MSG, SR_PK, SR_R, SR_S ^ bytes32(uint256(1)), true);
    }

    function test_readBattery_allAssumptionsHold() public {
        // give the probe some stake to read at the sample hotkey
        staking.setStake(SAMPLE_HOTKEY, probeColdkey, 42);

        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);

        // 0x080c runtime mapping — the custody-model KAT
        assertTrue(b.blakeOk, "address mapping callable");
        assertEq(b.mirrorKat, MIRROR_KAT, "mirror KAT");
        assertTrue(b.blakeKatMatch, "mirror KAT matches");
        assertEq(b.selfColdkey, probeColdkey, "self coldkey = mirror(probe)");

        // 0x402 ed25519 — good verifies, tampered is rejected
        assertTrue(b.edOk, "ed25519 callable");
        assertTrue(b.edVerifyGood, "KAT sig verifies");
        assertTrue(b.edVerifyBad, "tampered sig rejected");

        // 0x403 sr25519 — the committed cross-language KAT
        assertTrue(b.srOk, "sr25519 callable");
        assertTrue(b.srVerifyGood, "sr25519 KAT verifies");
        assertTrue(b.srVerifyBad, "tampered sr25519 signature rejected");

        // 0x802 metagraph
        assertTrue(b.mgOk, "metagraph callable");
        assertEq(b.uidCount, 1, "uid count");
        assertEq(b.uid0Hotkey, SAMPLE_HOTKEY);
        assertEq(b.uid0Coldkey, SAMPLE_COLDKEY);

        // 0x804 neuron reverse lookup must distinguish live and absent keys
        assertTrue(b.neuronOk, "neuron callable");
        assertTrue(b.sampleExists, "sample hotkey exists");
        assertEq(b.sampleUid, 0);
        assertTrue(b.absentRejected, "absent hotkey rejected");

        // 0x805 staking view — the probe's own (contract-coldkey) stake
        assertTrue(b.stakeViewOk, "getStake callable");
        assertEq(b.sampleSelfStake, 42, "reads the probe's own stake");
        assertEq(b.nominatorMinimum, 1_000, "nominator minimum reported");
    }

    function test_readBattery_rejectsMalformedAbsentUidResponse() public {
        neuron.setUidResponse(NETUID, ABSENT_HOTKEY, false, 9);
        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);
        assertFalse(b.absentRejected, "absent response must carry canonical zero uid");
    }

    /// @dev Runtime 455 routes 0x09 to BN128 addition, which rejects the
    ///      canonical EIP-152 input. Explicit mapping answers keep the mock
    ///      independent of Foundry's different native 0x09 implementation.
    function _runtime455Mapping() internal {
        vm.mockCall(
            address(0x080c),
            abi.encodeWithSignature(
                "addressMapping(address)", address(0x1111111111111111111111111111111111111111)
            ),
            abi.encode(MIRROR_KAT)
        );
        vm.mockCall(
            address(0x080c),
            abi.encodeWithSignature("addressMapping(address)", address(probe)),
            abi.encode(probeColdkey)
        );
        vm.mockCallRevert(address(0x09), bytes(""), bytes(""));
    }

    /// @dev EIP-152 returns two words; the blake2b-256 answer is the first.
    ///      Reject malformed lengths before decoding without narrowing bytes.
    function _referenceBlake2fMatchesKnownAnswer(bytes memory output) internal pure returns (bool) {
        if (output.length != 64) return false;
        (bytes32 digest,) = abi.decode(output, (bytes32, bytes32));
        return digest == MIRROR_KAT;
    }

    /// @dev A digest-sized or incomplete compression response is not valid.
    function test_referenceBlake2fRejectsShortOutput() public pure {
        bytes memory validOutput = abi.encode(MIRROR_KAT, keccak256("synthetic compression remainder"));
        uint256[4] memory lengths = [uint256(0), 31, 32, 63];
        for (uint256 index = 0; index < lengths.length; index++) {
            bytes memory output = new bytes(lengths[index]);
            for (uint256 offset = 0; offset < output.length; offset++) {
                output[offset] = validOutput[offset];
            }
            assertFalse(_referenceBlake2fMatchesKnownAnswer(output), "short reference response");
        }
    }

    /// @dev An exact known-answer prefix cannot hide trailing response bytes.
    function test_referenceBlake2fRejectsLongOutput() public pure {
        bytes memory validOutput = abi.encode(MIRROR_KAT, keccak256("synthetic compression remainder"));
        uint256[2] memory lengths = [uint256(65), 96];
        for (uint256 index = 0; index < lengths.length; index++) {
            bytes memory output = new bytes(lengths[index]);
            for (uint256 offset = 0; offset < validOutput.length; offset++) {
                output[offset] = validOutput[offset];
            }
            assertFalse(_referenceBlake2fMatchesKnownAnswer(output), "long reference response");
        }
    }

    /// @dev Every byte of the 256-bit answer is checked independently.
    function test_referenceBlake2fRejectsChangedKnownAnswer() public pure {
        bytes memory output = abi.encode(MIRROR_KAT, keccak256("synthetic compression remainder"));
        assertTrue(_referenceBlake2fMatchesKnownAnswer(output), "exact reference answer");
        for (uint256 offset = 0; offset < 32; offset++) {
            output[offset] ^= 0x01;
            assertFalse(_referenceBlake2fMatchesKnownAnswer(output), "changed reference answer");
            output[offset] ^= 0x01;
        }
    }

    /// @dev Deterministically reproduces the live old-library failure while
    ///      the corrected battery still proves the exact mapping and stake.
    function test_readBattery_runtime455RejectsLegacyBlake2f() public {
        // Twelve rounds, the blake2b-256 IV, "evm:" plus the known address,
        // 128-byte padded message, t0=24, t1=0 and final=true: 213 bytes.
        bytes memory legacyInput = abi.encodePacked(
            hex"0000000c28c9bdf267e6096a3ba7ca8485ae67bb2bf894fe72f36e3cf1361d5f3af54fa5d182e6ad7f520e511f6c3e2b8c68059b6bbd41fbabd9831f79217e1319cde05b",
            bytes4(0x65766d3a),
            address(0x1111111111111111111111111111111111111111),
            new bytes(104),
            hex"1800000000000000",
            bytes8(0),
            uint8(1)
        );
        assertEq(legacyInput.length, 213, "canonical EIP-152 input");
        (bool referenceOk, bytes memory referenceOutput) = address(0x09).staticcall(legacyInput);
        assertTrue(referenceOk, "the old input is valid EIP-152 in Foundry");
        assertEq(referenceOutput.length, 64);
        assertTrue(_referenceBlake2fMatchesKnownAnswer(referenceOutput), "reference known answer");
        _runtime455Mapping();
        (bool legacyOk,) = address(0x09).staticcall(legacyInput);
        assertFalse(legacyOk, "runtime 455 rejects the old route");
        staking.setStake(SAMPLE_HOTKEY, probeColdkey, 42);

        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);
        assertTrue(b.blakeOk);
        assertEq(b.mirrorKat, MIRROR_KAT);
        assertTrue(b.blakeKatMatch);
        assertEq(b.selfColdkey, probeColdkey);
        assertTrue(b.stakeViewOk);
        assertEq(b.sampleSelfStake, 42);
        assertTrue(b.edVerifyGood && b.edVerifyBad && b.srVerifyGood && b.srVerifyBad);
        assertTrue(b.mgOk && b.neuronOk && b.sampleExists && b.absentRejected);
    }

    /// @dev Every adjacent value-bearing reader must use the same runtime
    ///      custody mapping, including both moves, dividend reads and recovery.
    function test_runtime455MappingPreservesValueCustody() public {
        _runtime455Mapping();
        bytes32 destination = keccak256("runtime455-recovery-coldkey");
        vm.startPrank(deployer);
        probe.seedFromTao{value: 1_000 * 1e9}(HOTKEY_A, 1_000);
        assertEq(probe.selfStake(HOTKEY_A), 1_000);
        probe.moveRoundTrip(HOTKEY_A, HOTKEY_B, 400);
        assertEq(probe.selfStake(HOTKEY_A), 600);
        assertEq(probe.selfStake(HOTKEY_B), 400);
        probe.moveRoundTrip(HOTKEY_B, HOTKEY_A, 400);
        probe.snapshot(HOTKEY_A);
        staking.setStake(HOTKEY_A, probeColdkey, 1_050);
        (uint256 baseline, uint256 current,) = probe.dividendDelta(HOTKEY_A);
        assertEq(baseline, 1_000);
        assertEq(current, 1_050);
        probe.transferOut(destination, HOTKEY_A, 1_050);
        vm.stopPrank();
        assertEq(probe.selfStake(HOTKEY_A), 0);
        assertEq(probe.selfStake(HOTKEY_B), 0);
        assertEq(staking.stakes(HOTKEY_A, destination), 1_050);
    }

    /// @dev A correct KAT cannot hide an independently failed self mapping.
    ///      The former uncaught stake-reader mapping reverted this whole call.
    function test_readBattery_failedSelfMappingRetainsOtherChecks() public {
        vm.mockCallRevert(
            address(0x080c), abi.encodeWithSignature("addressMapping(address)", address(probe)), bytes("")
        );
        staking.setStake(SAMPLE_HOTKEY, bytes32(0), 99);
        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);
        assertTrue(b.blakeOk && b.blakeKatMatch);
        assertEq(b.selfColdkey, bytes32(0));
        assertFalse(b.stakeViewOk);
        assertEq(b.sampleSelfStake, 0, "must not read the zero coldkey");
        assertEq(b.nominatorMinimum, 1_000);
        assertTrue(b.edOk && b.srOk && b.mgOk && b.neuronOk);
    }

    /// @dev A missing runtime mapping is not an Ethereum fallback; the other
    ///      runtime families remain visible and custody remains unproven.
    function test_readBattery_missingMappingRetainsOtherChecks() public {
        vm.etch(address(0x080c), bytes(""));
        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);
        assertFalse(b.blakeOk);
        assertFalse(b.blakeKatMatch);
        assertEq(b.selfColdkey, bytes32(0));
        assertFalse(b.stakeViewOk);
        assertTrue(b.edOk && b.srOk && b.mgOk && b.neuronOk);
        assertEq(b.nominatorMinimum, 1_000);
    }

    /// @dev A successful call returning a zero self key must not measure an
    ///      unrelated custody slot and accidentally mark that check passed.
    function test_readBattery_zeroSelfMappingCannotQueryZeroCustody() public {
        vm.mockCall(
            address(0x080c),
            abi.encodeWithSignature("addressMapping(address)", address(probe)),
            abi.encode(bytes32(0))
        );
        staking.setStake(SAMPLE_HOTKEY, bytes32(0), 99);
        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);
        assertTrue(b.blakeOk && b.blakeKatMatch);
        assertEq(b.selfColdkey, bytes32(0));
        assertFalse(b.stakeViewOk);
        assertEq(b.sampleSelfStake, 0);
        assertTrue(b.edOk && b.srOk && b.mgOk && b.neuronOk);
    }

    /// @dev The preserved KAT must reject a callable but incorrect mapping.
    function test_readBattery_wrongMappingFailsKnownAnswer() public {
        vm.mockCall(
            address(0x080c),
            abi.encodeWithSignature(
                "addressMapping(address)", address(0x1111111111111111111111111111111111111111)
            ),
            abi.encode(bytes32(uint256(1)))
        );
        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);
        assertTrue(b.blakeOk);
        assertEq(b.mirrorKat, bytes32(uint256(1)));
        assertFalse(b.blakeKatMatch);
    }

    /// @dev An ABI bytes32 response has exactly 32 bytes; accepting a prefix
    ///      would mask the wrong precompile or a malformed runtime response.
    function test_mirrorExt_rejectsMalformedMappingResponses() public {
        uint256[4] memory lengths = [uint256(0), 31, 33, 64];
        for (uint256 i; i < lengths.length; i++) {
            vm.mockCall(
                address(0x080c),
                abi.encodeWithSignature("addressMapping(address)", address(probe)),
                new bytes(lengths[i])
            );
            vm.expectRevert("Blake2b: address mapping failed");
            probe.mirrorExt(address(probe));
            vm.clearMockedCalls();
        }
    }

    function test_seedFromTao_custodyIsContractColdkey() public {
        vm.prank(deployer);
        probe.seedFromTao{value: 1_000 * 1e9}(HOTKEY_A, 1_000);
        // the α landed under the CONTRACT's coldkey, not the deployer's
        assertEq(probe.selfStake(HOTKEY_A), 1_000);
        assertEq(staking.stakes(HOTKEY_A, probeColdkey), 1_000);
        assertEq(staking.stakes(HOTKEY_A, Blake2b.mirror(deployer)), 0);
    }

    /// @dev Reproduces the LAN failure at block 8053712: forwarding all of
    ///      msg.value leaves the caller's native account empty before staking.
    ///      Registration already retains funds for the same native debit.
    function test_seedFromTao_retainsFundingForNativeDebit() public {
        uint256 amountRao = 20_000_000;
        uint256 amountWei = amountRao * 1e9;
        uint256 deployerBefore = deployer.balance;
        uint256 precompileBefore = ISTAKING_ADDRESS.balance;
        assertEq(address(probe).balance, 0, "probe starts unfunded");

        vm.expectEmit(true, false, false, true, address(probe));
        emit STSubnetProbe.Seeded(HOTKEY_A, amountRao, amountWei, amountRao);
        vm.prank(deployer);
        probe.seedFromTao{value: amountWei}(HOTKEY_A, amountRao);

        assertEq(probe.selfStake(HOTKEY_A), amountRao, "stake belongs to probe");
        assertEq(deployer.balance, deployerBefore - amountWei, "single funding debit");
        assertEq(address(probe).balance, 0, "native stake consumed supplied funding");
        assertEq(ISTAKING_ADDRESS.balance, precompileBefore, "no funds stranded at precompile");
    }

    /// @dev Owner-supplied prefunding remains usable, and only the requested
    ///      amount is consumed even when the account contains surplus funds.
    function test_seedFromTao_usesPrefundingWithoutForwardingSurplus() public {
        uint256 amountRao = 1_000;
        uint256 surplusWei = 777 * 1e9;
        vm.deal(address(probe), amountRao * 1e9 + surplusWei);
        uint256 deployerBefore = deployer.balance;
        uint256 precompileBefore = ISTAKING_ADDRESS.balance;

        vm.prank(deployer);
        probe.seedFromTao(HOTKEY_A, amountRao);

        assertEq(probe.selfStake(HOTKEY_A), amountRao);
        assertEq(address(probe).balance, surplusWei);
        assertEq(deployer.balance, deployerBefore);
        assertEq(ISTAKING_ADDRESS.balance, precompileBefore);
    }

    /// @dev Supplied value cannot fabricate stake when it is below the native
    ///      amount; a refused call rolls funding and stake back together.
    function test_seedFromTao_rejectsInsufficientNativeFunding() public {
        uint256 amountRao = 1_000;
        uint256 amountWei = amountRao * 1e9;
        uint256 deployerBefore = deployer.balance;
        vm.startPrank(deployer);
        vm.expectRevert("NotEnoughBalanceToStake");
        probe.seedFromTao(HOTKEY_A, amountRao);
        vm.expectRevert("NotEnoughBalanceToStake");
        probe.seedFromTao{value: amountWei - 1}(HOTKEY_A, amountRao);
        vm.stopPrank();
        assertEq(probe.selfStake(HOTKEY_A), 0);
        assertEq(address(probe).balance, 0);
        assertEq(deployer.balance, deployerBefore);
    }

    /// @dev Value attached by an unauthorized caller cannot alter probe
    ///      funding, existing custody, or native-precompile balances.
    function test_seedFromTao_rejectsUnauthorizedFunding() public {
        address intruder = makeAddr("funded-intruder");
        uint256 amountWei = 1_000 * 1e9;
        vm.deal(intruder, amountWei);
        vm.deal(address(probe), amountWei);
        vm.prank(intruder);
        vm.expectRevert("probe: not owner");
        probe.seedFromTao{value: amountWei}(HOTKEY_A, 1_000);
        assertEq(intruder.balance, amountWei);
        assertEq(address(probe).balance, amountWei);
        assertEq(probe.selfStake(HOTKEY_A), 0);
    }

    function test_moveRoundTrip_slippageFreeAndAttributed() public {
        vm.startPrank(deployer);
        probe.seedFromTao{value: 1_000 * 1e9}(HOTKEY_A, 1_000);
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
        probe.seedFromTao{value: 1_000 * 1e9}(HOTKEY_A, 1_000);
        probe.transferOut(dest, HOTKEY_A, 250);
        vm.stopPrank();
        assertEq(staking.stakes(HOTKEY_A, dest), 250);
        assertEq(probe.selfStake(HOTKEY_A), 750);
    }

    function test_dividendTwoStep_detectsCompounding() public {
        vm.startPrank(deployer);
        probe.seedFromTao{value: 1_000 * 1e9}(SAMPLE_HOTKEY, 1_000);
        probe.snapshot(SAMPLE_HOTKEY);
        vm.stopPrank();

        // model a tempo's dividend auto-restaking onto (hotkey, mirror(probe))
        staking.setStake(SAMPLE_HOTKEY, probeColdkey, 1_050);

        (uint256 baseline, uint256 current, uint64 sinceBlock) = probe.dividendDelta(SAMPLE_HOTKEY);
        assertEq(baseline, 1_000);
        assertEq(current, 1_050, "dividends auto-compounded, no action taken");
        assertEq(sinceBlock, uint64(block.number));
    }

    function test_snapshotRejectsBlockNumberDowncastOverflow() public {
        uint256 overflowing = uint256(type(uint64).max) + 1;
        vm.roll(overflowing);
        vm.prank(deployer);
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, overflowing)
        );
        probe.snapshot(SAMPLE_HOTKEY);
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
        STSubnetProbe.Battery memory b = probe.readBattery(SAMPLE_HOTKEY, ABSENT_HOTKEY);
        assertFalse(b.mgOk, "missing metagraph -> false, not a revert");
        // the other precompiles still report
        assertTrue(b.blakeOk);
        assertTrue(b.edOk);
    }
}
