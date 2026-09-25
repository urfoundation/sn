// Real signed operator history reproduces an empty closed source following an
// older committed payout. No fixture grants the source a fabricated chain root.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/payoutartifact"
)

// Numbered block reads retain the same explicit RPC hash domain as production.
type policyRateEmptySourceTestReader struct {
	heads map[uint64]ChainHead
	fail  uint64
	calls []uint64
}

// Fail one chosen read without synthesizing a successful canonical marker.
func (self *policyRateEmptySourceTestReader) EVMBlockByNumber(ctx context.Context, number *big.Int) (ChainHead, error) {
	if err := ctx.Err(); err != nil {
		return ChainHead{}, err
	}
	n := number.Uint64()
	self.calls = append(self.calls, n)
	if n == self.fail {
		return ChainHead{}, errors.New("temporary source block read failure")
	}
	head, ok := self.heads[n]
	if !ok {
		return ChainHead{}, errors.New("unexpected source block read")
	}
	return head, nil
}

// The producer reads signed HTTP artifacts with exact identities, then the
// normal admission path independently verifies the retained signed source.
func newPolicyRateEmptySourceTest(t *testing.T, nonempty, foreignSigner bool) (*ResolvedConfig, *ScenarioObservation, *policyRateEmptySourceTestReader) {
	t.Helper()
	cfg := policyRateProvisionalTestConfig(t)
	cfg.Config.Topology.Miners, cfg.Config.Topology.Operators = 4, 2
	cfg.Config.Topology.HeadFleets, cfg.Config.Topology.ChallengerFleets, cfg.Config.Topology.ClientsPerHeadFleet = 2, 0, 1
	roles := &RoleSecrets{Schema: "urnetwork-sim-role-secrets-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, EVM: map[string]EVMRoleSecret{}}
	for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
		key, err := crypto.ToECDSA(bytes.Repeat([]byte{byte(60 + noId)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		label := fmt.Sprintf("operator-%d-artifact", noId)
		roles.EVM[label] = EVMRoleSecret{Label: label, Address: crypto.PubkeyToAddress(key.PublicKey).Hex()}
	}
	var err error
	cfg, err = configWithPolicyRateArtifactSigners(cfg, roles)
	if err != nil {
		t.Fatal(err)
	}
	clients := lifecyclePayoutTestClients(cfg)
	current := testScenarioObservation(cfg, 12)
	contracts := current.Status.Contracts
	contracts.CurrentEpochStart, contracts.CurrentEpochEnd = 1300, 1600
	contracts.Policy.EffectiveEpoch, contracts.Policy.EffectiveBlock, contracts.Policy.EpochBlocks = 11, 1000, 300
	contracts.FinalizedHead = ChainHead{Number: 1350, Hash: common.Hash{0x35}.Hex()}
	contracts.MinimumTransferRao = 100_000
	contracts.Epochs = []EpochView{{Epoch: 9}, {Epoch: 11}}
	current.Operators = nil
	reader := &policyRateEmptySourceTestReader{heads: map[uint64]ChainHead{
		1000: {Number: 1000, Hash: common.Hash{0x31}.Hex()},
		1300: {Number: 1300, Hash: common.Hash{0x32}.Hex()},
		1350: contracts.FinalizedHead,
	}}
	for noId := 1; noId <= 2; noId++ {
		key, err := crypto.ToECDSA(bytes.Repeat([]byte{byte(60 + noId)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		objects := map[string][]byte{}
		page := payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1"}
		for _, epoch := range []uint64{9, 11} {
			var providers []payoutartifact.ProviderInput
			if epoch == 9 || nonempty {
				for client, miner := range clients {
					if operatorForMiner(cfg, miner) != noId {
						continue
					}
					head := miner <= cfg.Config.Topology.fleetCandidateMiners()
					provider := payoutartifact.ProviderInput{ClientID: client, NetworkID: [16]byte{9, byte(noId)}, Coldkey: [32]byte{10, byte(miner)}, UsageBytes: 1, Assignments: 8, Confirmations: 8, Eligible: !head, HeadExcluded: head}
					if head {
						provider.ExclusionReason = "head_fleet_active"
					}
					providers = append(providers, provider)
				}
			}
			artifact, err := payoutartifact.Build(payoutartifact.BuildInput{
				DeploymentID: cfg.Config.Deployment.DeploymentID, GenesisHash: cfg.Public.Chain.GenesisHash, PolicyHash: cfg.PolicyHash,
				ChainID: cfg.ChainID, Netuid: cfg.Netuid, Coordinator: contracts.Deployment.CoordinatorProxy, SettlementVault: contracts.Deployment.SettlementVault,
				Epoch: epoch, NoID: uint64(noId), Start: payoutartifact.Boundary{Number: 1000, Hash: reader.heads[1000].Hash}, End: payoutartifact.Boundary{Number: 1300, Hash: reader.heads[1300].Hash},
				OperatorSnapshotHash: "sha256:" + strings.Repeat("33", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("34", 32), Providers: providers, ReliabilityAMin: cfg.Policy.Verify.ReliabilityAMin, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			})
			if err != nil {
				t.Fatal(err)
			}
			signingKey := key
			if foreignSigner && epoch == 11 {
				signingKey, err = crypto.ToECDSA(bytes.Repeat([]byte{0x44}, 32))
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := payoutartifact.Sign(artifact, signingKey); err != nil {
				t.Fatal(err)
			}
			raw, err := payoutartifact.Bytes(artifact)
			if err != nil {
				t.Fatal(err)
			}
			objects[artifact.ContentHash] = raw
			page.Objects = append(page.Objects, payoutArtifactHistoryObject{Key: fmt.Sprintf("st/v1/history/%s/%d/%d/%d/%s.json", cfg.Config.Deployment.DeploymentID, cfg.Netuid, epoch, noId, strings.TrimPrefix(artifact.ContentHash, "sha256:")), ContentHash: artifact.ContentHash, Size: int64(len(raw))})
			if epoch == 9 {
				contracts.Epochs[0].Operators = append(contracts.Epochs[0].Operators, EpochOperatorView{NoID: uint64(noId), ArtifactHash: "0x" + strings.TrimPrefix(artifact.ContentHash, "sha256:"), PayoutRoot: fleetLifecycleHex(artifact.PayoutRoot), CommitBlock: 1300, Status: 2})
			} else {
				contracts.Epochs[1].Operators = append(contracts.Epochs[1].Operators, EpochOperatorView{NoID: uint64(noId), ArtifactHash: common.Hash{}.Hex(), PayoutRoot: common.Hash{}.Hex(), Status: 1})
			}
		}
		sort.Slice(page.Objects, func(i, j int) bool { return page.Objects[i].Key < page.Objects[j].Key })
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sn/artifacts" {
				_ = json.NewEncoder(w).Encode(page)
				return
			}
			if r.URL.Path == "/sn/artifact" {
				if raw, ok := objects[r.URL.Query().Get("hash")]; ok {
					_, _ = w.Write(raw)
					return
				}
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(server.Close)
		probe := &liveScenarioProbe{cfg: cfg, stateDir: t.TempDir(), client: server.Client()}
		surfaces := scenarioOperatorSurfaces{}
		surfaces[scenarioOperatorStatus] = scenarioOperatorRead{status: http.StatusOK}
		surfaces[scenarioOperatorKeys] = scenarioOperatorRead{data: []byte(`{"keys":[{"server_key_id":1,"public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]}`)}
		surfaces[scenarioOperatorStats] = scenarioOperatorRead{data: []byte(`{"schema":"urnetwork-verify-stats-index-v1","rows":[]}`)}
		surfaces[scenarioOperatorProofs] = scenarioOperatorRead{data: []byte(`{"schema":"urnetwork-verify-proof-index-v1","rows":[]}`)}
		current.Operators = append(current.Operators, probe.inspectOperatorWithSurfaces(t.Context(), contracts, noId, crypto.PubkeyToAddress(key.PublicKey).Hex(), server.URL, clients, surfaces))
	}
	proof := &PolicyRateReadinessObservation{Head: contracts.FinalizedHead, AlphaPriceWei: "500000000000000", MinimumTransferTaoRao: contracts.MinimumTransferRao}
	for _, operator := range current.Operators {
		if operator.RateSource != nil {
			proof.Sources = append(proof.Sources, *operator.RateSource)
		}
	}
	current.PolicyRateReadiness = proof
	return cfg, current, reader
}

// The two real admission gates start a future interval without changing the
// signed empty epoch or claiming native margin, payout or final acceptance.
func TestPolicyRateEmptySourceStartsAfterOlderCommittedPayout(t *testing.T) {
	cfg, observation, reader := newPolicyRateEmptySourceTest(t, false, false)
	for _, operator := range observation.Operators {
		if operator.Error != "" || operator.LatestArtifactEpoch != 9 || operator.RateSource.Epoch != 11 || operator.EmptyRateSource == nil || operator.MatchingArtifacts != 1 {
			t.Fatalf("producer did not retain distinct source and payout: %+v", operator)
		}
	}
	contracts, proof := observation.Status.Contracts, observation.PolicyRateReadiness
	completePolicyRateReadiness(cfg, contracts, observation.Operators, proof, big.NewInt(500_000_000_000_000))
	if proof.ProvisionalLowUsage != nil {
		t.Fatal("unread canonical source acquired a deferral")
	}
	if err := authenticateEmptyPolicyRateSources(t.Context(), cfg, contracts, observation.Operators, reader); err != nil {
		t.Fatal(err)
	}
	if len(reader.calls) != 3 {
		t.Fatal("shared source boundaries were repeatedly read")
	}
	completePolicyRateReadiness(cfg, contracts, observation.Operators, proof, big.NewInt(500_000_000_000_000))
	if proof.Ready || proof.ProvisionalLowUsage == nil || proof.ProvisionalLowUsage.Shortfalls[0].EquivalentTaoRao != "0" {
		t.Fatal("empty source failed to retain its honest zero margin")
	}
	before, _ := json.Marshal(observation)
	if err := validateScenarioPolicyRateAdmission(cfg, observation); err != nil {
		t.Fatal(err)
	}
	probe := &scenarioIntervalProbe{}
	if current, err := waitScenarioPolicyRateReadiness(t.Context(), cfg, "release-1.0", observation, probe, time.Second, func(*ScenarioObservation) error { return errors.New("retained proof rewritten") }); err != nil || current != observation || probe.calls.Load() != 0 {
		t.Fatal("empty source remained blocked", err)
	}
	definition := scenarioDefinition{Name: "release-1.0", GoalEpochs: uint64(cfg.Config.Scenarios.ShortEpochs)}
	window, err := buildScenarioAcceptanceWindow(cfg, definition, observation)
	if err != nil || window.FirstEpoch != 13 || window.StartBlock != contracts.CurrentEpochEnd {
		t.Fatal("empty source credited historical work", err)
	}
	after, _ := json.Marshal(observation)
	if !bytes.Equal(before, after) {
		t.Fatal("admission rewrote the source")
	}
	strict := *cfg
	strict.provisionalResume = nil
	if validateScenarioPolicyRateAdmission(&strict, observation) == nil {
		t.Fatal("empty source passed strict readiness")
	}
}

// A nonempty pending payout and a foreign signed empty artifact receive no new
// authority, even when an older payout already matches the chain.
func TestPolicyRateEmptySourceRefusesNonemptyAndForeignSigner(t *testing.T) {
	for _, c := range []struct{ nonempty, foreign bool }{{nonempty: true}, {foreign: true}} {
		cfg, observation, reader := newPolicyRateEmptySourceTest(t, c.nonempty, c.foreign)
		if err := authenticateEmptyPolicyRateSources(t.Context(), cfg, observation.Status.Contracts, observation.Operators, reader); err != nil {
			t.Fatal(err)
		}
		for _, operator := range observation.Operators {
			if operator.EmptyRateSource != nil {
				t.Fatalf("unapproved source acquired empty proof: %+v", c)
			}
		}
		completePolicyRateReadiness(cfg, observation.Status.Contracts, observation.Operators, observation.PolicyRateReadiness, big.NewInt(500_000_000_000_000))
		if observation.PolicyRateReadiness.ProvisionalLowUsage != nil || validateScenarioPolicyRateAdmission(cfg, observation) == nil {
			t.Fatalf("unapproved source entered provisional interval: %+v", c)
		}
	}
}

// Replayed serialized proofs cannot change source identity, signer, signature,
// empty structure, exact chain boundaries or the pinned head.
func TestPolicyRateEmptySourceRejectsTamperedEvidence(t *testing.T) {
	cfg, observation, reader := newPolicyRateEmptySourceTest(t, false, false)
	if err := authenticateEmptyPolicyRateSources(t.Context(), cfg, observation.Status.Contracts, observation.Operators, reader); err != nil {
		t.Fatal(err)
	}
	completePolicyRateReadiness(cfg, observation.Status.Contracts, observation.Operators, observation.PolicyRateReadiness, big.NewInt(500_000_000_000_000))
	raw, _ := json.Marshal(observation)
	for _, fault := range []string{"source hash", "signer", "foreign signed source", "signature", "boundary", "head", "operator", "policy", "usage", "commit", "missing"} {
		var changed ScenarioObservation
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		evidence := changed.Operators[1].EmptyRateSource
		switch fault {
		case "source hash":
			changed.PolicyRateReadiness.Sources[1].ContentHash = "sha256:" + strings.Repeat("55", 32)
		case "signer":
			evidence.ExpectedSigner = common.Address{0x77}.Hex()
		case "foreign signed source":
			foreignKey, err := crypto.ToECDSA(bytes.Repeat([]byte{0x44}, 32))
			if err != nil {
				t.Fatal(err)
			}
			originalHash := evidence.Artifact.ContentHash
			if err := payoutartifact.Sign(&evidence.Artifact, foreignKey); err != nil {
				t.Fatal(err)
			}
			if err := verifyPayoutArtifact(&evidence.Artifact); err != nil {
				t.Fatal("foreign substitution must remain a valid signed empty artifact", err)
			}
			evidence.ExpectedSigner = evidence.Artifact.Signer.Hex()
			changed.Operators[1].RateSource.ContentHash = evidence.Artifact.ContentHash
			changed.PolicyRateReadiness.Sources[1].ContentHash = evidence.Artifact.ContentHash
			for i, hash := range changed.Operators[1].ArtifactHashes {
				if hash == originalHash {
					changed.Operators[1].ArtifactHashes[i] = evidence.Artifact.ContentHash
				}
			}
		case "signature":
			evidence.Artifact.Signature = "0x" + strings.Repeat("11", 65)
		case "boundary":
			evidence.Artifact.End.Number++
		case "head":
			evidence.CanonicalHead.Number++
		case "operator":
			evidence.Artifact.NoID = 1
		case "policy":
			evidence.Artifact.PolicyHash = common.Hash{0x77}.Hex()
		case "usage":
			evidence.Artifact.TotalUsageBytes = 1
		case "commit":
			changed.Status.Contracts.Epochs[1].Operators[1].ArtifactHash = common.Hash{0x55}.Hex()
		case "missing":
			changed.Operators[1].EmptyRateSource = nil
		}
		if validateScenarioPolicyRateAdmission(cfg, &changed) == nil {
			t.Fatalf("%s changed the source authority", fault)
		}
	}
}

// The retained role store supplies detached invocation authority. Replaying an
// otherwise valid observation without that authority cannot grant a deferral.
func TestPolicyRateEmptySourceRetainsIndependentRoleAuthority(t *testing.T) {
	cfg, observation, reader := newPolicyRateEmptySourceTest(t, false, false)
	if err := authenticateEmptyPolicyRateSources(t.Context(), cfg, observation.Status.Contracts, observation.Operators, reader); err != nil {
		t.Fatal(err)
	}
	completePolicyRateReadiness(cfg, observation.Status.Contracts, observation.Operators, observation.PolicyRateReadiness, big.NewInt(500_000_000_000_000))
	unbound := *cfg
	unbound.policyRateArtifactSignerAddresses = nil
	if validateScenarioPolicyRateAdmission(&unbound, observation) == nil {
		t.Fatal("observation supplied its own signer authority")
	}
	roles := &RoleSecrets{Schema: "urnetwork-sim-role-secrets-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, EVM: map[string]EVMRoleSecret{}}
	for noId, address := range cfg.policyRateArtifactSignerAddresses {
		label := fmt.Sprintf("operator-%d-artifact", noId)
		roles.EVM[label] = EVMRoleSecret{Label: label, Address: address.Hex()}
	}
	bound, err := configWithPolicyRateArtifactSigners(&unbound, roles)
	if err != nil {
		t.Fatal(err)
	}
	delete(roles.EVM, "operator-1-artifact")
	if len(unbound.policyRateArtifactSignerAddresses) != 0 || validateScenarioPolicyRateAdmission(bound, observation) != nil {
		t.Fatal("role binding mutated the caller or retained a mutable role-store alias")
	}
	if _, err := configWithPolicyRateArtifactSigners(&unbound, roles); err == nil {
		t.Fatal("incomplete retained roles supplied source authority")
	}
	roles.EVM["operator-1-artifact"] = EVMRoleSecret{Label: "operator-1-artifact", Address: cfg.policyRateArtifactSignerAddresses[1].Hex()}
	roles.DeploymentID = "foreign-deployment.example"
	if _, err := configWithPolicyRateArtifactSigners(&unbound, roles); err == nil {
		t.Fatal("foreign retained roles supplied source authority")
	}
}

// Read failures and canonical drift leave no trusted marker. A subsequent
// complete read can recover without changing any signed source bytes.
func TestPolicyRateEmptySourceCanonicalReadFailureAndDrift(t *testing.T) {
	for _, fault := range []string{"read", "boundary", "head"} {
		cfg, observation, reader := newPolicyRateEmptySourceTest(t, false, false)
		if err := authenticateEmptyPolicyRateSources(t.Context(), cfg, observation.Status.Contracts, observation.Operators, reader); err != nil {
			t.Fatal(err)
		}
		original := reader.heads[1300]
		switch fault {
		case "read":
			reader.fail = 1300
		case "boundary":
			reader.heads[1300] = ChainHead{Number: 1300, Hash: common.Hash{0x78}.Hex()}
		case "head":
			reader.heads[1350] = ChainHead{Number: 1350, Hash: common.Hash{0x79}.Hex()}
		}
		if authenticateEmptyPolicyRateSources(t.Context(), cfg, observation.Status.Contracts, observation.Operators, reader) == nil {
			t.Fatalf("%s was admitted", fault)
		}
		for _, operator := range observation.Operators {
			if operator.EmptyRateSource.CanonicalHead != (ChainHead{}) {
				t.Fatalf("%s retained a partial authentication", fault)
			}
		}
		reader.fail, reader.heads[1300], reader.heads[1350] = 0, original, observation.Status.Contracts.FinalizedHead
		if err := authenticateEmptyPolicyRateSources(t.Context(), cfg, observation.Status.Contracts, observation.Operators, reader); err != nil {
			t.Fatal(err)
		}
	}
}
