// A readiness observation links accepted local preparation to current finalized
// role prerequisites. It supplies no signer, live authority or service port.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const bootstrapChainReadinessSchema = "urnetwork-mainnet-bootstrap-chain-readiness-v1"

// Observed prerequisites are a limited projection of a complete census. Live
// admission still needs every separately listed activation prerequisite.
type bootstrapChainRoleReadiness struct {
	Role                   string                    `json:"role"`
	ValidatorId            uint64                    `json:"validator_id,omitempty"`
	Expected               subnetIdentityExpectation `json:"expected_registration"`
	Observed               *subnetRegistration       `json:"observed_registration,omitempty"`
	ApprovedFromBlock      uint64                    `json:"approved_from_block"`
	ApprovedThroughBlock   uint64                    `json:"approved_through_block"`
	ApprovedCheckpointHash string                    `json:"approved_checkpoint_hash,omitempty"`
	ObservedCheckpointHash string                    `json:"observed_checkpoint_hash,omitempty"`
	Active                 *bool                     `json:"active,omitempty"`
	ValidatorPermit        *bool                     `json:"validator_permit,omitempty"`
	ObservationBlockers    []string                  `json:"observation_blockers"`
	ActivationBlockers     []string                  `json:"activation_blockers"`
}

// An unresolved result deliberately contains no partially successful census or
// role observation. Its accepted plan and retained child seals remain explicit.
type bootstrapChainReadiness struct {
	Schema                   string                        `json:"schema"`
	PlanHash                 string                        `json:"plan_hash"`
	Status                   string                        `json:"status"`
	ObservationComplete      bool                          `json:"observation_complete"`
	LocalPreparation         *bootstrapChainReadinessState `json:"local_preparation,omitempty"`
	Census                   *subnetPreviewEnvelope        `json:"census,omitempty"`
	PassiveRoot              *rootPreviewEnvelope          `json:"passive_root,omitempty"`
	UrValidators             []bootstrapChainRoleReadiness `json:"ur_validators"`
	RootValidator            bootstrapChainRoleReadiness   `json:"root_validator"`
	Blockers                 []string                      `json:"observation_blockers"`
	PendingChainPhases       []string                      `json:"pending_chain_phases"`
	CurrentAuthorityVerified bool                          `json:"current_authority_verified"`
	NativeSigning            bool                          `json:"native_signing"`
	NetworkEffects           bool                          `json:"network_effects"`
	ActivationReady          bool                          `json:"activation_ready"`
	ContentHash              string                        `json:"content_hash"`
}

// Both role observations begin unresolved even when all offline approvals pass.
func newBootstrapChainReadiness(preparation bootstrapChainPreparation) bootstrapChainReadiness {
	result := bootstrapChainReadiness{Schema: bootstrapChainReadinessSchema, PlanHash: preparation.Plan.ContentHash,
		Status: "unresolved", Blockers: []string{"CURRENT_FINALIZED_OBSERVATION_UNRESOLVED"}, PendingChainPhases: bootstrapChainPendingPhases()}
	for i, role := range preparation.Plan.Config.Validators {
		approval := preparation.Plan.ValidatorInspections[i].Approval
		result.UrValidators = append(result.UrValidators, bootstrapChainRoleReadiness{Role: role.Role, ValidatorId: role.ValidatorId,
			Expected: role.subnetIdentityExpectation, ApprovedFromBlock: approval.ValidFromNativeBlock, ApprovedThroughBlock: approval.ValidThroughNativeBlock,
			ObservationBlockers: []string{"CURRENT_FINALIZED_OBSERVATION_UNRESOLVED"},
			ActivationBlockers:  []string{"NATIVE_EPOCH_APPROVAL_WINDOW_UNVERIFIED", "SIGNED_ACTIVATION_CHECKPOINT_UNVERIFIED", "PRODUCTION_ADMISSION_AND_OPERATOR_HEALTH_UNVERIFIED", "DEPLOYED_CONTRACTS_UNVERIFIED", "SIGNING_DEVICE_AND_GLOBAL_CUSTODY_FENCE_UNVERIFIED"}})
		if role.Role == "majority" {
			result.UrValidators[i].ActivationBlockers = append(result.UrValidators[i].ActivationBlockers, "EFFECTIVE_STAKE_MAJORITY_UNVERIFIED")
		}
	}
	action := preparation.Root.Service.Packet.Action
	role := preparation.Plan.Config.RootValidator
	result.RootValidator = bootstrapChainRoleReadiness{Role: role.Role,
		Expected:          subnetIdentityExpectation{Hotkey: role.Hotkey, Coldkey: role.Coldkey, RegistrationBlock: &action.Scope.Seat.RegistrationBlock},
		ApprovedFromBlock: action.BirthBlock, ApprovedThroughBlock: action.BirthBlock + action.Period - 1,
		ApprovedCheckpointHash: action.BirthHash,
		ObservationBlockers:    []string{"CURRENT_FINALIZED_OBSERVATION_UNRESOLVED"},
		ActivationBlockers:     []string{"ROOT_EFFECTIVE_STAKE_AND_ELIGIBILITY_UNVERIFIED", "ROOT_NONCE_AND_WEIGHT_PREREQUISITES_UNVERIFIED", "ROOT_CURRENT_AUTHORITY_AND_GLOBAL_CUSTODY_FENCE_UNVERIFIED", "ROOT_SERVICE_ACTIVATION_PENDING"}}
	if preparation.Root.PassiveService != nil {
		policy := preparation.Root.PassiveService.Policy
		result.RootValidator.Expected.RegistrationBlock = &policy.ExpectedSeat.RegistrationBlock
		result.RootValidator.ApprovedFromBlock, result.RootValidator.ApprovedThroughBlock = policy.ValidFromBlock, policy.ValidThroughBlock
		result.RootValidator.ApprovedCheckpointHash = ""
		result.RootValidator.ActivationBlockers = []string{"PASSIVE_ROOT_OBSERVER_INSTALLATION_AND_MONITORING_UNVERIFIED", "PASSIVE_ROOT_OBSERVATION_HAS_NO_TRANSACTION_AUTHORITY"}
	}
	return result
}

// Input approval failures precede output; observation failures emit a sealed
// unresolved result. The route is observation-only and never approves submission.
func runBootstrapChainReadiness(ctx context.Context, preparation bootstrapChainPreparation, rpcUrl string, retryWindow time.Duration, stdout, stderr io.Writer) int {
	if !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) {
		fmt.Fprintln(stderr, "bootstrap readiness requires accepted v3/v4 scope; v1/v2 remain resumable at their original scope")
		return 3
	}
	client, err := newRpcClient(rpcUrl, retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer client.httpClient.CloseIdleConnections()
	result, err := client.observeBootstrapChainReadiness(ctx, preparation)
	result.ContentHash = rootObjectHash(result)
	if outputErr := json.NewEncoder(stdout).Encode(result); outputErr != nil {
		fmt.Fprintln(stderr, "bootstrap readiness output:", outputErr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap readiness unresolved:", err)
		if errors.Is(err, errRpcIntegrity) {
			return 3
		}
		return 1
	}
	if result.Status != "observed-prerequisites" {
		return 3
	}
	return 0
}

// One deadline and one finalized census cover both subnet roles and root. No
// historical trim census substitutes for current registration or approval time.
func (self *rpcClient) observeBootstrapChainReadiness(ctx context.Context, preparation bootstrapChainPreparation) (result bootstrapChainReadiness, resultErr error) {
	if err := preparation.validate(); err != nil || !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) {
		return result, errors.Join(errors.New("bootstrap readiness requires complete accepted v3/v4 preparation"), err)
	}
	result = newBootstrapChainReadiness(preparation)
	if ctx == nil {
		return result, errors.New("bootstrap readiness context is absent")
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	retained, err := openBootstrapChainReadinessState(sampleCtx, preparation)
	if err != nil {
		result.Blockers = []string{"ORIGINAL_PREPARED_CUSTODY_UNRESOLVED"}
		return result, err
	}
	result.LocalPreparation = retained
	defer func() {
		resultErr = errors.Join(resultErr, retained.checkpoint(sampleCtx))
		if err := retained.close(); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
		if resultErr != nil {
			local, blockers := result.LocalPreparation, result.Blockers
			if len(blockers) == 0 {
				blockers = []string{"CURRENT_FINALIZED_OBSERVATION_UNRESOLVED"}
			}
			result = newBootstrapChainReadiness(preparation)
			result.LocalPreparation, result.Blockers = local, blockers
		}
	}()
	policyRaw, err := readBootstrapChainInput(sampleCtx, preparation.Plan.Config.OwnerTrimPolicy, maxRpcReplyBytes)
	if err != nil {
		return result, err
	}
	var policy subnetCensusPolicy
	if err := decodePlanJson(policyRaw, &policy); err != nil {
		return result, err
	}
	preview, err := self.readSubnetPreview(sampleCtx, policy, preparation.Plan.Config.OwnerTrimPolicy.Sha256)
	if err != nil {
		return result, err
	}
	// The old root action's era checkpoint remains independently approved. A
	// matching generation on a different branch cannot replace that checkpoint.
	action := preparation.Root.Service.Packet.Action
	anchorMatches := false
	var anchor string
	if preparation.Root.PassiveService != nil {
		policy := preparation.Root.PassiveService.Policy
		passive, err := self.readRootPreviewAt(sampleCtx, policy, rootObjectHash(policy), preview.Identity.FinalizedHash)
		if err != nil {
			return result, err
		}
		envelope, err := sealRootPreview(passive)
		if err != nil {
			return result, err
		}
		result.PassiveRoot, anchorMatches = &envelope, true
	} else if action.BirthBlock <= preview.Identity.FinalizedNumber {
		if err := self.call(sampleCtx, "chain_getBlockHash", []any{action.BirthBlock}, &anchor); err != nil {
			return result, err
		}
		if !rootCanonicalHash(strings.ToLower(anchor)) {
			return result, fmt.Errorf("%w: root approved checkpoint is unavailable", errRpcIntegrity)
		}
		anchorMatches = strings.EqualFold(anchor, action.BirthHash)
	}
	// Recheck canonical membership after every additional prerequisite read.
	var confirmed string
	if err := self.call(sampleCtx, "chain_getBlockHash", []any{preview.Identity.FinalizedNumber}, &confirmed); err != nil {
		return result, err
	}
	if !strings.EqualFold(confirmed, preview.Identity.FinalizedHash) {
		return result, fmt.Errorf("%w: finalized block changed during bootstrap readiness", errRpcIntegrity)
	}
	if err := self.closeSnapshotFinality(sampleCtx, preview.Identity); err != nil {
		return result, err
	}
	if err := retained.checkpoint(sampleCtx); err != nil {
		return result, err
	}
	envelope, err := sealSubnetPreview(preview)
	if err != nil {
		return result, err
	}
	result.Census, result.ObservationComplete = &envelope, true
	result.Blockers = []string{}
	if preview.SubnetOwnerColdkey != policy.SubnetOwnerColdkey {
		result.Blockers = append(result.Blockers, "SUBNET_OWNER_DIFFERS_FROM_APPROVED_POLICY")
	}
	if preview.SubnetRegistrationBlock != *policy.SubnetRegistrationBlock || preview.SubnetGeneration != *policy.SubnetGeneration {
		result.Blockers = append(result.Blockers, "SUBNET_GENERATION_DIFFERS_FROM_APPROVED_POLICY")
	}
	for i := range result.UrValidators {
		role := &result.UrValidators[i]
		role.ObservationBlockers = slices.Clone(result.Blockers)
		for _, seat := range preview.Seats {
			if seat.Hotkey == role.Expected.Hotkey {
				registration, active, permit := seat.subnetRegistration, seat.Active, seat.ValidatorPermit
				role.Observed, role.Active, role.ValidatorPermit = &registration, &active, &permit
				break
			}
		}
		if role.Observed == nil {
			role.ObservationBlockers = append(role.ObservationBlockers, "APPROVED_REGISTRATION_ABSENT")
		} else {
			if role.Observed.Coldkey != role.Expected.Coldkey || role.Observed.RegistrationBlock != *role.Expected.RegistrationBlock {
				role.ObservationBlockers = append(role.ObservationBlockers, "APPROVED_REGISTRATION_GENERATION_DIFFERS")
			}
			if !*role.Active {
				role.ObservationBlockers = append(role.ObservationBlockers, "VALIDATOR_INACTIVE")
			}
			if !*role.ValidatorPermit {
				role.ObservationBlockers = append(role.ObservationBlockers, "VALIDATOR_PERMIT_ABSENT")
			}
		}
		if preview.Identity.FinalizedNumber < role.ApprovedFromBlock || preview.Identity.FinalizedNumber > role.ApprovedThroughBlock {
			role.ObservationBlockers = append(role.ObservationBlockers, "SIGNED_NATIVE_BLOCK_WINDOW_CLOSED")
		}
		if len(preview.Seats) > int(preparation.Plan.ValidatorInspections[i].Approval.MaximumSubnetUids) {
			role.ObservationBlockers = append(role.ObservationBlockers, "SIGNED_SUBNET_CENSUS_LIMIT_EXCEEDED")
		}
	}
	root := &result.RootValidator
	root.ObservedCheckpointHash = strings.ToLower(anchor)
	root.ObservationBlockers = []string{}
	for _, registration := range preview.RootRegistrations {
		if registration.Hotkey == root.Expected.Hotkey {
			root.Observed = &registration
			break
		}
	}
	if root.Observed == nil {
		root.ObservationBlockers = append(root.ObservationBlockers, "APPROVED_ROOT_REGISTRATION_ABSENT")
	} else if root.Observed.Coldkey != root.Expected.Coldkey || root.Observed.RegistrationBlock != *root.Expected.RegistrationBlock || root.Observed.Uid != preparation.Plan.Config.RootValidator.Seat.Uid {
		root.ObservationBlockers = append(root.ObservationBlockers, "APPROVED_ROOT_SEAT_GENERATION_DIFFERS")
	}
	if preview.Identity.FinalizedNumber < root.ApprovedFromBlock || preview.Identity.FinalizedNumber > root.ApprovedThroughBlock {
		root.ObservationBlockers = append(root.ObservationBlockers, "SIGNED_ROOT_MORTAL_WINDOW_CLOSED")
	}
	if !anchorMatches {
		root.ObservationBlockers = append(root.ObservationBlockers, "SIGNED_ROOT_CHECKPOINT_UNCONFIRMED")
	}
	if result.PassiveRoot != nil {
		root.ObservationBlockers = append(root.ObservationBlockers, result.PassiveRoot.Observation.Blockers...)
	}
	result.Status = "observed-prerequisites"
	if len(result.Blockers) != 0 || len(root.ObservationBlockers) != 0 || slices.ContainsFunc(result.UrValidators, func(role bootstrapChainRoleReadiness) bool { return len(role.ObservationBlockers) != 0 }) {
		result.Status = "blocked"
	}
	return result, nil
}
