//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// The public pass cannot obtain original history from mutable runtime paths.
// Only the combined artifact/public verifier installs this closed loader.
type finalValidatorReplayLoaderContextV2 struct{}

type finalValidatorSourcesReaderV2 interface {
	ValidatorSourcesV2(context.Context, *FinalSemanticEvidence, *validatorpkg.ReleaseEvidenceV2Archive) ([]validatorpkg.ReleaseEvidenceV2DecisionObservation, []FinalRPCExchange, error)
}

func finalValidatorReplayContextV2(ctx context.Context, load FinalArtifactLoader) context.Context {
	return context.WithValue(ctx, finalValidatorReplayLoaderContextV2{}, load)
}

func verifyFinalValidatorSourcesOnChainV2(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) (resultErr error) {
	if len(evidence.ValidatorReplayV2) == 0 {
		return nil
	}
	load, ok := ctx.Value(finalValidatorReplayLoaderContextV2{}).(FinalArtifactLoader)
	if !ok || load == nil {
		return errors.New("public V2 verification requires the complete closed artifact loader")
	}
	checkpoints, ok := reader.(finalNativeCheckpointReaderV2)
	if !ok {
		return errors.New("public V2 reader lacks actual native checkpoint verification")
	}
	sources, ok := reader.(finalValidatorSourcesReaderV2)
	if !ok {
		return errors.New("public V2 reader lacks actual historical source and publication verification")
	}
	owners, err := openFinalValidatorReplayOwnersV2(ctx, evidence, load)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeFinalValidatorReplayOwnersV2(owners), ctx.Err()) }()
	for _, entry := range evidence.ValidatorReplayV2 {
		owner := owners[entry.ValidatorID]
		observations, exchanges, err := sources.ValidatorSourcesV2(ctx, evidence, owner.archive)
		if err != nil {
			return fmt.Errorf("public validator %d original V2 sources: %w", entry.ValidatorID, err)
		}
		if !finalJSONEqual(observations, owner.observations) {
			return fmt.Errorf("validator %d captured decision sources differ from independent historical reads", entry.ValidatorID)
		}
		for _, original := range owner.coverage.Checkpoints {
			at := ChainHead{Number: original.Mapping.Query.EVMNumber, Hash: original.Mapping.Query.EVMHash.Hex()}
			observed, reads, err := checkpoints.NativeCheckpointV2(ctx, evidence, at, original.Weights.ValidatorUID, owner.manifest.Capture.Hotkey)
			if err != nil {
				return fmt.Errorf("public validator %d native checkpoint: %w", entry.ValidatorID, err)
			}
			if !finalNativeCheckpointEqualV2(original, observed) {
				return fmt.Errorf("validator %d captured native checkpoint differs from independent historical reads", entry.ValidatorID)
			}
			if len(reads) == 0 {
				return errors.New("public native checkpoint has no original RPC transcript")
			}
			exchanges = append(exchanges, reads...)
		}
		for _, original := range owner.coverage.RewardMappings {
			at := ChainHead{Number: original.Query.EVMNumber, Hash: original.Query.EVMHash.Hex()}
			observed, reads, err := checkpoints.NativeCheckpointV2(ctx, evidence, at, owner.manifest.Capture.NativeCheckpoints[0].Weights.ValidatorUID, owner.manifest.Capture.Hotkey)
			if err != nil || observed.Mapping != original {
				return errors.Join(errors.New("native payout/owner-stake mapping differs from independent runtime proof"), err)
			}
			if len(reads) == 0 {
				return errors.New("native payout mapping has no actual RPC transcript")
			}
			exchanges = append(exchanges, reads...)
		}
		if len(exchanges) == 0 {
			return errors.New("public V2 source verifier returned no original historical transcript")
		}
		for _, exchange := range exchanges {
			// Each source has its own immutable native/EVM head. The terminal
			// cannot relabel an older metadata, decision or activation read.
			if err := appendExchanges(exchange.Chain, exchange.PinnedHead, []FinalRPCExchange{exchange}); err != nil {
				return err
			}
		}
	}
	return nil
}
