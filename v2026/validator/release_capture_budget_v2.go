//go:build linux || darwin

// A synchronous capture retains each provenance observation while charging
// equal content once. Separate data/control owners prevent metadata from
// borrowing the much larger proof-tape allowance.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
)

// No worker, mutable global or external call under a lock exists here.
type releaseEvidenceCaptureBudgetV2 struct {
	ctx              context.Context
	remaining        uint64
	dataRemaining    uint64
	controlRemaining uint64
	maximumObjects   uint64
	sources          map[ReleaseEvidenceV2CaptureSource][32]byte
	content          map[[32]byte]uint8
	retain           func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error
}

// Zero optional class fields preserve the original aggregate-only interface.
func newReleaseEvidenceCaptureBudgetV2(ctx context.Context, options ReleaseEvidenceV2CaptureOptions, retain func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error) (*releaseEvidenceCaptureBudgetV2, error) {
	if ctx == nil || retain == nil || options.MaximumBytes == 0 || options.MaximumBytes > uint64(^uint64(0)>>1) || options.MaximumObjects == 0 || options.MaximumObjects > uint64(^uint(0)>>1) {
		return nil, errors.New("compact capture budget owner is absent or exceeds signed allocation bounds")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, control := options.MaximumDataBytes, options.MaximumControlBytes
	if data == 0 {
		data = options.MaximumBytes
	}
	if control == 0 {
		control = options.MaximumBytes
	}
	if data > options.MaximumBytes || control > options.MaximumBytes {
		return nil, errors.New("compact capture class exceeds its complete byte ceiling")
	}
	return &releaseEvidenceCaptureBudgetV2{ctx: ctx, remaining: options.MaximumBytes, dataRemaining: data, controlRemaining: control, maximumObjects: options.MaximumObjects, sources: map[ReleaseEvidenceV2CaptureSource][32]byte{}, content: map[[32]byte]uint8{}, retain: retain}, nil
}

// Class attribution is separate from global content identity: equal bytes in
// a control source still consume control capacity, even if a tape named them.
func (self *releaseEvidenceCaptureBudgetV2) emit(source ReleaseEvidenceV2CaptureSource, encoded []byte) error {
	if self == nil || self.ctx == nil || self.retain == nil {
		return errors.New("compact capture budget owner is absent")
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if len(encoded) == 0 || source.Name == "" || source.Kind == "" {
		return errors.New("compact capture source is empty")
	}
	hash := sha256.Sum256(encoded)
	if previous, found := self.sources[source]; found {
		if previous != hash {
			return errors.New("compact capture source changed during collection")
		}
		return nil
	}
	class := uint8(2)
	classRemaining := self.controlRemaining
	if source.Kind == AttemptStreamV2Records || source.Kind == AttemptStreamV2Proofs {
		class, classRemaining = 1, self.dataRemaining
	}
	seenClasses := self.content[hash]
	size := uint64(len(encoded))
	if uint64(len(self.sources)) >= self.maximumObjects || seenClasses == 0 && size > self.remaining || seenClasses&class == 0 && size > classRemaining {
		return errors.New("compact capture exceeds its approved object/data/control capacity")
	}
	if err := self.retain(self.ctx, source, encoded); err != nil {
		return err
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if seenClasses == 0 {
		self.remaining -= size
	}
	if seenClasses&class == 0 {
		if class == 1 {
			self.dataRemaining -= size
		} else {
			self.controlRemaining -= size
		}
	}
	self.sources[source] = hash
	self.content[hash] = seenClasses | class
	return nil
}
