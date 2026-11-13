// Runtime486 API capability is separate from exact artifact and owner authority.
package crv4

import (
	"encoding/json"
	"testing"
)

// This protocol declaration contains no participant or finalized-block identity.
const runtime486TestVersionReply = `{"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",3],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",1],["0x43580abff6baab45",5],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"authoringVersion":1,"implName":"node-subtensor","implVersion":1,"specName":"node-subtensor","specVersion":486,"stateVersion":1,"systemVersion":1,"transactionVersion":1}`

// The actual old implementation refuses this unchanged consumed API3 shape.
func TestRuntime486SelectiveMetagraphApiAdmission(t *testing.T) {
	if err := validateValidatorProducerRuntimeApis(json.RawMessage(runtime486TestVersionReply)); err != nil {
		t.Fatal("runtime486 consumed API declaration refused", err)
	}
}

// The single reviewed tuple cannot authorize a sibling or altered encoding.
func TestRuntime486SelectiveMetagraphApiRejectsForeignIdentity(t *testing.T) {
	for _, test := range []struct {
		Field string
		Value any
	}{
		{Field: "specVersion", Value: 485}, {Field: "specVersion", Value: 487}, {Field: "specName", Value: "synthetic-other-runtime"}, {Field: "transactionVersion", Value: 2}, {Field: "stateVersion", Value: 2},
	} {
		var value map[string]any
		if err := json.Unmarshal([]byte(runtime486TestVersionReply), &value); err != nil {
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
		if err := json.Unmarshal([]byte(runtime486TestVersionReply), &value); err != nil {
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

// A source capability grants neither artifact catalog membership nor signing.
func TestRuntime486OwnerSourceAndCatalogBoundary(t *testing.T) {
	const source = "d665b0eb4c95809f985ddf0c5a100253b496a74e"
	if spec, ok := ReviewedNativeOwnerSourceSpec(source); !ok || spec != 486 {
		t.Fatalf("exact reviewed source not bound to486: %d %t", spec, ok)
	}
	if actual, ok := ReviewedNativeOwnerSpecSource(486); !ok || actual != source || !ReviewedPostAlphaMigrationSource(source) {
		t.Fatal("reviewed486 source capability missing")
	}
	for _, spec := range []uint32{485, 487} {
		if _, ok := ReviewedNativeOwnerSpecSource(spec); ok {
			t.Fatalf("unreviewed sibling%d inherited source capability", spec)
		}
	}
	if _, ok := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 486, TransactionVersion: 1, StateVersion: 1}); ok {
		t.Fatal("interface review silently became formal artifact qualification")
	}
}
