package main

import (
	"math/big"
	"path/filepath"
	"testing"
	"time"

	validatorpkg "github.com/urfoundation/sn/validator"
)

func TestDishonestDepositActionIsExactAndBounded(t *testing.T) {
	action := dishonestDepositBaseAction(testResolvedConfig(t))
	noID, amount, err := dishonestDepositParameters(action)
	if err != nil || noID != 2 || amount.String() != "5000000000" {
		t.Fatalf("canonical dishonest action = %d/%v, %v", noID, amount, err)
	}
	wrongAmount := action
	wrongAmount.Parameters = cloneStrings(action.Parameters)
	wrongAmount.Parameters["amount_rao"] = "5000000001"
	if _, _, err := dishonestDepositParameters(wrongAmount); err == nil {
		t.Fatal("dishonest action accepted a widened alpha amount")
	}
	wrongOperator := action
	wrongOperator.Parameters = cloneStrings(action.Parameters)
	wrongOperator.Parameters["no_id"] = "1"
	if _, _, err := dishonestDepositParameters(wrongOperator); err == nil {
		t.Fatal("dishonest action accepted another operator")
	}
	wrongEpoch := action
	wrongEpoch.Parameters = cloneStrings(action.Parameters)
	wrongEpoch.Parameters["target_epoch"] = "current"
	if _, _, err := dishonestDepositParameters(wrongEpoch); err == nil {
		t.Fatal("dishonest action accepted an unfenced epoch")
	}
	wrongReserve := action
	wrongReserve.Parameters = cloneStrings(action.Parameters)
	wrongReserve.Parameters["reserve_rounding_allowance_rao"] = "1"
	if _, _, err := dishonestDepositParameters(wrongReserve); err == nil {
		t.Fatal("dishonest action accepted a one-leg reserve allowance")
	}
}

func TestDishonestDepositPublicationActionRetainsRuntimeEnvelope(t *testing.T) {
	action := dishonestDepositBaseAction(testResolvedConfig(t))
	noID, amount, err := dishonestDepositParameters(action)
	if err != nil || noID != 2 || amount.String() != "5000000000" {
		t.Fatalf("publication action = %d/%v, %v", noID, amount, err)
	}
	if action.Parameters["reserve_runtime_share_transitions"] != "2" || action.Parameters["reserve_rounding_allowance_rao"] != "2" {
		t.Fatalf("publication action lost its runtime envelope: %+v", action.Parameters)
	}
}

func TestDishonestDepositCounterUsesConfiguredTransactionAmount(t *testing.T) {
	expected := new(big.Int).SetUint64(dishonestDepositRao)
	if err := verifyDishonestDepositAmount(new(big.Int).Set(expected), expected); err != nil {
		t.Fatalf("configured deposit was rejected: %v", err)
	}
	for _, observed := range []*big.Int{big.NewInt(1), new(big.Int).Add(expected, big.NewInt(1)), nil} {
		if err := verifyDishonestDepositAmount(observed, expected); err == nil {
			t.Errorf("wrong deposit %v was accepted", observed)
		}
	}
}

func TestDishonestDepositPlannedEVMEnvelopeRemainsExactAndParseable(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	action := actionByID(t, plan, dishonestDepositActionID)
	if _, _, err := dishonestDepositParameters(action); err != nil {
		t.Fatalf("planned dishonest-deposit envelope was rejected: %v", err)
	}
	if _, err := decodeHex32("planned deployment manifest hash", action.Parameters[deploymentManifestHashParameter]); err != nil {
		t.Fatal(err)
	}
	delete(action.Parameters, evmMaximumFeePerGasParameter)
	if _, _, err := dishonestDepositParameters(action); err == nil {
		t.Fatal("partial dishonest-deposit EVM envelope was accepted")
	}
}

func TestDishonestDepositRejectsMissingAndMalformedDeploymentBinding(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	action := actionByID(t, plan, dishonestDepositActionID)
	missing := action
	missing.Parameters = cloneStrings(action.Parameters)
	delete(missing.Parameters, deploymentManifestHashParameter)
	if _, _, err := dishonestDepositParameters(missing); err == nil {
		t.Fatal("dishonest-deposit action without a deployment binding was accepted")
	}
	malformed := action
	malformed.Parameters = cloneStrings(action.Parameters)
	malformed.Parameters[deploymentManifestHashParameter] = "0x12"
	if _, _, err := dishonestDepositParameters(malformed); err == nil {
		t.Fatal("dishonest-deposit action with a malformed deployment binding was accepted")
	}
}

func dishonestValidatorIntent(t *testing.T, poolUID uint16) validatorpkg.SteeringIntent {
	t.Helper()
	intent := validatorpkg.SteeringIntent{
		Schema: validatorpkg.SteeringIntentSchema, ValidatorID: 1, Netuid: 521,
		SubnetEpoch: 9, SettlementEpoch: 7, Status: "applied", ApplicationBlock: 100, ApplicationBlockHash: "0xapplication",
		MaskedUIDs: []uint16{4}, UIDs: []uint16{9}, Scores: []validatorpkg.RationalJSON{{Numerator: "1", Denominator: "1"}}, Values: []uint16{65535},
		DepositAudits: []validatorpkg.DepositAudit{
			{NoID: 1, Epoch: 7, SourceEpoch: 6, Status: validatorpkg.DepositAuditCompliant, Compliant: true, Disposition: "pool_weight_eligible", RequiredDepositRao: "10", ObservedDepositRao: "10"},
			{NoID: 2, Epoch: 7, SourceEpoch: 6, Status: validatorpkg.DepositAuditMismatch, Disposition: "zero_pool_weight", RequiredDepositRao: "10000000000", ObservedDepositRao: "5000000000", ArtifactHash: "sha256:artifact"},
		},
	}
	if poolUID == 9 {
		t.Fatal("fixture pool UID collides with the positive head UID")
	}
	hash, err := intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	intent.VectorHash = hash
	return intent
}

func writeDishonestValidatorIntent(t *testing.T, stateDir string, intent validatorpkg.SteeringIntent) {
	t.Helper()
	path := filepath.Join(stateDir, "runtime", "validator-1", "state", "steering-intents.json")
	if err := writePublicJSON(path, map[string]any{"schema": validatorpkg.SteeringIntentSchema, "current": intent, "history": []any{}}); err != nil {
		t.Fatal(err)
	}
}

func TestValidatorDishonestDepositEvidenceRequiresMismatchAndPoolExclusion(t *testing.T) {
	stateDir := t.TempDir()
	intent := dishonestValidatorIntent(t, 22)
	writeDishonestValidatorIntent(t, stateDir, intent)
	evidence, err := validatorDishonestDepositEvidence(stateDir, 1, 7, 2, 22, "5000000000")
	if err != nil || evidence.PoolPresent || evidence.PoolWeight != 0 || evidence.Audit.RequiredDepositRao != "10000000000" {
		t.Fatalf("dishonest deposit evidence = %+v, %v", evidence, err)
	}

	intent.DepositAudits[1].RequiredDepositRao = "5000000000"
	intent.VectorHash, _ = intent.ReconstructedVectorHash()
	writeDishonestValidatorIntent(t, stateDir, intent)
	if _, err := validatorDishonestDepositEvidence(stateDir, 1, 7, 2, 22, "5000000000"); err == nil {
		t.Fatal("an exact deposit was reported as a dishonest mismatch")
	}
}

func TestValidatorDishonestDepositEvidenceRejectsIncludedPoolAndHashDrift(t *testing.T) {
	stateDir := t.TempDir()
	intent := dishonestValidatorIntent(t, 22)
	intent.UIDs = append(intent.UIDs, 22)
	intent.Scores = append(intent.Scores, validatorpkg.RationalJSON{Numerator: "1", Denominator: "1"})
	intent.Values = append(intent.Values, 1)
	intent.VectorHash, _ = intent.ReconstructedVectorHash()
	writeDishonestValidatorIntent(t, stateDir, intent)
	if _, err := validatorDishonestDepositEvidence(stateDir, 1, 7, 2, 22, "5000000000"); err == nil {
		t.Fatal("dishonest pool remained in the applied weight vector")
	}

	intent = dishonestValidatorIntent(t, 22)
	intent.DepositAudits[1].ObservedDepositRao = "5000000001"
	writeDishonestValidatorIntent(t, stateDir, intent)
	if _, err := validatorDishonestDepositEvidence(stateDir, 1, 7, 2, 22, "5000000000"); err == nil {
		t.Fatal("tampered deposit audit retained an old vector hash")
	}
}

func TestValidatorDishonestDepositEvidenceRejectsObsoleteDustLiteral(t *testing.T) {
	stateDir := t.TempDir()
	intent := dishonestValidatorIntent(t, 22)
	writeDishonestValidatorIntent(t, stateDir, intent)
	if _, err := validatorDishonestDepositEvidence(stateDir, 1, 7, 2, 22, "1"); err == nil {
		t.Fatal("configured 5-alpha evidence was accepted as an obsolete one-rao deposit")
	}
}
