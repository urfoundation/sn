// A reviewed rate-only bridge separates immutable attempt replay authority
// from the policy that governs a later on-chain deposit decision.
package validator

import (
	"errors"
	"reflect"
	"slices"

	"github.com/urfoundation/sn/protocol"
)

// Selects only an exact configured document. The predecessor is admitted only
// by the complete reviewed rate amendment, never by a hash-only allowlist.
func ReleasePolicyForHash(cfg *ReleaseConfig, encodedHash string) (protocol.Policy, error) {
	if cfg == nil {
		return protocol.Policy{}, errors.New("release policy authority is absent")
	}
	expected, err := parseHash32("release policy selection", encodedHash)
	if err != nil {
		return protocol.Policy{}, err
	}
	current, err := cfg.Policy.Hash()
	if err != nil {
		return protocol.Policy{}, err
	}
	configured, err := parseHash32("configured release policy", cfg.PolicyHash)
	if err != nil || configured != current {
		return protocol.Policy{}, errors.Join(errors.New("configured release policy document differs"), err)
	}
	if cfg.PreviousPolicy != nil {
		if err := protocol.ValidateTestnetRateAmendment(cfg.PreviousPolicy, &cfg.Policy); err != nil {
			return protocol.Policy{}, err
		}
	}
	selected := cfg.Policy
	if expected != current {
		if cfg.PreviousPolicy == nil {
			return protocol.Policy{}, errors.New("release policy is not configured")
		}
		previous, err := cfg.PreviousPolicy.Hash()
		if err != nil || previous != expected {
			return protocol.Policy{}, errors.Join(errors.New("release policy is not the exact configured predecessor"), err)
		}
		selected = *cfg.PreviousPolicy
	}
	selected.Deposit.Tiers = slices.Clone(selected.Deposit.Tiers)
	return selected, nil
}

// Historical readers retain every non-policy configuration pin and narrow
// policy authority to the exact document named by the original signed record.
func releaseConfigForPolicyHash(cfg *ReleaseConfig, hash string) (*ReleaseConfig, error) {
	policy, err := ReleasePolicyForHash(cfg, hash)
	if err != nil {
		return nil, err
	}
	owned := *cfg
	owned.Policy = policy
	owned.PolicyHash = hash
	if !reflect.DeepEqual(policy, cfg.Policy) {
		owned.PreviousPolicy = nil
	}
	return &owned, nil
}

// Mutable tier storage never escapes a policy authority owner by alias.
func cloneReleasePolicy(policy *protocol.Policy) *protocol.Policy {
	if policy == nil {
		return nil
	}
	owned := *policy
	owned.Deposit.Tiers = slices.Clone(policy.Deposit.Tiers)
	return &owned
}
