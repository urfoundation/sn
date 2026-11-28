// Offline continuation retains the exact combined checkpoint before compacting
// settled observations. A signed operational revision may enlarge resources;
// neither the command nor its output grants economic or restart authority.
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

const economicConservationArchiveRequestSchema = "urnetwork-economic-conservation-archive-request-v1"
const economicConservationArchivePlanSchema = "urnetwork-economic-conservation-archive-plan-v1"

type economicConservationArchiveRequest struct {
	RetainPrincipalOriginals bool                               `json:"retain_principal_originals,omitempty"`
	NativeRenewal            *economicConservationNativeRenewal `json:"native_approval_adoption,omitempty"`
	ClaimWindows             []economicConservationClaimWindow  `json:"claim_windows,omitempty"`
	FeeRevision              *economicConservationFeeRevision   `json:"native_fee_revision,omitempty"`
	RetireNativeFees         bool                               `json:"retire_native_fees,omitempty"`
	Schema                   string                             `json:"schema"`
	Policy                   economicConservationPolicy         `json:"original_policy"`
	Original                 monitorHistoryReference            `json:"original"`
	ArchivePath              string                             `json:"archive_path"`
	FormerWriterFence        planFileReference                  `json:"former_writer_fence"`
	FutureSegments           uint64                             `json:"future_segments"`
	FutureIndexEntries       uint64                             `json:"future_index_entries"`
	FutureIndexBytes         uint64                             `json:"future_index_bytes"`
	Renewal                  *economicConservationRenewal       `json:"resource_renewal,omitempty"`
}

type economicConservationArchivePlan struct {
	Schema               string                             `json:"schema"`
	Request              economicConservationArchiveRequest `json:"request"`
	Declaration          durablevolume.Reference            `json:"declaration"`
	Archive              monitorHistoryReference            `json:"archive"`
	Next                 monitorHistoryReference            `json:"next"`
	Resources            economicConservationResources      `json:"resources"`
	RequiredSegments     uint64                             `json:"required_segments"`
	RequiredIndexEntries uint64                             `json:"required_index_entries"`
	RequiredIndexBytes   uint64                             `json:"required_index_bytes"`
	RequiredHeadBytes    uint64                             `json:"required_head_bytes"`
	RequiredBytes        uint64                             `json:"required_bytes"`
	RequiredInodes       uint64                             `json:"required_inodes"`
	RestartAuthorized    bool                               `json:"restart_authorized"`
	PlanHash             string                             `json:"plan_hash"`
}

func (self economicConservationArchivePlan) hash() string {
	self.PlanHash = ""
	return rootObjectHash(self)
}

func decodeEconomicConservation(ctx context.Context, raw []byte, policy economicConservationPolicy) (*economicConservationState, error) {
	if len(raw) == 0 || uint64(len(raw)) > policy.storageMaximum() {
		return nil, errors.New("economic conservation checkpoint exceeds its fixed byte capacity")
	}
	var state economicConservationState
	if err := decodeMonitorHistoryInput(raw, &state); err != nil {
		return nil, err
	}
	native, err := state.nativeOperatingPolicy(policy)
	if err != nil {
		return nil, err
	}
	if err := state.Native.admitRuntime(ctx, native); err != nil {
		return nil, err
	}
	return &state, state.validate(ctx, policy)
}

func validateEconomicConservationArchiveRequest(ctx context.Context, request economicConservationArchiveRequest) error {
	if err := durablepath.Require(ctx); err != nil {
		return err
	}
	if request.Schema != economicConservationArchiveRequestSchema || request.FutureSegments == 0 || request.FutureSegments > 256 || request.FutureIndexEntries == 0 || request.FutureIndexEntries > 512*1024 || request.FutureIndexBytes == 0 || request.FutureIndexBytes > 128*1024*1024 {
		return errors.New("economic archive requires separate finite positive resource forecasts")
	}
	if err := errors.Join(request.Policy.validate(), request.Policy.validateReference(request.Original)); err != nil {
		return err
	}
	archive := request.Original
	archive.Path = request.ArchivePath
	if err := request.Policy.validateReference(archive); err != nil {
		return err
	}
	if monitorHistoryPathsAlias(request.Original.Path, archive.Path) {
		return errors.New("economic archive aliases original checkpoint custody")
	}
	if request.Renewal != nil {
		if request.Renewal.Original != request.Original {
			return errors.New("economic resource renewal names another original checkpoint")
		}
		if err := request.Renewal.verify(request.Policy); err != nil {
			return err
		}
	}
	if request.FeeRevision != nil {
		if request.FeeRevision.Original != request.Original {
			return errors.New("economic fee revision names another original checkpoint")
		}
		if err := request.FeeRevision.verify(request.Policy); err != nil {
			return err
		}
	}
	if request.NativeRenewal != nil {
		if request.NativeRenewal.Original != request.Original {
			return errors.New("economic native adoption names another original checkpoint")
		}
		if err := request.NativeRenewal.verify(request.Policy); err != nil {
			return err
		}
	}
	if len(request.ClaimWindows) > len(request.Policy.Claims) {
		return errors.New("economic Claim adoption exceeds original role census")
	}
	for _, window := range request.ClaimWindows {
		if window.Original != request.Original {
			return errors.New("economic Claim adoption names another original checkpoint")
		}
		if err := window.verify(request.Policy); err != nil {
			return err
		}
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
		return errors.New("economic archive requires the exact stopped-and-joined original writer fence")
	}
	return nil
}

// The held view already authenticated every predecessor. Extend its index
// once for this proposed snapshot; no historical payload is read again here.
func buildEconomicConservationArchivePlan(ctx context.Context, request economicConservationArchiveRequest, original []byte, view *economicConservationArchiveView) (plan economicConservationArchivePlan, next []byte, resultErr error) {
	if uint64(len(original)) != request.Original.Bytes || monitorReadDigest(original) != request.Original.Sha256 {
		return plan, nil, errors.New("economic archive original checkpoint changed after review")
	}
	state, err := decodeEconomicConservation(ctx, original, request.Policy)
	if err != nil {
		return plan, nil, err
	}
	if view == nil {
		return plan, nil, errors.New("economic archive predecessor admission is absent")
	}
	state.archiveView = view
	if err := state.validateClaimCheckpointPath(request.Original.Path); err != nil {
		return plan, nil, err
	}
	if state.Renewal != nil && state.Renewal.Original.Path != request.Original.Path {
		return plan, nil, errors.New("economic archive moved a retained operational revision")
	}
	if state.FeeRevision != nil && state.FeeRevision.Original.Path != request.Original.Path {
		return plan, nil, errors.New("economic archive moved a retained fee revision")
	}
	if state.NativeRenewal != nil && state.NativeRenewal.Original.Path != request.Original.Path {
		return plan, nil, errors.New("economic archive moved a retained native adoption")
	}
	if state.Archive != nil {
		for _, reference := range state.Archive.Segments {
			if monitorHistoryPathsAlias(reference.Path, request.ArchivePath) || monitorHistoryPathsAlias(reference.Path, request.Original.Path) {
				return plan, nil, errors.New("economic archive aliases an original retained segment")
			}
		}
	}
	archive := request.Original
	archive.Path = request.ArchivePath
	compacted, err := compactEconomicConservationWithPrincipalRetention(ctx, request.Policy, state, archive, request.Renewal, request.RetireNativeFees, request.FeeRevision, request.RetainPrincipalOriginals)
	if err != nil {
		return plan, nil, err
	}
	resources, err := compacted.resources(request.Policy)
	if err != nil {
		return plan, nil, err
	}
	view.resources = resources
	if err := view.admit(ctx, state, compacted); err != nil {
		return plan, nil, err
	}
	if err := view.retainClaimWindows(request.Policy, state); err != nil {
		return plan, nil, err
	}
	if err := view.setClaimBasis(request.Policy, state, archive); err != nil {
		return plan, nil, err
	}
	// Fee compaction validates a detached index before admitting the original
	// segment. Subsequent adoptions must use this now-complete retained view.
	compacted.archiveView = view
	if err := applyEconomicConservationClaimWindows(ctx, request.Policy, compacted, request.ClaimWindows); err != nil {
		return plan, nil, err
	}
	if err := applyEconomicConservationNativeRenewal(ctx, request.Policy, compacted, request.NativeRenewal); err != nil {
		return plan, nil, err
	}
	if request.Renewal != nil && view.reviews[request.Renewal.ReviewSha256] {
		return plan, nil, errors.New("economic resource renewal reused its original or archived review")
	}
	if request.FeeRevision != nil && view.feeReviews[request.FeeRevision.ReviewSha256] {
		return plan, nil, errors.New("economic fee revision reused its original or archived review")
	}
	next, err = json.Marshal(compacted)
	if err != nil {
		return plan, nil, err
	}
	next = append(next, '\n')
	declaration, ok := durablevolume.ReferenceFromContext(ctx)
	if !ok {
		return plan, nil, errors.New("economic archive durable declaration is absent")
	}
	plan = economicConservationArchivePlan{Schema: economicConservationArchivePlanSchema, Request: request, Declaration: declaration, Archive: archive, Next: monitorHistoryReference{Path: request.Original.Path, Bytes: uint64(len(next)), Sha256: monitorReadDigest(next)}, Resources: resources}
	plan.RequiredSegments = 2 * (uint64(len(compacted.Archive.Segments)) + request.FutureSegments)
	plan.RequiredIndexEntries = 2 * (view.entries + view.claimBasisEntries + request.FutureIndexEntries)
	plan.RequiredIndexBytes = 2 * (view.bytes + view.claimBasisBytes + view.principalReservedBytes + request.FutureIndexBytes)
	// Catalog paths and hot liabilities share one fixed head. Reserving an
	// additional worst-case reference for each forecast segment avoids a count
	// revision accidentally exhausting the serialized owner before publication.
	// Admitted control characters can expand each pathname byte sixfold in JSON.
	referenceBytes, feeSummaryBytes := uint64(6*maximumMonitorHistoryPath+256), uint64(0)
	if request.RetainPrincipalOriginals {
		referenceBytes += 6*maximumMonitorHistoryPath + 256
	}
	if request.RetireNativeFees {
		// Each future retirement also names its proof segment and retains
		// one bounded census header, even if this snapshot has no fees yet.
		referenceBytes *= 2
		feeSummaryBytes = 2048
	}
	if request.FeeRevision != nil {
		// The next compaction retains a bounded policy/ordinal head alongside
		// any later signed revision, even when no fee payload is retired.
		feeSummaryBytes += 2048
	}
	if request.NativeRenewal != nil {
		// The next compaction retains the original bounded approval list in
		// its derived head, in addition to any future signed adoption frame.
		// Native review paths have their own grammar, so measure the actual
		// authenticated head instead of assuming monitor pathname limits.
		head, err := compacted.nativeApprovalHead(request.Policy)
		if err != nil {
			return plan, nil, err
		}
		raw, err := json.Marshal(head)
		if err != nil {
			return plan, nil, err
		}
		feeSummaryBytes += uint64(len(raw)) + 256
	}
	plan.RequiredHeadBytes = 2 * (uint64(len(next)) + request.FutureSegments*referenceBytes + feeSummaryBytes)
	if plan.RequiredSegments > resources.ArchiveSegments || plan.RequiredIndexEntries > resources.IndexEntries || plan.RequiredIndexBytes > resources.IndexBytes || plan.RequiredHeadBytes > resources.headBytes() {
		return plan, nil, errors.New("economic archive requires reviewed capacity for its two-times segment/index/head forecast")
	}
	retainedBytes := uint64(0)
	paths := []string{request.Original.Path}
	for _, reference := range compacted.Archive.Segments {
		retainedBytes += reference.Bytes
		paths = append(paths, reference.Path)
	}
	plan.RequiredBytes = 2 * (retainedBytes + (request.FutureSegments+1)*request.Policy.storageMaximum())
	plan.RequiredInodes = 2 * (2*(uint64(len(compacted.Archive.Segments))+request.FutureSegments) + 2)
	if err := monitorHistoryDeclarationForecast(declaration, paths, plan.RequiredBytes, plan.RequiredInodes); err != nil {
		return plan, nil, err
	}
	plan.PlanHash = plan.hash()
	return plan, next, errors.Join(ctx.Err(), view.check())
}

func planEconomicConservationArchive(ctx context.Context, request economicConservationArchiveRequest, hooks monitorServiceHooks) (plan economicConservationArchivePlan, resultErr error) {
	if err := validateEconomicConservationArchiveRequest(ctx, request); err != nil {
		return plan, err
	}
	source, raw, err := request.Policy.openHistoryReader(ctx, request.Original)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	state, err := decodeEconomicConservation(ctx, raw, request.Policy)
	if err != nil {
		return plan, err
	}
	prior, err := openEconomicConservationArchive(ctx, request.Policy, state, hooks)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, prior.close()) }()
	archive, err := request.Policy.openHistorySnapshot(ctx, request.ArchivePath, false)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, archive.close()) }()
	existing, present, err := archive.read()
	if err != nil {
		return plan, err
	}
	if present && !bytes.Equal(existing, raw) {
		return plan, errors.New("economic archive destination already retains different bytes")
	}
	plan, _, err = buildEconomicConservationArchivePlan(ctx, request, raw, prior)
	return plan, errors.Join(err, source.check(), archive.check(), prior.check(), ctx.Err())
}

// Only exact original/next heads reconcile a lost acknowledgment. Every read
// failure retains its cause before any comparison can assert a contradiction.
func applyEconomicConservationArchive(ctx context.Context, plan economicConservationArchivePlan, hooks monitorServiceHooks) (resultErr error) {
	if plan.Schema != economicConservationArchivePlanSchema || plan.RestartAuthorized || plan.PlanHash != plan.hash() {
		return errors.New("economic archive reviewed plan differs")
	}
	if err := validateEconomicConservationArchiveRequest(ctx, plan.Request); err != nil {
		return err
	}
	declaration, _ := durablevolume.ReferenceFromContext(ctx)
	if declaration != plan.Declaration {
		return errors.New("economic archive durable declaration changed after review")
	}
	source, err := plan.Request.Policy.openHistorySnapshot(ctx, plan.Request.Original.Path, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	archive, err := plan.Request.Policy.openHistorySnapshot(ctx, plan.Request.ArchivePath, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, archive.close()) }()
	hooks.beforeHistoryRead(economicConservationRole, "archive-original")
	current, present, err := source.read()
	if err != nil {
		return fmt.Errorf("economic archive original checkpoint read: %w", err)
	}
	if !present {
		return errors.New("economic archive original checkpoint is absent")
	}
	retained, archived, err := archive.read()
	if err != nil {
		return fmt.Errorf("economic archive retained snapshot read: %w", err)
	}
	original := current
	if archived {
		original = retained
	}
	state, err := decodeEconomicConservation(ctx, original, plan.Request.Policy)
	if err != nil {
		return err
	}
	prior, err := openEconomicConservationArchive(ctx, plan.Request.Policy, state, hooks)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, prior.close()) }()
	expected, next, err := buildEconomicConservationArchivePlan(ctx, plan.Request, original, prior)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, plan) {
		return errors.New("economic archive plan changed original or deterministic next custody")
	}
	if !bytes.Equal(current, original) && !bytes.Equal(current, next) {
		return errors.New("economic archive active checkpoint acquired unreviewed progress")
	}
	if !archived && !bytes.Equal(current, original) {
		return errors.New("economic compact checkpoint lost its original archive")
	}
	check := func() error { return errors.Join(ctx.Err(), source.check(), archive.check(), prior.check()) }
	hook := func(kind string) func(*os.File) error {
		if hooks.syncDirectory == nil {
			return nil
		}
		return func(file *os.File) error { return hooks.syncDirectory(economicConservationRole, kind, file) }
	}
	if err := check(); err != nil {
		return err
	}
	if !archived {
		if err := archive.publish(original, hook("archive")); err != nil {
			return err
		}
		hooks.beforeHistoryRead(economicConservationRole, "archive-published")
		retained, present, err = archive.read()
		if err != nil {
			return fmt.Errorf("economic archive publication read: %w", err)
		}
		if !present || !bytes.Equal(retained, original) {
			return errors.New("economic archive publication differs from exact original bytes")
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

// No signer is loaded here. Operators supply an independently signed optional
// resource revision in the request, then explicitly apply the exact plan.
func runEconomicConservationArchive(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	if len(args) != 0 && args[0] == "native-proposal" {
		return runEconomicConservationNativeRenewal(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && (args[0] == "restore-request" || args[0] == "restore-cohort-plan") {
		return runEconomicConservationRestore(ctx, args, stdout, stderr, hooks)
	}
	if len(args) == 0 || args[0] != "plan" && args[0] != "apply" {
		fmt.Fprintln(stderr, "usage: economic-conservation-archive native-proposal|plan --request FILE --request-sha256 HASH | apply --plan FILE --plan-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("economic-conservation-archive "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path, hash := "", ""
	if mode == "plan" {
		flags.StringVar(&path, "request", "", "exact offline request")
		flags.StringVar(&hash, "request-sha256", "", "reviewed request digest")
	} else {
		flags.StringVar(&path, "plan", "", "exact offline plan")
		flags.StringVar(&hash, "plan-sha256", "", "reviewed plan digest")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || path == "" || !planSha256(hash) {
		return 2
	}
	raw, digest, err := readPlanFile(ctx, path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "economic archive reviewed input read:", err)
		return 2
	}
	if digest != hash {
		fmt.Fprintln(stderr, "economic archive reviewed input differs from exact digest")
		return 2
	}
	var plan economicConservationArchivePlan
	if mode == "plan" {
		var request economicConservationArchiveRequest
		err = decodeMonitorHistoryInput(raw, &request)
		if err == nil {
			plan, err = planEconomicConservationArchive(ctx, request, hooks)
		}
	} else {
		err = decodeMonitorHistoryInput(raw, &plan)
		if err == nil {
			err = applyEconomicConservationArchive(ctx, plan, hooks)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "economic archive:", err)
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
		fmt.Fprintln(stderr, "economic archive report not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
