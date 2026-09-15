//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/block"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/crv4"
	"golang.org/x/crypto/blake2b"
)

// Event indices come from the reviewed original metadata. The SDK encodes
// actual event fields, phases and topics; the production parser verifies them.
type releaseHistoricalReconcileEvent struct {
	pallet string
	name   string
	fields []any
}

func releaseHistoricalReconcileEvents(t *testing.T, metadata *types.Metadata, events []releaseHistoricalReconcileEvent) []byte {
	t.Helper()
	encoded, err := codec.Encode(types.NewUCompactFromUInt(uint64(len(events))))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		found := false
		for _, pallet := range metadata.AsMetadataV14.Pallets {
			if string(pallet.Name) != event.pallet || !pallet.HasEvents {
				continue
			}
			definition := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()]
			if definition == nil || !definition.Def.IsVariant {
				t.Fatalf("original %s event metadata is absent", event.pallet)
			}
			for _, variant := range definition.Def.Variant.Variants {
				if string(variant.Name) != event.name {
					continue
				}
				if len(variant.Fields) != len(event.fields) {
					t.Fatalf("original %s.%s field census differs", event.pallet, event.name)
				}
				phase, err := codec.Encode(types.Phase{IsApplyExtrinsic: true, AsApplyExtrinsic: 0})
				if err != nil {
					t.Fatal(err)
				}
				encoded = append(encoded, phase...)
				encoded = append(encoded, byte(pallet.Index), byte(variant.Index))
				for _, field := range event.fields {
					wire, err := codec.Encode(field)
					if err != nil {
						t.Fatal(err)
					}
					encoded = append(encoded, wire...)
				}
				topics, err := codec.Encode([]types.Hash{})
				if err != nil {
					t.Fatal(err)
				}
				encoded = append(encoded, topics...)
				found = true
				break
			}
			break
		}
		if !found {
			t.Fatalf("original %s.%s event index is absent", event.pallet, event.name)
		}
	}
	return encoded
}

func releaseHistoricalReconcileDispatchInfo() types.DispatchInfo {
	return types.DispatchInfo{Weight: types.NewWeight(types.NewUCompactFromUInt(0), types.NewUCompactFromUInt(0)), Class: types.DispatchClass{IsNormal: true}, PaysFee: types.Pays{IsYes: true}}
}

type releaseHistoricalReconcileReceipt struct {
	hash            types.Hash
	number          uint64
	events          []byte
	registration    []byte
	blocks          int
	eventReads      int
	commitmentReads int
	runtimeReads    int
	submissions     int
	subscriptions   int
}

type releaseHistoricalReconcileClient struct {
	*validatorRuntimeIdentityTestClient
	receipt *releaseHistoricalReconcileReceipt
}

func (self *releaseHistoricalReconcileClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	self.receipt.subscriptions++
	return nil, errors.New("historical receipt recovery attempted subscription")
}

// Preparation and inclusion are separate canonical blocks. Every receipt
// runtime, body, event and commitment read must retain the inclusion hash.
func installReleaseHistoricalReconcileReceipt(t *testing.T, native *releaseNativeValidatorTestFixture, metadataHex string, prepared *crv4.PreparedSubmission, events []byte) *releaseHistoricalReconcileReceipt {
	t.Helper()
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	eventsKey, err := types.CreateStorageKey(metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	result := &releaseHistoricalReconcileReceipt{hash: types.Hash{0x43}, number: native.blockNumber + 1, events: bytes.Clone(events)}
	var commitmentKey, lastKey types.StorageKey
	if prepared.SourceCommitment != nil {
		hotkey, err := hexutil.Decode(prepared.HotkeyHex)
		if err != nil {
			t.Fatal(err)
		}
		commitmentKey, err = types.CreateStorageKey(metadata, "Commitments", "CommitmentOf", binary.LittleEndian.AppendUint16(nil, prepared.Netuid), hotkey)
		if err != nil {
			t.Fatal(err)
		}
		lastKey, err = types.CreateStorageKey(metadata, "Commitments", "LastCommitment", binary.LittleEndian.AppendUint16(nil, prepared.Netuid), hotkey)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := types.NewHashFromHexString(prepared.SourceCommitment.Hash)
		if err != nil {
			t.Fatal(err)
		}
		info, err := crv4.EncodeFleetCommitmentInfo([32]byte(hash))
		if err != nil {
			t.Fatal(err)
		}
		result.registration = binary.LittleEndian.AppendUint64(nil, 1)
		result.registration = binary.LittleEndian.AppendUint32(result.registration, uint32(result.number))
		result.registration = append(result.registration, info...)
	}
	original := native.chain.API.Client
	assign := func(target any, value any) error {
		wire, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return json.Unmarshal(wire, target)
	}
	native.chain.API.Client = &releaseHistoricalReconcileClient{receipt: result, validatorRuntimeIdentityTestClient: &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasPrefix(method, "author_") {
			result.submissions++
			return errors.New("historical receipt recovery attempted submission")
		}
		switch method {
		case "chain_getFinalizedHead":
			if len(args) != 0 {
				return errors.New("finalized head request changed shape")
			}
			return assign(target, result.hash.Hex())
		case "chain_getHeader":
			if len(args) == 1 && args[0] == result.hash.Hex() {
				return assign(target, types.Header{Number: types.BlockNumber(result.number)})
			}
		case "chain_getBlockHash":
			if len(args) == 1 && args[0] == result.number {
				return assign(target, result.hash.Hex())
			}
		case "chain_getBlock":
			if len(args) != 1 {
				return errors.New("receipt body request changed shape")
			}
			result.blocks++
			if args[0] == native.block.Hex() {
				return assign(target, block.SignedBlock{Block: block.Block{Header: types.Header{Number: types.BlockNumber(native.blockNumber)}, Extrinsics: []string{}}})
			}
			if args[0] == result.hash.Hex() {
				return assign(target, block.SignedBlock{Block: block.Block{Header: types.Header{Number: types.BlockNumber(result.number)}, Extrinsics: []string{prepared.ExtrinsicHex}}})
			}
			return errors.New("receipt body escaped its canonical source blocks")
		case "state_getRuntimeVersion":
			if len(args) == 1 && args[0] == result.hash.Hex() {
				result.runtimeReads++
				return assign(target, native.expected.Version)
			}
		case "state_getStorageHash":
			if len(args) == 2 && args[0] == "0x3a636f6465" && args[1] == result.hash.Hex() {
				return assign(target, native.expected.CodeHash)
			}
		case "state_getMetadata":
			if len(args) == 1 && args[0] == result.hash.Hex() {
				return assign(target, metadataHex)
			}
		case "state_getStorage":
			if len(args) == 2 && args[0] == eventsKey.Hex() {
				if args[1] != result.hash.Hex() {
					return errors.New("receipt events used preparation or moving block")
				}
				result.eventReads++
				return assign(target, hexutil.Encode(result.events))
			}
			if prepared.SourceCommitment != nil && len(args) == 2 && (args[0] == commitmentKey.Hex() || args[0] == lastKey.Hex()) {
				if args[1] != result.hash.Hex() {
					return errors.New("source commitment used preparation or moving block")
				}
				result.commitmentReads++
				if args[0] == commitmentKey.Hex() {
					return assign(target, hexutil.Encode(result.registration))
				}
				return assign(target, hexutil.Encode(binary.LittleEndian.AppendUint32(nil, uint32(result.number))))
			}
		}
		return original.CallContext(ctx, target, method, args...)
	}}}
	return result
}

// The existing real signed measurement/envelope and durable legacy store are
// retained, while the SDK signs its actual original455 encrypted-weight call.
func releaseHistoricalReconcileLegacyPending(t *testing.T) (*ReleaseSteerer, *SteeringIntent, *releaseHistoricalReconcileReceipt) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	intent := testSteeringIntent(t, stateDir, 3, "")
	hotkey := testIntentHotkey(t)
	native := newReleaseNativeValidatorTestFixture(t, hotkey.PublicKey())
	metadataHex := installReleaseHistoricalTestNative(t, native)
	metadata, _, err := crv4.DecodeRuntimeMetadata(*metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := hexutil.Decode(intent.Prepared.CiphertextHex)
	if err != nil {
		t.Fatal(err)
	}
	call, err := types.NewCall(metadata, "SubtensorModule.commit_timelocked_weights", types.U16(intent.Netuid), types.Bytes(ciphertext), types.U64(intent.Prepared.RevealRound), types.U16(intent.Prepared.CommitRevealVersion))
	if err != nil {
		t.Fatal(err)
	}
	signer := &crv4.Chain{Meta: metadata, GenesisHash: native.genesis, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1}}
	signed, err := signer.NewSignedExtrinsic(hotkey, call, intent.Prepared.AccountNonce)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := codec.Encode(signed)
	if err != nil {
		t.Fatal(err)
	}
	intent.Prepared.ExtrinsicHex = hexutil.Encode(wire)
	intent.Prepared.ExtrinsicHash = types.Hash(blake2b.Sum256(wire)).Hex()
	intent.Prepared.PreparedAtBlock, intent.Prepared.PreparedAtBlockHash = native.blockNumber, native.block.Hex()
	measurement, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(intent.MeasurementArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	envelope, envelopeHash, _, err := SealReleaseMeasurementEnvelope(measurement, intent.SelfUID, hotkey, intent.Prepared.ExtrinsicHash, time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeSize, err = persistReleaseMeasurementEnvelope(stateDir, envelope, envelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	intent.MeasurementEnvelopeHash = envelopeHash
	pending, err := store.Begin(intent)
	if err != nil {
		t.Fatalf("original signed legacy pending store: %v", err)
	}
	events := releaseHistoricalReconcileEvents(t, metadata, []releaseHistoricalReconcileEvent{{pallet: "System", name: "ExtrinsicSuccess", fields: []any{releaseHistoricalReconcileDispatchInfo()}}})
	receipt := installReleaseHistoricalReconcileReceipt(t, native, *metadataHex, pending.Prepared, events)
	cfg := runtime458ValidatorTestConfig()
	cfg.Netuid = intent.Netuid
	return &ReleaseSteerer{cfg: &cfg, native: native.chain, intents: store, hotkey: hotkey}, pending, receipt
}

func TestReleaseEvidenceV2HistoricalRuntimeLegacyReconcilePersistsExactReceipt(t *testing.T) {
	steerer, pending, receipt := releaseHistoricalReconcileLegacyPending(t)
	prepared, err := json.Marshal(pending.Prepared)
	if err != nil {
		t.Fatal(err)
	}
	metadata, runtime := steerer.native.Meta, steerer.native.Runtime
	done, err := steerer.reconcilePending(t.Context(), pending, &crv4.EpochScheduleState{SubnetEpochIndex: pending.SubnetEpoch})
	if err != nil || !done {
		t.Fatalf("original455 finalized legacy recovery: done=%t error=%v", done, err)
	}
	reopened, err := NewIntentStore(steerer.intents.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Current()
	if err != nil || current == nil || current.Status != "finalized" || current.VectorHash != pending.VectorHash || current.FinalizedBlock != receipt.number || current.FinalizedBlockHash != receipt.hash.Hex() || current.ExtrinsicHash != pending.Prepared.ExtrinsicHash {
		t.Fatalf("reopened original finalized receipt differs: %+v %v", current, err)
	}
	retained, err := json.Marshal(current.Prepared)
	if err != nil || !bytes.Equal(prepared, retained) {
		t.Fatalf("recovery changed original signed preparation: %v", err)
	}
	if receipt.runtimeReads == 0 || receipt.eventReads == 0 || receipt.submissions != 0 || receipt.subscriptions != 0 {
		t.Fatalf("legacy recovery crossed wrong boundary: %+v", receipt)
	}
	if steerer.native.Meta != metadata || steerer.native.Runtime != runtime {
		t.Fatal("legacy receipt recovery changed current signing view")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeLegacyReconcileRejectsDispatchFailure(t *testing.T) {
	steerer, pending, receipt := releaseHistoricalReconcileLegacyPending(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(releaseHistoricalTestMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	receipt.events = releaseHistoricalReconcileEvents(t, metadata, []releaseHistoricalReconcileEvent{{pallet: "System", name: "ExtrinsicFailed", fields: []any{types.DispatchError{IsOther: true}, releaseHistoricalReconcileDispatchInfo()}}})
	before, err := os.ReadFile(steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	done, err := steerer.reconcilePending(t.Context(), pending, &crv4.EpochScheduleState{SubnetEpochIndex: pending.SubnetEpoch})
	var dispatch *crv4.FinalizedDispatchError
	if done || !errors.As(err, &dispatch) || dispatch.BlockHash != receipt.hash {
		t.Fatalf("historical failure became local finality: done=%t error=%v", done, err)
	}
	after, readErr := os.ReadFile(steerer.intents.path)
	if readErr != nil || !bytes.Equal(before, after) || receipt.submissions != 0 || receipt.subscriptions != 0 {
		t.Fatalf("failed receipt changed source or submitted: %v", readErr)
	}
}

// This is complete signed native-source acceptance through the real V2
// reconcile caller, stopped by an explicit existing publication fault. It does
// not claim successful V2 measurement replay or durable V2 publication.
func TestReleaseEvidenceV2HistoricalRuntimeSignedReconcileAuthenticatesReceiptBeforePublication(t *testing.T) {
	for _, changedCommitment := range []bool{false, true} {
		fixture := newReleaseHistoricalSourceTestFixture(t)
		metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.metadataHex)
		if err != nil {
			t.Fatal(err)
		}
		prepared := fixture.intent.Prepared
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
		receipt := installReleaseHistoricalReconcileReceipt(t, fixture.native, fixture.metadataHex, prepared, events)
		if changedCommitment {
			receipt.registration[len(receipt.registration)-1] ^= 1
		}
		store := intentV2PublicationTest(t)
		publicationFault := errors.New("retained V2 publication fault after native receipt proof")
		store.v2.fault = publicationFault
		steerer := &ReleaseSteerer{cfg: &fixture.config, native: fixture.native.chain, intents: store}
		before, err := json.Marshal(prepared)
		if err != nil {
			t.Fatal(err)
		}
		liveMetadata, liveRuntime := fixture.native.chain.Meta, fixture.native.chain.Runtime
		done, err := steerer.reconcilePendingV2(t.Context(), fixture.intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1})
		if changedCommitment {
			if err == nil || errors.Is(err, publicationFault) || !strings.Contains(err.Error(), "source metadata readback") {
				t.Fatalf("changed original commitment reached publication: %v", err)
			}
		} else if !errors.Is(err, publicationFault) {
			t.Fatalf("valid original455 source did not finish receipt proof: %v", err)
		}
		if done || receipt.runtimeReads == 0 || receipt.eventReads != 2 || receipt.commitmentReads != 2 || receipt.submissions != 0 || receipt.subscriptions != 0 {
			t.Fatalf("V2 historical receipt boundary differs: changed_commitment=%t done=%t receipt=%+v error=%v", changedCommitment, done, receipt, err)
		}
		after, marshalErr := json.Marshal(prepared)
		if marshalErr != nil || !bytes.Equal(before, after) || fixture.native.chain.Meta != liveMetadata || fixture.native.chain.Runtime != liveRuntime {
			t.Fatalf("V2 receipt proof changed original source or signing view: changed_commitment=%t error=%v", changedCommitment, marshalErr)
		}
		if _, statErr := os.Stat(store.path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("publication refusal wrote intent state: changed_commitment=%t error=%v", changedCommitment, statErr)
		}
	}
}
