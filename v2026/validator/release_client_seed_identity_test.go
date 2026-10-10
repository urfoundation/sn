package validator

// A genuine constructor-bound ledger may share a key with another configured
// operator/domain. Key equality alone cannot select the runtime's ledger.

import (
	"os"
	"strings"
	"testing"
)

// The original preparation constructs and validates the real ledger; only
// the later configured runtime routing changes. No ledger field is fabricated.
func TestReleaseClientSeedContinuityRejectsDifferentPreparedDomain(t *testing.T) {
	var admitted []string
	for _, dimension := range []string{"deployment", "chain", "genesis", "netuid", "validator", "no-id"} {
		fixture := newReleaseClientSeedContinuityFixture(t)
		configuration := *fixture.cfg
		fixture.cfg = &configuration
		switch dimension {
		case "deployment":
			configuration.DeploymentID += "-another"
		case "chain":
			configuration.ChainID++
		case "genesis":
			configuration.GenesisHash = "0x" + strings.Repeat("12", 32)
		case "netuid":
			configuration.Netuid++
		case "validator":
			configuration.ValidatorID++
		case "no-id":
			fixture.op = configuration.Operators[1]
			if err := os.MkdirAll(fixture.op.StateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.op.ClientKeySeedFile, fixture.seed[:], 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := configuration.Validate(); err != nil {
			t.Fatalf("%s runtime configuration is not valid: %v", dimension, err)
		}
		observed, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
		if entries == 1 {
			if observed != fixture.publicKey {
				t.Fatal("routing fixture changed the genuine prepared public key")
			}
			admitted = append(admitted, dimension)
		} else if entries != 0 || err == nil || !strings.Contains(err.Error(), "prepared attempt ledger identity differs from operator configuration") {
			t.Fatalf("%s did not refuse its conflicting prepared ledger identity: %v", dimension, err)
		}
	}
	if len(admitted) != 0 {
		t.Fatalf("matching VPK admitted a different prepared operator domain: %v", admitted)
	}
}
