package validator

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/crv4"
	"golang.org/x/crypto/blake2b"
)

func testPreparedSubmission(t *testing.T, epoch uint64, uids []uint16) *crv4.PreparedSubmission {
	t.Helper()
	var hotkey [32]byte
	for index := range hotkey {
		hotkey[index] = byte(index + 1)
	}
	values := []uint16{32768, 65535}
	payload, err := (&crv4.Payload{Hotkey: hotkey, Uids: uids, Values: values, VersionKey: 1}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := []byte{0xde, 0xad, 0xbe, 0xef}
	body := append([]byte{0x84, 0xaa}, ciphertext...)
	raw := append([]byte{byte(len(body) << 2)}, body...)
	txHash := blake2b.Sum256(raw)
	cipherHash := sha256.Sum256(ciphertext)
	return &crv4.PreparedSubmission{
		Schema: crv4.PreparedSubmissionSchema, Netuid: 7, HotkeyHex: "0x" + hex.EncodeToString(hotkey[:]),
		VersionKey: 1, CommitRevealVersion: 4, AccountNonce: 3,
		PreparedAtBlock: 100, PreparedAtBlockHash: types.NewHash([]byte{1}).Hex(), SubnetEpoch: epoch,
		RevealRound: 8, RevealBlock: 120, UIDs: append([]uint16(nil), uids...), Values: values,
		PayloadHex: codec.HexEncodeToString(payload), CiphertextHex: codec.HexEncodeToString(ciphertext),
		CiphertextSHA256: "0x" + hex.EncodeToString(cipherHash[:]),
		ExtrinsicHex:     codec.HexEncodeToString(raw), ExtrinsicHash: types.Hash(txHash).Hex(),
	}
}

func testSteeringIntent(t *testing.T, epoch uint64) SteeringIntent {
	t.Helper()
	scores, err := rationalJSON([]*big.Rat{big.NewRat(1, 3), big.NewRat(2, 3)})
	if err != nil {
		t.Fatal(err)
	}
	intent := SteeringIntent{
		ValidatorID:         1,
		Netuid:              7,
		SubnetEpoch:         epoch,
		NativeSnapshotBlock: 100,
		NativeSnapshotHash:  "0xnative",
		EVMSnapshotBlock:    98,
		EVMSnapshotHash:     "0xevm",
		SettlementEpoch:     4,
		PolicyHash:          "0xpolicy",
		SelfUID:             5,
		MaskedUIDs:          []uint16{5, 7},
		DepositAudits: []DepositAudit{
			{NoID: 1, Epoch: 4, SourceEpoch: 3, Status: DepositAuditCompliant, Compliant: true, Disposition: "pool_weight_eligible", ConvictionBeforeRao: "0", RequiredDepositRao: "1", ObservedDepositRao: "1"},
			{NoID: 2, Epoch: 4, SourceEpoch: 3, Status: DepositAuditMismatch, Disposition: "zero_pool_weight", ConvictionBeforeRao: "0", RequiredDepositRao: "1", ObservedDepositRao: "0"},
		},
		UIDs:   []uint16{2, 9},
		Scores: scores,
	}
	intent.Prepared = testPreparedSubmission(t, epoch, intent.UIDs)
	return intent
}

func TestIntentStoreDurableLifecycleAndRestartGuard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.Begin(testSteeringIntent(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	if intent.VectorHash == "" || intent.Status != "pending" {
		t.Fatalf("intent = %+v", intent)
	}

	// A fresh process sees the pending write and cannot double-submit it.
	restarted, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Begin(testSteeringIntent(t, 3)); !errors.Is(err, ErrSteeringIntentPending) {
		t.Fatalf("pending restart guard = %v", err)
	}
	if err := restarted.MarkFinalized(intent.VectorHash, intent.Prepared.ExtrinsicHash, 105, "0xfinal", intent.Prepared.RevealBlock, intent.Prepared.Values); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Begin(testSteeringIntent(t, 3)); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("finalized restart guard = %v", err)
	}
	if err := restarted.MarkApplied(intent.VectorHash, 121, "0xapplied"); err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "applied" || current.ApplicationBlock != 121 || current.SelfUID != 5 || len(current.MaskedUIDs) != 2 {
		t.Fatalf("current = %+v", current)
	}

	// The next native epoch archives the complete prior record.
	next, err := restarted.Begin(testSteeringIntent(t, 4))
	if err != nil || next.SubnetEpoch != 4 {
		t.Fatalf("next = %+v, %v", next, err)
	}
}

func TestIntentStoreRejectsRelativeState(t *testing.T) {
	if _, err := NewIntentStore("relative"); err == nil {
		t.Fatal("relative state accepted")
	}
}

func TestSteeringIntentHashBindsProvenSelfMask(t *testing.T) {
	a := testSteeringIntent(t, 3)
	b := a
	b.SelfUID = 6
	b.MaskedUIDs = []uint16{6, 7}
	ha, err := a.computeVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.computeVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Fatal("steering intent hash did not bind the validator self-mask")
	}
}

func TestSteeringIntentVectorHashCommitsDepositAuditEvidence(t *testing.T) {
	store, err := NewIntentStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Begin(testSteeringIntent(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	if err := created.VerifyVectorHash(); err != nil {
		t.Fatal(err)
	}
	created.DepositAudits[0].ObservedDepositRao = "2"
	if err := created.VerifyVectorHash(); err == nil {
		t.Fatal("deposit-audit mutation retained the original steering vector hash")
	}
}

func TestIntentRejectsPreparedAndReceiptMismatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := testSteeringIntent(t, 3)
	mismatch.Prepared.Netuid++
	if _, err := store.Begin(mismatch); err == nil {
		t.Fatal("intent accepted a prepared submission for another netuid")
	}

	intent, err := store.Begin(testSteeringIntent(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFinalized(intent.VectorHash, "0xwrong", 105, "0xfinal", intent.Prepared.RevealBlock, intent.Prepared.Values); err == nil {
		t.Fatal("intent accepted a mismatched finalized receipt")
	}
	current, err := store.Current()
	if err != nil || current.Status != "pending" {
		t.Fatalf("failed receipt mutated pending intent: %+v, %v", current, err)
	}
}
