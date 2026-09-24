//go:build linux || darwin

// A real historical signature, atomic native receipt, and empty successor
// intent store reproduce the generation change that lost native role continuity.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/stabi"
	"golang.org/x/crypto/blake2b"
)

// Source custody is independent of economic acceptance. This signed artifact
// deliberately uses an earlier policy and is never imported as current scoring.
type sourceRolePredecessorTestFixtureV2 struct {
	historical *releaseHistoricalSourceTestFixture
	receipt    *releaseHistoricalReconcileReceipt
	hotkey     *crv4.Keypair
	previous   string
	encoded    []byte
}

// All native readers use reviewed metadata and real encoded event/slot bytes;
// no callback grants a verification result or modifies the fresh intent store.
func newSourceRolePredecessorTestFixtureV2(t *testing.T) *sourceRolePredecessorTestFixtureV2 {
	t.Helper()
	f := newReleaseHistoricalSourceTestFixture(t)
	key, err := crv4.KeypairFromSeed([32]byte{0x77})
	if err != nil {
		t.Fatal(err)
	}
	artifact := f.artifact
	artifact.DeploymentID, artifact.ValidatorID, artifact.ChainID, artifact.Netuid = "source-role-test", 2, 945, 521
	artifact.GenesisHash = f.native.genesis.Hex()
	artifact.Coordinator, artifact.SettlementVault = common.Address{0x21}.Hex(), common.Address{0x22}.Hex()
	artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash = 90, common.Hash{0x23}.Hex()
	artifact.PolicyHash, artifact.SettlementEpoch = common.Hash{0x24}.Hex(), 1
	f.measurement, err = canonicalReleaseMeasurementBytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &f.config
	cfg.DeploymentID, cfg.ValidatorID, cfg.ChainID, cfg.GenesisHash = artifact.DeploymentID, artifact.ValidatorID, artifact.ChainID, artifact.GenesisHash
	cfg.Coordinator, cfg.SettlementVault, cfg.PolicyHash = artifact.Coordinator, artifact.SettlementVault, common.Hash{0x25}.Hex()
	cfg.EvidenceV2.Bounds.MaxHistoryBytes = 1024 * 1024
	cfg.StateDir = newAttemptSettlementRuntimeV2TestStateDir(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(f.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	prepared := f.intent.Prepared
	prepared.SourceCommitment.Hash = releaseHex32(releaseNativeSourceHashV2(f.measurement))
	ciphertext, err := hexutil.Decode(prepared.CiphertextHex)
	if err != nil {
		t.Fatal(err)
	}
	signer := &crv4.Chain{Meta: metadata, GenesisHash: f.native.genesis, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1}}
	anchor, err := signer.NewSetFleetCommitmentCall(cfg.Netuid, releaseNativeSourceHashV2(f.measurement))
	if err != nil {
		t.Fatal(err)
	}
	commit, err := types.NewCall(metadata, "SubtensorModule.commit_timelocked_weights", types.U16(cfg.Netuid), types.Bytes(ciphertext), types.U64(prepared.RevealRound), types.U16(prepared.CommitRevealVersion))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := types.NewCall(metadata, "Utility.batch_all", []types.Call{anchor, commit})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.NewSignedExtrinsic(key, batch, prepared.AccountNonce)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.Encode(signed)
	if err != nil {
		t.Fatal(err)
	}
	prepared.ExtrinsicHex, prepared.ExtrinsicHash = hexutil.Encode(raw), types.Hash(blake2b.Sum256(raw)).Hex()
	if err := signer.ValidatePreparedSource(prepared); err != nil {
		t.Fatal(err)
	}
	digest := blake2b.Sum256(ciphertext)
	events := releaseHistoricalReconcileEvents(t, metadata, []releaseHistoricalReconcileEvent{
		{pallet: "Commitments", name: "Commitment", fields: []any{types.U16(prepared.Netuid), key.PublicKey()}},
		{pallet: "Utility", name: "ItemCompleted"},
		{pallet: "SubtensorModule", name: "TimelockedWeightsCommitted", fields: []any{key.PublicKey(), types.U16(prepared.Netuid), digest, types.U64(prepared.RevealRound)}},
		{pallet: "Utility", name: "ItemCompleted"},
		{pallet: "Utility", name: "BatchCompleted"},
		{pallet: "System", name: "ExtrinsicSuccess", fields: []any{releaseHistoricalReconcileDispatchInfo()}},
	})
	receipt := installReleaseHistoricalReconcileReceipt(t, f.native, f.metadataHex, prepared, events)
	signedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	envelope := newReleaseMeasurementEnvelope(artifact, f.measurement, key.PublicKey(), f.intent.SelfUID, [32]byte(common.HexToHash(prepared.ExtrinsicHash)), signedAt, ReleaseMeasurementEnvelopeSchemaV2)
	signingHash, err := releaseMeasurementEnvelopeSigningDigestWithDomain(envelope, ReleaseMeasurementEnvelopeSigningDomainV2)
	if err != nil {
		t.Fatal(err)
	}
	envelope.SigningHash = "sha256:" + strings.TrimPrefix(hexutil.Encode(signingHash[:]), "0x")
	signature, err := key.Sign(signingHash[:])
	if err != nil {
		t.Fatal(err)
	}
	envelope.Signature = hexutil.Encode(signature)
	envelopeBytes, err := canonicalReleaseMeasurementEnvelopeBytes(envelope)
	if err != nil {
		t.Fatal(err)
	}
	previous := newAttemptSettlementRuntimeV2TestStateDir(t)
	intent := f.intent
	intent.Schema, intent.ValidatorID, intent.Netuid, intent.PolicyHash, intent.Status = steeringIntentSchema, cfg.ValidatorID, cfg.Netuid, artifact.PolicyHash, "finalized"
	intent.CreatedAt, intent.UpdatedAt = signedAt.Format(time.RFC3339Nano), signedAt.Format(time.RFC3339Nano)
	intent.ExtrinsicHash, intent.FinalizedBlock, intent.FinalizedBlockHash = prepared.ExtrinsicHash, receipt.number, receipt.hash.Hex()
	intent.RevealBlock, intent.Values = prepared.RevealBlock, append([]uint16(nil), prepared.Values...)
	intent.MeasurementArtifactHash = ReleaseMeasurementContentHash(f.measurement)
	intent.MeasurementArtifactPath, intent.MeasurementArtifactSize, err = persistReleaseMeasurementArtifact(previous, f.measurement, intent.MeasurementArtifactHash)
	if err != nil {
		t.Fatal(err)
	}
	intent.MeasurementEnvelopeHash = ReleaseMeasurementEnvelopeContentHash(envelopeBytes)
	intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeSize, err = persistReleaseMeasurementEnvelope(previous, envelopeBytes, intent.MeasurementEnvelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	fileBytes, err := json.MarshalIndent(steeringIntentFile{Schema: steeringIntentSchema, Current: intent}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "steering-intents.json"), append(fileBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	encoded, err := PrepareReleaseSourceRolePredecessorV2(t.Context(), cfg, previous)
	if err != nil {
		t.Fatalf("prepare finalized source predecessor: %v", err)
	}
	ref, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(cfg.StateDir, "source-predecessor.json"), encoded, ReleaseSourceRolePredecessorV2MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SourceRolePredecessorV2 = &ref
	return &sourceRolePredecessorTestFixtureV2{historical: f, receipt: receipt, hotkey: key, previous: previous, encoded: encoded}
}

// A different policy does not erase the old source's actual hotkey and native
// inclusion. Authentication leaves both source history and fresh state intact.
func TestReleaseSourceRolePredecessorV2AuthenticatesOriginalFinality(t *testing.T) {
	f := newSourceRolePredecessorTestFixtureV2(t)
	cfg, native := &f.historical.config, f.historical.native.chain
	before, err := os.ReadFile(filepath.Join(f.previous, "steering-intents.json"))
	if err != nil {
		t.Fatal(err)
	}
	metadata, runtime := native.Meta, native.Runtime
	witness, err := authenticateReleaseSourceRolePredecessorV2(t.Context(), cfg, native, f.hotkey.PublicKey())
	if err != nil || witness == nil {
		t.Fatalf("retained source role failed: %v", err)
	}
	observed := &crv4.FinalizedCommitment{Hash: [32]byte(common.HexToHash(f.historical.intent.Prepared.SourceCommitment.Hash)), CommitmentBlock: f.receipt.number}
	if !witness.matches(cfg.Netuid, f.hotkey.PublicKey(), observed) {
		t.Fatal("authenticated predecessor cannot recognize its occupied native slot")
	}
	if f.receipt.eventReads == 0 || f.receipt.commitmentReads == 0 || f.receipt.blocks == 0 || f.receipt.runtimeReads == 0 || f.receipt.submissions != 0 || f.receipt.subscriptions != 0 || native.Meta != metadata || native.Runtime != runtime {
		t.Fatalf("proof skipped finality or changed native owner: %+v", f.receipt)
	}
	after, err := os.ReadFile(filepath.Join(f.previous, "steering-intents.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("role authentication changed prior intent history: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, "steering-intents.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("role authentication imported old intent/EMA state: %v", err)
	}
	for _, fault := range []string{"hash", "block", "hotkey", "netuid"} {
		changed, hotkey, netuid := *observed, f.hotkey.PublicKey(), cfg.Netuid
		switch fault {
		case "hash":
			changed.Hash[0] ^= 1
		case "block":
			changed.CommitmentBlock++
		case "hotkey":
			hotkey[0] ^= 1
		case "netuid":
			netuid++
		}
		if witness.matches(netuid, hotkey, &changed) {
			t.Fatalf("role witness accepted changed %s", fault)
		}
	}
}

// The actual role gate used to reject this source solely because the successor
// store was empty. A retained proof fixes that path while mirrors and foreign
// writes remain rejected before any preparation or current intent can be made.
func TestReleaseSourceRolePredecessorV2EmptyGenerationContinuesExactRole(t *testing.T) {
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
	coordinator := stabi.NewSTCoordinator()
	snapshot := &ReleaseSnapshot{BlockNumber: 90, BlockHash: [32]byte(common.Hash{0x23})}
	var mirrored atomic.Bool
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
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &envelope) != nil || json.Unmarshal(call.Params[1], &selector) != nil || envelope.Input != hexutil.Encode(coordinator.PackMirroredCommitments(f.hotkey.PublicKey())) || selector.BlockHash != common.Hash(snapshot.BlockHash) || !selector.RequireCanonical {
				t.Error("role mirror read changed its actor or canonical snapshot")
				http.Error(writer, "invalid role request", http.StatusBadRequest)
				return
			}
			raw := make([]byte, 96)
			if mirrored.Load() {
				raw[0] = 1
			}
			result = hexutil.Encode(raw)
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
	store, err := newReleaseIntentStoreV2(&releaseRuntimeV2{ctx: t.Context(), cfg: *cfg, history: &releaseEvidenceV2StartupHistory{}})
	if err != nil {
		t.Fatal(err)
	}
	steerer := &ReleaseSteerer{cfg: cfg, native: &native, chain: &ChainClient{client: client, coordinator: coordinator, release: true}, hotkey: f.hotkey, intents: store, sourceRolePredecessorV2: witness}
	for range 2 {
		if err := steerer.checkSourceRoleV2(t.Context(), snapshot, f.receipt.hash, nil); err != nil {
			t.Fatalf("fresh generation rejected the exact retained native role: %v", err)
		}
	}
	steerer.sourceRolePredecessorV2 = nil
	if err := steerer.checkSourceRoleV2(t.Context(), snapshot, f.receipt.hash, nil); err == nil || !strings.Contains(err.Error(), "unretained write") {
		t.Fatalf("occupied slot acquired authority without a predecessor proof: %v", err)
	}
	steerer.sourceRolePredecessorV2 = witness
	mirrored.Store(true)
	if err := steerer.checkSourceRoleV2(t.Context(), snapshot, f.receipt.hash, nil); err == nil || !strings.Contains(err.Error(), "fleet commitment mirror") {
		t.Fatalf("source proof bypassed fleet-role separation: %v", err)
	}
	mirrored.Store(false)
	f.receipt.registration[len(f.receipt.registration)-1] ^= 1
	if err := steerer.checkSourceRoleV2(t.Context(), snapshot, f.receipt.hash, nil); err == nil || !strings.Contains(err.Error(), "unretained write") {
		t.Fatalf("source proof adopted an unrelated occupied slot: %v", err)
	}
	if current, err := store.currentV2(t.Context()); err != nil || current != nil {
		t.Fatalf("role proof changed fresh current intent: %+v %v", current, err)
	}
}

// Plausible local signatures are insufficient when the original chain body,
// atomic events, slot write, or expected hotkey disagrees.
func TestReleaseSourceRolePredecessorV2RejectsNativeProofFailure(t *testing.T) {
	for _, fault := range []string{"slot", "events", "hotkey", "cancel"} {
		f := newSourceRolePredecessorTestFixtureV2(t)
		hotkey := f.hotkey.PublicKey()
		ctx := t.Context()
		switch fault {
		case "slot":
			f.receipt.registration[len(f.receipt.registration)-1] ^= 1
		case "events":
			f.receipt.events = []byte{0}
		case "hotkey":
			hotkey[0] ^= 1
		case "cancel":
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			ctx = canceled
		}
		if witness, err := authenticateReleaseSourceRolePredecessorV2(ctx, &f.historical.config, f.historical.native.chain, hotkey); err == nil || witness != nil {
			t.Fatalf("failed %s source acquired role authority: %v", fault, err)
		}
	}
}

// Rollout reads a later current runtime while historical receipt verification
// stays at the original runtime. Neither a missing proof nor a stale slot can
// gain authority merely because the descriptor's signatures are valid.
func TestReleaseSourceRolePredecessorV2RolloutChecksCurrentSlot(t *testing.T) {
	f := newSourceRolePredecessorTestFixtureV2(t)
	cfg, native := &f.historical.config, f.historical.native.chain
	currentCfg := runtime467ValidatorTestConfig()
	cfg.RuntimeSpec, cfg.TransactionVersion, cfg.StateVersion = currentCfg.RuntimeSpec, currentCfg.TransactionVersion, currentCfg.StateVersion
	cfg.RuntimeCodeHash, cfg.RuntimeMetadataHash = currentCfg.RuntimeCodeHash, currentCfg.RuntimeMetadataHash
	metadata, metadataHex := provisionalValidatorMetadataTest(t, "../crv4/runtime-profile-v1.scale.gz.base64", currentCfg.RuntimeMetadataHash)
	head := types.Hash{0x45}
	netuid := binary.LittleEndian.AppendUint16(nil, cfg.Netuid)
	hotkey := f.hotkey.PublicKey()
	commitmentKey, err := types.CreateStorageKey(metadata, "Commitments", "CommitmentOf", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(metadata, "Commitments", "LastCommitment", netuid, hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	currentRegistration := bytes.Clone(f.receipt.registration)
	absent := false
	prior := native.API.Client
	native.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch method {
		case "chain_getFinalizedHead":
			return setReleaseHistoricalTestResult(result, head.Hex())
		case "chain_getBlockHash":
			if len(args) == 1 && fmt.Sprint(args[0]) == fmt.Sprint(f.receipt.number+1) {
				return setReleaseHistoricalTestResult(result, head.Hex())
			}
		case "chain_getHeader":
			if len(args) == 1 && args[0] == head.Hex() {
				return setReleaseHistoricalTestResult(result, types.Header{Number: types.BlockNumber(f.receipt.number + 1)})
			}
		case "state_getRuntimeVersion":
			if len(args) == 1 && args[0] == head.Hex() {
				return setReleaseHistoricalTestResult(result, crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion})
			}
		case "state_getStorageHash":
			if len(args) == 2 && args[0] == "0x3a636f6465" && args[1] == head.Hex() {
				return setReleaseHistoricalTestResult(result, cfg.RuntimeCodeHash)
			}
		case "state_getMetadata":
			if len(args) == 1 && args[0] == head.Hex() {
				return setReleaseHistoricalTestResult(result, metadataHex)
			}
		case "state_getStorage":
			if len(args) == 2 && args[1] == head.Hex() && (args[0] == commitmentKey.Hex() || args[0] == lastKey.Hex()) {
				if absent {
					return setReleaseHistoricalTestResult(result, nil)
				}
				if args[0] == commitmentKey.Hex() {
					return setReleaseHistoricalTestResult(result, hexutil.Encode(currentRegistration))
				}
				return setReleaseHistoricalTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint32(nil, uint32(f.receipt.number))))
			}
		}
		return prior.CallContext(ctx, result, method, args...)
	}}
	if err := VerifyReleaseSourceRolePredecessorV2(t.Context(), cfg, native, hotkey); err != nil {
		t.Fatalf("historical source/current slot handoff failed: %v", err)
	}
	currentRegistration[len(currentRegistration)-1] ^= 1
	if err := VerifyReleaseSourceRolePredecessorV2(t.Context(), cfg, native, hotkey); err == nil || !strings.Contains(err.Error(), "current native slot") {
		t.Fatalf("stale predecessor was selected for rollout: %v", err)
	}
	cfg.SourceRolePredecessorV2 = nil
	if err := VerifyReleaseSourceRolePredecessorV2(t.Context(), cfg, native, hotkey); err == nil || !strings.Contains(err.Error(), "current native slot") {
		t.Fatalf("missing predecessor concealed an occupied slot: %v", err)
	}
	absent = true
	if err := VerifyReleaseSourceRolePredecessorV2(t.Context(), cfg, native, hotkey); err != nil {
		t.Fatalf("genuinely unused hotkey required an invented predecessor: %v", err)
	}
}

// Initial absence is optional. Partial or empty files cannot silently discard
// a proof, and an unresolved transaction is never relabeled as a predecessor.
func TestReleaseSourceRolePredecessorV2PreparationPreservesMissingAndMalformedState(t *testing.T) {
	f := newSourceRolePredecessorTestFixtureV2(t)
	cfg := &f.historical.config
	missing := newAttemptSettlementRuntimeV2TestStateDir(t)
	if encoded, err := PrepareReleaseSourceRolePredecessorV2(t.Context(), cfg, missing); err != nil || encoded != nil {
		t.Fatalf("clean absence failed: %v", err)
	}
	empty, err := json.MarshalIndent(steeringIntentFile{Schema: steeringIntentSchema}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missing, "steering-intents.json"), append(empty, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if encoded, err := PrepareReleaseSourceRolePredecessorV2(t.Context(), cfg, missing); err != nil || encoded != nil {
		t.Fatalf("canonical empty history required an invented predecessor: %v", err)
	}
	for _, encoded := range [][]byte{nil, []byte("{"), []byte("{}\n")} {
		if err := os.WriteFile(filepath.Join(missing, "steering-intents.json"), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareReleaseSourceRolePredecessorV2(t.Context(), cfg, missing); err == nil {
			t.Fatal("partial source history became absence")
		}
	}
	proof, err := DecodeReleaseSourceRolePredecessorV2(t.Context(), cfg, f.encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"validator", "genesis", "contract", "reference", "pending", "signature"} {
		changedCfg, changed := *cfg, *proof
		switch fault {
		case "validator":
			changedCfg.ValidatorID++
		case "genesis":
			changedCfg.GenesisHash = common.Hash{0x38}.Hex()
		case "contract":
			changedCfg.Coordinator = common.Address{0x39}.Hex()
		case "reference":
			changed.Measurement.Bytes++
		case "pending":
			changed.Intent.Status = "pending"
		case "signature":
			changed.Envelope.SHA256 = common.Hash{0x3a}.Hex()
		}
		encoded, err := json.MarshalIndent(changed, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeReleaseSourceRolePredecessorV2(t.Context(), &changedCfg, append(encoded, '\n')); err == nil {
			t.Fatalf("changed predecessor %s was accepted", fault)
		}
	}
}
