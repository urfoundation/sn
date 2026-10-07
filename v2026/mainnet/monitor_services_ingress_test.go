// Complete service-policy frames share actual protected file custody with
// operational records while retaining separate finite ingress authority.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

// Even a small legacy policy must reach its actual decoder before any worker
// starts; a larger possible fee census does not invalidate legacy admission.
func TestMonitorServicesIngressReadsLegacyPolicyWithOriginalFrame(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha")
	loaded, err := loadMonitorServices(t.Context(), f.policyPath, monitorTestExpectation(), f.checkpointPath, f.metricsPath)
	if err != nil || loaded == nil || len(loaded.Validators) != 1 || loaded.Validators[0] != f.policy.Validators[0] {
		t.Fatal("actual legacy service ingress was refused", err)
	}
	if raw, err := readMonitorServiceFile(t.Context(), f.policyPath, maxMonitorFeeServicesBytes, false, monitorServiceReadHooks{}); err == nil || raw != nil {
		t.Fatal("generic operational record borrowed the service-policy profile", err)
	}
}

// The larger policy ingress cannot publish bytes after cancellation, failed
// descriptor close or an equal-byte replacement of its original inode.
func TestMonitorServicesIngressKeepsOriginalFileCustody(t *testing.T) {
	f := newMonitorServicesFixture(t, "alpha")
	ctx, cancel := context.WithCancel(t.Context())
	raw, err := readMonitorServicesPolicyFile(ctx, f.policyPath, monitorServiceReadHooks{afterRead: func(*os.File) error { cancel(); return nil }})
	if raw != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled policy ingress published bytes", err)
	}
	cause := errors.New("synthetic policy close fault")
	raw, err = readMonitorServicesPolicyFile(t.Context(), f.policyPath, monitorServiceReadHooks{afterClose: func(file *os.File) error {
		_, closed := file.Stat()
		if !errors.Is(closed, os.ErrClosed) {
			return errors.New("policy close observer ran before physical close")
		}
		return cause
	}})
	if raw != nil || !errors.Is(err, cause) {
		t.Fatal("failed policy close published bytes", err)
	}
	original, err := os.ReadFile(f.policyPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = readMonitorServicesPolicyFile(t.Context(), f.policyPath, monitorServiceReadHooks{afterRead: func(*os.File) error {
		return publishMonitorFile(f.policyPath, original, 0644, nil)
	}})
	if err == nil || raw != nil {
		t.Fatal("equal-byte policy inode replacement was accepted")
	}
	retained, err := readMonitorServicesPolicyFile(t.Context(), f.policyPath, monitorServiceReadHooks{})
	if err != nil || !bytes.Equal(retained, original) {
		t.Fatal("fresh policy observation could not recover", err)
	}
}

// The active adapter consumes the same populated signed role census as live
// monitoring, then retains its original one-stop and one-start episode.
func TestRepairActiveValidatorReadsExplicitFeePolicyFrame(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	raw, err := os.ReadFile(f.active.Plan.MonitorServices.Path)
	if err != nil {
		t.Fatal(err)
	}
	var policy monitorServicesPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	fees := &nativeFeeCensusPolicy{Schema: nativeFeeCensusSchema, ReviewSha256: monitorReadDigest([]byte("synthetic active repair original fee roster"))}
	for index := 1; index <= 2*rootCensusLimit; index++ {
		fees.Participants = append(fees.Participants, fmt.Sprintf("0x%064x", index))
	}
	policy.NativeEconomics = []monitorEconomicNativePolicy{{Observation: economicEmissionPolicy{Execution: &nativeExecutionPolicy{FeeCensus: fees}}}}
	raw, err = json.Marshal(policy)
	if err != nil || len(raw) <= 128*1024 || policy.validateFrame(len(raw)) != nil {
		t.Fatal("synthetic active policy lacks an admitted full fee census", len(raw), err)
	}
	repairValidatorTestWrite(t, f.active.Plan.MonitorServices.Path, raw, 0600)
	f.active.Plan.MonitorServices.Sha256 = monitorReadDigest(raw)
	f.signActive()
	if observed, err := f.host.read(t.Context(), f.active.Plan.MonitorServices.Path, f.active.Plan.Process.MonitorUid, maxMonitorFeeServicesBytes, false); err == nil || observed != nil {
		t.Fatal("generic host evidence reader borrowed the policy profile", err)
	}
	f.claimActive()
	result, exit, detail := f.commandActive("resume")
	if exit != 0 || result.Completed == nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("active repair could not consume original complete service policy", exit, result, detail)
	}
	result, exit, detail = f.commandActive("resume")
	if exit != 0 || result.Completed == nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("full policy reopen renewed original repair authority", exit, result, detail)
	}
}
