// Package hotkeywallet keeps a miner's global hotkey wallet consent and
// delegates the miner's network on each operator to it, so one coldkey
// signature pays the miner on every operator.
//
// The consent is a chain of generations. The coldkey and the hotkey both sign
// each generation once, and a later generation replaces the coldkey. The chain
// is kept under <base>/hotkey-wallet. Each operator stores the chain, and the
// hotkey alone signs a delegation of the miner's network there to the chain's
// head.
//
// Sign signs only these two statement kinds. EnsureDelegation signs an
// operator's challenge only after the protocol decoder accepts it and it names
// exactly what was requested. A hostile operator therefore cannot obtain a
// signature that delegates another network, adopts another consent head, earns
// over other epochs or approves anything else.
package hotkeywallet

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The statement that extends the chain by one generation, for the coldkey and
// then the hotkey to sign. Its generation and previous hash come from the
// verified chain, which it must continue with the same subnet and hotkey and a
// later from epoch.
func NextStatement(chain []protocol.HotkeyWalletMappingConsent, subnet protocol.HotkeyWalletMappingSubnet, hotkey, coldkey [32]byte, fromEpoch, throughEpoch uint64, now time.Time) (*protocol.HotkeyWalletMappingStatement, error) {
	statement := protocol.HotkeyWalletMappingStatement{
		Schema:       protocol.HotkeyWalletMappingConsentSchema,
		Scope:        protocol.HotkeyWalletMappingScope,
		Subnet:       subnet,
		Hotkey:       hotkey,
		Coldkey:      coldkey,
		Generation:   1,
		IssuedAt:     now.Unix(),
		FromEpoch:    fromEpoch,
		ThroughEpoch: throughEpoch,
	}
	if len(chain) > 0 {
		head, headHash, err := protocol.VerifyHotkeyWalletMappingLineage(context.Background(), chain)
		if err != nil {
			return nil, fmt.Errorf("hotkey wallet chain: %w", err)
		}
		if head.Subnet != subnet || head.Hotkey != hotkey {
			return nil, errors.New("the hotkey wallet chain belongs to another hotkey or subnet")
		}
		if fromEpoch <= head.FromEpoch {
			return nil, fmt.Errorf("from epoch %d must be after the previous generation's from epoch %d", fromEpoch, head.FromEpoch)
		}
		statement.Generation, statement.PreviousHash = head.Generation+1, headHash
	}
	if _, err := rand.Read(statement.Nonce[:]); err != nil {
		return nil, err
	}
	// every field rule is the protocol's
	if _, err := statement.Message(); err != nil {
		return nil, err
	}
	return &statement, nil
}

// The sr25519 signature over "<Bytes>" + message + "</Bytes>", as polkadot-js
// wallets sign it. The wrapper keeps the signature from ever being a valid
// extrinsic payload. Only a global hotkey consent or a hotkey delegation is
// signed. A sign-in challenge, another consent kind or any other text is
// refused, so this is never a general-purpose signer.
func Sign(key *crv4.Keypair, message string) ([64]byte, error) {
	if key == nil {
		return [64]byte{}, errors.New("hotkey wallet signing needs a key")
	}
	if _, err := protocol.DecodeHotkeyWalletMappingStatement(message); err != nil {
		if _, err := protocol.DecodeHotkeyNetworkDelegationStatement(message); err != nil {
			return [64]byte{}, fmt.Errorf("%w: refusing to sign a message that is neither a hotkey wallet consent nor a hotkey delegation", protocol.ErrWalletMappingIntegrity)
		}
	}
	signature, err := key.Sign([]byte("<Bytes>" + message + "</Bytes>"))
	if err != nil {
		return [64]byte{}, err
	}
	if len(signature) != 64 {
		return [64]byte{}, fmt.Errorf("sr25519 signature has %d bytes", len(signature))
	}
	return [64]byte(signature), nil
}
