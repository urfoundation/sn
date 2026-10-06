// Real disk stores force publication/readback ordering without sleeps or
// allocation measurements. A callback observes completion of each real read.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/server/v2026"
)

// The production store methods remain real; only their exact event boundary
// is recorded to prove no next source is read/prepared before both replicas.
type streamingCampaignEvidenceStore struct {
	server.BlobStore
	operator  int
	paths     map[string]string
	events    *[]string
	afterRead func(int, string, bool)
}

// Preserve the actual immutable upload and record its original signed path.
func (self *streamingCampaignEvidenceStore) PutIfAbsent(ctx context.Context, key, localPath, contentType string) (bool, error) {
	raw, err := os.ReadFile(localPath)
	if err != nil {
		return false, err
	}
	var envelope ReleaseEvidenceEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return false, err
	}
	name := envelope.Kind
	if envelope.Kind == campaignEvidenceFileKind {
		var payload campaignEvidenceFilePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return false, err
		}
		name = payload.Path
	}
	self.paths[key] = name
	*self.events = append(*self.events, fmt.Sprintf("write/%d/%s", self.operator, name))
	return self.BlobStore.PutIfAbsent(ctx, key, localPath, contentType)
}

// The actual reader bytes are never replaced by the test observation.
func (self *streamingCampaignEvidenceStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := self.BlobStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &streamingCampaignEvidenceRead{ReadCloser: reader, closed: func() {
		name := self.paths[key]
		*self.events = append(*self.events, fmt.Sprintf("read/%d/%s", self.operator, name))
		if self.afterRead != nil {
			self.afterRead(self.operator, name, strings.Contains(key, "/history/"))
		}
	}}, nil
}

// One close event corresponds to ownership release of one real disk reader.
type streamingCampaignEvidenceRead struct {
	io.ReadCloser
	closed func()
}

func (self *streamingCampaignEvidenceRead) Close() error {
	err := self.ReadCloser.Close()
	if self.closed != nil {
		self.closed()
		self.closed = nil
	}
	return err
}

// The normal renderer receipt, owner signatures and both isolated blob stores
// are retained; the only small input choice is two non-Json raw source files.
type streamingCampaignEvidenceFixture struct {
	cfg      *ResolvedConfig
	roles    *RoleSecrets
	stateDir string
	runId    string
	hashes   map[string]string
	stores   map[int]*streamingCampaignEvidenceStore
	events   []string
}

func newStreamingCampaignEvidenceFixture(t *testing.T) *streamingCampaignEvidenceFixture {
	t.Helper()
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &streamingCampaignEvidenceFixture{cfg: cfg, roles: roles, stateDir: stateDir, runId: "streaming-archive", hashes: map[string]string{}, stores: map[int]*streamingCampaignEvidenceStore{}}
	for _, name := range []string{"a.bin", "z.bin"} {
		raw := []byte(name + "\n")
		if err := atomicWrite(filepath.Join(stateDir, "runs", fixture.runId, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.hashes[name] = bytesSHA256(raw)
	}
	for operator := 1; operator <= 2; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		fixture.stores[operator] = &streamingCampaignEvidenceStore{BlobStore: server.NewLocalBlobStore(filepath.Join(stateDir, "object-store"), prefix), operator: operator, paths: map[string]string{}, events: &fixture.events}
	}
	return fixture
}

// Drive the actual live archive publisher with the ordinary factory boundary.
func (self *streamingCampaignEvidenceFixture) publish(ctx context.Context) (*ReleaseEvidenceEnvelope, error) {
	return publishCampaignEvidenceArchive(ctx, self.cfg, self.roles, self.stateDir, self.runId, "0x"+strings.Repeat("11", 32), "sha256:"+strings.Repeat("22", 32), self.hashes, func(operator int) (server.BlobStore, error) { return self.stores[operator], nil })
}

// Before this fix, operator one received every file before operator two had
// read back the first; this exact real-store event assertion would fail.
func TestCampaignEvidenceStreamingReadsBothReplicasBeforeNextObject(t *testing.T) {
	fixture := newStreamingCampaignEvidenceFixture(t)
	manifest, err := fixture.publish(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCampaignEvidenceManifest(manifest)
	if err != nil || len(decoded.Files) != 2 {
		t.Fatalf("closed file census: %+v %v", decoded, err)
	}
	readCounts := map[string]int{}
	for _, event := range fixture.events {
		if strings.HasPrefix(event, "write/") && strings.HasSuffix(event, "/z.bin") {
			if readCounts["read/1/a.bin"] < 4 || readCounts["read/2/a.bin"] < 4 {
				t.Fatalf("next object preceded both exact content/history readbacks: %v", fixture.events)
			}
		}
		readCounts[event]++
	}
	for operator := 1; operator <= 2; operator++ {
		if readCounts[fmt.Sprintf("read/%d/%s", operator, campaignEvidenceManifestKind)] != 4 {
			t.Fatalf("manifest lacked immutable-upload and final content/history readbacks: %v", fixture.events)
		}
	}
}

// An actual changed next file is observed after the first owned carrier has
// been discharged. No preloaded snapshot can hide this mutation.
func TestCampaignEvidenceStreamingRejectsChangedNextSource(t *testing.T) {
	fixture := newStreamingCampaignEvidenceFixture(t)
	changed := false
	historyReads := 0
	fixture.stores[2].afterRead = func(operator int, name string, history bool) {
		if changed || name != "a.bin" || !history {
			return
		}
		historyReads++
		if historyReads != 2 {
			return
		}
		changed = true
		if err := os.WriteFile(filepath.Join(fixture.stateDir, "runs", fixture.runId, "z.bin"), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := fixture.publish(t.Context())
	if !changed || err == nil || manifest != nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("changed next source: manifest=%v changed=%v err=%v", manifest, changed, err)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runs", fixture.runId, campaignEvidenceManifestFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete graph acquired final manifest: %v", err)
	}
}

// Cancellation is forced at the replica readback boundary. No worker exists
// to linger or continue preparing later files after the owner returns.
func TestCampaignEvidenceStreamingCancellationStopsAtOwnedObject(t *testing.T) {
	fixture := newStreamingCampaignEvidenceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	historyReads := 0
	fixture.stores[2].afterRead = func(operator int, name string, history bool) {
		if name == "a.bin" && history {
			historyReads++
			if historyReads == 2 {
				cancel()
			}
		}
	}
	manifest, err := fixture.publish(ctx)
	if manifest != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled archive returned completion: %v %v", manifest, err)
	}
	for _, event := range fixture.events {
		if strings.HasSuffix(event, "/z.bin") || strings.HasSuffix(event, "/"+campaignEvidenceManifestKind) {
			t.Fatalf("cancelled owner continued publication: %v", fixture.events)
		}
	}
	fixture.stores[2].afterRead = nil
	if _, err := fixture.publish(t.Context()); err != nil {
		t.Fatalf("immutable partial-object retry failed: %v", err)
	}
}

// Late reference validation cannot turn a partial immutable upload into a
// signed complete graph, even when every preceding raw object was genuine.
func TestCampaignEvidenceStreamingReferenceMismatchCannotPublishFinalManifest(t *testing.T) {
	fixture := newStreamingCampaignEvidenceFixture(t)
	raw, err := json.Marshal(FinalArtifactLocator{Kind: "self-reference", URI: "cycle.json", ContentHash: "sha256:" + strings.Repeat("33", 32), SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "runs", fixture.runId, "cycle.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.hashes["cycle.json"] = bytesSHA256(raw)
	manifest, err := fixture.publish(t.Context())
	if manifest != nil || err == nil {
		t.Fatalf("invalid reference census completed: %v %v", manifest, err)
	}
	for _, event := range fixture.events {
		if strings.HasSuffix(event, "/"+campaignEvidenceManifestKind) {
			t.Fatalf("invalid reference graph published its manifest: %v", fixture.events)
		}
	}
}
