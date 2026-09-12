package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	nativeTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

type fleetRenewalTestFixture struct {
	cfg      *ResolvedConfig
	stateDir string
	base     *SetupPlan
	roles    *RoleSecrets
	observed fleetRenewalObservation
	renewal  FleetRenewal
}

// Reproduces the retained 202-fleet state: expired generations 1/2 and two
// live generation-3 takeover fleets. No RPC or externally owned state is used.
func newFleetRenewalTestFixture(t *testing.T) fleetRenewalTestFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	public, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base, err := buildPlan(cfg, testSetupFacts(), public, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			label := fmt.Sprintf("miner-%d", miner)
			role := roles.Clients[label]
			role.ClientIDHex = fmt.Sprintf("%032x", miner)
			roles.Clients[label] = role
		}
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveContractDeployment(stateDir, base.Deployment); err != nil {
		t.Fatal(err)
	}
	observation := fleetRenewalObservation{Records: map[[16]byte]fleetBindingVersionRead{}, Native: map[[32]byte]ExistingUIDFact{}, Accounts: map[[32]byte]subtensorAccountInfo{}, Evidence: map[[16]byte][]FleetBindingEvidence{}}
	observation.Renewal = FleetRenewal{Round: 1, SourcePlanHash: base.PlanHash, JournalHash: common.Hash{0x77}.Hex(), NativeHead: ChainHead{Number: 100, Hash: common.Hash{0x11}.Hex()}, EVMHead: ChainHead{Number: 101, Hash: common.Hash{0x12}.Hex()}, ObservedEpoch: 316, ValidFromEpoch: 318, ValidToEpoch: 349, MaximumFeePerGasWei: 1_000_000_000, Oracle: common.HexToAddress(roles.EVM["commitment-oracle"].Address), Keeper: common.HexToAddress(roles.EVM["keeper"].Address), OracleNonce: 500, KeeperNonce: 700, CampaignLiabilityWei: "1000000000000000"}
	for _, action := range base.Actions {
		if action.ID == "campaign.evm-gas-reserve" {
			observation.Renewal.CampaignReserveBeforeWei = action.Spend.EVMGasWei
		}
	}
	observation.Renewal.BatchSize = fleetRenewalBatchSize
	observation.Renewal.MaximumInFlight = fleetRenewalMaximumInFlight
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		generation, from, to := uint64(2), uint64(30), uint64(61)
		label := fleetHotkeyLabel(fleet)
		if fleet > cfg.Config.Topology.HeadFleets {
			generation, from, to = 1, 40, 71
		}
		if fleet == fleetLifecycleTargetFleet {
			generation, from, to = 3, 294, 325
			label = churnHotkeyLabel(fleetLifecycleTargetChurn)
		}
		if fleet == fleetLifecycleCompanionFleet {
			generation, from, to = 3, 294, 325
			label = churnHotkeyLabel(fleetLifecycleCompanionChurn)
		}
		miners := []int{}
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miners = append(miners, fleetMemberMinerIndex(cfg, fleet, member))
		}
		manifest, _, _, err := fleetManifestForMembers(cfg, stateDir, roles, derive32(cfg, fmt.Sprintf("fleet-id/%d", fleet)), label, generation, miners)
		if err != nil {
			t.Fatal(err)
		}
		hotkey, err := crv4.KeypairFromSeedHex(roles.Substrate[label].SeedHex)
		if err != nil {
			t.Fatal(err)
		}
		coldkey, err := roleBytes32(roles, strings.TrimSuffix(label, "-hotkey")+"-coldkey")
		if err != nil {
			t.Fatal(err)
		}
		uid := uint16(fleet + 2)
		observation.Native[manifest.Hotkey] = ExistingUIDFact{UID: uid, Hotkey: fleetLifecycleHex(manifest.Hotkey), Coldkey: fleetLifecycleHex(coldkey)}
		account := subtensorAccountInfo{Nonce: 4}
		account.Data.Free = nativeTypes.U64(20_000_000)
		observation.Accounts[manifest.Hotkey] = account
		for index, member := range manifest.Members {
			binding, err := manifest.Binding(member, from, to)
			if err != nil {
				t.Fatal(err)
			}
			seed, _ := hex.DecodeString(roles.Clients[fmt.Sprintf("miner-%d", miners[index])].SeedHex)
			clientSig, err := binding.SignClient(ed25519.NewKeyFromSeed(seed))
			if err != nil {
				t.Fatal(err)
			}
			digest, _ := binding.Digest()
			hotSig, err := hotkey.Sign(digest[:])
			if err != nil {
				t.Fatal(err)
			}
			evidence := FleetBindingEvidence{Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: fleetLifecycleHex16(member.ClientID), ClientKey: fleetLifecycleHex(member.ClientKey), FleetID: fleetLifecycleHex(manifest.FleetID), Hotkey: fleetLifecycleHex(manifest.Hotkey), Generation: generation, ValidFromEpoch: from, ValidToEpoch: to, CommitmentHash: fleetLifecycleHex(binding.CommitmentHash), BindingDigest: fleetLifecycleHex(digest), ClientSignature: "0x" + hex.EncodeToString(clientSig), HotkeySignature: "0x" + hex.EncodeToString(hotSig), UID: uid, TransactionHash: common.Hash{0x34}.Hex(), BlockNumber: 100, BlockHash: common.Hash{0x35}.Hex()}
			observation.Evidence[member.ClientID] = []FleetBindingEvidence{evidence}
			observation.Records[member.ClientID] = fleetBindingVersionRead{Count: new(big.Int).SetUint64(generation), Record: stabi.STCoordinatorBindingRecord{FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientKey: binding.ClientKey, CommitmentHash: binding.CommitmentHash, Generation: generation, ValidFromEpoch: from, ValidToEpoch: to, Uid: uid}}
		}
	}
	renewal, err := prepareFleetRenewal(cfg, stateDir, base, roles, observation)
	if err != nil {
		t.Fatal(err)
	}
	return fleetRenewalTestFixture{cfg, stateDir, base, roles, observation, renewal}
}

func cloneFleetRenewalForTest(t *testing.T, source FleetRenewal) FleetRenewal {
	t.Helper()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var result FleetRenewal
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFleetRenewalPlansExpiredAndLiveMixedGenerations(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	before, _ := json.Marshal(fixture.base)
	plan, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFleetRenewalCustody(fixture.cfg, fixture.stateDir, fixture.roles, fixture.renewal); err != nil {
		t.Fatal(err)
	}
	actions, err := fleetRenewalActions(plan, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	oracleNonce, keeperNonce := fixture.renewal.OracleNonce, fixture.renewal.KeeperNonce
	for _, a := range actions {
		if a.Kind == "evm-transaction" {
			want := keeperNonce
			if a.Parameters["operation"] == "mirror" {
				want = oracleNonce
				oracleNonce++
			} else {
				keeperNonce++
			}
			if a.Parameters["renewal_expected_nonce"] != fmt.Sprint(want) {
				t.Fatal("pipeline changed exact nonce order")
			}
		}
		if a.Parameters["operation"] == "commitment" && a.Parameters["fleet"] == "11" && len(a.DependsOn) != 40 {
			t.Fatal("next native batch does not wait for every prior binding")
		}
		if a.Parameters["operation"] == "commitment" && a.Parameters["fleet"] == "2" && len(a.DependsOn) != 0 {
			t.Fatal("independent native signers remain serialized")
		}
		counts[a.Parameters["operation"]]++
		if a.Spend.AlphaRao != 0 || a.Spend.Registrations != 0 || a.Spend.SubnetCreations != 0 {
			t.Fatal("renewal authorized funding or registration")
		}
	}
	if counts["commitment"] != 202 || counts["mirror"] != 202 || counts["revoke"] != 8 || counts["bind"] != 808 {
		t.Fatalf("wrong exact action partition: %v", counts)
	}
	if plan.MaximumSpend.AlphaRao != fixture.base.MaximumSpend.AlphaRao || plan.MaximumSpend.Registrations != fixture.base.MaximumSpend.Registrations || plan.Limits != fixture.base.Limits {
		t.Fatal("renewal changed lifetime principal or registration limits")
	}
	if comparison, err := plan.MaximumSpend.EVMGasWei.Cmp(fixture.base.MaximumSpend.EVMGasWei); err != nil || comparison != 0 {
		t.Fatal("renewal increased the total EVM gas ceiling")
	}
	for _, fleet := range fixture.renewal.Fleets {
		for _, member := range fleet.Members {
			if member.Binding.Generation != member.Prior.Generation+1 || member.Binding.Hotkey != member.Prior.Hotkey || member.Binding.ClientID != member.Prior.ClientID || member.Binding.UID != member.Prior.UID {
				t.Fatal("renewal changed an existing identity")
			}
		}
	}
	after, _ := json.Marshal(fixture.base)
	if !bytes.Equal(before, after) {
		t.Fatal("renewal mutated the source plan")
	}
	raw, _ := json.Marshal(plan)
	if _, err := decodePersistedPlanBytes(raw); err != nil {
		t.Fatalf("renewal wire approval does not round trip: %v", err)
	}
	for _, fault := range []string{"missing-revoke", "unnecessary-revoke", "fee", "generation", "uid", "calldata", "budget"} {
		t.Run(fault, func(t *testing.T) {
			renewal := cloneFleetRenewalForTest(t, fixture.renewal)
			switch fault {
			case "missing-revoke":
				renewal.Fleets[4].Members[0].RevokeSignature = ""
			case "unnecessary-revoke":
				renewal.Fleets[0].Members[0].RevokeSignature = renewal.Fleets[4].Members[0].RevokeSignature
			case "fee":
				renewal.MaximumFeePerGasWei = fixture.base.MaximumEVMFeePerGasWei + 1
			case "generation":
				renewal.Fleets[0].Members[0].Prior.Generation++
			case "uid":
				renewal.Fleets[0].Members[0].Binding.UID++
			case "calldata":
				renewal.Fleets[0].Members[0].Binding.ClientSignature = "0x" + strings.Repeat("00", 64)
			case "budget":
				renewal.CampaignLiabilityWei = renewal.CampaignReserveBeforeWei
			}
			if _, err := appendFleetRenewalPlan(fixture.base, renewal); err == nil {
				t.Fatal("unsafe renewal accepted")
			}
		})
	}
}

func TestFleetRenewalRejectsChangedPrestateAndPreservesApproval(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	if err := validateFleetRenewalFreshPrestate(fixture.renewal, fixture.observed); err != nil {
		t.Fatal(err)
	}
	manifest, err := protocol.ParseFleetManifest(fixture.renewal.Fleets[0].Manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"nonce", "uid", "coldkey", "binding", "epoch"} {
		t.Run(fault, func(t *testing.T) {
			fresh := fixture.observed
			fresh.Native = map[[32]byte]ExistingUIDFact{}
			for key, value := range fixture.observed.Native {
				fresh.Native[key] = value
			}
			fresh.Records = map[[16]byte]fleetBindingVersionRead{}
			for key, value := range fixture.observed.Records {
				fresh.Records[key] = value
			}
			switch fault {
			case "nonce":
				fresh.Renewal.KeeperNonce++
			case "uid":
				entry := fresh.Native[manifest.Hotkey]
				entry.UID++
				fresh.Native[manifest.Hotkey] = entry
			case "coldkey":
				entry := fresh.Native[manifest.Hotkey]
				entry.Coldkey = common.Hash{1}.Hex()
				fresh.Native[manifest.Hotkey] = entry
			case "binding":
				entry := fresh.Records[manifest.Members[0].ClientID]
				entry.Record.Generation++
				fresh.Records[manifest.Members[0].ClientID] = entry
			case "epoch":
				fresh.Renewal.ObservedEpoch = fixture.renewal.ValidFromEpoch
			}
			if err := validateFleetRenewalFreshPrestate(fixture.renewal, fresh); err == nil {
				t.Fatal("changed prestate accepted")
			}
		})
	}
	plan, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	entries := []JournalEntry{{EntryHash: fixture.renewal.JournalHash}}
	if err := validateFleetRenewalSource(fixture.base, plan, entries); err != nil {
		t.Fatal(err)
	}
	entries = append(entries, JournalEntry{PlanHash: fixture.base.PlanHash, ActionID: "external-new-action"})
	if err := validateFleetRenewalSource(fixture.base, plan, entries); err == nil {
		t.Fatal("unreviewed journal advance accepted")
	}
	path := filepath.Join(fixture.stateDir, "public", "retained.json")
	if err := writeFleetRenewalImmutable(path, map[string]string{"generation": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := writeFleetRenewalImmutable(path, map[string]string{"generation": "2"}); err == nil {
		t.Fatal("old public generation overwritten")
	}
	retained, _ := os.ReadFile(path)
	if !bytes.Contains(retained, []byte(`"1"`)) {
		t.Fatal("old evidence changed")
	}
}

func TestFleetRenewalExactEVMRecoveryDoesNotResignOrRebroadcast(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("6", 64))
	if err != nil {
		t.Fatal(err)
	}
	chain := big.NewInt(945)
	to := common.Address{0x17}
	data := []byte{1, 2, 3, 4}
	action := Action{ID: "fleet.renew.1.1.bind.1", Kind: "evm-transaction", Target: to.Hex(), Parameters: map[string]string{"operation": "bind", "renewal_expected_nonce": "4", "renewal_expected_signer": crypto.PubkeyToAddress(key.PublicKey).Hex(), "renewal_calldata": "0x01020304", evmMaximumGasUnitsParameter: "100000", evmMaximumFeePerGasParameter: "10"}, Spend: Spend{EVMGasWei: "1000000"}}
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: chain, Nonce: 4, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(10), Gas: 55000, To: &to, Value: new(big.Int), Data: data}), ethTypes.LatestSignerForChainID(chain), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFleetRenewalSignedTransaction(action, tx, chain); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := tx.MarshalBinary()
	if err := atomicWrite(filepath.Join(stateDir, "transactions", stringsTrim0x(tx.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []JournalEntry{{DeploymentID: "renewal-test", PlanHash: "renewal-plan", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}, {DeploymentID: "renewal-test", PlanHash: "renewal-plan", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, Signer: crypto.PubkeyToAddress(key.PublicKey).Hex(), Nonce: "4", TransactionHash: tx.Hash().Hex(), RecoveryBlock: 19, RecoveryBlockHash: common.Hash{0x19}.Hex()}} {
		if err := journal.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	receipt := &ethTypes.Receipt{Type: tx.Type(), Status: 1, TxHash: tx.Hash(), BlockNumber: big.NewInt(20), BlockHash: common.Hash{0x20}, GasUsed: 40000, CumulativeGasUsed: 40000, EffectiveGasPrice: big.NewInt(6), Logs: []*ethTypes.Log{}}
	var writes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch call.Method {
		case "eth_getTransactionReceipt":
			result = receipt
		case "eth_getBlockByNumber":
			if string(call.Params[0]) == `"finalized"` {
				result = map[string]any{"number": "0x15", "hash": common.Hash{0x21}}
			} else {
				result = map[string]any{"number": "0x14", "hash": common.Hash{0x20}}
			}
		default:
			writes.Add(1)
			http.Error(w, "unexpected mutation/signing RPC "+call.Method, 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
	}))
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	manager := &EvmTxManager{client: client, chainID: chain, deploymentID: "renewal-test", stateDir: stateDir, journal: journal, key: key}
	got, err := manager.Send(context.Background(), "renewal-plan", action, &to, new(big.Int), data)
	if err != nil || got.TxHash != tx.Hash() {
		t.Fatalf("exact persisted renewal recovery failed: %v", err)
	}
	if writes.Load() != 0 {
		t.Fatal("finalized renewal recovery attempted a fresh nonce or broadcast")
	}
	changed := append([]byte(nil), data...)
	changed[0] ^= 1
	if _, err := manager.Send(context.Background(), "renewal-plan", action, &to, new(big.Int), changed); err == nil {
		t.Fatal("changed renewal calldata resumed")
	}
	for _, fault := range []string{"nonce", "signer", "target", "fee", "gas", "chain"} {
		t.Run(fault, func(t *testing.T) {
			copy := action
			copy.Parameters = map[string]string{}
			for k, v := range action.Parameters {
				copy.Parameters[k] = v
			}
			otherChain := new(big.Int).Set(chain)
			switch fault {
			case "nonce":
				copy.Parameters["renewal_expected_nonce"] = "5"
			case "signer":
				copy.Parameters["renewal_expected_signer"] = common.Address{0x45}.Hex()
			case "target":
				copy.Target = common.Address{0x46}.Hex()
			case "fee":
				copy.Parameters[evmMaximumFeePerGasParameter] = "9"
			case "gas":
				copy.Parameters[evmMaximumGasUnitsParameter] = "54000"
			case "chain":
				otherChain.SetInt64(1)
			}
			if err := validateFleetRenewalSignedTransaction(copy, tx, otherChain); err == nil {
				t.Fatal("unsafe signed renewal accepted")
			}
		})
	}
}

func TestFleetRenewalCLIRequiresExactImportedApproval(t *testing.T) {
	for _, args := range [][]string{{"fleet-renew"}, {"fleet-renew", "--apply", "--plan-hash", common.Hash{1}.Hex()}, {"fleet-renew", "--renewal-plan", "relative.json"}, {"setup", "--renewal-valid-from-epoch", "318"}, {"fleet-renew", "--renewal-plan", "/tmp/reviewed.json", "--renewal-valid-from-epoch", "318"}} {
		if _, _, err := parseCLI(args); err == nil {
			t.Fatalf("unsafe renewal command accepted: %v", args)
		}
	}
	if _, _, err := parseCLI([]string{"fleet-renew", "--renewal-valid-from-epoch", "318", "--renewal-valid-to-epoch", "349", "--renewal-max-fee-per-gas-wei", "25000000000"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseCLI([]string{"fleet-renew", "--apply", "--renewal-plan", "/tmp/reviewed.json", "--plan-hash", common.Hash{1}.Hex()}); err != nil {
		t.Fatal(err)
	}
}

func TestFleetRenewalHistoricalScopeExcludesFundingAndUnrelatedActions(t *testing.T) {
	cfg := testResolvedConfig(t)
	for _, test := range []struct {
		id   string
		want int
	}{{"fleet.commitment.201", 1}, {"fleet.refresh.commitment.100", 1}, {"fleet.mirror.202", 1}, {"fleet.bind.202.4", 1}, {"fleet.refresh.batch.1", 10}, {"fleet.install.batch.20", 10}, {"lifecycle.prepare.target.mirror", 1}, {"lifecycle.prepare.companion.bind.4", 1}, {"fleet.fund.1", 0}, {"fleet.register.1", 0}, {"lifecycle.provider.register", 0}, {"lifecycle.provider.cleanup.1", 0}, {"config.render", 0}, {"topology.launch", 0}, {"fleet.renew.1.1.bind.1", 0}} {
		fleets, err := fleetRenewalHistoricalActionFleets(cfg, Action{ID: test.id})
		if err != nil || len(fleets) != test.want {
			t.Fatalf("historical renewal scope %s: %v %v", test.id, fleets, err)
		}
	}
	if _, err := fleetRenewalHistoricalActionFleets(cfg, Action{ID: "fleet.refresh.batch.0"}); err == nil {
		t.Fatal("invalid historical batch admitted")
	}
}
