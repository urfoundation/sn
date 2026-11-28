package miner

// The share limits of a provide run (sdk provide_share.go): the fastest the
// host shares and the most data it shares in a calendar month (UTC), from
// --max-share-speed and --monthly-share-limit (owner decision 2026-11-28,
// feature 10). Off unless the owner sets them. A cap lowers how much the host
// shares, which can lower its reliability ranking and its earnings, and the
// option help says so.
//
// The limits are the host's: each is split evenly over the run's provider
// slots, and with --all-operators over the operators too, as --max-memory is.
// Each slot keeps its share of the limits and its month's data in its own
// folder of the state directory (.provide_share/<slot>), so a restart, an
// update and the background service keep both: a run without the flags keeps
// the limits it finds there, and 0 removes one.

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Bytes per second in a megabit per second.
const provideShareBytesPerMegabit = 125000

// The fastest speed the flag reads, 10 terabits per second: past it the
// count of bytes per second stops being exact.
const provideShareMaxMegabits = 10 * 1000 * 1000

// The limits the flags set, and which flags were given. A limit of 0 is no
// limit; a flag not given keeps the slot's stored limit.
type provideShareFlags struct {
	limits     sdk.ProvideShareLimits
	speedSet   bool
	monthlySet bool
}

// Reads --max-share-speed, in megabits per second, and --monthly-share-limit,
// a byte count with an optional suffix b, kib, mib, gib or tib.
func parseProvideShareFlags(opts docopt.Opts) (provideShareFlags, error) {
	flags := provideShareFlags{}
	if raw, err := opts.String("--max-share-speed"); err == nil {
		megabits, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || megabits < 0 || math.IsNaN(megabits) || provideShareMaxMegabits < megabits {
			return provideShareFlags{}, fmt.Errorf("--max-share-speed=%s is not a speed in megabits per second such as 20", raw)
		}
		flags.limits.MaxBytesPerSecond = sdk.ByteCount(math.Round(megabits * provideShareBytesPerMegabit))
		flags.speedSet = true
	}
	if raw, err := opts.String("--monthly-share-limit"); err == nil {
		byteCount, err := connect.ParseByteCount(strings.TrimSpace(raw))
		if err != nil || byteCount < 0 {
			return provideShareFlags{}, fmt.Errorf("--monthly-share-limit=%s is not a byte count such as 500gib", raw)
		}
		flags.limits.MonthlyByteCount = sdk.ByteCount(byteCount)
		flags.monthlySet = true
	}
	return flags, nil
}

// Whether either flag was given.
func (self provideShareFlags) set() bool {
	return self.speedSet || self.monthlySet
}

// One of count equal shares of a limit, at least 1 so a limit never becomes
// no limit; 0 stays 0.
func provideShareSplit(limit sdk.ByteCount, count int) sdk.ByteCount {
	if limit <= 0 || count <= 1 {
		return limit
	}
	return max(1, limit/sdk.ByteCount(count))
}

// A slot's limits: the flags' limits split evenly over slotCount slots, and
// for a flag not given the slot's stored limit.
func (self provideShareFlags) slotLimits(stored *sdk.ProvideShareLimits, slotCount int) *sdk.ProvideShareLimits {
	limits := &sdk.ProvideShareLimits{}
	if stored != nil {
		*limits = *stored
	}
	if self.speedSet {
		limits.MaxBytesPerSecond = provideShareSplit(self.limits.MaxBytesPerSecond, slotCount)
	}
	if self.monthlySet {
		limits.MonthlyByteCount = provideShareSplit(self.limits.MonthlyByteCount, slotCount)
	}
	return limits
}

// The flags an operator child of provide --all-operators runs with: the
// given limits split evenly over childCount children, as exact counts the
// child reads back (bytes per second as megabits, the month as bytes).
func (self provideShareFlags) childArgs(childCount int) []string {
	args := []string{}
	if self.speedSet {
		bytesPerSecond := provideShareSplit(self.limits.MaxBytesPerSecond, childCount)
		megabits := strconv.FormatFloat(float64(bytesPerSecond)/provideShareBytesPerMegabit, 'f', -1, 64)
		args = append(args, "--max-share-speed="+megabits)
	}
	if self.monthlySet {
		args = append(args, fmt.Sprintf("--monthly-share-limit=%db", provideShareSplit(self.limits.MonthlyByteCount, childCount)))
	}
	return args
}

// The folder a slot keeps its share limits and month's data in, created when
// missing: .provide_share/direct, or .provide_share/proxy-<hash> named as the
// slot's credential file is, in the state directory.
func providerShareStorageDir(proxySettings *connect.ProxySettings) (string, error) {
	name := "direct"
	if proxySettings != nil {
		sum := sha256.Sum256([]byte(proxySettings.Network + "\x00" + proxySettings.Address))
		name = fmt.Sprintf("proxy-%x", sum[:8])
	}
	dir, err := providerStatePath(filepath.Join(".provide_share", name))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}
