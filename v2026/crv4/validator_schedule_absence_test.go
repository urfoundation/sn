// An absent mapping is an eligibility observation only at the independently
// pinned canonical finalized block, just like a present registration.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Missing storage must not bypass the identity reader's chain and finality
// checks and revoke a cached validator on an unauthenticated observation.
func TestValidatorScheduleAbsentMappingRequiresCanonicalFinality(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*validatorIdentityTestFixture)
	}{
		{name: "genesis", change: func(f *validatorIdentityTestFixture) { f.blockHashes[0] = types.Hash{9} }},
		{name: "canonical hash", change: func(f *validatorIdentityTestFixture) { f.blockHashes[100] = types.Hash{9} }},
		{name: "header number", change: func(f *validatorIdentityTestFixture) { f.headers[f.query.BlockHash.Hex()] = types.Header{Number: 99} }},
		{name: "finalized coverage", change: func(f *validatorIdentityTestFixture) { f.headers[f.finalized.Hex()] = types.Header{Number: 99} }},
		{name: "finalized canonical hash", change: func(f *validatorIdentityTestFixture) { f.blockHashes[103] = types.Hash{9} }},
		{name: "zero finalized hash", change: func(f *validatorIdentityTestFixture) { f.finalized = types.Hash{} }},
		{name: "registration changed", change: func(f *validatorIdentityTestFixture) {
			f.after = func(method string, args ...any) {
				if method == "state_getStorage" && f.keyNames[args[0].(string)] == "Uids" {
					f.storage["Uids"] = validatorIdentityTestHex([]byte{1, 0})
				}
			}
		}},
		{name: "late canonical change", change: func(f *validatorIdentityTestFixture) {
			f.after = func(method string, args ...any) {
				if method == "state_getStorage" && f.keyNames[args[0].(string)] == "Uids" {
					f.blockHashes[100] = types.Hash{9}
				}
			}
		}},
	} {
		fixture, query := newValidatorScheduleTestFixture(t)
		fixture.identity.storage["Uids"] = json.RawMessage("null")
		test.change(fixture.identity)
		observation, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
		if err == nil || errors.Is(err, ErrValidatorScheduleHotkeyUnregistered) || observation != (ValidatorScheduleObservation{}) {
			t.Errorf("%s: unauthenticated absence became an unregistered observation: %v", test.name, err)
		}
	}
}

// A failed confirmation is an unavailable read, not proof of registration or
// deregistration. Keep the typed cause for callers retaining prior eligibility.
func TestValidatorScheduleAbsentMappingConfirmationPreservesReadFailure(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	identity := fixture.identity
	identity.storage["Uids"] = json.RawMessage("null")
	originalHook := identity.hook
	reads := 0
	identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		if method == "state_getStorage" && identity.keyNames[args[0].(string)] == "Uids" {
			reads++
			if reads == 2 {
				return true, context.DeadlineExceeded
			}
		}
		return originalHook(ctx, result, method, args...)
	}
	observation, err := ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
	if reads != 2 || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrValidatorScheduleHotkeyUnregistered) || strings.Contains(err.Error(), "changed") || observation != (ValidatorScheduleObservation{}) {
		t.Fatalf("unavailable absence confirmation changed eligibility: reads=%d, %v", reads, err)
	}
}
