package chain

import (
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic/extensions"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
)

// EncodeSignedCall encodes the exact metadata-driven signed transaction (an
// immortal era, zero tip, no CheckMetadataHash) used by every native write in
// this repository. One encoder keeps a recovery verifier from drifting from the
// executor's wire representation.
func EncodeSignedCall(chain *crv4.Chain, signer signature.KeyringPair, call types.Call, nonce uint32) ([]byte, error) {
	if chain == nil || chain.Meta == nil || chain.Runtime == nil {
		return nil, errors.New("substrate signing context is unavailable")
	}
	ext := extrinsic.NewExtrinsic(call)
	err := ext.Sign(signer, chain.Meta,
		extrinsic.WithEra(types.ExtrinsicEra{IsImmortalEra: true}, chain.GenesisHash),
		extrinsic.WithNonce(types.NewUCompactFromUInt(uint64(nonce))),
		extrinsic.WithTip(types.NewUCompactFromUInt(0)),
		extrinsic.WithSpecVersion(chain.Runtime.SpecVersion),
		extrinsic.WithTransactionVersion(chain.Runtime.TransactionVersion),
		extrinsic.WithGenesisHash(chain.GenesisHash),
		extrinsic.WithMetadataMode(extensions.CheckMetadataModeDisabled, extensions.CheckMetadataHash{Hash: types.NewEmptyOption[types.H256]()}),
	)
	if err != nil {
		return nil, err
	}
	return codec.Encode(ext)
}

// ExtrinsicHash is the Blake2b-256 of the exact SCALE bytes, the identity the
// node reports for a submitted extrinsic.
func ExtrinsicHash(raw []byte) types.Hash {
	return types.Hash(blake2b.Sum256(raw))
}

// LoadKeypairFile loads an existing sr25519 seed file (64 hex chars with an
// optional 0x prefix, or exactly 32 raw bytes) without creating one.
func LoadKeypairFile(path string) (*crv4.Keypair, error) {
	seed, err := crv4.LoadSeedFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return crv4.KeypairFromSeed(seed)
}
