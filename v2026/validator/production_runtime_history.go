// Production history is explicitly approved input to a signed schema 3
// configuration. Observation-only runtime documents never become producer pins.
package validator

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const releaseProductionRuntimeApprovalSchema = "urnetwork-validator-production-runtime-history-v1"

// Shares the complete wire coordinates without inheriting observation scope.
type releaseProductionRuntimeApproval releaseMainnetRuntimeApproval

// Loaded documents are immutable. The signed config selects their exact bytes;
// an old tuple never gains current signing authority through this history.
type releaseProductionRuntimeHistory struct {
	approvals  []releaseProductionRuntimeApproval
	encoded    [][]byte
	configHash [32]byte
}

// The current signed envelope already grants one finite exact-artifact window.
// Optional earlier windows must be explicit, ordered, nonoverlapping and bounded.
func loadReleaseProductionRuntimeHistory(cfg *ReleaseConfig) error {
	if len(cfg.ProductionRuntimeApprovals) > maximumReleaseMainnetRuntimeApprovals {
		return errors.New("production runtime history exceeds its approval bound")
	}
	encoded := make([][]byte, len(cfg.ProductionRuntimeApprovals))
	for index, reference := range cfg.ProductionRuntimeApprovals {
		raw, err := readRetainedProductionFile(cfg, reference, maximumReleaseMainnetRuntimeApprovalBytes, retainedProductionRuntimePath(cfg, reference.SHA256))
		if err != nil {
			return fmt.Errorf("production runtime history %d: %w", index+1, err)
		}
		encoded[index] = raw
	}
	return loadReleaseProductionRuntimeHistoryBytes(cfg, encoded)
}

// Original runtime documents travel with the complete signed authority, so
// decoding historical evidence never depends on the original source pathname.
func loadReleaseProductionRuntimeHistoryBytes(cfg *ReleaseConfig, encoded [][]byte) error {
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return err
	}
	if cfg.RuntimeSpec == 0 || cfg.TransactionVersion != 1 || cfg.StateVersion != 1 || approved.Approval.ValidThroughNativeBlock > math.MaxUint32 {
		return errors.New("production runtime requires supported signing encodings and a bounded native block window")
	}
	if len(cfg.ProductionRuntimeApprovals) > maximumReleaseMainnetRuntimeApprovals || len(encoded) != len(cfg.ProductionRuntimeApprovals) {
		return errors.New("production runtime history exceeds its approval bound")
	}
	history := &releaseProductionRuntimeHistory{}
	for index, reference := range cfg.ProductionRuntimeApprovals {
		raw := encoded[index]
		if err := matchProductionAuthorityBytes(reference, raw, maximumReleaseMainnetRuntimeApprovalBytes); err != nil {
			return fmt.Errorf("production runtime history %d: %w", index+1, err)
		}
		if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
			return err
		}
		var approval releaseProductionRuntimeApproval
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&approval); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return errors.New("production runtime history has trailing JSON")
		}
		if err := approval.validate(cfg, &approved.Approval, index, history.approvals); err != nil {
			return fmt.Errorf("production runtime history %d: %w", index+1, err)
		}
		history.approvals = append(history.approvals, approval)
		history.encoded = append(history.encoded, bytes.Clone(raw))
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	history.configHash = sha256.Sum256(raw)
	cfg.productionRuntimeHistory = history
	return nil
}

// Runtime revision numbers can increase or decrease only through a new exact
// approved interval. Numerical ordering and a compiled catalog grant nothing.
func (self releaseProductionRuntimeApproval) validate(cfg *ReleaseConfig, current *OwnerRecycleApproval, index int, previous []releaseProductionRuntimeApproval) error {
	if self.Schema != releaseProductionRuntimeApprovalSchema || self.Revision != uint64(index+1) ||
		self.NativeChain != current.NativeChain || self.GenesisHash != cfg.GenesisHash || self.EvmChainId != 964 ||
		self.DeploymentId != cfg.DeploymentID || self.ValidatorId != cfg.ValidatorID || self.Netuid != cfg.Netuid ||
		self.Coordinator != cfg.Coordinator || self.PolicyHash != cfg.PolicyHash {
		return errors.New("production history schema, revision or deployment scope differs")
	}
	if self.ValidFromBlock == 0 || self.ValidThroughBlock < self.ValidFromBlock || self.ValidThroughBlock > math.MaxUint32 ||
		self.ValidThroughBlock >= current.ValidFromNativeBlock {
		return errors.New("production history interval must be finite and precede the signed current window")
	}
	if index == 0 {
		if self.PreviousSha256 != "" {
			return errors.New("production history cannot begin with an omitted predecessor")
		}
	} else if self.PreviousSha256 != cfg.ProductionRuntimeApprovals[index-1].SHA256 || self.ValidFromBlock <= previous[index-1].ValidThroughBlock {
		return errors.New("production history must append exact prior bytes without overlapping intervals")
	}
	if self.RuntimeVersion.SpecName != "node-subtensor" || self.RuntimeVersion.SpecVersion == 0 ||
		self.RuntimeVersion.TransactionVersion != 1 || self.RuntimeVersion.StateVersion != 1 ||
		!releaseMainnetRuntimeHex(self.RuntimeCodeHash, 32, "0x") || !releaseMainnetRuntimeHex(self.RuntimeMetadataHash, 32, "0x") ||
		!releaseMainnetRuntimeHex(self.RuntimeSourceCommit, 20, "") || !releaseMainnetRuntimeHex(self.RuntimeReviewSha256, 32, "") ||
		self.RuntimeReviewScope != crv4.ValidatorProducerRuntimeProfile {
		return errors.New("production history lacks exact reviewed provenance and producer capability scope")
	}
	return nil
}

// A copied or edited public config cannot expand privately loaded authority.
func validateReleaseProductionRuntimeHistory(cfg *ReleaseConfig) error {
	if err := validateOwnerRecycleProductionConfig(cfg); err != nil {
		return err
	}
	if cfg.productionRuntimeHistory == nil || len(cfg.productionRuntimeHistory.approvals) != len(cfg.ProductionRuntimeApprovals) {
		return errors.New("production runtime history was not authenticated by its loader")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if sha256.Sum256(raw) != cfg.productionRuntimeHistory.configHash {
		return errors.New("production runtime config changed after authentication")
	}
	return nil
}

// Fresh production always uses the signed current window. Historical readers
// can select only an explicitly approved earlier artifact at its original block.
func releaseProductionRuntimeAt(cfg *ReleaseConfig, block uint64, historical bool) (crv4.RuntimeArtifactIdentity, error) {
	if err := validateReleaseProductionRuntimeHistory(cfg); err != nil {
		return crv4.RuntimeArtifactIdentity{}, err
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return crv4.RuntimeArtifactIdentity{}, err
	}
	if approved.Approval.ValidFromNativeBlock <= block && block <= approved.Approval.ValidThroughNativeBlock {
		return releaseNativeRuntimeIdentity(cfg), nil
	}
	if historical {
		windows, err := productionHistoricalRuntimeWindows(cfg)
		if err != nil {
			return crv4.RuntimeArtifactIdentity{}, err
		}
		for _, window := range windows {
			if window.from <= block && block <= window.through {
				return window.artifact, nil
			}
		}
	}
	return crv4.RuntimeArtifactIdentity{}, errors.New("native block is outside the approved production runtime interval")
}

// Production readers keep the independently approved exact block interval.
// Only original non-production readers retain the compiled companion catalog.
func releaseHistoricalRuntimeArtifactsAt(cfg *ReleaseConfig, number uint64) ([]crv4.RuntimeArtifactIdentity, error) {
	if cfg == nil {
		return nil, errors.New("historical runtime configuration is unavailable")
	}
	if isOwnerRecycleProductionConfig(cfg) {
		artifact, err := releaseProductionRuntimeAt(cfg, number, true)
		if err != nil {
			return nil, err
		}
		return []crv4.RuntimeArtifactIdentity{artifact}, nil
	}
	if err := rejectMainnetRuntimeObservationWrites(cfg); err != nil {
		return nil, err
	}
	if cfg.ChainID == 964 {
		return nil, errors.New("mainnet historical runtime requires independently loaded production authority")
	}
	return HistoricalReleaseRuntimeArtifacts(releaseNativeRuntimeIdentity(cfg)), nil
}
