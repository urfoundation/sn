package clientauth

// Quarantine of a rejected network token.
//
// The miner's operator children under provide --all-operators --auto-register
// set a network token the server rejected aside, so the supervisor signs in
// again with the hotkey. The token file is renamed, under the token's owner
// lock and only while it still holds the rejected token, to
// <token>.rejected-<unixnano>-<consecutive> in the same directory. The name
// carries the rejection time and how many rejections came in a row, so the
// supervisor's limit on automatic sign-ins survives its own restart. Only the
// newest few set-aside files are kept.

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// How many set-aside files are kept beside the token.
const maximumNetworkTokenQuarantine = 3

// Rejections closer together than this count as consecutive: a sign-in that
// does not hold for a day is rejected again in a row. Longer than the
// supervisor's largest wait between automatic sign-ins, so a chain at that
// wait stays consecutive.
const networkTokenQuarantineConsecutiveWindow = 25 * time.Hour

// NetworkTokenQuarantine is one set-aside rejected network token.
type NetworkTokenQuarantine struct {
	// the file the token was renamed to
	Path string
	Time time.Time
	// rejections in a row, this one included
	Consecutive int
}

func networkTokenQuarantinePrefix(name string) string {
	return name + ".rejected-"
}

func networkTokenQuarantineName(name string, at time.Time, consecutive int) string {
	return networkTokenQuarantinePrefix(name) + strconv.FormatInt(at.UnixNano(), 10) + "-" + strconv.Itoa(consecutive)
}

// parseNetworkTokenQuarantineName reads the time and the count from a
// set-aside file's name, or false for any other name.
func parseNetworkTokenQuarantineName(name string, leaf string) (time.Time, int, bool) {
	rest, ok := strings.CutPrefix(leaf, networkTokenQuarantinePrefix(name))
	if !ok {
		return time.Time{}, 0, false
	}
	nanos, count, ok := strings.Cut(rest, "-")
	if !ok {
		return time.Time{}, 0, false
	}
	at, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil || at <= 0 || strconv.FormatInt(at, 10) != nanos {
		return time.Time{}, 0, false
	}
	consecutive, err := strconv.Atoi(count)
	if err != nil || consecutive < 1 || strconv.Itoa(consecutive) != count {
		return time.Time{}, 0, false
	}
	return time.Unix(0, at).UTC(), consecutive, true
}

// networkTokenQuarantines lists the set-aside files of the token name among a
// directory's names, oldest first.
func networkTokenQuarantines(dir string, name string, names []string) []NetworkTokenQuarantine {
	var quarantines []NetworkTokenQuarantine
	for _, leaf := range names {
		if at, consecutive, ok := parseNetworkTokenQuarantineName(name, leaf); ok {
			quarantines = append(quarantines, NetworkTokenQuarantine{Path: filepath.Join(dir, leaf), Time: at, Consecutive: consecutive})
		}
	}
	slices.SortFunc(quarantines, func(a, b NetworkTokenQuarantine) int { return a.Time.Compare(b.Time) })
	return quarantines
}

// nextNetworkTokenQuarantine is the time and count of a quarantine at now
// after the given ones: one more in a row when the latest is within the
// consecutive window, else the first. Its time is strictly after the latest,
// so names never collide and order is kept.
func nextNetworkTokenQuarantine(quarantines []NetworkTokenQuarantine, now time.Time) (time.Time, int) {
	// the time as its name records it
	now = time.Unix(0, now.UnixNano()).UTC()
	if len(quarantines) == 0 {
		return now, 1
	}
	latest := quarantines[len(quarantines)-1]
	at := now
	if !at.After(latest.Time) {
		at = latest.Time.Add(time.Nanosecond)
	}
	if at.Sub(latest.Time) < networkTokenQuarantineConsecutiveWindow {
		return at, latest.Consecutive + 1
	}
	return at, 1
}

// LatestNetworkTokenQuarantine is the newest set-aside file of the token at
// path, or false when there is none.
func LatestNetworkTokenQuarantine(path string) (NetworkTokenQuarantine, bool, error) {
	entries, err := os.ReadDir(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return NetworkTokenQuarantine{}, false, nil
	}
	if err != nil {
		return NetworkTokenQuarantine{}, false, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	quarantines := networkTokenQuarantines(filepath.Dir(path), filepath.Base(path), names)
	if len(quarantines) == 0 {
		return NetworkTokenQuarantine{}, false, nil
	}
	return quarantines[len(quarantines)-1], true, nil
}
