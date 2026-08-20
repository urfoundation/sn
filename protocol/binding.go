package protocol

import (
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

const FleetBindingDomain = "urnetwork/fleet-binding/v1"

type FleetBinding struct {
	ChainID        uint64   `json:"chain_id"`
	Netuid         uint16   `json:"netuid"`
	Coordinator    [20]byte `json:"coordinator"`
	FleetID        [32]byte `json:"fleet_id"`
	Hotkey         [32]byte `json:"hotkey"`
	ClientID       [16]byte `json:"client_id"`
	ClientKey      [32]byte `json:"client_key"`
	Generation     uint64   `json:"generation"`
	ValidFromEpoch uint64   `json:"valid_from_epoch"`
	ValidToEpoch   uint64   `json:"valid_to_epoch"`
	CommitmentHash [32]byte `json:"commitment_hash"`
}

const FleetBindingPayloadSize = len(FleetBindingDomain) + 8 + 2 + 20 + 32 + 32 + 16 + 32 + 8 + 8 + 8 + 32

func (b FleetBinding) Validate() error {
	if b.ChainID == 0 || b.Netuid == 0 {
		return errors.New("binding chain_id/netuid is zero")
	}
	if b.Coordinator == ([20]byte{}) || b.FleetID == ([32]byte{}) || b.Hotkey == ([32]byte{}) || b.ClientID == ([16]byte{}) || b.ClientKey == ([32]byte{}) || b.CommitmentHash == ([32]byte{}) {
		return errors.New("binding contains a zero identity/hash")
	}
	if b.Generation == 0 || b.ValidToEpoch < b.ValidFromEpoch {
		return errors.New("binding generation/validity is invalid")
	}
	return nil
}

func (b FleetBinding) Payload() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	out := make([]byte, 0, FleetBindingPayloadSize)
	out = append(out, FleetBindingDomain...)
	var u64 [8]byte
	binary.BigEndian.PutUint64(u64[:], b.ChainID)
	out = append(out, u64[:]...)
	var u16 [2]byte
	binary.BigEndian.PutUint16(u16[:], b.Netuid)
	out = append(out, u16[:]...)
	out = append(out, b.Coordinator[:]...)
	out = append(out, b.FleetID[:]...)
	out = append(out, b.Hotkey[:]...)
	out = append(out, b.ClientID[:]...)
	out = append(out, b.ClientKey[:]...)
	binary.BigEndian.PutUint64(u64[:], b.Generation)
	out = append(out, u64[:]...)
	binary.BigEndian.PutUint64(u64[:], b.ValidFromEpoch)
	out = append(out, u64[:]...)
	binary.BigEndian.PutUint64(u64[:], b.ValidToEpoch)
	out = append(out, u64[:]...)
	out = append(out, b.CommitmentHash[:]...)
	if len(out) != FleetBindingPayloadSize {
		return nil, fmt.Errorf("binding payload length %d, want %d", len(out), FleetBindingPayloadSize)
	}
	return out, nil
}

func (b FleetBinding) Digest() ([32]byte, error) {
	payload, err := b.Payload()
	if err != nil {
		return [32]byte{}, err
	}
	return crypto.Keccak256Hash(payload), nil
}

func (b FleetBinding) SignClient(privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if string(publicKey) != string(b.ClientKey[:]) {
		return nil, errors.New("client private key does not match binding")
	}
	digest, err := b.Digest()
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(privateKey, digest[:]), nil
}

func (b FleetBinding) VerifyClient(signature []byte) bool {
	digest, err := b.Digest()
	return err == nil && ed25519.Verify(ed25519.PublicKey(b.ClientKey[:]), digest[:], signature)
}

func (b FleetBinding) VerifyHotkey(signature []byte) bool {
	digest, err := b.Digest()
	if err != nil || len(signature) != 64 {
		return false
	}
	publicKey, err := (sr25519.Scheme{}).FromPublicKey(b.Hotkey[:])
	return err == nil && publicKey.Verify(digest[:], signature)
}
