//go:build linux || darwin

// Every authority selector consumes committed header coordinates. Generated
// signed approvals and actual Rpc readers expose adjacent admission bypasses.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Independent review reproduced this against889: the producer gate refused
// the same250→150 substitution that the upload freshness selector accepted.
func TestAdjacentHeaderUploadRejectsSignedWindowSubstitution(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	owner := fixture.owner(t)
	native := fixture.production.rpc.native
	fixture.production.rpc.head = 250
	hash := mainnetRuntimeTestBlock(250)
	client := native.API.Client.(*validatorRuntimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getHeader" && len(args) == 1 && args[0] == hash.Hex() {
			header := mainnetRuntimeTestHeader(250)
			header.Number = 150
			return setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(header))
		}
		if method == "chain_getBlockHash" && len(args) == 1 && args[0] == uint64(150) {
			return setReleaseHistoricalTestResult(result, hash.Hex())
		}
		return original(ctx, result, method, args...)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, fixture.production.cfg, hash); err == nil || !strings.Contains(err.Error(), "header SCALE hash") {
		t.Fatalf("producer control did not reject committed-height substitution: %v", err)
	}
	got, err := ValidatorUploadNativeObserverContext(t.Context(), native, owner.config.Deployment)
	if err == nil || got != (ValidatorUploadNativeObserver{}) || !strings.Contains(err.Error(), "header SCALE hash") {
		t.Fatalf("upload acquired signed-window authority from250→150: %+v %v", got, err)
	}
}

// Tag8 remains a valid committed header under the original current authority.
func TestAdjacentHeaderUploadAcceptsApprovedUpgrade(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	owner := fixture.owner(t)
	hash := installProductionRuntimeUpgradeHeaderTest(t, fixture.production, 150)
	native := fixture.production.rpc.native
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, fixture.production.cfg, hash); err != nil {
		t.Fatal(err)
	}
	got, err := ValidatorUploadNativeObserverContext(t.Context(), native, owner.config.Deployment)
	if err != nil || got.Number != 150 || got.Hash != hash || got.TimestampMillis != fixture.upload.currentMillis {
		t.Fatalf("approved upgrade lost upload observation: %+v %v", got, err)
	}
}

// Artifact cache reuse cannot conceal an opening or closing canonical switch.
func TestAdjacentHeaderUploadRejectsCanonicalSwitch(t *testing.T) {
	for _, warm := range []bool{false, true} {
		for _, closing := range []bool{false, true} {
			fixture := newValidatorUploadProductionTestFixture(t)
			owner := fixture.owner(t)
			native := fixture.production.rpc.native
			if warm {
				if _, err := ValidatorUploadNativeObserverContext(t.Context(), native, owner.config.Deployment); err != nil {
					t.Fatal(err)
				}
			}
			client := native.API.Client.(*validatorRuntimeIdentityTestClient)
			original := client.callContext
			reads := 0
			timestampRead := false
			client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
				if method == "chain_getBlockHash" && len(args) == 1 && args[0] == uint64(150) {
					reads++
					if !closing || timestampRead {
						return setReleaseHistoricalTestResult(result, types.Hash{0xee}.Hex())
					}
				}
				err := original(ctx, result, method, args...)
				if err == nil && method == "state_getStorage" {
					timestampRead = true
				}
				return err
			}
			got, err := ValidatorUploadNativeObserverContext(t.Context(), native, owner.config.Deployment)
			if err == nil || got != (ValidatorUploadNativeObserver{}) {
				t.Fatalf("upload retained canonical switch warm=%t closing=%t: %+v %v", warm, closing, got, err)
			}
			if closing && (!timestampRead || reads != 3) {
				t.Fatalf("closing control did not reach both canonical reads: %d %v", reads, err)
			}
		}
	}
}

// An independently signed observation profile is read-only authority, but its
// finite window must still follow committed coordinates at current/old heads.
func TestAdjacentHeaderObservationRejectsSignedWindowSubstitution(t *testing.T) {
	for _, historical := range []bool{false, true} {
		fixture := newMainnetRuntimeTestFixture(t)
		cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		fixture.head = 250
		requested := types.Hash{}
		if historical {
			fixture.head = 300
			requested = mainnetRuntimeTestBlock(250)
		}
		hash := mainnetRuntimeTestBlock(250)
		client := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
		original := client.callContext
		client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
			if method == "chain_getHeader" && args[0] == hash.Hex() {
				header := mainnetRuntimeTestHeader(250)
				header.Number = 150
				return setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(header))
			}
			if method == "chain_getBlockHash" && args[0] == uint64(150) {
				return setReleaseHistoricalTestResult(result, hash.Hex())
			}
			return original(ctx, result, method, args...)
		}
		oldMeta, oldRuntime := fixture.native.Meta, fixture.native.Runtime
		got, err := ObserveMainnetRuntimeAtContext(t.Context(), fixture.native, cfg, requested)
		if err == nil || got != nil || !strings.Contains(err.Error(), "header SCALE hash") {
			t.Fatalf("observation selected signed window from uncommitted height historical=%t: %+v %v", historical, got, err)
		}
		if fixture.native.Meta != oldMeta || fixture.native.Runtime != oldRuntime || fixture.callKVs["state_getRuntimeVersion"] != 0 {
			t.Fatal("observation selected or rebound runtime before authenticating header")
		}
	}
}

// A historical target cannot retain a finalized witness replaced while its
// runtime was read, even if the target's own canonical hash stays unchanged.
func TestAdjacentHeaderObservationRejectsClosingFinalitySwitch(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	client := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getStorageHash" {
			fixture.canonicalHashKVs[150] = types.Hash{0xee}
		}
		return original(ctx, result, method, args...)
	}
	got, err := ObserveMainnetRuntimeAtContext(t.Context(), fixture.native, cfg, mainnetRuntimeTestBlock(100))
	if err == nil || got != nil {
		t.Fatalf("historical observation retained replaced finality witness: %+v %v", got, err)
	}
}

// The concrete startup stake reader cannot publish queried height50 for the
// committed block100 merely because the same Rpc agrees with that number.
func TestAdjacentHeaderStartupStakeRejectsHeightSubstitution(t *testing.T) {
	fixture := newReleaseNativeValidatorTestFixture(t)
	fixture.blockNumber = 50
	got, err := readReleaseNativeValidatorAtContext(fixture.ctx, fixture.chain, fixture.genesis, 521, fixture.hotkey, fixture.uid, fixture.expected)
	if err == nil || got.Identity.BlockNumber != 0 || !strings.Contains(err.Error(), "header SCALE hash") {
		t.Fatalf("startup stake published uncommitted coordinates: %+v %v", got, err)
	}
}

// Retained approval authorizes100–200, while the actual header commits250.
// Consistent forged canonical answers and exact storage cannot backdate it.
func TestAdjacentHeaderRecycleCensusRejectsSignedWindowSubstitution(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	header, hash := releaseReceiptTestHeader(t, types.Hash{2}, 250)
	fixture.finalized = hash
	client := fixture.chain.API.Client.(*recycleAdmissionRouteClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getHeader" && args[0] == hash.Hex() {
			header.Number = 100
			return setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(header))
		}
		return original(ctx, result, method, args...)
	}
	got, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain)
	if err == nil || got != nil || !strings.Contains(err.Error(), "header SCALE hash") {
		t.Fatalf("recycle census backdated committed250 into signed window: %+v %v", got, err)
	}
}

// Even when the number is unchanged, a missing root or changed opaque digest
// cannot become freshness evidence under a valid production config.
func TestAdjacentHeaderUploadRejectsIncompleteCommitment(t *testing.T) {
	for _, missing := range []bool{false, true} {
		fixture := newValidatorUploadProductionTestFixture(t)
		owner := fixture.owner(t)
		native := fixture.production.rpc.native
		client := native.API.Client.(*validatorRuntimeIdentityTestClient)
		original := client.callContext
		client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
			if method == "chain_getHeader" {
				encoded, err := json.Marshal(releaseReceiptTestHeaderWire(mainnetRuntimeTestHeader(150)))
				if err != nil {
					return err
				}
				var wire map[string]any
				if err := json.Unmarshal(encoded, &wire); err != nil {
					return err
				}
				if missing {
					delete(wire, "stateRoot")
				} else {
					wire["digest"] = map[string]any{"logs": []string{"0x08"}}
				}
				return setReleaseHistoricalTestResult(result, wire)
			}
			return original(ctx, result, method, args...)
		}
		got, err := ValidatorUploadNativeObserverContext(t.Context(), native, owner.config.Deployment)
		if err == nil || got != (ValidatorUploadNativeObserver{}) {
			t.Fatalf("upload retained incomplete or substituted commitment missing=%t: %+v %v", missing, got, err)
		}
	}
}

// The legacy durable application path re-reads the header after runtime
// admission. A second Rpc answer cannot rewrite its original applied receipt.
func TestAdjacentHeaderApplicationPreservesOriginalJournal(t *testing.T) {
	for _, fault := range []string{"none", "number", "root", "closing"} {
		fixture := newProductionRuntimeTestFixture(t, false)
		store, err := NewIntentStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		intent, err := store.Begin(testSteeringIntent(t, store.stateDir, 3, ""))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.MarkFinalized(intent.VectorHash, intent.Prepared.ExtrinsicHash, 105, types.Hash{0x41}.Hex(), intent.Prepared.RevealBlock, intent.Prepared.Values); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.path)
		if err != nil {
			t.Fatal(err)
		}
		metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.rpc.metadata)
		if err != nil {
			t.Fatal(err)
		}
		key, err := types.CreateStorageKey(metadata, crv4.PalletName, "Weights", binary.LittleEndian.AppendUint16(nil, fixture.cfg.Netuid), binary.LittleEndian.AppendUint16(nil, intent.SelfUID))
		if err != nil {
			t.Fatal(err)
		}
		var row []crv4.WeightPair
		for index, uid := range intent.UIDs {
			row = append(row, crv4.WeightPair{UID: types.U16(uid), Value: types.U16(intent.Prepared.Values[index])})
		}
		encoded, err := codec.EncodeToHex(row)
		if err != nil {
			t.Fatal(err)
		}
		client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
		original := client.callContext
		headers, weightReads := 0, 0
		client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
			if method == "chain_getHeader" {
				headers++
				if headers == 2 && (fault == "number" || fault == "root") {
					header := mainnetRuntimeTestHeader(150)
					if fault == "number" {
						header.Number = 151
					} else {
						header.StateRoot = types.Hash{0xa7}
					}
					return setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(header))
				}
			}
			if method == "state_getStorage" && args[0] == key.Hex() {
				weightReads++
				return setReleaseHistoricalTestResult(result, encoded)
			}
			if method == "chain_getBlockHash" && fault == "closing" && weightReads > 0 && args[0] == uint64(150) {
				return setReleaseHistoricalTestResult(result, types.Hash{0xab}.Hex())
			}
			return original(ctx, result, method, args...)
		}
		steerer := &ReleaseSteerer{cfg: fixture.cfg, native: fixture.rpc.native, intents: store, hotkey: testIntentHotkey(t)}
		err = steerer.checkApplication(t.Context(), nil, map[[32]byte]uint16{steerer.hotkey.PublicKey(): intent.SelfUID})
		after, readErr := os.ReadFile(store.path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		current, readErr := store.Current()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if fault == "none" {
			if err != nil || current.Status != "applied" || current.ApplicationBlock != 150 || headers != 2 || weightReads != 1 {
				t.Fatalf("actual approved row did not apply: %+v %v", current, err)
			}
		} else if err == nil || current.Status != "finalized" || !bytes.Equal(before, after) {
			t.Fatalf("%s application changed original finalized journal: status=%s error=%v", fault, current.Status, err)
		}
	}
}

// Production's retained continuation has its own historical observation path.
// Its closing check must also precede the irreversible local applied marker.
func TestAdjacentHeaderProductionApplicationPreservesJournalOnCanonicalSwitch(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	native.receiptNumber, fixture.production.head = 103, 104
	fixture.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("original receipt did not finalize: %v", err)
	}
	finalized := fixture.restart(t)
	native.applied = true
	fixture.production.head = max(uint64(104), finalized.RevealBlock)
	before, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	metadata := fixture.production.metadata
	key, err := types.CreateStorageKey(metadata, crv4.PalletName, "Weights", binary.LittleEndian.AppendUint16(nil, fixture.steerer.cfg.Netuid), binary.LittleEndian.AppendUint16(nil, finalized.SelfUID))
	if err != nil {
		t.Fatal(err)
	}
	client := native.validatorRuntimeIdentityTestClient
	original := client.callContext
	observedRow := false
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getStorage" && args[0] == key.Hex() {
			observedRow = true
		}
		if method == "chain_getBlockHash" && args[0] == fixture.production.head && observedRow {
			return setReleaseHistoricalTestResult(result, types.Hash{0xea}.Hex())
		}
		return original(ctx, result, method, args...)
	}
	err = fixture.steerer.observeProductionApplicationV2(t.Context(), finalized)
	after, readErr := os.ReadFile(fixture.steerer.intents.path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil || !observedRow || !strings.Contains(err.Error(), "canonical") || !bytes.Equal(before, after) {
		t.Fatalf("production applied changed canonical row: row=%t journal_unchanged=%t error=%v", observedRow, bytes.Equal(before, after), err)
	}
	client.callContext = original
	if fixture.restart(t).Status != "finalized" {
		t.Fatal("failed observation changed retained status")
	}
	var transition *productionSteeringTransition
	if err := fixture.steerer.observeProductionApplicationV2(t.Context(), finalized); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("restored original application could not progress: %v", err)
	}
	if fixture.restart(t).Status != "applied" || len(native.broadcasts) != 1 {
		t.Fatal("recovery replaced original bytes or failed to apply")
	}
}

// The activation header is reread after both real validator observations.
// Its immutable commitment still matters after the earlier runtime admission.
func TestAdjacentHeaderRecycleActivationRejectsLateRootSubstitution(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, _ := fixture.stage(t)
	native := fixture.operator.measurement.admission.chain
	client := native.API.Client.(*recycleAdmissionRouteClient)
	original := client.callContext
	reads := 0
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getHeader" {
			reads++
		}
		return original(ctx, result, method, args...)
	}
	got, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, native, stage.authority)
	if err != nil || got == nil || len(got.Validators) != 2 || reads == 0 {
		t.Fatalf("complete two-validator activation control failed: %+v %v", got, err)
	}
	lastHeader := reads
	reads = 0
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getHeader" {
			reads++
			if reads == lastHeader {
				var raw json.RawMessage
				if err := original(ctx, &raw, method, args...); err != nil {
					return err
				}
				var wire map[string]any
				if err := json.Unmarshal(raw, &wire); err != nil {
					return err
				}
				wire["stateRoot"] = types.Hash{0xe9}.Hex()
				return setReleaseHistoricalTestResult(result, wire)
			}
		}
		return original(ctx, result, method, args...)
	}
	got, err = observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, native, stage.authority)
	if err == nil || got != nil || reads != lastHeader || !strings.Contains(err.Error(), "header SCALE hash") {
		t.Fatalf("activation retained late substituted root after two validators: reads=%d/%d %+v %v", reads, lastHeader, got, err)
	}
}

// The historical target can stay canonical while its separate finality witness
// changes during runtime authentication. Neither cold nor warm state may bind.
func TestAdjacentHeaderProductionHistoricalRejectsClosingFinalitySwitch(t *testing.T) {
	for _, warm := range []bool{false, true} {
		fixture := newProductionRuntimeTestFixture(t, true)
		native := fixture.rpc.native
		if warm {
			if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, fixture.cfg, mainnetRuntimeTestBlock(150)); err != nil {
				t.Fatal(err)
			}
		}
		client := native.API.Client.(*validatorRuntimeIdentityTestClient)
		original := client.callContext
		changed := false
		client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
			if method == "state_getStorageHash" && args[len(args)-1] == mainnetRuntimeTestBlock(100).Hex() {
				fixture.rpc.canonicalHashKVs[150] = types.Hash{0xee}
				changed = true
			}
			return original(ctx, result, method, args...)
		}
		priorMeta, priorRuntime := native.Meta, native.Runtime
		err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), native, fixture.cfg, mainnetRuntimeTestBlock(100))
		if !changed || err == nil || !strings.Contains(err.Error(), "not canonical") {
			t.Fatalf("historical production bound replaced finality witness warm=%t changed=%t: %v", warm, changed, err)
		}
		if native.Meta != priorMeta || native.Runtime != priorRuntime {
			t.Fatal("failed historical finality check replaced prior runtime authority")
		}
	}
}

// Stable finality still permits the original read-only historical view; a
// cancellation at its closing witness leaves the prior view entirely intact.
func TestAdjacentHeaderProductionHistoricalFinalityClosurePreservesPurpose(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		fixture := newProductionRuntimeTestFixture(t, true)
		native := fixture.rpc.native
		client := native.API.Client.(*validatorRuntimeIdentityTestClient)
		original := client.callContext
		ctx, cancel := context.WithCancel(t.Context())
		reads := 0
		client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
			if method == "chain_getBlockHash" && args[0] == uint64(150) {
				reads++
				if canceled && reads == 2 {
					cancel()
				}
			}
			return original(ctx, result, method, args...)
		}
		priorMeta, priorRuntime := native.Meta, native.Runtime
		err := authenticateHistoricalNativeRuntimeAtContext(ctx, native, fixture.cfg, mainnetRuntimeTestBlock(100))
		cancel()
		if reads != 2 {
			t.Fatalf("historical finality witness was not closed: %d %v", reads, err)
		}
		if canceled {
			if !errors.Is(err, context.Canceled) || native.Meta != priorMeta || native.Runtime != priorRuntime {
				t.Fatalf("canceled finality closure changed authority: %v", err)
			}
		} else if err != nil || validateReleaseNativeSigningRuntime(native, fixture.cfg) == nil {
			t.Fatalf("stable historical view failed or gained current signing: %v", err)
		}
	}
}
