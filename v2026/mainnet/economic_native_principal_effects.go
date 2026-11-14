// Stake effects come from the exact admitted original callsites and committed
// storage writes. Independent boundary queries close the stock equation; any
// uncovered mutation or residual keeps causality incomplete, even at net zero.
package main

import (
	"encoding/binary"
	"errors"
	"math/big"
	"reflect"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const nativePrincipalEffectsSchema = "urnetwork-original-stake-execution-effects-v1"

// The original signer admits the complete top-storage dependency scope and
// reviewed causal layouts. This schema does not infer those from storage names.
type nativePrincipalEffectsPolicy struct {
	Schema          string   `json:"schema"`
	ReviewSha256    string   `json:"independent_effect_scope_review_sha256"`
	StoragePrefixes []string `json:"complete_top_storage_prefixes"`
}

func (self *nativePrincipalEffectsPolicy) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != nativePrincipalEffectsSchema || !planSha256(self.ReviewSha256) || self.StoragePrefixes == nil {
		return errors.New("native principal effects lack independent original scope authority")
	}
	return validateHistoricalPrincipalPrefixes(self.StoragePrefixes)
}

func (self *nativePrincipalPolicy) effectsAt(parent economicEmissionBoundary) bool {
	return self != nil && self.Effects != nil && (parent == self.Parent || parent.Number > self.Parent.Number)
}

// The original host observation retains the reviewed memory and actual phase.
// The extrinsic hash binds original body bytes; only a reviewed vault callsite
// also reports the actual EVM transaction identity needed by the vault join.
type nativePrincipalExecutionEffect struct {
	Original           historicalReplayObservation `json:"original_callsite_observation"`
	Query              historicalPrincipalQuery    `json:"original_identity"`
	Kind               string                      `json:"cause"`
	Before             string                      `json:"before_alpha"`
	After              string                      `json:"after_alpha"`
	Amount             string                      `json:"effect_alpha"`
	ExtrinsicIndex     *uint32                     `json:"original_extrinsic_index,omitempty"`
	ExtrinsicHash      string                      `json:"original_extrinsic_blake2b_256,omitempty"`
	EvmTransactionHash string                      `json:"original_evm_transaction_hash,omitempty"`
}

// These companion bytes leave the legacy aggregate hash unchanged while
// binding every observation to that aggregate, original job and trace.
type nativePrincipalExecutionProjection struct {
	Schema          string                           `json:"schema"`
	Authority       nativePrincipalPolicy            `json:"principal_authority"`
	Parent          economicEmissionBoundary         `json:"parent"`
	Boundary        economicEmissionBoundary         `json:"boundary"`
	ParentHeaderHex string                           `json:"original_parent_header_hex"`
	ChildHeaderHex  string                           `json:"original_child_header_hex"`
	Runtime         rootReceiptProfile               `json:"original_runtime"`
	AggregateHash   string                           `json:"aggregate_basis_hash"`
	AdmissionHash   string                           `json:"admission_hash"`
	JobHash         string                           `json:"job_hash"`
	TraceHash       string                           `json:"trace_hash"`
	ExtrinsicHashes []string                         `json:"original_extrinsic_hashes"`
	Before          []historicalPrincipalObservation `json:"original_parent_queries"`
	After           []historicalPrincipalObservation `json:"original_completed_overlay_queries"`
	Mutations       []historicalPrincipalMutation    `json:"committed_mutation_census"`
	Effects         []nativePrincipalExecutionEffect `json:"original_causal_effects"`
	ContentHash     string                           `json:"content_hash"`
}

func (self nativePrincipalExecutionProjection) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

// This decoder accepts any actual block phase. Native emission's separate
// decoder still requires Initialization and is not weakened for transactions.
func principalEffectMemory(record historicalReplayObservation, name string, width int) ([]byte, error) {
	if record.Native == nil || record.Native.ExecutionPhaseHex == nil {
		return nil, errors.New("principal effect omitted actual execution phase")
	}
	for _, memory := range record.Native.Memory {
		if memory.Name == name {
			raw, err := historicalReplayHex(memory.BytesHex, width)
			if err != nil || len(raw) != width {
				return nil, errors.New("principal effect original memory width differs: " + name)
			}
			return raw, nil
		}
	}
	return nil, errors.New("principal effect omitted original memory: " + name)
}

func decodeNativePrincipalEffect(record historicalReplayObservation, extrinsics []string) (nativePrincipalExecutionEffect, error) {
	result := nativePrincipalExecutionEffect{Original: record, Kind: strings.TrimPrefix(record.Purpose, "native-principal-")}
	if !historicalPrincipalEffectPurpose(record.Purpose) || record.Operation != "set" {
		return result, errors.New("principal causal effect lacks original committed set")
	}
	hotkey, e1 := principalEffectMemory(record, "hotkey", 32)
	coldkey, e2 := principalEffectMemory(record, "coldkey", 32)
	netuid, e3 := principalEffectMemory(record, "netuid", 2)
	before, e4 := principalEffectMemory(record, "before", 8)
	after, e5 := principalEffectMemory(record, "after", 8)
	if err := errors.Join(e1, e2, e3, e4, e5); err != nil {
		return result, err
	}
	copy(result.Query.Hotkey[:], hotkey)
	copy(result.Query.Coldkey[:], coldkey)
	result.Query.Netuid = binary.LittleEndian.Uint16(netuid)
	if err := validateHistoricalPrincipalQueries([]historicalPrincipalQuery{result.Query}); err != nil {
		return result, err
	}
	x, y := new(big.Int).SetUint64(binary.LittleEndian.Uint64(before)), new(big.Int).SetUint64(binary.LittleEndian.Uint64(after))
	result.Before, result.After = x.String(), y.String()
	delta := new(big.Int).Sub(y, x)
	if result.Kind == "withdrawal" || result.Kind == "vault-capture" {
		delta.Neg(delta)
	}
	if delta.Sign() < 0 || result.Kind == "support" && delta.Sign() != 0 {
		return result, errors.New("principal original cause contradicts its actual direction")
	}
	result.Amount = delta.String()
	phase, err := historicalReplayHex(*record.Native.ExecutionPhaseHex, 5)
	if err != nil || len(phase) == 0 || phase[0] > 2 || phase[0] == 0 && len(phase) != 5 || phase[0] != 0 && len(phase) != 1 {
		return result, errors.New("principal original execution phase differs")
	}
	if phase[0] == 0 {
		index := binary.LittleEndian.Uint32(phase[1:])
		if uint64(index) >= uint64(len(extrinsics)) {
			return result, errors.New("principal effect lost original extrinsic placement")
		}
		raw, err := historicalReplayHex(extrinsics[index], 8*1024*1024)
		if err != nil {
			return result, err
		}
		result.ExtrinsicIndex = &index
		hash := historicalReplayDigest(blake2b.Sum256(raw))
		result.ExtrinsicHash = economicNativeFeeHash(hash)
	}
	if result.Kind == "earning" && phase[0] != 2 {
		return result, errors.New("principal miner earning occurred outside original Initialization")
	}
	if result.Kind == "vault-capture" {
		if result.ExtrinsicIndex == nil {
			return result, errors.New("principal vault capture has no original extrinsic")
		}
		tx, err := principalEffectMemory(record, "transaction-hash", 32)
		if err != nil {
			return result, err
		}
		var digest historicalReplayDigest
		copy(digest[:], tx)
		if digest == (historicalReplayDigest{}) {
			return result, errors.New("principal vault capture omitted original transaction identity")
		}
		result.EvmTransactionHash = economicNativeFeeHash(digest)
	}
	return result, nil
}

func deriveNativePrincipalEffects(policy economicEmissionPolicy, admission nativeExecutionAdmission, job historicalReplayJob, report *historicalReplayReport, outcome nativeExecutionOutcome) (*nativePrincipalExecutionProjection, error) {
	if policy.Execution == nil {
		return nil, errors.New("principal effects lack original native execution authority")
	}
	authority := policy.Execution.Principal
	if job.PrincipalEffects != authority.effectsAt(admission.Parent) {
		return nil, errors.New("native principal execution effects changed original authority")
	}
	if !job.PrincipalEffects {
		return nil, nil
	}
	if err := authority.validate(); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(admission.Principal, authority) || !reflect.DeepEqual(job.PrincipalQueries, authority.queriesAt(admission.Parent)) || job.ObservationProfile == nil || report == nil || report.HookObservations == nil || !reflect.DeepEqual(job.ObservationProfile.PrincipalStoragePrefixes, authority.Effects.StoragePrefixes) {
		return nil, errors.New("native principal effect query/callsite/storage authority differs")
	}
	if err := errors.Join(validateHistoricalPrincipalReport(job.PrincipalQueries, report.OpeningPrincipals), validateHistoricalClosingPrincipal(job, *report), validateHistoricalPrincipalMutations(job.ObservationProfile, report.HookObservations)); err != nil {
		return nil, err
	}
	result := &nativePrincipalExecutionProjection{Schema: nativePrincipalEffectsSchema, Authority: *authority, Parent: admission.Parent, Boundary: admission.Child, ParentHeaderHex: job.ParentHeaderHex, ChildHeaderHex: job.ChildHeaderHex, Runtime: admission.Runtime, AggregateHash: nativeExecutionEffectBasis(outcome), AdmissionHash: outcome.AdmissionHash, JobHash: outcome.JobHash, TraceHash: outcome.TraceHash, Before: report.OpeningPrincipals, After: report.ClosingPrincipals, Mutations: append([]historicalPrincipalMutation{}, report.HookObservations.PrincipalMutations...), Effects: []nativePrincipalExecutionEffect{}}
	for _, encoded := range job.ExtrinsicsHex {
		raw, err := historicalReplayHex(encoded, 8*1024*1024)
		if err != nil {
			return nil, err
		}
		result.ExtrinsicHashes = append(result.ExtrinsicHashes, economicNativeFeeHash(historicalReplayDigest(blake2b.Sum256(raw))))
	}
	mutations := map[uint64]historicalPrincipalMutation{}
	for _, mutation := range result.Mutations {
		mutations[mutation.Ordinal] = mutation
	}
	for _, record := range report.HookObservations.Observations {
		if !historicalPrincipalEffectPurpose(record.Purpose) || record.Operation != "set" {
			continue
		}
		effect, err := decodeNativePrincipalEffect(record, job.ExtrinsicsHex)
		if err != nil {
			return nil, err
		}
		effect.Query = nativePrincipalAdmittedQuery(effect.Query, authority.Queries)
		if effect.Query.Netuid != policy.Netuid {
			continue
		}
		mutation, found := mutations[record.Ordinal]
		if !found || mutation.KeyHex != record.KeyHex {
			return nil, errors.New("principal original effect escaped admitted mutation census")
		}
		result.Effects = append(result.Effects, effect)
	}
	result.ContentHash = result.hash()
	if err := result.validate(policy, outcome); err != nil {
		return nil, err
	}
	return result, nil
}

// Check persisted identities and exact SCALE again. Arithmetic summaries below
// are recomputed from original evidence, never trusted as standalone amounts.
func (self *nativePrincipalExecutionProjection) validate(policy economicEmissionPolicy, outcome nativeExecutionOutcome) error {
	if self == nil || policy.Execution == nil || policy.Execution.Principal == nil || !policy.Execution.Principal.effectsAt(self.Parent) || self.Schema != nativePrincipalEffectsSchema || self.ContentHash != self.hash() || !reflect.DeepEqual(&self.Authority, policy.Execution.Principal) || self.Boundary != outcome.Boundary || self.Parent.Number+1 != self.Boundary.Number || self.Runtime != policy.Runtime || self.AggregateHash != nativeExecutionEffectBasis(outcome) || self.AdmissionHash != outcome.AdmissionHash || self.JobHash != outcome.JobHash || self.TraceHash != outcome.TraceHash || !outcome.AmountsAuthenticated || outcome.ContentHash != outcome.hash() || len(self.Mutations) > 16384 || len(self.Effects) > 16384 || len(self.ExtrinsicHashes) > 65536 {
		return errors.New("principal execution projection differs from original authority or replay")
	}
	if err := errors.Join(self.Authority.validate(), validateHistoricalPrincipalReport(self.Authority.Queries, self.Before), validateHistoricalPrincipalReport(self.Authority.Queries, self.After)); err != nil {
		return err
	}
	for _, boundary := range []struct {
		boundary economicEmissionBoundary
		header   string
	}{{boundary: self.Parent, header: self.ParentHeaderHex}, {boundary: self.Boundary, header: self.ChildHeaderHex}} {
		value := nativePrincipalProjection{Parent: boundary.boundary, ParentHeaderHex: boundary.header}
		header, err := value.parentHeader()
		if err != nil {
			return err
		}
		if boundary.boundary == self.Boundary && header.ParentHash != self.Parent.Hash {
			return errors.New("principal execution child changed original parent")
		}
	}
	for _, hash := range self.ExtrinsicHashes {
		if !rootCanonicalHash(hash) {
			return errors.New("principal execution changed original extrinsic identity")
		}
	}
	trace := &historicalReplayObservations{HostCalls: 65536, PrincipalMutations: self.Mutations}
	for _, effect := range self.Effects {
		trace.Observations = append(trace.Observations, effect.Original)
	}
	if err := validateHistoricalPrincipalMutations(&historicalReplayObservationProfile{PrincipalStoragePrefixes: self.Authority.Effects.StoragePrefixes}, trace); err != nil {
		return err
	}
	mutations := map[uint64]historicalPrincipalMutation{}
	previous := uint64(0)
	for _, mutation := range self.Mutations {
		if mutation.Ordinal <= previous {
			return errors.New("principal mutation census repeated or reordered")
		}
		previous = mutation.Ordinal
		mutations[mutation.Ordinal] = mutation
	}
	previous = 0
	for _, effect := range self.Effects {
		if effect.Original.Ordinal <= previous || effect.Kind != strings.TrimPrefix(effect.Original.Purpose, "native-principal-") || !historicalPrincipalEffectPurpose(effect.Original.Purpose) {
			return errors.New("principal causal effect repeated or relabelled")
		}
		previous = effect.Original.Ordinal
		mutation, found := mutations[effect.Original.Ordinal]
		if !found || mutation.KeyHex != effect.Original.KeyHex || mutation.Operation != effect.Original.Operation {
			return errors.New("principal causal effect lost original committed mutation")
		}
		if effect.Original.Native == nil || effect.Original.Native.ExecutionPhaseHex == nil {
			return errors.New("principal causal effect lost original phase")
		}
		phase, err := historicalReplayHex(*effect.Original.Native.ExecutionPhaseHex, 5)
		if err != nil || len(phase) == 0 || phase[0] > 2 || phase[0] == 0 && len(phase) != 5 || phase[0] != 0 && len(phase) != 1 || (phase[0] == 0) != (effect.ExtrinsicIndex != nil) {
			return errors.New("principal causal effect changed original phase")
		}
		if phase[0] == 0 {
			index := binary.LittleEndian.Uint32(phase[1:])
			if index != *effect.ExtrinsicIndex || uint64(index) >= uint64(len(self.ExtrinsicHashes)) || effect.ExtrinsicHash != self.ExtrinsicHashes[index] {
				return errors.New("principal causal effect changed original body placement")
			}
		}
		if effect.Kind == "earning" && phase[0] != 2 {
			return errors.New("principal causal earning changed original Initialization")
		}
		if effect.Kind == "vault-capture" {
			raw, err := principalEffectMemory(effect.Original, "transaction-hash", 32)
			var hash historicalReplayDigest
			copy(hash[:], raw)
			if err != nil || effect.EvmTransactionHash != economicNativeFeeHash(hash) {
				return errors.New("principal vault capture changed original transaction")
			}
		} else if effect.EvmTransactionHash != "" {
			return errors.New("principal unrelated effect invented a vault transaction")
		}
		// The immutable projection retains original body hash, not whole bodies.
		// Decode its memory again independently of the derived amount fields.
		before, e1 := principalEffectMemory(effect.Original, "before", 8)
		after, e2 := principalEffectMemory(effect.Original, "after", 8)
		hot, e3 := principalEffectMemory(effect.Original, "hotkey", 32)
		cold, e4 := principalEffectMemory(effect.Original, "coldkey", 32)
		netuid, e5 := principalEffectMemory(effect.Original, "netuid", 2)
		if err := errors.Join(e1, e2, e3, e4, e5); err != nil {
			return err
		}
		x, y := new(big.Int).SetUint64(binary.LittleEndian.Uint64(before)), new(big.Int).SetUint64(binary.LittleEndian.Uint64(after))
		delta := new(big.Int).Sub(y, x)
		if effect.Kind == "withdrawal" || effect.Kind == "vault-capture" {
			delta.Neg(delta)
		}
		var query historicalPrincipalQuery
		copy(query.Hotkey[:], hot)
		copy(query.Coldkey[:], cold)
		query.Netuid = binary.LittleEndian.Uint16(netuid)
		query = nativePrincipalAdmittedQuery(query, self.Authority.Queries)
		if query != effect.Query || effect.Before != x.String() || effect.After != y.String() || effect.Amount != delta.String() || delta.Sign() < 0 || effect.Kind == "support" && delta.Sign() != 0 || effect.ExtrinsicIndex != nil && !rootCanonicalHash(effect.ExtrinsicHash) || effect.ExtrinsicIndex == nil && effect.ExtrinsicHash != "" || effect.Kind == "vault-capture" && (effect.ExtrinsicIndex == nil || !rootCanonicalHash(effect.EvmTransactionHash)) {
			return errors.New("principal original observation and derived effect disagree")
		}
	}
	return nil
}
