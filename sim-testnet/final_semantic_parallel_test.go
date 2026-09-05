package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// Bound release-scale test views independently of host CPU count. Each view
// still exercises the complete signed graph and real verifier.
const finalSemanticTestCaseWorkers = 4

type finalSemanticTestCase struct {
	name   string
	verify func(context.Context) error
}

// Join every case and retain source order, including failures. A failed
// adversarial case must neither cancel nor conceal its independent neighbors.
func runFinalSemanticTestCases(ctx context.Context, cases []finalSemanticTestCase) []error {
	results := make([]error, len(cases))
	indices := make(chan int)
	var workers sync.WaitGroup
	for range min(finalSemanticTestCaseWorkers, len(cases)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range indices {
				results[index] = cases[index].verify(ctx)
			}
		}()
	}
	for index := range cases {
		indices <- index
	}
	close(indices)
	workers.Wait()
	return results
}

// Raw signed byte slices are immutable after publication. Only this view's
// maps, history slices, probe and HTTP client may change between cases.
type finalPublicScenarioTestView struct {
	objects              map[string][]byte
	commitVisible        map[int]bool
	supplementHistory    map[int][]string
	objectOverrides      map[int]map[string][]byte
	directOwnerVisible   bool
	trustedEvidenceOwner common.Address
	campaignResultVerify func(*ResolvedConfig, *ScenarioResult, string) error
	missingHash          string
	wantError            string
}

func (self finalPublicScenarioTestView) snapshot() finalPublicScenarioTestView {
	self.objects = maps.Clone(self.objects)
	self.commitVisible = maps.Clone(self.commitVisible)
	history := make(map[int][]string, len(self.supplementHistory))
	for operator, hashes := range self.supplementHistory {
		history[operator] = append([]string(nil), hashes...)
	}
	self.supplementHistory = history
	overrides := make(map[int]map[string][]byte, len(self.objectOverrides))
	for operator, objects := range self.objectOverrides {
		overrides[operator] = maps.Clone(objects)
	}
	self.objectOverrides = overrides
	return self
}

// Record transport/body failures even when discovery correctly ignores a bad
// candidate. Only the explicitly injected missing hash may return HTTP 404.
type finalSemanticTestResponseBody struct {
	io.ReadCloser
	record func(error)
}

func (self *finalSemanticTestResponseBody) Read(buffer []byte) (int, error) {
	n, err := self.ReadCloser.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		self.record(err)
	}
	return n, err
}

// Give each adversarial snapshot a real, independent TLS origin and probe.
// No per-case mutation reaches another handler or the immutable signed bytes.
func verifyFinalPublicScenarioTestView(ctx context.Context, cfg *ResolvedConfig, public *PublicDeploymentManifest, runID, phase, manifestURI, completionHash string, bundleHashes, commitHashes []string, view finalPublicScenarioTestView) error {
	historyKey := func(kind, hash string) string {
		return "fixture/st/v1/evidence/history/" + cfg.Config.Deployment.DeploymentID + "/" + fmt.Sprint(cfg.Netuid) + "/" + kind + "/" + runID + "/" + strings.TrimPrefix(hash, "sha256:") + ".json"
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operator := 0
		if strings.HasPrefix(r.URL.Path, "/operator-1/") {
			operator = 1
		} else if strings.HasPrefix(r.URL.Path, "/operator-2/") {
			operator = 2
		}
		switch {
		case operator != 0 && strings.HasSuffix(r.URL.Path, "/sn/evidence/history"):
			var keys []map[string]string
			switch r.URL.Query().Get("kind") {
			case "scenario-bundle":
				keys = append(keys, map[string]string{"key": historyKey("scenario-bundle", bundleHashes[operator-1])})
			case "scenario-complete-commit":
				if view.directOwnerVisible {
					keys = append(keys, map[string]string{"key": historyKey("scenario-complete-commit", completionHash)})
				}
				if view.commitVisible[operator] {
					keys = append(keys, map[string]string{"key": historyKey("scenario-complete-commit", commitHashes[operator-1])})
				}
			case finalSemanticSupplementKind:
				for _, hash := range view.supplementHistory[operator] {
					keys = append(keys, map[string]string{"key": historyKey(finalSemanticSupplementKind, hash)})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": "urnetwork-release-evidence-history-v1", "objects": keys})
		case operator != 0 && strings.HasSuffix(r.URL.Path, "/sn/evidence"):
			hash := r.URL.Query().Get("hash")
			encoded, overridden := view.objectOverrides[operator][hash]
			if !overridden {
				encoded = view.objects[hash]
			}
			if len(encoded) == 0 {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(encoded)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, serverPort, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		return err
	}
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // isolated httptest listener
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	var stateLock sync.Mutex
	var transportErrs []error
	record := func(err error) {
		stateLock.Lock()
		defer stateLock.Unlock()
		transportErrs = append(transportErrs, err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err != nil {
			record(err)
			return response, err
		}
		if response.StatusCode != http.StatusOK && !(response.StatusCode == http.StatusNotFound && view.missingHash != "" && request.URL.Query().Get("hash") == view.missingHash) {
			record(fmt.Errorf("unexpected fixture HTTP %d at %s", response.StatusCode, request.URL.Path))
		}
		response.Body = &finalSemanticTestResponseBody{ReadCloser: response.Body, record: record}
		return response, nil
	})}
	manifest := *public
	manifest.Operators = nil
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		base := fmt.Sprintf("https://example.com:%s/operator-%d", serverPort, operator)
		manifest.Operators = append(manifest.Operators, PublicOperator{NoID: operator, APIURL: base, HistoryURL: base + "/sn/evidence/history"})
	}
	probe := &liveScenarioProbe{
		cfg: cfg, client: client, trustedEvidenceOwner: view.trustedEvidenceOwner,
		publicManifestURI: manifestURI, campaignResultVerify: view.campaignResultVerify,
		finalSemanticVerify: func(ctx context.Context, _ *PublicDeploymentManifest, evidence *FinalSemanticEvidence, _ string) error {
			return VerifyFinalSemanticEvidenceOnChain(ctx, evidence, &finalTestChainReader{evidence: evidence})
		},
	}
	authenticated, verifyErr := probe.fetchAuthenticatedScenarioCampaign(ctx, &manifest, runID, phase)
	stateLock.Lock()
	transportErr := errors.Join(transportErrs...)
	stateLock.Unlock()
	if transportErr != nil {
		return fmt.Errorf("fixture transport failed: %w", transportErr)
	}
	if view.wantError != "" {
		if verifyErr == nil || !strings.Contains(verifyErr.Error(), view.wantError) {
			return fmt.Errorf("replay error = %v, want rejection containing %q", verifyErr, view.wantError)
		}
		return nil
	}
	if verifyErr != nil {
		return verifyErr
	}
	if authenticated == nil || authenticated.candidate == nil || authenticated.candidate.bundle == nil || authenticated.candidate.bundle.Result == nil || authenticated.candidate.bundle.Result.RunID != runID {
		return errors.New("replicated committed scenario bundle lost the requested run")
	}
	return nil
}
