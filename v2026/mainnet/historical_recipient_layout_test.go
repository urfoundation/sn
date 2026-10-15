// Wire guards distinguish actual dynamic Owner reads from runtime get results
// and memory captures. Synthetic accounts never assert live-chain authority.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// Keep the selected original call path while adding one bounded dynamic read.
func historicalRecipientOwnerTestProfile() (historicalReplayJob, historicalReplayObservations) {
	job, trace := historicalStorageCallTestProfile()
	mode := nativeRecipientStorageLayoutSchema
	job.ObservationProfile.RecipientLayout = &mode
	rule := &job.ObservationProfile.Rules[0]
	rule.Purpose, rule.RecipientOwner = "native-owner-recycle", true
	rule.Memory = []historicalNativeCapture{{Name: "hotkey", Address: 16, Bytes: 32}}
	hotkey := bytes.Repeat([]byte{0x39}, 32)
	owner := "0x" + hex.EncodeToString(bytes.Repeat([]byte{0x77}, 32))
	record := &trace.Observations[0]
	record.Purpose = rule.Purpose
	record.Native.Memory = []historicalNativeMemory{{Name: "hotkey", Address: 16, BytesHex: "0x" + hex.EncodeToString(hotkey)}}
	values := []historicalExecutionStateValue{{KeyHex: historicalRecipientOwnerKey(hotkey), ValueHex: &owner}}
	record.Native.ExecutionState = &values
	raw, _ := json.Marshal(job.ObservationProfile)
	trace.ProfileSha256 = historicalReplayDigest(sha256.Sum256(raw))
	return job, trace
}

func TestHistoricalRecipientOwnerPreservesExplicitLiveStateAndOptionalWire(t *testing.T) {
	legacy, _ := historicalStorageCallTestProfile()
	raw, err := json.Marshal(legacy.ObservationProfile)
	if err != nil || bytes.Contains(raw, []byte("recipient_")) {
		t.Fatal("omitted legacy fields changed wire", err)
	}
	job, trace := historicalRecipientOwnerTestProfile()
	if err := job.ObservationProfile.validate(job); err != nil {
		t.Fatal(err)
	}
	if err := validateHistoricalReplayObservations(job, &trace); err != nil {
		t.Fatal("actual Owner lookup refused", err)
	}
	(*trace.Observations[0].Native.ExecutionState)[0].ValueHex = nil
	if err := validateHistoricalReplayObservations(job, &trace); err != nil {
		t.Fatal("explicit proven absence lost its wire identity", err)
	}
	if trace.Observations[0].StorageReturn != nil {
		t.Fatal("observer state read invented a runtime return")
	}
}

func TestHistoricalRecipientOwnerRefusesChangedKeyValueAndUnboundedScope(t *testing.T) {
	for _, kind := range []string{"missing", "extra", "key", "short", "long", "hotkey"} {
		job, trace := historicalRecipientOwnerTestProfile()
		record := trace.Observations[0].Native
		switch kind {
		case "missing":
			record.ExecutionState = nil
		case "extra":
			*record.ExecutionState = append(*record.ExecutionState, (*record.ExecutionState)[0])
		case "key":
			(*record.ExecutionState)[0].KeyHex = "0x01"
		case "short":
			value := "0x" + hex.EncodeToString(make([]byte, 31))
			(*record.ExecutionState)[0].ValueHex = &value
		case "long":
			value := "0x" + hex.EncodeToString(make([]byte, 33))
			(*record.ExecutionState)[0].ValueHex = &value
		case "hotkey":
			record.Memory[0].BytesHex = "0x" + hex.EncodeToString(bytes.Repeat([]byte{0x58}, 32))
		}
		if err := validateHistoricalReplayObservations(job, &trace); err == nil {
			t.Fatal("changed Owner provenance accepted", kind)
		}
	}
	for _, kind := range []string{"mode", "flag", "purpose", "width", "path", "static", "captured", "subnet-owner-hotkey", "auto-stake-destination", "coldkey"} {
		job, _ := historicalRecipientOwnerTestProfile()
		rule := &job.ObservationProfile.Rules[0]
		switch kind {
		case "mode":
			job.ObservationProfile.RecipientLayout = nil
		case "flag":
			rule.RecipientOwner = false
		case "purpose":
			rule.Purpose = "native-miner-credit"
		case "width":
			rule.Memory[0].Bytes = 33
		case "path":
			rule.StorageCall = nil
		case "static":
			rule.StateReads = []string{"0x01"}
		default:
			rule.Memory = append(rule.Memory, historicalNativeCapture{Name: kind, Bytes: 32})
		}
		if err := job.ObservationProfile.validate(job); err == nil {
			t.Fatal("unbounded or substituted Owner recipe accepted", kind)
		}
	}
}
