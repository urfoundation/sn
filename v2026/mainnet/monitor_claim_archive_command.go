// Offline rollover publishes the original archive before the compact active
// head. Both owners must be joined, preprovisioned and explicitly reviewed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorClaimArchiveRequestSchema = "urnetwork-claim-monitor-archive-request-v1"
const monitorClaimArchivePlanSchema = "urnetwork-claim-monitor-archive-plan-v1"

type monitorClaimArchiveRequest struct {
	Schema            string                  `json:"schema"`
	Expected          identityExpectation     `json:"expected"`
	Policy            monitorClaimPolicy      `json:"policy"`
	Original          monitorHistoryReference `json:"original"`
	ArchivePath       string                  `json:"archive_path"`
	FormerWriterFence planFileReference       `json:"former_writer_fence"`
	FutureSegments    uint64                  `json:"future_segments"`
}

type monitorClaimArchivePlan struct {
	Schema            string                     `json:"schema"`
	Request           monitorClaimArchiveRequest `json:"request"`
	Declaration       durablevolume.Reference    `json:"declaration"`
	Archive           monitorHistoryReference    `json:"archive"`
	Next              monitorHistoryReference    `json:"next"`
	Segments          int                        `json:"segments"`
	RequiredSegments  uint64                     `json:"required_segments"`
	RequiredBytes     uint64                     `json:"required_bytes"`
	RequiredInodes    uint64                     `json:"required_inodes"`
	RestartAuthorized bool                       `json:"restart_authorized"`
	PlanHash          string                     `json:"plan_hash"`
}

func (self monitorClaimArchivePlan) hash() string {
	self.PlanHash = ""
	return rootObjectHash(self)
}

func validateMonitorClaimArchiveRequest(ctx context.Context, request monitorClaimArchiveRequest) error {
	if err := durablepath.Require(ctx); err != nil {
		return err
	}
	if request.Schema != monitorClaimArchiveRequestSchema {
		return errors.New("claim archive request schema differs")
	}
	maximumSegments := uint64(maximumMonitorHistorySegments)
	if request.Policy.HistoryCatalog != nil {
		maximumSegments = maximumReviewedMonitorHistorySegments
	}
	if request.FutureSegments == 0 || request.FutureSegments > maximumSegments/2 {
		return errors.New("claim archive requires a finite positive segment forecast with two-times margin")
	}
	if err := errors.Join(request.Policy.validate(request.Expected), request.Original.validate()); err != nil {
		return err
	}
	archive := request.Original
	archive.Path = request.ArchivePath
	if err := archive.validate(); err != nil {
		return err
	}
	if monitorHistoryPathsAlias(request.Original.Path, archive.Path) {
		return errors.New("claim archive aliases original checkpoint custody")
	}
	raw, err := readBootstrapChainInput(ctx, request.FormerWriterFence, 16*1024)
	if err != nil {
		return err
	}
	var fence monitorHistoryWriterFence
	if err := decodeMonitorHistoryInput(raw, &fence); err != nil {
		return err
	}
	if fence.Schema != monitorHistoryWriterFenceSchema || !fence.StoppedAndJoined || fence.Original != request.Original || fence.PolicyHash != request.Policy.archiveIdentityHash() {
		return errors.New("claim archive requires the exact stopped-and-joined original writer fence")
	}
	return nil
}

// This is pure after the original bytes and prefix have been authenticated.
func buildMonitorClaimArchivePlan(ctx context.Context, request monitorClaimArchiveRequest, original []byte, record monitorClaimCheckpointRecord, retained ...[]monitorHistoryReference) (plan monitorClaimArchivePlan, next []byte, resultErr error) {
	if uint64(len(original)) != request.Original.Bytes || monitorReadDigest(original) != request.Original.Sha256 {
		return plan, nil, errors.New("claim archive original checkpoint changed after review")
	}
	if err := record.Catalog.checkPath(request.Original.Path); err != nil {
		return plan, nil, err
	}
	if record.Archive != nil {
		for _, reference := range record.Archive.Segments {
			if monitorHistoryPathsAlias(reference.Path, request.ArchivePath) || monitorHistoryPathsAlias(reference.Path, request.Original.Path) {
				return plan, nil, errors.New("claim archive aliases a retained immutable segment")
			}
		}
	}
	archive := request.Original
	archive.Path = request.ArchivePath
	compacted, _, err := compactMonitorClaim(record, archive, request.Policy)
	if err != nil {
		return plan, nil, err
	}
	next, err = json.Marshal(compacted)
	if err != nil {
		return plan, nil, err
	}
	next = append(next, '\n')
	if len(next) > maxRpcReplyBytes {
		return plan, nil, errors.New("claim compact checkpoint exceeds its fixed capacity")
	}
	declaration, ok := durablevolume.ReferenceFromContext(ctx)
	if !ok {
		return plan, nil, errors.New("claim archive declaration is absent")
	}
	plan = monitorClaimArchivePlan{Schema: monitorClaimArchivePlanSchema, Request: request, Declaration: declaration, Archive: archive, Next: monitorHistoryReference{Path: request.Original.Path, Bytes: uint64(len(next)), Sha256: monitorReadDigest(next)}, Segments: len(compacted.Archive.Segments)}
	references := compacted.Archive.Segments
	if len(retained) != 0 {
		references = append(append([]monitorHistoryReference(nil), retained[0]...), archive)
	}
	plan.Segments = len(references)
	plan.RequiredSegments = 2 * (uint64(plan.Segments) + request.FutureSegments)
	capacity := record.Catalog.capacity(request.Policy.HistoryCatalog)
	if plan.RequiredSegments > capacity.Segments || plan.RequiredSegments > capacity.HeldReaders {
		return plan, nil, errors.New("claim archive segment forecast needs a reviewed catalog capacity revision")
	}
	forecast, err := monitorClaimCatalogForecast(compacted, request.FutureSegments, 0)
	if record.Window != nil {
		for _, reference := range references {
			raw, encodingErr := json.Marshal(reference)
			if encodingErr != nil {
				return plan, nil, encodingErr
			}
			forecast += 2 * uint64(len(raw)+1)
		}
	}
	if err != nil || forecast > capacity.CatalogBytes {
		return plan, nil, errors.Join(errors.New("claim archive metadata forecast needs a reviewed catalog capacity revision"), err)
	}
	retainedBytes := uint64(0)
	for _, reference := range references {
		retainedBytes += reference.Bytes
	}
	plan.RequiredBytes = 2 * (retainedBytes + request.FutureSegments*maxRpcReplyBytes)
	plan.RequiredInodes = 2 * (2*(uint64(plan.Segments)+request.FutureSegments) + 2)
	if err := monitorHistoryDeclarationForecast(declaration, []string{request.Original.Path, request.ArchivePath}, plan.RequiredBytes, plan.RequiredInodes); err != nil {
		return plan, nil, err
	}
	plan.PlanHash = plan.hash()
	return plan, next, nil
}

func planMonitorClaimArchive(ctx context.Context, request monitorClaimArchiveRequest) (plan monitorClaimArchivePlan, resultErr error) {
	if err := validateMonitorClaimArchiveRequest(ctx, request); err != nil {
		return plan, err
	}
	source, raw, err := openMonitorHistoryReader(ctx, request.Original)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	record, err := decodeMonitorClaimCheckpoint(raw, request.Policy)
	if err != nil {
		return plan, err
	}
	prior, err := openMonitorClaimArchive(ctx, request.Policy, record, request.Original.Path)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, prior.close()) }()
	record, err = hydrateMonitorClaimRecord(record, prior.epochStateKVs, request.Policy)
	if err != nil {
		return plan, err
	}
	archive, err := openMonitorHistorySnapshot(ctx, request.ArchivePath, false)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, archive.close()) }()
	existing, present, err := archive.read()
	if err != nil {
		return plan, err
	}
	if present && !bytes.Equal(existing, raw) {
		return plan, errors.New("claim archive destination already retains different bytes")
	}
	plan, _, err = buildMonitorClaimArchivePlan(ctx, request, raw, record, prior.references)
	err = errors.Join(err, prior.check())
	return plan, errors.Join(err, source.check(), archive.check(), ctx.Err())
}

// Lost acknowledgment is recoverable only from the exact original or planned
// next head. An unknown head/partial segment never permits guessed replay.
func applyMonitorClaimArchive(ctx context.Context, plan monitorClaimArchivePlan, hooks monitorServiceHooks) (resultErr error) {
	if plan.Schema != monitorClaimArchivePlanSchema || plan.RestartAuthorized || plan.PlanHash != plan.hash() {
		return errors.New("claim archive reviewed plan differs")
	}
	if err := validateMonitorClaimArchiveRequest(ctx, plan.Request); err != nil {
		return err
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	if declaration != plan.Declaration {
		return errors.New("claim archive durable declaration changed after review")
	}
	source, err := openMonitorHistorySnapshot(ctx, plan.Request.Original.Path, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	archive, err := openMonitorHistorySnapshot(ctx, plan.Request.ArchivePath, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, archive.close()) }()
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "archive-original")
	current, present, err := source.read()
	if err != nil {
		return fmt.Errorf("claim archive original checkpoint read: %w", err)
	}
	if !present {
		return errors.New("claim archive original owner disappeared")
	}
	retained, archived, err := archive.read()
	if err != nil {
		return err
	}
	original := current
	if archived {
		original = retained
	}
	record, err := decodeMonitorClaimCheckpoint(original, plan.Request.Policy)
	if err != nil {
		return err
	}
	prior, err := openMonitorClaimArchive(ctx, plan.Request.Policy, record, plan.Request.Original.Path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, prior.close()) }()
	record, err = hydrateMonitorClaimRecord(record, prior.epochStateKVs, plan.Request.Policy)
	if err != nil {
		return err
	}
	expected, next, err := buildMonitorClaimArchivePlan(ctx, plan.Request, original, record, prior.references)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, plan) {
		return errors.New("claim archive plan does not bind exact original and next heads")
	}
	if !bytes.Equal(current, original) && !bytes.Equal(current, next) {
		return errors.New("claim archive active checkpoint acquired unreviewed progress")
	}
	if !archived && !bytes.Equal(current, original) {
		return errors.New("claim compact checkpoint lost its original archive")
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return errors.Join(prior.check(), source.check(), archive.check())
	}
	hook := func(kind string) func(*os.File) error {
		if hooks.syncDirectory == nil {
			return nil
		}
		return func(file *os.File) error { return hooks.syncDirectory(plan.Request.Policy.Role, kind, file) }
	}
	if err := check(); err != nil {
		return err
	}
	if !archived {
		if err := archive.publish(original, hook("archive")); err != nil {
			return err
		}
		hooks.beforeHistoryRead(plan.Request.Policy.Role, "archive-published")
		retained, present, err = archive.read()
		if err != nil {
			return fmt.Errorf("claim archive publication read: %w", err)
		}
		if !present || !bytes.Equal(retained, original) {
			return errors.New("claim archive publication cannot be authenticated")
		}
	}
	if err := check(); err != nil {
		return err
	}
	if !bytes.Equal(current, next) {
		if err := source.publish(next, hook("checkpoint")); err != nil {
			return err
		}
	}
	return check()
}

// Actual public entrypoint; stdout loss never resets either retained owner.
func runMonitorClaimArchive(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	if len(args) != 0 && args[0] == "restore-request" {
		return runMonitorClaimArchiveRestoreRequest(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "restore-cohort-plan" {
		return runMonitorClaimArchiveRestoreCohort(ctx, args[1:], stdout, stderr)
	}
	if len(args) == 0 || (args[0] != "plan" && args[0] != "apply") {
		fmt.Fprintln(stderr, "usage: monitor-claim-archive plan --request FILE --request-sha256 HASH | apply --plan FILE --plan-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("monitor-claim-archive "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path, hash := "", ""
	if mode == "plan" {
		flags.StringVar(&path, "request", "", "exact offline request")
		flags.StringVar(&hash, "request-sha256", "", "accepted request digest")
	} else {
		flags.StringVar(&path, "plan", "", "reviewed offline plan")
		flags.StringVar(&hash, "plan-sha256", "", "accepted plan digest")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || path == "" || hash == "" {
		fmt.Fprintln(stderr, "claim archive requires an exact public document and digest")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, "claim archive declaration:", err)
		return 2
	}
	raw, digest, err := readPlanFile(ctx, path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "claim archive reviewed input read failed:", err)
		return 2
	}
	if digest != hash {
		fmt.Fprintln(stderr, "claim archive reviewed input differs: digest mismatch")
		return 2
	}
	var plan monitorClaimArchivePlan
	if mode == "plan" {
		var request monitorClaimArchiveRequest
		err = decodeMonitorHistoryInput(raw, &request)
		if err == nil {
			plan, err = planMonitorClaimArchive(ctx, request)
		}
	} else {
		err = decodeMonitorHistoryInput(raw, &plan)
		if err == nil {
			err = applyMonitorClaimArchive(ctx, plan, hooks)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "claim archive:", err)
		return 2
	}
	raw, err = json.Marshal(plan)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw = append(raw, '\n')
	written, err := stdout.Write(raw)
	if err != nil || written != len(raw) {
		fmt.Fprintln(stderr, "claim archive report not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
