//go:build linux || darwin

package main

import (
	"context"

	"github.com/urfoundation/sn/crv4"
)

// One observation keeps the independent native/EVM identities, the runtime
// proof of their execution mapping, actual schedule and complete native row.
// It contains observations only; original signed intent and coverage checks
// remain the responsibility of the containing final verifier.
type FinalNativeCheckpointV2 struct {
	Mapping            crv4.EVMCheckpointObservation     `json:"mapping"`
	Identity           crv4.ValidatorScheduleObservation `json:"identity"`
	Schedule           crv4.EpochScheduleState           `json:"schedule"`
	RevealPeriodEpochs uint64                            `json:"reveal_period_epochs"`
	PayoutHead         ChainHead                         `json:"payout_head"`
	PayoutParent       ChainHead                         `json:"payout_parent"`
	Weights            FinalNativeWeightState            `json:"weights"`
}

type finalNativeCheckpointReaderV2 interface {
	NativeCheckpointV2(context.Context, *FinalSemanticEvidence, ChainHead, uint16, string) (FinalNativeCheckpointV2, []FinalRPCExchange, error)
}
