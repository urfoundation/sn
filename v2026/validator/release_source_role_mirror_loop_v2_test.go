//go:build linux || darwin

// The source-role incident at the production steering loop. The commitment
// oracle mirrored the validator hotkey's own native source write, the
// hotkey-signed Utility.batch_all of set_commitment and the CRv4 weight
// commit, while the native slot still held that write. The retired rule
// refused every nonzero mirror, so each attempt was a hard error, the loop
// stopped at its failure limit, the release entry point panicked on that
// error and the service manager restarted it into the same refusal.
package validator

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The validator's earlier source write lands at this block, the decision's
// own write at the next one.
const (
	sourceRoleMirrorLoopTestEarlierBlock = uint64(101)
	sourceRoleMirrorLoopTestReceiptBlock = uint64(102)
)

// The production continuation fixture: the mainnet production config, the
// durable V2 intent store, a sealed first decision signed through
// PrepareWeightsCRv4ExactAtContext, and a native chain that loses the submit
// acknowledgement and later shows the receipt, slot and applied row.
type sourceRoleMirrorLoopTestV2 struct {
	fixture     *productionContinuationTestFixture
	native      *productionContinuationNativeTestClient
	coordinator *ChainClient
	snapshot    *ReleaseSnapshot
	mirror      atomic.Pointer[[]byte]
	// The earlier source hash the oracle mirrored, and the decision's own.
	earlier [32]byte
	source  [32]byte
}

// The earlier write is a second real preparation of the decision's row from
// the same hotkey, so its bytes have the exact source shape. The retained
// predecessor proof owns it, as startup authenticates one for the first
// intent of a generation (release_source_role_predecessor_v2_test.go).
func newSourceRoleMirrorLoopTestV2(t *testing.T) *sourceRoleMirrorLoopTestV2 {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	production := fixture.production
	self := &sourceRoleMirrorLoopTestV2{fixture: fixture, earlier: [32]byte{0x5e, 0xa1}}
	self.source = [32]byte(common.HexToHash(fixture.intent.Prepared.SourceCommitment.Hash))
	stage, provider := production.stage(t)
	row, err := ownerRecycleProductionRowDecision(provider, stage.proof.Row)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := types.NewHashFromHexString(stage.proof.Decision.NativeSnapshotHash)
	if err != nil {
		t.Fatal(err)
	}
	signer := production.operator.measurement.admission.chain
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), signer, production.cfg, selected); err != nil {
		t.Fatal(err)
	}
	options := releaseSubmitOptions(production.cfg)
	options.SourceHash = self.earlier
	options.Now = func() time.Time { return time.Unix(2_000_000_000, 0) }
	earlierWrite, err := crv4.PrepareWeightsCRv4ExactAtContext(t.Context(), signer, production.hotkey, production.cfg.Netuid, row.UIDs, row.Scores, options, selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.ValidatePreparedSource(earlierWrite); err != nil || earlierWrite.SourceCommitment.Hash != releaseHex32(self.earlier) {
		t.Fatalf("earlier write is not the hotkey's batch_all source shape: %v", err)
	}
	self.native = installProductionContinuationNative(t, fixture)
	self.native.receiptNumber = sourceRoleMirrorLoopTestReceiptBlock
	production.extrinsicsKVs = map[uint64][]string{sourceRoleMirrorLoopTestEarlierBlock: {earlierWrite.ExtrinsicHex}}
	production.head = sourceRoleMirrorLoopTestEarlierBlock
	hotkey := production.hotkey.PublicKey()
	netuid := binary.LittleEndian.AppendUint16(nil, production.cfg.Netuid)
	commitmentKey, err := types.CreateStorageKey(production.metadata, "Commitments", "CommitmentOf", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(production.metadata, "Commitments", "LastCommitment", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	// The slot holds the earlier write from its block on, and the decision's
	// write once the canonical chain includes it. The continuation client
	// answers the receipt block itself.
	prior := self.native.call
	self.native.call = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "state_getStorage" && len(args) == 2 && (args[0] == commitmentKey.Hex() || args[0] == lastKey.Hex()) {
			for number := sourceRoleMirrorLoopTestEarlierBlock; number <= production.head; number++ {
				if args[1] != production.block(number).Hex() {
					continue
				}
				hash, block := self.earlier, sourceRoleMirrorLoopTestEarlierBlock
				if number >= sourceRoleMirrorLoopTestReceiptBlock && slices.Contains(production.extrinsicsKVs[sourceRoleMirrorLoopTestReceiptBlock], fixture.intent.Prepared.ExtrinsicHex) {
					hash, block = self.source, sourceRoleMirrorLoopTestReceiptBlock
				}
				value := binary.LittleEndian.AppendUint32(nil, uint32(block))
				if args[0] == commitmentKey.Hex() {
					info, err := crv4.EncodeFleetCommitmentInfo(hash)
					if err != nil {
						return err
					}
					value = append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(nil, 1), uint32(block)), info...)
				}
				encoded, err := json.Marshal(codec.HexEncodeToString(value))
				if err != nil {
					return err
				}
				return json.Unmarshal(encoded, target)
			}
		}
		return prior(ctx, target, method, args...)
	}
	self.snapshot = &ReleaseSnapshot{BlockNumber: fixture.intent.EVMSnapshotBlock, BlockHash: [32]byte(common.HexToHash(fixture.intent.EVMSnapshotHash))}
	self.coordinator = sourceRoleMirrorChainV2(t, hotkey, self.snapshot, &self.mirror)
	self.setMirror(self.earlier, production.block(sourceRoleMirrorLoopTestEarlierBlock), sourceRoleMirrorLoopTestEarlierBlock)
	fixture.steerer.sourceRolePredecessorV2 = &releaseSourceRoleWitnessV2{hotkey: hotkey, netuid: production.cfg.Netuid, sourceHash: releaseHex32(self.earlier), commitmentBlock: sourceRoleMirrorLoopTestEarlierBlock}
	fixture.runtime.progress = &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
	return self
}

// The coordinator's mirroredCommitments record for the validator hotkey.
func (self *sourceRoleMirrorLoopTestV2) setMirror(hash [32]byte, blockHash types.Hash, block uint64) {
	raw := sourceRoleMirrorV2Raw(hash, blockHash, block)
	self.mirror.Store(&raw)
}

// submitOnceV2's role gate, on a private signing view as submitOnceV2 makes
// one, at the finalized native head and the decision's EVM snapshot.
func (self *sourceRoleMirrorLoopTestV2) gate(ctx context.Context) (*ReleaseSteerer, error) {
	owned := *self.fixture.steerer
	native := *owned.native
	owned.native, owned.chain = &native, self.coordinator
	current, err := owned.intents.currentV2(ctx)
	if err != nil {
		return nil, err
	}
	nativeHash, err := authenticatePinnedNativeRuntimeContext(ctx, owned.native, owned.cfg)
	if err != nil {
		return nil, err
	}
	return &owned, owned.checkSourceRoleV2(ctx, self.snapshot, nativeHash, current)
}

// One poll of the production steering operation. A retained intent goes to
// submitOnceV2 itself. Without one, the fixture's sealed decision stands in
// for collection, measurement and signing, and the steps of submitOnceV2
// from its role gate to the first broadcast run unchanged.
func (self *sourceRoleMirrorLoopTestV2) attempt(ctx context.Context) error {
	return runReleaseSteeringOperation(ctx, productionSteeringReadTimeout, func(ctx context.Context) error {
		current, err := self.fixture.steerer.intents.currentV2(ctx)
		if err != nil {
			return err
		}
		if current != nil {
			return self.fixture.steerer.submitOnceV2(ctx)
		}
		owned, err := self.gate(ctx)
		if err != nil {
			return err
		}
		intent, err := owned.intents.beginV2(ctx, *self.fixture.intent)
		if err != nil {
			return err
		}
		authorized, err := owned.intents.ownerRecyclePreparedConfig(ctx, owned.cfg, intent.Prepared)
		if err != nil {
			return err
		}
		if err := owned.runtimeV2.authenticationPending(intent); err != nil {
			return err
		}
		_, attempted, err := submitPreparedNativeRuntimeContext(ctx, owned.native, authorized, intent.Prepared)
		if err != nil {
			if !attempted {
				return err
			}
			return owned.recordReleasePendingError(intent.VectorHash, err)
		}
		return errors.New("the fixture acknowledged a broadcast it loses")
	})
}

// The outcome the loop last reported.
func (self *sourceRoleMirrorLoopTestV2) outcome() string {
	progress := self.fixture.runtime.progress
	progress.stateLock.Lock()
	defer progress.stateLock.Unlock()
	if progress.value.Steering == nil {
		return ""
	}
	return progress.value.Steering.Outcome
}

// Under the retired rule every poll is the same hard error, nothing is signed
// into an intent or broadcast, and the tenth poll ends the loop with the
// failure limit, which the release worker owner returns and runReleaseConfig
// panics on. Under the current rule the same fixture steers: the decision is
// begun and broadcast, its receipt finalizes it and its row applies it.
// Afterwards the old mirror still passes against the slot the new write
// moved, and a foreign record at the mirrored block still fails.
func TestReleaseSourceRoleMirrorV2IncidentLoopSteersAndFinalizes(t *testing.T) {
	m := newSourceRoleMirrorLoopTestV2(t)
	steerer, native := m.fixture.steerer, m.native
	steerer.sourceRoleMirrorRuleV2 = func(context.Context, []byte) error {
		return errors.New(releaseSourceRoleMirrorV2Rejection)
	}
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	operations := releaseShutdownTestOperations(func() {})
	var attemptErrs []error
	operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
		return releaseShutdownSteererFunc(func(ctx context.Context) error {
			return runReleaseProductionSteeringLoopWithWait(ctx, func() error {
				err := m.attempt(ctx)
				attemptErrs = append(attemptErrs, err)
				return err
			}, func() bool { return true }, m.fixture.runtime.progress)
		}), nil
	}
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || limit.failures != releaseSteeringFailureLimit || len(attemptErrs) != releaseSteeringFailureLimit || m.outcome() != "hard_error" {
		t.Fatalf("retired rule did not stop at the failure limit: %d attempts, outcome %q, %v", len(attemptErrs), m.outcome(), err)
	}
	for index, attemptErr := range attemptErrs {
		if attemptErr == nil || !strings.Contains(attemptErr.Error(), releaseSourceRoleMirrorV2Rejection) {
			t.Fatalf("retired rule attempt %d: %v", index+1, attemptErr)
		}
	}
	if current, err := steerer.intents.currentV2(t.Context()); err != nil || current != nil || len(native.broadcasts) != 0 {
		t.Fatalf("retired rule signed into an intent or broadcast: %v %v", current, err)
	}

	// The restarted, fixed validator on the same store and chain state.
	steerer.sourceRoleMirrorRuleV2 = nil
	production := m.fixture.production
	var outcomes []string
	var statuses []string
	attempts := 0
	err = runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		attempts++
		return m.attempt(t.Context())
	}, func() bool {
		outcomes = append(outcomes, m.outcome())
		current, err := steerer.intents.currentV2(t.Context())
		if err != nil || current == nil {
			t.Fatalf("steering left no current intent after poll %d: %v", attempts, err)
		}
		statuses = append(statuses, current.Status)
		switch attempts {
		case 1:
			production.extrinsicsKVs[sourceRoleMirrorLoopTestReceiptBlock] = []string{current.Prepared.ExtrinsicHex}
			production.head = sourceRoleMirrorLoopTestReceiptBlock + 1
		case 2:
			native.applied = true
			production.head = max(sourceRoleMirrorLoopTestReceiptBlock+2, current.RevealBlock)
		}
		return attempts < 3
	}, m.fixture.runtime.progress)
	if err != nil || !slices.Equal(outcomes, []string{"receipt_transport_wait", "complete", "working"}) || !slices.Equal(statuses, []string{"pending", "finalized", "applied"}) {
		t.Fatalf("fixed rule did not steer to an applied intent: outcomes %v statuses %v: %v", outcomes, statuses, err)
	}
	current := m.fixture.restart(t)
	if current.Status != "applied" || current.FinalizedBlock != sourceRoleMirrorLoopTestReceiptBlock || current.Prepared.SourceCommitment.Hash != releaseHex32(m.source) || !slices.Equal(native.broadcasts, []string{m.fixture.intent.Prepared.ExtrinsicHex}) {
		t.Fatalf("steering finalized another write or broadcast twice: %+v", current)
	}

	// The slot now holds the decision's write; the mirror still names the
	// earlier one, which the archive still proves.
	if _, err := m.gate(t.Context()); err != nil {
		t.Fatalf("old mirror failed once the validator wrote again: %v", err)
	}
	foreign := m.earlier
	foreign[31] ^= 1
	m.setMirror(foreign, production.block(sourceRoleMirrorLoopTestEarlierBlock), sourceRoleMirrorLoopTestEarlierBlock)
	if _, err := m.gate(t.Context()); err == nil || !strings.Contains(err.Error(), releaseSourceRoleMirrorV2Rejection) {
		t.Fatalf("foreign record at the mirrored block passed: %v", err)
	}
	m.setMirror(m.source, production.block(sourceRoleMirrorLoopTestReceiptBlock), sourceRoleMirrorLoopTestReceiptBlock)
	if _, err := m.gate(t.Context()); err != nil {
		t.Fatalf("mirror of the new write failed: %v", err)
	}
}
