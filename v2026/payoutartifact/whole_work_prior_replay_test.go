// A publisher cannot turn timeless original endpoint signatures into a second
// earned window by omitting a separately retained reconciliation checkpoint.
package payoutartifact

import (
	"bytes"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

func TestWholeWorkIndependentPriorPreventsRedatedOriginalContractReplay(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	first, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || first == nil || !first.Complete || len(first.ReconciledContracts) != 2 {
		t.Fatal("initial original reconciliation unavailable", first, err)
	}
	oldReports := bytes.Clone(fixture.artifact.ClosedWork.Records[0].OriginalReports)
	fixture.expected.PriorContracts = append([]WholeWorkPriorContract(nil), first.ReconciledContracts...)
	old := fixture.artifact
	old.Epoch++
	old.ClosedWork.Epoch++
	start, end := fixture.inventory.Clock.EndTime, fixture.inventory.Clock.EndTime.Add(time.Hour)
	startHeader := &types.Header{Number: new(big.Int).SetUint64(old.End.Number), Time: uint64(start.Unix())}
	endHeader := &types.Header{Number: new(big.Int).SetUint64(old.End.Number + 100), Time: uint64(end.Unix())}
	old.Start = Boundary{Number: startHeader.Number.Uint64(), Hash: startHeader.Hash().Hex()}
	old.End = Boundary{Number: endHeader.Number.Uint64(), Hash: endHeader.Hash().Hex()}
	old.ClosedWork.Start, old.ClosedWork.End = old.Start, old.End
	old.ClosedWork.WindowStart, old.ClosedWork.WindowEnd = start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)
	fixture.inventory.Window.Start, fixture.inventory.Window.End = old.ClosedWork.WindowStart, old.ClosedWork.WindowEnd
	fixture.inventory.Clock = &ClosedWorkWindowClock{Start: old.Start, End: old.End, StartTime: start, EndTime: end}
	fixture.inventory.Clock.StartHeader, err = rlp.EncodeToBytes(startHeader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.inventory.Clock.EndHeader, err = rlp.EncodeToBytes(endHeader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.authority.Epoch, fixture.authority.Start, fixture.authority.End = old.Epoch, old.Start, old.End
	for index := range old.ClosedWork.Records {
		closed, err := time.Parse(time.RFC3339Nano, old.ClosedWork.Records[index].ClosedAt)
		if err != nil {
			t.Fatal(err)
		}
		redated := closed.Add(time.Hour).Format(time.RFC3339Nano)
		old.ClosedWork.Records[index].ClosedAt = redated
		fixture.inventory.Window.Records[index].ClosedAt = &redated
	}
	for owner := range fixture.inventory.Owners {
		prior, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), fixture.inventory.Owners[owner].End)
		if err != nil {
			t.Fatal(err)
		}
		for side, boundary := range []Boundary{old.Start, old.End} {
			fixture.changeCut(t, owner, side, func(cut *coreprotocol.OriginalWorkCut) {
				cut.Epoch, cut.Block, cut.BlockHash = old.Epoch, boundary.Number, [32]byte(commonWholeWorkHash(t, boundary.Hash))
				cut.Contracts, cut.Revision = prior.Contracts, prior.Revision
			})
			pair := &fixture.inventory.Owners[owner]
			raw := pair.StartRequest
			if side == 1 {
				raw = pair.EndRequest
			}
			request, err := coreprotocol.DecodeOriginalWorkRequest(raw, fixture.authority.RequestPublicKey)
			if err != nil {
				t.Fatal(err)
			}
			request.Epoch, request.Block, request.BlockHash = old.Epoch, boundary.Number, [32]byte(commonWholeWorkHash(t, boundary.Hash))
			request.IssuedAtUnix = []time.Time{start, end}[side].Unix()
			request.ExpiresAtUnix = request.IssuedAtUnix + 300
			request, err = coreprotocol.SignOriginalWorkRequest(request, fixture.requestKey)
			if err != nil {
				t.Fatal(err)
			}
			raw, err = request.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if side == 0 {
				pair.StartRequest = raw
			} else {
				pair.EndRequest = raw
			}
		}
	}
	fixture.signAuthority(t)
	creationRebuildArtifact(t, fixture)
	if !bytes.Equal(oldReports, fixture.artifact.ClosedWork.Records[0].OriginalReports) || len(fixture.authority.PriorContracts) != 0 {
		t.Fatal("fixture changed original signed reports or proposed the omitted prior")
	}
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("redated SQL and omitted prior reused an independently reconciled contract", value, err)
	}
}
