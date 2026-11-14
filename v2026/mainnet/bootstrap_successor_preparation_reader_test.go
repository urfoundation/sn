// Read-only successor consumers must not acquire the preparation writer's
// recovery powers or treat a mutable report as retained approval.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A complete source is opened through the real publication owner; the reader
// must preserve exact file bytes and leave incomplete sources for that owner.
func TestBootstrapSuccessorPreparationReaderRequiresCompleteClaim(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, bootstrapSuccessorPreparationApproval)
	}{
		{name: "missing marker", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			if err := os.Remove(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorPreparationFile+".lock")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "pending marker", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			if err := os.WriteFile(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorPreparationFile+".lock"), []byte(rootObjectHash(approval)+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "partial completion", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			if err := os.WriteFile(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorPreparationFile+".lock"), []byte(rootObjectHash(approval)+"\n"+bootstrapRootClaimComplete[:3]), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing record", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			if err := os.Remove(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorPreparationFile)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "foreign marker", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			if err := os.WriteFile(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorPreparationFile+".lock"), []byte(rootObjectHash("synthetic foreign claim")+"\n"+bootstrapRootClaimComplete), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "same claimant stage", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			name := (&bootstrapSuccessorPreparationStore{approval: approval}).stageName("record")
			if err := os.WriteFile(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, name), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unknown claimant stage", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			if err := os.WriteFile(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorStagePrefix+"synthetic-foreign.claim"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "resealed forged approval", change: func(t *testing.T, approval bootstrapSuccessorPreparationApproval) {
			approval.Signature = strings.Repeat("00", 64)
			record := bootstrapSuccessorPreparationRecord{Schema: bootstrapSuccessorPreparationStateSchema, Phase: "prepared-offline", Approval: approval}
			record.ContentHash = rootObjectHash(record)
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorPreparationFile), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, bootstrapSuccessorPreparationFile+".lock"), []byte(rootObjectHash(approval)+"\n"+bootstrapRootClaimComplete), 0600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
		owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
		reader, record, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
		if err != nil || record.validate(approval) != nil {
			t.Fatalf("complete source refused before %s: %v", c.name, err)
		}
		if err := reader.close(); err != nil {
			t.Fatal(err)
		}
		c.change(t, approval)
		before := bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)
		reader, _, err = openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
		if reader != nil {
			reader.close()
		}
		if err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)) {
			t.Fatalf("read-only inspection accepted or repaired %s", c.name)
		}
	}
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	if reader, _, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil); err == nil || reader != nil || len(bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)) != 0 {
		t.Fatal("read-only inspection created absent custody")
	}
}

// Shared readers coexist, exclude a real preparation writer and release all
// ownership on rejection or cancellation without timing-dependent scheduling.
func TestBootstrapSuccessorPreparationReaderFencesPublication(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
	if reader != nil {
		reader.close()
	}
	if err == nil || !strings.Contains(err.Error(), "active local owner") {
		t.Fatal("reader escaped active publication", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)
	first, _, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, _, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
	if err != nil {
		t.Fatal("independent read-only inspections cannot coexist", err)
	}
	defer second.close()
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if owner != nil {
		owner.close()
	}
	if err == nil {
		t.Fatal("preparation writer escaped retained shared readers")
	}
	if err := errors.Join(first.close(), second.close()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(storage.Context)
	reader, _, err = openBootstrapSuccessorPreparationReader(ctx, approval.Plan, func(stage string) error {
		if stage == "reader-validated" {
			cancel()
		}
		return nil
	})
	if reader != nil {
		reader.close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("reader ignored cancellation after validating the claim", err)
	}
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal("rejected reader retained ownership", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)) {
		t.Fatal("read-only ownership changed original bytes")
	}
}

// Descriptor identity and private regular-file rules apply to reads as well as
// publication; replacing a path cannot lend a signed claim to a fresh root.
func TestBootstrapSuccessorPreparationReaderRejectsUnsafeOrReplacedCustody(t *testing.T) {
	for _, name := range []string{bootstrapSuccessorPreparationFile, bootstrapSuccessorPreparationFile + ".lock"} {
		for _, kind := range []string{"permissions", "symlink", "hardlink", "oversized"} {
			approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
			owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, name)
			switch kind {
			case "permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				other := filepath.Join(t.TempDir(), "synthetic-retained-file")
				if err := os.Rename(path, other); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, filepath.Join(t.TempDir(), "synthetic-alias")); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(path, []byte(strings.Repeat("x", maximumBootstrapSuccessorPreparationBytes+1)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			reader, _, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
			if reader != nil {
				reader.close()
			}
			if err == nil {
				t.Fatalf("reader accepted unsafe %s %s", name, kind)
			}
		}
	}
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	root := approval.Plan.Proposal.OriginalRunDirectory
	moved := filepath.Join(t.TempDir(), "synthetic-original-root")
	reader, _, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, func(stage string) error {
		if stage == "reader-validated" {
			if err := os.Rename(root, moved); err != nil {
				return err
			}
			return os.Mkdir(root, 0700)
		}
		return nil
	})
	if reader != nil {
		reader.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("reader accepted replacement after claim admission", err)
	}
}
