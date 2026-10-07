// Synthetic identities and explicit block transitions test bounded subset
// qualification through the real reader. No live chain, signer or submission exists.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Four protected seats and six approved miners leave two explicit old-miner
// residuals at capacity six. All swap evidence is synthetic and block-pinned.
func newOwnerTrimBoundedFixture(t *testing.T) (*rpcClient, *rootRpcFixture, subnetCensusPolicy, ownerTrimWindow) {
	t.Helper()
	client, fixture, policy := newSubnetFixture(t)
	netuidArg := []byte{25, 0}
	fixture.set(t, "SubnetworkN", []byte{10, 0}, netuidArg)
	fixture.set(t, "MaxAllowedUids", []byte{12, 0}, netuidArg)
	fixture.set(t, "ImmunityPeriod", []byte{32, 0}, netuidArg)
	fixture.set(t, "ImmuneOwnerUidsLimit", []byte{2, 0}, netuidArg)
	fixture.set(t, "BlocksSinceLastStep", binary.LittleEndian.AppendUint64(nil, 50), netuidArg)
	fixture.set(t, "ColdkeySwapAnnouncementDelay", binary.LittleEndian.AppendUint32(nil, 20))
	fixture.set(t, "Active", subnetTestVector(bytes.Repeat([]byte{1}, 10), 1), netuidArg)
	fixture.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 0, 0, 0, 0, 0, 0, 0}, 1), netuidArg)
	maximum := uint16(6)
	policy.TrimMaximumUids = &maximum
	emissions := []byte{}
	for index := 0; index < 10; index++ {
		uidArg := binary.LittleEndian.AppendUint16(nil, uint16(index))
		hotkey := bytes.Repeat([]byte{byte(0x41 + index)}, 32)
		coldkey := bytes.Repeat([]byte{byte(0x61 + index)}, 32)
		if index == 1 {
			coldkey = bytes.Repeat([]byte{0x61}, 32)
		}
		fixture.set(t, "LastHotkeySwapOnNetuid", binary.LittleEndian.AppendUint64(nil, 90), netuidArg, coldkey)
		if index == 2 || index == 3 {
			registeredAt := uint64(90)
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, registeredAt), netuidArg, uidArg)
			policy.Preserve[index-2].RegistrationBlock = &registeredAt
		}
		if index >= 6 {
			registeredAt := uint64(40 + index)
			fixture.set(t, "Keys", hotkey, netuidArg, uidArg)
			fixture.set(t, "Uids", uidArg, netuidArg, hotkey)
			fixture.set(t, "Owner", coldkey, hotkey)
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, registeredAt), netuidArg, uidArg)
			fixture.set(t, "IsNetworkMember", []byte{1}, hotkey, netuidArg)
			policy.Remove = append(policy.Remove, subnetIdentityExpectation{Hotkey: "0x" + hex.EncodeToString(hotkey), Coldkey: "0x" + hex.EncodeToString(coldkey), RegistrationBlock: &registeredAt})
		}
		emissions = binary.LittleEndian.AppendUint64(emissions, uint64(index))
	}
	fixture.set(t, "Emission", subnetTestVector(emissions, 8), netuidArg)
	window := ownerTrimWindow{Schema: ownerTrimWindowSchema, PolicyHash: "sha256:" + strings.Repeat("a", 64), FinalizedHash: testFinalizedHash,
		FinalizedNumber: 100, MortalPeriod: 8, SelectionRule: ownerTrimSubsetRule}
	return client, fixture, policy, window
}

// Production sampling can pass every observed predicate without granting any
// action authority or pretending an exact residual subset is already known.
func TestOwnerTrimBoundedReaderQualifiesConditionalSafeSet(t *testing.T) {
	client, fixture, policy, window := newOwnerTrimBoundedFixture(t)
	result, err := client.readOwnerTrimBoundedQualification(t.Context(), policy, window.PolicyHash, window, window.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ConditionalSafeSet || result.ApplyAuthority || result.ResetReady || result.FullResetCompleted ||
		result.DeathExclusive != 108 || result.MortalEraHex != "0x4200" || result.MaximumImmuneCount != 4 ||
		result.ImmunePercentage != 66 || result.ConditionalRemovals == nil || *result.ConditionalRemovals != 4 ||
		result.ConditionalResidualCount == nil || *result.ConditionalResidualCount != 2 || len(result.RequestedGenerations) != 6 ||
		len(result.RemovableGenerations) != 6 || len(result.WindowState.Coldkeys) != 9 || len(result.RequiredAssumptions) != 3 ||
		!slices.Contains(result.RequiredAssumptions, "PUBLIC_NETWORK_REGISTRATION_PRUNING_CANNOT_REMOVE_OR_REUSE_APPROVED_SUBNET_GENERATION_THROUGH_EXPIRY") ||
		!slices.Contains(result.ExecutionBlockers, "WINDOW_ASSUMPTIONS_NOT_AUTHENTICATED") || !planSha256(result.ContentHash) {
		t.Fatalf("unsafe or incomplete conditional result: %+v", result)
	}
	for _, requested := range result.RequestedGenerations {
		if requested.Disposition != "may-be-removed-or-retained" {
			t.Fatalf("invented residual prefix: %+v", requested)
		}
	}
	if fixture.count("chain_getFinalizedHead") < 3 || fixture.count("author_submitExtrinsic") != 0 {
		t.Fatal("missing final head recheck or unexpected mutation")
	}
}

// Homogeneous boundary variations cover immunity, all three epoch triggers,
// capacity arithmetic and both swap paths without scheduler timing or sleeps.
func TestOwnerTrimBoundedWindowPredicateBoundaries(t *testing.T) {
	client, _, policy, window := newOwnerTrimBoundedFixture(t)
	baseline, err := client.readOwnerTrimBoundedQualification(t.Context(), policy, window.PolicyHash, window, window.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name    string
		change  func(*subnetPreview, *ownerTrimWindowState, *subnetCensusPolicy)
		blocker string
	}{
		{name: "protected-expiry-equals-death", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) { c.ImmunityBlocks = 18 }},
		{name: "protected-expires-last-inclusion", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) { c.ImmunityBlocks = 17 }, blocker: "OWNER_TRIM_NONIMMUNE_GENERATION_OUTSIDE_APPROVED_SET:"},
		{name: "owner-outside-immune-subset", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) { c.Seats[1].OwnerImmune = false }, blocker: "OWNER_TRIM_NONIMMUNE_GENERATION_OUTSIDE_APPROVED_SET:"},
		{name: "registration-open", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) { c.RegistrationAllowed = true }, blocker: "OWNER_TRIM_REGISTRATION_NOT_CLOSED"},
		{name: "pow-registration-open", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) {
			c.PowRegistrationAllowed = true
		}, blocker: "OWNER_TRIM_REGISTRATION_NOT_CLOSED"},
		{name: "first-hotkey-swap", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			s.Coldkeys[0].LastHotkeySwapBlock = 0
		}, blocker: "OWNER_TRIM_HOTKEY_SWAP_NOT_FENCED:"},
		{name: "hotkey-cooldown-through-last-inclusion", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.HotkeySwapInterval = 17 }},
		{name: "hotkey-cooldown-ends-before-last-inclusion", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.HotkeySwapInterval = 16 }, blocker: "OWNER_TRIM_HOTKEY_SWAP_NOT_FENCED:"},
		{name: "hotkey-cooldown-saturates", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			s.HotkeySwapInterval = math.MaxUint64
		}},
		{name: "new-coldkey-swap-at-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.ColdkeySwapDelay = 7 }},
		{name: "new-coldkey-swap-before-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.ColdkeySwapDelay = 6 }, blocker: "OWNER_TRIM_NEW_COLDKEY_SWAP_CAN_MATURE_BEFORE_EXPIRY"},
		{name: "announced-coldkey-swap-at-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			when := uint64(108)
			s.Coldkeys[1].ColdkeySwapAvailableAt = &when
		}},
		{name: "announced-coldkey-swap-before-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			when := uint64(107)
			s.Coldkeys[1].ColdkeySwapAvailableAt = &when
		}, blocker: "OWNER_TRIM_ANNOUNCED_COLDKEY_SWAP_CAN_MATURE_BEFORE_EXPIRY:"},
		{name: "owner-announcement-blocks-trim", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			when := uint64(108)
			s.Coldkeys[0].ColdkeySwapAvailableAt = &when
		}, blocker: "OWNER_TRIM_OWNER_COLDKEY_HAS_SWAP_RESTRICTION"},
		{name: "automatic-epoch-before-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.LastEpochBlock = 7 }, blocker: "OWNER_TRIM_EPOCH_CAN_OCCUR_BEFORE_EXPIRY"},
		{name: "pending-epoch-before-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.PendingEpochAt = 107 }, blocker: "OWNER_TRIM_EPOCH_CAN_OCCUR_BEFORE_EXPIRY"},
		{name: "epoch-fallback-before-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.BlocksSinceLastStep = 94 }, blocker: "OWNER_TRIM_EPOCH_CAN_OCCUR_BEFORE_EXPIRY"},
		{name: "epoch-fallback-at-death", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.BlocksSinceLastStep = 93 }},
		{name: "epoch-fallback-overflow", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			s.BlocksSinceLastStep = math.MaxUint64
		}, blocker: "OWNER_TRIM_EPOCH_CAN_OCCUR_BEFORE_EXPIRY"},
		{name: "disabled-epoch", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			s.Tempo = 0
			s.PendingEpochAt = 101
			s.BlocksSinceLastStep = math.MaxUint64
		}},
		{name: "admin-freeze-enters-before-expiry", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.AdminFreezeWindow = 44 }, blocker: "OWNER_TRIM_ADMIN_WINDOW_NOT_OPEN_THROUGH_EXPIRY"},
		{name: "admin-freeze-equality-open", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.AdminFreezeWindow = 43 }},
		{name: "future-trigger-freezes-now", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) { s.PendingEpochAt = 200 }, blocker: "OWNER_TRIM_ADMIN_WINDOW_NOT_OPEN_THROUGH_EXPIRY"},
		{name: "lease-present", change: func(_ *subnetPreview, s *ownerTrimWindowState, _ *subnetCensusPolicy) {
			id := uint32(0)
			s.SubnetLeaseId = &id
		}, blocker: "OWNER_TRIM_LEASE_LIFECYCLE_NOT_FENCED"},
		{name: "prior-trim-build-rate-unknown", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) { c.Trim.OwnerLastTrimBlock = 90 }, blocker: "OWNER_TRIM_RATE_LIMIT_BUILD_VALUE_NOT_AUTHENTICATED"},
		{name: "strict-eighty-percent", change: func(_ *subnetPreview, _ *ownerTrimWindowState, p *subnetCensusPolicy) {
			m := uint16(5)
			p.TrimMaximumUids = &m
		}, blocker: "OWNER_TRIM_IMMUNITY_THRESHOLD_REACHED_DURING_WINDOW"},
		{name: "no-capacity-reduction", change: func(_ *subnetPreview, _ *ownerTrimWindowState, p *subnetCensusPolicy) {
			m := uint16(10)
			p.TrimMaximumUids = &m
		}, blocker: "OWNER_TRIM_TARGET_IS_NOT_A_PERMITTED_CAPACITY_REDUCTION"},
		{name: "below-runtime-minimum", change: func(_ *subnetPreview, _ *ownerTrimWindowState, p *subnetCensusPolicy) {
			m := uint16(1)
			p.TrimMaximumUids = &m
		}, blocker: "OWNER_TRIM_TARGET_IS_NOT_A_PERMITTED_CAPACITY_REDUCTION"},
		{name: "missing-call-schema", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) { c.Trim.Call = nil }, blocker: "OWNER_TRIM_CALL_NOT_AUTHENTICATED"},
		{name: "missing-immune-threshold", change: func(c *subnetPreview, _ *ownerTrimWindowState, _ *subnetCensusPolicy) {
			c.Trim.MaximumImmunePercentage = nil
		}, blocker: "OWNER_TRIM_IMMUNITY_THRESHOLD_NOT_AUTHENTICATED"},
	} {
		census, state, changedPolicy := baseline.Census.Observation, baseline.WindowState, policy
		census.Seats = slices.Clone(census.Seats)
		state.Coldkeys = slices.Clone(state.Coldkeys)
		item.change(&census, &state, &changedPolicy)
		result, err := qualifyOwnerTrimWindow(t.Context(), changedPolicy, window.PolicyHash, window, window.PolicyHash, census, state)
		if err != nil {
			t.Fatalf("%s: %v", item.name, err)
		}
		matched := false
		for _, blocker := range result.QualificationBlockers {
			matched = matched || strings.HasPrefix(blocker, item.blocker)
		}
		if item.blocker == "" && !result.ConditionalSafeSet || item.blocker != "" && (result.ConditionalSafeSet || !matched) || result.ApplyAuthority {
			t.Fatalf("%s: incorrect boundary: %+v", item.name, result.QualificationBlockers)
		}
	}
}

// Reordering even the lowest protected emissions cannot change safe-set
// admission. Permit/custody protection cannot be overridden by high emissions.
func TestOwnerTrimBoundedEmissionOrderDoesNotAuthorizeProtectedRisk(t *testing.T) {
	client, _, policy, window := newOwnerTrimBoundedFixture(t)
	baseline, err := client.readOwnerTrimBoundedQualification(t.Context(), policy, window.PolicyHash, window, window.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	census := baseline.Census.Observation
	for index := range census.Seats {
		census.Seats[index].emission = uint64(100 - index)
		census.Seats[index].EmissionRao = strconv.FormatUint(census.Seats[index].emission, 10)
	}
	result, err := qualifyOwnerTrimWindow(t.Context(), policy, window.PolicyHash, window, window.PolicyHash, census, baseline.WindowState)
	if err != nil || !result.ConditionalSafeSet || !reflect.DeepEqual(baseline.RemovableGenerations, result.RemovableGenerations) ||
		!reflect.DeepEqual(baseline.RequestedGenerations, result.RequestedGenerations) {
		t.Fatalf("emission ordering incorrectly changed approved set: %v %+v", err, result.QualificationBlockers)
	}
	census.Seats[5].ValidatorPermit = true
	result, err = qualifyOwnerTrimWindow(t.Context(), policy, window.PolicyHash, window, window.PolicyHash, census, baseline.WindowState)
	if err != nil || result.ConditionalSafeSet || !slices.Contains(result.QualificationBlockers, "OWNER_TRIM_NONIMMUNE_GENERATION_OUTSIDE_APPROVED_SET:"+census.Seats[5].Hotkey) {
		t.Fatal("emission ranking overrode a protected validator role")
	}
}

// Approved miners may lose immunity during the window, including just before
// the first possible inclusion. Their expiry never expands the approved set.
func TestOwnerTrimBoundedApprovedImmunityExpiryAndResiduals(t *testing.T) {
	for _, item := range []struct {
		registration uint64
		capacity     uint16
		immuneCount  uint16
		disposition  string
	}{
		{registration: 69, capacity: 6, immuneCount: 4, disposition: "may-be-removed-or-retained"},
		{registration: 75, capacity: 7, immuneCount: 5, disposition: "may-be-removed-or-retained"},
		{registration: 90, capacity: 7, immuneCount: 5, disposition: "retained-through-runtime-immunity"},
	} {
		client, fixture, policy, window := newOwnerTrimBoundedFixture(t)
		registration, capacity := item.registration, item.capacity
		fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, registration), []byte{25, 0}, []byte{4, 0})
		policy.Remove[0].RegistrationBlock = &registration
		policy.TrimMaximumUids = &capacity
		result, err := client.readOwnerTrimBoundedQualification(t.Context(), policy, window.PolicyHash, window, window.PolicyHash)
		if err != nil || !result.ConditionalSafeSet || result.MaximumImmuneCount != item.immuneCount ||
			len(result.RequestedGenerations) != len(policy.Remove) || result.RequestedGenerations[0].Disposition != item.disposition ||
			result.ConditionalResidualCount == nil || *result.ConditionalResidualCount != capacity-4 {
			t.Fatalf("expiry for registration %d: %v %+v", registration, err, result)
		}
	}
}

// Missing coverage, duplicates, future swap history and stale generations are
// integrity failures or explicit unresolved residuals, never assumed harmless.
func TestOwnerTrimBoundedRejectsIncompleteColdkeyEvidence(t *testing.T) {
	client, _, policy, window := newOwnerTrimBoundedFixture(t)
	baseline, err := client.readOwnerTrimBoundedQualification(t.Context(), policy, window.PolicyHash, window, window.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"missing", "duplicate", "foreign", "future-swap", "future-dispute"} {
		state := baseline.WindowState
		state.Coldkeys = slices.Clone(state.Coldkeys)
		switch change {
		case "missing":
			state.Coldkeys = state.Coldkeys[1:]
		case "duplicate":
			state.Coldkeys = append(state.Coldkeys, state.Coldkeys[0])
		case "foreign":
			state.Coldkeys[0].Coldkey = "0x" + strings.Repeat("7f", 32)
		case "future-swap":
			state.Coldkeys[0].LastHotkeySwapBlock = 101
		case "future-dispute":
			when := uint64(101)
			state.Coldkeys[0].ColdkeySwapDisputedAt = &when
		}
		result, err := qualifyOwnerTrimWindow(t.Context(), policy, window.PolicyHash, window, window.PolicyHash, baseline.Census.Observation, state)
		if !errors.Is(err, errRpcIntegrity) || result.ContentHash != "" {
			t.Fatalf("%s coldkey evidence accepted: %v", change, err)
		}
	}
}

// A mortal proposal cannot be immortal, rounded to another birth, unbounded,
// overflowed or silently rebound to a different policy/anchor/selection scope.
func TestOwnerTrimBoundedRejectsInvalidWindowBeforeRpc(t *testing.T) {
	for _, change := range []string{"schema", "hash", "policy", "rule", "zero-birth", "overflow", "immortal", "nonpower", "quantized"} {
		client, fixture, policy, window := newOwnerTrimBoundedFixture(t)
		switch change {
		case "schema":
			window.Schema = "unknown"
		case "hash":
			window.FinalizedHash = "0x00"
		case "policy":
			window.PolicyHash = "sha256:" + strings.Repeat("b", 64)
		case "rule":
			window.SelectionRule = "any-current-seat"
		case "zero-birth":
			window.FinalizedNumber = 0
		case "overflow":
			window.FinalizedNumber = math.MaxUint32
		case "immortal":
			window.MortalPeriod = 0
		case "nonpower":
			window.MortalPeriod = 7
		case "quantized":
			window.MortalPeriod = 8192
		}
		result, err := client.readOwnerTrimBoundedQualification(t.Context(), policy, "sha256:"+strings.Repeat("a", 64), window, "sha256:"+strings.Repeat("a", 64))
		if err == nil || result.ContentHash != "" || fixture.count("chain_getFinalizedHead") != 0 {
			t.Fatalf("%s reached RPC or produced evidence", change)
		}
	}
}

// Mortality and cooldown wire changes are refused rather than replaced with
// source defaults. Authenticated metadata cannot omit an essential predicate.
func TestOwnerTrimBoundedMetadataRefusesChangedMortalityOrCooldown(t *testing.T) {
	for _, change := range []string{"mortality-missing", "mortality-duplicate", "mortality-shape", "interval-missing", "interval-duplicate", "interval-shape", "interval-width"} {
		metadata, _, _ := rootTestMetadata(t)
		if strings.HasPrefix(change, "mortality") {
			for index, extension := range metadata.AsMetadataV14.Extrinsic.SignedExtensions {
				if extension.Identifier != "CheckMortality" {
					continue
				}
				switch change {
				case "mortality-missing":
					metadata.AsMetadataV14.Extrinsic.SignedExtensions[index].Identifier = "Other"
				case "mortality-duplicate":
					metadata.AsMetadataV14.Extrinsic.SignedExtensions = append(metadata.AsMetadataV14.Extrinsic.SignedExtensions, extension)
				case "mortality-shape":
					metadata.AsMetadataV14.Extrinsic.SignedExtensions[index].Type = extension.AdditionalSigned
				}
				break
			}
		} else {
			for index := range metadata.AsMetadataV14.Pallets {
				pallet := &metadata.AsMetadataV14.Pallets[index]
				if pallet.Name != "SubtensorModule" {
					continue
				}
				for index, constant := range pallet.Constants {
					if constant.Name != "HotkeySwapOnSubnetInterval" {
						continue
					}
					switch change {
					case "interval-missing":
						pallet.Constants[index].Name = "Other"
					case "interval-duplicate":
						pallet.Constants = append(pallet.Constants, constant)
					case "interval-shape":
						pallet.Constants[index].Type = types.NewSi1LookupTypeIDFromUInt(0)
					case "interval-width":
						pallet.Constants[index].Value = []byte{0}
					}
					break
				}
			}
		}
		if _, err := ownerTrimWindowMetadata(metadata); err == nil {
			t.Fatalf("%s accepted", change)
		}
	}
}

// Adjacent storage shapes preserve optional absence and require the exact hasher
// rather than accepting an unknown specification as an implicit wildcard.
func TestOwnerTrimBoundedStorageProfileAndExactWidths(t *testing.T) {
	metadata, _, _ := rootTestMetadata(t)
	if _, err := observationStorageProfile(metadata, ownerTrimWindowStorageSpecs); err != nil {
		t.Fatal(err)
	}
	for _, shape := range []string{"u32", "coldkey-announcement"} {
		width := 4
		if shape == "coldkey-announcement" {
			width = 36
		}
		for _, size := range []int{width - 1, width, width + 1} {
			if err := rootValidateScale(make([]byte, size), shape); (err == nil) != (size == width) {
				t.Fatalf("%s width %d was not fully consumed", shape, size)
			}
		}
	}
	for _, hasher := range []string{"identity", "blake128concat", "unrecognized"} {
		spec := rootStorageSpec{name: "ColdkeySwapAnnouncements", keys: []string{"account"}, hashers: []string{hasher}, value: "coldkey-announcement", optional: true}
		if _, err := observationStorageProfile(metadata, []rootStorageSpec{spec}); err == nil {
			t.Fatalf("incorrect hasher %q accepted", hasher)
		}
	}
	for _, change := range []string{"query", "hash", "value"} {
		changed, _, _ := rootTestMetadata(t)
		for palletIndex := range changed.AsMetadataV14.Pallets {
			pallet := &changed.AsMetadataV14.Pallets[palletIndex]
			if pallet.Name != "SubtensorModule" {
				continue
			}
			for index := range pallet.Storage.Items {
				entry := &pallet.Storage.Items[index]
				if entry.Name != "ColdkeySwapAnnouncements" {
					continue
				}
				switch change {
				case "query":
					entry.Modifier.IsOptional = false
					entry.Modifier.IsDefault = true
				case "hash":
					entry.Type.AsMap.Hashers[0] = types.StorageHasherV10{IsIdentity: true}
				case "value":
					entry.Type.AsMap.Value = entry.Type.AsMap.Key
				}
			}
		}
		if _, err := observationStorageProfile(changed, ownerTrimWindowStorageSpecs); err == nil {
			t.Fatalf("changed %s admitted", change)
		}
	}
}

// Unknown profile hashers must fail closed, even when every runtime key/value
// shape matches. The former two-hasher condition treated this as a wildcard.
func TestObservationStorageProfileRejectsUnknownHasher(t *testing.T) {
	metadata, _, _ := rootTestMetadata(t)
	spec := rootStorageSpec{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"unknown"}, value: "bool"}
	if _, err := observationStorageProfile(metadata, []rootStorageSpec{spec}); err == nil {
		t.Fatal("unknown profile hasher bypassed exact metadata matching")
	}
}

// Deterministic transport transitions invalidate an otherwise favorable result
// at each observation boundary; neither an earlier good result nor partial data leaks.
func TestOwnerTrimBoundedStaleInterruptedAndInconsistentReads(t *testing.T) {
	for _, change := range []string{"stale-anchor", "stale-head", "fork", "metadata", "same-hash-timing", "interrupted"} {
		client, fixture, policy, window := newOwnerTrimBoundedFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		var windowRead atomic.Bool
		blocksKey, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "BlocksSinceLastStep", []byte{25, 0})
		if err != nil {
			t.Fatal(err)
		}
		tempoKey, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "Tempo", []byte{25, 0})
		if err != nil {
			t.Fatal(err)
		}
		if change == "stale-anchor" {
			window.FinalizedNumber--
		}
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			var call struct {
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(raw, &call); err != nil {
				return nil, err
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+blocksKey.Hex()+`"` {
				windowRead.Store(true)
				if change == "interrupted" {
					cancel()
				}
			}
			response, err := fixture.roundTrip(request)
			if err != nil {
				return response, err
			}
			var replace any
			switch {
			case change == "stale-head" && windowRead.Load() && call.Method == "chain_getFinalizedHead":
				replace = "0x" + strings.Repeat("d", 64)
			case change == "fork" && windowRead.Load() && call.Method == "chain_getBlockHash":
				replace = "0x" + strings.Repeat("d", 64)
			case change == "metadata" && call.Method == "state_getMetadata" && fixture.count("state_getMetadata") == 2:
				replace = "0x00"
			case change == "same-hash-timing" && fixture.count("state_getMetadata") >= 2 && call.Method == "state_getStorage" && string(call.Params[0]) == `"`+tempoKey.Hex()+`"`:
				replace = "0x6500"
			}
			if replace != nil {
				response.Body.Close()
				encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": replace})
				if err != nil {
					return nil, err
				}
				response.Body = io.NopCloser(bytes.NewReader(encoded))
			}
			return response, nil
		})
		result, err := client.readOwnerTrimBoundedQualification(ctx, policy, window.PolicyHash, window, window.PolicyHash)
		cancel()
		if err == nil || result.ContentHash != "" || result.ConditionalSafeSet {
			t.Fatalf("%s published partial/stale qualification: %v", change, err)
		}
		if change != "interrupted" && !errors.Is(err, errRpcIntegrity) {
			t.Fatalf("%s failed for unexpected reason: %v", change, err)
		}
	}
}

// Unknown newcomers and changed generations remain unapproved even when their
// emission makes the old fixed-prefix prediction appear attractive.
func TestOwnerTrimBoundedConcurrentRegistrationAndCustodyDrift(t *testing.T) {
	for _, change := range []string{"newcomer", "coldkey", "generation", "custody-role"} {
		client, fixture, policy, window := newOwnerTrimBoundedFixture(t)
		hotkey := bytes.Repeat([]byte{0x46}, 32)
		switch change {
		case "newcomer":
			policy.Remove = policy.Remove[:1]
		case "coldkey":
			newColdkey := bytes.Repeat([]byte{0x7f}, 32)
			fixture.set(t, "Owner", newColdkey, hotkey)
			fixture.set(t, "LastHotkeySwapOnNetuid", binary.LittleEndian.AppendUint64(nil, 90), []byte{25, 0}, newColdkey)
		case "generation":
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 46), []byte{25, 0}, []byte{5, 0})
		case "custody-role":
			policy.Preserve = append(policy.Preserve, subnetProtectedIdentity{subnetIdentityExpectation: policy.Remove[1], Roles: []string{"custody"}})
			policy.Remove = append(policy.Remove[:1:1], policy.Remove[2:]...)
		}
		result, err := client.readOwnerTrimBoundedQualification(t.Context(), policy, window.PolicyHash, window, window.PolicyHash)
		if err != nil || result.ConditionalSafeSet || result.ApplyAuthority {
			t.Fatalf("%s was admitted or unexpectedly failed reading: %v %+v", change, err, result.QualificationBlockers)
		}
		if len(result.RequestedGenerations) != len(policy.Remove) {
			t.Fatalf("%s lost requested old generations", change)
		}
		if change == "coldkey" || change == "generation" {
			if result.RequestedGenerations[1].Disposition != "unresolved-original-generation" || result.RequestedGenerations[1].ObservedUid != nil {
				t.Fatalf("%s falsely resolved an old generation", change)
			}
		}
	}
}

// The actual command accepts no proof/health booleans or execution flags; output
// zero is only a conditional qualification with both input byte hashes retained.
func TestOwnerTrimBoundedCommandRemainsSignerFree(t *testing.T) {
	_, fixture, policy, window := newOwnerTrimBoundedFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response, err := fixture.roundTrip(request)
		if err != nil {
			http.Error(writer, err.Error(), 500)
			return
		}
		defer response.Body.Close()
		writer.WriteHeader(response.StatusCode)
		io.Copy(writer, response.Body)
	}))
	defer server.Close()
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(policyRaw)
	window.PolicyHash = "sha256:" + hex.EncodeToString(digest[:])
	windowRaw, err := json.Marshal(window)
	if err != nil {
		t.Fatal(err)
	}
	policyPath, windowPath := filepath.Join(t.TempDir(), "policy.json"), filepath.Join(t.TempDir(), "window.json")
	if err := os.WriteFile(policyPath, policyRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(windowPath, windowRaw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"owner-trim-qualify", "--rpc", server.URL, "--policy", policyPath, "--window", windowPath}
	var stdout, stderr bytes.Buffer
	code := runMain(t.Context(), args, &stdout, &stderr)
	var result ownerTrimBoundedQualification
	if code != 0 || json.Unmarshal(stdout.Bytes(), &result) != nil || !result.ConditionalSafeSet || result.ApplyAuthority || result.Window.PolicyHash != window.PolicyHash {
		t.Fatalf("conditional command: %d %s %s", code, &stdout, &stderr)
	}
	eraRaw, err := hex.DecodeString(result.MortalEraHex[2:])
	var era types.ExtrinsicEra
	if err != nil || codec.Decode(eraRaw, &era) != nil || !era.IsMortalEra || era.IsImmortalEra {
		t.Fatal("proposed era is not canonical mortal SCALE")
	}
	for _, flag := range []string{"--apply", "--signer", "--key", "--custody-fenced"} {
		stdout.Reset()
		stderr.Reset()
		before := fixture.count("chain_getFinalizedHead")
		if code := runMain(t.Context(), append(slices.Clone(args), flag), &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("chain_getFinalizedHead") != before {
			t.Fatalf("execution flag %s was admitted", flag)
		}
	}
	for _, raw := range [][]byte{
		append(append(slices.Clone(windowRaw[:len(windowRaw)-1]), []byte(`,"custody_fenced":true`)...), '}'),
		append(append(slices.Clone(windowRaw[:len(windowRaw)-1]), []byte(`,"mortal_period_blocks":8`)...), '}'),
	} {
		if err := os.WriteFile(windowPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		stdout.Reset()
		stderr.Reset()
		before := fixture.count("chain_getFinalizedHead")
		if code := runMain(t.Context(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("chain_getFinalizedHead") != before {
			t.Fatal("caller evidence boolean or duplicate JSON reached RPC")
		}
	}
}
