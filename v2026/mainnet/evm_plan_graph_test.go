// Graph checks bound repeated projection work without wall-clock assertions.
// The fixtures select real approved payloads but need no ancestor RPC or send.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Selection uses the actual eight-action fixture and reviewed release bytes.
func evmTestEvidencePlan(t *testing.T) evmCreatePlan {
	t.Helper()
	f := newEvmEvidenceFixture(t)
	plan, err := selectEvmCreatePlan(context.Background(), f.plan, "evidence-create", f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// Walk object identities rather than repeated paths through shared ancestors.
func evmTestPlanNodes(plan *evmCreatePlan) []*evmCreatePlan {
	nodes := []*evmCreatePlan{}
	seen := map[*evmCreatePlan]bool{}
	queue := []*evmCreatePlan{plan}
	for len(queue) != 0 {
		node := queue[0]
		queue = queue[1:]
		if node == nil || seen[node] {
			continue
		}
		seen[node] = true
		nodes = append(nodes, node)
		queue = append(queue, node.Reserve, node.Vault, node.Coordinator, node.Escrow, node.Proxy, node.ReserveLink, node.VaultLink)
	}
	return nodes
}

// Compare exact values at each projection without serializing repeated paths.
// Retained records are deliberately outside an owner's copied runtime graph.
func evmTestProjectionBytes(t *testing.T, plan evmCreatePlan) []byte {
	t.Helper()
	plan.Reserve, plan.Vault, plan.Coordinator, plan.Escrow, plan.Proxy, plan.ReserveLink, plan.VaultLink = nil, nil, nil, nil, nil, nil, nil
	plan.Prerequisites = nil
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Seven separately addressed reserve projections and six shared descendants
// plus the selected root must stay fourteen objects, never 128 expanded paths.
func TestEvmCreatePlanCopyPreservesBoundedGraph(t *testing.T) {
	plan := evmTestEvidencePlan(t)
	nodes := evmTestPlanNodes(&plan)
	if len(nodes) != 14 {
		t.Fatalf("unexpected approved graph objects: %d", len(nodes))
	}
	for _, node := range nodes {
		node.Prerequisites = []evmActionRecord{{TransactionHash: "invocation-only"}}
	}
	copied := copyEvmCreatePlan(plan)
	owned := evmTestPlanNodes(&copied)
	if len(owned) != len(nodes) || copied.Vault != copied.Coordinator.Vault || copied.Proxy != copied.ReserveLink.Proxy || copied.Reserve == copied.Vault.Reserve {
		t.Fatalf("private graph expanded or merged distinct objects: original=%d copied=%d", len(nodes), len(owned))
	}
	for index, node := range owned {
		if node == nodes[index] || len(node.Prerequisites) != 0 || !bytes.Equal(evmTestProjectionBytes(t, *nodes[index]), evmTestProjectionBytes(t, *node)) {
			t.Fatalf("projection %d changed values, retained records or caller ownership", index)
		}
		original, err := nodes[index].Config.Plan.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		current, err := node.Config.Plan.signingBytes()
		if err != nil || !bytes.Equal(original, current) || nodes[index].Config.Plan.hash() != node.Config.Plan.hash() || rootObjectHash(nodes[index].Config) != rootObjectHash(node.Config) {
			t.Fatalf("projection %d changed public approval bytes or hashes: %v", index, err)
		}
	}
}

// Caller edits and a second owner's edits cannot alter the first private graph,
// including action-address pointers, constructor pointers and postcondition slices.
func TestEvmCreatePlanCopyOwnsEveryProjection(t *testing.T) {
	plan := evmTestEvidencePlan(t)
	first, second := copyEvmCreatePlan(plan), copyEvmCreatePlan(plan)
	owned := evmTestPlanNodes(&first)
	before := make([][]byte, len(owned))
	for index, node := range owned {
		before[index] = evmTestProjectionBytes(t, *node)
	}
	for _, source := range []*evmCreatePlan{&plan, &second} {
		for _, node := range evmTestPlanNodes(source) {
			node.Config.Plan.Actions[0].Data = "0xchanged"
			for index := range node.Config.Plan.Actions {
				if node.Config.Plan.Actions[index].To != nil {
					*node.Config.Plan.Actions[index].To = [20]byte{99}
				}
			}
			if len(node.Runtime) != 0 {
				node.Runtime[0] = 99
			}
			if len(node.Getters) != 0 {
				node.Getters[0].Expected = "changed"
			}
			if len(node.Storage) != 0 {
				node.Storage[0].Expected = "changed"
			}
			if node.VaultConstructor != nil {
				node.VaultConstructor.MinimumClaimTtlBlocks = 99
			}
			if node.EscrowRegistration != nil {
				node.EscrowRegistration.FundingWei = "changed"
			}
			if node.ProxyConstructor != nil {
				node.ProxyConstructor.Owner = [20]byte{99}
			}
			if node.ReserveBinding != nil {
				node.ReserveBinding.Recorder = [20]byte{99}
			}
			if node.VaultBinding != nil {
				node.VaultBinding.Coordinator = [20]byte{99}
			}
			if node.EvidenceConstructor != nil {
				node.EvidenceConstructor.DeploymentIdHash = [32]byte{99}
			}
		}
		for index, node := range owned {
			if !bytes.Equal(before[index], evmTestProjectionBytes(t, *node)) {
				t.Fatalf("projection %d aliases another graph's mutable state", index)
			}
		}
		if err := first.validateSelection(); err != nil {
			t.Fatalf("caller mutation changed private selection: %v", err)
		}
	}
}

// Equal action indices are not object identity. A conflicting nested vault
// remains observable after the ordinary top-level vault was already validated.
func TestEvmCreatePlanValidationRejectsDistinctConflictingProjection(t *testing.T) {
	plan := evmTestEvidencePlan(t)
	for _, fault := range []string{"config", "descendant"} {
		candidate := copyEvmCreatePlan(plan)
		coordinator, duplicate := *candidate.Coordinator, *candidate.Vault
		coordinator.Vault = &duplicate
		candidate.Coordinator = &coordinator
		if err := candidate.validateSelection(); err != nil {
			t.Fatalf("equal distinct projection rejected: %v", err)
		}
		expected := "coordinator selection lacks the approved vault prerequisite"
		if fault == "config" {
			duplicate.Config.Signature = strings.Repeat("00", 64)
		} else {
			duplicate.Coordinator = candidate.Coordinator
			expected = "vault selection contains a descendant projection"
		}
		if err := candidate.validateSelection(); err == nil || !strings.Contains(err.Error(), expected) {
			t.Fatalf("distinct %s projection escaped validation: %v", fault, err)
		}
	}
}

// A successful prior invocation grants no authority to a changed config or
// constructor at the same address. Restoring the bytes restores admission.
func TestEvmCreatePlanValidationRechecksMutations(t *testing.T) {
	plan := evmTestEvidencePlan(t)
	if err := plan.validateSelection(); err != nil {
		t.Fatal(err)
	}
	signature := plan.Vault.Config.Signature
	plan.Vault.Config.Signature = strings.Repeat("00", 64)
	if err := plan.validateSelection(); err == nil || !strings.Contains(err.Error(), "approved vault prerequisite") {
		t.Fatalf("prior validation hid changed approval: %v", err)
	}
	plan.Vault.Config.Signature = signature
	if err := plan.validateSelection(); err != nil {
		t.Fatalf("restored approval rejected: %v", err)
	}
	domain := plan.EvidenceConstructor.DeploymentIdHash
	plan.EvidenceConstructor.DeploymentIdHash = [32]byte{99}
	if err := plan.validateSelection(); err == nil || !strings.Contains(err.Error(), "evidence constructor projection differs") {
		t.Fatalf("prior validation hid changed constructor: %v", err)
	}
	plan.EvidenceConstructor.DeploymentIdHash = domain
	if err := plan.validateSelection(); err != nil {
		t.Fatalf("restored constructor rejected: %v", err)
	}
}

// Identical values in a shared graph must need substantially less allocation
// work than 128 distinct objects. The bound compares work, never elapsed time.
func TestEvmCreatePlanValidationBoundsSharedWork(t *testing.T) {
	plan := evmTestEvidencePlan(t)
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var expanded evmCreatePlan
	if err := json.Unmarshal(raw, &expanded); err != nil {
		t.Fatal(err)
	}
	if len(evmTestPlanNodes(&plan)) != 14 || len(evmTestPlanNodes(&expanded)) != 128 {
		t.Fatal("work comparison does not contain the shared and expanded graphs")
	}
	sharedWork := testing.AllocsPerRun(1, func() {
		if err := plan.validateSelection(); err != nil {
			t.Fatal(err)
		}
	})
	expandedWork := testing.AllocsPerRun(1, func() {
		if err := expanded.validateSelection(); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("validation allocations: shared=%.0f expanded=%.0f", sharedWork, expandedWork)
	if sharedWork*2 >= expandedWork {
		t.Fatalf("shared predecessors repeat expanded validation work: shared=%.0f expanded=%.0f", sharedWork, expandedWork)
	}
}
