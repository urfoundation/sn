//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

type releaseHistoricalSourceTestFixture struct {
	native      *releaseNativeValidatorTestFixture
	config      ReleaseConfig
	artifact    *ReleaseMeasurementArtifact
	measurement []byte
	intent      *SteeringIntent
	metadataHex string
}

// The real SDK signs the exact two-call batch using the reviewed original
// metadata. This fixture models source custody, not measurement scoring.
func newReleaseHistoricalSourceTestFixture(t *testing.T) *releaseHistoricalSourceTestFixture {
	t.Helper()
	key, err := crv4.KeypairFromSeed([32]byte{0x77})
	if err != nil {
		t.Fatal(err)
	}
	native := newReleaseNativeValidatorTestFixture(t, key.PublicKey())
	metadataHex := installReleaseHistoricalTestNative(t, native)
	metadata, _, err := crv4.DecodeRuntimeMetadata(*metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	epochKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "SubnetEpochIndex", binary.LittleEndian.AppendUint16(nil, 521))
	if err != nil {
		t.Fatal(err)
	}
	original := native.chain.API.Client
	native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getStorage" && len(args) == 2 && args[0] == epochKey.Hex() {
			if args[1] != native.block.Hex() {
				return errors.New("source schedule changed original block")
			}
			return setReleaseHistoricalTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint64(nil, 1)))
		}
		return original.CallContext(ctx, result, method, args...)
	}}
	artifact := &ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, SubnetEpoch: 1, NativeSnapshotBlock: native.blockNumber, NativeSnapshotHash: native.block.Hex(), SelfUID: native.uid}
	measurement, err := canonicalReleaseMeasurementBytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := releaseNativeSourceHashV2(measurement)
	ciphertext := bytes.Repeat([]byte{0x14, 0x25, 0x36}, 96)
	cipherHash := sha256.Sum256(ciphertext)
	payload, err := (&crv4.Payload{Hotkey: key.PublicKey(), Uids: []uint16{1, 2}, Values: []uint16{32768, 65535}, VersionKey: 1}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	signer := &crv4.Chain{Meta: metadata, GenesisHash: native.genesis, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1}}
	anchor, err := signer.NewSetFleetCommitmentCall(521, sourceHash)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := types.NewCall(metadata, "SubtensorModule.commit_timelocked_weights", types.U16(521), types.Bytes(ciphertext), types.U64(8), types.U16(4))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := types.NewCall(metadata, "Utility.batch_all", []types.Call{anchor, commit})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.NewSignedExtrinsic(key, batch, 3)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.Encode(signed)
	if err != nil {
		t.Fatal(err)
	}
	digest := blake2b.Sum256(raw)
	prepared := &crv4.PreparedSubmission{Schema: crv4.PreparedSourceSubmissionSchema, Netuid: 521, HotkeyHex: hexutil.Encode(native.hotkey[:]), VersionKey: 1, CommitRevealVersion: 4, AccountNonce: 3, PreparedAtBlock: native.blockNumber, PreparedAtBlockHash: native.block.Hex(), SubnetEpoch: 1, RevealRound: 8, RevealBlock: native.blockNumber + 10, UIDs: []uint16{1, 2}, Values: []uint16{32768, 65535}, PayloadHex: hexutil.Encode(payload), CiphertextHex: hexutil.Encode(ciphertext), CiphertextSHA256: hexutil.Encode(cipherHash[:]), ExtrinsicHex: hexutil.Encode(raw), ExtrinsicHash: hexutil.Encode(digest[:]), SourceCommitment: &crv4.PreparedSourceCommitment{Hash: hexutil.Encode(sourceHash[:]), GenesisHash: native.genesis.Hex(), RuntimeSpec: 455, TransactionVersion: 1}}
	if err := signer.ValidatePreparedSource(prepared); err != nil {
		t.Fatalf("original455 SDK signed source: %v", err)
	}
	cfg := runtime461ValidatorTestConfig()
	cfg.Netuid = 521
	cfg.EvidenceV2.Bounds.MaxArtifactBytes, cfg.EvidenceV2.Bounds.MaxControlBytes, cfg.EvidenceV2.Bounds.MaxOperators = 1024*1024, 1024*1024, 1
	return &releaseHistoricalSourceTestFixture{native: native, config: cfg, artifact: artifact, measurement: measurement, metadataHex: *metadataHex,
		intent: &SteeringIntent{Status: "pending", Prepared: prepared, SubnetEpoch: 1, SelfUID: native.uid}}
}

func TestReleaseEvidenceV2HistoricalRuntimeSourceAuthenticatesOriginalSignedBatch(t *testing.T) {
	fixture := newReleaseHistoricalSourceTestFixture(t)
	metadata, runtime := fixture.native.chain.Meta, fixture.native.chain.Runtime
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), fixture.native.chain, &fixture.config, fixture.intent, fixture.artifact); err != nil {
		t.Fatalf("original455 signed source replay under460: %v", err)
	}
	if fixture.native.chain.Meta != metadata || fixture.native.chain.Runtime != runtime {
		t.Fatal("signed source replay changed current signing owner")
	}
	fixture.intent.Prepared.SourceCommitment.RuntimeSpec = 458
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), fixture.native.chain, &fixture.config, fixture.intent, fixture.artifact); err == nil {
		t.Fatal("original signature was relabeled as runtime458")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeCaptureRetainsExactOriginalMetadata(t *testing.T) {
	fixture := newReleaseHistoricalSourceTestFixture(t)
	metadata, runtime := fixture.native.chain.Meta, fixture.native.chain.Runtime
	var reads []ReleaseEvidenceV2NativeRead
	err := CaptureReleaseNativeSourceV2(t.Context(), fixture.native.chain, &fixture.config, fixture.intent, fixture.measurement, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		reads = append(reads, read)
		return nil
	})
	if err != nil {
		t.Fatalf("original455 source capture under460: %v", err)
	}
	if fixture.native.chain.Meta != metadata || fixture.native.chain.Runtime != runtime {
		t.Fatal("historical capture changed current signing owner")
	}
	want, err := json.Marshal(fixture.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	metadataCount := 0
	for _, read := range reads {
		if read.Method == "state_getMetadata" {
			metadataCount++
			wantParameters, err := json.Marshal([]any{fixture.native.block.Hex()})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(read.Parameters, wantParameters) {
				t.Fatal("capture changed the original metadata block")
			}
			if !bytes.Equal(read.Result, want) {
				t.Fatal("capture replaced original metadata bytes")
			}
		}
	}
	if metadataCount == 0 {
		t.Fatal("capture omitted historical metadata after authentication")
	}
}

// Inclusion is deliberately absent; real original source signatures still may
// not be replayed into the new runtime. The public reader must reach the scan.
func TestReleaseEvidenceV2HistoricalRuntimePendingSignedV2RefusesNewSubmission(t *testing.T) {
	fixture := newReleaseHistoricalSourceTestFixture(t)
	original := fixture.native.chain.API.Client
	blocks, sends := 0, 0
	fixture.native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if strings.HasPrefix(method, "author_") {
			sends++
			return errors.New("historical signed source attempted broadcast")
		}
		if method == "chain_getBlock" {
			if len(args) != 1 || args[0] != fixture.native.block.Hex() {
				return errors.New("pending signed source changed block")
			}
			blocks++
			raw, err := json.Marshal(map[string]any{"block": map[string]any{"header": types.Header{Number: types.BlockNumber(fixture.native.blockNumber)}, "extrinsics": []string{}}})
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, result)
		}
		return original.CallContext(ctx, result, method, args...)
	}}
	steerer := &ReleaseSteerer{cfg: &fixture.config, native: fixture.native.chain}
	if _, err := steerer.reconcilePendingV2(t.Context(), fixture.intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1}); err == nil || !strings.Contains(err.Error(), "pending steering replay uses a historical signing runtime") || blocks != 1 || sends != 0 {
		t.Fatalf("old signed source replay: blocks=%d sends=%d error=%v", blocks, sends, err)
	}
}

// A real signed source reaches the existing finalized-event verifier. This
// canary isolates its runtime dispatch without fabricating successful events.
func TestReleaseEvidenceV2HistoricalRuntimeFinalizedSourceReachesOriginalEventVerifier(t *testing.T) {
	fixture := newReleaseHistoricalSourceTestFixture(t)
	fixture.intent.FinalizedBlock, fixture.intent.FinalizedBlockHash = fixture.native.blockNumber, fixture.native.block.Hex()
	metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	eventKey, err := types.CreateStorageKey(metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	original := fixture.native.chain.API.Client
	events := 0
	canonical := fixture.native.block
	canary := errors.New("original finalized event reader reached")
	fixture.native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getBlockHash" && len(args) == 1 && args[0] == fixture.native.blockNumber {
			return setReleaseHistoricalTestResult(result, canonical.Hex())
		}
		if method == "chain_getBlock" {
			if len(args) != 1 || args[0] != fixture.native.block.Hex() {
				return errors.New("finalized source changed original block")
			}
			raw, err := json.Marshal(map[string]any{"block": map[string]any{"header": types.Header{Number: types.BlockNumber(fixture.native.blockNumber)}, "extrinsics": []string{fixture.intent.Prepared.ExtrinsicHex}}})
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, result)
		}
		if method == "state_getStorage" && len(args) == 2 && args[0] == eventKey.Hex() {
			if args[1] != fixture.native.block.Hex() {
				return errors.New("source events changed original block")
			}
			events++
			return canary
		}
		return original.CallContext(ctx, result, method, args...)
	}}
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), fixture.native.chain, &fixture.config, fixture.intent, fixture.artifact); !errors.Is(err, canary) || events != 1 {
		t.Fatalf("finalized original source did not reach event verification: events=%d error=%v", events, err)
	}
	canonical[0] ^= 1
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), fixture.native.chain, &fixture.config, fixture.intent, fixture.artifact); err == nil || !strings.Contains(err.Error(), "validator identity block is not canonical at its pinned height") || events != 1 {
		t.Fatalf("substituted native block escaped original schedule authentication: events=%d error=%v", events, err)
	}
}
