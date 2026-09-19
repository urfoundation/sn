//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

func artifactHttpCaptureTest(t testing.TB, endpoint string) *releaseArtifactCaptureReaderV2 {
	t.Helper()
	stateDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := crv4.KeypairFromSeed([32]byte{67})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ReleaseConfig{StateDir: stateDir, DeploymentID: "http-observation-test", ChainID: 945, GenesisHash: "0x" + strings.Repeat("11", 32), Coordinator: "0x" + strings.Repeat("22", 20), SettlementVault: "0x" + strings.Repeat("33", 20), PolicyHash: "0x" + strings.Repeat("44", 32), ValidatorID: 7, Netuid: 521}
	cfg.Policy.Deposit.UsageLagEpochs = 1
	cfg.EvidenceV2.Bounds.MaxArtifactBytes = 1024 * 1024
	cfg.EvidenceV2.Bounds.MaxControlBytes = 8 * 1024 * 1024
	cfg.EvidenceV2.Bounds.MaxHistoryBytes = 16 * 1024 * 1024
	decision := &ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash, Coordinator: cfg.Coordinator, SettlementVault: cfg.SettlementVault, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, PolicyHash: cfg.PolicyHash, SubnetEpoch: 20, NativeSnapshotBlock: 501, NativeSnapshotHash: "0x" + strings.Repeat("55", 32), EVMSnapshotBlock: 500, EVMSnapshotHash: "0x" + strings.Repeat("66", 32), SettlementEpoch: 2, SelfUID: 7}
	reader, err := NewHTTPArtifactReader(endpoint, cfg.DeploymentID, cfg.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	return &releaseArtifactCaptureReaderV2{ctx: t.Context(), cfg: cfg, hotkey: key, decision: decision, reader: reader}
}

func TestArtifactHttpObservationRetainsNegativeAndReplaysWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"actual":"temporarily unavailable"}`))
	}))
	t.Cleanup(server.Close)
	reader := artifactHttpCaptureTest(t, server.URL)
	artifact, observedErr, hash, err := reader.capture(t.Context(), 1, 9)
	if err != nil || artifact != nil || !errors.Is(observedErr, ErrArtifactUnavailable) || hash == "" || requests.Load() != 1 {
		t.Fatalf("actual negative capture artifact=%v observation=%v hash=%s fatal=%v calls=%d", artifact, observedErr, hash, err, requests.Load())
	}
	server.Close()
	again, againErr, againHash, err := reader.capture(t.Context(), 1, 9)
	if err != nil || again != nil || !errors.Is(againErr, ErrArtifactUnavailable) || againErr.Error() != observedErr.Error() || againHash != hash || requests.Load() != 1 {
		t.Fatalf("retained replay consulted or changed live evidence: %v %v %s calls=%d", err, againErr, againHash, requests.Load())
	}
}

func TestArtifactHttpObservationRetainsExactInvalidHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"schema":"wrong","objects":[]}`))
	}))
	t.Cleanup(server.Close)
	reader := artifactHttpCaptureTest(t, server.URL)
	_, observedErr, hash, err := reader.capture(t.Context(), 1, 9)
	if err != nil || observedErr == nil || errors.Is(observedErr, ErrArtifactUnavailable) || hash == "" {
		t.Fatalf("invalid history evidence changed classification: %v %v", observedErr, err)
	}
	expected, path, err := releaseArtifactHttpRequestV2(reader.cfg, reader.hotkey.PublicKey(), reader.decision, 9, 1, reader.reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeArtifactHttpObservationV2(t.Context(), encoded, 1024*1024, expected)
	if err != nil || len(value.Exchanges) != 1 || !bytes.Equal(value.Exchanges[0].Body, []byte(`{"schema":"wrong","objects":[]}`)) {
		t.Fatalf("exact received invalid bytes were not retained: %v", err)
	}
}

func TestArtifactHttpObservationRejectsTamperAndWrongSignedBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"schema":"urnetwork-payout-artifact-history-v1","objects":[]}`))
	}))
	t.Cleanup(server.Close)
	reader := artifactHttpCaptureTest(t, server.URL)
	if _, _, _, err := reader.capture(t.Context(), 1, 9); err != nil {
		t.Fatal(err)
	}
	expected, path, err := releaseArtifactHttpRequestV2(reader.cfg, reader.hotkey.PublicKey(), reader.decision, 9, 1, reader.reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var original releaseArtifactHttpObservationV2
	if err := json.Unmarshal(encoded, &original); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"body", "native", "operator", "origin"} {
		var value releaseArtifactHttpObservationV2
		if err := json.Unmarshal(encoded, &value); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "body":
			value.Exchanges[0].Body[0] ^= 1
		case "native":
			value.Decision.NativeSnapshotBlock++
		case "operator":
			value.NoId++
		case "origin":
			value.Origin = "http://127.0.0.1:1"
		}
		if fault != "body" {
			digest, err := releaseArtifactHttpObservationDigestV2(value)
			if err != nil {
				t.Fatal(err)
			}
			signature, err := reader.hotkey.Sign(digest[:])
			if err != nil {
				t.Fatal(err)
			}
			value.Signature = hex.EncodeToString(signature)
		}
		changed, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeArtifactHttpObservationV2(t.Context(), changed, 1024*1024, expected); err == nil {
			t.Fatalf("tampered or validly signed wrong request %s was accepted", fault)
		}
	}
	original.Exchanges = append(original.Exchanges, original.Exchanges[0])
	if _, _, err := replayArtifactHttpObservationV2(t.Context(), reader.reader, original); err == nil {
		t.Fatal("surplus retained response was accepted")
	}
}

func TestArtifactHttpObservationCancellationDoesNotPublishNegative(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	reader := artifactHttpCaptureTest(t, server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	artifact, observedErr, hash, err := reader.capture(ctx, 1, 9)
	if !errors.Is(err, context.Canceled) || artifact != nil || observedErr != nil || hash != "" || calls.Load() != 0 {
		t.Fatalf("cancellation became a historical negative: %v %v %s calls=%d", err, observedErr, hash, calls.Load())
	}
}

func TestNativeSourceHashCommitsExactPrePreparedBytes(t *testing.T) {
	first := []byte(`{"schema":"urnetwork-validator-release-measurement-v2","source":"a"}`)
	second := bytes.Clone(first)
	second[len(second)-3] = 'b'
	if releaseNativeSourceHashV2(first) == releaseNativeSourceHashV2(second) || releaseNativeSourceHashV2(first) == releaseNativeSourceHashV2(append(bytes.Clone(first), '\n')) {
		t.Fatal("native source hash omitted exact source bytes")
	}
}
