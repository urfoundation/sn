// Read-only scenario observations can recover from isolated transport failures.
// Every occurrence remains durable; malformed evidence and exhausted budgets stop.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

const (
	scenarioSnapshotMaximumRecoveries = 2
	scenarioSnapshotRetryTimeout      = 5 * time.Minute
	scenarioSnapshotRetryDelay        = 250 * time.Millisecond
	scenarioSnapshotRetryFilename     = "snapshot-retries.json"
	// This is a diagnostic storage bound, independent of the number of
	// isolated transient outages tolerated by strict final acceptance.
	scenarioProvisionalSnapshotMaximumRecords = 4096
	// Allow even a fully escaped diagnostic at every record boundary.
	scenarioProvisionalSnapshotMaximumBytes = 32 * 1024 * 1024
	scenarioProvisionalSnapshotMaximumDelay = 5 * time.Second
)

// A typed terminal result keeps the outer observation loop from retrying a
// permanent error, or restarting this wrapper's already exhausted retry budget.
type scenarioSnapshotTerminalError struct{ cause error }

func (self *scenarioSnapshotTerminalError) Error() string { return self.cause.Error() }
func (self *scenarioSnapshotTerminalError) Unwrap() error { return self.cause }

// Do not infer transport failures from message text. In particular, EOF while
// decoding a local artifact, certificate errors, and mixed integrity/network
// errors cannot become retryable by mentioning a timeout.
func scenarioSnapshotTransportError(err error, transportOrigin bool) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !scenarioSnapshotTransportError(cause, transportOrigin) {
				return false
			}
		}
		return true
	}
	switch cause := err.(type) {
	case *scenarioSnapshotTerminalError:
		return false
	case *os.PathError:
		return false
	case *evmReadRpcExhaustedError:
		// This owner performed only RPC reads. Its typed deadline may be
		// retried by the bounded snapshot owner without admitting unrelated
		// context deadlines or any mixed integrity failure.
		return scenarioSnapshotTransportError(cause.cause, true)
	case *url.Error:
		return scenarioSnapshotTransportError(cause.Err, true)
	case *net.OpError:
		return scenarioSnapshotTransportError(cause.Err, true)
	case *publicEVMResponseBodyReadError:
		return scenarioSnapshotTransportError(cause.cause, true)
	case gethrpc.HTTPError:
		switch cause.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	case *gethrpc.HTTPError:
		return scenarioSnapshotTransportError(*cause, transportOrigin)
	case gethrpc.Error:
		// Preserve the lower read owner's explicit provider-response rules,
		// including refusal of pruned state and contract reverts.
		return evmReadRpcErrorIsTransient(cause)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return scenarioSnapshotTransportError(wrapped.Unwrap(), transportOrigin)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return transportOrigin
	}
	if errors.Is(err, gethrpc.ErrMissingBatchResponse) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

// One state owns the whole release/production phase, including preparation.
// Its lock also protects final evidence reads while a canceled probe unwinds.
type scenarioSnapshotRetryState struct {
	stateLock      sync.Mutex
	runDir         string
	phase          string
	now            func() time.Time
	wait           func(context.Context, time.Duration) error
	records        []AssertionRecord
	terminalError  string
	provisional    bool
	retainedStrict bool
}

type scenarioSnapshotRetryEvidence struct {
	Schema              string            `json:"schema"`
	RunID               string            `json:"run_id"`
	Phase               string            `json:"phase"`
	MaximumRecoveries   int               `json:"maximum_recoveries"`
	RetryTimeoutSeconds int               `json:"retry_timeout_seconds"`
	Records             []AssertionRecord `json:"records"`
	TerminalError       string            `json:"terminal_error,omitempty"`
	Provisional         bool              `json:"provisional,omitempty"`
}

// Disk evidence may retain a stricter old policy, but cannot authorize a more
// permissive retry owner than this invocation's admitted provisional mode.
func (self *scenarioSnapshotRetryState) provisionalRetries() bool {
	return self.provisional && !self.retainedStrict
}

func (self *scenarioSnapshotRetryState) maximumRecords() int {
	if self.provisionalRetries() {
		return scenarioProvisionalSnapshotMaximumRecords
	}
	return scenarioSnapshotMaximumRecoveries
}

// Existing pre-acceptance diagnostics retain their budget after a process
// restart. The signed acceptance interval still cannot restart in place.
func (self *scenarioSnapshotRetryState) load() error {
	maximumBytes := int64(64 * 1024)
	if self.provisional {
		maximumBytes = scenarioProvisionalSnapshotMaximumBytes
	}
	raw, err := readValidatorEvidenceHistoricalFile(self.runDir, scenarioSnapshotRetryFilename, maximumBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var evidence scenarioSnapshotRetryEvidence
	if err := decodeStrictJSONBytes(raw, &evidence); err != nil {
		return err
	}
	retainedStrict := false
	maximumRecords := scenarioProvisionalSnapshotMaximumRecords
	if evidence.Schema == "urnetwork-sim-snapshot-retries-v1" && !evidence.Provisional {
		retainedStrict = true
		maximumRecords = scenarioSnapshotMaximumRecoveries
	} else if evidence.Schema != "urnetwork-sim-snapshot-retries-v2" || !evidence.Provisional || !self.provisional {
		return errors.New("scenario snapshot retry evidence has no admitted retry policy")
	}
	if evidence.RunID != filepath.Base(self.runDir) || evidence.Phase != self.phase || evidence.MaximumRecoveries != maximumRecords || evidence.RetryTimeoutSeconds != int(scenarioSnapshotRetryTimeout/time.Second) || len(evidence.Records) > maximumRecords+1 {
		return errors.New("scenario snapshot retry evidence has a different phase or budget")
	}
	for index, record := range evidence.Records {
		if record.ID != fmt.Sprintf("scenario_snapshot_retry_%06d", index+1) || record.Message == "" || record.StartedAt == "" || record.CompletedAt == "" || record.DurationSeconds < 0 {
			return errors.New("scenario snapshot retry evidence is malformed")
		}
		started, startErr := time.Parse(time.RFC3339Nano, record.StartedAt)
		completed, completeErr := time.Parse(time.RFC3339Nano, record.CompletedAt)
		if startErr != nil || completeErr != nil || completed.Before(started) || record.DurationSeconds != completed.Sub(started).Seconds() {
			return errors.New("scenario snapshot retry evidence has invalid chronology")
		}
		if !record.Passed && (!completed.Equal(started) || record.DurationSeconds != 0) {
			return errors.New("scenario snapshot pending retry evidence is malformed")
		}
		if index != 0 && !evidence.Records[index-1].Passed && record.Passed {
			return errors.New("scenario snapshot retry evidence skips an unresolved incident")
		}
	}
	self.records = evidence.Records
	self.terminalError = evidence.TerminalError
	self.retainedStrict = retainedStrict
	return nil
}

// Atomic evidence precedes every retry, so a second outage cannot erase the
// first incident, including when the next observation never completes.
func (self *scenarioSnapshotRetryState) persistWithLock() error {
	schema := "urnetwork-sim-snapshot-retries-v1"
	if self.provisionalRetries() {
		schema = "urnetwork-sim-snapshot-retries-v2"
	}
	return writePublicJSON(filepath.Join(self.runDir, scenarioSnapshotRetryFilename), scenarioSnapshotRetryEvidence{
		Schema: schema, RunID: filepath.Base(self.runDir), Phase: self.phase,
		MaximumRecoveries: self.maximumRecords(), RetryTimeoutSeconds: int(scenarioSnapshotRetryTimeout / time.Second), Records: self.records, TerminalError: self.terminalError, Provisional: self.provisionalRetries(),
	})
}

func (self *scenarioSnapshotRetryState) assertions() []AssertionRecord {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]AssertionRecord(nil), self.records...)
}

// Persist exhaustion separately from an interrupted pending retry. Reopening
// the phase must never turn consecutive failures into isolated recoveries.
func (self *scenarioSnapshotRetryState) terminal(failure error) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.records) != 0 {
		self.terminalError = failure.Error()
		if self.provisionalRetries() && len(self.terminalError) > 4*1024 {
			self.terminalError = self.terminalError[:4*1024] + " [diagnostic truncated]"
		}
		failure = errors.Join(failure, self.persistWithLock())
	}
	return &scenarioSnapshotTerminalError{cause: failure}
}

func (self *scenarioSnapshotRetryState) priorTerminal() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.terminalError != "" {
		return &scenarioSnapshotTerminalError{cause: errors.New(self.terminalError)}
	}
	return nil
}

// A crash can occur after a failed read is durable but before either the retry
// or its terminal verdict is written. Reopen that pending retry with only its
// original remaining time and attempt, never as a new initial observation.
func (self *scenarioSnapshotRetryState) pendingRetryBudget() (time.Duration, bool, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var pending []AssertionRecord
	for _, record := range self.records {
		if !record.Passed {
			pending = append(pending, record)
		}
	}
	if len(self.records) > self.maximumRecords() || !self.provisionalRetries() && len(pending) > 1 {
		return 0, false, errors.New("scenario snapshot transport retry budget exhausted before restart")
	}
	if len(pending) == 0 {
		return 0, false, nil
	}
	started, err := time.Parse(time.RFC3339Nano, pending[0].StartedAt)
	now := self.now().UTC()
	if err != nil || now.Before(started) {
		return 0, false, errors.New("scenario snapshot retry evidence has invalid chronology")
	}
	remaining := scenarioSnapshotRetryTimeout - now.Sub(started)
	if remaining <= 0 {
		return 0, false, fmt.Errorf("scenario snapshot transport retry deadline elapsed before restart: %w", context.DeadlineExceeded)
	}
	return remaining, true, nil
}

func (self *scenarioSnapshotRetryState) record(failure error) (bool, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	now := self.now().UTC()
	message := failure.Error()
	if self.provisionalRetries() && len(message) > 1024 {
		message = message[:1024] + " [diagnostic truncated]"
	}
	self.records = append(self.records, AssertionRecord{ID: fmt.Sprintf("scenario_snapshot_retry_%06d", len(self.records)+1),
		Message: "retryable transport observation failed: " + message, StartedAt: now.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano)})
	return len(self.records) <= self.maximumRecords(), self.persistWithLock()
}

// A successful fresh observation resolves pending diagnostics, without
// manufacturing or reusing a snapshot from before the transport failure.
func (self *scenarioSnapshotRetryState) recovered() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.records) > self.maximumRecords() {
		return errors.New("scenario snapshot transport retry phase budget is exhausted")
	}
	changed := false
	for index := range self.records {
		record := &self.records[index]
		if record.Passed {
			continue
		}
		now := self.now().UTC()
		started, err := time.Parse(time.RFC3339Nano, record.StartedAt)
		if err != nil || now.Before(started) {
			return errors.New("scenario snapshot retry evidence has invalid chronology")
		}
		record.Passed, record.CompletedAt, record.DurationSeconds = true, now.Format(time.RFC3339Nano), now.Sub(started).Seconds()
		record.Message = "recovered isolated transport read; " + record.Message
		changed = true
	}
	if changed {
		return self.persistWithLock()
	}
	return nil
}

type scenarioSnapshotRetryProbe struct {
	source  scenarioProbe
	retries *scenarioSnapshotRetryState
}

// The original read keeps its normal deadline. Strict runs allow one retry.
// An explicitly provisional owner may continue transient reads inside the
// same five-minute incident deadline; neither a retry nor a crash resets it.
func (self *scenarioSnapshotRetryProbe) Snapshot(ctx context.Context) (*ScenarioObservation, error) {
	if err := self.retries.priorTerminal(); err != nil {
		return nil, err
	}
	readCtx := ctx
	firstAttempt := 0
	remaining, pending, err := self.retries.pendingRetryBudget()
	if err != nil {
		return nil, self.retries.terminal(err)
	}
	if pending {
		firstAttempt = 1
		var cancel context.CancelFunc
		readCtx, cancel = context.WithTimeout(ctx, remaining)
		defer cancel()
	}
	for attempt := firstAttempt; ; attempt++ {
		if err := readCtx.Err(); err != nil {
			return nil, self.retries.terminal(err)
		}
		observation, err := self.source.Snapshot(readCtx)
		if readCtx.Err() != nil {
			return nil, self.retries.terminal(readCtx.Err())
		}
		if err == nil {
			if observation == nil {
				return nil, self.retries.terminal(errors.New("scenario snapshot returned no observation"))
			}
			if _, _, budgetErr := self.retries.pendingRetryBudget(); budgetErr != nil {
				return nil, self.retries.terminal(budgetErr)
			}
			if err := self.retries.recovered(); err != nil {
				return nil, self.retries.terminal(err)
			}
			return observation, nil
		}
		if !scenarioSnapshotTransportError(err, false) {
			return nil, self.retries.terminal(err)
		}
		allowed, persistErr := self.retries.record(err)
		if persistErr != nil {
			return nil, self.retries.terminal(errors.Join(err, fmt.Errorf("persist snapshot retry: %w", persistErr)))
		}
		if !allowed || !self.retries.provisionalRetries() && attempt != 0 {
			return nil, self.retries.terminal(fmt.Errorf("scenario snapshot transport retry budget exhausted: %w", err))
		}
		if attempt == 0 {
			remaining, _, budgetErr := self.retries.pendingRetryBudget()
			if budgetErr != nil {
				return nil, self.retries.terminal(budgetErr)
			}
			var cancel context.CancelFunc
			readCtx, cancel = context.WithTimeout(ctx, remaining)
			defer cancel()
		}
		delay := scenarioSnapshotRetryDelay
		if self.retries.provisionalRetries() {
			for count := 0; count < attempt && delay < scenarioProvisionalSnapshotMaximumDelay; count++ {
				delay = min(2*delay, scenarioProvisionalSnapshotMaximumDelay)
			}
		}
		if err := self.retries.wait(readCtx, delay); err != nil {
			return nil, self.retries.terminal(fmt.Errorf("scenario snapshot transport retry deadline: %w", err))
		}
		if _, _, budgetErr := self.retries.pendingRetryBudget(); budgetErr != nil {
			return nil, self.retries.terminal(budgetErr)
		}
	}
}

// Preserve the optional scheduler interface exactly: heartbeats must continue
// to advance fault timing while a full observation is being retried.
type scenarioSnapshotRetryHeadProbe struct {
	*scenarioSnapshotRetryProbe
	head scenarioFinalizedHeadProbe
}

func (self *scenarioSnapshotRetryHeadProbe) FinalizedHead(ctx context.Context) (ChainHead, error) {
	return self.head.FinalizedHead(ctx)
}

func (self *scenarioSnapshotRetryState) wrap(source scenarioProbe) scenarioProbe {
	probe := &scenarioSnapshotRetryProbe{source: source, retries: self}
	if head, ok := source.(scenarioFinalizedHeadProbe); ok {
		return &scenarioSnapshotRetryHeadProbe{scenarioSnapshotRetryProbe: probe, head: head}
	}
	return probe
}
