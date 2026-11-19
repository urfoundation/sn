// Historical execution runs in one bounded child process over exact public
// bytes. Reproduction relative to those bytes is separate from runtime/finality
// admission and from transaction-specific native fee withdrawal/refund.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const historicalReplaySchema = "urnetwork-historical-proof-replay-v1"
const historicalReplaySdk = "cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a"
const historicalReplayJobLimit = 96 * 1024 * 1024
const historicalReplayReportLimit = 1024 * 1024
const historicalReplayEngineLimit = 512 * 1024 * 1024

// Rust serializes fixed digests as byte arrays. The standard Go array decoder
// accepts short/long arrays; explicit count/range checks preserve the wire pin.
type historicalReplayDigest [32]byte

func (self *historicalReplayDigest) UnmarshalJSON(raw []byte) error {
	var values []uint16
	if err := json.Unmarshal(raw, &values); err != nil || len(values) != len(self) {
		return errors.Join(errors.New("historical digest must contain exactly32 bytes"), err)
	}
	for index, value := range values {
		if value > 255 {
			return errors.New("historical digest contains a non-byte value")
		}
		self[index] = byte(value)
	}
	return nil
}

// The executable rechecks complete SCALE bodies/proofs and derives roots. The
// caller also binds every report to the exact job and independent file pins.
type historicalReplayJob struct {
	Schema                string                              `json:"schema"`
	ParentHeaderHex       string                              `json:"parent_header_hex"`
	ParentHash            historicalReplayDigest              `json:"parent_hash"`
	ChildHeaderHex        string                              `json:"child_header_hex"`
	ChildHash             historicalReplayDigest              `json:"child_hash"`
	ExtrinsicsHex         []string                            `json:"extrinsics_hex"`
	RuntimeCodeHex        string                              `json:"runtime_code_hex"`
	RuntimeCodeSha256     historicalReplayDigest              `json:"runtime_code_sha256"`
	RuntimeCodeBlake2b256 historicalReplayDigest              `json:"runtime_code_blake2b_256"`
	ExecutionStateVersion uint8                               `json:"execution_state_version"`
	ProofNodesHex         []string                            `json:"proof_nodes_hex"`
	ObservationProfile    *historicalReplayObservationProfile `json:"observation_profile,omitempty"`
	PrincipalQueries      []historicalPrincipalQuery          `json:"principal_queries,omitempty"`
	PrincipalEffects      bool                                `json:"principal_effects,omitempty"`
}

type historicalReplayReport struct {
	Schema                    string                           `json:"schema"`
	JobSha256                 historicalReplayDigest           `json:"job_sha256"`
	SdkRevision               string                           `json:"sdk_revision"`
	HostProfile               string                           `json:"host_profile"`
	ParentHash                historicalReplayDigest           `json:"parent_hash"`
	ChildHash                 historicalReplayDigest           `json:"child_hash"`
	ParentStateRoot           historicalReplayDigest           `json:"parent_state_root"`
	ChildStateRoot            historicalReplayDigest           `json:"child_state_root"`
	RuntimeCodeSha256         historicalReplayDigest           `json:"runtime_code_sha256"`
	ProofSha256               historicalReplayDigest           `json:"proof_sha256"`
	Extrinsics                uint64                           `json:"extrinsics"`
	ProofNodes                uint64                           `json:"proof_nodes"`
	ProofBytes                uint64                           `json:"proof_bytes"`
	StorageCalls              uint64                           `json:"storage_calls"`
	StorageIoBytes            uint64                           `json:"storage_io_bytes"`
	PostStateReproduced       bool                             `json:"post_state_reproduced"`
	AnchorAuthority           string                           `json:"anchor_authority"`
	RuntimeAdmitted           bool                             `json:"runtime_admitted"`
	NativeFeeDebit            *string                          `json:"native_fee_debit"`
	NativeFeeWithdrawalRefund bool                             `json:"native_fee_withdrawal_refund_observed"`
	ProductionSelection       bool                             `json:"production_selection"`
	HookObservations          *historicalReplayObservations    `json:"hook_observations,omitempty"`
	OpeningPrincipals         []historicalPrincipalObservation `json:"opening_principals,omitempty"`
	ClosingPrincipals         []historicalPrincipalObservation `json:"closing_principals,omitempty"`
}

type historicalReplayRequest struct {
	Engine planFileReference
	Job    planFileReference
	Budget time.Duration
}

// Hooks belong to a single caller and only expose real process/pipe boundaries
// for deterministic tests. The command constructs an empty hook set.
type historicalReplayHooks struct {
	beforeStart func(context.Context, *os.File)
	afterStart  func(context.Context, int)
	afterOutput func()
}

// Output refusal actively cancels the owned child; simply returning a writer
// error could otherwise leave a child with no reader waiting until deadline.
type historicalReplayOutput struct {
	buffer  bytes.Buffer
	maximum int
	cancel  context.CancelFunc
	read    func()
	err     error
}

func (self *historicalReplayOutput) Write(raw []byte) (int, error) {
	if len(raw) > self.maximum-self.buffer.Len() {
		self.err = errors.New("historical replay child output exceeds bound")
		self.cancel()
		return 0, self.err
	}
	n, err := self.buffer.Write(raw)
	if self.read != nil {
		self.read()
	}
	return n, err
}

// Execute once. Ambiguous, canceled, malformed or partial output never becomes
// a proof fact. A future fee observer can consume this same verified boundary.
func runHistoricalReplay(ctx context.Context, request historicalReplayRequest, hooks historicalReplayHooks) (result *historicalReplayReport, resultErr error) {
	if request.Budget < time.Minute || request.Budget > 15*time.Minute || !planSha256(request.Job.Sha256) {
		return nil, errors.New("historical replay requires a60–900second owned budget and exact input pin")
	}
	owner, cancel := context.WithTimeout(ctx, request.Budget)
	defer cancel()
	raw, digest, err := readBootstrapRootFile(owner, request.Job.Path, historicalNativeJobLimit)
	if err != nil {
		return nil, fmt.Errorf("read historical replay job: %w", err)
	}
	if digest != request.Job.Sha256 {
		return nil, errors.New("historical replay job differs from exact input pin")
	}
	var job historicalReplayJob
	if err := decodePlanJson(raw, &job); err != nil {
		return nil, err
	}
	maximumJob, maximumNodes, _ := historicalJobLimits(job.ObservationProfile)
	if len(raw) > maximumJob || job.Schema != historicalReplaySchema || len(job.ProofNodesHex) == 0 || len(job.ProofNodesHex) > maximumNodes || len(job.ExtrinsicsHex) > 16384 || job.ExecutionStateVersion > 1 {
		return nil, errors.New("historical replay job exceeds declared protocol bounds")
	}
	if err := errors.Join(job.ObservationProfile.validate(job), validateHistoricalPrincipalQueries(job.PrincipalQueries), validateHistoricalPrincipalEffectsRequest(job.PrincipalEffects, job.PrincipalQueries)); err != nil {
		return nil, err
	}
	maximumReportBytes := historicalReplayReportLimit
	if job.ObservationProfile != nil {
		maximumReportBytes = historicalReplayObservedReportLimit
		if job.ObservationProfile.Schema == historicalNativeProfileSchema {
			maximumReportBytes = historicalNativeReportLimit
		}
	}
	if job.PrincipalQueries != nil {
		maximumReportBytes += historicalPrincipalReportLimit
	}
	if job.PrincipalEffects {
		maximumReportBytes += historicalPrincipalReportLimit
	}
	for _, query := range job.PrincipalQueries {
		if query.Availability {
			maximumReportBytes += historicalAvailabilityReportLimit
			if job.PrincipalEffects {
				maximumReportBytes += historicalAvailabilityReportLimit
			}
			break
		}
	}
	output, err := runHistoricalProofWorker(owner, cancel, historicalProofWorkerRequest{Engine: request.Engine, Input: raw, Directory: filepath.Dir(request.Job.Path), MaximumReport: maximumReportBytes}, hooks)
	if err != nil {
		return nil, err
	}
	var report historicalReplayReport
	if err := decodePlanJson(output, &report); err != nil {
		return nil, err
	}
	if err := validateHistoricalReplayReport(job, raw, report); err != nil {
		return nil, err
	}
	return &report, owner.Err()
}

func validateHistoricalReplayReport(job historicalReplayJob, raw []byte, report historicalReplayReport) error {
	_, _, maximumProofBytes := historicalJobLimits(job.ObservationProfile)
	if report.Schema != historicalReplaySchema || report.JobSha256 != historicalReplayDigest(sha256.Sum256(raw)) || report.SdkRevision != historicalReplaySdk || (report.HostProfile != "substrate-proof-bounded-storage-v1" && report.HostProfile != "substrate-proof-bounded-hosts-v2") || report.ParentHash != job.ParentHash || report.ChildHash != job.ChildHash || report.RuntimeCodeSha256 != job.RuntimeCodeSha256 || report.Extrinsics != uint64(len(job.ExtrinsicsHex)) || report.ProofNodes != uint64(len(job.ProofNodesHex)) || report.ProofBytes > uint64(maximumProofBytes) || report.StorageCalls > 65536 || report.StorageIoBytes > 64*1024*1024 || !report.PostStateReproduced {
		return errors.New("historical replay report differs from exact input or execution profile")
	}
	if report.AnchorAuthority != "caller-supplied-unapproved" || report.RuntimeAdmitted || report.NativeFeeDebit != nil || report.NativeFeeWithdrawalRefund || report.ProductionSelection {
		return errors.New("historical replay report claims unestablished runtime, fee or finality authority")
	}
	return errors.Join(validateHistoricalReplayObservations(job, report.HookObservations), validateHistoricalPrincipalReport(job.PrincipalQueries, report.OpeningPrincipals), validateHistoricalClosingPrincipal(job, report))
}

// Both fixed workers share exact executable custody and joined bounded pipes.
// The optional node descriptor selects capture; it never selects another argv,
// executable path, signer or network target inside the supervisor.
type historicalProofWorkerRequest struct {
	Engine        planFileReference
	Input         []byte
	Directory     string
	MaximumReport int
	Nodes         *os.File
	Feed          *historicalNativeFeed
}

func runHistoricalProofWorker(owner context.Context, cancel context.CancelFunc, request historicalProofWorkerRequest, hooks historicalReplayHooks) (result []byte, resultErr error) {
	if request.MaximumReport <= 0 || request.MaximumReport > historicalNativeCaptureReportLimit || len(request.Input) > historicalNativeJobLimit {
		return nil, errors.New("historical proof worker exceeds its fixed profile")
	}
	engine, err := historicalReplayEngine(owner, request.Engine)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, engine.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	// The currently running image, not a re-resolved binary name, contains
	// the matching supervisor. Its process has no unrelated child owners.
	supervisor, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, supervisor.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	argument := "--retained-engine-fd3"
	if request.Nodes != nil {
		argument = "--retained-capture-engine-fd3-nodes-fd5"
	}
	var feedPipes *historicalNativeFeedPipes
	if request.Feed != nil {
		if request.Nodes == nil {
			return nil, errors.New("historical proof feed has no owned capture directory")
		}
		feedPipes, err = newHistoricalNativeFeedPipes()
		if err != nil {
			return nil, err
		}
		defer func() {
			resultErr = errors.Join(resultErr, feedPipes.closeChild(), feedPipes.closeParent())
			if resultErr != nil {
				result = nil
			}
		}()
		argument = "--retained-capture-engine-fd3-nodes-fd5-feed-fd6-fd7"
	}
	command := exec.CommandContext(owner, "/proc/self/fd/4", argument)
	command.Args[0] = "urnetwork-historical-replay-supervisor"
	command.ExtraFiles = []*os.File{engine, supervisor}
	if request.Nodes != nil {
		command.ExtraFiles = append(command.ExtraFiles, request.Nodes)
	}
	if feedPipes != nil {
		command.ExtraFiles = append(command.ExtraFiles, feedPipes.requestWrite, feedPipes.responseRead)
	}
	command.Env = []string{"LANG=C", "LC_ALL=C", "RUST_BACKTRACE=0"}
	command.Dir = request.Directory
	command.Stdin = bytes.NewReader(request.Input)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		// The supervisor kills/reaps its entire engine group before exiting.
		err := command.Process.Signal(syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	stdout := historicalReplayOutput{maximum: request.MaximumReport, cancel: cancel, read: hooks.afterOutput}
	stderr := historicalReplayOutput{maximum: 64 * 1024, cancel: cancel}
	command.Stdout, command.Stderr = &stdout, &stderr
	if hooks.beforeStart != nil {
		hooks.beforeStart(owner, engine)
	}
	if err := command.Start(); err != nil {
		return nil, errors.Join(err, owner.Err())
	}
	var feedDone chan error
	var feedCancel context.CancelFunc
	var stopFeedClose func() bool
	if feedPipes != nil {
		feedCtx, localCancel := context.WithCancel(owner)
		feedCancel = localCancel
		stopFeedClose = context.AfterFunc(feedCtx, func() { feedPipes.closeParent() })
		feedDone = make(chan error, 1)
		childCloseErr := feedPipes.closeChild()
		go func() {
			feedErr := childCloseErr
			if feedErr == nil {
				feedErr = request.Feed.serve(feedCtx, feedPipes.requestRead, feedPipes.responseWrite)
			}
			if feedErr != nil {
				cancel()
			}
			feedDone <- feedErr
		}()
	}
	if hooks.afterStart != nil {
		hooks.afterStart(owner, command.Process.Pid)
	}
	err = command.Wait()
	if feedDone != nil {
		var feedErr error
		if err == nil {
			// Every descendant is reaped and the parent's writer copy is closed.
			// Let the final EOF join naturally; cancellation must not race that
			// successful EOF into a synthetic closed-descriptor failure.
			select {
			case feedErr = <-feedDone:
			case <-owner.Done():
				feedCancel()
				feedPipes.closeParent()
				feedErr = <-feedDone
			}
		} else {
			feedCancel()
			feedPipes.closeParent()
			feedErr = <-feedDone
		}
		feedCancel()
		stopFeedClose()
		closeErr := feedPipes.closeParent()
		err = errors.Join(err, historicalNativeFeedError(feedErr), closeErr)
	}
	// Join before examining any buffer written by os/exec's pipe goroutines.
	if err := errors.Join(err, stdout.err, stderr.err, owner.Err()); err != nil {
		return nil, historicalReplayRefusal(err, stderr.buffer.Bytes())
	}
	return stdout.buffer.Bytes(), owner.Err()
}

// Diagnostic text is bounded context only. The original joined errors retain
// cancellation, I/O and process exit identity; text never chooses a retry or
// integrity classification for either capture or replay.
func historicalReplayRefusal(cause error, diagnostic []byte) error {
	const maximum = 2048
	if len(diagnostic) > maximum {
		diagnostic = diagnostic[:maximum]
	}
	text := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, string(diagnostic)))
	if text == "" {
		return fmt.Errorf("historical replay child refused: %w", cause)
	}
	return fmt.Errorf("historical replay child refused: %w; child diagnostic: %s", cause, text)
}
