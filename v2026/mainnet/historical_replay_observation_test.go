// Synthetic process replies test the actual Go owner and wire joins. Separate
// Rust complete-proof fixtures exercise execution and runtime event decoding.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"golang.org/x/crypto/blake2b"
)

func historicalObservationTestTrace(job historicalReplayJob) *historicalReplayObservations {
	profile := job.ObservationProfile
	raw, _ := json.Marshal(profile)
	trace := &historicalReplayObservations{ProfileSha256: historicalReplayDigest(sha256.Sum256(raw)), SourceReviewSha256: profile.SourceReviewSha256, Authority: "caller-supplied-unapproved-callsite-profile", OriginalFunctionBodiesPreserved: true, HostCalls: 3, Observations: []historicalReplayObservation{}}
	if profile.MetadataSha256 == nil {
		return trace
	}
	source := historicalReplayAddress{29}
	payer := historicalReplayDigest(blake2b.Sum256(append([]byte("evm:"), source[:]...)))
	transaction := historicalReplayDigest{41}
	index := uint32(0)
	withdrawal, refund, debit := "1000", "250", "750"
	if job.RuntimeCodeHex == "0xf1" {
		refund, debit = "0", "1000"
	}
	key := "0x" + hex.EncodeToString(append(xxhash.New128([]byte("System")).Sum(nil), xxhash.New128([]byte("Events")).Sum(nil)...))
	fees := &historicalReplayFeeEvents{MetadataSha256: *profile.MetadataSha256, Authority: "original-runtime-metadata-and-unapproved-callsite-profile"}
	ordinals := []uint64{}
	for item, purpose := range []string{"fee-withdraw", "fee-refund", "ethereum-executed"} {
		if item == 1 && job.RuntimeCodeHex == "0xf0" {
			continue
		}
		value := []byte{byte(item + 1)}
		valueHex := "0x" + hex.EncodeToString(value)
		ordinal := uint64(item + 1)
		trace.Observations = append(trace.Observations, historicalReplayObservation{Ordinal: ordinal, Purpose: purpose, Operation: "append", KeyHex: key, ValueHex: &valueHex, Stack: []historicalReplayFrame{{FunctionIndex: uint32(item), FunctionOffset: 1}}})
		event := historicalReplayFeeEvent{ObservationOrdinal: ordinal, Purpose: purpose, Phase: "apply-extrinsic", ExtrinsicIndex: &index, EventSha256: historicalReplayDigest(sha256.Sum256(value))}
		switch item {
		case 0:
			event.Event, event.Payer, event.AmountRao = "Balances.Withdraw", &payer, &withdrawal
		case 1:
			event.Event, event.Payer, event.AmountRao = "Balances.Deposit", &payer, &refund
		case 2:
			event.Event, event.Source, event.TransactionHash = "Ethereum.Executed", &source, &transaction
		}
		fees.Events = append(fees.Events, event)
		ordinals = append(ordinals, ordinal)
	}
	candidate := historicalReplayFeeCandidate{TransactionHash: transaction, Payer: payer, ExtrinsicIndex: &index, WithdrawalRao: &withdrawal, RefundRao: &refund, DebitRao: &debit, Status: "observed-pair-unadmitted", EventOrdinals: ordinals}
	if job.RuntimeCodeHex == "0xf0" {
		candidate.RefundRao, candidate.DebitRao, candidate.Status = nil, nil, "refund-unobserved"
	}
	fees.Candidates = []historicalReplayFeeCandidate{candidate}
	trace.FeeEvents = fees
	switch job.RuntimeCodeHex {
	case "0xe1":
		trace.ProfileSha256[0] ^= 1
	case "0xe2":
		fees.MetadataSha256[0] ^= 1
	case "0xe3":
		changed := "751"
		fees.Candidates[0].DebitRao = &changed
	case "0xe4":
		trace.Observations[0].Stack[0].FunctionIndex = 100
	case "0xe5":
		fees.Events[0].EventSha256[0] ^= 1
	case "0xe6":
		fees.Authority = "independently-verified"
	case "0xe7":
		fees.UnmatchedFeeEvents++
	case "0xe8":
		fees.Candidates[0].Payer[0] ^= 1
	case "0xe9":
		fees.Candidates[0].EventOrdinals = []uint64{3}
	}
	return trace
}

func historicalObservationTestRequest(t *testing.T, mode string) historicalReplayRequest {
	t.Helper()
	request := historicalReplayTestRequest(t, mode)
	raw, err := os.ReadFile(request.Job.Path)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := decodePlanJson(raw, &job); err != nil {
		t.Fatal(err)
	}
	metadata := historicalReplayDigest{17}
	profile := &historicalReplayObservationProfile{Schema: "urnetwork-original-wasm-hook-observation-v1", RuntimeCodeSha256: job.RuntimeCodeSha256, SourceReviewSha256: historicalReplayDigest{19}, MetadataSha256: &metadata}
	for index, purpose := range []string{"fee-withdraw", "fee-refund", "ethereum-executed"} {
		profile.Rules = append(profile.Rules, historicalReplayHookRule{Purpose: purpose, FunctionIndex: uint32(index), FunctionBodySha256: historicalReplayDigest{byte(index + 1)}, OffsetStart: 0, OffsetEnd: 4})
	}
	job.ObservationProfile = profile
	historicalObservationTestWrite(t, &request, job)
	return request
}

func historicalObservationTestWrite(t *testing.T, request *historicalReplayRequest, job historicalReplayJob) {
	t.Helper()
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.Job.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request.Job.Sha256 = monitorReadDigest(raw)
}

func TestHistoricalReplayPublicObservationRetainsUnapprovedExactCandidate(t *testing.T) {
	request := historicalObservationTestRequest(t, "0xe0")
	var stdout, stderr bytes.Buffer
	code := runMain(t.Context(), []string{"verify-historical-execution", "--engine", request.Engine.Path, "--engine-sha256", request.Engine.Sha256, "--job", request.Job.Path, "--job-sha256", request.Job.Sha256}, &stdout, &stderr)
	var report historicalReplayReport
	if code != 0 || decodePlanJson(stdout.Bytes(), &report) != nil || report.HookObservations == nil || report.HookObservations.FeeEvents == nil {
		t.Fatal("public observed replay did not retain exact candidate", code, stderr.String())
	}
	fees := report.HookObservations.FeeEvents
	if report.RuntimeAdmitted || report.NativeFeeDebit != nil || report.NativeFeeWithdrawalRefund || report.ProductionSelection || len(fees.Candidates) != 1 || fees.Candidates[0].DebitRao == nil || *fees.Candidates[0].DebitRao != "750" || fees.Candidates[0].Status != "observed-pair-unadmitted" {
		t.Fatal("candidate was lost or promoted into fee authority", report)
	}
}

func TestHistoricalReplayObservationBindsOriginalProfileTraceAndEvents(t *testing.T) {
	for _, mode := range []string{"0xe1", "0xe2", "0xe3", "0xe4", "0xe5", "0xe6", "0xe7", "0xe8", "0xe9"} {
		request := historicalObservationTestRequest(t, mode)
		result, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
		if err == nil || result != nil {
			t.Fatal("contradictory observation was admitted by actual caller", mode, result, err)
		}
	}
}

func TestHistoricalReplayObservationDistinguishesMissingAndActualZeroRefund(t *testing.T) {
	for _, mode := range []string{"0xf0", "0xf1"} {
		request := historicalObservationTestRequest(t, mode)
		result, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
		if err != nil || result == nil || result.HookObservations == nil || result.HookObservations.FeeEvents == nil || len(result.HookObservations.FeeEvents.Candidates) != 1 {
			t.Fatal("refund observation did not survive actual caller", mode, result, err)
		}
		candidate := result.HookObservations.FeeEvents.Candidates[0]
		if mode == "0xf0" && (candidate.RefundRao != nil || candidate.DebitRao != nil || candidate.Status != "refund-unobserved") || mode == "0xf1" && (candidate.RefundRao == nil || *candidate.RefundRao != "0" || candidate.DebitRao == nil || *candidate.DebitRao != "1000") {
			t.Fatal("missing refund became zero or actual zero was lost", mode, candidate)
		}
		if result.NativeFeeDebit != nil || result.NativeFeeWithdrawalRefund {
			t.Fatal("unapproved observation became native fee authority")
		}
	}
}

func TestHistoricalReplayObservationProfileRefusesBeforeProcessLaunch(t *testing.T) {
	for _, fault := range []string{"code", "review", "metadata", "overlap", "purpose", "count"} {
		request := historicalObservationTestRequest(t, "0xe0")
		raw, err := os.ReadFile(request.Job.Path)
		if err != nil {
			t.Fatal(err)
		}
		var job historicalReplayJob
		if err := decodePlanJson(raw, &job); err != nil {
			t.Fatal(err)
		}
		profile := job.ObservationProfile
		switch fault {
		case "code":
			profile.RuntimeCodeSha256[0] ^= 1
		case "review":
			profile.SourceReviewSha256 = historicalReplayDigest{}
		case "metadata":
			*profile.MetadataSha256 = historicalReplayDigest{}
		case "overlap":
			profile.Rules = append(profile.Rules, profile.Rules[0])
		case "purpose":
			profile.Rules[0].Purpose = "fee-verified"
		case "count":
			profile.Rules = make([]historicalReplayHookRule, 33)
		}
		historicalObservationTestWrite(t, &request, job)
		started := false
		result, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { started = true }})
		if err == nil || result != nil || started {
			t.Fatal("invalid observation profile reached a process", fault, result, err, started)
		}
	}
}

func TestHistoricalReplayObservedOutputBoundCancelsActualChild(t *testing.T) {
	request := historicalObservationTestRequest(t, "0xf3")
	result, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
	if result != nil || err == nil || !strings.Contains(err.Error(), "output exceeds bound") || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("observed report exceeded its streamed bound or waited for deadline", result, err)
	}
}

func TestHistoricalReplayAddressRefusesNoncanonicalArray(t *testing.T) {
	for _, raw := range []string{`[]`, `[1]`, strings.Repeat("[", 1) + strings.Repeat("1,", 20) + `1]`, `[256,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]`} {
		var address historicalReplayAddress
		if err := json.Unmarshal([]byte(raw), &address); err == nil {
			t.Fatal("short, long or non-byte address accepted", raw)
		}
	}
	var address historicalReplayAddress
	if err := json.Unmarshal([]byte(`[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,1]`), &address); err != nil || address[19] != 1 {
		t.Fatal("exact address refused", address, err)
	}
}
