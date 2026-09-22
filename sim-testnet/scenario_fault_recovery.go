// Recovery owns a bounded cleanup context independent of the failed observer.
// Cohort controls may require more than one ordinary request timeout.
package main

import (
	"context"
	"time"
)

const scenarioFaultRecoveryMaximumTimeout = 30 * time.Minute

// Drivers can account for actual outstanding targets; absence keeps the
// existing small cleanup allowance for ordinary single-process faults.
type scenarioFaultRecoveryBudget interface {
	RecoveryTimeout() time.Duration
}

// Invalid proposals cannot remove the finite bound or shorten ordinary cleanup.
func newScenarioFaultRecoveryContext(driver scenarioFaultDriver) (context.Context, context.CancelFunc) {
	timeout := 30 * time.Second
	if budget, ok := driver.(scenarioFaultRecoveryBudget); ok {
		timeout = max(timeout, min(budget.RecoveryTimeout(), scenarioFaultRecoveryMaximumTimeout))
	}
	return context.WithTimeout(context.Background(), timeout)
}
