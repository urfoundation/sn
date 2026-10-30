// Public review exports retain independently approved synthetic source identity
// and never acquire a signature from the admitted bootstrap config.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

func bootstrapValidatorOriginalRequestWrite(t *testing.T, path string, request bootstrapValidatorOriginalRoleRequest) string {
	t.Helper()
	bootstrapRootTestWrite(t, path, request)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

func bootstrapValidatorOriginalPreparationWrite(t *testing.T, path string, preparation validator.ProviderAttemptRequestPreparation) validator.ReleaseEvidenceV2File {
	t.Helper()
	raw, err := json.Marshal(preparation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return validator.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(raw)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(raw))}
}

// Client/VPK and request birth are explicit independent fixture inputs. They
// are not computed from original config approval signers or current SQL state.
func newBootstrapValidatorOriginalRoleFixture(t *testing.T) (*bootstrapChainFixture, bootstrapValidatorOriginalRoleRequest, string, string) {
	t.Helper()
	f, _ := newBootstrapContractRoleFixture(t)
	directory := filepath.Dir(f.path)
	request := bootstrapValidatorOriginalRoleRequest{Schema: bootstrapValidatorOriginalRoleSchema, PreparationHash: f.preparation.Plan.ContentHash, ServerProfile: "testnet"}
	for index, original := range f.validators {
		cfg := original.config
		selection := bootstrapValidatorOriginalRoleSelection{ValidatorId: cfg.ValidatorID}
		for _, operator := range cfg.Operators {
			key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(130 + index*2 + int(operator.NoID))}, ed25519.SeedSize))
			preparation := validator.ProviderAttemptRequestPreparation{Identity: validator.ProviderAttemptRequestIdentity{
				Ledger:      validator.AttemptLedgerIdentity{DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash, Netuid: cfg.Netuid, ValidatorID: cfg.ValidatorID, ValidatorUID: uint16(index), NoID: operator.NoID, ValidatorVPK: fmt.Sprintf("0x%x", key.Public().(ed25519.PublicKey))},
				Coordinator: strings.ToLower(cfg.Coordinator), ClientId: connect.Id{byte(150 + index*2 + int(operator.NoID))}, PolicyHash: common.HexToHash(cfg.PolicyHash)},
				Limits: validator.ProviderAttemptRequestLimits{MaxRecords: 128, MaxRecordBytes: 8192, MaxJournalBytes: 8 * 1024 * 1024},
				Birth:  validator.AttemptBoundary{SettlementEpoch: 7, EVMBlock: 100, EVMBlockHash: "0x" + strings.Repeat("72", 32)}}
			if err := preparation.Validate(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, fmt.Sprintf("validator-%d-operator-%d-requests.json", cfg.ValidatorID, operator.NoID))
			selection.Operators = append(selection.Operators, validator.ProductionOriginalRequestSelection{NoId: operator.NoID, Preparation: bootstrapValidatorOriginalPreparationWrite(t, path, preparation)})
		}
		request.Validators = append(request.Validators, selection)
	}
	path := filepath.Join(directory, "validator-source-roles.json")
	return f, request, path, bootstrapValidatorOriginalRequestWrite(t, path, request)
}

func bootstrapValidatorOriginalRoleArgs(f *bootstrapChainFixture, path, digest string) []string {
	return []string{"bootstrap-chain", "validator-source-role-config", "--config", f.path, "--sources", path, "--sources-sha256", digest}
}

// The public command emits the exact runtime OperatorConfig additions, complete
// offline publication scope and hashes, while retaining every original signature.
func TestBootstrapValidatorOriginalRoleExportBindsActualOperatorAndOriginalHashes(t *testing.T) {
	f, request, path, digest := newBootstrapValidatorOriginalRoleFixture(t)
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	originals := map[string][]byte{}
	for _, original := range f.validators {
		for _, path := range []string{original.path, original.config.OwnerRecycleApproval.Approval.Path} {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			originals[path] = raw
		}
	}
	var first, second, stderr bytes.Buffer
	args := bootstrapValidatorOriginalRoleArgs(f, path, digest)
	if code := runMain(f.storageContext(t.Context()), args, &first, &stderr); code != 0 {
		t.Fatal("actual validator source role export failed", code, stderr.String())
	}
	if code := runMain(f.storageContext(t.Context()), args, &second, &stderr); code != 0 || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("exact original source export changed on repeat", code, stderr.String())
	}
	var result bootstrapValidatorOriginalRoleConfig
	if err := decodePlanJson(first.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	seal := result.ContentHash
	result.ContentHash = ""
	if seal != rootObjectHash(result) || !result.RequiresOwnerSigningAndAdoption || !result.RequiresStoppedWriterPreparation || result.ActivationReady || result.NetworkEffects || result.Request.Path != path || result.Request.Sha256 != digest || result.ServerProfile != "testnet" || len(result.Validators) != len(f.validators) {
		t.Fatal("unsigned source patch lost original hashes, explicit profile or required owner adoption")
	}
	for index, patch := range result.Validators {
		cfg := f.validators[index].config
		if patch.OriginalConfig != f.preparation.Plan.Config.Validators[index].Config || patch.ValidatorId != cfg.ValidatorID || len(patch.RoleConfig.Operators) != len(cfg.Operators) {
			t.Fatal("source patch changed original config identity or complete operator roster")
		}
		for opIndex, operator := range patch.RoleConfig.Operators {
			reference := request.Validators[index].Operators[opIndex].Preparation
			scope := operator.RequestReceiptScope
			if operator.RequestPreparation == nil || *operator.RequestPreparation != reference || scope == nil || scope.Profile != request.ServerProfile || scope.GenesisHash != common.HexToHash(cfg.GenesisHash) || scope.PolicyHash != common.HexToHash(cfg.PolicyHash) || scope.DeploymentId != cfg.DeploymentID || scope.DeploymentKey != fmt.Sprintf("%d:%s", cfg.ChainID, strings.ToLower(common.HexToAddress(cfg.Coordinator).Hex())) || scope.Netuid != uint64(cfg.Netuid) || scope.NoId != operator.NoID {
				t.Fatal("actual OperatorConfig lost approved request or exact original Server scope")
			}
			original := cfg.Operators[opIndex]
			operator.RequestPreparation, operator.RequestReceiptScope = original.RequestPreparation, original.RequestReceiptScope
			if !reflect.DeepEqual(operator, original) {
				t.Fatal("source export rewrote unrelated original operator settings")
			}
		}
		publication := patch.RoleConfig.PublicationPreparation
		hash, err := publication.Digest()
		if err != nil || hash != patch.RoleConfig.PublicationSha256 || publication.StateDir != cfg.StateDir || publication.Directory() != filepath.Join(cfg.StateDir, "provider-attempt-publications") || len(publication.Operators) != len(cfg.Operators) {
			t.Fatal("source export lost complete bounded publication preparation", err)
		}
		for _, operator := range publication.Operators {
			if operator.Preparation.Identity.ClientId == (connect.Id{}) || operator.Preparation.Identity.Ledger.ValidatorVPK == "" || operator.ReceiptScope.Profile != request.ServerProfile {
				t.Fatal("source output inferred or omitted independent client/key/profile")
			}
		}
	}
	for path, original := range originals {
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, original) {
			t.Fatal("review export changed or re-signed original admitted configuration", err)
		}
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !reflect.DeepEqual(reads, f.contracts.counts) {
		t.Fatal("source export changed original preparation custody or contacted chain")
	}
}

func TestBootstrapValidatorOriginalRoleRefusesMissingProfileOrIncompleteRoster(t *testing.T) {
	f, original, path, _ := newBootstrapValidatorOriginalRoleFixture(t)
	for _, change := range []string{"missing-profile", "unknown-profile", "validator", "operator", "duplicate"} {
		request := original
		request.Validators = append([]bootstrapValidatorOriginalRoleSelection(nil), original.Validators...)
		switch change {
		case "missing-profile":
			request.ServerProfile = ""
		case "unknown-profile":
			request.ServerProfile = "production"
		case "validator":
			request.Validators = request.Validators[:1]
		case "operator":
			request.Validators[0].Operators = request.Validators[0].Operators[:1]
		case "duplicate":
			request.Validators[1] = request.Validators[0]
		}
		digest := bootstrapValidatorOriginalRequestWrite(t, path, request)
		var stdout bytes.Buffer
		if code := runMain(f.storageContext(t.Context()), bootstrapValidatorOriginalRoleArgs(f, path, digest), &stdout, io.Discard); code != 2 || stdout.Len() != 0 {
			t.Fatal("public source export defaulted authority or published a partial census", change, code)
		}
	}
}

func TestBootstrapValidatorOriginalRoleRefusesForeignPreparedScope(t *testing.T) {
	f, request, path, _ := newBootstrapValidatorOriginalRoleFixture(t)
	reference := request.Validators[0].Operators[0].Preparation
	raw, err := os.ReadFile(reference.Path)
	if err != nil {
		t.Fatal(err)
	}
	var original validator.ProviderAttemptRequestPreparation
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"genesis", "chain", "deployment", "validator", "operator", "coordinator", "policy", "client", "capacity"} {
		preparation := original
		switch change {
		case "genesis":
			preparation.Identity.Ledger.GenesisHash = "0x" + strings.Repeat("76", 32)
		case "chain":
			preparation.Identity.Ledger.ChainID++
		case "deployment":
			preparation.Identity.Ledger.DeploymentID += "-foreign"
		case "validator":
			preparation.Identity.Ledger.ValidatorID++
		case "operator":
			preparation.Identity.Ledger.NoID++
		case "coordinator":
			preparation.Identity.Coordinator = "0x" + strings.Repeat("78", 20)
		case "policy":
			preparation.Identity.PolicyHash[0] ^= 1
		case "client":
			preparation.Identity.ClientId = connect.Id{}
		case "capacity":
			preparation.Limits.MaxJournalBytes = 1024 * 1024 * 1024 * 1024
		}
		request.Validators[0].Operators[0].Preparation = bootstrapValidatorOriginalPreparationWrite(t, reference.Path, preparation)
		digest := bootstrapValidatorOriginalRequestWrite(t, path, request)
		var stdout bytes.Buffer
		if code := runMain(f.storageContext(t.Context()), bootstrapValidatorOriginalRoleArgs(f, path, digest), &stdout, io.Discard); code != 2 || stdout.Len() != 0 {
			t.Fatal("review export borrowed an approved hash for foreign original scope", change, code)
		}
	}
}

func TestBootstrapValidatorOriginalRoleRetainsExactInputsAcrossOutputFailureAndCancellation(t *testing.T) {
	f, _, path, digest := newBootstrapValidatorOriginalRoleFixture(t)
	args := bootstrapValidatorOriginalRoleArgs(f, path, digest)
	if code := runMain(f.storageContext(t.Context()), args, bootstrapContractRoleFailedOutput{}, io.Discard); code != 1 {
		t.Fatal("source export hid its failed review artifact output", code)
	}
	var stdout bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), args, &stdout, io.Discard); code != 0 || stdout.Len() == 0 {
		t.Fatal("unchanged source inputs could not repeat after output loss", code)
	}
	ctx, cancel := context.WithCancel(f.storageContext(t.Context()))
	cancel()
	stdout.Reset()
	if code := runMain(ctx, args, &stdout, io.Discard); code != 2 || stdout.Len() != 0 {
		t.Fatal("canceled source export lost caller ownership", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if code := runMain(f.storageContext(t.Context()), args, &stdout, io.Discard); code != 2 || stdout.Len() != 0 {
		t.Fatal("source export accepted a changed manifest under the prior independent hash", code)
	}
}
