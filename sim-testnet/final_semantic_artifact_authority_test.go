package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

func TestFinalPayoutArtifactSeparatesProvenanceFromRootAuthorization(t *testing.T) {
	t.Parallel()
	fixture, data := finalPayoutArtifactTestFixture(t)
	rootKey, err := crypto.ToECDSA(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	fixture.pool.PayoutRootSigner = strings.ToLower(crypto.PubkeyToAddress(rootKey.PublicKey).Hex())
	if strings.EqualFold(fixture.expected.ArtifactSigner.Hex(), fixture.pool.PayoutRootSigner) {
		t.Fatal("fixture keys are not distinct")
	}
	if err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, fixture.expected, fixture.assignments, fixture.reliabilityMin, data); err != nil {
		t.Fatalf("configured artifact key with distinct root authority: %v", err)
	}
	artifact, err := payoutartifact.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, rootKey); err != nil {
		t.Fatal(err)
	}
	rootSigned, err := payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payoutartifact.Decode(rootSigned); err != nil {
		t.Fatalf("negative control is not validly signed: %v", err)
	}
	// Even a real registered root key and a matching new content commitment
	// cannot redefine the separately retained artifact provenance.
	expected := finalPayoutArtifactTestExpectation(artifact)
	expected.ArtifactSigner = fixture.expected.ArtifactSigner
	finalPayoutArtifactTestRequireRejection(t, "root key substituted for artifact key", "authenticated operator artifact identity", fixture, expected, fixture.assignments, rootSigned)
	expected.ArtifactSigner = common.Address{}
	finalPayoutArtifactTestRequireRejection(t, "missing independent signer", "authenticated operator artifact identity", fixture, expected, fixture.assignments, data)
}

func TestFinalPayoutArtifactConfiguredRoleCensusRejectsSubstitutions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"valid", "missing", "malformed", "zero", "reused", "deployment", "schema"} {
		t.Run(name, func(t *testing.T) {
			identities := &finalPublicIdentities{Schema: "urnetwork-sim-public-identities-v1", DeploymentID: "artifact-authority", EVM: map[string]string{
				"operator-1-artifact": common.Address{1}.Hex(), "operator-2-artifact": common.Address{2}.Hex(),
				"operator-1-root": common.Address{3}.Hex(), "operator-2-root": common.Address{4}.Hex(),
			}}
			switch name {
			case "missing":
				delete(identities.EVM, "operator-1-artifact")
			case "malformed":
				identities.EVM["operator-1-artifact"] = "not-an-address"
			case "zero":
				identities.EVM["operator-1-artifact"] = common.Address{}.Hex()
			case "reused":
				identities.EVM["operator-2-artifact"] = identities.EVM["operator-1-artifact"]
			case "deployment":
				identities.DeploymentID = "another-deployment"
			case "schema":
				identities.Schema = "unknown"
			}
			signers, err := finalOperatorArtifactSigners(identities, "artifact-authority", 2)
			if name == "valid" {
				if err != nil || len(signers) != 2 || signers[1] != (common.Address{1}) || signers[2] != (common.Address{2}) {
					t.Fatalf("exact retained artifact roles: %v", err)
				}
			} else if err == nil {
				t.Fatal("invalid artifact role source was admitted")
			}
		})
	}
}

func TestFinalPayoutArtifactSignedAuditProvenanceBindsAllDecisionSources(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"ordinary", "penalty", "recovery"} {
		t.Run(name, func(t *testing.T) {
			artifactSigner, rootSigner := common.Address{1}, common.Address{2}
			cycle := FinalCRv4Cycle{Pools: []FinalPoolWeightEvidence{{NoID: 1, ArtifactSigner: strings.ToLower(artifactSigner.Hex()), RootSigner: strings.ToLower(rootSigner.Hex()), RootCommitter: strings.ToLower(rootSigner.Hex())}}}
			evidence := &FinalSemanticEvidence{ExpectedOperators: 1}
			var target *FinalCRv4Cycle
			switch name {
			case "ordinary":
				evidence.Validators = []FinalValidatorIdentityEvidence{{Cycles: []FinalCRv4Cycle{cycle}}}
				target = &evidence.Validators[0].Cycles[0]
			case "penalty":
				evidence.DishonestDeposit = &FinalDishonestDepositEvidence{Penalties: []FinalDishonestDepositDecision{{Cycle: cycle}}}
				target = &evidence.DishonestDeposit.Penalties[0].Cycle
			case "recovery":
				evidence.DishonestDeposit = &FinalDishonestDepositEvidence{Recoveries: []FinalDishonestDepositDecision{{Cycle: cycle}}}
				target = &evidence.DishonestDeposit.Recoveries[0].Cycle
			}
			signers := map[uint64]common.Address{1: artifactSigner}
			if err := verifyFinalArtifactSignerProvenance(evidence, signers); err != nil {
				t.Fatal(err)
			}
			target.Pools[0].ArtifactSigner = target.Pools[0].RootSigner
			if err := verifyFinalArtifactSignerProvenance(evidence, signers); err == nil {
				t.Fatal("audit changed its configured artifact signer to root authority")
			}
		})
	}
}

func TestFinalPayoutArtifactCollectorUsesRetainedSigner(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"configured", "root-key", "other-operator"} {
		t.Run(name, func(t *testing.T) {
			fixture, first := finalPayoutArtifactTestFixture(t)
			originalSigner := fixture.expected.ArtifactSigner
			rootKey, err := crypto.ToECDSA(bytes.Repeat([]byte{8}, 32))
			if err != nil {
				t.Fatal(err)
			}
			rootSigner := crypto.PubkeyToAddress(rootKey.PublicKey)
			prior, err := payoutartifact.Decode(first)
			if err != nil {
				t.Fatal(err)
			}
			if name == "root-key" {
				fixture.key = rootKey
			}
			if name == "other-operator" {
				fixture.key, err = crypto.ToECDSA(bytes.Repeat([]byte{9}, 32))
				if err != nil {
					t.Fatal(err)
				}
			}
			bodies := map[string][]byte{}
			hashes := []string{}
			for _, epoch := range []uint64{9, 10} {
				fixture.expected.Epoch = epoch
				artifact, raw := finalPayoutArtifactTestBuild(t, fixture, 1, fixture.providers, prior.Start, prior.End, fixture.reliabilityMin)
				bodies[artifact.ContentHash] = raw
				hashes = append(hashes, artifact.ContentHash)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Host != "127.0.0.1:18081" || r.URL.Path != "/sn/artifact" {
					http.Error(w, "wrong request", http.StatusBadRequest)
					return
				}
				raw, ok := bodies[r.URL.Query().Get("hash")]
				if !ok {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write(raw)
			}))
			defer server.Close()
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			cfg := testResolvedConfig(t)
			cfg.Config.Deployment.DeploymentID = fixture.evidence.DeploymentID
			cfg.Config.Topology.Operators = 1
			cfg.ChainID, cfg.Netuid, cfg.PolicyHash = fixture.evidence.ChainID, fixture.evidence.Netuid, fixture.evidence.PolicyHash
			cfg.Public.Chain.GenesisHash = fixture.evidence.GenesisHash
			identities := &finalPublicIdentities{Schema: "urnetwork-sim-public-identities-v1", DeploymentID: fixture.evidence.DeploymentID, EVM: map[string]string{"operator-1-artifact": originalSigner.Hex(), "operator-1-root": rootSigner.Hex()}}
			terminal := &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{{NoID: 1, RootSigner: rootSigner.Hex()}}}}, Operators: []OperatorObservation{{NoID: 1, APIURL: "http://127.0.0.1:18081", ArtifactHashes: hashes}}}
			runRoot := t.TempDir()
			collected, lifecycle, err := collectFinalPayoutArtifactsWithClient(t.Context(), cfg, runRoot, terminal, &ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 1}, identities, &http.Client{Transport: transport})
			if name != "configured" {
				if err == nil || !strings.Contains(err.Error(), "payout artifact is missing") {
					t.Fatalf("foreign artifact signer was collected: %v", err)
				}
				if entries, readErr := os.ReadDir(runRoot); readErr != nil || len(entries) != 0 {
					t.Fatalf("foreign signature wrote artifact bytes: %v", readErr)
				}
				return
			}
			if err != nil || len(collected) != 2 || len(lifecycle) != 0 {
				t.Fatalf("distinct artifact/root collector: %d/%d %v", len(collected), len(lifecycle), err)
			}
			for index, item := range collected {
				if item.NoID != 1 || item.Epoch != uint64(index+9) {
					t.Fatal("collector changed the complete source/accepted epoch census")
				}
				raw, readErr := os.ReadFile(filepath.Join(runRoot, item.Artifact.URI))
				if readErr != nil || !bytes.Equal(raw, bodies[hashes[index]]) {
					t.Fatalf("collector changed canonical signed bytes: %v", readErr)
				}
			}
		})
	}
}
