// Historical startup owns original liabilities before current preparation.
// Fresh eligibility and settlement publication remain prerequisites for new
// work; neither a read outage nor an old signature grants a replacement epoch.
package validator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Only the actual authenticated intent reader requests preparation. Only the
// current-eligibility/publication owner closes ready. No monitor owns either.
type releaseProductionPreparation struct {
	requested   chan struct{}
	ready       chan struct{}
	requestOnce sync.Once
	readyOnce   sync.Once
}

type productionPreparationPending struct {
	nativeEpoch uint64
	epochKnown  bool
}

func (self *productionPreparationPending) Error() string {
	return "production fresh work waits for current eligibility and initial settlement publication"
}

// A pending or finalized original intent returns through its reconciliation
// branch before this call. An empty/applied/failed real owner may request fresh
// preparation, but cannot sign or begin another intent before readiness.
func (self *ReleaseSteerer) requestProductionPreparation(intent *SteeringIntent) error {
	if self.runtimeV2 == nil || self.runtimeV2.preparation == nil {
		return errors.New("production current preparation has no startup owner")
	}
	if intent != nil && intent.Status != "applied" && intent.Status != "failed" {
		return errors.New("production preparation was requested before original liability resolved")
	}
	owner := self.runtimeV2.preparation
	if owner.requested == nil || owner.ready == nil {
		return errors.New("production current preparation channels are absent")
	}
	owner.requestOnce.Do(func() { close(owner.requested) })
	if err := self.runtimeV2.authenticationPending(intent); err != nil {
		return err
	}
	select {
	case <-owner.ready:
		return nil
	default:
		wait := &productionPreparationPending{}
		if intent != nil {
			wait.nativeEpoch, wait.epochKnown = intent.SubnetEpoch, true
		}
		return wait
	}
}

// Reuse the independently signed activation block across exact compatible
// configuration renewals. Every historical identity/canonical/purpose check
// still runs; the unrelated current tuple is not an initialization dependency.
func dialProductionNativeHistory(ctx context.Context, cfg *ReleaseConfig) (*crv4.Chain, error) {
	if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
		return nil, err
	}
	original := cfg
	if history := cfg.productionAuthorityHistory; history != nil && len(history.entries) != 0 {
		original = history.entries[0].config
	}
	approval, err := ownerRecycleProductionApproval(original)
	if err != nil {
		return nil, err
	}
	block := types.Hash(approval.Approval.Production.ActivationNativeHash)
	var failures []error
	for _, endpoint := range cfg.Substrate {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(append(failures, err)...)
		}
		attempt, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(cfg))
		native, err := crv4.DialChainAtContext(attempt, endpoint, block)
		if err == nil {
			// A signed opt-in installs successor admission before any shared
			// read; without it this connection keeps exact runtime pins.
			err = enableReleaseRuntimeSuccession(native, cfg)
		}
		if err == nil {
			err = authenticateHistoricalNativeRuntimeAtContext(attempt, native, cfg, block)
		}
		cancel()
		if err == nil {
			return native, nil
		}
		if native != nil {
			native.API.Client.Close()
		}
		failures = append(failures, fmt.Errorf("approved historical native endpoint: %w", err))
	}
	return nil, fmt.Errorf("no approved historical native endpoint answered: %w", errors.Join(failures...))
}

// Each attempt releases its failed file/transport ownership. The actual read
// clients retain finite I/O budgets; local authenticated history reconstruction
// uses caller cancellation, never an unrelated timer that discards CPU work.
func awaitProductionStartupStage(ctx context.Context, cfg *ReleaseConfig, progress *releaseProgress, stage string, operation func(context.Context) error) error {
	if ctx == nil || cfg == nil || operation == nil || !isOwnerRecycleProductionConfig(cfg) {
		return errors.New("production startup stage has no lifecycle or authority")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt, cancel := context.WithCancel(ctx)
		err := errors.Join(operation(attempt), attempt.Err())
		cancel()
		if err == nil {
			return ctx.Err()
		}
		if ctx.Err() != nil || !retryableProductionSteeringRead(err) {
			return errors.Join(err, ctx.Err())
		}
		progress.observeSteering(0, false, "read_wait", false)
		code := map[string]string{"EVM identity": "startup_evm_identity_unavailable", "native identity": "startup_native_identity_unavailable", "activation history": "startup_activation_history_unavailable", "server-key history": "startup_server_key_history_unavailable", "disk and intent history": "startup_disk_intent_history_unavailable", "native retained owner": "startup_native_owner_unavailable"}[stage]
		if code == "" {
			code = "startup_unavailable"
		}
		releaseDiagnostic(ctx, "startup", code, 0, false, 0, releaseDiagnosticFacts{cause: releaseDiagnosticReadCause(err)})
		if err := waitReleaseSnapshotRetry(ctx, time.Duration(cfg.PollSeconds)*time.Second); err != nil {
			return err
		}
	}
}

// Every reconstructed participant must agree. The independent history, rather
// than a new current snapshot, selects the epoch before workers can be attached.
func (self *releaseRuntimeV2) authenticatedStartupEpoch() (uint64, error) {
	if self == nil || self.history == nil || len(self.history.participants) == 0 {
		return 0, errors.New("production startup has no authenticated participant history")
	}
	var epoch uint64
	for index, participant := range self.history.participants {
		cursor, exists := self.history.current[participant.NoID]
		if !exists || index > 0 && cursor.epoch != epoch {
			return 0, errors.New("production startup participant epochs disagree")
		}
		epoch = cursor.epoch
	}
	return epoch, nil
}

// The independent refresh worker stays dormant while an original native
// outcome is unresolved. Once requested, current identity/stake and real
// publication must both pass before trails or fresh native preparation begin.
func (self *ReleaseSteerer) runProductionPreparationAndRefresh(ctx context.Context) error {
	if self.runtimeV2 == nil || self.runtimeV2.preparation == nil {
		return errors.New("production preparation worker has no runtime owner")
	}
	runtime := self.runtimeV2
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtime.preparation.requested:
	}
	first := true
	for {
		if err := runtime.authenticationPending(nil); err != nil {
			var wait *productionOperatorAuthenticationPending
			if !errors.As(err, &wait) {
				return err
			}
			observeProductionAuthenticationWait(ctx, runtime.progress, wait)
			if err := waitReleaseSnapshotRetry(ctx, time.Duration(self.cfg.PollSeconds)*time.Second); err != nil {
				return err
			}
			continue
		}
		var snapshot *ReleaseSnapshot
		err := self.productionRead(ctx, productionReadPreparation, nil, func(attempt context.Context) error {
			var err error
			snapshot, err = self.chain.ReleaseSnapshotContext(attempt)
			if err != nil || !first {
				return err
			}
			uid, found, err := self.chain.FindUidByHotkeyAtHashContext(attempt, snapshot.BlockNumber, snapshot.BlockHash, self.cfg.Netuid, self.hotkey.PublicKey())
			if err != nil {
				return err
			}
			if !found {
				return errors.New("production current signing hotkey has no finalized UID")
			}
			// Current admission owns its metadata copy independently of the
			// steerer and historical readers sharing this physical client.
			native := *self.native
			_, err = authenticateReleaseValidatorStakeContext(attempt, &native, self.cfg, self.hotkey.PublicKey(), uid)
			return err
		})
		if err == nil {
			err = runtime.advance(ctx, snapshot)
		}
		if err != nil {
			if ctx.Err() != nil {
				return errors.Join(err, ctx.Err())
			}
			pending := releaseOnlyErrors(err, errAttemptCutPending, errAttemptCutSnapshotStale, errAttemptSettlementSnapshotStale)
			if !pending && !retryableProductionSteeringRead(err) {
				return fmt.Errorf("production preparation or settlement integrity: %w", err)
			}
			runtime.progress.observeSteering(0, false, "read_wait", false)
			releaseDiagnostic(ctx, "startup", "preparation_unavailable", 0, false, 0, releaseDiagnosticFacts{phase: productionReadPreparation, cause: releaseDiagnosticReadCause(err)})
		} else if first {
			runtime.preparation.readyOnce.Do(func() { close(runtime.preparation.ready) })
			first = false
		}
		if err := waitReleaseSnapshotRetry(ctx, time.Duration(self.cfg.PollSeconds)*time.Second); err != nil {
			return err
		}
	}
}
