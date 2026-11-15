// Root service tests force durable-boundary failures with synthetic keys and
// in-memory chain/custody adapters. No test calls a node or loads a key file.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// Public historical metadata exercises the wrapper's advertised inner payment
// extension. It is not current mainnet metadata or independent approval.
func rootActionFixture(t *testing.T) (rootAction, subkey.KeyPair, string) {
	t.Helper()
	metadata, _, _ := rootTestMetadata(t)
	raw, err := codec.Encode(metadata)
	if err != nil {
		t.Fatal(err)
	}
	digest := blake2b.Sum256(raw)
	seed := sha256.Sum256([]byte("synthetic root service tests only"))
	pair, err := (sr25519.Scheme{}).FromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(mainnetPrivateTestDir(t), "private-root-action")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	action := rootAction{
		Scope: rootActionScope{
			Schema: rootActionSchema, Role: "bittensor-root-validator", Netuid: 0, NativeChain: "synthetic-mainnet", EvmChainId: mainnetEvmChainId,
			GenesisHash: "0x" + strings.Repeat("12", 32), RuntimeSourceCommit: rootProfileSource,
			RuntimeVersion:  crv4.RuntimeVersionIdentity{SpecName: "synthetic-runtime", SpecVersion: 991, TransactionVersion: 1, StateVersion: 1},
			RuntimeCodeHash: "0x" + strings.Repeat("34", 32), RuntimeMetadataHash: "0x" + hex.EncodeToString(digest[:]),
			Hotkey: "0x" + hex.EncodeToString(pair.Public()), Coldkey: "0x" + strings.Repeat("56", 32), Seat: rootSeatExpectation{Uid: 3, RegistrationBlock: 11},
			PolicyHash: rootObjectHash("synthetic-explicit-root-policy"), ApprovalHash: rootObjectHash("synthetic-approval-only"), CustodyId: "synthetic-custody", StatePath: filepath.Join(directory, "action.json"),
			Strategy: "explicit_root_weights", FeeReserveRao: 200, ValidUntilBlock: 1088, MaxBroadcasts: 2,
		},
		Dests: []uint16{0, 1, 2, 3, 4, 5, 6, 7}, Weights: []uint16{10, 10, 10, 10, 10, 10, 10, 10}, Nonce: 7, BirthBlock: 1024, BirthHash: "0x" + strings.Repeat("78", 32), Period: 64,
	}
	metadataHex := "0x" + hex.EncodeToString(raw)
	action, err = prepareRootAction(action, metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	return action, pair, metadataHex
}

// Simulates a custody device which persists before returning, including when
// the caller loses its response. Signing twice is a test failure in itself.
type rootSignerFixture struct {
	pair       subkey.KeyPair
	signature  []byte
	request    string
	signs      int
	recoveries int
	returnErr  error
	recoverErr error
}

// Records one real signature, then optionally loses the response.
func (self *rootSignerFixture) signOnce(ctx context.Context, action rootAction) ([]byte, error) {
	self.signs++
	if self.signs != 1 {
		return nil, errors.New("duplicate root signature request")
	}
	self.request = action.RequestHash
	raw, _ := hex.DecodeString(action.Payload[2:])
	if len(raw) > 256 {
		digest := blake2b.Sum256(raw)
		raw = digest[:]
	}
	var err error
	self.signature, err = self.pair.Sign(raw)
	return append([]byte(nil), self.signature...), errors.Join(err, self.returnErr)
}

// Recovery cannot create a signature if custody has no recorded request.
func (self *rootSignerFixture) recoverSignature(ctx context.Context, request string) ([]byte, error) {
	self.recoveries++
	if self.recoverErr != nil {
		return nil, self.recoverErr
	}
	if self.request == "" && self.signature == nil {
		return nil, errRootSignatureNotIssued
	}
	if request != self.request || self.signature == nil {
		return nil, errors.New("custody outcome unknown")
	}
	return append([]byte(nil), self.signature...), nil
}

// A test-only admission port makes every authorization invocation countable.
type rootAuthorityFixture struct {
	calls int
	err   error
}

// Deliberately supplies no production authority or eligibility implementation.
func (self *rootAuthorityFixture) authorize(context.Context, rootAction, rootActionObservation) error {
	self.calls++
	return self.err
}

// Chain state advances only at explicit test transitions, never by elapsed time.
type rootChainFixture struct {
	result       rootActionReconciliation
	reconcileErr error
	submitErr    error
	submissions  [][]byte
}

// Returns an independently copied result like a decoded RPC response.
func (self *rootChainFixture) reconcile(context.Context, rootAction, []byte) (rootActionReconciliation, error) {
	raw, _ := json.Marshal(self.result)
	var copied rootActionReconciliation
	json.Unmarshal(raw, &copied)
	return copied, self.reconcileErr
}

// Captures exact attempted bytes even when acknowledgement is lost.
func (self *rootChainFixture) submit(ctx context.Context, raw []byte) error {
	self.submissions = append(self.submissions, append([]byte(nil), raw...))
	return self.submitErr
}

// Builds an unsigned durable owner with no network access.
func rootOwnerFixture(t *testing.T) (*rootActionOwner, *rootActionStore, *rootSignerFixture, *rootChainFixture) {
	t.Helper()
	action, pair, _ := rootActionFixture(t)
	storage := durablefixture.New(t, t.Context(), filepath.Dir(action.Scope.StatePath))
	prepareMainnetSnapshotTest(t, action.Scope.StatePath, "mainnet-root-action", rootActionStoreLimit)
	store, err := openRootActionStore(action.Scope.StatePath, &action, storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	chain := &rootChainFixture{result: rootActionReconciliation{
		Observation: rootActionObservation{NativeChain: action.Scope.NativeChain, GenesisHash: action.Scope.GenesisHash, EvmChainId: action.Scope.EvmChainId, FinalizedNumber: action.BirthBlock, FinalizedHash: action.BirthHash, RuntimeVersion: action.Scope.RuntimeVersion, RuntimeCodeHash: action.Scope.RuntimeCodeHash, RuntimeMetadataHash: action.Scope.RuntimeMetadataHash, Hotkey: action.Scope.Hotkey, Coldkey: action.Scope.Coldkey, Seat: action.Scope.Seat, AccountNonce: action.Nonce},
		AnchorHash:  action.BirthHash, CheckedFrom: action.BirthBlock + 1, CheckedThrough: action.BirthBlock,
	}}
	signer := &rootSignerFixture{pair: pair}
	return &rootActionOwner{store: store, authority: &rootAuthorityFixture{}, signer: signer, chain: chain}, store, signer, chain
}

// A genuine finalized receipt remains accessible after any local restart.
func rootFixtureFinalize(t *testing.T, store *rootActionStore, chain *rootChainFixture) rootActionRecord {
	t.Helper()
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	chain.result.Observation.FinalizedNumber = record.Action.BirthBlock + 2
	chain.result.Observation.FinalizedHash = "0x" + strings.Repeat("9a", 32)
	chain.result.Observation.AccountNonce = record.Action.Nonce + 1
	chain.result.CheckedThrough = record.Action.BirthBlock + 2
	chain.result.Receipt = &rootActionReceipt{BlockNumber: record.Action.BirthBlock + 1, BlockHash: "0x" + strings.Repeat("bc", 32), RawExtrinsic: record.RawExtrinsic, EventHash: "0x" + strings.Repeat("de", 32), Success: true, ActualFeeRao: 12, ExecutionRuntimeVersion: record.Action.Scope.RuntimeVersion, ExecutionCodeHash: record.Action.Scope.RuntimeCodeHash, ExecutionMetadataHash: record.Action.Scope.RuntimeMetadataHash}
	return record
}

// Fixed SCALE vectors expose accidental immortal or rounded-era encodings.
func TestRootActionMortalBoundaries(t *testing.T) {
	for _, item := range []struct {
		birth, period uint64
		want          string
	}{
		{birth: 1024, period: 64, want: "0500"}, {birth: 1057, period: 64, want: "1502"}, {birth: 1027, period: 4, want: "3100"}, {birth: 1025, period: 256, want: "1700"},
	} {
		raw, err := rootMortalEra(item.birth, item.period)
		if err != nil || hex.EncodeToString(raw) != item.want {
			t.Fatalf("era %d/%d: %x %v", item.birth, item.period, raw, err)
		}
		var era types.ExtrinsicEra
		if err := codec.Decode(raw, &era); err != nil || !era.IsMortalEra || era.IsImmortalEra {
			t.Fatalf("mortal era decode: %+v %v", era, err)
		}
	}
	for _, period := range []uint64{0, 1, 3, 6, 512, 65536} {
		if _, err := rootMortalEra(1024, period); err == nil {
			t.Fatalf("accepted unbounded/noncanonical period %d", period)
		}
	}
	if _, err := rootMortalEra(4294967294, 64); err == nil {
		t.Fatal("accepted block overflow")
	}
}

// Both payload lengths use real signatures; an unrelated key cannot authorize.
func TestRootActionSignatureAndPayloadBounds(t *testing.T) {
	action, pair, metadata := rootActionFixture(t)
	for _, count := range []int{8, 80} {
		action.Dests, action.Weights = make([]uint16, count), make([]uint16, count)
		for index := range action.Dests {
			action.Dests[index], action.Weights[index] = uint16(index), 10
		}
		prepared, err := prepareRootAction(action, metadata)
		if err != nil {
			t.Fatal(err)
		}
		signer := &rootSignerFixture{pair: pair}
		signature, err := signer.signOnce(context.Background(), prepared)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := prepared.signed(signature)
		if err != nil || len(raw) == 0 {
			t.Fatalf("cannot verify %d-destination action: %v", count, err)
		}
		altered := prepared
		altered.Scope.GenesisHash = "0x" + strings.Repeat("ab", 32)
		altered, err = prepareRootAction(altered, metadata)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := altered.signed(signature); err == nil {
			t.Fatal("signature replayed on another genesis")
		}
		signature[0] ^= 1
		if _, err := prepared.signed(signature); err == nil {
			t.Fatal("corrupt signature accepted")
		}
	}
}

// Wrong chain, absent seat authority and accumulation cannot open signing.
func TestRootActionPolicyAdmission(t *testing.T) {
	for _, change := range []func(*rootAction){
		func(a *rootAction) { a.Scope.EvmChainId = 945 },
		func(a *rootAction) { a.Scope.GenesisHash = "" },
		func(a *rootAction) { a.Scope.Seat.RegistrationBlock = 0 },
		func(a *rootAction) { a.Scope.Strategy = "accumulate_in_place" },
		func(a *rootAction) { a.Scope.ApprovalHash = "" },
		func(a *rootAction) { a.Scope.FeeReserveRao = 0 },
		func(a *rootAction) { a.Scope.MaxBroadcasts = 9 },
		func(a *rootAction) { a.Scope.ValidUntilBlock-- },
		func(a *rootAction) { a.Nonce = ^uint32(0) },
	} {
		action, _, metadata := rootActionFixture(t)
		change(&action)
		if _, err := prepareRootAction(action, metadata); err == nil {
			t.Fatal("accepted incomplete or different root approval")
		}
	}
	owner, _, signer, chain := rootOwnerFixture(t)
	owner.authority = nil
	result, err := owner.step(context.Background())
	if err == nil || result.Status != "blocked" || signer.signs != 0 || len(chain.submissions) != 0 {
		t.Fatalf("absent production authority admitted action: %+v %v", result, err)
	}
}

// Reordered, unknown and differently encoded extensions cannot be ignored.
func TestRootActionMetadataProfile(t *testing.T) {
	for _, change := range []func(*types.Metadata){
		func(m *types.Metadata) { m.AsMetadataV14.Extrinsic.SignedExtensions[5].Identifier = "UnknownNonce" },
		func(m *types.Metadata) { e := m.AsMetadataV14.Extrinsic.SignedExtensions; e[3], e[4] = e[4], e[3] },
		func(m *types.Metadata) {
			m.AsMetadataV14.Extrinsic.SignedExtensions = m.AsMetadataV14.Extrinsic.SignedExtensions[:12]
		},
		func(m *types.Metadata) {
			m.AsMetadataV14.Extrinsic.SignedExtensions[7].Identifier = "ChargeTransactionPaymentWrapper"
		},
		func(m *types.Metadata) {
			m.AsMetadataV14.Extrinsic.SignedExtensions[5].Type = m.AsMetadataV14.Extrinsic.SignedExtensions[6].Type
		},
		func(m *types.Metadata) { m.AsMetadataV14.Extrinsic.Version = 5 },
	} {
		_, _, raw := rootActionFixture(t)
		metadata, _, err := crv4.DecodeRuntimeMetadata(raw)
		if err != nil {
			t.Fatal(err)
		}
		change(metadata)
		if _, err := rootSigningProfile(metadata); err == nil {
			t.Fatal("accepted incompatible signing metadata")
		}
	}
}

// A signing timeout means custody may hold a signature; only recovery follows.
func TestRootActionLostSignerResponseRecoversOnce(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	signer.returnErr = context.DeadlineExceeded
	result, err := owner.step(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || result.Phase != "signing" {
		t.Fatalf("signing outcome was lost: %+v %v", result, err)
	}
	restarted := &rootActionOwner{store: store, signer: signer, authority: owner.authority, chain: chain}
	result, err = restarted.step(context.Background())
	if err != nil || result.Phase != "signed" || signer.signs != 1 || signer.recoveries != 1 || len(chain.submissions) != 0 {
		t.Fatalf("restart failed exact recovery: %+v %v signs=%d recoveries=%d", result, err, signer.signs, signer.recoveries)
	}
}

// Explicit before/after-write failures model process death at durable seams.
type rootFailingStore struct {
	store *rootActionStore
	phase string
	after bool
}

// Reads retain the production store's validation and private file semantics.
func (self *rootFailingStore) load() (rootActionRecord, error) {
	return self.store.load()
}

// Injects one unambiguous pre-write or ambiguous post-write failure.
func (self *rootFailingStore) save(record rootActionRecord) error {
	if record.Phase != self.phase {
		return self.store.save(record)
	}
	if self.after {
		if err := self.store.save(record); err != nil {
			return err
		}
	}
	return errors.New("synthetic durable boundary interruption")
}

// A crash after remote signing cannot generate another randomized signature.
func TestRootActionCrashRetainingSignature(t *testing.T) {
	for _, after := range []bool{false, true} {
		owner, store, signer, chain := rootOwnerFixture(t)
		owner.store = &rootFailingStore{store: store, phase: "signed", after: after}
		if _, err := owner.step(context.Background()); err == nil {
			t.Fatal("expected forced signature retention failure")
		}
		if _, err := owner.step(context.Background()); err == nil || signer.signs != 1 || len(chain.submissions) != 0 {
			t.Fatal("poisoned owner performed another side effect")
		}
		restarted := &rootActionOwner{store: store, signer: signer, authority: owner.authority, chain: chain}
		if _, err := restarted.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		if err != nil || record.Signature != hex.EncodeToString(signer.signature) || signer.signs != 1 || (!after && signer.recoveries != 1) {
			t.Fatalf("lost original signed request: %+v %v", record, err)
		}
	}
}

// Pre-broadcast crashes consume an attempt, preserving the original byte set.
func TestRootActionCrashAtBroadcastIntent(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	owner.store = &rootFailingStore{store: store, phase: "pending", after: true}
	if _, err := owner.step(context.Background()); err == nil || len(chain.submissions) != 0 {
		t.Fatal("broadcast escaped failed intent persistence")
	}
	restarted := &rootActionOwner{store: store, signer: signer, authority: owner.authority, chain: chain}
	if _, err := restarted.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, _ := store.load()
	if record.Broadcasts != 2 || len(chain.submissions) != 1 || record.RawExtrinsic != "0x"+hex.EncodeToString(chain.submissions[0]) || signer.signs != 1 {
		t.Fatal("restart replaced bytes or lost attempt ownership")
	}
}

// A lost send acknowledgement is reconciled before any identical-byte resend.
func TestRootActionSubmissionTimeoutKeepsNonceUntilReceipt(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	chain.submitErr = context.DeadlineExceeded
	result, err := owner.step(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || result.Phase != "pending" || result.Status != "pending" {
		t.Fatalf("timeout became terminal failure: %+v %v", result, err)
	}
	rootFixtureFinalize(t, store, chain)
	result, err = owner.step(context.Background())
	if err != nil || result.Phase != "finalized" || signer.signs != 1 || len(chain.submissions) != 1 {
		t.Fatalf("receipt was not reconciled before send: %+v %v", result, err)
	}
}

// A newer runtime cannot authorize rebroadcast, but cannot erase old inclusion.
func TestRootActionUpgradeRetainsOldReceipt(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	chain.result.Observation.RuntimeVersion.SpecVersion++
	chain.result.Observation.RuntimeCodeHash = "0x" + strings.Repeat("ef", 32)
	result, err := owner.step(context.Background())
	if err == nil || result.Status != "blocked" || len(chain.submissions) != 0 {
		t.Fatal("upgraded runtime admitted old request")
	}
	rootFixtureFinalize(t, store, chain)
	result, err = owner.step(context.Background())
	if err != nil || result.Phase != "finalized" || signer.signs != 1 {
		t.Fatalf("upgrade erased historical inclusion: %+v %v", result, err)
	}
}

// Every mutable eligibility generation is checked before a first signature.
func TestRootActionWrongDomainAndStaleSeatNeverSign(t *testing.T) {
	for _, change := range []func(*rootActionObservation){
		func(o *rootActionObservation) { o.EvmChainId = 945 },
		func(o *rootActionObservation) { o.GenesisHash = "0x" + strings.Repeat("ed", 32) },
		func(o *rootActionObservation) { o.RuntimeVersion.TransactionVersion++ },
		func(o *rootActionObservation) { o.RuntimeMetadataHash = "0x" + strings.Repeat("ed", 32) },
		func(o *rootActionObservation) { o.Seat.RegistrationBlock++ },
		func(o *rootActionObservation) { o.Seat.Uid++ },
		func(o *rootActionObservation) { o.Hotkey = o.Coldkey },
		func(o *rootActionObservation) { o.Coldkey = o.Hotkey },
		func(o *rootActionObservation) { o.AccountNonce++ },
	} {
		owner, _, signer, chain := rootOwnerFixture(t)
		change(&chain.result.Observation)
		result, err := owner.step(context.Background())
		if err == nil || result.Status != "blocked" || signer.signs != 0 || len(chain.submissions) != 0 {
			t.Fatalf("stale admission signed: %+v %v", result, err)
		}
	}
}

// Local time and a best-head expiry do not release the retained nonce/fee.
func TestRootActionFinalizedExpiryRequiresCompleteAbsence(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, _ := store.load()
	chain.result.Observation.FinalizedNumber = record.Action.BirthBlock + record.Action.Period
	chain.result.Observation.FinalizedHash = "0x" + strings.Repeat("fc", 32)
	chain.result.CheckedThrough = chain.result.Observation.FinalizedNumber - 2
	if _, err := owner.step(context.Background()); err == nil {
		t.Fatal("incomplete scan released action")
	}
	chain.result.CheckedThrough++
	chain.result.Observation.AccountNonce++
	if _, err := owner.step(context.Background()); err == nil {
		t.Fatal("unexplained consumed nonce released action")
	}
	chain.result.Observation.AccountNonce--
	result, err := owner.step(context.Background())
	if err != nil || result.Phase != "expired" || signer.signs != 1 || len(chain.submissions) != 0 {
		t.Fatalf("complete finalized absence did not settle: %+v %v", result, err)
	}
	if _, err := owner.step(context.Background()); err != nil || signer.signs != 1 {
		t.Fatal("expiry reused signature allowance")
	}
}

// Exceeding the send cap keeps receipt reads alive and never refreshes a nonce.
func TestRootActionBroadcastBoundStillReconciles(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	for index := 0; index < 3; index++ {
		if _, err := owner.step(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.step(context.Background()); err == nil || len(chain.submissions) != 2 || !bytes.Equal(chain.submissions[0], chain.submissions[1]) {
		t.Fatal("broadcast cap or exact bytes lost")
	}
	rootFixtureFinalize(t, store, chain)
	result, err := owner.step(context.Background())
	if err != nil || result.Phase != "finalized" || signer.signs != 1 || len(chain.submissions) != 2 {
		t.Fatalf("exhausted attempts lost receipt: %+v %v", result, err)
	}
}

// Returned failed dispatch and fees remain financial evidence, even over cap.
func TestRootActionRetainsDispatchFailureAndFeeOverrun(t *testing.T) {
	for _, fee := range []uint64{12, 201} {
		owner, store, _, chain := rootOwnerFixture(t)
		if _, err := owner.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		rootFixtureFinalize(t, store, chain)
		chain.result.Receipt.Success = false
		chain.result.Receipt.DispatchError = "synthetic dispatch failure"
		chain.result.Receipt.ActualFeeRao = fee
		result, err := owner.step(context.Background())
		want := "dispatch-failed"
		if fee > 200 {
			want = "fee-overrun"
		}
		if err != nil || result.Phase != want {
			t.Fatalf("actual failed outcome lost: %+v %v", result, err)
		}
		record, err := store.load()
		if err != nil || record.Reconciliation.Receipt.ActualFeeRao != fee {
			t.Fatal("fee evidence did not survive durable read")
		}
	}
}

// Missing/empty required state cannot accidentally resurrect spent authority.
func TestRootActionStoreMissingEmptyAndSingleOwner(t *testing.T) {
	for _, remove := range []bool{false, true} {
		action, _, _ := rootActionFixture(t)
		storage := durablefixture.New(t, t.Context(), filepath.Dir(action.Scope.StatePath))
		prepareMainnetSnapshotTest(t, action.Scope.StatePath, "mainnet-root-action", rootActionStoreLimit)
		store, err := openRootActionStore(action.Scope.StatePath, &action, storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		if other, err := openRootActionStore(action.Scope.StatePath, nil, storage.Context); err == nil {
			other.close()
			t.Fatal("second process acquired owned action")
		}
		store.close()
		if remove {
			err = os.Remove(action.Scope.StatePath)
		} else {
			err = os.WriteFile(action.Scope.StatePath, nil, 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := openRootActionStore(action.Scope.StatePath, nil, storage.Context); err == nil {
			t.Fatal("missing/empty record treated as unused allowance")
		}
		if _, err := openRootActionStore(action.Scope.StatePath, &action, storage.Context); err == nil {
			t.Fatal("ownership marker allowed a replacement action")
		}
	}
}

// Rehashing a different policy cannot escape the immutable marker binding.
func TestRootActionStoreRehashedReplacementRejected(t *testing.T) {
	_, store, _, _ := rootOwnerFixture(t)
	record, _ := store.load()
	record.Action.Scope.PolicyHash = rootObjectHash("synthetic replacement policy")
	record.Action.RequestHash = ""
	record.Action.RequestHash = rootObjectHash(record.Action)
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	raw, _ := json.Marshal(record)
	if err := os.WriteFile(store.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Fatal("rehashed request replacement admitted")
	}
}

// A marker persisted before custody is called cannot strand a recoverable
// request, but its authoritative never-issued proof cannot bypass new drift.
func TestRootActionCrashBeforeSignerCall(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		owner, store, signer, chain := rootOwnerFixture(t)
		owner.store = &rootFailingStore{store: store, phase: "signing", after: true}
		if _, err := owner.step(context.Background()); err == nil || signer.signs != 0 {
			t.Fatal("signer escaped ambiguous intent persistence")
		}
		if upgrade {
			chain.result.Observation.RuntimeVersion.SpecVersion++
		}
		restarted := &rootActionOwner{store: store, signer: signer, authority: owner.authority, chain: chain}
		result, err := restarted.step(context.Background())
		if upgrade {
			if err == nil || result.Status != "blocked" || signer.signs != 0 {
				t.Fatal("never-issued proof bypassed changed runtime admission")
			}
		} else if err != nil || result.Phase != "signed" || signer.signs != 1 {
			t.Fatalf("authoritative pre-sign recovery failed: %+v %v", result, err)
		}
	}
}

// A transient historical GET does not become terminal and needs no fresh plan.
func TestRootActionReadTimeoutRetainsExactAction(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := store.load()
	chain.reconcileErr = context.DeadlineExceeded
	result, err := owner.step(context.Background())
	after, _ := store.load()
	if !errors.Is(err, context.DeadlineExceeded) || result.Status != "pending" || before.ContentHash != after.ContentHash || signer.signs != 1 || len(chain.submissions) != 0 {
		t.Fatal("read timeout consumed or replaced pending action")
	}
	chain.reconcileErr = nil
	if _, err := owner.step(context.Background()); err != nil || signer.signs != 1 || len(chain.submissions) != 1 {
		t.Fatal("transient read could not resume original bytes")
	}
}

// Finalized progress learned during an upgrade block is still durable.
func TestRootActionBlockedProgressRejectsFinalityRollback(t *testing.T) {
	owner, store, _, chain := rootOwnerFixture(t)
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	chain.result.Observation.FinalizedNumber++
	chain.result.Observation.FinalizedHash = "0x" + strings.Repeat("ad", 32)
	chain.result.CheckedThrough++
	chain.result.Observation.RuntimeVersion.SpecVersion++
	if _, err := owner.step(context.Background()); err == nil {
		t.Fatal("upgrade should block")
	}
	record, _ := store.load()
	if record.LastFinalized != chain.result.Observation.FinalizedNumber {
		t.Fatal("blocked action discarded finality progress")
	}
	chain.result.Observation.FinalizedNumber--
	chain.result.Observation.FinalizedHash = record.Action.BirthHash
	chain.result.Observation.RuntimeVersion.SpecVersion--
	chain.result.CheckedThrough--
	if _, err := owner.step(context.Background()); err == nil || len(chain.submissions) != 0 {
		t.Fatal("rollback reopened old signing domain")
	}
}

// Native signatures bind spec/transaction versions, not the runtime code hash.
// A same-version code replacement is an incident whose fee/receipt must survive.
func TestRootActionRetainsExecutionRuntimeDeviation(t *testing.T) {
	owner, store, _, chain := rootOwnerFixture(t)
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	rootFixtureFinalize(t, store, chain)
	chain.result.Receipt.ExecutionCodeHash = "0x" + strings.Repeat("fe", 32)
	result, err := owner.step(context.Background())
	if err != nil || result.Phase != "runtime-deviation" || result.Status != "complete" {
		t.Fatalf("execution deviation was hidden: %+v %v", result, err)
	}
	record, err := store.load()
	if err != nil || record.Reconciliation.Receipt.ActualFeeRao != 12 {
		t.Fatal("runtime deviation discarded actual fee evidence")
	}
}

// Corrupt receipt correspondence is distinct from an unavailable read.
func TestRootActionRejectsWrongReceiptEvidence(t *testing.T) {
	for _, change := range []func(*rootActionReconciliation){
		func(r *rootActionReconciliation) { r.Receipt.RawExtrinsic += "00" },
		func(r *rootActionReconciliation) { r.Receipt.BlockNumber = r.CheckedThrough + 1 },
		func(r *rootActionReconciliation) { r.Receipt.Success = false },
		func(r *rootActionReconciliation) { r.Receipt.EventHash = "" },
		func(r *rootActionReconciliation) { r.Receipt.ExecutionCodeHash = "" },
		func(r *rootActionReconciliation) { r.Observation.GenesisHash = "0x" + strings.Repeat("ed", 32) },
		func(r *rootActionReconciliation) { r.AnchorHash = "0x" + strings.Repeat("ed", 32) },
		func(r *rootActionReconciliation) { r.CheckedFrom++ },
	} {
		owner, store, _, chain := rootOwnerFixture(t)
		if _, err := owner.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		rootFixtureFinalize(t, store, chain)
		change(&chain.result)
		result, err := owner.step(context.Background())
		if err == nil || result.Status != "blocked" || len(chain.submissions) != 0 {
			t.Fatalf("contradictory receipt accepted: %+v %v", result, err)
		}
		record, _ := store.load()
		if record.Reconciliation != nil {
			t.Fatal("bad receipt became durable terminal state")
		}
	}
}

// Joining an unavailable read to a never-issued marker is not affirmative proof.
func TestRootActionAmbiguousCustodyAbsenceCannotSign(t *testing.T) {
	owner, store, signer, chain := rootOwnerFixture(t)
	owner.store = &rootFailingStore{store: store, phase: "signing", after: true}
	if _, err := owner.step(context.Background()); err == nil {
		t.Fatal("expected durable interruption")
	}
	signer.recoverErr = errors.Join(errRootSignatureNotIssued, context.DeadlineExceeded)
	restarted := &rootActionOwner{store: store, signer: signer, authority: owner.authority, chain: chain}
	result, err := restarted.step(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || result.Phase != "signing" || signer.signs != 0 || len(chain.submissions) != 0 {
		t.Fatal("ambiguous not-issued read produced a new signature")
	}
}

// Local artifact tampering cannot turn the root-only codec into a generic call.
func TestRootActionRehashedOtherCallRejected(t *testing.T) {
	action, _, _ := rootActionFixture(t)
	action.Call = "0x0700" + action.Call[6:]
	action.RequestHash = ""
	action.RequestHash = rootObjectHash(action)
	if err := action.validate(); err == nil {
		t.Fatal("root owner accepted another native action")
	}
}

// Special files are rejected without opening a blocking FIFO read.
func TestRootActionStoreRejectsSpecialAndSymlinkFiles(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink", "public"} {
		_, store, _, _ := rootOwnerFixture(t)
		original, err := os.ReadFile(store.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(store.path); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "fifo":
			err = syscall.Mkfifo(store.path, 0600)
		case "symlink":
			target := store.path + ".replacement"
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			err = os.Symlink(target, store.path)
		case "public":
			err = os.WriteFile(store.path, original, 0644)
			if err == nil {
				err = os.Chmod(store.path, 0644)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.load(); err == nil {
			t.Fatalf("accepted %s root action state", kind)
		}
	}
}
