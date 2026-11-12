package miner

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/sdk"
)

// These use only entry points that predate the extender's reasons: the
// extender listener with its diagnostics record, and provider status over a
// status file, so they fail on the tree before the fix rather than not build.

// The defect: an extender that couldn't bind tcp 443 logged only
// listen_failed:true, with no reason and no fix (provider_diagnostics.go).
func TestExtenderBindFailureLogsTheReasonAndTheFix(t *testing.T) {
	cases := []struct {
		name   string
		status *sdk.ExtenderProvideStatus
		reason string
		says   []string
	}{
		{
			name: "permission denied",
			status: &sdk.ExtenderProvideStatus{
				Supported:           true,
				State:               sdk.ExtenderProvideStateError,
				ErrorCase:           sdk.ExtenderProvideErrorTcpUnavailable,
				TcpUnavailableError: "listen tcp :443: bind: permission denied",
				ListenError:         "tcp: listen tcp :443: bind: permission denied",
			},
			reason: "tcp_443_permission_denied",
			says:   []string{"The extender is off", "TCP port 443", "every 3 minutes", "https://ur.xyz/docs/miner/extender"},
		},
		{
			name: "in use",
			status: &sdk.ExtenderProvideStatus{
				Supported:           true,
				State:               sdk.ExtenderProvideStateError,
				ErrorCase:           sdk.ExtenderProvideErrorTcpUnavailable,
				TcpUnavailableError: "listen tcp :443: bind: address already in use",
				ListenError:         "tcp: listen tcp :443: bind: address already in use",
			},
			reason: "tcp_443_in_use",
			says:   []string{"another program on this host holds TCP port 443", "Only one process per host"},
		},
		{
			name: "unreachable",
			status: &sdk.ExtenderProvideStatus{
				Supported:             true,
				State:                 sdk.ExtenderProvideStateError,
				ErrorCase:             sdk.ExtenderProvideErrorActivationUnreachable,
				Enabled:               true,
				Listening:             true,
				LastActivationTime:    time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC).UnixMilli(),
				LastActivationError:   "the tcp carrier did not answer within 5s at 198.51.100.7:443",
				LastActivationRefused: true,
			},
			reason: "unreachable_on_tcp_443",
			says:   []string{"unreachable on TCP 443", "TCP 443, UDP 443 and UDP 4053", "firewall", "security group", "router", "restart the miner"},
		},
	}
	for _, c := range cases {
		sink := &providerDiagnosticRecorder{records: make(chan []byte, 4)}
		owner := newProviderDiagnosticTestOwner(t, sink)
		listener := newProviderExtenderStatusListener(owner, 0)
		listener.ExtenderProvideStatusChanged(c.status)
		raw := sink.next(t)
		record := struct {
			Domain   string         `json:"domain"`
			Extender map[string]any `json:"extender"`
			Guidance string         `json:"guidance"`
		}{}
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if record.Domain != "extender" || record.Extender["reason"] != c.reason {
			t.Fatalf("%s: the record names no reason %q:\n%s", c.name, c.reason, raw)
		}
		for _, want := range c.says {
			if !strings.Contains(record.Guidance, want) {
				t.Fatalf("%s: the guidance doesn't say %q:\n%s", c.name, want, raw)
			}
		}
		// the SDK's own words, which can carry an address, never enter the log
		if bytes.Contains(raw, []byte("listen tcp")) || bytes.Contains(raw, []byte("198.51.100.")) {
			t.Fatalf("%s: the record carries the SDK's text:\n%s", c.name, raw)
		}
	}
	// a serving, activated extender has nothing to fix
	sink := &providerDiagnosticRecorder{records: make(chan []byte, 4)}
	owner := newProviderDiagnosticTestOwner(t, sink)
	newProviderExtenderStatusListener(owner, 0).ExtenderProvideStatusChanged(&sdk.ExtenderProvideStatus{Supported: true, State: sdk.ExtenderProvideStateActive, Enabled: true, Listening: true, ActivatedV4: true})
	if raw := sink.next(t); bytes.Contains(raw, []byte(`"reason"`)) || bytes.Contains(raw, []byte(`"guidance"`)) {
		t.Fatalf("an active extender got a reason:\n%s", raw)
	}
}

// The defect: provider status read nothing of the extender, so a miner whose
// extender was off for want of tcp 443 looked fully healthy.
func TestStatusShowsTheExtenderReason(t *testing.T) {
	dir := phase2State(t)
	cliRun = func(string, ...string) (string, error) { return "", os.ErrNotExist }
	defer func() { cliRun = defaultCliRun }()
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "kitchen-pi"}))
	writeState(t, dir, ".provider.jwt", fakeJwt(map[string]any{"client_id": "01a15cc0-0000-0000-0000-00000000c21f", "device_id": "x"}))
	now := time.Now().UTC()
	// written as the running miner writes it: the extender beside the members
	raw, _ := json.Marshal(map[string]any{
		"schema":     minerStatusSchema,
		"pid":        os.Getpid(),
		"version":    "2026.11.11-1",
		"started_at": now.Add(-time.Hour),
		"updated_at": now,
		"members":    []map[string]any{{"slot": "direct", "client_id": "01a15cc0-0000-0000-0000-00000000c21f", "lifecycle": "running", "connected": true, "ready": true}},
		"extender":   map[string]any{"bind": "tcp_443_permission_denied", "activation": "none", "reason": "tcp_443_permission_denied"},
	})
	writeState(t, dir, minerStatusFileName, string(raw))

	var out bytes.Buffer
	code := statusCmd(docopt.Opts{}, &out)
	text := out.String()
	if !strings.Contains(text, "Extender") || !strings.Contains(text, "may not bind TCP 443 (permission denied)") {
		t.Fatalf("status says nothing of the extender:\n%s", text)
	}
	if code != 0 {
		t.Fatalf("the optional extender changed the exit code to %d:\n%s", code, text)
	}
	out.Reset()
	statusCmd(docopt.Opts{"--json": true}, &out)
	report := map[string]any{}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	extender, _ := report["extender"].(map[string]any)
	if extender == nil || extender["reason"] != "tcp_443_permission_denied" || extender["bind"] != "tcp_443_permission_denied" {
		t.Fatalf("status --json has no extender reason:\n%s", out.String())
	}
	if report["state"] != "running" {
		t.Fatalf("the extender changed the state:\n%s", out.String())
	}
}
