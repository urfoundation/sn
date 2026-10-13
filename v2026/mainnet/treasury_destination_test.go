// Receive-only public commands use real native storage replay and HTTP finality
// with synthetic chain state, without constructing a signer or custody owner.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/validator"
	"gopkg.in/yaml.v3"
)

// The public receiving interface has no System account or Multisig metadata,
// registration fee storage, private key, native signature or hardware reference.
func treasuryDestinationTestFixture(t *testing.T) (treasuryDestinationInput, *treasuryFixture, *rootReceiptFixture) {
	t.Helper()
	metadata, _ := treasuryTestMetadata(t)
	pallets := make([]types.PalletMetadataV14, 0, len(metadata.AsMetadataV14.Pallets))
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name == "System" || pallet.Name == "Multisig" {
			continue
		}
		if pallet.Name == "SubtensorModule" {
			entries := make([]types.StorageEntryMetadataV14, 0, len(pallet.Storage.Items))
			for _, entry := range pallet.Storage.Items {
				if entry.Name != "Burn" && entry.Name != "NetworkRegistrationAllowed" {
					entries = append(entries, entry)
				}
			}
			pallet.Storage.Items = entries
		}
		pallets = append(pallets, pallet)
	}
	metadata.AsMetadataV14.Pallets = pallets
	raw, err := codec.Encode(*metadata)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "0x" + hex.EncodeToString(raw)
	metadata, _, err = crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	destination := treasuryDestination{Schema: treasuryDestinationSchema, Profile: "mainnet", Netuid: 25,
		GenesisHash: "0x" + strings.Repeat("16", 32), AccountId: "0x" + strings.Repeat("41", 32),
		RecipientHotkeys: []string{"0x" + strings.Repeat("18", 32), "0x" + strings.Repeat("28", 32)}}
	f := &treasuryFixture{metadata: metadata, values: map[string]string{}}
	netuid := []byte{25, 0}
	for name, value := range map[string][]byte{"NetworksAdded": {1}, "SubnetworkN": {2, 0}, "MaxAllowedUids": {16, 0}, "NetworkRegisteredAt": binary.LittleEndian.AppendUint64(nil, 10), "RegisteredSubnetCounter": binary.LittleEndian.AppendUint64(nil, 3), "SubnetOwner": bytes.Repeat([]byte{71}, 32), "SubnetOwnerHotkey": bytes.Repeat([]byte{72}, 32)} {
		f.set(t, "SubtensorModule", name, value, netuid)
	}
	account, _ := hex.DecodeString(destination.AccountId[2:])
	for i, hotkeyHex := range destination.RecipientHotkeys {
		hotkey, _ := hex.DecodeString(hotkeyHex[2:])
		uid := binary.LittleEndian.AppendUint16(nil, uint16(i))
		f.set(t, "SubtensorModule", "Owner", account, hotkey)
		f.set(t, "SubtensorModule", "Uids", uid, netuid, hotkey)
		f.set(t, "SubtensorModule", "Keys", hotkey, netuid, uid)
		f.set(t, "SubtensorModule", "IsNetworkMember", []byte{1}, hotkey, netuid)
		f.set(t, "SubtensorModule", "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 20+uint64(i)), netuid, uid)
	}
	policy := treasuryChainPolicy{NativeChain: "synthetic-receiving-chain", GenesisHash: destination.GenesisHash, EvmChainId: mainnetEvmChainId,
		rootReceiptProfile: rootReceiptProfile{RuntimeSourceCommit: rootPassiveSource,
			RuntimeVersion:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1},
			RuntimeCodeHash: "0x" + strings.Repeat("26", 32), RuntimeMetadataHash: rootExtrinsicHash(raw)}}
	header, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("9a", 32), 100, nil, false)
	chain := &rootReceiptFixture{metadata: metadata, metadataHex: encoded, profile: policy.rootReceiptProfile,
		headers: map[string]rootReceiptHeader{hash: header}, byHeight: map[uint64]string{100: hash},
		storageKVs: f.values, evmHex: "0x3c4", runtimeKVs: map[string]rootReceiptProfile{}, metadataKVs: map[string]string{}, counts: map[string]int{}, finalized: hash}
	chain.action.Scope.NativeChain, chain.action.Scope.GenesisHash = policy.NativeChain, policy.GenesisHash
	server := httptest.NewServer(http.HandlerFunc(chain.serve))
	t.Cleanup(server.Close)
	input := treasuryDestinationInput{Schema: treasuryDestinationInputSchema, Policy: policy, Destination: destination, Metadata: encoded,
		Route: ownedSubmissionRoute{RpcUrl: server.URL, ReadRetrySeconds: 60}}
	return input, f, chain
}

// A public command captures the exact finalized original before any replay.
func treasuryDestinationTestObserve(t *testing.T, input treasuryDestinationInput) treasuryDestinationInput {
	t.Helper()
	path, _ := treasuryTestJson(t, t.TempDir(), "receive-observe.json", input)
	var output, diagnostic bytes.Buffer
	if code := runMain(t.Context(), []string{"treasury", "observe", "--input", path}, &output, &diagnostic); code != 0 {
		t.Fatal("receive observation refused without sending custody", code, diagnostic.String())
	}
	if err := decodePlanJson(output.Bytes(), &input.Observation); err != nil {
		t.Fatal(err)
	}
	return input
}

// The selected public address alone is describable; it makes no roster claim.
func TestTreasuryDestinationDescribeAcceptsReceivingAccountOnly(t *testing.T) {
	destination := treasuryDestination{Schema: treasuryDestinationSchema, Profile: "mainnet", Netuid: 25,
		GenesisHash: "0x" + strings.Repeat("16", 32), AccountId: "0x1815103f41a8d1e24c55d380c6f843fb36d715b4322a4e4f02bff36dfe74a410"}
	raw, err := yaml.Marshal(destination)
	if err != nil {
		t.Fatal(err)
	}
	path, pin := ownerRecycleTestFile(t, t.TempDir(), "destination.yml", raw)
	var output, diagnostic bytes.Buffer
	if code := runMain(t.Context(), []string{"treasury", "describe", "--destination", path, "--destination-sha256", pin}, &output, &diagnostic); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	var received treasuryDestination
	if err := decodePlanJson(output.Bytes(), &received); err != nil || received.AccountId != destination.AccountId || len(received.RecipientHotkeys) != 0 {
		t.Fatal("description invented custody or registrations", err)
	}
	for _, extra := range []string{"threshold: 2\n", "signatories: []\n", "device_config: {}\n", "seed: forbidden\n", "schema: duplicate\n", "---\n{}\n", "profile: &profile mainnet\n"} {
		if _, err := decodeTreasuryDestination(append(bytes.Clone(raw), []byte(extra)...)); err == nil {
			t.Fatal("receiving descriptor admitted signing or ambiguous fields", extra)
		}
	}
	if _, err := decodeTreasuryDescriptor(raw); err == nil {
		t.Fatal("receive-only account acquired sending custody")
	}
}

// The entire CLI path succeeds with no signing interfaces or funded accounts.
func TestTreasuryDestinationCommandObservesAndPlansWithoutSendCustody(t *testing.T) {
	input, _, chain := treasuryDestinationTestFixture(t)
	input = treasuryDestinationTestObserve(t, input)
	path, _ := treasuryTestJson(t, t.TempDir(), "receive-policy.json", input)
	var output, diagnostic bytes.Buffer
	if code := runMain(t.Context(), []string{"treasury", "policy-plan", "--input", path}, &output, &diagnostic); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	var result struct {
		Policy      validator.TreasuryPolicy `json:"treasury_policy"`
		Hash        string                   `json:"treasury_policy_hash"`
		Observation string                   `json:"observation_hash"`
		Approved    bool                     `json:"approved"`
	}
	if err := decodePlanJson(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	hash, err := result.Policy.Hash()
	if err != nil || result.Policy.Schema != validator.TreasuryReceivePolicySchema || result.Policy.Threshold != 0 || result.Policy.Signatories != nil || len(result.Policy.Recipients) != 2 || result.Approved || result.Hash != "0x"+hex.EncodeToString(hash[:]) || result.Observation != input.Observation.ContentHash {
		t.Fatal("receive-only projection lost exact policy or invented authority", err, output.String())
	}
	chain.stateLock.Lock()
	defer chain.stateLock.Unlock()
	if chain.counts["author_submitExtrinsic"] != 0 || chain.counts["chain_getFinalizedHead"] < 2 || chain.counts["system_chain"] < 2 {
		t.Fatal("receiving observation lost read-only finality closure", chain.counts)
	}
}

// Changed original scope, missing rows and native contradictions fail replay.
func TestTreasuryDestinationPolicyReplayRejectsScopeOrNativeDrift(t *testing.T) {
	input, f, _ := treasuryDestinationTestFixture(t)
	input = treasuryDestinationTestObserve(t, input)
	for _, fault := range []string{"schema", "destination", "runtime", "missing-row", "duplicate-row", "owner", "generation", "membership", "owner-hotkey"} {
		raw, _ := json.Marshal(input)
		var changed treasuryDestinationInput
		if err := decodePlanJson(raw, &changed); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "schema":
			changed.Observation.Schema = treasuryObservationSchema
		case "destination":
			changed.Destination.AccountId = "0x" + strings.Repeat("42", 32)
		case "runtime":
			changed.Policy.RuntimeCodeHash = "0x" + strings.Repeat("27", 32)
		case "missing-row":
			changed.Observation.Rows = changed.Observation.Rows[1:]
		case "duplicate-row":
			changed.Observation.Rows = append(changed.Observation.Rows, changed.Observation.Rows[0])
		default:
			netuid, uid := []byte{25, 0}, []byte{0, 0}
			hotkey, _ := hex.DecodeString(input.Destination.RecipientHotkeys[0][2:])
			name, args, value := "Owner", [][]byte{hotkey}, bytes.Repeat([]byte{0x42}, 32)
			switch fault {
			case "generation":
				name, args, value = "BlockAtRegistration", [][]byte{netuid, uid}, binary.LittleEndian.AppendUint64(nil, 101)
			case "membership":
				name, args, value = "IsNetworkMember", [][]byte{hotkey, netuid}, []byte{0}
			case "owner-hotkey":
				name, args, value = "SubnetOwnerHotkey", [][]byte{netuid}, hotkey
			}
			key, err := types.CreateStorageKey(f.metadata, "SubtensorModule", name, args...)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for i := range changed.Observation.Rows {
				if changed.Observation.Rows[i].Key == key.Hex() {
					encoded := "0x" + hex.EncodeToString(value)
					changed.Observation.Rows[i].Raw = &encoded
					found = true
				}
			}
			if !found {
				t.Fatal("negative test failed to change original native row", fault)
			}
		}
		changed.Observation.ContentHash = ""
		changed.Observation.ContentHash = rootObjectHash(changed.Observation)
		if _, err := changed.facts(t.Context()); err == nil {
			t.Fatal("receiving replay admitted changed scope or native evidence", fault)
		}
	}
}

// A valid receiving policy cannot be relabeled as registration or send custody.
func TestTreasuryDestinationNeverAuthorizesNativeExecution(t *testing.T) {
	input, _, _ := treasuryDestinationTestFixture(t)
	input = treasuryDestinationTestObserve(t, input)
	path, _ := treasuryTestJson(t, t.TempDir(), "receive-only.json", input)
	for _, mode := range []string{"plan", "reserve", "export", "sign", "submit"} {
		var output, diagnostic bytes.Buffer
		if code := runMain(context.Background(), []string{"treasury", mode, "--input", path}, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("receive-only input acquired execution authority", mode, diagnostic.String())
		}
	}
	f := newTreasuryFixture(t)
	f.input.Action.Descriptor.Multisig.Threshold = 0
	f.input.Action.Descriptor.Multisig.Signatories = nil
	if _, err := prepareTreasuryPlan(t.Context(), f.input); err == nil {
		t.Fatal("optional sending workflow no longer requires its original custody")
	}
}

// Accounting copies preserve the canonical receive policy and own their roster.
func TestTreasuryReceiveAccountingClonePreservesPolicyHash(t *testing.T) {
	input, _, _ := treasuryDestinationTestFixture(t)
	input = treasuryDestinationTestObserve(t, input)
	facts, err := input.facts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := input.Destination.policy(facts)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	clone := cloneNativeTreasuryAmounts(&nativeTreasuryAmounts{Policy: policy, PolicyHash: hash})
	if cloneHash, err := clone.Policy.Hash(); err != nil || cloneHash != hash || clone.Policy.Signatories != nil {
		t.Fatal("accounting clone invented signer fields or changed policy hash", err)
	}
	clone.Policy.Recipients[0].Hotkey[0]++
	if clone.Policy.Recipients[0].Hotkey == policy.Recipients[0].Hotkey {
		t.Fatal("accounting clone aliases original recipient authority")
	}
}
