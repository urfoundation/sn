//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func retainedPublicationV2TestReplicas(origins [2]string, objects [2]map[string][]byte) [2]ValidatorEvidenceRetainedReplicaV2 {
	var result [2]ValidatorEvidenceRetainedReplicaV2
	for index, values := range objects {
		result[index] = ValidatorEvidenceRetainedReplicaV2{Origin: origins[index], ReadMetadata: func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			raw, found := values["metadata/"+hash]
			if !found {
				return nil, errors.New("original replica object is missing")
			}
			return bytes.Clone(raw), ctx.Err()
		}}
	}
	return result
}

// Actual signed terminal bytes remain usable after both public servers stop;
// the ordinary relay still requires its live HTTP readers.
func TestValidatorEvidencePublicationV2RetainedReadsStoppedOrigins(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, true)
	var objects [2]map[string][]byte
	for index, store := range fixture.startup.stores {
		objects[index], _, _ = store.snapshot()
		store.stopHTTP()
	}
	replicas := retainedPublicationV2TestReplicas(fixture.readOptions.Origins, objects)
	actual, err := ReadRetainedValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions, replicas)
	if err != nil || !sameReleaseArchivePublicationV2(actual, fixture.publication) {
		t.Fatal("stopped original replicas lost exact signed publication", err)
	}
	if actual, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions); err == nil || actual != nil {
		t.Fatal("ordinary live relay silently accepted stopped public origins", err)
	}
	for _, member := range fixture.publication.Members {
		for _, hash := range []string{attemptHex32(fixture.manifest.CensusHash), attemptHex32(member.SignedArtifactHash), attemptHex32(member.Evidence.Header.PayloadHash)} {
			key := "metadata/" + hash
			original := objects[1][key]
			delete(objects[1], key)
			if actual, err := ReadRetainedValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions, replicas); err == nil || actual != nil {
				t.Fatal("missing second replica object was replaced by the first", hash, err)
			}
			objects[1][key] = append(bytes.Clone(original), 'x')
			if actual, err := ReadRetainedValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions, replicas); err == nil || actual != nil {
				t.Fatal("changed retained metadata was accepted", hash, err)
			}
			objects[1][key] = original
		}
	}
	changed := replicas
	changed[1].Origin = changed[0].Origin
	if actual, err := ReadRetainedValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions, changed); err == nil || actual != nil {
		t.Fatal("retained reader collapsed original replica identities", err)
	}
	changed = replicas
	changed[1].ReadMetadata = nil
	if actual, err := ReadRetainedValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions, changed); err == nil || actual != nil {
		t.Fatal("retained reader admitted an incomplete replica census", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if actual, err := ReadRetainedValidatorEvidencePublicationV2(ctx, fixture.manifest, fixture.readOptions, replicas); !errors.Is(err, context.Canceled) || actual != nil {
		t.Fatal("canceled retained reader returned authority", err)
	}
}

// Audit payloads and original later subjects use the same stopped-replica path.
func TestValidatorEvidenceDepositAuditV2RetainedReadsStoppedOrigins(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	want, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	var objects [2]map[string][]byte
	for index, store := range fixture.base.stores {
		store.stateLock.Lock()
		objects[index] = make(map[string][]byte, len(store.objects))
		for key, raw := range store.objects {
			objects[index][key] = bytes.Clone(raw)
		}
		store.stateLock.Unlock()
		fixture.stopOrigins[index]()
	}
	replicas := retainedPublicationV2TestReplicas(fixture.options.Origins, objects)
	actual, err := ReadRetainedValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options, replicas)
	if err != nil || !sameReleaseArchivePublicationV2(actual, want) {
		t.Fatal("stopped audit source changed exact signed payloads or calldata", err)
	}
	if actual, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options); err == nil || actual != nil {
		t.Fatal("ordinary audit relay silently accepted stopped public origins", err)
	}
	for _, member := range want.Members {
		key := "metadata/" + attemptHex32(member.Evidence.Header.PayloadHash)
		original := objects[1][key]
		delete(objects[1], key)
		if actual, err := ReadRetainedValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options, replicas); err == nil || actual != nil {
			t.Fatal("stopped audit lost its second original payload replica", err)
		}
		objects[1][key] = original
	}
}
