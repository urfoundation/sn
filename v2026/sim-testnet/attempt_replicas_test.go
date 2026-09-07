//go:build linux || darwin

package main

// These adapter tests use real local server/blob storage and the production
// typed object codec. They do not stand in for public HTTP or live MinIO tests.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/startifact"
)

// Owns two independent namespaces without reading host configuration or keys.
func newAttemptReplicaStoreTestConfig(t *testing.T) (*ResolvedConfig, map[int]server.BlobStore, validator.AttemptCutV2Bounds) {
	t.Helper()
	cfg := &ResolvedConfig{
		Config: &HarnessConfig{
			Deployment: DeploymentConfig{DeploymentID: "typed-replicas", Network: "bittensor-testnet"},
			Topology:   TopologyConfig{Operators: 2},
			Artifacts:  ArtifactConfig{MinioPrefix: "sim-testnet/${deployment_id}"},
		},
		Public: &PublicManifest{}, ChainID: testnetChainID,
		OperatorAPIOrigins: []string{"http://127.0.0.1:18081", "http://127.0.0.1:18082"},
	}
	cfg.Public.Chain.ChainID, cfg.Public.Chain.GenesisHash = testnetChainID, testnetGenesis
	stores := map[int]server.BlobStore{}
	for operator := 1; operator <= 2; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		stores[operator] = server.NewLocalBlobStore(t.TempDir(), prefix)
	}
	stream := validator.AttemptStreamV2Bounds{
		MaxDataBytes: 1024, MaxItems: 16, MaxChunkBytes: 64, MaxChunks: 8,
		MaxPages: 4, MaxPageBytes: 32, MaxDescriptorsPerPage: 2, MaxManifestBytes: 32,
	}
	bounds := validator.AttemptCutV2Bounds{MaxHeaderBytes: 32, Records: stream, Proofs: stream}
	bounds.Proofs.MaxChunkBytes = 48
	return cfg, stores, bounds
}

// A single typed callback cannot write into another operator or object family.
func TestSimulatorAttemptCutV2StoreReplicasUseRealTypedStorage(t *testing.T) {
	t.Parallel()
	cfg, stores, bounds := newAttemptReplicaStoreTestConfig(t)
	replicas, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds)
	if err != nil {
		t.Fatal(err)
	}
	readBounds := startifact.AttemptObjectBounds{MetadataBytes: 32, RecordBytes: 64, ProofBytes: 48}
	for index, replica := range replicas {
		if replica.Origin != cfg.OperatorAPIOrigins[index] {
			t.Fatalf("operator %d origin changed: %q", index+1, replica.Origin)
		}
		for _, item := range []struct {
			kind  string
			write validator.AttemptStreamV2ObjectWriter
		}{
			{kind: "records", write: replica.WriteRecords},
			{kind: "proofs", write: replica.WriteProofs},
			{kind: "metadata", write: replica.WriteMetadata},
		} {
			raw := []byte(fmt.Sprintf("operator-%d-%s", index+1, item.kind))
			hash := fmt.Sprintf("0x%x", sha256.Sum256(raw))
			if err := item.write(t.Context(), hash, raw); err != nil {
				t.Fatalf("operator %d %s: %v", index+1, item.kind, err)
			}
			var decoded bytes.Buffer
			count, err := startifact.ReadAttemptObjectTo(t.Context(), stores[index+1], readBounds, item.kind, hash, &decoded)
			if err != nil || count != uint64(len(raw)) || !bytes.Equal(raw, decoded.Bytes()) {
				t.Fatalf("operator %d %s stored bytes differ: %v", index+1, item.kind, err)
			}
			if _, err := startifact.ReadAttemptObjectTo(t.Context(), stores[2-index], readBounds, item.kind, hash, &bytes.Buffer{}); err == nil {
				t.Fatalf("operator %d %s leaked to the other namespace", index+1, item.kind)
			}
		}
	}
	for operator, store := range stores {
		objects, err := store.List(t.Context(), store.Prefix()+"/st/v2/attempt/")
		if err != nil || len(objects) != 3 {
			t.Fatalf("operator %d typed object census=%d: %v", operator, len(objects), err)
		}
	}
}

// Incomplete admission never leaks even the first operator's usable callbacks.
func TestSimulatorAttemptCutV2StoreReplicasRejectInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, edit := range []func(*ResolvedConfig, map[int]server.BlobStore, *validator.AttemptCutV2Bounds){
		func(cfg *ResolvedConfig, _ map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { cfg.Public = nil },
		func(cfg *ResolvedConfig, _ map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { cfg.Config = nil },
		func(cfg *ResolvedConfig, _ map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { cfg.ChainID++ },
		func(cfg *ResolvedConfig, _ map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { cfg.Config.Topology.Operators = 1 },
		func(cfg *ResolvedConfig, _ map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { cfg.Config.Deployment.DeploymentID = "other" },
		func(_ *ResolvedConfig, stores map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { delete(stores, 2) },
		func(_ *ResolvedConfig, stores map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { stores[2] = nil },
		func(_ *ResolvedConfig, stores map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { stores[1], stores[2] = stores[2], stores[1] },
		func(_ *ResolvedConfig, stores map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { stores[0] = stores[2]; delete(stores, 2) },
		func(_ *ResolvedConfig, stores map[int]server.BlobStore, _ *validator.AttemptCutV2Bounds) { stores[3] = stores[2] },
		func(_ *ResolvedConfig, _ map[int]server.BlobStore, bounds *validator.AttemptCutV2Bounds) { bounds.MaxHeaderBytes = 0 },
		func(_ *ResolvedConfig, _ map[int]server.BlobStore, bounds *validator.AttemptCutV2Bounds) { bounds.MaxHeaderBytes = 33 },
		func(_ *ResolvedConfig, _ map[int]server.BlobStore, bounds *validator.AttemptCutV2Bounds) { bounds.Records.MaxItems = 0 },
		func(_ *ResolvedConfig, _ map[int]server.BlobStore, bounds *validator.AttemptCutV2Bounds) { bounds.Proofs.MaxItems = 0 },
		func(_ *ResolvedConfig, _ map[int]server.BlobStore, bounds *validator.AttemptCutV2Bounds) { bounds.Records.MaxPageBytes = ^uint64(0) },
	} {
		cfg, stores, bounds := newAttemptReplicaStoreTestConfig(t)
		originals := [2]server.BlobStore{stores[1], stores[2]}
		edit(cfg, stores, &bounds)
		replicas, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds)
		if err == nil {
			t.Fatal("invalid replica configuration was accepted")
		}
		for _, replica := range replicas {
			if replica.Origin != "" || replica.WriteRecords != nil || replica.WriteProofs != nil || replica.WriteMetadata != nil {
				t.Fatal("failed construction leaked a partially admitted publisher")
			}
		}
		for _, store := range originals {
			objects, err := store.List(t.Context(), store.Prefix()+"/")
			if err != nil || len(objects) != 0 {
				t.Fatalf("invalid configuration changed storage: objects=%d error=%v", len(objects), err)
			}
		}
	}
	if _, err := newAttemptCutV2StoreReplicas(nil, nil, validator.AttemptCutV2Bounds{}); err == nil {
		t.Fatal("nil deployment was accepted")
	}
}

// Plaintext stays restricted to the exact ordered testnet loopback tuple.
func TestSimulatorAttemptCutV2StoreReplicasEnforcePublicOrigins(t *testing.T) {
	t.Parallel()
	for _, origins := range [][]string{
		nil, {"https://no1.example"}, {"https://no1.example", "https://no1.example"},
		{"http://127.0.0.1:18082", "http://127.0.0.1:18081"},
		{"http://127.0.0.1:18081", "http://127.0.0.1:18083"},
		{"https://no1.example/path", "https://no2.example"},
		{"https://user:pass@no1.example", "https://no2.example"},
		{"https://127.0.0.1", "https://no2.example"},
	} {
		cfg, stores, bounds := newAttemptReplicaStoreTestConfig(t)
		cfg.OperatorAPIOrigins = origins
		if _, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds); err == nil {
			t.Fatalf("invalid origins were accepted: %q", origins)
		}
	}
	cfg, stores, bounds := newAttemptReplicaStoreTestConfig(t)
	cfg.Config.Deployment.Network = "bittensor-mainnet"
	if _, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds); err == nil {
		t.Fatal("testnet loopback origins were accepted for mainnet")
	}
	cfg.OperatorAPIOrigins = []string{"https://NO1.example/", "https://NO2.example/"}
	replicas, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds)
	if err != nil || replicas[0].Origin != "https://no1.example" || replicas[1].Origin != "https://no2.example" {
		t.Fatalf("bare public HTTPS origins failed canonical admission: %+v error=%v", replicas, err)
	}
}

// Returned callbacks own their store/bound values, not the mutable input maps.
func TestSimulatorAttemptCutV2StoreReplicasDetachConfiguration(t *testing.T) {
	t.Parallel()
	cfg, stores, bounds := newAttemptReplicaStoreTestConfig(t)
	original := stores[1]
	replicas, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds)
	if err != nil {
		t.Fatal(err)
	}
	clear(stores)
	cfg.OperatorAPIOrigins[0] = "https://changed.example"
	cfg.Config.Deployment.DeploymentID = "changed"
	bounds.Records.MaxChunkBytes = 1
	raw := []byte(strings.Repeat("r", 64))
	hash := fmt.Sprintf("0x%x", sha256.Sum256(raw))
	if err := replicas[0].WriteRecords(t.Context(), hash, raw); err != nil {
		t.Fatal(err)
	}
	var decoded bytes.Buffer
	_, err = startifact.ReadAttemptObjectTo(t.Context(), original, startifact.AttemptObjectBounds{MetadataBytes: 32, RecordBytes: 64, ProofBytes: 48}, "records", hash, &decoded)
	if err != nil || !bytes.Equal(raw, decoded.Bytes()) || replicas[0].Origin != "http://127.0.0.1:18081" {
		t.Fatalf("publisher retained mutable configuration: %v", err)
	}
}

// Each size refusal uses the correct hash; each hash refusal is within bounds.
func TestSimulatorAttemptCutV2StoreReplicasPreserveIndependentBounds(t *testing.T) {
	t.Parallel()
	cfg, stores, bounds := newAttemptReplicaStoreTestConfig(t)
	replicas, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		limit int
		write validator.AttemptStreamV2ObjectWriter
	}{
		{limit: 32, write: replicas[0].WriteMetadata},
		{limit: 64, write: replicas[0].WriteRecords},
		{limit: 48, write: replicas[0].WriteProofs},
	} {
		oversize := bytes.Repeat([]byte("x"), item.limit+1)
		if err := item.write(t.Context(), fmt.Sprintf("0x%x", sha256.Sum256(oversize)), oversize); err == nil {
			t.Fatalf("correct-hash oversized object passed limit %d", item.limit)
		}
		raw := oversize[:item.limit]
		if err := item.write(t.Context(), fmt.Sprintf("0x%x", sha256.Sum256([]byte("different"))), raw); err == nil {
			t.Fatalf("wrong-hash in-bound object passed limit %d", item.limit)
		}
		if err := item.write(t.Context(), fmt.Sprintf("0x%x", sha256.Sum256(raw)), raw); err != nil {
			t.Fatalf("exact bound %d was refused: %v", item.limit, err)
		}
	}
	objects, err := stores[1].List(t.Context(), stores[1].Prefix()+"/")
	if err != nil || len(objects) != 3 {
		t.Fatalf("refused objects reached storage: objects=%d error=%v", len(objects), err)
	}
}

// Overrides only actual storage admission while preserving real typed routing.
type attemptReplicaFailureStore struct {
	server.BlobStore
	failure error
}

// Refusal reaches callers without a successful publication acknowledgment.
func (self *attemptReplicaFailureStore) PutIfAbsent(context.Context, string, string, string) (bool, error) {
	return false, self.failure
}

// Cancellation and storage failures are not translated into empty success.
func TestSimulatorAttemptCutV2StoreReplicasPropagateFailure(t *testing.T) {
	t.Parallel()
	cfg, stores, bounds := newAttemptReplicaStoreTestConfig(t)
	failure := errors.New("owned test store refused publication")
	stores[2] = &attemptReplicaFailureStore{BlobStore: stores[2], failure: failure}
	replicas, err := newAttemptCutV2StoreReplicas(cfg, stores, bounds)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("records")
	hash := fmt.Sprintf("0x%x", sha256.Sum256(raw))
	if err := replicas[1].WriteRecords(t.Context(), hash, raw); !errors.Is(err, failure) {
		t.Fatalf("storage error was lost: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := replicas[0].WriteRecords(ctx, hash, raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
	if err := replicas[0].WriteRecords(nil, hash, raw); err == nil {
		t.Fatal("nil context was accepted")
	}
	for operator, store := range stores {
		objects, err := store.List(t.Context(), store.Prefix()+"/")
		if err != nil || len(objects) != 0 {
			t.Fatalf("operator %d stored failed publication: objects=%d error=%v", operator, len(objects), err)
		}
	}
}
