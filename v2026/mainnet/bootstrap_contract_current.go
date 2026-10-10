// Current bootstrap admission checks reviewed fields at one finalized mapping.
// Owned-RPC assertions grant no complete-storage, historical or send authority.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

const bootstrapContractCurrentSchema = "urnetwork-mainnet-bootstrap-contract-current-v1"

// Each entry states exactly which projected getters and storage words matched.
// Mapping roots are individual words, never an assertion about hidden entries.
type bootstrapContractCurrentAccount struct {
	Projection  string                `json:"projection"`
	Address     common.Address        `json:"address"`
	RuntimeHash string                `json:"runtime_hash"`
	Getters     []contractGetter      `json:"getters"`
	Storage     []contractStorageWord `json:"storage"`
}

// The initial policy uses its original EVM inclusion; the clock uses the selected
// EVM snapshot. A later native head does not relabel any of these observations.
type bootstrapContractCurrentFields struct {
	Accounts         []bootstrapContractCurrentAccount `json:"accounts"`
	EvidencePointer  common.Address                    `json:"evidence_pointer"`
	EvidenceState    string                            `json:"evidence_pointer_state"`
	CurrentEpoch     uint64                            `json:"current_epoch"`
	PolicyStartBlock uint64                            `json:"policy_start_evm_block"`
}

// Matching the original five-account bootstrap profile is narrower than full
// installation. Safe authority and all mapping/history claims stay unverified.
type bootstrapContractCurrentAdmission struct {
	Schema                       string                            `json:"schema"`
	Profile                      string                            `json:"profile"`
	StateAuthority               string                            `json:"state_authority"`
	HistoricalPrefix             bootstrapContractReceiptAdmission `json:"historical_prefix"`
	Snapshot                     finalizedMappingEnvelope          `json:"snapshot"`
	Fields                       bootstrapContractCurrentFields    `json:"fields"`
	CheckedThroughNativeHash     string                            `json:"checked_through_native_hash"`
	CheckedThroughNativeBlock    uint64                            `json:"checked_through_native_block"`
	CurrentBootstrapStateMatches bool                              `json:"current_bootstrap_state_matches"`
	CompleteStorageVerified      bool                              `json:"complete_storage_verified"`
	EvidenceAnchorVerified       bool                              `json:"evidence_anchor_verified"`
	SafeAuthorityVerified        bool                              `json:"safe_authority_verified"`
	InstallationComplete         bool                              `json:"installation_complete"`
	ActivationReady              bool                              `json:"activation_ready"`
	NetworkEffects               bool                              `json:"network_effects"`
	PendingChainPhases           []string                          `json:"pending_chain_phases"`
	OwnerTrimPhase               string                            `json:"owner_trim_phase,omitempty"`
	ContentHash                  string                            `json:"content_hash"`
}

// All bootstrap projections remain exact. Only the current clock is recomputed,
// using the sole original policy and EVM height rather than native height.
func bootstrapContractCurrentGetters(plan evmCreatePlan, receipt evmCreateReceipt, evmBlock uint64) ([]contractGetter, error) {
	if receipt.BlockNumber == 0 || evmBlock < receipt.BlockNumber {
		return nil, errors.New("contract current snapshot precedes original EVM inclusion")
	}
	getters, err := plan.receiptGetters(receipt)
	if err != nil {
		return nil, err
	}
	getters = append([]contractGetter(nil), getters...)
	if plan.ActionIndex == 4 {
		if plan.ProxyConstructor == nil || plan.ProxyConstructor.ApprovedPolicy.EpochBlocks == 0 {
			return nil, errors.New("contract current clock lacks its original policy")
		}
		epoch := (evmBlock - receipt.BlockNumber) / plan.ProxyConstructor.ApprovedPolicy.EpochBlocks
		selector := "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackCurrentEpoch())
		found := false
		for i := range getters {
			if getters[i].Data == selector {
				getters[i].Expected = common.BigToHash(new(big.Int).SetUint64(epoch)).Hex()
				found = true
			}
		}
		if !found {
			return nil, errors.New("contract current projection omits its clock")
		}
	}
	return getters, nil
}

// One selector covers the five distinct accounts using their last original
// projections. Individual bounded reads inherit cancellation, not one aggregate
// budget. Only an unset or exact expected evidence pointer is admitted; neither
// proves the anchor transaction, governance history or Safe storage authority.
func (self *bootstrapContractReceiptScope) currentFields(ctx context.Context, mapping finalizedMapping) (bootstrapContractCurrentFields, error) {
	var result bootstrapContractCurrentFields
	if ctx == nil || self == nil || self.closed || self.chain == nil || len(self.plans) != 8 || len(self.records) != 8 || !rootCanonicalHash(mapping.EvmHeader.Hash) {
		return result, errors.New("contract current field scope is unavailable")
	}
	block := map[string]any{"blockHash": mapping.EvmHeader.Hash, "requireCanonical": true}
	evidenceSelector := "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackValidatorEvidence())
	result.EvidenceState = "unset"
	for _, index := range []int{2, 4, 5, 6, 7} {
		plan, record := self.plans[index], self.records[index]
		if record.Receipt == nil {
			return result, errors.New("contract current field scope lacks original receipt")
		}
		getters, err := bootstrapContractCurrentGetters(plan, *record.Receipt, mapping.EvmHeader.Number)
		if err != nil {
			return result, err
		}
		var code string
		if err := self.chain.read(ctx, "eth_getCode", []any{plan.Address.Hex(), block}, &code); err != nil {
			return result, err
		}
		if code != "0x"+hex.EncodeToString(plan.Runtime) {
			return result, fmt.Errorf("contract current %s runtime differs", plan.Config.Plan.Actions[index].Id)
		}
		for i, getter := range getters {
			var output string
			if err := self.chain.read(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": getter.Data}, block}, &output); err != nil {
				return result, err
			}
			if index == 4 && getter.Data == evidenceSelector && output == common.BytesToHash(self.plans[7].Address[:]).Hex() {
				getter.Expected = output
				result.EvidencePointer, result.EvidenceState = self.plans[7].Address, "expected-journal-observed"
			}
			if output != getter.Expected {
				return result, fmt.Errorf("contract current %s getter differs", plan.Config.Plan.Actions[index].Id)
			}
			getters[i] = getter
		}
		for _, expected := range plan.Storage {
			var output string
			if err := self.chain.read(ctx, "eth_getStorageAt", []any{plan.Address.Hex(), expected.Slot, block}, &output); err != nil {
				return result, err
			}
			if output != expected.Expected {
				return result, fmt.Errorf("contract current %s storage differs", plan.Config.Plan.Actions[index].Id)
			}
		}
		if index == 4 {
			result.PolicyStartBlock = record.Receipt.BlockNumber
			result.CurrentEpoch = (mapping.EvmHeader.Number - record.Receipt.BlockNumber) / plan.ProxyConstructor.ApprovedPolicy.EpochBlocks
		}
		result.Accounts = append(result.Accounts, bootstrapContractCurrentAccount{Projection: plan.Config.Plan.Actions[index].Id,
			Address: plan.Address, RuntimeHash: crypto.Keccak256Hash(plan.Runtime).Hex(), Getters: getters,
			Storage: append([]contractStorageWord(nil), plan.Storage...)})
	}
	return result, ctx.Err()
}

// Original receipts retain their own authenticated historical blocks. Current
// fields use one new mapping, corroborated after all reads; later finality may
// advance while the original inclusions and selected snapshot remain canonical.
func (self *bootstrapContractReceiptScope) inspectCurrent(ctx context.Context) (bootstrapContractCurrentAdmission, error) {
	var result bootstrapContractCurrentAdmission
	history, err := self.inspect(ctx)
	if err != nil {
		return result, err
	}
	head, err := self.chain.client.readIdentity(ctx)
	if err != nil {
		return result, err
	}
	point := evmActionRecord{ScanNumber: head.FinalizedNumber, ScanHash: head.FinalizedHash}
	if err := self.continuity(ctx, point, head); err != nil {
		return result, err
	}
	mapping, err := self.chain.client.readFinalizedMappingAtIdentity(ctx, head)
	if err != nil {
		return result, err
	}
	fields, err := self.currentFields(ctx, mapping)
	if err != nil {
		return result, err
	}
	check, err := self.chain.client.readFinalizedMappingAtIdentity(ctx, head)
	if err != nil || rootObjectHash(check) != rootObjectHash(mapping) {
		return result, errors.Join(errors.New("contract current selected mapping changed during observation"), err)
	}
	latest, err := self.chain.client.readIdentity(ctx)
	if err != nil {
		return result, err
	}
	if err := self.continuity(ctx, point, latest); err != nil {
		return result, err
	}
	if err := self.checkpoint(ctx); err != nil {
		return result, err
	}
	snapshot, err := sealFinalizedMapping(mapping)
	if err != nil {
		return result, err
	}
	result = bootstrapContractCurrentAdmission{Schema: bootstrapContractCurrentSchema, Profile: "original-five-account-bootstrap-fields-v1",
		StateAuthority: "owned-rpc-assertion", HistoricalPrefix: history, Snapshot: snapshot, Fields: fields,
		CheckedThroughNativeHash: latest.FinalizedHash, CheckedThroughNativeBlock: latest.FinalizedNumber,
		CurrentBootstrapStateMatches: true, PendingChainPhases: bootstrapChainPendingPhasesForSchema(self.preparation.Plan.Config.Schema),
		OwnerTrimPhase: self.preparation.Plan.OwnerTrimPhase}
	result.ContentHash = rootObjectHash(result)
	return result, nil
}

// The current read-only port requires exact original custody and explicit online
// observation. No credential, signature, pending-state or send option exists.
func runBootstrapContractCurrentCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("contract-current-state", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "exact private original v3 chain configuration")
	directory := flags.String("run-dir", "", "original retained custody directory")
	accepted := flags.String("accept-plan-hash", "", "original accepted preparation hash")
	online := flags.Bool("online", false, "read original history and current bootstrap fields through the owned route")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" || *directory == "" || !planSha256(*accepted) || !*online {
		fmt.Fprintln(stderr, "contract-current-state requires --config, --run-dir, --accept-plan-hash and --online")
		return 2
	}
	scope, err := openBootstrapContractReceiptScope(ctx, *path, *directory, *accepted)
	if err != nil {
		fmt.Fprintln(stderr, "contract current original custody:", err)
		return 2
	}
	result, err := scope.inspectCurrent(ctx)
	if err = errors.Join(err, scope.close()); err != nil {
		fmt.Fprintln(stderr, "contract current observation unresolved:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "contract current output:", err)
		return 1
	}
	return 0
}
