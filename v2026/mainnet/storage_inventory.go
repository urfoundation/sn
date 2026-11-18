// Local storage inspection requires its explicitly selected policy scope, an
// exclusive root lease, and a separate former-writer fence. Reports retain
// opaque owner metadata without granting semantic restore or restart authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The public dispatcher supplies the immutable external policy in context.
func runStorageInventory(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runStorageInspection(ctx, args, stdout, stderr, false, func(_ durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablepath.OpenVolume(ctx, root, access)
	})
}

// Verification is a byte/metadata comparison, never a restore or service start.
func runStorageVerify(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runStorageInspection(ctx, args, stdout, stderr, true, func(_ durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablepath.OpenVolume(ctx, root, access)
	})
}

// Owner-local inspection is a distinct public command, never an implicit
// system-filesystem fallback in the daemon inventory path.
func runStorageOwnerInventory(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runStorageInspection(ctx, args, stdout, stderr, false, func(_ durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablepath.OpenOwnerLocalVolume(ctx, root, access)
	})
}

// The explicit owner-local verifier has the same bounded report-only semantics.
func runStorageOwnerVerify(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runStorageInspection(ctx, args, stdout, stderr, true, func(_ durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablepath.OpenOwnerLocalVolume(ctx, root, access)
	})
}

// The instance opener permits facts-only tests; no flag or environment selects it.
func runStorageInspection(ctx context.Context, args []string, stdout, stderr io.Writer, verify bool, open func(durablevolume.Reference, string, durablevolume.Access) (*durablevolume.Owner, error)) int {
	flags := flag.NewFlagSet("storage-inventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Exact pre-provisioned state root in the reviewed declaration")
	fencePath := flags.String("former-writer-fence", "", "Protected external stop/join assertion")
	fenceHash := flags.String("former-writer-fence-sha256", "", "Exact sha256: digest of that assertion")
	entries := flags.Uint64("max-entries", 1000, "Maximum files and directories (at most 10000)")
	bytes := flags.Uint64("max-bytes", 64*1024*1024, "Maximum content bytes hashed (at most one TiB)")
	depth := flags.Uint64("max-depth", 16, "Maximum nested path depth (at most 32)")
	ownerAttributes := flags.Uint64("max-owner-attributes", 1000, "Maximum retained owner attributes (at most 10000)")
	ownerAttributeBytes := flags.Uint64("max-owner-attribute-bytes", 4*1024*1024, "Maximum retained owner attribute bytes (at most 16 MiB)")
	var expectedPath, expectedHash string
	var compareReviewedRebound bool
	if verify {
		flags.StringVar(&expectedPath, "inventory", "", "Retained protected inventory")
		flags.StringVar(&expectedHash, "inventory-sha256", "", "Exact sha256: digest of retained inventory")
		flags.BoolVar(&compareReviewedRebound, "compare-reviewed-rebound", false, "Compare against the separately reviewed target declaration; never rebind or authorize restart")
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" || *fencePath == "" || *fenceHash == "" || verify && (expectedPath == "" || expectedHash == "") {
		fmt.Fprintln(stderr, "storage inspection requires an exact root, former-writer fence and hashes; verification also requires a retained inventory")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	reference, _ := durablevolume.ReferenceFromContext(ctx)
	owner, err := open(reference, *root, durablevolume.Snapshot)
	if err != nil {
		return storageInspectionFailure(stderr, err)
	}
	defer owner.Close()
	fence := durablevolume.Reference{Path: *fencePath, Sha256: *fenceHash}
	limits := durablevolume.InventoryLimits{MaxEntries: *entries, MaxBytes: *bytes, MaxDepth: *depth, MaxOwnerAttributes: *ownerAttributes, MaxOwnerAttributeBytes: *ownerAttributeBytes}
	var report any
	if verify {
		expected := durablevolume.Reference{Path: expectedPath, Sha256: expectedHash}
		if compareReviewedRebound {
			report, err = owner.VerifyReboundInventory(ctx, expected, fence, limits)
		} else {
			report, err = owner.VerifyInventory(ctx, expected, fence, limits)
		}
	} else {
		report, err = owner.Inventory(ctx, fence, limits)
	}
	if err != nil {
		return storageInspectionFailure(stderr, err)
	}
	raw, err := json.Marshal(report)
	if err := errors.Join(err, ctx.Err(), owner.Close()); err != nil {
		return storageInspectionFailure(stderr, err)
	}
	// Lease closure precedes potentially slow output. No partial inventory is
	// emitted when traversal, cancellation, identity, or owned closure fails.
	reportBytes := append(raw, '\n')
	written, err := stdout.Write(reportBytes)
	if err == nil && written != len(reportBytes) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return storageInspectionFailure(stderr, err)
	}
	return 0
}

// A busy/unavailable root is pending work, with a distinct retryable status.
func storageInspectionFailure(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "storage inspection:", err)
	if errors.Is(err, durablevolume.ErrIdentity) {
		return 3
	}
	if errors.Is(err, durablevolume.ErrBusy) || errors.Is(err, durablevolume.ErrUnavailable) {
		return 4
	}
	return 1
}
