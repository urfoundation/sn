//go:build linux || darwin

// Offline cohort commands acquire every accepted root before applying any of
// them. Failed delivery retains the same review hash and original journals.
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

// Only explicit daemon cohorts are currently exposed; request contents cannot
// select owner-local authority or turn a read-only check into publication.
func runStoragePreparationCohort(ctx context.Context, args []string, stdout, stderr io.Writer, ownerLocal bool) int {
	if ownerLocal || len(args) == 0 || args[0] != "cohort-check" && args[0] != "cohort-apply" && args[0] != "cohort-config" {
		fmt.Fprintln(stderr, "storage cohort requires an explicit daemon check, apply or config")
		return 2
	}
	flags := flag.NewFlagSet("storage-prepare "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("cohort", "", "exact reviewed cohort file")
	hash := flags.String("cohort-sha256", "", "accepted complete cohort digest")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" || *hash == "" {
		fmt.Fprintln(stderr, "storage cohort requires only its exact file and digest")
		return 2
	}
	operation := durablepath.CheckPreparationCohort
	if args[0] == "cohort-apply" {
		operation = durablepath.ApplyPreparationCohort
	}
	result, err := operation(ctx, durablevolume.Reference{Path: *path, Sha256: *hash}, storagePreparationAdapter(false))
	if err != nil {
		fmt.Fprintln(stderr, "storage cohort:", err)
		return 2
	}
	raw, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintln(stderr, "storage cohort result:", err)
		return 2
	}
	raw = append(raw, '\n')
	if args[0] == "cohort-config" {
		if !result.Complete || result.DeclarationDocument == "" {
			fmt.Fprintln(stderr, "storage cohort remains incomplete; no runtime declaration is available")
			return 2
		}
		raw = []byte(result.DeclarationDocument)
	}
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "storage cohort result was not fully delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
