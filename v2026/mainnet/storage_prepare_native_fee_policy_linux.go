//go:build linux

// Complete fee rosters occur in original signed approval files. Restore inputs
// retain an exact policy hash so multiple owners can share one prepared root
// without duplicating that roster inside Core's unchanged request envelope.
package main

import (
	"context"
	"errors"
)

// Only the account vector is projected out. The explicit original descriptor
// selects its admitted document frame; nil legacy policy remains byte-exact.
func compactStorageNativeProducerScope(scope storageNativeProducerScope) (storageNativeProducerScope, error) {
	if scope.Policy.Execution == nil || scope.Policy.Execution.FeeCensus == nil {
		return scope, nil
	}
	fees := scope.Policy.Execution.FeeCensus
	if err := fees.validate(); err != nil {
		return storageNativeProducerScope{}, err
	}
	if scope.FeePolicyHash != "" || scope.ApprovalSources == nil || scope.Approvals != nil {
		return storageNativeProducerScope{}, errors.New("compact fee policy requires exact original approval sources")
	}
	execution, descriptor := *scope.Policy.Execution, *fees
	scope.FeePolicyHash = rootObjectHash(fees)
	descriptor.Participants = nil
	execution.FeeCensus = &descriptor
	scope.Policy.Execution = &execution
	return scope, nil
}

// No reconstructed policy escapes before the complete original key, runtime,
// signature, checkpoint and renewal lineage pass their existing validation.
func loadStorageNativeProducerScope(ctx context.Context, scope storageNativeProducerScope) (storageNativeProducerScope, []nativeProducerReviewedAuthority, error) {
	if ctx == nil || scope.Policy.Execution == nil || scope.Policy.Execution.Producer == nil {
		return storageNativeProducerScope{}, nil, errors.New("native restore lacks original producer scope")
	}
	if err := ctx.Err(); err != nil {
		return storageNativeProducerScope{}, nil, err
	}
	if scope.FeePolicyHash != "" {
		fees, producer := scope.Policy.Execution.FeeCensus, scope.Policy.Execution.Producer
		if !planSha256(scope.FeePolicyHash) || fees == nil || fees.Schema != nativeFeeCensusSchema || !planSha256(fees.ReviewSha256) || fees.Participants != nil || scope.Approvals != nil || len(scope.ApprovalSources) != 1+len(producer.Renewals) {
			return storageNativeProducerScope{}, nil, errors.New("native restore compact fee policy lost its exact original descriptor or sources")
		}
		if err := producer.validate(); err != nil {
			return storageNativeProducerScope{}, nil, err
		}
		raw, err := readStorageNativeApproval(ctx, scope, 0, producer.Authority)
		if err != nil {
			return storageNativeProducerScope{}, nil, err
		}
		var original nativeProducerAuthority
		if err := decodePlanJson(raw, &original); err != nil {
			return storageNativeProducerScope{}, nil, err
		}
		if original.FeeCensus == nil || original.FeeCensus.Schema != fees.Schema || original.FeeCensus.ReviewSha256 != fees.ReviewSha256 || rootObjectHash(original.FeeCensus) != scope.FeePolicyHash {
			return storageNativeProducerScope{}, nil, errors.New("native restore fee roster differs from the original complete policy hash")
		}
		execution := *scope.Policy.Execution
		execution.FeeCensus = original.FeeCensus
		scope.Policy.Execution, scope.FeePolicyHash = &execution, ""
	}
	authorities, err := readStorageNativeProducerAuthorities(ctx, scope)
	if err != nil {
		return storageNativeProducerScope{}, nil, err
	}
	return scope, authorities, nil
}

// Callers that need only authority retain the same full-policy admission.
func storageNativeProducerAuthorities(ctx context.Context, scope storageNativeProducerScope) ([]nativeProducerReviewedAuthority, error) {
	_, authorities, err := loadStorageNativeProducerScope(ctx, scope)
	return authorities, err
}
