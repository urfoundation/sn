// Settlement v2 fixtures approve one complete five-action graph of the
// reviewed release from the shared contract fixture's synthetic deployer,
// directory and owned route. No fixture signature authorizes live work.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The synthetic graph's approved inputs: a whole-rao demand fee, and the
// validator's admitted address as the sole auditor with quorum one.
var (
	evmV2TestHotkey   = [32]byte{82}
	evmV2TestFee      = big.NewInt(4_000_000_000_000_000)
	evmV2TestOwner    = common.Address{41}
	evmV2TestV1       = common.Address{43}
	evmV2TestAuditor  = common.Address{44}
	evmV2TestBurnCap  = uint64(2)
	evmV2TestFeeCap   = "10"
	evmV2TestMaxTotal = "2106500000"
	// The five limits and the escrow funding, plus the batcher's 400,000 gas.
	evmV2TestBatcherMaxTotal = "2110500000"
)

// The reviewed settlement v2 catalog entries as their release file.
func evmV2TestRelease(t testing.TB) contractReleaseArtifacts {
	t.Helper()
	return evmTestCatalog(t, "SettlementV2ReleaseArtifacts", contractReleaseV2Schema)
}

// The exact release artifact by name.
func evmV2TestArtifactNamed(t *testing.T, release contractReleaseArtifacts, name string) contractReleaseArtifact {
	t.Helper()
	for _, artifact := range release.Artifacts {
		if artifact.Name == name {
			return artifact
		}
	}
	t.Fatalf("release lacks %s", name)
	return contractReleaseArtifact{}
}

// The vault2 constructor calldata for explicit words.
func evmV2TestVaultData(t *testing.T, release contractReleaseArtifacts, netuid uint16, hotkey, coldkey [32]byte, bootstrap common.Address, fee *big.Int) string {
	t.Helper()
	artifact := evmV2TestArtifactNamed(t, release, "SettlementVault2")
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := parsed.Pack("", netuid, hotkey, coldkey, bootstrap, fee)
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + artifact.Creation + hex.EncodeToString(arguments)
}

// The proxy CREATE calldata for an explicit implementation and initializer.
func evmV2TestProxyData(t *testing.T, release contractReleaseArtifacts, implementation common.Address, initializer []byte) string {
	t.Helper()
	artifact := evmV2TestArtifactNamed(t, release, "ERC1967Proxy")
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := parsed.Pack("", implementation, initializer)
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + artifact.Creation + hex.EncodeToString(arguments)
}

// The five approved envelopes of one deployer at consecutive nonces.
func evmV2TestActions(t *testing.T, release contractReleaseArtifacts, sender common.Address, nonce uint64) []evmPhaseAction {
	t.Helper()
	vault, implementation, proxy := crypto.CreateAddress(sender, nonce), crypto.CreateAddress(sender, nonce+2), crypto.CreateAddress(sender, nonce+3)
	initializer := stabi.NewSTSettlement2().PackInitialize(evmV2TestOwner, evmV2TestV1, vault, []common.Address{evmV2TestAuditor}, big.NewInt(1))
	funding := new(big.Int).Mul(new(big.Int).SetUint64(evmV2TestBurnCap), big.NewInt(1_000_000_000)).String()
	envelope := func(id string, offset uint64, to *common.Address, data, value string, gas uint64) evmPhaseAction {
		return evmPhaseAction{Id: id, Sender: sender, Nonce: nonce + offset, To: to, Data: data, ValueWei: value, Gas: gas, FeeCapWei: evmV2TestFeeCap, TipCapWei: "1"}
	}
	return []evmPhaseAction{
		envelope("vault2-create", 0, nil, evmV2TestVaultData(t, release, 25, evmV2TestHotkey, ss58.EvmMirrorPubkey(vault), sender, evmV2TestFee), "0", 4_000_000),
		envelope("escrow2-register", 1, &vault, "0x"+hex.EncodeToString(stabi.NewSTSettlementVault2().PackRegisterEscrow(evmV2TestBurnCap)), funding, 2_500_000),
		envelope("settlement2-create", 2, nil, "0x"+evmV2TestArtifactNamed(t, release, "Settlement2").Creation, "0", 3_000_000),
		envelope("proxy2-create", 3, nil, evmV2TestProxyData(t, release, implementation, initializer), "0", 1_000_000),
		envelope("vault2-link", 4, &vault, "0x"+hex.EncodeToString(stabi.NewSTSettlementVault2().PackSetCoordinatorOnce(proxy)), "0", 150_000),
	}
}

// The optional sixth envelope: the claim batcher's argument-free CREATE at
// the deployer's nonce after the binding.
func evmV2TestBatcherAction(t *testing.T, release contractReleaseArtifacts, sender common.Address, nonce uint64) evmPhaseAction {
	t.Helper()
	return evmPhaseAction{Id: "batcher-create", Sender: sender, Nonce: nonce, Data: "0x" + evmV2TestArtifactNamed(t, release, "ClaimBatcher").Creation, ValueWei: "0", Gas: 400_000, FeeCapWei: evmV2TestFeeCap, TipCapWei: "1"}
}

// Publish a release file and pin it in the fixture's unsigned plan.
func (self *evmCreateFixture) writeV2Release(release contractReleaseArtifacts) {
	self.t.Helper()
	raw, err := json.Marshal(release)
	if err != nil {
		self.t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(self.configPath), "release-v2.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		self.t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	self.config.Plan.Artifacts = planFileReference{Path: path, Sha256: "sha256:" + hex.EncodeToString(digest[:])}
}

// The original fixture's identity, route, window and custody inputs carry
// over unchanged; the separate v2 approval replaces the graph and the release.
func newEvmV2Fixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmCreateFixture(t)
	release := evmV2TestRelease(t)
	sender := f.config.Plan.Actions[0].Sender
	f.config.Schema = evmPhase2ConfigSchema
	f.config.Plan.Schema, f.config.Plan.DeploymentId, f.config.Plan.ReserveHotkey = evmPhase2Schema, "synthetic-settlement-v2", ""
	f.config.Plan.Actions = evmV2TestActions(t, release, sender, 0)
	f.config.Plan.MaximumAttempts, f.config.Plan.MaximumTotalWei = 8, evmV2TestMaxTotal
	f.writeV2Release(release)
	for _, name := range evmV2JournalNames() {
		prepareMainnetSnapshotTest(t, filepath.Join(f.config.Plan.RunDirectory, name), "mainnet-evm-action", 512*1024)
	}
	f.publishConfig()
	return f
}

// The v2 fixture whose approval adds the claim batcher's CREATE.
func newEvmV2BatcherFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmV2Fixture(t)
	sender := f.config.Plan.Actions[0].Sender
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmV2TestBatcherAction(t, evmV2TestRelease(t), sender, 5))
	f.config.Plan.MaximumTotalWei = evmV2TestBatcherMaxTotal
	f.publishConfig()
	return f
}

// The complete approved graph, built through the production selection: the
// five actions, then the claim batcher's when the plan approves it.
func (self *evmCreateFixture) v2Graph() []*evmCreatePlan {
	self.t.Helper()
	link, err := selectEvmCreatePlan(self.t.Context(), self.plan, "vault2-link", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	graph := append(link.v2Prior()[:4], &link)
	if len(self.config.Plan.Actions) > len(evmV2Actions) {
		batcher, err := selectEvmCreatePlan(self.t.Context(), self.plan, "batcher-create", self.configPath)
		if err != nil {
			self.t.Fatal(err)
		}
		graph = append(graph, &batcher)
	}
	return graph
}
