// Actual Frontier preimages retain the exact millisecond hash while economic
// windows use the independently fixed public-seconds boundary profile.
package payoutartifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// Render the actual fixed Frontier shape, including its external runtime fee.
func wholeWorkRenderedFrontier(t *testing.T, number, milliseconds uint64) (*types.Header, []byte, []byte) {
	t.Helper()
	header := &types.Header{Number: new(big.Int).SetUint64(number), Difficulty: big.NewInt(0), Time: milliseconds}
	raw, err := rlp.EncodeToBytes(header)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	fields["timestamp"] = json.RawMessage(fmt.Sprintf("\"0x%x\"", milliseconds/1000))
	fields["baseFeePerGas"] = json.RawMessage(`"0x7"`)
	fields["author"] = fields["miner"]
	delete(fields, "mixHash")
	encoded, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return header, raw, encoded
}

func TestWholeWorkFrontierRecoversExactCommittedMillisecondPreimage(t *testing.T) {
	header, raw, rendered := wholeWorkRenderedFrontier(t, 44, 1791244800789)
	actual, err := RecoverFrontierWindowHeader(t.Context(), rendered, Boundary{Number: 44, Hash: header.Hash().Hex()})
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("exact committed Frontier preimage not recovered", err)
	}
	var echo map[string]json.RawMessage
	json.Unmarshal(rendered, &echo)
	echo["timestamp"] = json.RawMessage(`"0x1"`)
	changed, _ := json.Marshal(echo)
	if _, err := RecoverFrontierWindowHeader(t.Context(), changed, Boundary{Number: 44, Hash: header.Hash().Hex()}); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("hash echo replaced actual preimage", err)
	}
}

func TestWholeWorkFrontierWindowClockRequiresOriginalProfileAndHash(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	start := fixture.inventory.Clock.StartTime
	end := fixture.inventory.Clock.EndTime
	startHeader, startRaw, _ := wholeWorkRenderedFrontier(t, fixture.artifact.Start.Number, uint64(start.Unix())*1000+17)
	endHeader, endRaw, _ := wholeWorkRenderedFrontier(t, fixture.artifact.End.Number, uint64(end.Unix())*1000+998)
	fixture.artifact.Start.Hash, fixture.artifact.End.Hash = startHeader.Hash().Hex(), endHeader.Hash().Hex()
	fixture.artifact.ClosedWork.Start, fixture.artifact.ClosedWork.End = fixture.artifact.Start, fixture.artifact.End
	clock := &ClosedWorkWindowClock{HeaderProfile: FrontierWindowClockProfile, Start: fixture.artifact.Start, End: fixture.artifact.End, StartTime: start, EndTime: end, StartHeader: startRaw, EndHeader: endRaw}
	if !clock.matches(t.Context(), fixture.artifact, start, end) {
		t.Fatal("original Frontier seconds projection rejected")
	}
	clock.HeaderProfile = ""
	if clock.matches(t.Context(), fixture.artifact, start, end) {
		t.Fatal("millisecond bytes accepted as Ethereum seconds")
	}
	clock.HeaderProfile = FrontierWindowClockProfile
	clock.StartTime = start.Add(time.Millisecond)
	if clock.matches(t.Context(), fixture.artifact, clock.StartTime, end) {
		t.Fatal("artifact-selected fractional clock replaced fixed projection")
	}
}

func TestWholeWorkFrontierUnknownProjectionAndFiniteOwnerStayDistinct(t *testing.T) {
	header, _, rendered := wholeWorkRenderedFrontier(t, 45, 1791244800019)
	expected := Boundary{Number: 45, Hash: header.Hash().Hex()}
	var fields map[string]json.RawMessage
	json.Unmarshal(rendered, &fields)
	fields["unreviewedRuntimeField"] = json.RawMessage(`1`)
	changed, _ := json.Marshal(fields)
	if _, err := RecoverFrontierWindowHeader(t.Context(), changed, expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("unreviewed renderer profile accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RecoverFrontierWindowHeader(ctx, rendered, expected); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled projection escaped owner", err)
	}
	if _, err := RecoverFrontierWindowHeader(t.Context(), make([]byte, 1024*1024+1), expected); !errors.Is(err, ErrClosedWorkCapacity) {
		t.Fatal("oversized header parsed", err)
	}
}
