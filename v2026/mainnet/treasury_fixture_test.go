// Synthetic treasury state uses the retained identity-free runtime470 call/type
// registry, with source-shaped storage and deliberately synthetic deposit values.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// No production account, route, state or metadata constant is copied here.
func treasuryTestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	_, m := ownerRecycleTestMetadata(t)
	next := uint64(0)
	for id := range m.AsMetadataV14.EfficientLookup {
		if next <= uint64(id) {
			next = uint64(id) + 1
		}
	}
	add := func(def types.Si1TypeDef) types.Si1LookupTypeID {
		id := types.NewSi1LookupTypeIDFromUInt(next)
		next++
		value := types.Si1Type{Def: def}
		m.AsMetadataV14.EfficientLookup[id.Int64()] = &value
		m.AsMetadataV14.Lookup.Types = append(m.AsMetadataV14.Lookup.Types, types.PortableTypeV14{ID: id, Type: value})
		return id
	}
	ids := map[string]types.Si1LookupTypeID{}
	for _, primitive := range []struct {
		name string
		kind types.Si0TypeDefPrimitive
	}{{"u8", types.IsU8}, {"u16", types.IsU16}, {"u32", types.IsU32}, {"u64", types.IsU64}, {"u128", types.IsU128}, {"bool", types.IsBool}} {
		ids[primitive.name] = add(types.Si1TypeDef{IsPrimitive: true, Primitive: types.Si1TypeDefPrimitive{Si0TypeDefPrimitive: primitive.kind}})
	}
	ids["account"] = add(types.Si1TypeDef{IsArray: true, Array: types.Si1TypeDefArray{Len: 32, Type: ids["u8"]}})
	ids["accounts"] = add(types.Si1TypeDef{IsSequence: true, Sequence: types.Si1TypeDefSequence{Type: ids["account"]}})
	composite := func(names, shapes []string) types.Si1LookupTypeID {
		fields := make([]types.Si1Field, len(names))
		for i, name := range names {
			fields[i] = types.Si1Field{HasName: true, Name: types.Text(name), Type: ids[shapes[i]]}
		}
		return add(types.Si1TypeDef{IsComposite: true, Composite: types.Si1TypeDefComposite{Fields: fields}})
	}
	ids["timepoint"] = composite([]string{"height", "index"}, []string{"u32", "u32"})
	ids["pending"] = composite([]string{"when", "deposit", "depositor", "approvals"}, []string{"timepoint", "u64", "account", "accounts"})
	ids["collateral"] = composite([]string{"locked", "drain_ratio", "min_locked", "earned"}, []string{"u64", "u128", "u64", "u64"})
	entry := func(name string, keys, hashers []string, value string, optional bool) types.StorageEntryMetadataV14 {
		key := ids[keys[0]]
		if len(keys) > 1 {
			parts := make([]types.Si1LookupTypeID, len(keys))
			for i, k := range keys {
				parts[i] = ids[k]
			}
			key = add(types.Si1TypeDef{IsTuple: true, Tuple: parts})
		}
		hs := make([]types.StorageHasherV10, len(hashers))
		for i, h := range hashers {
			switch h {
			case "identity":
				hs[i].IsIdentity = true
			case "blake":
				hs[i].IsBlake2_128Concat = true
			case "twox":
				hs[i].IsTwox64Concat = true
			}
		}
		fallback := make(types.Bytes, map[string]int{"bool": 1, "u16": 2, "u64": 8, "account": 32, "accounts": 1}[value])
		if optional {
			fallback = nil
		}
		return types.StorageEntryMetadataV14{Name: types.Text(name), Modifier: types.StorageFunctionModifierV0{IsDefault: !optional, IsOptional: optional}, Fallback: fallback, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Key: key, Value: ids[value], Hashers: hs}}}
	}
	for i := range m.AsMetadataV14.Pallets {
		p := &m.AsMetadataV14.Pallets[i]
		if p.Name == "SubtensorModule" {
			upsert := func(e types.StorageEntryMetadataV14) {
				for j := range p.Storage.Items {
					if p.Storage.Items[j].Name == e.Name {
						p.Storage.Items[j] = e
						return
					}
				}
				p.Storage.Items = append(p.Storage.Items, e)
			}
			for _, item := range []struct{ name, shape string }{{name: "NetworksAdded", shape: "bool"}, {name: "SubnetworkN", shape: "u16"}, {name: "MaxAllowedUids", shape: "u16"}, {name: "NetworkRegisteredAt", shape: "u64"}, {name: "RegisteredSubnetCounter", shape: "u64"}, {name: "NetworkRegistrationAllowed", shape: "bool"}, {name: "SubnetOwner", shape: "account"}, {name: "SubnetOwnerHotkey", shape: "account"}, {name: "Burn", shape: "u64"}} {
				upsert(entry(item.name, []string{"u16"}, []string{"identity"}, item.shape, false))
			}
			upsert(entry("Owner", []string{"account"}, []string{"blake"}, "account", false))
			upsert(entry("OwnedHotkeys", []string{"account"}, []string{"blake"}, "accounts", false))
			upsert(entry("Uids", []string{"u16", "account"}, []string{"identity", "blake"}, "u16", true))
			upsert(entry("Keys", []string{"u16", "u16"}, []string{"identity", "identity"}, "account", false))
			upsert(entry("BlockAtRegistration", []string{"u16", "u16"}, []string{"identity", "identity"}, "u64", false))
			upsert(entry("IsNetworkMember", []string{"account", "u16"}, []string{"blake", "identity"}, "bool", false))
			upsert(entry("AutoStakeDestination", []string{"account", "u16"}, []string{"blake", "identity"}, "account", true))
			upsert(entry("MinerCollateral", []string{"u16", "account", "account"}, []string{"identity", "blake", "blake"}, "collateral", true))
		}
		if p.Name == "Multisig" {
			p.HasStorage = true
			p.Storage = types.StorageMetadataV14{Prefix: "Multisig", Items: []types.StorageEntryMetadataV14{entry("Multisigs", []string{"account", "account"}, []string{"twox", "blake"}, "pending", true)}}
			p.Constants = []types.ConstantMetadataV14{{Name: "DepositBase", Type: ids["u64"], Value: binary.LittleEndian.AppendUint64(nil, 10)}, {Name: "DepositFactor", Type: ids["u64"], Value: binary.LittleEndian.AppendUint64(nil, 5)}, {Name: "MaxSignatories", Type: ids["u32"], Value: binary.LittleEndian.AppendUint32(nil, 100)}}
		}
	}
	raw, err := codec.Encode(*m)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "0x" + hex.EncodeToString(raw)
	m, _, err = crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return m, encoded
}

// The fixture owns one original action, synthetic signers and real private files.
type treasuryFixture struct {
	input    treasuryPlanInput
	config   treasuryConfig
	key      string
	approval ed25519.PrivateKey
	signer   ed25519.PrivateKey
	metadata *types.Metadata
	values   map[string]string
	storage  *durablefixture.Fixture
}

// Stable synthetic keys make exact public protocol vectors reproducible.
func treasuryTestKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("synthetic treasury " + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

// Values use authenticated metadata keys but synthetic native state only.
func (f *treasuryFixture) set(t *testing.T, pallet, name string, raw []byte, args ...[]byte) string {
	t.Helper()
	key, err := types.CreateStorageKey(f.metadata, pallet, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	f.values[key.Hex()] = "0x" + hex.EncodeToString(raw)
	return key.Hex()
}

// Observation creation exercises the production row consumer, not a fake verdict.
func (f *treasuryFixture) observation(t *testing.T, a treasuryAction, number uint64, hash string) treasuryObservation {
	t.Helper()
	o := treasuryObservation{Schema: treasuryObservationSchema, ScopeHash: treasuryObservationScope(a), FinalizedNumber: number, FinalizedHash: hash}
	seen := map[string]bool{}
	_, err := treasuryReadFacts(context.Background(), a, f.metadata, number, func(_ context.Context, key string, fallback []byte) ([]byte, bool, error) {
		v, ok := f.values[key]
		if !seen[key] {
			row := treasuryStorageRow{Key: key}
			if ok {
				row.Raw = &v
			}
			o.Rows = append(o.Rows, row)
			seen[key] = true
		}
		if !ok {
			return fallback, false, nil
		}
		raw, err := hex.DecodeString(v[2:])
		return raw, true, err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(o.Rows, func(i, j int) bool { return o.Rows[i].Key < o.Rows[j].Key })
	o.ContentHash = rootObjectHash(o)
	return o
}

// All authority keys are test-only. The native inner call is the real codec.
func newTreasuryFixture(t *testing.T) *treasuryFixture {
	t.Helper()
	metadata, encoded := treasuryTestMetadata(t)
	f := &treasuryFixture{metadata: metadata, values: map[string]string{}, approval: treasuryTestKey("independent approval"), signer: treasuryTestKey("signatory one")}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	d := treasuryDescriptor{Schema: treasuryCustodySchema, Profile: "mainnet", Netuid: 25, GenesisHash: "0x" + strings.Repeat("16", 32)}
	d.Multisig.Threshold = 2
	for _, key := range []ed25519.PrivateKey{f.signer, treasuryTestKey("signatory two"), treasuryTestKey("signatory three")} {
		d.Multisig.Signatories = append(d.Multisig.Signatories, treasurySignatory{AccountId: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), SignatureScheme: "ed25519", DeviceConfig: treasuryDeviceReference{Path: filepath.Join(directory, "public-device.json"), Bytes: 1, Sha256: "0x" + strings.Repeat("17", 32)}})
	}
	sort.Slice(d.Multisig.Signatories, func(i, j int) bool { return d.Multisig.Signatories[i].AccountId < d.Multisig.Signatories[j].AccountId })
	accounts := make([][32]byte, len(d.Multisig.Signatories))
	for i, s := range d.Multisig.Signatories {
		raw, _ := hex.DecodeString(s.AccountId[2:])
		copy(accounts[i][:], raw)
	}
	derived, err := crv4.DeriveNativeMultisigAccount(accounts, 2)
	if err != nil {
		t.Fatal(err)
	}
	d.Multisig.AccountId = "0x" + hex.EncodeToString(derived[:])
	for _, label := range []string{"recipient one", "recipient two"} {
		key := treasuryTestKey(label)
		d.RecipientHotkeys = append(d.RecipientHotkeys, treasuryHotkey{AccountId: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), DeviceConfig: treasuryDeviceReference{Path: filepath.Join(directory, "hotkey-device.json"), Bytes: 1, Sha256: "0x" + strings.Repeat("18", 32)}})
	}
	sort.Slice(d.RecipientHotkeys, func(i, j int) bool { return d.RecipientHotkeys[i].AccountId < d.RecipientHotkeys[j].AccountId })
	ledger := "0x6d6574610f00"
	ledgerHash, _ := ownerLedgerMetadataHash(ledger)
	raw, _ := hex.DecodeString(encoded[2:])
	a := treasuryAction{Schema: treasuryActionSchema, Policy: treasuryChainPolicy{NativeChain: "synthetic-treasury-chain", GenesisHash: d.GenesisHash, EvmChainId: mainnetEvmChainId, rootReceiptProfile: rootReceiptProfile{RuntimeSourceCommit: rootPassiveSource, RuntimeVersion: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1}, RuntimeCodeHash: "0x" + strings.Repeat("26", 32), RuntimeMetadataHash: rootExtrinsicHash(raw)}}, Descriptor: d, ReviewHash: rootObjectHash("synthetic treasury review"), StatePath: filepath.Join(directory, treasuryStateFile), CustodyId: "synthetic-treasury", Owner: "0x" + hex.EncodeToString(f.signer.Public().(ed25519.PublicKey)), SubnetRegistrationBlock: 10, SubnetGeneration: 3, Nonce: 7, BirthBlock: 100, BirthHash: "0x" + strings.Repeat("36", 32), Period: 8, FeeReserveRao: 200, DepositLimitRao: 20, Operation: "approve_as_multi", Inner: treasuryInnerCall{Kind: "register_limit", Hotkey: d.RecipientHotkeys[0].AccountId, LimitPrice: 100}, MaxRefTime: 1000000000, MaxProofSize: 65536, SignatureScheme: "ed25519", MetadataDigest: "0x" + strings.Repeat("46", 32), DerivationPath: "m/44'/354'/0'/0'/0'", LedgerMetadataHash: ledgerHash}
	net := []byte{25, 0}
	for name, value := range map[string][]byte{"NetworksAdded": {1}, "SubnetworkN": {0, 0}, "MaxAllowedUids": {16, 0}, "NetworkRegisteredAt": binary.LittleEndian.AppendUint64(nil, 10), "RegisteredSubnetCounter": binary.LittleEndian.AppendUint64(nil, 3), "NetworkRegistrationAllowed": {1}, "SubnetOwner": bytes.Repeat([]byte{71}, 32), "SubnetOwnerHotkey": bytes.Repeat([]byte{72}, 32), "Burn": binary.LittleEndian.AppendUint64(nil, 90)} {
		f.set(t, "SubtensorModule", name, value, net)
	}
	for _, account := range []string{a.Owner, d.Multisig.AccountId} {
		id, _ := hex.DecodeString(account[2:])
		data := make([]byte, 56)
		binary.LittleEndian.PutUint32(data, a.Nonce)
		binary.LittleEndian.PutUint64(data[16:], 100000)
		f.set(t, "System", "Account", data, id)
	}
	o := f.observation(t, a, a.BirthBlock, a.BirthHash)
	a.ObservationHash = o.ContentHash
	f.input = treasuryPlanInput{Action: a, Observation: o, Metadata: encoded, LedgerMetadata: ledger, Route: ownedSubmissionRoute{RpcUrl: "http://192.0.2.25:9944", ReadRetrySeconds: 60, SendTimeoutSeconds: 1}, MaximumPosts: 1, AuthorityHash: rootObjectHash("synthetic production authority")}
	f.key = "0x" + hex.EncodeToString(f.approval.Public().(ed25519.PublicKey))
	f.replan(t)
	prepareMainnetSnapshotTest(t, a.StatePath, "mainnet-native-treasury", treasuryStoreLimit)
	f.storage = durablefixture.New(t, t.Context(), directory)
	return f
}

// Fresh fixture mutations require a fresh synthetic independent signature.
func (f *treasuryFixture) replan(t *testing.T) {
	t.Helper()
	var err error
	f.config, err = prepareTreasuryPlan(t.Context(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Signature = hex.EncodeToString(ed25519.Sign(f.approval, f.config.signingBytes()))
}

// Materialize the actual public request without bypassing metadata authentication.
func (f *treasuryFixture) request(t *testing.T) treasurySigningRequest {
	t.Helper()
	r, err := newTreasurySigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Exact JSON file helpers retain the production unknown-field and size checks.
func treasuryTestJson(t *testing.T, dir, name string, value any) (string, string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return ownerRecycleTestFile(t, dir, name, raw)
}
