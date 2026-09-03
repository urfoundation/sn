package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

func TestGovernanceEntitlementSnapshotJSONPreservesEveryField(t *testing.T) {
	want := GovernanceEntitlementSnapshot{
		Epoch: 7, NoID: 2, PayoutRoot: "0xroot", ArtifactHash: "0xartifact",
		FundedRao: "101", TotalRao: "99", ClaimedRao: "42", ExpiryBlock: 123, Status: 2,
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got GovernanceEntitlementSnapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("governance entitlement evidence did not round-trip: got=%+v want=%+v json=%s", got, want, b)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"epoch", "no_id", "payout_root", "artifact_hash", "funded_rao", "total_rao", "claimed_rao", "expiry_block", "status"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("governance entitlement evidence omitted %q: %s", name, b)
		}
	}
}

func TestGovernanceCustodyProbeCalldataTargetsEveryImmutableBoundary(t *testing.T) {
	root, artifact := [32]byte{1}, [32]byte{2}
	destination, reserveHotkey := [32]byte{3}, [32]byte{4}
	data, err := governanceCustodyProbeCalldata(7, 2, root, artifact, 999, destination, reserveHotkey)
	if err != nil {
		t.Fatal(err)
	}
	adversaryABI, err := abi.JSON(strings.NewReader(CoordinatorAdversaryABI))
	if err != nil {
		t.Fatal(err)
	}
	method := adversaryABI.Methods["runCustodyProbes"]
	if len(data) < 4 || !bytes.Equal(data[:4], method.ID) || hex.EncodeToString(method.ID) != "bec929e5" {
		t.Fatalf("governance probe has selector %x, want installed v1 selector bec929e5", method.ID)
	}
	probeEvent := adversaryABI.Events["CustodyProbe"]
	if probeEvent.ID.Hex() != "0x8cb997f19649b53679d8ba4f21bba5755082e5f321a6e28307be984f0b0f4e7d" || len(probeEvent.Inputs) != 3 || !probeEvent.Inputs[0].Indexed || probeEvent.Inputs[1].Indexed || probeEvent.Inputs[2].Indexed {
		t.Fatalf("governance probe event is incompatible with installed v1: %+v", probeEvent)
	}
	values, err := method.Inputs.Unpack(data[4:])
	if err != nil || len(values) != 7 {
		t.Fatalf("unpack adversary call: values=%d err=%v", len(values), err)
	}
	if values[0].(*big.Int).Uint64() != 7 || values[1].(*big.Int).Uint64() != 2 || values[2].([32]byte) != root || values[3].([32]byte) != artifact || values[4].(uint64) != 999 || values[5].([32]byte) != destination || values[6].([32]byte) != reserveHotkey {
		t.Fatalf("installed v1 custody-probe arguments drifted: %v", values)
	}
}

func TestGovernanceSnapshotPinsEveryReadToOneCanonicalHead(t *testing.T) {
	head := ChainHead{Number: 123, Hash: "0x" + strings.Repeat("ab", 32)}
	proxy := common.HexToAddress("0x1111111111111111111111111111111111111111")
	implementation := common.HexToAddress("0x2222222222222222222222222222222222222222")
	vaultAddress := common.HexToAddress("0x3333333333333333333333333333333333333333")
	reserveAddress := common.HexToAddress("0x4444444444444444444444444444444444444444")
	owner := common.HexToAddress("0x5555555555555555555555555555555555555555")
	guardian := common.HexToAddress("0x6666666666666666666666666666666666666666")

	fixture := &finalSemanticPinnedEVMFixture{
		t: t, head: head, proxy: proxy.Hex(), slot: erc1967ImplementationSlot,
		code: map[string]string{strings.ToLower(implementation.Hex()): "0x6001"}, callResults: map[string]string{},
	}
	fixture.callResults["implementation-slot"] = hexutil.Encode(common.LeftPadBytes(implementation.Bytes(), common.HashLength))

	coordinatorABI, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	vaultABI, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		t.Fatal(err)
	}
	reserveABI, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	vault := stabi.NewSTSettlementVault()
	reserve := stabi.NewSTReserveSink()
	policy := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: [32]byte{7}, EffectiveEpoch: 1, EffectiveBlock: 100,
		EpochBlocks: 300, RootCommitWindowBlocks: 50, FinalizeOffsetBlocks: 150,
		CloseGraceBlocks: 5, ClaimTTLEpochs: 4, ClaimGraceEpochs: 1,
		MaximumBindingValidityEpochs: 9, CommitmentMaxAgeBlocks: 60,
		EpochDepositCapRao: big.NewInt(10), CampaignDepositCapRao: big.NewInt(100),
	}
	entitlement := stabi.STSettlementVaultEntitlement{
		PayoutRoot: [32]byte{8}, ArtifactHash: [32]byte{9}, Funded: big.NewInt(90),
		Total: big.NewInt(80), Claimed: big.NewInt(20), ExpiryBlock: 900, Status: 2,
	}
	fixture.setCallResult(t, &coordinatorABI, "owner", proxy.Hex(), coordinator.PackOwner(), owner)
	fixture.setCallResult(t, &coordinatorABI, "guardian", proxy.Hex(), coordinator.PackGuardian(), guardian)
	fixture.setCallResult(t, &coordinatorABI, "paused", proxy.Hex(), coordinator.PackPaused(), true)
	fixture.setCallResult(t, &coordinatorABI, "currentEpoch", proxy.Hex(), coordinator.PackCurrentEpoch(), big.NewInt(7))
	fixture.setCallResult(t, &coordinatorABI, "policyAt", proxy.Hex(), coordinator.PackPolicyAt(big.NewInt(7)), policy)
	fixture.setCallResult(t, &vaultABI, "coordinator", vaultAddress.Hex(), vault.PackCoordinator(), proxy)
	fixture.setCallResult(t, &reserveABI, "recorder", reserveAddress.Hex(), reserve.PackRecorder(), proxy)
	fixture.setCallResult(t, &reserveABI, "principal", reserveAddress.Hex(), reserve.PackPrincipal(), big.NewInt(1234))
	fixture.setCallResult(t, &reserveABI, "liveStake", reserveAddress.Hex(), reserve.PackLiveStake(), big.NewInt(1300))
	fixture.setCallResult(t, &vaultABI, "entitlement", vaultAddress.Hex(), vault.PackEntitlement(big.NewInt(6), big.NewInt(2)), entitlement)

	server := httptest.NewServer(fixture)
	defer server.Close()
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	manager := &EVMTxManager{client: ethclient.NewClient(rpcClient)}
	defer manager.Close()
	executor := &Executor{owner: manager, payloads: &DeploymentPayloads{Manifest: ContractDeployment{
		CoordinatorProxy: proxy, SettlementVault: vaultAddress, ReserveSink: reserveAddress,
	}}}
	ctx, err := withFinalizedEVMHead(t.Context(), head)
	if err != nil {
		t.Fatal(err)
	}
	got, err := executor.governanceSnapshot(ctx, 6, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.FinalizedHead != head || got.Implementation != implementation.Hex() || got.Owner != owner.Hex() || got.Guardian != guardian.Hex() || !got.Paused || got.PolicyHash != "0x"+hex.EncodeToString(policy.PolicyHash[:]) || got.VaultCoordinator != proxy.Hex() || got.ReserveRecorder != proxy.Hex() || got.ReservePrincipalRao != "1234" || got.ReserveLiveStakeRao != "1300" || got.Entitlement != entitlementSnapshot(6, 2, entitlement) {
		t.Fatalf("governance snapshot drifted: %+v", got)
	}
	code, err := codeAtCanonicalHead(ctx, manager, implementation, head)
	if err != nil || !bytes.Equal(code, []byte{0x60, 0x01}) {
		t.Fatalf("canonical implementation code = %x, error=%v", code, err)
	}
	fixture.mu.Lock()
	requestCount := len(fixture.calls)
	fixture.mu.Unlock()
	if requestCount != 12 {
		t.Fatalf("governance snapshot and code check issued %d canonical reads, want 12", requestCount)
	}
}
