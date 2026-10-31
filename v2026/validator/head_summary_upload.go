package validator

// head_summary_upload.go publishes the validator's per-tempo head result to
// each configured operator (docs/spec/head-summary-v2.md; server LEADERTOP.md
// §6.1, design question 9). It is off the steering path. offerHeadSummary
// copies the durably begun intent's head result into a bounded queue and
// returns at once; a background builder signs each recipient's summary, and one worker per
// operator posts it with a short timeout and bounded retries, failing open.
// Nothing here returns an error to steering, holds a steering lock or writes
// steering state, so a failing or hanging operator cannot delay, change or
// fail the intent, the EMA commit or the CRv4 commit.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/protocol/headsummary"
)

const (
	// Each operator queues at most this many summaries; the oldest drops.
	headSummaryQueueCapacity = 8
	// One request and its acknowledgement.
	headSummaryAttemptTimeout = 15 * time.Second
	// Attempts per summary per operator, the first included.
	headSummaryMaxAttempts = 3
	// The longest Retry-After the uploader honors.
	headSummaryMaxRetryAfter = 60 * time.Second
	// The most of a refusal's body read before closing it.
	headSummaryMaxResponseBytes = 4096
)

// The waits before the second and third attempts.
var headSummaryRetryDelays = [headSummaryMaxAttempts - 1]time.Duration{5 * time.Second, 15 * time.Second}

var errHeadSummaryNoSession = errors.New("head summary operator session is unavailable")

// headSummaryJob is one begun intent's head result, owned by the uploader:
// every slice is a copy, and nothing points back into steering state.
type headSummaryJob struct {
	netuid                                                   uint16
	nativeEpoch, nativeBlock, evmBlock, settlementEpoch      uint64
	nativeHash, evmHash, policyHash, measurementArtifactHash string
	selfUID                                                  uint16
	eligible, selected, rejected                             []uint16
	scores                                                   []RationalJSON
	head                                                     []HeadEMAMeasurement
	previousMeasurementArtifactHash                          string
	evidenceEmptyPrior                                       bool
	evidenceRecipient                                        uint64
	evidenceOperators                                        []uint64
	evidenceCuts                                             []headsummary.EvidenceCut
	evidencePolicy                                           *protocol.Policy
}

// headSummaryUpload is one signed summary queued for one operator.
type headSummaryUpload struct {
	nativeEpoch uint64
	body        []byte
}

// headSummaryQueue is a bounded FIFO that drops its oldest item when full.
// push never waits.
type headSummaryQueue[T any] struct {
	stateLock sync.Mutex
	items     []T
	capacity  int
	ready     chan struct{}
}

func newHeadSummaryQueue[T any](capacity int) *headSummaryQueue[T] {
	return &headSummaryQueue[T]{capacity: capacity, ready: make(chan struct{}, 1)}
}

// push appends item, dropping and returning the oldest when the queue is full.
func (self *headSummaryQueue[T]) push(item T) (dropped T, wasDropped bool) {
	self.stateLock.Lock()
	if len(self.items) == self.capacity {
		dropped, wasDropped = self.items[0], true
		self.items = slices.Delete(self.items, 0, 1)
	}
	self.items = append(self.items, item)
	self.stateLock.Unlock()
	select {
	case self.ready <- struct{}{}:
	default:
	}
	return dropped, wasDropped
}

// pop waits for the oldest item, or for ctx to end.
func (self *headSummaryQueue[T]) pop(ctx context.Context) (T, bool) {
	for {
		self.stateLock.Lock()
		if len(self.items) != 0 {
			item := self.items[0]
			self.items = slices.Delete(self.items, 0, 1)
			self.stateLock.Unlock()
			return item, true
		}
		self.stateLock.Unlock()
		select {
		case <-ctx.Done():
			var zero T
			return zero, false
		case <-self.ready:
		}
	}
}

func (self *headSummaryQueue[T]) len() int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return len(self.items)
}

// headSummaryTarget is one operator: its route and its live API session.
type headSummaryTarget struct {
	noID       uint64
	endpoint   string
	credential func() string
	queue      *headSummaryQueue[headSummaryUpload]
}

// Counted outcomes. Every failure is also a validator diagnostic.
type headSummaryCounters struct {
	offered      atomic.Uint64
	offerFailed  atomic.Uint64
	offerDropped atomic.Uint64
	built        atomic.Uint64
	buildFailed  atomic.Uint64
	sendDropped  atomic.Uint64
	attempts     atomic.Uint64
	uploaded     atomic.Uint64
	failed       atomic.Uint64
}

// Tests observe or replace waits, clocks and the signer; nil keeps the real one.
type headSummaryHooks struct {
	wait           func(context.Context, time.Duration) error
	attemptTimeout time.Duration
	now            func() time.Time
	beforeBuild    func()
	afterBuild     func(nativeEpoch uint64, err error)
	afterUpload    func(noID, nativeEpoch uint64, err error)
}

type headSummaryUploader struct {
	ctx      context.Context
	cancel   context.CancelFunc
	hotkey   [32]byte
	sign     func([]byte) ([]byte, error)
	policyID uint64
	maximum  uint16
	builds   *headSummaryQueue[headSummaryJob]
	targets  []*headSummaryTarget
	client   *http.Client
	hooks    headSummaryHooks
	counters headSummaryCounters
	closed   atomic.Bool
	workers  sync.WaitGroup
}

// newReleaseHeadSummaryUploader sends to every configured operator through
// the same live API session and origin as its attempt uploads, which already
// require TLS for credentials. It is always on; there is no switch.
func newReleaseHeadSummaryUploader(ctx context.Context, cfg *ReleaseConfig, hotkey *crv4.Keypair, runtimes []*releaseOperatorRuntime) *headSummaryUploader {
	targets := make([]*headSummaryTarget, 0, len(runtimes))
	for _, runtime := range runtimes {
		if runtime == nil || runtime.attemptUpload == nil || runtime.attemptUpload.writer == nil {
			continue
		}
		upload := runtime.attemptUpload
		endpoint := upload.writer.endpoint
		endpoint.Path, endpoint.RawPath, endpoint.RawQuery = headsummary.Path, "", ""
		targets = append(targets, &headSummaryTarget{noID: upload.noID, endpoint: endpoint.String(), credential: upload.writer.byJwt})
	}
	return startHeadSummaryUploader(ctx, hotkey.PublicKey(), hotkey.Sign, cfg.Policy.PolicyID, cfg.Policy.Steering.MaximumHeadFleets, targets, headSummaryHooks{})
}

// startHeadSummaryUploader starts the builder and one worker per target. All
// of them stop when ctx ends or close is called.
func startHeadSummaryUploader(ctx context.Context, hotkey [32]byte, sign func([]byte) ([]byte, error), policyID uint64, maximum uint16, targets []*headSummaryTarget, hooks headSummaryHooks) *headSummaryUploader {
	ctx, cancel := context.WithCancel(ctx)
	self := &headSummaryUploader{
		ctx: ctx, cancel: cancel, hotkey: hotkey, sign: sign, policyID: policyID, maximum: maximum,
		builds: newHeadSummaryQueue[headSummaryJob](headSummaryQueueCapacity), targets: targets, hooks: hooks,
		client: &http.Client{Transport: releaseAPIHTTPTransport, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
	for _, target := range targets {
		target.queue = newHeadSummaryQueue[headSummaryUpload](headSummaryQueueCapacity)
	}
	self.workers.Add(1 + len(targets))
	go self.runBuilder()
	for _, target := range targets {
		go self.runTarget(target)
	}
	return self
}

// close stops the workers, abandoning queued and in-flight summaries, and
// waits for them; an in-flight request is canceled, so it returns promptly.
func (self *headSummaryUploader) close() {
	if self == nil {
		return
	}
	self.closed.Store(true)
	self.cancel()
	self.workers.Wait()
}

// offerHeadSummary hands a durably begun intent's head result to the
// uploader. It returns nothing and never waits: it copies the fields it
// needs and queues them. It reads intent and artifact and changes neither.
func (self *ReleaseSteerer) offerHeadSummary(intent *SteeringIntent, artifact *ReleaseMeasurementArtifact) {
	if self == nil {
		return
	}
	self.headSummary.offer(intent, artifact)
}

// offer copies one intent's head result into the build queue.
func (self *headSummaryUploader) offer(intent *SteeringIntent, artifact *ReleaseMeasurementArtifact) {
	if self == nil || intent == nil || artifact == nil || self.closed.Load() {
		return
	}
	defer func() {
		if recover() != nil {
			self.counters.offerFailed.Add(1)
		}
	}()
	job := headSummaryJob{
		netuid: intent.Netuid, nativeEpoch: intent.SubnetEpoch, nativeBlock: intent.NativeSnapshotBlock, evmBlock: intent.EVMSnapshotBlock,
		settlementEpoch: intent.SettlementEpoch, nativeHash: intent.NativeSnapshotHash, evmHash: intent.EVMSnapshotHash,
		policyHash: intent.PolicyHash, measurementArtifactHash: intent.MeasurementArtifactHash, selfUID: intent.SelfUID,
		eligible: slices.Clone(intent.EligibleHeadUIDs), selected: slices.Clone(intent.SelectedHeadUIDs), rejected: slices.Clone(intent.RejectedHeadUIDs),
		scores: slices.Clone(intent.EligibleHeadScores), head: slices.Clone(artifact.HeadEMA),
	}
	if artifact.Schema == ReleaseMeasurementSchemaV2 {
		cuts, err := headSummaryEvidence(artifact)
		if err != nil {
			self.counters.offerFailed.Add(1)
			return
		}
		job.previousMeasurementArtifactHash = artifact.PreviousArtifactHash
		// A first intent may inherit a migrated EMA store. Only an explicit
		// absence of every prior permits reconstruction from an empty state.
		job.evidenceEmptyPrior = artifact.PreviousArtifactHash == ""
		for _, record := range artifact.HeadEMA {
			job.evidenceEmptyPrior = job.evidenceEmptyPrior && !record.HasPrior
		}
		job.evidenceCuts = cuts
		job.evidencePolicy = cloneReleasePolicy(&artifact.Policy)
		job.evidenceOperators = make([]uint64, len(cuts))
		for index, cut := range cuts {
			job.evidenceOperators[index] = cut.NoId
		}
	}
	self.counters.offered.Add(1)
	if _, dropped := self.builds.push(job); dropped {
		self.counters.offerDropped.Add(1)
	}
}

func (self *headSummaryUploader) now() time.Time {
	if self.hooks.now != nil {
		return self.hooks.now()
	}
	return time.Now()
}

// runBuilder signs the shared v1 result once, or one private v2 body per operator.
func (self *headSummaryUploader) runBuilder() {
	defer self.workers.Done()
	reported := uint64(0)
	for {
		job, ok := self.builds.pop(self.ctx)
		if !ok {
			return
		}
		if dropped := self.counters.offerDropped.Load(); dropped != reported {
			releaseDiagnostic(self.ctx, "steering", "head_summary_dropped", 0, false, dropped-reported)
			reported = dropped
		}
		publish := func(scoped headSummaryJob, targets []*headSummaryTarget) {
			body, err := self.build(scoped)
			if self.hooks.afterBuild != nil {
				self.hooks.afterBuild(job.nativeEpoch, err)
			}
			if err != nil {
				self.counters.buildFailed.Add(1)
				releaseDiagnostic(self.ctx, "steering", "head_summary_build_failed", job.nativeEpoch, true, 0, releaseDiagnosticFacts{cause: releaseDiagnosticHardError})
				return
			}
			self.counters.built.Add(1)
			for _, target := range targets {
				if dropped, wasDropped := target.queue.push(headSummaryUpload{nativeEpoch: job.nativeEpoch, body: body}); wasDropped {
					self.counters.sendDropped.Add(1)
					releaseDiagnostic(self.ctx, "operator", "head_summary_dropped", dropped.nativeEpoch, true, 0, releaseDiagnosticFacts{operatorId: target.noID, operatorKnown: true})
				}
			}
		}
		if job.evidencePolicy == nil {
			publish(job, self.targets)
			continue
		}
		// The global fleet result is shared; each eligible-client census is
		// signed and delivered only to its owning operator's authenticated API.
		for _, target := range self.targets {
			scoped := job
			scoped.evidenceRecipient = target.noID
			scoped.evidenceCuts = nil
			for index := range job.evidenceCuts {
				if job.evidenceCuts[index].NoId == target.noID {
					scoped.evidenceCuts = job.evidenceCuts[index : index+1]
					break
				}
			}
			publish(scoped, []*headSummaryTarget{target})
		}
	}
}

// build renders and signs one summary. A panic is contained as a failure.
func (self *headSummaryUploader) build(job headSummaryJob) (body []byte, resultErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			body, resultErr = nil, fmt.Errorf("head summary build panicked: %v", recovered)
		}
	}()
	if self.hooks.beforeBuild != nil {
		self.hooks.beforeBuild()
	}
	summary, err := newHeadSummary(job, self.hotkey, self.policyID, self.maximum)
	if err != nil {
		return nil, err
	}
	return headsummary.Sign(summary, self.hotkey, self.now(), self.sign)
}

// newHeadSummary maps one intent's head result onto the envelope. Each
// eligible UID's live identities with a positive EMA must sum exactly to its
// published score, and every such identity must belong to an eligible UID;
// the envelope's own validation then reconstructs the rank order and the
// selection from the scores.
func newHeadSummary(job headSummaryJob, hotkey [32]byte, policyID uint64, maximum uint16) (*headsummary.Summary, error) {
	if len(job.eligible) != len(job.scores) {
		return nil, errors.New("head summary eligible scores are incomplete")
	}
	type identities struct {
		hotkey [32]byte
		fleets []headsummary.Fleet
		sum    *big.Rat
	}
	byUID := map[uint16]*identities{}
	for _, record := range job.head {
		if !record.HasRaw {
			continue
		}
		next, err := decodeRationalJSON(record.Next)
		if err != nil {
			return nil, err
		}
		if next.Sign() == 0 {
			continue
		}
		uid := byUID[record.Key.UID]
		if uid == nil {
			uid = &identities{hotkey: record.Key.Hotkey, sum: new(big.Rat)}
			byUID[record.Key.UID] = uid
		}
		if uid.hotkey != record.Key.Hotkey {
			return nil, fmt.Errorf("head UID %d has identities under two hotkeys", record.Key.UID)
		}
		uid.fleets = append(uid.fleets, headsummary.Fleet{FleetID: headsummary.Hex32(record.Key.FleetID), Generation: record.Key.Generation})
		uid.sum.Add(uid.sum, next)
	}
	if len(byUID) != len(job.eligible) {
		return nil, errors.New("head summary identities differ from the eligible UIDs")
	}
	entries := make([]headsummary.Entry, 0, len(job.eligible))
	for index, uid := range job.eligible {
		found := byUID[uid]
		score, err := decodeRationalJSON(job.scores[index])
		if err != nil || found == nil || found.sum.Cmp(score) != 0 {
			return nil, fmt.Errorf("head UID %d score differs from its live identities", uid)
		}
		slices.SortFunc(found.fleets, func(a, b headsummary.Fleet) int {
			if order := strings.Compare(a.FleetID, b.FleetID); order != 0 {
				return order
			}
			return compareUint64(a.Generation, b.Generation)
		})
		entries = append(entries, headsummary.Entry{Rank: uint32(index) + 1, UID: uid, Hotkey: headsummary.Hex32(found.hotkey), Score: job.scores[index], Selected: index < int(maximum), Fleets: found.fleets})
	}
	nonNil := func(values []uint16) []uint16 {
		if values == nil {
			return []uint16{}
		}
		return values
	}
	schema := headsummary.Schema
	if job.evidencePolicy != nil {
		schema = headsummary.SchemaV2
	}
	return &headsummary.Summary{
		Schema: schema, Netuid: job.netuid, NativeEpoch: job.nativeEpoch,
		NativeSnapshotBlock: job.nativeBlock, NativeSnapshotHash: strings.ToLower(job.nativeHash),
		EVMSnapshotBlock: job.evmBlock, EVMSnapshotHash: strings.ToLower(job.evmHash), SettlementEpoch: job.settlementEpoch,
		PolicyID: policyID, PolicyHash: strings.ToLower(job.policyHash), MeasurementArtifactHash: job.measurementArtifactHash,
		PreviousMeasurementArtifactHash: job.previousMeasurementArtifactHash, EvidenceEmptyPrior: job.evidenceEmptyPrior, EvidenceCuts: job.evidenceCuts, EvidencePolicy: job.evidencePolicy,
		EvidenceRecipient: job.evidenceRecipient, EvidenceOperators: job.evidenceOperators,
		ValidatorHotkey: headsummary.Hex32(hotkey), ValidatorUID: job.selfUID, MaximumHeadFleets: maximum,
		Entries: entries, SelectedUIDs: nonNil(job.selected), RejectedUIDs: nonNil(job.rejected),
	}, nil
}

func compareUint64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// runTarget sends each queued summary to one operator, one at a time.
func (self *headSummaryUploader) runTarget(target *headSummaryTarget) {
	defer self.workers.Done()
	for {
		upload, ok := target.queue.pop(self.ctx)
		if !ok {
			return
		}
		attempts, err := self.send(target, upload)
		if err == nil {
			self.counters.uploaded.Add(1)
			releaseDiagnostic(self.ctx, "operator", "head_summary_uploaded", upload.nativeEpoch, true, attempts, releaseDiagnosticFacts{operatorId: target.noID, operatorKnown: true})
		} else if self.ctx.Err() == nil {
			self.counters.failed.Add(1)
			releaseDiagnostic(self.ctx, "operator", "head_summary_upload_failed", upload.nativeEpoch, true, attempts, releaseDiagnosticFacts{operatorId: target.noID, operatorKnown: true, cause: headSummaryFailureCause(err)})
		}
		if self.hooks.afterUpload != nil {
			self.hooks.afterUpload(target.noID, upload.nativeEpoch, err)
		}
	}
}

// send makes at most headSummaryMaxAttempts attempts, waiting between them.
// A panic is contained as a failure.
func (self *headSummaryUploader) send(target *headSummaryTarget, upload headSummaryUpload) (attempts uint64, resultErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			resultErr = fmt.Errorf("head summary send panicked: %v", recovered)
		}
	}()
	for attempts < headSummaryMaxAttempts {
		attempts++
		self.counters.attempts.Add(1)
		retryAfter, retryable, err := self.post(target, upload.body)
		if err == nil {
			return attempts, nil
		}
		if !retryable || attempts == headSummaryMaxAttempts {
			return attempts, err
		}
		delay := headSummaryRetryDelays[attempts-1]
		if retryAfter > 0 {
			delay = min(retryAfter, headSummaryMaxRetryAfter)
		}
		if waitErr := self.wait(delay); waitErr != nil {
			return attempts, errors.Join(err, waitErr)
		}
	}
	return attempts, errors.New("head summary attempts are exhausted")
}

func (self *headSummaryUploader) wait(delay time.Duration) error {
	if self.hooks.wait != nil {
		return self.hooks.wait(self.ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-self.ctx.Done():
		return self.ctx.Err()
	case <-timer.C:
		return nil
	}
}

// headSummaryStatusError is an operator's refusal, without its body.
type headSummaryStatusError struct{ status int }

func (self *headSummaryStatusError) Error() string {
	return fmt.Sprintf("head summary upload refused with HTTP %d", self.status)
}

// post makes one attempt. It reports whether a failure may be retried and
// any Retry-After the operator asked for.
func (self *headSummaryUploader) post(target *headSummaryTarget, body []byte) (time.Duration, bool, error) {
	credential := target.credential()
	if credential == "" || len(credential) > 16*1024 || strings.TrimSpace(credential) != credential || strings.ContainsAny(credential, "\r\n") {
		return 0, true, errHeadSummaryNoSession
	}
	timeout := headSummaryAttemptTimeout
	if self.hooks.attemptTimeout > 0 {
		timeout = self.hooks.attemptTimeout
	}
	ctx, cancel := context.WithTimeout(self.ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, false, err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := doReleaseApiHttpRequest(self.client, request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return 0, true, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, headSummaryMaxResponseBytes))
	_ = response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return 0, false, nil
	}
	refusal := &headSummaryStatusError{status: response.StatusCode}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return headSummaryRetryAfter(response.Header.Get("Retry-After"), self.now()), true, refusal
	}
	return 0, false, refusal
}

// headSummaryRetryAfter reads delay-seconds or an HTTP date; zero is none.
func headSummaryRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

// The diagnostic cause of a final failure; never its text.
func headSummaryFailureCause(err error) releaseDiagnosticCause {
	var refusal *headSummaryStatusError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return releaseDiagnosticTimeout
	case errors.Is(err, errHeadSummaryNoSession):
		return releaseDiagnosticUnavailable
	case errors.As(err, &refusal):
		if refusal.status == http.StatusTooManyRequests || refusal.status >= 500 {
			return releaseDiagnosticUnavailable
		}
		return releaseDiagnosticHardError
	}
	return releaseDiagnosticTransport
}
