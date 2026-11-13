// This finite offline command reads one bounded metadata file. It opens no
// route, signer, journal or device and cannot produce an executable action.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The caller independently supplies the exact metadata pin and reviewed source.
// Wrong pins are refused before SCALE decoding; a report never supplies approval.
func runRootCurrentCapabilitiesCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("root-capabilities", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("metadata", "", "local raw SCALE metadata14 file; no RPC URL or key material")
	metadataHash := flags.String("metadata-hash", "", "independently supplied Blake2b-256 metadata hash")
	source := flags.String("runtime-source-commit", "", "exact reviewed current-root source commit")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || ctx == nil || *path == "" || !rootCanonicalHash(*metadataHash) || !crv4.ReviewedNativeOwnerSource(*source) {
		fmt.Fprintln(stderr, "root-capabilities requires --metadata, --metadata-hash and the exact reviewed --runtime-source-commit")
		return 2
	}
	raw, fileHash, err := readPlanFile(ctx, *path, maxMetadataRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "root capability metadata unavailable:", err)
		return 1
	}
	metadata, digest, err := nativePinnedMetadata("0x"+hex.EncodeToString(raw), *metadataHash)
	if err != nil {
		fmt.Fprintln(stderr, "root capability metadata refused:", err)
		return 3
	}
	result, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, *source)
	if err != nil || ctx.Err() != nil {
		fmt.Fprintln(stderr, "root capability profile refused:", err, ctx.Err())
		return 3
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "root capability output:", err)
		return 1
	}
	if !result.MetadataInterfaceCompatible {
		return 3
	}
	return 0
}
