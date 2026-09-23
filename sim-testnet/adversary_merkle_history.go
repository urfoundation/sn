// Keeps authenticated historical payout metadata across bounded custody samples.
// Bodies and live contract observations are never retained as current evidence.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// One actor owns this bounded cache. An exact scope change discards its entries;
// only fully verified immutable content hashes can add an epoch. No body is held.
type liveMerkleHistoryCache struct {
	scope    liveMerkleHistoryScope
	epochKVs map[string]uint64
}

// Binds retained metadata to both its full payout domain and serving generation.
type liveMerkleHistoryScope struct {
	configHash, deploymentId, policyHash, genesisHash, operatorBase, generation string
	previousPolicyHash                                                          string
	chainId                                                                     uint64
	netuid                                                                      uint16
	operatorId                                                                  int
	coordinator, vault                                                          common.Address
}

// Returns no cache authority when the durable owner is missing or inconsistent.
// The probe can still validate fresh artifacts; it cannot borrow prior metadata.
func liveMerkleSourceGeneration(stateDir string, operatorId int) string {
	states, specs, err := (&liveScenarioFaultDriver{stateDir: stateDir}).processSnapshot()
	if err != nil {
		return ""
	}
	id := fmt.Sprintf("operator-%d-api", operatorId)
	state, stateOk := states[id]
	spec, specOk := specs[id]
	if !stateOk || !specOk || state.Role != "operator-api" || spec.Role != state.Role || state.Identity != fmt.Sprintf("no:%d", operatorId) || state.Identity != spec.Identity || state.PID <= 1 || state.Restarts < 0 {
		return ""
	}
	if _, err := time.Parse(time.RFC3339Nano, state.StartedAt); err != nil {
		return ""
	}
	generation, err := canonicalHashHex(struct {
		Spec      ProcessSpec
		Pid       int
		StartedAt string
		Restarts  int
	}{Spec: spec, Pid: state.PID, StartedAt: state.StartedAt, Restarts: state.Restarts})
	if err != nil {
		return ""
	}
	return generation
}

// Validates every new history member, preserves a verified prefix on interruption,
// and always obtains a freshly verified body for the selected latest artifact.
// The caller refreshes the bounded history and checks generation around this call.
func selectLiveMerkleArtifact(ctx context.Context, cfg *ResolvedConfig, deployment *ContractDeployment, operatorBase string, operatorId int, keys []string, get func(context.Context, string) ([]byte, error), cache *liveMerkleHistoryCache, generation string) (*payoutArtifact, error) {
	if len(keys) > maximumPayoutArtifactHistoryKeys {
		return nil, errors.New("payout artifact history exceeds the retained metadata bound")
	}
	scope := liveMerkleHistoryScope{configHash: cfg.ConfigHash, deploymentId: cfg.Config.Deployment.DeploymentID, policyHash: cfg.PolicyHash, genesisHash: cfg.Public.Chain.GenesisHash, operatorBase: operatorBase, generation: generation, chainId: cfg.ChainID, netuid: cfg.Netuid, operatorId: operatorId, coordinator: deployment.CoordinatorProxy, vault: deployment.SettlementVault}
	if cfg.previousPolicy != nil {
		var err error
		scope.previousPolicyHash, err = cfg.previousPolicy.HashHex()
		if err != nil || !policyRateAmendmentHistoryHash(cfg, scope.previousPolicyHash) {
			return nil, errors.Join(errors.New("payout history predecessor policy is not approved"), err)
		}
	}
	if cache != nil {
		if cache.scope != scope || generation == "" {
			cache.scope, cache.epochKVs = scope, nil
		}
		if generation == "" {
			cache = nil
		} else if cache.epochKVs == nil {
			cache.epochKVs = map[string]uint64{}
		}
	}
	fetch := func(hash string) (*payoutArtifact, error) {
		body, err := get(ctx, hash)
		if err != nil {
			return nil, err
		}
		var candidate payoutArtifact
		if err := json.Unmarshal(body, &candidate); err != nil {
			return nil, fmt.Errorf("decode payout artifact: %w", err)
		}
		if err := verifyPayoutArtifact(&candidate); err != nil {
			return nil, fmt.Errorf("verify payout artifact: %w", err)
		}
		if !strings.EqualFold(candidate.ContentHash, "sha256:"+hash) || candidate.DeploymentID != scope.deploymentId || candidate.ChainID != scope.chainId || candidate.Netuid != scope.netuid || candidate.NoID != uint64(scope.operatorId) || !strings.EqualFold(candidate.GenesisHash, scope.genesisHash) || !policyRateAmendmentHistoryHash(cfg, candidate.PolicyHash) || candidate.Coordinator != scope.coordinator || candidate.SettlementVault != scope.vault {
			return nil, errors.New("payout artifact identity does not match the active deployment")
		}
		return &candidate, nil
	}
	var latest *payoutArtifact
	var latestHash string
	var latestEpoch uint64
	epochHashKVs := map[uint64]string{}
	seenHashKVs := map[string]bool{}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hash := strings.ToLower(strings.TrimSuffix(filepath.Base(key), filepath.Ext(key)))
		if len(hash) != 64 || seenHashKVs[hash] {
			return nil, errors.New("payout artifact history is not uniquely content-addressed")
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return nil, errors.New("payout artifact history contains a non-hexadecimal content address")
		}
		seenHashKVs[hash] = true
		var candidate *payoutArtifact
		var epoch uint64
		var retained bool
		if cache != nil {
			epoch, retained = cache.epochKVs[hash]
		}
		if !retained {
			var err error
			candidate, err = fetch(hash)
			if err != nil {
				return nil, err
			}
			epoch = candidate.Epoch
			if cache != nil && len(cache.epochKVs) < maximumPayoutArtifactHistoryKeys {
				cache.epochKVs[hash] = epoch
			}
		}
		if priorHash, ok := epochHashKVs[epoch]; ok && priorHash != hash {
			return nil, fmt.Errorf("operator %d equivocated at payout epoch %d", operatorId, epoch)
		}
		epochHashKVs[epoch] = hash
		if latestHash == "" || epoch > latestEpoch {
			latestHash, latestEpoch, latest = hash, epoch, candidate
		}
	}
	if latestHash == "" {
		return nil, fmt.Errorf("%w: operator has no verified payout artifact", errLiveMerkleEvidenceUnavailable)
	}
	if latest == nil {
		return fetch(latestHash)
	}
	return latest, nil
}
