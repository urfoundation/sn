// Independently selected SDK generations remain separate owners even when
// they share a client and key; discovery order never chooses a winning cut.
package payoutartifact

import (
	"errors"
	"testing"

	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Add a separately signed second lifecycle for the first client. The original
// generation remains in the admitted roster and retains its actual contracts.
func (self *wholeWorkTestFixture) addGeneration(t *testing.T, carryContracts bool) {
	t.Helper()
	owner := self.authority.Owners[0]
	owner.Generation[0]++
	original := self.inventory.Owners[0]
	pair := WholeWorkOwnerCuts{}
	for side, raw := range [][]byte{original.Start, original.End} {
		cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		cut.Generation = owner.Generation
		if !carryContracts {
			cut.Revision = 0
			cut.Contracts = []coreprotocol.OriginalWorkContract{}
		}
		cut, err = coreprotocol.SignOriginalWorkCut(t.Context(), cut, self.ownerKeys[0])
		if err != nil {
			t.Fatal(err)
		}
		cutRaw, err := cut.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		requestRaw := original.StartRequest
		if side == 1 {
			requestRaw = original.EndRequest
		}
		request, err := coreprotocol.DecodeOriginalWorkRequest(requestRaw, self.authority.RequestPublicKey)
		if err != nil {
			t.Fatal(err)
		}
		request.Generation = owner.Generation
		request.RequestId[2] = 1
		request, err = coreprotocol.SignOriginalWorkRequest(request, self.requestKey)
		if err != nil {
			t.Fatal(err)
		}
		requestRaw, err = request.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if side == 0 {
			pair.Start, pair.StartRequest = cutRaw, requestRaw
		} else {
			pair.End, pair.EndRequest = cutRaw, requestRaw
		}
	}
	self.authority.Owners = append(self.authority.Owners[:1:1], append([]WholeWorkOwner{owner}, self.authority.Owners[1:]...)...)
	self.inventory.Owners = append(self.inventory.Owners[:1:1], append([]WholeWorkOwnerCuts{pair}, self.inventory.Owners[1:]...)...)
	self.ownerKeys = append(self.ownerKeys[:1:1], self.ownerKeys...)
	signed, err := SignWholeWorkAuthority(t.Context(), self.authority, self.authorityKey)
	if err != nil {
		t.Fatal("independent generation roster cannot represent both original lifecycles", err)
	}
	self.authority = signed
	self.inventory.Authority, err = signed.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestWholeWorkMultipleSdkGenerationsRequireEveryOriginalPair(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.addGeneration(t, false)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.Contracts != 2 || len(value.ExpectedProviders) != 2 {
		t.Fatal("distinct admitted SDK generations did not preserve complete original work", value, err)
	}
	fixture.inventory.Owners[0].End = nil
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("later authentic generation replaced the missing older original", err)
	}
	fixture.inventory.Owners[0] = fixture.inventory.Owners[1]
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("valid newer cut was substituted for the independently admitted predecessor", err)
	}
}

func TestWholeWorkSdkGenerationHandoffCannotOverwriteCarriedContract(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.addGeneration(t, true)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("two SDK owners of one carried contract selected an implicit latest head", err)
	}
}

func TestWholeWorkSdkGenerationRosterRejectsDuplicatesAndNetworkChanges(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.addGeneration(t, false)
	for _, change := range []func(*WholeWorkAuthority){func(a *WholeWorkAuthority) {
		a.Owners[1].Generation = a.Owners[0].Generation
	}, func(a *WholeWorkAuthority) {
		a.Owners[1].NetworkId[0]++
	}, func(a *WholeWorkAuthority) {
		a.Owners[0], a.Owners[1] = a.Owners[1], a.Owners[0]
	}} {
		authority := fixture.authority
		authority.Owners = append([]WholeWorkOwner(nil), authority.Owners...)
		change(&authority)
		if _, err := SignWholeWorkAuthority(t.Context(), authority, fixture.authorityKey); !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("ambiguous SDK generation census was signed", err)
		}
	}
}
