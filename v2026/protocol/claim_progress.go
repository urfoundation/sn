// Claim observations describe retained evidence, not signing or payment
// authority. Accepted liability and aggregate coldkey transfer stay distinct.
package protocol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strings"
	"time"
)

const ClaimProgressSchema = "urnetwork-claim-progress-v1"
const ClaimObservationSchema = "urnetwork-claim-observation-v1"
const MaxClaimProgressEntries = 64
const MaxClaimProgressBytes = 256 * 1024

// Configuration supplies this pool independently of API responses, JWTs and
// transport ClientIds. It does not by itself prove deployment or finality.
type ClaimProgressPool struct {
	ChainId uint64 `json:"chain_id" yaml:"chain_id"`
	Vault   string `json:"vault" yaml:"vault"`
	NoId    string `json:"no_id" yaml:"no_id"`
	Coldkey string `json:"coldkey" yaml:"coldkey"`
}

func claimProgressHex(value string, size int) bool {
	if len(value) != 2+2*size || !strings.HasPrefix(value, "0x") || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

// Empty means unknown; a canonical zero is a separately known amount.
func ClaimProgressAmount(value string) bool {
	if len(value) == 0 || len(value) > 78 || len(value) > 1 && value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	return ok && parsed.Sign() >= 0 && parsed.BitLen() <= 256
}

func (self ClaimProgressPool) Validate() error {
	if self.ChainId == 0 || !claimProgressHex(self.Vault, 20) || self.Vault == "0x"+strings.Repeat("0", 40) || !ClaimProgressAmount(self.NoId) || self.NoId == "0" || !claimProgressHex(self.Coldkey, 32) || self.Coldkey == "0x"+strings.Repeat("0", 64) {
		return errors.New("claim pool needs exact chain, vault, operator and payout coldkey")
	}
	return nil
}

// Values are exact decimal uint256. AggregatePaidRao may include older epochs
// and other operators; it cannot be allocated to this epoch without a ledger.
type ClaimObservation struct {
	Schema            string            `json:"schema"`
	EvidenceKind      string            `json:"evidence_kind"`
	Authority         string            `json:"authority"`
	GenesisStatus     string            `json:"genesis_status"`
	Epoch             int64             `json:"epoch"`
	ObservedAt        string            `json:"observed_at"`
	Pool              ClaimProgressPool `json:"pool"`
	ShareBps          uint64            `json:"share_bps,omitempty"`
	ProofStatus       string            `json:"proof_status"`
	PayoutRoot        string            `json:"payout_root,omitempty"`
	ArtifactHash      string            `json:"artifact_hash,omitempty"`
	LeafClaimed       *bool             `json:"leaf_claimed,omitempty"`
	BlockNumber       uint64            `json:"block_number,omitempty"`
	BlockHash         string            `json:"block_hash,omitempty"`
	TransactionHash   string            `json:"transaction_hash,omitempty"`
	Relayer           string            `json:"relayer,omitempty"`
	AcceptedAmountRao string            `json:"accepted_amount_rao,omitempty"`
	PaymentStatus     string            `json:"payment_status"`
	UnpaidCreditRao   string            `json:"unpaid_credit_rao,omitempty"`
	AggregatePaidRao  string            `json:"aggregate_paid_rao,omitempty"`
}

func (self ClaimObservation) Validate() error {
	if self.Schema != ClaimObservationSchema || self.Epoch < 0 || self.GenesisStatus != "unverified" {
		return errors.New("claim observation identity differs")
	}
	when, err := time.Parse(time.RFC3339Nano, self.ObservedAt)
	if err != nil || when.IsZero() {
		return errors.New("claim observation time is absent")
	}
	if self.EvidenceKind == "api-no-claim" {
		if self.Authority != "api-assertion" {
			return errors.New("API observation authority differs")
		}
		if self.Pool != (ClaimProgressPool{}) || self.ShareBps != 0 || self.ProofStatus != "unknown" || self.PaymentStatus != "unknown" || self.BlockNumber != 0 || self.BlockHash != "" || self.TransactionHash != "" || self.Relayer != "" || self.AcceptedAmountRao != "" || self.UnpaidCreditRao != "" || self.AggregatePaidRao != "" || self.PayoutRoot != "" || self.ArtifactHash != "" || self.LeafClaimed != nil {
			return errors.New("API absence cannot prove chain identity or payment")
		}
		return nil
	}
	if self.Authority != "configured-rpc-assertion" {
		return errors.New("claim observation does not carry independent native finality authority")
	}
	if self.Pool.Validate() != nil || self.ShareBps == 0 || self.ShareBps > 10000 || self.BlockNumber == 0 || !claimProgressHex(self.BlockHash, 32) {
		return errors.New("claim chain observation coordinates differ")
	}
	if self.ArtifactHash != "" && !claimProgressHex(self.ArtifactHash, 32) {
		return errors.New("claim artifact hash differs")
	}
	switch self.EvidenceKind {
	case "finalized-leaf":
		if self.LeafClaimed == nil || !claimProgressHex(self.PayoutRoot, 32) || (self.ProofStatus != "merkle-verified" && self.ProofStatus != "unknown") || self.TransactionHash != "" || self.Relayer != "" || self.AcceptedAmountRao != "" || self.PaymentStatus != "unknown" || self.UnpaidCreditRao != "" || self.AggregatePaidRao != "" {
			return errors.New("leaf state cannot prove an exact receipt or payment")
		}
	case "signed-receipt":
		if self.ProofStatus != "contract-accepted" || self.LeafClaimed != nil || self.PayoutRoot != "" || self.ArtifactHash != "" || !claimProgressHex(self.TransactionHash, 32) || !claimProgressHex(self.Relayer, 20) || !ClaimProgressAmount(self.AcceptedAmountRao) {
			return errors.New("signed claim receipt coordinates differ")
		}
		switch self.PaymentStatus {
		case "unknown", "invalid":
			if self.UnpaidCreditRao != "" || self.AggregatePaidRao != "" {
				return errors.New("unverified payment has an amount")
			}
		case "deferred":
			if !claimProgressAmountAtLeast(self.UnpaidCreditRao, self.AcceptedAmountRao) || self.AggregatePaidRao != "" {
				return errors.New("deferred payment grammar differs")
			}
		case "aggregate-paid":
			if !claimProgressAmountAtLeast(self.AggregatePaidRao, self.AcceptedAmountRao) || self.UnpaidCreditRao != "" {
				return errors.New("aggregate payment grammar differs")
			}
		default:
			return errors.New("unknown payment observation kind")
		}
	default:
		return errors.New("unknown claim observation kind")
	}
	return nil
}

// Settlement happens after adding the current accepted liability to credit.
func claimProgressAmountAtLeast(amount, accepted string) bool {
	if !ClaimProgressAmount(amount) || !ClaimProgressAmount(accepted) {
		return false
	}
	left, _ := new(big.Int).SetString(amount, 10)
	right, _ := new(big.Int).SetString(accepted, 10)
	return left.Cmp(right) >= 0
}

// Only sanitized fields cross the public boundary. Raw signed bytes, private
// paths, credentials, RPC addresses and arbitrary error text are excluded.
type ClaimProgressEntry struct {
	Epoch             int64             `json:"epoch"`
	QueueStatus       string            `json:"queue_status"`
	ObservationStatus string            `json:"observation_status"`
	DomainStatus      string            `json:"domain_status"`
	Observation       *ClaimObservation `json:"observation,omitempty"`
}

// Sequence/PublishedAt describe durable acknowledgements, including repeated
// checks and retry bookkeeping; they do not imply settlement progress. Counts
// cover the retained census, with only bounded detail and no ledger authority.
type ClaimProgress struct {
	Schema                   string               `json:"schema"`
	Member                   string               `json:"member"`
	Status                   string               `json:"status"`
	InstanceId               string               `json:"instance_id,omitempty"`
	StartedAt                string               `json:"started_at,omitempty"`
	PublishedAt              string               `json:"published_at,omitempty"`
	Sequence                 uint64               `json:"sequence,omitempty"`
	QueueSha256              string               `json:"queue_sha256,omitempty"`
	DeclaredPool             *ClaimProgressPool   `json:"declared_pool,omitempty"`
	Entries                  []ClaimProgressEntry `json:"entries,omitempty"`
	OmittedEntries           uint64               `json:"omitted_entries,omitempty"`
	OmittedObservations      uint64               `json:"omitted_observations,omitempty"`
	TotalEntries             uint64               `json:"total_entries"`
	UnresolvedEntries        uint64               `json:"unresolved_entries"`
	OmittedUnresolvedEntries uint64               `json:"omitted_unresolved_entries"`
	OldestUnresolvedEpoch    *int64               `json:"oldest_unresolved_epoch,omitempty"`
	FinalizedEntries         uint64               `json:"finalized_entries"`
	NoClaimEntries           uint64               `json:"no_claim_entries"`
}

// Decode bounds the entire message before any array allocation. The producer
// has the same finite census; omitted entries never imply a complete history.
func DecodeClaimProgress(raw []byte) (*ClaimProgress, error) {
	if len(raw) == 0 || len(raw) > MaxClaimProgressBytes {
		return nil, errors.New("claim progress byte bound")
	}
	if err := ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	var result ClaimProgress
	wire := struct {
		*ClaimProgress
		Entries json.RawMessage `json:"entries"`
	}{ClaimProgress: &result}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("claim progress trailing data")
	}
	if len(wire.Entries) != 0 {
		entries := json.NewDecoder(bytes.NewReader(wire.Entries))
		entries.DisallowUnknownFields()
		token, err := entries.Token()
		if err != nil || token != json.Delim('[') {
			return nil, errors.New("claim entries must be a bounded array")
		}
		for entries.More() {
			if len(result.Entries) == MaxClaimProgressEntries {
				return nil, errors.New("claim entry count bound")
			}
			var entry ClaimProgressEntry
			if err := entries.Decode(&entry); err != nil {
				return nil, err
			}
			result.Entries = append(result.Entries, entry)
		}
		if _, err := entries.Token(); err != nil {
			return nil, err
		}
		if err := entries.Decode(new(any)); !errors.Is(err, io.EOF) {
			return nil, errors.New("claim entry array trailing bytes")
		}
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}

func (self ClaimProgress) Validate() error {
	if self.Schema != ClaimProgressSchema || len(self.Member) == 0 || len(self.Member) > 128 || len(self.Entries) > MaxClaimProgressEntries {
		return errors.New("claim progress header differs")
	}
	if self.DeclaredPool != nil && self.DeclaredPool.Validate() != nil {
		return errors.New("claim declared pool differs")
	}
	switch self.Status {
	case "unknown", "active", "unavailable", "closed":
	default:
		return errors.New("claim writer status differs")
	}
	if self.Sequence == 0 {
		if self.Status != "unknown" && self.Status != "unavailable" && self.Status != "closed" || self.QueueSha256 != "" || len(self.Entries) != 0 || self.PublishedAt != "" || self.TotalEntries != 0 || self.UnresolvedEntries != 0 || self.OmittedUnresolvedEntries != 0 || self.OldestUnresolvedEpoch != nil || self.FinalizedEntries != 0 || self.NoClaimEntries != 0 || self.OmittedEntries != 0 || self.OmittedObservations != 0 {
			return errors.New("claim unadmitted writer invented retained data")
		}
		return nil
	}
	if self.Status == "unknown" || len(self.InstanceId) != 32 || len(self.QueueSha256) != 64 || strings.ToLower(self.InstanceId) != self.InstanceId || strings.ToLower(self.QueueSha256) != self.QueueSha256 {
		return errors.New("claim publication identity differs")
	}
	if _, err := hex.DecodeString(self.InstanceId); err != nil {
		return err
	}
	if _, err := hex.DecodeString(self.QueueSha256); err != nil {
		return err
	}
	start, err := time.Parse(time.RFC3339Nano, self.StartedAt)
	if err != nil {
		return err
	}
	published, err := time.Parse(time.RFC3339Nano, self.PublishedAt)
	if err != nil || start.IsZero() || published.Before(start) {
		return errors.New("claim publication time differs")
	}
	if self.OmittedEntries > self.TotalEntries || self.TotalEntries-self.OmittedEntries != uint64(len(self.Entries)) || self.UnresolvedEntries > self.TotalEntries || self.FinalizedEntries > self.TotalEntries-self.UnresolvedEntries || self.NoClaimEntries != self.TotalEntries-self.UnresolvedEntries-self.FinalizedEntries || self.OmittedUnresolvedEntries > self.UnresolvedEntries || self.OmittedUnresolvedEntries > self.OmittedEntries {
		return errors.New("claim census counts differ")
	}
	if self.UnresolvedEntries == 0 && self.OldestUnresolvedEpoch != nil || self.UnresolvedEntries != 0 && (self.OldestUnresolvedEpoch == nil || *self.OldestUnresolvedEpoch < 0) {
		return errors.New("claim unresolved census has no oldest epoch")
	}
	selectedUnresolved := uint64(0)
	selectedFinalized, selectedNoClaim := uint64(0), uint64(0)
	oldestIncluded := self.UnresolvedEntries == 0
	seen := map[int64]bool{}
	for _, entry := range self.Entries {
		if entry.Epoch < 0 || seen[entry.Epoch] {
			return errors.New("claim progress repeats epoch")
		}
		seen[entry.Epoch] = true
		switch entry.QueueStatus {
		case "pending", "retry", "submitting", "uncertain", "finalized", "no-claim", "unknown":
		default:
			return errors.New("claim queue status differs")
		}
		if entry.QueueStatus != "finalized" && entry.QueueStatus != "no-claim" {
			selectedUnresolved++
			if self.OldestUnresolvedEpoch == nil || entry.Epoch < *self.OldestUnresolvedEpoch {
				return errors.New("claim selected epoch predates declared oldest unresolved")
			}
			oldestIncluded = oldestIncluded || entry.Epoch == *self.OldestUnresolvedEpoch
		} else if entry.QueueStatus == "finalized" {
			selectedFinalized++
		} else {
			selectedNoClaim++
		}
		if entry.DomainStatus != "unknown" && entry.DomainStatus != "match" && entry.DomainStatus != "identity" {
			return errors.New("claim domain status differs")
		}
		if entry.Observation == nil {
			if entry.ObservationStatus != "unknown" || entry.DomainStatus != "unknown" {
				return errors.New("absent claim observation invented evidence")
			}
			continue
		}
		if entry.ObservationStatus != "retained" || entry.Observation.Epoch != entry.Epoch || entry.Observation.Validate() != nil {
			return errors.New("claim retained observation differs")
		}
		observed, _ := time.Parse(time.RFC3339Nano, entry.Observation.ObservedAt)
		if observed.After(published) {
			return errors.New("claim observation is newer than its publication")
		}
		domain := "unknown"
		if self.DeclaredPool != nil && entry.Observation.EvidenceKind != "api-no-claim" {
			domain = "identity"
			if *self.DeclaredPool == entry.Observation.Pool {
				domain = "match"
			}
		}
		if entry.DomainStatus != domain {
			return errors.New("claim declared and observed domain comparison differs")
		}
	}
	if selectedUnresolved != self.UnresolvedEntries-self.OmittedUnresolvedEntries || !oldestIncluded || selectedFinalized > self.FinalizedEntries || selectedNoClaim > self.NoClaimEntries {
		return errors.New("claim bounded census hides its oldest unresolved epoch")
	}
	return nil
}
