package miner

// main_test.go — parses representative argv against the real usage
// string, so the docopt patterns for the subnet subcommands cannot
// silently rot. A no-op help handler keeps parse failures as returned
// errors instead of process exits.

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"
	"golang.org/x/net/proxy"
)

func TestProviderClientJwtPathIsStableAndSecretFree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	direct, err := providerClientJwtPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	if direct != filepath.Join(home, ".urnetwork", ".provider.jwt") {
		t.Fatalf("direct path = %s", direct)
	}

	first := &connect.ProxySettings{
		Network: "tcp",
		Address: "proxy.example:1080",
		Auth:    &proxy.Auth{User: "first-user", Password: "first-secret"},
	}
	second := &connect.ProxySettings{
		Network: "tcp",
		Address: "proxy.example:1080",
		Auth:    &proxy.Auth{User: "other-user", Password: "other-secret"},
	}
	firstPath, err := providerClientJwtPath(first)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := providerClientJwtPath(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstPath != secondPath {
		t.Fatalf("credential rotation changed provider identity path: %s != %s", firstPath, secondPath)
	}
	if firstPath == direct || filepath.Base(firstPath) == ".provider.jwt" {
		t.Fatalf("proxy path did not receive a distinct identity: %s", firstPath)
	}
}

// parseArgsForTest parses argv against mainUsage without exiting on
// error.
func parseArgsForTest(t *testing.T, argv []string) docopt.Opts {
	t.Helper()
	parser := &docopt.Parser{
		HelpHandler: docopt.NoHelpHandler,
	}
	opts, err := parser.ParseArgs(mainUsage(), argv, "test")
	if err != nil {
		t.Fatalf("parse %v: %s", argv, err)
	}
	return opts
}

func TestMainUsageProvideWallet(t *testing.T) {
	opts := parseArgsForTest(t, []string{"provide", "--wallet=5Grw"})
	if provide_, _ := opts.Bool("provide"); !provide_ {
		t.Fatalf("provide not set")
	}
	if coldkeySs58, err := opts.String("--wallet"); err != nil || coldkeySs58 != "5Grw" {
		t.Fatalf("--wallet %q err %v", coldkeySs58, err)
	}
	// the wallet command must not fire from the --wallet option
	if wallet, _ := opts.Bool("wallet"); wallet {
		t.Fatalf("wallet command set by --wallet option")
	}

	// --wallet stays optional
	opts = parseArgsForTest(t, []string{"provide"})
	if _, err := opts.String("--wallet"); err == nil {
		t.Fatalf("--wallet unexpectedly set")
	}
}

func TestTestEgressSourceIPBindsControlAndExitDialer(t *testing.T) {
	opts := parseArgsForTest(t, []string{"provide", "--test-egress-source-ip=127.64.0.6"})
	dial, err := testEgressDialContext(opts)
	if err != nil || dial == nil {
		t.Fatalf("test egress dialer = %v, %v", dial, err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dial.DialContext(ctx, "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("source-bound dial: %v", err)
	}
	defer conn.Close()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	remote, ok := accepted.RemoteAddr().(*net.TCPAddr)
	if !ok || remote.IP.String() != "127.64.0.6" {
		t.Fatalf("observed source = %v, want 127.64.0.6", accepted.RemoteAddr())
	}

	invalid := parseArgsForTest(t, []string{"provide", "--test-egress-source-ip=198.51.100.1"})
	if _, err := testEgressDialContext(invalid); err == nil {
		t.Fatal("non-loopback test source IP was accepted")
	}
	plain := parseArgsForTest(t, []string{"provide"})
	if got, err := testEgressDialContext(plain); err != nil || got != nil {
		t.Fatalf("ordinary provide installed test dialer: %v, %v", got, err)
	}
}

func TestMainUsageAuthProvideWallet(t *testing.T) {
	opts := parseArgsForTest(t, []string{"auth-provide", "code", "--wallet=5Grw"})
	if authProvide, _ := opts.Bool("auth-provide"); !authProvide {
		t.Fatalf("auth-provide not set")
	}
	if coldkeySs58, err := opts.String("--wallet"); err != nil || coldkeySs58 != "5Grw" {
		t.Fatalf("--wallet %q err %v", coldkeySs58, err)
	}
}

func TestMainUsageWalletSet(t *testing.T) {
	opts := parseArgsForTest(t, []string{"wallet", "set", "5Grw"})
	if wallet, _ := opts.Bool("wallet"); !wallet {
		t.Fatalf("wallet not set")
	}
	if set, _ := opts.Bool("set"); !set {
		t.Fatalf("set not set")
	}
	if coldkeySs58, err := opts.String("<coldkey_ss58>"); err != nil || coldkeySs58 != "5Grw" {
		t.Fatalf("<coldkey_ss58> %q err %v", coldkeySs58, err)
	}
	if provide_, _ := opts.Bool("provide"); provide_ {
		t.Fatalf("provide set")
	}
}

func TestMainUsageClaim(t *testing.T) {
	opts := parseArgsForTest(t, []string{"claim", "--epoch=12", "--rpc=http://a", "--rpc=http://b", "--dry-run"})
	if claim_, _ := opts.Bool("claim"); !claim_ {
		t.Fatalf("claim not set")
	}
	if epochStr, err := opts.String("--epoch"); err != nil || epochStr != "12" {
		t.Fatalf("--epoch %q err %v", epochStr, err)
	}
	if dryRun, _ := opts.Bool("--dry-run"); !dryRun {
		t.Fatalf("--dry-run not set")
	}
	rpcUrls, ok := opts["--rpc"].([]string)
	if !ok || len(rpcUrls) != 2 || rpcUrls[0] != "http://a" || rpcUrls[1] != "http://b" {
		t.Fatalf("--rpc %v", opts["--rpc"])
	}

	// all claim options stay optional
	opts = parseArgsForTest(t, []string{"claim"})
	if claim_, _ := opts.Bool("claim"); !claim_ {
		t.Fatalf("claim not set")
	}
	if _, err := opts.String("--epoch"); err == nil {
		t.Fatalf("--epoch unexpectedly set")
	}
	if rpcUrls, ok := opts["--rpc"].([]string); ok && 0 < len(rpcUrls) {
		t.Fatalf("--rpc unexpectedly set: %v", rpcUrls)
	}
}

func TestMainUsageFleetLifecycle(t *testing.T) {
	manifest := parseArgsForTest(t, []string{"fleet", "manifest", "--manifest=fleet.json"})
	if ok, _ := manifest.Bool("fleet"); !ok {
		t.Fatal("fleet command not parsed")
	}
	if ok, _ := manifest.Bool("manifest"); !ok {
		t.Fatal("manifest command not parsed")
	}
	publish := parseArgsForTest(t, []string{"fleet", "publish", "--manifest=fleet.json", "--substrate=ws://one", "--substrate=ws://two", "--hotkey_seed_file=hot.seed"})
	if got := publish["--substrate"].([]string); len(got) != 2 {
		t.Fatalf("substrate failover = %v", got)
	}
	bind := parseArgsForTest(t, []string{"fleet", "bind", "--manifest=fleet.json", "--client_id=0x01", "--client_seed_file=client.seed", "--hotkey_seed_file=hot.seed", "--valid_from_epoch=2", "--valid_to_epoch=10", "--rpc=http://one", "--relayer_key_file=relay.key", "--dry-run"})
	if ok, _ := bind.Bool("bind"); !ok {
		t.Fatal("bind command not parsed")
	}
	if dry, _ := bind.Bool("--dry-run"); !dry {
		t.Fatal("bind dry-run not parsed")
	}
	status := parseArgsForTest(t, []string{"fleet", "status", "--manifest=fleet.json", "--client_id=0x01", "--substrate=ws://one", "--rpc=http://one"})
	if ok, _ := status.Bool("status"); !ok {
		t.Fatal("status command not parsed")
	}
	revoke := parseArgsForTest(t, []string{"fleet", "revoke", "--manifest=fleet.json", "--client_id=0x01", "--client_seed_file=client.seed", "--effective_epoch=3", "--rpc=http://one", "--relayer_key_file=relay.key"})
	if ok, _ := revoke.Bool("revoke"); !ok {
		t.Fatal("revoke command not parsed")
	}
}

func TestMainUsageChooseNetwork(t *testing.T) {
	opts := parseArgsForTest(t, []string{"choose_network", "https://example.com", "wss://example.com"})
	if chooseNetwork, _ := opts.Bool("choose_network"); !chooseNetwork {
		t.Fatalf("choose_network not set")
	}
	apiUrl, err := opts.String("<api_url>")
	if err != nil || apiUrl != "https://example.com" {
		t.Fatalf("<api_url> = %q, err %v", apiUrl, err)
	}
	connectUrl, err := opts.String("<connect_url>")
	if err != nil || connectUrl != "wss://example.com" {
		t.Fatalf("<connect_url> = %q, err %v", connectUrl, err)
	}
}

func TestMainUsageChooseNetworkReset(t *testing.T) {
	opts := parseArgsForTest(t, []string{"choose_network", "--reset"})
	if chooseNetwork, _ := opts.Bool("choose_network"); !chooseNetwork {
		t.Fatalf("choose_network not set")
	}
	if reset, _ := opts.Bool("--reset"); !reset {
		t.Fatalf("--reset not set")
	}
	if show, _ := opts.Bool("--show"); show {
		t.Fatalf("--show set by --reset")
	}
}

func TestMainUsageChooseNetworkShow(t *testing.T) {
	opts := parseArgsForTest(t, []string{"choose_network", "--show"})
	if chooseNetwork, _ := opts.Bool("choose_network"); !chooseNetwork {
		t.Fatalf("choose_network not set")
	}
	if show, _ := opts.Bool("--show"); !show {
		t.Fatalf("--show not set")
	}
	if reset, _ := opts.Bool("--reset"); reset {
		t.Fatalf("--reset set by --show")
	}
}

// TestMainUsageChooseNetworkRejectsBadForms: the mutually exclusive
// forms must stay mutually exclusive, and the two-URL form must require
// both URLs.
func TestMainUsageChooseNetworkRejectsBadForms(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	for _, argv := range [][]string{
		{"choose_network"},
		{"choose_network", "https://example.com"},
		{"choose_network", "--reset", "--show"},
		{"choose_network", "--reset", "https://example.com", "wss://example.com"},
	} {
		if _, err := parser.ParseArgs(mainUsage(), argv, "test"); err == nil {
			t.Errorf("parse %v: expected an error, got nil", argv)
		}
	}
}
