package commitmentoracle

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func newGoldenHarness(t *testing.T, settings Settings, dryRun bool) *testHarness {
	key := testOracleKey(t)
	address := keyAddress(key)
	evm := newFakeEvm(t, settings.Coordinator, address, goldenFinalized)
	settings.DryRun = dryRun
	return newTestHarness(t, goldenNative(t), evm, settings, key, address, &testClock{now: time.Unix(1760000000, 0)}, &testLog{})
}

// The golden commitment is mirrored with exactly the runbook's arguments, and
// only after its receipt is final.
func TestCycleMirrorsGoldenCommitment(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	// Each confirmation poll mines a block and finalizes it.
	harness.oracle.sleep = func(ctx context.Context, duration time.Duration) error {
		harness.clock.now = harness.clock.now.Add(duration)
		harness.evm.mine()
		harness.evm.finalize()
		return nil
	}
	if err := harness.oracle.admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.mined) != 1 || !harness.evm.mined[0].success {
		t.Fatalf("mined %d transactions", len(harness.evm.mined))
	}
	transaction := harness.evm.mined[0].transaction
	if string(transaction.Data()) != string(goldenCalldata(t)) {
		t.Fatalf("calldata 0x%x, want 0x%x", transaction.Data(), goldenCalldata(t))
	}
	if *transaction.To() != harness.oracle.settings.Coordinator || transaction.Nonce() != 0 || transaction.Gas() != 108000 || transaction.GasPrice().Cmp(big.NewInt(5*1000*1000*1000)) != 0 || transaction.ChainId().Uint64() != 964 {
		t.Fatalf("transaction to %s nonce %d gas %d price %s chain %s", transaction.To(), transaction.Nonce(), transaction.Gas(), transaction.GasPrice(), transaction.ChainId())
	}
	record := harness.evm.recordAt(mustHash(t, goldenHotkeyHex), harness.evm.finalized)
	if record.commitmentHash != mustHash(t, goldenCommitmentHex) || record.finalizedBlock != goldenBlock || record.finalizedBlockHash != mustHash(t, goldenBlockHashHex) {
		t.Fatalf("coordinator record %+v", record)
	}
	metrics := harness.oracle.metrics
	if metrics.pending != 0 || metrics.mirrorsSent != 1 || metrics.lastMirror == 0 || metrics.lastSuccess == 0 || metrics.inFlight != 0 {
		t.Fatalf("metrics pending %d sent %d last mirror %v last success %v in flight %v", metrics.pending, metrics.mirrorsSent, metrics.lastMirror, metrics.lastSuccess, metrics.inFlight)
	}
	// The next cycle finds it mirrored and sends nothing.
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.distinctBroadcasts()) != 1 || harness.oracle.metrics.mirrored != 1 {
		t.Fatalf("broadcasts %d mirrored %d", len(harness.evm.distinctBroadcasts()), harness.oracle.metrics.mirrored)
	}
}

func TestCycleSkipsAlreadyMirrored(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	harness.evm.setRecord(mustHash(t, goldenHotkeyHex), mirroredRecord{commitmentHash: mustHash(t, goldenCommitmentHex), finalizedBlockHash: mustHash(t, goldenBlockHashHex), finalizedBlock: goldenBlock}, goldenFinalized-10)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.broadcasts) != 0 {
		t.Fatalf("broadcast %d transactions for a mirrored commitment", len(harness.evm.broadcasts))
	}
	if harness.oracle.metrics.mirrored != 1 || harness.oracle.metrics.pending != 0 || harness.oracle.metrics.fleet != 1 {
		t.Fatalf("mirrored %d pending %d fleet %d", harness.oracle.metrics.mirrored, harness.oracle.metrics.pending, harness.oracle.metrics.fleet)
	}
}

// A mirror written at an unfinalized EVM block is not yet trusted: the oracle
// reads the coordinator at the finalized block and still counts it pending.
func TestCycleReadsCoordinatorAtFinalizedBlock(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), true)
	harness.evm.latest = goldenFinalized + 3
	harness.evm.setRecord(mustHash(t, goldenHotkeyHex), mirroredRecord{commitmentHash: mustHash(t, goldenCommitmentHex), finalizedBlockHash: mustHash(t, goldenBlockHashHex), finalizedBlock: goldenBlock}, goldenFinalized+2)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if harness.oracle.metrics.mirrored != 0 || harness.oracle.metrics.pending != 1 {
		t.Fatalf("mirrored %d pending %d", harness.oracle.metrics.mirrored, harness.oracle.metrics.pending)
	}
}

func TestAdmitRefusesWhenNotActiveOracle(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	harness.evm.activeOracle = common.HexToAddress("0x000000000000000000000000000000000000beef")
	err := harness.oracle.admit(context.Background())
	if !errors.Is(err, errNotActiveOracle) {
		t.Fatalf("admit: %v", err)
	}
	if harness.oracle.metrics.oracleActive != 0 {
		t.Fatal("oracle_active is set")
	}
	// A dry run is refused the same way.
	dry := newGoldenHarness(t, testSettings(t.TempDir()), true)
	dry.evm.activeOracle = common.HexToAddress("0x000000000000000000000000000000000000beef")
	if err := dry.oracle.admit(context.Background()); !errors.Is(err, errNotActiveOracle) {
		t.Fatalf("dry run admit: %v", err)
	}
}

func TestAdmitRefusesWrongChainOrNetuid(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	harness.evm.chain = 945
	if err := harness.oracle.admit(context.Background()); err == nil || !strings.Contains(err.Error(), "chain id") {
		t.Fatalf("wrong chain: %v", err)
	}
	harness.evm.chain = 964
	harness.evm.netuid = 26
	if err := harness.oracle.admit(context.Background()); err == nil || !strings.Contains(err.Error(), "netuid") {
		t.Fatalf("wrong netuid: %v", err)
	}
}

// An oracle rotation that takes effect while running stops every send.
func TestCycleStopsWhenOracleRotatesAway(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	if err := harness.oracle.admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	harness.evm.activeOracle = common.HexToAddress("0x000000000000000000000000000000000000beef")
	if err := harness.oracle.cycle(context.Background()); !errors.Is(err, errNotActiveOracle) {
		t.Fatalf("cycle: %v", err)
	}
	if len(harness.evm.broadcasts) != 0 || harness.oracle.metrics.oracleActive != 0 {
		t.Fatalf("broadcasts %d oracle_active %v", len(harness.evm.broadcasts), harness.oracle.metrics.oracleActive)
	}
}

// Only the finalized head's state is read; a newer commitment visible only at
// an unfinalized block is never mirrored.
func TestCycleNeverActsOnUnfinalizedBlocks(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	harness.oracle.sleep = func(ctx context.Context, duration time.Duration) error {
		harness.evm.mine()
		harness.evm.finalize()
		return nil
	}
	unfinalized := [32]byte{0xbe, 0x57}
	hotkey := [32]byte{0x51}
	native := harness.native
	native.block(goldenFinalized+5, unfinalized)
	native.block(goldenFinalized+4, [32]byte{0x44})
	native.put(unfinalized, hotkey, mustHex(t, "0000000000000000"+"b5b08d00"+"04"+"29"+fleetTagHex+strings.Repeat("cd", 32)), mustHex(t, "b5b08d00"))
	// The golden hotkey republished at an unfinalized block, too.
	native.put(unfinalized, mustHash(t, goldenHotkeyHex), mustHex(t, "0000000000000000"+"b5b08d00"+"04"+"29"+fleetTagHex+strings.Repeat("ef", 32)), mustHex(t, "b5b08d00"))
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, hash := range native.stateReads {
		if hash != goldenHeadHash() {
			t.Fatalf("read native state at 0x%x, not the finalized head", hash)
		}
	}
	if len(harness.evm.mined) != 1 || string(harness.evm.mined[0].transaction.Data()) != string(goldenCalldata(t)) {
		t.Fatalf("mined %d transactions", len(harness.evm.mined))
	}
	if record := harness.evm.recordAt(hotkey, harness.evm.latest); record.finalizedBlock != 0 {
		t.Fatal("mirrored an unfinalized commitment")
	}
}

// A sent mirror counts only once its receipt is final; until then nothing
// else is signed.
func TestCycleWaitsForFinalReceipt(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	second := [32]byte{0x52}
	harness.native.block(goldenBlock+10, [32]byte{0x99})
	harness.native.put(goldenHeadHash(), second, mustHex(t, "0000000000000000"+"6b4b8d00"+"04"+"29"+fleetTagHex+strings.Repeat("12", 32)), mustHex(t, "6b4b8d00"))
	// Mined but never finalized during the cycle.
	harness.oracle.sleep = func(ctx context.Context, duration time.Duration) error {
		harness.clock.now = harness.clock.now.Add(duration)
		harness.evm.mine()
		return nil
	}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.distinctBroadcasts()) != 1 || harness.oracle.metrics.inFlight != 1 {
		t.Fatalf("broadcasts %d in flight %v", len(harness.evm.distinctBroadcasts()), harness.oracle.metrics.inFlight)
	}
	// Still not final: the next cycle signs nothing new.
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.distinctBroadcasts()) != 1 {
		t.Fatalf("signed %d transactions while one was in flight", len(harness.evm.distinctBroadcasts()))
	}
	// Finality arrives; the record completes and the second commitment is sent
	// with the next nonce.
	harness.evm.finalize()
	harness.oracle.sleep = func(ctx context.Context, duration time.Duration) error {
		harness.evm.mine()
		harness.evm.finalize()
		return nil
	}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	hashes := harness.evm.distinctBroadcasts()
	if len(hashes) != 2 || len(harness.evm.mined) != 2 || harness.evm.mined[1].transaction.Nonce() != 1 {
		t.Fatalf("broadcasts %d mined %d", len(hashes), len(harness.evm.mined))
	}
	if harness.oracle.metrics.pending != 0 || harness.oracle.metrics.inFlight != 0 {
		t.Fatalf("pending %d in flight %v", harness.oracle.metrics.pending, harness.oracle.metrics.inFlight)
	}
}

// A restart with a signed transaction the chain never saw rebroadcasts the
// exact recorded bytes and signs nothing new.
func TestRestartRebroadcastsRecordedTransaction(t *testing.T) {
	dir := t.TempDir()
	harness := newGoldenHarness(t, testSettings(dir), false)
	harness.evm.dropBroadcasts = true
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := harness.evm.distinctBroadcasts()
	if len(first) != 1 || len(harness.evm.mined) != 0 {
		t.Fatalf("broadcasts %d mined %d", len(first), len(harness.evm.mined))
	}
	records, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(records) != 1 {
		t.Fatalf("records %v", records)
	}

	// A new process over the same state directory and chain.
	harness.evm.dropBroadcasts = false
	restarted := newTestHarness(t, harness.native, harness.evm, harness.oracle.settings, testOracleKey(t), harness.oracle.address, harness.clock, &testLog{})
	if err := restarted.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hashes := harness.evm.distinctBroadcasts(); len(hashes) != 1 || hashes[0] != first[0] || len(harness.evm.mempool) != 1 {
		t.Fatalf("restart broadcast %v, want only %v", hashes, first)
	}
	harness.evm.mine()
	harness.evm.finalize()
	if err := restarted.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hashes := harness.evm.distinctBroadcasts(); len(hashes) != 1 || len(harness.evm.mined) != 1 || !harness.evm.mined[0].success {
		t.Fatalf("broadcasts %d mined %d", len(hashes), len(harness.evm.mined))
	}
	send, err := restarted.oracle.sender.readRecord(records[0])
	if err != nil || !send.record.Finalized {
		t.Fatalf("record finalized %t: %v", send.record.Finalized, err)
	}
	if restarted.oracle.metrics.mirrored != 1 || restarted.oracle.metrics.pending != 0 {
		t.Fatalf("mirrored %d pending %d", restarted.oracle.metrics.mirrored, restarted.oracle.metrics.pending)
	}
}

// The runbook's manual cast mirror consumed the oracle's nonce while a
// recorded transaction was unsent: the record is set aside and the commitment,
// already mirrored, is not sent again.
func TestRestartDropsRecordWhoseNonceWasConsumed(t *testing.T) {
	dir := t.TempDir()
	harness := newGoldenHarness(t, testSettings(dir), false)
	harness.evm.dropBroadcasts = true
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	recorded := harness.evm.distinctBroadcasts()[0]

	// An operator mirrors the same arguments by hand with nonce 0 and a
	// different gas price.
	harness.evm.dropBroadcasts = false
	coordinator := harness.oracle.settings.Coordinator
	manual, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: 0, GasPrice: big.NewInt(6 * 1000 * 1000 * 1000), Gas: 100000, To: &coordinator, Value: new(big.Int), Data: goldenCalldata(t)}), types.LatestSignerForChainID(big.NewInt(964)), testOracleKey(t))
	if err != nil {
		t.Fatal(err)
	}
	harness.evm.mempool = append(harness.evm.mempool, manual)
	harness.evm.mine()
	harness.evm.finalize()

	restarted := newTestHarness(t, harness.native, harness.evm, harness.oracle.settings, testOracleKey(t), harness.oracle.address, harness.clock, &testLog{})
	if err := restarted.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, transaction := range harness.evm.broadcasts {
		if transaction.Hash() != recorded {
			t.Fatalf("signed a new transaction %s", transaction.Hash())
		}
	}
	if len(harness.evm.mempool) != 0 || restarted.oracle.metrics.mirrored != 1 {
		t.Fatalf("mempool %d mirrored %d", len(harness.evm.mempool), restarted.oracle.metrics.mirrored)
	}
	dropped, _ := filepath.Glob(filepath.Join(dir, "*.dropped-0"))
	if len(dropped) != 1 {
		t.Fatalf("dropped records %v", dropped)
	}
}

// A transaction of the oracle account that the oracle did not record is
// pending: nothing is signed.
func TestSendRefusesWithForeignPendingTransaction(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	coordinator := harness.oracle.settings.Coordinator
	foreign, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: 0, GasPrice: big.NewInt(5 * 1000 * 1000 * 1000), Gas: 100000, To: &coordinator, Value: new(big.Int), Data: goldenCalldata(t)}), types.LatestSignerForChainID(big.NewInt(964)), testOracleKey(t))
	if err != nil {
		t.Fatal(err)
	}
	harness.evm.mempool = append(harness.evm.mempool, foreign)
	if err := harness.oracle.cycle(context.Background()); !errors.Is(err, errForeignTransaction) {
		t.Fatalf("cycle: %v", err)
	}
	if len(harness.evm.broadcasts) != 0 {
		t.Fatal("signed a transaction next to a foreign pending one")
	}
	// Mined but not final is refused too.
	harness.evm.mine()
	if err := harness.oracle.cycle(context.Background()); !errors.Is(err, errForeignTransaction) {
		t.Fatalf("cycle after mining: %v", err)
	}
}

func TestSendRespectsFeeBounds(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	harness.evm.gasPrice = big.NewInt(21 * 1000 * 1000 * 1000)
	if err := harness.oracle.cycle(context.Background()); !errors.Is(err, errGasPriceTooHigh) {
		t.Fatalf("gas price: %v", err)
	}
	harness.evm.gasPrice = big.NewInt(5 * 1000 * 1000 * 1000)
	harness.oracle.sender.fees.maxGasLimit = 100000
	if err := harness.oracle.cycle(context.Background()); !errors.Is(err, errGasLimitTooHigh) {
		t.Fatalf("gas limit: %v", err)
	}
	harness.oracle.sender.fees.maxGasLimit = 250000
	harness.evm.balances[harness.oracle.address] = big.NewInt(1000)
	if err := harness.oracle.cycle(context.Background()); !errors.Is(err, errInsufficientFunds) {
		t.Fatalf("balance: %v", err)
	}
	if len(harness.evm.broadcasts) != 0 {
		t.Fatal("sent outside the fee bounds")
	}
}

// A dry run with only the address logs the exact call, even unfunded, and
// never signs.
func TestDryRunLogsCallsAndNeverSigns(t *testing.T) {
	key := testOracleKey(t)
	settings := testSettings(t.TempDir())
	settings.DryRun = true
	evm := newFakeEvm(t, settings.Coordinator, keyAddress(key), goldenFinalized)
	evm.balances[keyAddress(key)] = new(big.Int)
	log := &testLog{}
	harness := newTestHarness(t, goldenNative(t), evm, settings, nil, keyAddress(key), &testClock{now: time.Unix(1760000000, 0)}, log)
	if err := harness.oracle.admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(evm.broadcasts) != 0 || len(evm.mempool) != 0 {
		t.Fatal("a dry run broadcast a transaction")
	}
	entries, _ := os.ReadDir(settings.StateDir)
	if len(entries) != 0 {
		t.Fatalf("a dry run wrote %d records", len(entries))
	}
	if !log.contains("dry run: would send mirrorCommitment(hotkey=0x"+goldenHotkeyHex+" commitment=0x"+goldenCommitmentHex+" block=9259873 block_hash=0x"+goldenBlockHashHex+")") || !log.contains("funded false") {
		t.Fatalf("log %v", log.lines)
	}
	if harness.oracle.metrics.pending != 1 || harness.oracle.metrics.dryRun != 1 {
		t.Fatalf("pending %d dry run %v", harness.oracle.metrics.pending, harness.oracle.metrics.dryRun)
	}
}

// A mirror that reverts on chain is set aside and re-evaluated from state.
func TestRevertedMirrorIsReevaluated(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	hotkey := mustHash(t, goldenHotkeyHex)
	// Between the preflight and inclusion another sender mirrors a newer
	// commitment, so this one reverts StaleCommitment on chain.
	harness.oracle.sleep = func(ctx context.Context, duration time.Duration) error {
		if len(harness.evm.mined) == 0 {
			harness.evm.setRecord(hotkey, mirroredRecord{commitmentHash: [32]byte{0x77}, finalizedBlockHash: [32]byte{0x78}, finalizedBlock: goldenBlock + 1}, harness.evm.latest)
		}
		harness.evm.mine()
		harness.evm.finalize()
		return nil
	}
	err := harness.oracle.cycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dropped") {
		t.Fatalf("cycle: %v", err)
	}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.distinctBroadcasts()) != 1 || harness.oracle.metrics.conflicting != 1 {
		t.Fatalf("broadcasts %d conflicting %d", len(harness.evm.distinctBroadcasts()), harness.oracle.metrics.conflicting)
	}
}

// Every other endpoint must agree before a mirror is sent.
func TestCycleRequiresSecondaryAgreement(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), true)
	secondary := goldenNative(t)
	secondary.block(goldenBlock, [32]byte{0x01})
	harness.oracle.connections.secondaries = []nativeReader{secondary}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if harness.oracle.metrics.pending != 0 || harness.oracle.metrics.inconsistent != 1 || !harness.log.contains("disagrees on the hash") {
		t.Fatalf("pending %d inconsistent %d", harness.oracle.metrics.pending, harness.oracle.metrics.inconsistent)
	}
	harness.oracle.connections.secondaries = []nativeReader{goldenNative(t)}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if harness.oracle.metrics.pending != 1 {
		t.Fatalf("pending %d with agreeing endpoints", harness.oracle.metrics.pending)
	}
}

func TestCycleRefusesWhilePaused(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), false)
	harness.evm.paused = true
	if err := harness.oracle.cycle(context.Background()); !errors.Is(err, errCoordinatorPaused) {
		t.Fatalf("cycle: %v", err)
	}
	if len(harness.evm.broadcasts) != 0 || harness.oracle.metrics.paused != 1 || harness.oracle.metrics.pending != 1 {
		t.Fatalf("broadcasts %d paused %v pending %d", len(harness.evm.broadcasts), harness.oracle.metrics.paused, harness.oracle.metrics.pending)
	}
}

// A record that is not this oracle's mirror transaction is refused rather
// than reconciled.
func TestUnfinishedRefusesForeignRecord(t *testing.T) {
	dir := t.TempDir()
	harness := newGoldenHarness(t, testSettings(dir), false)
	harness.evm.dropBroadcasts = true
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	records, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	encoded, _ := os.ReadFile(records[0])
	tampered := strings.Replace(string(encoded), `"finalized_block": 9259873`, `"finalized_block": 9259874`, 1)
	if err := os.WriteFile(records[0], []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := harness.oracle.cycle(context.Background()); err == nil || !strings.Contains(err.Error(), "not this oracle's mirror transaction") {
		t.Fatalf("cycle: %v", err)
	}
}
