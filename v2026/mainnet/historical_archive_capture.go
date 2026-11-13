// Unsigned archive capture reuses the original proof broker and owned worker.
// RPC observations select candidate bytes; only exact-root execution and strict
// replay can accept them. Neither successful capture nor local files grant
// runtime, finality, treasury, signing or producer enrollment authority.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const historicalArchiveObservationLimit = 64 * 1024 * 1024

type historicalArchiveCaptureRequest struct {
	Capture         historicalCaptureRequest
	Rpc             string
	Expected        identityExpectation
	CacheDirectory  string
	OwnerLocalCache bool
	MaximumBytes    uint64
	MaximumEntries  uint64
}

// One deadline covers immutable input admission, all RPC reads, cache custody,
// proof refill, the original child wait and closing finality checks. Nested
// existing budgets inherit this deadline and cannot extend it.
func runHistoricalArchiveCapture(ctx context.Context, request historicalArchiveCaptureRequest, hooks historicalReplayHooks) (result *historicalCaptureReport, resultErr error) {
	capture := request.Capture
	if capture.Budget < time.Minute || capture.Budget > 15*time.Minute || !bootstrapRootAbsolutePath(capture.Engine.Path) || !planSha256(capture.Engine.Sha256) || !bootstrapRootAbsolutePath(capture.Input.Path) || !planSha256(capture.Input.Sha256) || capture.Nodes != "" || capture.Feed != nil || !bootstrapRootAbsolutePath(request.CacheDirectory) || request.Rpc == "" || request.Expected.NativeChain == "" || !rootCanonicalHash(request.Expected.GenesisHash) || request.Expected.EvmChainId == 0 || request.MaximumBytes < 2*nativeProducerBoundaryReserve || request.MaximumBytes > 64*1024*1024*1024 || request.MaximumEntries < 2*nativeProducerBoundaryEntries || request.MaximumEntries > 1024*1024 {
		return nil, errors.New("archive capture requires exact input/engine, explicit network, private bounded cache and a60–900second budget")
	}
	owner, cancel := context.WithTimeout(ctx, capture.Budget)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, owner.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	raw, digest, err := readBootstrapRootFile(owner, capture.Input.Path, historicalCaptureRequestLimit)
	if err != nil || digest != capture.Input.Sha256 {
		return nil, errors.Join(errors.New("archive capture request differs from its exact pin"), err)
	}
	var input historicalCaptureInput
	if err := decodePlanJson(raw, &input); err != nil {
		return nil, err
	}
	if err := input.validate(); err != nil {
		return nil, err
	}
	if !historicalNativeProfile(input.ObservationProfile) {
		return nil, errors.New("archive proof feed requires the bounded original native observation profile")
	}
	parent, boundary, err := decodeEconomicFinalityHeader(input.ParentHeaderHex)
	if err != nil || boundary.Hash != "0x"+hex.EncodeToString(input.ParentHash[:]) {
		return nil, errors.Join(errors.New("archive capture parent header differs from its exact hash"), err)
	}
	client, err := newRpcClient(request.Rpc, capture.Budget)
	if err != nil {
		return nil, err
	}
	defer client.httpClient.CloseIdleConnections()
	files, err := openNativeEvidenceFilesInScope(owner, request.CacheDirectory, request.MaximumBytes, request.MaximumEntries, request.OwnerLocalCache)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, files.close())
		if resultErr != nil {
			result = nil
		}
	}()
	// This reserves the existing complete native boundary before any new file:
	// <=64MiB proof,20MiB request,64MiB observation,192MiB job and bounded nodes.
	if err := files.admitMargin(1); err != nil {
		return nil, err
	}
	observation, err := client.readHistoricalCaptureRequest(owner, request.Expected, "0x"+hex.EncodeToString(input.ChildHash[:]), input.ObservationProfile)
	if err != nil {
		return nil, err
	}
	var observed historicalCaptureInput
	if err := decodePlanJson([]byte(observation.RequestJSON), &observed); err != nil {
		return nil, err
	}
	// Principal queries are explicit unsigned execution inputs, not RPC facts.
	// All chain-derived fields must still match the exact retained request.
	observed.PrincipalQueries, observed.PrincipalEffects = input.PrincipalQueries, input.PrincipalEffects
	if !reflect.DeepEqual(input, observed) {
		return nil, errors.Join(errRpcIntegrity, errors.New("archive observation differs from exact capture request"))
	}
	observationRaw, err := json.Marshal(historicalCaptureObservationEnvelope{Observation: observation, ContentHash: rootObjectHash(observation)})
	if err != nil {
		return nil, err
	}
	if _, err := files.publish("observations/"+strings.TrimPrefix(monitorReadDigest(observationRaw), "sha256:")+".json", observationRaw, historicalArchiveObservationLimit); err != nil {
		return nil, err
	}
	inputReference, err := files.publish("inputs/"+strings.TrimPrefix(digest, "sha256:")+".json", raw, historicalCaptureRequestLimit)
	if err != nil {
		return nil, err
	}
	nodes, err := files.child("nodes", true)
	if err != nil {
		return nil, err
	}
	if err := nodes.Close(); err != nil {
		return nil, err
	}
	capture.Input = inputReference
	capture.Nodes = filepath.Join(files.path, "nodes")
	capture.Feed = &historicalNativeFeed{files: files, client: client, requestSha256: digest, parentHash: boundary.Hash, parentStateRoot: parent.StateRoot}
	result, err = runHistoricalCapture(owner, capture, hooks)
	if err != nil {
		return nil, err
	}
	if err := client.closeSnapshotFinality(owner, observation.ParentRuntime.Identity); err != nil {
		return nil, err
	}
	if err := client.closeSnapshotFinality(owner, observation.Identity); err != nil {
		return nil, err
	}
	// The exact exported bytes are directly consumable by the independent replay
	// command. Never re-encode the job or overwrite a different retained attempt.
	job := []byte(result.JobJSON)
	if _, err := files.publish("jobs/"+strings.TrimPrefix(monitorReadDigest(job), "sha256:")+".json", job, historicalNativeJobLimit); err != nil {
		return nil, err
	}
	return result, errors.Join(owner.Err(), files.check())
}
