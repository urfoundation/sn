// Bounded operational records preserve unknown states and reject ambiguous
// inputs before a consumer considers their numbers or timestamps.
package protocol

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// A synthetic source has no relation to a deployed validator or chain.
func validatorProgressTestValue() ValidatorProgress {
	return ValidatorProgress{Schema: ValidatorProgressSchema,
		Source: ValidatorProgressSource{ConfigHash: "sha256:" + strings.Repeat("1", 64), DeploymentId: "synthetic-progress",
			ValidatorId: 1, ChainId: 964, GenesisHash: "0x" + strings.Repeat("2", 64), Netuid: 25},
		InstanceId: strings.Repeat("3", 32), StartedAt: "2026-01-01T00:00:00Z", HeartbeatAt: "2026-01-01T00:00:01Z",
		Publisher: ValidatorPublicationObservation{Outcome: "starting"}}
}

// Fresh heartbeat alone leaves every protocol domain unobserved. A successful
// empty intent read is explicit and remains distinct from startup or failure.
func TestValidatorProgressPreservesUnknownAndKnownEmpty(t *testing.T) {
	value := validatorProgressTestValue()
	raw, err := value.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeValidatorProgress(raw)
	if err != nil || decoded.Intent != nil || decoded.Native != nil || decoded.Settlement != nil {
		t.Fatal("heartbeat fabricated protocol evidence", err)
	}
	value.Intent = &ValidatorIntentObservation{ObservedAt: value.HeartbeatAt, LastSuccessAt: value.HeartbeatAt, Current: true}
	raw, err = value.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeValidatorProgress(raw)
	if err != nil || decoded.Intent == nil || !decoded.Intent.Current || decoded.Intent.Value != nil {
		t.Fatal("known empty intent lost its successful observation", err)
	}
	value.Intent.Current, value.Intent.LastSuccessAt = false, ""
	if _, err := value.Encode(); err != nil {
		t.Fatal("an initial failed observation cannot be retained", err)
	}
}

// None of the unsupported encodings can become a successful zero-value sample.
func TestValidatorProgressRejectsAmbiguousAndOversizedWire(t *testing.T) {
	raw, err := validatorProgressTestValue().Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range [][]byte{
		append(bytes.TrimSpace(raw), []byte(" {}")...),
		bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"other","schema":`), 1),
		bytes.Replace(raw, []byte(`"schema":`), []byte(`"unexpected":0,"schema":`), 1),
		bytes.Repeat([]byte(" "), MaxValidatorProgressBytes+1),
	} {
		if value, err := DecodeValidatorProgress(candidate); err == nil || value != nil {
			t.Fatal("ambiguous or oversized wire became an observation")
		}
	}
}

// Explicit completion requires its original boundaries; missing data never
// becomes an implicit finalized or applied result.
func TestValidatorProgressRequiresCompleteObservedBoundaries(t *testing.T) {
	for _, mutate := range []func(*ValidatorProgress){
		func(value *ValidatorProgress) {
			value.Intent = &ValidatorIntentObservation{ObservedAt: value.HeartbeatAt, Current: true}
		},
		func(value *ValidatorProgress) {
			value.Native = &ValidatorNativeObservation{ObservedAt: value.HeartbeatAt, LastSuccessAt: value.HeartbeatAt, Current: true}
		},
		func(value *ValidatorProgress) {
			value.Steering = &ValidatorSteeringObservation{ObservedAt: value.HeartbeatAt, Outcome: "successful-looking-unrecognized-state"}
		},
		func(value *ValidatorProgress) {
			value.Steering = &ValidatorSteeringObservation{ObservedAt: value.HeartbeatAt, NativeEpoch: 9, Outcome: "read_wait"}
		},
		func(value *ValidatorProgress) {
			value.Settlement = &ValidatorSettlementObservation{ObservedAt: value.HeartbeatAt, LastSuccessAt: value.HeartbeatAt, Current: true}
		},
		func(value *ValidatorProgress) { value.Publisher.Outcome = "published" },
	} {
		value := validatorProgressTestValue()
		mutate(&value)
		if _, err := value.Encode(); err == nil {
			t.Fatal("incomplete observation became usable wire")
		}
	}
}

// The wire must not erase future evidence on clock rollback. Freshness and
// rollback are decisions for the independently clocked consumer.
func TestValidatorProgressRetainsClockRollbackForConsumer(t *testing.T) {
	value := validatorProgressTestValue()
	value.HeartbeatAt = time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC).Format(time.RFC3339Nano)
	raw, err := value.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeValidatorProgress(raw)
	if err != nil || decoded.StartedAt != value.StartedAt || decoded.HeartbeatAt != value.HeartbeatAt {
		t.Fatal("clock rollback was hidden", err)
	}
}
