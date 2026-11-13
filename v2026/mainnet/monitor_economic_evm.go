// Each EVM economic role observes one independently selected contract. Vault
// and reserve roles have separate checkpoints, budgets and failure lifetimes.
package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const maximumMonitorEvmBlocks = 32
const maximumMonitorEvmTransactions = 1024
const maximumMonitorEvmLogs = 4096
const maximumMonitorEvmReadBytes = 8 * 1024 * 1024

// Hashes and identities come from the local review, never from discovered RPC
// code or observed event labels. EVM heights are not native heights.
type monitorEconomicEvmPolicy struct {
	archiveReferenceBytes uint64
	CaptureIdentity       bool                         `json:"original_capture_identity,omitempty"`
	Role                  string                       `json:"role"`
	Network               planNetwork                  `json:"network"`
	EvmGenesisHash        string                       `json:"evm_genesis_hash"`
	ContractKind          string                       `json:"contract_kind"`
	Address               string                       `json:"address"`
	CodeHash              string                       `json:"code_hash"`
	Netuid                uint16                       `json:"netuid"`
	PoolIds               []string                     `json:"pool_ids"`
	Coldkeys              []string                     `json:"coldkeys,omitempty"`
	FeePayers             []string                     `json:"fee_payers,omitempty"`
	From                  economicEmissionBoundary     `json:"from_exclusive"`
	BatchBlocks           uint64                       `json:"batch_blocks"`
	HistoryEntries        uint64                       `json:"history_entries"`
	StallSeconds          uint64                       `json:"stall_seconds"`
	ReadBudgetSeconds     uint64                       `json:"read_budget_seconds,omitempty"`
	Finality              string                       `json:"finality"`
	ResourceRevision      *monitorEvmResourceRevision  `json:"resource_revision,omitempty"`
	HistoryCatalog        *monitorHistoryCatalogPolicy `json:"history_catalog,omitempty"`
}

func monitorEvmAddress(value string) bool {
	return common.IsHexAddress(value) && common.HexToAddress(value) != (common.Address{}) && common.HexToAddress(value).Hex() == value
}

func (self monitorEconomicEvmPolicy) validate(expected identityExpectation) error {
	if self.CaptureIdentity && self.ContractKind != "settlement-vault" {
		return errors.New("economic capture identity requires the original settlement vault")
	}
	if err := self.validateResourceRevision(); err != nil {
		return err
	}
	if !monitorRolePattern.MatchString(self.Role) || self.Network.NativeChain != expected.NativeChain || self.Network.GenesisHash != expected.GenesisHash || self.Network.EvmChainId != expected.EvmChainId || self.Network.EvmChainId != mainnetEvmChainId || !rootCanonicalHash(self.EvmGenesisHash) || !rootCanonicalHash(self.Network.GenesisHash) {
		return errors.New("EVM economic role requires the independent native and EVM genesis identity")
	}
	if self.ContractKind != "settlement-vault" && self.ContractKind != "reserve-sink" || !monitorEvmAddress(self.Address) || !rootCanonicalHash(self.CodeHash) || self.Netuid != 25 || self.Finality != "owned-rpc-assertion" {
		return errors.New("EVM economic contract requires a reviewed code hash, ABI purpose and RPC assertion policy")
	}
	if self.From.Number == 0 || self.From.Number > math.MaxInt64 || !rootCanonicalHash(self.From.Hash) || self.BatchBlocks == 0 || self.BatchBlocks > maximumMonitorEvmBlocks || self.HistoryEntries == 0 || self.HistoryEntries > maximumMonitorEconomicEvents || self.StallSeconds < 60 || self.StallSeconds > 3600 || self.ReadBudgetSeconds != 0 && (self.ReadBudgetSeconds < 60 || self.ReadBudgetSeconds > 900) {
		return errors.New("EVM economic role requires a bounded original cursor, history and one read budget")
	}
	if len(self.PoolIds) == 0 || len(self.PoolIds) > 16 || len(self.Coldkeys) > 16 || len(self.FeePayers) > 16 || self.ContractKind == "reserve-sink" && len(self.Coldkeys) != 0 {
		return errors.New("EVM economic expected pool, credit or fee census exceeds its bound")
	}
	for _, census := range []struct {
		values []string
		kind   string
	}{{values: self.PoolIds, kind: "pool"}, {values: self.Coldkeys, kind: "coldkey"}, {values: self.FeePayers, kind: "payer"}} {
		seen := map[string]bool{}
		for _, value := range census.values {
			valid := false
			switch census.kind {
			case "pool":
				n, err := monitorEconomicInteger(value)
				valid = err == nil && n.Sign() > 0
			case "coldkey":
				valid = rootCanonicalHash(value) && value != "0x"+strings.Repeat("0", 64)
			case "payer":
				valid = monitorEvmAddress(value)
			}
			if !valid || seen[value] {
				return fmt.Errorf("EVM economic %s is malformed or repeated", census.kind)
			}
			seen[value] = true
		}
	}
	return self.HistoryCatalog.validate()
}

func monitorEconomicEvmPaths(checkpoint, metrics, role string) (string, string) {
	return strings.TrimSuffix(checkpoint, ".json") + ".evm-economics-" + role + ".json", strings.TrimSuffix(metrics, ".prom") + ".evm-economics-" + role + ".prom"
}

// Event values are canonical ABI integers/addresses/bytes, not JSON floating
// point values. The original receipt and inclusion position remain retained.
type monitorEconomicEvmEvent struct {
	CaptureIdentity  *monitorEconomicVaultCaptureIdentity `json:"original_capture_identity,omitempty"`
	Block            economicEmissionBoundary             `json:"block"`
	TransactionHash  string                               `json:"transaction_hash"`
	TransactionIndex uint64                               `json:"transaction_index"`
	LogIndex         uint64                               `json:"log_index"`
	Name             string                               `json:"name"`
	Values           map[string]string                    `json:"values"`
	ReceiptHash      string                               `json:"receipt_hash"`
}

// Fees concern independently selected senders and included transactions,
// including failed execution. The effective price is an RPC assertion; native
// debit/refund placement and denomination equivalence remain unproved.
type monitorEconomicEvmFee struct {
	Block                economicEmissionBoundary `json:"block"`
	TransactionHash      string                   `json:"transaction_hash"`
	TransactionIndex     uint64                   `json:"transaction_index"`
	Payer                string                   `json:"payer"`
	GasUsed              uint64                   `json:"gas_used"`
	EffectiveGasPriceWei string                   `json:"effective_gas_price_wei"`
	FeeWei               string                   `json:"fee_wei"`
	Success              bool                     `json:"success"`
	ReceiptHash          string                   `json:"receipt_hash"`
}

// These counters are block-pinned contract assertions. Backing is not an
// independently proven native allocation, denominator or recipient payment.
type monitorEconomicEvmSnapshot struct {
	Counters map[string]string `json:"counters"`
	Pools    map[string]string `json:"pools"`
	Credits  map[string]string `json:"credits"`
}

func (self monitorEconomicEvmSnapshot) clone() monitorEconomicEvmSnapshot {
	result := monitorEconomicEvmSnapshot{Counters: map[string]string{}, Pools: map[string]string{}, Credits: map[string]string{}}
	for k, v := range self.Counters {
		result.Counters[k] = v
	}
	for k, v := range self.Pools {
		result.Pools[k] = v
	}
	for k, v := range self.Credits {
		result.Credits[k] = v
	}
	return result
}

func (self monitorEconomicEvmSnapshot) validate(policy monitorEconomicEvmPolicy) error {
	names := []string{"principal", "liveStake"}
	if policy.ContractKind == "settlement-vault" {
		names = []string{"totalCaptured", "totalPaid", "pendingFunding", "outstandingLiability", "escrowAccounted", "liveEscrowStake"}
	}
	if len(self.Counters) != len(names) || len(self.Pools) != len(policy.PoolIds) || len(self.Credits) != len(policy.Coldkeys) {
		return errors.New("EVM economic retained counter census changed")
	}
	for _, name := range names {
		if _, err := monitorEconomicInteger(self.Counters[name]); err != nil {
			return err
		}
	}
	for k, v := range self.Pools {
		if !slices.Contains(policy.PoolIds, k) {
			return errors.New("EVM economic retained pool is not independently expected")
		}
		if _, err := monitorEconomicInteger(v); err != nil {
			return err
		}
	}
	for k, v := range self.Credits {
		if !slices.Contains(policy.Coldkeys, k) {
			return errors.New("EVM economic retained credit is not independently expected")
		}
		if _, err := monitorEconomicInteger(v); err != nil {
			return err
		}
	}
	return nil
}
