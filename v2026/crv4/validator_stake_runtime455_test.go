package crv4

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Activation calls the complete schedule reader with the configured current
// artifact. These local RPC fixtures authenticate 455 and then retain the
// same weighted-stake/permit/registered-owner rules as the reviewed 454 source.
func TestRuntimeArtifactMetadataValidatorScheduleRuntime455(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name      string
		threshold uint64
		eligible  bool
		change    func(*testing.T, *validatorStakeTestFixture)
	}{
		{"threshold-equality", 150, true, nil},
		{"below-threshold", 151, false, nil},
		{"missing-permit", 100, false, func(t *testing.T, f *validatorStakeTestFixture) {
			f.fields[57] = []byte{12, 0, 0, 0}
			f.identity.storage["ValidatorPermit"] = validatorIdentityTestHex(f.fields[57])
		}},
		{"registered-owner", 151, true, func(t *testing.T, f *validatorStakeTestFixture) {
			f.fields[57] = []byte{12, 0, 0, 0}
			f.identity.storage["ValidatorPermit"] = validatorIdentityTestHex(f.fields[57])
			uid := f.identity.query.UID
			f.setOwner(t, f.identity.hotkey, &uid)
		}},
		{"unregistered-owner", 151, false, func(t *testing.T, f *validatorStakeTestFixture) {
			f.setOwner(t, [32]byte{44}, nil)
		}},
		{"absent-threshold-default", 0, true, func(t *testing.T, f *validatorStakeTestFixture) {
			f.identity.storage["StakeThreshold"] = json.RawMessage("null")
		}},
	} {
		t.Run(example.name, func(t *testing.T) {
			fixture, query := newValidatorScheduleTestFixture(t)
			identity := fixture.identity
			identity.version.SpecVersion = 455
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
				t.Fatalf("runtime455 schedule should retain authenticated stake observation: %+v, eligible=%t, error=%v", result, example.eligible, err)
			}
			if identity.chain.Meta != metadata || identity.chain.Runtime != runtime {
				t.Fatal("runtime455 schedule changed the dial-time signing identity")
			}
		})
	}
}

// Layout admission does not authorize another artifact or any future runtime.
// Even caller-allowed unknown versions must stop before the fixed API decoder.
func TestRuntimeArtifactMetadataValidatorStakeRuntime455RejectsForeignIdentity(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name   string
		change func(*validatorIdentityTestFixture)
	}{
		{"prior-artifact-authority", func(f *validatorIdentityTestFixture) { f.allowed[0].Version.SpecVersion = 454 }},
		{"code-hash", func(f *validatorIdentityTestFixture) { f.allowed[0].CodeHash = types.Hash{88}.Hex() }},
		{"metadata-hash", func(f *validatorIdentityTestFixture) { f.allowed[0].MetadataHash = types.Hash{77}.Hex() }},
		{"unreviewed-453", func(f *validatorIdentityTestFixture) { f.version.SpecVersion = 453; f.allowed[0].Version = f.version }},
		{"future-456", func(f *validatorIdentityTestFixture) { f.version.SpecVersion = 456; f.allowed[0].Version = f.version }},
		{"spec-name", func(f *validatorIdentityTestFixture) {
			f.version.SpecName = "foreign-subtensor"
			f.allowed[0].Version = f.version
		}},
		{"transaction-version", func(f *validatorIdentityTestFixture) {
			f.version.TransactionVersion = 2
			f.allowed[0].Version = f.version
		}},
		{"state-version", func(f *validatorIdentityTestFixture) { f.version.StateVersion = 2; f.allowed[0].Version = f.version }},
	} {
		t.Run(example.name, func(t *testing.T) {
			fixture := newValidatorStakeTestFixture(t)
			fixture.identity.version.SpecVersion = 455
			fixture.identity.allowed[0].Version = fixture.identity.version
			example.change(fixture.identity)
			result, err := fixture.read()
			if err == nil || result != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 0 {
				t.Fatalf("foreign runtime455 authority reached stake decoder: %+v, error=%v, runtime_calls=%d", result, err, fixture.runtimeCalls)
			}
		})
	}
}
