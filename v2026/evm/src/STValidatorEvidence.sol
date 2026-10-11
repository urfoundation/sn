// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import {STCoordinator} from "./STCoordinator.sol";
import {ValidatorEvidence} from "./lib/ValidatorEvidence.sol";
import {ValidatorEvidenceActivation} from "./lib/ValidatorEvidenceActivation.sol";

/// @notice Immutable hash journal in the coordinator's operator-pool namespace.
/// It authenticates dual-key consent and contract-local timing/domain facts,
/// not historical validator eligibility, proof truth or public availability.
/// Readers must replay those facts from finalized history and API/MinIO bytes.
/// No owner, operator approval, pause, payment or custody capability exists here.
contract STValidatorEvidence {
    struct Activation {
        ValidatorEvidenceActivation.Record record;
        uint64 publishedBlock;
    }

    struct Commitment {
        ValidatorEvidence.Header header;
        uint64 publishedBlock;
    }

    STCoordinator public immutable coordinator;
    address public immutable settlementVault;
    uint64 public immutable chainId;
    uint16 public immutable netuid;
    bytes32 public immutable genesisHash;
    bytes32 public immutable deploymentIdHash;

    mapping(bytes32 digest => Activation activation) private _activations;
    mapping(bytes32 slot => Commitment commitment) private _commitments;

    event ActivationPublished(
        bytes32 indexed activationHash, bytes32 indexed hotkey, uint64 indexed noId, uint64 epoch
    );
    event EvidenceCommitted(
        bytes32 indexed slot,
        uint64 indexed noId,
        uint64 indexed epoch,
        bytes32 headerHash,
        bytes32 payloadHash,
        bytes32 censusHash,
        uint64 payloadBytes
    );

    error InvalidConfiguration();
    error Unanchored();
    error InvalidActivation();
    error InvalidEvidence();
    error AlreadyPublished();
    error AlreadyCommitted();

    constructor(STCoordinator coordinator_, bytes32 genesisHash_, bytes32 deploymentIdHash_) {
        if (
            address(coordinator_).code.length == 0 || genesisHash_ == bytes32(0)
                || deploymentIdHash_ == bytes32(0)
        ) revert InvalidConfiguration();
        address vault = address(coordinator_.settlementVault());
        uint16 subnet = coordinator_.netuid();
        if (vault.code.length == 0 || subnet == 0 || block.chainid == 0) revert InvalidConfiguration();
        coordinator = coordinator_;
        settlementVault = vault;
        chainId = SafeCast.toUint64(block.chainid);
        netuid = subnet;
        genesisHash = genesisHash_;
        deploymentIdHash = deploymentIdHash_;
    }

    /// @dev A lookalike deployment cannot become the approved pool journal.
    /// Rechecking chain ID also refuses replay after an incompatible chain fork.
    modifier anchored() {
        if (coordinator.validatorEvidence() != address(this) || block.chainid != chainId) {
            revert Unanchored();
        }
        _;
    }

    function activation(bytes32 digest) external view returns (Activation memory) {
        return _activations[digest];
    }

    function commitment(bytes32 slot) external view returns (Commitment memory) {
        return _commitments[slot];
    }

    /// @notice Any relayer may publish both keys' exact activation consent.
    /// A past epoch cannot be activated retrospectively. Migration prefixes and
    /// native/EVM snapshot hashes remain signed claims for independent replay;
    /// a current-state precompile cannot authenticate their historical truth.
    function publishActivation(
        ValidatorEvidenceActivation.Record calldata record,
        bytes calldata vpkSignature,
        bytes calldata hotkeySignature
    ) external anchored returns (bytes32 digest) {
        if (
            !ValidatorEvidenceActivation.valid(record) || record.domain.chainId != chainId
                || record.domain.genesisHash != genesisHash || record.domain.netuid != netuid
                || record.domain.coordinator != address(coordinator)
                || record.domain.settlementVault != settlementVault
                || record.domain.deploymentIdHash != deploymentIdHash
                || record.domain.epoch < coordinator.currentEpoch() || record.evmBlock >= block.number
        ) revert InvalidActivation();
        STCoordinator.PolicySnapshot memory policy = coordinator.policyAt(record.domain.epoch);
        STCoordinator.OperatorVersion memory operator =
            coordinator.operatorAt(record.noId, record.domain.epoch);
        if (record.domain.policyHash != policy.policyHash || !operator.active) revert InvalidActivation();
        digest = ValidatorEvidenceActivation.digest(record);
        if (_activations[digest].publishedBlock != 0) revert AlreadyPublished();

        // The independent contract fields were checked above. Comparing this
        // signed record with itself does not certify its migration/history;
        // this use of the common helper verifies consent only for those claims.
        if (!ValidatorEvidenceActivation.verify(record, record, vpkSignature, hotkeySignature)) {
            revert InvalidActivation();
        }
        _activations[digest] = Activation({record: record, publishedBlock: SafeCast.toUint64(block.number)});
        emit ActivationPublished(digest, record.hotkey, record.noId, record.domain.epoch);
    }

    /// @notice Write-once closed-census or later audit commitment. The owner
    /// slot excludes content, boundary, VPK, relayer and activation revisions,
    /// so changing any of them cannot authorize a second version of a slot.
    /// Publication never waits for an operator payout root or its deadline.
    function commitEvidence(
        ValidatorEvidence.Header calldata header,
        bytes calldata vpkSignature,
        bytes calldata hotkeySignature
    ) external anchored returns (bytes32 slot) {
        Activation storage prior = _activations[header.domain.activationHash];
        if (
            prior.publishedBlock == 0 || !ValidatorEvidence.valid(header)
                || prior.publishedBlock > header.boundaryBlock || header.hotkey != prior.record.hotkey
                || header.noId != prior.record.noId || header.vpk != prior.record.vpk
                || header.boundaryBlock >= block.number
        ) revert InvalidEvidence();
        ValidatorEvidence.Domain memory domain = ValidatorEvidenceActivation.evidenceDomain(prior.record);
        STCoordinator.PolicySnapshot memory policy = coordinator.policyAt(header.epoch);
        if (policy.policyHash != header.domain.policyHash) revert InvalidEvidence();
        uint64 start = SafeCast.toUint64(coordinator.epochStartBlock(header.epoch));
        uint64 end = SafeCast.toUint64(coordinator.epochEndBlock(header.epoch));
        if (end >= block.number) revert InvalidEvidence();

        if (header.kind == ValidatorEvidence.DEPOSIT_AUDIT) {
            uint256 observationStart = coordinator.epochStartBlock(header.subject.observationEpoch);
            uint256 observationEnd = coordinator.epochEndBlock(header.subject.observationEpoch);
            if (header.boundaryBlock < observationStart || header.boundaryBlock >= observationEnd) {
                revert InvalidEvidence();
            }
        }
        // "finalizedBlock" is the library's upper observation bound here,
        // not an RPC-finality assertion. A consumer must independently wait
        // for finalized inclusion and authenticate both claimed block hashes.
        ValidatorEvidence.Window memory window = ValidatorEvidence.Window({
            epoch: header.epoch,
            startBlock: start,
            endBlock: end,
            finalizedBlock: SafeCast.toUint64(block.number - 1),
            subject: header.subject
        });
        if (!ValidatorEvidence.verify(header, domain, window, vpkSignature, hotkeySignature)) {
            revert InvalidEvidence();
        }
        slot = ValidatorEvidence.slotKey(header);
        if (_commitments[slot].publishedBlock != 0) revert AlreadyCommitted();
        _commitments[slot] = Commitment({header: header, publishedBlock: SafeCast.toUint64(block.number)});
        emit EvidenceCommitted(
            slot,
            header.noId,
            header.epoch,
            ValidatorEvidence.digest(header),
            header.payloadHash,
            header.censusHash,
            header.payloadBytes
        );
    }
}
