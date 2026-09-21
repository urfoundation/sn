//go:build linux || darwin

package validator

// Real metadata and SDK signatures exercise the runtime boundary. Every
// block, account, storage value and pending intent is synthetic.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/crv4"
	"golang.org/x/crypto/blake2b"
)

// Preserve the future-runtime branch when the reviewed release advances.
const provisionalValidatorSuccessorTestSpec = crv4.ReviewedRuntimeSpecVersion + 1

// Metadata is public protocol data, independently content-addressed here.
func provisionalValidatorMetadataTest(t *testing.T, path, expectedHash string) (*types.Metadata, string) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 1024*1024))
	if err := errors.Join(err, reader.Close()); err != nil {
		t.Fatal(err)
	}
	wire := hexutil.Encode(raw)
	metadata, hash, err := crv4.DecodeRuntimeMetadata(wire)
	if err != nil || hash != expectedHash {
		t.Fatalf("metadata hash %s: %v", hash, err)
	}
	return metadata, wire
}

// Counts subscription attempts as well as author RPCs before any submission.
type provisionalValidatorRuntimeClient struct {
	*validatorRuntimeIdentityTestClient
	submissions *int
}

func (self *provisionalValidatorRuntimeClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	(*self.submissions)++
	return nil, errors.New("unexpected provisional test submission")
}

type provisionalValidatorRuntimeFixture struct {
	chain          *crv4.Chain
	cfg            ReleaseConfig
	block          types.Hash
	oldBlock       types.Hash
	metadata       *types.Metadata
	oldMetadata    *types.Metadata
	metadataHex    string
	oldMetadataHex string
	code           string
	version        uint32
	transaction    uint32
	blocks         int
	submissions    int
	storageReads   int
}

// Both sides of a synthetic upgrade consume the reviewed metadata, with
// distinct code hashes and signing domains. Unsupported calls fail explicitly.
func newProvisionalValidatorRuntimeFixture(t *testing.T) *provisionalValidatorRuntimeFixture {
	t.Helper()
	self := &provisionalValidatorRuntimeFixture{cfg: validReleaseConfig(t), block: types.Hash{0xa1}, oldBlock: types.Hash{0xa0}, code: types.Hash{0x77}.Hex(), version: provisionalValidatorSuccessorTestSpec, transaction: 1}
	self.cfg.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
	self.metadata, self.metadataHex = provisionalValidatorMetadataTest(t, "../crv4/runtime-profile-v1.scale.gz.base64", "0xb0fae6d022b74faf948e3b98463b98b46c4738e87348e24340f91146ededa4bf")
	self.oldMetadata, self.oldMetadataHex = provisionalValidatorMetadataTest(t, "../crv4/runtime-profile-v1.scale.gz.base64", "0xb0fae6d022b74faf948e3b98463b98b46c4738e87348e24340f91146ededa4bf")
	genesis, err := types.NewHashFromHexString(self.cfg.GenesisHash)
	if err != nil {
		t.Fatal(err)
	}
	self.chain = &crv4.Chain{GenesisHash: genesis, Meta: self.oldMetadata, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: types.U32(self.cfg.RuntimeSpec), TransactionVersion: 1}}
	client := &provisionalValidatorRuntimeClient{submissions: &self.submissions}
	client.validatorRuntimeIdentityTestClient = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasPrefix(method, "author_") {
			self.submissions++
			return errors.New("unexpected author RPC")
		}
		if method == "chain_getFinalizedHead" {
			return setReleaseHistoricalTestResult(result, self.block.Hex())
		}
		if method == "chain_getBlockHash" {
			if len(args) != 1 {
				return errors.New("block hash arguments differ")
			}
			switch args[0] {
			case uint64(0):
				return setReleaseHistoricalTestResult(result, self.chain.GenesisHash.Hex())
			case uint64(100):
				return setReleaseHistoricalTestResult(result, self.oldBlock.Hex())
			case uint64(101):
				return setReleaseHistoricalTestResult(result, self.block.Hex())
			default:
				return fmt.Errorf("unexpected block number %v", args[0])
			}
		}
		if len(args) == 0 {
			return fmt.Errorf("unpinned %s", method)
		}
		block := args[len(args)-1]
		if block != self.block.Hex() && block != self.oldBlock.Hex() {
			return fmt.Errorf("changed block for %s: %v", method, block)
		}
		old := block == self.oldBlock.Hex()
		number := uint64(101)
		if old {
			number = 100
		}
		switch method {
		case "chain_getHeader":
			return setReleaseHistoricalTestResult(result, types.Header{Number: types.BlockNumber(number)})
		case "chain_getBlock":
			self.blocks++
			return setReleaseHistoricalTestResult(result, map[string]any{"block": map[string]any{"header": types.Header{Number: types.BlockNumber(number)}, "extrinsics": []string{}}})
		case "state_getRuntimeVersion":
			version, transaction := self.version, self.transaction
			if old {
				version, transaction = self.cfg.RuntimeSpec, self.cfg.TransactionVersion
			}
			return setReleaseHistoricalTestResult(result, map[string]any{"specName": "node-subtensor", "specVersion": version, "transactionVersion": transaction, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		case "state_getStorageHash":
			if len(args) != 2 || args[0] != "0x3a636f6465" {
				return errors.New("runtime code key changed")
			}
			code := self.code
			if old {
				code = self.cfg.RuntimeCodeHash
			}
			return setReleaseHistoricalTestResult(result, code)
		case "state_getMetadata":
			metadata := self.metadataHex
			if old {
				metadata = self.oldMetadataHex
			}
			return setReleaseHistoricalTestResult(result, metadata)
		case "state_getStorage":
			self.storageReads++
			key, err := types.CreateStorageKey(self.metadata, "Timestamp", "Now")
			if err != nil {
				return err
			}
			if len(args) != 2 || args[0] != key.Hex() {
				return errors.New("unexpected native storage read")
			}
			return setReleaseHistoricalTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint64(nil, 120000)))
		default:
			return fmt.Errorf("unexpected provisional native RPC %s", method)
		}
	}}
	self.chain.API = &gsrpc.SubstrateAPI{Client: client}
	return self
}

func TestReleaseProvisionalRuntimeConfigRequiresSeparateExactAuthority(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
	loaded, err := LoadReleaseConfig(writeReleaseConfig(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RuntimeSpec != cfg.RuntimeSpec || loaded.RuntimeCodeHash != cfg.RuntimeCodeHash {
		t.Fatal("provisional profile relabeled original config")
	}
	if err := loaded.ValidateHistorical(); err == nil {
		t.Fatal("provisional config authorized a final archive")
	}
	for _, item := range []struct {
		name   string
		mutate func(*ReleaseConfig)
	}{
		{"profile", func(c *ReleaseConfig) { c.ProvisionalRuntimeCompatibility = "unreviewed-profile" }},
		{"chain", func(c *ReleaseConfig) { c.ChainID = 1 }},
		{"genesis", func(c *ReleaseConfig) { c.GenesisHash = types.Hash{9}.Hex() }},
		{"policy", func(c *ReleaseConfig) { c.Policy.NetworkProfile = "mainnet" }},
	} {
		changed := cfg
		item.mutate(&changed)
		if _, err := LoadReleaseConfig(writeReleaseConfig(t, changed)); err == nil {
			t.Fatalf("%s authority admitted", item.name)
		}
	}
	request, path := historyAdoptionRequestTest(t)
	strict, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	strict.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
	if err := request.configure(strict, path); err == nil {
		t.Fatal("provisional config authorized strict history adoption")
	}
}

func TestReleaseProvisionalRuntimeBindsObservedVersionWithoutRelabelingConfig(t *testing.T) {
	fixture := newProvisionalValidatorRuntimeFixture(t)
	strict := fixture.cfg
	strict.ProvisionalRuntimeCompatibility = ""
	strict.ProvisionalDeferClosedNativeInput = true
	if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &strict); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticatePinnedNativeRuntimeContext(t.Context(), fixture.chain, &strict); err == nil {
		t.Fatal("closed-input permission enabled future runtime")
	}
	if fixture.chain.Runtime.SpecVersion != types.U32(fixture.cfg.RuntimeSpec) {
		t.Fatal("strict refusal changed signing view")
	}
	if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	hash, err := authenticatePinnedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hash != fixture.block || fixture.chain.Runtime.SpecVersion != types.U32(provisionalValidatorSuccessorTestSpec) || fixture.chain.Meta == fixture.oldMetadata || fixture.cfg.RuntimeSpec != crv4.ReviewedRuntimeSpecVersion {
		t.Fatal("actual runtime was not bound independently from original config")
	}
	if err := validateReleaseNativeSigningRuntime(fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseNativeSigningRuntime(fixture.chain, &strict); err == nil {
		t.Fatal("strict signing postcheck inherited another owner's permission")
	}
	files, err := os.ReadDir(filepath.Join(fixture.cfg.StateDir, "runtime-compatibility"))
	if err != nil || len(files) != 1 {
		t.Fatalf("durable observation count=%d: %v", len(files), err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture.cfg.StateDir, "runtime-compatibility", files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{fixture.cfg.GenesisHash, fixture.block.Hex(), fixture.code, fmt.Sprintf(`"specVersion": %d`, provisionalValidatorSuccessorTestSpec), `"provisional": true`, `"final_acceptance": false`} {
		if !bytes.Contains(raw, []byte(value)) {
			t.Fatalf("observation omitted %s", value)
		}
	}
}

func TestReleaseProvisionalRuntimeRejectsWrongGenesisAndObservationFailure(t *testing.T) {
	for _, fault := range []string{"genesis", "observation", "transaction"} {
		fixture := newProvisionalValidatorRuntimeFixture(t)
		if fault == "genesis" {
			fixture.chain.GenesisHash = types.Hash{8}
		}
		if fault == "observation" {
			if err := os.MkdirAll(filepath.Dir(fixture.cfg.StateDir), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.cfg.StateDir, []byte("blocked"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if fault == "transaction" {
			fixture.transaction = 2
		}
		err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg)
		if err == nil {
			_, err = authenticatePinnedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg)
		}
		if err == nil || fixture.chain.Runtime.SpecVersion != types.U32(fixture.cfg.RuntimeSpec) || fixture.submissions != 0 {
			t.Fatalf("%s admitted or changed signing view: %v", fault, err)
		}
	}
}

// A retained batch keeps its reviewed signing domain while an absent receipt
// is recovered under the independently authenticated successor observation.
func provisionalValidatorPendingReviewedTest(t *testing.T, fixture *provisionalValidatorRuntimeFixture) *SteeringIntent {
	t.Helper()
	signer := &crv4.Chain{Meta: fixture.oldMetadata, GenesisHash: fixture.chain.GenesisHash, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: types.U32(fixture.cfg.RuntimeSpec), TransactionVersion: 1}}
	return provisionalValidatorSignedSourceTest(t, fixture, signer)
}

// The signer contributes its actual authenticated version and optional profile
// to the retained envelope; SDK signing uses that same native runtime view.
func provisionalValidatorSignedSourceTest(t *testing.T, fixture *provisionalValidatorRuntimeFixture, signer *crv4.Chain) *SteeringIntent {
	t.Helper()
	retained := newReleaseHistoricalSourceTestFixture(t)
	prepared := retained.intent.Prepared
	prepared.PreparedAtBlock, prepared.PreparedAtBlockHash = 100, fixture.oldBlock.Hex()
	if signer.Runtime.SpecVersion == types.U32(provisionalValidatorSuccessorTestSpec) {
		prepared.PreparedAtBlock, prepared.PreparedAtBlockHash = 101, fixture.block.Hex()
	}
	prepared.SourceCommitment.GenesisHash = fixture.chain.GenesisHash.Hex()
	prepared.SourceCommitment.RuntimeSpec = uint32(signer.Runtime.SpecVersion)
	prepared.SourceCommitment.CompatibilityProfile = signer.CurrentRuntimeCompatibilityProfile()
	key, err := crv4.KeypairFromSeed([32]byte{0x77})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := types.NewHashFromHexString(prepared.SourceCommitment.Hash)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := signer.NewSetFleetCommitmentCall(prepared.Netuid, [32]byte(hash))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := hexutil.Decode(prepared.CiphertextHex)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := types.NewCall(signer.Meta, "SubtensorModule.commit_timelocked_weights", types.U16(prepared.Netuid), types.Bytes(ciphertext), types.U64(prepared.RevealRound), types.U16(prepared.CommitRevealVersion))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := types.NewCall(signer.Meta, "Utility.batch_all", []types.Call{anchor, commit})
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
	digest := blake2b.Sum256(raw)
	prepared.ExtrinsicHex, prepared.ExtrinsicHash = hexutil.Encode(raw), hexutil.Encode(digest[:])
	if err := signer.ValidatePreparedSource(prepared); err != nil {
		t.Fatal(err)
	}
	return retained.intent
}

func TestReleaseProvisionalRuntimeSourceUsesActualSuccessorSignedDomain(t *testing.T) {
	fixture := newProvisionalValidatorRuntimeFixture(t)
	if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticatePinnedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	intent := provisionalValidatorSignedSourceTest(t, fixture, fixture.chain)
	if intent.Prepared.SourceCommitment.RuntimeSpec != provisionalValidatorSuccessorTestSpec || intent.Prepared.SourceCommitment.CompatibilityProfile != crv4.ProvisionalRuntimeCompatibilityProfile || fixture.cfg.RuntimeSpec != crv4.ReviewedRuntimeSpecVersion {
		t.Fatal("actual signed domain replaced config authority")
	}
	intent.Prepared.SourceCommitment.RuntimeSpec = fixture.cfg.RuntimeSpec
	if err := fixture.chain.ValidatePreparedSource(intent.Prepared); err == nil {
		t.Fatal("successor signature was relabeled with the reviewed signing domain")
	}
	intent.Prepared.SourceCommitment.RuntimeSpec = provisionalValidatorSuccessorTestSpec
	intent.Prepared.SourceCommitment.CompatibilityProfile = ""
	if err := fixture.chain.ValidatePreparedSource(intent.Prepared); err == nil {
		t.Fatal("future source without explicit profile was admitted")
	}
}

func TestReleaseProvisionalRuntimePendingReviewedRefusesReplayInBothIntentOwners(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		fixture := newProvisionalValidatorRuntimeFixture(t)
		if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg); err != nil {
			t.Fatal(err)
		}
		intent := provisionalValidatorPendingReviewedTest(t, fixture)
		originalBytes := intent.Prepared.ExtrinsicHex
		steerer := &ReleaseSteerer{cfg: &fixture.cfg, native: fixture.chain}
		var done bool
		var err error
		if version == "v1" {
			done, err = steerer.reconcilePending(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1})
		} else {
			done, err = steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1})
		}
		if done || err == nil || !strings.Contains(err.Error(), "signing runtime differs") || fixture.blocks != 2 || fixture.submissions != 0 || fixture.storageReads != 0 {
			t.Fatalf("%s recovery done=%t blocks=%d submit=%d storage=%d error=%v", version, done, fixture.blocks, fixture.submissions, fixture.storageReads, err)
		}
		if intent.Prepared.SourceCommitment.RuntimeSpec != fixture.cfg.RuntimeSpec || intent.Prepared.ExtrinsicHex != originalBytes || fixture.chain.Runtime.SpecVersion != types.U32(provisionalValidatorSuccessorTestSpec) {
			t.Fatalf("%s changed retained authority or live signing runtime", version)
		}
	}
}

// A real V1 commit has no source-domain check inside SubmitPrepared. Both
// fresh production callers therefore use this same authenticated boundary.
func TestReleaseProvisionalRuntimeFreshSubmissionRejectsUpgradeBeforeBroadcast(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		fixture := newProvisionalValidatorRuntimeFixture(t)
		if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg); err != nil {
			t.Fatal(err)
		}
		prepared := provisionalValidatorPendingReviewedTest(t, fixture).Prepared
		if version == "v1" {
			prepared.Schema, prepared.SourceCommitment = crv4.PreparedSubmissionSchema, nil
			ciphertext, err := hexutil.Decode(prepared.CiphertextHex)
			if err != nil {
				t.Fatal(err)
			}
			call, err := types.NewCall(fixture.oldMetadata, "SubtensorModule.commit_timelocked_weights", types.U16(prepared.Netuid), types.Bytes(ciphertext), types.U64(prepared.RevealRound), types.U16(prepared.CommitRevealVersion))
			if err != nil {
				t.Fatal(err)
			}
			key, err := crv4.KeypairFromSeed([32]byte{0x77})
			if err != nil {
				t.Fatal(err)
			}
			signer := &crv4.Chain{Meta: fixture.oldMetadata, GenesisHash: fixture.chain.GenesisHash, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: types.U32(fixture.cfg.RuntimeSpec), TransactionVersion: 1}}
			signed, err := signer.NewSignedExtrinsic(key, call, prepared.AccountNonce)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := codec.Encode(signed)
			if err != nil {
				t.Fatal(err)
			}
			digest := blake2b.Sum256(raw)
			prepared.ExtrinsicHex, prepared.ExtrinsicHash = hexutil.Encode(raw), hexutil.Encode(digest[:])
		}
		if _, err := prepared.Validate(); err != nil {
			t.Fatal(err)
		}
		result, attempted, err := submitPreparedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg, prepared)
		if result != nil || attempted || err == nil || !strings.Contains(err.Error(), "signing runtime differs") || fixture.submissions != 0 {
			t.Fatalf("%s stale fresh submission attempted=%t submissions=%d error=%v", version, attempted, fixture.submissions, err)
		}
	}
}

// This reaches the real transport submission boundary for a successor SDK
// source, then stops at a deterministic transport refusal without live I/O.
func TestReleaseProvisionalRuntimeFreshCurrentSourceReachesSubmissionBoundary(t *testing.T) {
	fixture := newProvisionalValidatorRuntimeFixture(t)
	if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticatePinnedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	prepared := provisionalValidatorSignedSourceTest(t, fixture, fixture.chain).Prepared
	result, attempted, err := submitPreparedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg, prepared)
	if result != nil || !attempted || err == nil || !strings.Contains(err.Error(), "unexpected provisional test submission") || fixture.submissions != 1 {
		t.Fatalf("current submission attempted=%t submissions=%d error=%v", attempted, fixture.submissions, err)
	}
}

func TestValidatorUploadProvisionalRuntimeInstallsIndependentObservedOwner(t *testing.T) {
	fixture := newProvisionalValidatorRuntimeFixture(t)
	upload := newValidatorUploadAuthorityTestFixture(t)
	cfg := upload.config()
	cfg.Deployment.GenesisHash = [32]byte(fixture.chain.GenesisHash)
	cfg.Deployment.NativeRuntime = releaseRuntimeIdentityV2(&fixture.cfg)
	cfg.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
	cfg.RuntimeObservationDir = filepath.Join(t.TempDir(), "upload-runtime")
	owner, err := newValidatorUploadAdmissionState(t.Context(), upload.chain, fixture.chain, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.cancel()
	observer, err := ValidatorUploadNativeObserverContext(t.Context(), owner.native, cfg.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	if observer.Hash != fixture.block || observer.Number != 101 || observer.TimestampMillis != 120000 || fixture.storageReads != 1 || cfg.Deployment.NativeRuntime.Version.SpecVersion != fixture.cfg.RuntimeSpec {
		t.Fatal("upload observation did not preserve current state and original deployment authority")
	}
	for _, fault := range []string{"profile", "directory", "chain", "genesis"} {
		changed := cfg
		switch fault {
		case "profile":
			changed.ProvisionalRuntimeCompatibility = ""
		case "directory":
			changed.RuntimeObservationDir = ""
		case "chain":
			changed.Deployment.ChainID = 1
		case "genesis":
			changed.Deployment.GenesisHash = [32]byte{9}
		}
		if err := changed.Validate(); err == nil {
			t.Fatalf("upload %s accepted", fault)
		}
	}
}
