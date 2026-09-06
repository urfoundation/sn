// Fleet controls compare an independent preserved serial preparation and
// force ownership/lifecycle boundaries without repeating full public seals.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/connect"
)

// The complete cold graph supplies authenticated identity and public client
// IDs; deriving owned roles does not rebuild its measurements or terminal cuts.
func finalSemanticFixtureFleetTestContext(t *testing.T) finalSemanticFixtureFleetContext {
	t.Helper()
	evidence, artifacts := finalSemanticFixture(t)
	if evidence.ExpectedMiners != 1000 || evidence.ExpectedCandidates != 202 || evidence.ExpectedHeadSlots != 200 {
		t.Fatal("fleet control lost complete release census")
	}
	plan, err := decodePersistedPlanBytes(artifacts[evidence.PlanArtifact.URI])
	if err != nil || plan.PlanHash != evidence.PlanHash {
		t.Fatalf("fleet control plan differs: %v", err)
	}
	cfg := testResolvedConfig(t)
	cfg.Config.Deployment.DeploymentID = evidence.DeploymentID
	cfg.Netuid, cfg.ChainID, cfg.ConfigHash, cfg.PolicyHash = evidence.Netuid, evidence.ChainID, evidence.ConfigHash, evidence.PolicyHash
	cfg.Policy, err = finalSemanticFixturePolicy(&evidence, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var miners []FinalMinerProcessEvidence
	if err := decodeStrictJSONBytes(artifacts[evidence.Topology.MinerManifest.URI], &miners); err != nil {
		t.Fatal(err)
	}
	for index := range miners {
		role := roles.Clients[fmt.Sprintf("miner-%d", index+1)]
		id, err := connect.ParseId(miners[index].ClientID)
		if err != nil {
			t.Fatal(err)
		}
		role.ClientIDHex = hex.EncodeToString(id[:])
		roles.Clients[role.Label] = role
	}
	actions := make(map[string]Action, len(plan.Actions))
	for _, action := range plan.Actions {
		if _, duplicate := actions[action.ID]; duplicate {
			t.Fatal("fleet control has duplicate action")
		}
		actions[action.ID] = action
	}
	return finalSemanticFixtureFleetContext{cfg: cfg, roles: roles, actions: actions, deploymentID: evidence.DeploymentID, planHash: plan.PlanHash, chainID: evidence.ChainID, netuid: evidence.Netuid, coordinator: common.HexToAddress(evidence.Deployment.CoordinatorProxy)}
}

// Only a cryptographically verified randomized hotkey signature may differ.
// All message fields, Ed25519 signatures and declared digests remain exact.
func finalSemanticFixtureFleetComparableBinding(t *testing.T, source finalSemanticFixtureFleetContext, name string, data []byte, version finalSemanticFixtureFleetVersion, member int) []byte {
	t.Helper()
	if strings.HasSuffix(name, ".refresh.binding.json") {
		var value FleetRefreshBindingEvidence
		if err := decodeStrictJSONBytes(data, &value); err != nil {
			t.Fatal(err)
		}
		binding, revoke, err := fleetRefreshEvidenceBindings(value, source.coordinator, source.chainID, source.netuid)
		if err != nil || !fleetRefreshReplacementMatchesManifest(binding, version.manifest, member, version.commitmentHash, version.validFrom, version.validTo) {
			t.Fatalf("refresh preparation has invalid signed context: %v", err)
		}
		client, clientOK := evidenceFixedHex(value.ClientSignature, ed25519.SignatureSize)
		hotkey, hotkeyOK := evidenceFixedHex(value.HotkeySignature, ed25519.SignatureSize)
		revocation, revokeOK := evidenceFixedHex(value.RevokeSignature, ed25519.SignatureSize)
		if !clientOK || !hotkeyOK || !revokeOK || !binding.VerifyClient(client) || !binding.VerifyHotkey(hotkey) || !revoke.VerifyClient(ed25519.PublicKey(binding.ClientKey[:]), revocation) {
			t.Fatal("refresh preparation has invalid genuine signatures")
		}
		digest, err := binding.Digest()
		if err != nil || !strings.EqualFold(value.BindingDigest, "0x"+hex.EncodeToString(digest[:])) {
			t.Fatal("refresh binding digest differs")
		}
		revokeDigest, err := revoke.Digest()
		if err != nil || !strings.EqualFold(value.RevokeDigest, "0x"+hex.EncodeToString(revokeDigest[:])) {
			t.Fatal("refresh revoke digest differs")
		}
		value.HotkeySignature = ""
		comparable, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return comparable
	}
	var value FleetBindingEvidence
	if err := decodeStrictJSONBytes(data, &value); err != nil {
		t.Fatal(err)
	}
	if value.Schema != "urnetwork-fleet-binding-evidence-v1" || value.DeploymentID != source.deploymentID || value.PlanHash != source.planHash || value.Generation != version.manifest.Generation || value.ValidFromEpoch != version.validFrom || value.ValidToEpoch != version.validTo {
		t.Fatal("initial preparation has invalid declared context")
	}
	binding, err := version.manifest.Binding(version.manifest.Members[member-1], version.validFrom, version.validTo)
	if err != nil {
		t.Fatal(err)
	}
	fields := []struct {
		got  string
		want []byte
	}{
		{got: value.ClientID, want: binding.ClientID[:]}, {got: value.ClientKey, want: binding.ClientKey[:]},
		{got: value.FleetID, want: binding.FleetID[:]}, {got: value.Hotkey, want: binding.Hotkey[:]}, {got: value.CommitmentHash, want: binding.CommitmentHash[:]},
	}
	for _, field := range fields {
		got, valid := evidenceFixedHex(field.got, len(field.want))
		if !valid || !bytes.Equal(got, field.want) {
			t.Fatal("initial preparation signed fields differ from manifest")
		}
	}
	client, clientOK := evidenceFixedHex(value.ClientSignature, ed25519.SignatureSize)
	hotkey, hotkeyOK := evidenceFixedHex(value.HotkeySignature, ed25519.SignatureSize)
	if !clientOK || !hotkeyOK || !binding.VerifyClient(client) || !binding.VerifyHotkey(hotkey) {
		t.Fatal("initial preparation has invalid genuine signatures")
	}
	digest, err := binding.Digest()
	if err != nil || !strings.EqualFold(value.BindingDigest, "0x"+hex.EncodeToString(digest[:])) {
		t.Fatal("initial binding digest differs")
	}
	value.HotkeySignature = ""
	comparable, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return comparable
}

// Compare every fresh prepared fleet against the independent old serial code.
// Journal fact order is checked as separate commitment and binding phases.
func TestFinalSemanticFixtureFleetPreparationMatchesIndependentSerial(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	prepared, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	reference, referenceCommitments, referenceBindings := finalSemanticFixtureFleetSerialReference(t, source)
	var commitments, bindings []finalSemanticFixtureFleetMutation
	for index, job := range prepared {
		want := reference[index]
		if job.fleet != want.fleet || len(job.versions) != len(want.versions) || !reflect.DeepEqual(job.versions, want.versions) || len(job.files) != len(want.files) {
			t.Fatalf("fleet %d deterministic version preparation differs from old serial code", job.fleet)
		}
		commitments = append(commitments, job.commitments...)
		bindings = append(bindings, job.bindings...)
		for name, data := range job.files {
			if !strings.HasSuffix(name, ".binding.json") {
				if !bytes.Equal(data, want.files[name]) {
					t.Fatalf("deterministic fleet bytes differ at %s", name)
				}
				continue
			}
			generation := 0
			if strings.HasSuffix(name, ".refresh.binding.json") {
				generation = 1
			}
			var member int
			if _, err := fmt.Sscanf(name, fmt.Sprintf("public/fleet-%d-member-%%d", job.fleet), &member); err != nil || member < 1 || member > int(finalFleetGenerationMembersPerFleet) {
				t.Fatalf("bad genuine binding path %s: %v", name, err)
			}
			fresh := finalSemanticFixtureFleetComparableBinding(t, source, name, data, job.versions[generation], member)
			old := finalSemanticFixtureFleetComparableBinding(t, source, name, want.files[name], want.versions[generation], member)
			if !bytes.Equal(fresh, old) {
				t.Fatalf("signed fleet message or deterministic signature differs at %s", name)
			}
		}
	}
	wantCommitments := int(finalFleetGenerationSetupFleetCount*2 + finalFleetGenerationChallengerFleetCount)
	wantBindings := int((finalFleetGenerationSetupFleetCount + finalFleetGenerationChallengerFleetCount) * finalFleetGenerationMembersPerFleet)
	if wantCommitments != 402 || wantBindings != 808 || len(commitments) != wantCommitments || len(bindings) != wantBindings {
		t.Fatalf("fleet preparation changed complete journal fact census: commitments=%d/%d bindings=%d/%d, want original 402/808", len(commitments), wantCommitments, len(bindings), wantBindings)
	}
	if !reflect.DeepEqual(commitments, referenceCommitments) || !reflect.DeepEqual(bindings, referenceBindings) {
		t.Fatal("fleet preparation changed original commitment-before-binding journal facts")
	}
}

// Pin the actual release topology independently of derived lengths, including
// all non-fleet miners. Read installed coordinates from their real receipt;
// a signature-work total cannot determine the fleet partition by itself.
func TestFinalSemanticFixtureFleetTopologyAndInstalledCensus(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	topology := source.cfg.Config.Topology
	if topology.Miners != 1000 || topology.HeadFleets != 200 || topology.ChallengerFleets != 2 || topology.ClientsPerHeadFleet != 4 || finalFleetGenerationSetupFleetCount != 200 || finalFleetGenerationChallengerFleetCount != 2 || finalFleetGenerationMembersPerFleet != 4 || finalFleetGenerationBindingCheckLimit != 40 {
		t.Fatal("fleet control changed the original 1000-miner/202-fleet/four-client topology or 40-tuple bound")
	}
	wantMiner := 0
	for fleet := 1; fleet <= 202; fleet++ {
		for member := 1; member <= 4; member++ {
			wantMiner++
			if got := fleetMemberMinerIndex(source.cfg, fleet, member); got != wantMiner {
				t.Fatalf("canonical fleet %d member %d maps to miner %d, want %d", fleet, member, got, wantMiner)
			}
		}
	}
	if wantMiner != 808 || fleetMemberMinerIndex(source.cfg, 4, 4) != 16 || fleetMemberMinerIndex(source.cfg, 5, 4) != 20 {
		t.Fatal("first-wave failure target or complete fleet membership differs")
	}
	for miner := 1; miner <= 1000; miner++ {
		label := fmt.Sprintf("miner-%d", miner)
		role, exists := source.roles.Clients[label]
		if !exists || role.Label != label || role.ClientIDHex == "" || role.PublicKeyHex == "" {
			t.Fatalf("complete topology lost miner identity %d", miner)
		}
	}
	archive, batch := finalGenerationWorkFixture(t, nil)
	var receipt FleetInstallBatchEvidence
	path := fmt.Sprintf("public/fleet-install-batch-%d.json", batch.Batch)
	if err := decodeStrictJSONBytes(archive.archive.files[path], &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.InstalledFleets) == 0 || len(receipt.InstalledFleets)+len(receipt.CarriedFleets) != 10 || len(receipt.MemberEvidence) != 40 || len(receipt.InstalledFleets) != len(batch.InstalledFleets) || len(receipt.CarriedFleets) != len(batch.CarriedFleets) {
		t.Fatal("actual installed receipt lost its complete ten-fleet/four-member partition")
	}
	for index, fleet := range receipt.InstalledFleets {
		if fleet < 1 || uint64(fleet) != batch.InstalledFleets[index] {
			t.Fatal("installed fleet coordinates differ from the authenticated receipt")
		}
	}
	for index, fleet := range receipt.CarriedFleets {
		if fleet < 1 || uint64(fleet) != batch.CarriedFleets[index] {
			t.Fatal("carried fleet coordinates differ from the authenticated receipt")
		}
	}
	t.Logf("actual selected batch=%d installed=%v carried=%v clients_per_fleet=4 complete_member_paths=%d installed_member_checks=%d cache_bound=40 eviction_input_tuples=41", batch.Batch, receipt.InstalledFleets, receipt.CarriedFleets, len(receipt.MemberEvidence), len(receipt.InstalledFleets)*4)
}

// The reached signing body, not its goroutine launcher, overlaps at width four.
func TestFinalSemanticFixtureFleetSigningStagesOverlap(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	audit := newFinalSemanticFixtureStageAudit()
	jobs, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: t.Context(), entered: audit.enter})
	if err != nil {
		t.Fatal(err)
	}
	got := audit.snapshot()[finalSemanticFixtureFleetAssembly]
	if len(jobs) != 202 || got.entered != 202 || got.completed != 202 || got.active != 0 || got.maximum != 4 {
		t.Fatalf("fleet signing overlap=%+v jobs=%d", got, len(jobs))
	}
}

// A genuine bad private seed fails after stage admission and joins every peer;
// no later wave or partial prepared census may become publishable.
func TestFinalSemanticFixtureFleetSigningFailureJoinsAllOwners(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	const fleet, member = 4, 4
	label := fmt.Sprintf("miner-%d", fleetMemberMinerIndex(source.cfg, fleet, member))
	role, exists := source.roles.Clients[label]
	if !exists || role.Label != label {
		t.Fatal("first-wave fourth owner has no genuine client signer")
	}
	role.SeedHex = "invalid-seed"
	source.roles.Clients[label] = role
	audit := newFinalSemanticFixtureStageAudit()
	jobs, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: t.Context(), entered: audit.enter})
	if err == nil || !strings.Contains(err.Error(), "fleet 4 member 4 seed is invalid") || jobs != nil {
		t.Fatalf("real signing error returned publishable output: jobs=%d error=%v", len(jobs), err)
	}
	got := audit.snapshot()[finalSemanticFixtureFleetAssembly]
	if got.entered != 4 || got.completed != 4 || got.active != 0 {
		t.Fatalf("failed signing did not join all admitted owners: %+v", got)
	}
}

// Cancel only after four real signing bodies have entered. Every entered owner
// returns its exit accounting before cancellation is reported to the caller.
func TestFinalSemanticFixtureFleetSigningCancellationJoinsAllOwners(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stateLock sync.Mutex
	entered, completed := 0, 0
	release := make(chan struct{})
	hook := func(ctx context.Context, stage string, index int) (func(), error) {
		if stage != finalSemanticFixtureFleetAssembly {
			return nil, errors.New("unexpected fleet stage")
		}
		stateLock.Lock()
		entered++
		if entered == 4 {
			cancel()
			close(release)
		}
		stateLock.Unlock()
		<-release
		return func() { stateLock.Lock(); completed++; stateLock.Unlock() }, nil
	}
	jobs, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: ctx, entered: hook})
	if !errors.Is(err, context.Canceled) || jobs != nil {
		t.Fatalf("canceled signing returned output: jobs=%d error=%v", len(jobs), err)
	}
	if entered != 4 || completed != 4 {
		t.Fatalf("canceled signing owners entered=%d completed=%d", entered, completed)
	}
}

// Canonical joins use the same genuine prepared slots and detach every mutable
// field, so completion order and later owner mutation cannot rewrite output.
func TestFinalSemanticFixtureFleetJoinIsCanonicalAndOwned(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	jobs, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	first, err := joinFinalSemanticFixtureFleetJobs(t.Context(), jobs)
	if err != nil {
		t.Fatal(err)
	}
	for a, b := 0, len(jobs)-1; a < b; a, b = a+1, b-1 {
		jobs[a], jobs[b] = jobs[b], jobs[a]
	}
	second, err := joinFinalSemanticFixtureFleetJobs(t.Context(), jobs)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("completion order changed complete fleet join: %v", err)
	}
	target := len(first) - 1
	priorMember := first[target].versions[0].manifest.Members[0]
	priorCanonical := string(first[target].versions[0].canonical)
	priorCommitment := first[target].commitments[0]
	path := fmt.Sprintf("public/fleet-%d.json", jobs[0].fleet)
	priorFile := string(first[target].files[path])
	jobs[0].versions[0].manifest.Members[0].ClientKey[0] ^= 1
	jobs[0].versions[0].canonical[0] ^= 1
	jobs[0].commitments[0].nativeHead.Number++
	jobs[0].files[path][0] ^= 1
	if !reflect.DeepEqual(first, second) || first[target].versions[0].manifest.Members[0] != priorMember || string(first[target].versions[0].canonical) != priorCanonical || first[target].commitments[0] != priorCommitment || string(first[target].files[path]) != priorFile {
		t.Fatal("joined fleet output aliases its prior owner")
	}
}

// Complete genuine slots still fail closed for cross-owner namespace collisions.
func TestFinalSemanticFixtureFleetJoinRejectsConflictingNamespace(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	jobs, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	const oldPath = "public/fleet-1-member-1.binding.json"
	const conflict = "public/fleet-2-member-1.binding.json"
	jobs[0].files[conflict] = jobs[0].files[oldPath]
	delete(jobs[0].files, oldPath)
	if joined, err := joinFinalSemanticFixtureFleetJobs(t.Context(), jobs); err == nil || joined != nil {
		t.Fatal("conflicting fleet namespace was published")
	}
}

// Missing, repeated or canceled complete-census joins never return partial slots.
func TestFinalSemanticFixtureFleetJoinRejectsIncompleteOrCanceledCensus(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	jobs, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	if joined, err := joinFinalSemanticFixtureFleetJobs(t.Context(), jobs[:len(jobs)-1]); err == nil || joined != nil {
		t.Fatal("missing fleet owner was published")
	}
	duplicate := append([]finalSemanticFixtureFleetJob(nil), jobs...)
	duplicate[len(duplicate)-1] = jobs[0]
	if joined, err := joinFinalSemanticFixtureFleetJobs(t.Context(), duplicate); err == nil || joined != nil {
		t.Fatal("duplicate fleet owner was published")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if joined, err := joinFinalSemanticFixtureFleetJobs(ctx, jobs); !errors.Is(err, context.Canceled) || joined != nil {
		t.Fatal("canceled fleet join was published")
	}
}

// A complete but misrouted typed fact cannot hide behind correct file counts.
func TestFinalSemanticFixtureFleetJoinRejectsWrongFactOwner(t *testing.T) {
	t.Parallel()
	source := finalSemanticFixtureFleetTestContext(t)
	jobs, err := prepareFinalSemanticFixtureFleets(source, finalSemanticFixtureWorkControl{ctx: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	jobs[0].bindings[0].actionID = "fleet.bind.2.1"
	if joined, err := joinFinalSemanticFixtureFleetJobs(t.Context(), jobs); err == nil || joined != nil {
		t.Fatal("wrong binding fact owner was published")
	}
}
