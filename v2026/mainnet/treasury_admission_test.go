// Admission roots exercise original native positions, owner exclusions and
// policy generations before any signature or treasury transmission is created.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Register the complete synthetic roster under the independently derived account.
func treasuryTestRegistered(t *testing.T, f *treasuryFixture) {
	t.Helper()
	multisig, _ := hex.DecodeString(f.input.Action.Descriptor.Multisig.AccountId[2:])
	net := []byte{25, 0}
	f.set(t, "SubtensorModule", "SubnetworkN", []byte{2, 0}, net)
	for i, recipient := range f.input.Action.Descriptor.RecipientHotkeys {
		hotkey, _ := hex.DecodeString(recipient.AccountId[2:])
		uid := binary.LittleEndian.AppendUint16(nil, uint16(i))
		f.set(t, "SubtensorModule", "Owner", multisig, hotkey)
		f.set(t, "SubtensorModule", "Uids", uid, net, hotkey)
		f.set(t, "SubtensorModule", "Keys", hotkey, net, uid)
		f.set(t, "SubtensorModule", "IsNetworkMember", []byte{1}, hotkey, net)
		f.set(t, "SubtensorModule", "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, uint64(20+i)), net, uid)
	}
}

// Foreign mutable Owner rows cannot erase original owner-set exclusion.
func TestTreasuryObservationRejectsRecognizedOwnerAndCapacityExhaustion(t *testing.T) {
	f := newTreasuryFixture(t)
	a := f.input.Action
	owner := bytes.Repeat([]byte{71}, 32)
	hotkey, _ := hex.DecodeString(a.Descriptor.RecipientHotkeys[0].AccountId[2:])
	key := f.set(t, "SubtensorModule", "OwnedHotkeys", append([]byte{4}, hotkey...), owner)
	read := func(_ context.Context, key string, fallback []byte) ([]byte, bool, error) {
		v, ok := f.values[key]
		if !ok {
			return fallback, false, nil
		}
		raw, err := hex.DecodeString(v[2:])
		return raw, true, err
	}
	if _, err := treasuryReadFacts(t.Context(), a, f.metadata, a.BirthBlock, read); !errors.Is(err, errRpcIntegrity) {
		t.Fatal("recognized owner hotkey admitted via absent Owner mapping", err)
	}
	delete(f.values, key)
	f.set(t, "SubtensorModule", "MaxAllowedUids", []byte{1, 0}, []byte{25, 0})
	facts, err := treasuryReadFacts(t.Context(), a, f.metadata, a.BirthBlock, read)
	if err != nil {
		t.Fatal(err)
	}
	if err := facts.admits(a, true); err == nil {
		t.Fatal("registration could prune an existing seat before complete treasury roster fits")
	}
}

// The exact validator projection has no local device paths and cannot survive
// UID reuse, even when a current response still contains the same hotkey.
func TestTreasuryPublicPolicyProjectionPinsActualRecipientGenerations(t *testing.T) {
	f := newTreasuryFixture(t)
	treasuryTestRegistered(t, f)
	a := f.input.Action
	observation := f.observation(t, a, a.BirthBlock, a.BirthHash)
	facts, err := observation.facts(t.Context(), a, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := treasuryPublicPolicy(a.Descriptor, facts)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	a.Inner = treasuryInnerCall{Kind: "transfer_keep_alive", Destination: "0x" + hex.EncodeToString(bytes.Repeat([]byte{79}, 32)), Amount: 100}
	a.Treasury, a.TreasuryPolicyHash = &policy, "0x"+hex.EncodeToString(hash[:])
	if err := a.validateTreasuryPolicy(); err != nil {
		t.Fatal(err)
	}
	input := f.input
	input.Action = a
	input.Observation = f.observation(t, a, a.BirthBlock, a.BirthHash)
	input.Action.ObservationHash = input.Observation.ContentHash
	path, _ := treasuryTestJson(t, filepath.Dir(a.StatePath), "transfer-plan.json", input)
	var out, diagnostic bytes.Buffer
	if code := runTreasuryCommand(t.Context(), []string{"plan", "--input", path}, &out, &diagnostic); code != 0 {
		t.Fatal("approved policy failed exact native transfer admission", diagnostic.String())
	}
	f.set(t, "SubtensorModule", "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 80), []byte{25, 0}, []byte{0, 0})
	changed := f.observation(t, a, a.BirthBlock, a.BirthHash)
	changedFacts, err := changed.facts(t.Context(), a, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := changedFacts.admits(a, false); err == nil {
		t.Fatal("current UID generation replaced original treasury policy")
	}
	input.Action = f.input.Action
	input.Observation = observation
	path, _ = treasuryTestJson(t, filepath.Dir(a.StatePath), "policy-plan.json", input)
	out.Reset()
	diagnostic.Reset()
	if code := runTreasuryCommand(t.Context(), []string{"policy-plan", "--input", path}, &out, &diagnostic); code != 0 {
		t.Fatal(diagnostic.String())
	}
	if bytes.Contains(out.Bytes(), []byte("device_config")) || bytes.Contains(out.Bytes(), []byte(filepath.Dir(a.StatePath))) || !bytes.Contains(out.Bytes(), []byte(`"approved":false`)) {
		t.Fatal("public projection leaked local custody or claimed approval", out.String())
	}
}

// The selected hotkey's collateral is required even when a sibling contributes
// ample coldkey-wide availability. An absent post-liquidation position stays nil.
func TestTreasuryLiquidationChecksSelectedCollateralAndOriginalAvailability(t *testing.T) {
	f := newTreasuryFixture(t)
	treasuryTestRegistered(t, f)
	a := f.input.Action
	o := f.observation(t, a, a.BirthBlock, a.BirthHash)
	facts, err := o.facts(t.Context(), a, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := treasuryPublicPolicy(a.Descriptor, facts)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := policy.Hash()
	a.Treasury, a.TreasuryPolicyHash = &policy, "0x"+hex.EncodeToString(digest[:])
	a.Inner = treasuryInnerCall{Kind: "remove_stake_limit", Hotkey: a.Descriptor.RecipientHotkeys[0].AccountId, Amount: 100, LimitPrice: 1}
	hotkey, _ := hex.DecodeString(a.Inner.Hotkey[2:])
	coldkey, _ := hex.DecodeString(a.Descriptor.Multisig.AccountId[2:])
	positionInput := append(append(append([]byte(nil), hotkey...), coldkey...), 25, 0)
	positionKey := "runtime-api:" + treasuryPositionApi + ":0x" + hex.EncodeToString(positionInput)
	position := append(append([]byte{1}, hotkey...), coldkey...)
	for _, v := range []uint64{25, 200, 0, 0, 0, 0} {
		position = append(position, rootCompact(v)...)
	}
	position = append(position, 1)
	f.values[positionKey] = "0x" + hex.EncodeToString(position)
	availabilityInput := append([]byte{4}, coldkey...)
	availabilityInput = append(availabilityInput, 1, 4, 25, 0)
	availabilityKey := "runtime-api:" + treasuryAvailabilityApi + ":0x" + hex.EncodeToString(availabilityInput)
	availability := append([]byte{4}, coldkey...)
	availability = append(availability, 4, 25, 0)
	for _, v := range []uint64{1000, 0, 900} {
		availability = append(availability, rootCompact(v)...)
	}
	f.values[availabilityKey] = "0x" + hex.EncodeToString(availability)
	collateral := make([]byte, 40)
	binary.LittleEndian.PutUint64(collateral, 100)
	f.set(t, "SubtensorModule", "MinerCollateral", collateral, []byte{25, 0}, hotkey, coldkey)
	o = f.observation(t, a, a.BirthBlock, a.BirthHash)
	facts, err = o.facts(t.Context(), a, f.metadata)
	if err != nil || facts.LiquidationAvailable == nil || *facts.LiquidationAvailable != 100 {
		t.Fatal("selected collateral was covered by a sibling", facts.LiquidationAvailable, err)
	}
	if err := facts.admits(a, true); err != nil {
		t.Fatal(err)
	}
	a.Inner.Amount = 101
	if err := facts.admits(a, true); err == nil {
		t.Fatal("collateral became liquidated principal")
	}
	a.Inner.Amount = 100
	f.values[positionKey] = "0x00"
	o = f.observation(t, a, a.BirthBlock, a.BirthHash)
	facts, err = o.facts(t.Context(), a, f.metadata)
	if err != nil || facts.LiquidationAvailable != nil {
		t.Fatal("absent position became observed free balance", err)
	}
	if err := facts.admits(a, true); err == nil {
		t.Fatal("absent original position admitted a new liquidation")
	}
	key, err := treasuryCollateralKey(f.metadata, hotkey, coldkey)
	expected, expectedErr := types.CreateStorageKey(f.metadata, "SubtensorModule", "MinerCollateral", []byte{25, 0}, hotkey, coldkey)
	if err != nil || expectedErr != nil || !bytes.Equal(key, expected) {
		t.Fatal("collateral selected another position", err, expectedErr)
	}
}
