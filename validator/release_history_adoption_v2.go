//go:build linux || darwin

package validator

// An explicit source pin permits adoption of real sparse history, never a
// historical audit waiver. Every chain, cut, decision and receipt still enters
// ordinary strict startup. The one fresh epoch is fixed before workers start.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const ReleaseHistoryAdoptionV2Schema = "urnetwork-validator-history-adoption-v2"
const ReleaseHistoryAdoptionV2MaximumBytes = 64 * 1024

// This is an approval request, not a verification receipt. The simulator pins
// its canonical bytes in the owned validator argv after approving the new plan.
type ReleaseHistoryAdoptionV2 struct {
	Schema              string `json:"schema"`
	DeploymentID        string `json:"deployment_id"`
	ValidatorID         uint64 `json:"validator_id"`
	ApprovedPlanHash    string `json:"approved_plan_hash"`
	SourcePlanHash      string `json:"source_plan_hash"`
	ConfigSHA256        string `json:"config_sha256"`
	CoordinatorStateDir string `json:"coordinator_state_dir"`
	IntentPrefixSHA256  string `json:"intent_prefix_sha256"`
	IntentPrefixCount   uint64 `json:"intent_prefix_count"`
	LastNativeEpoch     uint64 `json:"last_native_epoch"`
	LastArtifactHash    string `json:"last_artifact_hash,omitempty"`
	FirstNativeEpoch    uint64 `json:"first_native_epoch"`
}

// Neither decoded request data nor a field on disk can construct this owner.
// Prefix matching precedes all replay; runtime publication follows every
// ordinary strict reader and joined Close. The original byte pin never changes.
type releaseHistoryAdoptionV2 struct {
	request ReleaseHistoryAdoptionV2
	prefix  []releaseHistoryAdoptionIntentV2
}

type releaseHistoryAdoptionIntentV2 struct {
	native, settlement uint64
	vector, artifact   string
}

func RunReleaseWithHistoryAdoptionV2(ctx context.Context, configPath string, encoded []byte, expectedSHA256 string) error {
	request, err := DecodeReleaseHistoryAdoptionV2(encoded, expectedSHA256)
	if err != nil {
		return err
	}
	return runReleaseWithStartupV2(ctx, configPath, nil, request)
}

// Decoding verifies the caller's exact pin and finite canonical wire. It
// grants no file, replay, application or future-gap authority by itself.
func DecodeReleaseHistoryAdoptionV2(encoded []byte, expectedSHA256 string) (*ReleaseHistoryAdoptionV2, error) {
	if len(encoded) == 0 || len(encoded) > ReleaseHistoryAdoptionV2MaximumBytes || provisionalActivationSetupSHA256(encoded) != expectedSHA256 {
		return nil, errors.New("strict history adoption bytes differ from the approved pin")
	}
	var request ReleaseHistoryAdoptionV2
	if err := json.Unmarshal(encoded, &request); err != nil {
		return nil, err
	}
	canonical, err := json.MarshalIndent(request, "", "  ")
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.New("strict history adoption request is not canonical")
	}
	if request.Schema != ReleaseHistoryAdoptionV2Schema || request.DeploymentID == "" || request.ValidatorID == 0 || request.IntentPrefixCount > 16384 || request.FirstNativeEpoch == 0 || request.FirstNativeEpoch == ^uint64(0) || request.FirstNativeEpoch <= request.LastNativeEpoch || request.ApprovedPlanHash == request.SourcePlanHash {
		return nil, errors.New("strict history adoption scope is incomplete")
	}
	for _, value := range []string{request.ApprovedPlanHash, request.SourcePlanHash} {
		if _, err := canonicalAttemptHex32("strict history adoption plan", value, false); err != nil {
			return nil, err
		}
	}
	for _, value := range []string{request.ConfigSHA256, request.IntentPrefixSHA256} {
		if _, err := parseReleaseContentHash(value); err != nil {
			return nil, err
		}
	}
	if request.IntentPrefixCount == 0 {
		if request.LastNativeEpoch != 0 || request.LastArtifactHash != "" || request.IntentPrefixSHA256 != ReleaseMeasurementContentHash(nil) {
			return nil, errors.New("empty adoption prefix does not bind actual absence")
		}
	} else if request.LastNativeEpoch == 0 {
		return nil, errors.New("retained adoption prefix has no terminal native epoch")
	} else if _, err := parseReleaseContentHash(request.LastArtifactHash); err != nil {
		return nil, err
	}
	return &request, nil
}

func (request *ReleaseHistoryAdoptionV2) configure(cfg *ReleaseConfig, configPath string) error {
	if request == nil || cfg == nil || cfg.ProvisionalDeferClosedNativeInput || cfg.ChainID != 945 || cfg.Policy.NetworkProfile != "testnet" || cfg.GenesisHash != "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105" || request.DeploymentID != cfg.DeploymentID || request.ValidatorID != cfg.ValidatorID {
		return errors.New("strict history adoption differs from its configured testnet owner")
	}
	if !filepath.IsAbs(configPath) || filepath.Clean(configPath) != configPath || request.CoordinatorStateDir != filepath.Join(filepath.Dir(configPath), "coordinator-state-v2") {
		return errors.New("strict history adoption changes the retained coordinator namespace")
	}
	encoded, err := os.ReadFile(configPath)
	if err != nil || ReleaseMeasurementContentHash(encoded) != request.ConfigSHA256 {
		return errors.Join(errors.New("strict history adoption validator config changed"), err)
	}
	owned, err := decodeReleaseConfigBytes(configPath, encoded)
	if err != nil {
		return err
	}
	if owned.ProvisionalDeferClosedNativeInput || owned.DeploymentID != request.DeploymentID || owned.ValidatorID != request.ValidatorID || owned.ChainID != 945 || owned.Policy.NetworkProfile != "testnet" || owned.GenesisHash != "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105" {
		return errors.New("strict history adoption pinned config changed its owner or mode")
	}
	owner, err := openAttemptPrivateDirectory(request.CoordinatorStateDir)
	if err != nil {
		return err
	}
	private := owner.anchor.uid == uint32(os.Geteuid()) && owner.anchor.mode&0o7777 == 0o700
	if err := errors.Join(owner.check(), owner.close()); err != nil {
		return err
	}
	if !private {
		return errors.New("strict history adoption requires its existing private coordinator directory")
	}
	copy := *request
	owned.StateDir, owned.historyAdoptionV2 = request.CoordinatorStateDir, &copy
	*cfg = *owned
	return cfg.Validate()
}

// Reconstruct the original canonical file from the retained terminal prefix,
// including lifecycle receipts and timestamps. Later append-only progress may
// extend it; rewriting or dropping an original intent cannot satisfy the pin.
func (request *ReleaseHistoryAdoptionV2) matchPrefix(ctx context.Context, file *steeringIntentFile, encoded []byte, limit uint64) (*releaseHistoryAdoptionV2, error) {
	if request == nil {
		return nil, nil
	}
	if ctx == nil || file == nil || file.Schema != steeringIntentSchema || limit == 0 {
		return nil, errors.New("strict history adoption prefix owner is incomplete")
	}
	all := make([]*SteeringIntent, 0, len(file.History)+1)
	for index := range file.History {
		all = append(all, &file.History[index])
	}
	if file.Current != nil {
		all = append(all, file.Current)
	}
	if uint64(len(all)) < request.IntentPrefixCount {
		return nil, errors.New("strict history adoption lost an original intent")
	}
	owner := &releaseHistoryAdoptionV2{request: *request}
	if request.IntentPrefixCount == 0 {
		if len(all) == 0 && encoded != nil {
			return nil, errors.New("strict history adoption absence was replaced by a file")
		}
		if len(all) != 0 && all[0].SubnetEpoch != request.FirstNativeEpoch {
			return nil, errors.New("strict history adoption first intent differs from its fixed epoch")
		}
		return owner, ctx.Err()
	}
	count := int(request.IntentPrefixCount)
	prefix := steeringIntentFile{Schema: steeringIntentSchema, Current: all[count-1]}
	if count > 1 {
		prefix.History = make([]SteeringIntent, count-1)
		for index := range prefix.History {
			prefix.History[index] = *all[index]
		}
	}
	raw, err := marshalAttemptSettlementV2JSON(ctx, &prefix, limit, true, true)
	if err != nil || ReleaseMeasurementContentHash(raw) != request.IntentPrefixSHA256 {
		return nil, errors.Join(errors.New("strict history adoption original intent bytes changed"), err)
	}
	for index, intent := range all[:count] {
		if intent.Status != "applied" || intent.Prepared == nil || intent.Prepared.SourceCommitment == nil {
			return nil, errors.New("strict history adoption requires applied V2 source intents")
		}
		// The containing strict reader still verifies the complete lifecycle,
		// vector hash, signed measurement and actual native source afterward.
		if index > 0 {
			if err := validateSteeringIntentSuccessorWithGapsV2(all[index-1], intent, true); err != nil {
				return nil, err
			}
		}
		owner.prefix = append(owner.prefix, releaseHistoryAdoptionIntentV2{native: intent.SubnetEpoch, settlement: intent.SettlementEpoch, vector: intent.VectorHash, artifact: intent.MeasurementArtifactHash})
	}
	last := owner.prefix[len(owner.prefix)-1]
	if last.native != request.LastNativeEpoch || last.artifact != request.LastArtifactHash {
		return nil, errors.New("strict history adoption terminal intent differs")
	}
	return owner, ctx.Err()
}

func (self *releaseHistoryAdoptionV2) allowsIntentEdge(previous, current *SteeringIntent) bool {
	if self == nil || previous == nil || current == nil {
		return false
	}
	for index := 1; index < len(self.prefix); index++ {
		before, after := self.prefix[index-1], self.prefix[index]
		if previous.VectorHash == before.vector && current.VectorHash == after.vector && previous.MeasurementArtifactHash == before.artifact && current.MeasurementArtifactHash == after.artifact {
			return true
		}
	}
	return previous.SubnetEpoch == self.request.LastNativeEpoch && previous.MeasurementArtifactHash == self.request.LastArtifactHash && current.SubnetEpoch == self.request.FirstNativeEpoch
}

func (self *releaseHistoryAdoptionV2) allowsArtifactEdge(previousHash string, previous, current *ReleaseMeasurementArtifact) bool {
	if self == nil || previous == nil || current == nil {
		return false
	}
	encoded, err := canonicalReleaseMeasurementBytes(current)
	if err != nil {
		return false
	}
	currentHash := ReleaseMeasurementContentHash(encoded)
	for index := 1; index < len(self.prefix); index++ {
		if self.prefix[index-1].artifact == previousHash && self.prefix[index].artifact == currentHash {
			return true
		}
	}
	return previousHash == self.request.LastArtifactHash && previous.SubnetEpoch == self.request.LastNativeEpoch && current.SubnetEpoch == self.request.FirstNativeEpoch
}

func (self *releaseHistoryAdoptionV2) requireFirstEpoch(current *SteeringIntent, epoch uint64) error {
	if self == nil || current != nil && current.SubnetEpoch >= self.request.FirstNativeEpoch {
		return nil
	}
	if epoch < self.request.FirstNativeEpoch {
		return ErrSteeringAlreadyFinal
	}
	if epoch != self.request.FirstNativeEpoch {
		return fmt.Errorf("strict history adoption missed its approved first native epoch %d", self.request.FirstNativeEpoch)
	}
	return nil
}
