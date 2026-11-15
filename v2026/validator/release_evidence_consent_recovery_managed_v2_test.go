//go:build linux || darwin

package validator

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

type consentRecoveryManagedTestInputs struct {
	cfg        ReleaseConfig
	header     protocol.ValidatorEvidenceHeader
	activation ReleaseEvidenceV2ActivationContext
	expected   AttemptCutV2Context
	scratch    string
	keys       map[byte]ed25519.PublicKey
}

func newConsentRecoveryManagedTestInputs(t *testing.T) consentRecoveryManagedTestInputs {
	t.Helper()
	fixture := newReleasePublicationV2TestFixture(t, false)
	cfg := fixture.startup.cfg
	cfg.Operators = append([]OperatorConfig(nil), cfg.Operators[:1]...)
	cfg.EvidenceV2.Operators = append([]ReleaseEvidenceV2OperatorConfig(nil), cfg.EvidenceV2.Operators[:1]...)
	noId := cfg.Operators[0].NoID
	return consentRecoveryManagedTestInputs{cfg: cfg, header: fixture.publication.Members[0].Evidence.Header, activation: fixture.startup.inputs[0].Context, expected: fixture.expected[noId], scratch: filepath.Join(t.TempDir(), "replay"), keys: fixture.startup.keys[noId]}
}

func TestConsentRecoveryManagedSourceUsesOriginalActivationAndHistoricalPolicy(t *testing.T) {
	input := newConsentRecoveryManagedTestInputs(t)
	// A current rendered context may be different. Only the separately pinned
	// original public context selects this historical evidence header.
	input.cfg.EvidenceV2.Operators[0].Context = ReleaseEvidenceV2File{Path: filepath.Join(t.TempDir(), "synthetic-current-context.json"), SHA256: attemptHex32([32]byte{1}), Bytes: 1}
	op, policy, err := validateConsentRecoveryManagedV2Source(&input.cfg, input.header, input.activation, input.expected, input.scratch, input.keys)
	policyHash, hashErr := policy.Hash()
	if err != nil || hashErr != nil || op.NoID != input.header.NoID || policyHash != input.expected.Activation.Domain.PolicyHash {
		t.Fatalf("independently pinned original source was not preserved: op=%d err=%v", op.NoID, err)
	}
}

func TestConsentRecoveryManagedSourceRejectsChangedPublicIdentityBeforeKeysOrRpc(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*consentRecoveryManagedTestInputs)
	}{
		{name: "operator", mutate: func(i *consentRecoveryManagedTestInputs) { i.header.NoID++ }},
		{name: "historical domain", mutate: func(i *consentRecoveryManagedTestInputs) { i.header.Domain.PolicyHash[0] ^= 1 }},
		{name: "activation", mutate: func(i *consentRecoveryManagedTestInputs) { i.activation.Activation.Hotkey[0] ^= 1 }},
		{name: "boundary", mutate: func(i *consentRecoveryManagedTestInputs) { i.expected.Boundary.EVMBlock++ }},
		{name: "managed namespace", mutate: func(i *consentRecoveryManagedTestInputs) { i.cfg.DeploymentID = "synthetic-unexpected-deployment" }},
		{name: "scratch overlap", mutate: func(i *consentRecoveryManagedTestInputs) {
			i.scratch = filepath.Join(i.cfg.Operators[0].StateDir, "synthetic-replay")
		}},
	} {
		input := newConsentRecoveryManagedTestInputs(t)
		change.mutate(&input)
		input.cfg.RPC = []string{"http://192.0.2.1:1"}
		input.cfg.Operators[0].ClientKeySeedFile = filepath.Join(t.TempDir(), "synthetic-missing-key")
		result, closeManaged, err := OpenValidatorEvidenceConsentRecoveryV2(t.Context(), &input.cfg, input.header, input.activation, input.expected, input.scratch, input.keys)
		if err == nil || closeManaged != nil || result.Chain != nil || result.Census.Hotkey != nil || strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "synthetic-missing-key") {
			t.Fatalf("%s escaped public-only admission: result=%+v err=%v", change.name, result, err)
		}
	}
}

func TestConsentRecoveryManagedConfigPinsExactBytesBeforeNormalDecoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-config.yml")
	raw := []byte("synthetic_unknown_field: true\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	wrong := fmt.Sprintf("%x", sha256.Sum256([]byte("different synthetic configuration")))
	if result, err := LoadValidatorEvidenceConsentRecoveryConfig(path, wrong); err == nil || result != nil || !strings.Contains(err.Error(), "independent pin") {
		t.Fatalf("unpinned configuration reached normal decoding: result=%v err=%v", result, err)
	}
	correct := fmt.Sprintf("%x", sha256.Sum256(raw))
	if result, err := LoadValidatorEvidenceConsentRecoveryConfig(path, correct); err == nil || result != nil || strings.Contains(err.Error(), "independent pin") {
		t.Fatalf("pinned invalid YAML escaped normal complete loader: result=%v err=%v", result, err)
	}
}
