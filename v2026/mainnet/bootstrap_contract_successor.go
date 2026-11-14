// Successor proposals adopt retained completion and budget floors without
// approving a new signer, interpreting Safe bytecode or creating execution custody.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
)

const bootstrapContractSuccessorRequestSchema = "urnetwork-mainnet-contract-successor-request-v1"
const bootstrapContractSuccessorProposalSchema = "urnetwork-mainnet-contract-successor-proposal-v1"

// Independently chosen public request fields are unapproved intentions. The
// original phase remains immutable, including any ninth sealed reservation.
type bootstrapContractSuccessorRequest struct {
	Schema                    string         `json:"schema"`
	SuccessorId               string         `json:"successor_id"`
	BootstrapPlanHash         string         `json:"bootstrap_plan_hash"`
	OriginalContractPlanHash  string         `json:"original_contract_plan_hash"`
	AdditionalMaximumAttempts uint8          `json:"additional_maximum_attempts"`
	AdditionalMaximumWei      string         `json:"additional_maximum_wei"`
	IntendedOwnerSafe         common.Address `json:"intended_owner_safe"`
	IntendedSafeNonce         string         `json:"intended_safe_nonce"`
	IntendedRelayer           common.Address `json:"intended_relayer"`
	IntendedRelayerNonce      uint64         `json:"intended_relayer_nonce"`
}

// The coordinator call is derived from the original release and completed
// graph. No Safe digest, signatures or outer transaction encoding is invented.
type bootstrapContractAnchorIntent struct {
	ExpectedOwner common.Address `json:"expected_original_owner"`
	Coordinator   common.Address `json:"coordinator"`
	Evidence      common.Address `json:"validator_evidence"`
	CallData      string         `json:"coordinator_call_data"`
	ValueWei      string         `json:"value_wei"`
}

// Proposed increases are additive to the original ceilings. Retained envelopes
// remain conservative liabilities; receipt gas is not a native debit/refund proof.
type bootstrapContractSuccessorBudget struct {
	OriginalMaximumAttempts           uint8  `json:"original_maximum_attempts"`
	RetainedAttempts                  uint16 `json:"retained_attempts"`
	UnfinishedActionCount             uint8  `json:"unfinished_action_count"`
	AdditionalMaximumAttempts         uint8  `json:"additional_maximum_attempts"`
	ProposedMaximumCumulativeAttempts uint16 `json:"proposed_maximum_cumulative_attempts"`
	ProposedRemainingAttempts         uint16 `json:"proposed_remaining_attempts"`
	RetryMarginAttempts               uint16 `json:"retry_margin_attempts"`
	OriginalMaximumWei                string `json:"original_maximum_wei"`
	CompletedEnvelopeReservationWei   string `json:"completed_envelope_reservation_wei"`
	UnexecutedEnvelopeReservationWei  string `json:"unexecuted_envelope_reservation_wei"`
	AdditionalMaximumWei              string `json:"additional_maximum_wei"`
	ProposedMaximumLifetimeWei        string `json:"proposed_maximum_lifetime_wei"`
}

// A content seal identifies review material only. It is intentionally not a
// signing payload or an approval accepted by any current transaction owner.
type bootstrapContractSuccessorProposal struct {
	Schema                         string                            `json:"schema"`
	Status                         string                            `json:"status"`
	Request                        bootstrapContractSuccessorRequest `json:"request"`
	RequestSha256                  string                            `json:"request_sha256"`
	OriginalConfigHash             string                            `json:"original_config_hash"`
	OriginalCustodyId              string                            `json:"original_custody_id"`
	OriginalRunDirectory           string                            `json:"original_run_directory"`
	LocalPreparation               *bootstrapChainReadinessState     `json:"local_preparation,omitempty"`
	AdoptedActions                 []bootstrapChainContractAction    `json:"adopted_actions"`
	OriginalUnexecutedActions      []evmPhaseAction                  `json:"original_unexecuted_actions"`
	UnfinishedActions              []string                          `json:"unfinished_actions"`
	Anchor                         bootstrapContractAnchorIntent     `json:"evidence_anchor_intent"`
	Budget                         bootstrapContractSuccessorBudget  `json:"proposed_budget"`
	RequiredPrerequisites          []string                          `json:"required_prerequisites"`
	ApprovalVerified               bool                              `json:"approval_verified"`
	ApprovalSigningPayloadProvided bool                              `json:"approval_signing_payload_provided"`
	Executable                     bool                              `json:"executable"`
	CurrentChainVerified           bool                              `json:"current_chain_verified"`
	SafeAuthorityVerified          bool                              `json:"safe_authority_verified"`
	NetworkEffects                 bool                              `json:"network_effects"`
	InstallationComplete           bool                              `json:"installation_complete"`
	ActivationReady                bool                              `json:"activation_ready"`
	ContentHash                    string                            `json:"content_hash"`
}

// Known envelope collisions are rejected before custody is read. The declared
// Safe/relayer identities and nonces still need their independent live admission.
func (self bootstrapContractSuccessorRequest) validate(config evmPhaseConfig) error {
	additional, additionalErr := evmWei(self.AdditionalMaximumWei)
	_, nonceErr := evmWei(self.IntendedSafeNonce)
	if self.Schema != bootstrapContractSuccessorRequestSchema || !planLabel(self.SuccessorId) || self.SuccessorId == config.Plan.CustodyId ||
		!planSha256(self.BootstrapPlanHash) || self.OriginalContractPlanHash != config.Plan.hash() ||
		self.AdditionalMaximumAttempts == 0 || self.AdditionalMaximumAttempts > 16 || additionalErr != nil || additional.Sign() == 0 || nonceErr != nil ||
		self.IntendedOwnerSafe == (common.Address{}) || self.IntendedRelayer == (common.Address{}) || self.IntendedOwnerSafe == self.IntendedRelayer {
		return errors.New("contract successor request lacks exact original scope, separate identities or finite canonical bounds")
	}
	for _, action := range config.Plan.Actions {
		if action.Sender == self.IntendedRelayer && action.Nonce == self.IntendedRelayerNonce {
			return errors.New("contract successor relayer nonce overlaps original consumed or reserved custody")
		}
	}
	original, err := evmWei(config.Plan.MaximumTotalWei)
	if err != nil || new(big.Int).Add(original, additional).BitLen() > 256 {
		return errors.New("contract successor lifetime ceiling overflows its canonical uint256 bound")
	}
	return nil
}

// Inspect genuine original custody; a caller cannot adopt a report by toggling
// its summary flags. All eight successful records must validate under old authority.
func inspectBootstrapContractSuccessor(ctx context.Context, reserve evmCreatePlan, configPath string, request bootstrapContractSuccessorRequest, requestSha256 string) (bootstrapContractSuccessorProposal, error) {
	var proposal bootstrapContractSuccessorProposal
	if !planSha256(requestSha256) {
		return proposal, errors.New("contract successor request lacks an exact input digest")
	}
	if err := request.validate(reserve.Config); err != nil {
		return proposal, err
	}
	readiness, plans, err := prepareBootstrapContractReadiness(ctx, reserve, configPath)
	if err != nil {
		return proposal, err
	}
	if len(plans) != 8 || plans[7].ProxyConstructor == nil || plans[7].ProxyConstructor.Owner != request.IntendedOwnerSafe {
		return proposal, errors.New("contract successor requires the original eight-action graph and exact configured owner identity")
	}
	for _, plan := range plans {
		if request.IntendedRelayer == plan.Address {
			return proposal, errors.New("contract successor relayer aliases an original contract address")
		}
	}
	if err := inspectBootstrapContractCustody(ctx, plans, &readiness); err != nil {
		return proposal, err
	}
	if !readiness.CustodyInspectionComplete || readiness.RetainedCompletedActions != 8 {
		return proposal, errors.New("contract successor requires eight retained successful original receipts; reconcile unfinished work with its original owner")
	}
	completed, unexecuted := new(big.Int), new(big.Int)
	for i, action := range reserve.Config.Plan.Actions {
		tx, err := action.unsigned()
		if err != nil {
			return proposal, err
		}
		liability := new(big.Int).Add(tx.Value(), new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap()))
		if i < 8 {
			completed.Add(completed, liability)
		} else {
			unexecuted.Add(unexecuted, liability)
		}
	}
	original, _ := evmWei(reserve.Config.Plan.MaximumTotalWei)
	additional, _ := evmWei(request.AdditionalMaximumWei)
	cumulativeAttempts := uint16(reserve.Config.Plan.MaximumAttempts) + uint16(request.AdditionalMaximumAttempts)
	remainingAttempts := cumulativeAttempts - readiness.RetainedAttempts
	evidence := plans[7]
	proposal = bootstrapContractSuccessorProposal{Schema: bootstrapContractSuccessorProposalSchema, Status: "unsigned-proposal-prerequisites-unresolved",
		Request: request, RequestSha256: requestSha256, OriginalConfigHash: rootObjectHash(reserve.Config),
		OriginalCustodyId: reserve.Config.Plan.CustodyId, OriginalRunDirectory: reserve.Config.Plan.RunDirectory,
		AdoptedActions: readiness.Actions[:8], OriginalUnexecutedActions: append([]evmPhaseAction{}, reserve.Config.Plan.Actions[8:]...),
		UnfinishedActions: []string{"evidence-anchor"},
		Anchor: bootstrapContractAnchorIntent{ExpectedOwner: request.IntendedOwnerSafe, Coordinator: evidence.Proxy.Address, Evidence: evidence.Address,
			CallData: "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackFixValidatorEvidence(evidence.Address)), ValueWei: "0"},
		Budget: bootstrapContractSuccessorBudget{OriginalMaximumAttempts: reserve.Config.Plan.MaximumAttempts, RetainedAttempts: readiness.RetainedAttempts, UnfinishedActionCount: 1,
			AdditionalMaximumAttempts: request.AdditionalMaximumAttempts, ProposedMaximumCumulativeAttempts: cumulativeAttempts,
			ProposedRemainingAttempts: remainingAttempts, RetryMarginAttempts: remainingAttempts - 1,
			OriginalMaximumWei: original.String(), CompletedEnvelopeReservationWei: completed.String(), UnexecutedEnvelopeReservationWei: unexecuted.String(),
			AdditionalMaximumWei: additional.String(), ProposedMaximumLifetimeWei: new(big.Int).Add(original, additional).String()},
		RequiredPrerequisites: []string{"SIGNED_SUCCESSOR_DOMAIN_AND_ADOPTION_OWNER", "CANONICAL_REAUTHENTICATION_OF_EIGHT_ORIGINAL_RECEIPTS", "REVIEWED_SAFE_SOURCE_ABI_RUNTIME_AND_STORAGE_PROFILE", "CURRENT_SAFE_SINGLETON_OWNERS_THRESHOLD_MODULES_GUARD_AND_FALLBACK", "CURRENT_SAFE_AND_RELAYER_NONCE_AND_GLOBAL_CUSTODY", "EXACT_SAFE_DIGEST_SIGNATURES_AND_OUTER_RELAYER_ENVELOPE", "SAFE_INNER_SUCCESS_AND_EXACT_COORDINATOR_BINDING", "EVIDENCE_IMMUTABLE_DOMAIN_AND_CURRENT_RUNTIME_PROVENANCE", "CUMULATIVE_LIFETIME_CUSTODY_AND_CURRENT_FUNDING"}}
	if len(proposal.OriginalUnexecutedActions) != 0 {
		proposal.RequiredPrerequisites = append(proposal.RequiredPrerequisites, "ORIGINAL_UNEXECUTED_RESERVATION_DISPOSITION")
	}
	return proposal, ctx.Err()
}

// Original v3 custody is held through proposal construction. This command has
// no approval/import/apply mode and publishes no durable successor journal.
func runBootstrapContractSuccessorCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bootstrap-chain contract-successor-plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "original private v3 preparation config")
	runDirectory := flags.String("run-dir", "", "original retained custody directory")
	accepted := flags.String("accept-plan-hash", "", "original accepted v3 preparation hash")
	requestPath := flags.String("request", "", "private unsigned public-identity and incremental-budget request")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configPath == "" || *runDirectory == "" || !planSha256(*accepted) || *requestPath == "" {
		fmt.Fprintln(stderr, "contract-successor-plan needs --config, --run-dir, --accept-plan-hash and --request")
		return 2
	}
	preparation, err := loadBootstrapChainPreparation(ctx, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "contract successor original inputs:", err)
		return 2
	}
	if !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) || *accepted != preparation.Plan.ContentHash || *runDirectory != preparation.Plan.Config.RunDirectory {
		fmt.Fprintln(stderr, "contract successor requires the exact original accepted v3 preparation")
		return 3
	}
	raw, requestHash, err := readBootstrapRootFile(ctx, *requestPath, 16*1024)
	var request bootstrapContractSuccessorRequest
	if err == nil {
		err = decodePlanJson(raw, &request)
	}
	if err == nil {
		err = request.validate(preparation.Contracts.Config)
	}
	if err != nil || request.BootstrapPlanHash != preparation.Plan.ContentHash {
		fmt.Fprintln(stderr, "contract successor unsigned request differs from the exact original scope:", err)
		return 2
	}
	_, plans, err := prepareBootstrapContractReadiness(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path)
	if err == nil {
		err = validateBootstrapContractReadinessPaths(preparation, plans)
	}
	if err != nil {
		fmt.Fprintln(stderr, "contract successor original graph:", err)
		return 2
	}
	for _, path := range preparation.protectedPaths() {
		if *requestPath == path || *requestPath == path+".lock" {
			fmt.Fprintln(stderr, "contract successor request aliases original custody")
			return 2
		}
	}
	for i := range plans {
		path := filepath.Join(*runDirectory, bootstrapContractStateFile(i))
		if *requestPath == path || *requestPath == path+".lock" {
			fmt.Fprintln(stderr, "contract successor request aliases original action custody")
			return 2
		}
	}
	retained, err := openBootstrapChainReadinessState(ctx, preparation)
	if err != nil {
		fmt.Fprintln(stderr, "contract successor original custody unresolved:", err)
		return 1
	}
	proposal, inspectErr := inspectBootstrapContractSuccessor(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path, request, requestHash)
	err = errors.Join(inspectErr, retained.checkpoint(ctx), retained.close())
	if err != nil {
		fmt.Fprintln(stderr, "contract successor original progress unresolved; preserve original journals:", err)
		return 1
	}
	proposal.LocalPreparation = retained
	proposal.ContentHash = rootObjectHash(proposal)
	if err := json.NewEncoder(stdout).Encode(proposal); err != nil {
		fmt.Fprintln(stderr, "contract successor proposal output:", err)
		return 1
	}
	return 3
}
