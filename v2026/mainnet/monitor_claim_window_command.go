// Window adoption is an offline, signed operational change. It publishes the
// exact old checkpoint before reducing the hot census and emits the next policy
// explicitly; neither command starts a service or grants financial authority.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorClaimWindowRequestSchema = "urnetwork-claim-monitor-window-request-v1"
const monitorClaimWindowPlanSchema = "urnetwork-claim-monitor-window-plan-v1"
const monitorClaimWindowApprovalSchema = "urnetwork-claim-monitor-window-approval-v1"

type monitorClaimWindowRequest struct {
	Schema            string                  `json:"schema"`
	Expected          identityExpectation     `json:"expected"`
	Policy            monitorClaimPolicy      `json:"policy"`
	NextPolicy        monitorClaimPolicy      `json:"next_policy"`
	Original          monitorHistoryReference `json:"original"`
	ArchivePath       string                  `json:"archive_path"`
	FormerWriterFence planFileReference       `json:"former_writer_fence"`
	FutureSegments    uint64                  `json:"future_segments"`
	ReviewSha256      string                  `json:"review_sha256"`
}

type monitorClaimWindowPlan struct {
	Schema               string                       `json:"schema"`
	Request              monitorClaimWindowRequest    `json:"request"`
	Declaration          durablevolume.Reference      `json:"declaration"`
	Transition           monitorClaimWindowTransition `json:"transition"`
	SigningBytes         string                       `json:"signing_bytes_hex"`
	NextPolicy           monitorClaimPolicy           `json:"next_policy"`
	UnsignedStateSha256  string                       `json:"unsigned_state_sha256"`
	RequiredSegments     uint64                       `json:"required_segments"`
	RequiredCatalogBytes uint64                       `json:"required_catalog_bytes"`
	RequiredReceiptBytes uint64                       `json:"required_receipt_bytes"`
	RequiredBytes        uint64                       `json:"required_bytes"`
	RequiredInodes       uint64                       `json:"required_inodes"`
	RestartAuthorized    bool                         `json:"restart_authorized"`
	PlanHash             string                       `json:"plan_hash"`
}

type monitorClaimWindowApproval struct {
	Schema         string `json:"schema"`
	TransitionHash string `json:"transition_hash"`
	Signature      string `json:"signature_ed25519"`
}

func (self monitorClaimWindowPlan) hash() string { self.PlanHash = ""; return rootObjectHash(self) }

func validateMonitorClaimWindowRequest(ctx context.Context, request monitorClaimWindowRequest) error {
	if request.Schema != monitorClaimWindowRequestSchema || request.Policy.HistoryCatalog == nil || request.NextPolicy.Window != nil || request.NextPolicy.Renewal != nil || !planSha256(request.ReviewSha256) {
		return errors.New("Claim window requires the original independent approver and explicit next expectations")
	}
	if err := request.NextPolicy.validate(request.Expected); err != nil {
		return err
	}
	return validateMonitorClaimArchiveRequest(ctx, monitorClaimArchiveRequest{Schema: monitorClaimArchiveRequestSchema, Expected: request.Expected, Policy: request.Policy, Original: request.Original, ArchivePath: request.ArchivePath, FormerWriterFence: request.FormerWriterFence, FutureSegments: request.FutureSegments})
}

// The plan binds an unsigned next state. The fixed-size signature is supplied
// independently at apply; all financial observations are reconstructed first.
func buildMonitorClaimWindowPlan(ctx context.Context, request monitorClaimWindowRequest, raw []byte, record monitorClaimCheckpointRecord, prior *monitorClaimArchiveAdmission) (plan monitorClaimWindowPlan, next monitorClaimCheckpointRecord, resultErr error) {
	if uint64(len(raw)) != request.Original.Bytes || monitorReadDigest(raw) != request.Original.Sha256 {
		return plan, next, errors.New("Claim window original checkpoint differs from reviewed bytes")
	}
	if err := record.Catalog.checkPath(request.Original.Path); err != nil {
		return plan, next, err
	}
	if prior == nil || prior.windows == nil {
		return plan, next, errors.New("Claim window lacks complete retained history admission")
	}
	for _, reference := range prior.references {
		if monitorHistoryPathsAlias(reference.Path, request.ArchivePath) || monitorHistoryPathsAlias(reference.Path, request.Original.Path) {
			return plan, next, errors.New("Claim window aliases retained original history")
		}
	}
	if prior.windows.reviews[request.ReviewSha256] {
		return plan, next, errors.New("Claim window reused an archived independent review")
	}
	history, err := record.retainedClaimPolicyHistory(request.Policy)
	if err != nil {
		return plan, next, err
	}
	for _, entry := range history.Entries {
		if entry.ReviewSha256 == request.ReviewSha256 {
			return plan, next, errors.New("Claim window reused a prior policy review")
		}
	}
	archive := request.Original
	archive.Path = request.ArchivePath
	transition, state, err := deriveMonitorClaimWindow(request.Policy, request.NextPolicy, archive, record, request.ReviewSha256)
	if err != nil {
		return plan, next, err
	}
	nextPolicy := request.NextPolicy
	nextPolicy.Window = &monitorClaimWindowPolicy{Schema: monitorClaimWindowSchema, Ordinal: transition.Ordinal, OriginalPolicyHash: transition.OriginalPolicyHash, TransitionHash: transition.hash()}
	next = monitorClaimCheckpointRecord{Schema: monitorClaimCheckpointSchema, PolicyHash: nextPolicy.hash(), State: state, Catalog: record.Catalog,
		Window: &monitorClaimWindowState{Transition: transition, PreviousPolicy: request.Policy}}
	next.PolicyHistory = newMonitorProgressPolicyHistory(nextPolicy.resources(), nextPolicy.hash(), "")
	declaration, ok := durablevolume.ReferenceFromContext(ctx)
	if !ok {
		return plan, next, errors.New("Claim window durable declaration is absent")
	}
	plan = monitorClaimWindowPlan{Schema: monitorClaimWindowPlanSchema, Request: request, Declaration: declaration, Transition: transition, NextPolicy: nextPolicy, UnsignedStateSha256: rootObjectHash(next)}
	message, err := transition.signingBytes()
	if err != nil {
		return plan, next, err
	}
	plan.SigningBytes = hex.EncodeToString(message)
	capacity := record.Catalog.capacity(request.Policy.HistoryCatalog)
	plan.RequiredSegments = 2 * (uint64(len(prior.references)) + 1 + request.FutureSegments)
	if plan.RequiredSegments > capacity.Segments || plan.RequiredSegments > capacity.HeldReaders {
		return plan, next, errors.New("Claim window needs reviewed segment and reader growth before retirement")
	}
	plan.RequiredCatalogBytes, err = monitorClaimCatalogForecast(next, request.FutureSegments, 0)
	if err != nil {
		return plan, next, err
	}
	for _, reference := range prior.references {
		encoded, err := json.Marshal(reference)
		if err != nil {
			return plan, next, err
		}
		plan.RequiredCatalogBytes += 2 * uint64(len(encoded)+1)
	}
	if plan.RequiredCatalogBytes > capacity.CatalogBytes {
		return plan, next, errors.New("Claim window complete metadata forecast needs reviewed catalog growth")
	}
	retiredBytes := prior.windows.retainedBytes
	for _, epoch := range record.State.Epochs {
		if monitorClaimWindowRetirable(epoch) {
			encoded, err := json.Marshal(epoch)
			if err != nil {
				return plan, next, err
			}
			retiredBytes += uint64(len(encoded))
		}
	}
	plan.RequiredReceiptBytes = 2 * (retiredBytes + request.FutureSegments*maximumMonitorClaimActiveBytes)
	if plan.RequiredReceiptBytes > maximumMonitorClaimRetiredBytes {
		return plan, next, errors.New("Claim original receipt lookup forecast holds this window for reviewed capacity")
	}
	retainedBytes := request.Original.Bytes
	for _, reference := range prior.references {
		retainedBytes += reference.Bytes
	}
	plan.RequiredBytes = 2 * (retainedBytes + (request.FutureSegments+1)*maxMonitorClaimCheckpointBytes)
	plan.RequiredInodes = 2 * (2*(uint64(len(prior.references))+1+request.FutureSegments) + 2)
	if err := errors.Join(monitorClaimActiveBudget(next.State, nextPolicy), monitorClaimCatalogHeadBudget(next, nextPolicy, capacity), monitorHistoryDeclarationForecast(declaration, []string{request.Original.Path, request.ArchivePath}, plan.RequiredBytes, plan.RequiredInodes)); err != nil {
		return plan, next, err
	}
	// Bound final signature framing without treating the placeholder as approved.
	preview := next
	value := *next.Window
	value.Signature = strings.Repeat("0", 128)
	preview.Window = &value
	if _, err := encodeMonitorClaimCheckpoint(preview); err != nil {
		return plan, next, err
	}
	plan.PlanHash = plan.hash()
	return plan, next, ctx.Err()
}

func planMonitorClaimWindow(ctx context.Context, request monitorClaimWindowRequest) (plan monitorClaimWindowPlan, resultErr error) {
	if err := validateMonitorClaimWindowRequest(ctx, request); err != nil {
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
	prior, err := openMonitorClaimArchive(ctx, request.Policy, record, request.Original.Path, request.ArchivePath)
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
		return plan, errors.New("Claim window archive already retains different bytes")
	}
	plan, _, err = buildMonitorClaimWindowPlan(ctx, request, raw, record, prior)
	return plan, errors.Join(err, prior.check(), source.check(), archive.check(), ctx.Err())
}

// Exact old/next reconciliation survives archive or checkpoint acknowledgment
// loss. The original archive is never overwritten, and later progress refuses
// replay rather than being discarded to fit an old plan.
func applyMonitorClaimWindow(ctx context.Context, plan monitorClaimWindowPlan, approval monitorClaimWindowApproval, hooks monitorServiceHooks) (result monitorHistoryReference, resultErr error) {
	if plan.Schema != monitorClaimWindowPlanSchema || plan.RestartAuthorized || plan.PlanHash != plan.hash() || approval.Schema != monitorClaimWindowApprovalSchema || approval.TransitionHash != plan.Transition.hash() {
		return result, errors.New("Claim window plan or approval differs")
	}
	if err := validateMonitorClaimWindowRequest(ctx, plan.Request); err != nil {
		return result, err
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	if declaration != plan.Declaration {
		return result, errors.New("Claim window durable declaration changed after review")
	}
	source, err := openMonitorHistorySnapshot(ctx, plan.Request.Original.Path, true)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	archive, err := openMonitorHistorySnapshot(ctx, plan.Request.ArchivePath, true)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, archive.close()) }()
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "window-original")
	current, present, err := source.read()
	if err != nil {
		return result, fmt.Errorf("Claim window original read: %w", err)
	}
	if !present {
		return result, errors.New("Claim window original owner is absent")
	}
	retained, archived, err := archive.read()
	if err != nil {
		return result, err
	}
	original := current
	if archived {
		original = retained
	}
	record, err := decodeMonitorClaimCheckpoint(original, plan.Request.Policy)
	if err != nil {
		return result, err
	}
	prior, err := openMonitorClaimArchive(ctx, plan.Request.Policy, record, plan.Request.Original.Path, plan.Request.ArchivePath)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, prior.close()) }()
	record, err = hydrateMonitorClaimRecord(record, prior.epochStateKVs, plan.Request.Policy)
	if err != nil {
		return result, err
	}
	expected, next, err := buildMonitorClaimWindowPlan(ctx, plan.Request, original, record, prior)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(expected, plan) {
		return result, errors.New("Claim window lost exact original obligations or capacity forecast")
	}
	next.Window.Signature = approval.Signature
	if err := next.Window.validate(plan.NextPolicy, next.PolicyHistory); err != nil {
		return result, err
	}
	raw, err := encodeMonitorClaimCheckpoint(next)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(current, original) && !bytes.Equal(current, raw) {
		return result, errors.New("Claim window checkpoint acquired unreviewed progress")
	}
	if !archived && !bytes.Equal(current, original) {
		return result, errors.New("Claim window lost its original archive")
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
		return result, err
	}
	if !archived {
		if err := archive.publish(original, hook("window-archive")); err != nil {
			return result, err
		}
		hooks.beforeHistoryRead(plan.Request.Policy.Role, "window-archive-published")
		retained, present, err = archive.read()
		if err != nil {
			return result, fmt.Errorf("Claim window archive readback: %w", err)
		}
		if !present || !bytes.Equal(retained, original) {
			return result, errors.New("Claim window archive readback differs")
		}
	}
	if err := check(); err != nil {
		return result, err
	}
	if !bytes.Equal(current, raw) {
		if err := source.publish(raw, hook("window-checkpoint")); err != nil {
			return result, err
		}
	}
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "window-checkpoint-published")
	observed, present, err := source.read()
	if err != nil {
		return result, fmt.Errorf("Claim window checkpoint readback: %w", err)
	}
	if !present || !bytes.Equal(observed, raw) {
		return result, errors.New("Claim window checkpoint readback differs")
	}
	return monitorHistoryReference{Path: plan.Request.Original.Path, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, check()
}

// Public reports contain the exact importable next role policy. Signing remains
// outside this command, using the independently pinned operational catalog key.
func runMonitorClaimWindow(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	if len(args) == 0 || (args[0] != "plan" && args[0] != "apply") {
		fmt.Fprintln(stderr, "usage: monitor-claim-window plan --request FILE --request-sha256 HASH | apply --plan FILE --plan-sha256 HASH --approval FILE --approval-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("monitor-claim-window "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path, hash, approvalPath, approvalHash := "", "", "", ""
	if mode == "plan" {
		flags.StringVar(&path, "request", "", "exact independent next window")
		flags.StringVar(&hash, "request-sha256", "", "accepted request digest")
	} else {
		flags.StringVar(&path, "plan", "", "reviewed window plan")
		flags.StringVar(&hash, "plan-sha256", "", "accepted plan digest")
		flags.StringVar(&approvalPath, "approval", "", "independent signed transition")
		flags.StringVar(&approvalHash, "approval-sha256", "", "accepted approval digest")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || path == "" || hash == "" || mode == "apply" && (approvalPath == "" || approvalHash == "") {
		fmt.Fprintln(stderr, "Claim window requires exact public documents and digests")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, "Claim window declaration:", err)
		return 2
	}
	raw, digest, err := readPlanFile(ctx, path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "Claim window input read:", err)
		return 2
	}
	if digest != hash {
		fmt.Fprintln(stderr, "Claim window reviewed input differs")
		return 2
	}
	var plan monitorClaimWindowPlan
	var checkpoint monitorHistoryReference
	if mode == "plan" {
		var request monitorClaimWindowRequest
		err = decodeMonitorHistoryInput(raw, &request)
		if err == nil {
			plan, err = planMonitorClaimWindow(ctx, request)
		}
	} else {
		err = decodeMonitorHistoryInput(raw, &plan)
		var approval monitorClaimWindowApproval
		if err == nil {
			raw, digest, err = readPlanFile(ctx, approvalPath, maximumMonitorHistoryApprovalBytes)
			if err == nil && digest != approvalHash {
				err = errors.New("Claim window approval digest differs")
			}
		}
		if err == nil {
			err = decodeMonitorHistoryInput(raw, &approval)
		}
		if err == nil {
			checkpoint, err = applyMonitorClaimWindow(ctx, plan, approval, hooks)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "Claim window:", err)
		return 2
	}
	var report any = plan
	if mode == "apply" {
		report = struct {
			Schema            string                  `json:"schema"`
			PlanHash          string                  `json:"plan_hash"`
			Policy            monitorClaimPolicy      `json:"policy"`
			Checkpoint        monitorHistoryReference `json:"checkpoint"`
			RestartAuthorized bool                    `json:"restart_authorized"`
		}{"urnetwork-claim-monitor-window-adoption-v1", plan.PlanHash, plan.NextPolicy, checkpoint, false}
	}
	raw, err = json.Marshal(report)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "Claim window report not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
