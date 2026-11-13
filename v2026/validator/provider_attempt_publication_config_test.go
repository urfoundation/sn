// Explicit scope YAML joins the same signed JSON domain without changing its
// canonical receipt bytes. Optional evidence is admitted by its own owner.
package validator

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"gopkg.in/yaml.v3"
)

// The public strict loader accepts the documented nested snake-case keys.
func TestProviderRequestConfigLoadsCanonicalReceiptScope(t *testing.T) {
	cfg := validReleaseConfig(t)
	scope := protocol.ProviderAttemptReceiptScope{Profile: "testnet", GenesisHash: [32]byte{1}, DeploymentId: "synthetic-deployment", DeploymentKey: "945:0x1111111111111111111111111111111111111111", PolicyHash: [32]byte{2}, Netuid: 7, NoId: cfg.Operators[0].NoID}
	cfg.Operators[0].RequestReceiptScope = &scope
	cfg.Operators[0].RequestPreparation = &ReleaseEvidenceV2File{Path: filepath.Join(t.TempDir(), "original.json"), Bytes: 128, SHA256: attemptHex32(sha256.Sum256([]byte("synthetic-original")))}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"request_receipt_scope:", "genesis_hash:", "deployment_id:", "deployment_key:", "policy_hash:", "no_id:"} {
		if !bytes.Contains(raw, []byte(key)) {
			t.Fatal("canonical receipt YAML key omitted", key)
		}
	}
	for _, key := range []string{"genesishash:", "deploymentid:", "deploymentkey:", "policyhash:", "noid:"} {
		if bytes.Contains(raw, []byte(key)) {
			t.Fatal("implicit receipt YAML spelling escaped", key)
		}
	}
	path := filepath.Join(t.TempDir(), "validator.yml")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadReleaseConfig(path)
	if err != nil || got == nil || got.Operators[0].RequestReceiptScope == nil || *got.Operators[0].RequestReceiptScope != scope {
		t.Fatal("actual strict receipt scope loader", err)
	}
	expected := `{"profile":"testnet","genesis_hash":`
	encoded, err := json.Marshal(scope)
	if err != nil || !strings.HasPrefix(string(encoded), expected) || bytes.Contains(encoded, []byte("yaml")) {
		t.Fatal("receipt JSON grammar changed", err)
	}
}
