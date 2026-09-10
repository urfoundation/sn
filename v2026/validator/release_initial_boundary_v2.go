//go:build linux || darwin

// Initial startup authority joins complete prefix/EMA replay with actual
// finalized coordinator views. Neither a matching file checksum nor a signed
// activation can choose an arbitrary initial settlement boundary.
package validator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
)

// The independently pinned initial cut must fall inside its real on-chain
// activation epoch, under that policy and an active operator. The native clock
// is deliberately never compared numerically to an EVM block.
func (self *ChainClient) authenticateReleaseInitialBoundaryV2Context(ctx context.Context, expected ReleaseEvidenceV2ActivationContext, maxHeaderBytes uint64) (resultErr error) {
	if ctx == nil {
		return errors.New("initial boundary context is absent")
	}
	defer func() { resultErr = errors.Join(resultErr, ctx.Err()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := expected.CanonicalJSON(maxHeaderBytes); err != nil {
		return err
	}
	if self == nil || self.client == nil || self.coordinator == nil || self.chainId == nil {
		return errors.New("initial boundary chain owner is absent")
	}
	if err := self.requireRelease(); err != nil {
		return err
	}
	if self.contractAddr != common.Address(expected.Activation.Domain.Coordinator) || self.chainId.Cmp(new(big.Int).SetUint64(expected.Activation.Domain.ChainID)) != 0 {
		return errors.New("initial boundary differs from the configured chain owner")
	}
	boundary := expected.InitialCut.Boundary
	blockHash, err := canonicalAttemptHex32("initial boundary EVM hash", boundary.EVMBlockHash, false)
	if err != nil {
		return err
	}
	finalized, finalizedHash, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	if finalized < boundary.EVMBlock || finalized == boundary.EVMBlock && finalizedHash != blockHash {
		return errors.New("initial boundary is not finalized at its configured EVM hash")
	}
	epoch := new(big.Int).SetUint64(boundary.SettlementEpoch)
	outputs, err := self.batchCallsAtHashContext(ctx, boundary.EVMBlock, blockHash, []chainBatchCall{
		{address: self.contractAddr, calldata: self.coordinator.PackCurrentEpoch()},
		{address: self.contractAddr, calldata: self.coordinator.PackPolicyAt(epoch)},
		{address: self.contractAddr, calldata: self.coordinator.PackOperatorAt(new(big.Int).SetUint64(expected.Activation.NoID), epoch)},
	})
	if err != nil {
		return err
	}
	current, err := self.coordinator.UnpackCurrentEpoch(outputs[0])
	if err != nil {
		return err
	}
	if current == nil || !current.IsUint64() || current.Uint64() != boundary.SettlementEpoch {
		return errors.New("initial boundary belongs to another on-chain settlement epoch")
	}
	if err := canonicalReleaseActivationV2View("currentEpoch", outputs[0], current); err != nil {
		return err
	}
	policy, err := self.coordinator.UnpackPolicyAt(outputs[1])
	if err != nil {
		return err
	}
	if err := canonicalReleaseActivationV2View("policyAt", outputs[1], policy); err != nil {
		return err
	}
	if policy.PolicyHash != expected.Activation.Domain.PolicyHash || policy.EffectiveEpoch > boundary.SettlementEpoch || policy.EffectiveBlock == 0 || policy.EpochBlocks == 0 {
		return errors.New("initial boundary policy differs from approved activation")
	}
	// Match the coordinator's uint256 cadence arithmetic without uint64 wrap.
	start := new(big.Int).Mul(new(big.Int).SetUint64(boundary.SettlementEpoch-policy.EffectiveEpoch), new(big.Int).SetUint64(policy.EpochBlocks))
	start.Add(start, new(big.Int).SetUint64(policy.EffectiveBlock))
	end := new(big.Int).Add(new(big.Int).Set(start), new(big.Int).SetUint64(policy.EpochBlocks))
	block := new(big.Int).SetUint64(boundary.EVMBlock)
	if block.Cmp(start) < 0 || block.Cmp(end) >= 0 {
		return errors.New("initial boundary falls outside its on-chain policy window")
	}
	operator, err := self.coordinator.UnpackOperatorAt(outputs[2])
	if err != nil {
		return err
	}
	if err := canonicalReleaseActivationV2View("operatorAt", outputs[2], operator); err != nil {
		return err
	}
	if !operator.Active || operator.EffectiveEpoch > boundary.SettlementEpoch {
		return errors.New("initial boundary operator was not active at activation")
	}
	return ctx.Err()
}

// All configured history is replayed before historical RPC begins. Each
// operator has bounded independent work; any error cancels and joins the full
// census and publishes no partial history. Actual disk/generation ownership
// still must be established before starting a measurement worker.
func authenticateReleaseEvidenceV2InitialHistory(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey) (last *AttemptSettlementClosure, resultErr error) {
	if ctx == nil || cfg == nil || chain == nil {
		return nil, errors.New("initial history owner is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			last = nil
		}
	}()
	last, err := replayReleaseEvidenceV2ActivationHistories(ctx, cfg, inputs, serverKeys)
	if err != nil {
		return nil, err
	}
	operationCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(cfg))
	defer cancel()
	failures := make([]error, len(inputs))
	completed := make([]bool, len(inputs))
	var next atomic.Uint64
	var joined sync.WaitGroup
	workers := min(len(inputs), runtime.GOMAXPROCS(0))
	joined.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer joined.Done()
			for {
				index := next.Add(1) - 1
				if index >= uint64(len(inputs)) || operationCtx.Err() != nil {
					return
				}
				// A synchronous client-side Goexit still runs this owned defer.
				// Neither a lost result slot nor an unclaimed index is success.
				func() {
					defer func() {
						if !completed[index] {
							failures[index] = errors.New("initial history boundary worker did not complete")
							cancel()
						}
					}()
					failures[index] = chain.authenticateReleaseInitialBoundaryV2Context(operationCtx, inputs[index].Context, cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
					completed[index] = true
				}()
				if failures[index] != nil {
					cancel()
					return
				}
			}
		}()
	}
	joined.Wait()
	for index := range completed {
		if !completed[index] && failures[index] == nil {
			failures[index] = errors.New("initial history boundary census was not completed")
		}
	}
	if err := errors.Join(append(failures, operationCtx.Err())...); err != nil {
		return nil, err
	}
	return last, nil
}
