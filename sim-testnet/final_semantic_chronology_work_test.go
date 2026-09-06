package main

// Pins historical replay's complete source-decoding work and ownership using
// the original release-scale, genuinely signed fixture and all receipt rows.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Owns one test's detached graph; no validated verdict is cached here.
type finalHistoricalCoordinatorWorkFixture struct {
	evidence *FinalSemanticEvidence
	current  *SetupPlan
	cache    map[string][]byte
}

// Obtains the full cold fixture through its existing owner, then checks the
// independently fixed census before using its complete historical artifacts.
func finalHistoricalCoordinatorWorkTestFixture(t *testing.T) finalHistoricalCoordinatorWorkFixture {
	t.Helper()
	evidence, cache := finalSemanticFixture(t)
	if evidence.ExpectedMiners != 1000 || evidence.ExpectedCandidates != 202 || evidence.ExpectedHeadSlots != 200 || evidence.ExpectedValidators != 2 || evidence.ExpectedOperators != 2 || evidence.FleetGeneration == nil || len(evidence.FleetGeneration.SetupFleets) != 200 || len(evidence.FleetGeneration.ChallengerFleets) != 2 || len(evidence.FleetGeneration.Batches) != 40 || len(evidence.HistoricalCoordinatorReceipts) < 2 || len(evidence.HistoricalCoordinatorTimeline) == 0 {
		t.Fatal("historical work fixture lost the complete release census")
	}
	current, err := verifyFinalSetupPlanArtifact(&evidence, cache[evidence.PlanArtifact.URI])
	if err != nil || len(current.PriorPlanHashes) != 1 {
		t.Fatalf("historical work fixture lost its approved predecessor: %v", err)
	}
	return finalHistoricalCoordinatorWorkFixture{evidence: &evidence, current: current, cache: cache}
}

// Selects an actual current-plan row. A retained predecessor plan does not
// imply any historical receipt was executed under that predecessor.
func (self finalHistoricalCoordinatorWorkFixture) currentRow(t *testing.T) *FinalHistoricalCoordinatorReceiptEvidence {
	t.Helper()
	for index := range self.evidence.HistoricalCoordinatorReceipts {
		row := &self.evidence.HistoricalCoordinatorReceipts[index]
		if row.PlanHash == self.current.PlanHash {
			return row
		}
	}
	t.Fatal("complete historical fixture has no actual current-plan receipt")
	return nil
}

// Counts executed decoders synchronously; the callback cannot replace their
// inputs or verdicts and retains only fixed-size metadata for this call.
func finalHistoricalCoordinatorCountedWork(t *testing.T, counts *[4]int, inputBytes *[4]int) finalHistoricalCoordinatorArtifactWork {
	t.Helper()
	return finalHistoricalCoordinatorArtifactWork{decoded: func(kind string, size int) {
		index := -1
		switch kind {
		case "lineage":
			index = 0
		case "plan":
			index = 1
		case "journal":
			index = 2
		case "receipt":
			index = 3
		default:
			t.Fatalf("unknown historical decoder kind %q", kind)
		}
		if size <= 0 {
			t.Fatalf("historical decoder %s did not receive its actual source bytes", kind)
		}
		counts[index]++
		inputBytes[index] += size
	}}
}

// Identical, already decoded sources must be joined inside one invocation,
// retaining the full row census and independent receipt proof verification.
func TestFinalHistoricalCoordinatorDecodesEachSourceOnce(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	value.currentRow(t)
	planCount := len(value.current.PriorPlanHashes)
	currentRows, predecessorRows := 0, 0
	for _, row := range value.evidence.HistoricalCoordinatorReceipts {
		if row.PlanHash == value.current.PlanHash {
			currentRows++
		} else if value.current.allowedPlanHashes()[row.PlanHash] {
			predecessorRows++
		} else {
			t.Fatalf("historical row names an unapproved plan %s", row.PlanHash)
		}
	}
	if currentRows != 0 {
		planCount++
	}
	want := [4]int{1, planCount, 1, len(value.evidence.HistoricalCoordinatorReceipts)}
	var counts, inputBytes [4]int
	if err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, finalHistoricalCoordinatorCountedWork(t, &counts, &inputBytes)); err != nil {
		t.Fatal(err)
	}
	t.Logf("historical complete source work: deployment=%s current_plan=%s predecessors=%d rows=%d current_rows=%d predecessor_rows=%d lineage_bytes=%d executed=[lineage plan journal receipt]=%v bytes=%v", value.evidence.DeploymentID, value.current.PlanHash, len(value.current.PriorPlanHashes), len(value.evidence.HistoricalCoordinatorReceipts), currentRows, predecessorRows, len(value.cache[value.evidence.FleetGeneration.Artifact.URI]), counts, inputBytes)
	if counts != want {
		t.Fatalf("historical coordinator verifier repeated authenticated source decoding: got [lineage plan journal receipt]=%v, want %v", counts, want)
	}
}

// Separate full calls and standalone lineage lookups retain their complete
// authentication work; identical inputs never grant cross-call authority.
func TestFinalHistoricalCoordinatorStandaloneRechecksEveryCall(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	var previous [4]int
	for call := 0; call < 2; call++ {
		var counts, inputBytes [4]int
		if err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, finalHistoricalCoordinatorCountedWork(t, &counts, &inputBytes)); err != nil {
			t.Fatal(err)
		}
		for _, count := range counts {
			if count == 0 {
				t.Fatalf("independent historical call %d bypassed a full source decoder: %v", call, counts)
			}
		}
		if call != 0 && counts != previous {
			t.Fatalf("independent historical call reused an earlier verdict: previous=%v current=%v", previous, counts)
		}
		previous = counts
		counts, inputBytes = [4]int{}, [4]int{}
		plans, entries, journal, err := finalHistoricalCoordinatorArtifactLineageWithWork(value.evidence, value.current, value.cache, finalHistoricalCoordinatorCountedWork(t, &counts, &inputBytes))
		if err != nil || counts != [4]int{1, len(value.current.PriorPlanHashes), 1, 0} || len(plans) != 1+len(value.current.PriorPlanHashes) || len(entries) == 0 || len(journal) == 0 {
			t.Fatalf("standalone historical lineage bypassed full decoding: counts=%v error=%v", counts, err)
		}
	}
}

// Valid alternative JSON spelling cannot replace the exact separately sealed
// current-plan bytes, even when its persisted plan hash is unchanged.
func TestFinalHistoricalCoordinatorRejectsChangedPlanBytes(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	row := value.currentRow(t)
	if row.PlanArtifact.URI == value.evidence.PlanArtifact.URI {
		t.Fatal("historical row and independently sealed current plan share an artifact")
	}
	data := append(append([]byte(nil), value.cache[row.PlanArtifact.URI]...), ' ')
	plan, err := decodePersistedPlanBytes(data)
	if err != nil || plan.PlanHash != row.PlanHash {
		t.Fatalf("changed plan spelling is not a valid equal-hash control: %v", err)
	}
	value.cache[row.PlanArtifact.URI] = data
	err = verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache)
	if err == nil || !strings.Contains(err.Error(), "differs from the approved lineage") {
		t.Fatalf("changed exact current-plan bytes escaped historical replay: %v", err)
	}
}

// A valid unchanged hash chain with different transport bytes still differs
// from the exact journal sealed in the full fleet-lineage source artifact.
func TestFinalHistoricalCoordinatorRejectsChangedSharedJournalBytes(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	row := value.currentRow(t)
	data := append(append([]byte(nil), value.cache[row.JournalArtifact.URI]...), '\n')
	if _, err := decodeFinalSemanticJournalBytes(data); err != nil {
		t.Fatalf("changed journal spelling is not a valid hash-chain control: %v", err)
	}
	value.cache[row.JournalArtifact.URI] = data
	err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache)
	if err == nil || !strings.Contains(err.Error(), "differs from fleet-lineage journal") {
		t.Fatalf("changed exact shared journal escaped historical replay: %v", err)
	}
}

// The reused receipt representation must remain tied to every exact calldata
// byte and the independently sealed raw-log proof, not just transaction ids.
func TestFinalHistoricalCoordinatorRejectsChangedReceiptProjection(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	row := value.currentRow(t)
	original := value.cache[row.ReceiptArtifact.URI]
	if row.ReceiptArtifact.URI == row.Receipt.Proof.URI {
		t.Fatal("historical replay and independent raw proof share an artifact")
	}
	if _, _, err := finalHistoricalCoordinatorReceiptArtifactTransaction(row, original); err != nil {
		t.Fatal(err)
	}
	var artifact finalHistoricalCoordinatorReceiptArtifact
	if err := decodeStrictJSONBytes(original, &artifact); err != nil {
		t.Fatal(err)
	}
	artifact.Input += "00"
	changed, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	value.cache[row.ReceiptArtifact.URI] = changed
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err == nil || !strings.Contains(err.Error(), "transaction artifact differs from its sealed row") {
		t.Fatalf("changed receipt calldata escaped historical replay: %v", err)
	}
	value.cache[row.ReceiptArtifact.URI] = original
	var proof finalHistoricalCoordinatorReceiptProof
	if err := decodeStrictJSONBytes(value.cache[row.Receipt.Proof.URI], &proof); err != nil || len(proof.Logs) == 0 {
		t.Fatalf("independent raw receipt proof is unavailable: %v", err)
	}
	proof.Logs[0].Data += "00"
	changed, err = json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	value.cache[row.Receipt.Proof.URI] = changed
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err == nil || !strings.Contains(err.Error(), "captured receipt proof") {
		t.Fatalf("changed independent raw proof escaped historical replay: %v", err)
	}
}

// Shared parsed sources cannot replace per-row intent/emitter comparisons or
// independent timeline/oracle-window validation against their signed context.
func TestFinalHistoricalCoordinatorRetainsRowAndChronologyChecks(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	row := value.currentRow(t)
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err != nil {
		t.Fatal(err)
	}
	originalIntent := row.IntentHash
	row.IntentHash = finalLineageWorkChangedHash(t, row.IntentHash)
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err == nil {
		t.Fatal("changed row intent escaped historical replay")
	}
	row.IntentHash = originalIntent
	originalEmitters := row.Emitters
	row.Emitters = append(append([]string(nil), row.Emitters...), row.CoordinatorProxy)
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err == nil || !strings.Contains(err.Error(), "emitter graph differs") {
		t.Fatalf("changed row emitter census escaped historical replay: %v", err)
	}
	row.Emitters = originalEmitters
	timeline := &value.evidence.HistoricalCoordinatorTimeline[0]
	originalRuntime := timeline.ProxyRuntimeHash
	timeline.ProxyRuntimeHash = finalLineageWorkChangedHash(t, originalRuntime)
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err == nil || !strings.Contains(err.Error(), "timeline artifact differs from signed evidence") {
		t.Fatalf("changed timeline context escaped historical replay: %v", err)
	}
	timeline.ProxyRuntimeHash = originalRuntime
	var window finalHistoricalCoordinatorOracleWindowArtifact
	if err := decodeStrictJSONBytes(value.cache[value.evidence.FleetRefreshOracleWindow.Artifact.URI], &window); err != nil {
		t.Fatal(err)
	}
	window.AwaitRestored.PlanHash = finalLineageWorkChangedHash(t, window.AwaitRestored.PlanHash)
	changed, err := json.Marshal(window)
	if err != nil {
		t.Fatal(err)
	}
	value.cache[value.evidence.FleetRefreshOracleWindow.Artifact.URI] = changed
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err == nil || !strings.Contains(err.Error(), "oracle fleet.refresh.oracle-await-restored verified identity differs") {
		t.Fatalf("changed oracle-window lineage escaped historical replay: %v", err)
	}
}

// Exact byte hashes and typed wire snapshots catch changes to caller-owned
// maps, nested raw messages, plans and evidence without normalizing any data.
func TestFinalHistoricalCoordinatorDoesNotMutateInputs(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	snapshot := func() ([]byte, []byte, map[string]string) {
		evidence, err := json.Marshal(value.evidence)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := json.Marshal(value.current)
		if err != nil {
			t.Fatal(err)
		}
		artifactHashes := make(map[string]string, len(value.cache))
		for uri, data := range value.cache {
			artifactHashes[uri] = fmt.Sprintf("%d:%s", len(data), bytesSHA256(data))
		}
		return evidence, plan, artifactHashes
	}
	evidenceBefore, planBefore, hashesBefore := snapshot()
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err != nil {
		t.Fatal(err)
	}
	evidenceAfter, planAfter, hashesAfter := snapshot()
	if !bytes.Equal(evidenceBefore, evidenceAfter) || !bytes.Equal(planBefore, planAfter) || !reflect.DeepEqual(hashesBefore, hashesAfter) {
		t.Fatal("historical replay mutated its detached caller-owned source graph")
	}
}

// A changed but internally content-consistent lineage at the same URI is
// decoded anew; exact per-row plan bytes and unrelated file hashes still bind.
func TestFinalHistoricalCoordinatorChangedLineageReauthenticatesAcrossCalls(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	value.currentRow(t)
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(value.evidence, value.current, value.cache); err != nil {
		t.Fatal(err)
	}
	uri := value.evidence.FleetGeneration.Artifact.URI
	original := value.cache[uri]
	var artifact finalFleetGenerationLineageArtifact
	if err := decodeStrictJSONBytes(original, &artifact); err != nil {
		t.Fatal(err)
	}
	name := "launch-foundation/plan.json"
	found := false
	for index := range artifact.Files {
		file := &artifact.Files[index]
		if file.Path != name {
			continue
		}
		file.Data = append(file.Data, ' ')
		file.SizeBytes, file.ContentHash = uint64(len(file.Data)), bytesSHA256(file.Data)
		found = true
	}
	if !found {
		t.Fatal("real current-plan source file is absent from full lineage")
	}
	changed, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalFleetGenerationArtifactFiles(value.evidence, changed); err != nil {
		t.Fatalf("changed lineage is not an internally content-consistent control: %v", err)
	}
	value.cache[uri] = changed
	var counts, inputBytes [4]int
	err = verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, finalHistoricalCoordinatorCountedWork(t, &counts, &inputBytes))
	if err == nil || !strings.Contains(err.Error(), "current plan differs from fleet lineage bytes") || counts[0] == 0 {
		t.Fatalf("changed same-URI lineage reused prior authority: decodes=%v error=%v", counts, err)
	}
	value.cache[uri] = original
	plans, _, _, err := finalHistoricalCoordinatorArtifactLineage(value.evidence, value.current, value.cache)
	if err != nil {
		t.Fatal(err)
	}
	predecessorHash := value.current.PriorPlanHashes[0]
	predecessorPath := filepath.ToSlash(filepath.Join("plan-history", stringsTrim0x(predecessorHash)+".json"))
	files, err := finalFleetGenerationArtifactFiles(value.evidence, original)
	if err != nil || len(files[predecessorPath]) == 0 || plans[predecessorHash] == nil {
		t.Fatalf("retained predecessor source is unavailable independently of receipt rows: %v", err)
	}
	if data, err := finalHistoricalCoordinatorArtifactPlanBytes(value.evidence, value.current, plans[predecessorHash], value.cache); err != nil || !bytes.Equal(data, files[predecessorPath]) {
		t.Fatalf("standalone exact plan lookup rejected restored source: %v", err)
	}
	if err := decodeStrictJSONBytes(original, &artifact); err != nil {
		t.Fatal(err)
	}
	changedUnrelated := false
	for index := range artifact.Files {
		file := &artifact.Files[index]
		if file.Path == predecessorPath {
			continue
		}
		digest := finalLineageWorkChangedHash(t, "0x"+strings.TrimPrefix(file.ContentHash, "sha256:"))
		file.ContentHash = "sha256:" + strings.TrimPrefix(digest, "0x")
		changedUnrelated = true
		break
	}
	if !changedUnrelated {
		t.Fatal("complete lineage has no unrelated file for strict predecessor lookup")
	}
	changed, err = json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	value.cache[uri] = changed
	if _, err := finalHistoricalCoordinatorArtifactPlanBytes(value.evidence, value.current, plans[predecessorHash], value.cache); err == nil || !strings.Contains(err.Error(), "content-address mismatched") {
		t.Fatalf("standalone plan lookup skipped unrelated lineage file authentication: %v", err)
	}
}
