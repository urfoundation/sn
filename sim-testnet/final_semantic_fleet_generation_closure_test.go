package main

// Ordinary fleet lineage must retain every source consumed through a shared
// archive helper, including the two challenger-only native registrations.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Both challengers exercise the shared native receipt helper. Replay uses only
// the sealed source map, so an input left in the outer archive cannot hide.
func TestFinalFleetGenerationSourceRetainsBothChallengerPostconditionsForReplay(t *testing.T) {
	archive, evidence, chain, events, _, priorPath := finalFleetGenerationSourceNamespaceFixture(t)
	prior, err := decodePersistedPlanBytes(archive.files[priorPath])
	if err != nil {
		t.Fatal(err)
	}
	entries, err := decodeFinalSemanticJournalBytes(archive.files["launch-foundation/journal.jsonl"])
	if err != nil {
		t.Fatal(err)
	}
	appendEntry := func(entry JournalEntry) {
		entry.Schema, entry.Sequence = "urnetwork-sim-journal-v1", uint64(len(entries)+1)
		entry.Time = time.Unix(1_700_000_000+int64(entry.Sequence), 0).UTC().Format(time.RFC3339Nano)
		entry.PreviousHash = entries[len(entries)-1].EntryHash
		entry.EntryHash = ""
		var err error
		entry.EntryHash, err = canonicalHashHex(entry)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	paths := map[uint64]string{}
	for fleetID := uint64(201); fleetID <= 202; fleetID++ {
		action, err := exactPlanActionByID(prior, fmt.Sprintf("fleet.register.%d", fleetID))
		if err != nil {
			t.Fatal(err)
		}
		coldkey, _ := finalNativeTestAccount(t, byte(fleetID))
		_, hotkey := finalNativeTestAccount(t, byte(fleetID+1))
		head := testEVMHead(500+fleetID, byte(fleetID))
		observed := map[string]any{"role": fleetHotkeyLabel(int(fleetID)), "coldkey": coldkey.Address(), "hotkey": hotkey, "uid": fleetID}
		postcondition := &ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
			OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false,
			SubstrateFinalized: head, EVMFinalized: head, IndependentSubstrateFinalized: head, IndependentEVMFinalized: head,
			EVMHashDomain: "evm-rpc", IndependentEVMHashDomain: "evm-rpc", Observed: observed, IndependentObserved: observed,
		}
		data, err := json.Marshal(postcondition)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeFinalActionPostconditionV4(data); err != nil {
			t.Fatal(err)
		}
		path, err := postconditionRelativePath(prior.PlanHash, action.ID)
		if err != nil {
			t.Fatal(err)
		}
		paths[fleetID], archive.files[path] = path, data
		postHash, err := canonicalHashHex(postcondition)
		if err != nil {
			t.Fatal(err)
		}
		transactionHash := finalTestHex(byte(fleetID + 2))
		appendEntry(JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, Signer: coldkey.Address(), Nonce: "1", TransactionHash: transactionHash, RecoveryBlock: 500, RecoveryBlockHash: finalTestHex(0x10)})
		appendEntry(JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: transactionHash, BlockNumber: head.Number, BlockHash: head.Hash})
		appendEntry(JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: postHash})
		evidence.HeadTransitions = append(evidence.HeadTransitions, FinalHeadTournamentTransition{ChallengerFleetID: fleetID, Artifact: finalFleetGenerationTestArtifact("head-tournament-transition", fmt.Sprintf("transition-%d", fleetID))})
	}
	archive.files["launch-foundation/journal.jsonl"] = finalSemanticFixtureJournalBytes(t, entries)
	derived := map[string][]byte{}
	archive.artifactDeriver = func(kind, name string, data []byte) (FinalArtifactLocator, error) {
		derived[name] = append([]byte(nil), data...)
		return FinalArtifactLocator{Kind: kind, URI: name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}, nil
	}
	source, err := newFinalFleetGenerationSource(archive, evidence, chain, events)
	if err != nil {
		t.Fatal(err)
	}
	receipts := map[uint64]FinalNativeReceipt{}
	for fleetID := uint64(201); fleetID <= 202; fleetID++ {
		registration, _, err := source.challenger(fleetID)
		if err != nil {
			t.Fatalf("challenger %d source: %v", fleetID, err)
		}
		receipts[fleetID] = registration
		if !bytes.Equal(source.raw[paths[fleetID]], archive.files[paths[fleetID]]) {
			t.Fatalf("challenger %d canonical postcondition was consumed without being sealed", fleetID)
		}
	}
	sealedFiles := map[string][]byte{}
	for name, data := range source.raw {
		sealedFiles[name] = append([]byte(nil), data...)
	}
	sealed := &finalSemanticArchive{files: sealedFiles, artifactDeriver: finalFleetGenerationArtifactDeriver(derived)}
	replayed, err := newFinalFleetGenerationSource(sealed, evidence, chain, events)
	if err != nil {
		t.Fatal(err)
	}
	for fleetID := uint64(201); fleetID <= 202; fleetID++ {
		registration, _, err := replayed.challenger(fleetID)
		if err != nil || !finalJSONEqual(registration, receipts[fleetID]) {
			t.Fatalf("challenger %d could not replay from sealed inputs: %v", fleetID, err)
		}
		original := sealedFiles[paths[fleetID]]
		delete(sealedFiles, paths[fleetID])
		if _, _, err := replayed.challenger(fleetID); err == nil {
			t.Errorf("challenger %d replay accepted missing canonical postcondition", fleetID)
		}
		sealedFiles[paths[fleetID]] = append([]byte(nil), original...)
		sealedFiles[paths[fleetID]][len(original)/2] ^= 1
		if _, _, err := replayed.challenger(fleetID); err == nil {
			t.Errorf("challenger %d replay accepted substituted canonical postcondition", fleetID)
		}
		sealedFiles[paths[fleetID]] = original
	}
}
