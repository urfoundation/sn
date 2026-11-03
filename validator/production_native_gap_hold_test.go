//go:build linux || darwin

// The hold's tests use the bridge and head admission fixtures, which share that
// constraint. The loop and the error graph are exercised directly.
package validator

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol"
)

// The real head admission, at a native epoch that does not follow its last
// fold, with no edge. Nothing a retry can do changes this answer.
func nativeGapHoldTestRefusal(t *testing.T) func() error {
	t.Helper()
	const last = uint64(26073)
	limits := headEMAStoreV2TestLimits()
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	alpha := protocol.Rational{Numerator: 1, Denominator: 4}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 145}
	if _, _, err := store.FoldForEpochV2(t.Context(), last, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha); err != nil {
		t.Fatal(err)
	}
	budget := releaseHeadV2Budget{limit: limits.MaxControlBytes}
	return func() error {
		return store.admitReleaseHeadV2Known(t.Context(), last+2, alpha, limits.MaxEntries, budget)
	}
}

// On 11-01 and 11-02 this refusal ended the process every ten failed attempts,
// 41 restarts in all, each losing about 14 minutes of trails to startup, and
// the restart could not change the answer. The loop now keeps running.
func TestProductionSteeringLoopSurvivesNativeGapRefusal(t *testing.T) {
	t.Parallel()
	refusal := nativeGapHoldTestRefusal(t)
	polls := 0
	progress := &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		polls++
		return refusal()
	}, func() bool { return polls < 3*releaseSteeringFailureLimit }, progress)
	if err != nil || polls != 3*releaseSteeringFailureLimit {
		t.Fatalf("a refusal no retry can clear ended the steering loop: %d polls: %v", polls, err)
	}
	if progress.value.Steering == nil || progress.value.Steering.Outcome != "hard_error" || progress.value.Steering.Current {
		t.Fatalf("a held gap must keep reading as a failure to the health exporter: %+v", progress.value.Steering)
	}
}

// The whole release keeps its other workers: trails, the settlement refresh
// and the audit stop only for an ordinary shutdown, never for this refusal.
func TestReleaseWorkersKeepRunningThroughNativeGapRefusal(t *testing.T) {
	t.Parallel()
	refusal := nativeGapHoldTestRefusal(t)
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	operations := releaseShutdownTestOperations(func() {})
	polls := 0
	operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
		return releaseShutdownSteererFunc(func(ctx context.Context) error {
			return runReleaseProductionSteeringLoopWithWait(ctx, func() error {
				polls++
				if polls == 3*releaseSteeringFailureLimit {
					cancel()
				}
				return refusal()
			}, func() bool { return ctx.Err() == nil }, nil)
		}), nil
	}
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	var limit *releaseSteeringFailureLimitError
	if errors.As(err, &limit) {
		t.Fatalf("the refusal reached the supervisor as a failure limit, which panics the process: %v", err)
	}
	if err != nil || polls < 3*releaseSteeringFailureLimit {
		t.Fatalf("release workers did not run until shutdown: %d polls: %v", polls, err)
	}
}

func nativeGapHoldTestProgress() *releaseProgress {
	return &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
}

func nativeGapHoldTestRefused() *productionNativeGapHold {
	return &productionNativeGapHold{lastEpoch: 26073, lastStatus: "applied", lastArtifactHash: "sha256:" + strings.Repeat("0f", 32), nativeEpoch: 26075}
}

// A completed refusal looks again at the slower pace. An attempt that failed
// first polls at the ordinary one: its cut may be pending, and new trails wait.
func TestProductionNativeGapHoldPace(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		pace int
	}{
		{"refused", nativeGapHoldTestRefused(), productionNativeGapHoldPolls},
		{"plain refusal", errReleaseHeadEMANativeGap, productionNativeGapHoldPolls},
		{"attempt failed first", nativeGapHoldTestRefused().attemptFailed(errors.New(`attempt upload response status is 403: "Validator reserved upload is unavailable or ambiguous."`)), 1},
		{"drain", nativeGapHoldTestRefused().attemptFailed(errAttemptCutPending), 1},
	} {
		polls, waits := 0, 0
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			polls++
			return test.err
		}, func() bool {
			waits++
			return polls < 3*releaseSteeringFailureLimit
		}, nativeGapHoldTestProgress())
		if err != nil || polls != 3*releaseSteeringFailureLimit {
			t.Fatalf("%s: the hold ended the loop: %d polls: %v", test.name, polls, err)
		}
		// The last poll's first wait returns false and ends the run.
		if want := (polls-1)*test.pace + 1; waits != want {
			t.Fatalf("%s: %d waits over %d polls, want %d", test.name, waits, polls, want)
		}
	}
}

// Failures before the refusal cannot end a process with nothing left to retry,
// and failures after it are ordinary failures again.
func TestProductionNativeGapHoldResetsAndKeepsTheBudget(t *testing.T) {
	t.Parallel()
	local := &os.PathError{Op: "read", Path: "synthetic-state", Err: os.ErrNotExist}
	polls := 0
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		polls++
		if polls == releaseSteeringFailureLimit {
			return nativeGapHoldTestRefused()
		}
		return local
	}, func() bool { return true }, nativeGapHoldTestProgress())
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || polls != 2*releaseSteeringFailureLimit-1 {
		t.Fatalf("the hold changed the budget of real failures: %d polls: %v", polls, err)
	}
}

// Only a graph that is wholly the refusal holds. A local file error stays hard,
// joined to it or inside its attempt, because a restart can matter there.
func TestProductionNativeGapHoldLeavesRealFailuresHard(t *testing.T) {
	t.Parallel()
	local := &os.PathError{Op: "read", Path: "synthetic-state", Err: os.ErrNotExist}
	for _, hard := range []error{
		errors.Join(errReleaseHeadEMANativeGap, local),
		errors.Join(nativeGapHoldTestRefused(), local),
		errors.Join(errReleaseHeadEMANativeGap, errors.New("independent cause")),
		nativeGapHoldTestRefused().attemptFailed(local),
		&TrailFatalError{Err: errReleaseHeadEMANativeGap},
	} {
		polls := 0
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			polls++
			return hard
		}, func() bool { return true }, nativeGapHoldTestProgress())
		var limit *releaseSteeringFailureLimitError
		if !errors.As(err, &limit) || polls != releaseSteeringFailureLimit {
			t.Fatalf("a hold concealed %v: %d polls: %v", hard, polls, err)
		}
	}
}

// Shutdown and the operation budget keep their meaning inside a hold.
func TestProductionNativeGapHoldKeepsCancellation(t *testing.T) {
	t.Parallel()
	hold := nativeGapHoldTestRefused()
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.Join(context.Canceled, context.DeadlineExceeded)} {
		if err := hold.attemptFailed(cause); err != cause {
			t.Fatalf("attempt cancellation became a hold: %v", err)
		}
	}
	var none *productionNativeGapHold
	ordinary := errors.New("ordinary attempt failure")
	if err := none.attemptFailed(ordinary); err != ordinary {
		t.Fatalf("an attempt without a gap changed its error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	polls := 0
	err := runReleaseProductionSteeringLoopWithWait(ctx, func() error {
		polls++
		cancel()
		return hold.attemptFailed(errors.New("failed while shutting down"))
	}, func() bool { return true }, nil)
	if polls != 1 || err == nil {
		t.Fatalf("a failure joined to shutdown vanished: %d polls: %v", polls, err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	polls = 0
	waits := 0
	err = runReleaseProductionSteeringLoopWithWait(ctx, func() error {
		polls++
		return hold
	}, func() bool {
		if waits++; waits == 3 {
			cancel()
			return false
		}
		return true
	}, nil)
	if polls != 1 || err != nil {
		t.Fatalf("shutdown during a held wait was not clean: %d polls: %v", polls, err)
	}
}

// Every health signal keeps reporting a failure, and the operator gets the
// values a bridge needs once per native epoch, without the private intent file.
func TestProductionNativeGapHoldTellsTheOperator(t *testing.T) {
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 64)}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	progress := nativeGapHoldTestProgress()
	first := nativeGapHoldTestRefused()
	later := nativeGapHoldTestRefused()
	later.nativeEpoch++
	failed := nativeGapHoldTestRefused()
	failed.lastStatus, failed.nativeEpoch = "failed", later.nativeEpoch+1
	sequence := []*productionNativeGapHold{first, first, later, failed}
	polls := 0
	err = runReleaseProductionSteeringLoopWithWait(ctx, func() error {
		polls++
		return sequence[polls-1]
	}, func() bool { return polls < len(sequence) }, progress)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	hash := first.lastArtifactHash
	for _, want := range []string{
		"validator steering hold: native epoch 26075 does not follow the applied intent at native epoch 26073 (" + hash + ") and no bridge edge admits it; trails and settlement keep running; steering resumes after a restart with the edge last_native_epoch=26073 last_artifact_hash=" + hash + " first_native_epoch=26076 or later\n",
		"validator steering hold: native epoch 26076 does not follow the applied intent at native epoch 26073 (" + hash + ") and no bridge edge admits it; trails and settlement keep running; steering resumes after a restart with the edge last_native_epoch=26073 last_artifact_hash=" + hash + " first_native_epoch=26077 or later\n",
		"validator steering hold: native epoch 26077 does not follow the failed intent at native epoch 26073 (" + hash + ") and no bridge edge admits it; trails and settlement keep running; a bridge edge needs an applied predecessor, so this one needs an operator's review\n",
		"validator diagnostic: domain=steering code=hard_error native_epoch=26075 epoch_known=true attempt=1 phase=unknown cause=hard_error",
		"validator diagnostic: domain=steering code=native_gap_hold native_epoch=26076 epoch_known=true attempt=0 phase=unknown cause=hard_error",
		"validator steering causes: code=hard_error attempt=1 leaves=1 distinct=1: 1x ",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	if count := strings.Count(text, "validator steering hold: native epoch 26075 "); count != 1 {
		t.Fatalf("the hold line repeated within its native epoch: %d times in %q", count, text)
	}
	if count := strings.Count(text, "code=hard_error native_epoch="); count != len(sequence) {
		t.Fatalf("each look must count toward SnValidatorSteeringFailing: %d in %q", count, text)
	}
	if value := progress.value.Steering; value == nil || value.Outcome != "hard_error" || value.Current || value.NativeEpoch != 26077 || !value.EpochKnown {
		t.Fatalf("progress lost the hold: %+v", value)
	}
}

// The same rule as compact head admission in every case that matters, and a
// bridge edge that admits the epoch keeps it out of the hold.
func TestNativeGapHoldRuleMatchesHeadAdmission(t *testing.T) {
	t.Parallel()
	const L, F = nativeGapBridgeTestLast, nativeGapBridgeTestFirst
	limits := headEMAStoreV2TestLimits()
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	if store.refusesNativeEpochGap(L + 9) {
		t.Fatal("a store with no fold has no gap")
	}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	if _, _, err := store.FoldForEpochV2(t.Context(), L, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha); err != nil {
		t.Fatal(err)
	}
	budget := releaseHeadV2Budget{limit: limits.MaxControlBytes}
	bridge := nativeGapBridgeTest()
	owner, err := bridge.matchHistory(t.Context(), []*SteeringIntent{nativeGapBridgeTestIntent(L, "applied")})
	if err != nil {
		t.Fatal(err)
	}
	for _, bridged := range []*releaseNativeGapBridgeV2{nil, owner} {
		store.v2.nativeGapBridge = bridged
		for _, epoch := range []uint64{L - 1, L, L + 1, L + 2, F - 1, F, F + 7} {
			admission := store.admitReleaseHeadV2Known(t.Context(), epoch, alpha, limits.MaxEntries, budget)
			refused := admission != nil && errors.Is(admission, errReleaseHeadEMANativeGap)
			if refused != store.refusesNativeEpochGap(epoch) {
				t.Fatalf("bridged=%t epoch %d: admission %v, rule %t", bridged != nil, epoch, admission, !refused)
			}
		}
	}
	store.v2.nativeGapBridge = owner
	if store.refusesNativeEpochGap(F) || !store.refusesNativeEpochGap(F-1) {
		t.Fatal("the bridge's first native epoch moved the hold")
	}
}

// A hold belongs to one native epoch and one predecessor. Any change runs a
// whole attempt again, and a runtime without a refusal holds nothing.
func TestHeldNativeGapBelongsToOneEpochAndPredecessor(t *testing.T) {
	t.Parallel()
	runtime := &releaseRuntimeV2{}
	current := &SteeringIntent{SubnetEpoch: 26073, MeasurementArtifactHash: nativeGapHoldTestRefused().lastArtifactHash}
	if runtime.heldNativeGap(current, 26075) != nil {
		t.Fatal("no refusal is stored yet")
	}
	held := nativeGapHoldTestRefused()
	runtime.nativeGapHeld.Store(held)
	if runtime.heldNativeGap(current, 26075) != held {
		t.Fatal("the same epoch and predecessor must not redo the attempt")
	}
	other := *current
	other.MeasurementArtifactHash = "sha256:" + strings.Repeat("1e", 32)
	moved := *current
	moved.SubnetEpoch++
	for name, got := range map[string]*productionNativeGapHold{
		"later epoch":       runtime.heldNativeGap(current, 26076),
		"other predecessor": runtime.heldNativeGap(&other, 26075),
		"moved intent":      runtime.heldNativeGap(&moved, 26075),
		"no intent":         runtime.heldNativeGap(nil, 26075),
		"no runtime":        (*releaseRuntimeV2)(nil).heldNativeGap(current, 26075),
	} {
		if got != nil {
			t.Fatalf("%s reused a hold", name)
		}
	}
}

// The order that makes the hold cheap and keeps its cut: the idle check first,
// then the stored refusal before any replay, then the cut, then head admission.
func TestSubmitOnceV2HoldsBeforeItReplaysAndAfterItCuts(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "release_submit_v2.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "submitOnceV2" {
			body = function.Body
		}
	}
	if body == nil {
		t.Fatal("submitOnceV2 is gone")
	}
	var calls []string
	ast.Inspect(body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
				calls = append(calls, selector.Sel.Name)
			}
		}
		return true
	})
	position := func(name string, after int) int {
		for index := after + 1; index < len(calls); index++ {
			if calls[index] == name {
				return index
			}
		}
		t.Fatalf("submitOnceV2 does not call %s after position %d: %v", name, after, calls)
		return -1
	}
	at := position("requireFirstEpoch", -1)
	at = position("requireFirstEpoch", at)
	at = position("heldNativeGap", at)
	at = position("restoreHeadEmaV2", at)
	at = position("nativeGapHoldFor", at)
	at = position("collect", at)
	at = position("gatherHeadV2", at)
	position("beginV2", at)
}
