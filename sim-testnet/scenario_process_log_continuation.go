// Provisional observation may outlive specific repairable process findings.
// Their evidence and final verdict stay blocking; unsafe/unknown errors stop.
package main

import "fmt"

// Carry the actual classified findings instead of classifying error strings.
type processLogFindingsFailure struct {
	findings []ProcessLogFinding
}

// Preserve the public error wording used by existing reports and diagnostics.
func (self *processLogFindingsFailure) Error() string {
	if self == nil || len(self.findings) == 0 {
		return "process log gate has no classified findings"
	}
	first := self.findings[0]
	return fmt.Sprintf("process log gate found %d release-blocking class(es); first=%s/%s/%s count=%d", len(self.findings), first.ProcessID, first.Stream, first.Class, first.Count)
}

// Defer only explicitly repairable classes during an authorized non-accepting
// interval. No persisted flag, unknown class or joined I/O error grants this.
func scenarioProcessLogFailureDeferred(cfg *ResolvedConfig, phase string, failure error, gates ...scenarioProcessLogGate) bool {
	if failure == nil || !provisionalResumeEnabled(cfg) || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance || cfg.readOnlyAudit || phase != "release-1.0" && phase != "production-soak" {
		return false
	}
	var repairable func(error) bool
	repairable = func(failure error) bool {
		switch failure := failure.(type) {
		case *processLogFindingsFailure:
			if failure == nil || len(failure.findings) == 0 {
				return false
			}
			for _, finding := range failure.findings {
				switch finding.Class {
				case "release-steering-attempt-failure", "release-steering-continuity", "tls-handshake-timeout", "packet-read-timeout", "connection-close-timeout", "exit-gap-timeout", "restart-stale-contract":
				case "warning":
					if len(gates) != 1 {
						return false
					}
					owner, ok := gates[0].(interface {
						provisionalArtifactStreamCancellation(ProcessLogFinding) bool
					})
					if !ok || !owner.provisionalArtifactStreamCancellation(finding) {
						return false
					}
				default:
					return false
				}
			}
			return true
		case interface{ Unwrap() []error }:
			children := failure.Unwrap()
			if len(children) == 0 {
				return false
			}
			for _, child := range children {
				if !repairable(child) {
					return false
				}
			}
			return true
		case interface{ Unwrap() error }:
			return repairable(failure.Unwrap())
		default:
			return false
		}
	}
	return repairable(failure)
}
