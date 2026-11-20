//go:build linux || darwin

// Public resource planning emits an unsigned successor from retained local
// custody. It neither loads a private key nor activates a producer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/validator"
)

// The request hash commits every forecast and physical root/fence bound.
func runValidatorCapacityPreview(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validator-capacity-preview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "Reviewed exact original config, retained heads, finite roots and future resource forecasts")
	hash := flags.String("request-sha256", "", "Exact sha256: digest of the unsigned planning request")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" || !planSha256(*hash) {
		fmt.Fprintln(stderr, "capacity preview requires an exact request and hash")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw, err := readPlanReference(ctx, *path, planFileReference{Path: *path, Sha256: *hash}, 2*1024*1024)
	if err != nil {
		fmt.Fprintln(stderr, "capacity request:", err)
		return 2
	}
	var request validator.ProductionCapacityRequest
	if err := decodePlanJson(raw, &request); err != nil {
		fmt.Fprintln(stderr, "capacity request:", err)
		return 2
	}
	preview, err := validator.BuildProductionCapacityPreview(ctx, request)
	if err != nil {
		return storageInspectionFailure(stderr, err)
	}
	raw, err = json.Marshal(preview)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return storageInspectionFailure(stderr, err)
	}
	raw = append(raw, '\n')
	written, err := stdout.Write(raw)
	if err == nil && written != len(raw) {
		err = io.ErrShortWrite
	}
	if err != nil {
		fmt.Fprintln(stderr, "capacity preview output:", err)
		return 1
	}
	return 0
}

// Completion consumes an external public signature, not a signing key or
// physical journal. It emits the exact validated document without writing it.
func runValidatorCapacityConfig(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validator-capacity-config", flag.ContinueOnError)
	flags.SetOutput(stderr)
	previewPath := flags.String("preview", "", "Exact reviewed preview")
	previewHash := flags.String("preview-sha256", "", "Accepted preview digest")
	approvalPath := flags.String("approval", "", "Independent completed public approval")
	approvalHash := flags.String("approval-sha256", "", "Accepted approval digest")
	configPath := flags.String("config-path", "", "Future document path, never created by this command")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *previewPath == "" || *approvalPath == "" || *configPath == "" || !planSha256(*previewHash) || !planSha256(*approvalHash) {
		fmt.Fprintln(stderr, "capacity config requires exact preview, public approval and output path")
		return 2
	}
	previewRaw, err := readPlanReference(ctx, *previewPath, planFileReference{Path: *previewPath, Sha256: *previewHash}, 4*1024*1024)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var preview validator.ProductionCapacityPreview
	if err := decodePlanJson(previewRaw, &preview); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	approvalRaw, err := readPlanReference(ctx, *approvalPath, planFileReference{Path: *approvalPath, Sha256: *approvalHash}, 64*1024)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ref := validator.ReleaseEvidenceV2File{Path: *approvalPath, Bytes: uint64(len(approvalRaw)), SHA256: "0x" + strings.TrimPrefix(*approvalHash, "sha256:")}
	raw, err := validator.CompleteProductionCapacityDocument(ctx, *configPath, preview, ref)
	if err != nil {
		fmt.Fprintln(stderr, "capacity config:", err)
		return 2
	}
	written, err := stdout.Write(raw)
	if err != nil || written != len(raw) {
		fmt.Fprintln(stderr, "capacity config output:", errors.Join(io.ErrShortWrite, err))
		return 1
	}
	return 0
}
