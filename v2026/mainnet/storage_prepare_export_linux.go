//go:build linux

// Retained export is a read-only prerequisite to reviewed semantic restoration.
// It includes original leaf identities while preserving all owner bytes/anchors.
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

// The explicit command chooses daemon or owner-local scope; a report grants no
// automatic rewrite, reconciliation, signing or restart of the original owner.
func runStoragePreparationExport(ctx context.Context, args []string, stdout, stderr io.Writer, ownerLocal bool) int {
	flags := flag.NewFlagSet("storage-prepare export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Exact retained root in its reviewed durable declaration")
	fencePath := flags.String("former-writer-fence", "", "Protected stopped-and-joined original writer assertion")
	fenceHash := flags.String("former-writer-fence-sha256", "", "Exact accepted assertion digest")
	limits := durablevolume.InventoryLimits{}
	flags.Uint64Var(&limits.MaxEntries, "max-entries", 1000, "Maximum original files/directories (at most 32768; includes namespace structure)")
	flags.Uint64Var(&limits.MaxBytes, "max-bytes", 64*1024*1024, "Maximum original bytes read (at most 1 TiB)")
	flags.Uint64Var(&limits.MaxDepth, "max-depth", 16, "Maximum original namespace depth (at most 32)")
	flags.Uint64Var(&limits.MaxOwnerAttributes, "max-owner-attributes", 1000, "Maximum retained owner attributes (at most 10000)")
	flags.Uint64Var(&limits.MaxOwnerAttributeBytes, "max-owner-attribute-bytes", 4*1024*1024, "Maximum retained attribute bytes (at most 16 MiB)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" || *fencePath == "" || *fenceHash == "" {
		fmt.Fprintln(stderr, "retained export requires an exact declared root and stopped-writer assertion digest")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, "retained export declaration:", err)
		return 2
	}
	open := durablepath.OpenVolume
	if ownerLocal {
		open = durablepath.OpenOwnerLocalVolume
	}
	owner, err := open(ctx, *root, durablevolume.Snapshot)
	if err != nil {
		return storageInspectionFailure(stderr, err)
	}
	defer owner.Close()
	report, err := owner.InventoryPhysical(ctx, durablevolume.Reference{Path: *fencePath, Sha256: *fenceHash}, limits)
	if err != nil {
		return storageInspectionFailure(stderr, err)
	}
	raw, err := json.Marshal(report)
	if err := errors.Join(err, ctx.Err(), owner.Close()); err != nil {
		return storageInspectionFailure(stderr, err)
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return storageInspectionFailure(stderr, err)
	}
	return 0
}
