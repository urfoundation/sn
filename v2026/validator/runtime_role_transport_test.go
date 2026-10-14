// Complete role observations retain one transport census across network,
// artifact, purpose and closing reads. Forced generations make the cuts exact.
package validator

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A retained proof can expire before its role callback begins. The existing
// outer production owner still consumes that typed cause without renewing its
// total budget or changing the original policy, block or signing view.
func TestProductionRuntimeRetainedExpiryReentersOriginalReadOwner(t *testing.T) {
	fixture := newProductionContinuityPolicyTestFixture(t)
	native := fixture.owner.rpc.native
	client := &productionTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	artifact, err := crv4.ReadRuntimeArtifactAtContext(t.Context(), native, fixture.hashes[150], fixture.candidate)
	if err != nil {
		t.Fatal(err)
	}
	client.generation.Add(1)
	owner := &ReleaseSteerer{cfg: fixture.owner.cfg}
	var budgets []time.Duration
	owner.productionReadHooks.withTimeout = func(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
		budgets = append(budgets, budget)
		return context.WithTimeout(ctx, budget)
	}
	attempts, waits := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		return ctx.Err()
	}
	var observation *ProductionRuntimeContinuityInspection
	err = owner.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
		attempts++
		if attempts == 1 {
			return crv4.ValidateRuntimeArtifactOwnerContext(ctx, native, artifact)
		}
		var err error
		observation, err = InspectProductionRuntimeContinuityContext(ctx, native, fixture.owner.cfg, fixture.hashes[150], fixture.policyRaw, fixture.certificateRaw)
		return err
	})
	if err != nil || observation == nil || observation.NativeHash != fixture.hashes[150] || observation.SigningAuthority || attempts != 2 || waits != 1 ||
		!slices.Equal(budgets, []time.Duration{300 * time.Second, 60 * time.Second, 60 * time.Second}) {
		t.Fatalf("retained runtime expiry escaped the original bounded production owner: attempts=%d waits=%d budgets=%v observation=%+v err=%v", attempts, waits, budgets, observation, err)
	}
}

// The selected block remains the first finalized hash even when the retry sees
// a newer head. Its independent network identity must be observed twice.
func TestMainnetRuntimeEarlyReconnectRepeatsNetworkAndOriginalBlock(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	original := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
	client := &productionTransportTestClient{Client: original}
	client.generation.Store(1)
	fixture.native.API.Client = client
	faulted := false
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := fixture.callContext(ctx, target, method, args...)
		if err == nil && method == "state_getStorageHash" && !faulted {
			faulted = true
			client.generation.Add(1)
			fixture.head = 151
		}
		return err
	}
	observed, err := ObserveMainnetRuntimeAtContext(t.Context(), fixture.native, cfg, types.Hash{})
	if err != nil || observed == nil || !faulted || fixture.callKVs["system_chain"] != 2 ||
		observed.BlockHash != mainnetRuntimeTestBlock(150) || observed.FinalizedHash != mainnetRuntimeTestBlock(151) {
		t.Fatalf("artifact retry retained an earlier network census or moved the original block: calls=%v observed=%+v err=%v", fixture.callKVs, observed, err)
	}
}

// The last canonical response is still part of the same read; a successful
// artifact from before a replacement cannot publish a mixed observation.
func TestMainnetRuntimeClosingReconnectRepeatsCompleteObservation(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	original := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
	client := &productionTransportTestClient{Client: original}
	client.generation.Store(1)
	fixture.native.API.Client = client
	canonicalReads := 0
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := fixture.callContext(ctx, target, method, args...)
		if err == nil && method == "chain_getBlockHash" && args[0] == uint64(150) {
			canonicalReads++
			if canonicalReads == 3 {
				client.generation.Add(1)
				fixture.head = 151
			}
		}
		return err
	}
	artifact, observed, err := authenticateReleaseMainnetRuntimeAtContext(t.Context(), fixture.native, cfg, types.Hash{})
	if err != nil || observed == nil || fixture.callKVs["system_chain"] != 2 ||
		observed.BlockHash != mainnetRuntimeTestBlock(150) || observed.FinalizedHash != mainnetRuntimeTestBlock(151) ||
		crv4.ValidateRuntimeArtifactOwnerContext(t.Context(), fixture.native, artifact) != nil {
		t.Fatalf("closing reconnect published an expired role observation: calls=%v observed=%+v err=%v", fixture.callKVs, observed, err)
	}
}

// A replacement that serves the original code and metadata still needs the
// separately approved network identity. Its contradiction stays permanent.
func TestMainnetRuntimeReconnectCannotBorrowEarlierNetworkIdentity(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	original := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
	client := &productionTransportTestClient{Client: original}
	client.generation.Store(1)
	fixture.native.API.Client = client
	faulted := false
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := fixture.callContext(ctx, target, method, args...)
		if err == nil && method == "state_getStorageHash" && !faulted {
			faulted = true
			client.generation.Add(1)
			fixture.genesis = types.Hash{0xf1}
		}
		return err
	}
	metadata, runtime := fixture.native.Meta, fixture.native.Runtime
	observed, err := ObserveMainnetRuntimeAtContext(t.Context(), fixture.native, cfg, types.Hash{})
	if observed != nil || err == nil || RetryableEvidenceTransportError(err) || !faulted || fixture.callKVs["system_chain"] != 2 ||
		fixture.native.Meta != metadata || fixture.native.Runtime != runtime {
		t.Fatalf("replacement borrowed original network authority: calls=%v observed=%+v err=%v", fixture.callKVs, observed, err)
	}
}

// A blocked second census owns cancellation, while another configured role
// completes its own exact observation without waiting for this one.
func TestMainnetRuntimeReconnectCancellationLeavesPeerIndependent(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	original := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
	client := &productionTransportTestClient{Client: original}
	client.generation.Store(1)
	fixture.native.API.Client = client
	entered := make(chan struct{})
	faulted, networkReads := false, 0
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "system_chain" {
			networkReads++
			if networkReads == 2 {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}
		}
		err := fixture.callContext(ctx, target, method, args...)
		if err == nil && method == "state_getStorageHash" && !faulted {
			faulted = true
			client.generation.Add(1)
		}
		return err
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		observation, err := ObserveMainnetRuntimeAtContext(ctx, fixture.native, cfg, types.Hash{})
		if observation != nil {
			err = errors.New("interrupted role published an observation")
		}
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("role did not repeat and own its complete network census: %v", err)
	}
	peer := newMainnetRuntimeTestFixture(t)
	peerCfg, err := LoadMainnetRuntimeObservationConfig(peer.path)
	if err != nil {
		t.Fatal(err)
	}
	observed, peerErr := ObserveMainnetRuntimeAtContext(t.Context(), peer.native, peerCfg, types.Hash{})
	cancel()
	err = <-done
	if peerErr != nil || observed == nil || !errors.Is(err, context.Canceled) || networkReads != 2 {
		t.Fatalf("role cancellation stopped its peer or lost the owned join: peer=%+v peerErr=%v err=%v", observed, peerErr, err)
	}
}

// The purpose bind remains private until the closing canonical response and
// transport agree. Failed replacement admission preserves the previous view.
func TestProductionRuntimeClosingReconnectPreservesPreviousBinding(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	native := fixture.rpc.native
	original := native.API.Client.(*validatorRuntimeIdentityTestClient)
	call := original.callContext
	client := &productionTransportTestClient{Client: original}
	client.generation.Store(1)
	native.API.Client = client
	canonicalReads := 0
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := call(ctx, target, method, args...)
		if err == nil && method == "chain_getBlockHash" && args[0] == uint64(150) {
			canonicalReads++
			if canonicalReads == 4 {
				client.generation.Add(1)
				fixture.rpc.nativeChain = "replacement.example"
			}
		}
		return err
	}
	metadata, runtime := native.Meta, native.Runtime
	err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, fixture.cfg, mainnetRuntimeTestBlock(150))
	if err == nil || RetryableEvidenceTransportError(err) || canonicalReads != 4 || fixture.rpc.callKVs["system_chain"] != 2 ||
		native.Meta != metadata || native.Runtime != runtime {
		t.Fatalf("closing replacement published a production binding: canonical=%d calls=%v err=%v", canonicalReads, fixture.rpc.callKVs, err)
	}
}

// A certificate does not grant the earlier transport's network identity to
// its replacement after the complete candidate proof has already been read.
func TestProductionRuntimeContinuityClosingReconnectRechecksNetwork(t *testing.T) {
	fixture := newProductionContinuityPolicyTestFixture(t)
	native := fixture.owner.rpc.native
	client := &productionTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	canonicalReads, heads := 0, 0
	faulted := false
	fixture.fault = func(_ context.Context, _ any, method string, args ...any) (bool, error) {
		if method == "chain_getFinalizedHead" {
			heads++
		}
		if method == "chain_getBlockHash" && args[0] == uint64(150) {
			canonicalReads++
			if heads >= 2 && !faulted {
				faulted = true
				client.generation.Add(1)
				fixture.owner.rpc.nativeChain = "replacement.example"
			}
		}
		return false, nil
	}
	observed, err := InspectProductionRuntimeContinuityContext(t.Context(), native, fixture.owner.cfg, fixture.hashes[150], fixture.policyRaw, fixture.certificateRaw)
	if observed != nil || err == nil || retryableProductionSteeringRead(err) || !faulted || fixture.owner.rpc.callKVs["system_chain"] != 2 {
		t.Fatalf("continuity certificate borrowed earlier network identity: canonical=%d observed=%+v err=%v", canonicalReads, observed, err)
	}
}

// A completed owner census cannot survive a replacement during its last
// storage read. The retry must reproduce every registered owner and row.
func TestOwnerRecycleRuntimeReconnectRepeatsCompleteCensus(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	baseline, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain)
	if err != nil {
		t.Fatal(err)
	}
	client := &productionTransportTestClient{Client: fixture.chain.API.Client}
	client.generation.Store(1)
	fixture.chain.API.Client = client
	_, nextHead := releaseReceiptTestHeader(t, types.Hash(recycleTestId(1200)), 101)
	reads, networks, finalityReads := 0, 0, 0
	fixture.before = func(_ context.Context, method string, args []any) error {
		if method == "system_chain" {
			networks++
		}
		if method == "chain_getHeader" && args[0] == nextHead.Hex() {
			finalityReads++
		}
		if method == "state_getStorage" && args[0] == fixture.keys["cap"] {
			reads++
			if reads == 1 {
				client.generation.Add(1)
				fixture.headHash, fixture.headNumber = nextHead, 101
			}
		}
		return nil
	}
	observed, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain)
	if err != nil || reads != 2 || networks != 2 || finalityReads == 0 || !reflect.DeepEqual(observed, baseline) {
		t.Fatalf("reconnect retained a partial owner census or moved its original block: reads=%d networks=%d finality=%d observed=%+v err=%v", reads, networks, finalityReads, observed, err)
	}
}

// The decision validator rows and drained activation observation belong to
// one read, including storage after each nested artifact has authenticated.
func TestOwnerRecycleRuntimeReconnectRepeatsDecisionAndActivation(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, _ := fixture.stage(t)
	admission := fixture.operator.measurement.admission
	baseline, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority)
	if err != nil {
		t.Fatal(err)
	}
	original := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	call := original.callContext
	client := &productionTransportTestClient{Client: admission.chain.API.Client}
	client.generation.Store(1)
	admission.chain.API.Client = client
	activation := types.Hash(stage.proof.Eligibility.ActivationHash).Hex()
	activationReads, decisionReads := 0, 0
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := call(ctx, target, method, args...)
		if err == nil && method == "state_getStorage" {
			if args[0] == fixture.storageNameKVs["LastUpdate"] {
				decisionReads++
			}
			if args[0] == fixture.storageNameKVs["PendingServerEmission"] && args[1] == activation {
				activationReads++
				if activationReads == 1 {
					client.generation.Add(1)
				}
			}
		}
		return err
	}
	observed, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority)
	if err != nil || decisionReads != 2 || activationReads != 2 || !reflect.DeepEqual(observed, baseline) {
		t.Fatalf("reconnect mixed decision and activation censuses: decisions=%d activations=%d observed=%+v err=%v", decisionReads, activationReads, observed, err)
	}
}

// Fresh timestamp storage cannot carry earlier route authority across a
// replacement, even when both transports report the same runtime artifact.
func TestValidatorUploadRuntimeReconnectRepeatsNativeFreshness(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	owner := fixture.owner(t)
	native := fixture.production.rpc.native
	original := native.API.Client.(*validatorRuntimeIdentityTestClient)
	call := original.callContext
	client := &productionTransportTestClient{Client: original}
	client.generation.Store(1)
	native.API.Client = client
	storageReads, networks := 0, 0
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		err := call(ctx, target, method, args...)
		if err == nil && method == "system_chain" {
			networks++
		}
		if err == nil && method == "state_getStorage" {
			storageReads++
			if storageReads == 1 {
				client.generation.Add(1)
				fixture.production.rpc.genesis = types.Hash{0xf3}
			}
		}
		return err
	}
	metadata, runtime := native.Meta, native.Runtime
	observed, err := ValidatorUploadNativeObserverContext(t.Context(), native, owner.config.Deployment)
	if err == nil || observed != (ValidatorUploadNativeObserver{}) || storageReads != 1 || networks != 2 || native.Meta != metadata || native.Runtime != runtime {
		t.Fatalf("upload freshness borrowed the earlier native route: storage=%d networks=%d observation=%+v err=%v", storageReads, networks, observed, err)
	}
}
