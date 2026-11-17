// Native incentive events are observed separately from the policy denominator,
// provider entitlement, collateral and actual recycling. No observation signs.
package main

import (
	"errors"
	"math"
)

const economicEmissionPolicySchema = "urnetwork-native-miner-observation-policy-v1"
const economicEmissionSchema = "urnetwork-native-miner-observation-v1"
const economicEmissionBlockLimit = 128
const economicEmissionBytesLimit = 8 * 1024 * 1024
const economicEmissionEventBytesLimit = 1024 * 1024
const economicEmissionEventLimit = 16384
const economicEmissionMechanismStride = 4096

// A boundary is an exact numbered native hash, never an implicit latest block.
type economicEmissionBoundary struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

// This independent read policy does not grant action or economic authority.
// From is excluded; every following block through Through must be observed.
type economicEmissionPolicy struct {
	Execution               *nativeExecutionPolicy   `json:"execution_witness,omitempty"`
	Schema                  string                   `json:"schema"`
	Network                 planNetwork              `json:"network"`
	Runtime                 rootReceiptProfile       `json:"runtime"`
	Netuid                  uint16                   `json:"netuid"`
	SubnetRegistrationBlock *uint64                  `json:"subnet_registration_block"`
	SubnetGeneration        *uint64                  `json:"subnet_generation"`
	From                    economicEmissionBoundary `json:"from_exclusive"`
	Through                 economicEmissionBoundary `json:"through_inclusive"`
	MaximumUids             uint16                   `json:"maximum_uids"`
	FeePayers               []string                 `json:"fee_payers,omitempty"`
}

// Missing generations, unknown sources and an unbounded archive window fail
// before any route is opened. Runtime numbers are never mainnet defaults.
func (self economicEmissionPolicy) validate() error {
	version := self.Runtime.RuntimeVersion
	if self.Schema != economicEmissionPolicySchema || self.Network.NativeChain == "" || self.Network.EvmChainId != mainnetEvmChainId ||
		!rootCanonicalHash(self.Network.GenesisHash) || self.Netuid != 25 || !mainnetRuntimeCodecSource(self.Runtime.RuntimeSourceCommit) ||
		version.SpecName == "" || version.SpecVersion == 0 || version.TransactionVersion == 0 || version.StateVersion != 1 ||
		!rootCanonicalHash(self.Runtime.RuntimeCodeHash) || !rootCanonicalHash(self.Runtime.RuntimeMetadataHash) {
		return errors.New("native miner observation requires exact independent mainnet identity and reviewed runtime artifacts")
	}
	if self.SubnetRegistrationBlock == nil || self.SubnetGeneration == nil || *self.SubnetRegistrationBlock > self.From.Number ||
		self.From.Number == 0 || self.Through.Number > math.MaxUint32 || self.Through.Number <= self.From.Number ||
		self.Through.Number-self.From.Number > economicEmissionBlockLimit || !rootCanonicalHash(self.From.Hash) ||
		!rootCanonicalHash(self.Through.Hash) || self.From.Hash == self.Through.Hash || self.MaximumUids == 0 || self.MaximumUids > rootCensusLimit {
		return errors.New("native miner observation requires an explicit generation, 1..128 exact blocks and 1..4096 UID bound")
	}
	if len(self.FeePayers) > 16 {
		return errors.New("native fee observation exceeds 16 independently expected payers")
	}
	seen := map[string]bool{}
	for _, payer := range self.FeePayers {
		if !rootCanonicalHash(payer) || seen[payer] {
			return errors.New("native fee payer is missing, malformed or repeated")
		}
		seen[payer] = true
	}
	if self.Execution != nil && self.Execution.Principal != nil {
		principal := self.Execution.Principal
		if principal.Parent.Number > self.From.Number || principal.Parent.Number == self.From.Number && principal.Parent.Hash != self.From.Hash {
			return errors.New("native principal authority replaced its original opening parent")
		}
		for _, query := range principal.Queries {
			if query.Netuid != self.Netuid {
				return errors.New("native principal authority borrowed another subnet")
			}
		}
	}
	return self.Execution.validate()
}

// Raw storage is retained alongside decoded epoch and pending-budget facts.
// Pending snapshots do not reveal accrual that was drained earlier in a block.
type economicEmissionState struct {
	Boundary              economicEmissionBoundary `json:"boundary"`
	SubnetRegistration    uint64                   `json:"subnet_registration_block"`
	SubnetGeneration      uint64                   `json:"subnet_generation"`
	SubnetUids            uint16                   `json:"subnet_uids"`
	MechanismCount        uint8                    `json:"mechanism_count"`
	Epoch                 uint64                   `json:"subnet_epoch_index"`
	LastEpochBlock        uint64                   `json:"last_epoch_block"`
	Tempo                 uint16                   `json:"tempo"`
	PendingEpochAt        uint64                   `json:"pending_epoch_at"`
	PendingServerAlpha    string                   `json:"pending_server_alpha"`
	PendingValidatorAlpha string                   `json:"pending_validator_alpha"`
	PendingRootAlpha      string                   `json:"pending_root_alpha"`
	PendingOwnerAlpha     string                   `json:"pending_owner_alpha"`
	BlockAlphaOut         string                   `json:"stored_block_alpha_out"`
	OwnerCutEnabled       bool                     `json:"owner_cut_enabled"`
	OwnerCutParts         uint16                   `json:"owner_cut_parts"`
	RecycleMode           string                   `json:"recycle_mode"`
	Storage               []rootStorageValue       `json:"storage"`
	ModeStorageKey        string                   `json:"mode_storage_key"`
	ModeRawStorage        *string                  `json:"mode_raw_storage"`
}

// Event position and block hash identify an occurrence. UID names the runtime
// event's slot, never an inferred hotkey, provider role or retained generation.
type economicEmissionEvent struct {
	EventIndex uint64   `json:"event_index"`
	Kind       string   `json:"kind"`
	Netuid     uint16   `json:"netuid"`
	Mechanism  uint8    `json:"mechanism"`
	AlphaByUid []string `json:"alpha_by_uid,omitempty"`
	TotalAlpha string   `json:"incentive_total_alpha,omitempty"`
	FromBlock  uint64   `json:"from_block,omitempty"`
	ToBlock    uint64   `json:"to_block,omitempty"`
}

// Scheduling and owner changes explain execution order, not economic amounts.
// The indexed phase and exact values remain separate from terminal epoch events.
type economicEmissionContextEvent struct {
	EventIndex      uint64  `json:"event_index"`
	Kind            string  `json:"kind"`
	Netuid          uint16  `json:"netuid"`
	Phase           string  `json:"phase"`
	ExtrinsicIndex  *uint32 `json:"extrinsic_index,omitempty"`
	Tempo           *uint16 `json:"tempo,omitempty"`
	OldOwnerColdkey string  `json:"old_owner_coldkey,omitempty"`
	NewOwnerColdkey string  `json:"new_owner_coldkey,omitempty"`
}

// Unknown amounts stay null. In particular, a zero incentive vector cannot
// erase a positive pre-withholding tranche redirected to validator dividends.
type economicEmissionDenominator struct {
	Status                     string   `json:"status"`
	ObservedIncentiveAlpha     string   `json:"observed_incentive_alpha"`
	PriorPendingServerAlpha    string   `json:"prior_pending_server_alpha"`
	AfterPendingServerAlpha    string   `json:"after_pending_server_alpha"`
	NativeMinerAllocationAlpha *string  `json:"native_miner_allocation_alpha"`
	RuntimeTruncationDustAlpha *string  `json:"runtime_truncation_dust_alpha"`
	ZeroIncentiveFallback      bool     `json:"zero_incentive_fallback_possible"`
	Complete                   bool     `json:"complete"`
	Blockers                   []string `json:"blockers"`
}

// Headers and complete raw events are retained even if a later read fails.
// A block's event execution uses the approved parent runtime, not a later tip.
type economicEmissionBlock struct {
	ExecutionOutcome *nativeExecutionOutcome        `json:"execution_outcome,omitempty"`
	ExecutionRuntime *rootReceiptProfile            `json:"execution_runtime,omitempty"`
	PostStateRuntime *rootReceiptProfile            `json:"post_state_runtime,omitempty"`
	Boundary         economicEmissionBoundary       `json:"boundary"`
	Header           rootReceiptHeader              `json:"header"`
	BodyCount        int                            `json:"body_count"`
	EventsHash       string                         `json:"events_hash"`
	RawEvents        string                         `json:"raw_events"`
	RawEventsStorage *string                        `json:"raw_events_storage"`
	Before           *economicEmissionState         `json:"before"`
	After            *economicEmissionState         `json:"after"`
	Events           []economicEmissionEvent        `json:"events"`
	ContextEvents    []economicEmissionContextEvent `json:"context_events,omitempty"`
	Fees             []economicNativeFee            `json:"native_fees,omitempty"`
	Denominator      economicEmissionDenominator    `json:"denominator"`
}

// Complete means the explicit archive range was read and rechecked. It never
// means the economic target, denominator, payment or finality authority passed.
type economicEmissionObservation struct {
	runtimeAdmission            *nativeProducerRuntimeAdmission
	ExecutionWindow             *nativeExecutionWindow        `json:"execution_window,omitempty"`
	ExecutionProducer           *nativeExecutionProducerState `json:"execution_producer,omitempty"`
	RuntimeCatalog              []monitorEconomicRuntimeEntry `json:"runtime_catalog,omitempty"`
	Schema                      string                        `json:"schema"`
	PolicyHash                  string                        `json:"policy_hash"`
	Policy                      economicEmissionPolicy        `json:"policy"`
	Status                      string                        `json:"status"`
	Complete                    bool                          `json:"complete"`
	FinalityAuthority           string                        `json:"finality_authority"`
	RuntimeSourceProven         bool                          `json:"runtime_source_proven"`
	IndependentStorageProof     bool                          `json:"independent_storage_proof"`
	Finalized                   economicEmissionBoundary      `json:"observed_finalized"`
	FinalizedHeader             *rootReceiptHeader            `json:"finalized_header"`
	Ancestry                    []rootReceiptHeader           `json:"ancestry"`
	ClosingFinalized            economicEmissionBoundary      `json:"closing_finalized"`
	ClosingAncestry             []rootReceiptHeader           `json:"closing_ancestry"`
	HistoricalFinality          string                        `json:"historical_finality,omitempty"`
	RangeAncestry               []rootReceiptHeader           `json:"range_ancestry,omitempty"`
	ClosingFinalizedHeader      *rootReceiptHeader            `json:"closing_finalized_header,omitempty"`
	RequestedThrough            *economicEmissionBoundary     `json:"requested_through,omitempty"`
	MetadataHex                 string                        `json:"metadata_hex"`
	InitialState                *economicEmissionState        `json:"initial_state"`
	Blocks                      []economicEmissionBlock       `json:"blocks"`
	AttemptedBlock              *economicEmissionBlock        `json:"attempted_block,omitempty"`
	ObservedIncentiveTotalAlpha string                        `json:"observed_incentive_total_alpha"`
	NativeMinerAllocationAlpha  *string                       `json:"native_miner_allocation_alpha"`
	ProviderEntitlementAlpha    *string                       `json:"provider_entitlement_alpha"`
	OwnerRecycledAlpha          *string                       `json:"owner_recycled_alpha"`
	QuantizationToleranceAlpha  *string                       `json:"quantization_tolerance_alpha"`
	TargetMet                   *bool                         `json:"target_met"`
	ActualNativeOutcomeVerified bool                          `json:"actual_native_outcome_verified"`
	ActivationReady             bool                          `json:"activation_ready"`
	Issue                       string                        `json:"issue,omitempty"`
	Blockers                    []string                      `json:"economic_blockers"`
	ContentHash                 string                        `json:"content_hash"`
}
