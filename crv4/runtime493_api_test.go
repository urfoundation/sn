// Runtime493 API capability is separate from exact artifact and owner authority.
package crv4

import (
	"encoding/json"
	"testing"
)

// This protocol declaration contains no participant or finalized-block identity:
// Finney's state_getRuntimeVersion at block 9549412, the first runtime493 block.
const runtime493TestVersionReply = `{"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",3],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",1],["0x43580abff6baab45",5],["0x292808ec5d0c890b",2],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"authoringVersion":1,"implName":"node-subtensor","implVersion":1,"specName":"node-subtensor","specVersion":493,"stateVersion":1,"systemVersion":1,"transactionVersion":1}`

// The pre-493 implementation refuses this unchanged consumed API3 declaration
// (the 2026-11-20 mainnet upgrade crash-looped the validator on exactly this).
func TestRuntime493SelectiveMetagraphApiAdmission(t *testing.T) {
	if err := validateValidatorProducerRuntimeApis(json.RawMessage(runtime493TestVersionReply)); err != nil {
		t.Fatal("runtime493 consumed API declaration refused", err)
	}
}

// Every runtime API keeps runtime492's identity, order and declared version,
// except the unrelated GammaRuntimeApi, which moves from version1 to version2;
// the consumed SubnetInfoRuntimeApi stays at version3.
func TestRuntime493DeclaresRuntime492ApisExceptGamma(t *testing.T) {
	const gammaRuntimeApi = `"0x292808ec5d0c890b"`
	const subnetInfoRuntimeApi = `"0x8375104b299b74c5"`
	var declared492, declared493 struct {
		Apis        [][2]json.RawMessage `json:"apis"`
		SpecVersion uint32               `json:"specVersion"`
	}
	if err := json.Unmarshal([]byte(runtime492TestVersionReply), &declared492); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(runtime493TestVersionReply), &declared493); err != nil {
		t.Fatal(err)
	}
	if declared492.SpecVersion != 492 || declared493.SpecVersion != 493 || len(declared492.Apis) != len(declared493.Apis) {
		t.Fatalf("runtime API census differs: %d %d", len(declared492.Apis), len(declared493.Apis))
	}
	gamma, subnetInfo := false, false
	for index := range declared492.Apis {
		id, version492, version493 := string(declared493.Apis[index][0]), string(declared492.Apis[index][1]), string(declared493.Apis[index][1])
		if string(declared492.Apis[index][0]) != id {
			t.Fatalf("runtime API %s differs from runtime492", id)
		}
		switch id {
		case gammaRuntimeApi:
			if version492 != "1" || version493 != "2" {
				t.Fatalf("GammaRuntimeApi declared %s then %s, not version1 then version2", version492, version493)
			}
			gamma = true
		case subnetInfoRuntimeApi:
			if version492 != "3" || version493 != "3" {
				t.Fatalf("consumed SubnetInfoRuntimeApi declared %s then %s, not version3", version492, version493)
			}
			subnetInfo = true
		default:
			if version492 != version493 {
				t.Fatalf("runtime API %s differs from runtime492", id)
			}
		}
	}
	if !gamma || !subnetInfo {
		t.Fatalf("runtime API census lacks GammaRuntimeApi=%v or SubnetInfoRuntimeApi=%v", gamma, subnetInfo)
	}
}

// The single reviewed tuple cannot authorize a sibling or altered encoding.
func TestRuntime493SelectiveMetagraphApiRejectsForeignIdentity(t *testing.T) {
	for _, test := range []struct {
		Field string
		Value any
	}{
		{Field: "specVersion", Value: 489}, {Field: "specVersion", Value: 494}, {Field: "specName", Value: "synthetic-other-runtime"}, {Field: "transactionVersion", Value: 2}, {Field: "stateVersion", Value: 2},
	} {
		var value map[string]any
		if err := json.Unmarshal([]byte(runtime493TestVersionReply), &value); err != nil {
			t.Fatal(err)
		}
		value[test.Field] = test.Value
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateValidatorProducerRuntimeApis(raw); err == nil {
			t.Fatalf("foreign %s=%v accepted", test.Field, test.Value)
		}
	}
	for _, fault := range []string{"missing", "duplicate", "version"} {
		var value map[string]any
		if err := json.Unmarshal([]byte(runtime493TestVersionReply), &value); err != nil {
			t.Fatal(err)
		}
		apis := value["apis"].([]any)
		switch fault {
		case "missing":
			value["apis"] = []any{}
		case "duplicate":
			for _, raw := range apis {
				pair := raw.([]any)
				if pair[0] == "0x8375104b299b74c5" {
					value["apis"] = append(apis, pair)
					break
				}
			}
		case "version":
			for _, raw := range apis {
				pair := raw.([]any)
				if pair[0] == "0x8375104b299b74c5" {
					pair[1] = float64(4)
					break
				}
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateValidatorProducerRuntimeApis(raw); err == nil {
			t.Fatalf("%s API declaration accepted", fault)
		}
	}
}

// Runtime493 is admitted for the consumed API3 interface only: no owner-source
// capability or artifact catalog membership is implied by the interface review.
func TestRuntime493InterfaceGrantsNoSourceOrCatalogAuthority(t *testing.T) {
	if _, ok := ReviewedNativeOwnerSpecSource(493); ok {
		t.Fatal("interface review silently granted runtime493 owner-source capability")
	}
	if _, ok := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 493, TransactionVersion: 1, StateVersion: 1}); ok {
		t.Fatal("interface review silently became formal artifact qualification")
	}
}
