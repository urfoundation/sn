// Synthetic Ledger signatories exercise the native multisig owner codec against
// the retained identity-free runtime 470 projection. No device, chain or key
// leaves the test; the public ur-owner vector is used only for derivation.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ss58"
	"golang.org/x/crypto/blake2b"
)

// Publicly reproducible seeds; strictly synthetic test signatories.
func ownerTrimMultisigTestKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("synthetic owner multisig signatory " + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

// Sorted synthetic signer set, the key behind each account and its owner.
type ownerTrimMultisigTestSigners struct {
	accounts []string
	keys     map[string]ed25519.PrivateKey
	owner    string
}

// The derived owner replaces a fixture's subnet owner wherever it is used.
func newOwnerTrimMultisigTestSigners(t *testing.T) ownerTrimMultisigTestSigners {
	t.Helper()
	result := ownerTrimMultisigTestSigners{keys: map[string]ed25519.PrivateKey{}}
	for _, label := range []string{"one", "two", "three"} {
		key := ownerTrimMultisigTestKey(label)
		account := "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))
		result.keys[account] = key
		result.accounts = append(result.accounts, account)
	}
	sort.Strings(result.accounts)
	raw := make([][32]byte, len(result.accounts))
	for index, account := range result.accounts {
		decoded, _ := hex.DecodeString(account[2:])
		copy(raw[index][:], decoded)
	}
	derived, err := crv4.DeriveNativeMultisigAccount(raw, 2)
	if err != nil {
		t.Fatal(err)
	}
	result.owner = "0x" + hex.EncodeToString(derived[:])
	return result
}

// The retained runtime 470 projection keeps the Multisig/AdminUtils calls,
// events and type registry; storage and constants are restored synthetically
// with the reviewed public deposit values (132,000,000 and 32,000,000 rao).
func ownerTrimMultisigTestMetadata(t *testing.T) (*types.Metadata, string) {
	t.Helper()
	metadata, _ := treasuryTestMetadata(t)
	for index := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[index]
		if pallet.Name != "Multisig" {
			continue
		}
		for constant := range pallet.Constants {
			switch pallet.Constants[constant].Name {
			case "DepositBase":
				pallet.Constants[constant].Value = binary.LittleEndian.AppendUint64(nil, 132_000_000)
			case "DepositFactor":
				pallet.Constants[constant].Value = binary.LittleEndian.AppendUint64(nil, 32_000_000)
			}
		}
	}
	raw, err := codec.Encode(*metadata)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "0x" + hex.EncodeToString(raw)
	decoded, _, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return decoded, encoded
}

// Convert the ordinary synthetic action into a first approval by the named
// signatory, prepared against the supplied metadata and freshly approved.
func ownerTrimMultisigTestConfig(t *testing.T, f *ownerTrimTestFixture, signers ownerTrimMultisigTestSigners, signatory string) {
	t.Helper()
	f.config.Schema = ownerTrimMultisigExecutionSchema
	action := f.config.Action
	action.Schema, action.SignatureScheme, action.SelectionRule = ownerTrimMultisigActionSchema, "ed25519", ownerTrimBestEffortSelection
	action.Coldkey, action.MetadataDigest, action.DerivationPath = signers.owner, "0x"+strings.Repeat("ab", 32), "m/44'/354'/1'/0'/0'"
	f.ledgerMetadata = "0x" + hex.EncodeToString(append([]byte{'m', 'e', 't', 'a', 15}, []byte("synthetic multisig metadata shape for adapter fixture only")...))
	action.LedgerMetadataHash, _ = ownerLedgerMetadataHash(f.ledgerMetadata)
	action.Multisig = &ownerTrimMultisig{AccountId: signers.owner, Threshold: 2, Signatories: slices.Clone(signers.accounts), Signatory: signatory,
		Operation: "as_multi", MaxRefTime: 1_000_000_000, MaxProofSize: 65_536, DepositLimitRao: 196_000_000}
	var err error
	f.config.Action, err = prepareOwnerTrimAction(action, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
}

// A first approval prepared against the pinned projection metadata.
func newOwnerTrimMultisigTestFixture(t *testing.T) (*ownerTrimTestFixture, ownerTrimMultisigTestSigners, *types.Metadata) {
	t.Helper()
	f := newOwnerTrimActionTestFixture(t)
	metadata, encoded := ownerTrimMultisigTestMetadata(t)
	raw, _ := hex.DecodeString(encoded[2:])
	f.metadata = encoded
	f.config.Action.Runtime.RuntimeMetadataHash = rootExtrinsicHash(raw)
	signers := newOwnerTrimMultisigTestSigners(t)
	ownerTrimMultisigTestConfig(t, f, signers, signers.accounts[2])
	return f, signers, metadata
}

// A later step on the same operation, prepared and independently approved.
func ownerTrimMultisigTestStep(t *testing.T, f *ownerTrimTestFixture, operation, signatory string, timepoint treasuryTimepoint) ownerTrimExecutionConfig {
	t.Helper()
	template := ownerTrimMultisigStepTemplate{Operation: operation, Signatory: signatory, DerivationPath: "m/44'/354'/0'/0'/0'", Nonce: 3,
		BirthBlock: uint64(timepoint.Height) + 1, BirthHash: "0x" + strings.Repeat("5c", 32), Period: 16, FeeReserveRao: 1_000_000, MaxBroadcasts: 2}
	if operation == "as_multi" {
		template.MaxRefTime, template.MaxProofSize = 1_000_000_000, 65_536
	}
	config, err := ownerTrimMultisigStepConfig(f.config, template, timepoint)
	if err != nil {
		t.Fatal(err)
	}
	config.Action, err = prepareOwnerTrimAction(config.Action, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	config.Signature = hex.EncodeToString(ed25519.Sign(f.approval, config.signingBytes()))
	return config
}

// The explicitly requested public vector: btcli preset ur-owner, threshold two.
// These are public on-chain addresses, used here and nowhere else in fixtures.
func TestOwnerTrimMultisigDerivationMatchesUrOwnerVector(t *testing.T) {
	signatories := []string{"5HnhcDkB7fQcbabDsmQXeP6N4fDKnciKjMkKCScUwkJYHH3u", "5GeoGiGEvUEQqTfTaUsvXqMQD4zVMNeYtYqMN8JLaMfiDp4J", "5DFCNQmzRedo6hbZci4PTFMRuQJQ5PX6f6WrJBxDCDU3yBzS"}
	accounts := [][32]byte{}
	hexes := []string{}
	for _, address := range signatories {
		raw, err := ss58.DecodeWithPrefix(address, 42)
		if err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, raw)
		hexes = append(hexes, "0x"+hex.EncodeToString(raw[:]))
	}
	sort.Slice(accounts, func(i, j int) bool { return bytes.Compare(accounts[i][:], accounts[j][:]) < 0 })
	sort.Strings(hexes)
	derived, err := crv4.DeriveNativeMultisigAccount(accounts, 2)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ss58.Encode(derived, 42)
	if err != nil || address != "5HTeZ5168DjjGWZgbvzGfysEAj9fnexF24gYFKJHENU5cc8a" {
		t.Fatal("ur-owner multisig derivation differs", address, err)
	}
	owner := "0x" + hex.EncodeToString(derived[:])
	multisig := ownerTrimMultisig{AccountId: owner, Threshold: 2, Signatories: hexes, Signatory: hexes[2], Operation: "as_multi", MaxRefTime: 1, MaxProofSize: 1, DepositLimitRao: 1}
	if err := multisig.validate(owner); err != nil {
		t.Fatal("ur-owner signer set refused by the action validator", err)
	}
	for _, threshold := range []uint16{1, 3} {
		changed := multisig.clone()
		changed.Threshold = threshold
		if changed.validate(owner) == nil {
			t.Fatal("another threshold derived the same owner", threshold)
		}
	}
	changed := multisig.clone()
	changed.Signatories = changed.Signatories[:2]
	if changed.validate(owner) == nil {
		t.Fatal("incomplete signer set derived the owner")
	}
}

// Independent GSRPC encoding of the typed runtime calls equals both exact
// approvals and the cancellation, with the nested trim call bound by hash.
func TestOwnerTrimMultisigApprovalCallsMatchTypedRuntimeCodec(t *testing.T) {
	f, signers, metadata := newOwnerTrimMultisigTestFixture(t)
	first := f.config.Action
	inner, err := types.NewCall(metadata, "AdminUtils.sudo_trim_to_max_allowed_uids", types.U16(25), types.U16(first.MaximumUids))
	if err != nil {
		t.Fatal(err)
	}
	innerRaw, err := codec.Encode(inner)
	if err != nil || !bytes.Equal(innerRaw, []byte{19, 78, 25, 0, byte(first.MaximumUids), 0}) || first.Multisig.InnerCall != "0x"+hex.EncodeToString(innerRaw) {
		t.Fatal("nested trim call differs from the pinned AdminUtils index", hex.EncodeToString(innerRaw), err)
	}
	digest := blake2b.Sum256(innerRaw)
	if first.Multisig.CallHash != "0x"+hex.EncodeToString(digest[:]) {
		t.Fatal("inner call hash is not blake2_256 of the exact trim call")
	}
	others := func(signatory string) []types.AccountID {
		result := []types.AccountID{}
		for _, account := range signers.accounts {
			if account != signatory {
				raw, _ := hex.DecodeString(account[2:])
				id, _ := types.NewAccountID(raw)
				result = append(result, *id)
			}
		}
		return result
	}
	weight := types.NewWeight(types.NewUCompactFromUInt(1_000_000_000), types.NewUCompactFromUInt(65_536))
	timepoint := treasuryTimepoint{Height: 101, Index: 1}
	final := ownerTrimMultisigTestStep(t, f, "as_multi", signers.accounts[0], timepoint).Action
	cancel := ownerTrimMultisigTestStep(t, f, "cancel_as_multi", signers.accounts[2], timepoint).Action
	hash, _ := types.NewHashFromHexString(first.Multisig.CallHash)
	point := types.TimePoint{Height: 101, Index: 1}
	for _, item := range []struct {
		name   string
		action ownerTrimAction
		call   func() (types.Call, error)
	}{
		{name: "first", action: first, call: func() (types.Call, error) {
			return types.NewCall(metadata, "Multisig.as_multi", types.U16(2), others(first.Multisig.Signatory), types.NewEmptyOption[types.TimePoint](), inner, weight)
		}},
		{name: "final", action: final, call: func() (types.Call, error) {
			return types.NewCall(metadata, "Multisig.as_multi", types.U16(2), others(final.Multisig.Signatory), types.NewOption(point), inner, weight)
		}},
		{name: "cancel", action: cancel, call: func() (types.Call, error) {
			return types.NewCall(metadata, "Multisig.cancel_as_multi", types.U16(2), others(cancel.Multisig.Signatory), point, hash)
		}},
	} {
		call, err := item.call()
		if err != nil {
			t.Fatal(item.name, err)
		}
		typed, err := codec.Encode(call)
		if err != nil || "0x"+hex.EncodeToString(typed) != item.action.Call {
			t.Fatal(item.name, "outer multisig call differs from typed runtime encoding", err)
		}
		if item.name != "cancel" && !bytes.Contains(typed, innerRaw) {
			t.Fatal(item.name, "approval does not carry the nested trim call for device display")
		}
		payload, _ := hex.DecodeString(item.action.Payload[2:])
		if !bytes.HasPrefix(payload, typed) || len(payload) > 256 || !bytes.Equal(ownerSigningBytes(item.action), payload) {
			t.Fatal(item.name, "signing payload does not begin with the exact outer call")
		}
		key := signers.keys[item.action.Multisig.Signatory]
		signed, err := item.action.signed(ed25519.Sign(key, payload))
		if err != nil || ownerTrimSignedAction(item.action, signed) != nil {
			t.Fatal(item.name, "signatory envelope differs", err)
		}
		reader := rootScaleReader{data: signed}
		if _, err := reader.compact(); err != nil || !bytes.Equal(signed[reader.offset+2:reader.offset+34], key.Public().(ed25519.PublicKey)) {
			t.Fatal(item.name, "outer extrinsic is not signed by the named signatory", err)
		}
		if _, err := item.action.signed(ed25519.Sign(signers.keys[signers.accounts[1]], payload)); err == nil {
			t.Fatal(item.name, "another signatory's signature was admitted")
		}
	}
	// The first approval's deposit limit binds the metadata deposit constants.
	limited := first
	limited.Multisig = new(first.Multisig.clone())
	limited.Multisig.DepositLimitRao = 195_999_999
	if _, err := prepareOwnerTrimAction(limited, f.metadata); err == nil {
		t.Fatal("deposit above the approved limit was prepared")
	}
}

// The signer set must derive the reviewed owner; the named signer must belong to
// it; the inner call and its hash cannot be substituted; roles stay fixed.
func TestOwnerTrimMultisigRefusesForeignSignerSetCallHashAndRoles(t *testing.T) {
	f, signers, _ := newOwnerTrimMultisigTestFixture(t)
	outsider := "0x" + hex.EncodeToString(ownerTrimMultisigTestKey("outsider").Public().(ed25519.PublicKey))
	for _, item := range []struct {
		name   string
		change func(*ownerTrimAction)
	}{
		{name: "signatory-outside-set", change: func(a *ownerTrimAction) { a.Multisig.Signatory = outsider }},
		{name: "set-not-deriving-owner", change: func(a *ownerTrimAction) {
			a.Multisig.Signatories[0] = outsider
			slices.Sort(a.Multisig.Signatories)
		}},
		{name: "owner-not-derived", change: func(a *ownerTrimAction) { a.Coldkey, a.Multisig.AccountId = outsider, outsider }},
		{name: "unsorted-set", change: func(a *ownerTrimAction) { slices.Reverse(a.Multisig.Signatories) }},
		{name: "threshold-three", change: func(a *ownerTrimAction) { a.Multisig.Threshold = 3 }},
		{name: "first-without-deposit-limit", change: func(a *ownerTrimAction) { a.Multisig.DepositLimitRao = 0 }},
		{name: "unbounded-weight", change: func(a *ownerTrimAction) { a.Multisig.MaxRefTime = 0 }},
		{name: "direct-domain", change: func(a *ownerTrimAction) { a.Schema = ownerTrimBestEffortActionSchema }},
	} {
		action := f.config.Action
		multisig := action.Multisig.clone()
		action.Multisig = &multisig
		item.change(&action)
		if _, err := prepareOwnerTrimAction(action, f.metadata); err == nil {
			t.Fatal(item.name, "was prepared")
		}
	}
	for _, item := range []struct {
		name   string
		change func(*ownerTrimMultisig)
	}{
		{name: "call-hash", change: func(m *ownerTrimMultisig) { m.CallHash = "0x" + strings.Repeat("cd", 32) }},
		{name: "inner-call", change: func(m *ownerTrimMultisig) { m.InnerCall = "0x134e19000700" }},
	} {
		action := f.config.Action
		multisig := action.Multisig.clone()
		item.change(&multisig)
		action.Multisig = &multisig
		if action.validate() == nil {
			t.Fatal(item.name, "substitution kept the original approval bytes")
		}
	}
	template := ownerTrimMultisigStepTemplate{Operation: "cancel_as_multi", Signatory: signers.accounts[0]}
	if _, err := ownerTrimMultisigStepConfig(f.config, template, treasuryTimepoint{Height: 101, Index: 1}); err == nil {
		t.Fatal("a non-depositor cancellation was planned")
	}
	template = ownerTrimMultisigStepTemplate{Operation: "as_multi", Signatory: f.config.Action.Multisig.Signatory}
	if _, err := ownerTrimMultisigStepConfig(f.config, template, treasuryTimepoint{Height: 101, Index: 1}); err == nil {
		t.Fatal("the depositor's own second approval was planned as the final approval")
	}
	final := ownerTrimMultisigTestStep(t, f, "as_multi", signers.accounts[0], treasuryTimepoint{Height: 101, Index: 1})
	late := final.Action
	late.Multisig = new(late.Multisig.clone())
	late.Multisig.Timepoint.Height = uint32(late.BirthBlock) + 1
	if _, err := prepareOwnerTrimAction(late, f.metadata); err == nil {
		t.Fatal("a later step born before its original timepoint was prepared")
	}
}

// The owner pins the subnet owner and its own named signatory independently.
// Replies retain the signatory; direct-owner shapes gain no multisig field.
func TestOwnerTrimMultisigRequestBindsNamedSignatoryAndOwner(t *testing.T) {
	f, signers, _ := newOwnerTrimMultisigTestFixture(t)
	request, err := newOwnerSigningRequest(f.config, f.key, f.metadata, f.ledgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	signatory := f.config.Action.Multisig.Signatory
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: f.key, Owner: signers.owner, Genesis: f.config.Action.Network.GenesisHash, Signatory: signatory}
	if err := request.validate(trust); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name   string
		change func(*ownerSigningTrust)
	}{
		{name: "missing-signatory", change: func(v *ownerSigningTrust) { v.Signatory = "" }},
		{name: "other-signatory", change: func(v *ownerSigningTrust) { v.Signatory = signers.accounts[0] }},
		{name: "outsider", change: func(v *ownerSigningTrust) {
			v.Signatory = "0x" + hex.EncodeToString(ownerTrimMultisigTestKey("outsider").Public().(ed25519.PublicKey))
		}},
		{name: "signatory-as-owner", change: func(v *ownerSigningTrust) { v.Owner = signatory }},
		{name: "other-chain-owner", change: func(v *ownerSigningTrust) { v.Owner = "0x" + strings.Repeat("71", 32) }},
	} {
		changed := trust
		item.change(&changed)
		if request.validate(changed) == nil {
			t.Fatal(item.name, "admitted a multisig request")
		}
	}
	payload, _ := hex.DecodeString(f.config.Action.Payload[2:])
	reply, err := newOwnerSigningReply(request, ed25519.Sign(signers.keys[signatory], payload))
	if err != nil || reply.Owner != signers.owner || reply.Signatory != signatory {
		t.Fatal("multisig reply lost its owner or signatory", err)
	}
	if _, err := reply.validate(request); err != nil {
		t.Fatal(err)
	}
	changed := reply
	changed.Signatory = signers.accounts[0]
	if _, err := changed.validate(request); err == nil {
		t.Fatal("reply relabeled another signatory")
	}
	if _, err := newOwnerSigningReply(request, ed25519.Sign(signers.keys[signers.accounts[0]], payload)); err == nil {
		t.Fatal("another signatory's signature produced a reply")
	}
	// The direct owner path keeps its original JSON shapes.
	_, direct, directTrust := ownerSigningTestRequest(t)
	if direct.validate(ownerSigningTrust{RequestHash: directTrust.RequestHash, ApprovalKey: directTrust.ApprovalKey, Owner: directTrust.Owner,
		Genesis: directTrust.Genesis, Signatory: directTrust.Owner}) == nil {
		t.Fatal("a direct owner request accepted a multisig signatory pin")
	}
	directReply, err := newOwnerSigningReply(direct, ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(direct.Config.Action)))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{direct, directReply, direct.Config.Action} {
		raw, _ := json.Marshal(value)
		if bytes.Contains(raw, []byte("multisig")) || bytes.Contains(raw, []byte("signatory_account_id")) {
			t.Fatal("direct owner JSON gained a multisig field", string(raw))
		}
	}
}

// LAUNCH.md's hard rule keeps SN25 ownership with ur-owner. No tool in this
// module builds a coldkey-swap call today; a tool that starts to must refuse a
// source equal to SubnetOwner(25) and replace this tripwire with that refusal's
// own test. Retained third-party and evidence sources are not tools.
func TestOwnerTrimMultisigNoToolBuildsColdkeySwap(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal("module root not found", err)
	}
	markers := []string{"announce_coldkey_swap", "swap_coldkey", "schedule_swap_coldkey"}
	scanned := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "third_party" || name == "evidence" || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(name)
		if extension != ".go" && extension != ".py" && extension != ".sh" && extension != ".rs" && extension != ".ts" && extension != ".js" ||
			strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.py") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for _, marker := range markers {
			if bytes.Contains(raw, []byte(marker)) {
				t.Errorf("%s names %s; refuse a source equal to SubnetOwner(25) per the LAUNCH.md hard rule", path, marker)
			}
		}
		return nil
	})
	if err != nil || scanned < 100 {
		t.Fatal("module tool sources were not scanned", scanned, err)
	}
}

// Direct-owner actions, requests and replies keep the exact bytes and seals the
// fixtures produced before multisig support existed (values recorded on the
// base commit with the synthetic fixtures and a fixed journal path).
func TestOwnerTrimMultisigDirectOwnerBytesUnchanged(t *testing.T) {
	golden := map[string][3]string{
		"v1": {"0x134e1900060002001c0000df030000010000001212121212121212121212121212121212121212121212121212121212121212787878787878787878787878787878787878787878787878787878787878787800",
			"sha256:3ffefe2160dc42de8e81002925cda4703dae14841da05cd7f7b6be1a128346ba", "sha256:9cff3ec30c386e6a822da72f34f15ccd44249230231296c1cd415fabea59d03e"},
		"v2": {"0x134e1900060002001c0001df030000010000001212121212121212121212121212121212121212121212121212121212121212787878787878787878787878787878787878787878787878787878787878787801abababababababababababababababababababababababababababababababab",
			"sha256:cc6e357436f57efdbad91bdd8f478c443dd898b18744d1cfb555825bd7edd490", "sha256:e633fa1368675487d92eb94f56c1f841f19e1bc96be74b4bbc9085b0e90d3a68"},
		"best-effort": {"0x134e1900060002001c0001df030000010000001212121212121212121212121212121212121212121212121212121212121212787878787878787878787878787878787878787878787878787878787878787801abababababababababababababababababababababababababababababababab",
			"sha256:d06285d6363c69e280a6ca49e584f644292b950e0492fc27b7b163481a2dedee", "sha256:213251706caf93907012b626e6b0912b76d29ddc8e047465c280d3e3ab76dae4"},
	}
	for schema, want := range golden {
		f := newOwnerTrimActionTestFixture(t)
		if schema != "v1" {
			ownerSigningTestLedgerConfig(t, f)
		}
		action := f.config.Action
		if schema == "best-effort" {
			f.config.Schema = ownerTrimBestEffortExecutionSchema
			action.Schema, action.SelectionRule = ownerTrimBestEffortActionSchema, ownerTrimBestEffortSelection
		}
		action.StatePath = "/synthetic-run/owner-trim-action.json"
		var err error
		if action, err = prepareOwnerTrimAction(action, f.metadata); err != nil {
			t.Fatal(err)
		}
		f.config.Action = action
		f.approve()
		ledger := []string{f.ledgerMetadata}
		if schema == "v1" {
			ledger = nil
		}
		request, err := newOwnerSigningRequest(f.config, f.key, f.metadata, ledger...)
		if err != nil || action.Payload != want[0] || action.RequestHash != want[1] || request.ContentHash != want[2] {
			t.Fatal(schema, "direct owner bytes changed", action.Payload, action.RequestHash, request.ContentHash, err)
		}
		if schema == "v1" {
			continue
		}
		signed, err := action.signed(ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(action)))
		if err != nil || rootExtrinsicHash(signed) != "0xdb318d1c48da7c80bd5b33a36f9249cb180cee73235d8281dc1cf94a3561f54b" {
			t.Fatal(schema, "direct owner extrinsic changed", err)
		}
	}
}
