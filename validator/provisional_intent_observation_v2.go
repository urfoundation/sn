//go:build linux || darwin

package validator

// Local runtime status is deliberately separate from authenticated V2 replay.
// This observer owns no ledger, writes nothing and performs no chain requests.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"
)

type ProvisionalIntentObservationV2Options struct {
	ConfigPath, HandoffSHA256, PlanHash, DeploymentID string
	Handoff                                           []byte
	ValidatorID                                       uint64
	Netuid                                            uint16
	Hotkey                                            [32]byte
}

// Receipts are recorded runtime claims, not independently verified chain facts.
// Prepared signed bytes are never returned by this observation API.
type ProvisionalIntentReceiptV2 struct {
	Status                string `json:"status"`
	SubnetEpoch           uint64 `json:"subnet_epoch"`
	SettlementEpoch       uint64 `json:"settlement_epoch"`
	VectorHash            string `json:"vector_hash"`
	PreparedExtrinsicHash string `json:"prepared_extrinsic_hash"`
	PreparedAtBlock       uint64 `json:"prepared_at_block"`
	PreparedAtBlockHash   string `json:"prepared_at_block_hash"`
	UpdatedAt             string `json:"updated_at"`
	ExtrinsicHash         string `json:"extrinsic_hash,omitempty"`
	FinalizedBlock        uint64 `json:"finalized_block,omitempty"`
	FinalizedBlockHash    string `json:"finalized_block_hash,omitempty"`
	RevealBlock           uint64 `json:"reveal_block,omitempty"`
	ApplicationBlock      uint64 `json:"application_block,omitempty"`
	ApplicationBlockHash  string `json:"application_block_hash,omitempty"`
}

type ProvisionalIntentObservationV2 struct {
	Scope                    string                       `json:"scope"`
	FinalAcceptance          bool                         `json:"final_acceptance"`
	ObservedAt               string                       `json:"observed_at"`
	State                    string                       `json:"state"` // absent, observed or unknown
	StateDirectory           string                       `json:"state_directory,omitempty"`
	HandoffSHA256            string                       `json:"handoff_sha256,omitempty"`
	StoreSHA256              string                       `json:"store_sha256,omitempty"`
	RecordedFinalizedIntents *int                         `json:"recorded_finalized_intents,omitempty"`
	RecordedAppliedIntents   *int                         `json:"recorded_applied_intents,omitempty"`
	Receipts                 []ProvisionalIntentReceiptV2 `json:"recorded_receipts,omitempty"`
	Error                    string                       `json:"error,omitempty"`
}

func ObserveProvisionalIntentsV2(ctx context.Context, options ProvisionalIntentObservationV2Options) (result *ProvisionalIntentObservationV2, resultErr error) {
	result = &ProvisionalIntentObservationV2{Scope: "local-runtime-observation", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "unknown"}
	defer func() {
		if resultErr != nil {
			result.State, result.Error = "unknown", fmt.Sprintf("%.1024s", resultErr.Error())
			result.Receipts, result.RecordedFinalizedIntents, result.RecordedAppliedIntents = nil, nil, nil
		}
	}()
	if ctx == nil || options.Hotkey == ([32]byte{}) || !filepath.IsAbs(options.ConfigPath) || filepath.Clean(options.ConfigPath) != options.ConfigPath || len(options.Handoff) == 0 || len(options.Handoff) > ProvisionalActivationSetupV2MaximumBytes || provisionalActivationSetupSHA256(options.Handoff) != options.HandoffSHA256 {
		return result, errors.New("local intent observation lacks its pinned runtime handoff")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var setup ProvisionalActivationSetupV2
	if err := json.Unmarshal(options.Handoff, &setup); err != nil {
		return result, err
	}
	canonical, err := json.MarshalIndent(setup, "", "  ")
	if err != nil || !bytes.Equal(options.Handoff, append(canonical, '\n')) || setup.ApprovedPlanHash != options.PlanHash || setup.DeploymentID != options.DeploymentID || setup.ValidatorID != options.ValidatorID {
		return result, errors.New("local intent observation handoff differs from its approved identity")
	}
	setup.contentHash = options.HandoffSHA256
	cfg, err := LoadReleaseConfig(options.ConfigPath)
	if err != nil {
		return result, err
	}
	if cfg.Netuid != options.Netuid {
		return result, errors.New("local intent observation netuid differs")
	}
	// This is the child's exact byte-pin and existing private-directory check;
	// only the returned in-memory state path is selected, never a fallback.
	if err := setup.validate(cfg, options.ConfigPath); err != nil {
		return result, err
	}
	result.StateDirectory, result.HandoffSHA256 = cfg.StateDir, options.HandoffSHA256
	limit := cfg.EvidenceV2.Bounds.IntentFileLimit()
	custody := &releaseEvidenceV2StartupReferences{remaining: limit}
	defer func() { resultErr = errors.Join(resultErr, custody.check(), custody.close(), ctx.Err()) }()
	encoded, err := custody.read(ctx, filepath.Join(cfg.StateDir, "steering-intents.json"), limit, true)
	if err != nil {
		return result, err
	}
	checkPublication := func() error {
		for _, name := range []string{releaseIntentV2Marker, releaseIntentV2Candidate} {
			_, err := custody.owners[0].directory.stat(name)
			if !releaseMeasurementInputV2OnlyMissing(err) {
				return errors.Join(errors.New("local intent publication is in progress or unresolved"), err)
			}
		}
		return nil
	}
	if err := checkPublication(); err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, checkPublication()) }()
	finalized, applied := 0, 0
	result.RecordedFinalizedIntents, result.RecordedAppliedIntents = &finalized, &applied
	if encoded == nil {
		result.State = "absent"
		return result, nil
	}
	var file steeringIntentFile
	if err := decodeAttemptStreamV2JSON(encoded, limit, &file); err != nil {
		return result, err
	}
	canonical, err = marshalAttemptSettlementV2JSON(ctx, &file, limit, true, true)
	if err != nil || !bytes.Equal(encoded, canonical) || file.Schema != SteeringIntentSchema || len(file.History) > 16384 {
		return result, errors.Join(errors.New("local intent store schema, bytes or census differs"), err)
	}
	all := append([]SteeringIntent(nil), file.History...)
	if file.Current != nil {
		all = append(all, *file.Current)
	}
	seen := make(map[string]bool, len(all))
	// Retained provisional testnet history can omit native submissions for
	// closed epochs. The production V2 runtime independently authenticates the
	// terminal records around such a gap; this observer should report that
	// retained transcript instead of converting it into an observation error.
	allowProvisionalGaps := provisionalClosedNativeInputEnabled(cfg)
	for index := range all {
		intent := &all[index]
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := validateSteeringIntentLifecycle(intent, index < len(file.History)); err != nil {
			return result, err
		}
		if intent.ValidatorID != cfg.ValidatorID || intent.Netuid != cfg.Netuid || intent.PolicyHash != cfg.PolicyHash || intent.Prepared.HotkeyHex != releaseHex32(options.Hotkey) || intent.Prepared.Netuid != cfg.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || !slices.Equal(intent.Prepared.UIDs, intent.UIDs) {
			return result, errors.New("local intent differs from its configured validator identity")
		}
		if err := intent.VerifyVectorHash(); err != nil {
			return result, err
		}
		if seen[intent.VectorHash] {
			return result, errors.New("local intent store repeats a vector")
		}
		seen[intent.VectorHash] = true
		if index > 0 {
			if err := validateSteeringIntentSuccessorWithGapsV2(&all[index-1], intent, allowProvisionalGaps); err != nil {
				return result, err
			}
		}
		if intent.Status == "finalized" || intent.Status == "applied" {
			finalized++
		}
		if intent.Status == "applied" {
			applied++
		}
		result.Receipts = append(result.Receipts, ProvisionalIntentReceiptV2{Status: intent.Status, SubnetEpoch: intent.SubnetEpoch, SettlementEpoch: intent.SettlementEpoch,
			VectorHash: intent.VectorHash, PreparedExtrinsicHash: intent.Prepared.ExtrinsicHash, PreparedAtBlock: intent.Prepared.PreparedAtBlock, PreparedAtBlockHash: intent.Prepared.PreparedAtBlockHash, UpdatedAt: intent.UpdatedAt,
			ExtrinsicHash: intent.ExtrinsicHash, FinalizedBlock: intent.FinalizedBlock, FinalizedBlockHash: intent.FinalizedBlockHash, RevealBlock: intent.RevealBlock,
			ApplicationBlock: intent.ApplicationBlock, ApplicationBlockHash: intent.ApplicationBlockHash})
	}
	result.State, result.StoreSHA256 = "observed", provisionalActivationSetupSHA256(encoded)
	return result, nil
}
