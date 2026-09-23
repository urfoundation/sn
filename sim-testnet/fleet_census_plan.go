// A census repeatedly reads the same authenticated plan. Cache only its pure
// renewal/signature validation; the wire hash and every other plan gate stay
// fresh, including artifact validation which can depend on external files.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Bump when fleet renewal or lifecycle renewal acceptance rules change.
const fleetCensusPlanVerifierVersion = "fleet-renewal-plan-v1"

// This reader owns no transaction authorization and creates no trusted mutable
// plan object. Each caller receives a newly decoded, byte-authenticated plan.
func readFleetCensusPlan(cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	return readFleetCensusPlanWithVerifier(cfg, stateDir, validateFleetRenewalPlan)
}

// The verifier callback makes exact cold/warm behavior observable in tests;
// production always uses the ordinary complete renewal verifier above.
func readFleetCensusPlanWithVerifier(cfg *ResolvedConfig, stateDir string, verify func(*SetupPlan) error) (*SetupPlan, error) {
	if verify == nil {
		return nil, errors.New("fleet census plan verifier is unavailable")
	}
	raw, err := readSetupPlanBytes(stateDir, "plan.json")
	if err != nil {
		return nil, err
	}
	plan, err := decodePersistedPlanWire(raw)
	if err != nil {
		return nil, err
	}
	entry := newFleetCensusPlanCacheEntry(cfg, stateDir, raw)
	err = validatePlanBudgetWithFleetRenewalVerifier(plan, func(plan *SetupPlan) error {
		if entry.readSuccess() {
			return nil
		}
		if err := verify(plan); err != nil {
			return err
		}
		entry.saveSuccess(context.Background())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persisted setup plan: %w", err)
	}
	return plan, nil
}

// Reuse the existing private authenticated-success store without scanning old
// receipt caches: no earlier verifier ever emitted this new local proof kind.
func newFleetCensusPlanCacheEntry(cfg *ResolvedConfig, stateDir string, raw []byte) *historicalAuditCacheEntry {
	if cfg == nil || cfg.Config == nil || cfg.WalletMaterial == "" {
		return nil
	}
	contextHash, err := canonicalHashHex(struct {
		DeploymentId string
		ConfigHash   string
		ChainId      uint64
		Netuid       uint16
	}{DeploymentId: cfg.Config.Deployment.DeploymentID, ConfigHash: cfg.ConfigHash, ChainId: cfg.ChainID, Netuid: cfg.Netuid})
	if err != nil {
		return nil
	}
	proof := historicalAuditCacheProof{Schema: historicalAuditCacheSchema, VerifierVersion: fleetCensusPlanVerifierVersion,
		ContextHash: contextHash, Kind: "fleet-census-plan-renewal", InputHash: bytesSHA256(raw), Success: true}
	nameHash, err := canonicalHashHex(proof)
	if err != nil {
		return nil
	}
	return &historicalAuditCacheEntry{stateDir: stateDir, readOnly: cfg.readOnlyAudit, name: strings.TrimPrefix(nameHash, "0x") + ".json",
		key: derive32(cfg, "fleet-census-plan-cache/v1"), proof: proof}
}
