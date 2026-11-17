// A synthetic executable protocol peer exercises real public command/process
// custody. It does not pretend to execute Wasm or qualify a runtime fee hook;
// the separate Rust tests own trie/execution correctness.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Only the private test binary uses this argv grammar. The production child is
// the separately hash-pinned Rust ELF and has no test-mode environment switch.
func init() {
	if os.Args[0] != "urnetwork-historical-replay" {
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--synthetic-replay-descendant" {
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	}
	if len(os.Args) == 2 && os.Args[1] == "--historical-proof-capture-v1" {
		historicalCaptureSyntheticPeer()
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "--historical-proof-capture-feed-v1" {
		historicalArchiveCaptureSyntheticPeer()
		os.Exit(0)
	}
	if len(os.Args) != 2 || os.Args[1] != "--historical-proof-replay-v1" {
		os.Exit(6)
	}
	reader := bufio.NewReader(os.Stdin)
	prefix, _ := reader.Peek(256)
	if bytes.Contains(prefix, []byte(`"parent_header_hex":"0xb10c"`)) {
		_, _ = os.Stdout.WriteString("{\"schema\":")
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, historicalReplayJobLimit+1))
	var job historicalReplayJob
	if err != nil || len(raw) > historicalReplayJobLimit || json.Unmarshal(raw, &job) != nil || os.Getenv("SYNTHETIC_REPLAY_INHERITED") != "" {
		os.Exit(7)
	}
	report := historicalReplayReport{Schema: historicalReplaySchema, JobSha256: historicalReplayDigest(sha256.Sum256(raw)), SdkRevision: historicalReplaySdk, HostProfile: "substrate-proof-bounded-storage-v1", ParentHash: job.ParentHash, ChildHash: job.ChildHash, RuntimeCodeSha256: job.RuntimeCodeSha256, Extrinsics: uint64(len(job.ExtrinsicsHex)), ProofNodes: uint64(len(job.ProofNodesHex)), ProofBytes: 1, StorageCalls: 1, StorageIoBytes: 1, PostStateReproduced: true, AnchorAuthority: "caller-supplied-unapproved"}
	if job.RuntimeCodeHex == "0xfd" {
		if historicalFeeContextSyntheticPeer(job, &report) != nil {
			os.Exit(7)
		}
	} else if job.ObservationProfile != nil {
		if job.ObservationProfile.Schema == historicalNativeProfileSchema {
			report.HookObservations = historicalNativeExecutionTestTrace(job)
		} else {
			report.HookObservations = historicalObservationTestTrace(job)
		}
	}
	switch job.RuntimeCodeHex {
	case "0xdb":
		_, _ = os.Stderr.WriteString("principal API refusal; timeout and integrity are diagnostic words only\n" + strings.Repeat("x", 4096))
		os.Exit(7)
	case "0xdc":
		_, _ = os.Stderr.WriteString("principal API execution interrupted\n")
		_, _ = os.Stdout.WriteString("{\"schema\":")
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	case "0xc0", "0xc1", "0xc2", "0xc3", "0xc4":
		report.HostProfile = "substrate-proof-bounded-hosts-v2"
		switch job.RuntimeCodeHex {
		case "0xc1":
			report.HostProfile = "unreviewed-automatic-hosts"
		case "0xc2":
			report.StorageCalls = 65537
		case "0xc3":
			report.StorageIoBytes = 64*1024*1024 + 1
		case "0xc4":
			report.NativeFeeWithdrawalRefund = true
		}
	case "0x01":
		report.JobSha256[0] ^= 1
	case "0x02":
		report.ParentHash[0] ^= 1
	case "0x03":
		report.RuntimeCodeSha256[0] ^= 1
	case "0x04":
		report.NativeFeeDebit = new(string)
		*report.NativeFeeDebit = "0"
		report.NativeFeeWithdrawalRefund = true
	case "0x05":
		report.AnchorAuthority = "verified"
		report.RuntimeAdmitted = true
	case "0x06":
		_, _ = os.Stdout.WriteString("{\"schema\":")
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	case "0x07":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte{'x'}, historicalReplayReportLimit+1))
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	case "0x08":
		_, _ = os.Stderr.Write(bytes.Repeat([]byte{'x'}, 64*1024+1))
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	case "0x09":
		_ = json.NewEncoder(os.Stdout).Encode(report)
		_, _ = os.Stdout.WriteString("{}")
		os.Exit(0)
	case "0x0a":
		_ = json.NewEncoder(os.Stdout).Encode(report)
		os.Exit(9)
	case "0x0b":
		child := exec.Command("/proc/self/exe", "--synthetic-replay-descendant")
		child.Args[0] = "urnetwork-historical-replay"
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(10)
		}
		path, err := hex.DecodeString(strings.TrimPrefix(job.ExtrinsicsHex[0], "0x"))
		if err != nil || os.WriteFile(string(path), []byte(strconv.Itoa(child.Process.Pid)), 0600) != nil {
			os.Exit(10)
		}
		// The direct engine exits zero with a superficially valid result.
		// The supervisor must refuse it while retaining and reaping the peer.
	case "0xf3":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte{'x'}, historicalReplayObservedReportLimit+1))
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	}
	_ = json.NewEncoder(os.Stdout).Encode(report)
	os.Exit(0)
}

// Real files/processes have no shared mutable fixture state. Protected selected
// engines keep their path; ordinary go test images get an owned private copy.
func historicalReplayTestRequest(t *testing.T, mode string) historicalReplayRequest {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	engine, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engineReference, err := historicalReplayFixtureEngine(engine, root)
	if err != nil {
		t.Fatal(err)
	}
	job := historicalReplayJob{Schema: historicalReplaySchema, ParentHeaderHex: "0x00", ParentHash: historicalReplayDigest{1}, ChildHeaderHex: "0x00", ChildHash: historicalReplayDigest{2}, RuntimeCodeHex: mode, RuntimeCodeSha256: historicalReplayDigest{3}, RuntimeCodeBlake2b256: historicalReplayDigest{4}, ExecutionStateVersion: 1, ExtrinsicsHex: []string{"0x00"}, ProofNodesHex: []string{"0x00"}}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "synthetic-proof.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return historicalReplayRequest{Engine: engineReference, Job: planFileReference{Path: path, Sha256: monitorReadDigest(raw)}, Budget: 300 * time.Second}
}

func TestHistoricalReplayPublicCommandRetainsUnknownFeeAndAuthority(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x00")
	t.Setenv("SYNTHETIC_REPLAY_INHERITED", "synthetic-not-forwarded")
	var stdout, stderr bytes.Buffer
	exit := runMain(t.Context(), []string{"verify-historical-execution", "--engine", request.Engine.Path, "--engine-sha256", request.Engine.Sha256, "--job", request.Job.Path, "--job-sha256", request.Job.Sha256}, &stdout, &stderr)
	var report historicalReplayReport
	if exit != 0 || decodePlanJson(stdout.Bytes(), &report) != nil || !report.PostStateReproduced || report.NativeFeeDebit != nil || report.NativeFeeWithdrawalRefund || report.RuntimeAdmitted || report.ProductionSelection || report.AnchorAuthority != "caller-supplied-unapproved" {
		t.Fatal("public execution caller changed proof authority", exit, stderr.String(), stdout.String())
	}
}

func TestHistoricalReplayReportBindsOriginalJobAndAuthority(t *testing.T) {
	for _, mode := range []string{"0x01", "0x02", "0x03", "0x04", "0x05", "0x09", "0x0a"} {
		request := historicalReplayTestRequest(t, mode)
		result, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
		if err == nil || result != nil {
			t.Fatal("foreign, partial or authority-forging replay result was accepted", mode, result, err)
		}
	}
}

func TestHistoricalReplayCancellationJoinsPartialOutput(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x06")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var once sync.Once
	pid := 0
	started := time.Now()
	report, err := runHistoricalReplay(ctx, request, historicalReplayHooks{afterStart: func(owner context.Context, value int) {
		pid = value
		deadline, ok := owner.Deadline()
		minimum, maximum := started.Add(request.Budget), time.Now().Add(request.Budget)
		if parent, present := ctx.Deadline(); present {
			if parent.Before(minimum) {
				minimum = parent
			}
			if parent.Before(maximum) {
				maximum = parent
			}
		}
		if !ok || deadline.Before(minimum) || deadline.After(maximum) {
			t.Error("default owned replay deadline differs", deadline, ok, minimum, maximum)
		}
	}, afterOutput: func() { once.Do(cancel) }})
	if !errors.Is(err, context.Canceled) || report != nil || pid == 0 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("partial replay cancellation lost cause or left a child", report, pid, err)
	}
}

// The test owns clock advancement, so instrumented engine admission cannot
// consume the intended post-launch deadline before that phase is reached. This
// context has its own Done channel; propagation must use its typed Err.
type historicalReplayTestDeadline struct {
	context.Context
	deadline time.Time
	done     chan struct{}
	once     sync.Once
}

func (self *historicalReplayTestDeadline) Deadline() (time.Time, bool) {
	return self.deadline, true
}
func (self *historicalReplayTestDeadline) Done() <-chan struct{} { return self.done }
func (self *historicalReplayTestDeadline) Err() error {
	select {
	case <-self.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}
func (self *historicalReplayTestDeadline) expire() { self.once.Do(func() { close(self.done) }) }

func historicalReplayControlledDeadline(t *testing.T) *historicalReplayTestDeadline {
	t.Helper()
	clock := &historicalReplayTestDeadline{Context: t.Context(), deadline: time.Now().Add(5 * time.Second), done: make(chan struct{})}
	stop := context.AfterFunc(t.Context(), clock.expire)
	t.Cleanup(func() {
		stop()
		clock.expire()
	})
	return clock
}

func TestHistoricalReplayOwnerDeadlineClipsChildAndJoins(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x06")
	ctx := historicalReplayControlledDeadline(t)
	pid, output := 0, false
	report, err := runHistoricalReplay(ctx, request, historicalReplayHooks{afterStart: func(owner context.Context, value int) {
		pid = value
		got, ok := owner.Deadline()
		want, _ := ctx.Deadline()
		if !ok || !got.Equal(want) {
			t.Error("child reset the caller deadline", got, want)
		}
	}, afterOutput: func() {
		output = true
		ctx.expire()
	}})
	if !errors.Is(err, context.DeadlineExceeded) || report != nil || pid == 0 || !output || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("owned replay deadline did not join actual started child", report, pid, output, err)
	}
}

func TestHistoricalReplayExpiredOwnerRefusesBeforeEngineAdmission(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x00")
	// An attempted open would produce a different, observable filesystem error.
	request.Engine.Path = filepath.Join(filepath.Dir(request.Job.Path), "absent-engine")
	ctx := historicalReplayControlledDeadline(t)
	ctx.expire()
	file, err := historicalReplayEngine(ctx, request.Engine)
	if file != nil || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired owner performed engine admission work", file, err)
	}
	started := false
	report, err := runHistoricalReplay(ctx, request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { started = true }})
	if report != nil || !errors.Is(err, context.DeadlineExceeded) || started {
		t.Fatal("prelaunch deadline was lost or started a child", report, started, err)
	}
}

func TestHistoricalReplayOutputBoundsCancelRealChild(t *testing.T) {
	for _, mode := range []string{"0x07", "0x08"} {
		request := historicalReplayTestRequest(t, mode)
		pid := 0
		report, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{afterStart: func(_ context.Context, value int) { pid = value }})
		if err == nil || !strings.Contains(err.Error(), "output exceeds bound") || report != nil || pid == 0 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			t.Fatal("replay output bound left a child or published partial evidence", mode, pid, report, err)
		}
	}
}

func TestHistoricalReplayInputPinsAndCanonicalDigestsPrecedeLaunch(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x00")
	original, err := os.ReadFile(request.Job.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{
		append([]byte(`{"schema":"duplicate",`), original[1:]...),
		bytes.Replace(original, []byte(`"parent_hash":[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]`), []byte(`"parent_hash":[1]`), 1),
	} {
		if bytes.Equal(raw, original) {
			t.Fatal("invalid input fixture did not change its wire bytes")
		}
		if err := os.WriteFile(request.Job.Path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		request.Job.Sha256 = monitorReadDigest(raw)
		started := false
		report, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{afterStart: func(context.Context, int) { started = true }})
		if err == nil || report != nil || started {
			t.Fatal("malformed replay input reached executable", err, started)
		}
	}
	request.Job.Sha256 = "sha256:" + strings.Repeat("e", 64)
	if report, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{}); err == nil || report != nil {
		t.Fatal("changed replay input pin was accepted")
	}
}

func TestHistoricalReplayCanceledOwnerDoesNotLaunch(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x00")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	started := false
	report, err := runHistoricalReplay(ctx, request, historicalReplayHooks{afterStart: func(context.Context, int) { started = true }})
	if !errors.Is(err, context.Canceled) || report != nil || started {
		t.Fatal("canceled owner invoked replay", err, started)
	}
}

func TestHistoricalReplayDescendantPipesCannotOutliveResult(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x0b")
	path := filepath.Join(filepath.Dir(request.Job.Path), "synthetic-child-pid")
	raw, err := os.ReadFile(request.Job.Path)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	job.ExtrinsicsHex = []string{"0x" + hex.EncodeToString([]byte(path))}
	raw, err = json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.Job.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request.Job.Sha256 = monitorReadDigest(raw)
	report, runErr := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
	pidRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("engine did not reach descendant boundary", err, runErr)
	}
	pid, err := strconv.Atoi(string(pidRaw))
	if err == nil && pid > 1 {
		// The old direct-child control intentionally leaves this owned peer;
		// always clean it after recording the intended assertion failure.
		defer syscall.Kill(pid, syscall.SIGKILL)
	}
	if err != nil || pid <= 1 || runErr == nil || report != nil || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("engine descendant survived or gained a valid result", pid, report, runErr, err)
	}
}

func TestHistoricalReplayCancellationJoinsBlockedInput(t *testing.T) {
	request := historicalReplayTestRequest(t, "0x00")
	raw, err := os.ReadFile(request.Job.Path)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	job.ParentHeaderHex = "0xb10c"
	job.ExtrinsicsHex = []string{"0x" + strings.Repeat("11", 8*1024*1024)}
	raw, err = json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.Job.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request.Job.Sha256 = monitorReadDigest(raw)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var once sync.Once
	pid := 0
	report, err := runHistoricalReplay(ctx, request, historicalReplayHooks{afterStart: func(_ context.Context, value int) { pid = value }, afterOutput: func() { once.Do(cancel) }})
	if !errors.Is(err, context.Canceled) || report != nil || pid == 0 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("cancellation left blocked replay input or process", pid, report, err)
	}
}

func TestHistoricalReplayPublicBoundedHostProfileRetainsUnknownAuthority(t *testing.T) {
	request := historicalReplayTestRequest(t, "0xc0")
	var stdout, stderr bytes.Buffer
	code := runMain(t.Context(), []string{"verify-historical-execution", "--engine", request.Engine.Path, "--engine-sha256", request.Engine.Sha256, "--job", request.Job.Path, "--job-sha256", request.Job.Sha256}, &stdout, &stderr)
	var report historicalReplayReport
	if code != 0 || decodePlanJson(stdout.Bytes(), &report) != nil || report.HostProfile != "substrate-proof-bounded-hosts-v2" || !report.PostStateReproduced || report.RuntimeAdmitted || report.NativeFeeWithdrawalRefund || report.NativeFeeDebit != nil || report.ProductionSelection {
		t.Fatal("bounded host support changed economic authority", code, stderr.String(), stdout.String())
	}
}

func TestHistoricalReplayBoundedHostProfilePreservesWorkAndAuthorityRefusal(t *testing.T) {
	for _, mode := range []string{"0xc1", "0xc2", "0xc3", "0xc4"} {
		request := historicalReplayTestRequest(t, mode)
		result, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
		if err == nil || result != nil {
			t.Fatal("new host profile bypassed exact work or authority refusal", mode, result, err)
		}
	}
}

func TestHistoricalReplayJobReadFailureRetainsCauseWithoutInventingIdentity(t *testing.T) {
	for _, fault := range []string{"canceled", "missing", "changed-digest"} {
		request := historicalReplayTestRequest(t, "0xc0")
		ctx, cancel := context.WithCancel(t.Context())
		var expected error
		switch fault {
		case "canceled":
			cancel()
			expected = context.Canceled
		case "missing":
			request.Job.Path += ".absent"
			expected = os.ErrNotExist
		case "changed-digest":
			request.Job.Sha256 = "sha256:" + strings.Repeat("a", 64)
		}
		started := false
		report, err := runHistoricalReplay(ctx, request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { started = true }})
		cancel()
		if report != nil || err == nil || started {
			t.Fatal("unread or mismatched job reached execution", fault, report, started, err)
		}
		contradiction := strings.Contains(err.Error(), "historical replay job differs from exact input pin")
		if expected != nil && (!errors.Is(err, expected) || contradiction) {
			t.Fatal("read refusal invented an input contradiction or lost its cause", fault, err)
		}
		if expected == nil && !contradiction {
			t.Fatal("actual completed-read digest mismatch lost its refusal", err)
		}
	}
}
