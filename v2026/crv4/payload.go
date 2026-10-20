package crv4

import (
	"encoding/binary"
	"fmt"
)

// Payload is the weights payload that gets timelock-encrypted for a CRv4
// commit. It mirrors subtensor's WeightsTlockPayload exactly
// (pallets/subtensor/src/coinbase/reveal_commits.rs, freeze_struct
// "b6833b5029be4127", subtensor v3.4.9-424):
//
//	pub struct WeightsTlockPayload {
//	    pub hotkey: Vec<u8>,   // SCALE-encoded AccountId32 of the committer = raw 32 bytes
//	    pub uids: Vec<u16>,
//	    pub values: Vec<u16>,
//	    pub version_key: u64,
//	}
//
// The chain SCALE-decodes the decrypted bytes and REJECTS the reveal if
// payload.hotkey does not decode to the AccountId that signed the commit
// extrinsic. Hotkey must therefore be the sr25519 public key (32 bytes) of
// the hotkey used in Commit.
type Payload struct {
	Hotkey     [32]byte
	Uids       []uint16
	Values     []uint16
	VersionKey uint64
}

// Encode returns the SCALE encoding (parity-scale-codec) of the payload,
// byte-identical to WeightsTlockPayload::encode() in bittensor-drand v2.0.0
// and subtensor. Layout:
//
//	compact(32) ++ hotkey[32]
//	compact(len(uids)) ++ uids as u16 LE
//	compact(len(values)) ++ values as u16 LE
//	version_key as u64 LE
func (p *Payload) Encode() ([]byte, error) {
	if len(p.Uids) != len(p.Values) {
		return nil, fmt.Errorf("crv4: uids/values length mismatch: %d != %d", len(p.Uids), len(p.Values))
	}
	out := make([]byte, 0, 1+32+5+2*len(p.Uids)+5+2*len(p.Values)+8)
	out = appendCompact(out, 32)
	out = append(out, p.Hotkey[:]...)
	out = appendCompact(out, uint64(len(p.Uids)))
	for _, u := range p.Uids {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	out = appendCompact(out, uint64(len(p.Values)))
	for _, v := range p.Values {
		out = binary.LittleEndian.AppendUint16(out, v)
	}
	out = binary.LittleEndian.AppendUint64(out, p.VersionKey)
	return out, nil
}

// appendCompact appends the SCALE compact encoding of v.
func appendCompact(out []byte, v uint64) []byte {
	switch {
	case v < 1<<6:
		return append(out, byte(v<<2))
	case v < 1<<14:
		return binary.LittleEndian.AppendUint16(out, uint16(v<<2)|0b01)
	case v < 1<<30:
		return binary.LittleEndian.AppendUint32(out, uint32(v<<2)|0b10)
	default:
		n := 0
		for tmp := v; tmp > 0; tmp >>= 8 {
			n++
		}
		out = append(out, byte(n-4)<<2|0b11)
		for i := 0; i < n; i++ {
			out = append(out, byte(v>>(8*i)))
		}
		return out
	}
}
