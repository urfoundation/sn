// Producer renewals extend independently approved runtime and resource scope.
// The initial economic identity, signing key and completion chain never reset.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

const nativeProducerRenewalSchema = "urnetwork-native-execution-producer-renewal-v1"
const maximumNativeProducerRenewals = 128

// MaximumJobs is cumulative, including every original completion. The disk
// byte/inode bounds remain separate. A signed extension is not free storage.
type nativeProducerCapacity struct {
	Jobs              uint64 `json:"completed_jobs"`
	Bytes             uint64 `json:"artifact_bytes"`
	Entries           uint64 `json:"artifact_entries"`
	DescendantHeaders uint64 `json:"descendant_headers"`
}

// Bounds include the actual pre-job metadata census plus one complete admitted
// job reserve. They are a conservative forecast, never a fresh disk measurement.
type nativeProducerResourceForecast struct {
	BytesUpperBound   uint64 `json:"artifact_bytes_upper_bound"`
	EntriesUpperBound uint64 `json:"artifact_entries_upper_bound"`
}

func (self nativeProducerAuthority) capacity() nativeProducerCapacity {
	return nativeProducerCapacity{Jobs: self.MaximumJobs, Bytes: self.MaximumBytes, Entries: self.MaximumEntries, DescendantHeaders: self.MaximumDescendantHeaders}
}

func (self nativeProducerCapacity) validate() error {
	if self.Jobs == 0 || self.Jobs > math.MaxUint32 || self.Bytes < 2*nativeProducerBoundaryReserve || self.Bytes > 64*1024*1024*1024 || self.Entries < 2*nativeProducerBoundaryEntries || self.Entries > 1024*1024 || self.DescendantHeaders > 4094 {
		return errors.New("native producer renewal exceeds separate finite job/byte/entry/finality bounds")
	}
	return nil
}

// An operator may approve a new resource/runtime scope at a completed cursor.
// From/Checkpoint inside Next are still the original anchor. After is the
// exact reviewed ancestor. Already-authorized work may finish before adoption;
// its immutable completion links must connect that ancestor to the live cursor.
type nativeProducerRenewal struct {
	Schema          string                   `json:"schema"`
	Original        planFileReference        `json:"original_authority"`
	Previous        planFileReference        `json:"previous_authority"`
	Ordinal         uint64                   `json:"ordinal"`
	After           economicEmissionBoundary `json:"after_completed_boundary"`
	Completed       uint64                   `json:"completed_jobs"`
	CompletionChain string                   `json:"completion_chain"`
	Next            nativeProducerAuthority  `json:"next_authority"`
	Signature       string                   `json:"signature_ed25519"`
}

func (self nativeProducerRenewal) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > nativeProducerAuthorityMaximum(self.Next.FeeCensus) {
		return nil, errors.Join(errors.New("native producer renewal exceeds its finite approval frame"), err)
	}
	return append([]byte(nativeProducerRenewalSchema+"\x00"), raw...), nil
}

type nativeProducerRenewalAcknowledgement struct {
	Revision         planFileReference        `json:"revision"`
	After            economicEmissionBoundary `json:"after_completed_boundary"`
	Completed        uint64                   `json:"completed_jobs"`
	CompletionChain  string                   `json:"completion_chain"`
	Capacity         nativeProducerCapacity   `json:"capacity"`
	AdoptedAfter     economicEmissionBoundary `json:"adopted_after_completed_boundary"`
	AdoptedCompleted uint64                   `json:"adopted_completed_jobs"`
	AdoptedChain     string                   `json:"adopted_completion_chain"`
	FirstCompletion  *planFileReference       `json:"first_completion,omitempty"`
}

func (self nativeProducerRenewal) acknowledgement(reference planFileReference, state nativeExecutionProducerState) nativeProducerRenewalAcknowledgement {
	return nativeProducerRenewalAcknowledgement{Revision: reference, After: self.After, Completed: self.Completed, CompletionChain: self.CompletionChain, Capacity: self.Next.capacity(), AdoptedAfter: state.Cursor, AdoptedCompleted: state.Completed, AdoptedChain: state.CompletionChain}
}

// This copy is embedded in the first immutable completion. The separately
// checkpointed reference to that completion cannot be part of its own hash.
func (self nativeProducerRenewalAcknowledgement) admission() nativeProducerRenewalAcknowledgement {
	self.FirstCompletion = nil
	return self
}

func (self *nativeProducerSession) verifyRenewalAcknowledgement(ack nativeProducerRenewalAcknowledgement, selected nativeProducerReviewedAuthority) error {
	if ack.FirstCompletion == nil {
		return errors.Join(errRpcIntegrity, errors.New("native producer acknowledgement omitted its original first completion"))
	}
	raw, err := self.files.readReference(*ack.FirstCompletion, nativeProducerCompletionMaximum(self.originalPolicy.Execution.FeeCensus))
	if err != nil {
		return err
	}
	return validateNativeProducerRenewalAcknowledgement(ack, selected, self.files.path, raw)
}

func validateNativeProducerRenewalAcknowledgement(ack nativeProducerRenewalAcknowledgement, selected nativeProducerReviewedAuthority, directory string, raw []byte) error {
	review := selected.renewal
	if review == nil || ack.Revision != selected.reference || ack.After != review.After || ack.Completed != review.Completed || ack.CompletionChain != review.CompletionChain || ack.Capacity != review.Next.capacity() || ack.FirstCompletion == nil || monitorReadDigest(raw) != ack.FirstCompletion.Sha256 {
		return errors.Join(errRpcIntegrity, errors.New("native producer acknowledgement changed its original signed renewal"))
	}
	var first nativeProducerCompletion
	if err := decodePlanJson(raw, &first); err != nil {
		return errors.Join(errRpcIntegrity, err)
	}
	admission := ack.admission()
	expectedPath := filepath.Join(directory, fmt.Sprintf("b%010d-%s", first.Admission.Child.Number, strings.TrimPrefix(first.Admission.Child.Hash, "0x")), "complete.json")
	if first.Schema != nativeProducerCompletionSchema || first.AuthorityHash != ack.Revision.Sha256 || first.Sequence != ack.AdoptedCompleted+1 || first.Previous != ack.AdoptedChain || first.Admission.Parent != ack.AdoptedAfter || first.Admission.Child.Number != ack.AdoptedAfter.Number+1 || !rootCanonicalHash(first.Admission.Child.Hash) || first.Admission.Runtime != selected.value.Runtime || first.Renewal == nil || !reflect.DeepEqual(*first.Renewal, admission) || ack.FirstCompletion.Path != expectedPath {
		return errors.Join(errRpcIntegrity, errors.New("native producer first renewed completion changed its retained adoption"))
	}
	return nil
}

// Only first adoption walks intervening completions. Each immutable digest is
// authenticated by the next retained link; reads never re-execute a job or RPC.
// Subsequent owners check the acknowledged first completion above instead.
func (self *nativeProducerSession) verifyRenewalAncestor(revision *nativeProducerRenewal) error {
	state := self.state
	if state.AuthorityHash != revision.Previous.Sha256 || revision.Completed > state.Completed || revision.After.Number > state.Cursor.Number {
		return errors.Join(errRpcIntegrity, errors.New("native producer renewal is not an ancestor of the retained authority"))
	}
	if len(state.AuthorityRevisions) != 0 && revision.Completed <= state.AuthorityRevisions[len(state.AuthorityRevisions)-1].AdoptedCompleted {
		return errors.Join(errRpcIntegrity, errors.New("native producer renewal predates its original predecessor authority"))
	}
	point, sequence, chain := state.Cursor, state.Completed, state.CompletionChain
	for sequence > revision.Completed {
		if err := self.files.check(); err != nil {
			return err
		}
		name := filepath.Join(fmt.Sprintf("b%010d-%s", point.Number, strings.TrimPrefix(point.Hash, "0x")), "complete.json")
		raw, err := self.files.read(name, nativeProducerCompletionMaximum(self.originalPolicy.Execution.FeeCensus))
		if err != nil {
			return err
		}
		var completion nativeProducerCompletion
		if err := decodePlanJson(raw, &completion); err != nil {
			return errors.Join(errRpcIntegrity, err)
		}
		if completion.Schema != nativeProducerCompletionSchema || rootObjectHash(completion) != chain || completion.AuthorityHash != revision.Previous.Sha256 || completion.Sequence != sequence || completion.Admission.Child != point || completion.Admission.Parent.Number >= math.MaxUint32 || completion.Admission.Parent.Number+1 != point.Number || !rootCanonicalHash(completion.Admission.Parent.Hash) || !planSha256(completion.Previous) {
			return errors.Join(errRpcIntegrity, errors.New("native producer intervening original completion differs from its authenticated chain"))
		}
		point, sequence, chain = completion.Admission.Parent, sequence-1, completion.Previous
	}
	if point != revision.After || chain != revision.CompletionChain {
		return errors.Join(errRpcIntegrity, errors.New("native producer reviewed ancestor differs from retained completion chain"))
	}
	return nil
}

type nativeProducerReviewedAuthority struct {
	reference planFileReference
	value     nativeProducerAuthority
	renewal   *nativeProducerRenewal
}

func nativeProducerRenewalReferences(producer *nativeExecutionProducerPolicy) error {
	if producer == nil {
		return nil
	}
	if len(producer.Renewals) > maximumNativeProducerRenewals {
		return errors.New("native producer renewal references exceed the retained approval profile")
	}
	seen := map[string]bool{producer.Authority.Path: true, producer.Authority.Sha256: true}
	for _, reference := range producer.Renewals {
		if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) || seen[reference.Path] || seen[reference.Sha256] {
			return errors.New("native producer renewal is missing, repeated or aliases an original approval")
		}
		seen[reference.Path], seen[reference.Sha256] = true, true
	}
	return nil
}

func (self nativeProducerRenewal) validate(policy economicEmissionPolicy, previous nativeProducerReviewedAuthority, ordinal uint64) error {
	producer := policy.Execution.Producer
	if self.Schema != nativeProducerRenewalSchema || self.Original != producer.Authority || self.Previous != previous.reference || self.Ordinal != ordinal || self.After.Number <= previous.value.From.Number || self.After.Number > math.MaxUint32 || !rootCanonicalHash(self.After.Hash) || self.Completed != self.After.Number-previous.value.From.Number || self.Completed > previous.value.MaximumJobs || !planSha256(self.CompletionChain) || self.Next.Signature != "" {
		return errors.New("native producer renewal changed its original authority or completed predecessor")
	}
	if previous.renewal != nil && (self.After.Number <= previous.renewal.After.Number || self.Completed <= previous.renewal.Completed) {
		return errors.New("native producer renewal regressed its reviewed cursor or completion count")
	}
	capacity, old := self.Next.capacity(), previous.value.capacity()
	if err := capacity.validate(); err != nil {
		return err
	}
	if capacity.Jobs < old.Jobs || capacity.Bytes < old.Bytes || capacity.Entries < old.Entries || capacity.DescendantHeaders < old.DescendantHeaders || capacity.Jobs-self.Completed < 2 {
		return errors.New("native producer renewal shrank capacity or lost the two-job forecast margin")
	}
	// Only these reviewed fields can change. The original network/generation,
	// provider membership, path custody and initial finality anchor stay exact.
	expected := previous.value
	expected.Runtime, expected.ReviewSha256, expected.Profile = self.Next.Runtime, self.Next.ReviewSha256, self.Next.Profile
	expected.CaptureEngine, expected.ReplayEngine = self.Next.CaptureEngine, self.Next.ReplayEngine
	expected.MaximumJobs, expected.MaximumBytes = capacity.Jobs, capacity.Bytes
	expected.MaximumEntries, expected.MaximumDescendantHeaders = capacity.Entries, capacity.DescendantHeaders
	expected.Signature = ""
	if !reflect.DeepEqual(expected, self.Next) {
		return errors.New("native producer renewal rewrote original economic, provider or custody authority")
	}
	unchanged := previous.value
	unchanged.Signature = ""
	if reflect.DeepEqual(unchanged, self.Next) {
		return errors.New("native producer renewal adds no reviewed runtime, engine or resource scope")
	}
	profile := self.Next.Runtime
	if profile.RuntimeSourceCommit != nativeExecutionRuntimeSource(self.Next.Treasury) || profile.RuntimeVersion.SpecName == "" || profile.RuntimeVersion.SpecVersion == 0 || profile.RuntimeVersion.TransactionVersion == 0 || profile.RuntimeVersion.StateVersion != 1 || !rootCanonicalHash(profile.RuntimeCodeHash) || !rootCanonicalHash(profile.RuntimeMetadataHash) || !planSha256(self.Next.ReviewSha256) || self.Next.Profile == nil || self.Next.Profile.Schema != historicalNativeProfileSchema {
		return errors.New("native producer renewal lacks exact original runtime and callsite approval")
	}
	if err := nativeProducerReviewedProfile(self.Next); err != nil {
		return err
	}
	for _, engine := range []planFileReference{self.Next.CaptureEngine, self.Next.ReplayEngine} {
		if !bootstrapRootAbsolutePath(engine.Path) || !planSha256(engine.Sha256) {
			return errors.New("native producer renewal lost an exact capture or replay engine")
		}
	}
	message, messageErr := self.signingBytes()
	key, keyErr := rootReceiptHex(policy.Execution.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if messageErr != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("native producer renewal lacks the original independent approver signature")
	}
	return nil
}

func nativeProducerReviewedProfile(authority nativeProducerAuthority) error {
	if authority.Profile == nil || authority.ReviewSha256 != fmt.Sprintf("sha256:%x", authority.Profile.SourceReviewSha256) {
		return errors.New("native producer callsite profile names a different original semantic review")
	}
	return errors.Join(authority.Profile.validate(historicalReplayJob{RuntimeCodeSha256: authority.Profile.RuntimeCodeSha256}), authority.Principal.validate(), authority.Yuma.validate(), authority.FeeCensus.validateProducer(authority), authority.Treasury.validate(), validateNativeTreasuryPrincipal(authority.Treasury, authority.Principal), validateNativeTreasuryProfile(authority))
}

func loadNativeProducerAuthorities(ctx context.Context, policy economicEmissionPolicy) ([]nativeProducerReviewedAuthority, error) {
	return readNativeProducerAuthorities(ctx, policy, func(ctx context.Context, reference planFileReference) ([]byte, error) {
		return readNativeProducerRuntimeOriginal(ctx, reference, nativeProducerAuthorityMaximum(policy.Execution.FeeCensus))
	})
}

// Copied approval bytes use the original loader and renewal lineage checks.
// Restoring custody cannot select another key or reinterpret a signed revision.
func readNativeProducerAuthorities(ctx context.Context, policy economicEmissionPolicy, read func(context.Context, planFileReference) ([]byte, error)) ([]nativeProducerReviewedAuthority, error) {
	original, err := readNativeProducerAuthority(ctx, policy, read)
	if err != nil {
		return nil, err
	}
	producer := policy.Execution.Producer
	result := []nativeProducerReviewedAuthority{{reference: producer.Authority, value: *original}}
	for index, reference := range producer.Renewals {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := read(ctx, reference)
		if err != nil {
			return nil, err
		}
		if len(raw) > nativeProducerAuthorityMaximum(policy.Execution.FeeCensus) {
			return nil, errors.New("native producer renewal exceeds its original document profile")
		}
		if monitorReadDigest(raw) != reference.Sha256 {
			return nil, errors.Join(errRpcIntegrity, errors.New("native producer retained renewal bytes differ"))
		}
		var revision nativeProducerRenewal
		if err := decodePlanJson(raw, &revision); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		if err := revision.validate(policy, result[len(result)-1], uint64(index+1)); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		result = append(result, nativeProducerReviewedAuthority{reference: reference, value: revision.Next, renewal: &revision})
	}
	profiles := []rootReceiptProfile{}
	for _, authority := range result {
		if !slices.Contains(profiles, authority.value.Runtime) {
			profiles = append(profiles, authority.value.Runtime)
		}
	}
	if len(profiles) > 64 {
		return nil, errors.New("native producer renewed runtimes exceed the existing 64-artifact read profile")
	}
	return result, nil
}

// Standalone observation uses only the exact signed producer runtime list.
// A configured monitor runtime catalog remains a separate read-purpose gate.
func (self *nativeProducerSession) runtimeCatalog() []monitorEconomicRuntimeEntry {
	result := []monitorEconomicRuntimeEntry{}
	seen := map[rootReceiptProfile]bool{}
	for _, authority := range self.authorities {
		if seen[authority.value.Runtime] {
			continue
		}
		seen[authority.value.Runtime] = true
		result = append(result, monitorEconomicRuntimeEntry{Profile: authority.value.Runtime, ReviewSha256: authority.value.ReviewSha256, Purposes: []string{economicRuntimeStatePurpose, economicRuntimeEventsPurpose, economicRuntimeFeePurpose}})
	}
	return result
}

func (self *nativeProducerSession) useAuthority(index int) error {
	selected := self.authorities[index]
	self.authority = &selected.value
	self.policy = self.originalPolicy
	// Clone both mutable policy levels; original signed config bytes stay exact.
	execution := *self.originalPolicy.Execution
	producer := *execution.Producer
	execution.Producer = &producer
	execution.Engine = selected.value.ReplayEngine
	execution.ReviewSha256 = selected.value.ReviewSha256
	profile, err := json.Marshal(selected.value.Profile)
	if err != nil {
		return err
	}
	execution.ProfileSha256 = monitorReadDigest(profile)
	producer.CaptureEngine = selected.value.CaptureEngine
	producer.MaximumJobs, producer.MaximumBytes = selected.value.MaximumJobs, selected.value.MaximumBytes
	producer.MaximumEntries, producer.MaximumDescendantHeaders = selected.value.MaximumEntries, selected.value.MaximumDescendantHeaders
	self.policy.Execution = &execution
	self.policy.Runtime = selected.value.Runtime
	self.files.policy = producer
	return nil
}

// Pending artifacts belong to their original reviewed engine. A new proposal
// cannot reinterpret them, even when the outer economic checkpoint lost an ack.
func (self *nativeProducerSession) admitRenewal(block economicEmissionBlock, runtime rootReceiptProfile) error {
	index := len(self.state.AuthorityRevisions) + 1
	if index == len(self.authorities) {
		return nil
	}
	selected := self.authorities[index]
	revision := selected.renewal
	if revision.After.Number > self.state.Cursor.Number {
		return nil
	}
	intentName := fmt.Sprintf("intents/%010d.json", block.Boundary.Number)
	resuming := false
	if raw, err := self.files.read(intentName, 16*1024); err == nil {
		var intent nativeProducerIntent
		if err := decodePlanJson(raw, &intent); err != nil {
			return errors.Join(errRpcIntegrity, err)
		}
		if intent.Parent != self.state.Cursor || intent.Child != block.Boundary || intent.Runtime != runtime {
			return errors.Join(errRpcIntegrity, errors.New("native producer pending intent changed its original boundary"))
		}
		if intent.AuthorityHash == self.state.AuthorityHash {
			return nil
		}
		if intent.AuthorityHash != selected.reference.Sha256 {
			return errors.Join(errRpcIntegrity, errors.New("native producer pending intent requires a different original approval"))
		}
		resuming = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// An early runtime review does not stop ordinary old-runtime progress.
	// Activation requires the exact independently authenticated execution runtime.
	if runtime != selected.value.Runtime {
		if resuming {
			return errors.Join(errRpcIntegrity, errors.New("native producer renewed pending intent changed its approved runtime"))
		}
		return nil
	}
	if err := self.verifyRenewalAncestor(revision); err != nil {
		return err
	}
	if self.state.Completed > revision.Next.MaximumJobs || revision.Next.MaximumJobs-self.state.Completed < 2 {
		return errMonitorEconomicCapacity
	}
	if !resuming {
		// Before a new intent, the increased profile must cover the complete
		// retained namespace plus two future job reserves. A retained intent
		// resumes the budget it already admitted; it does not reserve it twice.
		old := self.files.policy
		self.files.policy.MaximumBytes, self.files.policy.MaximumEntries = revision.Next.MaximumBytes, revision.Next.MaximumEntries
		err := self.files.admitMargin(2)
		self.files.policy = old
		if err != nil {
			return err
		}
	}
	self.state.AuthorityRevisions = append(append([]nativeProducerRenewalAcknowledgement(nil), self.state.AuthorityRevisions...), revision.acknowledgement(selected.reference, self.state))
	self.state.AuthorityHash = selected.reference.Sha256
	return self.useAuthority(index)
}

type nativeProducerIntent struct {
	AuthorityHash string                   `json:"authority_hash"`
	Parent        economicEmissionBoundary `json:"parent"`
	Child         economicEmissionBoundary `json:"child"`
	Runtime       rootReceiptProfile       `json:"execution_runtime"`
}

// The same bytes serve old checkpoints and new policies with an appended
// review list. Only acknowledged signed revisions gain operational authority.
func nativeProducerOriginalPolicy(policy economicEmissionPolicy) economicEmissionPolicy {
	if policy.Execution != nil && policy.Execution.Producer != nil {
		execution, producer := *policy.Execution, *policy.Execution.Producer
		producer.Renewals = nil
		execution.Producer = &producer
		policy.Execution = &execution
	}
	return policy
}

type nativeProducerCapacitySummary struct {
	AuthorityHash        string                          `json:"acknowledged_authority_hash"`
	ConfiguredRenewals   int                             `json:"configured_renewals"`
	AcknowledgedRenewals int                             `json:"acknowledged_renewals"`
	Completed            uint64                          `json:"completed_jobs"`
	Capacity             nativeProducerCapacity          `json:"acknowledged_capacity"`
	Forecast             *nativeProducerResourceForecast `json:"resource_forecast,omitempty"`
	JobsRemaining        uint64                          `json:"jobs_remaining"`
	ForecastJobs         uint64                          `json:"next_batch_jobs"`
	CapacityWarning      bool                            `json:"two_times_forecast_warning"`
}

func (self *monitorEconomicNativeState) producerCapacity(policy monitorEconomicNativePolicy) *nativeProducerCapacitySummary {
	if policy.Observation.Execution == nil || policy.Observation.Execution.Producer == nil {
		return nil
	}
	producer := policy.Observation.Execution.Producer
	result := &nativeProducerCapacitySummary{AuthorityHash: producer.Authority.Sha256, ConfiguredRenewals: len(producer.Renewals), Capacity: nativeProducerCapacity{Jobs: producer.MaximumJobs, Bytes: producer.MaximumBytes, Entries: producer.MaximumEntries, DescendantHeaders: producer.MaximumDescendantHeaders}, ForecastJobs: max(policy.BatchBlocks, 1)}
	if self.ExecutionProducer != nil {
		state := self.ExecutionProducer
		result.AuthorityHash, result.AcknowledgedRenewals, result.Completed = state.AuthorityHash, len(state.AuthorityRevisions), state.Completed
		result.Forecast = state.ResourceForecast
		if len(state.AuthorityRevisions) != 0 {
			result.Capacity = state.AuthorityRevisions[len(state.AuthorityRevisions)-1].Capacity
		}
	}
	if result.Completed < result.Capacity.Jobs {
		result.JobsRemaining = result.Capacity.Jobs - result.Completed
	}
	result.CapacityWarning = result.JobsRemaining < 2*result.ForecastJobs || result.AcknowledgedRenewals+1 >= maximumNativeProducerRenewals
	if result.Forecast != nil {
		// Subtraction is safe for valid checkpoints, but diagnostics also refuse
		// a malformed bound rather than wrapping and reporting ample capacity.
		result.CapacityWarning = result.CapacityWarning || result.Forecast.BytesUpperBound > result.Capacity.Bytes || result.Capacity.Bytes-result.Forecast.BytesUpperBound < 2*nativeProducerBoundaryReserve || result.Forecast.EntriesUpperBound > result.Capacity.Entries || result.Capacity.Entries-result.Forecast.EntriesUpperBound < 2*nativeProducerBoundaryEntries
	}
	return result
}
