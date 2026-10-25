package crv4

import (
	"encoding/json"
	"testing"
)

// Exact public version reply captured at finalized block 9348644.
const runtime484TestVersionReply = `{"specName":"node-subtensor","implName":"node-subtensor","authoringVersion":1,"specVersion":484,"implVersion":1,"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",3],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",1],["0x43580abff6baab45",5],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"transactionVersion":1,"systemVersion":1,"stateVersion":1}`

func TestRuntime484SelectiveMetagraphApiAdmission(t *testing.T) {
	if err := validateValidatorProducerRuntimeApis(json.RawMessage(runtime484TestVersionReply)); err != nil {
		t.Fatal("reviewed runtime484 selective-metagraph declaration halted producer", err)
	}
}

func TestRuntime484SelectiveMetagraphApiRejectsOtherIdentities(t *testing.T) {
	for _, test := range []struct {
		field string
		value any
	}{
		{"specVersion", 485}, {"specName", "other-runtime"}, {"transactionVersion", 2}, {"stateVersion", 2},
	} {
		t.Run(test.field, func(t *testing.T) {
			var v map[string]any
			if err := json.Unmarshal([]byte(runtime484TestVersionReply), &v); err != nil {
				t.Fatal(err)
			}
			v[test.field] = test.value
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateValidatorProducerRuntimeApis(raw); err == nil {
				t.Fatal("unreviewed runtime identity admitted")
			}
		})
	}
}
