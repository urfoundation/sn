//go:build !linux

// Other platforms retain explicit context writers and bounded memory sinks.
// Their descriptor adapters must be qualified before daemon output is admitted.
package diagnostics

import (
	"errors"
	"os"
)

// Refusal is observable exporter state and never invokes an unbounded write.
func newDescriptorSink(*os.File) (ownedSink, error) {
	return nil, errors.New("diagnostic descriptor adapter is unavailable on this platform")
}
