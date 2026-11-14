package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func hostSnapshotTestObservation() (historicalReplayJob, *historicalReplayObservations) {
	host := "ext_allocator_malloc_version_1"
	profile := &historicalReplayObservationProfile{
		Schema:            historicalNativeProfileSchema,
		RuntimeCodeSha256: historicalReplayDigest{1}, SourceReviewSha256: historicalReplayDigest{2},
		Rules: []historicalReplayHookRule{{Purpose: "native-epoch", FunctionIndex: 17, FunctionBodySha256: historicalReplayDigest{3}, OffsetStart: 5, OffsetEnd: 7, Memory: []historicalNativeCapture{{Name: "netuid", Address: 8, Bytes: 2}}, HostSnapshot: &host}},
	}
	raw, _ := json.Marshal(profile)
	trace := &historicalReplayObservations{
		ProfileSha256: historicalReplayDigest(sha256.Sum256(raw)), SourceReviewSha256: profile.SourceReviewSha256,
		Authority: "caller-supplied-unapproved-callsite-profile", OriginalFunctionBodiesPreserved: true, HostCalls: 1,
		Observations: []historicalReplayObservation{{Ordinal: 1, Purpose: "native-epoch", Operation: "host", KeyHex: "0x" + hex.EncodeToString([]byte(host)), Stack: []historicalReplayFrame{{FunctionIndex: 17, FunctionOffset: 5}}, Native: &historicalNativeObservation{Memory: []historicalNativeMemory{{Name: "netuid", Address: 8, BytesHex: "0x1900"}}}}},
	}
	return historicalReplayJob{RuntimeCodeSha256: profile.RuntimeCodeSha256, ObservationProfile: profile}, trace
}

func TestHistoricalHostSnapshotRetainsExactOptionalWireAndOriginalCallsite(t *testing.T) {
	job, trace := hostSnapshotTestObservation()
	if err := job.ObservationProfile.validate(job); err != nil {
		t.Fatal(err)
	}
	if err := validateHistoricalReplayObservations(job, trace); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(job.ObservationProfile.Rules[0])
	if !bytes.HasSuffix(raw, []byte(`,"host_snapshot":"ext_allocator_malloc_version_1"}`)) {
		t.Fatal("host field order differs", string(raw))
	}
	job.ObservationProfile.Rules[0].HostSnapshot = nil
	raw, _ = json.Marshal(job.ObservationProfile.Rules[0])
	if bytes.Contains(raw, []byte("host_snapshot")) {
		t.Fatal("legacy rule acquired optional field")
	}
}

func TestHistoricalHostSnapshotRefusesStorageRelabelingAndOuterFrames(t *testing.T) {
	for _, mutate := range []func(*historicalReplayObservationProfile, *historicalReplayObservation){
		func(p *historicalReplayObservationProfile, r *historicalReplayObservation) { r.Operation = "get" },
		func(p *historicalReplayObservationProfile, r *historicalReplayObservation) {
			value := "0x01"
			r.Operation = "set"
			r.ValueHex = &value
		},
		func(p *historicalReplayObservationProfile, r *historicalReplayObservation) { r.KeyHex = "0x00" },
		func(p *historicalReplayObservationProfile, r *historicalReplayObservation) {
			r.StorageReturn = &historicalStorageReturn{}
		},
		func(p *historicalReplayObservationProfile, r *historicalReplayObservation) {
			r.Stack = append([]historicalReplayFrame{{FunctionIndex: 18, FunctionOffset: 2}}, r.Stack...)
		},
		func(p *historicalReplayObservationProfile, r *historicalReplayObservation) {
			p.Rules[0].HostSnapshot = nil
		},
		func(p *historicalReplayObservationProfile, r *historicalReplayObservation) {
			p.Rules[0].Purpose = "native-miner-credit"
			r.Purpose = "native-miner-credit"
		},
	} {
		job, trace := hostSnapshotTestObservation()
		mutate(job.ObservationProfile, &trace.Observations[0])
		raw, _ := json.Marshal(job.ObservationProfile)
		trace.ProfileSha256 = historicalReplayDigest(sha256.Sum256(raw))
		if err := validateHistoricalReplayObservations(job, trace); err == nil {
			t.Fatal("contradictory allocator record accepted")
		}
	}
}
