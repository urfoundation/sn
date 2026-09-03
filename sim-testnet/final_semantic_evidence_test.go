package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/ss58"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

type finalAttemptFixtureToken struct {
	clientID connect.Id
	binding  validatorpkg.AttemptBinding
	egress   [32]byte
}

const (
	finalAttemptSettlementSchema       = "urnetwork-validator-settlement-transition-v1"
	finalAttemptSettlementDigestDomain = "urnetwork/validator/settlement-transition/digest/v1\x00"
	finalAttemptSettlementSignDomain   = "urnetwork/validator/settlement-transition/sign/v1\x00"
)

// These local wire projections deliberately reproduce the public settlement
// protocol instead of reaching into validator internals. The fixture thereby
// checks that an independent consumer can reconstruct both the content digest
// and signature message accepted by the production verifier.
type finalAttemptSettlementCore struct {
	Schema       string                                  `json:"schema"`
	Identity     validatorpkg.AttemptLedgerIdentity      `json:"identity"`
	FromBoundary validatorpkg.AttemptBoundary            `json:"from_boundary"`
	ToEpoch      uint64                                  `json:"to_epoch"`
	PreFold      validatorpkg.ReleaseStatsMeasurement    `json:"pre_fold"`
	PostFold     []validatorpkg.AttemptSettlementQuality `json:"post_fold"`
}

type finalAttemptSettlementPayload struct {
	Core  finalAttemptSettlementCore             `json:"core"`
	Batch []validatorpkg.AttemptSettlementMember `json:"batch"`
}

func finalAttemptSettlementCoreFrom(transition *validatorpkg.AttemptSettlementTransition) finalAttemptSettlementCore {
	return finalAttemptSettlementCore{
		Schema: transition.Schema, Identity: transition.Identity,
		FromBoundary: transition.FromBoundary, ToEpoch: transition.ToEpoch,
		PreFold: transition.PreFold, PostFold: transition.PostFold,
	}
}

func finalAttemptSettlementDigest(t *testing.T, transition *validatorpkg.AttemptSettlementTransition) string {
	t.Helper()
	encoded, err := json.Marshal(finalAttemptSettlementCoreFrom(transition))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(finalAttemptSettlementDigestDomain))
	_, _ = digest.Write(encoded)
	return "0x" + hex.EncodeToString(digest.Sum(nil))
}

func finalAttemptSettlementMessage(t *testing.T, transition *validatorpkg.AttemptSettlementTransition) []byte {
	t.Helper()
	encoded, err := json.Marshal(finalAttemptSettlementPayload{Core: finalAttemptSettlementCoreFrom(transition), Batch: transition.Batch})
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(finalAttemptSettlementSignDomain), encoded...)
}

func finalAttemptFixtureID(value uint64) connect.Id {
	var id connect.Id
	id[0] = 0xa7
	binary.BigEndian.PutUint64(id[len(id)-8:], value)
	return id
}

// attachFinalAttemptCuts mirrors the production append order: every server-
// signed pending assignment is durable before the validator-signed terminal
// record, and the signed cut is the sole authority for counters and proofs.
func attachFinalAttemptCuts(t *testing.T, artifact *validatorpkg.ReleaseMeasurementArtifact, validatorKey ed25519.PrivateKey, serverKeys []ed25519.PrivateKey, root string, previous *validatorpkg.ReleaseMeasurementArtifact) {
	t.Helper()
	vpk := validatorKey.Public().(ed25519.PublicKey)
	bindingByNO := map[uint64]map[connect.Id]validatorpkg.AttemptBinding{}
	for _, binding := range artifact.Bindings {
		clientID, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		if bindingByNO[binding.NoID] == nil {
			bindingByNO[binding.NoID] = map[connect.Id]validatorpkg.AttemptBinding{}
		}
		attemptBinding := validatorpkg.AttemptBinding{ClientID: clientID, FleetID: "0x" + strings.Repeat("0", 64), Hotkey: "0x" + strings.Repeat("0", 64)}
		if binding.Active {
			attemptBinding.Active = true
			attemptBinding.FleetID = binding.FleetID
			attemptBinding.Hotkey = binding.Hotkey
			attemptBinding.Generation = binding.Generation
			attemptBinding.UIDFound = binding.LiveUIDFound
			attemptBinding.UID = binding.LiveUID
		}
		bindingByNO[binding.NoID][clientID] = attemptBinding
	}
	previousByNO := map[uint64]validatorpkg.ReleaseMeasurementInput{}
	if previous != nil {
		for _, input := range previous.Inputs {
			previousByNO[input.NoID] = input
		}
	}
	transitions := make([]*validatorpkg.AttemptSettlementTransition, 0, len(artifact.Inputs))
	for inputIndex := range artifact.Inputs {
		input := &artifact.Inputs[inputIndex]
		if input.NoID == 0 || input.NoID > uint64(len(serverKeys)) {
			t.Fatalf("invalid fixture operator %d", input.NoID)
		}
		serverKey := serverKeys[input.NoID-1]
		ledger, err := validatorpkg.NewAttemptLedger(filepath.Join(root, fmt.Sprintf("no-%d", input.NoID)), validatorpkg.AttemptLedgerIdentity{
			DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID, GenesisHash: artifact.GenesisHash,
			Netuid: artifact.Netuid, ValidatorID: artifact.ValidatorID, ValidatorUID: artifact.SelfUID, NoID: input.NoID,
		}, validatorKey)
		if err != nil {
			t.Fatal(err)
		}
		boundary := validatorpkg.AttemptBoundary{SettlementEpoch: input.SettlementEpoch, EVMBlock: input.CutEVMSnapshotBlock, EVMBlockHash: input.CutEVMSnapshotHash}
		tokens := make([]finalAttemptFixtureToken, 0)
		for providerIndex := range input.Stats.Providers {
			provider := &input.Stats.Providers[providerIndex]
			if len(provider.EgressIPHashHexes) == 0 {
				continue
			}
			clientID, err := connect.ParseId(provider.ClientID)
			if err != nil {
				t.Fatal(err)
			}
			egressBytes, err := hex.DecodeString(strings.TrimPrefix(provider.EgressIPHashHexes[0], "0x"))
			if err != nil || len(egressBytes) != 32 {
				t.Fatalf("invalid fixture egress for %s", provider.ClientID)
			}
			var egress [32]byte
			copy(egress[:], egressBytes)
			provider.Assignments, provider.Confirmations = 1, 1
			provider.LatencyBuckets[0] = 1
			tokens = append(tokens, finalAttemptFixtureToken{clientID: clientID, binding: bindingByNO[input.NoID][clientID], egress: egress})
		}
		sequence := uint64(0)
		for len(tokens) != 0 {
			groupSize := len(tokens)
			if groupSize > connect.VerifyMMax-1 {
				groupSize = connect.VerifyMMax - 1
				if remainder := len(tokens) - groupSize; remainder > 0 && remainder < connect.VerifyMMin-1 {
					groupSize -= connect.VerifyMMin - 1 - remainder
				}
			}
			if groupSize < connect.VerifyMMin-1 {
				t.Fatalf("fixture attempt group has only %d providers", groupSize)
			}
			group := tokens[:groupSize]
			tokens = tokens[groupSize:]
			sequence++
			trailID := finalAttemptFixtureID(artifact.ValidatorID*100_000_000 + artifact.SettlementEpoch*1_000_000 + input.NoID*10_000 + sequence)
			seedID := finalAttemptFixtureID(9_000_000_000 + artifact.ValidatorID*100_000_000 + artifact.SettlementEpoch*1_000_000 + input.NoID*10_000 + sequence)
			nonce := make([]byte, connect.VerifyNonceSize)
			binary.BigEndian.PutUint64(nonce[len(nonce)-8:], artifact.ValidatorID*100_000_000+artifact.SettlementEpoch*1_000_000+input.NoID*10_000+sequence)
			m := len(group) + 1
			trail := []connect.Id{seedID}
			hops := []connect.VerifyProofHop{{ClientId: seedID, TimeMs: sequence * 1000}}
			assignments := make([]validatorpkg.AttemptAssignment, 0, len(group))
			for tokenIndex, token := range group {
				walked := append(append([]connect.Id(nil), trail...), token.clientID)
				message, err := connect.BuildVerifyAssignMessage(1, trailID, nonce, vpk, byte(m), walked)
				if err != nil {
					t.Fatal(err)
				}
				assignments = append(assignments, validatorpkg.AttemptAssignment{
					Trail: append([]connect.Id(nil), trail...), NextHop: token.clientID, ServerKeyID: 1,
					AssignMessage: message, AssignSignature: ed25519.Sign(serverKey, message), Confirmed: true, HasLatency: true, Binding: token.binding,
				})
				trail = walked
				hops = append(hops, connect.VerifyProofHop{ClientId: token.clientID, TimeMs: sequence*1000 + uint64(tokenIndex+1), EgressIpHash: token.egress})
			}
			finalMessage, err := connect.BuildVerifyFinalMessage(1, trailID, nonce, vpk, byte(m), hops)
			if err != nil {
				t.Fatal(err)
			}
			extendMessage, err := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(m), trail)
			if err != nil {
				t.Fatal(err)
			}
			digest := connect.VerifyFinalDigest(finalMessage)
			pathID := validatorpkg.TrailPathId(trailID, vpk, 1)
			proof := &validatorpkg.ProofRecord{
				Version: 1, Epoch: input.SettlementEpoch, TrailId: trailID, ServerNonce: nonce, Vpk: append([]byte(nil), vpk...), M: m, Hops: hops,
				ServerKeyId: 1, FinalSig: ed25519.Sign(serverKey, finalMessage), VerifierSig: ed25519.Sign(validatorKey, extendMessage),
				FinalDigest: digest[:], VpkSig: ed25519.Sign(validatorKey, finalMessage), Coverage: uint64(m - 1), PathId: pathID[:], CompleteTimeMs: hops[len(hops)-1].TimeMs,
			}
			for assignmentIndex := range assignments {
				checkpoint := append([]validatorpkg.AttemptAssignment(nil), assignments[:assignmentIndex+1]...)
				last := len(checkpoint) - 1
				checkpoint[last].Confirmed, checkpoint[last].HasLatency, checkpoint[last].LatencyBucket = false, false, 0
				if _, err := ledger.Append(validatorpkg.AttemptRecord{Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: m, Assignments: checkpoint, Disposition: validatorpkg.AttemptDispositionPending}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ledger.Append(validatorpkg.AttemptRecord{Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: m, Assignments: assignments, Disposition: validatorpkg.AttemptDispositionComplete, Proof: proof}); err != nil {
				t.Fatal(err)
			}
		}
		firstSequence := uint64(1)
		if prior, exists := previousByNO[input.NoID]; exists && artifact.SettlementEpoch != previous.SettlementEpoch {
			if prior.Stats.AttemptCut == nil {
				t.Fatalf("prior fixture operator %d has no attempt cut", input.NoID)
			}
			firstSequence = prior.Stats.AttemptCut.LastSequence + 1
		}
		cut, err := ledger.BuildCut(boundary, firstSequence, firstSequence)
		if err != nil {
			t.Fatal(err)
		}
		if err := validatorpkg.VerifyAttemptLedgerCut(cut, vpk, map[byte]ed25519.PublicKey{1: serverKey.Public().(ed25519.PublicKey)}); err != nil {
			t.Fatal(err)
		}
		input.Stats.AttemptCut = cut
		prior, hasPrior := previousByNO[input.NoID]
		if !hasPrior || artifact.SettlementEpoch == previous.SettlementEpoch {
			continue
		}
		if artifact.SettlementEpoch != previous.SettlementEpoch+1 {
			t.Fatalf("fixture settlement epoch jumped from %d to %d", previous.SettlementEpoch, artifact.SettlementEpoch)
		}
		preFold := prior.Stats
		preFold.SettlementTransition = nil
		verified, err := validatorpkg.VerifyReleaseStatsMeasurement(preFold)
		if err != nil {
			t.Fatalf("prior fixture operator %d statistics: %v", input.NoID, err)
		}
		postFold := make([]validatorpkg.AttemptSettlementQuality, 0, len(verified.Providers))
		priorQuality := make(map[string]validatorpkg.AttemptSettlementQuality, len(verified.Providers))
		for clientID, provider := range verified.Providers {
			if !provider.HasQuality {
				continue
			}
			quality := validatorpkg.AttemptSettlementQuality{ClientID: clientID.String(), HasQuality: true, QualityPPM: provider.QualityPPM}
			postFold = append(postFold, quality)
			priorQuality[quality.ClientID] = quality
		}
		sort.Slice(postFold, func(i, j int) bool { return postFold[i].ClientID < postFold[j].ClientID })
		for providerIndex := range input.Stats.Providers {
			provider := &input.Stats.Providers[providerIndex]
			quality, exists := priorQuality[provider.ClientID]
			provider.HasPriorQuality = exists
			provider.PriorQualityPPM = quality.QualityPPM
		}
		transition := &validatorpkg.AttemptSettlementTransition{
			Schema: finalAttemptSettlementSchema, Identity: cut.Identity,
			FromBoundary: prior.Stats.AttemptCut.Boundary, ToEpoch: artifact.SettlementEpoch,
			PreFold: preFold, PostFold: postFold,
		}
		input.Stats.SettlementTransition = transition
		transitions = append(transitions, transition)
	}
	if len(transitions) == 0 {
		return
	}
	sort.Slice(transitions, func(i, j int) bool { return transitions[i].Identity.NoID < transitions[j].Identity.NoID })
	batch := make([]validatorpkg.AttemptSettlementMember, len(transitions))
	for index, transition := range transitions {
		batch[index] = validatorpkg.AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: finalAttemptSettlementDigest(t, transition)}
	}
	for _, transition := range transitions {
		transition.Batch = append([]validatorpkg.AttemptSettlementMember(nil), batch...)
		transition.Signature = ed25519.Sign(validatorKey, finalAttemptSettlementMessage(t, transition))
	}
	if err := validatorpkg.VerifyAttemptSettlementBatch(transitions); err != nil {
		t.Fatalf("fixture settlement transition batch: %v", err)
	}
}

func TestFinalSemanticEvidenceBuildRenderAndArtifacts(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, artifacts := finalSemanticFixture(t)
	firstDraft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	if markdown, err := RenderFinalSemanticEvidenceMarkdown(firstDraft); err == nil || len(markdown) != 0 || !strings.Contains(err.Error(), "public archive-RPC verification is missing") {
		t.Fatalf("unsealed FINAL.md was not refused: bytes=%d err=%v", len(markdown), err)
	}
	first, err := SealFinalSemanticEvidenceOnChain(context.Background(), firstDraft, &finalTestChainReader{evidence: firstDraft})
	if err != nil {
		t.Fatal(err)
	}
	secondDraft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SealFinalSemanticEvidenceOnChain(context.Background(), secondDraft, &finalTestChainReader{evidence: secondDraft})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("semantic evidence generation is not deterministic")
	}
	markdown, err := RenderFinalSemanticEvidenceMarkdown(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{first.EvidenceHash, "202 positive candidates", "two zero-weight rejects", "funded 10000", "Validator 1 → NO 1"} {
		if !bytes.Contains(markdown, []byte(want)) {
			t.Fatalf("FINAL.md semantic section does not contain %q", want)
		}
	}
	for _, origin := range first.PublicVerification.OperatorEvidenceOrigins {
		for _, command := range []string{"inspect", "analyze"} {
			want := fmt.Sprintf("go run ./sim-testnet %s --config sim-testnet/testnet.yml --manifest %s --format json", command, origin.ManifestURI)
			if !bytes.Contains(markdown, []byte(want)) {
				t.Fatalf("FINAL.md does not emit operator %d %s command", origin.OperatorNoID, command)
			}
		}
	}
	load := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, ok := artifacts[locator.URI]
		if !ok {
			return nil, fmt.Errorf("missing fixture artifact %s", locator.URI)
		}
		return append([]byte(nil), data...), nil
	}
	if err := VerifyFinalSemanticArtifacts(context.Background(), first, load); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), first, &finalTestChainReader{evidence: first}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), first, &finalTestChainReader{evidence: first, failCanonical: true}); err == nil || !strings.Contains(err.Error(), "archive unavailable") {
		t.Fatalf("public archive absence was not fatal: %v", err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), first, &finalTestChainReader{evidence: first, corruptWeights: true}); err == nil || !strings.Contains(err.Error(), "applied vector") {
		t.Fatalf("public applied-weight mismatch was not fatal: %v", err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), first, &finalTestChainReader{evidence: first, corruptCustody: true}); err == nil || !strings.Contains(err.Error(), "custody/policy state mismatch") {
		t.Fatalf("public custody substitution was not fatal: %v", err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), first, &finalTestChainReader{evidence: first, corruptSettlement: true}); err == nil || !strings.Contains(err.Error(), "settlement-vault accounting mismatch") {
		t.Fatalf("public settlement-vault substitution was not fatal: %v", err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), first, &finalTestChainReader{evidence: first, corruptOwnerStake: true}); err == nil || !strings.Contains(err.Error(), "owner-pair stake mismatch") {
		t.Fatalf("public owner-pair stake substitution was not fatal: %v", err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), first, &finalTestChainReader{evidence: first, corruptReserveReceipt: true}); err == nil || !strings.Contains(err.Error(), "ReservePrincipalAdded receipt") {
		t.Fatalf("public ReservePrincipalAdded receipt substitution was not fatal: %v", err)
	}

	// Reusing one URI with a different declared hash must not exploit the
	// loader cache. Every use is checked against its own immutable locator.
	tampered := finalSemanticClone(t, first)
	tampered.Pools[0].OwnershipArtifact.URI = tampered.Pools[0].Registration.Proof.URI
	resignFinalSemantic(t, tampered)
	if err := VerifyFinalSemanticArtifacts(context.Background(), tampered, load); err == nil || !strings.Contains(err.Error(), "size or content hash mismatch") {
		t.Fatalf("shared-URI hash substitution was not rejected: %v", err)
	}

	tamperedBytes := make(map[string][]byte, len(artifacts))
	for name, data := range artifacts {
		tamperedBytes[name] = append([]byte(nil), data...)
	}
	tamperedBytes[first.Validators[0].Cycles[0].IntentArtifact.URI][0] ^= 0xff
	if err := VerifyFinalSemanticArtifacts(context.Background(), first, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		return tamperedBytes[locator.URI], nil
	}); err == nil || !strings.Contains(err.Error(), "size or content hash mismatch") {
		t.Fatalf("tampered artifact was not rejected: %v", err)
	}
}

func TestFinalSemanticDerivedTransitionsRejectIndependentMutations(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, baselineArtifacts := finalSemanticFixture(t)
	if err := verifyFinalHeadTournamentTransitionArtifacts(&source, baselineArtifacts); err != nil {
		t.Fatalf("baseline tournament artifacts: %v", err)
	}
	if err := verifyFinalValidatorViewTransitionArtifact(&source, baselineArtifacts[source.ValidatorView.Artifact.URI]); err != nil {
		t.Fatalf("baseline validator view artifact: %v", err)
	}
	type mutation struct {
		name string
		edit func(*FinalSemanticEvidence, map[string][]byte)
	}
	mutations := []mutation{
		{name: "declaration", edit: func(evidence *FinalSemanticEvidence, _ map[string][]byte) {
			evidence.ValidatorView.WithheldFleetID = 199
		}},
		{name: "artifact", edit: func(evidence *FinalSemanticEvidence, artifacts map[string][]byte) {
			value := finalValidatorViewTransitionArtifact{FaultEpoch: 10, RestoredEpoch: 11, AffectedValidatorID: 1, ControlValidatorID: 2, WithheldFleetID: 199, ReplacementFleetID: 5}
			replaceFinalSemanticFixtureArtifact(t, &evidence.ValidatorView.Artifact, artifacts, value)
		}},
		{name: "cycle", edit: func(evidence *FinalSemanticEvidence, _ map[string][]byte) {
			for index := range evidence.Validators[0].Cycles[0].Candidates {
				candidate := &evidence.Validators[0].Cycles[0].Candidates[index]
				if candidate.FleetID == 201 {
					candidate.UID++
					return
				}
			}
			t.Fatal("fixture cycle has no challenger fleet 201")
		}},
		{name: "v4 field", edit: func(evidence *FinalSemanticEvidence, artifacts map[string][]byte) {
			transition := &evidence.HeadTransitions[0]
			var value finalHeadTournamentTransitionArtifact
			if err := json.Unmarshal(artifacts[transition.Artifact.URI], &value); err != nil {
				t.Fatal(err)
			}
			value.Postcondition.IndependentEVMHashDomain = "ethereum"
			replaceFinalSemanticFixtureArtifact(t, &transition.Artifact, artifacts, value)
		}},
		{name: "pruned identity", edit: func(evidence *FinalSemanticEvidence, artifacts map[string][]byte) {
			transition := &evidence.HeadTransitions[0]
			var value finalHeadTournamentTransitionArtifact
			if err := json.Unmarshal(artifacts[transition.Artifact.URI], &value); err != nil {
				t.Fatal(err)
			}
			value.Pruned.SS58 = evidence.HeadFleets[0].Hotkey
			replaceFinalSemanticFixtureArtifact(t, &transition.Artifact, artifacts, value)
		}},
		{name: "signed manifest identity", edit: func(evidence *FinalSemanticEvidence, artifacts map[string][]byte) {
			fleet := &evidence.HeadFleets[evidence.HeadTransitions[0].ChallengerFleetID-1]
			var wrapper struct {
				Manifest json.RawMessage `json:"manifest"`
				UID      uint16          `json:"uid"`
				Snapshot ChainHead       `json:"snapshot"`
			}
			if err := json.Unmarshal(artifacts[fleet.BindingArtifact.URI], &wrapper); err != nil {
				t.Fatal(err)
			}
			manifest, err := protocol.ParseFleetManifest(wrapper.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			manifest.FleetID[0] ^= 0xff
			wrapper.Manifest, err = manifest.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			replaceFinalSemanticFixtureArtifact(t, &fleet.BindingArtifact, artifacts, wrapper)
		}},
		{name: "restoration", edit: func(evidence *FinalSemanticEvidence, artifacts map[string][]byte) {
			evidence.ValidatorView.RestoredEpoch = 12
			value := finalValidatorViewTransitionArtifact{FaultEpoch: 10, RestoredEpoch: 12, AffectedValidatorID: 1, ControlValidatorID: 2, WithheldFleetID: 200, ReplacementFleetID: 5}
			replaceFinalSemanticFixtureArtifact(t, &evidence.ValidatorView.Artifact, artifacts, value)
		}},
	}
	for _, mutation := range mutations {
		candidate := finalSemanticClone(t, &source)
		artifacts := make(map[string][]byte, len(baselineArtifacts))
		for uri, data := range baselineArtifacts {
			artifacts[uri] = append([]byte(nil), data...)
		}
		mutation.edit(candidate, artifacts)
		verificationErr := verifyFinalHeadTournamentTransitionArtifacts(candidate, artifacts)
		if verificationErr == nil {
			verificationErr = verifyFinalValidatorViewTransitionArtifact(candidate, artifacts[candidate.ValidatorView.Artifact.URI])
		}
		if verificationErr == nil {
			t.Fatalf("%s mutation was accepted", mutation.name)
		}
	}
}

func replaceFinalSemanticFixtureArtifact(t *testing.T, locator *FinalArtifactLocator, artifacts map[string][]byte, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	artifacts[locator.URI] = data
	locator.ContentHash = bytesSHA256(data)
	locator.SizeBytes = uint64(len(data))
}

func TestFinalPublicChainVerificationRequiresTwoCanonicalOperatorOrigins(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, _ := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
	if err != nil {
		t.Fatal(err)
	}
	baseline := sealed.PublicVerification
	if baseline == nil {
		t.Fatal("sealed fixture has no public verification")
	}
	mutations := []struct {
		name string
		edit func(*FinalPublicChainVerification)
	}{
		{name: "one origin", edit: func(value *FinalPublicChainVerification) {
			value.OperatorEvidenceOrigins = value.OperatorEvidenceOrigins[:1]
		}},
		{name: "duplicate origin", edit: func(value *FinalPublicChainVerification) {
			value.OperatorEvidenceOrigins[1].ManifestURI = value.OperatorEvidenceOrigins[0].ManifestURI
		}},
		{name: "reordered origins", edit: func(value *FinalPublicChainVerification) {
			value.OperatorEvidenceOrigins[0], value.OperatorEvidenceOrigins[1] = value.OperatorEvidenceOrigins[1], value.OperatorEvidenceOrigins[0]
		}},
		{name: "nonmember primary", edit: func(value *FinalPublicChainVerification) {
			value.EvidenceURI = "https://third.example/deployment.json?hash=sha256:" + strings.Repeat("33", 32)
		}},
	}
	for _, mutation := range mutations {
		encoded, err := json.Marshal(baseline)
		if err != nil {
			t.Fatal(err)
		}
		var candidate FinalPublicChainVerification
		if err := json.Unmarshal(encoded, &candidate); err != nil {
			t.Fatal(err)
		}
		mutation.edit(&candidate)
		if err := finalizePublicChainVerification(&candidate, sealed.ChainID, sealed.GenesisHash); err == nil {
			t.Fatalf("%s mutation was accepted", mutation.name)
		}
	}
}

func TestFinalSemanticArtifactVerificationCacheBindsExactBytesAndIsConcurrent(t *testing.T) {
	evidence := &FinalSemanticEvidence{EvidenceHash: finalTestHex(0xa1)}
	firstGraph := map[string][]byte{"artifact/a.json": []byte("{\"value\":1}\n")}
	first, err := finalSemanticArtifactVerificationCacheKey(evidence, firstGraph)
	if err != nil {
		t.Fatal(err)
	}
	secondGraph := map[string][]byte{"artifact/a.json": append([]byte(nil), firstGraph["artifact/a.json"]...)}
	secondGraph["artifact/a.json"][9] = '2'
	second, err := finalSemanticArtifactVerificationCacheKey(evidence, secondGraph)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("artifact cache key ignored an exact-byte replacement")
	}
	otherEvidence := *evidence
	otherEvidence.EvidenceHash = finalTestHex(0xa2)
	third, err := finalSemanticArtifactVerificationCacheKey(&otherEvidence, firstGraph)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("artifact cache key ignored the signed semantic object identity")
	}

	finalSemanticArtifactVerificationCacheStore(first)
	if !finalSemanticArtifactVerificationCacheHit(first) || finalSemanticArtifactVerificationCacheHit(second) || finalSemanticArtifactVerificationCacheHit(third) {
		t.Fatal("artifact verification cache accepted an unverified byte or evidence identity")
	}
	done := make(chan bool, 64)
	for index := 0; index < cap(done); index++ {
		go func() {
			finalSemanticArtifactVerificationCacheStore(first)
			done <- finalSemanticArtifactVerificationCacheHit(first)
		}()
	}
	for index := 0; index < cap(done); index++ {
		if !<-done {
			t.Fatal("concurrent artifact verification cache lost a verified identity")
		}
	}
}

func TestFinalExitReceiptsUseAdversarialCampaignBoundary(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, _ := finalSemanticFixture(t)
	for index := range source.ExitCriteria {
		criterion := &source.ExitCriteria[index]
		if criterion.ID != "invalid-merkle-proof-rejected" {
			continue
		}
		// Preparation completes before the complete-epoch baseline. These
		// transactions are nevertheless inside the continuously running
		// adversarial campaign and must remain admissible evidence.
		criterion.EVMReceipts[0].Block = ChainHead{Number: 50, Hash: finalTestHex(50)}
	}
	if _, err := BuildFinalSemanticEvidence(source); err != nil {
		t.Fatalf("pre-baseline campaign receipts rejected: %v", err)
	}
	for index := range source.ExitCriteria {
		criterion := &source.ExitCriteria[index]
		if criterion.ID == "invalid-merkle-proof-rejected" {
			criterion.EVMReceipts[0].Block = ChainHead{Number: source.EVMCampaignStartHead.Number - 1, Hash: finalTestHex(3)}
		}
	}
	if _, err := BuildFinalSemanticEvidence(source); err == nil || !strings.Contains(err.Error(), "outside [4,1750]") {
		t.Fatalf("pre-campaign receipt accepted: %v", err)
	}
}

func TestFinalSemanticArtifactsRejectSupervisorRestartOutsideFaultCensus(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, artifacts := finalSemanticFixture(t)
	locator := &source.ContractCleanup.SupervisorStateArtifact
	var state SupervisorState
	if err := json.Unmarshal(artifacts[locator.URI], &state); err != nil {
		t.Fatal(err)
	}
	state.Processes[0].Restarts++
	wire, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	artifacts[locator.URI] = wire
	locator.ContentHash = bytesSHA256(wire)
	locator.SizeBytes = uint64(len(wire))
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFinalSemanticArtifacts(context.Background(), draft, func(_ context.Context, artifact FinalArtifactLocator) ([]byte, error) {
		wire, ok := artifacts[artifact.URI]
		if !ok {
			return nil, fmt.Errorf("missing fixture artifact %s", artifact.URI)
		}
		return append([]byte(nil), wire...), nil
	}); err == nil || !strings.Contains(err.Error(), "health/restarts differ from the fault-attributed census") {
		t.Fatalf("unplanned terminal restart error=%v", err)
	}
}

func TestFinalSemanticArtifactsRejectDifferentArchiveRetentionReceipt(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, artifacts := finalSemanticFixture(t)
	locator := &source.ArchiveRetention.Artifact
	var receipt FinalArchiveRetentionPreflight
	if err := json.Unmarshal(artifacts[locator.URI], &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.PlannedSpanBlocks++
	receipt.RequiredDepthBlocks++
	receipt.Substrate.HistoricalHead.Number--
	receipt.Substrate.RequiredDepthBlocks++
	receipt.EVM.HistoricalHead.Number--
	receipt.EVM.RequiredDepthBlocks++
	var err error
	receipt.EvidenceHash, err = finalArchiveRetentionPreflightHash(&receipt)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(&receipt)
	if err != nil {
		t.Fatal(err)
	}
	artifacts[locator.URI] = wire
	locator.ContentHash = bytesSHA256(wire)
	locator.SizeBytes = uint64(len(wire))
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFinalSemanticArtifacts(context.Background(), draft, func(_ context.Context, artifact FinalArtifactLocator) ([]byte, error) {
		wire, ok := artifacts[artifact.URI]
		if !ok {
			return nil, fmt.Errorf("missing fixture artifact %s", artifact.URI)
		}
		return append([]byte(nil), wire...), nil
	}); err == nil || !strings.Contains(err.Error(), "differs from semantic declaration") {
		t.Fatalf("different archive-retention receipt error=%v", err)
	}
}

func TestFinalSemanticReplayIgnoresAdvancingFinalizedTipsButRejectsFork(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, _ := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	reader := &finalTestChainReader{evidence: draft, nativeTip: draft.NativeTerminalHead.Number, evmTip: draft.EVMTerminalHead.Number}
	sealed, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, reader)
	if err != nil {
		t.Fatal(err)
	}
	reader.evidence = sealed
	reader.nativeTip += 500
	reader.evmTip += 500
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), sealed, reader); err != nil {
		t.Fatalf("advancing finalized tips changed immutable replay: %v", err)
	}
	reader.forkTarget = true
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), sealed, reader); err == nil || !strings.Contains(err.Error(), "canonical target mismatch") {
		t.Fatalf("forked target was not rejected: %v", err)
	}
}

func TestFinalSemanticEvidenceFailsClosed(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, _ := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*FinalSemanticEvidence)
		want string
	}{
		{name: "candidate census", edit: func(e *FinalSemanticEvidence) {
			e.Validators[0].Cycles[0].Candidates = e.Validators[0].Cycles[0].Candidates[:201]
		}, want: "candidate evidence=201"},
		{name: "zero weight reject", edit: func(e *FinalSemanticEvidence) { e.Validators[0].Cycles[0].Candidates[201].AppliedWeight = 1 }, want: "zero-weight reject"},
		{name: "validator self mask", edit: func(e *FinalSemanticEvidence) { e.Validators[0].Cycles[0].MaskedUIDs = nil }, want: "self UID"},
		{name: "rank boundary", edit: func(e *FinalSemanticEvidence) {
			e.Validators[0].Cycles[0].Candidates[199], e.Validators[0].Cycles[0].Candidates[200] = e.Validators[0].Cycles[0].Candidates[200], e.Validators[0].Cycles[0].Candidates[199]
		}, want: "candidate rank"},
		{name: "deposit formula", edit: func(e *FinalSemanticEvidence) { e.Validators[0].Cycles[0].Pools[0].RequiredDepositRao = "99" }, want: "floor-and-cap"},
		{name: "quality formula", edit: func(e *FinalSemanticEvidence) {
			e.Validators[0].Cycles[0].Pools[0].QualityFactor = FinalRational{Numerator: "2", Denominator: "3"}
		}, want: "quality factor"},
		{name: "realized theta", edit: func(e *FinalSemanticEvidence) { e.Validators[0].Cycles[0].RealizedHeadValue++ }, want: "realized theta"},
		{name: "topology census", edit: func(e *FinalSemanticEvidence) { e.Topology.MinerSDKInstances = 999 }, want: "want 1000/20/202/200/2/2"},
		{name: "unplanned process restart", edit: func(e *FinalSemanticEvidence) { e.Topology.ProcessRestarts[0].ObservedRestarts++ }, want: "restart count expected/observed/faults"},
		{name: "duplicate restart fault attribution", edit: func(e *FinalSemanticEvidence) {
			e.Topology.ProcessRestarts[1].FaultIDs[0] = e.Topology.ProcessRestarts[0].FaultIDs[0]
		}, want: "restart fault identities are not canonical"},
		{name: "short archive retention span", edit: func(e *FinalSemanticEvidence) { e.ArchiveRetention.PlannedSpanBlocks-- }, want: "full campaign and peer-review margin"},
		{name: "stale archive preflight", edit: func(e *FinalSemanticEvidence) {
			e.ArchiveRetention.GeneratedAt = time.Unix(1_699_900_000, 0).UTC().Format(time.RFC3339Nano)
		}, want: "preflight is stale"},
		{name: "cleanup cutoff binding", edit: func(e *FinalSemanticEvidence) { e.ContractCleanup.CutoffUnixNano++ }, want: "canonical UTC instant"},
		{name: "cleanup failed invocation", edit: func(e *FinalSemanticEvidence) { e.ContractCleanup.FailedInvocations = 1 }, want: "pre-start contract cleanup is incomplete"},
		{name: "implementation slot", edit: func(e *FinalSemanticEvidence) { e.Deployment.ObservedImplementationSlot = finalTestHex(7) }, want: "implementation slot does not point"},
		{name: "active guardian substitution", edit: func(e *FinalSemanticEvidence) {
			e.Deployment.CoordinatorActiveGuardian = "0x8888888888888888888888888888888888888888"
		}, want: "guardian/oracle activation"},
		{name: "signed plan minimum transfer", edit: func(e *FinalSemanticEvidence) { e.Deployment.VaultMinimumTransferTaoRao++ }, want: "signed plan"},
		{name: "settlement global equation", edit: func(e *FinalSemanticEvidence) { e.SettlementAccounting.After.OutstandingLiabilityRao = "49" }, want: "escrowAccounted != pendingFunding + outstandingLiability"},
		{name: "settlement event reconciliation", edit: func(e *FinalSemanticEvidence) { e.SettlementAccounting.EmissionCapturedEventRao = "9999" }, want: "EmissionCaptured/ClaimPaid event sums"},
		{name: "operator substituted for vault custody", edit: func(e *FinalSemanticEvidence) { e.Pools[0].Coldkey = e.Pools[0].OperatorColdkey }, want: "not owned by the immutable settlement-vault coldkey"},
		{name: "vault substituted for operator registry identity", edit: func(e *FinalSemanticEvidence) { e.Pools[0].OperatorColdkey = e.Pools[0].Coldkey }, want: "does not separate operator registry identity from vault custody"},
		{name: "reserve underbacked", edit: func(e *FinalSemanticEvidence) { e.Reserve.LiveStakeAfterRao = "119" }, want: "one-way backing"},
		{name: "reserve baseline lacks emission growth", edit: func(e *FinalSemanticEvidence) { e.Reserve.LiveStakeBeforeRao = e.Reserve.PrincipalBeforeRao }, want: "above principal at both checkpoints"},
		{name: "reserve no auto-compounding", edit: func(e *FinalSemanticEvidence) { e.Reserve.LiveStakeAfterRao = e.Reserve.PrincipalAfterRao }, want: "does not prove native emission auto-compounding"},
		{name: "reserve receipt sum", edit: func(e *FinalSemanticEvidence) { e.Reserve.PrincipalAdditions[0].AmountRao = "19" }, want: "operator/total/live"},
		{name: "deposit receipt logs", edit: func(e *FinalSemanticEvidence) { e.Validators[0].Cycles[0].Pools[0].DepositReceipt.LogsHash = "" }, want: "logs hash"},
		{name: "receipt order", edit: func(e *FinalSemanticEvidence) { e.Validators[0].Cycles[0].Application.Block.Number = 19 }, want: "receipt order"},
		{name: "payout conservation", edit: func(e *FinalSemanticEvidence) { e.Epochs[0].OutstandingRao = "399" }, want: "committed total !="},
		{name: "carry terminal", edit: func(e *FinalSemanticEvidence) { e.Pools[0].FinalCarryRao = "1" }, want: "terminal carry"},
		{name: "reward emission change", edit: func(e *FinalSemanticEvidence) { e.NativeRewards[0].DeltaRao = "9" }, want: "emission change"},
		{name: "reward aggregate stake change", edit: func(e *FinalSemanticEvidence) { e.NativeRewards[0].StakeDeltaRao = "9" }, want: "aggregate stake change"},
		{name: "conflicting immutable reward checkpoint", edit: func(e *FinalSemanticEvidence) {
			var prior *FinalNativeRewardDelta
			for index := range e.NativeRewards {
				reward := &e.NativeRewards[index]
				if reward.Role != "head" || reward.SubjectID != 1 {
					continue
				}
				if prior == nil {
					prior = reward
					continue
				}
				reward.Before = prior.After
				reward.OwnerStakeBeforeEVM = prior.OwnerStakeAfterEVM
				return
			}
		}, want: "native reward checkpoint conflict"},
		{name: "head owner stake substitution", edit: func(e *FinalSemanticEvidence) {
			before, _ := new(big.Int).SetString(e.NativeRewards[0].OwnerStakeBeforeRao, 10)
			e.NativeRewards[0].OwnerStakeAfterRao = "999"
			e.NativeRewards[0].OwnerStakeDeltaRao = new(big.Int).Sub(big.NewInt(999), before).String()
		}, want: "head owner-pair stake differs"},
		{name: "reserve validator split", edit: func(e *FinalSemanticEvidence) {
			for index := range e.NativeRewards {
				if e.NativeRewards[index].Role == "validator" && e.NativeRewards[index].SubjectID == 1 {
					e.NativeRewards[index].ReserveStakeAfterRao = "1004"
					return
				}
			}
		}, want: "reserve-validator"},
		{name: "path coverage", edit: func(e *FinalSemanticEvidence) { e.PathProofs = nil }, want: "path proof records=0"},
		{name: "exit coverage", edit: func(e *FinalSemanticEvidence) { e.ExitCriteria = e.ExitCriteria[:len(e.ExitCriteria)-1] }, want: "final exit criteria="},
		{name: "typed exit assertion", edit: func(e *FinalSemanticEvidence) { e.ExitCriteria[0].Assertions[0].Observed-- }, want: "typed expectation"},
		{name: "unsafe artifact", edit: func(e *FinalSemanticEvidence) { e.PathProofs[0].Artifact.URI = "https://secret@example.test/proofs" }, want: "credential-free"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := finalSemanticClone(t, valid)
			test.edit(candidate)
			resignFinalSemantic(t, candidate)
			verifyErr := VerifyFinalSemanticEvidence(candidate)
			if verifyErr == nil || !strings.Contains(verifyErr.Error(), test.want) {
				t.Fatalf("got %v, want error containing %q", verifyErr, test.want)
			}
			if markdown, err := RenderFinalSemanticEvidenceMarkdown(candidate); err == nil || len(markdown) != 0 {
				t.Fatalf("partial FINAL.md was emitted: bytes=%d err=%v", len(markdown), err)
			}
		})
	}

	hashTamper := finalSemanticClone(t, valid)
	hashTamper.EvidenceHash = finalTestHex(0xee)
	if err := VerifyFinalSemanticEvidence(hashTamper); err == nil || !strings.Contains(err.Error(), "reconstructed") {
		t.Fatalf("tampered evidence hash was not rejected: %v", err)
	}
}

func TestFinalSemanticEvidenceRaceTamperCoverage(t *testing.T) {
	baseline := ChainHead{Number: 10, Hash: finalTestHex(10)}
	terminal := ChainHead{Number: 20, Hash: finalTestHex(20)}
	evidence := FinalSemanticEvidence{
		Window:          ScenarioAcceptanceWindow{BaselineHead: baseline, FirstEpoch: 1, EpochCount: 1},
		EVMTerminalHead: terminal,
		SettlementAccounting: FinalSettlementVaultAccounting{
			Before:                FinalSettlementVaultState{TotalCapturedRao: "10", TotalPaidRao: "4", EscrowAccountedRao: "6", PendingFundingRao: "2", OutstandingLiabilityRao: "4", LiveEscrowStakeRao: "7", Block: baseline},
			After:                 FinalSettlementVaultState{TotalCapturedRao: "20", TotalPaidRao: "8", EscrowAccountedRao: "12", PendingFundingRao: "5", OutstandingLiabilityRao: "7", LiveEscrowStakeRao: "13", Block: terminal},
			TotalCapturedDeltaRao: "10", TotalPaidDeltaRao: "4", EscrowAccountedDeltaRao: "6", PendingFundingDeltaRao: "3", OutstandingLiabilityDeltaRao: "3", LiveEscrowStakeDeltaRao: "6",
			EmissionCapturedEventRao: "10", ClaimPaidEventRao: "4",
		},
	}
	if err := verifyFinalSettlementAccounting(&evidence); err != nil {
		t.Fatal(err)
	}
	originalCaptured := evidence.SettlementAccounting.EmissionCapturedEventRao
	evidence.SettlementAccounting.EmissionCapturedEventRao = "9999"
	if err := verifyFinalSettlementAccounting(&evidence); err == nil || !strings.Contains(err.Error(), "EmissionCaptured/ClaimPaid event sums") {
		t.Fatalf("settlement event substitution was accepted: %v", err)
	}
	evidence.SettlementAccounting.EmissionCapturedEventRao = originalCaptured

	var hotkeyBytes, coldkeyBytes [32]byte
	hotkeyBytes[0], coldkeyBytes[0] = 1, 2
	hotkey, err := ss58.Encode(hotkeyBytes, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	coldkey, err := ss58.Encode(coldkeyBytes, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	rewardArtifact := FinalArtifactLocator{Kind: "native-reward-snapshot", URI: "native-reward.json", ContentHash: bytesSHA256([]byte("x")), SizeBytes: 1}
	evidence.NativeStartHead = ChainHead{Number: 1, Hash: finalTestHex(1)}
	evidence.NativeTerminalHead = ChainHead{Number: 2, Hash: finalTestHex(2)}
	evidence.HeadFleets = []FinalHeadFleetEvidence{{FleetID: 1, UID: 7, Hotkey: hotkey, Coldkey: coldkey}}
	evidence.Validators = []FinalValidatorIdentityEvidence{{Cycles: []FinalCRv4Cycle{{SettlementEpoch: 1, Candidates: []FinalHeadCandidateEvidence{{FleetID: 1, Selected: true}}}}}}
	evidence.NativeRewards = []FinalNativeRewardDelta{{
		Epoch: 1, Role: "head", SubjectID: 1, UID: 7, Hotkey: "0x" + hex.EncodeToString(hotkeyBytes[:]),
		Before: ChainHead{Number: 1, Hash: finalTestHex(1)}, After: ChainHead{Number: 2, Hash: finalTestHex(2)},
		BeforeRao: "1", AfterRao: "2", DeltaRao: "1", StakeBeforeRao: "100", StakeAfterRao: "101", StakeDeltaRao: "1",
		OwnerColdkey: "0x" + hex.EncodeToString(coldkeyBytes[:]), OwnerStakeBeforeRao: "100", OwnerStakeAfterRao: "101", OwnerStakeDeltaRao: "1",
		OwnerStakeBeforeEVM: ChainHead{Number: 1, Hash: finalTestHex(3)}, OwnerStakeAfterEVM: ChainHead{Number: 2, Hash: finalTestHex(4)},
		AfterIncentiveU16: 1, Expected: "positive", SnapshotArtifact: rewardArtifact,
	}}
	if err := verifyFinalRewards(&evidence, map[uint64]FinalPoolUIDEvidence{}, map[uint64]FinalValidatorIdentityEvidence{}); err != nil {
		t.Fatal(err)
	}
	originalOwnerAfter, originalOwnerDelta := evidence.NativeRewards[0].OwnerStakeAfterRao, evidence.NativeRewards[0].OwnerStakeDeltaRao
	evidence.NativeRewards[0].OwnerStakeAfterRao = "102"
	evidence.NativeRewards[0].OwnerStakeDeltaRao = "2"
	if err := verifyFinalRewards(&evidence, map[uint64]FinalPoolUIDEvidence{}, map[uint64]FinalValidatorIdentityEvidence{}); err == nil || !strings.Contains(err.Error(), "head owner-pair stake differs") {
		t.Fatalf("owner-pair substitution was accepted: %v", err)
	}
	evidence.NativeRewards[0].OwnerStakeAfterRao, evidence.NativeRewards[0].OwnerStakeDeltaRao = originalOwnerAfter, originalOwnerDelta

	unsafe := rewardArtifact
	unsafe.URI = "https://secret@example.test/proofs"
	if err := verifyFinalArtifact("race credential substitution", unsafe, "native-reward-snapshot"); err == nil || !strings.Contains(err.Error(), "credential-free") {
		t.Fatalf("credential-bearing artifact was accepted: %v", err)
	}
}

func TestFinalNativeRewardEmissionCanDecreaseAcrossEpochs(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, _ := finalSemanticFixture(t)
	reward := &source.NativeRewards[0]
	if reward.Expected != "positive" || reward.Role != "head" {
		t.Fatalf("unexpected fixture reward: %+v", reward)
	}
	reward.BeforeRao = "20"
	reward.AfterRao = "10"
	reward.DeltaRao = "-10"
	reward.BeforeIncentiveU16 = 2
	reward.AfterIncentiveU16 = 1
	if _, err := BuildFinalSemanticEvidence(source); err != nil {
		t.Fatalf("a decreasing per-epoch emission vector was rejected: %v", err)
	}
}

func TestFinalSemanticPathProofArtifactCount(t *testing.T) {
	proof := &FinalValidatorPathProofEvidence{ProofCount: 2}
	if err := verifyFinalPathProofArtifact(proof, []byte("{\"epoch\":10}\n{\"epoch\":11}\n")); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalPathProofArtifact(proof, []byte("{\"epoch\":10}\n")); err == nil || !strings.Contains(err.Error(), "declared 2") {
		t.Fatalf("short proof artifact was not rejected: %v", err)
	}
	if err := verifyFinalPathProofArtifact(proof, []byte("[]\n{}\n")); err == nil || !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("malformed proof artifact was not rejected: %v", err)
	}
}

func TestFinalSemanticPoolAuditDistinguishesUnderpaymentFromRecovery(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, _ := finalSemanticFixture(t)
	pool := source.Validators[0].Cycles[0].Pools[0]
	pool.AuditCompliant = false
	pool.AuditStatus = validatorpkg.DepositAuditMismatch
	pool.AuditDisposition = "zero_pool_weight"
	pool.AuditError = "observed deposit does not match signed usage"
	pool.ObservedDepositRao = "50"
	pool.QualityPPM = 0
	pool.QualityFactor = FinalRational{Numerator: "0", Denominator: "1"}
	pool.ImpliedUsageGiB = FinalRational{Numerator: "0", Denominator: "1"}
	pool.RawScore = FinalRational{Numerator: "0", Denominator: "1"}
	pool.AppliedWeight = 0
	cycle := source.Validators[0].Cycles[0]
	score, eligible, err := verifyFinalPoolWeight(&source, cycle.SettlementEpoch, cycle.QualityMinimumPPM, cycle.QualityMaximumPPM, &pool)
	if err != nil || eligible || score == nil || score.Sign() != 0 {
		t.Fatalf("underpayment audit score=%v eligible=%t error=%v", score, eligible, err)
	}
	pool.ObservedDepositRao = pool.RequiredDepositRao
	if _, _, err := verifyFinalPoolWeight(&source, cycle.SettlementEpoch, cycle.QualityMinimumPPM, cycle.QualityMaximumPPM, &pool); err == nil || !strings.Contains(err.Error(), "underpayment/zero-pool") {
		t.Fatalf("dishonest rejection accepted a compliant cumulative deposit: %v", err)
	}
	pool.AuditCompliant = true
	pool.AuditStatus = validatorpkg.DepositAuditCompliant
	pool.AuditDisposition = "pool_weight_eligible"
	pool.AuditError = ""
	pool.QualityPPM = 500_000
	pool.QualityFactor = FinalRational{Numerator: "1", Denominator: "2"}
	pool.ImpliedUsageGiB = FinalRational{Numerator: "1", Denominator: "1"}
	pool.RawScore = FinalRational{Numerator: "1", Denominator: "2"}
	pool.AppliedWeight = source.Validators[0].Cycles[0].Pools[0].AppliedWeight
	pool.QualityFactor = FinalRational{Numerator: "3", Denominator: "4"}
	pool.RawScore = FinalRational{Numerator: "3", Denominator: "4"}
	if score, eligible, err := verifyFinalPoolWeight(&source, cycle.SettlementEpoch, cycle.QualityMinimumPPM, cycle.QualityMaximumPPM, &pool); err != nil || !eligible || score.Cmp(big.NewRat(3, 4)) != 0 {
		t.Fatalf("recovered audit score=%v eligible=%t error=%v", score, eligible, err)
	}
}

func requireFinalSemanticReleaseScaleFixture(t *testing.T) {
	t.Helper()
	if finalSemanticRaceEnabled {
		t.Skip("the complete 1,000-miner semantic graph runs in the normal gate; bounded cache, tamper, public-RPC, publication, cancellation, and concurrent-winner races are adjacent")
	}
}

// Build exact prefix claims from the raw-score inputs. A numerator is the
// number of claims per fleet and a denominator is the number of fleets sharing
// each claim, so the independent verifier reconstructs the declared rational.
func finalSemanticFixtureHeadEgress(t *testing.T, candidates []FinalHeadCandidateEvidence, memberCount int, settlementEpoch uint64) (map[uint64][][32]byte, map[uint64]*big.Rat) {
	t.Helper()
	if memberCount <= 0 || settlementEpoch == 0 {
		t.Fatal("fixture head egress context is incomplete")
	}
	groups := map[string][]FinalHeadCandidateEvidence{}
	scores := map[string]*big.Rat{}
	seenFleets := map[uint64]bool{}
	for _, candidate := range candidates {
		if candidate.FleetID == 0 || seenFleets[candidate.FleetID] {
			t.Fatalf("fixture head fleet %d is zero or duplicated", candidate.FleetID)
		}
		seenFleets[candidate.FleetID] = true
		score, err := finalPositiveRational("fixture candidate raw score", candidate.RawScore)
		if err != nil {
			t.Fatal(err)
		}
		key := score.RatString()
		groups[key] = append(groups[key], candidate)
		scores[key] = score
	}
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	egressByFleet := make(map[uint64][][32]byte, len(candidates))
	for _, key := range groupKeys {
		group := groups[key]
		sort.Slice(group, func(i, j int) bool { return group[i].FleetID < group[j].FleetID })
		numerator, denominator := scores[key].Num(), scores[key].Denom()
		if !numerator.IsUint64() || !denominator.IsUint64() {
			t.Fatalf("fixture candidate score %s exceeds uint64", key)
		}
		numeratorValue, denominatorValue := numerator.Uint64(), denominator.Uint64()
		if numeratorValue == 0 || numeratorValue > uint64(memberCount) {
			t.Fatalf("fixture candidate score %s needs %d claims but fleets have %d members", key, numeratorValue, memberCount)
		}
		if denominatorValue == 0 || denominatorValue > uint64(len(group)) || uint64(len(group))%denominatorValue != 0 {
			t.Fatalf("fixture candidate score %s has %d fleets, not complete groups of %d", key, len(group), denominatorValue)
		}
		groupSize := int(denominatorValue)
		for groupStart := 0; groupStart < len(group); groupStart += groupSize {
			for claimIndex := uint64(0); claimIndex < numeratorValue; claimIndex++ {
				egress := sha256.Sum256([]byte(fmt.Sprintf("final-semantic-fixture/egress/v1/%d/%s/%d/%d", settlementEpoch, key, groupStart/groupSize, claimIndex)))
				if egress == ([32]byte{}) {
					t.Fatal("fixture candidate egress hash is zero")
				}
				for index := groupStart; index < groupStart+groupSize; index++ {
					fleetID := group[index].FleetID
					egressByFleet[fleetID] = append(egressByFleet[fleetID], egress)
				}
			}
		}
	}
	claimCounts := map[[32]byte]uint64{}
	for fleetID, egresses := range egressByFleet {
		seenEgress := map[[32]byte]bool{}
		for _, egress := range egresses {
			if seenEgress[egress] {
				t.Fatalf("fixture head fleet %d repeats an egress claim", fleetID)
			}
			seenEgress[egress] = true
			claimCounts[egress]++
		}
	}
	rawByFleet := make(map[uint64]*big.Rat, len(candidates))
	for _, candidate := range candidates {
		raw := new(big.Rat)
		for _, egress := range egressByFleet[candidate.FleetID] {
			raw.Add(raw, new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).SetUint64(claimCounts[egress])))
		}
		declared, err := finalPositiveRational("fixture candidate raw score", candidate.RawScore)
		if err != nil || raw.Cmp(declared) != 0 {
			t.Fatalf("fixture head fleet %d prefix score=%s, want %s: %v", candidate.FleetID, raw.RatString(), candidate.RawScore.Numerator+"/"+candidate.RawScore.Denominator, err)
		}
		rawByFleet[candidate.FleetID] = raw
	}
	return egressByFleet, rawByFleet
}

// Selection is downstream of prefix evidence; changing a pre-seal selection
// hint must not change the raw claims or their exact reconstructed scores.
func TestFinalSemanticFixtureHeadEgressFollowsRawScoreNotSelection(t *testing.T) {
	candidates := []FinalHeadCandidateEvidence{
		{FleetID: 1, UID: 101, RawScore: FinalRational{Numerator: "2", Denominator: "1"}},
		{FleetID: 2, UID: 102, RawScore: FinalRational{Numerator: "1", Denominator: "1"}, Selected: true},
		{FleetID: 3, UID: 103, RawScore: FinalRational{Numerator: "1", Denominator: "2"}},
		{FleetID: 4, UID: 104, RawScore: FinalRational{Numerator: "1", Denominator: "2"}, Selected: true},
		{FleetID: 5, UID: 105, RawScore: FinalRational{Numerator: "3", Denominator: "2"}},
		{FleetID: 6, UID: 106, RawScore: FinalRational{Numerator: "3", Denominator: "2"}, Selected: true},
	}
	firstEgress, firstRaw := finalSemanticFixtureHeadEgress(t, candidates, 4, 10)
	for index := range candidates {
		candidates[index].Selected = !candidates[index].Selected
	}
	secondEgress, secondRaw := finalSemanticFixtureHeadEgress(t, candidates, 4, 10)
	wantClaims := map[uint64]int{1: 2, 2: 1, 3: 1, 4: 1, 5: 3, 6: 3}
	for fleetID, want := range wantClaims {
		if len(firstEgress[fleetID]) != want || len(secondEgress[fleetID]) != want || firstRaw[fleetID].Cmp(secondRaw[fleetID]) != 0 {
			t.Fatalf("fixture head fleet %d claims/raw changed with selection: %d/%d %s/%s", fleetID, len(firstEgress[fleetID]), len(secondEgress[fleetID]), firstRaw[fleetID], secondRaw[fleetID])
		}
		for index := range firstEgress[fleetID] {
			if firstEgress[fleetID][index] != secondEgress[fleetID][index] {
				t.Fatalf("fixture head fleet %d claim %d changed with selection", fleetID, index)
			}
		}
	}
	if firstEgress[3][0] != firstEgress[4][0] || firstEgress[5][0] != firstEgress[6][0] || firstEgress[5][1] != firstEgress[6][1] || firstEgress[5][2] != firstEgress[6][2] {
		t.Fatal("fixture shared-prefix groups are not exact")
	}
}

// Construct the synthetic evidence policy from release-scale resolved inputs.
// Its reduced usage rate retains production epoch and campaign ceilings.
func finalSemanticFixtureReleasePolicy(cfg *ResolvedConfig) (protocol.Policy, error) {
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || uint64(cfg.Config.Scenarios.ShortEpochs) != finalReleaseEpochCount {
		return protocol.Policy{}, fmt.Errorf("semantic fixture release policy context is incomplete")
	}
	if len(cfg.Policy.Deposit.Tiers) < 2 || cfg.Policy.Deposit.Tiers[1].MinConvictionRao != cfg.Config.Scenarios.VoluntaryConvictionRao || cfg.Config.Scenarios.DishonestDepositRao < cfg.Config.Scenarios.VoluntaryConvictionRao || cfg.Config.Scenarios.DishonestDepositRao >= cfg.Policy.Deposit.EpochCapRaoPerOperator {
		return protocol.Policy{}, fmt.Errorf("semantic fixture release deposit economics are inconsistent")
	}
	policy := *cfg.Policy
	policy.EffectiveEpoch = 10
	policy.Settlement.EpochBlocks = finalReleaseEpochBlocks
	policy.Settlement.RootCommitWindowBlocks = 1
	policy.Settlement.FinalizeOffsetBlocks = finalReleaseFinalizeOffsetBlocks
	policy.Settlement.CloseGraceBlocks = 1
	policy.Steering.MaxWeightLimitU16 = crv4.U16Max
	policy.Deposit.Tiers = append([]protocol.DepositTier(nil), cfg.Policy.Deposit.Tiers...)
	policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB = 100
	policy.Deposit.Tiers[0].RateDenominator = 1
	policy.Verify.TrailDepth = 2
	resolved := *cfg
	resolved.Policy = &policy
	requiredRao, err := releaseCampaignDepositRequirement(&resolved)
	if err != nil {
		return protocol.Policy{}, fmt.Errorf("derive semantic fixture campaign cap: %w", err)
	}
	policy.Deposit.TotalTestCampaignCapRao = requiredRao
	if err := policy.Validate(); err != nil {
		return protocol.Policy{}, err
	}
	return policy, nil
}

// Proves the fixture admits the exact release campaign requirement, rejects
// one rao less, and conserves that cap across operator allocations.
func TestFinalSemanticFixtureReleasePolicyUsesDerivedCampaignCap(t *testing.T) {
	cfg := testResolvedConfig(t)
	policy, err := finalSemanticFixtureReleasePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := *cfg
	resolved.Policy = &policy
	resolved.PolicyHash, err = policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	requiredRao, err := releaseCampaignDepositRequirement(&resolved)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Deposit.TotalTestCampaignCapRao != requiredRao {
		t.Fatalf("fixture campaign cap=%d, want derived requirement %d", policy.Deposit.TotalTestCampaignCapRao, requiredRao)
	}
	if policy.Deposit.EpochCapRaoPerOperator != cfg.Policy.Deposit.EpochCapRaoPerOperator || policy.Deposit.EpochCapRaoPerOperator <= cfg.Config.Scenarios.DishonestDepositRao || len(policy.Deposit.Tiers) != len(cfg.Policy.Deposit.Tiers) || policy.Deposit.Tiers[1].MinConvictionRao != cfg.Config.Scenarios.VoluntaryConvictionRao {
		t.Fatal("fixture policy changed or invalidated release-scale epoch-cap and conviction boundaries")
	}
	publicRoles, err := derivePublicRoles(&resolved)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(&resolved, testSetupFacts(), publicRoles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("exact fixture campaign requirement was rejected: %v", err)
	}
	allocatedRao := uint64(0)
	operatorActionCount := 0
	for _, action := range plan.Actions {
		if !strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") {
			continue
		}
		amountRao, parseErr := strconv.ParseUint(action.Parameters["campaign_requirement_rao"], 10, 64)
		var addOK bool
		allocatedRao, addOK = checkedAdd(allocatedRao, amountRao)
		if parseErr != nil || !addOK {
			t.Fatalf("operator campaign allocation is invalid: %v", parseErr)
		}
		operatorActionCount++
	}
	if operatorActionCount != cfg.Config.Topology.Operators || allocatedRao != policy.Deposit.TotalTestCampaignCapRao {
		t.Fatalf("operator campaign allocations=%d/%d, want %d actions totaling cap %d", operatorActionCount, allocatedRao, cfg.Config.Topology.Operators, policy.Deposit.TotalTestCampaignCapRao)
	}
	underfunded := policy
	underfunded.Deposit.TotalTestCampaignCapRao--
	resolved.Policy = &underfunded
	resolved.PolicyHash, err = underfunded.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildPlan(&resolved, testSetupFacts(), publicRoles, time.Unix(1, 0).UTC()); err == nil || !strings.Contains(err.Error(), "below release requirement") {
		t.Fatalf("underfunded fixture campaign was accepted: %v", err)
	}
}

func finalSemanticFixture(t *testing.T) (FinalSemanticEvidence, map[string][]byte) {
	t.Helper()
	artifacts := map[string][]byte{}
	artifact := func(kind, name string, data []byte) FinalArtifactLocator {
		uri := "artifacts/" + name
		artifacts[uri] = append([]byte(nil), data...)
		return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	}
	cfg := testResolvedConfig(t)
	policy, err := finalSemanticFixtureReleasePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	policyBytes, err := policy.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	policyHash, err := policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	policyLocator := artifact("policy", "policy.json", policyBytes)
	ss58Key := func(namespace byte, index int) string {
		var key [32]byte
		key[0], key[1], key[30], key[31] = namespace, byte(index>>8), byte(index), namespace^byte(index)
		encoded, err := ss58.Encode(key, ss58.BittensorPrefix)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	validatorPathKeys := make([]ed25519.PrivateKey, 2)
	operatorServerKeys := make([]ed25519.PrivateKey, 2)
	validatorHotkeys := make([]*crv4.Keypair, 2)
	for i := 0; i < 2; i++ {
		validatorPathKeys[i] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x41 + i)}, ed25519.SeedSize))
		operatorServerKeys[i] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x51 + i)}, ed25519.SeedSize))
		var seed [32]byte
		seed[0], seed[31] = byte(0x61+i), byte(0x71+i)
		validatorHotkeys[i], err = crv4.KeypairFromSeed(seed)
		if err != nil {
			t.Fatal(err)
		}
	}
	nativeReceipt := func(name string, block uint64, extrinsic bool) FinalNativeReceipt {
		receipt := FinalNativeReceipt{Block: ChainHead{Number: block, Hash: finalTestHex(byte(block))}, Proof: artifact("native-receipt", name+".json", []byte(name))}
		if extrinsic {
			receipt.ExtrinsicHash = finalTestHex(byte(block + 1))
		}
		return receipt
	}
	evmReceiptIndex := byte(1)
	evmReceipt := func(name string, block uint64) FinalEVMReceipt {
		evmReceiptIndex++
		return FinalEVMReceipt{TransactionHash: finalTestHex(evmReceiptIndex), Block: ChainHead{Number: block, Hash: finalTestHex(byte(block))}, Status: "success", LogsHash: finalTestHex(evmReceiptIndex + 100), Proof: artifact("evm-receipt", name+".json", []byte(name))}
	}

	cycle := FinalCRv4Cycle{
		SettlementEpoch: 10, SubnetEpoch: 7,
		NativeSnapshot:    ChainHead{Number: 15, Hash: finalTestHex(15)},
		EVMSnapshot:       ChainHead{Number: 101, Hash: finalTestHex(101)},
		Theta:             FinalRational{Numerator: "3", Denominator: "10"},
		QualityMinimumPPM: policy.Steering.QualityTransform.MinimumPPM,
		QualityMaximumPPM: policy.Steering.QualityTransform.MaximumPPM,
		MaximumHeadFleets: policy.Steering.MaximumHeadFleets, MaxWeightLimitU16: policy.Steering.MaxWeightLimitU16,
		Formula: finalWeightFormula,
		Commit:  nativeReceipt("commit", 20, true), Reveal: nativeReceipt("reveal", 21, false), Application: nativeReceipt("application", 22, false),
	}
	for i := 0; i < finalHeadCandidateCount; i++ {
		cycle.Candidates = append(cycle.Candidates, FinalHeadCandidateEvidence{
			FleetID: uint64(i + 1), Rank: uint16(i + 1), UID: uint16(1000 + i),
			RawScore: FinalRational{Numerator: "1", Denominator: "1"},
			Selected: i < finalHeadSlotCount,
		})
	}
	for noID, uid := range []uint16{11, 13} {
		cycle.Pools = append(cycle.Pools, FinalPoolWeightEvidence{
			NoID: uint64(noID + 1), UID: uid, SourceEpoch: 9, UsageBytes: 1 << 30,
			ConvictionBeforeRao: "1000", RateNumeratorRaoPerGiB: policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB, RateDenominator: policy.Deposit.Tiers[0].RateDenominator,
			EpochDepositCapRao: strconv.FormatUint(policy.Deposit.EpochCapRaoPerOperator, 10), RequiredDepositRao: "100", ObservedDepositRao: "100",
			QualityPPM: 500_000, QualityFactor: FinalRational{Numerator: "3", Denominator: "4"},
			ImpliedUsageGiB: FinalRational{Numerator: "1", Denominator: "1"}, RawScore: FinalRational{Numerator: "3", Denominator: "4"},
			Formula: finalDepositFormula, AuditStatus: validatorpkg.DepositAuditCompliant, AuditCompliant: true, AuditDisposition: "pool_weight_eligible",
			ArtifactHash: finalTestHex(byte(44 + noID)), PayoutArtifact: artifact("payout-artifact", fmt.Sprintf("deposit-payout-%d.json", noID+1), []byte(fmt.Sprintf("deposit payout %d", noID+1))),
			DepositReceipt: evmReceipt(fmt.Sprintf("deposit-%d", noID+1), uint64(90+2*noID)),
		})
	}
	minerClientID := func(minerID uint64) connect.Id {
		var clientID connect.Id
		clientID[0], clientID[14], clientID[15] = 1, byte(minerID>>8), byte(minerID)
		return clientID
	}
	key32 := func(kind string, id uint64) [32]byte {
		return sha256.Sum256([]byte(fmt.Sprintf("%s-%d", kind, id)))
	}
	hex32 := func(value [32]byte) string { return "0x" + hex.EncodeToString(value[:]) }
	fixtureFleetManifest := func(fleetID uint64) protocol.FleetManifest {
		manifest := protocol.FleetManifest{
			Schema: protocol.FleetManifestSchema, ChainID: 945, Netuid: 521,
			Coordinator: [20]byte(common.HexToAddress("0x1111111111111111111111111111111111111111")),
			FleetID:     key32("fleet", fleetID), Hotkey: key32("hotkey", fleetID), Generation: 1,
		}
		for member := uint64(1); member <= 4; member++ {
			minerID := (fleetID-1)*4 + member
			manifest.Members = append(manifest.Members, protocol.FleetMember{ClientID: [16]byte(minerClientID(minerID)), ClientKey: key32("client", minerID)})
		}
		return manifest
	}
	previousMeasurement := map[uint64][]byte{}
	previousArtifact := map[uint64]*validatorpkg.ReleaseMeasurementArtifact{}
	attemptRoots := map[uint64]string{}
	buildMeasurement := func(cycle FinalCRv4Cycle, validatorID uint64) ([]byte, string, *validatorpkg.VerifiedReleaseMeasurement) {
		statsByNO := map[uint64][]validatorpkg.ReleaseProviderMeasurement{1: {}, 2: {}}
		bindings := make([]validatorpkg.ReleaseBindingMeasurement, 0, 1000)
		headKeys := make(map[uint64]validatorpkg.FleetScoreKey, finalHeadCandidateCount)
		candidateUIDs := make(map[uint64]uint16, len(cycle.Candidates))
		for _, candidate := range cycle.Candidates {
			candidateUIDs[candidate.FleetID] = candidate.UID
		}
		egressByFleet, rawByFleet := finalSemanticFixtureHeadEgress(t, cycle.Candidates, cfg.Config.Topology.ClientsPerHeadFleet, cycle.SettlementEpoch)
		zero := hex32([32]byte{})
		for minerID := uint64(1); minerID <= 1000; minerID++ {
			clientID := minerClientID(minerID)
			noID := uint64(operatorForMiner(cfg, int(minerID)))
			provider := validatorpkg.ReleaseProviderMeasurement{
				ClientID: clientID.String(), LatencyBuckets: make([]uint64, 31),
				HasPriorQuality: true, PriorQualityPPM: 500_000,
			}
			binding := validatorpkg.ReleaseBindingMeasurement{NoID: noID, ClientID: clientID.String(), FleetID: zero, Hotkey: zero, ClientKey: zero, LocalClientKey: zero, CommitmentHash: zero}
			if minerID <= finalHeadCandidateCount*4 {
				fleetID := (minerID-1)/4 + 1
				fleetManifest := fixtureFleetManifest(fleetID)
				commitment, commitmentErr := fleetManifest.CommitmentHash()
				if commitmentErr != nil {
					t.Fatal(commitmentErr)
				}
				fleetKey, hotkey := fleetManifest.FleetID, fleetManifest.Hotkey
				clientKey := key32("client", minerID)
				uid := candidateUIDs[fleetID]
				memberIndex := int((minerID - 1) % uint64(cfg.Config.Topology.ClientsPerHeadFleet))
				if egresses := egressByFleet[fleetID]; memberIndex < len(egresses) {
					provider.EgressIPHashHexes = []string{hex32(egresses[memberIndex])}
				}
				binding = validatorpkg.ReleaseBindingMeasurement{
					NoID: noID, ClientID: clientID.String(), Active: true, FleetID: hex32(fleetKey), Hotkey: hex32(hotkey),
					ClientKey: hex32(clientKey), LocalClientKey: hex32(clientKey), CommitmentHash: hex32(commitment), Generation: 1,
					ValidFromEpoch: 1, ValidToEpoch: cycle.SettlementEpoch + 100, RecordUID: uid, LiveUIDFound: true, LiveUID: uid,
				}
				headKeys[fleetID] = validatorpkg.FleetScoreKey{FleetID: fleetKey, Hotkey: hotkey, Generation: 1, UID: uid}
			}
			statsByNO[noID] = append(statsByNO[noID], provider)
			bindings = append(bindings, binding)
		}
		for noID := uint64(1); noID <= 2; noID++ {
			sort.Slice(statsByNO[noID], func(i, j int) bool { return statsByNO[noID][i].ClientID < statsByNO[noID][j].ClientID })
		}
		sort.Slice(bindings, func(i, j int) bool {
			if bindings[i].NoID != bindings[j].NoID {
				return bindings[i].NoID < bindings[j].NoID
			}
			return bindings[i].ClientID < bindings[j].ClientID
		})
		type fixtureEMAValue struct {
			key   validatorpkg.FleetScoreKey
			value *big.Rat
		}
		currentEMA := make(map[string]fixtureEMAValue, finalHeadCandidateCount)
		for fleetID := uint64(1); fleetID <= finalHeadCandidateCount; fleetID++ {
			raw := rawByFleet[fleetID]
			if raw == nil {
				t.Fatalf("fixture head fleet %d has no reconstructed raw score", fleetID)
			}
			key := headKeys[fleetID]
			currentEMA[key.String()] = fixtureEMAValue{key: key, value: new(big.Rat).Set(raw)}
		}
		priorEMA := map[string]fixtureEMAValue{}
		if prior := previousArtifact[validatorID]; prior != nil {
			for _, record := range prior.HeadEMA {
				value, ok := new(big.Rat).SetString(record.Next.Numerator + "/" + record.Next.Denominator)
				if !ok {
					t.Fatalf("invalid fixture prior EMA %+v", record.Next)
				}
				priorEMA[record.Key.String()] = fixtureEMAValue{key: record.Key, value: value}
			}
		}
		keys := make([]string, 0, len(currentEMA)+len(priorEMA))
		seenKeys := map[string]bool{}
		for key := range currentEMA {
			seenKeys[key] = true
			keys = append(keys, key)
		}
		for key := range priorEMA {
			if !seenKeys[key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		alpha := new(big.Rat).SetFrac(new(big.Int).SetUint64(policy.Steering.HeadScoreEMA.Numerator), new(big.Int).SetUint64(policy.Steering.HeadScoreEMA.Denominator))
		oneMinusAlpha := new(big.Rat).Sub(big.NewRat(1, 1), alpha)
		headEMA := make([]validatorpkg.HeadEMAMeasurement, 0, len(keys))
		for _, id := range keys {
			current, hasRaw := currentEMA[id]
			prior, hasPrior := priorEMA[id]
			key := current.key
			if !hasRaw {
				key = prior.key
				current.value = new(big.Rat)
			}
			if !hasPrior {
				prior.value = new(big.Rat)
			}
			next := new(big.Rat)
			if !hasPrior {
				next.Set(current.value)
			} else {
				next.Add(new(big.Rat).Mul(alpha, current.value), new(big.Rat).Mul(oneMinusAlpha, prior.value))
			}
			encode := func(value *big.Rat) validatorpkg.RationalJSON {
				return validatorpkg.RationalJSON{Numerator: value.Num().String(), Denominator: value.Denom().String()}
			}
			headEMA = append(headEMA, validatorpkg.HeadEMAMeasurement{
				Key: key, HasRaw: hasRaw, Raw: encode(current.value), HasPrior: hasPrior, Prior: encode(prior.value), Next: encode(next),
			})
		}
		audits := make([]validatorpkg.DepositAudit, len(cycle.Pools))
		pools := make([]validatorpkg.ReleasePoolMeasurement, len(cycle.Pools))
		for index, pool := range cycle.Pools {
			audits[index] = finalDepositAuditFromPool(cycle.SettlementEpoch, &pool)
			pools[index] = validatorpkg.ReleasePoolMeasurement{NoID: pool.NoID, UID: pool.UID, PoolHotkey: hex32(key32("pool", pool.NoID))}
		}
		previousHash := ""
		if prior := previousMeasurement[validatorID]; len(prior) != 0 {
			previousHash = validatorpkg.ReleaseMeasurementContentHash(prior)
		}
		cutBlock := cycle.NativeSnapshot.Number
		if cutBlock > 1 {
			cutBlock--
		}
		measurement := &validatorpkg.ReleaseMeasurementArtifact{
			Schema: validatorpkg.ReleaseMeasurementSchema, DeploymentID: "ur-subnet-testnet-v1-attempt-4", ChainID: 945,
			GenesisHash: finalTestHex(5), Coordinator: "0x1111111111111111111111111111111111111111", SettlementVault: "0x3333333333333333333333333333333333333333",
			ValidatorID: validatorID, Netuid: 521, SubnetEpoch: cycle.SubnetEpoch, NativeSnapshotBlock: cycle.NativeSnapshot.Number,
			NativeSnapshotHash: cycle.NativeSnapshot.Hash, EVMSnapshotBlock: cycle.EVMSnapshot.Number, EVMSnapshotHash: cycle.EVMSnapshot.Hash,
			SettlementEpoch: cycle.SettlementEpoch, PolicyHash: policyHash, Policy: policy, PreviousArtifactHash: previousHash,
			ControlledNOIDs: []uint64{},
			Inputs: []validatorpkg.ReleaseMeasurementInput{
				{NoID: 1, SettlementEpoch: cycle.SettlementEpoch, CutNativeBlock: cutBlock, CutNativeBlockHash: finalTestHex(byte(cutBlock)), CutEVMSnapshotBlock: cycle.EVMSnapshot.Number, CutEVMSnapshotHash: cycle.EVMSnapshot.Hash, Stats: validatorpkg.ReleaseStatsMeasurement{Config: validatorpkg.ReleaseStatsConfig{AMin: policy.Verify.ReliabilityAMin, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000}, Providers: statsByNO[1]}},
				{NoID: 2, SettlementEpoch: cycle.SettlementEpoch, CutNativeBlock: cutBlock, CutNativeBlockHash: finalTestHex(byte(cutBlock)), CutEVMSnapshotBlock: cycle.EVMSnapshot.Number, CutEVMSnapshotHash: cycle.EVMSnapshot.Hash, Stats: validatorpkg.ReleaseStatsMeasurement{Config: validatorpkg.ReleaseStatsConfig{AMin: policy.Verify.ReliabilityAMin, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000}, Providers: statsByNO[2]}},
			},
			Bindings: bindings, HeadEMA: headEMA, Pools: pools, DepositAudits: audits, SelfUID: uint16(10 + 2*validatorID),
		}
		if attemptRoots[validatorID] == "" {
			attemptRoots[validatorID] = t.TempDir()
		}
		attachFinalAttemptCuts(t, measurement, validatorPathKeys[validatorID-1], operatorServerKeys, attemptRoots[validatorID], previousArtifact[validatorID])
		encoded, contentHash, verified, err := validatorpkg.SealReleaseMeasurementArtifact(measurement)
		if err != nil {
			t.Fatalf("%v; first binding=%+v", err, bindings[0])
		}
		previousMeasurement[validatorID] = append([]byte(nil), encoded...)
		previousArtifact[validatorID] = measurement
		return encoded, contentHash, verified
	}
	sealCycle := func(cycle FinalCRv4Cycle, validatorID uint64) FinalCRv4Cycle {
		epochStart := uint64(100) + (cycle.SettlementEpoch-10)*finalReleaseEpochBlocks
		cycle.NativeSnapshot = ChainHead{Number: epochStart + 5, Hash: finalTestHex(byte(epochStart + 5))}
		cycle.Commit.Block = ChainHead{Number: epochStart + 10 + validatorID, Hash: finalTestHex(byte(epochStart + 10 + validatorID))}
		cycle.Reveal.Block = ChainHead{Number: epochStart + 20 + validatorID, Hash: finalTestHex(byte(epochStart + 20 + validatorID))}
		cycle.Application.Block = ChainHead{Number: epochStart + 30 + validatorID, Hash: finalTestHex(byte(epochStart + 30 + validatorID))}
		fleetByUID := make(map[uint16]uint64, len(cycle.Candidates))
		for _, candidate := range cycle.Candidates {
			if candidate.UID == 0 || fleetByUID[candidate.UID] != 0 {
				t.Fatalf("fixture candidate UID %d is zero or duplicated", candidate.UID)
			}
			fleetByUID[candidate.UID] = candidate.FleetID
		}
		measurementBytes, measurementHash, verified := buildMeasurement(cycle, validatorID)
		if verified == nil {
			t.Fatal("fixture release measurement has no verified decision")
		}
		selected := make(map[uint16]bool, len(verified.SelectedHead))
		for _, head := range verified.SelectedHead {
			selected[head.UID] = true
		}
		cycle.Candidates = make([]FinalHeadCandidateEvidence, 0, len(verified.EligibleHead))
		for rank, head := range verified.EligibleHead {
			fleetID := fleetByUID[head.UID]
			if fleetID == 0 {
				t.Fatalf("verified fixture candidate UID %d has no fleet", head.UID)
			}
			cycle.Candidates = append(cycle.Candidates, FinalHeadCandidateEvidence{
				FleetID: fleetID, Rank: uint16(rank + 1), UID: head.UID,
				RawScore: finalRationalFromBig(head.Score), Selected: selected[head.UID],
			})
		}
		valueUIDs := append([]uint16(nil), verified.UIDs...)
		scores := make([]*big.Rat, len(verified.Scores))
		for index, score := range verified.Scores {
			scores[index] = new(big.Rat).Set(score)
		}
		capped, err := crv4.ApplyMaxWeightLimitRational(scores, cycle.MaxWeightLimitU16)
		if err != nil {
			t.Fatal(err)
		}
		valueUIDs, values, err := crv4.NormalizeRationalToU16(valueUIDs, capped)
		if err != nil {
			t.Fatal(err)
		}
		if err := finalRepairMaxWeightLimitU16(valueUIDs, values, cycle.MaxWeightLimitU16); err != nil {
			t.Fatal(err)
		}
		valueByUID := map[uint16]uint16{}
		cycle.Submitted = nil
		cycle.RealizedHeadValue, cycle.RealizedPoolValue, cycle.RealizedTotalValue = 0, 0, 0
		for index, uid := range valueUIDs {
			valueByUID[uid] = values[index]
			cycle.Submitted = append(cycle.Submitted, FinalSubmittedWeight{UID: uid, Score: finalRationalFromBig(scores[index]), Value: values[index]})
		}
		for index := range cycle.Candidates {
			cycle.Candidates[index].AppliedWeight = valueByUID[cycle.Candidates[index].UID]
			if cycle.Candidates[index].Selected {
				cycle.RealizedHeadValue += uint64(cycle.Candidates[index].AppliedWeight)
			}
		}
		for index := range cycle.Pools {
			cycle.Pools[index].AppliedWeight = valueByUID[cycle.Pools[index].UID]
			cycle.RealizedPoolValue += uint64(cycle.Pools[index].AppliedWeight)
		}
		for _, submitted := range cycle.Submitted {
			cycle.RealizedTotalValue += uint64(submitted.Value)
		}
		encodedValues, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		cycle.ValuesHash = bytesSHA256(encodedValues)
		eligibleUIDs := make([]uint16, len(cycle.Candidates))
		eligibleScores := make([]validatorpkg.RationalJSON, len(cycle.Candidates))
		for index, candidate := range cycle.Candidates {
			eligibleUIDs[index] = candidate.UID
			eligibleScores[index] = validatorpkg.RationalJSON{Numerator: candidate.RawScore.Numerator, Denominator: candidate.RawScore.Denominator}
		}
		selectedUIDs, rejectedUIDs := finalCandidateUIDs(cycle.Candidates)
		intentScores := make([]validatorpkg.RationalJSON, len(cycle.Submitted))
		for index, submitted := range cycle.Submitted {
			intentScores[index] = validatorpkg.RationalJSON{Numerator: submitted.Score.Numerator, Denominator: submitted.Score.Denominator}
		}
		cycle.MaskedUIDs = append([]uint16(nil), verified.MaskedUIDs...)
		cycle.MeasurementArtifact = artifact("validator-release-measurement", fmt.Sprintf("validator-%d-measurement-%d.json", validatorID, cycle.SettlementEpoch), measurementBytes)
		prepared := finalTestPreparedSubmission(t, valueUIDs, values, cycle, validatorHotkeys[validatorID-1].PublicKey())
		cycle.Commit.ExtrinsicHash = prepared.ExtrinsicHash
		envelopeBytes, envelopeHash, _, err := validatorpkg.SealReleaseMeasurementEnvelope(measurementBytes, uint16(10+2*validatorID), validatorHotkeys[validatorID-1], prepared.ExtrinsicHash, time.Unix(1_700_000_000+int64(cycle.SettlementEpoch), 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		cycle.MeasurementEnvelope = artifact("validator-release-measurement-envelope", fmt.Sprintf("validator-%d-measurement-envelope-%d.json", validatorID, cycle.SettlementEpoch), envelopeBytes)
		audits := make([]validatorpkg.DepositAudit, len(cycle.Pools))
		for i, pool := range cycle.Pools {
			audits[i] = finalDepositAuditFromPool(cycle.SettlementEpoch, &pool)
		}
		intent := validatorpkg.SteeringIntent{
			Schema: validatorpkg.SteeringIntentSchema, ValidatorID: validatorID, Netuid: 521,
			SubnetEpoch: cycle.SubnetEpoch, NativeSnapshotBlock: cycle.NativeSnapshot.Number, NativeSnapshotHash: cycle.NativeSnapshot.Hash,
			EVMSnapshotBlock: cycle.EVMSnapshot.Number, EVMSnapshotHash: cycle.EVMSnapshot.Hash, SettlementEpoch: cycle.SettlementEpoch,
			PolicyHash: policyHash, MeasurementArtifactPath: "measurements/" + strings.TrimPrefix(measurementHash, "sha256:") + ".json",
			MeasurementArtifactHash: measurementHash, MeasurementArtifactSize: uint64(len(measurementBytes)), SelfUID: uint16(10 + 2*validatorID),
			MeasurementEnvelopePath: "measurements/envelopes/" + strings.TrimPrefix(envelopeHash, "sha256:") + ".json", MeasurementEnvelopeHash: envelopeHash, MeasurementEnvelopeSize: uint64(len(envelopeBytes)),
			MaskedUIDs: cycle.MaskedUIDs, EligibleHeadUIDs: eligibleUIDs, EligibleHeadScores: eligibleScores,
			SelectedHeadUIDs: selectedUIDs, RejectedHeadUIDs: rejectedUIDs, DepositAudits: audits,
			UIDs: valueUIDs, Scores: intentScores, Prepared: prepared,
		}
		vectorHash, err := intent.ReconstructedVectorHash()
		if err != nil {
			t.Fatal(err)
		}
		intent.VectorHash, intent.Status, intent.Values = vectorHash, "applied", values
		intent.ExtrinsicHash, intent.FinalizedBlock, intent.FinalizedBlockHash = cycle.Commit.ExtrinsicHash, cycle.Commit.Block.Number, cycle.Commit.Block.Hash
		intent.RevealBlock, intent.ApplicationBlock, intent.ApplicationBlockHash = cycle.Reveal.Block.Number, cycle.Application.Block.Number, cycle.Application.Block.Hash
		intentBytes, err := json.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		cycle.IntentVectorHash = vectorHash
		cycle.IntentArtifact = artifact("steering-intent", fmt.Sprintf("steering-intent-%d-%d.json", validatorID, cycle.SettlementEpoch), intentBytes)
		return cycle
	}
	cloneCycle := func(source FinalCRv4Cycle) FinalCRv4Cycle {
		wire, marshalErr := json.Marshal(source)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var clone FinalCRv4Cycle
		if unmarshalErr := json.Unmarshal(wire, &clone); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		return clone
	}
	cycle11 := cloneCycle(cycle)
	cycle11.SettlementEpoch, cycle11.SubnetEpoch = 11, 8
	cycle11.EVMSnapshot = ChainHead{Number: 401, Hash: finalTestHex(145)}
	cycle11.Commit, cycle11.Reveal, cycle11.Application = nativeReceipt("commit-1-11", 26, true), nativeReceipt("reveal-1-11", 27, false), nativeReceipt("application-1-11", 28, false)
	for i := range cycle11.Pools {
		cycle11.Pools[i].SourceEpoch = 10
		cycle11.Pools[i].PayoutArtifact = artifact("payout-artifact", fmt.Sprintf("deposit-payout-%d-10.json", i+1), []byte(fmt.Sprintf("deposit payout %d epoch 10", i+1)))
		cycle11.Pools[i].DepositReceipt = evmReceipt(fmt.Sprintf("deposit-%d-11", i+1), uint64(390+2*i))
	}
	cycle12 := cloneCycle(cycle11)
	cycle12.SettlementEpoch, cycle12.SubnetEpoch = 12, 9
	cycle12.EVMSnapshot = ChainHead{Number: 701, Hash: finalTestHex(189)}
	cycle12.Commit, cycle12.Reveal, cycle12.Application = nativeReceipt("commit-1-12", 32, true), nativeReceipt("reveal-1-12", 33, false), nativeReceipt("application-1-12", 34, false)
	cycle13 := cloneCycle(cycle12)
	cycle13.SettlementEpoch, cycle13.SubnetEpoch = 13, 10
	cycle13.EVMSnapshot = ChainHead{Number: 1001, Hash: finalTestHex(233)}
	cycle13.Commit, cycle13.Reveal, cycle13.Application = nativeReceipt("commit-1-13", 38, true), nativeReceipt("reveal-1-13", 39, false), nativeReceipt("application-1-13", 40, false)
	cycle14 := cloneCycle(cycle13)
	cycle14.SettlementEpoch, cycle14.SubnetEpoch = 14, 11
	cycle14.EVMSnapshot = ChainHead{Number: 1301, Hash: finalTestHex(21)}
	cycle14.Commit, cycle14.Reveal, cycle14.Application = nativeReceipt("commit-1-14", 44, true), nativeReceipt("reveal-1-14", 45, false), nativeReceipt("application-1-14", 46, false)
	for index, candidate := range []*FinalCRv4Cycle{&cycle12, &cycle13, &cycle14} {
		candidateEpoch := uint64(12 + index)
		for poolIndex := range candidate.Pools {
			candidate.Pools[poolIndex].SourceEpoch = candidateEpoch - 1
			candidate.Pools[poolIndex].DepositReceipt = evmReceipt(fmt.Sprintf("deposit-%d-%d", poolIndex+1, candidateEpoch), candidate.EVMSnapshot.Number-11+uint64(2*poolIndex))
		}
	}
	cycle2 := cloneCycle(cycle)
	cycle2.Commit, cycle2.Reveal, cycle2.Application = nativeReceipt("commit-2", 23, true), nativeReceipt("reveal-2", 24, false), nativeReceipt("application-2", 25, false)
	cycle211 := cloneCycle(cycle11)
	cycle211.Commit, cycle211.Reveal, cycle211.Application = nativeReceipt("commit-2-11", 29, true), nativeReceipt("reveal-2-11", 30, false), nativeReceipt("application-2-11", 31, false)
	cycle212 := cloneCycle(cycle12)
	cycle212.Commit, cycle212.Reveal, cycle212.Application = nativeReceipt("commit-2-12", 35, true), nativeReceipt("reveal-2-12", 36, false), nativeReceipt("application-2-12", 37, false)
	cycle213 := cloneCycle(cycle13)
	cycle213.Commit, cycle213.Reveal, cycle213.Application = nativeReceipt("commit-2-13", 41, true), nativeReceipt("reveal-2-13", 42, false), nativeReceipt("application-2-13", 43, false)
	cycle214 := cloneCycle(cycle14)
	cycle214.Commit, cycle214.Reveal, cycle214.Application = nativeReceipt("commit-2-14", 47, true), nativeReceipt("reveal-2-14", 48, false), nativeReceipt("application-2-14", 49, false)

	miners := make([]FinalMinerProcessEvidence, 1000)
	bindings := make([]FinalFleetMemberBindingEvidence, 1000)
	for i := range miners {
		minerID := uint64(i + 1)
		clientID := minerClientID(minerID)
		miners[i] = FinalMinerProcessEvidence{MinerID: minerID, ProcessID: fmt.Sprintf("miner-swarm-%d", i%finalMinerSwarmProcessCount+1), ProcessGeneration: 77, ClientID: clientID.String(), ProviderID: fmt.Sprintf("provider-%d", minerID), SDKSourceHash: finalTestHex(81), Running: true}
		binding := FinalFleetMemberBindingEvidence{MinerID: minerID, NoID: uint64(operatorForMiner(cfg, int(minerID))), ClientID: miners[i].ClientID, ProviderID: miners[i].ProviderID}
		if i < finalHeadCandidateCount*4 {
			binding.Tier = "head-candidate"
			binding.FleetID = uint64(i/4 + 1)
			binding.HeadUID = uint16(999 + binding.FleetID)
			if binding.FleetID == fleetLifecycleTargetFleet {
				binding.HeadUID = fleetLifecycleCompanionExpectedUID
			} else if binding.FleetID == fleetLifecycleCompanionFleet {
				binding.HeadUID = fleetLifecycleTerminalVictimUID
			}
			binding.Generation = 1
			binding.BindingActive = true
		} else {
			binding.Tier = "pool-tail"
		}
		bindings[i] = binding
	}
	minerBytes, err := json.Marshal(miners)
	if err != nil {
		t.Fatal(err)
	}
	bindingBytes, err := json.Marshal(bindings)
	if err != nil {
		t.Fatal(err)
	}
	minerLocator := artifact("miner-process-manifest", "miners.json", minerBytes)
	bindingLocator := artifact("fleet-binding-manifest", "fleet-bindings.json", bindingBytes)
	topology := FinalTopologyEvidence{MinerSDKInstances: 1000, MinerSwarmProcesses: finalMinerSwarmProcessCount, HeadCandidateFleets: 202, HeadSlots: 200, ValidatorProcesses: 2, OperatorPools: 2, MinerManifestHash: minerLocator.ContentHash, MinerManifest: minerLocator, BindingManifestHash: bindingLocator.ContentHash, BindingManifest: bindingLocator}
	coordinatorAddress := common.HexToAddress("0x1111111111111111111111111111111111111111")
	vaultAddress := common.HexToAddress("0x3333333333333333333333333333333333333333")
	payoutKeys := make([]*ecdsa.PrivateKey, 0, 2)
	for i := 0; i < 2; i++ {
		key, keyErr := crypto.ToECDSA(bytes.Repeat([]byte{byte(i + 1)}, 32))
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		payoutKeys = append(payoutKeys, key)
	}
	type fixturePayout struct {
		locator  FinalArtifactLocator
		artifact *payoutartifact.Artifact
	}
	minerSet := func(miners ...int) map[int]bool {
		result := make(map[int]bool, len(miners))
		for _, miner := range miners {
			result[miner] = true
		}
		return result
	}
	targetMiners := make([]int, 0, cfg.Config.Topology.ClientsPerHeadFleet)
	companionMiners := make([]int, 0, cfg.Config.Topology.ClientsPerHeadFleet)
	fallbackMiners := make([]int, 0, cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		targetMiners = append(targetMiners, fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet, member))
		companionMiners = append(companionMiners, fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet, member))
		fallbackMiner, fallbackErr := fleetLifecycleFallbackMinerIndex(cfg, member)
		if fallbackErr != nil {
			t.Fatal(fallbackErr)
		}
		fallbackMiners = append(fallbackMiners, fallbackMiner)
	}
	targetSet := minerSet(targetMiners...)
	companionSet := minerSet(companionMiners...)
	fallbackSet := minerSet(fallbackMiners...)
	tierAt := func(miner int, epoch uint64) string {
		tier := bindings[miner-1].Tier
		switch {
		case targetSet[miner] && epoch >= 11 && epoch < 13:
			return "pool-tail"
		case fallbackSet[miner] && epoch >= 11 && epoch < 15:
			return "head-candidate"
		case companionSet[miner] && epoch >= 13 && epoch < 15:
			return "pool-tail"
		default:
			return tier
		}
	}
	payouts := map[string]fixturePayout{}
	for epoch := uint64(9); epoch <= 14; epoch++ {
		for noID := 1; noID <= 2; noID++ {
			providerMiners := map[int]bool{}
			addMiner := func(miner int) {
				if operatorForMiner(cfg, miner) == noID {
					providerMiners[miner] = true
				}
			}
			// One ordinary head and one ordinary tail keep every artifact
			// representative even outside the lifecycle transition epochs.
			addMiner(fleetMemberMinerIndex(cfg, noID, 1))
			addMiner(cfg.Config.Topology.fleetCandidateMiners() + cfg.Config.Topology.Operators*cfg.Config.Topology.ClientsPerHeadFleet + noID)
			for _, miner := range targetMiners {
				addMiner(miner)
			}
			for _, miner := range companionMiners {
				addMiner(miner)
			}
			for _, miner := range fallbackMiners {
				addMiner(miner)
			}
			orderedMiners := make([]int, 0, len(providerMiners))
			for miner := range providerMiners {
				orderedMiners = append(orderedMiners, miner)
			}
			sort.Ints(orderedMiners)
			poolCount := 0
			for _, miner := range orderedMiners {
				if tierAt(miner, epoch) == "pool-tail" {
					poolCount++
				}
			}
			if poolCount == 0 {
				t.Fatalf("fixture payout epoch %d operator %d has no pool provider", epoch, noID)
			}
			const fixtureUsageBytes = uint64(1 << 30)
			baseUsage := fixtureUsageBytes / uint64(poolCount)
			usageRemainder := fixtureUsageBytes % uint64(poolCount)
			poolIndex := uint64(0)
			providers := make([]payoutartifact.ProviderInput, 0, len(orderedMiners))
			for _, miner := range orderedMiners {
				clientID, parseErr := connect.ParseId(bindings[miner-1].ClientID)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				var network [16]byte
				var coldkey [32]byte
				network[0], network[14], network[15] = 3, byte(miner>>8), byte(miner)
				coldkey[0], coldkey[30], coldkey[31] = 5, byte(miner>>8), byte(miner)
				headExcluded := tierAt(miner, epoch) == "head-candidate"
				usage := uint64(0)
				if !headExcluded {
					usage = baseUsage
					if poolIndex < usageRemainder {
						usage++
					}
					poolIndex++
				}
				provider := payoutartifact.ProviderInput{
					ClientID: [16]byte(clientID), NetworkID: network, Coldkey: coldkey, UsageBytes: usage,
					Assignments: policy.Verify.ReliabilityAMin, Confirmations: policy.Verify.ReliabilityAMin,
					Eligible: true, HeadExcluded: headExcluded, BindingGeneration: 1,
				}
				if headExcluded {
					provider.ExclusionReason = "head_candidate"
				}
				providers = append(providers, provider)
			}
			built, buildErr := payoutartifact.Build(payoutartifact.BuildInput{
				DeploymentID: "ur-subnet-testnet-v1-attempt-4", GenesisHash: finalTestHex(5), PolicyHash: policyHash, ChainID: 945, Netuid: 521,
				Coordinator: coordinatorAddress, SettlementVault: vaultAddress, Epoch: epoch, NoID: uint64(noID),
				Start:                payoutartifact.Boundary{Number: 70 + (epoch-9)*finalReleaseEpochBlocks, Hash: finalTestHex(byte(70 + (epoch-9)*finalReleaseEpochBlocks))},
				End:                  payoutartifact.Boundary{Number: 79 + (epoch-9)*finalReleaseEpochBlocks, Hash: finalTestHex(byte(79 + (epoch-9)*finalReleaseEpochBlocks))},
				OperatorSnapshotHash: bytesSHA256([]byte{byte(80 + noID)}), FleetSnapshotHash: bytesSHA256([]byte{byte(90 + noID)}), ReliabilityAMin: policy.Verify.ReliabilityAMin,
				Providers: providers,
				CreatedAt: time.Unix(1_700_000_000+int64(epoch*10+uint64(noID)), 0).UTC(),
			})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if signErr := payoutartifact.Sign(built, payoutKeys[noID-1]); signErr != nil {
				t.Fatal(signErr)
			}
			data, bytesErr := payoutartifact.Bytes(built)
			if bytesErr != nil {
				t.Fatal(bytesErr)
			}
			key := fmt.Sprintf("%d/%d", epoch, noID)
			payouts[key] = fixturePayout{locator: artifact("payout-artifact", fmt.Sprintf("payout-%d-%d.json", epoch, noID), data), artifact: built}
		}
	}
	applyCyclePayouts := func(value *FinalCRv4Cycle) {
		for i := range value.Pools {
			payout := payouts[fmt.Sprintf("%d/%d", value.Pools[i].SourceEpoch, value.Pools[i].NoID)]
			value.Pools[i].PayoutArtifact = payout.locator
			value.Pools[i].ArtifactContentHash = payout.artifact.ContentHash
			value.Pools[i].ArtifactHash = "0x" + strings.TrimPrefix(payout.artifact.ContentHash, "sha256:")
			value.Pools[i].PayoutRoot = "0x" + hex.EncodeToString(payout.artifact.PayoutRoot[:])
			authority := strings.ToLower(payout.artifact.Signer.Hex())
			value.Pools[i].ArtifactSigner = authority
			value.Pools[i].RootCommitter = authority
			value.Pools[i].RootSigner = authority
			value.Pools[i].SourceStartBlock = payout.artifact.Start.Number
			value.Pools[i].SourceStartHash = payout.artifact.Start.Hash
			value.Pools[i].SourceEndBlock = payout.artifact.End.Number
			value.Pools[i].SourceEndHash = payout.artifact.End.Hash
			value.Pools[i].RootCommitBlock = payout.artifact.End.Number + 1
			value.Pools[i].ObservedAtBlock = value.EVMSnapshot.Number
			value.Pools[i].ArtifactDeadlineBlock = value.EVMSnapshot.Number + 8
		}
	}
	applyLifecycleCandidateUIDs := func(value *FinalCRv4Cycle) {
		for index := range value.Candidates {
			value.Candidates[index].RawScore = FinalRational{Numerator: "1", Denominator: "1"}
			value.Candidates[index].Selected = true
			switch value.Candidates[index].FleetID {
			case fleetLifecycleTargetFleet:
				value.Candidates[index].UID = fleetLifecycleTargetExpectedUID
				value.Candidates[index].RawScore = FinalRational{Numerator: "1", Denominator: "2"}
				value.Candidates[index].Selected = false
				if value.SettlementEpoch >= 13 {
					value.Candidates[index].UID = fleetLifecycleCompanionExpectedUID
				}
			case fleetLifecycleCompanionFleet:
				value.Candidates[index].UID = fleetLifecycleCompanionExpectedUID
				value.Candidates[index].RawScore = FinalRational{Numerator: "1", Denominator: "2"}
				value.Candidates[index].Selected = false
				if value.SettlementEpoch >= 13 {
					value.Candidates[index].UID = fleetLifecycleTargetExpectedUID
				}
			}
		}
		sort.Slice(value.Candidates, func(i, j int) bool {
			left, _ := finalPositiveRational("fixture candidate score", value.Candidates[i].RawScore)
			right, _ := finalPositiveRational("fixture candidate score", value.Candidates[j].RawScore)
			if comparison := left.Cmp(right); comparison != 0 {
				return comparison > 0
			}
			return value.Candidates[i].UID < value.Candidates[j].UID
		})
		for index := range value.Candidates {
			value.Candidates[index].Rank = uint16(index + 1)
			value.Candidates[index].Selected = index < finalHeadSlotCount
		}
	}
	for _, value := range []*FinalCRv4Cycle{&cycle, &cycle11, &cycle12, &cycle13, &cycle14, &cycle2, &cycle211, &cycle212, &cycle213, &cycle214} {
		applyLifecycleCandidateUIDs(value)
	}
	// Validator 1 has one exact local-view substitution in epoch 10. Three
	// fleets share two prefixes so fleet 5 wins the final slot by UID; after
	// one ordinary raw window, the exact EMA restores fleet 200 in epoch 11.
	for index := range cycle.Candidates {
		switch cycle.Candidates[index].FleetID {
		case 5, 6, 200:
			cycle.Candidates[index].RawScore = FinalRational{Numerator: "2", Denominator: "3"}
		}
	}
	applyCyclePayouts(&cycle)
	applyCyclePayouts(&cycle11)
	applyCyclePayouts(&cycle12)
	applyCyclePayouts(&cycle13)
	applyCyclePayouts(&cycle14)
	applyCyclePayouts(&cycle2)
	applyCyclePayouts(&cycle211)
	applyCyclePayouts(&cycle212)
	applyCyclePayouts(&cycle213)
	applyCyclePayouts(&cycle214)
	cycle = sealCycle(cycle, 1)
	cycle11 = sealCycle(cycle11, 1)
	cycle12 = sealCycle(cycle12, 1)
	cycle13 = sealCycle(cycle13, 1)
	cycle14 = sealCycle(cycle14, 1)
	cycle2 = sealCycle(cycle2, 2)
	cycle211 = sealCycle(cycle211, 2)
	cycle212 = sealCycle(cycle212, 2)
	cycle213 = sealCycle(cycle213, 2)
	cycle214 = sealCycle(cycle214, 2)
	headFleets := make([]FinalHeadFleetEvidence, 0, finalHeadCandidateCount)
	for i := 0; i < finalHeadCandidateCount; i++ {
		fleetID := uint64(i + 1)
		uid := uint16(1000 + i)
		if fleetID == fleetLifecycleTargetFleet {
			uid = fleetLifecycleCompanionExpectedUID
		} else if fleetID == fleetLifecycleCompanionFleet {
			uid = fleetLifecycleTerminalVictimUID
		}
		manifest := fixtureFleetManifest(fleetID)
		hotkeyBytes := manifest.Hotkey
		hotkey, encodeErr := ss58.Encode(hotkeyBytes, ss58.BittensorPrefix)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		manifestBytes, manifestErr := manifest.Canonical()
		if manifestErr != nil {
			t.Fatal(manifestErr)
		}
		bindingArtifactBytes, marshalErr := json.Marshal(map[string]any{"manifest": json.RawMessage(manifestBytes), "uid": uid, "snapshot": ChainHead{Number: 100, Hash: finalTestHex(100)}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		headFleets = append(headFleets, FinalHeadFleetEvidence{
			FleetID: fleetID, UID: uid, Hotkey: hotkey, Coldkey: ss58Key(0x32, i+1),
			Generation: 1, MemberCount: 4, Registered: true, Registration: nativeReceipt(fmt.Sprintf("head-registration-%d", fleetID), uint64(30+i%50), true),
			Snapshot: ChainHead{Number: 100, Hash: finalTestHex(100)}, BindingArtifact: artifact("head-fleet-binding", fmt.Sprintf("head-fleet-%d.json", fleetID), bindingArtifactBytes),
		})
	}
	headTransitions := make([]FinalHeadTournamentTransition, 0, 2)
	for offset := 0; offset < 2; offset++ {
		fleet := &headFleets[finalHeadSlotCount+offset]
		fleetID := fleet.FleetID
		promotedHotkey, promotedColdkey, identityErr := finalSemanticSS58Pair("fixture challenger", fleet.Hotkey, fleet.Coldkey)
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		prunedChurn := uint64(offset + 1)
		prunedSS58 := ss58Key(0x33, int(fleetID))
		prunedKey, prunedPrefix, prunedErr := ss58.Decode(prunedSS58)
		if prunedErr != nil || prunedPrefix != ss58.BittensorPrefix {
			t.Fatal(prunedErr)
		}
		nativeSnapshot := ChainHead{Number: uint64(90 + offset), Hash: finalTestHex(byte(90 + offset))}
		evmSnapshot := ChainHead{Number: uint64(96 + offset), Hash: finalTestHex(byte(96 + offset))}
		observed := map[string]any{
			"role": fmt.Sprintf("fleet-%d-hotkey", fleetID), "hotkey": "0x" + hex.EncodeToString(promotedHotkey[:]),
			"coldkey": "0x" + hex.EncodeToString(promotedColdkey[:]), "uid": uint64(fleet.UID),
			"replaced_churn": prunedChurn, "replaced_uid": uint64(fleet.UID), "uid_count": uint64(1300),
		}
		postcondition := &ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: "ur-subnet-testnet-v1-attempt-4", PlanHash: finalTestHex(2),
			ActionID: fmt.Sprintf("fleet.register.%d", fleetID), IntentHash: finalTestHex(byte(160 + offset)),
			OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true,
			SubstrateFinalized: nativeSnapshot, EVMFinalized: evmSnapshot, EVMHashDomain: "evm-rpc", Observed: observed,
			IndependentSubstrateFinalized: nativeSnapshot, IndependentEVMFinalized: evmSnapshot,
			IndependentEVMHashDomain: "evm-rpc", IndependentObserved: observed,
		}
		postconditionBytes, marshalErr := json.Marshal(postcondition)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		fleet.Registration.Proof = artifact("native-receipt", fmt.Sprintf("challenger-registration-%d.json", fleetID), postconditionBytes)
		transitionArtifact := finalHeadTournamentTransitionArtifact{
			Postcondition: postcondition,
			Pruned:        finalHeadTournamentIdentity{Role: fmt.Sprintf("churn-%d-hotkey", prunedChurn), PublicKey: "0x" + hex.EncodeToString(prunedKey[:]), SS58: prunedSS58},
		}
		transitionBytes, marshalErr := json.Marshal(transitionArtifact)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		headTransitions = append(headTransitions, FinalHeadTournamentTransition{
			ChallengerFleetID: fleetID, PromotedUID: fleet.UID, PromotedHotkey: fleet.Hotkey,
			PrunedUID: fleet.UID, PrunedChurn: prunedChurn, PrunedHotkey: prunedSS58,
			OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true, Registration: fleet.Registration,
			Snapshot: nativeSnapshot, IndependentSnapshot: nativeSnapshot, EVMSnapshot: evmSnapshot, IndependentEVMSnapshot: evmSnapshot,
			Artifact: artifact("head-tournament-transition", fmt.Sprintf("head-transition-%d.json", fleetID), transitionBytes),
		})
	}
	implementation := "0x2222222222222222222222222222222222222222"
	reserveAddress := common.HexToAddress("0x4444444444444444444444444444444444444444")
	coordinatorMirror := ss58.EvmMirrorPubkey(coordinatorAddress)
	contractVaultMirror := ss58.EvmMirrorPubkey(vaultAddress)
	reserveMirror := ss58.EvmMirrorPubkey(reserveAddress)
	minimumTTL, ok := checkedMul(policy.Settlement.EpochBlocks, policy.Settlement.ClaimTTLEpochs)
	if !ok || minimumTTL == 0 {
		t.Fatal("fixture settlement TTL overflow")
	}
	deployment := FinalContractDeploymentEvidence{
		CoordinatorProxy: strings.ToLower(coordinatorAddress.Hex()), CoordinatorImplementation: implementation,
		SettlementVault: strings.ToLower(vaultAddress.Hex()), ReserveSink: strings.ToLower(reserveAddress.Hex()), GovernanceOwner: "0x5555555555555555555555555555555555555555",
		CoordinatorNetuid: 521, CoordinatorSelfColdkey: strings.ToLower(fmt.Sprintf("0x%x", coordinatorMirror[:])),
		CoordinatorSettlementVault: strings.ToLower(vaultAddress.Hex()), CoordinatorReserveSink: strings.ToLower(reserveAddress.Hex()),
		CoordinatorGuardian: "0x6666666666666666666666666666666666666666", CoordinatorActiveGuardian: "0x6666666666666666666666666666666666666666",
		CoordinatorCommitmentOracle: "0x7777777777777777777777777777777777777777", CoordinatorActiveCommitmentOracle: "0x7777777777777777777777777777777777777777",
		VaultCoordinator: strings.ToLower(coordinatorAddress.Hex()), VaultNetuid: 521, VaultSelfColdkey: strings.ToLower(fmt.Sprintf("0x%x", contractVaultMirror[:])),
		VaultEscrowHotkey: finalTestHex(0x81), VaultEscrowRegistered: true, VaultMinimumClaimTTLBlocks: minimumTTL,
		VaultMinimumTransferTaoRao: 1_000, PlanDefaultMinTransferTaoRao: 1_000,
		ReserveRecorder: strings.ToLower(coordinatorAddress.Hex()), ReserveNetuid: 521, ReserveSelfColdkey: strings.ToLower(fmt.Sprintf("0x%x", reserveMirror[:])), ReserveHotkey: finalTestHex(0x82),
		CoordinatorProxyCodeHash: finalTestHex(51), ImplementationCodeHash: finalTestHex(52), SettlementVaultCodeHash: finalTestHex(53), ReserveSinkCodeHash: finalTestHex(54),
		ERC1967ImplementationSlot: "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", ObservedImplementationSlot: "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(implementation, "0x"),
		PolicyVersion: 1, PolicyEffectiveEpoch: 10, PolicyEffectiveBlock: 100, Snapshot: ChainHead{Number: 1750, Hash: finalTestHex(214)}, Artifact: artifact("contract-deployment", "contract-deployment.json", []byte("contract deployment")),
	}
	reserveAdditionReceipt := evmReceipt("reserve-principal-added", 110)
	reserve := FinalReserveEvidence{
		PrincipalBeforeRao: "100", PrincipalAfterRao: "120", PrincipalDeltaRao: "20", PrincipalAddedRao: "20",
		LiveStakeBeforeRao: "110", LiveStakeAfterRao: "140", Before: ChainHead{Number: 99, Hash: finalTestHex(99)}, After: ChainHead{Number: 1750, Hash: finalTestHex(214)},
		PrincipalAdditions: []FinalReservePrincipalAddedEvidence{{Epoch: 10, NoID: 1, AmountRao: "20", OperatorPrincipalRao: "20", TotalPrincipalRao: "120", LiveStakeRao: "140", Receipt: reserveAdditionReceipt}},
		Artifact:           artifact("reserve-state", "reserve.json", []byte("reserve state")),
	}
	settlementAccounting := FinalSettlementVaultAccounting{
		Before:                FinalSettlementVaultState{TotalCapturedRao: "100", TotalPaidRao: "50", EscrowAccountedRao: "50", PendingFundingRao: "0", OutstandingLiabilityRao: "50", LiveEscrowStakeRao: "60", Block: ChainHead{Number: 99, Hash: finalTestHex(99)}},
		After:                 FinalSettlementVaultState{TotalCapturedRao: "10100", TotalPaidRao: "10050", EscrowAccountedRao: "50", PendingFundingRao: "0", OutstandingLiabilityRao: "50", LiveEscrowStakeRao: "70", Block: ChainHead{Number: 1750, Hash: finalTestHex(214)}},
		TotalCapturedDeltaRao: "10000", TotalPaidDeltaRao: "10000", EscrowAccountedDeltaRao: "0", PendingFundingDeltaRao: "0",
		OutstandingLiabilityDeltaRao: "0", LiveEscrowStakeDeltaRao: "10", EmissionCapturedEventRao: "10000", ClaimPaidEventRao: "10000",
	}
	deploymentArtifactBytes, err := json.Marshal(map[string]any{
		"deployment": ContractDeployment{
			DeploymentID: "ur-subnet-testnet-v1-attempt-4", CoordinatorProxy: coordinatorAddress,
			CoordinatorImplementation: common.HexToAddress(implementation), SettlementVault: vaultAddress, ReserveSink: reserveAddress,
		},
		"upgrade": CoordinatorUpgrade{}, "terminal": deployment.Snapshot, "runtime_code_hashes": map[string]string{}, "policy": PolicyView{},
		"custody": ContractCustodyView{
			CoordinatorNetuid: deployment.CoordinatorNetuid, CoordinatorSelfColdkey: deployment.CoordinatorSelfColdkey,
			CoordinatorVault: deployment.CoordinatorSettlementVault, CoordinatorReserve: deployment.CoordinatorReserveSink,
			CoordinatorGuardian: deployment.CoordinatorGuardian, CoordinatorActiveGuardian: deployment.CoordinatorActiveGuardian,
			CoordinatorPaused: deployment.CoordinatorPaused, CoordinatorCommitmentOracle: deployment.CoordinatorCommitmentOracle,
			CoordinatorActiveCommitmentOracle: deployment.CoordinatorActiveCommitmentOracle,
			VaultCoordinator:                  deployment.VaultCoordinator, VaultNetuid: deployment.VaultNetuid, VaultSelfColdkey: deployment.VaultSelfColdkey,
			VaultEscrowHotkey: deployment.VaultEscrowHotkey, VaultEscrowRegistered: deployment.VaultEscrowRegistered,
			VaultMinimumClaimTTLBlocks: deployment.VaultMinimumClaimTTLBlocks, VaultMinimumTransferRao: deployment.VaultMinimumTransferTaoRao,
			ReserveRecorder: deployment.ReserveRecorder, ReserveNetuid: deployment.ReserveNetuid,
			ReserveSelfColdkey: deployment.ReserveSelfColdkey, ReserveHotkey: deployment.ReserveHotkey,
		},
		"plan_hash":                     finalTestHex(2),
		"plan_default_min_transfer_rao": deployment.PlanDefaultMinTransferTaoRao,
		"expected_guardian":             deployment.CoordinatorGuardian, "expected_commitment_oracle": deployment.CoordinatorCommitmentOracle,
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment.Artifact = artifact("contract-deployment", "contract-deployment.json", deploymentArtifactBytes)
	reserveBefore := &ContractView{FinalizedHead: reserve.Before, TotalCaptured: settlementAccounting.Before.TotalCapturedRao, TotalPaid: settlementAccounting.Before.TotalPaidRao, EscrowAccounted: settlementAccounting.Before.EscrowAccountedRao, PendingFunding: settlementAccounting.Before.PendingFundingRao, Outstanding: settlementAccounting.Before.OutstandingLiabilityRao, LiveEscrowStake: settlementAccounting.Before.LiveEscrowStakeRao, ReservePrincipal: reserve.PrincipalBeforeRao, ReserveLiveStake: reserve.LiveStakeBeforeRao}
	reserveAfter := &ContractView{FinalizedHead: reserve.After, TotalCaptured: settlementAccounting.After.TotalCapturedRao, TotalPaid: settlementAccounting.After.TotalPaidRao, EscrowAccounted: settlementAccounting.After.EscrowAccountedRao, PendingFunding: settlementAccounting.After.PendingFundingRao, Outstanding: settlementAccounting.After.OutstandingLiabilityRao, LiveEscrowStake: settlementAccounting.After.LiveEscrowStakeRao, ReservePrincipal: reserve.PrincipalAfterRao, ReserveLiveStake: reserve.LiveStakeAfterRao}
	reserveArtifactBytes, err := json.Marshal(map[string]any{"before": reserveBefore, "after": reserveAfter, "settlement_accounting": settlementAccounting, "principal_additions": reserve.PrincipalAdditions})
	if err != nil {
		t.Fatal(err)
	}
	reserve.Artifact = artifact("reserve-state", "reserve.json", reserveArtifactBytes)
	exitAssertions := map[string]map[string]uint64{
		"all-miner-tier-assignments":    {"active_fleet_member_bindings": 808, "miner_tier_assignments": 1000, "pool_tail_assignments": 192},
		"deposit-conviction-receipts":   {"operator_conviction_receipts": 2, "operator_epoch_deposit_audits": 20},
		"dishonest-deposit-recovery":    {"dishonest_underpayments_succeeded": 1, "recovery_topups_succeeded": 1},
		"invalid-merkle-proof-rejected": {"invalid_merkle_attempts_rejected": 1},
		"no-process-log-anomalies":      {"error_warning_panic_restart_anomalies": 0},
		"payout-double-claim-rejected":  {"double_claim_attempts_rejected": 1},
		"reserve-one-way-backed":        {"reserve_backing_violations": 0},
		"theta-head-tail-realized":      {"verified_theta_weight_vectors": 10},
		"unauthorized-upgrade-rejected": {"unauthorized_upgrade_attempts_rejected": 1},
	}
	requiredExitCriteria := finalRequiredExitCriteriaForPhase("release-1.0")
	exitCriteria := make([]FinalExitCriterionEvidence, len(requiredExitCriteria))
	for i, id := range requiredExitCriteria {
		criterion := FinalExitCriterionEvidence{ID: id, Expected: "release invariant holds", Observed: "release invariant observed", Passed: true, Checkpoint: ChainHead{Number: 1750, Hash: finalTestHex(214)}, Artifacts: []FinalArtifactLocator{artifact("exit-criterion", "exit-"+id+".json", []byte(id))}}
		for metric, value := range exitAssertions[id] {
			criterion.Assertions = append(criterion.Assertions, FinalMetricAssertion{Metric: metric, Expected: value, Observed: value})
		}
		failedReceipt := func(name string) FinalEVMReceipt {
			receipt := evmReceipt(name, 111)
			receipt.Status = "failed"
			return receipt
		}
		switch id {
		case "dishonest-deposit-recovery":
			criterion.EVMReceipts = []FinalEVMReceipt{evmReceipt("dishonest-deposit-underpayment", 111), evmReceipt("dishonest-deposit-recovery", 112)}
		case "invalid-merkle-proof-rejected":
			criterion.EVMReceipts = []FinalEVMReceipt{failedReceipt("invalid-merkle-proof")}
		case "payout-double-claim-rejected":
			criterion.EVMReceipts = []FinalEVMReceipt{failedReceipt("payout-double-claim")}
		case "unauthorized-upgrade-rejected":
			criterion.EVMReceipts = []FinalEVMReceipt{failedReceipt("unauthorized-upgrade")}
		}
		exitCriteria[i] = criterion
	}
	pools := make([]FinalPoolUIDEvidence, 2)
	vaultMirror := ss58.EvmMirrorPubkey(common.HexToAddress(deployment.SettlementVault))
	vaultColdkey, err := ss58.Encode(vaultMirror, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	for i, uid := range []uint16{11, 13} {
		pools[i] = FinalPoolUIDEvidence{
			NoID: uint64(i + 1), UID: uid, Hotkey: ss58Key(0x41, i+1), Coldkey: vaultColdkey, OperatorColdkey: ss58Key(0x42, i+1), Registered: true,
			Registration: evmReceipt(fmt.Sprintf("pool-registration-%d", i+1), uint64(5+2*i)), Snapshot: ChainHead{Number: 100, Hash: finalTestHex(100)}, FinalCarryRao: "0",
			DepositHotkey: fmt.Sprintf("5DepositHotkey%d", i+1), DepositSigner: fmt.Sprintf("0x%040x", 0x60+i), PayoutRootSigner: strings.ToLower(crypto.PubkeyToAddress(payoutKeys[i].PublicKey).Hex()),
			ConvictionReceipt: evmReceipt(fmt.Sprintf("conviction-%d", i+1), uint64(91+2*i)),
			EffectiveEpoch:    10, VersionCount: 1, Active: true, ServerKeyHistory: []FinalServerKey{{KeyID: 1, PublicKey: "0x" + hex.EncodeToString(operatorServerKeys[i].Public().(ed25519.PublicKey))}},
			OwnershipArtifact: artifact("native-ownership", fmt.Sprintf("pool-ownership-%d.json", i+1), []byte(fmt.Sprintf("pool ownership %d", i+1))),
		}
		hotkey, coldkey, identityErr := finalSemanticSS58Pair("fixture pool ownership", pools[i].Hotkey, pools[i].Coldkey)
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		state := FinalCollectedNativeUIDState{
			UID: uid, HotkeyPublicKey: "0x" + hex.EncodeToString(hotkey[:]), ColdkeyPublicKey: "0x" + hex.EncodeToString(coldkey[:]),
			RegistrationBlock: pools[i].Registration.Block.Number,
		}
		ownershipBytes, marshalErr := json.Marshal(map[string]any{
			"snapshot": pools[i].Snapshot, "state": state, "settlement_vault": deployment.SettlementVault,
			"vault_mirror_coldkey": pools[i].Coldkey, "operator_registry_coldkey": pools[i].OperatorColdkey,
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		pools[i].OwnershipArtifact = artifact("native-ownership", fmt.Sprintf("pool-ownership-%d.json", i+1), ownershipBytes)
	}
	validators := []FinalValidatorIdentityEvidence{
		{ValidatorID: 1, UID: 12, Hotkey: validatorHotkeys[0].Address(), Coldkey: ss58Key(0x51, 1), Registered: true, Registration: nativeReceipt("validator-registration-1", 6, true), StakeRao: "1000000", ValidatorPermit: true, ValidatorTrustU16: 42, PathVPK: "0x" + hex.EncodeToString(validatorPathKeys[0].Public().(ed25519.PublicKey)), Snapshot: ChainHead{Number: 100, Hash: finalTestHex(100)}, SnapshotArtifact: artifact("native-validator-state", "validator-state-1.json", []byte("validator state 1")), Cycles: []FinalCRv4Cycle{cycle, cycle11, cycle12, cycle13, cycle14}},
		{ValidatorID: 2, UID: 14, Hotkey: validatorHotkeys[1].Address(), Coldkey: ss58Key(0x51, 2), Registered: true, Registration: nativeReceipt("validator-registration-2", 8, true), StakeRao: "1000000", ValidatorPermit: true, ValidatorTrustU16: 43, PathVPK: "0x" + hex.EncodeToString(validatorPathKeys[1].Public().(ed25519.PublicKey)), Snapshot: ChainHead{Number: 100, Hash: finalTestHex(100)}, SnapshotArtifact: artifact("native-validator-state", "validator-state-2.json", []byte("validator state 2")), Cycles: []FinalCRv4Cycle{cycle2, cycle211, cycle212, cycle213, cycle214}},
	}
	for index := range validators {
		validator := &validators[index]
		hotkey, coldkey, identityErr := finalSemanticSS58Pair("fixture validator state", validator.Hotkey, validator.Coldkey)
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		state := FinalCollectedNativeUIDState{
			UID: validator.UID, HotkeyPublicKey: "0x" + hex.EncodeToString(hotkey[:]), ColdkeyPublicKey: "0x" + hex.EncodeToString(coldkey[:]),
			RegistrationBlock: validator.Registration.Block.Number, StakeRao: validator.StakeRao,
			ValidatorPermit: validator.ValidatorPermit, ValidatorTrustU16: validator.ValidatorTrustU16,
		}
		stateBytes, marshalErr := json.Marshal(map[string]any{"snapshot": validator.Snapshot, "state": state})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		validator.SnapshotArtifact = artifact("native-validator-state", fmt.Sprintf("validator-state-%d.json", validator.ValidatorID), stateBytes)
	}
	epochs := make([]FinalEpochOperatorEvidence, 0, 10)
	for epoch := uint64(10); epoch <= 14; epoch++ {
		for i := 0; i < 2; i++ {
			epochStart := uint64(100) + (epoch-10)*finalReleaseEpochBlocks
			root := evmReceipt(fmt.Sprintf("root-%d-%d", epoch, i+1), epochStart+5+uint64(i))
			payout := payouts[fmt.Sprintf("%d/%d", epoch, i+1)]
			payoutLocator := payout.locator
			finalizeBlock := epochStart + finalReleaseFinalizeOffsetBlocks
			claims := make([]FinalClaimEvidence, 0, len(payout.artifact.Leaves))
			claimed := new(big.Int)
			for _, leaf := range payout.artifact.Leaves {
				amount := new(big.Int).Mul(new(big.Int).SetUint64(leaf.ShareBPS), big.NewInt(1_000))
				amount.Quo(amount, big.NewInt(10_000))
				if amount.Sign() == 0 {
					t.Fatalf("fixture payout epoch %d operator %d leaf %d has zero entitlement", epoch, i+1, leaf.Index)
				}
				claimed.Add(claimed, amount)
				claims = append(claims, FinalClaimEvidence{
					LeafIndex: leaf.Index, Payee: "0x" + hex.EncodeToString(leaf.Coldkey[:]), ShareBPS: leaf.ShareBPS,
					ClaimedRao: amount.String(), PaidRao: amount.String(), DeferredRao: "0",
					Receipt: evmReceipt(fmt.Sprintf("claim-%d-%d-%d", epoch, i+1, leaf.Index), finalizeBlock),
				})
			}
			if claimed.Cmp(big.NewInt(1_000)) != 0 {
				t.Fatalf("fixture payout epoch %d operator %d claims total %s, want 1000", epoch, i+1, claimed)
			}
			epochs = append(epochs, FinalEpochOperatorEvidence{Epoch: epoch, NoID: uint64(i + 1), Capture: evmReceipt(fmt.Sprintf("capture-%d-%d", epoch, i+1), epochStart+uint64(i)), RootDisposition: "committed", Root: &root, Finalize: evmReceipt(fmt.Sprintf("finalize-%d-%d", epoch, i+1), finalizeBlock), PayoutRoot: "0x" + hex.EncodeToString(payout.artifact.PayoutRoot[:]), ArtifactHash: "0x" + strings.TrimPrefix(payout.artifact.ContentHash, "sha256:"), PayoutArtifact: &payoutLocator, CapturedRao: "1000", CarryInRao: "0", FundedRao: "1000", TotalRao: "1000", ClaimedRao: claimed.String(), PaidRao: claimed.String(), DeferredCreditRao: "0", OutstandingRao: "0", CarryOutRao: "0", Status: 2, Claims: claims})
		}
	}
	rewards := make([]FinalNativeRewardDelta, 0, int(finalReleaseEpochCount)*(finalHeadCandidateCount+4))
	for epochIndex, heads := range []struct {
		epoch         uint64
		before, after ChainHead
	}{{10, ChainHead{Number: 15, Hash: finalTestHex(15)}, ChainHead{Number: 28, Hash: finalTestHex(28)}}, {11, ChainHead{Number: 28, Hash: finalTestHex(28)}, ChainHead{Number: 40, Hash: finalTestHex(40)}}, {12, ChainHead{Number: 40, Hash: finalTestHex(40)}, ChainHead{Number: 55, Hash: finalTestHex(55)}}, {13, ChainHead{Number: 55, Hash: finalTestHex(55)}, ChainHead{Number: 70, Hash: finalTestHex(70)}}, {14, ChainHead{Number: 70, Hash: finalTestHex(70)}, ChainHead{Number: 100, Hash: finalTestHex(100)}}} {
		// The same EVM height must always carry one canonical hash, including
		// when another receipt or snapshot already references that height.
		beforeEVM := ChainHead{Number: heads.before.Number, Hash: finalTestHex(byte(heads.before.Number))}
		afterEVM := ChainHead{Number: heads.after.Number, Hash: finalTestHex(byte(heads.after.Number))}
		for _, fleet := range headFleets {
			expected, beforeRao, afterRao, deltaRao := "zero", "0", "0", "0"
			beforeIncentive, afterIncentive := uint16(0), uint16(0)
			if fleet.FleetID <= finalHeadSlotCount {
				expected, beforeRao, afterRao, deltaRao = "positive", fmt.Sprint(10+10*epochIndex), fmt.Sprint(20+10*epochIndex), "10"
				beforeIncentive, afterIncentive = uint16(epochIndex+1), uint16(epochIndex+2)
			}
			stakeBefore, stakeAfter, stakeDelta := "100", "100", "0"
			if expected == "positive" {
				stakeBefore, stakeAfter, stakeDelta = fmt.Sprint(1_000+10*epochIndex), fmt.Sprint(1_010+10*epochIndex), "10"
			}
			hotkey, ownerColdkey, err := finalSemanticSS58Pair("fixture head", fleet.Hotkey, fleet.Coldkey)
			if err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("head-reward-%d-%d.json", heads.epoch, fleet.FleetID)
			rewards = append(rewards, FinalNativeRewardDelta{Epoch: heads.epoch, Role: "head", SubjectID: fleet.FleetID, UID: fleet.UID, Hotkey: "0x" + hex.EncodeToString(hotkey[:]), Before: heads.before, After: heads.after, BeforeRao: beforeRao, AfterRao: afterRao, DeltaRao: deltaRao, StakeBeforeRao: stakeBefore, StakeAfterRao: stakeAfter, StakeDeltaRao: stakeDelta, OwnerColdkey: "0x" + hex.EncodeToString(ownerColdkey[:]), OwnerStakeBeforeRao: stakeBefore, OwnerStakeAfterRao: stakeAfter, OwnerStakeDeltaRao: stakeDelta, OwnerStakeBeforeEVM: beforeEVM, OwnerStakeAfterEVM: afterEVM, BeforeIncentiveU16: beforeIncentive, AfterIncentiveU16: afterIncentive, Expected: expected, SnapshotArtifact: artifact("native-reward-snapshot", name, []byte(name))})
		}
		for noID, uid := range []uint16{11, 13} {
			hotkey, ownerColdkey, err := finalSemanticSS58Pair("fixture pool", pools[noID].Hotkey, pools[noID].Coldkey)
			if err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("pool-reward-%d-%d.json", heads.epoch, noID+1)
			// Pools can move stake into and out of vault custody; preserve exact
			// shared checkpoint values while deliberately exercising both signs.
			poolStake := []int64{500, 490, 510, 480, 520, 470}
			stakeBefore, stakeAfter := big.NewInt(poolStake[epochIndex]), big.NewInt(poolStake[epochIndex+1])
			stakeDelta := new(big.Int).Sub(new(big.Int).Set(stakeAfter), stakeBefore).String()
			rewards = append(rewards, FinalNativeRewardDelta{Epoch: heads.epoch, Role: "pool", SubjectID: uint64(noID + 1), UID: uid, Hotkey: "0x" + hex.EncodeToString(hotkey[:]), Before: heads.before, After: heads.after, BeforeRao: fmt.Sprint(10 + 10*epochIndex), AfterRao: fmt.Sprint(20 + 10*epochIndex), DeltaRao: "10", StakeBeforeRao: stakeBefore.String(), StakeAfterRao: stakeAfter.String(), StakeDeltaRao: stakeDelta, OwnerColdkey: "0x" + hex.EncodeToString(ownerColdkey[:]), OwnerStakeBeforeRao: stakeBefore.String(), OwnerStakeAfterRao: stakeAfter.String(), OwnerStakeDeltaRao: stakeDelta, OwnerStakeBeforeEVM: beforeEVM, OwnerStakeAfterEVM: afterEVM, BeforeIncentiveU16: uint16(epochIndex + 1), AfterIncentiveU16: uint16(epochIndex + 2), Expected: "positive", SnapshotArtifact: artifact("native-reward-snapshot", name, []byte(name))})
		}
		for validatorID, uid := range []uint16{12, 14} {
			hotkey, ownerColdkey, err := finalSemanticSS58Pair("fixture validator", validators[validatorID].Hotkey, validators[validatorID].Coldkey)
			if err != nil {
				t.Fatal(err)
			}
			stakeBefore, stakeAfter := fmt.Sprint(2_000+5*epochIndex), fmt.Sprint(2_005+5*epochIndex)
			ownerBefore, ownerAfter, ownerDelta := stakeBefore, stakeAfter, "5"
			name := fmt.Sprintf("validator-reward-%d-%d.json", heads.epoch, validatorID+1)
			row := FinalNativeRewardDelta{Epoch: heads.epoch, Role: "validator", SubjectID: uint64(validatorID + 1), UID: uid, Hotkey: "0x" + hex.EncodeToString(hotkey[:]), Before: heads.before, After: heads.after, BeforeRao: fmt.Sprint(20 + 5*epochIndex), AfterRao: fmt.Sprint(25 + 5*epochIndex), DeltaRao: "5", StakeBeforeRao: stakeBefore, StakeAfterRao: stakeAfter, StakeDeltaRao: "5", OwnerColdkey: "0x" + hex.EncodeToString(ownerColdkey[:]), OwnerStakeBeforeRao: ownerBefore, OwnerStakeAfterRao: ownerAfter, OwnerStakeDeltaRao: ownerDelta, OwnerStakeBeforeEVM: beforeEVM, OwnerStakeAfterEVM: afterEVM, BeforeDividendsU16: uint16(epochIndex + 1), AfterDividendsU16: uint16(epochIndex + 2), Expected: "positive", SnapshotArtifact: artifact("native-reward-snapshot", name, []byte(name))}
			if validatorID == 0 {
				row.OwnerStakeBeforeRao, row.OwnerStakeAfterRao, row.OwnerStakeDeltaRao = fmt.Sprint(1_000+2*epochIndex), fmt.Sprint(1_002+2*epochIndex), "2"
				row.ReserveColdkey = deployment.ReserveSelfColdkey
				row.ReserveStakeBeforeRao, row.ReserveStakeAfterRao, row.ReserveStakeDeltaRao = fmt.Sprint(1_000+3*epochIndex), fmt.Sprint(1_003+3*epochIndex), "3"
			}
			rewards = append(rewards, row)
		}
	}
	for epoch := uint64(10); epoch <= 14; epoch++ {
		indices := make([]int, 0, finalHeadCandidateCount+4)
		maximumUID := uint16(0)
		for index := range rewards {
			if rewards[index].Epoch == epoch {
				indices = append(indices, index)
				if rewards[index].UID > maximumUID {
					maximumUID = rewards[index].UID
				}
			}
		}
		if len(indices) == 0 {
			t.Fatalf("fixture reward epoch %d is empty", epoch)
		}
		before := &NativeRewardObservation{FinalizedHead: rewards[indices[0]].Before, EmissionRao: make([]string, int(maximumUID)+1), Incentive: make([]uint16, int(maximumUID)+1), Dividends: make([]uint16, int(maximumUID)+1), TotalHotkeyAlphaRao: make([]string, int(maximumUID)+1)}
		after := &NativeRewardObservation{FinalizedHead: rewards[indices[0]].After, EmissionRao: make([]string, int(maximumUID)+1), Incentive: make([]uint16, int(maximumUID)+1), Dividends: make([]uint16, int(maximumUID)+1), TotalHotkeyAlphaRao: make([]string, int(maximumUID)+1)}
		for index := range before.EmissionRao {
			before.EmissionRao[index], before.TotalHotkeyAlphaRao[index] = "0", "0"
			after.EmissionRao[index], after.TotalHotkeyAlphaRao[index] = "0", "0"
		}
		beforePositions, afterPositions := map[string]FinalCollectedRewardStakePosition{}, map[string]FinalCollectedRewardStakePosition{}
		for _, index := range indices {
			reward := &rewards[index]
			uid := int(reward.UID)
			before.EmissionRao[uid], after.EmissionRao[uid] = reward.BeforeRao, reward.AfterRao
			before.TotalHotkeyAlphaRao[uid], after.TotalHotkeyAlphaRao[uid] = reward.StakeBeforeRao, reward.StakeAfterRao
			before.Incentive[uid], after.Incentive[uid] = reward.BeforeIncentiveU16, reward.AfterIncentiveU16
			before.Dividends[uid], after.Dividends[uid] = reward.BeforeDividendsU16, reward.AfterDividendsU16
			hotkeyHex := reward.Hotkey
			ownerKey := hotkeyHex + "/" + reward.OwnerColdkey
			beforePositions[ownerKey] = FinalCollectedRewardStakePosition{Identity: reward.Role + "-" + strconv.FormatUint(reward.SubjectID, 10) + "-owner", HotkeyPublicKey: hotkeyHex, ColdkeyPublicKey: reward.OwnerColdkey, StakeRao: reward.OwnerStakeBeforeRao}
			afterPositions[ownerKey] = FinalCollectedRewardStakePosition{Identity: reward.Role + "-" + strconv.FormatUint(reward.SubjectID, 10) + "-owner", HotkeyPublicKey: hotkeyHex, ColdkeyPublicKey: reward.OwnerColdkey, StakeRao: reward.OwnerStakeAfterRao}
			if reward.ReserveColdkey != "" {
				reserveKey := hotkeyHex + "/" + reward.ReserveColdkey
				beforePositions[reserveKey] = FinalCollectedRewardStakePosition{Identity: "reserve-validator-sink", HotkeyPublicKey: hotkeyHex, ColdkeyPublicKey: reward.ReserveColdkey, StakeRao: reward.ReserveStakeBeforeRao}
				afterPositions[reserveKey] = FinalCollectedRewardStakePosition{Identity: "reserve-validator-sink", HotkeyPublicKey: hotkeyHex, ColdkeyPublicKey: reward.ReserveColdkey, StakeRao: reward.ReserveStakeAfterRao}
			}
		}
		beforeSnapshot := &FinalCollectedRewardStakeSnapshot{NativeHead: before.FinalizedHead, EVMHead: rewards[indices[0]].OwnerStakeBeforeEVM}
		afterSnapshot := &FinalCollectedRewardStakeSnapshot{NativeHead: after.FinalizedHead, EVMHead: rewards[indices[0]].OwnerStakeAfterEVM}
		for _, position := range beforePositions {
			beforeSnapshot.Positions = append(beforeSnapshot.Positions, position)
		}
		for _, position := range afterPositions {
			afterSnapshot.Positions = append(afterSnapshot.Positions, position)
		}
		sort.Slice(beforeSnapshot.Positions, func(i, j int) bool {
			return beforeSnapshot.Positions[i].Identity < beforeSnapshot.Positions[j].Identity
		})
		sort.Slice(afterSnapshot.Positions, func(i, j int) bool { return afterSnapshot.Positions[i].Identity < afterSnapshot.Positions[j].Identity })
		applicationBlock, err := finalSemanticApplicationBlock(&FinalSemanticEvidence{Validators: validators}, epoch)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(map[string]any{"epoch": epoch, "application_block": applicationBlock, "before": before, "after": after, "before_owner_stakes": beforeSnapshot, "after_owner_stakes": afterSnapshot})
		if err != nil {
			t.Fatal(err)
		}
		locator := artifact("native-reward-snapshot", fmt.Sprintf("native-reward-epoch-%d.json", epoch), data)
		for _, index := range indices {
			rewards[index].SnapshotArtifact = locator
		}
	}
	pathProofs := make([]FinalValidatorPathProofEvidence, 0, 4)
	for validatorID := 1; validatorID <= 2; validatorID++ {
		for noID := 1; noID <= 2; noID++ {
			var data []byte
			for epoch := uint64(10); epoch <= 14; epoch++ {
				var trailID connect.Id
				trailID[0], trailID[1], trailID[14], trailID[15] = byte(validatorID), byte(noID), byte(epoch>>8), byte(epoch)
				nonce := bytes.Repeat([]byte{byte(0x60 + validatorID + noID + int(epoch-10))}, connect.VerifyNonceSize)
				vpk := validatorPathKeys[validatorID-1].Public().(ed25519.PublicKey)
				hops := make([]connect.VerifyProofHop, policy.Verify.TrailDepth)
				trail := make([]connect.Id, len(hops))
				for hopIndex := range hops {
					trail[hopIndex][0], trail[hopIndex][1], trail[hopIndex][2], trail[hopIndex][15] = byte(validatorID), byte(noID), byte(epoch), byte(hopIndex+1)
					hops[hopIndex].ClientId = trail[hopIndex]
					hops[hopIndex].TimeMs = uint64(1_000 + validatorID*100 + noID*10 + hopIndex)
				}
				finalMessage, messageErr := connect.BuildVerifyFinalMessage(1, trailID, nonce, vpk, byte(len(hops)), hops)
				if messageErr != nil {
					t.Fatal(messageErr)
				}
				extendMessage, messageErr := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(len(hops)), trail)
				if messageErr != nil {
					t.Fatal(messageErr)
				}
				digest := connect.VerifyFinalDigest(finalMessage)
				pathID := validatorpkg.TrailPathId(trailID, vpk, 1)
				record := validatorpkg.ProofRecord{
					Version: 1, Epoch: epoch, TrailId: trailID, ServerNonce: nonce, Vpk: vpk, M: len(hops), Hops: hops, ServerKeyId: 1,
					FinalSig: ed25519.Sign(operatorServerKeys[noID-1], finalMessage), VerifierSig: ed25519.Sign(validatorPathKeys[validatorID-1], extendMessage),
					FinalDigest: digest[:], VpkSig: ed25519.Sign(validatorPathKeys[validatorID-1], finalMessage), Coverage: uint64(len(hops) - 1), PathId: pathID[:], CompleteTimeMs: hops[len(hops)-1].TimeMs,
				}
				line, marshalErr := json.Marshal(FinalCollectedProofRecord{Schema: finalCollectedProofRecordSchema, Record: record})
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				data = append(data, line...)
				data = append(data, '\n')
			}
			locator := artifact("validator-path-proofs", fmt.Sprintf("path-proofs-%d-%d.jsonl", validatorID, noID), data)
			pathProofs = append(pathProofs, FinalValidatorPathProofEvidence{ValidatorID: uint64(validatorID), NoID: uint64(noID), FirstEpoch: 10, LastEpoch: 14, ProofCount: 5, TrailDepth: policy.Verify.TrailDepth, ProofsHash: locator.ContentHash, Artifact: locator})
		}
	}
	cleanupCutoff := time.Unix(1_700_000_000, 456).UTC()
	cleanupCutoffText := cleanupCutoff.Format(time.RFC3339Nano)
	cleanupManifestHash := finalTestHex(90)
	cleanupState := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: 4242, SupervisorStartTimeTicks: 987654,
		ManifestHash: cleanupManifestHash, ContractCleanupCutoff: cleanupCutoffText,
		Processes: []ProcessState{
			{ID: "operator-1-taskworker", Role: "operator-taskworker", Identity: "no:1", PID: 101, Healthy: true, Restarts: 1},
			{ID: "operator-2-taskworker", Role: "operator-taskworker", Identity: "no:2", PID: 102, Healthy: true, Restarts: 1},
		},
	}
	topology.ProcessRestarts = []FinalProcessRestartEvidence{
		{ProcessID: "operator-1-taskworker", ExpectedRestarts: 1, ObservedRestarts: 1, FaultIDs: []string{"release-rolling-01"}},
		{ProcessID: "operator-2-taskworker", ExpectedRestarts: 1, ObservedRestarts: 1, FaultIDs: []string{"release-rolling-02"}},
	}
	cleanupStateBytes, err := json.Marshal(cleanupState)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := FinalContractCleanupEvidence{
		Schema: "urnetwork-sim-final-contract-cleanup-v1", Cutoff: cleanupCutoffText, CutoffUnixNano: cleanupCutoff.UnixNano(),
		SupervisorManifestHash: cleanupManifestHash, SupervisorStartTimeTicks: cleanupState.SupervisorStartTimeTicks,
		SuccessfulInvocations: 2, FailedInvocations: 0, SupervisorStateArtifact: artifact("supervisor-cleanup-generation", "accepted-supervisor-cleanup.json", cleanupStateBytes),
	}
	for operator := 1; operator <= 2; operator++ {
		id := fmt.Sprintf("operator-%d-taskworker", operator)
		result := serverContractCleanupResult{Schema: "urnetwork-sim-server-contract-cleanup-v1", Cutoff: cleanupCutoffText, Passes: operator, Closed: int64(operator - 1), Converged: true}
		resultBytes, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		base := fmt.Sprintf("%s-contract-cleanup-%d", id, cleanupCutoff.UnixNano())
		cleanup.Operators = append(cleanup.Operators, FinalOperatorContractCleanupEvidence{
			NoID: uint64(operator), TaskworkerID: id, Passes: result.Passes, Closed: result.Closed, Converged: true,
			ResultArtifact: artifact("server-contract-cleanup-result", base+".json", resultBytes),
			LogArtifact:    artifact("server-contract-cleanup-log", base+".log", []byte("cleanup converged\n")),
		})
	}
	campaignStartedAt := time.Unix(1_699_999_000, 0).UTC()
	archiveReceipt := FinalArchiveRetentionPreflight{
		Schema: finalArchiveRetentionPreflightSchema, GeneratedAt: campaignStartedAt.Add(-time.Minute).Format(time.RFC3339Nano), DeploymentID: "ur-subnet-testnet-v1-attempt-4",
		PublicManifestHash: finalTestHex(91), PlannedSpanBlocks: finalReleaseArchiveMinimumSpanBlocks, SafetyMarginBlocks: finalReleaseArchiveMinimumSafetyMarginBlocks, RequiredDepthBlocks: 15_770,
		Substrate: FinalArchiveProbeResult{
			Endpoint: "wss://substrate.example.test", FinalizedHead: ChainHead{Number: 25_000, Hash: finalTestHex(92)}, EarliestRequiredHead: ChainHead{Number: 20_000, Hash: finalTestHex(93)}, HistoricalHead: ChainHead{Number: 9_230, Hash: finalTestHex(94)}, RequiredDepthBlocks: 15_770,
			MetadataHash: "sha256:" + strings.Repeat("11", 32), EventsHash: "sha256:" + strings.Repeat("22", 32), ExactMetadataHash: "sha256:" + strings.Repeat("33", 32), ExactEventsHash: "sha256:" + strings.Repeat("44", 32),
		},
		EVM: FinalArchiveProbeResult{
			Endpoint: "https://evm.example.test", FinalizedHead: ChainHead{Number: 25_000, Hash: finalTestHex(95)}, EarliestRequiredHead: ChainHead{Number: 20_000, Hash: finalTestHex(96)}, HistoricalHead: ChainHead{Number: 9_230, Hash: finalTestHex(97)}, RequiredDepthBlocks: 15_770,
			GenericStateHash: finalTestHex(98), ExactStateHash: finalTestHex(99), DeploymentHead: ChainHead{Number: 21_000, Hash: finalTestHex(100)}, CodeHash: finalTestHex(101), CallResultHash: finalTestHex(102),
		},
		Passed: true,
	}
	archiveReceipt.EvidenceHash, err = finalArchiveRetentionPreflightHash(&archiveReceipt)
	if err != nil {
		t.Fatal(err)
	}
	archiveWire, err := json.Marshal(&archiveReceipt)
	if err != nil {
		t.Fatal(err)
	}
	archiveEvidence := FinalArchiveRetentionEvidence{
		GeneratedAt: archiveReceipt.GeneratedAt, DeploymentID: archiveReceipt.DeploymentID, PublicManifestHash: archiveReceipt.PublicManifestHash,
		PlannedSpanBlocks: archiveReceipt.PlannedSpanBlocks, SafetyMarginBlocks: archiveReceipt.SafetyMarginBlocks, RequiredDepthBlocks: archiveReceipt.RequiredDepthBlocks,
		EvidenceHash: archiveReceipt.EvidenceHash, Artifact: artifact("archive-retention-preflight", "archive-retention-preflight.json", archiveWire),
	}
	source := FinalSemanticEvidence{
		Phase: "release-1.0", RunID: "release-run-1", ResultHash: finalTestHex(1), CampaignStartedAt: campaignStartedAt.Format(time.RFC3339Nano), CampaignCompletedAt: time.Unix(1_700_001_000, 0).UTC().Format(time.RFC3339Nano), DeploymentID: "ur-subnet-testnet-v1-attempt-4", PlanHash: finalTestHex(2), ConfigHash: finalTestHex(3), PolicyHash: policyHash, GenesisHash: finalTestHex(5), ChainID: 945, Netuid: 521, PolicyArtifact: policyLocator,
		Window: ScenarioAcceptanceWindow{
			Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 99, Hash: finalTestHex(99)}, BaselineObservationHash: finalTestHex(6),
			BaselineEpoch: 9, FirstEpoch: 10, EpochCount: finalReleaseEpochCount, EpochBlocks: finalReleaseEpochBlocks, StartBlock: 100, EndBlock: 1600, FinalizeOffsetBlocks: finalReleaseFinalizeOffsetBlocks, TerminalBlock: 1750,
			PolicyEffectiveEpoch: 10, PolicyEffectiveBlock: 100,
		},
		EVMCampaignStartHead: ChainHead{Number: 4, Hash: finalTestHex(4)}, NativeStartHead: ChainHead{Number: 10, Hash: finalTestHex(10)}, NativeTerminalHead: ChainHead{Number: 100, Hash: finalTestHex(100)}, EVMTerminalHead: ChainHead{Number: 1750, Hash: finalTestHex(214)},
		ExpectedOperators: 2, ExpectedValidators: 2, ExpectedMiners: 1000, ExpectedCandidates: finalHeadCandidateCount, ExpectedHeadSlots: finalHeadSlotCount,
		Topology: topology, HeadFleets: headFleets, HeadTransitions: headTransitions,
		ValidatorView: FinalValidatorViewTransition{
			FaultEpoch: 10, RestoredEpoch: 11, AffectedValidatorID: 1, ControlValidatorID: 2, WithheldFleetID: 200, ReplacementFleetID: 5,
			Artifact: artifact("validator-view-transition", "validator-view-transition.json", mustFinalSemanticJSON(t, finalValidatorViewTransitionArtifact{
				FaultEpoch: 10, RestoredEpoch: 11, AffectedValidatorID: 1, ControlValidatorID: 2, WithheldFleetID: 200, ReplacementFleetID: 5,
			})),
		},
		ContractCleanup: cleanup, ArchiveRetention: archiveEvidence, Deployment: deployment, SettlementAccounting: settlementAccounting, Reserve: reserve, Pools: pools, Validators: validators, Epochs: epochs,
		Conservation:  FinalPoolConservation{CapturedRao: "10000", CarryInRao: "0", FundedRao: "10000", ClaimedRao: "10000", PaidRao: "10000", DeferredCreditRao: "0", OutstandingRao: "0", CarryOutRao: "0"},
		NativeRewards: rewards, PathProofs: pathProofs, ExitCriteria: exitCriteria,
	}
	attachFinalFleetLifecycleFixture(t, &source, artifacts)
	return source, artifacts
}

func finalTestPreparedSubmission(t *testing.T, uids, values []uint16, cycle FinalCRv4Cycle, hotkey [32]byte) *crv4.PreparedSubmission {
	t.Helper()
	payload, err := (&crv4.Payload{Hotkey: hotkey, Uids: uids, Values: values, VersionKey: 7}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := []byte{0xaa, hotkey[0]}
	body := append([]byte{0x84}, ciphertext...)
	raw := append([]byte{byte(len(body) << 2)}, body...)
	cipherHash := sha256.Sum256(ciphertext)
	extrinsicHash := blake2b.Sum256(raw)
	prepared := &crv4.PreparedSubmission{
		Schema: crv4.PreparedSubmissionSchema, Netuid: 521, HotkeyHex: "0x" + hex.EncodeToString(hotkey[:]), VersionKey: 7,
		CommitRevealVersion: crv4.CommitRevealVersion4, AccountNonce: 1, PreparedAtBlock: cycle.NativeSnapshot.Number, PreparedAtBlockHash: cycle.NativeSnapshot.Hash,
		SubnetEpoch: cycle.SubnetEpoch, RevealRound: 1, RevealBlock: cycle.Reveal.Block.Number, UIDs: append([]uint16(nil), uids...), Values: append([]uint16(nil), values...),
		PayloadHex: "0x" + hex.EncodeToString(payload), CiphertextHex: "0x" + hex.EncodeToString(ciphertext), CiphertextSHA256: "0x" + hex.EncodeToString(cipherHash[:]),
		ExtrinsicHex: "0x" + hex.EncodeToString(raw), ExtrinsicHash: "0x" + hex.EncodeToString(extrinsicHash[:]),
	}
	if _, err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}
	return prepared
}

func finalSemanticClone(t *testing.T, source *FinalSemanticEvidence) *FinalSemanticEvidence {
	t.Helper()
	b, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var clone FinalSemanticEvidence
	if err := json.Unmarshal(b, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
}

func resignFinalSemantic(t *testing.T, evidence *FinalSemanticEvidence) {
	t.Helper()
	hash, err := finalSemanticEvidenceHash(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidence.EvidenceHash = hash
}

func finalTestHex(seed byte) string {
	return "0x" + hex.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

func mustFinalSemanticJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

type finalTestChainReader struct {
	evidence              *FinalSemanticEvidence
	failCanonical         bool
	corruptWeights        bool
	corruptPoolOwnership  bool
	corruptRegistration   bool
	corruptCustody        bool
	corruptSettlement     bool
	corruptOwnerStake     bool
	corruptReserveReceipt bool
	nativeTip             uint64
	evmTip                uint64
	forkTarget            bool
}

func (r *finalTestChainReader) Endpoints() (string, string, string) {
	return "wss://substrate.example/rpc", "https://evm.example/rpc", "https://evidence.example/deployment-manifest.json?hash=sha256:" + strings.Repeat("11", 32)
}

func (r *finalTestChainReader) PublicManifestHash() string { return finalTestHex(0x5a) }

func (r *finalTestChainReader) OperatorEvidenceOrigins() []FinalOperatorEvidenceOrigin {
	return []FinalOperatorEvidenceOrigin{
		{OperatorNoID: 1, ManifestURI: "https://evidence.example/deployment-manifest.json?hash=sha256:" + strings.Repeat("11", 32)},
		{OperatorNoID: 2, ManifestURI: "https://evidence-2.example/deployment-manifest.json?hash=sha256:" + strings.Repeat("22", 32)},
	}
}

func (r *finalTestChainReader) exchange(chain, method string, head ChainHead) []FinalRPCExchange {
	return []FinalRPCExchange{{Chain: chain, Method: method, Params: json.RawMessage("[]"), PinnedHead: head, Result: json.RawMessage("{\"ok\":true}")}}
}

func (r *finalTestChainReader) CanonicalSubstrateHead(_ context.Context, head ChainHead) ([]FinalRPCExchange, error) {
	if r.failCanonical {
		return nil, errors.New("archive unavailable")
	}
	if r.nativeTip != 0 && head.Number > r.nativeTip {
		return nil, errors.New("checkpoint is ahead of finalized tip")
	}
	if r.forkTarget {
		return nil, errors.New("canonical target mismatch")
	}
	return r.exchange("substrate", "chain_getHeader", head), nil
}

func (r *finalTestChainReader) CanonicalEVMHead(_ context.Context, head ChainHead) ([]FinalRPCExchange, error) {
	if r.evmTip != 0 && head.Number > r.evmTip {
		return nil, errors.New("checkpoint is ahead of finalized tip")
	}
	if r.forkTarget {
		return nil, errors.New("canonical target mismatch")
	}
	return r.exchange("evm", "eth_getBlockByNumber", head), nil
}

func (r *finalTestChainReader) NativeUID(_ context.Context, _ uint16, uid uint16, head ChainHead) (FinalNativeUIDState, []FinalRPCExchange, error) {
	for _, pool := range r.evidence.Pools {
		if pool.UID == uid {
			hotkey := pool.Hotkey
			if r.corruptPoolOwnership {
				hotkey += "-wrong"
			}
			return FinalNativeUIDState{UID: uid, Hotkey: hotkey, Coldkey: pool.Coldkey, Registered: pool.Registered}, r.exchange("substrate", "state_getStorage", head), nil
		}
	}
	for _, validator := range r.evidence.Validators {
		if validator.UID == uid {
			return FinalNativeUIDState{UID: uid, Hotkey: validator.Hotkey, Coldkey: validator.Coldkey, Registered: validator.Registered, StakeRao: validator.StakeRao, ValidatorPermit: validator.ValidatorPermit, ValidatorTrustU16: validator.ValidatorTrustU16}, r.exchange("substrate", "state_getStorage", head), nil
		}
	}
	for _, fleet := range r.evidence.HeadFleets {
		if fleet.UID == uid {
			return FinalNativeUIDState{UID: uid, Hotkey: fleet.Hotkey, Coldkey: fleet.Coldkey, Registered: fleet.Registered}, r.exchange("substrate", "state_getStorage", head), nil
		}
	}
	return FinalNativeUIDState{}, nil, errors.New("unknown UID")
}

func (r *finalTestChainReader) NativeEvent(_ context.Context, receipt FinalNativeReceipt, event string) (FinalNativeEventState, []FinalRPCExchange, error) {
	return FinalNativeEventState{ExtrinsicHash: receipt.ExtrinsicHash, Block: receipt.Block, Success: true, Event: event}, r.exchange("substrate", "chain_getBlock", receipt.Block), nil
}

func (r *finalTestChainReader) NativeWeights(_ context.Context, _ uint16, validatorUID uint16, head ChainHead) (FinalNativeWeightState, []FinalRPCExchange, error) {
	for _, validator := range r.evidence.Validators {
		if validator.UID != validatorUID {
			continue
		}
		for _, cycle := range validator.Cycles {
			if cycle.Application.Block == head {
				uids, values := finalSubmittedValues(cycle.Submitted)
				if r.corruptWeights && len(values) != 0 {
					values[0]++
				}
				return FinalNativeWeightState{ValidatorUID: validatorUID, UIDs: uids, Values: values, Block: head}, r.exchange("substrate", "state_getStorage", head), nil
			}
		}
	}
	return FinalNativeWeightState{}, nil, errors.New("unknown weight checkpoint")
}

func (r *finalTestChainReader) NativePruneSnapshot(_ context.Context, _ uint16, head ChainHead) (FleetLifecyclePruneSnapshot, []FinalRPCExchange, error) {
	if r.evidence.FleetLifecycle == nil {
		return FleetLifecyclePruneSnapshot{}, nil, errors.New("no lifecycle fixture")
	}
	state := &r.evidence.FleetLifecycle.State
	if state.LaunchPrune != nil && state.LaunchPrune.Head == head {
		return *state.LaunchPrune, r.exchange("substrate", "state_queryStorageAt", head), nil
	}
	for _, registration := range []*FleetLifecycleRegistrationEvidence{state.FallbackRegistration, state.ProviderRegistration, state.TerminalRegistration} {
		if registration == nil {
			continue
		}
		if registration.PrePrune.Head == head {
			return registration.PrePrune, r.exchange("substrate", "state_queryStorageAt", head), nil
		}
		if registration.PostRegistration.Head == head {
			return registration.PostRegistration, r.exchange("substrate", "state_queryStorageAt", head), nil
		}
	}
	for _, census := range state.CandidateCensuses {
		for _, validator := range census.Validators {
			if validator.NativeSnapshot != head {
				continue
			}
			inputs := make([]FleetLifecyclePruneInput, len(census.CandidateUIDs))
			for index, uid := range census.CandidateUIDs {
				inputs[index] = FleetLifecyclePruneInput{UID: uid, Hotkey: census.CandidateHotkeys[index]}
			}
			return FleetLifecyclePruneSnapshot{Head: head, UIDCount: uint16(len(inputs)), Inputs: inputs}, r.exchange("substrate", "state_queryStorageAt", head), nil
		}
	}
	return FleetLifecyclePruneSnapshot{}, nil, errors.New("unknown lifecycle prune checkpoint")
}

func (r *finalTestChainReader) NativeFleetCommitment(_ context.Context, _ uint16, hotkey string, head ChainHead) (FinalNativeFleetCommitmentState, []FinalRPCExchange, error) {
	if r.evidence.FleetLifecycle != nil {
		for _, variant := range r.evidence.FleetLifecycle.Variants {
			commitmentHead := ChainHead{Number: variant.Commitment.FinalizedBlock, Hash: strings.ToLower(variant.Commitment.FinalizedBlockHash)}
			if strings.EqualFold(variant.Hotkey, hotkey) && commitmentHead == head {
				return FinalNativeFleetCommitmentState{Hotkey: strings.ToLower(hotkey), CommitmentHash: strings.ToLower(variant.Commitment.CommitmentHash), CommitmentBlock: variant.Commitment.CommitmentBlock, Block: head}, r.exchange("substrate", "state_queryStorageAt", head), nil
			}
		}
	}
	return FinalNativeFleetCommitmentState{}, nil, errors.New("unknown lifecycle commitment checkpoint")
}

func (r *finalTestChainReader) FleetMirror(_ context.Context, hotkey string, head ChainHead) (FinalFleetMirrorChainState, []FinalRPCExchange, error) {
	if r.evidence.FleetLifecycle != nil {
		for _, variant := range r.evidence.FleetLifecycle.Variants {
			mirrorHead := ChainHead{Number: variant.Mirror.BlockNumber, Hash: strings.ToLower(variant.Mirror.BlockHash)}
			if strings.EqualFold(variant.Hotkey, hotkey) && mirrorHead == head {
				return FinalFleetMirrorChainState{Hotkey: strings.ToLower(hotkey), CommitmentHash: strings.ToLower(variant.Mirror.CommitmentHash), FinalizedBlock: variant.Mirror.FinalizedBlock, FinalizedBlockHash: strings.ToLower(variant.Mirror.FinalizedBlockHash), Block: head}, r.exchange("evm", "eth_call", head), nil
			}
		}
	}
	return FinalFleetMirrorChainState{}, nil, errors.New("unknown lifecycle mirror checkpoint")
}

func finalTestFleetBindingState(binding FleetBindingEvidence, head ChainHead, cleaned bool, cleanedAt uint64) FinalFleetBindingChainState {
	return FinalFleetBindingChainState{
		Active: !cleaned, ClientID: strings.ToLower(binding.ClientID), FleetID: strings.ToLower(binding.FleetID), Hotkey: strings.ToLower(binding.Hotkey), ClientKey: strings.ToLower(binding.ClientKey),
		CommitmentHash: strings.ToLower(binding.CommitmentHash), Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch,
		CleanedAtEpoch: cleanedAt, UID: binding.UID, Cleaned: cleaned, Block: head,
	}
}

func (r *finalTestChainReader) FleetBinding(_ context.Context, clientID string, epoch uint64, head ChainHead) (FinalFleetBindingChainState, []FinalRPCExchange, error) {
	if r.evidence.FleetLifecycle != nil {
		for _, variant := range r.evidence.FleetLifecycle.Variants {
			for _, binding := range variant.Bindings {
				bindingHead := ChainHead{Number: binding.BlockNumber, Hash: strings.ToLower(binding.BlockHash)}
				if strings.EqualFold(binding.ClientID, clientID) && binding.ValidFromEpoch == epoch && bindingHead == head {
					return finalTestFleetBindingState(binding, head, false, 0), r.exchange("evm", "eth_call", head), nil
				}
			}
		}
	}
	return FinalFleetBindingChainState{}, nil, errors.New("unknown lifecycle binding checkpoint")
}

func (r *finalTestChainReader) FleetBindingRecord(_ context.Context, clientID string, head ChainHead) (FinalFleetBindingChainState, []FinalRPCExchange, error) {
	if r.evidence.FleetLifecycle != nil {
		for _, variant := range r.evidence.FleetLifecycle.Variants {
			bindings := make(map[string]FleetBindingEvidence, len(variant.Bindings))
			for _, binding := range variant.Bindings {
				bindings[strings.ToLower(binding.ClientID)] = binding
			}
			for _, cleanup := range variant.Cleanups {
				if !strings.EqualFold(cleanup.ClientID, clientID) {
					continue
				}
				binding, exists := bindings[strings.ToLower(clientID)]
				if !exists {
					return FinalFleetBindingChainState{}, nil, errors.New("fixture cleanup has no prior binding")
				}
				cleanupHead := ChainHead{Number: cleanup.BlockNumber, Hash: strings.ToLower(cleanup.BlockHash)}
				if cleanup.BeforeBlock == head {
					return finalTestFleetBindingState(binding, head, false, 0), r.exchange("evm", "eth_call", head), nil
				}
				if cleanupHead == head {
					return finalTestFleetBindingState(binding, head, true, cleanup.CleanedAtEpoch), r.exchange("evm", "eth_call", head), nil
				}
			}
		}
	}
	return FinalFleetBindingChainState{}, nil, errors.New("unknown lifecycle binding-record checkpoint")
}

func (r *finalTestChainReader) FleetMemberCount(_ context.Context, fleetID string, head ChainHead) (uint64, []FinalRPCExchange, error) {
	if r.evidence.FleetLifecycle != nil {
		for _, variant := range r.evidence.FleetLifecycle.Variants {
			for _, cleanup := range variant.Cleanups {
				if !strings.EqualFold(cleanup.FleetID, fleetID) {
					continue
				}
				cleanupHead := ChainHead{Number: cleanup.BlockNumber, Hash: strings.ToLower(cleanup.BlockHash)}
				if cleanup.BeforeBlock == head {
					return cleanup.MemberCountBefore, r.exchange("evm", "eth_call", head), nil
				}
				if cleanupHead == head {
					return cleanup.MemberCountAfter, r.exchange("evm", "eth_call", head), nil
				}
			}
		}
	}
	return 0, nil, errors.New("unknown lifecycle member-count checkpoint")
}

func (r *finalTestChainReader) FleetLifecycleEvents(_ context.Context, transactionHash string, head ChainHead) ([]FinalFleetLifecycleEventState, []FinalRPCExchange, error) {
	if r.evidence.FleetLifecycle != nil {
		for _, variant := range r.evidence.FleetLifecycle.Variants {
			if strings.EqualFold(variant.Mirror.TransactionHash, transactionHash) {
				return []FinalFleetLifecycleEventState{{Kind: "commitment-mirrored", TransactionHash: strings.ToLower(transactionHash), Block: head, Hotkey: strings.ToLower(variant.Mirror.Hotkey), CommitmentHash: strings.ToLower(variant.Mirror.CommitmentHash), FinalizedBlock: variant.Mirror.FinalizedBlock, FinalizedBlockHash: strings.ToLower(variant.Mirror.FinalizedBlockHash)}}, r.exchange("evm", "eth_getTransactionReceipt", head), nil
			}
			for _, binding := range variant.Bindings {
				if strings.EqualFold(binding.TransactionHash, transactionHash) {
					return []FinalFleetLifecycleEventState{{Kind: "fleet-bound", TransactionHash: strings.ToLower(transactionHash), Block: head, ClientID: strings.ToLower(binding.ClientID), FleetID: strings.ToLower(binding.FleetID), Hotkey: strings.ToLower(binding.Hotkey), Generation: binding.Generation, UID: binding.UID, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch}}, r.exchange("evm", "eth_getTransactionReceipt", head), nil
				}
			}
			for _, cleanup := range variant.Cleanups {
				if strings.EqualFold(cleanup.TransactionHash, transactionHash) {
					return []FinalFleetLifecycleEventState{{Kind: "fleet-binding-cleaned", TransactionHash: strings.ToLower(transactionHash), Block: head, ClientID: strings.ToLower(cleanup.ClientID), CleanedAtEpoch: cleanup.CleanedAtEpoch}}, r.exchange("evm", "eth_getTransactionReceipt", head), nil
				}
			}
		}
	}
	return nil, nil, errors.New("unknown lifecycle event checkpoint")
}

var _ FinalSemanticLifecycleChainReader = (*finalTestChainReader)(nil)

func (r *finalTestChainReader) NativeReward(_ context.Context, _ uint16, uid uint16, head ChainHead) (FinalNativeRewardState, []FinalRPCExchange, error) {
	for _, reward := range r.evidence.NativeRewards {
		if reward.UID != uid {
			continue
		}
		if reward.Before == head {
			return FinalNativeRewardState{UID: uid, EmissionRao: reward.BeforeRao, StakeRao: reward.StakeBeforeRao, IncentiveU16: reward.BeforeIncentiveU16, DividendsU16: reward.BeforeDividendsU16, Block: head}, r.exchange("substrate", "state_getStorage", head), nil
		}
		if reward.After == head {
			return FinalNativeRewardState{UID: uid, EmissionRao: reward.AfterRao, StakeRao: reward.StakeAfterRao, IncentiveU16: reward.AfterIncentiveU16, DividendsU16: reward.AfterDividendsU16, Block: head}, r.exchange("substrate", "state_getStorage", head), nil
		}
	}
	return FinalNativeRewardState{}, nil, errors.New("unknown reward checkpoint")
}

func (r *finalTestChainReader) NativeOwnerStake(_ context.Context, hotkey, coldkey string, head ChainHead) (FinalNativeOwnerStakeState, []FinalRPCExchange, error) {
	result := func(stake string) (FinalNativeOwnerStakeState, []FinalRPCExchange, error) {
		if r.corruptOwnerStake {
			value, ok := new(big.Int).SetString(stake, 10)
			if !ok {
				return FinalNativeOwnerStakeState{}, nil, errors.New("fixture owner stake is invalid")
			}
			stake = value.Add(value, big.NewInt(1)).String()
		}
		return FinalNativeOwnerStakeState{HotkeyPublicKey: hotkey, ColdkeyPublicKey: coldkey, StakeRao: stake, Block: head}, r.exchange("evm", "eth_call", head), nil
	}
	for _, reward := range r.evidence.NativeRewards {
		if reward.Hotkey != hotkey {
			continue
		}
		if reward.OwnerStakeBeforeEVM == head && reward.OwnerColdkey == coldkey {
			return result(reward.OwnerStakeBeforeRao)
		}
		if reward.OwnerStakeAfterEVM == head && reward.OwnerColdkey == coldkey {
			return result(reward.OwnerStakeAfterRao)
		}
		if reward.OwnerStakeBeforeEVM == head && reward.ReserveColdkey == coldkey {
			return result(reward.ReserveStakeBeforeRao)
		}
		if reward.OwnerStakeAfterEVM == head && reward.ReserveColdkey == coldkey {
			return result(reward.ReserveStakeAfterRao)
		}
	}
	return FinalNativeOwnerStakeState{}, nil, errors.New("unknown native owner stake checkpoint")
}

func (r *finalTestChainReader) EVMReceipt(_ context.Context, receipt FinalEVMReceipt) (FinalEVMReceiptState, []FinalRPCExchange, error) {
	state := FinalEVMReceiptState{TransactionHash: receipt.TransactionHash, Block: receipt.Block, Status: receipt.Status, LogsHash: receipt.LogsHash}
	if r.corruptRegistration {
		for _, pool := range r.evidence.Pools {
			if pool.Registration.TransactionHash == receipt.TransactionHash {
				state.LogsHash = finalTestHex(0xfe)
				if state.LogsHash == receipt.LogsHash {
					state.LogsHash = finalTestHex(0xfd)
				}
				break
			}
		}
	}
	if r.corruptReserveReceipt {
		for _, addition := range r.evidence.Reserve.PrincipalAdditions {
			if addition.Receipt.TransactionHash == receipt.TransactionHash {
				state.LogsHash = finalTestHex(0xfc)
				if state.LogsHash == receipt.LogsHash {
					state.LogsHash = finalTestHex(0xfb)
				}
				break
			}
		}
	}
	return state, r.exchange("evm", "eth_getTransactionReceipt", receipt.Block), nil
}

func (r *finalTestChainReader) PoolEpoch(_ context.Context, epoch, noID uint64, head ChainHead) (FinalPoolEpochChainState, []FinalRPCExchange, error) {
	for _, row := range r.evidence.Epochs {
		if row.Epoch == epoch && row.NoID == noID {
			return FinalPoolEpochChainState{Epoch: epoch, NoID: noID, PayoutRoot: row.PayoutRoot, ArtifactHash: row.ArtifactHash, FundedRao: row.FundedRao, TotalRao: row.TotalRao, ClaimedRao: row.ClaimedRao, Status: row.Status, Block: head}, r.exchange("evm", "eth_call", head), nil
		}
	}
	return FinalPoolEpochChainState{}, nil, errors.New("unknown pool epoch")
}

func (r *finalTestChainReader) ContractDeployment(_ context.Context, head ChainHead) (FinalContractDeploymentState, []FinalRPCExchange, error) {
	deployment := r.evidence.Deployment
	state := FinalContractDeploymentState{
		CoordinatorProxy: deployment.CoordinatorProxy, CoordinatorImplementation: deployment.CoordinatorImplementation,
		SettlementVault: deployment.SettlementVault, ReserveSink: deployment.ReserveSink, GovernanceOwner: deployment.GovernanceOwner,
		CoordinatorNetuid: deployment.CoordinatorNetuid, CoordinatorSelfColdkey: deployment.CoordinatorSelfColdkey,
		CoordinatorSettlementVault: deployment.CoordinatorSettlementVault, CoordinatorReserveSink: deployment.CoordinatorReserveSink,
		CoordinatorGuardian: deployment.CoordinatorGuardian, CoordinatorActiveGuardian: deployment.CoordinatorActiveGuardian,
		CoordinatorPaused: deployment.CoordinatorPaused, CoordinatorCommitmentOracle: deployment.CoordinatorCommitmentOracle,
		CoordinatorActiveCommitmentOracle: deployment.CoordinatorActiveCommitmentOracle,
		VaultCoordinator:                  deployment.VaultCoordinator, VaultNetuid: deployment.VaultNetuid,
		VaultSelfColdkey: deployment.VaultSelfColdkey, VaultEscrowHotkey: deployment.VaultEscrowHotkey,
		VaultEscrowRegistered: deployment.VaultEscrowRegistered, VaultMinimumClaimTTLBlocks: deployment.VaultMinimumClaimTTLBlocks,
		VaultMinimumTransferTaoRao: deployment.VaultMinimumTransferTaoRao,
		ReserveRecorder:            deployment.ReserveRecorder, ReserveNetuid: deployment.ReserveNetuid,
		ReserveSelfColdkey: deployment.ReserveSelfColdkey, ReserveHotkey: deployment.ReserveHotkey,
		CoordinatorProxyCodeHash: deployment.CoordinatorProxyCodeHash, ImplementationCodeHash: deployment.ImplementationCodeHash,
		SettlementVaultCodeHash: deployment.SettlementVaultCodeHash, ReserveSinkCodeHash: deployment.ReserveSinkCodeHash,
		ObservedImplementationSlot: deployment.ObservedImplementationSlot, PolicyHash: r.evidence.PolicyHash, PolicyVersion: deployment.PolicyVersion,
		PolicyEffectiveEpoch: deployment.PolicyEffectiveEpoch, PolicyEffectiveBlock: deployment.PolicyEffectiveBlock, Block: head,
	}
	if r.corruptCustody {
		state.CoordinatorActiveGuardian = "0x8888888888888888888888888888888888888888"
	}
	return state, r.exchange("evm", "eth_getCode", head), nil
}

func (r *finalTestChainReader) SettlementVaultState(_ context.Context, head ChainHead) (FinalSettlementVaultChainState, []FinalRPCExchange, error) {
	accounting := r.evidence.SettlementAccounting
	if accounting.Before.Block == head {
		return FinalSettlementVaultChainState{
			TotalCapturedRao: accounting.Before.TotalCapturedRao, TotalPaidRao: accounting.Before.TotalPaidRao,
			EscrowAccountedRao: accounting.Before.EscrowAccountedRao, PendingFundingRao: accounting.Before.PendingFundingRao,
			OutstandingLiabilityRao: accounting.Before.OutstandingLiabilityRao, LiveEscrowStakeRao: accounting.Before.LiveEscrowStakeRao,
			Block: head,
		}, r.exchange("evm", "eth_call", head), nil
	}
	if accounting.After.Block == head {
		state := FinalSettlementVaultChainState{
			TotalCapturedRao: accounting.After.TotalCapturedRao, TotalPaidRao: accounting.After.TotalPaidRao,
			EscrowAccountedRao: accounting.After.EscrowAccountedRao, PendingFundingRao: accounting.After.PendingFundingRao,
			OutstandingLiabilityRao: accounting.After.OutstandingLiabilityRao, LiveEscrowStakeRao: accounting.After.LiveEscrowStakeRao,
			Block: head,
		}
		if r.corruptSettlement {
			state.TotalCapturedRao = "999"
		}
		return state, r.exchange("evm", "eth_call", head), nil
	}
	return FinalSettlementVaultChainState{}, nil, errors.New("unknown settlement-vault checkpoint")
}

func (r *finalTestChainReader) ReserveState(_ context.Context, head ChainHead) (FinalReserveState, []FinalRPCExchange, error) {
	reserve := r.evidence.Reserve
	if reserve.Before == head {
		return FinalReserveState{PrincipalRao: reserve.PrincipalBeforeRao, LiveStakeRao: reserve.LiveStakeBeforeRao, Block: head}, r.exchange("evm", "eth_call", head), nil
	}
	if reserve.After == head {
		return FinalReserveState{PrincipalRao: reserve.PrincipalAfterRao, LiveStakeRao: reserve.LiveStakeAfterRao, Block: head}, r.exchange("evm", "eth_call", head), nil
	}
	for _, reward := range r.evidence.NativeRewards {
		if reward.ReserveColdkey == "" {
			continue
		}
		if reward.OwnerStakeBeforeEVM == head {
			return FinalReserveState{PrincipalRao: "0", LiveStakeRao: reward.ReserveStakeBeforeRao, Block: head}, r.exchange("evm", "eth_call", head), nil
		}
		if reward.OwnerStakeAfterEVM == head {
			return FinalReserveState{PrincipalRao: "0", LiveStakeRao: reward.ReserveStakeAfterRao, Block: head}, r.exchange("evm", "eth_call", head), nil
		}
	}
	return FinalReserveState{}, nil, errors.New("unknown reserve checkpoint")
}
