package miner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/hotkeyauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

// Prompts that fail the test: every input here is on the command line.
func testNoLoginPrompts(t *testing.T) loginPrompts {
	return loginPrompts{
		password: func() (string, error) {
			t.Error("prompted for a password")
			return "", errors.New("unexpected password prompt")
		},
		authCode: func() (string, error) {
			t.Error("prompted for an auth code")
			return "", errors.New("unexpected auth code prompt")
		},
	}
}

func TestMainUsageOperatorCommands(t *testing.T) {
	provide := parseArgsForTest(t, []string{"provide", "--all-operators", "--operators-url=https://list.example/operators.yml", "--operators-refresh=2h", "--auto-register", "--hotkey_seed_file=/synthetic/hotkey.seed", "--port=8080", "--max-memory=2gib", "--allow-client-registration", "-vv"})
	for _, flag := range []string{"provide", "--all-operators", "--auto-register", "--allow-client-registration"} {
		if value, err := provide.Bool(flag); err != nil || !value {
			t.Fatalf("all-operators provide lost %s: %v, %v", flag, value, err)
		}
	}
	for option, want := range map[string]string{
		"--operators-url":     "https://list.example/operators.yml",
		"--operators-refresh": "2h",
		"--hotkey_seed_file":  "/synthetic/hotkey.seed",
		"--port":              "8080",
		"--max-memory":        "2gib",
	} {
		if value, err := provide.String(option); err != nil || value != want {
			t.Fatalf("all-operators provide %s = %q, %v; want %q", option, value, err, want)
		}
	}
	if verbosity, ok := provide["-v"].(int); !ok || verbosity != 2 {
		t.Fatalf("all-operators provide verbosity = %#v", provide["-v"])
	}
	// the hotkey alone is accepted for the hotkey wallet
	parseArgsForTest(t, []string{"provide", "--all-operators", "--hotkey_seed_file=/synthetic/hotkey.seed"})
	if port, err := parseArgsForTest(t, []string{"provide", "--all-operators"}).String("--port"); err != nil || port != "0" {
		t.Fatalf("all-operators provide port default = %q, %v", port, err)
	}

	for _, arguments := range [][]string{
		{"auth", "--operator=alpha.example", "synthetic-code"},
		{"auth", "--operator=alpha.example", "--user_auth=synthetic-user", "--password=synthetic-password", "-f"},
		{"auth", "--operator=alpha.example", "--hotkey_seed_file=/synthetic/hotkey.seed", "--operators-url=https://list.example/operators.yml"},
	} {
		auth := parseArgsForTest(t, arguments)
		if selected, _ := auth.Bool("auth"); !selected {
			t.Fatalf("parse %v: auth not selected", arguments)
		}
		if domain, err := auth.String("--operator"); err != nil || domain != "alpha.example" {
			t.Fatalf("parse %v: --operator = %q, %v", arguments, domain, err)
		}
	}
	operators := parseArgsForTest(t, []string{"operators", "--operators-url=https://list.example/operators.yml", "-v"})
	if selected, _ := operators.Bool("operators"); !selected {
		t.Fatal("operators command not selected")
	}

	// the single-operator forms keep their meaning
	single := parseArgsForTest(t, []string{"auth", "synthetic-code", "--api_url=https://api.example"})
	if _, err := single.String("--operator"); err == nil {
		t.Fatal("plain auth selected an operator")
	}
	if allOperators, _ := parseArgsForTest(t, []string{"provide", "--api_url=https://api.example"}).Bool("--all-operators"); allOperators {
		t.Fatal("plain provide selected all operators")
	}
}

func TestMainUsageRejectsMixedOperatorForms(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	for _, arguments := range [][]string{
		{"provide", "--all-operators", "--auto-register"},
		{"provide", "--all-operators", "--api_url=https://api.example"},
		{"provide", "--operators-url=https://list.example/operators.yml"},
		{"auth", "--operator=alpha.example", "synthetic-code", "--hotkey_seed_file=/synthetic/hotkey.seed"},
		{"auth", "--operator=alpha.example", "--api_url=https://api.example"},
		{"auth", "--hotkey_seed_file=/synthetic/hotkey.seed"},
		{"operators", "--api_url=https://api.example"},
	} {
		if _, err := parser.ParseArgs(mainUsage(), arguments, "synthetic"); err == nil {
			t.Errorf("parse %v: accepted", arguments)
		}
	}
}

// Each refusal names the options, where docopt would print the whole usage.
func TestOperatorArgsErrorNamesTheExcludedOptions(t *testing.T) {
	for _, excluded := range []string{
		"--api_url=https://api.example",
		"--connect_url=wss://connect.example",
		"--provider-jwt=/synthetic/provider.jwt",
		"--wallet=synthetic-coldkey",
		"--wallet-from-epoch=1",
		"--wallet-through-epoch=2",
		"--coldkey_seed_file=/synthetic/coldkey.seed",
		"--message=synthetic",
		"--signature=00",
		"--test-egress-source-ip=127.0.0.2",
		"--close-report-domain=/synthetic/domain.json",
		"--whole-work-capture=/synthetic/work.json",
		"--whole-work-capture-sha256=sha256:00",
		"--require-whole-work-capture",
		"--original-contract-capture=/synthetic/contracts.json",
		"--original-contract-capture-sha256=sha256:00",
		"--require-original-contract-capture",
		"--adopt-legacy-provider-key",
	} {
		name, _, _ := strings.Cut(excluded, "=")
		err := operatorArgsError([]string{"provide", "--all-operators", excluded})
		if err == nil || !strings.Contains(err.Error(), "--all-operators excludes "+name) {
			t.Errorf("%s with --all-operators: %v", excluded, err)
		}
	}
	for _, c := range []struct {
		arguments []string
		want      string
	}{
		{arguments: []string{"provide", "--all-operators", "--auto-register"}, want: "--auto-register needs --hotkey_seed_file"},
		{arguments: []string{"provide", "--operators-url=https://list.example", "--auto-register"}, want: "--operators-url, --auto-register only applies with --all-operators"},
		{arguments: []string{"auth", "--operator=alpha.example", "--api_url=https://api.example"}, want: "auth --operator excludes --api_url"},
		{arguments: []string{"auth", "synthetic-code", "--hotkey_seed_file=/synthetic/hotkey.seed"}, want: "--hotkey_seed_file only applies with --operator"},
	} {
		if err := operatorArgsError(c.arguments); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v; want %q", c.arguments, err, c.want)
		}
	}
	for _, arguments := range [][]string{
		{"provide", "--all-operators", "--auto-register", "--hotkey_seed_file=/synthetic/hotkey.seed", "--operators-refresh=2h", "--max-memory=2gib", "--port=8080", "--allow-client-registration", "-vv"},
		{"provide", "--api_url=https://api.example", "--wallet=synthetic-coldkey", "--allow-client-registration"},
		{"auth", "--operator=alpha.example", "--hotkey_seed_file=/synthetic/hotkey.seed", "--operators-url=https://list.example"},
		{"auth", "synthetic-code", "--api_url=https://api.example", "--max-memory=1gib"},
		{"operators"},
		{"wallet", "set", "synthetic-coldkey", "--provider-jwt=/synthetic/provider.jwt"},
		{"claim", "--epoch=3"},
	} {
		if err := operatorArgsError(arguments); err != nil {
			t.Errorf("%v refused: %v", arguments, err)
		}
	}
}

func TestOperatorSupervisorSettingsLoadTheHotkeyAndRefuseAMissingSeed(t *testing.T) {
	base := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", base)
	seedPath := writeTestSeedFile(t, testAliceSeedHex)
	opts := parseArgsForTest(t, []string{"provide", "--all-operators", "--auto-register", "--hotkey_seed_file=" + seedPath, "--operators-refresh=2h", "--max-memory=2gib", "-v"})
	settings, listSettings, err := newOperatorSupervisorSettings(opts)
	if err != nil {
		t.Fatal(err)
	}
	if settings.hotkey == nil || settings.hotkey.Address() != testAliceKeypair(t).Address() || !settings.autoRegister {
		t.Fatalf("hotkey not loaded for auto-register")
	}
	if settings.baseStateDir != base || settings.maxMemory != 2*1024*1024*1024 || settings.verbosity != 1 || settings.port != 0 || settings.allowClientRegistration {
		t.Fatalf("settings = %+v", settings)
	}
	if listSettings.Url != "" || listSettings.Interval != 2*time.Hour || listSettings.CachePath != filepath.Join(base, "operators.yml") {
		t.Fatalf("list settings = %+v", listSettings)
	}
	if _, listSettings, err = newOperatorSupervisorSettings(parseArgsForTest(t, []string{"provide", "--all-operators"})); err != nil || listSettings.Interval != time.Hour {
		t.Fatalf("default refresh = %v, %v", listSettings.Interval, err)
	}

	// a client never creates the hotkey
	missing := filepath.Join(filepath.Dir(seedPath), "missing.seed")
	_, _, err = newOperatorSupervisorSettings(parseArgsForTest(t, []string{"provide", "--all-operators", "--auto-register", "--hotkey_seed_file=" + missing}))
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing seed: %v", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing seed file was created: %v", err)
	}
	for _, refresh := range []string{"30s", "soon"} {
		if _, _, err := newOperatorSupervisorSettings(parseArgsForTest(t, []string{"provide", "--all-operators", "--operators-refresh=" + refresh})); err == nil {
			t.Errorf("--operators-refresh=%s accepted", refresh)
		}
	}
}

func TestOperatorAuthWritesTheOperatorJwtWithTheHotkey(t *testing.T) {
	base := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", base)
	seedPath := writeTestSeedFile(t, testAliceSeedHex)
	operator := newTestWalletOperator(t, testAliceKeypair(t))
	operator.setList(t, testListedOperator("alpha.example"), operator.operator("local.example"))
	opts := parseArgsForTest(t, []string{"auth", "--operator=local.example", "--hotkey_seed_file=" + seedPath, "--operators-url=" + operator.listUrl()})
	var out bytes.Buffer
	if err := operatorAuth(t.Context(), opts, testNoLoginPrompts(t), &out); err != nil {
		t.Fatal(err)
	}
	jwtPath := filepath.Join(base, "operators", "local.example", "jwt")
	jwt, err := clientauth.ReadToken(jwtPath)
	if want := "jwt:" + hotkeyauth.NetworkName(testAliceKeypair(t).PublicKey()); err != nil || jwt != want {
		t.Fatalf("operator jwt = %q, %v; want %q", jwt, err, want)
	}
	for _, line := range []string{
		"Created the hotkey's network on local.example",
		"Jwt written to " + jwtPath,
		"its first run needs: provider provide --all-operators --allow-client-registration",
	} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output %q lacks %q", out.String(), line)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "jwt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth --operator wrote the single-operator jwt: %v", err)
	}
	if _, err := operatorlist.Parse(mustReadFile(t, filepath.Join(base, "operators.yml"))); err != nil {
		t.Fatalf("operator list not cached: %v", err)
	}
}

func TestOperatorAuthWritesTheOperatorJwtFromAnAuthCode(t *testing.T) {
	base := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", base)
	operator := newTestWalletOperator(t, nil)
	operator.setList(t, operator.operator("local.example"))
	opts := parseArgsForTest(t, []string{"auth", "--operator=local.example", testAuthCode, "--operators-url=" + operator.listUrl()})
	if err := operatorAuth(t.Context(), opts, testNoLoginPrompts(t), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if jwt, err := clientauth.ReadToken(filepath.Join(base, "operators", "local.example", "jwt")); err != nil || jwt != "jwt:code" {
		t.Fatalf("operator jwt = %q, %v", jwt, err)
	}
}

func TestOperatorAuthRefusesAnUnlistedOperator(t *testing.T) {
	base := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", base)
	operator := newTestWalletOperator(t, nil)
	operator.setList(t, testListedOperator("alpha.example"))
	opts := parseArgsForTest(t, []string{"auth", "--operator=missing.example", testAuthCode, "--operators-url=" + operator.listUrl()})
	err := operatorAuth(t.Context(), opts, testNoLoginPrompts(t), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `"missing.example" is not in the operator list`) {
		t.Fatalf("unlisted operator: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "operators")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an unlisted operator got a directory: %v", err)
	}
}

func TestPrintOperatorsShowsCredentialStateSourceAndDigest(t *testing.T) {
	base := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", base)
	operator := newTestWalletOperator(t, nil)
	digest := operator.setList(t, testListedOperator("alpha.example"), testListedOperator("beta.example"))
	if err := clientauth.WriteToken(operatorJwtPath(base, "alpha.example"), "synthetic-jwt"); err != nil {
		t.Fatal(err)
	}
	opts := parseArgsForTest(t, []string{"operators", "--operators-url=" + operator.listUrl()})
	var out bytes.Buffer
	if err := printOperators(t.Context(), opts, &out); err != nil {
		t.Fatal(err)
	}
	table := "" +
		"alpha.example  jwt present    https://api.alpha.example  wss://connect.alpha.example\n" +
		"beta.example   awaiting auth  https://api.beta.example   wss://connect.beta.example\n" +
		"authenticate an operator awaiting auth with: provider auth --operator=<domain>\n"
	if want := "source: fetched " + operator.listUrl() + "\ndigest: " + digest + "\n" + table; out.String() != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out.String(), want)
	}

	// unreachable, the list comes from the cache with the same digest
	operator.setListOnline(false)
	out.Reset()
	if err := printOperators(t.Context(), opts, &out); err != nil {
		t.Fatal(err)
	}
	source, rest, _ := strings.Cut(out.String(), "\n")
	fetchedAt, ok := strings.CutPrefix(source, "source: cached "+filepath.Join(base, "operators.yml")+", fetched ")
	if _, err := time.Parse(time.RFC3339, fetchedAt); !ok || err != nil {
		t.Fatalf("cached source line = %q", source)
	}
	if want := "digest: " + digest + "\n" + table; rest != want {
		t.Fatalf("cached output:\n%s\nwant:\n%s", rest, want)
	}
}

// --user_auth without --password logged in with an empty password: an absent
// option is not a string, and only an empty string prompted.
func TestNetworkLoginPromptsForAnAbsentPassword(t *testing.T) {
	operator := newTestWalletOperator(t, nil)
	prompts := testNoLoginPrompts(t)
	prompted := 0
	prompts.password = func() (string, error) {
		prompted++
		return "synthetic-password", nil
	}
	opts := parseArgsForTest(t, []string{"auth", "--user_auth=synthetic-user"})
	byJwt, err := networkLogin(t.Context(), opts, operator.server.URL, prompts)
	if err != nil || byJwt != "jwt:password" {
		t.Fatalf("login = %q, %v", byJwt, err)
	}
	if passwords := operator.receivedPasswords(); prompted != 1 || len(passwords) == 0 || passwords[len(passwords)-1] != "synthetic-password" {
		t.Fatalf("prompted %d times; operator received %q", prompted, passwords)
	}
}

// A login answer without a network dereferenced nil.
func TestNetworkLoginRefusesAnAnswerWithoutANetwork(t *testing.T) {
	operator := newTestWalletOperator(t, nil)
	operator.passwordNoNetwork = true
	opts := parseArgsForTest(t, []string{"auth", "--user_auth=synthetic-user", "--password=synthetic-password"})
	if byJwt, err := networkLogin(t.Context(), opts, operator.server.URL, testNoLoginPrompts(t)); err == nil {
		t.Fatalf("login without a network = %q", byJwt)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
