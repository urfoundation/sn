package main

// adversary_matrix.go loads the release-locked research catalogue which maps
// known Bittensor and UR-network attack classes to bounded concurrent actors,
// exact local-runtime tests, or observation-only sentinels. Keeping this
// separate from the whitepaper scenario matrix prevents a passive test name
// from being mistaken for a live adversarial exercise.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AdversarialMatrixSafety struct {
	SharedTestnet string `json:"shared_testnet"`
	LocalRuntime  string `json:"local_runtime"`
	Lifecycle     string `json:"lifecycle"`
}

type AdversarialMatrixRow struct {
	ID             string   `json:"id"`
	Class          string   `json:"class"`
	Vector         string   `json:"vector"`
	Sources        []string `json:"sources"`
	Preconditions  string   `json:"preconditions"`
	ExecutionMode  string   `json:"execution_mode"`
	ActorIDs       []string `json:"actor_ids"`
	Oracle         string   `json:"oracle"`
	Metrics        []string `json:"metrics"`
	StopConditions []string `json:"stop_conditions"`
	LocalTests     []string `json:"local_tests"`
}

type AdversarialMatrix struct {
	Schema  string                  `json:"schema"`
	Release string                  `json:"release"`
	Safety  AdversarialMatrixSafety `json:"safety"`
	Rows    []AdversarialMatrixRow  `json:"rows"`
	Hash    string                  `json:"-"`
}

var requiredAdversarialVectors = []string{
	"consensus-stale-weight-copy",
	"consensus-reveal-following",
	"consensus-minority-cabal-self-weight",
	"consensus-rival-knifing",
	"consensus-threshold-stake-churn",
	"liquid-alpha-bond-copy-participation-churn",
	"weights-withhold-late-invalid-reveal",
	"weights-fee-free-block-fill",
	"validator-dropout-disagreement",
	"uid-churn-pruning-reregistration",
	"commitment-field-parser-type-confusion",
	"hotkey-swap-reputation-reset",
	"proxy-scope-alias-bypass",
	"root-staking-index-state-bloat",
	"root-claimed-swap-watermark-inflation",
	"root-basket-hidden-reward-on-unstake",
	"proxy-staking-mev-slippage",
	"h160-ss58-domain-unit-confusion",
	"runtime-precompile-metadata-drift",
	"runtime-signed-precompile-foreign-frame",
	"registration-burn-price-race",
	"subnet-eviction-moving-price-pressure",
	"subnet-reserve-drain-flow-accounting",
	"rpc-lag-equivocation-history-gap",
	"mev-shield-finality-era-expiry",
	"plaintext-unauthenticated-miner-transport-mitm",
	"empty-body-hash-integrity-bypass",
	"missing-signature-validator-impersonation",
	"operator-api-resource-pressure",
	"verify-poison-distinguishability",
	"verify-replay-concurrent-extend",
	"verify-vpk-rotation-rate-bypass",
	"artifact-withhold-tamper-equivocate",
	"selective-service-teach-to-test",
	"provider-collusion-short-circuit",
	"shared-prefix-sybil-rebind",
	"affiliation-mask-evasion",
	"deposit-tier-boundary-cross-no-replay",
	"payout-root-omission-duplication-wallet-change",
	"signature-domain-nonce-expiry-replay",
	"contract-reentrancy-partial-funding-reorder",
	"malicious-upgrade-pause-role-compromise",
	"late-keeper-carry-claim-ttl-double-claim",
	"rounding-gas-unbounded-loop-accounting",
	"settlement-runtime-transfer-floor-credit",
	"runtime-composite-atomicity-rollback",
	"runtime-lock-identity-migration-integrity",
	"runtime-order-fill-idempotency",
	"runtime-issuance-reserve-migration-conservation",
	"runtime-resource-metering-panic-resistance",
	"drand-round-jump-timelock-denial",
	"drand-randomness-signature-binding",
	"nested-proxy-filter-intersection",
	"balance-transfer-coldkey-swap-caller",
	"beta-escrow-stake-transfer-injection",
	"queued-registration-reservation-rate-price",
	"childkey-graph-cycle-empty-set",
	"subnet-lease-termination-stranded-stake",
	"registration-lock-floor-price-manipulation",
	"concentrated-liquidity-subnet-freeze",
	"dependency-restart-state-loss",
}

var requiredSubtensorAdvisories = map[string]string{
	"GHSA-h98r-p37h-h4mv": "weights-fee-free-block-fill",
	"GHSA-m759-m8mv-q3m5": "proxy-scope-alias-bypass",
	"GHSA-qh57-vpv2-3fvp": "proxy-scope-alias-bypass",
	"GHSA-xm63-2wwx-pm6w": "proxy-scope-alias-bypass",
	"GHSA-vpjj-mhgr-cphg": "hotkey-swap-reputation-reset",
	"GHSA-wc2g-rc74-vgw3": "hotkey-swap-reputation-reset",
	"GHSA-rhmm-mqf8-v6gv": "root-staking-index-state-bloat",
	"GHSA-6c95-q3r3-rgwq": "root-claimed-swap-watermark-inflation",
}

// These public issues are the reviewed security/economic history as of the
// release lock. Keeping the issue-to-vector mapping executable prevents a
// later catalogue rewrite from retaining a generic category while silently
// dropping the concrete upstream regression that motivated it.
var requiredSubtensorIssueVectors = map[string]string{
	"1651": "subnet-eviction-moving-price-pressure",
	"2102": "weights-fee-free-block-fill",
	"2104": "registration-burn-price-race",
	"2107": "childkey-graph-cycle-empty-set",
	"2109": "childkey-graph-cycle-empty-set",
	"2110": "childkey-graph-cycle-empty-set",
	"2113": "runtime-issuance-reserve-migration-conservation",
	"2146": "runtime-lock-identity-migration-integrity",
	"2154": "subnet-lease-termination-stranded-stake",
	"2156": "runtime-composite-atomicity-rollback",
	"2194": "runtime-issuance-reserve-migration-conservation",
	"2195": "late-keeper-carry-claim-ttl-double-claim",
	"2200": "uid-churn-pruning-reregistration",
	"2201": "runtime-lock-identity-migration-integrity",
	"2211": "uid-churn-pruning-reregistration",
	"2228": "concentrated-liquidity-subnet-freeze",
	"2274": "runtime-issuance-reserve-migration-conservation",
	"2291": "registration-burn-price-race",
	"2336": "rounding-gas-unbounded-loop-accounting",
	"2338": "proxy-scope-alias-bypass",
	"2367": "runtime-issuance-reserve-migration-conservation",
	"2394": "runtime-resource-metering-panic-resistance",
	"2397": "runtime-resource-metering-panic-resistance",
	"2398": "childkey-graph-cycle-empty-set",
	"2399": "childkey-graph-cycle-empty-set",
	"2405": "runtime-resource-metering-panic-resistance",
	"2411": "runtime-resource-metering-panic-resistance",
	"2455": "runtime-precompile-metadata-drift",
	"2515": "runtime-lock-identity-migration-integrity",
	"2553": "rpc-lag-equivocation-history-gap",
	"2572": "uid-churn-pruning-reregistration",
	"2573": "subnet-lease-termination-stranded-stake",
	"2639": "rpc-lag-equivocation-history-gap",
	"2661": "runtime-composite-atomicity-rollback",
	"2662": "runtime-composite-atomicity-rollback",
	"2663": "subnet-lease-termination-stranded-stake",
	"2664": "runtime-composite-atomicity-rollback",
	"2665": "runtime-lock-identity-migration-integrity",
	"2666": "runtime-composite-atomicity-rollback",
	"2667": "runtime-issuance-reserve-migration-conservation",
	"2724": "runtime-resource-metering-panic-resistance",
	"2726": "runtime-lock-identity-migration-integrity",
	"2733": "runtime-issuance-reserve-migration-conservation",
	"2735": "runtime-composite-atomicity-rollback",
	"2737": "subnet-reserve-drain-flow-accounting",
	"2738": "runtime-issuance-reserve-migration-conservation",
	"2739": "runtime-lock-identity-migration-integrity",
	"2740": "runtime-composite-atomicity-rollback",
	"2741": "runtime-resource-metering-panic-resistance",
	"2792": "runtime-order-fill-idempotency",
	"2793": "runtime-issuance-reserve-migration-conservation",
	"2794": "drand-round-jump-timelock-denial",
	"2795": "runtime-order-fill-idempotency",
	"2827": "runtime-precompile-metadata-drift",
	"2844": "registration-lock-floor-price-manipulation",
	"3005": "registration-lock-floor-price-manipulation",
	"3008": "root-basket-hidden-reward-on-unstake",
	"3024": "subnet-eviction-moving-price-pressure",
	"3026": "registration-lock-floor-price-manipulation",
	"3064": "commitment-field-parser-type-confusion",
	"3066": "proxy-staking-mev-slippage",
	"3068": "rpc-lag-equivocation-history-gap",
	"3081": "proxy-staking-mev-slippage",
}

// The Python SDK and transport layer have security-relevant behavior which is
// independent of the runtime repository. Keep those public reports locked in
// the same executable catalogue so a Subtensor-only issue scan cannot produce
// a false claim of exhaustive Bittensor coverage.
var requiredBittensorIssueVectors = map[string]string{
	"3392": "missing-signature-validator-impersonation",
	"3395": "mev-shield-finality-era-expiry",
	"3406": "plaintext-unauthenticated-miner-transport-mitm",
	"3407": "empty-body-hash-integrity-bypass",
}

func loadAdversarialMatrix(snRepo, relative string) (*AdversarialMatrix, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return nil, errors.New("adversarial matrix path must be repository-relative")
	}
	root := filepath.Clean(snRepo)
	path := filepath.Clean(filepath.Join(root, relative))
	if !pathWithin(root, path) {
		return nil, errors.New("adversarial matrix path escapes the sn repository")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var matrix AdversarialMatrix
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		return nil, fmt.Errorf("decode adversarial matrix: %w", err)
	}
	if matrix.Schema != "urnetwork-adversarial-matrix-v1" || matrix.Release != "1.0" {
		return nil, errors.New("adversarial matrix must use schema v1 and release 1.0")
	}
	if len(matrix.Rows) < len(requiredAdversarialVectors) || matrix.Safety.SharedTestnet == "" || matrix.Safety.LocalRuntime == "" || matrix.Safety.Lifecycle == "" {
		return nil, errors.New("adversarial matrix is incomplete")
	}
	allowedModes := map[string]bool{"live-safe": true, "bounded-emulation": true, "observation-only": true, "local-runtime-only": true}
	seen := map[string]bool{}
	for index, row := range matrix.Rows {
		if row.ID == "" || seen[row.ID] {
			return nil, fmt.Errorf("adversarial matrix row %d has an empty or duplicate id %q", index, row.ID)
		}
		seen[row.ID] = true
		if row.Class == "" || row.Vector == "" || row.Preconditions == "" || row.Oracle == "" || !allowedModes[row.ExecutionMode] || len(row.Sources) == 0 || len(row.ActorIDs) == 0 || len(row.Metrics) == 0 || len(row.StopConditions) == 0 || len(row.LocalTests) == 0 {
			return nil, fmt.Errorf("adversarial matrix row %s is incomplete", row.ID)
		}
		for _, source := range row.Sources {
			if !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "repo://") {
				return nil, fmt.Errorf("adversarial matrix row %s has unsupported source %q", row.ID, source)
			}
			for prefix, issueMap := range map[string]map[string]string{
				"https://github.com/RaoFoundation/subtensor/issues/": requiredSubtensorIssueVectors,
				"https://github.com/RaoFoundation/bittensor/issues/": requiredBittensorIssueVectors,
			} {
				if !strings.HasPrefix(source, prefix) {
					continue
				}
				issue := strings.TrimPrefix(source, prefix)
				if _, ok := issueMap[issue]; !ok {
					return nil, fmt.Errorf("adversarial matrix row %s has unreviewed issue source %s", row.ID, source)
				}
			}
		}
		for _, value := range append(append(append([]string{}, row.ActorIDs...), row.Metrics...), row.StopConditions...) {
			if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
				return nil, fmt.Errorf("adversarial matrix row %s contains a malformed reference", row.ID)
			}
		}
	}
	for _, id := range requiredAdversarialVectors {
		if !seen[id] {
			return nil, fmt.Errorf("adversarial matrix is missing required vector %s", id)
		}
	}
	for advisory, vectorID := range requiredSubtensorAdvisories {
		found := false
		for _, row := range matrix.Rows {
			if row.ID != vectorID {
				continue
			}
			for _, source := range row.Sources {
				if strings.Contains(source, "/security/advisories/"+advisory) {
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("adversarial matrix does not map published Subtensor advisory %s to %s", advisory, vectorID)
		}
	}
	for issue, vectorID := range requiredSubtensorIssueVectors {
		found := false
		for _, row := range matrix.Rows {
			if row.ID != vectorID {
				continue
			}
			for _, source := range row.Sources {
				if strings.Contains(source, "/issues/"+issue) {
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("adversarial matrix does not map reviewed Subtensor issue #%s to %s", issue, vectorID)
		}
	}
	for issue, vectorID := range requiredBittensorIssueVectors {
		found := false
		for _, row := range matrix.Rows {
			if row.ID != vectorID {
				continue
			}
			for _, source := range row.Sources {
				if strings.Contains(source, "github.com/RaoFoundation/bittensor/issues/"+issue) {
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("adversarial matrix does not map reviewed Bittensor issue #%s to %s", issue, vectorID)
		}
	}
	var canonical any
	if err := json.Unmarshal(b, &canonical); err != nil {
		return nil, err
	}
	matrix.Hash, err = canonicalHashHex(canonical)
	if err != nil {
		return nil, err
	}
	return &matrix, nil
}

func validateAdversarialActorCoverage(matrix *AdversarialMatrix, actorIDs []string) error {
	if matrix == nil {
		return errors.New("adversarial matrix is absent")
	}
	known := map[string]bool{}
	for _, id := range actorIDs {
		if id == "" || known[id] {
			return fmt.Errorf("adversarial actor id %q is empty or duplicated", id)
		}
		known[id] = true
	}
	covered := map[string]bool{}
	for _, row := range matrix.Rows {
		for _, id := range row.ActorIDs {
			if !known[id] {
				return fmt.Errorf("adversarial matrix row %s references unavailable concurrent actor %s", row.ID, id)
			}
			covered[id] = true
		}
	}
	for _, id := range actorIDs {
		if !covered[id] {
			return fmt.Errorf("concurrent adversarial actor %s is not mapped to a researched vector", id)
		}
	}
	return nil
}
