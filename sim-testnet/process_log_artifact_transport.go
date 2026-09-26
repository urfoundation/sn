// Artifact stream transport resets remain strict release failures. Their
// exact typed diagnostic lets a provisional owner finish collecting evidence.
package main

import (
	"net/netip"
	"strconv"
	"strings"
)

// Accept only the handler's complete quoted tcp reset, including its matching
// read/write operation. Additional storage, integrity or deadline causes fail.
func processLogArtifactTransportReset(text string) bool {
	prefix, encoded, found := strings.Cut(text, " context=<nil> error=")
	if !found || !processLogArtifactRequestCancellation(prefix+" context=context canceled error=context canceled") {
		return false
	}
	cause, err := strconv.Unquote(encoded)
	if err != nil {
		return false
	}
	operation, endpoints, found := strings.Cut(cause, " tcp ")
	if !found || operation != "read" && operation != "write" {
		return false
	}
	endpoints, found = strings.CutSuffix(endpoints, ": "+operation+": connection reset by peer")
	if !found {
		return false
	}
	source, destination, found := strings.Cut(endpoints, "->")
	if !found {
		return false
	}
	for _, endpoint := range []string{source, destination} {
		address, err := netip.ParseAddrPort(endpoint)
		if err != nil || address.Port() == 0 || address.String() != endpoint {
			return false
		}
	}
	return true
}
