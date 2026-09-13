// The real replica boundary owns one detached signed carrier. Store callbacks
// force source mutation after admission so repeated preparation fails exactly.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/server"
)

// Observe real staged bytes and original descriptor ownership. The first-put
// hook runs synchronously after preparation, without clocks or goroutine luck.
type campaignWireOwnerStoreTest struct {
	server.BlobStore
	want          []byte
	afterFirstPut func()
	puts          int
	gets          int
	closes        int
	stagedPaths   []string
}

// Every route must receive the same canonical wire admitted before the hook.
func (self *campaignWireOwnerStoreTest) PutIfAbsent(ctx context.Context, key, localPath, contentType string) (bool, error) {
	exact, err := os.ReadFile(localPath)
	if err != nil || !bytes.Equal(exact, self.want) {
		return false, errors.Join(errors.New("replica staged wire changed"), err)
	}
	self.stagedPaths = append(self.stagedPaths, localPath)
	created, err := self.BlobStore.PutIfAbsent(ctx, key, localPath, contentType)
	if err != nil {
		return false, err
	}
	self.puts++
	if self.puts == 1 && self.afterFirstPut != nil {
		self.afterFirstPut()
	}
	return created, nil
}

// Count the original read, not a stand-in body or precomputed digest.
func (self *campaignWireOwnerStoreTest) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := self.BlobStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	self.gets++
	return &campaignWireOwnerReaderTest{ReadCloser: reader, store: self}, nil
}

// The original reader remains owned until its actual Close returns.
type campaignWireOwnerReaderTest struct {
	io.ReadCloser
	store *campaignWireOwnerStoreTest
}

// All four per-replica reads must be discharged before source acceptance.
func (self *campaignWireOwnerReaderTest) Close() error {
	self.store.closes++
	return self.ReadCloser.Close()
}

// This actual production helper cannot prepare the original a second time:
// the first immutable put destroys both its backing payload and all identity
// fields. All remaining replica operations must use the one admitted owner.
func TestCampaignEvidencePreparedWireOwnsBothReplicas(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := buildEVMRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := signEvidence(cfg, "unit", "wire-owner-v1", map[string]string{"value": strings.Repeat("synthetic", 4096)}, roles["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mutations := 0
	stores := map[int]server.BlobStore{}
	for index, prefix := range []string{"operator-1", "operator-2"} {
		store := &campaignWireOwnerStoreTest{BlobStore: server.NewLocalBlobStore(t.TempDir(), prefix), want: want}
		if index == 0 {
			store.afterFirstPut = func() {
				mutations++
				for index := range envelope.Payload {
					envelope.Payload[index] = 'x'
				}
				*envelope = ReleaseEvidenceEnvelope{Payload: envelope.Payload}
			}
		}
		stores[index+1] = store
	}
	if err := publishCampaignEvidenceReplicas(t.Context(), stores, envelope); err != nil {
		t.Fatal(err)
	}
	if mutations != 1 {
		t.Fatal("first native publication did not force the preparation boundary")
	}
	stagedPath := ""
	for _, store := range stores {
		observed := store.(*campaignWireOwnerStoreTest)
		if observed.puts != 2 || observed.gets != 4 || observed.closes != 4 {
			t.Fatal("actual replica publication omitted a route or one of eight readbacks")
		}
		for _, path := range observed.stagedPaths {
			if stagedPath == "" {
				stagedPath = path
			}
			if path != stagedPath {
				t.Fatal("one authenticated object repeated its staging across routes or replicas")
			}
		}
	}
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("synchronous replica publication retained its staging file", err)
	}
	if err := publishCampaignEvidenceReplicas(t.Context(), stores, envelope); err == nil {
		t.Fatal("a later object reused the earlier object's authentication")
	}
}

// Marshal counts prove the writer does not encode its source payload again
// during signing. Each independent invocation must still read its own input.
type campaignWirePayloadTest struct {
	marshals int
}

// A deterministic source-side marshal observer retains normal Json semantics.
func (self *campaignWirePayloadTest) MarshalJSON() ([]byte, error) {
	self.marshals++
	return []byte(`{"value":"synthetic"}`), nil
}

// The pre-fix first write marshaled this same source twice. The retry must
// still compare its freshly marshaled input with the existing signed bytes.
func TestCampaignEvidenceLocalWriterMarshalsPayloadOnce(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := buildEVMRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	localPath := filepath.Join(stateRoot, "wire-owner.evidence.json")
	payload := &campaignWirePayloadTest{}
	first, firstBytes, err := prepareLocalEvidence(cfg, stateRoot, localPath, "unit", "wire-owner-v1", payload, roles["testnet-owner"], 0)
	if err != nil || payload.marshals != 1 {
		t.Fatalf("first writer encoded source %d times: %v", payload.marshals, err)
	}
	second, secondBytes, err := prepareLocalEvidence(cfg, stateRoot, localPath, "unit", "wire-owner-v1", payload, roles["testnet-owner"], 0)
	if err != nil || payload.marshals != 2 || first.ContentHash != second.ContentHash || !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("idempotent writer lost fresh input comparison: %d %v", payload.marshals, err)
	}
}

// Legacy mutable-input verification and the final-semantic adapter retain the
// same canonical wire and empty-run history sentinel as the original server.
func TestCampaignEvidencePreparedWirePreservesFinalAndLegacyChecks(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := buildEVMRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := signEvidence(cfg, "deployment-manifest", "", map[string]string{"value": "<>&\u2028"}, roles["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	store := &campaignWireOwnerStoreTest{BlobStore: server.NewLocalBlobStore(t.TempDir(), "operator-1"), want: want}
	if err := publishAndReadBackFinalSemanticEnvelope(t.Context(), store, envelope); err != nil {
		t.Fatal(err)
	}
	if err := readBackFinalSemanticEnvelope(t.Context(), store, envelope); err != nil || store.puts != 2 || store.gets != 6 || store.closes != 6 {
		t.Fatalf("final-semantic exact empty-run readback changed: %v", err)
	}
	legacy, err := startifactEvidenceEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Payload = json.RawMessage(`{"forged":true}`)
	before := store.gets
	if err := verifyDirectEvidencePublication(t.Context(), store, legacy, nil); err == nil || store.gets != before {
		t.Fatal("legacy verifier reused authentication for a mutated envelope")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := publishCampaignEvidenceReplicas(cancelled, map[int]server.BlobStore{1: store}, envelope); !errors.Is(err, context.Canceled) || store.puts != 2 {
		t.Fatal("cancelled campaign publication reached a replica")
	}
}
