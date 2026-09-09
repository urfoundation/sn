//go:build linux || darwin

// Native replicated sealing uses the runtime's existing authenticated upload
// owners and complete configured census; portable session ownership is separate.
package validator

import (
	"context"
	"errors"
	"fmt"
)

// The complete configured runtime census is checked before returning either
// writer. Public origins select two existing authenticated sessions, never a
// new login, credential file, artifact signing key or raw object-store client.
// Inputs must not be mutated during construction; returned routing owns values.
func releaseAttemptUploadReplicasV2(cfg *ReleaseConfig, origins [2]string, runtimes []*releaseOperatorRuntime) ([2]AttemptCutV2Replica, error) {
	var zero [2]AttemptCutV2Replica
	if cfg == nil || len(cfg.Operators) < 2 || len(runtimes) != len(cfg.Operators) ||
		uint64(len(runtimes)) > cfg.EvidenceV2.Bounds.MaxOperators || uint64(len(runtimes)) > cfg.EvidenceV2.Bounds.MaxParticipants {
		return zero, errors.New("release attempt upload runtime census is incomplete or exceeds its bounds")
	}
	configured := make(map[uint64]OperatorConfig, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		if operator.NoID == 0 || configured[operator.NoID].NoID != 0 {
			return zero, errors.New("release attempt upload configured operator is zero or duplicated")
		}
		configured[operator.NoID] = operator
	}
	seen := make(map[uint64]bool, len(runtimes))
	var selected [2]*releaseAttemptUploadV2
	for _, runtime := range runtimes {
		if runtime == nil || runtime.measurement == nil || runtime.attemptUpload == nil {
			return zero, errors.New("release attempt upload runtime owner is missing")
		}
		owner := runtime.attemptUpload
		operator, found := configured[owner.noID]
		if !found || seen[owner.noID] || runtime.measurement.NoID != owner.noID || owner.origin != operator.APIURL || owner.bounds != cfg.EvidenceV2.Bounds.Cut || owner.ctx == nil || owner.cancel == nil || owner.writer == nil {
			return zero, errors.New("release attempt upload runtime differs from its configured operator")
		}
		if err := owner.ctx.Err(); err != nil {
			return zero, err
		}
		// Check the actual attached writer, not only its owner's markers. This
		// constructs no request and never calls the live credential getter.
		expected, err := NewHTTPAttemptStreamV2Writer(operator.APIURL, owner.bounds, owner.writer.byJwt)
		if err != nil {
			return zero, err
		}
		writer := owner.writer
		if owner.bounds.MaxHeaderBytes == 0 || owner.bounds.MaxHeaderBytes > expected.metadataBytes ||
			writer.endpoint != expected.endpoint || writer.metadataBytes != expected.metadataBytes ||
			writer.recordBytes != expected.recordBytes || writer.proofBytes != expected.proofBytes ||
			writer.client == nil || writer.client.Timeout != expected.client.Timeout || writer.client.CheckRedirect == nil ||
			writer.client.Transport != nil || writer.client.Jar != nil {
			return zero, errors.New("release attempt upload concrete writer differs from its admitted route or bounds")
		}
		// Function identity cannot prove credential ownership. The private
		// constructor still binds the actual SDK session and redirect refusal.
		seen[owner.noID] = true
		for index, origin := range origins {
			if origin == owner.origin {
				if selected[index] != nil {
					return zero, errors.New("release attempt upload public origin has competing runtime owners")
				}
				selected[index] = owner
			}
		}
	}
	var replicas [2]AttemptCutV2Replica
	for index, owner := range selected {
		if owner == nil {
			return zero, fmt.Errorf("release attempt upload public origin %d has no authenticated runtime", index+1)
		}
		writer := func(kind string) AttemptStreamV2ObjectWriter {
			return func(ctx context.Context, hash string, raw []byte) error { return owner.write(ctx, kind, hash, raw) }
		}
		replicas[index] = AttemptCutV2Replica{Origin: owner.origin, WriteRecords: writer(AttemptStreamV2Records), WriteProofs: writer(AttemptStreamV2Proofs), WriteMetadata: writer("metadata")}
	}
	// Reuse the exact public reader's alias, header and stream-bound admission.
	if _, err := newAttemptCutV2Replicas(cfg.EvidenceV2.Bounds.Cut, replicas); err != nil {
		return zero, err
	}
	return replicas, nil
}
