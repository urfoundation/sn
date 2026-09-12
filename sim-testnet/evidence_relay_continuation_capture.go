//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	nativeTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/stabi"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

func validateEvidenceRelayContinuationCapacity(cfg *ResolvedConfig, bounds validatorcomponent.ReleaseEvidenceV2Bounds, span uint64, observed validatorcomponent.StoppedAttemptLedgerCapacity) error {
	if cfg == nil || cfg.Public == nil || cfg.Policy == nil || span == 0 {
		return errors.New("relay continuation capacity has no finite source workload")
	}
	seconds, ok := checkedMul(span, cfg.Public.Chain.ExpectedBlockSeconds)
	if !ok {
		return errors.New("relay continuation source seconds overflow")
	}
	minutes, err := evidenceRelayContinuationCeil(seconds, 60)
	if err != nil {
		return err
	}
	minutes, ok = checkedAdd(minutes, 1+uint64(validatorProcessRestartLimit))
	if !ok {
		return errors.New("relay continuation source minute count overflows")
	}
	trails, ok := checkedMul(minutes, uint64(cfg.Policy.Verify.HardSeedPerMinutePerSource))
	if !ok {
		return errors.New("relay continuation source trail count overflows")
	}
	tail, ok := checkedMul(1+uint64(validatorProcessRestartLimit), uint64(cfg.Policy.Verify.HardActiveTrailsPerSource))
	if !ok {
		return errors.New("relay continuation active trail count overflows")
	}
	trails, ok = checkedAdd(trails, tail)
	if !ok {
		return errors.New("relay continuation trail tail overflows")
	}
	records, ok := checkedMul(trails, uint64(cfg.Policy.Verify.TrailDepth))
	if !ok {
		return errors.New("relay continuation record forecast overflows")
	}
	raw, ok := checkedMul(records, bounds.Disk.MaxRecordBytes)
	if !ok {
		return errors.New("relay continuation raw record forecast overflows")
	}
	within := func(used, next, limit uint64) bool { total, ok := checkedAdd(used, next); return ok && total <= limit }
	if !within(observed.Head.TrailCount, trails, bounds.Disk.MaxTrailCount) || !within(observed.Head.LastSequence, records, bounds.Disk.MaxRecordCount) || !within(observed.Head.RecordBytes, raw, bounds.Disk.MaxRawRecordBytes) || observed.StorageBytes > bounds.Disk.MaxStorageBytes || observed.StorageFiles > bounds.Disk.MaxStorageFiles {
		return errors.New("relay continuation exceeds unchanged source lifetime or storage bounds")
	}
	return nil
}

// Enumerate all retained signatures, including original queues and earlier
// approved recovery/renewal inputs. Nonce coverage is a complete custody check,
// not an inference from an empty evidence-relay directory.
func readEvidenceRelayContinuationTransactions(cfg *ResolvedConfig, stateDir string, plan *SetupPlan) ([]string, error) {
	inputs, err := readFleetRenewalQueueTransactions(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	for _, renewal := range plan.FleetRenewals {
		inputs = append(inputs, renewal.TransactionEvidence...)
	}
	files, err := os.ReadDir(filepath.Join(stateDir, "transactions"))
	if err != nil {
		return nil, err
	}
	if len(files) > 20000 {
		return nil, errors.New("relay continuation signed transaction census exceeds its existing bound")
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".rlp") {
			continue
		}
		if !validCanonicalHashHex("0x" + strings.TrimSuffix(file.Name(), ".rlp")) {
			return nil, errors.New("relay continuation transaction filename is not canonical")
		}
		raw, err := readValidatorEvidenceHistoricalFile(stateDir, "transactions/"+file.Name(), 64*1024)
		if err != nil {
			return nil, err
		}
		var transaction ethTypes.Transaction
		if err := transaction.UnmarshalBinary(raw); err != nil || transaction.Hash().Hex() != "0x"+strings.TrimSuffix(file.Name(), ".rlp") {
			return nil, errors.Join(errors.New("relay continuation retained signed bytes differ from their transaction name"), err)
		}
		inputs = append(inputs, "0x"+hex.EncodeToString(raw))
	}
	return canonicalFleetRenewalTransactions(inputs)
}

func readEvidenceRelayContinuationDebits(ctx context.Context, stateDir string, plan *SetupPlan, entries []JournalEntry) ([]EvidenceRelayContinuationDebit, []validatorcomponent.ValidatorEvidenceTransactionV2Expected, error) {
	seen := map[string]bool{}
	owners := map[string]*SetupPlan{}
	var debits []EvidenceRelayContinuationDebit
	var retained []validatorcomponent.ValidatorEvidenceTransactionV2Expected
	for _, entry := range entries {
		if !strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) || seen[entry.ActionID] {
			continue
		}
		if entry.DeploymentID != plan.DeploymentID || !plan.allowedPlanHashes()[entry.PlanHash] || len(seen) >= int(evidenceRelayContinuationSlots/2) {
			return nil, nil, errors.New("relay continuation has an unowned or excessive original debit")
		}
		owner, record, raw, err := readOwnedEvidenceRelayRequest(ctx, stateDir, plan, entries, entry.ActionID, owners)
		if err != nil {
			return nil, nil, err
		}
		if owner.EvidenceRelayContinuation != nil {
			return nil, nil, errors.New("relay continuation cannot replace a prior continuation")
		}
		debits = append(debits, EvidenceRelayContinuationDebit{PlanHash: owner.PlanHash, ActionID: entry.ActionID, RequestSHA256: bytesSHA256(raw), AllowanceWei: record.Action.Spend.EVMGasWei})
		retained = append(retained, record.Evidence)
		seen[entry.ActionID] = true
	}
	files, err := os.ReadDir(filepath.Join(stateDir, "evidence-relay"))
	if errors.Is(err, os.ErrNotExist) && len(seen) == 0 {
		return debits, retained, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if len(files) > 2*int(evidenceRelayContinuationSlots) {
		return nil, nil, errors.New("relay continuation original request directory exceeds its finite census")
	}
	for _, file := range files {
		id := evidenceRelayActionPrefix + strings.TrimSuffix(file.Name(), ".json")
		if strings.HasSuffix(file.Name(), ".receipt.json") {
			id = strings.TrimSuffix(file.Name(), ".receipt.json")
		}
		info, err := file.Info()
		if err != nil {
			return nil, nil, err
		}
		if !info.Mode().IsRegular() || !seen[id] {
			return nil, nil, errors.New("relay continuation encountered an unjournaled original request or receipt")
		}
	}
	sort.Slice(debits, func(i, j int) bool { return debits[i].ActionID < debits[j].ActionID })
	return debits, retained, nil
}

func (self *evidenceRelayRuntime) readContinuationPublicCensus(ctx context.Context, block uint64, hash [32]byte) ([]validatorcomponent.ValidatorEvidenceTransactionV2Expected, error) {
	var result []validatorcomponent.ValidatorEvidenceTransactionV2Expected
	for index := range self.sources {
		source := &self.sources[index]
		closed, err := validatorcomponent.DiscoverValidatorEvidencePublicationV2Manifests(ctx, source.stateDir, source.bounds)
		if err != nil {
			return nil, err
		}
		for index := range closed {
			requests, err := self.readClosedPublication(ctx, source, &closed[index], block, hash)
			if err != nil {
				return nil, err
			}
			result = append(result, requests...)
		}
		audits, err := validatorcomponent.DiscoverValidatorEvidenceDepositAuditV2Manifests(ctx, source.stateDir, source.bounds)
		if err != nil {
			return nil, err
		}
		for index := range audits {
			requests, err := self.readAuditPublication(ctx, source, &audits[index], block, hash)
			if err != nil {
				return nil, err
			}
			result = append(result, requests...)
		}
		if len(result) > int(evidenceRelayContinuationSlots) {
			return nil, errors.New("relay continuation public census exceeds its original aggregate monetary reserve")
		}
	}
	return result, nil
}

func canonicalEvidenceRelayContinuationRequests(requests []validatorcomponent.ValidatorEvidenceTransactionV2Expected) ([]validatorcomponent.ValidatorEvidenceTransactionV2Expected, error) {
	bySlot := map[string]validatorcomponent.ValidatorEvidenceTransactionV2Expected{}
	var keys []string
	for _, request := range requests {
		request.SignedTransaction = nil
		request.Relayer = common.Address{}
		request.MaxGas = 0
		request.MaxFeePerGas = 0
		slot, err := request.Evidence.Header.SlotKey()
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%x", slot)
		if previous, found := bySlot[key]; found {
			if !reflect.DeepEqual(previous, request) {
				return nil, errors.New("relay continuation public/request census changes an immutable signed slot")
			}
			continue
		}
		keys = append(keys, key)
		bySlot[key] = request
	}
	if len(keys) > int(evidenceRelayContinuationSlots) {
		return nil, errors.New("relay continuation unique source census exceeds its bound")
	}
	sort.Strings(keys)
	result := make([]validatorcomponent.ValidatorEvidenceTransactionV2Expected, 0, len(keys))
	for _, key := range keys {
		result = append(result, bySlot[key])
	}
	return result, nil
}

func captureEvidenceRelayContinuation(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, endBlock uint64) (_ *SetupPlan, resultErr error) {
	return captureEvidenceRelayContinuationAt(ctx, cfg, stateDir, base, endBlock, nil)
}

func captureEvidenceRelayContinuationAt(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, endBlock uint64, pin *EvidenceRelayContinuation) (result *SetupPlan, resultErr error) {
	if ctx == nil || cfg == nil || base == nil || base.EvidenceRelayContinuation != nil || provisionalResumeEnabled(cfg) || endBlock == 0 {
		return nil, errors.New("relay continuation requires one explicit strict end and original source approval")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	resolved, roles, renderBase, activationPlan, prepared, completed, err := strictHistoryAdoptionInputs(ctx, cfg, stateDir, base)
	if err != nil {
		return nil, err
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil || len(entries) == 0 {
		return nil, errors.Join(errors.New("relay continuation requires the original deployment journal"), err)
	}
	journal := &Journal{entries: entries}
	executor, err := NewExecutor(ctx, cfg, stateDir, base, journal, roles)
	if err != nil {
		return nil, err
	}
	defer executor.Close()
	runtime, err := openEvidenceRelayRuntime(ctx, cfg, executor, "release-1.0", false, func(error) {})
	if err != nil {
		return nil, err
	}
	defer runtime.cancel()
	defer runtime.chain.Close()
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		return nil, err
	}
	runtime.work = work
	block, hash, err := runtime.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	if endBlock <= block {
		return nil, errors.New("relay continuation fixed end already passed")
	}
	nativeHash, nativeBlock, err := executor.substrate.finalizedHeadContext(ctx)
	if err != nil {
		return nil, err
	}
	if pin != nil {
		if pin.SourcePlanHash != base.PlanHash || pin.EndBlock != endBlock || pin.EVMHead.Number > block || pin.NativeHead.Number > nativeBlock {
			return nil, errors.New("relay continuation imported snapshot is not finalized in its original source")
		}
		canonical, err := runtime.chain.BlockHashContext(ctx, pin.EVMHead.Number)
		if err != nil || fmt.Sprintf("0x%x", canonical) != pin.EVMHead.Hash {
			return nil, errors.Join(errors.New("relay continuation approved EVM snapshot is no longer canonical"), err)
		}
		block, hash = pin.EVMHead.Number, canonical
		nativeBlock, nativeHash = pin.NativeHead.Number, nativeTypes.Hash(common.HexToHash(pin.NativeHead.Hash))
	}
	anchor := runtime.sources[0].activations[0]
	freshNative := anchor
	freshNative.NativeBlock = nativeBlock
	freshNative.NativeHash = [32]byte(nativeHash)
	nativeEpoch, err := runtime.readHorizonNative(ctx, freshNative, false)
	if err != nil {
		return nil, err
	}
	oracle, err := readFleetRefreshOracleStateAt(ctx, executor.owner, base.Deployment.CoordinatorProxy, stabi.NewSTCoordinator(), block)
	if err != nil {
		return nil, err
	}
	reserve, err := exactPlanActionByID(base, evidenceRelayReserveId)
	if err != nil {
		return nil, err
	}
	c := EvidenceRelayContinuation{Schema: evidenceRelayContinuationSchema, SourcePlanHash: base.PlanHash, ConfigHash: cfg.ConfigHash, ActivationPlanHash: activationPlan, PreparedSHA256: prepared, CompletedSHA256: completed, JournalHash: entries[len(entries)-1].EntryHash, OriginalReserve: reserve, EVMHead: ChainHead{Number: block, Hash: fmt.Sprintf("0x%x", hash)}, NativeHead: ChainHead{Number: nativeBlock, Hash: nativeHash.Hex()}, SettlementEpoch: oracle.CurrentEpoch, NativeEpoch: nativeEpoch, EndBlock: endBlock}
	c.RequiredWorkBlocks, err = work.remaining("release-1.0", false)
	if err != nil {
		return nil, err
	}
	closed, err := evidenceRelayContinuationCeil(endBlock-block, work.settlementCadence)
	if err != nil {
		return nil, err
	}
	native, err := evidenceRelayContinuationCeil(endBlock-block, work.nativeCadence)
	if err != nil {
		return nil, err
	}
	var ok bool
	c.EndSettlementEpoch, ok = checkedAdd(c.SettlementEpoch, closed)
	if !ok {
		return nil, errors.New("relay continuation settlement end overflows")
	}
	c.EndNativeEpoch, ok = checkedAdd(c.NativeEpoch, native)
	if !ok {
		return nil, errors.New("relay continuation native end overflows")
	}
	if err := c.validateClocks(work); err != nil {
		return nil, err
	}
	for index, configured := range resolved.Config.ValidatorEvidenceV2 {
		id := int(configured.ValidatorID)
		configBytes, err := marshalRuntimeValidatorConfig(resolved, stateDir, roles, renderBase, id)
		if err != nil {
			return nil, err
		}
		configPath := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", id), "validator.yml")
		if nativeEpoch == ^uint64(0) {
			return nil, errors.New("relay continuation native snapshot cannot name a future strict bridge")
		}
		raw, err := validatorcomponent.CaptureReleaseHistoryAdoptionV2(ctx, configPath, configBytes, base.PlanHash, activationPlan, nativeEpoch+1)
		if err != nil {
			return nil, err
		}
		var request validatorcomponent.ReleaseHistoryAdoptionV2
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		runtime.sources[index].stateDir = request.CoordinatorStateDir
		for member, operator := range configured.Evidence.Operators {
			activation := runtime.sources[index].activations[member]
			raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.Context, configured.Evidence.Bounds.MaxControlBytes)
			if err != nil {
				return nil, err
			}
			var contextValue validatorcomponent.ReleaseEvidenceV2ActivationContext
			if err := decodeStrictJSONBytes(raw, &contextValue); err != nil {
				return nil, err
			}
			canonical, err := contextValue.CanonicalJSON(configured.Evidence.Bounds.MaxControlBytes)
			if err != nil || !bytes.Equal(raw, canonical) || contextValue.Activation != activation {
				return nil, errors.Join(errors.New("relay continuation original context differs from activation"), err)
			}
			vpk, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.VPKSignature, 64)
			if err != nil {
				return nil, err
			}
			hotkey, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.HotkeySignature, 64)
			if err != nil {
				return nil, err
			}
			authority := validatorcomponent.ReleaseActivationV2Authority{Expected: activation, Journal: base.ValidatorEvidence.Address, RuntimeHash: [32]byte(base.ValidatorEvidence.RuntimeCodeHash), ValidatorUID: contextValue.ValidatorUID, NativeRuntime: executor.runtimeEvidenceNativeIdentityV2()}
			if _, err := runtime.chain.AuthenticateReleaseActivationV2Context(ctx, executor.substrate.chain, authority, activation, vpk, hotkey, block, hash); err != nil {
				return nil, err
			}
			operatorDir := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", id), "state", "operators", fmt.Sprintf("no-%d", operator.NoID))
			capacity, err := validatorcomponent.ReadStoppedAttemptLedgerCapacity(ctx, operatorDir, contextValue.InitialCut.Identity, strings.ToLower(base.Deployment.CoordinatorProxy.Hex()), ed25519.PublicKey(activation.VPK[:]), configured.Evidence.Bounds.Disk)
			if err != nil {
				return nil, err
			}
			if err := validateEvidenceRelayContinuationCapacity(cfg, configured.Evidence.Bounds, endBlock-block, capacity); err != nil {
				return nil, err
			}
			c.Sources = append(c.Sources, EvidenceRelayContinuationSource{ValidatorID: configured.ValidatorID, NoID: operator.NoID, CoordinatorStateDir: request.CoordinatorStateDir, IntentPrefixSHA256: request.IntentPrefixSHA256, IntentPrefixCount: request.IntentPrefixCount, LastNativeEpoch: request.LastNativeEpoch, LastArtifactHash: request.LastArtifactHash, Activation: activation, Capacity: capacity})
		}
	}
	c.Debits, c.Retained, err = readEvidenceRelayContinuationDebits(ctx, stateDir, base, entries)
	if err != nil {
		return nil, err
	}
	pending, err := runtime.readContinuationPublicCensus(ctx, block, hash)
	if err != nil {
		return nil, err
	}
	c.Retained, err = canonicalEvidenceRelayContinuationRequests(append(c.Retained, pending...))
	if err != nil {
		return nil, err
	}
	c.NewSlots, c.HistoricalLiabilityWei, err = evidenceRelayContinuationNewSlots(reserve.Spend.EVMGasWei, c.Debits)
	if err != nil {
		return nil, err
	}
	transactions, err := readEvidenceRelayContinuationTransactions(cfg, stateDir, base)
	if err != nil {
		return nil, err
	}
	exposure, err := fleetRenewalCampaignExposure(stateDir, base, entries, transactions)
	if err != nil {
		return nil, err
	}
	c.Nonces, err = observeFleetRenewalNonces(ctx, executor.keeper, roles, block)
	if err != nil {
		return nil, err
	}
	if err := validateFleetRenewalNonceCoverage(roles, exposure, c.Nonces); err != nil {
		return nil, err
	}
	for _, point := range c.Nonces {
		if point.Finalized != point.Latest || point.Finalized != point.Pending {
			return nil, fmt.Errorf("relay continuation role %s still has unfinalized nonce liability", point.Role)
		}
		if err := validateFleetRenewalUnusedSignerNonce(exposure, point.Address, point.Pending); err != nil {
			return nil, err
		}
		if executor.independentEVM == nil {
			return nil, errors.New("relay continuation nonce census requires the independent reader")
		}
		independent, err := executor.independentEVM.NonceAt(ctx, point.Address, new(big.Int).SetUint64(block))
		if err != nil || independent != point.Finalized {
			return nil, errors.Join(errors.New("relay continuation independent finalized nonce differs"), err)
		}
	}
	admitted := map[string]bool{}
	for _, entry := range entries {
		if entry.TransactionHash == "" || !base.allowedPlanHashes()[entry.PlanHash] {
			continue
		}
		if strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) {
			admitted[entry.TransactionHash] = true
			continue
		}
		for _, source := range c.Sources {
			if entry.ActionID == runtimeEvidenceActivationActionId(int(source.ValidatorID), int(source.NoID)) && entry.PlanHash == c.ActivationPlanHash {
				admitted[entry.TransactionHash] = true
			}
		}
	}
	for hash, transaction := range exposure.Transactions {
		if transaction.To() != nil && *transaction.To() == base.ValidatorEvidence.Address && !admitted[hash.Hex()] {
			return nil, errors.New("relay continuation found a signed companion transaction outside original relay admission")
		}
	}
	encoded, err := json.Marshal(transactions)
	if err != nil {
		return nil, err
	}
	c.TransactionsSHA256 = bytesSHA256(encoded)
	latest, err := readJournalEntries(stateDir)
	if err != nil || !reflect.DeepEqual(entries, latest) {
		return nil, errors.Join(errors.New("relay continuation deployment journal changed during capture"), err)
	}
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return nil, err
	}
	// Capture duration consumes the chosen runway; it does not shift the end.
	current, _, err := runtime.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	if current > endBlock || c.RequiredWorkBlocks > endBlock-current {
		return nil, errors.New("relay continuation capture exhausted its fixed remaining-work runway")
	}
	return appendEvidenceRelayContinuationPlan(base, c)
}
