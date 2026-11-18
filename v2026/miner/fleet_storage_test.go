//go:build linux || darwin

// `provider fleet storage` end to end: real descriptors, xattrs, journals and
// runtime owners, with only kernel mount facts synthetic. No chain, key or network.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablesys"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The test owns one private synthetic mount holding a new state directory.
type fleetStorageFixture struct {
	ctx     context.Context
	host    *durablefixture.Host
	volume  durablevolume.VolumeSpec
	state   string
	custody string
}

func newFleetStorageFixture(t *testing.T) *fleetStorageFixture {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	observed := filepath.Join(parent, "observed")
	if err := os.Mkdir(observed, 0700); err != nil {
		t.Fatal(err)
	}
	physical := durablefixture.New(t, t.Context(), observed)
	config, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	return fleetStorageFixtureOn(t, physical.Host, config.Volumes[0], filepath.Join(parent, "state"))
}

// A new state directory on an existing synthetic mount. Preparation is given
// only kernel facts: no declaration exists yet.
func fleetStorageFixtureOn(t *testing.T, host *durablefixture.Host, volume durablevolume.VolumeSpec, state string) *fleetStorageFixture {
	t.Helper()
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("URNETWORK_STATE_DIR", state)
	return &fleetStorageFixture{ctx: durablepath.WithHost(t.Context(), host), host: host, volume: volume, state: state, custody: filepath.Join(state, "fleet-custody")}
}

func (self *fleetStorageFixture) recoveryRoot() string {
	return filepath.Join(self.state, "fleet-mainnet-recovery")
}
func (self *fleetStorageFixture) nativeRoot() string {
	return filepath.Join(self.state, "fleet-native")
}

// Parses the real grammar, then runs the command. Options go through docopt, so
// a test can't call a form the usage doesn't have.
func (self *fleetStorageFixture) run(t *testing.T, args ...string) (durablevolume.Reference, string, error) {
	t.Helper()
	opts, err := parseArgsForTestErr(append([]string{"fleet", "storage"}, args...))
	if err != nil {
		t.Fatalf("%v does not parse: %v", args, err)
	}
	var stdout, stderr bytes.Buffer
	err = fleetStorage(self.ctx, opts, &stdout, &stderr)
	var reference durablevolume.Reference
	if err == nil {
		// Output is one JSON document and nothing else, so it can be saved.
		decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		if decodeErr := decoder.Decode(&reference); decodeErr != nil || decoder.More() {
			t.Fatalf("%v stdout is not one JSON document: %v\n%s", args, decodeErr, stdout.String())
		}
	} else if stdout.Len() != 0 {
		t.Fatalf("refused %v still printed to stdout:\n%s", args, stdout.String())
	}
	return reference, stderr.String(), err
}

func (self *fleetStorageFixture) requestArgs() []string {
	return []string{"request", "--mount_path=" + self.volume.MountPath, "--filesystem_uuid=" + self.volume.FilesystemUuid, "--filesystem_type=" + self.volume.FilesystemType,
		"--former-writers-stopped", "--no-previous-owner-state", "--evidence=A new fleet: these keys have never signed a fleet write, and no other machine holds them."}
}

func (self *fleetStorageFixture) request(t *testing.T) durablevolume.Reference {
	t.Helper()
	reference, _, err := self.run(t, self.requestArgs()...)
	if err != nil {
		t.Fatal("valid request refused: ", err)
	}
	return reference
}

func (self *fleetStorageFixture) plan(t *testing.T, request durablevolume.Reference) durablevolume.Reference {
	t.Helper()
	reference, _, err := self.tryPlan(t, request)
	if err != nil {
		t.Fatal("valid plan refused: ", err)
	}
	return reference
}

func (self *fleetStorageFixture) tryPlan(t *testing.T, request durablevolume.Reference) (durablevolume.Reference, string, error) {
	t.Helper()
	return self.run(t, "plan", "--request="+request.Path, "--request-sha256="+request.Sha256)
}

func (self *fleetStorageFixture) tryApply(t *testing.T, plan durablevolume.Reference) (fleetStorageResult, string, error) {
	t.Helper()
	opts, err := parseArgsForTestErr([]string{"fleet", "storage", "apply", "--plan=" + plan.Path, "--plan-sha256=" + plan.Sha256})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = fleetStorage(self.ctx, opts, &stdout, &stderr)
	var result fleetStorageResult
	if err == nil {
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			t.Fatalf("apply stdout is not one JSON document: %v\n%s", decodeErr, stdout.String())
		}
	} else if stdout.Len() != 0 {
		t.Fatalf("refused apply still printed to stdout:\n%s", stdout.String())
	}
	return result, stderr.String(), err
}

// The whole customer path, returning the declaration a fleet write is given.
func (self *fleetStorageFixture) prepare(t *testing.T) durablevolume.Reference {
	t.Helper()
	result, _, err := self.tryApply(t, self.plan(t, self.request(t)))
	if err != nil {
		t.Fatal("valid apply refused: ", err)
	}
	return result.Declaration
}

// A fleet write's context: the declaration it was given, and these kernel facts.
func (self *fleetStorageFixture) writeContext(declaration durablevolume.Reference) context.Context {
	return durablepath.WithHost(durablevolume.WithReference(context.Background(), declaration), self.host)
}

func fleetStorageRootAttributes(t *testing.T, root string) []string {
	t.Helper()
	file, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	buffer := make([]byte, 64*1024)
	count, err := durablesys.ListAttributes(int(file.Fd()), buffer)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, name := range strings.Split(string(buffer[:count]), "\x00") {
		if strings.HasPrefix(name, "user.urnetwork.") {
			names = append(names, name)
		}
	}
	return names
}

// Refusal left no custody: no generation or reservation on either journal
// directory, none of its members, and no marker, lease, control journal or
// declaration anywhere. A test's planted file is not what this looks for.
func (self *fleetStorageFixture) requireNothingEnrolled(t *testing.T) {
	t.Helper()
	for _, root := range []string{self.recoveryRoot(), self.nativeRoot()} {
		if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
			continue
		}
		for _, name := range fleetStorageRootAttributes(t, root) {
			if name == durablevolume.RootGenerationAttribute || name == durablevolume.PreparationAttribute {
				t.Fatalf("%s gained %s", root, name)
			}
		}
		for _, name := range []string{"initialized", "journal.json", "native-transactions.jsonl", "native-transactions"} {
			if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s/%s exists or can't be observed: %v", root, name, err)
			}
		}
	}
	for _, member := range []string{"recovery", "native"} {
		for _, name := range []string{"identity", "root.lease", "preparation.jsonl", "durable-volumes.json"} {
			if _, err := os.Lstat(filepath.Join(self.custody, member, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s/%s exists or can't be observed: %v", member, name, err)
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(self.custody, "durable-volumes.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a combined declaration exists or can't be observed: %v", err)
	}
}

func fleetStorageRefused(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted; wanted a refusal naming %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("refused for another reason; wanted %q, got: %v", want, err)
	}
}

func fleetStorageRewrite(t *testing.T, path string, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return durablefixture.Digest(raw)
}

func fleetStorageDecode(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

// Rewrites one directory's request and re-pins it, as an operator editing the
// request files could, so the check under test is the one that has to refuse.
func (self *fleetStorageFixture) editRequest(t *testing.T, cover durablevolume.Reference, member string, edit func(*durablevolume.PreparationRequest)) durablevolume.Reference {
	t.Helper()
	var requests fleetStorageCover
	fleetStorageDecode(t, cover.Path, &requests)
	reference := &requests.Recovery
	if member == "native" {
		reference = &requests.Native
	}
	var request durablevolume.PreparationRequest
	fleetStorageDecode(t, reference.Path, &request)
	edit(&request)
	reference.Sha256 = fleetStorageRewrite(t, reference.Path, request)
	return durablevolume.Reference{Path: cover.Path, Sha256: fleetStorageRewrite(t, cover.Path, requests)}
}

// Rewrites one directory's former-writer fence and re-pins it all the way up.
func (self *fleetStorageFixture) editFence(t *testing.T, cover durablevolume.Reference, member string, edit func(*durablevolume.PreparationFence)) durablevolume.Reference {
	t.Helper()
	path := filepath.Join(self.custody, member, "former-writer.json")
	var fence durablevolume.PreparationFence
	fleetStorageDecode(t, path, &fence)
	edit(&fence)
	digest := fleetStorageRewrite(t, path, fence)
	return self.editRequest(t, cover, member, func(request *durablevolume.PreparationRequest) { request.FormerWriterFence.Sha256 = digest })
}

// The customer path yields one declaration, in the owner-local schema, under
// which the unchanged runtime owners open both journals. request and plan leave
// the journal directories exactly as they were.
func TestFleetStorageDeclarationOpensTheActualJournals(t *testing.T) {
	f := newFleetStorageFixture(t)
	request, hint, err := f.run(t, f.requestArgs()...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hint, "provider fleet storage plan --request="+request.Path+" --request-sha256="+request.Sha256) {
		t.Fatalf("request does not give the next command:\n%s", hint)
	}
	f.requireNothingEnrolled(t)
	plan, hint, err := f.tryPlan(t, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hint, "provider fleet storage apply --plan="+plan.Path+" --plan-sha256="+plan.Sha256) {
		t.Fatalf("plan does not give the next command:\n%s", hint)
	}
	f.requireNothingEnrolled(t)
	result, hint, err := f.tryApply(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Schema != fleetStorageResultSchema || result.Plan != plan || !reflect.DeepEqual(result.Roots, []string{f.recoveryRoot(), f.nativeRoot()}) {
		t.Fatalf("result: %+v", result)
	}
	for _, want := range []string{"--durable-volumes=" + result.Declaration.Path, "--durable-volumes-sha256=" + result.Declaration.Sha256, "URNETWORK_STATE_DIR=" + f.state} {
		if !strings.Contains(hint, want) {
			t.Fatalf("apply does not say %q:\n%s", want, hint)
		}
	}
	config, err := durablevolume.LoadOwnerLocal(result.Declaration)
	if err != nil {
		t.Fatal("the declaration does not load as owner-local: ", err)
	}
	if len(config.Volumes) != 1 || len(config.Volumes[0].StateRoots) != 2 || config.Volumes[0].StateRoots[0].Path != f.recoveryRoot() || config.Volumes[0].StateRoots[1].Path != f.nativeRoot() {
		t.Fatalf("declaration: %+v", config)
	}
	// Lost output: the same plan returns the same declaration, and nothing new.
	again, _, err := f.tryApply(t, plan)
	if err != nil || !reflect.DeepEqual(again, result) {
		t.Fatalf("repeating the accepted plan changed its result: %+v %v", again, err)
	}
	ctx := f.writeContext(result.Declaration)
	store, err := openFleetRecoveryStore(ctx)
	if err != nil {
		t.Fatal("the actual recovery journal does not open: ", err)
	}
	if len(store.records) != 0 {
		t.Fatal("a fresh journal holds records")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	journal, err := snchain.OpenOwnerLocalJournal(ctx, f.nativeRoot())
	if err != nil {
		t.Fatal("the actual native journal does not open: ", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	// Runtime now owns the journal: the original plan can never reset it.
	if _, _, err := f.tryApply(t, plan); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("the original plan ran again over a live journal: %v", err)
	}
	if store, err = openFleetRecoveryStore(ctx); err != nil {
		t.Fatal("refusing the repeat disturbed the journal: ", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
}

// All four mainnet writes run through their real journals on the declaration
// the customer path produced, against a local RPC with synthetic chain identity.
func TestFleetStorageDeclarationAdmitsEveryFleetWrite(t *testing.T) {
	for _, command := range []struct {
		name       string
		run        func(context.Context, docopt.Opts, *protocol.FleetManifest) error
		configure  func(*testing.T, *fleetMainnetTestFixture)
		sends      string
		wantNative bool
	}{
		{name: "register", run: fleetRegister, sends: "author_submitAndWatchExtrinsic", wantNative: true, configure: func(t *testing.T, fixture *fleetMainnetTestFixture) {
			fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, true)}
			fixture.opts["--apply"] = true
		}},
		{name: "publish", run: fleetPublish, sends: "author_submitAndWatchExtrinsic", configure: func(t *testing.T, fixture *fleetMainnetTestFixture) {
			fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
		}},
		{name: "bind", run: fleetBind, sends: "eth_sendRawTransaction", configure: func(t *testing.T, fixture *fleetMainnetTestFixture) { fixture.opts["--dry-run"] = false }},
		{name: "revoke", run: fleetRevoke, sends: "eth_sendRawTransaction", configure: func(t *testing.T, fixture *fleetMainnetTestFixture) { fixture.opts["--dry-run"] = false }},
	} {
		t.Run(command.name, func(t *testing.T) {
			fixture := newFleetMainnetTestFixture(t)
			command.configure(t, fixture)
			config, err := durablevolume.LoadOwnerLocal(fixture.durable.Reference)
			if err != nil {
				t.Fatal(err)
			}
			f := fleetStorageFixtureOn(t, fixture.durable.Host, config.Volumes[0], filepath.Join(config.Volumes[0].MountPath, "customer-state"))
			declaration := f.prepare(t)
			if err := command.run(f.writeContext(declaration), fixture.opts, fixture.manifest); err != nil {
				t.Fatalf("%s refused the prepared declaration: %v", command.name, err)
			}
			if fixture.count(command.sends) != 1 {
				t.Fatalf("%s sent %d times", command.name, fixture.count(command.sends))
			}
			raw, err := os.ReadFile(filepath.Join(f.recoveryRoot(), "journal.json"))
			if err != nil {
				t.Fatal(err)
			}
			var journal fleetRecoveryJournal
			if err := json.Unmarshal(raw, &journal); err != nil || len(journal.Records) != 1 || journal.Records[0].Stage != "finalized" {
				t.Fatalf("%s left no finalized record in the prepared journal: %v\n%s", command.name, err, raw)
			}
			info, err := os.Stat(filepath.Join(f.nativeRoot(), "native-transactions.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if (0 < info.Size()) != command.wantNative {
				t.Fatalf("%s native journal holds %d bytes", command.name, info.Size())
			}
		})
	}
}

// Writes still refuse without the flags, or with anything but this exact
// declaration, once the storage is prepared. Nothing is sent in any of them.
func TestFleetStorageLeavesEveryWriteRefusalInPlace(t *testing.T) {
	for _, command := range []struct {
		name  string
		run   func(context.Context, docopt.Opts, *protocol.FleetManifest) error
		sends string
	}{
		{name: "register", run: fleetRegister, sends: "author_submitAndWatchExtrinsic"},
		{name: "publish", run: fleetPublish, sends: "author_submitAndWatchExtrinsic"},
		{name: "bind", run: fleetBind, sends: "eth_sendRawTransaction"},
		{name: "revoke", run: fleetRevoke, sends: "eth_sendRawTransaction"},
	} {
		t.Run(command.name, func(t *testing.T) {
			fixture := newFleetMainnetTestFixture(t)
			fixture.opts["--dry-run"] = false
			fixture.opts["--apply"] = true
			config, err := durablevolume.LoadOwnerLocal(fixture.durable.Reference)
			if err != nil {
				t.Fatal(err)
			}
			f := fleetStorageFixtureOn(t, fixture.durable.Host, config.Volumes[0], filepath.Join(config.Volumes[0].MountPath, "customer-state"))
			declaration := f.prepare(t)
			for _, refusal := range []struct {
				name string
				ctx  context.Context
				want string
			}{
				{"no flags", durablepath.WithHost(context.Background(), f.host), "explicit durable-volume declaration and hash are required"},
				{"another digest", f.writeContext(durablevolume.Reference{Path: declaration.Path, Sha256: fleetStorageTestDigest}), "declaration bytes differ"},
				{"the host's declaration, which names other roots", f.writeContext(fixture.durable.Reference), "outside the declared roots"},
			} {
				err := command.run(refusal.ctx, fixture.opts, fixture.manifest)
				fleetStorageRefused(t, err, refusal.want)
				if fixture.count(command.sends) != 0 {
					t.Fatalf("%s sent after refusing: %s", command.name, refusal.name)
				}
			}
			if _, err := os.Lstat(filepath.Join(f.recoveryRoot(), "journal.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a refused write opened the journal: %v", err)
			}
		})
	}
}

// One directory's own declaration is not enough for register, which is why the
// customer path publishes a single declaration naming both directories.
func TestFleetRegisterNeedsTheNativeJournalDeclared(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	config, err := durablevolume.LoadOwnerLocal(fixture.durable.Reference)
	if err != nil {
		t.Fatal(err)
	}
	f := fleetStorageFixtureOn(t, fixture.durable.Host, config.Volumes[0], filepath.Join(config.Volumes[0].MountPath, "customer-state"))
	f.prepare(t)
	path := filepath.Join(f.custody, "recovery", "durable-volumes.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, true)}
	err = fleetRegister(f.writeContext(durablevolume.Reference{Path: path, Sha256: durablefixture.Digest(raw)}), fixture.opts, fixture.manifest)
	fleetStorageRefused(t, err, "outside the declared roots")
	if fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("register sent without its native journal")
	}
}

// A prepared declaration names owner-local custody: daemon admission refuses it.
func TestFleetStorageDeclarationIsNotADaemonDeclaration(t *testing.T) {
	f := newFleetStorageFixture(t)
	declaration := f.prepare(t)
	if _, err := durablevolume.Load(declaration); err == nil {
		t.Fatal("the daemon loader accepted it")
	}
	if directory, err := durablepath.Open(f.writeContext(declaration), f.recoveryRoot(), durablevolume.ReadWrite, false); err == nil {
		directory.Close()
		t.Fatal("daemon admission opened a fleet journal")
	}
}

// What the operator must state can't be left out, and nothing is written first.
func TestFleetStorageRequestRefusals(t *testing.T) {
	statements := func(f *fleetStorageFixture, drop string) docopt.Opts {
		opts := docopt.Opts{"request": true, "--mount_path": f.volume.MountPath, "--filesystem_uuid": f.volume.FilesystemUuid, "--filesystem_type": f.volume.FilesystemType, "--former-writers-stopped": true, "--no-previous-owner-state": true, "--evidence": "checked"}
		delete(opts, drop)
		return opts
	}
	for _, test := range []struct {
		name string
		opts func(*fleetStorageFixture) docopt.Opts
		want string
	}{
		{"writers not stated stopped", func(f *fleetStorageFixture) docopt.Opts { return statements(f, "--former-writers-stopped") }, "--former-writers-stopped"},
		{"no previous state not stated", func(f *fleetStorageFixture) docopt.Opts { return statements(f, "--no-previous-owner-state") }, "--no-previous-owner-state"},
		{"no evidence", func(f *fleetStorageFixture) docopt.Opts { return statements(f, "--evidence") }, "--evidence"},
		{"blank evidence", func(f *fleetStorageFixture) docopt.Opts {
			opts := statements(f, "")
			opts["--evidence"] = "   "
			return opts
		}, "--evidence"},
		{"evidence past the fence's bound", func(f *fleetStorageFixture) docopt.Opts {
			opts := statements(f, "")
			opts["--evidence"] = strings.Repeat("x", 8193)
			return opts
		}, "--evidence"},
		{"relative mount", func(f *fleetStorageFixture) docopt.Opts {
			opts := statements(f, "")
			opts["--mount_path"] = "data"
			return opts
		}, "--mount_path"},
		{"state directory outside the mount", func(f *fleetStorageFixture) docopt.Opts {
			opts := statements(f, "")
			opts["--mount_path"] = f.state + "-sibling"
			return opts
		}, "URNETWORK_STATE_DIR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFleetStorageFixture(t)
			var stdout, stderr bytes.Buffer
			fleetStorageRefused(t, fleetStorage(f.ctx, test.opts(f), &stdout, &stderr), test.want)
			if _, err := os.Lstat(f.custody); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("custody files exist after refusal: %v", err)
			}
			f.requireNothingEnrolled(t)
		})
	}

	t.Run("missing state directory is not created", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		missing := f.state + "-missing"
		t.Setenv("URNETWORK_STATE_DIR", missing)
		_, _, err := f.run(t, f.requestArgs()...)
		fleetStorageRefused(t, err, "must already exist")
		if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("it was created: %v", err)
		}
	})
	for _, root := range []string{"fleet-mainnet-recovery", "fleet-native"} {
		t.Run("retained "+root, func(t *testing.T) {
			f := newFleetStorageFixture(t)
			path := filepath.Join(f.state, root)
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "journal.json"), []byte("retained"), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := f.run(t, f.requestArgs()...)
			fleetStorageRefused(t, err, "can't be prepared as new")
			if _, err := os.Lstat(f.custody); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("custody files exist after refusal: %v", err)
			}
		})
		t.Run("group-readable "+root+" keeps its mode", func(t *testing.T) {
			f := newFleetStorageFixture(t)
			path := filepath.Join(f.state, root)
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0750); err != nil {
				t.Fatal(err)
			}
			_, _, err := f.run(t, f.requestArgs()...)
			fleetStorageRefused(t, err, "private directory")
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0750 {
				t.Fatalf("mode changed: %v %v", info, err)
			}
		})
		t.Run("symlinked "+root, func(t *testing.T) {
			f := newFleetStorageFixture(t)
			target := f.state + "-elsewhere"
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(f.state, root)); err != nil {
				t.Fatal(err)
			}
			_, _, err := f.run(t, f.requestArgs()...)
			fleetStorageRefused(t, err, "private directory")
		})
	}
	t.Run("a second request never replaces the first", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		first := f.request(t)
		before, err := os.ReadFile(first.Path)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = f.run(t, f.requestArgs()...)
		fleetStorageRefused(t, err, "--request-sha256="+first.Sha256)
		after, err := os.ReadFile(first.Path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("the request changed: %v", err)
		}
	})
	t.Run("an interrupted request resumes only on the same statements", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		first := f.request(t)
		if err := os.Remove(first.Path); err != nil {
			t.Fatal(err)
		}
		again := f.request(t)
		if again != first {
			t.Fatalf("the same statements gave another request: %+v %+v", first, again)
		}
		if err := os.Remove(first.Path); err != nil {
			t.Fatal(err)
		}
		args := f.requestArgs()
		args[len(args)-1] = "--evidence=something else"
		_, _, err := f.run(t, args...)
		fleetStorageRefused(t, err, "already exists with other contents")
	})
}

// Everything `sn-mainnet storage-owner-prepare plan` refuses, refused the same
// way here, on both directories, with no custody created.
func TestFleetStoragePlanRefusals(t *testing.T) {
	type edit func(*testing.T, *fleetStorageFixture, durablevolume.Reference, string) durablevolume.Reference
	request := func(change func(*fleetStorageFixture, *durablevolume.PreparationRequest)) edit {
		return func(t *testing.T, f *fleetStorageFixture, cover durablevolume.Reference, member string) durablevolume.Reference {
			return f.editRequest(t, cover, member, func(value *durablevolume.PreparationRequest) { change(f, value) })
		}
	}
	fence := func(change func(*durablevolume.PreparationFence)) edit {
		return func(t *testing.T, f *fleetStorageFixture, cover durablevolume.Reference, member string) durablevolume.Reference {
			return f.editFence(t, cover, member, change)
		}
	}
	state := func(change func(*testing.T, *fleetStorageFixture, string)) edit {
		return func(t *testing.T, f *fleetStorageFixture, cover durablevolume.Reference, member string) durablevolume.Reference {
			change(t, f, member)
			return cover
		}
	}
	rootOf := func(f *fleetStorageFixture, member string) string {
		if member == "native" {
			return f.nativeRoot()
		}
		return f.recoveryRoot()
	}
	inputs := func(change func(map[string]any)) func(*fleetStorageFixture, *durablevolume.PreparationRequest) {
		return func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			var fields map[string]any
			if err := json.Unmarshal(value.Owners[0].Inputs, &fields); err != nil {
				panic(err)
			}
			change(fields)
			raw, err := json.Marshal(fields)
			if err != nil {
				panic(err)
			}
			value.Owners[0].Inputs = raw
		}
	}
	for _, test := range []struct {
		name string
		edit edit
		want string
	}{
		{"daemon scope", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) { value.Scope = "daemon" }), "explicit scope"},
		{"restore purpose has no adapter here", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.Purpose, value.Owners[0].Purpose = "restore", "restore"
		}), "restore checkpoint adapters are required"},
		{"another root", request(func(f *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.RootPath = filepath.Join(f.state, "elsewhere")
		}), "fleet writes here open"},
		{"another owner kind", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.Owners[0].Kind = "mainnet-owner-signing"
		}), "as its one owner"},
		{"a second owner", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.Owners = append(value.Owners, value.Owners[0])
		}), "as its one owner"},
		{"a retained owner purpose", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.Owners[0].Purpose = "retained"
		}), "fixed fresh kind"},
		{"an owner below the root", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.Owners[0].RelativePath = "below"
		}), "fixed fresh kind"},
		{"a private key in the inputs", request(inputs(func(fields map[string]any) { fields["private_key"] = "forbidden" })), "unknown field"},
		{"another mount", request(func(f *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.MountPath = f.state
		}), "absent, ambiguous or has another filesystem type"},
		{"another filesystem type", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) { value.FilesystemType = "xfs" }), "absent, ambiguous or has another filesystem type"},
		{"an uppercase uuid", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.FilesystemUuid = "ABCD-1234"
		}), "uuid is not canonical"},
		{"no reserve", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) { value.MinAvailableBytes = 0 }), "positive reserve"},
		{"no limits", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.Limits = durablevolume.PreparationLimits{}
		}), "capacities are absent"},
		{"a marker inside the journal directory", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.MarkerPath = filepath.Join(value.RootPath, "identity")
		}), "inside a journal root"},
		{"staging inside the journal directory", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.StagingDirectory = filepath.Join(value.RootPath, "staging")
		}), "staging or former-writer reference"},
		{"a marker outside the mount", request(func(_ *fleetStorageFixture, value *durablevolume.PreparationRequest) {
			value.MarkerPath = "/outside/identity"
		}), "identity, roots or positive reserve"},
		{"writers not stopped", fence(func(value *durablevolume.PreparationFence) { value.FormerWritersStopped = false }), "stopped former-writer evidence"},
		{"previous state", fence(func(value *durablevolume.PreparationFence) { value.NoPreviousOwnerState = false }), "stopped former-writer evidence"},
		{"blank evidence", fence(func(value *durablevolume.PreparationFence) { value.Evidence = " " }), "stopped former-writer evidence"},
		{"evidence past its bound", fence(func(value *durablevolume.PreparationFence) { value.Evidence = strings.Repeat("x", 8193) }), "stopped former-writer evidence"},
		{"a fence for another inode", fence(func(value *durablevolume.PreparationFence) { value.RootInode++ }), "stopped former-writer evidence"},
		{"a fence for another root", fence(func(value *durablevolume.PreparationFence) { value.RootPath += "-other" }), "stopped former-writer evidence"},
		{"fence bytes changed after the pin", func(t *testing.T, f *fleetStorageFixture, cover durablevolume.Reference, member string) durablevolume.Reference {
			var value durablevolume.PreparationFence
			path := filepath.Join(f.custody, member, "former-writer.json")
			fleetStorageDecode(t, path, &value)
			value.Evidence = "changed afterwards"
			fleetStorageRewrite(t, path, value)
			return cover
		}, "former-writer fence bytes differ"},
		{"request bytes changed after the pin", func(t *testing.T, f *fleetStorageFixture, cover durablevolume.Reference, member string) durablevolume.Reference {
			var value durablevolume.PreparationRequest
			path := filepath.Join(f.custody, member, "preparation-request.json")
			fleetStorageDecode(t, path, &value)
			value.Limits.MaxEntries++
			fleetStorageRewrite(t, path, value)
			return cover
		}, "bytes differ from the accepted sha256: digest"},
		{"a file in the journal directory", state(func(t *testing.T, f *fleetStorageFixture, member string) {
			if err := os.WriteFile(filepath.Join(rootOf(f, member), "retained"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}), "existing custody cannot be reenrolled"},
		{"custody metadata on the journal directory", state(func(t *testing.T, f *fleetStorageFixture, member string) {
			file, err := os.Open(rootOf(f, member))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := durablesys.SetAttribute(int(file.Fd()), "user.urnetwork.previous", []byte("x"), durablesys.AttributeCreate); err != nil {
				t.Fatal(err)
			}
		}), "previous owner custody metadata"},
		{"a group-readable journal directory", state(func(t *testing.T, f *fleetStorageFixture, member string) {
			if err := os.Chmod(rootOf(f, member), 0750); err != nil {
				t.Fatal(err)
			}
		}), "private and owned"},
		{"a replaced journal directory", state(func(t *testing.T, f *fleetStorageFixture, member string) {
			root := rootOf(f, member)
			if err := os.Rename(root, root+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
		}), "stopped former-writer evidence"},
		{"group-readable custody files", state(func(t *testing.T, f *fleetStorageFixture, member string) {
			if err := os.Chmod(filepath.Join(f.custody, member), 0750); err != nil {
				t.Fatal(err)
			}
		}), "private and owned"},
		{"a read-only filesystem", state(func(_ *testing.T, f *fleetStorageFixture, _ string) { f.host.SetReadOnly(true) }), "write reserve is unavailable"},
		{"below the byte reserve", state(func(_ *testing.T, f *fleetStorageFixture, _ string) {
			f.host.SetReserve(fleetStorageMinAvailableBytes-1, fleetStorageMinAvailableInodes)
		}), "write reserve is unavailable"},
		{"below the inode reserve", state(func(_ *testing.T, f *fleetStorageFixture, _ string) {
			f.host.SetReserve(fleetStorageMinAvailableBytes, fleetStorageMinAvailableInodes-1)
		}), "write reserve is unavailable"},
	} {
		// The native member comes second, so a refusal there has already
		// planned recovery, and still enrolls nothing anywhere.
		for _, member := range []string{"recovery", "native"} {
			t.Run(test.name+"/"+member, func(t *testing.T) {
				f := newFleetStorageFixture(t)
				cover := test.edit(t, f, f.request(t), member)
				_, _, err := f.tryPlan(t, cover)
				fleetStorageRefused(t, err, test.want)
				f.requireNothingEnrolled(t)
				if _, err := os.Lstat(filepath.Join(f.custody, "preparation-plan.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("a refused plan left its cover: %v", err)
				}
			})
		}
	}
	// Any retained metadata means earlier custody: it is refused and left alone.
	for _, member := range []string{"recovery", "native"} {
		for _, name := range []string{"identity", "root.lease", "preparation.jsonl", "durable-volumes.json"} {
			t.Run("a retained "+name+"/"+member, func(t *testing.T) {
				f := newFleetStorageFixture(t)
				cover := f.request(t)
				path := filepath.Join(f.custody, member, name)
				if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				_, _, err := f.tryPlan(t, cover)
				fleetStorageRefused(t, err, "already exists without this plan's custody")
				raw, err := os.ReadFile(path)
				if err != nil || string(raw) != "old" {
					t.Fatalf("it was changed: %q %v", raw, err)
				}
			})
		}
	}
	// Capacities are per format, so each is exercised on its own directory.
	for _, test := range []struct {
		name, member, field string
		value               any
		want                string
	}{
		{"a larger recovery journal", "recovery", "maximum_bytes", fleetRecoveryMaxBytes + 1, "differ from its fixed runtime profile"},
		{"more recovery records", "recovery", "maximum_records", fleetRecoveryMaxRecords + 1, "differ from its fixed runtime profile"},
		{"a larger raw record", "recovery", "maximum_raw_record_bytes", fleetRecoveryMaxRaw + 1, "differ from its fixed runtime profile"},
		{"a renamed recovery journal", "recovery", "name", "../journal.json", "differ from its fixed runtime profile"},
		{"a larger native journal", "native", "maximum_journal_bytes", 16*1024*1024 + 1, "fixed runtime profile"},
		{"more native raw bytes", "native", "maximum_raw_bytes", 64*1024*1024 + 1, "fixed runtime profile"},
		{"more native raw members", "native", "maximum_raw_members", 10001, "fixed runtime profile"},
		{"a larger native record", "native", "maximum_raw_record_bytes", 1024*1024 + 1, "fixed runtime profile"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFleetStorageFixture(t)
			cover := f.editRequest(t, f.request(t), test.member, func(value *durablevolume.PreparationRequest) {
				inputs(func(fields map[string]any) { fields[test.field] = test.value })(f, value)
			})
			_, _, err := f.tryPlan(t, cover)
			fleetStorageRefused(t, err, test.want)
			f.requireNothingEnrolled(t)
		})
	}
	// The shared adapter's own refusal for anything outside its registry, below
	// this command's check that a directory has its one owner.
	t.Run("the adapter refuses every other kind", func(t *testing.T) {
		for _, kind := range []string{"provider-claim-queue", "mainnet-owner-signing", "validator-attempt-ledger", "unknown"} {
			f := newFleetStorageFixture(t)
			cover := f.editRequest(t, f.request(t), "recovery", func(value *durablevolume.PreparationRequest) { value.Owners[0].Kind = kind })
			var requests fleetStorageCover
			fleetStorageDecode(t, cover.Path, &requests)
			if _, err := durablepath.PlanOwnerLocalPreparation(f.ctx, requests.Recovery, fleetStorageAdapter()); err == nil {
				t.Fatalf("%s was planned", kind)
			}
		}
	})
	for _, test := range []struct {
		name string
		args func(durablevolume.Reference) []string
		want string
	}{
		{"another digest", func(cover durablevolume.Reference) []string {
			return []string{"plan", "--request=" + cover.Path, "--request-sha256=" + fleetStorageTestDigest}
		}, "bytes differ from the accepted sha256: digest"},
		{"a bare digest", func(cover durablevolume.Reference) []string {
			return []string{"plan", "--request=" + cover.Path, "--request-sha256=" + strings.TrimPrefix(cover.Sha256, "sha256:")}
		}, "bytes differ from the accepted sha256: digest"},
		{"a relative path", func(cover durablevolume.Reference) []string {
			return []string{"plan", "--request=preparation-request.json", "--request-sha256=" + cover.Sha256}
		}, "absolute path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFleetStorageFixture(t)
			_, _, err := f.run(t, test.args(f.request(t))...)
			fleetStorageRefused(t, err, test.want)
			f.requireNothingEnrolled(t)
		})
	}
	t.Run("one directory's request is not a cover", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		var requests fleetStorageCover
		fleetStorageDecode(t, f.request(t).Path, &requests)
		_, _, err := f.tryPlan(t, requests.Recovery)
		if err == nil {
			t.Fatal("a member request planned as a cover")
		}
		f.requireNothingEnrolled(t)
	})
	t.Run("another state directory", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		cover := f.request(t)
		t.Setenv("URNETWORK_STATE_DIR", f.state+"-other")
		_, _, err := f.tryPlan(t, cover)
		fleetStorageRefused(t, err, "URNETWORK_STATE_DIR")
		f.requireNothingEnrolled(t)
	})
	t.Run("a repeated plan returns the retained one", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		cover := f.request(t)
		first := f.plan(t, cover)
		second := f.plan(t, cover)
		if first != second {
			t.Fatalf("the plan changed: %+v %+v", first, second)
		}
		entries, err := os.ReadDir(filepath.Join(f.custody, "recovery", "staging"))
		if err != nil || len(entries) != 1 {
			t.Fatalf("a repeated plan staged again: %v %v", entries, err)
		}
	})
	t.Run("a retained plan is not reused for another request", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		cover := f.request(t)
		f.plan(t, cover)
		other := f.editRequest(t, cover, "recovery", func(value *durablevolume.PreparationRequest) { value.MinAvailableInodes++ })
		_, _, err := f.tryPlan(t, other)
		fleetStorageRefused(t, err, "planned from another request")
	})
}

// Everything `sn-mainnet storage-owner-prepare apply` refuses, before any effect.
func TestFleetStorageApplyRefusals(t *testing.T) {
	editMember := func(t *testing.T, plan durablevolume.Reference, member string, change func(*durablevolume.PreparationPlan)) durablevolume.Reference {
		var plans fleetStorageCover
		fleetStorageDecode(t, plan.Path, &plans)
		reference := &plans.Recovery
		if member == "native" {
			reference = &plans.Native
		}
		var value durablevolume.PreparationPlan
		fleetStorageDecode(t, reference.Path, &value)
		change(&value)
		reference.Sha256 = fleetStorageRewrite(t, reference.Path, value)
		return durablevolume.Reference{Path: plan.Path, Sha256: fleetStorageRewrite(t, plan.Path, plans)}
	}
	for _, test := range []struct {
		name string
		edit func(*testing.T, *fleetStorageFixture, durablevolume.Reference) durablevolume.Reference
		want string
	}{
		{"another digest", func(_ *testing.T, _ *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			return durablevolume.Reference{Path: plan.Path, Sha256: fleetStorageTestDigest}
		}, "bytes differ from the accepted sha256: digest"},
		{"a relative path", func(_ *testing.T, _ *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			return durablevolume.Reference{Path: "preparation-plan.json", Sha256: plan.Sha256}
		}, "absolute path"},
		{"a member plan changed after the pin", func(t *testing.T, _ *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			var plans fleetStorageCover
			fleetStorageDecode(t, plan.Path, &plans)
			var value durablevolume.PreparationPlan
			fleetStorageDecode(t, plans.Recovery.Path, &value)
			value.Nonce[0] ^= 1
			fleetStorageRewrite(t, plans.Recovery.Path, value)
			return plan
		}, "bytes differ from the accepted sha256: digest"},
		{"a plan that grants restart", func(t *testing.T, _ *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			return editMember(t, plan, "recovery", func(value *durablevolume.PreparationPlan) { value.RestartAuthorized = true })
		}, "malformed or outside its exact scope"},
		{"a plan for another owner's census", func(t *testing.T, _ *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			return editMember(t, plan, "recovery", func(value *durablevolume.PreparationPlan) { value.Owners[0].Owner.Kind = "provider-claim-queue" })
		}, "changed its fixed scope"},
		{"a plan with an extra source", func(t *testing.T, _ *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			return editMember(t, plan, "native", func(value *durablevolume.PreparationPlan) {
				value.Sources = append(value.Sources, value.Sources[0])
			})
		}, "source paths are duplicated"},
		{"a plan with a source it did not stage", func(t *testing.T, _ *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			return editMember(t, plan, "native", func(value *durablevolume.PreparationPlan) { value.Sources[0].Identity.Inode++ })
		}, "differs from the accepted plan"},
		{"a request changed after the plan", func(t *testing.T, f *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			path := filepath.Join(f.custody, "native", "preparation-request.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			return plan
		}, "request bytes differ"},
		{"a fence changed after the plan", func(t *testing.T, f *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			path := filepath.Join(f.custody, "recovery", "former-writer.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			return plan
		}, "former-writer fence bytes differ"},
		{"a file in the journal directory", func(t *testing.T, f *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			if err := os.WriteFile(filepath.Join(f.recoveryRoot(), "retained"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			return plan
		}, "not empty"},
		{"a replaced journal directory", func(t *testing.T, f *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			root := f.recoveryRoot()
			if err := os.Rename(root, root+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			return plan
		}, "physical generation changed"},
		{"a remounted filesystem", func(_ *testing.T, f *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			f.host.ReplaceMount()
			return plan
		}, "physical generation changed"},
		{"a read-only filesystem", func(_ *testing.T, f *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			f.host.SetReadOnly(true)
			return plan
		}, "write reserve is unavailable"},
		{"below the reserve", func(_ *testing.T, f *fleetStorageFixture, plan durablevolume.Reference) durablevolume.Reference {
			f.host.SetReserve(1, 1)
			return plan
		}, "write reserve is unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFleetStorageFixture(t)
			plan := test.edit(t, f, f.plan(t, f.request(t)))
			_, _, err := f.tryApply(t, plan)
			fleetStorageRefused(t, err, test.want)
			if _, err := os.Lstat(filepath.Join(f.custody, "durable-volumes.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a refused apply published a declaration: %v", err)
			}
		})
	}
	t.Run("another state directory", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		plan := f.plan(t, f.request(t))
		t.Setenv("URNETWORK_STATE_DIR", f.state+"-other")
		_, _, err := f.tryApply(t, plan)
		fleetStorageRefused(t, err, "URNETWORK_STATE_DIR")
		f.requireNothingEnrolled(t)
	})
	t.Run("a request cover is not a plan", func(t *testing.T) {
		f := newFleetStorageFixture(t)
		_, _, err := f.tryApply(t, f.request(t))
		fleetStorageRefused(t, err, fleetStoragePlanSchema)
		f.requireNothingEnrolled(t)
	})
}

// The declaration appears only once every directory is complete. A failure part
// way keeps what finished and the same plan continues, through each control journal.
func TestFleetStoragePartialApplyExposesNoDeclaration(t *testing.T) {
	f := newFleetStorageFixture(t)
	plan := f.plan(t, f.request(t))
	blocker := filepath.Join(f.nativeRoot(), "blocker")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.tryApply(t, plan)
	fleetStorageRefused(t, err, "not empty")
	combined := filepath.Join(f.custody, "durable-volumes.json")
	if _, err := os.Lstat(combined); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a partial apply published a declaration: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.recoveryRoot(), "initialized")); err != nil {
		t.Fatalf("the finished directory did not keep its custody: %v", err)
	}
	recoveryOnly := fleetStorageRootAttributeValues(t, f.recoveryRoot())
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	result, _, err := f.tryApply(t, plan)
	if err != nil {
		t.Fatal("the same plan did not continue: ", err)
	}
	if !reflect.DeepEqual(recoveryOnly, fleetStorageRootAttributeValues(t, f.recoveryRoot())) {
		t.Fatal("continuing changed the finished directory's custody")
	}
	if _, err := durablevolume.LoadOwnerLocal(result.Declaration); err != nil {
		t.Fatal(err)
	}
}

func fleetStorageRootAttributeValues(t *testing.T, root string) map[string]string {
	t.Helper()
	file, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	values := map[string]string{}
	for _, name := range fleetStorageRootAttributes(t, root) {
		buffer := make([]byte, 8192)
		count, err := durablesys.GetAttribute(int(file.Fd()), name, buffer)
		if err != nil {
			t.Fatal(err)
		}
		values[name] = string(buffer[:count])
	}
	return values
}

// Before any apply the only thing to lose is the request: the published way to
// correct one really works, while the same removal after an apply is refused.
func TestFleetStorageStartsOverOnlyBeforeAnyApply(t *testing.T) {
	f := newFleetStorageFixture(t)
	f.plan(t, f.request(t))
	f.requireNothingEnrolled(t)
	if err := os.RemoveAll(f.custody); err != nil {
		t.Fatal(err)
	}
	args := f.requestArgs()
	args[len(args)-1] = "--evidence=Corrected: a new fleet, and no other machine holds its keys."
	request, _, err := f.run(t, args...)
	if err != nil {
		t.Fatal("a corrected request was refused: ", err)
	}
	result, _, err := f.tryApply(t, f.plan(t, request))
	if err != nil {
		t.Fatal("a corrected request did not prepare: ", err)
	}
	if _, err := durablevolume.LoadOwnerLocal(result.Declaration); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.custody); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.run(t, args...)
	fleetStorageRefused(t, err, "can't be prepared as new")
}

// A published declaration is never replaced: other bytes under its name stop apply.
func TestFleetStorageNeverReplacesAPublishedDeclaration(t *testing.T) {
	f := newFleetStorageFixture(t)
	plan := f.plan(t, f.request(t))
	path := filepath.Join(f.custody, "durable-volumes.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.tryApply(t, plan)
	fleetStorageRefused(t, err, "already exists with other contents")
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "{}\n" {
		t.Fatalf("it was replaced: %q %v", raw, err)
	}
}

// A published name holds complete private bytes, under any interleaving: the same
// bytes are a lost acknowledgement, and other bytes never take its place.
func TestFleetStoragePublishNeverReplacesAndNeverTears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "published.json")
	raw := bytes.Repeat([]byte("exact bytes\n"), 4096)
	results := make(chan error, 8)
	for range cap(results) {
		go func() { results <- fleetStoragePublish(path, raw) }()
	}
	for range cap(results) {
		if err := <-results; err != nil {
			t.Fatal("publishing the same bytes refused: ", err)
		}
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("published mode: %v %v", info, err)
	}
	fleetStorageRefused(t, fleetStoragePublish(path, []byte("other")), "already exists with other contents")
	published, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(published, raw) {
		t.Fatalf("the published bytes changed: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("publishing left %v: %v", entries, err)
	}
}

// The command never reaches for a fleet: a dispatch that loaded --manifest would
// refuse here for a reason unrelated to the request.
func TestFleetCommandRoutesStorageBeforeAnyManifest(t *testing.T) {
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	opts, err := parseArgsForTestErr([]string{"fleet", "storage", "plan", "--request=/absent-request.json", "--request-sha256=" + fleetStorageTestDigest})
	if err != nil {
		t.Fatal(err)
	}
	err = fleetCommand(opts)
	if err == nil || !strings.Contains(err.Error(), "/absent-request.json") {
		t.Fatalf("fleet storage plan did not reach its own request: %v", err)
	}
}
