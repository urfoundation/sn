package main

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/crv4"
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

func BuildRoleSecrets(cfg *ResolvedConfig) (*RoleSecrets, error) {
	r := &RoleSecrets{Schema: "urnetwork-sim-role-secrets-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, EVM: map[string]EVMRoleSecret{}, Substrate: map[string]SubstrateRoleSecret{}, Clients: map[string]ClientRoleSecret{}}
	addEVM := func(label string) error {
		seed := derive32(cfg, "evm/"+label)
		key, err := crypto.ToECDSA(seed[:])
		if err != nil {
			return err
		}
		r.EVM[label] = EVMRoleSecret{Label: label, PrivateKeyHex: hex.EncodeToString(crypto.FromECDSA(key)), Address: crypto.PubkeyToAddress(key.PublicKey).Hex()}
		return nil
	}
	for _, label := range []string{"deployer", "testnet-owner", "guardian", "commitment-oracle", "keeper"} {
		if err := addEVM(label); err != nil {
			return nil, err
		}
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		for _, purpose := range []string{"deposit", "root", "artifact"} {
			if err := addEVM(fmt.Sprintf("operator-%d-%s", i, purpose)); err != nil {
				return nil, err
			}
		}
	}
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		if err := addEVM(fmt.Sprintf("miner-%d-claim-relayer", i)); err != nil {
			return nil, err
		}
	}
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
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		for _, label := range []string{fleetHotkeyLabel(fleet), fleetColdkeyLabel(fleet)} {
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

func validatorHotkeyLabel(index int) string {
	if index == 1 {
		return "reserve-hotkey"
	}
	return fmt.Sprintf("validator-%d-hotkey", index)
}

func fleetHotkeyLabel(index int) string  { return fmt.Sprintf("fleet-%d-hotkey", index) }
func fleetColdkeyLabel(index int) string { return fmt.Sprintf("fleet-%d-coldkey", index) }

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

func LoadOrWriteRoleSecrets(cfg *ResolvedConfig, stateDir string) (*RoleSecrets, error) {
	path := filepath.Join(stateDir, "secrets", "roles.json")
	expected, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(path); err == nil {
		var got RoleSecrets
		if err := json.Unmarshal(b, &got); err != nil {
			return nil, err
		}
		a, _ := got.secretFingerprint()
		e, _ := expected.secretFingerprint()
		if a != e {
			return nil, fmt.Errorf("existing role store does not match deterministic deployment identities")
		}
		return &got, nil
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

func (r RoleSecrets) Public() map[string]any {
	evm := map[string]string{}
	for k, v := range r.EVM {
		evm[k] = v.Address
	}
	sub := map[string]map[string]string{}
	for k, v := range r.Substrate {
		sub[k] = map[string]string{"public_key": "0x" + v.PublicKeyHex, "ss58": v.SS58}
	}
	clients := map[string]map[string]string{}
	for k, v := range r.Clients {
		clients[k] = map[string]string{"client_id": "0x" + v.ClientIDHex, "client_key": "0x" + v.PublicKeyHex}
	}
	return map[string]any{"schema": "urnetwork-sim-public-identities-v1", "deployment_id": r.DeploymentID, "evm": evm, "substrate": sub, "clients": clients}
}
