//go:build linux || darwin

// Discovery reads the activation journal in fixed ranges from the deployment
// block. A range that ends at or below the finalized EVM head never changes,
// so a process reads it once for each permitted hotkey and keeps the rows.
// Each refresh then reads only the open range at the head, plus the closed
// ranges again for hotkeys that newly hold a permit, all of those hotkeys in
// one OR-list. A process starts with nothing kept: its first scan reads every
// closed range, two at a time after a random start delay, and keeps each one
// it completes even when the refresh fails, so a scan cut by the deadline
// continues where it stopped. Kept rows still pass the canonical-hash recheck
// on every refresh.
package validator

import (
	"cmp"
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// Concurrent reads of closed ranges that nothing is kept for. The open range
// and backfills are read one at a time. A roll restarts several processes at
// once against one node, which also serves the validator.
const validatorUploadColdScanParallelism = 2

// A process that keeps nothing starts a cold scan of at least this many
// ranges after a random delay below maximumValidatorUploadColdScanJitter, so
// the processes of one roll do not all scan at once.
const validatorUploadColdScanJitterRanges = 8
const maximumValidatorUploadColdScanJitter = 20 * time.Second

func validatorUploadColdScanJitter() time.Duration {
	return rand.N(maximumValidatorUploadColdScanJitter)
}

// The rows that all closed ranges may keep together, about 16 MiB. A range
// past it is not kept and is read again on every refresh, as before the cache.
// Only permitted hotkeys' activations are kept.
const maximumValidatorUploadKeptRows = 1 << 17

// One discovery range. A closed range is full and ends at or below the
// finalized head.
type validatorUploadRange struct {
	from   uint64
	to     uint64
	closed bool
}

// The fixed ranges from start through head. Boundaries are start plus whole
// multiples of blocksPerRange, so they do not move as the head advances.
func validatorUploadDiscoveryRanges(start, head, blocksPerRange, maximumRanges uint64) ([]validatorUploadRange, error) {
	if blocksPerRange == 0 || head < start {
		return nil, errors.New("validator staging discovery range exceeds its approved domain or bounds")
	}
	var ranges []validatorUploadRange
	for from := start; ; from += blocksPerRange {
		if uint64(len(ranges)) == maximumRanges {
			return nil, errors.New("validator staging complete discovery exceeds its explicit range budget")
		}
		to := from + blocksPerRange - 1
		closed := to <= head
		if !closed {
			to = head
		}
		ranges = append(ranges, validatorUploadRange{from: from, to: to, closed: closed})
		if to == head {
			return ranges, nil
		}
	}
}

// A closed range's rows for the hotkeys it was read for, in canonical order.
// The range cannot gain rows for those hotkeys later.
type validatorUploadClosedRange struct {
	to      uint64
	hotkeys map[[32]byte]bool
	events  []ValidatorUploadActivationEvent
}

// One eth_getLogs read of a range, for some hotkeys.
type validatorUploadRangeRead struct {
	index   int
	from    uint64
	to      uint64
	hotkeys [][32]byte
}

// The permitted rows of each range, in range order, the number of range reads
// made and the number of closed ranges answered from kept rows alone. Closed ranges with nothing kept are read first, several at a
// time only while the process has kept nothing; then the open range and the
// backfills of newly permitted hotkeys are read one at a time. Every row of a
// block that this refresh did not read is rechecked against the canonical
// block hash.
//
// A changed canonical hash discredits every kept range, not only the one
// that showed it: they are all dropped, so the next refresh scans cold, as a
// new process does, and reads the chain as it is now.
func (self *ValidatorUploadAdmission) discoverValidatorUploadRanges(ctx context.Context, ranges []validatorUploadRange, hotkeys [][32]byte, observer ValidatorUploadObserver) (_ [][]ValidatorUploadActivationEvent, _ int, _ int, resultErr error) {
	defer func() {
		if errors.Is(resultErr, errValidatorUploadCanonicalChanged) {
			clear(self.closedRanges)
			self.closedRangeRows = 0
		}
	}()
	result := make([][]ValidatorUploadActivationEvent, len(ranges))
	// An empty census reads nothing.
	if len(hotkeys) == 0 {
		return result, 0, 0, ctx.Err()
	}
	permitted := make(map[[32]byte]bool, len(hotkeys))
	for _, hotkey := range hotkeys {
		permitted[hotkey] = true
	}
	var cold, warm []validatorUploadRangeRead
	for index, scanned := range ranges {
		request := validatorUploadRangeRead{index: index, from: scanned.from, to: scanned.to, hotkeys: hotkeys}
		kept := self.closedRanges[scanned.from]
		switch {
		case !scanned.closed:
			warm = append(warm, request)
		case kept == nil || kept.to != scanned.to:
			cold = append(cold, request)
		default:
			request.hotkeys = nil
			for _, hotkey := range hotkeys {
				if !kept.hotkeys[hotkey] {
					request.hotkeys = append(request.hotkeys, hotkey)
				}
			}
			if len(request.hotkeys) != 0 {
				warm = append(warm, request)
			}
		}
	}
	if len(cold) >= validatorUploadColdScanJitterRanges && len(self.closedRanges) == 0 && self.coldScanJitter != nil {
		if delay := self.coldScanJitter(); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, 0, 0, ctx.Err()
			case <-timer.C:
			}
		}
	}
	read := func(ctx context.Context, request validatorUploadRangeRead) ([]ValidatorUploadActivationEvent, error) {
		return self.chain.ValidatorUploadActivationEventsContext(ctx, self.config.Deployment, request.from, request.to, self.config.BlocksPerRange, maximumValidatorUploadRangeRows, request.hotkeys, observer)
	}
	// The read rechecked each of its rows' blocks.
	checked := make(map[uint64][32]byte)
	served := make([]bool, len(ranges))
	// One completed read: an open range's rows are this refresh's; a closed
	// range's join its kept rows and stay kept.
	keep := func(request validatorUploadRangeRead, rows []ValidatorUploadActivationEvent) error {
		for _, event := range rows {
			checked[event.Block] = event.BlockHash
		}
		served[request.index] = true
		scanned := ranges[request.index]
		if !scanned.closed {
			result[request.index] = rows
			return nil
		}
		// Rows of hotkeys that lost their permit are dropped; such a
		// hotkey is read again if it regains one.
		var prior []ValidatorUploadActivationEvent
		hotkeysRead := make(map[[32]byte]bool, len(hotkeys))
		if kept := self.closedRanges[scanned.from]; kept != nil && kept.to == scanned.to {
			for hotkey := range kept.hotkeys {
				if permitted[hotkey] {
					hotkeysRead[hotkey] = true
				}
			}
			for _, event := range kept.events {
				if permitted[event.Hotkey] {
					prior = append(prior, event)
				}
			}
		}
		for _, hotkey := range request.hotkeys {
			hotkeysRead[hotkey] = true
		}
		events, err := mergeValidatorUploadRangeEvents(prior, rows)
		if err != nil {
			return err
		}
		self.keepValidatorUploadClosedRange(scanned.from, &validatorUploadClosedRange{to: scanned.to, hotkeys: hotkeysRead, events: events})
		result[request.index] = events
		return nil
	}
	// Each completed read is kept before a failed one fails the refresh.
	for _, reads := range []struct {
		requests []validatorUploadRangeRead
		parallel int
	}{{cold, validatorUploadColdScanParallelism}, {warm, 1}} {
		rows, done, err := readValidatorUploadRanges(ctx, reads.requests, reads.parallel, read)
		for position, request := range reads.requests {
			if done[position] {
				if err := keep(request, rows[position]); err != nil {
					return nil, 0, 0, err
				}
			}
		}
		if err != nil {
			return nil, 0, 0, err
		}
	}
	answeredFromKept := 0
	for index, scanned := range ranges {
		if served[index] {
			continue
		}
		kept := self.closedRanges[scanned.from]
		if kept == nil {
			return nil, 0, 0, errors.New("validator staging closed range was neither kept nor read")
		}
		answeredFromKept++
		for _, event := range kept.events {
			if permitted[event.Hotkey] {
				result[index] = append(result[index], event)
			}
		}
	}
	for _, events := range result {
		for _, event := range events {
			hash, done := checked[event.Block]
			if !done {
				if err := self.chain.recheckValidatorUploadBlockContext(ctx, event.Block, event.BlockHash); err != nil {
					return nil, 0, 0, err
				}
				checked[event.Block], hash = event.BlockHash, event.BlockHash
			}
			if hash != event.BlockHash {
				return nil, 0, 0, errValidatorUploadCanonicalChanged
			}
		}
	}
	return result, len(cold) + len(warm), answeredFromKept, ctx.Err()
}

// The kept rows of a range and a backfill's rows are of different hotkeys.
// Their union passes the checks that one read of the range makes. Two hashes
// for one block mean the chain changed under the kept rows.
func mergeValidatorUploadRangeEvents(prior, rows []ValidatorUploadActivationEvent) ([]ValidatorUploadActivationEvent, error) {
	events := make([]ValidatorUploadActivationEvent, 0, len(prior)+len(rows))
	events = append(append(events, prior...), rows...)
	if len(events) > maximumValidatorUploadRangeRows {
		return nil, errors.New("validator staging event result is absent or exceeds its bound")
	}
	slices.SortStableFunc(events, func(a, b ValidatorUploadActivationEvent) int {
		if order := cmp.Compare(a.Block, b.Block); order != 0 {
			return order
		}
		return cmp.Compare(a.Index, b.Index)
	})
	digests := make(map[[32]byte]bool, len(events))
	for index, event := range events {
		if digests[event.Digest] {
			return nil, errors.New("validator staging event digest is duplicated")
		}
		digests[event.Digest] = true
		if index == 0 || event.Block != events[index-1].Block {
			continue
		}
		if event.BlockHash != events[index-1].BlockHash {
			return nil, errValidatorUploadCanonicalChanged
		}
		if event.Index == events[index-1].Index {
			return nil, errors.New("validator staging events are not a strict canonical order")
		}
	}
	return events, nil
}

// Replaces a range's kept rows, unless that would pass the kept-row budget.
func (self *ValidatorUploadAdmission) keepValidatorUploadClosedRange(from uint64, entry *validatorUploadClosedRange) {
	if prior := self.closedRanges[from]; prior != nil {
		self.closedRangeRows -= len(prior.events)
		delete(self.closedRanges, from)
	}
	if self.closedRangeRows+len(entry.events) > self.closedRangeRowBudget {
		return
	}
	self.closedRanges[from] = entry
	self.closedRangeRows += len(entry.events)
}

// Makes reads at most parallel at a time and returns their rows in request
// order, with which of them completed. The first failure cancels the reads in
// flight, starts no others and is the error returned.
func readValidatorUploadRanges(ctx context.Context, requests []validatorUploadRangeRead, parallel int, read func(context.Context, validatorUploadRangeRead) ([]ValidatorUploadActivationEvent, error)) ([][]ValidatorUploadActivationEvent, []bool, error) {
	rows := make([][]ValidatorUploadActivationEvent, len(requests))
	done := make([]bool, len(requests))
	if len(requests) == 0 {
		return rows, done, ctx.Err()
	}
	parallel = max(parallel, 1)
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	slots := make(chan struct{}, parallel)
	var group sync.WaitGroup
	var failure error
	var failureOnce sync.Once
	for index := range requests {
		select {
		case slots <- struct{}{}:
		case <-readCtx.Done():
		}
		// A failed read cancels before it frees its slot, so no read starts
		// after a failure.
		if readCtx.Err() != nil {
			break
		}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			value, err := read(readCtx, requests[index])
			if err != nil {
				failureOnce.Do(func() {
					failure = err
					cancel()
				})
				return
			}
			rows[index], done[index] = value, true
		}()
	}
	group.Wait()
	if failure != nil {
		return rows, done, failure
	}
	return rows, done, ctx.Err()
}
