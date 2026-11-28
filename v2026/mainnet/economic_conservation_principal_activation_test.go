// The public combined owner requires an independently observed drained
// starting boundary even when original replay and principal queries succeed.
package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestEconomicConservationPublicPrincipalRefusesUndrainedActivationWithHealthySiblings(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	// Deliberately conflict with the genuinely drained exported original
	// proof. A current nonzero RPC boundary cannot activate a new window.
	f.source.native.set(t, 100, "PendingServerEmission", nativeExecutionTestWords(1))
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err, diagnostic.String())
	}
	if code != 3 || summary.NativeCurrent || summary.NativeCursor.Number != 100 || summary.OpeningPrincipals != nil || !strings.Contains(summary.NativeIssue, "lacks its original drained activation boundary") || !summary.VaultCurrent || summary.VaultCursor.Number != 11 || f.source.claimReads.Load() == 0 {
		t.Fatal("undrained initial boundary was accepted or stopped healthy sibling observation", code, diagnostic.String(), summary)
	}
}
