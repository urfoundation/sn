// Missing native observations are distinct from changed transcripts or an
// unfinished intent. This diagnostic never grants permission to bridge history.
package validator

import "fmt"

// Constructed only for a forward jump larger than one. The range describes
// absent local commits, not proof that no native submission exists elsewhere.
type nativeEpochGapError struct {
	component     string
	previousEpoch uint64
	currentEpoch  uint64
}

// Keep the missing range explicit; another poll cannot create its observation.
func (self *nativeEpochGapError) Error() string {
	return fmt.Sprintf("%s epoch jumped from %d to %d: no retained commit for native epochs %d through %d; strict continuation requires authorized history recovery", self.component, self.previousEpoch, self.currentEpoch, self.previousEpoch+1, self.currentEpoch-1)
}
