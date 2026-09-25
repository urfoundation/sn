// Signed synthetic artifacts exercise the real producer and release predicate,
// including a newer artifact published while terminal work is still running.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/payoutartifact"
)

// Both operators publish head exclusion plus actual pool-tail leaves for two
// accepted epochs and one later epoch. No source is admitted by test-only trust.
func scenarioPayoutWindowTestEvaluation(t *testing.T, badEpoch uint64) *scenarioEvaluation {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Miners, cfg.Config.Topology.Operators = 4, 2
	cfg.Config.Topology.HeadFleets, cfg.Config.Topology.ChallengerFleets, cfg.Config.Topology.ClientsPerHeadFleet = 2, 0, 1
	clients := lifecyclePayoutTestClients(cfg)
	current := testScenarioObservation(cfg, 103)
	contracts := current.Status.Contracts
	contracts.Epochs = nil
	current.Operators = nil
	keys := make(map[int]*ecdsa.PrivateKey)
	archives := make(map[int]map[string][]byte)
	pages := make(map[int]*payoutArtifactHistoryPage)
	for noId := 1; noId <= 2; noId++ {
		key, err := crypto.ToECDSA(bytes.Repeat([]byte{byte(40 + noId)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		keys[noId], archives[noId] = key, map[string][]byte{}
		pages[noId] = &payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1"}
	}
	for _, epoch := range []uint64{100, 101, 102} {
		view := EpochView{Epoch: epoch}
		for noId := 1; noId <= 2; noId++ {
			var providers []payoutartifact.ProviderInput
			for client, miner := range clients {
				if operatorForMiner(cfg, miner) != noId {
					continue
				}
				head := miner <= cfg.Config.Topology.fleetCandidateMiners() && epoch != badEpoch
				provider := payoutartifact.ProviderInput{ClientID: client, NetworkID: [16]byte{9, byte(noId)}, Coldkey: [32]byte{10, byte(miner)}, UsageBytes: 100, Assignments: 8, Confirmations: 8, Eligible: !head, HeadExcluded: head}
				if head {
					provider.ExclusionReason = "head_fleet_active"
				}
				providers = append(providers, provider)
			}
			artifact, err := payoutartifact.Build(payoutartifact.BuildInput{
				DeploymentID: cfg.Config.Deployment.DeploymentID, GenesisHash: cfg.Public.Chain.GenesisHash, PolicyHash: cfg.PolicyHash,
				ChainID: cfg.ChainID, Netuid: cfg.Netuid, Coordinator: contracts.Deployment.CoordinatorProxy, SettlementVault: contracts.Deployment.SettlementVault,
				Epoch: epoch, NoID: uint64(noId), Start: payoutartifact.Boundary{Number: epoch * 10, Hash: "0x" + strings.Repeat("31", 32)}, End: payoutartifact.Boundary{Number: epoch*10 + 9, Hash: "0x" + strings.Repeat("32", 32)},
				OperatorSnapshotHash: "sha256:" + strings.Repeat("33", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("34", 32), Providers: providers, ReliabilityAMin: 8, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := payoutartifact.Sign(artifact, keys[noId]); err != nil {
				t.Fatal(err)
			}
			raw, err := payoutartifact.Bytes(artifact)
			if err != nil {
				t.Fatal(err)
			}
			archives[noId][artifact.ContentHash] = raw
			pages[noId].Objects = append(pages[noId].Objects, payoutArtifactHistoryObject{Key: fmt.Sprintf("st/v1/history/%s/%d/%d/%d/%s.json", cfg.Config.Deployment.DeploymentID, cfg.Netuid, epoch, noId, strings.TrimPrefix(artifact.ContentHash, "sha256:")), ContentHash: artifact.ContentHash, Size: int64(len(raw))})
			view.Operators = append(view.Operators, EpochOperatorView{NoID: uint64(noId), ArtifactHash: "0x" + strings.TrimPrefix(artifact.ContentHash, "sha256:"), PayoutRoot: fleetLifecycleHex(artifact.PayoutRoot), CommitBlock: epoch*10 + 9, Status: 2})
		}
		contracts.Epochs = append(contracts.Epochs, view)
	}
	for noId := 1; noId <= 2; noId++ {
		objects, page := archives[noId], pages[noId]
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
		operator := probe.inspectOperatorWithSurfaces(t.Context(), contracts, noId, crypto.PubkeyToAddress(keys[noId].PublicKey).Hex(), server.URL, clients, surfaces)
		if operator.MatchingArtifacts != 3 || len(operator.PayoutTierArtifacts) != 3 {
			t.Fatalf("signed source producer lost exact epochs: matches=%d rows=%+v error=%s", operator.MatchingArtifacts, operator.PayoutTierArtifacts, operator.Error)
		}
		current.Operators = append(current.Operators, operator)
	}
	return &scenarioEvaluation{Cfg: cfg, Current: current, Window: &ScenarioAcceptanceWindow{FirstEpoch: 100, EpochCount: 2}, Definition: scenarioDefinition{Name: "release-1.0"}}
}

// Uses the actual release gate so deleting its scope projection is causal.
func scenarioPayoutWindowTestCheck(t *testing.T, e *scenarioEvaluation) (bool, string) {
	t.Helper()
	for _, check := range releaseScenarioChecks() {
		if check.ID == "payout_artifacts_enforce_one_tier" {
			return check.Check(e)
		}
	}
	t.Fatal("missing production payout check")
	return false, ""
}

func TestScenarioPayoutWindowRetainsSignedEpochsAndIgnoresLaterCohort(t *testing.T) {
	e := scenarioPayoutWindowTestEvaluation(t, 102)
	before, _ := json.Marshal(e.Current)
	if e.Current.Operators[0].TierMembershipValid {
		t.Fatal("fixture latest out-of-window artifact must differ")
	}
	if ok, reason := scenarioPayoutWindowTestCheck(t, e); !ok {
		t.Fatalf("valid signed window was judged by later artifact: %s", reason)
	}
	for _, operator := range e.Current.Operators {
		for index, row := range operator.PayoutTierArtifacts {
			if row.Epoch != 100+uint64(index) || row.NoId != operator.NoID || row.ContentHash == "" || row.PayoutRoot == "" {
				t.Fatal("producer lost sorted source identity")
			}
		}
	}
	after, _ := json.Marshal(e.Current)
	if !bytes.Equal(before, after) {
		t.Fatal("acceptance scope rewrote original observation")
	}
}

func TestScenarioPayoutWindowRejectsBadEarlierEpochDespiteLatestSuccess(t *testing.T) {
	e := scenarioPayoutWindowTestEvaluation(t, 100)
	if !e.Current.Operators[0].TierMembershipValid {
		t.Fatal("fixture latest must be valid")
	}
	if ok, reason := scenarioPayoutWindowTestCheck(t, e); ok || !strings.Contains(reason, "epoch=100") {
		t.Fatalf("newer success hid an in-window bad payout: %v %s", ok, reason)
	}
}

func TestScenarioPayoutWindowRejectsMissingOrChangedAuthority(t *testing.T) {
	base := scenarioPayoutWindowTestEvaluation(t, 0)
	raw, _ := json.Marshal(base.Current)
	for _, test := range []struct {
		name string
		edit func(*scenarioEvaluation)
	}{
		{name: "missing epoch", edit: func(e *scenarioEvaluation) {
			e.Current.Operators[0].PayoutTierArtifacts = e.Current.Operators[0].PayoutTierArtifacts[1:]
		}},
		{name: "duplicate epoch", edit: func(e *scenarioEvaluation) {
			o := &e.Current.Operators[0]
			o.PayoutTierArtifacts = append(o.PayoutTierArtifacts, o.PayoutTierArtifacts[0])
		}},
		{name: "foreign operator", edit: func(e *scenarioEvaluation) { e.Current.Operators[0].PayoutTierArtifacts[0].NoId = 2 }},
		{name: "swapped artifact hash", edit: func(e *scenarioEvaluation) {
			e.Current.Operators[0].PayoutTierArtifacts[0].ContentHash = "sha256:" + strings.Repeat("78", 32)
		}},
		{name: "swapped root", edit: func(e *scenarioEvaluation) {
			e.Current.Operators[0].PayoutTierArtifacts[0].PayoutRoot = "0x" + strings.Repeat("79", 32)
		}},
		{name: "unfinalized row", edit: func(e *scenarioEvaluation) { e.Current.Status.Contracts.Epochs[0].Operators[0].Status = 1 }},
		{name: "future commit", edit: func(e *scenarioEvaluation) {
			e.Current.Status.Contracts.Epochs[0].Operators[0].CommitBlock = e.Current.Status.Contracts.FinalizedHead.Number + 1
		}},
		{name: "duplicate contract row", edit: func(e *scenarioEvaluation) {
			rows := e.Current.Status.Contracts.Epochs
			e.Current.Status.Contracts.Epochs = append(rows, rows[0])
		}},
		{name: "missing signed window", edit: func(e *scenarioEvaluation) { e.Window = nil }},
		{name: "overflow", edit: func(e *scenarioEvaluation) {
			e.Window = &ScenarioAcceptanceWindow{FirstEpoch: ^uint64(0), EpochCount: 2}
		}},
		{name: "missing operator", edit: func(e *scenarioEvaluation) { e.Current.Operators = e.Current.Operators[:1] }},
		{name: "duplicate operator", edit: func(e *scenarioEvaluation) { e.Current.Operators[1] = e.Current.Operators[0] }},
		{name: "unavailable legacy summary", edit: func(e *scenarioEvaluation) { e.Current.Operators[0].PayoutTierArtifacts = nil }},
		{name: "changed count despite flag", edit: func(e *scenarioEvaluation) { e.Current.Operators[0].PayoutTierArtifacts[0].CandidateLeaves = 1 }},
	} {
		copy := *base
		copy.Window = &ScenarioAcceptanceWindow{FirstEpoch: 100, EpochCount: 2}
		copy.Current = &ScenarioObservation{}
		if err := json.Unmarshal(raw, copy.Current); err != nil {
			t.Fatal(err)
		}
		test.edit(&copy)
		if ok, reason := scenarioPayoutWindowTestCheck(t, &copy); ok {
			t.Fatalf("%s authority accepted: %s", test.name, reason)
		}
	}
}

func TestScenarioPayoutWindowKeepsNonWindowLatestAndUnorderedScopedRows(t *testing.T) {
	e := scenarioPayoutWindowTestEvaluation(t, 102)
	for i := range e.Current.Operators {
		rows := e.Current.Operators[i].PayoutTierArtifacts
		rows[0], rows[2] = rows[2], rows[0]
	}
	if ok, reason := scenarioPayoutWindowTestCheck(t, e); !ok {
		t.Fatal(reason)
	}
	e.Window = nil
	e.Definition.Name = "smoke"
	if ok, _ := scenarioPayoutWindowTestCheck(t, e); ok {
		t.Fatal("non-window caller lost its current invalid cohort")
	}
}

// Old signed aggregate-only observations cannot manufacture last_discovered=0
// as a measured fact. This remains failed coverage, never historical success.
func TestScenarioPayoutWindowLegacyClaimFieldsAreUnavailableNotZero(t *testing.T) {
	e := scenarioClaimWindowTestEvaluation()
	for i := range e.Current.Claims {
		e.Current.Claims[i].LastDiscovered = 0
		e.Current.Claims[i].EpochOutcomes = nil
	}
	before, _ := json.Marshal(e.Current.Claims)
	_, err := scenarioClaimsForAcceptance(e)
	if err == nil || !strings.Contains(err.Error(), "scoped epoch evidence is unavailable") || strings.Contains(err.Error(), "last=0") {
		t.Fatalf("legacy fields became fabricated discovery evidence: %v", err)
	}
	after, _ := json.Marshal(e.Current.Claims)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("legacy evidence rewritten")
	}
}
