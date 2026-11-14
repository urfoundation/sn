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
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorNativeArchiveRequestSchema = "urnetwork-native-monitor-archive-request-v1"
const monitorNativeArchivePlanSchema = "urnetwork-native-monitor-archive-plan-v1"
const monitorHistoryWriterFenceSchema = "urnetwork-monitor-history-writer-fence-v1"

type monitorNativeArchiveRequest struct {
	Schema            string                      `json:"schema"`
	Expected          identityExpectation         `json:"expected"`
	Policy            monitorEconomicNativePolicy `json:"policy"`
	Original          monitorHistoryReference     `json:"original"`
	ArchivePath       string                      `json:"archive_path"`
	FormerWriterFence planFileReference           `json:"former_writer_fence"`
	FutureSegments    uint64                      `json:"future_segments"`
}

// The fence is a protected operator assertion, not chain/signing authority.
// Actual nonblocking per-file leases independently exclude an active writer.
type monitorHistoryWriterFence struct {
	Schema           string                  `json:"schema"`
	Original         monitorHistoryReference `json:"original"`
	PolicyHash       string                  `json:"policy_hash"`
	StoppedAndJoined bool                    `json:"stopped_and_joined"`
}

type monitorNativeArchivePlan struct {
	Schema            string                      `json:"schema"`
	Request           monitorNativeArchiveRequest `json:"request"`
	Declaration       durablevolume.Reference     `json:"declaration"`
	Archive           monitorHistoryReference     `json:"archive"`
	Next              monitorHistoryReference     `json:"next"`
	Segments          int                         `json:"segments"`
	RequiredSegments  uint64                      `json:"required_segments"`
	RequiredBytes     uint64                      `json:"required_bytes"`
	RequiredInodes    uint64                      `json:"required_inodes"`
	RestartAuthorized bool                        `json:"restart_authorized"`
	PlanHash          string                      `json:"plan_hash"`
}

func (self monitorNativeArchivePlan) hash() string {
	self.PlanHash = ""
	return rootObjectHash(self)
}

func decodeMonitorHistoryInput(raw []byte, value any) error {
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("monitor history input has trailing JSON")
	}
	return nil
}

func validateMonitorNativeArchiveRequest(ctx context.Context, request monitorNativeArchiveRequest) error {
	if err := durablepath.Require(ctx); err != nil {
		return err
	}
	if request.Schema != monitorNativeArchiveRequestSchema {
		return errors.New("native archive request schema differs")
	}
	maximumSegments := uint64(maximumMonitorHistorySegments)
	if request.Policy.HistoryCatalog != nil {
		maximumSegments = maximumReviewedMonitorHistorySegments
	}
	if request.FutureSegments == 0 || request.FutureSegments > maximumSegments/2 {
		return errors.New("native archive requires a finite positive segment forecast with two-times margin")
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
		return errors.New("native archive aliases original checkpoint custody")
	}
	raw, err := readBootstrapChainInput(ctx, request.FormerWriterFence, 16*1024)
	if err != nil {
		return err
	}
	var fence monitorHistoryWriterFence
	if err := decodeMonitorHistoryInput(raw, &fence); err != nil {
		return err
	}
	if fence.Schema != monitorHistoryWriterFenceSchema || !fence.StoppedAndJoined || fence.Original != request.Original || fence.PolicyHash != request.Policy.identityHash() {
		return errors.New("native archive requires the exact stopped-and-joined original writer fence")
	}
	return nil
}

func monitorHistoryPathsAlias(a, b string) bool { return a == b || a+".lock" == b || a == b+".lock" }

// This is pure after the original bytes and prefix have been authenticated.
func buildMonitorNativeArchivePlan(ctx context.Context, request monitorNativeArchiveRequest, original []byte) (plan monitorNativeArchivePlan, next []byte, resultErr error) {
	if uint64(len(original)) != request.Original.Bytes || monitorReadDigest(original) != request.Original.Sha256 {
		return plan, nil, errors.New("native archive original checkpoint changed after review")
	}
	record, err := decodeMonitorEconomicNativeCheckpoint(original, request.Policy, ctx)
	if err != nil {
		return plan, nil, err
	}
	if err := record.State.Catalog.checkPath(request.Original.Path); err != nil {
		return plan, nil, err
	}
	if record.State.Archive != nil {
		for _, reference := range record.State.Archive.Segments {
			if monitorHistoryPathsAlias(reference.Path, request.ArchivePath) || monitorHistoryPathsAlias(reference.Path, request.Original.Path) {
				return plan, nil, errors.New("native archive aliases a retained immutable segment")
			}
		}
	}
	archive := request.Original
	archive.Path = request.ArchivePath
	compacted, err := compactMonitorEconomicNative(record, archive, request.Policy)
	if err != nil {
		return plan, nil, err
	}
	next, err = json.Marshal(compacted)
	if err != nil {
		return plan, nil, err
	}
	next = append(next, '\n')
	if len(next) > maxRpcReplyBytes {
		return plan, nil, errors.New("native compact checkpoint exceeds its fixed capacity")
	}
	declaration, ok := durablevolume.ReferenceFromContext(ctx)
	if !ok {
		return plan, nil, errors.New("native archive declaration is absent")
	}
	plan = monitorNativeArchivePlan{Schema: monitorNativeArchivePlanSchema, Request: request, Declaration: declaration, Archive: archive, Next: monitorHistoryReference{Path: request.Original.Path, Bytes: uint64(len(next)), Sha256: monitorReadDigest(next)}, Segments: len(compacted.State.Archive.Segments)}
	plan.RequiredSegments = 2 * (uint64(plan.Segments) + request.FutureSegments)
	capacity := record.State.Catalog.capacity(request.Policy.HistoryCatalog)
	if plan.RequiredSegments > capacity.Segments || plan.RequiredSegments > capacity.HeldReaders {
		return plan, nil, errors.New("native archive segment forecast needs a reviewed catalog capacity revision")
	}
	if request.Policy.HistoryCatalog != nil {
		forecast, err := monitorNativeCatalogForecast(compacted, request.Policy, request.FutureSegments, 0)
		if err != nil || forecast > capacity.CatalogBytes {
			return plan, nil, errors.Join(errors.New("native archive metadata forecast needs a reviewed catalog capacity revision"), err)
		}
		if err := monitorNativeCatalogHeadBudget(compacted, request.Policy, capacity); err != nil {
			return plan, nil, err
		}
	}
	retainedBytes := uint64(0)
	for _, reference := range compacted.State.Archive.Segments {
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

// Physical reserves are separate from logical retention and reader capacities.
func monitorHistoryDeclarationForecast(declaration durablevolume.Reference, paths []string, requiredBytes, requiredInodes uint64) error {
	config, err := durablevolume.Load(declaration)
	if err != nil {
		return err
	}
	for _, path := range paths {
		found := false
		for _, volume := range config.Volumes {
			for _, root := range volume.StateRoots {
				relative, err := filepath.Rel(root.Path, path)
				if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
					if found || volume.MinAvailableBytes < requiredBytes || volume.MinAvailableInodes < requiredInodes {
						return errors.New("native archive declaration lacks exact unique two-times byte/inode forecast floors")
					}
					found = true
				}
			}
		}
		if !found {
			return errors.New("native archive forecast is outside declared custody")
		}
	}
	return nil
}

func planMonitorNativeArchive(ctx context.Context, request monitorNativeArchiveRequest) (plan monitorNativeArchivePlan, resultErr error) {
	if err := validateMonitorNativeArchiveRequest(ctx, request); err != nil {
		return plan, err
	}
	source, raw, err := openMonitorHistoryReader(ctx, request.Original)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	record, err := decodeMonitorEconomicNativeCheckpoint(raw, request.Policy, ctx)
	if err != nil {
		return plan, err
	}
	prior, err := openMonitorEconomicNativeArchive(ctx, request.Policy, &record.State)
	if err != nil {
		return plan, err
	}
	defer func() {
		for _, owner := range prior {
			resultErr = errors.Join(resultErr, owner.close())
		}
	}()
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
		return plan, errors.New("native archive destination already retains different bytes")
	}
	plan, _, err = buildMonitorNativeArchivePlan(ctx, request, raw)
	for _, owner := range prior {
		err = errors.Join(err, owner.check())
	}
	return plan, errors.Join(err, source.check(), archive.check(), ctx.Err())
}

// Lost acknowledgment is recoverable only from the exact original or planned
// next head. An unknown head/partial segment never permits guessed replay.
func applyMonitorNativeArchive(ctx context.Context, plan monitorNativeArchivePlan, hooks monitorServiceHooks) (resultErr error) {
	if plan.Schema != monitorNativeArchivePlanSchema || plan.RestartAuthorized || plan.PlanHash != plan.hash() {
		return errors.New("native archive reviewed plan differs")
	}
	if err := validateMonitorNativeArchiveRequest(ctx, plan.Request); err != nil {
		return err
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	if declaration != plan.Declaration {
		return errors.New("native archive durable declaration changed after review")
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
		return fmt.Errorf("native archive original checkpoint read: %w", err)
	}
	if !present {
		return errors.New("native archive original owner disappeared")
	}
	retained, archived, err := archive.read()
	if err != nil {
		return err
	}
	original := current
	if archived {
		original = retained
	}
	expected, next, err := buildMonitorNativeArchivePlan(ctx, plan.Request, original)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, plan) {
		return errors.New("native archive plan does not bind exact original and next heads")
	}
	if !bytes.Equal(current, original) && !bytes.Equal(current, next) {
		return errors.New("native archive active checkpoint acquired unreviewed progress")
	}
	if !archived && !bytes.Equal(current, original) {
		return errors.New("native compact checkpoint lost its original archive")
	}
	record, err := decodeMonitorEconomicNativeCheckpoint(original, plan.Request.Policy, ctx)
	if err != nil {
		return err
	}
	prior, err := openMonitorEconomicNativeArchive(ctx, plan.Request.Policy, &record.State)
	if err != nil {
		return err
	}
	defer func() {
		for _, owner := range prior {
			resultErr = errors.Join(resultErr, owner.close())
		}
	}()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, owner := range prior {
			if err := owner.check(); err != nil {
				return err
			}
		}
		return errors.Join(source.check(), archive.check())
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
			return fmt.Errorf("native archive publication read: %w", err)
		}
		if !present || !bytes.Equal(retained, original) {
			return errors.New("native archive publication cannot be authenticated")
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
func runMonitorNativeArchive(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	if len(args) != 0 && args[0] == "restore-cohort-plan" {
		return runMonitorNativeArchiveRestoreCohort(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "restore-request" {
		return runMonitorNativeArchiveRestoreRequest(ctx, args[1:], stdout, stderr)
	}
	if len(args) == 0 || (args[0] != "plan" && args[0] != "apply") {
		fmt.Fprintln(stderr, "usage: monitor-native-archive plan --request FILE --request-sha256 HASH | apply --plan FILE --plan-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("monitor-native-archive "+mode, flag.ContinueOnError)
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
		fmt.Fprintln(stderr, "native archive requires an exact public document and digest")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, "native archive declaration:", err)
		return 2
	}
	raw, digest, err := readPlanFile(ctx, path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "native archive reviewed input read failed:", err)
		return 2
	}
	if digest != hash {
		fmt.Fprintln(stderr, "native archive reviewed input differs: digest mismatch")
		return 2
	}
	var plan monitorNativeArchivePlan
	if mode == "plan" {
		var request monitorNativeArchiveRequest
		err = decodeMonitorHistoryInput(raw, &request)
		if err == nil {
			plan, err = planMonitorNativeArchive(ctx, request)
		}
	} else {
		err = decodeMonitorHistoryInput(raw, &plan)
		if err == nil {
			err = applyMonitorNativeArchive(ctx, plan, hooks)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "native archive:", err)
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
		fmt.Fprintln(stderr, "native archive report not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
