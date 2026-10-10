// The public adapter reads the actual native replay, complete EVM receipt/getter
// reader and Claim HTTP consumer. It owns one durable checkpoint. It never
// imports a caller-authored amount report or authorizes a transaction.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

const economicConservationRole = "economic-conservation"

type economicConservationSummary struct {
	Treasury                       *economicConservationTreasurySummary        `json:"original_treasury_custody,omitempty"`
	Progress                       *economicConservationProgressSummary        `json:"operational_progress,omitempty"`
	OriginalFees                   *economicWholeFeeSummary                    `json:"original_complete_fee_census,omitempty"`
	OriginalFinality               *economicConservationFinalitySummary        `json:"original_consensus_coverage,omitempty"`
	Funding                        *economicConservationFundingSummary         `json:"original_funding_composition,omitempty"`
	Conformance                    *economicConservationConformance            `json:"observed_economic_conformance,omitempty"`
	OriginalEntitlements           *economicConservationEntitlementSummary     `json:"original_entitlement_census,omitempty"`
	Yuma                           *economicConservationYumaSummary            `json:"complete_native_allocation,omitempty"`
	PrincipalEffects               *economicConservationPrincipalEffectSummary `json:"original_principal_execution,omitempty"`
	OpeningPrincipals              *economicConservationPrincipalSummary       `json:"original_opening_principal,omitempty"`
	NativeFeeIssue                 string                                      `json:"native_fee_issue,omitempty"`
	NativeFeeHeldRequest           string                                      `json:"native_fee_held_request,omitempty"`
	NativeFeeHeldPolicy            string                                      `json:"native_fee_held_policy,omitempty"`
	NativeFeePending               bool                                        `json:"native_fee_pending,omitempty"`
	AdmittedNativeFees             *economicConservationFeeSummary             `json:"admitted_native_fee_census,omitempty"`
	Schema                         string                                      `json:"schema"`
	PolicyHash                     string                                      `json:"policy_hash"`
	CheckpointHash                 string                                      `json:"checkpoint_hash"`
	SampleAt                       time.Time                                   `json:"sample_at"`
	NativeCursor                   economicEmissionBoundary                    `json:"native_cursor"`
	VaultCursor                    economicEmissionBoundary                    `json:"vault_cursor"`
	NativeCurrent                  bool                                        `json:"native_current"`
	VaultCurrent                   bool                                        `json:"vault_current"`
	ClaimStatuses                  []string                                    `json:"claim_statuses"`
	NativePending                  bool                                        `json:"native_pending"`
	NativeIssue                    string                                      `json:"native_issue,omitempty"`
	VaultIssue                     string                                      `json:"vault_issue,omitempty"`
	JoinIssue                      string                                      `json:"join_issue,omitempty"`
	NativeHeld                     bool                                        `json:"native_integrity_held"`
	VaultHeld                      bool                                        `json:"vault_integrity_held"`
	Execution                      *nativeExecutionWindow                      `json:"native_execution"`
	DirectGrossAlpha               *string                                     `json:"direct_provider_gross_alpha"`
	TailGrossAlpha                 *string                                     `json:"tail_provider_gross_alpha"`
	UnroutedGrossAlpha             *string                                     `json:"unrouted_provider_gross_alpha"`
	RewardCollateralAlpha          *string                                     `json:"provider_reward_collateral_alpha"`
	VaultState                     *monitorEconomicEvmSnapshot                 `json:"observed_vault_state"`
	EarningOccurrences             int                                         `json:"earning_occurrences"`
	Captures                       int                                         `json:"captures"`
	CausallyJoinedCaptures         uint64                                      `json:"original_native_receipt_captures"`
	MatchedCaptures                int                                         `json:"mapped_captures"`
	AcceptedClaims                 int                                         `json:"accepted_claims"`
	AggregatePayments              int                                         `json:"aggregate_payments"`
	MatchedReceipts                int                                         `json:"matched_original_receipts"`
	FactsRemaining                 uint64                                      `json:"facts_remaining"`
	CapacityWarning                bool                                        `json:"capacity_warning"`
	ArchiveSegments                uint64                                      `json:"archive_segments"`
	ArchiveIndexEntries            uint64                                      `json:"archive_index_entries"`
	ArchiveIndexBytes              uint64                                      `json:"archive_index_bytes"`
	Resources                      economicConservationResources               `json:"resources"`
	OpeningPrincipalAlpha          *string                                     `json:"opening_principal_alpha"`
	NativeFeeWithdrawalRao         *string                                     `json:"native_fee_withdrawal_rao"`
	NativeFeeRefundRao             *string                                     `json:"native_fee_refund_rao"`
	FullQuantizationToleranceAlpha *string                                     `json:"full_quantization_tolerance_alpha"`
	TargetMet                      *bool                                       `json:"target_met"`
	ActualNativeOutcomeVerified    bool                                        `json:"actual_native_outcome_verified"`
	ActivationReady                bool                                        `json:"activation_ready"`
	Authority                      string                                      `json:"authority"`
	MissingEvidence                []string                                    `json:"missing_evidence"`
}

func (self *economicConservationState) summary(ctx context.Context, policy economicConservationPolicy, nativeCurrent, vaultCurrent bool) (economicConservationSummary, error) {
	var err error
	policy, err = self.operatingPolicy(policy)
	if err != nil {
		return economicConservationSummary{}, err
	}
	resources, err := self.resources(policy)
	if err != nil {
		return economicConservationSummary{}, err
	}
	result := economicConservationSummary{Schema: "urnetwork-economic-conservation-sample-v1", PolicyHash: self.PolicyHash, CheckpointHash: self.ContentHash, SampleAt: self.SampleAt, NativeCursor: self.Native.Cursor, VaultCursor: self.Vault.Cursor, NativeCurrent: nativeCurrent, VaultCurrent: vaultCurrent, NativeIssue: self.NativeIssue, NativePending: self.NativePending, VaultIssue: self.VaultIssue, JoinIssue: self.JoinIssue, NativeHeld: self.NativeHeld, VaultHeld: self.VaultHeld, Execution: self.Native.ExecutionAccounting, VaultState: self.Vault.Snapshot, EarningOccurrences: len(self.Lots), Captures: len(self.Captures), AcceptedClaims: len(self.Claims), AggregatePayments: len(self.Payments), FactsRemaining: policy.MaximumFacts - self.facts(), Resources: resources, Authority: "admitted-native-replay-and-owned-rpc-vault-claim-observations", MissingEvidence: []string{"independently-admitted-runtime-build-and-full-quantization", "opening-pool-principal-and-complete-stake-effects", "native-fee-withdrawal-refund-and-precompile-rollback", "independent-vault-finality-and-complete-entitlement-census"}}
	if self.Archive != nil {
		result.ArchiveSegments = uint64(len(self.Archive.Segments))
	}
	result.OriginalEntitlements, err = self.entitlementCensusSummary(ctx, policy)
	if err != nil {
		return result, err
	}
	result.NativeFeeIssue, result.NativeFeeHeldRequest, result.NativeFeePending = self.NativeFeeIssue, self.NativeFeeHeldRequest, self.NativeFeePending
	result.NativeFeeHeldPolicy = self.NativeFeeHeldPolicy
	result.Yuma, err = self.yumaSummary(ctx, policy)
	if err != nil {
		return result, err
	}
	if result.Yuma != nil && result.Yuma.Current {
		result.FullQuantizationToleranceAlpha = result.Yuma.FullQuantizationTolerance
	}
	if err := self.Native.admitRuntime(ctx, policy.Native); err != nil {
		return result, err
	}
	result.PrincipalEffects, err = self.principalEffectsSummary(policy)
	if err != nil {
		return result, err
	}
	result.Treasury, err = self.treasurySummary(policy)
	if err != nil {
		return result, err
	}
	if result.Treasury != nil {
		result.Schema = "urnetwork-economic-conservation-sample-v2"
	}
	result.OpeningPrincipals, err = self.principalSummary(policy)
	if err != nil {
		return result, err
	}
	if result.OpeningPrincipals != nil {
		result.OpeningPrincipalAlpha = result.OpeningPrincipals.OpeningAlpha
	}
	result.AdmittedNativeFees, err = self.feeSummary(policy)
	if err != nil {
		return result, err
	}
	result.OriginalFees, err = self.wholeFeeSummary(ctx, policy)
	if err != nil {
		return economicConservationSummary{}, err
	}
	if self.archiveView != nil {
		result.ArchiveIndexEntries = self.archiveView.entries + self.archiveView.claimBasisEntries
		result.ArchiveIndexBytes = self.archiveView.bytes + self.archiveView.claimBasisBytes + self.archiveView.principalReservedBytes
	}
	for _, state := range self.ClaimStates {
		result.ClaimStatuses = append(result.ClaimStatuses, state.Status)
	}
	for _, capture := range self.Captures {
		if capture.causalComplete() {
			result.CausallyJoinedCaptures++
		}
		if capture.Native != nil {
			result.MatchedCaptures++
		}
	}
	for _, receipt := range self.Receipts {
		if receipt.ClaimId != "" {
			result.MatchedReceipts++
		}
	}
	if self.Native.ExecutionAccounting != nil {
		direct, tail, unrouted, collateral := "0", "0", "0", "0"
		if self.Archive != nil {
			direct, tail, unrouted, collateral = self.Archive.Amounts.Direct, self.Archive.Amounts.Tail, self.Archive.Amounts.Unrouted, self.Archive.Amounts.Collateral
			result.EarningOccurrences += int(self.Archive.Counts.Lots)
			result.CausallyJoinedCaptures += self.Archive.Counts.CausalCaptures
			result.Captures += int(self.Archive.Counts.Captures)
			result.MatchedCaptures += int(self.Archive.Counts.Captures)
			result.AcceptedClaims += int(self.Archive.Counts.Claims)
			result.AggregatePayments += int(self.Archive.Counts.Payments)
			result.MatchedReceipts += int(self.Archive.Counts.Receipts)
		}
		for _, lot := range self.Lots {
			if !lot.Effect.Provider {
				continue
			}
			target := &direct
			if lot.Route == nil {
				target = &unrouted
			} else if lot.Route.Kind == "tail-pool" {
				target = &tail
			}
			var err error
			*target, err = economicConservationSum(*target, lot.Effect.Gross)
			if err != nil {
				return result, err
			}
			collateral, err = economicConservationSum(collateral, lot.Effect.Collateral)
			if err != nil {
				return result, err
			}
		}
		result.DirectGrossAlpha, result.TailGrossAlpha, result.RewardCollateralAlpha = &direct, &tail, &collateral
		result.UnroutedGrossAlpha = &unrouted
	}
	result.OriginalFinality, err = self.finalitySummary(ctx, policy)
	if err != nil {
		return economicConservationSummary{}, err
	}
	if result.OriginalEntitlements != nil {
		result.OriginalEntitlements.IndependentFinalityAuthenticated = result.OriginalFinality != nil && result.OriginalFinality.Complete
	}
	result.Funding, err = self.fundingSummary(ctx)
	if err != nil {
		return result, err
	}
	if err := result.assessConformance(); err != nil {
		return result, err
	}
	raw, err := json.Marshal(self)
	if err != nil {
		return result, err
	}
	// Warning is explicit before the bounded active owner fills. No warning
	// grants a larger capacity or discards old/unmatched obligations.
	result.CapacityWarning = self.facts()*2 >= policy.MaximumFacts || uint64(len(raw))*2 >= resources.headBytes() || self.Native.CapacityRemaining*2 <= policy.Native.HistoryEntries || self.Vault.CapacityRemaining*2 <= policy.Vault.HistoryEntries || 2*(result.ArchiveSegments+1) >= resources.ArchiveSegments || 4*result.ArchiveIndexEntries >= resources.IndexEntries || 4*result.ArchiveIndexBytes >= resources.IndexBytes
	return result, nil
}

func cloneEconomicConservation(value *economicConservationState, policies ...economicConservationPolicy) (*economicConservationState, error) {
	maximum := uint64(maxRpcReplyBytes)
	if len(policies) > 1 {
		return nil, errors.New("economic clone requires one original policy")
	}
	if len(policies) == 1 {
		if err := policies[0].StorageProfile.validate(); err != nil {
			return nil, err
		}
		maximum = policies[0].storageMaximum()
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)+1) > maximum {
		return nil, errMonitorEconomicCapacity
	}
	var result economicConservationState
	if err := decodePlanJson(raw, &result); err != nil {
		return nil, err
	}
	result.archiveView = value.archiveView
	result.principalProvisional = value.principalProvisional
	result.Native.runtimeAdmission = value.Native.runtimeAdmission
	return &result, nil
}

func loadEconomicConservation(ctx context.Context, owner *monitorCheckpointStore, policy economicConservationPolicy) (*economicConservationState, error) {
	if err := owner.requireOwner(); err != nil {
		return nil, err
	}
	raw, err := owner.directory.read(filepath.Base(owner.path), int(policy.storageMaximum()), true)
	if monitorCheckpointAbsent(err) {
		return newEconomicConservationState(policy), nil
	}
	if err != nil {
		return nil, err
	}
	var result economicConservationState
	if err := decodePlanJson(raw, &result); err != nil {
		return nil, err
	}
	native, err := result.nativeOperatingPolicy(policy)
	if err != nil {
		return nil, err
	}
	if err := result.Native.admitRuntime(ctx, native); err != nil {
		return nil, err
	}
	if err := result.validate(ctx, policy); err != nil {
		return nil, err
	}
	if result.Renewal != nil && result.Renewal.Original.Path != owner.path {
		return nil, errors.New("economic resource renewal moved the original checkpoint")
	}
	if result.FeeRevision != nil && result.FeeRevision.Original.Path != owner.path {
		return nil, errors.New("economic fee revision moved the original checkpoint")
	}
	if result.NativeRenewal != nil && result.NativeRenewal.Original.Path != owner.path {
		return nil, errors.New("economic native adoption moved the original checkpoint")
	}
	if err := result.validateClaimCheckpointPath(owner.path); err != nil {
		return nil, err
	}
	return &result, nil
}

func saveEconomicConservation(ctx context.Context, owner *monitorCheckpointStore, policy economicConservationPolicy, state *economicConservationState) error {
	if err := state.archiveView.requireLive(); err != nil {
		return err
	}
	if state.principalProvisional != nil {
		return errors.New("economic publication requires retained principal snapshot custody, not a provisional plan")
	}
	if err := state.validateClaimCheckpointPath(owner.path); err != nil {
		return err
	}
	if err := state.requireEntitlementHistory(); err != nil {
		return err
	}
	// The original lineage must exist; the complete custody fence below is
	// retained immediately before hashing, validation and publication.
	if err := state.validateNativeApprovalHistory(); err != nil {
		return err
	}
	if (len(state.ClaimWindows) != 0 || state.Archive != nil && len(state.Archive.ClaimHeads) != 0) && state.archiveView == nil {
		return errors.New("economic Claim publication requires admitted original history")
	}
	resources, err := state.resources(policy)
	if err != nil {
		return err
	}
	maximum := resources.headBytes()
	if err := state.archiveView.check(); err != nil {
		return err
	}
	state.ContentHash = state.hash()
	if err := state.validate(ctx, policy); err != nil {
		return err
	}
	if err := owner.requireOwner(); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if uint64(len(raw)+1) > maximum {
		return errMonitorEconomicCapacity
	}
	return errors.Join(owner.directory.publish(filepath.Base(owner.path), append(raw, '\n'), 0600, owner.syncDirectory), owner.requireOwner())
}

func economicConservationIssue(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	return message
}

// A finite sample joins all selected reads. Continuous producer mode has a
// separate native owner; its complete result joins the latest sibling state.
func sampleEconomicConservation(ctx context.Context, policy economicConservationPolicy, prior *economicConservationState, native, vault *rpcClient, now time.Time, hooks monitorServiceHooks) (*economicConservationState, bool, bool, error) {
	return sampleEconomicConservationWithNativeWorker(ctx, policy, prior, native, vault, now, hooks, nil)
}

func sampleEconomicConservationWithNativeWorker(ctx context.Context, policy economicConservationPolicy, prior *economicConservationState, native, vault *rpcClient, now time.Time, hooks monitorServiceHooks, worker *economicConservationNativeWorker) (*economicConservationState, bool, bool, error) {
	if err := prior.archiveView.requireLive(); err != nil {
		return nil, false, false, err
	}
	if err := prior.requireEntitlementHistory(); err != nil {
		return nil, false, false, err
	}
	// These presence checks are pure. One complete fence immediately below
	// protects the dependent policy lookup and source reads.
	if err := prior.validateNativeApprovalHistory(); err != nil {
		return nil, false, false, err
	}
	if (len(prior.ClaimWindows) != 0 || prior.Archive != nil && len(prior.Archive.ClaimHeads) != 0) && prior.archiveView == nil {
		return nil, false, false, errors.New("economic Claim read requires admitted original history")
	}
	if err := prior.archiveView.check(); err != nil {
		return nil, false, false, err
	}
	var err error
	policy, err = prior.operatingPolicy(policy)
	if err != nil {
		return nil, false, false, err
	}
	if now.IsZero() || !prior.SampleAt.IsZero() && now.Before(prior.SampleAt) {
		return nil, false, false, errors.New("economic conservation sample clock is unavailable or behind retained publication")
	}
	seconds := monitorEconomicReadSeconds(policy.ReadBudgetSeconds)
	readCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	var nativeValue *economicEmissionObservation
	var vaultValue *monitorEvmObservation
	var nativeErr, vaultErr error
	claimValues := make([]*protocol.ClaimProgress, len(policy.Claims))
	claimCodes := make([]string, len(policy.Claims))
	client := newMonitorProviderClient()
	defer client.CloseIdleConnections()
	var joined sync.WaitGroup
	joined.Add(2 + len(policy.Claims))
	go func() {
		defer joined.Done()
		if worker == nil && !prior.NativeHeld {
			nativeValue, nativeErr = observeMonitorEconomicNative(readCtx, native, policy.Native, &prior.Native)
		}
	}()
	go func() {
		defer joined.Done()
		if !prior.VaultHeld {
			vaultValue, vaultErr = observeMonitorEconomicEvm(readCtx, vault, policy.Vault, &prior.Vault)
		}
	}()
	for index, claim := range policy.Claims {
		go func(index int, claim monitorClaimPolicy) {
			defer joined.Done()
			if monitorClaimTerminal(prior.ClaimStates[index].Status) {
				claimCodes[index] = prior.ClaimStates[index].Status
				return
			}
			clock := monitorProgressReadClock{}
			if hooks.rpcWait != nil {
				clock.wait = func(ctx context.Context, delay time.Duration) error { return hooks.rpcWait(ctx, claim.Role, delay) }
			}
			claimValues[index], claimCodes[index] = readMonitorClaimWithBudget(readCtx, client, claim, time.Duration(seconds)*time.Second, clock)
			if claimCodes[index] == "ok" {
				claimCodes[index] = prior.claimPublicationCode(claim, claimValues[index])
			}
		}(index, claim)
	}
	joined.Wait()
	nativeObserved := worker == nil
	if worker != nil {
		if result, ready := worker.take(); ready {
			nativeObserved = true
			if result.nativeHash != rootObjectHash(prior.Native) || result.policyHash != rootObjectHash(policy.Native) {
				return nil, false, false, errors.Join(errRpcIntegrity, result.err, errors.New("economic native handoff changed its original cursor or admitted policy"))
			}
			nativeValue, nativeErr = result.observation, result.err
		}
	}
	// Real integrity observations dominate a simultaneous parent cancellation.
	hardNative := nativeErr != nil && monitorEconomicNativeReadCode(nativeErr) == "identity-conflict"
	hardVault := vaultErr != nil && monitorEconomicEvmReadCode(vaultErr) == "identity-conflict"
	hardClaim := false
	for _, code := range claimCodes {
		hardClaim = hardClaim || monitorClaimTerminal(code)
	}
	if ctx.Err() != nil {
		if !hardNative && !hardVault && !hardClaim {
			return nil, false, false, ctx.Err()
		}
		var claimErr error
		if hardClaim {
			claimErr = errors.New("economic Claim source contradicted original authority")
		}
		return nil, false, false, errors.Join(ctx.Err(), nativeErr, vaultErr, claimErr)
	}
	next, err := cloneEconomicConservation(prior, policy)
	if err != nil {
		return nil, false, false, err
	}
	next.SampleAt = now.UTC()
	nativeCurrent, vaultCurrent := nativeObserved && !prior.NativeHeld && nativeErr == nil, !prior.VaultHeld && vaultErr == nil
	next.NativePending = worker != nil && worker.active
	if nativeCurrent && nativeValue != nil {
		if hooks.beforeEconomicNativeAppend != nil {
			hooks.beforeEconomicNativeAppend(readCtx, cancel)
		}
		candidate, copyErr := cloneEconomicConservation(next, policy)
		if copyErr != nil {
			return nil, false, false, copyErr
		}
		nativeErr = candidate.appendNative(readCtx, policy, nativeValue, now)
		if nativeErr == nil {
			next = candidate
		} else {
			nativeCurrent = false
			hardNative = economicConservationAppendHeld(nativeErr)
		}
	}
	if nativeObserved && !prior.NativeHeld {
		next.NativeIssue, next.NativeHeld = economicConservationIssue(nativeErr), hardNative
	}
	if vaultCurrent && vaultValue != nil {
		candidate, copyErr := cloneEconomicConservation(next, policy)
		if copyErr != nil {
			return nil, false, false, copyErr
		}
		vaultErr = candidate.appendVault(policy, vaultValue, now)
		if vaultErr == nil {
			next = candidate
		} else {
			vaultCurrent = false
			hardVault = economicConservationAppendHeld(vaultErr)
		}
	}
	if !prior.VaultHeld {
		next.VaultIssue, next.VaultHeld = economicConservationIssue(vaultErr), hardVault
	}
	for index, claim := range policy.Claims {
		next.ClaimStates[index].observe(claim, claimValues[index], claimCodes[index], now.UTC())
	}
	// Arithmetic and evidence validation can outlive the read join. Recheck the
	// actual owner before publishing any candidate made during that work.
	if ctx.Err() != nil {
		if !hardNative && !hardVault && !hardClaim {
			return nil, false, false, ctx.Err()
		}
		var claimErr error
		if hardClaim {
			claimErr = errors.New("economic Claim source contradicted original authority")
		}
		return nil, false, false, errors.Join(ctx.Err(), nativeErr, vaultErr, claimErr)
	}
	candidate, err := cloneEconomicConservation(next, policy)
	if err != nil {
		return nil, false, false, err
	}
	var fundingErr error
	if policy.EntitlementSources != nil {
		fundingErr = candidate.reconcileEntitlementFunding(ctx)
	}
	if err := errors.Join(candidate.reconcile(policy), candidate.reconcileEntitlementLeaves(ctx), fundingErr); err != nil {
		next.JoinIssue = economicConservationIssue(err)
	} else if err := candidate.reconcileCaptureEffects(ctx, policy, false); err != nil {
		next.JoinIssue = economicConservationIssue(err)
	} else {
		candidate.JoinIssue = ""
		next = candidate
	}
	return next, nativeCurrent, vaultCurrent, nil
}

// Exit zero means a complete sample was durably published, not that 10/90 or
// fee authority has been established. --follow retains this same owner and
// input policy, with bounded reads and no signing or transaction submission.
func runEconomicConservationCommand(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) (code int) {
	refuse := func(err error) int {
		if ctx.Err() != nil && monitorOnlyCancellationCauses(err, 0) {
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 3
	}
	flags := flag.NewFlagSet("observe-economic-conservation", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "exact independent native/vault/Claim policy file")
	policyPin := flags.String("policy-sha256", "", "sha256:DIGEST of the exact policy bytes")
	nativeUrl := flags.String("native-rpc", "", "explicit owned native archive RPC")
	vaultUrl := flags.String("evm-rpc", "", "explicit owned EVM archive RPC")
	checkpoint := flags.String("checkpoint", "", "preprovisioned durable monitor checkpoint")
	metricsPath := flags.String("metrics-file", "", "optional preprovisioned durable Prometheus textfile")
	feeRequest := flags.String("native-fee-request", "", "optional original fee-verifier request, never a derived amount report")
	feeRequestPin := flags.String("native-fee-request-sha256", "", "exact sha256:DIGEST of the optional original fee request")
	follow := flags.Bool("follow", false, "continue from this original retained cursor")
	interval := flags.Duration("interval", 5*time.Second, "sample interval, 1s through 5m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *policyPath == "" || !planSha256(*policyPin) || *nativeUrl == "" || *vaultUrl == "" || *checkpoint == "" || *interval < time.Second || *interval > 5*time.Minute {
		fmt.Fprintln(stderr, "observe-economic-conservation requires --policy FILE --policy-sha256 sha256:DIGEST --native-rpc URL --evm-rpc URL --checkpoint FILE")
		return 2
	}
	if *metricsPath != "" && (*metricsPath == *checkpoint || *metricsPath == *policyPath || *metricsPath == *feeRequest) {
		fmt.Fprintln(stderr, "economic metrics must not replace an original input or checkpoint")
		return 2
	}
	startupRefuse := func(err error) int {
		if *follow && ctx.Err() == nil && economicConservationStartupPending(err) {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return refuse(err)
	}
	raw, digest, err := readPlanFile(ctx, *policyPath, maxRpcReplyBytes)
	if err != nil {
		return startupRefuse(err)
	}
	if digest != *policyPin {
		fmt.Fprintln(stderr, "economic conservation policy differs from exact input pin")
		return 2
	}
	var policy economicConservationPolicy
	if err := decodePlanJson(raw, &policy); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := policy.validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if (*feeRequest == "") != (*feeRequestPin == "") || *feeRequest != "" && (policy.FeeAuthority == nil || !bootstrapRootAbsolutePath(*feeRequest) || !planSha256(*feeRequestPin)) {
		fmt.Fprintln(stderr, "economic native fees require the original policy authority and exact absolute request pin")
		return 2
	}
	seconds := monitorEconomicReadSeconds(policy.ReadBudgetSeconds)
	native, err := newRpcClient(*nativeUrl, time.Duration(seconds)*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer native.httpClient.CloseIdleConnections()
	vault, err := newRpcClient(*vaultUrl, time.Duration(seconds)*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer vault.httpClient.CloseIdleConnections()
	if hooks.rpcWait != nil {
		native.retryWait = func(ctx context.Context, delay time.Duration) error {
			return hooks.rpcWait(ctx, policy.Native.Role, delay)
		}
		vault.retryWait = func(ctx context.Context, delay time.Duration) error {
			return hooks.rpcWait(ctx, policy.Vault.Role, delay)
		}
	}
	network := policy.Native.Observation.Network
	owner, err := policy.openCheckpoint(ctx, *checkpoint, identityExpectation{NativeChain: network.NativeChain, GenesisHash: network.GenesisHash, EvmChainId: network.EvmChainId})
	if err != nil {
		return startupRefuse(err)
	}
	var metrics *monitorMetricsStore
	defer func() {
		if err := closeMonitorServiceOwners(economicConservationRole, metrics, owner, hooks); err != nil {
			fmt.Fprintln(stderr, err)
			code = 3
		}
	}()
	if hooks.afterCheckpointOpen != nil {
		hooks.afterCheckpointOpen(ctx, economicConservationRole, owner.lock)
	}
	state, err := loadEconomicConservation(ctx, owner, policy)
	if err != nil {
		return startupRefuse(err)
	}
	archive, err := openEconomicConservationArchive(ctx, policy, state, hooks)
	if err != nil {
		return startupRefuse(err)
	}
	defer func() {
		if err := archive.close(); err != nil {
			fmt.Fprintln(stderr, err)
			code = 3
		}
	}()
	state.archiveView = archive
	if err := state.validateClaimCheckpointPath(owner.path); err != nil {
		return refuse(err)
	}
	if err := policy.PrincipalRetention.validatePaths(owner.path, *policyPath, *metricsPath, *feeRequest); err != nil {
		return refuse(err)
	}
	if *metricsPath != "" {
		metrics, err = openMonitorMetrics(*metricsPath, ctx)
		if err != nil {
			return startupRefuse(err)
		}
		if hooks.syncDirectory != nil {
			metrics.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(economicConservationRole, "metrics", file) }
		}
		if err := initializeEconomicConservationMetrics(metrics); err != nil {
			return startupRefuse(err)
		}
	}

	operating, err := state.operatingPolicy(policy)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	var feeWorker *economicConservationFeeWorker
	if *feeRequest != "" {
		feeWorker = newEconomicConservationFeeWorker(ctx, policy, planFileReference{Path: *feeRequest, Sha256: *feeRequestPin}, time.Duration(operating.ReadBudgetSeconds)*time.Second, hooks)
		defer func() {
			if err := feeWorker.close(); err != nil {
				fmt.Fprintln(stderr, err)
				code = 3
			}
		}()
	}
	entitlementWorker, err := newEconomicConservationEntitlementWorker(ctx, policy, *vaultUrl, hooks)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	defer func() {
		if err := entitlementWorker.close(); err != nil {
			fmt.Fprintln(stderr, err)
			code = 3
		}
	}()
	native.retryWindow = time.Duration(operating.ReadBudgetSeconds) * time.Second
	vault.retryWindow = native.retryWindow
	var nativeWorker *economicConservationNativeWorker
	if *follow && policy.Native.Observation.Execution.Producer != nil {
		nativeWorker = newEconomicConservationNativeWorker(ctx, policy, native, hooks)
		defer func() {
			if err := nativeWorker.close(); err != nil {
				fmt.Fprintln(stderr, err)
				code = 3
			}
		}()
	}
	if hooks.syncDirectory != nil {
		owner.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(economicConservationRole, "checkpoint", file) }
	}
	progress := newEconomicConservationProgress(state, owner.path, now())
	nextSample := time.Duration(0)
	if *follow {
		nextSample = *interval
	}
	for ctx.Err() == nil {
		// Principal facts advance only when this owner takes a completed native
		// attempt. Compact before its successor starts; never replace a live
		// worker's original cursor or discard its pending result.
		if nativeWorker == nil || !nativeWorker.active {
			state, archive, err = maintainEconomicPrincipalArchive(ctx, policy, owner, state, archive, hooks)
			if err != nil {
				return refuse(err)
			}
		}
		if err := nativeWorker.start(state); err != nil {
			return refuse(err)
		}
		feeWorker.start(state)
		entitlementWorker.start(state)
		next, nativeCurrent, vaultCurrent, err := sampleEconomicConservationWithNativeWorker(ctx, policy, state, native, vault, now().UTC(), hooks, nativeWorker)
		if err != nil {
			if ctx.Err() != nil && monitorOnlyCancellationCauses(err, 0) {
				return 0
			}
			fmt.Fprintln(stderr, err)
			return 3
		}
		if result, ready := feeWorker.take(!*follow); ready {
			next, err = applyEconomicConservationFeeResult(ctx, policy, next, result)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 3
			}
		} else if feeWorker != nil {
			next.NativeFeePending = feeWorker.active
		}
		entitlementWorker.start(next)
		for _, result := range entitlementWorker.take(!*follow) {
			next, err = applyEconomicEntitlementResult(ctx, policy, next, result)
			if err != nil {
				if ctx.Err() != nil && monitorOnlyCancellationCauses(err, 0) {
					return 0
				}
				fmt.Fprintln(stderr, err)
				return 3
			}
		}
		if err := saveEconomicConservation(ctx, owner, policy, next); err != nil {
			return refuse(err)
		}
		state = next
		summary, err := state.summary(ctx, policy, nativeCurrent, vaultCurrent)
		if err != nil {
			return refuse(err)
		}
		summary.Progress = progress.observe(state, summary, now(), nextSample)
		metricsAcknowledged := true
		if metrics != nil {
			if err := owner.requireOwner(); err != nil {
				return refuse(err)
			}
			if err := saveEconomicConservationMetrics(metrics, &summary); err != nil {
				if !*follow || ctx.Err() != nil || !economicConservationStartupPending(err) {
					return refuse(err)
				}
				// The financial checkpoint is already acknowledged. A known
				// textfile I/O outage must not stop its healthy read workers or
				// replay those retained effects. Retry on the next bounded
				// sample, without emitting success for this failed publication.
				fmt.Fprintln(stderr, "economic metrics publication pending:", err)
				metricsAcknowledged = false
			}
		}
		if metricsAcknowledged {
			encoded, err := json.Marshal(summary)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 3
			}
			encoded = append(encoded, '\n')
			written, err := stdout.Write(encoded)
			if err == nil && written != len(encoded) {
				err = io.ErrShortWrite
			}
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 3
			}
			if hooks.afterEvent != nil {
				hooks.afterEvent(ctx, economicConservationRole)
			}
		}
		if *follow {
			feeWorker.start(state)
		}
		if !*follow {
			if !nativeCurrent || !vaultCurrent || state.JoinIssue != "" || state.NativeFeeIssue != "" || state.NativeFeePending || summary.OriginalEntitlements != nil && summary.OriginalEntitlements.PendingRoots != 0 {
				return 3
			}
			for _, claim := range state.ClaimStates {
				if claim.Status != "ok" {
					return 3
				}
			}
			return 0
		}
		if hooks.wait != nil {
			if !hooks.wait(ctx, economicConservationRole, *interval) {
				return 0
			}
			continue
		}
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
	return 0
}
