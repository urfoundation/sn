// Synthetic state exercises current470 protocol metadata without live identities,
// keys or routes. Every custody race/failure uses an explicit state boundary.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

// Independent synthetic approval and native account keys never leave this test.
type ownerRecycleTestFixture struct {
	storage  *durablefixture.Fixture
	input    ownerRecyclePlanInput
	config   ownerRecycleConfig
	key      string
	approval ed25519.PrivateKey
	pair     subkey.KeyPair
	edKey    ed25519.PrivateKey
	metadata *types.Metadata
}

// The identity-free public protocol projection retains consumed wire layouts.
func ownerRecycleTestMetadata(t *testing.T) (string, *types.Metadata) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime470-recycle-codec.scale.gz.base64")
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
	raw, err := io.ReadAll(io.LimitReader(reader, maxMetadataRpcReplyBytes))
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	metadata, _, err := crv4.DecodeRuntimeMetadata("0x" + hex.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + hex.EncodeToString(raw), metadata
}

// Build a full open-window Burn observation and an independently approved plan.
func newOwnerRecycleTestFixture(t *testing.T, ledger bool) *ownerRecycleTestFixture {
	t.Helper()
	metadataHex, metadata := ownerRecycleTestMetadata(t)
	seed := sha256.Sum256([]byte("synthetic recycle sr25519 owner"))
	pair, err := (sr25519.Scheme{}).FromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	edSeed := sha256.Sum256([]byte("synthetic recycle ed25519 owner"))
	edKey := ed25519.NewKeyFromSeed(edSeed[:])
	approvalSeed := sha256.Sum256([]byte("synthetic recycle independent reviewer"))
	approval := ed25519.NewKeyFromSeed(approvalSeed[:])
	_, digest, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	action := ownerRecycleAction{Schema: ownerRecycleActionSchema, Policy: recyclePolicy{Schema: recyclePolicySchema, NativeChain: "synthetic-recycle-chain", GenesisHash: "0x" + strings.Repeat("13", 32), EvmChainId: mainnetEvmChainId, Netuid: 25, StorageProfile: recycleStorageProfile, RuntimeSourceCommit: rootPassiveSource, RuntimeVersion: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1}, RuntimeCodeHash: "0x" + strings.Repeat("24", 32), RuntimeMetadataHash: digest}, ReviewHash: rootObjectHash("synthetic independent recycle review"), CustodyId: "synthetic-recycle-owner", StatePath: filepath.Join(t.TempDir(), ownerRecycleStateFile), Owner: "0x" + hex.EncodeToString(pair.Public()), SubnetRegistrationBlock: 10, Nonce: 7, BirthBlock: 100, BirthHash: "0x" + strings.Repeat("35", 32), Period: 8, FeeReserveRao: 200, SignatureScheme: "sr25519"}
	if err := os.Chmod(filepath.Dir(action.StatePath), 0700); err != nil {
		t.Fatal(err)
	}
	ledgerMetadata := ""
	if ledger {
		action.Owner = "0x" + hex.EncodeToString(edKey.Public().(ed25519.PublicKey))
		action.SignatureScheme = "ed25519"
		action.MetadataDigest = "0x" + strings.Repeat("46", 32)
		action.DerivationPath = "m/44'/354'/0'/0'/0'"
		ledgerMetadata = "0x6d6574610f00"
		action.LedgerMetadataHash, _ = ownerLedgerMetadataHash(ledgerMetadata)
	}
	entries, err := ownerRecycleEntries(metadata, action.Owner)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := hex.DecodeString(action.Owner[2:])
	account := make([]byte, 56)
	binary.LittleEndian.PutUint32(account, action.Nonce)
	binary.LittleEndian.PutUint64(account[16:], 100000)
	values := map[string][]byte{"NetworksAdded": {1}, "SubnetOwner": owner, "NetworkRegisteredAt": binary.LittleEndian.AppendUint64(nil, 10), "Tempo": binary.LittleEndian.AppendUint16(nil, 100), "LastEpochBlock": binary.LittleEndian.AppendUint64(nil, 96), "System.Account": account}
	observation := ownerRecycleObservation{Schema: ownerRecycleObservationSchema, PolicyHash: rootObjectHash(action.Policy), FinalizedNumber: action.BirthBlock, FinalizedHash: action.BirthHash, Owner: action.Owner}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := entries[name]
		row := rootStorageValue{Name: name, Key: entry.key, EffectiveScale: "0x" + hex.EncodeToString(entry.fallback), ValueSource: "authenticated-metadata-fallback"}
		if data, ok := values[name]; ok {
			encoded := "0x" + hex.EncodeToString(data)
			row.RawStorage = &encoded
			row.EffectiveScale = encoded
			row.ValueSource = "finalized-storage"
		}
		observation.Storage = append(observation.Storage, row)
	}
	observation.ContentHash = rootObjectHash(observation)
	action.ObservationHash = observation.ContentHash
	f := &ownerRecycleTestFixture{input: ownerRecyclePlanInput{Action: action, Observation: observation, Metadata: metadataHex, LedgerMetadata: ledgerMetadata, Route: ownedSubmissionRoute{RpcUrl: "http://192.0.2.25:9944", ReadRetrySeconds: 60, SendTimeoutSeconds: 1}}, key: "0x" + hex.EncodeToString(approval.Public().(ed25519.PublicKey)), approval: approval, pair: pair, edKey: edKey, metadata: metadata}
	f.config, err = prepareOwnerRecyclePlan(f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	prepareMainnetSnapshotTest(t, f.config.Action.StatePath, "mainnet-owner-recycle", ownerRecycleStoreLimit)
	f.storage = durablefixture.New(t, t.Context(), filepath.Dir(f.config.Action.StatePath))
	return f
}

// Only the synthetic independent review key can authorize fixture mutations.
func (self *ownerRecycleTestFixture) approve() {
	self.config.Signature = hex.EncodeToString(ed25519.Sign(self.approval, self.config.signingBytes()))
}

// The public native signature is generated only from the fixture's private key.
func (self *ownerRecycleTestFixture) signature(t *testing.T) []byte {
	t.Helper()
	payload, _ := hex.DecodeString(self.config.Action.Payload[2:])
	if self.config.Action.SignatureScheme == "ed25519" {
		return ed25519.Sign(self.edKey, payload)
	}
	signature, err := self.pair.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

// Re-seal a mutated raw row without allowing stale derived storage to survive.
func ownerRecycleTestSet(observation *ownerRecycleObservation, name string, data []byte) {
	for index := range observation.Storage {
		row := &observation.Storage[index]
		if row.Name == name {
			encoded := "0x" + hex.EncodeToString(data)
			row.RawStorage = &encoded
			row.EffectiveScale = encoded
			row.ValueSource = "finalized-storage"
		}
	}
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(*observation)
}

// Typed Substrate call/payload/extrinsic encoding is independent of manual codec.
func TestOwnerRecycleCurrentMetadataTypedNativeEnvelopes(t *testing.T) {
	for _, ledger := range []bool{false, true} {
		f := newOwnerRecycleTestFixture(t, ledger)
		a := f.config.Action
		call, err := types.NewCall(f.metadata, "AdminUtils.sudo_set_recycle_or_burn", types.U16(25), types.U8(1))
		if err != nil {
			t.Fatal(err)
		}
		callRaw, err := codec.Encode(call)
		if err != nil || a.Call != "0x"+hex.EncodeToString(callRaw) || len(callRaw) != 5 || callRaw[1] != 80 {
			t.Fatal("current470 call differs", err)
		}
		genesis, _ := types.NewHashFromHexString(a.Policy.GenesisHash)
		birth, _ := types.NewHashFromHexString(a.BirthHash)
		mode := types.U8(0)
		metadataHash := types.NewOption[types.Hash](types.Hash{})
		metadataHash.SetNone()
		if ledger {
			mode = 1
			digest, _ := types.NewHashFromHexString(a.MetadataDigest)
			metadataHash = types.NewOption(digest)
		}
		payload := extrinsic.Payload{EncodedCall: types.BytesBare(callRaw), SignedFields: []*extrinsic.SignedField{{Name: extrinsic.EraSignedField, Value: types.ExtrinsicEra{IsMortalEra: true, AsMortalEra: types.MortalEra{First: 0x42, Second: 0}}, Mutated: true}, {Name: extrinsic.NonceSignedField, Value: types.NewUCompactFromUInt(7), Mutated: true}, {Name: extrinsic.TipSignedField, Value: types.NewUCompactFromUInt(0), Mutated: true}, {Name: extrinsic.CheckMetadataHashModeSignedField, Value: mode, Mutated: true}}, SignedExtraFields: []*extrinsic.SignedField{{Name: extrinsic.SpecVersionSignedField, Value: types.U32(470), Mutated: true}, {Name: extrinsic.TransactionVersionSignedField, Value: types.U32(1), Mutated: true}, {Name: extrinsic.GenesisHashSignedField, Value: genesis, Mutated: true}, {Name: extrinsic.BlockHashSignedField, Value: birth, Mutated: true}, {Name: extrinsic.CheckMetadataHashSignedField, Value: metadataHash, Mutated: true}}}
		payloadRaw, err := codec.Encode(&payload)
		if err != nil || a.Payload != "0x"+hex.EncodeToString(payloadRaw) {
			t.Fatal("typed native payload differs", ledger, err)
		}
		signature := f.signature(t)
		account, _ := hex.DecodeString(a.Owner[2:])
		address, _ := types.NewMultiAddressFromAccountID(account)
		multi := types.MultiSignature{IsSr25519: true, AsSr25519: types.NewSignature(signature)}
		if ledger {
			multi = types.MultiSignature{IsEd25519: true, AsEd25519: types.NewSignature(signature)}
		}
		typed := extrinsic.Extrinsic{Version: extrinsic.Version4 | extrinsic.BitSigned, Method: call, Signature: &extrinsic.Signature{Signer: address, Signature: multi, SignedFields: payload.SignedFields}}
		want, err := codec.Encode(typed)
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.signed(signature)
		if err != nil || !bytes.Equal(got, want) || ownerRecycleSignedAction(a, got) != nil {
			t.Fatal("native envelope differs", ledger, err)
		}
		request, err := newOwnerRecycleSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
		if err != nil || request.SigningBytes != a.Payload {
			t.Fatal("portable native bytes differ", err)
		}
	}
}

// Null/default Burn triggers the transition planner; it never passes a mode gate.
func TestOwnerRecyclePlanRejectsClosedWindowAndChangedFacts(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	mutations := []func(*ownerRecyclePlanInput){
		func(i *ownerRecyclePlanInput) { ownerRecycleTestSet(&i.Observation, "RecycleOrBurn", []byte{1}) },
		func(i *ownerRecyclePlanInput) { ownerRecycleTestSet(&i.Observation, "RecycleOrBurn", []byte{2}) },
		func(i *ownerRecyclePlanInput) { ownerRecycleTestSet(&i.Observation, "NetworksAdded", []byte{0}) },
		func(i *ownerRecyclePlanInput) {
			ownerRecycleTestSet(&i.Observation, "SubnetOwner", bytes.Repeat([]byte{9}, 32))
		},
		func(i *ownerRecyclePlanInput) {
			ownerRecycleTestSet(&i.Observation, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 110))
		},
		func(i *ownerRecyclePlanInput) {
			ownerRecycleTestSet(&i.Observation, "LastRateLimitedBlock", binary.LittleEndian.AppendUint64(nil, 90))
		},
		func(i *ownerRecyclePlanInput) {
			ownerRecycleTestSet(&i.Observation, "Tempo", binary.LittleEndian.AppendUint16(nil, 20))
			ownerRecycleTestSet(&i.Observation, "AdminFreezeWindow", binary.LittleEndian.AppendUint16(nil, 10))
		},
		func(i *ownerRecyclePlanInput) { i.Action.Nonce++ },
		func(i *ownerRecyclePlanInput) { i.Action.FeeReserveRao = 100001 },
		func(i *ownerRecyclePlanInput) { i.Action.BirthHash = "0x" + strings.Repeat("58", 32) },
		func(i *ownerRecyclePlanInput) { i.Action.Policy.RuntimeSourceCommit = rootProfileSource },
	}
	for index, mutate := range mutations {
		input := ownerTrimTestCopy(t, f.input)
		mutate(&input)
		input.Action.ObservationHash = input.Observation.ContentHash
		if _, err := prepareOwnerRecyclePlan(input); err == nil {
			t.Fatalf("changed/closed facts accepted %d", index)
		}
	}
	window := ownerRecycleWindow{Tempo: 100, LastEpoch: 100, Freeze: 0, RateEpochs: 2, LastUpdate: 1}
	if window.permits(200) || !window.permits(201) {
		t.Fatal("invalid rate boundary")
	}
	window = ownerRecycleWindow{Tempo: 100, LastEpoch: 100, Freeze: 10}
	if !window.permits(190) || window.permits(191) {
		t.Fatal("strict remaining<freeze boundary changed")
	}
	window = ownerRecycleWindow{Tempo: 0, PendingEpoch: 999, Freeze: 10, RateEpochs: 2, LastUpdate: 100}
	if !window.permits(100) {
		t.Fatal("tempo0 source short circuit changed")
	}
}

// Rehashed metadata still cannot redefine the consumed call, fallback or rate key.
func TestOwnerRecycleMetadataDriftFailsClosed(t *testing.T) {
	mutations := []func(*types.Metadata){
		func(m *types.Metadata) {
			for _, p := range m.AsMetadataV14.Pallets {
				if p.Name == "AdminUtils" {
					variants := m.AsMetadataV14.EfficientLookup[p.Calls.Type.Int64()]
					for i := range variants.Def.Variant.Variants {
						if variants.Def.Variant.Variants[i].Name == "sudo_set_recycle_or_burn" {
							variants.Def.Variant.Variants[i].Index = 81
						}
					}
				}
			}
		},
		func(m *types.Metadata) {
			for p := range m.AsMetadataV14.Pallets {
				for i := range m.AsMetadataV14.Pallets[p].Storage.Items {
					e := &m.AsMetadataV14.Pallets[p].Storage.Items[i]
					if e.Name == "RecycleOrBurn" {
						e.Fallback = []byte{1}
					}
				}
			}
		},
		func(m *types.Metadata) {
			for _, p := range m.AsMetadataV14.Pallets {
				for _, e := range p.Storage.Items {
					if e.Name == "RecycleOrBurn" {
						variants := m.AsMetadataV14.EfficientLookup[e.Type.AsMap.Value.Int64()]
						variants.Def.Variant.Variants[1].Index = 2
					}
				}
			}
		},
		func(m *types.Metadata) {
			for _, p := range m.AsMetadataV14.Pallets {
				for _, e := range p.Storage.Items {
					if e.Name == "LastRateLimitedBlock" {
						variants := m.AsMetadataV14.EfficientLookup[e.Type.AsMap.Key.Int64()]
						for i := range variants.Def.Variant.Variants {
							if variants.Def.Variant.Variants[i].Name == "OwnerHyperparamUpdate" {
								variants.Def.Variant.Variants[i].Index = 7
							}
						}
					}
				}
			}
		},
		func(m *types.Metadata) {
			m.AsMetadataV14.Pallets = append(m.AsMetadataV14.Pallets, m.AsMetadataV14.Pallets[0])
		},
	}
	for index, mutate := range mutations {
		f := newOwnerRecycleTestFixture(t, false)
		mutate(f.metadata)
		if _, err := ownerRecycleEntries(f.metadata, f.config.Action.Owner); err == nil {
			t.Fatalf("metadata drift accepted %d", index)
		}
	}
}

// Request trust pins and fresh approval domains are checked before device output.
func TestOwnerRecycleLedgerRequestTrustAndTranscript(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	request, err := newOwnerRecycleSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: f.key, Owner: f.config.Action.Owner, Genesis: f.config.Action.Policy.GenesisHash}
	proof := bytes.Repeat([]byte{19}, 400)
	transcript, err := request.ledgerTranscript(trust, proof)
	if err != nil || transcript.DeviceQualified || transcript.MetadataProofVerified || transcript.Signing || transcript.NetworkEffects || transcript.RequestHash != request.ContentHash {
		t.Fatal("Ledger planning granted authority", err)
	}
	var joined []byte
	for _, encoded := range transcript.SigningApdus[1:] {
		data, _ := hex.DecodeString(encoded[2:])
		joined = append(joined, data[5:]...)
	}
	payload, _ := hex.DecodeString(request.SigningBytes[2:])
	if !bytes.Equal(joined, append(payload, proof...)) {
		t.Fatal("Ledger chunks changed exact payload/proof")
	}
	for _, mutate := range []func(*ownerSigningTrust){func(t *ownerSigningTrust) { t.RequestHash = rootObjectHash("foreign request") }, func(t *ownerSigningTrust) { t.Owner = "0x" + strings.Repeat("67", 32) }, func(t *ownerSigningTrust) { t.Genesis = "0x" + strings.Repeat("78", 32) }, func(t *ownerSigningTrust) { t.ApprovalKey = "0x" + strings.Repeat("89", 32) }} {
		changed := trust
		mutate(&changed)
		if _, err := request.ledgerTranscript(changed, proof); err == nil {
			t.Fatal("foreign trust admitted")
		}
	}
	if _, err := request.ledgerTranscript(trust, bytes.Repeat([]byte{1}, ownerLedgerPayloadLimit)); err == nil {
		t.Fatal("unbounded proof admitted")
	}
	changed := f.config
	changed.Schema = ownerTrimLedgerExecutionSchema
	changed.Signature = hex.EncodeToString(ed25519.Sign(f.approval, changed.signingBytes()))
	if changed.validate(f.key) == nil {
		t.Fatal("trim approval domain admitted recycle")
	}
}

// Real filesystem custody forces failure after durable rename, then resumes the
// exact export/signature and refuses replacement or disappearance of signed state.
func TestOwnerRecycleDurableExportImportRecovery(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	if _, err := openOwnerRecycleStore(f.config, f.key, false, f.storage.Context); err == nil {
		t.Fatal("second local owner admitted")
	}
	store.syncDirectory = func(*os.File) error { return errors.New("synthetic post-rename sync failure") }
	if _, err := custody.export(f.input.Metadata, ""); err == nil {
		t.Fatal("export acknowledged ambiguous durability")
	}
	if _, err := custody.export(f.input.Metadata, ""); err == nil {
		t.Fatal("poisoned export continued")
	}
	store.close()
	store, err = openOwnerRecycleStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody = ownerRecycleCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, "")
	if err != nil {
		t.Fatal(err)
	}
	signature := f.signature(t)
	store.syncDirectory = func(*os.File) error { return errors.New("synthetic lost import acknowledgement") }
	if _, err := custody.importSignature(request.ContentHash, signature); err == nil {
		t.Fatal("import acknowledged ambiguous durability")
	}
	store.close()
	store, err = openOwnerRecycleStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody = ownerRecycleCustody{config: f.config, key: f.key, store: store}
	result, err := custody.importSignature(request.ContentHash, signature)
	if err != nil || result.Phase != "signed" {
		t.Fatal("lost import did not recover", err)
	}
	record, err := custody.load()
	if err != nil {
		t.Fatal(err)
	}
	original := record.RawExtrinsic
	if _, err := custody.importSignature(request.ContentHash, f.signature(t)); err == nil {
		t.Fatal("replacement randomized signature admitted")
	}
	if _, err := custody.export(f.input.Metadata, ""); err == nil {
		t.Fatal("signed custody re-exported for signing")
	}
	record, err = custody.load()
	if err != nil || record.RawExtrinsic != original {
		t.Fatal("original bytes changed", err)
	}
	store.close()
	if err := os.Remove(f.config.Action.StatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := openOwnerRecycleStore(f.config, f.key, false, f.storage.Context); err == nil {
		t.Fatal("lost completed state became fresh custody")
	}
}

// The fixture builds exact canonical bodies and modern native fee/dispatch events.
func ownerRecycleTestChain(t *testing.T, f *ownerRecycleTestFixture, count int, included bool) (*ownerRecycleCanonicalChain, *rootReceiptFixture, ownerRecycleSigningRequest, []byte) {
	t.Helper()
	a := f.config.Action
	fixture := &rootReceiptFixture{metadata: f.metadata, metadataHex: f.input.Metadata, profile: a.runtime(), headers: map[string]rootReceiptHeader{}, byHeight: map[uint64]string{}, bodies: map[string][]string{}, storageKVs: map[string]string{}, evmHex: "0x3c4", runtimeKVs: map[string]rootReceiptProfile{}, metadataKVs: map[string]string{}, counts: map[string]int{}}
	fixture.action.Scope.NativeChain, fixture.action.Scope.GenesisHash = a.Policy.NativeChain, a.Policy.GenesisHash
	anchor, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("9a", 32), a.BirthBlock, nil, false)
	fixture.headers[hash], fixture.byHeight[a.BirthBlock] = anchor, hash
	f.input.Action.BirthHash = hash
	f.input.Observation.FinalizedHash = hash
	f.input.Observation.ContentHash = ""
	f.input.Observation.ContentHash = rootObjectHash(f.input.Observation)
	f.input.Action.ObservationHash = f.input.Observation.ContentHash
	var err error
	f.config, err = prepareOwnerRecyclePlan(f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	a = f.config.Action
	signed, err := a.signed(f.signature(t))
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= count; index++ {
		var body [][]byte
		if index == 1 && included {
			body = [][]byte{{8, 4, 0}, signed}
		}
		header, next := rootReceiptHeaderFixture(t, hash, a.BirthBlock+uint64(index), body, false)
		fixture.headers[next], fixture.byHeight[a.BirthBlock+uint64(index)] = header, next
		fixture.bodies[next] = []string{}
		for _, raw := range body {
			fixture.bodies[next] = append(fixture.bodies[next], "0x"+hex.EncodeToString(raw))
		}
		hash = next
	}
	fixture.finalized = hash
	for _, row := range f.input.Observation.Storage {
		if row.RawStorage != nil {
			fixture.storageKVs[row.Key] = *row.RawStorage
		}
	}
	entries, err := ownerRecycleEntries(f.metadata, a.Owner)
	if err != nil {
		t.Fatal(err)
	}
	if included {
		fixture.storageKVs[entries["RecycleOrBurn"].key] = "0x01"
		fixture.storageKVs[entries["LastRateLimitedBlock"].key] = "0x" + hex.EncodeToString(binary.LittleEndian.AppendUint64(nil, a.BirthBlock+1))
		account, _ := hex.DecodeString(fixture.storageKVs[entries["System.Account"].key][2:])
		binary.LittleEndian.PutUint32(account, a.Nonce+1)
		fixture.storageKVs[entries["System.Account"].key] = "0x" + hex.EncodeToString(account)
	}
	owner, _ := hex.DecodeString(a.Owner[2:])
	events := []byte{8}
	events = append(events, rootReceiptEventFixture(t, f.metadata, "TransactionPayment.TransactionFeePaid", 1, owner, binary.LittleEndian.AppendUint64(nil, 12), make([]byte, 8))...)
	events = append(events, rootReceiptEventFixture(t, f.metadata, "System.ExtrinsicSuccess", 1)...)
	eventKey, err := types.CreateStorageKey(f.metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	fixture.storageKVs[eventKey.Hex()] = "0x" + hex.EncodeToString(events)
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	native, err := newRootCanonicalChain(client, identityExpectation{NativeChain: a.Policy.NativeChain, GenesisHash: a.Policy.GenesisHash, EvmChainId: mainnetEvmChainId}, []rootReceiptProfile{a.runtime()})
	if err != nil {
		t.Fatal(err)
	}
	request, err := newOwnerRecycleSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	return &ownerRecycleCanonicalChain{rootCanonicalChain: native, config: f.config, key: f.key}, fixture, request, signed
}

// Exact inclusion and readback qualify only mode, never10/90 allocation/activation.
func TestOwnerRecycleCanonicalReceiptAndModeReadback(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	chain, fixture, request, signed := ownerRecycleTestChain(t, f, 3, true)
	evidence, err := chain.reconcile(context.Background(), request, signed)
	if err != nil || evidence.Receipt == nil || !evidence.Receipt.Success || evidence.Receipt.ActualFeeRao != 12 || evidence.Readback == nil || ownerRecyclePhase(request, evidence) != "finalized" {
		t.Fatalf("exact transition not recovered: %+v %v", evidence, err)
	}
	fixture.stateLock.Lock()
	sends := fixture.counts["author_submitExtrinsic"]
	fixture.stateLock.Unlock()
	if sends != 0 {
		t.Fatal("read-only reconciliation submitted")
	}
	result := ownerRecycleRetainedResult(ownerRecycleRecord{Phase: "finalized", Reconciliation: &evidence})
	if !result.TransactionFinalized || !result.RecycleModeObserved || result.ActivationReady {
		t.Fatal("mode receipt promoted to economic activation")
	}
}

// Complete absence distinguishes expiry from a foreign nonce, not successful
// transition. Wrong anchor/body/runtime and duplicate inclusion cannot be skipped.
func TestOwnerRecycleCanonicalExpiryAndFailureRefusals(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	chain, fixture, request, signed := ownerRecycleTestChain(t, f, 8, false)
	evidence, err := chain.reconcile(context.Background(), request, signed)
	if err != nil || ownerRecyclePhase(request, evidence) != "expired" || evidence.CheckedThrough != 107 {
		t.Fatal("complete expiry failed", err)
	}
	entries, _ := ownerRecycleEntries(f.metadata, f.config.Action.Owner)
	fixture.stateLock.Lock()
	account, _ := hex.DecodeString(fixture.storageKVs[entries["System.Account"].key][2:])
	binary.LittleEndian.PutUint32(account, 8)
	fixture.storageKVs[entries["System.Account"].key] = "0x" + hex.EncodeToString(account)
	fixture.stateLock.Unlock()
	evidence, err = chain.reconcile(context.Background(), request, signed)
	if err != nil || ownerRecyclePhase(request, evidence) != "nonce-conflict" {
		t.Fatal("foreign nonce became expiry", err)
	}
	fixture.stateLock.Lock()
	fixture.bodies[fixture.byHeight[101]] = []string{"0x080400"}
	fixture.stateLock.Unlock()
	if _, err := chain.reconcile(context.Background(), request, signed); err == nil {
		t.Fatal("incomplete body was skipped")
	}
	if _, err := chain.reconcile(context.Background(), request, append(signed, 0)); err == nil {
		t.Fatal("changed signed bytes reached scan")
	}
}

// A missing readback preserves the original receipt; retry enriches only that
// exact inclusion state and cannot reissue signing or submission work.
func TestOwnerRecycleReadbackOutageAndRecovery(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	chain, fixture, request, signed := ownerRecycleTestChain(t, f, 3, true)
	entries, _ := ownerRecycleEntries(f.metadata, f.config.Action.Owner)
	fixture.fault = func(method string, params []json.RawMessage, _ int) (any, bool) {
		if method == "state_getStorage" {
			var key string
			json.Unmarshal(params[0], &key)
			if key == entries["RecycleOrBurn"].key {
				return "0x02", true
			}
		}
		return nil, false
	}
	evidence, err := chain.reconcile(context.Background(), request, signed)
	if err != nil || evidence.Receipt == nil || evidence.Readback != nil || evidence.ReadbackIssue == "" || ownerRecyclePhase(request, evidence) != "finalized-readback-pending" {
		t.Fatal("readback gap erased financial receipt", err)
	}
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	if _, err := custody.export(f.input.Metadata, f.input.LedgerMetadata); err != nil {
		t.Fatal(err)
	}
	if _, err := custody.importSignature(request.ContentHash, f.signature(t)); err != nil {
		t.Fatal(err)
	}
	result, err := custody.reconcile(context.Background(), chain)
	if err != nil || result.Phase != "finalized-readback-pending" || !result.TransactionFinalized || result.RecycleModeObserved {
		t.Fatal("unresolved state not retained", err)
	}
	fixture.stateLock.Lock()
	fixture.fault = nil
	fixture.stateLock.Unlock()
	result, err = custody.reconcile(context.Background(), chain)
	if err != nil || result.Phase != "finalized" || !result.RecycleModeObserved || result.ActivationReady {
		t.Fatal("original readback did not recover", err)
	}
	record, err := custody.load()
	if err != nil || rootObjectHash(record.Reconciliation.Receipt) != rootObjectHash(evidence.Receipt) || record.RawExtrinsic != "0x"+hex.EncodeToString(signed) {
		t.Fatal("recovery changed original financial bytes", err)
	}
}

// An inclusion-block later Burn call prevents a mode-success claim even though
// this exact setter dispatched successfully and paid its fee.
func TestOwnerRecycleLaterBurnAndDispatchFailureRemainTerminal(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	chain, fixture, request, signed := ownerRecycleTestChain(t, f, 2, true)
	entries, _ := ownerRecycleEntries(f.metadata, f.config.Action.Owner)
	fixture.storageKVs[entries["RecycleOrBurn"].key] = "0x00"
	evidence, err := chain.reconcile(context.Background(), request, signed)
	if err != nil || evidence.Receipt == nil || ownerRecyclePhase(request, evidence) != "finalized-state-conflict" {
		t.Fatal("same-block Burn admitted", err)
	}
	evidence.Receipt.Success = false
	evidence.Receipt.DispatchError = "synthetic admin window closed"
	if ownerRecyclePhase(request, evidence) != "dispatch-failed" {
		t.Fatal("dispatch failure lost")
	}
	evidence.Receipt.ActualFeeRao = f.config.Action.FeeReserveRao + 1
	if ownerRecyclePhase(request, evidence) != "fee-overrun" {
		t.Fatal("fee overrun lost")
	}
}

// Bare effect names, service routes and secret-key options carry no authority.
func TestOwnerRecyclePublicCommandRefusesEffects(t *testing.T) {
	for _, args := range [][]string{{"sign"}, {"submit"}, {"apply"}, {"service"}, {"ledger-plan", "--seed", "synthetic"}, {"observe", "--owner-account-id", "bad"}} {
		var out, errOut bytes.Buffer
		if code := runOwnerRecycleCommand(context.Background(), args, &out, &errOut); code == 0 || out.Len() != 0 {
			t.Fatal("unqualified effect command admitted", args)
		}
	}
}
