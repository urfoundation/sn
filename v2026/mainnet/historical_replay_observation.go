// The process owner preserves proof-relative observation candidates without
// upgrading a caller-supplied source review into runtime or fee authority.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"golang.org/x/crypto/blake2b"
)

const historicalReplayObservedReportLimit = 8 * 1024 * 1024

// Field order matches the pinned Rust profile serialization used for its hash.
type historicalReplayObservationProfile struct {
	Schema                   string                     `json:"schema"`
	RuntimeCodeSha256        historicalReplayDigest     `json:"runtime_code_sha256"`
	SourceReviewSha256       historicalReplayDigest     `json:"source_review_sha256"`
	Rules                    []historicalReplayHookRule `json:"rules"`
	MetadataSha256           *historicalReplayDigest    `json:"metadata_sha256,omitempty"`
	PrincipalStoragePrefixes []string                   `json:"principal_storage_prefixes,omitempty"`
	OriginalGlobals          []historicalOriginalGlobal `json:"original_globals,omitempty"`
	EpochLayout              *string                    `json:"epoch_layout,omitempty"`
	RecipientLayout          *string                    `json:"recipient_layout,omitempty"`
}

// The engine may expose only these existing globals to its read-only host.
// It verifies export-only transformation before assigning a distinct cache key.
type historicalOriginalGlobal struct {
	GlobalIndex uint32 `json:"global_index"`
	ExportName  string `json:"export_name"`
}

type historicalReplayHookRule struct {
	Purpose            string                    `json:"purpose"`
	FunctionIndex      uint32                    `json:"function_index"`
	FunctionBodySha256 historicalReplayDigest    `json:"function_body_sha256"`
	OffsetStart        uint32                    `json:"offset_start"`
	OffsetEnd          uint32                    `json:"offset_end"`
	Memory             []historicalNativeCapture `json:"memory,omitempty"`
	HostSnapshot       *string                   `json:"host_snapshot,omitempty"`
	StateReads         []string                  `json:"state_reads,omitempty"`
	StorageCall        *historicalStorageCall    `json:"storage_call,omitempty"`
	RecipientOwner     bool                      `json:"recipient_owner,omitempty"`
}

type historicalNativeCapture struct {
	Name               string                  `json:"name"`
	Address            uint32                  `json:"address"`
	Global             *string                 `json:"global,omitempty"`
	DereferenceOffsets []uint32                `json:"dereference_offsets,omitempty"`
	Bytes              uint32                  `json:"bytes"`
	Repeat             *historicalNativeRepeat `json:"repeat,omitempty"`
}

// Count is read from original memory; Maximum and Stride are reviewed bounds.
// This admits tuple-backed vectors without inventing a contiguous copy in Wasm.
type historicalNativeRepeat struct {
	Count   historicalNativePointer `json:"count"`
	Maximum uint32                  `json:"maximum"`
	Stride  uint32                  `json:"stride"`
}

type historicalNativePointer struct {
	Address            uint32   `json:"address"`
	Global             *string  `json:"global,omitempty"`
	DereferenceOffsets []uint32 `json:"dereference_offsets,omitempty"`
}

type historicalNativeMemory struct {
	Name         string  `json:"name"`
	Address      uint32  `json:"address"`
	BytesHex     string  `json:"bytes_hex"`
	ElementCount *uint32 `json:"element_count,omitempty"`
}

type historicalExecutionStateValue struct {
	KeyHex   string  `json:"key_hex"`
	ValueHex *string `json:"value_hex"`
}

type historicalNativeObservation struct {
	ExecutionPhaseHex *string                          `json:"execution_phase_hex"`
	Memory            []historicalNativeMemory         `json:"memory"`
	ExecutionState    *[]historicalExecutionStateValue `json:"execution_state,omitempty"`
}

type historicalStorageReturn struct {
	Present      bool    `json:"present"`
	ValueHex     *string `json:"value_hex"`
	Offset       *uint32 `json:"offset"`
	OutputLength *uint32 `json:"output_length"`
}

type historicalReplayFrame struct {
	FunctionIndex  uint32 `json:"function_index"`
	FunctionOffset uint32 `json:"function_offset"`
}

type historicalReplayObservation struct {
	Ordinal       uint64                       `json:"ordinal"`
	Purpose       string                       `json:"purpose"`
	Operation     string                       `json:"operation"`
	KeyHex        string                       `json:"key_hex"`
	ValueHex      *string                      `json:"value_hex"`
	Stack         []historicalReplayFrame      `json:"stack"`
	StorageReturn *historicalStorageReturn     `json:"storage_return,omitempty"`
	Native        *historicalNativeObservation `json:"native,omitempty"`
}

type historicalReplayObservations struct {
	ProfileSha256                   historicalReplayDigest        `json:"profile_sha256"`
	SourceReviewSha256              historicalReplayDigest        `json:"source_review_sha256"`
	Authority                       string                        `json:"authority"`
	OriginalFunctionBodiesPreserved bool                          `json:"original_function_bodies_preserved"`
	HostCalls                       uint64                        `json:"host_calls"`
	DiscardedOnRollback             uint64                        `json:"discarded_on_rollback"`
	Observations                    []historicalReplayObservation `json:"observations"`
	FeeEvents                       *historicalReplayFeeEvents    `json:"fee_events,omitempty"`
	PrincipalMutations              []historicalPrincipalMutation `json:"principal_mutations,omitempty"`
}

// Fixed-width address decoding refuses the standard array decoder's implicit
// padding/truncation, just as the existing digest wire decoder does.
type historicalReplayAddress [20]byte

func (self *historicalReplayAddress) UnmarshalJSON(raw []byte) error {
	var values []uint16
	if err := json.Unmarshal(raw, &values); err != nil || len(values) != len(self) {
		return errors.Join(errors.New("historical address requires exactly20 bytes"), err)
	}
	for index, value := range values {
		if value > 255 {
			return errors.New("historical address contains a non-byte value")
		}
		self[index] = byte(value)
	}
	return nil
}

type historicalReplayFeeEvent struct {
	ObservationOrdinal uint64                   `json:"observation_ordinal"`
	Purpose            string                   `json:"purpose"`
	Phase              string                   `json:"phase"`
	ExtrinsicIndex     *uint32                  `json:"extrinsic_index"`
	Event              string                   `json:"event"`
	Payer              *historicalReplayDigest  `json:"payer"`
	AmountRao          *string                  `json:"amount_rao"`
	Source             *historicalReplayAddress `json:"source"`
	TransactionHash    *historicalReplayDigest  `json:"transaction_hash"`
	EventSha256        historicalReplayDigest   `json:"event_sha256"`
}

type historicalReplayFeeCandidate struct {
	TransactionHash historicalReplayDigest `json:"transaction_hash"`
	Payer           historicalReplayDigest `json:"payer"`
	ExtrinsicIndex  *uint32                `json:"extrinsic_index"`
	WithdrawalRao   *string                `json:"withdrawal_rao"`
	RefundRao       *string                `json:"refund_rao"`
	DebitRao        *string                `json:"debit_rao"`
	Status          string                 `json:"status"`
	EventOrdinals   []uint64               `json:"event_ordinals"`
}

type historicalReplayFeeEvents struct {
	MetadataSha256     historicalReplayDigest         `json:"metadata_sha256"`
	Authority          string                         `json:"authority"`
	Events             []historicalReplayFeeEvent     `json:"events"`
	Candidates         []historicalReplayFeeCandidate `json:"candidates"`
	UnmatchedFeeEvents uint64                         `json:"unmatched_fee_events"`
}

// The engine owns Wasm body/range/metadata interpretation. The caller admits
// only a bounded, exact profile and never treats its review digest as approval.
func (self *historicalReplayObservationProfile) validate(job historicalReplayJob) error {
	if self == nil {
		return nil
	}
	if (self.Schema != "urnetwork-original-wasm-hook-observation-v1" && self.Schema != historicalNativeProfileSchema) || self.RuntimeCodeSha256 != job.RuntimeCodeSha256 || self.SourceReviewSha256 == (historicalReplayDigest{}) || len(self.Rules) == 0 || len(self.Rules) > 32 || self.MetadataSha256 != nil && *self.MetadataSha256 == (historicalReplayDigest{}) {
		return errors.New("historical observation profile differs or exceeds bound")
	}
	if err := validateHistoricalPrincipalPrefixes(self.PrincipalStoragePrefixes); err != nil {
		return err
	}
	if self.PrincipalStoragePrefixes != nil && self.Schema != historicalNativeProfileSchema {
		return errors.New("principal storage scope requires native profile")
	}
	if err := self.validateEpochLayout(); err != nil {
		return err
	}
	if err := self.validateRecipientLayout(); err != nil {
		return err
	}
	if err := self.validateOriginalGlobals(); err != nil {
		return err
	}
	for index, rule := range self.Rules {
		if !(historicalReplayPurpose(rule.Purpose) || self.Schema == historicalNativeProfileSchema && historicalNativePurpose(rule.Purpose)) || rule.FunctionBodySha256 == (historicalReplayDigest{}) || rule.OffsetStart >= rule.OffsetEnd {
			return errors.New("historical observation rule identity or range differs")
		}
		if err := validateHistoricalStorageCallRule(self, rule); err != nil {
			return err
		}
		if err := validateHistoricalHostSnapshotRule(self, rule); err != nil {
			return err
		}
		if err := validateHistoricalNativeCaptures(rule); err != nil {
			return err
		}
		for _, prior := range self.Rules[:index] {
			if (prior.StorageCall != nil && rule.StorageCall != nil || prior.FunctionIndex == rule.FunctionIndex && rule.OffsetStart < prior.OffsetEnd && prior.OffsetStart < rule.OffsetEnd) && historicalStoragePathsOverlap(prior, rule) {
				return errors.New("historical observation rules overlap")
			}
		}
	}
	return nil
}

func historicalReplayPurpose(purpose string) bool {
	return purpose == "fee-withdraw" || purpose == "fee-refund" || purpose == "ethereum-executed"
}

func historicalReplayHex(raw string, maximum int) ([]byte, error) {
	if !strings.HasPrefix(raw, "0x") || len(raw) > 2+2*maximum || len(raw)%2 != 0 || raw != strings.ToLower(raw) {
		return nil, errors.New("historical observation hexadecimal shape differs")
	}
	return hex.DecodeString(raw[2:])
}

func historicalReplayAmount(value *string) (uint64, bool) {
	if value == nil {
		return 0, true
	}
	number, err := strconv.ParseUint(*value, 10, 64)
	return number, err == nil && strconv.FormatUint(number, 10) == *value
}

// Validation is bounded by the original trace grammar. Economic interpretation
// remains in the pinned engine; this boundary refuses contradictory wire facts.
func validateHistoricalReplayObservations(job historicalReplayJob, trace *historicalReplayObservations) error {
	profile := job.ObservationProfile
	if profile == nil && trace == nil {
		return nil
	}
	if profile == nil || trace == nil {
		return errors.New("historical observation profile/report presence differs")
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	maximumRecords, maximumBytes := uint64(4096), 2*1024*1024
	if profile.Schema == historicalNativeProfileSchema {
		maximumRecords, maximumBytes = 16384, 32*1024*1024
		for _, rule := range profile.Rules {
			if historicalYumaPurpose(rule.Purpose) {
				maximumRecords = 6 * 4096
				break
			}
		}
	}
	if trace.ProfileSha256 != historicalReplayDigest(sha256.Sum256(raw)) || trace.SourceReviewSha256 != profile.SourceReviewSha256 || trace.Authority != "caller-supplied-unapproved-callsite-profile" || !trace.OriginalFunctionBodiesPreserved || trace.HostCalls > 65536 || trace.DiscardedOnRollback > maximumRecords || uint64(len(trace.Observations))+trace.DiscardedOnRollback > maximumRecords {
		return errors.New("historical observation binding, authority or resource bound differs")
	}
	observations := make(map[uint64]historicalReplayObservation, len(trace.Observations))
	var previous uint64
	var encodedBytes int
	for _, observation := range trace.Observations {
		if observation.Ordinal <= previous || observation.Ordinal > trace.HostCalls || len(observation.Stack) == 0 || len(observation.Stack) > 64 {
			return errors.New("historical observation order or stack bound differs")
		}
		previous = observation.Ordinal
		if _, err := historicalReplayHex(observation.KeyHex, 512); err != nil {
			return err
		}
		if observation.ValueHex != nil {
			if _, err := historicalReplayHex(*observation.ValueHex, 1024*1024); err != nil {
				return err
			}
		}
		switch observation.Operation {
		case "set", "append":
			if observation.ValueHex == nil {
				return errors.New("historical value-bearing observation omits bytes")
			}
		case "get", "read", "clear", "clear_prefix", "exists", "next_key", "host":
			if observation.ValueHex != nil {
				return errors.New("historical request-only observation invents a value")
			}
		default:
			return errors.New("historical observation operation differs")
		}
		matches := 0
		for _, rule := range profile.Rules {
			for frameIndex := range observation.Stack {
				if historicalRuleMatchesAt(rule, observation.Stack, frameIndex) {
					if err := validateHistoricalHostSnapshotRecord(profile, rule, observation, frameIndex); err != nil {
						return err
					}
					if rule.StorageCall != nil && rule.StorageCall.Operation != observation.Operation {
						return errors.New("original storage call operation differs")
					}
					if rule.Purpose != observation.Purpose {
						return errors.New("historical observation purpose contradicts original profile")
					}
					matches++
					break
				}
			}
		}
		if matches != 1 {
			return errors.New("historical observation lacks one original callsite")
		}
		if err := validateHistoricalNativeObservation(profile, observation); err != nil {
			return err
		}
		encoded, err := json.Marshal(observation)
		if err != nil {
			return err
		}
		encodedBytes += len(encoded)
		if encodedBytes > maximumBytes {
			return errors.New("historical retained observation bytes exceed bound")
		}
		observations[observation.Ordinal] = observation
	}
	if err := validateHistoricalPrincipalMutations(profile, trace); err != nil {
		return err
	}
	fees := trace.FeeEvents
	if !historicalProfileFeeEvents(profile) {
		if fees != nil {
			return errors.New("historical fee candidates lack explicit metadata-pinned fee callsites")
		}
		return nil
	}
	if profile.MetadataSha256 == nil || fees == nil || fees.MetadataSha256 != *profile.MetadataSha256 || fees.Authority != "original-runtime-metadata-and-unapproved-callsite-profile" || len(fees.Events) > len(observations) || len(fees.Candidates) > len(fees.Events) || fees.UnmatchedFeeEvents > uint64(len(fees.Events)) {
		return errors.New("historical fee candidate metadata, authority or bound differs")
	}
	events := make(map[uint64]historicalReplayFeeEvent, len(fees.Events))
	eventKey := "0x" + hex.EncodeToString(append(xxhash.New128([]byte("System")).Sum(nil), xxhash.New128([]byte("Events")).Sum(nil)...))
	executions := 0
	previous = 0
	for _, event := range fees.Events {
		observation, ok := observations[event.ObservationOrdinal]
		if !ok || event.ObservationOrdinal <= previous || observation.Operation != "append" || observation.KeyHex != eventKey || observation.ValueHex == nil || observation.Purpose != event.Purpose {
			return errors.New("historical fee event lacks original ordered append")
		}
		previous = event.ObservationOrdinal
		value, err := historicalReplayHex(*observation.ValueHex, 1024*1024)
		if err != nil || event.EventSha256 != historicalReplayDigest(sha256.Sum256(value)) {
			return errors.Join(errors.New("historical fee event bytes differ"), err)
		}
		if event.Phase == "apply-extrinsic" {
			if event.ExtrinsicIndex == nil || uint64(*event.ExtrinsicIndex) >= uint64(len(job.ExtrinsicsHex)) {
				return errors.New("historical fee event body placement differs")
			}
		} else if (event.Phase != "initialization" && event.Phase != "finalization") || event.ExtrinsicIndex != nil {
			return errors.New("historical fee event phase differs")
		}
		if event.Event == "Ethereum.Executed" {
			executions++
			if event.Purpose != "ethereum-executed" || event.Source == nil || event.TransactionHash == nil || event.Payer != nil || event.AmountRao != nil {
				return errors.New("historical execution candidate fields differ")
			}
		} else if (event.Event != "Balances.Withdraw" || event.Purpose != "fee-withdraw") && (event.Event != "Balances.Deposit" || event.Purpose != "fee-refund") || event.Payer == nil || event.AmountRao == nil || event.Source != nil || event.TransactionHash != nil {
			return errors.New("historical balance candidate fields differ")
		}
		if _, valid := historicalReplayAmount(event.AmountRao); !valid {
			return errors.New("historical fee event amount is not an exact u64")
		}
		events[event.ObservationOrdinal] = event
	}
	transactions := make(map[historicalReplayDigest]bool, len(fees.Candidates))
	if executions != len(fees.Candidates) {
		return errors.New("historical fee candidates omit or invent executions")
	}
	usedEvents := make(map[uint64]bool, len(fees.Events))
	for _, candidate := range fees.Candidates {
		withdrawal, validWithdrawal := historicalReplayAmount(candidate.WithdrawalRao)
		refund, validRefund := historicalReplayAmount(candidate.RefundRao)
		debit, validDebit := historicalReplayAmount(candidate.DebitRao)
		if !validWithdrawal || !validRefund || !validDebit || transactions[candidate.TransactionHash] || len(candidate.EventOrdinals) == 0 || len(candidate.EventOrdinals) > 3 {
			return errors.New("historical fee candidate amount, identity or references differ")
		}
		transactions[candidate.TransactionHash] = true
		if candidate.ExtrinsicIndex != nil && uint64(*candidate.ExtrinsicIndex) >= uint64(len(job.ExtrinsicsHex)) {
			return errors.New("historical fee candidate body placement differs")
		}
		if candidate.Status == "observed-pair-unadmitted" {
			if candidate.ExtrinsicIndex == nil || candidate.WithdrawalRao == nil || candidate.RefundRao == nil || candidate.DebitRao == nil || refund > withdrawal || withdrawal-refund != debit {
				return errors.New("historical candidate debit contradicts actual pair")
			}
		} else if candidate.Status != "placement-unresolved" && candidate.Status != "withdrawal-unobserved" && candidate.Status != "refund-unobserved" || candidate.DebitRao != nil {
			return errors.New("historical candidate claims an unestablished fee")
		}
		last := uint64(0)
		var observedWithdrawal, observedRefund *string
		for index, ordinal := range candidate.EventOrdinals {
			event, ok := events[ordinal]
			if !ok || ordinal <= last || usedEvents[ordinal] || (event.ExtrinsicIndex == nil) != (candidate.ExtrinsicIndex == nil) || event.ExtrinsicIndex != nil && *event.ExtrinsicIndex != *candidate.ExtrinsicIndex {
				return errors.New("historical fee candidate lost original events")
			}
			usedEvents[ordinal] = true
			last = ordinal
			if index == len(candidate.EventOrdinals)-1 {
				if event.Event != "Ethereum.Executed" || event.TransactionHash == nil || *event.TransactionHash != candidate.TransactionHash || event.Source == nil || candidate.Payer != historicalReplayDigest(blake2b.Sum256(append([]byte("evm:"), event.Source[:]...))) {
					return errors.New("historical fee candidate transaction or payer differs")
				}
			} else {
				if event.Payer == nil || *event.Payer != candidate.Payer || event.AmountRao == nil {
					return errors.New("historical fee candidate payment differs from event")
				}
				switch event.Event {
				case "Balances.Withdraw":
					if observedWithdrawal != nil || observedRefund != nil {
						return errors.New("historical fee candidate withdrawal order differs")
					}
					observedWithdrawal = event.AmountRao
				case "Balances.Deposit":
					if observedRefund != nil {
						return errors.New("historical fee candidate refund is duplicated")
					}
					observedRefund = event.AmountRao
				default:
					return errors.New("historical fee candidate includes a foreign event")
				}
			}
		}
		for _, pair := range [][2]*string{{candidate.WithdrawalRao, observedWithdrawal}, {candidate.RefundRao, observedRefund}} {
			if (pair[0] == nil) != (pair[1] == nil) || pair[0] != nil && *pair[0] != *pair[1] {
				return errors.New("historical fee candidate amount differs from original events")
			}
		}
		switch candidate.Status {
		case "placement-unresolved":
			if candidate.ExtrinsicIndex != nil || observedWithdrawal != nil || observedRefund != nil {
				return errors.New("historical unresolved placement fabricates attribution")
			}
		case "withdrawal-unobserved":
			if candidate.ExtrinsicIndex == nil || observedWithdrawal != nil {
				return errors.New("historical unknown withdrawal contradicts its event")
			}
		case "refund-unobserved":
			if candidate.ExtrinsicIndex == nil || observedWithdrawal == nil || observedRefund != nil {
				return errors.New("historical unknown refund contradicts its events")
			}
		}
	}
	if uint64(len(fees.Events)-len(usedEvents)) != fees.UnmatchedFeeEvents {
		return errors.New("historical unmatched fee event census differs")
	}
	return nil
}
