// The optional root-owned controller is a consumer of a fixed signed-envelope
// roster. Preparation, independent approval and installation remain explicit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

// The production adapter has no injectable executable, approver or signer flag.
func runRepairControllerCommand(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runRepairControllerCommandWithHost(ctx, args, stdout, stderr, now, newRepairValidatorHost())
}

// A supervisor may retry unavailable shared observations without refunding any
// retained allowance. Confirmed identity and ambiguous publication stay held.
func repairControllerCommandExit(err error) int {
	if errors.Is(err, errRepairControllerHeartbeatHeld) {
		return 3
	}
	status, cause := repairControllerCause(err)
	if status == "held" {
		return 3
	}
	if cause == "cancelled" && errors.Is(err, context.Canceled) {
		return 0
	}
	return 1
}

// Open one prepared original checkpoint; a missing retained journal or marker
// cannot create another controller census or refresh an envelope allowance.
func openRepairControllerStore(ctx context.Context, path string, manifest repairControllerManifest, hash string, now time.Time) (*repairValidatorFileOwner, repairControllerRecord, error) {
	var record repairControllerRecord
	_, statErr := os.Lstat(path)
	create := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !create {
		return nil, record, statErr
	}
	owner, err := openRepairValidatorFileOwner(ctx, path, repairControllerSchema+" "+hash+"\n", create)
	if err != nil {
		return nil, record, err
	}
	if create {
		record = repairControllerRecord{Schema: repairControllerSchema, ManifestHash: hash, HighWaterAt: now}
		for _, entry := range manifest.Entries {
			record.Entries = append(record.Entries, repairControllerEntryState{Id: entry.Id, ApprovalHash: entry.Approval.Sha256, Status: "pending", ObservedAt: now})
		}
		err = saveRepairControllerStore(owner, record, manifest, hash)
	} else {
		var raw []byte
		raw, _, err = owner.storage.readFile(ctx, path, 64*1024)
		if err == nil {
			err = decodePlanJson(raw, &record)
		}
		if err == nil {
			err = record.validate(manifest, hash)
		}
		if err == nil {
			owner.expectedHash = monitorReadDigest(raw)
		}
	}
	if err != nil {
		return nil, repairControllerRecord{}, errors.Join(err, owner.close())
	}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	return owner, record, nil
}

// A failed publication poisons this owner. Durable bytes retain any uncertainty
// and require an explicit reopen rather than another in-memory attempt.
func saveRepairControllerStore(owner *repairValidatorFileOwner, record repairControllerRecord, manifest repairControllerManifest, hash string) error {
	if err := owner.validateOwner(); err != nil {
		return err
	}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(manifest, hash); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > 64*1024 {
		return errors.Join(errors.New("repair controller checkpoint exceeds its finite frame"), err)
	}
	raw = append(raw, '\n')
	if err := owner.storage.publish(owner.path, raw, owner.syncDirectory); err != nil {
		if !mainnetDurableAdmissionPending(err) {
			owner.poisoned = errors.Join(errMainnetDurablePublicationUncertain, err)
		}
		return err
	}
	owner.expectedHash = monitorReadDigest(raw)
	return nil
}

// Mutated approvals are per-entry holds. Manifest or shared checkpoint loss
// stops the owner because no envelope may borrow uncertain controller custody.
func runRepairControllerCommandWithHost(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, host *repairValidatorHost) int {
	if ctx == nil || host == nil || now == nil || runtime.GOOS != "linux" || uint32(os.Geteuid()) != host.rootUid {
		fmt.Fprintln(stderr, "repair controller requires Linux root custody")
		return 2
	}
	ctx, cancelOwner := context.WithCancel(ctx)
	defer cancelOwner()
	flags := flag.NewFlagSet("repair-controller", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "immutable independently reviewed envelope roster")
	manifestHash := flags.String("manifest-sha256", "", "sha256 of the exact original manifest")
	checkpoint := flags.String("checkpoint", "", "prepared private controller census")
	metricsPath := flags.String("metrics-file", "", "separately prepared .prom output")
	follow := flags.Bool("follow", false, "repeat bounded original-envelope observations")
	interval := flags.Duration("interval", time.Minute, "bounded cycle interval")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !repairValidatorPath(*manifestPath) || !validMonitorReadDigest(*manifestHash) || !repairValidatorPath(*checkpoint) || !repairValidatorPath(*metricsPath) || *interval < time.Second || *interval > time.Hour || *checkpoint == *metricsPath || *checkpoint == *manifestPath || *metricsPath == *manifestPath {
		fmt.Fprintln(stderr, "repair controller requires exact manifest/hash, distinct prepared checkpoint/metrics and interval 1s..1h")
		return 2
	}
	manifestReference := planFileReference{Path: *manifestPath, Sha256: *manifestHash}
	if err := errors.Join(host.pin(ctx, manifestReference, 64*1024, false), host.parents(*checkpoint, host.rootUid), host.parents(*metricsPath, host.rootUid)); err != nil {
		fmt.Fprintln(stderr, err)
		return repairControllerCommandExit(err)
	}
	raw, hash, err := readPlanFile(ctx, *manifestPath, 64*1024)
	var manifest repairControllerManifest
	if err == nil {
		err = decodePlanJson(raw, &manifest)
	}
	if err == nil {
		err = manifest.validate()
	}
	if err != nil || hash != *manifestHash {
		fmt.Fprintln(stderr, "repair controller original manifest refused:", err)
		return 2
	}
	unitLocks := make([]*sync.Mutex, len(manifest.Entries))
	units := map[string]*sync.Mutex{}
	inputs := map[string]bool{*manifestPath: true}
	writes := map[string]bool{}
	for _, path := range []string{*checkpoint, *checkpoint + ".lock", *metricsPath, *metricsPath + ".lock"} {
		for existing := range writes {
			if rootPassiveHostPathContains(path, existing) || rootPassiveHostPathContains(existing, path) {
				fmt.Fprintln(stderr, "repair controller output paths overlap")
				return 2
			}
		}
		writes[path] = true
	}
	for index, entry := range manifest.Entries {
		inputs[entry.Approval.Path] = true
		envelope, err := loadRepairControllerEnvelope(ctx, entry, host)
		var closure *repairRootClosureError
		if errors.As(err, &closure) {
			// An authenticated but incomplete graph cannot prove any shared
			// destination disjoint. No census or metrics owner may open yet.
			fmt.Fprintln(stderr, err)
			return repairControllerCommandExit(err)
		}
		if envelope.plan.Unit.Name == "" {
			unitLocks[index] = &sync.Mutex{}
			continue
		}
		p := envelope.plan
		for _, path := range []string{p.StatePath, p.StatePath + ".lock"} {
			for existing := range writes {
				if rootPassiveHostPathContains(path, existing) || rootPassiveHostPathContains(existing, path) {
					fmt.Fprintln(stderr, "repair controller journal paths overlap")
					return 2
				}
			}
			writes[path] = true
		}
		for _, path := range envelope.protectedPaths() {
			if path != "" {
				inputs[path] = true
			}
		}
		if units[p.Unit.Name] == nil {
			units[p.Unit.Name] = &sync.Mutex{}
		}
		unitLocks[index] = units[p.Unit.Name]
	}
	for path := range writes {
		for input := range inputs {
			for _, protected := range []string{input, input + ".lock"} {
				if rootPassiveHostPathContains(path, protected) || rootPassiveHostPathContains(protected, path) {
					fmt.Fprintln(stderr, "repair controller write overlaps an original input")
					return 2
				}
			}
		}
	}
	store, record, err := openRepairControllerStore(ctx, *checkpoint, manifest, hash, now())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return repairControllerCommandExit(err)
	}
	metrics, err := openMonitorMetrics(*metricsPath, ctx)
	if err != nil {
		err = errors.Join(err, store.close())
		fmt.Fprintln(stderr, err)
		return repairControllerCommandExit(err)
	}
	heartbeat := &repairControllerHeartbeat{store: store, metrics: metrics, manifest: manifest, hash: hash, now: now, checkManifest: func(observation context.Context) error {
		return host.pin(observation, manifestReference, 64*1024, false)
	}, cancelOwner: cancelOwner, stderr: stderr}
	heartbeat.retain(record)
	heartbeat.start(ctx)
	controller := repairController{manifest: manifest, record: record, unitLocks: unitLocks, now: now, step: repairControllerHostStep(host, now), save: func(record repairControllerRecord) error { return heartbeat.persist(ctx, record) }}
	for {
		err = heartbeat.check(ctx)
		if err == nil {
			err = controller.cycle(ctx)
		}
		if err == nil {
			err = heartbeat.publish(ctx)
		}
		if err == nil {
			err = json.NewEncoder(stdout).Encode(controller.record)
		}
		if err != nil || !*follow {
			break
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(*interval):
		}
		if err != nil {
			break
		}
	}
	// The cycle has joined every action. Cancel both the timer's child context
	// and the original context retained by file owners before joining the timer.
	cancelOwner()
	err = errors.Join(err, heartbeat.close(), metrics.close(), store.close())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return repairControllerCommandExit(err)
	}
	return 0
}
