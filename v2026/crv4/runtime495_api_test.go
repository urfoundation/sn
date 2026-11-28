// Runtime495 API capability is separate from exact artifact and owner authority.
package crv4

import (
	"encoding/json"
	"testing"
)

// This protocol declaration contains no participant or finalized-block identity:
// Finney's state_getRuntimeVersion at block 9578500, the first runtime495 block.
const runtime495TestVersionReply = `{"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",3],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",2],["0x43580abff6baab45",5],["0x292808ec5d0c890b",2],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"authoringVersion":1,"implName":"node-subtensor","implVersion":1,"specName":"node-subtensor","specVersion":495,"stateVersion":1,"systemVersion":1,"transactionVersion":2}`

// The pre-495 implementation refuses this unchanged consumed API3 declaration
// (the 2026-11-24 mainnet upgrade crash-looped the validator on its identity).
func TestRuntime495SelectiveMetagraphApiAdmission(t *testing.T) {
	if err := validateValidatorProducerRuntimeApis(json.RawMessage(runtime495TestVersionReply)); err != nil {
		t.Fatal("runtime495 consumed API declaration refused", err)
	}
}

// Every runtime API keeps runtime494's identity, order and declared version,
// the consumed SubnetInfoRuntimeApi version3 included. Only the spec version
// moves among the identity fields; the transaction version stays 2.
func TestRuntime495DeclaresRuntime494Apis(t *testing.T) {
	var declared494, declared495 struct {
		Apis               [][2]json.RawMessage `json:"apis"`
		SpecName           string               `json:"specName"`
		ImplVersion        uint32               `json:"implVersion"`
		SpecVersion        uint32               `json:"specVersion"`
		TransactionVersion uint32               `json:"transactionVersion"`
		StateVersion       uint32               `json:"stateVersion"`
	}
	if err := json.Unmarshal([]byte(runtime494TestVersionReply), &declared494); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(runtime495TestVersionReply), &declared495); err != nil {
		t.Fatal(err)
	}
	if declared494.SpecVersion != 494 || declared495.SpecVersion != 495 || len(declared494.Apis) != 26 || len(declared494.Apis) != len(declared495.Apis) {
		t.Fatalf("runtime API census differs: %d %d", len(declared494.Apis), len(declared495.Apis))
	}
	if declared494.SpecName != declared495.SpecName || declared494.ImplVersion != declared495.ImplVersion ||
		declared494.TransactionVersion != 2 || declared495.TransactionVersion != 2 || declared494.StateVersion != 1 || declared495.StateVersion != 1 {
		t.Fatalf("runtime495 identity fields differ from the review: impl %d then %d, tx %d then %d, state %d then %d",
			declared494.ImplVersion, declared495.ImplVersion, declared494.TransactionVersion, declared495.TransactionVersion, declared494.StateVersion, declared495.StateVersion)
	}
	subnetInfo := false
	for index := range declared494.Apis {
		if string(declared494.Apis[index][0]) != string(declared495.Apis[index][0]) || string(declared494.Apis[index][1]) != string(declared495.Apis[index][1]) {
			t.Fatalf("runtime API %s differs from runtime494", declared495.Apis[index][0])
		}
		if string(declared495.Apis[index][0]) == `"0x8375104b299b74c5"` {
			if string(declared495.Apis[index][1]) != "3" {
				t.Fatalf("consumed SubnetInfoRuntimeApi declared %s, not version3", declared495.Apis[index][1])
			}
			subnetInfo = true
		}
	}
	if !subnetInfo {
		t.Fatal("runtime API census lacks SubnetInfoRuntimeApi")
	}
}

// The reviewed tuple cannot authorize a sibling or altered encoding: not
// runtime495 at transaction version 1 or 3, and not a neighbouring spec at
// transaction version 2 that has no review of its own.
func TestRuntime495SelectiveMetagraphApiRejectsForeignIdentity(t *testing.T) {
	for _, test := range []struct {
		Field string
		Value any
	}{
		{Field: "specVersion", Value: 493}, {Field: "specVersion", Value: 496}, {Field: "specName", Value: "synthetic-other-runtime"},
		{Field: "transactionVersion", Value: 1}, {Field: "transactionVersion", Value: 3}, {Field: "stateVersion", Value: 2},
	} {
		var value map[string]any
		if err := json.Unmarshal([]byte(runtime495TestVersionReply), &value); err != nil {
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
		if err := json.Unmarshal([]byte(runtime495TestVersionReply), &value); err != nil {
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

// The raised transaction version is reviewed for runtime494 and runtime495
// exactly, and for no identity next to them.
func TestRuntime495RaisedTransactionVersionIsExact(t *testing.T) {
	for _, reviewed := range []RuntimeVersionIdentity{
		{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 2, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 1},
	} {
		if !reviewedRaisedTransactionVersion(reviewed) {
			t.Fatalf("raised transaction version of %+v is not reviewed", reviewed)
		}
	}
	for _, foreign := range []RuntimeVersionIdentity{
		{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 1, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 3, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 496, TransactionVersion: 2, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 493, TransactionVersion: 2, StateVersion: 1},
		{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 2},
		{SpecName: "synthetic-other-runtime", SpecVersion: 495, TransactionVersion: 2, StateVersion: 1},
	} {
		if reviewedRaisedTransactionVersion(foreign) {
			t.Fatalf("unreviewed identity %+v joined the producer encoding family", foreign)
		}
	}
}

// Runtime495 is admitted for the consumed interfaces only: no owner-source
// capability, artifact catalog membership or historical source encoding is
// implied by the interface review.
func TestRuntime495InterfaceGrantsNoSourceOrCatalogAuthority(t *testing.T) {
	if _, ok := ReviewedNativeOwnerSpecSource(495); ok {
		t.Fatal("interface review silently granted runtime495 owner-source capability")
	}
	if _, ok := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 1}); ok {
		t.Fatal("interface review silently became formal artifact qualification")
	}
	if reviewedSourceEncodingVersion(495, 2) {
		t.Fatal("interface review silently joined the historical source encodings")
	}
}
