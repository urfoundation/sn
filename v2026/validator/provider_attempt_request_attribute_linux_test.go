//go:build linux

// A real ext4 block admits the forecast's widest attributes. This is the
// filesystem the forecast models; other filesystems give each value more room.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Create the four attributes of a prepared ledger root in storage-prepare's
// order: its anchor, the root generation, then the ledger owner's custody
// checkpoint and the request attribute holding request.
func writeProviderRequestTestRootAttributes(path string, preparation ProviderAttemptRequestPreparation, request []byte) error {
	custody, anchor, err := providerAttemptRequestWidestRootAttributes(preparation)
	if err != nil {
		return err
	}
	for _, attribute := range []struct {
		name  string
		value []byte
	}{{durablevolume.PreparationAttribute, anchor}, {durablevolume.RootGenerationAttribute, bytes.Repeat([]byte{7}, durablevolume.RootGenerationBytes)}, {attemptLedgerCustodyAttribute, custody}, {ProviderAttemptRequestAttribute, request}} {
		if err := unix.Setxattr(path, attribute.name, attribute.value, unix.XATTR_CREATE); err != nil {
			return fmt.Errorf("%s: %w", attribute.name, err)
		}
	}
	return nil
}

// Fill the inode body with a filler attribute, so that every root attribute
// after it lands in the one block that the forecast counts. A value that no
// longer fits the body allocates the block, which the block count shows.
func fillProviderRequestTestInodeBody(t *testing.T, path string) {
	t.Helper()
	blocks := func() int64 {
		var stat unix.Stat_t
		if err := unix.Stat(path, &stat); err != nil {
			t.Fatal(err)
		}
		return stat.Blocks
	}
	empty := blocks()
	for size := 0; size <= ext4AttributeBlockBytes; size += 4 {
		if err := unix.Setxattr(path, "user.inode-body", make([]byte, size), 0); err != nil {
			t.Fatal(err)
		}
		if blocks() == empty {
			continue
		}
		// One step too wide: shrink it back into the body, or drop it when the
		// inode has no body for attributes at all.
		err := unix.Removexattr(path, "user.inode-body")
		if size > 0 {
			err = unix.Setxattr(path, "user.inode-body", make([]byte, size-4), 0)
		}
		if err != nil || blocks() != empty {
			t.Fatal("inode body filler did not return to the inode", err)
		}
		return
	}
	t.Fatal("inode body filler never reached the attribute block")
}

// The custody checkpoint at exactly the forecast budget fits one real ext4
// block beside the widest anchor, the generation and the request record, and
// one byte more does not. Same-width replacements still fit. Beside a
// production-sized identity's widest attributes, the sealed v1 checkpoint
// gets ENOSPC and v2 fits.
func TestProviderRequestRootAttributesFitOneRealExt4Block(t *testing.T) {
	parent := t.TempDir()
	var filesystem unix.Statfs_t
	if err := unix.Statfs(parent, &filesystem); err != nil {
		t.Fatal(err)
	}
	if filesystem.Type != unix.EXT4_SUPER_MAGIC || filesystem.Bsize != ext4AttributeBlockBytes {
		t.Skip("TMPDIR is not ext4 with 4 KiB blocks", filesystem.Type, filesystem.Bsize)
	}
	directory := func(name string) string {
		path := filepath.Join(parent, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		fillProviderRequestTestInodeBody(t, path)
		return path
	}
	widest := func(preparation ProviderAttemptRequestPreparation) []byte {
		raw, err := encodeProviderAttemptRequestCheckpoint(providerRequestWidestTestCheckpoint(preparation))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	budget := providerRequestBudgetPreparation(t, 2844)
	if err := budget.Validate(); err != nil {
		t.Fatal(err)
	}
	path := directory("budget")
	if err := writeProviderRequestTestRootAttributes(path, budget, widest(budget)); err != nil {
		t.Fatal("ext4 refused the forecast's widest root attributes", err)
	}
	over := providerRequestBudgetPreparation(t, 2845)
	if err := writeProviderRequestTestRootAttributes(directory("over"), over, widest(over)); !errors.Is(err, unix.ENOSPC) {
		t.Fatal("ext4 admitted a custody checkpoint one byte over the forecast", err)
	}
	custody, _, err := providerAttemptRequestWidestRootAttributes(budget)
	if err != nil {
		t.Fatal(err)
	}
	fresh := providerRequestWidestTestCheckpoint(budget)
	fresh.Pending, fresh.Closed = nil, nil
	replacement, err := encodeProviderAttemptRequestCheckpoint(fresh)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{attemptLedgerCustodyAttribute: custody, ProviderAttemptRequestAttribute: replacement} {
		if err := unix.Setxattr(path, name, value, unix.XATTR_REPLACE); err != nil {
			t.Fatal("ext4 refused a same-width replacement", name, err)
		}
	}
	if raw := providerRequestTestAttribute(t, path); !bytes.Equal(raw, replacement) {
		t.Fatal("request replacement did not read back")
	}
	production := providerRequestTestPreparation("sn25-mainnet-20261007-01")
	legacy, err := json.Marshal(providerRequestWidestTestCheckpoint(production))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeProviderRequestTestRootAttributes(directory("v1"), production, legacy); !errors.Is(err, unix.ENOSPC) {
		t.Fatal("sealed v1 checkpoint fit beside production-sized root attributes", len(legacy), err)
	}
	if err := writeProviderRequestTestRootAttributes(directory("v2"), production, widest(production)); err != nil {
		t.Fatal("ext4 refused the v2 record beside production-sized root attributes", err)
	}
}
