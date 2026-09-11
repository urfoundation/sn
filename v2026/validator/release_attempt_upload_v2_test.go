//go:build linux || darwin

// Real API credential ownership, HTTP uploads and independent public reads
// surround the genuine cut sealer. Barriers force cancellation and concurrency.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Each physical server owns its objects and expected credential. Faults are
// chosen only before operations; snapshot copies retained bytes under the lock.
type releaseAttemptUploadV2TestStore struct {
	stateLock  sync.Mutex
	objects    map[string][]byte
	credential atomic.Value
	posts      atomic.Uint64
	gets       atomic.Uint64
	api        *sdk.Api
	getFault   string
}

// The production SDK session is updated independently of the transport owner.
func (self *releaseAttemptUploadV2TestStore) setJWT(value string) {
	self.credential.Store(value)
	self.api.SetByJwt(value)
}

// Object evidence is detached before the caller compares independent replicas.
func (self *releaseAttemptUploadV2TestStore) snapshot() map[string][]byte {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	objects := make(map[string][]byte, len(self.objects))
	for key, raw := range self.objects {
		objects[key] = bytes.Clone(raw)
	}
	return objects
}

// Genuine SDK sessions supply refreshed credentials to the exact production
// constructor. Server auth policy itself is tested in server/api/handlers.
func newReleaseAttemptUploadV2HTTPFixture(t *testing.T, count int, bounds AttemptCutV2Bounds) (*ReleaseConfig, []*releaseOperatorRuntime, []*releaseAttemptUploadV2TestStore) {
	t.Helper()
	operators := make([]OperatorConfig, count)
	runtimes := make([]*releaseOperatorRuntime, count)
	stores := make([]*releaseAttemptUploadV2TestStore, count)
	evidenceBounds := releaseEvidenceV2TestConfig(t.TempDir(), operators).Bounds
	evidenceBounds.Cut = bounds
	for index := range operators {
		store := &releaseAttemptUploadV2TestStore{objects: map[string][]byte{}}
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			kind, hash := r.URL.Query().Get("kind"), r.URL.Query().Get("hash")
			key := kind + "/" + hash
			if r.URL.Path != "/sn/attempt-artifact" || len(r.URL.Query()) != 2 {
				t.Error("upload fixture received another path or query")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			switch r.Method {
			case http.MethodPost:
				store.posts.Add(1)
				if r.Header.Get("Authorization") != "Bearer "+store.credential.Load().(string) {
					t.Error("upload lost the refreshed operator-owned credential")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil || int64(len(raw)) != r.ContentLength || attemptHex32(sha256.Sum256(raw)) != hash {
					t.Errorf("upload object lost exact bytes: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				func() { store.stateLock.Lock(); defer store.stateLock.Unlock(); store.objects[key] = bytes.Clone(raw) }()
				w.Header().Set("ETag", `"`+hash+`"`)
				w.WriteHeader(http.StatusNoContent)
			case http.MethodGet:
				store.gets.Add(1)
				if r.Header.Get("Authorization") != "" {
					t.Error("public proof read leaked upload credentials")
				}
				raw := func() []byte {
					store.stateLock.Lock()
					defer store.stateLock.Unlock()
					return bytes.Clone(store.objects[key])
				}()
				if raw == nil || store.getFault == "missing" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if store.getFault == "corrupt" {
					raw[0] ^= 1
				}
				contentType := "application/x-ndjson"
				if kind == "metadata" {
					contentType = "application/json"
				}
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
				_, _ = w.Write(raw)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		}))
		t.Cleanup(endpoint.Close)
		strategy := connect.NewClientStrategyWithDefaults(t.Context())
		api := sdk.NewApi(t.Context(), strategy, endpoint.URL)
		store.api = api
		store.setJWT(fmt.Sprintf("operator-%d-private-fixture", index+1))
		t.Cleanup(func() {
			if err := api.CloseAndWait(context.Background()); err != nil {
				t.Error(err)
			}
			strategy.Close()
		})
		operator := OperatorConfig{NoID: uint64(index + 1), APIURL: endpoint.URL}
		upload, err := newReleaseAttemptUploadV2(t.Context(), operator, evidenceBounds, api.GetByJwt)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(upload.close)
		operators[index] = operator
		runtimes[index] = &releaseOperatorRuntime{measurement: &ReleaseMeasurementContext{NoID: operator.NoID}, attemptUpload: upload}
		stores[index] = store
	}
	config := &ReleaseConfig{Operators: operators, EvidenceV2: releaseEvidenceV2TestConfig(t.TempDir(), operators)}
	config.EvidenceV2.Bounds.Cut = bounds
	return config, runtimes, stores
}

func TestReleaseEvidenceV2UploadUsesLiveAPISession(t *testing.T) {
	t.Parallel()
	_, runtimes, stores := newReleaseAttemptUploadV2HTTPFixture(t, 1, attemptCutV2ReplicaTestBounds())
	data := []byte("{\"session\":true}\n")
	hash := attemptHex32(sha256.Sum256(data))
	for _, credential := range []string{"first-private-fixture", "refreshed-private-fixture"} {
		stores[0].setJWT(credential)
		if err := runtimes[0].attemptUpload.write(t.Context(), "metadata", hash, data); err != nil {
			t.Fatal(err)
		}
	}
	stores[0].api.SetByJwt("")
	if err := runtimes[0].attemptUpload.write(t.Context(), "metadata", hash, data); err == nil {
		t.Fatal("logged-out API session authorized upload")
	}
	if stores[0].posts.Load() != 2 {
		t.Fatal("revoked API session made an HTTP request")
	}
}

func TestReleaseEvidenceV2UploadOwnerCancellationJoinsInFlight(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"owner", "parent", "caller"} {
		entered, joined := make(chan struct{}), make(chan struct{})
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			close(entered)
			<-r.Context().Done()
			close(joined)
		}))
		parent, cancelParent := context.WithCancel(t.Context())
		caller, cancelCaller := context.WithCancel(t.Context())
		upload, err := newReleaseAttemptUploadV2(parent, OperatorConfig{NoID: 1, APIURL: endpoint.URL}, ReleaseEvidenceV2Bounds{Cut: attemptCutV2ReplicaTestBounds(), MaxTransitionBytes: 1024 * 1024}, func() string { return "fixture" })
		if err != nil {
			cancelParent()
			cancelCaller()
			endpoint.Close()
			t.Fatal(err)
		}
		data := []byte("original")
		result := make(chan error, 1)
		go func() { result <- upload.write(caller, "metadata", attemptHex32(sha256.Sum256(data)), data) }()
		<-entered
		switch fault {
		case "owner":
			upload.close()
		case "parent":
			cancelParent()
		case "caller":
			cancelCaller()
		}
		writeErr := <-result
		<-joined
		upload.close()
		cancelParent()
		cancelCaller()
		endpoint.Close()
		if !errors.Is(writeErr, context.Canceled) {
			t.Fatalf("%s cancellation lost actual HTTP ownership: %v", fault, writeErr)
		}
		if err := upload.write(t.Context(), "metadata", attemptHex32(sha256.Sum256(data)), data); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s closed owner accepted another upload: %v", fault, err)
		}
	}
}

func TestReleaseEvidenceV2UploadCancellationInsideCredentialGetter(t *testing.T) {
	t.Parallel()
	var upload *releaseAttemptUploadV2
	var calls int
	var err error
	upload, err = newReleaseAttemptUploadV2(t.Context(), OperatorConfig{NoID: 1, APIURL: "http://127.0.0.1:1"}, ReleaseEvidenceV2Bounds{Cut: attemptCutV2ReplicaTestBounds(), MaxTransitionBytes: 1024 * 1024}, func() string { upload.close(); return "fixture" })
	if err != nil {
		t.Fatal(err)
	}
	upload.writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected transport") })
	data := []byte("data")
	err = upload.write(t.Context(), "metadata", attemptHex32(sha256.Sum256(data)), data)
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("synchronous session revocation reached transport: %v", err)
	}
}

func TestReleaseEvidenceV2UploadRejectsIncompleteAdmission(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"context", "cancelled", "operator", "getter", "origin", "header", "stream", "transition-zero", "transition-overflow"} {
		ctx, cancel := context.WithCancel(t.Context())
		operator := OperatorConfig{NoID: 1, APIURL: "http://127.0.0.1:1"}
		bounds := attemptCutV2ReplicaTestBounds()
		transitionBytes := uint64(1024 * 1024)
		getter := func() string { t.Error("constructor fetched credentials"); return "fixture" }
		switch fault {
		case "context":
			ctx = nil
		case "cancelled":
			cancel()
		case "operator":
			operator.NoID = 0
		case "getter":
			getter = nil
		case "origin":
			operator.APIURL = "http://example.invalid"
		case "header":
			bounds.MaxHeaderBytes = max(bounds.Records.MaxPageBytes, bounds.Records.MaxManifestBytes, bounds.Proofs.MaxPageBytes, bounds.Proofs.MaxManifestBytes) + 1
		case "stream":
			bounds.Records.MaxChunkBytes = 0
		case "transition-zero":
			transitionBytes = 0
		case "transition-overflow":
			transitionBytes = ^uint64(0)
		}
		owner, err := newReleaseAttemptUploadV2(ctx, operator, ReleaseEvidenceV2Bounds{Cut: bounds, MaxTransitionBytes: transitionBytes}, getter)
		cancel()
		if owner != nil {
			owner.close()
		}
		if err == nil || owner != nil {
			t.Fatalf("%s incomplete owner was admitted", fault)
		}
	}
}

func TestReleaseEvidenceV2UploadReplicasRequireCompleteRuntimeCensus(t *testing.T) {
	t.Parallel()
	cfg, runtimes, stores := newReleaseAttemptUploadV2HTTPFixture(t, 3, attemptCutV2ReplicaTestBounds())
	origins := [2]string{cfg.Operators[2].APIURL, cfg.Operators[0].APIURL}
	if _, err := releaseAttemptUploadReplicasV2(cfg, origins, runtimes); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"missing-runtime", "nil-runtime", "duplicate-runtime", "missing-upload", "measurement", "role", "origin", "bounds", "transition", "closed", "writer", "duplicate-config", "capacity", "unselected-origin", "same-origin"} {
		config := *cfg
		config.Operators = slices.Clone(cfg.Operators)
		owners := slices.Clone(runtimes)
		runtime := *owners[0]
		measurement := *runtime.measurement
		owner := *runtime.attemptUpload
		runtime.measurement, runtime.attemptUpload, owners[0] = &measurement, &owner, &runtime
		selected := origins
		switch fault {
		case "missing-runtime":
			owners = owners[:2]
		case "nil-runtime":
			owners[1] = nil
		case "duplicate-runtime":
			owners[1] = owners[0]
		case "missing-upload":
			runtime.attemptUpload = nil
		case "measurement":
			measurement.NoID++
		case "role":
			owner.noID = 91
		case "origin":
			owner.origin = "https://other.invalid"
		case "bounds":
			owner.bounds.MaxHeaderBytes++
		case "transition":
			owner.maxTransitionBytes++
		case "closed":
			var cancel context.CancelFunc
			owner.ctx, cancel = context.WithCancel(t.Context())
			cancel()
		case "writer":
			owner.writer = nil
		case "duplicate-config":
			config.Operators[1].NoID = config.Operators[0].NoID
		case "capacity":
			config.EvidenceV2.Bounds.MaxParticipants = 2
		case "unselected-origin":
			selected[0] = "https://other.invalid"
		case "same-origin":
			selected[1] = selected[0]
		}
		actual, err := releaseAttemptUploadReplicasV2(&config, selected, owners)
		if err == nil || !reflect.DeepEqual(actual, [2]AttemptCutV2Replica{}) {
			t.Fatalf("%s admitted a partial or redirected replica census", fault)
		}
	}
	for _, store := range stores {
		if store.posts.Load() != 0 || store.gets.Load() != 0 {
			t.Fatal("replica census admission performed HTTP")
		}
	}
	if _, err := releaseAttemptUploadReplicasV2(cfg, origins, runtimes); err != nil {
		t.Fatalf("fault census mutated original ownership: %v", err)
	}
}

// Every source writer is genuinely constructed for its own live SDK session.
// A nonnil replacement must be refused before either physical server sees I/O.
func TestReleaseEvidenceV2UploadReplicasRejectConcreteWriterMisbinding(t *testing.T) {
	t.Parallel()
	cfg, runtimes, stores := newReleaseAttemptUploadV2HTTPFixture(t, 2, attemptCutV2ReplicaTestBounds())
	origins := [2]string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}
	if _, err := releaseAttemptUploadReplicasV2(cfg, origins, runtimes); err != nil {
		t.Fatalf("actual session census prerequisite: %v", err)
	}
	for _, fault := range []string{"other-operator", "path", "query", "metadata", "records", "proofs", "getter", "client", "timeout", "redirect", "transport", "cookie-jar", "cancel"} {
		owners := slices.Clone(runtimes)
		runtime := *owners[0]
		owner := *runtime.attemptUpload
		writer := *owner.writer
		client := *writer.client
		writer.client, owner.writer, runtime.attemptUpload, owners[0] = &client, &writer, &owner, &runtime
		switch fault {
		case "other-operator":
			owner.writer = runtimes[1].attemptUpload.writer
		case "path":
			writer.endpoint.Path = "/another-upload"
		case "query":
			writer.endpoint.RawQuery = "another=owner"
		case "metadata":
			writer.metadataBytes++
		case "records":
			writer.recordBytes++
		case "proofs":
			writer.proofBytes++
		case "getter":
			writer.byJwt = nil
		case "client":
			writer.client = nil
		case "timeout":
			client.Timeout = 0
		case "redirect":
			client.CheckRedirect = nil
		case "transport":
			client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
				t.Error("admission invoked substituted transport")
				return nil, errors.New("unexpected transport")
			})
		case "cookie-jar":
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client.Jar = jar
		case "cancel":
			owner.cancel = nil
		}
		actual, err := releaseAttemptUploadReplicasV2(cfg, origins, owners)
		if err == nil || !reflect.DeepEqual(actual, [2]AttemptCutV2Replica{}) {
			t.Fatalf("concrete writer misbinding reached replica admission: %s", fault)
		}
		for _, store := range stores {
			if store.posts.Load() != 0 || store.gets.Load() != 0 {
				t.Fatalf("%s misbinding reached a real HTTP origin", fault)
			}
		}
	}
	if _, err := releaseAttemptUploadReplicasV2(cfg, origins, runtimes); err != nil {
		t.Fatalf("writer mutation changed original ownership: %v", err)
	}
}

func TestReleaseEvidenceV2UploadReplicasOwnConfiguredRouting(t *testing.T) {
	t.Parallel()
	cfg, runtimes, stores := newReleaseAttemptUploadV2HTTPFixture(t, 2, attemptCutV2ReplicaTestBounds())
	replicas, err := releaseAttemptUploadReplicasV2(cfg, [2]string{cfg.Operators[1].APIURL, cfg.Operators[0].APIURL}, runtimes)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Operators[0].APIURL = "https://other.invalid"
	cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes = 0
	runtimes[0].attemptUpload = nil
	clear(runtimes)
	data := []byte("{\"owned\":true}\n")
	hash := attemptHex32(sha256.Sum256(data))
	for index, replica := range replicas {
		stores[1-index].setJWT("refreshed-after-binding")
		if err := replica.WriteMetadata(t.Context(), hash, data); err != nil {
			t.Fatal(err)
		}
	}
	for _, store := range stores {
		if store.posts.Load() != 1 || !bytes.Equal(store.snapshot()["metadata/"+hash], data) {
			t.Fatal("replica callback redirected or lost original routing")
		}
	}
}

func TestReleaseEvidenceV2UploadReplicasSealRealAttemptEvidence(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 1)
	bounds := attemptCutV2ReplicaTestBounds()
	cfg, runtimes, stores := newReleaseAttemptUploadV2HTTPFixture(t, 2, bounds)
	replicas, err := releaseAttemptUploadReplicasV2(cfg, [2]string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}, runtimes)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, bounds, AttemptCutV2ReplicaOptions{ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas})
	if err != nil || publication == nil {
		t.Fatalf("real authenticated HTTP replica seal: %v", err)
	}
	if publication.Cut.RecordCount != 10 || publication.Cut.CompleteCount != 1 || publication.Cut.FailedCount != 1 {
		t.Fatal("real attempt census changed during API publication")
	}
	for _, store := range stores {
		raw := store.snapshot()["metadata/"+publication.ContentHash]
		cut, err := DecodeAttemptCutV2(raw, fixture.expected, bounds)
		if err != nil || !reflect.DeepEqual(cut, publication.Cut) || store.posts.Load() < 5 || store.gets.Load() < store.posts.Load() {
			t.Fatalf("replica omitted genuine signed proof bytes or public readback: %v", err)
		}
	}
	if !reflect.DeepEqual(stores[0].snapshot(), stores[1].snapshot()) {
		t.Fatal("authenticated replicas retained different evidence")
	}
}

func TestReleaseEvidenceV2UploadReplicasRequireIndependentPublicReadback(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"missing", "corrupt"} {
		cfg, runtimes, stores := newReleaseAttemptUploadV2HTTPFixture(t, 2, attemptCutV2ReplicaTestBounds())
		stores[1].getFault = fault
		replicas, err := releaseAttemptUploadReplicasV2(cfg, [2]string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}, runtimes)
		if err != nil {
			t.Fatal(err)
		}
		publisher, err := newAttemptCutV2Replicas(cfg.EvidenceV2.Bounds.Cut, replicas)
		if err != nil {
			t.Fatal(err)
		}
		data := []byte("{\"public\":true}\n")
		if err := publisher.writer("metadata")(t.Context(), attemptHex32(sha256.Sum256(data)), data); err == nil {
			t.Fatalf("%s public proof bytes were replaced by POST acknowledgement", fault)
		}
		if stores[1].posts.Load() != 1 || stores[1].gets.Load() != 1 {
			t.Fatal("failed replica did not exercise actual successful upload and independent failed GET")
		}
	}
}

func TestReleaseEvidenceV2UploadReplicasOverlapAndJoinFailure(t *testing.T) {
	t.Parallel()
	entered := make(chan int, 2)
	fail := make(chan struct{})
	joined := make(chan struct{})
	bounds := attemptCutV2ReplicaTestBounds()
	operators := make([]OperatorConfig, 2)
	runtimes := make([]*releaseOperatorRuntime, 2)
	evidenceBounds := releaseEvidenceV2TestConfig(t.TempDir(), operators).Bounds
	evidenceBounds.Cut = bounds
	for index := range operators {
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			entered <- index
			if index == 0 {
				<-fail
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			<-r.Context().Done()
			close(joined)
		}))
		t.Cleanup(endpoint.Close)
		operators[index] = OperatorConfig{NoID: uint64(index + 1), APIURL: endpoint.URL}
		owner, err := newReleaseAttemptUploadV2(t.Context(), operators[index], evidenceBounds, func() string { return "fixture" })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(owner.close)
		runtimes[index] = &releaseOperatorRuntime{measurement: &ReleaseMeasurementContext{NoID: uint64(index + 1)}, attemptUpload: owner}
	}
	cfg := &ReleaseConfig{Operators: operators, EvidenceV2: releaseEvidenceV2TestConfig(t.TempDir(), operators)}
	cfg.EvidenceV2.Bounds.Cut = bounds
	replicas, err := releaseAttemptUploadReplicasV2(cfg, [2]string{operators[0].APIURL, operators[1].APIURL}, runtimes)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := newAttemptCutV2Replicas(bounds, replicas)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("{\"parallel\":true}\n")
	result := make(chan error, 1)
	go func() { result <- publisher.writer("metadata")(t.Context(), attemptHex32(sha256.Sum256(data)), data) }()
	first, second := <-entered, <-entered
	close(fail)
	writeErr := <-result
	<-joined
	if first == second || writeErr == nil {
		t.Fatal("replicas failed to overlap or acknowledged a failed member")
	}
}
