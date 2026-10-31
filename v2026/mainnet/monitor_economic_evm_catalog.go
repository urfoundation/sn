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

const monitorEvmCatalogRequestSchema = "urnetwork-evm-monitor-catalog-request-v1"
const monitorEvmCatalogPlanSchema = "urnetwork-evm-monitor-catalog-plan-v1"

type monitorEvmCatalogRequest struct {
	Schema            string                   `json:"schema"`
	Expected          identityExpectation      `json:"expected"`
	Policy            monitorEconomicEvmPolicy `json:"policy"`
	Original          monitorHistoryReference  `json:"original"`
	FormerWriterFence planFileReference        `json:"former_writer_fence"`
	Capacity          monitorHistoryCapacity   `json:"capacity"`
	FutureSegments    uint64                   `json:"future_segments"`
}

type monitorEvmCatalogPlan struct {
	Schema            string                        `json:"schema"`
	Request           monitorEvmCatalogRequest      `json:"request"`
	Revision          monitorHistoryCatalogRevision `json:"revision"`
	SigningBytes      string                        `json:"signing_bytes_hex"`
	RestartAuthorized bool                          `json:"restart_authorized"`
	PlanHash          string                        `json:"plan_hash"`
}

func (self monitorEvmCatalogPlan) hash() string {
	self.PlanHash = ""
	return rootObjectHash(self)
}

func monitorEvmCatalogProgress(record monitorEconomicEvmCheckpoint) string {
	record.State.Catalog, record.ContentHash = nil, ""
	return rootObjectHash(record)
}

// JSON escaping can consume six bytes per pathname byte. Forecast that public
// bound, not the length of today's shorter path or the unused accounting slots.
func monitorEvmCatalogForecast(record monitorEconomicEvmCheckpoint, policy monitorEconomicEvmPolicy, future, additional uint64) (uint64, error) {
	if future == 0 || future > maximumReviewedMonitorHistorySegments/2 || additional > maximumMonitorHistoryApprovalBytes {
		return 0, errors.New("EVM history forecast exceeds its finite segment or revision bound")
	}
	raw, err := monitorEvmCatalogBytes(record.State)
	if err != nil {
		return 0, err
	}
	return 2 * (uint64(len(raw)) + future*(6*maximumMonitorHistoryPath+256) + additional), nil
}

// Reserve the complete accepted active history, original resource reviews and
// retention metadata in the unchanged one-MiB snapshot envelope. A capacity
// signature cannot make an otherwise accepted publication impossible to encode.
func monitorEvmCatalogHeadBudget(record monitorEconomicEvmCheckpoint, policy monitorEconomicEvmPolicy, capacity monitorHistoryCapacity) error {
	record.State.Archive, record.State.Catalog, record.State.History, record.State.Fees = nil, nil, nil, nil
	// The complete configured review allowance has its own serialization
	// cost, independent of today's short acknowledgment prefix.
	entry := monitorEvmResourceAcknowledgment{Resources: monitorEvmResources{ReadBudgetSeconds: 900, HistoryEntries: maximumMonitorEconomicEvents, BatchBlocks: maximumMonitorEvmBlocks, StallSeconds: 3600}, ReviewSha256: "sha256:" + strings.Repeat("f", 64), ReviewHistoryEntries: maximumMonitorEvmReviewHistoryEntries, PreviousSha256: "sha256:" + strings.Repeat("f", 64), ContentHash: "sha256:" + strings.Repeat("f", 64)}
	history := &monitorEvmResourceHistory{LegacyCheckpointSha256: "sha256:" + strings.Repeat("f", 64)}
	for range policy.reviewHistoryEntries() {
		history.Entries = append(history.Entries, entry)
	}
	record.ResourceHistory = history
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	maximum := uint64(len(raw)) + maximumMonitorEconomicBytes + capacity.CatalogBytes + 4096
	if maximum > maxRpcReplyBytes {
		return errors.New("EVM history revision exceeds the joined active-history, reviews and checkpoint envelope")
	}
	return nil
}

func validateMonitorEvmCatalogRequest(ctx context.Context, request monitorEvmCatalogRequest) error {
	if err := durablepath.Require(ctx); err != nil {
		return err
	}
	if request.Schema != monitorEvmCatalogRequestSchema || request.Policy.HistoryCatalog == nil || request.FutureSegments == 0 || request.FutureSegments > maximumReviewedMonitorHistorySegments/2 {
		return errors.New("EVM catalog revision requires its original opted-in authority and finite forecast")
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
	if fence.Schema != monitorHistoryWriterFenceSchema || !fence.StoppedAndJoined || fence.Original != request.Original || fence.PolicyHash != request.Policy.identityHash() {
		return errors.New("EVM catalog requires the exact stopped-and-joined original owner fence")
	}
	return nil
}

func encodeMonitorEvmCheckpoint(record monitorEconomicEvmCheckpoint) ([]byte, error) {
	record.ContentHash = record.hash()
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > maxRpcReplyBytes {
		return nil, errors.Join(errMonitorEconomicCapacity, err)
	}
	return append(raw, '\n'), nil
}

// This derivation is pure except for read-only declaration admission. The caller
// holds the original and archive owners throughout planning and final readback.
func buildMonitorEvmCatalogPlan(ctx context.Context, request monitorEvmCatalogRequest, raw []byte) (plan monitorEvmCatalogPlan, resultErr error) {
	if uint64(len(raw)) != request.Original.Bytes || monitorReadDigest(raw) != request.Original.Sha256 {
		return plan, errors.New("EVM catalog original checkpoint differs from reviewed bytes")
	}
	record, err := decodeMonitorEconomicEvmCheckpoint(raw, request.Policy)
	if err != nil {
		return plan, err
	}
	canonical, err := encodeMonitorEvmCheckpoint(record)
	if err != nil || !bytes.Equal(canonical, raw) {
		return plan, errors.Join(errors.New("EVM catalog requires the original canonical checkpoint frame"), err)
	}
	if err := record.State.Catalog.checkPath(request.Original.Path); err != nil {
		return plan, err
	}
	capacity := record.State.Catalog.capacity(request.Policy.HistoryCatalog)
	if !request.Capacity.grows(capacity) {
		return plan, errors.New("EVM catalog capacity must grow without shrinking a retained dimension")
	}
	ordinal := uint64(1)
	if record.State.Catalog != nil {
		ordinal += uint64(len(record.State.Catalog.Revisions))
	}
	if ordinal > maximumMonitorHistoryRevisions {
		return plan, errors.New("EVM catalog approval history requires a separately reviewed successor profile")
	}
	segments, retainedBytes := uint64(0), uint64(0)
	if record.State.Archive != nil {
		segments = uint64(len(record.State.Archive.Segments))
		for _, reference := range record.State.Archive.Segments {
			retainedBytes += reference.Bytes
		}
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	revision := monitorHistoryCatalogRevision{Schema: monitorHistoryCatalogRevisionSchema, Role: request.Policy.Role,
		PolicyHash: request.Policy.identityHash(), Ordinal: ordinal, Previous: record.State.Catalog.previous(request.Policy.identityHash()),
		Original: request.Original, ProgressHash: monitorEvmCatalogProgress(record), From: capacity, To: request.Capacity,
		FutureSegments: request.FutureSegments, RequiredSegments: 2 * (segments + request.FutureSegments),
		RequiredBytes: 2 * (retainedBytes + (request.FutureSegments+1)*maxRpcReplyBytes), RequiredInodes: 2 * (2*(segments+request.FutureSegments) + 2),
		Declaration: declaration, FormerWriterFence: request.FormerWriterFence}
	revision.RequiredCatalogBytes, err = monitorEvmCatalogForecast(record, request.Policy, request.FutureSegments, maximumMonitorHistoryApprovalBytes)
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
		return plan, errors.Join(errors.New("EVM catalog preview cannot produce a bounded approval frame"), err)
	}
	if err := errors.Join(monitorEvmCatalogHeadBudget(record, request.Policy, request.Capacity),
		monitorHistoryDeclarationForecast(declaration, []string{request.Original.Path}, revision.RequiredBytes, revision.RequiredInodes)); err != nil {
		return plan, err
	}
	plan = monitorEvmCatalogPlan{Schema: monitorEvmCatalogPlanSchema, Request: request, Revision: revision}
	plan.SigningBytes, err = monitorHistorySigningHex(revision)
	if err != nil {
		return plan, err
	}
	plan.PlanHash = plan.hash()
	return plan, nil
}

func planMonitorEvmCatalog(ctx context.Context, request monitorEvmCatalogRequest) (plan monitorEvmCatalogPlan, resultErr error) {
	if err := validateMonitorEvmCatalogRequest(ctx, request); err != nil {
		return plan, err
	}
	source, raw, err := openMonitorHistoryReader(ctx, request.Original)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	record, err := decodeMonitorEconomicEvmCheckpoint(raw, request.Policy)
	if err != nil {
		return plan, err
	}
	prior, err := openMonitorEconomicEvmArchive(ctx, request.Policy, record)
	if err != nil {
		return plan, err
	}
	defer func() {
		for _, owner := range prior {
			resultErr = errors.Join(resultErr, owner.close())
		}
	}()
	plan, err = buildMonitorEvmCatalogPlan(ctx, request, raw)
	for _, owner := range prior {
		err = errors.Join(err, owner.check())
	}
	return plan, errors.Join(err, source.check(), ctx.Err())
}

// Lost acknowledgment can select only the exact old head or its one signed next
// head. Later progress, a foreign approval or incomplete bytes never get reset.
func applyMonitorEvmCatalog(ctx context.Context, plan monitorEvmCatalogPlan, approval monitorHistoryCatalogApproval, hooks monitorServiceHooks) (resultErr error) {
	if plan.Schema != monitorEvmCatalogPlanSchema || plan.RestartAuthorized || plan.PlanHash != plan.hash() || approval.Revision != plan.Revision {
		return errors.New("EVM catalog plan or approval differs from its exact reviewed revision")
	}
	if err := errors.Join(validateMonitorEvmCatalogRequest(ctx, plan.Request), approval.validate(plan.Request.Policy.HistoryCatalog)); err != nil {
		return err
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	if declaration != plan.Revision.Declaration {
		return errors.New("EVM catalog declaration changed after independent approval")
	}
	source, err := openMonitorHistorySnapshot(ctx, plan.Request.Original.Path, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "catalog-original")
	current, present, err := source.read()
	if err != nil {
		return fmt.Errorf("EVM catalog original checkpoint read: %w", err)
	}
	if !present {
		return errors.New("EVM catalog original checkpoint is absent")
	}
	record, err := decodeMonitorEconomicEvmCheckpoint(current, plan.Request.Policy)
	if err != nil {
		return err
	}
	original := current
	if catalog := record.State.Catalog; catalog != nil && len(catalog.Revisions) != 0 && reflect.DeepEqual(catalog.Revisions[len(catalog.Revisions)-1], approval) {
		if monitorEvmCatalogProgress(record) != approval.Revision.ProgressHash {
			return errors.New("EVM catalog already has later progress; original approval cannot rewrite it")
		}
		if len(catalog.Revisions) == 1 {
			record.State.Catalog = nil
		} else {
			record.State.Catalog = &monitorHistoryCatalogState{Revisions: append([]monitorHistoryCatalogApproval(nil), catalog.Revisions[:len(catalog.Revisions)-1]...)}
		}
		original, err = encodeMonitorEvmCheckpoint(record)
		if err != nil {
			return err
		}
	}
	hooks.beforeHistoryRead(plan.Request.Policy.Role, "catalog-forecast")
	expected, err := buildMonitorEvmCatalogPlan(ctx, plan.Request, original)
	if err != nil {
		return fmt.Errorf("EVM catalog reviewed plan reconstruction: %w", err)
	}
	if !reflect.DeepEqual(expected, plan) {
		return errors.New("EVM catalog approval lost exact original progress or forecast")
	}
	record, err = decodeMonitorEconomicEvmCheckpoint(original, plan.Request.Policy)
	if err != nil {
		return err
	}
	prior, err := openMonitorEconomicEvmArchive(ctx, plan.Request.Policy, record)
	if err != nil {
		return err
	}
	defer func() {
		for _, owner := range prior {
			resultErr = errors.Join(resultErr, owner.close())
		}
	}()
	revisions := []monitorHistoryCatalogApproval{}
	if record.State.Catalog != nil {
		revisions = append(revisions, record.State.Catalog.Revisions...)
	}
	record.State.Catalog = &monitorHistoryCatalogState{Revisions: append(revisions, approval)}
	if err := errors.Join(record.State.Catalog.validate(plan.Request.Policy.HistoryCatalog, plan.Request.Policy.Role, plan.Request.Policy.identityHash(), plan.Request.Original.Path), record.State.validate(plan.Request.Policy)); err != nil {
		return err
	}
	next, err := encodeMonitorEvmCheckpoint(record)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) && !bytes.Equal(current, next) {
		return errors.New("EVM catalog checkpoint acquired unreviewed progress")
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, owner := range prior {
			if err := owner.check(); err != nil {
				return err
			}
		}
		return source.check()
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
		return fmt.Errorf("EVM catalog publication read: %w", err)
	}
	if !present || !bytes.Equal(retained, next) {
		return errors.New("EVM catalog publication cannot be authenticated")
	}
	return check()
}

// Only public documents and an independent signature are imported. No private
// key, signer device, RPC or service manager is reachable from this dispatcher.
func runMonitorEvmCatalog(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	if len(args) == 0 || (args[0] != "plan" && args[0] != "apply") {
		fmt.Fprintln(stderr, "usage: monitor-evm-catalog plan --request FILE --request-sha256 HASH | apply --plan FILE --plan-sha256 HASH --approval FILE --approval-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("monitor-evm-catalog "+mode, flag.ContinueOnError)
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
		fmt.Fprintln(stderr, "EVM catalog requires exact public documents and digests")
		return 2
	}
	if err := durablepath.Require(ctx); err != nil {
		fmt.Fprintln(stderr, "EVM catalog declaration:", err)
		return 2
	}
	raw, digest, err := readPlanFile(ctx, path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "EVM catalog input read failed:", err)
		return 2
	}
	if digest != hash {
		fmt.Fprintln(stderr, "EVM catalog input differs: digest mismatch")
		return 2
	}
	var plan monitorEvmCatalogPlan
	if mode == "plan" {
		var request monitorEvmCatalogRequest
		err = decodeMonitorHistoryInput(raw, &request)
		if err == nil {
			plan, err = planMonitorEvmCatalog(ctx, request)
		}
	} else {
		err = decodeMonitorHistoryInput(raw, &plan)
		var approval monitorHistoryCatalogApproval
		if err == nil {
			raw, digest, err = readPlanFile(ctx, approvalPath, maximumMonitorHistoryApprovalBytes)
			if err == nil && digest != approvalHash {
				err = errors.New("EVM catalog approval digest differs")
			}
		}
		if err == nil {
			err = decodeMonitorHistoryInput(raw, &approval)
		}
		if err == nil {
			err = applyMonitorEvmCatalog(ctx, plan, approval, hooks)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "EVM catalog:", err)
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
		fmt.Fprintln(stderr, "EVM catalog report not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
