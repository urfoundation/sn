// Settlement v2 phase regressions cross the real command, approval, release
// pin and private journals. No fixture signature authorizes live work.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The plan command's exact public review output.
type evmV2TestPlanOutput struct {
	Plan                   evmPhasePlan                    `json:"plan"`
	PlanHash               string                          `json:"plan_hash"`
	ExecutableAction       string                          `json:"executable_action"`
	InstallationComplete   bool                            `json:"installation_complete"`
	EscrowRegistration     *contractEscrowRegistration     `json:"escrow_registration"`
	VaultBinding           *contractVaultBinding           `json:"vault_binding"`
	Vault2Constructor      *contractVault2Constructor      `json:"vault2_constructor"`
	Settlement2Initializer *contractSettlement2Initializer `json:"settlement2_initializer"`
}

// Run one signed review through the top-level dispatcher.
func (self *evmCreateFixture) v2Plan(action string) (evmV2TestPlanOutput, int, string) {
	self.t.Helper()
	var stdout, stderr bytes.Buffer
	code := runMain(self.storage.Context, []string{"bootstrap-contracts", "plan", "--config", self.configPath, "--action", action}, &stdout, &stderr)
	var output evmV2TestPlanOutput
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			self.t.Fatal(err)
		}
	}
	return output, code, stderr.String()
}

// SV-8, A7: one approval covers the five-action graph, with the escrow
// registration directly after vault2. Every action selects from the same plan
// hash, and each exports exactly the review values its prefix decoded. Review
// reads no route.
func TestEvmV2PlanSelectsEveryActionOfOneGraph(t *testing.T) {
	f := newEvmV2Fixture(t)
	sender := f.config.Plan.Actions[0].Sender
	vault, proxy := crypto.CreateAddress(sender, 0), crypto.CreateAddress(sender, 3)
	mirror := ss58.EvmMirrorPubkey(vault)
	if !slices.Equal(evmV2ActionIds(), []string{"vault2-create", "escrow2-register", "settlement2-create", "proxy2-create", "vault2-link"}) {
		t.Fatalf("escrow registration must directly follow vault2 creation: %v", evmV2ActionIds())
	}
	for index, id := range evmV2ActionIds() {
		output, code, diagnostic := f.v2Plan(id)
		if code != 0 || output.PlanHash != f.config.Plan.hash() || output.ExecutableAction != id || output.InstallationComplete || rootObjectHash(output.Plan) != rootObjectHash(f.config.Plan) {
			t.Fatalf("%s plan: %+v %d %s", id, output, code, diagnostic)
		}
		if output.Vault2Constructor == nil || *output.Vault2Constructor != (contractVault2Constructor{EscrowHotkey: common.Hash(evmV2TestHotkey).Hex(), DemandFeeWei: evmV2TestFee.String()}) {
			t.Fatalf("%s vault2 constructor: %+v", id, output.Vault2Constructor)
		}
		registration := contractEscrowRegistration{MaximumBurnRao: evmV2TestBurnCap, FundingWei: "2000000000", Hotkey: common.Hash(evmV2TestHotkey).Hex(), Coldkey: common.Hash(mirror).Hex()}
		if (index >= 1) != (output.EscrowRegistration != nil) || output.EscrowRegistration != nil && *output.EscrowRegistration != registration {
			t.Fatalf("%s escrow registration: %+v", id, output.EscrowRegistration)
		}
		initializer := contractSettlement2Initializer{Owner: evmV2TestOwner, V1Coordinator: evmV2TestV1, Vault: vault, Auditors: []common.Address{evmV2TestAuditor}, Quorum: 1}
		if (index >= 3) != (output.Settlement2Initializer != nil) || output.Settlement2Initializer != nil && rootObjectHash(*output.Settlement2Initializer) != rootObjectHash(initializer) {
			t.Fatalf("%s initializer: %+v", id, output.Settlement2Initializer)
		}
		if (index == 4) != (output.VaultBinding != nil) || output.VaultBinding != nil && *output.VaultBinding != (contractVaultBinding{Vault: vault, Coordinator: proxy}) {
			t.Fatalf("%s binding: %+v", id, output.VaultBinding)
		}
	}
	if len(f.counts) != 0 {
		t.Fatal("signed review reached the route")
	}
}

// SV-8, A5: every builder check refuses its own fault. Each fault is
// independently approved so the refusal is semantic, and any fault in the graph
// blocks even the first action before custody or a route opens.
func TestEvmV2BuilderRefusesEachCheck(t *testing.T) {
	type fault struct {
		name, expected string
		edit           func(*evmCreateFixture, contractReleaseArtifacts)
	}
	action := func(f *evmCreateFixture, index int) *evmPhaseAction { return &f.config.Plan.Actions[index] }
	sender := func(f *evmCreateFixture) common.Address { return f.config.Plan.Actions[0].Sender }
	vaultAddress := func(f *evmCreateFixture) common.Address { return crypto.CreateAddress(sender(f), 0) }
	mirror := func(f *evmCreateFixture) [32]byte { return ss58.EvmMirrorPubkey(vaultAddress(f)) }
	setVault := func(f *evmCreateFixture, release contractReleaseArtifacts, netuid uint16, hotkey, coldkey [32]byte, bootstrap common.Address, fee *big.Int) {
		action(f, 0).Data = evmV2TestVaultData(t, release, netuid, hotkey, coldkey, bootstrap, fee)
	}
	setInitializer := func(f *evmCreateFixture, release contractReleaseArtifacts, owner, v1, vault common.Address, auditors []common.Address, quorum int64) {
		initializer := stabi.NewSTSettlement2().PackInitialize(owner, v1, vault, auditors, big.NewInt(quorum))
		action(f, 3).Data = evmV2TestProxyData(t, release, crypto.CreateAddress(sender(f), 2), initializer)
	}
	editData := func(f *evmCreateFixture, index int, change func([]byte) []byte) {
		data, err := hex.DecodeString(action(f, index).Data[2:])
		if err != nil {
			t.Fatal(err)
		}
		action(f, index).Data = "0x" + hex.EncodeToString(change(data))
	}
	replace := func(f *evmCreateFixture, release contractReleaseArtifacts, artifact contractReleaseArtifact) {
		changed := contractReleaseArtifacts{Schema: release.Schema}
		for _, existing := range release.Artifacts {
			if existing.Name == artifact.Name {
				existing = artifact
			}
			changed.Artifacts = append(changed.Artifacts, existing)
		}
		f.writeV2Release(changed)
	}
	seventeen := []common.Address{}
	for index := range 17 {
		seventeen = append(seventeen, common.Address{60, byte(index)})
	}
	structure := "contract phase lacks exact identity, scope or finite bounds"
	edges := "settlement v2 actions must be one deployer's consecutive nonces with zero-value CREATEs"
	release := "contract release must contain exactly its phase's production artifacts"
	vaultPayload := "vault2 payload is not the exact release creation and five constructor words"
	vaultConstructor := "approved vault2 constructor differs"
	implementation := "approved settlement2 implementation differs from the exact argument-free release creation"
	escrowCall := "vault2 escrow registration must be one exact uint64 call to the derived vault2"
	escrowValue := "vault2 escrow registration selector, nonzero burn cap or exact wei funding differs"
	proxyCreation := "settlement2 proxy payload differs from the exact release creation"
	initializerValues := "settlement2 initializer vault, owner, v1 coordinator, auditors, quorum or role separation differs"
	proxyEncoding := "settlement2 proxy payload differs from the exact release and initializer encoding"
	link := "vault2 binding must call the derived vault2 with the exact initialized proxy and zero value"
	faults := []fault{
		{"config-schema", structure, func(f *evmCreateFixture, _ contractReleaseArtifacts) { f.config.Schema = evmPhaseConfigSchema }},
		{"reserve-hotkey", structure, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			f.config.Plan.ReserveHotkey = common.Hash{31}.Hex()
		}},
		{"four-actions", structure, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			f.config.Plan.Actions = f.config.Plan.Actions[:4]
		}},
		{"unknown-sixth-action", "contract phase action sequence differs", func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			extra := *action(f, 4)
			extra.Id, extra.Nonce = "vault2-extra", 5
			f.config.Plan.Actions = append(f.config.Plan.Actions, extra)
		}},
		{"seven-actions", structure, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			batcher := evmV2TestBatcherAction(t, current, sender(f), 5)
			extra := batcher
			extra.Id, extra.Nonce = "batcher-extra", 6
			f.config.Plan.Actions = append(f.config.Plan.Actions, batcher, extra)
		}},
		{"action-order", "contract phase action sequence differs", func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			action(f, 1).Id, action(f, 3).Id = action(f, 3).Id, action(f, 1).Id
		}},
		{"first-target", "first contract action must be a zero-value CREATE", func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			target := common.Address{42}
			action(f, 0).To = &target
		}},
		{"nonce-overflow", "settlement v2 nonces overflow", func(f *evmCreateFixture, _ contractReleaseArtifacts) { action(f, 0).Nonce = ^uint64(0) - 3 }},
		{"sender", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) { action(f, 2).Sender = common.Address{42} }},
		{"nonce", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) { action(f, 1).Nonce = 9 }},
		{"create-target", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			target := common.Address{42}
			action(f, 2).To = &target
		}},
		{"create-value", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			action(f, 3).ValueWei, f.config.Plan.MaximumTotalWei = "1", "2106500001"
		}},
		{"config-alias", "contract phase input aliases a settlement v2 journal", func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			f.configPath = filepath.Join(f.config.Plan.RunDirectory, "vault2-link.json")
		}},
		{"release-alias", "contract phase input aliases a settlement v2 journal", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			f.writeV2Release(current)
			alias := filepath.Join(f.config.Plan.RunDirectory, "proxy2-create.json.lock")
			raw, err := os.ReadFile(f.config.Plan.Artifacts.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(alias); err != nil || os.WriteFile(alias, raw, 0600) != nil {
				t.Fatal(err)
			}
			f.config.Plan.Artifacts.Path = alias
		}},
		{"original-release", release, func(f *evmCreateFixture, _ contractReleaseArtifacts) { f.writeV2Release(evmTestRelease(t)) }},
		{"missing-artifact", release, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			f.writeV2Release(contractReleaseArtifacts{Schema: current.Schema, Artifacts: current.Artifacts[:2]})
		}},
		{"unknown-artifact", "contract release has an unknown or duplicated artifact", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			unknown := current.Artifacts[1]
			unknown.Name = "Coordinator"
			f.writeV2Release(contractReleaseArtifacts{Schema: current.Schema, Artifacts: []contractReleaseArtifact{current.Artifacts[0], unknown, current.Artifacts[2], current.Artifacts[3]}})
		}},
		{"vault2-creation", vaultPayload, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 0, func(data []byte) []byte { data[0] ^= 1; return data })
		}},
		{"vault2-trailing", vaultPayload, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 0, func(data []byte) []byte { return append(data, 0) })
		}},
		{"vault2-short", vaultPayload, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 0, func(data []byte) []byte { return data[:len(data)-32] })
		}},
		{"vault2-netuid", vaultConstructor, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setVault(f, current, 26, evmV2TestHotkey, mirror(f), sender(f), evmV2TestFee)
		}},
		{"vault2-wide-netuid", vaultConstructor, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 0, func(data []byte) []byte { data[len(data)-5*32] = 1; return data })
		}},
		{"vault2-zero-hotkey", vaultConstructor, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setVault(f, current, 25, [32]byte{}, mirror(f), sender(f), evmV2TestFee)
		}},
		{"vault2-coldkey", vaultConstructor, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setVault(f, current, 25, evmV2TestHotkey, ss58.EvmMirrorPubkey(crypto.CreateAddress(sender(f), 1)), sender(f), evmV2TestFee)
		}},
		{"vault2-bootstrap", vaultConstructor, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setVault(f, current, 25, evmV2TestHotkey, mirror(f), evmV2TestOwner, evmV2TestFee)
		}},
		{"vault2-zero-fee", vaultConstructor, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setVault(f, current, 25, evmV2TestHotkey, mirror(f), sender(f), new(big.Int))
		}},
		{"vault2-sub-rao-fee", vaultConstructor, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setVault(f, current, 25, evmV2TestHotkey, mirror(f), sender(f), new(big.Int).Add(evmV2TestFee, big.NewInt(1)))
		}},
		{"vault2-constructor-abi", vaultConstructor, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			artifact := evmV2TestArtifactNamed(t, current, "SettlementVault2")
			artifact.Abi = strings.Replace(artifact.Abi, `"name":"demandFeeWei_","type":"uint256"`, `"name":"demandFeeWei_","type":"uint64"`, 1)
			replace(f, current, artifact)
		}},
		{"vault2-immutables", "contract immutable census differs", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			artifact := evmV2TestArtifactNamed(t, current, "SettlementVault2")
			references := maps.Clone(artifact.ImmutableReferences)
			delete(references, "demandFeeWei")
			artifact.ImmutableReferences = references
			replace(f, current, artifact)
		}},
		{"settlement2-creation", implementation, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 2, func(data []byte) []byte { data[0] ^= 1; return data })
		}},
		{"settlement2-arguments", implementation, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 2, func(data []byte) []byte { return append(data, make([]byte, 32)...) })
		}},
		{"settlement2-constructor", implementation, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			artifact := evmV2TestArtifactNamed(t, current, "Settlement2")
			artifact.Abi = strings.Replace(artifact.Abi, `{"type":"constructor","inputs":[]`, `{"type":"constructor","inputs":[{"name":"owner_","type":"address","internalType":"address"}]`, 1)
			replace(f, current, artifact)
		}},
		{"settlement2-immutables", "contract immutable self is absent or not one word", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			artifact := evmV2TestArtifactNamed(t, current, "Settlement2")
			artifact.ImmutableReferences = map[string][]int{"self": {0}}
			replace(f, current, artifact)
		}},
		{"escrow-target", escrowCall, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			target := crypto.CreateAddress(sender(f), 2)
			action(f, 1).To = &target
		}},
		{"escrow-length", escrowCall, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 1, func(data []byte) []byte { return data[:35] })
		}},
		{"escrow-trailing", escrowCall, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 1, func(data []byte) []byte { return append(data, 0) })
		}},
		{"escrow-selector", escrowValue, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 1, func(data []byte) []byte { data[0] ^= 1; return data })
		}},
		{"escrow-zero-cap", escrowValue, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			action(f, 1).Data, action(f, 1).ValueWei = "0x"+hex.EncodeToString(stabi.NewSTSettlementVault2().PackRegisterEscrow(0)), "0"
		}},
		{"escrow-value", escrowValue, func(f *evmCreateFixture, _ contractReleaseArtifacts) { action(f, 1).ValueWei = "1999999999" }},
		{"proxy-immutables", "contract immutable census differs", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			artifact := evmV2TestArtifactNamed(t, current, "ERC1967Proxy")
			artifact.ImmutableReferences = map[string][]int{"implementation": {0}}
			replace(f, current, artifact)
		}},
		{"proxy-creation", proxyCreation, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 3, func(data []byte) []byte { data[0] ^= 1; return data })
		}},
		{"proxy-constructor", "settlement2 proxy constructor requires an implementation and an atomic initializer", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			action(f, 3).Data = "0x" + evmV2TestArtifactNamed(t, current, "ERC1967Proxy").Creation + strings.Repeat("00", 31)
		}},
		{"proxy-implementation", "settlement2 proxy implementation or initializer differs", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			initializer := stabi.NewSTSettlement2().PackInitialize(evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor}, big.NewInt(1))
			action(f, 3).Data = evmV2TestProxyData(t, current, vaultAddress(f), initializer)
		}},
		{"proxy-initializer-short", "settlement2 proxy implementation or initializer differs", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			action(f, 3).Data = evmV2TestProxyData(t, current, crypto.CreateAddress(sender(f), 2), []byte{1, 2, 3})
		}},
		{"proxy-vault", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, crypto.CreateAddress(sender(f), 2), []common.Address{evmV2TestAuditor}, 1)
		}},
		{"proxy-zero-owner", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, common.Address{}, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor}, 1)
		}},
		{"proxy-zero-v1", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, common.Address{}, vaultAddress(f), []common.Address{evmV2TestAuditor}, 1)
		}},
		{"proxy-no-auditors", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{}, 1)
		}},
		{"proxy-seventeen-auditors", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, vaultAddress(f), seventeen, 1)
		}},
		{"proxy-zero-auditor", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor, {}}, 1)
		}},
		{"proxy-duplicate-auditor", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor, evmV2TestAuditor}, 1)
		}},
		{"proxy-zero-quorum", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor}, 0)
		}},
		{"proxy-quorum-above-auditors", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor}, 2)
		}},
		{"proxy-owner-deployer", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, sender(f), evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor}, 1)
		}},
		{"proxy-auditor-deployer", initializerValues, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			setInitializer(f, current, evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor, sender(f)}, 1)
		}},
		{"proxy-initializer-selector", proxyEncoding, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			initializer := stabi.NewSTSettlement2().PackInitialize(evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor}, big.NewInt(1))
			initializer[0] ^= 1
			action(f, 3).Data = evmV2TestProxyData(t, current, crypto.CreateAddress(sender(f), 2), initializer)
		}},
		{"proxy-initializer-word", proxyEncoding, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			initializer := stabi.NewSTSettlement2().PackInitialize(evmV2TestOwner, evmV2TestV1, vaultAddress(f), []common.Address{evmV2TestAuditor}, big.NewInt(1))
			initializer[4] = 1
			action(f, 3).Data = evmV2TestProxyData(t, current, crypto.CreateAddress(sender(f), 2), initializer)
		}},
		{"proxy-trailing", proxyEncoding, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 3, func(data []byte) []byte { return append(data, make([]byte, 32)...) })
		}},
		{"link-target", link, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			target := crypto.CreateAddress(sender(f), 3)
			action(f, 4).To = &target
		}},
		{"link-value", link, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			action(f, 4).ValueWei, f.config.Plan.MaximumTotalWei = "1", "2106500001"
		}},
		{"link-coordinator", link, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			action(f, 4).Data = "0x" + hex.EncodeToString(stabi.NewSTSettlementVault2().PackSetCoordinatorOnce(crypto.CreateAddress(sender(f), 2)))
		}},
		{"link-selector", link, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, 4, func(data []byte) []byte { data[0] ^= 1; return data })
		}},
	}
	for _, fault := range faults {
		f := newEvmV2Fixture(t)
		current := evmV2TestRelease(t)
		fault.edit(f, current)
		f.publishApproval()
		for _, id := range []string{"vault2-create", "vault2-link"} {
			if _, code, diagnostic := f.v2Plan(id); code != 2 || !strings.Contains(diagnostic, "contract phase authority: "+fault.expected) {
				t.Fatalf("%s escaped settlement v2 admission of %s: %d %s", fault.name, id, code, diagnostic)
			}
		}
		if len(f.counts) != 0 {
			t.Fatalf("%s reached the route", fault.name)
		}
	}
}

// SV-8: the plan hash is the v2 domain over the v2 approval bytes. The same
// typed plan under the original domains hashes and signs differently, and a
// single changed field changes the hash that apply must accept.
func TestEvmV2PlanHashIsDomainSeparated(t *testing.T) {
	f := newEvmV2Fixture(t)
	p := f.config.Plan
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	message, err := p.signingBytes()
	if err != nil || !bytes.Equal(message, append([]byte(evmPhase2ApprovalSchema+"\x00"), raw...)) {
		t.Fatalf("v2 approval bytes differ from their domain and typed plan: %v", err)
	}
	expected := rootObjectHash(struct {
		Domain string
		Bytes  []byte
	}{Domain: evmPhase2Schema, Bytes: message})
	original := rootObjectHash(struct {
		Domain string
		Bytes  []byte
	}{Domain: evmPhaseSchema, Bytes: append([]byte(evmPhaseApprovalSchema+"\x00"), raw...)})
	if p.hash() != expected || p.hash() == original || strings.Contains(string(raw), "reserve_hotkey") {
		t.Fatalf("v2 plan hash is not domain separated: %s %s %s", p.hash(), expected, original)
	}
	changed := p
	changed.Actions = slices.Clone(p.Actions)
	changed.Actions[4].Gas++
	if changed.hash() == p.hash() {
		t.Fatal("changed graph kept its approval hash")
	}
	for _, accepted := range []struct{ hash, run string }{{changed.hash(), p.RunDirectory}, {p.hash(), filepath.Join(p.RunDirectory, "other")}} {
		var stdout, stderr bytes.Buffer
		code := runMain(f.storage.Context, []string{"bootstrap-contracts", "apply", "--action", "vault2-create", "--config", f.configPath, "--run-dir", accepted.run, "--accept-plan-hash", accepted.hash}, &stdout, &stderr)
		if code != 3 || !strings.Contains(stderr.String(), "acceptance differs; no journal opened") {
			t.Fatalf("unaccepted v2 plan opened custody: %d %s", code, stderr.String())
		}
	}
	if _, err := os.Lstat(filepath.Join(p.RunDirectory, "vault2-create.json")); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("refused acceptance left custody or reached the route: %v", err)
	}
}

// SV-8: only an Ed25519 signature by the independent key over the exact v2
// domain bytes admits the graph. An original-domain signature over the same
// plan, a changed plan, another key and every alternate spelling are refused.
func TestEvmV2ApprovalIsIndependent(t *testing.T) {
	f := newEvmV2Fixture(t)
	seed := sha256.Sum256([]byte("synthetic contract independent approval"))
	key := ed25519.NewKeyFromSeed(seed[:])
	other := ed25519.NewKeyFromSeed(make([]byte, 32))
	raw, err := json.Marshal(f.config.Plan)
	if err != nil {
		t.Fatal(err)
	}
	original := hex.EncodeToString(ed25519.Sign(key, append([]byte(evmPhaseApprovalSchema+"\x00"), raw...)))
	message, err := f.config.Plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	invalid, spelling := "contract phase independent approval is invalid", "approval signature requires 128 unprefixed lowercase hex characters"
	for _, fault := range []struct {
		name, expected string
		edit           func(*evmPhaseConfig)
	}{
		{"original-domain", invalid, func(config *evmPhaseConfig) { config.Signature = original }},
		{"other-key", invalid, func(config *evmPhaseConfig) { config.Signature = hex.EncodeToString(ed25519.Sign(other, message)) }},
		{"changed-plan", invalid, func(config *evmPhaseConfig) { config.Plan.DeploymentId = "changed-settlement-v2" }},
		{"changed-key", invalid, func(config *evmPhaseConfig) {
			config.ApprovalPublicKey = "0x" + hex.EncodeToString(other.Public().(ed25519.PublicKey))
		}},
		{"prefixed", spelling, func(config *evmPhaseConfig) { config.Signature = "0x" + config.Signature[2:] }},
		{"uppercase", spelling, func(config *evmPhaseConfig) { config.Signature = strings.ToUpper(config.Signature) }},
		{"short", spelling, func(config *evmPhaseConfig) { config.Signature = config.Signature[:126] }},
		{"unsigned", spelling, func(config *evmPhaseConfig) { config.Signature = "" }},
	} {
		config := copyEvmPhaseConfig(f.config)
		fault.edit(&config)
		raw, err := json.Marshal(config)
		if err != nil || os.WriteFile(f.configPath, raw, 0600) != nil {
			t.Fatal(err)
		}
		if _, code, diagnostic := f.v2Plan("vault2-create"); code != 2 || !strings.Contains(diagnostic, fault.expected) {
			t.Fatalf("%s approval admitted the v2 graph: %d %s", fault.name, code, diagnostic)
		}
	}
	f.publishApproval()
	if _, code, diagnostic := f.v2Plan("vault2-create"); code != 0 {
		t.Fatalf("exact approval refused: %d %s", code, diagnostic)
	}
}

// SV-8: unsigned preview exports the exact approval bytes, every predicted
// identity and the complete completion readback without custody or a route.
// A signed config is refused; plan is the signed review.
func TestEvmV2PreviewExportsCompletionReadback(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	f.config.Signature = ""
	f.config.Plan.RunDirectory = filepath.Join(f.config.Plan.RunDirectory, "absent-run")
	raw, err := json.Marshal(f.config)
	if err != nil || os.WriteFile(f.configPath, raw, 0600) != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "preview", "--config", f.configPath, "--action", "proxy2-create"}, &stdout, &stderr)
	var preview evmV2PhasePreview
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil || code != 0 {
		t.Fatalf("v2 preview: %d %v %s", code, err, stderr.String())
	}
	message, err := f.config.Plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(message)
	readback := func(plan *evmCreatePlan) evmV2PreviewContract {
		return evmV2PreviewContract{Address: plan.Address.Hex(), RuntimeHash: crypto.Keccak256Hash(plan.Runtime).Hex(), Getters: plan.Getters, Storage: plan.Storage}
	}
	if preview.Schema != evmPhase2PreviewSchema || preview.PlanHash != f.config.Plan.hash() || preview.ApprovalSigningMessageHex != hex.EncodeToString(message) || preview.ApprovalSigningMessageSha256 != "sha256:"+hex.EncodeToString(digest[:]) || preview.ApprovalVerified || preview.InstallationComplete || preview.ExecutableAction != "proxy2-create" || preview.ApprovalPublicKey != f.config.ApprovalPublicKey {
		t.Fatalf("v2 preview changed its approval material: %+v", preview)
	}
	if rootObjectHash(preview.Vault2) != rootObjectHash(readback(graph[4])) || rootObjectHash(preview.Settlement2Implementation) != rootObjectHash(readback(graph[2])) || rootObjectHash(preview.Settlement2) != rootObjectHash(readback(graph[3])) || rootObjectHash(preview.Settlement2Initializer) != rootObjectHash(graph[4].Settlement2Initializer) || *preview.Vault2Constructor != *graph[4].Vault2Constructor || *preview.EscrowRegistration != *graph[4].EscrowRegistration || *preview.VaultBinding != *graph[4].VaultBinding {
		t.Fatalf("v2 preview omitted the completion readback or review values: %+v", preview)
	}
	if _, err := os.Lstat(f.config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("v2 preview acquired custody or a route: %v", err)
	}
	f.config.Plan.RunDirectory = filepath.Dir(f.config.Plan.RunDirectory)
	f.publishApproval()
	stdout.Reset()
	stderr.Reset()
	if code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "preview", "--config", f.configPath, "--action", "vault2-create"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "contract preview requires an unsigned config") {
		t.Fatalf("signed v2 config passed unsigned preview: %d %s", code, stderr.String())
	}
}

// SV-8: preview refusals for a malformed draft, a draft outside the phase's
// bounds, an unknown action and a draft that is not the settlement v2 graph.
func TestEvmV2PreviewRefusesForeignInputs(t *testing.T) {
	f := newEvmV2Fixture(t)
	unsigned := copyEvmPhaseConfig(f.config)
	unsigned.Signature = ""
	write := func(config evmPhaseConfig) {
		raw, err := json.Marshal(config)
		if err != nil || os.WriteFile(f.configPath, raw, 0600) != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.configPath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEvmV2PhasePreview(t.Context(), f.configPath, "vault2-create"); err == nil {
		t.Fatal("malformed draft previewed")
	}
	malformed := copyEvmPhaseConfig(unsigned)
	malformed.Plan.MaximumAttempts = 0
	write(malformed)
	if _, err := loadEvmV2PhasePreview(t.Context(), f.configPath, "vault2-create"); err == nil || !strings.Contains(err.Error(), "contract phase lacks exact identity") {
		t.Fatalf("unbounded draft previewed: %v", err)
	}
	write(unsigned)
	if _, err := loadEvmV2PhasePreview(t.Context(), f.configPath, "vault-create"); !errors.Is(err, errEvmV2Action) {
		t.Fatalf("original action previewed as settlement v2: %v", err)
	}
	original := newEvmVaultFixture(t)
	original.config.Signature = ""
	raw, err := json.Marshal(original.config)
	if err != nil || os.WriteFile(original.configPath, raw, 0600) != nil {
		t.Fatal(err)
	}
	if _, err := loadEvmV2PhasePreview(t.Context(), original.configPath, "vault2-create"); err == nil || !strings.Contains(err.Error(), "not the five-action settlement v2 phase") {
		t.Fatalf("original graph previewed as settlement v2: %v", err)
	}
}

// SV-8: the v2 graph binds a reviewed runtime source to the spec version it
// declares. Every reviewed pair passes, as does the frontier mapping source,
// which declares none. A reviewed source named for another version is refused,
// already in unsigned review, and the original phase keeps the pairs its
// recorded approvals hold.
func TestEvmV2PhaseBindsReviewedSourceToItsSpecVersion(t *testing.T) {
	f := newEvmV2Fixture(t)
	mismatch := "contract phase runtime version differs from the one its source commit was reviewed for"
	pair := func(config evmPhaseConfig, source string, spec uint32) evmPhaseConfig {
		changed := copyEvmPhaseConfig(config)
		changed.Plan.Runtime.RuntimeSourceCommit, changed.Plan.Runtime.RuntimeVersion.SpecVersion = source, spec
		return changed
	}
	reviewed := map[string]uint32{crv4.NativeOwnerSource470: 470, crv4.NativeOwnerSource473: 473, crv4.NativeOwnerSource475: 475, crv4.NativeOwnerSource477: 477, crv4.NativeOwnerSource478: 478, crv4.NativeOwnerSource479: 479}
	for source, spec := range reviewed {
		if err := pair(f.config, source, spec).validateStructure(); err != nil {
			t.Fatalf("reviewed source %s refused at its own version %d: %v", source, spec, err)
		}
		for _, other := range []uint32{spec - 1, spec + 1, 470, 473, 475, 477, 478, 479} {
			if other == spec {
				continue
			}
			if err := pair(f.config, source, other).validateStructure(); err == nil || err.Error() != mismatch {
				t.Fatalf("reviewed source %s labelled runtime %d: %v", source, other, err)
			}
		}
	}
	for _, spec := range []uint32{1, 475, 477, 478, 479, 480} {
		if err := pair(f.config, frontierMappingSourceCommit, spec).validateStructure(); err != nil {
			t.Fatalf("frontier mapping source refused at version %d: %v", spec, err)
		}
	}
	// A predecessor's review on the runtime that replaced it, as an unsigned
	// draft: 475's on 477, 477's on 478, and 478's on 479.
	for source, spec := range map[string]uint32{crv4.NativeOwnerSource475: 477, crv4.NativeOwnerSource477: 478, crv4.NativeOwnerSource478: 479} {
		draft := pair(f.config, source, spec)
		draft.Signature = ""
		raw, err := json.Marshal(draft)
		if err != nil || os.WriteFile(f.configPath, raw, 0600) != nil {
			t.Fatal(err)
		}
		if _, err := loadEvmV2PhasePreview(t.Context(), f.configPath, "vault2-create"); err == nil || err.Error() != mismatch {
			t.Fatalf("draft naming source %s for runtime %d previewed: %v", source, spec, err)
		}
	}
	original := newEvmVaultFixture(t)
	if err := pair(original.config, crv4.NativeOwnerSource475, 477).validateStructure(); err != nil {
		t.Fatalf("original phase lost a recorded source and version pair: %v", err)
	}
}

// SV-8: the original and v2 phases never select each other's actions, in
// signed review and in unsigned preview.
func TestEvmV2CommandRefusesCrossPhaseActions(t *testing.T) {
	f := newEvmV2Fixture(t)
	for _, id := range []string{"reserve-create", "vault-link"} {
		if _, code, diagnostic := f.v2Plan(id); code != 2 || !strings.Contains(diagnostic, "contract phase selected action: "+errEvmV2Action.Error()) {
			t.Fatalf("v2 approval selected original %s: %d %s", id, code, diagnostic)
		}
	}
	original := newEvmVaultFixture(t)
	if _, code, diagnostic := original.v2Plan("vault2-create"); code != 2 || !strings.Contains(diagnostic, "contract phase selected action: contract action is not implemented") {
		t.Fatalf("original approval selected a v2 action: %d %s", code, diagnostic)
	}
	unsigned := copyEvmPhaseConfig(f.config)
	unsigned.Signature = ""
	raw, err := json.Marshal(unsigned)
	if err != nil || os.WriteFile(f.configPath, raw, 0600) != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "preview", "--config", f.configPath, "--action", "vault-create"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "contract release must contain exactly its phase's production artifacts") {
		t.Fatalf("original preview accepted the v2 release: %d %s", code, stderr.String())
	}
	if len(f.counts) != 0 || len(original.counts) != 0 {
		t.Fatal("cross-phase review reached a route")
	}
}

// SV-8: offline apply creates only the first v2 journal under its own schema
// and imports exact signed bytes. A later action needs the retained, readback-
// verified predecessor; a signed-byte path may not alias v2 custody.
func TestEvmV2OfflineCustodyPreparesFirstAction(t *testing.T) {
	f := newEvmV2Fixture(t)
	f.signSelectedAction()
	sender := f.config.Plan.Actions[0].Sender
	result, code, diagnostic := f.command("apply", "--action", "vault2-create")
	if code != 0 || result.Status != "signature-awaiting-import" || result.ExecutableAction != "vault2-create" || result.Vault2Address != crypto.CreateAddress(sender, 0).Hex() || result.Address != "" || result.InstallationComplete || result.ActivationReady || !slices.Equal(result.RemainingActions, evmV2ActionIds()[1:]) || result.Vault2Constructor == nil || result.EscrowRegistration != nil {
		t.Fatalf("v2 preparation: %+v %d %s", result, code, diagnostic)
	}
	alias := filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json.lock")
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--signed-transaction", alias, "--signed-transaction-hash", f.signedHash); code != 2 || !strings.Contains(diagnostic, "signed-byte input aliases custody") {
		t.Fatalf("signed bytes aliased v2 custody: %d %s", code, diagnostic)
	}
	result, code, diagnostic = f.command("resume", "--action", "vault2-create", "--signed-transaction", f.signedPath, "--signed-transaction-hash", f.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != f.tx.Hash().Hex() {
		t.Fatalf("v2 import: %+v %d %s", result, code, diagnostic)
	}
	raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json"))
	var record evmActionRecord
	if err != nil || json.Unmarshal(raw, &record) != nil || record.Schema != "urnetwork-mainnet-evm-vault2-state-v1" || record.PredecessorHash != "" || record.Signed != "0x"+hex.EncodeToString(f.raw) {
		t.Fatalf("v2 journal identity: %+v %v", record, err)
	}
	if _, code, diagnostic := f.command("apply", "--action", "escrow2-register"); code != 3 || !strings.Contains(diagnostic, "action requires the exact retained successful vault2 postconditions") {
		t.Fatalf("escrow registration opened without completed vault2: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, "escrow2-register.json")); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("refused successor left custody or reached the route: %v", err)
	}
}

// SV-8, A7: vault2's apply prints the escrow envelope only from the complete,
// consistent graph. An approval whose action 1 is missing or inconsistent
// refuses apply before vault2's journal opens, an envelope prints or a route
// is read.
func TestEvmV2Vault2ApplyRefusesEscrowFaults(t *testing.T) {
	escrowCall, escrowValue, edges := "vault2 escrow registration must be one exact uint64 call to the derived vault2", "vault2 escrow registration selector, nonzero burn cap or exact wei funding differs", "settlement v2 actions must be one deployer's consecutive nonces with zero-value CREATEs"
	escrow := func(f *evmCreateFixture) *evmPhaseAction { return &f.config.Plan.Actions[1] }
	for _, fault := range []struct {
		name, expected string
		edit           func(*evmCreateFixture)
	}{
		{"missing", "contract phase lacks exact identity, scope or finite bounds", func(f *evmCreateFixture) {
			f.config.Plan.Actions = slices.Delete(slices.Clone(f.config.Plan.Actions), 1, 2)
		}},
		{"renamed", "contract phase action sequence differs", func(f *evmCreateFixture) { escrow(f).Id = "escrow-register" }},
		{"target", escrowCall, func(f *evmCreateFixture) {
			target := crypto.CreateAddress(escrow(f).Sender, 2)
			escrow(f).To = &target
		}},
		{"create", escrowCall, func(f *evmCreateFixture) { escrow(f).To = nil }},
		{"calldata", escrowCall, func(f *evmCreateFixture) { escrow(f).Data = escrow(f).Data[:len(escrow(f).Data)-2] }},
		{"selector", escrowValue, func(f *evmCreateFixture) { escrow(f).Data = "0x" + "00" + escrow(f).Data[4:] }},
		{"zero-cap", escrowValue, func(f *evmCreateFixture) {
			escrow(f).Data, escrow(f).ValueWei = "0x"+hex.EncodeToString(stabi.NewSTSettlementVault2().PackRegisterEscrow(0)), "0"
		}},
		{"value", escrowValue, func(f *evmCreateFixture) { escrow(f).ValueWei = "1999999999" }},
		{"nonce", edges, func(f *evmCreateFixture) { escrow(f).Nonce = 9 }},
		{"sender", edges, func(f *evmCreateFixture) { escrow(f).Sender = common.Address{42} }},
	} {
		f := newEvmV2Fixture(t)
		fault.edit(f)
		f.publishApproval()
		var stdout, stderr bytes.Buffer
		code := runMain(f.storage.Context, []string{"bootstrap-contracts", "apply", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--action", "vault2-create"}, &stdout, &stderr)
		_, journal := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json"))
		if code != 2 || !strings.Contains(stderr.String(), "contract phase authority: "+fault.expected) || stdout.Len() != 0 || !errors.Is(journal, os.ErrNotExist) || len(f.counts) != 0 {
			t.Fatalf("%s: apply printed or opened custody: %d %q %q %v", fault.name, code, stdout.String(), stderr.String(), journal)
		}
	}
}

// SV-8: defensive selection refusals. A projection must be the five-action
// graph and carry its exact predecessors and its prefix's review values.
func TestEvmV2SelectionRefusesInconsistentProjection(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	shape, predecessors, review := "settlement v2 selection differs from its five-action graph", "settlement v2 selection lacks its exact approved predecessors", "settlement v2 selection review values differ from its action"
	for _, fault := range []struct {
		name, expected string
		edit           func(*evmCreatePlan)
	}{
		{"index", shape, func(plan *evmCreatePlan) { plan.ActionIndex = 5 }},
		{"negative-index", shape, func(plan *evmCreatePlan) { plan.ActionIndex = -1 }},
		{"reserve", shape, func(plan *evmCreatePlan) { plan.Reserve = graph[0] }},
		{"original-review", shape, func(plan *evmCreatePlan) { plan.VaultConstructor = &contractVaultConstructor{} }},
		{"missing-predecessor", predecessors, func(plan *evmCreatePlan) { plan.Escrow = nil }},
		{"descendant", predecessors, func(plan *evmCreatePlan) { plan.Proxy, plan.ActionIndex = graph[3], 2 }},
		{"predecessor-index", predecessors, func(plan *evmCreatePlan) { plan.Coordinator = graph[1] }},
		{"predecessor-approval", predecessors, func(plan *evmCreatePlan) {
			changed := *plan.Vault
			changed.Config.Signature = strings.Repeat("00", 64)
			plan.Vault = &changed
		}},
		{"missing-constructor", review, func(plan *evmCreatePlan) { plan.Vault2Constructor = nil }},
		{"missing-binding", review, func(plan *evmCreatePlan) { plan.VaultBinding = nil }},
		{"early-initializer", review, func(plan *evmCreatePlan) {
			*plan = *graph[2]
			plan.Settlement2Initializer = graph[3].Settlement2Initializer
		}},
	} {
		plan := copyEvmCreatePlan(*graph[4])
		fault.edit(&plan)
		if err := plan.validateSelection(); err == nil || err.Error() != fault.expected {
			t.Fatalf("%s projection admitted: %v", fault.name, err)
		}
	}
	for _, plan := range graph {
		if err := plan.validateSelection(); err != nil {
			t.Fatal(err)
		}
	}
}

// SV-8: custody opens only for a v2 selection with its exact predecessor prefix.
func TestEvmV2CustodyRequiresExactPrefix(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	if _, err := openEvmV2ActionStore(*graph[1], nil, true, nil, f.storage.Context); err == nil || err.Error() != "settlement v2 custody requires its exact predecessor prefix" {
		t.Fatalf("escrow custody opened without vault2: %v", err)
	}
	if _, err := openEvmV2ActionStore(*graph[0], []evmActionRecord{{}}, true, nil, f.storage.Context); err == nil || err.Error() != "settlement v2 custody requires its exact predecessor prefix" {
		t.Fatalf("vault2 custody accepted a predecessor: %v", err)
	}
	broken := *graph[0]
	broken.Vault2Constructor = nil
	if _, err := openEvmV2ActionStore(broken, nil, true, nil, f.storage.Context); err == nil || !strings.Contains(err.Error(), "review values differ") {
		t.Fatalf("invalid selection opened custody: %v", err)
	}
	original := newEvmVaultFixture(t)
	if _, err := openEvmV2ActionStore(original.plan, nil, true, nil, original.storage.Context); err == nil || err.Error() != "settlement v2 custody requires its exact predecessor prefix" {
		t.Fatalf("original reserve opened v2 custody: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused custody left a journal: %v", err)
	}
}

// SV-8, A3: installation completes only with the binding's successful
// retained receipt; every earlier or reverted outcome leaves remaining actions.
// The escrow collateral is reported only where a retained receipt read it back.
func TestEvmV2ResultCompletesOnlyAtBinding(t *testing.T) {
	f := newEvmV2Fixture(t)
	graph := f.v2Graph()
	for index, plan := range graph {
		for _, receipt := range []*evmCreateReceipt{nil, {Status: 0}, {Status: 1}} {
			result := evmV2CreateResult(*plan, evmCreateResult{Address: plan.Address.Hex(), Receipt: receipt})
			complete := index == 4 && receipt != nil && receipt.Status == 1
			observation := ""
			if receipt != nil {
				observation = "retained"
			}
			if result.InstallationComplete != complete || result.ActivationReady || result.Address != "" || result.ExecutableAction != evmV2Actions[index].id || len(result.RemainingActions) != 4-index || result.ReceiptObservation != observation || result.Vault2Address != graph[0].Address.Hex() {
				t.Fatalf("action %d result: %+v", index, result)
			}
			if (index >= 2) != (result.Settlement2ImplementationAddress == graph[2].Address.Hex()) || (index >= 3) != (result.Settlement2Address == graph[3].Address.Hex()) {
				t.Fatalf("action %d addresses: %+v", index, result)
			}
			collateral := (index == 1 || index == 4) && receipt != nil && receipt.Status == 1
			if collateral != (result.EscrowCollateral != nil) || collateral && *result.EscrowCollateral != (contractEscrowCollateral{DrainRatio: "0"}) {
				t.Fatalf("action %d escrow collateral report: %+v", index, result.EscrowCollateral)
			}
			// A7: only vault2's results carry the escrow registration's envelope.
			digest, unsigned := f.config.Plan.Actions[1].envelope()
			if index == 0 && (result.EscrowSigningDigest != digest || result.EscrowUnsignedTransaction != unsigned) || index != 0 && (result.EscrowSigningDigest != "" || result.EscrowUnsignedTransaction != "") {
				t.Fatalf("action %d escrow envelope: %+v", index, result)
			}
		}
	}
}
