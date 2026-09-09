//go:build linux || darwin

// Campaign admission joins the existing activation-anchored funded horizon
// to independently enforced source, replay, capture and upload capacities.
// Block equivalents forecast workload; they do not promise chain speed or
// unlimited preparation, archive latency or filesystem amplification.
package main

import (
	"errors"
	"fmt"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Values are per validator/operator source except the explicitly combined
// capture/history fields. No count or byte allowance causes preallocation.
type runtimeEvidenceSourceCapacity struct {
	span                 uint64
	closed               uint64
	native               uint64
	trails               uint64
	records              uint64
	decisionAttempts     uint64
	captureFiles         uint64
	historyBytes         uint64
	recordBytes          uint64
	proofBytes           uint64
	storageBytes         uint64
	storageFiles         uint64
	recordChunks         uint64
	recordPages          uint64
	proofChunks          uint64
	proofPages           uint64
	objectsPerHour       uint64
	bytesPerHour         uint64
	retryRequestsPerHour uint64
}

// Count partial boundary windows and process restarts explicitly. A failed
// measurement can retain observations before publishing any funded header;
// the relay allowance therefore cannot refund or remove those source bytes.
func requiredRuntimeEvidenceSourceCapacity(cfg *ResolvedConfig, bounds validatorpkg.ReleaseEvidenceV2Bounds) (runtimeEvidenceSourceCapacity, error) {
	var result runtimeEvidenceSourceCapacity
	span, closed, native, err := evidenceRelayConfiguredHorizon(cfg)
	if err != nil {
		return result, err
	}
	if span == 0 || cfg.Config.Topology.Operators != 2 || cfg.Config.Topology.Validators != 2 || cfg.Config.Topology.Miners <= 0 || cfg.Policy.Verify.HardSeedPerMinutePerSource <= 0 || cfg.Policy.Verify.HardActiveTrailsPerSource <= 0 || cfg.Policy.Verify.TrailDepth <= 0 {
		return result, errors.New("source capacity has no admitted complete campaign geometry")
	}
	operators := uint64(cfg.Config.Topology.Operators)
	if err := bounds.Validate(operators); err != nil {
		return result, err
	}
	result.span, result.closed, result.native = span, closed, native
	var arithmeticErr error
	add := func(values ...uint64) uint64 {
		var result uint64
		for _, value := range values {
			next, ok := checkedAdd(result, value)
			if !ok {
				arithmeticErr = errors.New("source capacity addition overflows")
				return 0
			}
			result = next
		}
		return result
	}
	multiply := func(values ...uint64) uint64 {
		result := uint64(1)
		for _, value := range values {
			next, ok := checkedMul(result, value)
			if !ok {
				arithmeticErr = errors.New("source capacity multiplication overflows")
				return 0
			}
			result = next
		}
		return result
	}
	ceil := func(value, divisor uint64) uint64 {
		if divisor == 0 {
			arithmeticErr = errors.New("source capacity divisor is zero")
			return 0
		}
		result := value / divisor
		if value%divisor != 0 {
			result = add(result, 1)
		}
		return result
	}
	restarts := uint64(validatorProcessRestartLimit)
	seconds := multiply(span, cfg.Public.Chain.ExpectedBlockSeconds)
	// Initial partial minute plus one after each allowed restart; admitted
	// active trails are retained even when their process is interrupted.
	minutes := add(ceil(seconds, 60), 1, restarts)
	result.trails = add(multiply(minutes, uint64(cfg.Policy.Verify.HardSeedPerMinutePerSource)), multiply(add(restarts, 1), uint64(cfg.Policy.Verify.HardActiveTrailsPerSource)))
	result.records = multiply(result.trails, uint64(cfg.Policy.Verify.TrailDepth))
	result.decisionAttempts = multiply(add(native, restarts), uint64(validatorpkg.ReleaseSteeringFailureLimit))
	// MaxProviders belongs to EACH operator, not their union. Actual launch
	// miners are a lower count; no runtime admission reduces this configured cap.
	result.captureFiles = add(multiply(result.decisionAttempts, operators, bounds.MaxProviders), multiply(restarts, operators))
	result.recordBytes = multiply(bounds.Disk.MaxRecordCount, bounds.Disk.MaxRecordBytes)
	result.proofBytes = multiply(bounds.Disk.MaxTrailCount, bounds.Replay.MaxProofBytes)
	// Storage reserves two complete raw/projection copies plus the existing
	// legacy/control owners. Physical backend caps still refuse amplification.
	result.storageBytes = add(multiply(2, add(bounds.Disk.MaxRawRecordBytes, bounds.Disk.MaxProofBytes)), bounds.Disk.MaxLegacyBytes, bounds.Persistence.MaxSnapshotBytes, bounds.Persistence.MaxJournalBytes)
	// The public writer packs whole rows. Every nonfinal chunk contains more
	// than chunk-row bytes, so a division by nominal chunk size is insufficient.
	streamCapacity := func(kind string, stream validatorpkg.AttemptStreamV2Bounds, row uint64) (uint64, uint64) {
		if row == 0 || stream.MaxChunkBytes < row {
			arithmeticErr = errors.New("source stream cannot carry one whole canonical row")
			return 0, 0
		}
		capacity, err := stream.WriterPageCapacity(kind, row)
		if err != nil {
			arithmeticErr = err
			return 0, 0
		}
		chunks := min(stream.MaxItems, ceil(stream.MaxDataBytes, stream.MaxChunkBytes-row+1))
		return chunks, ceil(chunks, capacity)
	}
	result.recordChunks, result.recordPages = streamCapacity(validatorpkg.AttemptStreamV2Records, bounds.Cut.Records, bounds.Replay.MaxRecordBytes)
	result.proofChunks, result.proofPages = streamCapacity(validatorpkg.AttemptStreamV2Proofs, bounds.Cut.Proofs, bounds.Replay.MaxProofBytes)
	if arithmeticErr != nil {
		return runtimeEvidenceSourceCapacity{}, arithmeticErr
	}
	result.storageFiles = add(multiply(2, add(bounds.Cut.Records.MaxChunks, bounds.Cut.Proofs.MaxChunks, bounds.Cut.Records.MaxPages, bounds.Cut.Proofs.MaxPages)), 8)
	// The real shared head owner charges every response byte eightfold.
	// It is one aggregate per decision, not 8 MiB times every client.
	keyBytes := bounds.MaxControlBytes / 8
	artifactObservationBytes := min(bounds.MaxArtifactBytes, bounds.MaxControlBytes/8)
	envelopeBytes := min(bounds.MaxControlBytes, validatorpkg.ReleaseMeasurementEnvelopeV2MaximumBytes)
	perDecision := add(bounds.MaxArtifactBytes, envelopeBytes, keyBytes, multiply(operators, artifactObservationBytes))
	inputBytes := multiply(native, operators, bounds.MaxInputJournalBytes)
	closureBytes := multiply(closed, bounds.MaxClosureBytes)
	crashBytes := multiply(restarts, add(multiply(operators, bounds.MaxInputJournalBytes), bounds.MaxClosureBytes))
	// Startup charges this union to one owner; ordinary current-intent update
	// may reread one artifact/envelope. The store is independently control-bound.
	result.historyBytes = add(inputBytes, closureBytes, crashBytes, bounds.MaxControlBytes, multiply(result.decisionAttempts, perDecision), bounds.MaxArtifactBytes, envelopeBytes)
	// The same immutable subject may be retried without a new publication.
	// Additional funded subjects consume the finite total slot allowance.
	preparedAuditBytes := multiply(cfg.Config.ValidatorEvidenceRelay.MaxSlots, artifactObservationBytes)
	result.historyBytes = max(result.historyBytes, preparedAuditBytes)
	// Redis idempotency belongs to one UTC-hour bucket. Catch-up can place
	// the whole retained source census in that bucket, not just fresh epochs.
	// Within a settlement epoch, native cuts keep FirstSequence unchanged:
	// full content-addressed chunks repeat exactly; only the trailing chunk
	// and backwards-linked metadata pages can change on a longer prefix.
	prefixCuts := add(result.decisionAttempts, closed, restarts)
	recordStreamBytes := add(bounds.Disk.MaxRawRecordBytes, bounds.Disk.MaxRecordCount)
	proofStreamBytes := bounds.Disk.MaxProofBytes
	fullChunks := add(ceil(recordStreamBytes, bounds.Cut.Records.MaxChunkBytes-bounds.Replay.MaxRecordBytes+1), ceil(proofStreamBytes, bounds.Cut.Proofs.MaxChunkBytes-bounds.Replay.MaxProofBytes+1))
	dataObjects := add(fullChunks, multiply(prefixCuts, 2))
	dataBytes := add(recordStreamBytes, proofStreamBytes, multiply(prefixCuts, add(bounds.Cut.Records.MaxChunkBytes, bounds.Cut.Proofs.MaxChunkBytes)))
	pageObjects := add(bounds.Cut.Records.MaxPages, bounds.Cut.Proofs.MaxPages, 3)
	pageBytes := add(multiply(bounds.Cut.Records.MaxPages, bounds.Cut.Records.MaxPageBytes), multiply(bounds.Cut.Proofs.MaxPages, bounds.Cut.Proofs.MaxPageBytes), bounds.Cut.Records.MaxManifestBytes, bounds.Cut.Proofs.MaxManifestBytes, bounds.Cut.MaxHeaderBytes)
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		return result, err
	}
	nativePerHour := add(ceil(3600, multiply(work.nativeCadence, cfg.Public.Chain.ExpectedBlockSeconds)), 1)
	poll := validatorPollSeconds(cfg)
	if poll < 1 || poll > 60 {
		return result, errors.New("source upload capacity has no admitted validator poll interval")
	}
	// Settlement refresh retries transient failures at each actual poll; it
	// does not use the native ten-failure ceiling. Include its initial read
	// and restart owners as well as every bounded native attempt.
	operations := add(ceil(3600, uint64(poll)), 1, restarts, multiply(add(nativePerHour, restarts), uint64(validatorpkg.ReleaseSteeringFailureLimit)))
	// Each funded member uploads payload, census and signed header. Before a
	// completed closed locator exists, failed signing/upload retries may create
	// new signature bytes; include both members for every admitted operation.
	metadataObjects := add(multiply(add(cfg.Config.ValidatorEvidenceRelay.MaxSlots, restarts), 3), multiply(operations, operators))
	metadataBytes := multiply(metadataObjects, max(bounds.Cut.MaxHeaderBytes, bounds.Cut.Records.MaxPageBytes, bounds.Cut.Proofs.MaxPageBytes, bounds.Cut.Records.MaxManifestBytes, bounds.Cut.Proofs.MaxManifestBytes))
	result.objectsPerHour = add(dataObjects, multiply(prefixCuts, pageObjects), metadataObjects)
	result.bytesPerHour = add(dataBytes, multiply(prefixCuts, pageBytes), metadataBytes)
	attackRate := cfg.Config.Scenarios.Adversaries.MaximumOperatorRequestsPerSec
	if attackRate <= 0 || attackRate > 20 {
		return result, errors.New("source upload capacity has no bounded adversary admission")
	}
	result.retryRequestsPerHour = add(multiply(operations, result.objectsPerHour), multiply(uint64(attackRate), 3600))
	if arithmeticErr != nil {
		return runtimeEvidenceSourceCapacity{}, arithmeticErr
	}
	return result, nil
}

// This is called only by the real full-campaign owner before preparation or
// spending. Setup-only configuration/rendering keeps its existing admission.
func validateRuntimeEvidenceSourceCapacity(cfg *ResolvedConfig) error {
	if cfg == nil || cfg.Config == nil || len(cfg.Config.ValidatorEvidenceV2) != cfg.Config.Topology.Validators || cfg.Config.Topology.Validators == 0 || len(cfg.Config.Artifacts.ReservedAttemptUploads) != cfg.Config.Topology.Operators {
		return errors.New("source capacity has no complete validator/operator owner census")
	}
	seen := map[uint64]bool{}
	if _, err := campaignEvidenceLimitsForConfig(cfg); err != nil {
		return fmt.Errorf("source archive metadata capacity: %w", err)
	}
	for _, source := range cfg.Config.ValidatorEvidenceV2 {
		if source.ValidatorID == 0 || source.ValidatorID > uint64(cfg.Config.Topology.Validators) || seen[source.ValidatorID] || source.Evidence.Schema != validatorpkg.ReleaseEvidenceV2ConfigSchema || len(source.Evidence.Operators) != cfg.Config.Topology.Operators {
			return errors.New("source capacity validator or operator ownership differs")
		}
		seen[source.ValidatorID] = true
		for index, operator := range source.Evidence.Operators {
			if operator.NoID != uint64(index+1) {
				return errors.New("source capacity original operator census differs")
			}
		}
		bounds := source.Evidence.Bounds
		minimum, err := requiredRuntimeEvidenceSourceCapacity(cfg, bounds)
		if err != nil {
			return fmt.Errorf("validator %d source capacity: %w", source.ValidatorID, err)
		}
		row, ok := checkedAdd(bounds.Disk.MaxRecordBytes, 1)
		if !ok || bounds.Replay.MaxRecordBytes < row {
			return errors.New("source replay row omits the canonical record newline")
		}
		streamBytes, ok := checkedAdd(bounds.Disk.MaxRawRecordBytes, bounds.Disk.MaxRecordCount)
		if !ok {
			return errors.New("source canonical record stream bytes overflow")
		}
		for _, field := range []struct {
			name            string
			actual, minimum uint64
		}{
			{name: "disk.maxtrailcount", actual: bounds.Disk.MaxTrailCount, minimum: minimum.trails},
			{name: "disk.maxrecordcount", actual: bounds.Disk.MaxRecordCount, minimum: minimum.records},
			{name: "disk.maxrawrecordbytes", actual: bounds.Disk.MaxRawRecordBytes, minimum: minimum.recordBytes},
			{name: "disk.maxproofbytes", actual: bounds.Disk.MaxProofBytes, minimum: minimum.proofBytes},
			{name: "disk.maxstoragebytes", actual: bounds.Disk.MaxStorageBytes, minimum: minimum.storageBytes},
			{name: "disk.maxstoragefiles", actual: bounds.Disk.MaxStorageFiles, minimum: minimum.storageFiles},
			{name: "cut.records.maxdatabytes", actual: bounds.Cut.Records.MaxDataBytes, minimum: streamBytes},
			{name: "cut.records.maxchunks", actual: bounds.Cut.Records.MaxChunks, minimum: minimum.recordChunks},
			{name: "cut.records.maxpages", actual: bounds.Cut.Records.MaxPages, minimum: minimum.recordPages},
			{name: "cut.proofs.maxchunks", actual: bounds.Cut.Proofs.MaxChunks, minimum: minimum.proofChunks},
			{name: "cut.proofs.maxpages", actual: bounds.Cut.Proofs.MaxPages, minimum: minimum.proofPages},
			{name: "replay.maxscratchbytes", actual: bounds.Replay.MaxScratchBytes, minimum: minimum.storageBytes},
			{name: "replay.maxscratchfiles", actual: bounds.Replay.MaxScratchFiles, minimum: minimum.storageFiles},
			{name: "max_capture_files", actual: bounds.CaptureFileLimit(), minimum: minimum.captureFiles},
			{name: "max_history_bytes", actual: bounds.MaxHistoryBytes, minimum: minimum.historyBytes},
			{name: "max_providers", actual: bounds.MaxProviders, minimum: uint64(cfg.Config.Topology.Miners)},
			{name: "max_head_entries", actual: bounds.MaxHeadEntries, minimum: uint64(cfg.Config.Topology.fleetCandidateMiners())},
		} {
			if field.actual < field.minimum {
				return fmt.Errorf("validator %d %s=%d is below campaign source minimum %d", source.ValidatorID, field.name, field.actual, field.minimum)
			}
		}
		for index, destination := range cfg.Config.Artifacts.ReservedAttemptUploads {
			if destination.Admission.ReplicaNoID != uint64(index+1) || destination.Admission.MaximumOwners < uint64(cfg.Config.Topology.Validators*cfg.Config.Topology.Operators) || source.Evidence.UploadIntentSeconds > destination.Admission.MaximumIntentSeconds {
				return errors.New("source publication owner or intent capacity differs")
			}
			blocks, ok := checkedMul(destination.Admission.BlocksPerRange, destination.Admission.MaximumRanges)
			if !ok || blocks < minimum.span {
				return errors.New("source admission history cannot retain the funded block horizon")
			}
			if destination.Budget.ObjectsPerHour < minimum.objectsPerHour || destination.Budget.BytesPerHour < minimum.bytesPerHour || destination.Budget.RetryRequestsPerHour < minimum.retryRequestsPerHour {
				return fmt.Errorf("replica %d protected publication capacity is below the campaign source/retry workload", index+1)
			}
		}
	}
	return nil
}
