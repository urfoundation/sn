package main

// The aggregate gate's dependency mode, as the release builder's: pinned seals
// the run, local tests the working checkouts as they are. These tests run the
// gate's own text, with only external commands replaced by recorders.

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const releaseGateLocalPassLine = "[release-1.0] local release gate passed (local mode: working checkouts, not sealed)"
const releaseGatePinnedPassLine = "[release-1.0] local release gate passed"

// One contiguous fragment of the local gate, from its first marker up to, but
// excluding, its end marker; an empty end marker takes the rest of the script.
func releaseGateLocalFragment(t *testing.T, start, end string) string {
	t.Helper()
	script := readReleaseGateLocalScript(t)
	first := strings.Index(script, start)
	if first < 0 || strings.Count(script, start) != 1 {
		t.Fatalf("local gate lacks one fragment start %q", start)
	}
	if end == "" {
		return script[first:]
	}
	last := strings.Index(script[first:], end)
	if last < 0 {
		t.Fatalf("local gate fragment %q has no end %q", start, end)
	}
	return script[first : first+last]
}

// The environment without an inherited mode, so each case sets its own.
func releaseGateEnvironmentWithoutMode(extra ...string) []string {
	environment := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "RELEASE_GATE_DEPENDENCY_MODE=") || strings.HasPrefix(entry, "release_gate_dependency_mode=")
	})
	return append(environment, extra...)
}

// Run the gate's mode selection alone and report the mode it selected.
func runReleaseGateModeSelection(t *testing.T, extra ...string) (string, string, error) {
	t.Helper()
	block := releaseGateLocalFragment(t, `release_gate_requested_dependency_mode="${RELEASE_GATE_DEPENDENCY_MODE:-pinned}"`, "\n# Release qualification cannot hide")
	command := exec.Command("bash", "-c", "set -euo pipefail\n"+block+`
[[ ! -v RELEASE_GATE_DEPENDENCY_MODE ]]
printf 'selected %s\n' "$release_gate_requested_dependency_mode"
`)
	command.Env = releaseGateEnvironmentWithoutMode(extra...)
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	return string(output), stderr.String(), err
}

// Run the gate's initial preflight block with every preflight recorded rather
// than executed, and return the recorded label and command of each.
func recordReleaseGatePreflights(t *testing.T, mode string, diagnostic bool) []string {
	t.Helper()
	block := releaseGateLocalFragment(t, "# The ordinary entry stays strict", "\nif ! command -v forge")
	// A diagnostic owner never exits zero, even when every preflight passes.
	flag, status := "0", 0
	if diagnostic {
		flag, status = "1", 2
	}
	fixture := newReleaseGateJobsFixture(t, `
release_gate_dependency_mode=`+mode+`; export release_gate_dependency_mode
release_gate_diagnostic=`+flag+`; export release_gate_diagnostic
release_gate_record_preflight() { printf '%s\n' "$*" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/preflights"; }
export -f release_gate_record_preflight
`+block+`
release_gate_complete
`)
	fixture.join(t, status)
	recorded, err := os.ReadFile(filepath.Join(fixture.root, "preflights"))
	if err != nil {
		t.Fatal(err)
	}
	sn, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(fixture.root, "workspace")
	calls := strings.Split(strings.TrimSpace(strings.NewReplacer(sn, "SN", workspace, "WORKSPACE").Replace(string(recorded))), "\n")
	return calls
}

// The gate's preflight calls in each mode, as today's pinned gate makes them.
func releaseGateExpectedPreflights(mode string, diagnostic bool) []string {
	var calls []string
	if diagnostic {
		calls = append(calls, "repository-inventory release_gate_diagnostic_inventory "+mode)
	}
	calls = append(calls,
		"source-freeze SN/scripts/check-release-source-freeze.sh --dependency-mode "+mode+" WORKSPACE",
		"source-integrity bash -c set -euo pipefail; release_gate_source_integrity_preflight",
		"binding-toolchain SN/stabi/generate.sh --preflight",
	)
	if mode == "pinned" {
		calls = append(calls,
			"runtime-source SN/scripts/check-runtime-v454-source.sh",
			"runtime-metadata SN/scripts/check-runtime-metadata-artifacts.sh",
		)
	}
	return calls
}

// Run the gate's final fences and verdict after passing phases, with Git, Go
// and the source snapshot recorded, over an sn and a vault checkout.
func runReleaseGateFinalFences(t *testing.T, mode string) ([]string, string) {
	t.Helper()
	tail := releaseGateLocalFragment(t, "# Join every admitted phase before inspecting the source again", "")
	sn := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sn, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	checker := "#!/bin/sh\nprintf 'snapshot\\n' >> \"$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-events\"\nprintf 'frozen\\n'\n"
	if err := os.WriteFile(filepath.Join(sn, "scripts", "check-release-source-freeze.sh"), []byte(checker), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := newReleaseGateJobsFixture(t, `
go() { printf 'go %s\n' "$*" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-events"; }
git() { printf 'git %s\n' "$*" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-events"; }
sn_repo='`+sn+`'
release_repos=(sn vault)
release_source_snapshot=frozen
release_gate_dependency_mode=`+mode+`; export release_gate_dependency_mode
`+tail)
	fixture.join(t, 0)
	events, err := os.ReadFile(filepath.Join(fixture.root, "fence-events"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	workspace := filepath.Join(fixture.root, "workspace")
	return strings.Split(strings.TrimSpace(strings.ReplaceAll(string(events), workspace, "WORKSPACE")), "\n"), fixture.output.String()
}

// The skip that each mode gives the aggregate job, from its own assignment.
func releaseGateAggregateSkipFor(t *testing.T, mode string) string {
	t.Helper()
	block := releaseGateLocalFragment(t, "release_gate_aggregate_skip='"+releaseGateAggregatePinnedSkip+"'", "release_phase_sn_all_normal() {")
	output, err := exec.Command("bash", "-c", "set -euo pipefail\nrelease_gate_dependency_mode="+mode+"\n"+block+`printf '%s' "$release_gate_aggregate_skip"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

// The last line the gate itself prints, before its owner's retained-state note.
func releaseGateVerdictLine(output string) string {
	verdict := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "[release-1.0] ") {
			verdict = line
		}
	}
	return verdict
}

// Local mode skips the source freeze's provenance snapshots, the release-lock
// comparison as a final fence and, with the vault-discovery test, in every job
// that would run it, and both runtime attestations. Every other preflight,
// fence and job runs, and every place the script consults its mode is one of
// these.
func TestReleaseGateLocalModeSkipsExactlyTheSealingChecks(t *testing.T) {
	for _, diagnostic := range []bool{false, true} {
		if got, want := recordReleaseGatePreflights(t, "local", diagnostic), releaseGateExpectedPreflights("local", diagnostic); !slices.Equal(got, want) {
			t.Errorf("local preflights (diagnostic=%t) = %q, want %q", diagnostic, got, want)
		}
	}
	events, _ := runReleaseGateFinalFences(t, "local")
	if want := []string{"git -C WORKSPACE/sn diff --check", "git -C WORKSPACE/sn diff --cached --check"}; !slices.Equal(events, want) {
		t.Errorf("local final fences = %q, want patch hygiene alone, without the vault", events)
	}
	if got := releaseGateAggregateSkipFor(t, "local"); got != releaseGateAggregateLocalSkip {
		t.Errorf("local aggregate skip = %q, want %q", got, releaseGateAggregateLocalSkip)
	}
	var consulted []string
	for _, line := range strings.Split(readReleaseGateLocalScript(t), "\n") {
		if strings.Contains(line, "release_gate_dependency_mode") {
			consulted = append(consulted, strings.TrimSpace(line))
		}
	}
	const local = `if [[ "$release_gate_dependency_mode" == local ]]; then`
	want := []string{
		`release_gate_dependency_mode="$release_gate_requested_dependency_mode"`,
		"export release_gate_dependency_mode",
		`release_gate_record_preflight repository-inventory release_gate_diagnostic_inventory "$release_gate_dependency_mode" || release_gate_preflight_refusal "$?"`,
		`release_source_snapshot="$(release_gate_record_preflight source-freeze "$sn_repo/scripts/check-release-source-freeze.sh" --dependency-mode "$release_gate_dependency_mode" "$workspace")" || release_gate_preflight_refusal "$?"`,
		local,
		local,
		`if [[ "$release_gate_diagnostic" == 1 && "$release_gate_dependency_mode" == pinned ]]; then`,
		`if [[ "$release_gate_dependency_mode" == local && "$repo" == vault ]]; then`,
		local,
		local,
	}
	if !slices.Equal(consulted, want) {
		t.Errorf("the local gate consults its mode at %q, want exactly %q", consulted, want)
	}
}

// Unset, empty and pinned all select pinned, which keeps every preflight, both
// final fences and the aggregate job's pinned skip, as before.
func TestReleaseGatePinnedModeAndUnsetDefaultAreUnchanged(t *testing.T) {
	for _, value := range []string{"", "RELEASE_GATE_DEPENDENCY_MODE=", "RELEASE_GATE_DEPENDENCY_MODE=pinned"} {
		var extra []string
		if value != "" {
			extra = append(extra, value)
		}
		output, stderr, err := runReleaseGateModeSelection(t, extra...)
		want := "[release-1.0] dependency mode pinned: sealed, clean, fetched, at-origin checkouts and the release lock\nselected pinned\n"
		if err != nil || output != want || stderr != "" {
			t.Errorf("mode selection %q = %q %q %v, want pinned", value, output, stderr, err)
		}
	}
	for _, diagnostic := range []bool{false, true} {
		if got, want := recordReleaseGatePreflights(t, "pinned", diagnostic), releaseGateExpectedPreflights("pinned", diagnostic); !slices.Equal(got, want) {
			t.Errorf("pinned preflights (diagnostic=%t) = %q, want %q", diagnostic, got, want)
		}
	}
	events, _ := runReleaseGateFinalFences(t, "pinned")
	want := []string{
		"git -C WORKSPACE/sn diff --check", "git -C WORKSPACE/sn diff --cached --check",
		"git -C WORKSPACE/vault diff --check", "git -C WORKSPACE/vault diff --cached --check",
		"go test ./sim-testnet -run ^TestReleaseLockMatchesCheckout$ -count=1", "snapshot",
	}
	if !slices.Equal(events, want) {
		t.Errorf("pinned final fences = %q, want %q", events, want)
	}
	if got := releaseGateAggregateSkipFor(t, "pinned"); got != releaseGateAggregatePinnedSkip {
		t.Errorf("pinned aggregate skip = %q, want %q", got, releaseGateAggregatePinnedSkip)
	}
}

// Any other value is refused by the real entry before test storage, the job
// owner or any preflight exists.
func TestReleaseGateRefusesAnUnknownDependencyMode(t *testing.T) {
	for _, value := range []string{"sealed", "Local", "PINNED", "local ", "pinned,local"} {
		root := t.TempDir()
		temporary := filepath.Join(root, "tmp")
		if err := os.Mkdir(temporary, 0o700); err != nil {
			t.Fatal(err)
		}
		command := exec.Command("bash", "../scripts/test-release-1.0-local.sh")
		command.Env = releaseGateEnvironmentWithoutMode("RELEASE_GATE_DEPENDENCY_MODE="+value, "SN_TEST_STORAGE_ROOT="+filepath.Join(root, "storage"), "TMPDIR="+temporary)
		var stdout, stderr strings.Builder
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		if command.ProcessState == nil || command.ProcessState.ExitCode() != 1 || stdout.String() != "" || stderr.String() != "RELEASE_GATE_DEPENDENCY_MODE must be pinned or local\n" {
			t.Fatalf("mode %q was not refused alone: %v\nstdout=%q\nstderr=%q", value, err, stdout.String(), stderr.String())
		}
		for _, path := range []string{filepath.Join(root, "storage"), filepath.Join(temporary, "urnetwork-release-gate.*")} {
			if matches, _ := filepath.Glob(path); len(matches) != 0 {
				t.Fatalf("mode %q created %s before its refusal", value, matches[0])
			}
		}
	}
	output, stderr, err := runReleaseGateModeSelection(t, "RELEASE_GATE_DEPENDENCY_MODE=local")
	if err != nil || output != strings.Join(releaseGateLocalModeSelectionLines, "\n")+"\nselected local\n" || stderr != "" {
		t.Fatalf("local mode selection = %q %q %v", output, stderr, err)
	}
}

// Local mode runs no Git command in a vault path, with or without one, and its
// repository preflights pass in a workspace that has none, even when the other
// checkouts are dirty, unpublished and re-pointed.
func TestReleaseGateLocalModeNeverTouchesTheVault(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	log := filepath.Join(shim, "git.log")
	wrapper := "#!/bin/sh\nprintf '%s %s\\n' \"$PWD\" \"$*\" >> '" + log + "'\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	vault := filepath.Join(workspace, "vault")
	freeze := func() string {
		t.Helper()
		output, err := exec.Command("../scripts/check-release-source-freeze.sh", "--dependency-mode", "local", workspace).CombinedOutput()
		if err != nil {
			t.Fatalf("local source freeze refused: %v\n%s", err, output)
		}
		return string(output)
	}
	freeze()
	if err := os.WriteFile(filepath.Join(workspace, "server", "README.md"), []byte("working change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, filepath.Join(workspace, "sdk"), "config", "remote.origin.url", "git@github.com:someone/sdk.git")
	if err := os.WriteFile(filepath.Join(workspace, "sn", "README.md"), []byte("unpublished\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, filepath.Join(workspace, "sn"), "commit", "-qam", "unpublished local change")
	if err := os.RemoveAll(vault); err != nil {
		t.Fatal(err)
	}
	freeze()
	inventory, err := exec.Command("bash", "-c", `set -euo pipefail; source ../scripts/release-gate-jobs.sh; workspace="$1"; release_gate_diagnostic_inventory local`, "bash", workspace).CombinedOutput()
	if err != nil {
		t.Fatalf("local inventory required a vault: %v\n%s", err, inventory)
	}
	if output, err := exec.Command("bash", "-c", `set -euo pipefail; source ../scripts/release-gate-jobs.sh; workspace="$1"; release_gate_diagnostic_inventory`, "bash", workspace).CombinedOutput(); err == nil || !strings.Contains(string(output), "incomplete diagnostic Git checkout: vault") {
		t.Fatalf("pinned inventory accepted a missing vault: %v\n%s", err, output)
	}
	// The gate's own preflight block: the repository preflights run as
	// written; the sourceguard and binding-tool preflights read no checkout.
	block := releaseGateLocalFragment(t, "# The ordinary entry stays strict", "\nif ! command -v forge")
	for diagnostic, status := range map[string]int{"0": 0, "1": 2} {
		fixture := newReleaseGateJobsFixture(t, `
workspace='`+workspace+`'
release_gate_dependency_mode=local; export release_gate_dependency_mode
release_gate_diagnostic=`+diagnostic+`; export release_gate_diagnostic
eval "release_gate_record_original() $(declare -f release_gate_record_preflight | tail -n +2)"
release_gate_record_preflight() {
  case "$1" in
    source-integrity | binding-toolchain) printf '%s\n' "$1" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/not-repository"; return 0 ;;
  esac
  release_gate_record_original "$@"
}
export -f release_gate_record_preflight release_gate_record_original
`+block+`
status=0; release_gate_complete || status=$?
exit "$status"
`)
		fixture.join(t, status)
		root := releaseGateDiagnosticRoot(t, fixture.root)
		labels := []string{"source-freeze"}
		if diagnostic == "1" {
			labels = append(labels, "repository-inventory")
		}
		for _, label := range labels {
			if got := releaseGateDiagnosticRead(t, filepath.Join(root, "preflights", label, "exit")); got != "0\n" {
				t.Fatalf("local %s preflight (diagnostic=%s) failed without a vault: %q\n%s", label, diagnostic, got, fixture.output.String())
			}
		}
		for _, label := range []string{"runtime-source", "runtime-metadata"} {
			if _, err := os.Stat(filepath.Join(root, "preflights", label)); !os.IsNotExist(err) {
				t.Fatalf("local mode ran the %s attestation: %v", label, err)
			}
		}
		if got := releaseGateDiagnosticRead(t, filepath.Join(fixture.root, "not-repository")); got != "source-integrity\nbinding-toolchain\n" {
			t.Fatalf("local mode changed the other preflights: %q", got)
		}
	}
	events, _ := runReleaseGateFinalFences(t, "local")
	calls := releaseGateDiagnosticRead(t, log)
	if !strings.Contains(calls, filepath.Join(workspace, "server")+" ls-files --cached") {
		t.Fatalf("local source freeze did not inventory the other checkouts:\n%s", calls)
	}
	for _, line := range append(strings.Split(calls, "\n"), events...) {
		if strings.Contains(line, vault) || strings.Contains(line, "WORKSPACE/vault") {
			t.Fatalf("local mode ran Git in the vault: %s", line)
		}
	}
}

// The run states its mode first and last: a local pass names local mode, and
// a pinned pass keeps the sealed gate's unchanged line.
func TestReleaseGateFinalLineNamesTheDependencyMode(t *testing.T) {
	for _, testCase := range []struct{ mode, start, verdict string }{
		{mode: "local", start: "[release-1.0] dependency mode local: the working checkouts as they are, not sealed", verdict: releaseGateLocalPassLine},
		{mode: "pinned", start: "[release-1.0] dependency mode pinned: sealed, clean, fetched, at-origin checkouts and the release lock", verdict: releaseGatePinnedPassLine},
	} {
		output, _, err := runReleaseGateModeSelection(t, "RELEASE_GATE_DEPENDENCY_MODE="+testCase.mode)
		if err != nil || !strings.HasPrefix(output, testCase.start+"\n") {
			t.Errorf("%s mode starts with %q, %v", testCase.mode, output, err)
		}
		_, fences := runReleaseGateFinalFences(t, testCase.mode)
		if got := releaseGateVerdictLine(fences); got != testCase.verdict {
			t.Errorf("%s mode ends with %q, want %q\n%s", testCase.mode, got, testCase.verdict, fences)
		}
	}
}

// Local mode keeps the module inventory and the tidy checks of every checkout
// but the vault, and checks no provenance: dirty, untracked, re-pointed and
// unpublished checkouts pass, and it emits no snapshot. Pinned is unchanged.
func TestReleaseSourceFreezeLocalModeKeepsModuleChecksWithoutProvenance(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	freeze := func(args ...string) (string, string, error) {
		t.Helper()
		command := exec.Command("../scripts/check-release-source-freeze.sh", append(args, workspace)...)
		var stderr strings.Builder
		command.Stderr = &stderr
		output, err := command.Output()
		return string(output), stderr.String(), err
	}
	pinned, _, err := freeze()
	if err != nil {
		t.Fatal(err)
	}
	if explicit, _, err := freeze("--dependency-mode", "pinned"); err != nil || explicit != pinned || len(strings.Split(strings.TrimSpace(pinned), "\n")) != 14 {
		t.Fatalf("explicit pinned mode differs from the default: %v\n%s\n%s", err, explicit, pinned)
	}
	if err := os.WriteFile(filepath.Join(workspace, "server", "README.md"), []byte("working change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "connect", "untracked.txt"), []byte("untracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, filepath.Join(workspace, "sdk"), "config", "remote.origin.url", "git@github.com:someone/sdk.git")
	if err := os.WriteFile(filepath.Join(workspace, "sn", "README.md"), []byte("unpublished\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, filepath.Join(workspace, "sn"), "commit", "-qam", "unpublished local change")
	if _, _, err := freeze(); err == nil {
		t.Fatal("pinned mode accepted unsealed checkouts")
	}
	output, stderr, err := freeze("--dependency-mode", "local")
	if err != nil || output != "" {
		t.Fatalf("local mode refused or sealed its working checkouts: %v\n%s\n%s", err, output, stderr)
	}
	var verified []string
	for _, line := range strings.Split(stderr, "\n") {
		if module, found := strings.CutPrefix(line, "verify tidy Go module: "); found {
			verified = append(verified, module)
		}
	}
	if !slices.Equal(verified, releaseSourceFreezeLiveModules) {
		t.Fatalf("local mode verified %q tidy, want %q", verified, releaseSourceFreezeLiveModules)
	}
	extra := filepath.Join(workspace, "proxy", "extra")
	if err := os.MkdirAll(extra, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "go.mod"), []byte("module example.invalid/proxy-extra\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, filepath.Join(workspace, "proxy"), "add", "extra/go.mod")
	if _, stderr, err := freeze("--dependency-mode", "local"); err == nil || !strings.Contains(stderr, "release Go module inventory differs") {
		t.Fatalf("local mode accepted an unclassified module: %v\n%s", err, stderr)
	}
	runTestGit(t, filepath.Join(workspace, "proxy"), "rm", "-q", "--cached", "extra/go.mod")
	if err := os.RemoveAll(extra); err != nil {
		t.Fatal(err)
	}
	releaseSourceFreezeUntidyModule(t, workspace, "connect/sctp")
	if _, stderr, err := freeze("--dependency-mode", "local"); err == nil || !strings.Contains(stderr, "diff current/go.mod tidy/go.mod") {
		t.Fatalf("local mode accepted an untidy live module: %v\n%s", err, stderr)
	}
	if err := os.RemoveAll(filepath.Join(workspace, "glog")); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := freeze("--dependency-mode", "local"); err == nil || !strings.Contains(stderr, "release repository is missing or not a Git checkout: "+filepath.Join(workspace, "glog")) {
		t.Fatalf("local mode accepted a missing checkout: %v\n%s", err, stderr)
	}
	for _, args := range [][]string{{"--dependency-mode", "sealed"}, {"--dependency-mode"}} {
		command := exec.Command("../scripts/check-release-source-freeze.sh", args...)
		if output, err := command.CombinedOutput(); command.ProcessState == nil || command.ProcessState.ExitCode() != 2 || !strings.Contains(string(output), "pinned or local") {
			t.Fatalf("source freeze %q was not refused: %v\n%s", args, err, output)
		}
	}
}
