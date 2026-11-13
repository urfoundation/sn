// Recovery is a separately approved finite owner lifecycle. Its allowance is
// retained inside the existing service journal, never in a recreatable sidecar.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const rootServiceRecoverySchema = "urnetwork-mainnet-root-service-recovery-v1"
const rootServiceRecoveryDomain = "urnetwork-mainnet-root-service-recovery-approval-v1"

// The service approver separately signs the exact runtime, original journal,
// physical root declaration, time window and lifetime restart allowance.
type rootServiceRecoveryApproval struct {
	Schema                  string                  `json:"schema"`
	RuntimeSha256           string                  `json:"runtime_sha256"`
	ServiceConfigHash       string                  `json:"service_config_hash"`
	InitialServiceStateHash string                  `json:"initial_service_state_hash"`
	DurableVolumes          durablevolume.Reference `json:"durable_volumes"`
	MaximumOpens            uint32                  `json:"maximum_opens"`
	MaximumStepsPerOpen     uint32                  `json:"maximum_steps_per_open"`
	IntervalSeconds         uint32                  `json:"interval_seconds"`
	ValidFrom               time.Time               `json:"valid_from"`
	ExpiresAt               time.Time               `json:"expires_at"`
	Signature               string                  `json:"signature_ed25519"`
}

// The independent signature uses a domain distinct from activation and action.
func (self rootServiceRecoveryApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(rootServiceRecoveryDomain+"\x00"), raw...), err
}

// A recovery approval creates no current eligibility, native key, or route.
func (self rootServiceRecoveryApproval) validate(ctx context.Context, preparation rootServiceRuntimePreparation, now time.Time) error {
	if ctx == nil || !rootCanonicalHash(preparation.Config.Role.ApprovalPublicKey) {
		return errors.New("root recovery context or independent service approver is absent")
	}
	reference, present := durablevolume.ReferenceFromContext(ctx)
	if self.Schema != rootServiceRecoverySchema || self.RuntimeSha256 != preparation.Input.Sha256 ||
		self.ServiceConfigHash != rootObjectHash(preparation.Root.Service) || !planSha256(self.InitialServiceStateHash) ||
		!present || reference != self.DurableVolumes || !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) ||
		self.MaximumOpens == 0 || self.MaximumOpens > 4 || self.MaximumStepsPerOpen == 0 || self.MaximumStepsPerOpen > 10000 ||
		self.IntervalSeconds < 60 || self.IntervalSeconds > 3600 || self.ValidFrom.IsZero() || !self.ExpiresAt.After(self.ValidFrom) ||
		self.ExpiresAt.Sub(self.ValidFrom) > 24*time.Hour || now.Before(self.ValidFrom) || !now.Before(self.ExpiresAt) {
		return errors.New("root recovery approval differs from exact runtime, physical generation, lifetime bounds or validity")
	}
	key, keyErr := hex.DecodeString(preparation.Config.Role.ApprovalPublicKey[2:])
	signature, signatureErr := hex.DecodeString(self.Signature)
	message, err := self.signingBytes()
	if keyErr != nil || signatureErr != nil || err != nil || len(key) != ed25519.PublicKeySize ||
		len(signature) != ed25519.SignatureSize || hex.EncodeToString(signature) != self.Signature || !ed25519.Verify(key, message, signature) {
		return errors.New("root recovery independent service approval signature differs")
	}
	return ctx.Err()
}

// Retained counters are validated with the original record, then against the
// separate signed approval before a controller consumes another attempt.
type rootServiceRecoveryRecord struct {
	ApprovalHash  string    `json:"approval_hash"`
	MaximumOpens  uint32    `json:"maximum_opens"`
	OpenAttempts  uint32    `json:"open_attempts"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
}

// Omission preserves the complete old record encoding and content hash.
func (self rootServiceRecoveryRecord) validate() error {
	if !planSha256(self.ApprovalHash) || self.MaximumOpens == 0 || self.MaximumOpens > 4 ||
		self.OpenAttempts > self.MaximumOpens || (self.OpenAttempts == 0) != self.LastAttemptAt.IsZero() {
		return errors.New("root recovery retained allowance is malformed")
	}
	return nil
}

// Loading authenticates bounded bytes before opening any service ownership.
func loadRootServiceRecovery(ctx context.Context, reference planFileReference, preparation rootServiceRuntimePreparation, now time.Time) (rootServiceRecoveryApproval, error) {
	var approval rootServiceRecoveryApproval
	if !planSha256(reference.Sha256) {
		return approval, errors.New("root recovery approval requires an exact byte pin")
	}
	raw, hash, err := readBootstrapRootFile(ctx, reference.Path, 16*1024)
	if err != nil || hash != reference.Sha256 {
		return approval, errors.Join(errors.New("root recovery approval bytes differ"), err)
	}
	if err := decodePlanJson(raw, &approval); err != nil {
		return approval, err
	}
	return approval, approval.validate(ctx, preparation, now)
}

// Explicit preparation may add a first recovery policy to this original state.
// It cannot replace a policy, replenish spent attempts, or recreate any journal.
func prepareRootServiceRecovery(ctx context.Context, preparation rootServiceRuntimePreparation, approval rootServiceRecoveryApproval, now time.Time) (resultErr error) {
	if err := errors.Join(preparation.validate(), approval.validate(ctx, preparation, now)); err != nil {
		return err
	}
	store, err := openRootServiceStore(preparation.Root.Service, false, ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, store.close()) }()
	record, err := store.load()
	if err != nil {
		return err
	}
	if record.Recovery != nil {
		if record.Recovery.ApprovalHash != rootObjectHash(approval) || record.Recovery.MaximumOpens != approval.MaximumOpens {
			return errors.New("root recovery cannot replace its retained original approval")
		}
		return nil
	}
	if record.ContentHash != approval.InitialServiceStateHash || !record.SubmissionPrepared || record.SubmissionConfigHash != rootObjectHash(preparation.Submission) {
		return errors.New("root recovery requires its exact independently reviewed prepared service state")
	}
	record.Recovery = &rootServiceRecoveryRecord{ApprovalHash: rootObjectHash(approval), MaximumOpens: approval.MaximumOpens}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	return store.save(record)
}

// Each open reservation commits before acquiring runtime owners. An uncertain
// reservation stops here; no runtime, observation or dependent effect follows.
func reserveRootServiceRecovery(ctx context.Context, preparation rootServiceRuntimePreparation, approval rootServiceRecoveryApproval, now time.Time) (attempt uint32, wait time.Duration, resultErr error) {
	return reserveRootServiceRecoveryWithSync(ctx, preparation, approval, now, nil)
}

// The optional instance hook reaches the actual directory-sync boundary; no
// runtime flag can select it or substitute a successful persistence verdict.
func reserveRootServiceRecoveryWithSync(ctx context.Context, preparation rootServiceRuntimePreparation, approval rootServiceRecoveryApproval, now time.Time, syncDirectory func(*os.File) error) (attempt uint32, wait time.Duration, resultErr error) {
	if err := approval.validate(ctx, preparation, now); err != nil {
		return 0, 0, err
	}
	store, err := openRootServiceStore(preparation.Root.Service, false, ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.close()) }()
	store.syncDirectoryForTest = syncDirectory
	record, err := store.load()
	if err != nil {
		return 0, 0, err
	}
	recovery := record.Recovery
	if recovery == nil || recovery.ApprovalHash != rootObjectHash(approval) || recovery.MaximumOpens != approval.MaximumOpens {
		return 0, 0, errors.New("root recovery original prepared approval is absent or different")
	}
	if recovery.OpenAttempts == recovery.MaximumOpens {
		return 0, 0, errors.New("root recovery lifetime open allowance is exhausted")
	}
	if recovery.OpenAttempts != 0 {
		if now.Before(recovery.LastAttemptAt) {
			return 0, 0, errors.New("root recovery clock moved before its retained attempt")
		}
		next := recovery.LastAttemptAt.Add(time.Duration(approval.IntervalSeconds) * time.Second)
		if now.Before(next) {
			return recovery.OpenAttempts, next.Sub(now), nil
		}
	}
	recovery.OpenAttempts++
	recovery.LastAttemptAt = now.UTC()
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := store.save(record); err != nil {
		return 0, 0, err
	}
	return recovery.OpenAttempts, 0, nil
}

// Tests inject only per-controller lifecycle boundaries. Public commands use
// the concrete original-runtime opener and a cancellable real cadence.
type rootServiceRecoveryPorts struct {
	open    func(context.Context, rootServiceRuntimePreparation, bool) (*rootServiceRuntime, error)
	now     func() time.Time
	wait    func(context.Context, time.Duration) bool
	reserve func(context.Context, rootServiceRuntimePreparation, rootServiceRecoveryApproval, time.Time) (uint32, time.Duration, error)
}

// A finite budget bounds even operational open failures. Only this controller's
// uncertain/unavailable owner is reopened; integrity failures stop permanently.
func runRootServiceRecovery(ctx context.Context, preparation rootServiceRuntimePreparation, approval rootServiceRecoveryApproval, stdout io.Writer, ports rootServiceRecoveryPorts) (result rootServiceRuntimeResult, resultErr error) {
	result = rootServiceRuntimeStatus(preparation)
	result.RecoveryApprovalHash = rootObjectHash(approval)
	if ports.open == nil || ports.now == nil || ports.wait == nil {
		return result, errors.New("root recovery lifecycle ports are absent")
	}
	if err := preparation.validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithDeadline(ctx, approval.ExpiresAt)
	defer cancel()
	reserve := ports.reserve
	if reserve == nil {
		reserve = reserveRootServiceRecovery
	}
	for {
		attempt, delay, err := reserve(ctx, preparation, approval, ports.now())
		if err != nil {
			if mainnetDurableAdmissionPending(err) && ctx.Err() == nil {
				if ports.wait(ctx, time.Duration(approval.IntervalSeconds)*time.Second) {
					continue
				}
				err = errors.Join(err, ctx.Err())
			}
			return result, err
		}
		if delay != 0 {
			if !ports.wait(ctx, delay) {
				return result, errors.Join(errors.New("root recovery cadence was interrupted"), ctx.Err())
			}
			continue
		}
		result.RecoveryOpenAttempts = attempt
		attemptCtx, cancel := context.WithDeadline(ctx, approval.ExpiresAt)
		runtime, err := ports.open(attemptCtx, preparation, false)
		if err == nil {
			result, err = executeRootServiceRuntime(attemptCtx, runtime, result, approval.MaximumStepsPerOpen, time.Duration(approval.IntervalSeconds)*time.Second, stdout)
		}
		cancel()
		if err == nil || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, errRpcIntegrity) || ctx.Err() != nil ||
			errors.Is(err, errRootServiceRuntimeClose) ||
			!errors.Is(err, errMainnetDurablePublicationUncertain) && !mainnetDurableAdmissionPending(err) {
			return result, err
		}
		if attempt == approval.MaximumOpens {
			return result, errors.Join(errors.New("root recovery exhausted its lifetime allowance; retain original owner"), err)
		}
		// executeRootServiceRuntime has synchronously joined and closed every
		// child before another attempt is even reserved. No live port is added.
		if !ports.wait(ctx, time.Duration(approval.IntervalSeconds)*time.Second) {
			return result, errors.Join(err, ctx.Err())
		}
	}
}
