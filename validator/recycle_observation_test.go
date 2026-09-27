// Synthetic approved metadata and exact storage rows exercise the real reader.
// Approval keys, native identities and RPC results confer no mainnet authority.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/crv4"
)

// Uses the reviewed map hashers and SCALE type shapes, with portable ids that
// have no relationship to any captured production metadata.
func recycleAdmissionTestMetadata(t *testing.T, mutate func(*types.Metadata)) (string, string, *types.Metadata) {
	t.Helper()
	id := types.NewSi1LookupTypeIDFromUInt
	primitive := func(kind types.Si0TypeDefPrimitive) types.Si1TypeDef {
		return types.Si1TypeDef{IsPrimitive: true, Primitive: types.Si1TypeDefPrimitive{Si0TypeDefPrimitive: kind}}
	}
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	metadata.AsMetadataV14.Lookup.Types = []types.PortableTypeV14{
		{ID: id(0), Type: types.Si1Type{Def: primitive(types.IsU8)}},
		{ID: id(1), Type: types.Si1Type{Def: primitive(types.IsU16)}},
		{ID: id(2), Type: types.Si1Type{Def: primitive(types.IsU64)}},
		{ID: id(3), Type: types.Si1Type{Def: types.Si1TypeDef{IsArray: true, Array: types.Si1TypeDefArray{Len: 32, Type: id(0)}}}},
		{ID: id(4), Type: types.Si1Type{Def: types.Si1TypeDef{IsComposite: true, Composite: types.Si1TypeDefComposite{Fields: []types.Si1Field{{Type: id(3)}}}}}},
		{ID: id(5), Type: types.Si1Type{Def: types.Si1TypeDef{IsComposite: true, Composite: types.Si1TypeDefComposite{Fields: []types.Si1Field{{Type: id(1)}}}}}},
		{ID: id(6), Type: types.Si1Type{Def: types.Si1TypeDef{IsComposite: true, Composite: types.Si1TypeDefComposite{Fields: []types.Si1Field{{Type: id(0)}}}}}},
		{ID: id(7), Type: types.Si1Type{Def: types.Si1TypeDef{IsSequence: true, Sequence: types.Si1TypeDefSequence{Type: id(4)}}}},
		{ID: id(8), Type: types.Si1Type{Def: types.Si1TypeDef{IsTuple: true, Tuple: types.Si1TypeDefTuple{id(5), id(1)}}}},
		{ID: id(9), Type: types.Si1Type{Def: types.Si1TypeDef{IsTuple: true, Tuple: types.Si1TypeDefTuple{id(5), id(4)}}}},
		{ID: id(10), Type: types.Si1Type{Def: types.Si1TypeDef{IsVariant: true, Variant: types.Si1TypeDefVariant{Variants: []types.Si1Variant{{Name: "Burn", Index: 0}, {Name: "Recycle", Index: 1}}}}}},
	}
	identity := types.StorageHasherV10{IsIdentity: true}
	blake := types.StorageHasherV10{IsBlake2_128Concat: true}
	twox := types.StorageHasherV10{IsTwox64Concat: true}
	var entries []types.StorageEntryMetadataV14
	for _, item := range []struct {
		name       string
		key, value uint64
		hashers    []types.StorageHasherV10
		optional   bool
		fallback   []byte
	}{
		{name: "SubnetOwner", key: 5, value: 4, hashers: []types.StorageHasherV10{identity}, fallback: make([]byte, 32)},
		{name: "OwnedHotkeys", key: 4, value: 7, hashers: []types.StorageHasherV10{blake}, fallback: []byte{0}},
		{name: "SubnetOwnerHotkey", key: 5, value: 4, hashers: []types.StorageHasherV10{identity}, fallback: make([]byte, 32)},
		{name: "RecycleOrBurn", key: 5, value: 10, hashers: []types.StorageHasherV10{identity}, fallback: []byte{0}},
		{name: "SubnetworkN", key: 5, value: 1, hashers: []types.StorageHasherV10{identity}, fallback: []byte{0, 0}},
		{name: "Keys", key: 8, value: 4, hashers: []types.StorageHasherV10{identity, identity}, fallback: make([]byte, 32)},
		{name: "Uids", key: 9, value: 1, hashers: []types.StorageHasherV10{identity, blake}, optional: true, fallback: []byte{0}},
		{name: "BlockAtRegistration", key: 8, value: 2, hashers: []types.StorageHasherV10{identity, identity}, fallback: make([]byte, 8)},
		{name: "MechanismCountCurrent", key: 5, value: 6, hashers: []types.StorageHasherV10{twox}, fallback: []byte{1}},
		{name: "SubnetEpochIndex", key: 5, value: 2, hashers: []types.StorageHasherV10{identity}, fallback: make([]byte, 8)},
		{name: "MinAllowedWeights", key: 5, value: 1, hashers: []types.StorageHasherV10{identity}, fallback: []byte{1, 0}},
		{name: "MaxWeightsLimit", key: 5, value: 1, hashers: []types.StorageHasherV10{identity}, fallback: []byte{255, 255}},
	} {
		entries = append(entries, types.StorageEntryMetadataV14{Name: types.Text(item.name), Modifier: types.StorageFunctionModifierV0{IsDefault: !item.optional, IsOptional: item.optional}, Fallback: item.fallback,
			Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Key: id(item.key), Value: id(item.value), Hashers: item.hashers}}})
	}
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{Name: "SubtensorModule", HasStorage: true, Storage: types.StorageMetadataV14{Prefix: "SubtensorModule", Items: entries}}}
	if mutate != nil {
		mutate(metadata)
	}
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	decoded, digest, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, digest, decoded
}

// One instance owns its RPC transcript and approval material. Tests mutate it
// only before a synchronous observation, or across an explicit joined barrier.
type recycleAdmissionFixture struct {
	cfg        *ReleaseConfig
	chain      *crv4.Chain
	approval   OwnerRecycleApproval
	private    ed25519.PrivateKey
	raw        []byte
	metadata   string
	storage    map[string]any
	keys       map[string]string
	finalized  types.Hash
	headHash   types.Hash
	headNumber uint64
	canonical  types.Hash
	calls      []string
	before     func(context.Context, string, []any) error
	chainName  string
	evmChainId string
	version    crv4.RuntimeVersionIdentity
	codeHash   string
}

// Signs only generated fixture authority and writes private bounded inputs.
func newRecycleAdmissionFixture(t *testing.T, mutate func(*types.Metadata)) *recycleAdmissionFixture {
	t.Helper()
	metadataHex, metadataHash, metadata := recycleAdmissionTestMetadata(t, mutate)
	input := recycleTestInput(t)
	cfg := &ReleaseConfig{SchemaVersion: 1, Production: true, Release: "1.0", DeploymentID: "synthetic-recycle", ValidatorID: 1,
		ChainID: 964, Netuid: 25, GenesisHash: releaseHex32(input.Proposal.Runtime.GenesisHash), RuntimeSpec: 471, TransactionVersion: 1, StateVersion: 1,
		RuntimeCodeHash: releaseHex32(input.Proposal.Runtime.CodeHash), RuntimeMetadataHash: metadataHash,
		Coordinator: "0x1111111111111111111111111111111111111111", SettlementVault: "0x2222222222222222222222222222222222222222",
		Substrate: []string{"wss://recycle.example"}, RPC: []string{"https://recycle.example"},
		DeployBlock: 1, Policy: input.ParentPolicy, StateDir: t.TempDir(), HotkeySeedFile: "/synthetic/missing-signer.seed"}
	if err := cfg.normalize(filepath.Dir(cfg.StateDir)); err != nil {
		t.Fatal(err)
	}
	cfg.PolicyHash, _ = cfg.Policy.HashHex()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x39}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	cfg.OwnerRecycleApproval = &ReleaseOwnerRecycleApprovalConfig{Approval: ReleaseEvidenceV2File{Path: filepath.Join(t.TempDir(), "approval.json")}, Signer: "0x" + hex.EncodeToString(public)}
	for _, directory := range []string{cfg.StateDir, filepath.Dir(cfg.OwnerRecycleApproval.Approval.Path)} {
		if err := os.Chmod(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	proposal := input.Proposal
	proposal.Runtime.Version.SpecName = "node-subtensor"
	proposal.Runtime.MetadataHash, _ = parseHash32("synthetic metadata", metadataHash)
	configHash, err := OwnerRecycleConfigHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &recycleAdmissionFixture{cfg: cfg, private: private, metadata: metadataHex, storage: map[string]any{}, keys: map[string]string{}, finalized: types.Hash(recycleTestId(1003)),
		chainName: "Synthetic Mainnet", evmChainId: "0x3c4", version: proposal.Runtime.Version, codeHash: cfg.RuntimeCodeHash,
		approval: OwnerRecycleApproval{Schema: ownerRecycleApprovalSchema, ConfigHash: configHash, Proposal: proposal,
			NativeChain: "Synthetic Mainnet", RuntimeReviewHash: recycleTestId(1010), ValidatorHotkey: recycleTestId(4), SubnetOwner: recycleTestId(1011),
			OwnerHotkeys: [][32]byte{recycleTestId(2), recycleTestId(3)}, FirstNativeEpoch: 20, ValidFromNativeBlock: 90, ValidThroughNativeBlock: 110, MaximumSubnetUids: 6, MaximumOwnedHotkeys: 16}}
	put := func(label, name string, value []byte, args ...[]byte) {
		key, err := types.CreateStorageKey(metadata, "SubtensorModule", name, args...)
		if err != nil {
			t.Fatal(err)
		}
		fixture.keys[label] = key.Hex()
		if value == nil {
			fixture.storage[key.Hex()] = nil
		} else {
			fixture.storage[key.Hex()] = codec.HexEncodeToString(value)
		}
	}
	netuid := binary.LittleEndian.AppendUint16(nil, 25)
	put("mode", "RecycleOrBurn", []byte{1}, netuid)
	put("owner", "SubnetOwner", fixture.approval.SubnetOwner[:], netuid)
	put("explicit", "SubnetOwnerHotkey", nil, netuid)
	owned := append([]byte{8}, fixture.approval.OwnerHotkeys[0][:]...)
	owned = append(owned, fixture.approval.OwnerHotkeys[1][:]...)
	put("owned", "OwnedHotkeys", owned, fixture.approval.SubnetOwner[:])
	put("mechanisms", "MechanismCountCurrent", []byte{1}, netuid)
	put("epoch", "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 19), netuid)
	put("count", "SubnetworkN", []byte{6, 0}, netuid)
	put("minimum", "MinAllowedWeights", []byte{2, 0}, netuid)
	put("cap", "MaxWeightsLimit", []byte{1, 0}, netuid)
	for uid := uint16(0); uid < 6; uid++ {
		hotkey := recycleTestId(uid)
		uidRaw := binary.LittleEndian.AppendUint16(nil, uid)
		put(fmt.Sprintf("key-%d", uid), "Keys", hotkey[:], netuid, uidRaw)
		put(fmt.Sprintf("uid-%d", uid), "Uids", uidRaw, netuid, hotkey[:])
		put(fmt.Sprintf("registered-%d", uid), "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, uint64(10+uid)), netuid, uidRaw)
	}
	assign := func(target any, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, target)
	}
	client := &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		fixture.calls = append(fixture.calls, method)
		if fixture.before != nil {
			if err := fixture.before(ctx, method, args); err != nil {
				return err
			}
		}
		switch method {
		case "system_chain":
			return assign(target, fixture.chainName)
		case "eth_chainId":
			return assign(target, fixture.evmChainId)
		case "chain_getFinalizedHead":
			if fixture.headHash != (types.Hash{}) {
				return assign(target, fixture.headHash)
			}
			return assign(target, fixture.finalized)
		case "chain_getBlockHash":
			if len(args) != 1 {
				return errors.New("synthetic canonical query shape changed")
			}
			if args[0] == uint64(0) {
				return assign(target, types.Hash(proposal.Runtime.GenesisHash))
			}
			if args[0] == uint64(100) {
				if fixture.canonical != (types.Hash{}) {
					return assign(target, fixture.canonical)
				}
				return assign(target, fixture.finalized)
			}
			return errors.New("synthetic canonical query height changed")
		case "chain_getHeader":
			if fixture.headHash != (types.Hash{}) && len(args) == 1 && args[0] == fixture.headHash.Hex() {
				return assign(target, types.Header{Number: types.BlockNumber(fixture.headNumber)})
			}
			if len(args) != 1 || args[0] != fixture.finalized.Hex() {
				return errors.New("synthetic header lost finalized hash")
			}
			return assign(target, types.Header{Number: 100})
		case "state_getRuntimeVersion", "state_getMetadata":
			if len(args) != 1 || args[0] != fixture.finalized.Hex() {
				return errors.New("synthetic runtime lost finalized hash")
			}
			if method == "state_getMetadata" {
				return assign(target, fixture.metadata)
			}
			return assign(target, fixture.version)
		case "state_getStorageHash":
			if len(args) != 2 || args[0] != "0x3a636f6465" || args[1] != fixture.finalized.Hex() {
				return errors.New("synthetic code lost finalized hash")
			}
			return assign(target, fixture.codeHash)
		case "state_getStorage":
			if len(args) != 2 || args[1] != fixture.finalized.Hex() {
				return errors.New("synthetic storage lost finalized hash")
			}
			value, exists := fixture.storage[args[0].(string)]
			if !exists {
				return errors.New("synthetic storage key escaped approved census")
			}
			return assign(target, value)
		default:
			return fmt.Errorf("unexpected owner-recycle RPC %s", method)
		}
	}}
	fixture.chain = &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: &recycleAdmissionRouteClient{validatorRuntimeIdentityTestClient: client, route: cfg.Substrate[0]}}, GenesisHash: types.Hash(proposal.Runtime.GenesisHash)}
	fixture.sign(t)
	return fixture
}

// The connection identifies the exact externally approved fixture route.
type recycleAdmissionRouteClient struct {
	*validatorRuntimeIdentityTestClient
	route string
}

// No public endpoint is selected implicitly by the observer.
func (self *recycleAdmissionRouteClient) URL() string {
	return self.route
}

// External signing is explicit in fixtures; production never owns this key.
func (self *recycleAdmissionFixture) sign(t *testing.T) {
	t.Helper()
	message, err := self.approval.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope := OwnerRecycleApprovalEnvelope{Approval: self.approval, Signature: hex.EncodeToString(ed25519.Sign(self.private, message))}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	self.raw = append(raw, '\n')
	self.cfg.OwnerRecycleApproval.Approval.Bytes = uint64(len(self.raw))
	self.cfg.OwnerRecycleApproval.Approval.SHA256 = attemptHex32(sha256.Sum256(self.raw))
	if err := os.WriteFile(self.cfg.OwnerRecycleApproval.Approval.Path, self.raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// A real immutable writer must complete before any finalized RPC observation.
func (self *recycleAdmissionFixture) retain(t *testing.T) {
	t.Helper()
	if _, err := RetainOwnerRecycleApproval(t.Context(), self.cfg); err != nil {
		t.Fatal(err)
	}
}

// Approved storage authority proves two recognized destinations, while every
// activation/outcome flag and unproved validator/operator census stays false.
func TestOwnerRecycleAdmissionAuthenticatesDurableApprovalAndCompleteCensus(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	steerer := &ReleaseSteerer{cfg: fixture.cfg, native: fixture.chain}
	observation, err := steerer.ObserveOwnerRecycleAdmission(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !observation.ApprovalAuthenticated || !observation.OwnerCensusAuthenticated || !observation.RecycleModeAuthenticated ||
		observation.ActivationReady || observation.NativeOutcomeVerified || len(observation.Blockers) != 4 || len(observation.Snapshot.LiveValidatorUids) != 0 || len(observation.Snapshot.HealthyOperators) != 0 {
		t.Fatalf("admission overstated authority: %+v", observation)
	}
	if len(observation.Snapshot.Registrations) != 6 || len(observation.RecognizedOwners) != 2 || observation.RecognizedOwners[0].Uid != 3 || observation.RecognizedOwners[1].Uid != 2 ||
		observation.Snapshot.FinalizedHash != [32]byte(fixture.finalized) || observation.NativeEpoch != 19 || observation.FirstNativeEpoch != 20 {
		t.Fatalf("owner census differs: %+v", observation)
	}
	if observation.StoredMaximumWeightLimit != 1 || observation.RuntimeMaximumWeightLimit != 65535 || observation.SignedMaximumWeightLimit != fixture.cfg.Policy.Steering.MaxWeightLimitU16 {
		t.Fatal("storage cap replaced source getter or signed policy cap")
	}
	if err := steerer.SubmitOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "activation is blocked") {
		t.Fatal("authenticated census authorized an unmigrated production vector")
	}
}

// Absence or changed custody is a hard stop before the first chain read.
func TestOwnerRecycleAdmissionRequiresRetainedExactApproval(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	if result, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain); err == nil || result != nil || len(fixture.calls) != 0 {
		t.Fatal("unretained approval reached chain")
	}
	fixture.retain(t)
	path := filepath.Join(fixture.cfg.StateDir, retainedOwnerRecycleApprovalName)
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain); err == nil || result != nil || len(fixture.calls) != 0 {
		t.Fatal("changed retained approval reached chain")
	}
}

// Both restart and idempotent install use original bytes. Signed replacements
// still need a history migration and cannot overwrite the fixed approval leaf.
func TestOwnerRecycleApprovalCustodyIsImmutableAndRestartable(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	fixture.retain(t)
	original := bytes.Clone(fixture.raw)
	if err := os.Remove(fixture.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain); err != nil {
		t.Fatal(err)
	}
	fixture.approval.FirstNativeEpoch++
	fixture.sign(t)
	if _, err := RetainOwnerRecycleApproval(t.Context(), fixture.cfg); err == nil {
		t.Fatal("signed replacement overwrote retained authority")
	}
	actual, err := os.ReadFile(filepath.Join(fixture.cfg.StateDir, retainedOwnerRecycleApprovalName))
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("replacement destroyed original approval")
	}
}

// A matching declared signature cannot inherit a different route/config,
// parent cap, signer, runtime review, owner set or native activation window.
func TestOwnerRecycleApprovalRejectsChangedScopeBeforeMutation(t *testing.T) {
	for _, change := range []string{"config", "signer", "review", "owners", "self", "window", "testnet", "genesis"} {
		fixture := newRecycleAdmissionFixture(t, nil)
		switch change {
		case "config":
			fixture.cfg.ControlledNOIDs = []uint64{2}
		case "signer":
			fixture.cfg.OwnerRecycleApproval.Signer = releaseHex32(recycleTestId(90))
		case "review":
			fixture.approval.RuntimeReviewHash = [32]byte{}
			fixture.sign(t)
		case "owners":
			fixture.approval.OwnerHotkeys = fixture.approval.OwnerHotkeys[:1]
			fixture.sign(t)
		case "self":
			fixture.approval.OwnerHotkeys[1] = fixture.approval.ValidatorHotkey
			fixture.sign(t)
		case "window":
			fixture.approval.ValidThroughNativeBlock = 1
			fixture.sign(t)
		case "testnet":
			fixture.cfg.ChainID = 945
		case "genesis":
			fixture.cfg.GenesisHash = provisionalRuntimeTestnetGenesis
		}
		if _, err := RetainOwnerRecycleApproval(t.Context(), fixture.cfg); err == nil {
			t.Errorf("accepted %s", change)
		}
		if _, err := os.Stat(filepath.Join(fixture.cfg.StateDir, retainedOwnerRecycleApprovalName)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s mutated custody: %v", change, err)
		}
	}
}

// Unknown, duplicate, trailing and altered signature bytes never get persisted.
func TestOwnerRecycleApprovalRejectsAmbiguousEnvelope(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	for _, raw := range [][]byte{
		append(append([]byte(nil), fixture.raw...), []byte("{}")...),
		bytes.Replace(fixture.raw, []byte(`"signature":`), []byte(`"unknown":0,"signature":`), 1),
		bytes.Replace(fixture.raw, []byte(`"signature":`), []byte(`"signature":"00","signature":`), 1),
		bytes.Replace(fixture.raw, []byte(`"first_native_epoch":20`), []byte(`"first_native_epoch":21`), 1),
	} {
		if _, err := decodeOwnerRecycleApproval(fixture.cfg, raw); err == nil {
			t.Fatal("ambiguous or forged approval accepted")
		}
	}
}

// A current Recycle value cannot compensate for wrong chain, artifact,
// registration, owner identity, source mode or a missed signed epoch.
func TestOwnerRecycleAdmissionRejectsRuntimeAndCensusDrift(t *testing.T) {
	for _, change := range []string{"chain", "evm", "version", "code", "burn", "absent", "owner", "reverse", "duplicate", "registration", "owners", "mechanisms", "epoch", "count"} {
		fixture := newRecycleAdmissionFixture(t, nil)
		fixture.retain(t)
		switch change {
		case "chain":
			fixture.chainName = "Synthetic Other"
		case "evm":
			fixture.evmChainId = "0x3b1"
		case "version":
			fixture.version.SpecVersion++
		case "code":
			fixture.codeHash = releaseHex32(recycleTestId(2010))
		case "burn":
			fixture.storage[fixture.keys["mode"]] = "0x00"
		case "absent":
			fixture.storage[fixture.keys["mode"]] = nil
		case "owner":
			fixture.storage[fixture.keys["owner"]] = releaseHex32(recycleTestId(2011))
		case "reverse":
			fixture.storage[fixture.keys["uid-2"]] = "0x0300"
		case "duplicate":
			fixture.storage[fixture.keys["key-3"]] = releaseHex32(recycleTestId(2))
		case "registration":
			fixture.storage[fixture.keys["registered-2"]] = codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, 101))
		case "owners":
			fixture.storage[fixture.keys["owned"]] = codec.HexEncodeToString(append([]byte{4}, fixture.approval.OwnerHotkeys[0][:]...))
		case "mechanisms":
			fixture.storage[fixture.keys["mechanisms"]] = "0x02"
		case "epoch":
			fixture.storage[fixture.keys["epoch"]] = codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, 21))
		case "count":
			fixture.storage[fixture.keys["count"]] = "0x0700"
		}
		if result, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain); err == nil || result != nil {
			t.Errorf("accepted %s: %+v %v", change, result, err)
		}
	}
}

// Source recognition includes an explicit registered owner hotkey even if it
// is not in OwnedHotkeys, but never counts a duplicate destination twice.
func TestOwnerRecycleAdmissionAuthenticatesExplicitOwnerWithoutDuplication(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		fixture := newRecycleAdmissionFixture(t, nil)
		fixture.retain(t)
		fixture.storage[fixture.keys["explicit"]] = releaseHex32(fixture.approval.OwnerHotkeys[1])
		if !duplicate {
			fixture.storage[fixture.keys["owned"]] = codec.HexEncodeToString(append([]byte{4}, fixture.approval.OwnerHotkeys[0][:]...))
		}
		result, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain)
		if err != nil || len(result.RecognizedOwners) != 2 {
			t.Fatalf("duplicate=%v: %+v %v", duplicate, result, err)
		}
	}
}

// Even independently signed exact bytes cannot change the consumed source
// interface while claiming its original profile and semantics.
func TestOwnerRecycleAdmissionRejectsApprovedIncompatibleMetadata(t *testing.T) {
	for _, change := range []string{"mode", "default", "hasher", "width", "duplicate"} {
		fixture := newRecycleAdmissionFixture(t, func(metadata *types.Metadata) {
			switch change {
			case "mode":
				metadata.AsMetadataV14.Lookup.Types[10].Type.Def.Variant.Variants[1].Index = 2
			case "default":
				metadata.AsMetadataV14.Pallets[0].Storage.Items[3].Fallback = []byte{1}
			case "hasher":
				metadata.AsMetadataV14.Pallets[0].Storage.Items[8].Type.AsMap.Hashers[0] = types.StorageHasherV10{IsIdentity: true}
			case "width":
				metadata.AsMetadataV14.Lookup.Types[2].Type.Def.Primitive.Si0TypeDefPrimitive = types.IsU32
			case "duplicate":
				metadata.AsMetadataV14.Pallets[0].Storage.Items = append(metadata.AsMetadataV14.Pallets[0].Storage.Items, metadata.AsMetadataV14.Pallets[0].Storage.Items[0])
			}
		})
		fixture.retain(t)
		if result, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain); err == nil || result != nil {
			t.Errorf("accepted %s metadata", change)
		}
		for _, call := range fixture.calls {
			if call == "state_getStorage" {
				t.Errorf("%s reached storage with incompatible metadata", change)
			}
		}
	}
}

// The final canonical check detects a provider changing the block during an
// otherwise valid census; no partially authenticated observation escapes.
func TestOwnerRecycleAdmissionRejectsCanonicalRetarget(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	canonicalCalls := 0
	fixture.before = func(_ context.Context, method string, args []any) error {
		if method == "chain_getBlockHash" && args[0] == uint64(100) {
			canonicalCalls++
			if canonicalCalls == 2 {
				fixture.finalized = types.Hash{99}
			}
		}
		return nil
	}
	if result, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain); err == nil || result != nil || canonicalCalls != 2 {
		t.Fatalf("retarget: %+v %v calls=%d", result, err, canonicalCalls)
	}
}

// Cancellation crosses the actual caller context at a stalled storage read.
func TestOwnerRecycleAdmissionCancelsStorageWithoutPartialObservation(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	started := make(chan struct{})
	fixture.before = func(ctx context.Context, method string, _ []any) error {
		if method == "state_getStorage" {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		result, err := ObserveOwnerRecycleAdmission(ctx, fixture.cfg, fixture.chain)
		if result != nil {
			err = errors.New("canceled observation escaped")
		}
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

// Length prefixes and physical storage bytes are bounded before any census
// allocation, and malformed absence cannot turn Burn into explicit Recycle.
func TestOwnerRecycleAdmissionBoundsMalformedStorage(t *testing.T) {
	for _, change := range []string{"empty", "trailing", "huge", "prefix", "duplicate", "zero"} {
		fixture := newRecycleAdmissionFixture(t, nil)
		fixture.retain(t)
		switch change {
		case "empty":
			fixture.storage[fixture.keys["mode"]] = "0x"
		case "trailing":
			fixture.storage[fixture.keys["mode"]] = "0x0100"
		case "huge":
			fixture.storage[fixture.keys["owned"]] = "0x" + strings.Repeat("01", 600)
		case "prefix":
			fixture.storage[fixture.keys["owned"]] = "0x0300"
		case "duplicate":
			raw := append([]byte{8}, fixture.approval.OwnerHotkeys[0][:]...)
			raw = append(raw, fixture.approval.OwnerHotkeys[0][:]...)
			fixture.storage[fixture.keys["owned"]] = codec.HexEncodeToString(raw)
		case "zero":
			fixture.storage[fixture.keys["owned"]] = codec.HexEncodeToString(append([]byte{4}, make([]byte, 32)...))
		}
		if result, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain); err == nil || result != nil {
			t.Errorf("accepted %s malformed storage", change)
		}
	}
}
