// Decimal unsigned-integer tests lock canonical approval encoding and
// arbitrary-precision arithmetic across the old uint64 boundary.
package main

import (
	"encoding/json"
	"math"
	"testing"
)

// Prove large campaign approvals remain exact JSON numbers while legacy
// machine-sized values retain their byte representation.
func TestDecimalUintJSONPreservesLargeAndLegacyNumbers(t *testing.T) {
	large := DecimalUint("100000000000000000000")
	encoded, err := json.Marshal(large)
	if err != nil || string(encoded) != large.String() {
		t.Fatalf("large decimal JSON = %s, %v", encoded, err)
	}
	var decoded DecimalUint
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != large {
		t.Fatalf("large decimal round trip = %s, %v", decoded, err)
	}
	legacy := decimalUint64(math.MaxUint64)
	encoded, err = json.Marshal(legacy)
	if err != nil || string(encoded) != "18446744073709551615" {
		t.Fatalf("legacy decimal JSON = %s, %v", encoded, err)
	}
	encoded, err = json.Marshal(DecimalUint(""))
	if err != nil || string(encoded) != "0" {
		t.Fatalf("zero decimal JSON = %s, %v", encoded, err)
	}
}

func TestSpendSemanticZeroSurvivesDecimalJSONCanonicalization(t *testing.T) {
	if !spendIsZero(Spend{}) || !spendIsZero(Spend{EVMGasWei: "0"}) {
		t.Fatal("empty and canonical decimal zero spends differ semantically")
	}
	for _, nonzero := range []Spend{
		{TAORao: 1}, {AlphaRao: 1}, {EVMGasWei: "1"}, {Registrations: 1}, {SubnetCreations: 1},
	} {
		if spendIsZero(nonzero) {
			t.Fatalf("nonzero spend was accepted as zero: %+v", nonzero)
		}
	}
}

// Reject every alternate numeric spelling so one approved amount has one
// hash representation.
func TestDecimalUintJSONRejectsNoncanonicalAndUnsafeValues(t *testing.T) {
	values := []string{`"1"`, `-1`, `01`, `1.0`, `1e3`, `+1`, `null`, `[]`, `{}`}
	for _, value := range values {
		var decoded DecimalUint
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			t.Errorf("noncanonical decimal %s was accepted as %s", value, decoded)
		}
	}
}

// Exercise addition, subtraction, rational allocation, and rao rounding at a
// value that the former uint64 representation could not hold.
func TestDecimalUintArithmeticCrossesUint64WithoutLosingOneWei(t *testing.T) {
	large := DecimalUint("100000000000000000000")
	added, err := addDecimalUint(large, decimalUint64(1))
	if err != nil || added != DecimalUint("100000000000000000001") {
		t.Fatalf("large addition = %s, %v", added, err)
	}
	restored, err := subtractDecimalUint(added, decimalUint64(1))
	if err != nil || restored != large {
		t.Fatalf("large subtraction = %s, %v", restored, err)
	}
	third, err := multiplyDivideDecimalUint(large, 1, 3)
	if err != nil || third != DecimalUint("33333333333333333333") {
		t.Fatalf("large rational allocation = %s, %v", third, err)
	}
	rao, err := ceilDivideDecimalUintToUint64(DecimalUint("1000000001"), 1_000_000_000)
	if err != nil || rao != 2 {
		t.Fatalf("ceil wei-to-rao conversion = %d, %v", rao, err)
	}
	if _, err := subtractDecimalUint(decimalUint64(0), decimalUint64(1)); err == nil {
		t.Fatal("decimal underflow was accepted")
	}
}

// Confirm changing the in-memory gas type does not alter a legacy action's
// canonical JSON or intent hash when its value fit uint64.
func TestDecimalUintKeepsLegacyActionIntentHashStable(t *testing.T) {
	type legacySpend struct {
		TAORao          uint64 `json:"tao_rao"`
		AlphaRao        uint64 `json:"alpha_rao"`
		EVMGasWei       uint64 `json:"evm_gas_wei"`
		Registrations   uint32 `json:"registrations"`
		SubnetCreations uint32 `json:"subnet_creations"`
	}
	action := Action{
		ID: "legacy", Kind: "evm-transaction", Target: "target", Description: "legacy action",
		Parameters: map[string]string{"value": "1"}, Spend: Spend{TAORao: 2, AlphaRao: 3, EVMGasWei: decimalUint64(4), Registrations: 5},
		DependsOn: []string{"prior"},
	}
	currentHash, err := actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	legacyHash, err := canonicalHashHex(struct {
		ID, Kind, Target, Description string
		Parameters                    map[string]string
		Spend                         legacySpend
		DependsOn                     []string
	}{
		ID: action.ID, Kind: action.Kind, Target: action.Target, Description: action.Description,
		Parameters: action.Parameters,
		Spend: legacySpend{
			TAORao: action.Spend.TAORao, AlphaRao: action.Spend.AlphaRao, EVMGasWei: 4,
			Registrations: action.Spend.Registrations, SubnetCreations: action.Spend.SubnetCreations,
		},
		DependsOn: action.DependsOn,
	})
	if err != nil || currentHash != legacyHash {
		t.Fatalf("current/legacy action hashes = %s/%s, %v", currentHash, legacyHash, err)
	}
}
