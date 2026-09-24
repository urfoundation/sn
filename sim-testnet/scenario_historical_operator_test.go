// Historical observations retain the signed auxiliary-provider count. Reading
// that metadata neither changes payout membership nor relaxes hash validation.
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestScenarioObservationRetainsHistoricalAuxiliaryProviderCount(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := testScenarioObservation(cfg, 7)
	observation.Operators = []OperatorObservation{{NoID: 1, LatestArtifactProviders: 5, ExcludedUnconfiguredProviders: 2}}
	observation.ObservationHash = ""
	var err error
	observation.ObservationHash, err = canonicalHashHex(observation)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	history, err := decodeScenarioObservationLog(append(raw, '\n'))
	if err != nil || len(history) != 1 {
		t.Fatalf("historical auxiliary-provider observation rejected: %v", err)
	}
	if history[0].Operators[0].ExcludedUnconfiguredProviders != 2 || history[0].ObservationHash != observation.ObservationHash {
		t.Fatal("historical count or its signed observation identity changed")
	}
	encoded, err := json.Marshal(history[0])
	if err != nil || !bytes.Equal(raw, encoded) {
		t.Fatalf("historical observation did not roundtrip exactly: %v", err)
	}
	altered := bytes.Replace(raw, []byte(`"excluded_unconfigured_providers":2`), []byte(`"excluded_unconfigured_providers":3`), 1)
	if bytes.Equal(raw, altered) {
		t.Fatal("fixture did not retain the count")
	}
	if _, err := decodeScenarioObservationLog(append(altered, '\n')); err == nil {
		t.Fatal("changed provider count retained the prior observation commitment")
	}
	unknown := bytes.Replace(raw, []byte(`"excluded_unconfigured_providers":2`), []byte(`"excluded_unconfigured_providers":2,"unknown_provider_scope":1`), 1)
	if _, err := decodeScenarioObservationLog(append(unknown, '\n')); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unrecognized historical provider field was discarded: %v", err)
	}
	legacy, err := json.Marshal(OperatorObservation{NoID: 1})
	if err != nil || bytes.Contains(legacy, []byte("excluded_unconfigured_providers")) {
		t.Fatalf("legacy observation representation changed: %v", err)
	}
}
