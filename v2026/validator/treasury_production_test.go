//go:build linux || darwin

// Generated public custody and actual signed provider/approval/intent fixtures
// exercise the treasury successor without live identities or chain mutation.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Setup occurs before the independent final production signature. Original
// provider proof bytes and validator identities remain those actually replayed.
func configureTreasuryProductionTest(t *testing.T, fixture *ownerRecycleProductionTestFixture) {
	t.Helper()
	policy := &TreasuryPolicy{Schema: TreasuryPolicySchema, Threshold: 2,
		Signatories: [][32]byte{{0x51}, {0x52}, {0x53}}, ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10},
		TreasuryShare: protocol.Rational{Numerator: 9, Denominator: 10}, MaxWeightLimitU16: 32768}
	var err error
	policy.MultisigAccount, err = crv4.DeriveNativeMultisigAccount(policy.Signatories, policy.Threshold)
	if err != nil {
		t.Fatal(err)
	}
	configureTreasuryProductionTestWithPolicy(t, fixture, policy)
}

// Both explicit public policy schemas use the same authenticated native
// ownership, exact roster and independent production-approval fixture.
func configureTreasuryProductionTestWithPolicy(t *testing.T, fixture *ownerRecycleProductionTestFixture, policy *TreasuryPolicy) {
	t.Helper()
	measurement := fixture.operator.measurement
	admission, cfg := measurement.admission, fixture.cfg
	// Only the masks are read here; a reserve-only fixture has no provider row.
	options := measurement.provider.options(t)
	options.treasuryReserveFallback = true
	_, provider, err := DecodeReleaseMeasurementArtifactV2(t.Context(), measurement.encoded, options)
	if err != nil {
		t.Fatal(err)
	}
	excludedKVs := map[uint16]bool{}
	for _, uid := range append(slices.Clone(provider.Decision.MaskedUIDs), fixture.validatorUids...) {
		excludedKVs[uid] = true
	}
	for _, owner := range measurement.authority.observation.RecognizedOwners {
		excludedKVs[owner.Uid] = true
	}
	for _, pool := range measurement.provider.artifact.Pools {
		excludedKVs[pool.UID] = true
	}
	for _, binding := range measurement.provider.artifact.Bindings {
		if binding.LiveUIDFound {
			excludedKVs[binding.LiveUID] = true
		}
	}
	for _, registration := range measurement.authority.observation.Snapshot.Registrations {
		if !excludedKVs[registration.Uid] && len(policy.Recipients) < 2 {
			policy.Recipients = append(policy.Recipients, TreasuryRecipient{Uid: registration.Uid, Hotkey: registration.Hotkey, RegistrationBlock: registration.RegistrationBlock})
		}
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	// Older compatible producer metadata may omit this unused optional map.
	// Add its actual source shape under a new synthetic portable id when needed.
	entries, err := ownerRecycleStorageProfile(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := entries["AutoStakeDestination"]; !exists {
		id := types.NewSi1LookupTypeIDFromUInt(30000)
		keyEntry := entries["Keys"]
		keys := fixture.metadata.AsMetadataV14.EfficientLookup[keyEntry.Type.AsMap.Key.Int64()]
		if keys == nil || !keys.Def.IsTuple || len(keys.Def.Tuple) != 2 {
			t.Fatal("synthetic key metadata is not an exact tuple")
		}
		fixture.metadata.AsMetadataV14.Lookup.Types = append(fixture.metadata.AsMetadataV14.Lookup.Types, types.PortableTypeV14{ID: id,
			Type: types.Si1Type{Def: types.Si1TypeDef{IsTuple: true, Tuple: types.Si1TypeDefTuple{entries["Owner"].Type.AsMap.Key, keys.Def.Tuple[0]}}}})
		entry := types.StorageEntryMetadataV14{Name: "AutoStakeDestination", Modifier: types.StorageFunctionModifierV0{IsOptional: true},
			Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsBlake2_128Concat: true}, {IsIdentity: true}}, Key: id, Value: entries["Owner"].Type.AsMap.Value}}, Fallback: []byte{0}}
		for index := range fixture.metadata.AsMetadataV14.Pallets {
			if fixture.metadata.AsMetadataV14.Pallets[index].Name == crv4.PalletName {
				fixture.metadata.AsMetadataV14.Pallets[index].Storage.Items = append(fixture.metadata.AsMetadataV14.Pallets[index].Storage.Items, entry)
			}
		}
	}
	encoded, err := codec.EncodeToHex(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadata, digest, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	*fixture.metadata = *metadata
	admission.metadata, cfg.RuntimeMetadataHash = encoded, digest
	cfg.RuntimeSpec, admission.version.SpecVersion = 470, 470
	admission.approval.Proposal.Runtime.MetadataHash, _ = parseHash32("synthetic treasury metadata", digest)
	admission.approval.Proposal.Runtime.Version.SpecVersion = 470
	admission.approval.Proposal.Runtime.SourceCommit = ownerRecycleSourceCommit470
	admission.approval.Schema = TreasuryApprovalSchema
	admission.approval.Proposal.Schema, admission.approval.Proposal.Treasury = TreasuryProposalSchema, policy
	admission.approval.Proposal.Remainder, admission.approval.Proposal.OwnerAllocation = "ordinary_treasury_credit", "equal_exact_registered"
	admission.approval.Production.Schema = TreasuryProductionScope
	selection := *cfg.OwnerRecycleApproval
	selection.Approval = ReleaseEvidenceV2File{Path: filepath.Join(filepath.Dir(selection.Approval.Path), "treasury-approval.json")}
	cfg.TreasuryApproval, cfg.OwnerRecycleApproval = &selection, nil
	for _, recipient := range policy.Recipients {
		treasuryProductionTestPut(t, fixture, "Owner", policy.MultisigAccount[:], recipient.Hotkey[:])
	}
	netuid := binary.LittleEndian.AppendUint16(nil, cfg.Netuid)
	treasuryProductionTestPut(t, fixture, "AutoStakeDestination", nil, policy.MultisigAccount[:], netuid)
}

// Changes only a raw SCALE storage value; no observer result is fabricated.
func treasuryProductionTestPut(t *testing.T, fixture *ownerRecycleProductionTestFixture, name string, value []byte, args ...[]byte) {
	t.Helper()
	key, err := types.CreateStorageKey(fixture.metadata, crv4.PalletName, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	var wire any
	if value != nil {
		wire = codec.HexEncodeToString(value)
	}
	fixture.operator.measurement.admission.storage[key.Hex()] = wire
}

// The existing producer fixture still signs and loads through actual gates.
func newTreasuryProductionTestFixture(t *testing.T) *ownerRecycleProductionTestFixture {
	t.Helper()
	return newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
		configureTreasuryProductionTest(t, fixture)
	})
}

// Healthy preparation uses the original provider transcript, exact 45/45
// treasury scores, unchanged masks and two independent hotkey signatures.
func TestTreasuryProductionPreparesExactMeasuredSuccessor(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	measurement := fixture.operator.measurement
	original := bytes.Clone(measurement.encoded)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, intent, original, measurement.provider.artifact, provider)
	if err != nil || intent.Treasury == nil || intent.OwnerRecycle != nil || stage.proof.Schema != treasuryProductionDecisionSchema {
		t.Fatalf("treasury actual measured intent lost its distinct authority: %v", err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, measurement.provider.artifact, verified); err != nil {
		t.Fatal(err)
	}
	policy := measurement.admission.approval.Proposal.Treasury
	if !slices.Equal(stage.proof.Row.TreasuryUids, []uint16{policy.Recipients[0].Uid, policy.Recipients[1].Uid}) || len(stage.proof.Row.OwnerUids) != 0 {
		t.Fatal("treasury row relabeled owners or lost the complete roster")
	}
	providerSum := new(big.Rat)
	for index, uid := range verified.UIDs {
		if slices.Contains(stage.proof.Row.TreasuryUids, uid) {
			if verified.Scores[index].Cmp(big.NewRat(9, 20)) != 0 {
				t.Fatal("treasury recipient did not receive exactly 45 percent")
			}
		} else {
			providerSum.Add(providerSum, verified.Scores[index])
		}
	}
	if providerSum.Cmp(big.NewRat(1, 10)) != 0 || !bytes.Equal(original, measurement.encoded) ||
		!slices.Equal(provider.MaskedUIDs, verified.MaskedUIDs) || stage.sourceHash == ownerRecycleProductionSourceHash(original, stage.encoded) {
		t.Fatal("treasury successor changed provider evidence, masks, split or reused the owner source domain")
	}
	if err := validateOwnerRecyclePreparedAuthorization(fixture.cfg, intent.Prepared); err == nil {
		t.Fatal("signed policy alone granted durable submission authority")
	}
}

// Each raw native fault is refused by the actual public pinned census. Restored
// storage is a positive control and never reuses a successful observer cache.
func TestTreasuryAdmissionRejectsRecipientAndOwnerDrift(t *testing.T) {
	testTreasuryAdmissionRejectsRecipientAndOwnerDrift(t, newTreasuryProductionTestFixture(t))
}

// Each explicit policy schema retains the same production native admission.
func testTreasuryAdmissionRejectsRecipientAndOwnerDrift(t *testing.T, fixture *ownerRecycleProductionTestFixture) {
	t.Helper()
	admission := fixture.operator.measurement.admission
	policy := admission.approval.Proposal.Treasury
	recipient := policy.Recipients[0]
	netuid, uid := binary.LittleEndian.AppendUint16(nil, fixture.cfg.Netuid), binary.LittleEndian.AppendUint16(nil, recipient.Uid)
	original := maps.Clone(admission.storage)
	for _, fault := range []string{"owner", "generation", "uid", "owner-set", "explicit-owner", "subnet-owner", "auto-stake"} {
		admission.storage = maps.Clone(original)
		switch fault {
		case "owner":
			other := recycleTestId(6100)
			treasuryProductionTestPut(t, fixture, "Owner", other[:], recipient.Hotkey[:])
		case "generation":
			treasuryProductionTestPut(t, fixture, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, recipient.RegistrationBlock+1), netuid, uid)
		case "uid":
			treasuryProductionTestPut(t, fixture, "Uids", binary.LittleEndian.AppendUint16(nil, recipient.Uid+1), netuid, recipient.Hotkey[:])
		case "owner-set":
			owned := append(slices.Clone(admission.approval.OwnerHotkeys), recipient.Hotkey)
			raw := releaseNativeValidatorTestCompact(t, uint64(len(owned)))
			for _, hotkey := range owned {
				raw = append(raw, hotkey[:]...)
			}
			treasuryProductionTestPut(t, fixture, "OwnedHotkeys", raw, admission.approval.SubnetOwner[:])
		case "explicit-owner":
			treasuryProductionTestPut(t, fixture, "SubnetOwnerHotkey", recipient.Hotkey[:], netuid)
		case "subnet-owner":
			treasuryProductionTestPut(t, fixture, "SubnetOwner", policy.MultisigAccount[:], netuid)
		case "auto-stake":
			other := recycleTestId(6101)
			treasuryProductionTestPut(t, fixture, "AutoStakeDestination", other[:], policy.MultisigAccount[:], netuid)
		}
		got, err := ObserveTreasuryAdmissionAt(t.Context(), fixture.cfg, admission.chain, [32]byte(admission.finalized))
		if got != nil || err == nil || retryableProductionSteeringRead(err) {
			t.Fatalf("%s native drift admitted a treasury census: %v", fault, err)
		}
	}
	admission.storage = original
	got, err := ObserveTreasuryAdmission(t.Context(), fixture.cfg, admission.chain)
	if err != nil || !reflect.DeepEqual(got.TreasuryRecipients, policy.Recipients) {
		t.Fatalf("restored ordinary treasury census did not recover: %v", err)
	}
}

// The actual measured derivation refuses a mask instead of reallocating its
// missing share, and keeps every original provider score independently intact.
func TestTreasuryMeasuredRowRefusesMaskedOrMissingRecipient(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	measurement := fixture.operator.measurement
	for _, fault := range []string{"mask", "missing", "provider-overlap"} {
		candidate := *provider
		authority := *stage.authority
		authority.observation = stage.authority.observation
		uid := stage.proof.Row.TreasuryUids[0]
		switch fault {
		case "mask":
			candidate.MaskedUIDs = append(slices.Clone(candidate.MaskedUIDs), uid)
		case "missing":
			authority.observation.TreasuryRecipients = slices.Clone(authority.observation.TreasuryRecipients[1:])
		case "provider-overlap":
			candidate.UIDs = slices.Clone(candidate.UIDs)
			candidate.UIDs[0] = uid
		}
		row, err := deriveOwnerRecycleMeasuredRow(&authority, measurement.provider.artifact, &candidate)
		if err == nil || len(row.Uids) != 0 {
			t.Fatalf("%s changed complete treasury roster or provider score authority: %v", fault, err)
		}
	}
}

// Signed native-policy projection authenticates exact bytes and independent
// signer; neither an old owner signature nor changed public custody can pass.
func TestTreasuryApprovalProjectionBindsOriginalScopeAndPolicy(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	admission := fixture.operator.measurement.admission
	public := [32]byte(admission.private.Public().(ed25519.PublicKey))
	reference := fixture.cfg.TreasuryApproval.Approval
	envelope, err := VerifyTreasuryApproval(admission.raw, public, reference.SHA256)
	if err != nil || !reflect.DeepEqual(envelope.Approval, admission.approval) {
		t.Fatalf("complete signed treasury scope projection differs: %v", err)
	}
	for _, fault := range []string{"bytes", "signer", "hash", "old-domain", "old-signature-domain", "policy"} {
		raw, signer, hash := bytes.Clone(admission.raw), public, reference.SHA256
		switch fault {
		case "bytes":
			raw = append(raw, '\n')
		case "signer":
			signer[0] ^= 1
		case "hash":
			hash = attemptHex32([32]byte{1})
		case "old-domain", "old-signature-domain", "policy":
			var changed TreasuryApprovalEnvelope
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			if fault == "old-domain" {
				changed.Approval.Schema = ownerRecycleProductionApprovalSchema
			} else if fault == "old-signature-domain" {
				body, _ := json.Marshal(changed.Approval)
				digest := sha256.Sum256(append([]byte("urnetwork-owner-recycle-production-approval-signature-v2\n"), body...))
				changed.Signature = hex.EncodeToString(ed25519.Sign(admission.private, digest[:]))
			} else {
				changed.Approval.Proposal.Treasury.Recipients[0].RegistrationBlock++
			}
			raw, _ = json.Marshal(changed)
			raw = append(raw, '\n')
			hash = attemptHex32(sha256.Sum256(raw))
		}
		if got, err := VerifyTreasuryApproval(raw, signer, hash); got != nil || err == nil {
			t.Fatalf("%s acquired authenticated treasury scope", fault)
		}
	}
}

// A valid treasury signature cannot be relabeled as owner authority, coexist
// with another sidecar or authorize a prepared row/source from another policy.
func TestTreasuryProductionRejectsSidecarRouteAndPreparedSubstitution(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	valid := fixture.intent(t, stage, provider)
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	measurement := fixture.operator.measurement
	for _, fault := range []string{"absent", "owner-route", "both-routes", "owner-schema", "owner-source", "approval", "census", "roster", "signature", "prepared-row"} {
		var candidate SteeringIntent
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "absent":
			candidate.Treasury = nil
		case "owner-route":
			candidate.OwnerRecycle, candidate.Treasury = candidate.Treasury, nil
		case "both-routes":
			candidate.OwnerRecycle = candidate.Treasury
		case "owner-schema":
			candidate.Treasury.Proof.Schema = ownerRecycleProductionDecisionSchema
		case "owner-source":
			candidate.Prepared.SourceCommitment.Hash = releaseHex32(ownerRecycleProductionSourceHash(measurement.encoded, stage.encoded))
		case "approval":
			candidate.Treasury.Proof.Approval[0] ^= 1
		case "census":
			candidate.Treasury.Proof.Census[0] ^= 1
		case "roster":
			candidate.Treasury.Proof.Row.TreasuryUids = candidate.Treasury.Proof.Row.TreasuryUids[:1]
		case "signature":
			candidate.Treasury.Signature = "0x" + strings.Repeat("00", 64)
		case "prepared-row":
			candidate.Prepared.Values[0]++
		}
		if got, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, &candidate, measurement.encoded, measurement.provider.artifact, provider); got != nil || err == nil {
			t.Fatalf("%s acquired a different economic row or authority", fault)
		}
	}
}

// Actual durable begin, submitted-byte acknowledgement loss, restart and
// receipt/application reconciliation retain one immutable treasury sidecar.
func TestTreasuryProductionSubmissionRecoveryKeepsOriginalIntent(t *testing.T) {
	fixture := newProductionContinuationTestFixtureWithStartup(t, nil, func(production *ownerRecycleProductionTestFixture, _ *productionContinuationTestFixture) {
		configureTreasuryProductionTest(t, production)
	})
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	if pending.Treasury == nil || pending.OwnerRecycle != nil {
		t.Fatal("treasury submission lost its explicit sidecar")
	}
	before, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	native.bodyError = context.DeadlineExceeded
	var wait *productionSteeringReadWait
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &wait) {
		t.Fatalf("treasury receipt outage lost its original pending owner: %v", err)
	}
	fixture.restart(t)
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(before, after) || len(native.broadcasts) != 1 {
		t.Fatalf("treasury restart changed original signed bytes or rebroadcast: %v", err)
	}
	native.bodyError, native.receiptNumber, fixture.production.head = nil, 101, 102
	fixture.production.extrinsicsKVs = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("treasury original receipt did not reconcile: %v", err)
	}
	finalized := fixture.restart(t)
	native.applied, fixture.production.head = true, max(uint64(102), finalized.RevealBlock)
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("treasury applied native row did not reconcile: %v", err)
	}
	applied := fixture.restart(t)
	if applied.Status != "applied" || applied.Treasury == nil || applied.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || len(native.broadcasts) != 1 {
		t.Fatal("treasury recovery lost original transaction, sidecar or one-send boundary")
	}
}

// Omitted successor fields and original signature/source domains remain exact.
func TestTreasurySuccessorPreservesOwnerRecycleWireDomains(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	admission := fixture.operator.measurement.admission
	for _, value := range []any{fixture.cfg, admission.approval, stage.proof, intent} {
		raw, err := json.Marshal(value)
		if err != nil || bytes.Contains(bytes.ToLower(raw), []byte("\"treasury")) {
			t.Fatalf("legacy wire acquired a treasury field: %v", err)
		}
	}
	proposal, _ := json.Marshal(admission.approval.Proposal)
	proposalHash, err := admission.approval.Proposal.Hash(fixture.cfg.Policy)
	if err != nil || proposalHash != sha256.Sum256(proposal) {
		t.Fatal("original owner proposal hash domain changed")
	}
	body, _ := json.Marshal(admission.approval)
	expected := sha256.Sum256(append([]byte("urnetwork-owner-recycle-production-approval-signature-v2\n"), body...))
	actual, err := admission.approval.SigningMessage()
	if err != nil || !bytes.Equal(actual, expected[:]) {
		t.Fatal("original owner approval signature bytes changed")
	}
	if got, err := releaseIntentNativeSourceHash(t.Context(), fixture.operator.measurement.encoded, intent); err != nil || got != stage.sourceHash {
		t.Fatalf("original owner source hash changed: %v", err)
	}
	if !strings.Contains(string(stage.encoded), ownerRecycleProductionDecisionSchema) {
		t.Fatal("legacy proof was relabeled")
	}
}

// An explicit signed economic transition retains the original authority bundle
// and replays its real owner intent before admitting a new drained treasury row.
func TestTreasuryProductionTransitionRetainsOriginalOwnerAuthority(t *testing.T) {
	rpcFixture := &productionStartupTestFixture{latestRead: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(rpcFixture.serveNative))
	t.Cleanup(server.Close)
	continuation := newProductionContinuationTestFixtureWithStartup(t, nil, func(production *ownerRecycleProductionTestFixture, _ *productionContinuationTestFixture) {
		endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
		production.cfg.Substrate = []string{endpoint}
		production.operator.measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).route = endpoint
	})
	fixture := continuation.production
	oldStage, oldProvider := fixture.stage(t)
	native := installProductionContinuationNative(t, continuation)
	rpcFixture.continuation, rpcFixture.native = continuation, native
	pending := continuation.beginAndLoseAcknowledgement(t, native)
	fixture.extrinsicsKVs, fixture.head = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}, 102
	if err := continuation.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("actual original owner receipt did not reconcile before transition: %v", err)
	}
	finalized := continuation.restart(t)
	native.applied, fixture.head = true, max(uint64(102), finalized.RevealBlock)
	var appliedTransition *productionSteeringTransition
	if err := continuation.steerer.submitOnceV2(t.Context()); !errors.As(err, &appliedTransition) || appliedTransition.revealWait {
		t.Fatalf("actual original owner application did not finish before transition: %v", err)
	}
	oldIntent := continuation.restart(t)
	if oldIntent.Status != "applied" {
		t.Fatal("original owner intent is not actually applied")
	}
	originalDisk, err := os.ReadFile(continuation.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	activationBlock := fixture.head + 1
	measurement, admission := fixture.operator.measurement, fixture.operator.measurement.admission
	originalCfg := *fixture.cfg
	originalConfig, _ := json.Marshal(&originalCfg)
	originalApproval := bytes.Clone(admission.raw)
	originalIntent, _ := json.Marshal(oldIntent)
	originalMeasurement := bytes.Clone(measurement.encoded)
	originalMetadata, originalVersion, originalStorage := admission.metadata, admission.version, maps.Clone(admission.storage)
	bundle, err := BuildOwnerRecycleProductionAuthority(t.Context(), &originalCfg)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(identityTestStateDir(t), "original-owner-authority.json"), bundle, maximumProductionAuthorityBundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	configureTreasuryProductionTest(t, fixture)
	cfg := fixture.cfg
	cfg.ProductionAuthorityHistory = []ReleaseEvidenceV2File{reference}
	admission.approval.Proposal.PolicyId++
	admission.approval.Proposal.EffectiveEpoch++
	admission.approval.FirstNativeEpoch++
	admission.approval.ValidFromNativeBlock = activationBlock
	admission.approval.Production.ActivationNativeBlock = activationBlock
	admission.approval.Production.ActivationNativeHash = [32]byte(fixture.block(activationBlock))
	fixture.head, fixture.epoch = activationBlock, admission.approval.FirstNativeEpoch
	admission.approval.ConfigHash, err = TreasuryConfigHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admission.sign(t)
	if err := loadOwnerRecycleProductionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionAuthorityHistory(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := RetainTreasuryApproval(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	client := native.validatorRuntimeIdentityTestClient
	previousCall := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		historical := false
		if len(args) != 0 {
			for number := uint64(100); number < activationBlock; number++ {
				if args[len(args)-1] == fixture.block(number).Hex() {
					historical = true
					break
				}
			}
		}
		if historical {
			var value any
			handled := true
			switch method {
			case "state_getMetadata":
				value = originalMetadata
			case "state_getRuntimeVersion":
				value = map[string]any{"specName": originalVersion.SpecName, "specVersion": originalVersion.SpecVersion,
					"transactionVersion": originalVersion.TransactionVersion, "stateVersion": originalVersion.StateVersion, "apis": []any{[]any{"0x8375104b299b74c5", 2}}}
			case "state_getStorage":
				value, handled = originalStorage[args[0].(string)]
			default:
				handled = false
			}
			if handled {
				raw, err := json.Marshal(value)
				if err != nil {
					return err
				}
				return json.Unmarshal(raw, target)
			}
		}
		return previousCall(ctx, target, method, args...)
	}
	historicalCall := client.callContext
	var currentMetadataReads atomic.Int32
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if (method == "state_getMetadata" || method == "state_getRuntimeVersion") && len(args) == 1 && args[0] == fixture.block(activationBlock).Hex() {
			currentMetadataReads.Add(1)
			return errors.New("synthetic current-only treasury metadata outage")
		}
		return historicalCall(ctx, target, method, args...)
	}
	readOnly, err := dialProductionNativeHistory(t.Context(), cfg)
	if err != nil {
		t.Fatalf("historical startup required unrelated new treasury metadata: %v", err)
	}
	readOnly.API.Client.Close()
	if currentMetadataReads.Load() != 0 {
		t.Fatal("historical startup selected the new treasury activation")
	}
	client.callContext = historicalCall
	continuation.runtime.cfg = *cfg
	continuation.runtime.history.cfg = *cfg
	continuation.steerer.cfg = &continuation.runtime.cfg
	restored := continuation.restart(t)
	restoredDisk, err := os.ReadFile(continuation.steerer.intents.path)
	if err != nil || restored.OwnerRecycle == nil || restored.Treasury != nil || restored.Status != "applied" || !bytes.Equal(originalDisk, restoredDisk) {
		t.Fatalf("actual treasury-config restart changed the original owner intent or first epoch: %v", err)
	}
	original, err := productionConfigForIntent(cfg, oldIntent)
	if err != nil || original == cfg || !original.ownerRecycleProduction.historicalOnly || original.TreasuryApproval != nil {
		t.Fatalf("treasury successor did not retain exact read-only original authority: %v", err)
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(t.Context(), originalMeasurement, cfg.EvidenceV2.Bounds.MaxArtifactBytes, cfg.EvidenceV2.Bounds.MaxOperators)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := prepareOwnerRecycleProductionDecision(t.Context(), original, admission.chain, fixture.operator.chain,
		originalMeasurement, artifact, oldProvider, measurement.provider.options(t))
	if err != nil || !bytes.Equal(replayed.encoded, oldStage.encoded) {
		t.Fatalf("treasury history relabeled original owner proof: %v", err)
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), original, replayed, oldIntent, originalMeasurement, artifact, oldProvider); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "finalized", "failed"} {
		candidate := *oldIntent
		candidate.Status = status
		if err := requireOwnerRecycleProductionFirstIntent(cfg, &candidate, fixture.epoch); err == nil {
			t.Fatalf("%s original liability authorized an economic transition", status)
		}
	}
	if err := requireOwnerRecycleProductionFirstIntent(cfg, oldIntent, fixture.epoch+1); err == nil {
		t.Fatal("treasury transition missed its approved drained first epoch")
	}
	if err := requireOwnerRecycleProductionFirstIntent(cfg, oldIntent, fixture.epoch); err != nil {
		t.Fatal(err)
	}
	measurement.provider.artifact.SubnetEpoch = fixture.epoch
	measurement.provider.artifact.NativeSnapshotBlock, measurement.provider.artifact.NativeSnapshotHash = activationBlock, fixture.block(activationBlock).Hex()
	measurement.provider.artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(originalMeasurement)
	for index := range measurement.provider.artifact.Inputs {
		measurement.provider.artifact.Inputs[index].CutNativeBlock = activationBlock
		measurement.provider.artifact.Inputs[index].CutNativeBlockHash = fixture.block(activationBlock).Hex()
	}
	measurement.encoded, _, err = SealReleaseMeasurementArtifactV2(t.Context(), measurement.provider.artifact, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	next, nextProvider := fixture.stage(t)
	nextIntent := fixture.intent(t, next, nextProvider)
	if next.proof.Eligibility.ActivationBlock != activationBlock || next.proof.Eligibility.ActivationNativeEpoch != fixture.epoch || nextIntent.Treasury == nil {
		t.Fatal("treasury first row did not use its independently approved drained activation")
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), cfg, next, nextIntent, measurement.encoded, measurement.provider.artifact, nextProvider); err != nil {
		t.Fatal(err)
	}
	retainedApproval, err := os.ReadFile(originalCfg.OwnerRecycleApproval.Approval.Path)
	if err != nil || !bytes.Equal(retainedApproval, originalApproval) {
		t.Fatalf("treasury transition changed original approval bytes: %v", err)
	}
	afterConfig, _ := json.Marshal(&originalCfg)
	afterIntent, _ := json.Marshal(oldIntent)
	if !bytes.Equal(originalConfig, afterConfig) || !bytes.Equal(originalIntent, afterIntent) {
		t.Fatal("treasury transition rewrote original config or intent bytes")
	}
	if _, err := BuildTreasuryProductionAuthority(t.Context(), cfg); err != nil {
		t.Fatalf("treasury authority with retained original history could not be exported: %v", err)
	}
}

// The new custody reads borrow the same original read deadline and selected
// hash. Missing evidence stays pending; a mixed integrity cause stays hard.
func TestTreasuryAdmissionReadFailurePreservesOwnerAndCause(t *testing.T) {
	for _, mode := range []string{"recover", "deadline", "mixed"} {
		fixture := newTreasuryProductionTestFixture(t)
		admission := fixture.operator.measurement.admission
		policy := admission.approval.Proposal.Treasury
		key, err := types.CreateStorageKey(fixture.metadata, crv4.PalletName, "Owner", policy.Recipients[0].Hotkey[:])
		if err != nil {
			t.Fatal(err)
		}
		parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		client := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
		call := client.callContext
		reads, waits := 0, 0
		hard := errors.New("synthetic treasury custody integrity failure")
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			deadline, finite := ctx.Deadline()
			originalDeadline, _ := parent.Deadline()
			if !finite || deadline.After(originalDeadline) {
				t.Fatal("treasury custody read replaced its original parent deadline")
			}
			if method == "state_getStorage" && args[0] == key.Hex() {
				reads++
				if args[1] != fixture.block(100).Hex() {
					t.Fatal("treasury read retry selected another finalized block")
				}
				if mode != "recover" || reads == 1 {
					if mode == "mixed" {
						return errors.Join(context.DeadlineExceeded, hard)
					}
					return context.DeadlineExceeded
				}
			}
			return call(ctx, target, method, args...)
		}
		steerer := &ReleaseSteerer{cfg: fixture.cfg}
		steerer.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			if mode == "recover" && waits == 1 {
				fixture.head = 101
				return ctx.Err()
			}
			return context.DeadlineExceeded
		}
		var observation *TreasuryAdmissionObservation
		err = steerer.productionRead(parent, productionReadPreparation, nil, func(ctx context.Context) error {
			var err error
			observation, err = ObserveTreasuryAdmission(ctx, fixture.cfg, admission.chain)
			return err
		})
		cancel()
		if mode == "recover" {
			if err != nil || observation == nil || observation.Snapshot.FinalizedHash != [32]byte(fixture.block(100)) || waits != 1 || reads != 2 {
				t.Fatalf("treasury read recovery changed its owner/selection: reads=%d waits=%d error=%v", reads, waits, err)
			}
		} else if observation != nil || !errors.Is(err, context.DeadlineExceeded) || reads != 1 ||
			(mode == "mixed" && (!errors.Is(err, hard) || retryableProductionSteeringRead(err) || waits != 0)) ||
			(mode == "deadline" && (!retryableProductionSteeringRead(err) || waits != 1)) {
			t.Fatalf("%s treasury custody failure lost result/cause boundaries: reads=%d waits=%d error=%v", mode, reads, waits, err)
		}
	}
}
