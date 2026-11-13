//go:build linux || darwin

// Real protected publication and fetch-back retain their preparation stage
// while the production loop preserves each existing retry or hard verdict.
package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// A barrier expires the caller's deadline after the real server accepts its
// request. WithoutCancel hides an ancestor's cancellation identity while
// retaining its diagnostic values; healthy siblings keep that ancestor live.
type preparationDiagnosticDeadlineContext struct {
	context.Context
	done     chan struct{}
	deadline time.Time
	expired  atomic.Bool
}

// The finite deadline is independent from the explicit expiry barrier.
func (self *preparationDiagnosticDeadlineContext) Deadline() (time.Time, bool) {
	return self.deadline, true
}

// Only the test's owned deadline closes this channel.
func (self *preparationDiagnosticDeadlineContext) Done() <-chan struct{} { return self.done }

// Children see the same exact standard deadline identity as their owner.
func (self *preparationDiagnosticDeadlineContext) Err() error {
	if self.expired.Load() {
		return context.DeadlineExceeded
	}
	return nil
}

// One expiry joins all real HTTP and collection owners without a timer race.
func (self *preparationDiagnosticDeadlineContext) expire() {
	if self.expired.CompareAndSwap(false, true) {
		close(self.done)
	}
}

// A genuine nonempty cut reaches protected writes and metadata fetch-back.
// Its canceled child cannot stop a sibling or erase immutable staged bytes.
func TestPreparationDiagnosticsCollectRetainsFetchBackStage(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	fixture.startup.trail(t, 0)
	fixture.startup.trail(t, 1)
	sink := &preparationDiagnosticsHttpSink{records: make(chan []byte, 1024)}
	base, diagnostics, err := newReleaseDiagnostics(t.Context(), sink, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostics.close()
	operation := &preparationDiagnosticDeadlineContext{Context: context.WithoutCancel(base), done: make(chan struct{}), deadline: time.Now().Add(time.Hour)}
	defer operation.expire()
	entered, handlerJoined := make(chan uint64, 2), make(chan uint64, 2)
	var stateLock sync.Mutex
	sourceByHash, blockedSources := map[string]uint64{}, map[uint64]bool{}
	store := fixture.stores[0]
	store.stateLock.Lock()
	initialObjects := len(store.objects)
	initialPosts := len(store.postsBySource)
	store.requestHook = func(_ http.ResponseWriter, request *http.Request) bool {
		if request.URL.Query().Get("kind") != "metadata" {
			return false
		}
		hash := request.URL.Query().Get("hash")
		if request.Method == http.MethodPost {
			header, err := protocol.VerifyValidatorAttemptUploadHeader(request.Header.Get(protocol.ValidatorAttemptUploadHeader))
			if err == nil {
				for _, input := range fixture.startup.inputs {
					if header.VPK == input.Context.Activation.VPK {
						stateLock.Lock()
						sourceByHash[hash] = input.Config.NoID
						stateLock.Unlock()
					}
				}
			}
			return false
		}
		if request.Method != http.MethodGet {
			return false
		}
		stateLock.Lock()
		source := sourceByHash[hash]
		blocked := source != 0 && !blockedSources[source]
		if blocked {
			blockedSources[source] = true
		}
		stateLock.Unlock()
		if !blocked {
			return false
		}
		entered <- source
		defer func() { handlerJoined <- source }()
		<-request.Context().Done()
		return true
	}
	store.stateLock.Unlock()
	boundary := fixture.startup.boundary
	hash, err := parseReleaseHex32("synthetic preparation Evm hash", boundary.EVMBlockHash, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(boundary.SettlementEpoch), BlockNumber: boundary.EVMBlock, BlockHash: hash}
	native := fixture.startup.nativeFixture
	completed := make(chan error, 1)
	go func() {
		_, _, err := fixture.runtime.collect(operation, steerer, nil, snapshot, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
		completed <- err
	}()
	first, second := <-entered, <-entered
	if first == second {
		t.Fatal("fixture did not hold two distinct protected source owners")
	}
	var output strings.Builder
	// Both operators are held before any journal can publish. A FIFO marker
	// drains prior diagnostic traffic without assuming delivery of burst records.
	flush := func() {
		const barrier = "synthetic collect diagnostic delivery barrier\n"
		for !diagnostics.exporter.Offer("steering", []byte(barrier)) {
			output.Write(<-sink.records)
		}
		for {
			raw := <-sink.records
			if string(raw) == barrier {
				return
			}
			output.Write(raw)
		}
	}
	flush()
	// The shared parent and another destination still serve a real read while
	// this operator's fetch-back holds at its server barrier.
	request, err := http.NewRequestWithContext(base, http.MethodGet, fixture.origins[1]+"/verify/keys", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := doReleaseApiHttpRequest(&http.Client{Transport: releaseAPIHTTPTransport}, request)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	if err := errors.Join(readErr, response.Body.Close()); err != nil || response.StatusCode != http.StatusOK || base.Err() != nil {
		t.Fatalf("blocked child stopped a healthy sibling: %v", err)
	}
	operation.expire()
	collectErr := <-completed
	<-handlerJoined
	<-handlerJoined
	if !errors.Is(collectErr, context.DeadlineExceeded) || base.Err() != nil {
		t.Fatalf("actual deadline or shared lifetime changed: %v", collectErr)
	}
	// Use the existing production projection and loop. The diagnostic change
	// adds no signing authority or alternate retry decision to this fixture.
	projection := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	observed := observeProductionSteeringRead(collectErr, evidenceReadContextError(operation) == context.DeadlineExceeded)
	wait := projection.productionRetainedReadFailure(operation, productionReadPreparation, nil, observed)
	flush()
	progress, _ := newReleaseProgressTest(t)
	if retryableProductionSteeringRead(observed) {
		t.Fatal("existing lower error-graph refusal was incorrectly made retryable")
	}
	if err := runReleaseProductionSteeringLoopWithWait(base, func() error { return wait }, func() bool { return false }, progress); err == nil || !errors.Is(err, context.DeadlineExceeded) || progress.value.Steering == nil || progress.value.Steering.Outcome != "hard_error" {
		t.Fatalf("diagnostic collection changed the existing hard refusal: %v", err)
	}
	flush()
	store.stateLock.Lock()
	store.requestHook = nil
	newPosts := len(store.postsBySource)
	staged := make(map[string][]byte, len(store.objects))
	for key, raw := range store.objects {
		staged[key] = bytes.Clone(raw)
	}
	store.stateLock.Unlock()
	if len(staged) <= initialObjects || newPosts <= initialPosts {
		t.Fatal("fixture did not reach genuine protected publication")
	}
	inputs, options, err := fixture.runtime.collect(base, steerer, nil, snapshot, 1, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
	if err != nil || len(inputs) != 2 || len(options.Operators) != 2 {
		t.Fatalf("fresh owner could not finish exact retained work: inputs=%d operators=%d error=%v", len(inputs), len(options.Operators), err)
	}
	store.stateLock.Lock()
	for key, raw := range staged {
		if !bytes.Equal(raw, store.objects[key]) {
			t.Error("retry replaced immutable published bytes", key)
		}
	}
	store.stateLock.Unlock()
	if current, err := steerer.intents.Current(); err != nil || current != nil {
		t.Fatalf("diagnostic collection created a native intent: %v / %v", current, err)
	}
	if err := diagnostics.close(); err != nil {
		t.Fatal(err)
	}
	for len(sink.records) != 0 {
		output.Write(<-sink.records)
	}
	text := output.String()
	for _, expected := range []string{
		"stage=http_open event=failed",
		"cancel_cause=deadline",
		"code=hard_error",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("broad outcome projection lost actual preparation boundary %q in:\n%s", expected, text)
		}
	}
}

// A deadline-only real read remains the existing preparation wait, with its
// exact request cause still reachable after owner and loop classification.
func TestPreparationDiagnosticsDeadlineProjectionKeepsActualCause(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	fixture := newPreparationDiagnosticsHttpFixture(t, func(_ http.ResponseWriter, request *http.Request) {
		close(entered)
		<-request.Context().Done()
		close(stopped)
	})
	operation := &preparationDiagnosticDeadlineContext{Context: context.WithoutCancel(fixture.ctx), done: make(chan struct{}), deadline: time.Now().Add(time.Hour)}
	defer operation.expire()
	steerer := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	var original error
	completed := make(chan error, 1)
	go func() {
		completed <- steerer.productionRead(operation, productionReadPreparation, nil, func(ctx context.Context) error {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.endpoint.URL+"/blocked", nil)
			if err != nil {
				return err
			}
			_, original = doReleaseApiHttpRequest(fixture.client, request)
			return original
		})
	}()
	<-entered
	operation.expire()
	result := <-completed
	<-stopped
	wait := releaseErrorMarker[*productionSteeringReadWait](result)
	if wait == nil || wait.phase != productionReadPreparation || original == nil || !errors.Is(result, original) || !errors.Is(result, context.DeadlineExceeded) || fixture.ctx.Err() != nil {
		t.Fatalf("real preparation owner lost original deadline cause: %v", result)
	}
	fixture.requireStage(t, "http_open", 1)
	progress, _ := newReleaseProgressTest(t)
	if err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { return result }, func() bool { return false }, progress); err != nil || progress.value.Steering == nil || progress.value.Steering.Outcome != "read_wait" || progress.value.Steering.Current {
		t.Fatalf("deadline-only read changed its production outcome: %v", err)
	}
}
