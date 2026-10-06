// Producer status is bounded operational evidence. Heartbeats, successful
// observations and durable transitions remain separate from chain acceptance.
package protocol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const ValidatorProgressSchema = "urnetwork-validator-progress-v1"
const MaxValidatorProgressBytes = 8 * 1024

// The currently running producer identifies its exact configuration. Retained
// intents carry their own original configuration hash independently below.
type ValidatorProgressSource struct {
	ConfigHash   string `json:"config_hash"`
	DeploymentId string `json:"deployment_id"`
	ValidatorId  uint64 `json:"validator_id"`
	ChainId      uint64 `json:"chain_id"`
	GenesisHash  string `json:"genesis_hash"`
	Netuid       uint16 `json:"netuid"`
}

// No vectors, signatures, credentials, paths, participant lists or raw errors
// belong in this snapshot. Its size is independent of retained history.
type ValidatorProgress struct {
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
	Diagnostics *ValidatorDiagnosticObservation `json:"diagnostics,omitempty"`
}

// A fixed domain census reports only this producer instance's diagnostic
// exporter. These counts never indicate protocol progress or alert delivery.
type ValidatorDiagnosticObservation struct {
	ObservedAt string                   `json:"observed_at"`
	Startup    ValidatorDiagnosticState `json:"startup"`
	Steering   ValidatorDiagnosticState `json:"steering"`
	Progress   ValidatorDiagnosticState `json:"progress"`
	Operator   ValidatorDiagnosticState `json:"operator"`
	Runtime    ValidatorDiagnosticState `json:"runtime"`
}

// Counters saturate and reset with InstanceId. LastSuccessAt acknowledges a
// prior completed sink write; it cannot attest to this status file's own write.
type ValidatorDiagnosticState struct {
	Outcome       string `json:"outcome"`
	Delivered     uint64 `json:"delivered"`
	Dropped       uint64 `json:"dropped"`
	DroppedBytes  uint64 `json:"dropped_bytes"`
	Unavailable   uint64 `json:"unavailable"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
}

// The immutable order supplies bounded metric routing without accepting labels
// from producer JSON. Returned values contain no aliased mutable memory.
func (self ValidatorDiagnosticObservation) States() [5]ValidatorDiagnosticState {
	return [5]ValidatorDiagnosticState{self.Startup, self.Steering, self.Progress, self.Operator, self.Runtime}
}

// The file reports the previous completed publication attempt. Its own
// durability cannot be acknowledged inside the bytes being committed.
type ValidatorPublicationObservation struct {
	Outcome       string `json:"outcome"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
}

// Current is false after restart or a failed observation. An absent Value is
// known empty only after a successful current observation, never by default.
type ValidatorIntentObservation struct {
	ObservedAt    string                   `json:"observed_at"`
	LastSuccessAt string                   `json:"last_success_at,omitempty"`
	Current       bool                     `json:"current"`
	Value         *ValidatorIntentProgress `json:"value,omitempty"`
}

// Only semantic lifecycle changes advance ProgressAt. Pending error updates
// and successful repeated reads cannot reset CreatedAt or ProgressAt.
type ValidatorIntentProgress struct {
	ConfigHash       string `json:"config_hash"`
	VectorHash       string `json:"vector_hash"`
	NativeEpoch      uint64 `json:"native_epoch"`
	SettlementEpoch  uint64 `json:"settlement_epoch"`
	Status           string `json:"status"`
	CreatedAt        string `json:"created_at"`
	ProgressAt       string `json:"progress_at"`
	PreparedAtBlock  uint64 `json:"prepared_at_block"`
	FinalizedBlock   uint64 `json:"finalized_block"`
	RevealBlock      uint64 `json:"reveal_block"`
	ApplicationBlock uint64 `json:"application_block"`
}

// These are the inputs from an already authenticated finalized scheduler read.
// A monitor may report a known boundary; it cannot invent a future chain result.
type ValidatorNativeObservation struct {
	ObservedAt          string `json:"observed_at"`
	LastSuccessAt       string `json:"last_success_at,omitempty"`
	Current             bool   `json:"current"`
	Block               uint64 `json:"block"`
	Epoch               uint64 `json:"epoch"`
	LastEpochBlock      uint64 `json:"last_epoch_block"`
	PendingEpochAt      uint64 `json:"pending_epoch_at"`
	Tempo               uint16 `json:"tempo"`
	BlocksSinceLastStep uint64 `json:"blocks_since_last_step"`
}

// Durable closure and public publication are distinct. A failed publication
// can leave an advanced durable cursor with pending work and no fresh success.
type ValidatorSettlementObservation struct {
	ObservedAt          string `json:"observed_at"`
	LastSuccessAt       string `json:"last_success_at,omitempty"`
	Current             bool   `json:"current"`
	CursorKnown         bool   `json:"cursor_known"`
	Epoch               uint64 `json:"epoch"`
	TargetEpoch         uint64 `json:"target_epoch"`
	PendingPublications uint64 `json:"pending_publications"`
	FirstPendingEpoch   uint64 `json:"first_pending_epoch"`
	ProgressAt          string `json:"progress_at,omitempty"`
}

// The real loop supplies its classified outcome. Unknown scheduler epochs stay
// unknown; a successful receipt search with no result is not completion.
type ValidatorSteeringObservation struct {
	ObservedAt    string `json:"observed_at"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	Current       bool   `json:"current"`
	EpochKnown    bool   `json:"epoch_known"`
	NativeEpoch   uint64 `json:"native_epoch"`
	Outcome       string `json:"outcome"`
}

// Reject ambiguous, oversized or structurally incomplete observations before
// callers use numeric zeroes. Time ordering is evaluated against their clock.
func DecodeValidatorProgress(raw []byte) (*ValidatorProgress, error) {
	if len(raw) == 0 || len(raw) > MaxValidatorProgressBytes {
		return nil, errors.New("validator progress exceeds its byte bound")
	}
	if err := ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value ValidatorProgress
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("validator progress has trailing data")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &value, nil
}

// Encode only the closed bounded wire shape, including a final newline for
// local operators. No validation grants protocol or signing authority.
func (self ValidatorProgress) Encode() ([]byte, error) {
	if err := self.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw)+1 > MaxValidatorProgressBytes {
		return nil, errors.New("validator progress exceeds its byte bound")
	}
	return append(raw, '\n'), nil
}

// Fixed hashes and closed statuses keep source identity and unknown states
// explicit. A future timestamp is retained for the consumer's clock alarm.
func (self ValidatorProgress) Validate() error {
	validTime := func(value string) bool {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		return err == nil && !parsed.IsZero() && len(value) <= 35
	}
	validHash := func(value, prefix string, size int) bool {
		if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+2*size || value != strings.ToLower(value) {
			return false
		}
		raw, err := hex.DecodeString(value[len(prefix):])
		return err == nil && !bytes.Equal(raw, make([]byte, size))
	}
	validObservation := func(observed, success string, current bool) bool {
		return validTime(observed) && (success == "" || validTime(success)) && (!current || success != "")
	}
	if self.Schema != ValidatorProgressSchema || !validHash(self.Source.ConfigHash, "sha256:", 32) ||
		len(self.Source.DeploymentId) == 0 || len(self.Source.DeploymentId) > 256 || self.Source.ValidatorId == 0 ||
		self.Source.ChainId == 0 || self.Source.Netuid == 0 || !validHash(self.Source.GenesisHash, "0x", 32) ||
		!validHash(self.InstanceId, "", 16) || !validTime(self.StartedAt) || !validTime(self.HeartbeatAt) {
		return errors.New("validator progress source or heartbeat is incomplete")
	}
	if self.Publisher.LastSuccessAt != "" && !validTime(self.Publisher.LastSuccessAt) ||
		self.Publisher.Outcome == "published" && self.Publisher.LastSuccessAt == "" {
		return errors.New("validator publication observation is incomplete")
	}
	switch self.Publisher.Outcome {
	case "starting", "published", "retrying":
	default:
		return errors.New("validator publication outcome is unknown")
	}
	if value := self.Intent; value != nil {
		if !validObservation(value.ObservedAt, value.LastSuccessAt, value.Current) {
			return errors.New("validator intent observation is incomplete")
		}
		if intent := value.Value; intent != nil {
			if !validHash(intent.ConfigHash, "sha256:", 32) || !validHash(intent.VectorHash, "0x", 32) ||
				!validTime(intent.CreatedAt) || !validTime(intent.ProgressAt) {
				return errors.New("validator intent progress is incomplete")
			}
			switch intent.Status {
			case "pending", "finalized", "applied", "failed":
			default:
				return errors.New("validator intent status is unknown")
			}
			if (intent.Status == "finalized" || intent.Status == "applied") && (intent.FinalizedBlock == 0 || intent.RevealBlock == 0) ||
				intent.Status == "applied" && intent.ApplicationBlock == 0 {
				return errors.New("validator intent completion lacks its original boundary")
			}
		}
	}
	if value := self.Native; value != nil {
		if !validObservation(value.ObservedAt, value.LastSuccessAt, value.Current) || value.Current && value.Block == 0 {
			return errors.New("validator native observation is incomplete")
		}
	}
	if value := self.Settlement; value != nil {
		if !validObservation(value.ObservedAt, value.LastSuccessAt, value.Current) || value.ProgressAt != "" && !validTime(value.ProgressAt) ||
			value.PendingPublications == 0 && value.FirstPendingEpoch != 0 || value.Current && !value.CursorKnown ||
			!value.CursorKnown && (value.Epoch != 0 || value.PendingPublications != 0 || value.ProgressAt != "") {
			return errors.New("validator settlement observation is incomplete")
		}
	}
	if value := self.Steering; value != nil {
		if !validObservation(value.ObservedAt, value.LastSuccessAt, value.Current) || !value.EpochKnown && value.NativeEpoch != 0 {
			return errors.New("validator steering observation is incomplete")
		}
		switch value.Outcome {
		case "starting", "working", "epoch_wait", "reveal_wait", "receipt_pending", "receipt_transport_wait", "read_wait", "complete", "hard_error":
		default:
			return errors.New("validator steering outcome is unknown")
		}
	}
	if value := self.Diagnostics; value != nil {
		if !validTime(value.ObservedAt) {
			return errors.New("validator diagnostic observation time is invalid")
		}
		for _, state := range value.States() {
			if state.LastSuccessAt != "" && !validTime(state.LastSuccessAt) || (state.LastSuccessAt == "") != (state.Delivered == 0) || state.Outcome == "delivered" && state.Delivered == 0 {
				return errors.New("validator diagnostic acknowledgment is incomplete")
			}
			switch state.Outcome {
			case "starting", "delivered", "retrying", "unavailable":
			default:
				return errors.New("validator diagnostic outcome is unknown")
			}
		}
	}
	return nil
}
