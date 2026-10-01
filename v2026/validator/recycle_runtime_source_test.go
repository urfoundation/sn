// A reviewed current source may be named by a new approval. It does not confer
// live runtime authority or change any earlier signed proposal's identity.
package validator

import (
	"reflect"
	"testing"
)

// The same synthetic observation requires a different proposal hash when its
// source changes, while independent runtime pins still fence every preview.
func TestOwnerRecycleCurrentSourceRetainsExactRuntimeAuthority(t *testing.T) {
	input := recycleTestInput(t)
	legacy, err := PreviewOwnerRecycle(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Proposal.Runtime.SourceCommit = ownerRecycleSourceCommit470
	requireRecycleRefusal(t, input, "finalized runtime pins")
	input.Snapshot.Runtime.SourceCommit = ownerRecycleSourceCommit470
	current, err := PreviewOwnerRecycle(input)
	if err != nil || current.ProposalHash == legacy.ProposalHash || !reflect.DeepEqual(current.WireValues, legacy.WireValues) || current.AdmissionError() == nil {
		t.Fatal("current source converted approval or changed the retained row", current, err)
	}
	input.Snapshot.Runtime.CodeHash = recycleTestId(800)
	requireRecycleRefusal(t, input, "finalized runtime pins")
}
