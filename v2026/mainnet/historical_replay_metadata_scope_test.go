// Synthetic transcripts exercise scope separation without granting a runtime,
// metadata source, or fee policy independent execution authority.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func nativeMetadataScopeTestTrace(t *testing.T, profile *historicalReplayObservationProfile) (historicalReplayJob, *historicalReplayObservations) {
	t.Helper()
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	job := historicalReplayJob{RuntimeCodeSha256: profile.RuntimeCodeSha256, ObservationProfile: profile}
	trace := &historicalReplayObservations{ProfileSha256: historicalReplayDigest(sha256.Sum256(raw)), SourceReviewSha256: profile.SourceReviewSha256, Authority: "caller-supplied-unapproved-callsite-profile", OriginalFunctionBodiesPreserved: true, Observations: []historicalReplayObservation{}}
	return job, trace
}

func TestHistoricalNativeMetadataPinDoesNotSelectFeeCandidates(t *testing.T) {
	pin := historicalReplayDigest{17}
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema, RuntimeCodeSha256: historicalReplayDigest{3}, SourceReviewSha256: historicalReplayDigest{5}, MetadataSha256: &pin, Rules: []historicalReplayHookRule{{Purpose: "native-epoch", FunctionIndex: 1, FunctionBodySha256: historicalReplayDigest{7}, OffsetStart: 0, OffsetEnd: 4}}}
	job, trace := nativeMetadataScopeTestTrace(t, profile)
	if err := profile.validate(job); err != nil {
		t.Fatal("native metadata-only profile refused", err)
	}
	if err := validateHistoricalReplayObservations(job, trace); err != nil || trace.FeeEvents != nil {
		t.Fatal("metadata pin required unrelated fee candidates", trace, err)
	}
	trace.FeeEvents = &historicalReplayFeeEvents{MetadataSha256: pin, Authority: "original-runtime-metadata-and-unapproved-callsite-profile"}
	if err := validateHistoricalReplayObservations(job, trace); err == nil || !strings.Contains(err.Error(), "explicit metadata-pinned fee callsites") {
		t.Fatal("fee report borrowed a native metadata pin", err)
	}
}

func TestHistoricalMetadataFeeCandidatesRemainExplicitAndPinned(t *testing.T) {
	pin := historicalReplayDigest{17}
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema, RuntimeCodeSha256: historicalReplayDigest{3}, SourceReviewSha256: historicalReplayDigest{5}, MetadataSha256: &pin, Rules: []historicalReplayHookRule{{Purpose: "fee-withdraw", FunctionIndex: 1, FunctionBodySha256: historicalReplayDigest{7}, OffsetStart: 0, OffsetEnd: 4}}}
	job, trace := nativeMetadataScopeTestTrace(t, profile)
	if err := validateHistoricalReplayObservations(job, trace); err == nil {
		t.Fatal("selected fee metadata lost its mandatory candidate report")
	}
	trace.FeeEvents = &historicalReplayFeeEvents{MetadataSha256: pin, Authority: "original-runtime-metadata-and-unapproved-callsite-profile"}
	if err := validateHistoricalReplayObservations(job, trace); err != nil {
		t.Fatal("quiet selected fee scope refused", err)
	}
	trace.FeeEvents.MetadataSha256[0] ^= 1
	if err := validateHistoricalReplayObservations(job, trace); err == nil {
		t.Fatal("fee candidate metadata substitution admitted")
	}
	profile.MetadataSha256 = nil
	job, trace = nativeMetadataScopeTestTrace(t, profile)
	if err := validateHistoricalReplayObservations(job, trace); err != nil {
		t.Fatal("legacy raw observation required fee decoding", err)
	}
	raw, err := json.Marshal(profile)
	if err != nil || strings.Contains(string(raw), "metadata_sha256") {
		t.Fatal("historical nil metadata wire changed", string(raw), err)
	}
}

func TestNativeExecutionMetadataScopePreservesFeeAuthority(t *testing.T) {
	for _, item := range []struct {
		name, purpose string
		pin, fees     bool
		refuse        bool
	}{
		{"legacy-native", "native-epoch", false, false, false},
		{"pinned-native", "native-epoch", true, false, false},
		{"pinned-yuma", "native-yuma-meta", true, false, false},
		{"fee-no-pin", "fee-withdraw", false, true, true},
		{"fee-with-pin", "fee-withdraw", true, true, false},
		{"unapproved-fee", "fee-withdraw", true, false, true},
		{"unapproved-quiet-fee", "native-fee-exempt", false, false, true},
	} {
		profile := &historicalReplayObservationProfile{Rules: []historicalReplayHookRule{{Purpose: item.purpose}}}
		if item.pin {
			pin := historicalReplayDigest{17}
			profile.MetadataSha256 = &pin
		}
		var fees *nativeFeeCensusPolicy
		if item.fees {
			fees = &nativeFeeCensusPolicy{}
		}
		if err := validateNativeExecutionMetadataScope(profile, fees); (err != nil) != item.refuse {
			t.Fatal("metadata scope changed fee authority", item.name, err)
		}
	}
}

func TestNativeExecutionMetadataPinPreservesQuietNativeAdmission(t *testing.T) {
	pin := historicalReplayDigest{17}
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema, RuntimeCodeSha256: historicalReplayDigest{3}, SourceReviewSha256: historicalReplayDigest{5}, MetadataSha256: &pin, Rules: []historicalReplayHookRule{{Purpose: "native-epoch", FunctionIndex: 1, FunctionBodySha256: historicalReplayDigest{7}, OffsetStart: 0, OffsetEnd: 4}}}
	job, trace := nativeMetadataScopeTestTrace(t, profile)
	job.ParentHash, job.ChildHash, job.RuntimeCodeBlake2b256 = historicalReplayDigest{11}, historicalReplayDigest{13}, historicalReplayDigest{19}
	jobHash := historicalReplayDigest{23}
	report := historicalReplayReport{PostStateReproduced: true, JobSha256: jobHash, ParentHash: job.ParentHash, ChildHash: job.ChildHash, RuntimeCodeSha256: job.RuntimeCodeSha256, HookObservations: trace}
	admission := nativeExecutionAdmission{ProfileSha256: "sha256:" + hex.EncodeToString(trace.ProfileSha256[:]), ReviewSha256: "sha256:" + hex.EncodeToString(profile.SourceReviewSha256[:]), Job: planFileReference{Sha256: "sha256:" + hex.EncodeToString(jobHash[:])}, Parent: economicEmissionBoundary{Number: 100, Hash: nativeExecutionTestHex(job.ParentHash[:])}, Child: economicEmissionBoundary{Number: 101, Hash: nativeExecutionTestHex(job.ChildHash[:])}}
	admission.Runtime.RuntimeCodeHash = nativeExecutionTestHex(job.RuntimeCodeBlake2b256[:])
	block := economicEmissionBlock{Boundary: admission.Child}
	outcome, err := deriveNativeExecution(economicEmissionPolicy{Netuid: 25}, admission, block, job, report, [3]nativeExecutionDrain{}, nil)
	if err != nil || outcome == nil || !outcome.AmountsAuthenticated || outcome.FeeCensus != nil || outcome.Yuma != nil || outcome.MinerAllocation != "0" {
		t.Fatal("native decoder treated metadata pin as optional authority selection", outcome, err)
	}
}
