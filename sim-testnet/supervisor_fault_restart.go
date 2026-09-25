// The supervisor owns escalation of its own retained restart intent. Readiness
// still belongs to ordinary fault restoration after a replacement is healthy.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"time"
)

// Give every child at least the longest normal graceful shutdown allowance.
// The grace begins when this supervisor first observes the durable fault, never
// at process startup or at the first failed health probe.
const supervisorFaultRestartGrace = max(supervisorConsumerStopTimeout, supervisorProviderStopTimeout, supervisorInfrastructureStopTimeout)

type supervisorFaultRestartPending struct {
	faultHash string
	identity  supervisedProcessIdentity
	firstSeen time.Time
	signaled  bool
}

type supervisorFaultRestartEscalation struct {
	faultId   string
	processId string
	identity  supervisedProcessIdentity
}

// Only the supervisor event loop calls this state; no other goroutine shares it.
type supervisorFaultRestartRecovery struct {
	pendingProcessIdKVs map[string]supervisorFaultRestartPending
}

// Read and validate the whole retained cohort before signaling any child. The
// injected signal function must preserve the supervisor's immutable kernel,
// executable and argv checks; production uses the same primitive as shutdown.
// Neither a stale fault nor a healthy replacement can inherit the old deadline.
func (self *supervisorFaultRestartRecovery) reconcile(ctx context.Context, now time.Time, stateDir string, commands []supervisedCommand, signal func(supervisedCommand, syscall.Signal) bool) ([]supervisorFaultRestartEscalation, error) {
	if ctx == nil || now.IsZero() || signal == nil {
		return nil, errors.New("supervisor fault restart owner is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	active, err := readActiveFaultFile(filepath.Join(stateDir, "active-faults.json"))
	if err != nil {
		return nil, err
	}
	commandIdKVs := make(map[string]supervisedCommand, len(commands))
	for _, command := range commands {
		if command.spec.ID == "" {
			return nil, errors.New("supervisor restart inventory has an unnamed process")
		}
		if _, duplicate := commandIdKVs[command.spec.ID]; duplicate {
			return nil, errors.New("supervisor restart inventory has duplicate processes")
		}
		commandIdKVs[command.spec.ID] = command
	}
	originalIdKVs := make(map[string]FaultProcessEvidence, len(active.Processes))
	for _, original := range active.Processes {
		originalIdKVs[original.ID] = original
	}
	type candidate struct {
		faultId   string
		faultHash string
		command   supervisedCommand
	}
	candidates := make([]candidate, 0)
	for _, fault := range active.Faults {
		if fault.Kind != "process-restart" {
			continue
		}
		faultHash, err := canonicalHashHex(fault)
		if err != nil {
			return nil, err
		}
		for _, id := range fault.Targets {
			command, ok := commandIdKVs[id]
			original := originalIdKVs[id]
			if !ok || command.spec.Role == "" || command.spec.Identity == "" || command.spec.RestartLimit < 1 || original.Role != command.spec.Role || original.Identity != command.spec.Identity {
				return nil, fmt.Errorf("retained restart %s target %s differs from the supervisor owner", fault.ID, id)
			}
			// A legacy record lacks exact signaling authority. A stopped child
			// or a replacement needs ordinary exit/restart/health handling only.
			if original.StartTimeTicks == 0 || command.cmd == nil || command.cmd.Process == nil || command.cmd.Process.Pid != original.PID || command.identity.PID != original.PID || command.identity.StartTimeTicks != original.StartTimeTicks {
				continue
			}
			if command.identity.ProcessGroupID != original.PID || command.identity.ExecutableFile.Inode == 0 || command.identity.CommandLineHash == "" {
				return nil, fmt.Errorf("retained restart %s target %s has incomplete kernel ownership", fault.ID, id)
			}
			candidates = append(candidates, candidate{faultId: fault.ID, faultHash: faultHash, command: command})
		}
	}
	next := make(map[string]supervisorFaultRestartPending, len(candidates))
	var escalations []supervisorFaultRestartEscalation
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return escalations, err
		}
		id := candidate.command.spec.ID
		pending, ok := self.pendingProcessIdKVs[id]
		if !ok || pending.faultHash != candidate.faultHash || !sameSupervisedProcessIdentity(pending.identity, candidate.command.identity) {
			pending = supervisorFaultRestartPending{faultHash: candidate.faultHash, identity: candidate.command.identity, firstSeen: now}
		}
		if !pending.signaled && now.Sub(pending.firstSeen) >= supervisorFaultRestartGrace && signal(candidate.command, syscall.SIGKILL) {
			pending.signaled = true
			escalations = append(escalations, supervisorFaultRestartEscalation{faultId: candidate.faultId, processId: id, identity: candidate.command.identity})
		}
		next[id] = pending
	}
	self.pendingProcessIdKVs = next
	return escalations, nil
}
