//go:build linux || darwin

// Source-role readback uses the caller's independently authorized runtime
// observer. It must not silently install a writer into live validator state.
package validator

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// An unknown successor is admitted only after explicit consumed-interface
// policy installation; successful readback preserves the original signing view.
func TestReleaseSourceRoleReadbackRequiresOwnedRuntimeCompatibility(t *testing.T) {
	fixture := newProvisionalValidatorRuntimeFixture(t)
	hotkey := [32]byte{0x55}
	netuid := binary.LittleEndian.AppendUint16(nil, fixture.cfg.Netuid)
	commitment, err := types.CreateStorageKey(fixture.metadata, "Commitments", "CommitmentOf", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	last, err := types.CreateStorageKey(fixture.metadata, "Commitments", "LastCommitment", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	prior := fixture.chain.API.Client
	fixture.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getStorage" && len(args) == 2 && args[1] == fixture.block.Hex() && (args[0] == commitment.Hex() || args[0] == last.Hex()) {
			return setReleaseHistoricalTestResult(result, nil)
		}
		return prior.CallContext(ctx, result, method, args...)
	}}
	if err := VerifyReleaseSourceRolePredecessorV2(t.Context(), &fixture.cfg, fixture.chain, hotkey); err == nil {
		t.Fatal("raw connection inferred successor runtime authority from config text")
	}
	if _, err := os.Stat(filepath.Join(fixture.cfg.StateDir, "runtime-compatibility")); !os.IsNotExist(err) {
		t.Fatalf("read-only verifier installed a live-state runtime writer: %v", err)
	}
	if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	originalRuntime, originalMetadata, configuredVersion := fixture.chain.Runtime, fixture.chain.Meta, fixture.cfg.RuntimeSpec
	if err := VerifyReleaseSourceRolePredecessorV2(t.Context(), &fixture.cfg, fixture.chain, hotkey); err != nil {
		t.Fatalf("approved consumed-interface source readback failed: %v", err)
	}
	if fixture.chain.Runtime != originalRuntime || fixture.chain.Meta != originalMetadata || fixture.cfg.RuntimeSpec != configuredVersion || fixture.submissions != 0 {
		t.Fatal("source-role readback changed signed runtime authority or submitted a transaction")
	}
	files, err := os.ReadDir(filepath.Join(fixture.cfg.StateDir, "runtime-compatibility"))
	if err != nil || len(files) != 1 {
		t.Fatalf("successor runtime lacks its separate durable observation: %v", err)
	}
	strict := fixture.cfg
	strict.ProvisionalRuntimeCompatibility = ""
	if err := VerifyReleaseSourceRolePredecessorV2(t.Context(), &strict, fixture.chain, hotkey); err == nil {
		t.Fatal("strict config inherited another owner's runtime compatibility")
	}
}
