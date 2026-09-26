//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	gojwt "github.com/golang-jwt/jwt/v5"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

func TestPolicyRolloverGenerationV2RolesSeparateOnlyValidatorClients(t *testing.T) {
	t.Parallel()
	f := newRuntimeEvidenceProvisionV2TestFixture(t)
	before := cloneRoleSecrets(f.roles)
	first, err := policyRolloverGenerationRolesV2(f.cfg, f.roles, 1)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := policyRolloverGenerationRolesV2(f.cfg, f.roles, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := policyRolloverGenerationRolesV2(f.cfg, f.roles, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, retry) || !reflect.DeepEqual(before, f.roles) || !reflect.DeepEqual(first.EVM, before.EVM) || !reflect.DeepEqual(first.Substrate, before.Substrate) {
		t.Fatal("generation derivation changed retained roles or is not deterministic")
	}
	for label, original := range before.Clients {
		got := first.Clients[label]
		if strings.HasPrefix(label, "validator-") {
			if got.PublicKeyHex == original.PublicKeyHex || got.PublicKeyHex == second.Clients[label].PublicKeyHex || got.ClientIDHex != "" || got.Label != label {
				t.Fatal("generation reused a VPK or invented a client ID")
			}
		} else if got != original {
			t.Fatal("generation changed an unrelated client")
		}
	}
	if _, err := policyRolloverGenerationRolesV2(f.cfg, f.roles, 0); err == nil {
		t.Fatal("zero generation accepted")
	}
}

type policyRolloverGenerationTestV2 struct {
	fixture    *runtimeEvidenceProvisionV2TestFixture
	roles      *RoleSecrets
	members    []runtimeEvidenceActivationMemberV2
	generation uint64
	original   map[string][]byte
}

func newPolicyRolloverGenerationTestV2(t *testing.T, configure ...func(*ResolvedConfig)) *policyRolloverGenerationTestV2 {
	t.Helper()
	f := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, func(cfg *ResolvedConfig) {
		for _, apply := range configure {
			if apply != nil {
				apply(cfg)
			}
		}
	})
	renderFinalValidatorFixtureTest(t, f)
	public, err := json.Marshal(f.roles.Public())
	if err != nil {
		t.Fatal(err)
	}
	publicPath := filepath.Join(f.stateDir, "public", "identities.json")
	if err := os.WriteFile(publicPath, public, 0o600); err != nil {
		t.Fatal(err)
	}
	original := map[string][]byte{publicPath: public}
	for validatorID := 1; validatorID <= f.cfg.Config.Topology.Validators; validatorID++ {
		for noID := 1; noID <= f.cfg.Config.Topology.Operators; noID++ {
			root := filepath.Join(f.stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "operators", fmt.Sprintf("no-%d", noID))
			for _, name := range []string{"network.jwt", "attempt-ledger.records"} {
				path := filepath.Join(root, name)
				data := []byte(fmt.Sprintf("retained-%s-%d-%d\n", name, validatorID, noID))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				original[path] = data
			}
			for _, name := range []string{"client.key"} {
				path := filepath.Join(root, name)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				original[path] = data
			}
		}
		path := filepath.Join(f.stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "validator.yml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		original[path] = data
	}
	roles, err := policyRolloverGenerationRolesV2(f.cfg, f.roles, 7)
	if err != nil {
		t.Fatal(err)
	}
	// The source config and retained activation stay under their old policy;
	// generation staging must use the separately selected current policy.
	currentPolicy := *f.cfg.Policy
	currentPolicy.Deposit.TotalTestCampaignCapRao++
	currentPolicyHash, err := currentPolicy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Policy, f.cfg.PolicyHash = &currentPolicy, fmt.Sprintf("0x%x", currentPolicyHash)
	members := slices.Clone(f.prepared.Members)
	for index := range members {
		member := &members[index]
		hotkey, key, err := runtimeEvidenceActivationKeysV2(roles, member.ValidatorId, member.NoId)
		if err != nil {
			t.Fatal(err)
		}
		member.Activation.VPK = [32]byte(key[32:])
		member.Activation.Domain.Epoch++
		member.Activation.Domain.PolicyHash = currentPolicyHash
		member.VpkSignature, err = member.Activation.SignVPK(key)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := member.Activation.Digest()
		if err != nil {
			t.Fatal(err)
		}
		member.HotkeySignature, err = hotkey.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
	}
	return &policyRolloverGenerationTestV2{fixture: f, roles: roles, members: members, generation: 7, original: original}
}

func policyRolloverGenerationClientProvisionTestV2(t *testing.T) func(context.Context, string, string, string, string) (connect.Id, error) {
	t.Helper()
	return func(ctx context.Context, apiURL, network, target, description string) (connect.Id, error) {
		idHash := sha256.Sum256([]byte(target))
		id := connect.Id(idHash[:16])
		token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"client_id": id.String(), "device_id": id.String()}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			return connect.Id{}, err
		}
		_, err = validatorcomponent.WriteReleaseEvidenceV2File(ctx, target, []byte(token), 4096)
		return id, err
	}
}

func (g *policyRolloverGenerationTestV2) provision(t *testing.T) {
	t.Helper()
	f := g.fixture
	if err := provisionPolicyRolloverGenerationClientsWithV2(t.Context(), f.cfg, f.stateDir, g.generation, g.roles, policyRolloverGenerationClientProvisionTestV2(t)); err != nil {
		t.Fatal(err)
	}
}

func (g *policyRolloverGenerationTestV2) stage(t *testing.T) ([]policyRolloverValidatorHandoffV2, error) {
	t.Helper()
	f := g.fixture
	return stagePolicyRolloverGenerationV2(t.Context(), f.cfg, f.plan, f.stateDir, g.generation, g.roles, g.members, f.completed.Boundary)
}

func TestPolicyRolloverGenerationV2StagesCurrentPolicyWithoutTouchingPriorAuthority(t *testing.T) {
	t.Parallel()
	g := newPolicyRolloverGenerationTestV2(t)
	g.provision(t)
	handoffs, err := g.stage(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(handoffs) != 2 {
		t.Fatal("generation omitted a validator")
	}
	for _, handoff := range handoffs {
		cfg, err := validatorcomponent.LoadReleaseConfig(handoff.Config.Path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.StateDir != handoff.StateDir || cfg.PolicyHash != g.fixture.cfg.PolicyHash || cfg.StateDir == handoff.PreviousStateDir || cfg.Operators[0].StateDir != filepath.Join(handoff.ClientStateDir, "operators", "no-1") {
			t.Fatal("generation config selects the wrong authority")
		}
		for _, operator := range handoff.Evidence.Operators {
			wire, err := validatorcomponent.ReadReleaseEvidenceV2File(t.Context(), operator.Context, handoff.Evidence.Bounds.Cut.MaxHeaderBytes)
			if err != nil {
				t.Fatal(err)
			}
			var authority validatorcomponent.ReleaseEvidenceV2ActivationContext
			if err := json.Unmarshal(wire, &authority); err != nil {
				t.Fatal(err)
			}
			if authority.Activation.FirstSequence != 1 || authority.Activation.PriorRoot != ([32]byte{}) || authority.InitialCut.FirstSequence != 1 || authority.InitialCut.EgressFirstSequence != 1 || authority.InitialCut.EgressGeneration != 1 || authority.InitialCut.PriorRoot != "0x"+strings.Repeat("0", 64) {
				t.Fatal("generation invented ledger continuity")
			}
			member := g.members[int(handoff.ValidatorID-1)*2+int(operator.NoID-1)]
			if authority.Activation != member.Activation || authority.ValidatorUID != member.ValidatorUid {
				t.Fatal("generation changed its signed activation")
			}
			historyWire, err := validatorcomponent.ReadReleaseEvidenceV2File(t.Context(), operator.History, handoff.Evidence.Bounds.MaxHistoryBytes)
			if err != nil {
				t.Fatal(err)
			}
			var history validatorcomponent.ReleaseEvidenceV2ActivationHistory
			if err := json.Unmarshal(historyWire, &history); err != nil || history.LegacyClosures == nil || len(history.LegacyClosures) != 0 {
				t.Fatal("generation claims a prior history")
			}
		}
		publicBytes, err := validatorcomponent.ReadReleaseEvidenceV2File(t.Context(), handoff.Identities, handoff.Evidence.Bounds.MaxControlBytes)
		if err != nil {
			t.Fatal(err)
		}
		var identities finalPublicIdentities
		if err := json.Unmarshal(publicBytes, &identities); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(identities, g.roles.Public()) {
			t.Fatal("generation public identity does not match provisioned clients")
		}
	}
	for path, want := range g.original {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("retained source changed at %s: %v", path, err)
		}
	}
	// The same immutable static inputs can be retried after the new generation
	// has acquired dynamic authority. The stage must not reset that authority.
	dynamic := filepath.Join(handoffs[0].ClientStateDir, "operators", "no-1", "attempt-ledger.records")
	if err := os.WriteFile(dynamic, []byte("new signed ledger"), 0o600); err != nil {
		t.Fatal(err)
	}
	retry, err := g.stage(t)
	if err != nil || !reflect.DeepEqual(handoffs, retry) {
		t.Fatalf("exact retry failed: %v", err)
	}
	if got, err := os.ReadFile(dynamic); err != nil || string(got) != "new signed ledger" {
		t.Fatal("retry reset generation state")
	}
}

func TestPolicyRolloverGenerationV2RejectsChangedAuthorityBeforeStaging(t *testing.T) {
	for _, mode := range []string{"old-key", "old-policy", "partial-census", "different-uid", "unprovisioned-client", "wrong-generation", "preexisting-state", "changed-file"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			g := newPolicyRolloverGenerationTestV2(t)
			g.provision(t)
			f := g.fixture
			switch mode {
			case "old-key":
				g.members[0] = f.prepared.Members[0]
			case "old-policy":
				g.members[0].Activation.Domain.PolicyHash[0] ^= 1
			case "partial-census":
				g.members = g.members[:3]
			case "different-uid":
				g.members[1].ValidatorUid++
			case "unprovisioned-client":
				role := g.roles.Clients["validator-1-no-1"]
				role.ClientIDHex = ""
				g.roles.Clients["validator-1-no-1"] = role
			case "wrong-generation":
				g.generation++
			case "preexisting-state":
				path := filepath.Join(policyRolloverGenerationOperatorDirV2(f.stateDir, 1, 1, g.generation), "attempt-ledger.records")
				if err := os.WriteFile(path, []byte("unexplained authority"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "changed-file":
				path := filepath.Join(policyRolloverGenerationRootV2(f.stateDir, 2, g.generation), "evidence-v2", "no-2", "context.json")
				if _, err := validatorcomponent.WriteReleaseEvidenceV2File(t.Context(), path, []byte("wrong context"), 4096); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := g.stage(t); err == nil {
				t.Fatal("changed authority was accepted")
			}
			for validatorID := uint64(1); validatorID <= 2; validatorID++ {
				_, err := os.Stat(filepath.Join(policyRolloverGenerationRootV2(f.stateDir, validatorID, g.generation), "validator.yml"))
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed full census preflight partially staged a config")
				}
			}
		})
	}
}

func TestPolicyRolloverGenerationV2ClientProvisioningRecoversDurableIDs(t *testing.T) {
	t.Parallel()
	g := newPolicyRolloverGenerationTestV2(t)
	f := g.fixture
	provision := policyRolloverGenerationClientProvisionTestV2(t)
	calls := 0
	err := provisionPolicyRolloverGenerationClientsWithV2(t.Context(), f.cfg, f.stateDir, g.generation, g.roles, func(ctx context.Context, a, b, c, d string) (connect.Id, error) {
		calls++
		if calls == 3 {
			return connect.Id{}, errors.New("interrupted")
		}
		return provision(ctx, a, b, c, d)
	})
	if err == nil {
		t.Fatal("provisioning interruption lost")
	}
	firstID := g.roles.Clients["validator-1-no-1"].ClientIDHex
	g.roles, err = policyRolloverGenerationRolesV2(f.cfg, f.roles, g.generation)
	if err != nil {
		t.Fatal(err)
	}
	g.provision(t)
	if got := g.roles.Clients["validator-1-no-1"].ClientIDHex; got != firstID {
		t.Fatal("retry replaced a durable API client identity")
	}
	for label, role := range g.roles.Clients {
		if strings.HasPrefix(label, "validator-") {
			id, err := hex.DecodeString(role.ClientIDHex)
			if err != nil || len(id) != 16 {
				t.Fatal("generation client identity incomplete")
			}
		}
	}
	if _, err := g.stage(t); err != nil {
		t.Fatal(err)
	}
}
