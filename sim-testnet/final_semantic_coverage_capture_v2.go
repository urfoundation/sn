//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

func finalNativeCoverageEVMBlocksV2(window ScenarioAcceptanceWindow) ([]uint64, error) {
	if window.EpochCount == 0 || window.EpochBlocks == 0 || window.StartBlock == 0 || window.EpochCount > 1024 {
		return nil, errors.New("native coverage settlement window is incomplete or unbounded")
	}
	result := []uint64{window.StartBlock}
	for index := uint64(1); index <= window.EpochCount; index++ {
		span, ok := checkedMul(index, window.EpochBlocks)
		if !ok {
			return nil, errors.New("native coverage epoch span overflows")
		}
		end, ok := checkedAdd(window.StartBlock, span)
		if !ok || end == 0 {
			return nil, errors.New("native coverage epoch boundary overflows")
		}
		result = append(result, end-1)
	}
	return result, nil
}

func collectFinalNativeCoverageV2(ctx context.Context, release *validatorpkg.ReleaseConfig, chain *validatorpkg.ChainClient, native *crv4.Chain, hotkey [32]byte, captured *validatorpkg.ReleaseEvidenceV2Capture, window ScenarioAcceptanceWindow, retain func(context.Context, validatorpkg.ReleaseEvidenceV2CaptureSource, []byte) error) ([]FinalNativeCheckpointV2, error) {
	blocks, err := finalNativeCoverageEVMBlocksV2(window)
	if err != nil {
		return nil, err
	}
	if len(captured.Intents) == 0 {
		return nil, errors.New("native coverage has no original signed intent history")
	}
	uid := captured.Intents[len(captured.Intents)-1].Intent.SelfUID
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: release.RuntimeSpec, TransactionVersion: release.TransactionVersion, StateVersion: release.StateVersion}, CodeHash: release.RuntimeCodeHash, MetadataHash: release.RuntimeMetadataHash}
	var result []FinalNativeCheckpointV2
	for _, number := range blocks {
		hash, err := chain.BlockHashContext(ctx, number)
		if err != nil {
			return nil, err
		}
		observation, err := readFinalNativeCheckpointV2(ctx, native, ChainHead{Number: number, Hash: fmt.Sprintf("0x%x", hash)}, release.Netuid, uid, hotkey, runtime)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(observation)
		if err != nil {
			return nil, err
		}
		if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "native-coverage", Name: fmt.Sprintf("evm-%020d", number)}, raw); err != nil {
			return nil, err
		}
		result = append(result, observation)
	}
	seen := map[ChainHead]bool{}
	for _, checkpoint := range result[1:] {
		for _, head := range []ChainHead{checkpoint.PayoutParent, checkpoint.PayoutHead} {
			if seen[head] {
				continue
			}
			reward, err := readFinalNativeRewardAtV2(ctx, native, head, release.Netuid, runtime)
			if err != nil {
				return nil, err
			}
			raw, err := json.Marshal(reward)
			if err != nil {
				return nil, err
			}
			if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "native-reward", Name: fmt.Sprintf("native-%020d", head.Number)}, raw); err != nil {
				return nil, err
			}
			evmHash, err := chain.BlockHashContext(ctx, head.Number)
			if err != nil {
				return nil, err
			}
			nativeHash, err := gsrpctypes.NewHashFromHexString(head.Hash)
			if err != nil {
				return nil, err
			}
			mapping, err := crv4.ReadEVMCheckpointAtContext(ctx, native, crv4.EVMCheckpointQuery{GenesisHash: native.GenesisHash, NativeHash: nativeHash, NativeNumber: head.Number, EVMHash: gsrpctypes.Hash(evmHash), EVMNumber: head.Number}, runtime)
			if err != nil {
				return nil, err
			}
			raw, err = json.Marshal(mapping)
			if err != nil {
				return nil, err
			}
			if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "native-reward-mapping", Name: fmt.Sprintf("native-%020d", head.Number)}, raw); err != nil {
				return nil, err
			}
			seen[head] = true
		}
	}
	return result, nil
}

func finalNativeRewardHeadsV2(validators []FinalCollectedValidatorInputs) ([]ChainHead, error) {
	byNumber := map[uint64]ChainHead{}
	for _, validator := range validators {
		if validator.EvidenceV2 == nil || len(validator.EvidenceV2.NativeCheckpoints) < 2 {
			return nil, errors.New("native reward capture mixes source generations")
		}
		for _, checkpoint := range validator.EvidenceV2.NativeCheckpoints[1:] {
			for _, head := range []ChainHead{checkpoint.PayoutParent, checkpoint.PayoutHead} {
				if prior, ok := byNumber[head.Number]; ok && prior != head {
					return nil, errors.New("native reward capture has conflicting payout boundaries")
				}
				byNumber[head.Number] = head
			}
		}
	}
	result := make([]ChainHead, 0, len(byNumber))
	for _, head := range byNumber {
		result = append(result, head)
	}
	slices.SortFunc(result, func(a, b ChainHead) int {
		if a.Number < b.Number {
			return -1
		}
		if a.Number > b.Number {
			return 1
		}
		return 0
	})
	return result, nil
}

// Native application starts at the paired canonical reveal/WeightsSet event.
// ApplicationBlock is a later observation receipt and cannot move that event.
func selectFinalCoverageIntentsV2(intents []validatorpkg.ReleaseEvidenceV2CapturedIntent, checkpoints []FinalNativeCheckpointV2) (map[uint64]bool, error) {
	if len(checkpoints) < 2 {
		return nil, errors.New("native coverage lacks independent window endpoints")
	}
	first, last := finalNativeCoverageFirstBlockV2(checkpoints), checkpoints[len(checkpoints)-1].Mapping.Query.NativeNumber
	var applied []validatorpkg.ReleaseEvidenceV2CapturedIntent
	for _, item := range intents {
		if item.Intent.Status == "applied" {
			applied = append(applied, item)
		}
	}
	selected := map[uint64]bool{}
	for index, item := range applied {
		from, to := item.Intent.RevealBlock, last
		if index+1 < len(applied) {
			if applied[index+1].Intent.RevealBlock <= from {
				return nil, errors.New("native coverage application events are not strictly ordered")
			}
			to = applied[index+1].Intent.RevealBlock - 1
		}
		if from <= last && to >= first {
			selected[item.Sequence] = true
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("native coverage has no actual overlapping application interval")
	}
	for _, checkpoint := range checkpoints {
		matches := 0
		for index, item := range applied {
			if !selected[item.Sequence] || item.Intent.RevealBlock > checkpoint.Mapping.Query.NativeNumber || index+1 < len(applied) && applied[index+1].Intent.RevealBlock <= checkpoint.Mapping.Query.NativeNumber {
				continue
			}
			if !slices.Equal(item.Intent.UIDs, checkpoint.Weights.UIDs) || !slices.Equal(item.Intent.Values, checkpoint.Weights.Values) {
				return nil, errors.New("native coverage checkpoint differs from its actual application interval")
			}
			matches++
		}
		if matches != 1 {
			return nil, errors.New("native coverage checkpoint has missing or conflicting application intervals")
		}
	}
	return selected, nil
}

func readFinalNativeIntentReferencesV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, validatorID int) ([]validatorpkg.SteeringIntent, error) {
	if !finalUsesEvidenceV2(cfg) {
		return readValidatorIntentFile(stateRoot, validatorID)
	}
	release, _, _, err := finalReleaseCaptureConfigWithAdoptionV2(ctx, cfg, stateRoot, uint64(validatorID))
	if err != nil {
		return nil, err
	}
	raw, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(release.StateDir, "steering-intents.json"), release.EvidenceV2.Bounds.IntentFileLimit())
	if err != nil {
		return nil, err
	}
	return decodeValidatorIntentBytes(raw, validatorID)
}
