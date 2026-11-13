package main

import (
	"crypto/sha256"
	"encoding/json"
	"testing"
)

func historicalStorageCallTestProfile() (historicalReplayJob, historicalReplayObservations) {
	rule, record := nativeExecutionTestRecord("native-miner-credit", 7, nil)
	rule.FunctionBodySha256 = historicalReplayDigest{7}
	rule.StorageCall = &historicalStorageCall{Operation: "set", Path: []historicalStorageCallsite{{FunctionIndex: 2, FunctionBodySha256: historicalReplayDigest{2}, OffsetStart: 10, OffsetEnd: 12}, {FunctionIndex: 7, FunctionBodySha256: rule.FunctionBodySha256, OffsetStart: 0, OffsetEnd: 4}}}
	value := "0x01"
	record.Ordinal, record.Operation, record.KeyHex, record.ValueHex = 1, "set", "0x21", &value
	record.Stack = []historicalReplayFrame{{FunctionIndex: 2, FunctionOffset: 10}, {FunctionIndex: 7, FunctionOffset: 1}, {FunctionIndex: 20, FunctionOffset: 4}}
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema, RuntimeCodeSha256: historicalReplayDigest{1}, SourceReviewSha256: historicalReplayDigest{2}, Rules: []historicalReplayHookRule{rule}}
	job := historicalReplayJob{ObservationProfile: profile, RuntimeCodeSha256: profile.RuntimeCodeSha256}
	raw, _ := json.Marshal(profile)
	trace := historicalReplayObservations{ProfileSha256: historicalReplayDigest(sha256.Sum256(raw)), SourceReviewSha256: profile.SourceReviewSha256, Authority: "caller-supplied-unapproved-callsite-profile", OriginalFunctionBodiesPreserved: true, HostCalls: 1, Observations: []historicalReplayObservation{record}}
	return job, trace
}

func TestHistoricalStorageCallPathRejectsReorderedOuterAndWrongOperationRecords(t *testing.T) {
	job, trace := historicalStorageCallTestProfile()
	if err := job.ObservationProfile.validate(job); err != nil {
		t.Fatal(err)
	}
	if err := validateHistoricalReplayObservations(job, &trace); err != nil {
		t.Fatal("valid exact original path refused", err)
	}
	for _, kind := range []string{"outer", "omission", "offset", "operation", "reverse"} {
		job, trace := historicalStorageCallTestProfile()
		record := &trace.Observations[0]
		switch kind {
		case "outer":
			record.Stack = append([]historicalReplayFrame{{FunctionIndex: 99, FunctionOffset: 1}}, record.Stack...)
		case "omission":
			record.Stack = record.Stack[1:]
		case "offset":
			record.Stack[0].FunctionOffset = 12
		case "operation":
			record.Operation = "append"
		case "reverse":
			record.Stack[0], record.Stack[1] = record.Stack[1], record.Stack[0]
		}
		if err := validateHistoricalReplayObservations(job, &trace); err == nil {
			t.Fatal("nonmatching original call path acquired credit", kind)
		}
	}
}

func TestHistoricalStorageCallPathAllowsOnlyDisjointBoundedReviewedPrefixes(t *testing.T) {
	job, _ := historicalStorageCallTestProfile()
	first := job.ObservationProfile.Rules[0]
	second := first
	second.StorageCall = &historicalStorageCall{Operation: "get", Path: append([]historicalStorageCallsite(nil), first.StorageCall.Path...)}
	second.StorageCall.Path[0].OffsetStart = 20
	second.StorageCall.Path[0].OffsetEnd = 22
	job.ObservationProfile.Rules = append(job.ObservationProfile.Rules, second)
	if err := job.ObservationProfile.validate(job); err != nil {
		t.Fatal("disjoint get/set prefixes sharing original semantic caller refused", err)
	}
	job.ObservationProfile.Rules = append(job.ObservationProfile.Rules, first)
	if err := job.ObservationProfile.validate(job); err == nil {
		t.Fatal("duplicated call path admitted")
	}
	for _, kind := range []string{"empty", "nine", "body", "ancestor", "allocator"} {
		job, _ := historicalStorageCallTestProfile()
		rule := &job.ObservationProfile.Rules[0]
		switch kind {
		case "empty":
			rule.StorageCall.Path = nil
		case "nine":
			rule.StorageCall.Path = make([]historicalStorageCallsite, 9)
		case "body":
			rule.StorageCall.Path[0].FunctionBodySha256 = historicalReplayDigest{}
		case "ancestor":
			rule.StorageCall.Path[1].FunctionIndex++
		case "allocator":
			name := "ext_allocator_malloc_version_1"
			rule.HostSnapshot = &name
		}
		if err := job.ObservationProfile.validate(job); err == nil {
			t.Fatal("unreviewed storage path accepted", kind)
		}
	}
}

func TestHistoricalStorageCallPathRejectsIntersectingPrefixesAtDifferentDepths(t *testing.T) {
	job, _ := historicalStorageCallTestProfile()
	longer := job.ObservationProfile.Rules[0]
	parent := historicalStorageCallsite{FunctionIndex: 20, FunctionBodySha256: historicalReplayDigest{20}, OffsetStart: 4, OffsetEnd: 6}
	longer.FunctionIndex, longer.FunctionBodySha256, longer.OffsetStart, longer.OffsetEnd = parent.FunctionIndex, parent.FunctionBodySha256, parent.OffsetStart, parent.OffsetEnd
	longer.StorageCall = &historicalStorageCall{Operation: "set", Path: append(append([]historicalStorageCallsite(nil), longer.StorageCall.Path...), parent)}
	job.ObservationProfile.Rules = append(job.ObservationProfile.Rules, longer)
	if err := job.ObservationProfile.validate(job); err == nil {
		t.Fatal("short and long exact prefixes can match one callback")
	}
}
