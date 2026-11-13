// Provider observations distinguish process lifetime, carrier readiness and
// protocol progress. Traffic or HTTP success never establishes a subnet proof.
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

const ProviderProgressSchema = "urnetwork-provider-progress-v1"
const MaxProviderProgressBytes = 2 * 1024 * 1024
const MaxProviderProgressMembers = 4096

type ProviderProgressSource struct {
	Mode       string `json:"mode"`
	ConfigHash string `json:"config_hash"`
}

type ProviderProgress struct {
	Schema     string                   `json:"schema"`
	Source     ProviderProgressSource   `json:"source"`
	InstanceId string                   `json:"instance_id"`
	StartedAt  string                   `json:"started_at"`
	ObservedAt string                   `json:"observed_at"`
	Sequence   uint64                   `json:"sequence"`
	Members    []ProviderMemberProgress `json:"members"`
	Proof      string                   `json:"proof"`
	Settlement string                   `json:"settlement"`
}

// Slot names and client identities are observations, never metric labels.
// Generation changes when a member is replaced inside the same process.
type ProviderMemberProgress struct {
	Slot          string `json:"slot"`
	Generation    uint64 `json:"generation"`
	ClientId      string `json:"client_id,omitempty"`
	Lifecycle     string `json:"lifecycle"`
	Current       bool   `json:"current"`
	Connected     bool   `json:"connected"`
	KeyRegistered bool   `json:"key_registered"`
	Ready         bool   `json:"ready"`
}

func providerHex(value string, size int) bool {
	if len(value) != size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (self ProviderProgressSource) Validate() error {
	if self.Mode != "standalone" && self.Mode != "swarm" || !providerHex(self.ConfigHash, 32) {
		return errors.New("provider source requires a known mode and exact configuration hash")
	}
	return nil
}

func ValidProviderClientId(value string) bool {
	return len(value) == 36 && value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-' && providerHex(strings.ReplaceAll(value, "-", ""), 16)
}

func ValidProviderSlot(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.') {
			return false
		}
	}
	return true
}

func (self ProviderProgress) Validate() error {
	if self.Schema != ProviderProgressSchema || self.Source.Validate() != nil || !providerHex(self.InstanceId, 16) || self.Sequence == 0 || len(self.Members) == 0 || len(self.Members) > MaxProviderProgressMembers {
		return errors.New("provider observation header or census is invalid")
	}
	start, err := time.Parse(time.RFC3339Nano, self.StartedAt)
	if err != nil {
		return err
	}
	observed, err := time.Parse(time.RFC3339Nano, self.ObservedAt)
	if err != nil || start.IsZero() || observed.Before(start) {
		return errors.New("provider observation times are invalid")
	}
	if self.Proof != "unknown" || self.Settlement != "unknown" {
		return errors.New("provider proof and settlement lack a qualified producer hook")
	}
	slots, clients := map[string]bool{}, map[string]bool{}
	for _, member := range self.Members {
		if !ValidProviderSlot(member.Slot) || slots[member.Slot] {
			return errors.New("provider member slots are invalid or repeated")
		}
		slots[member.Slot] = true
		switch member.Lifecycle {
		case "starting", "running", "stopping", "stopped", "failed":
		default:
			return errors.New("provider member lifecycle is unknown")
		}
		if member.ClientId != "" {
			if !ValidProviderClientId(member.ClientId) || clients[member.ClientId] {
				return errors.New("provider client identity is invalid or repeated")
			}
			clients[member.ClientId] = true
		}
		if member.Current && (member.Generation == 0 || member.ClientId == "" || member.Lifecycle != "running") {
			return errors.New("provider current observation lacks its live identity")
		}
		if !member.Current && (member.Connected || member.KeyRegistered || member.Ready) || member.Ready && (!member.Connected || !member.KeyRegistered) {
			return errors.New("provider readiness exceeds observed carrier and current-key facts")
		}
	}
	return nil
}

// Bound the complete response before decoding, then count members before
// appending their structures. Neither a producer count nor a huge array drives
// an unchecked allocation. Duplicate fields and trailing bytes are refused.
func DecodeProviderProgress(raw []byte) (*ProviderProgress, error) {
	if len(raw) == 0 || len(raw) > MaxProviderProgressBytes {
		return nil, errors.New("provider observation exceeds its byte bound")
	}
	if err := ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	var header struct {
		Schema     string                 `json:"schema"`
		Source     ProviderProgressSource `json:"source"`
		InstanceId string                 `json:"instance_id"`
		StartedAt  string                 `json:"started_at"`
		ObservedAt string                 `json:"observed_at"`
		Sequence   uint64                 `json:"sequence"`
		Members    json.RawMessage        `json:"members"`
		Proof      string                 `json:"proof"`
		Settlement string                 `json:"settlement"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("provider observation has trailing JSON")
	}
	value := &ProviderProgress{Schema: header.Schema, Source: header.Source, InstanceId: header.InstanceId, StartedAt: header.StartedAt, ObservedAt: header.ObservedAt, Sequence: header.Sequence, Proof: header.Proof, Settlement: header.Settlement}
	members := json.NewDecoder(bytes.NewReader(header.Members))
	members.DisallowUnknownFields()
	token, err := members.Token()
	if err != nil || token != json.Delim('[') {
		return nil, errors.New("provider members must be a bounded array")
	}
	for members.More() {
		if len(value.Members) == MaxProviderProgressMembers {
			return nil, errors.New("provider member count exceeds its bound")
		}
		var member ProviderMemberProgress
		if err := members.Decode(&member); err != nil {
			return nil, err
		}
		value.Members = append(value.Members, member)
	}
	if _, err := members.Token(); err != nil {
		return nil, err
	}
	if err := members.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("provider members have trailing data")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}
