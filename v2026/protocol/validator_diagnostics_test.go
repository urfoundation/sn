// The optional diagnostic extension requires consumer-first deployment and
// stays operational evidence even at its maximum counter and identity sizes.
package protocol

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// Every domain is explicit; absent extension alone represents an old producer.
func validatorDiagnosticTestValue() ValidatorProgress {
	value := validatorProgressTestValue()
	state := ValidatorDiagnosticState{Outcome: "delivered", Delivered: math.MaxUint64, Dropped: math.MaxUint64, DroppedBytes: math.MaxUint64, Unavailable: math.MaxUint64, LastSuccessAt: value.HeartbeatAt}
	value.Diagnostics = &ValidatorDiagnosticObservation{ObservedAt: value.HeartbeatAt, Startup: state, Steering: state, Progress: state, Operator: state, Runtime: state}
	return value
}

// New readers accept both producer generations without inventing diagnostics.
// The old strict reader rejects the extension, so it must be upgraded first.
func TestValidatorDiagnosticsConsumerFirstWireAndByteBound(t *testing.T) {
	legacy := validatorProgressTestValue()
	raw, err := legacy.Encode()
	if err != nil {
		t.Fatal(err)
	}
	old, err := DecodeValidatorProgress(raw)
	if err != nil || old.Diagnostics != nil {
		t.Fatal("legacy producer gained known output", err)
	}
	value := validatorDiagnosticTestValue()
	value.Source.DeploymentId = strings.Repeat("test", 64)
	raw, err = value.Encode()
	if err != nil || len(raw) > MaxValidatorProgressBytes {
		t.Fatal("bounded extension escaped wire", err)
	}
	decoded, err := DecodeValidatorProgress(raw)
	if err != nil || decoded.Diagnostics.Steering.Delivered != math.MaxUint64 || decoded.Intent != nil {
		t.Fatal("diagnostic counters became protocol progress", err)
	}
	// This is the original strict top-level grammar; its nested types did not
	// change when the optional top-level diagnostic member was added.
	var previous struct {
		Schema      string                          `json:"schema"`
		Source      ValidatorProgressSource         `json:"source"`
		InstanceId  string                          `json:"instance_id"`
		StartedAt   string                          `json:"started_at"`
		HeartbeatAt string                          `json:"heartbeat_at"`
		Publisher   ValidatorPublicationObservation `json:"publisher"`
		Intent      *ValidatorIntentObservation     `json:"intent,omitempty"`
		Native      *ValidatorNativeObservation     `json:"native,omitempty"`
		Settlement  *ValidatorSettlementObservation `json:"settlement,omitempty"`
		Steering    *ValidatorSteeringObservation   `json:"steering,omitempty"`
	}
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.DisallowUnknownFields()
	if err := reader.Decode(&previous); err == nil || !strings.Contains(err.Error(), "diagnostics") {
		t.Fatal("consumer-first compatibility boundary disappeared", err)
	}
}

// Missing acknowledgments, open labels and partial fixed domains are refused.
func TestValidatorDiagnosticsRejectsContradictoryAcknowledgments(t *testing.T) {
	for _, mutate := range []func(*ValidatorProgress){
		func(value *ValidatorProgress) { value.Diagnostics.Steering.LastSuccessAt = "" },
		func(value *ValidatorProgress) { value.Diagnostics.Steering.Delivered = 0 },
		func(value *ValidatorProgress) { value.Diagnostics.Steering.Outcome = "arbitrary-error-text" },
		func(value *ValidatorProgress) { value.Diagnostics.Runtime = ValidatorDiagnosticState{} },
		func(value *ValidatorProgress) { value.Diagnostics.ObservedAt = "" },
	} {
		value := validatorDiagnosticTestValue()
		mutate(&value)
		if _, err := value.Encode(); err == nil {
			t.Fatal("contradictory diagnostic observation admitted")
		}
	}
	value := validatorDiagnosticTestValue()
	copy := value.Diagnostics.States()
	value.Diagnostics.Startup.Delivered = 1
	if copy[0].Delivered != math.MaxUint64 {
		t.Fatal("snapshot retains mutable diagnostic alias")
	}
}
