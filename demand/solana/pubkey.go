// Package solana is the demand deposit funder's minimal Solana client: base58
// public keys and program-derived addresses, the JSON-RPC calls it needs
// (balances, accounts, simulation, signature statuses and broadcast), parsing
// and signing of the transactions Jupiter builds, and compiling the v0
// transaction of the LayerZero bridge send.
package solana

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
)

// PublicKey is a 32-byte Solana address. It marshals to and from JSON as its
// base58 string, so it can key maps in persisted state.
type PublicKey [32]byte

var (
	SystemProgram          = MustPublicKey("11111111111111111111111111111111")
	TokenProgram           = MustPublicKey("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
	Token2022Program       = MustPublicKey("TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb")
	AssociatedTokenProgram = MustPublicKey("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
	ComputeBudgetProgram   = MustPublicKey("ComputeBudget111111111111111111111111111111")
)

// ParsePublicKey decodes a base58 address of exactly 32 bytes.
func ParsePublicKey(s string) (PublicKey, error) {
	b, err := Base58Decode(s)
	if err != nil {
		return PublicKey{}, err
	}
	if len(b) != 32 {
		return PublicKey{}, fmt.Errorf("solana: address %q decodes to %d bytes, not 32", s, len(b))
	}
	return PublicKey(b), nil
}

// MustPublicKey is ParsePublicKey for constants; it panics on a bad address.
func MustPublicKey(s string) PublicKey {
	p, err := ParsePublicKey(s)
	if err != nil {
		panic(err)
	}
	return p
}

func (p PublicKey) String() string { return Base58Encode(p[:]) }

// Bytes is the key as a slice, for seeds and encoders.
func (p PublicKey) Bytes() []byte { return p[:] }

// IsZero reports the all-zero key, which is never a real account here.
func (p PublicKey) IsZero() bool { return p == PublicKey{} }

func (p PublicKey) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

func (p *PublicKey) UnmarshalText(text []byte) error {
	parsed, err := ParsePublicKey(string(text))
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// PublicKeyOf is the address of an ed25519 private key.
func PublicKeyOf(key ed25519.PrivateKey) PublicKey {
	var p PublicKey
	copy(p[:], key.Public().(ed25519.PublicKey))
	return p
}

var (
	curveP = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	// d = -121665/121666 mod p
	curveD = func() *big.Int {
		inv := new(big.Int).ModInverse(big.NewInt(121666), curveP)
		d := new(big.Int).Mul(big.NewInt(-121665), inv)
		return d.Mod(d, curveP)
	}()
	curvePMinus2   = new(big.Int).Sub(curveP, big.NewInt(2))
	curveHalfOrder = new(big.Int).Rsh(new(big.Int).Sub(curveP, big.NewInt(1)), 1)
)

// IsOnCurve reports whether b decompresses to an ed25519 point, with the
// semantics of curve25519-dalek's CompressedEdwardsY::decompress that Solana
// uses: the sign bit is ignored, y is taken mod p, and the point exists when
// (y²−1)/(d·y²+1) is a square (zero included).
func IsOnCurve(b [32]byte) bool {
	var be [32]byte
	for i := range b {
		be[31-i] = b[i]
	}
	be[0] &= 0x7f
	y := new(big.Int).SetBytes(be[:])
	y.Mod(y, curveP)
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, curveP)
	u := new(big.Int).Sub(y2, big.NewInt(1))
	u.Mod(u, curveP)
	v := new(big.Int).Mul(curveD, y2)
	v.Add(v, big.NewInt(1))
	v.Mod(v, curveP)
	// v is never zero: -1/d is not a square mod p.
	x2 := new(big.Int).Exp(v, curvePMinus2, curveP)
	x2.Mul(x2, u)
	x2.Mod(x2, curveP)
	if x2.Sign() == 0 {
		return true
	}
	return new(big.Int).Exp(x2, curveHalfOrder, curveP).Cmp(big.NewInt(1)) == 0
}

// FindProgramAddress is Pubkey::find_program_address: the first bump from 255
// down whose hash is off the curve.
func FindProgramAddress(seeds [][]byte, program PublicKey) (PublicKey, uint8, error) {
	if len(seeds) > 15 {
		return PublicKey{}, 0, errors.New("solana: more than 15 seeds")
	}
	for _, seed := range seeds {
		if len(seed) > 32 {
			return PublicKey{}, 0, fmt.Errorf("solana: seed of %d bytes exceeds 32", len(seed))
		}
	}
	for bump := 255; bump >= 0; bump-- {
		h := sha256.New()
		for _, seed := range seeds {
			h.Write(seed)
		}
		h.Write([]byte{byte(bump)})
		h.Write(program[:])
		h.Write([]byte("ProgramDerivedAddress"))
		var candidate PublicKey
		copy(candidate[:], h.Sum(nil))
		if !IsOnCurve(candidate) {
			return candidate, uint8(bump), nil
		}
	}
	return PublicKey{}, 0, errors.New("solana: no viable bump seed")
}

// AssociatedTokenAddress is owner's associated token account for mint under
// the classic SPL Token program.
func AssociatedTokenAddress(owner, mint PublicKey) (PublicKey, error) {
	address, _, err := FindProgramAddress([][]byte{owner[:], TokenProgram[:], mint[:]}, AssociatedTokenProgram)
	return address, err
}
