//go:build linux || darwin

// On a durable mainnet deployment `validator run` opens each operator's attempt
// ledger only from storage-prepare's offline output, so the first activation
// meets that prepared root. Activation admits exactly an empty ledger prepared
// for its own member identity, and the runtime then opens that same ledger.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablesys"
	"golang.org/x/sys/unix"
)

// One mainnet operator whose credentials live outside its prepared ledger
// root, and its real signed activation member.
type releaseActivationLedgerFixture struct {
	cfg       *ReleaseConfig
	root      string
	clientKey ed25519.PrivateKey
	member    ReleaseActivationMemberV2
}

func newReleaseActivationLedgerFixture(t *testing.T) *releaseActivationLedgerFixture {
	t.Helper()
	root, credentials := identityTestStateDir(t), identityTestStateDir(t)
	cfg := &ReleaseConfig{DeploymentID: "sn25-mainnet-activation-ledger", ChainID: 964, GenesisHash: "0x" + strings.Repeat("2F", 32), Netuid: 25, ValidatorID: 1,
		Coordinator: "0xC18925925E2B7BB9059B7D696B8C92762AE86406", SettlementVault: "0x98BF47BA01828676855B5ED10F2F22A867F0CB59",
		Operators: []OperatorConfig{{NoID: 1, StateDir: filepath.Join(root, "no-1"), ClientKeySeedFile: filepath.Join(credentials, "client.key"),
			NetworkJWTFile: filepath.Join(credentials, "network.jwt"), ClientJWTFile: filepath.Join(credentials, "client.jwt")}}}
	cfg.EvidenceV2 = releaseEvidenceV2TestConfig(root, cfg.Operators)
	hotkey, err := crv4.KeypairFromSeed([32]byte{4})
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := parseHash32("synthetic genesis", cfg.GenesisHash)
	if err != nil {
		t.Fatal(err)
	}
	deployment := ReleaseActivationDeploymentV2{DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: genesis, Netuid: cfg.Netuid,
		Coordinator: [20]byte(common.HexToAddress(cfg.Coordinator)), SettlementVault: [20]byte(common.HexToAddress(cfg.SettlementVault)), PolicyHash: [32]byte{0x33}}
	clientKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x61}, ed25519.SeedSize))
	activation, err := BuildFreshReleaseActivationV2(deployment, ReleaseActivationSnapshotV2{Epoch: 9, NativeBlock: 120, NativeHash: [32]byte{5}, EVMBlock: 200, EVMHash: [32]byte{6}},
		hotkey.PublicKey(), 1, [32]byte(clientKey[ed25519.SeedSize:]))
	if err != nil {
		t.Fatal(err)
	}
	vpkSignature, hotkeySignature, err := SignReleaseActivationV2(activation, hotkey, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	return &releaseActivationLedgerFixture{cfg: cfg, root: root, clientKey: clientKey,
		member: ReleaseActivationMemberV2{NoID: 1, ValidatorUID: 139, Activation: activation, VPKSignature: vpkSignature, HotkeySignature: hotkeySignature}}
}

// The exact preparation scope storage-prepare is given for one identity.
func (self *releaseActivationLedgerFixture) scope(identity AttemptLedgerIdentity, coordinator string) AttemptLedgerPreparationScope {
	return AttemptLedgerPreparationScope{Identity: identity, Coordinator: coordinator, Limits: self.cfg.EvidenceV2.Bounds.Disk, ExpectedHead: AttemptLedgerHead{Root: zeroAttemptHash()}}
}

// Stages one fresh root under the fixture root as storage-prepare does and,
// when publish is set, publishes its custody checkpoint as apply does.
func (self *releaseActivationLedgerFixture) prepare(t *testing.T, name string, scope AttemptLedgerPreparationScope, publish bool) string {
	t.Helper()
	parent, err := os.Open(self.root)
	if err != nil {
		t.Fatal(err)
	}
	census, buildErr := BuildFreshAttemptLedgerPreparation(t.Context(), parent, name, scope)
	if err := errors.Join(buildErr, parent.Close()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.root, name)
	if !publish {
		return path
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, checkpointErr := BuildAttemptLedgerPreparationCheckpoint(t.Context(), file, scope, census)
	if checkpointErr == nil {
		checkpointErr = durablesys.SetAttribute(int(file.Fd()), attemptLedgerCustodyAttribute, checkpoint, unix.XATTR_CREATE)
	}
	if err := errors.Join(checkpointErr, file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	return path
}

// The same activation scope with its one operator rooted at path.
func (self *releaseActivationLedgerFixture) at(path string) *ReleaseConfig {
	cfg := *self.cfg
	cfg.Operators = []OperatorConfig{cfg.Operators[0]}
	cfg.Operators[0].StateDir = path
	return &cfg
}

// The storage-prepared empty ledger for exactly this member is admitted; the
// identity rendered into the member's context is that ledger's identity; and
// the runtime's durable ledger constructor opens it unchanged.
func TestFreshActivationAdmitsItsStoragePreparedEmptyLedger(t *testing.T) {
	f := newReleaseActivationLedgerFixture(t)
	members := []ReleaseActivationMemberV2{f.member}
	if admitted, err := requireFreshReleaseOperatorState(t.Context(), f.cfg, members); err != nil || len(admitted) != 0 {
		t.Fatal("an absent operator root was not fresh", admitted, err)
	}
	identity := releaseActivationLedgerIdentity(f.cfg, f.member)
	path := f.prepare(t, "no-1", f.scope(identity, strings.ToLower(f.cfg.Coordinator)), true)
	// Credential files may sit beside the ledger, as before.
	if err := os.WriteFile(filepath.Join(path, filepath.Base(f.cfg.Operators[0].ClientKeySeedFile)), bytes.Repeat([]byte{1}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	// Activation runs with the declared durable root, as on the host.
	storage := durablefixture.New(t, t.Context(), path)
	admitted, err := requireFreshReleaseOperatorState(storage.Context, f.cfg, members)
	if err != nil || !reflect.DeepEqual(admitted, []uint64{1}) {
		t.Fatal("the member's own storage-prepared empty ledger was refused", admitted, err)
	}
	paths := DefaultReleaseEvidenceV2OperatorPaths(identityTestStateDir(t), 1)
	entry, inputs, err := RenderReleaseActivationInputsV2(releaseActivationLedger(f.cfg), f.member, [20]byte{0x33}, [32]byte{8}, ReleaseActivationBoundaryV2{Block: 260, Hash: [32]byte{7}}, f.cfg.EvidenceV2.Bounds, paths)
	if err != nil {
		t.Fatal(err)
	}
	var renderedContext ReleaseEvidenceV2ActivationContext
	if err := json.Unmarshal(inputs[entry.Context.Path], &renderedContext); err != nil || renderedContext.InitialCut.Identity != identity {
		t.Fatal("the rendered context opens another ledger identity than activation admitted", renderedContext.InitialCut.Identity, err)
	}
	ledger, err := NewDiskAttemptLedger(storage.Context, path, renderedContext.InitialCut.Identity, strings.ToLower(f.cfg.Coordinator), f.clientKey, f.cfg.EvidenceV2.Bounds.Disk)
	if err != nil {
		t.Fatal("the runtime refused the admitted prepared ledger", err)
	}
	head, headErr := ledger.Head()
	if err := errors.Join(headErr, ledger.Close()); err != nil || head != (AttemptLedgerHead{Root: zeroAttemptHash()}) {
		t.Fatal("the runtime opened a non-empty prepared ledger", head, err)
	}
}

// Anything but this member's published, empty prepared ledger stays retained
// history that a fresh activation refuses.
func TestFreshActivationRefusesForeignOrRetainedLedger(t *testing.T) {
	f := newReleaseActivationLedgerFixture(t)
	members := []ReleaseActivationMemberV2{f.member}
	identity := releaseActivationLedgerIdentity(f.cfg, f.member)
	coordinator := strings.ToLower(f.cfg.Coordinator)
	foreignKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x62}, ed25519.SeedSize))
	for name, item := range map[string]struct {
		identity    func(AttemptLedgerIdentity) AttemptLedgerIdentity
		coordinator string
		publish     bool
		mutate      func(*testing.T, string, *ReleaseConfig)
		diagnostic  string
	}{
		"another uid": {identity: func(value AttemptLedgerIdentity) AttemptLedgerIdentity { value.ValidatorUID++; return value }, publish: true,
			diagnostic: "not this activation's storage-prepared empty ledger"},
		"another client key": {identity: func(value AttemptLedgerIdentity) AttemptLedgerIdentity {
			value.ValidatorVPK = attemptHex32([32]byte(foreignKey.Public().(ed25519.PublicKey)))
			return value
		}, publish: true, diagnostic: "not this activation's storage-prepared empty ledger"},
		"another operator": {identity: func(value AttemptLedgerIdentity) AttemptLedgerIdentity { value.NoID = 2; return value }, publish: true,
			diagnostic: "not this activation's storage-prepared empty ledger"},
		"another validator": {identity: func(value AttemptLedgerIdentity) AttemptLedgerIdentity { value.ValidatorID = 2; return value }, publish: true,
			diagnostic: "not this activation's storage-prepared empty ledger"},
		"another coordinator":    {coordinator: "0x" + strings.Repeat("12", 20), publish: true, diagnostic: "not this activation's storage-prepared empty ledger"},
		"unpublished checkpoint": {diagnostic: "no published custody checkpoint"},
		"replaced checkpoint": {publish: true, mutate: func(t *testing.T, path string, _ *ReleaseConfig) {
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			raw, err := readAttemptLedgerCustodyAttribute(file)
			if err != nil {
				t.Fatal(err)
			}
			var checkpoint attemptLedgerCustodyCheckpoint
			if err := json.Unmarshal(raw, &checkpoint); err != nil {
				t.Fatal(err)
			}
			checkpoint.DatabaseInode++
			if raw, err = json.Marshal(checkpoint); err == nil {
				err = durablesys.SetAttribute(int(file.Fd()), attemptLedgerCustodyAttribute, raw, unix.XATTR_REPLACE)
			}
			if err != nil {
				t.Fatal(err)
			}
		}, diagnostic: "not this activation's storage-prepared empty ledger"},
		"retained settlement state": {publish: true, mutate: func(t *testing.T, path string, _ *ReleaseConfig) {
			if err := os.WriteFile(filepath.Join(path, "stats.json"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, diagnostic: "already holds stats.json"},
		"provider request journal": {publish: true, mutate: func(_ *testing.T, _ string, cfg *ReleaseConfig) {
			cfg.Operators[0].RequestPreparation = &ReleaseEvidenceV2File{Path: filepath.Join(f.root, "request-preparation.json")}
		}, diagnostic: "provider request journal"},
	} {
		scopeIdentity, scopeCoordinator := identity, coordinator
		if item.identity != nil {
			scopeIdentity = item.identity(identity)
		}
		if item.coordinator != "" {
			scopeCoordinator = item.coordinator
		}
		path := f.prepare(t, "no-1-"+strings.ReplaceAll(name, " ", "-"), f.scope(scopeIdentity, scopeCoordinator), item.publish)
		cfg := f.at(path)
		if item.mutate != nil {
			item.mutate(t, path, cfg)
		}
		admitted, err := requireFreshReleaseOperatorState(t.Context(), cfg, members)
		if err == nil || len(admitted) != 0 || !strings.Contains(err.Error(), item.diagnostic) {
			t.Errorf("%s: fresh activation admitted the ledger or refused it for another cause: %v %v", name, admitted, err)
			continue
		}
		t.Logf("%s: %v", name, err)
	}
	// A signed, non-empty ledger for this exact identity is retained history,
	// with or without its imported legacy source beside it.
	path, scope, retained := attemptPreparationRetainedFixture(t)
	vpk, err := canonicalAttemptHex32("retained vpk", retained.identity.ValidatorVPK, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ReleaseConfig{DeploymentID: retained.identity.DeploymentID, ChainID: retained.identity.ChainID, GenesisHash: retained.identity.GenesisHash, Netuid: retained.identity.Netuid,
		ValidatorID: retained.identity.ValidatorID, Coordinator: attemptLedgerDiskTestCoordinator, Operators: []OperatorConfig{{NoID: retained.identity.NoID, StateDir: path}}}
	cfg.EvidenceV2.Bounds.Disk = scope.Limits
	member := ReleaseActivationMemberV2{NoID: retained.identity.NoID, ValidatorUID: retained.identity.ValidatorUID, Activation: protocol.ValidatorEvidenceActivation{VPK: vpk}}
	if releaseActivationLedgerIdentity(cfg, member) != retained.identity || scope.ExpectedHead.LastSequence == 0 {
		t.Fatal("retained control does not describe this member's non-empty ledger")
	}
	if _, err := requireFreshReleaseOperatorState(t.Context(), cfg, []ReleaseActivationMemberV2{member}); err == nil || !strings.Contains(err.Error(), "already holds "+attemptLedgerLegacyName) {
		t.Fatal("a retained ledger with its legacy source was admitted or refused for another cause", err)
	}
	if err := os.Remove(filepath.Join(path, attemptLedgerLegacyName)); err != nil {
		t.Fatal(err)
	}
	if _, err := requireFreshReleaseOperatorState(t.Context(), cfg, []ReleaseActivationMemberV2{member}); err == nil || !strings.Contains(err.Error(), "not this activation's storage-prepared empty ledger") {
		t.Fatal("a non-empty signed ledger was admitted as fresh or refused for another cause", err)
	}
}
