package main

// final_semantic_fleet_generation_artifact.go replays the sealed ordinary
// fleet source graph without writing files. It ensures FINAL evidence carries
// the exact predecessor plans, journal, signed inputs, postconditions, and
// receipt logs that originally produced its 202-fleet lineage.

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"
)

// Holds the wire fields emitted for one retained EVM receipt. The source
// builder serializes this exact shape before deriving the receipt locator, so
// the artifact verifier can rebuild its event index without an RPC endpoint.
type finalFleetGenerationReceiptArtifact struct {
	Status          string                 `json:"status"`
	TransactionHash string                 `json:"transaction_hash"`
	Block           ChainHead              `json:"block"`
	Logs            []finalCanonicalEVMLog `json:"logs"`
}

// Rebuilds the ordinary-fleet source graph from the authenticated lineage
// artifact and all already-loaded derived objects. The injected deriver never
// writes: it accepts only byte-for-byte matches from the caller's immutable
// cache, then the production source builder validates the plan/journal/action
// and signed manifest/binding semantics a second time.
func verifyFinalFleetGenerationArtifacts(evidence *FinalSemanticEvidence, cache map[string][]byte) error {
	if evidence == nil || evidence.FleetGeneration == nil || len(cache) == 0 {
		return errors.New("ordinary fleet generation artifact inputs are unavailable")
	}
	lineage := evidence.FleetGeneration
	artifactData, found := cache[lineage.Artifact.URI]
	if !found {
		return errors.New("ordinary fleet generation lineage artifact is not loaded")
	}
	files, err := finalFleetGenerationArtifactFiles(evidence, artifactData)
	if err != nil {
		return err
	}
	planData, found := files["launch-foundation/plan.json"]
	if !found || !bytes.Equal(planData, cache[evidence.PlanArtifact.URI]) {
		return errors.New("ordinary fleet generation source plan differs from the approved plan artifact")
	}
	plan, err := verifyFinalSetupPlanArtifact(evidence, planData)
	if err != nil {
		return fmt.Errorf("ordinary fleet generation source plan: %w", err)
	}
	batcher, _, err := finalPlanFleetBatcher(plan)
	if err != nil {
		return fmt.Errorf("ordinary fleet generation source batcher: %w", err)
	}
	events, err := finalFleetGenerationArtifactEvents(lineage, cache)
	if err != nil {
		return err
	}
	archive := &finalSemanticArchive{
		files:           files,
		artifactDeriver: finalFleetGenerationArtifactDeriver(cache),
	}
	replayed := *evidence
	replayed.FleetGeneration = nil
	chain := &FinalCollectedChainSnapshot{FleetBatcher: strings.ToLower(batcher.Hex())}
	if err := archive.buildFleetGeneration(&replayed, chain, events); err != nil {
		return fmt.Errorf("replay ordinary fleet generation source graph: %w", err)
	}
	if !finalJSONEqual(replayed.FleetGeneration, lineage) {
		return errors.New("ordinary fleet generation source graph differs from sealed lineage")
	}
	return nil
}

// Decodes the embedded source namespace and rejects aliases, traversal, and
// alternate ordering before it is exposed to the reconstruction builder.
func finalFleetGenerationArtifactFiles(evidence *FinalSemanticEvidence, data []byte) (map[string][]byte, error) {
	if evidence == nil || len(data) == 0 {
		return nil, errors.New("ordinary fleet generation lineage artifact is empty")
	}
	var artifact finalFleetGenerationLineageArtifact
	if err := decodeStrictJSONBytes(data, &artifact); err != nil {
		return nil, fmt.Errorf("decode ordinary fleet generation lineage artifact: %w", err)
	}
	if artifact.Schema != finalFleetGenerationLineageSchema || artifact.DeploymentID != evidence.DeploymentID || !strings.EqualFold(artifact.PlanHash, evidence.PlanHash) || len(artifact.Files) == 0 {
		return nil, errors.New("ordinary fleet generation lineage artifact identity differs from semantic evidence")
	}
	files := make(map[string][]byte, len(artifact.Files))
	previous := ""
	for index, item := range artifact.Files {
		if item.Path == "" || path.IsAbs(item.Path) || item.Path != path.Clean(item.Path) || item.Path == "." || strings.HasPrefix(item.Path, "../") || strings.Contains(item.Path, "\\") || previous != "" && previous >= item.Path || files[item.Path] != nil || len(item.Data) == 0 || item.SizeBytes != uint64(len(item.Data)) || item.ContentHash != bytesSHA256(item.Data) {
			return nil, fmt.Errorf("ordinary fleet generation lineage file %d is unsafe, unordered, duplicated, or content-address mismatched", index)
		}
		previous = item.Path
		files[item.Path] = append([]byte(nil), item.Data...)
	}
	return files, nil
}

// Converts every retained receipt proof into the narrow event index consumed
// by source replay. Comparing the complete ordered log list here prevents a
// cached proof from omitting a release event before ABI decoding begins.
func finalFleetGenerationArtifactEvents(lineage *FinalFleetGenerationLineageEvidence, cache map[string][]byte) (*finalSemanticEventIndex, error) {
	if lineage == nil || len(cache) == 0 {
		return nil, errors.New("ordinary fleet generation receipt artifacts are unavailable")
	}
	result := &finalSemanticEventIndex{byName: map[string][]finalSemanticEvent{}, byTx: map[string][]finalCanonicalEVMLog{}}
	add := func(write FinalFleetGenerationWriteEvidence) error {
		data, found := cache[write.Receipt.Proof.URI]
		if !found {
			return fmt.Errorf("ordinary fleet generation receipt %s is not loaded", write.Receipt.TransactionHash)
		}
		var receipt finalFleetGenerationReceiptArtifact
		if err := decodeStrictJSONBytes(data, &receipt); err != nil {
			return fmt.Errorf("decode ordinary fleet generation receipt %s: %w", write.Receipt.TransactionHash, err)
		}
		if receipt.Status != write.Receipt.Status || !strings.EqualFold(receipt.TransactionHash, write.Receipt.TransactionHash) || receipt.Block != write.Receipt.Block || len(receipt.Logs) != len(write.Events) {
			return fmt.Errorf("ordinary fleet generation receipt %s differs from its sealed identity", write.Receipt.TransactionHash)
		}
		logsHash, err := finalCanonicalReceiptLogsHash(receipt.Logs)
		if err != nil || !strings.EqualFold(logsHash, write.Receipt.LogsHash) {
			return stateMismatchError(err, "ordinary fleet generation receipt %s logs differ", write.Receipt.TransactionHash)
		}
		if _, duplicate := result.byTx[write.Receipt.TransactionHash]; duplicate {
			return fmt.Errorf("ordinary fleet generation receipt %s is reused by another write", write.Receipt.TransactionHash)
		}
		for index := range receipt.Logs {
			if !finalSemanticCanonicalLogEqual(receipt.Logs[index], write.Events[index].Log) {
				return fmt.Errorf("ordinary fleet generation receipt %s log %d differs from its event projection", write.Receipt.TransactionHash, index)
			}
		}
		result.byTx[write.Receipt.TransactionHash] = append([]finalCanonicalEVMLog(nil), receipt.Logs...)
		return nil
	}
	for _, batch := range lineage.Batches {
		for _, write := range batch.CarriedHistory {
			if err := add(write); err != nil {
				return nil, err
			}
		}
		if batch.BatchWrite != nil {
			if err := add(*batch.BatchWrite); err != nil {
				return nil, err
			}
			continue
		}
		if batch.Generation != 1 {
			return nil, fmt.Errorf("ordinary fleet generation refresh batch %d has no receipt", batch.Batch)
		}
	}
	return result, nil
}

// Resolves only byte-identical derived output already retained by the caller.
// It makes a source replay fail if one manifest, postcondition, receipt, or
// aggregate lineage artifact would produce a different locator or payload.
func finalFleetGenerationArtifactDeriver(cache map[string][]byte) finalSemanticArtifactDeriver {
	return func(kind, uri string, data []byte) (FinalArtifactLocator, error) {
		if kind == "" || uri == "" || len(data) == 0 {
			return FinalArtifactLocator{}, errors.New("ordinary fleet generation derived artifact identity is incomplete")
		}
		cached, found := cache[uri]
		if !found || !bytes.Equal(cached, data) {
			return FinalArtifactLocator{}, fmt.Errorf("ordinary fleet generation derived artifact %s is absent or differs", uri)
		}
		return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}, nil
	}
}
