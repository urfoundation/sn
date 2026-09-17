// The original activation owns its copied VPK independently of destination API sessions.
package validator

import (
	"crypto/ed25519"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Constructed only from the complete authenticated startup census. Values and
// signing bytes are copied before runtime callbacks can retain the source.
type releaseAttemptUploadSourceV2 struct {
	activation           protocol.ValidatorEvidenceActivation
	maximumIntentSeconds uint64
	privateKey           [ed25519.PrivateKeySize]byte
}
