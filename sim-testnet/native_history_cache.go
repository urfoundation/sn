// Native inclusion proofs survive compatible recovery builds. The checkpoint
// and observer remain exact, and the reviewed runtime catalogue is part of the
// proof; provisional or opaque older proofs cannot acquire strict authority.
package main

import (
	"errors"
)

const historicalNativeExtrinsicCacheKind = "finalized-native-extrinsic"

// The extra runtime binding deliberately gives old opaque proofs a different
// input hash. Their original context cannot establish strict runtime admission.
type historicalNativeExtrinsicCacheInput struct {
	Recorded             ChainHead `json:"recorded"`
	Transaction          string    `json:"transaction"`
	Observer             string    `json:"observer"`
	RuntimeArtifactsHash string    `json:"runtime_artifacts_hash"`
}

// Only strict, reviewed historical runtime verification may issue reusable
// native proofs. The exact admission catalogue also invalidates removed pins.
func nativeHistoryCacheInput(cfg *ResolvedConfig, recorded ChainHead, transaction, observer string) (historicalNativeExtrinsicCacheInput, error) {
	input := historicalNativeExtrinsicCacheInput{Recorded: recorded, Transaction: transaction, Observer: observer}
	if cfg == nil || cfg.Public == nil || cfg.Release == nil || provisionalResumeEnabled(cfg) ||
		!validConformanceTransaction(transaction, recorded.Hash, recorded.Number) || observer == "" {
		return input, errors.New("strict native history proof identity is unavailable")
	}
	artifacts, err := releaseHistoryRuntimeArtifacts(cfg)
	if err != nil {
		return input, err
	}
	input.RuntimeArtifactsHash, err = canonicalHashHex(artifacts)
	return input, err
}
