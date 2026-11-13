//go:build linux || darwin

// The signed settlement amendments (sn/amendment, published beside the price
// sheet) as the settlement v2 ledger audit reads them: E_x, the first epoch
// the extender pool amendment covers for a pool (WHITEPAPER.md §8.6.7), which
// ledger.Verify needs to know which covered epochs must carry an extender
// component (check 11).
//
// An epoch is covered when the signed entry in force for it is, or, only
// where no signed entry of its kind is in force, when a configured line is
// (amendment.File.At). The configured lines are the fallback for an entry
// whose signing cannot happen before its first epoch starts, given to
// `validator run --amendments-fallback`, identical on every reader; they are
// not part of the signed release configuration, so no config hash changes
// with them. A deployment with a pinned signer reads its verified file,
// refreshed at most every settlementV2AmendmentsRefresh and kept at
// state_dir/settlement-v2-amendments/amendments.yml; while none is held what
// is in force is unknown, the audit decides nothing and retries, and
// configuration does not stand in. A deployment without a pinned signer
// publishes none, so only its configured lines can put the amendment in
// force.
//
// That copy is the ledger audit's own. Capture the flag's steering reader
// (release_capture_the_flag.go) keeps its verified copy of the same published
// file at state_dir/amendments/amendments.yml, and each reader reads and
// writes only its own: neither a stale nor a newer copy of one can change what
// the other holds, and each source refuses a published file that drops or
// changes an entry of the one it holds.
package validator

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/urfoundation/sn/amendment"
)

// Spaces the fetches of the published file.
const settlementV2AmendmentsRefresh = 10 * time.Minute

// The seams tests replace: the published address and the pinned signers.
var (
	settlementV2AmendmentsUrl    = amendment.URL
	settlementV2AmendmentsSigner = amendment.SignerFor
)

// Where the ledger audit keeps its verified amendments file: a directory
// of its own, apart from capture the flag's state_dir/amendments.
func settlementV2AmendmentsStorePath(stateDir string) string {
	return filepath.Join(stateDir, "settlement-v2-amendments", "amendments.yml")
}

// Holds one deployment's amendments for the audit.
type settlementV2Amendments struct {
	// Nil for a deployment without a pinned signer, whose file is fixed.
	source   *amendment.Source
	fallback *amendment.Fallback
	now      func() time.Time

	stateLock sync.Mutex
	file      *amendment.File
	refreshed time.Time
}

// Reads the configured fallback lines and, for a deployment with a
// pinned signer, keeps its verified file at
// settlementV2AmendmentsStorePath(stateDir).
func newSettlementV2Amendments(chainId uint64, netuid uint16, stateDir string, lines []string) (*settlementV2Amendments, error) {
	fallback, err := amendment.ParseFallback(chainId, netuid, lines)
	if err != nil {
		return nil, fmt.Errorf("--amendments-fallback: %w", err)
	}
	amendments := &settlementV2Amendments{fallback: fallback, now: time.Now}
	signer, pinned := settlementV2AmendmentsSigner(chainId, netuid)
	if !pinned {
		if amendments.file, err = amendment.Unpublished(chainId, netuid); err != nil {
			return nil, err
		}
		return amendments, nil
	}
	store := &releaseDepositPriceStore{path: settlementV2AmendmentsStorePath(stateDir)}
	amendments.source = amendment.NewSource(settlementV2AmendmentsUrl, chainId, netuid, signer, store)
	return amendments, nil
}

// The verified file, refreshed when due, nil while none is held. A
// published file that cannot be taken leaves the held one in force.
func (self *settlementV2Amendments) held(ctx context.Context) *amendment.File {
	if self.source == nil {
		return self.file
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	now := self.now()
	if self.file == nil || !now.Before(self.refreshed.Add(settlementV2AmendmentsRefresh)) {
		file, problem := self.source.Refresh(ctx)
		if file != nil {
			self.file = file
		}
		if ctx.Err() == nil {
			self.refreshed = now
		}
		if problem != nil {
			heldNote := "the last verified file stays in force"
			if self.file == nil {
				heldNote = "no verified file is held, so the ledger audit decides nothing until one is"
			}
			fmt.Printf("settlement v2 audit: amendments: %v; %s\n", problem, heldNote)
		}
	}
	return self.file
}

// E_x for pool noID over a root's covered epochs first … last: the
// first of them the extender pool amendment covers, 0 when it covers
// none. With no verified file held it is amendment.ErrUnknown.
func (self *settlementV2Amendments) extendersFrom(ctx context.Context, noId, first, last uint64) (uint64, error) {
	file := self.held(ctx)
	if file == nil {
		return 0, amendment.ErrUnknown
	}
	return settlementV2ExtendersFrom(file, self.fallback, noId, first, last)
}

// The first epoch of first … last that the extender pool amendment covers for
// noID, 0 for none. A version 1 entry covers every epoch from its first, and
// configuration stands in only before a signed entry, so the covered epochs
// run from it to last; anything else is refused, as ledger.Verify can state
// only that.
func settlementV2ExtendersFrom(file *amendment.File, fallback *amendment.Fallback, noId, first, last uint64) (uint64, error) {
	var from uint64
	if last < first {
		return 0, nil
	}
	for epoch := first; ; epoch++ {
		in, err := file.At(noId, epoch, fallback)
		if err != nil {
			return 0, err
		}
		switch covered := in.Extenders != nil; {
		case covered && from == 0:
			from = epoch
		case !covered && from != 0:
			return 0, fmt.Errorf("settlement v2 audit: the extender pool amendment covers epoch %d of pool %d and not epoch %d", from, noId, epoch)
		}
		if epoch == last {
			return from, nil
		}
	}
}

// Reads --amendments-fallback: one configured line per line of the file,
// blank lines and lines starting with "#" passed over. The lines are
// checked when the audit is built.
func readSettlementV2AmendmentsFallback(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--amendments-fallback: %w", err)
	}
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("--amendments-fallback: %w", err)
	}
	return lines, nil
}
