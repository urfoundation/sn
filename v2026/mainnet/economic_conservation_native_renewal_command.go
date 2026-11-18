// A read-only proposal exposes canonical signing bytes for the original native
// approver. Applying the signed result uses existing archive-before-head order.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"time"
)

type economicConservationNativeRenewalRequest struct {
	Schema       string                     `json:"schema"`
	Policy       economicConservationPolicy `json:"original_policy"`
	Original     monitorHistoryReference    `json:"original_checkpoint"`
	Renewals     []planFileReference        `json:"next_native_reviews"`
	ReviewSha256 string                     `json:"adoption_review_sha256"`
}

type economicConservationNativeRenewalProposal struct {
	Schema            string                            `json:"schema"`
	Adoption          economicConservationNativeRenewal `json:"unsigned_adoption"`
	SigningBytes      string                            `json:"signing_bytes_hex"`
	RetainedStateHash string                            `json:"retained_state_hash"`
	RestartAuthorized bool                              `json:"restart_authorized"`
	ApplyCommand      string                            `json:"apply_command"`
}

func proposeEconomicConservationNativeRenewal(ctx context.Context, request economicConservationNativeRenewalRequest, hooks monitorServiceHooks) (_ economicConservationNativeRenewalProposal, resultErr error) {
	var proposal economicConservationNativeRenewalProposal
	if request.Schema != "urnetwork-economic-native-approval-request-v1" || !planSha256(request.ReviewSha256) {
		return proposal, errors.New("economic native proposal requires exact original approval input")
	}
	if err := errors.Join(ctx.Err(), request.Policy.validate(), request.Policy.validateReference(request.Original)); err != nil {
		return proposal, err
	}
	if request.Policy.Native.Observation.Execution.Producer == nil {
		return proposal, errors.New("economic native proposal cannot enroll a producer key")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(monitorEconomicReadSeconds(request.Policy.ReadBudgetSeconds))*time.Second)
	defer cancel()
	source, raw, err := request.Policy.openHistoryReader(ctx, request.Original)
	if err != nil {
		return proposal, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	state, err := decodeEconomicConservation(ctx, raw, request.Policy)
	if err != nil {
		return proposal, err
	}
	if state.NativeRenewal != nil && state.NativeRenewal.Original.Path != request.Original.Path {
		return proposal, errors.New("economic native proposal moved an original adoption checkpoint")
	}
	view, err := openEconomicConservationArchive(ctx, request.Policy, state, hooks)
	if err != nil {
		return proposal, err
	}
	defer func() { resultErr = errors.Join(resultErr, view.close()) }()
	state.archiveView = view
	if err := state.validateClaimCheckpointPath(request.Original.Path); err != nil {
		return proposal, err
	}
	if view.nativeReviews[request.ReviewSha256] || state.NativeRenewal != nil && state.NativeRenewal.ReviewSha256 == request.ReviewSha256 {
		return proposal, errors.New("economic native proposal reused an original independent review")
	}
	head, err := state.nativeApprovalHead(request.Policy)
	if err != nil {
		return proposal, err
	}
	if head.Ordinal == ^uint64(0) {
		return proposal, errors.New("economic native approval ordinal is exhausted")
	}
	adoption := economicConservationNativeRenewal{Schema: economicConservationNativeRenewalSchema, PolicyHash: request.Policy.identityHash(), Original: request.Original, Ordinal: head.Ordinal + 1, Previous: head.Hash, ReviewSha256: request.ReviewSha256, From: slices.Clone(head.Renewals), To: slices.Clone(request.Renewals)}
	message, err := adoption.signingBytes()
	if err != nil {
		return proposal, err
	}
	operating, err := state.nativeOperatingPolicy(request.Policy)
	if err != nil {
		return proposal, err
	}
	if operating.Observation.Execution == nil || operating.Observation.Execution.Producer == nil {
		return proposal, errors.New("economic native proposal cannot enroll a producer key")
	}
	// nativeOperatingPolicy returned private clones; no caller config changes.
	operating.Observation.Execution.Producer.Renewals = slices.Clone(adoption.To)
	if _, err := loadNativeProducerAuthorities(ctx, operating.Observation); err != nil {
		return proposal, err
	}
	proposal = economicConservationNativeRenewalProposal{Schema: "urnetwork-economic-native-approval-proposal-v1", Adoption: adoption, SigningBytes: hex.EncodeToString(message), RetainedStateHash: state.ContentHash, ApplyCommand: "economic-conservation-archive plan/apply with signed native_approval_adoption"}
	return proposal, errors.Join(ctx.Err(), source.check(), view.check())
}

func runEconomicConservationNativeRenewal(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	flags := flag.NewFlagSet("economic-conservation-archive native-proposal", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "exact independent native approval request")
	pin := flags.String("request-sha256", "", "sha256:DIGEST of request bytes")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" || !planSha256(*pin) {
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "economic native proposal input read:", err)
		return 2
	}
	if digest != *pin {
		fmt.Fprintln(stderr, "economic native proposal input differs from exact pin")
		return 2
	}
	var request economicConservationNativeRenewalRequest
	if err := decodeMonitorHistoryInput(raw, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	proposal, err := proposeEconomicConservationNativeRenewal(ctx, request, hooks)
	if err != nil {
		fmt.Fprintln(stderr, "economic native proposal:", err)
		return 2
	}
	raw, err = json.Marshal(proposal)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw = append(raw, '\n')
	written, err := stdout.Write(raw)
	if err != nil || written != len(raw) {
		fmt.Fprintln(stderr, "economic native proposal not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
