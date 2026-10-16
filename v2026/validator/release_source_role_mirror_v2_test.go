//go:build linux || darwin

// A coordinator mirror of the validator hotkey is accepted only as an exact
// copy of that hotkey's own archived native write. The mirror cannot be
// cleared, so the proof must not depend on which intents are still retained.
package validator

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The receipt block holds the fixture's finalized source write. A later
// canonical head has a scripted slot, by default an unchanged one.
type sourceRoleMirrorTestV2 struct {
	f        *sourceRolePredecessorTestFixtureV2
	steerer  *ReleaseSteerer
	snapshot *ReleaseSnapshot
	mirror   atomic.Pointer[[]byte]
	// The source hash written at the receipt block.
	hash       [32]byte
	head       types.Hash
	headNumber uint64
	headSlot   []byte
	headLast   uint64
}

func sourceRoleMirrorV2Raw(hash [32]byte, blockHash types.Hash, block uint64) []byte {
	raw := make([]byte, 96)
	copy(raw[:32], hash[:])
	copy(raw[32:64], blockHash[:])
	binary.BigEndian.PutUint64(raw[88:], block)
	return raw
}

func newSourceRoleMirrorTestV2(t *testing.T) *sourceRoleMirrorTestV2 {
	t.Helper()
	f := newSourceRolePredecessorTestFixtureV2(t)
	cfg := &f.historical.config
	witness, err := authenticateReleaseSourceRolePredecessorV2(t.Context(), cfg, f.historical.native.chain, f.hotkey.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	native := *f.historical.native.chain
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), &native, cfg, f.receipt.hash); err != nil {
		t.Fatal(err)
	}
	self := &sourceRoleMirrorTestV2{f: f, snapshot: &ReleaseSnapshot{BlockNumber: 90, BlockHash: [32]byte(common.Hash{0x23})},
		hash: [32]byte(common.HexToHash(f.historical.intent.Prepared.SourceCommitment.Hash))}
	self.mirror.Store(&[]byte{})
	header, head := releaseReceiptTestHeader(t, f.receipt.hash, f.receipt.number+1)
	self.head, self.headNumber = head, f.receipt.number+1
	self.setHeadSlot(t, self.hash, f.receipt.number)
	netuid, hotkey := binary.LittleEndian.AppendUint16(nil, cfg.Netuid), f.hotkey.PublicKey()
	commitmentKey, err := types.CreateStorageKey(native.Meta, "Commitments", "CommitmentOf", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(native.Meta, "Commitments", "LastCommitment", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	prior := native.API.Client
	native.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch method {
		case "chain_getBlockHash":
			if len(args) == 1 && fmt.Sprint(args[0]) == fmt.Sprint(self.headNumber) {
				return setReleaseHistoricalTestResult(result, head.Hex())
			}
		case "chain_getHeader":
			if len(args) == 1 && args[0] == head.Hex() {
				return setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(header))
			}
		case "state_getStorage":
			if len(args) == 2 && args[1] == head.Hex() && args[0] == commitmentKey.Hex() {
				return setReleaseHistoricalTestResult(result, hexutil.Encode(self.headSlot))
			}
			if len(args) == 2 && args[1] == head.Hex() && args[0] == lastKey.Hex() {
				return setReleaseHistoricalTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint32(nil, uint32(self.headLast))))
			}
		}
		return prior.CallContext(ctx, result, method, args...)
	}}
	store, err := newReleaseIntentStoreV2(&releaseRuntimeV2{ctx: t.Context(), cfg: *cfg, history: &releaseEvidenceV2StartupHistory{}})
	if err != nil {
		t.Fatal(err)
	}
	self.steerer = &ReleaseSteerer{cfg: cfg, native: &native, chain: sourceRoleMirrorChainV2(t, hotkey, self.snapshot, &self.mirror), hotkey: f.hotkey, intents: store, sourceRolePredecessorV2: witness}
	return self
}

// The coordinator answers its mirror for this hotkey at the canonical snapshot.
func sourceRoleMirrorChainV2(t *testing.T, hotkey [32]byte, snapshot *ReleaseSnapshot, mirror *atomic.Pointer[[]byte]) *ChainClient {
	t.Helper()
	coordinator := stabi.NewSTCoordinator()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		var result any
		switch call.Method {
		case "eth_getBlockByHash":
			result = map[string]any{"number": hexutil.EncodeUint64(snapshot.BlockNumber), "hash": common.Hash(snapshot.BlockHash).Hex()}
		case "eth_call":
			var envelope struct {
				Input string `json:"input"`
			}
			var selector struct {
				BlockHash        common.Hash `json:"blockHash"`
				RequireCanonical bool        `json:"requireCanonical"`
			}
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &envelope) != nil || json.Unmarshal(call.Params[1], &selector) != nil || envelope.Input != hexutil.Encode(coordinator.PackMirroredCommitments(hotkey)) || selector.BlockHash != common.Hash(snapshot.BlockHash) || !selector.RequireCanonical {
				t.Error("role mirror read changed its actor or canonical snapshot")
				http.Error(writer, "invalid role request", http.StatusBadRequest)
				return
			}
			result = hexutil.Encode(*mirror.Load())
		default:
			t.Errorf("unexpected role rpc %s", call.Method)
			http.Error(writer, "unexpected method", http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &ChainClient{client: client, coordinator: coordinator, release: true}
}

// The later head's slot: exactly one Sha256 field written at block.
func (self *sourceRoleMirrorTestV2) setHeadSlot(t *testing.T, hash [32]byte, block uint64) {
	t.Helper()
	info, err := crv4.EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	self.headSlot = append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(nil, 1), uint32(block)), info...)
	self.headLast = block
}

func (self *sourceRoleMirrorTestV2) check(t *testing.T, raw []byte, nativeHash types.Hash, previous *SteeringIntent) error {
	t.Helper()
	self.mirror.Store(&raw)
	return self.steerer.checkSourceRoleV2(t.Context(), self.snapshot, nativeHash, previous)
}

// The deployed oracle mirrored the validator's own finalized source write. The
// exact copy passes with whichever retained proof owns the current slot, and
// the proof is the archived write itself. A zero mirror passes as before.
func TestReleaseSourceRoleMirrorV2AcceptsExactOwnWrite(t *testing.T) {
	m := newSourceRoleMirrorTestV2(t)
	receipt, intent := m.f.receipt, m.f.historical.intent
	if intent.Status != "finalized" || intent.FinalizedBlock != receipt.number || intent.FinalizedBlockHash != receipt.hash.Hex() {
		t.Fatalf("fixture intent is not the finalized receipt: %+v", intent)
	}
	exact := sourceRoleMirrorV2Raw(m.hash, receipt.hash, receipt.number)
	reads := receipt.commitmentReads
	if err := m.check(t, make([]byte, 96), receipt.hash, nil); err != nil {
		t.Fatalf("zero mirror was rejected: %v", err)
	}
	slotReads := receipt.commitmentReads - reads
	reads = receipt.commitmentReads
	if err := m.check(t, exact, receipt.hash, nil); err != nil {
		t.Fatalf("exact own mirror rejected with the predecessor proof: %v", err)
	}
	if proofReads := receipt.commitmentReads - reads - slotReads; proofReads != 2 {
		t.Fatalf("exact mirror made %d reads at its own block, want its registration and LastCommitment", proofReads)
	}
	m.steerer.sourceRolePredecessorV2 = nil
	if err := m.check(t, exact, receipt.hash, intent); err != nil {
		t.Fatalf("exact own mirror rejected with the previous intent: %v", err)
	}
	if err := m.check(t, exact, receipt.hash, nil); err == nil || !strings.Contains(err.Error(), "unretained write") {
		t.Fatalf("an accepted mirror excused an unretained native slot: %v", err)
	}
}

// Only the exact write passes: another hash, another block, another block
// hash, or a malformed or partial record is the original rejection.
func TestReleaseSourceRoleMirrorV2RejectsAnyOtherMirror(t *testing.T) {
	m := newSourceRoleMirrorTestV2(t)
	receipt := m.f.receipt
	hash, blockHash, block := m.hash, receipt.hash, receipt.number
	foreign := hash
	foreign[31] ^= 1
	exact := sourceRoleMirrorV2Raw(hash, blockHash, block)
	wide := sourceRoleMirrorV2Raw(hash, blockHash, block)
	wide[87] = 1
	for name, raw := range map[string][]byte{
		"short":                            exact[:64],
		"one byte short":                   exact[:95],
		"long":                             append(append([]byte(nil), exact...), 0),
		"double":                           append(append([]byte(nil), exact...), exact...),
		"foreign hash":                     sourceRoleMirrorV2Raw(foreign, blockHash, block),
		"same hash at the next block":      sourceRoleMirrorV2Raw(hash, blockHash, block+1),
		"same hash at the previous block":  sourceRoleMirrorV2Raw(hash, blockHash, block-1),
		"same write under a later hash":    sourceRoleMirrorV2Raw(hash, m.head, block),
		"same write under the later block": sourceRoleMirrorV2Raw(hash, m.head, m.headNumber),
		"same write under an unknown hash": sourceRoleMirrorV2Raw(hash, types.Hash{0x44}, block),
		"zero hash":                        sourceRoleMirrorV2Raw([32]byte{}, blockHash, block),
		"zero block hash":                  sourceRoleMirrorV2Raw(hash, types.Hash{}, block),
		"zero block":                       sourceRoleMirrorV2Raw(hash, blockHash, 0),
		"block beyond uint64":              wide,
	} {
		if err := m.check(t, raw, blockHash, nil); err == nil || !strings.Contains(err.Error(), releaseSourceRoleMirrorV2Rejection) {
			t.Fatalf("%s mirror was not rejected as a fleet mirror: %v", name, err)
		}
	}
	if err := m.check(t, exact, blockHash, nil); err != nil {
		t.Fatalf("exact own mirror rejected after the faults: %v", err)
	}
}

// The mirror never moves while the validator keeps writing. Once no retained
// intent or predecessor holds the mirrored write, the archived block proves it.
func TestReleaseSourceRoleMirrorV2AcceptsMirrorOlderThanRetainedIntents(t *testing.T) {
	m := newSourceRoleMirrorTestV2(t)
	receipt := m.f.receipt
	newer := [32]byte{0x6b, 0x01}
	m.setHeadSlot(t, newer, m.headNumber)
	latest := &SteeringIntent{Status: "applied", FinalizedBlock: m.headNumber, FinalizedBlockHash: m.head.Hex(),
		Prepared: &crv4.PreparedSubmission{SourceCommitment: &crv4.PreparedSourceCommitment{Hash: releaseHex32(newer)}}}
	m.steerer.sourceRolePredecessorV2 = nil
	if history, err := m.steerer.intents.authenticatedIntentsV2(t.Context()); err != nil || len(history) != 0 {
		t.Fatalf("store retains an intent: %d %v", len(history), err)
	}
	old := sourceRoleMirrorV2Raw(m.hash, receipt.hash, receipt.number)
	if err := m.check(t, old, m.head, latest); err != nil {
		t.Fatalf("mirror of an unretained own write was rejected: %v", err)
	}
	for name, raw := range map[string][]byte{
		"newer hash at the old block":    sourceRoleMirrorV2Raw(newer, receipt.hash, receipt.number),
		"old hash at the newer block":    sourceRoleMirrorV2Raw(m.hash, m.head, m.headNumber),
		"old hash with the newer number": sourceRoleMirrorV2Raw(m.hash, receipt.hash, m.headNumber),
		"newer hash with the old number": sourceRoleMirrorV2Raw(newer, m.head, receipt.number),
		"foreign hash at the old block":  sourceRoleMirrorV2Raw([32]byte{0x6b, 0x02}, receipt.hash, receipt.number),
	} {
		if err := m.check(t, raw, m.head, latest); err == nil || !strings.Contains(err.Error(), releaseSourceRoleMirrorV2Rejection) {
			t.Fatalf("%s mirror was not rejected as a fleet mirror: %v", name, err)
		}
	}
	if err := m.check(t, sourceRoleMirrorV2Raw(newer, m.head, m.headNumber), m.head, latest); err != nil {
		t.Fatalf("mirror of the latest own write was rejected: %v", err)
	}
	if err := m.check(t, old, m.head, nil); err == nil || !strings.Contains(err.Error(), "unretained write") {
		t.Fatalf("an accepted old mirror excused an unretained native slot: %v", err)
	}
}

// A finalized intent retained only in the actual production intent store owns
// the slot. The exact mirror of its write passes through that history alone.
func TestReleaseSourceRoleMirrorV2AcceptsExactWriteOfRetainedHistory(t *testing.T) {
	continuation := newProductionContinuationTestFixture(t)
	pending, err := continuation.steerer.intents.beginV2(t.Context(), *continuation.intent)
	if err != nil {
		t.Fatal(err)
	}
	fixture := installProductionSourceReceiptTest(t, continuation, false)
	continuation.runtime.cfg = *fixture.current
	continuation.steerer.cfg = &continuation.runtime.cfg
	if done, err := continuation.steerer.reconcilePendingV2(t.Context(), pending, nil); err != nil || !done {
		t.Fatalf("retained production receipt did not finalize: done=%t error=%v", done, err)
	}
	receipt := fixture.receipt
	finalized := continuation.restart(t)
	if finalized.Status != "finalized" || finalized.FinalizedBlock != receipt.BlockNumber || finalized.FinalizedBlockHash != receipt.BlockHash.Hex() {
		t.Fatalf("store does not retain the finalized receipt: %+v", finalized)
	}
	steerer := continuation.steerer
	snapshot := &ReleaseSnapshot{BlockNumber: 90, BlockHash: [32]byte(common.Hash{0x23})}
	var mirror atomic.Pointer[[]byte]
	steerer.chain = sourceRoleMirrorChainV2(t, steerer.hotkey.PublicKey(), snapshot, &mirror)
	steerer.sourceRolePredecessorV2 = nil
	hash := [32]byte(common.HexToHash(finalized.Prepared.SourceCommitment.Hash))
	foreign := hash
	foreign[31] ^= 1
	check := func(raw []byte) error {
		mirror.Store(&raw)
		return steerer.checkSourceRoleV2(t.Context(), snapshot, receipt.BlockHash, nil)
	}
	if err := check(make([]byte, 96)); err != nil {
		t.Fatalf("zero mirror rejected with the retained history: %v", err)
	}
	if err := check(sourceRoleMirrorV2Raw(hash, receipt.BlockHash, receipt.BlockNumber)); err != nil {
		t.Fatalf("exact own mirror rejected with the retained history: %v", err)
	}
	for name, raw := range map[string][]byte{
		"same hash at the next block":     sourceRoleMirrorV2Raw(hash, receipt.BlockHash, receipt.BlockNumber+1),
		"same hash at the previous block": sourceRoleMirrorV2Raw(hash, receipt.BlockHash, receipt.BlockNumber-1),
		"foreign hash":                    sourceRoleMirrorV2Raw(foreign, receipt.BlockHash, receipt.BlockNumber),
		"short":                           make([]byte, 64),
	} {
		if err := check(raw); err == nil || !strings.Contains(err.Error(), releaseSourceRoleMirrorV2Rejection) {
			t.Fatalf("%s mirror was not rejected as a fleet mirror: %v", name, err)
		}
	}
}

// The validator keeps writing after the oracle mirrored its receipt's write:
// two newer source commitments land at the next two blocks and the slot moves
// to the latest. The old mirror still names exactly what its own block held,
// so it passes against every later slot, as does a mirror of either newer
// write. A record naming anything else in this hotkey's archive still fails,
// and an accepted mirror never excuses a slot no retained proof owns.
func TestReleaseSourceRoleMirrorV2OldMirrorSurvivesNewerSourceWrites(t *testing.T) {
	m := newSourceRoleMirrorTestV2(t)
	receipt, cfg, native := m.f.receipt, &m.f.historical.config, m.steerer.native
	netuid, hotkey := binary.LittleEndian.AppendUint16(nil, cfg.Netuid), m.f.hotkey.PublicKey()
	commitmentKey, err := types.CreateStorageKey(native.Meta, "Commitments", "CommitmentOf", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(native.Meta, "Commitments", "LastCommitment", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	// One canonical block after the receipt per index: the validator writes
	// at the first two, and the third is the finalized head.
	type archivedBlock struct {
		number, written uint64
		hash            types.Hash
		header          types.Header
		source          [32]byte
	}
	newer := [][32]byte{{0x6b, 0x11}, {0x6b, 0x12}}
	var blocks []archivedBlock
	parent, source, written := receipt.hash, m.hash, receipt.number
	for index := range 3 {
		number := receipt.number + 1 + uint64(index)
		if index < len(newer) {
			source, written = newer[index], number
		}
		header, hash := releaseReceiptTestHeader(t, parent, number)
		blocks = append(blocks, archivedBlock{number: number, written: written, hash: hash, header: header, source: source})
		parent = hash
	}
	head := blocks[len(blocks)-1]
	prior := native.API.Client
	native.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, block := range blocks {
			switch {
			case method == "chain_getBlockHash" && len(args) == 1 && fmt.Sprint(args[0]) == fmt.Sprint(block.number):
				return setReleaseHistoricalTestResult(result, block.hash.Hex())
			case method == "chain_getHeader" && len(args) == 1 && args[0] == block.hash.Hex():
				return setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(block.header))
			case method == "state_getStorage" && len(args) == 2 && args[1] == block.hash.Hex() && args[0] == commitmentKey.Hex():
				info, err := crv4.EncodeFleetCommitmentInfo(block.source)
				if err != nil {
					return err
				}
				return setReleaseHistoricalTestResult(result, hexutil.Encode(append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(nil, 1), uint32(block.written)), info...)))
			case method == "state_getStorage" && len(args) == 2 && args[1] == block.hash.Hex() && args[0] == lastKey.Hex():
				return setReleaseHistoricalTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint32(nil, uint32(block.written))))
			}
		}
		return prior.CallContext(ctx, result, method, args...)
	}}
	latest := &SteeringIntent{Status: "applied", FinalizedBlock: blocks[1].number, FinalizedBlockHash: blocks[1].hash.Hex(),
		Prepared: &crv4.PreparedSubmission{SourceCommitment: &crv4.PreparedSourceCommitment{Hash: releaseHex32(newer[1])}}}
	m.steerer.sourceRolePredecessorV2 = nil
	for name, raw := range map[string][]byte{
		"the receipt's write":    sourceRoleMirrorV2Raw(m.hash, receipt.hash, receipt.number),
		"the first newer write":  sourceRoleMirrorV2Raw(newer[0], blocks[0].hash, blocks[0].number),
		"the second newer write": sourceRoleMirrorV2Raw(newer[1], blocks[1].hash, blocks[1].number),
	} {
		if err := m.check(t, raw, head.hash, latest); err != nil {
			t.Fatalf("mirror of %s failed after the newer writes: %v", name, err)
		}
	}
	foreign := m.hash
	foreign[31] ^= 1
	for name, raw := range map[string][]byte{
		"foreign hash at the receipt":         sourceRoleMirrorV2Raw(foreign, receipt.hash, receipt.number),
		"newer hash at the receipt":           sourceRoleMirrorV2Raw(newer[0], receipt.hash, receipt.number),
		"old hash after the slot moved":       sourceRoleMirrorV2Raw(m.hash, blocks[0].hash, blocks[0].number),
		"old hash at the head":                sourceRoleMirrorV2Raw(m.hash, head.hash, head.number),
		"latest write read at the head":       sourceRoleMirrorV2Raw(newer[1], head.hash, blocks[1].number),
		"another hotkey's write at its block": sourceRoleMirrorV2Raw([32]byte{0x7c, 0x01}, blocks[1].hash, blocks[1].number),
	} {
		if err := m.check(t, raw, head.hash, latest); err == nil || !strings.Contains(err.Error(), releaseSourceRoleMirrorV2Rejection) {
			t.Fatalf("%s mirror was not rejected as a fleet mirror: %v", name, err)
		}
	}
	if err := m.check(t, sourceRoleMirrorV2Raw(m.hash, receipt.hash, receipt.number), head.hash, nil); err == nil || !strings.Contains(err.Error(), "unretained write") {
		t.Fatalf("an accepted old mirror excused the moved slot: %v", err)
	}
}
