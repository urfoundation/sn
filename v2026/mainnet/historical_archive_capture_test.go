// A synthetic protocol peer exercises actual command, RPC, cache and original
// process ownership. It is not a Wasm/trie execution or economic-proof claim.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/crypto/blake2b"
)

// The fixture asks for one exact parent node through the actual inherited
// FIFOs, then reads its immutable published bytes through the retained fd.
func historicalArchiveCaptureSyntheticPeer() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, historicalCaptureRequestLimit+1))
	var input historicalCaptureInput
	if err != nil || len(raw) > historicalCaptureRequestLimit || decodePlanJson(raw, &input) != nil {
		os.Exit(7)
	}
	parent, boundary, err := decodeEconomicFinalityHeader(input.ParentHeaderHex)
	if err != nil {
		os.Exit(7)
	}
	node := []byte("synthetic original parent node")
	nodeHash := blake2b.Sum256(node)
	hashHex := "0x" + hex.EncodeToString(nodeHash[:])
	request := historicalNativeFeedRequest{Schema: historicalNativeFeedSchema, Id: 1, RequestSha256: monitorReadDigest(raw), ParentHash: boundary.Hash, ParentStateRoot: parent.StateRoot, Operation: "storage", PrefixHex: "0x1234", ChildStorageKey: "0x", MissingHash: hashHex}
	encoded, err := json.Marshal(request)
	if err != nil {
		os.Exit(7)
	}
	writer, reader := os.NewFile(5, "original-feed-request"), os.NewFile(6, "original-feed-response")
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(encoded)))
	if historicalNativeFeedWrite(writer, append(frame, encoded...)) != nil {
		os.Exit(7)
	}
	var length [4]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil || binary.BigEndian.Uint32(length[:]) > historicalNativeFeedFrame {
		os.Exit(7)
	}
	responseRaw := make([]byte, binary.BigEndian.Uint32(length[:]))
	var response historicalNativeFeedResponse
	if _, err := io.ReadFull(reader, responseRaw); err != nil || decodePlanJson(responseRaw, &response) != nil || response.Schema != historicalNativeFeedSchema || response.Id != 1 || response.RequestSha256 != request.RequestSha256 || response.MissingHash != hashHex || !response.Retained {
		os.Exit(7)
	}
	retained, err := os.ReadFile("/proc/self/fd/4/" + strings.TrimPrefix(hashHex, "0x"))
	if err != nil || !bytes.Equal(retained, node) {
		os.Exit(7)
	}
	code := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	job := historicalReplayJob{Schema: historicalReplaySchema, ParentHeaderHex: input.ParentHeaderHex, ParentHash: input.ParentHash, ChildHeaderHex: input.ChildHeaderHex, ChildHash: input.ChildHash, ExtrinsicsHex: input.ExtrinsicsHex, RuntimeCodeHex: nativeExecutionTestHex(code), RuntimeCodeSha256: input.RuntimeCodeSha256, RuntimeCodeBlake2b256: input.RuntimeCodeBlake2b256, ExecutionStateVersion: input.ExecutionStateVersion, ObservationProfile: input.ObservationProfile, ProofNodesHex: []string{nativeExecutionTestHex(node)}}
	jobRaw, err := json.Marshal(job)
	if err != nil {
		os.Exit(7)
	}
	profileRaw, err := json.Marshal(input.ObservationProfile)
	if err != nil {
		os.Exit(7)
	}
	trace := &historicalReplayObservations{ProfileSha256: historicalReplayDigest(sha256.Sum256(profileRaw)), SourceReviewSha256: input.ObservationProfile.SourceReviewSha256, Authority: "caller-supplied-unapproved-callsite-profile", OriginalFunctionBodiesPreserved: true, HostCalls: 1, Observations: []historicalReplayObservation{}}
	replay := historicalReplayReport{Schema: historicalReplaySchema, JobSha256: historicalReplayDigest(sha256.Sum256(jobRaw)), SdkRevision: historicalReplaySdk, HostProfile: "substrate-proof-bounded-hosts-v2", ParentHash: input.ParentHash, ChildHash: input.ChildHash, RuntimeCodeSha256: input.RuntimeCodeSha256, Extrinsics: uint64(len(input.ExtrinsicsHex)), ProofNodes: 1, ProofBytes: uint64(len(node)), StorageCalls: 1, StorageIoBytes: uint64(len(node)), PostStateReproduced: true, AnchorAuthority: "caller-supplied-unapproved", HookObservations: trace}
	report := historicalCaptureReport{Schema: historicalCaptureSchema, RequestSha256: historicalReplayDigest(sha256.Sum256(raw)), SdkRevision: historicalReplaySdk, CaptureMethod: "pinned-sdk-execution-proof-plus-strict-replay", BackendReads: 1, BackendReadBytes: uint64(len(node)), JobJSON: string(jobRaw), Replay: replay}
	if json.NewEncoder(os.Stdout).Encode(report) != nil {
		os.Exit(7)
	}
}

type historicalArchiveCaptureFixture struct {
	ctx      context.Context
	request  historicalArchiveCaptureRequest
	input    historicalCaptureInput
	original *historicalCaptureRpcFixture
	proofs   atomic.Int64
	closing  atomic.Bool
}

// Real private directory custody and the existing exact-header RPC fixture
// are shared; only the external runtime protocol is synthetic.
func newHistoricalArchiveCaptureFixture(t *testing.T, mode string) *historicalArchiveCaptureFixture {
	t.Helper()
	original := newHistoricalCaptureRpcFixture(t, mode)
	ctx, policy, files := nativeProducerTestFiles(t, nil)
	if err := files.close(); err != nil {
		t.Fatal(err)
	}
	base := historicalReplayTestRequest(t, "0x00")
	parent, err := historicalCaptureHeaderHex(original.parent, original.parentHash)
	if err != nil {
		t.Fatal(err)
	}
	child, err := historicalCaptureHeaderHex(original.child, original.childHash)
	if err != nil {
		t.Fatal(err)
	}
	parentHash, _ := historicalCaptureHash(original.parentHash)
	childHash, _ := historicalCaptureHash(original.childHash)
	codeHash := historicalReplayDigest(sha256.Sum256(original.code))
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema, RuntimeCodeSha256: codeHash, SourceReviewSha256: historicalReplayDigest{19}, Rules: []historicalReplayHookRule{{Purpose: "native-epoch", FunctionIndex: 1, FunctionBodySha256: historicalReplayDigest{2}, OffsetStart: 1, OffsetEnd: 4}}}
	f := &historicalArchiveCaptureFixture{ctx: ctx, original: original, input: historicalCaptureInput{Schema: historicalCaptureSchema, ParentHeaderHex: parent, ParentHash: parentHash, ChildHeaderHex: child, ChildHash: childHash, ExtrinsicsHex: original.body, RuntimeCodeSha256: codeHash, RuntimeCodeBlake2b256: historicalReplayDigest(blake2b.Sum256(original.code)), ExecutionStateVersion: 1, ObservationProfile: profile}}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, historicalNativeFeedFrame+1))
		if err != nil || len(raw) > historicalNativeFeedFrame {
			t.Error("fixture request bound", err)
			return
		}
		var call struct {
			Id     int               `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &call) != nil {
			t.Error("fixture request grammar")
			return
		}
		if call.Method != "state_getReadProof" {
			if call.Method == "chain_getBlockHash" && f.closing.Load() {
				if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": original.expected.GenesisHash}); err != nil {
					t.Error(err)
				}
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			original.server.Config.Handler.ServeHTTP(writer, request)
			return
		}
		f.proofs.Add(1)
		var at string
		var keys []string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &keys) != nil || json.Unmarshal(call.Params[1], &at) != nil || at != original.parentHash || len(keys) != 1 || keys[0] != "0x1234" {
			t.Error("archive feed escaped its exact selected parent/probe")
			return
		}
		if mode == "cancel-proof" {
			close(original.entered)
			<-request.Context().Done()
			close(original.joined)
			return
		}
		if mode == "wrong-proof-parent" {
			at = original.childHash
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": map[string]any{"at": at, "proof": []string{nativeExecutionTestHex([]byte("synthetic original parent node"))}}}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	f.request = historicalArchiveCaptureRequest{Capture: historicalCaptureRequest{Engine: base.Engine, Input: planFileReference{Path: filepath.Join(filepath.Dir(base.Job.Path), "archive-request.json")}, Budget: 300 * time.Second}, Rpc: server.URL, Expected: original.expected, CacheDirectory: policy.Directory, MaximumBytes: policy.Producer.MaximumBytes, MaximumEntries: policy.Producer.MaximumEntries}
	f.write(t)
	return f
}

func (self *historicalArchiveCaptureFixture) write(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(self.input)
	if err != nil {
		t.Fatal(err)
	}
	self.request.Capture.Input.Sha256 = monitorReadDigest(raw)
	if err := os.WriteFile(self.request.Capture.Input.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalArchiveCapturePublicCommandRefillsAndRetainsUnsignedJob(t *testing.T) {
	f := newHistoricalArchiveCaptureFixture(t, "success")
	var output, diagnostic bytes.Buffer
	request := f.request
	code := runMain(f.ctx, []string{"capture-historical-execution", "--engine", request.Capture.Engine.Path, "--engine-sha256", request.Capture.Engine.Sha256, "--request", request.Capture.Input.Path, "--request-sha256", request.Capture.Input.Sha256, "--rpc", request.Rpc, "--cache-dir", request.CacheDirectory, "--expected-chain", request.Expected.NativeChain, "--expected-genesis", request.Expected.GenesisHash, "--expected-evm-chain-id", "964"}, &output, &diagnostic)
	var report historicalCaptureReport
	if code != 0 || decodePlanJson(output.Bytes(), &report) != nil || f.proofs.Load() != 1 || !report.Replay.PostStateReproduced || report.Replay.RuntimeAdmitted || report.Replay.NativeFeeWithdrawalRefund || report.Replay.AnchorAuthority != "caller-supplied-unapproved" {
		t.Fatal("public unsigned archive capture refused or invented authority", code, diagnostic.String(), f.proofs.Load())
	}
	jobPath := filepath.Join(request.CacheDirectory, "jobs", strings.TrimPrefix(monitorReadDigest([]byte(report.JobJSON)), "sha256:")+".json")
	raw, err := os.ReadFile(jobPath)
	if err != nil || string(raw) != report.JobJSON {
		t.Fatal("exported job bytes were not retained exactly", err)
	}
	files, err := openNativeEvidenceFiles(f.ctx, request.CacheDirectory, request.MaximumBytes, request.MaximumEntries)
	if err != nil {
		t.Fatal("archive owner did not release its original lock", err)
	}
	if err := files.close(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalArchiveCaptureRejectsChangedChainInputsBeforeWorker(t *testing.T) {
	for _, fault := range []string{"pin", "parent-hash", "child-header", "body", "code", "state-version", "metadata", "expected-network", "legacy-profile"} {
		f := newHistoricalArchiveCaptureFixture(t, "success")
		switch fault {
		case "pin":
			f.request.Capture.Input.Sha256 = "sha256:" + strings.Repeat("ab", 32)
		case "parent-hash":
			f.input.ParentHash[0] ^= 1
		case "child-header":
			f.input.ChildHeaderHex = f.input.ParentHeaderHex
		case "body":
			f.input.ExtrinsicsHex = []string{"0x0c010204"}
		case "code":
			f.input.RuntimeCodeSha256[0] ^= 1
			f.input.ObservationProfile.RuntimeCodeSha256 = f.input.RuntimeCodeSha256
		case "state-version":
			f.input.ExecutionStateVersion = 0
		case "metadata":
			metadata := historicalReplayDigest{9}
			f.input.ObservationProfile.MetadataSha256 = &metadata
		case "expected-network":
			f.request.Expected.EvmChainId++
		case "legacy-profile":
			f.input.ObservationProfile.Schema = "urnetwork-original-wasm-hook-observation-v1"
		}
		if fault != "pin" {
			f.write(t)
		}
		started := false
		report, err := runHistoricalArchiveCapture(f.ctx, f.request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { started = true }})
		if err == nil || report != nil || started || f.proofs.Load() != 0 {
			t.Fatal("changed original input acquired capture process", fault, report, started, err)
		}
	}
}

func TestHistoricalArchiveCaptureKeepsExplicitPrincipalInputAndParentDeadline(t *testing.T) {
	f := newHistoricalArchiveCaptureFixture(t, "success")
	f.input.PrincipalQueries = []historicalPrincipalQuery{historicalPrincipalTestValue(14).Query}
	f.input.PrincipalEffects = true
	f.write(t)
	deadline := time.Now().Add(45 * time.Second)
	ctx, cancel := context.WithDeadline(f.ctx, deadline)
	defer cancel()
	admitted, started := false, false
	report, err := runHistoricalArchiveCapture(ctx, f.request, historicalReplayHooks{
		beforeStart: func(owner context.Context, _ *os.File) {
			admitted = true
			actual, ok := owner.Deadline()
			if !ok || !actual.Equal(deadline) {
				t.Error("archive reopened its inherited deadline", actual, deadline)
			}
			cancel()
		},
		afterStart: func(context.Context, int) { started = true },
	})
	if !admitted || started || report != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("unsigned principal input was altered or acquired renewed process budget", admitted, started, report, err)
	}
	raw, err := os.ReadFile(filepath.Join(f.request.CacheDirectory, "inputs", strings.TrimPrefix(f.request.Capture.Input.Sha256, "sha256:")+".json"))
	original, readErr := os.ReadFile(f.request.Capture.Input.Path)
	if err != nil || readErr != nil || !bytes.Equal(raw, original) {
		t.Fatal("principal request bytes changed", err, readErr)
	}
}

func TestHistoricalArchiveCaptureRefusesUndeclaredBusyOrExhaustedCache(t *testing.T) {
	for _, fault := range []string{"undeclared", "busy", "bytes", "entries", "full-cache", "foreign-member", "shared-directory"} {
		f := newHistoricalArchiveCaptureFixture(t, "success")
		ctx := f.ctx
		var held *nativeProducerFiles
		switch fault {
		case "undeclared":
			ctx = t.Context()
		case "busy":
			var err error
			held, err = openNativeEvidenceFiles(ctx, f.request.CacheDirectory, f.request.MaximumBytes, f.request.MaximumEntries)
			if err != nil {
				t.Fatal(err)
			}
		case "bytes":
			f.request.MaximumBytes = 2*nativeProducerBoundaryReserve - 1
		case "entries":
			f.request.MaximumEntries = 2*nativeProducerBoundaryEntries - 1
		case "full-cache":
			file, err := os.OpenFile(filepath.Join(f.request.CacheDirectory, "retained"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(file.Truncate(int64(f.request.MaximumBytes-nativeProducerBoundaryReserve+1)), file.Close()); err != nil {
				t.Fatal(err)
			}
		case "foreign-member", "shared-directory":
			path := filepath.Join(f.request.CacheDirectory, "unprotected")
			mode := os.FileMode(0644)
			if fault == "shared-directory" {
				mode = 0755
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte{1}, 0600); err != nil {
				t.Fatal(err)
			}
			// The qualified owner uses umask 077. Establish the actual fault on
			// this test-owned inode; a requested creation mode is not evidence.
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode().Perm() != mode || info.IsDir() != (fault == "shared-directory") || !info.IsDir() && !info.Mode().IsRegular() {
				t.Fatal("cache custody fixture did not establish its actual permission fault", fault, info, err)
			}
		}
		started := false
		report, err := runHistoricalArchiveCapture(ctx, f.request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { started = true }})
		if held != nil {
			if closeErr := held.close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		if err == nil || report != nil || started || f.original.artifactReads.Load() != 0 || fault == "busy" && !errors.Is(err, durablevolume.ErrBusy) {
			t.Fatal("unadmitted cache acquired network or worker", fault, started, report, err)
		}
	}
}

func TestHistoricalArchiveCaptureCancellationJoinsOriginalProofAndProcess(t *testing.T) {
	f := newHistoricalArchiveCaptureFixture(t, "cancel-proof")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	pid := 0
	go func() {
		report, err := runHistoricalArchiveCapture(ctx, f.request, historicalReplayHooks{afterStart: func(_ context.Context, actual int) { pid = actual }})
		if report != nil {
			t.Error("canceled capture returned a proof report")
		}
		done <- err
	}()
	select {
	case <-f.original.entered:
	case err := <-done:
		t.Fatal("capture never reached original proof request", err)
	case <-time.After(30 * time.Second):
		t.Fatal("fixture did not reach original proof request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || pid == 0 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			t.Fatal("original process was not joined on capture cancellation", pid, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("capture did not join its canceled process and proof")
	}
	select {
	case <-f.original.joined:
	case <-time.After(30 * time.Second):
		t.Fatal("proof HTTP request survived original owner cancellation")
	}
	files, err := openNativeEvidenceFiles(f.ctx, f.request.CacheDirectory, f.request.MaximumBytes, f.request.MaximumEntries)
	if err != nil {
		t.Fatal("canceled capture leaked exclusive cache custody", err)
	}
	if err := files.close(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalArchiveCaptureRejectsWrongProofParentAndClosingFinality(t *testing.T) {
	for _, fault := range []string{"wrong-proof-parent", "closing-finality"} {
		f := newHistoricalArchiveCaptureFixture(t, fault)
		hooks := historicalReplayHooks{}
		if fault == "closing-finality" {
			hooks.afterOutput = func() { f.closing.Store(true) }
		}
		report, err := runHistoricalArchiveCapture(f.ctx, f.request, hooks)
		if err == nil || report != nil || f.proofs.Load() != 1 {
			t.Fatal("changed proof or closing original became a result", fault, report, err)
		}
		jobs, readErr := os.ReadDir(filepath.Join(f.request.CacheDirectory, "jobs"))
		if readErr == nil && len(jobs) != 0 || readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			t.Fatal("refused capture published a completed job", fault, jobs, readErr)
		}
	}
}

func TestHistoricalArchiveCaptureRetainsPinnedInputAcrossOriginalPathReplacement(t *testing.T) {
	f := newHistoricalArchiveCaptureFixture(t, "success")
	checked := false
	report, err := runHistoricalArchiveCapture(f.ctx, f.request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) {
		checked = true
		if err := os.Rename(f.request.Capture.Input.Path, f.request.Capture.Input.Path+".retained"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.request.Capture.Input.Path, []byte("different request"), 0600); err != nil {
			t.Fatal(err)
		}
	}})
	if err != nil || report == nil || !checked || f.proofs.Load() != 1 {
		t.Fatal("path replacement redirected the pinned capture", report, checked, err)
	}
	var job historicalReplayJob
	if err := decodePlanJson([]byte(report.JobJSON), &job); err != nil || job.ParentHash != f.input.ParentHash || job.ChildHash != f.input.ChildHash || job.RuntimeCodeSha256 != f.input.RuntimeCodeSha256 {
		t.Fatal("immutable original input changed", err)
	}
}

func TestHistoricalArchiveCaptureCommandRejectsMixedModesAndUnboundedCache(t *testing.T) {
	f := newHistoricalArchiveCaptureFixture(t, "success")
	base := []string{"capture-historical-execution", "--engine", f.request.Capture.Engine.Path, "--engine-sha256", f.request.Capture.Engine.Sha256, "--request", f.request.Capture.Input.Path, "--request-sha256", f.request.Capture.Input.Sha256}
	for _, extra := range [][]string{
		{"--nodes", f.request.CacheDirectory, "--rpc", f.request.Rpc},
		{"--nodes", f.request.CacheDirectory, "--cache-max-entries", "1"},
		{"--nodes", f.request.CacheDirectory, "--owner-local-cache"},
		{"--cache-dir", f.request.CacheDirectory},
		{"--rpc", f.request.Rpc, "--cache-dir", f.request.CacheDirectory, "--expected-chain", f.request.Expected.NativeChain, "--expected-genesis", f.request.Expected.GenesisHash, "--expected-evm-chain-id", "964", "--cache-max-bytes", "18446744073709551615"},
	} {
		var output, diagnostic bytes.Buffer
		if code := runMain(f.ctx, append(append([]string{}, base...), extra...), &output, &diagnostic); code != 2 || output.Len() != 0 {
			t.Fatal("ambiguous or unbounded archive mode admitted", extra, code, diagnostic.String())
		}
	}
	if f.original.artifactReads.Load() != 0 || f.proofs.Load() != 0 {
		t.Fatal("invalid CLI input acquired a network route")
	}
}

func TestHistoricalArchiveCaptureExplicitOwnerLocalCommandRetainsUnsignedEvidence(t *testing.T) {
	f := newHistoricalArchiveCaptureFixture(t, "success")
	ctx := ownerLocalDurableTestContext(t, f.request.CacheDirectory)
	request := f.request
	var output, diagnostic bytes.Buffer
	code := runMain(ctx, []string{"capture-historical-execution", "--engine", request.Capture.Engine.Path, "--engine-sha256", request.Capture.Engine.Sha256, "--request", request.Capture.Input.Path, "--request-sha256", request.Capture.Input.Sha256, "--rpc", request.Rpc, "--cache-dir", request.CacheDirectory, "--owner-local-cache", "--expected-chain", request.Expected.NativeChain, "--expected-genesis", request.Expected.GenesisHash, "--expected-evm-chain-id", "964"}, &output, &diagnostic)
	var report historicalCaptureReport
	if code != 0 || decodePlanJson(output.Bytes(), &report) != nil || f.proofs.Load() != 1 || report.Replay.RuntimeAdmitted || report.Replay.AnchorAuthority != "caller-supplied-unapproved" {
		t.Fatal("explicit owner-local evidence capture failed or granted authority", code, diagnostic.String())
	}
	jobPath := filepath.Join(request.CacheDirectory, "jobs", strings.TrimPrefix(monitorReadDigest([]byte(report.JobJSON)), "sha256:")+".json")
	raw, err := os.ReadFile(jobPath)
	if err != nil || string(raw) != report.JobJSON {
		t.Fatal("owner-local capture lost exact original job", err)
	}
	files, err := openNativeEvidenceFilesInScope(ctx, request.CacheDirectory, request.MaximumBytes, request.MaximumEntries, true)
	if err != nil {
		t.Fatal("owner-local capture did not release original custody", err)
	}
	if err := files.close(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalArchiveCaptureOwnerLocalCannotRelabelDaemonProducer(t *testing.T) {
	for _, fault := range []string{"owner-declaration-default-archive", "daemon-declaration-local-archive", "owner-declaration-producer"} {
		f := newHistoricalArchiveCaptureFixture(t, "success")
		ctx := ownerLocalDurableTestContext(t, f.request.CacheDirectory)
		started := false
		var err error
		if fault == "owner-declaration-producer" {
			files, openErr := openNativeProducerFiles(ctx, &nativeExecutionPolicy{Directory: f.request.CacheDirectory, Producer: &nativeExecutionProducerPolicy{MaximumBytes: f.request.MaximumBytes, MaximumEntries: f.request.MaximumEntries}})
			err = openErr
			if files != nil {
				_ = files.close()
				t.Fatal("owner-local declaration acquired signed-producer storage")
			}
		} else {
			if fault == "daemon-declaration-local-archive" {
				ctx = f.ctx
				f.request.OwnerLocalCache = true
			}
			report, captureErr := runHistoricalArchiveCapture(ctx, f.request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { started = true }})
			err = captureErr
			if report != nil {
				t.Fatal("wrong storage scope returned evidence")
			}
		}
		if err == nil || started || f.original.artifactReads.Load() != 0 || f.proofs.Load() != 0 {
			t.Fatal("declaration filename silently changed storage authority", fault, started, err)
		}
	}
}
