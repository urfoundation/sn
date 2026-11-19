//go:build linux || darwin

package validator

// A hop that recorded the zero egress-IP-hash shows no head unit (VALIDATOR.md
// §8.1). The operator records it for a provider whose transports declared
// that they leave through a proxy, so a proxy slot adds no breadth. Both head
// projections skip it: the live claims (AttemptCutEgressClaims) and the
// compact replay. So a fleet that bought proxies for six slots scores its
// host's prefix alone, and a fleet of three direct hosts keeps one unit per
// host. This guards the rule the operator relies on, and it held before any
// operator recorded the zero hash.

import (
	"context"
	"testing"

	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/protocol/headscore"
)

// A bound member's prefix on one complete record: the seed hop, then the
// member's hop.
type headZeroTestMember struct {
	clientId connect.Id
	key      FleetScoreKey
	prefix   [32]byte
}

func headZeroTestRecords(members []headZeroTestMember) []AttemptRecord {
	records := make([]AttemptRecord, 0, len(members))
	for i, member := range members {
		binding := AttemptBinding{
			ClientID: member.clientId, Active: true, UIDFound: true, UID: member.key.UID,
			FleetID: attemptHex32(member.key.FleetID), Hotkey: attemptHex32(member.key.Hotkey), Generation: member.key.Generation,
		}
		records = append(records, AttemptRecord{
			Sequence:    uint64(i + 1),
			Disposition: AttemptDispositionComplete,
			Assignments: []AttemptAssignment{{NextHop: member.clientId, Confirmed: true, Binding: binding}},
			Proof: &ProofRecord{Hops: []connect.VerifyProofHop{
				{ClientId: connect.NewId(), EgressIpHash: [32]byte{0x5e}},
				{ClientId: member.clientId, EgressIpHash: member.prefix},
			}},
		})
	}
	return records
}

func TestHeadZeroEgressHashShowsNoUnit(t *testing.T) {
	fleet := FleetScoreKey{FleetID: [32]byte{0xa1}, Hotkey: [32]byte{0xb1}, Generation: 1, UID: 7}
	hosts := FleetScoreKey{FleetID: [32]byte{0xa2}, Hotkey: [32]byte{0xb2}, Generation: 1, UID: 8}
	members := []headZeroTestMember{{clientId: connect.NewId(), key: fleet, prefix: [32]byte{0x01}}}
	// the fleet's six proxy slots record the zero hash
	for range 6 {
		members = append(members, headZeroTestMember{clientId: connect.NewId(), key: fleet})
	}
	for i := range 3 {
		members = append(members, headZeroTestMember{clientId: connect.NewId(), key: hosts, prefix: [32]byte{0x10, byte(i)}})
	}
	records := headZeroTestRecords(members)
	want := map[FleetScoreKey]string{fleet: "1", hosts: "3"}

	// the live claims
	claims, err := AttemptCutEgressClaims(&AttemptLedgerCut{EgressFirstSequence: 1, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	census := map[FleetScoreKey]map[[32]byte]bool{fleet: {}, hosts: {}}
	for _, claim := range claims {
		key := fleet
		if claim.Binding.FleetID == attemptHex32(hosts.FleetID) {
			key = hosts
		}
		hash, err := parseReleaseHex32("attempt egress hash", claim.EgressIPHash, false)
		if err != nil {
			t.Fatal(err)
		}
		if hash == ([32]byte{}) {
			t.Fatalf("a zero hash became a claim of %s", claim.Binding.ClientID)
		}
		census[key][hash] = true
	}
	for key, raw := range headscore.RawScores(census) {
		if raw.RatString() != want[key] {
			t.Errorf("live claims: fleet uid %d scores %s, want %s", key.UID, raw.RatString(), want[key])
		}
	}

	// the compact replay's projection
	current := map[connect.Id]FleetScoreKey{}
	for _, member := range members {
		current[member.clientId] = member.key
	}
	projection, err := newAttemptCutV2HeadProjection(context.Background(), 1, AttemptCutV2HeadOptions{CurrentBindingKVs: current, MaxProviders: 64, MaxFleetPrefixes: 64})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := projection.visitRecord(record); err != nil {
			t.Fatal(err)
		}
	}
	for key, raw := range headscore.RawScores(projection.fleetsKVs) {
		if raw.RatString() != want[key] {
			t.Errorf("compact replay: fleet uid %d scores %s, want %s", key.UID, raw.RatString(), want[key])
		}
		if projection.fleetsKVs[key][[32]byte{}] {
			t.Errorf("compact replay: fleet uid %d holds the zero hash", key.UID)
		}
	}
}
