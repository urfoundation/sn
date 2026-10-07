// Owner-only planning exercises real authenticated synthetic storage. A selected
// unsigned method must never become authorization, an executed reset or custody.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Even favorable inputs leave authority and execution-time gates unresolved.
func TestOwnerTrimRanksAllSafeCapacitiesWithoutResetAuthority(t *testing.T) {
	client, fixture, policy := newSubnetFixture(t)
	preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Schema != ownerTrimPlanSchema || plan.ResetReady || plan.ApplyAuthority || plan.FullResetCompleted ||
		plan.Best == nil || !plan.Best.RuntimeReady || plan.Best.MaximumUids != 4 || plan.Best.ApprovedRemovals != 2 ||
		!plan.AllRequestedSelected || len(plan.Residual) != 0 || len(plan.Candidates) != 4 ||
		plan.Candidates[1].MaximumUids != 5 || !plan.Candidates[1].SelectionSafe || plan.Candidates[2].SelectionSafe || plan.Candidates[3].SelectionSafe {
		t.Fatalf("incorrect ranking or authority: %+v", plan)
	}
	if !reflect.DeepEqual(plan.Best.ExpectedRemoved, preview.Trim.Removed) || !reflect.DeepEqual(plan.Best.Survivors, preview.Trim.Survivors) ||
		!reflect.DeepEqual(plan.Census.Observation.RootRegistrations, preview.RootRegistrations) || len(preview.RootRegistrations) != 2 || fixture.count("chain_getFinalizedHead") != 4 {
		t.Fatal("mapping changed generations, root membership or finalized sample")
	}
	call, err := hex.DecodeString(plan.Best.UnsignedCallHex[2:])
	if err != nil || len(call) != 6 || call[0] != preview.Trim.Call.PalletIndex || call[1] != preview.Trim.Call.CallIndex ||
		binary.LittleEndian.Uint16(call[2:4]) != 25 || binary.LittleEndian.Uint16(call[4:6]) != 4 {
		t.Fatalf("unsigned bytes do not encode exact metadata call/netuid/capacity: %x %v", call, err)
	}
	digest := sha256.Sum256(call)
	if plan.Best.UnsignedCallHash != "sha256:"+hex.EncodeToString(digest[:]) ||
		!slices.Contains(plan.ExecutionBlockers, "EXECUTION_TIME_SELECTION_GUARD_NOT_PROVEN") ||
		!slices.Contains(plan.ExecutionBlockers, "CUSTODY_COLLATERAL_STAKE_CLAIM_AND_HISTORY_AUDIT_NOT_COMPLETE") {
		t.Fatal("unsigned review bytes acquired execution or custody authority")
	}
}

// The previous exact-set preview blocks this case. Ranking must retain the
// immune old generation explicitly and still find the one safe owner removal.
func TestOwnerTrimRetainsImmuneResidualAndFindsPartialRemoval(t *testing.T) {
	client, fixture, policy := newSubnetFixture(t)
	registered := uint64(99)
	policy.Remove[0].RegistrationBlock = &registered
	fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, registered), []byte{25, 0}, []byte{4, 0})
	preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Trim.MatchesRequestedRemovalSet || len(preview.Trim.Blockers) == 0 {
		t.Fatal("fixture did not reproduce exact-set refusal")
	}
	plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || plan.Best == nil || plan.Best.MaximumUids != 5 || plan.Best.ApprovedRemovals != 1 ||
		len(plan.Best.ExpectedRemoved) != 1 || plan.Best.ExpectedRemoved[0].Uid != 5 || len(plan.Residual) != 1 ||
		plan.AllRequestedSelected || plan.FullResetCompleted || plan.ResetReady {
		t.Fatalf("partial trim lost safe progress or claimed reset: %+v %v", plan, err)
	}
	residual := plan.Residual[0]
	if residual.Hotkey != policy.Remove[0].Hotkey || residual.Coldkey != policy.Remove[0].Coldkey ||
		*residual.RegistrationBlock != registered || residual.ObservedUid == nil || *residual.ObservedUid != 4 ||
		residual.Disposition != "retained_by_runtime" || !slices.Contains(residual.Reasons, "temporary-immunity") {
		t.Fatalf("residual lost the exact old custody generation: %+v", residual)
	}
}

// A tighter runtime minimum leaves one miner. Ties remove the higher UID, while
// unequal emissions can remove a lower UID and renumber the surviving generation.
func TestOwnerTrimRespectsMinimumTieOrderAndSurvivorRenumbering(t *testing.T) {
	for _, removeLower := range []bool{false, true} {
		client, fixture, policy := newSubnetFixture(t)
		fixture.set(t, "MinAllowedUids", []byte{5, 0}, []byte{25, 0})
		wantRemoved, wantSurvivor := uint16(5), uint16(4)
		if removeLower {
			emissions := []byte{}
			for _, value := range []uint64{100, 90, 80, 70, 0, 1} {
				emissions = binary.LittleEndian.AppendUint64(emissions, value)
			}
			fixture.set(t, "Emission", subnetTestVector(emissions, 8), []byte{25, 0})
			wantRemoved, wantSurvivor = 4, 5
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		if err != nil || plan.Best == nil || len(plan.Candidates) != 1 || plan.Best.MaximumUids != 5 ||
			len(plan.Residual) != 1 || plan.Best.ExpectedRemoved[0].Uid != wantRemoved {
			t.Fatalf("lower=%t: minimum/tie order changed: %+v %v", removeLower, plan, err)
		}
		survivor := plan.Best.Survivors[4]
		if survivor.Uid != wantSurvivor || survivor.NewUid != 4 || survivor.RegistrationBlock != uint64(40+wantSurvivor) ||
			survivor.Hotkey != preview.Seats[wantSurvivor].Hotkey || survivor.Coldkey != preview.Seats[wantSurvivor].Coldkey {
			t.Fatalf("renumbering changed ownership/generation: %+v", survivor)
		}
	}
}

// Policy roles protect a low-emission identity even without an active permit.
func TestOwnerTrimNeverRemovesDeclaredCustodyOrValidatorRoles(t *testing.T) {
	for _, role := range []string{"owner", "ur-validator", "third-party-validator", "reserve", "pool", "escrow", "custody", "other"} {
		client, fixture, policy := newSubnetFixture(t)
		policy.Preserve[1].Roles = []string{role}
		emissions := []byte{}
		for _, value := range []uint64{100, 90, 80, 0, 1, 1} {
			emissions = binary.LittleEndian.AppendUint64(emissions, value)
		}
		fixture.set(t, "Emission", subnetTestVector(emissions, 8), []byte{25, 0})
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil || preview.Seats[3].ValidatorPermit {
			t.Fatalf("%s did not exercise a declared role without permit: %v", role, err)
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		if err != nil || plan.Best != nil || len(plan.Residual) != 2 || plan.AllRequestedSelected || plan.ApplyAuthority {
			t.Fatalf("%s was removed by capacity optimization: %+v %v", role, plan, err)
		}
	}
}

// Source owner immunity is narrower than owner custody. Unresolved scope and a
// newly granted validator permit also cannot silently become removable miners.
func TestOwnerTrimProtectsNonimmuneOwnerUnresolvedAndPermitSeats(t *testing.T) {
	for _, change := range []string{"owner-outside-immunity", "unresolved", "permit"} {
		client, fixture, policy := newSubnetFixture(t)
		emissions := []uint64{100, 90, 80, 70, 1, 1}
		switch change {
		case "owner-outside-immunity":
			emissions[1] = 0
		case "unresolved":
			policy.Preserve = policy.Preserve[:1]
			emissions[3] = 0
		case "permit":
			fixture.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 0, 1, 0}, 1), []byte{25, 0})
		}
		encoded := []byte{}
		for _, value := range emissions {
			encoded = binary.LittleEndian.AppendUint64(encoded, value)
		}
		fixture.set(t, "Emission", subnetTestVector(encoded, 8), []byte{25, 0})
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil {
			t.Fatal(err)
		}
		if change == "owner-outside-immunity" && (preview.Seats[1].OwnerImmune || !preview.Seats[1].OwnerRecognized) {
			t.Fatal("fixture collapsed owner recognition into runtime immunity")
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		if err != nil || plan.Best != nil || len(plan.Residual) != 2 {
			t.Fatalf("%s acquired a destructive candidate: %+v %v", change, plan, err)
		}
	}
}

// The full uint16 capacity ceiling creates one compact no-op range, not 65,530
// allocations. Original proposed capacity does not constrain best-effort ranking.
func TestOwnerTrimBoundsEnumerationByOccupiedSeats(t *testing.T) {
	client, fixture, policy := newSubnetFixture(t)
	fixture.set(t, "MaxAllowedUids", []byte{255, 255}, []byte{25, 0})
	proposed := uint16(65535)
	policy.TrimMaximumUids = &proposed
	preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || len(plan.Candidates) != 4 || plan.Best == nil || plan.Best.MaximumUids != 4 ||
		plan.RequestedCapacity != proposed || plan.NoRemovalCapacityRange == nil ||
		plan.NoRemovalCapacityRange.Minimum != 6 || plan.NoRemovalCapacityRange.Maximum != 65535 {
		t.Fatalf("capacity expansion escaped occupied-seat bound: %+v %v", plan, err)
	}
}

// One immune seat is exactly 25% of capacity four; equality fails, so capacity
// five becomes the best partial selection. The threshold is real metadata.
func TestOwnerTrimUsesMetadataStrictImmunityThreshold(t *testing.T) {
	for _, threshold := range []byte{25, 26} {
		client, fixture, policy := newSubnetFixture(t)
		for palletIndex := range fixture.metadata.AsMetadataV14.Pallets {
			pallet := &fixture.metadata.AsMetadataV14.Pallets[palletIndex]
			if pallet.Name == "SubtensorModule" {
				for index := range pallet.Constants {
					if pallet.Constants[index].Name == "MaxImmuneUidsPercentage" {
						pallet.Constants[index].Value = []byte{threshold}
					}
				}
			}
		}
		subnetTestApproveMetadata(t, fixture, &policy)
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		want := uint16(4)
		if threshold == 25 {
			want = 5
		}
		if err != nil || plan.Best == nil || plan.Best.MaximumUids != want || plan.Best.ImmunePercentage != uint8(100/int(want)) {
			t.Fatalf("threshold %d changed strict comparison: %+v %v", threshold, plan, err)
		}
	}
}

// Timing cannot be guessed from a nominal interval or turned into readiness by
// a safe identity selection. The conditional best set remains useful evidence.
func TestOwnerTrimRetainsSelectionButBlocksUnknownCooldownAndClosedWindow(t *testing.T) {
	for _, change := range []string{"prior-trim", "pending", "freeze"} {
		client, fixture, policy := newSubnetFixture(t)
		switch change {
		case "prior-trim":
			fixture.set(t, "TransactionKeyLastBlock", binary.LittleEndian.AppendUint64(nil, 1), bytes.Repeat([]byte{0x61}, 32), []byte{25, 0}, []byte{9, 0})
		case "pending":
			fixture.set(t, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 101), []byte{25, 0})
		case "freeze":
			fixture.set(t, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 3), []byte{25, 0})
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		if err != nil || plan.Best == nil || !plan.Best.SelectionSafe || plan.Best.RuntimeReady || len(plan.Best.Blockers) == 0 {
			t.Fatalf("%s lost evidence or inferred timing authority: %+v %v", change, plan, err)
		}
	}
}

// Fresh generation, ownership and exact requested identities remain required
// even if an internal caller accidentally reuses older classification flags.
func TestOwnerTrimRebindsPolicyGenerationsAndRejectsForgedClassification(t *testing.T) {
	for _, change := range []string{"old-subnet", "owner", "smaller-scope", "coldkey", "registration", "protected", "unresolved"} {
		client, _, policy := newSubnetFixture(t)
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil {
			t.Fatal(err)
		}
		switch change {
		case "old-subnet":
			preview.SubnetGeneration++
		case "owner":
			preview.SubnetOwnerColdkey = "0x" + strings.Repeat("ab", 32)
		case "smaller-scope":
			policy.Remove = policy.Remove[:1]
		case "coldkey":
			preview.Seats[5].Coldkey = "0x" + strings.Repeat("ab", 32)
		case "registration":
			preview.Seats[5].RegistrationBlock++
		case "protected":
			policy.Preserve = append(policy.Preserve, subnetProtectedIdentity{subnetIdentityExpectation: policy.Remove[1], Roles: []string{"custody"}})
			policy.Remove = policy.Remove[:1]
		case "unresolved":
			preview.Seats[5].Disposition = "unresolved"
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		if err != nil || plan.Best != nil || plan.AllRequestedSelected || len(plan.Residual) != len(policy.Remove) {
			t.Fatalf("%s reused stale selection or underflowed residual: %+v %v", change, plan, err)
		}
	}
}

// An internally mismatched census is not a weaker form of mainnet evidence.
func TestOwnerTrimRejectsForeignRuntimeAndMalformedCensus(t *testing.T) {
	for _, change := range []string{"testnet", "genesis", "runtime", "source", "metadata", "code", "emission", "duplicate", "capacity", "incomplete"} {
		client, _, policy := newSubnetFixture(t)
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil {
			t.Fatal(err)
		}
		switch change {
		case "testnet":
			preview.Identity.EvmChainId = 945
		case "genesis":
			preview.Identity.GenesisHash = "0x" + strings.Repeat("ab", 32)
		case "runtime":
			preview.RuntimeVersion.StateVersion++
		case "source":
			preview.RuntimeSourceCommit = strings.Repeat("b", 40)
		case "metadata":
			preview.RuntimeMetadataHash = "0x" + strings.Repeat("ab", 32)
		case "code":
			preview.RuntimeCodeHash = "0x" + strings.Repeat("ab", 32)
		case "emission":
			preview.Seats[5].EmissionRao = "0"
		case "duplicate":
			preview.Seats[5].Hotkey = preview.Seats[4].Hotkey
		case "capacity":
			preview.MaximumUids = 1
		case "incomplete":
			preview.CensusComplete = false
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		if err == nil || plan.ContentHash != "" || plan.Best != nil {
			t.Fatalf("%s produced apparently retained evidence: %+v %v", change, plan, err)
		}
	}
}

// Capability absence is a useful blocked plan, never an invented call index.
func TestOwnerTrimBlocksMissingMetadataCapabilities(t *testing.T) {
	for _, change := range []string{"call", "threshold"} {
		client, fixture, policy := newSubnetFixture(t)
		for index := range fixture.metadata.AsMetadataV14.Pallets {
			pallet := &fixture.metadata.AsMetadataV14.Pallets[index]
			if change == "call" && pallet.Name == "AdminUtils" {
				pallet.Name = "SyntheticUnknownAdmin"
			}
			if change == "threshold" && pallet.Name == "SubtensorModule" {
				for constantIndex := range pallet.Constants {
					if pallet.Constants[constantIndex].Name == "MaxImmuneUidsPercentage" {
						pallet.Constants[constantIndex].Name = "SyntheticUnknownThreshold"
					}
				}
			}
		}
		subnetTestApproveMetadata(t, fixture, &policy)
		preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
		if err != nil || plan.Best != nil || len(plan.Residual) != 2 || len(plan.ExecutionBlockers) == 0 {
			t.Fatalf("missing %s became owner capability: %+v %v", change, plan, err)
		}
	}
}

// Reentry affects execution safety even when the current ranked set is exact.
func TestOwnerTrimRecordsCompetingRegistrationGate(t *testing.T) {
	client, fixture, policy := newSubnetFixture(t)
	fixture.set(t, "NetworkRegistrationAllowed", []byte{1}, []byte{25, 0})
	preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || plan.Best == nil || plan.ApplyAuthority || !slices.Contains(plan.ExecutionBlockers, "COMPETING_REGISTRATION_AND_REENTRY_POLICY_NOT_ENFORCED") {
		t.Fatalf("ranking hid reentry risk: %+v %v", plan, err)
	}
}

// The same immutable census produces identical bytes; raw storage is part of the
// seal, while the seal itself confers no authenticated execution authority.
func TestOwnerTrimDeterministicHashBindsCompleteCensus(t *testing.T) {
	client, _, policy := newSubnetFixture(t)
	preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
	if err != nil {
		t.Fatal(err)
	}
	first, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("identical census changed plan: %v", err)
	}
	want := first.ContentHash
	first.ContentHash = ""
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte(ownerTrimPlanSchema+"\x00"), raw...))
	if want != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal("plan seal cannot be independently reproduced")
	}
	preview.Storage[0].ValueSource = "synthetic-changed-retained-evidence"
	changed, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || changed.ContentHash == want || changed.Census.ContentHash == second.Census.ContentHash {
		t.Fatal("raw census evidence escaped plan hash")
	}
}

// CLI shares one finalized census and cannot acquire apply/signing flags.
func TestOwnerTrimCommandPublishesOnlyUnsignedReviewEvidence(t *testing.T) {
	_, fixture, policy := newSubnetFixture(t)
	server := rootFixtureServer(t, fixture)
	path := filepath.Join(t.TempDir(), "synthetic-owner-trim-policy.json")
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(t.Context(), []string{"owner-trim-plan", "--rpc", server.URL, "--policy", path}, &stdout, &stderr)
	var plan ownerTrimPlan
	if code != 0 || json.Unmarshal(stdout.Bytes(), &plan) != nil || plan.Best == nil ||
		plan.ResetReady || plan.ApplyAuthority || plan.FullResetCompleted || fixture.count("chain_getFinalizedHead") != 4 {
		t.Fatalf("CLI lost evidence or became an apply path: %d %s %s", code, stdout.String(), stderr.String())
	}
	digest := sha256.Sum256(raw)
	if plan.Census.Observation.PolicyHash != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal("CLI omitted exact input policy bytes")
	}
	priorReads := fixture.count("system_chain")
	for _, badFlag := range [][]string{{"--apply"}, {"--seed", "synthetic-private.seed"}, {"--retry-window", "1s"}, {"--retry-window", "16m"}} {
		stdout.Reset()
		stderr.Reset()
		args := append([]string{"owner-trim-plan", "--rpc", server.URL, "--policy", path}, badFlag...)
		if code := runMain(t.Context(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("system_chain") != priorReads {
			t.Fatalf("unsupported owner command reached RPC: %d %s", code, stderr.String())
		}
	}
	if code := runSubnetCommand(t.Context(), nil, &stdout, &stderr); code != 2 {
		t.Fatal("missing command was accepted")
	}
}

// A canonical mismatch or wrong route publishes nothing. Timing/capacity
// refusals instead retain the authenticated census as explicitly blocked output.
func TestOwnerTrimCommandSeparatesIntegrityFromRetainedBlockedEvidence(t *testing.T) {
	for _, change := range []string{"testnet", "fork", "timing", "minimum"} {
		_, fixture, policy := newSubnetFixture(t)
		switch change {
		case "testnet":
			fixture.evmChainHex = "0x3b1"
		case "fork":
			fixture.forkAfterStorage = true
		case "timing":
			fixture.set(t, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 101), []byte{25, 0})
		case "minimum":
			fixture.set(t, "MinAllowedUids", []byte{6, 0}, []byte{25, 0})
		}
		server := rootFixtureServer(t, fixture)
		path := filepath.Join(t.TempDir(), "synthetic-owner-trim-policy.json")
		raw, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code := runMain(t.Context(), []string{"owner-trim-plan", "--rpc", server.URL, "--policy", path}, &stdout, &stderr)
		if code != 3 {
			t.Fatalf("%s lost explicit refusal: %d %s", change, code, stderr.String())
		}
		if change == "testnet" || change == "fork" {
			if stdout.Len() != 0 || change == "testnet" && fixture.count("state_getStorage") != 0 {
				t.Fatalf("%s published or trusted foreign census", change)
			}
			continue
		}
		var plan ownerTrimPlan
		if json.Unmarshal(stdout.Bytes(), &plan) != nil || !plan.Census.Observation.CensusComplete || plan.ResetReady ||
			change == "timing" && (plan.Best == nil || plan.Best.RuntimeReady) || change == "minimum" && plan.Best != nil {
			t.Fatalf("%s discarded useful blocked evidence: %s", change, stdout.String())
		}
	}
}

// Cancellation is checked before processing, with no partial plan to resume as
// though it were a complete authenticated ranking.
func TestOwnerTrimCancellationPublishesNoPartialPlan(t *testing.T) {
	client, _, policy := newSubnetFixture(t)
	preview, err := client.readSubnetPreview(t.Context(), policy, "synthetic-policy-hash")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	plan, err := buildOwnerTrimPlan(ctx, policy, preview)
	if !errors.Is(err, context.Canceled) || plan.ContentHash != "" || plan.Best != nil {
		t.Fatalf("canceled ranking published evidence: %+v %v", plan, err)
	}
}
