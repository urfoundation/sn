//go:build linux || darwin

// A missing lifecycle index must not conceal independently signed ordinary
// payouts, and a diagnostic projection must never become a strict waiver.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/payoutartifact"
)

// Actual signed artifact fixtures and the strict collector own validation;
// only the transport is replaced by a deterministic in-memory response.
type terminalDiagnosticPayoutTestFixture struct {
	cfg        *ResolvedConfig
	terminal   *ScenarioObservation
	identities *finalPublicIdentities
	window     *ScenarioAcceptanceWindow
	bodies     map[string][]byte
	client     *http.Client
	reads      int
}

func newTerminalDiagnosticPayoutTestFixture(t *testing.T) *terminalDiagnosticPayoutTestFixture {
	t.Helper()
	fixture, first := finalPayoutArtifactTestFixture(t)
	prior, err := payoutartifact.Decode(first)
	if err != nil {
		t.Fatal(err)
	}
	rootKey, err := crypto.ToECDSA(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	rootSigner := crypto.PubkeyToAddress(rootKey.PublicKey)
	result := &terminalDiagnosticPayoutTestFixture{cfg: testResolvedConfig(t), bodies: map[string][]byte{}, window: &ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 1}}
	var hashes []string
	for _, epoch := range []uint64{9, 10} {
		fixture.expected.Epoch = epoch
		artifact, raw := finalPayoutArtifactTestBuild(t, fixture, 1, fixture.providers, prior.Start, prior.End, fixture.reliabilityMin)
		result.bodies[artifact.ContentHash] = raw
		hashes = append(hashes, artifact.ContentHash)
	}
	result.cfg.Config.Deployment.DeploymentID = fixture.evidence.DeploymentID
	result.cfg.Config.Topology.Operators = 1
	result.cfg.ChainID, result.cfg.Netuid, result.cfg.PolicyHash = fixture.evidence.ChainID, fixture.evidence.Netuid, fixture.evidence.PolicyHash
	result.cfg.Public.Chain.GenesisHash = fixture.evidence.GenesisHash
	result.identities = &finalPublicIdentities{Schema: "urnetwork-sim-public-identities-v1", DeploymentID: fixture.evidence.DeploymentID, EVM: map[string]string{"operator-1-artifact": fixture.expected.ArtifactSigner.Hex(), "operator-1-root": rootSigner.Hex()}}
	result.terminal = &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{{NoID: 1, RootSigner: rootSigner.Hex()}}}}, Operators: []OperatorObservation{{NoID: 1, APIURL: "http://127.0.0.1:18081", ArtifactHashes: hashes}}, FleetLifecycle: &FleetLifecycleEvidence{Stage: "release-handoff", ProvisionalBypass: &FleetLifecycleProvisionalBypass{}}}
	result.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		result.reads++
		if request.Method != http.MethodGet || request.URL.Host != "127.0.0.1:18081" || request.URL.Path != "/sn/artifact" {
			t.Fatalf("unexpected diagnostic request %s %s", request.Method, request.URL)
		}
		raw, ok := result.bodies[request.URL.Query().Get("hash")]
		if !ok {
			t.Fatal("diagnostic selected unknown content hash")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw)), Request: request}, nil
	})}
	return result
}

// Pass every requested scope to the unchanged production verifier.
func (self *terminalDiagnosticPayoutTestFixture) read(ctx context.Context, cfg *ResolvedConfig, output string, terminal *ScenarioObservation, window *ScenarioAcceptanceWindow, identities *finalPublicIdentities) ([]FinalCollectedPayoutArtifact, []FinalCollectedPayoutArtifact, error) {
	return collectFinalPayoutArtifactsWithClient(ctx, cfg, output, terminal, window, identities, self.client)
}

func TestTerminalDiagnosticPayoutsPreserveOrdinaryEvidenceWithMissingLifecycle(t *testing.T) {
	fixture := newTerminalDiagnosticPayoutTestFixture(t)
	before, err := json.Marshal(fixture.terminal)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.read(t.Context(), fixture.cfg, t.TempDir(), fixture.terminal, fixture.window, fixture.identities); err == nil || !strings.Contains(err.Error(), "no payout artifact index") || fixture.reads != 0 {
		t.Fatalf("strict prerequisite changed: %v", err)
	}
	collector := &terminalDiagnosticCollector{ctx: t.Context(), output: t.TempDir(), report: &terminalDiagnosticReport{ReadOnly: true, Window: fixture.window}}
	collectTerminalDiagnosticPayouts(collector, fixture.cfg, fixture.terminal, fixture.identities, "", fixture.read)
	collector.check("independent-original-failure", "", time.Minute, func(context.Context) (any, error) { return nil, errors.New("original failed qualification") })
	checks := collector.report.Checks
	if len(checks) != 3 || checks[0].Id != "signed-payout-artifacts" || checks[0].Status != "pass" || checks[1].Id != "lifecycle-payout-artifacts" || checks[1].Status != "unavailable" || checks[2].Status != "fail" || fixture.reads != 2 || collector.report.FinalAcceptance {
		t.Fatalf("diagnostic coupled or waived scopes: %+v, reads=%d", checks, fixture.reads)
	}
	evidence := checks[0].Evidence.(map[string]any)
	payouts := evidence["acceptance"].([]FinalCollectedPayoutArtifact)
	if len(payouts) != 2 || evidence["scope"] != "signed-acceptance-window" || evidence["final_acceptance"] != false {
		t.Fatal("ordinary evidence was mislabeled as lifecycle or final acceptance")
	}
	for index, payout := range payouts {
		raw, err := os.ReadFile(filepath.Join(collector.output, payout.Artifact.URI))
		if err != nil || payout.Epoch != uint64(index+9) || !bytes.Equal(raw, fixture.bodies[fixture.terminal.Operators[0].ArtifactHashes[index]]) {
			t.Fatalf("signed artifact bytes or epoch changed: %v", err)
		}
	}
	after, err := json.Marshal(fixture.terminal)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("diagnostic erased original lifecycle exception", err)
	}
	if _, _, err := fixture.read(t.Context(), fixture.cfg, t.TempDir(), fixture.terminal, fixture.window, fixture.identities); err == nil || !strings.Contains(err.Error(), "no payout artifact index") {
		t.Fatalf("diagnostic projection escaped into strict collection: %v", err)
	}
}

func TestTerminalDiagnosticPayoutsKeepMalformedLifecycleFailed(t *testing.T) {
	fixture := newTerminalDiagnosticPayoutTestFixture(t)
	fixture.terminal.FleetLifecycle.Payouts = []FleetLifecyclePayoutEvidence{{NoID: 1, Epoch: 10}}
	collector := &terminalDiagnosticCollector{ctx: t.Context(), output: t.TempDir(), report: &terminalDiagnosticReport{ReadOnly: true, Window: fixture.window}}
	collectTerminalDiagnosticPayouts(collector, fixture.cfg, fixture.terminal, fixture.identities, "", fixture.read)
	if len(collector.report.Checks) != 2 || collector.report.Checks[0].Status != "pass" || collector.report.Checks[1].Status != "fail" || !strings.Contains(collector.report.Checks[1].Detail, "payout index is incomplete") || fixture.reads != 2 || collector.report.FinalAcceptance {
		t.Fatalf("malformed lifecycle was excused or hid ordinary payouts: %+v", collector.report.Checks)
	}
}

func TestTerminalDiagnosticPayoutsRejectChangedSignedArtifact(t *testing.T) {
	fixture := newTerminalDiagnosticPayoutTestFixture(t)
	fixture.bodies[fixture.terminal.Operators[0].ArtifactHashes[0]] = []byte("{}\n")
	collector := &terminalDiagnosticCollector{ctx: t.Context(), output: t.TempDir(), report: &terminalDiagnosticReport{ReadOnly: true, Window: fixture.window}}
	collectTerminalDiagnosticPayouts(collector, fixture.cfg, fixture.terminal, fixture.identities, "", fixture.read)
	if len(collector.report.Checks) != 2 || collector.report.Checks[0].Status != "fail" || collector.report.Checks[1].Status != "unavailable" || fixture.reads != 1 || collector.report.FinalAcceptance {
		t.Fatalf("ordinary signed artifact validation was waived: %+v", collector.report.Checks)
	}
}

func TestTerminalDiagnosticPayoutsCannotAuthorizeAcceptingCapture(t *testing.T) {
	fixture := newTerminalDiagnosticPayoutTestFixture(t)
	collector := &terminalDiagnosticCollector{ctx: t.Context(), output: t.TempDir(), report: &terminalDiagnosticReport{ReadOnly: true, FinalAcceptance: true, Window: fixture.window}}
	collectTerminalDiagnosticPayouts(collector, fixture.cfg, fixture.terminal, fixture.identities, "", fixture.read)
	if len(collector.report.Checks) != 2 || collector.report.Checks[0].Status != "unavailable" || collector.report.Checks[1].Status != "unavailable" || fixture.reads != 0 {
		t.Fatalf("accepting owner borrowed diagnostic projection: %+v", collector.report.Checks)
	}
}
