// Availability marker provenance is preserved through the same finite graph
// rules as runtime shutdown; a local custody cause cannot become API-local.
package validator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/sdk/v2026"
)

// The complete original SDK and clientauth markers remain opaque. Typed nils,
// foreign wrappers, missing branches and file operations cannot impersonate one.
func TestReleaseRegistrationClassifiersRefuseIncompleteCauseGraphs(t *testing.T) {
	unavailable := &sdk.NetworkClientRegistrationUnavailableError{}
	cycle := &releaseCauseJoin{}
	cycle.causes = []error{unavailable, cycle}
	wide := &releaseCauseJoin{causes: make([]error, 129)}
	for index := range wide.causes {
		wide.causes[index] = unavailable
	}
	var unavailableNil *sdk.NetworkClientRegistrationUnavailableError
	var refusalNil *clientauth.RegistrationRefusedError
	var refreshNil *clientauth.RegistrationRefreshUnavailableError
	for _, cause := range []error{
		cycle, wide, unavailableNil, refusalNil, refreshNil,
		&releaseCauseJoin{causes: []error{unavailable, nil}},
		&releaseCauseMatcher{cause: unavailable},
		&os.PathError{Op: "read", Path: "synthetic-client-token", Err: unavailable},
		&os.LinkError{Op: "rename", Old: "synthetic-token-before", New: "synthetic-token-after", Err: unavailable},
		errors.Join(unavailable, errors.New("synthetic durable custody failure")),
	} {
		if measurementAuthenticationRetryable(cause, 0) {
			t.Fatal("measurement replay accepted an incomplete or hard cause graph")
		}
		if wait, blocked := productionRegistrationWait(cause); wait || blocked {
			t.Fatal("production registration waited on an incomplete or hard cause graph")
		}
		if productionRegistrationLocalFailure(cause) {
			t.Fatal("shared malformed or custody failure became only API-local")
		}
	}
}

// Pure original unavailability still retries, exact recovery codes retain the
// blocked result, and mixed API-local replies retain their local disposition.
func TestReleaseRegistrationClassifiersPreserveOriginalMarkerPolicy(t *testing.T) {
	unavailable := &sdk.NetworkClientRegistrationUnavailableError{}
	original := errors.Join(fmt.Errorf("first original: %w", unavailable), unavailable)
	if !measurementAuthenticationRetryable(original, 0) {
		t.Fatal("measurement lost the original unavailable reply's retry")
	}
	if wait, blocked := productionRegistrationWait(original); !wait || blocked {
		t.Fatal("production lost its ordinary original unavailable wait")
	}
	blockedOriginal := errors.Join(original, &clientauth.RegistrationRefusedError{Code: "network_authentication_missing"})
	if wait, blocked := productionRegistrationWait(blockedOriginal); !wait || !blocked {
		t.Fatal("an exact recovery-required original lost its blocked result")
	}
	local := errors.Join(&sdk.ClientControlResponseError{}, context.DeadlineExceeded)
	if !productionRegistrationLocalFailure(local) {
		t.Fatal("a complete API-local refusal became a shared service failure")
	}
	if wait, _ := productionRegistrationWait(local); wait || measurementAuthenticationRetryable(local, 0) {
		t.Fatal("a complete invalid API reply gained automatic retry")
	}
}
