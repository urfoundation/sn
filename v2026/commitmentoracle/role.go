package commitmentoracle

// Who a commitment belongs to. The one-field Sha256 storage format does not
// identify a fleet: the validator's source commitment
// (crv4/source_commitment.go) writes the same Commitments.set_commitment
// field from its own hotkey, inside one Utility.batch_all with its CRv4
// weight commit, and the validator refuses any coordinator mirror of its
// source hotkey (validator/release_submit_v2.go checkSourceRoleV2). On
// 2026-10-15 a manual mirror of the SN25 validator's source commitment (UID 1,
// 0x2830…c67b, tx 0x4bb7941d…222b5) halted its steering.
//
// So a fleet commitment is mirrored only when both hold:
//
//  1. Role, at the native finalized head on every endpoint: the hotkey is
//     registered on the netuid and its UID holds no validator permit.
//  2. Positive fleet evidence, in the commitment block's authenticated body
//     on every endpoint: the hotkey signed exactly one extrinsic in that block
//     and its call is exactly the standalone
//     Commitments.set_commitment(netuid, [Sha256(hash)]) the miner's
//     `provider fleet publish` sends. A batch (the validator's source write),
//     a proxy, a different hash or an extrinsic the oracle cannot parse is not
//     evidence.
//
// Anything that cannot be read or decoded mirrors nothing.

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// A hotkey's role on the netuid at one block.
type neuronRole struct {
	registered      bool
	uid             uint16
	validatorPermit bool
}

// Decodes SubtensorModule.ValidatorPermit(netuid), a Vec<bool> that must have
// exactly one entry per UID of SubnetworkN.
func decodeValidatorPermits(raw []byte, subnetworkN uint16) ([]bool, error) {
	var length types.UCompact
	if err := codec.Decode(raw, &length); err != nil {
		return nil, fmt.Errorf("ValidatorPermit length: %w", err)
	}
	prefix, err := codec.Encode(length)
	if err != nil {
		return nil, err
	}
	count := length.Int64()
	if count != int64(subnetworkN) || int64(len(raw)-len(prefix)) != count {
		return nil, fmt.Errorf("ValidatorPermit has %d entries in %d bytes, want %d", count, len(raw)-len(prefix), subnetworkN)
	}
	permits := make([]bool, count)
	for index, value := range raw[len(prefix):] {
		switch value {
		case 0:
		case 1:
			permits[index] = true
		default:
			return nil, fmt.Errorf("ValidatorPermit entry %d is not a bool", index)
		}
	}
	return permits, nil
}

// Derives the role from SubnetworkN, Uids(netuid, hotkey) (nil when the
// hotkey is not registered), Keys(netuid, uid) and ValidatorPermit(netuid).
func decodeNeuronRole(hotkey [32]byte, subnetworkN []byte, uid []byte, keyOfUid func(uint16) ([]byte, error), permitsRaw []byte) (neuronRole, error) {
	if len(subnetworkN) != 2 {
		return neuronRole{}, errors.New("SubnetworkN is not a u16")
	}
	count := uint16(subnetworkN[0]) | uint16(subnetworkN[1])<<8
	permits, err := decodeValidatorPermits(permitsRaw, count)
	if err != nil {
		return neuronRole{}, err
	}
	if uid == nil {
		return neuronRole{}, nil
	}
	if len(uid) != 2 {
		return neuronRole{}, errors.New("Uids value is not a u16")
	}
	value := uint16(uid[0]) | uint16(uid[1])<<8
	if value >= count {
		return neuronRole{}, fmt.Errorf("UID %d is outside SubnetworkN %d", value, count)
	}
	key, err := keyOfUid(value)
	if err != nil {
		return neuronRole{}, err
	}
	if !bytes.Equal(key, hotkey[:]) {
		return neuronRole{}, fmt.Errorf("Keys(%d) is not the hotkey", value)
	}
	return neuronRole{registered: true, uid: value, validatorPermit: permits[value]}, nil
}

// The most restrictive of the roles every endpoint reported.
func combineRoles(roles []neuronRole) (neuronRole, error) {
	if len(roles) == 0 {
		return neuronRole{}, errors.New("no role read")
	}
	combined := roles[0]
	for _, role := range roles[1:] {
		if role.registered != combined.registered || role.uid != combined.uid {
			return neuronRole{}, errors.New("substrate endpoints disagree on the hotkey's registration")
		}
		combined.validatorPermit = combined.validatorPermit || role.validatorPermit
	}
	return combined, nil
}

func decodeCompact(raw []byte) (uint64, int, error) {
	if len(raw) == 0 {
		return 0, 0, errors.New("compact is empty")
	}
	switch raw[0] & 3 {
	case 0:
		return uint64(raw[0] >> 2), 1, nil
	case 1:
		if len(raw) < 2 {
			return 0, 0, errors.New("compact is truncated")
		}
		return uint64(raw[0]>>2) | uint64(raw[1])<<6, 2, nil
	case 2:
		if len(raw) < 4 {
			return 0, 0, errors.New("compact is truncated")
		}
		return uint64(raw[0]>>2) | uint64(raw[1])<<6 | uint64(raw[2])<<14 | uint64(raw[3])<<22, 4, nil
	default:
		size := int(raw[0]>>2) + 4
		if size > 8 || len(raw) < 1+size {
			return 0, 0, errors.New("compact is too large or truncated")
		}
		var value uint64
		for index := size - 1; index >= 0; index-- {
			value = value<<8 | uint64(raw[1+index])
		}
		return value, 1 + size, nil
	}
}

// One signed extrinsic's signer and call.
type signedExtrinsic struct {
	signer [32]byte
	call   []byte
}

// Splits a version 4 signed extrinsic into its signer and call. It accepts
// the SN25 runtime's extension layout only (era, compact nonce, compact tip,
// CheckMetadataHash mode); the source commitment validator pins the same
// layout (crv4 preparedSourceEncoding). ok is false for an unsigned
// extrinsic or a bare v5 inherent. Any other shape is an error.
func parseSignedExtrinsic(raw []byte) (signedExtrinsic, bool, error) {
	length, size, err := decodeCompact(raw)
	if err != nil {
		return signedExtrinsic{}, false, err
	}
	body := raw[size:]
	if uint64(len(body)) != length || len(body) == 0 {
		return signedExtrinsic{}, false, errors.New("extrinsic length prefix differs from its body")
	}
	switch body[0] {
	case 0x04, 0x05:
		// Unsigned v4 or bare v5 (inherents): no signer.
		return signedExtrinsic{}, false, nil
	case 0x84:
	default:
		return signedExtrinsic{}, false, fmt.Errorf("extrinsic version 0x%02x is not supported", body[0])
	}
	rest := body[1:]
	if len(rest) < 33 || rest[0] != 0x00 {
		return signedExtrinsic{}, false, errors.New("extrinsic signer is not a MultiAddress::Id")
	}
	var extrinsic signedExtrinsic
	copy(extrinsic.signer[:], rest[1:33])
	rest = rest[33:]
	if len(rest) < 1 {
		return signedExtrinsic{}, false, errors.New("extrinsic signature is truncated")
	}
	switch rest[0] {
	case 0, 1:
		rest = rest[1:]
		if len(rest) < 64 {
			return signedExtrinsic{}, false, errors.New("extrinsic signature is truncated")
		}
		rest = rest[64:]
	case 2:
		rest = rest[1:]
		if len(rest) < 65 {
			return signedExtrinsic{}, false, errors.New("extrinsic signature is truncated")
		}
		rest = rest[65:]
	default:
		return signedExtrinsic{}, false, errors.New("extrinsic signature type is unknown")
	}
	// Era: immortal is one zero byte, mortal is two bytes.
	if len(rest) < 1 {
		return signedExtrinsic{}, false, errors.New("extrinsic era is truncated")
	}
	if rest[0] == 0 {
		rest = rest[1:]
	} else {
		if len(rest) < 2 {
			return signedExtrinsic{}, false, errors.New("extrinsic era is truncated")
		}
		rest = rest[2:]
	}
	for _, name := range []string{"nonce", "tip"} {
		_, size, err := decodeCompact(rest)
		if err != nil {
			return signedExtrinsic{}, false, fmt.Errorf("extrinsic %s: %w", name, err)
		}
		rest = rest[size:]
	}
	if len(rest) < 1 || rest[0] > 1 {
		return signedExtrinsic{}, false, errors.New("extrinsic metadata hash mode is invalid")
	}
	extrinsic.call = rest[1:]
	if len(extrinsic.call) < 2 {
		return signedExtrinsic{}, false, errors.New("extrinsic call is truncated")
	}
	return extrinsic, true, nil
}

// Requires positive fleet evidence in a commitment block: the hotkey signed
// exactly one extrinsic there, and its call equals fleetCall, the standalone
// set_commitment of the mirrored hash. An extrinsic that cannot be parsed
// fails only when it carries the hotkey's bytes, since it could be the
// hotkey's.
func fleetEvidence(extrinsics [][]byte, hotkey [32]byte, fleetCall []byte) error {
	if len(fleetCall) == 0 {
		return errors.New("no fleet call to match")
	}
	signed := []signedExtrinsic{}
	for index, raw := range extrinsics {
		extrinsic, ok, err := parseSignedExtrinsic(raw)
		if err != nil {
			if bytes.Contains(raw, hotkey[:]) {
				return fmt.Errorf("extrinsic %d names the hotkey and cannot be parsed: %w", index, err)
			}
			continue
		}
		if ok && extrinsic.signer == hotkey {
			signed = append(signed, extrinsic)
		}
	}
	if len(signed) != 1 {
		return fmt.Errorf("the hotkey signed %d extrinsics in the commitment block, want exactly 1", len(signed))
	}
	if !bytes.Equal(signed[0].call, fleetCall) {
		return fmt.Errorf("the hotkey's extrinsic is not a standalone fleet set_commitment (call 0x%x)", signed[0].call[:2])
	}
	return nil
}
