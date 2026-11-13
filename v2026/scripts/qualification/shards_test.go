package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestQualificationShardsPreserveBroadAndFocusedRootUnion(t *testing.T) {
	plan := testPlan(t)
	var outcomes strings.Builder
	for index := 0; index < 147; index++ {
		fmt.Fprintf(&outcomes, "TestSynthetic%03d\tPASS\n", index)
	}
	testWrite(t, plan.Suites[0].Outcomes, outcomes.String())
	plan.Suites[0].RootsPerShard = 4
	expanded, partitions, err := partitionSuites(plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(expanded.Suites) != 37 || len(partitions) != 1 || len(partitions[0].Roots) != 147 || expanded.Limits != plan.Limits {
		t.Fatalf("lost bounded partition or changed budgets: %d/%+v", len(expanded.Suites), partitions)
	}
	counts := map[string]int{}
	for index, suite := range expanded.Suites {
		expected, err := expectedInputs(suite.Outcomes, suite.FailureLiterals)
		if err != nil || len(expected.Roots) == 0 || len(expected.Roots) > 4 || !reflect.DeepEqual(expected.Roots, partitions[0].Shards[index].Roots) {
			t.Fatalf("shard %s changed exact members: %+v %v", suite.Id, expected, err)
		}
		if suite.Mode != plan.Suites[0].Mode || suite.Package != plan.Suites[0].Package || suite.RootsPerShard != 0 {
			t.Fatalf("shard changed execution identity: %+v", suite)
		}
		for _, root := range expected.Roots {
			counts[root]++
		}
	}
	for index, root := range partitions[0].Roots {
		if counts[root] != 1 {
			t.Fatalf("root %d has %d executions", index, counts[root])
		}
	}
	// The 143-root broad and 18-root focused obligations overlap in 14 roots;
	// neither selector can replace the other. Their exact union is 147 roots.
	focused := append(append([]string(nil), partitions[0].Roots[:14]...), partitions[0].Roots[143:]...)
	if len(focused) != 18 {
		t.Fatal("incorrect focused obligation fixture")
	}
	for _, root := range focused {
		if counts[root] != 1 {
			t.Fatal("lost focused-only root from combined qualification")
		}
	}
	for index := 0; index < 143; index++ {
		if counts[fmt.Sprintf("TestSynthetic%03d", index)] != 1 {
			t.Fatal("lost broad root from combined qualification")
		}
	}
}

func TestQualificationShardsKeepDescendantsAndFailureOwnership(t *testing.T) {
	plan := testPlan(t)
	testWrite(t, plan.Suites[0].Outcomes, "TestFirst\tFAIL\nTestFirst/child\tFAIL\nTestSecond\tPASS\nTestSecond/child\tPASS\n")
	testWrite(t, plan.Suites[0].FailureLiterals, "TestFirst\tparent failure\nTestFirst/child\tchild failure\n")
	plan.Suites[0].RootsPerShard = 1
	expanded, _, err := partitionSuites(plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := expectedInputs(expanded.Suites[0].Outcomes, expanded.Suites[0].FailureLiterals)
	if err != nil || len(first.Outcomes) != 2 || len(first.Markers) != 2 || first.Markers["TestFirst/child"] != "child failure" {
		t.Fatalf("lost descendant or causal ownership: %+v %v", first, err)
	}
	second, err := expectedInputs(expanded.Suites[1].Outcomes, expanded.Suites[1].FailureLiterals)
	if err != nil || len(second.Outcomes) != 2 || len(second.Markers) != 0 || second.Outcomes["TestSecond/child"] != "pass" {
		t.Fatalf("descendants crossed root shards: %+v %v", second, err)
	}
}

func TestQualificationShardsRejectInvalidSizeAndIdentityCollision(t *testing.T) {
	for _, fault := range []string{"negative", "oversized", "long-id", "collision"} {
		plan := testPlan(t)
		plan.Suites[0].RootsPerShard = 1
		switch fault {
		case "negative":
			plan.Suites[0].RootsPerShard = -1
		case "oversized":
			plan.Suites[0].RootsPerShard = 1025
		case "long-id":
			plan.Suites[0].Id = strings.Repeat("s", 58)
		case "collision":
			other := plan.Suites[0]
			other.Id, other.Mode, other.RootsPerShard = "normal-s0001", "race", 0
			plan.Suites = append(plan.Suites, other)
		}
		if _, _, err := partitionSuites(plan, t.TempDir()); err == nil {
			t.Errorf("accepted %s", fault)
		}
	}
}

func TestQualificationShardCompletionSurvivesLaterFailureAndCancellation(t *testing.T) {
	nodes := map[string]stage{"build": {}, "suite-a": {Dependencies: []string{"build"}, Binary: "build"}, "suite-b": {Dependencies: []string{"build"}, Binary: "build"}, "suite-c": {Dependencies: []string{"build"}, Binary: "build"}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var lock sync.Mutex
	calls := map[string]int{}
	var checkpoint map[string]stageResult
	results, err := runDAG(ctx, nodes, 1, func(_ context.Context, name string, _ stage) stageResult {
		lock.Lock()
		calls[name]++
		lock.Unlock()
		if name == "suite-b" {
			cancel()
			return stageResult{Status: "failed", Error: "synthetic interrupted shard"}
		}
		return stageResult{Status: "passed", BinarySHA256: "exact parent"}
	}, func(current map[string]stageResult, _ []string, _ int) error {
		checkpoint = map[string]stageResult{}
		for name, result := range current {
			if result.Status == "passed" {
				checkpoint[name] = result
			}
		}
		return nil
	})
	if err != nil || results["suite-b"].Status != "failed" || results["suite-c"].Status != "canceled" || len(checkpoint) != 2 {
		t.Fatalf("unexpected interrupted census: %+v %+v %v", results, checkpoint, err)
	}
	resumed, err := runDAGFrom(t.Context(), nodes, 2, checkpoint, func(_ context.Context, name string, node stage) stageResult {
		lock.Lock()
		defer lock.Unlock()
		calls[name]++
		if node.BinarySHA256 != "exact parent" {
			t.Errorf("resumed body lost parent binary: %+v", node)
		}
		return stageResult{Status: "passed"}
	}, func(map[string]stageResult, []string, int) error { return nil })
	if err != nil || len(resumed) != 4 || calls["build"] != 1 || calls["suite-a"] != 1 || calls["suite-b"] != 2 || calls["suite-c"] != 1 {
		t.Fatalf("resume repeated completed work or lost remaining shards: %+v %v", calls, err)
	}
}

func TestQualificationResumeSchedulerRejectsForeignFailedOrOrphanStage(t *testing.T) {
	nodes := map[string]stage{"build": {}, "suite": {Dependencies: []string{"build"}}}
	for _, retained := range []map[string]stageResult{{"foreign": {Status: "passed"}}, {"build": {Status: "failed"}}, {"suite": {Status: "passed"}}} {
		_, err := runDAGFrom(t.Context(), nodes, 1, retained, func(context.Context, string, stage) stageResult {
			t.Error("invalid checkpoint started a process")
			return stageResult{Status: "passed"}
		}, func(map[string]stageResult, []string, int) error { return nil })
		if err == nil {
			t.Fatalf("accepted incomplete resume dependency: %+v", retained)
		}
	}
}

func TestQualificationResumePlanPreservesBudgetsAndDescendantOutcomes(t *testing.T) {
	for _, fault := range []string{"same", "limit", "mode", "root", "descendant"} {
		plan := testPlan(t)
		current := plan
		current.Suites = append([]suiteSpec(nil), plan.Suites...)
		current.Suites[0].Outcomes = filepath.Join(t.TempDir(), "outcomes.tsv")
		content, err := os.ReadFile(plan.Suites[0].Outcomes)
		if err != nil {
			t.Fatal(err)
		}
		testWrite(t, current.Suites[0].Outcomes, string(content))
		switch fault {
		case "limit":
			current.Limits.TestSeconds++
		case "mode":
			current.Suites[0].Mode = "race"
		case "root":
			testWrite(t, current.Suites[0].Outcomes, "TestFirst\tPASS\n")
		case "descendant":
			testWrite(t, current.Suites[0].Outcomes, "TestFirst\tPASS\nTestFirst/child\tPASS\nTestSecond\tPASS\n")
		}
		err = sameResumePlan(plan, current)
		if (err == nil) != (fault == "same") {
			t.Errorf("%s resume authority: %v", fault, err)
		}
	}
}
