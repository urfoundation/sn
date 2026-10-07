//go:build linux || darwin

// The real original-runtime signer, SCALE event decoder, complete receipt
// reader and source verifier share one physical scripted RPC transcript.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

// No verified outcome is injectable. Faults replace a single wire result or
// transport error after the independently signed original batch is admitted.
type releaseSourceFinalityReadFixture struct {
	source   *releaseHistoricalSourceTestFixture
	receipt  *releaseHistoricalReconcileReceipt
	chain    *crv4.Chain
	events   string
	fault    func(context.Context, any, string, ...any) (bool, error)
	requests []string
}

// Real metadata supplies the event fields and extension encoding. All key,
// block and transaction identities are synthetic and signed by the fixture.
func newReleaseSourceFinalityReadFixture(t *testing.T) *releaseSourceFinalityReadFixture {
	t.Helper()
	source := newReleaseHistoricalSourceTestFixture(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(source.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	prepared := source.intent.Prepared
	hotkey, err := types.NewHashFromHexString(prepared.HotkeyHex)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := hexutil.Decode(prepared.CiphertextHex)
	if err != nil {
		t.Fatal(err)
	}
	digest := blake2b.Sum256(ciphertext)
	events := releaseHistoricalReconcileEvents(t, metadata, []releaseHistoricalReconcileEvent{
		{pallet: "Commitments", name: "Commitment", fields: []any{types.U16(prepared.Netuid), [32]byte(hotkey)}},
		{pallet: "Utility", name: "ItemCompleted"},
		{pallet: "SubtensorModule", name: "TimelockedWeightsCommitted", fields: []any{[32]byte(hotkey), types.U16(prepared.Netuid), digest, types.U64(prepared.RevealRound)}},
		{pallet: "Utility", name: "ItemCompleted"},
		{pallet: "Utility", name: "BatchCompleted"},
		{pallet: "System", name: "ExtrinsicSuccess", fields: []any{releaseHistoricalReconcileDispatchInfo()}},
	})
	receipt := installReleaseHistoricalReconcileReceipt(t, source.native, source.metadataHex, prepared, events)
	historical := *source.native.chain
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), &historical, &source.config, source.native.block); err != nil {
		t.Fatal(err)
	}
	if err := historical.ValidatePreparedSource(prepared); err != nil {
		t.Fatal(err)
	}
	eventsKey, err := types.CreateStorageKey(historical.Meta, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	self := &releaseSourceFinalityReadFixture{source: source, receipt: receipt, chain: &historical, events: eventsKey.Hex()}
	api := *historical.API
	original := api.Client
	api.Client = &releaseHistoricalReconcileClient{receipt: receipt, validatorRuntimeIdentityTestClient: &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		self.requests = append(self.requests, method)
		if self.fault != nil {
			if handled, err := self.fault(ctx, target, method, args...); handled || err != nil {
				return err
			}
		}
		return original.CallContext(ctx, target, method, args...)
	}}}
	historical.API = &api
	return self
}

// Exercise the exported source proof with the unchanged actual signed bytes.
func (self *releaseSourceFinalityReadFixture) verify(ctx context.Context) error {
	transaction, err := types.NewHashFromHexString(self.source.intent.Prepared.ExtrinsicHash)
	if err != nil {
		return err
	}
	return self.chain.VerifyFinalizedSourceContext(ctx, self.source.intent.Prepared, &crv4.FinalizedExtrinsic{ExtrinsicHash: transaction, BlockHash: self.receipt.hash, BlockNumber: self.receipt.number})
}

// Header/canonical reads previously joined a timeout with an invented
// contradiction. The actual strict retry classifier must see only I/O failure.
func TestReleaseSourceFinalityReadPreservesTransportCause(t *testing.T) {
	for _, selected := range []string{"chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash", "chain_getBlock", "events", "commitment"} {
		fixture := newReleaseSourceFinalityReadFixture(t)
		before, err := json.Marshal(fixture.source.intent.Prepared)
		if err != nil {
			t.Fatal(err)
		}
		injected := false
		fixture.fault = func(_ context.Context, _ any, method string, args ...any) (bool, error) {
			matches := selected == method
			if method == "state_getStorage" && len(args) == 2 {
				matches = selected == "events" && args[0] == fixture.events || selected == "commitment" && args[0] != fixture.events
			}
			if matches && !injected {
				injected = true
				return true, context.DeadlineExceeded
			}
			return false, nil
		}
		err = fixture.verify(t.Context())
		if !injected || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
			t.Fatalf("%s source read timeout became a finality contradiction: %v", selected, err)
		}
		if err := fixture.verify(t.Context()); err != nil {
			t.Fatalf("%s source proof did not recover with the original signed batch: %v", selected, err)
		}
		after, err := json.Marshal(fixture.source.intent.Prepared)
		if err != nil || !bytes.Equal(before, after) || fixture.receipt.submissions != 0 || fixture.receipt.subscriptions != 0 {
			t.Fatalf("%s source read recovery changed signed work or submitted: %v", selected, err)
		}
	}
}

// Source events must use the same authenticated body index and same decoded
// storage as dispatch proof; a second mutable response cannot replace either.
func TestReleaseSourceFinalityReadUsesOneAdmittedBodyAndEvents(t *testing.T) {
	fixture := newReleaseSourceFinalityReadFixture(t)
	bodyReads, eventReads := 0, 0
	fixture.fault = func(_ context.Context, _ any, method string, args ...any) (bool, error) {
		if method == "chain_getBlock" {
			bodyReads++
			if bodyReads > 1 {
				return true, errors.New("second source body is deliberately unavailable")
			}
		}
		if method == "state_getStorage" && len(args) == 2 && args[0] == fixture.events {
			eventReads++
			if eventReads > 1 {
				return true, errors.New("second source event vector is deliberately unavailable")
			}
		}
		return false, nil
	}
	if err := fixture.verify(t.Context()); err != nil {
		t.Fatalf("source proof reread an already admitted body or event vector: %v", err)
	}
	if bodyReads != 1 || eventReads != 1 || fixture.receipt.commitmentReads != 2 {
		t.Fatalf("single receipt witness differs: body=%d events=%d commitment=%d", bodyReads, eventReads, fixture.receipt.commitmentReads)
	}
	fixture.fault = nil
	fixture.receipt.registration[len(fixture.receipt.registration)-1] ^= 1
	if err := fixture.verify(t.Context()); err == nil || !strings.Contains(err.Error(), "source metadata readback") {
		t.Fatalf("later invocation reused stale commitment success: %v", err)
	}
}

// Missing results preserve unknown receipt outcome; the typed marker does not
// grant transaction absence, historical approval or signing authority.
func TestReleaseSourceFinalityReadDistinguishesMissingEvidence(t *testing.T) {
	for _, selected := range []string{"head", "header", "canonical", "block", "extrinsics", "events-null", "events-empty", "events-hex-empty"} {
		fixture := newReleaseSourceFinalityReadFixture(t)
		injected := false
		fixture.fault = func(_ context.Context, target any, method string, args ...any) (bool, error) {
			matches := selected == "head" && method == "chain_getFinalizedHead" || selected == "header" && method == "chain_getHeader" || selected == "canonical" && method == "chain_getBlockHash" || (selected == "block" || selected == "extrinsics") && method == "chain_getBlock" || strings.HasPrefix(selected, "events-") && method == "state_getStorage" && len(args) == 2 && args[0] == fixture.events
			if !matches {
				return false, nil
			}
			injected = true
			var result any
			switch selected {
			case "extrinsics":
				result = map[string]any{"block": map[string]any{}}
			case "events-empty":
				result = ""
			case "events-hex-empty":
				result = "0x"
			}
			return true, setReleaseHistoricalTestResult(target, result)
		}
		err := fixture.verify(t.Context())
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		var dispatch *crv4.FinalizedDispatchError
		if !injected || !errors.As(err, &unavailable) || errors.As(err, &dispatch) || fixture.receipt.commitmentReads != 0 {
			t.Fatalf("%s missing evidence became receipt failure/success: %v", selected, err)
		}
		fixture.fault = nil
		if err := fixture.verify(t.Context()); err != nil {
			t.Fatalf("%s missing evidence did not recover from original bytes: %v", selected, err)
		}
	}
}

// Successfully returned contradictory evidence is never downgraded to a read
// wait, even when a timeout or missing-evidence marker is joined to it.
func TestReleaseSourceFinalityReadKeepsContradictionsHard(t *testing.T) {
	for _, selected := range []string{"canonical", "header", "body", "events-malformed", "events-no-success", "dispatch", "source-order", "commitment", "joined-timeout", "pruned"} {
		fixture := newReleaseSourceFinalityReadFixture(t)
		injected := false
		fixture.fault = func(_ context.Context, target any, method string, args ...any) (bool, error) {
			if selected == "canonical" && method == "chain_getBlockHash" {
				injected = true
				return true, setReleaseHistoricalTestResult(target, types.Hash{99}.Hex())
			}
			if selected == "header" && method == "chain_getHeader" {
				injected = true
				header, _ := releaseReceiptTestHeader(t, fixture.source.native.block, fixture.receipt.number+1, fixture.source.intent.Prepared.ExtrinsicHex)
				return true, setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(header))
			}
			if selected == "body" && method == "chain_getBlock" {
				injected = true
				header, _ := releaseReceiptTestHeader(t, fixture.source.native.block, fixture.receipt.number, fixture.source.intent.Prepared.ExtrinsicHex)
				return true, setReleaseHistoricalTestResult(target, map[string]any{"block": map[string]any{"header": releaseReceiptTestHeaderWire(header), "extrinsics": []string{}}})
			}
			if method == "state_getStorage" && len(args) == 2 && args[0] == fixture.events {
				switch selected {
				case "events-malformed":
					injected = true
					return true, setReleaseHistoricalTestResult(target, "0xgg")
				case "events-no-success":
					injected = true
					return true, setReleaseHistoricalTestResult(target, "0x00")
				case "joined-timeout":
					injected = true
					return true, errors.Join(context.DeadlineExceeded, errors.New("independently detected response integrity failure"))
				case "pruned":
					injected = true
					return true, errors.New("State already discarded")
				}
			}
			return false, nil
		}
		switch selected {
		case "dispatch":
			injected = true
			fixture.receipt.events = releaseHistoricalReconcileEvents(t, fixture.chain.Meta, []releaseHistoricalReconcileEvent{{pallet: "System", name: "ExtrinsicFailed", fields: []any{types.DispatchError{IsOther: true}, releaseHistoricalReconcileDispatchInfo()}}})
		case "source-order":
			injected = true
			fixture.receipt.events = releaseHistoricalReconcileEvents(t, fixture.chain.Meta, []releaseHistoricalReconcileEvent{{pallet: "System", name: "ExtrinsicSuccess", fields: []any{releaseHistoricalReconcileDispatchInfo()}}})
		case "commitment":
			injected = true
			fixture.receipt.registration[len(fixture.receipt.registration)-1] ^= 1
		}
		err := fixture.verify(t.Context())
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if !injected || err == nil || errors.As(err, &unavailable) || RetryableEvidenceTransportError(err) {
			t.Fatalf("%s contradiction was accepted or classified as transport: %v", selected, err)
		}
		if fixture.receipt.submissions != 0 || fixture.receipt.subscriptions != 0 {
			t.Fatalf("%s contradiction submitted replacement bytes", selected)
		}
	}
}

// The adjacent prepared-source path must not invent a schedule/version
// contradiction when its exact historical RPC has not returned any evidence.
func TestReleaseSourcePreparationReadPreservesTransportCause(t *testing.T) {
	fixture := newReleaseSourceFinalityReadFixture(t)
	fixture.fault = func(_ context.Context, _ any, method string, _ ...any) (bool, error) {
		if method == "chain_getHeader" {
			return true, context.DeadlineExceeded
		}
		return false, nil
	}
	err := fixture.chain.ValidatePreparedSourceWeightsContext(t.Context(), fixture.source.intent.Prepared, []uint16{1, 2}, []*big.Rat{big.NewRat(1, 1), big.NewRat(2, 1)}, crv4.SubmitOptions{VersionKey: 1})
	if !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
		t.Fatalf("source preparation timeout became a schedule contradiction: %v", err)
	}
}

// Actual SCALE schedule/control values reach each subsequent storage boundary;
// no schedule or runtime-verdict callback substitutes for the production path.
func TestReleaseSourcePreparationControlReadPreservesTransportCause(t *testing.T) {
	for _, selected := range []string{"CommitRevealWeightsEnabled", "CommitRevealWeightsVersion", "MaxWeightsLimit"} {
		fixture := newReleaseSourceFinalityReadFixture(t)
		keyNames := map[string]string{}
		values := map[string][]byte{
			"LastEpochBlock": binary.LittleEndian.AppendUint64(nil, 90), "PendingEpochAt": binary.LittleEndian.AppendUint64(nil, 0),
			"SubnetEpochIndex": binary.LittleEndian.AppendUint64(nil, 1), "BlocksSinceLastStep": binary.LittleEndian.AppendUint64(nil, 10),
			"Tempo": binary.LittleEndian.AppendUint16(nil, 20), "CommitRevealWeightsEnabled": {1},
			"CommitRevealWeightsVersion": binary.LittleEndian.AppendUint16(nil, 4), "MaxWeightsLimit": binary.LittleEndian.AppendUint16(nil, 65535),
		}
		for name := range values {
			var arguments [][]byte
			if name != "CommitRevealWeightsVersion" {
				arguments = [][]byte{binary.LittleEndian.AppendUint16(nil, fixture.source.intent.Prepared.Netuid)}
			}
			key, err := types.CreateStorageKey(fixture.chain.Meta, "SubtensorModule", name, arguments...)
			if err != nil {
				t.Fatal(err)
			}
			keyNames[key.Hex()] = name
		}
		injected := false
		fixture.fault = func(_ context.Context, target any, method string, args ...any) (bool, error) {
			if method == "state_getStorage" && len(args) == 2 && args[1] == fixture.source.native.block.Hex() {
				key, ok := args[0].(string)
				if !ok {
					return true, errors.New("control read key is not a wire string")
				}
				name := keyNames[key]
				if name == selected {
					injected = true
					return true, context.DeadlineExceeded
				}
				if value, found := values[name]; found {
					return true, setReleaseHistoricalTestResult(target, hexutil.Encode(value))
				}
			}
			return false, nil
		}
		err := fixture.chain.ValidatePreparedSourceWeightsContext(t.Context(), fixture.source.intent.Prepared, []uint16{1, 2}, []*big.Rat{big.NewRat(1, 1), big.NewRat(2, 1)}, crv4.SubmitOptions{VersionKey: 1})
		if !injected || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
			t.Fatalf("%s source control timeout became a version contradiction: %v", selected, err)
		}
	}
}

// The physical event read honors cancellation and returns no source proof or
// readback progress after its owner has stopped.
func TestReleaseSourceFinalityReadCancellationRemainsUnknown(t *testing.T) {
	fixture := newReleaseSourceFinalityReadFixture(t)
	entered := make(chan struct{})
	fixture.fault = func(ctx context.Context, _ any, method string, args ...any) (bool, error) {
		if method == "state_getStorage" && len(args) == 2 && args[0] == fixture.events {
			close(entered)
			<-ctx.Done()
			return true, ctx.Err()
		}
		return false, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fixture.verify(ctx) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("source proof did not reach cancellable event read: %v", err)
	}
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) || RetryableEvidenceTransportError(err) || fixture.receipt.commitmentReads != 0 || fixture.receipt.submissions != 0 {
		t.Fatalf("canceled source read changed receipt authority: %v", err)
	}
}

// An absent CommitmentOf is not sufficient: a timed-out LastCommitment keeps
// the slot unknown until both exact-block reads actually establish absence.
func TestReleaseSourceSlotReadPreservesTransportCause(t *testing.T) {
	fixture := newReleaseSourceFinalityReadFixture(t)
	prepared := fixture.source.intent.Prepared
	hotkey, err := types.NewHashFromHexString(prepared.HotkeyHex)
	if err != nil {
		t.Fatal(err)
	}
	commitmentKey, err := types.CreateStorageKey(fixture.chain.Meta, "Commitments", "CommitmentOf", binary.LittleEndian.AppendUint16(nil, prepared.Netuid), hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(fixture.chain.Meta, "Commitments", "LastCommitment", binary.LittleEndian.AppendUint16(nil, prepared.Netuid), hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	commitmentReads, lastReads := 0, 0
	fixture.fault = func(_ context.Context, target any, method string, args ...any) (bool, error) {
		if method != "state_getStorage" || len(args) != 2 || args[1] != fixture.source.native.block.Hex() {
			return false, nil
		}
		if args[0] == commitmentKey.Hex() {
			commitmentReads++
			return true, setReleaseHistoricalTestResult(target, nil)
		}
		if args[0] == lastKey.Hex() {
			lastReads++
			if lastReads == 1 {
				return true, context.DeadlineExceeded
			}
			if lastReads == 3 {
				return true, setReleaseHistoricalTestResult(target, hexutil.Encode(binary.LittleEndian.AppendUint32(nil, 1)))
			}
			return true, setReleaseHistoricalTestResult(target, nil)
		}
		return false, nil
	}
	slot, err := fixture.chain.SourceCommitmentSlotAtContext(t.Context(), prepared.Netuid, [32]byte(hotkey), fixture.source.native.block)
	if slot != nil || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) || commitmentReads != 1 || lastReads != 1 {
		t.Fatalf("absent-slot LastCommitment timeout became an occupied-slot contradiction: %+v %v reads=%d/%d", slot, err, commitmentReads, lastReads)
	}
	slot, err = fixture.chain.SourceCommitmentSlotAtContext(t.Context(), prepared.Netuid, [32]byte(hotkey), fixture.source.native.block)
	if err != nil || slot != nil || commitmentReads != 2 || lastReads != 2 {
		t.Fatalf("recovered slot did not establish exact complete absence: %+v %v reads=%d/%d", slot, err, commitmentReads, lastReads)
	}
	slot, err = fixture.chain.SourceCommitmentSlotAtContext(t.Context(), prepared.Netuid, [32]byte(hotkey), fixture.source.native.block)
	if err == nil || slot != nil || RetryableEvidenceTransportError(err) || !strings.Contains(err.Error(), "occupied LastCommitment") {
		t.Fatalf("occupied LastCommitment became a retry or empty slot: %+v %v", slot, err)
	}
	after, marshalErr := json.Marshal(prepared)
	if marshalErr != nil || !bytes.Equal(before, after) || fixture.receipt.submissions != 0 || fixture.receipt.subscriptions != 0 {
		t.Fatalf("slot recovery changed signed bytes or submitted: %v", marshalErr)
	}
}
