// Original closed-work rows are a separately identified component of payout
// evidence. They reproduce the operator's database projection, not physical
// traffic, client consent, reliability, wallet eligibility or consensus truth.
package payoutartifact

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const ClosedWorkSchema = "urnetwork-original-closed-work-census-v1"
const MaxClosedWorkRecords = 32768
const MaxClosedWorkOriginalBytes = 8 * 1024 * 1024
const MaxClosedWorkRecordBytes = 1024 * 1024

var ErrClosedWorkUnavailable = errors.New("original closed-work census is unavailable or incomplete")
var ErrClosedWorkIntegrity = errors.New("original closed-work census contradicts its signed payout")
var ErrClosedWorkCapacity = errors.New("original closed-work census exceeds its finite evidence capacity")

// Bytes are the exact original jsonb reader surface, not a later reconstructed
// provider vector. Contract identity and close time disambiguate equal rows.
type ClosedWorkRecord struct {
	ContractId      [16]byte `json:"contract_id"`
	ClosedAt        string   `json:"closed_at"`
	Original        []byte   `json:"original_snapshot"`
	OriginalReports []byte   `json:"original_close_reports,omitempty"`
}

// The producer fills Records and Count in the very same complete SQL snapshot
// that supplies payout usage. Signature/content custody comes from the containing
// artifact. Count is checked, but is not proof that a dishonest source omitted no
// database rows; independently trusted provenance remains a separate obligation.
type ClosedWorkCensus struct {
	WholeInventory       *WholeWorkInventory `json:"whole_inventory,omitempty"`
	Schema               string              `json:"schema"`
	DeploymentId         string              `json:"deployment_id"`
	ChainId              uint64              `json:"chain_id"`
	GenesisHash          string              `json:"genesis_hash"`
	Netuid               uint16              `json:"netuid"`
	Coordinator          common.Address      `json:"coordinator"`
	SettlementVault      common.Address      `json:"settlement_vault"`
	Epoch                uint64              `json:"epoch"`
	NoId                 uint64              `json:"no_id"`
	PolicyHash           string              `json:"policy_hash"`
	Start                Boundary            `json:"start"`
	End                  Boundary            `json:"end"`
	WindowStart          string              `json:"window_start_utc"`
	WindowEnd            string              `json:"window_end_utc"`
	EarningPolicyHash    string              `json:"earning_policy_sha256,omitempty"`
	EarningStart         string              `json:"earning_start_utc,omitempty"`
	EarningSelectionHash string              `json:"earning_selection_sha256,omitempty"`
	Count                uint64              `json:"contract_count"`
	Records              []ClosedWorkRecord  `json:"records"`
}

// These derived facts intentionally cannot assert full authenticated provider
// measurements. Original ordinary snapshots lack signed client-close inputs.
type VerifiedClosedWork struct {
	CensusHash                string
	Contracts                 uint64
	Providers                 uint64
	UsageBytes                uint64
	OrdinarySnapshots         uint64
	ExpiredSnapshots          uint64
	UncreditedLegacySnapshots uint64
}

// Canonical order does not change any original source bytes. Called only after
// the complete bounded SQL census has been collected successfully.
func (self *ClosedWorkCensus) Sort() {
	sort.Slice(self.Records, func(i, j int) bool {
		return bytes.Compare(self.Records[i].ContractId[:], self.Records[j].ContractId[:]) < 0
	})
}

// Copy caller-owned bytes before an artifact can be signed or published.
func cloneClosedWork(ctx context.Context, census *ClosedWorkCensus) (*ClosedWorkCensus, error) {
	if census == nil {
		return nil, nil
	}
	if len(census.Records) > MaxClosedWorkRecords {
		return nil, ErrClosedWorkCapacity
	}
	copy := *census
	wholeInventory, err := cloneWholeWorkInventory(ctx, census.WholeInventory)
	if err != nil {
		return nil, err
	}
	copy.WholeInventory = wholeInventory
	copy.Records = make([]ClosedWorkRecord, len(census.Records))
	if census.Records == nil {
		copy.Records = nil
	}
	used := 0
	for index, row := range census.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(row.Original) > MaxClosedWorkRecordBytes || len(row.OriginalReports) > MaxClosedWorkRecordBytes || len(row.Original)+len(row.OriginalReports) > MaxClosedWorkOriginalBytes-used {
			return nil, ErrClosedWorkCapacity
		}
		used += len(row.Original) + len(row.OriginalReports)
		copy.Records[index] = row
		copy.Records[index].Original = bytes.Clone(row.Original)
		copy.Records[index].OriginalReports = bytes.Clone(row.OriginalReports)
	}
	return &copy, nil
}

// The exact witness is already signed by its containing payout artifact.
func (self *ClosedWorkCensus) Hash() string { return SnapshotHash(self) }

// Decode identifiers without importing the database owner's mutable directory.
func closedWorkId(text string) ([16]byte, error) {
	var value [16]byte
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' || strings.ToLower(text) != text {
		return value, ErrClosedWorkIntegrity
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(text, "-", ""))
	if err != nil || len(raw) != 16 {
		return value, ErrClosedWorkIntegrity
	}
	copy(value[:], raw)
	if value == ([16]byte{}) {
		return value, ErrClosedWorkIntegrity
	}
	return value, nil
}

// New evidence has one digest wire form; old artifact digest parsing stays unchanged.
func canonicalClosedWorkDigest(value string) bool {
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(raw) == 32 && value == "sha256:"+hex.EncodeToString(raw)
}

// Original fields retain absence separately from a real zero measurement.
type closedWorkSnapshot struct {
	Version   int    `json:"version"`
	ByteCount *int64 `json:"byte_count"`
	Providers *[]struct {
		ClientId  string `json:"client_id"`
		NetworkId string `json:"network_id"`
		ByteCount *int64 `json:"byte_count"`
	} `json:"providers"`
	ExcludedReason string `json:"excluded_reason,omitempty"`
	Expiry         *struct {
		Capacity *int64 `json:"capacity"`
		Reports  *map[string]struct {
			ByteCount  *int64 `json:"byte_count"`
			Checkpoint *bool  `json:"checkpoint"`
		} `json:"reports"`
	} `json:"expiry,omitempty"`
	Legacy *struct {
		Manifest        string `json:"repair_manifest_sha256"`
		ContractId      string `json:"contract_id"`
		Epoch           uint64 `json:"epoch"`
		ClosedAt        string `json:"closed_at"`
		Minimum         *int64 `json:"retained_report_minimum"`
		FinalAcceptance *bool  `json:"final_acceptance"`
	} `json:"legacy_exclusion,omitempty"`
}

// Semantic checks reproduce the current immutable snapshot grammar, including
// zero-credit debt and expiry lower bounds. They cannot recreate absent signed
// reports or authenticate the original operator's database history by themselves.
func decodeClosedWorkSnapshot(row ClosedWorkRecord, epoch uint64) (*closedWorkSnapshot, error) {
	var snapshot closedWorkSnapshot
	decoder := json.NewDecoder(bytes.NewReader(row.Original))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, errors.Join(ErrClosedWorkIntegrity, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrClosedWorkIntegrity
	}
	if snapshot.Version != 1 || snapshot.ByteCount == nil || snapshot.Providers == nil || *snapshot.ByteCount < 0 {
		return nil, ErrClosedWorkIntegrity
	}
	if snapshot.ExcludedReason != "" && (snapshot.ExcludedReason != "legacy_usage_unavailable" && snapshot.ExcludedReason != "expired_unconfirmed" || *snapshot.ByteCount != 0 || len(*snapshot.Providers) != 0) {
		return nil, ErrClosedWorkIntegrity
	}
	if legacy := snapshot.Legacy; legacy != nil {
		id, err := closedWorkId(legacy.ContractId)
		closed, parseErr := time.Parse(time.RFC3339Nano, legacy.ClosedAt)
		original, originalErr := time.Parse(time.RFC3339Nano, row.ClosedAt)
		if err != nil || parseErr != nil || originalErr != nil || id != row.ContractId || legacy.Epoch != epoch || !closed.Equal(original) || !canonicalClosedWorkDigest(legacy.Manifest) || legacy.Minimum == nil || *legacy.Minimum < 0 || legacy.FinalAcceptance == nil || *legacy.FinalAcceptance || snapshot.Expiry != nil || snapshot.ExcludedReason != "legacy_usage_unavailable" {
			return nil, ErrClosedWorkIntegrity
		}
	} else if snapshot.ExcludedReason == "legacy_usage_unavailable" {
		return nil, ErrClosedWorkIntegrity
	}
	if expiry := snapshot.Expiry; expiry != nil {
		if expiry.Capacity == nil || *expiry.Capacity < 0 || expiry.Reports == nil || len(*expiry.Reports) > 2 {
			return nil, ErrClosedWorkIntegrity
		}
		minimum := *expiry.Capacity
		for party, report := range *expiry.Reports {
			if party != "source" && party != "destination" || report.ByteCount == nil || *report.ByteCount < 0 || report.Checkpoint == nil {
				return nil, ErrClosedWorkIntegrity
			}
			minimum = min(minimum, *report.ByteCount)
		}
		if len(*expiry.Reports) < 2 {
			minimum = 0
		}
		if minimum != *snapshot.ByteCount || (len(*expiry.Reports) < 2) != (snapshot.ExcludedReason == "expired_unconfirmed") {
			return nil, ErrClosedWorkIntegrity
		}
	}
	return &snapshot, nil
}

// Independently derive all usage and provider identities from the complete
// original rows. This verifies a source component, never reliability/eligibility
// or whole consumer-work truth merely because the on-chain root agrees.
func VerifyClosedWork(ctx context.Context, artifact *Artifact) (*VerifiedClosedWork, error) {
	return verifyClosedWorkWithExpectedProviders(ctx, artifact, nil)
}

// Only a complete independently admitted provider roster may preserve an idle
// zero row absent from earning snapshots. The public legacy path stays strict.
func verifyClosedWorkWithExpectedProviders(ctx context.Context, artifact *Artifact, expected []WholeWorkExpectedProvider) (*VerifiedClosedWork, error) {
	return verifyClosedWorkWithEarningSelection(ctx, artifact, expected, nil)
}

// The earning selection changes aggregate usage only. Every original row,
// participant partition and physical epoch boundary is still verified.
func verifyClosedWorkWithEarningSelection(ctx context.Context, artifact *Artifact, expected []WholeWorkExpectedProvider, selection *WholeWorkEarningSelection) (*VerifiedClosedWork, error) {
	if ctx == nil {
		return nil, errors.New("closed-work verification requires an owner context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if artifact == nil || artifact.ClosedWork == nil {
		return nil, ErrClosedWorkUnavailable
	}
	used := 0
	if len(artifact.ClosedWork.Records) > MaxClosedWorkRecords {
		return nil, ErrClosedWorkCapacity
	}
	for _, row := range artifact.ClosedWork.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(row.Original) > MaxClosedWorkRecordBytes || len(row.OriginalReports) > MaxClosedWorkRecordBytes || len(row.Original)+len(row.OriginalReports) > MaxClosedWorkOriginalBytes-used {
			return nil, ErrClosedWorkCapacity
		}
		used += len(row.Original) + len(row.OriginalReports)
	}
	if err := VerifyWithContext(ctx, artifact); err != nil {
		if errors.Is(err, ErrClosedWorkCapacity) || err == context.Canceled || err == context.DeadlineExceeded {
			return nil, err
		}
		return nil, errors.Join(ErrClosedWorkIntegrity, err)
	}
	census := artifact.ClosedWork
	if census.Schema != ClosedWorkSchema || census.DeploymentId != artifact.DeploymentID || census.ChainId != artifact.ChainID || census.GenesisHash != artifact.GenesisHash || census.Netuid != artifact.Netuid || census.Coordinator != artifact.Coordinator || census.SettlementVault != artifact.SettlementVault || census.Epoch != artifact.Epoch || census.NoId != artifact.NoID || census.PolicyHash != artifact.PolicyHash || census.Start != artifact.Start || census.End != artifact.End {
		return nil, ErrClosedWorkUnavailable
	}
	if len(census.Records) > MaxClosedWorkRecords {
		return nil, ErrClosedWorkCapacity
	}
	if census.Records == nil || census.Count != uint64(len(census.Records)) {
		return nil, ErrClosedWorkUnavailable
	}
	start, e1 := time.Parse(time.RFC3339Nano, census.WindowStart)
	end, e2 := time.Parse(time.RFC3339Nano, census.WindowEnd)
	if e1 != nil || e2 != nil || !start.Before(end) || census.WindowStart != start.UTC().Format(time.RFC3339Nano) || census.WindowEnd != end.UTC().Format(time.RFC3339Nano) || census.EarningPolicyHash != "" && !canonicalClosedWorkDigest(census.EarningPolicyHash) {
		return nil, ErrClosedWorkIntegrity
	}
	earningStart, err := verifyWholeWorkEarningSelection(census, selection)
	if err != nil {
		return nil, err
	}
	type amount struct {
		network [16]byte
		bytes   uint64
	}
	providers := map[[16]byte]amount{}
	result := &VerifiedClosedWork{Contracts: census.Count}
	originalBytes := 0
	for index, row := range census.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(row.Original) > MaxClosedWorkRecordBytes || len(row.OriginalReports) > MaxClosedWorkRecordBytes || len(row.Original)+len(row.OriginalReports) > MaxClosedWorkOriginalBytes-originalBytes {
			return nil, ErrClosedWorkCapacity
		}
		originalBytes += len(row.Original) + len(row.OriginalReports)
		closed, err := time.Parse(time.RFC3339Nano, row.ClosedAt)
		if err != nil || row.ClosedAt != closed.UTC().Format(time.RFC3339Nano) || closed.Before(start) || !closed.Before(end) || row.ContractId == ([16]byte{}) || index > 0 && bytes.Compare(census.Records[index-1].ContractId[:], row.ContractId[:]) >= 0 {
			return nil, ErrClosedWorkIntegrity
		}
		earns := earningStart.IsZero() || !closed.Before(earningStart)
		snapshot, err := decodeClosedWorkSnapshot(row, census.Epoch)
		if err != nil {
			return nil, err
		}
		if snapshot.Legacy != nil {
			result.UncreditedLegacySnapshots++
		} else if snapshot.Expiry != nil || snapshot.ExcludedReason == "expired_unconfirmed" {
			result.ExpiredSnapshots++
		} else {
			result.OrdinarySnapshots++
		}
		var total int64
		var prior [16]byte
		for providerIndex, provider := range *snapshot.Providers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			client, e1 := closedWorkId(provider.ClientId)
			network, e2 := closedWorkId(provider.NetworkId)
			if e1 != nil || e2 != nil || provider.ByteCount == nil || *provider.ByteCount < 0 || *provider.ByteCount > math.MaxInt64-total || providerIndex > 0 && bytes.Compare(prior[:], client[:]) >= 0 {
				return nil, ErrClosedWorkIntegrity
			}
			count := int64(len(*snapshot.Providers))
			want := *snapshot.ByteCount / count
			if int64(providerIndex) < *snapshot.ByteCount%count {
				want++
			}
			if *provider.ByteCount != want {
				return nil, fmt.Errorf("%w: original participant partition differs", ErrClosedWorkIntegrity)
			}
			prior = client
			total += *provider.ByteCount
			if earns {
				known, exists := providers[client]
				if exists && known.network != network || uint64(*provider.ByteCount) > math.MaxInt64-known.bytes {
					return nil, ErrClosedWorkIntegrity
				}
				known.network = network
				known.bytes += uint64(*provider.ByteCount)
				providers[client] = known
			}
		}
		if total != *snapshot.ByteCount {
			return nil, ErrClosedWorkIntegrity
		}
		if uint64(total) > math.MaxUint64-result.UsageBytes {
			return nil, ErrClosedWorkIntegrity
		}
		if earns {
			result.UsageBytes += uint64(total)
		}
	}
	if expected == nil && len(providers) != len(artifact.Providers) || result.UsageBytes != artifact.TotalUsageBytes {
		return nil, fmt.Errorf("%w: original complete usage census differs", ErrClosedWorkIntegrity)
	}
	expectedKVs := make(map[[16]byte][16]byte, len(expected))
	if len(expected) > MaxWholeWorkOwners {
		return nil, ErrClosedWorkCapacity
	}
	for _, provider := range expected {
		if _, exists := expectedKVs[provider.ClientId]; exists || provider.ClientId == ([16]byte{}) || provider.NetworkId == ([16]byte{}) {
			return nil, ErrClosedWorkIntegrity
		}
		expectedKVs[provider.ClientId] = provider.NetworkId
	}
	for _, provider := range artifact.Providers {
		got, ok := providers[provider.ClientID]
		if !ok && expected != nil && provider.UsageBytes == 0 {
			if network, admitted := expectedKVs[provider.ClientID]; admitted && network == provider.NetworkID {
				continue
			}
		}
		if !ok || got.network != provider.NetworkID || got.bytes != provider.UsageBytes {
			return nil, fmt.Errorf("%w: original provider identity or usage differs", ErrClosedWorkIntegrity)
		}
		delete(providers, provider.ClientID)
	}
	if len(providers) != 0 {
		return nil, ErrClosedWorkIntegrity
	}
	result.Providers = uint64(len(artifact.Providers))
	result.CensusHash = census.Hash()
	return result, ctx.Err()
}
