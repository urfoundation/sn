//go:build linux || darwin

// Phase-shaped payload traffic and the complete configured metadata census
// are separate measurements. Neither represents a 384 GiB payload execution.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/server"
	"github.com/urnetwork/server/startifact"
)

// Only descriptors and byte counts survive between reads. Atomic counters
// cover real Http handlers as well as the synchronous publisher.
type campaignPopulationReadMetricsV2 struct {
	active atomic.Uint64
	peak   atomic.Uint64
	reads  atomic.Uint64
	bytes  atomic.Uint64
}

// Acquire a real reader without copying or replacing any returned bytes.
func (self *campaignPopulationReadMetricsV2) own(reader io.ReadCloser) io.ReadCloser {
	active := self.active.Add(1)
	self.reads.Add(1)
	for previous := self.peak.Load(); previous < active; previous = self.peak.Load() {
		if self.peak.CompareAndSwap(previous, active) {
			break
		}
	}
	return &campaignPopulationReaderV2{ReadCloser: reader, metrics: self}
}

// One successful Close releases exactly one descriptor/body owner. These
// counters are not Go heap or resident-set estimates.
type campaignPopulationReaderV2 struct {
	io.ReadCloser
	metrics *campaignPopulationReadMetricsV2
	closed  atomic.Bool
}

// Count only bytes actually returned by the underlying disk or Http reader.
func (self *campaignPopulationReaderV2) Read(buffer []byte) (int, error) {
	count, err := self.ReadCloser.Read(buffer)
	self.metrics.bytes.Add(uint64(count))
	return count, err
}

// The real reader closes before the ownership debit is discharged.
func (self *campaignPopulationReaderV2) Close() error {
	if !self.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := self.ReadCloser.Close()
	self.metrics.active.Add(^uint64(0))
	return err
}

// Each replica is an isolated real local store. No envelope body is retained
// by this observer; staged wire lengths come from the actual file descriptor.
type campaignPopulationReplicaV2 struct {
	server.BlobStore
	metrics       *campaignPopulationReadMetricsV2
	contentPuts   atomic.Uint64
	historyPuts   atomic.Uint64
	contentGets   atomic.Uint64
	historyGets   atomic.Uint64
	wireBytes     atomic.Uint64
	peakWireBytes atomic.Uint64
}

// Expose only quota coordination; all actual Put/Get calls still traverse this
// observer, so a batch cannot erase any production readback or byte census.
func (self *campaignPopulationReplicaV2) LocalBlobBatchSource() server.BlobStore {
	return self.BlobStore
}

// Preserve immutable create/readback semantics and distinguish both routes.
func (self *campaignPopulationReplicaV2) PutIfAbsent(ctx context.Context, key, localPath, contentType string) (bool, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return false, errors.New("population publication has no bounded staged wire")
	}
	created, err := self.BlobStore.PutIfAbsent(ctx, key, localPath, contentType)
	if err != nil {
		return false, err
	}
	switch {
	case strings.Contains(key, "/evidence/content/"):
		self.contentPuts.Add(1)
	case strings.Contains(key, "/evidence/history/"):
		self.historyPuts.Add(1)
	default:
		return false, errors.New("population publication used an unexpected object route")
	}
	size := uint64(info.Size())
	self.wireBytes.Add(size)
	for previous := self.peakWireBytes.Load(); previous < size; previous = self.peakWireBytes.Load() {
		if self.peakWireBytes.CompareAndSwap(previous, size) {
			break
		}
	}
	return created, nil
}

// Count the exact content/history read while preserving the real store body.
func (self *campaignPopulationReplicaV2) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := self.BlobStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(key, "/evidence/content/"):
		self.contentGets.Add(1)
	case strings.Contains(key, "/evidence/history/"):
		self.historyGets.Add(1)
	default:
		return nil, errors.Join(errors.New("population read used an unexpected object route"), reader.Close())
	}
	return self.metrics.own(reader), nil
}

// A source retains only its independent path, size and expected digest.
type campaignPopulationSourceV2 struct {
	path  string
	bytes uint64
	hash  string
}

// The fixture creates reproducible non-Json tape bytes without allocating a
// second source-sized body while the actual producer still owns its carrier.
type campaignPopulationBytesV2 struct {
	prefix    string
	position  uint64
	remaining uint64
}

// Deterministic bytes are generated into the caller's fixed 32 KiB buffer.
func (self *campaignPopulationBytesV2) Read(buffer []byte) (int, error) {
	if self.remaining == 0 {
		return 0, io.EOF
	}
	count := int(min(uint64(len(buffer)), self.remaining))
	for index := range buffer[:count] {
		position := self.position + uint64(index)
		if position < uint64(len(self.prefix)) {
			buffer[index] = self.prefix[position]
		} else {
			buffer[index] = byte(position % 251)
		}
	}
	self.position += uint64(count)
	self.remaining -= uint64(count)
	return count, nil
}

// One fixed fixture buffer computes every expected hash before publication;
// these are representative raw tapes, not invented valid scoring proofs.
func campaignPopulationSourcesV2(t *testing.T, cfg *ResolvedConfig) []campaignPopulationSourceV2 {
	t.Helper()
	if cfg.Config.Topology.Miners != 1000 || cfg.Config.Topology.HeadSlots != 200 || cfg.Config.Topology.Validators != 2 || cfg.Config.Topology.Operators != 2 || cfg.Policy.Verify.TrailDepth != 8 || cfg.Policy.Verify.HardSeedPerMinutePerSource != 40 {
		t.Fatal("population ownership test requires the unchanged launch geometry")
	}
	trails := uint64(5*300+3*360) * uint64(cfg.Public.Chain.ExpectedBlockSeconds) * uint64(cfg.Policy.Verify.HardSeedPerMinutePerSource) / 60
	if trails != 20640 {
		t.Fatal("production-prefix nominal geometry changed")
	}
	// The independently retained launch wire guard gives 43025 bytes for
	// seven M8 checkpoints plus terminal, and 2567 bytes per proof record.
	tapes := []struct {
		name          string
		bytesPerTrail uint64
	}{{name: "records", bytesPerTrail: 43025}, {name: "proofs", bytesPerTrail: 2567}}
	buffer := make([]byte, 32*1024)
	sources := []campaignPopulationSourceV2{}
	for _, configured := range cfg.Config.ValidatorEvidenceV2 {
		for _, operator := range configured.Evidence.Operators {
			for _, tape := range tapes {
				chunkBytes := configured.Evidence.Bounds.Cut.Records.MaxChunkBytes
				if tape.name == "proofs" {
					chunkBytes = configured.Evidence.Bounds.Cut.Proofs.MaxChunkBytes
				}
				if chunkBytes != 4*1024*1024 {
					t.Fatal("phase-shaped corpus requires the actual 4 MiB chunk bound")
				}
				count := (trails*tape.bytesPerTrail + chunkBytes - 1) / chunkBytes
				for index := uint64(0); index < count; index++ {
					source := campaignPopulationSourceV2{path: fmt.Sprintf("population/validator-%02d/no-%02d/%s/part-%06d.bin", configured.ValidatorID, operator.NoID, tape.name, index), bytes: 320 * 1024}
					if tape.name == "records" && index == 0 {
						source.bytes = chunkBytes
					}
					hash := sha256.New()
					count, err := io.CopyBuffer(hash, &campaignPopulationBytesV2{prefix: source.path + "\x00", remaining: source.bytes}, buffer)
					if err != nil || uint64(count) != source.bytes {
						t.Fatalf("hash representative source: %d %v", count, err)
					}
					source.hash = fmt.Sprintf("sha256:%x", hash.Sum(nil))
					sources = append(sources, source)
				}
			}
		}
	}
	sort.Slice(sources, func(first, second int) bool { return sources[first].path < sources[second].path })
	if len(sources) != 900 {
		t.Fatalf("phase-shaped source census is %d, want 900", len(sources))
	}
	return sources
}

// Only the next exact source is materialized. Earlier originals remain on
// disk; no proof bytes are deleted or replaced to make the test smaller.
func writeCampaignPopulationSourceV2(root string, source campaignPopulationSourceV2, buffer []byte) error {
	path := filepath.Join(root, filepath.FromSlash(source.path))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	count, writeErr := io.CopyBuffer(struct{ io.Writer }{Writer: file}, &campaignPopulationBytesV2{prefix: source.path + "\x00", remaining: source.bytes}, buffer)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if uint64(count) != source.bytes {
		return errors.New("representative source was not fully materialized")
	}
	return nil
}

// Materialize all 900 production-prefix data-chunk slots at representative
// sizes, including one full 4 MiB chunk for each of the four source owners.
// Its 296 MiB raw corpus crosses the unchanged legacy 256 MiB byte boundary.
// The actual producer cannot preload the corpus: the next file does not exist
// until both replicas have completed both immutable content/history checks.
func TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	sources := campaignPopulationSourcesV2(t, cfg)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	evmRoles, err := buildEVMRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The archive needs its genuine deterministic owner key, not a second
	// full renderer or unused native-key derivation for this transport test.
	roles := &RoleSecrets{Schema: "urnetwork-sim-role-secrets-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, EVM: evmRoles}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	runId := "population-stream-v2"
	runDir := filepath.Join(stateDir, "runs", runId)
	buffer := make([]byte, 32*1024)
	hashes := make(map[string]string, len(sources))
	var sourceBytes uint64
	for _, source := range sources {
		hashes[source.path] = source.hash
		sourceBytes += source.bytes
	}
	if sourceBytes != 296*1024*1024 || sourceBytes <= maximumCampaignEvidenceAggregateBytes || sourceBytes > limits.maximumBytes {
		t.Fatalf("actual representative byte census %d does not cross the legacy bound within the explicit launch allowance", sourceBytes)
	}
	if err := writeCampaignPopulationSourceV2(runDir, sources[0], buffer); err != nil {
		t.Fatal(err)
	}
	storeMetrics := &campaignPopulationReadMetricsV2{}
	replicas := make([]*campaignPopulationReplicaV2, 2)
	for index := range replicas {
		prefix, err := operatorArtifactPrefix(cfg.Config, index+1)
		if err != nil {
			t.Fatal(err)
		}
		replicas[index] = &campaignPopulationReplicaV2{BlobStore: server.NewLocalBlobStore(filepath.Join(stateDir, "objects", fmt.Sprintf("operator-%d", index+1)), prefix), metrics: storeMetrics}
	}
	// These are the same real publisher/readback calls as the production
	// per-object accept boundary; no byte or signature check is replaced.
	operatorStores := make(map[int]server.BlobStore, len(replicas))
	for index, replica := range replicas {
		operatorStores[index+1] = replica
	}
	accepted := 0
	var activeOriginalBytes, peakOriginalBytes, acceptedBytes uint64
	publicationStart := time.Now()
	var manifest *ReleaseEvidenceEnvelope
	err = withCampaignEvidencePublicationBatches(t.Context(), operatorStores, func(publish func(*ReleaseEvidenceEnvelope) error) error {
		var err error
		manifest, err = streamCampaignEvidenceArchive(t.Context(), cfg, roles, stateDir, runId, "0x"+strings.Repeat("11", 32), "sha256:"+strings.Repeat("22", 32), hashes, func(envelope *ReleaseEvidenceEnvelope) error {
			if accepted >= len(sources) || activeOriginalBytes != 0 {
				return errors.New("producer overlapped source acceptance")
			}
			var payload struct {
				Path        string `json:"path"`
				Scope       string `json:"scope"`
				Size        uint64 `json:"size"`
				ContentHash string `json:"content_hash"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				return err
			}
			source := sources[accepted]
			if envelope.Kind != campaignEvidenceFileKind || payload.Path != source.path || payload.Scope != "run" || payload.Size != source.bytes || payload.ContentHash != source.hash {
				return errors.New("producer did not preserve the exact representative source")
			}
			if accepted+1 < len(sources) {
				if _, err := os.Stat(filepath.Join(runDir, filepath.FromSlash(sources[accepted+1].path))); !errors.Is(err, os.ErrNotExist) {
					return errors.New("next source existed before both replica readbacks")
				}
			}
			activeOriginalBytes = source.bytes
			peakOriginalBytes = max(peakOriginalBytes, activeOriginalBytes)
			readsBefore := storeMetrics.reads.Load()
			if err := publish(envelope); err != nil {
				return err
			}
			if storeMetrics.reads.Load()-readsBefore != 8 || storeMetrics.active.Load() != 0 {
				return errors.New("source acceptance omitted or retained a content/history replica read")
			}
			acceptedBytes += source.bytes
			activeOriginalBytes = 0
			accepted++
			if accepted < len(sources) {
				return writeCampaignPopulationSourceV2(runDir, sources[accepted], buffer)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if accepted != len(sources) || acceptedBytes != sourceBytes || activeOriginalBytes != 0 || peakOriginalBytes != 4*1024*1024 {
			return fmt.Errorf("producer ownership: count=%d bytes=%d active=%d peak=%d", accepted, acceptedBytes, activeOriginalBytes, peakOriginalBytes)
		}
		return publish(manifest)
	})
	if err != nil {
		t.Fatal(err)
	}
	publicationElapsed := time.Since(publicationStart)
	objects := uint64(len(sources) + 1)
	var storedWireBytes uint64
	maximumWireBytes, err := maximumCampaignFileEnvelopeBytes(peakOriginalBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, replica := range replicas {
		if replica.contentPuts.Load() != objects || replica.historyPuts.Load() != objects || replica.contentGets.Load() != 2*objects || replica.historyGets.Load() != 2*objects {
			t.Fatal("publisher did not complete the exact two-route replica census")
		}
		if replica.peakWireBytes.Load() <= peakOriginalBytes || replica.peakWireBytes.Load() > uint64(maximumWireBytes) {
			t.Fatal("actual staged carrier exceeds its source-derived bound")
		}
		storedWireBytes += replica.wireBytes.Load()
	}
	if storeMetrics.reads.Load() != 8*objects || storeMetrics.active.Load() != 0 || storeMetrics.peak.Load() != 1 || storeMetrics.bytes.Load() != 2*storedWireBytes {
		t.Fatal("actual publication readers escaped bounded synchronous ownership")
	}
	publicationReadBytes := storeMetrics.bytes.Load()

	profile, err := resolvedPublicEvidenceTransportProfile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, EvidenceTransportProfile: profile}
	originOperators := map[string]int{}
	for index, origin := range cfg.OperatorAPIOrigins {
		parsed, err := url.Parse(origin)
		if err != nil {
			t.Fatal(err)
		}
		originOperators[parsed.Host] = index
		public.Operators = append(public.Operators, PublicOperator{NoID: index + 1, APIURL: origin})
	}
	httpMetrics := &campaignPopulationReadMetricsV2{}
	httpRequests := atomic.Uint64{}
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		httpRequests.Add(1)
		index, found := originOperators[request.Host]
		if !found || request.Method != http.MethodGet || request.URL.Path != "/sn/evidence" {
			http.NotFound(response, request)
			return
		}
		key, err := startifact.EvidenceContentKey(replicas[index], request.URL.Query().Get("hash"))
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		reader, err := replicas[index].Get(request.Context(), key)
		if err != nil {
			http.NotFound(response, request)
			return
		}
		defer reader.Close()
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(response, reader)
	}))
	t.Cleanup(httpServer.Close)
	target, err := url.Parse(httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := httpServer.Client().Transport
	probe := &liveScenarioProbe{cfg: cfg, client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		routed := request.Clone(request.Context())
		routed.Host = request.URL.Host
		routed.URL.Scheme, routed.URL.Host = target.Scheme, target.Host
		response, err := transport.RoundTrip(routed)
		if err == nil {
			response.Body = httpMetrics.own(response.Body)
		}
		return response, err
	})}}
	readbackStart := time.Now()
	publicManifest, err := probe.fetchReplicatedCampaignEnvelope(t.Context(), public, manifest.ContentHash, campaignEvidenceManifestKind, runId, manifest.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCampaignEvidenceManifestWithLimits(publicManifest, limits)
	if err != nil || len(decoded.Files) != len(sources) || len(decoded.References) != 0 {
		t.Fatalf("actual public manifest census: %v", err)
	}
	if _, err := campaignEvidenceManifestFiles(decoded.Files); err == nil {
		t.Fatal("the actual transferred corpus no longer exceeds the unchanged legacy byte admission")
	}
	for index, source := range sources {
		entry := decoded.Files[index]
		if entry.Path != source.path || entry.ContentHash != source.hash || entry.Size != source.bytes {
			t.Fatal("public manifest lost an original source identity")
		}
	}
	readback, err := probe.streamPublicCampaignArchiveV2(t.Context(), public, manifest.Signer.Hex(), decoded)
	if err != nil {
		t.Fatal(err)
	}
	readbackElapsed := time.Since(readbackStart)
	// Close joins real server handlers before testing disk-reader discharge;
	// client response ownership is independently synchronous and exact.
	httpServer.Close()
	if readback.sourceCount != uint64(len(sources)) || readback.sourceBytes != sourceBytes || readback.peakOriginalBytes != peakOriginalBytes || len(readback.controls) != 0 || readback.retainedControlBytes != 0 || readback.graphMetadataBytes != 0 {
		t.Fatalf("readback retained tape bodies or changed its source census: %+v", readback)
	}
	if httpRequests.Load() != 2*objects || httpMetrics.reads.Load() != 2*objects || httpMetrics.active.Load() != 0 || httpMetrics.peak.Load() != 1 || storeMetrics.active.Load() != 0 {
		t.Fatal("actual public response/handler owner did not terminate with the full census")
	}
	if httpMetrics.bytes.Load()*2 != storedWireBytes || storeMetrics.bytes.Load()-publicationReadBytes != httpMetrics.bytes.Load() {
		t.Fatal("actual public wire-byte census differs from retained content replicas")
	}
	for _, replica := range replicas {
		if replica.contentGets.Load() != 3*objects || replica.historyGets.Load() != 2*objects {
			t.Fatal("independent public readback omitted a retained replica")
		}
	}
	t.Logf("actual full-population-shaped transfer: miners=1000 head_slots=200 source_owners=4 data_objects=%d raw_bytes=%d stored_content_history_wire_bytes=%d direct_read_bytes=%d public_read_bytes=%d", len(sources), sourceBytes, storedWireBytes, publicationReadBytes, httpMetrics.bytes.Load())
	t.Logf("logical ownership: producer_original_peak=%d readback_original_peak=%d staged_carrier_peak=%d retained_tape_controls=%d client_body_peak=%d store_reader_peak=%d final_active_readers=%d fixture_buffer_bytes=%d; publication_elapsed=%s readback_elapsed=%s; heap/maxRSS is a separate Terra observation", peakOriginalBytes, readback.peakOriginalBytes, replicas[0].peakWireBytes.Load(), readback.retainedControlBytes, httpMetrics.peak.Load(), storeMetrics.peak.Load(), storeMetrics.active.Load(), len(buffer), publicationElapsed, readbackElapsed)
}

// Every configured metadata slot is actually admitted and drained, but no
// payload is fabricated for these slots. A one-over URI is an atomic refusal.
func TestCampaignEvidencePopulationV2AdmitsFullConfiguredMetadataCensus(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	if cfg.Config.Topology.Miners != 1000 || cfg.Config.Topology.HeadSlots != 200 || cfg.Config.Topology.Validators != 2 || cfg.Config.Topology.Operators != 2 {
		t.Fatal("metadata census requires the unchanged four-source launch")
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if limits.maximumObjects != 1191936 {
		t.Fatalf("configured full object census is %d", limits.maximumObjects)
	}
	started := time.Now()
	files := make(map[string]string, int(limits.maximumObjects))
	hash := "sha256:" + strings.Repeat("33", 32)
	for index := uint64(0); index < limits.maximumObjects; index++ {
		files[fmt.Sprintf("source-%020d.bin", index)] = hash
	}
	queue, err := newCampaignArtifactQueueV2(files, limits)
	if err != nil {
		t.Fatal(err)
	}
	// Small admission batches avoid a redundant full-size temporary map.
	batch := make(map[string]bool, 64)
	for index := uint64(0); index < limits.maximumObjects; index++ {
		batch[fmt.Sprintf("source-%020d.bin", index)] = true
		if len(batch) == 64 || index+1 == limits.maximumObjects {
			if err := queue.admit(batch); err != nil {
				t.Fatal(err)
			}
			clear(batch)
		}
	}
	if queue.objects != limits.maximumObjects || uint64(len(queue.known)) != limits.maximumObjects || uint64(len(queue.names)) != limits.maximumObjects {
		t.Fatal("full configured metadata census was not materialized")
	}
	if err := queue.admit(map[string]bool{"one-over.bin": true}); err == nil || queue.objects != limits.maximumObjects || uint64(len(queue.known)) != limits.maximumObjects || uint64(len(queue.names)) != limits.maximumObjects {
		t.Fatal("one-over metadata admission was not an atomic refusal")
	}
	for index := uint64(0); index < limits.maximumObjects; index++ {
		name, found := queue.next()
		if !found || name != fmt.Sprintf("source-%020d.bin", index) {
			t.Fatalf("full-census lexicographic drain at %d: %q %t", index, name, found)
		}
	}
	if err := queue.admit(map[string]bool{fmt.Sprintf("source-%020d.bin", 0): true}); err != nil {
		t.Fatal(err)
	}
	if _, found := queue.next(); found || queue.objects != limits.maximumObjects || uint64(len(queue.known)) != limits.maximumObjects {
		t.Fatal("a repeated consumed source was requeued or recounted")
	}
	t.Logf("actual metadata-only admission/drain: configured_slots=%d retained_known_slots=%d peak_queued_slots=%d source_body_bytes=0 temporary_batch_slots=64 elapsed=%s; this is not 1191936 payload transfers or a 384 GiB memory allocation", limits.maximumObjects, len(queue.known), limits.maximumObjects, time.Since(started))
}
