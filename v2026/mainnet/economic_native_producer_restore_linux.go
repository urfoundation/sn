//go:build linux

// Offline semantic restore checks the retained job graph without executing it.
// Original checkpoint custody grants no new amounts, approval or signing right.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"github.com/urnetwork/server/v2026/strecovery"
)

type nativeProducerRestoreWorkKey struct{}

// Count actual source read/decode work for this one offline admission. Runtime
// samples do not retain or rebuild this restore-only index.
func nativeProducerRestoreWork(ctx context.Context, stage string, units uint64) {
	if observe, ok := ctx.Value(nativeProducerRestoreWorkKey{}).(func(string, uint64)); ok {
		observe(stage, units)
	}
}

func validateStorageNativeProducerHistory(ctx context.Context, scope storageNativeProducerScope, authorities []nativeProducerReviewedAuthority, prefix string, files []durablevolume.PreparationFile, read func(durablevolume.PreparationFile) ([]byte, error)) error {
	if ctx == nil || len(authorities) == 0 || read == nil {
		return errors.New("native artifact restore lacks original authority or copied reader")
	}
	entries := map[string]durablevolume.PreparationFile{}
	completions := map[uint64]string{}
	intents := map[uint64]nativeProducerIntent{}
	readFiles := map[string]bool{}
	completed := uint64(0)
	if scope.State != nil {
		completed = scope.State.Completed
	}
	original := authorities[0].value
	if scope.Cursor.Number > math.MaxUint32 || original.From.Number > math.MaxUint32 || scope.Cursor.Number < original.From.Number || scope.Cursor.Number-original.From.Number != completed {
		return errors.New("native artifact cursor differs from original completed interval")
	}
	maximum := scope.Cursor.Number
	if maximum < math.MaxUint32 {
		maximum++ // The terminal Substrate height has no representable next job.
	}
	readExact := func(path string) ([]byte, error) {
		entry, present := entries[path]
		if !present || entry.Kind != "file" {
			return nil, errors.New("native artifact original reference is absent from complete source census")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := read(entry)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nativeProducerRestoreWork(ctx, "member-read", 1)
		nativeProducerRestoreWork(ctx, "member-bytes", uint64(len(raw)))
		if uint64(len(raw)) != entry.Bytes || monitorReadDigest(raw) != entry.Sha256 {
			return nil, errors.New("native artifact returned bytes differ from original inventory")
		}
		readFiles[path] = true
		return raw, nil
	}
	for _, file := range files {
		relative := file.Path
		if prefix != "." {
			if file.Path != prefix && !strings.HasPrefix(file.Path, prefix+"/") {
				if file.Kind != "directory" || !strings.HasPrefix(prefix, file.Path+"/") {
					return errors.New("native artifact member is outside its original namespace")
				}
				continue
			}
			relative = strings.TrimPrefix(file.Path, prefix+"/")
			if file.Path == prefix {
				relative = ""
			}
		}
		if relative == "" || file.Kind == "directory" {
			continue
		}
		if _, duplicate := entries[relative]; duplicate {
			return errors.New("native artifact source repeats a logical member")
		}
		entries[relative] = file
		parts := strings.Split(relative, "/")
		if len(parts) >= 2 && storageNativeProducerBoundaryName(parts[0]) {
			number, err := strconv.ParseUint(parts[0][1:11], 10, 32)
			if err != nil || number <= original.From.Number || number > maximum {
				return errors.New("native artifact source has a foreign or skipped execution interval")
			}
			if parts[1] == "complete.json" && len(parts) == 2 {
				if completions[number] != "" {
					return errors.New("native artifact source duplicates one execution height")
				}
				completions[number] = relative
			}
		}
	}
	// Intent values are small and identify the original approval even when a
	// later runtime review exists. No pending intent is rebound to that review.
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !strings.HasPrefix(path, "intents/") || strings.HasSuffix(path, ".pending") {
			continue
		}
		raw, err := readExact(path)
		if err != nil {
			return err
		}
		var intent nativeProducerIntent
		if err := decodePlanJson(raw, &intent); err != nil {
			return err
		}
		expected := fmt.Sprintf("intents/%010d.json", intent.Child.Number)
		if path != expected || intent.Parent.Number >= math.MaxUint32 || intent.Parent.Number+1 != intent.Child.Number || intent.Child.Number <= original.From.Number || intent.Child.Number > maximum || !rootCanonicalHash(intent.Parent.Hash) || !rootCanonicalHash(intent.Child.Hash) {
			return errors.New("native restored intent changed its original interval")
		}
		known := false
		for _, authority := range authorities {
			known = known || authority.reference.Sha256 == intent.AuthorityHash && authority.value.Runtime == intent.Runtime
		}
		if !known {
			return errors.New("native restored intent uses an unapproved original runtime")
		}
		intents[intent.Child.Number] = intent
	}
	for _, path := range paths {
		parts := strings.Split(path, "/")
		if len(parts) < 2 || !storageNativeProducerBoundaryName(parts[0]) {
			continue
		}
		number, err := strconv.ParseUint(parts[0][1:11], 10, 32)
		if err != nil {
			return err
		}
		intent, present := intents[number]
		if !present || strings.TrimPrefix(intent.Child.Hash, "0x") != parts[0][12:] {
			return errors.New("native restored artifact directory lost its exact original intent")
		}
	}
	readReference := func(reference planFileReference) ([]byte, error) {
		relative, err := filepath.Rel(scope.Policy.Execution.Directory, reference.Path)
		if err != nil || !storageMonitorTreePath(relative) || entries[relative].Sha256 != reference.Sha256 {
			return nil, errors.New("native artifact reference changed its original directory or pin")
		}
		return readExact(relative)
	}
	previous, cursor, anchor := scope.Policy.Execution.Producer.Authority.Sha256, original.From, original.Checkpoint
	type predecessor struct {
		boundary  economicEmissionBoundary
		chain     string
		authority int
	}
	reviewed := map[uint64]predecessor{}
	wanted := map[uint64]bool{}
	for _, authority := range authorities[1:] {
		wanted[authority.renewal.Completed] = true
	}
	activeAuthority, adoptedCompleted := 0, uint64(0)
	var windowReference planFileReference
	var window strecovery.NativeExecutionFinalityProof
	var previousFinality *strecovery.NativeExecutionFinality
	last := completed
	if completions[scope.Cursor.Number+1] != "" {
		last++ // An unacknowledged completion stays pending, never advances state.
	}
	if uint64(len(completions)) != last {
		return errors.New("native artifact completion census omitted an original interval")
	}
	for sequence := uint64(1); sequence <= last; sequence++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := completions[original.From.Number+sequence]
		if path == "" {
			return errors.New("native artifact completion chain has a missing original job")
		}
		raw, err := readExact(path)
		if err != nil {
			return err
		}
		var completion nativeProducerCompletion
		if err := decodePlanJson(raw, &completion); err != nil {
			return err
		}
		nativeProducerRestoreWork(ctx, "completion-decoded", 1)
		intent, present := intents[completion.Admission.Child.Number]
		if !present || completion.Schema != nativeProducerCompletionSchema || completion.Sequence != sequence || completion.Previous != previous || completion.Admission.Parent != cursor || completion.Admission.Child != intent.Child || intent.Parent != cursor || completion.AuthorityHash != intent.AuthorityHash || completion.Admission.Runtime != intent.Runtime || !planSha256(completion.OutcomeHash) || path != fmt.Sprintf("b%010d-%s/complete.json", intent.Child.Number, strings.TrimPrefix(intent.Child.Hash, "0x")) {
			return errors.New("native restored completion changed its original cursor, intent or predecessor")
		}
		if completion.AuthorityHash != authorities[activeAuthority].reference.Sha256 {
			next := activeAuthority + 1
			if next >= len(authorities) || completion.AuthorityHash != authorities[next].reference.Sha256 {
				return errors.New("native restored completion skipped or regressed original approval lineage")
			}
			selected := authorities[next]
			review := selected.renewal
			point, present := reviewed[review.Completed]
			if !present || point.boundary != review.After || point.chain != review.CompletionChain || point.authority != activeAuthority || review.Completed <= adoptedCompleted {
				return errors.New("native restored renewal changed its reviewed original predecessor")
			}
			ack := review.acknowledgement(selected.reference, nativeExecutionProducerState{Cursor: cursor, Completed: sequence - 1, CompletionChain: previous})
			if completion.Renewal == nil || !reflect.DeepEqual(*completion.Renewal, ack) {
				return errors.New("native restored first completion lost exact renewal adoption")
			}
			if sequence <= completed {
				ack.FirstCompletion = &planFileReference{Path: filepath.Join(scope.Policy.Execution.Directory, path), Sha256: monitorReadDigest(raw)}
				if scope.State == nil || activeAuthority >= len(scope.State.AuthorityRevisions) || !reflect.DeepEqual(ack, scope.State.AuthorityRevisions[activeAuthority]) {
					return errors.New("native restored renewal changed its acknowledged first completion")
				}
			}
			activeAuthority, adoptedCompleted = next, sequence-1
		} else if completion.Renewal != nil {
			return errors.New("native restored completion repeated an original authority adoption")
		}
		admitted := &authorities[activeAuthority].value
		profile, err := json.Marshal(admitted.Profile)
		if err != nil {
			return err
		}
		admission := completion.Admission
		// The final synced completion may not yet be in the acknowledged chain.
		// Its complete economic policy still comes from the original signer.
		if !reflect.DeepEqual(admission.Treasury, admitted.Treasury) || !reflect.DeepEqual(admission.Yuma, admitted.Yuma) || !reflect.DeepEqual(admission.FeeCensus, admitted.FeeCensus) {
			return errors.New("native restored completion changed original allocation or fee authority")
		}
		if admission.Schema != nativeTreasurySchema(admitted.Treasury, nativeExecutionAdmissionSchema, nativeTreasuryExecutionAdmissionSchema) || admission.Network != scope.Policy.Network || admission.Netuid != scope.Policy.Netuid || admission.Registration != admitted.Registration || admission.Generation != admitted.Generation || admission.ReviewSha256 != admitted.ReviewSha256 || admission.ProfileSha256 != monitorReadDigest(profile) || admission.EngineSha256 != admitted.ReplayEngine.Sha256 || !reflect.DeepEqual(admission.Principal, admitted.Principal) || admission.Signature != "" || admission.FinalityAuthority != "approved-anchor-and-verified-grandpa-original-execution" {
			return errors.New("native restored completion changed runtime, engine or economic authority")
		}
		if completion.Input.Path != filepath.Join(scope.Policy.Execution.Directory, filepath.Dir(path), "input.json") || admission.Job.Path != filepath.Join(scope.Policy.Execution.Directory, filepath.Dir(path), "job.json") || sequence > admitted.MaximumJobs {
			return errors.New("native restored capture or job moved outside its original interval")
		}
		if forecast := completion.ResourceForecast; forecast != nil && (forecast.BytesUpperBound < nativeProducerBoundaryReserve || forecast.BytesUpperBound > admitted.MaximumBytes || forecast.EntriesUpperBound < nativeProducerBoundaryEntries || forecast.EntriesUpperBound > admitted.MaximumEntries) {
			return errors.New("native restored completion changed its approved resource forecast")
		}
		if previousFinality != nil && previousFinality.Certified.Number == cursor.Number && previousFinality.Certified.Hash == cursor.Hash {
			anchor = previousFinality.NextCheckpoint
		}
		if completion.Anchor.Hash() != anchor.Hash() {
			return errors.New("native restored authority checkpoint is not the consumed certified tip")
		}
		if windowReference != completion.Window {
			raw, err := readReference(completion.Window)
			if err != nil {
				return err
			}
			if err := decodePlanJson(raw, &window); err != nil {
				return err
			}
			windowReference = completion.Window
			nativeProducerRestoreWork(ctx, "finality-window-decoded", 1)
		}
		selected := window
		selected.Parent = strecovery.ObservedBlockIdentity{Number: cursor.Number, Hash: cursor.Hash}
		selected.Child = strecovery.ObservedBlockIdentity{Number: admission.Child.Number, Hash: admission.Child.Hash}
		finality, err := strecovery.VerifyNativeExecutionFinality(ctx, scope.Policy.Network.GenesisHash, &anchor, &selected)
		if err != nil {
			return err
		}
		if finality.Certified.Number != completion.Certified.Number || finality.Certified.Hash != completion.Certified.Hash {
			return errors.New("native restored proof changed its original certified tip")
		}
		inputRaw, err := readReference(completion.Input)
		if err != nil {
			return err
		}
		var input historicalCaptureInput
		if err := decodePlanJson(inputRaw, &input); err != nil {
			return err
		}
		if err := input.validate(); err != nil {
			return err
		}
		if input.ParentHeaderHex != finality.ParentHeaderScale || input.ChildHeaderHex != finality.ChildHeaderScale || !reflect.DeepEqual(input.ObservationProfile, admitted.Profile) || !reflect.DeepEqual(input.PrincipalQueries, admitted.Principal.queriesAt(cursor)) || input.PrincipalEffects != admitted.Principal.effectsAt(cursor) {
			return errors.New("native restored input changed original finality, profile or principal query")
		}
		jobRaw, err := readReference(admission.Job)
		if err != nil {
			return err
		}
		var job historicalReplayJob
		if err := decodePlanJson(jobRaw, &job); err != nil {
			return err
		}
		nativeProducerRestoreWork(ctx, "job-decoded", 1)
		if job.Schema != historicalReplaySchema || job.ParentHeaderHex != input.ParentHeaderHex || job.ParentHash != input.ParentHash || job.ChildHeaderHex != input.ChildHeaderHex || job.ChildHash != input.ChildHash || !slices.Equal(job.ExtrinsicsHex, input.ExtrinsicsHex) || job.RuntimeCodeSha256 != input.RuntimeCodeSha256 || job.RuntimeCodeBlake2b256 != input.RuntimeCodeBlake2b256 || job.ExecutionStateVersion != input.ExecutionStateVersion || !reflect.DeepEqual(job.ObservationProfile, input.ObservationProfile) || !reflect.DeepEqual(job.PrincipalQueries, input.PrincipalQueries) || job.PrincipalEffects != input.PrincipalEffects || !storageNativeProducerRuntimeMatches(job, input) {
			return errors.New("native restored job changed its exact original capture input")
		}
		_, maximumNodes, maximumBytes := historicalJobLimits(job.ObservationProfile)
		if len(job.ProofNodesHex) == 0 || len(job.ProofNodesHex) > maximumNodes {
			return errors.New("native restored job lost its bounded original proof")
		}
		proofBytes := 0
		for _, encoded := range job.ProofNodesHex {
			if err := ctx.Err(); err != nil {
				return err
			}
			node, err := historicalReplayHex(encoded, min(historicalProofNodeLimit(job.ObservationProfile), maximumBytes-proofBytes))
			if err != nil || len(node) == 0 {
				return errors.Join(errors.New("native restored job proof exceeds its original profile"), err)
			}
			proofBytes += len(node)
		}
		previous, cursor, previousFinality = rootObjectHash(completion), admission.Child, finality
		if wanted[sequence] {
			reviewed[sequence] = predecessor{boundary: cursor, chain: previous, authority: activeAuthority}
		}
		if sequence == completed {
			state := scope.State
			if state == nil || len(state.AuthorityRevisions) != activeAuthority || state.Completion == nil || state.Completion.Path != filepath.Join(scope.Policy.Execution.Directory, path) || state.Completion.Sha256 != monitorReadDigest(raw) || state.CompletionChain != previous || state.Cursor != cursor || state.AuthorityHash != completion.AuthorityHash || state.Window == nil || *state.Window != completion.Window || state.Certified == nil || *state.Certified != completion.Certified || state.Anchor.Hash() != completion.Anchor.Hash() || !reflect.DeepEqual(state.ResourceForecast, completion.ResourceForecast) {
				return errors.New("native restored history differs from the exact acknowledged checkpoint")
			}
		}
	}
	if intent, present := intents[scope.Cursor.Number+1]; present && last == completed {
		if intent.Parent != scope.Cursor {
			return errors.New("native restored unfinished intent changed original parent")
		}
		if intent.AuthorityHash != authorities[activeAuthority].reference.Sha256 {
			next := activeAuthority + 1
			if next >= len(authorities) || intent.AuthorityHash != authorities[next].reference.Sha256 {
				return errors.New("native restored unfinished intent skipped original authority")
			}
			review := authorities[next].renewal
			point, present := reviewed[review.Completed]
			if !present || point.boundary != review.After || point.chain != review.CompletionChain || point.authority != activeAuthority || review.Completed <= adoptedCompleted {
				return errors.New("native restored unfinished renewal lost its reviewed ancestor")
			}
		}
	}
	// Read every remaining original member, including partial writes. No
	// unacknowledged evidence is discarded just because it is not yet usable.
	for _, path := range paths {
		if readFiles[path] {
			continue
		}
		raw, err := readExact(path)
		if err != nil {
			return err
		}
		_, pending, ok := storageNativeProducerMemberFor(path, scope.Policy.Execution.FeeCensus)
		if !ok {
			return errors.New("native artifact member has no fixed producer grammar")
		}
		if pending {
			nativeProducerRestoreWork(ctx, "pending-retained", 1)
			continue
		}
		if strings.HasPrefix(path, "nodes/") {
			if !storageNativeProducerNodeMatches(strings.TrimPrefix(path, "nodes/"), raw) {
				return errors.New("native restored trie node differs from its content address")
			}
			nativeProducerRestoreWork(ctx, "trie-node-verified", 1)
		} else if !json.Valid(raw) {
			return errors.New("native restored completed artifact is not a complete original frame")
		}
	}
	return ctx.Err()
}
