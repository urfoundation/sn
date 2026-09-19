package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

func paidTrafficTestManifest(t *testing.T) (PaidTrafficManifest, string, string, string) {
	t.Helper()
	root := t.TempDir()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 17
	seedPath := filepath.Join(root, "consumer.seed")
	if err := os.WriteFile(seedPath, seed, 0600); err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(seed)
	manifest := PaidTrafficManifest{
		Schema: paidTrafficSchema, ParentPlanHash: strings.Repeat("a", 64), DeploymentId: "synthetic-testnet", Operator: 1,
		DatabaseContainer: "synthetic-operator-db", ApiUrl: "http://api.example", ConnectUrl: "wss://connect.example",
		ClientId: "01000000-0000-0000-0000-000000000001", NetworkId: "02000000-0000-0000-0000-000000000001",
		ClientKeySeedFile: seedPath, ClientPublicKeyHex: hex.EncodeToString(key[ed25519.SeedSize:]),
		Providers: []PaidTrafficProvider{
			{ClientId: "01000000-0000-0000-0000-000000000002", NetworkId: "02000000-0000-0000-0000-000000000002"},
			{ClientId: "01000000-0000-0000-0000-000000000003", NetworkId: "02000000-0000-0000-0000-000000000002"},
		},
		TargetPayloadBytes: 16, HardPayloadCapBytes: 32, BatchPayloadBytes: 16, MaximumAttempts: 4,
		InflightMessages: 2, BatchTimeoutSeconds: 60, RetryMinimumSeconds: 1, RetryMaximumSeconds: 4,
		ExpiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	manifestPath := filepath.Join(root, "review.json")
	if err := writePaidTrafficRecord(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	hash, err := paidTrafficManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, manifestPath, filepath.Join(root, "sidecar"), hash
}

func paidTrafficTestDependencies() paidTrafficDependencies {
	return paidTrafficDependencies{
		now:  func() time.Time { return time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC) },
		wait: func(context.Context, time.Duration) error { return nil },
		send: func(_ context.Context, _ PaidTrafficManifest, allocations []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
			return PaidTrafficBatchResult{Acknowledged: allocations, CleanupJoined: true}, nil
		},
	}
}

func TestPaidTrafficRejectsSameNetworkVerificationWorkload(t *testing.T) {
	manifest, _, _, _ := paidTrafficTestManifest(t)
	manifest.Providers[0].NetworkId = manifest.NetworkId
	if err := manifest.Validate(); err == nil {
		t.Fatal("same-network verification traffic was admitted as an accounted workload")
	}
}

func TestPaidTrafficRetriesTimeoutWithoutRepeatingCompletedProvider(t *testing.T) {
	manifest, path, stateDir, hash := paidTrafficTestManifest(t)
	dependencies := paidTrafficTestDependencies()
	var calls int
	var delays []time.Duration
	dependencies.wait = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}
	dependencies.send = func(_ context.Context, _ PaidTrafficManifest, allocations []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		calls++
		// Audit runs while the sender owns the lock and sees the reservation
		// that was synced before any network side effect.
		observed, err := ReadPaidTrafficSidecarStatus(stateDir)
		if err != nil || observed.Attempts != uint64(calls) || observed.UncertainPayloadBytes == 0 || observed.FinalAcceptance || observed.SettlementVerified {
			t.Fatalf("concurrent audit lost the durable reservation: %+v, %v", observed, err)
		}
		if calls == 1 {
			return PaidTrafficBatchResult{Acknowledged: []PaidTrafficAllocation{
				{ClientId: manifest.Providers[0].ClientId, PayloadBytes: 4},
				{ClientId: manifest.Providers[1].ClientId, PayloadBytes: 8},
			}, CleanupJoined: true}, context.DeadlineExceeded
		}
		want := []PaidTrafficAllocation{{ClientId: manifest.Providers[0].ClientId, PayloadBytes: 4}}
		if !reflect.DeepEqual(allocations, want) {
			t.Fatalf("retry = %+v, want only unfinished provider %+v", allocations, want)
		}
		return PaidTrafficBatchResult{Acknowledged: allocations, CleanupJoined: true}, nil
	}
	status, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies)
	if err != nil || status.Stage != "payload_complete" || status.AcknowledgedPayloadBytes != 16 || status.ReservedPayloadBytes != 20 || status.FailedAttempts != 1 || calls != 2 || !reflect.DeepEqual(delays, []time.Duration{time.Second}) {
		t.Fatalf("timeout recovery = %+v, %v; calls=%d delays=%v", status, err, calls, delays)
	}
	dependencies.send = func(context.Context, PaidTrafficManifest, []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		t.Fatal("completed sidecar repeated its traffic on restart")
		return PaidTrafficBatchResult{}, nil
	}
	if resumed, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies); err != nil || resumed.AcknowledgedPayloadBytes != 16 {
		t.Fatalf("completed restart = %+v, %v", resumed, err)
	}
}

func TestPaidTrafficCrashReservationRemainsInsideLifetimeCap(t *testing.T) {
	_, path, stateDir, hash := paidTrafficTestManifest(t)
	dependencies := paidTrafficTestDependencies()
	crash := errors.New("deterministic crash after durable reserve")
	dependencies.afterReserve = func(paidTrafficReservation) error { return crash }
	dependencies.send = func(context.Context, PaidTrafficManifest, []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		t.Fatal("crash barrier permitted network side effects")
		return PaidTrafficBatchResult{}, nil
	}
	if _, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies); !errors.Is(err, crash) {
		t.Fatalf("crash boundary = %v", err)
	}
	status, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, paidTrafficTestDependencies())
	if err != nil || status.Stage != "payload_complete" || status.Attempts != 2 || status.ReservedPayloadBytes != 32 || status.UncertainPayloadBytes != 16 || status.AcknowledgedPayloadBytes != 16 {
		t.Fatalf("crash continuation lost or duplicated its allowance: %+v, %v", status, err)
	}
}

func TestPaidTrafficExhaustedByteBudgetDoesNotResetOnRestart(t *testing.T) {
	_, path, stateDir, hash := paidTrafficTestManifest(t)
	dependencies := paidTrafficTestDependencies()
	var calls int
	dependencies.send = func(context.Context, PaidTrafficManifest, []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		calls++
		return PaidTrafficBatchResult{CleanupJoined: true}, context.DeadlineExceeded
	}
	status, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies)
	if err != nil || status.Stage != "payload_budget_exhausted" || status.ReservedPayloadBytes != 32 || calls != 2 {
		t.Fatalf("bounded retry = %+v, %v; calls=%d", status, err, calls)
	}
	if _, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies); err != nil || calls != 2 {
		t.Fatalf("restart reset the exhausted allowance: %v; calls=%d", err, calls)
	}
}

func TestPaidTrafficExclusiveOwnerLeavesCoreSupervisorUntouched(t *testing.T) {
	_, path, stateDir, hash := paidTrafficTestManifest(t)
	coreManifest := filepath.Join(filepath.Dir(stateDir), "supervisor.json")
	coreData := []byte("synthetic core supervisor remains immutable\n")
	if err := os.WriteFile(coreManifest, coreData, 0600); err != nil {
		t.Fatal(err)
	}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	dependencies := paidTrafficTestDependencies()
	var sends atomic.Int64
	dependencies.send = func(_ context.Context, _ PaidTrafficManifest, allocations []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		sends.Add(1)
		close(entered)
		<-release
		return PaidTrafficBatchResult{Acknowledged: allocations, CleanupJoined: true}, nil
	}
	go func() {
		_, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies)
		finished <- err
	}()
	<-entered
	_, secondErr := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, paidTrafficTestDependencies())
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if secondErr == nil || !strings.Contains(secondErr.Error(), "another paid traffic owner") || sends.Load() != 1 {
		t.Fatalf("second writer was admitted: %v, sends=%d", secondErr, sends.Load())
	}
	if data, err := os.ReadFile(coreManifest); err != nil || string(data) != string(coreData) {
		t.Fatalf("sidecar changed core topology: %q, %v", data, err)
	}
}

func TestPaidTrafficRejectsUnapprovedManifestBeforeMutation(t *testing.T) {
	_, path, stateDir, _ := paidTrafficTestManifest(t)
	if _, err := runPaidTrafficSidecar(t.Context(), path, stateDir, strings.Repeat("b", 64), paidTrafficTestDependencies()); err == nil {
		t.Fatal("unapproved manifest was admitted")
	}
	if _, err := os.Stat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unapproved run mutated its directory: %v", err)
	}
}

func TestPaidTrafficChangedSeedAndProviderMembershipAreRejected(t *testing.T) {
	manifest, _, _, _ := paidTrafficTestManifest(t)
	seed, err := readPaidTrafficSeed(manifest)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("synthetic retained seed = %d, %v", len(seed), err)
	}
	seed[0]++
	if err := os.WriteFile(manifest.ClientKeySeedFile, seed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPaidTrafficSeed(manifest); err == nil {
		t.Fatal("a different client key was silently adopted")
	}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.MapClaims{"client_id": manifest.ClientId, "network_id": manifest.NetworkId}).SignedString([]byte("synthetic-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	identity := paidTrafficIdentity{ClientId: manifest.ClientId, NetworkId: manifest.NetworkId, Jwt: token, Active: true, RemainingBytes: 100, Providers: map[string]string{}}
	for _, provider := range manifest.Providers {
		identity.Providers[provider.ClientId] = provider.NetworkId
	}
	if err := validatePaidTrafficIdentity(manifest, identity, 16); err != nil {
		t.Fatal(err)
	}
	identity.Providers[manifest.Providers[0].ClientId] = manifest.NetworkId
	if err := validatePaidTrafficIdentity(manifest, identity, 16); err == nil {
		t.Fatal("provider membership changed into a free same-network path")
	}
}

func TestPaidTrafficRejectsAcknowledgementsBeyondReservedBytes(t *testing.T) {
	_, path, stateDir, hash := paidTrafficTestManifest(t)
	dependencies := paidTrafficTestDependencies()
	dependencies.send = func(_ context.Context, _ PaidTrafficManifest, allocations []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		allocations[0].PayloadBytes++
		return PaidTrafficBatchResult{Acknowledged: allocations, CleanupJoined: true}, nil
	}
	if _, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies); err == nil || !strings.Contains(err.Error(), "acknowledgement exceeds") {
		t.Fatalf("invented progress was accepted: %v", err)
	}
}

func TestPaidTrafficUnjoinedCleanupCannotStartAnotherLiveOwner(t *testing.T) {
	_, path, stateDir, hash := paidTrafficTestManifest(t)
	dependencies := paidTrafficTestDependencies()
	var calls int
	dependencies.send = func(_ context.Context, _ PaidTrafficManifest, allocations []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		calls++
		return PaidTrafficBatchResult{Acknowledged: allocations, CleanupJoined: false}, context.DeadlineExceeded
	}
	status, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies)
	if err == nil || status == nil || status.Stage != "cleanup_pending" || !status.CleanupPending || status.AcknowledgedPayloadBytes != 16 {
		t.Fatalf("unjoined cleanup was declared complete: %+v, %v", status, err)
	}
	status, err = runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies)
	if err == nil || !strings.Contains(err.Error(), "cleanup owner is still live") || calls != 1 || status.Stage != "cleanup_pending" {
		t.Fatalf("unjoined identity was reused: %+v, %v; calls=%d", status, err, calls)
	}
}

func TestPaidTrafficPartialResultWithoutErrorStillRetries(t *testing.T) {
	manifest, path, stateDir, hash := paidTrafficTestManifest(t)
	dependencies := paidTrafficTestDependencies()
	var calls, waits int
	dependencies.wait = func(context.Context, time.Duration) error { waits++; return nil }
	dependencies.send = func(_ context.Context, _ PaidTrafficManifest, allocations []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		calls++
		if calls == 1 {
			return PaidTrafficBatchResult{Acknowledged: []PaidTrafficAllocation{{ClientId: manifest.Providers[0].ClientId, PayloadBytes: 8}}, CleanupJoined: true}, nil
		}
		return PaidTrafficBatchResult{Acknowledged: allocations, CleanupJoined: true}, nil
	}
	status, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies)
	if err != nil || status.Stage != "payload_complete" || status.FailedAttempts != 1 || waits != 1 || calls != 2 {
		t.Fatalf("partial result bypassed bounded retry: %+v, %v; calls=%d waits=%d", status, err, calls, waits)
	}
}

func TestPaidTrafficExpiredManifestDoesNotSend(t *testing.T) {
	manifest, path, stateDir, hash := paidTrafficTestManifest(t)
	dependencies := paidTrafficTestDependencies()
	dependencies.now = func() time.Time { return manifest.ExpiresAt }
	dependencies.send = func(context.Context, PaidTrafficManifest, []PaidTrafficAllocation) (PaidTrafficBatchResult, error) {
		t.Fatal("expired workload sent traffic")
		return PaidTrafficBatchResult{}, nil
	}
	status, err := runPaidTrafficSidecar(t.Context(), path, stateDir, hash, dependencies)
	if err != nil || status.Stage != "expired" || status.Attempts != 0 {
		t.Fatalf("expiry result = %+v, %v", status, err)
	}
}
