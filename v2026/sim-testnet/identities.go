package main

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/crv4"
)

type EVMRoleSecret struct {
	Label         string `json:"label"`
	PrivateKeyHex string `json:"private_key_hex"`
	Address       string `json:"address"`
}
type SubstrateRoleSecret struct {
	Label        string `json:"label"`
	SeedHex      string `json:"seed_hex"`
	PublicKeyHex string `json:"public_key_hex"`
	SS58         string `json:"ss58"`
}
type ClientRoleSecret struct {
	Label        string `json:"label"`
	SeedHex      string `json:"seed_hex"`
	PublicKeyHex string `json:"public_key_hex"`
	ClientIDHex  string `json:"client_id_hex"`
}
type RoleSecrets struct {
	Schema       string                         `json:"schema"`
	DeploymentID string                         `json:"deployment_id"`
	EVM          map[string]EVMRoleSecret       `json:"evm"`
	Substrate    map[string]SubstrateRoleSecret `json:"substrate"`
	Clients      map[string]ClientRoleSecret    `json:"clients"`
}

type finalPublicIdentity struct {
	PublicKey string `json:"public_key"`
	SS58      string `json:"ss58"`
}

type finalPublicClientIdentity struct {
	ClientID  string `json:"client_id"`
	ClientKey string `json:"client_key"`
}

// Keep the fields in encoding/json's former map-key order. The production
// writer and final decoder now share one wire contract without changing the
// canonical identities.json bytes or any hash that contains them.
type finalPublicIdentities struct {
	Clients      map[string]finalPublicClientIdentity `json:"clients"`
	DeploymentID string                               `json:"deployment_id"`
	EVM          map[string]string                    `json:"evm"`
	Schema       string                               `json:"schema"`
	Substrate    map[string]finalPublicIdentity       `json:"substrate"`
}

var roleSecretsCache struct {
	sync.Mutex
	key   [32]byte
	roles *RoleSecrets
}

func derive32(cfg *ResolvedConfig, label string) [32]byte {
	mac := hmac.New(sha256.New, []byte(cfg.WalletMaterial))
	mac.Write([]byte("urnetwork/sim-testnet/secret/v1\x00"))
	mac.Write([]byte(cfg.Config.Deployment.DeploymentID))
	mac.Write([]byte{0})
	mac.Write([]byte(label))
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

func buildEVMRoleSecrets(cfg *ResolvedConfig) (map[string]EVMRoleSecret, error) {
	if cfg == nil || cfg.WalletMaterial == "" || cfg.Config.Deployment.DeploymentID == "" {
		return nil, fmt.Errorf("role-secret derivation inputs are incomplete")
	}
	roles := map[string]EVMRoleSecret{}
	addEVM := func(label string) error {
		seed := derive32(cfg, "evm/"+label)
		key, err := crypto.ToECDSA(seed[:])
		if err != nil {
			return err
		}
		roles[label] = EVMRoleSecret{Label: label, PrivateKeyHex: hex.EncodeToString(crypto.FromECDSA(key)), Address: crypto.PubkeyToAddress(key.PublicKey).Hex()}
		return nil
	}
	for _, label := range []string{"deployer", "testnet-owner", "guardian", "commitment-oracle", "keeper"} {
		if err := addEVM(label); err != nil {
			return nil, err
		}
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		for _, purpose := range []string{"deposit", "root", "artifact", "claim-relayer"} {
			if err := addEVM(fmt.Sprintf("operator-%d-%s", i, purpose)); err != nil {
				return nil, err
			}
		}
	}
	return roles, nil
}

func buildRoleSecretsUncached(cfg *ResolvedConfig) (*RoleSecrets, error) {
	evm, err := buildEVMRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	r := &RoleSecrets{Schema: "urnetwork-sim-role-secrets-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, EVM: evm, Substrate: map[string]SubstrateRoleSecret{}, Clients: map[string]ClientRoleSecret{}}
	addSub := func(label string) error {
		seed := derive32(cfg, "substrate/"+label)
		kp, err := crv4.KeypairFromSeed(seed)
		if err != nil {
			return err
		}
		publicKey := kp.PublicKey()
		r.Substrate[label] = SubstrateRoleSecret{Label: label, SeedHex: hex.EncodeToString(seed[:]), PublicKeyHex: hex.EncodeToString(publicKey[:]), SS58: kp.Address()}
		return nil
	}
	for _, label := range []string{"reserve-hotkey", "escrow-hotkey"} {
		if err := addSub(label); err != nil {
			return nil, err
		}
	}
	for generation := uint64(1); generation <= maximumContractRegistrationGeneration(cfg.Config.Topology); generation++ {
		for _, label := range contractRegistrationRoleLabels(cfg.Config.Topology, generation) {
			if err := addSub(label); err != nil {
				return nil, err
			}
		}
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		for _, label := range []string{fleetHotkeyLabel(fleet), fleetColdkeyLabel(fleet)} {
			if err := addSub(label); err != nil {
				return nil, err
			}
		}
	}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		for _, label := range []string{churnHotkeyLabel(churn), churnColdkeyLabel(churn)} {
			if err := addSub(label); err != nil {
				return nil, err
			}
		}
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		for _, purpose := range []string{"pool-hotkey", "deposit-hotkey", "coldkey"} {
			if err := addSub(fmt.Sprintf("operator-%d-%s", i, purpose)); err != nil {
				return nil, err
			}
		}
	}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		if err := addSub(fmt.Sprintf("validator-%d-coldkey", i)); err != nil {
			return nil, err
		}
		if i > 1 {
			if err := addSub(fmt.Sprintf("validator-%d-hotkey", i)); err != nil {
				return nil, err
			}
		}
	}
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		if err := addSub(fmt.Sprintf("miner-%d-payout", i)); err != nil {
			return nil, err
		}
		seed := derive32(cfg, fmt.Sprintf("client/miner-%d", i))
		sk := ed25519.NewKeyFromSeed(seed[:])
		pub := sk.Public().(ed25519.PublicKey)
		// Client IDs are assigned by the operator API during account
		// provisioning. Only the independent Ed25519 key is derivable before
		// that finalized/local lifecycle step.
		r.Clients[fmt.Sprintf("miner-%d", i)] = ClientRoleSecret{Label: fmt.Sprintf("miner-%d", i), SeedHex: hex.EncodeToString(seed[:]), PublicKeyHex: hex.EncodeToString(pub)}
	}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		for op := 1; op <= cfg.Config.Topology.Operators; op++ {
			label := fmt.Sprintf("validator-%d-no-%d", i, op)
			seed := derive32(cfg, "client/"+label)
			sk := ed25519.NewKeyFromSeed(seed[:])
			pub := sk.Public().(ed25519.PublicKey)
			r.Clients[label] = ClientRoleSecret{Label: label, SeedHex: hex.EncodeToString(seed[:]), PublicKeyHex: hex.EncodeToString(pub)}
		}
	}
	return r, nil
}

func cloneRoleSecrets(source *RoleSecrets) *RoleSecrets {
	if source == nil {
		return nil
	}
	clone := &RoleSecrets{
		Schema: source.Schema, DeploymentID: source.DeploymentID,
		EVM: make(map[string]EVMRoleSecret, len(source.EVM)), Substrate: make(map[string]SubstrateRoleSecret, len(source.Substrate)), Clients: make(map[string]ClientRoleSecret, len(source.Clients)),
	}
	for label, role := range source.EVM {
		clone.EVM[label] = role
	}
	for label, role := range source.Substrate {
		clone.Substrate[label] = role
	}
	for label, role := range source.Clients {
		clone.Clients[label] = role
	}
	return clone
}

func roleSecretsCacheKey(cfg *ResolvedConfig) ([32]byte, error) {
	if cfg == nil || cfg.WalletMaterial == "" || cfg.Config.Deployment.DeploymentID == "" {
		return [32]byte{}, fmt.Errorf("role-secret derivation inputs are incomplete")
	}
	encoded, err := json.Marshal(struct {
		WalletMaterial string
		DeploymentID   string
		Topology       TopologyConfig
	}{WalletMaterial: cfg.WalletMaterial, DeploymentID: cfg.Config.Deployment.DeploymentID, Topology: cfg.Config.Topology})
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode role-secret cache identity: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

// BuildRoleSecrets derives the complete launch-scale identity set once per
// resolved deployment and returns a detached copy to every caller. Planning,
// revision, and doctor paths intentionally share the expensive deterministic
// 1,000-miner derivation without allowing one caller to mutate another's view.
func BuildRoleSecrets(cfg *ResolvedConfig) (*RoleSecrets, error) {
	key, err := roleSecretsCacheKey(cfg)
	if err != nil {
		return nil, err
	}
	roleSecretsCache.Lock()
	defer roleSecretsCache.Unlock()
	if roleSecretsCache.roles != nil && roleSecretsCache.key == key {
		return cloneRoleSecrets(roleSecretsCache.roles), nil
	}
	roles, err := buildRoleSecretsUncached(cfg)
	if err != nil {
		return nil, err
	}
	roleSecretsCache.key = key
	roleSecretsCache.roles = cloneRoleSecrets(roles)
	return roles, nil
}

func validatorHotkeyLabel(index int) string {
	if index == 1 {
		return "reserve-hotkey"
	}
	return fmt.Sprintf("validator-%d-hotkey", index)
}

func fleetHotkeyLabel(index int) string  { return fmt.Sprintf("fleet-%d-hotkey", index) }
func fleetColdkeyLabel(index int) string { return fmt.Sprintf("fleet-%d-coldkey", index) }
func churnHotkeyLabel(index int) string  { return fmt.Sprintf("churn-%d-hotkey", index) }
func churnColdkeyLabel(index int) string { return fmt.Sprintf("churn-%d-coldkey", index) }

func fleetMemberMinerIndex(cfg *ResolvedConfig, fleet, member int) int {
	return (fleet-1)*cfg.Config.Topology.ClientsPerHeadFleet + member
}

func (r RoleSecrets) secretFingerprint() (string, error) {
	clone := r
	clone.Clients = make(map[string]ClientRoleSecret, len(r.Clients))
	for label, client := range r.Clients {
		client.ClientIDHex = "" // server-assigned public state, not key material
		clone.Clients[label] = client
	}
	return canonicalHashHex(clone)
}

func saveRoleSecrets(path string, roles *RoleSecrets) error {
	b, err := json.MarshalIndent(roles, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0o600)
}

func extendRoleSecretsWithContractGenerations(topology TopologyConfig, got, expected *RoleSecrets) (*RoleSecrets, bool, error) {
	if got == nil || expected == nil || got.Schema != expected.Schema || got.DeploymentID != expected.DeploymentID {
		return nil, false, errors.New("existing role store identity does not match deterministic deployment identities")
	}
	merged := cloneRoleSecrets(got)
	if len(got.EVM) != len(expected.EVM) {
		return nil, false, errors.New("existing role store EVM identity set changed")
	}
	for label, role := range got.EVM {
		if expected.EVM[label] != role {
			return nil, false, fmt.Errorf("existing EVM role %s changed", label)
		}
	}
	if len(got.Clients) != len(expected.Clients) {
		return nil, false, errors.New("existing role store client identity set changed")
	}
	for label, role := range got.Clients {
		want, found := expected.Clients[label]
		assignedID := role.ClientIDHex
		role.ClientIDHex, want.ClientIDHex = "", ""
		if !found || role != want {
			return nil, false, fmt.Errorf("existing client role %s changed", label)
		}
		preserved := merged.Clients[label]
		preserved.ClientIDHex = assignedID
		merged.Clients[label] = preserved
	}
	for label, role := range got.Substrate {
		if expected.Substrate[label] != role {
			return nil, false, fmt.Errorf("existing substrate role %s changed", label)
		}
	}
	changed := false
	for label, role := range expected.Substrate {
		if _, found := got.Substrate[label]; found {
			continue
		}
		generation, _, _, contractRole := parseContractRegistrationRoleLabel(topology, label)
		if !contractRole || generation == 0 {
			return nil, false, fmt.Errorf("existing role store is missing non-extension role %s", label)
		}
		merged.Substrate[label] = role
		changed = true
	}
	if len(merged.Substrate) != len(expected.Substrate) {
		return nil, false, errors.New("existing role store has unapproved substrate roles")
	}
	return merged, changed, nil
}

func LoadOrWriteRoleSecrets(cfg *ResolvedConfig, stateDir string) (*RoleSecrets, error) {
	path := filepath.Join(stateDir, "secrets", "roles.json")
	expected, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	if b, readErr := os.ReadFile(path); readErr == nil {
		var got RoleSecrets
		if err := json.Unmarshal(b, &got); err != nil {
			return nil, err
		}
		merged, changed, err := extendRoleSecretsWithContractGenerations(cfg.Config.Topology, &got, expected)
		if err != nil {
			return nil, err
		}
		if changed {
			if err := saveRoleSecrets(path, merged); err != nil {
				return nil, fmt.Errorf("persist contract-generation role extension: %w", err)
			}
		}
		return merged, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	if err := saveRoleSecrets(path, expected); err != nil {
		return nil, err
	}
	return expected, nil
}

func (r *RoleSecrets) EVMKey(label string) (*EVMRoleSecret, error) {
	v, ok := r.EVM[label]
	if !ok {
		return nil, fmt.Errorf("missing EVM role %q", label)
	}
	return &v, nil
}
func (r *RoleSecrets) EVMAddress(label string) (common.Address, error) {
	v, e := r.EVMKey(label)
	if e != nil {
		return common.Address{}, e
	}
	if !common.IsHexAddress(v.Address) {
		return common.Address{}, fmt.Errorf("invalid role address")
	}
	return common.HexToAddress(v.Address), nil
}

func (r RoleSecrets) Public() finalPublicIdentities {
	evm := map[string]string{}
	for k, v := range r.EVM {
		evm[k] = v.Address
	}
	sub := map[string]finalPublicIdentity{}
	for k, v := range r.Substrate {
		sub[k] = finalPublicIdentity{PublicKey: "0x" + v.PublicKeyHex, SS58: v.SS58}
	}
	clients := map[string]finalPublicClientIdentity{}
	for k, v := range r.Clients {
		clients[k] = finalPublicClientIdentity{ClientID: "0x" + v.ClientIDHex, ClientKey: "0x" + v.PublicKeyHex}
	}
	return finalPublicIdentities{Clients: clients, DeploymentID: r.DeploymentID, EVM: evm, Schema: "urnetwork-sim-public-identities-v1", Substrate: sub}
}
