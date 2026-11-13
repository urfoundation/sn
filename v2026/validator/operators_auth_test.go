//go:build linux || darwin

package validator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/hotkeyauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

type operatorAuthTest struct {
	stateDir string
	seedPath string
	listUrl  string
	signIn   *operatorTestSignIn
}

// A listed operator serving the wallet sign-in, a list naming it and a hotkey seed.
func newOperatorAuthTest(t *testing.T) *operatorAuthTest {
	t.Helper()
	signIn, server := newOperatorTestSignIn(t)
	list := newOperatorTestListServer(t, operatorlist.Operator{Domain: "one.example", ApiUrl: server.URL, ConnectUrl: "ws" + strings.TrimPrefix(server.URL, "http")})
	stateDir := operatorTestDir(t)
	seedPath := filepath.Join(operatorTestDir(t), "hotkey.seed")
	writeOperatorTestHotkeySeed(t, seedPath, 13)
	return &operatorAuthTest{stateDir: stateDir, seedPath: seedPath, listUrl: list.URL, signIn: signIn}
}

func (self *operatorAuthTest) run(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	opts, err := parseValidatorArgsForTest(t, append([]string{"auth", "--operator=" + "one.example", "--operators-url=" + self.listUrl, "--state_dir=" + self.stateDir}, args...))
	if err != nil {
		t.Fatal(err)
	}
	return runOperatorAuthTest(t, opts, input)
}

// auth's two steps without the process signal handling.
func runOperatorAuthTest(t *testing.T, opts docopt.Opts, input string) (string, error) {
	t.Helper()
	var output strings.Builder
	target, proceed, err := confirmAuthTarget(t.Context(), opts, strings.NewReader(input), &output)
	if err != nil || !proceed {
		return output.String(), err
	}
	err = writeAuthJwt(t.Context(), opts, target, &output)
	return output.String(), err
}

func TestAuthOperatorWritesTheOperatorsJwt(t *testing.T) {
	test := newOperatorAuthTest(t)
	output, err := test.run(t, "", "--hotkey_seed_file="+test.seedPath, "-f")
	if err != nil {
		t.Fatal(err)
	}
	jwtPath := filepath.Join(test.stateDir, "operators", "one.example", "jwt")
	token, err := clientauth.ReadToken(jwtPath)
	if err != nil || token != "jwt:"+hotkeyauth.NetworkName(operatorTestHotkey(t, 13).PublicKey()) || test.signIn.networkCount() != 1 {
		t.Fatal("auth --operator did not write the hotkey network's JWT", err)
	}
	if info, err := os.Stat(jwtPath); err != nil || info.Mode().Perm() != 0600 || !strings.Contains(output, "Jwt written to "+jwtPath) {
		t.Fatal("auth --operator JWT is not private or not reported", err, output)
	}
	if _, err := os.Stat(filepath.Join(test.stateDir, "operators.yml")); err != nil {
		t.Fatal("auth --operator did not cache the list", err)
	}
	// An existing JWT is replaced only after confirmation.
	if err := clientauth.WriteToken(jwtPath, "synthetic-previous-jwt"); err != nil {
		t.Fatal(err)
	}
	if _, err := test.run(t, "n\n", "--hotkey_seed_file="+test.seedPath); err != nil {
		t.Fatal(err)
	}
	if token, err := clientauth.ReadToken(jwtPath); err != nil || token != "synthetic-previous-jwt" {
		t.Fatal("declined overwrite replaced the JWT", err)
	}
	if _, err := test.run(t, "y\n", "--hotkey_seed_file="+test.seedPath); err != nil {
		t.Fatal(err)
	}
	if token, err := clientauth.ReadToken(jwtPath); err != nil || !strings.HasPrefix(token, "jwt:tao-") {
		t.Fatal("confirmed overwrite kept the old JWT", err)
	}
}

func TestAuthOperatorRefusesAnUnlistedDomain(t *testing.T) {
	test := newOperatorAuthTest(t)
	opts, err := parseValidatorArgsForTest(t, []string{"auth", "--operator=two.example", "--operators-url=" + test.listUrl, "--state_dir=" + test.stateDir, "--hotkey_seed_file=" + test.seedPath, "-f"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runOperatorAuthTest(t, opts, ""); err == nil || !strings.Contains(err.Error(), "two.example") {
		t.Fatal("an unlisted operator was authenticated", err)
	}
	if _, err := os.Stat(filepath.Join(test.stateDir, "operators")); !os.IsNotExist(err) || test.signIn.networkCount() != 0 {
		t.Fatal("an unlisted operator selected a directory or signed in", err)
	}
}

// Regression: a password login answer without a network dereferenced nil, and
// an answer without a JWT ended the command without writing or failing. A
// JSON null decodes to no answer at all, for both login forms.
func TestAuthLoginRefusesAnswersWithoutANetworkJwt(t *testing.T) {
	var answerLock sync.Mutex
	answers := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		answerLock.Lock()
		answer, ok := answers[request.URL.Path]
		answerLock.Unlock()
		if !ok {
			writer.WriteHeader(http.StatusOK)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(answer))
	}))
	defer server.Close()
	list := newOperatorTestListServer(t, operatorlist.Operator{Domain: "one.example", ApiUrl: server.URL, ConnectUrl: "ws" + strings.TrimPrefix(server.URL, "http")})
	for _, testCase := range []struct {
		path   string
		answer string
		login  []string
		writes bool
	}{
		{path: "/auth/login-with-password", answer: `{}`},
		{path: "/auth/login-with-password", answer: `null`},
		{path: "/auth/login-with-password", answer: `{"network":{"name":"synthetic"}}`},
		{path: "/auth/login-with-password", answer: `{"network":{"by_jwt":"synthetic-network-jwt"}}`, writes: true},
		{path: "/auth/code-login", answer: `{}`},
		{path: "/auth/code-login", answer: `null`},
		{path: "/auth/code-login", answer: `{"by_jwt":"synthetic-network-jwt"}`, writes: true},
	} {
		login := []string{"--user_auth=synthetic@one.example", "--password=synthetic"}
		if testCase.path == "/auth/code-login" {
			login = []string{"SYNTHETICCODE"}
		}
		func() {
			answerLock.Lock()
			defer answerLock.Unlock()
			answers = map[string]string{testCase.path: testCase.answer}
		}()
		stateDir := operatorTestDir(t)
		args := append(append([]string{"auth"}, login...), "-f", "--operator=one.example", "--operators-url="+list.URL, "--state_dir="+stateDir)
		opts, err := parseValidatorArgsForTest(t, args)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runOperatorAuthTest(t, opts, "")
		token, readErr := clientauth.ReadToken(filepath.Join(stateDir, "operators", "one.example", "jwt"))
		if testCase.writes {
			if err != nil || readErr != nil || token != "synthetic-network-jwt" {
				t.Fatalf("%s %s: network JWT not written: %v %v", testCase.path, testCase.answer, err, readErr)
			}
		} else if err == nil || !os.IsNotExist(readErr) {
			t.Fatalf("%s %s: an answer without a network JWT was accepted: %v", testCase.path, testCase.answer, err)
		}
	}
}

func TestAuthHotkeyNeverCreatesTheSeed(t *testing.T) {
	test := newOperatorAuthTest(t)
	missing := filepath.Join(operatorTestDir(t), "missing.seed")
	if _, err := test.run(t, "", "--hotkey_seed_file="+missing, "-f"); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatal("a missing hotkey seed was not named", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) || test.signIn.networkCount() != 0 {
		t.Fatal("auth created a hotkey or signed in without one", err)
	}
	if _, err := os.Stat(filepath.Join(test.stateDir, "operators", "one.example", "jwt")); !os.IsNotExist(err) {
		t.Fatal("auth wrote a JWT without signing in", err)
	}
}
