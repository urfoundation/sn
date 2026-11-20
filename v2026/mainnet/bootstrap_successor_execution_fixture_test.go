// Synthetic canonical adapters isolate custody transitions. Safe signatures use
// the independently executed published proxy's digest and test-only owner keys.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Each fixture owns distinct directories while deterministic test keys make
// cross-root nonce conflicts reproducible. No real identity or route is used.
type bootstrapSuccessorExecutionFixture struct {
	t            *testing.T
	storage      *durablefixture.Fixture
	approval     bootstrapSuccessorExecutionApproval
	key          ed25519.PrivateKey
	profile      *safeExecutionProfile
	oracle       *safeExecutionFixture
	observation  bootstrapSuccessorExecutionObservation
	resolution   bootstrapSuccessorExecutionReconciliation
	authErr      error
	submitErr    error
	changeSeals  func([]string) []string
	observeHook  func(int, *bootstrapSuccessorExecutionObservation)
	observations int
	writes       [][]byte
}

// Default nonces intentionally differ across the inner and outer domains.
func newBootstrapSuccessorExecutionFixture(t *testing.T) *bootstrapSuccessorExecutionFixture {
	return newBootstrapSuccessorExecutionNonceFixture(t, "17", 42)
}

// Reapprove synthetic original scope before preparing it. The execution domain
// is signed only after exact signature and transaction bytes are available.
func newBootstrapSuccessorExecutionNonceFixture(t *testing.T, safeNonce string, outerNonce uint64) *bootstrapSuccessorExecutionFixture {
	t.Helper()
	return newBootstrapSuccessorExecutionOwnerFixture(t, safeNonce, outerNonce, false)
}

// The single-owner request schema is selected explicitly; its published Safe has
// one owner at threshold one and the request carries one 65-byte signature.
func newBootstrapSuccessorExecutionOwnerFixture(t *testing.T, safeNonce string, outerNonce uint64, singleOwner bool) *bootstrapSuccessorExecutionFixture {
	t.Helper()
	prepared, key, safeRequest, safeReference, archive := bootstrapSuccessorSafeTestInputs(t, "1.4.1", "Safe")
	oracle := newSafeExecutionOwnerFixture(t, "1.4.1", "Safe", singleOwner)
	schema, modes, threshold := bootstrapSuccessorExecutionRequestSchema, []string{"raw", "raw"}, uint64(2)
	if singleOwner {
		schema, modes, threshold = bootstrapSuccessorExecutionSingleOwnerRequestSchema, []string{"raw"}, 1
	}
	relayer, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic successor execution relayer")))
	if err != nil {
		t.Fatal(err)
	}
	p := &prepared.Plan.Proposal
	p.Request.IntendedRelayer = crypto.PubkeyToAddress(relayer.PublicKey)
	p.Request.IntendedRelayerNonce, p.Request.IntendedSafeNonce = outerNonce, safeNonce
	p.AdoptedActions[7].Receipt.RuntimeHash = crypto.Keccak256Hash([]byte("synthetic retained evidence runtime")).Hex()
	p.AdoptedActions[7].Receipt.GetterHash = rootObjectHash("synthetic retained immutable evidence domain")
	for i := range p.AdoptedActions {
		p.AdoptedActions[i].CustodyHash = rootObjectHash(struct{ Index int }{Index: i})
	}
	prepared = bootstrapSuccessorPreparationTestSign(t, prepared.Plan, key)
	safeRequest.PreparationPlanHash = prepared.Plan.hash()
	record := bootstrapSuccessorSafeTestRecord(prepared)
	safeRequest.PreparationRecordHash = record.ContentHash
	registry := bootstrapSuccessorExecutionTestDirectory(t)
	prepareBootstrapSuccessorMembersTest(t, registry, true)
	storage := durablefixture.New(t, t.Context(), prepared.Plan.Proposal.OriginalRunDirectory, registry)
	preparation, err := openBootstrapSuccessorPreparationStore(storage.Context, prepared.Plan, prepared, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := preparation.close(); err != nil {
		t.Fatal(err)
	}
	review, err := buildBootstrapSuccessorSafeReview(t.Context(), prepared.Plan, record, safeRequest, safeReference, archive)
	if err != nil {
		t.Fatal(err)
	}
	request := bootstrapSuccessorExecutionRequest{Schema: schema, SafeReviewHash: review.ContentHash, RegistryDirectory: registry,
		Owners: slices.Clone(oracle.owners), Singleton: common.BytesToAddress(crypto.Keccak256([]byte("synthetic singleton 1.4.1Safe")))}
	draft := bootstrapSuccessorExecutionPlan{Review: review, Request: request}
	signatures := oracle.signatures(draft.transaction(), modes...)
	request.SafeSignatures = bootstrapSuccessorExecutionTestRaw(t, "synthetic-safe-signatures.bin", signatures)
	draft.Request, draft.SafeSignatures = request, "0x"+hex.EncodeToString(signatures)
	outer, err := draft.outer(oracle.profile)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := outer.unsigned()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId)), relayer)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	request.RelayerTransaction = bootstrapSuccessorExecutionTestRaw(t, "synthetic-relayer-transaction.bin", raw)
	requestReference := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-execution-request.json"), request)
	plan, err := buildBootstrapSuccessorExecution(t.Context(), review, request, requestReference, oracle.profile)
	if err != nil {
		t.Fatal(err)
	}
	f := &bootstrapSuccessorExecutionFixture{t: t, storage: storage, key: key, profile: oracle.profile, oracle: oracle, resolution: bootstrapSuccessorExecutionReconciliation{Status: "absent"}}
	f.approval = bootstrapSuccessorExecutionTestSign(t, plan, key, oracle.profile)
	f.observation = bootstrapSuccessorExecutionObservation{NativeNumber: safeRequest.StartNativeNumber, NativeHash: common.HexToHash(safeRequest.StartNativeHash),
		Singleton: request.Singleton, Owners: slices.Clone(request.Owners), Threshold: threshold, SafeNonce: safeNonce, RelayerNonce: outerNonce, RelayerPendingNonce: outerNonce,
		RelayerBalanceWei: review.Relayer.MaximumLiabilityWei, CoordinatorOwner: review.Transaction.Safe,
		EvidenceRuntimeHash: p.AdoptedActions[7].Receipt.RuntimeHash, EvidenceGetterHash: p.AdoptedActions[7].Receipt.GetterHash}
	pin, err := loadSafeReleasePin("1.4.1", "Safe")
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range pin.Artifacts {
		if artifact.Name == "Safe" {
			f.observation.SingletonRuntimeHash = common.HexToHash(artifact.RuntimeKeccak256)
		} else if artifact.Name == "SafeProxy" {
			f.observation.SafeProxyRuntimeHash = common.HexToHash(artifact.RuntimeKeccak256)
		}
	}
	return f
}

// Testing.TempDir uses ambient creation permissions for its numbered children.
// Every admitted fixture directory therefore declares the production mode.
func bootstrapSuccessorExecutionTestDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}

// Binary files have literal pinned bytes and a private parent. Signature hex
// belongs only in plans; permissive temporary directories are not custody.
func bootstrapSuccessorExecutionTestRaw(t *testing.T, name string, raw []byte) planFileReference {
	t.Helper()
	path := filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), name)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return planFileReference{Path: path, Sha256: safeReleaseHash(raw)}
}

// No production signer participates in independent approval tests.
func bootstrapSuccessorExecutionTestSign(t *testing.T, plan bootstrapSuccessorExecutionPlan, key ed25519.PrivateKey, profile *safeExecutionProfile) bootstrapSuccessorExecutionApproval {
	t.Helper()
	message, err := plan.signingBytes(profile)
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapSuccessorExecutionApproval{Schema: bootstrapSuccessorExecutionEnvelopeSchema, Plan: plan, Signature: hex.EncodeToString(ed25519.Sign(key, message))}
}

// Clones isolate mutations from baseline owner and receipt expectations.
func bootstrapSuccessorExecutionTestCopy[T any](t *testing.T, value T) T {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var copied T
	if err := decodePlanJson(raw, &copied); err != nil {
		t.Fatal(err)
	}
	return copied
}

// The model explicitly supplies authenticated seals. Public command tests
// separately exercise actual original-file reconstruction and all marker locks.
func (self *bootstrapSuccessorExecutionFixture) authenticate(ctx context.Context, plan bootstrapSuccessorExecutionPlan) ([]string, error) {
	seals := []string{}
	for _, action := range self.approval.Plan.Review.Preparation.Approval.Plan.Proposal.AdoptedActions {
		seals = append(seals, action.CustodyHash)
	}
	if self.changeSeals != nil {
		seals = self.changeSeals(seals)
	}
	return seals, errors.Join(ctx.Err(), self.authErr)
}

// Admission hook number two forces changes after durable attempt publication.
func (self *bootstrapSuccessorExecutionFixture) observe(ctx context.Context, plan bootstrapSuccessorExecutionPlan) (bootstrapSuccessorExecutionObservation, error) {
	self.observations++
	observation := bootstrapSuccessorExecutionTestCopy(self.t, self.observation)
	if self.observeHook != nil {
		self.observeHook(self.observations, &observation)
	}
	return observation, ctx.Err()
}

// This synthetic model never claims that fabricated receipts are live facts.
func (self *bootstrapSuccessorExecutionFixture) reconcile(ctx context.Context, plan bootstrapSuccessorExecutionPlan) (bootstrapSuccessorExecutionReconciliation, error) {
	return bootstrapSuccessorExecutionTestCopy(self.t, self.resolution), ctx.Err()
}

// Lost reply errors occur after the exact public transaction has been observed.
func (self *bootstrapSuccessorExecutionFixture) submit(ctx context.Context, plan bootstrapSuccessorExecutionPlan, raw []byte) error {
	if !bytes.Equal(raw, common.FromHex(self.approval.Plan.SignedRelayer)) {
		self.t.Fatal("owner replaced approved signed bytes")
	}
	self.writes = append(self.writes, slices.Clone(raw))
	return errors.Join(ctx.Err(), self.submitErr)
}

// An internally consistent synthetic canonical receipt isolates binding checks.
// Safe's published event ABI provides the event signature used by the model.
func (self *bootstrapSuccessorExecutionFixture) receipt() *bootstrapSuccessorExecutionReceipt {
	p := self.approval.Plan
	block := crypto.Keccak256Hash([]byte("synthetic canonical successor block"))
	log := &types.Log{Address: p.Review.Transaction.Safe, Topics: []common.Hash{self.oracle.oracleAbi.Events["ExecutionSuccess"].ID, p.Review.Transaction.Digest},
		Data: make([]byte, 32), BlockNumber: 119, TxHash: p.TransactionHash, BlockHash: block, Index: 1}
	anchor := p.Review.Preparation.Approval.Plan.Proposal.Anchor
	binding := &types.Log{Address: anchor.Coordinator, Topics: []common.Hash{crypto.Keccak256Hash([]byte("ValidatorEvidenceFixed(address)")), common.BytesToHash(anchor.Evidence[:])},
		BlockNumber: 119, TxHash: p.TransactionHash, BlockHash: block}
	return &bootstrapSuccessorExecutionReceipt{NativeNumber: 119, NativeHash: block,
		Receipt:             safeExecutionReceipt{TransactionHash: p.TransactionHash, BlockHash: block, BlockNumber: 119, To: p.Review.Transaction.Safe, Status: 1, Logs: []*types.Log{binding, log}},
		CoordinatorEvidence: p.Review.Preparation.Approval.Plan.Proposal.Anchor.Evidence,
		EvidenceRuntimeHash: self.observation.EvidenceRuntimeHash, EvidenceGetterHash: self.observation.EvidenceGetterHash}
}

// Tests retain no open owner at fixture cleanup, including failed assertions.
func (self *bootstrapSuccessorExecutionFixture) open(create bool, hook func(string) error) *bootstrapSuccessorExecutionStore {
	self.t.Helper()
	owner, err := openBootstrapSuccessorExecutionStore(self.storageContext(self.t.Context()), self.approval.Plan, self.approval, self.profile, create, hook)
	if err != nil {
		self.t.Fatal(err)
	}
	self.t.Cleanup(func() { owner.close() })
	return owner
}

// Each operation preserves its cancellation while selecting the same original
// declaration; test faults never re-enroll a missing or replaced registry.
func (self *bootstrapSuccessorExecutionFixture) storageContext(ctx context.Context) context.Context {
	return durablepath.WithHost(durablevolume.WithReference(ctx, self.storage.Reference), self.storage.Host)
}
