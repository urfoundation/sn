package main

// Deterministic decision-model supplements for five security and economic
// boundaries added by Subtensor runtime 453. The pinned upstream Rust tests and
// source review establish runtime behavior; these Go models keep the harness's
// adversarial oracles stable while live actors continuously pin deployed code.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
)

// Mirror the runtime verifier's explicit binding after its BLS pairing has
// authenticated the round signature.
func runtime453DrandPulseAuthenticates(signature, randomness []byte, pairingValid bool) bool {
	if !pairingValid {
		return false
	}
	digest := sha256.Sum256(signature)
	return bytes.Equal(randomness, digest[:])
}

// Distinguish the outer proxy envelope from the final effect reached through
// that envelope.
type runtime453ProxyCallClass uint8

const (
	runtime453ProxyEnvelope runtime453ProxyCallClass = iota
	runtime453BalanceTransfer
	runtime453StakeMutation
)

// Represent the subset of calls admitted by one proxy definition.
type runtime453ProxyFilter struct {
	AllowedCalls map[runtime453ProxyCallClass]bool
}

// Apply one filter without treating an absent call class as authorized.
func (self runtime453ProxyFilter) allows(call runtime453ProxyCallClass) bool {
	return self.AllowedCalls[call]
}

// Cover both pallet entry points whose inherited origins were repaired.
type runtime453NestedProxyPath uint8

const (
	runtime453DirectProxy runtime453NestedProxyPath = iota
	runtime453AnnouncedProxy
)

// Require the outer authority to admit both the proxy envelope and the final
// effect, then intersect it with the inner proxy authority.
func runtime453NestedProxyAllows(path runtime453NestedProxyPath, outer, inner runtime453ProxyFilter, finalCall runtime453ProxyCallClass) bool {
	switch path {
	case runtime453DirectProxy, runtime453AnnouncedProxy:
	default:
		return false
	}
	return outer.allows(runtime453ProxyEnvelope) && outer.allows(finalCall) && inner.allows(finalCall)
}

// Identify the repaired payable path and the unchanged adjacent keep-alive path
// which preserve the same true-caller policy through different mechanisms.
type runtime453BalanceTransferEntrypoint uint8

const (
	runtime453TransferAllowDeath runtime453BalanceTransferEntrypoint = iota
	runtime453TransferKeepAlive
)

// Model the effective swap policy. Runtime 453 adds an explicit true-caller
// precheck to payable transfer; keep-alive already dispatches as that caller
// through the centralized runtime filter.
func runtime453BalanceTransferCallerAllowed(entrypoint runtime453BalanceTransferEntrypoint, caller string, pendingColdkeySwaps map[string]bool) bool {
	switch entrypoint {
	case runtime453TransferAllowDeath, runtime453TransferKeepAlive:
	default:
		return false
	}
	return caller != "" && !pendingColdkeySwaps[caller]
}

// Keep protocol-owned basket custody outside user stake-transition targets.
func runtime453StakeTransferDestinationAllowed(destination, betaEscrow string) bool {
	return destination != "" && betaEscrow != "" && destination != betaEscrow
}

// Retain the payer/hotkey binding and economic state captured when a deferred
// subnet registration enters the queue.
type runtime453QueuedRegistration struct {
	Coldkey     string
	Hotkey      string
	LockAmount  uint64
	QueuedBlock uint64
}

// Model only the queue-time state changed by runtime 453. Applying a queued
// entry must not price the same registration a second time.
type runtime453RegistrationQueueState struct {
	HotkeyOwners   map[string]string
	Queue          []runtime453QueuedRegistration
	LastLock       uint64
	LastLockBlock  uint64
	RegisteredRows uint64
}

// Reserve the hotkey and consume the global lock/rate-limit state atomically.
func (self *runtime453RegistrationQueueState) queue(coldkey, hotkey string, lockAmount, currentBlock, rateLimit uint64) error {
	if coldkey == "" || hotkey == "" || lockAmount == 0 || currentBlock == 0 {
		return errors.New("registration identity, lock, and block must be nonzero")
	}
	if owner, ok := self.HotkeyOwners[hotkey]; ok && owner != coldkey {
		return errors.New("queued hotkey is already owned by another coldkey")
	}
	if self.LastLockBlock != 0 && (currentBlock < self.LastLockBlock || currentBlock-self.LastLockBlock < rateLimit) {
		return errors.New("queued registration exceeds the network rate limit")
	}
	if self.HotkeyOwners == nil {
		self.HotkeyOwners = map[string]string{}
	}
	self.HotkeyOwners[hotkey] = coldkey
	self.LastLock = lockAmount
	self.LastLockBlock = currentBlock
	self.Queue = append(self.Queue, runtime453QueuedRegistration{
		Coldkey: coldkey, Hotkey: hotkey, LockAmount: lockAmount, QueuedBlock: currentBlock,
	})
	return nil
}

// Materialize the oldest entry while preserving its queue-time pricing state.
func (self *runtime453RegistrationQueueState) applyNext() error {
	if len(self.Queue) == 0 {
		return errors.New("registration queue is empty")
	}
	entry := self.Queue[0]
	if self.HotkeyOwners[entry.Hotkey] != entry.Coldkey {
		return errors.New("queued hotkey reservation changed before application")
	}
	self.Queue = self.Queue[1:]
	self.RegisteredRows++
	return nil
}

// A valid BLS pairing alone cannot authenticate a separately supplied
// randomness field; the signature digest is now an independent hard boundary.
func TestRuntime453DrandPulseBindsRandomnessToSignatureDigest(t *testing.T) {
	signature := []byte("runtime-453-reviewed-quicknet-signature")
	digest := sha256.Sum256(signature)
	tampered := append([]byte(nil), digest[:]...)
	tampered[0] ^= 1

	if !runtime453DrandPulseAuthenticates(signature, digest[:], true) {
		t.Fatal("signature-bound drand pulse was rejected")
	}
	if runtime453DrandPulseAuthenticates(signature, tampered, true) {
		t.Fatal("pairing-valid pulse with unbound randomness was accepted")
	}
	if runtime453DrandPulseAuthenticates(signature, digest[:], false) {
		t.Fatal("signature-digest match bypassed a failed BLS pairing")
	}
}

// The direct path used to discard the already-authenticated outer filter when
// the inner proxy dispatched its final effect.
func TestRuntime453NestedDirectProxyIntersectsOuterAndInnerFilters(t *testing.T) {
	outer := runtime453ProxyFilter{AllowedCalls: map[runtime453ProxyCallClass]bool{
		runtime453ProxyEnvelope: true,
	}}
	inner := runtime453ProxyFilter{AllowedCalls: map[runtime453ProxyCallClass]bool{
		runtime453BalanceTransfer: true,
		runtime453StakeMutation:   true,
	}}
	if !inner.allows(runtime453BalanceTransfer) {
		t.Fatal("fixture does not reproduce the pre-fix inner-only authorization")
	}
	if runtime453NestedProxyAllows(runtime453DirectProxy, outer, inner, runtime453BalanceTransfer) {
		t.Fatal("direct nested proxy discarded its outer transfer restriction")
	}
	outer.AllowedCalls[runtime453StakeMutation] = true
	if !runtime453NestedProxyAllows(runtime453DirectProxy, outer, inner, runtime453StakeMutation) {
		t.Fatal("direct nested proxy rejected an effect admitted by both filters")
	}
}

// The announced path shares the same intersection rule even though its delay
// and announcement checks occur before proxy dispatch.
func TestRuntime453NestedAnnouncedProxyIntersectsOuterAndInnerFilters(t *testing.T) {
	outer := runtime453ProxyFilter{AllowedCalls: map[runtime453ProxyCallClass]bool{
		runtime453ProxyEnvelope: true,
	}}
	inner := runtime453ProxyFilter{AllowedCalls: map[runtime453ProxyCallClass]bool{
		runtime453BalanceTransfer: true,
	}}
	if !inner.allows(runtime453BalanceTransfer) {
		t.Fatal("fixture does not reproduce the pre-fix announced inner-only authorization")
	}
	if runtime453NestedProxyAllows(runtime453AnnouncedProxy, outer, inner, runtime453BalanceTransfer) {
		t.Fatal("announced nested proxy discarded its outer transfer restriction")
	}
	outer.AllowedCalls[runtime453BalanceTransfer] = true
	if !runtime453NestedProxyAllows(runtime453AnnouncedProxy, outer, inner, runtime453BalanceTransfer) {
		t.Fatal("announced nested proxy rejected an effect admitted by both filters")
	}
}

// Frontier moves msg.value into the precompile account before dispatch, but a
// pending coldkey swap belongs to the true caller's authorization state.
func TestRuntime453BalanceTransferChecksTrueEVMCallerColdkeySwap(t *testing.T) {
	caller := "evm-caller"
	dispatchAccount := "balance-transfer-precompile"
	entrypoints := []struct {
		name       string
		entrypoint runtime453BalanceTransferEntrypoint
	}{
		{name: "transferAllowDeath", entrypoint: runtime453TransferAllowDeath},
		{name: "transferKeepAlive", entrypoint: runtime453TransferKeepAlive},
	}
	for _, testCase := range entrypoints {
		pending := map[string]bool{caller: true}
		if pending[dispatchAccount] {
			t.Fatalf("%s fixture does not reproduce the pre-fix dispatch-account bypass", testCase.name)
		}
		if runtime453BalanceTransferCallerAllowed(testCase.entrypoint, caller, pending) {
			t.Fatalf("%s let a caller with a pending coldkey swap transfer", testCase.name)
		}

		pending = map[string]bool{dispatchAccount: true}
		if !runtime453BalanceTransferCallerAllowed(testCase.entrypoint, caller, pending) {
			t.Fatalf("%s applied the internal dispatch account's swap state to an unflagged caller", testCase.name)
		}
	}
}

// A one-rao same-subnet transfer previously bypassed minimum-amount defenses
// and could create protocol escrow holdings without matching basket shares.
func TestRuntime453StakeTransferRejectsBetaEscrowDestination(t *testing.T) {
	betaEscrow := "protocol-beta-escrow"
	if runtime453StakeTransferDestinationAllowed(betaEscrow, betaEscrow) {
		t.Fatal("user stake transition into beta escrow was accepted")
	}
	if !runtime453StakeTransferDestinationAllowed("user-coldkey", betaEscrow) {
		t.Fatal("ordinary user stake destination was rejected")
	}
}

// Deferred registration must establish ownership immediately so a rival
// registration cannot acquire the hotkey before queue processing.
func TestRuntime453QueuedRegistrationReservesHotkeyAtQueueTime(t *testing.T) {
	state := &runtime453RegistrationQueueState{HotkeyOwners: map[string]string{}}
	if err := state.queue("payer", "new-hotkey", 50, 100, 10); err != nil {
		t.Fatal(err)
	}
	if state.HotkeyOwners["new-hotkey"] != "payer" {
		t.Fatal("queued registration left its hotkey unreserved")
	}
	if err := state.queue("rival", "new-hotkey", 60, 110, 10); err == nil {
		t.Fatal("rival acquired a queue-reserved hotkey")
	}
	if len(state.Queue) != 1 || state.HotkeyOwners["new-hotkey"] != "payer" {
		t.Fatalf("failed rival registration mutated queue state: %+v", state)
	}
}

// Pricing and rate-limit state belongs to queue admission, while later queue
// materialization must leave the recorded admission block and lock untouched.
func TestRuntime453QueuedRegistrationConsumesLockAndRateStateAtQueueTime(t *testing.T) {
	state := &runtime453RegistrationQueueState{HotkeyOwners: map[string]string{}}
	if err := state.queue("payer-a", "hotkey-a", 50, 100, 100); err != nil {
		t.Fatal(err)
	}
	if state.LastLock != 50 || state.LastLockBlock != 100 {
		t.Fatalf("queue admission did not consume price state: %+v", state)
	}
	if err := state.queue("payer-b", "hotkey-b", 75, 199, 100); err == nil {
		t.Fatal("second registration bypassed the queue-time rate limit")
	}
	if _, ok := state.HotkeyOwners["hotkey-b"]; ok || len(state.Queue) != 1 || state.LastLock != 50 || state.LastLockBlock != 100 {
		t.Fatalf("rate-limited registration partially mutated state: %+v", state)
	}
	if err := state.applyNext(); err != nil {
		t.Fatal(err)
	}
	if state.LastLock != 50 || state.LastLockBlock != 100 || state.RegisteredRows != 1 {
		t.Fatalf("queue application repriced an admitted registration: %+v", state)
	}
	if err := state.queue("payer-b", "hotkey-b", 75, 200, 100); err != nil {
		t.Fatalf("exact rate-limit boundary was rejected: %v", err)
	}
	if state.LastLock != 75 || state.LastLockBlock != 200 {
		t.Fatalf("next queue admission did not advance price state: %+v", state)
	}
}
