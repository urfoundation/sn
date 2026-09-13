package main

// The expected authority comes from pre-existing public provisioner roles.
// Real signed M8 records remain valid controls throughout these mutations.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

// Changes only public test data, keeping the source document available to reset.
func rewriteOperatorPathPublicTestFile(t *testing.T, fixture *simulatorClientSeedTestFixture, mutate func(*finalPublicIdentities)) {
	t.Helper()
	path := filepath.Join(fixture.stateRoot, "public", "identities.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var public finalPublicIdentities
	if err := json.Unmarshal(data, &public); err != nil {
		t.Fatal(err)
	}
	mutate(&public)
	data, err = json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSimulatorOperatorPathAuthorityRejectsMissingOrSwappedPublicLabels(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	publicPath := filepath.Join(fixture.stateRoot, "public", "identities.json")
	original, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing-second", "swapped", "extra", "noncanonical"} {
		rewriteOperatorPathPublicTestFile(t, fixture, func(public *finalPublicIdentities) {
			switch kind {
			case "missing-second":
				delete(public.Clients, "validator-1-no-2")
			case "swapped":
				public.Clients["validator-1-no-1"], public.Clients["validator-1-no-2"] = public.Clients["validator-1-no-2"], public.Clients["validator-1-no-1"]
			case "extra":
				public.Clients["validator-1-no-3"] = public.Clients["validator-1-no-2"]
			case "noncanonical":
				value := public.Clients["validator-1-no-2"]
				value.ClientKey = strings.ToUpper(value.ClientKey)
				public.Clients["validator-1-no-2"] = value
			}
		})
		for _, consumer := range []string{"proofs", "collection", "closures"} {
			requireSimulatorSeedRefusal(t, consumer, fixture.read(t, consumer))
		}
		if err := os.WriteFile(publicPath, original, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, consumer := range []string{"proofs", "collection", "closures"} {
		if err := fixture.read(t, consumer); err != nil {
			t.Fatalf("restored independent public authority %s: %v", consumer, err)
		}
	}
}

func TestSimulatorOperatorPathAuthorityRejectsReplacedKeyAndResignedProof(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	seed := bytes.Repeat([]byte{0x79}, ed25519.SeedSize)
	key := ed25519.NewKeyFromSeed(seed)
	vpk := key.Public().(ed25519.PublicKey)
	data, err := os.ReadFile(fixture.proofPaths[1])
	if err != nil {
		t.Fatal(err)
	}
	var proof validatorpkg.ProofRecord
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	trail := make([]connect.Id, len(proof.Hops))
	for index, hop := range proof.Hops {
		trail[index] = hop.ClientId
	}
	message, err := connect.BuildVerifyFinalMessage(proof.ServerKeyId, proof.TrailId, proof.ServerNonce, vpk, byte(proof.M), proof.Hops)
	if err != nil {
		t.Fatal(err)
	}
	extend, err := connect.BuildVerifyExtendMessage(proof.TrailId, proof.ServerNonce, vpk, byte(proof.M), trail)
	if err != nil {
		t.Fatal(err)
	}
	serverKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize))
	digest, pathID := connect.VerifyFinalDigest(message), validatorpkg.TrailPathId(proof.TrailId, vpk, proof.ServerKeyId)
	proof.Vpk, proof.FinalSig, proof.VpkSig = vpk, ed25519.Sign(serverKey, message), ed25519.Sign(key, message)
	proof.VerifierSig, proof.FinalDigest, proof.PathId = ed25519.Sign(key, extend), digest[:], pathID[:]
	if err := validatorpkg.VerifyProofRecord(&proof, vpk, map[byte]ed25519.PublicKey{1: serverKey.Public().(ed25519.PublicKey)}, fixture.cfg.Policy.Verify.TrailDepth); err != nil {
		t.Fatalf("re-signed alternate control is not cryptographically valid: %v", err)
	}
	data, err = json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.proofPaths[1], append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "operators", "no-2", "client.key"), seed, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, consumer := range []string{"proofs", "collection", "closures"} {
		err := fixture.read(t, consumer)
		requireSimulatorSeedRefusal(t, consumer, err)
		if !strings.Contains(err.Error(), "differs from public provisioner role") {
			t.Fatalf("%s trusted a re-signed replacement instead of public authority: %v", consumer, err)
		}
	}
}

func TestSimulatorOperatorPathAuthorityRejectsForeignDeployment(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	rewriteOperatorPathPublicTestFile(t, fixture, func(public *finalPublicIdentities) { public.DeploymentID += "-foreign" })
	for _, consumer := range []string{"proofs", "collection", "closures"} {
		err := fixture.read(t, consumer)
		requireSimulatorSeedRefusal(t, consumer, err)
		if !strings.Contains(err.Error(), "deployment or schema differs") {
			t.Fatalf("%s did not reject the independent public domain: %v", consumer, err)
		}
	}
}

func TestSimulatorOperatorPathAuthorityChecksEveryKeyBeforeObservation(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	fixture.seedPath = filepath.Join(fixture.root, "operators", "no-2", "client.key")
	fixture.makeUnsafe(t, "leaf-symlink")
	// The existing observer asserts zero observations and zero run output on
	// refusal, so accepting operator one before checking two cannot pass.
	requireSimulatorSeedRefusal(t, "collection", fixture.read(t, "collection"))
}

// Successful independent public admission must not replace the later signed
// closure domain check. Every mutation retains the original real signed bytes.
func TestSimulatorOperatorPathClosureDomainChecksAfterPublicAdmission(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	for _, consumer := range []string{"proofs", "collection", "closures"} {
		if err := fixture.read(t, consumer); err != nil {
			t.Fatalf("original signed authority %s: %v", consumer, err)
		}
	}
	unchanged := preserveSimulatorSeedTestFiles(t, fixture)
	publicPath := filepath.Join(fixture.stateRoot, "public", "identities.json")
	publicBytes, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	deployment, chainID := fixture.cfg.Config.Deployment.DeploymentID, fixture.cfg.ChainID
	genesis, netuid := fixture.cfg.Public.Chain.GenesisHash, fixture.cfg.Netuid
	uid := fixture.terminal.Validators[0].SelfUID
	for _, kind := range []string{"deployment", "chain", "genesis", "netuid", "uid"} {
		switch kind {
		case "deployment":
			rewriteOperatorPathPublicTestFile(t, fixture, func(public *finalPublicIdentities) { public.DeploymentID = deployment + "-foreign" })
			fixture.cfg.Config.Deployment.DeploymentID += "-foreign"
		case "chain":
			fixture.cfg.ChainID++
		case "genesis":
			fixture.cfg.Public.Chain.GenesisHash = finalTestHex(0x79)
		case "netuid":
			fixture.cfg.Netuid++
		case "uid":
			fixture.terminal.Validators[0].SelfUID++
		}
		if _, err := loadFinalOperatorPathAuthority(fixture.cfg, fixture.stateRoot, []uint64{1}); err != nil {
			t.Fatalf("%s public-authority prerequisite refused before the signed-domain check: %v", kind, err)
		}
		if err := fixture.read(t, "closures"); err == nil || !strings.Contains(err.Error(), "signed validator/operator domain differs") {
			t.Fatalf("%s matching public authority bypassed the signed closure domain: %v", kind, err)
		}
		unchanged()
		fixture.cfg.Config.Deployment.DeploymentID, fixture.cfg.ChainID = deployment, chainID
		fixture.cfg.Public.Chain.GenesisHash, fixture.cfg.Netuid = genesis, netuid
		fixture.terminal.Validators[0].SelfUID = uid
		if err := os.WriteFile(publicPath, publicBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := fixture.read(t, "closures"); err != nil {
			t.Fatalf("%s restored original signed closure was rejected: %v", kind, err)
		}
	}
	unchanged()
}

func TestSimulatorOperatorPathIdentityVectorRejectsIncompleteCensus(t *testing.T) {
	good := []FinalOperatorPathIdentity{{NoID: 1, PathVPK: finalTestHex(1)}, {NoID: 2, PathVPK: finalTestHex(2)}}
	if _, err := finalOperatorPathKeys(good[0].PathVPK, good, 2); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"nil", "short", "long", "zero", "duplicate", "reordered", "summary", "malformed"} {
		paths := append([]FinalOperatorPathIdentity(nil), good...)
		summary := good[0].PathVPK
		switch kind {
		case "nil":
			paths = nil
		case "short":
			paths = paths[:1]
		case "long":
			paths = append(paths, FinalOperatorPathIdentity{NoID: 3, PathVPK: finalTestHex(3)})
		case "zero":
			paths[0].NoID = 0
		case "duplicate":
			paths[1].NoID = 1
		case "reordered":
			paths[0], paths[1] = paths[1], paths[0]
		case "summary":
			summary = good[1].PathVPK
		case "malformed":
			paths[1].PathVPK = "0x01"
		}
		if _, err := finalOperatorPathKeys(summary, paths, 2); err == nil {
			t.Errorf("accepted %s operator path vector", kind)
		}
	}
}

func TestSimulatorOperatorPathIdentityUniquenessPreservesSharedOperatorKeys(t *testing.T) {
	public := map[uint64][]FinalOperatorPathIdentity{1: finalSharedPathIdentityTestVector(finalTestHex(1), 2), 2: finalSharedPathIdentityTestVector(finalTestHex(2), 2)}
	if _, err := decodeFinalOperatorPathAuthority(finalPathIdentityTestPublicBytes(t, "path-test", public), "path-test", 2, 2); err != nil {
		t.Fatalf("explicit shared-within-validator authority: %v", err)
	}
	public[2][1].PathVPK = public[1][0].PathVPK
	if _, err := decodeFinalOperatorPathAuthority(finalPathIdentityTestPublicBytes(t, "path-test", public), "path-test", 2, 2); err == nil || !strings.Contains(err.Error(), "reused across validators") {
		t.Fatalf("cross-validator second-operator key reuse: %v", err)
	}
	source := finalSemanticIdentityFixture(t)
	identity := finalSemanticValidatorUIDZeroFixture(t, &source)
	keys := map[string]bool{}
	if err := verifyFinalValidatorIdentity(&source, &identity, map[uint16]bool{}, keys); err != nil || len(keys) != 1 {
		t.Fatalf("shared-key structural control: keys=%d error=%v", len(keys), err)
	}
	identity.OperatorPaths[0].PathVPK = finalTestHex(4)
	identity.PathVPK = finalTestHex(4)
	// A fresh UID map isolates the VPK rule from the earlier duplicate-UID rule.
	if err := verifyFinalValidatorIdentity(&source, &identity, map[uint16]bool{}, keys); err == nil || !strings.Contains(err.Error(), "reused") {
		t.Fatalf("structural replay ignored the already-owned second key: %v", err)
	}
	if len(keys) != 1 {
		t.Fatal("rejected key census partially published its first key")
	}
}

func TestSimulatorOperatorPathCutCollectionAcceptsDistinctKeys(t *testing.T) {
	fixture := newFinalOperatorPathReplayTestFixture(t)
	data := fixture.measurement(t)
	records := map[uint64]map[uint64]validatorpkg.AttemptRecord{1: {}, 2: {}}
	if err := collectFinalAttemptCuts(1, data, fixture.authority.keysByValidator[1], fixture.servers, records); err != nil {
		t.Fatal(err)
	}
	for _, transition := range fixture.closure.Transitions {
		if len(records[transition.Identity.NoID]) != len(transition.PreFold.AttemptCut.Records) {
			t.Fatal("measurement collector did not preserve every operator checkpoint")
		}
	}
	before, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := collectFinalAttemptCuts(1, data, fixture.authority.keysByValidator[1], fixture.servers, records); err != nil {
		t.Fatalf("exact overlapping signed prefix: %v", err)
	}
	after, err := json.Marshal(records)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("exact repeated measurement changed its canonical union")
	}
}

func TestSimulatorOperatorPathCutCollectionRejectsLateKeyWithoutPublication(t *testing.T) {
	fixture := newFinalOperatorPathReplayTestFixture(t)
	data := fixture.measurement(t)
	keys := map[uint64]ed25519.PublicKey{1: fixture.authority.keysByValidator[1][1], 2: fixture.authority.keysByValidator[1][1]}
	records := map[uint64]map[uint64]validatorpkg.AttemptRecord{1: {}, 2: {}}
	if err := collectFinalAttemptCuts(1, data, keys, fixture.servers, records); err == nil || !strings.Contains(err.Error(), "operator 2") {
		t.Fatalf("late wrong operator key refusal: %v", err)
	}
	if len(records[1]) != 0 || len(records[2]) != 0 {
		t.Fatal("late key rejection exposed a partial operator cut")
	}
	conflict := fixture.closure.Transitions[1].PreFold.AttemptCut.Records[0]
	conflict.RecordHash = finalTestHex(0x71)
	records[2][conflict.Sequence] = conflict
	before, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := collectFinalAttemptCuts(1, data, fixture.authority.keysByValidator[1], fixture.servers, records); err == nil || !strings.Contains(err.Error(), "conflicts with prior authority") {
		t.Fatalf("late overlap refusal: %v", err)
	}
	after, err := json.Marshal(records)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("late measurement conflict modified the caller's prior authority")
	}
}

func TestSimulatorOperatorPathClosureRejectsLateConflictWithoutPublication(t *testing.T) {
	fixture := newFinalOperatorPathReplayTestFixture(t)
	for _, kind := range []string{"key", "record"} {
		records := map[uint64]map[uint64]validatorpkg.AttemptRecord{1: {}, 2: {}}
		keys := map[uint64]ed25519.PublicKey{1: fixture.authority.keysByValidator[1][1], 2: fixture.authority.keysByValidator[1][2]}
		if kind == "key" {
			keys[2] = keys[1]
		} else {
			conflict := fixture.closure.Transitions[1].PreFold.AttemptCut.Records[0]
			conflict.RecordHash = finalTestHex(0x73)
			records[2][conflict.Sequence] = conflict
		}
		before, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := collectFinalSettlementClosure(fixture.data, 42, fixture.identity, keys, fixture.servers, records); err == nil {
			t.Fatalf("late closure %s conflict was accepted", kind)
		}
		after, err := json.Marshal(records)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("late closure %s rejection exposed partial authority", kind)
		}
	}
	if _, err := collectFinalSettlementClosure(fixture.data, 42, fixture.identity, fixture.authority.keysByValidator[1], fixture.servers, map[uint64]map[uint64]validatorpkg.AttemptRecord{1: {}, 2: {}}); err != nil {
		t.Fatalf("genuine closure control after refusals: %v", err)
	}
}

func TestSimulatorOperatorPathClosedGraphBindsPublicAuthority(t *testing.T) {
	fixture := newFinalOperatorPathReplayTestFixture(t)
	if err := verifyFinalCollectedSettlementAuthority(fixture.live.cfg, fixture.collected, fixture.live.terminal, fixture.loaded); err != nil {
		t.Fatalf("genuine distinct-key closed graph: %v", err)
	}
	replaceOperatorPathReplayTestAuthority(t, fixture)
	if err := verifyFinalCollectedSettlementAuthority(fixture.live.cfg, fixture.collected, fixture.live.terminal, fixture.loaded); err == nil || !strings.Contains(err.Error(), "public provisioner role") {
		t.Fatalf("collected unsigned operator key was trusted: %v", err)
	}
}

func TestSimulatorOperatorPathFinalReplayBindsPublicAuthority(t *testing.T) {
	fixture := newFinalOperatorPathReplayTestFixture(t)
	if err := verifyFinalSettlementClosureArtifacts(fixture.evidence, fixture.loaded); err != nil {
		t.Fatalf("genuine distinct-key final closure replay: %v", err)
	}
	baselineLocator := fixture.evidence.FleetLifecycle.LineageArtifact
	baselineBytes := append([]byte(nil), fixture.loaded[baselineLocator.URI]...)
	for _, kind := range []string{"schema", "deployment", "plan", "run", "census", "order", "file-size", "file-hash"} {
		var lineage finalFleetLifecycleLineageArtifact
		if err := json.Unmarshal(baselineBytes, &lineage); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "schema":
			lineage.Schema += "-foreign"
		case "deployment":
			lineage.DeploymentID += "-foreign"
		case "plan":
			lineage.PlanHash = finalTestHex(0x75)
		case "run":
			lineage.RunID += "foreign"
		case "census":
			lineage.Files = lineage.Files[:len(lineage.Files)-1]
		case "order":
			lineage.Files[0], lineage.Files[1] = lineage.Files[1], lineage.Files[0]
		case "file-size":
			lineage.Files[0].SizeBytes++
		case "file-hash":
			lineage.Files[0].ContentHash = bytesSHA256([]byte("foreign"))
		}
		data, err := json.Marshal(lineage)
		if err != nil {
			t.Fatal(err)
		}
		fixture.loaded[baselineLocator.URI] = data
		fixture.evidence.FleetLifecycle.LineageArtifact.ContentHash = bytesSHA256(data)
		fixture.evidence.FleetLifecycle.LineageArtifact.SizeBytes = uint64(len(data))
		if err := verifyFinalSettlementClosureArtifacts(fixture.evidence, fixture.loaded); err == nil || !strings.Contains(err.Error(), "fleet lifecycle lineage") {
			t.Fatalf("rehashing %s bypassed full lineage admission: %v", kind, err)
		}
	}
	for _, test := range []struct{ path, want string }{
		{path: "launch-foundation/plan.json", want: "path authority plan differs"},
		{path: "launch-foundation/journal.jsonl", want: "captured journal line 1 failed hash-chain validation"},
	} {
		var lineage finalFleetLifecycleLineageArtifact
		if err := json.Unmarshal(baselineBytes, &lineage); err != nil {
			t.Fatal(err)
		}
		for index := range lineage.Files {
			file := &lineage.Files[index]
			if file.Path == test.path {
				file.Data = []byte("{}")
				file.ContentHash, file.SizeBytes = bytesSHA256(file.Data), uint64(len(file.Data))
			}
		}
		data, err := json.Marshal(lineage)
		if err != nil {
			t.Fatal(err)
		}
		fixture.loaded[baselineLocator.URI] = data
		fixture.evidence.FleetLifecycle.LineageArtifact.ContentHash = bytesSHA256(data)
		fixture.evidence.FleetLifecycle.LineageArtifact.SizeBytes = uint64(len(data))
		if err := verifyFinalSettlementClosureArtifacts(fixture.evidence, fixture.loaded); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("rehashing incomplete %s bypassed authenticated setup history: %v", test.path, err)
		}
	}
	fixture.loaded[baselineLocator.URI] = baselineBytes
	fixture.evidence.FleetLifecycle.LineageArtifact = baselineLocator
	replaceOperatorPathReplayTestAuthority(t, fixture)
	if err := verifyFinalSettlementClosureArtifacts(fixture.evidence, fixture.loaded); err == nil || !strings.Contains(err.Error(), "public provisioner role") {
		t.Fatalf("final unsigned operator key was trusted: %v", err)
	}
	fixture.evidence.Validators[0].OperatorPaths = append([]FinalOperatorPathIdentity(nil), fixture.authority.pathsByValidator[1]...)
	locator := fixture.evidence.FleetLifecycle.LineageArtifact
	fixture.loaded[locator.URI] = append(fixture.loaded[locator.URI], ' ')
	if err := verifyFinalSettlementClosureArtifacts(fixture.evidence, fixture.loaded); err == nil || !strings.Contains(err.Error(), "lineage content differs") {
		t.Fatalf("standalone replay bypassed its exact public-lineage locator: %v", err)
	}
}

// Produces an entire alternative operator-two history through the same real
// append/settlement APIs. Re-signing all cuts, proofs and the batch plus all
// unsigned projections still cannot replace the separately retained public map.
func replaceOperatorPathReplayTestAuthority(t *testing.T, fixture *finalOperatorPathReplayTestFixture) {
	t.Helper()
	alternate := newSimulatorClientSeedTestFixtureWithOperatorSeeds(t, fixture.live.cfg, map[uint64][]byte{1: fixture.live.seed, 2: bytes.Repeat([]byte{0x79}, ed25519.SeedSize)})
	data, err := validatorpkg.ReadAttemptSettlementClosure(alternate.root, 42)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(data, fixture.servers)
	if err != nil {
		t.Fatalf("alternate history is not fully signed: %v", err)
	}
	paths := make([]FinalOperatorPathIdentity, len(closure.Transitions))
	for index, transition := range closure.Transitions {
		paths[index] = FinalOperatorPathIdentity{NoID: transition.Identity.NoID, PathVPK: transition.Identity.ValidatorVPK}
		records := map[uint64]validatorpkg.AttemptRecord{}
		if err := mergeFinalAttemptCut(transition.PreFold.AttemptCut, records); err != nil {
			t.Fatal(err)
		}
		_, summary, err := persistFinalAttemptRecords(alternate.runRoot, 1, int(transition.Identity.NoID), records)
		if err != nil {
			t.Fatal(err)
		}
		attemptData, err := os.ReadFile(filepath.Join(alternate.runRoot, summary.Artifact.URI))
		if err != nil {
			t.Fatal(err)
		}
		fixture.collected.Validators[0].Attempts[index] = summary
		fixture.loaded[summary.Artifact.URI] = attemptData
		proofData, count, err := finalAcceptedAttemptProofBytes(records, 42, 42)
		if err != nil || count != 1 {
			t.Fatalf("alternate proof projection: count=%d error=%v", count, err)
		}
		proof := &fixture.evidence.PathProofs[index]
		fixture.loaded[proof.Artifact.URI] = proofData
		proof.Artifact.SizeBytes, proof.Artifact.ContentHash = uint64(len(proofData)), bytesSHA256(proofData)
		proof.ProofsHash = proof.Artifact.ContentHash
		fixture.collected.Validators[0].PathProofs[index].Artifact = proof.Artifact
		locator := &proof.SettlementClosures[0].Artifact
		fixture.loaded[locator.URI] = data
		locator.ContentHash, locator.SizeBytes = bytesSHA256(data), uint64(len(data))
	}
	fixture.collected.Validators[0].SettlementClosures = append([]FinalCollectedSettlementClosure(nil), fixture.evidence.PathProofs[0].SettlementClosures...)
	fixture.evidence.Validators[0].OperatorPaths = paths
	fixture.collected.Validators[0].OperatorPaths = append([]FinalOperatorPathIdentity(nil), paths...)
	// The complete alternative projection is valid under its claimed keys.
	for index, proof := range fixture.evidence.PathProofs {
		if err := verifyFinalPathProofArtifactBound(&proof, fixture.loaded[proof.Artifact.URI], &fixture.evidence.Validators[0], &fixture.evidence.Pools[index], map[string]bool{}, map[string]bool{}); err != nil {
			t.Fatalf("alternate signed proof is not a positive crypto control: %v", err)
		}
	}
}

func TestSimulatorOperatorPathIdentityRejectsConflictingPublicCopies(t *testing.T) {
	value, loaded := &FinalSemanticCollectedInputs{}, map[string][]byte{}
	paths := map[uint64][]FinalOperatorPathIdentity{1: finalSharedPathIdentityTestVector(finalTestHex(1), 2)}
	public := finalPathIdentityTestPublicBytes(t, "path-test", paths)
	attachFinalPathIdentityTestCollectedBundle(t, value, loaded, public)
	first := value.ClosedInputBundles[0]
	value.ClosedInputBundles = append(value.ClosedInputBundles, first)
	if got, err := finalCollectedPublicIdentityBytes(value, loaded); err != nil || !bytes.Equal(got, public) {
		t.Fatalf("identical duplicate public locator: %v", err)
	}
	value.ClosedInputBundles = value.ClosedInputBundles[:1]
	paths[1][1].PathVPK = finalTestHex(2)
	other, otherLoaded := &FinalSemanticCollectedInputs{}, map[string][]byte{}
	attachFinalPathIdentityTestCollectedBundle(t, other, otherLoaded, finalPathIdentityTestPublicBytes(t, "path-test", paths))
	locator := other.ClosedInputBundles[0]
	data := otherLoaded[locator.URI]
	locator.URI = "second-public-authority.json"
	loaded[locator.URI] = data
	value.ClosedInputBundles = append(value.ClosedInputBundles, locator)
	if _, err := finalCollectedPublicIdentityBytes(value, loaded); err == nil || !strings.Contains(err.Error(), "conflicting public identity copies") {
		t.Fatalf("second public authority was accepted: %v", err)
	}
	value.ClosedInputBundles[1] = first
	value.ClosedInputBundles[1].SizeBytes++
	if _, err := finalCollectedPublicIdentityBytes(value, loaded); err == nil {
		t.Fatal("same URI with a conflicting public locator was accepted")
	}
	for _, kind := range []string{"size", "hash", "kind"} {
		changed := first
		switch kind {
		case "size":
			changed.SizeBytes++
		case "hash":
			changed.ContentHash = bytesSHA256([]byte("foreign"))
		case "kind":
			changed.Kind = "foreign-kind"
		}
		loads := 0
		_, err := loadFinalSemanticArtifactUses(context.Background(), []finalSemanticArtifactUse{{locator: first}, {locator: changed}}, func(context.Context, FinalArtifactLocator) ([]byte, error) {
			loads++
			return loaded[first.URI], nil
		})
		want := "size or content hash mismatch"
		if kind == "kind" {
			want = "conflicting locator declarations"
		}
		if err == nil || !strings.Contains(err.Error(), want) || loads != 1 {
			t.Fatalf("%s duplicate locator bypassed checked single-load reuse: loads=%d error=%v", kind, loads, err)
		}
	}
}

func TestSimulatorOperatorPathSourceBuilderBindsEveryRoleLabel(t *testing.T) {
	fixture := newFinalOperatorPathReplayTestFixture(t)
	archive := &finalSemanticArchive{files: map[string][]byte{"public/identities.json": fixture.authority.publicBytes}, collected: fixture.collected}
	source := &FinalSemanticEvidence{DeploymentID: fixture.identity.DeploymentID, ExpectedValidators: 1, ExpectedOperators: 2}
	// Passing admission reaches the next real invariant; the unit fixture does
	// not fabricate the unrelated native-cycle graph to claim full construction.
	if err := archive.buildValidators(source, fixture.authority.identities, &FinalCollectedChainSnapshot{}, &finalSemanticEventIndex{}); err == nil || !strings.Contains(err.Error(), "terminal native identity is incomplete") {
		t.Fatalf("valid authority did not reach native construction: %v", err)
	}
	for _, kind := range []string{"missing", "wrong-second", "summary"} {
		paths := append([]FinalOperatorPathIdentity(nil), fixture.authority.pathsByValidator[1]...)
		fixture.collected.Validators[0].PathVPK = paths[0].PathVPK
		switch kind {
		case "missing":
			paths = paths[:1]
		case "wrong-second":
			paths[1].PathVPK = finalTestHex(0x73)
		case "summary":
			fixture.collected.Validators[0].PathVPK = paths[1].PathVPK
		}
		fixture.collected.Validators[0].OperatorPaths = paths
		err := archive.buildValidators(source, fixture.authority.identities, &FinalCollectedChainSnapshot{}, &finalSemanticEventIndex{})
		if err == nil || strings.Contains(err.Error(), "terminal native identity") || len(source.Validators) != 0 {
			t.Fatalf("%s source authority reached or published native construction: %v", kind, err)
		}
	}
}

func TestSimulatorOperatorPathProofVerifierRoutesByNoID(t *testing.T) {
	fixture := newFinalOperatorPathReplayTestFixture(t)
	identity := fixture.evidence.Validators[0]
	for index, proof := range fixture.evidence.PathProofs {
		pool := fixture.evidence.Pools[index]
		if err := verifyFinalPathProofArtifactBound(&proof, fixture.loaded[proof.Artifact.URI], &identity, &pool, map[string]bool{}, map[string]bool{}); err != nil {
			t.Fatalf("operator %d genuine M8 proof: %v", proof.NoID, err)
		}
	}
	proof, pool := fixture.evidence.PathProofs[1], fixture.evidence.Pools[1]
	identity.OperatorPaths = finalSharedPathIdentityTestVector(identity.PathVPK, 2)
	if err := verifyFinalPathProofArtifactBound(&proof, fixture.loaded[proof.Artifact.URI], &identity, &pool, map[string]bool{}, map[string]bool{}); err == nil {
		t.Fatal("operator two proof was authenticated with operator one's key")
	}
	identity = fixture.evidence.Validators[0]
	pool.NoID = 1
	if err := verifyFinalPathProofArtifactBound(&proof, fixture.loaded[proof.Artifact.URI], &identity, &pool, map[string]bool{}, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "identity is unavailable") {
		t.Fatalf("wrong operator proof locator routing: %v", err)
	}
}

func TestSimulatorOperatorPathCollectedSchemaRequiresExplicitVector(t *testing.T) {
	cfg := testResolvedConfig(t)
	window := ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", FirstEpoch: 42, EpochCount: uint64(cfg.Config.Scenarios.ShortEpochs), StartBlock: 100, EpochBlocks: 10}
	value := finalSemanticBuilderCollectedManifest(cfg, "operator-path-schema", finalTestHex(1), window)
	var err error
	value.EvidenceHash, err = finalSemanticCollectedInputsHash(value)
	if err != nil {
		t.Fatal(err)
	}
	if finalSemanticCollectedInputsSchema != "urnetwork-final-semantic-collected-inputs-v4" {
		t.Fatal("operator authority changed without its explicit collected schema")
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, value); err != nil {
		t.Fatalf("new canonical collected schema control: %v", err)
	}
	value.Schema = "urnetwork-final-semantic-collected-inputs-v3"
	if err := verifyFinalSemanticCollectedInputs(cfg, value); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("old collected schema was silently promoted: %v", err)
	}
	value.Schema = finalSemanticCollectedInputsSchema
	value.Validators[1].OperatorPaths = nil
	if err := verifyFinalSemanticCollectedInputs(cfg, value); err == nil || !strings.Contains(err.Error(), "census is incomplete") {
		t.Fatalf("new collected schema inferred missing operator keys: %v", err)
	}
}

func TestSimulatorOperatorPathFinalSchemaRequiresExplicitVector(t *testing.T) {
	if finalSemanticEvidenceSchema != "urnetwork-final-semantic-evidence-v10" {
		t.Fatalf("unexpected operator authority schema %q", finalSemanticEvidenceSchema)
	}
	if err := VerifyFinalSemanticEvidence(&FinalSemanticEvidence{Schema: "urnetwork-final-semantic-evidence-v9"}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("old semantic schema was silently promoted: %v", err)
	}
	source := finalSemanticIdentityFixture(t)
	identity := finalSemanticValidatorUIDZeroFixture(t, &source)
	if err := verifyFinalValidatorIdentity(&source, &identity, map[uint16]bool{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	identity.OperatorPaths = nil
	if err := verifyFinalValidatorIdentity(&source, &identity, map[uint16]bool{}, map[string]bool{}); err == nil {
		t.Fatal("final identity inferred an operator vector from PathVPK")
	}
	data, err := json.Marshal(FinalValidatorIdentityEvidence{ValidatorID: 1, PathVPK: finalTestHex(1), OperatorPaths: finalSharedPathIdentityTestVector(finalTestHex(1), 2)})
	if err != nil || !bytes.Contains(data, []byte(`"operator_paths"`)) || !bytes.Contains(data, []byte(fmt.Sprintf(`"no_id":%d`, 2))) {
		t.Fatalf("canonical wire omitted the second explicit operator: %v", err)
	}
}
