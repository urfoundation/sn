// Synthetic460 responses exercise the complete authenticated schedule path.
package crv4

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Weighted stake, exact threshold equality and registered-owner eligibility
// retain their independent current-artifact and exact-block checks.
func TestRuntimeArtifactMetadataValidatorScheduleRuntime460(t *testing.T) {
	for _, example := range []struct {
		name      string
		threshold uint64
		eligible  bool
		change    func(*testing.T, *validatorStakeTestFixture)
	}{
		{name: "threshold equality", threshold: 150, eligible: true},
		{name: "below threshold", threshold: 151, eligible: false},
		{name: "missing permit", threshold: 100, eligible: false, change: func(t *testing.T, f *validatorStakeTestFixture) {
			f.fields[57] = []byte{12, 0, 0, 0}
			f.identity.storage["ValidatorPermit"] = validatorIdentityTestHex(f.fields[57])
		}},
		{name: "registered owner", threshold: 151, eligible: true, change: func(t *testing.T, f *validatorStakeTestFixture) {
			f.fields[57] = []byte{12, 0, 0, 0}
			f.identity.storage["ValidatorPermit"] = validatorIdentityTestHex(f.fields[57])
			uid := f.identity.query.UID
			f.setOwner(t, f.identity.hotkey, &uid)
		}},
		{name: "unregistered owner", threshold: 151, eligible: false, change: func(t *testing.T, f *validatorStakeTestFixture) {
			f.setOwner(t, [32]byte{44}, nil)
		}},
		{name: "absent threshold default", threshold: 0, eligible: true, change: func(t *testing.T, f *validatorStakeTestFixture) {
			f.identity.storage["StakeThreshold"] = json.RawMessage("null")
		}},
	} {
		fixture, query := newValidatorScheduleTestFixture(t)
		identity := fixture.identity
		identity.version.SpecVersion = 460
		identity.allowed[0].Version = identity.version
		identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, example.threshold))
		if example.change != nil {
			example.change(t, fixture)
		}
		fixture.publish(t)
		metadata, runtime := identity.chain.Meta, identity.chain.Runtime
		result, err := ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
		if err != nil || result.SubnetEpochIndex != 77 || result.Stake.Identity.UID != 1 ||
			result.Stake.Identity.Hotkey != query.Hotkey || result.Stake.Identity.BlockHash != query.BlockHash ||
			result.Stake.Identity.Runtime != identity.allowed[0] || result.Stake.TotalStakeRao != 150 ||
			result.Stake.StakeThresholdRao != example.threshold || result.Stake.MeetsNonSelfStakeAndPermit() != example.eligible || fixture.runtimeCalls != 1 {
			t.Fatalf("runtime460 %s lost authenticated schedule semantics: %+v error=%v", example.name, result, err)
		}
		if identity.chain.Meta != metadata || identity.chain.Runtime != runtime {
			t.Fatal("runtime460 schedule changed the signing binding")
		}
	}
}

// Layout compatibility does not authorize another artifact or unknown version.
func TestRuntimeArtifactMetadataValidatorStakeRuntime460RejectsForeignIdentity(t *testing.T) {
	for _, example := range []struct {
		name   string
		change func(*validatorIdentityTestFixture)
	}{
		{name: "prior authority", change: func(f *validatorIdentityTestFixture) { f.allowed[0].Version.SpecVersion = 455 }},
		{name: "code", change: func(f *validatorIdentityTestFixture) { f.allowed[0].CodeHash = types.Hash{88}.Hex() }},
		{name: "metadata", change: func(f *validatorIdentityTestFixture) { f.allowed[0].MetadataHash = types.Hash{77}.Hex() }},
		{name: "unreviewed456", change: func(f *validatorIdentityTestFixture) { f.version.SpecVersion = 456; f.allowed[0].Version = f.version }},
		{name: "unreviewed457", change: func(f *validatorIdentityTestFixture) { f.version.SpecVersion = 457; f.allowed[0].Version = f.version }},
		{name: "future461", change: func(f *validatorIdentityTestFixture) { f.version.SpecVersion = 461; f.allowed[0].Version = f.version }},
		{name: "name", change: func(f *validatorIdentityTestFixture) {
			f.version.SpecName = "synthetic-foreign"
			f.allowed[0].Version = f.version
		}},
		{name: "transaction", change: func(f *validatorIdentityTestFixture) {
			f.version.TransactionVersion = 2
			f.allowed[0].Version = f.version
		}},
		{name: "state", change: func(f *validatorIdentityTestFixture) { f.version.StateVersion = 2; f.allowed[0].Version = f.version }},
	} {
		fixture := newValidatorStakeTestFixture(t)
		fixture.identity.version.SpecVersion = 460
		fixture.identity.allowed[0].Version = fixture.identity.version
		example.change(fixture.identity)
		result, err := fixture.read()
		if err == nil || result != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 0 {
			t.Fatalf("%s reached the460 stake decoder: %+v error=%v calls=%d", example.name, result, err, fixture.runtimeCalls)
		}
	}
}
