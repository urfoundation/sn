// Actual Frontier preimages retain the exact millisecond hash while economic
// windows use the independently fixed public-seconds boundary profile.
package payoutartifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
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

// Snow's finalized Finney block 9,278,937 commits this exact header in its
// Frontier digest. Its stored time equals Timestamp.Now there; the public
// eth_getBlockByNumber projection rendered the same block as 0x6acefc24 seconds.
func wholeWorkMainnetFrontierHeader(t *testing.T) []byte {
	t.Helper()
	header := &types.Header{ParentHash: common.HexToHash("0xd1d20d71b8ebcd22e6d47cc21d13cbe09ee7fcf71fd37c85478966fc9eab42f1"), UncleHash: types.EmptyUncleHash, Coinbase: common.HexToAddress("0xa465590dc5684ee630be3e01b808e106b3547e21"), Root: common.HexToHash("0xa127f70f130ba7e49a9f92fc1cf989d5bbaa41e061b434d31ac2c20bafafe171"), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: big.NewInt(9278937), GasLimit: 75000000, Time: 1791949860000, Extra: []byte{}}
	if header.Hash().Hex() != "0xa13fc554995ad5bc6a8da64a8911a9b286e2dccbd1b5cc0c7d8c420b38f450ab" {
		t.Fatal("mainnet header vector differs from its native commitment")
	}
	raw, err := rlp.EncodeToBytes(header)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// PH-15: each header is read under its own declared profile. Frontier floors
// its stored milliseconds to the public seconds; ordinary EVM keeps its seconds.
func TestWholeWorkClockTimeFollowsDeclaredHeaderProfile(t *testing.T) {
	// 0x6acefc24 = 1,791,949,860 s = 2026-10-14T03:51:00Z.
	mainnet := time.Date(2026, time.October, 14, 3, 51, 0, 0, time.UTC)
	if actual, err := WindowClockTime(FrontierWindowClockProfile, wholeWorkMainnetFrontierHeader(t)); err != nil || !actual.Equal(mainnet) || actual.Unix() != 0x6acefc24 {
		t.Fatal("mainnet Frontier milliseconds lost their public seconds instant", actual, err)
	}
	_, remainder, _ := wholeWorkRenderedFrontier(t, 9278937, 1791949860999)
	if actual, err := WindowClockTime(FrontierWindowClockProfile, remainder); err != nil || !actual.Equal(mainnet) {
		t.Fatal("millisecond remainder moved the public seconds instant", actual, err)
	}
	ordinary, err := rlp.EncodeToBytes(&types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0), Time: 1700000000})
	if err != nil {
		t.Fatal(err)
	}
	// 1,700,000,000 s = 2023-11-14T22:13:20Z.
	if actual, err := WindowClockTime("", ordinary); err != nil || !actual.Equal(time.Date(2023, time.November, 14, 22, 13, 20, 0, time.UTC)) {
		t.Fatal("ordinary EVM seconds profile changed", actual, err)
	}
}

// PH-15: an unknown profile has no unit, and bytes outside the declared profile
// or the Unix range are refused rather than read under another unit.
func TestWholeWorkClockTimeRefusesUnknownProfileAndForeignBytes(t *testing.T) {
	mainnet := wholeWorkMainnetFrontierHeader(t)
	if _, err := WindowClockTime("frontier-legacy-rlp15-seconds", mainnet); !errors.Is(err, ErrClosedWorkUnavailable) || !strings.Contains(err.Error(), `profile "frontier-legacy-rlp15-seconds" is unknown`) {
		t.Fatal("unknown header profile chose a unit", err)
	}
	london, err := rlp.EncodeToBytes(&types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0), Time: 1700000000, BaseFee: big.NewInt(7)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WindowClockTime(FrontierWindowClockProfile, london); !errors.Is(err, ErrClosedWorkIntegrity) || !strings.Contains(err.Error(), "not the Frontier RLP15 profile") {
		t.Fatal("sixteen-field seconds header read as Frontier milliseconds", err)
	}
	if _, err := WindowClockTime(FrontierWindowClockProfile, mainnet[:len(mainnet)-1]); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("truncated header bytes produced an instant", err)
	}
	overflow, err := rlp.EncodeToBytes(&types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0), Time: math.MaxInt64 + 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WindowClockTime("", overflow); !errors.Is(err, ErrClosedWorkIntegrity) || !strings.Contains(err.Error(), "exceeds the Unix range") {
		t.Fatal("seconds beyond the Unix range wrapped", err)
	}
}
