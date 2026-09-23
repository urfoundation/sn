// Exact-block replay comparisons admit a later finalized watermark without
// letting a conflicting same-height hash or changed identity hide behind it.
// The opt-in live probe and deterministic controls call the same helper.
package crv4

import (
	"errors"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Both observations already passed the reader's complete block/runtime checks.
// Only a genuinely later finality watermark may differ from the first result.
func validateValidatorIdentityReplay(first, replayed ValidatorIdentityObservation) error {
	if replayed.FinalizedNumber < first.FinalizedNumber || replayed.FinalizedHash == (types.Hash{}) {
		return errors.New("public finality watermark regressed or is missing")
	}
	if replayed.FinalizedNumber == first.FinalizedNumber && replayed.FinalizedHash != first.FinalizedHash {
		return errors.New("public finality watermark has conflicting same-height hashes")
	}
	replayIdentity := replayed
	replayIdentity.FinalizedHash = first.FinalizedHash
	replayIdentity.FinalizedNumber = first.FinalizedNumber
	if replayIdentity != first {
		return errors.New("exact-block validator identity changed on replay")
	}
	return nil
}

// Real metadata authentication and historical storage decoding supply the
// complete initial observation; the comparison never substitutes their verdict.
func validatorIdentityReplayTestObservation(t *testing.T) ValidatorIdentityObservation {
	t.Helper()
	fixture := newValidatorIdentityTestFixture(t)
	observed, err := ReadValidatorIdentityAtContext(fixture.ctx, fixture.chain, fixture.query, fixture.allowed...)
	if err != nil {
		t.Fatal(err)
	}
	return observed
}

// The old live comparison erased the conflicting hash before equality. This
// deterministic refusal exercises the exact helper now used by that probe.
func TestRuntimeArtifactMetadataValidatorIdentityReplayRejectsSameHeightDifferentHash(t *testing.T) {
	t.Parallel()
	first := validatorIdentityReplayTestObservation(t)
	replayed := first
	replayed.FinalizedHash[0] ^= 1
	if err := validateValidatorIdentityReplay(first, replayed); err == nil {
		t.Fatal("same-height conflicting finalized hash was accepted")
	}
}

// Repeating an unchanged finalized head remains a successful observation.
func TestRuntimeArtifactMetadataValidatorIdentityReplayAcceptsUnchanged(t *testing.T) {
	t.Parallel()
	first := validatorIdentityReplayTestObservation(t)
	if err := validateValidatorIdentityReplay(first, first); err != nil {
		t.Fatalf("unchanged exact-block replay was refused: %v", err)
	}
}

// A later finalized head may change while the selected historical identity
// remains fixed; value-only normalization must not mutate either input.
func TestRuntimeArtifactMetadataValidatorIdentityReplayAcceptsNewerFinality(t *testing.T) {
	t.Parallel()
	first := validatorIdentityReplayTestObservation(t)
	replayed := first
	replayed.FinalizedNumber++
	replayed.FinalizedHash[0] ^= 1
	beforeFirst, beforeReplay := first, replayed
	if err := validateValidatorIdentityReplay(first, replayed); err != nil {
		t.Fatalf("newer finalized watermark was refused: %v", err)
	}
	if first != beforeFirst || replayed != beforeReplay {
		t.Fatal("replay comparison mutated its caller's observations")
	}
}

// A decreasing finalized height cannot be excused by an otherwise identical
// historical identity or by repeating the old finalized hash.
func TestRuntimeArtifactMetadataValidatorIdentityReplayRejectsRegressingFinality(t *testing.T) {
	t.Parallel()
	first := validatorIdentityReplayTestObservation(t)
	for _, sameHash := range []bool{false, true} {
		replayed := first
		replayed.FinalizedNumber--
		if !sameHash {
			replayed.FinalizedHash[0] ^= 1
		}
		if err := validateValidatorIdentityReplay(first, replayed); err == nil {
			t.Errorf("regressing watermark was accepted, same hash=%t", sameHash)
		}
	}
}

// A missing finalized hash is not an unchanged or newer valid watermark.
func TestRuntimeArtifactMetadataValidatorIdentityReplayRejectsZeroFinalizedHash(t *testing.T) {
	t.Parallel()
	first := validatorIdentityReplayTestObservation(t)
	for _, newer := range []bool{false, true} {
		replayed := first
		replayed.FinalizedHash = types.Hash{}
		if newer {
			replayed.FinalizedNumber++
		}
		if err := validateValidatorIdentityReplay(first, replayed); err == nil {
			t.Errorf("missing finalized hash was accepted, newer=%t", newer)
		}
	}
}

// Every non-watermark field stays exact, including ownership, full-width
// stake and each independent runtime pin, even with a genuinely newer head.
func TestRuntimeArtifactMetadataValidatorIdentityReplayRejectsIdentityChange(t *testing.T) {
	t.Parallel()
	first := validatorIdentityReplayTestObservation(t)
	for _, example := range []struct {
		name   string
		change func(*ValidatorIdentityObservation)
	}{
		{name: "genesis", change: func(value *ValidatorIdentityObservation) { value.GenesisHash[0] ^= 1 }},
		{name: "block hash", change: func(value *ValidatorIdentityObservation) { value.BlockHash[0] ^= 1 }},
		{name: "block number", change: func(value *ValidatorIdentityObservation) { value.BlockNumber++ }},
		{name: "netuid", change: func(value *ValidatorIdentityObservation) { value.Netuid++ }},
		{name: "UID", change: func(value *ValidatorIdentityObservation) { value.UID++ }},
		{name: "census", change: func(value *ValidatorIdentityObservation) { value.SubnetUIDs++ }},
		{name: "hotkey", change: func(value *ValidatorIdentityObservation) { value.Hotkey[0] ^= 1 }},
		{name: "coldkey", change: func(value *ValidatorIdentityObservation) { value.Coldkey[0] ^= 1 }},
		{name: "stake", change: func(value *ValidatorIdentityObservation) { value.StakeAlphaRao++ }},
		{name: "permit", change: func(value *ValidatorIdentityObservation) { value.ValidatorPermit = !value.ValidatorPermit }},
		{name: "spec name", change: func(value *ValidatorIdentityObservation) { value.Runtime.Version.SpecName += "-changed" }},
		{name: "spec version", change: func(value *ValidatorIdentityObservation) { value.Runtime.Version.SpecVersion++ }},
		{name: "transaction version", change: func(value *ValidatorIdentityObservation) { value.Runtime.Version.TransactionVersion++ }},
		{name: "state version", change: func(value *ValidatorIdentityObservation) { value.Runtime.Version.StateVersion++ }},
		{name: "code hash", change: func(value *ValidatorIdentityObservation) { value.Runtime.CodeHash = types.Hash{99}.Hex() }},
		{name: "metadata hash", change: func(value *ValidatorIdentityObservation) { value.Runtime.MetadataHash = types.Hash{99}.Hex() }},
	} {
		for _, newer := range []bool{false, true} {
			replayed := first
			if newer {
				replayed.FinalizedNumber++
				replayed.FinalizedHash[0] ^= 1
			}
			example.change(&replayed)
			if err := validateValidatorIdentityReplay(first, replayed); err == nil {
				t.Errorf("changed %s was accepted, newer=%t", example.name, newer)
			}
		}
	}
}
