package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func providerProgressTestValue() ProviderProgress {
	return ProviderProgress{Schema: ProviderProgressSchema, Source: ProviderProgressSource{Mode: "swarm", ConfigHash: strings.Repeat("1", 64)}, InstanceId: strings.Repeat("2", 32), StartedAt: "2026-10-02T00:00:00Z", ObservedAt: "2026-10-02T00:01:00Z", Sequence: 1, Members: []ProviderMemberProgress{{Slot: "member-a", Generation: 1, ClientId: "11111111-1111-1111-1111-111111111111", Lifecycle: "running", Current: true, Connected: true, KeyRegistered: true, Ready: true}}, Proof: "unknown", Settlement: "unknown"}
}

func TestProviderProgressProtocolRoundTripRetainsUnknownProtocolWork(t *testing.T) {
	value := providerProgressTestValue()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeProviderProgress(raw)
	if err != nil || decoded.Members[0] != value.Members[0] || decoded.Proof != "unknown" || decoded.Settlement != "unknown" {
		t.Fatal("provider observation changed provenance", decoded, err)
	}
}

func TestProviderProgressProtocolRejectsUnqualifiedProofAndReadiness(t *testing.T) {
	value := providerProgressTestValue()
	value.Proof = "complete"
	if value.Validate() == nil {
		t.Fatal("traffic could impersonate a completed subnet proof")
	}
	value = providerProgressTestValue()
	value.Members[0].KeyRegistered = false
	if value.Validate() == nil {
		t.Fatal("readiness ignored current-key registration")
	}
	value = providerProgressTestValue()
	value.Members[0].Current = false
	if value.Validate() == nil {
		t.Fatal("retained member facts became current")
	}
	value = providerProgressTestValue()
	value.Members = append(value.Members, value.Members[0])
	if value.Validate() == nil {
		t.Fatal("duplicate member could satisfy a census")
	}
}

func TestProviderProgressProtocolBoundsBeforeMemberAllocation(t *testing.T) {
	raw := []byte(`{"members":[` + strings.Repeat(`{},`, MaxProviderProgressMembers) + `{}]}`)
	if _, err := DecodeProviderProgress(raw); err == nil || !strings.Contains(err.Error(), "member count exceeds") {
		t.Fatal("unbounded member array escaped its admission limit", err)
	}
	if _, err := DecodeProviderProgress(make([]byte, MaxProviderProgressBytes+1)); err == nil || !strings.Contains(err.Error(), "byte bound") {
		t.Fatal("response byte bound was not checked first", err)
	}
}

func TestProviderProgressProtocolRejectsAmbiguousWire(t *testing.T) {
	raw, _ := json.Marshal(providerProgressTestValue())
	for _, candidate := range [][]byte{append(append([]byte(nil), raw...), []byte(` {}`)...), []byte(strings.Replace(string(raw), `"sequence":1`, `"sequence":1,"sequence":2`, 1)), []byte(strings.Replace(string(raw), `"sequence":1`, `"sequence":1,"unreviewed":true`, 1))} {
		if _, err := DecodeProviderProgress(candidate); err == nil {
			t.Fatal("ambiguous provider wire was accepted")
		}
	}
}
