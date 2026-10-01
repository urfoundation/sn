// Active repair adds stop propagation checks and fresh independent evidence to
// the fixed stopped-repair host profile. No command invokes a shell or restart.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Reverse dependencies can propagate an explicit stop beyond the approved unit.
var repairActiveValidatorProperties = []string{"Names", "RequiredBy", "RequisiteOf", "BoundBy", "ConsistsOf", "PropagatesStopTo", "StopPropagatedFrom", "UpheldBy", "ConflictedBy", "SendSIGKILL", "SendSIGHUP", "KillSignal", "FinalKillSignal", "TimeoutStopUSec", "TimeoutStartUSec", "TimeoutStopFailureMode", "NotifyAccess", "RefuseManualStart", "RefuseManualStop", "CanStart", "CanStop", "StopWhenUnneeded"}

// A fresh manager view must admit both the finite kill profile and no other
// affected units. Unknown/missing properties fail closed on unqualified hosts.
func (self *repairValidatorHost) inspectActive(ctx context.Context, plan repairActiveValidatorPlan) (repairValidatorManager, error) {
	p := plan.Process
	manager, err := self.inspect(ctx, p)
	if err != nil {
		return manager, err
	}
	raw, err := self.command(ctx, p, "--system", "--no-pager", "show", "--all", "--property="+strings.Join(repairActiveValidatorProperties, ","), "--", p.Unit.Name)
	if err != nil || len(raw) > 32*1024 {
		return manager, errors.Join(errors.New("active repair stop profile unavailable"), err)
	}
	expected := map[string]string{"Names": p.Unit.Name, "SendSIGKILL": "yes", "SendSIGHUP": "no", "KillSignal": "15", "FinalKillSignal": "9", "TimeoutStopUSec": "30s", "TimeoutStartUSec": "30s", "TimeoutStopFailureMode": "terminate", "NotifyAccess": "none", "RefuseManualStart": "no", "RefuseManualStop": "no", "CanStart": "yes", "CanStop": "yes", "StopWhenUnneeded": "no"}
	lines := make([]string, 0, len(repairActiveValidatorProperties))
	for _, key := range repairActiveValidatorProperties {
		lines = append(lines, key+"="+expected[key])
	}
	actual := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	slices.Sort(lines)
	slices.Sort(actual)
	if !slices.Equal(lines, actual) {
		return manager, errors.New("active repair loaded stop profile or dependency propagation differs")
	}
	// Removing the last requiring service must not stop an otherwise idle
	// prerequisite, even without any explicit reverse propagation edge.
	for _, dependency := range append(slices.Clone(p.RequiredMounts), "system.slice") {
		raw, err := self.command(ctx, p, "--system", "--no-pager", "show", "--all", "--property=Id,StopWhenUnneeded", "--", dependency)
		lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
		slices.Sort(lines)
		if err != nil || !slices.Equal(lines, []string{"Id=" + dependency, "StopWhenUnneeded=no"}) {
			return manager, errors.New("active repair prerequisite may stop when unused or is unavailable")
		}
	}
	return manager, nil
}

// The exact previous generation must still be running; pid reuse is insufficient.
func repairActiveValidatorRunning(plan repairValidatorPlan, manager repairValidatorManager) bool {
	return manager.Active == "active" && manager.SubState == "running" && manager.MainPid == plan.Previous.Pid && manager.Generation == plan.Previous && manager.Cgroup == "/system.slice/"+plan.Unit.Name
}

// The policy hash pins the entire role census. Only the named role supplies
// this incident's margins; another role cannot substitute its source or policy.
func (self *repairValidatorHost) activeIncident(ctx context.Context, plan repairActiveValidatorPlan, now time.Time, fresh bool) error {
	p := plan.Process
	raw, err := self.read(ctx, plan.MonitorServices.Path, p.MonitorUid, maxMonitorServicesBytes, false)
	var policy monitorServicesPolicy
	if err != nil || monitorReadDigest(raw) != plan.MonitorServices.Sha256 || decodePlanJson(raw, &policy) != nil || policy.Schema != monitorServicesSchema || len(policy.Validators) > maxMonitorValidatorRoles {
		return errors.New("active repair monitor policy changed or is unavailable")
	}
	found := 0
	for _, role := range policy.Validators {
		if role.Role != p.Role {
			continue
		}
		found++
		if role.ProgressFile != p.Unit.ProgressFile || role.ExpectedSource != p.Source || role.SteeringLiveness == nil || *role.SteeringLiveness != *plan.policy().SteeringLiveness {
			return errors.New("active repair role or liveness policy differs")
		}
	}
	if found != 1 {
		return errors.New("active repair policy role is not unique")
	}
	raw, err = self.read(ctx, p.MonitorCheckpoint, p.MonitorUid, maxMonitorServiceCheckpointBytes, true)
	var checkpoint monitorServiceCheckpointRecord
	if err != nil || decodePlanJson(raw, &checkpoint) != nil {
		return errors.Join(errors.New("active repair monitor checkpoint unavailable"), err)
	}
	if err := plan.incident(checkpoint); err != nil {
		return err
	}
	state := checkpoint.State
	if state.SampleAt.Before(p.Original.State.SampleAt) || state.SampleAt.After(now) || now.Sub(state.SampleAt) > time.Duration(p.MaximumSampleAgeSeconds)*time.Second || monitorProgressMaximumTime(state.Record).After(now) {
		return errors.New("active repair checkpoint is stale or from the future")
	}
	if fresh {
		state.readCurrent = true
		if state.ReadStatus != "ok" || state.steeringLiveness(plan.policy()).Status != "stalled" {
			return errors.New("active repair current steering liveness is unknown or recovered")
		}
	}
	raw, err = self.read(ctx, p.Unit.ProgressFile, p.Unit.Uid, protocol.MaxValidatorProgressBytes, false)
	if !fresh && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	value, err := protocol.DecodeValidatorProgress(raw)
	if err != nil {
		return err
	}
	return plan.stalledProgress(value, now, fresh)
}

// This single bounded command can stop only its already admitted generation.
// Trusted host custody excludes privileged changes after the final observation.
func (self *repairValidatorHost) stopActive(ctx context.Context, plan repairActiveValidatorPlan) error {
	_, err := self.command(ctx, plan.Process, "--system", "--no-pager", "--no-ask-password", "--job-mode=fail", "stop", "--", plan.Process.Unit.Name)
	return err
}

// A fixed generation claim outlives controller processes and alternate journals.
// Its name deliberately excludes incident ID and approval/state path.
func (self repairActiveValidatorPlan) claimPath() string {
	p := self.Process
	return p.Unit.File.Path + ".sn-active-" + strings.ReplaceAll(p.BootId, "-", "") + "-" + p.Previous.InvocationId + ".claim"
}

// A stopped-repair envelope cannot take over a generation already claimed by
// active repair. Callers hold the shared unit owner while testing both names.
func (self *repairValidatorHost) refuseActiveClaim(plan repairValidatorPlan) error {
	path := (repairActiveValidatorPlan{Process: plan}).claimPath()
	if err := self.parents(path, self.rootUid); err != nil {
		return err
	}
	for _, name := range []string{path, path + ".lock"} {
		if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errors.New("validator generation has active-repair custody or unavailable evidence"), err)
		}
	}
	return nil
}

// Exclusive creation consumes generation custody, even when later journal work
// fails. Reopen never repairs missing claims; no code deletes lifetime evidence.
func (self *repairValidatorHost) activeClaim(ctx context.Context, approval repairActiveValidatorApproval, key string, create bool) error {
	path := approval.Plan.claimPath()
	marker := []byte(repairActiveValidatorSchema + " " + rootObjectHash(approval) + " " + key + "\n")
	if err := self.parents(path, self.rootUid); err != nil {
		return err
	}
	if create {
		// Independent permanent names make either single missing artifact a
		// refusal on both resume and a differently signed fresh claim.
		for _, name := range []string{path + ".lock", path} {
			fd, err := syscall.Open(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
			if err != nil {
				return errors.Join(errors.New("active repair generation was already claimed or is unavailable"), err)
			}
			file := os.NewFile(uintptr(fd), name)
			_, writeErr := file.Write(marker)
			if err := errors.Join(writeErr, file.Sync(), file.Close(), ctx.Err()); err != nil {
				return err
			}
		}
		directory, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			return err
		}
	}
	for _, name := range []string{path, path + ".lock"} {
		raw, err := self.read(ctx, name, self.rootUid, 512, true)
		if err != nil || string(raw) != string(marker) {
			return errors.Join(fmt.Errorf("active repair generation claim differs: %s", approval.Plan.Process.IncidentId), err)
		}
	}
	return nil
}

// A fresh heartbeat is insufficient: the new acknowledged process must have
// returned an actual steering outcome. Read waits prove liveness, not success.
func (self *repairValidatorHost) activeProgress(ctx context.Context, plan repairActiveValidatorPlan, startedAt, now time.Time) (*repairValidatorPostcondition, error) {
	p := plan.Process
	raw, err := self.read(ctx, p.Unit.ProgressFile, p.Unit.Uid, protocol.MaxValidatorProgressBytes, false)
	if err != nil {
		return nil, err
	}
	value, err := protocol.DecodeValidatorProgress(raw)
	if err != nil || value.Source != p.Source || value.InstanceId == p.Original.State.Record.InstanceId || monitorProgressTime(value.StartedAt).Before(startedAt) || monitorProgressMaximumTime(value).After(now) || value.Publisher.Outcome != "published" || monitorProgressTime(value.Publisher.LastSuccessAt).Before(startedAt) || !monitorServiceFresh(now, monitorProgressTime(value.HeartbeatAt)) || !monitorServiceFresh(now, monitorProgressTime(value.Publisher.LastSuccessAt)) || value.Steering == nil || value.Steering.Outcome == "starting" || monitorProgressTime(value.Steering.ObservedAt).Before(monitorProgressTime(value.StartedAt)) || !monitorServiceFresh(now, monitorProgressTime(value.Steering.ObservedAt)) {
		return nil, errors.New("active repair new generation has not published responsive steering")
	}
	canonical, err := value.Encode()
	if err != nil {
		return nil, err
	}
	return &repairValidatorPostcondition{ObservedAt: now, InstanceId: value.InstanceId, RecordHash: monitorReadDigest(canonical)}, nil
}
