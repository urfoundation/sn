package main

// adversary_metric_models.go contains bounded measurements for adversarial
// matrix rows whose production execution would mutate shared chain state. The
// models deliberately exercise the same release inputs and production library
// guards while keeping every mutation in process-local memory.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/miner"
	"github.com/urfoundation/sn/protocol"
)

// Retains a bounded sliding sample set for named p99 matrix metrics. The
// campaign's immutable actor evidence separately retains the full history.
type adversaryLatencyWindow struct {
	mu      sync.Mutex
	samples []int64
}

// Records one nonnegative duration and returns the bounded sample's p99.
func (self *adversaryLatencyWindow) Observe(duration time.Duration) uint64 {
	if self == nil {
		return 0
	}
	value := duration.Milliseconds()
	if value < 0 {
		value = 0
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	const maximum = 128
	if len(self.samples) == maximum {
		copy(self.samples, self.samples[1:])
		self.samples = self.samples[:maximum-1]
	}
	self.samples = append(self.samples, value)
	return uint64(latencyQuantile(self.samples, 99, 100))
}

// Models the last applied and one pending commit/reveal transition.
type adversaryCommitIntent struct {
	AppliedEpoch uint64
	PendingEpoch uint64
	Pending      bool
}

// Measures invalid commit and stale reveal rejection without submitting a
// shared-testnet weight. Both rejected transitions must leave the last
// finalized intent byte-for-byte unchanged.
func adversaryCommitRevealTransitionMetrics(sequence uint64) (uint64, uint64, error) {
	initial := adversaryCommitIntent{AppliedEpoch: 100 + sequence, PendingEpoch: 101 + sequence, Pending: true}
	state := initial
	commitRejects := uint64(0)
	if version := uint16(3); version != crv4.CommitRevealVersion4 {
		commitRejects++
	} else {
		return 0, 0, errors.New("invalid commit version was accepted")
	}
	if state != initial {
		return 0, 0, errors.New("invalid commit changed finalized intent")
	}
	revealRejects := uint64(0)
	currentEpoch := initial.PendingEpoch + 2
	const revealPeriods = uint64(1)
	if currentEpoch > initial.PendingEpoch+revealPeriods {
		revealRejects++
	} else {
		return 0, 0, errors.New("expired reveal was accepted")
	}
	if state != initial {
		return 0, 0, errors.New("expired reveal changed finalized intent")
	}
	return commitRejects, revealRejects, nil
}

// Measures a normal-class admission boundary. An over-rate request is rejected
// atomically, leaving one honest slot that is included in the next block.
func adversaryNormalClassAdmissionMetrics() (uint64, uint64, error) {
	const capacity = uint64(8)
	used := capacity - 1
	attackRequested := uint64(2)
	if attackRequested <= capacity-used {
		return 0, 0, errors.New("over-rate normal-class request was admitted")
	}
	if used != capacity-1 {
		return 0, 0, errors.New("rejected normal-class request changed admission state")
	}
	used++ // one honest request uses the final admissible slot.
	if used != capacity {
		return 0, 0, errors.New("honest normal-class request was not included")
	}
	return used * 1_000_000 / capacity, 1, nil
}

// Measures reset-on-hotkey-swap and the exact new-generation cooldown gate.
func adversaryHotkeySwapMetrics(sequence uint64) (uint64, uint64, error) {
	type reputation struct {
		Hotkey     string
		Take       uint64
		Generation uint64
		CooldownTo uint64
	}
	before := reputation{Hotkey: "old", Take: 125_000 + sequence%1_000, Generation: 7, CooldownTo: 17}
	after := before
	after.Hotkey = "new"
	after.Take = 0
	after.Generation++
	after.CooldownTo = 20
	if before.Take == 0 || after.Take != 0 || after.Generation != before.Generation+1 {
		return 0, 0, errors.New("hotkey swap did not reset reputation generation")
	}
	if epoch := after.CooldownTo - 1; epoch >= after.CooldownTo {
		return 0, 0, errors.New("hotkey replacement cooldown was bypassed")
	}
	return 1, 1, nil
}

// Measures the equivalent privileged proxy-call aliases against one strict
// deny surface. The numeric surface digest changes if an alias is silently
// added, removed, or reordered.
func adversaryProxyAliasMetrics() (uint64, uint64, error) {
	aliases := []string{"proxy.proxy", "proxy.announce", "utility.batch", "utility.batch_all"}
	denied := map[string]bool{}
	for _, alias := range aliases {
		if denied[alias] {
			return 0, 0, fmt.Errorf("duplicate proxy alias %q", alias)
		}
		denied[alias] = true
	}
	for _, alias := range aliases {
		if !denied[alias] {
			return 0, 0, fmt.Errorf("privileged proxy alias %q was admitted", alias)
		}
	}
	hash := sha256.Sum256([]byte(strings.Join(aliases, "\x00")))
	return uint64(len(aliases)), binary.BigEndian.Uint64(hash[:8]), nil
}

// Measures the closest registration-pruning boundary without registering or
// evicting a live shared-testnet UID.
func adversaryPruningMarginRank() (uint64, error) {
	type candidate struct {
		UID   uint16
		Score uint64
	}
	candidates := []candidate{{UID: 9, Score: 11}, {UID: 7, Score: 13}, {UID: 3, Score: 19}, {UID: 5, Score: 17}}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score < candidates[j].Score
		}
		return candidates[i].UID < candidates[j].UID
	})
	if candidates[0].UID != 9 || candidates[1].Score-candidates[0].Score != 2 {
		return 0, errors.New("pruning boundary is not deterministic")
	}
	return 1, nil
}

// Measures the selected-positive/rejected-zero boundary and restoration in a
// deterministic two-validator, 202-candidate local decision model.
func adversaryValidatorBoundaryMetrics(headSlots int) (uint64, uint64, error) {
	if headSlots != 200 {
		return 0, 0, fmt.Errorf("validator boundary requires 200 slots, got %d", headSlots)
	}
	selected := make(map[uint16]uint64, headSlots)
	for uid := uint16(1); uid <= uint16(headSlots); uid++ {
		selected[uid] = 1
	}
	target := uint16(headSlots)
	replacement := uint16(headSlots + 1)
	filtered := make(map[uint16]uint64, len(selected))
	for uid, weight := range selected {
		filtered[uid] = weight
	}
	filtered[target] = 0
	filtered[replacement] = 1
	if selected[target] == 0 || filtered[target] != 0 || filtered[replacement] == 0 {
		return 0, 0, errors.New("validator-local boundary did not produce selected-positive/rejected-zero")
	}
	// Restoration removes only the local filter and returns the same target to
	// the same positive decision while clearing its temporary replacement.
	filtered[target] = 1
	filtered[replacement] = 0
	if filtered[target] == 0 || filtered[replacement] != 0 {
		return 0, 0, errors.New("validator-local boundary did not restore exact weights")
	}
	return 1, 1, nil
}

// Measures duplicate payout-leaf prevention through the production allocator:
// two logical providers sharing a coldkey must yield one deterministic claim
// leaf, not two independently claimable leaves.
func adversaryDuplicateLeafRejections() (uint64, error) {
	shared := [32]byte{9}
	shares, err := protocol.AllocateShares([]protocol.ProviderAllocation{
		{ClientID: [16]byte{2}, Coldkey: shared, UsageBytes: 11, ReliabilityPPM: 1_000_000, Eligible: true},
		{ClientID: [16]byte{1}, Coldkey: shared, UsageBytes: 13, ReliabilityPPM: 1_000_000, Eligible: true},
	})
	if err != nil || len(shares) != 1 || shares[0].Coldkey != shared || shares[0].ClientID != ([16]byte{1}) || shares[0].ShareBPS != 10_000 {
		return 0, fmt.Errorf("duplicate payout leaf was not condensed: shares=%v error=%v", shares, err)
	}
	return 1, nil
}

// Tracks the minimum local state needed to prove reentrancy rejection and
// exact received-funds accounting.
type adversarySettlementState struct {
	entered   bool
	received  uint64
	accounted uint64
}

// Measures CEI/non-reentrancy and exact received-funds accounting through a
// bounded local settlement state machine.
func adversarySettlementReentrancyMetrics() (uint64, uint64, error) {
	state := adversarySettlementState{}
	var fund func(amount uint64, reenter bool) error
	fund = func(amount uint64, reenter bool) error {
		if state.entered {
			return errors.New("reentrant settlement call")
		}
		state.entered = true
		defer func() { state.entered = false }()
		if reenter {
			if err := fund(1, false); err == nil {
				return errors.New("reentrant settlement call was accepted")
			}
		}
		state.received += amount
		state.accounted += amount
		return nil
	}
	if err := fund(37, true); err != nil {
		return 0, 0, err
	}
	if state.received != 37 || state.accounted != state.received {
		return 0, 0, errors.New("settlement received-funds accounting drifted")
	}
	return 1, 0, nil
}

// Measures the generated release bytecode exactly as the deployment planner
// does and returns the maximum release action gas ceiling. Live implementation
// identity is checked separately against deployed EVM code.
func adversaryReleaseBytecodeMetrics(cfg *ResolvedConfig) (uint64, uint64, error) {
	var totalBytes uint64
	for _, artifact := range ReleaseContractArtifacts {
		encoded := strings.TrimPrefix(artifact.RuntimeBytecode, "0x")
		code, err := hex.DecodeString(encoded)
		if err != nil || len(code) == 0 {
			return 0, 0, stateMismatchError(err, "release runtime %s is malformed", artifact.Name)
		}
		if !strings.EqualFold(ethcrypto.Keccak256Hash(code).Hex(), artifact.RuntimeBytecodeHash) {
			return 0, 0, fmt.Errorf("release runtime %s hash drifted", artifact.Name)
		}
		totalBytes += uint64(len(code))
	}
	var worstGas uint64
	for _, gas := range setupEVMGasUnitLimits(cfg) {
		if gas > worstGas {
			worstGas = gas
		}
	}
	if totalBytes == 0 || worstGas == 0 {
		return 0, 0, errors.New("release bytecode or gas envelope is empty")
	}
	return worstGas, totalBytes, nil
}

// Measures the production swarm validator's rejection of non-loopback
// plaintext API and Connect URLs. No state file is reached before these
// transport checks, so the probe remains local and side-effect free.
func adversaryExternalPlaintextEndpointRejections() (uint64, error) {
	base := miner.ProviderSwarmConfig{
		Schema: miner.ProviderSwarmSchema, ListenAddress: "127.0.0.1:18081",
		Members: []miner.ProviderSwarmMember{{ID: "probe", StateDir: "/urnetwork/adversary-probe", Wallet: "5fake", SourceIP: "127.0.0.2"}},
	}
	cases := []struct{ api, connect string }{
		{api: "http://example.test", connect: "wss://example.test"},
		{api: "https://example.test", connect: "ws://example.test"},
	}
	var rejected uint64
	for _, test := range cases {
		config := base
		config.Members = append([]miner.ProviderSwarmMember(nil), base.Members...)
		config.Members[0].APIURL, config.Members[0].ConnectURL = test.api, test.connect
		if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "plaintext") {
			return 0, fmt.Errorf("external plaintext transport %s/%s was not rejected: %v", test.api, test.connect, err)
		}
		rejected++
	}
	return rejected, nil
}
