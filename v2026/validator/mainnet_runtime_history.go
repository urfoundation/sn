// Mainnet runtime observation has independent, immutable approval history.
// Exact artifacts are selected by block interval, never by a release's catalog
// or by the numerical order of spec versions. This authority cannot sign.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const releaseMainnetRuntimeObservationSchemaVersion = 2
const releaseMainnetRuntimeApprovalSchema = "urnetwork-validator-mainnet-runtime-approval-v1"
const releaseMainnetRuntimeObservationScope = "urnetwork-validator-runtime-observation-v1"
const maximumReleaseMainnetRuntimeApprovals = 64
const maximumReleaseMainnetRuntimeApprovalBytes = 16 * 1024

// Each approval adds one closed block interval. A successor must reference the
// preceding document's exact bytes and cannot replace or overlap its interval.
// Review hashes are independently supplied provenance, not signatures or proof
// that the review was performed. The config's pinned references are authority.
type releaseMainnetRuntimeApproval struct {
	Schema              string                      `json:"schema"`
	Revision            uint64                      `json:"revision"`
	PreviousSha256      string                      `json:"previous_sha256,omitempty"`
	NativeChain         string                      `json:"native_chain"`
	GenesisHash         string                      `json:"genesis_hash"`
	EvmChainId          uint64                      `json:"evm_chain_id"`
	DeploymentId        string                      `json:"deployment_id"`
	ValidatorId         uint64                      `json:"validator_id"`
	Netuid              uint16                      `json:"netuid"`
	Coordinator         string                      `json:"coordinator"`
	PolicyHash          string                      `json:"policy_hash"`
	ValidFromBlock      uint64                      `json:"valid_from_block"`
	ValidThroughBlock   uint64                      `json:"valid_through_block"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeReviewSha256 string                      `json:"runtime_review_sha256"`
	RuntimeReviewScope  string                      `json:"runtime_review_scope"`
}

// Retained parsed documents and config identity are private and immutable after
// loading. Concurrent observations share them without reopening mutable paths.
type releaseMainnetRuntimeHistory struct {
	approvals  []releaseMainnetRuntimeApproval
	configHash [32]byte
}

// Loads schema 2 only for read-only runtime observation. Writer, bootstrap and
// archive loaders deliberately reject it, even if the runtime is compiled in.
func LoadMainnetRuntimeObservationConfig(path string) (*ReleaseConfig, error) {
	return loadReleaseConfig(path, releaseConfigLoadMode{mainnetRuntimeObservation: true})
}

// A read-only config never inherits authority from a matching signing tuple.
func rejectMainnetRuntimeObservationWrites(cfg *ReleaseConfig) error {
	if cfg != nil && (cfg.SchemaVersion == releaseMainnetRuntimeObservationSchemaVersion || len(cfg.MainnetRuntimeApprovals) != 0 || cfg.mainnetRuntimeHistory != nil) {
		return errors.New("mainnet runtime observation approval cannot authorize production signing, submission or archive acceptance")
	}
	return nil
}

// Canonical nonzero identities make references and provenance unambiguous.
func releaseMainnetRuntimeHex(value string, size int, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+2*size || strings.ToLower(value) != value {
		return false
	}
	raw, err := hex.DecodeString(value[len(prefix):])
	return err == nil && !bytes.Equal(raw, make([]byte, size))
}

// Mainnet observation grants no provisional, recycle, adoption or predecessor
// authority. Full policy/config validation still runs in the observation loader.
func validateReleaseMainnetRuntimeHistoryScope(cfg *ReleaseConfig) error {
	if cfg == nil || cfg.SchemaVersion != releaseMainnetRuntimeObservationSchemaVersion || !cfg.Production ||
		cfg.ChainID != 964 || cfg.Policy.NetworkProfile != "mainnet" || cfg.Netuid == 0 || cfg.ValidatorID == 0 ||
		cfg.ProvisionalRuntimeCompatibility != "" || cfg.ProvisionalDeferClosedNativeInput ||
		productionEconomicSelection(cfg) != nil || cfg.SourceRolePredecessorV2 != nil || cfg.PreviousPolicy != nil || cfg.historyAdoptionV2 != nil {
		return errors.New("mainnet runtime observation requires schema 2, independent mainnet policy and no provisional or producer authority")
	}
	if !releaseMainnetRuntimeHex(cfg.GenesisHash, 32, "0x") || cfg.GenesisHash == provisionalRuntimeTestnetGenesis ||
		len(cfg.MainnetRuntimeApprovals) == 0 || len(cfg.MainnetRuntimeApprovals) > maximumReleaseMainnetRuntimeApprovals {
		return errors.New("mainnet runtime observation requires an independent genesis and bounded pinned approval history")
	}
	return nil
}

// The private descriptor reader checks exact size/hash and rejects mutable or
// aliased inputs. No config, key, state or journal file is written by this path.
func loadReleaseMainnetRuntimeHistory(cfg *ReleaseConfig) error {
	if err := validateReleaseMainnetRuntimeHistoryScope(cfg); err != nil {
		return err
	}
	history := &releaseMainnetRuntimeHistory{}
	for index, reference := range cfg.MainnetRuntimeApprovals {
		raw, err := ReadReleaseEvidenceV2File(context.Background(), reference, maximumReleaseMainnetRuntimeApprovalBytes)
		if err != nil {
			return fmt.Errorf("mainnet runtime approval %d: %w", index+1, err)
		}
		if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
			return fmt.Errorf("mainnet runtime approval %d: %w", index+1, err)
		}
		var approval releaseMainnetRuntimeApproval
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&approval); err != nil {
			return fmt.Errorf("mainnet runtime approval %d: %w", index+1, err)
		}
		if err := approval.validate(cfg, index, history.approvals); err != nil {
			return fmt.Errorf("mainnet runtime approval %d: %w", index+1, err)
		}
		history.approvals = append(history.approvals, approval)
	}
	tail := history.approvals[len(history.approvals)-1].artifactIdentity()
	if tail != releaseNativeRuntimeIdentity(cfg) {
		return errors.New("mainnet runtime observation config does not pin the last approved exact artifact")
	}
	cfg.mainnetRuntimeHistory = history
	return sealReleaseMainnetRuntimeHistory(cfg)
}

// Version changes can be upgrades, unchanged versions or explicit rollbacks;
// their immutable block windows, complete artifacts and review scope decide.
func (self releaseMainnetRuntimeApproval) validate(cfg *ReleaseConfig, index int, previous []releaseMainnetRuntimeApproval) error {
	if self.Schema != releaseMainnetRuntimeApprovalSchema || self.Revision != uint64(index+1) ||
		self.NativeChain == "" || strings.TrimSpace(self.NativeChain) != self.NativeChain ||
		self.GenesisHash != cfg.GenesisHash || self.EvmChainId != cfg.ChainID || self.DeploymentId != cfg.DeploymentID ||
		self.ValidatorId != cfg.ValidatorID || self.Netuid != cfg.Netuid ||
		!releaseMainnetRuntimeHex(self.Coordinator, 20, "0x") || !strings.EqualFold(self.Coordinator, cfg.Coordinator) ||
		!releaseMainnetRuntimeHex(self.PolicyHash, 32, "0x") || self.PolicyHash != cfg.PolicyHash {
		return errors.New("approval schema, revision or mainnet deployment/policy scope differs")
	}
	if self.ValidFromBlock == 0 || self.ValidThroughBlock < self.ValidFromBlock || self.ValidThroughBlock > math.MaxUint32 {
		return errors.New("approval needs a finite nonzero native block interval")
	}
	if index == 0 {
		if self.PreviousSha256 != "" {
			return errors.New("first approval cannot replace omitted history")
		}
	} else {
		prior := previous[index-1]
		if self.PreviousSha256 != cfg.MainnetRuntimeApprovals[index-1].SHA256 ||
			self.NativeChain != prior.NativeChain || self.ValidFromBlock <= prior.ValidThroughBlock {
			return errors.New("approval must append the exact prior bytes without overlapping or rewriting history")
		}
	}
	if self.RuntimeVersion.SpecName != "node-subtensor" || self.RuntimeVersion.SpecVersion == 0 ||
		self.RuntimeVersion.TransactionVersion == 0 || self.RuntimeVersion.StateVersion == 0 ||
		!releaseMainnetRuntimeHex(self.RuntimeCodeHash, 32, "0x") || !releaseMainnetRuntimeHex(self.RuntimeMetadataHash, 32, "0x") ||
		!releaseMainnetRuntimeHex(self.RuntimeSourceCommit, 20, "") || !releaseMainnetRuntimeHex(self.RuntimeReviewSha256, 32, "") ||
		self.RuntimeReviewScope != releaseMainnetRuntimeObservationScope {
		return errors.New("approval lacks exact runtime, reviewed source/build provenance or read-only review scope")
	}
	return nil
}

// Artifact selection never consults the executable's reviewed-runtime catalog.
func (self releaseMainnetRuntimeApproval) artifactIdentity() crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{Version: self.RuntimeVersion, CodeHash: self.RuntimeCodeHash, MetadataHash: self.RuntimeMetadataHash}
}

// Sealing captures all normalized serialized config fields, including exact
// approval references. Copying or changing fields cannot expand loaded authority.
func sealReleaseMainnetRuntimeHistory(cfg *ReleaseConfig) error {
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	cfg.mainnetRuntimeHistory.configHash = sha256.Sum256(encoded)
	return nil
}

// Reuses parsed approvals only while their original config identity is intact.
func validateReleaseMainnetRuntimeHistory(cfg *ReleaseConfig) error {
	if err := validateReleaseMainnetRuntimeHistoryScope(cfg); err != nil {
		return err
	}
	if cfg.mainnetRuntimeHistory == nil || len(cfg.mainnetRuntimeHistory.approvals) != len(cfg.MainnetRuntimeApprovals) {
		return errors.New("mainnet runtime observation history was not authenticated by its loader")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if sha256.Sum256(encoded) != cfg.mainnetRuntimeHistory.configHash {
		return errors.New("mainnet runtime observation config changed after approval authentication")
	}
	return nil
}
