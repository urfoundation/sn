// Public commands must distinguish signed offline config admission from live
// eligibility, and retain exact original custody across every rejected restart.
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

// A refused read-only plan emits no partially usable projection or journal.
func (self *bootstrapChainFixture) rejectValidatorPlan(t *testing.T, diagnostic string) {
	t.Helper()
	bootstrapRootTestWrite(t, self.path, self.config)
	before := self.journals(t)
	var stdout, stderr bytes.Buffer
	if code := self.command(t.Context(), "plan", &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), diagnostic) {
		t.Fatalf("validator plan refusal: exit %d stdout %s stderr %s; want %q", code, stdout.String(), stderr.String(), diagnostic)
	}
	if !reflect.DeepEqual(before, self.journals(t)) || len(self.contracts.counts) != 0 {
		t.Fatal("refused config inspection changed custody or contacted a chain")
	}
}

// The accepted plan retains both real approvals and exact config hashes. Its
// success leaves live permit/stake, operator evidence and service gates open.
func TestBootstrapChainValidatorProductionConfigsBoundOffline(t *testing.T) {
	f := newBootstrapChainFixture(t)
	if len(f.preparation.Plan.ValidatorInspections) != 2 {
		t.Fatal("signed production inspections absent")
	}
	for i, inspection := range f.preparation.Plan.ValidatorInspections {
		if inspection.ApprovalReference != f.validators[i].config.OwnerRecycleApproval.Approval || !reflect.DeepEqual(inspection.Approval, f.validators[i].approval) ||
			inspection.ValidatorId != f.config.Validators[i].ValidatorId || inspection.ApprovalSigner != f.config.Validators[i].ApprovalPublicKey {
			t.Fatal("accepted plan lost exact signed identity", i)
		}
		for _, path := range inspection.DeclaredPaths {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("offline plan touched producer state or evidence", path, err)
			}
		}
	}
	first := f.result(t, "apply")
	before := f.journals(t)
	if !first.UrValidatorConfigsVerified || first.NetworkEffects || first.ActivationReady || !strings.Contains(first.UrValidatorsStatus, "live-admission-pending") ||
		!reflect.DeepEqual(first, f.result(t, "resume")) || !reflect.DeepEqual(before, f.journals(t)) || len(f.contracts.counts) != 0 {
		t.Fatal("offline inspection changed custody, readiness or restart authority")
	}
}

// This is the original gap: an exact hash over arbitrary config bytes was
// sufficient for local preparation. V2 now requires real producer admission.
func TestBootstrapChainValidatorRejectsArbitraryPinnedConfig(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.config.Validators[0].Config = bootstrapRootTestWrite(t, f.config.Validators[0].Config.Path, map[string]any{"schema_version": 3, "validator_id": 1, "production_admission": "pending"})
	bootstrapRootTestWrite(t, f.path, f.config)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "plan", &stdout, &stderr); code != 2 || stdout.Len() != 0 || len(f.journals(t)) != 0 {
		t.Fatalf("arbitrary pinned UR config bypassed production admission: exit %d stdout %s stderr %s", code, stdout.String(), stderr.String())
	}
}

// Config ID is covered by a valid independently pinned signature; a different
// approved producer still cannot substitute for the selected bootstrap role.
func TestBootstrapChainValidatorRejectsSignedForeignIdentity(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.validators[0].config.ValidatorID += 10
	f.config.Validators[0].Config = f.validators[0].publish(t)
	f.rejectValidatorPlan(t, "signed identity, network, deployment or independent signer differs")
}

// A valid signature under the observation/draft domain cannot be promoted to
// the production domain merely by keeping a schema-3 envelope and config.
func TestBootstrapChainValidatorRejectsWrongSignatureDomain(t *testing.T) {
	f := newBootstrapChainFixture(t)
	v := f.validators[0]
	raw, err := json.Marshal(v.approval)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte("urnetwork-owner-recycle-approval-signature-v1\n"), raw...))
	v.writeApproval(t, validator.OwnerRecycleApprovalEnvelope{Approval: v.approval, Signature: hex.EncodeToString(ed25519.Sign(v.private, digest[:]))})
	f.config.Validators[0].Config = v.writeConfig(t)
	f.rejectValidatorPlan(t, "approval signature differs")
}

// An exact new envelope hash cannot make an unsigned approval authoritative.
func TestBootstrapChainValidatorRejectsUnsignedApproval(t *testing.T) {
	f := newBootstrapChainFixture(t)
	v := f.validators[0]
	v.writeApproval(t, validator.OwnerRecycleApprovalEnvelope{Approval: v.approval})
	f.config.Validators[0].Config = v.writeConfig(t)
	f.rejectValidatorPlan(t, "approval signature differs")
}

// Bootstrap trust anchors and role intent are independent inputs; a config's
// self-selected signer or claimed majority label cannot supply them implicitly.
func TestBootstrapChainValidatorRejectsMissingOrMismatchedRoleInputs(t *testing.T) {
	for _, item := range []struct {
		mutate     func(*bootstrapChainValidator)
		diagnostic string
	}{
		{mutate: func(role *bootstrapChainValidator) { role.Role = "" }, diagnostic: "ordered majority"},
		{mutate: func(role *bootstrapChainValidator) { role.Role = "secondary" }, diagnostic: "ordered majority"},
		{mutate: func(role *bootstrapChainValidator) { role.Implementation = "alternate-validator" }, diagnostic: "ordered majority"},
		{mutate: func(role *bootstrapChainValidator) { role.ApprovalPublicKey = "" }, diagnostic: "ordered majority"},
		{mutate: func(role *bootstrapChainValidator) { role.ApprovalPublicKey = "0x" + strings.Repeat("ef", 32) }, diagnostic: "independent signer differs"},
		{mutate: func(role *bootstrapChainValidator) { role.RegistrationBlock = new(uint64(*role.RegistrationBlock + 1)) }, diagnostic: "generation is not explicitly protected"},
		{mutate: func(role *bootstrapChainValidator) { role.Coldkey = "0x" + strings.Repeat("ee", 32) }, diagnostic: "generation is not explicitly protected"},
	} {
		f := newBootstrapChainFixture(t)
		item.mutate(&f.config.Validators[0])
		f.rejectValidatorPlan(t, item.diagnostic)
	}
}

// Re-signed foreign config facts remain inadmissible even when each producer's
// local config and approval agree perfectly. Source/runtime pins come from the
// independent common bootstrap scope, not from the config being inspected.
func TestBootstrapChainValidatorRejectsSignedScopeMismatch(t *testing.T) {
	for _, item := range []struct {
		mutate     func(*bootstrapChainValidatorFixture)
		diagnostic string
	}{
		{mutate: func(v *bootstrapChainValidatorFixture) { v.config.DeploymentID = "synthetic-foreign-deployment" }, diagnostic: "signed identity"},
		{mutate: func(v *bootstrapChainValidatorFixture) { v.approval.NativeChain = "synthetic-foreign-chain" }, diagnostic: "signed identity"},
		{mutate: func(v *bootstrapChainValidatorFixture) {
			v.approval.ValidatorHotkey = v.approval.Production.ValidatorHotkeys[1]
		}, diagnostic: "signed identity"},
		{mutate: func(v *bootstrapChainValidatorFixture) {
			v.config.RuntimeSpec++
			v.approval.Proposal.Runtime.Version.SpecVersion++
		}, diagnostic: "runtime/source or subnet owner differs"},
		{mutate: func(v *bootstrapChainValidatorFixture) {
			v.approval.Proposal.Runtime.SourceCommit = strings.Repeat("e", 40)
		}, diagnostic: "exact reviewed source profile"},
		{mutate: func(v *bootstrapChainValidatorFixture) { v.approval.SubnetOwner = [32]byte{0xe1} }, diagnostic: "runtime/source or subnet owner differs"},
		{mutate: func(v *bootstrapChainValidatorFixture) { v.approval.Production.ValidatorHotkeys[1] = [32]byte{0xf1} }, diagnostic: "census omits a protected UR role"},
		{mutate: func(v *bootstrapChainValidatorFixture) { v.approval.ValidThroughNativeBlock++ }, diagnostic: "disagree on the initial production scope"},
		{mutate: func(v *bootstrapChainValidatorFixture) { v.config.Coordinator = "0x" + strings.Repeat("78", 20) }, diagnostic: "contract declarations"},
	} {
		f := newBootstrapChainFixture(t)
		item.mutate(f.validators[0])
		f.config.Validators[0].Config = f.validators[0].publish(t)
		f.rejectValidatorPlan(t, item.diagnostic)
	}
}

// The old config pin remains valid while its transitive approval changes.
// Restart must reload those bytes before touching any retained child owner.
func TestBootstrapChainValidatorRestartRefusesStaleApproval(t *testing.T) {
	f := newBootstrapChainFixture(t)
	first := f.result(t, "apply")
	before := f.journals(t)
	path := f.validators[0].config.OwnerRecycleApproval.Approval.Path
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Clone(raw)
	changed[len(changed)-3] ^= 1
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "configured identity") || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("stale transitive approval entered custody", code, stderr.String())
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if again := f.result(t, "resume"); !reflect.DeepEqual(first, again) || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("restored exact approval changed existing custody")
	}
}

// Reapproval can create a new review but cannot renew or replace original
// completed custody, even when the caller accepts the new plan hash explicitly.
func TestBootstrapChainValidatorRestartRefusesReapprovedConfig(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	before := f.journals(t)
	f.validators[0].config.PollSeconds++
	f.config.Validators[0].Config = f.validators[0].publish(t)
	bootstrapRootTestWrite(t, f.path, f.config)
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 3 || !strings.Contains(stderr.String(), "accepted plan") {
		t.Fatal("changed config reused original accepted plan", code, stderr.String())
	}
	updated, err := loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil || updated.Plan.ContentHash == f.preparation.Plan.ContentHash {
		t.Fatal("new approval did not produce its distinct review", err)
	}
	f.preparation = updated
	stderr.Reset()
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 3 || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("newly accepted config adopted original custody", code, stderr.String())
	}
}

// Separately valid production configs may still contend for the same host
// state. The composition catches cross-role and child-journal ownership gaps.
func TestBootstrapChainValidatorRejectsCustodyNamespaceOverlap(t *testing.T) {
	for _, shared := range []bool{false, true} {
		f := newBootstrapChainFixture(t)
		if shared {
			f.validators[0].config.StateDir = f.validators[1].config.StateDir
		} else {
			f.validators[0].config.StateDir = f.config.RunDirectory
		}
		f.config.Validators[0].Config = f.validators[0].publish(t)
		f.rejectValidatorPlan(t, "custody namespaces overlap")
	}
}

// A lexically intervening sibling must not hide an ancestor custody claim.
// This controls the sorted bounded comparison independently of file I/O.
func TestBootstrapChainValidatorNamespaceSiblingCannotHideAncestor(t *testing.T) {
	inspections := []validator.ProductionBootstrapInspection{
		{ApprovalReference: validator.ReleaseEvidenceV2File{Path: "/synthetic/first-approval"}, DeclaredPaths: []string{"/synthetic/a", "/synthetic/a-other"}},
		{ApprovalReference: validator.ReleaseEvidenceV2File{Path: "/synthetic/second-approval"}, DeclaredPaths: []string{"/synthetic/a/child"}},
	}
	if err := validateBootstrapChainValidatorPaths(inspections, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "custody namespaces overlap") {
		t.Fatal("intervening sibling hid an ancestor owned by another role", err)
	}
	inspections[1].DeclaredPaths = []string{"/synthetic/a-sibling"}
	if err := validateBootstrapChainValidatorPaths(inspections, map[string]bool{}); err != nil {
		t.Fatal("distinct sibling was mistaken for a child", err)
	}
}

// Exact v1 canonical bytes remain resumable with their original limited status.
// A v3 review cannot relabel or acquire that already claimed child custody.
func TestBootstrapChainV1RestartPreservesOriginalScope(t *testing.T) {
	f := newBootstrapChainFixture(t)
	v3 := f.config
	v3.Validators = append([]bootstrapChainValidator(nil), f.config.Validators...)
	f.config.Schema = bootstrapChainConfigSchemaV1
	f.config.RootValidator = nil
	for i := range f.config.Validators {
		role := &f.config.Validators[i]
		role.Role, role.Implementation, role.ApprovalPublicKey = "", "", ""
		role.Config = bootstrapRootTestWrite(t, role.Config.Path, map[string]any{"schema_version": 3, "validator_id": i + 1, "production_admission": "pending"})
	}
	bootstrapRootTestWrite(t, f.path, f.config)
	var err error
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	// This independent old wire shape catches accidental non-omitted v2 fields.
	legacyRaw, err := json.Marshal(f.preparation.Plan)
	if err != nil || bytes.Contains(legacyRaw, []byte("ur_validator_config_inspections")) || bytes.Contains(legacyRaw, []byte("approval_public_key_ed25519")) || bytes.Contains(legacyRaw, []byte("implementation")) || bytes.Contains(legacyRaw, []byte("root_validator")) {
		t.Fatal("v1 plan acquired new serialized authority fields", err)
	}
	unsigned := bytes.Replace(legacyRaw, []byte(f.preparation.Plan.ContentHash), nil, 1)
	digest := sha256.Sum256(append([]byte("urnetwork-mainnet-bootstrap-chain-preparation-v1\x00"), unsigned...))
	if f.preparation.Plan.ContentHash != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal("v1 preparation hash changed its original domain or canonical bytes")
	}
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "apply", io.Discard, &stderr); code != 3 || len(f.journals(t)) != 0 {
		t.Fatal("new v1 apply bypassed stronger preparation", code, stderr.String())
	}
	// Recreate the same pre-upgrade durable record through its unchanged owner.
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
	if got.Schema != "urnetwork-mainnet-bootstrap-chain-result-v1" || got.UrValidatorConfigsVerified || got.UrValidatorsStatus != "two-protected-role-inputs-pinned-production-admission-pending" ||
		!reflect.DeepEqual(first, got) || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("v1 restart changed scope, bytes or admission status")
	}
	f.config = v3
	for i, v := range f.validators {
		f.config.Validators[i].Config = v.publish(t)
	}
	bootstrapRootTestWrite(t, f.path, f.config)
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 3 || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("v3 adopted original v1 custody", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(f.config.RunDirectory, bootstrapChainStateFile)); err != nil {
		t.Fatal("v1 custody disappeared", err)
	}
}
