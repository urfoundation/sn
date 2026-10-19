// Offline custody tests use synthetic approval/native keys and local files only.
// Barriers force interruption and ambiguous persistence without clocks or nodes.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

// Independent synthetic approval and native keys exercise both signature domains.
type rootOfflineFixture struct {
	storage     *durablefixture.Fixture
	trust       rootOfflineCustodyTrust
	packet      rootOfflineCustodyPacket
	pair        subkey.KeyPair
	approvalKey ed25519.PrivateKey
}

// Approval binds the prepared native bytes before its hash enters the request.
func rootOfflineApprove(t *testing.T, trust rootOfflineCustodyTrust, action rootAction, key ed25519.PrivateKey) rootOfflineCustodyPacket {
	t.Helper()
	normalized := copyRootAction(action)
	normalized.Scope.ApprovalHash, normalized.RequestHash = "", ""
	approval := rootOfflineApproval{Schema: rootOfflineApprovalSchema, Action: normalized}
	message, err := approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	approval.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	action.Scope.ApprovalHash, action.RequestHash = rootObjectHash(approval), ""
	action.RequestHash = rootObjectHash(action)
	packet, err := newRootOfflineCustodyPacket(trust, action, approval)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

// Every fixture has a separately private directory and an explicit mainnet domain.
func newRootOfflineFixture(t *testing.T) rootOfflineFixture {
	t.Helper()
	action, pair, _ := rootActionFixture(t)
	seed := sha256.Sum256([]byte("synthetic offline approval tests only"))
	key := ed25519.NewKeyFromSeed(seed[:])
	directory := filepath.Join(mainnetPrivateTestDir(t), "private-offline-custody")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	trust := rootOfflineCustodyTrust{
		Schema: rootOfflineTrustSchema, NativeChain: action.Scope.NativeChain, GenesisHash: action.Scope.GenesisHash,
		EvmChainId: action.Scope.EvmChainId, Hotkey: action.Scope.Hotkey, CustodyId: action.Scope.CustodyId,
		PolicyHash: action.Scope.PolicyHash, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)),
		StatePath: filepath.Join(directory, "custody.json"),
	}
	prepareMainnetSnapshotTest(t, trust.StatePath, "mainnet-root-offline", rootOfflineStoreLimit)
	return rootOfflineFixture{storage: durablefixture.New(t, t.Context(), directory), trust: trust, packet: rootOfflineApprove(t, trust, action, key), pair: pair, approvalKey: key}
}

// A public signature is produced only by the test's synthetic native device.
func (self rootOfflineFixture) receipt(t *testing.T) rootOfflineSignature {
	t.Helper()
	signer := &rootSignerFixture{pair: self.pair}
	signature, err := signer.signOnce(context.Background(), self.packet.Action)
	if err != nil {
		t.Fatal(err)
	}
	return rootOfflineSignature{Schema: rootOfflineSignatureSchema, PacketHash: self.packet.ContentHash, Signature: hex.EncodeToString(signature)}
}

// Each open instance is closed even when a test ends before explicit restart.
func (self rootOfflineFixture) open(t *testing.T, create bool) (*rootOfflineCustody, *rootOfflineCustodyStore) {
	t.Helper()
	var packet *rootOfflineCustodyPacket
	if create {
		packet = &self.packet
	}
	store, err := openRootOfflineCustodyStore(self.trust, packet, self.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	owner, err := newRootOfflineCustody(self.trust, store)
	if err != nil {
		t.Fatal(err)
	}
	return owner, store
}

// The root action owner can recover original signed bytes after a lost handoff,
// but independent authority remains mandatory before any broadcast.
func TestRootOfflineCustodyActionRecovery(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	custody, custodyStore := fixture.open(t, true)
	action := fixture.packet.Action
	actionStorage := durablefixture.New(t, t.Context(), filepath.Dir(action.Scope.StatePath))
	prepareMainnetSnapshotTest(t, action.Scope.StatePath, "mainnet-root-action", rootActionStoreLimit)
	actionStore, err := openRootActionStore(action.Scope.StatePath, &action, actionStorage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer actionStore.close()
	chain := &rootChainFixture{result: rootActionReconciliation{
		Observation: rootActionObservation{NativeChain: action.Scope.NativeChain, GenesisHash: action.Scope.GenesisHash, EvmChainId: action.Scope.EvmChainId, FinalizedNumber: action.BirthBlock, FinalizedHash: action.BirthHash, RuntimeVersion: action.Scope.RuntimeVersion, RuntimeCodeHash: action.Scope.RuntimeCodeHash, RuntimeMetadataHash: action.Scope.RuntimeMetadataHash, Hotkey: action.Scope.Hotkey, Coldkey: action.Scope.Coldkey, Seat: action.Scope.Seat, AccountNonce: action.Nonce},
		AnchorHash:  action.BirthHash, CheckedFrom: action.BirthBlock + 1, CheckedThrough: action.BirthBlock,
	}}
	owner := &rootActionOwner{store: actionStore, signer: custody, chain: chain}
	if result, err := owner.step(context.Background()); err == nil || result.Phase != "reserved" {
		t.Fatalf("missing independent authority admitted custody: %+v %v", result, err)
	}
	owner.authority = &rootAuthorityFixture{}
	if result, err := owner.step(context.Background()); !errors.Is(err, errRootOfflineSignatureRequired) || result.Phase != "signing" {
		t.Fatalf("unsigned handoff lost its pending intent: %+v %v", result, err)
	}
	if result, err := owner.step(context.Background()); !errors.Is(err, errRootOfflineSignatureRequired) || result.Phase != "signing" {
		t.Fatalf("missing receipt renewed signing: %+v %v", result, err)
	}
	receipt := fixture.receipt(t)
	if err := custody.importSignature(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	if err := custodyStore.close(); err != nil {
		t.Fatal(err)
	}
	owner.signer, _ = fixture.open(t, false)
	owner.authority = nil
	if result, err := owner.step(context.Background()); err != nil || result.Phase != "signed" {
		t.Fatalf("retained old signature was not recovered: %+v %v", result, err)
	}
	record, err := actionStore.load()
	if err != nil || record.Signature != receipt.Signature || record.Broadcasts != 0 {
		t.Fatalf("recovered action differs: %+v %v", record, err)
	}
	if result, err := owner.step(context.Background()); err == nil || result.Status != "blocked" || len(chain.submissions) != 0 {
		t.Fatalf("signature receipt became live authority: %+v %v", result, err)
	}
	rootFixtureFinalize(t, actionStore, chain)
	if result, err := owner.step(context.Background()); err != nil || result.Phase != "finalized" {
		t.Fatalf("old finalized receipt unavailable after authority removal: %+v %v", result, err)
	}
}

// A missing receipt remains unknown across restart; packet exports own their
// vectors and cannot mutate the approved action or its independent signature.
func TestRootOfflineCustodyPendingAndPacketOwnership(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	owner, store := fixture.open(t, true)
	packet, err := owner.packet(context.Background())
	if err != nil || packet.ContentHash != fixture.packet.ContentHash {
		t.Fatal("original packet unavailable", err)
	}
	packet.Action.Dests[0] = 123
	packet.Approval.Action.Weights[0] = 456
	packet, err = owner.packet(context.Background())
	if err != nil || packet.Action.Dests[0] != 0 || packet.Approval.Action.Weights[0] != 10 {
		t.Fatal("packet caller changed retained vectors", err)
	}
	for _, recover := range []func() ([]byte, error){
		func() ([]byte, error) { return owner.signOnce(context.Background(), fixture.packet.Action) },
		func() ([]byte, error) {
			return owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash)
		},
	} {
		if signature, err := recover(); len(signature) != 0 || !errors.Is(err, errRootOfflineSignatureRequired) || errors.Is(err, errRootSignatureNotIssued) {
			t.Fatalf("absence became never-issued: %x %v", signature, err)
		}
	}
	store.close()
	owner, _ = fixture.open(t, false)
	if signature, err := owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash); len(signature) != 0 || !errors.Is(err, errRootOfflineSignatureRequired) {
		t.Fatalf("restart changed unresolved intent: %x %v", signature, err)
	}
	if _, err := owner.recoverSignature(context.Background(), rootObjectHash("another request")); err == nil || errors.Is(err, errRootSignatureNotIssued) {
		t.Fatal("another request admitted or reported never-issued", err)
	}
}

// Rehashing changed action, approval and packet cannot forge independent approval.
func TestRootOfflineCustodyApprovalBindsExactAction(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	for index, change := range []func(*rootAction){
		func(action *rootAction) { action.Nonce++ },
		func(action *rootAction) { action.Scope.FeeReserveRao++ },
		func(action *rootAction) { action.Scope.MaxBroadcasts++ },
		func(action *rootAction) { action.Scope.ValidUntilBlock++ },
		func(action *rootAction) { action.Scope.StatePath += "-replacement" },
		func(action *rootAction) { action.Scope.Coldkey = "0x" + strings.Repeat("ab", 32) },
		func(action *rootAction) { action.Scope.Seat.RegistrationBlock++ },
		func(action *rootAction) { action.Scope.Seat.Uid++ },
		func(action *rootAction) { action.Scope.RuntimeCodeHash = "0x" + strings.Repeat("cd", 32) },
		func(action *rootAction) { action.Scope.RuntimeMetadataHash = "0x" + strings.Repeat("ef", 32) },
		func(action *rootAction) { action.Scope.RuntimeVersion.SpecVersion++ },
		func(action *rootAction) { action.Weights[0]++ },
		func(action *rootAction) { action.Dests[7]++ },
		func(action *rootAction) { action.Period = 32 },
		func(action *rootAction) { action.BirthHash = "0x" + strings.Repeat("ab", 32) },
	} {
		packet := fixture.packet
		packet.Action = copyRootAction(packet.Action)
		change(&packet.Action)
		call, payload, err := packet.Action.encoding()
		if err != nil {
			t.Fatalf("case %d did not preserve a structurally valid native action: %v", index, err)
		}
		packet.Action.Call, packet.Action.Payload = "0x"+hex.EncodeToString(call), "0x"+hex.EncodeToString(payload)
		packet.Approval.Action = copyRootAction(packet.Action)
		packet.Approval.Action.Scope.ApprovalHash, packet.Approval.Action.RequestHash = "", ""
		packet.Action.Scope.ApprovalHash, packet.Action.RequestHash = rootObjectHash(packet.Approval), ""
		packet.Action.RequestHash = rootObjectHash(packet.Action)
		packet.ContentHash = ""
		packet.ContentHash = rootObjectHash(packet)
		if err := packet.validate(fixture.trust); err == nil || !strings.Contains(err.Error(), "approval signature") {
			t.Fatalf("case %d changed authenticated approval: %v", index, err)
		}
	}
}

// Trust inputs are independently provisioned; importing an approval cannot
// change network, hotkey, custody, policy or approval signer identity.
func TestRootOfflineCustodyIndependentTrust(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	for index, change := range []func(*rootOfflineCustodyTrust){
		func(trust *rootOfflineCustodyTrust) { trust.EvmChainId = 945 },
		func(trust *rootOfflineCustodyTrust) { trust.GenesisHash = "0x" + strings.Repeat("ab", 32) },
		func(trust *rootOfflineCustodyTrust) { trust.NativeChain = "another-chain" },
		func(trust *rootOfflineCustodyTrust) { trust.Hotkey = "0x" + strings.Repeat("cd", 32) },
		func(trust *rootOfflineCustodyTrust) { trust.CustodyId += "-other" },
		func(trust *rootOfflineCustodyTrust) { trust.PolicyHash = rootObjectHash("another-policy") },
		func(trust *rootOfflineCustodyTrust) { trust.ApprovalPublicKey = "0x" + strings.Repeat("ef", 32) },
		func(trust *rootOfflineCustodyTrust) { trust.StatePath = fixture.packet.Action.Scope.StatePath },
		func(trust *rootOfflineCustodyTrust) {
			trust.StatePath = fixture.packet.Action.Scope.StatePath + ".lock"
		},
	} {
		trust := fixture.trust
		change(&trust)
		packet := fixture.packet
		packet.TrustHash, packet.ContentHash = rootObjectHash(trust), ""
		packet.ContentHash = rootObjectHash(packet)
		if err := packet.validate(trust); err == nil {
			t.Fatalf("case %d accepted substituted independent trust", index)
		}
	}
	_, store := fixture.open(t, true)
	store.close()
	changed := fixture.trust
	changed.ApprovalPublicKey = "0x" + strings.Repeat("ef", 32)
	if _, err := openRootOfflineCustodyStore(changed, nil, fixture.storage.Context); err == nil {
		t.Fatal("recovery changed independent approval signer")
	}
}

// Invalid and foreign signatures cannot transition intent. Once signed, even
// another genuine signature for the same payload cannot change native bytes.
func TestRootOfflineCustodySignatureIdentity(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	owner, store := fixture.open(t, true)
	receipt := fixture.receipt(t)
	for index, change := range []func(*rootOfflineSignature){
		func(receipt *rootOfflineSignature) { receipt.Schema += "-other" },
		func(receipt *rootOfflineSignature) { receipt.PacketHash = rootObjectHash("foreign-packet") },
		func(receipt *rootOfflineSignature) { receipt.Signature = strings.Repeat("00", 64) },
		func(receipt *rootOfflineSignature) { receipt.Signature = receipt.Signature[:126] },
		func(receipt *rootOfflineSignature) { receipt.Signature += "00" },
		func(receipt *rootOfflineSignature) { receipt.Signature = strings.ToUpper(receipt.Signature) },
	} {
		invalid := receipt
		change(&invalid)
		if err := owner.importSignature(context.Background(), invalid); err == nil {
			t.Fatalf("case %d imported invalid native receipt", index)
		}
		record, err := store.load()
		if err != nil || record.Phase != "requested" {
			t.Fatalf("case %d changed pending intent: %+v %v", index, record, err)
		}
	}
	seed := sha256.Sum256([]byte("synthetic foreign native signer only"))
	foreignPair, err := (sr25519.Scheme{}).FromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	foreignSigner := &rootSignerFixture{pair: foreignPair}
	foreignSignature, err := foreignSigner.signOnce(context.Background(), fixture.packet.Action)
	if err != nil {
		t.Fatal(err)
	}
	foreignReceipt := receipt
	foreignReceipt.Signature = hex.EncodeToString(foreignSignature)
	if err := owner.importSignature(context.Background(), foreignReceipt); err == nil {
		t.Fatal("another native hotkey signed the packet")
	}
	if err := owner.importSignature(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	if err := owner.importSignature(context.Background(), receipt); err != nil {
		t.Fatal("identical receipt was not idempotent", err)
	}
	second := fixture.receipt(t)
	if second.Signature == receipt.Signature {
		t.Fatal("synthetic native device did not produce a distinct valid signature")
	}
	if err := owner.importSignature(context.Background(), second); err == nil {
		t.Fatal("different valid signature replaced retained bytes")
	}
	store.close()
	owner, _ = fixture.open(t, false)
	signature, err := owner.signOnce(context.Background(), fixture.packet.Action)
	if err != nil || hex.EncodeToString(signature) != receipt.Signature {
		t.Fatal("restart did not recover original signature", err)
	}
	signature[0] ^= 1
	signature, err = owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash)
	if err != nil || hex.EncodeToString(signature) != receipt.Signature {
		t.Fatal("signature caller changed retained bytes", err)
	}
}

// Restoring a file after an integrity failure cannot unpoison the open owner.
// Only a fresh locked load can establish the surviving original signed record.
func TestRootOfflineCustodyIntegrityFailureRequiresReopen(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	owner, store := fixture.open(t, true)
	receipt := fixture.receipt(t)
	if err := owner.importSignature(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(fixture.trust.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.trust.StatePath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if signature, err := owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash); err == nil || len(signature) != 0 || errors.Is(err, errRootSignatureNotIssued) {
		t.Fatal("corrupt state returned a signature or renewed issuance", err)
	}
	if err := os.WriteFile(fixture.trust.StatePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.packet(context.Background()); err == nil {
		t.Fatal("restoring a file unpoisoned the custody owner")
	}
	store.close()
	owner, _ = fixture.open(t, false)
	if signature, err := owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash); err != nil || hex.EncodeToString(signature) != receipt.Signature {
		t.Fatal("reopen did not recover surviving exact receipt", err)
	}
}

// Controlled save boundaries model both an uncommitted write and a committed
// receipt whose acknowledgement is lost. The wrapper has the owner's serialization.
type rootOfflineStoreBarrier struct {
	store   rootOfflineCustodyStorage
	entered chan struct{}
	release chan struct{}
	commit  bool
	saveErr error
	loads   int
	saves   int
}

// Counters are read only after the explicit owner barrier or joined operation.
func (self *rootOfflineStoreBarrier) load() (rootOfflineCustodyRecord, error) {
	self.loads++
	return self.store.load()
}

// Tests release every wait before joining its caller; there are no timed sleeps.
func (self *rootOfflineStoreBarrier) save(record rootOfflineCustodyRecord) error {
	self.saves++
	if self.entered != nil {
		close(self.entered)
		<-self.release
	}
	if self.commit {
		if err := self.store.save(record); err != nil {
			return err
		}
	}
	return self.saveErr
}

// Any ambiguous save poisons the current instance. Reopening distinguishes the
// complete surviving record, never an inferred never-issued signing allowance.
func TestRootOfflineCustodyAmbiguousDurability(t *testing.T) {
	for _, commit := range []bool{false, true} {
		fixture := newRootOfflineFixture(t)
		_, store := fixture.open(t, true)
		failure := errors.New("synthetic durability acknowledgement lost")
		barrier := &rootOfflineStoreBarrier{store: store, commit: commit, saveErr: failure}
		owner, err := newRootOfflineCustody(fixture.trust, barrier)
		if err != nil {
			t.Fatal(err)
		}
		receipt := fixture.receipt(t)
		if err := owner.importSignature(context.Background(), receipt); !errors.Is(err, failure) {
			t.Fatalf("commit %v lost injected persistence failure: %v", commit, err)
		}
		if signature, err := owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash); err == nil || len(signature) != 0 || errors.Is(err, errRootSignatureNotIssued) {
			t.Fatalf("commit %v used poisoned state: %x %v", commit, signature, err)
		}
		if err := owner.importSignature(context.Background(), receipt); err == nil || barrier.saves != 1 {
			t.Fatal("poisoned owner retried save", err)
		}
		store.close()
		owner, _ = fixture.open(t, false)
		signature, err := owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash)
		if commit && (err != nil || hex.EncodeToString(signature) != receipt.Signature) || !commit && (!errors.Is(err, errRootOfflineSignatureRequired) || len(signature) != 0) {
			t.Fatalf("commit %v recovery mismatch: %x %v", commit, signature, err)
		}
	}
}

// A canceled waiter cannot touch the journal; cancellation after durable import
// loses only the response, so restart recovers the exact signature normally.
func TestRootOfflineCustodyCancellationAndConcurrentImport(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	_, store := fixture.open(t, true)
	barrier := &rootOfflineStoreBarrier{store: store, entered: make(chan struct{}), release: make(chan struct{}), commit: true}
	owner, err := newRootOfflineCustody(fixture.trust, barrier)
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixture.receipt(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	imported := make(chan error, 1)
	go func() { imported <- owner.importSignature(ctx, receipt) }()
	<-barrier.entered
	loads := barrier.loads
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if _, err := owner.recoverSignature(waitCtx, fixture.packet.Action.RequestHash); !errors.Is(err, context.Canceled) || barrier.loads != loads {
		t.Fatal("canceled waiter entered retained-state owner", err)
	}
	if packet, err := owner.packet(waitCtx); !errors.Is(err, context.Canceled) || packet.ContentHash != "" {
		t.Fatal("canceled export published a packet", err)
	}
	cancel()
	close(barrier.release)
	if err := <-imported; !errors.Is(err, context.Canceled) {
		t.Fatal("interrupted save published a successful response", err)
	}
	if signature, err := owner.recoverSignature(context.Background(), fixture.packet.Action.RequestHash); err != nil || hex.EncodeToString(signature) != receipt.Signature {
		t.Fatal("lost response lost durably imported signature", err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		workers.Go(func() { results <- owner.importSignature(context.Background(), receipt) })
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent identical receipt not idempotent", err)
		}
	}
	if barrier.saves != 1 {
		t.Fatalf("concurrent receipts rewrote durable signature %d times", barrier.saves)
	}
}

// Neither a lost state file nor an interrupted initial marker can renew intent.
func TestRootOfflineCustodyStoreMissingEmptyAndSingleOwner(t *testing.T) {
	for _, remove := range []bool{false, true} {
		fixture := newRootOfflineFixture(t)
		_, store := fixture.open(t, true)
		if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
			t.Fatal("two journal owners admitted")
		}
		store.close()
		var err error
		if remove {
			err = os.Remove(fixture.trust.StatePath)
		} else {
			err = os.WriteFile(fixture.trust.StatePath, nil, 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
			t.Fatal("missing or empty state recovered as fresh intent")
		}
		if _, err := openRootOfflineCustodyStore(fixture.trust, &fixture.packet, fixture.storage.Context); err == nil {
			t.Fatal("missing or empty state allowed recreation")
		}
	}
	fixture := newRootOfflineFixture(t)
	if err := os.WriteFile(fixture.trust.StatePath+".lock", []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
		t.Fatal("incomplete creation marker recovered")
	}
	if _, err := openRootOfflineCustodyStore(fixture.trust, &fixture.packet, fixture.storage.Context); err == nil {
		t.Fatal("incomplete creation marker reused")
	}
}

// Even another genuinely approved action cannot replace this journal's original
// packet. Unknown/duplicate/trailing JSON and oversized files fail on recovery.
func TestRootOfflineCustodyStoreRejectsReplacementAndMalformedState(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	_, store := fixture.open(t, true)
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	store.close()
	replacement := record
	action := copyRootAction(record.Packet.Action)
	action.Scope.FeeReserveRao++
	replacement.Packet = rootOfflineApprove(t, fixture.trust, action, fixture.approvalKey)
	replacement.ContentHash = ""
	replacement.ContentHash = rootObjectHash(replacement)
	replacementRaw, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	for index, invalid := range [][]byte{
		replacementRaw,
		append([]byte(`{"unexpected":true,`), raw[1:]...),
		append([]byte(`{"schema":"duplicate",`), raw[1:]...),
		append(append([]byte(nil), raw...), []byte(` {}`)...),
		bytes.Repeat([]byte(" "), rootOfflineStoreLimit+1),
	} {
		if err := os.WriteFile(fixture.trust.StatePath, invalid, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
			t.Fatalf("case %d loaded altered custody state", index)
		}
	}
}

// Local ownership requires private regular files and no symlink traversal;
// nonblocking opens ensure a FIFO cannot hang state or marker recovery.
func TestRootOfflineCustodyStoreRejectsSpecialAndSymlinkFiles(t *testing.T) {
	fixture := newRootOfflineFixture(t)
	_, store := fixture.open(t, true)
	store.close()
	for _, suffix := range []string{"", ".lock"} {
		path := fixture.trust.StatePath + suffix
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
			t.Fatal("public custody file accepted", suffix)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
			t.Fatal("FIFO custody file accepted", suffix)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), path); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
			t.Fatal("symlink custody file accepted", suffix)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Dir(fixture.trust.StatePath), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := openRootOfflineCustodyStore(fixture.trust, nil, fixture.storage.Context); err == nil {
		t.Fatal("public custody directory accepted")
	}
}
