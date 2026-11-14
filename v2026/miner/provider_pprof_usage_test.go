// The --pprof grammar and help (urnetwork/connect#227). These tests use only
// the command grammar, so they also run against a miner without --pprof.
package miner

import (
	"strings"
	"testing"
)

// --pprof is off unless it is given, and provide and auth-provide take it.
func TestMainUsagePprofIsOffUnlessGiven(t *testing.T) {
	for _, arguments := range [][]string{
		{"provide"},
		{"provide", "--port=8080", "--max-memory=8gib", "-v"},
		{"auth-provide", "synthetic-code"},
	} {
		opts := parseArgsForTest(t, arguments)
		if value, declared := opts["--pprof"]; !declared || value != nil {
			t.Fatalf("parse %v: --pprof declared %v, value %#v; want declared and unset (nil)", arguments, declared, value)
		}
	}
	for _, c := range []struct {
		arguments []string
		want      string
	}{
		{[]string{"provide", "--pprof=6060"}, "6060"},
		{[]string{"provide", "--max-memory=8gib", "--pprof=127.0.0.1:6060", "-v"}, "127.0.0.1:6060"},
		{[]string{"auth-provide", "synthetic-code", "--pprof=0.0.0.0:6060"}, "0.0.0.0:6060"},
	} {
		opts := parseArgsForTest(t, c.arguments)
		if value, err := opts.String("--pprof"); err != nil || value != c.want {
			t.Fatalf("parse %v: --pprof = %q, %v; want %q", c.arguments, value, err, c.want)
		}
	}
}

// Every operator child would ask for the same address, so --all-operators
// refuses --pprof by name instead of starting.
func TestAllOperatorsRefusesPprof(t *testing.T) {
	err := operatorArgsError([]string{"provide", "--all-operators", "--pprof=6060"})
	if err == nil || !strings.Contains(err.Error(), "--all-operators excludes --pprof") {
		t.Fatalf("provide --all-operators --pprof=6060: %v", err)
	}
}

// provider provide --help documents the endpoint, its loopback default and
// the SIGUSR1 goroutine dump; a command without --pprof doesn't list it.
func TestCliProvideHelpDocumentsPprof(t *testing.T) {
	for _, command := range []string{"provide", "auth-provide"} {
		stdout := cliHelpFor([]string{command, "--help"})
		// help wraps descriptions to the terminal, so compare single-spaced
		text := strings.Join(strings.Fields(stdout), " ")
		for _, want := range []string{"--pprof=<addr>", "/debug/pprof/", "Off by default", "127.0.0.1 only", "0.0.0.0:6060", "kill -USR1 <pid>", "goroutine dump", "keeps running"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s --help does not say %q:\n%s", command, want, stdout)
			}
		}
	}
	if stdout := cliHelpFor([]string{"wallet", "--help"}); strings.Contains(stdout, "--pprof") {
		t.Fatalf("wallet --help lists --pprof:\n%s", stdout)
	}
}
