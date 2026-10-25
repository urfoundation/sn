// These source-graph regressions resolve modules without importing clientauth,
// so a wrong sibling or pin produces an assertion instead of a build failure.
package sourcegraph

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Only the effective module identity matters; local replacements can silently
// override a correct requirement and must remain visible to the assertions.
type sourceModule struct {
	Path    string
	Version string
	Dir     string
	Replace *sourceModule
}

// Resolve the checked-in main module with no workspace or network fallback.
// Qualification downloads dependencies first and runs with a frozen go.sum.
func resolvedSourceModule(t *testing.T, modulePath string) sourceModule {
	t.Helper()
	directory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "list", "-mod=readonly", "-m", "-json", modulePath)
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOSUMDB=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve %s without changing source or downloading modules: %v\n%s", modulePath, err, output)
	}
	var module sourceModule
	if err := json.Unmarshal(output, &module); err != nil {
		t.Fatalf("decode resolved %s: %v\n%s", modulePath, err, output)
	}
	return module
}

// A local build takes the module from exactly its sibling checkout beside SN;
// a pinned build takes it from its reviewed version. Any other replacement, or
// none, fails. An empty pin version admits any version of the pinned path, for
// a module whose bytes only go.sum binds.
func requireSiblingOrPin(t *testing.T, label, modulePath, sibling, pinPath, pinVersion string) {
	t.Helper()
	module := resolvedSourceModule(t, modulePath)
	replacement := module.Replace
	if module.Path != modulePath || replacement == nil {
		t.Fatalf("%s is neither a sibling checkout nor pinned: %+v", label, module)
	}
	if replacement.Version == "" {
		directory, err := filepath.Abs(filepath.Join("..", "..", "..", filepath.FromSlash(sibling)))
		if err != nil {
			t.Fatal(err)
		}
		if replacement.Path != "../"+sibling || replacement.Dir != directory {
			t.Fatalf("%s resolved outside its sibling checkout ../%s: replacement=%+v", label, sibling, replacement)
		}
		return
	}
	if replacement.Path != pinPath || (pinVersion != "" && replacement.Version != pinVersion) {
		t.Fatalf("%s resolved outside its reviewed pin: replacement=%+v", label, replacement)
	}
}

// The registration and refresh API used by SN: the SDK sibling in a local
// build, or the reviewed SDK pin in a pinned build.
func TestClientauthSourceGraphSelectsSdkSiblingOrPin(t *testing.T) {
	requireSiblingOrPin(t, "clientauth SDK", "github.com/urnetwork/sdk/v2026", "sdk", "github.com/urnetwork/sdk/v2026", "v0.0.0-20261001021058-5d37be3876e5")
}

// Redirect ownership and exhaustion causes require the matching Connect
// source: the Connect sibling, or the reviewed Connect pin.
func TestClientauthSourceGraphSelectsConnectSiblingOrPin(t *testing.T) {
	requireSiblingOrPin(t, "clientauth Connect", "github.com/urnetwork/connect/v2026", "connect", "github.com/urnetwork/connect/v2026", "v0.0.0-20261001021459-e1b5d77b5029")
}

// A dependency's fork replacement is not inherited. The SCTP source is the fork
// in the Connect sibling, or the fork at the reviewed Connect commit.
func TestClientauthSourceGraphSelectsSctpForkSiblingOrPin(t *testing.T) {
	requireSiblingOrPin(t, "clientauth SCTP", "github.com/pion/sctp", "connect/sctp", "github.com/urnetwork/connect/v2026/sctp", "v0.0.0-20261001021459-e1b5d77b5029")
}

// SN and the server share one gVisor fork: the gvisor sibling, or the
// published fork.
func TestSourceGraphSelectsGvisorForkSiblingOrPin(t *testing.T) {
	requireSiblingOrPin(t, "gVisor", "gvisor.dev/gvisor", "gvisor", "github.com/urnetwork/gvisor", "")
}

// A release build is local or pinned throughout; the release builder refuses a
// graph that mixes sibling checkouts with pins in either mode.
func TestSourceGraphSelectsOneDependencyMode(t *testing.T) {
	forms := map[bool][]string{}
	for _, modulePath := range []string{"github.com/urnetwork/sdk/v2026", "github.com/urnetwork/connect/v2026", "github.com/pion/sctp", "gvisor.dev/gvisor"} {
		module := resolvedSourceModule(t, modulePath)
		local := module.Replace != nil && module.Replace.Version == ""
		forms[local] = append(forms[local], modulePath)
	}
	if len(forms[true]) != 0 && len(forms[false]) != 0 {
		t.Fatalf("source graph mixes sibling checkouts %v with pins %v", forms[true], forms[false])
	}
}

// The fixed substrate transport is tracked within SN, never a sibling checkout.
func TestSourceGraphPinsNestedRpcFork(t *testing.T) {
	module := resolvedSourceModule(t, "github.com/centrifuge/go-substrate-rpc-client/v4")
	directory, err := filepath.Abs(filepath.Join("..", "..", "third_party/go-substrate-rpc-client"))
	if err != nil {
		t.Fatal(err)
	}
	if module.Replace == nil || module.Replace.Path != "./third_party/go-substrate-rpc-client" || module.Replace.Version != "" || module.Replace.Dir != directory {
		t.Fatalf("substrate RPC resolved outside the tracked fork: %+v replacement=%+v", module, module.Replace)
	}
}
