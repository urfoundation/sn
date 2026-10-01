//go:build linux || darwin

package validator

// Count limits must also bound bytes consumed by the shared statistics parser.
// The operation-local observation marks its actual entry, not an allocation or
// timing proxy; genuine M8 stream fixtures retain full policy/signature replay.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Malformed input must fail before any public object or scratch acquisition.
// The existing real sealer fixture is untouched and each call owns its options.
func observeAttemptCutV2StatsAdmission(t *testing.T, fixture *attemptCutV2StatsTestFixture, measurement ReleaseStatsMeasurement, edit func(*AttemptCutV2StatsOptions)) int {
	t.Helper()
	options := fixture.options(t, measurement)
	options.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
		t.Fatal("inadmissible statistics reached public metadata")
		return nil, nil
	}
	options.Replay.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) {
		t.Fatal("inadmissible statistics reached public data")
		return nil, nil
	}
	if edit != nil {
		edit(&options)
	}
	entries := 0
	verified, replayed, err := verifyReleaseStatsMeasurementWithAttemptCutV2AndObserver(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options, func() { entries++ })
	if err == nil || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("inadmissible statistics returned score authority: %v", err)
	}
	if _, err := os.Lstat(options.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inadmissible statistics touched scratch: %v", err)
	}
	return entries
}

// The existing UUID parser's error embeds arbitrary-length invalid IDs. The
// compact admission must reject all noncanonical widths before that allocation.
func TestAttemptCutV2StatsAdmitsClientWidthBeforeGenericVerification(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	id := fixture.measurement.Providers[0].ClientID
	if len(id) != 36 {
		t.Fatal("real canonical provider ID is not 36 bytes")
	}
	var observed []int
	for _, value := range []string{"", strings.ReplaceAll(id, "-", ""), id[:35], id + "0", strings.Repeat("A", 64*1024)} {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		measurement.Providers[0].ClientID = value
		observed = append(observed, observeAttemptCutV2StatsAdmission(t, fixture, measurement, nil))
	}
	if !reflect.DeepEqual(observed, make([]int, len(observed))) {
		t.Fatalf("provider ID width reached generic statistics verification before admission: got %v, want all zero", observed)
	}
}

// One long mixed-case hash enters strings.ToLower in the old generic path;
// limiting the number of hashes alone cannot bound that actual byte work.
func TestAttemptCutV2StatsAdmitsHashWidthBeforeGenericVerification(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	providerIndex := -1
	for index, provider := range fixture.measurement.Providers {
		if len(provider.EgressIPHashHexes) != 0 {
			providerIndex = index
			break
		}
	}
	if providerIndex < 0 {
		t.Fatal("real complete M8 trail has no routed egress")
	}
	hash := fixture.measurement.Providers[providerIndex].EgressIPHashHexes[0]
	if len(hash) != 66 {
		t.Fatal("real canonical egress hash is not 66 bytes")
	}
	var observed []int
	for _, value := range []string{"", hash[:65], hash + "0", strings.Repeat("Ab", 32*1024)} {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		measurement.Providers[providerIndex].EgressIPHashHexes[0] = value
		observed = append(observed, observeAttemptCutV2StatsAdmission(t, fixture, measurement, nil))
	}
	if !reflect.DeepEqual(observed, make([]int, len(observed))) {
		t.Fatalf("egress hash width reached generic statistics verification before admission: got %v, want all zero", observed)
	}
}

// Histogram shape has the same finite admission boundary even though the
// generic verifier already rejects its wrong length before score arithmetic.
func TestAttemptCutV2StatsAdmitsHistogramWidthBeforeGenericVerification(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	var observed []int
	for _, size := range []int{0, statsLatencyBuckets - 1, statsLatencyBuckets + 1, 64 * 1024} {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		measurement.Providers[0].LatencyBuckets = make([]uint64, size)
		observed = append(observed, observeAttemptCutV2StatsAdmission(t, fixture, measurement, nil))
	}
	if !reflect.DeepEqual(observed, make([]int, len(observed))) {
		t.Fatalf("latency histogram width reached generic statistics verification before admission: got %v, want all zero", observed)
	}
}

// Fixed shape is admission, not an alternate statistics verdict. The real
// complete+failed replay reaches the shared verifier once and both entrypoints
// match the independently signed v1 oracle without changing any input bytes.
func TestAttemptCutV2StatsAdmissionRetainsCanonicalPublicReplay(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 1)
	before := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
	entries := 0
	verified, replayed, err := verifyReleaseStatsMeasurementWithAttemptCutV2AndObserver(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, fixture.measurement), func() { entries++ })
	if err != nil || entries != 1 || !reflect.DeepEqual(verified, fixture.legacy) || replayed.CompleteCount != 1 || replayed.FailedCount != 1 || replayed.Records.ItemCount != 10 {
		t.Fatalf("canonical observed replay differs: entries=%d result=%+v error=%v", entries, replayed, err)
	}
	public, publicReplay, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, fixture.measurement))
	if err != nil || !reflect.DeepEqual(public, verified) || publicReplay != replayed || !reflect.DeepEqual(fixture.measurement, before) {
		t.Fatalf("public replay bypassed or changed the observed workflow: %v", err)
	}
}

// Correct fixed widths do not excuse invalid content, duplicate providers,
// zero hashes or inconsistent counts: all still reach the original verifier.
func TestAttemptCutV2StatsAdmissionRetainsGenericSemanticChecks(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	providerIndex := -1
	for index, provider := range fixture.measurement.Providers {
		if len(provider.EgressIPHashHexes) != 0 {
			providerIndex = index
			break
		}
	}
	if providerIndex < 0 {
		t.Fatal("real complete M8 trail has no routed egress")
	}
	edits := []func(*ReleaseStatsMeasurement){
		func(value *ReleaseStatsMeasurement) {
			value.Providers[0].ClientID = "z" + value.Providers[0].ClientID[1:]
		},
		func(value *ReleaseStatsMeasurement) {
			value.Providers[providerIndex].EgressIPHashHexes[0] = "0xA" + strings.Repeat("0", 63)
		},
		func(value *ReleaseStatsMeasurement) {
			value.Providers[providerIndex].EgressIPHashHexes[0] = "0xg" + strings.Repeat("0", 63)
		},
		func(value *ReleaseStatsMeasurement) {
			value.Providers[providerIndex].EgressIPHashHexes[0] = "0x" + strings.Repeat("0", 64)
		},
		func(value *ReleaseStatsMeasurement) { value.Providers[0].LatencyBuckets[0]++ },
		func(value *ReleaseStatsMeasurement) { value.Providers = append(value.Providers, value.Providers[0]) },
	}
	for index, edit := range edits {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		edit(&measurement)
		if entries := observeAttemptCutV2StatsAdmission(t, fixture, measurement, nil); entries != 1 {
			t.Fatalf("bounded invalid content %d bypassed original verification: entries=%d", index, entries)
		}
	}
}

// Existing independent count/configuration and replay-ownership admission
// stays ahead of the observed generic boundary as shape checks are added.
func TestAttemptCutV2StatsAdmissionPreservesEarlierRefusals(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	for index, edit := range []func(*AttemptCutV2StatsOptions){
		func(options *AttemptCutV2StatsOptions) { options.MaxProviders = 0 },
		func(options *AttemptCutV2StatsOptions) { options.MaxProviders-- },
		func(options *AttemptCutV2StatsOptions) { options.MaxEgressHashes = 0 },
		func(options *AttemptCutV2StatsOptions) { options.MaxEgressHashes-- },
		func(options *AttemptCutV2StatsOptions) { options.ExpectedConfig.LatRefMillis++ },
		func(options *AttemptCutV2StatsOptions) {
			options.Replay.VisitRecord = func(AttemptRecord) error { t.Fatal("foreign visitor was called"); return nil }
		},
	} {
		if entries := observeAttemptCutV2StatsAdmission(t, fixture, fixture.measurement, edit); entries != 0 {
			t.Fatalf("earlier admission %d reached the generic verifier: entries=%d", index, entries)
		}
	}
	for _, transition := range []bool{false, true} {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		if transition {
			measurement.SettlementTransition = &AttemptSettlementTransition{}
		} else {
			measurement.AttemptCut = &AttemptLedgerCut{}
		}
		if entries := observeAttemptCutV2StatsAdmission(t, fixture, measurement, nil); entries != 0 {
			t.Fatalf("competing legacy authority reached generic verification: entries=%d", entries)
		}
	}
}

// The observation cannot drift into a test-only alternate implementation.
// Production forwards all original arguments plus nil into the exact helper,
// which forwards the observer to shared admission immediately before its one
// real generic call. The workflow still performs its one full policy replay.
func TestAttemptCutV2StatsPublicPathRetainsObservedGenericBoundary(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "attempt_cut_v2_stats.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var public, helper, projection *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch function.Name.Name {
		case "VerifyReleaseStatsMeasurementWithAttemptCutV2":
			public = function
		case "verifyReleaseStatsMeasurementWithAttemptCutV2AndObserver":
			helper = function
		case "newAttemptCutV2StatsProjection":
			projection = function
		}
	}
	if public == nil || public.Body == nil || len(public.Body.List) != 1 || helper == nil || helper.Body == nil || projection == nil || projection.Body == nil {
		t.Fatal("public statistics replay no longer delegates to its observed workflow")
	}
	returned, ok := public.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(returned.Results) != 1 {
		t.Fatal("public statistics replay does not return the observed result")
	}
	call, ok := returned.Results[0].(*ast.CallExpr)
	if !ok {
		t.Fatal("public statistics result is not its helper call")
	}
	target, ok := call.Fun.(*ast.Ident)
	if !ok || target.Name != helper.Name.Name {
		t.Fatal("public statistics replay routes outside the observed workflow")
	}
	expected := []string{"ctx", "measurement", "cut", "expected", "policy", "bounds", "options", "nil"}
	if len(call.Args) != len(expected) {
		t.Fatal("public statistics replay changed its delegated argument count")
	}
	for index, name := range expected {
		argument, ok := call.Args[index].(*ast.Ident)
		if !ok || argument.Name != name {
			t.Fatalf("public statistics replay changed delegated argument %d", index)
		}
	}
	genericCalls, boundaryPairs, policyReplayCalls, statelessReplayCalls, projectionCalls := 0, 0, 0, 0, 0
	for _, body := range []*ast.BlockStmt{helper.Body, projection.Body} {
		ast.Inspect(body, func(node ast.Node) bool {
			candidate, ok := node.(*ast.CallExpr)
			if ok {
				if name, ok := candidate.Fun.(*ast.Ident); ok {
					switch name.Name {
					case "newAttemptCutV2StatsProjection":
						projectionCalls++
						wanted := []string{"ctx", "measurement", "expected", "policy", "options", "observeStatsVerifier"}
						if len(candidate.Args) != len(wanted) {
							t.Fatal("shared statistics admission changed its argument count")
						}
						for index, name := range wanted {
							argument, ok := candidate.Args[index].(*ast.Ident)
							if !ok || argument.Name != name {
								t.Fatalf("shared admission lost original argument %d", index)
							}
						}
					case "VerifyReleaseStatsMeasurement":
						genericCalls++
					case "ReplayAttemptCutV2WithPolicy":
						policyReplayCalls++
					case "ReplayAttemptCutV2":
						statelessReplayCalls++
					}
				}
			}
			return true
		})
	}
	for index, statement := range projection.Body.List {
		assignment, ok := statement.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 || index == 0 {
			continue
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "VerifyReleaseStatsMeasurement" {
			continue
		}
		observed, ok := projection.Body.List[index-1].(*ast.IfStmt)
		if !ok || len(observed.Body.List) != 1 {
			t.Fatal("actual generic call lost its immediately preceding observer")
		}
		expression, ok := observed.Body.List[0].(*ast.ExprStmt)
		if !ok {
			t.Fatal("observed boundary does not invoke its observer")
		}
		invocation, ok := expression.X.(*ast.CallExpr)
		if !ok {
			t.Fatal("observed boundary is not a call")
		}
		observer, ok := invocation.Fun.(*ast.Ident)
		if !ok || observer.Name != "observeStatsVerifier" || len(invocation.Args) != 0 {
			t.Fatal("observed boundary invokes a different callback")
		}
		boundaryPairs++
	}
	if genericCalls != 1 || boundaryPairs != 1 || projectionCalls != 1 {
		t.Fatalf("actual generic boundary count changed: calls=%d pairs=%d projections=%d", genericCalls, boundaryPairs, projectionCalls)
	}
	if policyReplayCalls != 1 || statelessReplayCalls != 0 {
		t.Fatalf("statistics replay changed authenticated policy admission: policy=%d stateless=%d", policyReplayCalls, statelessReplayCalls)
	}
}
