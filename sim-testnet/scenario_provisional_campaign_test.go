// Provisional full campaigns consume the same explicitly local receipt
// evidence as provisional epoch runs, without changing real workload checks.
package main

import (
	"reflect"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Synthetic observations follow the actual provisional reader's separate
// local fields; their strict independently authenticated counters stay zero.
func provisionalCampaignReceiptsTest(cfg *ResolvedConfig) *scenarioEvaluation {
	current := &ScenarioObservation{}
	for validatorId := 1; validatorId <= cfg.Config.Topology.Validators; validatorId++ {
		finalized, applied := 2, 1
		current.Validators = append(current.Validators, ValidatorObservation{ValidatorID: validatorId, LocalRuntimeIntents: &validatorpkg.ProvisionalIntentObservationV2{
			Scope: "local-runtime-observation", State: "observed", StoreSHA256: "sha256:" + strings.Repeat("51", 32), HandoffSHA256: "sha256:" + strings.Repeat("62", 32),
			RecordedFinalizedIntents: &finalized, RecordedAppliedIntents: &applied,
			Receipts: []validatorpkg.ProvisionalIntentReceiptV2{{Status: "applied", SubnetEpoch: 7, SettlementEpoch: 4, FinalizedBlock: 31, ApplicationBlock: 35}},
		}})
	}
	return &scenarioEvaluation{Cfg: cfg, Current: current}
}

// Both actual phase definitions must select the honest local check and still
// bind exactly the approved five/three epochs, matrix, faults and adversaries.
// Provisional release adds one mandatory precompile continuation completion.
func TestProvisionalFullCampaignSelectsLocalReceiptsWithoutChangingWork(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"release-1.0", "production-soak"} {
		cfg := testResolvedConfig(t)
		strict, err := scenarioDefinitionFor(cfg, phase)
		if err != nil {
			t.Fatal(err)
		}
		strictHash, err := scenarioDefinitionHash(strict)
		if err != nil {
			t.Fatal(err)
		}
		cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}
		provisional, err := scenarioDefinitionFor(cfg, phase)
		if err != nil {
			t.Fatalf("%s provisional matrix cannot bind its real checks: %v", phase, err)
		}
		if provisional.GoalEpochs != strict.GoalEpochs || !reflect.DeepEqual(provisional.Faults, strict.Faults) ||
			provisional.MatrixHash != strict.MatrixHash || provisional.AdversarialMatrixHash != strict.AdversarialMatrixHash {
			t.Fatalf("%s provisional mode changed the approved workload", phase)
		}
		wantAdded := 0
		if phase == "release-1.0" {
			wantAdded = 1
		}
		if len(provisional.Checks) != len(strict.Checks)+wantAdded {
			t.Fatalf("%s provisional checks=%d, want original %d plus %d conformance check", phase, len(provisional.Checks), len(strict.Checks), wantAdded)
		}
		evaluation := provisionalCampaignReceiptsTest(cfg)
		replaced, added := 0, 0
		normalized := provisional
		normalized.Checks = make([]scenarioCheck, 0, len(strict.Checks))
		for _, check := range provisional.Checks {
			if phase == "release-1.0" && check.ID == "precompile_conformance_complete" {
				added++
				current := &ScenarioObservation{}
				conformance := &scenarioEvaluation{Cfg: cfg, Current: current}
				if passed, _ := check.Check(conformance); passed {
					t.Fatal("provisional release accepted absent precompile conformance")
				}
				current.PrecompileConformance = completePrecompileEvidence()
				current.PrecompileConformanceValid = true
				if passed, message := check.Check(conformance); !passed {
					t.Fatalf("provisional release refused complete precompile conformance: %s", message)
				}
				current.PrecompileConformance.Dividend = PrecompileDividendStep{}
				if passed, _ := check.Check(conformance); passed {
					t.Fatal("provisional release accepted incomplete precompile conformance")
				}
				current.PrecompileConformance = completePrecompileEvidence()
				current.PrecompileConformanceError = "synthetic conformance failure"
				if passed, _ := check.Check(conformance); passed {
					t.Fatal("provisional release accepted failed precompile conformance")
				}
				continue
			}
			index := len(normalized.Checks)
			if index >= len(strict.Checks) {
				t.Fatalf("%s added unrelated assertion %s", phase, check.ID)
			}
			normalized.Checks = append(normalized.Checks, check)
			if strict.Checks[index].ID == "validator_intents_finalized" {
				if check.ID != "validator_local_v2_receipts_finalized_and_applied" {
					t.Fatalf("%s retained an impossible strict receipt check", phase)
				}
				if passed, message := check.Check(evaluation); !passed || !strings.Contains(message, "final_acceptance=false") {
					t.Fatalf("%s refused actual local receipt observations: %s", phase, message)
				}
				if passed, _ := strict.Checks[index].Check(evaluation); passed {
					t.Fatal("local receipt evidence filled strict authenticated counters")
				}
				normalized.Checks[index] = strict.Checks[index]
				replaced++
			} else if check.ID != strict.Checks[index].ID {
				t.Fatalf("%s replaced unrelated assertion %s", phase, strict.Checks[index].ID)
			}
			if check.ID == "scenario_matrix_coverage" {
				if passed, message := check.Check(evaluation); !passed || !strings.Contains(message, "strict_intent_replay=unrun final_acceptance=false") {
					t.Fatalf("provisional matrix claimed strict intent replay: %s", message)
				}
			}
		}
		if hash, err := scenarioDefinitionHash(normalized); err != nil || hash != strictHash || replaced != 1 || added != wantAdded {
			t.Fatalf("%s changed the approved checks beyond one local receipt substitution and %d conformance check: %v", phase, wantAdded, err)
		}
		cfg.provisionalResume = nil
		again, err := scenarioDefinitionFor(cfg, phase)
		if err != nil {
			t.Fatal(err)
		}
		if hash, err := scenarioDefinitionHash(again); err != nil || hash != strictHash {
			t.Fatalf("%s provisional construction changed the later strict definition: %v", phase, err)
		}
	}
}

// Unknown, incomplete, promoted or failed observations are never counted as
// operational success, even when unrelated strict counters claim progress.
func TestProvisionalFullCampaignRejectsMissingAndPromotedLocalReceipts(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"release-1.0", "production-soak"} {
		cfg := testResolvedConfig(t)
		cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}
		definition, err := scenarioDefinitionFor(cfg, phase)
		if err != nil {
			t.Fatal(err)
		}
		var localCheck *scenarioCheck
		for index := range definition.Checks {
			if definition.Checks[index].ID == "validator_local_v2_receipts_finalized_and_applied" {
				localCheck = &definition.Checks[index]
			}
		}
		if localCheck == nil {
			t.Fatalf("%s has no explicit local receipt check", phase)
		}
		for _, fault := range []string{"missing", "unknown", "promoted", "hash", "count", "receipts", "error"} {
			evaluation := provisionalCampaignReceiptsTest(cfg)
			value := &evaluation.Current.Validators[len(evaluation.Current.Validators)-1]
			value.FinalizedIntents, value.AppliedIntents = 99, 99
			switch fault {
			case "missing":
				value.LocalRuntimeIntents = nil
			case "unknown":
				value.LocalRuntimeIntents.State = "unknown"
			case "promoted":
				value.LocalRuntimeIntents.FinalAcceptance = true
			case "hash":
				value.LocalRuntimeIntents.HandoffSHA256 = ""
			case "count":
				*value.LocalRuntimeIntents.RecordedAppliedIntents = 0
			case "receipts":
				value.LocalRuntimeIntents.Receipts = nil
			case "error":
				value.Error = "synthetic changed source"
			}
			if passed, _ := localCheck.Check(evaluation); passed {
				t.Fatalf("%s accepted %s local receipt observation", phase, fault)
			}
		}
	}
}
