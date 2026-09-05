// Prove local verification reuse from owned, authenticated bytes while keeping
// every operator fetch, identity check, and public failure boundary observable.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Release the borrowed transport buffer as soon as the caller finishes reading.
type campaignReplicaTestBody struct {
	io.Reader
	close func()
}

// The callback deterministically reuses the carrier after its read completes.
func (self *campaignReplicaTestBody) Close() error {
	self.close()
	return nil
}

// Produce a real owner signature with two independent operator identities.
func campaignReplicaVerificationFixture(t *testing.T) (*PublicDeploymentManifest, *ReleaseEvidenceEnvelope, []byte) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := signEvidence(cfg, campaignEvidenceManifestKind, "release-replica-run", map[string]string{"schema": campaignEvidenceManifestSchema}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{
		DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		Operators: []PublicOperator{
			{NoID: 1, APIURL: "https://operator-1.example"},
			{NoID: 2, APIURL: "https://operator-2.example"},
		},
	}
	return public, envelope, encoded
}

// Identical replicas share one completed signature verification only inside
// this invocation; all transport buffers may be reused before the next read.
func TestFinalSemanticReplicatedEnvelopeVerifiesOwnedBytesOnce(t *testing.T) {
	t.Parallel()
	public, signed, encoded := campaignReplicaVerificationFixture(t)
	var carrier []byte
	requests, closedBodies := 0, 0
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		wantHost := fmt.Sprintf("operator-%d.example", (requests-1)%2+1)
		if request.URL.Host != wantHost || request.URL.Query().Get("hash") != signed.ContentHash {
			return nil, fmt.Errorf("unexpected replica request %s", request.URL)
		}
		carrier = append(carrier[:0], encoded...)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request, Body: &campaignReplicaTestBody{
			Reader: bytes.NewReader(carrier), close: func() { clear(carrier); closedBodies++ },
		}}, nil
	})}}
	verifications := 0
	for range 2 {
		actual, err := probe.fetchReplicatedCampaignEnvelopeWithVerify(context.Background(), public, signed.ContentHash, signed.Kind, signed.RunID, signed.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes, func(envelope *ReleaseEvidenceEnvelope) error {
			verifications++
			return verifyEvidence(envelope, nil)
		})
		if err != nil || !evidenceEnvelopesEqual(actual, signed) {
			t.Fatalf("owned replicated envelope changed: error = %v", err)
		}
		clear(actual.Payload)
	}
	if requests != 4 || closedBodies != 4 || verifications != 2 {
		t.Fatalf("replica work = %d requests / %d closed bodies / %d verifications, want 4 / 4 / 2 with fresh verification in each call", requests, closedBodies, verifications)
	}
}

// Byte-different replicas retain strict validation before the exact-byte
// mismatch verdict, and failed replicas never become reusable evidence.
func TestFinalSemanticReplicatedEnvelopePreservesEveryRejection(t *testing.T) {
	t.Parallel()
	public, signed, encoded := campaignReplicaVerificationFixture(t)
	reencoded, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mutated := func(change func(*ReleaseEvidenceEnvelope)) []byte {
		copyEnvelope := *signed
		copyEnvelope.Payload = append([]byte(nil), signed.Payload...)
		change(&copyEnvelope)
		data, err := json.Marshal(&copyEnvelope)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	badPayload := mutated(func(envelope *ReleaseEvidenceEnvelope) { envelope.Payload = json.RawMessage(`{"schema":"changed"}`) })
	badSignature := mutated(func(envelope *ReleaseEvidenceEnvelope) { envelope.Signature = "0x" + strings.Repeat("00", 65) })
	badHash := mutated(func(envelope *ReleaseEvidenceEnvelope) { envelope.ContentHash = "sha256:" + strings.Repeat("ab", 32) })
	badDomain := mutated(func(envelope *ReleaseEvidenceEnvelope) { envelope.DeploymentID += "-other" })
	transportErr := errors.New("fixture replica transport failure")
	for _, item := range []struct {
		name         string
		first        []byte
		second       []byte
		status       int
		transportErr error
		wantError    string
		wantRequests int
		wantVerifies int
	}{
		{name: "different whitespace", first: encoded, second: reencoded, wantError: "differs between operator replicas", wantRequests: 2, wantVerifies: 2},
		{name: "changed payload", first: encoded, second: badPayload, wantError: "operator 2 returned invalid campaign evidence", wantRequests: 2, wantVerifies: 2},
		{name: "changed signature", first: encoded, second: badSignature, wantError: "operator 2 returned invalid campaign evidence", wantRequests: 2, wantVerifies: 2},
		{name: "changed hash", first: encoded, second: badHash, wantError: "operator 2 returned invalid campaign evidence", wantRequests: 2, wantVerifies: 2},
		{name: "changed domain", first: encoded, second: badDomain, wantError: "operator 2 returned invalid campaign evidence", wantRequests: 2, wantVerifies: 2},
		{name: "invalid first replica", first: badSignature, second: encoded, wantError: "operator 1 returned invalid campaign evidence", wantRequests: 1, wantVerifies: 1},
		{name: "missing second replica", first: encoded, status: http.StatusNotFound, wantError: "HTTP 404", wantRequests: 2, wantVerifies: 1},
		{name: "second transport error", first: encoded, transportErr: transportErr, wantError: transportErr.Error(), wantRequests: 2, wantVerifies: 1},
		{name: "second exceeds bound", first: encoded, second: bytes.Repeat([]byte{' '}, len(encoded)+1), wantError: "size limit", wantRequests: 2, wantVerifies: 1},
	} {
		requests, verifications := 0, 0
		probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			raw, status := item.first, http.StatusOK
			if requests == 2 {
				if item.transportErr != nil {
					return nil, item.transportErr
				}
				raw = item.second
				if item.status != 0 {
					status = item.status
				}
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Request: request, Body: io.NopCloser(bytes.NewReader(raw))}, nil
		})}}
		limit := int64(max(len(encoded), len(reencoded)))
		if item.name == "second exceeds bound" {
			limit = int64(len(encoded))
		}
		actual, err := probe.fetchReplicatedCampaignEnvelopeWithVerify(context.Background(), public, signed.ContentHash, signed.Kind, signed.RunID, signed.Signer.Hex(), limit, func(envelope *ReleaseEvidenceEnvelope) error {
			verifications++
			return verifyEvidence(envelope, nil)
		})
		if actual != nil || err == nil || !strings.Contains(err.Error(), item.wantError) || requests != item.wantRequests || verifications != item.wantVerifies {
			t.Errorf("%s: error = %v, requests = %d/%d, verifications = %d/%d", item.name, err, requests, item.wantRequests, verifications, item.wantVerifies)
		}
	}
}

// Every replica must bind the currently requested deployment even when its
// bytes equal the owned, previously authenticated copy from this call.
func TestFinalSemanticReplicatedEnvelopeRechecksCurrentIdentity(t *testing.T) {
	t.Parallel()
	public, signed, encoded := campaignReplicaVerificationFixture(t)
	for _, change := range []func(*PublicDeploymentManifest){
		func(public *PublicDeploymentManifest) { public.DeploymentID += "-other" },
		func(public *PublicDeploymentManifest) { public.ChainID++ },
		func(public *PublicDeploymentManifest) { public.Netuid++ },
		func(public *PublicDeploymentManifest) { public.GenesisHash = "0x" + strings.Repeat("ab", 32) },
	} {
		current := *public
		requests := 0
		probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if requests == 2 {
				change(&current)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request, Body: io.NopCloser(bytes.NewReader(encoded))}, nil
		})}}
		actual, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), &current, signed.ContentHash, signed.Kind, signed.RunID, signed.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes)
		if actual != nil || err == nil || !strings.Contains(err.Error(), "operator 2 returned invalid campaign evidence") || requests != 2 {
			t.Fatalf("changed expected domain reused prior identity: requests = %d, error = %v", requests, err)
		}
	}
	response := encoded
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})}}
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, signed.ContentHash, signed.Kind, signed.RunID, signed.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []struct {
		hash   string
		kind   string
		runID  string
		signer string
	}{
		{hash: "sha256:" + strings.Repeat("ab", 32), kind: signed.Kind, runID: signed.RunID, signer: signed.Signer.Hex()},
		{hash: signed.ContentHash, kind: "other-kind", runID: signed.RunID, signer: signed.Signer.Hex()},
		{hash: signed.ContentHash, kind: signed.Kind, runID: "other-run", signer: signed.Signer.Hex()},
		{hash: signed.ContentHash, kind: signed.Kind, runID: signed.RunID, signer: "0x" + strings.Repeat("ab", 20)},
	} {
		if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, identity.hash, identity.kind, identity.runID, identity.signer, maximumCampaignEvidenceEnvelopeBytes); err == nil || !strings.Contains(err.Error(), "operator 1 returned invalid campaign evidence") {
			t.Fatalf("a new invocation reused a prior requested identity: %v", err)
		}
	}
	response = bytes.ReplaceAll(encoded, []byte(signed.Signature), []byte("0x"+strings.Repeat("00", 65)))
	if _, err := probe.fetchReplicatedCampaignEnvelope(context.Background(), public, signed.ContentHash, signed.Kind, signed.RunID, signed.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes); err == nil || !strings.Contains(err.Error(), "operator 1 returned invalid campaign evidence") {
		t.Fatalf("a new invocation reused a prior signature verdict: %v", err)
	}
}

// Cancellation after the last response is consumed must stop verification,
// including the branch that recognizes a byte-identical authenticated replica.
func TestFinalSemanticReplicatedEnvelopeHonorsCancellationAfterRead(t *testing.T) {
	t.Parallel()
	public, signed, encoded := campaignReplicaVerificationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests, verifications := 0, 0
	probe := &liveScenarioProbe{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request, Body: &campaignReplicaTestBody{
			Reader: bytes.NewReader(encoded), close: func() {
				if requests == 2 {
					cancel()
				}
			},
		}}, nil
	})}}
	actual, err := probe.fetchReplicatedCampaignEnvelopeWithVerify(ctx, public, signed.ContentHash, signed.Kind, signed.RunID, signed.Signer.Hex(), maximumCampaignEvidenceEnvelopeBytes, func(envelope *ReleaseEvidenceEnvelope) error {
		verifications++
		return verifyEvidence(envelope, nil)
	})
	if actual != nil || !errors.Is(err, context.Canceled) || requests != 2 || verifications != 1 {
		t.Fatalf("last replica cancellation: error = %v, requests = %d, verifications = %d", err, requests, verifications)
	}
}
