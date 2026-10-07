//go:build linux || darwin

// Real signed M8 ledgers and the actual native adapter establish the capsule's
// authority. Synthetic mainnet identity is selected before any proof is signed.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Each fixture owns its real approval custody, RPC transcript and proof stores.
type recycleMeasurementFixture struct {
	admission *recycleAdmissionFixture
	provider  *releaseMeasurementV2TestFixture
	encoded   []byte
	authority *OwnerRecycleMeasurementAuthority
}

// The mainnet ledger domain is declared before creating any records. Updating
// unsigned decision-time snapshots never rewrites a previously signed identity.
func newRecycleMeasurementFixture(t *testing.T) *recycleMeasurementFixture {
	t.Helper()
	return newRecycleMeasurementFixtureWithCompleted(t, 2)
}

// The positive control crosses the real pool-quality minimum; failure controls
// need only a nonempty genuine head and keep the smaller production M8 corpus.
func newRecycleMeasurementFixtureWithCompleted(t *testing.T, completed int) *recycleMeasurementFixture {
	t.Helper()
	return newRecycleMeasurementFixtureWithSetup(t, completed, nil)
}

// Route and raw decision observations are selected before approval is signed.
func newRecycleMeasurementFixtureWithSetup(t *testing.T, completed int, setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture)) *recycleMeasurementFixture {
	return newRecycleMeasurementFixtureWithHotkey(t, completed, setup, [32]byte{0x15})
}

// Select the actual hotkey before any compact proof or approval is signed.
func newRecycleMeasurementFixtureWithHotkey(t *testing.T, completed int, setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture), hotkey [32]byte) *recycleMeasurementFixture {
	return newRecycleMeasurementFixtureWithPolicy(t, completed, setup, hotkey, nil)
}

// A distinct policy is selected before the first proof, ledger signature or
// proposal approval; existing economic fixtures retain their paid policy.
func newRecycleMeasurementFixtureWithPolicy(t *testing.T, completed int, setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture), hotkey [32]byte, policy *protocol.Policy) *recycleMeasurementFixture {
	return newRecycleMeasurementFixtureWithActivation(t, completed, setup, hotkey, policy, nil)
}

// The startup variant signs an actual activation record before compact proof
// construction, with a ledger-owned first egress generation for empty history.
func newRecycleMeasurementFixtureWithActivation(t *testing.T, completed int, setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture), hotkey [32]byte, policy *protocol.Policy, anchor func(*recycleAdmissionFixture, *attemptCutV2SealTestFixture)) *recycleMeasurementFixture {
	t.Helper()
	admission := newRecycleAdmissionFixture(t, nil)
	if policy != nil {
		admission.cfg.Policy = *policy
		admission.cfg.PolicyHash, _ = policy.HashHex()
		admission.approval.Proposal.ParentPolicyHash, _ = policy.Hash()
	}
	identity := AttemptLedgerIdentity{DeploymentID: "synthetic-recycle-measured", ChainID: 964, GenesisHash: admission.cfg.GenesisHash,
		Netuid: 25, ValidatorID: 1, ValidatorUID: 7}
	var activate func(*attemptCutV2SealTestFixture)
	if anchor != nil {
		activate = func(seal *attemptCutV2SealTestFixture) {
			// The source already persisted this activation before its first
			// trail. Repeating the declarative anchor cannot reinterpret it.
			original := seal.expected.Activation
			anchor(admission, seal)
			if seal.expected.Activation != original {
				t.Fatal("startup compact sealing changed its durable activation")
			}
		}
	}
	provider := newReleaseMeasurementV2TestFixtureWithActivation(t, completed, func(noId uint64) *attemptCutV2SealTestFixture {
		selected := identity
		selected.NoID = noId
		if anchor == nil {
			fixture := newAttemptCutV2SealTestFixtureForDomain(t, 8, completed, 1, false, admission.cfg.Policy, selected)
			fixture.expected.Activation.Hotkey = hotkey
			return fixture
		}
		return newRecycleActivatedMeasurementTestSource(t, admission, selected, hotkey, completed, anchor)
	}, activate)
	artifact := provider.artifact
	artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash = 100, admission.finalized.Hex()
	for index := range artifact.Inputs {
		artifact.Inputs[index].CutNativeBlock = artifact.NativeSnapshotBlock
		artifact.Inputs[index].CutNativeBlockHash = artifact.NativeSnapshotHash
	}
	admission.cfg.DeploymentID, admission.cfg.Coordinator = artifact.DeploymentID, artifact.Coordinator
	admission.cfg.SettlementVault = artifact.SettlementVault
	admission.cfg.Operators = []OperatorConfig{{NoID: 9}, {NoID: 10}}
	admission.approval.ValidatorHotkey = hotkey
	admission.approval.FirstNativeEpoch = artifact.SubnetEpoch
	admission.approval.MaximumSubnetUids = 111
	if setup != nil {
		setup(admission, provider)
	}
	admission.approval.ConfigHash, _ = OwnerRecycleConfigHash(admission.cfg)
	_, _, metadata := recycleAdmissionTestMetadata(t, nil)
	netuid := binary.LittleEndian.AppendUint16(nil, 25)
	put := func(name string, value []byte, args ...[]byte) {
		key, err := types.CreateStorageKey(metadata, "SubtensorModule", name, args...)
		if err != nil {
			t.Fatal(err)
		}
		admission.storage[key.Hex()] = codec.HexEncodeToString(value)
	}
	put("SubnetworkN", binary.LittleEndian.AppendUint16(nil, 111), netuid)
	put("SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, artifact.SubnetEpoch), netuid)
	hotkeys := map[uint16][32]byte{artifact.SelfUID: admission.approval.ValidatorHotkey}
	for _, binding := range artifact.Bindings {
		if binding.LiveUIDFound {
			hotkeys[binding.LiveUID], _ = parseReleaseHex32("synthetic binding", binding.Hotkey, false)
		}
	}
	for _, pool := range artifact.Pools {
		hotkeys[pool.UID], _ = parseReleaseHex32("synthetic pool", pool.PoolHotkey, false)
	}
	for uid := uint16(0); uid < 111; uid++ {
		hotkey, exists := hotkeys[uid]
		if !exists {
			hotkey = recycleTestId(uid)
		}
		uidBytes := binary.LittleEndian.AppendUint16(nil, uid)
		put("Keys", hotkey[:], netuid, uidBytes)
		put("Uids", uidBytes, netuid, hotkey[:])
		put("BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 10), netuid, uidBytes)
	}
	admission.sign(t)
	admission.retain(t)
	authority, err := ObserveOwnerRecycleMeasurementAuthority(t.Context(), admission.cfg, admission.chain, releaseMeasurementV2Decision(artifact))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := SealReleaseMeasurementArtifactV2(t.Context(), artifact, provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	return &recycleMeasurementFixture{admission: admission, provider: provider, encoded: encoded, authority: authority}
}

// A startup source executes the real empty-prefix V2 transaction before any
// M8 work. Its single local operator is the complete declared source census;
// both independent sources are later joined by the public production root.
// No activation marker or reconstructed counters are pasted into a snapshot.
func newRecycleActivatedMeasurementTestSource(t *testing.T, admission *recycleAdmissionFixture, identity AttemptLedgerIdentity, hotkey [32]byte, completed int, anchor func(*recycleAdmissionFixture, *attemptCutV2SealTestFixture)) *attemptCutV2SealTestFixture {
	t.Helper()
	fixture := newAttemptCutV2SealTestFixtureForDomain(t, 8, 0, 0, true, admission.cfg.Policy, identity)
	fixture.expected.Activation.Hotkey = hotkey
	anchor(admission, fixture)
	seal, _ := newAttemptCutV2SealTestOptions(t, fixture)
	owner := &attemptSettlementRuntimeV2TestFixture{
		coordinator:  newAttemptSettlementRuntimeV2TestStateDir(t),
		participants: []AttemptSettlementRuntimeV2Participant{{NoID: identity.NoID, StateDir: filepath.Dir(fixture.ledger.path), Stats: fixture.engine.stats, Ledger: fixture.ledger}},
		fixtures:     []*attemptCutV2SealTestFixture{fixture}, sealers: map[uint64]AttemptCutV2SealOptions{identity.NoID: seal},
	}
	if err := InitializeAttemptSettlementEpochV2(t.Context(), owner.coordinator, owner.participants, fixture.expected.Boundary.SettlementEpoch, owner.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatalf("actual source activation before genuine M8 work: %v", err)
	}
	initial := fixture.engine.stats.snapshotStats()
	if initial.AttemptV2 == nil || initial.AttemptV2.Activation != fixture.expected.Activation || initial.AttemptLastAppliedSequence != 0 || initial.EgressGeneration != 1 {
		t.Fatal("real initial source owner did not retain its exact empty activation")
	}
	owner.trails(t, 0, completed, 1)
	head, err := fixture.ledger.Head()
	if err != nil || head.LastSequence != uint64(completed*8+2) {
		t.Fatalf("activated source lost its genuine complete/failed trail census: %v", err)
	}
	if err := fixture.ledger.Walk(t.Context(), 1, head.LastSequence, func(record AttemptRecord) error {
		if err := VerifyAttemptRecord(&record, fixture.ledger.identity, fixture.key.Public().(ed25519.PublicKey), fixture.server.serverPublicKeys()); err != nil {
			return err
		}
		fixture.recordTs = append(fixture.recordTs, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// Fresh replay namespaces prevent fixture retries from borrowing an old result.
func (self *recycleMeasurementFixture) seal(t *testing.T) ([]byte, *OwnerRecycleDecisionIntent) {
	t.Helper()
	encoded, intent, err := SealOwnerRecycleMeasurement(t.Context(), self.authority, self.encoded, self.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	return encoded, intent
}

// The row is derived through real proof replay, while the old wire and every
// production send fence remain intact. Equal-share sums are independent checks.
func TestOwnerRecycleMeasurementJoinsMeasuredRowAndBlockedIntent(t *testing.T) {
	fixture := newRecycleMeasurementFixtureWithCompleted(t, 15)
	original := bytes.Clone(fixture.encoded)
	encoded, intent := fixture.seal(t)
	if intent.Status != OwnerRecycleDecisionBlocked || intent.ActivationReady || intent.NativeOutcomeVerified || len(intent.Blockers) != 4 {
		t.Fatal("proposed measurement acquired activation or outcome authority")
	}
	if intent.CapsuleHash != ReleaseMeasurementContentHash(encoded) || intent.ProviderMeasurementHash != ReleaseMeasurementContentHash(original) || intent.ApprovalHash != fixture.admission.cfg.OwnerRecycleApproval.Approval.SHA256 {
		t.Fatal("decision intent lost exact evidence references")
	}
	providerShare, ownerShare := new(big.Rat), new(big.Rat)
	for index, uid := range intent.Row.Uids {
		score, err := decodeRationalJSON(intent.Row.Scores[index])
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(intent.Row.OwnerUids, uid) {
			ownerShare.Add(ownerShare, score)
			if score.Cmp(big.NewRat(9, 20)) != 0 {
				t.Fatal("recognized owner did not receive its exact proposed half of nine tenths")
			}
		} else {
			providerShare.Add(providerShare, score)
		}
	}
	if providerShare.Cmp(big.NewRat(1, 10)) != 0 || ownerShare.Cmp(big.NewRat(9, 10)) != 0 {
		t.Fatalf("proposal shares %s / %s", providerShare, ownerShare)
	}
	if err := VerifyOwnerRecycleDecisionIntent(t.Context(), fixture.authority, encoded, fixture.provider.options(t), intent); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fixture.encoded, original) {
		t.Fatal("successor changed original provider evidence")
	}
	_, parent, err := DecodeReleaseMeasurementArtifactV2(t.Context(), original, fixture.provider.options(t))
	if err != nil || !reflect.DeepEqual(parent.Decision.UIDs, fixture.provider.want.UIDs) || !reflect.DeepEqual(parent.Decision.Scores, fixture.provider.want.Scores) {
		t.Fatalf("original V2 parent meaning changed: %v", err)
	}
	if _, _, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, fixture.provider.options(t)); err == nil {
		t.Fatal("old measurement reader accepted a successor capsule")
	}
	if ownerRecycleProductionBoundary(fixture.admission.cfg) == nil {
		t.Fatal("measurement capsule removed the production send fence")
	}
}

// Restart needs retained approval and actual same-hash RPC replay, even after
// the latest head advances and the original supplied approval file disappears.
func TestOwnerRecycleMeasurementReplaysOriginalBlockAfterHeadAdvances(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	encoded, intent := fixture.seal(t)
	if err := os.Remove(fixture.admission.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	_, fixture.admission.headHash = releaseReceiptTestHeader(t, types.Hash(recycleTestId(1200)), 150)
	fixture.admission.headNumber = 150
	reobserved, err := ObserveOwnerRecycleMeasurementAuthority(t.Context(), fixture.admission.cfg, fixture.admission.chain, fixture.authority.expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyOwnerRecycleDecisionIntent(t.Context(), reobserved, encoded, fixture.provider.options(t), intent); err != nil {
		t.Fatalf("exact original decision did not survive a later head: %v", err)
	}
	fixture.admission.headNumber = 99
	if got, err := ObserveOwnerRecycleMeasurementAuthority(t.Context(), fixture.admission.cfg, fixture.admission.chain, fixture.authority.expected); err == nil || got != nil {
		t.Fatal("decision newer than finalized head was admitted")
	}
}

// A changed approval, census, schema or status cannot borrow the original
// authority, even when the outer object is canonical and independently rehashed.
func TestOwnerRecycleMeasurementRejectsRetargetedAuthorityBeforeProofReads(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	encoded, _ := fixture.seal(t)
	for _, fault := range []string{"approval", "census", "schema", "status", "unknown"} {
		var capsule OwnerRecycleMeasurement
		if err := json.Unmarshal(encoded, &capsule); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "approval":
			capsule.Approval[0] ^= 1
		case "census":
			capsule.Census[0] ^= 1
		case "schema":
			capsule.Schema = ReleaseMeasurementSchemaV2
		case "status":
			capsule.Status = "ready"
		}
		changed, _ := json.Marshal(capsule)
		if fault == "unknown" {
			changed = append(changed[:len(changed)-1], []byte(",\"approved\":true}")...)
		}
		changed = append(changed, '\n')
		options := fixture.provider.options(t)
		reads := 0
		for id, operator := range options.Operators {
			operator.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
				reads++
				return nil, errors.New("unexpected proof read")
			}
			options.Operators[id] = operator
		}
		if result, err := ReplayOwnerRecycleMeasurement(t.Context(), fixture.authority, changed, options); result != nil || err == nil || reads != 0 {
			t.Fatalf("%s authority reached proof readers: result=%v err=%v reads=%d", fault, result != nil, err, reads)
		}
	}
}

// A valid capsule cannot authorize a different native boundary, parent policy,
// controlled mask, configured operator census or borrowed predecessor policy.
func TestOwnerRecycleMeasurementRejectsReplayAuthorityDrift(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	encoded, _ := fixture.seal(t)
	for _, fault := range []string{"native-hash", "policy", "mask", "operator", "predecessor"} {
		options := fixture.provider.options(t)
		switch fault {
		case "native-hash":
			options.Expected.NativeSnapshotHash = releaseHex32(recycleTestId(1600))
		case "policy":
			options.Policy.PolicyID++
		case "mask":
			options.ControlledNOIDs = []uint64{9}
		case "operator":
			delete(options.Operators, 10)
		case "predecessor":
			policy := options.Policy
			options.ReplayPolicy = &policy
		}
		reads := 0
		for id, operator := range options.Operators {
			operator.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
				reads++
				return nil, errors.New("unexpected proof read")
			}
			options.Operators[id] = operator
		}
		if result, err := ReplayOwnerRecycleMeasurement(t.Context(), fixture.authority, encoded, options); result != nil || err == nil || reads != 0 {
			t.Fatalf("%s replay authority escaped admission: reads=%d err=%v", fault, reads, err)
		}
	}
}

// Neither changed proposed values nor a genuine old result can replace actual
// proof replay. The declared row is checked after a complete new reconstruction.
func TestOwnerRecycleMeasurementRejectsChangedRowAndMissingProof(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	encoded, _ := fixture.seal(t)
	var capsule OwnerRecycleMeasurement
	if err := json.Unmarshal(encoded, &capsule); err != nil {
		t.Fatal(err)
	}
	capsule.Row.Values[0]++
	changed, _ := json.Marshal(capsule)
	if result, err := ReplayOwnerRecycleMeasurement(t.Context(), fixture.authority, append(changed, '\n'), fixture.provider.options(t)); result != nil || err == nil || !strings.Contains(err.Error(), "declared row differs") {
		t.Fatalf("changed quantization accepted: %v", err)
	}
	options := fixture.provider.options(t)
	operator := options.Operators[9]
	operator.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
		return nil, errors.New("synthetic missing original proof")
	}
	options.Operators[9] = operator
	if result, err := ReplayOwnerRecycleMeasurement(t.Context(), fixture.authority, encoded, options); result != nil || err == nil || !strings.Contains(err.Error(), "synthetic missing original proof") {
		t.Fatalf("old capsule substituted for missing proof: %v", err)
	}
}

// Candidate readiness and provider-only rows are never interpreted as valid
// successor intents, even when all their original provider proofs are valid.
func TestOwnerRecycleDecisionIntentRejectsReadyAndParentRow(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	encoded, intent := fixture.seal(t)
	for _, fault := range []string{"ready", "parent-row", "census-hash", "snapshot"} {
		raw, _ := json.Marshal(intent)
		var changed OwnerRecycleDecisionIntent
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "ready":
			changed.ActivationReady = true
		case "parent-row":
			changed.Row.Uids = slices.Clone(fixture.provider.want.UIDs)
			changed.Row.Scores, _ = rationalJSON(fixture.provider.want.Scores)
		case "census-hash":
			changed.CensusHash = ReleaseMeasurementContentHash([]byte("different"))
		case "snapshot":
			changed.Decision.NativeSnapshotBlock++
		}
		if err := VerifyOwnerRecycleDecisionIntent(t.Context(), fixture.authority, encoded, fixture.provider.options(t), &changed); err == nil {
			t.Fatalf("%s intent accepted", fault)
		}
	}
}

// A real closing canonical read catches retargets; no partially authenticated
// decision owner may escape an invalid first epoch, chain or native ancestor.
func TestOwnerRecycleMeasurementAuthorityRejectsChangedDecision(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	for _, fault := range []string{"testnet", "epoch", "fork", "canonical", "burn"} {
		admission := fixture.admission
		expected := fixture.authority.expected
		originalMode := admission.storage[admission.keys["mode"]]
		checks := 0
		switch fault {
		case "testnet":
			expected.ChainID = 945
		case "epoch":
			expected.SubnetEpoch++
		case "fork":
			admission.canonical = types.Hash(recycleTestId(1300))
		case "canonical":
			admission.before = func(_ context.Context, method string, args []any) error {
				if method == "chain_getBlockHash" && len(args) == 1 && args[0] == uint64(100) {
					checks++
					if checks == 2 {
						admission.canonical = types.Hash(recycleTestId(1300))
					}
				}
				return nil
			}
		case "burn":
			admission.storage[admission.keys["mode"]] = "0x00"
		}
		got, err := ObserveOwnerRecycleMeasurementAuthority(t.Context(), admission.cfg, admission.chain, expected)
		admission.before = nil
		admission.canonical = types.Hash{}
		admission.storage[admission.keys["mode"]] = originalMode
		if got != nil || err == nil || fault == "canonical" && checks != 2 {
			t.Fatalf("%s admitted partial authority: %v", fault, err)
		}
	}
}

// Input ownership is tested at the actual synchronous proof callback, without
// timing or a forged successful verifier result.
func TestOwnerRecycleMeasurementOwnsInputAndIntentBeforeReplay(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	originalSigner := fixture.admission.cfg.OwnerRecycleApproval.Signer
	fixture.admission.before = func(context.Context, string, []any) error {
		fixture.admission.cfg.OwnerRecycleApproval.Signer = releaseHex32(recycleTestId(1700))
		return nil
	}
	authority, err := ObserveOwnerRecycleMeasurementAuthority(t.Context(), fixture.admission.cfg, fixture.admission.chain, fixture.authority.expected)
	fixture.admission.before = nil
	fixture.admission.cfg.OwnerRecycleApproval.Signer = originalSigner
	if err != nil || authority == nil || !bytes.Equal(authority.approval, fixture.authority.approval) {
		t.Fatalf("RPC callback replaced the detached configured approver: %v", err)
	}
	fixture.authority = authority
	original := bytes.Clone(fixture.encoded)
	options := fixture.provider.options(t)
	operator := options.Operators[9]
	read := operator.Measurement.Replay.ReadMetadata
	operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		fixture.encoded[0] ^= 1
		return read(ctx, hash, size)
	}
	options.Operators[9] = operator
	encoded, intent, err := SealOwnerRecycleMeasurement(t.Context(), fixture.authority, fixture.encoded, options)
	if err != nil || intent.ProviderMeasurementHash != ReleaseMeasurementContentHash(original) {
		t.Fatalf("caller mutation replaced replayed source bytes: %v", err)
	}
	fixture.encoded = original
	intent.ActivationReady = true
	options = fixture.provider.options(t)
	operator = options.Operators[9]
	read = operator.Measurement.Replay.ReadMetadata
	operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		intent.ActivationReady = false
		return read(ctx, hash, size)
	}
	options.Operators[9] = operator
	if err := VerifyOwnerRecycleDecisionIntent(t.Context(), fixture.authority, encoded, options, intent); err == nil {
		t.Fatal("proof callback laundered an initially forged ready intent")
	}
}

// Cancellation and capacity refusals publish no capsule or partial intent.
func TestOwnerRecycleMeasurementBoundsAndCancellationAreAtomic(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	for _, fault := range []string{"empty-owner", "short-bound", "cancel"} {
		authority := fixture.authority
		options := fixture.provider.options(t)
		ctx, cancel := context.WithCancel(t.Context())
		switch fault {
		case "empty-owner":
			authority = &OwnerRecycleMeasurementAuthority{}
		case "short-bound":
			options.MaxArtifactBytes = uint64(len(fixture.encoded))
		case "cancel":
			operator := options.Operators[9]
			read := operator.Measurement.Replay.ReadMetadata
			operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
				cancel()
				return read(ctx, hash, size)
			}
			options.Operators[9] = operator
		}
		encoded, intent, err := SealOwnerRecycleMeasurement(ctx, authority, fixture.encoded, options)
		cancel()
		if encoded != nil || intent != nil || err == nil {
			t.Fatalf("%s returned partial evidence: %v", fault, err)
		}
	}
	if _, err := ObserveOwnerRecycleAdmissionAt(t.Context(), fixture.admission.cfg, fixture.admission.chain, [32]byte{}); err == nil {
		t.Fatal("historical census inferred an omitted block hash")
	}
}

// Same-hash pool/census disagreement is refused after genuine proof replay;
// approved owner identities cannot be substituted as measured providers.
func TestOwnerRecycleMeasurementRejectsOwnerProviderCollision(t *testing.T) {
	fixture := newRecycleMeasurementFixture(t)
	artifact := fixture.provider.artifact
	for _, fault := range []string{"stale-hotkey", "owner-collision"} {
		original := artifact.Pools[0]
		if fault == "stale-hotkey" {
			artifact.Pools[0].PoolHotkey = releaseHex32(recycleTestId(1400))
		} else {
			artifact.Pools[0].UID, artifact.Pools[0].PoolHotkey = 2, releaseHex32(recycleTestId(2))
		}
		providerBytes, _, err := SealReleaseMeasurementArtifactV2(t.Context(), artifact, fixture.provider.options(t))
		if err != nil {
			t.Fatalf("%s must be a genuine parent measurement control: %v", fault, err)
		}
		if raw, intent, err := SealOwnerRecycleMeasurement(t.Context(), fixture.authority, providerBytes, fixture.provider.options(t)); err == nil || raw != nil || intent != nil || !strings.Contains(err.Error(), "differs from the native census or overlaps an owner") {
			t.Fatalf("%s escaped measured census binding: %v", fault, err)
		}
		artifact.Pools[0] = original
	}
}
