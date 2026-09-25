// Publication failures preserve bounded public detail while signed bytes,
// operator ownership and receipt validation remain unchanged.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real publisher signs and persists each request; only its transport is
// synthetic. Every response records whether the owned body was closed.
type evidencePublicationErrorFixture struct {
	cfg      *ResolvedConfig
	roles    *RoleSecrets
	stateDir string
	status   int
	detail   string
	calls    int
	closes   int
	wires    [][]byte
}

// Synthetic response ownership is synchronous with the real publisher call.
type evidencePublicationErrorBody struct {
	*strings.Reader
	fixture *evidencePublicationErrorFixture
}

// Closing a body records the actual publication reader's ownership release.
func (self *evidencePublicationErrorBody) Close() error {
	self.fixture.closes++
	return nil
}

// No production endpoint is contacted by this fixture.
func newEvidencePublicationErrorFixture(t *testing.T) *evidencePublicationErrorFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := &evidencePublicationErrorFixture{cfg: cfg, roles: roles, stateDir: t.TempDir(), status: http.StatusInsufficientStorage}
	prior := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		f.calls++
		if request.Method != http.MethodPost || request.URL.Path != "/sn/evidence" || request.Header.Get("Content-Type") != "application/json" {
			return nil, fmt.Errorf("unexpected publication request")
		}
		wire, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		if err := request.Body.Close(); err != nil {
			return nil, err
		}
		f.wires = append(f.wires, wire)
		detail := f.detail
		if f.status == http.StatusOK && detail == "" {
			var envelope ReleaseEvidenceEnvelope
			if err := json.Unmarshal(wire, &envelope); err != nil {
				return nil, err
			}
			receipt, err := json.Marshal(PublishedEvidence{ContentHash: envelope.ContentHash})
			if err != nil {
				return nil, err
			}
			detail = string(receipt)
		}
		return &http.Response{StatusCode: f.status, Header: make(http.Header), Body: &evidencePublicationErrorBody{Reader: strings.NewReader(detail), fixture: f}, Request: request}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prior })
	return f
}

// Capacity refusal is visible, one attempt remains one Post, and an explicit
// retry retains the same signed local envelope instead of inventing a new one.
func TestEvidencePublicationErrorKeepsCapacityDetailAndExactRetry(t *testing.T) {
	f := newEvidencePublicationErrorFixture(t)
	f.detail = "Evidence storage capacity exhausted; check bucket quota and disk headroom.\n"
	payload := map[string]bool{"synthetic": true}
	published, err := publishEvidence(t.Context(), f.cfg, f.roles, f.stateDir, "unit", "capacity-run", payload)
	if published != nil || err == nil || !strings.Contains(err.Error(), "HTTP 507") || !strings.Contains(err.Error(), "bucket quota and disk headroom") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("capacity failure lost its bounded public detail: published=%v error=%v", published, err)
	}
	if f.calls != 1 || f.closes != 1 {
		t.Fatalf("failed publication retried or leaked response ownership: calls=%d closes=%d", f.calls, f.closes)
	}
	local, err := os.ReadFile(filepath.Join(f.stateDir, "runs", "capacity-run", "unit.operator-1.evidence.json"))
	if err != nil || !bytes.Equal(bytes.TrimSpace(local), bytes.TrimSpace(f.wires[0])) {
		t.Fatalf("refused signed bytes were not retained exactly: %v", err)
	}
	f.status, f.detail = http.StatusOK, ""
	published, err = publishEvidence(t.Context(), f.cfg, f.roles, f.stateDir, "unit", "capacity-run", payload)
	if err != nil || len(published) != f.cfg.Config.Topology.Operators || !bytes.Equal(f.wires[0], f.wires[1]) || f.calls != 1+f.cfg.Config.Topology.Operators || f.closes != f.calls {
		t.Fatalf("explicit retry changed signed input or operator receipt census: published=%d calls=%d closes=%d error=%v", len(published), f.calls, f.closes, err)
	}
}

// Whitespace, control bytes and oversized backend detail cannot inflate the
// signed diagnostic or manufacture extra log lines.
func TestEvidencePublicationErrorBoundsAndEscapesDetail(t *testing.T) {
	f := newEvidencePublicationErrorFixture(t)
	for _, detail := range []string{"", "  \n\t", "storage\nsecond line\r\t\x00", strings.Repeat("x", 1024), strings.Repeat("x", 1025), strings.Repeat("\x00", 2048)} {
		f.detail = detail
		_, err := publishEvidence(t.Context(), f.cfg, f.roles, f.stateDir, "unit", "bounded-run", map[string]bool{"synthetic": true})
		want := "operator 1 evidence API returned HTTP 507"
		trimmed := strings.TrimSpace(detail)
		if trimmed != "" {
			if len(trimmed) > 1024 {
				trimmed = trimmed[:1024] + " [truncated]"
			}
			want += fmt.Sprintf(": %q", trimmed)
		}
		if err == nil || err.Error() != want || strings.ContainsAny(err.Error(), "\n\r\t\x00") || len(err.Error()) > 4200 {
			t.Fatalf("response detail escaped its bounded single-line contract: input=%d error=%v", len(detail), err)
		}
	}
	if f.calls != 6 || f.closes != f.calls {
		t.Fatalf("error detail processing changed request ownership: calls=%d closes=%d", f.calls, f.closes)
	}
}

// Public diagnostics do not bypass the existing response-size and exact
// signed receipt gates, including a successful status with a foreign hash.
func TestEvidencePublicationErrorKeepsReadAndReceiptRefusals(t *testing.T) {
	f := newEvidencePublicationErrorFixture(t)
	for _, sample := range []struct {
		status int
		detail string
		want   string
	}{
		{status: http.StatusInsufficientStorage, detail: strings.Repeat("x", 1024*1024+1), want: "exceeds"},
		{status: http.StatusOK, detail: `{"content_hash":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}`, want: "evidence receipt is invalid"},
		{status: http.StatusBadRequest, detail: "Evidence rejected.", want: "HTTP 400: \"Evidence rejected.\""},
	} {
		f.status, f.detail = sample.status, sample.detail
		published, err := publishEvidence(t.Context(), f.cfg, f.roles, f.stateDir, "unit", "strict-run", map[string]bool{"synthetic": true})
		if published != nil || err == nil || !strings.Contains(err.Error(), sample.want) {
			t.Fatalf("publication weakened existing refusal: status=%d published=%v error=%v", sample.status, published, err)
		}
	}
	if f.calls != 3 || f.closes != f.calls {
		t.Fatalf("refusal lost response ownership: calls=%d closes=%d", f.calls, f.closes)
	}
}
