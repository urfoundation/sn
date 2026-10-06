// Historical installation admission borrows original custody without importing
// signatures, advancing a journal or granting current installation authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

const bootstrapContractReceiptSchema = "urnetwork-mainnet-bootstrap-contract-receipts-v1"

// Each seal retains the full original signed record, including counted attempts
// and its distinct native/EVM inclusion. The report does not export signatures.
type bootstrapContractReceiptAction struct {
	Id              string           `json:"id"`
	CustodyHash     string           `json:"custody_hash"`
	JournalHash     string           `json:"journal_hash"`
	TransactionHash string           `json:"transaction_hash"`
	Attempts        uint8            `json:"attempts"`
	Receipt         evmCreateReceipt `json:"receipt"`
}

// The snapshot identifies canonical ancestry, not current contract storage.
// Scan-floor admission only excludes omission of the original deployment prefix.
type bootstrapContractReceiptAdmission struct {
	Schema                       string                              `json:"schema"`
	PreparationHash              string                              `json:"preparation_hash"`
	DeclarationHash              string                              `json:"declaration_hash"`
	ContractPlanHash             string                              `json:"contract_plan_hash"`
	OriginalConfigHash           string                              `json:"original_config_hash"`
	Actions                      []bootstrapContractReceiptAction    `json:"actions"`
	Validators                   []bootstrapContractValidatorBinding `json:"validators"`
	RetainedAttempts             uint16                              `json:"retained_attempts"`
	OriginalMaximumAttempts      uint8                               `json:"original_maximum_attempts"`
	CanonicalSnapshotNativeHash  string                              `json:"canonical_snapshot_native_hash"`
	CanonicalSnapshotNativeBlock uint64                              `json:"canonical_snapshot_native_block"`
	CheckedThroughNativeHash     string                              `json:"checked_through_native_hash"`
	CheckedThroughNativeBlock    uint64                              `json:"checked_through_native_block"`
	FinalityAssumption           string                              `json:"finality_assumption"`
	EarliestOriginalEvmBlock     uint64                              `json:"earliest_original_evm_block"`
	CanonicalReceiptsVerified    bool                                `json:"canonical_receipts_verified"`
	HistoricalStateVerified      bool                                `json:"historical_state_verified"`
	DeploymentScanFloorsVerified bool                                `json:"deployment_scan_floors_verified"`
	CurrentStateVerified         bool                                `json:"current_state_verified"`
	EvidenceAnchorVerified       bool                                `json:"evidence_anchor_verified"`
	InstallationComplete         bool                                `json:"installation_complete"`
	ActivationReady              bool                                `json:"activation_ready"`
	NetworkEffects               bool                                `json:"network_effects"`
	PendingChainPhases           []string                            `json:"pending_chain_phases"`
	ContentHash                  string                              `json:"content_hash"`
}

// All markers stay held through receipt reads and the final local checkpoint.
// This owner exposes no mutation, signing or submission operation.
type bootstrapContractReceiptScope struct {
	preparation bootstrapChainPreparation
	declaration bootstrapContractRolePlan
	retained    *bootstrapChainReadinessState
	plans       []evmCreatePlan
	records     []evmActionRecord
	locks       []*bootstrapContractReadinessMarker
	chain       *evmOwnedChain
	closed      bool
}

// Admission uses the exact original accepted preparation and complete original
// action graph. Re-signing inputs cannot replace already retained custody.
func openBootstrapContractReceiptScope(ctx context.Context, path, directory, accepted string) (_ *bootstrapContractReceiptScope, resultErr error) {
	if ctx == nil {
		return nil, errors.New("contract receipt context is absent")
	}
	declaration, err := loadBootstrapContractRolePlan(ctx, path)
	if err != nil {
		return nil, err
	}
	preparation, err := loadBootstrapChainPreparation(ctx, path)
	if err != nil || accepted != declaration.PreparationHash || accepted != preparation.Plan.ContentHash || directory != preparation.Plan.Config.RunDirectory {
		return nil, errors.Join(errors.New("contract receipt original preparation or declaration differs"), err)
	}
	_, plans, err := prepareBootstrapContractReadiness(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path)
	if err != nil || len(plans) != 8 {
		return nil, errors.Join(errors.New("contract receipt admission requires all eight original projections"), err)
	}
	if err := validateBootstrapContractReadinessPaths(preparation, plans); err != nil {
		return nil, err
	}
	self := &bootstrapContractReceiptScope{preparation: preparation, declaration: declaration}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	self.retained, err = openBootstrapChainReadinessState(ctx, preparation)
	if err != nil {
		return nil, err
	}
	attempts := uint16(0)
	for i, original := range plans {
		projection := copyEvmCreatePlan(original)
		projection.Prerequisites = append([]evmActionRecord(nil), self.records...)
		marker := rootObjectHash(original.Config) + "\n"
		if i > 0 {
			marker = rootObjectHash(struct{ ConfigHash, ActionId, PredecessorHash string }{
				ConfigHash: rootObjectHash(original.Config), ActionId: original.Config.Plan.Actions[i].Id,
				PredecessorHash: rootObjectHash(self.records[i-1])}) + "\n"
		}
		journal := filepath.Join(directory, bootstrapContractStateFile(i))
		lock, err := openBootstrapContractReadinessMarker(journal, marker+bootstrapRootClaimComplete, ctx)
		if err != nil {
			return nil, err
		}
		self.locks = append(self.locks, lock)
		raw, _, err := readBootstrapRootFile(ctx, journal, 512*1024)
		var record evmActionRecord
		if err == nil {
			err = decodePlanJson(raw, &record)
		}
		if err == nil {
			err = errors.Join(record.validateForAction(original.Config, i), validateEvmCreatePrerequisite(projection, record), validateEvmCreateCompletion(projection, record))
		}
		if err != nil || record.Receipt == nil || record.Receipt.Status != 1 {
			return nil, errors.Join(errors.New("contract receipt original complete record differs"), err)
		}
		if i > 0 && (record.Receipt.NativeNumber < self.records[i-1].Receipt.NativeNumber || record.Receipt.BlockNumber < self.records[i-1].Receipt.BlockNumber) {
			return nil, errors.New("contract receipt inclusion precedes its original predecessor")
		}
		attempts += uint16(record.Attempts)
		if attempts > uint16(original.Config.Plan.MaximumAttempts) {
			return nil, errors.New("contract receipt attempts exceed the original graph allowance")
		}
		self.plans, self.records = append(self.plans, projection), append(self.records, record)
	}
	self.chain, err = newEvmOwnedChain(preparation.Contracts.Config)
	if err != nil {
		return nil, err
	}
	return self, ctx.Err()
}

// Closing releases borrowed locks and transport resources, never retained files.
func (self *bootstrapContractReceiptScope) close() error {
	if self == nil || self.closed {
		return nil
	}
	self.closed = true
	var result error
	for i := len(self.locks) - 1; i >= 0; i-- {
		result = errors.Join(result, self.locks[i].close())
	}
	if self.retained != nil {
		result = errors.Join(result, self.retained.close())
	}
	if self.chain != nil {
		self.chain.client.httpClient.CloseIdleConnections()
	}
	return result
}

// The signed EVM scan floor is inclusive and may precede deployment. Native
// heights cannot substitute for EVM event-indexing heights.
func bootstrapContractReceiptScanFloor(validators []bootstrapContractValidatorBinding, records []evmActionRecord) (uint64, error) {
	if len(validators) != 2 || len(records) != 8 {
		return 0, errors.New("contract receipt scan floor lacks its complete deployment scope")
	}
	var earliest uint64
	for _, record := range records {
		if record.Receipt == nil || record.Receipt.Status != 1 || record.Receipt.BlockNumber == 0 {
			return 0, errors.New("contract receipt scan floor lacks a successful original inclusion")
		}
		if earliest == 0 || record.Receipt.BlockNumber < earliest {
			earliest = record.Receipt.BlockNumber
		}
	}
	for _, validator := range validators {
		if validator.DeclaredDeployBlock == 0 || validator.DeclaredDeployBlock > earliest {
			return 0, errors.New("contract receipt declared EVM scan floor omits original deployment history")
		}
	}
	return earliest, nil
}

// Reread sealed source and full original records around networking. Shared
// locks exclude cooperating writers; observed external replacements still fail.
func (self *bootstrapContractReceiptScope) checkpoint(ctx context.Context) error {
	if ctx == nil || self == nil || self.closed || self.chain == nil || len(self.records) != 8 || len(self.plans) != 8 || len(self.locks) != 8 {
		return errors.New("contract receipt scope is absent or closed")
	}
	if err := self.retained.checkpoint(ctx); err != nil {
		return err
	}
	declaration, err := loadBootstrapContractRolePlan(ctx, self.preparation.Plan.ConfigPath)
	if err != nil || declaration.ContentHash != self.declaration.ContentHash {
		return errors.Join(errors.New("contract receipt signed declarations changed during admission"), err)
	}
	for i, record := range self.records {
		if err := self.locks[i].checkpoint(); err != nil {
			return err
		}
		raw, _, err := readBootstrapRootFile(ctx, filepath.Join(self.preparation.Plan.Config.RunDirectory, bootstrapContractStateFile(i)), 512*1024)
		var current evmActionRecord
		if err == nil {
			err = decodePlanJson(raw, &current)
		}
		if err != nil || rootObjectHash(current) != rootObjectHash(record) {
			return errors.Join(errors.New("contract receipt original custody changed during admission"), err)
		}
	}
	return self.retained.checkpoint(ctx)
}

// A later head need not equal the snapshot. Every exact retained inclusion and
// the snapshot itself must still belong to the canonical finalized ancestry.
func (self *bootstrapContractReceiptScope) continuity(ctx context.Context, snapshot evmActionRecord, check chainIdentity) error {
	p := self.preparation.Contracts.Config.Plan
	if err := self.chain.continuity(ctx, p, snapshot, check); err != nil {
		return err
	}
	for _, record := range self.records {
		point := evmActionRecord{ScanNumber: record.Receipt.NativeNumber, ScanHash: record.Receipt.NativeHash}
		if err := self.chain.continuity(ctx, p, point, check); err != nil {
			return err
		}
	}
	return nil
}

// Each historical reconciliation has its own original finite retry budget.
// No aggregate timeout is imposed across eight independently bounded receipts.
func (self *bootstrapContractReceiptScope) inspect(ctx context.Context) (bootstrapContractReceiptAdmission, error) {
	var result bootstrapContractReceiptAdmission
	if err := self.checkpoint(ctx); err != nil {
		return result, err
	}
	earliest, err := bootstrapContractReceiptScanFloor(self.declaration.Validators, self.records)
	if err != nil {
		return result, err
	}
	head, err := self.chain.client.readIdentity(ctx)
	if err != nil {
		return result, err
	}
	p := self.preparation.Contracts.Config.Plan
	snapshot := evmActionRecord{ScanNumber: head.FinalizedNumber, ScanHash: head.FinalizedHash}
	if err := self.chain.continuity(ctx, p, snapshot, head); err != nil {
		return result, err
	}
	for i, plan := range self.plans {
		record := self.records[i]
		if record.Receipt.NativeNumber > head.FinalizedNumber {
			return result, errors.New("contract receipt inclusion exceeds the selected finalized snapshot")
		}
		observation, err := self.chain.reconcile(ctx, plan, record)
		if err != nil || observation.Receipt == nil || *observation.Receipt != *record.Receipt || observation.Status != plan.completedStatus() {
			return result, errors.Join(errors.New("contract receipt canonical inclusion or historical postcondition differs"), err)
		}
		result.Actions = append(result.Actions, bootstrapContractReceiptAction{Id: p.Actions[i].Id, CustodyHash: rootObjectHash(record),
			JournalHash: record.ContentHash, TransactionHash: record.TransactionHash, Attempts: record.Attempts, Receipt: *record.Receipt})
		result.RetainedAttempts += uint16(record.Attempts)
	}
	check, err := self.chain.client.readIdentity(ctx)
	if err != nil {
		return result, err
	}
	if err := self.continuity(ctx, snapshot, check); err != nil {
		return result, err
	}
	if err := self.checkpoint(ctx); err != nil {
		return result, err
	}
	result.Schema, result.PreparationHash, result.DeclarationHash = bootstrapContractReceiptSchema, self.preparation.Plan.ContentHash, self.declaration.ContentHash
	result.ContractPlanHash, result.OriginalConfigHash = p.hash(), rootObjectHash(self.preparation.Contracts.Config)
	result.Validators, result.OriginalMaximumAttempts = self.declaration.Validators, p.MaximumAttempts
	result.CanonicalSnapshotNativeHash, result.CanonicalSnapshotNativeBlock = head.FinalizedHash, head.FinalizedNumber
	result.CheckedThroughNativeHash, result.CheckedThroughNativeBlock = check.FinalizedHash, check.FinalizedNumber
	result.FinalityAssumption, result.EarliestOriginalEvmBlock = "owned-rpc-assertion", earliest
	result.CanonicalReceiptsVerified, result.HistoricalStateVerified, result.DeploymentScanFloorsVerified = true, true, true
	result.PendingChainPhases = bootstrapChainPendingPhases()
	result.ContentHash = rootObjectHash(result)
	return result, nil
}

// Online means read-only use of the original approved route. Neither signature
// import nor submission flags exist, and refusal emits no partial certificate.
func runBootstrapContractReceiptCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("contract-installation-receipts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "exact private original v3 chain configuration")
	directory := flags.String("run-dir", "", "original retained custody directory")
	accepted := flags.String("accept-plan-hash", "", "original accepted preparation hash")
	online := flags.Bool("online", false, "read canonical history through the original owned route")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" || *directory == "" || !planSha256(*accepted) || !*online {
		fmt.Fprintln(stderr, "contract-installation-receipts requires --config, --run-dir, --accept-plan-hash and --online")
		return 2
	}
	scope, err := openBootstrapContractReceiptScope(ctx, *path, *directory, *accepted)
	if err != nil {
		fmt.Fprintln(stderr, "contract receipt original custody:", err)
		return 2
	}
	result, err := scope.inspect(ctx)
	if err = errors.Join(err, scope.close()); err != nil {
		fmt.Fprintln(stderr, "contract receipt admission unresolved:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "contract receipt output:", err)
		return 1
	}
	return 0
}
