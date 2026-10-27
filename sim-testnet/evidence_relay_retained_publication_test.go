//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/startifact"
	"gopkg.in/yaml.v3"
)

// The small genuine runtime manifest binds each operator's exact private store
// bytes. Transport construction never connects to the fabricated test authority.
func TestEvidenceRelayRetainedPublicationAuthenticatesStoppedCaptureStores(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	cfg.OperatorAPIOrigins = []string{"http://127.0.0.1:18081", "http://127.0.0.1:18082"}
	cfg.readOnlyAudit = true
	cfg.relayCapturePlanHash = common.Hash{0x51}.Hex()
	plan := &SetupPlan{PlanHash: cfg.relayCapturePlanHash}
	for operator := 1; operator <= 2; operator++ {
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := yaml.Marshal(renderedOperatorBlobConfig{Authority: "127.0.0.1:29000", AccessKey: "test-access", SecretKey: "test-secret", Bucket: "blob", Prefix: prefix})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", "minio.yml"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	stores, err := newEvidenceRelayRetainedPublications(t.Context(), cfg, stateDir, plan)
	if err != nil || stores == nil || stores.stores[0] == nil || stores.stores[1] == nil || stores.stores[0].Prefix() == stores.stores[1].Prefix() {
		t.Fatal("stopped capture did not retain both authenticated namespaces", err)
	}
	cfg.readOnlyAudit = false
	if stores, err := newEvidenceRelayRetainedPublications(t.Context(), cfg, stateDir, plan); err == nil || stores != nil {
		t.Fatal("live relay obtained stopped-capture store authority", err)
	}
	cfg.readOnlyAudit = true
	changed := *plan
	changed.PlanHash = common.Hash{0x52}.Hex()
	if stores, err := newEvidenceRelayRetainedPublications(t.Context(), cfg, stateDir, &changed); err == nil || stores != nil {
		t.Fatal("another plan borrowed the stopped capture stores", err)
	}
	path := filepath.Join(stateDir, "runtime", "operator-2", "vault", "minio.yml")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{nil, bytes.ReplaceAll(original, []byte("operator-2"), []byte("operator-1")), append(bytes.Clone(original), []byte("# changed\n")...)} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if stores, err := newEvidenceRelayRetainedPublications(t.Context(), cfg, stateDir, plan); err == nil || stores != nil {
			t.Fatal("changed retained store configuration escaped its manifest", err)
		}
	}
}

type evidenceRelayRetainedCloseFailureStore struct{ server.BlobStore }
type evidenceRelayRetainedCloseFailureReader struct{ io.ReadCloser }

var errEvidenceRelayRetainedCloseTest = errors.New("retained test reader close failed")

func (self evidenceRelayRetainedCloseFailureStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := self.BlobStore.Get(ctx, key)
	if err != nil {
		return reader, err
	}
	return evidenceRelayRetainedCloseFailureReader{ReadCloser: reader}, nil
}

func (self evidenceRelayRetainedCloseFailureReader) Close() error {
	return errors.Join(self.ReadCloser.Close(), errEvidenceRelayRetainedCloseTest)
}

// Real typed blob objects remain independent of the stopped HTTP processes.
// Missing, changed, excessive and uncleanly closed bytes never produce a result.
func TestEvidenceRelayRetainedPublicationReadsExactObjectsAndRejectsReplicaFaults(t *testing.T) {
	cfg, stores, cut := newAttemptReplicaStoreTestConfig(t)
	bounds := validatorcomponent.ReleaseEvidenceV2Bounds{Cut: cut, MaxTransitionBytes: 32}
	owner := &evidenceRelayRetainedPublications{origins: [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]}, stores: [2]server.BlobStore{stores[1], stores[2]}}
	raw := []byte("retained-publication\n")
	hash := fmt.Sprintf("0x%x", sha256.Sum256(raw))
	objectBounds := startifact.AttemptObjectBounds{MetadataBytes: 32, RecordBytes: cut.Records.MaxChunkBytes, ProofBytes: cut.Proofs.MaxChunkBytes}
	for _, store := range owner.stores {
		if err := startifact.PublishAttemptObject(t.Context(), store, objectBounds, "metadata", hash, raw); err != nil {
			t.Fatal(err)
		}
	}
	replicas, err := owner.readers(bounds)
	if err != nil {
		t.Fatal(err)
	}
	for index, replica := range replicas {
		actual, err := replica.ReadMetadata(t.Context(), hash, uint64(len(raw)))
		if err != nil || !bytes.Equal(raw, actual) || replica.Origin != cfg.OperatorAPIOrigins[index] {
			t.Fatal("retained replica differs from its exact original object", index, err)
		}
		for _, size := range []uint64{0, uint64(len(raw) - 1), uint64(len(raw) + 1), 33} {
			if actual, err := replica.ReadMetadata(t.Context(), hash, size); err == nil || actual != nil {
				t.Fatal("incorrect declared size exposed retained bytes", size, err)
			}
		}
	}
	owner.stores[1] = evidenceRelayRetainedCloseFailureStore{BlobStore: stores[2]}
	failed, err := owner.readers(bounds)
	if err != nil {
		t.Fatal(err)
	}
	if actual, err := failed[1].ReadMetadata(t.Context(), hash, uint64(len(raw))); !errors.Is(err, errEvidenceRelayRetainedCloseTest) || actual != nil {
		t.Fatal("failed retained close exposed partial authority", err)
	}
	key, err := startifact.AttemptObjectKey(stores[2], "metadata", hash)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(strings.TrimPrefix(stores[2].Authority(), "local:"), filepath.FromSlash(key))
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, len(raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	if actual, err := replicas[1].ReadMetadata(t.Context(), hash, uint64(len(raw))); err == nil || actual != nil {
		t.Fatal("wrong retained hash was accepted", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if actual, err := replicas[1].ReadMetadata(t.Context(), hash, uint64(len(raw))); err == nil || actual != nil {
		t.Fatal("missing second replica borrowed the first object", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if actual, err := replicas[0].ReadMetadata(ctx, hash, uint64(len(raw))); !errors.Is(err, context.Canceled) || actual != nil {
		t.Fatal("canceled retained read returned bytes", err)
	}
}
