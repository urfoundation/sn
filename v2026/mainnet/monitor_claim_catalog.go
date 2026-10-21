// A signed catalog revision preserves every original economic field and archive
// reference. Only the affected stopped owner adopts it; healthy roles keep their
// independent leases. Public preview/export never signs or activates a service.
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
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorClaimCatalogRequestSchema = "urnetwork-claim-monitor-catalog-request-v1"
const monitorClaimCatalogPlanSchema = "urnetwork-claim-monitor-catalog-plan-v1"

type monitorClaimCatalogRequest struct {
	Schema            string                  `json:"schema"`
	Expected          identityExpectation     `json:"expected"`
	Policy            monitorClaimPolicy      `json:"policy"`
	Original          monitorHistoryReference `json:"original"`
	FormerWriterFence planFileReference       `json:"former_writer_fence"`
	Capacity          monitorHistoryCapacity  `json:"capacity"`
	FutureSegments    uint64                  `json:"future_segments"`
}

type monitorClaimCatalogPlan struct {
	Schema            string                        `json:"schema"`
	Request           monitorClaimCatalogRequest    `json:"request"`
	Revision          monitorHistoryCatalogRevision `json:"revision"`
	SigningBytes      string                        `json:"signing_bytes_hex"`
	RestartAuthorized bool                          `json:"restart_authorized"`
	PlanHash          string                        `json:"plan_hash"`
}

func (self monitorClaimCatalogPlan) hash() string {
	self.PlanHash = ""
	return rootObjectHash(self)
}

func monitorClaimCatalogProgress(record monitorClaimCheckpointRecord) string {
	record.Catalog, record.ContentHash = nil, ""
	return rootObjectHash(record)
}

// JSON escaping can consume six bytes per pathname byte. Forecast that public
// bound, not the length of today's shorter path or the unused accounting slots.
func monitorClaimCatalogForecast(record monitorClaimCheckpointRecord, future, additional uint64) (uint64, error) {
	if future == 0 || future > maximumReviewedMonitorHistorySegments/2 || additional > maximumMonitorHistoryApprovalBytes {
		return 0, errors.New("claim history forecast exceeds its finite segment or revision bound")
	}
	raw, err := monitorClaimCatalogBytes(record)
	if err != nil {
		return 0, err
	}
	return 2 * (uint64(len(raw)) + future*(6*maximumMonitorHistoryPath+256) + additional), nil
}

// Active claim assertions have their own bound under a new catalog profile.
const maximumMonitorClaimActiveBytes = 768 * 1024

// Opted-in claim policies reserve separate active-state, review and catalog
// envelopes. Capacity growth cannot consume space needed by admitted evidence.
func monitorClaimCatalogHeadBudget(record monitorClaimCheckpointRecord, policy monitorClaimPolicy, capacity monitorHistoryCapacity) error {
	record.Archive, record.Catalog, record.Window = nil, nil, nil
	record.State = monitorClaimState{}
	entry := monitorProgressPolicyAcknowledgment{Resources: monitorProgressPolicyResources{FreshnessSeconds: 300, ReadBudgetSeconds: 900, Epochs: maximumMonitorRetainedClaimEpochs, EpochCapacity: maximumMonitorRetainedClaimEpochs, ReviewHistoryEntries: maximumMonitorProgressReviews}, PolicyHash: strings.Repeat("f", 64), ReviewSha256: "sha256:" + strings.Repeat("f", 64), PreviousSha256: "sha256:" + strings.Repeat("f", 64), ContentHash: "sha256:" + strings.Repeat("f", 64)}
	history := &monitorProgressPolicyHistory{LegacyCheckpointSha256: strings.Repeat("f", 64)}
	for range policy.resources().reviews() {
		history.Entries = append(history.Entries, entry)
	}
	record.PolicyHistory = history
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if uint64(len(raw))+maximumMonitorClaimActiveBytes+capacity.CatalogBytes+4096 > maxMonitorClaimCheckpointBytes {
		return errors.New("claim catalog exceeds the joined active-state, policy-review and checkpoint envelope")
	}
	return nil
}

func validateMonitorClaimCatalogRequest(ctx context.Context, request monitorClaimCatalogRequest) error {
	if err := durablepath.Require(ctx); err != nil {
		return err
	}
	if request.Schema != monitorClaimCatalogRequestSchema || request.Policy.HistoryCatalog == nil || request.FutureSegments == 0 || request.FutureSegments > maximumReviewedMonitorHistorySegments/2 {
		return errors.New("claim catalog revision requires its original opted-in authority and finite forecast")
	}
	if err := errors.Join(request.Policy.validate(request.Expected), request.Original.validate(), request.Capacity.validate()); err != nil {
		return err
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
		return errors.New("claim catalog requires the exact stopped-and-joined original owner fence")
	}
	return nil
}

func encodeMonitorClaimCheckpoint(record monitorClaimCheckpointRecord) ([]byte, error) {
	var err error
	record.ContentHash, err = hashMonitorClaimCheckpoint(record)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if len(raw)+1 > maxMonitorClaimCheckpointBytes {
		return nil, errors.New("claim checkpoint exceeds its fixed byte capacity")
	}
	return append(raw, '\n'), nil
}

// This derivation is pure except for read-only declaration admission. The caller
// holds the original and archive owners throughout planning and final readback.
func buildMonitorClaimCatalogPlan(ctx context.Context, request monitorClaimCatalogRequest, raw []byte, retained ...[]monitorHistoryReference) (plan monitorClaimCatalogPlan, resultErr error) {
	if uint64(len(raw)) != request.Original.Bytes || monitorReadDigest(raw) != request.Original.Sha256 {
		return plan, errors.New("claim catalog original checkpoint differs from reviewed bytes")
	}
	record, err := decodeMonitorClaimCheckpoint(raw, request.Policy)
	if err != nil {
		return plan, err
	}
	canonical, err := encodeMonitorClaimCheckpoint(record)
	if err != nil || !bytes.Equal(canonical, raw) {
		return plan, errors.Join(errors.New("claim catalog requires the original canonical checkpoint frame"), err)
	}
	if err := record.Catalog.checkPath(request.Original.Path); err != nil {
		return plan, err
	}
	capacity := record.Catalog.capacity(request.Policy.HistoryCatalog)
	if !request.Capacity.grows(capacity) {
		return plan, errors.New("claim catalog capacity must grow without shrinking a retained dimension")
	}
	ordinal := uint64(1)
	if record.Catalog != nil {
		ordinal += uint64(len(record.Catalog.Revisions))
	}
	if ordinal > maximumMonitorHistoryRevisions {
		return plan, errors.New("claim catalog approval history requires a separately reviewed successor profile")
	}
	segments, retainedBytes := uint64(0), uint64(0)
	var references []monitorHistoryReference
	if record.Archive != nil {
		references = record.Archive.Segments
	}
	if len(retained) != 0 {
		references = retained[0]
	}
	segments = uint64(len(references))
	for _, reference := range references {
		retainedBytes += reference.Bytes
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	revision := monitorHistoryCatalogRevision{Schema: monitorHistoryCatalogRevisionSchema, Role: request.Policy.Role,
		PolicyHash: request.Policy.archiveIdentityHash(), Ordinal: ordinal, Previous: record.Catalog.previous(request.Policy.archiveIdentityHash()),
		Original: request.Original, ProgressHash: monitorClaimCatalogProgress(record), From: capacity, To: request.Capacity,
		FutureSegments: request.FutureSegments, RequiredSegments: 2 * (segments + request.FutureSegments),
		RequiredBytes: 2 * (retainedBytes + (request.FutureSegments+1)*maxRpcReplyBytes), RequiredInodes: 2 * (2*(segments+request.FutureSegments) + 2),
		Declaration: declaration, FormerWriterFence: request.FormerWriterFence}
	revision.RequiredCatalogBytes, err = monitorClaimCatalogForecast(record, request.FutureSegments, maximumMonitorHistoryApprovalBytes)
	if record.Window != nil {
		for _, reference := range references {
			raw, encodingErr := json.Marshal(reference)
			if encodingErr != nil {
				return plan, encodingErr
			}
			revision.RequiredCatalogBytes += 2 * uint64(len(raw)+1)
		}
	}
	if err != nil {
		return plan, err
	}
	if _, err := revision.signingBytes(); err != nil {
		return plan, err
	}
	// Signature hex has fixed size. Bound the complete importable envelope before
	// exporting signing bytes, including its framing and required report newline.
	frame, err := json.Marshal(monitorHistoryCatalogApproval{Schema: monitorHistoryCatalogApprovalSchema, Revision: revision, Signature: strings.Repeat("0", 128)})
	if err != nil || len(frame)+1 > maximumMonitorHistoryApprovalBytes {
		return plan, errors.Join(errors.New("claim catalog preview cannot produce a bounded approval frame"), err)
	}
	if err := errors.Join(monitorClaimCatalogHeadBudget(record, request.Policy, request.Capacity),
		monitorHistoryDeclarationForecast(declaration, []string{request.Original.Path}, revision.RequiredBytes, revision.RequiredInodes)); err != nil {
		return plan, err
	}
	plan = monitorClaimCatalogPlan{Schema: monitorClaimCatalogPlanSchema, Request: request, Revision: revision}
	plan.SigningBytes, err = monitorHistorySigningHex(revision)
	if err != nil {
		return plan, err
	}
	plan.PlanHash = plan.hash()
	return plan, nil
}

func planMonitorClaimCatalog(ctx context.Context, request monitorClaimCatalogRequest) (plan monitorClaimCatalogPlan, resultErr error) {
	if err := validateMonitorClaimCatalogRequest(ctx, request); err != nil {
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
	if _, err := hydrateMonitorClaimRecord(record, prior.epochStateKVs, request.Policy); err != nil {
		return plan, err
	}
	plan, err = buildMonitorClaimCatalogPlan(ctx, request, raw, prior.references)
	err = errors.Join(err, prior.check())
	return plan, errors.Join(err, source.check(), ctx.Err())
}

// Lost acknowledgment can select only the exact old head or its one signed next
// head. Later progress, a foreign approval or incomplete bytes never get reset.
func applyMonitorClaimCatalog(ctx context.Context, plan monitorClaimCatalogPlan, approval monitorHistoryCatalogApproval, hooks monitorServiceHooks) (resultErr error) {
	if plan.Schema != monitorClaimCatalogPlanSchema || plan.RestartAuthorized || plan.PlanHash != plan.hash() || approval.Revision != plan.Revision {
		return errors.New("claim catalog plan or approval differs from its exact reviewed revision")
	}
	if err := errors.Join(validateMonitorClaimCatalogRequest(ctx, plan.Request), approval.validate(plan.Request.Policy.HistoryCatalog)); err != nil {
		return err
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	if declaration != plan.Revision.Declaration {
		return errors.New("claim catalog declaration changed after independent approval")
	}
	source, err := openMonitorHistorySnapshot(ctx, plan.Request.Original.Path, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "catalog-original")
	current, present, err := source.read()
	if err != nil {
		return fmt.Errorf("claim catalog original checkpoint read: %w", err)
	}
	if !present {
		return errors.New("claim catalog original checkpoint is absent")
	}
	record, err := decodeMonitorClaimCheckpoint(current, plan.Request.Policy)
	if err != nil {
		return err
	}
	original := current
	if catalog := record.Catalog; catalog != nil && len(catalog.Revisions) != 0 && reflect.DeepEqual(catalog.Revisions[len(catalog.Revisions)-1], approval) {
		if monitorClaimCatalogProgress(record) != approval.Revision.ProgressHash {
			return errors.New("claim catalog already has later progress; original approval cannot rewrite it")
		}
		if len(catalog.Revisions) == 1 {
			record.Catalog = nil
		} else {
			record.Catalog = &monitorHistoryCatalogState{Revisions: append([]monitorHistoryCatalogApproval(nil), catalog.Revisions[:len(catalog.Revisions)-1]...)}
		}
		original, err = encodeMonitorClaimCheckpoint(record)
		if err != nil {
			return err
		}
	}
	record, err = decodeMonitorClaimCheckpoint(original, plan.Request.Policy)
	if err != nil {
		return err
	}
	prior, err := openMonitorClaimArchive(ctx, plan.Request.Policy, record, plan.Request.Original.Path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, prior.close()) }()
	if _, err := hydrateMonitorClaimRecord(record, prior.epochStateKVs, plan.Request.Policy); err != nil {
		return err
	}
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "catalog-forecast")
	expected, err := buildMonitorClaimCatalogPlan(ctx, plan.Request, original, prior.references)
	if err != nil {
		return fmt.Errorf("claim catalog reviewed plan reconstruction: %w", err)
	}
	if !reflect.DeepEqual(expected, plan) {
		return errors.New("claim catalog approval lost exact original progress or forecast")
	}
	revisions := []monitorHistoryCatalogApproval{}
	if record.Catalog != nil {
		revisions = append(revisions, record.Catalog.Revisions...)
	}
	record.Catalog = &monitorHistoryCatalogState{Revisions: append(revisions, approval)}
	if err := record.Catalog.validate(plan.Request.Policy.HistoryCatalog, plan.Request.Policy.Role, plan.Request.Policy.archiveIdentityHash(), plan.Request.Original.Path); err != nil {
		return err
	}
	next, err := encodeMonitorClaimCheckpoint(record)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) && !bytes.Equal(current, next) {
		return errors.New("claim catalog checkpoint acquired unreviewed progress")
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return errors.Join(prior.check(), source.check())
	}
	if err := check(); err != nil {
		return err
	}
	if !bytes.Equal(current, next) {
		var afterSync func(*os.File) error
		if hooks.syncDirectory != nil {
			afterSync = func(file *os.File) error {
				return hooks.syncDirectory(plan.Request.Policy.Role, "catalog-checkpoint", file)
			}
		}
		if err := source.publish(next, afterSync); err != nil {
			return err
		}
	}
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "catalog-published")
	retained, present, err := source.read()
	if err != nil {
		return fmt.Errorf("claim catalog publication read: %w", err)
	}
	if !present || !bytes.Equal(retained, next) {
		return errors.New("claim catalog publication cannot be authenticated")
	}
	return check()
}

// Only public documents and an independent signature are imported. No private
// key, signer device, RPC or service manager is reachable from this dispatcher.
func runMonitorClaimCatalog(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	if len(args) == 0 || (args[0] != "plan" && args[0] != "apply") {
		fmt.Fprintln(stderr, "usage: monitor-claim-catalog plan --request FILE --request-sha256 HASH | apply --plan FILE --plan-sha256 HASH --approval FILE --approval-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("monitor-claim-catalog "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path, hash, approvalPath, approvalHash := "", "", "", ""
	if mode == "plan" {
		flags.StringVar(&path, "request", "", "exact original owner and requested capacities")
		flags.StringVar(&hash, "request-sha256", "", "accepted request digest")
	} else {
		flags.StringVar(&path, "plan", "", "reviewed capacity plan")
		flags.StringVar(&hash, "plan-sha256", "", "accepted plan digest")
		flags.StringVar(&approvalPath, "approval", "", "independent signed revision")
		flags.StringVar(&approvalHash, "approval-sha256", "", "accepted approval digest")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || path == "" || hash == "" || mode == "apply" && (approvalPath == "" || approvalHash == "") {
		fmt.Fprintln(stderr, "claim catalog requires exact public documents and digests")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, "claim catalog declaration:", err)
		return 2
	}
	raw, digest, err := readPlanFile(ctx, path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "claim catalog input read failed:", err)
		return 2
	}
	if digest != hash {
		fmt.Fprintln(stderr, "claim catalog input differs: digest mismatch")
		return 2
	}
	var plan monitorClaimCatalogPlan
	if mode == "plan" {
		var request monitorClaimCatalogRequest
		err = decodeMonitorHistoryInput(raw, &request)
		if err == nil {
			plan, err = planMonitorClaimCatalog(ctx, request)
		}
	} else {
		err = decodeMonitorHistoryInput(raw, &plan)
		var approval monitorHistoryCatalogApproval
		if err == nil {
			raw, digest, err = readPlanFile(ctx, approvalPath, maximumMonitorHistoryApprovalBytes)
			if err == nil && digest != approvalHash {
				err = errors.New("claim catalog approval digest differs")
			}
		}
		if err == nil {
			err = decodeMonitorHistoryInput(raw, &approval)
		}
		if err == nil {
			err = applyMonitorClaimCatalog(ctx, plan, approval, hooks)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "claim catalog:", err)
		return 2
	}
	raw, err = json.Marshal(plan)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "claim catalog report not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
