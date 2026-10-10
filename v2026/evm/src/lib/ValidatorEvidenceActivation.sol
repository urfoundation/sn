// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ValidatorEvidence} from "./ValidatorEvidence.sol";
import {IEd25519Verify, IED25519VERIFY_ADDRESS} from "../interfaces/ed25519Verify.sol";
import {ISR25519Verify, ISR25519VERIFY_ADDRESS} from "../interfaces/sr25519Verify.sol";

/// @notice Earlier migration consent, independent of later evidence and payout
/// roots. Historical eligibility, migration replay and inclusion are external.
library ValidatorEvidenceActivation {
    string internal constant SIGN_DOMAIN = "urnetwork/validator-evidence-activation/v1";

    /// @dev No activation hash: this earlier record produces that hash itself.
    struct Domain {
        uint64 chainId;
        bytes32 genesisHash;
        uint16 netuid;
        address coordinator;
        address settlementVault;
        bytes32 deploymentIdHash;
        bytes32 policyHash;
        uint64 epoch;
    }

    /// @dev Fixed-width snapshots/prefix are independently authenticated by the
    /// caller. Neither current UID nor relayer is a persistent owner identity.
    struct Record {
        Domain domain;
        bytes32 hotkey;
        uint64 noId;
        bytes32 vpk;
        uint64 firstSequence;
        bytes32 priorRoot;
        uint64 nativeBlock;
        bytes32 nativeHash;
        uint64 evmBlock;
        bytes32 evmHash;
    }

    error InvalidActivation();

    /// @dev Epoch zero is permitted without inventing a placeholder anchor.
    function validDomain(Domain memory domain) internal pure returns (bool) {
        return domain.chainId != 0 && domain.genesisHash != bytes32(0) && domain.netuid != 0
            && domain.coordinator != address(0) && domain.settlementVault != address(0)
            && domain.coordinator != domain.settlementVault && domain.deploymentIdHash != bytes32(0)
            && domain.policyHash != bytes32(0);
    }

    /// @dev Cross-chain time ordering cannot be inferred by comparing heights.
    function valid(Record memory record) internal pure returns (bool) {
        return validDomain(record.domain) && record.hotkey != bytes32(0) && record.noId != 0
            && record.vpk != bytes32(0) && record.nativeBlock != 0 && record.nativeHash != bytes32(0)
            && record.evmBlock != 0 && record.evmHash != bytes32(0) && record.firstSequence != 0
            && record.firstSequence != type(uint64).max
            && ((record.firstSequence == 1) == (record.priorRoot == bytes32(0)));
    }

    /// @dev Exact expected state must come from authenticated history, not by
    /// copying this candidate. Every field participates before either signature.
    function validAt(Record memory record, Record memory expected) internal pure returns (bool) {
        if (!valid(record) || !valid(expected)) return false;
        return record.domain.chainId == expected.domain.chainId
            && record.domain.genesisHash == expected.domain.genesisHash
            && record.domain.netuid == expected.domain.netuid
            && record.domain.coordinator == expected.domain.coordinator
            && record.domain.settlementVault == expected.domain.settlementVault
            && record.domain.deploymentIdHash == expected.domain.deploymentIdHash
            && record.domain.policyHash == expected.domain.policyHash
            && record.domain.epoch == expected.domain.epoch && record.hotkey == expected.hotkey
            && record.noId == expected.noId && record.vpk == expected.vpk
            && record.firstSequence == expected.firstSequence && record.priorRoot == expected.priorRoot
            && record.nativeBlock == expected.nativeBlock && record.nativeHash == expected.nativeHash
            && record.evmBlock == expected.evmBlock && record.evmHash == expected.evmHash;
    }

    /// @dev Exact Go packed widths; neither signatures nor later objects enter.
    function payload(Record memory record) internal pure returns (bytes memory) {
        if (!valid(record)) revert InvalidActivation();
        return abi.encodePacked(
            bytes(SIGN_DOMAIN),
            bytes1(0),
            record.domain.chainId,
            record.domain.genesisHash,
            record.domain.netuid,
            record.domain.coordinator,
            record.domain.settlementVault,
            record.domain.deploymentIdHash,
            record.domain.policyHash,
            record.domain.epoch,
            record.hotkey,
            record.noId,
            record.vpk,
            record.firstSequence,
            record.priorRoot,
            record.nativeBlock,
            record.nativeHash,
            record.evmBlock,
            record.evmHash
        );
    }

    /// @dev Both owners sign this tagged unsigned digest; later evidence names it.
    function digest(Record memory record) internal pure returns (bytes32) {
        return sha256(payload(record));
    }

    /// @dev This value conversion is not a historical inclusion or role proof.
    function evidenceDomain(Record memory record) internal pure returns (ValidatorEvidence.Domain memory) {
        return ValidatorEvidence.Domain({
            chainId: record.domain.chainId,
            genesisHash: record.domain.genesisHash,
            netuid: record.domain.netuid,
            coordinator: record.domain.coordinator,
            settlementVault: record.domain.settlementVault,
            deploymentIdHash: record.domain.deploymentIdHash,
            policyHash: record.domain.policyHash,
            activationEpoch: record.domain.epoch,
            activationHash: digest(record)
        });
    }

    /// @dev Only called after the complete 64-byte length check.
    function signatureWords(bytes memory signature) private pure returns (bytes32 r, bytes32 s) {
        assembly ("memory-safe") {
            r := mload(add(signature, 32))
            s := mload(add(signature, 64))
        }
    }

    /// @dev No operator approval or sender lookup; consent can be relayed. The
    /// enclosing storage path must authenticate expected history independently.
    function verify(
        Record memory record,
        Record memory expected,
        bytes memory vpkSignature,
        bytes memory hotkeySignature
    ) internal pure returns (bool) {
        if (!validAt(record, expected) || vpkSignature.length != 64 || hotkeySignature.length != 64) return false;
        bytes32 message = digest(record);
        (bytes32 vpkR, bytes32 vpkS) = signatureWords(vpkSignature);
        (bytes32 hotkeyR, bytes32 hotkeyS) = signatureWords(hotkeySignature);
        bool vpkValid = IEd25519Verify(IED25519VERIFY_ADDRESS).verify(message, record.vpk, vpkR, vpkS);
        bool hotkeyValid =
            ISR25519Verify(ISR25519VERIFY_ADDRESS).verify(message, record.hotkey, hotkeyR, hotkeyS);
        return vpkValid && hotkeyValid;
    }
}
