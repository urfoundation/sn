// An offline proposal exposes canonical independent signing bytes. The signed
// result belongs in the existing economic-conservation-archive plan/apply
// request, whose two durable publications retain original-before-next order.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

type economicConservationClaimWindowRequest struct {
	Schema       string                     `json:"schema"`
	Policy       economicConservationPolicy `json:"original_policy"`
	Original     monitorHistoryReference    `json:"original"`
	Next         monitorClaimPolicy         `json:"next_claim_policy"`
	ReviewSha256 string                     `json:"review_sha256"`
}

type economicConservationClaimWindowProposal struct {
	Schema            string                          `json:"schema"`
	Window            economicConservationClaimWindow `json:"unsigned_window"`
	SigningBytes      string                          `json:"signing_bytes_hex"`
	RetainedStateHash string                          `json:"retained_claim_state_hash"`
	RestartAuthorized bool                            `json:"restart_authorized"`
	ApplyCommand      string                          `json:"apply_command"`
}

func proposeEconomicConservationClaimWindow(ctx context.Context, request economicConservationClaimWindowRequest, hooks monitorServiceHooks) (_ economicConservationClaimWindowProposal, resultErr error) {
	var proposal economicConservationClaimWindowProposal
	if request.Schema != "urnetwork-economic-claim-window-request-v1" || !planSha256(request.ReviewSha256) || request.Next.Renewal != nil || request.Next.Window != nil {
		return proposal, errors.New("economic Claim proposal requires explicit independent next expectations")
	}
	if err := errors.Join(request.Policy.validate(), request.Policy.validateReference(request.Original)); err != nil {
		return proposal, err
	}
	_, claim, exists := economicConservationClaimRole(request.Policy, request.Next.Role)
	if !exists || claim.HistoryCatalog == nil || !economicConservationClaimPolicyIncludes(claim, request.Next) {
		return proposal, errors.New("economic Claim proposal cannot enroll or replace original authority")
	}
	network := request.Policy.Native.Observation.Network
	if err := request.Next.validate(identityExpectation{NativeChain: network.NativeChain, GenesisHash: network.GenesisHash, EvmChainId: network.EvmChainId}); err != nil {
		return proposal, err
	}
	source, raw, err := request.Policy.openHistoryReader(ctx, request.Original)
	if err != nil {
		return proposal, err
	}
	defer func() { resultErr = errors.Join(resultErr, source.close()) }()
	state, err := decodeEconomicConservation(ctx, raw, request.Policy)
	if err != nil {
		return proposal, err
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
	if view.claimReviews[claim.Role+"/"+request.ReviewSha256] {
		return proposal, errors.New("economic Claim proposal reused an archived independent review")
	}
	for _, window := range state.ClaimWindows {
		if window.Role == claim.Role && window.ReviewSha256 == request.ReviewSha256 {
			return proposal, errors.New("economic Claim proposal reused its current independent review")
		}
	}
	if err := view.setClaimBasis(request.Policy, state, request.Original); err != nil {
		return proposal, err
	}
	window := economicConservationClaimWindow{Schema: economicConservationClaimWindowSchema, PolicyHash: request.Policy.identityHash(), Role: claim.Role, Original: request.Original, Next: request.Next, ReviewSha256: request.ReviewSha256}
	window, retained, err := deriveEconomicConservationClaimWindow(request.Policy, view.claimBasis, window)
	if err != nil {
		return proposal, err
	}
	message, err := window.signingBytes()
	if err != nil {
		return proposal, err
	}
	proposal = economicConservationClaimWindowProposal{Schema: "urnetwork-economic-claim-window-proposal-v1", Window: window, SigningBytes: hex.EncodeToString(message), RetainedStateHash: rootObjectHash(retained), ApplyCommand: "economic-conservation-archive plan/apply with signed claim_windows"}
	return proposal, errors.Join(ctx.Err(), source.check(), view.check())
}

func runEconomicConservationClaimWindow(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	flags := flag.NewFlagSet("economic-conservation-claim-window", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "exact independent next-window request")
	pin := flags.String("request-sha256", "", "sha256:DIGEST of request bytes")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" || !planSha256(*pin) {
		fmt.Fprintln(stderr, "economic-conservation-claim-window requires --request FILE --request-sha256 sha256:DIGEST")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	if digest != *pin {
		fmt.Fprintln(stderr, "economic Claim window request differs from exact input pin")
		return 2
	}
	var request economicConservationClaimWindowRequest
	if err := decodeMonitorHistoryInput(raw, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	proposal, err := proposeEconomicConservationClaimWindow(ctx, request, hooks)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	raw, err = json.Marshal(proposal)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	raw = append(raw, '\n')
	written, err := stdout.Write(raw)
	if err != nil || written != len(raw) {
		fmt.Fprintln(stderr, errors.Join(io.ErrShortWrite, err))
		return 3
	}
	return 0
}
