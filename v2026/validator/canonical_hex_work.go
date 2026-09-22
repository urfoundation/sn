package validator

// Observes actual lowercase work with invocation-owned metadata only. Public
// entrypoints use the zero value; no observer, input or verdict is retained.

import "strings"

// A containing call may count bytes at the real normalization boundary.
type canonicalHexWork struct {
	normalized func(int)
}

// Always performs the original normalization, including malformed UTF-8.
func (self canonicalHexWork) lower(encoded string) string {
	if self.normalized != nil {
		self.normalized(len(encoded))
	}
	return strings.ToLower(encoded)
}

// Prepared extrinsic expectations historically permit uppercase, unlike the
// canonical artifact fields. This keeps that separate normalization boundary.
func normalizeReleasePreparedHex32(encoded string, work canonicalHexWork) string {
	// A wrong-width input cannot normalize to an accepted ASCII hex identity.
	if len(encoded) != 66 {
		return ""
	}
	return work.lower(encoded)
}
