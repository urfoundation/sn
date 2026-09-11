//go:build linux || darwin

package validator

// The simulator may explicitly reuse its authenticated completed testnet
// setup. This is retained approval, not a new historical chain observation.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfoundation/sn/protocol"
)

const ProvisionalActivationSetupV2Schema = "urnetwork-sim-provisional-activation-setup-v1"
const ProvisionalActivationSetupV2MaximumBytes = 64 * 1024

type ProvisionalActivationSetupV2Receipt struct {
	ActionID          string `json:"action_id"`
	PostconditionHash string `json:"postcondition_hash"`
	SHA256            string `json:"sha256"`
}

type ProvisionalActivationSetupV2Member struct {
	NoID           uint64 `json:"no_id"`
	ContextSHA256  string `json:"context_sha256"`
	PublishedBlock uint64 `json:"published_block"`
}

// The private file is pinned by the simulator's exact supervisor arguments.
// Hashes name the original approved artifacts; they are not eligibility values.
type ProvisionalActivationSetupV2 struct {
	Schema              string                                `json:"schema"`
	Provisional         bool                                  `json:"provisional"`
	FinalAcceptance     bool                                  `json:"final_acceptance"`
	DeploymentID        string                                `json:"deployment_id"`
	ValidatorID         uint64                                `json:"validator_id"`
	ApprovedPlanHash    string                                `json:"approved_plan_hash"`
	SourcePlanHash      string                                `json:"source_plan_hash"`
	PreparedSHA256      string                                `json:"prepared_sha256"`
	CompletedSHA256     string                                `json:"completed_sha256"`
	ConfigSHA256        string                                `json:"config_sha256"`
	CoordinatorStateDir string                                `json:"coordinator_state_dir,omitempty"`
	Receipts            []ProvisionalActivationSetupV2Receipt `json:"receipts"`
	Members             []ProvisionalActivationSetupV2Member  `json:"members"`
	contentHash         string
}

// This entry point requires an explicit hash-pinned simulator handoff. The
// ordinary RunRelease entry point never selects retained activation admission.
func RunReleaseWithProvisionalActivationSetup(ctx context.Context, configPath string, encoded []byte, expectedSHA256 string) error {
	if len(encoded) == 0 || len(encoded) > ProvisionalActivationSetupV2MaximumBytes || provisionalActivationSetupSHA256(encoded) != expectedSHA256 {
		return errors.New("provisional activation handoff bytes differ from the supervisor pin")
	}
	var setup ProvisionalActivationSetupV2
	if err := json.Unmarshal(encoded, &setup); err != nil {
		return err
	}
	canonical, err := json.MarshalIndent(setup, "", "  ")
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return errors.New("provisional activation handoff is not canonical")
	}
	setup.contentHash = expectedSHA256
	return runReleaseWithActivationSetup(ctx, configPath, &setup)
}

func provisionalActivationSetupSHA256(encoded []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded))
}

func (self *ProvisionalActivationSetupV2) validate(cfg *ReleaseConfig, configPath string) error {
	if self == nil || cfg == nil || self.Schema != ProvisionalActivationSetupV2Schema || !self.Provisional || self.FinalAcceptance || cfg.ChainID != 945 || cfg.GenesisHash != "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105" || cfg.Policy.NetworkProfile != "testnet" || self.DeploymentID != cfg.DeploymentID || self.ValidatorID != cfg.ValidatorID || len(self.Members) != len(cfg.EvidenceV2.Operators) || len(self.Members) != 2 || self.ValidatorID < 1 || self.ValidatorID > 2 || len(self.Receipts) != 5 || self.ApprovedPlanHash == self.SourcePlanHash {
		return errors.New("provisional activation handoff differs from its retained testnet deployment")
	}
	for _, hash := range []string{self.ApprovedPlanHash, self.SourcePlanHash} {
		if _, err := canonicalAttemptHex32("provisional activation plan", hash, false); err != nil {
			return err
		}
	}
	for _, hash := range []string{self.PreparedSHA256, self.CompletedSHA256, self.ConfigSHA256, self.contentHash} {
		if len(hash) != 71 || hash[:7] != "sha256:" {
			return errors.New("provisional activation artifact hash is invalid")
		}
		if _, err := canonicalAttemptHex32("provisional activation artifact", "0x"+hash[7:], false); err != nil {
			return err
		}
	}
	wantReceipts := map[string]bool{"evidence.activate.1.1": true, "evidence.activate.1.2": true, "evidence.activate.2.1": true, "evidence.activate.2.2": true, "evidence.activation-boundary": true}
	for _, receipt := range self.Receipts {
		if !wantReceipts[receipt.ActionID] || len(receipt.SHA256) != 71 || receipt.SHA256[:7] != "sha256:" {
			return errors.New("provisional activation receipt census differs")
		}
		delete(wantReceipts, receipt.ActionID)
		for _, hash := range []string{receipt.PostconditionHash, "0x" + receipt.SHA256[7:]} {
			if _, err := canonicalAttemptHex32("provisional activation receipt", hash, false); err != nil {
				return err
			}
		}
	}
	for index, member := range self.Members {
		if member.NoID != cfg.EvidenceV2.Operators[index].NoID || member.ContextSHA256 != cfg.EvidenceV2.Operators[index].Context.SHA256 || member.PublishedBlock == 0 {
			return errors.New("provisional activation context differs from its configured pair")
		}
	}
	encoded, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if provisionalActivationSetupSHA256(encoded) != self.ConfigSHA256 {
		return errors.New("provisional activation validator config differs from its supervisor handoff")
	}
	if self.CoordinatorStateDir != "" {
		if self.CoordinatorStateDir != filepath.Join(filepath.Dir(configPath), "coordinator-state-v2") {
			return errors.New("provisional coordinator state is outside its original validator owner")
		}
		owner, err := openAttemptPrivateDirectory(self.CoordinatorStateDir)
		if err != nil {
			return err
		}
		private := owner.anchor.uid == uint32(os.Geteuid()) && owner.anchor.mode&0o7777 == 0o700
		if err := errors.Join(owner.check(), owner.close()); err != nil {
			return err
		}
		if !private {
			return errors.New("provisional coordinator state is not an existing private directory")
		}
		// Older running validators kept coordinator history beside operator
		// state. Restore that exact owner without moving any history or stats.
		cfg.StateDir = self.CoordinatorStateDir
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Only the locally admitted, hash-pinned handoff can construct this value.
// It contains no fabricated native stake or VerifiedReleaseActivationV2 result.
type provisionalReleaseActivationV2Admission struct {
	setupSHA256  string
	activation   protocol.ValidatorEvidenceActivation
	publication  uint64
	boundary     uint64
	boundaryHash [32]byte
}

func (self *ProvisionalActivationSetupV2) admit(input *releaseEvidenceV2ActivationInput) error {
	for _, member := range self.Members {
		if member.NoID != input.Config.NoID {
			continue
		}
		if member.ContextSHA256 != input.Config.Context.SHA256 || member.PublishedBlock <= input.Candidate.EVMBlock || member.PublishedBlock > input.Context.InitialCut.Boundary.EVMBlock || member.PublishedBlock > input.Context.ObservedEVMBlock {
			return errors.New("provisional activation publication differs from the authenticated original context")
		}
		input.retainedSetup = &provisionalReleaseActivationV2Admission{setupSHA256: self.contentHash, activation: input.Candidate, publication: member.PublishedBlock, boundary: input.Context.ObservedEVMBlock, boundaryHash: input.Context.ObservedEVMHash}
		return nil
	}
	return errors.New("provisional activation pair is absent from the retained handoff")
}

func (self *provisionalReleaseActivationV2Admission) matches(record protocol.ValidatorEvidenceActivation) bool {
	return self != nil && self.setupSHA256 != "" && self.activation == record && self.publication > record.EVMBlock && self.boundary >= self.publication && self.boundaryHash != ([32]byte{})
}

// Explicit provisional continuation replays every retained signed record and
// actual ledger locally, but does not repeat its historical external audits.
// Every configured input must belong to the same validated private handoff.
// New runtime observations never call this startup-only admission helper.
func provisionalRetainedStartupHistory(inputs []releaseEvidenceV2ActivationInput) bool {
	if len(inputs) == 0 || inputs[0].retainedSetup == nil {
		return false
	}
	setupHash := inputs[0].retainedSetup.setupSHA256
	for _, input := range inputs {
		if !input.retainedSetup.matches(input.Candidate) || input.retainedSetup.setupSHA256 != setupHash || input.Candidate != input.Context.Activation || input.Config.NoID != input.Candidate.NoID {
			return false
		}
	}
	return true
}
