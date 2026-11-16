//go:build linux || darwin

package validator

// Actual M8 producers, disk ledgers, authenticated HTTP staging and independent
// fetch-back verification share a test process with foreground work. Counters
// describe executed work, not a simulated scheduler or production host sizing.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

type releaseForegroundHttpWork struct {
	posts, gets, postedBytes, fetchedBytes uint64
	active, activeBytes, peak, peakBytes   uint64
	maximumPostBytes                       uint64
}

// The server owns only transport bytes. Record and proof acceptance remains in
// the real production replay; a successful POST alone supplies no verdict.
type releaseForegroundHttp struct {
	stateLock sync.Mutex
	objects   map[string][]byte
	work      releaseForegroundHttpWork
	entered   chan struct{}
	release   chan struct{}
	postDone  chan struct{}
	first     atomic.Bool
	resume    sync.Once
	upload    *releaseAttemptUploadV2
	reader    *HTTPAttemptStreamV2Reader
}

func newReleaseForegroundHttp(t *testing.T, fixture *attemptCutV2SealTestFixture, stall bool) *releaseForegroundHttp {
	t.Helper()
	// This fixture crosses the actual public reader. Its in-memory replay
	// allowance must not advertise a header larger than public metadata.
	fixture.bounds.MaxHeaderBytes = attemptStreamV2MetadataBytes(fixture.bounds)
	bounds := fixture.bounds
	owner := &releaseForegroundHttp{objects: map[string][]byte{}, entered: make(chan struct{}), release: make(chan struct{}), postDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	const credential = "synthetic-foreground-session"
	maximum := max(uint64(1024*1024), bounds.Records.MaxChunkBytes, bounds.Proofs.MaxChunkBytes)
	endpoint := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		kind, hash := request.URL.Query().Get("kind"), request.URL.Query().Get("hash")
		if request.URL.Path != "/sn/attempt-artifact" || len(request.URL.Query()) != 2 {
			output.WriteHeader(http.StatusBadRequest)
			return
		}
		key := kind + "/" + hash
		switch request.Method {
		case http.MethodPost:
			if request.Header.Get("Authorization") != "Bearer "+credential || request.ContentLength <= 0 || uint64(request.ContentLength) > maximum {
				output.WriteHeader(http.StatusUnauthorized)
				return
			}
			raw, err := io.ReadAll(io.LimitReader(request.Body, int64(maximum)+1))
			if err != nil || int64(len(raw)) != request.ContentLength || attemptHex32(sha256.Sum256(raw)) != hash {
				output.WriteHeader(http.StatusBadRequest)
				return
			}
			owner.stateLock.Lock()
			owner.work.posts++
			owner.work.postedBytes += uint64(len(raw))
			owner.work.active++
			owner.work.activeBytes += uint64(len(raw))
			owner.work.peak = max(owner.work.peak, owner.work.active)
			owner.work.peakBytes = max(owner.work.peakBytes, owner.work.activeBytes)
			owner.work.maximumPostBytes = max(owner.work.maximumPostBytes, uint64(len(raw)))
			owner.stateLock.Unlock()
			blocked := false
			defer func() {
				owner.stateLock.Lock()
				owner.work.active--
				owner.work.activeBytes -= uint64(len(raw))
				owner.stateLock.Unlock()
				if blocked {
					close(owner.postDone)
				}
			}()
			if stall && kind == AttemptStreamV2Records && owner.first.CompareAndSwap(false, true) {
				blocked = true
				close(owner.entered)
				select {
				case <-owner.release:
				case <-request.Context().Done():
					return
				case <-ctx.Done():
					return
				}
			}
			owner.stateLock.Lock()
			prior, present := owner.objects[key]
			if present && !bytes.Equal(prior, raw) {
				owner.stateLock.Unlock()
				output.WriteHeader(http.StatusConflict)
				return
			}
			owner.objects[key] = bytes.Clone(raw)
			owner.stateLock.Unlock()
			output.Header().Set("ETag", `"`+hash+`"`)
			output.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			if request.Header.Get("Authorization") != "" {
				output.WriteHeader(http.StatusUnauthorized)
				return
			}
			owner.stateLock.Lock()
			raw, present := owner.objects[key]
			raw = bytes.Clone(raw)
			owner.work.gets++
			owner.stateLock.Unlock()
			if !present {
				output.WriteHeader(http.StatusNotFound)
				return
			}
			contentType := "application/x-ndjson"
			if kind == "metadata" {
				contentType = "application/json"
			}
			output.Header().Set("Content-Type", contentType)
			output.Header().Set("Content-Length", strconv.Itoa(len(raw)))
			written, _ := output.Write(raw)
			owner.stateLock.Lock()
			owner.work.fetchedBytes += uint64(written)
			owner.stateLock.Unlock()
		default:
			output.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(func() {
		cancel()
		if owner.upload != nil {
			owner.upload.close()
		}
		endpoint.CloseClientConnections()
		endpoint.Close()
	})
	var err error
	owner.upload, err = newReleaseAttemptUploadV2(ctx, OperatorConfig{NoID: 9, APIURL: endpoint.URL}, ReleaseEvidenceV2Bounds{Cut: bounds, MaxTransitionBytes: 1024 * 1024}, func() string { return credential })
	if err != nil {
		t.Fatal(err)
	}
	owner.reader, err = NewHTTPAttemptStreamV2Reader(endpoint.URL, bounds)
	if err != nil {
		t.Fatal(err)
	}
	if owner.reader.metadataBytes != bounds.MaxHeaderBytes || owner.upload.bounds != bounds {
		t.Fatal("foreground upload and public reader disagree on admitted bounds")
	}
	return owner
}

func (self *releaseForegroundHttp) allow() { self.resume.Do(func() { close(self.release) }) }

func (self *releaseForegroundHttp) snapshot() releaseForegroundHttpWork {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.work
}

func (self *releaseForegroundHttp) sealOptions(t *testing.T, fixture *attemptCutV2SealTestFixture) AttemptCutV2SealOptions {
	t.Helper()
	write := func(kind string) AttemptStreamV2ObjectWriter {
		return func(ctx context.Context, hash string, raw []byte) error {
			return self.upload.write(ctx, kind, hash, raw)
		}
	}
	return AttemptCutV2SealOptions{ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "foreground-seal"), ServerKeys: fixture.server.serverPublicKeys(), WriteRecords: write(AttemptStreamV2Records), WriteProofs: write(AttemptStreamV2Proofs), WriteMetadata: write("metadata"), ReadMetadata: self.reader.ReadMetadata, OpenData: self.reader.OpenData}
}

// Time is only a finite liveness guard. Entry/release channels define all
// ordering assertions, so scheduler speed is not used to create contention.
func releaseForegroundWait(t *testing.T, channel <-chan struct{}, purpose string) {
	t.Helper()
	select {
	case <-channel:
	case <-t.Context().Done():
		t.Fatal(purpose, t.Context().Err())
	case <-time.After(30 * time.Second):
		t.Fatal("owned work did not reach boundary", purpose)
	}
}

type releaseForegroundSealResult struct {
	cut      *AttemptCutV2
	verified AttemptCutV2ReplayResult
	err      error
}

func releaseForegroundSealJoin(t *testing.T, result <-chan releaseForegroundSealResult) releaseForegroundSealResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(30 * time.Second):
		t.Fatal("owned background did not join")
	}
	return releaseForegroundSealResult{}
}

// A source refusal must be reported at its actual boundary, not hidden behind
// a later wait for replay that can no longer occur. Cleanup still joins once.
func releaseForegroundSealWait(t *testing.T, boundary <-chan struct{}, result <-chan releaseForegroundSealResult, joined *bool, purpose string) {
	t.Helper()
	select {
	case <-boundary:
	case outcome := <-result:
		*joined = true
		t.Fatalf("sealer returned before %s: %v", purpose, outcome.err)
	case <-t.Context().Done():
		t.Fatal(purpose, t.Context().Err())
	case <-time.After(30 * time.Second):
		t.Fatal("owned sealer did not reach boundary", purpose)
	}
}

// A real foreground proof appends eight original signed rows on the same
// physical ledger while the sealer retains its already captured prefix.
func releaseForegroundTrail(t *testing.T, fixture *attemptCutV2SealTestFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	proof, err := fixture.engine.RunTrail(ctx)
	if err != nil || proof == nil || proof.M != 8 || len(proof.Hops) != 8 {
		t.Fatalf("foreground M8 trail did not progress under background work: %v", err)
	}
}

func releaseForegroundUpload(t *testing.T, owner *releaseForegroundHttp, ordinal int) {
	t.Helper()
	raw := []byte(fmt.Sprintf("{\"synthetic_foreground\":%d}\n", ordinal))
	hash := attemptHex32(sha256.Sum256(raw))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := owner.upload.write(ctx, "metadata", hash, raw); err != nil {
		t.Fatalf("independent upload waited behind another owned operation: %v", err)
	}
	observed, err := owner.reader.ReadMetadata(ctx, hash, uint64(len(raw)))
	if err != nil || !bytes.Equal(observed, raw) {
		t.Fatalf("foreground upload lost independent readback: %v", err)
	}
}

// Read only metadata after the actual owner has joined. This measures retained
// scratch bytes/files, not peak process RSS or an operator volume forecast.
func releaseForegroundScratch(t *testing.T, path string) (uint64, uint64) {
	t.Helper()
	var bytes, files uint64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() < 0 {
			return errors.New("foreground scratch contains nonregular evidence")
		}
		bytes += uint64(info.Size())
		files++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return bytes, files
}

func TestReleaseForegroundProgressDuringBlockedHttpAndReplay(t *testing.T) {
	lineage := newReleaseArchiveLineageFixture(t)
	archiveOptions, archiveWork := lineage.measuredOptions(t)
	archive, err := OpenReleaseEvidenceV2Archive(t.Context(), archiveOptions)
	if err != nil || archive == nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := archive.ReplayDecisions(t.Context(), lineage.observations); err != nil {
		t.Fatal(err)
	}
	before, beforeBytes := archiveWork.snapshot()
	for _, completed := range []int{1, 4, 8} {
		func() {
			fixture := newAttemptCutV2SealTestFixture(t, 8, completed, 0)
			transport := newReleaseForegroundHttp(t, fixture, true)
			options := transport.sealOptions(t, fixture)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			decoded, checked, indexed := atomic.Uint64{}, atomic.Uint64{}, atomic.Uint64{}
			cpuEntered, cpuRelease := make(chan struct{}), make(chan struct{})
			var cpuOnce sync.Once
			hooks := attemptCutV2SealHooks{Replay: attemptCutV2ReplayHooks{
				RecordDecoded: func() { decoded.Add(1) },
				RecordIndexed: func() { indexed.Add(1) },
				RecordChecked: func() {
					checked.Add(1)
					cpuOnce.Do(func() {
						close(cpuEntered)
						select {
						case <-cpuRelease:
						case <-ctx.Done():
						}
					})
				},
			}}
			result := make(chan releaseForegroundSealResult, 1)
			go func() {
				cut, verified, err := sealAttemptCutV2WithHooks(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, hooks)
				result <- releaseForegroundSealResult{cut: cut, verified: verified, err: err}
			}()
			joined := false
			defer func() {
				cancel()
				transport.allow()
				if !joined {
					releaseForegroundSealJoin(t, result)
				}
			}()
			for phase := range 2 {
				if phase == 0 {
					releaseForegroundSealWait(t, transport.entered, result, &joined, "actual upload body")
				} else {
					releaseForegroundSealWait(t, cpuEntered, result, &joined, "actual checked record")
				}
				releaseForegroundTrail(t, fixture)
				releaseForegroundUpload(t, transport, phase)
				artifact, decision, err := archive.Measurement(lineage.file.Current.MeasurementArtifactHash)
				if err != nil || artifact == nil || decision == nil || len(artifact.HeadEMA) == 0 {
					t.Fatalf("admitted nonempty lineage could not serve foreground read: %v", err)
				}
				after, afterBytes := archiveWork.snapshot()
				if !reflect.DeepEqual(before, after) || beforeBytes != afterBytes {
					t.Fatal("foreground replayed admitted full lineage")
				}
				if phase == 0 {
					transport.allow()
				} else {
					close(cpuRelease)
				}
			}
			outcome := releaseForegroundSealJoin(t, result)
			joined = true
			want := uint64(completed * 8)
			if outcome.err != nil || outcome.cut == nil || outcome.cut.LastSequence != want || outcome.verified.CompleteCount != uint64(completed) || decoded.Load() != want || checked.Load() != want || indexed.Load() != want {
				t.Fatalf("actual verification work or captured prefix differs: trails=%d decoded=%d checked=%d indexed=%d err=%v", completed, decoded.Load(), checked.Load(), indexed.Load(), outcome.err)
			}
			head, err := fixture.ledger.Head()
			if err != nil || head.LastSequence != want+16 || head.Root == outcome.cut.Root {
				t.Fatal("foreground rows were lost or absorbed by old prefix", head, err)
			}
			work := transport.snapshot()
			maximum := max(fixture.bounds.Records.MaxChunkBytes, fixture.bounds.Proofs.MaxChunkBytes, attemptStreamV2MetadataBytes(fixture.bounds))
			if work.active != 0 || work.activeBytes != 0 || work.peak != 2 || work.peakBytes > 2*maximum || work.maximumPostBytes > maximum || work.gets == 0 || work.fetchedBytes == 0 {
				t.Fatal("actual bounded HTTP ownership census differs", work)
			}
			if fixture.ledger.records != nil || fixture.ledger.pending != nil || fixture.ledger.terminal != nil {
				t.Fatal("disk owner grew whole-history resident maps")
			}
			scratchBytes, scratchFiles := releaseForegroundScratch(t, options.ScratchDirectory)
			spoolBound := (fixture.bounds.Records.MaxChunks + fixture.bounds.Proofs.MaxChunks) * attemptStreamV2DescriptorBytes
			if scratchBytes > fixture.replay.MaxScratchBytes+spoolBound || scratchFiles > fixture.replay.MaxScratchFiles+4 {
				t.Fatal("actual joined scratch exceeded its declared byte/file envelope", scratchBytes, scratchFiles)
			}
			t.Logf("trails=%d rows=%d verified=%d indexed=%d scratch_bytes=%d scratch_files=%d http=%+v; fixed32KiB data chunks, source-snapshot prefix only", completed, want, checked.Load(), indexed.Load(), scratchBytes, scratchFiles, work)
		}()
	}
}

func TestReleaseForegroundCancellationJoinsBlockedUploadWithoutLosingAppend(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 4, 0)
	transport := newReleaseForegroundHttp(t, fixture, true)
	options := transport.sealOptions(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan releaseForegroundSealResult, 1)
	go func() {
		cut, verified, err := SealAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		result <- releaseForegroundSealResult{cut: cut, verified: verified, err: err}
	}()
	joined := false
	defer func() {
		cancel()
		transport.allow()
		if !joined {
			releaseForegroundSealJoin(t, result)
		}
	}()
	releaseForegroundSealWait(t, transport.entered, result, &joined, "cancelable actual POST")
	releaseForegroundTrail(t, fixture)
	cancel()
	outcome := releaseForegroundSealJoin(t, result)
	joined = true
	releaseForegroundWait(t, transport.postDone, "canceled HTTP server request")
	if !errors.Is(outcome.err, context.Canceled) || outcome.cut != nil || outcome.verified != (AttemptCutV2ReplayResult{}) {
		t.Fatal("canceled upload published a partial cut", outcome.err)
	}
	head, err := fixture.ledger.Head()
	if err != nil || head.LastSequence != 40 {
		t.Fatal("cancellation lost a completed foreground trail", head, err)
	}
	work := transport.snapshot()
	if work.posts != 1 || work.active != 0 || work.activeBytes != 0 {
		t.Fatal("cancellation replayed POST or retained an HTTP owner", work)
	}
	// A new caller on the unchanged owner remains usable after one cancellation.
	releaseForegroundUpload(t, transport, 99)
	if _, err := os.Stat(options.ScratchDirectory); err != nil {
		t.Fatal("canceled evidence scratch was silently erased", err)
	}
}

// Unlike the held-boundary control, every verifier round runs to completion.
// Only initial admission is synchronized. Genuine signature/lifecycle/index
// work continues while independent foreground requests run; no verdict is
// replaced by a fixture and all decoded/checked/indexed rows are counted.
func TestReleaseForegroundProgressDuringContinuousActualReplay(t *testing.T) {
	// Sixteen retained M8 trails plus one foreground M8 need 136 records.
	// Admit that exact workload with a 2x count/byte margin before construction;
	// the original 128-record fixture remains an explicit refusal control below.
	limits := attemptLedgerDiskTestLimits()
	limits.MaxRecordCount, limits.MaxTrailCount = 2*17*8, 2*17
	limits.MaxRawRecordBytes = max(limits.MaxRawRecordBytes, limits.MaxRecordCount*limits.MaxRecordBytes)
	identity := AttemptLedgerIdentity{DeploymentID: "attempt-cut-v2-sealer-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}), Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 9}
	fixture := newAttemptCutV2SealTestFixtureForDomainWithLimits(t, 8, 16, 0, false, exactPolicy(t), identity, limits)
	if fixture.ledger.diskLimits != limits {
		t.Fatal("foreground workload owner did not retain its declared capacity")
	}
	transport := newReleaseForegroundHttp(t, fixture, false)
	options := transport.sealOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil || cut == nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, foregroundEntered, overlap := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first, live sync.Once
	decoded, checked, indexed := atomic.Uint64{}, atomic.Uint64{}, atomic.Uint64{}
	var beforeUsage, afterUsage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &beforeUsage); err != nil {
		t.Fatal(err)
	}
	var beforeMemory, afterMemory runtime.MemStats
	runtime.ReadMemStats(&beforeMemory)
	startedAt := time.Now()
	result := make(chan error, 1)
	go func() {
		for round := range 4 {
			replay := AttemptCutV2ReplayOptions{Bounds: fixture.replay, ScratchDirectory: filepath.Join(filepath.Dir(options.ScratchDirectory), fmt.Sprintf("actual-replay-%d", round)), ServerKeys: fixture.server.serverPublicKeys(), ReadMetadata: transport.reader.ReadMetadata, OpenData: transport.reader.OpenData}
			hooks := attemptCutV2ReplayHooks{RecordDecoded: func() { decoded.Add(1) }, RecordIndexed: func() { indexed.Add(1) }, RecordChecked: func() {
				count := checked.Add(1)
				first.Do(func() {
					close(started)
					select {
					case <-foregroundEntered:
					case <-ctx.Done():
					}
				})
				if count == 32 {
					live.Do(func() { close(overlap) })
				}
			}}
			verified, err := replayAttemptCutV2WithHooks(ctx, *cut, fixture.expected, fixture.bounds, replay, hooks)
			if err != nil || verified.CompleteCount != 16 {
				result <- errors.Join(err, errors.New("continuous actual replay did not complete original proof census"))
				return
			}
		}
		result <- nil
	}()
	joined := false
	defer func() {
		cancel()
		if !joined {
			select {
			case <-result:
			case <-time.After(30 * time.Second):
				t.Error("continuous actual replay did not join")
			}
		}
	}()
	releaseForegroundWait(t, started, "actual verifier started")
	// This boundary establishes that real verification progresses while a
	// foreground request exists. It does not hold the verifier after admission.
	var announce sync.Once
	fixture.engine.transport = attemptCutV2SealTestTransport(func(owner context.Context, hop connect.Id, raw []byte) ([]byte, error) {
		announce.Do(func() { close(foregroundEntered) })
		select {
		case <-overlap:
		case <-owner.Done():
			return nil, owner.Err()
		}
		return fixture.server.PostVerify(owner, hop, raw)
	})
	releaseForegroundTrail(t, fixture)
	releaseForegroundUpload(t, transport, 201)
	for range 8 {
		if _, err := fixture.ledger.Head(); err != nil {
			t.Fatal(err)
		}
		if len(fixture.engine.stats.ProviderIDs()) == 0 {
			t.Fatal("foreground provider census vanished")
		}
	}
	foregroundChecked := checked.Load()
	if foregroundChecked < 32 {
		t.Fatal("foreground did not overlap actual verified work", foregroundChecked)
	}
	select {
	case err := <-result:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("continuous bounded replay did not finish")
	}
	runtime.ReadMemStats(&afterMemory)
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &afterUsage); err != nil {
		t.Fatal(err)
	}
	if decoded.Load() != 512 || checked.Load() != 512 || indexed.Load() != 512 {
		t.Fatal("replay CPU/index work was omitted or repeated", decoded.Load(), checked.Load(), indexed.Load())
	}
	head, err := fixture.ledger.Head()
	if err != nil || head.LastSequence != 136 || cut.LastSequence != 128 {
		t.Fatal("continuous replay changed live prefix ownership", head, err)
	}
	scratchBytes, scratchFiles := releaseForegroundScratch(t, filepath.Dir(options.ScratchDirectory))
	spoolBound := (fixture.bounds.Records.MaxChunks + fixture.bounds.Proofs.MaxChunks) * attemptStreamV2DescriptorBytes
	if scratchBytes > 5*fixture.replay.MaxScratchBytes+spoolBound || scratchFiles > 5*(fixture.replay.MaxScratchFiles+4) {
		t.Fatal("continuous replay exceeded its declared aggregate scratch envelope", scratchBytes, scratchFiles)
	}
	micros := func(value syscall.Timeval) int64 { return value.Sec*1000000 + int64(value.Usec) }
	t.Logf("actual replay rows=512 rounds=4 foreground_observed_checked=%d elapsed=%s process_user_cpu_us=%d process_system_cpu_us=%d process_total_alloc_bytes=%d scratch_bytes=%d scratch_files=%d http=%+v; process measurements include fixture transports and foreground, not production cgroup sizing", foregroundChecked, time.Since(startedAt), micros(afterUsage.Utime)-micros(beforeUsage.Utime), micros(afterUsage.Stime)-micros(beforeUsage.Stime), afterMemory.TotalAlloc-beforeMemory.TotalAlloc, scratchBytes, scratchFiles, transport.snapshot())
}

// The original small owner still refuses its seventeenth trail. The extra
// capacity above is a fixture declaration, not a relaxed production ceiling.
func TestReleaseForegroundOriginalRecordCeilingStillRefusesAdditionalTrail(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 16, 0)
	head, err := fixture.ledger.Head()
	if err != nil || head.LastSequence != 128 || fixture.ledger.diskLimits.MaxRecordCount != 128 {
		t.Fatal("original workload did not reach its declared record ceiling", head, err)
	}
	proof, err := fixture.engine.RunTrail(t.Context())
	if proof != nil || !errors.Is(err, errAttemptRecordStoreLimit) {
		t.Fatal("original owner admitted a record beyond its unchanged ceiling", proof, err)
	}
	after, err := fixture.ledger.Head()
	if err != nil || after != head {
		t.Fatal("capacity refusal changed committed records or root", after, err)
	}
	fixture.engine.stats.mu.Lock()
	active := fixture.engine.stats.activeAttemptCount
	fixture.engine.stats.mu.Unlock()
	if active != 0 {
		t.Fatal("capacity refusal retained a foreground attempt owner", active)
	}
}

// Closing the source owner must cancel a held remote POST and join the seal;
// releasing its iterator between reads must not lose lifetime ownership.
func TestReleaseForegroundLedgerCloseCancelsAndJoinsHeldUpload(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 4, 0)
	transport := newReleaseForegroundHttp(t, fixture, true)
	options := transport.sealOptions(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan releaseForegroundSealResult, 1)
	go func() {
		cut, verified, err := SealAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		result <- releaseForegroundSealResult{cut: cut, verified: verified, err: err}
	}()
	joined := false
	defer func() {
		cancel()
		transport.allow()
		if !joined {
			releaseForegroundSealJoin(t, result)
		}
	}()
	releaseForegroundSealWait(t, transport.entered, result, &joined, "owner-close held POST")
	closed := make(chan error, 1)
	go func() { closed <- fixture.ledger.Close() }()
	outcome := releaseForegroundSealJoin(t, result)
	joined = true
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal("source owner close failed", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("source owner did not join its canceled sealer")
	}
	releaseForegroundWait(t, transport.postDone, "owner-close HTTP request")
	if !errors.Is(outcome.err, context.Canceled) || outcome.cut != nil || outcome.verified != (AttemptCutV2ReplayResult{}) {
		t.Fatal("source close published incomplete remote authority", outcome.err)
	}
	if work := transport.snapshot(); work.posts != 1 || work.active != 0 || work.activeBytes != 0 {
		t.Fatal("source close retained or repeated a remote owner", work)
	}
}

// Public upload admission must still reject one extra header byte without
// credentials or requests. A valid foreground owner remains usable afterward.
func TestReleaseForegroundHeaderAllowanceMatchesActualPublicMetadata(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	transport := newReleaseForegroundHttp(t, fixture, false)
	bounds := fixture.bounds
	bounds.MaxHeaderBytes++
	credentials := 0
	invalid, err := newReleaseAttemptUploadV2(t.Context(), OperatorConfig{NoID: 9, APIURL: transport.upload.origin}, ReleaseEvidenceV2Bounds{Cut: bounds, MaxTransitionBytes: 1024 * 1024}, func() string {
		credentials++
		return "synthetic-unavailable-credential"
	})
	if invalid != nil {
		invalid.close()
	}
	if invalid != nil || err == nil || !strings.Contains(err.Error(), "header exceeds its public metadata bound") || credentials != 0 || transport.snapshot() != (releaseForegroundHttpWork{}) {
		t.Fatal("incompatible public header reached transport work", invalid, err, credentials, transport.snapshot())
	}
	releaseForegroundUpload(t, transport, 301)
	if work := transport.snapshot(); work.posts != 1 || work.gets != 1 || work.active != 0 {
		t.Fatal("refused header changed the independent valid upload owner", work)
	}
}

// A declared record ceiling applies before scratch and HTTP work. Refusal
// preserves the actual ledger and does not stop an independently owned trail.
func TestReleaseForegroundCapacityRefusesBeforeBackgroundEffects(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 4, 0)
	transport := newReleaseForegroundHttp(t, fixture, false)
	options := transport.sealOptions(t, fixture)
	bounds := fixture.bounds
	bounds.Records.MaxItems = 31
	options.ReplayBounds.MaxTrails = 31
	cut, verified, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, bounds, options)
	if err == nil || !strings.Contains(err.Error(), "checked prefix exceeds the record count bound") || cut != nil || verified != (AttemptCutV2ReplayResult{}) {
		t.Fatal("record ceiling did not refuse at actual source admission", err)
	}
	if work := transport.snapshot(); work.posts != 0 || work.gets != 0 {
		t.Fatal("declared source ceiling spent background HTTP work", work)
	}
	if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("declared source ceiling allocated background scratch", err)
	}
	releaseForegroundTrail(t, fixture)
	head, err := fixture.ledger.Head()
	if err != nil || head.LastSequence != 40 {
		t.Fatal("capacity refusal changed foreground custody", head, err)
	}
}
