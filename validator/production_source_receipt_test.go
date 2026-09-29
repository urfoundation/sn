//go:build linux || darwin

// Genuine signed source bytes, independently renewed approvals and complete
// committed receipt bodies exercise the production upgrade boundary offline.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/crv4"
	"golang.org/x/crypto/blake2b"
)

// The transport encodes facts only. Production readers still authenticate
// every approved artifact, raw header, body, event and commitment themselves.
type productionSourceReceiptTestFixture struct {
	production *ownerRecycleProductionTestFixture
	current    *ReleaseConfig
	intent     *SteeringIntent
	native     *productionContinuationNativeTestClient
	receipt    *crv4.FinalizedExtrinsic
	eventReads int
	stateReads int
	fault      func(context.Context, any, string, ...any) (bool, error)
}

// A light fixture is sufficient for semantic/capture readers. The durable
// recovery root supplies its real continuation owner to the same transport.
func newProductionSourceReceiptTestFixture(t *testing.T, upgradeDigest bool) *productionSourceReceiptTestFixture {
	t.Helper()
	production := newOwnerRecycleProductionTestFixture(t)
	stage, provider := production.stage(t)
	intent := production.intent(t, stage, provider)
	continuation := &productionContinuationTestFixture{production: production, intent: intent,
		steerer: &ReleaseSteerer{native: production.operator.measurement.admission.chain}}
	return installProductionSourceReceiptTest(t, continuation, upgradeDigest)
}

// The upgrade is installed in block101: block100 executes the original
// signature, while state at101 has a separately approved, unfamiliar spec.
func installProductionSourceReceiptTest(t *testing.T, continuation *productionContinuationTestFixture, upgradeDigest bool) *productionSourceReceiptTestFixture {
	t.Helper()
	production := continuation.production
	admission := production.operator.measurement.admission
	current, _ := productionAuthorityTestSuccessor(t, production.cfg, admission.approval, admission.private, true)
	production.extrinsicsKVs = map[uint64][]string{101: {continuation.intent.Prepared.ExtrinsicHex}}
	productionAuthorityTestUpgrade(t, production, current)
	native := installProductionContinuationNative(t, continuation)
	self := &productionSourceReceiptTestFixture{production: production, current: current, intent: continuation.intent, native: native}
	header, originalHash := production.receiptBlock(101)
	receiptHash := originalHash
	var headerWire map[string]any
	raw, err := json.Marshal(releaseReceiptTestHeaderWire(header))
	if err != nil || json.Unmarshal(raw, &headerWire) != nil {
		t.Fatal("cannot encode the complete synthetic receipt header")
	}
	if upgradeDigest {
		raw, err = codec.Encode(header)
		if err != nil || len(raw) == 0 || raw[len(raw)-1] != 0 {
			t.Fatal("synthetic receipt header does not end in its empty digest vector")
		}
		// The SDK cannot encode digest8. Append its reviewed one-byte wire
		// directly to the one-entry SCALE digest vector, then hash all fields.
		raw = append(raw[:len(raw)-1], 4, 8)
		receiptHash = types.Hash(blake2b.Sum256(raw))
		headerWire["digest"] = map[string]any{"logs": []string{"0x08"}}
	}
	finalizedHash := receiptHash
	var finalizedWire any = headerWire
	if upgradeDigest {
		// Observe the upgrade receipt from the next finalized block. Other
		// native readers still use the SDK for their finalized-head number;
		// the receipt itself must retain its actual digest8 and parent.
		production.head = 102
		finalizedHeader, hash := releaseReceiptTestHeader(t, receiptHash, 102)
		finalizedHash, finalizedWire = hash, releaseReceiptTestHeaderWire(finalizedHeader)
	}
	txHash, err := types.NewHashFromHexString(self.intent.Prepared.ExtrinsicHash)
	if err != nil {
		t.Fatal(err)
	}
	self.receipt = &crv4.FinalizedExtrinsic{ExtrinsicHash: txHash, BlockHash: receiptHash, BlockNumber: 101}
	eventsKey, err := types.CreateStorageKey(production.metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	original := native.validatorRuntimeIdentityTestClient.callContext
	native.validatorRuntimeIdentityTestClient.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if self.fault != nil {
			if handled, err := self.fault(ctx, target, method, args...); handled {
				return err
			}
		}
		// Match the transport's JSON decode for typed hashes/headers, nullable
		// storage and capture's bounded custom result, as well as raw replies.
		assign := func(value any) error { return setReleaseHistoricalTestResult(target, value) }
		if strings.HasPrefix(method, "author_") {
			t.Fatal("historical source receipt attempted a native mutation")
		}
		switch method {
		case "chain_getFinalizedHead":
			return assign(finalizedHash.Hex())
		case "chain_getBlockHash":
			if upgradeDigest && len(args) == 1 && args[0] == uint64(102) {
				return assign(finalizedHash.Hex())
			}
			if len(args) == 1 && args[0] == uint64(101) {
				return assign(receiptHash.Hex())
			}
		case "chain_getHeader":
			if upgradeDigest && len(args) == 1 && args[0] == finalizedHash.Hex() {
				return assign(finalizedWire)
			}
			if len(args) == 1 && args[0] == receiptHash.Hex() {
				return assign(headerWire)
			}
		case "chain_getBlock":
			if upgradeDigest && len(args) == 1 && args[0] == finalizedHash.Hex() {
				return assign(map[string]any{"block": map[string]any{"header": finalizedWire, "extrinsics": []string{}}})
			}
			if len(args) == 1 && args[0] == receiptHash.Hex() {
				return assign(map[string]any{"block": map[string]any{"header": headerWire, "extrinsics": production.extrinsicsKVs[101]}})
			}
		case "state_getStorage":
			if len(args) == 2 && args[1] == receiptHash.Hex() {
				if args[0] == eventsKey.Hex() {
					self.eventReads++
				} else {
					self.stateReads++
				}
			}
		}
		// Only the new digest changes this block identity. The underlying
		// independent state fixture answers the exact same committed state.
		translated := append([]any(nil), args...)
		if len(translated) != 0 && translated[len(translated)-1] == receiptHash.Hex() {
			translated[len(translated)-1] = originalHash.Hex()
		} else if upgradeDigest && len(translated) != 0 && translated[len(translated)-1] == finalizedHash.Hex() {
			translated[len(translated)-1] = production.block(102).Hex()
		}
		return original(ctx, target, method, translated...)
	}
	return self
}

// The semantic archive reader must preserve original authority through an
// upgrade instead of validating old signature fields against new post-state.
func TestProductionSourceReceiptPreservesSignedUpgradeBoundary(t *testing.T) {
	for _, digest := range []bool{false, true} {
		t.Run(fmt.Sprintf("upgrade_digest_%t", digest), func(t *testing.T) {
			fixture := newProductionSourceReceiptTestFixture(t, digest)
			intent := *fixture.intent
			intent.FinalizedBlock, intent.FinalizedBlockHash = fixture.receipt.BlockNumber, fixture.receipt.BlockHash.Hex()
			native := fixture.production.operator.measurement.admission.chain
			metadata, runtime := native.Meta, native.Runtime
			before, _ := json.Marshal(fixture.intent)
			if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), native, fixture.current, &intent, fixture.production.operator.measurement.provider.artifact); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(fixture.intent)
			if fixture.eventReads != 1 || fixture.stateReads != 2 || !bytes.Equal(before, after) || native.Meta != metadata || native.Runtime != runtime || len(fixture.native.broadcasts) != 0 {
				t.Fatal("receipt proof changed original bytes/view or skipped actual events and commitment reads")
			}
			original, err := productionConfigForIntent(fixture.current, &intent)
			if err != nil || !original.ownerRecycleProduction.historicalOnly || original.RuntimeSpec == fixture.current.RuntimeSpec || ownerRecycleProductionBoundary(original) == nil {
				t.Fatalf("receipt proof lost original read-only approval: %v", err)
			}
		})
	}
}

// Exact block proofs cannot be swapped, stripped or used to admit an old
// signature under a later execution spec. Neither view grants fresh signing.
func TestProductionSourceReceiptRequiresSeparateAuthenticatedViews(t *testing.T) {
	fixture := newProductionSourceReceiptTestFixture(t, false)
	native := *fixture.production.operator.measurement.admission.chain
	parent := fixture.production.block(100)
	if err := authenticateProductionSourceRuntimeAtContext(t.Context(), &native, fixture.production.cfg, parent); err != nil {
		t.Fatal(err)
	}
	execution, _, err := authenticateOwnerRecycleProductionArtifactWithHeadersAtContext(t.Context(), &native, fixture.current, parent, true, true)
	if err != nil {
		t.Fatal(err)
	}
	postState, _, err := authenticateOwnerRecycleProductionArtifactWithHeadersAtContext(t.Context(), &native, fixture.current, fixture.receipt.BlockHash, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := native.VerifyFinalizedSourceRuntimeContext(t.Context(), fixture.intent.Prepared, fixture.receipt, execution, postState); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"execution-post-state", "post-state-parent", "stripped-proof", "source-post-state"} {
		view, selectedExecution, selectedPostState := native, execution, postState
		switch fault {
		case "execution-post-state":
			selectedExecution = postState
		case "post-state-parent":
			selectedPostState = execution
		case "stripped-proof":
			selectedPostState = crv4.AuthenticatedRuntimeArtifact{BlockHash: postState.BlockHash, Version: postState.Version,
				CodeHash: postState.CodeHash, MetadataHash: postState.MetadataHash, Metadata: postState.Metadata, GenesisHash: postState.GenesisHash}
		case "source-post-state":
			if err := view.BindRuntimeArtifact(postState); err != nil {
				t.Fatal(err)
			}
		}
		reads := fixture.eventReads
		if err := view.VerifyFinalizedSourceRuntimeContext(t.Context(), fixture.intent.Prepared, fixture.receipt, selectedExecution, selectedPostState); err == nil || fixture.eventReads != reads {
			t.Fatalf("%s runtime authority reached event decoding: %v", fault, err)
		}
	}
	if validateReleaseNativeSigningRuntime(&native, fixture.current) == nil {
		t.Fatal("historical source receipt view acquired current signing authority")
	}
	// A later block executed by the successor cannot contain an accepted
	// old-spec signature merely because its post-state interface matches.
	header, hash := releaseReceiptTestHeader(t, fixture.receipt.BlockHash, 102, fixture.intent.Prepared.ExtrinsicHex)
	fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
		if len(args) == 0 || args[len(args)-1] != hash.Hex() {
			return false, nil
		}
		if method == "chain_getHeader" {
			return true, setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(header))
		}
		if method == "state_getRuntimeVersion" || method == "state_getMetadata" || method == "state_getStorageHash" {
			translated := append([]any(nil), args...)
			translated[len(translated)-1] = fixture.receipt.BlockHash.Hex()
			return true, native.API.Client.CallContext(ctx, target, method, translated...)
		}
		return false, nil
	}
	laterState, err := crv4.AuthenticateRuntimeArtifactAtContext(t.Context(), &native, hash, releaseRuntimeIdentityV2(fixture.current))
	if err != nil {
		t.Fatal(err)
	}
	laterReceipt := *fixture.receipt
	laterReceipt.BlockHash, laterReceipt.BlockNumber = hash, 102
	reads := fixture.eventReads
	if err := native.VerifyFinalizedSourceRuntimeContext(t.Context(), fixture.intent.Prepared, &laterReceipt, postState, laterState); err == nil || !strings.Contains(err.Error(), "runtime differs from independent signing authority") || fixture.eventReads != reads {
		t.Fatalf("original signed spec was admitted under successor execution: %v", err)
	}
}

// Even an independently signed current config cannot authorize a changed
// consumed interface; its exact incompatible metadata must remain a hard stop.
func TestProductionSourceReceiptRejectsApprovedUnsupportedInterface(t *testing.T) {
	fixture := newProductionSourceReceiptTestFixture(t, false)
	metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.production.operator.measurement.admission.metadata)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for palletIndex := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[palletIndex]
		if pallet.Name == "Commitments" {
			for itemIndex := range pallet.Storage.Items {
				item := &pallet.Storage.Items[itemIndex]
				if item.Name == "LastCommitment" {
					item.Fallback = append(item.Fallback, 0xff)
					changed = true
				}
			}
		}
	}
	if !changed {
		t.Fatal("missing consumed commitment fixture storage")
	}
	wire, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := crv4.DecodeRuntimeMetadata(wire)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := ownerRecycleProductionApproval(fixture.current)
	if err != nil {
		t.Fatal(err)
	}
	fixture.current.RuntimeMetadataHash = digest
	approval := approved.Approval
	approval.Proposal.Runtime.MetadataHash, _ = parseHash32("synthetic changed metadata", digest)
	approval.ConfigHash, err = OwnerRecycleConfigHash(fixture.current)
	if err != nil {
		t.Fatal(err)
	}
	approver := recycleAdmissionFixture{cfg: fixture.current, approval: approval, private: fixture.production.operator.measurement.admission.private}
	approver.sign(t)
	if err := loadOwnerRecycleProductionConfig(fixture.current); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(fixture.current); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionAuthorityHistory(fixture.current); err != nil {
		t.Fatal(err)
	}
	fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
		if method == "state_getMetadata" && len(args) == 1 && args[0] == fixture.receipt.BlockHash.Hex() {
			return true, setReleaseHistoricalTestResult(target, wire)
		}
		return false, nil
	}
	intent := *fixture.intent
	intent.FinalizedBlock, intent.FinalizedBlockHash = 101, fixture.receipt.BlockHash.Hex()
	err = authenticateReleaseNativeSourceReferenceV2(t.Context(), fixture.production.operator.measurement.admission.chain, fixture.current, &intent, fixture.production.operator.measurement.provider.artifact)
	if err == nil || !strings.Contains(err.Error(), "consumed interface storage/Commitments.LastCommitment changed") || fixture.eventReads != 0 {
		t.Fatalf("independently approved changed storage reached receipt decode: %v", err)
	}
}

// Missing history, altered code, incomplete canonical headers and archive
// unavailability never become dispatch failures or successful finalization.
func TestProductionSourceReceiptRejectsUnapprovedOrUnavailableEvidence(t *testing.T) {
	fixture := newProductionSourceReceiptTestFixture(t, false)
	intent := *fixture.intent
	intent.FinalizedBlock, intent.FinalizedBlockHash = 101, fixture.receipt.BlockHash.Hex()
	canary := errors.New("synthetic receipt archive unavailable")
	expectedKVs := map[string]string{
		"no-renewal":          "unreviewed identity",
		"unapproved-code":     "observed runtime code hash",
		"header-substitution": "receipt header SCALE hash differs",
	}
	for _, fault := range []string{"no-renewal", "unapproved-code", "header-substitution", "archive-unavailable", "cancelled"} {
		cfg := fixture.current
		ctx, cancel := context.WithCancel(t.Context())
		fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
			if fault == "unapproved-code" && method == "state_getStorageHash" && len(args) == 2 && args[1] == fixture.receipt.BlockHash.Hex() {
				return true, setReleaseHistoricalTestResult(target, types.Hash{0xe1}.Hex())
			}
			if method == "chain_getHeader" && len(args) == 1 && args[0] == fixture.receipt.BlockHash.Hex() {
				if fault == "header-substitution" {
					return true, setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(fixture.production.header(100)))
				}
				if fault == "archive-unavailable" {
					return true, canary
				}
			}
			return false, nil
		}
		if fault == "no-renewal" {
			cfg = fixture.production.cfg
		}
		if fault == "cancelled" {
			cancel()
		}
		err := authenticateReleaseNativeSourceReferenceV2(ctx, fixture.production.operator.measurement.admission.chain, cfg, &intent, fixture.production.operator.measurement.provider.artifact)
		cancel()
		var dispatch *crv4.FinalizedDispatchError
		if err == nil || errors.As(err, &dispatch) || fixture.eventReads != 0 || fault == "archive-unavailable" && !errors.Is(err, canary) || fault == "cancelled" && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s evidence escaped its actual refusal: %v", fault, err)
		}
		if expected := expectedKVs[fault]; expected != "" && !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s failed before its intended authority boundary: %v", fault, err)
		}
	}
}

// Finalized dispatch failure remains typed; a mismatching historical storage
// slot remains an integrity error rather than success or a retryable absence.
func TestProductionSourceReceiptPreservesDispatchAndStateFailures(t *testing.T) {
	fixture := newProductionSourceReceiptTestFixture(t, false)
	intent := *fixture.intent
	intent.FinalizedBlock, intent.FinalizedBlockHash = 101, fixture.receipt.BlockHash.Hex()
	eventsKey, err := types.CreateStorageKey(fixture.production.metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	public := fixture.production.hotkey.PublicKey()
	netuid, err := codec.Encode(types.U16(intent.Prepared.Netuid))
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(fixture.production.metadata, "Commitments", "LastCommitment", netuid, public[:])
	if err != nil {
		t.Fatal(err)
	}
	failed := releaseHistoricalReconcileEvents(t, fixture.production.metadata, []releaseHistoricalReconcileEvent{
		{pallet: "System", name: "ExtrinsicFailed", fields: []any{types.DispatchError{IsOther: true}, releaseHistoricalReconcileDispatchInfo()}},
	})
	for _, dispatchFailure := range []bool{true, false} {
		fixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
			if method == "state_getStorage" && len(args) == 2 && args[1] == fixture.receipt.BlockHash.Hex() {
				if dispatchFailure && args[0] == eventsKey.Hex() {
					return true, setReleaseHistoricalTestResult(target, codec.HexEncodeToString(failed))
				}
				if !dispatchFailure && args[0] == lastKey.Hex() {
					return true, setReleaseHistoricalTestResult(target, "0x64000000")
				}
			}
			return false, nil
		}
		err := authenticateReleaseNativeSourceReferenceV2(t.Context(), fixture.production.operator.measurement.admission.chain, fixture.current, &intent, fixture.production.operator.measurement.provider.artifact)
		var dispatch *crv4.FinalizedDispatchError
		if err == nil || errors.As(err, &dispatch) != dispatchFailure || dispatchFailure && dispatch.BlockHash != fixture.receipt.BlockHash ||
			!dispatchFailure && !strings.Contains(err.Error(), "differs from LastCommitment") {
			t.Fatalf("dispatch=%t changed historical failure semantics: %v", dispatchFailure, err)
		}
	}
}

// Capture explicitly retains the parent artifact even when its metadata is
// already cached, so offline replay keeps the same source/execution/state split.
func TestProductionSourceReceiptCaptureRetainsExecutionMetadata(t *testing.T) {
	fixture := newProductionSourceReceiptTestFixture(t, true)
	intent := *fixture.intent
	intent.FinalizedBlock, intent.FinalizedBlockHash = 101, fixture.receipt.BlockHash.Hex()
	metadataKVs := map[string]bool{}
	measurement := fixture.production.operator.measurement
	err := CaptureReleaseNativeSourceV2(t.Context(), measurement.admission.chain, fixture.current, &intent, measurement.encoded,
		func(ctx context.Context, read ReleaseEvidenceV2NativeRead) error {
			if read.Method == "state_getMetadata" {
				var parameters []string
				if err := json.Unmarshal(read.Parameters, &parameters); err != nil || len(parameters) != 1 {
					return errors.New("captured metadata lost its exact block")
				}
				metadataKVs[parameters[0]] = true
			}
			return ctx.Err()
		})
	if err != nil || !metadataKVs[fixture.production.block(100).Hex()] || !metadataKVs[fixture.receipt.BlockHash.Hex()] || fixture.eventReads != 1 || fixture.stateReads != 2 {
		t.Fatalf("cold source capture lost execution or post-state evidence: metadata=%v error=%v", metadataKVs, err)
	}
}

// The real pending owner reconciles its original signature through the
// compatible upgrade, persists finality and reopens without a second send.
func TestProductionSourceReceiptRecoversOriginalPending(t *testing.T) {
	continuation := newProductionContinuationTestFixture(t)
	pending, err := continuation.steerer.intents.beginV2(t.Context(), *continuation.intent)
	if err != nil {
		t.Fatal(err)
	}
	fixture := installProductionSourceReceiptTest(t, continuation, false)
	continuation.runtime.cfg = *fixture.current
	continuation.steerer.cfg = &continuation.runtime.cfg
	if done, err := continuation.steerer.reconcilePendingV2(t.Context(), pending, nil); err != nil || !done {
		t.Fatalf("actual retained production receipt did not finalize: done=%t error=%v", done, err)
	}
	finalized := continuation.restart(t)
	if finalized.Status != "finalized" || finalized.FinalizedBlockHash != fixture.receipt.BlockHash.Hex() || finalized.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex ||
		finalized.Prepared.SourceCommitment.RuntimeSpec == fixture.current.RuntimeSpec || finalized.CreatedAt != pending.CreatedAt || len(fixture.native.broadcasts) != 0 {
		t.Fatal("durable upgrade recovery rewrote original authority, source or custody")
	}
}
