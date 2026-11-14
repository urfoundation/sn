package validator

// The observer belongs to one synchronous verification invocation. It receives
// no mutable input or authority and never replaces equality or JSON encoding.

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

// Ordinary callers use the zero value; regressions observe executed work only.
type attemptAssignmentComparisonWork struct {
	compared   func()
	serialized func()
}

// Valid UTF8 has an injective JSON representation for these fixed fields. Slice
// nilness/order still matters; malformed strings retain JSON's normalization.
func (self attemptAssignmentComparisonWork) equal(left, right []AttemptAssignment) bool {
	if self.compared != nil {
		self.compared()
	}
	if (left == nil) != (right == nil) || len(left) != len(right) {
		return false
	}
	for index := range left {
		if !utf8.ValidString(left[index].Binding.FleetID) || !utf8.ValidString(left[index].Binding.Hotkey) ||
			!utf8.ValidString(right[index].Binding.FleetID) || !utf8.ValidString(right[index].Binding.Hotkey) {
			leftJSON, leftErr := self.marshal(left)
			rightJSON, rightErr := self.marshal(right)
			return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
		}
	}
	for index := range left {
		a, b := &left[index], &right[index]
		if a.NextHop != b.NextHop || a.ServerKeyID != b.ServerKeyID ||
			a.Confirmed != b.Confirmed || a.HasLatency != b.HasLatency || a.LatencyBucket != b.LatencyBucket ||
			a.Binding != b.Binding || (a.Trail == nil) != (b.Trail == nil) || len(a.Trail) != len(b.Trail) ||
			(a.AssignMessage == nil) != (b.AssignMessage == nil) || !bytes.Equal(a.AssignMessage, b.AssignMessage) ||
			(a.AssignSignature == nil) != (b.AssignSignature == nil) || !bytes.Equal(a.AssignSignature, b.AssignSignature) {
			return false
		}
		for hop := range a.Trail {
			if a.Trail[hop] != b.Trail[hop] {
				return false
			}
		}
	}
	return true
}

// Notification is adjacent to the real serializer, not a predicted work count.
func (self attemptAssignmentComparisonWork) marshal(assignments []AttemptAssignment) ([]byte, error) {
	if self.serialized != nil {
		self.serialized()
	}
	return json.Marshal(assignments)
}
