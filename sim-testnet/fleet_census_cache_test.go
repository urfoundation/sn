// Exercise durable reuse through actual file reads and signed fleet fixtures.
// Source mutation and recovery are explicit transitions, never timing guesses.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

type fleetCensusTestFixture struct {
	cfg         *ResolvedConfig
	stateDir    string
	coordinator common.Address
	descriptors []fleetLifecycleEvidenceDescriptor
}

// Each fleet uses a real signature and distinct client/uid. The small census
// makes invalidation deterministic without requiring a network or real roles.
func newFleetCensusTestFixture(t *testing.T, fleets int) fleetCensusTestFixture {
	t.Helper()
	cfg, coordinator, setup, binding, commitment := oneFleetEvidenceTestFixture(t)
	cfg.Config.Topology.HeadFleets = fleets
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := hex.DecodeString(roles.Clients["miner-1"].SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	client := ed25519.NewKeyFromSeed(seed)
	hotkey, err := crv4.KeypairFromSeedHex(roles.Substrate[fleetHotkeyLabel(1)].SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	fixture := fleetCensusTestFixture{cfg: cfg, coordinator: coordinator, stateDir: t.TempDir()}
	if err := os.Chmod(fixture.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(fixture.stateDir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	encodeHex := func(raw []byte) string { return "0x" + hex.EncodeToString(raw) }
	for fleet := 1; fleet <= fleets; fleet++ {
		descriptor := standardFleetEvidenceDescriptor(cfg, fleet)
		fixture.descriptors = append(fixture.descriptors, descriptor)
		manifest, err := protocol.ParseFleetManifest(setup["fleet_1_manifest"])
		if err != nil {
			t.Fatal(err)
		}
		manifest.FleetID[0], manifest.Members[0].ClientID[0] = byte(fleet), byte(fleet)
		canonical, err := manifest.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		signed, err := manifest.Binding(manifest.Members[0], binding.ValidFromEpoch, binding.ValidToEpoch)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := signed.Digest()
		if err != nil {
			t.Fatal(err)
		}
		clientSignature, err := signed.SignClient(client)
		if err != nil {
			t.Fatal(err)
		}
		hotkeySignature, err := hotkey.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		member := binding
		member.UID = uint16(fleet + 6)
		member.ClientID, member.FleetID = encodeHex(signed.ClientID[:]), encodeHex(signed.FleetID[:])
		member.CommitmentHash, member.BindingDigest = encodeHex(signed.CommitmentHash[:]), encodeHex(digest[:])
		member.ClientSignature, member.HotkeySignature = encodeHex(clientSignature), encodeHex(hotkeySignature)
		committed := commitment
		committed.ManifestURI, committed.CommitmentHash = descriptor.ManifestName, member.CommitmentHash
		if err := atomicWrite(filepath.Join(fixture.stateDir, "public", descriptor.ManifestName), canonical, 0o644); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]any{descriptor.CommitmentName: committed, descriptor.BindingNames[0]: member} {
			if err := writePublicJSON(filepath.Join(fixture.stateDir, "public", name), value); err != nil {
				t.Fatal(err)
			}
		}
	}
	return fixture
}

// A new cache object models a new runner process; no in-memory success crosses
// this boundary. Counting the real read callback proves the cold work stopped.
func (self fleetCensusTestFixture) cacheWithReadCount() (*fleetCensusCache, *int) {
	reads := new(int)
	cache := newFleetCensusCache(self.cfg, self.stateDir, self.coordinator)
	cache.readFile = func(path string) ([]byte, error) {
		*reads++
		return os.ReadFile(path)
	}
	return cache, reads
}

// Assert the actual live-observation projection, including retained uid and
// bound-member counts when an otherwise authentic fleet has expired.
func (self fleetCensusTestFixture) inspect(t *testing.T, epoch uint64, cache *fleetCensusCache, valid bool) {
	t.Helper()
	committed, count, bound, uids, hotkeys, groups := inspectFleetEvidenceCensus(self.cfg, self.coordinator, epoch, self.descriptors, cache)
	if !committed || count != len(self.descriptors) || bound != valid || len(uids) != len(self.descriptors) {
		t.Fatalf("census epoch=%d commitments=%t count=%d bound=%t uids=%v, want validity=%t", epoch, committed, count, bound, uids, valid)
	}
	if valid && (len(hotkeys) != len(self.descriptors) || len(groups) != len(self.descriptors)) {
		t.Fatal("accepted census lost hotkeys or member groups")
	}
	if !valid && (hotkeys != nil || groups != nil) {
		t.Fatal("inactive census leaked current candidate identities")
	}
}

func TestFleetCensusCacheReusesAcrossRunnerRecoveryAndEpochs(t *testing.T) {
	fixture := newFleetCensusTestFixture(t, 2)
	cache, reads := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	if *reads != 6 || len(cache.proof.Fleets) != 2 {
		t.Fatalf("cold census reads=%d proofs=%d, want 6/2", *reads, len(cache.proof.Fleets))
	}
	for _, epoch := range []uint64{2, 8, 9, 1, 10} {
		cache, reads = fixture.cacheWithReadCount()
		fixture.inspect(t, epoch, cache, epoch >= 2 && epoch <= 9)
		if *reads != 0 {
			t.Fatalf("recovery at epoch %d repeated %d immutable source reads", epoch, *reads)
		}
	}
}

func TestInspectFleetEvidencePersistsAndUsesExactCensus(t *testing.T) {
	fixture := newFleetCensusTestFixture(t, 2)
	if err := writePublicJSON(filepath.Join(fixture.stateDir, "public", "contracts.json"), ContractDeployment{CoordinatorProxy: fixture.coordinator}); err != nil {
		t.Fatal(err)
	}
	committed, count, bound, _, _, _ := inspectFleetEvidence(fixture.cfg, fixture.stateDir, 2)
	if !committed || count != 2 || !bound {
		t.Fatalf("live census failed: committed=%t count=%d bound=%t", committed, count, bound)
	}
	cache, reads := fixture.cacheWithReadCount()
	if len(cache.proof.Fleets) != 2 {
		t.Fatal("live observation did not persist immutable census proofs")
	}
	fixture.inspect(t, 3, cache, true)
	if *reads != 0 {
		t.Fatalf("live observation checkpoint did not survive recovery: %d source reads", *reads)
	}
}

func TestFleetCensusCacheChangedFleetDoesNotReplayUnchangedFleet(t *testing.T) {
	fixture := newFleetCensusTestFixture(t, 2)
	cache, _ := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	path := filepath.Join(fixture.stateDir, "public", fixture.descriptors[1].BindingNames[0])
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cache, reads := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	if *reads != 3 {
		t.Fatalf("one replaced fleet caused %d reads, want only its 3 inputs", *reads)
	}
	cache, reads = fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	if *reads != 0 {
		t.Fatalf("new verified fleet was not checkpointed: %d reads", *reads)
	}
}

func TestFleetCensusCacheRejectsChangedSignatureWithRestoredMtime(t *testing.T) {
	fixture := newFleetCensusTestFixture(t, 1)
	path := filepath.Join(fixture.stateDir, "public", fixture.descriptors[0].BindingNames[0])
	fixed := time.Unix(1234, 0)
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	cache, _ := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var binding FleetBindingEvidence
	if err := json.Unmarshal(raw, &binding); err != nil {
		t.Fatal(err)
	}
	bad := []byte(strings.Replace(string(raw), binding.ClientSignature, "0x"+strings.Repeat("00", 64), 1))
	if len(bad) != len(raw) {
		t.Fatal("mutation changed source size")
	}
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	cache, reads := fixture.cacheWithReadCount()
	committed, count, bound, _, _, _ := inspectFleetEvidenceCensus(fixture.cfg, fixture.coordinator, 2, fixture.descriptors, cache)
	if !committed || bound || count != 0 || *reads != 3 {
		t.Fatalf("rewritten signature reused a proof: committed=%t bound=%t count=%d reads=%d", committed, bound, count, *reads)
	}
}

func TestFleetCensusCacheRejectsMutationAfterWarmLookup(t *testing.T) {
	fixture := newFleetCensusTestFixture(t, 1)
	cache, _ := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	cache, reads := fixture.cacheWithReadCount()
	cache.beforeRecheck = func() {
		if err := os.Remove(filepath.Join(fixture.stateDir, "public", fixture.descriptors[0].BindingNames[0])); err != nil {
			t.Fatal(err)
		}
	}
	committed, count, bound, _, _, _ := inspectFleetEvidenceCensus(fixture.cfg, fixture.coordinator, 2, fixture.descriptors, cache)
	if committed || bound || count != 0 || *reads != 0 {
		t.Fatalf("source disappeared after warm lookup: committed=%t bound=%t count=%d reads=%d", committed, bound, count, *reads)
	}
}

func TestFleetCensusCacheTamperedProofFallsBackToSignedEvidence(t *testing.T) {
	fixture := newFleetCensusTestFixture(t, 1)
	cache, _ := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	path := filepath.Join(fixture.stateDir, fleetCensusCacheDirectoryName, cache.name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope fleetCensusCacheEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	proof := envelope.Proof.Fleets[1]
	proof.Uid++
	envelope.Proof.Fleets[1] = proof
	raw, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cache, reads := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	if *reads != 3 || cache.proof.Fleets[1].Uid != 7 {
		t.Fatalf("tampered cache uid authorized: reads=%d uid=%d", *reads, cache.proof.Fleets[1].Uid)
	}
}

func TestFleetCensusCacheVerifierAndCustodyChangesRequireReverification(t *testing.T) {
	for _, change := range []string{"verifier", "custody"} {
		fixture := newFleetCensusTestFixture(t, 1)
		cache, _ := fixture.cacheWithReadCount()
		fixture.inspect(t, 2, cache, true)
		if change == "verifier" {
			cache.proof.VerifierVersion = "different-verification-rules"
			cache.dirty = true
			cache.save()
		} else {
			fixture.cfg.WalletMaterial += "-other-test-custody"
		}
		cache, reads := fixture.cacheWithReadCount()
		fixture.inspect(t, 2, cache, true)
		if *reads != 3 {
			t.Fatalf("%s change inherited cached signatures with %d reads", change, *reads)
		}
	}
}

func TestFleetCensusCacheRechecksPolicyAndCoordinatorIdentity(t *testing.T) {
	for _, change := range []string{"policy", "coordinator"} {
		fixture := newFleetCensusTestFixture(t, 1)
		cache, _ := fixture.cacheWithReadCount()
		fixture.inspect(t, 2, cache, true)
		if change == "policy" {
			fixture.cfg.Policy.Binding.MaximumValidityEpochs = 1
		} else {
			fixture.coordinator[0] ^= 1
		}
		cache, reads := fixture.cacheWithReadCount()
		_, _, bound, _, _, _ := inspectFleetEvidenceCensus(fixture.cfg, fixture.coordinator, 2, fixture.descriptors, cache)
		if bound || *reads != 3 {
			t.Fatalf("%s change inherited an incompatible proof: bound=%t reads=%d", change, bound, *reads)
		}
	}
}

func TestFleetCensusCacheRechecksDescriptorAndDuplicateCensus(t *testing.T) {
	fixture := newFleetCensusTestFixture(t, 2)
	cache, _ := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, true)
	// Selecting another active generation is a new descriptor, even if it
	// points at already verified bytes. Reusing fleet 1 must expose duplicates.
	fixture.descriptors[1] = fixture.descriptors[0]
	cache, reads := fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, false)
	if *reads != 3 {
		t.Fatalf("changed descriptor read %d inputs, want 3", *reads)
	}
	cache, reads = fixture.cacheWithReadCount()
	fixture.inspect(t, 2, cache, false)
	if *reads != 0 {
		t.Fatalf("warm duplicate census repeated %d immutable reads", *reads)
	}
}

func TestExactVerifiedPlanActionIndexPreservesSingleActionRules(t *testing.T) {
	plan := &SetupPlan{PlanHash: "approved", PriorPlanHashes: []string{"ancestor"}, Actions: []Action{
		{ID: "current", IntentHash: "new"},
		{ID: "carried", IntentHash: "new", AcceptedPriorIntentHashes: []string{"old"}},
		{ID: "wrong-intent", IntentHash: "new"},
		{ID: "foreign-plan", IntentHash: "new"},
		{ID: "unfinished", IntentHash: "new"},
		{ID: "duplicate", IntentHash: "new"},
		{ID: "duplicate", IntentHash: "new"},
		{ID: "duplicate", IntentHash: "new"},
	}}
	entries := []JournalEntry{
		{PlanHash: "approved", ActionID: "current", IntentHash: "new", Stage: StageVerified},
		{PlanHash: "ancestor", ActionID: "carried", IntentHash: "old", Stage: StageVerified},
		{PlanHash: "approved", ActionID: "wrong-intent", IntentHash: "old", Stage: StageVerified},
		{PlanHash: "foreign", ActionID: "foreign-plan", IntentHash: "new", Stage: StageVerified},
		{PlanHash: "approved", ActionID: "unfinished", IntentHash: "new", Stage: StageFinalized},
		{PlanHash: "approved", ActionID: "duplicate", IntentHash: "new", Stage: StageVerified},
		{PlanHash: "approved", ActionID: "absent", IntentHash: "new", Stage: StageVerified},
	}
	got := exactVerifiedPlanActionIndex(plan, entries)
	want := map[string]bool{"current": true, "carried": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("verified index=%v, want %v", got, want)
	}
	for _, entry := range entries {
		if got[entry.ActionID] != exactVerifiedPlanAction(plan, entries, entry.ActionID) {
			t.Fatalf("index differs from exact verifier for %s", entry.ActionID)
		}
	}
}

func BenchmarkExactVerifiedPlanActionIndex(b *testing.B) {
	plan := &SetupPlan{PlanHash: "approved", PriorPlanHashes: []string{"ancestor"}}
	entries := make([]JournalEntry, 0, 2400)
	for index := 0; index < 800; index++ {
		id := fmt.Sprintf("binding-%d", index)
		plan.Actions = append(plan.Actions, Action{ID: id, IntentHash: "intent"})
		for _, stage := range []JournalStage{StageIntent, StageFinalized, StageVerified} {
			entries = append(entries, JournalEntry{PlanHash: "approved", ActionID: id, IntentHash: "intent", Stage: stage})
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if len(exactVerifiedPlanActionIndex(plan, entries)) != len(plan.Actions) {
			b.Fatal("verified member count changed")
		}
	}
}
