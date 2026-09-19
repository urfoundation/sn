package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This is an observation of retained runtime inputs, never a receipt claiming
// the current configuration was rendered. Strict acceptance still requires the
// pending current intent and its ordinary postcondition.
type provisionalConfigDeferral struct {
	Schema                    string       `json:"schema"`
	Provisional               bool         `json:"provisional"`
	FinalAcceptance           bool         `json:"final_acceptance"`
	CurrentActionVerified     bool         `json:"current_action_verified"`
	PlanHash                  string       `json:"plan_hash"`
	Action                    Action       `json:"deferred_action"`
	SourceReceipt             JournalEntry `json:"authenticated_predecessor_receipt"`
	SourceManifestHash        string       `json:"source_manifest_hash"`
	ObservedManifestHash      string       `json:"observed_retained_manifest_hash"`
	ObservedManifestBytesHash string       `json:"observed_retained_manifest_bytes_sha256"`
	ObservedConfigHash        string       `json:"observed_retained_config_hash"`
	CurrentConfigHash         string       `json:"current_config_hash"`
}

func (self *Executor) provisionalConfigRenderDeferral(action Action, entries []JournalEntry) (*provisionalConfigDeferral, error) {
	if self == nil || self.plan == nil || !provisionalResumeEnabled(self.cfg) || self.cfg.readOnlyAudit ||
		self.cfg.provisionalResume.Record.PlanHash != self.plan.PlanHash || !filepath.IsAbs(self.cfg.provisionalResume.RecordPath) ||
		action.ID != "config.render" || action.Kind != "local" || !spendIsZero(action.Spend) {
		return nil, errors.New("provisional config deferral lacks its exact invocation and local action")
	}
	allowed := self.plan.allowedPlanHashes()
	var sourceEntry *JournalEntry
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.ActionID == action.ID && entry.Stage == StageVerified && allowed[entry.PlanHash] && entry.PlanHash != self.plan.PlanHash {
			sourceEntry = &entry
			break
		}
	}
	if sourceEntry == nil {
		return nil, errors.New("provisional config deferral has no verified predecessor render")
	}
	source, err := readValidatorEvidenceHistoricalPlan(self.stateDir, sourceEntry.PlanHash)
	if err != nil {
		return nil, err
	}
	var sourceAction *Action
	for index := range source.Actions {
		candidate := &source.Actions[index]
		if candidate.ID == action.ID && actionAcceptsIntent(*candidate, sourceEntry.IntentHash) {
			sourceAction = candidate
			break
		}
	}
	if sourceAction == nil || sourceAction.Kind != "local" || !spendIsZero(sourceAction.Spend) ||
		source.DeploymentID != self.plan.DeploymentID || source.ChainID != self.plan.ChainID || source.GenesisHash != self.plan.GenesisHash || source.Netuid != self.plan.Netuid ||
		source.PolicyHash != self.plan.PolicyHash || sourceAction.Parameters["deployment_manifest_hash"] != action.Parameters["deployment_manifest_hash"] {
		return nil, errors.New("provisional config deferral predecessor has another deployment or policy")
	}
	receipt, err := self.readPersistedPostcondition(*sourceEntry)
	if err != nil {
		return nil, err
	}
	sourceManifestHash, ok := receipt.Observed["runtime_config_manifest_hash"].(string)
	if !ok || !validCanonicalHashHex(sourceManifestHash) {
		return nil, errors.New("provisional config deferral predecessor has no authenticated manifest identity")
	}
	raw, err := readValidatorEvidenceHistoricalFile(self.stateDir, "runtime-config-manifest.json", 4*1024*1024)
	if err != nil {
		return nil, err
	}
	var manifest RuntimeConfigManifest
	if err := decodeStrictJSONBytes(raw, &manifest); err != nil {
		return nil, err
	}
	hash, err := runtimeConfigManifestHash(manifest)
	if err != nil || !strings.EqualFold(hash, manifest.ManifestHash) || manifest.Schema != runtimeConfigManifestSchema ||
		manifest.DeploymentID != source.DeploymentID || manifest.ConfigHash != source.ConfigHash || manifest.PolicyHash != source.PolicyHash {
		return nil, stateMismatchError(err, "provisional retained runtime manifest identity differs from its predecessor deployment")
	}
	return &provisionalConfigDeferral{
		Schema: "urnetwork-sim-provisional-config-deferral-v1", Provisional: true,
		PlanHash: self.plan.PlanHash, Action: action, SourceReceipt: *sourceEntry,
		SourceManifestHash: sourceManifestHash, ObservedManifestHash: hash, ObservedManifestBytesHash: bytesSHA256(raw),
		ObservedConfigHash: manifest.ConfigHash, CurrentConfigHash: self.plan.ConfigHash,
	}, nil
}

func (self *Executor) deferProvisionalConfigRender(action Action) error {
	record, err := self.provisionalConfigRenderDeferral(action, self.journal.Entries())
	if err != nil {
		return err
	}
	wire, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "config-render-deferred.json")
	if err := atomicWrite(path, append(wire, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "sim-testnet: config.render deferred for retained live topology; current_action_verified=false; runtime files unchanged; final_acceptance=false")
	return nil
}
