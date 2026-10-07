// generate_script_test.go verifies hermetic generator tool discovery and pinning.
package stabi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// generatorTestEnvironment supplies only explicit tool links, so generator
// discovery cannot accidentally consume an abigen installed on the test host.
type generatorTestEnvironment struct {
	sourceDir    string
	artifactsDir string
	pathDir      string
	goBin        string
	goPath       string
	barrier      *generatorTestBarrier
}

// One fake version call owns one entry reader and one idempotent release.
// The invocation owner must join its real generator before closing the pipes.
type generatorTestBarrier struct {
	directory   string
	entry       *os.File
	release     *os.File
	entered     chan error
	readerDone  chan struct{}
	released    chan struct{}
	releaseOnce sync.Once
	releaseErr  error
}

// Both FIFO endpoints remain open even if a generator fails before abigen.
// The buffered one-record result cannot strand its reader during cleanup.
func newGeneratorTestBarrier(t *testing.T) *generatorTestBarrier {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"entered", "release"} {
		if err := syscall.Mkfifo(filepath.Join(directory, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := os.OpenFile(filepath.Join(directory, "entered"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	release, err := os.OpenFile(filepath.Join(directory, "release"), os.O_RDWR, 0)
	if err != nil {
		_ = entry.Close()
		t.Fatal(err)
	}
	barrier := &generatorTestBarrier{directory: directory, entry: entry, release: release, entered: make(chan error, 1), readerDone: make(chan struct{}), released: make(chan struct{})}
	go func() {
		defer close(barrier.readerDone)
		line, err := bufio.NewReader(entry).ReadString('\n')
		if err == nil && line != "entered\n" {
			err = fmt.Errorf("abigen entry = %q", line)
		}
		barrier.entered <- err
	}()
	return barrier
}

// Cancellation and early-failure cleanup may race to unblock the same child.
// The tiny record fits this exclusively owned pipe even before abigen starts.
func (self *generatorTestBarrier) unblock() error {
	self.releaseOnce.Do(func() {
		_, self.releaseErr = self.release.WriteString("release\n")
		close(self.released)
	})
	return self.releaseErr
}

// Called only after every generator has joined. Wake an unused entry reader
// explicitly; never close its pipe while an abigen writer may still open it.
func (self *generatorTestBarrier) close() error {
	_, wakeErr := self.entry.WriteString("cleanup\n")
	if wakeErr != nil {
		_ = self.entry.Close()
	}
	<-self.readerDone
	return errors.Join(wakeErr, self.entry.Close(), self.release.Close())
}

// newGeneratorTestEnvironment constructs a hermetic PATH plus a controllable
// fake implementation of the two go env queries used by generate.sh.
func newGeneratorTestEnvironment(t *testing.T) *generatorTestEnvironment {
	t.Helper()
	sourceDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	pathDir := t.TempDir()
	for _, name := range []string{"bash", "cmp", "dirname", "jq", "mktemp", "rm", "stat"} {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("locate test prerequisite %s: %v", name, err)
		}
		if err := os.Symlink(target, filepath.Join(pathDir, name)); err != nil {
			t.Fatalf("link test prerequisite %s: %v", name, err)
		}
	}
	writeGeneratorTestExecutable(t, filepath.Join(pathDir, "go"), `#!/bin/sh
case "$1:$2" in
env:GOBIN) printf '%s\n' "${FAKE_GO_BIN-}" ;;
env:GOPATH) printf '%s\n' "${FAKE_GO_PATH-}" ;;
*) printf 'unexpected fake go invocation: %s\n' "$*" >&2; exit 64 ;;
esac
`)
	// Tool-discovery tests own their metadata inputs. They must not depend on
	// a concurrent release gate's mutable Foundry build directory.
	artifactsDir := t.TempDir()
	for _, name := range []string{"STSubnet", "STCoordinator", "STSettlementVault", "STReserveSink", "STValidatorEvidence"} {
		abi, err := os.ReadFile(filepath.Join(sourceDir, "..", "evm", "abi", name+".abi.json"))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(struct {
			ABI json.RawMessage `json:"abi"`
		}{ABI: abi})
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(artifactsDir, name+".sol")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &generatorTestEnvironment{
		sourceDir:    sourceDir,
		artifactsDir: artifactsDir,
		pathDir:      pathDir,
		goPath:       t.TempDir(),
	}
}

// writeGeneratorTestExecutable creates one deterministic fake tool.
func writeGeneratorTestExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// installGeneratorTestAbigen creates an abigen double that reports the chosen
// version and reproduces checked-in bindings without running a host abigen.
func installGeneratorTestAbigen(t *testing.T, path, version string) {
	t.Helper()
	body := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
    if [ -n "${GENERATOR_TEST_BARRIER-}" ]; then
        printf 'entered\n' > "$GENERATOR_TEST_BARRIER/entered"
        read -r release < "$GENERATOR_TEST_BARRIER/release"
        [ "$release" = release ] || exit 66
        printf 'released\n' > "$GENERATOR_TEST_BARRIER/completed"
    fi
    printf 'abigen version %s\n'
    exit 0
fi
type_name=''
output=''
while [ "$#" -gt 0 ]; do
    case "$1" in
    --type) shift; type_name="$1" ;;
    --out) shift; output="$1" ;;
    esac
    shift
done
case "$type_name" in
STSubnet) source_name=stsubnet.go ;;
STCoordinator) source_name=stcoordinator.go ;;
STSettlementVault) source_name=stsettlementvault.go ;;
STReserveSink) source_name=streservesink.go ;;
STValidatorEvidence) source_name=stvalidatorevidence.go ;;
*) printf 'unexpected binding type: %%s\n' "$type_name" >&2; exit 65 ;;
esac
/bin/cp "$STABI_SOURCE_DIR/$source_name" "$output"
`, version)
	writeGeneratorTestExecutable(t, path, body)
}

// run invokes the real generator with no inherited tool-discovery variables.
func (self *generatorTestEnvironment) run(t *testing.T, abigenOverride string) ([]byte, error) {
	return self.runMode(t, abigenOverride, "--check")
}

// runMode invokes one explicit generator operation in the hermetic environment.
func (self *generatorTestEnvironment) runMode(t *testing.T, abigenOverride, mode string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return self.runModeContext(ctx, abigenOverride, mode, nil)
}

// The fake tools are finite except for our owned version barrier. Cancellation
// releases it cooperatively, then Wait joins Bash and its actual tool children.
// A nonnil observer is the invocation's buffered one-record cancellation ack.
func (self *generatorTestEnvironment) runModeContext(ctx context.Context, abigenOverride, mode string, canceled chan<- error) ([]byte, error) {
	command := exec.CommandContext(ctx, filepath.Join(self.sourceDir, "generate.sh"), mode)
	command.Cancel = func() error {
		var err error
		if self.barrier != nil {
			err = self.barrier.unblock()
		}
		if canceled != nil {
			canceled <- err
		}
		return err
	}
	if mode == "--check" {
		command.Args = append(command.Args, "--artifacts", self.artifactsDir)
	}
	command.Dir = self.sourceDir
	environment := make([]string, 0, len(os.Environ())+5)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "ABIGEN=") || strings.HasPrefix(value, "PATH=") ||
			strings.HasPrefix(value, "FAKE_GO_BIN=") || strings.HasPrefix(value, "FAKE_GO_PATH=") ||
			strings.HasPrefix(value, "STABI_SOURCE_DIR=") || strings.HasPrefix(value, "GENERATOR_TEST_BARRIER=") {
			continue
		}
		environment = append(environment, value)
	}
	barrierDir := ""
	if self.barrier != nil {
		barrierDir = self.barrier.directory
	}
	environment = append(environment,
		"PATH="+self.pathDir,
		"FAKE_GO_BIN="+self.goBin,
		"FAKE_GO_PATH="+self.goPath,
		"STABI_SOURCE_DIR="+self.sourceDir,
		"GENERATOR_TEST_BARRIER="+barrierDir,
	)
	if abigenOverride != "" {
		environment = append(environment, "ABIGEN="+abigenOverride)
	}
	command.Env = environment
	return command.CombinedOutput()
}

// Concurrent checks retain every pipe and child owner through success, an
// early peer failure and cancellation after both actual version calls enter.
func TestGenerateChecksIndependentArtifactInputs(t *testing.T) {
	type invocation struct {
		environment *generatorTestEnvironment
		done        chan struct{}
		canceled    chan error
		output      []byte
		err         error
		joined      bool
	}
	for _, outcome := range []string{"complete", "early-peer-failure", "cancel-after-entry"} {
		var invocations []*invocation
		var barriers []*generatorTestBarrier
		before := map[string]string{}
		var cleanupErr error
		func() {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			defer func() {
				// Release all children before waiting for any one owner. Entry
				// endpoints stay open until every real generator has joined.
				for _, barrier := range barriers {
					cleanupErr = errors.Join(cleanupErr, barrier.unblock())
				}
				for _, owner := range invocations {
					<-owner.done
					owner.joined = true
				}
				for _, barrier := range barriers {
					cleanupErr = errors.Join(cleanupErr, barrier.close())
				}
			}()
			first, second := newGeneratorTestEnvironment(t), newGeneratorTestEnvironment(t)
			for _, environment := range []*generatorTestEnvironment{first, second} {
				installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
				for _, name := range []string{"STSubnet", "STCoordinator", "STSettlementVault", "STReserveSink", "STValidatorEvidence"} {
					for _, path := range []string{
						filepath.Join(environment.artifactsDir, name+".sol", name+".json"),
						filepath.Join(environment.sourceDir, "..", "evm", "abi", name+".abi.json"),
						filepath.Join(environment.sourceDir, strings.ToLower(name)+".go"),
					} {
						data, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						before[path] = string(data)
					}
				}
				environment.barrier = newGeneratorTestBarrier(t)
				barriers = append(barriers, environment.barrier)
			}
			start := func(environment *generatorTestEnvironment) *invocation {
				owner := &invocation{environment: environment, done: make(chan struct{}), canceled: make(chan error, 1)}
				invocations = append(invocations, owner)
				go func() {
					owner.output, owner.err = environment.runModeContext(ctx, "", "--check", owner.canceled)
					close(owner.done)
				}()
				return owner
			}
			awaitEntry := func(owner *invocation) {
				select {
				case err := <-owner.environment.barrier.entered:
					if err != nil {
						t.Fatalf("%s actual abigen entry: %v", outcome, err)
					}
				case <-owner.done:
					t.Fatalf("%s generator exited before overlap barrier: %v\n%s", outcome, owner.err, owner.output)
				case <-ctx.Done():
					t.Fatalf("%s did not reach its owned entry: %v", outcome, ctx.Err())
				}
			}
			firstOwner := start(first)
			if outcome == "early-peer-failure" {
				// The healthy peer is already physically blocked before the
				// failing generator is started; cleanup must release/join it.
				awaitEntry(firstOwner)
				second.artifactsDir = filepath.Join(second.artifactsDir, "missing")
				secondOwner := start(second)
				<-secondOwner.done
				if secondOwner.err == nil || !strings.Contains(string(secondOwner.output), "explicit artifact directory") {
					t.Fatalf("early peer did not fail before abigen: %v\n%s", secondOwner.err, secondOwner.output)
				}
				return
			}
			secondOwner := start(second)
			awaitEntry(firstOwner)
			awaitEntry(secondOwner)
			if outcome == "cancel-after-entry" {
				cancel()
				for _, owner := range invocations {
					if err := <-owner.canceled; err != nil {
						t.Errorf("actual generator cancellation callback failed: %v", err)
					}
				}
				// The cancellation callbacks have completed, but cleanup has not
				// released anything. This is an ordering proof, not a timeout.
				for _, barrier := range barriers {
					select {
					case <-barrier.released:
					default:
						t.Error("cancellation left its real abigen blocked")
					}
				}
			}
			// A release-omission control must still unblock all actual children
			// before joining and reporting its cancellation-boundary failure.
			for _, barrier := range barriers {
				if err := barrier.unblock(); err != nil {
					t.Fatal(err)
				}
			}
			for _, owner := range invocations {
				<-owner.done
				if outcome == "cancel-after-entry" {
					if !errors.Is(owner.err, context.Canceled) {
						t.Errorf("canceled generator was killed or lost its cancellation: %v\n%s", owner.err, owner.output)
					}
				} else if owner.err != nil {
					t.Errorf("independent artifact check: %v\n%s", owner.err, owner.output)
				}
			}
		}()
		if cleanupErr != nil {
			t.Fatalf("%s owned generator cleanup: %v", outcome, cleanupErr)
		}
		for index, owner := range invocations {
			if !owner.joined {
				t.Fatalf("%s generator %d was not joined", outcome, index)
			}
			if outcome == "early-peer-failure" && index == 0 && owner.err != nil {
				t.Fatalf("early failure cleanup did not finish the healthy peer: %v\n%s", owner.err, owner.output)
			}
		}
		for index, barrier := range barriers {
			select {
			case <-barrier.readerDone:
			default:
				t.Fatalf("%s entry reader %d was not joined", outcome, index)
			}
			completed, err := os.ReadFile(filepath.Join(barrier.directory, "completed"))
			if outcome == "early-peer-failure" && index == 1 {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("early failed generator reached abigen: %q/%v", completed, err)
				}
			} else if err != nil || string(completed) != "released\n" {
				t.Fatalf("%s real abigen %d was not cooperatively released: %q/%v", outcome, index, completed, err)
			}
		}
		for path, expected := range before {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != expected {
				t.Errorf("%s artifact/check source changed: %s: %v", outcome, path, err)
			}
		}
	}
}

// An explicitly missing directory cannot borrow a previous release build.
func TestGenerateMissingExplicitArtifactsNeverFallsBack(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
	environment.artifactsDir = filepath.Join(t.TempDir(), "missing")
	output, err := environment.run(t, "")
	if err == nil || !strings.Contains(string(output), "explicit artifact directory") {
		t.Fatalf("missing explicit artifacts fell back: %v\n%s", err, output)
	}
}

// The explicit input root cannot be redirected to a different owner's graph.
func TestGenerateExplicitArtifactSymlinkRefused(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	path := filepath.Join(t.TempDir(), "redirect")
	if err := os.Symlink(environment.artifactsDir, path); err != nil {
		t.Fatal(err)
	}
	environment.artifactsDir = path
	output, err := environment.run(t, "")
	if err == nil || !strings.Contains(string(output), "explicit artifact directory") {
		t.Fatalf("redirected explicit artifact root was accepted: %v\n%s", err, output)
	}
}

// A symlink in an ancestor cannot redirect a seemingly physical final leaf.
func TestGenerateExplicitArtifactAncestorSymlinkRefused(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Dir(environment.artifactsDir), alias); err != nil {
		t.Fatal(err)
	}
	environment.artifactsDir = filepath.Join(alias, filepath.Base(environment.artifactsDir))
	output, err := environment.run(t, "")
	if err == nil || !strings.Contains(string(output), "explicit artifact directory") {
		t.Fatalf("ancestor artifact alias was accepted: %v\n%s", err, output)
	}
}

func TestGeneratePreflightStopsAfterVersionValidation(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
	if err := os.Remove(filepath.Join(environment.pathDir, "jq")); err != nil {
		t.Fatal(err)
	}
	output, err := environment.runMode(t, "", "--preflight")
	if err != nil {
		t.Fatalf("generate --preflight: %v\n%s", err, output)
	}
	if got, want := string(output), "abigen version 1.17.0-stable\n"; got != want {
		t.Fatalf("preflight output = %q, want %q", got, want)
	}
}

// An explicit override wins even when PATH contains an incompatible tool.
func TestGenerateUsesExplicitAbigenBeforePath(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.16.7-stable")
	explicitAbigen := filepath.Join(t.TempDir(), "abigen")
	installGeneratorTestAbigen(t, explicitAbigen, "1.17.0-stable")
	if output, err := environment.run(t, explicitAbigen); err != nil {
		t.Fatalf("generate --check with ABIGEN: %v\n%s", err, output)
	}
}

// The ordinary executable search remains the first implicit lookup path.
func TestGenerateDiscoversAbigenOnPath(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
	if output, err := environment.run(t, ""); err != nil {
		t.Fatalf("generate --check with PATH abigen: %v\n%s", err, output)
	}
}

// A configured Go binary directory works when PATH has no generator.
func TestGenerateDiscoversAbigenInGoBin(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	environment.goBin = t.TempDir()
	installGeneratorTestAbigen(t, filepath.Join(environment.goBin, "abigen"), "1.17.0-stable")
	if output, err := environment.run(t, ""); err != nil {
		t.Fatalf("generate --check with GOBIN abigen: %v\n%s", err, output)
	}
}

// Only the first Go workspace supplies the conventional fallback directory.
func TestGenerateDiscoversAbigenInFirstGoPath(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	secondGoPath := t.TempDir()
	environment.goPath += string(os.PathListSeparator) + secondGoPath
	if err := os.Mkdir(filepath.Join(strings.Split(environment.goPath, string(os.PathListSeparator))[0], "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	firstGoPath := strings.Split(environment.goPath, string(os.PathListSeparator))[0]
	installGeneratorTestAbigen(t, filepath.Join(firstGoPath, "bin", "abigen"), "1.17.0-stable")
	if output, err := environment.run(t, ""); err != nil {
		t.Fatalf("generate --check with GOPATH abigen: %v\n%s", err, output)
	}
}

// A discoverable generator from the wrong go-ethereum release fails closed.
func TestGenerateRejectsWrongAbigenVersion(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.16.7-stable")
	output, err := environment.run(t, "")
	if err == nil {
		t.Fatal("generate --check accepted the wrong abigen version")
	}
	message := string(output)
	if !strings.Contains(message, "got 'abigen version 1.16.7-stable', want 'abigen version 1.17.0-stable'") ||
		!strings.Contains(message, "go install github.com/ethereum/go-ethereum/cmd/abigen@v1.17.0") {
		t.Fatalf("wrong-version diagnostic is incomplete:\n%s", output)
	}
}

// Missing tools produce the exact reproducible installation instruction.
func TestGenerateRejectsMissingAbigen(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	output, err := environment.run(t, "")
	if err == nil {
		t.Fatal("generate --check accepted a missing abigen")
	}
	message := string(output)
	if !strings.Contains(message, "was not found in ABIGEN, PATH, GOBIN, or the first GOPATH/bin") ||
		!strings.Contains(message, "go install github.com/ethereum/go-ethereum/cmd/abigen@v1.17.0") {
		t.Fatalf("missing-tool diagnostic is incomplete:\n%s", output)
	}
}
