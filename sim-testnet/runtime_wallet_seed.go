package main

import (
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/urfoundation/sn/v2026/crv4"
)

func minerPayoutSeedPath(stateDir string, miner int) string {
	return filepath.Join(stateDir, "secrets", fmt.Sprintf("miner-%d-payout.seed", miner))
}

// Materialize only the existing role, with the same durable, private,
// non-replacing custody used by native keys. A rerender cannot rotate it.
func ensureMinerPayoutSeed(stateDir string, miner int, role SubstrateRoleSecret) error {
	label := fmt.Sprintf("miner-%d-payout", miner)
	seedBytes, err := hex.DecodeString(role.SeedHex)
	if err != nil || len(seedBytes) != 32 || role.Label != label {
		return fmt.Errorf("%s payout role has invalid seed material or identity", label)
	}
	var seed [32]byte
	copy(seed[:], seedBytes)
	key, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return err
	}
	public := key.PublicKey()
	if key.Address() != role.SS58 || hex.EncodeToString(public[:]) != role.PublicKeyHex {
		return fmt.Errorf("%s payout seed differs from its recorded identity", label)
	}
	if err := crv4.EnsureSeedFile(minerPayoutSeedPath(stateDir, miner), seed); err != nil {
		return fmt.Errorf("%s payout seed: %w", label, err)
	}
	return nil
}
