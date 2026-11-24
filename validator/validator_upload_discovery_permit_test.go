//go:build linux || darwin

// Anyone can publish an activation: the journal checks the two keys' consent,
// not native eligibility. These tests publish as an outsider would and check
// that discovery, its bounds and the owner census refuse only that outsider,
// never the eligible validators the lane already serves.
package validator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

// A self-keyed activation: a fresh hotkey and VPK sign their own consent for
// the deployment's real domain, exactly what publishActivation accepts from
// any relayer. Neither key holds a permit.
func (self *validatorUploadAuthorityTestFixture) publishSelfKeyed(t *testing.T, seed byte, block uint64) {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{seed, 0x5e})
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(append([]byte{seed, 0x5f}, make([]byte, 30)...))
	record := self.authority.Expected
	record.Hotkey = hotkey.PublicKey()
	copy(record.VPK[:], key[ed25519.SeedSize:])
	vpkSignature, err := record.SignVPK(key)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Verify(record, vpkSignature, hotkeySignature); err != nil {
		t.Fatalf("self-keyed consent prerequisite: %v", err)
	}
	self.records[digest] = ValidatorEvidenceActivationPublication{Record: record, PublishedBlock: block}
	self.events = append(self.events, ValidatorUploadActivationEvent{Digest: digest, Hotkey: record.Hotkey, NoID: record.NoID, Epoch: record.Domain.Epoch, Block: block,
		BlockHash: [32]byte(common.BigToHash(new(big.Int).SetUint64(block))), Index: uint64(len(self.events))})
}

// The permitted validator's own activation for another operator: the same
// hotkey and keys, so it is a distinct owner with real consent.
func (self *validatorUploadAuthorityTestFixture) publishOperator(t *testing.T, noID, block uint64) protocol.ValidatorEvidenceActivation {
	t.Helper()
	if self.extraOperators == nil {
		self.extraOperators = make(map[uint64]bool)
	}
	self.extraOperators[noID] = true
	record := self.authority.Expected
	record.NoID = noID
	self.publish(t, record, block, ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...)))
	return record
}

// Begins and closes one upload for a record's owner at now, with consent
// signed at now, or returns the refusal.
func validatorUploadDiscoveryTestBegin(t *testing.T, fixture *validatorUploadAuthorityTestFixture, owner *ValidatorUploadAdmission, record protocol.ValidatorEvidenceActivation, now time.Time) error {
	t.Helper()
	signed := fixture.now
	fixture.now = now
	header, sessionHash, digest := validatorUploadAdmissionTestHeader(t, fixture, record, 0x41, "current-api-client", []byte("object"))
	fixture.now = signed
	lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, digest, 6, now)
	if err != nil {
		return err
	}
	lease.Close()
	return nil
}

// Nine self-keyed publishes beside the validator's own, ten in one range
// whose bound is eight. Before the permit filter the range refused the whole
// refresh, and with it every validator's uploads. The node is asked only for
// the permitted hotkey; a node that ignores the filter is filtered here.
func TestValidatorUploadAdmissionSelfKeyedActivationsCannotFillARange(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"", "unfiltered-node"} {
		t.Run("fault="+fault, func(t *testing.T) {
			fixture := newValidatorUploadAuthorityTestFixture(t)
			for index := range 9 {
				fixture.publishSelfKeyed(t, byte(0x51+index), uint64(1002+index))
			}
			if uint64(len(fixture.events)) <= fixture.config().MaximumEventsPerRange {
				t.Fatal("fixture does not exceed the configured range bound")
			}
			fixture.fault = fault
			owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, fixture.config())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
			if err := owner.refresh(t.Context(), fixture.now); err != nil {
				// The production loop clears the admitted set on a failed refresh.
				owner.invalidate(err)
				t.Fatalf("self-keyed activations failed the refresh for every validator: %v", err)
			}
			if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
				t.Fatalf("the eligible validator lost its upload lane: %v", err)
			}
			for _, list := range fixture.logHotkeys {
				if len(list) != 1 || list[0] != common.Hash(fixture.authority.Expected.Hotkey) {
					t.Fatalf("discovery asked the node for hotkeys outside the permit census: %v", list)
				}
			}
			if len(fixture.logHotkeys) == 0 || len(owner.entries) != 1 {
				t.Fatalf("discovery bypassed the permit filter or admitted an outsider: %d queries, %d entries", len(fixture.logHotkeys), len(owner.entries))
			}
		})
	}
}

// Four permitted owners fill maximum_owners. A fifth permitted owner's
// activation must be refused alone: before, the census bound failed the whole
// refresh and the four lost their admission with it.
func TestValidatorUploadAdmissionOwnerOverflowRefusesOnlyTheNewcomer(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	if fixture.config().MaximumOwners != 4 {
		t.Fatal("fixture owner bound changed")
	}
	records := []protocol.ValidatorEvidenceActivation{fixture.authority.Expected}
	for _, noID := range []uint64{3, 4, 5} {
		records = append(records, fixture.publishOperator(t, noID, 1000+noID))
	}
	owner := validatorUploadAdmissionTestState(t, fixture)
	if len(owner.entries) != 4 {
		t.Fatalf("the first four owners were not admitted: %d", len(owner.entries))
	}
	newcomer := fixture.publishOperator(t, 6, 1006)
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		// The production loop clears the admitted set on a failed refresh.
		owner.invalidate(err)
		t.Fatalf("a fifth permitted owner failed the refresh for every owner: %v", err)
	}
	for _, record := range records {
		if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, record, fixture.now); err != nil {
			t.Fatalf("admitted owner %d lost its slot to the overflow: %v", record.NoID, err)
		}
	}
	err := validatorUploadDiscoveryTestBegin(t, fixture, owner, newcomer, fixture.now)
	if err == nil || !strings.Contains(err.Error(), "maximum_owners 4 is full") {
		t.Fatalf("the overflow owner was not refused and reported: %v", err)
	}
	if len(owner.ownerSlots) != 4 {
		t.Fatalf("owner slots exceed maximum_owners: %d", len(owner.ownerSlots))
	}
}

// Slots of owners that are no longer admitted are released once idle, so
// slots never accumulate past the owners admitted now. Before, a retired
// owner's slot stayed forever and counted against maximum_owners.
func TestValidatorUploadAdmissionReleasesIdleSlotsOfRetiredOwners(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	for _, noID := range []uint64{3, 4, 5} {
		fixture.publishOperator(t, noID, 1000+noID)
	}
	owner := validatorUploadAdmissionTestState(t, fixture)
	if len(owner.ownerSlots) != 4 {
		t.Fatalf("prerequisite: four owner slots, got %d", len(owner.ownerSlots))
	}
	// The hotkey loses its permit, so every one of its owners retires.
	fixture.currentPermit = false
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if len(owner.entries) != 0 || len(owner.ownerSlots) != 0 {
		t.Fatalf("retired owners kept their slots: %d entries, %d slots", len(owner.entries), len(owner.ownerSlots))
	}
	// With the permit back, a fifth owner competes with the four for the
	// free slots. Equal stake ranks them by operator, so the newest waits.
	fixture.currentPermit = true
	newcomer := fixture.publishOperator(t, 6, 1006)
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	if len(owner.ownerSlots) != 4 || len(owner.entries) != 4 {
		t.Fatalf("owner capacity was not refilled exactly: %d slots, %d entries", len(owner.ownerSlots), len(owner.entries))
	}
	if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, newcomer, fixture.now); err == nil {
		t.Fatal("the lowest-ranked owner was admitted over capacity")
	}
}

// A refresh that fails on a timeout keeps the admitted set until the expiry
// its last complete refresh published, and refuses after it. Any other
// failure clears the set at once.
func TestValidatorUploadAdmissionTransientFailureKeepsAdmissionUntilExpiry(t *testing.T) {
	t.Parallel()
	timeout := &url.Error{Op: "Post", URL: "http://rpc.example", Err: context.DeadlineExceeded}
	for _, test := range []struct {
		name   string
		cause  error
		retain bool
	}{
		{name: "rpc timeout", cause: timeout, retain: true},
		{name: "joined timeouts", cause: errors.Join(timeout, context.DeadlineExceeded), retain: true},
		{name: "integrity", cause: errors.New("validator staging event conflicts with the canonical anchored record"), retain: false},
		{name: "timeout beside integrity", cause: errors.Join(timeout, errors.New("runtime at 0x01 has identity node-subtensor/999/1/1, which is not a successor")), retain: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newValidatorUploadAuthorityTestFixture(t)
			owner := validatorUploadAdmissionTestState(t, fixture)
			validUntil := owner.validUntil
			if validUntil.IsZero() || !fixture.now.Before(validUntil) {
				t.Fatal("prerequisite: a fresh admitted set")
			}
			owner.clock = func() time.Time { return fixture.now }
			if owner.retainAfterTransientRefreshFailure(test.cause) != test.retain {
				t.Fatalf("retention of %v differs from %t", test.cause, test.retain)
			}
			if !test.retain {
				owner.invalidate(test.cause)
				if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err == nil {
					t.Fatal("a non-transient failure kept the admitted set")
				}
				return
			}
			if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
				t.Fatalf("a transient failure refused the admitted validator inside the window: %v", err)
			}
			if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, validUntil); err == nil || !strings.Contains(err.Error(), "admission expired") {
				t.Fatalf("the retained set outlived its published expiry: %v", err)
			}
			// Once the window has passed, the next transient failure clears it.
			owner.clock = func() time.Time { return validUntil }
			if owner.retainAfterTransientRefreshFailure(test.cause) {
				t.Fatal("a transient failure after expiry kept the admitted set")
			}
		})
	}
}

// The node's own log-query timeout is transient; an ordinary RPC refusal is not.
func TestValidatorUploadAdmissionTransientRefreshClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		cause     error
		transient bool
	}{
		{cause: validatorUploadTestRPCError("query timeout of 10 seconds exceeded"), transient: true},
		{cause: validatorUploadTestRPCError("Method not allowed"), transient: false},
		{cause: errors.New("validator staging event result is absent or exceeds its bound"), transient: false},
		{cause: nil, transient: false},
	} {
		if got := transientValidatorUploadRefreshError(test.cause); got != test.transient {
			t.Errorf("%v: transient=%t, want %t", test.cause, got, test.transient)
		}
	}
}

// A JSON-RPC error as go-ethereum's client returns one from a node.
type validatorUploadTestRPCError string

func (self validatorUploadTestRPCError) Error() string  { return string(self) }
func (self validatorUploadTestRPCError) ErrorCode() int { return -32603 }
