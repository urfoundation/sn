// An executor's fast action lookup belongs to one immutable plan object.
// Historical reader copies must resolve dependencies from their source plan.
package main

import "testing"

// Use the actual copied-executor shape shared by fleet, conviction, repair and
// evidence readers. A matching claimed hash is insufficient to share an index.
func TestExecutorPlanActionIndexFollowsHistoricalSource(t *testing.T) {
	activeAction := Action{ID: "synthetic.binding", Target: "synthetic-current-target", IntentHash: "synthetic-current-intent"}
	sourceAction := Action{ID: activeAction.ID, Target: "synthetic-source-target", IntentHash: "synthetic-source-intent"}
	currentOnly := Action{ID: "synthetic.current-only", Target: "synthetic-current-only-target"}
	active := &SetupPlan{PlanHash: "synthetic-same-claimed-hash", Actions: []Action{activeAction, currentOnly}}
	source := &SetupPlan{PlanHash: active.PlanHash, Actions: []Action{sourceAction}}
	executor := &Executor{plan: active, planActions: planActionIndex(active), planActionsOwner: active}
	historical := *executor
	historical.plan = source
	got, err := historical.planAction(sourceAction.ID)
	if err != nil || got.Target != sourceAction.Target || got.IntentHash != sourceAction.IntentHash {
		t.Fatalf("source reader reused active approval index: action=%+v err=%v", got, err)
	}
	if _, err := historical.planAction(currentOnly.ID); err == nil {
		t.Fatal("source reader acquired an action existing only in the active approval")
	}
	got, err = executor.planAction(activeAction.ID)
	if err != nil || got.Target != activeAction.Target || got.IntentHash != activeAction.IntentHash {
		t.Fatalf("historical lookup changed active lookup: action=%+v err=%v", got, err)
	}
}

// Missing ownership metadata cannot turn a supplied index into plan authority;
// ordinary literals still use the canonical first action from their plan.
func TestExecutorPlanActionIndexWithoutOwnerUsesPlan(t *testing.T) {
	first := Action{ID: "synthetic.action", Target: "synthetic-first"}
	second := Action{ID: first.ID, Target: "synthetic-second"}
	executor := &Executor{plan: &SetupPlan{Actions: []Action{first, second}}, planActions: map[string]Action{first.ID: second}}
	got, err := executor.planAction(first.ID)
	if err != nil || got.Target != first.Target {
		t.Fatalf("unowned index replaced canonical action: action=%+v err=%v", got, err)
	}
}
