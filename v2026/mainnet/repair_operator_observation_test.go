package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Failures follow actual descriptor operations and cannot supply replacement
// source bytes. The same original journal can proceed after observation returns.
func TestRepairOperatorResourceObservationUnavailableRetainsCustody(t *testing.T) {
	for _, operation := range []string{"operator-resource-open-stat", "operator-resource-read", "operator-resource-close", "operator-resource-directory-stat", "operator-resource-directory-close", "operator-quota-open-stat"} {
		f := newRepairOperatorFixture(t)
		f.claim()
		before := mainnetNamespaceTest(t, f.envelope.original.Plan.StateDirectory)
		faults := 0
		ctx := context.WithValue(f.ctx(), repairValidatorObservationKey{}, func(observed string) error {
			if observed == operation && faults == 0 {
				faults++
				return syscall.EIO
			}
			return nil
		})
		status, complete, err := f.envelope.resume(ctx, f.key, f.base.host, func() time.Time { return f.base.now })
		disposition, _ := repairControllerCause(err)
		if faults != 1 || status != "source-refused" || complete || f.base.starts != 0 || !errors.Is(err, syscall.EIO) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, errRpcIntegrity) || errors.Is(err, durablevolume.ErrIdentity) || disposition != "pending" {
			t.Fatal("unavailable operator descriptor changed original custody meaning", operation, faults, status, complete, err, f.base.starts, disposition)
		}
		raw, err := os.ReadFile(f.envelope.approval.Plan.StatePath)
		if err != nil {
			t.Fatal(err)
		}
		var record repairProcessRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if err := record.validate(f.envelope, f.key); err != nil || record.Observations != 1 || !record.StartAt.IsZero() || record.Generation != nil {
			t.Fatal("descriptor refusal reset a reservation or acquired a start", operation, err, record)
		}
		status, complete, err = f.resume()
		if err != nil || !complete || status != "resumed-generation-observed" || f.base.starts != 1 {
			t.Fatal("restored observation did not retain the original start allowance", operation, status, complete, err, f.base.starts)
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.envelope.original.Plan.StateDirectory)) {
			t.Fatal("descriptor retry rewrote original journals or quota custody", operation)
		}
	}
}

// A returned contradictory protection mode is independent of a failed close.
// The availability wrapper cannot soften already observed authority loss.
func TestRepairOperatorResourceContradictionDominatesCloseFailure(t *testing.T) {
	f := newRepairOperatorFixture(t)
	path := filepath.Join(f.envelope.original.Plan.env("WARP_VAULT_HOME"), "main", "1.0.0", "st.yml")
	if err := os.Chmod(path, 0622); err != nil {
		t.Fatal(err)
	}
	faults := 0
	ctx := context.WithValue(f.ctx(), repairValidatorObservationKey{}, func(operation string) error {
		if operation == "operator-resource-close" {
			faults++
			return syscall.EIO
		}
		return nil
	})
	_, _, err := readRepairOperatorFile(ctx, f.base.host, path, f.base.host.rootUid, 2*1024*1024)
	disposition, cause := repairControllerCause(err)
	if faults != 1 || !errors.Is(err, syscall.EIO) || !errors.Is(err, durablevolume.ErrUnavailable) || !errors.Is(err, errRpcIntegrity) || disposition != "held" || cause != "integrity" {
		t.Fatal("close unavailability hid the observed resource contradiction", faults, err, disposition, cause)
	}
}

// A completed Stat result remains evidence even if its caller is canceled or
// another observation fails immediately afterward. Unknown Stat results remain
// covered by the independent unavailable-observation cases above.
func TestRepairOperatorObservedMetadataContradictionDominatesReadFailure(t *testing.T) {
	for _, test := range []struct {
		kind   string
		cancel bool
	}{{"file", false}, {"file", true}, {"directory", false}, {"directory", true}, {"quota", false}, {"quota", true}} {
		f := newRepairOperatorFixture(t)
		ctx, cancel := context.WithCancel(f.ctx())
		t.Cleanup(cancel)
		faults := 0
		fail := func() error {
			faults++
			if test.cancel {
				cancel()
				return nil
			}
			return syscall.EIO
		}
		var err error
		switch test.kind {
		case "file":
			path := filepath.Join(f.envelope.original.Plan.env("WARP_VAULT_HOME"), "main", "1.0.0", "st.yml")
			if err := os.Chmod(path, 0622); err != nil {
				t.Fatal(err)
			}
			ctx = context.WithValue(ctx, repairValidatorObservationKey{}, func(operation string) error {
				if operation == "operator-resource-open-stat" {
					return fail()
				}
				return nil
			})
			_, _, err = readRepairOperatorFile(ctx, f.base.host, path, f.base.host.rootUid, 2*1024*1024)
		case "directory":
			root := f.envelope.original.Plan.env("WARP_VAULT_HOME")
			if err := os.Chmod(filepath.Join(root, "main", "1.0.0"), 0777); err != nil {
				t.Fatal(err)
			}
			observed := 0
			ctx = context.WithValue(ctx, repairValidatorObservationKey{}, func(operation string) error {
				if operation == "operator-resource-directory-stat" {
					observed++
					if observed == 3 {
						return fail()
					}
				}
				return nil
			})
			for _, tree := range f.envelope.original.Plan.Resources {
				if tree.Path == root {
					_, err = inspectRepairOperatorTree(ctx, f.base.host, tree, f.envelope.original.Plan)
				}
			}
		case "quota":
			var quota repairOperatorRetainedFile
			for _, file := range f.envelope.approval.Plan.OriginalFiles {
				if file.Quota {
					quota = file
					break
				}
			}
			if quota.File.Path == "" {
				t.Fatal("fixture has no original quota")
			}
			replaced := false
			ctx = context.WithValue(ctx, repairValidatorObservationKey{}, func(operation string) error {
				if operation == "operator-resource-close" && !replaced {
					// Keep the old inode alive so allocation cannot reuse it.
					if err := os.Rename(quota.File.Path, filepath.Join(f.base.directory, "retained-quota-before-observation")); err != nil {
						return err
					}
					if err := os.WriteFile(quota.File.Path, nil, 0600); err != nil {
						return err
					}
					if os.Geteuid() == 0 {
						plan := f.envelope.original.Plan
						if err := os.Chown(quota.File.Path, int(plan.Uid), int(plan.Gid)); err != nil {
							return err
						}
					}
					replaced = true
				}
				if operation == "operator-quota-open-stat" {
					return fail()
				}
				return nil
			})
			err = inspectRepairOperatorRetained(ctx, f.base.host, f.envelope.original.Plan, []repairOperatorRetainedFile{quota}, true)
		}
		expected := error(syscall.EIO)
		if test.cancel {
			expected = context.Canceled
		}
		disposition, cause := repairControllerCause(err)
		if faults != 1 || !errors.Is(err, expected) || !errors.Is(err, errRpcIntegrity) || disposition != "held" || cause != "integrity" {
			t.Fatal("late observation failure hid completed original metadata contradiction", test, faults, err, disposition, cause)
		}
	}
}
