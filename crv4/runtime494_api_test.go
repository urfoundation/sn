// Runtime494 API capability is separate from exact artifact and owner authority.
package crv4

import (
	"encoding/json"
	"testing"
)

// This protocol declaration contains no participant or finalized-block identity:
// Finney's state_getRuntimeVersion at block 9570596, the first runtime494 block.
const runtime494TestVersionReply = `{"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",3],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",2],["0x43580abff6baab45",5],["0x292808ec5d0c890b",2],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"authoringVersion":1,"implName":"node-subtensor","implVersion":1,"specName":"node-subtensor","specVersion":494,"stateVersion":1,"systemVersion":1,"transactionVersion":2}`

// The pre-494 implementation refuses this unchanged consumed API3 declaration
// (the 2026-11-23 mainnet upgrade crash-looped the validator on its identity).
func TestRuntime494SelectiveMetagraphApiAdmission(t *testing.T) {
	if err := validateValidatorProducerRuntimeApis(json.RawMessage(runtime494TestVersionReply)); err != nil {
		t.Fatal("runtime494 consumed API declaration refused", err)
	}
}

// Every runtime API keeps runtime493's identity, order and declared version,
// except the unrelated SubnetRegistrationRuntimeApi, which moves from version1
// to version2; the consumed SubnetInfoRuntimeApi stays at version3. Only the
// transaction version moves among the identity fields.
func TestRuntime494DeclaresRuntime493ApisExceptSubnetRegistration(t *testing.T) {
	const subnetRegistrationRuntimeApi = `"0xc6886e2f8e598b0a"`
	const subnetInfoRuntimeApi = `"0x8375104b299b74c5"`
	var declared493, declared494 struct {
		Apis               [][2]json.RawMessage `json:"apis"`
		SpecVersion        uint32               `json:"specVersion"`
		TransactionVersion uint32               `json:"transactionVersion"`
		StateVersion       uint32               `json:"stateVersion"`
	}
	if err := json.Unmarshal([]byte(runtime493TestVersionReply), &declared493); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(runtime494TestVersionReply), &declared494); err != nil {
		t.Fatal(err)
	}
	if declared493.SpecVersion != 493 || declared494.SpecVersion != 494 || len(declared493.Apis) != len(declared494.Apis) {
		t.Fatalf("runtime API census differs: %d %d", len(declared493.Apis), len(declared494.Apis))
	}
	if declared493.TransactionVersion != 1 || declared494.TransactionVersion != 2 || declared493.StateVersion != 1 || declared494.StateVersion != 1 {
		t.Fatalf("runtime494 encoding versions differ from the review: tx %d then %d, state %d then %d",
			declared493.TransactionVersion, declared494.TransactionVersion, declared493.StateVersion, declared494.StateVersion)
	}
	registration, subnetInfo := false, false
	for index := range declared493.Apis {
		id, version493, version494 := string(declared494.Apis[index][0]), string(declared493.Apis[index][1]), string(declared494.Apis[index][1])
		if string(declared493.Apis[index][0]) != id {
			t.Fatalf("runtime API %s differs from runtime493", id)
		}
		switch id {
		case subnetRegistrationRuntimeApi:
			if version493 != "1" || version494 != "2" {
				t.Fatalf("SubnetRegistrationRuntimeApi declared %s then %s, not version1 then version2", version493, version494)
			}
			registration = true
		case subnetInfoRuntimeApi:
			if version493 != "3" || version494 != "3" {
				t.Fatalf("consumed SubnetInfoRuntimeApi declared %s then %s, not version3", version493, version494)
			}
			subnetInfo = true
		default:
			if version493 != version494 {
				t.Fatalf("runtime API %s differs from runtime493", id)
			}
		}
	}
	if !registration || !subnetInfo {
		t.Fatalf("runtime API census lacks SubnetRegistrationRuntimeApi=%v or SubnetInfoRuntimeApi=%v", registration, subnetInfo)
	}
}

// The single reviewed tuple cannot authorize a sibling or altered encoding: not
// runtime494 at transaction version 1 or 3, and not a neighbouring spec that
// also raised its transaction version.
func TestRuntime494SelectiveMetagraphApiRejectsForeignIdentity(t *testing.T) {
	for _, test := range []struct {
		Field string
		Value any
	}{
		{Field: "specVersion", Value: 493}, {Field: "specVersion", Value: 496}, {Field: "specName", Value: "synthetic-other-runtime"},
		{Field: "transactionVersion", Value: 1}, {Field: "transactionVersion", Value: 3}, {Field: "stateVersion", Value: 2},
	} {
		var value map[string]any
		if err := json.Unmarshal([]byte(runtime494TestVersionReply), &value); err != nil {
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
		if err := json.Unmarshal([]byte(runtime494TestVersionReply), &value); err != nil {
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

// The raised transaction version is reviewed for exactly one identity.
func TestRuntime494RaisedTransactionVersionIsExact(t *testing.T) {
	reviewed := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 2, StateVersion: 1}
	if !reviewedRaisedTransactionVersion(reviewed) {
		t.Fatal("runtime494 raised transaction version is not reviewed")
	}
	for _, foreign := range []RuntimeVersionIdentity{
		{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 1, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 3, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 493, TransactionVersion: 2, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 496, TransactionVersion: 2, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 2, StateVersion: 2},
		{SpecName: "synthetic-other-runtime", SpecVersion: 494, TransactionVersion: 2, StateVersion: 1},
	} {
		if reviewedRaisedTransactionVersion(foreign) {
			t.Fatalf("unreviewed identity %+v joined the producer encoding family", foreign)
		}
	}
}

// Runtime494 is admitted for the consumed interfaces only: no owner-source
// capability, artifact catalog membership or historical source encoding is
// implied by the interface review.
func TestRuntime494InterfaceGrantsNoSourceOrCatalogAuthority(t *testing.T) {
	if _, ok := ReviewedNativeOwnerSpecSource(494); ok {
		t.Fatal("interface review silently granted runtime494 owner-source capability")
	}
	if _, ok := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 2, StateVersion: 1}); ok {
		t.Fatal("interface review silently became formal artifact qualification")
	}
	if reviewedSourceEncodingVersion(494, 2) {
		t.Fatal("interface review silently joined the historical source encodings")
	}
}
