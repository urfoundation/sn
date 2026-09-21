package main

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

const maximumPlanRenewalValidationCacheEntries = 32

// Snapshot readers revisit the active plan and its ancestors for contract,
// fleet and conviction observations. Renewal signature verification is pure
// over the authenticated plan bytes; repeating it does not make those bytes
// fresher. Retain only successful identities, never a mutable decoded plan.
// Each read still authenticates the wire hash and runs every other budget and
// artifact check, including checks which read files outside the plan.
type planRenewalValidationCache struct {
	mu        sync.Mutex
	successes map[[sha256.Size]byte]bool
	order     [][sha256.Size]byte
}

var persistedPlanRenewalValidations planRenewalValidationCache

func (cache *planRenewalValidationCache) verify(raw []byte, plan *SetupPlan, verify func(*SetupPlan) error) error {
	key := sha256.Sum256(raw)
	cache.mu.Lock()
	found := cache.successes[key]
	cache.mu.Unlock()
	if found {
		return nil
	}
	if err := verify(plan); err != nil {
		return err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.successes == nil {
		cache.successes = map[[sha256.Size]byte]bool{}
	}
	if !cache.successes[key] {
		if len(cache.order) == maximumPlanRenewalValidationCacheEntries {
			delete(cache.successes, cache.order[0])
			cache.order = cache.order[1:]
		}
		cache.successes[key] = true
		cache.order = append(cache.order, key)
	}
	return nil
}

func (cache *planRenewalValidationCache) decode(raw []byte, historical bool, verify func(*SetupPlan) error) (*SetupPlan, error) {
	plan, err := decodePersistedPlanWire(raw)
	if err != nil {
		return nil, err
	}
	plan.validatorEvidenceHistorical = historical
	if err := validatePlanBudgetWithFleetRenewalVerifier(plan, func(plan *SetupPlan) error {
		return cache.verify(raw, plan, verify)
	}); err != nil {
		return nil, fmt.Errorf("persisted setup plan: %w", err)
	}
	return plan, nil
}
