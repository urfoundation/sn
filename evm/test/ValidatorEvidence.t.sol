// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

// Pure byte vectors and exact mocked call tuples only. These tests do not
// establish actual runtime precompile cryptography or historical eligibility.

import {Test} from "forge-std/Test.sol";
import {ValidatorEvidence} from "../src/lib/ValidatorEvidence.sol";
import {IEd25519Verify, IED25519VERIFY_ADDRESS} from "../src/interfaces/ed25519Verify.sol";
import {ISR25519Verify, ISR25519VERIFY_ADDRESS} from "../src/interfaces/sr25519Verify.sol";

/// @dev No storage or transaction path: exposes stateless library operations.
contract ValidatorEvidenceHarness {
    function payload(ValidatorEvidence.Header memory header) external pure returns (bytes memory) {
        return ValidatorEvidence.payload(header);
    }

    function digest(ValidatorEvidence.Header memory header) external pure returns (bytes32) {
        return ValidatorEvidence.digest(header);
    }

    function slotKey(ValidatorEvidence.Header memory header) external pure returns (bytes32) {
        return ValidatorEvidence.slotKey(header);
    }

    function valid(ValidatorEvidence.Header memory header) external pure returns (bool) {
        return ValidatorEvidence.valid(header);
    }

    function validAt(
        ValidatorEvidence.Header memory header,
        ValidatorEvidence.Domain memory expected,
        ValidatorEvidence.Window memory window
    ) external pure returns (bool) {
        return ValidatorEvidence.validAt(header, expected, window);
    }

    function verify(
        ValidatorEvidence.Header memory header,
        ValidatorEvidence.Domain memory expected,
        ValidatorEvidence.Window memory window,
        bytes memory vpkSignature,
        bytes memory hotkeySignature
    ) external view returns (bool) {
        return ValidatorEvidence.verify(header, expected, window, vpkSignature, hotkeySignature);
    }
}

/// @dev Every unmocked/incorrect tuple fails, including premature verifier calls.
contract ValidatorEvidenceUnexpectedPrecompile {
    fallback() external {
        revert("unexpected evidence precompile call");
    }
}

/// @dev Public vectors are generated without importing the Go implementation.
contract ValidatorEvidenceTest is Test {
    bytes32 internal constant HOTKEY = hex"94ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b47";
    bytes32 internal constant VPK = hex"03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8";
    bytes32 internal constant CLOSED_DIGEST = hex"8472096e8efd7f696041e2deb755bf3f70d3e63ddd5e306de85f7bd2b9bc9880";
    bytes32 internal constant AUDIT_DIGEST = hex"1128a34f2893fa3e90d41f382f29e7fa57f7437a42ef4ef2a515ba01335341db";
    bytes32 internal constant CLOSED_SLOT = hex"1995af5d4e8460aff2899ddfc1997babc826ffc99c34b1c25804446a6d55b7e9";
    bytes32 internal constant AUDIT_SLOT = hex"68971bb15dfffe5292158b14b4a61bea59aef39f4af8d840612b76f7af405597";

    ValidatorEvidenceHarness internal harness;

    function setUp() public {
        harness = new ValidatorEvidenceHarness();
        address trap = address(new ValidatorEvidenceUnexpectedPrecompile());
        vm.etch(IED25519VERIFY_ADDRESS, trap.code);
        vm.etch(ISR25519VERIFY_ADDRESS, trap.code);
    }

    /// @dev All values match the independent fixed-width reference source.
    function _closed() internal pure returns (ValidatorEvidence.Header memory header) {
        header.domain = ValidatorEvidence.Domain({
            chainId: 945,
            genesisHash: bytes32(uint256(0x1313131313131313131313131313131313131313131313131313131313131313)),
            netuid: 17,
            coordinator: 0x1111111111111111111111111111111111111111,
            settlementVault: 0x1212121212121212121212121212121212121212,
            deploymentIdHash: bytes32(uint256(0x1414141414141414141414141414141414141414141414141414141414141414)),
            policyHash: bytes32(uint256(0x1515151515151515151515151515151515151515151515151515151515151515)),
            activationEpoch: 42,
            activationHash: bytes32(uint256(0x1616161616161616161616161616161616161616161616161616161616161616))
        });
        header.hotkey = HOTKEY;
        header.noId = 7;
        header.epoch = 44;
        header.kind = 1;
        header.vpk = VPK;
        header.boundaryBlock = 1059;
        header.boundaryHash = bytes32(uint256(0x1717171717171717171717171717171717171717171717171717171717171717));
        header.censusHash = bytes32(uint256(0x1818181818181818181818181818181818181818181818181818181818181818));
        header.payloadHash = bytes32(uint256(0x1919191919191919191919191919191919191919191919191919191919191919));
        header.payloadBytes = 4096;
    }

    /// @dev No payout, prepared intent, operator permission or live UID is needed.
    function _audit() internal pure returns (ValidatorEvidence.Header memory header) {
        header = _closed();
        header.kind = 2;
        header.subject = ValidatorEvidence.Subject({observationEpoch: 45, nativeEpoch: 700});
        header.boundaryBlock = 1070;
        header.payloadBytes = 512;
    }

    /// @dev End is the first block of the next epoch, not its predecessor.
    function _window(ValidatorEvidence.Header memory header) internal pure returns (ValidatorEvidence.Window memory) {
        return ValidatorEvidence.Window({
            epoch: 44,
            startBlock: 1000,
            endBlock: 1060,
            finalizedBlock: 1080,
            subject: ValidatorEvidence.Subject({
                observationEpoch: header.subject.observationEpoch, nativeEpoch: header.subject.nativeEpoch
            })
        });
    }

    function _vpkSignature() internal pure returns (bytes memory) {
        return hex"ecb2a7565e2c4caf8426c780c06298faf16fceabcbcf189c495868049e46294058178ba961d4c59b5fd2deedb450851c9ad24ce1447afd3e20b76dca3ab0410d";
    }

    function _hotkeySignature() internal pure returns (bytes memory) {
        return hex"8620a04a3aafffb730227218061de30c3750d95a7c5be4fdaea058965e5b113b45d4d00efebb8b214039106a96cbe25feeea0740663e02d51c475af78c743e80";
    }

    /// @dev ABI word decoding is independent of the library's assembly reads.
    function _mockChecks(
        bytes32 message,
        bytes memory vpkSignature,
        bytes memory hotkeySignature,
        bool vpkResult,
        bool hotkeyResult
    ) internal {
        (bytes32 vpkR, bytes32 vpkS) = abi.decode(vpkSignature, (bytes32, bytes32));
        (bytes32 hotkeyR, bytes32 hotkeyS) = abi.decode(hotkeySignature, (bytes32, bytes32));
        bytes memory vpkCall = abi.encodeCall(IEd25519Verify.verify, (message, VPK, vpkR, vpkS));
        bytes memory hotkeyCall = abi.encodeCall(ISR25519Verify.verify, (message, HOTKEY, hotkeyR, hotkeyS));
        vm.mockCall(IED25519VERIFY_ADDRESS, vpkCall, abi.encode(vpkResult));
        vm.mockCall(ISR25519VERIFY_ADDRESS, hotkeyCall, abi.encode(hotkeyResult));
        vm.expectCall(IED25519VERIFY_ADDRESS, vpkCall, uint64(1));
        vm.expectCall(ISR25519VERIFY_ADDRESS, hotkeyCall, uint64(1));
    }

    /// @dev Mutations stay well-shaped; a kind change necessarily clears subject.
    function _mutate(ValidatorEvidence.Header memory header, uint256 field)
        internal
        pure
        returns (ValidatorEvidence.Header memory)
    {
        if (field == 0) {
            header.domain.chainId++;
        } else if (field == 1) {
            header.domain.genesisHash ^= bytes32(uint256(1));
        } else if (field == 2) {
            header.domain.netuid++;
        } else if (field == 3) {
            header.domain.coordinator = address(uint160(header.domain.coordinator) + 1);
        } else if (field == 4) {
            header.domain.settlementVault = address(uint160(header.domain.settlementVault) + 1);
        } else if (field == 5) {
            header.domain.deploymentIdHash ^= bytes32(uint256(1));
        } else if (field == 6) {
            header.domain.policyHash ^= bytes32(uint256(1));
        } else if (field == 7) {
            header.domain.activationEpoch++;
        } else if (field == 8) {
            header.domain.activationHash ^= bytes32(uint256(1));
        } else if (field == 9) {
            header.hotkey ^= bytes32(uint256(1));
        } else if (field == 10) {
            header.noId++;
        } else if (field == 11) {
            header.epoch--;
        } else if (field == 12) {
            header.kind = 1;
            header.subject = ValidatorEvidence.Subject({observationEpoch: 0, nativeEpoch: 0});
        } else if (field == 13) {
            header.subject.observationEpoch++;
        } else if (field == 14) {
            header.subject.nativeEpoch++;
        } else if (field == 15) {
            header.vpk ^= bytes32(uint256(1));
        } else if (field == 16) {
            header.boundaryBlock++;
        } else if (field == 17) {
            header.boundaryHash ^= bytes32(uint256(1));
        } else if (field == 18) {
            header.censusHash ^= bytes32(uint256(1));
        } else if (field == 19) {
            header.payloadHash ^= bytes32(uint256(1));
        } else if (field == 20) {
            header.payloadBytes++;
        } else {
            revert("unknown mutation");
        }
        return header;
    }

    function testGoldenClosedVector() public view {
        ValidatorEvidence.Header memory header = _closed();
        assertEq(
            harness.payload(header),
            hex"75726e6574776f726b2f76616c696461746f722d65766964656e63652d6865616465722f76310000000000000003b1131313131313131313131313131313131313131313131313131313131313131300111111111111111111111111111111111111111111121212121212121212121212121212121212121214141414141414141414141414141414141414141414141414141414141414141515151515151515151515151515151515151515151515151515151515151515000000000000002a161616161616161616161616161616161616161616161616161616161616161694ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b470000000000000007000000000000002c01000000000000000000000000000000000000000000000000000000000000000003a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b800000000000004231717171717171717171717171717171717171717171717171717171717171717181818181818181818181818181818181818181818181818181818181818181819191919191919191919191919191919191919191919191919191919191919190000000000001000"
        );
        assertEq(harness.digest(header), CLOSED_DIGEST);
        assertEq(harness.slotKey(header), CLOSED_SLOT);
        assertEq(ValidatorEvidence.subjectHash(header), bytes32(0));
        assertTrue(harness.validAt(header, header.domain, _window(header)));
    }

    function testGoldenAuditVector() public view {
        ValidatorEvidence.Header memory header = _audit();
        assertEq(
            harness.payload(header),
            hex"75726e6574776f726b2f76616c696461746f722d65766964656e63652d6865616465722f76310000000000000003b1131313131313131313131313131313131313131313131313131313131313131300111111111111111111111111111111111111111111121212121212121212121212121212121212121214141414141414141414141414141414141414141414141414141414141414141515151515151515151515151515151515151515151515151515151515151515000000000000002a161616161616161616161616161616161616161616161616161616161616161694ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b470000000000000007000000000000002c022d6287f7a66230a2fc77d0bff7a27de82c5b2f42c72cd8ef820e9561b2a198c503a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8000000000000042e1717171717171717171717171717171717171717171717171717171717171717181818181818181818181818181818181818181818181818181818181818181819191919191919191919191919191919191919191919191919191919191919190000000000000200"
        );
        assertEq(harness.digest(header), AUDIT_DIGEST);
        assertEq(harness.slotKey(header), AUDIT_SLOT);
        assertEq(
            ValidatorEvidence.subjectHash(header), hex"2d6287f7a66230a2fc77d0bff7a27de82c5b2f42c72cd8ef820e9561b2a198c5"
        );
        assertTrue(harness.validAt(header, header.domain, _window(header)));
    }

    function testEveryFieldBindsDigestAndSlotRules() public view {
        ValidatorEvidence.Header memory original = _audit();
        for (uint256 field; field < 21; field++) {
            ValidatorEvidence.Header memory changed = _mutate(_audit(), field);
            assertTrue(harness.valid(changed), "mutation must retain valid shape");
            assertNotEq(harness.digest(changed), harness.digest(original), "field not signed");
            bool sameSlot = field == 6 || field == 7 || field == 8 || field >= 15;
            if (sameSlot) assertEq(harness.slotKey(changed), harness.slotKey(original), "mutable second slot");
            else assertNotEq(harness.slotKey(changed), harness.slotKey(original), "different owner/cycle");
        }
        ValidatorEvidence.Header memory terminal = _closed();
        terminal.boundaryBlock++;
        terminal.boundaryHash ^= bytes32(uint256(1));
        assertEq(harness.slotKey(terminal), CLOSED_SLOT, "alternate terminal boundary slot");
    }

    function testCompleteExpectedDomainBeforePrecompiles() public view {
        for (uint256 field; field < 9; field++) {
            ValidatorEvidence.Header memory changed = _mutate(_closed(), field);
            ValidatorEvidence.Header memory original = _closed();
            assertFalse(harness.validAt(changed, original.domain, _window(original)));
            assertFalse(
                harness.verify(changed, original.domain, _window(original), _vpkSignature(), _hotkeySignature())
            );
        }
    }

    function testClosedWindowAndAuditBounds() public view {
        ValidatorEvidence.Header memory header = _closed();
        ValidatorEvidence.Window memory window = _window(header);
        window.finalizedBlock = window.endBlock - 1;
        assertFalse(harness.validAt(header, header.domain, window), "unclosed epoch");
        window = _window(header);
        header.boundaryBlock--;
        assertFalse(harness.validAt(header, header.domain, window), "early terminal");
        header.boundaryBlock += 2;
        assertFalse(harness.validAt(header, header.domain, window), "next epoch terminal");
        header.boundaryBlock = window.finalizedBlock + 1;
        assertFalse(harness.validAt(header, header.domain, window), "future terminal");
        header = _audit();
        window = _window(header);
        window.subject.nativeEpoch++;
        assertFalse(harness.validAt(header, header.domain, window), "wrong native cycle");
        window = _window(header);
        window.subject.observationEpoch++;
        assertFalse(harness.validAt(header, header.domain, window), "wrong observation epoch");
        window = _window(header);
        header.boundaryBlock = window.endBlock - 1;
        assertFalse(harness.validAt(header, header.domain, window), "audit before closure");
        header.subject.observationEpoch = header.epoch;
        assertFalse(harness.valid(header), "not a later audit");
        header = _closed();
        header.subject.nativeEpoch = 1;
        assertFalse(harness.valid(header), "arbitrary terminal subject");
        header.subject.nativeEpoch = 0;
        header.kind = 3;
        assertFalse(harness.valid(header), "ordinary cut mislabeled terminal");
    }

    function testActivationAndMaximumWidths() public view {
        ValidatorEvidence.Header memory header = _closed();
        header.epoch = header.domain.activationEpoch - 1;
        assertFalse(harness.valid(header), "pre-activation");
        header = _closed();
        header.domain.activationEpoch = 0;
        header.epoch = 0;
        header.boundaryBlock = 1;
        ValidatorEvidence.Window memory window = ValidatorEvidence.Window({
            epoch: 0,
            startBlock: 1,
            endBlock: 2,
            finalizedBlock: 2,
            subject: ValidatorEvidence.Subject({observationEpoch: 0, nativeEpoch: 0})
        });
        assertTrue(harness.validAt(header, header.domain, window), "anchored epoch zero");
        header.domain.chainId = type(uint64).max;
        header.domain.netuid = type(uint16).max;
        header.domain.activationEpoch = type(uint64).max;
        header.epoch = type(uint64).max;
        header.noId = type(uint64).max;
        header.payloadBytes = type(uint64).max;
        header.boundaryBlock = type(uint64).max - 1;
        window.epoch = type(uint64).max;
        window.startBlock = type(uint64).max - 2;
        window.endBlock = type(uint64).max;
        window.finalizedBlock = type(uint64).max;
        assertTrue(harness.validAt(header, header.domain, window), "maximum width overflow");
        assertEq(harness.payload(header).length, harness.payload(_closed()).length);
        header.kind = 2;
        header.subject.observationEpoch = type(uint64).max;
        assertFalse(harness.valid(header), "later audit overflow");
    }

    function testIncompleteFieldsRejectedBeforePrecompiles() public view {
        for (uint256 field; field < 20; field++) {
            ValidatorEvidence.Header memory header = _closed();
            if (field == 0) header.domain.chainId = 0;
            else if (field == 1) header.domain.genesisHash = 0;
            else if (field == 2) header.domain.netuid = 0;
            else if (field == 3) header.domain.coordinator = address(0);
            else if (field == 4) header.domain.settlementVault = address(0);
            else if (field == 5) header.domain.settlementVault = header.domain.coordinator;
            else if (field == 6) header.domain.deploymentIdHash = 0;
            else if (field == 7) header.domain.policyHash = 0;
            else if (field == 8) header.domain.activationHash = 0;
            else if (field == 9) header.hotkey = 0;
            else if (field == 10) header.vpk = 0;
            else if (field == 11) header.noId = 0;
            else if (field == 12) header.boundaryBlock = 0;
            else if (field == 13) header.boundaryHash = 0;
            else if (field == 14) header.censusHash = 0;
            else if (field == 15) header.payloadHash = 0;
            else if (field == 16) header.payloadBytes = 0;
            else if (field == 17) header.kind = 0;
            else if (field == 18) header.subject.observationEpoch = 1;
            else if (field == 19) header.subject.nativeEpoch = 1;
            assertFalse(harness.valid(header), "incomplete shape");
            assertFalse(
                harness.verify(header, _closed().domain, _window(_closed()), _vpkSignature(), _hotkeySignature())
            );
        }
    }

    function testSignatureLengthsRejectedBeforePrecompiles() public view {
        ValidatorEvidence.Header memory header = _closed();
        for (uint256 length; length <= 65; length++) {
            if (length == 64) continue;
            assertFalse(harness.verify(header, header.domain, _window(header), new bytes(length), _hotkeySignature()));
            assertFalse(harness.verify(header, header.domain, _window(header), _vpkSignature(), new bytes(length)));
        }
    }

    function testIndependentRelayUsesExactlyTwoDigestChecks() public {
        _mockChecks(CLOSED_DIGEST, _vpkSignature(), _hotkeySignature(), true, true);
        ValidatorEvidence.Header memory header = _closed();
        vm.prank(address(0xBAD));
        assertTrue(harness.verify(header, header.domain, _window(header), _vpkSignature(), _hotkeySignature()));
    }

    function testVPKFailureStillChecksHotkey() public {
        _mockChecks(CLOSED_DIGEST, _vpkSignature(), _hotkeySignature(), false, true);
        ValidatorEvidence.Header memory header = _closed();
        assertFalse(harness.verify(header, header.domain, _window(header), _vpkSignature(), _hotkeySignature()));
    }

    function testHotkeyFailureRejects() public {
        _mockChecks(CLOSED_DIGEST, _vpkSignature(), _hotkeySignature(), true, false);
        ValidatorEvidence.Header memory header = _closed();
        assertFalse(harness.verify(header, header.domain, _window(header), _vpkSignature(), _hotkeySignature()));
    }

    function testAuditUsesExactDigestChecks() public {
        bytes memory vpkSignature =
            hex"805ad66b585194b9e153b6bb044d91e205225100b260e2dd57dac8e76f37ebca8fbbfae466a6ce988d3f4eecf164d7e421206c3165cdfa5ed2981c5fd4ab2800";
        bytes memory hotkeySignature =
            hex"30240f7b791ec19b67e19bf23b0e255f044710c463256ac6497fc20f375a0d466d9024aba52443a6e12744f4271b103f3388cc4f09a3b458270bbb6312bab082";
        _mockChecks(AUDIT_DIGEST, vpkSignature, hotkeySignature, true, true);
        ValidatorEvidence.Header memory header = _audit();
        assertTrue(harness.verify(header, header.domain, _window(header), vpkSignature, hotkeySignature));
    }

    function testTypedABIRejectsIntegerOverflow() public {
        uint256[5] memory words = [uint256(0), 2, 10, 12, 20];
        uint256[5] memory invalidValues = [
            uint256(type(uint64).max) + 1,
            uint256(type(uint16).max) + 1,
            uint256(type(uint64).max) + 1,
            uint256(type(uint8).max) + 1,
            uint256(type(uint64).max) + 1
        ];
        for (uint256 index; index < words.length; index++) {
            bytes memory callData = abi.encodeCall(harness.payload, (_closed()));
            uint256 word = words[index];
            uint256 value = invalidValues[index];
            assembly ("memory-safe") {
                mstore(add(add(callData, 36), mul(word, 32)), value)
            }
            (bool ok,) = address(harness).call(callData);
            assertFalse(ok, "ABI decoder silently truncated an integer");
        }
    }
}
