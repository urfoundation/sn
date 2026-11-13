// The optional claim batcher action of the settlement v2 phase: an
// approval's sixth action creates STClaimBatcher after the binding, and it
// completes only once its exact runtime is read back.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// SV-8, CB-7: the sixth action selects from the same plan hash, with every
// review value of the five before it, and only an approval holding it selects
// it, in signed review and in unsigned preview.
func TestEvmV2BatcherIsAnOptionalSixthAction(t *testing.T) {
	f := newEvmV2BatcherFixture(t)
	sender := f.config.Plan.Actions[0].Sender
	output, code, diagnostic := f.v2Plan("batcher-create")
	if code != 0 || output.PlanHash != f.config.Plan.hash() || output.ExecutableAction != "batcher-create" || output.InstallationComplete || rootObjectHash(output.Plan) != rootObjectHash(f.config.Plan) ||
		output.Vault2Constructor == nil || output.EscrowRegistration == nil || output.Settlement2Initializer == nil || output.VaultBinding == nil || *output.VaultBinding != (contractVaultBinding{Vault: crypto.CreateAddress(sender, 0), Coordinator: crypto.CreateAddress(sender, 3)}) {
		t.Fatalf("batcher plan: %+v %d %s", output, code, diagnostic)
	}
	for _, id := range evmV2ActionIds() {
		if output, code, diagnostic := f.v2Plan(id); code != 0 || output.PlanHash != f.config.Plan.hash() || output.ExecutableAction != id {
			t.Fatalf("%s of the six-action plan: %+v %d %s", id, output, code, diagnostic)
		}
	}
	five := newEvmV2Fixture(t)
	if _, code, diagnostic := five.v2Plan("batcher-create"); code != 2 || !strings.Contains(diagnostic, "contract phase selected action: the approved settlement v2 graph has no claim batcher action") {
		t.Fatalf("five-action approval selected the batcher: %d %s", code, diagnostic)
	}
	five.config.Signature = ""
	raw, err := json.Marshal(five.config)
	if err != nil || os.WriteFile(five.configPath, raw, 0600) != nil {
		t.Fatal(err)
	}
	if _, err := loadEvmV2PhasePreview(t.Context(), five.configPath, "batcher-create"); err == nil || err.Error() != "the approved settlement v2 graph has no claim batcher action" {
		t.Fatalf("five-action draft previewed the batcher: %v", err)
	}
	if len(f.counts) != 0 || len(five.counts) != 0 {
		t.Fatal("review reached the route")
	}
}

// SV-8, CB-7: every check of the batcher's envelope refuses its own fault,
// and any fault refuses even the first action.
func TestEvmV2BatcherBuilderRefusesEachCheck(t *testing.T) {
	action := func(f *evmCreateFixture) *evmPhaseAction { return &f.config.Plan.Actions[5] }
	payload := "approved claim batcher differs from the exact argument-free release creation"
	edges := "settlement v2 actions must be one deployer's consecutive nonces with zero-value CREATEs"
	editData := func(f *evmCreateFixture, change func([]byte) []byte) {
		data, err := hex.DecodeString(action(f).Data[2:])
		if err != nil {
			t.Fatal(err)
		}
		action(f).Data = "0x" + hex.EncodeToString(change(data))
	}
	replace := func(f *evmCreateFixture, release contractReleaseArtifacts, change func(*contractReleaseArtifact)) {
		changed := contractReleaseArtifacts{Schema: release.Schema}
		for _, existing := range release.Artifacts {
			if existing.Name == "ClaimBatcher" {
				change(&existing)
			}
			changed.Artifacts = append(changed.Artifacts, existing)
		}
		f.writeV2Release(changed)
	}
	for _, fault := range []struct {
		name, expected string
		edit           func(*evmCreateFixture, contractReleaseArtifacts)
	}{
		{"id", "contract phase action sequence differs", func(f *evmCreateFixture, _ contractReleaseArtifacts) { action(f).Id = "batcher2-create" }},
		{"creation", payload, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, func(data []byte) []byte { data[0] ^= 1; return data })
		}},
		{"arguments", payload, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			editData(f, func(data []byte) []byte { return append(data, make([]byte, 32)...) })
		}},
		{"constructor", payload, func(f *evmCreateFixture, current contractReleaseArtifacts) {
			replace(f, current, func(artifact *contractReleaseArtifact) {
				artifact.Abi = strings.Replace(artifact.Abi, `[{"type":"function"`, `[{"type":"constructor","inputs":[{"name":"vault_","type":"address","internalType":"address"}],"stateMutability":"nonpayable"},{"type":"function"`, 1)
			})
		}},
		{"immutables", "contract immutable census differs", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			replace(f, current, func(artifact *contractReleaseArtifact) { artifact.ImmutableReferences = map[string][]int{"vault": {0}} })
		}},
		{"missing-artifact", "contract release must contain exactly its phase's production artifacts", func(f *evmCreateFixture, current contractReleaseArtifacts) {
			f.writeV2Release(contractReleaseArtifacts{Schema: current.Schema, Artifacts: current.Artifacts[1:]})
		}},
		{"target", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			target := crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 0)
			action(f).To = &target
		}},
		{"value", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			action(f).ValueWei, f.config.Plan.MaximumTotalWei = "1", "2110500001"
		}},
		{"nonce", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) { action(f).Nonce = 6 }},
		{"sender", edges, func(f *evmCreateFixture, _ contractReleaseArtifacts) { action(f).Sender = common.Address{42} }},
		{"journal-alias", "contract phase input aliases a settlement v2 journal", func(f *evmCreateFixture, _ contractReleaseArtifacts) {
			f.configPath = filepath.Join(f.config.Plan.RunDirectory, "batcher-create.json")
		}},
	} {
		f := newEvmV2BatcherFixture(t)
		fault.edit(f, evmV2TestRelease(t))
		f.publishApproval()
		for _, id := range []string{"vault2-create", "batcher-create"} {
			if _, code, diagnostic := f.v2Plan(id); code != 2 || !strings.Contains(diagnostic, "contract phase authority: "+fault.expected) {
				t.Fatalf("%s escaped settlement v2 admission of %s: %d %s", fault.name, id, code, diagnostic)
			}
		}
		if len(f.counts) != 0 {
			t.Fatalf("%s reached the route", fault.name)
		}
	}
}

// SV-8, CB-7: unsigned preview exports the batcher's predicted address and
// exact runtime hash, with no getter or storage word; a five-action draft
// exports none.
func TestEvmV2BatcherPreviewExportsItsRuntime(t *testing.T) {
	for _, six := range []bool{false, true} {
		f := newEvmV2Fixture(t)
		if six {
			f = newEvmV2BatcherFixture(t)
		}
		graph := f.v2Graph()
		f.config.Signature = ""
		raw, err := json.Marshal(f.config)
		if err != nil || os.WriteFile(f.configPath, raw, 0600) != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "preview", "--config", f.configPath, "--action", "vault2-create"}, &stdout, &stderr); code != 0 {
			t.Fatalf("preview: %d %s", code, stderr.String())
		}
		var preview evmV2PhasePreview
		if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
			t.Fatal(err)
		}
		if !six {
			if preview.ClaimBatcher != nil || bytes.Contains(stdout.Bytes(), []byte("claim_batcher")) {
				t.Fatalf("five-action preview exported a batcher: %+v", preview.ClaimBatcher)
			}
			continue
		}
		release := evmV2TestRelease(t)
		runtime := evmV2TestArtifactNamed(t, release, "ClaimBatcher").Runtime
		want := evmV2PreviewContract{Address: crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 5).Hex(), RuntimeHash: crypto.Keccak256Hash(common.FromHex(runtime)).Hex()}
		if preview.ClaimBatcher == nil || rootObjectHash(*preview.ClaimBatcher) != rootObjectHash(want) || graph[5].Address.Hex() != want.Address || len(graph[5].Getters) != 0 || len(graph[5].Storage) != 0 {
			t.Fatalf("batcher preview: %+v, want %+v", preview.ClaimBatcher, want)
		}
	}
}

// SV-8, CB-7: the batcher's selection needs all five completed predecessors
// in order, the binding's review value among them; the binding itself never
// carries a binding projection as its predecessor.
func TestEvmV2BatcherSelectionRefusesInconsistentProjection(t *testing.T) {
	f := newEvmV2BatcherFixture(t)
	graph := f.v2Graph()
	predecessors, review := "settlement v2 selection lacks its exact approved predecessors", "settlement v2 selection review values differ from its action"
	for _, fault := range []struct {
		name, expected string
		plan           *evmCreatePlan
		edit           func(*evmCreatePlan)
	}{
		{"missing-binding-predecessor", predecessors, graph[5], func(plan *evmCreatePlan) { plan.VaultLink = nil }},
		{"binding-predecessor-index", predecessors, graph[5], func(plan *evmCreatePlan) { plan.VaultLink = graph[3] }},
		{"binding-with-binding-predecessor", predecessors, graph[4], func(plan *evmCreatePlan) { plan.VaultLink = graph[4] }},
		{"missing-binding-review", review, graph[5], func(plan *evmCreatePlan) { plan.VaultBinding = nil }},
		{"index", "settlement v2 selection differs from its five-action graph", graph[5], func(plan *evmCreatePlan) { plan.ActionIndex = 6 }},
	} {
		plan := copyEvmCreatePlan(*fault.plan)
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

// SV-8, CB-7: results of the six-action graph list the batcher among the
// remaining actions; the batcher's names its address and, once its receipt
// is retained, the complete installation.
func TestEvmV2BatcherResults(t *testing.T) {
	f := newEvmV2BatcherFixture(t)
	graph := f.v2Graph()
	for index, plan := range graph {
		for _, receipt := range []*evmCreateReceipt{nil, {Status: 0}, {Status: 1}} {
			result := evmV2CreateResult(*plan, evmCreateResult{Receipt: receipt})
			complete := index >= 4 && receipt != nil && receipt.Status == 1
			want := []string{}
			for _, action := range f.config.Plan.Actions[index+1:] {
				want = append(want, action.Id)
			}
			if result.ExecutableAction != f.config.Plan.Actions[index].Id || !slices.Equal(result.RemainingActions, want) || result.InstallationComplete != complete || (index == 5) != (result.ClaimBatcherAddress == graph[5].Address.Hex()) {
				t.Fatalf("action %d result: %+v", index, result)
			}
		}
	}
	if result := evmV2CreateResult(*graph[0], evmCreateResult{}); !slices.Equal(result.RemainingActions, []string{"escrow2-register", "settlement2-create", "proxy2-create", "vault2-link", "batcher-create"}) {
		t.Fatalf("remaining actions: %v", result.RemainingActions)
	}
}

// SV-8, CB-7: on the EVM, the batcher's journal opens only after the binding
// completed, its CREATE completes only once its exact runtime is read back at
// its canonical inclusion, and the installation is complete with it.
func TestEvmV2PhaseCreatesTheClaimBatcherWithReadback(t *testing.T) {
	f := newEvmV2ChainFixtureOf(t, newEvmV2BatcherFixture(t))
	for _, id := range evmV2ActionIds()[:4] {
		f.completeV2Action(id)
	}
	f.selectV2Action("vault2-link")
	f.prepareV2Action("vault2-link")
	f.selectV2Action("batcher-create")
	if _, code, diagnostic := f.command("apply", "--action", "batcher-create"); code != 3 || !strings.Contains(diagnostic, "action requires the exact retained successful vault2 coordinator binding postconditions") {
		t.Fatalf("batcher journal opened before the binding: %d %s", code, diagnostic)
	}
	f.selectV2Action("vault2-link")
	if result, code, diagnostic := f.command("resume", "--action", "vault2-link", "--online", "--submit"); code != 0 || result.Status != "submitted-awaiting-canonical-receipt" {
		t.Fatalf("binding submission: %+v %d %s", result, code, diagnostic)
	}
	link := f.finishV2Action("vault2-link")
	if !link.InstallationComplete || !slices.Equal(link.RemainingActions, []string{"batcher-create"}) {
		t.Fatalf("binding completion: %+v", link)
	}
	f.selectV2Action("batcher-create")
	f.prepareV2Action("batcher-create")
	if result, code, diagnostic := f.command("resume", "--action", "batcher-create", "--online", "--submit"); code != 0 || result.Status != "submitted-awaiting-canonical-receipt" {
		t.Fatalf("batcher submission: %+v %d %s", result, code, diagnostic)
	}
	graph := f.v2Graph()
	block := f.receipt["blockHash"].(string)
	f.override = func(method string, params []any, result any) any {
		if method != "eth_getCode" || len(params) != 2 || common.HexToAddress(params[0].(string)) != graph[5].Address {
			return result
		}
		if selector, ok := params[1].(map[string]any); ok && selector["blockHash"] == block {
			return "0x" + hex.EncodeToString(append(slices.Clone(graph[5].Runtime), 0))
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "batcher-create", "--online"); code != 1 || !strings.Contains(diagnostic, "created contract runtime differs from exact patched release") {
		t.Fatalf("a different batcher runtime completed: %d %s", code, diagnostic)
	}
	raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "batcher-create.json"))
	var record evmActionRecord
	if err != nil || json.Unmarshal(raw, &record) != nil || record.Receipt != nil || record.Schema != "urnetwork-mainnet-evm-batcher-state-v1" {
		t.Fatalf("refused readback retained a receipt: %+v %v", record, err)
	}
	f.override = nil
	batcher := f.finishV2Action("batcher-create")
	sender := f.config.Plan.Actions[0].Sender
	if batcher.Status != "batcher-created" || batcher.ClaimBatcherAddress != crypto.CreateAddress(sender, 5).Hex() || batcher.Receipt.ContractAddress != crypto.CreateAddress(sender, 5).Hex() ||
		batcher.Receipt.RuntimeHash != crypto.Keccak256Hash(graph[5].Runtime).Hex() || !batcher.InstallationComplete || len(batcher.RemainingActions) != 0 || batcher.Vault2Address != crypto.CreateAddress(sender, 0).Hex() {
		t.Fatalf("batcher completion: %+v %+v", batcher, batcher.Receipt)
	}
}
