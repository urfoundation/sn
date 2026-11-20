// Integration controls exercise the two independent read-only admission modes
// and prevent either optional journal projection from bypassing the other.
package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/validator"
)

// Each operation selects its original observer, shares the durable allowance
// and retains both start slots; an earlier projection cannot replace a read.
func TestValidatorActivationIntegrationAdmissionModesPreserveScope(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	f.activation.installed()
	before := f.activation.chain.journals(t)
	prior, code, detail := f.activation.command(t.Context(), "status", nil)
	if code != 0 {
		t.Fatal(detail)
	}
	for _, operation := range []string{"admit-stake", "admit-evidence", "admit", "admit-stake"} {
		f.stateLock.Lock()
		calls := f.runtimeCalls
		f.stateLock.Unlock()
		result, code, detail := f.activation.command(t.Context(), operation, nil)
		f.stateLock.Lock()
		reads := f.runtimeCalls - calls
		f.stateLock.Unlock()
		if result.Operations != prior.Operations+1 || result.ActivationReady || f.activation.starts != [2]int{} {
			t.Fatal("admission mode changed its allowance or start authority", operation, code, result, detail)
		}
		switch operation {
		case "admit-stake":
			if code != 0 || result.Status != "admitted-stake-capacity" || result.Readiness == nil || result.Readiness.Stake == nil || result.Readiness.Production != nil || reads != 1 {
				t.Fatal("stake mode did not execute its original native observer", result, code, detail, reads)
			}
		case "admit-evidence":
			if code != 3 || result.Status != "source-refused" || !strings.Contains(detail, "complete approved contract profile") || reads != 0 || !reflect.DeepEqual(result.Readiness, prior.Readiness) {
				t.Fatal("evidence mode borrowed stake scope or lost original contract preflight", result, code, detail, reads)
			}
		case "admit":
			if code != 0 || result.Status != "admitted-process-only" || result.Readiness == nil || result.Readiness.Native == nil || result.Readiness.Production != nil || result.Readiness.Stake != nil || reads != 0 {
				t.Fatal("ordinary admission inherited a retained extended observation", result, code, detail, reads)
			}
		}
		prior = result
	}
	result, code, detail := f.activation.command(t.Context(), "start", nil)
	if code != 3 || result.Status != "activation-authority-unavailable" || result.Operations != prior.Operations || f.activation.starts != [2]int{} || !reflect.DeepEqual(before, f.activation.chain.journals(t)) {
		t.Fatal("integrated modes opened start or changed original custody", code, result, detail)
	}
}

// These synthetic retained bytes test structural journal composition only;
// they do not stand in for a successful operator or deployed-contract read.
func TestValidatorActivationIntegrationValidatesBothRetainedProjections(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	f.activation.installed()
	result, code, detail := f.activation.command(t.Context(), "admit-stake", nil)
	if code != 0 || result.Readiness == nil || result.Readiness.Stake == nil {
		t.Fatal("native stake observation unavailable", code, detail)
	}
	readiness := *result.Readiness
	digest := "sha256:" + strings.Repeat("7", 64)
	canonical := "0x" + strings.Repeat("8", 64)
	production := &validatorActivationProductionReadiness{Schema: validatorActivationProductionSchema, ContractPlanHash: digest, MappingHash: digest, EvmBlock: 1, EvmHash: canonical}
	for i := byte(1); i <= 5; i++ {
		production.Contracts = append(production.Contracts, validatorActivationContractObservation{Address: common.Address{i}, RuntimeHash: canonical, GettersHash: digest, StorageHash: digest})
	}
	for i, unit := range f.activation.approval.Plan.Units {
		production.Validators = append(production.Validators, validator.ProductionBootstrapObservation{
			ConfigHash: unit.Unit.Config.Sha256, DeploymentId: unit.Source.DeploymentId, ValidatorId: unit.Source.ValidatorId, EvmBlock: production.EvmBlock, EvmHash: production.EvmHash,
			Native:    validator.ProductionBootstrapNativePoint{Block: readiness.FinalizedNumber, Hash: common.HexToHash(readiness.FinalizedHash), Epoch: readiness.Native.NativeEpoch, Hotkey: common.HexToHash(readiness.Roles[i].Expected.Hotkey)},
			Operators: []validator.ProductionBootstrapOperatorObservation{{NoId: 1, ActivationHash: canonical, PublishedBlock: 1, ClientId: "synthetic-client", ClientKey: canonical, ClientKeyGeneration: 1, ClientKeyRegistrationHash: canonical, ClientKeyResponseHash: canonical, ObservationNonce: canonical, RootSigner: common.Address{1}.Hex()}},
		})
	}
	production.EvidenceHash = rootObjectHash(*production)
	readiness.Production = production
	if err := readiness.validate(f.activation.approval.Plan); err != nil {
		t.Fatal("structurally complete independent projections refused", err)
	}
	for _, fault := range []string{"production", "stake"} {
		changed := readiness
		if fault == "production" {
			bad := *production
			bad.Schema = "synthetic-other-schema"
			bad.EvidenceHash = ""
			bad.EvidenceHash = rootObjectHash(bad)
			changed.Production = &bad
		} else {
			bad := *readiness.Stake
			bad.AppliedWeightsInfluenceProven = true
			bad.ContentHash = ""
			bad.ContentHash = rootObjectHash(bad)
			changed.Stake = &bad
		}
		if err := changed.validate(f.activation.approval.Plan); err == nil {
			t.Fatal("one retained projection bypassed the other", fault)
		}
	}
	// Each old independent shape remains readable; absence is not new authority.
	for _, projection := range []string{"production", "stake"} {
		independent := readiness
		if projection == "production" {
			independent.Stake = nil
		} else {
			independent.Production = nil
		}
		if err := independent.validate(f.activation.approval.Plan); err != nil {
			t.Fatal("original independent projection no longer reads", projection, err)
		}
	}
}
