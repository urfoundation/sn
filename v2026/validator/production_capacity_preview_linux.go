//go:build linux

// Offline resource planning holds every selected physical root under a joined
// writer fence, authenticates original signed ledgers, and emits unsigned input.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"gopkg.in/yaml.v3"
)

const ProductionCapacityRequestSchema = "urnetwork-validator-production-capacity-request-v1"

// Root traversal, logical record work and expected future storage are separate
// dimensions. Every supplied root must contain a configured persistent owner.
type ProductionCapacityRoot struct {
	Path              string                        `json:"path"`
	FormerWriterFence durablevolume.Reference       `json:"former_writer_fence"`
	Limits            durablevolume.InventoryLimits `json:"limits"`
}

// The head is independently nominated and then authenticated by the same
// signed-prefix reader used for retained preparation. Forecasts confer no act.
type ProductionCapacityInput struct {
	Ledger             AttemptLedgerPreparationScope `json:"ledger"`
	FutureRecords      uint64                        `json:"future_records"`
	FutureTrails       uint64                        `json:"future_trails"`
	FutureRecordBytes  uint64                        `json:"future_record_bytes"`
	FutureProofBytes   uint64                        `json:"future_proof_bytes"`
	FutureStorageBytes uint64                        `json:"future_storage_bytes"`
	FutureStorageFiles uint64                        `json:"future_storage_files"`
}

// Paths name future separately provisioned public documents only. This command
// never writes them, reads signing keys, renews a window or restarts a service.
type ProductionCapacityRequest struct {
	Schema                string                    `json:"schema"`
	Config                ReleaseEvidenceV2File     `json:"config"`
	SuccessorApprovalPath string                    `json:"successor_approval_path"`
	OriginalAuthorityPath string                    `json:"original_authority_path"`
	Bounds                ReleaseEvidenceV2Bounds   `json:"bounds"`
	Roots                 []ProductionCapacityRoot  `json:"roots"`
	Sources               []ProductionCapacityInput `json:"sources"`
	FutureHistoryBytes    uint64                    `json:"future_history_bytes"`
	FutureCaptureFiles    uint64                    `json:"future_capture_files"`
}

// Public bytes can be exported for review and an independent signer. Neither
// an unsigned body nor an observed filesystem is a production capability.
type ProductionCapacityPreview struct {
	Schema                 string                           `json:"schema"`
	Config                 *ReleaseConfig                   `json:"config"`
	ConfigDocument         string                           `json:"config_document"`
	Approval               OwnerRecycleApproval             `json:"approval"`
	SigningBytes           string                           `json:"signing_bytes"`
	OriginalAuthority      ReleaseEvidenceV2File            `json:"original_authority"`
	OriginalAuthorityBytes []byte                           `json:"original_authority_bytes"`
	Physical               []durablevolume.Inventory        `json:"physical"`
	Ledgers                []AttemptLedgerPreparationCensus `json:"ledgers"`
	RestartAuthorized      bool                             `json:"restart_authorized"`
}

// All roots stay exclusively retained until every source and the complete
// proposal has been checked. Failure closes/joined owners and emits no draft.
func BuildProductionCapacityPreview(ctx context.Context, request ProductionCapacityRequest) (result ProductionCapacityPreview, resultErr error) {
	if err := durablepath.Require(ctx); err != nil {
		return result, err
	}
	if request.Schema != ProductionCapacityRequestSchema || len(request.Sources) == 0 || len(request.Sources) > 256 || len(request.Roots) == 0 || len(request.Roots) > 257 {
		return result, errors.New("capacity request has no finite complete owner census")
	}
	raw, err := ReadReleaseEvidenceV2File(ctx, request.Config, maximumReleaseConfigBytes)
	if err != nil {
		return result, err
	}
	cfg, err := decodeReleaseConfigBytes(request.Config.Path, raw)
	if err != nil {
		return result, err
	}
	if cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion || len(cfg.Operators) != len(request.Sources) || len(cfg.EvidenceV2.Operators) != len(request.Sources) {
		return result, errors.New("capacity request differs from complete production operator authority")
	}
	if err := errors.Join(validateReleaseProductionAuthorityHistory(cfg), validateProductionCapacityBounds(cfg.EvidenceV2.Bounds, request.Bounds, uint64(len(request.Sources))), validateProductionCapacityGeometry(request.Bounds)); err != nil {
		return result, err
	}
	if err := errors.Join(ValidateReleaseEvidenceV2Path(request.SuccessorApprovalPath), ValidateReleaseEvidenceV2Path(request.OriginalAuthorityPath)); err != nil {
		return result, err
	}
	if request.SuccessorApprovalPath == request.OriginalAuthorityPath {
		return result, errors.New("capacity request aliases original or proposed public authority")
	}
	retainedPaths := []string{request.Config.Path, productionEconomicSelection(cfg).Approval.Path, cfg.HotkeySeedFile}
	for _, prior := range append(append([]ReleaseEvidenceV2File(nil), cfg.ProductionAuthorityHistory...), cfg.ProductionRuntimeApprovals...) {
		retainedPaths = append(retainedPaths, prior.Path)
	}
	for _, operator := range cfg.Operators {
		retainedPaths = append(retainedPaths, operator.NetworkJWTFile, operator.ClientJWTFile, operator.ClientKeySeedFile)
	}
	for _, operator := range cfg.EvidenceV2.Operators {
		for _, file := range operator.Files() {
			retainedPaths = append(retainedPaths, file.Path)
		}
	}
	for _, retained := range retainedPaths {
		if request.SuccessorApprovalPath == retained || request.OriginalAuthorityPath == retained {
			return result, errors.New("capacity request replaces retained historical authority")
		}
	}
	type selectedRoot struct {
		owner  *durablevolume.Owner
		report durablevolume.Inventory
	}
	selected := make([]selectedRoot, len(request.Roots))
	defer func() {
		for _, root := range selected {
			if root.owner != nil {
				resultErr = errors.Join(resultErr, root.owner.Close())
			}
		}
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ProductionCapacityPreview{}
		}
	}()
	used := make([]bool, len(request.Roots))
	locate := func(path string) (int, string, error) {
		found, relative := -1, ""
		for index, root := range request.Roots {
			part, err := filepath.Rel(root.Path, path)
			if err != nil || part == ".." || strings.HasPrefix(part, ".."+string(filepath.Separator)) || filepath.IsAbs(part) {
				continue
			}
			if found != -1 {
				return -1, "", errors.New("capacity owner belongs to overlapping declared roots")
			}
			found, relative = index, part
		}
		if found < 0 {
			return -1, "", errors.New("capacity request omitted configured persistent custody")
		}
		used[found] = true
		if relative == "." {
			relative = ""
		}
		return found, relative, nil
	}
	if _, _, err := locate(cfg.StateDir); err != nil {
		return result, err
	}
	for _, operator := range cfg.Operators {
		if _, _, err := locate(operator.StateDir); err != nil {
			return result, err
		}
	}
	for index, root := range request.Roots {
		if !used[index] {
			return result, errors.New("capacity request adds unrelated persistent custody")
		}
		owner, err := durablepath.OpenVolume(ctx, root.Path, durablevolume.Snapshot)
		if err != nil {
			return result, err
		}
		selected[index].owner = owner
		report, err := owner.InventoryPhysical(ctx, root.FormerWriterFence, root.Limits)
		if err != nil {
			return result, err
		}
		selected[index].report = report
		result.Physical = append(result.Physical, report)
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return result, err
	}
	revision := ProductionCapacityRevision{Schema: ProductionCapacityRevisionSchema, PredecessorConfigHash: attemptHex32(approved.Approval.ConfigHash), PredecessorApprovalSha256: productionEconomicSelection(cfg).Approval.SHA256, EconomicApprovalSha256: productionCapacityEconomicHash(approved.Approval), ValidThroughNativeBlock: approved.Approval.ValidThroughNativeBlock, ValidThroughNativeEpoch: approved.Approval.Production.ValidThroughNativeEpoch, Margin: 2, FutureHistoryBytes: request.FutureHistoryBytes, FutureCaptureFiles: request.FutureCaptureFiles}
	for _, root := range selected {
		if root.report.TotalBytes > ^uint64(0)-revision.RetainedHistoryBytes {
			return result, errors.New("capacity retained history byte sum overflows")
		}
		revision.RetainedHistoryBytes += root.report.TotalBytes
		for _, entry := range root.report.Entries {
			if entry.Kind == "file" {
				revision.RetainedCaptureFiles++
			}
		}
	}
	for index, input := range request.Sources {
		operator := cfg.Operators[index]
		if input.Ledger.Identity.NoID != operator.NoID || input.Ledger.Identity.NoID != cfg.EvidenceV2.Operators[index].NoID || input.Ledger.Coordinator != strings.ToLower(cfg.Coordinator) || !reflect.DeepEqual(input.Ledger.Limits, cfg.EvidenceV2.Bounds.Disk) {
			return result, errors.New("capacity input changes original operator, coordinator or read bound")
		}
		rootIndex, relative, err := locate(operator.StateDir)
		if err != nil {
			return result, err
		}
		root := selected[rootIndex]
		var census AttemptLedgerPreparationCensus
		err = func() (resultErr error) {
			file, err := root.owner.OpenDirectory(relative, false)
			if err != nil {
				return err
			}
			defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
			if err := root.owner.CheckReadDirectory(relative, file); err != nil {
				return err
			}
			if _, err := readAttemptLedgerCustodyAttribute(file); err != nil {
				return fmt.Errorf("capacity source requires its original custody checkpoint: %w", err)
			}
			census, err = InspectAttemptLedgerPreparation(ctx, file, input.Ledger)
			return errors.Join(err, ctx.Err(), root.owner.CheckReadDirectory(relative, file))
		}()
		if err != nil {
			return result, fmt.Errorf("capacity source %d: %w", operator.NoID, err)
		}
		encoded, err := json.Marshal(census)
		if err != nil {
			return result, err
		}
		source := ProductionCapacitySource{Identity: input.Ledger.Identity, Head: census.Head, CensusSha256: attemptHex32(sha256.Sum256(encoded)), FutureRecords: input.FutureRecords, FutureTrails: input.FutureTrails, FutureRecordBytes: input.FutureRecordBytes, FutureProofBytes: input.FutureProofBytes, StorageBytes: root.report.TotalBytes, FutureStorageBytes: input.FutureStorageBytes, FutureStorageFiles: input.FutureStorageFiles}
		for _, entry := range root.report.Entries {
			if entry.Kind == "file" {
				source.StorageFiles++
			}
		}
		revision.Sources = append(revision.Sources, source)
		result.Ledgers = append(result.Ledgers, census)
	}
	bundle, err := BuildOwnerRecycleProductionAuthority(ctx, cfg)
	if err != nil {
		return result, err
	}
	if len(cfg.ProductionAuthorityHistory) >= maximumProductionAuthorityHistory {
		return result, errors.New("capacity revision exhausts original authority history count")
	}
	raw, err = json.Marshal(cfg)
	if err != nil {
		return result, err
	}
	var next ReleaseConfig
	if err := json.Unmarshal(raw, &next); err != nil {
		return result, err
	}
	reference := ReleaseEvidenceV2File{Path: request.OriginalAuthorityPath, Bytes: uint64(len(bundle)), SHA256: attemptHex32(sha256.Sum256(bundle))}
	next.ProductionAuthorityHistory = append(next.ProductionAuthorityHistory, reference)
	next.EvidenceV2.Bounds = request.Bounds
	next.ProductionCapacityRevision = &revision
	selection := &ReleaseOwnerRecycleApprovalConfig{Signer: productionEconomicSelection(cfg).Signer, Approval: ReleaseEvidenceV2File{Path: request.SuccessorApprovalPath}}
	if cfg.TreasuryApproval != nil {
		next.TreasuryApproval = selection
	} else {
		next.OwnerRecycleApproval = selection
	}
	// Nested runtime bounds retain their original YAML names, which are not
	// always their default Go JSON field names. Emit that exact document and
	// validate it before any independent signer receives a message.
	raw, err = yaml.Marshal(&next)
	if err != nil {
		return result, err
	}
	decoded, err := decodeReleaseConfigDocument(request.Config.Path, raw)
	if err != nil {
		return result, err
	}
	beforeHash, beforeErr := OwnerRecycleConfigHash(&next)
	afterHash, afterErr := OwnerRecycleConfigHash(decoded)
	if beforeErr != nil || afterErr != nil || beforeHash != afterHash {
		return result, errors.Join(errors.New("capacity proposal changes under strict document decoding"), beforeErr, afterErr)
	}
	next = *decoded
	result.ConfigDocument = string(raw)
	approval := approved.Approval
	approval.ConfigHash, err = OwnerRecycleConfigHash(&next)
	if err != nil {
		return result, err
	}
	if err := validateProductionCapacityRevision(cfg, &next, approved.Approval, approval); err != nil {
		return result, err
	}
	message, err := approval.SigningMessage()
	if err != nil {
		return result, err
	}
	result.Schema, result.Config, result.Approval = "urnetwork-validator-production-capacity-preview-v1", &next, approval
	result.SigningBytes = "0x" + hex.EncodeToString(message)
	result.OriginalAuthority, result.OriginalAuthorityBytes = reference, bundle
	return result, nil
}

// Complete only the unsigned reference to an independently supplied approval.
// Every signed config field must remain equal to the preview; the actual full
// loader authenticates the original history and signature before output.
func CompleteProductionCapacityDocument(ctx context.Context, path string, preview ProductionCapacityPreview, approval ReleaseEvidenceV2File) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("capacity completion context is absent")
	}
	if err := errors.Join(ctx.Err(), ValidateReleaseEvidenceV2Path(path)); err != nil {
		return nil, err
	}
	if preview.Schema != "urnetwork-validator-production-capacity-preview-v1" || preview.RestartAuthorized || preview.ConfigDocument == "" || preview.Config == nil {
		return nil, errors.New("capacity completion requires the original unsigned document preview")
	}
	cfg, err := decodeReleaseConfigDocument(path, []byte(preview.ConfigDocument))
	if err != nil {
		return nil, err
	}
	if productionEconomicSelection(cfg) == nil || productionEconomicSelection(cfg).Approval.Path != approval.Path {
		return nil, errors.New("capacity completion changes the originally nominated approval path")
	}
	digest, err := OwnerRecycleConfigHash(cfg)
	viewDigest, viewErr := OwnerRecycleConfigHash(preview.Config)
	message, messageErr := preview.Approval.SigningMessage()
	if err != nil || viewErr != nil || messageErr != nil || digest != viewDigest || digest != preview.Approval.ConfigHash || "0x"+hex.EncodeToString(message) != preview.SigningBytes {
		return nil, errors.Join(errors.New("capacity completion changes the reviewed document or signing bytes"), err, viewErr, messageErr)
	}
	if _, err := ReadReleaseEvidenceV2File(ctx, approval, maximumOwnerRecycleApprovalBytes); err != nil {
		return nil, err
	}
	productionEconomicSelection(cfg).Approval = approval
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	loaded, err := decodeReleaseConfigBytes(path, raw)
	if err != nil {
		return nil, err
	}
	approved, err := ownerRecycleProductionApproval(loaded)
	if err != nil || !reflect.DeepEqual(approved.Approval, preview.Approval) {
		return nil, errors.Join(errors.New("capacity completion changes independently approved authority"), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
