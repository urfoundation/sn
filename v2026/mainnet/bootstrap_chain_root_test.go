// Public command regressions separate offline root config authority from live
// root eligibility, preserve legacy bytes and refuse renewed custody on restart.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/validator"
)

// The real child service and both independent public signatures are retained,
// while successful preparation leaves all live authority/effect gates pending.
func TestBootstrapChainRootProductionConfigBoundOffline(t *testing.T) {
	f := newBootstrapChainFixture(t)
	inspection := f.preparation.Plan.RootInspection
	if inspection == nil || !reflect.DeepEqual(inspection.Plan, f.preparation.Root) || inspection.Approval != f.rootRole.approval ||
		f.preparation.Plan.Schema != "urnetwork-mainnet-bootstrap-chain-preparation-v3" {
		t.Fatal("root config inspection lost the exact child plan or approval")
	}
	// Independently assert the documented wire domain instead of sharing the
	// production signing helper with this compatibility check.
	unsigned := inspection.Approval
	unsigned.Signature = ""
	raw, err := json.Marshal(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := hex.DecodeString(inspection.Approval.Signature)
	key, _ := hex.DecodeString(f.config.RootValidator.ApprovalPublicKey[2:])
	if !ed25519.Verify(key, append([]byte("urnetwork-mainnet-root-service-approval-v1\x00"), raw...), signature) {
		t.Fatal("service config signature differs from its documented domain")
	}
	if len(f.journals(t)) != 0 {
		t.Fatal("inspection opened custody")
	}
	first := f.result(t, "apply")
	before := f.journals(t)
	if first.Schema != "urnetwork-mainnet-bootstrap-chain-result-v3" || !first.RootValidatorConfigVerified ||
		first.RootValidatorStatus != "signed-root-service-config-verified-live-authority-pending" || first.NetworkEffects || first.ActivationReady ||
		first.Root.Observations != 0 || first.Root.Broadcasts != 0 || first.Root.SignatureStatus != "awaiting-import" ||
		!reflect.DeepEqual(first, f.result(t, "resume")) || !reflect.DeepEqual(before, f.journals(t)) || len(f.contracts.counts) != 0 {
		t.Fatal("root config admission changed custody, consumed allowance or implied live authority")
	}
}

// A valid action signature authenticates only its action. Changing the service
// observation allowance and repinning every child file still needs config approval.
func TestBootstrapChainRootRejectsUnsignedServiceAllowanceChange(t *testing.T) {
	f := newBootstrapChainFixture(t)
	service := copyRootServiceConfig(f.root.plan.Service)
	service.MaximumObservations++
	f.rootRole.service(t, service)
	f.rejectValidatorPlan(t, "complete service configuration")
	// The independently approved new service is a valid distinct review.
	f.rootRole.approve(t)
	bootstrapRootTestWrite(t, f.path, f.config)
	updated, err := loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil || updated.Plan.ContentHash == f.preparation.Plan.ContentHash || updated.Plan.RootInspection.Plan.Service.MaximumObservations != service.MaximumObservations {
		t.Fatal("fresh service approval did not admit exactly the new bounded config", err)
	}
}

// A service cannot supply its own action trust by reapproving an internally
// consistent packet, even when the full-config approver accepts those bytes.
func TestBootstrapChainRootRejectsSelfSelectedActionApprover(t *testing.T) {
	f := newBootstrapChainFixture(t)
	service := copyRootServiceConfig(f.root.plan.Service)
	foreign := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x6d}, ed25519.SeedSize))
	service.CustodyTrust.ApprovalPublicKey = "0x" + hex.EncodeToString(foreign.Public().(ed25519.PublicKey))
	service.Packet = rootOfflineApprove(t, service.CustodyTrust, service.Packet.Action, foreign)
	f.rootRole.service(t, service)
	f.rootRole.approve(t)
	f.rejectValidatorPlan(t, "independent role, action approver")
}

// Missing role inputs and valid but independently different role facts cannot
// be reconstructed from the root config being inspected.
func TestBootstrapChainRootRejectsMissingOrMismatchedRoleInputs(t *testing.T) {
	for _, item := range []struct {
		name       string
		mutate     func(*bootstrapChainRootValidator)
		diagnostic string
	}{
		{name: "role", mutate: func(role *bootstrapChainRootValidator) { role.Role = "ur-validator" }},
		{name: "missing-netuid", mutate: func(role *bootstrapChainRootValidator) { role.Netuid = nil }},
		{name: "ur-netuid", mutate: func(role *bootstrapChainRootValidator) { role.Netuid = new(uint16(25)) }},
		{name: "implementation", mutate: func(role *bootstrapChainRootValidator) { role.Implementation = "sn/validator" }},
		{name: "missing-hotkey", mutate: func(role *bootstrapChainRootValidator) { role.Hotkey = "" }},
		{name: "missing-seat", mutate: func(role *bootstrapChainRootValidator) { role.Seat = nil }},
		{name: "strategy", mutate: func(role *bootstrapChainRootValidator) { role.Strategy = "accumulate_in_place" }},
		{name: "missing-action-signer", mutate: func(role *bootstrapChainRootValidator) { role.ActionApprovalPublicKey = "" }},
		{name: "missing-config-signer", mutate: func(role *bootstrapChainRootValidator) { role.ApprovalPublicKey = "" }},
		{name: "uppercase-signer", mutate: func(role *bootstrapChainRootValidator) {
			role.ApprovalPublicKey = strings.ToUpper(role.ApprovalPublicKey)
		}},
		{name: "missing-approval", mutate: func(role *bootstrapChainRootValidator) { role.Approval = planFileReference{} }},
		{name: "hotkey", mutate: func(role *bootstrapChainRootValidator) { role.Hotkey = "0x" + strings.Repeat("e1", 32) }, diagnostic: "independent role"},
		{name: "coldkey", mutate: func(role *bootstrapChainRootValidator) { role.Coldkey = "0x" + strings.Repeat("e2", 32) }, diagnostic: "independent role"},
		{name: "uid", mutate: func(role *bootstrapChainRootValidator) { role.Seat.Uid++ }, diagnostic: "independent role"},
		{name: "generation", mutate: func(role *bootstrapChainRootValidator) { role.Seat.RegistrationBlock++ }, diagnostic: "independent role"},
		{name: "action-signer", mutate: func(role *bootstrapChainRootValidator) {
			role.ActionApprovalPublicKey = "0x" + strings.Repeat("e3", 32)
		}, diagnostic: "action approver"},
		{name: "config-signer", mutate: func(role *bootstrapChainRootValidator) { role.ApprovalPublicKey = "0x" + strings.Repeat("e4", 32) }, diagnostic: "independent signer"},
	} {
		t.Logf("root role case: %s", item.name)
		f := newBootstrapChainFixture(t)
		item.mutate(f.config.RootValidator)
		diagnostic := item.diagnostic
		if diagnostic == "" {
			diagnostic = "root role requires explicit"
		}
		f.rejectValidatorPlan(t, diagnostic)
	}
}

// Absence of the independently selected root role cannot reuse embedded trust.
func TestBootstrapChainRootRejectsMissingRole(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.config.RootValidator = nil
	f.rejectValidatorPlan(t, "independently selected root role")
}

// Exact outer file pins and locally valid signatures under another domain do
// not authenticate the complete service config under its selected approver.
func TestBootstrapChainRootRejectsInvalidConfigApproval(t *testing.T) {
	for _, item := range []string{"unsigned", "wrong-domain", "deployment", "root-plan", "service-config", "schema"} {
		t.Logf("root approval case: %s", item)
		f := newBootstrapChainFixture(t)
		approval := &f.rootRole.approval
		diagnostic := "complete service configuration"
		switch item {
		case "unsigned":
			approval.Signature = ""
			diagnostic = "approval signature"
		case "wrong-domain":
			unsigned := *approval
			unsigned.Signature = ""
			raw, err := json.Marshal(unsigned)
			if err != nil {
				t.Fatal(err)
			}
			approval.Signature = hex.EncodeToString(ed25519.Sign(f.rootRole.private, append([]byte(rootOfflineApprovalSchema+"\x00"), raw...)))
			diagnostic = "approval signature"
		case "deployment":
			approval.DeploymentId = "another-approved-deployment"
		case "root-plan":
			approval.RootPlanHash = rootObjectHash("another-approved-root-plan")
		case "service-config":
			approval.ServiceConfigHash = rootObjectHash("another-approved-service")
		case "schema":
			approval.Schema = rootOfflineApprovalSchema
		}
		if item != "unsigned" && item != "wrong-domain" {
			f.rootRole.sign(t)
		} else {
			f.rootRole.write(t)
		}
		f.rejectValidatorPlan(t, diagnostic)
	}
}

// The root approval is a new transitive source: every restart must reread it
// before any custody owner, even after successful local completion.
func TestBootstrapChainRootRestartRequiresOriginalApprovalSource(t *testing.T) {
	for _, item := range []string{"changed", "missing", "public", "symlink", "oversized"} {
		t.Logf("root approval source case: %s", item)
		f := newBootstrapChainFixture(t)
		first := f.result(t, "apply")
		before := f.journals(t)
		path := f.config.RootValidator.Approval.Path
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		switch item {
		case "changed":
			err = os.WriteFile(path, append(bytes.Clone(raw), '\n'), 0600)
		case "missing":
			err = os.Remove(path)
		case "public":
			err = os.Chmod(path, 0644)
		case "symlink":
			if err = os.Remove(path); err == nil {
				err = os.Symlink(f.root.config.RootService.Path, path)
			}
		case "oversized":
			err = os.WriteFile(path, bytes.Repeat([]byte{' '}, maximumBootstrapChainRootApprovalBytes+1), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "resume", &stdout, &stderr); code != 2 || stdout.Len() != 0 || !reflect.DeepEqual(before, f.journals(t)) {
			t.Fatal("unavailable original root approval entered retained custody", code, stderr.String())
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if again := f.result(t, "resume"); !reflect.DeepEqual(first, again) || !reflect.DeepEqual(before, f.journals(t)) {
			t.Fatal("restored exact root approval changed original custody")
		}
	}
}

// A newly signed allowance can produce a new review, but even accepting that
// review cannot replace or renew the service already owned by another plan.
func TestBootstrapChainRootRestartRefusesReapprovedService(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	before := f.journals(t)
	service := copyRootServiceConfig(f.root.plan.Service)
	service.MaximumObservations++
	f.rootRole.service(t, service)
	f.rootRole.approve(t)
	bootstrapRootTestWrite(t, f.path, f.config)
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 3 || !strings.Contains(stderr.String(), "accepted plan") || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("changed service reused the original accepted plan", code, stderr.String())
	}
	updated, err := loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil || updated.Plan.ContentHash == f.preparation.Plan.ContentHash {
		t.Fatal("new service approval did not create a different review", err)
	}
	f.preparation = updated
	stderr.Reset()
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 3 || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("new accepted root config adopted existing custody", code, stderr.String())
	}
}

// The new approval input participates in the existing namespace check, including
// nested UR custody paths. A service cannot later overwrite its own authority.
func TestBootstrapChainRootApprovalCannotOverlapCustody(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.rootRole.path = filepath.Join(f.validators[0].config.StateDir, "root-service-approval.json")
	if err := os.MkdirAll(filepath.Dir(f.rootRole.path), 0700); err != nil {
		t.Fatal(err)
	}
	f.rootRole.write(t)
	f.rejectValidatorPlan(t, "custody namespaces overlap")
	for _, path := range []string{f.root.config.RootService.Path, f.preparation.Root.Service.CustodyTrust.StatePath, f.preparation.Root.Service.CustodyTrust.StatePath + ".lock"} {
		preparation := f.preparation
		role := *preparation.Plan.Config.RootValidator
		role.Approval.Path = path
		preparation.Plan.Config.RootValidator = &role
		preparation.Plan.ContentHash = bootstrapChainPlanHash(preparation.Plan)
		if err := preparation.validate(); err == nil || !strings.Contains(err.Error(), "root approval overlaps") {
			t.Fatal("root approval aliases another bootstrap input or custody marker", err)
		}
	}
}

// Signature verification does not relax strict JSON grammar at the new file
// boundary. Duplicated fields cannot present different authority to reviewers.
func TestBootstrapChainRootApprovalRequiresStrictJson(t *testing.T) {
	for _, item := range []string{"duplicate", "unknown", "trailing", "null"} {
		t.Logf("root approval grammar case: %s", item)
		f := newBootstrapChainFixture(t)
		raw, err := json.Marshal(f.rootRole.approval)
		if err != nil {
			t.Fatal(err)
		}
		switch item {
		case "duplicate":
			raw = append([]byte(`{"deployment_id":"other",`), raw[1:]...)
		case "unknown":
			raw = append([]byte(`{"activation_ready":true,`), raw[1:]...)
		case "trailing":
			raw = append(raw, []byte(`{}`)...)
		case "null":
			raw = []byte("null")
		}
		if err := os.WriteFile(f.rootRole.path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		f.config.RootValidator.Approval.Sha256 = "sha256:" + hex.EncodeToString(digest[:])
		f.rejectValidatorPlan(t, "")
	}
}

// This independent pre-v3 shape fixes every canonical field and its order,
// rather than deriving expected legacy bytes from the new production struct.
type bootstrapChainLegacyConfigV2 struct {
	Schema          string                    `json:"schema"`
	DeploymentId    string                    `json:"deployment_id"`
	Netuid          uint16                    `json:"netuid"`
	Network         planNetwork               `json:"network"`
	RunDirectory    string                    `json:"run_directory"`
	OwnerTrimPolicy planFileReference         `json:"owner_trim_policy"`
	OwnerTrimPlan   planFileReference         `json:"owner_trim_plan"`
	Contracts       planFileReference         `json:"contracts"`
	Root            planFileReference         `json:"root"`
	Validators      []bootstrapChainValidator `json:"ur_validators"`
}

// Original v2 config inspections remain sealed under their historical domain.
type bootstrapChainLegacyPlanV2 struct {
	Schema               string                                    `json:"schema"`
	ConfigPath           string                                    `json:"config_path"`
	ConfigSha256         string                                    `json:"config_sha256"`
	Config               bootstrapChainLegacyConfigV2              `json:"config"`
	OwnerTrimContentHash string                                    `json:"owner_trim_content_hash"`
	OwnerTrimBlockers    []string                                  `json:"owner_trim_execution_blockers"`
	ContractPlanHash     string                                    `json:"contract_plan_hash"`
	RootPlanHash         string                                    `json:"root_plan_hash"`
	ValidatorInspections []validator.ProductionBootstrapInspection `json:"ur_validator_config_inspections,omitempty"`
	PendingChainPhases   []string                                  `json:"pending_chain_phases"`
	NetworkEffects       bool                                      `json:"network_effects"`
	NativeSigning        bool                                      `json:"native_signing"`
	ActivationReady      bool                                      `json:"activation_ready"`
	ContentHash          string                                    `json:"content_hash"`
}

// Existing v2 journals keep exact original seals and result scope; the new
// command can neither create more v2 custody nor adopt it as v3 authority.
func TestBootstrapChainV2RestartPreservesOriginalWireAndScope(t *testing.T) {
	f := newBootstrapChainFixture(t)
	v3 := f.config
	f.config.Schema, f.config.RootValidator = bootstrapChainConfigSchemaV2, nil
	bootstrapRootTestWrite(t, f.path, f.config)
	var err error
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	c, p := f.config, f.preparation.Plan
	legacy := bootstrapChainLegacyPlanV2{Schema: "urnetwork-mainnet-bootstrap-chain-preparation-v2", ConfigPath: p.ConfigPath, ConfigSha256: p.ConfigSha256,
		Config: bootstrapChainLegacyConfigV2{Schema: "urnetwork-mainnet-bootstrap-chain-config-v2", DeploymentId: c.DeploymentId, Netuid: c.Netuid, Network: c.Network,
			RunDirectory: c.RunDirectory, OwnerTrimPolicy: c.OwnerTrimPolicy, OwnerTrimPlan: c.OwnerTrimPlan, Contracts: c.Contracts, Root: c.Root, Validators: c.Validators},
		OwnerTrimContentHash: p.OwnerTrimContentHash, OwnerTrimBlockers: p.OwnerTrimBlockers, ContractPlanHash: p.ContractPlanHash, RootPlanHash: p.RootPlanHash,
		ValidatorInspections: p.ValidatorInspections, PendingChainPhases: p.PendingChainPhases}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte("urnetwork-mainnet-bootstrap-chain-preparation-v2\x00"), raw...))
	legacy.ContentHash = "sha256:" + hex.EncodeToString(digest[:])
	raw, err = json.Marshal(legacy)
	actual, actualErr := json.Marshal(p)
	if err != nil || actualErr != nil || legacy.ContentHash != p.ContentHash || !bytes.Equal(raw, actual) || p.RootInspection != nil {
		t.Fatal("v2 preparation bytes or hash changed", err, actualErr)
	}
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "apply", io.Discard, &stderr); code != 3 || len(f.journals(t)) != 0 {
		t.Fatal("new v2 apply bypassed root config admission", code, stderr.String())
	}
	store, err := openBootstrapChainStore(f.preparation, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, advanceErr := advanceBootstrapChain(t.Context(), store, nil)
	if err := errors.Join(advanceErr, store.close()); err != nil {
		t.Fatal(err)
	}
	before := f.journals(t)
	got := f.result(t, "resume")
	if got.Schema != "urnetwork-mainnet-bootstrap-chain-result-v2" || !got.UrValidatorConfigsVerified ||
		got.UrValidatorsStatus != "two-signed-production-configs-verified-live-admission-pending" || got.RootValidatorConfigVerified || got.RootValidatorStatus != "" ||
		!reflect.DeepEqual(first, got) || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("v2 restart changed scope, original bytes or admission status")
	}
	resultRaw, err := json.Marshal(got)
	if err != nil || bytes.Contains(resultRaw, []byte("root_validator")) {
		t.Fatal("v2 result acquired new serialized root authority", err)
	}
	f.config = v3
	bootstrapRootTestWrite(t, f.path, f.config)
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 3 || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("v3 adopted original v2 custody", code, stderr.String())
	}
}

// Version tags cannot be mixed to grant old journals new authority fields.
func TestBootstrapChainLegacyCannotClaimRootInspection(t *testing.T) {
	for _, schema := range []string{bootstrapChainConfigSchemaV1, bootstrapChainConfigSchemaV2} {
		t.Logf("legacy schema case: %s", schema)
		f := newBootstrapChainFixture(t)
		f.config.Schema = schema
		f.rejectValidatorPlan(t, "v1/v2 cannot acquire root config inspection authority")
	}
}
