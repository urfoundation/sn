// Live opt-in probes bind the checked-in release harness to runtime 453 and
// provide an explicitly confirmed, bounded bootstrap for netuid 521.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ss58"
)

func TestLiveNativeMirrorBalances(t *testing.T) {
	raw := strings.TrimSpace(os.Getenv("SIM_TESTNET_NATIVE_MIRRORS"))
	if raw == "" {
		t.Skip("set SIM_TESTNET_NATIVE_MIRRORS to a comma-separated EVM address list")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	chain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		t.Fatal(err)
	}
	header, err := chain.API.RPC.Chain.GetHeader(finalized)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range strings.Split(raw, ",") {
		text = strings.TrimSpace(text)
		if !common.IsHexAddress(text) {
			t.Fatalf("invalid EVM address %q", text)
		}
		address := common.HexToAddress(text)
		free, readErr := readFreeBalanceAtHash(chain, ss58.EvmMirrorPubkey(address), finalized)
		if readErr != nil {
			t.Fatal(readErr)
		}
		mirror, encodeErr := ss58.EvmMirrorAddress(address, ss58.BittensorPrefix)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		t.Logf("finalized_block=%d evm=%s mirror=%s free_rao=%d", header.Number, address.Hex(), mirror, free)
	}
}

func TestLiveVaultWalletResolution(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_WALLET") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_WALLET=1 to validate the configured encrypted testnet wallet")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Netuid != 521 || cfg.WalletPublic == "" || cfg.WalletHotkeyPublic == "" || cfg.WalletMaterial == "" || cfg.WalletPassword == "" {
		t.Fatal("resolved live wallet/config is incomplete")
	}
}

func TestLiveStakeNetuid521Alpha(t *testing.T) {
	const (
		confirmation = "stake-netuid-521-21000000-limit-473744"
		stakeTAORao  = uint64(21_000_000)
		limitPrice   = uint64(473_744)
		targetAlpha  = uint64(6_000_000_000)
		publicRPC    = "wss://test.finney.opentensor.ai:443"
		publicEVMRPC = "https://test.chain.opentensor.ai"
	)
	if os.Getenv("SIM_TESTNET_STAKE_ALPHA") != confirmation {
		t.Skip("explicit live-stake confirmation is absent")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Netuid != 521 || stakeTAORao > cfg.MaximumTAORao || targetAlpha > cfg.MaximumAlphaRao {
		t.Fatal("live stake request exceeds the configured netuid or spending limits")
	}
	cfg.OperationalSubstrate = publicRPC
	cfg.OperationalEVM = publicEVMRPC
	cfg.OperationalRPCMode = rpcModePublicOverride
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	before, beforeErr := ReadSetupFacts(ctx, cfg)
	if before == nil {
		t.Fatalf("read pre-stake facts: %v", beforeErr)
	}
	if before.AlphaAvailableRao >= targetAlpha {
		t.Logf("target already satisfied at finalized block %d: alpha=%d", before.FinalizedBlock, before.AlphaAvailableRao)
		return
	}
	if before.AlphaAvailableRao != 0 {
		t.Fatalf("refusing to apply fixed bootstrap amount over an unexpected existing alpha position of %d rao", before.AlphaAvailableRao)
	}
	if before.WalletFreeTAORao < stakeTAORao+10_000_000 {
		t.Fatal("wallet free balance is too low for the bounded stake and transaction fee reserve")
	}
	stateDir := filepath.Join(cfg.Repos.SN, "sim-testnet", "runs", "alpha-bootstrap-netuid-521")
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	manager, err := DialSubstrateManager(cfg, stateDir, journal)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	transferSetting, err := manager.ReadHyper("transfer_enabled")
	if err != nil {
		t.Fatal(err)
	}
	transferEnabled, ok := transferSetting.(bool)
	if !ok {
		t.Fatalf("unexpected transfer_enabled value %T", transferSetting)
	}
	if !transferEnabled {
		enableAction := Action{
			ID:          "bootstrap.enable-subtoken.netuid-521",
			Kind:        "substrate-extrinsic",
			Target:      "netuid:521",
			Description: "apply the release-1.0 transfer_enabled prerequisite",
			Parameters:  map[string]string{"transfer_enabled": "true"},
			Spend:       Spend{TAORao: 5_000_000},
		}
		enableAction.IntentHash, err = canonicalHashHex(enableAction)
		if err != nil {
			t.Fatal(err)
		}
		enablePlanHash, hashErr := canonicalHashHex(struct {
			Schema, DeploymentID string
			Netuid               uint16
			Action               Action
		}{Schema: "urnetwork-alpha-bootstrap-prerequisite-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, Netuid: cfg.Netuid, Action: enableAction})
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		if _, exists := journal.LatestTransaction(enablePlanHash, enableAction.ID, enableAction.IntentHash); !exists {
			if err := journal.Append(JournalEntry{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: enablePlanHash, ActionID: enableAction.ID, IntentHash: enableAction.IntentHash, Stage: StageIntent}); err != nil {
				t.Fatal(err)
			}
		}
		enableCall, callErr := manager.HyperCall("transfer_enabled", true)
		if callErr != nil {
			t.Fatal(callErr)
		}
		enableHash, enableBlock, sendErr := manager.Send(ctx, enablePlanHash, enableAction, enableCall)
		if sendErr != nil {
			t.Fatal(sendErr)
		}
		observed, readErr := manager.ReadHyper("transfer_enabled")
		if readErr != nil || observed != true {
			t.Fatalf("transfer_enabled finalized postcondition failed: value=%v error=%v", observed, readErr)
		}
		t.Logf("finalized transfer_enabled prerequisite tx=%s block=%d", enableHash.Hex(), enableBlock)
	}
	activation, err := manager.ActivationState()
	if err != nil {
		t.Fatal(err)
	}
	if !activation.SubtokenEnabled {
		readyAt, addOK := checkedAdd(activation.NetworkRegisteredAt, activation.StartCallDelay)
		if !addOK || activation.FirstEmissionBlockNumber != 0 || activation.FinalizedBlock < readyAt {
			t.Fatalf("subnet activation is not ready: %+v", activation)
		}
		startAction := Action{
			ID:          "bootstrap.start-call.netuid-521",
			Kind:        "substrate-extrinsic",
			Target:      "netuid:521",
			Description: "activate the existing subnet token and first emission block",
			Parameters: map[string]string{
				"network_registered_at": strconv.FormatUint(activation.NetworkRegisteredAt, 10),
				"ready_at":              strconv.FormatUint(readyAt, 10),
				"start_call_delay":      strconv.FormatUint(activation.StartCallDelay, 10),
			},
			Spend: Spend{TAORao: 5_000_000},
		}
		startAction.IntentHash, err = canonicalHashHex(startAction)
		if err != nil {
			t.Fatal(err)
		}
		startPlanHash, hashErr := canonicalHashHex(struct {
			Schema, DeploymentID string
			Netuid               uint16
			Action               Action
		}{Schema: "urnetwork-subnet-activation-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, Netuid: cfg.Netuid, Action: startAction})
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		if _, exists := journal.LatestTransaction(startPlanHash, startAction.ID, startAction.IntentHash); !exists {
			if err := journal.Append(JournalEntry{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: startPlanHash, ActionID: startAction.ID, IntentHash: startAction.IntentHash, Stage: StageIntent}); err != nil {
				t.Fatal(err)
			}
		}
		startCall, callErr := manager.StartCallCall()
		if callErr != nil {
			t.Fatal(callErr)
		}
		startHash, startBlock, sendErr := manager.Send(ctx, startPlanHash, startAction, startCall)
		if sendErr != nil {
			t.Fatal(sendErr)
		}
		activation, err = manager.ActivationState()
		if err != nil || !activation.SubtokenEnabled || activation.FirstEmissionBlockNumber == 0 {
			t.Fatalf("subnet activation finalized postcondition failed: state=%+v error=%v", activation, err)
		}
		t.Logf("finalized subnet activation tx=%s block=%d first_emission_block=%d", startHash.Hex(), startBlock, activation.FirstEmissionBlockNumber)
	}
	var spot uint64
	if err := manager.chain.API.Client.Call(&spot, "swap_currentAlphaPrice", cfg.Netuid); err != nil {
		t.Fatal(err)
	}
	if spot == 0 || spot > limitPrice {
		t.Fatalf("spot price %d exceeds bounded limit %d", spot, limitPrice)
	}
	var encodedQuote []byte
	if err := manager.chain.API.Client.Call(&encodedQuote, "swap_simSwapTaoForAlpha", cfg.Netuid, stakeTAORao); err != nil {
		t.Fatal(err)
	}
	if len(encodedQuote) != 48 {
		t.Fatalf("unexpected swap quote length %d", len(encodedQuote))
	}
	quote := make([]uint64, 6)
	for i := range quote {
		quote[i] = binary.LittleEndian.Uint64(encodedQuote[i*8 : (i+1)*8])
	}
	if quote[1] < targetAlpha || quote[0]+quote[2] > stakeTAORao {
		t.Fatalf("bounded quote is insufficient or malformed: tao=%d alpha=%d fee=%d", quote[0], quote[1], quote[2])
	}
	hotkey, err := ss58.DecodeWithPrefix(cfg.WalletHotkeyPublic, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	action := Action{
		ID:          "bootstrap.alpha.netuid-521.after-start",
		Kind:        "substrate-extrinsic",
		Target:      cfg.WalletHotkeyPublic,
		Description: "buy netuid 521 alpha with a fill-or-kill Dynamic TAO stake",
		Parameters: map[string]string{
			"allow_partial": "false", "amount_staked_rao": strconv.FormatUint(stakeTAORao, 10),
			"limit_price_rao_per_alpha": strconv.FormatUint(limitPrice, 10), "netuid": strconv.Itoa(int(cfg.Netuid)),
			"precondition": "SubtokenEnabled=true",
		},
		Spend: Spend{TAORao: 30_000_000},
	}
	action.IntentHash, err = canonicalHashHex(action)
	if err != nil {
		t.Fatal(err)
	}
	planHash, err := canonicalHashHex(struct {
		Schema, DeploymentID string
		Netuid               uint16
		Action               Action
	}{Schema: "urnetwork-alpha-bootstrap-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, Netuid: cfg.Netuid, Action: action})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := journal.LatestTransaction(planHash, action.ID, action.IntentHash); !exists {
		if err := journal.Append(JournalEntry{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
			t.Fatal(err)
		}
	}
	call, err := manager.AddStakeLimitCall(hotkey, stakeTAORao, limitPrice, false)
	if err != nil {
		t.Fatal(err)
	}
	txHash, block, err := manager.Send(ctx, planHash, action, call)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ReadSetupFacts(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	wantHotkey := fmt.Sprintf("0x%x", hotkey)
	if after.AlphaSourceHotkey != wantHotkey || after.AlphaAvailableRao < targetAlpha {
		t.Fatalf("finalized alpha postcondition failed: hotkey=%s alpha=%d", after.AlphaSourceHotkey, after.AlphaAvailableRao)
	}
	if before.WalletFreeTAORao < after.WalletFreeTAORao || before.WalletFreeTAORao-after.WalletFreeTAORao > action.Spend.TAORao {
		t.Fatalf("TAO spend exceeded bootstrap ceiling: before=%d after=%d", before.WalletFreeTAORao, after.WalletFreeTAORao)
	}
	t.Logf("finalized tx=%s block=%d alpha=%d tao_spent=%d spot=%d limit=%d quote_alpha=%d", txHash.Hex(), block, after.AlphaAvailableRao, before.WalletFreeTAORao-after.WalletFreeTAORao, spot, limitPrice, quote[1])
}

func TestLiveBalanceProbe(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_READ") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_READ=1 to run public testnet read checks")
	}
	// The finalized fact set includes the deterministic deployer nonce and
	// therefore needs the same role-secret derivation inputs as plan/doctor.
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg.OperationalSubstrate = "wss://test.finney.opentensor.ai:443"
	cfg.OperationalEVM = "https://test.chain.opentensor.ai"
	cfg.OperationalRPCMode = rpcModePublicOverride
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	facts, setupErr := ReadSetupFacts(ctx, cfg)
	b, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(b))
	if setupErr != nil {
		t.Logf("setup readiness: %v", setupErr)
	}
	rewards, rewardsErr := inspectNativeRewards(cfg, cfg.OperationalSubstrate)
	if rewardsErr != "" || rewards == nil || facts == nil || len(rewards.EmissionRao) != int(facts.ExistingUIDCount) || len(rewards.Incentive) != int(facts.ExistingUIDCount) || len(rewards.Dividends) != int(facts.ExistingUIDCount) || len(rewards.TotalHotkeyAlphaRao) != int(facts.ExistingUIDCount) {
		t.Fatalf("native reward vectors=%+v error=%s existing_uids=%v", rewards, rewardsErr, func() any {
			if facts == nil {
				return nil
			}
			return facts.ExistingUIDCount
		}())
	}
	t.Logf("native rewards at block %d: emission=%v incentive=%v dividends=%v total_hotkey_alpha=%v", rewards.FinalizedHead.Number, rewards.EmissionRao, rewards.Incentive, rewards.Dividends, rewards.TotalHotkeyAlphaRao)
	chain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	report, err := chain.DescribeCall(crv4.PalletName, "add_stake_limit")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("add_stake_limit metadata: %s", metadata)
	var quote []byte
	if err := chain.API.Client.Call(&quote, "swap_simSwapTaoForAlpha", cfg.Netuid, uint64(21_000_000)); err != nil {
		t.Fatal(err)
	}
	if len(quote)%8 != 0 {
		t.Fatalf("unexpected quote length %d", len(quote))
	}
	values := make([]uint64, len(quote)/8)
	for i := range values {
		values[i] = binary.LittleEndian.Uint64(quote[i*8 : (i+1)*8])
	}
	t.Logf("21m rao quote words: %v", values)
}

func TestLiveEVMMirrorBalanceSemantics(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_EVM_BALANCE") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_EVM_BALANCE=1 to compare finalized Substrate and EVM balances")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	address, err := roles.EVMAddress("deployer")
	if err != nil {
		t.Fatal(err)
	}
	mirror := ss58Mirror(address)
	chain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	key, err := types.CreateStorageKey(chain.Meta, "System", "Account", mirror[:])
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		t.Fatal(err)
	}
	var info subtensorAccountInfo
	if ok, readErr := chain.API.RPC.State.GetStorage(key, &info, finalized); readErr != nil || !ok {
		t.Fatalf("read finalized mirror account: present=%t error=%v", ok, readErr)
	}
	evm, err := ethclient.Dial(cfg.OperationalEVM)
	if err != nil {
		t.Fatal(err)
	}
	defer evm.Close()
	header, err := chain.API.RPC.Chain.GetHeader(finalized)
	if err != nil {
		t.Fatal(err)
	}
	balance, err := evm.BalanceAt(context.Background(), common.Address(address), new(big.Int).SetUint64(uint64(header.Number)))
	if err != nil {
		t.Fatal(err)
	}
	constant, err := chain.Meta.FindConstantValue("Balances", "ExistentialDeposit")
	if err != nil {
		t.Fatal(err)
	}
	depositRao, err := decodeRuntimeExistentialDepositRao(constant)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(info.Data.Reserved) != 0 || uint64(info.Data.Frozen) != 0 || uint64(info.Data.Free) <= depositRao {
		t.Fatalf("deployer mirror has unexpected account constraints: free=%d reserved=%d frozen=%d deposit=%d", info.Data.Free, info.Data.Reserved, info.Data.Frozen, depositRao)
	}
	want := new(big.Int).Mul(new(big.Int).SetUint64(uint64(info.Data.Free)-depositRao), new(big.Int).SetUint64(evmWeiPerRao))
	if balance.Cmp(want) != 0 {
		t.Fatalf("EVM reducible balance = %s, want (free-deposit)*1e9 = %s", balance, want)
	}
	t.Logf("address=%s free_rao=%d reserved_rao=%d frozen_rao=%d flags=%v evm_wei=%s", address, info.Data.Free, info.Data.Reserved, info.Data.Frozen, info.Data.Flags, balance)
}
