//go:build linux || darwin

// Stopped recovery reads the two authenticated retained replica namespaces.
// Both transports share every publication, signature, census and payload check.
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

// The ordinary reader always uses both public HTTP origins. Stopped recovery
// explicitly supplies both original retained stores, including uncached suffixes.
func ReadRetainedValidatorEvidencePublicationV2(ctx context.Context, manifest *ValidatorEvidencePublicationV2Manifest, options ValidatorEvidencePublicationV2ReadOptions, replicas [2]ValidatorEvidenceRetainedReplicaV2) (*ValidatorEvidenceCensusV2Publication, error) {
	return readValidatorEvidencePublicationV2(ctx, manifest, options, &replicas)
}

func ReadRetainedValidatorEvidenceDepositAuditV2(ctx context.Context, manifest *ValidatorEvidenceDepositAuditV2Manifest, options ValidatorEvidencePublicationV2ReadOptions, replicas [2]ValidatorEvidenceRetainedReplicaV2) (*ValidatorEvidenceCensusV2Publication, error) {
	return readValidatorEvidenceDepositAuditV2(ctx, manifest, options, &replicas)
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

func validatorEvidencePublicationV2Readers(options ValidatorEvidencePublicationV2ReadOptions, maximum uint64, retained *[2]ValidatorEvidenceRetainedReplicaV2) ([2]validatorEvidenceV2MetadataReader, error) {
	var result [2]validatorEvidenceV2MetadataReader
	// Construction preserves the original distinct-origin and finite-bound
	// admission. It performs no HTTP request when retained stores are selected.
	public, err := newReleaseEvidenceV2ReadersWithMetadataLimit(options.Origins, options.Bounds.Cut, maximum)
	if err != nil {
		return result, err
	}
	for index := range result {
		if retained == nil {
			result[index] = public[index]
			continue
		}
		replica := retained[index]
		if replica.Origin != options.Origins[index] || replica.ReadMetadata == nil {
			return [2]validatorEvidenceV2MetadataReader{}, errors.New("retained publication requires both exact configured replica origins")
		}
		result[index] = validatorEvidenceRetainedMetadataReaderV2{read: replica.ReadMetadata, maximum: maximum}
	}
	return result, nil
}
