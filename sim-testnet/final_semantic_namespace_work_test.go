package main

// Pins executed namespace decoding in the complete cold fixture and the real
// public verifier's cache-miss continuation, without storing any authority.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Reads only detached fixed-size metadata from the already completed graph.
func finalSemanticFixtureNamespaceDecodes() []finalSemanticNamespaceDecode {
	finalSemanticFixtureCache.stateLock.Lock()
	defer finalSemanticFixtureCache.stateLock.Unlock()
	return append([]finalSemanticNamespaceDecode(nil), finalSemanticFixtureCache.namespaceDecodes...)
}

// The observer has one synchronous owner and retains only immutable metadata.
func finalNamespaceCountedWork(observations *[]finalSemanticNamespaceDecode) finalSemanticNamespaceWork {
	return finalSemanticNamespaceWork{decoded: func(value finalSemanticNamespaceDecode) {
		*observations = append(*observations, value)
	}}
}

// Independently pins the exact full wire selected by every observed decode.
func assertFinalNamespaceObservedInput(t *testing.T, evidence *FinalSemanticEvidence, data []byte, observations []finalSemanticNamespaceDecode) {
	t.Helper()
	if evidence.ExpectedMiners != 1000 || evidence.ExpectedCandidates != 202 || evidence.ExpectedHeadSlots != 200 || evidence.ExpectedValidators != 2 || evidence.ExpectedOperators != 2 || evidence.FleetGeneration == nil || len(evidence.FleetGeneration.SetupFleets) != 200 || len(evidence.FleetGeneration.ChallengerFleets) != 2 || len(evidence.FleetGeneration.Batches) != 40 || len(data) == 0 || len(observations) == 0 {
		t.Fatal("namespace work lost the complete signed release graph")
	}
	digest := bytesSHA256(data)
	for _, observation := range observations {
		if observation.stage == "" || observation.deploymentID != evidence.DeploymentID || observation.planHash != evidence.PlanHash || observation.inputBytes != len(data) || observation.contentHash != digest {
			t.Fatalf("namespace observation is not the exact independently checked input: %+v", observation)
		}
	}
}

// The constructor's independent postcondition census and final chronology
// replay consume the same namespace; observations must count actual decodes.
func TestFinalNamespaceFixtureDecodesCompleteInputOnce(t *testing.T) {
	t.Parallel()
	evidence, artifacts := finalSemanticFixture(t)
	data := artifacts[evidence.FleetGeneration.Artifact.URI]
	observations := finalSemanticFixtureNamespaceDecodes()
	assertFinalNamespaceObservedInput(t, &evidence, data, observations)
	for _, observation := range observations {
		if observation.stage != "fixture-postconditions" && observation.stage != "historical-replay" {
			t.Fatalf("cold fixture observed unrelated namespace stage %q", observation.stage)
		}
	}
	t.Logf("cold namespace identity=%s/%s bytes=%d hash=%s executed=%+v", evidence.DeploymentID, evidence.PlanHash, len(data), bytesSHA256(data), observations)
	if len(observations) != 1 {
		t.Fatalf("cold fixture repeated identical authenticated namespace decoding: got %d, want exactly 1", len(observations))
	}
}

// Exercises the unchanged cache-miss body reached by the public wrapper.
// Both semantic admission and the complete actual loader precede observation;
// an existing public cache hit is neither disabled nor claimed to do work.
func TestFinalNamespaceArtifactReplayDecodesCompleteInputOnce(t *testing.T) {
	t.Parallel()
	evidence := finalLineageWorkFixture(t)
	_, artifacts := finalSemanticFixture(t)
	if err := VerifyFinalSemanticEvidence(evidence); err != nil {
		t.Fatal(err)
	}
	uses, err := finalSemanticArtifactUses(evidence)
	if err != nil {
		t.Fatal(err)
	}
	assertFinalSemanticArtifactCensus(t, evidence, uses)
	loaded, err := loadFinalSemanticArtifactUses(t.Context(), uses, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, found := artifacts[locator.URI]
		if !found {
			return nil, fmt.Errorf("complete namespace fixture lacks %s", locator.URI)
		}
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var observations []finalSemanticNamespaceDecode
	if err := verifyFinalSemanticArtifactInputs(t.Context(), evidence, uses, loaded, finalNamespaceCountedWork(&observations)); err != nil {
		t.Fatal(err)
	}
	data := artifacts[evidence.FleetGeneration.Artifact.URI]
	assertFinalNamespaceObservedInput(t, evidence, data, observations)
	for _, observation := range observations {
		if observation.stage != "fleet-replay" && observation.stage != "historical-replay" {
			t.Fatalf("artifact replay observed unrelated namespace stage %q", observation.stage)
		}
	}
	t.Logf("cache-miss namespace identity=%s/%s bytes=%d hash=%s executed=%+v", evidence.DeploymentID, evidence.PlanHash, len(data), bytesSHA256(data), observations)
	if len(observations) != 1 {
		t.Fatalf("public cache-miss replay repeated identical authenticated namespace decoding: got %d, want exactly 1", len(observations))
	}
}

// Each standalone call still decodes and content-checks its current bytes,
// including a changed valid JSON container with a mismatched file digest.
func TestFinalNamespaceStandaloneReauthenticatesChangedBytes(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	original := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	var artifact finalFleetGenerationLineageArtifact
	if err := json.Unmarshal(original, &artifact); err != nil || len(artifact.Files) == 0 {
		t.Fatalf("full lineage source is absent: %v", err)
	}
	artifact.Files[0].Data[0] ^= 1
	changed, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var observations []finalSemanticNamespaceDecode
	for index, data := range [][]byte{original, changed, original} {
		files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, finalNamespaceCountedWork(&observations), "standalone")
		if index == 1 {
			if err == nil || files != nil || !strings.Contains(err.Error(), "content-address mismatched") {
				t.Fatalf("changed standalone namespace was accepted: %v", err)
			}
		} else if err != nil || len(files) == 0 {
			t.Fatalf("unchanged standalone namespace was rejected: %v", err)
		}
		if len(observations) != index+1 || observations[index].inputBytes != len(data) || observations[index].contentHash != bytesSHA256(data) {
			t.Fatal("standalone verification reused another call's decoded authority")
		}
	}
}

// Returned namespace maps and bytes belong solely to their individual call.
func TestFinalNamespaceDecodedFilesOwnAllInputs(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	data := append([]byte(nil), value.cache[value.evidence.FleetGeneration.Artifact.URI]...)
	original := append([]byte(nil), data...)
	first, err := finalFleetGenerationArtifactFiles(value.evidence, data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := finalFleetGenerationArtifactFiles(value.evidence, original)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("independent namespace decodes differ: %v", err)
	}
	clear(data)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("namespace decoder retained the caller's mutable input bytes")
	}
	for name, content := range first {
		clear(content)
		delete(first, name)
	}
	third, err := finalFleetGenerationArtifactFiles(value.evidence, original)
	if err != nil || !reflect.DeepEqual(second, third) {
		t.Fatalf("one namespace owner changed another call's complete output: %v", err)
	}
}

// Identical bytes must be rechecked against each caller's exact namespace
// identity; a previously valid deployment or plan cannot grant another one.
func TestFinalNamespaceRejectsChangedEvidenceIdentity(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	data := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	for _, field := range []string{"deployment", "plan"} {
		changed := *value.evidence
		if field == "deployment" {
			changed.DeploymentID += "-changed"
		} else {
			changed.PlanHash = finalLineageWorkChangedHash(t, changed.PlanHash)
		}
		var observations []finalSemanticNamespaceDecode
		files, err := finalFleetGenerationArtifactFilesWithWork(&changed, data, finalNamespaceCountedWork(&observations), "standalone")
		if err == nil || files != nil || !strings.Contains(err.Error(), "identity differs") || len(observations) != 1 || observations[0].deploymentID != changed.DeploymentID || observations[0].planHash != changed.PlanHash {
			t.Fatalf("changed %s namespace identity reused another owner: %v", field, err)
		}
	}
}

// Strict decoding retains unknown-field/trailing-data rejection on a real
// full namespace rather than trusting an already-observed content digest.
func TestFinalNamespaceRetainsStrictContainerChecks(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	original := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	if len(original) < 2 || original[0] != '{' || original[len(original)-1] != '}' {
		t.Fatal("complete namespace is not its canonical JSON object")
	}
	unknown := append([]byte(`{"unexpected_namespace_field":1,`), original[1:]...)
	trailing := append(append([]byte(nil), original...), []byte(" {}")...)
	for index, data := range [][]byte{unknown, trailing} {
		var observations []finalSemanticNamespaceDecode
		files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, finalNamespaceCountedWork(&observations), "standalone")
		if err == nil || files != nil || len(observations) != 1 || observations[0].contentHash != bytesSHA256(data) {
			t.Fatalf("malformed full namespace case %d was not independently refused: %v", index, err)
		}
	}
}

// Cold metadata readers cannot rewrite the next caller's actual-work record.
func TestFinalNamespaceFixtureObservationIsDetached(t *testing.T) {
	t.Parallel()
	_, _ = finalSemanticFixture(t)
	first, second := finalSemanticFixtureNamespaceDecodes(), finalSemanticFixtureNamespaceDecodes()
	if len(first) == 0 || !slices.Equal(first, second) {
		t.Fatal("cold namespace observations are absent or unstable")
	}
	first[0].stage = "changed-reader"
	first[0].inputBytes++
	first[0].contentHash = "changed-reader"
	if !slices.Equal(second, finalSemanticFixtureNamespaceDecodes()) {
		t.Fatal("cold namespace observation escaped with a mutable alias")
	}
}

// The observation must remain on the actual public miss path, after complete
// admission/loading and before the unchanged successful-cache publication.
func TestFinalNamespaceContinuationIsPublicCacheMissPath(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "final_semantic_evidence.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wrapper *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "VerifyFinalSemanticArtifacts" {
			wrapper = function
		}
	}
	if wrapper == nil || wrapper.Body == nil {
		t.Fatal("public artifact verifier is absent")
	}
	var ordered []string
	continuations := 0
	forwardsFailure := false
	for _, statement := range wrapper.Body.List {
		conditional, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		assignment, ok := conditional.Init.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			continue
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "verifyFinalSemanticArtifactInputs" {
			continue
		}
		assigned, assignedOK := assignment.Lhs[0].(*ast.Ident)
		condition, conditionOK := conditional.Cond.(*ast.BinaryExpr)
		if !assignedOK || assigned.Name != "err" || !conditionOK || condition.Op != token.NEQ || len(conditional.Body.List) != 1 || conditional.Else != nil {
			t.Fatal("public continuation no longer returns its exact failure")
		}
		left, leftOK := condition.X.(*ast.Ident)
		right, rightOK := condition.Y.(*ast.Ident)
		returned, returnedOK := conditional.Body.List[0].(*ast.ReturnStmt)
		if !leftOK || left.Name != "err" || !rightOK || right.Name != "nil" || !returnedOK || len(returned.Results) != 1 {
			t.Fatal("public continuation failure guard was weakened")
		}
		result, ok := returned.Results[0].(*ast.Ident)
		forwardsFailure = ok && result.Name == "err"
	}
	ast.Inspect(wrapper.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		switch name.Name {
		case "VerifyFinalSemanticEvidence", "finalSemanticArtifactUses", "loadFinalSemanticArtifactUses", "finalSemanticArtifactVerificationCacheKey", "finalSemanticArtifactVerificationCacheHit", "verifyFinalSemanticArtifactInputs", "finalSemanticArtifactVerificationCacheStore":
			ordered = append(ordered, name.Name)
		}
		if name.Name == "verifyFinalSemanticArtifactInputs" {
			continuations++
			if len(call.Args) != 5 {
				t.Fatal("public miss continuation lost its exact input handoff")
			}
			for index, want := range []string{"ctx", "evidence", "uses", "cache"} {
				argument, ok := call.Args[index].(*ast.Ident)
				if !ok || argument.Name != want {
					t.Fatalf("public miss continuation argument %d is not the owned %s", index, want)
				}
			}
			work, ok := call.Args[4].(*ast.CompositeLit)
			if !ok || len(work.Elts) != 0 {
				t.Fatal("ordinary public miss path supplies a namespace observer")
			}
		}
		return true
	})
	wantOrder := []string{"VerifyFinalSemanticEvidence", "finalSemanticArtifactUses", "loadFinalSemanticArtifactUses", "finalSemanticArtifactVerificationCacheKey", "finalSemanticArtifactVerificationCacheHit", "verifyFinalSemanticArtifactInputs", "finalSemanticArtifactVerificationCacheStore"}
	if !forwardsFailure || continuations != 1 || !slices.Equal(ordered, wantOrder) {
		t.Fatalf("public cache-miss order changed: got %v, want %v", ordered, wantOrder)
	}
	for _, want := range []string{"verifyFinalSettlementClosureArtifactsWithAuthority", "verifyFinalFleetGenerationArtifactsWithNamespaceWork", "verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork"} {
		if !releaseClosureFunctionCalls(t, "final_semantic_evidence.go", "verifyFinalSemanticArtifactInputs")[want] {
			t.Fatalf("real cache-miss continuation no longer calls %s", want)
		}
	}
}

// A changed lineage wire is rejected by the complete actual content loader,
// before any namespace continuation can consume it as authenticated input.
func TestFinalNamespaceLoaderRejectsChangedBytesBeforeReplay(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	uses, err := finalSemanticArtifactUses(value.evidence)
	if err != nil {
		t.Fatal(err)
	}
	lineageURI := value.evidence.FleetGeneration.Artifact.URI
	lineageLoads := 0
	loaded, err := loadFinalSemanticArtifactUses(t.Context(), uses, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, found := value.cache[locator.URI]
		if !found {
			return nil, fmt.Errorf("missing complete artifact %s", locator.URI)
		}
		if locator.URI == lineageURI {
			lineageLoads++
			changed := append([]byte(nil), data...)
			changed[len(changed)/2] ^= 1
			return changed, nil
		}
		return data, nil
	})
	if err == nil || loaded != nil || lineageLoads != 1 || !strings.Contains(err.Error(), "size or content hash mismatch") {
		t.Fatalf("changed namespace crossed authenticated loading: loads=%d error=%v", lineageLoads, err)
	}
}

// The loader owns its first verified namespace before another loader callback
// can overwrite the returned buffer. No preexisting verdict is supplied.
func TestFinalNamespaceLoaderDetachesBeforeNextCallback(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	uses, err := finalSemanticArtifactUses(value.evidence)
	if err != nil {
		t.Fatal(err)
	}
	lineageURI := value.evidence.FleetGeneration.Artifact.URI
	var borrowed []byte
	mutations := 0
	loaded, err := loadFinalSemanticArtifactUses(t.Context(), uses, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		if borrowed != nil {
			clear(borrowed)
			borrowed = nil
			mutations++
		}
		data, found := value.cache[locator.URI]
		if !found {
			return nil, fmt.Errorf("missing complete artifact %s", locator.URI)
		}
		if locator.URI == lineageURI {
			borrowed = append([]byte(nil), data...)
			return borrowed, nil
		}
		return data, nil
	})
	if err != nil || mutations != 1 || borrowed != nil || !bytes.Equal(loaded[lineageURI], value.cache[lineageURI]) {
		t.Fatalf("namespace ownership did not cross the actual next callback: mutations=%d error=%v", mutations, err)
	}
	var observations []finalSemanticNamespaceDecode
	files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, loaded[lineageURI], finalNamespaceCountedWork(&observations), "owned-loader")
	if err != nil || len(files) == 0 || len(observations) != 1 {
		t.Fatalf("owned full namespace failed independent strict decoding: %v", err)
	}
	assertFinalNamespaceObservedInput(t, value.evidence, value.cache[lineageURI], observations)
}
