// A resource-only successor preserves economic authority and every retained
// prefix. Aggregate history limits may grow; resident work and membership do not.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

const ProductionCapacityRevisionSchema = "urnetwork-validator-production-capacity-revision-v1"

// Forecast counts describe retained and potential local evidence, not funded
// actions, active owners, signing allowances or uploaded objects. A successor
// must retain the original economic approval verbatim except its config hash.
type ProductionCapacitySource struct {
	Identity           AttemptLedgerIdentity `json:"identity" yaml:"identity"`
	Head               AttemptLedgerHead     `json:"head" yaml:"head"`
	CensusSha256       string                `json:"census_sha256" yaml:"census_sha256"`
	FutureRecords      uint64                `json:"future_records" yaml:"future_records"`
	FutureTrails       uint64                `json:"future_trails" yaml:"future_trails"`
	FutureRecordBytes  uint64                `json:"future_record_bytes" yaml:"future_record_bytes"`
	FutureProofBytes   uint64                `json:"future_proof_bytes" yaml:"future_proof_bytes"`
	StorageBytes       uint64                `json:"storage_bytes" yaml:"storage_bytes"`
	StorageFiles       uint64                `json:"storage_files" yaml:"storage_files"`
	FutureStorageBytes uint64                `json:"future_storage_bytes" yaml:"future_storage_bytes"`
	FutureStorageFiles uint64                `json:"future_storage_files" yaml:"future_storage_files"`
}

// The existing independent owner signs this field through the full config.
// Its lineage is transitive through immutable production-authority bundles;
// it never replaces their old byte/signature interpretation or resets a count.
type ProductionCapacityRevision struct {
	Schema                    string                     `json:"schema" yaml:"schema"`
	PredecessorConfigHash     string                     `json:"predecessor_config_hash" yaml:"predecessor_config_hash"`
	PredecessorApprovalSha256 string                     `json:"predecessor_approval_sha256" yaml:"predecessor_approval_sha256"`
	EconomicApprovalSha256    string                     `json:"economic_approval_sha256" yaml:"economic_approval_sha256"`
	ValidThroughNativeBlock   uint64                     `json:"valid_through_native_block" yaml:"valid_through_native_block"`
	ValidThroughNativeEpoch   uint64                     `json:"valid_through_native_epoch" yaml:"valid_through_native_epoch"`
	Margin                    uint64                     `json:"margin" yaml:"margin"`
	RetainedHistoryBytes      uint64                     `json:"retained_history_bytes" yaml:"retained_history_bytes"`
	FutureHistoryBytes        uint64                     `json:"future_history_bytes" yaml:"future_history_bytes"`
	RetainedCaptureFiles      uint64                     `json:"retained_capture_files" yaml:"retained_capture_files"`
	FutureCaptureFiles        uint64                     `json:"future_capture_files" yaml:"future_capture_files"`
	Sources                   []ProductionCapacitySource `json:"sources" yaml:"sources"`
}

// Nested numeric syntax uses the same exact-decimal admission as bounds,
// including the narrower uint16 fields of the retained ledger identity.
func (self *ProductionCapacityRevision) UnmarshalYAML(node *yaml.Node) error {
	type plain ProductionCapacityRevision
	var decoded plain
	if err := validateReleaseEvidenceV2YAML(node, reflect.TypeFor[ProductionCapacityRevision]()); err != nil {
		return err
	}
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*self = ProductionCapacityRevision(decoded)
	return nil
}

// Null bypasses custom YAML unmarshaling, so the public loader also validates
// the original optional node. Absence retains the historical JSON/hash path.
func validateProductionCapacityDocument(raw []byte) error {
	return validateReleaseEvidenceV2Document(raw, "production_capacity_revision", reflect.TypeFor[ProductionCapacityRevision]())
}

// Hashing clears only the new complete-config commitment. Every spend,
// destination, runtime, window and policy field remains economic authority.
func productionCapacityEconomicHash(approval OwnerRecycleApproval) string {
	approval.ConfigHash = [32]byte{}
	raw, _ := json.Marshal(approval)
	return attemptHex32(sha256.Sum256(raw))
}

// Explicitly enumerate aggregate dimensions. All other fields, including
// per-object sizes, page sizes, participants and active provider counts, stay
// exact. New field additions therefore do not silently become revisable.
func productionCapacityAggregate(name string) bool {
	switch name {
	case "Disk.MaxRecordCount", "Disk.MaxTrailCount", "Disk.MaxRawRecordBytes", "Disk.MaxStorageBytes", "Disk.MaxStorageFiles", "Disk.MaxProofBytes",
		"Cut.Records.MaxItems", "Cut.Records.MaxDataBytes", "Cut.Records.MaxChunks", "Cut.Records.MaxPages",
		"Cut.Proofs.MaxItems", "Cut.Proofs.MaxDataBytes", "Cut.Proofs.MaxChunks", "Cut.Proofs.MaxPages",
		"Replay.MaxTrails", "Replay.MaxScratchBytes", "Replay.MaxScratchFiles", "MaxHistoryBytes", "MaxCaptureFiles":
		return true
	}
	return false
}

// Both producer and reader capacities must remain admitted by their existing
// implementations; monotonic aggregate allowance is not a new wire profile.
func validateProductionCapacityBounds(before, after ReleaseEvidenceV2Bounds, operators uint64) error {
	if err := errors.Join(before.Validate(operators), after.Validate(operators)); err != nil {
		return err
	}
	before.MaxCaptureFiles, after.MaxCaptureFiles = before.CaptureFileLimit(), after.CaptureFileLimit()
	var walk func(string, reflect.Value, reflect.Value) error
	walk = func(prefix string, old, next reflect.Value) error {
		if old.Kind() == reflect.Struct {
			for index := 0; index < old.NumField(); index++ {
				name := old.Type().Field(index).Name
				if prefix != "" {
					name = prefix + "." + name
				}
				if err := walk(name, old.Field(index), next.Field(index)); err != nil {
					return err
				}
			}
			return nil
		}
		if old.Kind() != reflect.Uint64 || next.Kind() != reflect.Uint64 ||
			productionCapacityAggregate(prefix) && next.Uint() < old.Uint() ||
			!productionCapacityAggregate(prefix) && next.Uint() != old.Uint() {
			return fmt.Errorf("capacity revision changes a fixed dimension or reduces retained capacity: %s", prefix)
		}
		return nil
	}
	return walk("", reflect.ValueOf(before), reflect.ValueOf(after))
}

// The minimum includes the full retained prefix plus separately reviewed
// future evidence. Arithmetic is checked before allocation or storage work.
func productionCapacityMargin(retained, future, margin uint64) (uint64, error) {
	if margin != 2 || future > ^uint64(0)-retained || retained+future > ^uint64(0)/margin {
		return 0, errors.New("capacity revision margin or arithmetic is invalid")
	}
	return (retained + future) * margin, nil
}

// This is the actual signed-history admission boundary, not a shape-only
// report. The new signature grants resource headroom but no new economic act.
func validateProductionCapacityTransition(original, current *ReleaseConfig) error {
	if original == nil || current == nil {
		return errors.New("capacity transition has no original and current configuration")
	}
	if reflect.DeepEqual(original.EvidenceV2.Bounds, current.EvidenceV2.Bounds) && reflect.DeepEqual(original.ProductionCapacityRevision, current.ProductionCapacityRevision) {
		return nil
	}
	prior, err := ownerRecycleProductionApproval(original)
	if err != nil {
		return err
	}
	next, err := ownerRecycleProductionApproval(current)
	if err != nil {
		return err
	}
	return validateProductionCapacityRevision(original, current, prior.Approval, next.Approval)
}

// The preview uses this same arithmetic without inventing a signature. Only
// the loader's authenticated envelope path may admit the resulting successor.
func validateProductionCapacityRevision(original, current *ReleaseConfig, prior, next OwnerRecycleApproval) error {
	if original == nil || current == nil || productionEconomicSelection(original) == nil || prior.Production == nil || next.Production == nil {
		return errors.New("capacity revision has no authenticated production predecessor")
	}
	revision := current.ProductionCapacityRevision
	if revision == nil || revision.Schema != ProductionCapacityRevisionSchema || revision.Margin != 2 ||
		revision.PredecessorConfigHash != attemptHex32(prior.ConfigHash) || revision.PredecessorApprovalSha256 != productionEconomicSelection(original).Approval.SHA256 ||
		revision.EconomicApprovalSha256 != productionCapacityEconomicHash(prior) || productionCapacityEconomicHash(next) != revision.EconomicApprovalSha256 ||
		revision.ValidThroughNativeBlock != prior.ValidThroughNativeBlock || revision.ValidThroughNativeEpoch != prior.Production.ValidThroughNativeEpoch ||
		len(revision.Sources) != len(original.EvidenceV2.Operators) || len(revision.Sources) != len(original.Operators) || len(revision.Sources) == 0 {
		return errors.New("capacity revision does not retain the exact preceding config, economic approval and complete source census")
	}
	if err := validateProductionCapacityBounds(original.EvidenceV2.Bounds, current.EvidenceV2.Bounds, uint64(len(revision.Sources))); err != nil {
		return err
	}
	old, bounds := original.EvidenceV2.Bounds, current.EvidenceV2.Bounds
	var futureStorageBytes, futureStorageFiles uint64
	for index, source := range revision.Sources {
		identity := source.Identity
		if identity.NoID != original.EvidenceV2.Operators[index].NoID || identity.NoID != original.Operators[index].NoID ||
			identity.DeploymentID != original.DeploymentID || identity.ChainID != original.ChainID || identity.GenesisHash != original.GenesisHash ||
			identity.Netuid != original.Netuid || identity.ValidatorID != original.ValidatorID || source.FutureRecords == 0 || source.FutureTrails == 0 ||
			source.FutureTrails > source.FutureRecords || source.Head.LastSequence > old.Disk.MaxRecordCount || source.Head.RecordBytes > old.Disk.MaxRawRecordBytes || source.Head.TrailCount > old.Disk.MaxTrailCount || source.Head.TrailCount > source.Head.LastSequence {
			return errors.New("capacity revision source differs from retained identity, count or finite future forecast")
		}
		if err := validateAttemptLedgerIdentity(identity, nil); err != nil {
			return err
		}
		if _, err := canonicalAttemptHex32("capacity source census", source.CensusSha256, false); err != nil {
			return err
		}
		if _, err := canonicalAttemptHex32("capacity source prefix", source.Head.Root, source.Head.LastSequence == 0); err != nil {
			return err
		}
		if source.Head.LastSequence == 0 && source.Head != (AttemptLedgerHead{Root: zeroAttemptHash()}) {
			return errors.New("capacity revision changes an empty prefix")
		}
		if source.FutureRecords > ^uint64(0)/old.Disk.MaxRecordBytes || source.FutureRecordBytes < source.FutureRecords*old.Disk.MaxRecordBytes ||
			source.FutureTrails > ^uint64(0)/old.Replay.MaxProofBytes || source.FutureProofBytes < source.FutureTrails*old.Replay.MaxProofBytes ||
			source.Head.TrailCount > ^uint64(0)/old.Replay.MaxProofBytes {
			return errors.New("capacity revision future rows or proof bytes do not fit their own bounds")
		}
		for _, capacity := range []struct {
			name                        string
			retained, future, available uint64
		}{
			{name: "record count", retained: source.Head.LastSequence, future: source.FutureRecords, available: bounds.Disk.MaxRecordCount},
			{name: "trail count", retained: source.Head.TrailCount, future: source.FutureTrails, available: bounds.Disk.MaxTrailCount},
			{name: "record bytes", retained: source.Head.RecordBytes, future: source.FutureRecordBytes, available: bounds.Disk.MaxRawRecordBytes},
			{name: "proof bytes", retained: source.Head.TrailCount * old.Replay.MaxProofBytes, future: source.FutureProofBytes, available: bounds.Disk.MaxProofBytes},
			{name: "physical storage bytes", retained: source.StorageBytes, future: source.FutureStorageBytes, available: bounds.Disk.MaxStorageBytes},
			{name: "physical storage files", retained: source.StorageFiles, future: source.FutureStorageFiles, available: bounds.Disk.MaxStorageFiles},
		} {
			minimum, err := productionCapacityMargin(capacity.retained, capacity.future, revision.Margin)
			if err != nil || capacity.available < minimum {
				return errors.Join(fmt.Errorf("capacity revision %s lacks the retained plus future margin", capacity.name), err)
			}
		}
		if source.StorageFiles == 0 || source.FutureStorageFiles == 0 || source.FutureProofBytes > ^uint64(0)-source.FutureRecordBytes || source.FutureStorageBytes < source.FutureRecordBytes+source.FutureProofBytes {
			return errors.New("capacity revision omits retained storage or future record/proof footprint")
		}
		if source.FutureStorageBytes > ^uint64(0)-futureStorageBytes || source.FutureStorageFiles > ^uint64(0)-futureStorageFiles {
			return errors.New("capacity revision aggregate future storage overflows")
		}
		futureStorageBytes += source.FutureStorageBytes
		futureStorageFiles += source.FutureStorageFiles
	}
	if revision.FutureHistoryBytes < futureStorageBytes || revision.FutureCaptureFiles < futureStorageFiles {
		return errors.New("capacity revision aggregate history omits future source storage")
	}
	for _, capacity := range []struct {
		name                        string
		retained, future, available uint64
	}{
		{name: "history bytes", retained: revision.RetainedHistoryBytes, future: revision.FutureHistoryBytes, available: bounds.MaxHistoryBytes},
		{name: "capture files", retained: revision.RetainedCaptureFiles, future: revision.FutureCaptureFiles, available: bounds.CaptureFileLimit()},
	} {
		minimum, err := productionCapacityMargin(capacity.retained, capacity.future, revision.Margin)
		if err != nil || capacity.future == 0 || capacity.available < minimum {
			return errors.Join(fmt.Errorf("capacity revision %s lacks its retained plus future margin", capacity.name), err)
		}
	}
	return validateProductionCapacityGeometry(bounds)
}

// Worst-case chunk/page geometry is computed from the actual writer's page
// encoding. Count, byte, disk and scratch dimensions must agree; no per-row,
// transport message, active provider or resident page authority is enlarged.
func validateProductionCapacityGeometry(bounds ReleaseEvidenceV2Bounds) error {
	ceil := func(total, size uint64) uint64 {
		result := total / size
		if total%size != 0 {
			result++
		}
		return result
	}
	for _, stream := range []struct {
		kind   string
		bounds AttemptStreamV2Bounds
		row    uint64
	}{
		{kind: AttemptStreamV2Records, bounds: bounds.Cut.Records, row: bounds.Replay.MaxRecordBytes},
		{kind: AttemptStreamV2Proofs, bounds: bounds.Cut.Proofs, row: bounds.Replay.MaxProofBytes},
	} {
		if stream.row == 0 || stream.row > stream.bounds.MaxChunkBytes {
			return errors.New("capacity revision row cannot fit its fixed chunk")
		}
		capacity, err := stream.bounds.WriterPageCapacity(stream.kind, stream.row)
		if err != nil {
			return err
		}
		// Every closed non-final chunk consumed at least this many bytes.
		minimum := stream.bounds.MaxChunkBytes - stream.row + 1
		chunks := min(stream.bounds.MaxItems, ceil(stream.bounds.MaxDataBytes, minimum))
		if stream.bounds.MaxChunks < chunks || stream.bounds.MaxPages < ceil(chunks, capacity) {
			return fmt.Errorf("capacity revision %s cannot represent its admitted row/byte allowance", stream.kind)
		}
	}
	if bounds.Replay.MaxScratchBytes < bounds.Disk.MaxStorageBytes || bounds.Replay.MaxScratchFiles < bounds.Disk.MaxStorageFiles {
		return errors.New("capacity revision replay scratch cannot contain admitted retained storage")
	}
	return nil
}

// The runtime opens the original real ledger before publishing any worker.
// One exact authenticated record proves an unchanged prefix even after later
// appends; no full historical census is repeated on every checkpoint.
func validateProductionCapacityLedger(ctx context.Context, cfg *ReleaseConfig, noId uint64, ledger *AttemptLedger) error {
	if ctx == nil || cfg == nil {
		return errors.New("capacity ledger context or configuration is absent")
	}
	if cfg.ProductionCapacityRevision == nil {
		return nil
	}
	if err := errors.Join(ctx.Err(), validateReleaseProductionAuthorityHistory(cfg)); err != nil {
		return err
	}
	var expected *ProductionCapacitySource
	for index := range cfg.ProductionCapacityRevision.Sources {
		source := &cfg.ProductionCapacityRevision.Sources[index]
		if source.Identity.NoID == noId {
			if expected != nil {
				return errors.New("capacity revision repeats a source")
			}
			expected = source
		}
	}
	if expected == nil || ledger == nil || ledger.identity != expected.Identity {
		return errors.New("capacity revision ledger identity differs")
	}
	head, err := ledger.Head()
	if err != nil {
		return err
	}
	if head.LastSequence < expected.Head.LastSequence || head.RecordBytes < expected.Head.RecordBytes || head.TrailCount < expected.Head.TrailCount {
		return errors.New("capacity revision lost retained ledger progress")
	}
	if expected.Head.LastSequence == 0 {
		return ctx.Err()
	}
	matched := false
	err = ledger.Walk(ctx, expected.Head.LastSequence, expected.Head.LastSequence, func(record AttemptRecord) error {
		matched = record.Identity == expected.Identity && record.RecordHash == expected.Head.Root
		return nil
	})
	if err != nil || !matched {
		return errors.Join(errors.New("capacity revision retained signed prefix differs"), err)
	}
	return nil
}
