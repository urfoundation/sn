//go:build linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The claim queue's durable head bound, as the miner declares it.
const e2eClaimQueueBytes = 16 * 1024 * 1024

// e2eClaimQueueContext provisions each claim state directory as an enrolled
// durable claim queue and returns a context declaring their volume, as an
// operator's enrollment does: the daemon has no enrollment fallback. A
// claim-queue.json written beforehand is adopted as the committed head.
func e2eClaimQueueContext(t testing.TB, ctx context.Context, paths ...string) context.Context {
	t.Helper()
	var roots []string
	for _, path := range paths {
		parent, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		physical := filepath.Join(parent, filepath.Base(path))
		if err := os.MkdirAll(physical, 0o700); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, physical)
		attribute := durablehead.Attribute("provider-claim-queue", "claim-queue.json")
		if _, err := unix.Getxattr(physical, attribute, nil); err == nil {
			continue
		}
		var stat unix.Stat_t
		if err := unix.Stat(physical, &stat); err != nil {
			t.Fatal(err)
		}
		checkpoint := durablehead.Checkpoint{Schema: durablehead.Schema, Kind: "provider-claim-queue", Name: "claim-queue.json", MaximumBytes: e2eClaimQueueBytes, DirectoryInode: stat.Ino}
		if raw, err := os.ReadFile(filepath.Join(physical, "claim-queue.json")); err == nil {
			var file unix.Stat_t
			if err := unix.Stat(filepath.Join(physical, "claim-queue.json"), &file); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			checkpoint.Committed = durablehead.Member{Present: true, Inode: file.Ino, Size: int64(len(raw)), Sha256: hex.EncodeToString(digest[:])}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		raw, err := json.Marshal(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Setxattr(physical, attribute, raw, unix.XATTR_CREATE); err != nil {
			t.Fatal(err)
		}
		directory, err := os.Open(physical)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			t.Fatal(err)
		}
	}
	return durablefixture.New(t, ctx, roots...).Context
}

// e2eDaemon runs the real v2 claim daemon, miner.RunClaimDaemonV2, as a
// provider would: its own state directory, the fixture's RPC and ledger
// endpoints, vault2 pinned in its configuration, and one relayer key.
type e2eDaemon struct {
	f        *e2eFixture
	relayer  *e2eActor
	keyFile  string
	config   string
	v1       string
	stateDir string
	cancel   context.CancelFunc
	done     chan error
}

func (f *e2eFixture) daemon(relayer *e2eActor, coldkeys ...[32]byte) *e2eDaemon {
	f.t.Helper()
	parent, err := filepath.EvalSymlinks(f.t.TempDir())
	if err != nil {
		f.t.Fatal(err)
	}
	d := &e2eDaemon{f: f, relayer: relayer, keyFile: relayer.keyFile(f.t), stateDir: filepath.Join(parent, "claims-v2")}
	config := miner.ClaimDaemonV2Config{SchemaVersion: 2, APIURL: f.publisher.server.URL, RPC: []string{f.chain.server.URL}, ChainID: e2eChainID,
		Vault: strings.ToLower(f.vault2.Hex()), KeyFile: d.keyFile, StateDir: d.stateDir, PollSeconds: 5}
	for _, coldkey := range coldkeys {
		address, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
		if err != nil {
			f.t.Fatal(err)
		}
		config.Coldkeys = append(config.Coldkeys, address)
	}
	raw, err := yaml.Marshal(config)
	if err != nil {
		f.t.Fatal(err)
	}
	d.config = filepath.Join(parent, "claim-v2.yml")
	if err := os.WriteFile(d.config, raw, 0o600); err != nil {
		f.t.Fatal(err)
	}
	return d
}

// start runs the daemon until stop; demand is its --claim-now.
func (d *e2eDaemon) start(demand bool, extra ...string) {
	d.f.t.Helper()
	roots := append([]string{d.stateDir}, extra...)
	ctx, cancel := context.WithCancel(e2eClaimQueueContext(d.f.t, d.f.t.Context(), roots...))
	d.cancel, d.done = cancel, make(chan error, 1)
	go func() {
		d.done <- miner.RunClaimDaemonV2(ctx, d.config, d.v1, miner.ClaimV2DefaultThresholdRao, demand)
	}()
	d.f.t.Cleanup(func() {
		if d.cancel != nil {
			d.stop()
		}
	})
}

// stop cancels the daemon, as a process stop, and requires a clean exit.
func (d *e2eDaemon) stop() {
	d.f.t.Helper()
	d.cancel()
	d.cancel = nil
	if err := <-d.done; err != nil {
		d.f.t.Fatalf("v2 claim daemon: %v", err)
	}
}

// queue reads the queue the daemon last published.
func (d *e2eDaemon) queue() *miner.ClaimQueueV2 {
	raw, err := os.ReadFile(filepath.Join(d.stateDir, "claim-queue.json"))
	if err != nil {
		return nil
	}
	var queue miner.ClaimQueueV2
	if json.Unmarshal(raw, &queue) != nil || queue.Schema != miner.ClaimQueueV2Schema {
		return nil
	}
	return &queue
}

func (d *e2eDaemon) unit(index uint64) *miner.ClaimUnitV2 {
	if queue := d.queue(); queue != nil {
		return queue.Units[fmt.Sprintf("%s/%d/%d", strings.ToLower(d.f.vault2.Hex()), e2eNoID, index)]
	}
	return nil
}

// await waits up to three minutes, as the daemon polls every 5 seconds and
// retries an in-flight claim after a minute, for done.
func (d *e2eDaemon) await(what string, done func() bool) {
	d.f.t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for !done() {
		select {
		case err := <-d.done:
			// Keep the outcome for stop, which the test's cleanup runs.
			d.done <- err
			d.f.t.Fatalf("the daemon stopped before %s: %v", what, err)
		default:
		}
		if time.Now().After(deadline) {
			d.f.t.Fatalf("the daemon did not %s; queue %+v", what, d.queue())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// idle reports the unit of index settled with claimed and nothing in flight.
func (d *e2eDaemon) idle(index uint64, claimed *big.Int) bool {
	unit := d.unit(index)
	return unit != nil && unit.Status == "idle" && unit.Claimed == claimed.String() && unit.TxHash == "" && unit.RawTxHex == "" && d.f.claimed(index).Cmp(claimed) == 0
}

// requireClaim checks a claim mined from the relayer: its nonce and its exact
// Claimed event.
func (d *e2eDaemon) requireClaim(tx *types.Transaction, nonce, rootIndex, index uint64, coldkey [32]byte, cumulative, amount *big.Int) {
	d.f.t.Helper()
	receipt := d.f.chain.receipt(tx.Hash())
	if tx.Nonce() != nonce || receipt == nil {
		d.f.t.Fatalf("claim %s has nonce %d and receipt %v, want nonce %d", tx.Hash(), tx.Nonce(), receipt, nonce)
	}
	d.f.requireLogs(receipt, d.f.vault2Log("Claimed", e2eRao(e2eNoID), e2eRao(rootIndex), e2eRao(index), coldkey, cumulative, amount, d.relayer.address))
}

// claimIndex is the account index a claim transaction names.
func (f *e2eFixture) claimIndex(tx *types.Transaction) uint64 {
	f.t.Helper()
	method, err := f.vault2ABI.MethodById(tx.Data()[:4])
	if err != nil || method.Name != "claim" {
		f.t.Fatalf("transaction %s is not a vault2 claim", tx.Hash())
	}
	values, err := method.Inputs.Unpack(tx.Data()[4:])
	if err != nil {
		f.t.Fatal(err)
	}
	return abi.ConvertType(values[2], new(stabi.LedgerTreeAccount)).(*stabi.LedgerTreeAccount).Index.Uint64()
}

// cumulative is the account's value in the latest finalized root.
func (f *e2eFixture) cumulative(index uint64) *big.Int {
	return f.publisher.view(f.root(f.rootCount() - 1).Root).accounts[index].Cumulative.ToBig()
}

// e2eDaemonRows: Alice and Bob earn far above the claim threshold, Tiny far
// below it.
func e2eDaemonRows() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{e2eRow(1, e2eKey("alice"), 3_000), e2eRow(2, e2eKey("bob"), 2_000), e2eRow(3, e2eKey("tiny"), 1)}
}

// E-5 with the real daemon against the real vault2: a threshold claim (Alice
// at the threshold and more, Tiny below it); reopen on a newer root (only
// the increment is claimed, at the next nonce); and an on-demand claim (a
// restart with --claim-now claims Tiny, below the threshold, and nothing for
// Alice, who has nothing claimable).
func TestSettlementV2E2EDaemonThresholdReopenDemand(t *testing.T) {
	t.Parallel()
	relayer := newE2EActor("relayer")
	f := newE2EFixture(t, e2eDefaultFirst, relayer)
	root0 := f.epoch(f.firstEpoch, 1_450_000_000_000, e2eDaemonRows())
	alice, tiny := e2eAccountOf(t, root0.Accounts, e2eKey("alice")), e2eAccountOf(t, root0.Accounts, e2eKey("tiny"))
	if f.cumulative(tiny).Uint64() >= miner.ClaimV2DefaultThresholdRao || f.cumulative(alice).Uint64() < miner.ClaimV2DefaultThresholdRao {
		t.Fatal("the setting does not straddle the threshold")
	}
	d := f.daemon(relayer, e2eKey("alice"), e2eKey("tiny"))
	d.start(false)
	c0 := f.cumulative(alice)
	d.await("claim Alice at the threshold", func() bool { return d.idle(alice, c0) })
	if unit := d.unit(tiny); unit == nil || unit.Status != "idle" || f.claimed(tiny).Sign() != 0 {
		t.Fatalf("Tiny, below the threshold, was claimed: %+v", unit)
	}
	// A newer root reopens Alice for the increment.
	f.epoch(f.firstEpoch+1, 1_450_000_000_000, e2eDaemonRows())
	c1 := f.cumulative(alice)
	d.await("claim Alice's increment", func() bool { return d.idle(alice, c1) })
	if f.cumulative(tiny).Uint64() >= miner.ClaimV2DefaultThresholdRao || f.claimed(tiny).Sign() != 0 {
		t.Fatal("Tiny crossed the threshold or was claimed")
	}
	d.stop()
	// --claim-now claims Tiny once; Alice has nothing claimable.
	d.start(true)
	tinyValue := f.cumulative(tiny)
	d.await("claim Tiny on demand", func() bool { return d.idle(tiny, tinyValue) })
	d.stop()
	sent := f.chain.sentBy(relayer.address)
	if len(sent) != 3 {
		t.Fatalf("the relayer sent %d claims, want 3", len(sent))
	}
	d.requireClaim(sent[0], 0, 0, alice, e2eKey("alice"), c0, c0)
	d.requireClaim(sent[1], 1, 1, alice, e2eKey("alice"), c1, new(big.Int).Sub(c1, c0))
	d.requireClaim(sent[2], 2, 1, tiny, e2eKey("tiny"), tinyValue, tinyValue)
	if unit := d.unit(tiny); unit.Demand || unit.Status != "idle" {
		t.Fatalf("the demand was not spent: %+v", unit)
	}
	f.requireConservation()
}

// E-5 with the real daemon: a newer root during an in-flight claim, one claim
// in flight per account, a restart mid-claim and signed bytes never replaced.
// The chain holds the daemon's first claim unmined. A newer root raises both
// accounts, and the daemon is stopped mid-claim and restarted with
// --claim-now: it reconciles the exact retained bytes, which name the older
// root, and signs nothing more for that account while they are in flight;
// the other account gets its own claim at the next nonce. Once both land,
// the first account's increment is claimed at the nonce after.
func TestSettlementV2E2EDaemonInFlightRestart(t *testing.T) {
	t.Parallel()
	relayer := newE2EActor("relayer")
	f := newE2EFixture(t, e2eDefaultFirst, relayer)
	root0 := f.epoch(f.firstEpoch, 1_450_000_000_000, e2eDaemonRows())
	alice, bob := e2eAccountOf(t, root0.Accounts, e2eKey("alice")), e2eAccountOf(t, root0.Accounts, e2eKey("bob"))
	f.chain.holdTransactions()
	d := f.daemon(relayer, e2eKey("alice"), e2eKey("bob"))
	d.start(false)
	d.await("sign a first claim", func() bool { return len(f.chain.pooled()) == 1 })
	first := f.chain.pooled()[0]
	x, y := alice, bob
	if f.claimIndex(first) == bob {
		x, y = bob, alice
	}
	retained := d.unit(x)
	if raw, _ := first.MarshalBinary(); retained == nil || retained.RawTxHex != hexutil.Encode(raw) || retained.Status != "submitting" {
		t.Fatalf("the in-flight claim is not the retained bytes: %+v", retained)
	}
	cx0 := f.cumulative(x)

	// A newer root lands while the claim is in flight, and the process stops.
	f.epoch(f.firstEpoch+1, 1_450_000_000_000, e2eDaemonRows())
	d.stop()
	if unit := d.unit(x); unit.RawTxHex != retained.RawTxHex || unit.TxHash != retained.TxHash {
		t.Fatalf("the stop replaced the signed bytes: %+v", unit)
	}
	d.start(true)
	d.await("claim the other account at the next nonce", func() bool { return len(f.chain.pooled()) == 2 })
	pooled := f.chain.pooled()
	if pooled[0].Hash() != first.Hash() || pooled[1].Nonce() != first.Nonce()+1 || len(f.chain.sentBy(relayer.address)) != 2 {
		t.Fatal("a second claim was signed for the account in flight, or the other account did not take the next nonce")
	}
	if unit := d.unit(x); unit.Status != "uncertain" || unit.RawTxHex != retained.RawTxHex || !unit.Demand {
		t.Fatalf("the in-flight unit after the restart: %+v", unit)
	}
	f.chain.release()
	cx1, cy1 := f.cumulative(x), f.cumulative(y)
	d.await("settle the other account's claim", func() bool { return d.idle(y, cy1) })
	d.await("reconcile the retained claim and claim its increment", func() bool { return d.idle(x, cx1) })
	d.stop()
	sent := f.chain.sentBy(relayer.address)
	if len(sent) != 3 {
		t.Fatalf("the relayer sent %d claims, want 3", len(sent))
	}
	d.requireClaim(sent[0], 0, 0, x, root0.Accounts[x].Coldkey, cx0, cx0)
	d.requireClaim(sent[1], 1, 1, y, root0.Accounts[y].Coldkey, cy1, cy1)
	d.requireClaim(sent[2], 2, 1, x, root0.Accounts[x].Coldkey, cx1, new(big.Int).Sub(cx1, cx0))
	f.requireConservation()
}

// E-5 with the real daemon, a third party claiming first (SPEC A6): before
// the daemon signs, the account is done with nothing sent; after the
// daemon's claim is broadcast and before it is included, the third party's
// claim lands first and the daemon's reverts NothingToClaim in the vault,
// and the unit is done for that root without another claim. A newer root
// reopens it for the increment.
func TestSettlementV2E2EDaemonThirdPartyFirst(t *testing.T) {
	t.Parallel()
	relayer := newE2EActor("relayer")
	f := newE2EFixture(t, e2eDefaultFirst, relayer)
	root0 := f.epoch(f.firstEpoch, 1_450_000_000_000, e2eDaemonRows())
	alice := e2eAccountOf(t, root0.Accounts, e2eKey("alice"))
	claim := func(rootIndex uint64) {
		view := f.publisher.view(f.root(rootIndex).Root)
		account, proof := e2eProof(t, view.tree, view.accounts, alice)
		f.must(f.anyone, f.vault2, nil, e2eVault2Binding.PackClaim(e2eRao(e2eNoID), e2eRao(rootIndex), account, proof))
	}
	claim(0)
	d := f.daemon(relayer, e2eKey("alice"))
	d.start(false)
	d.await("see Alice's account", func() bool { return d.unit(alice) != nil })
	time.Sleep(6 * time.Second)
	if unit := d.unit(alice); unit.Status != "idle" || len(f.chain.sentBy(relayer.address)) != 0 {
		t.Fatalf("a claim was sent for an account a third party had claimed: %+v", unit)
	}
	// The third party's claim lands between the daemon's broadcast and its
	// inclusion.
	f.chain.holdTransactions()
	f.epoch(f.firstEpoch+1, 1_450_000_000_000, e2eDaemonRows())
	d.await("sign the reopened claim", func() bool { return len(f.chain.pooled()) == 1 })
	ours := f.chain.pooled()[0]
	claim(1)
	f.chain.release()
	c1 := f.cumulative(alice)
	d.await("settle the front-run claim", func() bool { return d.idle(alice, c1) })
	if receipt := f.chain.receipt(ours.Hash()); receipt == nil || receipt.Status != types.ReceiptStatusFailed || len(receipt.Logs) != 0 || f.chain.stake(f.escrowV2, e2eKey("alice")).Cmp(c1) != 0 {
		t.Fatal("the front-run claim did not revert in the vault, or Alice was paid twice")
	}
	// A newer root reopens the unit for the increment only.
	f.epoch(f.firstEpoch+2, 1_450_000_000_000, e2eDaemonRows())
	c2 := f.cumulative(alice)
	d.await("claim the increment", func() bool { return d.idle(alice, c2) })
	d.stop()
	sent := f.chain.sentBy(relayer.address)
	if len(sent) != 2 || sent[0].Hash() != ours.Hash() {
		t.Fatalf("the relayer sent %d claims", len(sent))
	}
	d.requireClaim(sent[1], 1, 2, alice, e2eKey("alice"), c2, new(big.Int).Sub(c2, c1))
	f.requireConservation()
}

// E-5 with the real daemons: v1 and v2 side by side in one process on one
// relayer key. The v1 daemon reconciles its retained, already included claim
// on the real v1 vault from its receipt, and the v2 daemon takes the nonce
// above it from the shared floor although this endpoint's nonce view lags.
func TestSettlementV2E2EDaemonBesideV1(t *testing.T) {
	relayer := newE2EActor("relayer")
	f := newE2EFixture(t, e2eDefaultFirst, relayer)
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	// v1 epoch 1 pays Carol all of its funding, through a claim the relayer
	// signed and broadcast before its process stopped, with a gas limit above
	// the transfer's declared weight.
	carol := e2eKey("carol")
	f.emitV1(2_000_000_000_000)
	funded := f.closeV1(1)
	root := [32]byte(merkle.PayoutLeaf(carol, big.NewInt(10_000)))
	f.commitV1(1, root, sha256.Sum256([]byte("v1 epoch 1 artifact")))
	f.finalizeV1(1)
	data, err := onchain.BuildClaimCalldata(onchain.ClaimIntent{E: big.NewInt(1), NoID: big.NewInt(e2eNoID), Coldkey: carol, ShareBps: big.NewInt(10_000)})
	if err != nil {
		t.Fatal(err)
	}
	v1Claim, err := types.SignNewTx(relayer.key, f.chain.signer, &types.LegacyTx{Nonce: 0, GasPrice: big.NewInt(e2eGasPrice), Gas: 3_000_000, To: &f.vault1, Value: new(big.Int), Data: data})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := v1Claim.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	f.requireLogs(f.chain.include(v1Claim), f.vault1Log("Claimed", e2eRao(1), e2eRao(e2eNoID), carol, big.NewInt(10_000), funded, relayer.address), f.vault1Log("ClaimPaid", carol, funded, relayer.address))
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v1Dir := filepath.Join(parent, "claims-v1")
	if err := os.MkdirAll(v1Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	queue, err := json.Marshal(miner.ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 1, Entries: map[string]*miner.ClaimQueueEntry{
		"1": {Epoch: 1, Status: "uncertain", Attempts: 1, TxHash: strings.ToLower(v1Claim.Hash().Hex()), RawTxHex: hexutil.Encode(signed)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v1Dir, "claim-queue.json"), queue, 0o600); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/sn/epoch" {
			_ = json.NewEncoder(writer).Encode(map[string]int{"epoch": 2})
			return
		}
		http.NotFound(writer, request)
	}))
	t.Cleanup(api.Close)
	// v2: Alice's finalized account, claimed by the same relayer key.
	root0 := f.epoch(f.firstEpoch, 1_450_000_000_000, e2eDaemonRows())
	alice := e2eAccountOf(t, root0.Accounts, e2eKey("alice"))
	d := f.daemon(relayer, e2eKey("alice"))
	v1Config, err := yaml.Marshal(miner.ClaimDaemonConfig{SchemaVersion: 1, Release: "1.0", APIURL: api.URL, RPC: []string{f.chain.server.URL}, KeyFile: d.keyFile, StateDir: v1Dir, PollSeconds: 5, LookbackEpochs: 2})
	if err != nil {
		t.Fatal(err)
	}
	d.v1 = filepath.Join(parent, "claim-v1.yml")
	if err := os.WriteFile(d.v1, v1Config, 0o600); err != nil {
		t.Fatal(err)
	}
	f.chain.do(func() { f.chain.nonceView[relayer.address] = 0 })
	d.start(false, v1Dir)
	c0 := f.cumulative(alice)
	d.await("settle both daemons", func() bool {
		raw, err := os.ReadFile(filepath.Join(v1Dir, "claim-queue.json"))
		var v1Queue miner.ClaimQueue
		return err == nil && json.Unmarshal(raw, &v1Queue) == nil && v1Queue.Entries["1"].Status == "finalized" && d.idle(alice, c0)
	})
	d.stop()
	sent := f.chain.sentBy(relayer.address)
	if len(sent) != 1 {
		t.Fatalf("the relayer sent %d transactions over RPC, want the v2 claim", len(sent))
	}
	d.requireClaim(sent[0], 1, 0, alice, e2eKey("alice"), c0, c0)
	raw, err := os.ReadFile(filepath.Join(v1Dir, "claim-queue.json"))
	var v1Queue miner.ClaimQueue
	if err != nil || json.Unmarshal(raw, &v1Queue) != nil {
		t.Fatal(err)
	}
	if entry := v1Queue.Entries["1"]; entry.TxHash != strings.ToLower(v1Claim.Hash().Hex()) || entry.RawTxHex != hexutil.Encode(signed) || entry.ReceiptStatus != 1 {
		t.Fatalf("the v1 entry: %+v", entry)
	}
	if !e2eView(f, f.vault1, e2eVault1Binding.PackConservationHolds(), e2eVault1Binding.UnpackConservationHolds) || f.chain.stake(f.escrowV1, carol).Cmp(funded) != 0 {
		t.Fatal("v1 did not pay Carol exactly")
	}
	f.requireConservation()
}
