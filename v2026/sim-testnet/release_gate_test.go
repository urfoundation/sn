package main

import (
	"os"
	"strings"
	"testing"
)

// A sibling release module can advance while the long race and Solidity gates
// are running. The final check must cover every module hashed by the release
// lock and occur after all other checked-in gate work.
func TestLocalReleaseGateRechecksCompleteWorkspaceAtEnd(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	const repositories = "release_repos=(sn server connect sdk glog goidenticons proxy userwireguard vault xops config)"
	if !strings.Contains(script, repositories) || !strings.Contains(script, `for repo in "${release_repos[@]}"`) {
		t.Fatal("local release gate does not check every release repository")
	}
	if !strings.Contains(script, `git -C "$workspace/$repo" diff --check`) || !strings.Contains(script, `git -C "$workspace/$repo" diff --cached --check`) {
		t.Fatal("local release gate does not check both unstaged and staged patches")
	}
	if !strings.Contains(script, `"$workspace/server/connect/sim-latency/baseline/verify.sh"`) ||
		!strings.Contains(script, `go list ./... | grep -v '^github\.com/urnetwork/server/connect/sim-latency/baseline/'`) ||
		!strings.Contains(script, `go test "${server_packages[@]}" -run '^$'`) {
		t.Fatal("local release gate does not verify the immutable server baseline and compile every executable package")
	}
	for _, required := range []string{
		"CoreStClientEpochUsesOneFinalizedBlock",
		"StatsAlphaPriceURLIsMainnetOnly",
		"StatsGaugeVecReplaceDeletesStaleSeries",
		"export WARP_ENV=local",
		"export WARP_SERVICE=test",
		"export BRINGYOUR_POSTGRES_HOSTNAME=local-pg.bringyour.com",
		"export BRINGYOUR_REDIS_HOSTNAME=local-redis.bringyour.com",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("local release gate omits operator regression %s", required)
		}
	}
	patchIndex := strings.LastIndex(script, `echo "[release-1.0] patch hygiene"`)
	lockIndex := strings.LastIndex(script, `go test ./sim-testnet -run '^TestReleaseLockMatchesCheckout$' -count=1`)
	passedIndex := strings.LastIndex(script, `echo "[release-1.0] local release gate passed"`)
	if patchIndex < 0 || lockIndex <= patchIndex || passedIndex <= lockIndex {
		t.Fatalf("final release-lock ordering patch=%d lock=%d passed=%d", patchIndex, lockIndex, passedIndex)
	}
}

// Keep enough deadline headroom for the complete launch-scale race suite. The
// measured baseline exceeded 9m40s before the policy-migration regressions were
// added, so restoring Go's 10-minute default would deterministically truncate
// required coverage on this release host.
func TestLocalReleaseGateAllowsCompleteSimulatorRaceSuite(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scriptBytes), "go test -race -timeout 15m ./sim-testnet") {
		t.Fatal("local release gate lacks the reviewed 15-minute full simulator race deadline")
	}
}
