//go:build scale

package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
)

// GL-10: 100k and 1M providers over a previous root of 1M accounts, in
// single-epoch and ten-epoch roots. The builder must finish well inside the
// 1-hour budget and the verifier inside 4 hours; wall time, peak memory and
// the bytes the verifier fetched are logged. Run one size with
// -run 'TestScaleLedger/providers=100000$'.
func TestScaleLedger(t *testing.T) {
	for _, providers := range []int{100_000, 1_000_000} {
		for _, epochs := range []int{1, 10} {
			t.Run(fmt.Sprintf("providers=%d/epochs=%d", providers, epochs), func(t *testing.T) {
				scaleRoot(t, providers, epochs)
			})
		}
	}
}

const (
	scaleAccounts = 1_000_000
	scaleEpoch    = 1_000
	// scaleFunding is about one epoch's pool share, 1,450 α, in rao.
	scaleFunding = 1_450_000_000_000
)

func scaleRoot(t *testing.T, providers, epochs int) {
	ctx := context.Background()
	random := rand.New(rand.NewPCG(uint64(providers), uint64(epochs)))
	randomKey := func() (k [32]byte) {
		for i := 0; i < 32; i += 8 {
			v := random.Uint64()
			for j := range 8 {
				k[i+j] = byte(v >> (8 * j))
			}
		}
		return k
	}

	// The previous root: 1M accounts, which the validator audited and holds
	// in its custody on disk.
	previous := make([]merkle.LedgerAccount, scaleAccounts)
	for i := range previous {
		previous[i].Coldkey = randomKey()
		previous[i].Cumulative.SetUint64(1 + random.Uint64N(1_000_000_000))
	}
	tree, err := merkle.BuildLedgerSumTree(ctx, previous)
	if err != nil {
		t.Fatal(err)
	}
	custody := &fileCustody{MemoryCustody: NewMemoryCustody(), dir: t.TempDir()}
	header := &Header{NoID: testNoID, Epoch: scaleEpoch, Epochs: []HeaderEpoch{{Epoch: scaleEpoch}}}
	for _, chunk := range encodeAccountChunks(previous) {
		ref := RefOf(chunk)
		header.Accounts = append(header.Accounts, ref)
		if err := custody.Keep(ctx, ref, chunk); err != nil {
			t.Fatal(err)
		}
	}
	headerBytes, err := EncodeHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	finalized := Root{Epoch: scaleEpoch, LeafCount: tree.LeafCount(), LedgerHash: RefOf(headerBytes)}
	finalized.Root, finalized.Total = tree.Root()
	if err := custody.Keep(ctx, finalized.LedgerHash, headerBytes); err != nil {
		t.Fatal(err)
	}
	tree = nil

	// Providers: 90% pay an existing account's coldkey, 10% a new one; 1,000
	// are head-fleet candidates, half of them bound.
	ids := make([][16]byte, providers)
	for i := range ids {
		key := randomKey()
		copy(ids[i][:], key[:16])
	}
	slices.SortFunc(ids, func(a, b [16]byte) int { return bytes.Compare(a[:], b[:]) })
	coldkeys := make([][32]byte, providers)
	for i := range coldkeys {
		if random.IntN(10) < 9 {
			coldkeys[i] = previous[random.IntN(scaleAccounts)].Coldkey
		} else {
			coldkeys[i] = randomKey()
		}
	}
	oracle := heads{}
	bound := map[int]bool{}
	for len(bound) < 1_000 {
		bound[random.IntN(providers)] = len(bound)%2 == 0
	}
	fundedThrough := map[uint64]uint256.Int{}
	input := BuildInput{NoID: testNoID, Epoch: scaleEpoch + uint64(epochs), FirstEpoch: 1, Previous: &Previous{Root: finalized, Accounts: previous}, FundedThrough: fundedThrough}
	for k := uint64(scaleEpoch + 1); k <= input.Epoch; k++ {
		rows := make([]protocol.LedgerUsageRow, providers)
		for i := range rows {
			rows[i] = protocol.LedgerUsageRow{ClientID: ids[i], Coldkey: coldkeys[i], UsageBytes: 1 + random.Uint64N(1_000_000_000), HeadExcluded: bound[i]}
		}
		input.Epochs = append(input.Epochs, EpochInput{Epoch: k, Rows: rows})
		oracle[k] = map[[16]byte]bool{}
		for i, active := range bound {
			oracle[k][ids[i]] = active
		}
		var funding uint256.Int
		funding.SetUint64(scaleFunding * (k - scaleEpoch))
		funding.Add(&funding, &finalized.Total)
		fundedThrough[k] = funding
	}
	ids, coldkeys = nil, nil

	var l *Ledger
	build := measure(t, func() {
		if l, err = Build(ctx, input); err != nil {
			t.Fatal(err)
		}
	})
	if want := fundedThrough[input.Epoch]; l.Commit.Total != want {
		t.Fatalf("total %s, funded %s", l.Commit.Total.Dec(), want.Dec())
	}
	// The server keeps serving the objects; nothing else the builder made
	// stays in memory.
	published, commit, objects := objectBytes(l.Objects), l.Commit, l.Objects
	input, l = BuildInput{}, nil

	var res *Result
	verify := measure(t, func() {
		res, err = Verify(ctx, VerifyInput{
			NoID: testNoID, FirstEpoch: 1, Pending: commit, Seq: 1, Previous: &finalized, FundedThrough: fundedThrough,
			Fetcher: objects, Custody: custody, Heads: oracle, MaxFetchBytes: DefaultMaxFetchBytes,
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	expectPass(t, res)
	if build.wall > time.Hour || verify.wall > 4*time.Hour {
		t.Fatalf("over budget: build %v, verify %v", build.wall, verify.wall)
	}
	t.Logf("GL-10 providers=%d epochs=%d accounts=%d->%d build=%v (peak RSS %s, heap +%s) verify=%v (peak RSS %s, heap +%s) fetched=%s of published %s, budget %s",
		providers, epochs, scaleAccounts, commit.LeafCount,
		build.wall.Round(time.Millisecond), mib(build.peakRSS), mib(build.heap),
		verify.wall.Round(time.Millisecond), mib(verify.peakRSS), mib(verify.heap),
		mib(res.FetchedBytes), mib(published), mib(DefaultMaxFetchBytes))
}

// fileCustody keeps objects in files, as a validator's durable custody does.
type fileCustody struct {
	*MemoryCustody
	dir string
}

func (c *fileCustody) Object(ctx context.Context, ref Ref) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(c.dir, ref.String()[7:]))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return data, err == nil, err
}

func (c *fileCustody) Keep(ctx context.Context, ref Ref, data []byte) error {
	return os.WriteFile(filepath.Join(c.dir, ref.String()[7:]), data, 0o600)
}

type measurement struct {
	wall time.Duration
	// peakRSS is the process's peak resident set during the call, inputs
	// included; heap is the peak growth of heap objects over the start.
	peakRSS, heap uint64
}

// measure runs f after a collection, and reports its wall time and peak memory.
func measure(t *testing.T, f func()) measurement {
	runtime.GC()
	debug.FreeOSMemory()
	// Writing 5 to clear_refs resets the peak RSS (VmHWM) on Linux.
	if err := os.WriteFile("/proc/self/clear_refs", []byte("5"), 0); err != nil {
		t.Logf("cannot reset peak RSS: %v", err)
	}
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	base := sample[0].Value.Uint64()
	peak := base
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			metrics.Read(sample)
			peak = max(peak, sample[0].Value.Uint64())
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	start := time.Now()
	f()
	wall := time.Since(start)
	close(done)
	<-stopped
	return measurement{wall: wall, peakRSS: peakRSSBytes(t), heap: peak - base}
}

func peakRSSBytes(t *testing.T) uint64 {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			kb, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return kb << 10
		}
	}
	t.Fatal("no VmHWM")
	return 0
}

func mib(n uint64) string { return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20)) }
