//go:build linux || darwin

package validator

// The child validates capacity through the same pinned handoff as activation.
import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Exercise the actual handoff validator without network or ledger replay.
func TestProvisionalActivationSourceBoundsPreserveOriginalConfig(t *testing.T) {
	cfg := validReleaseConfig(t)
	path := writeReleaseConfig(t, cfg)
	raw, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	original := cfg.EvidenceV2.Bounds
	approved, err := DoubledReleaseEvidenceV2SourceBounds(original)
	if err != nil { t.Fatal(err) }
	setup := ProvisionalActivationSetupV2{
		Schema: ProvisionalActivationSetupV2Schema, Provisional: true, DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID,
		ApprovedPlanHash: provisionalIntentPlanHash("51"), SourcePlanHash: provisionalIntentPlanHash("52"),
		PreparedSHA256: provisionalActivationSetupSHA256([]byte("synthetic prepared")), CompletedSHA256: provisionalActivationSetupSHA256([]byte("synthetic completed")), ConfigSHA256: provisionalActivationSetupSHA256(raw),
		SourceBounds: &ReleaseEvidenceV2SourceBounds{Original: original, Approved: approved},
	}
	for _, action := range []string{"evidence.activate.1.1", "evidence.activate.1.2", "evidence.activate.2.1", "evidence.activate.2.2", "evidence.activation-boundary"} {
		setup.Receipts = append(setup.Receipts, ProvisionalActivationSetupV2Receipt{ActionID: action, PostconditionHash: provisionalIntentPlanHash("53"), SHA256: provisionalActivationSetupSHA256([]byte(action))})
	}
	for _, operator := range cfg.EvidenceV2.Operators {
		setup.Members = append(setup.Members, ProvisionalActivationSetupV2Member{NoID: operator.NoID, ContextSHA256: operator.Context.SHA256, PublishedBlock: 17})
	}
	encoded, err := json.MarshalIndent(setup, "", "  ")
	if err != nil { t.Fatal(err) }
	setup.contentHash = provisionalActivationSetupSHA256(append(encoded, '\n'))
	if err := setup.validate(&cfg, path); err != nil { t.Fatal("retained child did not receive approved capacity", err) }
	if cfg.EvidenceV2.Bounds != approved || !cfg.ProvisionalDeferClosedNativeInput { t.Fatal("retained child lost its approved startup bounds") }
	if err := setup.validate(&cfg, path); err != nil || cfg.EvidenceV2.Bounds != approved { t.Fatal("repeated handoff multiplied capacity", err) }
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) { t.Fatal("handoff rewrote retained config", err) }
	for _, fault := range []string{"original", "approved", "record-policy", "rendered", "config-pin", "final-acceptance"} {
		changed := setup
		bounds := *setup.SourceBounds
		changed.SourceBounds = &bounds
		candidate := cfg
		candidate.EvidenceV2.Bounds = original
		switch fault {
		case "original": bounds.Original.Disk.MaxRecordCount++
		case "approved": bounds.Approved.Disk.MaxRecordCount++
		case "record-policy": bounds.Approved.Disk.MaxRecordBytes++
		case "rendered": candidate.EvidenceV2.Bounds.Disk.MaxRecordCount++
		case "config-pin": changed.ConfigSHA256 = provisionalActivationSetupSHA256([]byte("changed"))
		case "final-acceptance": changed.FinalAcceptance = true
		}
		before := candidate
		if err := changed.validate(&candidate, path); err == nil || !reflect.DeepEqual(candidate, before) { t.Fatal("invalid handoff changed child capacity", fault, err) }
	}
	legacy := setup
	legacy.SourceBounds = nil
	cfg.EvidenceV2.Bounds = original
	if err := legacy.validate(&cfg, path); err != nil || cfg.EvidenceV2.Bounds != original { t.Fatal("ordinary retained handoff gained capacity", err) }
}
