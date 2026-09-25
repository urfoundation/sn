// Log classification and provisional execution share one closed policy.
// Final acceptance continues to use the original blocking finding inventory.
package main

// A process can report availability failure without proving corrupt evidence.
// Unknown and catastrophic outcomes remain distinct and never grant recovery.
type processLogRuntimeCategory uint8

const (
	processLogRuntimeInvalid processLogRuntimeCategory = iota
	processLogRuntimeUnknown
	processLogRuntimeOperational
	processLogRuntimeIntegrity
	processLogRuntimeCatastrophic
)

// The classifier emits a typed member rather than a free-form class name.
// Every member needs one descriptor, enforced by the catalog coverage test.
type processLogClass uint8

const (
	processLogClassNone processLogClass = iota
	processLogClassPanic
	processLogClassFatal
	processLogClassSteeringAttempt
	processLogClassSteeringContinuity
	processLogClassCommitDeadline
	processLogClassReceiveBuffer
	processLogClassTlsTimeout
	processLogClassH3Internal
	processLogClassPacketTimeout
	processLogClassCloseTimeout
	processLogClassExitGap
	processLogClassContractCreate
	processLogClassRestartContract
	processLogClassTokenTimeout
	processLogClassSeedUnavailable
	processLogClassPostgresNull
	processLogClassConnectionCanceled
	processLogClassArtifactCanceled
	processLogClassError
	processLogClassWarning
	processLogClassLogIntegrity
	processLogClassLogOverrun
	processLogClassArtifactFailure
	processLogClassCount
)

type processLogClassDefinition struct {
	name     string
	category processLogRuntimeCategory
}

// Names are the unchanged signed wire identifiers. Recovery policy is compiled
// here, never supplied by a persisted finding or inferred from its message.
func (self processLogClass) definition() processLogClassDefinition {
	switch self {
	case processLogClassPanic:
		return processLogClassDefinition{name: "panic", category: processLogRuntimeCatastrophic}
	case processLogClassFatal:
		return processLogClassDefinition{name: "fatal", category: processLogRuntimeCatastrophic}
	case processLogClassSteeringAttempt:
		return processLogClassDefinition{name: "release-steering-attempt-failure", category: processLogRuntimeOperational}
	case processLogClassSteeringContinuity:
		return processLogClassDefinition{name: "release-steering-continuity", category: processLogRuntimeOperational}
	case processLogClassCommitDeadline:
		return processLogClassDefinition{name: "commit-deadline-warning", category: processLogRuntimeOperational}
	case processLogClassReceiveBuffer:
		return processLogClassDefinition{name: "quic-receive-buffer", category: processLogRuntimeOperational}
	case processLogClassTlsTimeout:
		return processLogClassDefinition{name: "tls-handshake-timeout", category: processLogRuntimeOperational}
	case processLogClassH3Internal:
		return processLogClassDefinition{name: "h3-tls-internal", category: processLogRuntimeOperational}
	case processLogClassPacketTimeout:
		return processLogClassDefinition{name: "packet-read-timeout", category: processLogRuntimeOperational}
	case processLogClassCloseTimeout:
		return processLogClassDefinition{name: "connection-close-timeout", category: processLogRuntimeOperational}
	case processLogClassExitGap:
		return processLogClassDefinition{name: "exit-gap-timeout", category: processLogRuntimeOperational}
	case processLogClassContractCreate:
		return processLogClassDefinition{name: "contract-create", category: processLogRuntimeOperational}
	case processLogClassRestartContract:
		return processLogClassDefinition{name: "restart-stale-contract", category: processLogRuntimeOperational}
	case processLogClassTokenTimeout:
		return processLogClassDefinition{name: "api-token-refresh-timeout", category: processLogRuntimeOperational}
	case processLogClassSeedUnavailable:
		return processLogClassDefinition{name: "seed-unavailable", category: processLogRuntimeOperational}
	case processLogClassPostgresNull:
		return processLogClassDefinition{name: "postgres-null-byte", category: processLogRuntimeIntegrity}
	case processLogClassConnectionCanceled:
		return processLogClassDefinition{name: "connection-canceled", category: processLogRuntimeOperational}
	case processLogClassArtifactCanceled:
		return processLogClassDefinition{name: "artifact-request-canceled", category: processLogRuntimeOperational}
	case processLogClassError:
		return processLogClassDefinition{name: "error", category: processLogRuntimeUnknown}
	case processLogClassWarning:
		return processLogClassDefinition{name: "warning", category: processLogRuntimeUnknown}
	case processLogClassLogIntegrity:
		return processLogClassDefinition{name: "log-integrity", category: processLogRuntimeIntegrity}
	case processLogClassLogOverrun:
		return processLogClassDefinition{name: "log-overrun", category: processLogRuntimeIntegrity}
	case processLogClassArtifactFailure:
		return processLogClassDefinition{name: "artifact-stream-failure", category: processLogRuntimeIntegrity}
	default:
		return processLogClassDefinition{}
	}
}

// Unknown wire classes remain unknown; even a name resembling a known class
// cannot claim that class's execution policy. No wire-format migration occurs.
func processLogRuntimeCategoryForName(name string) processLogRuntimeCategory {
	for class := processLogClassNone + 1; class < processLogClassCount; class++ {
		definition := class.definition()
		if definition.name == name {
			return definition.category
		}
	}
	return processLogRuntimeUnknown
}
