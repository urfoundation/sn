// Root service tests use synthetic approvals, native keys and local journals.
// Explicit barriers exercise lifecycle races without node access or sleeps.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// The service serializes this fixture's calls; tests mutate it only after joining.
type rootServiceObserverFixture struct {
	view    rootWeightObservation
	calls   int
	err     error
	entered chan struct{}
	release chan struct{}
	joined  chan struct{}
}

// A cancellation barrier proves that the service joins the active observation.
func (self *rootServiceObserverFixture) observeRootWeights(ctx context.Context, _ rootAction) (rootWeightObservation, error) {
	self.calls++
	if self.entered != nil {
		close(self.entered)
		select {
		case <-ctx.Done():
			close(self.joined)
			return rootWeightObservation{}, ctx.Err()
		case <-self.release:
		}
	}
	return copyRootWeightObservation(self.view), self.err
}

// Captures a separate approved submission capability without any transport.
type rootServiceSubmitterFixture struct {
	intents []rootServiceSubmission
	err     error
}

// The immutable packet and attempt identity survive ambiguous acknowledgements.
func (self *rootServiceSubmitterFixture) submitRoot(_ context.Context, intent rootServiceSubmission) error {
	self.intents = append(self.intents, intent)
	return self.err
}

// One fixture's adapters belong only to its independently approved existing seat.
type rootServiceFixture struct {
	storage   *durablefixture.Fixture
	config    rootServiceConfig
	observer  *rootServiceObserverFixture
	signer    *rootSignerFixture
	authority *rootAuthorityFixture
	chain     *rootChainFixture
	submitter *rootServiceSubmitterFixture
}

// Synthetic approval is distinct from both native signing and current authority.
func newRootServiceFixture(t *testing.T) rootServiceFixture {
	t.Helper()
	offline := newRootOfflineFixture(t)
	action := offline.packet.Action
	name := filepath.Base(action.Scope.StatePath)
	durablefixture.ProvisionSnapshot(t, filepath.Dir(action.Scope.StatePath), "mainnet-root-service", name, rootServiceStoreLimit, name+".lock", map[string][]byte{name + ".lock": nil})
	position := rootActionObservation{NativeChain: action.Scope.NativeChain, GenesisHash: action.Scope.GenesisHash, EvmChainId: action.Scope.EvmChainId, FinalizedNumber: action.BirthBlock, FinalizedHash: action.BirthHash, RuntimeVersion: action.Scope.RuntimeVersion, RuntimeCodeHash: action.Scope.RuntimeCodeHash, RuntimeMetadataHash: action.Scope.RuntimeMetadataHash, Hotkey: action.Scope.Hotkey, Coldkey: action.Scope.Coldkey, Seat: action.Scope.Seat, AccountNonce: action.Nonce}
	view := rootWeightObservation{Schema: rootWeightObservationSchema, Position: position, Enabled: true, ActiveNetworks: []uint16{0, 1, 2, 3, 4, 5, 6, 7}, StoredWeights: []rootStoredWeight{}, RateLimitBlocks: 10, ConcentrationCap: 4096, StorageHash: rootObjectHash("synthetic complete root weight storage")}
	return rootServiceFixture{
		storage:  durablefixture.New(t, t.Context(), filepath.Dir(action.Scope.StatePath)),
		config:   rootServiceConfig{Schema: rootServiceConfigSchema, CustodyTrust: offline.trust, Packet: offline.packet, MaximumObservations: 3},
		observer: &rootServiceObserverFixture{view: view}, signer: &rootSignerFixture{pair: offline.pair}, authority: &rootAuthorityFixture{}, submitter: &rootServiceSubmitterFixture{},
		chain: &rootChainFixture{result: rootActionReconciliation{Observation: position, AnchorHash: action.BirthHash, CheckedFrom: action.BirthBlock + 1, CheckedThrough: action.BirthBlock}},
	}
}

// Read-only, admission, custody and mutation ports remain separate objects.
func (self rootServiceFixture) ports() rootServicePorts {
	return rootServicePorts{Observer: self.observer, Reconciler: self.chain, Authority: self.authority, Signer: self.signer, Submitter: self.submitter}
}

// Opening never performs an observation or contacts a signer.
func (self rootServiceFixture) open(t *testing.T, create bool, ports rootServicePorts) (*rootServiceOwner, *rootServiceStore) {
	t.Helper()
	store, err := openRootServiceStore(self.config, create, self.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	owner, err := newRootServiceOwner(self.config, store, ports)
	if err != nil {
		t.Fatal(err)
	}
	return owner, store
}

// The fixture models an independently verified exact finalized native receipt.
func (self rootServiceFixture) finalize(t *testing.T, store *rootServiceStore, fee uint64) {
	t.Helper()
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	action := record.Action.Action
	self.chain.result.Observation.FinalizedNumber = action.BirthBlock + 2
	self.chain.result.Observation.FinalizedHash = "0x" + strings.Repeat("9a", 32)
	self.chain.result.Observation.AccountNonce = action.Nonce + 1
	self.chain.result.CheckedThrough = action.BirthBlock + 2
	self.chain.result.Receipt = &rootActionReceipt{BlockNumber: action.BirthBlock + 1, BlockHash: "0x" + strings.Repeat("bc", 32), RawExtrinsic: record.Action.RawExtrinsic, EventHash: "0x" + strings.Repeat("de", 32), Success: true, ActualFeeRao: fee, ExecutionRuntimeVersion: action.Scope.RuntimeVersion, ExecutionCodeHash: action.Scope.RuntimeCodeHash, ExecutionMetadataHash: action.Scope.RuntimeMetadataHash}
}

// These vectors cover the pinned fixed-point multiply/divide branch boundary,
// positive half rounding and a full-range target, without floating-point math.
func TestRootServiceWeightNormalization(t *testing.T) {
	for _, item := range []struct {
		input, expected []uint16
	}{
		{input: []uint16{0, 0}, expected: []uint16{0, 0}},
		{input: []uint16{1, 2}, expected: []uint16{32768, 65535}},
		{input: []uint16{1, 6}, expected: []uint16{10923, 65535}},
		{input: []uint16{1, 2, 3}, expected: []uint16{21845, 43690, 65535}},
		{input: []uint16{1, 32767, 32768}, expected: []uint16{2, 65533, 65535}},
		{input: []uint16{1, 32768, 32769}, expected: []uint16{2, 65533, 65535}},
		{input: []uint16{1, 32768, 65535}, expected: []uint16{1, 32768, 65535}},
		{input: []uint16{10, 10, 10}, expected: []uint16{65535, 65535, 65535}},
	} {
		if actual := rootMaxUpscale(item.input); !slices.Equal(actual, item.expected) {
			t.Fatalf("normalization %v: got %v want %v", item.input, actual, item.expected)
		}
	}
}

// Digests were produced by the unmodified pinned Rust max-upscale functions and
// their pinned substrate-fixed dependency. Every value 0..maximum is covered.
func TestRootServiceNormalizationMatchesPinnedRust(t *testing.T) {
	for _, item := range []struct {
		maximum uint16
		digest  string
	}{
		{maximum: 32768, digest: "d93cd9b51eaaae0e8a9296eb11e1cf96381792ade0e02e31d712ecf67af72e96"},
		{maximum: 32769, digest: "bd8438a329f760e8f2f893d1b65ddb4ba0acc3d29b9e33bfa3fed936ae2d120e"},
		{maximum: 65535, digest: "5f1d25a1e0df8a0b7ef756b0648a6c58f1723c22797a2f73705916f240152e0a"},
	} {
		digest := sha256.New()
		for value := uint32(0); value <= uint32(item.maximum); value++ {
			weights := rootMaxUpscale([]uint16{uint16(value), item.maximum})
			encoded := binary.LittleEndian.AppendUint16(nil, weights[0])
			encoded = binary.LittleEndian.AppendUint16(encoded, weights[1])
			digest.Write(encoded)
		}
		if hex.EncodeToString(digest.Sum(nil)) != item.digest {
			t.Fatalf("fixed-point normalization differs from pinned Rust at maximum %d", item.maximum)
		}
	}
}

// Source-specific gates retain explicit hold reasons; raw policy weights are
// compared with their stored max-upscaled vector, independent of entry order.
func TestRootServiceWeightDecisions(t *testing.T) {
	fixture := newRootServiceFixture(t)
	action := fixture.config.Packet.Action
	for _, item := range []struct {
		change func(*rootWeightObservation)
		want   string
	}{
		{change: func(view *rootWeightObservation) {}, want: "intent"},
		{change: func(view *rootWeightObservation) { view.Enabled = false }, want: "setter-disabled"},
		{change: func(view *rootWeightObservation) { view.LastUpdate = action.BirthBlock - 9 }, want: "rate-limited"},
		{change: func(view *rootWeightObservation) { view.LastUpdate = action.BirthBlock - 10 }, want: "intent"},
		{change: func(view *rootWeightObservation) { view.RateLimitBlocks = ^uint64(0) }, want: "intent"},
		{change: func(view *rootWeightObservation) { view.ActiveNetworks = []uint16{0, 1, 2} }, want: "destination-count"},
		{change: func(view *rootWeightObservation) { view.ActiveNetworks[7] = 8 }, want: "destination-unavailable"},
		{change: func(view *rootWeightObservation) {
			view.ActiveNetworks = append(view.ActiveNetworks, 8, 9, 10, 11, 12, 13, 14, 15)
		}, want: "concentration-cap"},
		{change: func(view *rootWeightObservation) {
			view.ActiveNetworks = append(view.ActiveNetworks, 8, 9, 10, 11, 12, 13, 14, 15)
			view.ConcentrationCap = 8192
		}, want: "intent"},
		{change: func(view *rootWeightObservation) {
			view.StoredWeights = []rootStoredWeight{{Netuid: 99, Weight: 0}}
			for index := len(action.Dests) - 1; index >= 0; index-- {
				view.StoredWeights = append(view.StoredWeights, rootStoredWeight{Netuid: action.Dests[index], Weight: 65535})
			}
			view.Enabled = false
		}, want: "target-observed"},
	} {
		view := copyRootWeightObservation(fixture.observer.view)
		item.change(&view)
		decision, err := decideRootWeights(action, view, 1)
		if err != nil || decision.Outcome != item.want || decision.validate(action) != nil {
			t.Fatalf("want %s, got %+v: %v", item.want, decision, err)
		}
	}
}

// Contradictory runtime/seat/nonce/head and incomplete storage never become an
// actionable decision, regardless of any observer's prior successful sample.
func TestRootServiceRejectsStaleDecisionInputs(t *testing.T) {
	fixture := newRootServiceFixture(t)
	for index, change := range []func(*rootWeightObservation){
		func(view *rootWeightObservation) { view.Position.EvmChainId = 945 },
		func(view *rootWeightObservation) { view.Position.GenesisHash = "0x" + strings.Repeat("ab", 32) },
		func(view *rootWeightObservation) { view.Position.FinalizedHash = "0x" + strings.Repeat("cd", 32) },
		func(view *rootWeightObservation) { view.Position.RuntimeCodeHash = "0x" + strings.Repeat("ef", 32) },
		func(view *rootWeightObservation) { view.Position.RuntimeMetadataHash = "0x" + strings.Repeat("ab", 32) },
		func(view *rootWeightObservation) { view.Position.Seat.RegistrationBlock++ },
		func(view *rootWeightObservation) { view.Position.AccountNonce++ },
		func(view *rootWeightObservation) { view.Position.StateUnavailable = true },
		func(view *rootWeightObservation) {
			view.Position.FinalizedNumber += fixture.config.Packet.Action.Period
		},
		func(view *rootWeightObservation) { view.LastUpdate = view.Position.FinalizedNumber + 1 },
		func(view *rootWeightObservation) { view.StoredWeights = nil },
		func(view *rootWeightObservation) {
			view.StoredWeights = []rootStoredWeight{{Netuid: 1, Weight: 0}, {Netuid: 1, Weight: 1}}
		},
		func(view *rootWeightObservation) { view.ActiveNetworks[0] = 1 },
		func(view *rootWeightObservation) { view.StorageHash = "" },
	} {
		view := copyRootWeightObservation(fixture.observer.view)
		change(&view)
		if _, err := decideRootWeights(fixture.config.Packet.Action, view, 1); err == nil {
			t.Fatalf("case %d admitted contradictory root view", index)
		}
	}
}

// A complete decision and reserved action are synced before any capability is
// invoked. A caller cannot mutate the journal through the returned decision.
func TestRootServiceDecisionPrecedesEffects(t *testing.T) {
	fixture := newRootServiceFixture(t)
	owner, store := fixture.open(t, true, fixture.ports())
	event, err := owner.step(context.Background())
	if err != nil || event.Phase != "active" || event.Status != "intent" || event.ActivationReady || event.Decision == nil {
		t.Fatalf("missing durable decision: %+v %v", event, err)
	}
	if fixture.signer.signs != 0 || fixture.authority.calls != 0 || len(fixture.submitter.intents) != 0 {
		t.Fatal("decision construction invoked a mutation capability")
	}
	event.Decision.Observation.ActiveNetworks[0] = 99
	record, err := store.load()
	if err != nil || record.Phase != "active" || record.Action.Phase != "reserved" || record.Observations != 1 || record.Decision.Observation.ActiveNetworks[0] != 0 {
		t.Fatalf("decision journal differs: %+v %v", record, err)
	}
}

// A later candidate cannot replace the last complete hold on a finalized
// rollback, same-height fork or reused hash. Its consumed attempt stays durable.
func TestRootServiceDecisionContinuity(t *testing.T) {
	for _, failure := range []string{"rollback", "fork", "reused-hash"} {
		fixture := newRootServiceFixture(t)
		fixture.observer.view.Position.FinalizedNumber += 10
		fixture.observer.view.Position.FinalizedHash = "0x" + strings.Repeat("ad", 32)
		fixture.observer.view.Enabled = false
		owner, store := fixture.open(t, true, fixture.ports())
		if result, err := owner.step(context.Background()); err != nil || result.Status != "setter-disabled" {
			t.Fatal("initial hold failed", result, err)
		}
		fixture.observer.view.Enabled = true
		switch failure {
		case "rollback":
			fixture.observer.view.Position.FinalizedNumber--
		case "fork":
			fixture.observer.view.Position.FinalizedHash = "0x" + strings.Repeat("bc", 32)
		case "reused-hash":
			fixture.observer.view.Position.FinalizedNumber++
		}
		if result, err := owner.step(context.Background()); err == nil || result.Decision != nil || result.ActivationReady {
			t.Fatalf("%s admitted new intent: %+v %v", failure, result, err)
		}
		record, err := store.load()
		if err != nil || record.Phase != "observing" || !record.ObservationPending || record.Observations != 2 || record.Decision.Attempt != 1 || record.Decision.Outcome != "setter-disabled" || fixture.signer.signs != 0 {
			t.Fatalf("%s lost original hold: %+v %v", failure, record, err)
		}
	}
}

// Independent ports cannot be invoked by forged requests, signed bytes without
// durable broadcast intent, or a view whose current authority has been denied.
func TestRootServiceAdapterAdmission(t *testing.T) {
	fixture := newRootServiceFixture(t)
	owner, store := fixture.open(t, true, fixture.ports())
	action := copyRootAction(fixture.config.Packet.Action)
	signer := &rootServiceSigner{owner: owner}
	chain := &rootServiceChain{owner: owner}
	admission := &rootServiceAdmission{owner: owner}
	if _, err := signer.signOnce(context.Background(), action); err == nil {
		t.Fatal("signing before durable decision succeeded")
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	signature, err := fixture.signer.pair.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := action.signed(signature)
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.submit(context.Background(), raw); err == nil {
		t.Fatal("submission without durable attempt succeeded")
	}
	action.Weights[0]++
	if _, err := signer.signOnce(context.Background(), action); err == nil {
		t.Fatal("altered request reached signer")
	}
	if _, err := chain.reconcile(context.Background(), action, nil); err == nil {
		t.Fatal("altered request reached reconciliation")
	}
	if err := admission.authorize(context.Background(), action, fixture.observer.view.Position); err == nil {
		t.Fatal("altered request reached authority")
	}
	if _, err := signer.recoverSignature(context.Background(), rootObjectHash("foreign request")); err == nil {
		t.Fatal("foreign request reached custody lookup")
	}
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.authority.err = errors.New("synthetic current authority revoked")
	if _, err := owner.step(context.Background()); err == nil {
		t.Fatal("denied current authority reached signing")
	}
	record, err := store.load()
	if err != nil || record.Action.Phase != "reserved" || record.Action.Broadcasts != 0 || fixture.signer.signs != 0 || len(fixture.submitter.intents) != 0 {
		t.Fatal("independent authority failure consumed mutation allowance", err)
	}
	forged := *record.Decision
	forged.Observation.Enabled = false
	forged.ContentHash = ""
	forged.ContentHash = rootObjectHash(forged)
	record.Decision = &forged
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(fixture.config); err == nil {
		t.Fatal("rehashed journal substituted an unsupported intent decision")
	}
}

// No missing production capability can accidentally consume signing or send
// allowance through the otherwise complete read-only observation/receipt ports.
func TestRootServiceMissingPortsPreserveAllowance(t *testing.T) {
	for _, missing := range []string{"authority", "signer", "submitter"} {
		fixture := newRootServiceFixture(t)
		ports := fixture.ports()
		switch missing {
		case "authority":
			ports.Authority = nil
		case "signer":
			ports.Signer = nil
		case "submitter":
			ports.Submitter = nil
		}
		owner, store := fixture.open(t, true, ports)
		if _, err := owner.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if event, err := owner.step(context.Background()); err == nil || event.Status != "blocked" {
			t.Fatalf("missing %s admitted an effect: %+v %v", missing, event, err)
		}
		record, err := store.load()
		if err != nil || record.Action.Phase != "reserved" || record.Action.Broadcasts != 0 || fixture.signer.signs != 0 || len(fixture.submitter.intents) != 0 || fixture.observer.calls != 1 {
			t.Fatalf("missing %s consumed or replaced intent: %+v %v", missing, record, err)
		}
	}
}

// A lost signer response is recovered after restart without observing a new
// basket. Old receipt reconciliation still works with every mutation port absent.
func TestRootServiceRestartReconcilesOriginalIntent(t *testing.T) {
	fixture := newRootServiceFixture(t)
	fixture.signer.returnErr = errors.New("synthetic signer acknowledgement lost")
	owner, store := fixture.open(t, true, fixture.ports())
	if _, err := owner.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if event, err := owner.step(context.Background()); err == nil || event.Action.Phase != "signing" || fixture.signer.signs != 1 {
		t.Fatalf("lost signer response did not retain intent: %+v %v", event, err)
	}
	store.close()
	ports := fixture.ports()
	ports.Observer, ports.Authority, ports.Submitter = nil, nil, nil
	owner, store = fixture.open(t, false, ports)
	if event, err := owner.step(context.Background()); err != nil || event.Action.Phase != "signed" || fixture.signer.signs != 1 {
		t.Fatalf("restart did not recover original signature: %+v %v", event, err)
	}
	record, err := store.load()
	if err != nil || record.Action.Signature != hex.EncodeToString(fixture.signer.signature) || record.Observations != 1 {
		t.Fatal("original signature or observation count changed", err)
	}
	fixture.finalize(t, store, 12)
	owner.ports.Signer = nil
	if event, err := owner.step(context.Background()); err != nil || event.Phase != "complete" || event.Action.Phase != "finalized" {
		t.Fatalf("mutation revocation erased original receipt: %+v %v", event, err)
	}
	if fixture.observer.calls != 1 || len(fixture.submitter.intents) != 0 {
		t.Fatal("recovery re-observed or submitted")
	}
}

// The separate submission port receives original approval and monotonically
// reserved attempt numbers. Lost replies do not mint a new action or budget.
func TestRootServiceSubmissionAttemptsAndFeeReconciliation(t *testing.T) {
	fixture := newRootServiceFixture(t)
	fixture.submitter.err = errors.New("synthetic submission acknowledgement lost")
	owner, store := fixture.open(t, true, fixture.ports())
	for step := 0; step < 2; step++ {
		if _, err := owner.step(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if event, err := owner.step(context.Background()); err == nil || event.Action.Phase != "pending" {
			t.Fatalf("attempt %d lost pending state: %+v %v", attempt, event, err)
		}
		intent := fixture.submitter.intents[attempt-1]
		if intent.Attempt != uint8(attempt) || intent.Packet.ContentHash != fixture.config.Packet.ContentHash || intent.ConfigHash != rootObjectHash(fixture.config) || attempt > 1 && intent.RawExtrinsic != fixture.submitter.intents[0].RawExtrinsic {
			t.Fatalf("attempt %d changed signed identity: %+v", attempt, intent)
		}
	}
	if event, err := owner.step(context.Background()); err == nil || event.Status != "blocked" || len(fixture.submitter.intents) != 2 || fixture.signer.signs != 1 {
		t.Fatalf("exhausted service renewed send allowance: %+v %v", event, err)
	}
	fixture.finalize(t, store, fixture.config.Packet.Action.Scope.FeeReserveRao+1)
	if event, err := owner.step(context.Background()); err != nil || event.Phase != "complete" || event.Action.Phase != "fee-overrun" {
		t.Fatalf("fee overrun disappeared after send exhaustion: %+v %v", event, err)
	}
	if event, err := owner.step(context.Background()); err != nil || event.Status != "complete" || fixture.observer.calls != 1 || fixture.signer.signs != 1 || len(fixture.submitter.intents) != 2 {
		t.Fatalf("terminal service restarted policy work: %+v %v", event, err)
	}
}

// Missing observations consume their durable read budget across restarts while
// preserving the original native action. They cannot reset a signing allowance.
func TestRootServiceObservationBudgetSurvivesRestart(t *testing.T) {
	fixture := newRootServiceFixture(t)
	fixture.config.MaximumObservations = 1
	fixture.observer.err = errors.New("synthetic unavailable finalized view")
	owner, store := fixture.open(t, true, fixture.ports())
	if event, err := owner.step(context.Background()); err == nil || event.Status != "observation-unavailable" || event.Decision != nil {
		t.Fatalf("failed observation published a decision: %+v %v", event, err)
	}
	store.close()
	fixture.observer.err = nil
	owner, store = fixture.open(t, false, fixture.ports())
	if event, err := owner.step(context.Background()); err == nil || event.Status != "blocked" || fixture.observer.calls != 1 {
		t.Fatalf("restart renewed read budget: %+v %v", event, err)
	}
	record, err := store.load()
	if err != nil || !record.ObservationPending || record.Observations != 1 || record.Action.Phase != "reserved" || fixture.signer.signs != 0 {
		t.Fatal("interrupted observation lost ownership", err)
	}
}

// A save barrier distinguishes both sides of ambiguous local persistence.
type rootServiceStoreFailure struct {
	store  rootServiceStorage
	failAt int
	saves  int
	commit bool
}

// Loads use the real journal's complete private-file checks.
func (self *rootServiceStoreFailure) load() (rootServiceRecord, error) { return self.store.load() }

// A post-commit error deliberately loses the acknowledgement of a complete write.
func (self *rootServiceStoreFailure) save(record rootServiceRecord) error {
	self.saves++
	if self.saves == self.failAt {
		if self.commit {
			if err := self.store.save(record); err != nil {
				return err
			}
		}
		return errors.New("synthetic root service durability failure")
	}
	return self.store.save(record)
}

// Failures around the atomic decision write and signing-intent write cannot
// publish partial decisions, sign before durability or lose the original request.
func TestRootServiceAmbiguousIntentWrites(t *testing.T) {
	for _, failAt := range []int{2, 3} {
		for _, commit := range []bool{false, true} {
			fixture := newRootServiceFixture(t)
			_, store := fixture.open(t, true, fixture.ports())
			failure := &rootServiceStoreFailure{store: store, failAt: failAt, commit: commit}
			owner, err := newRootServiceOwner(fixture.config, failure, fixture.ports())
			if err != nil {
				t.Fatal(err)
			}
			event, err := owner.step(context.Background())
			if failAt == 3 {
				if err != nil {
					t.Fatal(err)
				}
				event, err = owner.step(context.Background())
			}
			if err == nil || !owner.poisoned || fixture.signer.signs != 0 || failAt == 2 && event.Decision != nil {
				t.Fatalf("write %d committed=%v leaked an effect: %+v %v", failAt, commit, event, err)
			}
			if _, err := owner.step(context.Background()); err == nil || failure.saves != failAt {
				t.Fatal("poisoned service retried its write", err)
			}
			store.close()
			owner, store = fixture.open(t, false, fixture.ports())
			for step := 0; step < 3 && fixture.signer.signs == 0; step++ {
				if _, err := owner.step(context.Background()); err != nil {
					t.Fatal("restart could not recover surviving intent", err)
				}
			}
			record, err := store.load()
			if err != nil || fixture.signer.signs != 1 || record.Action.Action.RequestHash != fixture.config.Packet.Action.RequestHash || record.Action.Phase != "signed" {
				t.Fatalf("write %d committed=%v recovery differs: %+v %v", failAt, commit, record, err)
			}
		}
	}
}

// Cancellation joins the active observer, keeps its attempt reserved and lets
// later concurrent callers advance only one immutable signing/send lifecycle.
func TestRootServiceCancellationAndConcurrentSteps(t *testing.T) {
	fixture := newRootServiceFixture(t)
	fixture.observer.entered, fixture.observer.joined = make(chan struct{}), make(chan struct{})
	owner, store := fixture.open(t, true, fixture.ports())
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := owner.step(ctx); finished <- err }()
	<-fixture.observer.entered
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if _, err := owner.step(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled waiter entered root service", err)
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal("active observation did not stop", err)
	}
	<-fixture.observer.joined
	record, err := store.load()
	if err != nil || record.Observations != 1 || !record.ObservationPending || record.Decision != nil || fixture.signer.signs != 0 {
		t.Fatal("cancellation lost pending observation ownership", err)
	}
	fixture.observer.entered = nil
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() { owner.step(context.Background()) })
	}
	workers.Wait()
	record, err = store.load()
	if err != nil || record.Observations != 2 || fixture.observer.calls != 2 || fixture.signer.signs != 1 || len(fixture.submitter.intents) != 2 || record.Action.Broadcasts != 2 {
		t.Fatalf("concurrent service calls duplicated ownership: %+v %v", record, err)
	}
}

// An arbitrary synchronous writer has no admitted cancellation contract.
type rootServiceRefusedOutput struct{ calls int }

// Recording a call distinguishes refusal from an attempted failed write.
func (self *rootServiceRefusedOutput) Write([]byte) (int, error) {
	self.calls++
	return 0, errors.New("synthetic unsupported writer called")
}

// Optional publication cannot invalidate durable intent. A canceled bounded
// run joins its reader and cannot leave a detached output or signing worker.
func TestRootServiceBoundedRunPreservesIntentAndJoins(t *testing.T) {
	fixture := newRootServiceFixture(t)
	owner, store := fixture.open(t, true, fixture.ports())
	refused := &rootServiceRefusedOutput{}
	output, err := newRootServiceOutput(t.Context(), refused)
	if err != nil {
		t.Fatal(err)
	}
	runErr := owner.Run(context.Background(), 1, time.Hour, output)
	publication := output.snapshot()
	if runErr != nil || publication.Outcome != "unavailable" || publication.Dropped != 1 || publication.Delivered != 0 || publication.Unavailable != 0 || refused.calls != 0 {
		t.Fatal("optional publisher changed durable intent result", runErr)
	}
	record, err := store.load()
	if err != nil || record.Phase != "active" || fixture.signer.signs != 0 {
		t.Fatal("publisher failure lost durable intent or signed", err)
	}
	other := newRootServiceFixture(t)
	other.observer.entered, other.observer.joined = make(chan struct{}), make(chan struct{})
	otherOwner, _ := other.open(t, true, other.ports())
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	otherOutput, err := newRootServiceOutput(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		finished <- otherOwner.Run(ctx, 2, time.Hour, otherOutput)
	}()
	<-other.observer.entered
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) || otherOutput.snapshot().Dropped != 0 {
		t.Fatal("canceled run published partial work", err)
	}
	<-other.observer.joined
}

// Local service ownership must not silently convert or overwrite an existing
// standalone action journal. Original receipt recovery remains with that owner.
func TestRootServiceStoreRejectsReinitializationAndForeignState(t *testing.T) {
	fixture := newRootServiceFixture(t)
	_, store := fixture.open(t, true, fixture.ports())
	if _, err := openRootServiceStore(fixture.config, false, fixture.storage.Context); err == nil {
		t.Fatal("two service owners admitted")
	}
	store.close()
	path := fixture.config.Packet.Action.Scope.StatePath
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record rootServiceRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	record.Config.MaximumObservations++
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	replaced, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for index, invalid := range [][]byte{nil, replaced, append([]byte(`{"SCHEMA":"duplicate",`), raw[1:]...), append(raw, []byte(` {}`)...)} {
		if err := os.WriteFile(path, invalid, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootServiceStore(fixture.config, false, fixture.storage.Context); err == nil {
			t.Fatalf("case %d reused altered service state", index)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := openRootServiceStore(fixture.config, true, fixture.storage.Context); err == nil {
		t.Fatal("lost journal reset its durable marker")
	}
	other := newRootOfflineFixture(t)
	action := other.packet.Action
	actionStorage := durablefixture.New(t, t.Context(), filepath.Dir(action.Scope.StatePath))
	prepareMainnetSnapshotTest(t, action.Scope.StatePath, "mainnet-root-action", rootActionStoreLimit)
	actionStore, err := openRootActionStore(action.Scope.StatePath, &action, actionStorage.Context)
	if err != nil {
		t.Fatal(err)
	}
	actionStore.close()
	foreign := rootServiceConfig{Schema: rootServiceConfigSchema, CustodyTrust: other.trust, Packet: other.packet, MaximumObservations: 3}
	if _, err := openRootServiceStore(foreign, false, actionStorage.Context); err == nil {
		t.Fatal("standalone action implicitly migrated to service")
	}
	if _, err := openRootServiceStore(foreign, true, actionStorage.Context); err == nil {
		t.Fatal("standalone action was overwritten")
	}
}

// Private regular files are mandatory; a special ownership marker or state file
// cannot block a reader forever or be accepted as an unused service journal.
func TestRootServiceStoreRejectsSpecialAndPublicFiles(t *testing.T) {
	fixture := newRootServiceFixture(t)
	_, store := fixture.open(t, true, fixture.ports())
	store.close()
	for _, suffix := range []string{"", ".lock"} {
		path := fixture.config.Packet.Action.Scope.StatePath + suffix
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootServiceStore(fixture.config, false, fixture.storage.Context); err == nil {
			t.Fatal("public service file admitted", suffix)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootServiceStore(fixture.config, false, fixture.storage.Context); err == nil {
			t.Fatal("FIFO service file admitted", suffix)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("synthetic-missing-target", path); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootServiceStore(fixture.config, false, fixture.storage.Context); err == nil {
			t.Fatal("symlink service file admitted", suffix)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
