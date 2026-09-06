package main

// Neutral work counts exercise the real complete sealed fleet source. These
// tests never substitute a signature, journal verdict or reduced population;
// only actual loop visits and authentication entries are observed.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// A detached complete graph supplies genuine manifests, dual signatures,
// journal lineage and the exact expected ABI bytes for the selected batch.
func finalGenerationWorkFixture(t *testing.T, edit func(map[string][]byte)) (*finalFleetGenerationSource, FinalFleetGenerationBatchEvidence) {
	t.Helper()
	evidence, artifacts := finalSemanticFixture(t)
	if evidence.ExpectedMiners != 1000 || evidence.ExpectedCandidates != 202 || evidence.ExpectedHeadSlots != 200 || evidence.FleetGeneration == nil || len(evidence.FleetGeneration.SetupFleets) != 200 || len(evidence.FleetGeneration.Batches) != 40 {
		t.Fatal("generation work fixture lost the complete release census")
	}
	files, err := finalFleetGenerationArtifactFiles(&evidence, artifacts[evidence.FleetGeneration.Artifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	var selected FinalFleetGenerationBatchEvidence
	for _, batch := range evidence.FleetGeneration.Batches {
		if batch.Generation == 1 && batch.BatchWrite != nil && len(batch.InstalledFleets) != 0 {
			selected = batch
			break
		}
	}
	if selected.BatchWrite == nil {
		t.Fatal("complete generation has no installed batch")
	}
	if edit != nil {
		edit(files)
	}
	derived := &finalSemanticFixtureArtifacts{}
	archive := &finalSemanticArchive{ctx: t.Context(), files: files, collected: &FinalSemanticCollectedInputs{Policy: evidence.PolicyArtifact}, artifactDeriver: derived.derive}
	chain := &FinalCollectedChainSnapshot{FleetBatcher: selected.BatchWrite.BatcherAddress}
	events := &finalSemanticEventIndex{byName: map[string][]finalSemanticEvent{}, byTx: map[string][]finalCanonicalEVMLog{}}
	source, err := newFinalFleetGenerationSource(archive, &evidence, chain, events)
	if err != nil {
		t.Fatal(err)
	}
	return source, selected
}

// Exact output bytes come from the independently constructed, fully verified
// base graph, not from a second call to the candidate method.
func finalGenerationWorkInstallBytes(t *testing.T, source *finalFleetGenerationSource, batch FinalFleetGenerationBatchEvidence) []byte {
	t.Helper()
	raw, err := source.installCalldata(batch, batch.InstalledFleets)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hex.DecodeString(strings.TrimPrefix(batch.BatchWrite.Calldata, "0x"))
	if err != nil || !bytes.Equal(raw, expected) || !strings.EqualFold(crypto.Keccak256Hash(raw).Hex(), batch.CalldataHash) {
		t.Fatalf("complete installed calldata changed from sealed source: %v", err)
	}
	return raw
}

// Selects the exact real refresh mutation and all expected transaction fields.
func finalGenerationWorkRefreshMutation(t *testing.T, source *finalFleetGenerationSource) (string, FleetRefreshBatchEvidence) {
	t.Helper()
	const actionID = "fleet.refresh.batch.1"
	var receipt FleetRefreshBatchEvidence
	if err := decodeStrictJSONBytes(source.archive.files["public/fleet-refresh-batch-1.json"], &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.TransactionHash == "" || receipt.BlockNumber == 0 || receipt.BlockHash == "" {
		t.Fatal("real refresh receipt is incomplete")
	}
	return actionID, receipt
}

// Each exact installed member currently passes both signature primitives once
// in version construction and again in its immediate calldata reconstruction.
func TestFinalFleetGenerationAuthenticatesInitialMemberOncePerSourceUse(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	// Member coordinates distinguish every genuine signature pair in the batch.
	type memberKey struct{ fleet, member uint64 }
	counts := map[memberKey]int{}
	source.workObserver = func(work finalFleetGenerationWorkObservation) {
		if work.stage == "initial-member-authentication" {
			counts[memberKey{fleet: work.fleetID, member: work.member}]++
		}
	}
	_ = finalGenerationWorkInstallBytes(t, source, batch)
	want := len(batch.InstalledFleets) * int(finalFleetGenerationMembersPerFleet)
	if len(counts) != want {
		t.Fatalf("member authentication census=%d, want all %d", len(counts), want)
	}
	total := 0
	for _, count := range counts {
		total += count
	}
	if total != want {
		t.Fatalf("initial member authentication work=%d, want exactly %d complete member checks within one source use", total, want)
	}
}

// A single exact mutation must inspect its full matching candidate sets, not
// repeatedly traverse unrelated journal history for each member in the batch.
func TestFinalFleetGenerationInspectsOnlyExactJournalCandidates(t *testing.T) {
	t.Parallel()
	source, _ := finalGenerationWorkFixture(t, nil)
	actionID, receipt := finalGenerationWorkRefreshMutation(t, source)
	wantVerified, wantFinalized := 0, 0
	for _, entry := range source.entries {
		if entry.ActionID != actionID {
			continue
		}
		if entry.Stage == StageVerified {
			wantVerified++
		}
		if entry.Stage == StageFinalized {
			wantFinalized++
		}
	}
	if wantVerified == 0 || wantFinalized == 0 {
		t.Fatal("exact mutation has no complete real journal candidate sets")
	}
	verified, finalized := 0, 0
	source.workObserver = func(work finalFleetGenerationWorkObservation) {
		if work.actionID != actionID {
			t.Errorf("journal observation changed action identity: %s", work.actionID)
		}
		switch work.stage {
		case "verified-journal-candidate":
			verified++
		case "finalized-journal-candidate":
			finalized++
		}
	}
	_, entry, post, raw, err := source.verifiedMutation(actionID, receipt.TransactionHash, receipt.BlockNumber, receipt.BlockHash, nil)
	if err != nil || entry.TransactionHash != receipt.TransactionHash || post == nil || len(raw) == 0 {
		t.Fatalf("real exact mutation failed before work assertion: %v", err)
	}
	if verified != wantVerified || finalized != wantFinalized {
		t.Fatalf("journal candidate work verified=%d finalized=%d, want complete exact sets %d/%d", verified, finalized, wantVerified, wantFinalized)
	}
}

// Nil observation leaves the exact genuine installed ABI bytes untouched.
func TestFinalFleetGenerationWorkObservationPreservesSealedCalldata(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	_ = finalGenerationWorkInstallBytes(t, source, batch)
	if len(source.versions) != len(batch.InstalledFleets) {
		t.Fatal("installed source omitted a complete signed generation")
	}
}

// A new source must independently authenticate every member; observation and
// ownership cannot leak a successful check between two separately opened joins.
func TestFinalFleetGenerationWorkDoesNotReuseAcrossSources(t *testing.T) {
	t.Parallel()
	for index := 0; index < 2; index++ {
		source, batch := finalGenerationWorkFixture(t, nil)
		seen := map[string]bool{}
		source.workObserver = func(work finalFleetGenerationWorkObservation) {
			if work.stage == "initial-member-authentication" {
				seen[fmt.Sprintf("%d/%d", work.fleetID, work.member)] = true
			}
		}
		_ = finalGenerationWorkInstallBytes(t, source, batch)
		if len(seen) != len(batch.InstalledFleets)*int(finalFleetGenerationMembersPerFleet) {
			t.Fatalf("source %d reused another source's authentication", index)
		}
	}
}

// Warm generation caches still compare exact owned archive bytes before
// reconstructing a subsequent install. A changed source cannot reuse old input.
func TestFinalFleetGenerationWorkRejectsChangedMemberAfterWarmVersion(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	_ = finalGenerationWorkInstallBytes(t, source, batch)
	path := fmt.Sprintf("public/fleet-%d-member-1.binding.json", batch.InstalledFleets[0])
	prior := bytes.Clone(source.archive.files[path])
	var binding FleetBindingEvidence
	if err := decodeStrictJSONBytes(prior, &binding); err != nil {
		t.Fatal(err)
	}
	binding.ClientSignature = "0x" + strings.Repeat("00", 64)
	changed, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	source.archive.files[path] = changed
	if _, err := source.installCalldata(batch, batch.InstalledFleets); err == nil || !strings.Contains(err.Error(), "changed while being indexed") {
		t.Fatalf("changed warm source member was reused: %v", err)
	}
	source.archive.files[path] = prior
	_ = finalGenerationWorkInstallBytes(t, source, batch)
}

// Predicate calls remain outside all retained verdicts. Returning false on a
// later use must still refuse the exact otherwise-valid signed mutation.
func TestFinalFleetGenerationMutationPredicateRunsOnEveryUse(t *testing.T) {
	t.Parallel()
	source, _ := finalGenerationWorkFixture(t, nil)
	actionID, receipt := finalGenerationWorkRefreshMutation(t, source)
	for _, accept := range []bool{true, false, true} {
		calls := 0
		_, _, _, _, err := source.verifiedMutation(actionID, receipt.TransactionHash, receipt.BlockNumber, receipt.BlockHash, func(post *ActionPostcondition) bool { calls++; return accept })
		if calls != 1 || (err == nil) != accept {
			t.Fatalf("predicate accept=%t calls=%d error=%v", accept, calls, err)
		}
	}
}

// Appends a real existing journal fact with a new valid hash-chain position.
// Duplicate checks must remain visible after candidate indexing is introduced.
func appendFinalGenerationWorkDuplicate(t *testing.T, files map[string][]byte, stage string) {
	t.Helper()
	entries, err := decodeFinalSemanticJournalBytes(files["launch-foundation/journal.jsonl"])
	if err != nil {
		t.Fatal(err)
	}
	var duplicate JournalEntry
	for _, entry := range entries {
		if entry.ActionID == "fleet.refresh.batch.1" && string(entry.Stage) == stage {
			duplicate = entry
			break
		}
	}
	if duplicate.ActionID == "" {
		t.Fatal("real journal duplicate source is absent")
	}
	duplicate.Sequence = uint64(len(entries) + 1)
	duplicate.Time = entries[len(entries)-1].Time
	duplicate.PreviousHash = entries[len(entries)-1].EntryHash
	duplicate.EntryHash = ""
	duplicate.EntryHash, err = canonicalHashHex(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	files["launch-foundation/journal.jsonl"] = append(bytes.Clone(files["launch-foundation/journal.jsonl"]), append(raw, '\n')...)
}

// An exact duplicate finalized transaction is not a harmless deduplication.
func TestFinalFleetGenerationWorkRejectsDuplicateFinalizedCandidate(t *testing.T) {
	t.Parallel()
	source, _ := finalGenerationWorkFixture(t, func(files map[string][]byte) { appendFinalGenerationWorkDuplicate(t, files, string(StageFinalized)) })
	actionID, receipt := finalGenerationWorkRefreshMutation(t, source)
	if _, _, _, _, err := source.verifiedMutation(actionID, receipt.TransactionHash, receipt.BlockNumber, receipt.BlockHash, nil); err == nil || !strings.Contains(err.Error(), "duplicate exact finalization") {
		t.Fatalf("duplicate finalized candidate was hidden: %v", err)
	}
}

// Duplicate verified authority must not become a first-wins indexed entry.
func TestFinalFleetGenerationWorkRejectsDuplicateVerifiedCandidate(t *testing.T) {
	t.Parallel()
	source, _ := finalGenerationWorkFixture(t, func(files map[string][]byte) { appendFinalGenerationWorkDuplicate(t, files, string(StageVerified)) })
	actionID, receipt := finalGenerationWorkRefreshMutation(t, source)
	if _, _, _, _, err := source.verifiedMutation(actionID, receipt.TransactionHash, receipt.BlockNumber, receipt.BlockHash, nil); err == nil || !strings.Contains(err.Error(), "multiple matching verified mutations") {
		t.Fatalf("duplicate verified candidate was hidden: %v", err)
	}
}
