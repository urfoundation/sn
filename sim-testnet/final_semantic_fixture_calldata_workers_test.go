// Each calldata preparation owns source caches and staged artifacts. The
// authenticated immutable graph is borrowed only until all owners have joined.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Receipt replacement binds to the exact borrowed pre-preparation bytes.
type finalSemanticFixtureCalldataReceipt struct {
	path  string
	prior []byte
	data  []byte
}

// One batch preserves initial-to-refresh chronology inside its exclusive owner.
type finalSemanticFixtureCalldataJob struct {
	artifacts       finalSemanticFixtureArtifacts
	receipts        []finalSemanticFixtureCalldataReceipt
	initialCalldata []byte
	refreshCalldata []byte
}

// Fork only private caches after the caller fully authenticated the source.
// No method runs against the caller's mutable source or artifact callback.
func prepareFinalSemanticFixtureGenerationCalldata(source *finalFleetGenerationSource, batch uint64, work finalSemanticFixtureWorkControl, index int) (finalSemanticFixtureCalldataJob, error) {
	job := finalSemanticFixtureCalldataJob{}
	if work.ctx == nil || source == nil || source.archive == nil || source.current == nil || batch < 1 || batch > finalFleetGenerationBatchCount {
		return job, fmt.Errorf("fixture calldata source or batch is incomplete")
	}
	if err := work.ctx.Err(); err != nil {
		return job, err
	}
	var leave func()
	defer func() {
		if leave != nil {
			leave()
		}
	}()
	archive := &finalSemanticArchive{
		ctx: work.ctx, cfg: source.archive.cfg, files: source.archive.files, collected: source.archive.collected,
		artifactDeriver: func(kind, name string, data []byte) (FinalArtifactLocator, error) {
			if leave == nil {
				var err error
				leave, err = work.enter(finalSemanticFixtureGenerationCalldata, index)
				if err != nil {
					return FinalArtifactLocator{}, err
				}
			}
			return job.artifacts.derive(kind, name, data)
		},
	}
	private := &finalFleetGenerationSource{
		archive: archive, evidence: source.evidence, chain: source.chain, events: source.events,
		current: source.current, plans: source.plans, planPaths: source.planPaths, entries: source.entries, entryIndices: source.entryIndices,
		raw:        cloneFinalSemanticFixtureArtifacts(source.raw),
		postProofs: map[string]FinalArtifactLocator{}, versions: map[string]finalFleetGenerationCachedVersion{},
	}
	first := (batch-1)*finalFleetGenerationBatchSize + 1
	last := first + finalFleetGenerationBatchSize - 1
	installed := make([]uint64, 0, finalFleetGenerationBatchSize)
	for fleet := first; fleet <= last; fleet++ {
		installed = append(installed, fleet)
	}
	calldata, err := private.installCalldata(FinalFleetGenerationBatchEvidence{Batch: batch, Generation: 1}, installed)
	if err != nil {
		return job, fmt.Errorf("fixture install batch %d: %w", batch, err)
	}
	job.initialCalldata = append([]byte(nil), calldata...)
	path := fmt.Sprintf("public/fleet-install-batch-%d.json", batch)
	var install FleetInstallBatchEvidence
	if err := json.Unmarshal(source.archive.files[path], &install); err != nil {
		return job, err
	}
	install.CalldataHash = common.BytesToHash(crypto.Keccak256(calldata)).Hex()
	data, err := json.Marshal(install)
	if err != nil {
		return job, err
	}
	job.receipts = append(job.receipts, finalSemanticFixtureCalldataReceipt{path: path, prior: append([]byte(nil), source.archive.files[path]...), data: data})
	if err := work.ctx.Err(); err != nil {
		return job, err
	}
	path = fmt.Sprintf("public/fleet-refresh-batch-%d.json", batch)
	var refresh FleetRefreshBatchEvidence
	if err := json.Unmarshal(source.archive.files[path], &refresh); err != nil {
		return job, err
	}
	calldata, err = private.refreshCalldata(FinalFleetGenerationBatchEvidence{Batch: batch, Generation: 2, FirstFleet: first, LastFleet: last}, refresh)
	if err != nil {
		return job, fmt.Errorf("fixture refresh batch %d: %w", batch, err)
	}
	job.refreshCalldata = append([]byte(nil), calldata...)
	refresh.CalldataHash = common.BytesToHash(crypto.Keccak256(calldata)).Hex()
	data, err = json.Marshal(refresh)
	if err != nil {
		return job, err
	}
	job.receipts = append(job.receipts, finalSemanticFixtureCalldataReceipt{path: path, prior: append([]byte(nil), source.archive.files[path]...), data: data})
	if err := work.ctx.Err(); err != nil {
		return job, err
	}
	return job, job.artifacts.err
}
