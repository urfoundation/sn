package main

// Exercises exact-input reuse with the complete genuinely signed namespace.
// Standalone verification remains the independent reference; no fixture,
// cryptographic boundary, historical row or original work assertion is removed.

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

// Counts actual decoding without introducing an extra full-wire digest solely
// for a work counter. The original causal roots still record exact digests.
func finalNamespaceReuseCountedWork(t *testing.T, decodes *int) finalSemanticNamespaceWork {
	t.Helper()
	return (finalSemanticNamespaceWork{decodedBytes: func(inputBytes int) {
		if inputBytes <= 0 {
			t.Fatal("namespace decoder observed an empty input")
		}
		*decodes++
	}}).forInvocation()
}

// A later replay must not inherit mutable input or output aliases from the
// first consumer, including changes to entire map membership and file slices.
func TestFinalNamespaceReuseOwnsWireAndEveryReturnedFile(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	original := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	want, err := finalFleetGenerationArtifactFiles(value.evidence, original)
	if err != nil {
		t.Fatal(err)
	}
	borrowed := bytes.Clone(original)
	decodes := 0
	work := finalNamespaceReuseCountedWork(t, &decodes)
	first, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, borrowed, work, "first-owner")
	if err != nil || !reflect.DeepEqual(first, want) {
		t.Fatalf("first complete namespace differs from standalone: %v", err)
	}
	clear(borrowed)
	for name, data := range first {
		clear(data)
		delete(first, name)
	}
	second, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, original, work, "second-owner")
	if err != nil || !reflect.DeepEqual(second, want) {
		t.Fatalf("first owner changed retained namespace inputs: %v", err)
	}
	for name, data := range second {
		clear(data)
		delete(second, name)
	}
	third, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, bytes.Clone(original), work, "third-owner")
	if err != nil || !reflect.DeepEqual(third, want) || decodes != 1 || !bytes.Equal(work.reuse.data, original) {
		t.Fatalf("reuse exposes a retained input or output alias: decodes=%d error=%v", decodes, err)
	}
}

// Equivalent JSON is still a different source byte stream. Retaining only the
// latest successful input forces a restored prior encoding through decoding.
func TestFinalNamespaceReuseReauthenticatesEquivalentWireAndRetainsOneInput(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	original := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	changed := append(bytes.Clone(original), ' ')
	want, err := finalFleetGenerationArtifactFiles(value.evidence, changed)
	if err != nil {
		t.Fatalf("alternative exact-byte control is not a valid complete namespace: %v", err)
	}
	decodes := 0
	work := finalNamespaceReuseCountedWork(t, &decodes)
	for index, data := range [][]byte{original, changed, original} {
		files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, work, "equivalent-wire")
		if err != nil || !reflect.DeepEqual(files, want) || decodes != index+1 || !bytes.Equal(work.reuse.data, data) || !reflect.DeepEqual(work.reuse.pathFileBytes, want) {
			t.Fatalf("exact wire %d bypassed decoding or grew prior-input retention: decodes=%d error=%v", index, decodes, err)
		}
	}
}

// Invalid inputs never become reusable, either in an empty slot or after a
// valid decode. Failure must not overwrite the previously authenticated input.
func TestFinalNamespaceReuseNeverRetainsInvalidWire(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	original := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	var artifact finalFleetGenerationLineageArtifact
	if err := json.Unmarshal(original, &artifact); err != nil || len(artifact.Files) == 0 || len(artifact.Files[0].Data) == 0 {
		t.Fatalf("complete namespace lacks a corruption target: %v", err)
	}
	artifact.Files[0].Data[0] ^= 1
	invalid, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	decodes := 0
	work := finalNamespaceReuseCountedWork(t, &decodes)
	for call := 0; call < 2; call++ {
		files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, invalid, work, "invalid-empty")
		if err == nil || files != nil || !strings.Contains(err.Error(), "content-address mismatched") || decodes != call+1 || work.reuse.pathFileBytes != nil || work.reuse.data != nil {
			t.Fatalf("invalid namespace became reusable: decodes=%d error=%v", decodes, err)
		}
	}
	want, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, original, work, "valid")
	if err != nil || decodes != 3 {
		t.Fatalf("valid complete namespace did not recover after failure: %v", err)
	}
	unknown := append([]byte(`{"unexpected_namespace_field":1,`), original[1:]...)
	trailing := append(bytes.Clone(original), []byte(" {}")...)
	for index, data := range [][]byte{invalid, invalid, unknown, trailing} {
		files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, work, "invalid-populated")
		if err == nil || files != nil || decodes != index+4 || !bytes.Equal(work.reuse.data, original) || !reflect.DeepEqual(work.reuse.pathFileBytes, want) {
			t.Fatalf("invalid source %d escaped full decoding or replaced valid input: decodes=%d error=%v", index, decodes, err)
		}
	}
	files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, original, work, "restored")
	if err != nil || decodes != 7 || !reflect.DeepEqual(files, want) {
		t.Fatalf("failed input poisoned a later exact valid replay: decodes=%d error=%v", decodes, err)
	}
}

// Reuse binds every namespace context field, not just a digest or path. The
// complete decoder still decides semantic admission for a mismatched context.
func TestFinalNamespaceReuseRequiresExactContext(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	data := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	decodes := 0
	work := finalNamespaceReuseCountedWork(t, &decodes)
	want, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, work, "original-context")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name   string
		change func(*FinalSemanticEvidence)
	}{
		{name: "deployment", change: func(evidence *FinalSemanticEvidence) { evidence.DeploymentID += "-other" }},
		{name: "plan", change: func(evidence *FinalSemanticEvidence) {
			evidence.PlanHash = finalLineageWorkChangedHash(t, evidence.PlanHash)
		}},
		{name: "chain", change: func(evidence *FinalSemanticEvidence) { evidence.ChainID++ }},
		{name: "netuid", change: func(evidence *FinalSemanticEvidence) { evidence.Netuid++ }},
		{name: "kind", change: func(evidence *FinalSemanticEvidence) { evidence.FleetGeneration.Artifact.Kind += "-other" }},
		{name: "uri", change: func(evidence *FinalSemanticEvidence) { evidence.FleetGeneration.Artifact.URI += ".other" }},
		{name: "digest", change: func(evidence *FinalSemanticEvidence) { evidence.FleetGeneration.Artifact.ContentHash += "-other" }},
		{name: "size", change: func(evidence *FinalSemanticEvidence) { evidence.FleetGeneration.Artifact.SizeBytes++ }},
		{name: "lineage", change: func(evidence *FinalSemanticEvidence) { evidence.FleetGeneration = nil }},
	} {
		changed := *value.evidence
		lineage := *value.evidence.FleetGeneration
		changed.FleetGeneration = &lineage
		item.change(&changed)
		if files, found := work.reuse.files(finalSemanticNamespaceIdentityFor(&changed), data); found || files != nil {
			t.Fatalf("changed %s context reused an earlier namespace", item.name)
		}
		if item.name == "deployment" || item.name == "plan" {
			files, err := finalFleetGenerationArtifactFilesWithWork(&changed, data, work, "changed-context")
			if err == nil || files != nil || !strings.Contains(err.Error(), "identity differs") {
				t.Fatalf("changed %s was not rejected by full namespace decoding: %v", item.name, err)
			}
		}
	}
	if decodes != 3 {
		t.Fatalf("changed deployment/plan did not execute complete decoding: %d", decodes)
	}
	// Chain equality is a reuse key, not a newly invented namespace policy:
	// the original decoder accepts this container before later chain checks.
	changed := *value.evidence
	changed.ChainID++
	files, err := finalFleetGenerationArtifactFilesWithWork(&changed, data, work, "changed-chain")
	if err != nil || decodes != 4 || !reflect.DeepEqual(files, want) {
		t.Fatalf("context miss changed standalone namespace semantics: decodes=%d error=%v", decodes, err)
	}
}

// The actual two caller bodies must allocate fresh state. A copied observer
// cannot smuggle decoded inputs from an earlier invocation into either body.
func TestFinalNamespaceReuseIsFreshAtBothOwnerBoundaries(t *testing.T) {
	t.Parallel()
	for _, owner := range []struct {
		file     string
		function string
		name     string
	}{
		{file: "final_semantic_evidence.go", function: "verifyFinalSemanticArtifactInputs", name: "namespaceWork"},
		{file: "final_semantic_fixture_generation_test.go", function: "attachFinalSemanticFixtureGenerationWithWorkControl", name: "work.namespaceWork"},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), owner.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != owner.function || function.Body == nil {
				continue
			}
			for index, statement := range function.Body.List {
				if owner.name == "work.namespaceWork" && index == 0 {
					expression, ok := statement.(*ast.ExprStmt)
					if !ok {
						t.Fatal("fixture performs another operation before namespace ownership")
					}
					call, ok := expression.X.(*ast.CallExpr)
					if !ok || len(call.Args) != 0 {
						t.Fatal("fixture prefix is no longer only t.Helper()")
					}
					method, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || method.Sel.Name != "Helper" {
						t.Fatal("fixture prefix calls another owner")
					}
					receiver, ok := method.X.(*ast.Ident)
					if !ok || receiver.Name != "t" {
						t.Fatal("fixture prefix is not the test helper declaration")
					}
					continue
				}
				assignment, ok := statement.(*ast.AssignStmt)
				if !ok || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
					break
				}
				call, ok := assignment.Rhs[0].(*ast.CallExpr)
				if !ok || len(call.Args) != 0 {
					break
				}
				method, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || method.Sel.Name != "forInvocation" {
					break
				}
				name := func(expression ast.Expr) string {
					if identity, ok := expression.(*ast.Ident); ok {
						return identity.Name
					}
					if field, ok := expression.(*ast.SelectorExpr); ok {
						if identity, ok := field.X.(*ast.Ident); ok {
							return identity.Name + "." + field.Sel.Name
						}
					}
					return ""
				}
				found = name(assignment.Lhs[0]) == owner.name && name(method.X) == owner.name
				break
			}
		}
		if !found {
			t.Fatalf("actual %s owner no longer begins with fresh namespace inputs", owner.function)
		}
	}
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	data := value.cache[value.evidence.FleetGeneration.Artifact.URI]
	decodes := 0
	prior := finalNamespaceReuseCountedWork(t, &decodes)
	want, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, prior, "prior-call")
	if err != nil {
		t.Fatal(err)
	}
	next := prior.forInvocation()
	if next.reuse == prior.reuse || next.reuse.pathFileBytes != nil || next.reuse.data != nil {
		t.Fatal("new invocation inherited the prior namespace slot")
	}
	files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, next, "next-call")
	if err != nil || decodes != 2 || !reflect.DeepEqual(files, want) {
		t.Fatalf("independent invocation reused prior authentication: decodes=%d error=%v", decodes, err)
	}
}

// Namespace reuse is only decoded input reuse: the full historical verifier
// still authenticates every plan, journal, receipt and exact per-row binding.
func TestFinalNamespaceReuseHistoricalReplayStillChecksEveryRow(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	row := value.currentRow(t)
	decodes := 0
	namespace := finalNamespaceReuseCountedWork(t, &decodes)
	if _, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, value.cache[value.evidence.FleetGeneration.Artifact.URI], namespace, "earlier-full-decode"); err != nil {
		t.Fatal(err)
	}
	want := [4]int{0, len(value.current.PriorPlanHashes) + 1, 1, len(value.evidence.HistoricalCoordinatorReceipts)}
	if want != [4]int{0, 2, 1, 2} {
		t.Fatalf("complete current/predecessor historical census changed: %v", want)
	}
	var counts, sizes [4]int
	work := finalHistoricalCoordinatorCountedWork(t, &counts, &sizes)
	work.namespaceWork = namespace
	if err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, work); err != nil || counts != want || decodes != 1 {
		t.Fatalf("same-input historical replay skipped complete per-row authentication: counts=%v decodes=%d error=%v", counts, decodes, err)
	}
	var receipt finalHistoricalCoordinatorReceiptArtifact
	if err := json.Unmarshal(value.cache[row.ReceiptArtifact.URI], &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.Input += "00"
	changedReceipt, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name string
		uri  string
		data []byte
		want string
	}{
		{name: "plan", uri: row.PlanArtifact.URI, data: append(bytes.Clone(value.cache[row.PlanArtifact.URI]), ' '), want: "differs from the approved lineage"},
		{name: "journal", uri: row.JournalArtifact.URI, data: append(bytes.Clone(value.cache[row.JournalArtifact.URI]), '\n'), want: "differs from fleet-lineage journal"},
		{name: "receipt", uri: row.ReceiptArtifact.URI, data: changedReceipt, want: "transaction artifact differs from its sealed row"},
	} {
		original := value.cache[item.uri]
		value.cache[item.uri] = item.data
		counts, sizes = [4]int{}, [4]int{}
		err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, work)
		value.cache[item.uri] = original
		if err == nil || !strings.Contains(err.Error(), item.want) || counts[0] != 0 || decodes != 1 {
			t.Fatalf("namespace reuse bypassed exact %s row checks: counts=%v decodes=%d error=%v", item.name, counts, decodes, err)
		}
	}
	counts, sizes = [4]int{}, [4]int{}
	if err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, work); err != nil || counts != want || decodes != 1 {
		t.Fatalf("restored complete rows no longer verify independently: counts=%v decodes=%d error=%v", counts, decodes, err)
	}
}

// A changed independently loaded namespace must execute its complete decoder
// before any historical row; a warm original cannot authorize changed bytes.
func TestFinalNamespaceReuseRejectsChangedNamespaceBeforeHistoricalRows(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	uri := value.evidence.FleetGeneration.Artifact.URI
	original := value.cache[uri]
	decodes := 0
	namespace := finalNamespaceReuseCountedWork(t, &decodes)
	if _, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, original, namespace, "earlier-full-decode"); err != nil {
		t.Fatal(err)
	}
	var artifact finalFleetGenerationLineageArtifact
	if err := json.Unmarshal(original, &artifact); err != nil || len(artifact.Files) == 0 || len(artifact.Files[0].Data) == 0 {
		t.Fatalf("full namespace lacks a corruption target: %v", err)
	}
	artifact.Files[0].Data[0] ^= 1
	changed, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	value.cache[uri] = changed
	var counts, sizes [4]int
	work := finalHistoricalCoordinatorCountedWork(t, &counts, &sizes)
	work.namespaceWork = namespace
	err = verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, work)
	if err == nil || !strings.Contains(err.Error(), "content-address mismatched") || counts != [4]int{1, 0, 0, 0} || decodes != 2 || !bytes.Equal(namespace.reuse.data, original) {
		t.Fatalf("changed namespace crossed historical input admission: counts=%v decodes=%d error=%v", counts, decodes, err)
	}
	value.cache[uri] = original
	counts, sizes = [4]int{}, [4]int{}
	if err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, work); err != nil || counts != [4]int{0, 2, 1, 2} || decodes != 2 {
		t.Fatalf("failed namespace poisoned restored historical replay: counts=%v decodes=%d error=%v", counts, decodes, err)
	}
}

// Missing current inputs cannot be satisfied by an earlier retained source.
// Refusal preserves the original empty-input and missing-artifact boundaries.
func TestFinalNamespaceReuseMissingInputsCannotUseEarlierFiles(t *testing.T) {
	t.Parallel()
	value := finalHistoricalCoordinatorWorkTestFixture(t)
	uri := value.evidence.FleetGeneration.Artifact.URI
	data := value.cache[uri]
	decodes := 0
	namespace := finalNamespaceReuseCountedWork(t, &decodes)
	if _, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, data, namespace, "earlier-full-decode"); err != nil {
		t.Fatal(err)
	}
	if files, err := finalFleetGenerationArtifactFilesWithWork(nil, data, namespace, "missing-evidence"); err == nil || files != nil {
		t.Fatalf("missing evidence reused prior namespace: %v", err)
	}
	if files, err := finalFleetGenerationArtifactFilesWithWork(value.evidence, nil, namespace, "missing-bytes"); err == nil || files != nil {
		t.Fatalf("missing bytes reused prior namespace: %v", err)
	}
	delete(value.cache, uri)
	err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, finalHistoricalCoordinatorArtifactWork{namespaceWork: namespace})
	if err == nil || !strings.Contains(err.Error(), "fleet lineage artifact is not loaded") || decodes != 1 {
		t.Fatalf("missing current artifact reused prior namespace: decodes=%d error=%v", decodes, err)
	}
	value.cache[uri] = data
	if err := verifyFinalHistoricalCoordinatorReceiptArtifactsWithWork(value.evidence, value.current, value.cache, finalHistoricalCoordinatorArtifactWork{namespaceWork: namespace}); err != nil || decodes != 1 {
		t.Fatalf("missing-input refusal poisoned restored full replay: decodes=%d error=%v", decodes, err)
	}
}
