package main

// Keeps native plan authentication proportional to distinct exact archive
// inputs while preserving lineage, byte ownership and every decoder check.

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Fleet generation authenticates both plans before its challenger receipts
// reach the shared native builder; repeated receipts must reuse those results.
func TestFinalNativeSourceDecodesEachPlanOnce(t *testing.T) {
	archive, evidence, chain, events, priorHash, _ := finalFleetGenerationSourceNamespaceFixture(t)
	decodeCount := 0
	archive.planDecoder = func(data []byte) (*SetupPlan, error) {
		decodeCount++
		return decodePersistedPlanBytes(data)
	}
	source, err := newFinalFleetGenerationSource(archive, evidence, chain, events)
	if err != nil {
		t.Fatal(err)
	}
	action := source.current.Actions[0]
	for range 4 {
		for _, planHash := range []string{evidence.PlanHash, priorHash} {
			plan, current, err := archive.nativeActionPlan(planHash, action.ID, action.IntentHash)
			if err != nil || plan.PlanHash != planHash || current.PlanHash != evidence.PlanHash {
				t.Fatalf("native plan %s = %v, %v, %v", planHash, plan, current, err)
			}
		}
	}
	if decodeCount != 2 {
		t.Fatalf("authenticated %d plan byte streams for two unchanged source plans, want exactly 2", decodeCount)
	}
}

// Identical artifacts in independent reconstructions still undergo their own
// authentication; no result may cross an archive or public verification call.
func TestFinalNativeSourcePlanReuseIsArchiveLocal(t *testing.T) {
	archive, evidence, _, _, priorHash, _ := finalFleetGenerationSourceNamespaceFixture(t)
	for range 2 {
		decodeCount := 0
		separate := &finalSemanticArchive{
			files: archive.files,
			planDecoder: func(data []byte) (*SetupPlan, error) {
				decodeCount++
				return decodePersistedPlanBytes(data)
			},
		}
		if _, _, err := separate.nativeActionPlan(priorHash, "fleet.register.201", "fixture-intent"); err != nil {
			t.Fatal(err)
		}
		if decodeCount != 2 {
			t.Fatalf("independent archive %s authenticated %d plans, want 2", evidence.PlanHash, decodeCount)
		}
	}
}

// A warm lookup must observe in-place corruption, replacement, removal, and
// a different valid plan at the exact path; prior bytes remain privately owned.
func TestFinalNativeSourcePlanReuseRejectsChangedBytes(t *testing.T) {
	archive, evidence, _, _, priorHash, priorPath := finalFleetGenerationSourceNamespaceFixture(t)
	currentPath := "launch-foundation/plan.json"
	currentBytes := bytes.Clone(archive.files[currentPath])
	priorBytes := bytes.Clone(archive.files[priorPath])
	for _, mutation := range []struct {
		name string
		edit func()
		want string
	}{
		{name: "current in place", edit: func() { archive.files[currentPath][0] = '!' }, want: "authenticate current native registration plan"},
		{name: "prior in place", edit: func() { archive.files[priorPath][0] = '!' }, want: "differs from its canonical archive path"},
		{name: "current missing", edit: func() { delete(archive.files, currentPath) }, want: "closed semantic graph is missing launch-foundation/plan.json"},
		{name: "prior missing", edit: func() { delete(archive.files, priorPath) }, want: "authenticate carried native registration plan"},
		{name: "prior path substitution", edit: func() { archive.files[priorPath] = bytes.Clone(currentBytes) }, want: "differs from its canonical archive path"},
		{name: "current lineage substitution", edit: func() { archive.files[currentPath] = bytes.Clone(priorBytes) }, want: "outside the current approved lineage"},
	} {
		archive.files[currentPath] = bytes.Clone(currentBytes)
		archive.files[priorPath] = bytes.Clone(priorBytes)
		if _, _, err := archive.nativeActionPlan(priorHash, "fleet.register.201", "fixture-intent"); err != nil {
			t.Fatalf("%s warm lookup: %v", mutation.name, err)
		}
		mutation.edit()
		requestedHash := priorHash
		if mutation.name == "current lineage substitution" {
			requestedHash = evidence.PlanHash
		}
		if _, _, err := archive.nativeActionPlan(requestedHash, "fleet.register.201", "fixture-intent"); err == nil || !strings.Contains(err.Error(), mutation.want) {
			t.Errorf("%s error = %v, want %s", mutation.name, err, mutation.want)
		}
	}
}

// A byte-different but valid encoding needs fresh authentication even when
// its canonical plan hash and archive path remain identical.
func TestFinalNativeSourcePlanReuseAuthenticatesChangedValidBytes(t *testing.T) {
	archive, _, _, _, priorHash, priorPath := finalFleetGenerationSourceNamespaceFixture(t)
	decodeCount := 0
	archive.planDecoder = func(data []byte) (*SetupPlan, error) {
		decodeCount++
		return decodePersistedPlanBytes(data)
	}
	for index := range 2 {
		if index == 1 {
			archive.files[priorPath] = append([]byte(" \n"), archive.files[priorPath]...)
		}
		if _, _, err := archive.nativeActionPlan(priorHash, "fleet.register.201", "fixture-intent"); err != nil {
			t.Fatalf("valid encoding %d: %v", index, err)
		}
	}
	if decodeCount != 3 {
		t.Fatalf("authenticated %d plan byte streams, want one current plus both exact predecessor encodings", decodeCount)
	}
}

// Fresh hashes cannot launder an invalid approval budget through a warm cache.
func TestFinalNativeSourcePlanReuseRejectsRehashedBudget(t *testing.T) {
	archive, evidence, _, _, _, _ := finalFleetGenerationSourceNamespaceFixture(t)
	if _, _, err := archive.nativeActionPlan(evidence.PlanHash, "fleet.register.201", "fixture-intent"); err != nil {
		t.Fatal(err)
	}
	path := "launch-foundation/plan.json"
	var plan SetupPlan
	if err := json.Unmarshal(archive.files[path], &plan); err != nil {
		t.Fatal(err)
	}
	plan.MaximumSpend.TAORao++
	plan.PlanHash = ""
	var err error
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	archive.files[path], err = json.Marshal(&plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := archive.nativeActionPlan(plan.PlanHash, "fleet.register.201", "fixture-intent"); err == nil || !strings.Contains(err.Error(), "persisted setup plan:") || strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("rehashed invalid budget error = %v", err)
	}
}

// A failed decode cannot populate a successful plan cache or hide its cause.
func TestFinalNativeSourcePlanReuseDoesNotCacheFailure(t *testing.T) {
	archive, evidence, _, _, _, _ := finalFleetGenerationSourceNamespaceFixture(t)
	sentinel := errors.New("interrupted plan decoder")
	decodeCount := 0
	archive.planDecoder = func(data []byte) (*SetupPlan, error) {
		decodeCount++
		if decodeCount == 1 {
			return nil, sentinel
		}
		return decodePersistedPlanBytes(data)
	}
	if _, _, err := archive.nativeActionPlan(evidence.PlanHash, "fleet.register.201", "fixture-intent"); !errors.Is(err, sentinel) {
		t.Fatalf("first decode error = %v, want original cause", err)
	}
	if _, _, err := archive.nativeActionPlan(evidence.PlanHash, "fleet.register.201", "fixture-intent"); err != nil {
		t.Fatalf("retry after failed decode: %v", err)
	}
	if decodeCount != 2 {
		t.Fatalf("decode attempts = %d, want 2", decodeCount)
	}
}
