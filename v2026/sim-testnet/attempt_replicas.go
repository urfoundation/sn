//go:build linux || darwin

package main

// Typed publication uses the same per-operator server/blob namespaces as the
// supervised APIs. This bridge supplies storage, not validator signing authority.

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/startifact"
)

// Binds two previously authenticated runtime stores to their configured public
// origins. Admission is read-only; all configuration is copied before callbacks
// escape. The caller must not mutate configuration during construction and must
// authenticate the stores against the captured runtime manifest beforehand.
// This does not activate v2, enlarge a service limit, establish retained storage
// protection, or substitute for complete public replay by the replicated sealer.
func newAttemptCutV2StoreReplicas(cfg *ResolvedConfig, stores map[int]server.BlobStore, bounds validator.AttemptCutV2Bounds) ([2]validator.AttemptCutV2Replica, error) {
	var replicas [2]validator.AttemptCutV2Replica
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.ChainID == 0 || cfg.ChainID != cfg.Public.Chain.ChainID ||
		cfg.Config.Topology.Operators != len(replicas) || len(stores) != len(replicas) {
		return replicas, errors.New("attempt replica deployment or store census is incomplete")
	}
	_, origins, err := publicEvidenceTransportForOrigins(cfg.OperatorAPIOrigins, len(replicas), cfg.ChainID, cfg.Public.Chain.GenesisHash, cfg.Config.Deployment.Network)
	if err != nil {
		return replicas, fmt.Errorf("attempt replica public origins: %w", err)
	}
	// Reuse the actual reader's finite stream admission without making a request.
	if _, err := validator.NewHTTPAttemptStreamV2Reader(origins[0], bounds); err != nil {
		return replicas, fmt.Errorf("attempt replica stream bounds: %w", err)
	}
	objectBounds := startifact.AttemptObjectBounds{
		MetadataBytes: max(bounds.Records.MaxManifestBytes, bounds.Records.MaxPageBytes, bounds.Proofs.MaxManifestBytes, bounds.Proofs.MaxPageBytes),
		RecordBytes:   bounds.Records.MaxChunkBytes,
		ProofBytes:    bounds.Proofs.MaxChunkBytes,
	}
	if err := objectBounds.Validate(); err != nil {
		return replicas, err
	}
	if bounds.MaxHeaderBytes == 0 || bounds.MaxHeaderBytes > objectBounds.MetadataBytes {
		return replicas, errors.New("attempt replica header exceeds its public metadata bound")
	}
	var admittedStores [2]server.BlobStore
	for index := range admittedStores {
		operator := index + 1
		prefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			return replicas, err
		}
		store := stores[operator]
		if store == nil || store.Prefix() != prefix {
			return replicas, fmt.Errorf("attempt replica operator %d store namespace differs from deployment", operator)
		}
		admittedStores[index] = store
	}
	for index, store := range admittedStores {
		writer := func(kind string) validator.AttemptStreamV2ObjectWriter {
			return func(ctx context.Context, hash string, raw []byte) error {
				return startifact.PublishAttemptObject(ctx, store, objectBounds, kind, hash, raw)
			}
		}
		replicas[index] = validator.AttemptCutV2Replica{
			Origin: origins[index], WriteRecords: writer(validator.AttemptStreamV2Records),
			WriteProofs: writer(validator.AttemptStreamV2Proofs), WriteMetadata: writer("metadata"),
		}
	}
	return replicas, nil
}
