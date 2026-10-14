// Independent producers and hardware adapters validate the exact unsigned
// roster before selecting a signing key. This is not signature verification.
package payoutartifact

import "context"

// The signer and schema must already be independently selected. Signature
// bytes are cleared by the shared digest implementation, preserving its format.
func (self WholeWorkAuthority) SigningDigest(ctx context.Context) ([32]byte, error) {
	return self.digest(ctx)
}
