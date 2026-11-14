// The producer joins independently approved scope, real GRANDPA ancestry and an
// owned original-Wasm capture. Only a complete result proposes a next economic
// checkpoint. Immutable intermediate evidence survives failed reads and a lost
// publication acknowledgement without inventing another accounting cursor.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urnetwork/server/v2026/strecovery"
)

type nativeProducerStateKey struct{}
type nativeProducerSessionKey struct{}

type nativeProducerSession struct {
	finalityApproval []byte
	files            *nativeProducerFiles
	authority        *nativeProducerAuthority
	authorities      []nativeProducerReviewedAuthority
	originalPolicy   economicEmissionPolicy
	policy           economicEmissionPolicy
	state            nativeExecutionProducerState
}

func openNativeProducerSession(ctx context.Context, policy economicEmissionPolicy) (_ *nativeProducerSession, resultErr error) {
	authorities, err := loadNativeProducerAuthorities(ctx, policy)
	if err != nil {
		return nil, err
	}
	files, err := openNativeProducerFiles(ctx, policy.Execution)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, files.close())
		}
	}()
	authority := &authorities[0].value
	session := &nativeProducerSession{files: files, authority: authority, authorities: authorities, originalPolicy: policy, policy: policy}
	retained, _ := ctx.Value(nativeProducerStateKey{}).(*nativeExecutionProducerState)
	if retained == nil {
		if policy.From != authority.From {
			return nil, errors.Join(errRpcIntegrity, errors.New("native producer cannot recreate absent accounting history at a later cursor"))
		}
		session.state = nativeExecutionProducerState{Schema: nativeProducerSchema, AuthorityHash: policy.Execution.Producer.Authority.Sha256, Cursor: authority.From, Anchor: authority.Checkpoint, CompletionChain: policy.Execution.Producer.Authority.Sha256}
	} else {
		if err := retained.validate(policy, policy.From); err != nil {
			return nil, err
		}
		if retained.Cursor.Number < authority.From.Number || retained.Completed != retained.Cursor.Number-authority.From.Number {
			return nil, errors.Join(errRpcIntegrity, errors.New("native producer cumulative jobs differ from the original approved boundary"))
		}
		session.state = *retained
		session.state.AuthorityRevisions = append([]nativeProducerRenewalAcknowledgement(nil), retained.AuthorityRevisions...)
		for index, ack := range retained.AuthorityRevisions {
			selected := authorities[index+1]
			if err := session.verifyRenewalAcknowledgement(ack, selected); err != nil {
				return nil, err
			}
		}
		raw, err := files.readReference(*retained.Completion, nativeProducerCompletionMaximum(policy.Execution.FeeCensus))
		if err != nil {
			return nil, err
		}
		var completion nativeProducerCompletion
		if err := decodePlanJson(raw, &completion); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		if completion.Schema != nativeProducerCompletionSchema || rootObjectHash(completion) != retained.CompletionChain || completion.AuthorityHash != retained.AuthorityHash || completion.Sequence != retained.Completed || completion.Admission.Child != retained.Cursor || completion.Anchor.Hash() != retained.Anchor.Hash() || retained.Window == nil || completion.Window != *retained.Window || retained.Certified == nil || completion.Certified != *retained.Certified || !reflect.DeepEqual(completion.ResourceForecast, retained.ResourceForecast) {
			return nil, errors.Join(errRpcIntegrity, errors.New("native producer acknowledged completion no longer matches original checkpoint"))
		}
		// The exact last job and proof must still be present. Their absence never
		// resets the checkpoint or permits a replacement financial boundary.
		if _, err := files.readReference(completion.Input, historicalCaptureRequestLimit); err != nil {
			return nil, err
		}
		if _, err := files.readReference(completion.Admission.Job, historicalNativeJobLimit); err != nil {
			return nil, err
		}
		if _, err := files.readReference(completion.Window, strecovery.MaximumReceiptFinalityBytes); err != nil {
			return nil, err
		}
	}
	if err := session.useAuthority(len(session.state.AuthorityRevisions)); err != nil {
		return nil, err
	}
	return session, nil
}

// Re-select a child from a retained certified window. The original immutable
// proof is unchanged; verification replays its same outgoing authority sets.
func (self *nativeProducerSession) verifyWindow(ctx context.Context, reference planFileReference, parent, child economicEmissionBoundary) (*strecovery.NativeExecutionFinality, error) {
	raw, err := self.files.readReference(reference, strecovery.MaximumReceiptFinalityBytes)
	if err != nil {
		return nil, err
	}
	var proof strecovery.NativeExecutionFinalityProof
	if err := decodePlanJson(raw, &proof); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	proof.Parent = strecovery.ObservedBlockIdentity{Number: parent.Number, Hash: parent.Hash}
	proof.Child = strecovery.ObservedBlockIdentity{Number: child.Number, Hash: child.Hash}
	result, err := strecovery.VerifyNativeExecutionFinality(ctx, self.policy.Network.GenesisHash, &self.state.Anchor, &proof)
	if err != nil {
		return nil, err
	}
	if self.state.Certified != nil && (result.Certified.Number != self.state.Certified.Number || result.Certified.Hash != self.state.Certified.Hash) {
		return nil, errors.Join(errRpcIntegrity, errors.New("native retained certificate changed its certified tip"))
	}
	return result, nil
}

func (self *nativeProducerSession) finality(ctx context.Context, client *rpcClient, block economicEmissionBlock, directory string) (planFileReference, *strecovery.NativeExecutionFinality, error) {
	parent := economicEmissionBoundary{Number: block.Boundary.Number - 1, Hash: block.Header.ParentHash}
	if self.state.Cursor != parent {
		return planFileReference{}, nil, errors.Join(errRpcIntegrity, errors.New("native producer cannot skip its retained economic predecessor"))
	}
	if self.state.Window != nil && self.state.Certified != nil && self.state.Cursor == *self.state.Certified {
		// The anchor is advanced only after the certified child itself has been
		// accounted. Its old proof authenticates both the tip and next authority set.
		raw, err := self.files.readReference(*self.state.Window, strecovery.MaximumReceiptFinalityBytes)
		if err != nil {
			return planFileReference{}, nil, err
		}
		var proof strecovery.NativeExecutionFinalityProof
		if err := decodePlanJson(raw, &proof); err != nil {
			return planFileReference{}, nil, errors.Join(errRpcIntegrity, err)
		}
		result, err := strecovery.VerifyNativeExecutionFinality(ctx, self.policy.Network.GenesisHash, &self.state.Anchor, &proof)
		if err != nil {
			return planFileReference{}, nil, err
		}
		if result.Certified.Number != parent.Number || result.Certified.Hash != parent.Hash {
			return planFileReference{}, nil, errors.Join(errRpcIntegrity, errors.New("native authority handoff is not the fully consumed certified head"))
		}
		self.state.Anchor = result.NextCheckpoint
		self.state.Window, self.state.Certified = nil, nil
	}
	if self.state.Window != nil {
		result, err := self.verifyWindow(ctx, *self.state.Window, parent, block.Boundary)
		return *self.state.Window, result, err
	}
	child, err := self.files.child(filepath.Join(directory, "finality"), true)
	if err != nil {
		return planFileReference{}, nil, err
	}
	if err := child.Close(); err != nil {
		return planFileReference{}, nil, err
	}
	config := strecovery.NativeExecutionCaptureConfig{Schema: strecovery.NativeExecutionCaptureSchema, Genesis: self.policy.Network.GenesisHash, CheckpointHash: self.state.Anchor.Hash(), Parent: strecovery.ObservedBlockIdentity{Number: parent.Number, Hash: parent.Hash}, Child: strecovery.ObservedBlockIdentity{Number: block.Boundary.Number, Hash: block.Boundary.Hash}, Source: "native-producer", RpcUrl: client.url, MaximumDescendantHeaders: self.policy.Execution.Producer.MaximumDescendantHeaders, RetryWindowSeconds: uint64(client.retryWindow.Seconds())}
	result, err := strecovery.CaptureNativeExecutionFinality(ctx, &self.state.Anchor, config, filepath.Join(self.files.path, directory, "finality"))
	if err = errors.Join(err, self.files.check()); err != nil {
		return planFileReference{}, nil, err
	}
	reference := planFileReference{Path: result.Proof.Path, Sha256: result.Proof.Sha256}
	if _, err := self.files.readReference(reference, strecovery.MaximumReceiptFinalityBytes); err != nil {
		return planFileReference{}, nil, err
	}
	return reference, result.Finality, nil
}

// Only the actual original Initialization capture chooses registration/UID.
// Missing approved providers are not reassigned to a later hotkey. No epoch
// means no recipient allocation, but does not invent a zero-valued epoch.
func nativeProducerRecipients(policy economicEmissionPolicy, authority *nativeProducerAuthority, report *historicalReplayReport, profile *historicalReplayObservationProfile, metadata *types.Metadata) ([]nativeExecutionRecipient, error) {
	if report == nil || report.HookObservations == nil || profile == nil {
		return nil, errors.New("native producer has no authenticated original trace")
	}
	layout, err := newNativeEpochStorageLayout(profile, metadata, policy.Netuid)
	if err != nil {
		return nil, err
	}
	var drains []*historicalReplayObservation
	var drainKeys [3]nativeExecutionDrain
	if layout != nil {
		drainKeys, err = nativeExecutionDrainKeys(metadata, policy.Netuid)
		if err != nil {
			return nil, err
		}
	}
	var epoch *historicalReplayObservation
	for index := range report.HookObservations.Observations {
		record := &report.HookObservations.Observations[index]
		if layout != nil {
			if record.Purpose == "native-uid-census" || record.Purpose == "native-epoch-index" {
				if err := layout.collect(*record); err != nil {
					return nil, err
				}
				continue
			}
			if record.Purpose == "native-drain" {
				netuid, err := nativeEpochDrainNetuid(*record, drainKeys)
				if err != nil {
					return nil, err
				}
				if netuid != uint64(policy.Netuid) {
					continue
				}
				if epoch != nil || len(drains) == len(drainKeys) || record.Operation != "get" || record.KeyHex != drainKeys[len(drains)].Key || record.StorageReturn == nil {
					return nil, errors.New("native provider original drain is incomplete or reordered")
				}
				drains = append(drains, record)
				continue
			}
		}
		if record.Purpose != "native-epoch" {
			continue
		}
		netuid, err := nativeCaptureUint(*record, "netuid", 2)
		if err != nil {
			return nil, err
		}
		if netuid != uint64(policy.Netuid) {
			continue
		}
		if epoch != nil {
			return nil, errors.Join(errRpcIntegrity, errors.New("native producer has ambiguous normalization generations"))
		}
		epoch = record
	}
	result := []nativeExecutionRecipient{}
	if epoch == nil {
		if layout != nil && (layout.hasRecords() || len(drains) != 0) {
			return nil, errors.New("native provider original census has no completed epoch")
		}
		return result, nil
	}
	var hotkeys, uids []byte
	var count int
	if layout != nil {
		raw, err := nativeCapture(*epoch, "registered", -1)
		if err != nil || len(raw)%8 != 0 || len(raw)/8 > int(policy.MaximumUids) {
			return nil, errors.New("native provider registration vector exceeds original UID bound")
		}
		count = len(raw) / 8
		hotkeys, uids, _, err = layout.roster(*epoch, count, drains)
		if err != nil {
			return nil, err
		}
		if err := layout.validateGeneration(*drains[0], policy); err != nil {
			return nil, err
		}
	} else {
		hotkeys, err = nativeCapture(*epoch, "hotkeys", -1)
		if err != nil {
			return nil, err
		}
		if len(hotkeys)%32 != 0 || len(hotkeys)/32 > int(policy.MaximumUids) {
			return nil, errors.New("native provider generation census exceeds original UID bound")
		}
		count = len(hotkeys) / 32
		uids, err = nativeCapture(*epoch, "uids", count*2)
		if err != nil {
			return nil, err
		}
	}
	registered, err := nativeCaptureVector(*epoch, "registered", count)
	if err != nil {
		return nil, err
	}

	approved := map[string]string{}
	for _, provider := range authority.Providers {
		approved[provider.Hotkey] = provider.Coldkey
	}
	seen := map[string]bool{}
	for index := 0; index < count; index++ {
		hotkey := "0x" + hex.EncodeToString(hotkeys[index*32:(index+1)*32])
		if seen[hotkey] || binary.LittleEndian.Uint16(uids[index*2:]) != uint16(index) {
			return nil, errors.Join(errRpcIntegrity, errors.New("native original provider generation is duplicated or misordered"))
		}
		seen[hotkey] = true
		if coldkey, ok := approved[hotkey]; ok {
			result = append(result, nativeExecutionRecipient{Uid: uint16(index), Hotkey: hotkey, Registered: registered[index], Coldkey: coldkey})
		}
	}
	return result, nil
}

// Complete proof capture is atomic per boundary. It never signs an admission;
// the private admission below is a derived input to the existing amount kernel.
func (self *nativeProducerSession) observe(ctx context.Context, client *rpcClient, block economicEmissionBlock, runtime rootReceiptProfile, metadata *types.Metadata) (*nativeExecutionOutcome, error) {
	if err := self.admitRenewal(block, runtime); err != nil {
		return nil, err
	}
	if runtime != self.authority.Runtime {
		return nil, errors.Join(errRootReceiptProfileUnavailable, errors.New("native producer requires an independently reviewed original runtime"))
	}
	if self.state.Completed >= self.policy.Execution.Producer.MaximumJobs {
		return nil, errMonitorEconomicCapacity
	}
	directory := fmt.Sprintf("b%010d-%s", block.Boundary.Number, strings.TrimPrefix(block.Boundary.Hash, "0x"))
	completedRaw, completedErr := self.files.read(filepath.Join(directory, "complete.json"), nativeProducerCompletionMaximum(self.originalPolicy.Execution.FeeCensus))
	var retained *nativeProducerCompletion
	if completedErr == nil {
		var value nativeProducerCompletion
		if err := decodePlanJson(completedRaw, &value); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		if value.Schema != nativeProducerCompletionSchema || value.AuthorityHash != self.state.AuthorityHash || value.Previous != self.state.CompletionChain || value.Sequence != self.state.Completed+1 || value.Admission.Parent != self.state.Cursor || value.Admission.Child != block.Boundary {
			return nil, errors.Join(errRpcIntegrity, errors.New("native pending completion changed its original predecessor or boundary"))
		}
		for _, item := range []struct {
			reference planFileReference
			maximum   int
		}{{reference: value.Input, maximum: historicalCaptureRequestLimit}, {reference: value.Admission.Job, maximum: historicalNativeJobLimit}, {reference: value.Window, maximum: strecovery.MaximumReceiptFinalityBytes}} {
			if _, err := self.files.readReference(item.reference, item.maximum); err != nil {
				return nil, err
			}
		}
		retained = &value
	} else if !errors.Is(completedErr, os.ErrNotExist) {
		return nil, completedErr
	}
	if retained == nil {
		if err := self.files.admit(directory); err != nil {
			return nil, err
		}
	}
	intent := nativeProducerIntent{AuthorityHash: self.state.AuthorityHash, Parent: self.state.Cursor, Child: block.Boundary, Runtime: runtime}
	intentRaw, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	if _, err := self.files.publish(fmt.Sprintf("intents/%010d.json", block.Boundary.Number), append(intentRaw, '\n'), 16*1024); err != nil {
		return nil, err
	}
	reference, finality, err := self.finality(ctx, client, block, directory)
	if err != nil {
		return nil, err
	}
	inputName, jobName := filepath.Join(directory, "input.json"), filepath.Join(directory, "job.json")
	inputRaw, err := self.files.read(inputName, historicalCaptureRequestLimit)
	if errors.Is(err, os.ErrNotExist) {
		expected := identityExpectation{NativeChain: self.policy.Network.NativeChain, GenesisHash: self.policy.Network.GenesisHash, EvmChainId: self.policy.Network.EvmChainId}
		observation, err := client.readHistoricalCaptureRequest(ctx, expected, block.Boundary.Hash, self.authority.Profile)
		if err != nil {
			return nil, err
		}
		var input historicalCaptureInput
		if err := decodePlanJson([]byte(observation.RequestJSON), &input); err != nil {
			return nil, err
		}
		input.PrincipalQueries = self.authority.Principal.queriesAt(self.state.Cursor)
		input.PrincipalEffects = self.authority.Principal.effectsAt(self.state.Cursor)
		inputRaw, err = json.Marshal(input)
		if err != nil {
			return nil, err
		}
		if _, err := self.files.publish(inputName, inputRaw, historicalCaptureRequestLimit); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	var input historicalCaptureInput
	if err := decodePlanJson(inputRaw, &input); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	// Actual finality covers the exact raw parent/child headers fed to the VM.
	// A later certified state root can never stand in for the executed parent.
	profileRaw, err := json.Marshal(input.ObservationProfile)
	if err != nil {
		return nil, err
	}
	if input.PrincipalEffects != self.authority.Principal.effectsAt(self.state.Cursor) || !reflect.DeepEqual(input.PrincipalQueries, self.authority.Principal.queriesAt(self.state.Cursor)) || input.ParentHeaderHex != finality.ParentHeaderScale || input.ChildHeaderHex != finality.ChildHeaderScale || monitorReadDigest(profileRaw) != self.policy.Execution.ProfileSha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer capture input differs from finalized execution boundary or reviewed profile"))
	}
	jobRaw, err := self.files.read(jobName, historicalNativeJobLimit)
	var captured *historicalReplayReport
	if errors.Is(err, os.ErrNotExist) {
		nodes, err := self.files.child("nodes", true)
		if err != nil {
			return nil, err
		}
		if err := nodes.Close(); err != nil {
			return nil, err
		}
		feed := &historicalNativeFeed{files: self.files, client: client, requestSha256: monitorReadDigest(inputRaw), parentHash: finality.Parent.Hash, parentStateRoot: finality.ParentStateRoot}
		capture, err := runHistoricalCapture(ctx, historicalCaptureRequest{Engine: self.authority.CaptureEngine, Input: planFileReference{Path: filepath.Join(self.files.path, inputName), Sha256: monitorReadDigest(inputRaw)}, Nodes: self.policy.Execution.Producer.Nodes, Budget: client.retryWindow, Feed: feed}, historicalReplayHooks{})
		if err != nil {
			return nil, err
		}
		jobRaw = []byte(capture.JobJSON)
		if _, err := self.files.publish(jobName, jobRaw, historicalNativeJobLimit); err != nil {
			return nil, err
		}
		captured = &capture.Replay
	} else if err != nil {
		return nil, err
	}
	var job historicalReplayJob
	if err := decodePlanJson(jobRaw, &job); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	jobRef := planFileReference{Path: filepath.Join(self.files.path, jobName), Sha256: monitorReadDigest(jobRaw)}
	if job.PrincipalEffects != input.PrincipalEffects || !reflect.DeepEqual(job.PrincipalQueries, input.PrincipalQueries) || job.ParentHeaderHex != finality.ParentHeaderScale || job.ChildHeaderHex != finality.ChildHeaderScale || job.RuntimeCodeSha256 != input.RuntimeCodeSha256 || job.RuntimeCodeBlake2b256 != input.RuntimeCodeBlake2b256 || rootObjectHash(job.ExtrinsicsHex) != rootObjectHash(input.ExtrinsicsHex) {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer retained job differs from original finalized capture input"))
	}
	// Capture and replay are different fixed ELF protocols. The separately
	// approved replay image consumes the same immutable job on first use too.
	report, err := runHistoricalReplay(ctx, historicalReplayRequest{Engine: self.authority.ReplayEngine, Job: jobRef, Budget: client.retryWindow}, historicalReplayHooks{})
	if err != nil {
		return nil, err
	}
	if captured != nil && !reflect.DeepEqual(captured, report) {
		return nil, errors.Join(errRpcIntegrity, errors.New("native capture and approved replay disagree on the original job"))
	}
	providers, err := nativeProducerRecipients(self.policy, self.authority, report, job.ObservationProfile, metadata)
	if err != nil {
		return nil, err
	}
	admission := nativeExecutionAdmission{Treasury: self.authority.Treasury, Yuma: self.authority.Yuma, Principal: self.authority.Principal, Schema: nativeTreasurySchema(self.authority.Treasury, nativeExecutionAdmissionSchema, nativeTreasuryExecutionAdmissionSchema), Network: self.policy.Network, Netuid: self.policy.Netuid, Registration: self.authority.Registration, Generation: self.authority.Generation, Parent: self.state.Cursor, Child: block.Boundary, Runtime: runtime, ReviewSha256: self.policy.Execution.ReviewSha256, ProfileSha256: self.policy.Execution.ProfileSha256, EngineSha256: self.policy.Execution.Engine.Sha256, Job: jobRef, Providers: providers, FinalityAuthority: "approved-anchor-and-verified-grandpa-original-execution"}
	admission.FeeCensus = self.authority.FeeCensus
	outcome, err := validateNativeExecutionReplay(ctx, self.policy, admission, block, metadata, job, report)
	if err != nil {
		return nil, err
	}
	outcome.ProducerAuthorityHash = self.state.AuthorityHash
	outcome.FinalityProofHash = finality.ProofHash
	outcome.ContentHash = outcome.hash()
	outcome.CertifiedWindow, err = self.finalityProjection(ctx, reference, *outcome, finality)
	if err != nil {
		return nil, err
	}
	outcome.FeeCensus, err = deriveNativeFeeCensus(ctx, self.authority.FeeCensus, self.state.Cursor, job, report, *outcome)
	if err != nil {
		return nil, nativeExecutionDerivationError(err)
	}
	completion := nativeProducerCompletion{Schema: nativeProducerCompletionSchema, AuthorityHash: self.state.AuthorityHash, Previous: self.state.CompletionChain, Sequence: self.state.Completed + 1, Input: planFileReference{Path: filepath.Join(self.files.path, inputName), Sha256: monitorReadDigest(inputRaw)}, Admission: admission, Anchor: self.state.Anchor, Window: reference, Certified: economicEmissionBoundary{Number: finality.Certified.Number, Hash: finality.Certified.Hash}, OutcomeHash: outcome.ContentHash}
	completion.ResourceForecast = self.files.forecast
	if len(self.state.AuthorityRevisions) != 0 {
		ack := self.state.AuthorityRevisions[len(self.state.AuthorityRevisions)-1]
		if ack.FirstCompletion == nil {
			admission := ack.admission()
			completion.Renewal = &admission
		}
	}
	if retained != nil {
		completion.ResourceForecast = retained.ResourceForecast
	}
	raw, err := json.Marshal(completion)
	if err != nil {
		return nil, err
	}
	completed, err := self.files.publish(filepath.Join(directory, "complete.json"), append(raw, '\n'), nativeProducerCompletionMaximum(self.originalPolicy.Execution.FeeCensus))
	if err != nil {
		return nil, err
	}
	if completion.Renewal != nil {
		self.state.AuthorityRevisions[len(self.state.AuthorityRevisions)-1].FirstCompletion = &completed
	}
	self.state = nativeExecutionProducerState{Schema: nativeProducerSchema, AuthorityHash: self.state.AuthorityHash, AuthorityRevisions: self.state.AuthorityRevisions, ResourceForecast: completion.ResourceForecast, Cursor: block.Boundary, Anchor: self.state.Anchor, Window: &reference, Certified: &completion.Certified, Completed: completion.Sequence, Completion: &completed, CompletionChain: rootObjectHash(completion)}
	return outcome, nil
}
