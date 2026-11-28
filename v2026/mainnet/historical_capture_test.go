// The Go fixture is an explicit synthetic protocol peer, not a Wasm executor.
// Rust tests use real complete tries and the exact SDK proof/replay functions.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"
)

func historicalCaptureSyntheticReport(input historicalCaptureInput, raw, code []byte) (historicalCaptureReport, error) {
	job := historicalReplayJob{Schema: historicalReplaySchema, ParentHeaderHex: input.ParentHeaderHex, ParentHash: input.ParentHash, ChildHeaderHex: input.ChildHeaderHex, ChildHash: input.ChildHash, ExtrinsicsHex: input.ExtrinsicsHex, RuntimeCodeHex: "0x" + hex.EncodeToString(code), RuntimeCodeSha256: input.RuntimeCodeSha256, RuntimeCodeBlake2b256: input.RuntimeCodeBlake2b256, ExecutionStateVersion: input.ExecutionStateVersion, ProofNodesHex: []string{"0x00"}, ObservationProfile: input.ObservationProfile}
	if input.ChildHash[0] == 4 {
		job.ParentHash[0] ^= 1
	}
	jobRaw, err := json.Marshal(job)
	if err != nil {
		return historicalCaptureReport{}, err
	}
	replay := historicalReplayReport{Schema: historicalReplaySchema, JobSha256: historicalReplayDigest(sha256.Sum256(jobRaw)), SdkRevision: historicalReplaySdk, HostProfile: "substrate-proof-bounded-hosts-v2", ParentHash: job.ParentHash, ChildHash: job.ChildHash, RuntimeCodeSha256: job.RuntimeCodeSha256, Extrinsics: uint64(len(job.ExtrinsicsHex)), ProofNodes: 1, ProofBytes: 1, StorageCalls: 1, StorageIoBytes: 1, PostStateReproduced: true, AnchorAuthority: "caller-supplied-unapproved"}
	if job.ObservationProfile != nil {
		replay.HookObservations = historicalObservationTestTrace(job)
	}
	report := historicalCaptureReport{Schema: historicalCaptureSchema, RequestSha256: historicalReplayDigest(sha256.Sum256(raw)), SdkRevision: historicalReplaySdk, CaptureMethod: "pinned-sdk-execution-proof-plus-strict-replay", BackendReads: 1, BackendReadBytes: 1, JobJSON: string(jobRaw), Replay: replay}
	switch input.ChildHash[0] {
	case 3:
		report.RequestSha256[0] ^= 1
	case 6:
		report.Replay.RuntimeAdmitted = true
	}
	return report, nil
}

func historicalCaptureSyntheticPeer() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, historicalCaptureRequestLimit+1))
	var input historicalCaptureInput
	if err != nil || len(raw) > historicalCaptureRequestLimit || decodePlanJson(raw, &input) != nil {
		os.Exit(7)
	}
	nodes := os.NewFile(4, "retained-test-nodes")
	info, err := nodes.Stat()
	if err != nil || !info.IsDir() {
		os.Exit(7)
	}
	code, err := os.ReadFile("/proc/self/fd/4/original-code")
	if err != nil {
		os.Exit(7)
	}
	if input.ChildHash[0] == 12 {
		_, _ = os.Stdout.WriteString("{\"schema\":")
		time.Sleep(24 * time.Hour)
		os.Exit(8)
	}
	report, err := historicalCaptureSyntheticReport(input, raw, code)
	if err != nil || json.NewEncoder(os.Stdout).Encode(report) != nil {
		os.Exit(7)
	}
}

func historicalCaptureTestRequest(t *testing.T, mode byte, code byte, observation bool) (historicalCaptureRequest, historicalCaptureInput) {
	t.Helper()
	base := historicalReplayTestRequest(t, "0x00")
	nodes := filepath.Join(filepath.Dir(base.Job.Path), "parent-nodes")
	if err := os.Mkdir(nodes, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodes, "original-code"), []byte{code}, 0600); err != nil {
		t.Fatal(err)
	}
	input := historicalCaptureInput{Schema: historicalCaptureSchema, ParentHeaderHex: "0x00", ParentHash: historicalReplayDigest{1}, ChildHeaderHex: "0x00", ChildHash: historicalReplayDigest{mode}, ExtrinsicsHex: []string{"0x00"}, RuntimeCodeSha256: historicalReplayDigest(sha256.Sum256([]byte{code})), RuntimeCodeBlake2b256: historicalReplayDigest(blake2b.Sum256([]byte{code})), ExecutionStateVersion: 1}
	if observation {
		metadata := historicalReplayDigest{17}
		input.ObservationProfile = &historicalReplayObservationProfile{Schema: "urnetwork-original-wasm-hook-observation-v1", RuntimeCodeSha256: input.RuntimeCodeSha256, SourceReviewSha256: historicalReplayDigest{19}, MetadataSha256: &metadata}
		for index, purpose := range []string{"fee-withdraw", "fee-refund", "ethereum-executed"} {
			input.ObservationProfile.Rules = append(input.ObservationProfile.Rules, historicalReplayHookRule{Purpose: purpose, FunctionIndex: uint32(index), FunctionBodySha256: historicalReplayDigest{byte(index + 1)}, OffsetStart: 0, OffsetEnd: 4})
		}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(base.Job.Path), "capture-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return historicalCaptureRequest{Engine: base.Engine, Input: planFileReference{Path: path, Sha256: monitorReadDigest(raw)}, Nodes: nodes, Budget: 300 * time.Second}, input
}

func TestHistoricalCapturePublicCommandRetainsExactJobAndUnknownAuthority(t *testing.T) {
	request, input := historicalCaptureTestRequest(t, 2, 0xe0, true)
	var stdout, stderr bytes.Buffer
	exit := runMain(t.Context(), []string{"capture-historical-execution", "--engine", request.Engine.Path, "--engine-sha256", request.Engine.Sha256, "--request", request.Input.Path, "--request-sha256", request.Input.Sha256, "--nodes", request.Nodes}, &stdout, &stderr)
	var report historicalCaptureReport
	if exit != 0 || decodePlanJson(stdout.Bytes(), &report) != nil {
		t.Fatal("public complete-proof caller did not return its exact job", exit, stderr.String())
	}
	if report.Replay.ParentHash != input.ParentHash || report.Replay.ChildHash != input.ChildHash || !report.Replay.PostStateReproduced || report.Replay.RuntimeAdmitted || report.Replay.NativeFeeDebit != nil || report.Replay.NativeFeeWithdrawalRefund || report.Replay.ProductionSelection || report.Replay.AnchorAuthority != "caller-supplied-unapproved" || report.Replay.HookObservations == nil || report.Replay.HookObservations.FeeEvents == nil {
		t.Fatal("capture changed the proof identity or promoted observation authority", report)
	}
	if candidates := report.Replay.HookObservations.FeeEvents.Candidates; len(candidates) != 1 || candidates[0].DebitRao == nil || *candidates[0].DebitRao != "750" || candidates[0].Status != "observed-pair-unadmitted" {
		t.Fatal("original unadmitted candidate was lost", candidates)
	}
}

func TestHistoricalCapturePublicRefusesForeignRequestJobOrAuthority(t *testing.T) {
	for _, mode := range []byte{3, 4, 6} {
		request, _ := historicalCaptureTestRequest(t, mode, 0, false)
		report, err := runHistoricalCapture(t.Context(), request, historicalReplayHooks{})
		if err == nil || report != nil {
			t.Fatal("foreign or authority-forging captured job became evidence", mode, report, err)
		}
	}
}

func TestHistoricalCaptureCancellationJoinsPartialProofOutput(t *testing.T) {
	request, _ := historicalCaptureTestRequest(t, 12, 0, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pid := 0
	report, err := runHistoricalCapture(ctx, request, historicalReplayHooks{afterStart: func(_ context.Context, value int) { pid = value }, afterOutput: cancel})
	if !errors.Is(err, context.Canceled) || report != nil || pid == 0 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("capture cancellation did not join the real child before refusing partial proof", pid, report, err)
	}
}

func TestHistoricalCaptureRetainsNodeDirectoryAcrossNamedReplacement(t *testing.T) {
	request, _ := historicalCaptureTestRequest(t, 2, 0, false)
	opened := false
	report, err := runHistoricalCapture(t.Context(), request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) {
		opened = true
		if err := os.Rename(request.Nodes, request.Nodes+".original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(request.Nodes, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(request.Nodes, "original-code"), []byte{99}, 0600); err != nil {
			t.Fatal(err)
		}
	}})
	if err != nil || report == nil || !opened || !report.Replay.PostStateReproduced {
		t.Fatal("named directory replacement retargeted retained proof input", opened, report, err)
	}
}

func TestHistoricalCaptureInputAndNodeRefusalsDoNotStartWorker(t *testing.T) {
	request, _ := historicalCaptureTestRequest(t, 2, 0, false)
	original := request
	for _, kind := range []string{"canceled", "request-pin", "node-link", "node-missing"} {
		request = original
		ctx, cancel := context.WithCancel(t.Context())
		switch kind {
		case "canceled":
			cancel()
		case "request-pin":
			request.Input.Sha256 = "sha256:" + strings.Repeat("f", 64)
		case "node-link":
			request.Nodes += ".link"
			if err := os.Symlink(original.Nodes, request.Nodes); err != nil {
				cancel()
				t.Fatal(err)
			}
		case "node-missing":
			request.Nodes += ".absent"
		}
		started := false
		report, err := runHistoricalCapture(ctx, request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { started = true }})
		cancel()
		if err == nil || report != nil || started {
			t.Fatal("unadmitted capture input started a worker", kind, started, report, err)
		}
		if kind == "canceled" && (!errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "differs")) {
			t.Fatal("canceled request was relabelled a changed identity", err)
		}
	}
}

func TestHistoricalCaptureReportPreservesExactBodyProfileAndResourceCensus(t *testing.T) {
	_, input := historicalCaptureTestRequest(t, 2, 0xe0, true)
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	good, err := historicalCaptureSyntheticReport(input, raw, []byte{0xe0})
	if err != nil || validateHistoricalCaptureReport(input, raw, good) != nil {
		t.Fatal("valid wire fixture refused before mutation", err)
	}
	for _, mutate := range []func(*historicalCaptureReport){
		func(report *historicalCaptureReport) { report.BackendReads = 65537 },
		func(report *historicalCaptureReport) { report.BackendReadBytes = 64*1024*1024 + 1 },
		func(report *historicalCaptureReport) { report.Replay.ProofBytes++ },
		func(report *historicalCaptureReport) { report.JobJSON += "{}" },
		func(report *historicalCaptureReport) {
			report.JobJSON = strings.Replace(report.JobJSON, `"extrinsics_hex":["0x00"]`, `"extrinsics_hex":[]`, 1)
		},
		func(report *historicalCaptureReport) {
			report.JobJSON = strings.Replace(report.JobJSON, `"execution_state_version":1`, `"execution_state_version":0`, 1)
		},
		func(report *historicalCaptureReport) {
			report.JobJSON = strings.Replace(report.JobJSON, `"fee-withdraw"`, `"fee-refund"`, 1)
		},
	} {
		report := good
		mutate(&report)
		if err := validateHistoricalCaptureReport(input, raw, report); err == nil {
			t.Fatal("changed capture body/profile/resource census became evidence", report)
		}
	}
}

func TestHistoricalCaptureObservedZeroNeverSubstitutesForMissingRefund(t *testing.T) {
	for _, code := range []byte{0xf0, 0xf1} {
		request, _ := historicalCaptureTestRequest(t, 2, code, true)
		report, err := runHistoricalCapture(t.Context(), request, historicalReplayHooks{})
		if err != nil || report == nil || report.Replay.HookObservations == nil || report.Replay.HookObservations.FeeEvents == nil || len(report.Replay.HookObservations.FeeEvents.Candidates) != 1 {
			t.Fatal("capture dropped a partial or exact-zero fee observation", code, report, err)
		}
		candidate := report.Replay.HookObservations.FeeEvents.Candidates[0]
		if code == 0xf0 && (candidate.RefundRao != nil || candidate.DebitRao != nil || candidate.Status != "refund-unobserved") || code == 0xf1 && (candidate.RefundRao == nil || *candidate.RefundRao != "0" || candidate.DebitRao == nil || *candidate.DebitRao != "1000") || report.Replay.NativeFeeWithdrawalRefund || report.Replay.NativeFeeDebit != nil {
			t.Fatal("capture fabricated zero or promoted a candidate into native fee authority", code, candidate)
		}
	}
}
