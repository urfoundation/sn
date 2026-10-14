// Testnet provisional compatibility adds read authority for unreviewed
// successors only. Reviewed history keeps every strict proof check, and the
// receipt, registration and producer owners still refuse a provisional view.
package crv4

import (
	"errors"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const runtimeViewOwnerRefusal = "runtime view needs this owner's exact authenticated block artifact"

// Wraps the fixture transport before any proof is issued, then optionally
// installs the testnet approval with the given observation callback.
func validatorReadProvisionalTestChain(t *testing.T, identity *validatorIdentityTestFixture, provisional bool, observe func(AuthenticatedRuntimeArtifact) error) *runtimeTransportTestClient {
	t.Helper()
	client := newRuntimeTransportTestClient(identity.chain.API.Client)
	identity.chain.API.Client = client
	if provisional {
		if err := identity.chain.EnableProvisionalRuntimeCompatibility(identity.query.GenesisHash, observe); err != nil {
			t.Fatal(err)
		}
	}
	return client
}

// Reviewed history never needs a compatibility observation.
func refuseValidatorReadObservation(AuthenticatedRuntimeArtifact) error {
	return errors.New("reviewed history requested a compatibility observation")
}

// A provisional approval leaves reviewed history readable: stake and
// schedule reads keep their exact strict block artifact.
func TestValidatorReadKeepsReviewedHistoryOnProvisionalChain(t *testing.T) {
	t.Parallel()
	stake := newValidatorStakeTestFixture(t)
	validatorReadProvisionalTestChain(t, stake.identity, true, refuseValidatorReadObservation)
	observed, err := stake.read()
	if err != nil || observed.Identity.Runtime != stake.identity.allowed[0] || !observed.MeetsNonSelfStakeAndPermit() || stake.runtimeCalls != 1 {
		t.Fatalf("reviewed stake on a provisional chain: %+v, %v", observed, err)
	}
	schedule, query := newValidatorScheduleTestFixture(t)
	validatorReadProvisionalTestChain(t, schedule.identity, true, refuseValidatorReadObservation)
	result, err := ReadValidatorScheduleAtContext(schedule.identity.ctx, schedule.identity.chain, query, schedule.identity.allowed...)
	if err != nil || result.SubnetEpochIndex != 77 || result.Stake.Identity.Runtime != schedule.identity.allowed[0] || !result.Stake.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("reviewed schedule on a provisional chain: %+v, %v", result, err)
	}
}

// Exact identity, owner and transport generation bind a strict proof on the
// validator read path whether or not a provisional approval is installed.
func TestValidatorReadEnforcesStrictProofOnEitherChain(t *testing.T) {
	t.Parallel()
	for _, provisional := range []bool{false, true} {
		fixture := newValidatorStakeTestFixture(t)
		identity := fixture.identity
		client := validatorReadProvisionalTestChain(t, identity, provisional, refuseValidatorReadObservation)
		artifact, err := AuthenticateRuntimeArtifactAtContext(identity.ctx, identity.chain, identity.query.BlockHash, identity.allowed...)
		if err != nil || artifact.CompatibilityProfile != "" || artifact.authenticationProof == nil {
			t.Fatalf("provisional=%t reviewed block lost its strict artifact: %+v, %v", provisional, artifact, err)
		}
		if err := validateValidatorReadRuntimeAtContext(identity.ctx, identity.chain, artifact, validatorStakeRuntimePurpose); err != nil {
			t.Fatalf("provisional=%t exact strict proof refused: %v", provisional, err)
		}
		for _, change := range []struct {
			name string
			edit func(*AuthenticatedRuntimeArtifact)
		}{
			{"block", func(value *AuthenticatedRuntimeArtifact) { value.BlockHash = types.Hash{0x51} }},
			{"version", func(value *AuthenticatedRuntimeArtifact) { value.Version.SpecVersion = 455 }},
			{"code", func(value *AuthenticatedRuntimeArtifact) { value.CodeHash = types.Hash{0x52}.Hex() }},
			{"metadata hash", func(value *AuthenticatedRuntimeArtifact) { value.MetadataHash = types.Hash{0x53}.Hex() }},
			{"metadata", func(value *AuthenticatedRuntimeArtifact) { value.Metadata = types.NewMetadataV14() }},
			{"genesis", func(value *AuthenticatedRuntimeArtifact) { value.GenesisHash = types.Hash{0x54} }},
		} {
			changed := artifact
			change.edit(&changed)
			if err := validateValidatorReadRuntimeAtContext(identity.ctx, identity.chain, changed, validatorStakeRuntimePurpose); err == nil || err.Error() != runtimeViewOwnerRefusal {
				t.Errorf("provisional=%t changed %s escaped: %v", provisional, change.name, err)
			}
		}
		foreign := newValidatorStakeTestFixture(t)
		validatorReadProvisionalTestChain(t, foreign.identity, provisional, refuseValidatorReadObservation)
		if err := validateValidatorReadRuntimeAtContext(foreign.identity.ctx, foreign.identity.chain, artifact, validatorStakeRuntimePurpose); err == nil || err.Error() != runtimeViewOwnerRefusal {
			t.Errorf("provisional=%t foreign owner escaped: %v", provisional, err)
		}
		client.generation.Add(1)
		var expired *runtimeTransportObservationError
		if err := validateValidatorReadRuntimeAtContext(identity.ctx, identity.chain, artifact, validatorStakeRuntimePurpose); !errors.As(err, &expired) {
			t.Errorf("provisional=%t old transport generation escaped: %v", provisional, err)
		}
	}
}

// An unreviewed successor reads only through this connection's current
// compatibility proof, as before; it never reaches the strict owner check.
func TestValidatorReadProvisionalSuccessorUsesItsCompatibilityProof(t *testing.T) {
	t.Parallel()
	fixture, query := newProvisionalValidatorScheduleTestFixture(t)
	identity := fixture.identity
	client := validatorReadProvisionalTestChain(t, identity, true, func(AuthenticatedRuntimeArtifact) error { return nil })
	artifact, err := AuthenticateRuntimeArtifactAtContext(identity.ctx, identity.chain, query.BlockHash, identity.allowed...)
	if err != nil || artifact.CompatibilityProfile != ProvisionalRuntimeCompatibilityProfile || artifact.authenticationProof != nil {
		t.Fatalf("successor lost its provisional artifact: %+v, %v", artifact, err)
	}
	if err := validateValidatorReadRuntimeAtContext(identity.ctx, identity.chain, artifact, validatorScheduleRuntimePurpose); err != nil {
		t.Fatalf("compatible successor refused: %v", err)
	}
	if err := ValidateRuntimeArtifactOwnerContext(identity.ctx, identity.chain, artifact); err == nil || err.Error() != runtimeViewOwnerRefusal {
		t.Fatalf("provisional successor gained strict owner authority: %v", err)
	}
	foreign, _ := newProvisionalValidatorScheduleTestFixture(t)
	validatorReadProvisionalTestChain(t, foreign.identity, true, func(AuthenticatedRuntimeArtifact) error { return nil })
	if err := validateValidatorReadRuntimeAtContext(foreign.identity.ctx, foreign.identity.chain, artifact, validatorScheduleRuntimePurpose); err == nil || err.Error() != "validator stake runtime has no authenticated consumed-interface profile" {
		t.Fatalf("foreign compatibility proof escaped: %v", err)
	}
	client.generation.Add(1)
	var expired *runtimeTransportObservationError
	if err := validateValidatorReadRuntimeAtContext(identity.ctx, identity.chain, artifact, validatorScheduleRuntimePurpose); !errors.As(err, &expired) {
		t.Fatalf("old transport generation kept compatibility: %v", err)
	}
}

// The receipt and registration owners share this guard: a provisional chain's
// strict view never acquires their authority, while reads still admit it.
func TestRuntimeArtifactOwnerContextRefusesProvisionalChain(t *testing.T) {
	t.Parallel()
	for _, provisional := range []bool{false, true} {
		fixture := newValidatorStakeTestFixture(t)
		identity := fixture.identity
		validatorReadProvisionalTestChain(t, identity, provisional, refuseValidatorReadObservation)
		artifact, err := AuthenticateRuntimeArtifactAtContext(identity.ctx, identity.chain, identity.query.BlockHash, identity.allowed...)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateRuntimeArtifactReadOwnerContext(identity.ctx, identity.chain, artifact); err != nil {
			t.Fatalf("provisional=%t read owner refused an exact strict proof: %v", provisional, err)
		}
		err = ValidateRuntimeArtifactOwnerContext(identity.ctx, identity.chain, artifact)
		if provisional && (err == nil || err.Error() != runtimeViewOwnerRefusal) || !provisional && err != nil {
			t.Fatalf("provisional=%t receipt owner admission changed: %v", provisional, err)
		}
	}
}

// Production signing stays outside testnet compatibility: the producer refuses
// a provisional chain's otherwise exact strict view before any read.
func TestValidatorProducerRuntimeRefusesProvisionalChain(t *testing.T) {
	t.Parallel()
	fixture := validatorProducerRuntimeFixture(t)
	artifact := fixture.bind(t)
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact); err != nil {
		t.Fatal(err)
	}
	if err := fixture.chain.EnableProvisionalRuntimeCompatibility(fixture.chain.GenesisHash, refuseValidatorReadObservation); err != nil {
		t.Fatal(err)
	}
	before := fixture.calls
	err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact)
	if err == nil || err.Error() != "validator producer needs an exact authenticated non-provisional block artifact" || fixture.calls != before {
		t.Fatalf("producer admitted a provisional chain: calls=%d %v", fixture.calls-before, err)
	}
}
