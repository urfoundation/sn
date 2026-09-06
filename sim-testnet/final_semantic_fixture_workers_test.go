// Bounded fixture owners prepare independent signed graphs; only the caller
// publishes joined results. Production authentication remains unchanged.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Only one worker owns these mutable histories and outputs. Each epoch consumes
// its own predecessor before the caller joins the complete validator lane.
type finalSemanticFixtureValidatorLane struct {
	previousMeasurement []byte
	previousArtifact    *validatorpkg.ReleaseMeasurementArtifact
	attemptLedgers      map[uint64]*finalAttemptFixtureLedger
	cycles              []FinalCRv4Cycle
	measurements        []finalSemanticFixtureMeasurementJob
}

// Call-local controls never outlive a joined dispatch. Entry hooks run inside
// the actual preparation stage, not in the generic goroutine launcher.
type finalSemanticFixtureWorkControl struct {
	ctx                 context.Context
	observer            finalSemanticFixtureWorkObserver
	entered             func(context.Context, string, int) (func(), error)
	measurementObserved func(finalSemanticFixtureMeasurementWorkEvent)
	measurementEntered  func(context.Context, finalSemanticFixtureMeasurementWorkEvent) (func(), error)
	namespaceWork       finalSemanticNamespaceWork
}

// Complete all admitted owners before reporting the canonical first failure.
// Observed widths count actual goroutine admissions, never configured capacity.
func (self finalSemanticFixtureWorkControl) run(stage string, count, width int, prepare func(context.Context, int) error) error {
	if self.ctx == nil || count < 0 || width < 1 || width > 4 || prepare == nil {
		return errors.New("fixture work context, count or width is invalid")
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	for first := 0; first < count; first += width {
		batchContext, cancel := context.WithCancel(self.ctx)
		size := min(width, count-first)
		started := make(chan struct{}, size)
		start := make(chan struct{})
		errs := make([]error, size)
		var owners sync.WaitGroup
		for offset := 0; offset < size; offset++ {
			errs[offset] = errors.New("fixture owner did not return")
			owners.Add(1)
			go func(offset int) {
				defer owners.Done()
				defer func() {
					if errs[offset] != nil {
						cancel()
					}
				}()
				started <- struct{}{}
				<-start
				if err := self.ctx.Err(); err != nil {
					errs[offset] = err
					return
				}
				errs[offset] = prepare(batchContext, first+offset)
			}(offset)
		}
		for range size {
			<-started
		}
		observeFinalSemanticFixtureWork(self.observer, stage, first, size)
		close(start)
		owners.Wait()
		cancel()
		// Sibling cancellation only releases barriers; it cannot hide the
		// earliest actual preparation error behind a canceled earlier slot.
		for offset, err := range errs {
			if err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("fixture %s job %d: %w", stage, first+offset, err)
			}
		}
		for offset, err := range errs {
			if err != nil {
				return fmt.Errorf("fixture %s job %d: %w", stage, first+offset, err)
			}
		}
		if err := self.ctx.Err(); err != nil {
			return err
		}
	}
	return self.ctx.Err()
}

// Stage entries and exits are paired by the preparation owner itself.
func (self finalSemanticFixtureWorkControl) enter(stage string, index int) (func(), error) {
	if self.ctx == nil {
		return nil, errors.New("fixture stage context is absent")
	}
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	if self.entered != nil {
		leave, err := self.entered(self.ctx, stage, index)
		if err != nil {
			return nil, err
		}
		if leave == nil {
			return nil, errors.New("fixture stage entry did not return its owned exit")
		}
		return leave, nil
	}
	return func() {}, nil
}

// One owner stores immutable copies and rejects conflicting repeated names.
// It is intentionally not safe for concurrent use.
type finalSemanticFixtureArtifacts struct {
	values map[string]finalSemanticFixtureArtifact
	err    error
}

// A staged value binds its kind and exact locator to owned wire bytes.
type finalSemanticFixtureArtifact struct {
	locator FinalArtifactLocator
	data    []byte
}

// The production-compatible callback does not publish to another owner.
func (self *finalSemanticFixtureArtifacts) derive(kind, uri string, data []byte) (FinalArtifactLocator, error) {
	if self.err != nil {
		return FinalArtifactLocator{}, self.err
	}
	locator := FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	if kind == "" || uri == "" || len(data) == 0 {
		self.err = errors.New("fixture staged artifact identity or bytes are empty")
		return FinalArtifactLocator{}, self.err
	}
	if self.values == nil {
		self.values = map[string]finalSemanticFixtureArtifact{}
	}
	if prior, exists := self.values[uri]; exists && (prior.locator != locator || !bytes.Equal(prior.data, data)) {
		self.err = fmt.Errorf("fixture artifact %s conflicts with its staged identity or bytes", uri)
		return FinalArtifactLocator{}, self.err
	}
	self.values[uri] = finalSemanticFixtureArtifact{locator: locator, data: append([]byte(nil), data...)}
	return locator, nil
}

// Fixture sealers use the historic callback shape. A sticky error is checked
// before the owner can publish any staged result.
func (self *finalSemanticFixtureArtifacts) artifact(kind, name string, data []byte) FinalArtifactLocator {
	locator, _ := self.derive(kind, "final-derived/"+name, data)
	return locator
}

// Preflight the complete canonical union before mutating any destination.
// Returned slots own their bytes and are sorted independently of completion.
func joinFinalSemanticFixtureArtifacts(destination map[string][]byte, owners []*finalSemanticFixtureArtifacts) ([]finalSemanticFixtureArtifact, error) {
	joined := finalSemanticFixtureArtifacts{}
	for _, owner := range owners {
		if owner == nil {
			return nil, errors.New("fixture artifact owner is absent")
		}
		if owner.err != nil {
			return nil, owner.err
		}
		names := make([]string, 0, len(owner.values))
		for name := range owner.values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := owner.values[name]
			if value.locator.URI != name || value.locator.ContentHash != bytesSHA256(value.data) || value.locator.SizeBytes != uint64(len(value.data)) {
				return nil, fmt.Errorf("fixture artifact %s changed after staging", name)
			}
			if existing, exists := destination[name]; exists && !bytes.Equal(existing, value.data) {
				return nil, fmt.Errorf("fixture artifact %s conflicts with published bytes", name)
			}
			if _, err := joined.derive(value.locator.Kind, name, value.data); err != nil {
				return nil, err
			}
		}
	}
	names := make([]string, 0, len(joined.values))
	for name := range joined.values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]finalSemanticFixtureArtifact, 0, len(names))
	for _, name := range names {
		result = append(result, joined.values[name])
	}
	return result, nil
}

// Publishing is caller-only and follows a successful complete preflight.
func publishFinalSemanticFixtureArtifacts(destination map[string][]byte, joined []finalSemanticFixtureArtifact) {
	for _, value := range joined {
		destination[value.locator.URI] = append([]byte(nil), value.data...)
	}
}

// Real stage observations contain only detached counts, never fixture owners.
type finalSemanticFixtureStageObservation struct {
	entered   int
	completed int
	active    int
	maximum   int
}

// The cold graph uses deterministic first-batch barriers inside real work.
// All observations remain call-local until the completed graph is published.
type finalSemanticFixtureStageAudit struct {
	stateLock sync.Mutex
	stages    map[string]finalSemanticFixtureStageObservation
	barriers  map[string]chan struct{}
}

// Build one audit with the release graph's independently admitted widths.
func newFinalSemanticFixtureStageAudit() *finalSemanticFixtureStageAudit {
	return &finalSemanticFixtureStageAudit{
		stages: map[string]finalSemanticFixtureStageObservation{},
		barriers: map[string]chan struct{}{
			finalSemanticFixtureValidatorLanes:     make(chan struct{}),
			finalSemanticFixtureTerminalClosures:   make(chan struct{}),
			finalSemanticFixtureGenerationCalldata: make(chan struct{}),
			finalSemanticFixtureFleetAssembly:      make(chan struct{}),
		},
	}
}

// Only the first actual preparation batch waits for all peer bodies to enter.
func (self *finalSemanticFixtureStageAudit) enter(ctx context.Context, stage string, index int) (func(), error) {
	width := 4
	if stage == finalSemanticFixtureValidatorLanes {
		width = 2
	}
	self.stateLock.Lock()
	barrier, exists := self.barriers[stage]
	if !exists {
		self.stateLock.Unlock()
		return nil, fmt.Errorf("unknown fixture stage %s", stage)
	}
	value := self.stages[stage]
	value.entered++
	value.active++
	value.maximum = max(value.maximum, value.active)
	self.stages[stage] = value
	if value.entered == width {
		close(barrier)
	}
	self.stateLock.Unlock()
	leave := func() {
		self.stateLock.Lock()
		value := self.stages[stage]
		value.active--
		value.completed++
		self.stages[stage] = value
		self.stateLock.Unlock()
	}
	if index < width {
		select {
		case <-barrier:
		case <-ctx.Done():
			leave()
			return nil, ctx.Err()
		}
	}
	return leave, nil
}

// A completed audit never exports its mutable maps or synchronization objects.
func (self *finalSemanticFixtureStageAudit) snapshot() map[string]finalSemanticFixtureStageObservation {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	values := make(map[string]finalSemanticFixtureStageObservation, len(self.stages))
	for stage, value := range self.stages {
		values[stage] = value
	}
	return values
}
