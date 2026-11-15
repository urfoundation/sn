// Production adapter fixtures execute the original graph and the published
// Safe in one local EVM. Every identity, signature and authority is synthetic.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// A fixture retains command inputs so the command implementation reconstructs
// the same physical root, approvals, markers and exact signed transaction.
type bootstrapSuccessorCanonicalFixture struct {
	t               *testing.T
	original        *bootstrapChainFixture
	key             ed25519.PrivateKey
	profile         *safeExecutionProfile
	approval        bootstrapSuccessorExecutionApproval
	canonical       bootstrapSuccessorCanonicalApproval
	canonicalRef    planFileReference
	paths           []string
	approvalArgs    []string
	afterProvenance func()
}

// The original public v3 setup runs once per heavy root. Admission and receipt
// faults then share that history, without process-global fixtures or caching.
func newBootstrapSuccessorCanonicalFixture(t *testing.T) *bootstrapSuccessorCanonicalFixture {
	return newBootstrapSuccessorCanonicalFixtureWithClaimGate(t, nil)
}

// A test-only barrier observes the public claim before its first effect; the
// command still reconstructs every approval and physical owner itself.
func newBootstrapSuccessorCanonicalFixtureWithClaimGate(t *testing.T, beforeClaim func(*bootstrapSuccessorCanonicalFixture)) *bootstrapSuccessorCanonicalFixture {
	t.Helper()
	return newBootstrapSuccessorCanonicalOwnerFixture(t, beforeClaim, false)
}

// The single-owner variant installs a published one-owner/threshold-one Safe and
// selects its separate request schema; every other original input is shared.
func newBootstrapSuccessorCanonicalOwnerFixture(t *testing.T, beforeClaim func(*bootstrapSuccessorCanonicalFixture), singleOwner bool) *bootstrapSuccessorCanonicalFixture {
	t.Helper()
	schema, threshold := bootstrapSuccessorExecutionRequestSchema, 2
	if singleOwner {
		schema, threshold = bootstrapSuccessorExecutionSingleOwnerRequestSchema, 1
	}
	f := newBootstrapSuccessorCommandFixture(t)
	bootstrapSuccessorCommandTestComplete(t, f)
	request := bootstrapSuccessorTestRequest(f.contracts.config, f.preparation.Plan.ContentHash)
	relayer, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic canonical successor relayer")))
	if err != nil {
		t.Fatal(err)
	}
	request.IntendedRelayer, request.IntendedRelayerNonce = crypto.PubkeyToAddress(relayer.PublicKey), 42
	request.IntendedSafeNonce, request.AdditionalMaximumWei = "17", "10000000"
	requestPath := filepath.Join(filepath.Dir(f.path), "synthetic-canonical-original-request.json")
	bootstrapRootTestWrite(t, requestPath, request)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "contract-successor-preview", &stdout, &stderr, "--request", requestPath); code != 0 {
		t.Fatal("canonical preparation preview", code, stderr.String())
	}
	var preview bootstrapSuccessorPreparationPreview
	if err := decodePlanJson(stdout.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("synthetic contract independent approval"))
	key := ed25519.NewKeyFromSeed(seed[:])
	preparationApproval := bootstrapSuccessorPreparationTestSign(t, preview.Plan, key)
	preparationRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "synthetic-canonical-preparation-approval.json"), preparationApproval)
	stdout.Reset()
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-prepare", &stdout, &stderr, "--request", requestPath, "--approval", preparationRef.Path,
		"--approval-sha256", preparationRef.Sha256, "--accept-successor-hash", preview.PlanHash); code != 0 {
		t.Fatal("canonical preparation claim", code, stderr.String())
	}
	pin, archivePath, members := safeReleaseTestInputs(t, "1.4.1", "Safe")
	oracle := newSafeExecutionOwnerFixture(t, "1.4.1", "Safe", singleOwner)
	_, singletonArtifact, proxyArtifact := safeExecutionOracleArtifacts(t, pin, "Safe", members)
	singleton := common.BytesToAddress(crypto.Keccak256([]byte("synthetic canonical singleton")))
	func() {
		f.contracts.stateLock.Lock()
		defer f.contracts.stateLock.Unlock()
		f.contracts.state.SetCode(request.IntendedOwnerSafe, common.FromHex(proxyArtifact.Runtime), tracing.CodeChangeUnspecified)
		f.contracts.state.SetCode(singleton, common.FromHex(singletonArtifact.Runtime), tracing.CodeChangeUnspecified)
		f.contracts.state.SetState(request.IntendedOwnerSafe, common.Hash{}, common.BytesToHash(singleton[:]))
		setup, err := oracle.oracleAbi.Pack("setup", oracle.owners, big.NewInt(int64(threshold)), common.Address{}, []byte{}, common.Address{}, common.Address{}, big.NewInt(0), common.Address{})
		if err != nil {
			t.Fatal(err)
		}
		vm := f.contracts.vm
		vm.GasLimit = 5_000_000
		if _, _, err := runtime.Call(request.IntendedOwnerSafe, setup, &vm); err != nil {
			t.Fatal("canonical Safe setup", err)
		}
		f.contracts.state.SetState(request.IntendedOwnerSafe, common.BigToHash(big.NewInt(5)), common.BigToHash(big.NewInt(17)))
		f.contracts.state.SetNonce(request.IntendedRelayer, request.IntendedRelayerNonce, tracing.NonceChangeUnspecified)
		f.contracts.advanceEmpty()
	}()
	last := preview.Plan.Proposal.AdoptedActions[7].Receipt
	safeRequest := bootstrapSuccessorSafeRequest{Schema: bootstrapSuccessorSafeRequestSchema, PreparationPlanHash: preview.PlanHash,
		PreparationRecordHash: bootstrapSuccessorSafeTestRecord(preparationApproval).ContentHash,
		Version:               "1.4.1", Variant: "Safe", Archive: planFileReference{Path: archivePath, Sha256: pin.ArchiveSha256}, RelayerGas: 500000,
		RelayerFeeCapWei: "10", RelayerTipCapWei: "1", StartNativeNumber: last.NativeNumber, StartNativeHash: last.NativeHash, ValidThroughNative: last.NativeNumber + 100}
	safePath := filepath.Join(filepath.Dir(f.path), "synthetic-canonical-safe-request.json")
	bootstrapRootTestWrite(t, safePath, safeRequest)
	stdout.Reset()
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-safe-review", &stdout, &stderr, "--request", requestPath, "--safe-request", safePath); code != 3 {
		t.Fatal("canonical Safe review", code, stderr.String())
	}
	var review bootstrapSuccessorSafeReview
	if err := decodePlanJson(stdout.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	var signatures []byte
	for _, owner := range oracle.keys[:threshold] {
		raw, err := crypto.Sign(review.Transaction.Digest[:], owner)
		if err != nil {
			t.Fatal(err)
		}
		raw[64] += 27
		signatures = append(signatures, raw...)
	}
	registry := bootstrapSuccessorExecutionTestDirectory(t)
	prepareBootstrapSuccessorMembersTest(t, registry, true)
	// The separate nonce registry is explicitly declared before approving its
	// execution scope. Existing original root generations and heads are retained.
	f.root.storage = durablefixture.New(t, t.Context(), append(append([]string{}, f.root.storage.Roots...), registry)...)
	f.contracts.storage = f.root.storage
	executionRequest := bootstrapSuccessorExecutionRequest{Schema: schema, SafeReviewHash: review.ContentHash,
		RegistryDirectory: registry, Owners: oracle.owners, Singleton: singleton,
		SafeSignatures: bootstrapSuccessorExecutionTestRaw(t, "synthetic-canonical-safe-signatures.bin", signatures)}
	draft := bootstrapSuccessorExecutionPlan{Review: review, Request: executionRequest, SafeSignatures: "0x" + hex.EncodeToString(signatures)}
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
	signed, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	executionRequest.RelayerTransaction = bootstrapSuccessorExecutionTestRaw(t, "synthetic-canonical-relayer.bin", signed)
	executionPath := filepath.Join(filepath.Dir(f.path), "synthetic-canonical-execution-request.json")
	bootstrapRootTestWrite(t, executionPath, executionRequest)
	self := &bootstrapSuccessorCanonicalFixture{t: t, original: f, key: key, profile: oracle.profile,
		paths: []string{"--request", requestPath, "--safe-request", safePath, "--execution-request", executionPath}}
	stdout.Reset()
	if code, diagnostic := self.invoke("contract-successor-execution-preview", &stdout); code != 0 {
		t.Fatal("canonical execution preview", code, diagnostic)
	}
	var executionPreview struct {
		Schema       string                          `json:"schema"`
		Plan         bootstrapSuccessorExecutionPlan `json:"plan"`
		PlanHash     string                          `json:"execution_plan_hash"`
		SigningBytes string                          `json:"execution_signing_bytes"`
	}
	if err := decodePlanJson(stdout.Bytes(), &executionPreview); err != nil {
		t.Fatal(err)
	}
	self.approval = bootstrapSuccessorExecutionTestSign(t, executionPreview.Plan, key, self.profile)
	approvalRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "synthetic-canonical-execution-approval.json"), self.approval)
	self.approvalArgs = []string{"--approval", approvalRef.Path, "--approval-sha256", approvalRef.Sha256, "--accept-execution-hash", executionPreview.PlanHash}
	if beforeClaim != nil {
		beforeClaim(self)
	}
	stdout.Reset()
	if code, diagnostic := self.invoke("contract-successor-execution-claim", &stdout, self.approvalArgs...); code != 0 {
		t.Fatal("canonical execution claim", code, diagnostic)
	}
	currentRuntime := f.contracts.config.Plan.Runtime
	currentRuntime.RuntimeVersion.SpecVersion++
	self.canonical = bootstrapSuccessorCanonicalTestApproval(t, executionPreview.Plan, key, currentRuntime)
	self.canonicalRef = bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "synthetic-canonical-authorization.json"), self.canonical)
	func() {
		f.contracts.stateLock.Lock()
		defer f.contracts.stateLock.Unlock()
		upgradeNative, originalOverride := f.contracts.head, f.contracts.override
		f.contracts.override = func(method string, params []any, result any) any {
			result = originalOverride(method, params, result)
			if method == "state_getRuntimeVersion" {
				for number, hash := range f.contracts.hashes {
					if number >= upgradeNative && hash == params[0] {
						return currentRuntime.RuntimeVersion
					}
				}
			}
			return result
		}
		f.contracts.tx, f.contracts.raw, f.contracts.receipt = tx, signed, nil
		f.contracts.vm.Origin, f.contracts.vm.GasLimit = request.IntendedRelayer, tx.Gas()
		f.contracts.callIntrinsicGas = true
	}()
	return self
}

// Separate private evidence files and a new signature domain bind the original
// independent key to explicit build review and scoped signer inventory.
func bootstrapSuccessorCanonicalTestApproval(t *testing.T, plan bootstrapSuccessorExecutionPlan, key ed25519.PrivateKey, profile rootReceiptProfile) bootstrapSuccessorCanonicalApproval {
	t.Helper()
	provenance := bootstrapSuccessorCanonicalTestProvenance(t, plan, key)
	authority := bootstrapSuccessorCanonicalAuthorization{Schema: bootstrapSuccessorCanonicalSchema, ExecutionPlanHash: plan.hash(), Policy: bootstrapSuccessorCanonicalPolicy,
		SafeBuildEvidence: bootstrapSuccessorExecutionTestRaw(t, "synthetic-safe-build.txt", []byte("synthetic independent build review")),
		CurrentRuntime:    profile, RuntimeEvidence: bootstrapSuccessorExecutionTestRaw(t, "synthetic-current-runtime.txt", []byte("synthetic independent current runtime artifact and codec review")),
		CutoverEvidence: bootstrapSuccessorExecutionTestRaw(t, "synthetic-cutover.txt", []byte("synthetic all-signers registry cutover and complete outstanding-signature inventory")),
		SafeProvenance:  bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-safe-provenance.json"), provenance)}
	message, err := authority.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapSuccessorCanonicalApproval{Authorization: authority, Signature: hex.EncodeToString(ed25519.Sign(key, message))}
}

// The fixture review statement is separately signed and pinned. It is still
// insufficient for public submission, which has no provenance authenticator.
func bootstrapSuccessorCanonicalTestProvenance(t *testing.T, plan bootstrapSuccessorExecutionPlan, key ed25519.PrivateKey) bootstrapSuccessorSafeProvenanceApproval {
	t.Helper()
	p := bootstrapSuccessorSafeProvenance{Schema: bootstrapSuccessorSafeProvenanceSchema, ExecutionPlanHash: plan.hash(), Safe: plan.Review.Transaction.Safe,
		Version: plan.Review.Request.Version, Variant: plan.Review.Request.Variant, Singleton: plan.Request.Singleton,
		ThroughNativeNumber: plan.Review.Request.StartNativeNumber, ThroughNativeHash: common.HexToHash(plan.Review.Request.StartNativeHash),
		DeploymentTransactionHash: crypto.Keccak256Hash([]byte("synthetic reviewed Safe deployment")), Policy: bootstrapSuccessorSafeProvenancePolicy,
		HistoryEvidence: bootstrapSuccessorExecutionTestRaw(t, "synthetic-safe-history.txt", []byte("synthetic complete clean initialization, delegatecall and storage history"))}
	pin, err := loadSafeReleasePin(p.Version, p.Variant)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range pin.Artifacts {
		if artifact.Name == "SafeProxy" {
			p.SafeProxyRuntimeHash = common.HexToHash(artifact.RuntimeKeccak256)
		} else if artifact.Name == p.Variant {
			p.SingletonRuntimeHash = common.HexToHash(artifact.RuntimeKeccak256)
		}
	}
	message, err := p.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapSuccessorSafeProvenanceApproval{Provenance: p, Signature: hex.EncodeToString(ed25519.Sign(key, message))}
}

// This explicitly synthetic capability models the fixture's controlled setup
// and history. It is never reachable from public dispatch, a file or a flag and
// is not claimed to authenticate an arbitrary deployed Safe's storage history.
func (self *bootstrapSuccessorCanonicalFixture) authenticate(ctx context.Context, plan bootstrapSuccessorExecutionPlan, provenance bootstrapSuccessorSafeProvenanceApproval, chain *evmOwnedChain, head chainIdentity) error {
	if chain == nil || chain.client.url != self.original.contracts.server.URL || plan.hash() != self.approval.Plan.hash() ||
		provenance.Provenance.Safe != plan.Review.Transaction.Safe || head.GenesisHash != self.original.config.Network.GenesisHash || head.FinalizedNumber < provenance.Provenance.ThroughNativeNumber {
		return errors.New("synthetic canonical provenance fixture scope differs")
	}
	// The synchronous barrier changes authority while authentication is in
	// progress, without a sleep or a concurrent production-state mutation.
	if self.afterProvenance != nil {
		self.afterProvenance()
	}
	return ctx.Err()
}

// Lightweight authority roots use a synthetic reviewed profile without claiming
// a chain observation. The full fixture signs its distinct post-bootstrap runtime.
func bootstrapSuccessorCanonicalTestRuntime() rootReceiptProfile {
	return rootReceiptProfile{RuntimeSourceCommit: frontierMappingSourceCommit,
		RuntimeVersion:  crv4.RuntimeVersionIdentity{SpecName: "synthetic-canonical-runtime", SpecVersion: 1, TransactionVersion: 1, StateVersion: 1},
		RuntimeCodeHash: crypto.Keccak256Hash([]byte("synthetic-runtime-code")).Hex(), RuntimeMetadataHash: crypto.Keccak256Hash([]byte("synthetic-runtime-metadata")).Hex()}
}

// Resume uses the command implementation with this fixture's explicit synthetic
// history capability. Other commands continue through public dispatch.
func (self *bootstrapSuccessorCanonicalFixture) invoke(command string, stdout *bytes.Buffer, extra ...string) (int, string) {
	var stderr bytes.Buffer
	var code int
	if command == "contract-successor-execution-resume" {
		args := []string{command, "--config", self.original.path, "--run-dir", self.original.config.RunDirectory, "--accept-plan-hash", self.original.preparation.Plan.ContentHash}
		args = append(append(args, self.paths...), extra...)
		code = runBootstrapSuccessorExecutionCommandWithProvenance(self.original.storageContext(self.t.Context()), args, stdout, &stderr, self)
	} else {
		code = self.original.command(self.t.Context(), command, stdout, &stderr, append(append([]string{}, self.paths...), extra...)...)
	}
	return code, stderr.String()
}

// Reads and submits require the same explicitly pinned canonical approval.
func (self *bootstrapSuccessorCanonicalFixture) online(stdout *bytes.Buffer, submit bool) (int, string) {
	args := append(append([]string{}, self.approvalArgs...), "--online", "--canonical-approval", self.canonicalRef.Path, "--canonical-approval-sha256", self.canonicalRef.Sha256)
	if submit {
		args = append(args, "--submit")
	}
	return self.invoke("contract-successor-execution-resume", stdout, args...)
}

// Public input reconstruction keeps all five original preparation locks held;
// the concrete constructor adds eight historical contract marker locks.
func (self *bootstrapSuccessorCanonicalFixture) open() (*bootstrapSuccessorExecutionStore, *bootstrapSuccessorCanonicalChain) {
	self.t.Helper()
	owner, adapter, _ := self.openRuntimeRevisions()
	return owner, adapter
}

// Revision tests reopen the genuine fixture with additional independent
// artifacts and explicitly close all original marker locks before a restart.
func (self *bootstrapSuccessorCanonicalFixture) openRuntimeRevisions(revisions ...bootstrapSuccessorRuntimeApproval) (*bootstrapSuccessorExecutionStore, *bootstrapSuccessorCanonicalChain, func()) {
	self.t.Helper()
	ctx := self.original.storageContext(self.t.Context())
	plan, profile, retained, err := loadBootstrapSuccessorExecution(ctx, self.original.path, self.original.config.RunDirectory,
		self.original.preparation.Plan.ContentHash, self.paths[1], self.paths[3], self.paths[5], self.approvalArgs[1], self.canonicalRef.Path)
	if err != nil {
		self.t.Fatal(err)
	}
	self.t.Cleanup(func() { retained.close() })
	owner, err := openBootstrapSuccessorExecutionStore(ctx, plan, self.approval, profile, false, nil)
	if err != nil {
		self.t.Fatal(err)
	}
	self.t.Cleanup(func() { owner.close() })
	adapter, err := newBootstrapSuccessorCanonicalChainWithProvenance(ctx, owner, self.canonical, self, revisions...)
	if err != nil {
		self.t.Fatal(err)
	}
	self.t.Cleanup(func() { adapter.close() })
	return owner, adapter, func() {
		if err := errors.Join(adapter.close(), owner.close(), retained.close()); err != nil {
			self.t.Fatal(err)
		}
	}
}

// Lightweight getter tests share only the reviewed Safe oracle and local RPC
// engine; they intentionally do not claim original-v3 custody authentication.
func bootstrapSuccessorCanonicalSafeFixture(t *testing.T) (*bootstrapSuccessorCanonicalChain, *bootstrapSuccessorExecutionFixture, *evmCreateFixture) {
	t.Helper()
	return bootstrapSuccessorCanonicalSafeFixtureFor(t, newBootstrapSuccessorExecutionFixture(t))
}

// The same local RPC engine serves whichever explicitly selected owner profile
// the model's published Safe was set up with.
func bootstrapSuccessorCanonicalSafeFixtureFor(t *testing.T, model *bootstrapSuccessorExecutionFixture) (*bootstrapSuccessorCanonicalChain, *bootstrapSuccessorExecutionFixture, *evmCreateFixture) {
	t.Helper()
	chain := newEvmCreateFixture(t)
	chain.state, chain.vm = model.oracle.state, model.oracle.vm
	chain.vm.State = chain.state
	chain.vm.ChainConfig.ChainID = big.NewInt(mainnetEvmChainId)
	chain.history = &evmCreateHistory{}
	owned, err := newEvmOwnedChain(chain.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owned.client.httpClient.CloseIdleConnections() })
	adapter := &bootstrapSuccessorCanonicalChain{chain: owned, owner: &bootstrapSuccessorExecutionStore{profile: model.profile}}
	// The oracle exposes its proxy at this plan's exact synthetic address.
	model.approval.Plan.Review.Transaction.Safe = model.oracle.transaction.Safe
	tx := model.approval.Plan.transaction()
	model.approval.Plan.Review.Transaction.Digest = model.oracle.oracleDigest(tx)
	return adapter, model, chain
}
