// Opening principal is observed through an independently admitted original
// parent API. It remains stock at an exact boundary; it is never miner income,
// a vault credit estimate or an allowance for unexplained execution effects.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// The declaration fixes the decoder's complete SCALE grammar. A new runtime
// returning another layout needs a separately implemented and reviewed schema.
const historicalPrincipalLayout = "Option<StakeInfo{hotkey:AccountId32,coldkey:AccountId32,netuid:Compact<u16>,stake:Compact<u64>,locked:Compact<u64>,emission:Compact<u64>,tao_emission:Compact<u64>,drain:Compact<u64>,registered:bool}>"
const nativePrincipalAvailabilitySchema = "urnetwork-original-parent-stake-api-v2"
const historicalAvailabilityLayout = "BTreeMap<AccountId32,BTreeMap<u16,StakeAvailability{total:Compact<u64>,locked:Compact<u64>,available:Compact<u64>}>>"

// A new read-only runtime API needs its own independently approved layout and
// semantics. A caller-set transport flag alone grants no availability authority.
type nativeAvailabilityPolicy struct {
	Schema       string `json:"schema"`
	Api          string `json:"runtime_api"`
	LayoutSha256 string `json:"scale_layout_sha256"`
	ReviewSha256 string `json:"independent_api_review_sha256"`
}

func (self *nativeAvailabilityPolicy) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != historicalAvailabilitySchema || self.Api != historicalAvailabilityApi || self.LayoutSha256 != monitorReadDigest([]byte(historicalAvailabilityLayout)) || !planSha256(self.ReviewSha256) {
		return errors.New("native availability lacks independent original API/layout authority")
	}
	return nil
}

func nativePrincipalAuthoritySchema(authority *nativePrincipalPolicy) string {
	if authority != nil && authority.Availability != nil {
		return nativePrincipalAvailabilitySchema
	}
	return historicalPrincipalSchema
}

// The original observation policy and its signed execution/producer admission
// both bind this API/layout review and exact parent/query census, without amounts.
type nativePrincipalPolicy struct {
	Availability *nativeAvailabilityPolicy     `json:"stake_availability_authority,omitempty"`
	Effects      *nativePrincipalEffectsPolicy `json:"execution_effects,omitempty"`
	Schema       string                        `json:"schema"`
	Api          string                        `json:"runtime_api"`
	LayoutSha256 string                        `json:"scale_layout_sha256"`
	ReviewSha256 string                        `json:"independent_api_review_sha256"`
	Parent       economicEmissionBoundary      `json:"original_parent"`
	Queries      []historicalPrincipalQuery    `json:"query_census"`
}

func (self *nativePrincipalPolicy) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != nativePrincipalAuthoritySchema(self) || self.Api != historicalPrincipalApi || self.LayoutSha256 != monitorReadDigest([]byte(historicalPrincipalLayout)) || !planSha256(self.ReviewSha256) || self.Parent.Number == 0 || !rootCanonicalHash(self.Parent.Hash) || self.Queries == nil {
		return errors.New("native principal lacks independently pinned original API/layout/parent authority")
	}
	selected := false
	for _, query := range self.Queries {
		selected = selected || query.Availability
	}
	if selected != (self.Availability != nil) {
		return errors.New("native availability queries differ from independently selected successor authority")
	}
	return errors.Join(validateHistoricalPrincipalQueries(self.Queries), self.Effects.validate(), self.Availability.validate())
}

func (self *nativePrincipalPolicy) queriesAt(parent economicEmissionBoundary) []historicalPrincipalQuery {
	if self == nil || self.Parent != parent && (self.Effects == nil || parent.Number < self.Parent.Number) {
		return nil
	}
	return append([]historicalPrincipalQuery{}, self.Queries...)
}

// A companion projection binds the original parent result to its successful
// block replay and existing aggregate. It does not rewrite older completions.
type nativePrincipalProjection struct {
	Schema          string                           `json:"schema"`
	Authority       nativePrincipalPolicy            `json:"api_authority"`
	Parent          economicEmissionBoundary         `json:"parent"`
	ParentHeaderHex string                           `json:"original_parent_header_hex"`
	Boundary        economicEmissionBoundary         `json:"boundary"`
	Runtime         rootReceiptProfile               `json:"execution_runtime"`
	AggregateHash   string                           `json:"aggregate_basis_hash"`
	AdmissionHash   string                           `json:"admission_hash"`
	JobHash         string                           `json:"job_hash"`
	Observations    []historicalPrincipalObservation `json:"opening_principals"`
	ContentHash     string                           `json:"content_hash"`
}

func (self nativePrincipalProjection) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

// The enclosing caller has admitted the signed execution or continuous
// authority and executed the exact job. No external amount document enters here.
func deriveNativePrincipal(policy economicEmissionPolicy, admission nativeExecutionAdmission, job historicalReplayJob, report *historicalReplayReport, outcome nativeExecutionOutcome) (*nativePrincipalProjection, error) {
	if policy.Execution == nil || report == nil {
		return nil, errors.New("native principal has no admitted original execution")
	}
	authority := policy.Execution.Principal
	if err := authority.validate(); err != nil {
		return nil, err
	}
	queries := authority.queriesAt(admission.Parent)
	if !reflect.DeepEqual(job.PrincipalQueries, queries) || !reflect.DeepEqual(admission.Principal, authority) {
		return nil, errors.New("native principal job substituted independent API/query authority")
	}
	if err := validateHistoricalPrincipalReport(queries, report.OpeningPrincipals); err != nil {
		return nil, err
	}
	if queries == nil || admission.Parent != authority.Parent {
		return nil, nil
	}
	if authority.Parent != policy.From || authority.Parent != admission.Parent || admission.Parent.Number+1 != admission.Child.Number || admission.Child != outcome.Boundary || admission.Runtime != policy.Runtime || report.ParentHash != job.ParentHash || economicNativeFeeHash(job.ParentHash) != authority.Parent.Hash {
		return nil, errors.New("native principal substituted its exact admitted opening parent/runtime")
	}
	for _, query := range queries {
		if query.Netuid != policy.Netuid {
			return nil, errors.New("native principal query borrowed another subnet")
		}
	}
	result := &nativePrincipalProjection{Schema: nativePrincipalAuthoritySchema(authority), Authority: *authority, Parent: admission.Parent, ParentHeaderHex: job.ParentHeaderHex, Boundary: admission.Child, Runtime: admission.Runtime, AggregateHash: nativeExecutionEffectBasis(outcome), AdmissionHash: outcome.AdmissionHash, JobHash: outcome.JobHash, Observations: append([]historicalPrincipalObservation{}, report.OpeningPrincipals...)}
	result.ContentHash = result.hash()
	return result, nil
}

func (self *nativePrincipalProjection) validate(policy economicEmissionPolicy, outcome nativeExecutionOutcome) error {
	if self == nil || !outcome.AmountsAuthenticated || outcome.ContentHash != outcome.hash() || policy.Execution == nil || policy.Execution.Principal == nil || !reflect.DeepEqual(&self.Authority, policy.Execution.Principal) || self.Schema != nativePrincipalAuthoritySchema(&self.Authority) || self.ContentHash != self.hash() || self.Parent != self.Authority.Parent || self.Parent != policy.From || self.Boundary != outcome.Boundary || self.Parent.Number+1 != self.Boundary.Number || self.Runtime != policy.Runtime || self.AggregateHash != nativeExecutionEffectBasis(outcome) || self.AdmissionHash != outcome.AdmissionHash || self.JobHash != outcome.JobHash {
		return errors.New("native principal projection differs from original API/runtime/execution authority")
	}
	if err := self.Authority.validate(); err != nil {
		return err
	}
	if _, err := self.parentHeader(); err != nil {
		return err
	}
	return validateHistoricalPrincipalReport(self.Authority.Queries, self.Observations)
}

// Decode the exact replayed parent header before joining its Frontier digest.
// Neither a caller-provided EVM hash nor a matching timestamp creates a join.
func (self nativePrincipalProjection) parentHeader() (rootReceiptHeader, error) {
	var result rootReceiptHeader
	raw, err := historicalReplayHex(self.ParentHeaderHex, 64*1024)
	if err != nil {
		return result, err
	}
	var header types.Header
	if err := codec.Decode(raw, &header); err != nil {
		return result, err
	}
	canonical, err := codec.Encode(header)
	if err != nil || !bytes.Equal(raw, canonical) || uint64(header.Number) != self.Parent.Number {
		return result, errors.New("native principal original parent header is noncanonical")
	}
	result = rootReceiptHeader{ParentHash: header.ParentHash.Hex(), Number: fmt.Sprintf("0x%x", uint64(header.Number)), StateRoot: header.StateRoot.Hex(), ExtrinsicsRoot: header.ExtrinsicsRoot.Hex()}
	result.Digest.Logs = []string{}
	for _, item := range header.Digest {
		encoded, err := codec.Encode(item)
		if err != nil {
			return result, err
		}
		result.Digest.Logs = append(result.Digest.Logs, fmt.Sprintf("0x%x", encoded))
	}
	if _, err := result.authenticate(self.Parent.Hash); err != nil {
		return result, err
	}
	return result, nil
}
