// Complete signed source controls keep primitive reuse local, exact and
// bounded while every journal candidate and per-use check remains reachable.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

// Each control borrows a genuine binding from the fully verified complete
// cold graph, retaining its manifest and original exact signed byte strings.
type finalGenerationBindingCheckMember struct {
	binding         protocol.FleetBinding
	clientSignature []byte
	hotkeySignature []byte
	manifest        *protocol.FleetManifest
	commitment      FleetCommitmentEvidence
}

// Decode real archived bytes without manufacturing a proof or a verdict. The
// exercised check below must authenticate the actual detached signatures.
func finalGenerationBindingCheckInput(t *testing.T, source *finalFleetGenerationSource, fleet, member uint64) finalGenerationBindingCheckMember {
	t.Helper()
	manifestData, err := source.record(fmt.Sprintf("public/fleet-%d.json", fleet))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := protocol.ParseFleetManifest(manifestData)
	if err != nil || member == 0 || member > uint64(len(manifest.Members)) {
		t.Fatalf("real binding manifest is incomplete: %v", err)
	}
	var commitment FleetCommitmentEvidence
	if err := decodeStrictJSONBytes(source.archive.files[fmt.Sprintf("public/fleet-%d.commitment.json", fleet)], &commitment); err != nil {
		t.Fatal(err)
	}
	var evidence FleetBindingEvidence
	if err := decodeStrictJSONBytes(source.archive.files[fmt.Sprintf("public/fleet-%d-member-%d.binding.json", fleet, member)], &evidence); err != nil {
		t.Fatal(err)
	}
	binding, err := manifest.Binding(manifest.Members[member-1], evidence.ValidFromEpoch, evidence.ValidToEpoch)
	if err != nil {
		t.Fatal(err)
	}
	client, clientOK := evidenceFixedHex(evidence.ClientSignature, ed25519.SignatureSize)
	hotkey, hotkeyOK := evidenceFixedHex(evidence.HotkeySignature, 64)
	if !clientOK || !hotkeyOK {
		t.Fatal("real binding signatures have invalid widths")
	}
	return finalGenerationBindingCheckMember{binding: binding, clientSignature: client, hotkeySignature: hotkey, manifest: manifest, commitment: commitment}
}

// A second install on the same source rechecks every installed member even
// when parsed versions are warm; no primitive success crosses a public use.
func TestFinalFleetGenerationBindingReuseIsInstallLocal(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	checks := 0
	source.workObserver = func(work finalFleetGenerationWorkObservation) {
		if work.stage == "initial-member-authentication" {
			checks++
		}
	}
	want := len(batch.InstalledFleets) * int(finalFleetGenerationMembersPerFleet)
	for use := 1; use <= 2; use++ {
		_ = finalGenerationWorkInstallBytes(t, source, batch)
		if checks != use*want {
			t.Fatalf("install use %d performed %d complete checks, want %d", use, checks, use*want)
		}
	}
}

// The standalone entrypoint cannot accidentally inherit an install's success
// and still authenticates the full real journal binding on every invocation.
func TestFinalFleetGenerationBindingReuseLeavesStandaloneChecks(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	fleet := batch.InstalledFleets[0]
	input := finalGenerationBindingCheckInput(t, source, fleet, 1)
	checks := 0
	source.workObserver = func(work finalFleetGenerationWorkObservation) {
		if work.stage == "initial-member-authentication" {
			checks++
		}
	}
	for use := 1; use <= 2; use++ {
		if _, err := source.initialMember(fleet, 1, input.manifest, input.commitment); err != nil {
			t.Fatal(err)
		}
		if checks != use {
			t.Fatalf("standalone use %d performed %d checks", use, checks)
		}
	}
}

// Real distinct messages fill the exact capacity. Hits do not alter FIFO
// order; one more successful tuple evicts and the oldest must reauthenticate.
func TestFinalFleetGenerationBindingReuseEvictsAtFixedCapacity(t *testing.T) {
	t.Parallel()
	source, _ := finalGenerationWorkFixture(t, nil)
	checks := &finalFleetGenerationBindingChecks{}
	calls := 0
	observe := func() { calls++ }
	inputs := make([]finalGenerationBindingCheckMember, finalFleetGenerationBindingCheckLimit+1)
	for index := range inputs {
		inputs[index] = finalGenerationBindingCheckInput(t, source, uint64(index)/finalFleetGenerationMembersPerFleet+1, uint64(index)%finalFleetGenerationMembersPerFleet+1)
		input := inputs[index]
		if !checks.verify(input.binding, input.clientSignature, input.hotkeySignature, observe) {
			t.Fatalf("genuine capacity tuple %d failed", index)
		}
		if len(checks.checked) != min(index+1, int(finalFleetGenerationBindingCheckLimit)) {
			t.Fatalf("capacity after tuple %d is %d", index, len(checks.checked))
		}
		if index+1 == int(finalFleetGenerationBindingCheckLimit) {
			first := inputs[0]
			if !checks.verify(first.binding, first.clientSignature, first.hotkeySignature, observe) || calls != index+1 {
				t.Fatal("exact hit reauthenticated or changed FIFO admission")
			}
		}
	}
	first := inputs[0]
	if !checks.verify(first.binding, first.clientSignature, first.hotkeySignature, observe) || calls != len(inputs)+1 || len(checks.checked) != int(finalFleetGenerationBindingCheckLimit) {
		t.Fatal("oldest evicted signature tuple did not reauthenticate within the bound")
	}
	last := inputs[len(inputs)-1]
	if !checks.verify(last.binding, last.clientSignature, last.hotkeySignature, observe) || calls != len(inputs)+1 {
		t.Fatal("unevicted exact tuple was not retained")
	}
}

// Every changed message/domain/key and fresh valid randomized signature is a
// distinct tuple. All alternatives are genuinely signed and fully verified.
func TestFinalFleetGenerationBindingReuseRequiresCompleteTuple(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	input := finalGenerationBindingCheckInput(t, source, batch.InstalledFleets[0], 1)
	context := finalSemanticFixtureFleetTestContext(t)
	clientRole := context.roles.Clients[fmt.Sprintf("miner-%d", fleetMemberMinerIndex(context.cfg, int(batch.InstalledFleets[0]), 1))]
	clientSeed, err := hex.DecodeString(clientRole.SeedHex)
	if err != nil || len(clientSeed) != ed25519.SeedSize {
		t.Fatal("real client signer is unavailable")
	}
	private := ed25519.NewKeyFromSeed(clientSeed)
	hotkeySeed, err := hex.DecodeString(context.roles.Substrate[fleetHotkeyLabel(int(batch.InstalledFleets[0]))].SeedHex)
	if err != nil || len(hotkeySeed) != 32 {
		t.Fatal("real hotkey signer is unavailable")
	}
	var fixed [32]byte
	copy(fixed[:], hotkeySeed)
	pair, err := crv4.KeypairFromSeed(fixed)
	if err != nil {
		t.Fatal(err)
	}
	alternatePrivate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5a}, ed25519.SeedSize))
	alternatePair, err := crv4.KeypairFromSeed([32]byte{0x6b})
	if err != nil {
		t.Fatal(err)
	}
	checks := &finalFleetGenerationBindingChecks{}
	calls := 0
	observe := func() { calls++ }
	if !checks.verify(input.binding, input.clientSignature, input.hotkeySignature, observe) {
		t.Fatal("original genuine tuple failed")
	}
	for index, kind := range []string{"fresh-signature", "validity", "chain", "netuid", "coordinator", "client-key", "hotkey"} {
		binding, signer, hotkey := input.binding, private, pair
		switch kind {
		case "validity":
			binding.ValidToEpoch++
		case "chain":
			binding.ChainID++
		case "netuid":
			binding.Netuid++
		case "coordinator":
			binding.Coordinator[0] ^= 1
		case "client-key":
			signer = alternatePrivate
			copy(binding.ClientKey[:], signer.Public().(ed25519.PublicKey))
		case "hotkey":
			hotkey = alternatePair
			binding.Hotkey = hotkey.PublicKey()
		}
		clientSignature, err := binding.SignClient(signer)
		if err != nil {
			t.Fatalf("%s client signature: %v", kind, err)
		}
		digest, err := binding.Digest()
		if err != nil {
			t.Fatal(err)
		}
		hotkeySignature, err := hotkey.Sign(digest[:])
		if err != nil || kind == "fresh-signature" && bytes.Equal(hotkeySignature, input.hotkeySignature) {
			t.Fatalf("%s did not produce a distinct genuine signature: %v", kind, err)
		}
		if !checks.verify(binding, clientSignature, hotkeySignature, observe) || calls != index+2 {
			t.Fatalf("%s reused a different tuple, checks=%d", kind, calls)
		}
		if !checks.verify(binding, clientSignature, hotkeySignature, observe) || calls != index+2 {
			t.Fatalf("%s exact success was not retained", kind)
		}
	}
}

// Failure is never an entry, whether caused by message/key substitution or a
// changed detached signature. Restoring the original remains independently valid.
func TestFinalFleetGenerationBindingReuseNeverCachesFailure(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	input := finalGenerationBindingCheckInput(t, source, batch.InstalledFleets[0], 1)
	for _, kind := range []string{"message", "client-key", "hotkey", "client-signature", "hotkey-signature"} {
		checks := &finalFleetGenerationBindingChecks{}
		binding, client, hotkey := input.binding, bytes.Clone(input.clientSignature), bytes.Clone(input.hotkeySignature)
		switch kind {
		case "message":
			binding.ValidToEpoch++
		case "client-key":
			binding.ClientKey[0] ^= 1
		case "hotkey":
			binding.Hotkey[0] ^= 1
		case "client-signature":
			client[0] ^= 1
		case "hotkey-signature":
			hotkey[0] ^= 1
		}
		calls := 0
		for use := 1; use <= 2; use++ {
			if checks.verify(binding, client, hotkey, func() { calls++ }) || calls != use || len(checks.checked) != 0 {
				t.Fatalf("%s failure was accepted or cached on use %d", kind, use)
			}
		}
		if !checks.verify(input.binding, input.clientSignature, input.hotkeySignature, func() { calls++ }) || calls != 3 || len(checks.checked) != 1 {
			t.Fatalf("%s failed tuple hid the restored genuine message", kind)
		}
	}
}

// The tuple and primitive inputs are owned before the observation boundary.
// Later caller mutation cannot poison the stored key or change what was checked.
func TestFinalFleetGenerationBindingReuseOwnsSignatureArguments(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	input := finalGenerationBindingCheckInput(t, source, batch.InstalledFleets[0], 1)
	client, hotkey := bytes.Clone(input.clientSignature), bytes.Clone(input.hotkeySignature)
	checks := &finalFleetGenerationBindingChecks{}
	calls := 0
	if !checks.verify(input.binding, client, hotkey, func() { calls++; client[0] ^= 1; hotkey[0] ^= 1 }) {
		t.Fatal("observer changed the owned primitive inputs")
	}
	if !checks.verify(input.binding, input.clientSignature, input.hotkeySignature, func() { calls++ }) || calls != 1 {
		t.Fatal("caller mutation changed the original successful tuple")
	}
	if checks.verify(input.binding, client, hotkey, func() { calls++ }) || calls != 2 || len(checks.checked) != 1 {
		t.Fatal("mutated caller signature acquired original tuple authority")
	}
}

// Fixed-width keys never truncate or zero-pad a malformed supplied signature
// into a previously successful tuple, even when every retained byte matches.
func TestFinalFleetGenerationBindingReuseRejectsSignatureLengthAliases(t *testing.T) {
	t.Parallel()
	source, batch := finalGenerationWorkFixture(t, nil)
	input := finalGenerationBindingCheckInput(t, source, batch.InstalledFleets[0], 1)
	checks := &finalFleetGenerationBindingChecks{}
	if !checks.verify(input.binding, input.clientSignature, input.hotkeySignature, nil) {
		t.Fatal("original tuple failed")
	}
	for _, length := range []int{0, 63, 65} {
		for _, clientSide := range []bool{false, true} {
			client, hotkey := bytes.Clone(input.clientSignature), bytes.Clone(input.hotkeySignature)
			changed := make([]byte, length)
			if clientSide {
				copy(changed, client)
				client = changed
			} else {
				copy(changed, hotkey)
				hotkey = changed
			}
			calls := 0
			if checks.verify(input.binding, client, hotkey, func() { calls++ }) || calls != 0 || len(checks.checked) != 1 {
				t.Fatalf("client=%t signature length=%d aliased an exact tuple", clientSide, length)
			}
		}
	}
}

// Every authenticated journal row has exactly its original position within
// its complete action/stage list, including rows unused by this mutation.
func TestFinalFleetGenerationJournalIndexRetainsAllStagesAndOrder(t *testing.T) {
	t.Parallel()
	source, _ := finalGenerationWorkFixture(t, nil)
	count := 0
	for key, indices := range source.entryIndices {
		var expected []int
		for index, entry := range source.entries {
			if entry.ActionID == key.actionID && entry.Stage == key.stage {
				expected = append(expected, index)
			}
		}
		if !reflect.DeepEqual(indices, expected) {
			t.Fatalf("journal index lost original candidate order for %+v", key)
		}
		count += len(indices)
	}
	if count != len(source.entries) {
		t.Fatalf("journal index retained %d rows, want complete %d", count, len(source.entries))
	}
}

// A successful indexed lookup grants no reuse of a postcondition verdict.
// Later changed bytes fail and restoring the exact source remains admissible.
func TestFinalFleetGenerationJournalIndexRechecksPostconditionBytes(t *testing.T) {
	t.Parallel()
	source, _ := finalGenerationWorkFixture(t, nil)
	actionID, receipt := finalGenerationWorkRefreshMutation(t, source)
	_, entry, _, _, err := source.verifiedMutation(actionID, receipt.TransactionHash, receipt.BlockNumber, receipt.BlockHash, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := entry.PostconditionPath
	prior := bytes.Clone(source.archive.files[path])
	source.archive.files[path] = append(bytes.Clone(prior), ' ')
	if _, _, _, _, err := source.verifiedMutation(actionID, receipt.TransactionHash, receipt.BlockNumber, receipt.BlockHash, nil); err == nil || !strings.Contains(err.Error(), "changed while being indexed") {
		t.Fatalf("indexed mutation retained an earlier byte verdict: %v", err)
	}
	source.archive.files[path] = prior
	if _, _, _, _, err := source.verifiedMutation(actionID, receipt.TransactionHash, receipt.BlockNumber, receipt.BlockHash, nil); err != nil {
		t.Fatalf("restored exact indexed mutation failed: %v", err)
	}
}
