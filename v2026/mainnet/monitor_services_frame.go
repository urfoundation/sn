// Explicit whole-fee roles carry a populated original account roster. Their
// finite frame is shared by live loading and repair; legacy policy stays small.
package main

import "errors"

const maxMonitorFeeServicesBytes = maxMonitorServicesBytes + maximumMonitorEconomicRoles*nativeProducerFeeAuthorityLimit

// Each independently declared complete fee role admits one four-MiB frame.
// A malformed roster cannot grant capacity, and all other semantic policy
// checks still run before any service owner starts.
func (self monitorServicesPolicy) validateFrame(bytes int) error {
	if bytes < 0 || len(self.NativeEconomics) > maximumMonitorEconomicRoles {
		return errors.New("monitor policy exceeds its declared original role frame")
	}
	maximum := maxMonitorServicesBytes
	for _, role := range self.NativeEconomics {
		execution := role.Observation.Execution
		if execution != nil && execution.FeeCensus != nil {
			if err := execution.FeeCensus.validate(); err != nil {
				return err
			}
			maximum += nativeProducerFeeAuthorityLimit
		}
	}
	if bytes > maximum {
		return errors.New("monitor policy exceeds its declared original role frame")
	}
	return nil
}
