// These source-graph regressions resolve modules without importing clientauth,
// so a stale sibling override produces an assertion instead of a build failure.
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

// The registration and refresh API used by SN must not depend on ../sdk HEAD.
func TestClientauthSourceGraphPinsSdkWithoutSiblingOverride(t *testing.T) {
	module := resolvedSourceModule(t, "github.com/urnetwork/sdk/v2026")
	if module.Path != "github.com/urnetwork/sdk/v2026" || module.Replace == nil || module.Replace.Path != "github.com/urnetwork/sdk/v2026" || module.Replace.Version != "v0.0.0-20261001021058-5d37be3876e5" {
		t.Fatalf("clientauth SDK resolved outside the reviewed registration source: %+v replacement=%+v", module, module.Replace)
	}
}

// The SDK pin alone is insufficient: redirect ownership and exhaustion causes
// require the matching Connect source, even when an older sibling is clean.
func TestClientauthSourceGraphPinsConnectWithoutSiblingOverride(t *testing.T) {
	module := resolvedSourceModule(t, "github.com/urnetwork/connect/v2026")
	if module.Path != "github.com/urnetwork/connect/v2026" || module.Replace == nil || module.Replace.Path != "github.com/urnetwork/connect/v2026" || module.Replace.Version != "v0.0.0-20261001021459-e1b5d77b5029" {
		t.Fatalf("clientauth Connect resolved outside the reviewed transport source: %+v replacement=%+v", module, module.Replace)
	}
}

// A versioned Connect requirement does not inherit its fork replacement. Keep
// the effective SCTP source at that same commit, without a mutable local path.
func TestClientauthSourceGraphPinsReviewedSctpFork(t *testing.T) {
	module := resolvedSourceModule(t, "github.com/pion/sctp")
	if module.Path != "github.com/pion/sctp" || module.Replace == nil || module.Replace.Path != "github.com/urnetwork/connect/v2026/sctp" || module.Replace.Version != "v0.0.0-20261001021459-e1b5d77b5029" {
		t.Fatalf("clientauth SCTP resolved outside the reviewed Connect fork: %+v replacement=%+v", module, module.Replace)
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
