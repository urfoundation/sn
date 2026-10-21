// The conservation owner consumes only its own completed fee verifier. Exact
// original requests and proof reports remain in the checkpoint and its archive
// snapshots. A selected receipt census never becomes all-provider fee coverage.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"time"
)

type economicConservationFeeEvidence struct {
	Request  economicNativeFeeRequest  `json:"original_request"`
	Evidence economicNativeFeeEvidence `json:"original_evidence"`
}

type economicConservationFeeSummary struct {
	Census                 string  `json:"census"`
	OriginalRequests       uint64  `json:"original_requests"`
	SelectedTransactions   uint64  `json:"selected_transactions"`
	AuthenticatedFees      uint64  `json:"authenticated_fees"`
	MissingFees            uint64  `json:"missing_fees"`
	WithdrawalRao          *string `json:"withdrawal_rao"`
	RefundRao              *string `json:"refund_rao"`
	DebitRao               *string `json:"debit_rao"`
	SelectedCensusComplete bool    `json:"selected_census_complete"`
	WholeProviderCensus    bool    `json:"whole_provider_census"`
	EvidenceChain          string  `json:"evidence_chain"`
}

func (self economicConservationPolicy) validateFeeAuthority() error {
	if self.FeeAuthority == nil {
		return nil
	}
	value := self.FeeAuthority
	if !rootCanonicalHash(value.ApprovalPublicKey) || value.Genesis != self.Native.Observation.Network.GenesisHash || value.EvmChainId != self.Native.Observation.Network.EvmChainId || !planSha256(value.EngineSha256) || !planSha256(value.CheckpointSha256) || !planSha256(value.ReviewSha256) || !planSha256(value.ProfileSha256) {
		return errors.New("economic native fee authority differs from original network or reviewed policy")
	}
	return nil
}

// Immutable original receipts may reappear in another independently admitted
// archive request. Their exact context and known fees must agree; repetition
// does not earn another debit, and an unavailable observation cannot erase one.
func economicConservationSameFee(prior, current historicalFeeContextTransaction) bool {
	if prior.TransactionHash != current.TransactionHash || prior.Role != current.Role || prior.Sender != current.Sender || prior.Nonce != current.Nonce || !reflect.DeepEqual(prior.Receipt, current.Receipt) || !reflect.DeepEqual(prior.NativeBlock, current.NativeBlock) {
		return false
	}
	if prior.FeeAuthenticated && current.FeeAuthenticated {
		return reflect.DeepEqual(prior.Candidate, current.Candidate) && reflect.DeepEqual(prior.ActualWithdrawalRao, current.ActualWithdrawalRao) && reflect.DeepEqual(prior.ActualRefundRao, current.ActualRefundRao) && reflect.DeepEqual(prior.ActualDebitRao, current.ActualDebitRao)
	}
	return true
}

// Revalidate retained typed evidence without inventing a new execution result.
// Only admitNativeFees below can add a report: it invokes real proof/replay
// verification. This function is also used after archive/checkpoint decoding.
func (self *economicConservationState) feeSummary(policy economicConservationPolicy) (*economicConservationFeeSummary, error) {
	if _, err := self.nativeFeeAuthority(policy); err != nil {
		return nil, err
	}
	if err := self.validateAdmittedFeeArchive(); err != nil {
		return nil, err
	}
	if self.Archive != nil && (self.Archive.NativeFees != nil || self.Archive.FeeRevisionHead != nil) && self.archiveView == nil {
		// Decoding checks only bounded syntax. Every public owner admits the
		// full original archive and repeats validation before observing/output.
		return self.Archive.NativeFees, nil
	}
	if len(self.NativeFees) == 0 && (self.archiveView == nil || len(self.archiveView.feeRetired) == 0) {
		if self.archiveView != nil && len(self.archiveView.feeEvidence) != 0 {
			return nil, errors.New("economic active checkpoint dropped retained native fee evidence")
		}
		return nil, nil
	}
	if policy.FeeAuthority == nil {
		return nil, errors.New("economic checkpoint enrolled native fee authority after original admission")
	}
	if err := policy.validateFeeAuthority(); err != nil {
		return nil, err
	}
	result := &economicConservationFeeSummary{Census: "original-selected-signed-receipts", EvidenceChain: rootObjectHash(policy.FeeAuthority)}
	requests := map[string]string{}
	transactions := map[string]historicalFeeContextTransaction{}
	if self.archiveView != nil && self.archiveView.feeSummary != nil {
		requests, transactions = maps.Clone(self.archiveView.feeRetired), maps.Clone(self.archiveView.feeTransactions)
		result.OriginalRequests, result.EvidenceChain = uint64(len(requests)), self.archiveView.feeSummary.EvidenceChain
	}
	active := map[string]bool{}
	for _, retained := range self.NativeFees {
		request, report := retained.Request, retained.Evidence
		digest := report.ContentHash
		report.ContentHash = ""
		if !self.nativeFeePolicyKnown(policy, request.Policy) || report.Schema != economicNativeFeeEvidenceSchema || report.RequestHash != rootObjectHash(request) || active[report.RequestHash] || requests[report.RequestHash] != "" && requests[report.RequestHash] != digest || !planSha256(report.ApprovalHash) || !planSha256(digest) || rootObjectHash(report) != digest || report.Context == nil || report.Context.RequestHash != rootObjectHash(request.Context) || report.Context.Genesis != policy.FeeAuthority.Genesis || report.Context.EvmChainId != policy.FeeAuthority.EvmChainId || report.SpendingAuthorized || report.WholeBlockCensusComplete {
			return nil, errors.New("economic retained native fee evidence changed its original request or scope")
		}
		if err := request.Context.validate(); err != nil {
			return nil, err
		}
		context := report.Context
		if context.Replay == nil || context.ContextProof == nil || context.ContextProof.Finality == nil || !context.ContextProof.Finality.GrandpaCertificatesVerified || !context.ContextProof.Finality.NativeHeaderAncestryVerified || !context.ContextProof.Finality.NativeEvmCommitmentVerified || !context.AuthorityCheckpointAuthenticated || !context.RuntimeSourceAuthenticated || !context.FinalityAuthenticated || context.SpendingAuthorized || context.Admission != "independently-admitted-original-native-fee-context" {
			return nil, errors.New("economic retained native fee report lost original proof authority")
		}
		active[report.RequestHash] = true
		if requests[report.RequestHash] == "" {
			requests[report.RequestHash] = digest
			result.EvidenceChain = rootObjectHash([]string{result.EvidenceChain, digest})
			result.OriginalRequests++
		}
		var selected, authenticated uint64
		for _, transaction := range context.Transactions {
			if transaction.NativeBlock == nil {
				continue
			}
			selected++
			if transaction.Receipt == nil || transaction.TransactionHash != transaction.Receipt.Hash || transaction.NativeBlock.Hash != economicNativeFeeHash(context.Replay.ChildHash) || transaction.NativeBlock.Number != context.Native.NativeBlock.Number {
				return nil, errors.New("economic retained fee transaction lost original native/receipt identity")
			}
			if transaction.FeeAuthenticated {
				candidate := transaction.Candidate
				if candidate == nil || candidate.ExtrinsicIndex == nil || len(candidate.EventOrdinals) != 3 || candidate.WithdrawalRao == nil || candidate.RefundRao == nil || candidate.DebitRao == nil || !reflect.DeepEqual(transaction.ActualWithdrawalRao, candidate.WithdrawalRao) || !reflect.DeepEqual(transaction.ActualRefundRao, candidate.RefundRao) || !reflect.DeepEqual(transaction.ActualDebitRao, candidate.DebitRao) {
					return nil, errors.New("economic retained fee was detached from original withdrawal/refund")
				}
				withdrawal, e1 := monitorEconomicInteger(*transaction.ActualWithdrawalRao)
				refund, e2 := monitorEconomicInteger(*transaction.ActualRefundRao)
				debit, e3 := monitorEconomicInteger(*transaction.ActualDebitRao)
				if e1 != nil || e2 != nil || e3 != nil || refund.Cmp(withdrawal) > 0 || withdrawal.Sub(withdrawal, refund).Cmp(debit) != 0 {
					return nil, errors.New("economic retained native fee amounts do not conserve debit")
				}
				authenticated++
			} else if transaction.ActualWithdrawalRao != nil || transaction.ActualRefundRao != nil || transaction.ActualDebitRao != nil {
				return nil, errors.New("economic unknown native fee acquired an amount")
			}
			prior, known := transactions[transaction.TransactionHash]
			if known && !economicConservationSameFee(prior, transaction) {
				return nil, errors.New("economic native fee contradicts an original retained transaction")
			}
			if !known || !prior.FeeAuthenticated && transaction.FeeAuthenticated {
				transactions[transaction.TransactionHash] = transaction
			}
		}
		if selected != report.SelectedTransactions || authenticated != report.AuthenticatedTransactions || report.SelectedCensusComplete != (selected > 0 && selected == authenticated) {
			return nil, errors.New("economic native fee report changed exact selected transaction census")
		}
	}
	if self.archiveView != nil {
		for request, original := range self.archiveView.feeEvidence {
			if requests[request] != original {
				return nil, errors.New("economic active checkpoint dropped or changed original native fee evidence")
			}
		}
	}
	withdrawal, refund, debit := "0", "0", "0"
	for _, transaction := range transactions {
		result.SelectedTransactions++
		if !transaction.FeeAuthenticated {
			result.MissingFees++
			continue
		}
		result.AuthenticatedFees++
		var err error
		withdrawal, err = economicConservationSum(withdrawal, *transaction.ActualWithdrawalRao)
		if err != nil {
			return nil, err
		}
		refund, err = economicConservationSum(refund, *transaction.ActualRefundRao)
		if err != nil {
			return nil, err
		}
		debit, err = economicConservationSum(debit, *transaction.ActualDebitRao)
		if err != nil {
			return nil, err
		}
	}
	result.SelectedCensusComplete = result.SelectedTransactions != 0 && result.MissingFees == 0
	if result.SelectedCensusComplete {
		result.WithdrawalRao, result.RefundRao, result.DebitRao = &withdrawal, &refund, &debit
	}
	return result, nil
}

func (self economicConservationState) feeFacts() uint64 {
	result := uint64(len(self.NativeFeeObligations))
	for _, value := range self.NativeFees {
		result++
		if value.Evidence.Context != nil {
			result += uint64(len(value.Evidence.Context.Transactions))
		}
	}
	return result
}

// All writes are to a detached candidate after a completed owned verifier.
// Publication failure can safely retry the same request without a second fee.
func admitEconomicConservationNativeFees(ctx context.Context, policy economicConservationPolicy, prior *economicConservationState, request economicNativeFeeRequest, budget time.Duration, hooks historicalReplayHooks) (*economicConservationState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prior == nil || policy.FeeAuthority == nil || prior.PolicyHash != policy.identityHash() {
		return nil, errors.New("economic fee request differs from the original conservation authority")
	}
	if err := errors.Join(policy.validateFeeAuthority(), prior.archiveView.check()); err != nil {
		return nil, err
	}
	authorities, err := prior.admittedNativeFeePolicies(policy)
	if err != nil {
		return nil, err
	}
	if prior.retainedNativeFee(rootObjectHash(request)) != "" {
		if _, err := prior.feeSummary(policy); err != nil {
			return nil, err
		}
		return cloneEconomicConservation(prior, policy)
	}
	if authority, known := authorities[rootObjectHash(request.Policy)]; !known || request.Policy != authority {
		return nil, errors.Join(errEconomicNativeFeeIntegrity, errors.New("economic fee request differs from original-key retained policy authority"))
	}
	value, err := runEconomicNativeFeeEvidence(ctx, request, budget, hooks)
	if err != nil {
		return nil, err
	}
	return appendEconomicConservationNativeFeeEvidence(ctx, policy, prior, request, value)
}

// The independent worker transfers its own completed result here. Only fee
// fields are merged onto the latest checkpoint; its earlier native/vault view
// never replaces progress observed while verification was in flight.
func appendEconomicConservationNativeFeeEvidence(ctx context.Context, policy economicConservationPolicy, prior *economicConservationState, request economicNativeFeeRequest, value *economicNativeFeeEvidence) (*economicConservationState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prior == nil || policy.FeeAuthority == nil || prior.PolicyHash != policy.identityHash() || value == nil || value.Context == nil || value.RequestHash != rootObjectHash(request) {
		return nil, errors.Join(errEconomicNativeFeeIntegrity, errors.New("economic completed fee result differs from original request authority"))
	}
	if retained := prior.retainedNativeFee(value.RequestHash); retained != "" {
		if retained != value.ContentHash {
			return nil, errors.Join(errEconomicNativeFeeIntegrity, errors.New("economic completed fee result replaced original evidence"))
		}
		return cloneEconomicConservation(prior, policy)
	}
	authorities, err := prior.admittedNativeFeePolicies(policy)
	if err != nil {
		return nil, err
	}
	if authority, known := authorities[rootObjectHash(request.Policy)]; !known || request.Policy != authority {
		return nil, errors.Join(errEconomicNativeFeeIntegrity, errors.New("economic completed fee proof uses an unadmitted retained policy"))
	}
	operating, err := prior.operatingPolicy(policy)
	if err != nil {
		return nil, err
	}
	next, err := cloneEconomicConservation(prior, policy)
	if err != nil {
		return nil, err
	}
	next.NativeFees = append(next.NativeFees, economicConservationFeeEvidence{Request: request, Evidence: *value})
	next.NativeFeeObligations = next.pendingNativeFeeObligations()
	if next.facts() > operating.MaximumFacts {
		return nil, errMonitorEconomicCapacity
	}
	if _, err := next.feeSummary(policy); err != nil {
		return nil, errors.Join(errEconomicNativeFeeIntegrity, err)
	}
	next.ContentHash = next.hash()
	raw, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)+1) > operating.headBytes() {
		return nil, errMonitorEconomicCapacity
	}
	// The archive operation can retire these exact original reports under
	// retained custody; this live observer never discards a proof to fit.
	return next, errors.Join(ctx.Err(), prior.archiveView.check())
}

// Archive ownership retains the complete report in its original checkpoint.
// The small index prevents a restored head from deleting or replacing that
// evidence while leaving the old archive summary otherwise unchanged.
func (self *economicConservationArchiveView) retainFeeEvidence(original *economicConservationState) error {
	for _, retained := range original.NativeFees {
		request, digest := retained.Evidence.RequestHash, retained.Evidence.ContentHash
		if prior, ok := self.feeEvidence[request]; ok {
			if prior != digest {
				return errors.New("economic archived native fee evidence conflicts with original request")
			}
			continue
		}
		if err := self.charge([]string{request, digest}); err != nil {
			return err
		}
		self.feeEvidence[request] = digest
	}
	return nil
}
