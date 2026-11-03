//go:build linux || darwin

// Offline native checkpoint construction cannot label retained or canceled
// observations as fresh authority. No runtime enrollment occurs in this helper.
package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/connect/durablesys"

	"github.com/urnetwork/connect/durablevolume"
	"golang.org/x/sys/unix"
)

// Real empty files are prepared as explicit test inputs, without any xattr.
func nativePreparationInputFixture(t *testing.T) (*os.File, NativeJournalPreparationScope) {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, journalRawDir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, journalFileName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	return file, NativeJournalPreparationScope{Schema: NativeJournalPreparationSchema, MaximumJournalBytes: 16 * 1024 * 1024,
		MaximumRawBytes: 64 * 1024 * 1024, MaximumRawMembers: 10000, MaximumRawRecordBytes: 1024 * 1024}
}

// Data, aliases and unprotected original members cannot become an empty head.
func TestNativePreparationCheckpointRefusesRetainedIntent(t *testing.T) {
	for _, mode := range []string{"log-data", "raw-data", "linked-log", "symlink-log", "public-log"} {
		func() {
			root, scope := nativePreparationInputFixture(t)
			if raw, err := BuildFreshNativeJournalPreparationCheckpoint(t.Context(), root, scope); err != nil || len(raw) == 0 {
				t.Fatal("original explicit empty checkpoint is invalid", err)
			}
			log := filepath.Join(root.Name(), journalFileName)
			switch mode {
			case "log-data":
				if err := os.WriteFile(log, []byte("retained original intent"), 0600); err != nil {
					t.Fatal(err)
				}
			case "raw-data":
				if err := os.WriteFile(filepath.Join(root.Name(), journalRawDir, "original.scale"), []byte("original signed bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "linked-log":
				if err := os.Link(log, filepath.Join(root.Name(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "symlink-log":
				if err := os.Rename(log, log+".retained"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(log+".retained", log); err != nil {
					t.Fatal(err)
				}
			case "public-log":
				if err := os.Chmod(log, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if raw, err := BuildFreshNativeJournalPreparationCheckpoint(t.Context(), root, scope); len(raw) != 0 || !errors.Is(err, durablevolume.ErrIdentity) {
				t.Fatal("changed original members became fresh checkpoint authority", mode, err)
			}
			if _, err := unix.Getxattr(root.Name(), NativeJournalCustodyAttribute, make([]byte, 4096)); !errors.Is(err, durablesys.ErrNoAttribute) {
				t.Fatal("read-only checkpoint construction enrolled the root", mode, err)
			}
		}()
	}
}

// This context cancels at a real post-observation boundary without changing
// descriptors or inventing filesystem facts.
type nativePreparationCancelAfterRead struct {
	context.Context
	count  atomic.Uint64
	cancel context.CancelFunc
}

// The first observation admits entry; the second follows actual member reads.
func (self *nativePreparationCancelAfterRead) Err() error {
	if self.count.Add(1) == 2 {
		self.cancel()
	}
	return self.Context.Err()
}

// Canceled physical observations admit no checkpoint bytes or root mutation.
func TestNativePreparationCanceledReadAdmitsNoAuthority(t *testing.T) {
	root, scope := nativePreparationInputFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checked := &nativePreparationCancelAfterRead{Context: ctx, cancel: cancel}
	if raw, err := BuildFreshNativeJournalPreparationCheckpoint(checked, root, scope); len(raw) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled checkpoint construction admitted authority", err)
	}
	if _, err := unix.Getxattr(root.Name(), NativeJournalCustodyAttribute, make([]byte, 4096)); !errors.Is(err, durablesys.ErrNoAttribute) {
		t.Fatal("canceled checkpoint construction enrolled the root", err)
	}
}

// The registry entry every preparing command shares: the one admitted capacity
// vector builds the exact fixed layout and its real empty checkpoint.
func TestNativePreparationOwnerBuildsTheFixedLayout(t *testing.T) {
	stagingPath := t.TempDir()
	if err := os.Chmod(stagingPath, 0700); err != nil {
		t.Fatal(err)
	}
	staging, err := os.Open(stagingPath)
	if err != nil {
		t.Fatal(err)
	}
	defer staging.Close()
	scope := FreshNativeJournalPreparationScope()
	inputs, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	owner := durablevolume.PreparationOwner{Kind: NativeJournalPreparationKind, RelativePath: ".", Purpose: "fresh", Inputs: inputs}
	plan, err := BuildFreshNativeJournalPreparation(t.Context(), staging, "member", owner)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := NativeJournalPreparationLayout(scope)
	if err != nil {
		t.Fatal(err)
	}
	spec := durablevolume.PreparationAttributeSpec{Path: ".", Name: NativeJournalCustodyAttribute}
	if !reflect.DeepEqual(plan.Files, layout) || !reflect.DeepEqual(plan.Owner, owner) || plan.StagingName != "member" || plan.ExclusiveRoot || !bytes.Equal(plan.Census, inputs) || !reflect.DeepEqual(plan.Attributes, []durablevolume.PreparationAttributeSpec{spec}) {
		t.Fatalf("plan: %+v", plan)
	}
	root, err := os.Open(filepath.Join(stagingPath, "member"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	attributes, err := InspectFreshNativeJournalPreparation(t.Context(), root, plan)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := BuildFreshNativeJournalPreparationCheckpoint(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(attributes) != 1 || attributes[0].Spec != spec || !bytes.Equal(attributes[0].Raw, checkpoint) {
		t.Fatalf("inspection does not return the real checkpoint: %+v", attributes)
	}
}

// Whichever command calls it, nothing else builds or passes inspection.
func TestNativePreparationOwnerRefusals(t *testing.T) {
	scope := FreshNativeJournalPreparationScope()
	inputs, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	fresh := durablevolume.PreparationOwner{Kind: NativeJournalPreparationKind, RelativePath: ".", Purpose: "fresh", Inputs: inputs}
	field := func(old, new string) json.RawMessage {
		return json.RawMessage(strings.Replace(string(inputs), old, new, 1))
	}
	for _, test := range []struct {
		name string
		edit func(*durablevolume.PreparationOwner)
		want string
	}{
		{"another kind", func(owner *durablevolume.PreparationOwner) { owner.Kind = "fleet-recovery" }, "explicitly fresh custody"},
		{"retained custody", func(owner *durablevolume.PreparationOwner) { owner.Purpose = "retained" }, "explicitly fresh custody"},
		{"below the root", func(owner *durablevolume.PreparationOwner) { owner.RelativePath = "below" }, "explicitly fresh custody"},
		{"a private key", func(owner *durablevolume.PreparationOwner) { owner.Inputs = field("{", `{"private_key":"forbidden",`) }, "unknown field"},
		{"a duplicate key", func(owner *durablevolume.PreparationOwner) {
			owner.Inputs = field(`{"schema":`, `{"schema":"other","schema":`)
		}, "duplicate"},
		{"trailing JSON", func(owner *durablevolume.PreparationOwner) {
			owner.Inputs = append(append(json.RawMessage(nil), inputs...), " {}"...)
		}, "multiple JSON values"},
		{"a larger journal", func(owner *durablevolume.PreparationOwner) {
			owner.Inputs = field(`"maximum_journal_bytes":16777216`, `"maximum_journal_bytes":16777217`)
		}, "fixed runtime profile"},
		{"more raw bytes", func(owner *durablevolume.PreparationOwner) {
			owner.Inputs = field(`"maximum_raw_bytes":67108864`, `"maximum_raw_bytes":67108865`)
		}, "fixed runtime profile"},
		{"more raw members", func(owner *durablevolume.PreparationOwner) {
			owner.Inputs = field(`"maximum_raw_members":10000`, `"maximum_raw_members":10001`)
		}, "fixed runtime profile"},
		{"a larger record", func(owner *durablevolume.PreparationOwner) {
			owner.Inputs = field(`"maximum_raw_record_bytes":1048576`, `"maximum_raw_record_bytes":1048577`)
		}, "fixed runtime profile"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stagingPath := t.TempDir()
			staging, err := os.Open(stagingPath)
			if err != nil {
				t.Fatal(err)
			}
			defer staging.Close()
			owner := fresh
			test.edit(&owner)
			if _, err := BuildFreshNativeJournalPreparation(t.Context(), staging, "member", owner); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("wanted a refusal naming %q, got: %v", test.want, err)
			}
			if entries, err := os.ReadDir(stagingPath); err != nil || len(entries) != 0 {
				t.Fatalf("a refused owner staged %v: %v", entries, err)
			}
		})
	}
	for index, name := range []string{"", ".", "..", "a/b"} {
		t.Run(fmt.Sprintf("staging name %d", index), func(t *testing.T) {
			staging, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer staging.Close()
			if _, err := BuildFreshNativeJournalPreparation(t.Context(), staging, name, fresh); err == nil {
				t.Fatalf("staged under %q", name)
			}
		})
	}
	root, _ := nativePreparationInputFixture(t)
	layout, err := NativeJournalPreparationLayout(scope)
	if err != nil {
		t.Fatal(err)
	}
	spec := durablevolume.PreparationAttributeSpec{Path: ".", Name: NativeJournalCustodyAttribute}
	plan := func() durablevolume.PreparationOwnerPlan {
		return durablevolume.PreparationOwnerPlan{Owner: fresh, Files: append([]durablevolume.PreparationFile(nil), layout...), Census: inputs, Attributes: []durablevolume.PreparationAttributeSpec{spec}}
	}
	if _, err := InspectFreshNativeJournalPreparation(t.Context(), root, plan()); err != nil {
		t.Fatal("the accepted plan does not inspect: ", err)
	}
	for _, test := range []struct {
		name string
		edit func(*durablevolume.PreparationOwnerPlan)
		want string
	}{
		{"a restored owner", func(value *durablevolume.PreparationOwnerPlan) { value.Owner.Purpose = "restore" }, "cannot reinterpret"},
		{"another census", func(value *durablevolume.PreparationOwnerPlan) {
			value.Census = json.RawMessage(strings.Replace(string(inputs), "10000", "9999", 1))
		}, "reviewed capacity vector"},
		{"another member", func(value *durablevolume.PreparationOwnerPlan) { value.Files = append(value.Files, layout[0]) }, "fixed member or checkpoint census"},
		{"another attribute", func(value *durablevolume.PreparationOwnerPlan) { value.Attributes[0].Name = "user.urnetwork.other" }, "fixed member or checkpoint census"},
		{"a second attribute", func(value *durablevolume.PreparationOwnerPlan) { value.Attributes = append(value.Attributes, spec) }, "fixed member or checkpoint census"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := plan()
			test.edit(&value)
			if _, err := InspectFreshNativeJournalPreparation(t.Context(), root, value); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("wanted a refusal naming %q, got: %v", test.want, err)
			}
		})
	}
}
