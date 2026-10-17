// The joined command dispatcher preserves the explicit custody boundary for
// conservation observations and both retained Claim archive commands.
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Each public command refuses before its own input parser or any retained
// state access when the caller has supplied no durable-volume declaration.
func TestEconomicConservationAndClaimArchivesRequireExplicitDurableCustody(t *testing.T) {
	for _, command := range []string{"observe-economic-conservation", "monitor-claim-archive", "monitor-claim-catalog"} {
		var output, diagnostic bytes.Buffer
		code := runMain(context.Background(), []string{command}, &output, &diagnostic)
		if code != 2 || output.Len() != 0 || !strings.HasPrefix(diagnostic.String(), "durable custody declaration:") {
			t.Fatal("command bypassed durable custody admission", command, code, diagnostic.String())
		}
	}
}
