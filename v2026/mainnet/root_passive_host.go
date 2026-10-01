// Only this owner may install the fixed passive unit and consume its one
// acknowledged start. It never adopts an unacknowledged process after a crash.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type rootPassiveHost struct {
	files *validatorActivationHost
	// Production is always uid/gid zero. Private fixtures use their real owner.
	gid uint32
}

func newRootPassiveHost() *rootPassiveHost {
	return &rootPassiveHost{files: newValidatorActivationHost(), gid: 0}
}

func (self *rootPassiveHost) profile(plan rootPassiveHostPlan) repairValidatorPlan {
	return repairValidatorPlan{MachineId: plan.MachineId, BootId: plan.BootId, Systemctl: plan.Systemctl, RequiredMounts: plan.RequiredMounts, CommandTimeoutSeconds: plan.CommandTimeoutSeconds,
		Unit: repairValidatorUnit{Name: rootPassiveHostUnitName, File: plan.Unit, Binary: plan.Binary, Config: plan.Runtime, StateDirectory: plan.CheckpointDirectory, Uid: self.files.host.rootUid, Gid: self.gid}}
}

func (self *rootPassiveHost) inspect(ctx context.Context, plan rootPassiveHostPlan) (repairValidatorManager, error) {
	if filepath.Dir(plan.Unit.Path) != self.files.unitDirectory {
		return repairValidatorManager{}, errors.New("passive unit is outside the fixed system directory")
	}
	return self.files.host.inspectCommand(ctx, self.profile(plan), plan.arguments(), plan.sandbox())
}

// Existing inputs remain private and root-owned. The sandbox's sole writable
// directory has no symlink, foreign owner or retained authority below it.
func (self *rootPassiveHost) authority(ctx context.Context, plan rootPassiveHostPlan, preparation bootstrapChainPreparation) error {
	h := self.files.host
	machine, err := h.read(ctx, h.machinePath, h.rootUid, 128, false)
	if err != nil || strings.TrimSpace(string(machine)) != plan.MachineId {
		return errors.Join(errors.New("passive host machine identity differs"), err)
	}
	boot, err := os.Open(h.bootPath)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(boot, 129))
	if err := errors.Join(readErr, boot.Close(), ctx.Err()); err != nil || len(raw) > 128 || strings.TrimSpace(string(raw)) != plan.BootId {
		return errors.New("passive host boot identity differs")
	}
	for _, ref := range []planFileReference{plan.Binary, plan.Systemctl, plan.Preparation, plan.Runtime, preparation.Plan.Config.Root, preparation.Root.ServiceInput, preparation.Plan.Config.RootValidator.Approval} {
		if err := h.pin(ctx, ref, 512*1024*1024, ref == plan.Binary || ref == plan.Systemctl); err != nil {
			return err
		}
	}
	private := func(path string) error {
		if err := h.parents(path, h.rootUid); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != h.rootUid || stat.Nlink != 1 || info.Mode().Perm()&0077 != 0 {
			return errors.New("passive authority is not exclusive private host custody")
		}
		return nil
	}
	for _, path := range []string{plan.Preparation.Path, plan.Runtime.Path, preparation.Root.ConfigPath, preparation.Root.ServiceInput.Path, preparation.Plan.Config.RootValidator.Approval.Path} {
		if err := private(path); err != nil {
			return err
		}
	}
	for _, path := range preparation.childPaths() {
		for _, candidate := range []string{path, path + ".lock"} {
			if err := private(candidate); err != nil {
				return err
			}
		}
	}
	if err := h.parents(plan.StatePath, h.rootUid); err != nil {
		return err
	}
	if err := h.parents(filepath.Join(plan.CheckpointDirectory, "owned"), h.rootUid); err != nil {
		return err
	}
	info, err := os.Lstat(plan.CheckpointDirectory)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != h.rootUid || stat.Gid != self.gid {
		return errors.New("passive checkpoint directory is not private host custody")
	}
	// Writable directory contents cannot alias protected files through a hard
	// link or symlink. Publication may only replace the two fixed monitor names.
	fd, err := syscall.Open(plan.CheckpointDirectory, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), plan.CheckpointDirectory)
	entries, readErr := directory.ReadDir(3)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, directory.Close(), ctx.Err()); err != nil {
		return err
	}
	if len(entries) > 2 {
		return errors.New("passive checkpoint directory exceeds its fixed two-file census")
	}
	checkpoint := filepath.Base(preparation.Root.PassiveService.CheckpointPath)
	for _, entry := range entries {
		if entry.Name() != checkpoint && entry.Name() != checkpoint+".lock" {
			return errors.New("passive checkpoint directory contains unrelated state")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || stat.Uid != h.rootUid || info.Mode().Perm()&0077 != 0 {
			return errors.New("passive checkpoint entry is not exclusively owned private state")
		}
	}
	return ctx.Err()
}

func rootPassiveHostWindow(ctx context.Context, plan rootPassiveHostPlan, highWater, now time.Time, observation *rootPassiveHostObservation) error {
	if ctx == nil || ctx.Err() != nil || now.IsZero() || now.Before(highWater) || now.Before(plan.ValidFrom) || !now.Before(plan.ExpiresAt) {
		return errors.New("passive host clock or approval window closed")
	}
	if observation != nil && (observation.ObservedAt.After(now) || now.Sub(observation.ObservedAt) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second) {
		return errors.New("passive host observation is stale")
	}
	return nil
}

type rootPassiveHostResult struct {
	Schema                string                      `json:"schema"`
	Status                string                      `json:"status"`
	RecordHash            string                      `json:"record_hash"`
	Installed             bool                        `json:"installed"`
	StartConsumed         bool                        `json:"start_consumed"`
	Generation            *repairValidatorGeneration  `json:"acknowledged_generation,omitempty"`
	Observation           *rootPassiveHostObservation `json:"last_read_only_observation,omitempty"`
	CurrentProcessRunning bool                        `json:"current_process_running"`
	NativeSigning         bool                        `json:"native_signing"`
	NetworkSubmission     bool                        `json:"network_submission"`
	RootServiceReady      bool                        `json:"root_service_ready"`
	ActivationReady       bool                        `json:"activation_ready"`
	Disposition           string                      `json:"operator_disposition"`
}

func (self rootPassiveHostRecord) result() rootPassiveHostResult {
	disposition := "Retain original authority and journal. Read-only chain observations and process identity do not prove continuing service health or authorize native actions."
	if !self.StartAt.IsZero() && self.Generation == nil {
		disposition = "Start consumed without durable acknowledgment. Reconcile manually; never resend, replace the journal, or create another allowance."
	}
	return rootPassiveHostResult{Schema: rootPassiveHostSchema, Status: self.Status, RecordHash: self.ContentHash, Installed: self.Installed, StartConsumed: !self.StartAt.IsZero(), Generation: self.Generation, Observation: self.Observation, Disposition: disposition}
}

func advanceRootPassiveHost(ctx context.Context, store *rootPassiveHostStore, host *rootPassiveHost, preparation bootstrapChainPreparation, operation string, now func() time.Time) (result rootPassiveHostResult, resultErr error) {
	record, err := store.load(ctx)
	if err != nil {
		return result, err
	}
	p, h := store.approval.Plan, host.files.host
	finish := func(status string, cause error) (rootPassiveHostResult, error) {
		record.Status = status
		err := store.save(record)
		if err != nil {
			return record.result(), errors.Join(cause, err)
		}
		saved, err := store.load(ctx)
		return saved.result(), errors.Join(cause, err)
	}
	if err := rootPassiveHostWindow(ctx, p, record.HighWaterAt, now(), nil); err != nil {
		return finish("approval-window-closed", err)
	}
	if record.Operations >= p.MaximumOperations {
		return finish("operation-limit", errors.New("passive host operation allowance exhausted"))
	}
	record.Operations++
	record.HighWaterAt = now()
	record.Status = "operation-reserved"
	if err := store.save(record); err != nil {
		return record.result(), err
	}
	control, err := h.control(ctx, host.profile(p).Unit)
	if err != nil {
		return record.result(), err
	}
	defer func() { resultErr = errors.Join(resultErr, control.close()) }()
	if err := host.authority(ctx, p, preparation); err != nil {
		return finish("source-refused", err)
	}
	if !record.StartAt.IsZero() && record.Generation == nil {
		return finish("uncertain-consumed-start", errors.New("passive host cannot adopt or retry an uncertain invocation"))
	}
	if operation == "install" {
		if !record.StartAt.IsZero() {
			return finish("source-refused", errors.New("passive unit installation cannot follow a consumed start"))
		}
		if filepath.Dir(p.Unit.Path) != host.files.unitDirectory {
			return finish("source-refused", errors.New("passive unit path differs from the system directory"))
		}
		record.InstallIntent = true
		record.Status = "installing"
		if err := store.save(record); err != nil {
			return record.result(), err
		}
		if err := rootPassiveHostWindow(ctx, p, record.HighWaterAt, now(), nil); err != nil {
			return finish("approval-window-closed", err)
		}
		if err := control.validate(); err != nil {
			return finish("source-refused", err)
		}
		if err := host.files.installFile(ctx, p.Unit, p.render(), 0644, host.gid); err != nil {
			return finish("source-refused", err)
		}
		if err := errors.Join(store.validateOwner(), control.validate(), rootPassiveHostWindow(ctx, p, record.HighWaterAt, now(), nil)); err != nil {
			return finish("source-refused", err)
		}
		if _, err := h.command(ctx, host.profile(p), "--system", "--no-pager", "--no-ask-password", "daemon-reload"); err != nil {
			return finish("source-refused", err)
		}
		manager, err := host.inspect(ctx, p)
		if err == nil {
			err = h.stopped(ctx, host.profile(p), manager)
		}
		if err != nil {
			return finish("source-refused", err)
		}
		record.Installed = true
		return finish("installed", nil)
	}
	if !record.Installed {
		return finish("source-refused", errors.New("passive host requires exact completed installation"))
	}
	manager, err := host.inspect(ctx, p)
	if err != nil {
		return finish("source-refused", err)
	}
	if record.Generation != nil {
		if operation == "start" {
			return finish("source-refused", errors.New("passive host initial start is already consumed"))
		}
		running := repairValidatorRunning(host.profile(p), manager, *record.Generation)
		if !running {
			profile := host.profile(p)
			profile.Previous = *record.Generation
			err = h.stopped(ctx, profile, manager)
			if err != nil {
				return finish("generation-changed", err)
			}
		}
		if operation == "resume" {
			status := "acknowledged-stopped"
			if running {
				status = "acknowledged-running"
			}
			result, err := finish(status, nil)
			result.CurrentProcessRunning = running && err == nil
			return result, err
		}
	} else {
		if err := h.stopped(ctx, host.profile(p), manager); err != nil {
			return finish("generation-changed", err)
		}
		if operation == "resume" {
			return finish("source-refused", errors.New("passive resume has no acknowledged start"))
		}
	}
	service := preparation.Root.PassiveService
	client, err := newRpcClient(service.RpcUrl, time.Duration(service.ReadRetrySeconds)*time.Second)
	if err != nil {
		return finish("source-refused", err)
	}
	defer client.httpClient.CloseIdleConnections()
	preview, err := client.readRootPreview(ctx, copyRootPassivePolicy(service.Policy), record.PolicyHash)
	if err != nil || !preview.ReadOnlyReady {
		return finish("source-refused", errors.Join(errors.New("passive root current policy observation is blocked"), err))
	}
	sealed, err := sealRootPreview(preview)
	if err != nil {
		return finish("source-refused", err)
	}
	record.Observation = &rootPassiveHostObservation{ObservedAt: now(), Root: *projectRootMonitorObservation(&sealed)}
	if err := rootPassiveHostWindow(ctx, p, record.HighWaterAt, now(), record.Observation); err != nil {
		return finish("approval-window-closed", err)
	}
	record.HighWaterAt = now()
	if operation == "admit" {
		return finish("admitted-read-only", nil)
	}
	// Fresh host admission follows the RPC, immediately before reserving start.
	manager, err = host.inspect(ctx, p)
	if err == nil {
		err = h.stopped(ctx, host.profile(p), manager)
	}
	if err != nil {
		return finish("generation-changed", err)
	}
	monotonic, err := h.monotonic()
	if err != nil || monotonic == 0 {
		return finish("source-refused", errors.Join(errors.New("passive host monotonic clock unavailable"), err))
	}
	record.StartAt = now()
	record.HighWaterAt = record.StartAt
	record.StartMonotonicUsec = monotonic
	record.StartObservation = record.Observation
	record.Status = "start-consumed"
	if err := store.save(record); err != nil {
		return record.result(), err
	}
	// Publication can outlive approval. No transport is reached until both
	// lifetime owners, authority bytes, clock and manager have been rechecked.
	if err := errors.Join(store.validateOwner(), control.validate(), rootPassiveHostWindow(ctx, p, record.HighWaterAt, now(), record.StartObservation), host.authority(ctx, p, preparation)); err != nil {
		return finish("uncertain-consumed-start", err)
	}
	manager, err = host.inspect(ctx, p)
	if err == nil {
		err = h.stopped(ctx, host.profile(p), manager)
	}
	if err != nil {
		return finish("uncertain-consumed-start", err)
	}
	if err := rootPassiveHostWindow(ctx, p, record.HighWaterAt, now(), record.StartObservation); err != nil {
		return finish("uncertain-consumed-start", err)
	}
	if err := h.start(ctx, host.profile(p)); err != nil {
		return finish("uncertain-consumed-start", err)
	}
	manager, err = host.inspect(ctx, p)
	if err != nil || !repairValidatorRunning(host.profile(p), manager, manager.Generation) || manager.Generation.StartedUsec < monotonic {
		return finish("uncertain-consumed-start", errors.Join(errors.New("passive start has no exact acknowledged running generation"), err))
	}
	record.Generation = &manager.Generation
	result, err = finish("acknowledged-running", nil)
	result.CurrentProcessRunning = err == nil
	return result, err
}

// Keep authority outside the writable subtree, including path ancestors. The
// caller separately requires physical protected parents before any effect.
func rootPassiveHostPathContains(directory, path string) bool {
	return path == directory || strings.HasPrefix(path, directory+"/")
}
