// Native multisig accounts have no private key. This implements the exact
// pallet-multisig41 derivation in the reviewed SDK cacb431, independently of
// SS58 presentation, EVM addresses and any signing or runtime authority.
package crv4

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/blake2b"
)

// The caller supplies the canonical public signer set. Rejecting unsorted or
// duplicate inputs prevents a descriptor from displaying another account than
// the actual native call. No sorting mutates the caller's admitted authority.
func DeriveNativeMultisigAccount(signatories [][32]byte, threshold uint16) ([32]byte, error) {
	if len(signatories) < 2 || len(signatories) > 100 || threshold < 2 || int(threshold) > len(signatories) {
		return [32]byte{}, errors.New("native multisig requires two through100 signatories and a reachable threshold of at least two")
	}
	encoded := appendCompact([]byte("modlpy/utilisuba"), uint64(len(signatories)))
	for i, account := range signatories {
		if account == ([32]byte{}) || i > 0 && bytes.Compare(signatories[i-1][:], account[:]) >= 0 {
			return [32]byte{}, errors.New("native multisig signatories must be nonzero, unique and canonically sorted")
		}
		encoded = append(encoded, account[:]...)
	}
	encoded = binary.LittleEndian.AppendUint16(encoded, threshold)
	return blake2b.Sum256(encoded), nil
}
