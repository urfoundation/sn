package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func fleetFixture() FleetManifest {
	var coordinator [20]byte
	coordinator[19] = 1
	var fleetID, hotkey [32]byte
	fleetID[31], hotkey[31] = 2, 3
	member := func(id, key byte) FleetMember {
		var m FleetMember
		m.ClientID[15], m.ClientKey[31] = id, key
		return m
	}
	return FleetManifest{Schema: FleetManifestSchema, ChainID: 945, Netuid: 7, Coordinator: coordinator, FleetID: fleetID, Hotkey: hotkey, Generation: 1, Members: []FleetMember{member(2, 12), member(1, 11), member(3, 13)}}
}

func TestFleetManifestCanonicalOrderingAndHash(t *testing.T) {
	a := fleetFixture()
	b := a
	b.Members = append([]FleetMember(nil), a.Members...)
	b.Members[0], b.Members[2] = b.Members[2], b.Members[0]
	ac, err := a.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	bc, err := b.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ac, bc) {
		t.Fatalf("member ordering changed canonical bytes:\n%s\n%s", ac, bc)
	}
	if !json.Valid(ac) || bytes.Contains(ac, []byte("=")) {
		t.Fatalf("canonical manifest is not deterministic hex JSON: %s", ac)
	}
	ah, _ := a.CommitmentHash()
	bh, _ := b.CommitmentHash()
	if ah != bh || ah == ([32]byte{}) {
		t.Fatalf("commitment hash mismatch/zero: %x %x", ah, bh)
	}
}

func TestFleetManifestBindingUsesDistinctClientIDAndKey(t *testing.T) {
	m := fleetFixture()
	b, err := m.Binding(m.Members[0], 4, 12)
	if err != nil {
		t.Fatal(err)
	}
	if b.ClientID != m.Members[0].ClientID || b.ClientKey != m.Members[0].ClientKey || b.ValidFromEpoch != 4 || b.ValidToEpoch != 12 {
		t.Fatalf("bad binding: %+v", b)
	}
	if _, err := m.Binding(FleetMember{}, 4, 12); err == nil {
		t.Fatal("expected non-member rejection")
	}
}

func TestFleetManifestRejectsKeyAndIdentityReuse(t *testing.T) {
	m := fleetFixture()
	m.Members[1].ClientKey = m.Members[0].ClientKey
	if err := m.Validate(); err == nil {
		t.Fatal("expected duplicate key rejection")
	}
	m = fleetFixture()
	m.Members[1].ClientID = m.Members[0].ClientID
	if err := m.Validate(); err == nil {
		t.Fatal("expected duplicate id rejection")
	}
}

func TestParseFleetManifestStrictCanonicalRoundTrip(t *testing.T) {
	m := fleetFixture()
	b, err := m.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFleetManifest(append(b, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := parsed.CommitmentHash(); got != mustFleetHash(t, m) {
		t.Fatalf("roundtrip commitment %x", got)
	}
	bad := append([]byte(nil), b[:len(b)-1]...)
	bad = append(bad, []byte(`,"unknown":true}`)...)
	if _, err := ParseFleetManifest(bad); err == nil {
		t.Fatal("unknown manifest field accepted")
	}
	noncanonical := append([]byte(" \n"), b...)
	if _, err := ParseFleetManifest(noncanonical); err == nil {
		t.Fatal("non-canonical leading whitespace accepted")
	}
	// Whole-input canonical equality already protects this adjacent decoder.
	for _, suffix := range []string{" {", " [", " garbage", " \""} {
		wire := append(append([]byte(nil), b...), suffix...)
		if _, err := ParseFleetManifest(wire); err == nil {
			t.Errorf("malformed trailing JSON %q accepted", suffix)
		}
	}
}

func mustFleetHash(t *testing.T, m FleetManifest) [32]byte {
	t.Helper()
	h, err := m.CommitmentHash()
	if err != nil {
		t.Fatal(err)
	}
	return h
}
