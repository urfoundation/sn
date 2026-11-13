// Retained byte limits preserve custody at exact size boundaries and when a
// regular file grows between the descriptor stat and its bounded read.
package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Trailing JSON whitespace makes both files syntactically valid; the extra
// byte must be rejected for capacity before the decoder can accept it.
func TestClaimQueueCapacityReadAcceptsExactLimitAndRejectsExcess(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "claims")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte(" "), maximumClaimQueueBytes)
	copy(raw, []byte(`{"schema":"urnetwork-provider-claim-queue-v1","last_discovered":-1,"entries":{}}`))
	if err := os.WriteFile(filepath.Join(directory, "claim-queue.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := newClaimQueueTestStore(t, directory)
	queue, err := store.load()
	if err != nil || queue == nil || queue.LastDiscovered != -1 || len(queue.Entries) != 0 {
		t.Fatalf("exact-limit queue was not admitted: %v", err)
	}
	file, err := os.OpenFile(store.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write([]byte(" "))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := claimQueueCapacityDigest(t, store.path)
	if queue, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) || queue != nil {
		t.Fatalf("limit-plus-one queue was admitted: queue present=%t error=%v", queue != nil, err)
	}
	after, err := os.Stat(store.path)
	if err != nil || !os.SameFile(before, after) || after.Size() != maximumClaimQueueBytes+1 {
		t.Fatalf("oversized read changed retained bytes: %v", err)
	}
	if claimQueueCapacityDigest(t, store.path) != beforeHash {
		t.Fatal("oversized read changed the retained byte hash")
	}
}

// Growth is forced after the real opened-object stat, without sleeps or a
// replacement reader. A size check alone cannot bound this actual file read.
func TestClaimQueueCapacityReadRejectsGrowthAfterStat(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	if err := os.WriteFile(store.path, []byte("synthetic small original queue"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed := false
	var beforeHash [sha256.Size]byte
	raw, _, err := claimQueueReadFile(store.directory, filepath.Base(store.path), claimQueueReadHooks{afterStat: func() error {
		observed = true
		if err := os.Truncate(store.path, maximumClaimQueueBytes+4096); err != nil {
			return err
		}
		beforeHash = claimQueueCapacityDigest(t, store.path)
		return nil
	}})
	if !observed || !errors.Is(err, errClaimQueueCapacity) || raw != nil {
		t.Fatalf("post-stat growth escaped bounded read: observed=%t bytes=%d error=%v", observed, len(raw), err)
	}
	info, err := os.Stat(store.path)
	if err != nil || info.Size() != maximumClaimQueueBytes+4096 {
		t.Fatalf("growth rejection truncated retained evidence: %v", err)
	}
	if claimQueueCapacityDigest(t, store.path) != beforeHash {
		t.Fatal("growth rejection changed the retained byte hash")
	}
}

// The exact publication boundary includes indentation, escaping and its final
// newline. Oversize state cannot replace a previously acknowledged signature.
func TestClaimQueueCapacitySavePreservesExactLimitBytesOnExcess(t *testing.T) {
	cfg, _, entry, _, _ := signedClaimFixture(t, 70, 23)
	store := newClaimQueueTestStore(t, cfg.StateDir)
	entry.LastError = "x"
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	encoded, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// The existing one-byte marker offsets the publication's final newline.
	entry.LastError = strings.Repeat("x", maximumClaimQueueBytes-len(encoded))
	if err := store.save(queue); err != nil {
		t.Fatalf("exact-limit publication failed: %v", err)
	}
	before, err := os.Stat(store.path)
	if err != nil || before.Size() != maximumClaimQueueBytes {
		t.Fatalf("fixture did not reach exact byte limit: %v", err)
	}
	priorHash, priorRawTx, priorTxHash := store.savedHash, entry.RawTxHex, entry.TxHash
	entry.LastError += "x"
	if err := store.save(queue); !errors.Is(err, errClaimQueueCapacity) {
		t.Fatalf("oversized publication was accepted: %v", err)
	}
	after, err := os.Stat(store.path)
	if err != nil || !os.SameFile(before, after) || after.Size() != maximumClaimQueueBytes {
		t.Fatalf("oversized publication replaced retained queue: %v", err)
	}
	if claimQueueCapacityDigest(t, store.path) != priorHash || !store.saved || store.savedHash != priorHash || entry.RawTxHex != priorRawTx || entry.TxHash != priorTxHash {
		t.Fatal("oversized publication changed bytes, acknowledgment or signed custody")
	}
	files, err := os.ReadDir(filepath.Dir(store.path))
	if err != nil || len(files) != 1 || files[0].Name() != "claim-queue.json" {
		t.Fatalf("oversized publication left a temporary file: files=%d error=%v", len(files), err)
	}
}

// The descriptor publisher also rejects an oversized payload before creating
// temporary state, so a future caller cannot bypass the store's admission.
func TestClaimQueueCapacityPublisherRefusesExcessBeforeFileCreation(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	if err := claimQueuePublish(store.directory, filepath.Base(store.path), make([]byte, maximumClaimQueueBytes+1)); !errors.Is(err, errClaimQueueCapacity) {
		t.Fatalf("descriptor publisher admitted excess bytes: %v", err)
	}
	files, err := os.ReadDir(filepath.Dir(store.path))
	if err != nil || len(files) != 0 {
		t.Fatalf("oversized publication created state: files=%d error=%v", len(files), err)
	}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(store.path, maximumClaimQueueBytes+1); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := claimQueueCapacityDigest(t, store.path)
	// The first identical save detects the changed file while invalidating
	// its old acknowledgment. Repetition and changed content must still refuse.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt == 2 {
			queue.LastDiscovered = 1
		}
		if err := store.save(queue); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatalf("save %d replaced oversized retained bytes: %v", attempt, err)
		}
		if claimQueueCapacityDigest(t, store.path) != beforeHash {
			t.Fatalf("save %d changed the retained byte hash", attempt)
		}
	}
	after, err := os.Stat(store.path)
	if err != nil || !os.SameFile(before, after) || after.Size() != maximumClaimQueueBytes+1 {
		t.Fatalf("refused save replaced the oversized retained file: %v", err)
	}
	files, err = os.ReadDir(filepath.Dir(store.path))
	if err != nil || len(files) != 1 || files[0].Name() != "claim-queue.json" {
		t.Fatalf("refused save created temporary state: files=%d error=%v", len(files), err)
	}
}

// A sparse corrupt file supplies the startup fault without allocating it in
// the fixture. Refusal precedes API workers and preserves the original inode.
func TestClaimQueueCapacityDaemonRefusesExcessBeforeNetwork(t *testing.T) {
	configPath, cfg, requests := claimQueueOwnerDaemonFixture(t)
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := claimQueueTestContext(t, context.Background(), cfg.StateDir)
	path := filepath.Join(cfg.StateDir, "claim-queue.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(maximumClaimQueueBytes+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := claimQueueCapacityDigest(t, path)
	ready := false
	admission := &claimAdmission{}
	err = runClaimDaemonWithAdmission(ctx, configPath, admission, 0, func() { ready = true })
	if !errors.Is(err, durablevolume.ErrIdentity) || ready || requests.Load() != 0 || admission.nonceMinimum() != 0 {
		t.Fatalf("oversized startup reached custody or network work: ready=%t requests=%d nonce=%d error=%v", ready, requests.Load(), admission.nonceMinimum(), err)
	}
	if reopened, err := newClaimQueueStore(cfg.StateDir, ctx); reopened != nil || !errors.Is(err, durablevolume.ErrIdentity) {
		if reopened != nil {
			_ = reopened.close()
		}
		t.Fatalf("reopen admitted an unacknowledged oversized member: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || after.Size() != maximumClaimQueueBytes+1 {
		t.Fatalf("oversized startup rewrote retained evidence: %v", err)
	}
	if claimQueueCapacityDigest(t, path) != beforeHash {
		t.Fatal("oversized startup changed the retained byte hash")
	}
}

// Hash the complete real fixture without allocating another capacity-sized
// buffer, including sparse bytes and any signed fields retained in the file.
func claimQueueCapacityDigest(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

// Size the actual queue schema and signed ABI/RLP shape, including a 16-node
// proof and 2 KiB diagnostic per epoch, well beyond the nine-epoch claim window.
func TestClaimQueueCapacitySupportsDocumentedRetainedHorizon(t *testing.T) {
	cfg, _, _, _, _ := signedClaimFixture(t, 1, 0)
	key, err := onchain.LoadKeyFile(cfg.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	store := newClaimQueueTestStore(t, cfg.StateDir)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 1024, Entries: map[string]*ClaimQueueEntry{}}
	chainId, contract := big.NewInt(945), common.HexToAddress("0x1234")
	for epoch := int64(1); epoch <= 1024; epoch++ {
		calldata, err := onchain.BuildClaimCalldata(onchain.ClaimIntent{E: big.NewInt(epoch), NoID: big.NewInt(7), Coldkey: [32]byte{1, 2, 3}, ShareBps: big.NewInt(10_000), Proof: make([][32]byte, 16)})
		if err != nil {
			t.Fatal(err)
		}
		tx, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(epoch - 1), GasPrice: big.NewInt(1), Gas: 100_000, To: &contract, Value: big.NewInt(0), Data: calldata}), types.LatestSignerForChainID(chainId), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		entry := &ClaimQueueEntry{Epoch: epoch, Status: "finalized", Attempts: 1, ReconcileAttempts: 7,
			UpdatedAt: "2030-01-01T00:00:00.123456789Z", NextRetryAt: "2030-01-01T00:01:00.123456789Z",
			TxHash: strings.ToLower(tx.Hash().Hex()), RawTxHex: "0x" + hex.EncodeToString(raw), FinalizedBlock: uint64(epoch * 50400),
			FinalizedBlockHash: "0x" + strings.Repeat("1", 64), ReceiptStatus: 1, ReceiptLogsHash: strings.Repeat("2", 64),
			LastError: strings.Repeat("synthetic error ", 128)}
		encoded, err := json.MarshalIndent(entry, "", "  ")
		if err != nil || len(encoded) > 8*1024 {
			t.Fatalf("representative epoch exceeds its sizing budget: bytes=%d error=%v", len(encoded), err)
		}
		queue.Entries[fmt.Sprint(epoch)] = entry
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path)
	if err != nil || info.Size() >= maximumClaimQueueBytes/2 {
		t.Fatalf("documented horizon lacks its extra capacity margin: %v", err)
	}
	loaded, err := store.load()
	if err != nil || len(loaded.Entries) != 1024 || loaded.LastDiscovered != 1024 {
		t.Fatalf("documented retained horizon did not round trip: %v", err)
	}
	t.Logf("representative retained queue: %d epochs, %d bytes of %d", len(loaded.Entries), info.Size(), maximumClaimQueueBytes)
}
