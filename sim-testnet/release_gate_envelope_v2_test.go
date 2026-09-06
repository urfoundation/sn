package main

import "testing"

// Both public v2 signing boundaries must reach full artifact replay and the
// real sr25519 verifier, while the legacy decoder remains explicitly legacy.
// These source edges supplement, never replace, the real M8 behavioral tests.
func TestProducerGateStateSelectionCompactEnvelopeRetainsReplayAndDomains(t *testing.T) {
	for _, edge := range []struct{ path, caller, callee string }{
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "ownReleaseMeasurementEnvelopeV2Hotkey"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeV2Authority"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "decodeReleaseMeasurementV2Bytes"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "admitReleaseMeasurementEnvelopeV2Storage"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "VerifyReleaseMeasurementArtifactV2"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "releaseMeasurementEnvelopeSigningDigestWithDomain"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "Sign"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "Clone"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeV2Authority"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "releaseMeasurementEnvelopeV2Decision"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "DecodeReleaseMeasurementArtifactV2"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "releaseMeasurementEnvelopeMatchesArtifact"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "Clone"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "DecodeReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeV2"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "validateReleaseMeasurementEnvelopeV2", callee: "admitReleaseMeasurementEnvelopeV2Storage"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "admitReleaseMeasurementEnvelopeV2Storage", callee: "releaseMeasurementV2ControlStorage"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "admitReleaseMeasurementEnvelopeV2Storage", callee: "releaseMeasurementEnvelopeV2WireSize"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "validateReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeFieldsWithHexWork"},
		{path: "../validator/release_measurement_envelope.go", caller: "validateReleaseMeasurementEnvelopeFieldsWithHexWork", callee: "Verify"},
		{path: "../validator/release_measurement_envelope.go", caller: "DecodeReleaseMeasurementEnvelope", callee: "validateReleaseMeasurementEnvelope"},
		{path: "../validator/release_measurement_envelope.go", caller: "validateReleaseMeasurementEnvelope", callee: "validateReleaseMeasurementEnvelopeWithHexWork"},
		{path: "../validator/release_measurement_envelope.go", caller: "validateReleaseMeasurementEnvelopeWithHexWork", callee: "validateReleaseMeasurementEnvelopeFieldsWithHexWork"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] {
			t.Errorf("%s lost required envelope edge %s", edge.caller, edge.callee)
		}
	}
	for _, caller := range []string{"SealReleaseMeasurementEnvelopeV2", "VerifyReleaseMeasurementEnvelopeV2", "DecodeReleaseMeasurementEnvelopeV2"} {
		calls := releaseClosureFunctionCalls(t, "../validator/release_measurement_envelope_v2.go", caller)
		for _, legacy := range []string{"DecodeReleaseMeasurementArtifact", "VerifyReleaseMeasurementArtifact", "DecodeReleaseMeasurementEnvelope", "VerifyReleaseMeasurementEnvelope", "SealReleaseMeasurementEnvelope"} {
			if calls[legacy] {
				t.Errorf("compact boundary %s acquired legacy fallback %s", caller, legacy)
			}
		}
	}
}
