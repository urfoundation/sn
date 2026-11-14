//go:build linux || darwin

// Stopped recovery reads every authenticated retained replica namespace. Both
// transports share every publication, signature, census and payload check.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
)

// The caller authenticates each retained store against its original rendered
// origin before supplying read-only bytes. A replica never supplies authority.
type ValidatorEvidenceRetainedReplicaV2 struct {
	Origin       string
	ReadMetadata AttemptStreamV2MetadataReader
}

// The ordinary reader always uses every public HTTP origin. Stopped recovery
// explicitly supplies every original retained store, in configured origin
// order, including uncached suffixes.
func ReadRetainedValidatorEvidencePublicationV2(ctx context.Context, manifest *ValidatorEvidencePublicationV2Manifest, options ValidatorEvidencePublicationV2ReadOptions, replicas []ValidatorEvidenceRetainedReplicaV2) (*ValidatorEvidenceCensusV2Publication, error) {
	if replicas == nil {
		return nil, errors.New("retained publication requires every exact configured replica origin")
	}
	return readValidatorEvidencePublicationV2(ctx, manifest, options, replicas)
}

func ReadRetainedValidatorEvidenceDepositAuditV2(ctx context.Context, manifest *ValidatorEvidenceDepositAuditV2Manifest, options ValidatorEvidencePublicationV2ReadOptions, replicas []ValidatorEvidenceRetainedReplicaV2) (*ValidatorEvidenceCensusV2Publication, error) {
	if replicas == nil {
		return nil, errors.New("retained publication requires every exact configured replica origin")
	}
	return readValidatorEvidenceDepositAuditV2(ctx, manifest, options, replicas)
}

type validatorEvidenceRetainedMetadataReaderV2 struct {
	read    AttemptStreamV2MetadataReader
	maximum uint64
}

// Match the HTTP reader's exact-size/hash and cancellation contract before
// allowing retained bytes into the shared canonical publication decoder.
func (self validatorEvidenceRetainedMetadataReaderV2) ReadMetadata(ctx context.Context, hash string, size uint64) ([]byte, error) {
	if ctx == nil || self.read == nil || size == 0 || size > self.maximum {
		return nil, errors.New("retained publication metadata owner or bound is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected, err := canonicalAttemptHex32("retained publication content hash", hash, false)
	if err != nil {
		return nil, err
	}
	raw, err := self.read(ctx, hash, size)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	if uint64(len(raw)) != size || sha256.Sum256(raw) != expected {
		return nil, errors.New("retained publication metadata differs from its exact size or hash")
	}
	return raw, nil
}

// A nil retained census selects the public HTTP origins; otherwise it must
// name exactly one retained store for every configured origin, in order.
func validatorEvidencePublicationV2Readers(options ValidatorEvidencePublicationV2ReadOptions, maximum uint64, retained []ValidatorEvidenceRetainedReplicaV2) ([]validatorEvidenceV2MetadataReader, error) {
	// Construction preserves the original distinct-origin and finite-bound
	// admission. It performs no HTTP request when retained stores are selected.
	public, err := newReleaseEvidenceV2ReadersWithMetadataLimit(options.Origins, options.Bounds.Cut, maximum)
	if err != nil {
		return nil, err
	}
	if retained != nil && len(retained) != len(public) {
		return nil, errors.New("retained publication requires every exact configured replica origin")
	}
	result := make([]validatorEvidenceV2MetadataReader, len(public))
	for index := range result {
		if retained == nil {
			result[index] = public[index]
			continue
		}
		replica := retained[index]
		if replica.Origin != options.Origins[index] || replica.ReadMetadata == nil {
			return nil, errors.New("retained publication requires every exact configured replica origin")
		}
		result[index] = validatorEvidenceRetainedMetadataReaderV2{read: replica.ReadMetadata, maximum: maximum}
	}
	return result, nil
}
