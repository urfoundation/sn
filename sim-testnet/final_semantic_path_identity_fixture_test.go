package main

// Fixture-only public authority is declared before any custody mutation. These
// helpers never derive expected authority from a key after it has been changed.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Shared-key controls declare that sharing explicitly for each operator.
func finalSharedPathIdentityTestVector(key string, operators int) []FinalOperatorPathIdentity {
	paths := make([]FinalOperatorPathIdentity, operators)
	for index := range paths {
		paths[index] = FinalOperatorPathIdentity{NoID: uint64(index + 1), PathVPK: key}
	}
	return paths
}

// The complete expected public document contains only test-owned public data.
func finalPathIdentityTestPublicBytes(t *testing.T, deployment string, validators map[uint64][]FinalOperatorPathIdentity) []byte {
	t.Helper()
	public := finalPublicIdentities{
		Schema: "urnetwork-sim-public-identities-v1", DeploymentID: deployment,
		Clients: map[string]finalPublicClientIdentity{}, EVM: map[string]string{}, Substrate: map[string]finalPublicIdentity{},
	}
	for validatorID, paths := range validators {
		for _, path := range paths {
			public.Clients[fmt.Sprintf("validator-%d-no-%d", validatorID, path.NoID)] = finalPublicClientIdentity{
				ClientID: fmt.Sprintf("0x%032x", validatorID*100+path.NoID), ClientKey: path.PathVPK,
			}
		}
	}
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Writes the declared baseline before any test invokes the real consumer.
func writeFinalPathIdentityTestPublic(t *testing.T, root, deployment string, validators map[uint64][]FinalOperatorPathIdentity) {
	t.Helper()
	directory := filepath.Join(root, "public")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "identities.json"), finalPathIdentityTestPublicBytes(t, deployment, validators), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Derives the public values from the fixture's original producer keys only.
func finalPathIdentityTestKeyVector(keys []ed25519.PrivateKey) []FinalOperatorPathIdentity {
	paths := make([]FinalOperatorPathIdentity, len(keys))
	for index, key := range keys {
		paths[index] = FinalOperatorPathIdentity{NoID: uint64(index + 1), PathVPK: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))}
	}
	return paths
}

// Closure-only tests need the complete checked envelope and public identities,
// not fabricated fleet proof validity. Their unrelated envelope files are
// inert owned bytes; only full semantic fixtures execute deeper fleet replay.
func attachFinalPathIdentityTestLineage(t *testing.T, evidence *FinalSemanticEvidence, loaded map[string][]byte) {
	t.Helper()
	public := map[uint64][]FinalOperatorPathIdentity{}
	for _, validator := range evidence.Validators {
		public[validator.ValidatorID] = validator.OperatorPaths
	}
	paths, err := finalFleetLifecycleExpectedPaths(1)
	if err != nil {
		t.Fatal(err)
	}
	lineage := finalFleetLifecycleLineageArtifact{
		Schema: finalFleetLifecycleLineageSchema, DeploymentID: evidence.DeploymentID, PlanHash: evidence.PlanHash, RunID: evidence.RunID,
	}
	for _, path := range paths {
		data := []byte("{}")
		if path == "public/identities.json" {
			data = finalPathIdentityTestPublicBytes(t, evidence.DeploymentID, public)
		}
		lineage.Files = append(lineage.Files, finalFleetLifecycleLineageFile{Path: path, Data: data, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))})
	}
	data, err := json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	locator := FinalArtifactLocator{Kind: "fleet-lifecycle-lineage", URI: "fixture-path-lineage.json", ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	evidence.FleetLifecycle = &FinalFleetLifecycleEvidence{ClientsPerHeadFleet: 1, LineageArtifact: locator}
	loaded[locator.URI] = data
}

// Adds genuine bundle framing around the independent public role document.
func attachFinalPathIdentityTestCollectedBundle(t *testing.T, value *FinalSemanticCollectedInputs, loaded map[string][]byte, public []byte) {
	t.Helper()
	bundle := FinalCollectedFileBundle{
		Schema: finalCollectedFileBundleSchema, Name: "public",
		Files: []FinalCollectedFileBundleEntry{{Path: "identities.json", Data: public, ContentHash: bytesSHA256(public), SizeBytes: uint64(len(public))}},
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	locator := FinalArtifactLocator{Kind: "closed-input-bundle", URI: "fixture-public-path-identities.json", ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	value.ClosedInputBundles = append(value.ClosedInputBundles, locator)
	loaded[locator.URI] = data
}

// Owns genuine M8 cuts produced by the actual distinct-key provisioner fixture.
// The compact replay view is deliberately not a full campaign evidence claim.
type finalOperatorPathReplayTestFixture struct {
	live      *simulatorClientSeedTestFixture
	closure   *validatorpkg.AttemptSettlementClosure
	data      []byte
	identity  validatorpkg.AttemptLedgerIdentity
	authority *finalOperatorPathAuthority
	servers   map[uint64]map[byte]ed25519.PublicKey
	evidence  *FinalSemanticEvidence
	collected *FinalSemanticCollectedInputs
	loaded    map[string][]byte
}

func newFinalOperatorPathReplayTestFixture(t *testing.T) *finalOperatorPathReplayTestFixture {
	t.Helper()
	live := newSimulatorProvisionedOperatorPathFixture(t)
	authority, err := loadFinalOperatorPathAuthority(live.cfg, live.stateRoot, []uint64{1})
	if err != nil {
		t.Fatal(err)
	}
	data, err := validatorpkg.ReadAttemptSettlementClosure(live.root, 42)
	if err != nil {
		t.Fatal(err)
	}
	servers := map[uint64]map[byte]ed25519.PublicKey{}
	for _, operator := range live.terminal.Operators {
		servers[uint64(operator.NoID)] = map[byte]ed25519.PublicKey{1: operator.VerifyKeys[0].PublicKey}
	}
	closure, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(data, servers)
	if err != nil {
		t.Fatal(err)
	}
	identity := closure.Transitions[0].Identity
	identity.NoID = 0
	paths := append([]FinalOperatorPathIdentity(nil), authority.pathsByValidator[1]...)
	proofClosure := FinalCollectedSettlementClosure{Epoch: 42, Boundary: ChainHead{Number: 109, Hash: finalTestHex(42)}, Artifact: FinalArtifactLocator{Kind: "validator-settlement-closure", URI: "operator-closure.json", ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}}
	evidence := &FinalSemanticEvidence{DeploymentID: identity.DeploymentID, ChainID: identity.ChainID, GenesisHash: identity.GenesisHash, Netuid: identity.Netuid, Window: *live.window, Validators: []FinalValidatorIdentityEvidence{{ValidatorID: 1, UID: identity.ValidatorUID, PathVPK: paths[0].PathVPK, OperatorPaths: paths}}}
	collectedValidator := FinalCollectedValidatorInputs{ValidatorID: 1, PathVPK: paths[0].PathVPK, OperatorPaths: append([]FinalOperatorPathIdentity(nil), paths...), SettlementClosures: []FinalCollectedSettlementClosure{proofClosure}}
	loaded := map[string][]byte{proofClosure.Artifact.URI: data}
	for _, transition := range closure.Transitions {
		noID := transition.Identity.NoID
		records := map[uint64]validatorpkg.AttemptRecord{}
		if err := mergeFinalAttemptCut(transition.PreFold.AttemptCut, records); err != nil {
			t.Fatal(err)
		}
		proofData, count, err := finalAcceptedAttemptProofBytes(records, 42, 42)
		if err != nil || count != 1 {
			t.Fatalf("genuine M8 proof projection count=%d: %v", count, err)
		}
		proof := FinalArtifactLocator{Kind: "validator-path-proofs", URI: fmt.Sprintf("operator-%d-proofs.jsonl", noID), ContentHash: bytesSHA256(proofData), SizeBytes: uint64(len(proofData))}
		loaded[proof.URI] = proofData
		evidence.Pools = append(evidence.Pools, FinalPoolUIDEvidence{NoID: noID, ServerKeyHistory: []FinalServerKey{{KeyID: 1, PublicKey: "0x" + hex.EncodeToString(servers[noID][1])}}})
		evidence.PathProofs = append(evidence.PathProofs, FinalValidatorPathProofEvidence{ValidatorID: 1, NoID: noID, FirstEpoch: 42, LastEpoch: 42, ProofCount: count, TrailDepth: live.cfg.Policy.Verify.TrailDepth, Artifact: proof, ProofsHash: proof.ContentHash, SettlementClosures: []FinalCollectedSettlementClosure{proofClosure}})
		collectedValidator.PathProofs = append(collectedValidator.PathProofs, FinalCollectedValidatorPathProof{NoID: noID, FirstEpoch: 42, LastEpoch: 42, ProofCount: count, Artifact: proof})
		runRoot := t.TempDir()
		_, summary, err := persistFinalAttemptRecords(runRoot, 1, int(noID), records)
		if err != nil {
			t.Fatal(err)
		}
		loaded[summary.Artifact.URI], err = os.ReadFile(filepath.Join(runRoot, summary.Artifact.URI))
		if err != nil {
			t.Fatal(err)
		}
		collectedValidator.Attempts = append(collectedValidator.Attempts, summary)
	}
	collected := &FinalSemanticCollectedInputs{Window: *live.window, Validators: []FinalCollectedValidatorInputs{collectedValidator}}
	attachFinalPathIdentityTestLineage(t, evidence, loaded)
	attachFinalPathIdentityTestCollectedBundle(t, collected, loaded, authority.publicBytes)
	return &finalOperatorPathReplayTestFixture{live: live, closure: closure, data: data, identity: identity, authority: authority, servers: servers, evidence: evidence, collected: collected, loaded: loaded}
}

// A small AMin=1 control matches the unchanged durable fixture's policy input;
// it does not alter the release-scale fixture, topology, M8 depth, or any cap.
// Every provider has an explicit binding observation. One newly live binding
// retains a prior head EMA with zero new egress; its exact decay supplies the
// positive channel required by the measurement verifier. This unit transcript
// is not a substitute for the full fixture's independently joined chain data.
func (self *finalOperatorPathReplayTestFixture) measurement(t *testing.T) []byte {
	t.Helper()
	policy := *self.live.cfg.Policy
	policy.Verify.ReliabilityAMin = self.closure.Transitions[0].PreFold.Config.AMin
	hash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	artifact := &validatorpkg.ReleaseMeasurementArtifact{
		Schema: validatorpkg.ReleaseMeasurementSchema, DeploymentID: self.identity.DeploymentID, ChainID: self.identity.ChainID, GenesisHash: self.identity.GenesisHash,
		Coordinator: "0x1111111111111111111111111111111111111111", SettlementVault: "0x2222222222222222222222222222222222222222", ValidatorID: 1, Netuid: self.identity.Netuid,
		SubnetEpoch: 10, NativeSnapshotBlock: 100, NativeSnapshotHash: finalTestHex(100), EVMSnapshotBlock: 109, EVMSnapshotHash: finalTestHex(42), SettlementEpoch: 42,
		Policy: policy, PolicyHash: "0x" + hex.EncodeToString(hash[:]), SelfUID: self.identity.ValidatorUID,
		ControlledNOIDs: []uint64{}, Inputs: []validatorpkg.ReleaseMeasurementInput{}, Bindings: []validatorpkg.ReleaseBindingMeasurement{}, HeadEMA: []validatorpkg.HeadEMAMeasurement{}, Pools: []validatorpkg.ReleasePoolMeasurement{}, DepositAudits: []validatorpkg.DepositAudit{},
	}
	for _, transition := range self.closure.Transitions {
		stats := transition.PreFold
		artifact.Inputs = append(artifact.Inputs, validatorpkg.ReleaseMeasurementInput{NoID: transition.Identity.NoID, SettlementEpoch: 42, CutNativeBlock: 100, CutNativeBlockHash: finalTestHex(100), CutEVMSnapshotBlock: 109, CutEVMSnapshotHash: finalTestHex(42), Stats: stats})
		for _, provider := range stats.Providers {
			artifact.Bindings = append(artifact.Bindings, validatorpkg.ReleaseBindingMeasurement{NoID: transition.Identity.NoID, ClientID: provider.ClientID, FleetID: finalTestHex(0), Hotkey: finalTestHex(0), ClientKey: finalTestHex(0), LocalClientKey: finalTestHex(0), CommitmentHash: finalTestHex(0)})
		}
	}
	sort.Slice(artifact.Bindings, func(i, j int) bool {
		left, right := artifact.Bindings[i], artifact.Bindings[j]
		if left.NoID != right.NoID {
			return left.NoID < right.NoID
		}
		return left.ClientID < right.ClientID
	})
	if len(artifact.Bindings) == 0 {
		t.Fatal("genuine M8 cut has no provider observations")
	}
	binding := &artifact.Bindings[0]
	binding.Active, binding.LiveUIDFound, binding.RecordUID, binding.LiveUID = true, true, 100, 100
	binding.FleetID, binding.Hotkey, binding.ClientKey, binding.LocalClientKey = finalTestHex(0x31), finalTestHex(0x41), finalTestHex(0x51), finalTestHex(0x51)
	binding.CommitmentHash, binding.Generation, binding.ValidFromEpoch, binding.ValidToEpoch = finalTestHex(0x61), 1, 42, 42
	key := validatorpkg.FleetScoreKey{Generation: 1, UID: 100}
	for index := range key.FleetID {
		key.FleetID[index], key.Hotkey[index] = 0x31, 0x41
	}
	alpha := policy.Steering.HeadScoreEMA
	if alpha.Numerator >= alpha.Denominator {
		t.Fatal("fixture requires its configured nontrivial head EMA retention")
	}
	next := new(big.Rat).SetFrac(new(big.Int).SetUint64(alpha.Denominator-alpha.Numerator), new(big.Int).SetUint64(alpha.Denominator))
	artifact.HeadEMA = []validatorpkg.HeadEMAMeasurement{{Key: key, HasRaw: true, Raw: validatorpkg.RationalJSON{Numerator: "0", Denominator: "1"}, HasPrior: true, Prior: validatorpkg.RationalJSON{Numerator: "1", Denominator: "1"}, Next: validatorpkg.RationalJSON{Numerator: next.Num().String(), Denominator: next.Denom().String()}}}
	data, _, _, err := validatorpkg.SealReleaseMeasurementArtifact(artifact)
	if err != nil {
		t.Fatalf("genuine operator measurement control: %v", err)
	}
	return data
}
