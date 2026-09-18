//go:build linux || darwin

package validator

// Typed evidence has an independent finite payload budget. Local HTTP checks
// preserve the default stream cap and exact hash, length, EOF and cancellation.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The page and header limits match the small stream geometry; only the
// separately supplied transition allowance can admit the larger payload.
func evidenceMetadataV2TestBounds() ReleaseEvidenceV2Bounds {
	bounds := attemptCutV2ReplicaTestBounds()
	bounds.MaxHeaderBytes = 64 * 1024
	bounds.Records.MaxPageBytes, bounds.Proofs.MaxPageBytes = 64*1024, 64*1024
	return ReleaseEvidenceV2Bounds{Cut: bounds, MaxTransitionBytes: 4 * 1024 * 1024}
}

func TestValidatorEvidenceMetadataTransportKeepsDefaultBoundsAndExactBytes(t *testing.T) {
	bounds := evidenceMetadataV2TestBounds()
	raw := []byte(`{"payload":"` + strings.Repeat("x", 80*1024) + "\"}\n")
	hash := attemptHex32(sha256.Sum256(raw))
	var requests, credentials atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method == http.MethodPost {
			data, err := io.ReadAll(request.Body)
			if err != nil || !bytes.Equal(data, raw) || request.Header.Get("Authorization") != "Bearer synthetic-session" {
				t.Errorf("typed upload lost exact bytes or session: %v", err)
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			response.Header().Set("ETag", `"`+hash+`"`)
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if request.Header.Get("Authorization") != "" {
			t.Error("public read included upload credentials")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(raw)
	}))
	defer endpoint.Close()
	getter := func() string { credentials.Add(1); return "synthetic-session" }
	ordinaryReader, err := NewHTTPAttemptStreamV2Reader(endpoint.URL, bounds.Cut)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryWriter, err := NewHTTPAttemptStreamV2Writer(endpoint.URL, bounds.Cut, getter)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := ordinaryReader.ReadMetadata(t.Context(), hash, uint64(len(raw))); err == nil || value != nil {
		t.Fatal("default stream reader admitted evidence-sized metadata")
	}
	if err := ordinaryWriter.Write(t.Context(), "metadata", hash, raw); err == nil || requests.Load() != 0 || credentials.Load() != 0 {
		t.Fatal("default stream writer reached credentials or HTTP above its page bound")
	}
	owner, err := newReleaseAttemptUploadV2(t.Context(), OperatorConfig{NoID: 1, APIURL: endpoint.URL}, bounds, getter)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if err := owner.write(t.Context(), "metadata", hash, raw); err != nil {
		t.Fatalf("typed release upload: %v", err)
	}
	reader, err := newHttpAttemptStreamV2Reader(endpoint.URL, bounds.Cut, bounds.MaxTransitionBytes)
	if err != nil {
		t.Fatal(err)
	}
	value, err := reader.ReadMetadata(t.Context(), hash, uint64(len(raw)))
	if err != nil || !bytes.Equal(value, raw) || requests.Load() != 2 || credentials.Load() != 1 {
		t.Fatalf("typed metadata exact round trip: bytes=%d, requests=%d, credentials=%d, error=%v", len(value), requests.Load(), credentials.Load(), err)
	}
	oversized := make([]byte, bounds.MaxTransitionBytes+1)
	if err := owner.write(t.Context(), "metadata", attemptHex32(sha256.Sum256(oversized)), oversized); err == nil {
		t.Fatal("typed writer admitted an over-budget transition")
	}
	if value, err := reader.ReadMetadata(t.Context(), hash, bounds.MaxTransitionBytes+1); err == nil || value != nil {
		t.Fatal("typed reader admitted an over-budget transition")
	}
	if body, err := reader.OpenData(t.Context(), AttemptStreamV2Records, hash, bounds.Cut.Records.MaxChunkBytes+1); err == nil || body != nil {
		t.Fatal("typed metadata allowance enlarged the record chunk limit")
	}
	if err := owner.write(t.Context(), "metadata", attemptHex32([32]byte{1}), raw); err == nil {
		t.Fatal("typed writer accepted a different hash")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if value, err := reader.ReadMetadata(cancelled, hash, uint64(len(raw))); value != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled typed read returned bytes")
	}
	if requests.Load() != 2 || credentials.Load() != 1 {
		t.Fatal("refused typed request reached HTTP or the credential getter")
	}
	for _, limit := range []uint64{0, ^uint64(0)} {
		if value, err := newHttpAttemptStreamV2Reader(endpoint.URL, bounds.Cut, limit); err == nil || value != nil {
			t.Fatal("invalid explicit metadata reader allowance was admitted")
		}
		if value, err := newHttpAttemptStreamV2Writer(endpoint.URL, bounds.Cut, limit, getter); err == nil || value != nil {
			t.Fatal("invalid explicit metadata writer allowance was admitted")
		}
	}
}

func TestValidatorEvidenceMetadataTransportRefusesIncompleteOrChangedBody(t *testing.T) {
	bounds := evidenceMetadataV2TestBounds()
	raw := []byte(strings.Repeat("x", 80*1024))
	hash := attemptHex32(sha256.Sum256(raw))
	for _, fault := range []string{"hash", "short", "trailing"} {
		endpoint := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusOK)
			response.(http.Flusher).Flush()
			data := bytes.Clone(raw)
			switch fault {
			case "hash":
				data[len(data)-1] ^= 1
			case "short":
				data = data[:len(data)-1]
			case "trailing":
				data = append(data, 'x')
			}
			_, _ = response.Write(data)
		}))
		reader, err := newHttpAttemptStreamV2Reader(endpoint.URL, bounds.Cut, bounds.MaxTransitionBytes)
		if err != nil {
			endpoint.Close()
			t.Fatal(err)
		}
		value, readErr := reader.ReadMetadata(t.Context(), hash, uint64(len(raw)))
		endpoint.Close()
		if readErr == nil || value != nil {
			t.Fatalf("%s typed response returned partial or unauthenticated bytes", fault)
		}
	}
}
