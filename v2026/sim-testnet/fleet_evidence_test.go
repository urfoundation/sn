package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

func TestInspectFleetEvidenceCryptographicallyVerifiesBindings(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.ClientsPerHeadFleet = 1
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := common.HexToAddress("0x4000000000000000000000000000000000000004")
	clientRole := roles.Clients["miner-1"]
	clientSeed, _ := hex.DecodeString(clientRole.SeedHex)
	clientPrivate := ed25519.NewKeyFromSeed(clientSeed)
	hotkeyRole := roles.Substrate[fleetHotkeyLabel(1)]
	hotkey, err := crv4.KeypairFromSeedHex(hotkeyRole.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	fleetID := sha256.Sum256([]byte("fleet-evidence-test"))
	manifest := protocol.FleetManifest{
		Schema: protocol.FleetManifestSchema, ChainID: cfg.ChainID, Netuid: cfg.Netuid,
		FleetID: fleetID, Generation: 1,
	}
	copy(manifest.Coordinator[:], coordinator[:])
	manifest.Hotkey = hotkey.PublicKey()
	member := protocol.FleetMember{ClientID: [16]byte{1}}
	copy(member.ClientKey[:], clientPrivate.Public().(ed25519.PublicKey))
	manifest.Members = []protocol.FleetMember{member}
	manifestBytes, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	commitmentHash, _ := manifest.CommitmentHash()
	binding, err := manifest.Binding(member, 2, 9)
	if err != nil {
		t.Fatal(err)
	}
	clientSignature, _ := binding.SignClient(clientPrivate)
	digest, _ := binding.Digest()
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	hexValue := func(value []byte) string { return "0x" + hex.EncodeToString(value) }
	commitment := FleetCommitmentEvidence{
		Schema: "urnetwork-fleet-commitment-evidence-v1", ManifestURI: "fleet-1.json",
		CommitmentHash: hexValue(commitmentHash[:]), Hotkey: hexValue(manifest.Hotkey[:]),
		CommitmentBlock: 10, FinalizedBlock: 12, FinalizedBlockHash: "0x" + strings.Repeat("12", 32),
	}
	evidence := FleetBindingEvidence{
		Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: hexValue(member.ClientID[:]), ClientKey: hexValue(member.ClientKey[:]),
		FleetID: hexValue(manifest.FleetID[:]), Hotkey: hexValue(manifest.Hotkey[:]), Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 9,
		CommitmentHash: hexValue(commitmentHash[:]), BindingDigest: hexValue(digest[:]), ClientSignature: hexValue(clientSignature), HotkeySignature: hexValue(hotkeySignature),
		TransactionHash: "0x" + strings.Repeat("34", 32), BlockNumber: 13, BlockHash: "0x" + strings.Repeat("56", 32), UID: 7,
	}
	encode := func(value any) json.RawMessage {
		b, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return b
	}
	setup := map[string]json.RawMessage{
		"fleet_1_manifest": manifestBytes, "fleet_1_commitment": encode(commitment), "fleet_1_binding_1": encode(evidence),
	}
	commitmentOK, count, bindingsOK, uid, _ := inspectOneFleetEvidenceBytes(cfg, setup, coordinator, 1)
	if !commitmentOK || !bindingsOK || count != 1 || uid != 7 {
		t.Fatalf("valid evidence rejected: commitment=%t bindings=%t count=%d uid=%d", commitmentOK, bindingsOK, count, uid)
	}

	tampered := evidence
	tampered.ClientSignature = "0x" + strings.Repeat("00", 64)
	setup["fleet_1_binding_1"] = encode(tampered)
	commitmentOK, count, bindingsOK, _, _ = inspectOneFleetEvidenceBytes(cfg, setup, coordinator, 1)
	if !commitmentOK || bindingsOK || count != 0 {
		t.Fatalf("tampered signature accepted: commitment=%t bindings=%t count=%d", commitmentOK, bindingsOK, count)
	}

	setup["fleet_1_binding_1"] = encode(evidence)
	wrongCoordinator := common.HexToAddress("0x5000000000000000000000000000000000000005")
	commitmentOK, _, _, _, _ = inspectOneFleetEvidenceBytes(cfg, setup, wrongCoordinator, 1)
	if commitmentOK {
		t.Fatal("fleet evidence for a different coordinator was accepted")
	}
}
