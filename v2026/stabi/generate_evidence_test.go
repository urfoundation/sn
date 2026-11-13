// The evidence companion is a required production binding, not an optional
// artifact that may be silently borrowed from another Foundry build.
package stabi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Removing only the new artifact must fail the real generator despite the
// other four inputs and all checked-in bindings being present.
func TestGenerateRequiresValidatorEvidenceArtifact(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
	artifact := filepath.Join(environment.artifactsDir, "STValidatorEvidence.sol", "STValidatorEvidence.json")
	if err := os.Remove(artifact); err != nil {
		t.Fatal(err)
	}
	output, err := environment.run(t, "")
	if err == nil || !strings.Contains(string(output), "STValidatorEvidence") {
		t.Fatalf("missing evidence artifact accepted or wrong refusal: %v\n%s", err, output)
	}
}

// A private build with changed evidence methods cannot pass by checking only
// the earlier coordinator, vault, reserve and legacy subnet artifacts.
func TestGenerateChecksValidatorEvidenceABI(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
	artifact := filepath.Join(environment.artifactsDir, "STValidatorEvidence.sol", "STValidatorEvidence.json")
	if err := os.WriteFile(artifact, []byte(`{"abi":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := environment.run(t, "")
	if err == nil || !strings.Contains(string(output), "generated binding/ABI is stale: ../evm/abi/STValidatorEvidence.abi.json") {
		t.Fatalf("evidence ABI drift accepted or wrong refusal: %v\n%s", err, output)
	}
}
