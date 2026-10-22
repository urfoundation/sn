// The settlement v2 end-to-end fixture deploys the real stack on the e2e
// chain: the v1 reserve sink, vault and coordinator proxy from the reviewed
// catalog in Deploy.s.sol's order with one v1 operator, then vault2 with its
// escrow registration on the next nonce (SPEC A7), STSettlement2 behind an
// ERC1967Proxy with one auditor and quorum 1, and the pool through
// registerOperatorPool. The windows are mainnet's: a 7,200-block epoch 0, then
// 50,400-block epochs with a 1,200-block commit window. The operator builds
// each root with the ledger package and publishes it at the pinned ledger
// endpoints; the auditor approves only what ledger.Verify passes.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The test executable enters the real snclaim command under its name.
func init() {
	if len(os.Args) > 0 && os.Args[0] == "snclaim" {
		onchain.Run(os.Args[1:])
		os.Exit(0)
	}
}

const (
	e2eNoID         = 1
	e2eBurnLimit    = 1_000_000_000
	e2eDemandFeeWei = 1_000_000_000_000_000
	e2eWindow       = 14_400
	e2eCommitWindow = 1_200
	e2eEpochBlocks  = 50_400
	e2eEpoch0Blocks = 7_200
	e2eMinimumTTL   = 8 * e2eEpoch0Blocks
	e2eActorFunds   = 1_000_000 // TAO
	e2eDefaultFirst = 2
)

var (
	e2eVault2Binding      = stabi.NewSTSettlementVault2()
	e2eSettlementBinding  = stabi.NewSTSettlement2()
	e2eCoordinatorBinding = stabi.NewSTCoordinator()
	e2eVault1Binding      = stabi.NewSTSettlementVault()
	e2eSinkBinding        = stabi.NewSTReserveSink()
)

// e2eArtifact is a contract's ABI and exact creation bytecode.
type e2eArtifact struct {
	abi      abi.ABI
	creation []byte
}

var e2eArtifacts sync.Map

// e2eCatalogArtifact reads a contract from the reviewed release catalogs: the
// v1 release's, then settlement v2's.
func e2eCatalogArtifact(t testing.TB, name string) *e2eArtifact {
	t.Helper()
	if cached, ok := e2eArtifacts.Load("catalog/" + name); ok {
		return cached.(*e2eArtifact)
	}
	for _, release := range []func(testing.TB) contractReleaseArtifacts{evmTestRelease, evmV2TestRelease} {
		for _, artifact := range release(t).Artifacts {
			if artifact.Name != name {
				continue
			}
			parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
			if err != nil {
				t.Fatal(err)
			}
			loaded := &e2eArtifact{abi: parsed, creation: common.FromHex(artifact.Creation)}
			e2eArtifacts.Store("catalog/"+name, loaded)
			return loaded
		}
	}
	t.Fatalf("the reviewed catalogs have no %s", name)
	return nil
}

// e2eForgeSources are the test contracts the suite reads from a Foundry build
// of sn/evm: the test coordinator upgrade.
var e2eForgeSources = []struct{ source, name string }{
	{"test/utils/V2LifecycleHandler.sol", "STSettlement2Surplus"},
}

// e2eForgeBuild builds those sources once per test binary in a private
// directory and removes it once their artifacts are read, so the suite never
// reads another build, such as a release gate's that is still being written.
var e2eForgeBuild = sync.OnceValue(func() error {
	root, err := os.MkdirTemp("", "sn-e2e-forge-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	arguments := []string{"build"}
	for _, contract := range e2eForgeSources {
		arguments = append(arguments, contract.source)
	}
	build := exec.Command("forge", arguments...)
	build.Dir = filepath.Join("..", "evm")
	build.Env = append(os.Environ(), "FOUNDRY_OUT="+filepath.Join(root, "out"), "FOUNDRY_CACHE_PATH="+filepath.Join(root, "cache"))
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("forge build in sn/evm: %v\n%s", err, output)
	}
	for _, contract := range e2eForgeSources {
		raw, err := os.ReadFile(filepath.Join(root, "out", filepath.Base(contract.source), contract.name+".json"))
		if err != nil {
			return err
		}
		var artifact struct {
			ABI      json.RawMessage `json:"abi"`
			Bytecode struct {
				Object string `json:"object"`
			} `json:"bytecode"`
		}
		if err := json.Unmarshal(raw, &artifact); err != nil {
			return err
		}
		parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
		if err != nil {
			return err
		}
		loaded := &e2eArtifact{abi: parsed, creation: common.FromHex(artifact.Bytecode.Object)}
		if len(loaded.creation) == 0 {
			return fmt.Errorf("forge artifact %s has no creation bytecode", contract.name)
		}
		e2eArtifacts.Store("forge/"+contract.name, loaded)
	}
	return nil
})

// e2eForgeArtifact reads one of e2eForgeSources' contracts.
func e2eForgeArtifact(t testing.TB, name string) *e2eArtifact {
	t.Helper()
	if err := e2eForgeBuild(); err != nil {
		t.Fatal(err)
	}
	loaded, ok := e2eArtifacts.Load("forge/" + name)
	if !ok {
		t.Fatalf("the suite builds no %s", name)
	}
	return loaded.(*e2eArtifact)
}

// e2eActor is an EVM key.
type e2eActor struct {
	name    string
	key     *ecdsa.PrivateKey
	address common.Address
}

func newE2EActor(name string) *e2eActor {
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte("urnetwork-e2e-actor:" + name)))
	if err != nil {
		panic(err)
	}
	return &e2eActor{name: name, key: key, address: crypto.PubkeyToAddress(key.PublicKey)}
}

// keyFile writes the actor's key as snclaim and the daemon read it.
func (self *e2eActor) keyFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), self.name+".key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(crypto.FromECDSA(self.key))), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// e2eKey is a synthetic 32-byte hotkey or coldkey.
func e2eKey(name string) [32]byte {
	return crypto.Keccak256Hash([]byte("urnetwork-e2e-key:" + name))
}

// e2eRow is a provider row for client id client.
func e2eRow(client uint64, coldkey [32]byte, usage uint64) protocol.LedgerUsageRow {
	row := protocol.LedgerUsageRow{Coldkey: coldkey, UsageBytes: usage}
	binary.BigEndian.PutUint64(row.ClientID[8:], client)
	return row
}

func e2eRao(value uint64) *big.Int { return new(big.Int).SetUint64(value) }

func e2eWei(tao uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(tao), big.NewInt(1e18))
}

// e2eFixture is the deployed stack and its actors.
type e2eFixture struct {
	t     testing.TB
	chain *e2eChain
	// Actors: the deployer is both bootstraps; the operator is pool 1's v1
	// root signer and the server's sender; the auditor is the validator.
	deployer, owner, guardian, oracle, operator, depositSigner, auditor, anyone, watcher *e2eActor
	// v1
	sink, vault1, coordinator                common.Address
	vault1Coldkey                            [32]byte
	escrowV1, reserveHotkey, poolV1, deposit [32]byte
	operatorColdkey                          [32]byte
	// v2
	vault2, settlement              common.Address
	vault2Coldkey, escrowV2, poolV2 [32]byte
	firstEpoch                      uint64
	vault2ABI, settlementABI        abi.ABI
	publisher                       *e2ePublisher
	// The operator's state: each covered epoch's rows, and the chunks it was
	// first built with (§6.4 fixed inputs).
	rows   map[uint64][]protocol.LedgerUsageRow
	chunks map[uint64][]ledger.Ref
}

// newE2EFixture deploys v1 and v2 and registers pool 1 for v2 from
// firstEpoch.
func newE2EFixture(t testing.TB, firstEpoch uint64, extra ...*e2eActor) *e2eFixture {
	t.Helper()
	f := newE2EDeployment(t, firstEpoch, extra...)
	f.publisher = newE2EPublisher(t, f)
	f.registerPool()
	return f
}

// newE2EDeployment deploys v1 and v2 with no pool registered for v2 yet.
func newE2EDeployment(t testing.TB, firstEpoch uint64, extra ...*e2eActor) *e2eFixture {
	t.Helper()
	f := &e2eFixture{t: t, deployer: newE2EActor("deployer"), owner: newE2EActor("owner"), guardian: newE2EActor("guardian"), oracle: newE2EActor("oracle"),
		operator: newE2EActor("operator"), depositSigner: newE2EActor("deposit-signer"), auditor: newE2EActor("auditor"), anyone: newE2EActor("anyone"), watcher: newE2EActor("watcher"),
		escrowV1: e2eKey("v1-escrow"), reserveHotkey: e2eKey("v1-reserve"), poolV1: e2eKey("v1-pool"), deposit: e2eKey("v1-deposit"),
		operatorColdkey: e2eKey("operator-coldkey"), escrowV2: e2eKey("v2-escrow"), poolV2: e2eKey("v2-pool"), firstEpoch: firstEpoch,
		rows: map[uint64][]protocol.LedgerUsageRow{}, chunks: map[uint64][]ledger.Ref{}}
	funded := map[common.Address]*big.Int{}
	for _, actor := range append([]*e2eActor{f.deployer, f.owner, f.guardian, f.oracle, f.operator, f.depositSigner, f.auditor, f.anyone, f.watcher}, extra...) {
		funded[actor.address] = e2eWei(e2eActorFunds)
	}
	f.chain = newE2EChain(t, funded)
	f.deployV1()
	f.deployV2()
	return f
}

// deploy creates a contract from actor and returns its address.
func (f *e2eFixture) deploy(actor *e2eActor, artifact *e2eArtifact, value *big.Int, args ...any) common.Address {
	f.t.Helper()
	constructor, err := artifact.abi.Pack("", args...)
	if err != nil {
		f.t.Fatal(err)
	}
	receipt, revert := f.chain.send(actor.key, nil, value, append(slices.Clone(artifact.creation), constructor...))
	if receipt.Status != types.ReceiptStatusSuccessful {
		f.t.Fatalf("creation by %s reverted 0x%x", actor.name, revert)
	}
	return receipt.ContractAddress
}

// must sends data from actor and requires success.
func (f *e2eFixture) must(actor *e2eActor, to common.Address, value *big.Int, data []byte) *types.Receipt {
	f.t.Helper()
	receipt, revert := f.chain.send(actor.key, &to, value, data)
	if receipt.Status != types.ReceiptStatusSuccessful {
		f.t.Fatalf("%s's call 0x%x to %s reverted %s", actor.name, data[:4], to, f.errorName(revert))
	}
	return receipt
}

// refused sends data from actor and requires the named custom error.
func (f *e2eFixture) refused(actor *e2eActor, to common.Address, value *big.Int, data []byte, name string) *types.Receipt {
	f.t.Helper()
	receipt, revert := f.chain.send(actor.key, &to, value, data)
	if receipt.Status != types.ReceiptStatusFailed || f.errorName(revert) != name {
		f.t.Fatalf("%s's call 0x%x to %s: status %d revert %s, want %s", actor.name, data[:4], to, receipt.Status, f.errorName(revert), name)
	}
	return receipt
}

// errorName names a revert by the v2 contracts' and v1's custom errors.
func (f *e2eFixture) errorName(revert []byte) string {
	if len(revert) < 4 {
		return fmt.Sprintf("0x%x", revert)
	}
	for _, contract := range []abi.ABI{f.vault2ABI, f.settlementABI, e2eCatalogArtifact(f.t, "Coordinator").abi, e2eCatalogArtifact(f.t, "SettlementVault").abi} {
		if failure, err := contract.ErrorByID([4]byte(revert[:4])); err == nil {
			return failure.Name
		}
	}
	return fmt.Sprintf("0x%x", revert)
}

// view calls a view at the head and unpacks it.
func e2eView[T any](f *e2eFixture, to common.Address, data []byte, unpack func([]byte) (T, error)) T {
	f.t.Helper()
	out, err := f.chain.call(to, data)
	if err != nil {
		f.t.Fatalf("view 0x%x of %s: %v", data[:4], to, err)
	}
	value, err := unpack(out)
	if err != nil {
		f.t.Fatal(err)
	}
	return value
}

func (f *e2eFixture) policy(epochBlocks, finalizeOffset uint64) stabi.STCoordinatorPolicySnapshot {
	return stabi.STCoordinatorPolicySnapshot{PolicyHash: crypto.Keccak256Hash([]byte("urnetwork-e2e-policy-v1")), EpochBlocks: epochBlocks,
		RootCommitWindowBlocks: e2eCommitWindow, FinalizeOffsetBlocks: finalizeOffset, CloseGraceBlocks: 120, ClaimTTLEpochs: 8, ClaimGraceEpochs: 1,
		MaximumBindingValidityEpochs: 12, CommitmentMaxAgeBlocks: 100_800, EpochDepositCapRao: e2eRao(10_000_000_000), CampaignDepositCapRao: e2eRao(196_000_000_000)}
}

// deployV1 runs Deploy.s.sol's order: the sink, the vault, the coordinator
// implementation, the escrow registration, then the proxy, each contract's
// custody coldkey being its address's mapped coldkey. The owner schedules the
// production cadence from epoch 1 and registers operator 1.
func (f *e2eFixture) deployV1() {
	f.t.Helper()
	nonce := f.chain.nonce(f.deployer.address)
	predicted := func(offset uint64) common.Address { return crypto.CreateAddress(f.deployer.address, nonce+offset) }
	f.sink = f.deploy(f.deployer, e2eCatalogArtifact(f.t, "ReserveSink"), nil, uint16(e2eNetuid), f.reserveHotkey, ss58.EvmMirrorPubkey(predicted(0)), f.deployer.address)
	f.vault1Coldkey = ss58.EvmMirrorPubkey(predicted(1))
	f.vault1 = f.deploy(f.deployer, e2eCatalogArtifact(f.t, "SettlementVault"), nil, uint16(e2eNetuid), f.escrowV1, f.vault1Coldkey, uint64(e2eMinimumTTL), uint64(e2eTransferFloorTaoRao), f.deployer.address)
	implementation := f.deploy(f.deployer, e2eCatalogArtifact(f.t, "Coordinator"), nil)
	f.must(f.deployer, f.vault1, e2eWei(1), e2eVault1Binding.PackRegisterEscrow(e2eBurnLimit))
	initialize := e2eCoordinatorBinding.PackInitialize(e2eNetuid, f.owner.address, f.guardian.address, ss58.EvmMirrorPubkey(predicted(4)), f.vault1, f.sink, f.oracle.address, f.policy(e2eEpoch0Blocks, 2_400))
	f.coordinator = f.deploy(f.deployer, e2eCatalogArtifact(f.t, "ERC1967Proxy"), nil, implementation, initialize)
	if f.coordinator != predicted(4) {
		f.t.Fatal("coordinator proxy address drift")
	}
	f.must(f.deployer, f.sink, nil, e2eSinkBinding.PackSetRecorderOnce(f.coordinator))
	f.must(f.deployer, f.vault1, nil, e2eVault1Binding.PackSetCoordinatorOnce(f.coordinator))
	production := f.policy(e2eEpochBlocks, e2eWindow)
	production.EffectiveEpoch = 1
	f.must(f.owner, f.coordinator, nil, e2eCoordinatorBinding.PackSchedulePolicy(production))
	f.must(f.owner, f.coordinator, e2eWei(1), e2eCoordinatorBinding.PackRegisterOperator(e2eRao(e2eNoID), f.operatorColdkey, f.poolV1, f.deposit, f.depositSigner.address, f.operator.address, 0, e2eBurnLimit))
}

// deployV2 deploys vault2 and registers its escrow on the next nonce (SPEC
// A7), then STSettlement2's implementation and proxy, and fixes vault2's
// coordinator.
func (f *e2eFixture) deployV2() {
	f.t.Helper()
	vault2 := e2eCatalogArtifact(f.t, "SettlementVault2")
	settlement := e2eCatalogArtifact(f.t, "Settlement2")
	f.vault2ABI, f.settlementABI = vault2.abi, settlement.abi
	f.vault2Coldkey = ss58.EvmMirrorPubkey(crypto.CreateAddress(f.deployer.address, f.chain.nonce(f.deployer.address)))
	f.vault2 = f.deploy(f.deployer, vault2, nil, uint16(e2eNetuid), f.escrowV2, f.vault2Coldkey, f.deployer.address, e2eRao(e2eDemandFeeWei))
	f.must(f.deployer, f.vault2, e2eWei(1), e2eVault2Binding.PackRegisterEscrow(e2eBurnLimit))
	implementation := f.deploy(f.deployer, settlement, nil)
	initialize := e2eSettlementBinding.PackInitialize(f.owner.address, f.coordinator, f.vault2, []common.Address{f.auditor.address}, common.Big1)
	f.settlement = f.deploy(f.deployer, e2eCatalogArtifact(f.t, "ERC1967Proxy"), nil, implementation, initialize)
	f.must(f.deployer, f.vault2, nil, e2eVault2Binding.PackSetCoordinatorOnce(f.settlement))
}

// registerPool commits pool 1 to v2 from firstEpoch (§12.1 step 5).
func (f *e2eFixture) registerPool() {
	f.t.Helper()
	f.must(f.owner, f.settlement, e2eWei(1), e2eSettlementBinding.PackRegisterOperatorPool(e2eRao(e2eNoID), f.poolV2, e2eBurnLimit, f.firstEpoch))
}

func (f *e2eFixture) end(epoch uint64) uint64 {
	return e2eView(f, f.coordinator, e2eCoordinatorBinding.PackEpochEndBlock(e2eRao(epoch)), e2eCoordinatorBinding.UnpackEpochEndBlock).Uint64()
}

// The vault2 views the end-to-end tests read.
func (f *e2eFixture) pending() stabi.ISTSettlementVault2Pending {
	return e2eView(f, f.vault2, e2eVault2Binding.PackPending(e2eRao(e2eNoID)), e2eVault2Binding.UnpackPending)
}

func (f *e2eFixture) rootCount() uint64 {
	return e2eView(f, f.vault2, e2eVault2Binding.PackRootCount(e2eRao(e2eNoID)), e2eVault2Binding.UnpackRootCount).Uint64()
}

func (f *e2eFixture) root(index uint64) stabi.ISTSettlementVault2Root {
	return e2eView(f, f.vault2, e2eVault2Binding.PackRoots(e2eRao(e2eNoID), e2eRao(index)), e2eVault2Binding.UnpackRoots)
}

func (f *e2eFixture) fundedThrough(epoch uint64) *big.Int {
	return e2eView(f, f.vault2, e2eVault2Binding.PackFundedThrough(e2eRao(e2eNoID), e2eRao(epoch)), e2eVault2Binding.UnpackFundedThrough)
}

func (f *e2eFixture) claimed(index uint64) *big.Int {
	return e2eView(f, f.vault2, e2eVault2Binding.PackClaimed(e2eRao(e2eNoID), e2eRao(index)), e2eVault2Binding.UnpackClaimed)
}

func (f *e2eFixture) vault2Counter(pack func() []byte) *big.Int {
	return e2eView(f, f.vault2, pack(), func(out []byte) (*big.Int, error) { return new(big.Int).SetBytes(out), nil })
}

// requireConservation checks G1 on vault2 and the per-pool bounds.
func (f *e2eFixture) requireConservation() {
	f.t.Helper()
	if !e2eView(f, f.vault2, e2eVault2Binding.PackConservationHolds(), e2eVault2Binding.UnpackConservationHolds) {
		f.t.Fatal("vault2 conservation does not hold")
	}
	captured, paid, accounted := f.vault2Counter(e2eVault2Binding.PackTotalCaptured), f.vault2Counter(e2eVault2Binding.PackTotalPaid), f.vault2Counter(e2eVault2Binding.PackEscrowAccounted)
	poolCaptured := e2eView(f, f.vault2, e2eVault2Binding.PackCapturedTotal(e2eRao(e2eNoID)), e2eVault2Binding.UnpackCapturedTotal)
	poolClaimed := e2eView(f, f.vault2, e2eVault2Binding.PackClaimedTotal(e2eRao(e2eNoID)), e2eVault2Binding.UnpackClaimedTotal)
	finalized := e2eView(f, f.vault2, e2eVault2Binding.PackFinalizedTotal(e2eRao(e2eNoID)), e2eVault2Binding.UnpackFinalizedTotal)
	if new(big.Int).Add(paid, accounted).Cmp(captured) != 0 || new(big.Int).Sub(poolCaptured, poolClaimed).Cmp(accounted) != 0 ||
		poolClaimed.Cmp(finalized) > 0 || finalized.Cmp(poolCaptured) > 0 || f.chain.stake(f.escrowV2, f.vault2Coldkey).Cmp(accounted) < 0 {
		f.t.Fatalf("G1: captured %s paid %s accounted %s; pool captured %s claimed %s finalized %s", captured, paid, accounted, poolCaptured, poolClaimed, finalized)
	}
}

// e2eLog is an expected event: its emitter, name and every argument.
type e2eLog struct {
	address common.Address
	abi     *abi.ABI
	name    string
	args    []any
}

// encode packs the event's topics and data exactly as the EVM logs it.
func (self e2eLog) encode(t testing.TB) ([]common.Hash, []byte) {
	t.Helper()
	event, ok := self.abi.Events[self.name]
	if !ok || len(event.Inputs) != len(self.args) {
		t.Fatalf("event %s takes %d arguments", self.name, len(event.Inputs))
	}
	topics := []common.Hash{event.ID}
	var values []any
	for k, input := range event.Inputs {
		if !input.Indexed {
			values = append(values, self.args[k])
			continue
		}
		word, err := abi.Arguments{{Type: input.Type}}.Pack(self.args[k])
		if err != nil {
			t.Fatalf("event %s topic %s: %v", self.name, input.Name, err)
		}
		topics = append(topics, common.BytesToHash(word))
	}
	data, err := event.Inputs.NonIndexed().Pack(values...)
	if err != nil {
		t.Fatalf("event %s data: %v", self.name, err)
	}
	return topics, data
}

// requireLogs checks that the receipt logged exactly these events, in order,
// with every field.
func (f *e2eFixture) requireLogs(receipt *types.Receipt, expected ...e2eLog) {
	f.t.Helper()
	if len(receipt.Logs) != len(expected) {
		f.t.Fatalf("tx %s logged %d events, want %d", receipt.TxHash, len(receipt.Logs), len(expected))
	}
	for k, want := range expected {
		topics, data := want.encode(f.t)
		log := receipt.Logs[k]
		if log.Address != want.address || !slices.Equal(log.Topics, topics) || !bytes.Equal(log.Data, data) {
			f.t.Fatalf("tx %s event %d is not %s%v from %s", receipt.TxHash, k, want.name, want.args, want.address)
		}
	}
}

func (f *e2eFixture) vault2Log(name string, args ...any) e2eLog {
	return e2eLog{address: f.vault2, abi: &f.vault2ABI, name: name, args: args}
}

func (f *e2eFixture) settlementLog(name string, args ...any) e2eLog {
	return e2eLog{address: f.settlement, abi: &f.settlementABI, name: name, args: args}
}

// emitV2 stakes emission on the v2 pool hotkey, owned by vault2's coldkey.
func (f *e2eFixture) emitV2(amount uint64) {
	f.chain.emit(f.poolV2, f.vault2Coldkey, e2eRao(amount))
}

// closeV2 closes epoch at its end, as anyone may, and checks the capture.
func (f *e2eFixture) closeV2(epoch uint64) *types.Receipt {
	f.t.Helper()
	if end := f.end(epoch); f.chain.next() < end {
		f.chain.rollTo(end)
	}
	observed := f.chain.stake(f.poolV2, f.vault2Coldkey)
	before := e2eView(f, f.vault2, e2eVault2Binding.PackCapturedTotal(e2eRao(e2eNoID)), e2eVault2Binding.UnpackCapturedTotal)
	receipt := f.must(f.anyone, f.settlement, nil, e2eSettlementBinding.PackCloseOperatorEpoch(e2eRao(epoch), e2eRao(e2eNoID)))
	funded := new(big.Int).Add(before, observed)
	if observed.Sign() == 0 {
		f.requireLogs(receipt, f.vault2Log("EmissionCaptured", e2eRao(epoch), e2eRao(e2eNoID), observed, new(big.Int), funded))
	} else {
		f.requireLogs(receipt, f.vault2Log("EmissionCaptured", e2eRao(epoch), e2eRao(e2eNoID), observed, observed, funded))
	}
	return receipt
}

// previous is the latest finalized root and its accounts, nil before the
// pool's first root.
func (f *e2eFixture) previous() *ledger.Previous {
	f.t.Helper()
	count := f.rootCount()
	if count == 0 {
		return nil
	}
	root := f.root(count - 1)
	view := f.publisher.view(root.Root)
	if view == nil {
		f.t.Fatalf("the operator holds no accounts of finalized root 0x%x", root.Root)
	}
	return &ledger.Previous{Root: e2eLedgerRoot(root.Epoch, root.Root, root.Total, root.LeafCount, root.LedgerHash), Accounts: view.accounts}
}

func e2eLedgerRoot(epoch uint64, root [32]byte, total *big.Int, leafCount uint64, ledgerHash [32]byte) ledger.Root {
	return ledger.Root{Epoch: epoch, Root: root, Total: *uint256.MustFromBig(total), LeafCount: leafCount, LedgerHash: ledgerHash}
}

// build builds the root for epoch over the latest finalized root, with each
// covered epoch's rows (none for an epoch without rows) and vault2's funding.
func (f *e2eFixture) build(epoch uint64) (*ledger.Ledger, error) {
	f.t.Helper()
	previous := f.previous()
	p := f.firstEpoch - 1
	if previous != nil {
		p = previous.Root.Epoch
	}
	in := ledger.BuildInput{NoID: e2eNoID, Epoch: epoch, FirstEpoch: f.firstEpoch, Previous: previous, FundedThrough: map[uint64]uint256.Int{}}
	for k := p + 1; k <= epoch; k++ {
		in.FundedThrough[k] = *uint256.MustFromBig(f.fundedThrough(k))
		in.Epochs = append(in.Epochs, ledger.EpochInput{Epoch: k, Rows: f.rows[k], Chunks: f.chunks[k]})
	}
	built, err := ledger.Build(context.Background(), in)
	if err != nil {
		return nil, err
	}
	for _, allocation := range built.Epochs {
		if f.chunks[allocation.Epoch] == nil {
			f.chunks[allocation.Epoch] = allocation.Providers
		}
	}
	return built, nil
}

// commit commits commitment for epoch as the epoch's root signer, at the
// current block, and checks DistributionCommitted.
func (f *e2eFixture) commit(signer *e2eActor, epoch uint64, commitment ledger.Root) uint64 {
	f.t.Helper()
	before := f.pending()
	receipt := f.must(signer, f.settlement, nil, f.commitData(epoch, commitment))
	after := f.pending()
	f.requireLogs(receipt, f.vault2Log("DistributionCommitted", e2eRao(e2eNoID), after.Seq, epoch, commitment.Root, commitment.Total.ToBig(), commitment.LeafCount, commitment.LedgerHash, before.Seq))
	return after.Seq
}

func (f *e2eFixture) commitData(epoch uint64, commitment ledger.Root) []byte {
	return e2eSettlementBinding.PackCommitDistribution(e2eRao(epoch), e2eRao(e2eNoID), e2eRao(f.rootCount()),
		stabi.ISTSettlementVault2Commitment{Root: commitment.Root, Total: commitment.Total.ToBig(), LeafCount: commitment.LeafCount, LedgerHash: commitment.LedgerHash})
}

// audit runs the validator's ledger audit (§9.2, without head bindings) over
// the published objects, at the head.
func (f *e2eFixture) audit() *ledger.Result {
	f.t.Helper()
	pending := f.pending()
	in := ledger.VerifyInput{NoID: e2eNoID, FirstEpoch: f.firstEpoch, Seq: pending.Seq, FundedThrough: map[uint64]uint256.Int{},
		Pending: e2eLedgerRoot(pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash),
		Fetcher: f.publisher, Custody: ledger.NewMemoryCustody(), MaxFetchBytes: ledger.DefaultMaxFetchBytes}
	p := f.firstEpoch - 1
	if previous := f.previous(); previous != nil {
		in.Previous, p = &previous.Root, previous.Root.Epoch
	}
	for k := p + 1; k <= pending.Epoch; k++ {
		in.FundedThrough[k] = *uint256.MustFromBig(f.fundedThrough(k))
	}
	result, err := ledger.Verify(context.Background(), in)
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}

// approve audits the pending root and approves it as the auditor.
func (f *e2eFixture) approve() {
	f.t.Helper()
	if result := f.audit(); result.Action() != ledger.ActionApprove {
		f.t.Fatalf("the auditor refuses the pending root: %v (%v check: %v)", result.Action(), result.Check, result.Err)
	}
	f.approveBlind(f.auditor)
}

// approveBlind approves the pending root without an audit, as a bypassed
// admission does, and checks DistributionApproved.
func (f *e2eFixture) approveBlind(auditor *e2eActor) {
	f.t.Helper()
	pending := f.pending()
	receipt := f.must(auditor, f.settlement, nil, e2eSettlementBinding.PackApproveDistribution(e2eRao(e2eNoID), pending.Seq, pending.Root, pending.LedgerHash))
	f.requireLogs(receipt, f.settlementLog("DistributionApproved", e2eRao(e2eNoID), pending.Seq, auditor.address, uint64(1), common.Big1))
}

// finalize lets the pending root's window pass and finalizes it, as anyone
// may, checking DistributionFinalized.
func (f *e2eFixture) finalize() uint64 {
	f.t.Helper()
	pending := f.pending()
	if at := pending.CommittedBlock + e2eWindow; f.chain.next() < at {
		f.chain.rollTo(at)
	}
	index := f.rootCount()
	receipt := f.must(f.anyone, f.settlement, nil, e2eSettlementBinding.PackFinalizeDistribution(e2eRao(e2eNoID)))
	f.requireLogs(receipt, f.vault2Log("DistributionFinalized", e2eRao(e2eNoID), pending.Seq, e2eRao(index), pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash))
	return index
}

// epoch runs one v2 epoch as the server and validator do (§5, §8.1): the
// pool earns emission, the operator closes the epoch, finalizes nothing
// pending, builds the root over the latest finalized one, publishes its
// chunks then its header, and commits; the auditor approves; the root
// finalizes after its window. It returns the root.
func (f *e2eFixture) epoch(epoch, emission uint64, rows []protocol.LedgerUsageRow) *ledger.Ledger {
	f.t.Helper()
	f.rows[epoch] = rows
	f.emitV2(emission)
	f.closeV2(epoch)
	built, err := f.build(epoch)
	if err != nil {
		f.t.Fatalf("epoch %d: %v", epoch, err)
	}
	f.publisher.publish(built.Objects, built.Accounts, built.Tree)
	f.commit(f.operator, epoch, built.Commit)
	f.approve()
	f.finalize()
	return built
}

// forge is an attacker's successor to built: its header lists account chunks
// of accounts instead of the honest ones, over the same covered epochs. It
// returns the commitment of accounts' tree, the objects to publish and the
// tree.
func (f *e2eFixture) forge(built *ledger.Ledger, accounts []merkle.LedgerAccount) (ledger.Root, ledger.Objects, *merkle.LedgerSumTree) {
	f.t.Helper()
	header, err := ledger.DecodeHeader(built.Header)
	if err != nil {
		f.t.Fatal(err)
	}
	chunks, err := ledger.EncodeAccountChunks(accounts)
	if err != nil {
		f.t.Fatal(err)
	}
	objects := ledger.Objects{}
	honest := header.Accounts
	header.Accounts = nil
	for _, chunk := range chunks {
		header.Accounts = append(header.Accounts, ledger.RefOf(chunk))
		objects[ledger.RefOf(chunk)] = chunk
	}
	data, err := ledger.EncodeHeader(header)
	if err != nil {
		f.t.Fatal(err)
	}
	objects[ledger.RefOf(data)] = data
	// The covered epochs' provider chunks are the honest ones.
	for ref, object := range built.Objects {
		if _, present := objects[ref]; !present && ref != ledger.Ref(built.Commit.LedgerHash) && !slices.Contains(honest, ref) {
			objects[ref] = object
		}
	}
	tree, err := merkle.BuildLedgerSumTree(context.Background(), accounts)
	if err != nil {
		f.t.Fatal(err)
	}
	root, total := tree.Root()
	return ledger.Root{Epoch: built.Commit.Epoch, Root: root, Total: total, LeafCount: tree.LeafCount(), LedgerHash: ledger.RefOf(data)}, objects, tree
}

// e2eTreeView is a published tree with its accounts.
type e2eTreeView struct {
	accounts []merkle.LedgerAccount
	tree     *merkle.LedgerSumTree
}

// e2ePublisher is the operator's GET /sn/ledger/object and GET
// /sn/ledger/account (§8.3; SPEC "Ledger objects and endpoints"). It serves
// the objects published and, for vault2's latest finalized root and pending
// root at the finalized block, the accounts of the trees it published.
type e2ePublisher struct {
	t       testing.TB
	f       *e2eFixture
	lock    sync.Mutex
	objects map[ledger.Ref][]byte
	views   map[[32]byte]*e2eTreeView
	server  *httptest.Server
}

func newE2EPublisher(t testing.TB, f *e2eFixture) *e2ePublisher {
	publisher := &e2ePublisher{t: t, f: f, objects: map[ledger.Ref][]byte{}, views: map[[32]byte]*e2eTreeView{}}
	publisher.server = httptest.NewServer(publisher)
	t.Cleanup(publisher.server.Close)
	return publisher
}

// publish writes objects put-if-absent and records the tree's accounts.
func (self *e2ePublisher) publish(objects ledger.Objects, accounts []merkle.LedgerAccount, tree *merkle.LedgerSumTree) {
	self.lock.Lock()
	defer self.lock.Unlock()
	for ref, data := range objects {
		if _, present := self.objects[ref]; !present {
			self.objects[ref] = data
		}
	}
	if tree != nil {
		root, _ := tree.Root()
		self.views[root] = &e2eTreeView{accounts: accounts, tree: tree}
	}
}

// withhold removes an object from the endpoint.
func (self *e2ePublisher) withhold(ref ledger.Ref) {
	self.lock.Lock()
	defer self.lock.Unlock()
	delete(self.objects, ref)
}

func (self *e2ePublisher) view(root [32]byte) *e2eTreeView {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.views[root]
}

// Fetch serves the auditor the published objects, as GET /sn/ledger/object.
func (self *e2ePublisher) Fetch(ctx context.Context, ref ledger.Ref) (io.ReadCloser, error) {
	self.lock.Lock()
	defer self.lock.Unlock()
	data, ok := self.objects[ref]
	if !ok {
		return nil, fmt.Errorf("%w: %v", ledger.ErrObjectNotFound, ref)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (self *e2ePublisher) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch request.URL.Path {
	case "/sn/ledger/object":
		ref, err := ledger.ParseRef(request.URL.Query().Get("hash"))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		self.lock.Lock()
		data, ok := self.objects[ref]
		self.lock.Unlock()
		if !ok {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write(data)
	case "/sn/ledger/account":
		self.serveAccount(writer, request)
	default:
		http.NotFound(writer, request)
	}
}

// e2eAccountEntry is one pool's entry of GET /sn/ledger/account.
type e2eAccountEntry struct {
	Vault             string           `json:"vault"`
	NoID              uint64           `json:"no_id"`
	Index             uint64           `json:"index"`
	RootIndex         *uint64          `json:"root_index"`
	Cumulative        string           `json:"cumulative"`
	Proof             []map[string]any `json:"proof"`
	Claimed           string           `json:"claimed"`
	PendingCumulative *string          `json:"pending_cumulative"`
}

func (self *e2ePublisher) serveAccount(writer http.ResponseWriter, request *http.Request) {
	text := request.URL.Query().Get("coldkey")
	var coldkey [32]byte
	if raw, err := hex.DecodeString(strings.TrimPrefix(text, "0x")); strings.HasPrefix(text, "0x") && err == nil && len(raw) == 32 {
		copy(coldkey[:], raw)
	} else if decoded, err := ss58.DecodeWithPrefix(text, ss58.BittensorPrefix); err == nil {
		coldkey = decoded
	} else {
		http.Error(writer, "malformed coldkey", http.StatusBadRequest)
		return
	}
	f := self.f
	accounts := []e2eAccountEntry{}
	find := func(view *e2eTreeView) (uint64, bool) {
		if view == nil {
			return 0, false
		}
		for index, account := range view.accounts {
			if account.Coldkey == coldkey {
				return uint64(index), true
			}
		}
		return 0, false
	}
	// The handler runs on the server's goroutine, so chain reads report
	// errors instead of failing the test there.
	read := func(data []byte) []byte {
		out, err := f.chain.call(f.vault2, data)
		if err != nil {
			self.t.Errorf("ledger account endpoint read 0x%x: %v", data[:4], err)
		}
		return out
	}
	var finalized, pending *e2eTreeView
	var rootIndex uint64
	if count := new(big.Int).SetBytes(read(e2eVault2Binding.PackRootCount(e2eRao(e2eNoID)))).Uint64(); count > 0 {
		root, err := e2eVault2Binding.UnpackRoots(read(e2eVault2Binding.PackRoots(e2eRao(e2eNoID), e2eRao(count-1))))
		if err != nil {
			self.t.Error(err)
		}
		rootIndex, finalized = count-1, self.view(root.Root)
	}
	if next, err := e2eVault2Binding.UnpackPending(read(e2eVault2Binding.PackPending(e2eRao(e2eNoID)))); err != nil {
		self.t.Error(err)
	} else if next.Seq != 0 {
		pending = self.view(next.Root)
	}
	finalizedIndex, inFinalized := find(finalized)
	pendingIndex, inPending := find(pending)
	if inFinalized || inPending {
		entry := e2eAccountEntry{Vault: strings.ToLower(f.vault2.Hex()), NoID: e2eNoID, Cumulative: "0"}
		if inFinalized {
			proof, err := finalized.tree.Proof(finalizedIndex)
			if err != nil {
				self.t.Error(err)
			}
			entry.Index, entry.RootIndex, entry.Cumulative, entry.Proof = finalizedIndex, &rootIndex, finalized.accounts[finalizedIndex].Cumulative.Dec(), []map[string]any{}
			for _, node := range proof {
				entry.Proof = append(entry.Proof, map[string]any{"hash": fmt.Sprintf("0x%x", node.Hash), "sum": node.Sum.Dec()})
			}
		} else {
			entry.Index = pendingIndex
		}
		if inPending && (!inFinalized || pendingIndex == finalizedIndex) {
			value := pending.accounts[pendingIndex].Cumulative.Dec()
			entry.PendingCumulative = &value
		}
		entry.Claimed = new(big.Int).SetBytes(read(e2eVault2Binding.PackClaimed(e2eRao(e2eNoID), e2eRao(entry.Index)))).String()
		accounts = append(accounts, entry)
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"coldkey": fmt.Sprintf("0x%x", coldkey), "accounts": accounts})
}

// snclaim runs the real snclaim command against the fixture's RPC and ledger
// endpoints in a child process, and returns its stdout, stderr and exit code.
func (f *e2eFixture) snclaim(args ...string) (string, string, int) {
	f.t.Helper()
	executable, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	home := f.t.TempDir()
	command := &exec.Cmd{Path: executable, Args: append([]string{"snclaim"}, args...), Env: []string{"HOME=" + home, "TMPDIR=" + home, "PATH=" + os.Getenv("PATH")}}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		f.t.Fatal(err)
	}
	return stdout.String(), stderr.String(), command.ProcessState.ExitCode()
}

// snclaimArgs are the endpoint flags every v2 command takes.
func (f *e2eFixture) snclaimArgs(args ...string) []string {
	return append(args, "--vault="+f.vault2.Hex(), "--rpc="+f.chain.server.URL, "--api_url="+f.publisher.server.URL, fmt.Sprintf("--chain_id=%d", e2eChainID))
}

// e2eProof converts a tree's proof of index to vault2's argument types.
func e2eProof(t testing.TB, tree *merkle.LedgerSumTree, accounts []merkle.LedgerAccount, index uint64) (stabi.LedgerTreeAccount, []stabi.LedgerTreeProofNode) {
	t.Helper()
	proof, err := tree.Proof(index)
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]stabi.LedgerTreeProofNode, len(proof))
	for k := range proof {
		nodes[k] = stabi.LedgerTreeProofNode{Hash: proof[k].Hash, Sum: proof[k].Sum.ToBig()}
	}
	return stabi.LedgerTreeAccount{Index: e2eRao(index), Coldkey: accounts[index].Coldkey, Cumulative: accounts[index].Cumulative.ToBig()}, nodes
}

func e2ePath(path []merkle.LedgerPathNode) []stabi.LedgerTreePathNode {
	nodes := make([]stabi.LedgerTreePathNode, len(path))
	for k := range path {
		nodes[k] = stabi.LedgerTreePathNode{LeftHash: path[k].LeftHash, LeftSum: path[k].LeftSum.ToBig(), RightHash: path[k].RightHash, RightSum: path[k].RightSum.ToBig()}
	}
	return nodes
}

// accountOf returns the index of coldkey's account in accounts.
func e2eAccountOf(t testing.TB, accounts []merkle.LedgerAccount, coldkey [32]byte) uint64 {
	t.Helper()
	for index, account := range accounts {
		if account.Coldkey == coldkey {
			return uint64(index)
		}
	}
	t.Fatalf("no account of coldkey 0x%x", coldkey)
	return 0
}

// TestSettlementV2E2EHarness pins the fake runtime to the R battery's
// measured facts (scripts/runtime-rehearsal/README.md), through the real
// vault2: a move takes exactly the source amount and credits the destination
// in full or, at a share price above 1, one rao short; a move needs a gas
// limit covering its declared weight; the floor; the collateral lock; locked
// alpha; registration. The harness behind E-1 to E-7.
func TestSettlementV2E2EHarness(t *testing.T) {
	f := newE2EFixture(t, e2eDefaultFirst)
	// Registration: vault2 registered its escrow and pool hotkeys under its
	// coldkey, paying the burn from its balance and refunding the rest.
	for _, hotkey := range [][32]byte{f.escrowV2, f.poolV2} {
		var owner common.Hash
		var registered bool
		f.chain.do(func() {
			f.chain.runtime.db = f.chain.stateLocked(f.chain.head)
			owner = f.chain.runtime.db.GetState(e2eNeuron, e2eRuntimeSlot("owner", hotkey))
			_, registered = f.chain.runtime.uid(hotkey)
		})
		if !registered || owner != common.Hash(f.vault2Coldkey) {
			t.Fatalf("hotkey 0x%x registered %t to 0x%x", hotkey, registered, owner)
		}
	}
	if balance := f.chain.balance(f.vault2); balance.Sign() != 0 {
		t.Fatalf("vault2 kept %s wei of registration value", balance)
	}
	// Capture moves exactly the pool's stake; with the shortfall knob escrow
	// gains one rao less, and vault2 accounts what it gained.
	f.emitV2(5_000_000_000)
	f.chain.do(func() { f.chain.runtime.shortfall = true })
	// eth_estimateGas runs at the head, the block before the transaction's,
	// so the head is inside the close window too.
	f.chain.rollTo(f.end(f.firstEpoch) + 1)
	closing := e2eSettlementBinding.PackCloseOperatorEpoch(e2eRao(f.firstEpoch), e2eRao(e2eNoID))
	// The move is admitted only with its declared weight forwarded: the
	// close's estimate is within 2% of the limit R-6 measured for a capturing
	// close, 2,634,202, and a close with a 1M limit runs out of gas in the
	// precompile and records nothing.
	var estimate uint64
	var err error
	f.chain.do(func() { estimate, err = f.chain.estimateLocked(f.anyone.address, &f.settlement, nil, closing) })
	starved, signErr := types.SignNewTx(f.anyone.key, f.chain.signer, &types.LegacyTx{Nonce: f.chain.nonce(f.anyone.address), GasPrice: big.NewInt(e2eGasPrice), Gas: 1_000_000, To: &f.settlement, Data: closing})
	if err != nil || signErr != nil || estimate < 2_634_202*98/100 || estimate > 2_634_202*102/100 {
		t.Fatalf("close estimate %d: %v %v", estimate, err, signErr)
	}
	if receipt := f.chain.include(starved); receipt.Status != types.ReceiptStatusFailed || f.fundedThrough(f.firstEpoch).Sign() != 0 || f.chain.stake(f.poolV2, f.vault2Coldkey).Uint64() != 5_000_000_000 {
		t.Fatal("a close below the move's declared weight was not refused whole")
	}
	receipt := f.must(f.anyone, f.settlement, nil, closing)
	f.requireLogs(receipt, f.vault2Log("EmissionCaptured", e2eRao(f.firstEpoch), e2eRao(e2eNoID), e2eRao(5_000_000_000), e2eRao(4_999_999_999), e2eRao(4_999_999_999)))
	if f.chain.stake(f.poolV2, f.vault2Coldkey).Sign() != 0 || f.chain.stake(f.escrowV2, f.vault2Coldkey).Uint64() != 4_999_999_999 || receipt.GasUsed > estimate/5 {
		t.Fatalf("capture deltas differ from the runtime's, or it used %d gas of its %d estimate", receipt.GasUsed, estimate)
	}
	f.chain.do(func() { f.chain.runtime.shortfall = false })
	f.requireConservation()
	// Below the floor at a low price the runtime refuses the move, and the
	// close reverts whole: nothing is recorded.
	f.chain.native(func(rt *e2eRuntime) {
		rt.setWord(e2eAlpha, e2eRuntimeSlot("price"), big.NewInt(1e12))
		rt.setStake(f.poolV2, f.vault2Coldkey, big.NewInt(99_999_999))
	})
	f.chain.rollTo(f.end(f.firstEpoch + 1))
	failed, _ := f.chain.send(f.anyone.key, &f.settlement, nil, e2eSettlementBinding.PackCloseOperatorEpoch(e2eRao(f.firstEpoch+1), e2eRao(e2eNoID)))
	if failed.Status != types.ReceiptStatusFailed || f.fundedThrough(f.firstEpoch+1).Sign() != 0 || f.chain.stake(f.poolV2, f.vault2Coldkey).Uint64() != 99_999_999 {
		t.Fatal("a capture below the floor was not refused whole")
	}
	// Collateral locked on the pool position is never moved; the rest is.
	f.chain.native(func(rt *e2eRuntime) {
		rt.setWord(e2eAlpha, e2eRuntimeSlot("price"), big.NewInt(1e18))
		rt.setWord(e2eStaking, e2eRuntimeSlot("collateral", f.poolV2, f.vault2Coldkey), big.NewInt(9_999_999))
	})
	receipt = f.must(f.anyone, f.settlement, nil, e2eSettlementBinding.PackCloseOperatorEpoch(e2eRao(f.firstEpoch+1), e2eRao(e2eNoID)))
	f.requireLogs(receipt, f.vault2Log("EmissionCaptured", e2eRao(f.firstEpoch+1), e2eRao(e2eNoID), e2eRao(90_000_000), e2eRao(90_000_000), e2eRao(5_089_999_999)))
	if f.chain.stake(f.poolV2, f.vault2Coldkey).Uint64() != 9_999_999 {
		t.Fatal("capture dug into the collateral lock")
	}
	// A third party cannot push conviction-locked alpha onto vault2's
	// coldkey; its unlocked part moves.
	third := newE2EActor("third-party")
	thirdColdkey := ss58.EvmMirrorPubkey(third.address)
	f.chain.native(func(rt *e2eRuntime) {
		rt.setStake(f.escrowV2, thirdColdkey, big.NewInt(2_000_000_000))
		rt.setWord(e2eStaking, e2eRuntimeSlot("conviction", f.escrowV2, thirdColdkey), big.NewInt(1_000_000_000))
		rt.db.SetBalance(third.address, uint256.MustFromBig(e2eWei(10)), tracing.BalanceChangeUnspecified)
	})
	transfer := func(amount uint64) []byte {
		data, err := e2eRuntimeABI.Pack("transferStake", f.vault2Coldkey, f.escrowV2, big.NewInt(e2eNetuid), big.NewInt(e2eNetuid), e2eRao(amount))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	staking := e2eStaking
	if refused, _ := f.chain.send(third.key, &staking, nil, transfer(1_500_000_000)); refused.Status != types.ReceiptStatusFailed {
		t.Fatal("locked alpha reached vault2's coldkey")
	}
	if moved, _ := f.chain.send(third.key, &staking, nil, transfer(1_000_000_000)); moved.Status != types.ReceiptStatusSuccessful ||
		f.chain.stake(f.escrowV2, thirdColdkey).Uint64() != 1_000_000_000 || f.chain.stake(f.escrowV2, f.vault2Coldkey).Uint64() != 5_089_999_999+1_000_000_000 {
		t.Fatal("the unlocked part did not move exactly")
	}
	f.requireConservation()
}
