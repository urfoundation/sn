//go:build linux || darwin

// Real terminal signatures, public replay and immutable publication exercise
// member-specific ownership without substituting accepted verifier results.
package validator

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

func TestValidatorEvidenceCensusV2PreservesEverySourceReplicaOwner(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 1, 1)
	options := fixture.options(t)
	options.Replicas = [2]AttemptCutV2Replica{}
	options.ReplicasByOperator = make(map[uint64][2]AttemptCutV2Replica, len(fixture.operators))
	var mu sync.Mutex
	written := make(map[uint64][2][]string)
	var mutate sync.Once
	for _, operator := range fixture.operators {
		noID := operator.seal.expected.Identity.NoID
		replicas := fixture.replicas
		for index := range replicas {
			write := replicas[index].WriteMetadata
			replicas[index].WriteMetadata = func(ctx context.Context, hash string, raw []byte) error {
				// Every owner must already be copied when the first external
				// publisher is invoked, not looked up again between members.
				mutate.Do(func() { options.ReplicasByOperator[11] = [2]AttemptCutV2Replica{} })
				mu.Lock()
				counts := written[noID]
				counts[index] = append(counts[index], hash)
				written[noID] = counts
				mu.Unlock()
				return write(ctx, hash, raw)
			}
		}
		options.ReplicasByOperator[noID] = replicas
	}
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
	if err != nil || publication == nil || len(publication.Members) != 2 {
		t.Fatalf("real source-owned census publication: %v", err)
	}
	for index, member := range publication.Members {
		want := []string{attemptHex32(member.Evidence.Header.PayloadHash)}
		if index == 0 {
			want = append(want, attemptHex32(publication.CensusHash))
		}
		want = append(want, attemptHex32(member.SignedArtifactHash))
		for replica, hashes := range written[member.Evidence.Header.NoID] {
			if !reflect.DeepEqual(hashes, want) {
				t.Fatalf("source %d replica %d wrote %v, want its own objects %v", member.Evidence.Header.NoID, replica, hashes, want)
			}
		}
	}
}

func TestValidatorEvidenceCensusV2RejectsIncompleteOrMixedSourceReplicasBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	for _, change := range []func(*ValidatorEvidenceCensusV2Options){
		func(options *ValidatorEvidenceCensusV2Options) { delete(options.ReplicasByOperator, 11) },
		func(options *ValidatorEvidenceCensusV2Options) {
			delete(options.ReplicasByOperator, 11)
			options.ReplicasByOperator[12] = fixture.replicas
		},
		func(options *ValidatorEvidenceCensusV2Options) { options.Replicas = fixture.replicas },
		func(options *ValidatorEvidenceCensusV2Options) {
			options.ReplicasByOperator[11] = [2]AttemptCutV2Replica{}
		},
		func(options *ValidatorEvidenceCensusV2Options) {
			options.ReplicasByOperator[11] = [2]AttemptCutV2Replica{fixture.replicas[1], fixture.replicas[0]}
		},
	} {
		options := fixture.options(t)
		options.Replicas = [2]AttemptCutV2Replica{}
		options.ReplicasByOperator = map[uint64][2]AttemptCutV2Replica{9: fixture.replicas, 11: fixture.replicas}
		change(&options)
		before := fixture.counts()
		publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
		if err == nil || publication != nil || fixture.counts() != before {
			t.Fatalf("invalid source census crossed public I/O admission: %v", err)
		}
	}
}
