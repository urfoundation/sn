package commitmentoracle

// The oracle loop. Each cycle:
//
//  1. reads the coordinator at the EVM finalized block and stops unless the
//     key's address is the active commitment oracle;
//  2. reconciles its own unfinished transaction, and sends nothing new while
//     one is in flight;
//  3. lists CommitmentOf(netuid, *) at the native finalized head, keeps the
//     fleet-format entries whose hotkey is registered without a validator
//     permit on every endpoint (a role read failure mirrors nothing that
//     cycle), that are not mirrored at the EVM finalized block and
//     not older than commitmentMaxAgeBlocks, and derives each one's arguments
//     from that same finalized state (LastCommitment, the canonical block hash
//     and its header) plus positive fleet evidence in the commitment block's
//     body (role.go), cross-checked on every other configured endpoint;
//  4. mirrors them nearest expiry first, one at a time, each waiting for a
//     final receipt and checking the coordinator's record afterwards.
//
// A failed cycle backs off exponentially up to the maximum backoff. The
// oracle is driven by one goroutine and is not safe for concurrent use.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Operating settings; every bound has a flag in run.go.
type Settings struct {
	Netuid      uint16
	ChainId     uint64
	Coordinator common.Address
	// Holds mirror-sends/, the durable transaction records.
	StateDir string
	// Prometheus textfile; empty writes none.
	MetricsFile    string
	DryRun         bool
	Once           bool
	PollInterval   time.Duration
	MaxBackoff     time.Duration
	ConfirmTimeout time.Duration
	ConfirmPoll    time.Duration
	MaxGasPrice    *big.Int
	MaxGasLimit    uint64
	LowBalanceWei  *big.Int
	// Mirrors sent (or, in a dry run, logged) per cycle.
	MaxSendsPerCycle int
}

// The chain connections of one dial.
type connections struct {
	primary     nativeReader
	secondaries []nativeReader
	evm         evmClient
}

func (self connections) close() {
	if self.primary != nil {
		self.primary.close()
	}
	for _, secondary := range self.secondaries {
		secondary.close()
	}
	if self.evm != nil {
		self.evm.close()
	}
}

// Consecutive failed cycles before the connections are dialed again.
const redialAfterFailures = 3

type oracle struct {
	settings    Settings
	address     common.Address
	connections connections
	coordinator *coordinatorReader
	sender      *sender
	metrics     *metrics
	now         func() time.Time
	sleep       func(ctx context.Context, duration time.Duration) error
	logf        func(format string, args ...any)
	// Dials fresh connections after repeated failures; nil keeps the first.
	dial func(ctx context.Context) (connections, error)
	// The native finalized head of the last complete cycle.
	processedNative uint64
	failures        int
}

func newOracle(settings Settings, address common.Address, connections connections, sender *sender, now func() time.Time, logf func(string, ...any)) *oracle {
	sender.client = connections.evm
	return &oracle{
		settings:    settings,
		address:     address,
		connections: connections,
		coordinator: newCoordinatorReader(connections.evm, settings.Coordinator),
		sender:      sender,
		metrics:     newMetrics(settings, now()),
		now:         now,
		sleep:       sleepContext,
		logf:        logf,
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(duration):
		return nil
	}
}

// Startup admission: the RPC's chain id, the coordinator's netuid and the
// active commitment oracle at the EVM finalized block must all match. The
// oracle refuses to run otherwise.
func (self *oracle) admit(ctx context.Context) error {
	chainId, err := self.connections.evm.chainId(ctx)
	if err != nil {
		return err
	}
	if !chainId.IsUint64() || chainId.Uint64() != self.settings.ChainId {
		return fmt.Errorf("the EVM RPC reports chain id %s, want %d", chainId, self.settings.ChainId)
	}
	finalized, _, err := self.connections.evm.finalizedBlock(ctx)
	if err != nil {
		return err
	}
	netuid, err := self.coordinator.netuid(ctx, finalized)
	if err != nil {
		return err
	}
	if netuid != self.settings.Netuid {
		return fmt.Errorf("coordinator %s serves netuid %d, want %d", self.settings.Coordinator.Hex(), netuid, self.settings.Netuid)
	}
	active, err := self.coordinator.activeCommitmentOracle(ctx, finalized)
	if err != nil {
		return err
	}
	if active != self.address {
		self.metrics.oracleActive = 0
		return fmt.Errorf("%w: the active oracle at finalized block %d is %s, the key is %s", errNotActiveOracle, finalized, active.Hex(), self.address.Hex())
	}
	self.metrics.oracleActive = 1
	return nil
}

// Runs cycles until ctx ends, or one cycle with Settings.Once.
func (self *oracle) run(ctx context.Context) error {
	for {
		err := self.cycle(ctx)
		if err != nil && ctx.Err() == nil {
			self.failures += 1
			self.metrics.failed(self.now())
			self.logf("cycle failed (%d in a row): %v", self.failures, err)
		} else if err == nil {
			self.failures = 0
			self.metrics.succeeded()
		}
		self.writeMetrics()
		if self.settings.Once || ctx.Err() != nil {
			return err
		}
		if self.failures >= redialAfterFailures && self.dial != nil {
			if connections, dialErr := self.dial(ctx); dialErr == nil {
				self.connections.close()
				self.connections = connections
				self.coordinator = newCoordinatorReader(connections.evm, self.settings.Coordinator)
				self.sender.client = connections.evm
			} else {
				self.logf("redial: %v", dialErr)
			}
		}
		wait := self.settings.PollInterval
		for i := 0; i < self.failures && wait < self.settings.MaxBackoff; i += 1 {
			wait *= 2
		}
		wait = min(wait, max(self.settings.MaxBackoff, self.settings.PollInterval))
		if err := self.sleep(ctx, wait); err != nil {
			return nil
		}
	}
}

func (self *oracle) writeMetrics() {
	if self.settings.MetricsFile == "" {
		return
	}
	if err := self.metrics.write(self.settings.MetricsFile, self.now()); err != nil {
		self.logf("metrics: %v", err)
	}
}

// A mirror waiting to be sent, with its expiry for ordering.
type pendingMirror struct {
	args        mirrorArgs
	expiryBlock uint64
}

// One pass over finalized state.
func (self *oracle) cycle(ctx context.Context) error {
	evmFinalized, _, err := self.connections.evm.finalizedBlock(ctx)
	if err != nil {
		return err
	}
	self.metrics.evmFinalized = evmFinalized
	active, err := self.coordinator.activeCommitmentOracle(ctx, evmFinalized)
	if err != nil {
		return err
	}
	if active != self.address {
		self.metrics.oracleActive = 0
		return fmt.Errorf("%w: the active oracle at finalized block %d is %s, the key is %s", errNotActiveOracle, evmFinalized, active.Hex(), self.address.Hex())
	}
	self.metrics.oracleActive = 1
	paused, err := self.coordinator.paused(ctx, evmFinalized)
	if err != nil {
		return err
	}
	self.metrics.paused = boolGauge(paused)
	balance, err := self.connections.evm.balanceAt(ctx, self.address, nil)
	if err != nil {
		return err
	}
	self.metrics.setBalance(balance)

	// Our own unfinished transaction first; nothing new is signed while one is
	// in flight.
	unfinished, err := self.sender.unfinished()
	if err != nil {
		return err
	}
	for _, send := range unfinished {
		state, err := self.sender.reconcile(ctx, send)
		if err != nil {
			return err
		}
		switch state {
		case recordPending:
			self.metrics.setInFlight(send.record.SignedUnix, self.now())
			self.logf("mirror %s is not final yet (%s)", send.args, send.record.TransactionHash)
			return nil
		case recordDone:
			if err := self.checkMirrored(ctx, send.args); err != nil {
				return err
			}
			self.metrics.lastMirror = float64(self.now().Unix())
		}
	}
	self.metrics.setInFlight(0, self.now())

	head, err := self.connections.primary.finalizedHead(ctx)
	if err != nil {
		return err
	}
	self.metrics.nativeFinalized = head.number
	if self.processedNative != 0 && head.number >= self.processedNative {
		self.metrics.lagBlocks = head.number - self.processedNative
	}
	entries, err := self.connections.primary.commitments(ctx, self.settings.Netuid, head.hash)
	if err != nil {
		return err
	}
	maxAge, err := self.coordinator.commitmentMaxAgeBlocks(ctx, evmFinalized)
	if err != nil {
		return err
	}

	pending := []pendingMirror{}
	counts := map[skipReason]int{}
	fleetCount := 0
	for _, entry := range entries {
		commitment, err := parseFleetCommitment(entry)
		if err != nil {
			counts[skipNotFleetFormat] += 1
			continue
		}
		fleetCount += 1
		// Role first: a validator's source commitment has the fleet format. A
		// role that cannot be read fails the cycle before anything is sent.
		role, err := self.role(ctx, commitment.hotkey, head)
		if err != nil {
			return err
		}
		if !role.registered || role.validatorPermit {
			reason := skipUnregistered
			if role.validatorPermit {
				reason = skipValidator
			}
			counts[reason] += 1
			self.logf("not mirroring hotkey 0x%x commitment 0x%x block %d: %s", commitment.hotkey, commitment.commitmentHash, commitment.commitmentBlock, reason)
			continue
		}
		mirrored, err := self.coordinator.mirroredCommitment(ctx, commitment.hotkey, evmFinalized)
		if err != nil {
			return err
		}
		reason := mirrorDecision(commitment, head.number, maxAge, mirrored)
		if reason != skipNone {
			counts[reason] += 1
			if reason == skipConflict || reason == skipInconsistent || reason == skipNewerMirrored {
				self.logf("not mirroring hotkey 0x%x commitment 0x%x block %d: %s (mirrored 0x%x at block %d)", commitment.hotkey, commitment.commitmentHash, commitment.commitmentBlock, reason, mirrored.commitmentHash, mirrored.finalizedBlock)
			}
			continue
		}
		args, err := self.derive(ctx, commitment, head)
		var noEvidence *noFleetEvidenceError
		if errors.As(err, &noEvidence) {
			counts[skipNoFleetEvidence] += 1
			self.logf("not mirroring hotkey 0x%x block %d: %s: %v", commitment.hotkey, commitment.commitmentBlock, skipNoFleetEvidence, err)
			continue
		}
		if err != nil {
			counts[skipInconsistent] += 1
			self.logf("not mirroring hotkey 0x%x block %d: %v", commitment.hotkey, commitment.commitmentBlock, err)
			continue
		}
		pending = append(pending, pendingMirror{args: args, expiryBlock: commitment.commitmentBlock + maxAge})
	}
	slices.SortFunc(pending, func(a pendingMirror, b pendingMirror) int {
		if a.expiryBlock != b.expiryBlock {
			if a.expiryBlock < b.expiryBlock {
				return -1
			}
			return 1
		}
		return bytes.Compare(a.args.hotkey[:], b.args.hotkey[:])
	})
	self.metrics.setCommitments(len(entries), fleetCount, counts)
	self.metrics.setPending(len(pending), head.number, pendingExpiries(pending))

	if paused {
		return errCoordinatorPaused
	}
	sent := 0
	for index, mirror := range pending {
		if sent >= self.settings.MaxSendsPerCycle {
			break
		}
		outcome, send, err := self.sender.send(ctx, mirror.args)
		if err != nil {
			if errors.Is(err, errNotActiveOracle) {
				self.metrics.oracleActive = 0
			}
			return err
		}
		switch outcome {
		case outcomeStale, outcomeInvalid:
			self.logf("preflight of mirror %s reverted (%s); it is re-evaluated next cycle", mirror.args, outcome)
			continue
		case outcomeDryRun:
			sent += 1
			continue
		}
		sent += 1
		self.metrics.mirrorsSent += 1
		state, err := self.waitFinal(ctx, *send)
		if err != nil {
			return err
		}
		switch state {
		case recordPending:
			self.metrics.setInFlight(send.record.SignedUnix, self.now())
			self.logf("mirror %s sent and not final within %s; resuming next cycle", mirror.args, self.settings.ConfirmTimeout)
			return nil
		case recordDropped:
			return fmt.Errorf("mirror %s was dropped (%s)", mirror.args, send.record.TransactionHash)
		}
		if err := self.checkMirrored(ctx, mirror.args); err != nil {
			return err
		}
		self.metrics.lastMirror = float64(self.now().Unix())
		self.metrics.setPending(len(pending)-index-1, head.number, pendingExpiries(pending[index+1:]))
		self.logf("mirrored %s in %s", mirror.args, send.record.TransactionHash)
	}
	self.processedNative = head.number
	self.metrics.lagBlocks = 0
	self.metrics.lastSuccess = float64(self.now().Unix())
	return nil
}

func pendingExpiries(pending []pendingMirror) []uint64 {
	expiries := []uint64{}
	for _, mirror := range pending {
		expiries = append(expiries, mirror.expiryBlock)
	}
	return expiries
}

// The hotkey's role at the finalized head on every endpoint, combined to the
// most restrictive. Any read failure or disagreement is an error.
func (self *oracle) role(ctx context.Context, hotkey [32]byte, head nativeHead) (neuronRole, error) {
	roles := []neuronRole{}
	for index, reader := range append([]nativeReader{self.connections.primary}, self.connections.secondaries...) {
		role, err := reader.role(ctx, self.settings.Netuid, hotkey, head.hash)
		if err != nil {
			return neuronRole{}, fmt.Errorf("substrate endpoint %d role: %w", index+1, err)
		}
		roles = append(roles, role)
	}
	return combineRoles(roles)
}

// The commitment block holds no positive fleet evidence on some endpoint.
type noFleetEvidenceError struct {
	endpoint int
	err      error
}

func (self *noFleetEvidenceError) Error() string {
	return fmt.Sprintf("substrate endpoint %d: %v", self.endpoint, self.err)
}

func (self *noFleetEvidenceError) Unwrap() error {
	return self.err
}

// Requires positive fleet evidence (role.go fleetEvidence) in the body of the
// commitment block at blockHash on every endpoint.
func (self *oracle) requireFleetEvidence(ctx context.Context, commitment fleetCommitment, blockHash [32]byte) error {
	for index, reader := range append([]nativeReader{self.connections.primary}, self.connections.secondaries...) {
		call, err := reader.fleetCommitmentCall(self.settings.Netuid, commitment.commitmentHash)
		if err != nil {
			return fmt.Errorf("substrate endpoint %d: %w", index+1, err)
		}
		extrinsics, err := reader.blockExtrinsics(ctx, blockHash)
		if err != nil {
			return fmt.Errorf("substrate endpoint %d: %w", index+1, err)
		}
		if err := fleetEvidence(extrinsics, commitment.hotkey, call); err != nil {
			return &noFleetEvidenceError{endpoint: index + 1, err: err}
		}
	}
	return nil
}

// Derives the mirror arguments at the finalized head and requires every other
// endpoint to report the same commitment bytes at that head and the same
// canonical hash at the commitment block, and every endpoint to show positive
// fleet evidence in that block.
func (self *oracle) derive(ctx context.Context, commitment fleetCommitment, head nativeHead) (mirrorArgs, error) {
	primary := self.connections.primary
	lastCommitment, err := primary.lastCommitment(ctx, self.settings.Netuid, commitment.hotkey, head.hash)
	if err != nil {
		return mirrorArgs{}, err
	}
	blockHash, err := primary.blockHash(ctx, commitment.commitmentBlock)
	if err != nil {
		return mirrorArgs{}, err
	}
	headerNumber, err := primary.headerNumber(ctx, blockHash)
	if err != nil {
		return mirrorArgs{}, err
	}
	args, err := deriveMirrorArgs(commitment, head.number, lastCommitment, blockHash, headerNumber)
	if err != nil {
		return mirrorArgs{}, err
	}
	expected, err := primary.commitment(ctx, self.settings.Netuid, commitment.hotkey, head.hash)
	if err != nil {
		return mirrorArgs{}, err
	}
	for index, secondary := range self.connections.secondaries {
		raw, err := secondary.commitment(ctx, self.settings.Netuid, commitment.hotkey, head.hash)
		if err != nil {
			return mirrorArgs{}, fmt.Errorf("substrate endpoint %d: %w", index+2, err)
		}
		if !bytes.Equal(raw, expected) {
			return mirrorArgs{}, fmt.Errorf("substrate endpoint %d disagrees on the commitment at the finalized head", index+2)
		}
		hash, err := secondary.blockHash(ctx, commitment.commitmentBlock)
		if err != nil {
			return mirrorArgs{}, fmt.Errorf("substrate endpoint %d: %w", index+2, err)
		}
		if hash != blockHash {
			return mirrorArgs{}, fmt.Errorf("substrate endpoint %d disagrees on the hash of block %d", index+2, commitment.commitmentBlock)
		}
	}
	if err := self.requireFleetEvidence(ctx, commitment, blockHash); err != nil {
		return mirrorArgs{}, err
	}
	return args, nil
}

// Waits up to ConfirmTimeout for a sent record to become final or dropped.
func (self *oracle) waitFinal(ctx context.Context, send pendingSend) (recordState, error) {
	deadline := self.now().Add(self.settings.ConfirmTimeout)
	for {
		state, err := self.sender.reconcile(ctx, send)
		if err != nil || state != recordPending {
			return state, err
		}
		if !self.now().Before(deadline) {
			return recordPending, nil
		}
		if err := self.sleep(ctx, self.settings.ConfirmPoll); err != nil {
			return recordPending, err
		}
	}
}

// The coordinator's record at the EVM finalized block must equal the mirror.
func (self *oracle) checkMirrored(ctx context.Context, args mirrorArgs) error {
	finalized, _, err := self.connections.evm.finalizedBlock(ctx)
	if err != nil {
		return err
	}
	record, err := self.coordinator.mirroredCommitment(ctx, args.hotkey, finalized)
	if err != nil {
		return err
	}
	if !args.matches(record) && record.finalizedBlock <= args.finalizedBlock {
		return fmt.Errorf("after mirroring %s the coordinator holds commitment 0x%x block %d block hash 0x%x", args, record.commitmentHash, record.finalizedBlock, record.finalizedBlockHash)
	}
	return nil
}
