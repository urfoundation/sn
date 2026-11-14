// Public policy loading admits the full explicit fee roster without granting
// the same frame to legacy, malformed or additional undeclared role inputs.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A populated original roster reaches the actual strict file loader before
// any worker, RPC or engine can start. Legacy admission keeps its old limit.
func TestMonitorServicesWholeFeePopulatedFrameUsesExplicitAuthority(t *testing.T) {
	source := newEconomicConservationFixture(t, false)
	f := newMonitorServicesFixture(t, "alpha")
	execution := source.policy.Native.Observation.Execution
	execution.FeeCensus = &nativeFeeCensusPolicy{Schema: nativeFeeCensusSchema, ReviewSha256: monitorReadDigest([]byte("synthetic original complete fee roster"))}
	for index := 1; index <= 2*rootCensusLimit; index++ {
		execution.FeeCensus.Participants = append(execution.FeeCensus.Participants, fmt.Sprintf("0x%064x", index))
	}
	execution.Producer = &nativeExecutionProducerPolicy{Schema: nativeProducerSchema, Authority: planFileReference{Path: filepath.Join(f.directory, "original-fee-authority.json"), Sha256: monitorReadDigest([]byte("synthetic signed fee authority"))}, CaptureEngine: execution.Engine, Nodes: filepath.Join(f.directory, "original-nodes"), MaximumJobs: 4, MaximumBytes: 2 * nativeProducerBoundaryReserve, MaximumEntries: 2 * nativeProducerBoundaryEntries}
	f.policy.NativeEconomics = []monitorEconomicNativePolicy{source.policy.Native}
	raw, err := json.Marshal(f.policy)
	if err != nil || len(raw) <= maxMonitorServicesBytes || 2*len(raw) > nativeProducerFeeAuthorityLimit {
		t.Fatal("populated service policy lacks its explicit twofold frame", len(raw), err)
	}
	f.writePolicy(t)
	expected := monitorTestExpectation()
	expected.NativeChain = source.policy.Native.Observation.Network.NativeChain
	loaded, err := loadMonitorServices(t.Context(), f.policyPath, expected, f.checkpointPath, f.metricsPath)
	if err != nil || loaded == nil || len(loaded.NativeEconomics[0].Observation.Execution.FeeCensus.Participants) != 2*rootCensusLimit {
		t.Fatal("actual service admission lost complete original fee roster", err)
	}
	f.policy.NativeEconomics[0].Observation.Execution.FeeCensus = nil
	raw, err = json.Marshal(f.policy)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, bytes.Repeat([]byte{' '}, maxMonitorServicesBytes+1)...)
	if err := os.WriteFile(f.policyPath, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMonitorServices(t.Context(), f.policyPath, expected, f.checkpointPath, f.metricsPath); err == nil || !strings.Contains(err.Error(), "declared original role frame") {
		t.Fatal("legacy policy borrowed whole-fee frame", err)
	}
}

// The same bounded discriminator used by live and repair readers refuses a
// malformed roster, one extra byte and an undeclared fifth economic role.
func TestMonitorServicesWholeFeeFrameRefusesMalformedAndUnadmittedGrowth(t *testing.T) {
	fees := nativeWholeFeeTestAuthority(economicConservationPolicy{})
	policy := monitorServicesPolicy{NativeEconomics: []monitorEconomicNativePolicy{{Observation: economicEmissionPolicy{Execution: &nativeExecutionPolicy{FeeCensus: fees}}}}}
	maximum := maxMonitorServicesBytes + nativeProducerFeeAuthorityLimit
	if err := policy.validateFrame(maximum); err != nil {
		t.Fatal("exact original fee frame refused", err)
	}
	if err := policy.validateFrame(maximum + 1); err == nil {
		t.Fatal("unadmitted fee frame growth accepted")
	}
	fees.Schema = "unapproved"
	if err := policy.validateFrame(maximum); err == nil {
		t.Fatal("malformed fee authority granted frame capacity")
	}
	fees.Schema = nativeFeeCensusSchema
	policy.NativeEconomics = append(policy.NativeEconomics, policy.NativeEconomics[0], policy.NativeEconomics[0], policy.NativeEconomics[0], policy.NativeEconomics[0])
	if err := policy.validateFrame(maxMonitorFeeServicesBytes); err == nil {
		t.Fatal("fifth role borrowed complete fee frame capacity")
	}
}
