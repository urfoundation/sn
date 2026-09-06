package main

// A source graph supplements, but never replaces, the actual build and real
// bounded replay controls. Both retained head accounting variants are valid
// graphs: the old preview owns its guard, the repair delegates to its helper.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Read every production source, so a future unaccounted consumer cannot hide
// outside a hand-selected file list. Tests and comments cannot define the cap.
func releaseArtifactBoundProductionSources(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("../validator/*.go")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources[filepath.Base(path)] = string(raw)
	}
	return sources
}

// Require a single explicit constant alias to the original envelope owner and
// enumerate every consumer. This intentionally does not perform Go typechecking.
func verifyReleaseArtifactBoundSourceGraph(sources map[string]string) error {
	const alias = "maxReleaseMeasurementArtifactBytes"
	const owner = "releaseMeasurementEnvelopeMaxArtifactSize"
	type definition struct {
		path string
		kind token.Token
		value ast.Expr
	}
	definitions := map[string]definition{}
	uses := map[string]int{}
	totalIdentifiers := 0
	previewDelegates := false
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, sources[path], 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok && name.Name == alias {
				totalIdentifiers++
			}
			return true
		})
		for _, declaration := range file.Decls {
			if group, ok := declaration.(*ast.GenDecl); ok {
				for _, spec := range group.Specs {
					values, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for index, name := range values.Names {
						if name.Name != alias && name.Name != owner {
							continue
						}
						if _, duplicate := definitions[name.Name]; duplicate || len(values.Values) != len(values.Names) {
							return fmt.Errorf("duplicate or implicit artifact ceiling definition %s", name.Name)
						}
						definitions[name.Name] = definition{path: path, kind: group.Tok, value: values.Values[index]}
					}
				}
			}
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			key := path + ":" + function.Name.Name
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if name, ok := node.(*ast.Ident); ok && name.Name == alias {
					uses[key]++
				}
				if path == "release_head_v2.go" && function.Name.Name == "previewForEpochV2" {
					if call, ok := node.(*ast.CallExpr); ok {
						if name, ok := call.Fun.(*ast.SelectorExpr); ok && name.Sel.Name == "previewForEpochV2WithBudget" {
							previewDelegates = true
						}
					}
				}
				return true
			})
		}
	}
	compact, found := definitions[alias]
	target, isAlias := compact.value.(*ast.Ident)
	if !found || compact.kind != token.CONST || compact.path != "release_measurement_v2.go" || !isAlias || target.Name != owner {
		return fmt.Errorf("compact artifact ceiling lacks its exact envelope constant alias")
	}
	envelope, found := definitions[owner]
	if !found || envelope.kind != token.CONST || envelope.path != "release_measurement_envelope.go" {
		return fmt.Errorf("original envelope ceiling definition is missing")
	}
	var integer func(ast.Expr) (constant.Value, error)
	integer = func(expression ast.Expr) (constant.Value, error) {
		switch value := expression.(type) {
		case *ast.BasicLit:
			if value.Kind == token.INT {
				number := constant.MakeFromLiteral(value.Value, token.INT, 0)
				if number.Kind() == constant.Int {
					return number, nil
				}
			}
		case *ast.BinaryExpr:
			if value.Op == token.MUL {
				left, leftErr := integer(value.X)
				right, rightErr := integer(value.Y)
				if leftErr == nil && rightErr == nil {
					return constant.BinaryOp(left, token.MUL, right), nil
				}
			}
		}
		return nil, fmt.Errorf("envelope ceiling is not the original finite integer product")
	}
	number, err := integer(envelope.value)
	if err != nil {
		return err
	}
	value, exact := constant.Uint64Val(number)
	if !exact || value != 64*1024*1024 {
		return fmt.Errorf("original envelope ceiling changed: %s", number)
	}
	expected := map[string]int{
		"release_measurement_v2.go:ownReleaseMeasurementV2": 2,
		"release_measurement_v2.go:decodeReleaseMeasurementV2Bytes": 1,
		"release_measurement_v2.go:VerifyReleaseMeasurementIntentV2": 2,
		"release_head_v2.go:gatherHeadV2": 1,
		"release_head_v2.go:admitReleaseHeadV2Controls": 1,
		"release_head_v2_admission.go:admitReleaseHeadV2Known": 1,
		"release_head_v2_admission.go:admitReleaseHeadV2Current": 1,
		"release_head_v2_admission.go:previewForEpochV2WithBudget": 1,
		"release_head_v2_admission.go:previewReleaseHeadV2Fleets": 1,
	}
	if !previewDelegates {
		expected["release_head_v2.go:previewForEpochV2"] = 1
	}
	consumers := 0
	for _, count := range expected {
		consumers += count
	}
	if !reflect.DeepEqual(uses, expected) || totalIdentifiers != consumers+1 {
		return fmt.Errorf("artifact ceiling source graph changed: uses=%v identifiers=%d", uses, totalIdentifiers)
	}
	return nil
}

// A successful build alone does not pin an alias to the intended existing cap
// or retain every live/decoder/intent owner when composing independent lanes.
func TestProducerGateStateSelectionArtifactBoundSourceGraph(t *testing.T) {
	if err := verifyReleaseArtifactBoundSourceGraph(releaseArtifactBoundProductionSources(t)); err != nil {
		t.Fatal(err)
	}
	t.Log("ARTIFACT-BOUND-v1 PASS TestProducerGateStateSelectionArtifactBoundSourceGraph")
}

// The missing-definition mutation reconstructs the actual authored error in
// parser-owned source only. It is not reported as an executed pre-fix product.
func TestProducerGateStateSelectionArtifactBoundGraphRejectsMissingAuthority(t *testing.T) {
	sources := releaseArtifactBoundProductionSources(t)
	if err := verifyReleaseArtifactBoundSourceGraph(sources); err != nil {
		t.Fatal(err)
	}
	alias := "const maxReleaseMeasurementArtifactBytes = releaseMeasurementEnvelopeMaxArtifactSize"
	for _, replacement := range []string{"", "var maxReleaseMeasurementArtifactBytes = releaseMeasurementEnvelopeMaxArtifactSize", "const maxReleaseMeasurementArtifactBytes = 64 * 1024 * 1024", "const maxReleaseMeasurementArtifactBytes = unrelatedCeiling"} {
		changed := maps.Clone(sources)
		if strings.Count(changed["release_measurement_v2.go"], alias) != 1 {
			t.Fatal("missing-alias control lost its unique actual source precondition")
		}
		changed["release_measurement_v2.go"] = strings.Replace(changed["release_measurement_v2.go"], alias, replacement, 1)
		if err := verifyReleaseArtifactBoundSourceGraph(changed); err == nil {
			t.Fatalf("source graph admitted absent/mutable/detached ceiling: %q", replacement)
		}
	}
	for _, kind := range []string{"missing envelope", "raised envelope", "untracked consumer", "omitted guard"} {
		changed := maps.Clone(sources)
		switch kind {
		case "missing envelope":
			delete(changed, "release_measurement_envelope.go")
		case "raised envelope":
			before := "const releaseMeasurementEnvelopeMaxArtifactSize = 64 * 1024 * 1024"
			if strings.Count(changed["release_measurement_envelope.go"], before) != 1 {
				t.Fatal("raised-ceiling control lost the actual finite owner")
			}
			changed["release_measurement_envelope.go"] = strings.Replace(changed["release_measurement_envelope.go"], before, "const releaseMeasurementEnvelopeMaxArtifactSize = 128 * 1024 * 1024", 1)
		case "untracked consumer":
			changed["untracked_ceiling.go"] = "package validator\nfunc untrackedCeiling() int { return maxReleaseMeasurementArtifactBytes }\n"
		case "omitted guard":
			before := "options.MaxArtifactBytes > maxReleaseMeasurementArtifactBytes"
			if !strings.Contains(changed["release_measurement_v2.go"], before) {
				t.Fatal("omitted-guard control lost the actual admission comparison")
			}
			changed["release_measurement_v2.go"] = strings.Replace(changed["release_measurement_v2.go"], before, "false", 1)
		}
		if err := verifyReleaseArtifactBoundSourceGraph(changed); err == nil {
			t.Fatalf("source graph admitted %s", kind)
		}
	}
	t.Log("ARTIFACT-BOUND-v1 PASS TestProducerGateStateSelectionArtifactBoundGraphRejectsMissingAuthority")
}

// Existing selector alternatives retain the new behavior roots and this
// source guard in both modes; no prefix or earlier regression is removed.
func TestProducerGateStateSelectionCoversArtifactBoundRegressions(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "artifact-bound", []string{"../validator/release_measurement_v2_bound_test.go"})
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "artifact-bound-graph", []string{"release_gate_artifact_bound_test.go"})
	t.Log("ARTIFACT-BOUND-v1 PASS TestProducerGateStateSelectionCoversArtifactBoundRegressions")
}
