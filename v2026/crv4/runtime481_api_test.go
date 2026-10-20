package crv4

import (
	"encoding/json"
	"testing"
)

// Runtime 481 changes the SubnetInfoRuntimeApi declaration from 2 to 3 while
// preserving the consumed selective-metagraph method and complete SCALE layout.
// This is the direct regression for the API2-only production refusal.
func TestRuntime481SelectiveMetagraphApiAdmission(t *testing.T) {
	for _, reply := range []string{runtime479TestVersionReply, runtime481TestVersionReply} {
		if err := validateValidatorProducerRuntimeApis(json.RawMessage(reply)); err != nil {
			t.Fatal("reviewed selective-metagraph declaration halted the producer", err)
		}
	}
}

func runtime481TestVersionWith(t *testing.T, field string, value any) json.RawMessage {
	t.Helper()
	var version map[string]any
	if err := json.Unmarshal([]byte(runtime481TestVersionReply), &version); err != nil {
		t.Fatal(err)
	}
	if value == nil {
		delete(version, field)
	} else {
		version[field] = value
	}
	raw, err := json.Marshal(version)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// API3 is a reviewed 481 tuple, not a grant to unknown versions, alternate
// encoding families, missing identities or ambiguous API declarations.
func TestRuntime481SelectiveMetagraphApiRejectsUnreviewedDeclarations(t *testing.T) {
	for _, test := range []struct {
		name, field string
		value       any
	}{
		{"previous-spec", "specVersion", 480},
		{"future-spec", "specVersion", 482},
		{"other-runtime", "specName", "other-runtime"},
		{"transaction-version", "transactionVersion", 2},
		{"state-version", "stateVersion", 2},
		{"missing-name", "specName", nil},
		{"missing-spec", "specVersion", nil},
		{"missing-transaction", "transactionVersion", nil},
		{"missing-state", "stateVersion", nil},
		{"future-api", "apis", []any{[]any{"0x8375104b299b74c5", 4}}},
		{"string-api", "apis", []any{[]any{"0x8375104b299b74c5", "3"}}},
		{"negative-api", "apis", []any{[]any{"0x8375104b299b74c5", -1}}},
		{"missing-api", "apis", []any{}},
		{"duplicate-api", "apis", []any{[]any{"0x8375104b299b74c5", 3}, []any{"0x8375104b299b74c5", 3}}},
		{"mixed-api", "apis", []any{[]any{"0x8375104b299b74c5", 2}, []any{"0x8375104b299b74c5", 3}}},
		{"malformed-pair", "apis", []any{[]any{"0x8375104b299b74c5", 3, 0}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := runtime481TestVersionWith(t, test.field, test.value)
			if err := validateValidatorProducerRuntimeApis(raw); err == nil {
				t.Fatal("unreviewed API3 identity or declaration admitted", string(raw))
			}
		})
	}
	for _, raw := range []string{
		`{"apis":[["0x8375104b299b74c5",3]]}`,
		`{"specName":"node-subtensor","specVersion":481,"transactionVersion":1,"stateVersion":1,"apis":[],"apis":[["0x8375104b299b74c5",3]]}`,
		`{"specName":"node-subtensor","specVersion":480,"specVersion":481,"transactionVersion":1,"stateVersion":1,"apis":[["0x8375104b299b74c5",3]]}`,
	} {
		if err := validateValidatorProducerRuntimeApis(json.RawMessage(raw)); err == nil {
			t.Fatal("ambiguous or incomplete API3 declaration admitted", raw)
		}
	}
}
