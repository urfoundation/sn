// These controls exercise actual private files, proof HTTP and joined owned
// process pipes. Synthetic HTTP replies grant no trie/VM correctness claim;
// the separately selected two-Rust-image root supplies that execution boundary.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/unix"
)

func nativeProducerTestFiles(t *testing.T, configure func(context.Context) context.Context) (context.Context, *nativeExecutionPolicy, *nativeProducerFiles) {
	t.Helper()
	base := t.TempDir()
	if err := os.Chmod(base, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "artifacts")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	storage := durablefixture.New(t, t.Context(), root)
	ctx := durablepath.WithHost(durablevolume.WithReference(t.Context(), storage.Reference), storage.Host)
	if configure != nil {
		ctx = configure(ctx)
	}
	policy := &nativeExecutionPolicy{Directory: root, Producer: &nativeExecutionProducerPolicy{MaximumJobs: 8, MaximumBytes: 4 * nativeProducerBoundaryReserve, MaximumEntries: 4 * nativeProducerBoundaryEntries, Nodes: filepath.Join(root, "nodes")}}
	files, err := openNativeProducerFiles(ctx, policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.close(); err != nil {
			t.Error(err)
		}
	})
	return ctx, policy, files
}

func TestNativeProducerFilesRefuseFifoAndChangedCustody(t *testing.T) {
	_, _, files := nativeProducerTestFiles(t, nil)
	ref, err := files.publish("job/original", []byte("original\n"), 64)
	if err != nil {
		t.Fatal("valid publication baseline", err)
	}
	if _, err := files.readReference(ref, 64); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(files.path, "job", "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := files.read("job/fifo", 64); err == nil || raw != nil {
		t.Fatal("FIFO admitted or bytes returned", err)
	}
	if err := os.Link(ref.Path, filepath.Join(files.path, "job", "alias")); err != nil {
		t.Fatal(err)
	}
	if raw, err := files.readReference(ref, 64); err == nil || raw != nil {
		t.Fatal("multiply linked evidence admitted", err)
	}
}

func TestNativeProducerPublicationReestablishesLostDirectorySync(t *testing.T) {
	syncs := 0
	_, _, files := nativeProducerTestFiles(t, func(ctx context.Context) context.Context {
		return context.WithValue(ctx, nativeProducerSyncKey{}, func(relative string, _ *os.File) error {
			if relative == "job/complete.json" {
				syncs++
				if syncs == 1 {
					return unix.EIO
				}
			}
			return nil
		})
	})
	raw := []byte("exact original completion\n")
	if _, err := files.publish("job/complete.json", raw, 128); !errors.Is(err, unix.EIO) {
		t.Fatal("lost sync control did not stop acknowledgement", err)
	}
	var before, after unix.Stat_t
	name := filepath.Join(files.path, "job", "complete.json")
	if err := unix.Stat(name, &before); err != nil {
		t.Fatal(err)
	}
	ref, err := files.publish("job/complete.json", raw, 128)
	if err != nil || syncs != 2 {
		t.Fatal("visible exact bytes bypassed original directory sync", syncs, err)
	}
	if err := unix.Stat(name, &after); err != nil {
		t.Fatal(err)
	}
	retained, err := files.readReference(ref, 128)
	if err != nil || before.Ino != after.Ino || before.Dev != after.Dev || !bytes.Equal(raw, retained) {
		t.Fatal("lost ack replaced original pending outcome", err)
	}
	if _, err := files.publish("job/complete.json", []byte("different"), 128); !errors.Is(err, errRpcIntegrity) {
		t.Fatal("conflicting completed evidence admitted", err)
	}
}

func TestNativeProducerPublicationReestablishesLostFileSync(t *testing.T) {
	syncs := 0
	_, _, files := nativeProducerTestFiles(t, func(ctx context.Context) context.Context {
		return context.WithValue(ctx, nativeProducerFileSyncKey{}, func(relative string, _ *os.File) error {
			if relative == "job/complete.json" {
				syncs++
				if syncs == 1 {
					return unix.EIO
				}
			}
			return nil
		})
	})
	raw := []byte("original completion after a lost file-sync acknowledgement\n")
	if _, err := files.publish("job/complete.json", raw, 256); !errors.Is(err, unix.EIO) {
		t.Fatal("file-sync control did not retain the exact pending write", err)
	}
	pending := filepath.Join(files.path, "job", "complete.json.pending")
	var before, after unix.Stat_t
	if err := unix.Stat(pending, &before); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSuffix(pending, ".pending")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed file sync published final evidence", err)
	}
	ref, err := files.publish("job/complete.json", raw, 256)
	if err != nil || syncs != 2 {
		t.Fatal("exact pending bytes bypassed a new file sync", syncs, err)
	}
	if err := unix.Stat(ref.Path, &after); err != nil {
		t.Fatal(err)
	}
	actual, err := files.readReference(ref, 256)
	if err != nil || !bytes.Equal(actual, raw) || before.Dev != after.Dev || before.Ino != after.Ino {
		t.Fatal("retry replaced the original pending inode or payload", err)
	}
}

func TestNativeProducerPendingPublicationRefusesPartialAndReplacedInode(t *testing.T) {
	for _, fault := range []string{"partial", "replaced"} {
		var root string
		syncs := 0
		raw := []byte("original pending evidence\n")
		_, _, files := nativeProducerTestFiles(t, func(ctx context.Context) context.Context {
			return context.WithValue(ctx, nativeProducerFileSyncKey{}, func(relative string, _ *os.File) error {
				if relative != "job/input.json" {
					return nil
				}
				syncs++
				if syncs == 1 {
					return unix.EIO
				}
				if fault == "replaced" {
					path := filepath.Join(root, "job", "input.json.pending")
					if err := os.Rename(path, path+".held"); err != nil {
						return err
					}
					return os.WriteFile(path, raw, 0600)
				}
				return nil
			})
		})
		root = files.path
		if _, err := files.publish("job/input.json", raw, 128); !errors.Is(err, unix.EIO) {
			t.Fatal("valid original pending baseline", fault, err)
		}
		pending := filepath.Join(root, "job", "input.json.pending")
		if fault == "partial" {
			if err := os.Truncate(pending, int64(len(raw)-1)); err != nil {
				t.Fatal(err)
			}
		}
		_, err := files.publish("job/input.json", raw, 128)
		if fault == "partial" && (!errors.Is(err, errRpcIntegrity) || syncs != 1) || fault == "replaced" && (!errors.Is(err, durablevolume.ErrIdentity) || syncs != 2) {
			t.Fatal("pending custody failed at an unrelated boundary or published", fault, err, syncs)
		}
		if _, err := os.Stat(filepath.Join(root, "job", "input.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid pending inode acquired final name", fault, err)
		}
		retainedPath, expected := pending, raw[:len(raw)-1]
		if fault == "replaced" {
			retainedPath, expected = pending+".held", raw
		}
		actual, err := os.ReadFile(retainedPath)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("refusal discarded or rewrote pending bytes", fault, err)
		}
	}
}

func nativeFeedTestFrame(t *testing.T, request historicalNativeFeedRequest) []byte {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(raw))), raw...)
}

func TestNativeProducerFeedRetainsOnlyProgressAtExactParent(t *testing.T) {
	ctx, _, files := nativeProducerTestFiles(t, nil)
	parent := "0x" + strings.Repeat("12", 32)
	root := "0x" + strings.Repeat("34", 32)
	node := []byte("candidate node checked by the later SDK trie")
	hash := blake2b.Sum256(node)
	missing := "0x" + hex.EncodeToString(hash[:])
	var calls atomic.Int64
	waits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     int               `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		count := calls.Add(1)
		var at string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &at) != nil || at != parent || call.Method != "state_getReadProof" {
			t.Error("feed chose another proof route or boundary")
			return
		}
		nodes := []string{}
		if count > 1 {
			nodes = append(nodes, nativeExecutionTestHex(node))
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": map[string]any{"at": parent, "proof": nodes}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.retryWait = func(context.Context, time.Duration) error {
		waits++
		if _, err := os.Stat(filepath.Join(files.path, "nodes", strings.TrimPrefix(missing, "0x"))); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("incomplete reply published a missing node", err)
		}
		return nil
	}
	feed := &historicalNativeFeed{files: files, client: client, requestSha256: monitorReadDigest([]byte("input")), parentHash: parent, parentStateRoot: root}
	request := historicalNativeFeedRequest{Schema: historicalNativeFeedSchema, Id: 1, RequestSha256: feed.requestSha256, ParentHash: parent, ParentStateRoot: root, Operation: "storage", ChildStorageKey: "0x", PrefixHex: "0x", MissingHash: missing}
	var output bytes.Buffer
	if err := feed.serve(ctx, bytes.NewReader(nativeFeedTestFrame(t, request)), &output); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || waits != 1 || output.Len() == 0 {
		t.Fatal("missing proof extended or skipped the original finite retry", calls.Load(), waits)
	}
	retained, err := files.read("nodes/"+strings.TrimPrefix(missing, "0x"), 1024)
	if err != nil || !bytes.Equal(retained, node) {
		t.Fatal("acknowledged node was not retained", err)
	}
	request.ParentHash = root
	output.Reset()
	if err := feed.serve(ctx, bytes.NewReader(nativeFeedTestFrame(t, request)), &output); !errors.Is(err, errRpcIntegrity) || output.Len() != 0 || calls.Load() != 2 {
		t.Fatal("foreign parent obtained proof work or an ACK", err)
	}
}

func TestNativeProducerFeedRefusesChildAliasAndMalformedPrefixBeforeRpc(t *testing.T) {
	_, _, files := nativeProducerTestFiles(t, nil)
	feed := &historicalNativeFeed{files: files, requestSha256: monitorReadDigest([]byte("input")), parentHash: "0x" + strings.Repeat("12", 32), parentStateRoot: "0x" + strings.Repeat("34", 32)}
	nibble := uint8(0xa0)
	valid := historicalNativeFeedRequest{Schema: historicalNativeFeedSchema, Id: 1, RequestSha256: feed.requestSha256, ParentHash: feed.parentHash, ParentStateRoot: feed.parentStateRoot, Operation: "child-next-key", ChildStorageKey: nativeExecutionTestHex([]byte(":child_storage:default:first")), PrefixHex: "0x1234", PrefixNibble: &nibble, MissingHash: "0x" + strings.Repeat("56", 32)}
	child, key, err := feed.probe(valid, 1)
	if err != nil || string(child) != ":child_storage:default:first" || !bytes.Equal(key, []byte{0x12, 0x34, 0xa0}) {
		t.Fatal("valid exact child/odd prefix baseline", err)
	}
	for _, kind := range []string{"nibble", "top-child", "empty-child", "foreign-child", "sequence", "operation"} {
		fault := valid
		switch kind {
		case "nibble":
			bad := uint8(0xa1)
			fault.PrefixNibble = &bad
		case "top-child":
			fault.Operation = "storage-root"
		case "empty-child":
			fault.ChildStorageKey = "0x"
		case "foreign-child":
			fault.ChildStorageKey = nativeExecutionTestHex([]byte("other"))
		case "sequence":
			fault.Id = 2
		case "operation":
			fault.Operation = "submit"
		}
		if _, _, err := feed.probe(fault, 1); !errors.Is(err, errRpcIntegrity) {
			t.Fatal("invalid request acquired a proof route", kind, err)
		}
	}
}

// This explicit gate requires distinct actual capture/replay Rust ELFs. Start
// with an empty owned cache, refill inside one capture, then reopen and replay
// its retained job with the network stopped. A synthetic dual-protocol Go peer
// cannot establish this scope or substitute for either executable pin.
func TestNativeProducerActualCaptureRefillAndRetainedReplay(t *testing.T) {
	capturePath, replayPath, fixturePath := os.Getenv("URNETWORK_NATIVE_CAPTURE_ENGINE"), os.Getenv("URNETWORK_NATIVE_EXECUTION_ENGINE"), os.Getenv("URNETWORK_NATIVE_EXECUTION_FIXTURE")
	if capturePath == "" && replayPath == "" && fixturePath == "" {
		t.Skip("requires explicit two-Rust-image native producer gate")
	}
	_, captureHash, err := readPlanFile(t.Context(), capturePath, historicalReplayEngineLimit)
	if err != nil {
		t.Fatal(err)
	}
	_, replayHash, err := readPlanFile(t.Context(), replayPath, historicalReplayEngineLimit)
	if err != nil {
		t.Fatal(err)
	}
	if captureHash == replayHash {
		t.Fatal("capture and replay must be their distinct real protocol images")
	}
	fixtureRaw, _, err := readPlanFile(t.Context(), fixturePath, historicalNativeJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	var original historicalReplayJob
	if err := decodePlanJson(fixtureRaw, &original); err != nil {
		t.Fatal(err)
	}
	parentRaw, err := historicalReplayHex(original.ParentHeaderHex, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	// Decode the canonical header with the independent GSRPC codec below.
	parentRoot, err := nativeProducerTestHeaderRoot(parentRaw)
	if err != nil {
		t.Fatal(err)
	}
	ctx, policy, files := nativeProducerTestFiles(t, nil)
	directory, err := files.child("nodes", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
	input := historicalCaptureInput{Schema: historicalCaptureSchema, ParentHeaderHex: original.ParentHeaderHex, ParentHash: original.ParentHash, ChildHeaderHex: original.ChildHeaderHex, ChildHash: original.ChildHash, ExtrinsicsHex: original.ExtrinsicsHex, RuntimeCodeSha256: original.RuntimeCodeSha256, RuntimeCodeBlake2b256: original.RuntimeCodeBlake2b256, ExecutionStateVersion: original.ExecutionStateVersion, ObservationProfile: original.ObservationProfile}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	inputRef, err := files.publish("job/input.json", raw, historicalCaptureRequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	parent := "0x" + hex.EncodeToString(original.ParentHash[:])
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     int               `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		requests.Add(1)
		var at string
		if call.Method != "state_getReadProof" || len(call.Params) != 2 || json.Unmarshal(call.Params[1], &at) != nil || at != parent {
			t.Error("real capture changed selected parent proof route")
			return
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": map[string]any{"at": parent, "proof": original.ProofNodesHex}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	feed := &historicalNativeFeed{files: files, client: client, requestSha256: inputRef.Sha256, parentHash: parent, parentStateRoot: parentRoot}
	report, err := runHistoricalCapture(ctx, historicalCaptureRequest{Engine: planFileReference{Path: capturePath, Sha256: captureHash}, Input: inputRef, Nodes: policy.Producer.Nodes, Budget: 300 * time.Second, Feed: feed}, historicalReplayHooks{})
	if err != nil || report == nil || requests.Load() == 0 {
		t.Fatal("actual VM did not refill empty original parent cache", err, requests.Load())
	}
	job, err := files.publish("job/job.json", []byte(report.JobJSON), historicalNativeJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := files.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openNativeProducerFiles(ctx, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := reopened.readReference(job, historicalNativeJobLimit); err != nil {
		t.Fatal(err)
	}
	replay, err := runHistoricalReplay(ctx, historicalReplayRequest{Engine: planFileReference{Path: replayPath, Sha256: replayHash}, Job: job, Budget: 300 * time.Second}, historicalReplayHooks{})
	if err != nil || !reflect.DeepEqual(&report.Replay, replay) {
		t.Fatal("distinct real replay image changed retained capture after restart", err)
	}
	if !replay.PostStateReproduced || replay.RuntimeAdmitted || replay.NativeFeeWithdrawalRefund {
		t.Fatal("real proof component fabricated approval")
	}
}

// Using the independent codec avoids deriving expected roots with the feed's
// own request reader. SCALE trailing data is disallowed by the caller's pin.
func nativeProducerTestHeaderRoot(raw []byte) (string, error) {
	var header types.Header
	if err := codec.Decode(raw, &header); err != nil {
		return "", err
	}
	encoded, err := codec.Encode(header)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(encoded, raw) {
		return "", errors.New("fixture native header contains trailing or noncanonical bytes")
	}
	return nativeExecutionTestHex(header.StateRoot[:]), nil
}
