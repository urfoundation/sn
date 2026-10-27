// One independent approval admits an original runtime/layout/engine and initial
// GRANDPA authority state. Ordinary blocks then advance by verified evidence;
// the producer possesses no signing key and cannot enroll a new provider.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"

	"github.com/urnetwork/server/v2026/strecovery"
)

const nativeProducerSchema = "urnetwork-native-execution-producer-v1"
const nativeProducerAuthoritySchema = "urnetwork-native-execution-producer-authority-v1"
const nativeProducerCompletionSchema = "urnetwork-native-execution-completion-v1"
const nativeProducerAuthorityLimit = 1024 * 1024
const nativeProducerFeeAuthorityLimit = 4 * 1024 * 1024
const nativeProducerCompletionLimit = 2 * 1024 * 1024
const nativeProducerFeeCompletionLimit = 4 * 1024 * 1024
const nativeProducerBoundaryReserve = 512 * 1024 * 1024
const nativeProducerBoundaryEntries = 4*32768 + 8192 + 128 + 16 + historicalNativeProofNodes

// Full original provider pairs and their independent fee account roster need
// a distinct finite frame. Nil authority retains the exact legacy byte bound.
func nativeProducerAuthorityMaximum(fees *nativeFeeCensusPolicy) int {
	if fees != nil {
		return nativeProducerFeeAuthorityLimit
	}
	return nativeProducerAuthorityLimit
}

// Full original provider generations and fee accounts retain twice their
// serialized census inside one immutable completion. Legacy owners stay exact.
func nativeProducerCompletionMaximum(fees *nativeFeeCensusPolicy) int {
	if fees != nil {
		return nativeProducerFeeCompletionLimit
	}
	return nativeProducerCompletionLimit
}

// These are separate finite deployment dimensions. Growth needs a separately
// reviewed policy; exhaustion holds the original cursor and never deletes jobs.
type nativeExecutionProducerPolicy struct {
	Schema                   string              `json:"schema"`
	Authority                planFileReference   `json:"authority"`
	Renewals                 []planFileReference `json:"renewals,omitempty"`
	CaptureEngine            planFileReference   `json:"capture_engine"`
	Nodes                    string              `json:"parent_trie_nodes_directory"`
	MaximumJobs              uint64              `json:"maximum_completed_jobs"`
	MaximumBytes             uint64              `json:"maximum_artifact_bytes"`
	MaximumEntries           uint64              `json:"maximum_artifact_entries"`
	MaximumDescendantHeaders uint64              `json:"maximum_descendant_headers"`
}

func (self *nativeExecutionProducerPolicy) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != nativeProducerSchema || !bootstrapRootAbsolutePath(self.Authority.Path) || !planSha256(self.Authority.Sha256) || !bootstrapRootAbsolutePath(self.CaptureEngine.Path) || !planSha256(self.CaptureEngine.Sha256) || !bootstrapRootAbsolutePath(self.Nodes) || self.MaximumJobs == 0 || self.MaximumJobs > 4096 || self.MaximumBytes < 2*nativeProducerBoundaryReserve || self.MaximumBytes > 64*1024*1024*1024 || self.MaximumEntries < 2*nativeProducerBoundaryEntries || self.MaximumEntries > 1024*1024 || self.MaximumDescendantHeaders > 4094 {
		return errors.New("native producer requires independent authority, original trie source and separate finite job/byte/entry/finality capacities")
	}
	return nativeProducerRenewalReferences(self)
}

// Provider membership is approved once by hotkey and coldkey. UIDs and their
// registration heights are observed inside the original Initialization trace,
// so a same-block extrinsic takeover cannot relabel the earlier recipient.
type nativeProducerProvider struct {
	Hotkey  string `json:"hotkey"`
	Coldkey string `json:"coldkey"`
}

type nativeProducerAuthority struct {
	Treasury                 *nativeTreasuryAuthority            `json:"treasury_authority,omitempty"`
	FeeCensus                *nativeFeeCensusPolicy              `json:"complete_fee_authority,omitempty"`
	Yuma                     *nativeYumaPolicy                   `json:"complete_allocation_authority,omitempty"`
	Principal                *nativePrincipalPolicy              `json:"opening_principal_authority,omitempty"`
	Schema                   string                              `json:"schema"`
	Network                  planNetwork                         `json:"network"`
	Netuid                   uint16                              `json:"netuid"`
	Registration             uint64                              `json:"subnet_registration_block"`
	Generation               uint64                              `json:"subnet_generation"`
	From                     economicEmissionBoundary            `json:"from"`
	Runtime                  rootReceiptProfile                  `json:"execution_runtime"`
	ReviewSha256             string                              `json:"runtime_semantics_review_sha256"`
	Profile                  *historicalReplayObservationProfile `json:"original_callsite_profile"`
	CaptureEngine            planFileReference                   `json:"capture_engine"`
	ReplayEngine             planFileReference                   `json:"replay_engine"`
	Directory                string                              `json:"artifact_directory"`
	Nodes                    string                              `json:"parent_trie_nodes_directory"`
	MaximumJobs              uint64                              `json:"maximum_completed_jobs"`
	MaximumBytes             uint64                              `json:"maximum_artifact_bytes"`
	MaximumEntries           uint64                              `json:"maximum_artifact_entries"`
	MaximumDescendantHeaders uint64                              `json:"maximum_descendant_headers"`
	Checkpoint               strecovery.NativeFinalityCheckpoint `json:"initial_finality_checkpoint"`
	Providers                []nativeProducerProvider            `json:"providers"`
	Signature                string                              `json:"signature_ed25519"`
}

func (self nativeProducerAuthority) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > nativeProducerAuthorityMaximum(self.FeeCensus) {
		return nil, errors.Join(errors.New("native producer authority exceeds its finite frame"), err)
	}
	return append([]byte(nativeTreasurySchema(self.Treasury, nativeProducerAuthoritySchema, nativeTreasuryProducerAuthoritySchema)+"\x00"), raw...), nil
}

func loadNativeProducerAuthority(ctx context.Context, policy economicEmissionPolicy) (*nativeProducerAuthority, error) {
	return readNativeProducerAuthority(ctx, policy, func(ctx context.Context, reference planFileReference) ([]byte, error) {
		return nativeProducerReadApprovalFor(ctx, reference, policy.Execution.FeeCensus)
	})
}

// The restore adapter supplies the same original signed bytes through a copied
// source reader. Signature, runtime, layout and engine admission remain shared.
func readNativeProducerAuthority(ctx context.Context, policy economicEmissionPolicy, read func(context.Context, planFileReference) ([]byte, error)) (*nativeProducerAuthority, error) {
	if policy.Execution == nil || policy.Execution.Producer == nil {
		return nil, errors.New("native producer is not independently configured")
	}
	execution, producer := policy.Execution, policy.Execution.Producer
	if err := execution.FeeCensus.validate(); err != nil {
		return nil, err
	}
	if err := execution.validate(); err != nil {
		return nil, err
	}
	raw, err := read(ctx, producer.Authority)
	if err != nil {
		return nil, err
	}
	if len(raw) > nativeProducerAuthorityMaximum(execution.FeeCensus) {
		return nil, errors.New("native producer original approval exceeds its original document profile")
	}
	if monitorReadDigest(raw) != producer.Authority.Sha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer original approval bytes differ"))
	}
	var authority nativeProducerAuthority
	if err := decodePlanJson(raw, &authority); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if !reflect.DeepEqual(authority.FeeCensus, execution.FeeCensus) {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer changed original fee participant authority"))
	}
	profileRaw, err := json.Marshal(authority.Profile)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(authority.Treasury, execution.Treasury) || !reflect.DeepEqual(authority.Yuma, execution.Yuma) || !reflect.DeepEqual(authority.Principal, execution.Principal) || authority.Schema != nativeTreasurySchema(execution.Treasury, nativeProducerAuthoritySchema, nativeTreasuryProducerAuthoritySchema) || authority.Network != policy.Network || authority.Netuid != policy.Netuid || policy.SubnetRegistrationBlock == nil || policy.SubnetGeneration == nil || authority.Registration != *policy.SubnetRegistrationBlock || authority.Generation != *policy.SubnetGeneration || authority.Runtime != policy.Runtime || authority.Runtime.RuntimeSourceCommit != nativeExecutionRuntimeSource(authority.Treasury) || authority.ReviewSha256 != execution.ReviewSha256 || authority.Profile == nil || authority.Profile.Schema != historicalNativeProfileSchema || monitorReadDigest(profileRaw) != execution.ProfileSha256 || authority.CaptureEngine != producer.CaptureEngine || authority.ReplayEngine != execution.Engine || authority.Directory != execution.Directory || authority.Nodes != producer.Nodes || authority.Nodes != filepath.Join(execution.Directory, "nodes") || authority.MaximumJobs != producer.MaximumJobs || authority.MaximumBytes != producer.MaximumBytes || authority.MaximumEntries != producer.MaximumEntries || authority.MaximumDescendantHeaders != producer.MaximumDescendantHeaders || len(authority.Providers) > int(policy.MaximumUids) {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer reusable approval differs from original execution/provider/capacity policy"))
	}
	if err := nativeProducerReviewedProfile(authority); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	anchor, err := strecovery.NativeExecutionCheckpointIdentity(ctx, policy.Network.GenesisHash, &authority.Checkpoint)
	if err != nil {
		return nil, err
	}
	if authority.From.Number != anchor.Number || authority.From.Hash != anchor.Hash || authority.From.Number > policy.From.Number {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer initial finalized anchor differs from original window"))
	}
	seen := map[string]bool{}
	for _, provider := range authority.Providers {
		if !rootCanonicalHash(provider.Hotkey) || !rootCanonicalHash(provider.Coldkey) || seen[provider.Hotkey] {
			return nil, errors.Join(errRpcIntegrity, errors.New("native producer provider membership is invalid or repeated"))
		}
		seen[provider.Hotkey] = true
	}
	message, messageErr := authority.signingBytes()
	key, keyErr := rootReceiptHex(execution.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(authority.Signature)
	if messageErr != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer independent authority signature is invalid"))
	}
	if err := authority.Treasury.validateScope(policy, policy.Through, nil); err != nil {
		return nil, err
	}
	if authority.Treasury != nil && authority.From != authority.Treasury.Deployment.Activation {
		return nil, errors.New("native treasury producer changed its original activation anchor")
	}
	return &authority, nil
}

// Exact reads preserve observation errors before comparing returned content.
func nativeProducerReadApproval(ctx context.Context, reference planFileReference) ([]byte, error) {
	return nativeProducerReadApprovalFor(ctx, reference, nil)
}

// Source, finality and restore use the same admitted original frame limit.
func nativeProducerReadApprovalFor(ctx context.Context, reference planFileReference, fees *nativeFeeCensusPolicy) ([]byte, error) {
	raw, digest, err := readPlanFile(ctx, reference.Path, nativeProducerAuthorityMaximum(fees))
	if err != nil {
		return nil, err
	}
	if digest != reference.Sha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("native producer approval bytes differ from original pin"))
	}
	return raw, nil
}

// This state is committed together with the original economic cursor. A proof
// certifying a later tip remains paired with its older anchor until all selected
// children have been accounted. No state is inferred from an artifact directory.
type nativeExecutionProducerState struct {
	Schema             string                                 `json:"schema"`
	AuthorityHash      string                                 `json:"authority_hash"`
	AuthorityRevisions []nativeProducerRenewalAcknowledgement `json:"authority_revisions,omitempty"`
	ResourceForecast   *nativeProducerResourceForecast        `json:"resource_forecast,omitempty"`
	Cursor             economicEmissionBoundary               `json:"cursor"`
	Anchor             strecovery.NativeFinalityCheckpoint    `json:"anchor"`
	Window             *planFileReference                     `json:"certified_window,omitempty"`
	Certified          *economicEmissionBoundary              `json:"certified_tip,omitempty"`
	Completed          uint64                                 `json:"completed_jobs"`
	Completion         *planFileReference                     `json:"last_completion,omitempty"`
	CompletionChain    string                                 `json:"completion_chain"`
}

func (self *nativeExecutionProducerState) validate(policy economicEmissionPolicy, cursor economicEmissionBoundary) error {
	if self == nil {
		if policy.Execution != nil && policy.Execution.Producer != nil && cursor != policy.From {
			return errors.New("native producer lost its retained finality/cursor checkpoint")
		}
		return nil
	}
	if policy.Execution == nil || policy.Execution.Producer == nil || self.Schema != nativeProducerSchema || self.Cursor != cursor || self.Completed == 0 || self.Completion == nil || !planSha256(self.Completion.Sha256) || !bootstrapRootAbsolutePath(self.Completion.Path) || !planSha256(self.CompletionChain) || self.Window == nil || self.Certified == nil || !planSha256(self.Window.Sha256) || !bootstrapRootAbsolutePath(self.Window.Path) || self.Certified.Number < cursor.Number || !rootCanonicalHash(self.Certified.Hash) {
		return errors.Join(errRpcIntegrity, errors.New("native producer retained cursor, certificate or completion lineage differs"))
	}
	producer := policy.Execution.Producer
	if len(self.AuthorityRevisions) > len(producer.Renewals) || len(self.AuthorityRevisions) > maximumNativeProducerRenewals {
		return errors.Join(errRpcIntegrity, errors.New("native producer removed acknowledged independent renewals"))
	}
	authorityHash := producer.Authority.Sha256
	capacity := nativeProducerCapacity{Jobs: producer.MaximumJobs, Bytes: producer.MaximumBytes, Entries: producer.MaximumEntries, DescendantHeaders: producer.MaximumDescendantHeaders}
	var priorCompleted, priorBoundary, priorAdopted uint64
	for index, ack := range self.AuthorityRevisions {
		if ack.Revision != producer.Renewals[index] || ack.Completed <= priorCompleted || ack.Completed <= priorAdopted || ack.Completed >= self.Completed || ack.After.Number <= priorBoundary || ack.After.Number >= self.Cursor.Number || !rootCanonicalHash(ack.After.Hash) || !planSha256(ack.CompletionChain) || ack.Capacity.Jobs < capacity.Jobs || ack.Capacity.Bytes < capacity.Bytes || ack.Capacity.Entries < capacity.Entries || ack.Capacity.DescendantHeaders < capacity.DescendantHeaders || ack.AdoptedCompleted < ack.Completed || ack.AdoptedCompleted <= priorAdopted || ack.AdoptedCompleted >= self.Completed || ack.AdoptedAfter.Number < ack.After.Number || ack.AdoptedAfter.Number >= self.Cursor.Number || ack.AdoptedAfter.Number-ack.After.Number != ack.AdoptedCompleted-ack.Completed || !rootCanonicalHash(ack.AdoptedAfter.Hash) || !planSha256(ack.AdoptedChain) || ack.FirstCompletion == nil || !bootstrapRootAbsolutePath(ack.FirstCompletion.Path) || !planSha256(ack.FirstCompletion.Sha256) {
			return errors.Join(errRpcIntegrity, errors.New("native producer changed a retained renewal or its completed predecessor"))
		}
		if err := ack.Capacity.validate(); err != nil {
			return errors.Join(errRpcIntegrity, err)
		}
		authorityHash, capacity = ack.Revision.Sha256, ack.Capacity
		priorCompleted, priorBoundary = ack.Completed, ack.After.Number
		priorAdopted = ack.AdoptedCompleted
	}
	if self.AuthorityHash != authorityHash || self.Completed > capacity.Jobs {
		return errors.Join(errRpcIntegrity, errors.New("native producer current authority or cumulative job capacity differs"))
	}
	if self.ResourceForecast != nil && (self.ResourceForecast.BytesUpperBound < nativeProducerBoundaryReserve || self.ResourceForecast.BytesUpperBound > capacity.Bytes || self.ResourceForecast.EntriesUpperBound < nativeProducerBoundaryEntries || self.ResourceForecast.EntriesUpperBound > capacity.Entries) {
		return errors.Join(errRpcIntegrity, errors.New("native producer resource forecast differs from its admitted capacity"))
	}
	return nil
}

// The completion contains raw derivation bindings, never an approval signature.
// Its hash is checkpointed only after the same verified job has been accounted.
type nativeProducerCompletion struct {
	Schema           string                                `json:"schema"`
	AuthorityHash    string                                `json:"authority_hash"`
	Previous         string                                `json:"previous_completion_chain"`
	Sequence         uint64                                `json:"sequence"`
	Input            planFileReference                     `json:"capture_input"`
	Admission        nativeExecutionAdmission              `json:"admission"`
	Anchor           strecovery.NativeFinalityCheckpoint   `json:"anchor"`
	Window           planFileReference                     `json:"certified_window"`
	Certified        economicEmissionBoundary              `json:"certified_tip"`
	OutcomeHash      string                                `json:"outcome_hash"`
	ResourceForecast *nativeProducerResourceForecast       `json:"resource_forecast,omitempty"`
	Renewal          *nativeProducerRenewalAcknowledgement `json:"renewal_adoption,omitempty"`
}
