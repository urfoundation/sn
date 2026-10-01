//go:build linux || darwin

// The ordinary upload signer and real server admission readers meet at a
// synthetic signed production history, actual storage bytes and local evm rpc.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Legacy configuration bytes omit the new authority field, while an explicit
// production pin survives the same server configuration serialization.
func TestValidatorUploadProductionRuntimeReferencePreservesAbsentWireField(t *testing.T) {
	raw, err := json.Marshal(ValidatorUploadAdmissionConfig{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, present := fields["production_runtime_config"]; present {
		t.Fatal("absent production pin changed legacy configuration wire fields")
	}
	reference := ReleaseEvidenceV2File{Path: "/synthetic/production-runtime.yml", Bytes: 17, SHA256: attemptHex32([32]byte{1})}
	raw, err = json.Marshal(ValidatorUploadAdmissionConfig{ProductionRuntimeConfig: reference})
	if err != nil {
		t.Fatal(err)
	}
	var restored ValidatorUploadAdmissionConfig
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ProductionRuntimeConfig != reference {
		t.Fatal("explicit production pin was lost during configuration round trip")
	}
}

// Tests mutate responses only between synchronous refreshes. Storage reads
// remain exact-block, while old and current permit are independently controlled.
type validatorUploadProductionTestFixture struct {
	production      *productionRuntimeTestFixture
	upload          *validatorUploadAuthorityTestFixture
	stake           *productionRuntimeStakeTestState
	config          ValidatorUploadAdmissionConfig
	historicalReads uint64
	currentReads    uint64
	currentPermit   bool
}

// Two distinct unknown runtime artifacts are explicitly signed. No catalog
// version or fabricated admission verdict can stand in for the old activation.
func newValidatorUploadProductionTestFixture(t *testing.T) *validatorUploadProductionTestFixture {
	t.Helper()
	production := newProductionRuntimeTestFixture(t, true)
	upload := newValidatorUploadAuthorityTestFixture(t)
	record := &upload.authority.Expected
	record.Domain.ChainID, record.Domain.GenesisHash, record.Domain.Netuid = production.cfg.ChainID, [32]byte(production.rpc.genesis), production.cfg.Netuid
	record.Domain.Coordinator = [20]byte(common.HexToAddress(production.cfg.Coordinator))
	record.Domain.SettlementVault = [20]byte(common.HexToAddress(production.cfg.SettlementVault))
	record.Domain.DeploymentIDHash = sha256.Sum256([]byte(production.cfg.DeploymentID))
	record.Domain.PolicyHash, _ = parseHash32("production policy", production.cfg.PolicyHash)
	record.NativeHash = [32]byte(mainnetRuntimeTestBlock(100))
	upload.chain.chainId = new(big.Int).SetUint64(production.cfg.ChainID)
	upload.chain.contractAddr = common.Address(record.Domain.Coordinator)
	upload.native.chain, upload.native.expected = production.rpc.native, releaseNativeRuntimeIdentity(production.cfg)
	upload.authority.NativeRuntime = upload.native.expected
	upload.records = make(map[[32]byte]ValidatorEvidenceActivationPublication)
	upload.events = nil
	upload.publish(t, *record, 1001, ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...)))
	self := &validatorUploadProductionTestFixture{production: production, upload: upload, currentPermit: true}
	self.stake = installProductionRuntimeStakeTest(t, production, record.Hotkey)
	metadata, _, err := crv4.DecodeRuntimeMetadata(production.rpc.metadata)
	if err != nil {
		t.Fatal(err)
	}
	timestampKey, err := types.CreateStorageKey(metadata, "Timestamp", "Now")
	if err != nil {
		t.Fatal(err)
	}
	original := production.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient).callContext
	production.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient).callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getStorage" || method == "state_call" {
			if len(args) == 0 {
				return errors.New("production upload storage omits its block")
			}
			block := fmt.Sprint(args[len(args)-1])
			current := block == mainnetRuntimeTestBlock(production.rpc.head).Hex()
			if method == "state_getStorage" && args[0] == timestampKey.Hex() {
				if !current || len(args) != 2 {
					return errors.New("production upload timestamp escaped its current block")
				}
				return setReleaseHistoricalTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint64(nil, upload.currentMillis)))
			}
			if current {
				self.currentReads++
				args = append([]any(nil), args...)
				args[len(args)-1] = mainnetRuntimeTestBlock(100).Hex()
				priorPermit := self.stake.permit
				self.stake.permit = self.currentPermit
				defer func() { self.stake.permit = priorPermit }()
			} else {
				self.historicalReads++
			}
		}
		return original(ctx, result, method, args...)
	}
	self.config = upload.config()
	raw, err := os.ReadFile(production.path)
	if err != nil {
		t.Fatal(err)
	}
	self.config.ProductionRuntimeConfig = mainnetRuntimeTestWriteBytes(t, filepath.Join(identityTestStateDir(t), "production-runtime.yml"), raw)
	return self
}

// The same constructor used by the server loads only read-only projection.
// The test owns refresh sequencing and joins every lease during cleanup.
func (self *validatorUploadProductionTestFixture) owner(t *testing.T) *ValidatorUploadAdmission {
	t.Helper()
	owner, err := newValidatorUploadAdmissionState(t.Context(), self.upload.chain, self.production.rpc.native, self.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
	return owner
}

// A current exact artifact cannot decode the original unknown runtime. The
// signed predecessor window admits it through the actual downstream refresh.
func TestValidatorUploadProductionRuntimeAdmitsApprovedHistoricalEnvelope(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	native := fixture.production.rpc.native
	metadata, runtime := native.Meta, native.Runtime
	owner, err := NewValidatorUploadAdmission(t.Context(), fixture.upload.chain, native, fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	if len(HistoricalReleaseRuntimeArtifacts(fixture.config.Deployment.NativeRuntime)) != 1 {
		t.Fatal("fixture accidentally inherits a compiled historical artifact")
	}
	if err := owner.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	data := []byte("production-upload-object")
	header, session, digest := validatorUploadAdmissionTestHeader(t, fixture.upload, fixture.upload.authority.Expected, 0x41, "synthetic-client-session", data)
	lease, err := owner.Begin(t.Context(), header, session, 1, digest, uint64(len(data)))
	if err != nil {
		t.Fatalf("ordinary signed production upload was rejected by server admission: %v", err)
	}
	if err := lease.Start(true); err != nil {
		lease.Close()
		t.Fatal(err)
	}
	lease.Close()
	owner.Close()
	if fixture.historicalReads == 0 || fixture.currentReads == 0 || native.Meta != metadata || native.Runtime != runtime || native.ValidateValidatorProducerRuntime(fixture.config.Deployment.NativeRuntime) == nil {
		t.Fatal("admission omitted real eligibility or acquired a producer signing view")
	}
	if _, err := os.Stat(fixture.production.cfg.HotkeySeedFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only projection touched native custody: %v", err)
	}
	// A server restart independently loads the same signed bytes. The already
	// loaded projection then survives deletion without reopening mutable paths.
	restarted := fixture.owner(t)
	if err := os.Remove(fixture.config.ProductionRuntimeConfig.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.production.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	if err := restarted.refresh(t.Context(), fixture.upload.now); err != nil || len(restarted.entries) != 1 {
		t.Fatalf("loaded immutable projection lost original history: %v", err)
	}
}

// A renewed config retains the original complete signed authority. The server
// projects its old runtime window without requiring a duplicate history doc or
// acquiring economic/signing authority from the retained bundle.
func TestValidatorUploadProductionRuntimeRetainsRenewedAuthorityWindow(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	production := fixture.production
	originalCfg := production.cfg
	current, _ := productionAuthorityTestSuccessor(t, originalCfg, production.approval, production.private, true)
	production.cfg = current
	production.path = writeReleaseConfig(t, *current)
	record := &fixture.upload.authority.Expected
	record.NativeBlock = 101
	record.NativeHash = [32]byte(mainnetRuntimeTestBlock(101))
	fixture.upload.records = make(map[[32]byte]ValidatorEvidenceActivationPublication)
	fixture.upload.events = nil
	fixture.upload.publish(t, *record, 1001, ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...)))
	fixture.config.Deployment.NativeRuntime = releaseNativeRuntimeIdentity(current)
	raw, err := os.ReadFile(production.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.config.ProductionRuntimeConfig = mainnetRuntimeTestWriteBytes(t, filepath.Join(identityTestStateDir(t), "renewed-production.yml"), raw)
	client := production.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		// The synthetic registrations/stake are unchanged from block 100 to
		// 101, while runtime identity remains bound to the actual queried block.
		if (method == "state_getStorage" || method == "state_call") && len(args) != 0 && args[len(args)-1] == mainnetRuntimeTestBlock(101).Hex() {
			args = append([]any(nil), args...)
			args[len(args)-1] = mainnetRuntimeTestBlock(100).Hex()
		}
		if method == "state_getRuntimeVersion" || method == "state_getStorageHash" {
			hash, err := types.NewHashFromHexString(fmt.Sprint(args[len(args)-1]))
			if err != nil {
				return err
			}
			if binary.LittleEndian.Uint64(hash[:8]) >= 102 {
				if method == "state_getStorageHash" {
					return setReleaseHistoricalTestResult(result, current.RuntimeCodeHash)
				}
				return setReleaseHistoricalTestResult(result, map[string]any{"specName": "node-subtensor", "specVersion": current.RuntimeSpec,
					"transactionVersion": current.TransactionVersion, "stateVersion": current.StateVersion, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
			}
		}
		return original(ctx, result, method, args...)
	}
	owner, err := NewValidatorUploadAdmission(t.Context(), fixture.upload.chain, production.rpc.native, fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	if err := owner.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	data := []byte("renewed-production-upload")
	header, session, digest := validatorUploadAdmissionTestHeader(t, fixture.upload, *record, 0x41, "retained-production-session", data)
	lease, err := owner.Begin(t.Context(), header, session, 1, digest, uint64(len(data)))
	if err != nil {
		t.Fatalf("renewal lost the actual original activation admission: %v", err)
	}
	lease.Close()
	owner.Close()
	if fixture.historicalReads == 0 || fixture.currentReads == 0 || production.rpc.native.ValidateValidatorProducerRuntime(fixture.config.Deployment.NativeRuntime) == nil {
		t.Fatal("renewed upload omitted native evidence or acquired producer authority")
	}
	restarted := fixture.owner(t)
	old, err := restarted.config.Deployment.runtimeArtifactsAt(101, true)
	if err != nil || len(old) != 1 || old[0] != releaseNativeRuntimeIdentity(originalCfg) {
		t.Fatalf("upload projection lost the exact original runtime: %v", err)
	}
	if _, err := restarted.config.Deployment.runtimeArtifactsAt(101, false); err == nil {
		t.Fatal("original bundle widened the current runtime window")
	}
	for _, path := range []string{fixture.config.ProductionRuntimeConfig.Path, current.OwnerRecycleApproval.Approval.Path, current.ProductionAuthorityHistory[0].Path} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := restarted.refresh(t.Context(), fixture.upload.now); err != nil || len(restarted.entries) != 1 {
		t.Fatalf("renewed runtime projection reopened economic authority: %v", err)
	}
}

// The same current bytes outside the signed interval cannot refresh staging.
// The production loop invalidates its cache on this error and cancels leases.
func TestValidatorUploadProductionRuntimeRejectsCurrentWindowExpiry(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	owner := fixture.owner(t)
	if err := owner.refresh(t.Context(), fixture.upload.now); err != nil {
		t.Fatal(err)
	}
	header, session, digest := validatorUploadAdmissionTestHeader(t, fixture.upload, fixture.upload.authority.Expected, 0x41, "synthetic-client-session", []byte("object"))
	lease, err := owner.beginAt(t.Context(), header, session, 1, digest, 6, fixture.upload.now)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	for _, head := range []uint64{100, 201} {
		fixture.production.rpc.head = head
		before := fixture.currentReads
		observation, err := ValidatorUploadNativeObserverContext(t.Context(), fixture.production.rpc.native, owner.config.Deployment)
		if err == nil || observation != (ValidatorUploadNativeObserver{}) || fixture.currentReads != before {
			t.Fatalf("current runtime escaped signed window at %d: %+v %v", head, observation, err)
		}
	}
	err = owner.refresh(t.Context(), fixture.upload.now)
	if err == nil {
		t.Fatal("expired current runtime refreshed upload eligibility")
	}
	owner.invalidate(err)
	<-lease.Context().Done()
	if len(owner.entries) != 0 {
		t.Fatal("expired runtime retained a usable upload generation")
	}
	fixture.production.rpc.head = 200
	if err := owner.refresh(t.Context(), fixture.upload.now); err != nil || len(owner.entries) != 1 {
		t.Fatalf("inclusive approved upper bound was lost: %v", err)
	}
}

// Old consent never substitutes for present permit. Both observations are
// actual storage reads, independently varied without a timing-based race.
func TestValidatorUploadProductionRuntimeRequiresCurrentPermit(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	owner := fixture.owner(t)
	if err := owner.refresh(t.Context(), fixture.upload.now); err != nil || len(owner.entries) != 1 {
		t.Fatalf("initial admission: %v", err)
	}
	fixture.currentPermit = false
	if err := owner.refresh(t.Context(), fixture.upload.now); err != nil || len(owner.entries) != 0 || !fixture.stake.permit {
		t.Fatalf("historical permit replaced current eligibility: %v", err)
	}
}

// Exact hash selection and signature verification both precede remote work.
// Merely selecting schema 3 or a self-described runtime cannot grant admission.
func TestValidatorUploadProductionRuntimeRejectsChangedPinnedInputs(t *testing.T) {
	for _, fault := range []string{"missing-reference", "wrong-hash", "changed-config", "foreign-deployment", "wrong-chain", "expanded-census"} {
		fixture := newValidatorUploadProductionTestFixture(t)
		config := fixture.config
		switch fault {
		case "missing-reference":
			config.ProductionRuntimeConfig = ReleaseEvidenceV2File{}
		case "wrong-hash":
			config.ProductionRuntimeConfig.SHA256 = attemptHex32([32]byte{1})
		case "changed-config":
			raw, err := os.ReadFile(config.ProductionRuntimeConfig.Path)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte(fmt.Sprintf("validator_id: %d\n", fixture.production.cfg.ValidatorID)), []byte("validator_id: 9999\n"), 1)
			config.ProductionRuntimeConfig = mainnetRuntimeTestWriteBytes(t, config.ProductionRuntimeConfig.Path, raw)
		case "foreign-deployment":
			config.Deployment.DeploymentIDHash[0] ^= 1
		case "wrong-chain":
			config.Deployment.ChainID = 945
		case "expanded-census":
			config.Deployment.MaximumSubnetUIDs = fixture.production.approval.MaximumSubnetUids + 1
		}
		before := fixture.upload.calls.Load()
		owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.upload.chain, fixture.production.rpc.native, config)
		if err == nil || owner != nil || fixture.upload.calls.Load() != before || len(fixture.production.rpc.callKVs) != 0 {
			if owner != nil {
				owner.cancel()
			}
			t.Fatalf("%s became upload authority or performed remote work: %v", fault, err)
		}
	}
}

// A private projection cannot be transplanted into a changed deployment. Its
// exact native route identity is refreshed independently of cached dial fields.
func TestValidatorUploadProductionRuntimeRejectsAuthorityAndRouteSubstitution(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	owner := fixture.owner(t)
	deployment := owner.config.Deployment
	deployment.NativeRuntime.CodeHash = (types.Hash{0xf1}).Hex()
	if _, err := deployment.runtimeArtifactsAt(100, true); err == nil {
		t.Fatal("copied authority accepted a changed public runtime")
	}
	deployment = owner.config.Deployment
	deployment.productionRuntime = nil
	if _, err := deployment.runtimeArtifactsAt(100, true); err == nil {
		t.Fatal("identity-only mainnet inferred approved predecessor history")
	}
	native := fixture.production.rpc.native
	for _, fault := range []string{"chain", "genesis", "evm", "route"} {
		priorChain, priorGenesis, priorEvm, priorClient := fixture.production.rpc.nativeChain, fixture.production.rpc.genesis, fixture.production.rpc.evmChainId, native.API.Client
		switch fault {
		case "chain":
			fixture.production.rpc.nativeChain = "Synthetic Foreign Chain"
		case "genesis":
			fixture.production.rpc.genesis[0] ^= 1
		case "evm":
			fixture.production.rpc.evmChainId = "0x999"
		case "route":
			native.API.Client = &recycleAdmissionRouteClient{validatorRuntimeIdentityTestClient: priorClient.(*validatorRuntimeIdentityTestClient), route: "wss://unapproved-upload.example"}
		}
		if observation, err := ValidatorUploadNativeObserverContext(t.Context(), native, owner.config.Deployment); err == nil || observation != (ValidatorUploadNativeObserver{}) {
			t.Fatalf("cached native identity concealed %s substitution: %v", fault, err)
		}
		fixture.production.rpc.nativeChain, fixture.production.rpc.genesis, fixture.production.rpc.evmChainId, native.API.Client = priorChain, priorGenesis, priorEvm, priorClient
	}
}

// An unknown historical runtime remains denied even when the live artifact
// is still current and the activation carries valid native and vpk consent.
func TestValidatorUploadProductionRuntimeRejectsUnapprovedOriginalArtifact(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	owner := fixture.owner(t)
	fixture.production.rpc.versions[0].SpecVersion--
	if err := owner.refresh(t.Context(), fixture.upload.now); err != nil {
		t.Fatal(err)
	}
	if len(owner.entries) != 0 || fixture.historicalReads != 0 {
		t.Fatal("unapproved predecessor acquired a staging owner or storage authority")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := loadValidatorUploadRuntimeContext(ctx, fixture.config.Deployment, fixture.config.ProductionRuntimeConfig); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled projection did not fail before file work: %v", err)
	}
}
