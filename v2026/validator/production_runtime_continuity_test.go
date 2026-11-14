// Independent synthetic signatures, real metadata and fully hashed headers
// exercise the proposal boundary without granting new producer authority.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Calls are synchronous; tests mutate faults only between joined observations.
// The candidate at150 follows an unchanged original artifact at149.
type productionContinuityPolicyTestFixture struct {
	owner          *productionRuntimeTestFixture
	verifier       ed25519.PrivateKey
	policy         ProductionRuntimeContinuityPolicy
	result         ProductionRuntimeSemanticResult
	policyRaw      []byte
	certificateRaw []byte
	candidate      crv4.RuntimeArtifactIdentity
	head           uint64
	hashes         map[uint64]types.Hash
	headers        map[types.Hash]types.Header
	calls          int
	fault          func(context.Context, any, string, ...any) (bool, error)
}

// Fixture issuers are separate from node keys and from each other. No genuine
// semantic verifier is invented: its synthetic certificate remains an assertion.
func newProductionContinuityPolicyTestFixture(t *testing.T) *productionContinuityPolicyTestFixture {
	t.Helper()
	owner := newProductionRuntimeTestFixture(t, false)
	self := &productionContinuityPolicyTestFixture{owner: owner, verifier: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize)),
		head: 150, hashes: map[uint64]types.Hash{}, headers: map[types.Hash]types.Header{}, candidate: releaseNativeRuntimeIdentity(owner.cfg)}
	self.candidate.Version.SpecVersion++
	self.candidate.CodeHash = (types.Hash{0xe1}).Hex()
	self.policy = ProductionRuntimeContinuityPolicy{Schema: ProductionRuntimeContinuityPolicySchema,
		OriginalApprovalSha256: sha256.Sum256(owner.cfg.ownerRecycleProduction.encoded), OriginalConfigHash: owner.approval.ConfigHash,
		GenesisHash: [32]byte(owner.rpc.genesis), BaseArtifact: releaseNativeRuntimeIdentity(owner.cfg), InterfaceProfile: crv4.ValidatorProducerRuntimeProfile,
		VerifierBuildSha256: [32]byte{1}, SemanticRulesSha256: [32]byte{2}, SemanticDomains: productionRuntimeSemanticDomains(),
		ValidFromNativeBlock: 101, ValidThroughNativeBlock: 200}
	copy(self.policy.VerifierKey[:], self.verifier.Public().(ed25519.PublicKey))
	self.signPolicy(t)
	self.result = ProductionRuntimeSemanticResult{Schema: ProductionRuntimeSemanticCertificateSchema,
		Request: ProductionRuntimeSemanticRequest{Schema: ProductionRuntimeSemanticRequestSchema, PolicySha256: sha256.Sum256(self.policyRaw),
			Artifact: self.candidate, SourceCommit: strings.Repeat("37", 20), SourceBuildEvidenceHash: [32]byte{3}, ValidFromNativeBlock: 150, ValidThroughNativeBlock: 200},
		VerifierBuildSha256: self.policy.VerifierBuildSha256, SemanticRulesSha256: self.policy.SemanticRulesSha256,
		VerifiedDomains: productionRuntimeSemanticDomains(), EvidenceSha256: [32]byte{4}, Compatible: true}
	self.signCertificate(t)
	parent := types.Hash{0x71}
	for _, number := range []uint64{149, 150, 151} {
		header, hash := releaseReceiptTestHeader(t, parent, number)
		self.hashes[number], self.headers[hash], parent = hash, header, hash
	}
	client := owner.rpc.client
	original := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		self.calls++
		if self.fault != nil {
			if handled, err := self.fault(ctx, target, method, args...); handled {
				return err
			}
		}
		// Preserve the transport's decoding for typed hashes and headers as well
		// as raw replies; original production admission uses both result forms.
		assign := func(value any) error { return setReleaseHistoricalTestResult(target, value) }
		switch method {
		case "chain_getFinalizedHead":
			return assign(self.hashes[self.head].Hex())
		case "chain_getBlockHash":
			if number, ok := args[0].(uint64); ok && number != 0 {
				return assign(self.hashes[number].Hex())
			}
		case "chain_getHeader":
			for hash, header := range self.headers {
				if fmt.Sprint(args[0]) == hash.Hex() {
					return assign(releaseReceiptTestHeaderWire(header))
				}
			}
		}
		translated := append([]any(nil), args...)
		var selected uint64
		if len(args) != 0 {
			for number, hash := range self.hashes {
				if fmt.Sprint(args[len(args)-1]) == hash.Hex() {
					selected = number
					translated[len(args)-1] = mainnetRuntimeTestBlock(number).Hex()
				}
			}
		}
		if err := original(ctx, target, method, translated...); err != nil {
			return err
		}
		if selected >= 150 {
			switch method {
			case "state_getRuntimeVersion":
				v := self.candidate.Version
				return assign(map[string]any{"specName": v.SpecName, "specVersion": v.SpecVersion, "transactionVersion": v.TransactionVersion,
					"stateVersion": v.StateVersion, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
			case "state_getStorageHash":
				return assign(self.candidate.CodeHash)
			}
		}
		return nil
	}
	return self
}

// Only the independently loaded original approver signs the additional envelope.
func (self *productionContinuityPolicyTestFixture) signPolicy(t *testing.T) {
	t.Helper()
	message, err := self.policy.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	self.policyRaw, err = json.Marshal(ProductionRuntimeContinuityEnvelope{Policy: self.policy, Signature: hex.EncodeToString(ed25519.Sign(self.owner.private, message))})
	if err != nil {
		t.Fatal(err)
	}
}

// The external output key cannot change the original config or policy scope.
func (self *productionContinuityPolicyTestFixture) signCertificate(t *testing.T) {
	t.Helper()
	message, err := self.result.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	self.certificateRaw, err = json.Marshal(ProductionRuntimeSemanticCertificate{Result: self.result, Signature: hex.EncodeToString(ed25519.Sign(self.verifier, message))})
	if err != nil {
		t.Fatal(err)
	}
}

// Valid signatures and matching metadata do not install a production successor.
// Original historical reads and loaded signatures survive source-file loss.
func TestProductionRuntimeContinuityReviewKeepsProductionSelectionClosed(t *testing.T) {
	f := newProductionContinuityPolicyTestFixture(t)
	native := f.owner.rpc.native
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, f.owner.cfg, f.hashes[149]); err != nil {
		t.Fatal("original runtime fixture", err)
	}
	metadata, runtime := native.Meta, native.Runtime
	original := bytes.Clone(f.owner.cfg.ownerRecycleProduction.encoded)
	config, _ := json.Marshal(f.owner.cfg)
	if err := os.Remove(f.owner.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	f.fault = func(_ context.Context, _ any, method string, _ ...any) (bool, error) {
		if method == "state_getMetadata" {
			f.head = 151
		}
		return false, nil
	}
	report, err := InspectProductionRuntimeContinuityContext(t.Context(), native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
	if err != nil || report == nil {
		t.Fatal("signed runtime continuity inspection refused compatible successor", err)
	}
	after, _ := json.Marshal(f.owner.cfg)
	if report.Artifact != f.candidate || report.NativeHash != f.hashes[150] || report.NativeNumber != 150 || report.CheckedThroughNativeHash != f.hashes[151] ||
		report.CheckedThroughNativeNumber != 151 || !report.SemanticCertificateAuthenticated || report.SemanticsIndependentlyReplayed || report.ProductionSelectionInstalled || report.SigningAuthority ||
		report.OriginalApprovalSha256 != sha256.Sum256(original) || !bytes.Equal(config, after) || !bytes.Equal(original, f.owner.cfg.ownerRecycleProduction.encoded) ||
		native.Meta != metadata || native.Runtime != runtime || native.ProvisionalRuntimeCompatibilityEnabled() {
		t.Fatal("runtime continuity review changed custody, proof block or production authority")
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, f.owner.cfg, f.hashes[150]); err == nil {
		t.Fatal("runtime continuity certificate installed unqualified production selection")
	}
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), native, f.owner.cfg, f.hashes[149]); err != nil {
		t.Fatal("continuity review lost original historical authority", err)
	}
}

// The two signatures have different issuers and purposes. Unloaded public fields
// and copied certificates cannot create the original independent authority.
func TestProductionRuntimeContinuityRequiresIndependentSignatures(t *testing.T) {
	f := newProductionContinuityPolicyTestFixture(t)
	for _, fault := range []string{"policy", "certificate", "unloaded", "duplicate", "trailing", "oversized"} {
		policy, certificate, cfg := bytes.Clone(f.policyRaw), bytes.Clone(f.certificateRaw), *f.owner.cfg
		switch fault {
		case "policy":
			var value ProductionRuntimeContinuityEnvelope
			_ = json.Unmarshal(policy, &value)
			value.Signature = strings.Repeat("00", 64)
			policy, _ = json.Marshal(value)
			// A valid verifier output cannot repair a missing owner signature.
			result := f.result
			result.Request.PolicySha256 = sha256.Sum256(policy)
			message, err := result.SigningMessage()
			if err != nil {
				t.Fatal(err)
			}
			certificate, _ = json.Marshal(ProductionRuntimeSemanticCertificate{Result: result, Signature: hex.EncodeToString(ed25519.Sign(f.verifier, message))})
		case "certificate":
			var value ProductionRuntimeSemanticCertificate
			_ = json.Unmarshal(certificate, &value)
			value.Signature = strings.Repeat("00", 64)
			certificate, _ = json.Marshal(value)
		case "unloaded":
			cfg.ownerRecycleProduction = nil
		case "duplicate":
			policy = append([]byte(`{"policy":{},`), policy[1:]...)
		case "trailing":
			certificate = append(certificate, []byte(" {}")...)
		case "oversized":
			policy = bytes.Repeat([]byte{' '}, maximumProductionRuntimeContinuityBytes+1)
		}
		if result, err := InspectProductionRuntimeContinuityContext(t.Context(), f.owner.rpc.native, &cfg, f.hashes[150], policy, certificate); err == nil || result != nil || f.calls != 0 {
			t.Fatal("runtime continuity admitted unauthenticated " + fault)
		}
	}
}

// A newly signed policy still cannot extend original custody, widen the economic
// window, omit mandatory semantics or delegate approval back to a node key.
func TestProductionRuntimeContinuityPreservesOriginalPolicyScope(t *testing.T) {
	f := newProductionContinuityPolicyTestFixture(t)
	baseline := f.policy
	for _, fault := range []string{"approval", "config", "genesis", "base", "purpose", "window", "domains", "node", "subnet-owner", "approver", "build", "rules"} {
		f.policy = baseline
		switch fault {
		case "approval":
			f.policy.OriginalApprovalSha256[0] ^= 1
		case "config":
			f.policy.OriginalConfigHash[0] ^= 1
		case "genesis":
			f.policy.GenesisHash[0] ^= 1
		case "base":
			f.policy.BaseArtifact.Version.SpecVersion++
		case "purpose":
			f.policy.InterfaceProfile = crv4.ProvisionalRuntimeCompatibilityProfile
		case "window":
			f.policy.ValidThroughNativeBlock++
		case "domains":
			f.policy.SemanticDomains = f.policy.SemanticDomains[:1]
		case "node":
			f.policy.VerifierKey = f.owner.approval.ValidatorHotkey
		case "subnet-owner":
			f.policy.VerifierKey = f.owner.approval.SubnetOwner
		case "approver":
			copy(f.policy.VerifierKey[:], f.owner.private.Public().(ed25519.PublicKey))
		case "build":
			f.policy.VerifierBuildSha256 = [32]byte{}
		case "rules":
			f.policy.SemanticRulesSha256 = [32]byte{}
		}
		f.signPolicy(t)
		f.result.Request.PolicySha256 = sha256.Sum256(f.policyRaw)
		f.signCertificate(t)
		if _, _, err := verifyProductionRuntimeContinuity(f.owner.cfg, f.policyRaw, f.certificateRaw); err == nil {
			t.Fatal("runtime continuity policy expanded original " + fault)
		}
	}
}

// A signed semantic result must bind the exact artifact, source/build evidence,
// every semantic domain and verifier output provenance, not just metadata shape.
func TestProductionRuntimeContinuityRequiresCompleteSemanticOutput(t *testing.T) {
	f := newProductionContinuityPolicyTestFixture(t)
	baseline := f.result
	for _, fault := range []string{"policy", "build", "rules", "evidence", "domains", "incompatible", "source", "source-build", "window", "same-version", "same-code", "metadata", "transaction", "state"} {
		f.result = baseline
		switch fault {
		case "policy":
			f.result.Request.PolicySha256[0] ^= 1
		case "build":
			f.result.VerifierBuildSha256[0] ^= 1
		case "rules":
			f.result.SemanticRulesSha256[0] ^= 1
		case "evidence":
			f.result.EvidenceSha256 = [32]byte{}
		case "domains":
			f.result.VerifiedDomains = f.result.VerifiedDomains[:1]
		case "incompatible":
			f.result.Compatible = false
		case "source":
			f.result.Request.SourceCommit = "unreviewed"
		case "source-build":
			f.result.Request.SourceBuildEvidenceHash = [32]byte{}
		case "window":
			f.result.Request.ValidThroughNativeBlock++
		case "same-version":
			f.result.Request.Artifact.Version = f.policy.BaseArtifact.Version
		case "same-code":
			f.result.Request.Artifact.CodeHash = f.policy.BaseArtifact.CodeHash
		case "metadata":
			f.result.Request.Artifact.MetadataHash = (types.Hash{}).Hex()
		case "transaction":
			f.result.Request.Artifact.Version.TransactionVersion++
		case "state":
			f.result.Request.Artifact.Version.StateVersion++
		}
		f.signCertificate(t)
		if _, _, err := verifyProductionRuntimeContinuity(f.owner.cfg, f.policyRaw, f.certificateRaw); err == nil {
			t.Fatal("runtime continuity accepted incomplete semantic " + fault)
		}
	}
	for index := range 13 {
		f.result = baseline
		f.result.Request.Artifact.Version.SpecVersion += uint32(index)
		f.result.Request.Artifact.CodeHash = (types.Hash{0xe2, byte(index)}).Hex()
		f.signCertificate(t)
		if _, _, err := verifyProductionRuntimeContinuity(f.owner.cfg, f.policyRaw, f.certificateRaw); err != nil {
			t.Fatal("runtime continuity imposed a compiled version census", index, err)
		}
	}
}

// Even a genuine certificate cannot override the consumer's local structural
// checks or the selected block's freshly read exact artifact and network.
func TestProductionRuntimeContinuityChecksActualArtifactAndInterfaces(t *testing.T) {
	for _, fault := range []string{"code", "version", "genesis", "metadata", "storage-profile", "api"} {
		f := newProductionContinuityPolicyTestFixture(t)
		switch fault {
		case "code":
			f.candidate.CodeHash = (types.Hash{0xf1}).Hex()
		case "version":
			f.candidate.Version.SpecVersion++
		case "genesis":
			f.owner.rpc.genesis[0] ^= 1
		case "metadata":
			f.owner.rpc.metadata = "0x00"
		case "storage-profile":
			metadata, _, err := crv4.DecodeRuntimeMetadata(f.owner.rpc.metadata)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for i := range metadata.AsMetadataV14.Pallets {
				pallet := &metadata.AsMetadataV14.Pallets[i]
				if string(pallet.Name) == "System" {
					for j := range pallet.Storage.Items {
						if string(pallet.Storage.Items[j].Name) == "Account" {
							pallet.Storage.Items[j].Fallback = []byte{0x71}
							found = true
						}
					}
				}
			}
			if !found {
				t.Fatal("consumed account fixture absent")
			}
			f.owner.rpc.metadata, err = codec.EncodeToHex(metadata)
			if err != nil {
				t.Fatal(err)
			}
			_, hash, err := crv4.DecodeRuntimeMetadata(f.owner.rpc.metadata)
			if err != nil {
				t.Fatal(err)
			}
			f.candidate.MetadataHash, f.result.Request.Artifact.MetadataHash = hash, hash
			f.signCertificate(t)
		case "api":
			f.fault = func(_ context.Context, target any, method string, _ ...any) (bool, error) {
				if method == "state_getRuntimeVersion" {
					return true, setReleaseHistoricalTestResult(target, f.candidate.Version)
				}
				return false, nil
			}
		}
		if result, err := InspectProductionRuntimeContinuityContext(t.Context(), f.owner.rpc.native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw); err == nil || result != nil {
			t.Fatal("runtime continuity accepted observed " + fault)
		}
	}
}

// Late canonical replacement, forged complete headers, canceled work and local
// authority mutation cannot emit a successful review or install a candidate.
func TestProductionRuntimeContinuityRefusesChangedSnapshotAndCancellation(t *testing.T) {
	for _, fault := range []string{"header", "reorg", "cancel", "config"} {
		f := newProductionContinuityPolicyTestFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		armed := false
		f.fault = func(_ context.Context, target any, method string, args ...any) (bool, error) {
			if method == "state_getMetadata" {
				armed = true
				if fault == "cancel" {
					cancel()
				}
				if fault == "config" {
					f.owner.cfg.RuntimeSpec++
				}
			}
			if fault == "header" && method == "chain_getHeader" {
				changed := f.headers[f.hashes[150]]
				changed.StateRoot[0] ^= 1
				return true, setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(changed))
			}
			if fault == "reorg" && armed && method == "chain_getBlockHash" && args[0] == uint64(150) {
				return true, setReleaseHistoricalTestResult(target, (types.Hash{0x42}).Hex())
			}
			return false, nil
		}
		result, err := InspectProductionRuntimeContinuityContext(ctx, f.owner.rpc.native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
		cancel()
		if err == nil || result != nil || fault != "header" && !armed || fault == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatal("runtime continuity accepted changed observation "+fault, err)
		}
	}
}
