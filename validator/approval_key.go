// Offline approval keys share the existing private seed-file custody reader.
package validator

import (
	"crypto/ed25519"
	"fmt"

	"github.com/urfoundation/sn/crv4"
)

// Loads an existing private raw32 or bare64-hex seed; never generates a key,
// creates directories, or prints key material. Only ASCII edge space is allowed.
func loadApprovalKeyFile(path string) (ed25519.PrivateKey, error) {
	seed, err := crv4.LoadRawOrBareHexSeedFile(expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("approval key: %w", err)
	}
	return ed25519.NewKeyFromSeed(seed[:]), nil
}
