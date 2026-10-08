// Fixture observations and declarations follow the same original input paths
// as public owners. No helper marks an unobserved Claim or a reserve as known.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Native-only setup still has independently configured Claim obligations.
// Obtain their actual HTTP publication before retaining a combined checkpoint.
func observeEconomicConservationTestClaims(t *testing.T, ctx context.Context, source *economicConservationFixture, state *economicConservationState) {
	t.Helper()
	client := newMonitorProviderClient()
	defer client.CloseIdleConnections()
	for index, policy := range source.policy.Claims {
		value, code := readMonitorClaim(ctx, client, policy)
		if code != "ok" {
			t.Fatal("original Claim fixture publication unavailable", policy.Role, code)
		}
		state.ClaimStates[index].observe(policy, value, code, source.now)
		if err := validateMonitorClaimState(policy, state.ClaimStates[index]); err != nil {
			t.Fatal("original HTTP Claim observation cannot be retained", err)
		}
	}
}

// Observed free space does not enlarge an admitted reserve. Publish a separate
// fixture declaration carrying both byte and inode forecasts before archiving.
func economicConservationTestArchiveReserves(t *testing.T, ctx context.Context, metadata string, profiles ...economicConservationPolicy) context.Context {
	t.Helper()
	if len(profiles) > 1 {
		t.Fatal("archive fixture must select one original physical profile")
	}
	maximum := uint64(maxRpcReplyBytes)
	if len(profiles) == 1 {
		if err := profiles[0].StorageProfile.validate(); err != nil {
			t.Fatal(err)
		}
		maximum = profiles[0].storageMaximum()
	}
	// Reserve two full original segments and two future physical heads, each
	// at the selected immutable maximum, with the same two-times margin.
	minimumBytes := max(uint64(64*1024*1024), 2*(2*maximum+2*maximum))
	reference, present := durablevolume.ReferenceFromContext(ctx)
	if !present {
		t.Fatal("original fixture declaration absent")
	}
	config, err := durablevolume.Load(reference)
	if err != nil {
		t.Fatal(err)
	}
	for index := range config.Volumes {
		config.Volumes[index].MinAvailableBytes = max(config.Volumes[index].MinAvailableBytes, minimumBytes)
		config.Volumes[index].MinAvailableInodes = max(config.Volumes[index].MinAvailableInodes, 4096)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	reference = durablevolume.Reference{Path: filepath.Join(metadata, "archive-volumes.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return durablevolume.WithReference(ctx, reference)
}

// Economic command documents must not overwrite the earlier public storage
// request. Export/restore later reuses that exact preparation declaration.
func economicConservationTestRestoreMetadata(t *testing.T, source *storagePreparationCommandFixture) string {
	t.Helper()
	path := filepath.Join(source.metadata, "economic-review")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireEconomicConservationTestPreparation(t *testing.T, source *storagePreparationCommandFixture) {
	t.Helper()
	raw, err := os.ReadFile(source.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := decodeMonitorHistoryInput(raw, &request); err != nil {
		t.Fatal("original storage preparation was overwritten by a different command", err)
	}
	if monitorReadDigest(raw) != source.requestHash || request.Schema != durablevolume.PreparationRequestSchema || request.Scope != "daemon" || request.Purpose != "fresh" || request.RestoreSource != nil {
		t.Fatal("original exact preparation input changed before source export")
	}
}
