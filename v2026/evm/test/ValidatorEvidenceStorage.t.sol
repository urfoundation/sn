// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";
import {STValidatorEvidence} from "../src/STValidatorEvidence.sol";
import {ValidatorEvidence} from "../src/lib/ValidatorEvidence.sol";
import {ValidatorEvidenceActivation} from "../src/lib/ValidatorEvidenceActivation.sol";
import {IED25519VERIFY_ADDRESS} from "../src/interfaces/ed25519Verify.sol";
import {ISR25519VERIFY_ADDRESS} from "../src/interfaces/sr25519Verify.sol";

/// @dev Only the exact approved digest/key/signature tuples may reach a mock.
contract EvidenceUnexpectedPrecompile {
    fallback() external {
        revert("unexpected evidence precompile tuple");
    }
}

/// @dev These tests prove contract state, timing and exact verifier calls.
/// The protocol golden vectors and live precompile checks separately prove
/// cryptography; these mocks do not establish historical eligibility or proofs.
contract ValidatorEvidenceStorageTest is ReleaseBase {
    bytes32 internal constant GENESIS = keccak256("evidence-test-genesis");
    bytes32 internal constant DEPLOYMENT = keccak256("evidence-test-deployment");
    bytes32 internal constant HOTKEY = keccak256("evidence-test-hotkey");
    bytes32 internal constant VPK = keccak256("evidence-test-vpk");
    STValidatorEvidence internal evidence;

    function setUp() public override {
        super.setUp();
        vm.roll(START_BLOCK + 3);
        evidence = new STValidatorEvidence(coordinator, GENESIS, DEPLOYMENT);
        address trap = address(new EvidenceUnexpectedPrecompile());
        vm.etch(IED25519VERIFY_ADDRESS, trap.code);
        vm.etch(ISR25519VERIFY_ADDRESS, trap.code);
    }

    function _anchor() internal {
        vm.prank(owner);
        coordinator.fixValidatorEvidence(address(evidence));
    }

    function _signature() internal pure returns (bytes memory) {
        return abi.encodePacked(bytes32(uint256(11)), bytes32(uint256(12)));
    }

    function _approve(bytes32 digest, bytes32 vpk, bytes32 hotkey) internal {
        bytes memory vpkCall = abi.encodeWithSignature(
            "verify(bytes32,bytes32,bytes32,bytes32)", digest, vpk, bytes32(uint256(11)), bytes32(uint256(12))
        );
        bytes memory hotkeyCall = abi.encodeWithSignature(
            "verify(bytes32,bytes32,bytes32,bytes32)",
            digest,
            hotkey,
            bytes32(uint256(11)),
            bytes32(uint256(12))
        );
        vm.mockCall(IED25519VERIFY_ADDRESS, vpkCall, abi.encode(true));
        vm.mockCall(ISR25519VERIFY_ADDRESS, hotkeyCall, abi.encode(true));
        vm.expectCall(IED25519VERIFY_ADDRESS, vpkCall);
        vm.expectCall(ISR25519VERIFY_ADDRESS, hotkeyCall);
    }

    function _record() internal view returns (ValidatorEvidenceActivation.Record memory record) {
        record.domain = ValidatorEvidenceActivation.Domain({
            chainId: 945,
            genesisHash: GENESIS,
            netuid: NETUID,
            coordinator: address(coordinator),
            settlementVault: address(vault),
            deploymentIdHash: DEPLOYMENT,
            policyHash: coordinator.policyAt(0).policyHash,
            epoch: 0
        });
        record.hotkey = HOTKEY;
        record.noId = SafeCast.toUint64(NO1);
        record.vpk = VPK;
        record.firstSequence = 1;
        record.nativeBlock = 9_999;
        record.nativeHash = keccak256("independent-native-observation");
        record.evmBlock = START_BLOCK + 1;
        record.evmHash = keccak256("independent-evm-observation");
    }

    function _publish(ValidatorEvidenceActivation.Record memory record) internal returns (bytes32 digest) {
        digest = ValidatorEvidenceActivation.digest(record);
        _approve(digest, record.vpk, record.hotkey);
        vm.prank(stranger);
        assertEq(evidence.publishActivation(record, _signature(), _signature()), digest);
    }

    function _header(ValidatorEvidenceActivation.Record memory record)
        internal
        view
        returns (ValidatorEvidence.Header memory header)
    {
        header.domain = ValidatorEvidenceActivation.evidenceDomain(record);
        header.hotkey = record.hotkey;
        header.noId = record.noId;
        header.epoch = 0;
        header.kind = ValidatorEvidence.CLOSED_CENSUS;
        header.vpk = record.vpk;
        header.boundaryBlock = uint64(_end(0) - 1);
        header.boundaryHash = keccak256("closed-terminal-block");
        header.censusHash = keccak256("unsigned-complete-member-census");
        header.payloadHash = keccak256("canonical-public-signed-bytes");
        header.payloadBytes = 1_234;
    }

    function _commit(ValidatorEvidence.Header memory header) internal returns (bytes32 slot) {
        _approve(ValidatorEvidence.digest(header), header.vpk, header.hotkey);
        vm.prank(stranger);
        slot = evidence.commitEvidence(header, _signature(), _signature());
        assertEq(slot, ValidatorEvidence.slotKey(header));
    }

    function testEvidenceAnchorOwnerOnlyAndWriteOnce() public {
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        coordinator.fixValidatorEvidence(address(evidence));
        vm.prank(owner);
        vm.expectRevert(STCoordinator.InvalidConfiguration.selector);
        coordinator.fixValidatorEvidence(stranger);
        _anchor();
        assertEq(coordinator.validatorEvidence(), address(evidence));
        vm.prank(owner);
        vm.expectRevert(STCoordinator.InvalidConfiguration.selector);
        coordinator.fixValidatorEvidence(address(evidence));
    }

    function testEvidenceLookalikeIsNotAnchored() public {
        _anchor();
        STValidatorEvidence other = new STValidatorEvidence(coordinator, GENESIS, DEPLOYMENT);
        // Resolve the policy getter before arming the next external-call expectation.
        ValidatorEvidenceActivation.Record memory record = _record();
        bytes32 digest = ValidatorEvidenceActivation.digest(record);
        vm.expectRevert(STValidatorEvidence.Unanchored.selector);
        other.publishActivation(record, _signature(), _signature());
        assertEq(other.activation(digest).publishedBlock, 0);
        assertEq(evidence.activation(digest).publishedBlock, 0);
        assertEq(coordinator.validatorEvidence(), address(evidence));
        assertEq(_publish(record), digest, "same record is valid on the anchored companion");
        assertEq(evidence.activation(digest).publishedBlock, block.number);
        assertEq(other.activation(digest).publishedBlock, 0);
    }

    function testEvidenceActivationExactConsentAndReadback() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        bytes32 digest = _publish(record);
        STValidatorEvidence.Activation memory stored = evidence.activation(digest);
        assertEq(
            ValidatorEvidenceActivation.payload(stored.record), ValidatorEvidenceActivation.payload(record)
        );
        assertEq(stored.publishedBlock, block.number);
        assertEq(stored.record.nativeBlock, 9_999, "never order native and EVM heights numerically");
        vm.expectRevert(STValidatorEvidence.AlreadyPublished.selector);
        evidence.publishActivation(record, _signature(), _signature());
    }

    function testEvidenceActivationWrongContractDomainBeforePrecompiles() public {
        _anchor();
        for (uint256 field; field < 7; ++field) {
            ValidatorEvidenceActivation.Record memory record = _record();
            if (field == 0) record.domain.chainId++;
            else if (field == 1) record.domain.genesisHash ^= bytes32(uint256(1));
            else if (field == 2) record.domain.netuid++;
            else if (field == 3) record.domain.coordinator = stranger;
            else if (field == 4) record.domain.settlementVault = stranger;
            else if (field == 5) record.domain.deploymentIdHash ^= bytes32(uint256(1));
            else record.domain.policyHash ^= bytes32(uint256(1));
            vm.expectRevert(STValidatorEvidence.InvalidActivation.selector);
            evidence.publishActivation(record, _signature(), _signature());
        }
    }

    function testEvidenceActivationCannotBackdateOrObserveItsPublicationBlock() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        record.evmBlock = uint64(block.number);
        vm.expectRevert(STValidatorEvidence.InvalidActivation.selector);
        evidence.publishActivation(record, _signature(), _signature());
        record = _record();
        vm.roll(_end(0) + 1);
        vm.expectRevert(STValidatorEvidence.InvalidActivation.selector);
        evidence.publishActivation(record, _signature(), _signature());
    }

    function testEvidenceActivationRequiresBothKeyConsents() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        for (uint256 role; role < 2; ++role) {
            vm.expectRevert(STValidatorEvidence.InvalidActivation.selector);
            evidence.publishActivation(
                record, role == 0 ? bytes("") : _signature(), role == 1 ? bytes("") : _signature()
            );
        }
        bytes32 digest = ValidatorEvidenceActivation.digest(record);
        vm.mockCall(
            IED25519VERIFY_ADDRESS,
            abi.encodeWithSignature(
                "verify(bytes32,bytes32,bytes32,bytes32)",
                digest,
                record.vpk,
                bytes32(uint256(11)),
                bytes32(uint256(12))
            ),
            abi.encode(true)
        );
        vm.mockCall(
            ISR25519VERIFY_ADDRESS,
            abi.encodeWithSignature(
                "verify(bytes32,bytes32,bytes32,bytes32)",
                digest,
                record.hotkey,
                bytes32(uint256(11)),
                bytes32(uint256(12))
            ),
            abi.encode(false)
        );
        vm.expectRevert(STValidatorEvidence.InvalidActivation.selector);
        evidence.publishActivation(record, _signature(), _signature());
        assertEq(evidence.activation(digest).publishedBlock, 0);
    }

    function testEvidenceClosedCensusNeedsNoPayoutRootOrOperatorPermission() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        _publish(record);
        vm.roll(_end(0) + 1);
        ValidatorEvidence.Header memory header = _header(record);
        bytes32 slot = _commit(header);
        STValidatorEvidence.Commitment memory stored = evidence.commitment(slot);
        assertEq(ValidatorEvidence.payload(stored.header), ValidatorEvidence.payload(header));
        assertEq(stored.publishedBlock, block.number);
        (bytes32 payoutRoot, bytes32 artifactHash,,) = coordinator.rootCommitments(0, NO1);
        assertEq(payoutRoot, bytes32(0));
        assertEq(artifactHash, bytes32(0));
        assertEq(uint256(vault.entitlement(0, NO1).status), uint256(STSettlementVault.EpochStatus.Unset));
    }

    function testEvidenceClosedCensusRefusesOpenOrWrongBoundaryWindow() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        _publish(record);
        ValidatorEvidence.Header memory header = _header(record);
        vm.expectRevert(STValidatorEvidence.InvalidEvidence.selector);
        evidence.commitEvidence(header, _signature(), _signature());
        vm.roll(_end(0) + 1);
        header.boundaryBlock--;
        vm.expectRevert(STValidatorEvidence.InvalidEvidence.selector);
        evidence.commitEvidence(header, _signature(), _signature());
    }

    function testEvidenceUnknownActivationCannotOccupySlot() public {
        _anchor();
        ValidatorEvidence.Header memory header = _header(_record());
        vm.roll(_end(0) + 1);
        vm.expectRevert(STValidatorEvidence.InvalidEvidence.selector);
        evidence.commitEvidence(header, _signature(), _signature());
        assertEq(evidence.commitment(ValidatorEvidence.slotKey(header)).publishedBlock, 0);
    }

    function testEvidenceDuplicateAndEquivocationCannotRewriteSlot() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        _publish(record);
        vm.roll(_end(0) + 1);
        ValidatorEvidence.Header memory header = _header(record);
        bytes32 slot = _commit(header);
        bytes32 original = ValidatorEvidence.digest(header);
        vm.expectRevert(STValidatorEvidence.AlreadyCommitted.selector);
        evidence.commitEvidence(header, _signature(), _signature());
        header.payloadHash ^= bytes32(uint256(1));
        _approve(ValidatorEvidence.digest(header), header.vpk, header.hotkey);
        vm.expectRevert(STValidatorEvidence.AlreadyCommitted.selector);
        evidence.commitEvidence(header, _signature(), _signature());
        assertEq(ValidatorEvidence.digest(evidence.commitment(slot).header), original);
    }

    function testEvidenceLaterAuditUsesSeparateDeterministicSlot() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        _publish(record);
        vm.roll(_end(0) + 2);
        ValidatorEvidence.Header memory header = _header(record);
        bytes32 terminalSlot = _commit(header);
        header.kind = ValidatorEvidence.DEPOSIT_AUDIT;
        header.subject = ValidatorEvidence.Subject({observationEpoch: 1, nativeEpoch: 91});
        header.boundaryBlock = uint64(_end(0) + 1);
        header.payloadHash = keccak256("later-deposit-audit-unsigned-payload");
        bytes32 auditSlot = _commit(header);
        assertTrue(auditSlot != terminalSlot);
        assertEq(evidence.commitment(terminalSlot).header.kind, ValidatorEvidence.CLOSED_CENSUS);
        assertEq(evidence.commitment(auditSlot).header.kind, ValidatorEvidence.DEPOSIT_AUDIT);
        header.subject.observationEpoch = 2;
        vm.expectRevert(STValidatorEvidence.InvalidEvidence.selector);
        evidence.commitEvidence(header, _signature(), _signature());
    }

    function testEvidenceIndependentOperatorActivationsKeepDistinctSlots() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory first = _record();
        ValidatorEvidenceActivation.Record memory second = _record();
        second.noId = SafeCast.toUint64(NO2);
        second.vpk = keccak256("second-operator-scoped-vpk");
        _publish(first);
        _publish(second);
        vm.roll(_end(0) + 1);
        bytes32 firstSlot = _commit(_header(first));
        bytes32 secondSlot = _commit(_header(second));
        assertTrue(firstSlot != secondSlot);
        ValidatorEvidence.Header memory wrong = _header(first);
        wrong.noId = SafeCast.toUint64(NO2);
        vm.expectRevert(STValidatorEvidence.InvalidEvidence.selector);
        evidence.commitEvidence(wrong, _signature(), _signature());
    }

    function testEvidenceRemainsAvailableAfterPauseAndMissedRootDeadline() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        _publish(record);
        _close(0, NO1);
        vm.roll(_end(0) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(0, NO1);
        assertEq(uint256(vault.entitlement(0, NO1).status), uint256(STSettlementVault.EpochStatus.RootMissed));
        vm.prank(guardian);
        coordinator.setPaused(true);
        bytes32 slot = _commit(_header(record));
        assertEq(evidence.commitment(slot).header.noId, NO1);
        assertTrue(coordinator.paused());
    }

    function testEvidenceAnchorConsumesOnlyTheFirstReservedStorageSlot() public {
        _deposit(NO1, 77, 0);
        uint256 reserved = coordinator.campaignReserved();
        bytes32 tailMarker = keccak256("reserved-gap-tail-marker");
        vm.store(address(coordinator), bytes32(uint256(62)), tailMarker);
        _anchor();
        assertEq(
            vm.load(address(coordinator), bytes32(uint256(23))), bytes32(uint256(uint160(address(evidence))))
        );
        assertEq(vm.load(address(coordinator), bytes32(uint256(62))), tailMarker);
        assertEq(coordinator.campaignReserved(), reserved);
        assertEq(coordinator.epochDeposits(0, NO1), 77);
        assertEq(coordinator.operatorAt(NO1, 0).coldkey, COLD1);
        assertEq(address(coordinator.settlementVault()), address(vault));
        assertEq(address(coordinator.reserveSink()), address(sink));
        assertEq(coordinator.policyAt(0).epochBlocks, EPOCH_BLOCKS);
    }

    function testEvidencePublicationDoesNotChangeFinalizedPayments() public {
        _anchor();
        ValidatorEvidenceActivation.Record memory record = _record();
        _publish(record);
        _accrue(NO1, 1_000);
        _close(0, NO1);
        (bytes32 root, bytes32[] memory proof) = _singleLeaf(PROVIDER1, 10_000);
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(0, NO1, root, keccak256("payout-artifact"));
        vm.roll(_end(0) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(0, NO1);
        STSettlementVault.Entitlement memory before = vault.entitlement(0, NO1);
        uint256 captured = vault.totalCaptured();
        uint256 liability = vault.outstandingLiability();
        _commit(_header(record));
        assertEq(abi.encode(vault.entitlement(0, NO1)), abi.encode(before));
        assertEq(vault.totalCaptured(), captured);
        assertEq(vault.outstandingLiability(), liability);
        vault.claim(0, NO1, PROVIDER1, 10_000, proof);
        assertEq(vault.totalPaid(), before.total);
    }
}
