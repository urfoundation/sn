//go:build linux || darwin

package validator

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/protocol/headscore"
	"github.com/urfoundation/sn/v2026/protocol/headsummary"
)

// A deadline only for detecting a regression that blocks; no assertion
// depends on how long anything takes.
const headSummaryTestDeadline = 2 * time.Minute

const headSummaryTestSession = "validator-session-jwt"

func headSummaryTestKeypair(t *testing.T) *crv4.Keypair {
	t.Helper()
	keypair, err := crv4.KeypairFromSeed([32]byte{0x48, 0x53})
	if err != nil {
		t.Fatal(err)
	}
	return keypair
}

func headSummaryTestHash(seed byte) string {
	var value [32]byte
	for index := range value {
		value[index] = seed + byte(index)
	}
	return headsummary.Hex32(value)
}

// One tempo's real head decision from the validator's own arithmetic: UID 7
// has two live generations, UIDs 3 and 12 tie, UID 40 is live with no hops,
// and two seats leave UID 12 and UID 5 unselected.
func headSummaryTestIntent(t *testing.T, epoch uint64) (*SteeringIntent, []HeadEMAMeasurement) {
	t.Helper()
	identity := func(fleet byte, generation uint64, uid uint16) FleetScoreKey {
		return FleetScoreKey{FleetID: [32]byte{fleet, 0xf1}, Hotkey: [32]byte{byte(uid), 0x4b}, Generation: generation, UID: uid}
	}
	shared := [32]byte{0xee}
	census := map[FleetScoreKey]map[[32]byte]bool{
		identity(1, 1, 7):  {shared: true, {1}: true},
		identity(1, 2, 7):  {{2}: true},
		identity(2, 1, 3):  {{3}: true, {4}: true},
		identity(3, 1, 12): {{5}: true, {6}: true},
		identity(4, 1, 5):  {shared: true},
		identity(5, 1, 40): {},
	}
	store := &HeadEMAStore{values: map[string]headEMAEntry{}}
	out, records, err := store.foldWithLock(releaseRawHeadScores(census), protocol.Rational{Numerator: 1, Denominator: 4})
	if err != nil {
		t.Fatal(err)
	}
	eligible := make([]ExactWeightInput, 0, len(out))
	for uid, score := range out {
		eligible = append(eligible, ExactWeightInput{UID: uid, Score: score})
	}
	selection, err := selectHeadFleets(eligible, 2)
	if err != nil {
		t.Fatal(err)
	}
	ranked := append(append([]ExactWeightInput(nil), selection.Selected...), selection.Rejected...)
	scores := make([]*big.Rat, len(ranked))
	for index, input := range ranked {
		scores[index] = input.Score
	}
	encoded, err := rationalJSON(scores)
	if err != nil {
		t.Fatal(err)
	}
	intent := &SteeringIntent{
		ValidatorID: 1, Netuid: 25, SubnetEpoch: epoch, NativeSnapshotBlock: 6_000_000 + epoch, NativeSnapshotHash: headSummaryTestHash(0x10),
		EVMSnapshotBlock: 5_000_000 + epoch, EVMSnapshotHash: headSummaryTestHash(0x20), SettlementEpoch: 7, PolicyHash: headSummaryTestHash(0x30),
		MeasurementArtifactHash: "sha256:" + headSummaryTestHash(0x40)[2:], SelfUID: 1,
		EligibleHeadUIDs: headSelectionUIDs(ranked), EligibleHeadScores: encoded,
		SelectedHeadUIDs: headSelectionUIDs(selection.Selected), RejectedHeadUIDs: headSelectionUIDs(selection.Rejected),
	}
	if !slices.Equal(intent.EligibleHeadUIDs, []uint16{7, 3, 12, 5}) || !slices.Equal(intent.SelectedHeadUIDs, []uint16{7, 3}) {
		t.Fatalf("test head decision changed: eligible %v selected %v", intent.EligibleHeadUIDs, intent.SelectedHeadUIDs)
	}
	return intent, records
}

// The operator side of a test: every request, and how to answer it.
type headSummaryTestOperator struct {
	server   *httptest.Server
	stateMu  sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	answer   func(http.ResponseWriter, *http.Request)
	entered  chan struct{}
}

func newHeadSummaryTestOperator(t *testing.T, answer func(http.ResponseWriter, *http.Request)) *headSummaryTestOperator {
	t.Helper()
	self := &headSummaryTestOperator{answer: answer, entered: make(chan struct{}, 64)}
	self.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		self.stateMu.Lock()
		self.requests = append(self.requests, request.Clone(context.Background()))
		self.bodies = append(self.bodies, body)
		self.stateMu.Unlock()
		select {
		case self.entered <- struct{}{}:
		default:
		}
		if self.answer != nil {
			self.answer(writer, request)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(self.server.Close)
	return self
}

func (self *headSummaryTestOperator) count() int {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	return len(self.requests)
}

func (self *headSummaryTestOperator) request(index int) *http.Request {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	return self.requests[index]
}

// waitEntered waits for the next request to reach the operator.
func (self *headSummaryTestOperator) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-self.entered:
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("no request reached the operator")
	}
}

func (self *headSummaryTestOperator) body(index int) []byte {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	return self.bodies[index]
}

// A test uploader: real signing and HTTP, an injected wait that records the
// backoff instead of sleeping, and a channel of finished uploads.
type headSummaryTestUploader struct {
	*headSummaryUploader
	waitsMu  sync.Mutex
	waits    []time.Duration
	finished chan error
	builds   chan headSummaryTestBuild
}

type headSummaryTestBuild struct {
	epoch uint64
	err   error
}

func startHeadSummaryTestUploader(t *testing.T, keypair *crv4.Keypair, endpoints []string, credential func() string, hooks headSummaryHooks) *headSummaryTestUploader {
	t.Helper()
	self := &headSummaryTestUploader{finished: make(chan error, 256), builds: make(chan headSummaryTestBuild, 256)}
	if hooks.wait == nil {
		hooks.wait = func(ctx context.Context, delay time.Duration) error {
			self.waitsMu.Lock()
			self.waits = append(self.waits, delay)
			self.waitsMu.Unlock()
			return ctx.Err()
		}
	}
	hooks.afterUpload = func(_, _ uint64, err error) { self.finished <- err }
	hooks.afterBuild = func(epoch uint64, err error) { self.builds <- headSummaryTestBuild{epoch: epoch, err: err} }
	targets := make([]*headSummaryTarget, len(endpoints))
	for index, endpoint := range endpoints {
		targets[index] = &headSummaryTarget{noID: uint64(index) + 9, endpoint: endpoint + headsummary.Path, credential: credential}
	}
	self.headSummaryUploader = startHeadSummaryUploader(t.Context(), keypair.PublicKey(), keypair.Sign, 1, 2, targets, hooks)
	t.Cleanup(self.close)
	return self
}

func (self *headSummaryTestUploader) next(t *testing.T) error {
	t.Helper()
	select {
	case err := <-self.finished:
		return err
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("no upload finished")
		return nil
	}
}

func (self *headSummaryTestUploader) nextBuild(t *testing.T) headSummaryTestBuild {
	t.Helper()
	select {
	case build := <-self.builds:
		return build
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("no build finished")
		return headSummaryTestBuild{}
	}
}

func (self *headSummaryTestUploader) recordedWaits() []time.Duration {
	self.waitsMu.Lock()
	defer self.waitsMu.Unlock()
	return slices.Clone(self.waits)
}

func headSummaryTestSessionGetter() string { return headSummaryTestSession }

// A begun intent's head result reaches the operator as a canonical summary
// signed by the validator hotkey, with the exact scores, ranks, fleets and
// selection of the intent, over the operator session.
func TestHeadSummaryUploadDeliversTheSignedHeadResult(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	operator := newHeadSummaryTestOperator(t, nil)
	uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{})
	intent, head := headSummaryTestIntent(t, 51234)
	(&ReleaseSteerer{headSummary: uploader.headSummaryUploader}).offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
	if err := uploader.next(t); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	request := operator.request(0)
	if request.Method != http.MethodPost || request.URL.Path != "/sn/head-summary" || request.URL.RawQuery != "" ||
		request.Header.Get("Authorization") != "Bearer "+headSummaryTestSession || request.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request %s %s headers %v", request.Method, request.URL, request.Header)
	}
	summary, err := headsummary.Decode(operator.body(0))
	if err != nil {
		t.Fatalf("operator cannot decode the summary: %v", err)
	}
	if summary.ValidatorHotkey != headsummary.Hex32(keypair.PublicKey()) || summary.ValidatorUID != 1 || summary.Netuid != 25 || summary.NativeEpoch != 51234 ||
		summary.NativeSnapshotBlock != intent.NativeSnapshotBlock || summary.EVMSnapshotBlock != intent.EVMSnapshotBlock || summary.NativeSnapshotHash != intent.NativeSnapshotHash ||
		summary.EVMSnapshotHash != intent.EVMSnapshotHash || summary.SettlementEpoch != 7 || summary.PolicyID != 1 || summary.PolicyHash != intent.PolicyHash ||
		summary.MeasurementArtifactHash != intent.MeasurementArtifactHash || summary.MaximumHeadFleets != 2 {
		t.Fatalf("summary identity differs: %+v", summary)
	}
	if !slices.Equal(summary.SelectedUIDs, intent.SelectedHeadUIDs) || !slices.Equal(summary.RejectedUIDs, intent.RejectedHeadUIDs) || len(summary.Entries) != 4 {
		t.Fatalf("selection differs: %v %v", summary.SelectedUIDs, summary.RejectedUIDs)
	}
	for index, entry := range summary.Entries {
		if entry.Rank != uint32(index)+1 || entry.UID != intent.EligibleHeadUIDs[index] || entry.Score != intent.EligibleHeadScores[index] || entry.Selected != (index < 2) ||
			entry.Hotkey != headsummary.Hex32([32]byte{byte(entry.UID), 0x4b}) {
			t.Fatalf("entry %d differs: %+v", index, entry)
		}
	}
	// UID 7's score is its two live generations; UID 40, with no hops, is
	// not eligible.
	if fleets := summary.Entries[0].Fleets; len(fleets) != 2 || fleets[0].Generation != 1 || fleets[1].Generation != 2 || fleets[0].FleetID != headsummary.Hex32([32]byte{1, 0xf1}) {
		t.Fatalf("UID 7 fleets %+v", summary.Entries[0].Fleets)
	}
	if uploader.counters.offered.Load() != 1 || uploader.counters.built.Load() != 1 || uploader.counters.uploaded.Load() != 1 || uploader.counters.attempts.Load() != 1 || uploader.counters.failed.Load() != 0 {
		t.Fatal("counters differ")
	}
}

// offerHeadSummary only reads its arguments, and keeps its own copies: what
// steering does with them afterwards cannot change the uploaded summary.
func TestHeadSummaryOfferNeitherChangesNorRetainsItsInputs(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	operator := newHeadSummaryTestOperator(t, nil)
	release := make(chan struct{})
	uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{beforeBuild: func() { <-release }})
	intent, head := headSummaryTestIntent(t, 3)
	wantIntent, wantHead := *intent, slices.Clone(head)
	wantIntent.EligibleHeadUIDs, wantIntent.EligibleHeadScores = slices.Clone(intent.EligibleHeadUIDs), slices.Clone(intent.EligibleHeadScores)
	wantIntent.SelectedHeadUIDs, wantIntent.RejectedHeadUIDs = slices.Clone(intent.SelectedHeadUIDs), slices.Clone(intent.RejectedHeadUIDs)
	steerer := &ReleaseSteerer{headSummary: uploader.headSummaryUploader}
	steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
	if !reflect.DeepEqual(*intent, wantIntent) || !reflect.DeepEqual(head, wantHead) {
		t.Fatal("offer changed the intent or its head transcript")
	}
	// Steering's later use of its own values cannot reach the queued copy.
	intent.EligibleHeadScores[0] = RationalJSON{Numerator: "999", Denominator: "1"}
	intent.SelectedHeadUIDs[0] = 999
	head[0].Next = RationalJSON{Numerator: "999", Denominator: "1"}
	close(release)
	if err := uploader.next(t); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	summary, err := headsummary.Decode(operator.body(0))
	if err != nil || summary.Entries[0].Score != wantIntent.EligibleHeadScores[0] || !slices.Equal(summary.SelectedUIDs, wantIntent.SelectedHeadUIDs) {
		t.Fatalf("the queued summary followed later changes to steering's values: %v", err)
	}
}

// Offers never wait, even while the builder is stuck: the bounded queue
// keeps the newest summaries and drops the oldest.
func TestHeadSummaryOfferNeverWaitsAndDropsTheOldest(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	operator := newHeadSummaryTestOperator(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{beforeBuild: func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}})
	steerer := &ReleaseSteerer{headSummary: uploader.headSummaryUploader}
	first, head := headSummaryTestIntent(t, 1)
	steerer.offerHeadSummary(first, &ReleaseMeasurementArtifact{HeadEMA: head})
	<-entered
	offered := make(chan struct{})
	go func() {
		defer close(offered)
		for epoch := uint64(2); epoch <= 50; epoch++ {
			intent, head := headSummaryTestIntent(t, epoch)
			steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
		}
	}()
	select {
	case <-offered:
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("offers waited for a stuck builder")
	}
	// Epoch 1 is being built; epochs 2..42 were dropped for 43..50.
	if dropped := uploader.counters.offerDropped.Load(); dropped != 50-1-headSummaryQueueCapacity {
		t.Fatalf("dropped %d offers", dropped)
	}
	close(release)
	var built []uint64
	for range 1 + headSummaryQueueCapacity {
		build := uploader.nextBuild(t)
		if build.err != nil {
			t.Fatal(build.err)
		}
		built = append(built, build.epoch)
	}
	if !slices.Equal(built, []uint64{1, 43, 44, 45, 46, 47, 48, 49, 50}) {
		t.Fatalf("built epochs %v", built)
	}
	// Every built summary is delivered unless the operator queue dropped it.
	for uploader.counters.uploaded.Load()+uploader.counters.sendDropped.Load() < uint64(len(built)) {
		if err := uploader.next(t); err != nil {
			t.Fatal(err)
		}
	}
	for index := range operator.count() {
		summary, err := headsummary.Decode(operator.body(index))
		if err != nil || !slices.Contains(built, summary.NativeEpoch) {
			t.Fatalf("delivered summary %d: %v", index, err)
		}
	}
}

// A hanging operator holds only its own worker. Offers and the builder go
// on, its queue stays bounded, and close cancels the stuck request.
func TestHeadSummaryHangingOperatorBlocksNothing(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	operator := newHeadSummaryTestOperator(t, func(_ http.ResponseWriter, request *http.Request) { <-request.Context().Done() })
	uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{attemptTimeout: time.Hour})
	steerer := &ReleaseSteerer{headSummary: uploader.headSummaryUploader}
	intent, head := headSummaryTestIntent(t, 1)
	steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
	operator.waitEntered(t)
	offered := make(chan struct{})
	go func() {
		defer close(offered)
		for epoch := uint64(2); epoch <= 40; epoch++ {
			intent, head := headSummaryTestIntent(t, epoch)
			steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
		}
	}()
	select {
	case <-offered:
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("offers waited for a hanging operator")
	}
	if pending := uploader.targets[0].queue.len(); pending > headSummaryQueueCapacity {
		t.Fatalf("operator queue holds %d summaries", pending)
	}
	closed := make(chan struct{})
	go func() {
		uploader.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("close waited for a hanging operator")
	}
	if operator.count() != 1 || uploader.counters.uploaded.Load() != 0 || uploader.counters.failed.Load() != 0 {
		t.Fatalf("requests %d uploaded %d failed %d", operator.count(), uploader.counters.uploaded.Load(), uploader.counters.failed.Load())
	}
}

// Each attempt has its own timeout, and a summary gets at most three.
func TestHeadSummaryAttemptTimeoutBoundsAHangingOperator(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	operator := newHeadSummaryTestOperator(t, func(_ http.ResponseWriter, request *http.Request) { <-request.Context().Done() })
	uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{attemptTimeout: 50 * time.Millisecond})
	intent, head := headSummaryTestIntent(t, 1)
	uploader.offer(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
	err := uploader.next(t)
	if !errors.Is(err, context.DeadlineExceeded) || headSummaryFailureCause(err) != releaseDiagnosticTimeout {
		t.Fatalf("hanging operator: %v", err)
	}
	if operator.count() != 3 || uploader.counters.failed.Load() != 1 || !slices.Equal(uploader.recordedWaits(), []time.Duration{5 * time.Second, 15 * time.Second}) {
		t.Fatalf("attempts %d failed %d waits %v", operator.count(), uploader.counters.failed.Load(), uploader.recordedWaits())
	}
}

// Only transport failures, 408, 429 and 5xx are retried, at most three
// times, after 5 and 15 seconds or a Retry-After capped at 60 seconds.
func TestHeadSummaryRetriesOnlyTransientFailuresAndBoundsThem(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		retryAfter string
		attempts   int
		waits      []time.Duration
		ok         bool
	}{
		{name: "stored", status: http.StatusNoContent, attempts: 1, ok: true},
		{name: "ok", status: http.StatusOK, attempts: 1, ok: true},
		{name: "unavailable", status: http.StatusServiceUnavailable, attempts: 3, waits: []time.Duration{5 * time.Second, 15 * time.Second}},
		{name: "server error", status: http.StatusInternalServerError, attempts: 3, waits: []time.Duration{5 * time.Second, 15 * time.Second}},
		{name: "request timeout", status: http.StatusRequestTimeout, attempts: 3, waits: []time.Duration{5 * time.Second, 15 * time.Second}},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "7", attempts: 3, waits: []time.Duration{7 * time.Second, 7 * time.Second}},
		{name: "rate limited long", status: http.StatusTooManyRequests, retryAfter: "3600", attempts: 3, waits: []time.Duration{time.Minute, time.Minute}},
		{name: "invalid", status: http.StatusBadRequest, attempts: 1},
		{name: "unauthenticated", status: http.StatusUnauthorized, attempts: 1},
		{name: "forbidden", status: http.StatusForbidden, attempts: 1},
		{name: "no route yet", status: http.StatusNotFound, attempts: 1},
		{name: "too large", status: http.StatusRequestEntityTooLarge, attempts: 1},
		{name: "redirect", status: http.StatusFound, attempts: 1},
	}
	keypair := headSummaryTestKeypair(t)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			redirected := newHeadSummaryTestOperator(t, nil)
			operator := newHeadSummaryTestOperator(t, func(writer http.ResponseWriter, _ *http.Request) {
				if testCase.retryAfter != "" {
					writer.Header().Set("Retry-After", testCase.retryAfter)
				}
				if testCase.status == http.StatusFound {
					writer.Header().Set("Location", redirected.server.URL+headsummary.Path)
				}
				writer.WriteHeader(testCase.status)
			})
			uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{})
			intent, head := headSummaryTestIntent(t, 1)
			uploader.offer(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
			err := uploader.next(t)
			if (err == nil) != testCase.ok || operator.count() != testCase.attempts || redirected.count() != 0 || !slices.Equal(uploader.recordedWaits(), testCase.waits) {
				t.Fatalf("err %v attempts %d redirected %d waits %v", err, operator.count(), redirected.count(), uploader.recordedWaits())
			}
			if testCase.ok != (uploader.counters.uploaded.Load() == 1) || testCase.ok == (uploader.counters.failed.Load() == 1) {
				t.Fatal("outcome counters differ")
			}
		})
	}
	// A closed port and a missing session are retried, then given up.
	t.Run("unreachable", func(t *testing.T) {
		closed := httptest.NewServer(http.NotFoundHandler())
		closed.Close()
		uploader := startHeadSummaryTestUploader(t, keypair, []string{closed.URL}, headSummaryTestSessionGetter, headSummaryHooks{})
		intent, head := headSummaryTestIntent(t, 1)
		uploader.offer(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
		if err := uploader.next(t); err == nil || uploader.counters.attempts.Load() != 3 || headSummaryFailureCause(err) != releaseDiagnosticTransport {
			t.Fatalf("unreachable operator: %v attempts %d", err, uploader.counters.attempts.Load())
		}
	})
	t.Run("no session", func(t *testing.T) {
		operator := newHeadSummaryTestOperator(t, nil)
		uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, func() string { return "" }, headSummaryHooks{})
		intent, head := headSummaryTestIntent(t, 1)
		uploader.offer(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
		if err := uploader.next(t); !errors.Is(err, errHeadSummaryNoSession) || operator.count() != 0 || uploader.counters.attempts.Load() != 3 {
			t.Fatalf("no session: %v requests %d", err, operator.count())
		}
	})
	if got := headSummaryRetryAfter("Wed, 21 Oct 2015 07:28:30 GMT", time.Date(2015, 10, 21, 7, 28, 0, 0, time.UTC)); got != 30*time.Second {
		t.Fatalf("HTTP-date Retry-After = %v", got)
	}
	if headSummaryRetryAfter("soon", time.Now()) != 0 || headSummaryRetryAfter(strconv.Itoa(-1), time.Now()) != 0 {
		t.Fatal("an invalid Retry-After was honored")
	}
}

// A summary that does not match its own head result, a failing signer and a
// panicking signer are contained: nothing is sent, the failure is counted,
// and the next summary still goes out.
func TestHeadSummaryBuildFailuresAreContained(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	operator := newHeadSummaryTestOperator(t, nil)
	var signMu sync.Mutex
	signMode := "panic"
	setMode := func(mode string) {
		signMu.Lock()
		signMode = mode
		signMu.Unlock()
	}
	sign := func(message []byte) ([]byte, error) {
		signMu.Lock()
		mode := signMode
		signMu.Unlock()
		switch mode {
		case "panic":
			panic("signer fault")
		case "error":
			return nil, errors.New("signer unavailable")
		}
		return keypair.Sign(message)
	}
	finished, builds := make(chan error, 8), make(chan error, 8)
	target := &headSummaryTarget{noID: 9, endpoint: operator.server.URL + headsummary.Path, credential: headSummaryTestSessionGetter}
	uploader := startHeadSummaryUploader(t.Context(), keypair.PublicKey(), sign, 1, 2, []*headSummaryTarget{target}, headSummaryHooks{
		afterBuild:  func(_ uint64, err error) { builds <- err },
		afterUpload: func(_, _ uint64, err error) { finished <- err },
	})
	defer uploader.close()
	steerer := &ReleaseSteerer{headSummary: uploader}
	nextBuild := func() error {
		select {
		case err := <-builds:
			return err
		case <-time.After(headSummaryTestDeadline):
			t.Fatal("no build finished")
			return nil
		}
	}
	intent, head := headSummaryTestIntent(t, 1)
	mismatched, mismatchedHead := headSummaryTestIntent(t, 2)
	mismatched.EligibleHeadScores[0] = RationalJSON{Numerator: "1", Denominator: "1"}
	missing, _ := headSummaryTestIntent(t, 3)
	steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})               // the signer panics
	steerer.offerHeadSummary(mismatched, &ReleaseMeasurementArtifact{HeadEMA: mismatchedHead}) // a score its identities do not sum to
	steerer.offerHeadSummary(missing, &ReleaseMeasurementArtifact{})                           // eligible UIDs without identities
	for range 3 {
		if err := nextBuild(); err == nil {
			t.Fatal("a broken summary was built")
		}
	}
	setMode("error")
	steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
	if err := nextBuild(); err == nil {
		t.Fatal("a summary was built without a signature")
	}
	setMode("sign")
	steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
	if err := nextBuild(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("the uploader stopped after contained failures")
	}
	if operator.count() != 1 || uploader.counters.built.Load() != 1 || uploader.counters.buildFailed.Load() != 4 {
		t.Fatalf("requests %d built %d failed %d", operator.count(), uploader.counters.built.Load(), uploader.counters.buildFailed.Load())
	}
}

// An empty head is still a summary: the operator learns the validator ran.
func TestHeadSummaryEmptyHeadIsSent(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	operator := newHeadSummaryTestOperator(t, nil)
	uploader := startHeadSummaryTestUploader(t, keypair, []string{operator.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{})
	intent, _ := headSummaryTestIntent(t, 9)
	intent.EligibleHeadUIDs, intent.EligibleHeadScores, intent.SelectedHeadUIDs, intent.RejectedHeadUIDs = nil, nil, nil, nil
	uploader.offer(intent, &ReleaseMeasurementArtifact{})
	if err := uploader.next(t); err != nil {
		t.Fatal(err)
	}
	summary, err := headsummary.Decode(operator.body(0))
	if err != nil || len(summary.Entries) != 0 || summary.SelectedUIDs == nil || summary.RejectedUIDs == nil {
		t.Fatalf("empty head summary: %v", err)
	}
}

// Every configured operator gets the same signed bytes; one failing operator
// does not hold back another.
func TestHeadSummaryOperatorsAreIndependent(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	working := newHeadSummaryTestOperator(t, nil)
	hanging := newHeadSummaryTestOperator(t, func(_ http.ResponseWriter, request *http.Request) { <-request.Context().Done() })
	uploader := startHeadSummaryTestUploader(t, keypair, []string{hanging.server.URL, working.server.URL}, headSummaryTestSessionGetter, headSummaryHooks{attemptTimeout: time.Hour})
	intent, head := headSummaryTestIntent(t, 4)
	uploader.offer(intent, &ReleaseMeasurementArtifact{HeadEMA: head})
	if err := uploader.next(t); err != nil {
		t.Fatalf("the working operator was held back: %v", err)
	}
	hanging.waitEntered(t)
	if string(hanging.body(0)) != string(working.body(0)) {
		t.Fatal("operators received different bytes")
	}
}

// The steering hook is called right after the intent is durably begun, and
// as a statement whose type has no result: it cannot fail or return a value
// into the commit path.
func TestSubmitV2OffersTheHeadSummaryRightAfterTheDurableBegin(t *testing.T) {
	var _ func(*ReleaseSteerer, *SteeringIntent, *ReleaseMeasurementArtifact) = (*ReleaseSteerer).offerHeadSummary
	source, err := os.ReadFile("release_submit_v2.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "release_submit_v2.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	offers, found := 0, false
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok {
			if call, ok := node.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "offerHeadSummary" {
					offers++
				}
			}
			return true
		}
		if function.Name.Name != "submitOnceV2" {
			return true
		}
		for index, statement := range function.Body.List {
			assign, ok := statement.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || index+1 >= len(function.Body.List) {
				continue
			}
			left, leftOK := assign.Lhs[0].(*ast.Ident)
			right, rightOK := assign.Rhs[0].(*ast.Ident)
			if !leftOK || !rightOK || left.Name != "durableIntent" || right.Name != "intent" {
				continue
			}
			next, ok := function.Body.List[index+1].(*ast.ExprStmt)
			if !ok {
				t.Fatal("the statement after the durable begin is not the head summary offer")
			}
			call, ok := next.X.(*ast.CallExpr)
			if !ok {
				t.Fatal("the statement after the durable begin is not a call")
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				t.Fatal("the statement after the durable begin is not a method call")
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != "self" || selector.Sel.Name != "offerHeadSummary" || len(call.Args) != 2 {
				t.Fatal("the statement after the durable begin is not self.offerHeadSummary")
			}
			found = true
		}
		return true
	})
	if !found || offers != 1 {
		t.Fatalf("submitOnceV2 offers the head summary %d times after its durable begin (found=%t)", offers, found)
	}
}

// With an operator that hangs or refuses, the begun intent, its broadcast
// and its reconciliation to an applied row are exactly those without one.
func TestHeadSummaryOperatorCannotDelayOrAlterTheCommit(t *testing.T) {
	for _, behavior := range []string{"hang", "refuse"} {
		t.Run(behavior, func(t *testing.T) {
			fixture := newProductionContinuationTestFixture(t)
			operator := newHeadSummaryTestOperator(t, func(writer http.ResponseWriter, request *http.Request) {
				if behavior == "hang" {
					<-request.Context().Done()
					return
				}
				writer.WriteHeader(http.StatusInternalServerError)
			})
			finished := make(chan error, 8)
			hotkey := fixture.production.hotkey
			uploader := startHeadSummaryUploader(t.Context(), hotkey.PublicKey(), hotkey.Sign, fixture.steerer.cfg.Policy.PolicyID, fixture.steerer.cfg.Policy.Steering.MaximumHeadFleets,
				[]*headSummaryTarget{{noID: 9, endpoint: operator.server.URL + headsummary.Path, credential: headSummaryTestSessionGetter}},
				headSummaryHooks{attemptTimeout: time.Hour, wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }, afterUpload: func(_, _ uint64, err error) { finished <- err }})
			defer uploader.close()
			fixture.steerer.headSummary = uploader
			native := installProductionContinuationNative(t, fixture)
			// submitOnceV2's own order: durable begin, offer, then submit.
			intent, err := fixture.steerer.intents.beginV2(t.Context(), *fixture.intent)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(fixture.steerer.intents.path)
			if err != nil {
				t.Fatal(err)
			}
			begun, err := json.Marshal(intent)
			if err != nil {
				t.Fatal(err)
			}
			fixture.steerer.offerHeadSummary(intent, &ReleaseMeasurementArtifact{HeadEMA: []HeadEMAMeasurement{}})
			after, err := os.ReadFile(fixture.steerer.intents.path)
			if err != nil {
				t.Fatal(err)
			}
			offered, err := json.Marshal(intent)
			if err != nil || string(before) != string(after) || string(begun) != string(offered) {
				t.Fatalf("the offer changed the begun intent: %v", err)
			}
			authorized, err := fixture.steerer.intents.ownerRecyclePreparedConfig(t.Context(), fixture.steerer.cfg, intent.Prepared)
			if err != nil {
				t.Fatal(err)
			}
			if _, attempted, err := submitPreparedNativeRuntimeContext(t.Context(), fixture.steerer.native, authorized, intent.Prepared); !attempted || !errors.Is(err, context.DeadlineExceeded) || !slices.Equal(native.broadcasts, []string{intent.Prepared.ExtrinsicHex}) {
				t.Fatalf("submission: attempted=%t broadcasts=%d err=%v", attempted, len(native.broadcasts), err)
			}
			// The operator saw the summary while the commit went ahead.
			operator.waitEntered(t)
			native.receiptNumber, fixture.production.head = 101, 102
			fixture.production.extrinsicsKVs = map[uint64][]string{101: {intent.Prepared.ExtrinsicHex}}
			if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
				t.Fatalf("receipt did not reconcile: %v", err)
			}
			native.applied = true
			fixture.production.head = max(uint64(102), intent.Prepared.RevealBlock)
			var transition *productionSteeringTransition
			if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
				t.Fatalf("applied row did not reconcile: %v", err)
			}
			applied := fixture.restart(t)
			if applied.Status != "applied" || applied.VectorHash != intent.VectorHash || applied.Prepared.ExtrinsicHex != intent.Prepared.ExtrinsicHex || len(native.broadcasts) != 1 {
				t.Fatal("the commit changed under a failing operator")
			}
			if behavior == "refuse" {
				select {
				case err := <-finished:
					if err == nil || uploader.counters.failed.Load() != 1 || operator.count() != 3 {
						t.Fatalf("refusal: %v requests %d", err, operator.count())
					}
				case <-time.After(headSummaryTestDeadline):
					t.Fatal("the refused upload never finished")
				}
				return
			}
			// The hanging request is still open after the commit was applied.
			if uploader.counters.uploaded.Load() != 0 || uploader.counters.failed.Load() != 0 || operator.count() != 1 {
				t.Fatal("the hanging operator finished before the commit")
			}
		})
	}
}

// headscore and the summary agree on a decision's selection.
func TestHeadSummaryEntriesFollowHeadscoreRanking(t *testing.T) {
	intent, head := headSummaryTestIntent(t, 1)
	summary, err := newHeadSummary(headSummaryJob{eligible: intent.EligibleHeadUIDs, scores: intent.EligibleHeadScores, selected: intent.SelectedHeadUIDs, rejected: intent.RejectedHeadUIDs, head: head}, [32]byte{1}, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	scores := map[uint16]*big.Rat{}
	for _, entry := range summary.Entries {
		value, err := headscore.DecodeRational(entry.Score)
		if err != nil {
			t.Fatal(err)
		}
		scores[entry.UID] = value
	}
	ranked, err := headscore.Rank(scores)
	if err != nil || !slices.Equal(headscore.UIDs(ranked), intent.EligibleHeadUIDs) {
		t.Fatalf("ranked %v", headscore.UIDs(ranked))
	}
}
