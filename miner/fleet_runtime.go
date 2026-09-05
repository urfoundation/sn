package miner

// fleet_runtime.go binds every release fleet publish and status read to the
// exact node-subtensor v454 artifact. The fleet CLI has no release-lock input,
// so this immutable tuple is deliberately local and covered against that lock
// by fleet_runtime_test.go.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
)

const (
	fleetReleaseRuntimeSpecName           = "node-subtensor"
	fleetReleaseRuntimeSpecVersion        = uint32(454)
	fleetReleaseRuntimeTransactionVersion = uint32(1)
	fleetReleaseRuntimeStateVersion       = uint8(1)
	fleetReleaseRuntimeCodeHash           = "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef"
	fleetReleaseRuntimeMetadataHash       = "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702"
	fleetReleaseExpectedBlockSeconds      = 12
	fleetNativeAuthenticationBlockBudget  = 10
	fleetNativeEndpointTimeout            = time.Duration(fleetReleaseExpectedBlockSeconds*fleetNativeAuthenticationBlockBudget) * time.Second
	fleetStatusTimeout                    = 10 * time.Minute
)

// Selects the sole runtime artifact permitted for current fleet operations.
func fleetReleaseRuntimeArtifact() crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{
		Version: crv4.RuntimeVersionIdentity{
			SpecName:           fleetReleaseRuntimeSpecName,
			SpecVersion:        fleetReleaseRuntimeSpecVersion,
			TransactionVersion: fleetReleaseRuntimeTransactionVersion,
			StateVersion:       fleetReleaseRuntimeStateVersion,
		},
		CodeHash:     fleetReleaseRuntimeCodeHash,
		MetadataHash: fleetReleaseRuntimeMetadataHash,
	}
}

// Authenticates one caller-selected finalized block before metadata is used to
// construct a fleet call or decode a fleet commitment. Historical artifacts
// are intentionally absent: fleet publish and status are current operations.
func authenticateFleetRuntimeAtContext(ctx context.Context, chain *crv4.Chain, finalized types.Hash) (crv4.AuthenticatedRuntimeArtifact, error) {
	if ctx == nil || chain == nil || finalized == (types.Hash{}) {
		return crv4.AuthenticatedRuntimeArtifact{}, errors.New("fleet runtime authentication context is incomplete")
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, chain, finalized, fleetReleaseRuntimeArtifact())
	if err != nil {
		return crv4.AuthenticatedRuntimeArtifact{}, fmt.Errorf("fleet runtime at %s is not the reviewed node-subtensor/454/1/1 artifact: %w", finalized.Hex(), err)
	}
	return artifact, nil
}

// Replaces the dial-time metadata only after all four immutable identity
// dimensions have been authenticated. Callers own and serialize their Chain.
func bindFleetRuntime(chain *crv4.Chain, artifact crv4.AuthenticatedRuntimeArtifact) error {
	expected := fleetReleaseRuntimeArtifact()
	if chain == nil || artifact.BlockHash == (types.Hash{}) || artifact.Metadata == nil ||
		artifact.Version != expected.Version ||
		!strings.EqualFold(artifact.CodeHash, expected.CodeHash) ||
		!strings.EqualFold(artifact.MetadataHash, expected.MetadataHash) {
		return errors.New("refusing to bind an unreviewed fleet runtime artifact")
	}
	chain.Meta = artifact.Metadata
	chain.Runtime = &types.RuntimeVersion{
		SpecName:           artifact.Version.SpecName,
		SpecVersion:        types.U32(artifact.Version.SpecVersion),
		TransactionVersion: types.U32(artifact.Version.TransactionVersion),
	}
	return nil
}

// Authenticates and binds metadata for one exact block without leaving a
// partially updated Chain behind on an identity mismatch.
func authenticateAndBindFleetRuntimeAtContext(ctx context.Context, chain *crv4.Chain, finalized types.Hash) error {
	artifact, err := authenticateFleetRuntimeAtContext(ctx, chain, finalized)
	if err != nil {
		return err
	}
	return bindFleetRuntime(chain, artifact)
}

// Reads the current finalized head, then authenticates the exact artifact at
// that head before a caller signs or decodes any metadata-dependent value.
func authenticateAndBindFleetRuntimeFinalizedContext(ctx context.Context, chain *crv4.Chain) (types.Hash, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil {
		return types.Hash{}, errors.New("fleet finalized runtime chain is unavailable")
	}
	finalized, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return types.Hash{}, fmt.Errorf("fleet finalized head: %w", err)
	}
	if err := authenticateAndBindFleetRuntimeAtContext(ctx, chain, finalized); err != nil {
		return types.Hash{}, err
	}
	return finalized, nil
}

// Reads a current fleet commitment only through metadata authenticated for its
// same finalized state root.
func pinnedFleetCommitmentFinalizedContext(ctx context.Context, chain *crv4.Chain, netuid uint16, hotkey [32]byte) (*crv4.FinalizedCommitment, error) {
	finalized, err := authenticateAndBindFleetRuntimeFinalizedContext(ctx, chain)
	if err != nil {
		return nil, err
	}
	return chain.FleetCommitmentAtContext(ctx, netuid, hotkey, finalized)
}

// Re-reads the write postcondition using metadata authenticated at the receipt
// block. A runtime upgrade after the pre-signing gate is therefore a hard
// failure rather than a successful decode under stale metadata.
func verifyPinnedFleetCommitmentWriteContext(ctx context.Context, chain *crv4.Chain, netuid uint16, hotkey, expected [32]byte, receipt *crv4.FinalizedCommitment) (*crv4.FinalizedCommitment, error) {
	if receipt == nil || receipt.FinalizedHash == (types.Hash{}) || receipt.FinalizedAt == 0 {
		return nil, errors.New("fleet commitment receipt is incomplete")
	}
	if err := authenticateAndBindFleetRuntimeAtContext(ctx, chain, receipt.FinalizedHash); err != nil {
		return nil, err
	}
	observed, err := chain.FleetCommitmentAtContext(ctx, netuid, hotkey, receipt.FinalizedHash)
	if err != nil {
		return nil, err
	}
	if err := crv4.ValidateFleetCommitmentWrite(expected, receipt.FinalizedAt, observed); err != nil {
		return nil, err
	}
	observed.ExtrinsicHash = receipt.ExtrinsicHash
	return observed, nil
}
