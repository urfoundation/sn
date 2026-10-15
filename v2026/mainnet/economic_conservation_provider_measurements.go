// Provider conformance joins independently verified complete work and trial
// windows to every original payout row. These private inputs are produced by
// the original witness readers; caller-supplied summary booleans are not inputs.
package main

import (
	"context"
	"errors"
	"math"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A complete work inventory proves zero bytes for an absent provider only after
// all independently expected SDK cuts, including empty cuts, were verified.
type economicProviderWorkValues struct {
	artifactHash  string
	authorityHash string
	windowHash    string
	complete      bool
	providers     []economicProviderWorkValue
}

// Paid and free completed bytes have the same meaning in this projection.
type economicProviderWorkValue struct {
	clientId   [16]byte
	networkId  [16]byte
	usageBytes uint64
}

// The complete original registry includes failed and idle trials and the
// independently proved eligibility of every expected provider. An absent
// validator or eligibility proof cannot manufacture a zero or an eligible row.
type economicProviderTrialValues struct {
	artifactHash       string
	registryHash       string
	windowHash         string
	minimumAssignments uint64
	complete           bool
	providers          []economicProviderTrialValue
}

// These values come from original trial and eligibility records, independently
// of the operator's ProviderInput fields and its reconstructed Merkle root.
type economicProviderTrialValue struct {
	clientId          [16]byte
	networkId         [16]byte
	coldkey           [32]byte
	assignments       uint64
	confirmations     uint64
	eligible          bool
	headExcluded      bool
	exclusionReason   string
	bindingGeneration uint64
}

// This comparable projection is retained only beside its original verified
// census. A cold reference must match the original held snapshot exactly;
// possessing or hashing this projection alone supplies no authority.
type economicConservationProviderMeasurement struct {
	WorkInventoryHash   string `json:"work_inventory_hash"`
	WalletOriginalsHash string `json:"wallet_originals_hash"`
	// the retained network consent chains; absent when no provider fell back
	NetworkWalletOriginalsHash string `json:"network_wallet_originals_hash,omitempty"`
	// the retained delegation and global hotkey consent chains; absent when no
	// provider fell back to hotkey mode
	HotkeyDelegationOriginalsHash string `json:"hotkey_delegation_originals_hash,omitempty"`
	HotkeyConsentOriginalsHash    string `json:"hotkey_consent_originals_hash,omitempty"`
	BindingOriginalsHash          string `json:"binding_originals_hash"`
	TrialAuthorityHash            string `json:"trial_authority_hash"`
	TrialOriginalsHash            string `json:"trial_originals_hash"`
	ArtifactHash                  string `json:"artifact_hash"`
	WorkAuthorityHash             string `json:"work_authority_hash"`
	WorkWindowHash                string `json:"work_window_hash"`
	TrialRegistryHash             string `json:"trial_registry_hash"`
	TrialWindowHash               string `json:"trial_window_hash"`
	ProviderHash                  string `json:"provider_hash"`
	Providers                     uint64 `json:"providers"`
	CompletedBytes                uint64 `json:"completed_bytes"`
	Assignments                   uint64 `json:"assignments"`
	Confirmations                 uint64 `json:"confirmations"`
}

// Validate the present complete components before considering missing ones, so
// an observed contradiction cannot be hidden by an unrelated unavailable cut.
// Canonical artifact reconstruction independently rechecks every leaf and floor.
func reconcileEconomicProviderMeasurements(ctx context.Context, artifact *payoutartifact.Artifact, work *economicProviderWorkValues, trials *economicProviderTrialValues) (*economicConservationProviderMeasurement, error) {
	if ctx == nil {
		return nil, errors.New("economic provider measurement requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if artifact == nil {
		return nil, errors.New("economic provider measurement lacks its original artifact")
	}
	if len(artifact.Providers) > maximumEconomicEntitlementProviders {
		return nil, errMonitorEconomicCapacity
	}
	if err := payoutartifact.VerifyWithContext(ctx, artifact); err != nil {
		return nil, err
	}
	providers := make(map[[16]byte]payoutartifact.ProviderInput, len(artifact.Providers))
	for _, value := range artifact.Providers {
		providers[value.ClientID] = value
	}
	result := &economicConservationProviderMeasurement{ArtifactHash: artifact.ContentHash, ProviderHash: artifact.ProviderSnapshotHash, Providers: uint64(len(artifact.Providers))}
	workComplete := work != nil && work.complete
	if workComplete {
		if work.artifactHash != artifact.ContentHash || !payoutartifact.IsDigest(work.authorityHash, "sha256:") || !payoutartifact.IsDigest(work.windowHash, "sha256:") {
			return nil, errors.New("economic complete work changed its original artifact or window authority")
		}
		if len(work.providers) > maximumEconomicEntitlementProviders {
			return nil, errMonitorEconomicCapacity
		}
		if len(work.providers) != len(artifact.Providers) {
			return nil, errors.New("economic payout omitted or added an independently expected provider")
		}
		seen := make(map[[16]byte]bool, len(work.providers))
		for _, value := range work.providers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			original, exists := providers[value.clientId]
			if value.clientId == ([16]byte{}) || seen[value.clientId] || !exists || original.NetworkID != value.networkId || original.UsageBytes != value.usageBytes {
				return nil, errors.New("economic original provider completed bytes differ from complete work inventory")
			}
			if value.usageBytes > math.MaxUint64-result.CompletedBytes {
				return nil, errors.New("economic original provider byte sum overflows")
			}
			seen[value.clientId] = true
			result.CompletedBytes += value.usageBytes
		}
		for _, original := range artifact.Providers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !seen[original.ClientID] {
				return nil, errors.New("economic payout credits bytes absent from complete original work")
			}
		}
		if result.CompletedBytes != artifact.TotalUsageBytes {
			return nil, errors.New("economic complete work differs from original total completed bytes")
		}
		result.WorkAuthorityHash, result.WorkWindowHash = work.authorityHash, work.windowHash
	}
	trialsComplete := trials != nil && trials.complete
	if trialsComplete {
		if trials.artifactHash != artifact.ContentHash || !payoutartifact.IsDigest(trials.registryHash, "sha256:") || !payoutartifact.IsDigest(trials.windowHash, "sha256:") || trials.minimumAssignments == 0 || trials.minimumAssignments != artifact.ReliabilityAMin {
			return nil, errors.New("economic complete trials changed original registry, window or reliability policy")
		}
		if len(trials.providers) > maximumEconomicEntitlementProviders {
			return nil, errMonitorEconomicCapacity
		}
		if len(trials.providers) != len(artifact.Providers) {
			return nil, errors.New("economic payout omitted or added an independently expected provider")
		}
		seen := make(map[[16]byte]bool, len(trials.providers))
		for _, value := range trials.providers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			original, exists := providers[value.clientId]
			if value.clientId == ([16]byte{}) || seen[value.clientId] || !exists || original.NetworkID != value.networkId || original.Coldkey != value.coldkey || original.Assignments != value.assignments || original.Confirmations != value.confirmations || original.Eligible != value.eligible || original.HeadExcluded != value.headExcluded || original.ExclusionReason != value.exclusionReason || original.BindingGeneration != value.bindingGeneration || original.ReliabilityPPM != protocol.ReliabilityPPM(value.confirmations, value.assignments, trials.minimumAssignments) {
				return nil, errors.New("economic provider trial or eligibility differs from original complete authority")
			}
			if value.confirmations > value.assignments || value.assignments > math.MaxUint64-result.Assignments || value.confirmations > math.MaxUint64-result.Confirmations {
				return nil, errors.New("economic original trial census exceeds its exact exposure")
			}
			seen[value.clientId] = true
			result.Assignments += value.assignments
			result.Confirmations += value.confirmations
		}
		result.TrialRegistryHash, result.TrialWindowHash = trials.registryHash, trials.windowHash
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !workComplete || !trialsComplete {
		return nil, nil
	}
	return result, nil
}
