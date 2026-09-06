// Observe complete measurement and envelope authentication at their real
// fixture boundaries. Metadata never grants authority or changes scheduling.
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const (
	finalSemanticFixtureMeasurementReady = "measurement-ready"
	finalSemanticFixtureEnvelopeStart    = "envelope-start"
	finalSemanticFixtureEnvelopeEnd      = "envelope-end"
	finalSemanticFixtureOperatorPrepared = "operator-prepared"
	finalSemanticFixtureOperatorBody     = "operator-body"
)

// Scalar identities are immutable detached metadata, never a key, artifact,
// decoded result, cancellation decision or callback into a fixture owner.
type finalSemanticFixtureMeasurementWorkEvent struct {
	stage           string
	validatorID     uint64
	settlementEpoch uint64
	noID            uint64
}

// One call may report from two owners; the observer must be concurrency-safe.
// The nil path executes exactly the same public validators and record bodies.
func (self finalSemanticFixtureWorkControl) observeMeasurement(stage string, validatorID, settlementEpoch, noID uint64) {
	if self.measurementObserved != nil {
		self.measurementObserved(finalSemanticFixtureMeasurementWorkEvent{stage: stage, validatorID: validatorID, settlementEpoch: settlementEpoch, noID: noID})
	}
}

// A real record/envelope owner pairs its own entry and exit, independently of
// the existing four-stage audit and only for the joined lifetime of this call.
func (self finalSemanticFixtureWorkControl) enterMeasurement(stage string, validatorID, settlementEpoch, noID uint64) (func(), error) {
	if self.ctx == nil {
		return nil, errors.New("fixture measurement stage context is absent")
	}
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	if self.measurementEntered != nil {
		leave, err := self.measurementEntered(self.ctx, finalSemanticFixtureMeasurementWorkEvent{stage: stage, validatorID: validatorID, settlementEpoch: settlementEpoch, noID: noID})
		if err != nil {
			return nil, err
		}
		if leave == nil {
			return nil, errors.New("fixture measurement stage lacks its owned exit")
		}
		return leave, nil
	}
	return func() {}, nil
}

// This call-local audit owns only the finite cold graph's70 scalar events.
// It is safe for concurrent observations; snapshots never share its backing slice.
type finalSemanticFixtureMeasurementAudit struct {
	stateLock sync.Mutex
	events    []finalSemanticFixtureMeasurementWorkEvent
}

// Ordering is assigned at the actual boundary, not inferred from dispatch.
func (self *finalSemanticFixtureMeasurementAudit) observe(event finalSemanticFixtureMeasurementWorkEvent) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.events = append(self.events, event)
}

// Return owned metadata only after the complete cold graph has joined.
func (self *finalSemanticFixtureMeasurementAudit) snapshot() []finalSemanticFixtureMeasurementWorkEvent {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]finalSemanticFixtureMeasurementWorkEvent(nil), self.events...)
}

// Read an owned copy of a successful complete graph's published observation.
func finalSemanticFixtureMeasurementEvents() []finalSemanticFixtureMeasurementWorkEvent {
	finalSemanticFixtureCache.stateLock.Lock()
	defer finalSemanticFixtureCache.stateLock.Unlock()
	return append([]finalSemanticFixtureMeasurementWorkEvent(nil), finalSemanticFixtureCache.measurementWork...)
}

// Operator waves are scoped to one chronological validator/epoch; envelope
// owners share one separate ten-job completion stage after both lanes join.
type finalSemanticFixtureMeasurementOwner struct {
	stage           string
	validatorID     uint64
	settlementEpoch uint64
}

// First-wave barriers live inside actual public-seal/record bodies. This
// audit never shares counters or barriers with the original four-stage audit.
type finalSemanticFixtureMeasurementStageAudit struct {
	stateLock sync.Mutex
	stages    map[finalSemanticFixtureMeasurementOwner]finalSemanticFixtureStageObservation
	barriers  map[finalSemanticFixtureMeasurementOwner]chan struct{}
}

// Each complete cold graph owns its eleven independent bounded observations.
func newFinalSemanticFixtureMeasurementStageAudit() *finalSemanticFixtureMeasurementStageAudit {
	return &finalSemanticFixtureMeasurementStageAudit{
		stages:   map[finalSemanticFixtureMeasurementOwner]finalSemanticFixtureStageObservation{},
		barriers: map[finalSemanticFixtureMeasurementOwner]chan struct{}{},
	}
}

// Pair every admitted owner's leave, including cancellation while the first
// actual body wave is gathering. No worker can survive the enclosing join.
func (self *finalSemanticFixtureMeasurementStageAudit) enter(ctx context.Context, event finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
	if ctx == nil || event.validatorID < 1 || event.validatorID > 2 || event.settlementEpoch < 10 || event.settlementEpoch > 14 {
		return nil, errors.New("fixture measurement stage owner is invalid")
	}
	key := finalSemanticFixtureMeasurementOwner{stage: event.stage, validatorID: event.validatorID, settlementEpoch: event.settlementEpoch}
	width := 2
	if event.stage == finalSemanticFixtureEnvelopeStart && event.noID == 0 {
		key.validatorID, key.settlementEpoch = 0, 0
		width = 4
	} else if event.stage != finalSemanticFixtureOperatorBody || event.noID < 1 || event.noID > 2 {
		return nil, fmt.Errorf("fixture measurement body stage is invalid: %+v", event)
	}
	self.stateLock.Lock()
	barrier := self.barriers[key]
	if barrier == nil {
		barrier = make(chan struct{})
		self.barriers[key] = barrier
	}
	value := self.stages[key]
	value.entered++
	value.active++
	value.maximum = max(value.maximum, value.active)
	self.stages[key] = value
	if value.entered == width {
		close(barrier)
	}
	self.stateLock.Unlock()
	leave := func() {
		self.stateLock.Lock()
		value := self.stages[key]
		value.active--
		value.completed++
		self.stages[key] = value
		self.stateLock.Unlock()
	}
	select {
	case <-barrier:
		return leave, nil
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
}

// Publish counts only; readers receive no lock, channel or mutable map alias.
func (self *finalSemanticFixtureMeasurementStageAudit) snapshot() map[finalSemanticFixtureMeasurementOwner]finalSemanticFixtureStageObservation {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	values := make(map[finalSemanticFixtureMeasurementOwner]finalSemanticFixtureStageObservation, len(self.stages))
	for key, value := range self.stages {
		values[key] = value
	}
	return values
}
