//go:build linux || darwin

// One campaign-owned worker relays complete public source censuses while the
// real miners, validators and adversaries run. The shared keeper serializes
// account nonces; this worker owns only discovery, admission and its lifecycle.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Independent configured sources, never a candidate manifest's member list.
type evidenceRelaySource struct {
	validatorId uint64
	stateDir    string
	bounds      validatorcomponent.ReleaseEvidenceV2Bounds
	activations []protocol.ValidatorEvidenceActivation
	nextEpoch   uint64
}

// The worker alone mutates sources and admits actions. The small state lock
// protects only progress/error notification, never HTTP, files or chain calls.
type evidenceRelayRuntime struct {
	executor             *Executor
	chain                *validatorcomponent.ChainClient
	origins              [2]string
	sources              []evidenceRelaySource
	ctx                  context.Context
	cancel               context.CancelFunc
	done                 chan struct{}
	fail                 func(error)
	poll                 time.Duration
	phase                string
	prepared             bool
	work                 evidenceRelayWork
	horizon              *evidenceRelayHorizon
	ready                chan struct{}
	remainingRequests    chan evidenceRelayRemainingRequest
	stateLock            sync.Mutex
	changed              chan struct{}
	through              map[uint64]uint64
	completed            map[uint64]bool
	startedAuditPasses   uint64
	completedAuditPasses uint64
	resultErr            error
}

// Construction authenticates fixed inputs and opens an owned chain client
// before starting the worker; it never creates missing activation files.
func newEvidenceRelayRuntime(ctx context.Context, approved *ResolvedConfig, executor *Executor, phase string, prepared bool, fail func(error)) (*evidenceRelayRuntime, error) {
	self,err:=openEvidenceRelayRuntime(ctx,approved,executor,phase,prepared,fail)
	if err!=nil { return nil,err }
	go self.run()
	return self,nil
}

// Read-only capture owns the same authenticating readers, with no worker,
// journal writer, nonce admission or publication goroutine.
func openEvidenceRelayRuntime(ctx context.Context, approved *ResolvedConfig, executor *Executor, phase string, prepared bool, fail func(error)) (*evidenceRelayRuntime, error) {
	if ctx == nil || approved == nil || executor == nil || executor.cfg == nil || executor.plan == nil || executor.plan.ValidatorEvidence == nil || fail == nil {
		return nil, errors.New("evidence relay runtime owners are incomplete")
	}
	if phase != "release-1.0" && phase != "production-soak" {
		return nil, errors.New("evidence relay runtime has no funded campaign phase")
	}
	// The local proxy is an I/O derivative, not the endpoint identity approved
	// in the plan. Authenticate that identity before opening the real transport.
	if err := validateCampaignRPCTransport(approved, executor.cfg); err != nil {
		return nil, err
	}
	cfg, err := runtimeEvidenceV2ResolvedConfig(approved, executor.stateDir)
	if err != nil {
		return nil, err
	}
	if err := validateSimulatorEvidenceV2Census(cfg.Config); err != nil {
		return nil, err
	}
	if len(cfg.OperatorAPIOrigins) != 2 || cfg.Public.Chain.ExpectedBlockSeconds == 0 {
		return nil, errors.New("evidence relay requires two configured public origins and a finite polling cadence")
	}
	self := &evidenceRelayRuntime{executor: executor, origins: [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]},
		done: make(chan struct{}), changed: make(chan struct{}), through: map[uint64]uint64{}, completed: map[uint64]bool{}, fail: fail,
		phase: phase, prepared: prepared, ready: make(chan struct{}), remainingRequests: make(chan evidenceRelayRemainingRequest, 1),
		poll: time.Duration(cfg.Public.Chain.ExpectedBlockSeconds) * time.Second}
	if self.poll <= 0 {
		return nil, errors.New("evidence relay polling cadence overflows")
	}
	for _, configured := range cfg.Config.ValidatorEvidenceV2 {
		source := evidenceRelaySource{validatorId: configured.ValidatorID, stateDir: filepath.Join(executor.stateDir, "runtime", fmt.Sprintf("validator-%d", configured.ValidatorID), "state"), bounds: configured.Evidence.Bounds}
		if executor.plan.EvidenceRelayContinuation!=nil {
			source.stateDir=filepath.Join(executor.stateDir,"runtime",fmt.Sprintf("validator-%d",configured.ValidatorID),"coordinator-state-v2")
			if err:=validateEvidenceRelayContinuationNamespace(executor.plan,source.validatorId,source.stateDir);err!=nil { return nil,err }
		}
		for _, operator := range configured.Evidence.Operators {
			raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.Activation, uint64(protocol.ValidatorEvidenceActivationPayloadSize))
			if err != nil {
				return nil, err
			}
			activation, err := protocol.DecodeValidatorEvidenceActivationPayload(raw)
			if err != nil {
				return nil, err
			}
			hotkey, key, err := runtimeEvidenceActivationKeysV2(executor.roles, configured.ValidatorID, operator.NoID)
			if err != nil {
				return nil, err
			}
			if activation.Hotkey != hotkey.PublicKey() || activation.NoID != operator.NoID || string(activation.VPK[:]) != string(key[32:]) {
				return nil, errors.New("evidence relay activation differs from its original configured role")
			}
			vpk, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.VPKSignature, 64)
			if err != nil {
				return nil, err
			}
			hotkeySignature, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.HotkeySignature, 64)
			if err != nil {
				return nil, err
			}
			if err := activation.Verify(activation, vpk, hotkeySignature); err != nil {
				return nil, err
			}
			if len(source.activations) == 0 {
				source.nextEpoch = activation.Domain.Epoch
			} else if source.nextEpoch != activation.Domain.Epoch {
				return nil, errors.New("evidence relay source census has different activation epochs")
			}
			source.activations = append(source.activations, activation)
		}
		self.sources = append(self.sources, source)
		self.completed[source.validatorId] = false
	}
	self.chain, err = executor.runtimeEvidenceActivationChainV2(ctx)
	if err != nil {
		return nil, err
	}
	self.ctx, self.cancel = context.WithCancel(ctx)
	return self, nil
}

// Strip only owned shutdown cancellation leaves, retaining adjacent I/O or
// finality errors joined with cancellation. Deadlines remain real failures.
func evidenceRelayNonCancellationError(err error) error {
	if err == nil || err == context.Canceled {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var retained []error
		for _, child := range joined.Unwrap() {
			retained = append(retained, evidenceRelayNonCancellationError(child))
		}
		return errors.Join(retained...)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok && evidenceRelayNonCancellationError(wrapped.Unwrap()) == nil {
		return nil
	}
	return err
}

// Every worker exit joins its owned chain resources before publishing done.
func (self *evidenceRelayRuntime) run() {
	var outcome error
	completedAudits := map[evidenceRelayAuditKey][32]byte{}
	defer func() {
		if self.ctx.Err() != nil {
			outcome = evidenceRelayNonCancellationError(outcome)
		}
		self.chain.Close()
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.resultErr = outcome
			close(self.changed)
			self.changed = make(chan struct{})
		}()
		if outcome != nil {
			self.fail(outcome)
		}
		close(self.done)
	}()
	if err := self.prepareHorizon(); err != nil {
		outcome = fmt.Errorf("validator evidence funded phase admission: %w", err)
		return
	}
	close(self.ready)
	for {
		if err := self.ctx.Err(); err != nil {
			outcome = err
			return
		}
		if err := self.advance(); err != nil {
			outcome = fmt.Errorf("validator evidence relay: %w", err)
			return
		}
		pass, err := func() (uint64, error) {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			if self.startedAuditPasses == ^uint64(0) {
				return 0, errors.New("evidence audit completion sequence overflows")
			}
			self.startedAuditPasses++
			return self.startedAuditPasses, nil
		}()
		if err != nil {
			outcome = err
			return
		}
		if err := self.advanceDepositAudits(completedAudits); err != nil {
			outcome = fmt.Errorf("validator evidence audit relay: %w", err)
			return
		}
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.completedAuditPasses = pass
			close(self.changed)
			self.changed = make(chan struct{})
		}()
		select {
		case <-self.ctx.Done():
			outcome = self.ctx.Err()
			return
		case request := <-self.remainingRequests:
			outcome = self.checkRemaining(request)
			request.result <- outcome
			if outcome != nil {
				return
			}
		case <-time.After(self.poll):
		}
	}
}

// A missing next manifest is ordinary unpublished state, never permission to
// skip a source or epoch. The campaign's required-epoch wait enforces its end.
func (self *evidenceRelayRuntime) advance() error {
	block, hash, err := self.chain.FinalizedBlockContext(self.ctx)
	if err != nil {
		return err
	}
	if err := self.checkHorizonBlock(block); err != nil {
		return err
	}
	for index := range self.sources {
		source := &self.sources[index]
		path, err := validatorcomponent.ValidatorEvidencePublicationV2ManifestPath(source.stateDir, source.nextEpoch)
		if err != nil {
			return err
		}
		manifest, err := validatorcomponent.ReadValidatorEvidencePublicationV2Manifest(self.ctx, path, source.bounds.MaxClosureBytes, source.bounds.MaxParticipants)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		requests, err := self.readClosedPublication(self.ctx, source, manifest, block, hash)
		if err != nil {
			return err
		}
		for _, expected := range requests {
			if err := self.horizon.admit(expected.Evidence.Header, block); err != nil {
				return err
			}
			action, ownerPlanHash, err := self.executor.admitOwnedEvidenceRelayAction(self.ctx, expected)
			if err != nil {
				return err
			}
			result, err := self.executor.keeper.relayValidatorEvidenceTransaction(self.ctx, self.chain, ownerPlanHash, action, expected)
			if err != nil {
				return err
			}
			if result == nil || result.Winner == nil {
				return errors.New("evidence relay returned no canonical winner")
			}
			if err := self.retainOwnedResult(ownerPlanHash,action, result); err != nil {
				return err
			}
		}
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.through[source.validatorId], self.completed[source.validatorId] = source.nextEpoch, true
			close(self.changed)
			self.changed = make(chan struct{})
		}()
		if source.nextEpoch == ^uint64(0) {
			return errors.New("evidence relay reached the terminal uint64 epoch")
		}
		source.nextEpoch++
	}
	return self.ctx.Err()
}

// Retain immutable inclusion facts, not the moving finalized observation head.
// On restart the sender reauthenticates the original winning transaction.
func (self *evidenceRelayRuntime) retainResult(action Action, result *evidenceRelayTransactionResult) error {
	return self.retainOwnedResult(self.executor.plan.PlanHash,action,result)
}

func (self *evidenceRelayRuntime) retainOwnedResult(ownerPlanHash string,action Action, result *evidenceRelayTransactionResult) error {
	value := struct {
		Schema              string                                          `json:"schema"`
		PlanHash            string                                          `json:"plan_hash"`
		Action              Action                                          `json:"action"`
		SignedTransaction   []byte                                          `json:"winner_signed_transaction"`
		Receipt             *types.Receipt                                  `json:"winner_receipt"`
		Publication         validatorcomponent.ValidatorEvidencePublication `json:"publication"`
		OwnReceipt          *types.Receipt                                  `json:"own_receipt,omitempty"`
		LostPublicationRace bool                                            `json:"lost_publication_race"`
	}{Schema: "urnetwork-sim-evidence-relay-result-v2", PlanHash: ownerPlanHash, Action: action,
		SignedTransaction: result.Winner.SignedTransaction, Receipt: result.Winner.Receipt, Publication: result.Winner.Publication,
		OwnReceipt: result.OwnReceipt, LostPublicationRace: result.LostPublicationRace}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = validatorcomponent.WriteReleaseEvidenceV2File(self.ctx, filepath.Join(self.executor.stateDir, "evidence-relay", action.ID+".receipt.json"), raw, 4*1024*1024)
	return err
}

// Subscribe and read progress in one locked operation so no completion edge
// can fall between them. The caller supplies the original campaign deadline.
func (self *evidenceRelayRuntime) WaitThrough(ctx context.Context, epoch uint64) error {
	if ctx == nil {
		return errors.New("evidence relay completion context is absent")
	}
	for {
		ready, changed, outcome := func() (bool, <-chan struct{}, error) {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			changed := self.changed
			ready := len(self.sources) > 0
			for validatorId, completed := range self.completed {
				ready = ready && completed && self.through[validatorId] >= epoch
			}
			return ready, changed, self.resultErr
		}()
		if outcome != nil {
			return outcome
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		case <-self.done:
			return errors.New("evidence relay stopped before every configured source completed")
		}
	}
}

// The campaign calls this before closing its shared keeper or deployment lock.
func (self *evidenceRelayRuntime) Close() error {
	self.cancel()
	<-self.done
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.resultErr
}
