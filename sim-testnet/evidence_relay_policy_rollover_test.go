//go:build linux || darwin

package main

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

func newEvidencePolicyRolloverSourceTest(t *testing.T) (*evidenceRelayRuntime, *evidenceRelayRpcFixture, evidenceRelaySource, evidenceRelayPolicyGapActivation) {
	t.Helper()
	runtime, fixture, previous := newEvidencePolicyGapTest(t, false)
	runtime.horizon = nil
	runtime.executor.plan.ValidatorEvidence = &ValidatorEvidenceDeployment{Address: fixture.expected.Journal, RuntimeCodeHash: common.Hash(fixture.expected.RuntimeHash)}
	activation := previous.Activation
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x91}, 32))
	activation.VPK, activation.FirstSequence, activation.PriorRoot = [32]byte(key[32:]), 1, [32]byte{}
	signed := evidenceRelayPolicyGapActivation{Activation: activation}
	var err error
	signed.VPKSignature, err = activation.SignVPK(key)
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x71})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := activation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	signed.HotkeySignature, err = hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := parsed.Methods["activation"].Outputs.Pack(stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(activation), PublishedBlock: 1201})
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.responses[fixture.expected.Journal.Hex()+":"+hexutil.Encode(stabi.NewSTValidatorEvidence().PackActivation(digest))] = encoded
	fixture.finalizedBlock, fixture.finalizedHash, fixture.historicalPolicyGapReads = 1210, common.Hash{0xa2}, true
	fixture.stateLock.Unlock()
	successor := evidenceRelaySource{validatorId: 1, stateDir: filepath.Join(fixture.stateDir, "new-source"), bounds: runtime.sources[0].bounds, activations: []protocol.ValidatorEvidenceActivation{activation}}
	return runtime, fixture, successor, signed
}

func TestEvidencePolicyRolloverInstallsFinalizedFreshGenerationWithoutContinuity(t *testing.T) {
	runtime, fixture, successor, signed := newEvidencePolicyRolloverSourceTest(t)
	original := runtime.sources[0]
	if err := runtime.installPolicyRolloverSource(t.Context(), 1, successor, []evidenceRelayPolicyGapActivation{signed}); err != nil {
		t.Fatal(err)
	}
	source := &runtime.sources[0]
	if source.stateDir != original.stateDir || !reflect.DeepEqual(source.activations, original.activations) || source.nextEpoch != original.nextEpoch || source.forEpoch(8) != source || source.forEpoch(9).stateDir != successor.stateDir || source.forEpoch(9).activations[0] != signed.Activation {
		t.Fatal("fresh generation replaced old evidence identity or lost its exact cutoff")
	}
	runtime.through[1], runtime.completed[1] = 12, true
	if err := runtime.WaitRange(t.Context(), 9, 12); err == nil {
		t.Fatal("fresh generation counted its partial activation epoch")
	}
	if err := runtime.WaitRange(t.Context(), 10, 12); err != nil {
		t.Fatal("fresh generation refused its full future acceptance interval", err)
	}
	// Caller-owned memory cannot change the installed source or its consent.
	successor.activations[0].VPK[0] ^= 1
	signed.VPKSignature[0] ^= 1
	if source.successor.activations[0].VPK != runtime.policyGapCutoffs[1][0].Activation.VPK || bytes.Equal(signed.VPKSignature, runtime.policyGapCutoffs[1][0].VPKSignature) {
		t.Fatal("installed generation borrowed mutable caller inputs")
	}
	runtime.horizon = &evidenceRelayHorizon{work: evidenceRelayWork{settlementCadence: 100, nativeCadence: 100}, maximum: 100, anchorBlock: 1000, anchorEpoch: 7, anchorNativeEpoch: 1,
		sourceKVs: map[evidenceRelayHorizonSource]protocol.ValidatorEvidenceActivation{{hotkey: original.activations[0].Hotkey, noId: original.activations[0].NoID}: original.activations[0]}, headerKVs: map[[32]byte]protocol.ValidatorEvidenceHeader{}}
	runtime.installPolicyRolloverHorizon(runtime.horizon)
	header := fixture.expected.Evidence.Header
	if err := runtime.horizon.admit(header, 1210); err != nil {
		t.Fatal("successor discarded the original funded history", err)
	}
	header.Domain, _ = source.successor.activations[0].EvidenceDomain()
	header.VPK, header.Epoch, header.BoundaryBlock = source.successor.activations[0].VPK, 9, 1209
	if err := runtime.horizon.admit(header, 1210); err != nil {
		t.Fatal("funded source refused its authenticated successor", err)
	}
	header.Domain, _ = original.activations[0].EvidenceDomain()
	header.VPK = original.activations[0].VPK
	if err := runtime.horizon.admit(header, 1210); err == nil {
		t.Fatal("old generation crossed the successor cutoff")
	}
	if fixture.requestCount("eth_sendRawTransaction") != 0 || len(fixture.manager.journal.Entries()) != 0 {
		t.Fatal("source routing acquired transaction custody")
	}
}

func TestEvidencePolicyRolloverRejectsUnpublishedReusedOrUnfundedSource(t *testing.T) {
	for _, fault := range []string{"unpublished", "reused-path", "foreign-path", "bounds", "unsigned-generation", "invented-prefix"} {
		t.Run(fault, func(t *testing.T) {
			runtime, fixture, successor, signed := newEvidencePolicyRolloverSourceTest(t)
			switch fault {
			case "unpublished":
				digest, _ := signed.Activation.Digest()
				fixture.stateLock.Lock()
				fixture.responses[fixture.expected.Journal.Hex()+":"+hexutil.Encode(stabi.NewSTValidatorEvidence().PackActivation(digest))] = make([]byte, 18*32)
				fixture.stateLock.Unlock()
			case "reused-path":
				successor.stateDir = runtime.sources[0].stateDir
			case "foreign-path":
				successor.stateDir = t.TempDir()
			case "bounds":
				successor.bounds.MaxClosureBytes++
			case "unsigned-generation":
				signed.HotkeySignature[0] ^= 1
			case "invented-prefix":
				signed.Activation.FirstSequence, signed.Activation.PriorRoot = 8, [32]byte{0x34}
				successor.activations[0] = signed.Activation
			}
			if err := runtime.installPolicyRolloverSource(t.Context(), 1, successor, []evidenceRelayPolicyGapActivation{signed}); err == nil || runtime.sources[0].successor != nil || len(runtime.policyGapCutoffs) != 0 {
				t.Fatal("unauthenticated successor changed source routing", err)
			}
		})
	}
}

func TestEvidencePolicyRolloverReadsBothPublicGenerationsWithFreshSignatures(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	runtime := fixture.runtime
	source := &runtime.sources[0]
	old := append([]protocol.ValidatorEvidenceActivation(nil), source.activations...)
	next := *source
	next.stateDir = filepath.Join(runtime.executor.stateDir, "fresh-generation-publications")
	next.activations = append([]protocol.ValidatorEvidenceActivation(nil), source.activations...)
	cutoff := source.activations[0].Domain.Epoch + 2
	keys := map[uint64]ed25519.PrivateKey{}
	for index := range next.activations {
		activation := &next.activations[index]
		keys[activation.NoID] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0xb0 + index)}, 32))
		activation.VPK, activation.FirstSequence, activation.PriorRoot = [32]byte(keys[activation.NoID][32:]), 1, [32]byte{}
		activation.Domain.Epoch, activation.Domain.PolicyHash = cutoff, [32]byte{0xe9}
	}
	source.successor = &next
	fixture.addEpoch(t, cutoff, 810, 1110)
	closed := fixture.publication(t, cutoff, 1109, protocol.ValidatorEvidenceSubject{}, keys).(validatorcomponent.ValidatorEvidencePublicationV2Manifest)
	audit := fixture.publication(t, cutoff, 1130, protocol.ValidatorEvidenceSubject{ObservationEpoch: cutoff + 1, NativeEpoch: 79}, keys).(validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest)
	fixture.stateLock.Lock()
	fixture.finalized = 1150
	fixture.stateLock.Unlock()
	for index := range fixture.closed {
		requests, err := runtime.readClosedPublication(t.Context(), source, &fixture.closed[index], 1150, [32]byte(fixture.finalizedHash))
		if err != nil || len(requests) != len(old) || requests[0].Activation != old[0] {
			t.Fatal("old public generation lost its immutable identity", err)
		}
	}
	requests, err := runtime.readClosedPublication(t.Context(), source, &closed, 1150, [32]byte(fixture.finalizedHash))
	if err != nil || len(requests) != len(next.activations) || requests[0].Activation != next.activations[0] {
		t.Fatal("closed public reader did not verify the fresh generation", err)
	}
	requests, err = runtime.readAuditPublication(t.Context(), source, &audit, 1150, [32]byte(fixture.finalizedHash))
	if err != nil || len(requests) != len(next.activations) || requests[0].Activation != next.activations[0] {
		t.Fatal("audit public reader did not verify the fresh generation", err)
	}
	inventories, err := runtime.evidenceRelayStartupInventories(t.Context(), 100)
	if err != nil || len(inventories[source.validatorId].closed) != 3 || len(inventories[source.validatorId].audits) != 2 {
		t.Fatal("bounded startup inventory omitted a generation", err)
	}
	closedManifests, audits, err := runtime.readEvidenceRelayStartupManifests(t.Context(), source, inventories[source.validatorId])
	if err != nil || len(closedManifests) != 3 || len(audits) != 2 {
		t.Fatal("bounded startup reader omitted a generation", err)
	}
	if session, err := runtime.newEvidenceRelayStartupSession(t.Context(), runtime.executor.journal.Entries(), inventories); err != nil || session != nil {
		t.Fatal("single-generation receipt cache authorized a successor", err)
	}
	if session, err := runtime.newEvidenceRelayColdCensusSession(t.Context()); err != nil || session != nil {
		t.Fatal("single-generation census cache authorized a successor", err)
	}
	// Moving a valid signed new manifest into the old namespace cannot change
	// generation ownership, even when all payload bytes remain authentic.
	newPath, _ := validatorcomponent.ValidatorEvidencePublicationV2ManifestPath(next.stateDir, cutoff)
	oldPath, _ := validatorcomponent.ValidatorEvidencePublicationV2ManifestPath(source.stateDir, cutoff)
	raw, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(oldPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverEvidenceRelayClosedGenerations(t.Context(), source); err == nil {
		t.Fatal("foreign source namespace admitted authentic successor bytes")
	}
}
