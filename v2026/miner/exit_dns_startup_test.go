//go:build linux || darwin

package miner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/urnetwork/connect/v2026"
)

// Providers in some countries fail the quality check's URL loads at DNS
// through the tunnel, and their logs never said why. A run with a direct slot
// now checks the DNS over HTTPS servers clients use from this host, and a
// network that answers none is told in the log, the diagnostics and the
// status file. The run's authentication waits until the check has told it. A
// run of proxy slots, whose exits leave through their proxies, checks nothing.
func TestProviderRunTellsOfBlockedExitDns(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	t.Setenv(minerStateSubstitutedDirEnv, "")
	t.Setenv(minerStateSubstitutedProblemEnv, "")
	asked := minerTestExitDns(t, map[string]bool{})
	findingLog := &minerTestSignalWriter{text: "DNS lookups through this device fail", said: make(chan struct{})}
	stop := errors.New("synthetic stop after authentication")
	hooks := providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error {
		select {
		case <-findingLog.said:
		case <-time.After(10 * time.Second):
			t.Error("the exit DNS check never told the log")
		}
		return stop
	}}
	settings := fixture.settings(true)
	settings.findingLog = findingLog
	settings.exitDnsCheck = &minerExitDnsCheckSettings{FirstCheckDelay: 0, CheckInterval: time.Hour, QueryTimeout: 5 * time.Second}
	recorder := &providerDiagnosticRecorder{records: make(chan []byte, 64)}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), recorder); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	if log := findingLog.String(); !strings.Contains(log, "provider: ! Clients' DNS lookups through this device fail.") || !strings.Contains(log, "provider proxy add <host:port>") {
		t.Fatalf("log:\n%s", log)
	}
	records := drainProviderTestRecords(t, recorder)
	if record := findProviderTestRecord(records, minerFindingExitDnsBlocked); record == nil || record.Domain != "runtime" || !strings.Contains(record.Guidance, "DNS over HTTPS") {
		t.Fatalf("records %+v", records)
	}
	status, err := readMinerLocalStatus()
	if err != nil || len(status.Findings) != 1 || status.Findings[0].Code != minerFindingExitDnsBlocked {
		t.Fatalf("status file findings: %+v (%v)", status, err)
	}

	// proxy slots: nothing is asked
	*asked = (*asked)[:0]
	slot := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.10:1080", Auth: &proxy.Auth{User: "synthetic-user", Password: "synthetic-password"}}
	proxied := fixture.settings(true, slot)
	proxied.findingLog = &providerTestFindingLog{}
	proxied.exitDnsCheck = &minerExitDnsCheckSettings{FirstCheckDelay: 0, CheckInterval: time.Hour, QueryTimeout: 5 * time.Second}
	plainHooks := providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return stop }}
	ctx, cancel = context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := proxied.run(context.WithValue(ctx, providerRegistrationHooksKey{}, plainHooks), &providerRefusedWriter{}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	if len(*asked) != 0 {
		t.Fatalf("a run of proxy slots checked the host's exit DNS: %v", *asked)
	}
}
