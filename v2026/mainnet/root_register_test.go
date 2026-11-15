// Synthetic473 registration exercises the actual new operator domain without
// production accounts, secrets, network routes or hardware effects.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

// Keys exist only in deterministic fixtures; production reads public signatures.
type rootRegisterTestFixture struct {
	storage        *durablefixture.Fixture
	input          rootRegisterPlanInput
	config         rootRegisterConfig
	key            string
	approval       ed25519.PrivateKey
	pair           subkey.KeyPair
	edKey          ed25519.PrivateKey
	metadataHex    string
	ledgerMetadata string
	metadata       *types.Metadata
}

// The public plan is assembled through the real metadata/observation admission.
func newRootRegisterTestFixture(t *testing.T, ledger bool) *rootRegisterTestFixture {
	t.Helper()
	seed := sha256.Sum256([]byte("synthetic root registration operator sr25519"))
	pair, err := (sr25519.Scheme{}).FromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	edSeed := sha256.Sum256([]byte("synthetic root registration operator ed25519"))
	edKey := ed25519.NewKeyFromSeed(edSeed[:])
	approvalSeed := sha256.Sum256([]byte("synthetic independent root operator consent"))
	approval := ed25519.NewKeyFromSeed(approvalSeed[:])
	policy := rootRegisterPolicy{Schema: rootRegisterPolicySchema, NativeChain: "synthetic-root-registration", GenesisHash: "0x" + strings.Repeat("11", 32), EvmChainId: mainnetEvmChainId, RuntimeSourceCommit: crv4.NativeOwnerSource473, RuntimeVersion: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 473, TransactionVersion: 1, StateVersion: 1}, RuntimeCodeHash: "0x" + strings.Repeat("22", 32), RuntimeMetadataHash: "0x" + strings.Repeat("33", 32), Hotkey: "0x" + strings.Repeat("44", 32), Operator: "0x" + hex.EncodeToString(pair.Public()), Reserve: "0x" + strings.Repeat("55", 32), SubnetOwner: "0x" + strings.Repeat("66", 32)}
	if ledger {
		policy.Operator = "0x" + hex.EncodeToString(edKey.Public().(ed25519.PublicKey))
	}
	birth, hash, nonce := uint64(100), "0x"+strings.Repeat("77", 32), uint32(7)
	observation, metadataHex, metadata := rootRegisterTestObservation(t, policy, birth, hash, nonce)
	// Fixture metadata is generated locally, so its hash is established before
	// either policy or observation is sealed rather than inferred by production.
	_, policy.RuntimeMetadataHash, err = crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	observation.PolicyHash = rootObjectHash(policy)
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(observation)
	action := rootRegisterAction{Schema: rootRegisterActionSchema, Policy: policy, ObservationHash: observation.ContentHash, ReviewHash: rootObjectHash("synthetic independent473 root source review"), CustodyId: "synthetic-root-operator", StatePath: filepath.Join(t.TempDir(), rootRegisterStateFile), Nonce: nonce, BirthBlock: birth, BirthHash: hash, Period: 8, FeeReserveRao: 200, QuotedBurnLimitRao: 1000, Exposure: rootRegisterBalanceExposure, SigningProfile: rootRegisterPortableHardware, HardwareCustodyHash: rootObjectHash("synthetic independently chosen root hardware"), ExposureAcknowledgements: rootRegisterExposureAcknowledgements(), SignatureScheme: "sr25519"}
	if err := os.Chmod(filepath.Dir(action.StatePath), 0700); err != nil {
		t.Fatal(err)
	}
	ledgerMetadata := ""
	if ledger {
		action.SignatureScheme = "ed25519"
		action.SigningProfile = rootRegisterLedgerHardware
		action.MetadataDigest = "0x" + strings.Repeat("88", 32)
		action.DerivationPath = "m/44'/354'/7'/0'/1'"
		ledgerMetadata = "0x6d6574610f00"
		action.LedgerMetadataHash, err = ownerLedgerMetadataHash(ledgerMetadata)
		if err != nil {
			t.Fatal(err)
		}
	}
	f := &rootRegisterTestFixture{input: rootRegisterPlanInput{Action: action, Observation: observation, Metadata: metadataHex, LedgerMetadata: ledgerMetadata, Route: ownedSubmissionRoute{RpcUrl: "http://192.0.2.44:9944", ReadRetrySeconds: 60, SendTimeoutSeconds: 1}}, key: "0x" + hex.EncodeToString(approval.Public().(ed25519.PublicKey)), approval: approval, pair: pair, edKey: edKey, metadataHex: metadataHex, ledgerMetadata: ledgerMetadata, metadata: metadata}
	f.config, err = prepareRootRegisterPlan(f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	prepareMainnetSnapshotTest(t, f.config.Action.StatePath, "mainnet-root-register", rootRegisterStoreLimit)
	f.storage = durablefixture.New(t, t.Context(), filepath.Dir(f.config.Action.StatePath))
	return f
}

// Fixture consent remains independent of the native account signature.
func (self *rootRegisterTestFixture) approve() {
	self.config.Signature = hex.EncodeToString(ed25519.Sign(self.approval, self.config.signingBytes()))
}

// Only fixture keys may construct a native signature for these tests.
func (self *rootRegisterTestFixture) signature(t *testing.T) []byte {
	t.Helper()
	payload, err := hex.DecodeString(self.config.Action.Payload[2:])
	if err != nil {
		t.Fatal(err)
	}
	if self.config.Action.SignatureScheme == "ed25519" {
		return ed25519.Sign(self.edKey, payload)
	}
	signature, err := self.pair.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

// A separate SCALE encoder proves the exact62 call and operator-signed envelope.
func TestRootRegisterNativeEnvelopeBindsOperatorAndCall(t *testing.T) {
	for _, profile := range []string{"portable-sr25519", "portable-ed25519", "ledger-ed25519"} {
		ledger := profile == "ledger-ed25519"
		f := newRootRegisterTestFixture(t, profile != "portable-sr25519")
		if profile == "portable-ed25519" {
			f.input.Action.SigningProfile = rootRegisterPortableHardware
			f.input.Action.MetadataDigest, f.input.Action.LedgerMetadataHash, f.input.Action.DerivationPath = "", "", ""
			f.input.LedgerMetadata = ""
			var err error
			f.config, err = prepareRootRegisterPlan(f.input)
			if err != nil {
				t.Fatal(err)
			}
			f.approve()
		}
		action := f.config.Action
		hotkey, _ := hex.DecodeString(action.Policy.Hotkey[2:])
		var account types.AccountID
		copy(account[:], hotkey)
		call, err := types.NewCall(f.metadata, "SubtensorModule.root_register", account)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := codec.Encode(call)
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := hex.DecodeString(action.Call[2:])
		if !bytes.Equal(actual, encoded) || len(actual) != 34 || actual[0] != 7 || actual[1] != 62 {
			t.Fatalf("registration call differs from metadata codec: %x / %x", actual, encoded)
		}
		genesis, _ := types.NewHashFromHexString(action.Policy.GenesisHash)
		birth, _ := types.NewHashFromHexString(action.BirthHash)
		mode := types.U8(0)
		metadataHash := types.NewOption[types.Hash](types.Hash{})
		metadataHash.SetNone()
		if ledger {
			mode = 1
			digest, _ := types.NewHashFromHexString(action.MetadataDigest)
			metadataHash = types.NewOption(digest)
		}
		payload := extrinsic.Payload{EncodedCall: types.BytesBare(encoded), SignedFields: []*extrinsic.SignedField{
			{Name: extrinsic.EraSignedField, Value: types.ExtrinsicEra{IsMortalEra: true, AsMortalEra: types.MortalEra{First: 0x42, Second: 0}}, Mutated: true},
			{Name: extrinsic.NonceSignedField, Value: types.NewUCompactFromUInt(7), Mutated: true},
			{Name: extrinsic.TipSignedField, Value: types.NewUCompactFromUInt(0), Mutated: true},
			{Name: extrinsic.CheckMetadataHashModeSignedField, Value: mode, Mutated: true},
		}, SignedExtraFields: []*extrinsic.SignedField{
			{Name: extrinsic.SpecVersionSignedField, Value: types.U32(473), Mutated: true},
			{Name: extrinsic.TransactionVersionSignedField, Value: types.U32(1), Mutated: true},
			{Name: extrinsic.GenesisHashSignedField, Value: genesis, Mutated: true},
			{Name: extrinsic.BlockHashSignedField, Value: birth, Mutated: true},
			{Name: extrinsic.CheckMetadataHashSignedField, Value: metadataHash, Mutated: true},
		}}
		payloadRaw, err := codec.Encode(&payload)
		if err != nil || action.Payload != "0x"+hex.EncodeToString(payloadRaw) {
			t.Fatal("registration native payload differs from independent codec", err)
		}
		signature := f.signature(t)
		operator, _ := hex.DecodeString(action.Policy.Operator[2:])
		address, err := types.NewMultiAddressFromAccountID(operator)
		if err != nil {
			t.Fatal(err)
		}
		multi := types.MultiSignature{IsSr25519: true, AsSr25519: types.NewSignature(signature)}
		if action.SignatureScheme == "ed25519" {
			multi = types.MultiSignature{IsEd25519: true, AsEd25519: types.NewSignature(signature)}
		}
		typed := extrinsic.Extrinsic{Version: extrinsic.Version4 | extrinsic.BitSigned, Method: call, Signature: &extrinsic.Signature{Signer: address, Signature: multi, SignedFields: payload.SignedFields}}
		want, err := codec.Encode(typed)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := action.signed(signature)
		if err != nil || !bytes.Equal(raw, want) {
			t.Fatal("registration operator envelope differs from independent codec", err)
		}
		if err := rootRegisterSignedAction(action, raw); err != nil {
			t.Fatal(err)
		}
		changed := append([]byte(nil), raw...)
		changed[len(changed)-1] ^= 1
		if rootRegisterSignedAction(action, changed) == nil {
			t.Fatal("signature admitted another hotkey")
		}
	}
}

// Neither an approval signature nor another schema creates a native burn cap.
func TestRootRegisterApprovalRequiresExplicitUncappedExposure(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	for _, mutate := range []func(*rootRegisterAction){
		func(a *rootRegisterAction) { a.Policy.Operator = a.Policy.Reserve },
		func(a *rootRegisterAction) { a.Policy.Operator = a.Policy.SubnetOwner },
		func(a *rootRegisterAction) { a.Policy.Hotkey = a.Policy.Operator },
		func(a *rootRegisterAction) { a.Exposure = "strict-numeric-burn-cap" },
		func(a *rootRegisterAction) { value := uint64(0); a.StrictBurnCapRao = &value },
		func(a *rootRegisterAction) { a.ExposureAcknowledgements = nil },
		func(a *rootRegisterAction) { a.Policy.RuntimeSourceCommit = rootActionV1Source },
		func(a *rootRegisterAction) { a.Policy.RuntimeVersion.SpecVersion++ },
	} {
		action := f.config.Action
		mutate(&action)
		if _, err := prepareRootRegisterAction(action, f.metadataHex); err == nil {
			t.Fatal("unsupported operator authority or fictional cost cap admitted")
		}
	}
	config := f.config
	config.Signature = ""
	if config.validate(f.key) == nil {
		t.Fatal("unsigned consent became operator authority")
	}
}

// The exact quote is reviewable; changing its threshold never inserts a cap in
// the native payload. Overflowing aggregate allowances refuse before subtraction.
func TestRootRegisterPlanKeepsQuoteSeparateFromInclusionExposure(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	eligibility, err := f.input.Observation.eligibility(f.config.Action.Policy, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if f.config.Action.QuotedBurnRao != eligibility.BurnRao || f.config.Action.ObservedFreeRao != eligibility.FreeRao || f.config.Action.ObservedReducibleRao != eligibility.ConservativeReducibleRao {
		t.Fatal("signed plan lost its concrete original quote")
	}
	action := f.config.Action
	action.QuotedBurnLimitRao++
	changed, err := prepareRootRegisterAction(action, f.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Call != f.config.Action.Call || changed.Payload != f.config.Action.Payload || changed.RequestHash == f.config.Action.RequestHash {
		t.Fatal("off-chain consent and actual unbounded native call were conflated")
	}
	action.FeeReserveRao = math.MaxUint64
	if rootRegisterPreflightExposure(action, eligibility) == nil {
		t.Fatal("overflowing fee preflight passed")
	}
	eligibility.BurnRao = math.MaxUint64
	if rootRegisterPreflightExposure(f.config.Action, eligibility) == nil {
		t.Fatal("changed burn bypassed preflight")
	}
}

// Re-sealing public JSON cannot replace the independently selected operator,
// original approval key or the exact metadata used by the signing payload.
func TestRootRegisterPortableRequestRejectsRoleAndArtifactReplacement(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.metadataHex, f.ledgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: f.key, Owner: f.config.Action.Policy.Operator, Genesis: f.config.Action.Policy.GenesisHash}
	if err := request.validate(trust); err != nil {
		t.Fatal(err)
	}
	wrong := trust
	wrong.Owner = f.config.Action.Policy.Reserve
	if request.validate(wrong) == nil {
		t.Fatal("receive-only reserve inherited operator custody")
	}
	altered := request
	altered.Metadata = "0x00"
	altered.ContentHash = ""
	altered.ContentHash = rootObjectHash(altered)
	trust.RequestHash = altered.ContentHash
	if altered.validate(trust) == nil {
		t.Fatal("re-sealed metadata replacement reached signer")
	}
}
