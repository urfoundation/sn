// Original production authority is immutable signed input, separate from the
// current producer. A bounded append-only selection survives lost source files.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"

	"github.com/urfoundation/sn/v2026/protocol"
)

const releaseProductionAuthorityBundleSchema = "urnetwork-validator-production-authority-v1"
const maximumProductionAuthorityBundleBytes = 6 * 1024 * 1024
const maximumProductionAuthorityHistoryBytes = 64 * 1024 * 1024
const maximumProductionAuthorityHistory = 32

// The full original normalized config selects its independent signer and exact
// predecessor/runtime documents. No private key, verdict or derived row is stored.
type releaseProductionAuthorityBundle struct {
	Schema           string   `json:"schema"`
	Config           []byte   `json:"config"`
	Approval         []byte   `json:"approval"`
	RuntimeApprovals [][]byte `json:"runtime_approvals"`
}

// The loader owns all bytes/configs. Prefix slices share immutable earlier
// entries; no cycle, recursive file read or process-global cache is required.
type releaseProductionAuthorityHistory struct {
	entries    []releaseProductionAuthorityEntry
	windows    []releaseProductionRuntimeWindow
	configHash [32]byte
}

// This read-only projection has no economic config, signer or submission grant.
type releaseProductionRuntimeWindow struct {
	from, through uint64
	artifact      crv4.RuntimeArtifactIdentity
}

// A history config is permanently read-only even when its original window
// overlaps the current head. It cannot acquire a fresh prepared signing grant.
type releaseProductionAuthorityEntry struct {
	config  *ReleaseConfig
	encoded []byte
}

// File witnesses remain exact when their bytes travel inside another approved
// document. A hash, count or path change never weakens the original byte bound.
func matchProductionAuthorityBytes(reference ReleaseEvidenceV2File, raw []byte, maximum uint64) error {
	if err := reference.Validate(maximum); err != nil {
		return err
	}
	if uint64(len(raw)) != reference.Bytes || reference.SHA256 != attemptHex32(sha256.Sum256(raw)) {
		return errors.New("production authority bytes differ from the exact selected reference")
	}
	return nil
}

// Flat content-addressed names reuse the existing private state owner and
// cannot overwrite either the old fixed record or another signed approval.
func retainedProductionApprovalPath(cfg *ReleaseConfig) string {
	prefix := "owner-recycle-production-approval-"
	if cfg.TreasuryApproval != nil {
		prefix = "treasury-production-approval-"
	}
	return filepath.Join(cfg.StateDir, prefix+strings.TrimPrefix(productionEconomicSelection(cfg).Approval.SHA256, "0x")+".json")
}

// History lookup never trusts the source path to identify its contents.
func retainedProductionAuthorityPath(cfg *ReleaseConfig, hash string) string {
	return filepath.Join(cfg.StateDir, "owner-recycle-production-authority-"+strings.TrimPrefix(hash, "0x")+".json")
}

// Current runtime documents also retain their exact selected bytes independently
// of the original provisioning directory.
func retainedProductionRuntimePath(cfg *ReleaseConfig, hash string) string {
	return filepath.Join(cfg.StateDir, "owner-recycle-production-runtime-"+strings.TrimPrefix(hash, "0x")+".json")
}

// After the signed path fails, each retained copy is read only at the signed
// size and digest, so a location never changes the admitted bytes. Staging
// mounts no state_dir and last tries the content-addressed name beside its
// pinned config; a fixed record name is looked up in state_dir only.
func readRetainedProductionFile(cfg *ReleaseConfig, reference ReleaseEvidenceV2File, maximum uint64, retained string, fixed ...string) ([]byte, error) {
	raw, sourceErr := ReadReleaseEvidenceV2File(context.Background(), reference, maximum)
	if sourceErr == nil {
		return raw, nil
	}
	paths := append([]string{retained}, fixed...)
	if cfg.stagingConfigDirectory != "" {
		paths = append(paths, filepath.Join(cfg.stagingConfigDirectory, filepath.Base(retained)))
	}
	var err error
	for _, path := range paths {
		reference.Path = path
		if raw, err = ReadReleaseEvidenceV2File(context.Background(), reference, maximum); err == nil {
			return raw, nil
		}
	}
	return nil, errors.Join(sourceErr, err)
}

// Each original config must select exactly the already approved prefix. The
// current signature therefore commits the complete finite authority lineage.
func loadReleaseProductionAuthorityHistory(cfg *ReleaseConfig) error {
	if err := validateReleaseProductionRuntimeHistory(cfg); err != nil {
		return err
	}
	if len(cfg.ProductionAuthorityHistory) == 0 {
		if cfg.ProductionCapacityRevision != nil {
			return errors.New("capacity revision omits original signed production authority")
		}
		cfg.productionAuthorityHistory = nil
		return nil
	}
	if len(cfg.ProductionAuthorityHistory) > maximumProductionAuthorityHistory {
		return errors.New("production authority history exceeds its finite count")
	}
	remaining := uint64(maximumProductionAuthorityHistoryBytes)
	for _, reference := range cfg.ProductionAuthorityHistory {
		if err := reference.Validate(maximumProductionAuthorityBundleBytes); err != nil {
			return err
		}
		if reference.Bytes > remaining {
			return errors.New("production authority history exceeds its aggregate byte bound")
		}
		remaining -= reference.Bytes
	}
	current, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return err
	}
	history := &releaseProductionAuthorityHistory{}
	seenKVs := map[[32]byte]bool{}
	for index, reference := range cfg.ProductionAuthorityHistory {
		raw, err := readRetainedProductionFile(cfg, reference, maximumProductionAuthorityBundleBytes, retainedProductionAuthorityPath(cfg, reference.SHA256))
		if err != nil {
			return err
		}
		original, err := decodeProductionAuthorityBundle(raw, cfg, history.entries, index)
		if err != nil {
			return fmt.Errorf("production original authority %d: %w", index+1, err)
		}
		approved, err := ownerRecycleProductionApproval(original)
		if err != nil {
			return err
		}
		if seenKVs[approved.Approval.ConfigHash] || approved.Approval.ConfigHash == current.Approval.ConfigHash {
			return errors.New("production authority history repeats a complete config")
		}
		seenKVs[approved.Approval.ConfigHash] = true
		history.entries = append(history.entries, releaseProductionAuthorityEntry{config: original, encoded: bytes.Clone(raw)})
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	history.configHash = sha256.Sum256(raw)
	cfg.productionAuthorityHistory = history
	history.windows, err = buildProductionHistoricalRuntimeWindows(cfg)
	if err != nil {
		cfg.productionAuthorityHistory = nil
		return err
	}
	return validateReleaseProductionAuthorityHistory(cfg)
}

// Exact original JSON is decoded without reopening paths or applying new
// defaults. Its independent signature and complete config hash authenticate it.
func decodeProductionAuthorityBundle(raw []byte, current *ReleaseConfig, prefix []releaseProductionAuthorityEntry, index int) (*ReleaseConfig, error) {
	if len(raw) == 0 || len(raw) > maximumProductionAuthorityBundleBytes || index > maximumProductionAuthorityHistory {
		return nil, errors.New("production authority bundle exceeds its bound")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	var bundle releaseProductionAuthorityBundle
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(bundle)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) || bundle.Schema != releaseProductionAuthorityBundleSchema ||
		len(bundle.Config) == 0 || len(bundle.Config) > maximumReleaseConfigBytes || len(bundle.RuntimeApprovals) > maximumReleaseMainnetRuntimeApprovals {
		return nil, errors.New("production authority bundle schema or canonical bounds differ")
	}
	if err := protocol.ValidateUniqueJsonKeys(bundle.Config); err != nil {
		return nil, err
	}
	var cfg ReleaseConfig
	decoder = json.NewDecoder(bytes.NewReader(bundle.Config))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	canonical, err = json.Marshal(&cfg)
	if err != nil || !bytes.Equal(canonical, bundle.Config) || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion ||
		len(cfg.ProductionAuthorityHistory) != index || !reflect.DeepEqual(cfg.ProductionAuthorityHistory, current.ProductionAuthorityHistory[:index]) && index != 0 {
		return nil, errors.New("original production config is not canonical or changes its signed predecessor prefix")
	}
	if err := loadOwnerRecycleProductionConfigBytes(&cfg, bundle.Approval); err != nil {
		return nil, err
	}
	if err := loadReleaseProductionRuntimeHistoryBytes(&cfg, bundle.RuntimeApprovals); err != nil {
		return nil, err
	}
	if index != 0 {
		if err := validateProductionAuthorityContinuity(prefix[index-1].config, &cfg); err != nil {
			return nil, err
		}
	}
	if index != 0 {
		cfg.productionAuthorityHistory = &releaseProductionAuthorityHistory{entries: prefix, configHash: sha256.Sum256(canonical)}
		cfg.productionAuthorityHistory.windows, err = buildProductionHistoricalRuntimeWindows(&cfg)
		if err != nil {
			return nil, err
		}
	}
	// Each revision binds its immediate predecessor. All earlier links were
	// authenticated above; comparing an ancestor directly would skip that link.
	if index+1 == len(current.ProductionAuthorityHistory) {
		if err := validateProductionAuthorityContinuity(&cfg, current); err != nil {
			return nil, err
		}
	}
	cfg.ownerRecycleProduction.historicalOnly = true
	return &cfg, nil
}

// This first continuity class preserves policy, identities, operators, proof
// bounds and custody. Runtime pins, approval history and polling may advance.
// Rendering an activation-pending census is a separate exact class.
func validateProductionAuthorityContinuity(original, current *ReleaseConfig) error {
	if err := validateProductionCapacityTransition(original, current); err != nil {
		return err
	}
	if productionEvidenceRenderingTransition(original, current) {
		prior, err := ownerRecycleProductionApproval(original)
		if err != nil {
			return err
		}
		next, err := ownerRecycleProductionApproval(current)
		if err != nil {
			return err
		}
		return validateProductionEvidenceRendering(original, current, prior.Approval, next.Approval)
	}
	// A renewal may newly opt in to successor admission, but cannot withdraw
	// or change the profile under which original decisions were admitted.
	if original.RuntimeSuccessorProfile != "" && current.RuntimeSuccessorProfile != original.RuntimeSuccessorProfile {
		return errors.New("production authority continuity cannot withdraw or change runtime successor admission")
	}
	stable := func(cfg *ReleaseConfig) ([]byte, error) {
		owned := *cfg
		owned.RuntimeSpec, owned.TransactionVersion, owned.StateVersion = 0, 0, 0
		owned.RuntimeCodeHash, owned.RuntimeMetadataHash = "", ""
		owned.RuntimeSuccessorProfile = ""
		owned.ProductionRuntimeApprovals, owned.ProductionAuthorityHistory = nil, nil
		owned.OwnerRecycleApproval = &ReleaseOwnerRecycleApprovalConfig{Signer: productionEconomicSelection(cfg).Signer}
		owned.TreasuryApproval = nil
		owned.PollSeconds = 0
		owned.EvidenceV2.Bounds = ReleaseEvidenceV2Bounds{}
		owned.ProductionCapacityRevision = nil
		return json.Marshal(&owned)
	}
	old, err := stable(original)
	if err != nil {
		return err
	}
	next, err := stable(current)
	if err != nil || !bytes.Equal(old, next) {
		return errors.New("production authority continuity changes policy, identity, transport or custody")
	}
	prior, err := ownerRecycleProductionApproval(original)
	if err != nil {
		return err
	}
	approved, err := ownerRecycleProductionApproval(current)
	if err != nil {
		return err
	}
	a, b := prior.Approval, approved.Approval
	treasuryTransition := a.Proposal.Treasury == nil && b.Proposal.Treasury != nil
	if a.Proposal.Treasury != nil && b.Proposal.Treasury == nil {
		return errors.New("treasury authority cannot be reinterpreted as an old owner-recycle policy")
	}
	if treasuryTransition {
		if b.FirstNativeEpoch <= a.FirstNativeEpoch || b.Proposal.PolicyId <= a.Proposal.PolicyId ||
			b.Proposal.EffectiveEpoch <= a.Proposal.EffectiveEpoch || ownerRecycleActivationBlock(&b) <= ownerRecycleActivationBlock(&a) ||
			ownerRecycleActivationBlock(&b) != b.ValidFromNativeBlock {
			return errors.New("treasury successor requires an explicit advancing drained economic activation")
		}
	} else if !reflect.DeepEqual(a.Proposal.Treasury, b.Proposal.Treasury) {
		return errors.New("runtime or capacity continuity cannot change the signed treasury policy")
	}
	if a.ValidatorHotkey != b.ValidatorHotkey || a.NativeChain != b.NativeChain || a.SubnetOwner != b.SubnetOwner ||
		a.Production.EpochScheduleProfile != b.Production.EpochScheduleProfile ||
		a.ValidFromNativeBlock > b.ValidFromNativeBlock || !treasuryTransition && (a.FirstNativeEpoch != b.FirstNativeEpoch ||
		a.Production.ActivationNativeHash != b.Production.ActivationNativeHash || ownerRecycleActivationBlock(&a) != ownerRecycleActivationBlock(&b)) {
		return errors.New("production authority continuity changes the original signer or economic activation")
	}
	if a.ValidFromNativeBlock == b.ValidFromNativeBlock && releaseNativeRuntimeIdentity(original) != releaseNativeRuntimeIdentity(current) {
		return errors.New("production authority changes an exact runtime at the same activation block")
	}
	return nil
}

// Original current windows end at the next explicitly approved runtime window.
// Existing explicit historical documents remain exact and must agree wherever
// they overlap. Sorting/merging happens once at load, never per historical read.
func buildProductionHistoricalRuntimeWindows(cfg *ReleaseConfig) ([]releaseProductionRuntimeWindow, error) {
	current, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return nil, err
	}
	var windows []releaseProductionRuntimeWindow
	appendDocuments := func(source *ReleaseConfig) {
		for _, approval := range source.productionRuntimeHistory.approvals {
			windows = append(windows, releaseProductionRuntimeWindow{from: approval.ValidFromBlock, through: approval.ValidThroughBlock,
				artifact: crv4.RuntimeArtifactIdentity{Version: approval.RuntimeVersion, CodeHash: approval.RuntimeCodeHash, MetadataHash: approval.RuntimeMetadataHash}})
		}
	}
	appendDocuments(cfg)
	if owner := cfg.productionAuthorityHistory; owner != nil {
		for index, entry := range owner.entries {
			approved, err := ownerRecycleProductionApproval(entry.config)
			if err != nil {
				return nil, err
			}
			appendDocuments(entry.config)
			nextFrom := current.Approval.ValidFromNativeBlock
			if index+1 < len(owner.entries) {
				next, err := ownerRecycleProductionApproval(owner.entries[index+1].config)
				if err != nil {
					return nil, err
				}
				nextFrom = next.Approval.ValidFromNativeBlock
			}
			if approved.Approval.ValidFromNativeBlock < nextFrom {
				windows = append(windows, releaseProductionRuntimeWindow{from: approved.Approval.ValidFromNativeBlock,
					through: min(approved.Approval.ValidThroughNativeBlock, nextFrom-1), artifact: releaseNativeRuntimeIdentity(entry.config)})
			}
		}
	}
	slices.SortFunc(windows, func(a, b releaseProductionRuntimeWindow) int {
		if a.from < b.from {
			return -1
		}
		if a.from > b.from {
			return 1
		}
		if a.through < b.through {
			return -1
		}
		if a.through > b.through {
			return 1
		}
		return 0
	})
	merged := make([]releaseProductionRuntimeWindow, 0, len(windows))
	for _, window := range windows {
		if window.from == 0 || window.through < window.from || window.through >= current.Approval.ValidFromNativeBlock {
			return nil, errors.New("production original runtime window overlaps current signing authority")
		}
		if len(merged) != 0 {
			last := &merged[len(merged)-1]
			if window.from <= last.through && window.artifact != last.artifact {
				return nil, errors.New("production original runtime windows contain conflicting exact artifacts")
			}
			if window.from <= last.through+1 && window.artifact == last.artifact {
				last.through = max(last.through, window.through)
				continue
			}
		}
		merged = append(merged, window)
	}
	return merged, nil
}

// Downstream observers retain only this detached runtime/window projection.
// They never receive old economic configs or current producer authority.
func productionHistoricalRuntimeWindows(cfg *ReleaseConfig) ([]releaseProductionRuntimeWindow, error) {
	if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
		return nil, err
	}
	if err := validateReleaseProductionRuntimeHistory(cfg); err != nil {
		return nil, err
	}
	if cfg.productionAuthorityHistory != nil {
		return slices.Clone(cfg.productionAuthorityHistory.windows), nil
	}
	return buildProductionHistoricalRuntimeWindows(cfg)
}

// Empty history preserves the original schema-3 behavior. Nonempty history
// requires the private loader and an unchanged complete public config.
func validateReleaseProductionAuthorityHistory(cfg *ReleaseConfig) error {
	if err := validateOwnerRecycleProductionConfig(cfg); err != nil {
		return err
	}
	if len(cfg.ProductionAuthorityHistory) == 0 && cfg.productionAuthorityHistory == nil {
		if cfg.ProductionCapacityRevision != nil {
			return errors.New("capacity revision has no retained original authority")
		}
		return nil
	}
	owner := cfg.productionAuthorityHistory
	if owner == nil || len(owner.entries) != len(cfg.ProductionAuthorityHistory) || len(owner.entries) > maximumProductionAuthorityHistory {
		return errors.New("production authority history lacks its immutable loader")
	}
	raw, err := json.Marshal(cfg)
	if err != nil || sha256.Sum256(raw) != owner.configHash {
		return errors.New("production authority history changed after authentication")
	}
	return nil
}

// Select by original signed authority bytes, never version ordering or a
// candidate config hash alone. A returned historical config grants reads only.
func productionConfigForIntent(cfg *ReleaseConfig, intent *SteeringIntent) (*ReleaseConfig, error) {
	if !isOwnerRecycleProductionConfig(cfg) {
		return cfg, nil
	}
	if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
		return nil, err
	}
	if intent == nil || productionEconomicIntent(intent) == nil {
		return nil, errors.New("production intent lacks its original signed authority")
	}
	if bytes.Equal(productionEconomicIntent(intent).Proof.Approval, cfg.ownerRecycleProduction.encoded) {
		return cfg, nil
	}
	if cfg.productionAuthorityHistory != nil {
		for _, entry := range cfg.productionAuthorityHistory.entries {
			if bytes.Equal(productionEconomicIntent(intent).Proof.Approval, entry.config.ownerRecycleProduction.encoded) {
				return entry.config, nil
			}
		}
	}
	return nil, errors.New("production intent original authority is absent from the signed config history")
}

// Exports only already authenticated bytes for independent review and future
// config selection. Producing a bundle creates no new approval or chain action.
func BuildOwnerRecycleProductionAuthority(ctx context.Context, cfg *ReleaseConfig) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("production authority export context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
		return nil, err
	}
	if err := validateReleaseProductionRuntimeHistory(cfg); err != nil {
		return nil, err
	}
	config, err := json.Marshal(cfg)
	if err != nil || len(config) > maximumReleaseConfigBytes {
		return nil, errors.New("production authority config exceeds its byte bound")
	}
	bundle := releaseProductionAuthorityBundle{Schema: releaseProductionAuthorityBundleSchema, Config: config,
		Approval: cfg.ownerRecycleProduction.encoded, RuntimeApprovals: cfg.productionRuntimeHistory.encoded}
	raw, err := json.Marshal(bundle)
	if err != nil || len(raw)+1 > maximumProductionAuthorityBundleBytes {
		return nil, errors.New("production authority export exceeds its complete byte bound")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
