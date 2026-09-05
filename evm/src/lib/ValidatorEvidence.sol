// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {IEd25519Verify, IED25519VERIFY_ADDRESS} from "../interfaces/ed25519Verify.sol";
import {ISR25519Verify, ISR25519VERIFY_ADDRESS} from "../interfaces/sr25519Verify.sol";

/// @notice Stateless, fixed-cost evidence consent shared by either storage
/// transport. It does not prove historical eligibility or census completeness.
library ValidatorEvidence {
    string internal constant HEADER_DOMAIN = "urnetwork/validator-evidence-header/v1";
    string internal constant SLOT_DOMAIN = "urnetwork/validator-evidence-slot/v1";
    string internal constant AUDIT_SUBJECT_DOMAIN = "urnetwork/validator-evidence-audit-subject/v1";
    uint8 internal constant CLOSED_CENSUS = 1;
    uint8 internal constant DEPOSIT_AUDIT = 2;

    /// @dev Activation refers to an earlier immutable object, never this header.
    struct Domain {
        uint64 chainId;
        bytes32 genesisHash;
        uint16 netuid;
        address coordinator;
        address settlementVault;
        bytes32 deploymentIdHash;
        bytes32 policyHash;
        uint64 activationEpoch;
        bytes32 activationHash;
    }

    /// @dev Zero for terminal closure; later audits use actual cycle coordinates,
    /// not a verdict, prepared intent, content hash or caller-selected nonce.
    struct Subject {
        uint64 observationEpoch;
        uint64 nativeEpoch;
    }

    /// @dev Census hashes unsigned member payloads, never these signed headers.
    struct Header {
        Domain domain;
        bytes32 hotkey;
        uint64 noId;
        uint64 epoch;
        uint8 kind;
        Subject subject;
        bytes32 vpk;
        uint64 boundaryBlock;
        bytes32 boundaryHash;
        bytes32 censusHash;
        bytes32 payloadHash;
        uint64 payloadBytes;
    }

    /// @dev The caller supplies authenticated expected history. End is exclusive.
    struct Window {
        uint64 epoch;
        uint64 startBlock;
        uint64 endBlock;
        uint64 finalizedBlock;
        Subject subject;
    }

    error InvalidEvidence();

    /// @dev Zero activation epoch is valid only with a nonzero activation anchor.
    function validDomain(Domain memory domain) internal pure returns (bool) {
        return domain.chainId != 0 && domain.genesisHash != bytes32(0) && domain.netuid != 0
            && domain.coordinator != address(0) && domain.settlementVault != address(0)
            && domain.coordinator != domain.settlementVault && domain.deploymentIdHash != bytes32(0)
            && domain.policyHash != bytes32(0) && domain.activationHash != bytes32(0);
    }

    /// @dev All encoded integers retain their declared widths without truncation.
    function domainPayload(Domain memory domain) internal pure returns (bytes memory) {
        return abi.encodePacked(
            domain.chainId,
            domain.genesisHash,
            domain.netuid,
            domain.coordinator,
            domain.settlementVault,
            domain.deploymentIdHash,
            domain.policyHash,
            domain.activationEpoch,
            domain.activationHash
        );
    }

    /// @dev No ordinary native cut may be mislabeled as closed terminal evidence.
    function valid(Header memory header) internal pure returns (bool) {
        if (
            !validDomain(header.domain) || header.hotkey == bytes32(0) || header.vpk == bytes32(0) || header.noId == 0
                || header.epoch < header.domain.activationEpoch || header.boundaryBlock == 0
                || header.boundaryHash == bytes32(0) || header.censusHash == bytes32(0)
                || header.payloadHash == bytes32(0) || header.payloadBytes == 0
        ) return false;
        if (header.kind == CLOSED_CENSUS) {
            return header.subject.observationEpoch == 0 && header.subject.nativeEpoch == 0;
        }
        if (header.kind == DEPOSIT_AUDIT) return header.subject.observationEpoch > header.epoch;
        return false;
    }

    /// @dev Compares the complete trusted domain before accepting signed input.
    function validAt(Header memory header, Domain memory expected, Window memory window) internal pure returns (bool) {
        if (!validDomain(expected) || !valid(header)) return false;
        if (
            header.domain.chainId != expected.chainId || header.domain.genesisHash != expected.genesisHash
                || header.domain.netuid != expected.netuid || header.domain.coordinator != expected.coordinator
                || header.domain.settlementVault != expected.settlementVault
                || header.domain.deploymentIdHash != expected.deploymentIdHash
                || header.domain.policyHash != expected.policyHash
                || header.domain.activationEpoch != expected.activationEpoch
                || header.domain.activationHash != expected.activationHash
        ) return false;
        if (
            header.epoch != window.epoch || header.subject.observationEpoch != window.subject.observationEpoch
                || header.subject.nativeEpoch != window.subject.nativeEpoch || window.startBlock == 0
                || window.endBlock <= window.startBlock || window.finalizedBlock < window.endBlock
                || header.boundaryBlock > window.finalizedBlock
        ) return false;
        if (header.kind == CLOSED_CENSUS) return header.boundaryBlock == window.endBlock - 1;
        return header.boundaryBlock >= window.endBlock;
    }

    /// @dev A changed terminal boundary cannot create an alternate owner slot.
    function subjectHash(Header memory header) internal pure returns (bytes32) {
        if (!valid(header)) revert InvalidEvidence();
        if (header.kind == CLOSED_CENSUS) return bytes32(0);
        return sha256(
            abi.encodePacked(
                bytes(AUDIT_SUBJECT_DOMAIN), bytes1(0), header.subject.observationEpoch, header.subject.nativeEpoch
            )
        );
    }

    /// @dev Byte-for-byte equivalent to protocol.ValidatorEvidenceHeader.Payload.
    function payload(Header memory header) internal pure returns (bytes memory) {
        bytes32 subject = subjectHash(header);
        return abi.encodePacked(
            bytes(HEADER_DOMAIN),
            bytes1(0),
            domainPayload(header.domain),
            header.hotkey,
            header.noId,
            header.epoch,
            header.kind,
            subject,
            header.vpk,
            header.boundaryBlock,
            header.boundaryHash,
            header.censusHash,
            header.payloadHash,
            header.payloadBytes
        );
    }

    /// @dev Both signature precompiles receive this exact 32-byte message.
    function digest(Header memory header) internal pure returns (bytes32) {
        return sha256(payload(header));
    }

    /// @dev Excludes VPK, content, policy/activation revisions, UID and relayer.
    /// Complete domain validation is still mandatory before any future storage.
    function slotKey(Header memory header) internal pure returns (bytes32) {
        bytes32 subject = subjectHash(header);
        Domain memory domain = header.domain;
        return sha256(
            abi.encodePacked(
                bytes(SLOT_DOMAIN),
                bytes1(0),
                domain.chainId,
                domain.genesisHash,
                domain.netuid,
                domain.coordinator,
                domain.settlementVault,
                domain.deploymentIdHash,
                header.hotkey,
                header.noId,
                header.epoch,
                header.kind,
                subject
            )
        );
    }

    /// @dev The fixed signature length is checked before either word is read.
    function signatureWords(bytes memory signature) private pure returns (bytes32 r, bytes32 s) {
        assembly ("memory-safe") {
            r := mload(add(signature, 32))
            s := mload(add(signature, 64))
        }
    }

    /// @dev Exactly two fixed-size checks for well-shaped input, no census loop
    /// or current UID/permit lookup. Historical role remains public verification.
    function verify(
        Header memory header,
        Domain memory expected,
        Window memory window,
        bytes memory vpkSignature,
        bytes memory hotkeySignature
    ) internal view returns (bool) {
        if (!validAt(header, expected, window) || vpkSignature.length != 64 || hotkeySignature.length != 64) {
            return false;
        }
        bytes32 message = digest(header);
        (bytes32 vpkR, bytes32 vpkS) = signatureWords(vpkSignature);
        (bytes32 hotkeyR, bytes32 hotkeyS) = signatureWords(hotkeySignature);
        bool vpkValid = IEd25519Verify(IED25519VERIFY_ADDRESS).verify(message, header.vpk, vpkR, vpkS);
        bool hotkeyValid = ISR25519Verify(ISR25519VERIFY_ADDRESS).verify(message, header.hotkey, hotkeyR, hotkeyS);
        return vpkValid && hotkeyValid;
    }
}
