// A reusable signed execution scope admits the original participant accounts
// and complete fee paths. Each real replay accounts for every original body
// entry; an unobserved path or refund remains unresolved, including at zero.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"reflect"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"golang.org/x/crypto/blake2b"
)

const nativeFeeCensusSchema = "urnetwork-original-native-fee-census-v1"

// Native and fee observers share one original execution without sharing an
// interpretation. Only these explicit fee purposes enter this census.
func nativeFeeCensusPurpose(purpose string) bool {
	return purpose == "fee-withdraw" || purpose == "fee-refund" || purpose == "ethereum-executed" || purpose == "native-fee-exempt" || purpose == "native-fee-refund-zero"
}

// The original canonical roster is sorted once at admission, not scanned for
// every provider or transaction in a growing block census.
func (self *nativeFeeCensusPolicy) participant(account string) bool {
	if self == nil {
		return false
	}
	_, found := slices.BinarySearch(self.Participants, account)
	return found
}

// The original producer signature covers this policy, not an asserted fee or
// completeness result. Native hotkeys/coldkeys and additional fee accounts are
// fixed for the economic generation; runtime renewals cannot replace them.
type nativeFeeCensusPolicy struct {
	Schema       string   `json:"schema"`
	ReviewSha256 string   `json:"complete_fee_paths_review_sha256"`
	Participants []string `json:"original_participant_accounts"`
}

// A bounded canonical account set keeps equivalent domains byte-identical.
func (self *nativeFeeCensusPolicy) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != nativeFeeCensusSchema || !planSha256(self.ReviewSha256) || len(self.Participants) == 0 || len(self.Participants) > 2*rootCensusLimit+48 {
		return errors.New("native fee census lacks a bounded original participant and semantic authority")
	}
	for index, account := range self.Participants {
		if !rootCanonicalHash(account) || index != 0 && self.Participants[index-1] >= account {
			return errors.New("native fee participant accounts are not canonical, unique and ordered")
		}
	}
	return nil
}

// Reviewed fee callsites coexist with native allocation callsites. The exact
// original profile/code/metadata and both engines remain independently signed.
func (self *nativeFeeCensusPolicy) validateProducer(authority nativeProducerAuthority) error {
	if err := self.validate(); err != nil || self == nil {
		return err
	}
	if authority.Profile == nil || authority.Profile.MetadataSha256 == nil {
		return errors.New("native whole-fee producer lacks original generated event metadata")
	}
	purposes := map[string]bool{}
	for _, rule := range authority.Profile.Rules {
		purposes[rule.Purpose] = true
	}
	if !purposes["fee-withdraw"] || !purposes["fee-refund"] || !purposes["ethereum-executed"] {
		return errors.New("native whole-fee producer omits original withdrawal, refund or execution callsites")
	}
	for _, provider := range authority.Providers {
		if !self.participant(provider.Hotkey) || !self.participant(provider.Coldkey) {
			return errors.New("native whole-fee authority omits an original provider account")
		}
	}
	return nil
}

// Every entry retains the body identity and original observations. A native
// fee need not have an Ethereum transaction, and an unrelated payer is still
// counted in coverage even though it does not increase participant expenses.
type nativeFeeCensusExtrinsic struct {
	Index           uint32                       `json:"index"`
	Hash            string                       `json:"extrinsic_blake2b_256"`
	Events          []historicalReplayFeeEvent   `json:"original_fee_events"`
	Exemption       *historicalReplayObservation `json:"original_exemption,omitempty"`
	RefundZero      *historicalReplayObservation `json:"original_zero_refund,omitempty"`
	Payer           string                       `json:"payer,omitempty"`
	TransactionHash string                       `json:"evm_transaction_hash,omitempty"`
	Participant     bool                         `json:"original_participant"`
	Status          string                       `json:"status"`
	WithdrawalRao   *string                      `json:"withdrawal_rao"`
	RefundRao       *string                      `json:"refund_rao"`
	DebitRao        *string                      `json:"debit_rao"`
}

// This companion is made only from the admitted running replay and leaves the
// legacy completion digest unchanged. Restored evidence stays under original
// checkpoint/job custody; an externally self-sealed companion is not ingress.
type nativeFeeCensusProjection struct {
	Schema      string                     `json:"schema"`
	PolicyHash  string                     `json:"original_fee_policy_hash"`
	Parent      economicEmissionBoundary   `json:"parent"`
	Boundary    economicEmissionBoundary   `json:"boundary"`
	OutcomeHash string                     `json:"original_outcome_hash"`
	JobHash     string                     `json:"original_job_hash"`
	TraceHash   string                     `json:"original_trace_hash"`
	Extrinsics  []nativeFeeCensusExtrinsic `json:"complete_original_extrinsics"`
	Unplaced    []historicalReplayFeeEvent `json:"unplaced_original_fee_events"`
	ContentHash string                     `json:"content_hash"`
}

// Excluding only the seal preserves the complete original coverage grammar.
func (self nativeFeeCensusProjection) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

// A no-fee disposition requires an actual reviewed branch read during this
// ApplyExtrinsic and an explicit original Pays::No byte (the pinned SDK enum
// encodes Yes before No). Absence is not zero.
func nativeFeeExemptionIndex(record historicalReplayObservation) (uint32, error) {
	if record.Purpose != "native-fee-exempt" || record.Operation != "get" || record.StorageReturn == nil || record.Native == nil || record.Native.ExecutionPhaseHex == nil {
		return 0, errors.New("native fee exemption lacks its actual original branch read")
	}
	phase, err := historicalReplayHex(*record.Native.ExecutionPhaseHex, 5)
	if err != nil || len(phase) != 5 || phase[0] != 0 {
		return 0, errors.New("native fee exemption lacks exact ApplyExtrinsic placement")
	}
	value, err := principalEffectMemory(record, "pays-fee", 1)
	if err != nil || value[0] != 1 {
		return 0, errors.New("native fee exemption did not observe the original nonpaying branch")
	}
	return binary.LittleEndian.Uint32(phase[1:]), nil
}

// Some original paths omit a deposit event when the computed refund is zero.
// That case needs a reviewed branch read of the exact original u64 result.
func nativeFeeRefundZeroIndex(record historicalReplayObservation) (uint32, error) {
	if record.Purpose != "native-fee-refund-zero" || record.Operation != "get" || record.StorageReturn == nil || record.Native == nil || record.Native.ExecutionPhaseHex == nil {
		return 0, errors.New("native zero refund lacks its original branch read")
	}
	phase, err := historicalReplayHex(*record.Native.ExecutionPhaseHex, 5)
	if err != nil || len(phase) != 5 || phase[0] != 0 {
		return 0, errors.New("native zero refund lacks exact ApplyExtrinsic placement")
	}
	value, err := principalEffectMemory(record, "refund", 8)
	if err != nil || binary.LittleEndian.Uint64(value) != 0 {
		return 0, errors.New("native zero refund did not observe original zero")
	}
	return binary.LittleEndian.Uint32(phase[1:]), nil
}

// The same deterministic reducer validates retained checkpoint facts. Multiple
// fee pairs are unsupported rather than guessed; a refund must be explicit.
func classifyNativeFeeExtrinsic(policy *nativeFeeCensusPolicy, original nativeFeeCensusExtrinsic) (nativeFeeCensusExtrinsic, error) {
	result := nativeFeeCensusExtrinsic{Index: original.Index, Hash: original.Hash, Events: original.Events, Exemption: original.Exemption, RefundZero: original.RefundZero, Status: "fee-path-unobserved"}
	if !rootCanonicalHash(original.Hash) || original.Events == nil {
		return result, errors.New("native fee body entry lost original identity or event vector")
	}
	var withdrawal, refund *historicalReplayFeeEvent
	var priorOrdinal uint64
	for index := range original.Events {
		event := &original.Events[index]
		if err := validateNativeFeeEvent(*event); err != nil {
			return result, err
		}
		if event.ObservationOrdinal <= priorOrdinal || event.ExtrinsicIndex == nil || *event.ExtrinsicIndex != original.Index || event.Phase != "apply-extrinsic" {
			return result, errors.New("native fee original event order or placement differs")
		}
		priorOrdinal = event.ObservationOrdinal
		switch event.Purpose {
		case "fee-withdraw", "fee-refund":
			if event.Payer == nil || event.AmountRao == nil || event.TransactionHash != nil || event.Source != nil {
				return result, errors.New("native fee amount lost original payer semantics")
			}
			if _, ok := historicalReplayAmount(event.AmountRao); !ok {
				return result, errors.New("native fee amount is not an original bounded integer")
			}
			if event.Purpose == "fee-withdraw" {
				if event.Event != "Balances.Withdraw" || withdrawal != nil || refund != nil {
					return result, errors.New("native fee withdrawal census is ambiguous or reordered")
				}
				withdrawal = event
			} else {
				if event.Event != "Balances.Deposit" || refund != nil {
					return result, errors.New("native fee refund census is ambiguous")
				}
				refund = event
			}
		case "ethereum-executed":
			if event.Event != "Ethereum.Executed" || event.Source == nil || event.TransactionHash == nil || result.TransactionHash != "" {
				return result, errors.New("native fee execution identity is absent or ambiguous")
			}
			result.TransactionHash = economicNativeFeeHash(*event.TransactionHash)
		default:
			return result, errors.New("native fee census contains a foreign event purpose")
		}
	}
	if original.Exemption != nil {
		index, err := nativeFeeExemptionIndex(*original.Exemption)
		if err != nil || index != original.Index || len(original.Events) != 0 || original.RefundZero != nil {
			return result, errors.Join(errors.New("native fee exemption conflicts with original charge or placement"), err)
		}
		zero := "0"
		result.Status, result.WithdrawalRao, result.RefundRao, result.DebitRao = "original-exempt", &zero, &zero, &zero
		return result, nil
	}
	if original.RefundZero != nil {
		index, err := nativeFeeRefundZeroIndex(*original.RefundZero)
		if err != nil || index != original.Index || withdrawal == nil || refund != nil {
			return result, errors.Join(errors.New("native zero refund conflicts with original charge or placement"), err)
		}
		zero := "0"
		refund = &historicalReplayFeeEvent{Payer: withdrawal.Payer, AmountRao: &zero}
	}
	if withdrawal == nil {
		result.Status = "withdrawal-unobserved"
		return result, nil
	}
	result.Payer = economicNativeFeeHash(*withdrawal.Payer)
	result.Participant = policy.participant(result.Payer)
	if refund == nil {
		result.Status = "refund-unobserved"
		return result, nil
	}
	if *withdrawal.Payer != *refund.Payer {
		return result, errors.New("native fee original withdrawal and refund have different payers")
	}
	for _, event := range original.Events {
		if event.Source != nil {
			mapped := blake2b.Sum256(append([]byte("evm:"), event.Source[:]...))
			if historicalReplayDigest(mapped) != *withdrawal.Payer {
				return result, errors.New("native fee original execution sender differs from actual payer")
			}
		}
	}
	w, _ := monitorEconomicInteger(*withdrawal.AmountRao)
	r, _ := monitorEconomicInteger(*refund.AmountRao)
	if r.Cmp(w) > 0 {
		return result, errors.New("native fee refund exceeds original withdrawal")
	}
	wRaw, rRaw, dRaw := w.String(), r.String(), new(big.Int).Sub(w, r).String()
	result.Status, result.WithdrawalRao, result.RefundRao, result.DebitRao = "original-pair", &wRaw, &rRaw, &dRaw
	return result, nil
}

// The original engine decodes exact runtime metadata. Retained custody must
// still preserve its typed fields; a self-consistent summary is not enough.
func validateNativeFeeEvent(event historicalReplayFeeEvent) error {
	if event.ObservationOrdinal == 0 || event.EventSha256 == (historicalReplayDigest{}) {
		return errors.New("native fee event lost its original append identity")
	}
	if event.Event == "Ethereum.Executed" {
		if event.Purpose != "ethereum-executed" || event.Source == nil || event.TransactionHash == nil || event.Payer != nil || event.AmountRao != nil {
			return errors.New("native fee execution fields differ from original event")
		}
	} else if (event.Event != "Balances.Withdraw" || event.Purpose != "fee-withdraw") && (event.Event != "Balances.Deposit" || event.Purpose != "fee-refund") || event.Payer == nil || event.AmountRao == nil || event.Source != nil || event.TransactionHash != nil {
		return errors.New("native fee amount fields differ from original event")
	}
	if _, valid := historicalReplayAmount(event.AmountRao); !valid {
		return errors.New("native fee original amount is not an exact u64")
	}
	return nil
}

// Every body entry participates, including unrelated accounts and quiet
// blocks. The existing replay validator binds complete body/postroot and each
// observed callsite before this reducer is reachable from the producer.
func deriveNativeFeeCensus(ctx context.Context, policy *nativeFeeCensusPolicy, parent economicEmissionBoundary, job historicalReplayJob, report *historicalReplayReport, outcome nativeExecutionOutcome) (*nativeFeeCensusProjection, error) {
	if policy == nil {
		return nil, nil
	}
	if ctx == nil {
		return nil, errors.New("native fee census has no lifecycle owner")
	}
	if err := errors.Join(ctx.Err(), policy.validate()); err != nil {
		return nil, err
	}
	if report == nil || !report.PostStateReproduced || report.HookObservations == nil || report.HookObservations.FeeEvents == nil || report.Extrinsics != uint64(len(job.ExtrinsicsHex)) || len(job.ExtrinsicsHex) > 16384 || parent.Number >= math.MaxUint32 || parent.Number+1 != outcome.Boundary.Number || economicNativeFeeHash(report.ParentHash) != parent.Hash || economicNativeFeeHash(report.ChildHash) != outcome.Boundary.Hash || outcome.ContentHash != outcome.hash() || outcome.JobHash != economicNativeFeeSha(report.JobSha256) || outcome.TraceHash != rootObjectHash(report.HookObservations) {
		return nil, errors.New("native fee census differs from the actual complete original replay")
	}
	if err := validateHistoricalReplayObservations(job, report.HookObservations); err != nil {
		return nil, err
	}
	decoded := make(map[uint64]bool, len(report.HookObservations.FeeEvents.Events))
	for _, event := range report.HookObservations.FeeEvents.Events {
		decoded[event.ObservationOrdinal] = true
	}
	eventKey := "0x" + hex.EncodeToString(append(xxhash.New128([]byte("System")).Sum(nil), xxhash.New128([]byte("Events")).Sum(nil)...))
	for _, observation := range report.HookObservations.Observations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if (observation.Purpose == "fee-withdraw" || observation.Purpose == "fee-refund" || observation.Purpose == "ethereum-executed") && observation.Operation == "append" && observation.KeyHex == eventKey && !decoded[observation.Ordinal] {
			return nil, errors.New("native fee census omitted an original selected event append")
		}
	}
	result := &nativeFeeCensusProjection{Schema: nativeFeeCensusSchema, PolicyHash: rootObjectHash(policy), Parent: parent, Boundary: outcome.Boundary, OutcomeHash: outcome.ContentHash, JobHash: outcome.JobHash, TraceHash: outcome.TraceHash, Extrinsics: []nativeFeeCensusExtrinsic{}, Unplaced: []historicalReplayFeeEvent{}}
	for index, encoded := range job.ExtrinsicsHex {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := historicalReplayHex(encoded, 8*1024*1024)
		if err != nil {
			return nil, err
		}
		result.Extrinsics = append(result.Extrinsics, nativeFeeCensusExtrinsic{Index: uint32(index), Hash: economicNativeFeeHash(historicalReplayDigest(blake2b.Sum256(raw))), Events: []historicalReplayFeeEvent{}})
	}
	for _, event := range report.HookObservations.FeeEvents.Events {
		if event.ExtrinsicIndex == nil || event.Phase != "apply-extrinsic" {
			result.Unplaced = append(result.Unplaced, event)
			continue
		}
		if uint64(*event.ExtrinsicIndex) >= uint64(len(result.Extrinsics)) {
			return nil, errors.New("native fee event exceeds original complete body")
		}
		entry := &result.Extrinsics[*event.ExtrinsicIndex]
		entry.Events = append(entry.Events, event)
	}
	for _, observation := range report.HookObservations.Observations {
		if observation.Purpose != "native-fee-exempt" && observation.Purpose != "native-fee-refund-zero" {
			continue
		}
		index, err := nativeFeeExemptionIndex(observation)
		if observation.Purpose == "native-fee-refund-zero" {
			index, err = nativeFeeRefundZeroIndex(observation)
		}
		if err != nil || uint64(index) >= uint64(len(result.Extrinsics)) {
			return nil, errors.Join(errors.New("native fee exemption exceeds original body"), err)
		}
		entry := &result.Extrinsics[index]
		target := &entry.Exemption
		if observation.Purpose == "native-fee-refund-zero" {
			target = &entry.RefundZero
		}
		if *target != nil {
			return nil, errors.New("native fee exemption repeats an original body entry")
		}
		*target = &observation
	}
	for index, original := range result.Extrinsics {
		entry, err := classifyNativeFeeExtrinsic(policy, original)
		if err != nil {
			return nil, err
		}
		result.Extrinsics[index] = entry
	}
	result.ContentHash = result.hash()
	if err := result.validate(ctx, policy); err != nil {
		return nil, err
	}
	return result, nil
}

// Check decoded facts under the same original domain before using retained
// arithmetic. Only the real producer supplies new companions to this owner.
func (self nativeFeeCensusProjection) validate(ctx context.Context, policy *nativeFeeCensusPolicy) error {
	if ctx == nil {
		return errors.New("native retained fee census has no lifecycle owner")
	}
	if err := errors.Join(ctx.Err(), policy.validate()); err != nil {
		return err
	}
	if policy == nil || self.Schema != nativeFeeCensusSchema || self.PolicyHash != rootObjectHash(policy) || self.ContentHash != self.hash() || self.Parent.Number >= math.MaxUint32 || self.Boundary.Number != self.Parent.Number+1 || !rootCanonicalHash(self.Parent.Hash) || !rootCanonicalHash(self.Boundary.Hash) || !planSha256(self.OutcomeHash) || !planSha256(self.JobHash) || !planSha256(self.TraceHash) || self.Extrinsics == nil || self.Unplaced == nil || len(self.Extrinsics) > 16384 || len(self.Unplaced) > 6*rootCensusLimit {
		return errors.New("native fee retained census lost its original complete scope")
	}
	ordinals := map[uint64]bool{}
	for _, event := range self.Unplaced {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateNativeFeeEvent(event); err != nil {
			return err
		}
		if event.ExtrinsicIndex != nil || event.Phase != "initialization" && event.Phase != "finalization" || event.ObservationOrdinal == 0 || ordinals[event.ObservationOrdinal] {
			return errors.New("native unplaced fee observation changed original phase or identity")
		}
		ordinals[event.ObservationOrdinal] = true
	}
	for index, entry := range self.Extrinsics {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Index != uint32(index) {
			return errors.New("native fee retained body census is not complete and ordered")
		}
		for _, event := range entry.Events {
			if event.ObservationOrdinal == 0 || ordinals[event.ObservationOrdinal] || len(ordinals) >= 6*rootCensusLimit {
				return errors.New("native fee census repeated or exceeded original observations")
			}
			ordinals[event.ObservationOrdinal] = true
		}
		for _, branch := range []*historicalReplayObservation{entry.Exemption, entry.RefundZero} {
			if branch != nil {
				if branch.Ordinal == 0 || ordinals[branch.Ordinal] || len(ordinals) >= 6*rootCensusLimit {
					return errors.New("native fee branch repeated or exceeded original observations")
				}
				ordinals[branch.Ordinal] = true
			}
		}
		derived, err := classifyNativeFeeExtrinsic(policy, entry)
		if err != nil || !reflect.DeepEqual(entry, derived) {
			return errors.Join(errors.New("native fee retained amount or payer differs from original observations"), err)
		}
	}
	return ctx.Err()
}
