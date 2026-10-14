//go:build linux || darwin

// Actual persisted V2 ownership, exact signed submissions and independent raw
// chain responses exercise continuation. No receipt or replay verdict is supplied.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

// The actual submit call loses its acknowledgement after recording the exact
// bytes. A later canonical block, not this transport, supplies inclusion proof.
type productionContinuationNativeTestClient struct {
	*recycleAdmissionRouteClient
	fixture           *productionContinuationTestFixture
	broadcasts        []string
	bodyError         error
	bodyErrorKVs      map[uint64]error
	bodyReads         map[uint64]int
	blockNumberKVs    map[string]uint64
	currentReadError  error
	currentReads      int
	beforeCurrentRead func()
	afterBody         func(uint64)
	nonceReads        []uint64
	nonceKVs          map[uint64]uint32
	receiptNumber     uint64
	applied           bool
	call              func(context.Context, any, string, ...any) error
}

func (self *productionContinuationNativeTestClient) Subscribe(ctx context.Context, namespace, subscribe, unsubscribe, notification string, target any, args ...any) (*gsrpcgeth.ClientSubscription, error) {
	if namespace != "author" || subscribe != "submitAndWatchExtrinsic" || unsubscribe != "unwatchExtrinsic" || notification != "extrinsicUpdate" || len(args) != 1 {
		return nil, errors.New("continuation changed the native submit route")
	}
	encoded, ok := args[0].(string)
	if !ok || encoded != self.fixture.intent.Prepared.ExtrinsicHex {
		return nil, errors.New("continuation broadcast differs from the original signed transaction")
	}
	self.broadcasts = append(self.broadcasts, encoded)
	return nil, context.DeadlineExceeded
}

// Canonical bodies, account bytes and events are independently encoded here;
// the real metadata, commitment and ordered-trie readers must authenticate them.
func installProductionContinuationNative(t *testing.T, fixture *productionContinuationTestFixture) *productionContinuationNativeTestClient {
	t.Helper()
	production, prepared := fixture.production, fixture.intent.Prepared
	original := fixture.steerer.native.API.Client.(*recycleAdmissionRouteClient)
	self := &productionContinuationNativeTestClient{recycleAdmissionRouteClient: original, fixture: fixture, nonceKVs: map[uint64]uint32{}, receiptNumber: 101, call: original.CallContext}
	netuid := binary.LittleEndian.AppendUint16(nil, prepared.Netuid)
	public := production.hotkey.PublicKey()
	key := func(pallet, name string, args ...[]byte) string {
		value, err := types.CreateStorageKey(production.metadata, pallet, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		return value.Hex()
	}
	eventsKey := key("System", "Events")
	accountKey := key("System", "Account", public[:])
	commitmentKey := key("Commitments", "CommitmentOf", netuid, public[:])
	lastKey := key("Commitments", "LastCommitment", netuid, public[:])
	weightsKey := key(crv4.PalletName, "Weights", netuid, binary.LittleEndian.AppendUint16(nil, fixture.intent.SelfUID))
	ciphertext, err := codec.HexDecodeString(prepared.CiphertextHex)
	if err != nil {
		t.Fatal(err)
	}
	events := releaseHistoricalReconcileEvents(t, production.metadata, []releaseHistoricalReconcileEvent{
		{pallet: "Commitments", name: "Commitment", fields: []any{types.U16(prepared.Netuid), public}},
		{pallet: "Utility", name: "ItemCompleted"},
		{pallet: "SubtensorModule", name: "TimelockedWeightsCommitted", fields: []any{public, types.U16(prepared.Netuid), blake2b.Sum256(ciphertext), types.U64(prepared.RevealRound)}},
		{pallet: "Utility", name: "ItemCompleted"},
		{pallet: "Utility", name: "BatchCompleted"},
		{pallet: "System", name: "ExtrinsicSuccess", fields: []any{releaseHistoricalReconcileDispatchInfo()}},
	})
	source, err := types.NewHashFromHexString(prepared.SourceCommitment.Hash)
	if err != nil {
		t.Fatal(err)
	}
	info, err := crv4.EncodeFleetCommitmentInfo([32]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	client := &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		assign := func(value any) error {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, target)
		}
		selected := uint64(0)
		if self.blockNumberKVs != nil && len(args) != 0 {
			if hash, ok := args[len(args)-1].(string); ok {
				selected = self.blockNumberKVs[hash]
			}
		} else {
			for number := uint64(100); number <= production.head; number++ {
				if len(args) != 0 && args[len(args)-1] == production.block(number).Hex() {
					selected = number
					break
				}
			}
		}
		if method == "state_getRuntimeVersion" && selected == production.head && selected > self.receiptNumber {
			self.currentReads++
			if self.beforeCurrentRead != nil {
				self.beforeCurrentRead()
			}
			if self.currentReadError != nil {
				return self.currentReadError
			}
		}
		if method == "chain_getBlock" {
			if selected == 0 || len(args) != 1 {
				return errors.New("continuation receipt escaped the canonical fixture chain")
			}
			if self.bodyReads != nil {
				self.bodyReads[selected]++
			}
			if err := self.bodyErrorKVs[selected]; err != nil {
				return err
			}
			if self.bodyError != nil {
				return self.bodyError
			}
			vector := slices.Clone(production.extrinsicsKVs[selected])
			if vector == nil {
				vector = []string{}
			}
			if err := assign(map[string]any{"block": map[string]any{"header": releaseReceiptTestHeaderWire(production.header(selected)), "extrinsics": vector}}); err != nil {
				return err
			}
			if self.afterBody != nil {
				self.afterBody(selected)
			}
			return nil
		}
		if method == "state_getStorage" && len(args) == 2 {
			if args[0] == accountKey {
				if selected == 0 {
					return errors.New("continuation nonce read has no canonical block")
				}
				self.nonceReads = append(self.nonceReads, selected)
				row := make([]byte, 56)
				binary.LittleEndian.PutUint32(row, self.nonceKVs[selected])
				return assign(codec.HexEncodeToString(row))
			}
			if args[0] == weightsKey && self.applied {
				row := releaseNativeValidatorTestCompact(t, uint64(len(prepared.UIDs)))
				for index, uid := range prepared.UIDs {
					row = binary.LittleEndian.AppendUint16(row, uid)
					row = binary.LittleEndian.AppendUint16(row, prepared.Values[index])
				}
				return assign(codec.HexEncodeToString(row))
			}
			if selected == self.receiptNumber && slices.Contains(production.extrinsicsKVs[self.receiptNumber], prepared.ExtrinsicHex) {
				switch args[0] {
				case eventsKey:
					return assign(codec.HexEncodeToString(events))
				case commitmentKey:
					row := binary.LittleEndian.AppendUint64(nil, 1)
					row = binary.LittleEndian.AppendUint32(row, uint32(self.receiptNumber))
					return assign(codec.HexEncodeToString(append(row, info...)))
				case lastKey:
					return assign(codec.HexEncodeToString(binary.LittleEndian.AppendUint32(nil, uint32(self.receiptNumber))))
				}
			}
		}
		return self.call(ctx, target, method, args...)
	}}
	// Use a fresh route wrapper, leaving the original fixture's embedded owner
	// intact; the call delegate never recursively calls this replacement.
	self.recycleAdmissionRouteClient = &recycleAdmissionRouteClient{validatorRuntimeIdentityTestClient: client, route: original.route}
	fixture.steerer.native.API.Client = self
	fixture.steerer.productionReadHooks.wait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	return self
}

func (self *productionContinuationTestFixture) beginAndLoseAcknowledgement(t *testing.T, native *productionContinuationNativeTestClient) *SteeringIntent {
	t.Helper()
	current, err := self.steerer.intents.beginV2(t.Context(), *self.intent)
	if err != nil {
		t.Fatalf("persist the actual signed continuation: %v", err)
	}
	authorized, err := self.steerer.intents.ownerRecyclePreparedConfig(t.Context(), self.steerer.cfg, current.Prepared)
	if err != nil {
		t.Fatal(err)
	}
	_, attempted, err := submitPreparedNativeRuntimeContext(t.Context(), self.steerer.native, authorized, current.Prepared)
	if !attempted || !errors.Is(err, context.DeadlineExceeded) || !slices.Equal(native.broadcasts, []string{current.Prepared.ExtrinsicHex}) {
		t.Fatalf("actual one-send lost acknowledgement: attempted=%t broadcasts=%d error=%v", attempted, len(native.broadcasts), err)
	}
	return current
}

// The original transaction is immortal. Its old epoch remains pending through
// a receipt outage/restart, then finishes with its actual receipt and applied row.
func TestProductionContinuationRecoversOriginalAcrossEpochAndRestart(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	fixture.runtime.progress = &releaseProgress{now: func() time.Time {
		if fixture.steerer.intents.v2.active.Load() {
			t.Fatal("progress escaped before actual intent custody released")
		}
		return time.Unix(2_000_000_000, 0)
	}}
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	before, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	native.bodyError = context.DeadlineExceeded
	var wait *productionSteeringReadWait
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &wait) || wait.phase != productionReadReceipt {
		t.Fatalf("receipt outage lost original pending wait: %v", err)
	}
	fixture.production.head, fixture.production.epoch = 102, pending.SubnetEpoch+1
	fixture.restart(t)
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &wait) {
		t.Fatalf("epoch crossing converted unknown receipt into terminal failure: %v", err)
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("outage or epoch crossing changed signed work: %v", err)
	}
	native.bodyError = nil
	var immortal *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &immortal) || immortal.nativeEpoch != fixture.production.epoch {
		t.Fatalf("a later epoch expired an unresolved immortal signature: %v", err)
	}
	if fixture.restart(t).Status != "pending" || len(native.broadcasts) != 1 {
		t.Fatal("a missed native epoch discarded or replaced the original signed liability")
	}
	// Inclusion extends the already observed canonical prefix; it never
	// rewrites a block previously authenticated as finalized absence.
	native.receiptNumber, fixture.production.head = 103, 104
	fixture.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	native.currentReads = 0
	native.currentReadError = context.DeadlineExceeded
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("retained receipt was blocked by unrelated fresh runtime: %v", err)
	}
	finalized := fixture.restart(t)
	if finalized.Status != "finalized" || finalized.FinalizedBlock != 103 || finalized.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || finalized.CreatedAt != pending.CreatedAt || native.currentReads != 0 {
		t.Fatalf("durable receipt changed original authority or consulted fresh runtime: status=%s reads=%d", finalized.Status, native.currentReads)
	}
	native.currentReadError, native.applied = nil, true
	fixture.production.head = max(uint64(104), finalized.RevealBlock)
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("actual applied row did not finish retained work: %v", err)
	}
	applied := fixture.restart(t)
	if applied.Status != "applied" || applied.SubnetEpoch != pending.SubnetEpoch || applied.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("continuation invented an epoch/signature or lost the original one-send result")
	}
	observed := fixture.runtime.progress.value.Intent
	if observed == nil || !observed.Current || observed.Value == nil || observed.Value.Status != "applied" || observed.Value.CreatedAt != pending.CreatedAt || observed.Value.NativeEpoch != pending.SubnetEpoch || observed.Value.ConfigHash == "" {
		t.Fatalf("actual begin/update/restart omitted original durable progress: %+v", observed)
	}
}

// The newer head contains our transaction. Its nonce is never compared with
// absence that covered only the preceding block; the next scan finds our bytes.
func TestProductionContinuationNonceUsesExactScannedBoundary(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	native.afterBody = func(number uint64) {
		if number == 100 {
			fixture.production.extrinsicsKVs = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}
			fixture.production.head = 101
			native.nonceKVs[101] = pending.Prepared.AccountNonce + 1
			native.afterBody = nil
		}
	}
	var wait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &wait) {
		t.Fatalf("intervening self inclusion was classified as foreign nonce use: %v", err)
	}
	if !slices.Equal(native.nonceReads, []uint64{100}) || len(native.broadcasts) != 1 || fixture.restart(t).Status != "pending" {
		t.Fatalf("nonce crossed receipt coverage: reads=%v broadcasts=%d", native.nonceReads, len(native.broadcasts))
	}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("subsequent exact receipt was stranded: %v", err)
	}
	if fixture.restart(t).Status != "finalized" || len(native.broadcasts) != 1 {
		t.Fatal("self inclusion was replaced or broadcast twice")
	}
}

// A real descriptor is closed before this late fault. Joining a deadline to
// that integrity error must not convert a failed custody read into a wait.
func TestProductionContinuationCustodyFailureStaysHard(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	if _, err := fixture.steerer.intents.beginV2(t.Context(), *fixture.intent); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	hard := errors.New("physical continuation reference close failure")
	closed := 0
	fixture.steerer.intents.v2.referenceReadHooks.afterClose = func(file *os.File) error {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("late control replaced rather than followed physical close: %v", err)
		}
		closed++
		return errors.Join(hard, context.DeadlineExceeded)
	}
	err = fixture.steerer.submitOnceV2(t.Context())
	var wait *productionSteeringReadWait
	if !errors.Is(err, hard) || errors.As(err, &wait) || closed == 0 || fixture.steerer.intents.v2.active.Load() {
		t.Fatalf("failed actual custody was retried or remained owned: closes=%d error=%v", closed, err)
	}
	after, readErr := os.ReadFile(fixture.steerer.intents.path)
	if readErr != nil || !bytes.Equal(before, after) || len(native.broadcasts) != 0 {
		t.Fatalf("failed custody changed signed work or submitted: %v", readErr)
	}
}

// A returned-but-behind finalized head is unknown. A successful contradictory
// canonical hash remains hard; neither case changes the original signed batch.
func TestProductionContinuationRetainedReceiptHandlesNodeLag(t *testing.T) {
	fixture := newReleaseSourceFinalityReadFixture(t)
	fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
		if method == "chain_getFinalizedHead" {
			raw, _ := json.Marshal(fixture.source.native.block.Hex())
			return true, json.Unmarshal(raw, target)
		}
		return false, nil
	}
	err := fixture.verify(t.Context())
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	if !errors.As(err, &unavailable) || !retryableProductionSteeringRead(err) {
		t.Fatalf("behind node turned retained receipt into corruption: %v", err)
	}
	fixture.fault = nil
	if err := fixture.verify(t.Context()); err != nil {
		t.Fatalf("caught-up node did not recover exact receipt: %v", err)
	}
	fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
		if method == "chain_getBlockHash" {
			raw, _ := json.Marshal(types.Hash{0x71}.Hex())
			return true, json.Unmarshal(raw, target)
		}
		return false, nil
	}
	if err := fixture.verify(t.Context()); err == nil || retryableProductionSteeringRead(err) {
		t.Fatalf("canonical contradiction became node-lag wait: %v", err)
	}
}
