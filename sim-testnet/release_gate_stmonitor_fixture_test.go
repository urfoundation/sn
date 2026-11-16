package main

// server/stmonitor/testfixture's roots need a PostgreSQL admin URL. The
// aggregate job, which has no database, skips them by name; sn-e2e runs them,
// normal and race, on the gate's private services, and the gate sets that URL
// nowhere else.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const releaseGateStmonitorFixtureImport = "github.com/urnetwork/server/v2026/stmonitor/testfixture"

// The test roots of one package's files that reach testfixture.New, directly
// or through any function, method or package value of the package that names
// it. Matching by name over-approximates, so no route is missed.
func releaseGateStmonitorFixtureReachingRoots(paths []string) ([]string, error) {
	references := map[string]map[string]bool{}
	var roots []string
	for _, path := range paths {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		local := ""
		for _, spec := range parsed.Imports {
			if spec.Path.Value != strconv.Quote(releaseGateStmonitorFixtureImport) {
				continue
			}
			local = "testfixture"
			if spec.Name != nil {
				local = spec.Name.Name
			}
			if local == "." {
				return nil, fmt.Errorf("%s dot-imports the stmonitor fixture", path)
			}
		}
		record := func(name string, body ast.Node) {
			if references[name] == nil {
				references[name] = map[string]bool{}
			}
			ast.Inspect(body, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.SelectorExpr:
					if owner, ok := node.X.(*ast.Ident); ok && owner.Name == local {
						references[name]["testfixture."+node.Sel.Name] = true
					}
				case *ast.Ident:
					references[name][node.Name] = true
				}
				return true
			})
		}
		for _, declaration := range parsed.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Body == nil {
					continue
				}
				if declaration.Recv == nil && strings.HasSuffix(path, "_test.go") && strings.HasPrefix(declaration.Name.Name, "Test") {
					roots = append(roots, declaration.Name.Name)
				}
				record(declaration.Name.Name, declaration.Body)
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if value, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range value.Names {
							record(name.Name, value)
						}
					}
				}
			}
		}
	}
	reaching := map[string]bool{"testfixture.New": true}
	for changed := true; changed; {
		changed = false
		for name, named := range references {
			if reaching[name] {
				continue
			}
			for reference := range named {
				if reaching[reference] {
					reaching[name], changed = true, true
					break
				}
			}
		}
	}
	roots = slices.DeleteFunc(roots, func(root string) bool { return !reaching[root] })
	slices.Sort(roots)
	return roots, nil
}

// Every sn package under root, as ./dir, whose default build imports the
// stmonitor fixture, mapped to its roots that reach testfixture.New. Like go,
// it skips testdata, hidden and underscore directories and nested modules.
func releaseGateStmonitorFixtureUsers(root string) (map[string][]string, error) {
	packages := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if matched, err := build.Default.MatchFile(filepath.Dir(path), name); err != nil || !matched {
			return err
		}
		packages[filepath.Dir(path)] = append(packages[filepath.Dir(path)], path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Only a test file, or a file that imports the fixture, can reach it.
	users := map[string][]string{}
	for directory, paths := range packages {
		imports, reachable := false, []string{}
		for _, path := range paths {
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return nil, err
			}
			imported := slices.ContainsFunc(parsed.Imports, func(spec *ast.ImportSpec) bool {
				return spec.Path.Value == strconv.Quote(releaseGateStmonitorFixtureImport)
			})
			if imports = imports || imported; imported || strings.HasSuffix(path, "_test.go") {
				reachable = append(reachable, path)
			}
		}
		if !imports {
			continue
		}
		roots, err := releaseGateStmonitorFixtureReachingRoots(reachable)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, directory)
		if err != nil {
			return nil, err
		}
		users["./"+filepath.ToSlash(relative)] = roots
	}
	return users, nil
}

// Every top-level test root that ./mainnet's default build compiles.
func releaseGateMainnetRoots(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("../mainnet/*_test.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("mainnet test census: %d files, %v", len(paths), err)
	}
	var roots []string
	for _, path := range paths {
		if matched, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || !matched {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		names, err := releaseSourceTestDeclarations(string(source))
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, names...)
	}
	slices.Sort(roots)
	return roots
}

// The fixture's users are exactly the four reviewed mainnet roots.
func releaseGateStmonitorFixtureCensus(t *testing.T) []string {
	t.Helper()
	users, err := releaseGateStmonitorFixtureUsers("..")
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(slices.Values(releaseGateStmonitorFixtureRoots))
	if len(users) != 1 || !slices.Equal(users["./mainnet"], want) {
		t.Fatalf("sn roots reaching testfixture.New = %q, want ./mainnet's %q", users, want)
	}
	return want
}

// The gate sets the admin URL once, in sn-e2e after the private services'
// environment, from their PostgreSQL's endpoint and superuser, and clears an
// inherited value before any job can start.
func verifyReleaseGateStmonitorAdminURL(script string) error {
	var mentions []string
	for _, line := range strings.Split(script, "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, "STMONITOR_TEST_ADMIN_URL") && !strings.HasPrefix(line, "#") {
			mentions = append(mentions, line)
		}
	}
	if !slices.Equal(mentions, []string{"unset STMONITOR_TEST_ADMIN_URL", releaseGateStmonitorAdminURL}) {
		return fmt.Errorf("the gate names the stmonitor admin URL in %q, want only its unset and the private services' URL", mentions)
	}
	unset, jobs := strings.Index(script, "\nunset STMONITOR_TEST_ADMIN_URL\n"), strings.Index(script, "\n"+`source "$sn_repo/scripts/release-gate-jobs.sh"`+"\n")
	if unset < 0 || jobs < 0 || unset > jobs {
		return errors.New("the gate does not clear an inherited stmonitor admin URL before its jobs")
	}
	commands, _, err := releaseGatePhaseCommands(script, "sn_e2e")
	if err != nil {
		return err
	}
	environment, export := slices.Index(commands, `source "$RELEASE_GATE_SERVICE_ENV"`), slices.Index(commands, releaseGateStmonitorAdminURL)
	if environment < 0 || export < environment {
		return errors.New("sn-e2e does not set the stmonitor admin URL from the private services' environment")
	}
	return nil
}

// The fixture's roots are exactly four of ./mainnet, and both modes of the
// aggregate skip, which every ./mainnet shard runs, skip them by name, beside
// the E suite and nothing else of ./mainnet; dropping any of them from either
// mode's skip fails.
func TestReleaseGateStmonitorFixtureRootsLeaveTheAggregateJob(t *testing.T) {
	t.Parallel()
	fixtureRoots := releaseGateStmonitorFixtureCensus(t)
	suite := regexp.MustCompile(releaseGateAggregateE2ESkip)
	for _, mode := range []string{"pinned", "local"} {
		skip := regexp.MustCompile(releaseGateAggregateSkipFor(t, mode))
		for _, root := range releaseGateMainnetRoots(t) {
			if want := suite.MatchString(root) || slices.Contains(fixtureRoots, root); skip.MatchString(root) != want {
				t.Errorf("%s aggregate job skips mainnet root %s: %t, want %t", mode, root, !want, want)
			}
		}
	}
	census, err := releaseGateAggregateSkipCensus("..")
	if err != nil {
		t.Fatal(err)
	}
	script := readReleaseGateLocalScript(t)
	if err := verifyReleaseGateAggregateE2ESkip(script, census); err != nil {
		t.Fatal(err)
	}
	for _, skip := range []string{releaseGateAggregatePinnedSkip, releaseGateAggregateLocalSkip} {
		assignment := "release_gate_aggregate_skip='" + skip + "'"
		if strings.Count(script, assignment) != 1 {
			t.Fatalf("the gate has no one assignment %s", assignment)
		}
		changes := []string{strings.Replace(assignment, "|TestMonitorOperatorCredentialFenceAndPublicationAmbiguity$|TestMonitorOperatorCommandReadsProductionJournalAndRestartsOutage$|TestMonitorStorageStoppedOperatorReleasesOwnersBeforePeerStops$|TestMonitorStorageOperatorLostAckRetainsIncident$", "|TestMonitor", 1)}
		for _, root := range fixtureRoots {
			changes = append(changes, strings.Replace(assignment, "|"+root+"$", "", 1))
		}
		for _, change := range changes {
			if change == assignment {
				t.Fatalf("the mutation leaves %s unchanged", assignment)
			}
			if err := verifyReleaseGateAggregateE2ESkip(strings.Replace(script, assignment, change, 1), census); err == nil {
				t.Fatalf("the aggregate job accepted the skip %s", change)
			}
		}
	}
}

// sn-e2e runs exactly the fixture's roots, normal and race, after the E suite
// in its private-services body, admitted only beside the database suites.
func TestReleaseGateStmonitorFixtureRootsRunOnThePrivateServices(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	group := releaseGateSnE2EGroup(t)
	if err := verifyReleaseGateSnE2E(script, group); err != nil {
		t.Fatal(err)
	}
	fixtureRoots := releaseGateStmonitorFixtureCensus(t)
	selector, err := releaseConnectPolicySelectorAssignment(script, "stmonitor_fixture_tests")
	if err != nil {
		t.Fatal(err)
	}
	selected := regexp.MustCompile(selector)
	if got := slices.DeleteFunc(releaseGateMainnetRoots(t), func(root string) bool { return !selected.MatchString(root) }); !slices.Equal(got, fixtureRoots) {
		t.Fatalf("sn-e2e selects mainnet roots %q for the fixture, want %q", got, fixtureRoots)
	}
	assignment := releaseGateStmonitorFixtureLines[0]
	changes := []struct{ old, new string }{
		{assignment, strings.Replace(assignment, "|TestMonitorStorageOperatorLostAckRetainsIncident", "", 1)},
		{assignment, "stmonitor_fixture_tests='^TestMonitor'"},
		{releaseGateStmonitorAdminURL, ":"},
	}
	for _, command := range releaseGateStmonitorFixtureLines[2:] {
		for _, replacement := range []string{"# " + command, command + " || true", "if false; then\n      " + command + "\n    fi", strings.Replace(command, " -count=1", "", 1)} {
			changes = append(changes, struct{ old, new string }{command, replacement})
		}
	}
	for _, change := range changes {
		if strings.Count(script, change.old) != 1 {
			t.Fatalf("the mutation names no unique text: %s", change.old)
		}
		if err := verifyReleaseGateSnE2E(strings.Replace(script, change.old, change.new, 1), group); err == nil {
			t.Fatalf("sn-e2e accepted %q", change.new)
		}
	}
}

// The admin URL is only the private PostgreSQL's: the services helper's
// published loopback endpoint and its superuser, without TLS as the monitor
// allows only on loopback. An inherited or owner URL never reaches a job, and
// the producer gate, which runs no mainnet tests, neither sets nor needs one.
func TestReleaseGateStmonitorFixtureURLComesOnlyFromThePrivateServices(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	if err := verifyReleaseGateStmonitorAdminURL(script); err != nil {
		t.Fatal(err)
	}
	services, err := os.ReadFile("../../server/local/release-gate-services.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`--publish "127.0.0.1::$port"`,
		"-e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=urnetwork-local-test -e POSTGRES_DB=postgres",
		`printf 'export WARP_TEST_ENV_PORTABLE_POSTGRES_AUTHORITY=%q\n' "$release_gate_postgres_endpoint"`,
	} {
		if !strings.Contains(string(services), required) {
			t.Fatalf("the private services no longer provide %q", required)
		}
	}
	environment := filepath.Join(t.TempDir(), "environment.sh")
	if err := os.WriteFile(environment, []byte("export WARP_TEST_ENV_PORTABLE_POSTGRES_AUTHORITY=127.0.0.1:54321\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "-c", `set -euo pipefail
unset STMONITOR_TEST_ADMIN_URL
[[ ! -v STMONITOR_TEST_ADMIN_URL ]]
source "$1"
`+releaseGateStmonitorAdminURL+`
printf '%s' "$STMONITOR_TEST_ADMIN_URL"`, "bash", environment)
	command.Env = append(os.Environ(), "STMONITOR_TEST_ADMIN_URL=postgres://postgres:owner@127.0.0.1:5432/postgres?sslmode=disable")
	output, err := command.Output()
	if err != nil || string(output) != "postgres://postgres:urnetwork-local-test@127.0.0.1:54321/postgres?sslmode=disable" {
		t.Fatalf("admin URL = %q, %v", output, err)
	}
	parsed, err := url.Parse(string(output))
	if err != nil || net.ParseIP(parsed.Hostname()) == nil || !net.ParseIP(parsed.Hostname()).IsLoopback() || parsed.Port() == "" || parsed.RawQuery != "sslmode=disable" {
		t.Fatalf("admin URL %q is not the fixture's explicit loopback endpoint: %v", output, err)
	}
	for _, change := range []struct{ old, new string }{
		{releaseGateStmonitorAdminURL, `export STMONITOR_TEST_ADMIN_URL="postgres://postgres:urnetwork-local-test@127.0.0.1:5432/postgres?sslmode=disable"`},
		{releaseGateStmonitorAdminURL, `export STMONITOR_TEST_ADMIN_URL="${STMONITOR_TEST_ADMIN_URL:-postgres://postgres:urnetwork-local-test@$WARP_TEST_ENV_PORTABLE_POSTGRES_AUTHORITY/postgres?sslmode=disable}"`},
		{"\nunset STMONITOR_TEST_ADMIN_URL\n", "\n"},
		{"release_phase_sn_all_normal() {\n", "release_phase_sn_all_normal() {\n  export STMONITOR_TEST_ADMIN_URL=postgres://postgres@127.0.0.1:5432/postgres\n"},
	} {
		if strings.Count(script, change.old) != 1 {
			t.Fatalf("the mutation names no unique text: %q", change.old)
		}
		if err := verifyReleaseGateStmonitorAdminURL(strings.Replace(script, change.old, change.new, 1)); err == nil {
			t.Fatalf("the gate accepted an admin URL outside the private services: %q", change.new)
		}
	}
	// Moved, not added: the URL before the services' environment, and the
	// unset after the jobs' owner starts.
	environmentLine := "\n    " + `source "$RELEASE_GATE_SERVICE_ENV"` + "\n    source \"$workspace/server/test-env.sh\"\n    test_env_validate_suite_resource_manifest \"$TEST_ENV_SUITE_RESOURCE_MANIFEST\" \"$WARP_VAULT_HOME\" \"$WARP_CONFIG_HOME\"\n    cd \"$sn_repo\"\n"
	withoutURL := strings.Replace(script, "\n    "+releaseGateStmonitorAdminURL+"\n", "\n", 1)
	withoutUnset := strings.Replace(script, "\nunset STMONITOR_TEST_ADMIN_URL\n", "\n", 1)
	for name, changed := range map[string]string{
		"an admin URL before the private services' environment": strings.Replace(withoutURL, environmentLine, "\n    "+releaseGateStmonitorAdminURL+environmentLine, 1),
		"an inherited URL cleared only after the jobs start":    strings.Replace(withoutUnset, "\nrelease_gate_jobs_init\n", "\nrelease_gate_jobs_init\nunset STMONITOR_TEST_ADMIN_URL\n", 1),
	} {
		if strings.Count(script, environmentLine) != 1 || withoutURL == script || withoutUnset == script || strings.Count(changed, "STMONITOR_TEST_ADMIN_URL") != strings.Count(script, "STMONITOR_TEST_ADMIN_URL") {
			t.Fatalf("the mutation for %s does not move one line", name)
		}
		if err := verifyReleaseGateStmonitorAdminURL(changed); err == nil {
			t.Fatalf("the gate accepted %s", name)
		}
	}
	producer, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(producer), "\n") {
		if strings.Contains(line, "STMONITOR_TEST_ADMIN_URL") || strings.Contains(line, "go test") && (strings.Contains(line, "./mainnet") || strings.Contains(line, "./...")) {
			t.Fatalf("the producer gate runs mainnet tests or names the stmonitor admin URL: %s", line)
		}
	}
}
