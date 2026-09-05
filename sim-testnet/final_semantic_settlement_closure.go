package main

// The signed terminal batch closes the accepted proof census independently of
// the next native intent. Ordinary cuts retain continuity outside acceptance;
// only completed accepted-window records become public path-proof evidence.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Merges exact signed records without permitting conflicting overlap.
func mergeFinalAttemptCut(cut *validatorpkg.AttemptLedgerCut, records map[uint64]validatorpkg.AttemptRecord) error {
	if cut == nil || records == nil {
		return errors.New("signed attempt merge authority is incomplete")
	}
	for _, record := range cut.Records {
		if prior, exists := records[record.Sequence]; exists && !finalJSONEqual(prior, record) {
			return fmt.Errorf("validator %d operator %d attempt sequence %d differs across signed cuts", cut.Identity.ValidatorID, cut.Identity.NoID, record.Sequence)
		}
		records[record.Sequence] = record
	}
	return nil
}

// Resolves the configured public domain and the observed validator UID.
func finalCollectedAttemptIdentity(cfg *ResolvedConfig, terminal *ScenarioObservation, validatorID uint64, vpk ed25519.PublicKey) (validatorpkg.AttemptLedgerIdentity, error) {
	var uid uint16
	count := 0
	for _, validator := range terminal.Validators {
		if uint64(validator.ValidatorID) == validatorID {
			uid = validator.SelfUID
			count++
		}
	}
	if count != 1 || len(vpk) != ed25519.PublicKeySize {
		return validatorpkg.AttemptLedgerIdentity{}, errors.New("settlement closure validator domain is incomplete")
	}
	return validatorpkg.AttemptLedgerIdentity{DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: strings.ToLower(cfg.Public.Chain.GenesisHash), Netuid: cfg.Netuid, ValidatorID: validatorID,
		ValidatorUID: uid, ValidatorVPK: "0x" + hex.EncodeToString(vpk)}, nil
}

// Authenticates every operator before exposing any records to the collector.
func collectFinalSettlementClosure(data []byte, epoch uint64, identity validatorpkg.AttemptLedgerIdentity, serverKeys map[uint64]map[byte]ed25519.PublicKey, recordsByNO map[uint64]map[uint64]validatorpkg.AttemptRecord) (*validatorpkg.AttemptSettlementClosure, error) {
	closure, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(data, serverKeys)
	if err != nil {
		return nil, err
	}
	if closure.Epoch != epoch || len(closure.Transitions) != len(serverKeys) || len(recordsByNO) != len(serverKeys) {
		return nil, errors.New("settlement closure epoch or configured operator census differs")
	}
	_, err = finalEd25519PublicKey("settlement closure validator", identity.ValidatorVPK)
	if err != nil {
		return nil, err
	}
	for _, transition := range closure.Transitions {
		want := identity
		want.NoID = transition.Identity.NoID
		keys := serverKeys[want.NoID]
		if transition.Identity != want || len(keys) == 0 || recordsByNO[want.NoID] == nil {
			return nil, errors.New("settlement closure signed validator/operator domain differs")
		}
	}
	for _, transition := range closure.Transitions {
		if err := mergeFinalAttemptCut(transition.PreFold.AttemptCut, recordsByNO[transition.Identity.NoID]); err != nil {
			return nil, err
		}
	}
	return closure, nil
}

// Each complete epoch starts exactly where the preceding terminal cut ended.
func verifyFinalSettlementClosureContinuation(previous, current *validatorpkg.AttemptSettlementClosure) error {
	if previous == nil {
		return nil
	}
	if current.Epoch != previous.Epoch+1 || len(current.Transitions) != len(previous.Transitions) {
		return errors.New("settlement closure continuity epoch or operator census differs")
	}
	for index, transition := range current.Transitions {
		prior := previous.Transitions[index].PreFold.AttemptCut
		cut := transition.PreFold.AttemptCut
		if cut.Identity != prior.Identity || cut.FirstSequence != prior.LastSequence+1 || cut.PriorRoot != prior.Root {
			return errors.New("settlement closure continuity has a sequence or signed-root gap")
		}
	}
	return nil
}

// Successor measurements must carry the producer's exact terminal transaction,
// not a separately signed partial or equivocal pre-fold view of the same epoch.
// Full measurement verification remains at its existing replay boundary.
func verifyFinalMeasurementSettlementClosures(data []byte, closures map[uint64]*validatorpkg.AttemptSettlementClosure) error {
	var measurement validatorpkg.ReleaseMeasurementArtifact
	if err := decodeStrictJSONBytes(data, &measurement); err != nil {
		return err
	}
	for _, input := range measurement.Inputs {
		if input.SettlementEpoch == 0 {
			continue
		}
		closure := closures[input.SettlementEpoch-1]
		if closure == nil {
			continue
		}
		var expected *validatorpkg.AttemptSettlementTransition
		for _, transition := range closure.Transitions {
			if transition.Identity.NoID == input.NoID {
				expected = transition
				break
			}
		}
		if expected == nil || input.Stats.SettlementTransition == nil || !finalJSONEqual(expected, input.Stats.SettlementTransition) {
			return fmt.Errorf("validator %d operator %d successor measurement differs from the exact signed terminal closure", measurement.ValidatorID, input.NoID)
		}
	}
	return nil
}

// Requires every accepted terminal block, not merely a count of exported files.
func verifyFinalSettlementClosureLocators(closures []FinalCollectedSettlementClosure, window ScenarioAcceptanceWindow, acceptanceOnly bool) error {
	last := window.FirstEpoch + window.EpochCount - 1
	seen := map[uint64]bool{}
	for index, closure := range closures {
		if index > 0 && closure.Epoch != closures[index-1].Epoch+1 || acceptanceOnly && (closure.Epoch < window.FirstEpoch || closure.Epoch > last) {
			return errors.New("settlement closure epoch census is not consecutive")
		}
		if err := verifyFinalArtifact("settlement closure", closure.Artifact, "validator-settlement-closure"); err != nil {
			return err
		}
		if err := requireFinalHex32("settlement closure terminal hash", closure.Boundary.Hash); err != nil {
			return err
		}
		if closure.Epoch >= window.FirstEpoch && closure.Epoch <= last {
			wantBlock := window.StartBlock + (closure.Epoch-window.FirstEpoch+1)*window.EpochBlocks - 1
			if closure.Boundary.Number != wantBlock {
				return errors.New("settlement closure does not name the actual accepted epoch terminal block")
			}
			seen[closure.Epoch] = true
		}
	}
	if uint64(len(seen)) != window.EpochCount {
		return errors.New("settlement closure omits an accepted epoch")
	}
	return nil
}

// Canonical projection is deliberately bidirectional: deleting a valid tail
// and recomputing unsigned summaries cannot remove it from the signed census.
func finalAcceptedAttemptProofBytes(records map[uint64]validatorpkg.AttemptRecord, first, last uint64) ([]byte, uint64, error) {
	sequences := make([]uint64, 0, len(records))
	for sequence := range records {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	var data []byte
	var count uint64
	for _, sequence := range sequences {
		record := records[sequence]
		if record.Disposition != validatorpkg.AttemptDispositionComplete || record.Proof == nil || record.Proof.Epoch < first || record.Proof.Epoch > last {
			continue
		}
		line, err := json.Marshal(FinalCollectedProofRecord{Schema: finalCollectedProofRecordSchema, Record: *record.Proof})
		if err != nil {
			return nil, 0, err
		}
		data = append(append(data, line...), '\n')
		count++
	}
	return data, count, nil
}

// Joins all accepted batches once per validator, then every operator's exact
// proof projection. Loaded bytes participate in the existing owned-byte cache.
func verifyFinalSettlementClosureArtifacts(evidence *FinalSemanticEvidence, loaded map[string][]byte) error {
	serverKeys := map[uint64]map[byte]ed25519.PublicKey{}
	for _, pool := range evidence.Pools {
		keys := map[byte]ed25519.PublicKey{}
		for _, key := range pool.ServerKeyHistory {
			decoded, err := finalEd25519PublicKey("closure server key", key.PublicKey)
			if err != nil {
				return err
			}
			keys[key.KeyID] = decoded
		}
		serverKeys[pool.NoID] = keys
	}
	for _, validator := range evidence.Validators {
		records := map[uint64]map[uint64]validatorpkg.AttemptRecord{}
		for noID := range serverKeys {
			records[noID] = map[uint64]validatorpkg.AttemptRecord{}
		}
		var closures []FinalCollectedSettlementClosure
		for _, proof := range evidence.PathProofs {
			if proof.ValidatorID != validator.ValidatorID {
				continue
			}
			if closures == nil {
				closures = proof.SettlementClosures
			} else if !finalJSONEqual(closures, proof.SettlementClosures) {
				return errors.New("operator proof artifacts disagree on the validator terminal closure census")
			}
		}
		if err := verifyFinalSettlementClosureLocators(closures, evidence.Window, true); err != nil {
			return err
		}
		identity := validatorpkg.AttemptLedgerIdentity{DeploymentID: evidence.DeploymentID, ChainID: evidence.ChainID, GenesisHash: strings.ToLower(evidence.GenesisHash), Netuid: evidence.Netuid, ValidatorID: validator.ValidatorID, ValidatorUID: validator.UID, ValidatorVPK: validator.PathVPK}
		var previous *validatorpkg.AttemptSettlementClosure
		closuresByEpoch := map[uint64]*validatorpkg.AttemptSettlementClosure{}
		for _, declared := range closures {
			closure, err := collectFinalSettlementClosure(loaded[declared.Artifact.URI], declared.Epoch, identity, serverKeys, records)
			if err != nil {
				return err
			}
			if err := verifyFinalSettlementClosureContinuation(previous, closure); err != nil {
				return err
			}
			previous = closure
			closuresByEpoch[closure.Epoch] = closure
			boundary := closure.Transitions[0].FromBoundary
			if declared.Boundary != (ChainHead{Number: boundary.EVMBlock, Hash: boundary.EVMBlockHash}) {
				return errors.New("signed settlement closure boundary differs from public replay")
			}
		}
		for _, cycle := range validator.Cycles {
			if err := verifyFinalMeasurementSettlementClosures(loaded[cycle.MeasurementArtifact.URI], closuresByEpoch); err != nil {
				return err
			}
		}
		for _, proof := range evidence.PathProofs {
			if proof.ValidatorID != validator.ValidatorID {
				continue
			}
			data, count, err := finalAcceptedAttemptProofBytes(records[proof.NoID], proof.FirstEpoch, proof.LastEpoch)
			if err != nil {
				return err
			}
			if count != proof.ProofCount || !bytes.Equal(data, loaded[proof.Artifact.URI]) {
				return fmt.Errorf("validator %d operator %d proof projection differs from the complete signed settlement census", validator.ValidatorID, proof.NoID)
			}
		}
	}
	return nil
}

// Replays the live collector's exact authority union from the closed graph.
// Attempt summaries and proof file hashes alone are not evidence of completeness.
func verifyFinalCollectedSettlementAuthority(cfg *ResolvedConfig, value *FinalSemanticCollectedInputs, terminal *ScenarioObservation, loaded map[string][]byte) error {
	serverKeys := map[uint64]map[byte]ed25519.PublicKey{}
	for _, operator := range terminal.Operators {
		keys := map[byte]ed25519.PublicKey{}
		for _, key := range operator.VerifyKeys {
			if len(key.PublicKey) != ed25519.PublicKeySize || keys[key.ServerKeyID] != nil {
				return errors.New("collected closure server-key census differs")
			}
			keys[key.ServerKeyID] = append(ed25519.PublicKey(nil), key.PublicKey...)
		}
		if operator.NoID <= 0 || serverKeys[uint64(operator.NoID)] != nil || len(keys) == 0 {
			return errors.New("collected closure operator census differs")
		}
		serverKeys[uint64(operator.NoID)] = keys
	}
	if len(serverKeys) != cfg.Config.Topology.Operators {
		return errors.New("collected closure operator count differs")
	}
	for _, validator := range value.Validators {
		vpk, err := finalEd25519PublicKey("collected closure validator", validator.PathVPK)
		if err != nil {
			return err
		}
		identity, err := finalCollectedAttemptIdentity(cfg, terminal, validator.ValidatorID, vpk)
		if err != nil {
			return err
		}
		records := map[uint64]map[uint64]validatorpkg.AttemptRecord{}
		for noID := range serverKeys {
			records[noID] = map[uint64]validatorpkg.AttemptRecord{}
		}
		var previous *validatorpkg.AttemptSettlementClosure
		closuresByEpoch := map[uint64]*validatorpkg.AttemptSettlementClosure{}
		for _, declared := range validator.SettlementClosures {
			closure, err := collectFinalSettlementClosure(loaded[declared.Artifact.URI], declared.Epoch, identity, serverKeys, records)
			if err != nil {
				return err
			}
			if err := verifyFinalSettlementClosureContinuation(previous, closure); err != nil {
				return err
			}
			previous = closure
			closuresByEpoch[closure.Epoch] = closure
			boundary := closure.Transitions[0].FromBoundary
			if declared.Boundary != (ChainHead{Number: boundary.EVMBlock, Hash: boundary.EVMBlockHash}) {
				return errors.New("collected signed closure boundary differs")
			}
		}
		for _, intents := range [][]FinalCollectedValidatorIntent{validator.Intents, validator.LifecycleIntents} {
			for _, intent := range intents {
				if err := collectFinalAttemptCuts(int(validator.ValidatorID), loaded[intent.Measurement.URI], vpk, serverKeys, records); err != nil {
					return err
				}
				if err := verifyFinalMeasurementSettlementClosures(loaded[intent.Measurement.URI], closuresByEpoch); err != nil {
					return err
				}
			}
		}
		for _, summary := range validator.Attempts {
			var attempts FinalCollectedAttemptRecords
			if err := decodeStrictJSONBytes(loaded[summary.Artifact.URI], &attempts); err != nil {
				return err
			}
			if attempts.ValidatorID != validator.ValidatorID || attempts.NoID != summary.NoID || len(attempts.Records) != len(records[summary.NoID]) {
				return errors.New("collected attempt projection omits signed authority")
			}
			for _, record := range attempts.Records {
				want, ok := records[summary.NoID][record.Sequence]
				if !ok || !finalJSONEqual(want, record) {
					return errors.New("collected attempt projection differs from signed authority")
				}
			}
		}
		for _, proof := range validator.PathProofs {
			data, count, err := finalAcceptedAttemptProofBytes(records[proof.NoID], value.Window.FirstEpoch, value.Window.FirstEpoch+value.Window.EpochCount-1)
			if err != nil {
				return err
			}
			if count != proof.ProofCount || !bytes.Equal(data, loaded[proof.Artifact.URI]) {
				return errors.New("collected proof projection differs from the complete signed terminal census")
			}
		}
	}
	return nil
}

// Waits inside the already established scenario deadline. Missing archives are
// retryable while the independent producer drains; malformed authority is not.
func waitFinalValidatorSettlementClosures(ctx context.Context, cfg *ResolvedConfig, stateRoot string, terminal *ScenarioObservation, window *ScenarioAcceptanceWindow, deadline time.Time, poll time.Duration) error {
	return waitFinalValidatorSettlementClosuresWithWait(ctx, cfg, stateRoot, terminal, window, deadline, poll, func(ctx context.Context, delay time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			return nil
		}
	})
}

// Keeps the real file/key verifier while deterministic tests drive publication
// between polls without substituting the collector or manufacturing a closure.
func waitFinalValidatorSettlementClosuresWithWait(ctx context.Context, cfg *ResolvedConfig, stateRoot string, terminal *ScenarioObservation, window *ScenarioAcceptanceWindow, deadline time.Time, poll time.Duration, wait func(context.Context, time.Duration) error) error {
	if ctx == nil || cfg == nil || terminal == nil || window == nil || poll <= 0 {
		return errors.New("settlement closure wait context is incomplete")
	}
	serverKeys := map[uint64]map[byte]ed25519.PublicKey{}
	for _, operator := range terminal.Operators {
		keys := map[byte]ed25519.PublicKey{}
		for _, key := range operator.VerifyKeys {
			if len(key.PublicKey) != ed25519.PublicKeySize || keys[key.ServerKeyID] != nil {
				return errors.New("terminal closure server key census differs")
			}
			keys[key.ServerKeyID] = append(ed25519.PublicKey(nil), key.PublicKey...)
		}
		if operator.NoID <= 0 || serverKeys[uint64(operator.NoID)] != nil || len(keys) == 0 {
			return errors.New("terminal closure operator census differs")
		}
		serverKeys[uint64(operator.NoID)] = keys
	}
	if len(serverKeys) != cfg.Config.Topology.Operators {
		return errors.New("terminal closure configured operator count differs")
	}
	identities := map[int]validatorpkg.AttemptLedgerIdentity{}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		seed, err := os.ReadFile(filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "operators", "no-1", "client.key"))
		if err != nil || len(seed) != ed25519.SeedSize {
			return fmt.Errorf("validator %d terminal closure path identity is unavailable", validatorID)
		}
		identity, err := finalCollectedAttemptIdentity(cfg, terminal, uint64(validatorID), ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
		if err != nil {
			return err
		}
		identities[validatorID] = identity
	}
	verified := map[string][]byte{}
	return runFinalSettlementClosureWait(ctx, deadline, poll, func(ctx context.Context) (bool, error) {
		missing := false
		for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
			root := filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", validatorID), "state")
			for epoch := window.FirstEpoch; epoch < window.FirstEpoch+window.EpochCount; epoch++ {
				if err := ctx.Err(); err != nil {
					return false, err
				}
				data, err := validatorpkg.ReadAttemptSettlementClosure(root, epoch)
				if errors.Is(err, os.ErrNotExist) {
					missing = true
					continue
				}
				if err != nil {
					return false, err
				}
				name := validatorpkg.AttemptSettlementClosurePath(root, epoch)
				if bytes.Equal(verified[name], data) {
					continue
				}
				records := map[uint64]map[uint64]validatorpkg.AttemptRecord{}
				for noID := range serverKeys {
					records[noID] = map[uint64]validatorpkg.AttemptRecord{}
				}
				closure, err := collectFinalSettlementClosure(data, epoch, identities[validatorID], serverKeys, records)
				if err != nil {
					return false, err
				}
				boundary := closure.Transitions[0].FromBoundary
				wantBlock := window.StartBlock + (epoch-window.FirstEpoch+1)*window.EpochBlocks - 1
				if boundary.EVMBlock != wantBlock {
					return false, errors.New("terminal closure does not use the actual accepted epoch terminal block")
				}
				verified[name] = data
			}
		}
		return !missing, nil
	}, wait)
}

// Owns every scan/wait until completion or the existing scenario deadline.
func runFinalSettlementClosureWait(ctx context.Context, deadline time.Time, poll time.Duration, scan func(context.Context) (bool, error), wait func(context.Context, time.Duration) error) error {
	if ctx == nil || scan == nil || wait == nil || poll <= 0 {
		return errors.New("terminal closure wait dependencies are incomplete")
	}
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		if err := waitCtx.Err(); err != nil {
			return fmt.Errorf("await validator settlement terminal closures: %w", err)
		}
		ready, err := scan(waitCtx)
		if err != nil {
			return err
		}
		if ready {
			return waitCtx.Err()
		}
		if err := wait(waitCtx, poll); err != nil {
			return err
		}
	}
}
