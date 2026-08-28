package main

import (
	"encoding/json"
	"testing"
)

func TestGovernanceEntitlementSnapshotJSONPreservesEveryField(t *testing.T) {
	want := GovernanceEntitlementSnapshot{
		Epoch: 7, NoID: 2, PayoutRoot: "0xroot", ArtifactHash: "0xartifact",
		FundedRao: "101", TotalRao: "99", ClaimedRao: "42", ExpiryBlock: 123, Status: 2,
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got GovernanceEntitlementSnapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("governance entitlement evidence did not round-trip: got=%+v want=%+v json=%s", got, want, b)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"epoch", "no_id", "payout_root", "artifact_hash", "funded_rao", "total_rao", "claimed_rao", "expiry_block", "status"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("governance entitlement evidence omitted %q: %s", name, b)
		}
	}
}
