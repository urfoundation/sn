// Runtime consumers retain the original economic policy. Only the producer's
// signed lineage and actual first completion select a later execution runtime.
package main

import (
	"context"
	"errors"
	"slices"
)

// This small index is private, immutable and rebuilt on cold admission. It
// carries no execution amounts and cannot be supplied in a checkpoint or RPC.
type nativeProducerRuntimeAdmission struct {
	policyHash    string
	revisionsHash string
	authorities   []nativeProducerRuntimeAuthority
	adoptedAfter  []uint64
	catalog       []monitorEconomicRuntimeEntry
}

type nativeProducerRuntimeAuthority struct {
	hash    string
	runtime rootReceiptProfile
}

// Copied-source restore selects physical custody, never different signed bytes.
// All other owners use the same bounded exact-file reader as producer approval.
type nativeProducerRuntimeReadKey struct{}

// Older in-process callers have no lifecycle parameter. Public owners always
// pass their context; no runtime approval is inferred from a missing context.
func nativeRuntimeAdmissionContext(contexts []context.Context) context.Context {
	if len(contexts) != 0 {
		return contexts[0]
	}
	return context.Background()
}

func readNativeProducerRuntimeOriginal(ctx context.Context, reference planFileReference, maximum int) ([]byte, error) {
	if read, ok := ctx.Value(nativeProducerRuntimeReadKey{}).(func(context.Context, planFileReference, int) ([]byte, error)); ok {
		return read(ctx, reference, maximum)
	}
	raw, digest, err := readPlanFile(ctx, reference.Path, maximum)
	if err != nil {
		return nil, err
	}
	if digest != reference.Sha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("native runtime original bytes differ from their exact reference"))
	}
	return raw, nil
}

func nativeProducerRuntimePolicyHash(policy economicEmissionPolicy) string {
	// A continuous page changes only these two observation coordinates. The
	// signature loader still checks its original finalized anchor and domain.
	policy.From, policy.Through = economicEmissionBoundary{}, economicEmissionBoundary{}
	return rootObjectHash(policy)
}

func nativeProducerRuntimeRevisions(state *nativeExecutionProducerState) []nativeProducerRenewalAcknowledgement {
	if state == nil {
		return nil
	}
	return state.AuthorityRevisions
}

func (self *nativeProducerRuntimeAdmission) matches(policy economicEmissionPolicy, state *nativeExecutionProducerState) bool {
	return self != nil && self.policyHash == nativeProducerRuntimePolicyHash(policy) && self.revisionsHash == rootObjectHash(nativeProducerRuntimeRevisions(state))
}

// The caller has admitted every full signed original document. The first
// completion authenticates delayed adoption, not just the reviewed ancestor.
func admitNativeProducerRuntimes(policy economicEmissionPolicy, state *nativeExecutionProducerState, authorities []nativeProducerReviewedAuthority, read func(planFileReference) ([]byte, error)) (*nativeProducerRuntimeAdmission, error) {
	if policy.Execution == nil || policy.Execution.Producer == nil || len(authorities) != len(policy.Execution.Producer.Renewals)+1 || len(authorities) > maximumNativeProducerRenewals+1 {
		return nil, errors.New("native runtime admission lost its original signed producer lineage")
	}
	revisions := nativeProducerRuntimeRevisions(state)
	if len(revisions) >= len(authorities) {
		return nil, errors.New("native runtime admission lost an adopted original authority")
	}
	result := &nativeProducerRuntimeAdmission{policyHash: nativeProducerRuntimePolicyHash(policy), revisionsHash: rootObjectHash(revisions)}
	for index, authority := range authorities {
		result.authorities = append(result.authorities, nativeProducerRuntimeAuthority{hash: authority.reference.Sha256, runtime: authority.value.Runtime})
		found := false
		for _, entry := range result.catalog {
			found = found || entry.Profile == authority.value.Runtime
		}
		if !found {
			result.catalog = append(result.catalog, monitorEconomicRuntimeEntry{Profile: authority.value.Runtime, ReviewSha256: authority.value.ReviewSha256, Purposes: []string{economicRuntimeStatePurpose, economicRuntimeEventsPurpose, economicRuntimeFeePurpose}})
		}
		if index > 0 && index <= len(revisions) {
			ack := revisions[index-1]
			if ack.FirstCompletion == nil {
				return nil, errors.New("native runtime adoption omitted its original first completion")
			}
			raw, err := read(*ack.FirstCompletion)
			if err != nil {
				return nil, err
			}
			if err := validateNativeProducerRenewalAcknowledgement(ack, authority, policy.Execution.Directory, raw); err != nil {
				return nil, err
			}
			result.adoptedAfter = append(result.adoptedAfter, ack.AdoptedAfter.Number)
		}
	}
	return result, nil
}

func (self *monitorEconomicNativeState) admitRuntime(ctx context.Context, policy monitorEconomicNativePolicy) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	execution := policy.Observation.Execution
	if execution == nil || execution.Producer == nil || len(execution.Producer.Renewals) == 0 {
		self.runtimeAdmission = nil
		return nil
	}
	if self.runtimeAdmission.matches(policy.Observation, self.ExecutionProducer) {
		return nil
	}
	if ctx == nil {
		return errors.New("native renewed runtime requires original lifecycle admission")
	}
	authorities, err := loadNativeProducerAuthorities(ctx, policy.Observation)
	if err != nil {
		return err
	}
	admission, err := admitNativeProducerRuntimes(policy.Observation, self.ExecutionProducer, authorities, func(reference planFileReference) ([]byte, error) {
		return readNativeProducerRuntimeOriginal(ctx, reference, nativeProducerCompletionMaximum(execution.FeeCensus))
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.runtimeAdmission = admission
	return nil
}

// Explicit read purposes remain independent. The empty-catalog fallback uses
// only signed producer profiles, within the monitor's existing reviewed limits.
func (self *monitorEconomicNativeState) runtimeReadPolicy(policy monitorEconomicNativePolicy) (monitorEconomicNativePolicy, error) {
	policy.runtimeAdmission = nil
	if self.runtimeAdmission != nil || len(nativeProducerRuntimeRevisions(self.ExecutionProducer)) != 0 {
		if !self.runtimeAdmission.matches(policy.Observation, self.ExecutionProducer) {
			return policy, errors.New("native execution runtime lacks its admitted signed lineage")
		}
		policy.runtimeAdmission = self.runtimeAdmission
		if len(policy.RuntimeCatalog) == 0 {
			policy.RuntimeCatalog = self.runtimeAdmission.catalog
		}
	}
	return policy, policy.validateCatalog()
}

// Selection uses this already matched immutable index. Rehashing a complete
// fee participant policy per retained event would multiply bounded admission
// work by the entire hot-history length.
func (self *nativeProducerRuntimeAdmission) executionPolicy(policy economicEmissionPolicy, boundary economicEmissionBoundary, authorityHash string) (economicEmissionPolicy, error) {
	if policy.Execution == nil || policy.Execution.Producer == nil {
		return policy, nil
	}
	selected := nativeProducerRuntimeAuthority{hash: policy.Execution.Producer.Authority.Sha256, runtime: policy.Runtime}
	if self != nil {
		index := 0
		for _, after := range self.adoptedAfter {
			if boundary.Number <= after {
				break
			}
			index++
		}
		selected = self.authorities[index]
	}
	if authorityHash != "" && authorityHash != selected.hash {
		return policy, errors.New("native execution selected a different original producer authority")
	}
	policy.Runtime = selected.runtime
	return policy, nil
}

func (self *nativeProducerRuntimeAdmission) principalExecutionPolicy(policy economicEmissionPolicy, outcome nativeExecutionOutcome) (economicEmissionPolicy, error) {
	if policy.Execution != nil && policy.Execution.Producer != nil && outcome.ProducerAuthorityHash == "" {
		return policy, errors.New("native principal execution omitted its original producer authority")
	}
	return self.executionPolicy(policy, outcome.Boundary, outcome.ProducerAuthorityHash)
}

func (self *monitorEconomicNativeState) validateRuntimeContext(policy monitorEconomicNativePolicy, boundary economicEmissionBoundary, execution, post *rootReceiptProfile) error {
	if execution == nil || post == nil {
		return errors.New("native economic retained event lost its execution or post-state artifact")
	}
	if !slices.Contains(policy.profiles(), *execution) || !slices.Contains(policy.profiles(), *post) {
		return errors.New("native economic retained runtime context changed")
	}
	selected, err := policy.runtimeAdmission.executionPolicy(policy.Observation, boundary, "")
	if err != nil {
		return err
	}
	if policy.Observation.Execution != nil && policy.Observation.Execution.Producer != nil && selected.Runtime != *execution {
		return errors.New("native economic execution runtime differs from its adopted signed authority")
	}
	return nil
}
