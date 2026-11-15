package commitmentoracle

// Address-only dry runs reconcile real retained transaction files through the
// normal cycle, without broadcasting or changing their durable custody.

import (
	"bytes"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// All identities, transaction bytes and chain state are synthetic. The oracle
// receives no key; only fixture construction signs the retained test record.
type dryRunReconcileFixture struct {
	harness     *testHarness
	transaction *types.Transaction
	recordPath  string
	recordBytes []byte
}

// Construct a valid pending record before starting the address-only owner.
func newDryRunReconcileFixture(t *testing.T) dryRunReconcileFixture {
	t.Helper()
	settings := testSettings(t.TempDir())
	settings.ChainId = 945
	settings.Netuid = 7
	settings.Coordinator = common.Address{19: 0x42}
	settings.DryRun = true
	key := testOracleKey(t)
	address := keyAddress(key)
	evm := newFakeEvm(t, settings.Coordinator, address, 500)
	evm.chain, evm.netuid = settings.ChainId, settings.Netuid
	native := newFakeNative(nativeHead{number: 500, hash: [32]byte{0x55}})
	clock := &testClock{now: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)}
	harness := newTestHarness(t, native, evm, settings, nil, address, clock, &testLog{})
	args := mirrorArgs{hotkey: [32]byte{0x12}, commitmentHash: [32]byte{0x34}, finalizedBlock: 450, finalizedBlockHash: [32]byte{0x56}}
	transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(5 * 1000 * 1000 * 1000), Gas: 100000,
		To: &settings.Coordinator, Value: new(big.Int), Data: mirrorCalldata(args),
	}), types.LatestSignerForChainID(new(big.Int).SetUint64(settings.ChainId)), key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	path := harness.oracle.sender.recordPath(args)
	if err := writeSendRecord(path, sendRecord{
		Schema: sendRecordSchema, Hotkey: hexutil.Encode(args.hotkey[:]),
		CommitmentHash: hexutil.Encode(args.commitmentHash[:]), FinalizedBlock: args.finalizedBlock,
		FinalizedBlockHash: hexutil.Encode(args.finalizedBlockHash[:]), Nonce: transaction.Nonce(),
		TransactionHash: transaction.Hash().Hex(), Transaction: hexutil.Encode(raw), SignedUnix: clock.now.Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.oracle.admit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return dryRunReconcileFixture{harness: harness, transaction: transaction, recordPath: path, recordBytes: before}
}

// Assert both the network boundary and the original record's bytes/namespace.
func assertDryRunReconcileReadOnly(t *testing.T, fixture dryRunReconcileFixture) {
	t.Helper()
	if len(fixture.harness.evm.broadcasts) != 0 {
		t.Fatalf("dry run broadcast %d retained transactions", len(fixture.harness.evm.broadcasts))
	}
	after, err := os.ReadFile(fixture.recordPath)
	if err != nil {
		t.Fatalf("dry run moved or removed the retained record: %v", err)
	}
	if !bytes.Equal(after, fixture.recordBytes) {
		t.Fatal("dry run rewrote the retained record")
	}
	entries, err := os.ReadDir(filepath.Dir(fixture.recordPath))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(fixture.recordPath) {
		t.Fatalf("dry run changed retained record namespace: %v, %v", entries, err)
	}
	if fixture.harness.oracle.sender.key != nil || fixture.harness.oracle.metrics.mirrorsSent != 0 {
		t.Fatal("address-only dry run acquired signing authority or counted a send")
	}
}

// A missing receipt must not turn an address-only dry run into a rebroadcast.
func TestDryRunCycleRetainedUnknownTransactionNeverBroadcasts(t *testing.T) {
	fixture := newDryRunReconcileFixture(t)
	if err := fixture.harness.oracle.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertDryRunReconcileReadOnly(t, fixture)
	if len(fixture.harness.evm.mempool) != 0 || fixture.harness.oracle.metrics.inFlight != 1 {
		t.Fatal("dry run did not retain the unresolved transaction as pending")
	}
}

// A nonce consumed by another transaction is observed without renaming custody.
func TestDryRunCycleRetainedConsumedNoncePreservesRecord(t *testing.T) {
	fixture := newDryRunReconcileFixture(t)
	evm := fixture.harness.evm
	foreign, err := types.SignTx(types.NewTx(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(6 * 1000 * 1000 * 1000), Gas: 100000,
		To: fixture.transaction.To(), Value: new(big.Int), Data: fixture.transaction.Data(),
	}), types.LatestSignerForChainID(new(big.Int).SetUint64(evm.chain)), testOracleKey(t))
	if err != nil {
		t.Fatal(err)
	}
	evm.mempool = append(evm.mempool, foreign)
	evm.mine()
	evm.finalize()
	if err := fixture.harness.oracle.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertDryRunReconcileReadOnly(t, fixture)
	if fixture.harness.oracle.metrics.inFlight != 0 || !fixture.harness.log.contains("was consumed by another transaction") {
		t.Fatal("dry run lost the consumed-nonce observation")
	}
}

// A finalized revert is still reported while the original file stays untouched.
func TestDryRunCycleRetainedRevertedReceiptPreservesRecord(t *testing.T) {
	fixture := newDryRunReconcileFixture(t)
	evm := fixture.harness.evm
	evm.mempool = append(evm.mempool, fixture.transaction)
	evm.paused = true
	evm.mine()
	evm.finalize()
	evm.paused = false
	if err := fixture.harness.oracle.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertDryRunReconcileReadOnly(t, fixture)
	if fixture.harness.oracle.metrics.inFlight != 0 || !fixture.harness.log.contains("reverted in finalized block") {
		t.Fatal("dry run lost the finalized revert observation")
	}
}

// Finalized success verifies the normal coordinator readback without rewriting.
func TestDryRunCycleRetainedFinalizedReceiptPreservesRecord(t *testing.T) {
	fixture := newDryRunReconcileFixture(t)
	evm := fixture.harness.evm
	evm.mempool = append(evm.mempool, fixture.transaction)
	evm.mine()
	evm.finalize()
	if err := fixture.harness.oracle.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertDryRunReconcileReadOnly(t, fixture)
	if fixture.harness.oracle.metrics.inFlight != 0 || fixture.harness.oracle.metrics.lastMirror == 0 {
		t.Fatal("dry run lost the finalized success observation")
	}
}

// An unfinalized receipt remains pending; a dry run cannot promote it to done.
func TestDryRunCycleRetainedUnfinalizedReceiptStaysPending(t *testing.T) {
	fixture := newDryRunReconcileFixture(t)
	evm := fixture.harness.evm
	evm.mempool = append(evm.mempool, fixture.transaction)
	evm.mine()
	if err := fixture.harness.oracle.cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertDryRunReconcileReadOnly(t, fixture)
	if fixture.harness.oracle.metrics.inFlight != 1 || fixture.harness.oracle.metrics.lastMirror != 0 {
		t.Fatal("dry run treated an unfinalized receipt as complete")
	}
}
