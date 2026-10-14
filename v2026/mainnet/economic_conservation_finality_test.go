// Original SCALE headers and Ed25519 GRANDPA votes exercise the real verifier.
// These synthetic signatures authorize only this fixture's original checkpoint;
// they are not a claim of mainnet authority or runtime economics approval.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urnetwork/server/v2026/strecovery"
	"golang.org/x/crypto/blake2b"
)

// A selected child101 is certified by descendant102. Native100 maps to EVM10;
// native and EVM heights deliberately cannot be joined numerically.
type economicFinalityFixture struct {
	ctx    context.Context
	policy economicConservationPolicy
	state  *economicConservationState
	view   *economicConservationArchiveView
}

// Start with the same independently signed reusable approval grammar as the
// public producer, then construct every header and vote independently.
func newEconomicFinalityFixture(t *testing.T, repeatEvm ...bool) *economicFinalityFixture {
	t.Helper()
	ctx, policy, authorities, _, _ := nativeRenewalTestInputs(t)
	policy.Execution.Producer.Renewals = nil
	authority := authorities[0].value
	consensus := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize))
	var boundaries []economicEmissionBoundary
	var headers []string
	var evm []economicEmissionBoundary
	parent := types.Hash{1}
	for offset := uint64(0); offset < 3; offset++ {
		evm = append(evm, economicEmissionBoundary{Number: 10 + offset, Hash: "0x" + strings.Repeat(string("abc"[offset]), 64)})
		mapped := evm[offset].Hash
		if len(repeatEvm) == 1 && repeatEvm[0] && offset == 2 {
			mapped = evm[1].Hash
		}
		raw, err := hex.DecodeString(strings.TrimPrefix(mappingTestDigest(t, 3, mapped, nil), "0x"))
		if err != nil {
			t.Fatal(err)
		}
		var item types.DigestItem
		if err := codec.Decode(raw, &item); err != nil {
			t.Fatal(err)
		}
		header := types.Header{ParentHash: parent, Number: types.BlockNumber(100 + offset), StateRoot: types.Hash{2}, ExtrinsicsRoot: types.Hash{3}, Digest: types.Digest{item}}
		raw, err = codec.Encode(header)
		if err != nil {
			t.Fatal(err)
		}
		hash := blake2b.Sum256(raw)
		parent = types.Hash(hash)
		headers = append(headers, nativeExecutionTestHex(raw))
		boundaries = append(boundaries, economicEmissionBoundary{Number: 100 + offset, Hash: nativeExecutionTestHex(hash[:])})
	}
	policy.From, policy.Through = boundaries[0], boundaries[1]
	authority.From = policy.From
	authority.Checkpoint.HeaderScale = headers[0]
	authority.Checkpoint.Authorities = []strecovery.GrandpaAuthority{{PublicKey: nativeExecutionTestHex(consensus.Public().(ed25519.PublicKey)), Weight: 1}}
	message, err := authority.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	authority.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	policy.Execution.Producer.Authority = nativeRenewalTestWrite(t, policy.Execution.Producer.Authority.Path, authority)
	raw, err := os.ReadFile(policy.Execution.Producer.Authority.Path)
	if err != nil {
		t.Fatal(err)
	}
	proof := strecovery.NativeExecutionFinalityProof{Schema: strecovery.NativeExecutionFinalitySchema, CheckpointHash: authority.Checkpoint.Hash(), Parent: strecovery.ObservedBlockIdentity{Number: boundaries[0].Number, Hash: boundaries[0].Hash}, Child: strecovery.ObservedBlockIdentity{Number: boundaries[1].Number, Hash: boundaries[1].Hash}, Segments: []strecovery.GrandpaFinalitySegment{{Headers: headers[1:], JustificationScale: nativeExecutionTestHex(nativeProducerTestCertificate(t, boundaries[2], consensus, 19, 9))}}}
	combined := economicConservationPolicy{Native: monitorEconomicNativePolicy{Observation: policy}, Vault: monitorEconomicEvmPolicy{From: evm[0]}, MaximumFacts: 1024}
	view := newEconomicConservationArchiveView(combined.initialResources())
	view.finalityPolicy = &policy
	state := &economicConservationState{Native: monitorEconomicNativeState{Cursor: boundaries[1]}, Vault: monitorEconomicEvmState{Cursor: evm[2]}, FinalityApproval: raw, FinalityWindows: []nativeExecutionFinalityWindow{{Anchor: authority.Checkpoint, Proof: proof}}, FinalityFrom: &evm[0], FinalityVaultBlocks: evm[1:], archiveView: view}
	return &economicFinalityFixture{ctx: ctx, policy: combined, state: state, view: view}
}

// Two distinct native ancestors may repeat an EVM hash. That does not prove
// the missing quiet successor, and replay cannot increment the covered count.
func TestEconomicFinalityRepeatedMappingCannotCoverMissingQuietBlock(t *testing.T) {
	f := newEconomicFinalityFixture(t, true)
	window := f.state.FinalityWindows[0]
	// Admit the pending quiet interval first. The later original certificate
	// must resolve each retained requirement once despite repeated mappings.
	f.state.FinalityWindows = nil
	compacted := *f.state
	compacted.Archive = &economicConservationArchive{}
	if err := compacted.retireFinality(f.ctx, f.state); err != nil {
		t.Fatal(err)
	}
	if err := f.view.retainFinality(f.ctx, f.state, &compacted); err != nil {
		t.Fatal(err)
	}
	compacted.archiveView = f.view
	compacted.FinalityWindows = []nativeExecutionFinalityWindow{window, window}
	f.state = &compacted
	var prior *economicConservationFinalitySummary
	for range 3 {
		value, err := f.state.finalitySummary(f.ctx, f.policy)
		if err != nil || value == nil || value.Complete || value.Missing != 1 || value.Head.Covered != 2 || value.Head.Required != 3 || value.Head.Windows != 1 {
			t.Fatal("repeated mapping manufactured quiet-block finality", err, value)
		}
		if prior != nil && !reflect.DeepEqual(prior, value) {
			t.Fatal("same original consensus replay changed its head", prior, value)
		}
		prior = value
	}
}

// A later checkpoint must be exactly the previous certificate's verified
// authority handoff; changing a set id cannot enroll another signing domain.
func TestEconomicFinalityChangedAuthorityCannotSkipCertifiedTip(t *testing.T) {
	f := newEconomicFinalityFixture(t)
	index, err := f.state.prepareFinality(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := f.state.FinalityWindows[0]
	next.Anchor = index.checkpoint
	next.Anchor.SetId++
	next.Proof.CheckpointHash = next.Anchor.Hash()
	f.state.FinalityWindows = append(f.state.FinalityWindows, next)
	if value, err := f.state.finalitySummary(f.ctx, f.policy); err == nil || value != nil || len(f.view.finalityVerified) != 1 {
		t.Fatal("unapproved authority handoff replaced certified-tip checkpoint", err, value)
	}
}

// Every retained key/value map is charged before a candidate can merge. An
// insufficient final covered-map slot must leave the original index unchanged.
func TestEconomicFinalityAllRetainedMapsRequireTwofoldCapacity(t *testing.T) {
	f := newEconomicFinalityFixture(t)
	compacted := *f.state
	compacted.Archive = &economicConservationArchive{}
	if err := compacted.retireFinality(f.ctx, f.state); err != nil {
		t.Fatal(err)
	}
	entries, charged := f.view.entries, f.view.bytes
	byteLimit := f.view.resources.IndexBytes
	f.view.resources.IndexBytes = 2 * charged
	if err := f.view.retainFinality(f.ctx, f.state, &compacted); !errors.Is(err, errMonitorEconomicCapacity) || f.view.finality != nil || f.view.entries != entries || f.view.bytes != charged {
		t.Fatal("refused consensus byte capacity changed original admitted index", err, f.view.entries, f.view.bytes)
	}
	f.view.resources.IndexBytes = byteLimit
	// Anchor and verification caches plus one window and three entries in
	// each native header, EVM hash, requirement and covered membership map.
	f.view.resources.IndexEntries = 28
	if err := f.view.retainFinality(f.ctx, f.state, &compacted); !errors.Is(err, errMonitorEconomicCapacity) || f.view.finality != nil || f.view.entries != entries || f.view.bytes != charged {
		t.Fatal("partial consensus map charges changed original admitted index", err, f.view.entries, f.view.bytes)
	}
	f.view.resources.IndexEntries = 30
	if err := f.view.retainFinality(f.ctx, f.state, &compacted); err != nil || f.view.entries != 15 || len(f.view.finality.evm) != 3 || len(f.view.finality.covered) != 3 {
		t.Fatal("complete original map census did not fit exact twofold entry capacity", err, f.view.entries)
	}
}

// Observing a complete quiet interval without its original certificate is
// pending evidence. A trusted anchor alone does not certify later RPC headers.
func TestEconomicFinalityQuietIntervalWithoutCertificateStaysUnknown(t *testing.T) {
	f := newEconomicFinalityFixture(t)
	f.state.FinalityWindows = nil
	value, err := f.state.finalitySummary(f.ctx, f.policy)
	if err != nil || value == nil || value.Complete || value.Missing != 3 || value.Head.Windows != 0 || value.Head.Covered != 0 || value.Head.VaultBlocks != 2 || len(f.view.finalityVerified) != 0 {
		t.Fatal("quiet observed interval without original certificate acquired finality", err, value)
	}
}

// The complete quiet interval derives coverage; an unrelated receipt boundary
// is missing evidence, while an omitted observed block is an actual contradiction.
func TestEconomicFinalityOriginalAnchorCoversExactQuietInterval(t *testing.T) {
	f := newEconomicFinalityFixture(t)
	value, err := f.state.finalitySummary(f.ctx, f.policy)
	if err != nil || value == nil || !value.Complete || value.Missing != 0 || value.Head.Required != 3 || value.Head.Covered != 3 || value.Head.VaultBlocks != 2 || value.Head.Certified.Number != 102 || f.state.Native.Cursor.Number != 101 {
		t.Fatal("original certificate did not cover the independent exact interval", err, value)
	}
	f.state.Payments = []economicConservationPayment{{Event: monitorEconomicEvmEvent{Block: economicEmissionBoundary{Number: 13, Hash: "0x" + strings.Repeat("d", 64)}}}}
	value, err = f.state.finalitySummary(f.ctx, f.policy)
	if err != nil || value == nil || value.Complete || value.Missing != 1 {
		t.Fatal("uncovered original payment acquired finality", err, value)
	}
	f.state.Payments = nil
	f.state.FinalityVaultBlocks = f.state.FinalityVaultBlocks[:1]
	if value, err = f.state.finalitySummary(f.ctx, f.policy); err == nil || value != nil {
		t.Fatal("missing quiet original block acquired complete coverage", err, value)
	}
}

// A valid foreign checkpoint still cannot replace the independently signed
// initial authority. Invalid certificate bytes never populate the cache.
func TestEconomicFinalityForeignAnchorAndForgedCertificateCannotEnroll(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		f := newEconomicFinalityFixture(t)
		window := &f.state.FinalityWindows[0]
		if foreign {
			window.Anchor.SetId++
			window.Proof.CheckpointHash = window.Anchor.Hash()
			_, tip, err := decodeEconomicFinalityHeader(window.Proof.Segments[0].Headers[1])
			if err != nil {
				t.Fatal(err)
			}
			consensus := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize))
			window.Proof.Segments[0].JustificationScale = nativeExecutionTestHex(nativeProducerTestCertificate(t, tip, consensus, 19, window.Anchor.SetId))
		} else {
			raw, err := historicalReplayHex(window.Proof.Segments[0].JustificationScale, strecovery.MaximumReceiptFinalityBytes)
			if err != nil {
				t.Fatal(err)
			}
			raw[80] ^= 1
			window.Proof.Segments[0].JustificationScale = nativeExecutionTestHex(raw)
		}
		if value, err := f.state.finalitySummary(f.ctx, f.policy); err == nil || value != nil || len(f.view.finalityVerified) != 0 || f.view.finality != nil {
			t.Fatal("foreign or forged original proof entered finality authority", foreign, err, value)
		}
	}
}

// Successful cryptography is reused by exact proof identity. Cancellation or
// closed custody after that work cannot publish or cache its apparent success.
func TestEconomicFinalityVerificationCacheRetainsOwnerAndCancellation(t *testing.T) {
	for _, closeOwner := range []bool{false, true} {
		f := newEconomicFinalityFixture(t)
		ctx, cancel := context.WithCancel(f.ctx)
		calls := 0
		f.view.finalityWork = func(context.Context) {
			calls++
			if closeOwner {
				if err := f.view.close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
		}
		value, err := f.state.finalitySummary(ctx, f.policy)
		cancel()
		wanted := context.Canceled
		if closeOwner {
			wanted = os.ErrClosed
		}
		if value != nil || !errors.Is(err, wanted) || calls != 1 || len(f.view.finalityVerified) != 0 || f.view.entries != 1 {
			t.Fatal("late canceled or closed consensus verification published a cache", closeOwner, err, value, calls)
		}
	}
	f := newEconomicFinalityFixture(t)
	calls := 0
	f.view.finalityWork = func(context.Context) { calls++ }
	var original *economicConservationFinalitySummary
	var entries, charged uint64
	for attempt := 0; attempt < 3; attempt++ {
		value, err := f.state.finalitySummary(f.ctx, f.policy)
		if err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			original = value
			entries, charged = f.view.entries, f.view.bytes
		}
		if calls != 1 || !reflect.DeepEqual(original, value) || f.view.entries != entries || f.view.bytes != charged {
			t.Fatal("same original proof repeated cryptography or capacity charges", attempt, calls)
		}
	}
}

// Cold admission reconstructs the original proof chain and every obligation.
// The compact head alone cannot manufacture a certificate or discard a gap.
func TestEconomicFinalityColdRetirementRetainsOriginalProofAndObligations(t *testing.T) {
	f := newEconomicFinalityFixture(t)
	original, err := f.state.finalitySummary(f.ctx, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	compacted := *f.state
	compacted.Archive = &economicConservationArchive{}
	if err := compacted.retireFinality(f.ctx, f.state); err != nil {
		t.Fatal(err)
	}
	if err := f.view.retainFinality(f.ctx, f.state, &compacted); err != nil {
		t.Fatal(err)
	}
	compacted.archiveView = f.view
	value, err := compacted.finalitySummary(f.ctx, f.policy)
	if err != nil || !reflect.DeepEqual(value, original) || len(compacted.FinalityWindows) != 0 || len(compacted.FinalityVaultBlocks) != 0 {
		t.Fatal("cold original certificate coverage changed", err, value, original)
	}
	compacted.Archive.Finality.Covered--
	if value, err = compacted.finalitySummary(f.ctx, f.policy); err == nil || value != nil {
		t.Fatal("self-sealed cold consensus census acquired authority", err, value)
	}
}
