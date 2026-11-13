// Deterministic synthetic adapters force every signing/receipt boundary. No
// production keys, live routes or behavioral scheduling assumptions are used.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/vedhavyas/go-subkey/v2"
)

// Approval and native custody are deliberately different synthetic keys.
type ownerTrimTestFixture struct {
	storage        *durablefixture.Fixture
	config         ownerTrimExecutionConfig
	key            string
	approval       ed25519.PrivateKey
	pair           subkey.KeyPair
	metadata       string
	ledgerMetadata string
}

// The historical public metadata remains independently pinned by its raw hash.
func newOwnerTrimActionTestFixture(t *testing.T) *ownerTrimTestFixture {
	t.Helper()
	root, pair, metadata := rootActionFixture(t)
	seed := sha256.Sum256([]byte("synthetic owner trim approval only"))
	approval := ed25519.NewKeyFromSeed(seed[:])
	action := ownerTrimAction{Schema: ownerTrimActionSchema, PreparationHash: rootObjectHash("synthetic-v3"), PreparationStateHash: rootObjectHash("synthetic-original-custody"),
		PolicyHash: rootObjectHash("synthetic-trim-policy"), ReviewHash: rootObjectHash("synthetic-trim-review"),
		Network:   planNetwork{NativeChain: root.Scope.NativeChain, GenesisHash: root.Scope.GenesisHash, EvmChainId: mainnetEvmChainId},
		Runtime:   rootReceiptProfile{RuntimeSourceCommit: root.Scope.RuntimeSourceCommit, RuntimeVersion: root.Scope.RuntimeVersion, RuntimeCodeHash: root.Scope.RuntimeCodeHash, RuntimeMetadataHash: root.Scope.RuntimeMetadataHash},
		CustodyId: "synthetic-owner-custody", StatePath: filepath.Join(filepath.Dir(root.Scope.StatePath), ownerTrimStateFile), Coldkey: root.Scope.Hotkey,
		Netuid: 25, SubnetRegistrationBlock: 10, SubnetGeneration: 2, MaximumUids: 6, SelectionRule: ownerTrimSubsetRule,
		Nonce: 7, BirthBlock: root.BirthBlock, BirthHash: root.BirthHash, Period: 8, FeeReserveRao: 200, MaxBroadcasts: 2}
	var err error
	action, err = prepareOwnerTrimAction(action, metadata)
	if err != nil {
		t.Fatal(err)
	}
	f := &ownerTrimTestFixture{config: ownerTrimExecutionConfig{Schema: ownerTrimExecutionSchema, Action: action,
		Route: ownedSubmissionRoute{RpcUrl: "http://192.0.2.1:9944", ReadRetrySeconds: 60, SendTimeoutSeconds: 1}},
		key: "0x" + hex.EncodeToString(approval.Public().(ed25519.PublicKey)), approval: approval, pair: pair, metadata: metadata}
	f.approve()
	return f
}

// Only an explicit test approval can rebind an altered input.
func (self *ownerTrimTestFixture) approve() {
	self.config.Signature = hex.EncodeToString(ed25519.Sign(self.approval, self.config.signingBytes()))
}

// Value copies go through the public grammar so test ports cannot alias state.
func ownerTrimTestCopy[T any](t *testing.T, value T) T {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := decodePlanJson(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// Save faults happen after the new record is durable, matching rename ambiguity.
type ownerTrimTestStore struct {
	t      *testing.T
	record ownerTrimRecord
	fail   bool
	writes int
}

func (self *ownerTrimTestStore) load() (ownerTrimRecord, error) {
	return ownerTrimTestCopy(self.t, self.record), nil
}

func (self *ownerTrimTestStore) save(record ownerTrimRecord) error {
	self.record, self.writes = ownerTrimTestCopy(self.t, record), self.writes+1
	if self.fail {
		return errors.New("synthetic post-rename failure")
	}
	return nil
}

// The fixture can lose the first response after retaining one actual signature.
type ownerTrimTestSigner struct {
	pair       subkey.KeyPair
	signature  []byte
	request    string
	signs      int
	recoveries int
	lose       bool
	unknown    bool
}

func (self *ownerTrimTestSigner) signOnce(_ context.Context, action ownerTrimAction) ([]byte, error) {
	if self.signs != 0 {
		return nil, errors.New("synthetic custody refuses a second signing call")
	}
	self.signs++
	self.request = action.RequestHash
	payload, _ := hex.DecodeString(action.Payload[2:])
	var err error
	self.signature, err = self.pair.Sign(payload)
	if self.lose {
		return nil, errors.New("synthetic lost signature response")
	}
	return append([]byte(nil), self.signature...), err
}

func (self *ownerTrimTestSigner) recoverSignature(_ context.Context, request string) ([]byte, error) {
	self.recoveries++
	if self.unknown || request != self.request {
		return nil, errors.New("synthetic custody lookup unresolved")
	}
	return append([]byte(nil), self.signature...), nil
}

// Authority is deliberately a separate capability, never a conditional pass bit.
type ownerTrimTestAuthority struct{ calls int }

func (self *ownerTrimTestAuthority) authorize(context.Context, ownerTrimExecutionConfig, ownerTrimActionReconciliation) error {
	self.calls++
	return nil
}

type ownerTrimTestChain struct {
	t        *testing.T
	evidence ownerTrimActionReconciliation
	reads    int
	sends    int
	sendErr  error
	raw      []byte
}

func (self *ownerTrimTestChain) reconcile(context.Context, ownerTrimAction, []byte) (ownerTrimActionReconciliation, error) {
	self.reads++
	return ownerTrimTestCopy(self.t, self.evidence), nil
}

func (self *ownerTrimTestChain) submit(_ context.Context, _ ownerTrimExecutionConfig, raw []byte) error {
	self.sends++
	self.raw = append([]byte(nil), raw...)
	return self.sendErr
}

// The fake reader's census is irrelevant to the independent fake authority;
// production census admission is covered by the concrete reader tests below.
func ownerTrimTestOwner(t *testing.T, fixture *ownerTrimTestFixture) (*ownerTrimExecutor, *ownerTrimTestStore, *ownerTrimTestSigner, *ownerTrimTestChain) {
	t.Helper()
	action := fixture.config.Action
	record := ownerTrimRecord{Schema: ownerTrimRecordSchema, Config: fixture.config, ApprovalKey: fixture.key, Phase: "reserved"}
	record.ContentHash = rootObjectHash(record)
	store := &ownerTrimTestStore{t: t, record: record}
	nonce := action.Nonce
	chain := &ownerTrimTestChain{t: t, evidence: ownerTrimActionReconciliation{
		Observation: ownerTrimObservation{FinalizedNumber: action.BirthBlock, FinalizedHash: action.BirthHash, AccountNonce: &nonce, Census: &subnetPreviewEnvelope{}},
		AnchorHash:  action.BirthHash, CheckedFrom: action.BirthBlock + 1, CheckedThrough: action.BirthBlock}}
	signer := &ownerTrimTestSigner{pair: fixture.pair}
	owner := &ownerTrimExecutor{config: fixture.config, key: fixture.key, store: store, chain: chain, authority: &ownerTrimTestAuthority{}, signer: signer}
	return owner, store, signer, chain
}

// Reindexing remains bound to the exact metadata and action, while the existing
// root call/payload retains its separate encoding and approval domain.
func TestOwnerTrimActionMetadataReindexAndRootEquivalence(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	root, _, _ := rootActionFixture(t)
	metadata, _, _ := rootTestMetadata(t)
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "AdminUtils" {
			continue
		}
		entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		for i := range entry.Def.Variant.Variants {
			if entry.Def.Variant.Variants[i].Name == "sudo_trim_to_max_allowed_uids" {
				entry.Def.Variant.Variants[i].Index = 240
			}
		}
	}
	raw, err := codec.Encode(metadata)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "0x" + hex.EncodeToString(raw)
	action := f.config.Action
	action.Runtime.RuntimeMetadataHash = rootExtrinsicHash(raw)
	action, err = prepareOwnerTrimAction(action, encoded)
	if err != nil || action.CallIndex[1] != 240 || action.Call != "0x"+hex.EncodeToString([]byte{action.CallIndex[0], 240, 25, 0, 6, 0}) {
		t.Fatalf("compatible exact-metadata reindex refused: %+v %v", action, err)
	}
	root.Scope.RuntimeMetadataHash = action.Runtime.RuntimeMetadataHash
	changedRoot, err := prepareRootAction(root, encoded)
	if err != nil || changedRoot.Call != root.Call || changedRoot.Payload != root.Payload {
		t.Fatalf("shared native profile changed root wire bytes: %v", err)
	}
	metadata.AsMetadataV14.Extrinsic.SignedExtensions[5].Identifier = "SyntheticWrongNonceExtension"
	if nativeSigningProfile(metadata) == nil {
		t.Fatal("shared envelope accepted another nonce extension")
	}
	if _, err := rootSigningProfile(metadata); err == nil {
		t.Fatal("root wrapper lost shared nonce-extension refusal")
	}
}

// A config signature cannot be replayed onto another route, nonce or v3 lineage.
func TestOwnerTrimActionApprovalAndSignatureScope(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	payload, _ := hex.DecodeString(f.config.Action.Payload[2:])
	signature, err := f.pair.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.config.Action.signed(signature)
	if err != nil || ownerTrimSignedAction(f.config.Action, raw) != nil || f.config.validate(f.key) != nil {
		t.Fatal("valid separate action or public signature refused", err)
	}
	for _, kind := range []string{"route", "nonce", "custody"} {
		changed := f.config
		switch kind {
		case "route":
			changed.Route.RpcUrl = "http://192.0.2.2:9944"
		case "nonce":
			changed.Action.Nonce++
		case "custody":
			changed.Action.PreparationStateHash = rootObjectHash("different original custody")
		}
		changed.Action, err = prepareOwnerTrimAction(changed.Action, f.metadata)
		if err != nil || changed.validate(f.key) == nil {
			t.Fatalf("%s did not preserve independent approval refusal: %v", kind, err)
		}
	}
	raw[len(raw)-1] ^= 1
	if ownerTrimSignedAction(f.config.Action, raw) == nil {
		t.Fatal("native signature accepted altered capacity")
	}
}

// Losing a response after custody retained its bytes must never sign twice.
func TestOwnerTrimExecutionRecoversOriginalSignatureOnce(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	owner, store, signer, chain := ownerTrimTestOwner(t, f)
	signer.lose = true
	if _, err := owner.step(t.Context()); err == nil || store.record.Phase != "signing" || signer.signs != 1 {
		t.Fatal("lost response did not retain ambiguous signing")
	}
	owner = &ownerTrimExecutor{config: f.config, key: f.key, store: store, chain: chain, signer: signer}
	step, err := owner.step(t.Context())
	if err != nil || step.Phase != "signed" || signer.signs != 1 || signer.recoveries != 1 || chain.reads != 1 || chain.sends != 0 || store.record.RawExtrinsic == "" {
		t.Fatalf("original signature recovery changed action or needed fresh authority: %+v %v", step, err)
	}
}

// Unknown custody differs from an authoritative never-issued response.
func TestOwnerTrimExecutionUnknownSigningCannotIssueReplacement(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	owner, store, signer, chain := ownerTrimTestOwner(t, f)
	store.record.Phase = "signing"
	store.record.ContentHash = ""
	store.record.ContentHash = rootObjectHash(store.record)
	signer.unknown = true
	if _, err := owner.step(t.Context()); err == nil || signer.signs != 0 || chain.reads != 0 || chain.sends != 0 {
		t.Fatal("unknown signature lookup replenished signing authority")
	}
}

// The attempt is already spent when the transport loses its acknowledgement.
func TestOwnerTrimExecutionAmbiguousSendRetainsAllowance(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	owner, store, signer, chain := ownerTrimTestOwner(t, f)
	if _, err := owner.step(t.Context()); err != nil {
		t.Fatal(err)
	}
	chain.sendErr = errors.New("synthetic acknowledgement lost")
	if _, err := owner.step(t.Context()); err == nil || store.record.Broadcasts != 1 || store.record.Phase != "pending" || chain.sends != 1 {
		t.Fatal("uncertain send did not consume its original attempt")
	}
	first := store.record.RawExtrinsic
	if _, err := owner.step(t.Context()); err == nil || store.record.Broadcasts != 2 || chain.sends != 2 || signer.signs != 1 || first != store.record.RawExtrinsic {
		t.Fatal("retry changed original signature or broadcast accounting")
	}
	if _, err := owner.step(t.Context()); err == nil || chain.sends != 2 || chain.reads != 4 {
		t.Fatal("exhausted allowance either rebroadcast or skipped reconciliation")
	}
}

// Any failure after publishing a state transition poisons subsequent effects.
func TestOwnerTrimExecutionPostRenameFailurePoisonsOwner(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	owner, store, signer, chain := ownerTrimTestOwner(t, f)
	store.fail = true
	if _, err := owner.step(t.Context()); err == nil || !owner.poisoned || signer.signs != 0 || chain.sends != 0 {
		t.Fatal("post-rename ambiguity reached an external effect")
	}
	store.fail = false
	if _, err := owner.step(t.Context()); err == nil || signer.signs != 0 {
		t.Fatal("poisoned owner resumed without reopening")
	}
}

// Exact absence plus consumed nonce is conflict, never successful local expiry.
func TestOwnerTrimExecutionExpiryNonceConflictAndMissingNonce(t *testing.T) {
	for _, kind := range []string{"unchanged", "consumed", "unavailable"} {
		f := newOwnerTrimActionTestFixture(t)
		owner, store, signer, chain := ownerTrimTestOwner(t, f)
		action := f.config.Action
		chain.evidence.Observation.FinalizedNumber = action.BirthBlock + action.Period
		chain.evidence.Observation.FinalizedHash = "0x" + strings.Repeat("d1", 32)
		chain.evidence.CheckedThrough = action.BirthBlock + action.Period - 1
		if kind == "consumed" {
			chain.evidence.Observation.AccountNonce = new(action.Nonce + 1)
		} else if kind == "unavailable" {
			chain.evidence.Observation.AccountNonce = nil
		}
		step, err := owner.step(t.Context())
		if kind == "unavailable" {
			if err == nil || store.record.Reconciliation != nil {
				t.Fatal("unavailable nonce became expiry")
			}
		} else {
			wanted := "expired-unsigned"
			if kind == "consumed" {
				wanted = "nonce-conflict"
			}
			if err != nil || step.Phase != wanted || store.record.Reconciliation == nil {
				t.Fatalf("%s: %+v %v", kind, step, err)
			}
		}
		if signer.signs != 0 || chain.sends != 0 || step.FullResetCompleted || step.ActivationReady {
			t.Fatal("closed action acquired another signing or activation allowance")
		}
	}
}

// Fill only the independent census after financial finality. It never resends.
func TestOwnerTrimExecutionFinalityReadbackGapContinuesWithoutEffects(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	owner, store, signer, chain := ownerTrimTestOwner(t, f)
	if _, err := owner.step(t.Context()); err != nil {
		t.Fatal(err)
	}
	action := f.config.Action
	chain.evidence.Observation.FinalizedNumber++
	chain.evidence.Observation.FinalizedHash = "0x" + strings.Repeat("d2", 32)
	chain.evidence.CheckedThrough++
	chain.evidence.Receipt = &rootActionReceipt{BlockNumber: action.BirthBlock + 1, BlockHash: chain.evidence.Observation.FinalizedHash,
		RawExtrinsic: store.record.RawExtrinsic, EventHash: "0x" + strings.Repeat("d3", 32), Success: true, ActualFeeRao: 12,
		ExecutionRuntimeVersion: action.Runtime.RuntimeVersion, ExecutionCodeHash: action.Runtime.RuntimeCodeHash, ExecutionMetadataHash: action.Runtime.RuntimeMetadataHash}
	chain.evidence.Census = &ownerTrimReceiptCensus{Issue: "synthetic after-census unavailable"}
	step, err := owner.step(t.Context())
	if err != nil || !step.TransactionFinalized || step.GenerationReconciled || step.Status != "transaction-finalized-generation-outcome-unresolved" {
		t.Fatalf("receipt gap claimed reset or erased fees: %+v %v", step, err)
	}
	before, _ := sealSubnetPreview(subnetPreview{CensusComplete: true, Identity: chainIdentity{FinalizedNumber: action.BirthBlock, FinalizedHash: action.BirthHash}})
	after, _ := sealSubnetPreview(subnetPreview{CensusComplete: true, Identity: chainIdentity{FinalizedNumber: action.BirthBlock + 1, FinalizedHash: chain.evidence.Receipt.BlockHash}})
	chain.evidence.Census = &ownerTrimReceiptCensus{Before: &before, After: &after, Correspondence: &ownerTrimActualSubset{Matches: true, ResidualOld: []subnetRegistration{{Uid: 4}, {Uid: 5}}}}
	owner.authority, owner.signer = nil, nil
	step, err = owner.step(t.Context())
	if err != nil || !step.GenerationReconciled || step.ResidualOldCount == nil || *step.ResidualOldCount != 2 || step.FullResetCompleted || chain.sends != 0 || signer.signs != 1 {
		t.Fatalf("read-only continuation changed original effect or residuals: %+v %v", step, err)
	}
}

// Reorg and incomplete history may not advance a signer, even with fake approval.
func TestOwnerTrimExecutionRefusesConflictingFinalityAndCoverage(t *testing.T) {
	for _, kind := range []string{"anchor", "coverage", "same-height"} {
		f := newOwnerTrimActionTestFixture(t)
		owner, store, signer, chain := ownerTrimTestOwner(t, f)
		switch kind {
		case "anchor":
			chain.evidence.AnchorHash = "0x" + strings.Repeat("e1", 32)
		case "coverage":
			chain.evidence.CheckedFrom++
		case "same-height":
			store.record.LastFinalized, store.record.LastFinalizedHash = f.config.Action.BirthBlock, "0x"+strings.Repeat("e2", 32)
			store.record.ContentHash = ""
			store.record.ContentHash = rootObjectHash(store.record)
		}
		if _, err := owner.step(t.Context()); err == nil || signer.signs != 0 || chain.sends != 0 {
			t.Fatalf("%s crossed a signing boundary", kind)
		}
	}
}

// Trim success needs dispatch plus the exact coldkey fee; root additionally
// requires its own seat event. Neither protocol borrows another event phase.
func TestOwnerTrimReceiptEventAssociationPreservesRootRequirements(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	metadata, _, _ := rootTestMetadata(t)
	payer, _ := hex.DecodeString(f.config.Action.Coldkey[2:])
	fee := rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 1, payer, binary.LittleEndian.AppendUint64(nil, 17), make([]byte, 8))
	success := rootReceiptEventFixture(t, metadata, "System.ExtrinsicSuccess", 1)
	raw := append(append([]byte{8}, fee...), success...)
	receipt, err := nativeDecodeReceiptEvents(metadata, raw, 1, 2, f.config.Action.Coldkey, nil)
	if err != nil || !receipt.Success || receipt.ActualFeeRao != 17 {
		t.Fatal("exact owner fee/dispatch was refused", err)
	}
	root, _, _ := rootActionFixture(t)
	if _, err := rootDecodeReceiptEvents(metadata, raw, 1, 2, root); err == nil {
		t.Fatal("root receipt no longer requires its weight event")
	}
	for _, kind := range []string{"other-phase", "duplicate", "wrong-payer"} {
		changed := append([]byte(nil), raw...)
		switch kind {
		case "other-phase":
			binary.LittleEndian.PutUint32(changed[2:6], 0)
		case "duplicate":
			changed[0] = 12
			changed = append(changed, fee...)
		case "wrong-payer":
			changed = append(append([]byte{8}, rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 1, bytes.Repeat([]byte{0x91}, 32), binary.LittleEndian.AppendUint64(nil, 17), make([]byte, 8))...), success...)
		}
		if _, err := nativeDecodeReceiptEvents(metadata, changed, 1, 2, f.config.Action.Coldkey, nil); err == nil {
			t.Fatalf("%s admitted unbound financial evidence", kind)
		}
	}
}

// A partial actual subset must retain explicit old miners and protect exact roles.
func TestOwnerTrimActualSubsetRetainsResidualsAndRejectsGenerations(t *testing.T) {
	client, fixture, policy, window := newOwnerTrimBoundedFixture(t)
	before, err := client.readSubnetPreview(t.Context(), policy, window.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	f := newOwnerTrimActionTestFixture(t)
	action := f.config.Action
	action.Network = planNetwork{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}
	action.Runtime = rootReceiptProfile{RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion, RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash}
	action.PolicyHash, action.Coldkey = window.PolicyHash, policy.SubnetOwnerColdkey
	action.SubnetRegistrationBlock, action.SubnetGeneration = *policy.SubnetRegistrationBlock, *policy.SubnetGeneration
	action.BirthBlock, action.BirthHash = window.FinalizedNumber, window.FinalizedHash
	action, err = prepareOwnerTrimAction(action, fixture.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	after := ownerTrimTestCopy(t, before)
	after.Seats = after.Seats[:6]
	after.MaximumUids, after.Identity.FinalizedNumber, after.Identity.FinalizedHash = 6, 101, "0x"+strings.Repeat("d4", 32)
	result, err := reconcileOwnerTrimActualSubset(action, policy, before, after)
	if err != nil || !result.Matches || len(result.AbsentOld) != 4 || len(result.ResidualOld) != 2 || len(result.Requested) != 6 || result.FullReset || result.AttributedToTrim {
		t.Fatalf("partial trim lost residual scope: %+v %v", result, err)
	}
	for _, kind := range []string{"majority-birth", "secondary-coldkey", "root", "uid", "subnet-generation"} {
		changed := ownerTrimTestCopy(t, after)
		switch kind {
		case "majority-birth":
			changed.Seats[2].RegistrationBlock++
		case "secondary-coldkey":
			changed.Seats[3].Coldkey = "0x" + strings.Repeat("c1", 32)
		case "root":
			changed.RootRegistrations[0].RegistrationBlock++
		case "uid":
			changed.Seats[4], changed.Seats[5] = changed.Seats[5], changed.Seats[4]
			changed.Seats[4].Uid, changed.Seats[5].Uid = 4, 5
		case "subnet-generation":
			changed.SubnetGeneration++
		}
		result, err := reconcileOwnerTrimActualSubset(action, policy, before, changed)
		if err != nil || result.Matches || len(result.Blockers) == 0 {
			t.Fatalf("%s inherited protected correspondence: %+v %v", kind, result, err)
		}
	}
}

// The sixth journal is independently approved after real v3 custody preparation.
func ownerTrimPreparedTestFixture(t *testing.T, ownerOverride ...[]byte) (*bootstrapChainFixture, *ownerTrimTestFixture) {
	t.Helper()
	return ownerTrimPreparedTestFixtureWithCensus(t, nil, ownerOverride...)
}

// Select current registration predicates before any review or custody is signed.
func ownerTrimPreparedTestFixtureWithCensus(t *testing.T, configure func(*rootRpcFixture, *subnetCensusPolicy), ownerOverride ...[]byte) (*bootstrapChainFixture, *ownerTrimTestFixture) {
	t.Helper()
	chain := newBootstrapChainReadinessFixtureWithCensus(t, configure, nil, ownerOverride...)
	f := newOwnerTrimActionTestFixture(t)
	retained, err := openBootstrapChainReadinessState(chain.root.storage.Context, chain.preparation)
	if err != nil {
		t.Fatal(err)
	}
	defer retained.close()
	policyRaw, err := readBootstrapChainInput(t.Context(), chain.config.OwnerTrimPolicy, maxRpcReplyBytes)
	if err != nil {
		t.Fatal(err)
	}
	var policy subnetCensusPolicy
	if err := decodePlanJson(policyRaw, &policy); err != nil {
		t.Fatal(err)
	}
	reviewRaw, err := readBootstrapChainInput(t.Context(), chain.config.OwnerTrimPlan, maximumOwnerTrimPlanBytes)
	if err != nil {
		t.Fatal(err)
	}
	var review ownerTrimPlan
	if err := decodePlanJson(reviewRaw, &review); err != nil {
		t.Fatal(err)
	}
	a := f.config.Action
	a.PreparationHash, a.PreparationStateHash = chain.preparation.Plan.ContentHash, rootObjectHash(retained)
	a.PolicyHash, a.ReviewHash, a.Network = chain.config.OwnerTrimPolicy.Sha256, review.ContentHash, chain.config.Network
	a.Runtime = rootReceiptProfile{RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion, RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash}
	a.Coldkey, a.SubnetRegistrationBlock, a.SubnetGeneration = policy.SubnetOwnerColdkey, *policy.SubnetRegistrationBlock, *policy.SubnetGeneration
	a.MaximumUids, a.StatePath, a.BirthBlock, a.BirthHash = review.Best.MaximumUids, filepath.Join(chain.config.RunDirectory, ownerTrimStateFile), 100, testFinalizedHash
	f.config.Action, err = prepareOwnerTrimAction(a, chain.census.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	f.metadata = chain.census.metadataHex
	f.approve()
	prepareMainnetSnapshotTest(t, f.config.Action.StatePath, "mainnet-owner-trim", ownerTrimStoreLimit)
	f.storage = chain.root.storage
	return chain, f
}

// Exact old custody, original approvals and role locks survive restart unchanged.
func TestOwnerTrimStorePreservesOriginalV3CustodyAndCannotRebind(t *testing.T) {
	chain, f := ownerTrimPreparedTestFixture(t)
	original := chain.journals(t)
	store, err := openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openBootstrapChainStore(chain.preparation, false, nil, chain.storageContext(t.Context())); err == nil {
		t.Fatal("parent writer acquired original custody during trim ownership")
	}
	if _, err := openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, false); err == nil {
		t.Fatal("second local trim owner acquired exclusive custody")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, false)
	if err != nil {
		t.Fatal(err)
	}
	store.close()
	if !reflect.DeepEqual(original, chain.journals(t)) {
		t.Fatal("trim claim/resume mutated original five journals or signatures")
	}
	f.config.Action.Nonce++
	f.config.Action, err = prepareOwnerTrimAction(f.config.Action, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	if _, err := openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, false); err == nil {
		t.Fatal("new valid action approval adopted a previously claimed nonce")
	}
}

// Completed markers turn missing state into a custody error, not reservation.
func TestOwnerTrimStoreMissingStateAndPostRenameRemainRecoverable(t *testing.T) {
	chain, f := ownerTrimPreparedTestFixture(t)
	store, err := openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, true)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	store.syncDirectory = func(*os.File) error { return errors.New("synthetic directory sync failure") }
	if err := store.save(record); err == nil || store.failed == nil {
		t.Fatal("post-rename directory failure did not poison actual store")
	}
	store.close()
	store, err = openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, false)
	if err != nil {
		t.Fatal("complete durable row did not survive reopen", err)
	}
	store.close()
	if err := os.Remove(f.config.Action.StatePath); err != nil {
		t.Fatal(err)
	}
	for _, create := range []bool{false, true} {
		if _, err := openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, create); err == nil {
			t.Fatal("lost state replenished an existing action marker")
		}
	}
}

// Public commands refuse old/new acceptance mistakes before a new journal exists.
func TestOwnerTrimCommandClaimsOnlySeparatelyApprovedOriginalV3(t *testing.T) {
	chain, f := ownerTrimPreparedTestFixture(t)
	path := filepath.Join(filepath.Dir(chain.path), "trim-execution.json")
	bootstrapRootTestWrite(t, path, f.config)
	original := chain.journals(t)
	for _, mode := range []string{"trim-apply", "trim-resume"} {
		var stdout, stderr bytes.Buffer
		code := chain.command(t.Context(), mode, &stdout, &stderr, "--trim-config", path, "--trim-approval-key", f.key)
		if code != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"phase":"reserved"`)) || bytes.Contains(stdout.Bytes(), []byte(`"activation_ready":true`)) {
			t.Fatalf("%s: %d %s %s", mode, code, stderr.String(), stdout.String())
		}
	}
	if !reflect.DeepEqual(original, chain.journals(t)) {
		t.Fatal("trim command edited original v3 custody")
	}
	var stdout, stderr bytes.Buffer
	if code := chain.command(t.Context(), "trim-resume", &stdout, &stderr, "--trim-config", path, "--trim-approval-key", f.key, "--rpc", "http://192.0.2.2:9944"); code != 2 {
		t.Fatal("an unapproved route override was accepted", code)
	}
}

// Explicit metadata type changes cannot make unknown proxy state look absent.
func TestOwnerTrimAuthorityProxyMetadataDefaultAndShape(t *testing.T) {
	metadata, _, _ := rootTestMetadata(t)
	if _, err := ownerTrimProxyEntry(metadata); err != nil {
		t.Fatal("reviewed native proxy profile refused", err)
	}
	for i := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[i]
		if pallet.Name == "Proxy" {
			for j := range pallet.Storage.Items {
				if pallet.Storage.Items[j].Name == "Proxies" {
					pallet.Storage.Items[j].Fallback = types.Bytes{0}
				}
			}
		}
	}
	if _, err := ownerTrimProxyEntry(metadata); err == nil {
		t.Fatal("changed proxy query default became no delegation")
	}
}
