package crv4

// Proof ownership outlives bounded metadata reuse. Synthetic block-selected
// runtimes exercise eviction and concurrent readers without a live chain.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Only counters change during reads; metadata bytes and chain authority remain
// immutable. Each nonzero first block byte selects one compatible artifact.
type runtimeProofFixture struct {
	chain         *Chain
	allowed       RuntimeArtifactIdentity
	metadataReads atomic.Int64
	observations  atomic.Int64
	identityReads atomic.Int64
}

// Every response names its exact block. All methods are read-only, and metadata
// is the existing synthetic consumed-interface fixture.
func newRuntimeProofFixture(t *testing.T) *runtimeProofFixture {
	t.Helper()
	_, _, encoded := provisionalRuntimeMetadataTest(t)
	self := &runtimeProofFixture{}
	var ok bool
	self.allowed, ok = ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: ReviewedRuntimeSpecVersion, TransactionVersion: 1, StateVersion: 1})
	if !ok {
		t.Fatal("reviewed fixture authority missing")
	}
	genesis := types.Hash{0x71}
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		self.identityReads.Add(1)
		if method == "chain_getBlockHash" {
			if len(args) != 1 || args[0] != uint64(0) {
				return errors.New("unexpected genesis selector")
			}
			value, ok := target.(*types.Hash)
			if !ok {
				return fmt.Errorf("unexpected genesis target %T", target)
			}
			*value = genesis
			return nil
		}
		if len(args) == 0 {
			return errors.New("runtime read has no block selector")
		}
		blockHex, ok := args[len(args)-1].(string)
		if !ok {
			return errors.New("runtime block selector has wrong type")
		}
		block, err := types.NewHashFromHexString(blockHex)
		if err != nil || block[0] == 0 {
			return errors.New("runtime block selector is outside synthetic history")
		}
		switch method {
		case "state_getRuntimeVersion":
			return setRuntimeIdentityTestResult(target, map[string]any{
				"specName": "node-subtensor", "specVersion": ReviewedRuntimeSpecVersion + uint32(block[0]),
				"transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}},
			})
		case "state_getStorageHash":
			if len(args) != 2 || args[0] != "0x3a636f6465" {
				return errors.New("unexpected runtime storage selector")
			}
			return setRuntimeIdentityTestResult(target, types.Hash{0x61, block[0]}.Hex())
		case "state_getMetadata":
			self.metadataReads.Add(1)
			return setRuntimeIdentityTestResult(target, encoded)
		default:
			return fmt.Errorf("unexpected or mutating runtime RPC %s", method)
		}
	}}
	self.chain = &Chain{API: &gsrpc.SubstrateAPI{Client: client}, GenesisHash: genesis}
	if err := self.chain.EnableProvisionalRuntimeCompatibility(genesis, func(AuthenticatedRuntimeArtifact) error {
		self.observations.Add(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return self
}

// The actual cache evicts at least one artifact, independent of map iteration
// order. Held proofs and bound historical views must all remain usable.
func TestProvisionalRuntimeProofSurvivesMetadataEviction(t *testing.T) {
	fixture := newRuntimeProofFixture(t)
	var artifacts []AuthenticatedRuntimeArtifact
	var views []*Chain
	for index := byte(1); index <= 9; index++ {
		artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{index}, fixture.allowed)
		if err != nil {
			t.Fatal(err)
		}
		view := *fixture.chain
		if err := view.BindRuntimeArtifact(artifact); err != nil {
			t.Fatal(err)
		}
		artifacts, views = append(artifacts, artifact), append(views, &view)
	}
	policy := fixture.chain.provisionalRuntime
	if len(policy.artifacts) != 8 {
		t.Fatalf("resident cache=%d, want 8", len(policy.artifacts))
	}
	evicted := 0
	for index, artifact := range artifacts {
		identity := RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}
		if _, ok := policy.artifacts[identity]; ok {
			continue
		}
		evicted++
		if !fixture.chain.RuntimeArtifactCompatible(artifact) || views[index].CurrentRuntimeCompatibilityProfile() != ProvisionalRuntimeCompatibilityProfile {
			t.Fatal("metadata eviction revoked an already authenticated runtime proof")
		}
		lateView := *fixture.chain
		if err := lateView.BindRuntimeArtifact(artifact); err != nil {
			t.Fatalf("eviction between authentication and binding rejected the proof: %v", err)
		}
		prepared, key := sourcePreparedTest(t)
		prepared.SourceCommitment.GenesisHash = views[index].GenesisHash.Hex()
		prepared.SourceCommitment.RuntimeSpec = artifact.Version.SpecVersion
		prepared.SourceCommitment.CompatibilityProfile = ProvisionalRuntimeCompatibilityProfile
		signSourcePreparedTest(t, prepared, key, nil)
		if err := views[index].ValidatePreparedSource(prepared); err != nil {
			t.Fatalf("eviction rejected an unchanged retained source signature: %v", err)
		}
		beforeMetadata, beforeObservation, beforeIdentity := fixture.metadataReads.Load(), fixture.observations.Load(), fixture.identityReads.Load()
		again, err := AuthenticateRuntimeArtifactAtContext(t.Context(), views[index], artifact.BlockHash, identity)
		if err != nil || again.Metadata != artifact.Metadata || !views[index].RuntimeArtifactCompatible(again) {
			t.Fatalf("retained exact historical pin failed after eviction: %v", err)
		}
		if fixture.metadataReads.Load() != beforeMetadata || fixture.observations.Load() != beforeObservation || fixture.identityReads.Load()-beforeIdentity != 4 {
			t.Fatalf("retained proof must avoid repeated metadata/profile audit and repeat four identity reads: metadata=%d audit=%d identity=%d", fixture.metadataReads.Load()-beforeMetadata, fixture.observations.Load()-beforeObservation, fixture.identityReads.Load()-beforeIdentity)
		}
	}
	if evicted == 0 {
		t.Fatal("fixture did not force real bounded-cache eviction")
	}
}

// Retained metadata never replaces fresh exact-block identity reads. Altered
// genesis, consumed APIs, or code must still reject an otherwise valid proof.
func TestProvisionalRuntimeProofRechecksChainAndArtifactOnReuse(t *testing.T) {
	fixture := newRuntimeProofFixture(t)
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.chain.BindRuntimeArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	identity := RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	for _, fault := range []string{"genesis", "code", "api"} {
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			switch {
			case fault == "genesis" && method == "chain_getBlockHash":
				*target.(*types.Hash) = types.Hash{0x75}
				return nil
			case fault == "code" && method == "state_getStorageHash":
				return setRuntimeIdentityTestResult(target, types.Hash{0x76}.Hex())
			case fault == "api" && method == "state_getRuntimeVersion":
				return setRuntimeIdentityTestResult(target, map[string]any{"specName": artifact.Version.SpecName, "specVersion": artifact.Version.SpecVersion, "transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 3}}})
			default:
				return original(ctx, target, method, args...)
			}
		}
		if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, artifact.BlockHash, identity); err == nil {
			t.Errorf("retained proof bypassed changed %s", fault)
		}
	}
	if fixture.metadataReads.Load() != 1 || fixture.observations.Load() != 1 {
		t.Fatal("failed identity reads rewrote the original proof")
	}
}

// Proof reuse cannot relabel a tuple, transfer connection ownership or turn a
// provisional result into strict/mainnet authority. Failed binding is atomic.
func TestProvisionalRuntimeProofRejectsRelabelingAndForeignOwners(t *testing.T) {
	fixture := newRuntimeProofFixture(t)
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.chain.BindRuntimeArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name   string
		mutate func(*AuthenticatedRuntimeArtifact)
	}{
		{name: "version", mutate: func(value *AuthenticatedRuntimeArtifact) { value.Version.SpecVersion++ }},
		{name: "code", mutate: func(value *AuthenticatedRuntimeArtifact) { value.CodeHash = types.Hash{0x72}.Hex() }},
		{name: "metadata hash", mutate: func(value *AuthenticatedRuntimeArtifact) { value.MetadataHash = types.Hash{0x73}.Hex() }},
		{name: "metadata object", mutate: func(value *AuthenticatedRuntimeArtifact) { value.Metadata = new(types.Metadata) }},
		{name: "genesis", mutate: func(value *AuthenticatedRuntimeArtifact) { value.GenesisHash = types.Hash{0x74} }},
		{name: "profile", mutate: func(value *AuthenticatedRuntimeArtifact) { value.CompatibilityProfile = "" }},
		{name: "forged marker", mutate: func(value *AuthenticatedRuntimeArtifact) { value.compatibilityProof = nil }},
	} {
		changed := artifact
		change.mutate(&changed)
		if fixture.chain.RuntimeArtifactCompatible(changed) || fixture.chain.BindRuntimeArtifact(changed) == nil {
			t.Errorf("%s inherited a genuine proof", change.name)
		}
		if fixture.chain.Meta != artifact.Metadata || fixture.chain.CurrentRuntimeCompatibilityProfile() != ProvisionalRuntimeCompatibilityProfile {
			t.Fatalf("%s partial bind replaced the old view", change.name)
		}
	}
	foreign := newRuntimeProofFixture(t)
	if foreign.chain.RuntimeArtifactCompatible(artifact) || foreign.chain.BindRuntimeArtifact(artifact) == nil {
		t.Fatal("a separate connection inherited another owner's admission")
	}
	strict := *fixture.chain
	strict.provisionalRuntime = nil
	if strict.RuntimeArtifactCompatible(artifact) || strict.CurrentRuntimeCompatibilityProfile() != "" || strict.BindRuntimeArtifact(artifact) == nil {
		t.Fatal("strict consumer inherited provisional compatibility")
	}
	changedView := *fixture.chain
	changedView.Runtime = &types.RuntimeVersion{SpecName: artifact.Version.SpecName, SpecVersion: types.U32(artifact.Version.SpecVersion + 1), TransactionVersion: 1}
	if changedView.CurrentRuntimeCompatibilityProfile() != "" {
		t.Fatal("retained proof authorized a changed signing domain")
	}
}

// Cold reconnections reauthenticate and observe rather than trusting a proof
// from another owner; the original view still survives local cache eviction.
func TestProvisionalRuntimeProofColdOwnerReauthenticates(t *testing.T) {
	first := newRuntimeProofFixture(t)
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), first.chain, types.Hash{1}, first.allowed)
	if err != nil {
		t.Fatal(err)
	}
	second := newRuntimeProofFixture(t)
	identity := RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), second.chain, artifact.BlockHash, identity); err == nil {
		t.Fatal("unobserved exact pin supplied its own provisional authority")
	}
	rechecked, err := AuthenticateRuntimeArtifactAtContext(t.Context(), second.chain, artifact.BlockHash, second.allowed)
	if err != nil || second.metadataReads.Load() != 1 || second.observations.Load() != 1 || rechecked.Metadata == artifact.Metadata || !second.chain.RuntimeArtifactCompatible(rechecked) {
		t.Fatalf("cold owner did not establish fresh authority: %v", err)
	}
}

// One immutable bound view is read concurrently while the shared cache churns.
// Explicit completion joins all work; no runtime fields mutate under readers.
func TestProvisionalRuntimeProofConcurrentEvictionPreservesBoundView(t *testing.T) {
	fixture := newRuntimeProofFixture(t)
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed)
	if err != nil {
		t.Fatal(err)
	}
	view := *fixture.chain
	if err := view.BindRuntimeArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	identity := RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}
	start := make(chan struct{})
	var workers sync.WaitGroup
	proofErrors := make(chan error, 2)
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for index := byte(2); index <= 18; index++ {
			if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{index}, fixture.allowed); err != nil {
				proofErrors <- err
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 32 {
			if !view.RuntimeArtifactCompatible(artifact) || view.CurrentRuntimeCompatibilityProfile() != ProvisionalRuntimeCompatibilityProfile {
				proofErrors <- fmt.Errorf("bound proof changed during cache churn")
				return
			}
			if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), &view, artifact.BlockHash, identity); err != nil {
				proofErrors <- err
				return
			}
		}
	}()
	close(start)
	workers.Wait()
	close(proofErrors)
	for err := range proofErrors {
		t.Error(err)
	}
	if fixture.metadataReads.Load() != 18 || fixture.observations.Load() != 18 || len(fixture.chain.provisionalRuntime.artifacts) > 8 {
		t.Fatalf("bound proof was repeatedly audited or cache overflowed: metadata=%d audit=%d resident=%d", fixture.metadataReads.Load(), fixture.observations.Load(), len(fixture.chain.provisionalRuntime.artifacts))
	}
}
