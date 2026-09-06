// Operator slots own mutable provider counters and new ledger tails; the lane
// publishes only after both complete signed record/cut bodies have joined.
package main

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

// The existing prefix and shared transition are immutable for the joined call.
// Newly appended records, private key copies and provider arrays have one owner.
type finalSemanticFixtureAttemptJob struct {
	input           validatorpkg.ReleaseMeasurementInput
	ledger          *finalAttemptFixtureLedger
	validatorKey    ed25519.PrivateKey
	serverKey       ed25519.PrivateKey
	bindings        map[connect.Id]validatorpkg.AttemptBinding
	previous        *validatorpkg.ReleaseMeasurementInput
	transition      *validatorpkg.AttemptSettlementTransition
	validatorID     uint64
	settlementEpoch uint64
	changedEpoch    bool
}

// Copy every provider-owned mutable slice without normalizing nil/empty wires.
// Existing cut/transition inputs are immutable and replaced, never mutated.
func cloneFinalSemanticFixtureMeasurementInput(input validatorpkg.ReleaseMeasurementInput) validatorpkg.ReleaseMeasurementInput {
	if input.Stats.Providers != nil {
		providers := make([]validatorpkg.ReleaseProviderMeasurement, len(input.Stats.Providers))
		for index, provider := range input.Stats.Providers {
			if provider.LatencyBuckets != nil {
				provider.LatencyBuckets = append([]uint64{}, provider.LatencyBuckets...)
			}
			if provider.EgressIPHashHexes != nil {
				provider.EgressIPHashHexes = append([]string{}, provider.EgressIPHashHexes...)
			}
			providers[index] = provider
		}
		input.Stats.Providers = providers
	}
	return input
}

// This is the original complete operator record and public-cut verification
// body, reached by both the cold graph and focused ownership/failure controls.
func (self *finalSemanticFixtureAttemptJob) prepare(work finalSemanticFixtureWorkControl) error {
	if self == nil || self.ledger == nil || work.ctx == nil || len(self.validatorKey) != ed25519.PrivateKeySize || len(self.serverKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("fixture operator ownership is incomplete")
	}
	if err := work.ctx.Err(); err != nil {
		return err
	}
	input, ledger, validatorKey, serverKey := &self.input, self.ledger, self.validatorKey, self.serverKey
	vpk := validatorKey.Public().(ed25519.PublicKey)
	for _, provider := range input.Stats.Providers {
		if len(provider.LatencyBuckets) != 31 {
			return fmt.Errorf("fixture provider %s latency census is invalid", provider.ClientID)
		}
	}
	leave, err := work.enterMeasurement(finalSemanticFixtureOperatorBody, self.validatorID, self.settlementEpoch, input.NoID)
	if err != nil {
		return err
	}
	defer leave()
	work.observeMeasurement(finalSemanticFixtureOperatorBody, self.validatorID, self.settlementEpoch, input.NoID)
	boundary := validatorpkg.AttemptBoundary{SettlementEpoch: input.SettlementEpoch, EVMBlock: input.CutEVMSnapshotBlock, EVMBlockHash: input.CutEVMSnapshotHash}
	tokens := make([]finalAttemptFixtureToken, 0)
	providerIndexByID := map[connect.Id]int{}
	for providerIndex := range input.Stats.Providers {
		provider := &input.Stats.Providers[providerIndex]
		if len(provider.EgressIPHashHexes) == 0 {
			continue
		}
		clientID, err := connect.ParseId(provider.ClientID)
		if err != nil {
			return err
		}
		egressBytes, err := hex.DecodeString(strings.TrimPrefix(provider.EgressIPHashHexes[0], "0x"))
		if err != nil || len(egressBytes) != 32 {
			return fmt.Errorf("invalid fixture egress for %s", provider.ClientID)
		}
		var egress [32]byte
		copy(egress[:], egressBytes)
		provider.Assignments, provider.Confirmations = 1, 1
		provider.LatencyBuckets[0] = 1
		tokens = append(tokens, finalAttemptFixtureToken{clientID: clientID, binding: self.bindings[clientID], egress: egress})
		providerIndexByID[clientID] = providerIndex
	}
	allTokens := append([]finalAttemptFixtureToken(nil), tokens...)
	sequence := uint64(0)
	for len(tokens) != 0 {
		if err := work.ctx.Err(); err != nil {
			return err
		}
		groupSize := finalSemanticFixtureAttemptGroupSize(len(tokens))
		group := append([]finalAttemptFixtureToken(nil), tokens[:groupSize]...)
		tokens = tokens[groupSize:]
		for _, token := range allTokens {
			if len(group) == finalSemanticFixtureMaximumAttemptM-1 {
				break
			}
			present := false
			for _, member := range group {
				present = present || member.clientID == token.clientID
			}
			if present {
				continue
			}
			group = append(group, token)
			provider := &input.Stats.Providers[providerIndexByID[token.clientID]]
			provider.Assignments++
			provider.Confirmations++
			provider.LatencyBuckets[0]++
		}
		if len(group) != finalSemanticFixtureMaximumAttemptM-1 {
			return fmt.Errorf("fixture population cannot fill one policy-depth trail")
		}
		sequence++
		trailID := finalAttemptFixtureID(self.validatorID*100_000_000 + self.settlementEpoch*1_000_000 + input.NoID*10_000 + sequence)
		seedID := finalAttemptFixtureID(9_000_000_000 + self.validatorID*100_000_000 + self.settlementEpoch*1_000_000 + input.NoID*10_000 + sequence)
		nonce := make([]byte, connect.VerifyNonceSize)
		binary.BigEndian.PutUint64(nonce[len(nonce)-8:], self.validatorID*100_000_000+self.settlementEpoch*1_000_000+input.NoID*10_000+sequence)
		m := len(group) + 1
		trail := []connect.Id{seedID}
		hops := []connect.VerifyProofHop{{ClientId: seedID, TimeMs: sequence * 1000}}
		assignments := make([]validatorpkg.AttemptAssignment, 0, len(group))
		for tokenIndex, token := range group {
			walked := append(append([]connect.Id(nil), trail...), token.clientID)
			message, err := connect.BuildVerifyAssignMessage(1, trailID, nonce, vpk, byte(m), walked)
			if err != nil {
				return err
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
			return err
		}
		extendMessage, err := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(m), trail)
		if err != nil {
			return err
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
			if err := ledger.append(validatorpkg.AttemptRecord{Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: m, Assignments: checkpoint, Disposition: validatorpkg.AttemptDispositionPending}); err != nil {
				return err
			}
		}
		if err := ledger.append(validatorpkg.AttemptRecord{Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: m, Assignments: assignments, Disposition: validatorpkg.AttemptDispositionComplete, Proof: proof}); err != nil {
			return err
		}
	}
	firstSequence := uint64(1)
	if prior := self.previous; prior != nil && self.changedEpoch {
		if prior.Stats.AttemptCut == nil {
			return fmt.Errorf("prior fixture operator %d has no attempt cut", input.NoID)
		}
		firstSequence = prior.Stats.AttemptCut.LastSequence + 1
	}
	cut, err := ledger.buildCut(boundary, firstSequence, firstSequence)
	if err != nil {
		return err
	}
	if err := work.ctx.Err(); err != nil {
		return err
	}
	if err := validatorpkg.VerifyAttemptLedgerCut(cut, vpk, map[byte]ed25519.PublicKey{1: serverKey.Public().(ed25519.PublicKey)}); err != nil {
		return err
	}
	input.Stats.AttemptCut = cut
	if self.transition == nil {
		return work.ctx.Err()
	}
	transition := self.transition
	if transition == nil || transition.Identity != cut.Identity {
		return fmt.Errorf("fixture settlement transition operator %d identity changed", input.NoID)
	}
	priorQuality := make(map[string]validatorpkg.AttemptSettlementQuality, len(transition.PostFold))
	for _, quality := range transition.PostFold {
		priorQuality[quality.ClientID] = quality
	}
	for providerIndex := range input.Stats.Providers {
		provider := &input.Stats.Providers[providerIndex]
		quality, exists := priorQuality[provider.ClientID]
		provider.HasPriorQuality = exists
		provider.PriorQualityPPM = quality.QualityPPM
	}
	input.Stats.SettlementTransition = transition
	return work.ctx.Err()
}
