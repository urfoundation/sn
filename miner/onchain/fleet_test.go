package onchain

import (
	"crypto/ed25519"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

func TestBuildFleetBindingCalldataVerifiesBothSigners(t *testing.T) {
	clientSeed := [32]byte{1}
	clientPrivate := ed25519.NewKeyFromSeed(clientSeed[:])
	clientPublic := clientPrivate.Public().(ed25519.PublicKey)
	hotkeySeed := [32]byte{2}
	hotkey, err := crv4.KeypairFromSeed(hotkeySeed)
	if err != nil {
		t.Fatal(err)
	}
	binding := protocol.FleetBinding{
		ChainID: 945, Netuid: 7, Coordinator: [20]byte(common.HexToAddress("0x1111111111111111111111111111111111111111")),
		FleetID: [32]byte{3}, Hotkey: hotkey.PublicKey(), ClientID: [16]byte{4}, Generation: 1,
		ValidFromEpoch: 2, ValidToEpoch: 10, CommitmentHash: [32]byte{5},
	}
	copy(binding.ClientKey[:], clientPublic)
	clientSignature, err := binding.SignClient(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := binding.Digest()
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	got, err := BuildFleetBindingCalldata(binding, clientSignature, hotkeySignature)
	if err != nil {
		t.Fatal(err)
	}
	want, err := stCoordinator.TryPackBindFleetMember(stabi.STCoordinatorFleetBinding{
		ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: common.Address(binding.Coordinator),
		FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey,
		Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch,
		CommitmentHash: binding.CommitmentHash,
	}, clientSignature, hotkeySignature)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("fleet calldata differs from generated ABI")
	}
	clientSignature[0] ^= 1
	if _, err := BuildFleetBindingCalldata(binding, clientSignature, hotkeySignature); err == nil {
		t.Fatal("invalid client signature accepted")
	}
}
