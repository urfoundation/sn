package chain

import (
	"context"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
)

// FinalizedRuntime is one authenticated finalized checkpoint: the hash and
// height plus the exact reviewed artifact observed there.
type FinalizedRuntime struct {
	Hash     types.Hash
	Number   uint64
	Artifact crv4.AuthenticatedRuntimeArtifact
}

// AuthenticateFinalizedRuntimeContext reads the canonical finalized head,
// authenticates the complete runtime identity there against the caller's
// allowed artifacts (crv4/runtime_identity.go), and returns a bound view of
// the chain whose metadata and signing versions come from that artifact. The
// shared connection is never mutated, so concurrent readers keep their view.
func AuthenticateFinalizedRuntimeContext(ctx context.Context, chain *crv4.Chain, allowed ...crv4.RuntimeArtifactIdentity) (*crv4.Chain, FinalizedRuntime, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || len(allowed) == 0 {
		return nil, FinalizedRuntime{}, errors.New("finalized runtime authentication dependencies are unavailable")
	}
	finalized, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return nil, FinalizedRuntime{}, err
	}
	header, err := chain.HeaderAtContext(ctx, finalized)
	if err != nil {
		return nil, FinalizedRuntime{}, err
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, chain, finalized, allowed...)
	if err != nil {
		return nil, FinalizedRuntime{}, fmt.Errorf("finalized runtime at %s is not a pinned artifact: %w", finalized.Hex(), err)
	}
	if artifact.CompatibilityProfile != "" && !chain.RuntimeArtifactCompatible(artifact) {
		return nil, FinalizedRuntime{}, errors.New("finalized runtime compatibility lacks explicit authority")
	}
	bound := *chain
	bound.Meta = artifact.Metadata
	bound.Runtime = &types.RuntimeVersion{
		SpecName:           artifact.Version.SpecName,
		SpecVersion:        types.U32(artifact.Version.SpecVersion),
		TransactionVersion: types.U32(artifact.Version.TransactionVersion),
	}
	return &bound, FinalizedRuntime{Hash: finalized, Number: uint64(header.Number), Artifact: artifact}, nil
}

// RequireSameRuntime refuses to sign under one artifact and broadcast under
// another; interface compatibility does not preserve a signature's domain.
func RequireSameRuntime(prepared, current FinalizedRuntime) error {
	if prepared.Artifact.Version != current.Artifact.Version || prepared.Artifact.CodeHash != current.Artifact.CodeHash || prepared.Artifact.MetadataHash != current.Artifact.MetadataHash {
		return errors.New("native runtime changed between signing and broadcast; re-run against the new artifact")
	}
	return nil
}
