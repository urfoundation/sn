package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const FleetManifestSchema = "urnetwork-fleet-manifest-v1"

// FleetMember is one independently keyed UR client in a native head fleet.
// ClientID is the stable 16-byte UR identity; ClientKey is the separate
// Ed25519 verification key. They are never conflated.
type FleetMember struct {
	ClientID  [16]byte `json:"-"`
	ClientKey [32]byte `json:"-"`
}

func decodeFixedHex(name, value string, size int) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(b) != size {
		return nil, fmt.Errorf("%s must be %d-byte lowercase hex", name, size)
	}
	if value != "0x"+hex.EncodeToString(b) {
		return nil, fmt.Errorf("%s is not canonical lowercase 0x hex", name)
	}
	return b, nil
}

// ParseFleetManifest accepts only the canonical public JSON shape and rejects
// unknown fields, duplicate documents and non-canonical hex encodings.
func ParseFleetManifest(raw []byte) (*FleetManifest, error) {
	var j fleetManifestJSON
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return nil, fmt.Errorf("decode fleet manifest: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, errors.New("decode fleet manifest: multiple JSON values")
	}
	m := &FleetManifest{Schema: j.Schema, ChainID: j.ChainID, Netuid: j.Netuid, Generation: j.Generation}
	coordinator, err := decodeFixedHex("coordinator", j.Coordinator, 20)
	if err != nil {
		return nil, err
	}
	copy(m.Coordinator[:], coordinator)
	fleetID, err := decodeFixedHex("fleet_id", j.FleetID, 32)
	if err != nil {
		return nil, err
	}
	copy(m.FleetID[:], fleetID)
	hotkey, err := decodeFixedHex("hotkey", j.Hotkey, 32)
	if err != nil {
		return nil, err
	}
	copy(m.Hotkey[:], hotkey)
	for i, member := range j.Members {
		clientID, err := decodeFixedHex(fmt.Sprintf("members[%d].client_id", i), member.ClientID, 16)
		if err != nil {
			return nil, err
		}
		clientKey, err := decodeFixedHex(fmt.Sprintf("members[%d].client_key", i), member.ClientKey, 32)
		if err != nil {
			return nil, err
		}
		var parsed FleetMember
		copy(parsed.ClientID[:], clientID)
		copy(parsed.ClientKey[:], clientKey)
		m.Members = append(m.Members, parsed)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	canonical, err := m.Canonical()
	if err != nil {
		return nil, err
	}
	candidate := raw
	if bytes.HasSuffix(candidate, []byte("\r\n")) {
		candidate = candidate[:len(candidate)-2]
	} else if bytes.HasSuffix(candidate, []byte("\n")) {
		candidate = candidate[:len(candidate)-1]
	}
	if !bytes.Equal(candidate, canonical) {
		return nil, errors.New("fleet manifest is valid but not canonical (members/order/whitespace differ)")
	}
	return m, nil
}

// FleetManifest is the immutable member-set preimage whose SHA-256 is written
// by the fleet hotkey to Subtensor's commitments pallet. Epoch validity lives
// in each dual-signed FleetBinding, allowing the same committed generation to
// be activated at the next safe coordinator boundary.
type FleetManifest struct {
	Schema      string        `json:"schema"`
	ChainID     uint64        `json:"chain_id"`
	Netuid      uint16        `json:"netuid"`
	Coordinator [20]byte      `json:"-"`
	FleetID     [32]byte      `json:"-"`
	Hotkey      [32]byte      `json:"-"`
	Generation  uint64        `json:"generation"`
	Members     []FleetMember `json:"-"`
}

type fleetManifestJSON struct {
	Schema      string `json:"schema"`
	ChainID     uint64 `json:"chain_id"`
	Netuid      uint16 `json:"netuid"`
	Coordinator string `json:"coordinator"`
	FleetID     string `json:"fleet_id"`
	Hotkey      string `json:"hotkey"`
	Generation  uint64 `json:"generation"`
	Members     []struct {
		ClientID  string `json:"client_id"`
		ClientKey string `json:"client_key"`
	} `json:"members"`
}

func (m FleetManifest) Validate() error {
	if m.Schema != FleetManifestSchema {
		return fmt.Errorf("fleet manifest schema %q, want %q", m.Schema, FleetManifestSchema)
	}
	if m.ChainID == 0 || m.Netuid == 0 || m.Coordinator == ([20]byte{}) || m.FleetID == ([32]byte{}) || m.Hotkey == ([32]byte{}) || m.Generation == 0 {
		return errors.New("fleet manifest contains a zero chain/identity/generation")
	}
	if len(m.Members) == 0 {
		return errors.New("fleet manifest has no members")
	}
	ids := map[[16]byte]bool{}
	keys := map[[32]byte]bool{}
	for _, member := range m.Members {
		if member.ClientID == ([16]byte{}) || member.ClientKey == ([32]byte{}) {
			return errors.New("fleet manifest contains a zero member identity/key")
		}
		if ids[member.ClientID] || keys[member.ClientKey] {
			return errors.New("fleet manifest reuses a client identity/key")
		}
		ids[member.ClientID] = true
		keys[member.ClientKey] = true
	}
	return nil
}

// Canonical returns deterministic JSON with members ordered by ClientID.
// Encoding byte arrays explicitly as lowercase 0x hex removes Go's base64
// default and is shared by public evidence tooling.
func (m FleetManifest) Canonical() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	members := append([]FleetMember(nil), m.Members...)
	sort.Slice(members, func(i, j int) bool {
		return string(members[i].ClientID[:]) < string(members[j].ClientID[:])
	})
	j := fleetManifestJSON{
		Schema:      m.Schema,
		ChainID:     m.ChainID,
		Netuid:      m.Netuid,
		Coordinator: "0x" + hex.EncodeToString(m.Coordinator[:]),
		FleetID:     "0x" + hex.EncodeToString(m.FleetID[:]),
		Hotkey:      "0x" + hex.EncodeToString(m.Hotkey[:]),
		Generation:  m.Generation,
	}
	for _, member := range members {
		j.Members = append(j.Members, struct {
			ClientID  string `json:"client_id"`
			ClientKey string `json:"client_key"`
		}{ClientID: "0x" + hex.EncodeToString(member.ClientID[:]), ClientKey: "0x" + hex.EncodeToString(member.ClientKey[:])})
	}
	return json.Marshal(j)
}

// CommitmentHash is the SHA-256 stored as Data::Sha256 on Subtensor.
func (m FleetManifest) CommitmentHash() ([32]byte, error) {
	b, err := m.Canonical()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// Binding creates the canonical member binding anchored to this manifest.
func (m FleetManifest) Binding(member FleetMember, validFromEpoch, validToEpoch uint64) (FleetBinding, error) {
	hash, err := m.CommitmentHash()
	if err != nil {
		return FleetBinding{}, err
	}
	found := false
	for _, candidate := range m.Members {
		if candidate == member {
			found = true
			break
		}
	}
	if !found {
		return FleetBinding{}, errors.New("fleet binding member is not in committed manifest")
	}
	b := FleetBinding{ChainID: m.ChainID, Netuid: m.Netuid, Coordinator: m.Coordinator, FleetID: m.FleetID, Hotkey: m.Hotkey, ClientID: member.ClientID, ClientKey: member.ClientKey, Generation: m.Generation, ValidFromEpoch: validFromEpoch, ValidToEpoch: validToEpoch, CommitmentHash: hash}
	if err := b.Validate(); err != nil {
		return FleetBinding{}, err
	}
	return b, nil
}
