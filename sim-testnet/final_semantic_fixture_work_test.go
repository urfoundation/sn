package main

// Fixture work follows the same ownership graph as the evidence: two
// independent validator histories, ordered epochs, and a later output phase.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Runs only the two fixed validator owners through the existing joined test
// worker boundary. Each caller supplies detached cycle and ledger storage.
func sealFinalSemanticFixtureValidatorChains(chains [2][]*FinalCRv4Cycle, seal func(FinalCRv4Cycle, uint64) FinalCRv4Cycle) []error {
	if seal == nil {
		return []error{errors.New("fixture cycle seal is unavailable")}
	}
	seen := map[*FinalCRv4Cycle]bool{}
	for _, chain := range chains {
		if len(chain) == 0 {
			return []error{errors.New("fixture validator epoch owner is empty")}
		}
		for index, cycle := range chain {
			if cycle == nil || cycle.SettlementEpoch == 0 || seen[cycle] || index > 0 && cycle.SettlementEpoch != chain[index-1].SettlementEpoch+1 {
				return []error{errors.New("fixture validator epoch ownership is aliased or out of order")}
			}
			seen[cycle] = true
		}
	}
	cases := make([]finalSemanticTestCase, len(chains))
	for index, chain := range chains {
		cases[index] = finalSemanticTestCase{name: fmt.Sprintf("validator-%d", index+1), verify: func(context.Context) error {
			for _, cycle := range chain {
				*cycle = seal(*cycle, uint64(index+1))
			}
			return nil
		}}
	}
	return runFinalSemanticTestCases(context.Background(), cases)
}

// Explicit entry/release barriers prove two owners overlap while every
// dependent epoch remains ordered and executes exactly once before return.
func TestFinalSemanticFixtureValidatorChainsAreIndependentAndJoined(t *testing.T) {
	first := []FinalCRv4Cycle{{SettlementEpoch: 10}, {SettlementEpoch: 11}, {SettlementEpoch: 12}}
	second := []FinalCRv4Cycle{{SettlementEpoch: 10}, {SettlementEpoch: 11}, {SettlementEpoch: 12}}
	chains := [2][]*FinalCRv4Cycle{
		{&first[0], &first[1], &first[2]},
		{&second[0], &second[1], &second[2]},
	}
	entered := make(chan uint64, 2)
	release := make(chan struct{})
	joined := make(chan []error, 1)
	counts := [2]int{}
	go func() {
		joined <- sealFinalSemanticFixtureValidatorChains(chains, func(cycle FinalCRv4Cycle, validatorID uint64) FinalCRv4Cycle {
			index := validatorID - 1
			if cycle.SettlementEpoch != uint64(10+counts[index]) {
				t.Errorf("validator %d epoch %d preceded its owner cursor %d", validatorID, cycle.SettlementEpoch, counts[index])
			}
			counts[index]++
			if cycle.SettlementEpoch == 10 {
				entered <- validatorID
				<-release
			}
			cycle.ValuesHash = fmt.Sprintf("owner-%d-epoch-%d", validatorID, cycle.SettlementEpoch)
			return cycle
		})
	}()
	firstOwner, secondOwner := <-entered, <-entered
	close(release)
	errs := <-joined
	if firstOwner == secondOwner || len(errs) != 2 || errs[0] != nil || errs[1] != nil || counts != [2]int{3, 3} {
		t.Fatalf("fixture owners did not complete independently: owners=%d/%d counts=%v errors=%v", firstOwner, secondOwner, counts, errs)
	}
	for index, chain := range chains {
		for _, cycle := range chain {
			if cycle.ValuesHash != fmt.Sprintf("owner-%d-epoch-%d", index+1, cycle.SettlementEpoch) {
				t.Fatalf("fixture owner %d exposed an unjoined or foreign epoch %d", index+1, cycle.SettlementEpoch)
			}
		}
	}
}

// Shared mutable slots and missing/rolled-back epochs refuse before either
// owner runs; no scheduler-dependent data race is required to expose them.
func TestFinalSemanticFixtureValidatorChainsRejectAliasedOrSkippedEpochs(t *testing.T) {
	for _, invalid := range []string{"aliased", "nil", "skipped", "reversed", "zero", "empty", "missing-seal"} {
		first := []FinalCRv4Cycle{{SettlementEpoch: 10}, {SettlementEpoch: 11}}
		second := []FinalCRv4Cycle{{SettlementEpoch: 10}, {SettlementEpoch: 11}}
		chains := [2][]*FinalCRv4Cycle{{&first[0], &first[1]}, {&second[0], &second[1]}}
		var calls atomic.Int32
		seal := func(value FinalCRv4Cycle, _ uint64) FinalCRv4Cycle { calls.Add(1); return value }
		switch invalid {
		case "aliased":
			chains[1][0] = chains[0][0]
		case "nil":
			chains[1][1] = nil
		case "skipped":
			second[1].SettlementEpoch++
		case "reversed":
			second[1].SettlementEpoch = 9
		case "zero":
			second[0].SettlementEpoch = 0
		case "empty":
			chains[1] = nil
		case "missing-seal":
			seal = nil
		}
		errs := sealFinalSemanticFixtureValidatorChains(chains, seal)
		if len(errs) != 1 || errs[0] == nil || calls.Load() != 0 {
			t.Fatalf("%s fixture chain reached a seal: calls=%d errors=%v", invalid, calls.Load(), errs)
		}
	}
}

// Goexit cannot turn a partially constructed chain into success or strand its
// independent peer. The existing worker helper retains the missing return.
func TestFinalSemanticFixtureValidatorChainsJoinAfterGoexit(t *testing.T) {
	first := []FinalCRv4Cycle{{SettlementEpoch: 10}, {SettlementEpoch: 11}}
	second := []FinalCRv4Cycle{{SettlementEpoch: 10}, {SettlementEpoch: 11}}
	chains := [2][]*FinalCRv4Cycle{{&first[0], &first[1]}, {&second[0], &second[1]}}
	counts := [2]int{}
	errs := sealFinalSemanticFixtureValidatorChains(chains, func(value FinalCRv4Cycle, validatorID uint64) FinalCRv4Cycle {
		counts[validatorID-1]++
		if validatorID == 1 {
			runtime.Goexit()
		}
		return value
	})
	if len(errs) != 2 || errs[0] == nil || errs[1] != nil || counts != [2]int{1, 2} {
		t.Fatalf("non-returning fixture owner hid a partial chain or peer: counts=%v errors=%v", counts, errs)
	}
}

// A foreign deployment is refused before attempting a malformed signed
// closure. Restoring only that deployment reaches the unchanged closure
// decoder, proving successful executable admission still requires replay.
func TestFinalSemanticArtifactDeploymentAdmissionPrecedesSignedReplay(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	if source.ExpectedMiners != 1000 || source.Topology.HeadCandidateFleets != 202 || source.Topology.HeadSlots != 200 || len(source.Validators) != 2 || len(source.Pools) != 2 {
		t.Fatal("deployment admission fixture lost its complete release population")
	}
	if len(source.PathProofs) == 0 || len(source.PathProofs[0].SettlementClosures) == 0 {
		t.Fatal("deployment admission fixture has no genuine signed terminal closure")
	}
	deploymentBytes := append([]byte(nil), artifacts[source.Deployment.Artifact.URI]...)
	plan, err := decodePersistedPlanBytes(artifacts[source.PlanArtifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	lock, err := decodeReleaseLockBytes(artifacts[source.ReleaseLockArtifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalDeploymentArtifact(&source, plan, lock, deploymentBytes); err != nil {
		t.Fatalf("authentic executable prerequisite: %v", err)
	}
	closureURI := source.PathProofs[0].SettlementClosures[0].Artifact.URI
	serverKeys := map[uint64]map[byte]ed25519.PublicKey{}
	for _, pool := range source.Pools {
		keys := map[byte]ed25519.PublicKey{}
		for _, historical := range pool.ServerKeyHistory {
			key, err := finalEd25519PublicKey("fixture closure server key", historical.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			keys[historical.KeyID] = key
		}
		serverKeys[pool.NoID] = keys
	}
	closure, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(artifacts[closureURI], serverKeys)
	if err != nil || closure.Epoch != source.PathProofs[0].SettlementClosures[0].Epoch || len(closure.Transitions) != 2 {
		t.Fatalf("real signed terminal prerequisite: %v", err)
	}
	badClosure := []byte("!")
	artifacts[closureURI] = badClosure
	for proofIndex := range source.PathProofs {
		for closureIndex := range source.PathProofs[proofIndex].SettlementClosures {
			locator := &source.PathProofs[proofIndex].SettlementClosures[closureIndex].Artifact
			if locator.URI == closureURI {
				locator.ContentHash, locator.SizeBytes = bytesSHA256(badClosure), uint64(len(badClosure))
			}
		}
	}
	var deployment finalContractDeploymentArtifact
	if err := decodeStrictJSONBytes(deploymentBytes, &deployment); err != nil {
		t.Fatal(err)
	}
	deployment.RuntimeCodeHashes = finalDeploymentRuntimeMapCopy(deployment.RuntimeCodeHashes)
	deployment.RuntimeCodeHashes["0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"] = finalTestHex(0xe2)
	foreignDeployment, err := json.Marshal(deployment)
	if err != nil {
		t.Fatal(err)
	}
	load := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, found := artifacts[locator.URI]
		if !found {
			return nil, fmt.Errorf("missing full-scale artifact %s", locator.URI)
		}
		return append([]byte(nil), data...), nil
	}
	for _, invalidDeployment := range []bool{true, false} {
		data := deploymentBytes
		if invalidDeployment {
			data = foreignDeployment
		}
		artifacts[source.Deployment.Artifact.URI] = data
		source.Deployment.Artifact.ContentHash, source.Deployment.Artifact.SizeBytes = bytesSHA256(data), uint64(len(data))
		evidence, err := BuildFinalSemanticEvidence(source)
		if err != nil {
			t.Fatal(err)
		}
		err = VerifyFinalSemanticArtifacts(context.Background(), evidence, load)
		if invalidDeployment {
			if err == nil || !strings.Contains(err.Error(), "omits, adds, or substitutes") {
				t.Fatalf("deployment admission reached signed proof replay: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "invalid character '!'") {
			t.Fatalf("admitted executable omitted the actual signed closure decoder: %v", err)
		}
	}
}

// Builds the real release graph, authenticates its original head bindings and
// signed closure, then damages only the closure's retained bytes and locators.
func finalHeadProjectionAdmissionFixture(t *testing.T) (*FinalSemanticEvidence, map[string][]byte, FinalArtifactLoader) {
	t.Helper()
	source, artifacts := finalSemanticFixture(t)
	if source.ExpectedMiners != 1000 || source.Topology.HeadCandidateFleets != 202 || source.Topology.HeadSlots != 200 || len(source.Validators) != 2 || len(source.Pools) != 2 {
		t.Fatal("head admission fixture lost its complete release population")
	}
	evidence, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.HeadFleets) != 202 || len(evidence.PathProofs) == 0 || len(evidence.PathProofs[0].SettlementClosures) == 0 {
		t.Fatal("head admission fixture has no full fleet or signed closure census")
	}
	if err := verifyFinalHeadFleetBindingArtifacts(evidence, artifacts, artifacts[evidence.Topology.BindingManifest.URI]); err != nil {
		t.Fatalf("authentic head projection prerequisite: %v", err)
	}
	serverKeys := map[uint64]map[byte]ed25519.PublicKey{}
	for _, pool := range evidence.Pools {
		keys := map[byte]ed25519.PublicKey{}
		for _, historical := range pool.ServerKeyHistory {
			key, err := finalEd25519PublicKey("head admission closure server key", historical.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			keys[historical.KeyID] = key
		}
		serverKeys[pool.NoID] = keys
	}
	closureLocator := evidence.PathProofs[0].SettlementClosures[0]
	closure, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(artifacts[closureLocator.Artifact.URI], serverKeys)
	if err != nil || closure.Epoch != closureLocator.Epoch || len(closure.Transitions) != 2 {
		t.Fatalf("head admission signed terminal prerequisite: %v", err)
	}
	badClosure := []byte("!")
	artifacts[closureLocator.Artifact.URI] = badClosure
	for proofIndex := range evidence.PathProofs {
		for closureIndex := range evidence.PathProofs[proofIndex].SettlementClosures {
			locator := &evidence.PathProofs[proofIndex].SettlementClosures[closureIndex].Artifact
			if locator.URI == closureLocator.Artifact.URI {
				locator.ContentHash, locator.SizeBytes = bytesSHA256(badClosure), uint64(len(badClosure))
			}
		}
	}
	evidence.EvidenceHash, err = finalSemanticEvidenceHash(evidence)
	if err != nil {
		t.Fatal(err)
	}
	load := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, found := artifacts[locator.URI]
		if !found {
			return nil, fmt.Errorf("missing full-scale head admission artifact %s", locator.URI)
		}
		return append([]byte(nil), data...), nil
	}
	return evidence, artifacts, load
}

// The original signed decoder is an observable work boundary: a bad head
// projection must refuse first, and restoring only that projection reaches it.
func TestFinalSemanticFixtureHeadProjectionAdmissionPrecedesSignedReplay(t *testing.T) {
	t.Parallel()
	evidence, _, load := finalHeadProjectionAdmissionFixture(t)
	originalFleetKey := evidence.HeadFleets[0].FleetKey
	evidence.HeadFleets[0].FleetKey = finalTestHex(0xef)
	if evidence.HeadFleets[0].FleetKey == originalFleetKey {
		t.Fatal("head projection mutation did not change its owned fixture")
	}
	verify := func() error {
		var err error
		evidence.EvidenceHash, err = finalSemanticEvidenceHash(evidence)
		if err != nil {
			return err
		}
		return VerifyFinalSemanticArtifacts(t.Context(), evidence, load)
	}
	if err := verify(); err == nil || !strings.Contains(err.Error(), "sealed replay projection differs") {
		t.Fatalf("head projection admission reached signed proof replay: %v", err)
	}
	evidence.HeadFleets[0].FleetKey = originalFleetKey
	if err := verify(); err == nil || !strings.Contains(err.Error(), "invalid character '!'") {
		t.Fatalf("admitted head projection omitted the actual signed closure decoder: %v", err)
	}
}

// Earlier identity admission still requires fresh whole-object authentication.
// Rehashing a foreign checkpoint cannot hide it behind an unrelated bad proof.
func TestFinalSemanticFixtureHeadManifestAdmissionRequiresAuthenticatedIdentity(t *testing.T) {
	t.Parallel()
	evidence, artifacts, load := finalHeadProjectionAdmissionFixture(t)
	locator := &evidence.HeadFleets[0].BindingArtifact
	originalLocator := *locator
	originalBytes := append([]byte(nil), artifacts[locator.URI]...)
	var binding struct {
		Manifest json.RawMessage `json:"manifest"`
		Uid      uint16          `json:"uid"`
		Snapshot ChainHead       `json:"snapshot"`
	}
	if err := decodeStrictJSONBytes(originalBytes, &binding); err != nil {
		t.Fatal(err)
	}
	binding.Uid ^= 1
	foreignBytes, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	artifacts[locator.URI] = foreignBytes
	verify := func() error {
		var err error
		evidence.EvidenceHash, err = finalSemanticEvidenceHash(evidence)
		if err != nil {
			return err
		}
		return VerifyFinalSemanticArtifacts(t.Context(), evidence, load)
	}
	if err := verify(); err == nil || !strings.Contains(err.Error(), "size or content hash mismatch") {
		t.Fatalf("unauthenticated head bytes reached structural admission: %v", err)
	}
	locator.ContentHash, locator.SizeBytes = bytesSHA256(foreignBytes), uint64(len(foreignBytes))
	if err := verify(); err == nil || !strings.Contains(err.Error(), "binding artifact differs from its signed identity/checkpoint") {
		t.Fatalf("head manifest identity admission reached signed proof replay: %v", err)
	}
	*locator = originalLocator
	artifacts[locator.URI] = originalBytes
	if err := verify(); err == nil || !strings.Contains(err.Error(), "invalid character '!'") {
		t.Fatalf("admitted head identity omitted the actual signed closure decoder: %v", err)
	}
}
