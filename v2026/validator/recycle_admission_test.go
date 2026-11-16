// Real steering entry points must not submit the parent provider-only vector
// as if it were the selected mainnet owner-recycle economic successor.
package validator

import (
	"context"
	"errors"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The first attempted RPC is deterministic and cannot sign or broadcast.
func recycleAdmissionTestSteerer(t *testing.T, mainnet bool) (*ReleaseSteerer, *int) {
	t.Helper()
	cfg := runtime467ValidatorTestConfig()
	cfg.Policy = recycleTestInput(t).ParentPolicy
	cfg.ChainID = 964
	if !mainnet {
		cfg.Policy.NetworkProfile = "testnet"
		cfg.ChainID = 945
	}
	calls := new(int)
	client := &validatorRuntimeIdentityTestClient{callContext: func(context.Context, any, string, ...any) error {
		*calls++
		return errors.New("synthetic read boundary")
	}}
	return &ReleaseSteerer{cfg: &cfg, native: &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}}, calls
}

// Public and direct compact entry points must refuse before any chain/state
// work can give the unchanged parent vector production authority.
func TestOwnerRecycleMainnetSteeringRefusesUnmigratedSuccessor(t *testing.T) {
	for _, compact := range []bool{false, true} {
		steerer, calls := recycleAdmissionTestSteerer(t, true)
		var err error
		if compact {
			err = steerer.submitOnceV2(t.Context())
		} else {
			err = steerer.SubmitOnce(t.Context())
		}
		if err == nil || !strings.Contains(err.Error(), "owner-recycle successor activation is blocked") || *calls != 0 {
			t.Errorf("compact=%v: calls=%d error=%v", compact, *calls, err)
		}
	}
}

// The last shared native boundary remains closed even if a caller supplies
// a prepared parent-policy transaction without entering the steerer loop.
func TestOwnerRecycleMainnetPreparedSubmissionRemainsBlocked(t *testing.T) {
	steerer, calls := recycleAdmissionTestSteerer(t, true)
	prepared := &crv4.PreparedSubmission{PreparedAtBlockHash: types.Hash{7}.Hex()}
	_, attempted, err := submitPreparedNativeRuntimeContext(t.Context(), steerer.native, steerer.cfg, prepared)
	if attempted || err == nil || !strings.Contains(err.Error(), "owner-recycle successor activation is blocked") || *calls != 0 {
		t.Fatalf("attempted=%v calls=%d error=%v", attempted, *calls, err)
	}
}

// Existing testnet policy remains on its original release path.
func TestOwnerRecycleAdmissionPreservesTestnetSteering(t *testing.T) {
	steerer, calls := recycleAdmissionTestSteerer(t, false)
	err := steerer.SubmitOnce(t.Context())
	if err == nil || !strings.Contains(err.Error(), "synthetic read boundary") || *calls != 1 {
		t.Fatalf("testnet calls=%d error=%v", *calls, err)
	}
}
