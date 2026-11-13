// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

// Fixed-width vectors, ABI admission and exact mocked precompile tuples only.
// Real cryptography is tested in Go and separately on the pinned public runtime.
// Neither these mocks nor possession establish historical role or inclusion.

import {Test} from "forge-std/Test.sol";
import {ValidatorEvidence} from "../src/lib/ValidatorEvidence.sol";
import {ValidatorEvidenceActivation} from "../src/lib/ValidatorEvidenceActivation.sol";
import {IEd25519Verify, IED25519VERIFY_ADDRESS} from "../src/interfaces/ed25519Verify.sol";
import {ISR25519Verify, ISR25519VERIFY_ADDRESS} from "../src/interfaces/sr25519Verify.sol";

/// @dev Exposes the stateless library, not an activation or commitment contract.
contract ValidatorEvidenceActivationHarness {
    function payload(ValidatorEvidenceActivation.Record memory record) external pure returns (bytes memory) {
        return ValidatorEvidenceActivation.payload(record);
    }

    function digest(ValidatorEvidenceActivation.Record memory record) external pure returns (bytes32) {
        return ValidatorEvidenceActivation.digest(record);
    }

    function valid(ValidatorEvidenceActivation.Record memory record) external pure returns (bool) {
        return ValidatorEvidenceActivation.valid(record);
    }

    function validAt(
        ValidatorEvidenceActivation.Record memory record,
        ValidatorEvidenceActivation.Record memory expected
    ) external pure returns (bool) {
        return ValidatorEvidenceActivation.validAt(record, expected);
    }

    function evidenceDomain(ValidatorEvidenceActivation.Record memory record)
        external
        pure
        returns (ValidatorEvidence.Domain memory)
    {
        return ValidatorEvidenceActivation.evidenceDomain(record);
    }

    /// @dev The pure caller is a compile-time guard for the stateless verifier.
    function verify(
        ValidatorEvidenceActivation.Record memory record,
        ValidatorEvidenceActivation.Record memory expected,
        bytes memory vpkSignature,
        bytes memory hotkeySignature
    ) external pure returns (bool) {
        return ValidatorEvidenceActivation.verify(record, expected, vpkSignature, hotkeySignature);
    }
}

/// @dev Any unconfigured tuple or premature signature check fails the test.
contract ValidatorEvidenceActivationUnexpectedPrecompile {
    fallback() external {
        revert("unexpected activation precompile call");
    }
}

contract ValidatorEvidenceActivationTest is Test {
    bytes32 internal constant HOTKEY = hex"94ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b47";
    bytes32 internal constant VPK = hex"03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8";

    ValidatorEvidenceActivationHarness internal harness;

    function setUp() public {
        harness = new ValidatorEvidenceActivationHarness();
        address trap = address(new ValidatorEvidenceActivationUnexpectedPrecompile());
        vm.etch(IED25519VERIFY_ADDRESS, trap.code);
        vm.etch(ISR25519VERIFY_ADDRESS, trap.code);
    }

    /// @dev Literal fields match Go's independently encoded synthetic fixture.
    function _record() internal pure returns (ValidatorEvidenceActivation.Record memory record) {
        record.domain = ValidatorEvidenceActivation.Domain({
            chainId: 945,
            genesisHash: hex"1313131313131313131313131313131313131313131313131313131313131313",
            netuid: 17,
            coordinator: 0x1111111111111111111111111111111111111111,
            settlementVault: 0x1212121212121212121212121212121212121212,
            deploymentIdHash: hex"1414141414141414141414141414141414141414141414141414141414141414",
            policyHash: hex"1515151515151515151515151515151515151515151515151515151515151515",
            epoch: 42
        });
        record.hotkey = HOTKEY;
        record.noId = 7;
        record.vpk = VPK;
        record.firstSequence = 5;
        record.priorRoot = hex"2100000000000000000000000000000000000000000000000000000000000000";
        record.nativeBlock = 500;
        record.nativeHash = hex"2200000000000000000000000000000000000000000000000000000000000000";
        record.evmBlock = 980;
        record.evmHash = hex"2300000000000000000000000000000000000000000000000000000000000000";
    }

    /// @dev The oracle uses literal packed bytes, never either production encoder.
    function _golden() internal pure returns (bytes memory) {
        return hex"75726e6574776f726b2f76616c696461746f722d65766964656e63652d61637469766174696f6e2f76310000000000000003b1131313131313131313131313131313131313131313131313131313131313131300111111111111111111111111111111111111111111121212121212121212121212121212121212121214141414141414141414141414141414141414141414141414141414141414141515151515151515151515151515151515151515151515151515151515151515000000000000002a94ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b47000000000000000703a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b80000000000000005210000000000000000000000000000000000000000000000000000000000000000000000000001f4220000000000000000000000000000000000000000000000000000000000000000000000000003d42300000000000000000000000000000000000000000000000000000000000000";
    }

    /// @dev These distinct words are mocked call arguments, not real signatures.
    function _vpkSignature() internal pure returns (bytes memory) {
        return abi.encode(bytes32(uint256(0x31)), bytes32(uint256(0x32)));
    }

    function _hotkeySignature() internal pure returns (bytes memory) {
        return abi.encode(bytes32(uint256(0x33)), bytes32(uint256(0x34)));
    }

    /// @dev ABI decoding independently checks the library's assembly word reads.
    function _mockChecks(bool vpkResult, bool hotkeyResult) internal {
        (bytes32 vpkR, bytes32 vpkS) = abi.decode(_vpkSignature(), (bytes32, bytes32));
        (bytes32 hotkeyR, bytes32 hotkeyS) = abi.decode(_hotkeySignature(), (bytes32, bytes32));
        bytes32 message = sha256(_golden());
        bytes memory vpkCall = abi.encodeCall(IEd25519Verify.verify, (message, VPK, vpkR, vpkS));
        bytes memory hotkeyCall = abi.encodeCall(ISR25519Verify.verify, (message, HOTKEY, hotkeyR, hotkeyS));
        vm.mockCall(IED25519VERIFY_ADDRESS, vpkCall, abi.encode(vpkResult));
        vm.mockCall(ISR25519VERIFY_ADDRESS, hotkeyCall, abi.encode(hotkeyResult));
        vm.expectCall(IED25519VERIFY_ADDRESS, vpkCall, uint64(1));
        vm.expectCall(ISR25519VERIFY_ADDRESS, hotkeyCall, uint64(1));
    }

    /// @dev Exactly one valid-shape mutation for each of the17 fixed fields.
    function _mutate(ValidatorEvidenceActivation.Record memory record, uint256 field)
        internal
        pure
        returns (ValidatorEvidenceActivation.Record memory)
    {
        if (field == 0) {
            record.domain.chainId++;
        } else if (field == 1) {
            record.domain.genesisHash ^= bytes32(uint256(1));
        } else if (field == 2) {
            record.domain.netuid++;
        } else if (field == 3) {
            record.domain.coordinator = address(uint160(record.domain.coordinator) + 1);
        } else if (field == 4) {
            record.domain.settlementVault = address(uint160(record.domain.settlementVault) + 1);
        } else if (field == 5) {
            record.domain.deploymentIdHash ^= bytes32(uint256(1));
        } else if (field == 6) {
            record.domain.policyHash ^= bytes32(uint256(1));
        } else if (field == 7) {
            record.domain.epoch++;
        } else if (field == 8) {
            record.hotkey ^= bytes32(uint256(1));
        } else if (field == 9) {
            record.noId++;
        } else if (field == 10) {
            record.vpk ^= bytes32(uint256(1));
        } else if (field == 11) {
            record.firstSequence++;
        } else if (field == 12) {
            record.priorRoot ^= bytes32(uint256(1));
        } else if (field == 13) {
            record.nativeBlock++;
        } else if (field == 14) {
            record.nativeHash ^= bytes32(uint256(1));
        } else if (field == 15) {
            record.evmBlock++;
        } else if (field == 16) {
            record.evmHash ^= bytes32(uint256(1));
        } else {
            revert("unknown activation mutation");
        }
        return record;
    }

    /// @dev Homogeneous missing/contradictory-field variants, not valid epochs.
    function _invalid(uint256 field)
        internal
        pure
        returns (ValidatorEvidenceActivation.Record memory record)
    {
        record = _record();
        if (field == 0) record.domain.chainId = 0;
        else if (field == 1) record.domain.genesisHash = 0;
        else if (field == 2) record.domain.netuid = 0;
        else if (field == 3) record.domain.coordinator = address(0);
        else if (field == 4) record.domain.settlementVault = address(0);
        else if (field == 5) record.domain.settlementVault = record.domain.coordinator;
        else if (field == 6) record.domain.deploymentIdHash = 0;
        else if (field == 7) record.domain.policyHash = 0;
        else if (field == 8) record.hotkey = 0;
        else if (field == 9) record.vpk = 0;
        else if (field == 10) record.noId = 0;
        else if (field == 11) record.firstSequence = 0;
        else if (field == 12) record.firstSequence = type(uint64).max;
        else if (field == 13) record.firstSequence = 1;
        else if (field == 14) record.priorRoot = 0;
        else if (field == 15) record.nativeBlock = 0;
        else if (field == 16) record.nativeHash = 0;
        else if (field == 17) record.evmBlock = 0;
        else if (field == 18) record.evmHash = 0;
        else revert("unknown invalid activation field");
    }

    function testActivationGoldenPackedVector() public view {
        ValidatorEvidenceActivation.Record memory record = _record();
        assertEq(_golden().length, 389);
        assertEq(harness.payload(record), _golden());
        assertEq(harness.digest(record), sha256(_golden()));
        assertTrue(harness.validAt(record, _record()));
    }

    function testActivationEveryFixedFieldBindsDigest() public view {
        assertEq(abi.encode(_record()).length, 17 * 32, "fixed-field census changed");
        for (uint256 field; field < 17; field++) {
            ValidatorEvidenceActivation.Record memory changed = _mutate(_record(), field);
            assertTrue(harness.valid(changed), "mutation must retain valid shape");
            assertNotEq(harness.digest(changed), sha256(_golden()), "field is absent from consent");
        }
    }

    function testActivationCompleteExpectedContextBeforePrecompiles() public view {
        for (uint256 field; field < 17; field++) {
            ValidatorEvidenceActivation.Record memory changed = _mutate(_record(), field);
            assertFalse(harness.validAt(changed, _record()), "candidate field ignored");
            assertFalse(harness.validAt(_record(), changed), "expected field ignored");
            assertFalse(harness.verify(changed, _record(), _vpkSignature(), _hotkeySignature()));
            assertFalse(harness.verify(_record(), changed, _vpkSignature(), _hotkeySignature()));
        }
    }

    function testActivationIncompleteFieldsBeforePrecompiles() public view {
        for (uint256 field; field < 19; field++) {
            ValidatorEvidenceActivation.Record memory invalid = _invalid(field);
            assertFalse(harness.valid(invalid), "invalid field accepted");
            assertFalse(harness.validAt(invalid, invalid), "self-selected invalid shape accepted");
            assertFalse(harness.verify(invalid, _record(), _vpkSignature(), _hotkeySignature()));
            assertFalse(harness.verify(_record(), invalid, _vpkSignature(), _hotkeySignature()));
        }
    }

    function testActivationInvalidShapeEmitsNoPayloadDigestOrDomain() public {
        for (uint256 field; field < 19; field++) {
            ValidatorEvidenceActivation.Record memory invalid = _invalid(field);
            vm.expectRevert(ValidatorEvidenceActivation.InvalidActivation.selector);
            harness.payload(invalid);
            vm.expectRevert(ValidatorEvidenceActivation.InvalidActivation.selector);
            harness.digest(invalid);
            vm.expectRevert(ValidatorEvidenceActivation.InvalidActivation.selector);
            harness.evidenceDomain(invalid);
        }
    }

    function testActivationSignatureLengthsBeforePrecompiles() public view {
        for (uint256 length; length <= 65; length++) {
            if (length == 64) continue;
            assertFalse(harness.verify(_record(), _record(), new bytes(length), _hotkeySignature()));
            assertFalse(harness.verify(_record(), _record(), _vpkSignature(), new bytes(length)));
        }
    }

    function testActivationIndependentRelayChecksBothExactTuples() public {
        _mockChecks(true, true);
        vm.prank(address(0xBAD));
        assertTrue(harness.verify(_record(), _record(), _vpkSignature(), _hotkeySignature()));
    }

    function testActivationVPKFailureStillChecksHotkey() public {
        _mockChecks(false, true);
        assertFalse(harness.verify(_record(), _record(), _vpkSignature(), _hotkeySignature()));
    }

    function testActivationHotkeyFailureRefuses() public {
        _mockChecks(true, false);
        assertFalse(harness.verify(_record(), _record(), _vpkSignature(), _hotkeySignature()));
    }

    function testActivationBothSignatureFailuresRefuse() public {
        _mockChecks(false, false);
        assertFalse(harness.verify(_record(), _record(), _vpkSignature(), _hotkeySignature()));
    }

    function testActivationEmptyPrefixAndEpochZero() public view {
        ValidatorEvidenceActivation.Record memory record = _record();
        record.domain.epoch = 0;
        record.firstSequence = 1;
        record.priorRoot = 0;
        assertTrue(harness.validAt(record, record));
        ValidatorEvidence.Domain memory domain = harness.evidenceDomain(record);
        assertTrue(ValidatorEvidence.validDomain(domain));
        assertEq(domain.activationEpoch, 0);
        assertEq(domain.activationHash, harness.digest(record));
    }

    function testActivationFullWidthsDoNotCompareCrossChainHeights() public view {
        ValidatorEvidenceActivation.Record memory record = _record();
        record.domain.chainId = type(uint64).max;
        record.domain.netuid = type(uint16).max;
        record.domain.epoch = type(uint64).max;
        record.noId = type(uint64).max;
        record.firstSequence = type(uint64).max - 1;
        record.nativeBlock = type(uint64).max;
        record.evmBlock = 1;
        assertTrue(harness.validAt(record, record));
        assertEq(harness.payload(record).length, 389);
        record.nativeBlock = 1;
        record.evmBlock = type(uint64).max;
        assertTrue(harness.validAt(record, record));
        assertEq(harness.payload(record).length, 389);
    }

    function testActivationDerivedDomainMatchesEveryFieldAndIsDetached() public view {
        ValidatorEvidenceActivation.Record memory record = _record();
        ValidatorEvidence.Domain memory domain = harness.evidenceDomain(record);
        assertEq(domain.chainId, record.domain.chainId);
        assertEq(domain.genesisHash, record.domain.genesisHash);
        assertEq(domain.netuid, record.domain.netuid);
        assertEq(domain.coordinator, record.domain.coordinator);
        assertEq(domain.settlementVault, record.domain.settlementVault);
        assertEq(domain.deploymentIdHash, record.domain.deploymentIdHash);
        assertEq(domain.policyHash, record.domain.policyHash);
        assertEq(domain.activationEpoch, record.domain.epoch);
        assertEq(domain.activationHash, sha256(_golden()));
        domain.policyHash ^= bytes32(uint256(1));
        domain.activationHash ^= bytes32(uint256(1));
        assertEq(harness.digest(record), sha256(_golden()));
        assertEq(harness.evidenceDomain(record).activationHash, sha256(_golden()));
    }

    function testActivationLaterHeaderHasNoSelfReference() public view {
        ValidatorEvidenceActivation.Record memory record = _record();
        ValidatorEvidence.Header memory header;
        header.domain = harness.evidenceDomain(record);
        header.hotkey = HOTKEY;
        header.noId = 7;
        header.epoch = 44;
        header.kind = 1;
        header.vpk = VPK;
        header.boundaryBlock = 1059;
        header.boundaryHash = bytes32(uint256(0x51));
        header.censusHash = bytes32(uint256(0x52));
        header.payloadHash = bytes32(uint256(0x53));
        header.payloadBytes = 4096;
        ValidatorEvidence.Window memory window = ValidatorEvidence.Window({
            epoch: 44,
            startBlock: 1000,
            endBlock: 1060,
            finalizedBlock: 1080,
            subject: ValidatorEvidence.Subject({observationEpoch: 0, nativeEpoch: 0})
        });
        assertTrue(ValidatorEvidence.validAt(header, harness.evidenceDomain(record), window));
        bytes32 before = ValidatorEvidence.digest(header);
        header.censusHash ^= bytes32(uint256(1));
        header.payloadHash ^= bytes32(uint256(1));
        assertNotEq(ValidatorEvidence.digest(header), before);
        assertEq(harness.digest(record), sha256(_golden()));
        record.nativeBlock++;
        assertFalse(ValidatorEvidence.validAt(header, harness.evidenceDomain(record), window));
    }

    function testActivationTypedABIRejectsIntegerAndAddressTruncation() public {
        uint256[9] memory words = [uint256(0), 2, 3, 4, 7, 9, 11, 13, 15];
        uint256[9] memory invalidValues = [
            uint256(type(uint64).max) + 1,
            uint256(type(uint16).max) + 1,
            uint256(type(uint160).max) + 1,
            uint256(type(uint160).max) + 1,
            uint256(type(uint64).max) + 1,
            uint256(type(uint64).max) + 1,
            uint256(type(uint64).max) + 1,
            uint256(type(uint64).max) + 1,
            uint256(type(uint64).max) + 1
        ];
        for (uint256 index; index < words.length; index++) {
            bytes memory callData = abi.encodeCall(harness.payload, (_record()));
            uint256 word = words[index];
            uint256 value = invalidValues[index];
            assembly ("memory-safe") {
                mstore(add(add(callData, 36), mul(word, 32)), value)
            }
            (bool ok,) = address(harness).call(callData);
            assertFalse(ok, "ABI decoder silently truncated activation context");
        }
    }
}
