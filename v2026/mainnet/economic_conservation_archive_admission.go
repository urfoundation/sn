// Cold admission owns a private index until its complete original lineage and
// all held physical owners pass. Prefix work is reused only inside that call;
// restart and separate commands authenticate their own complete history.
package main

import (
	"errors"
	"os"
)

// Each original snapshot was authenticated by the actual reader before its
// facts entered this private index. Rechecking every growing physical prefix
// for every segment is quadratic and cannot publish an earlier result. Defer
// that complete fence to finishAdmission; cancellation still stops each step.
// A returned live view has no admission context, so all ordinary checks remain
// complete. No age, hash-only shortcut or process-global cache grants custody.
func (self *economicConservationArchiveView) checkAdmission() error {
	if self == nil || self.closed {
		return os.ErrClosed
	}
	if self.admission != nil {
		return self.admission.Err()
	}
	return self.check()
}

// The callback observes a real boundary and supplies no bytes or verdict.
// Failure leaves the index private; the opener closes every retained owner
// and returns nil. No partial admission can seed a later owner or sibling.
func (self *economicConservationArchiveView) finishAdmission() error {
	if self == nil || self.closed || self.admission == nil {
		return os.ErrClosed
	}
	if self.claimWork != nil {
		self.claimWork(economicConservationRole, "archive-index-ready", 1)
	}
	if err := errors.Join(self.admission.Err(), self.check()); err != nil {
		return err
	}
	self.admission = nil
	return nil
}
