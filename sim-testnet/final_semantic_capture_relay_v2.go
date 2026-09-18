//go:build linux || darwin

// Companion and dynamic relay evidence have their own exact raw census. The
// legacy six-contract snapshot must not silently stand in for these sources.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Reads immutable original requests/results first. Canonical winner readback
// then reuses the real transaction verifier, including third-party-first wins.
func captureFinalValidatorRelayV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, publications []validatorpkg.ReleaseEvidenceV2CapturedPublication, retain func(context.Context, validatorpkg.ReleaseEvidenceV2CaptureSource, []byte) error) error {
	if ctx == nil || cfg == nil || retain == nil || len(publications) == 0 {
		return errors.New("compact relay capture source census is absent")
	}
	plan, err := loadPersistedPlan(cfg, stateRoot)
	if err != nil || plan.ValidatorEvidence == nil {
		return errors.Join(errors.New("compact relay capture approved companion is missing"), err)
	}
	journal, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(stateRoot, "journal.jsonl"), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	entries, err := decodeFinalSemanticJournalBytes(journal)
	if err != nil {
		return err
	}
	if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-journal", Name: "journal.jsonl"}, journal); err != nil {
		return err
	}
	type retainedResult struct {
		Schema              string                                    `json:"schema"`
		PlanHash            string                                    `json:"plan_hash"`
		Action              Action                                    `json:"action"`
		SignedTransaction   []byte                                    `json:"winner_signed_transaction"`
		Receipt             *types.Receipt                            `json:"winner_receipt"`
		Publication         validatorpkg.ValidatorEvidencePublication `json:"publication"`
		OwnReceipt          *types.Receipt                            `json:"own_receipt,omitempty"`
		LostPublicationRace bool                                      `json:"lost_publication_race"`
	}
	type item struct {
		expected validatorpkg.ValidatorEvidenceTransactionV2Expected
		result   retainedResult
		slot     [32]byte
	}
	var selected []item
	seen := map[[32]byte]bool{}
	for _, publication := range publications {
		slot, err := publication.Evidence.Header.SlotKey()
		if err != nil {
			return err
		}
		if seen[slot] {
			continue
		}
		seen[slot] = true
		if uint64(len(seen)) > cfg.Config.ValidatorEvidenceRelay.MaxSlots {
			return errors.New("compact relay capture exceeds the approved finite slot reserve")
		}
		requestPath := filepath.Join(stateRoot, "evidence-relay", fmt.Sprintf("%x.json", slot))
		raw, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, requestPath, evidenceRelayActionBytes)
		if err != nil {
			return err
		}
		if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-request", Name: filepath.Base(requestPath)}, raw); err != nil {
			return err
		}
		action, err := validateEvidenceRelayRequest(plan, entries, raw)
		if err != nil {
			return err
		}
		var request evidenceRelayRequestRecord
		if err := decodeStrictJSONBytes(raw, &request); err != nil {
			return err
		}
		expected := request.Evidence
		if action.ID != fmt.Sprintf("%s%x", evidenceRelayActionPrefix, slot) || expected.Activation != publication.Activation || !reflect.DeepEqual(expected.Evidence, publication.Evidence) || expected.Window.Epoch != publication.Window.Epoch || expected.Window.Subject != publication.Window.Subject || expected.Window.StartBlock != publication.Window.StartBlock || expected.Window.EndBlock != publication.Window.EndBlock {
			return errors.New("compact relay request differs from its independently retained public source")
		}
		resultPath := filepath.Join(stateRoot, "evidence-relay", request.Action.ID+".receipt.json")
		raw, err = validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, resultPath, 4*1024*1024)
		if err != nil {
			return err
		}
		if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-result", Name: filepath.Base(resultPath)}, raw); err != nil {
			return err
		}
		var result retainedResult
		if err := decodeStrictJSONBytes(raw, &result); err != nil {
			return err
		}
		if result.Schema != "urnetwork-sim-evidence-relay-result-v2" || result.PlanHash != plan.PlanHash || !reflect.DeepEqual(result.Action, request.Action) || result.Receipt == nil || len(result.SignedTransaction) == 0 || result.LostPublicationRace != (result.OwnReceipt != nil) {
			return errors.New("compact relay result is incomplete or misrouted")
		}
		original, broadcast, _, err := evidenceRelayOriginalBroadcast(entries, cfg.Config.Deployment.DeploymentID, plan.PlanHash, request.Action)
		if err != nil {
			return err
		}
		if broadcast {
			name := "transactions/" + stringsTrim0x(original.TransactionHash) + ".rlp"
			raw, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(stateRoot, filepath.FromSlash(name)), 64*1024)
			if err != nil {
				return err
			}
			if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-original-transaction", Name: name}, raw); err != nil {
				return err
			}
		}
		selected = append(selected, item{expected: expected, result: result, slot: slot})
	}
	chain, err := validatorpkg.DialReleaseChainContext(ctx, []string{cfg.OperationalEVM}, plan.ValidatorEvidence.Coordinator)
	if err != nil {
		return err
	}
	defer chain.Close()
	for _, value := range selected {
		winner, err := chain.FindValidatorEvidenceSlotWinnerV2Context(ctx, value.expected)
		if err != nil {
			return err
		}
		if winner == nil || !bytes.Equal(winner.SignedTransaction, value.result.SignedTransaction) || winner.Receipt == nil || !finalJSONEqual(winner.Receipt, value.result.Receipt) || !finalJSONEqual(winner.Publication, value.result.Publication) {
			return errors.New("compact original relay result differs from the actual canonical winning transaction/publication")
		}
		raw, err := json.Marshal(winner)
		if err != nil {
			return err
		}
		if err := retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-winner", Name: fmt.Sprintf("%x.json", value.slot)}, raw); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Captures actual companion creation, anchor and activation transactions plus
// finalized companion views and logs. The approved journal selects every fixed
// action; missing receipts never become placeholder observations.
func captureFinalCompanionInputsV2(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, terminal *ScenarioObservation) ([]FinalArtifactLocator, error) {
	plan, err := loadPersistedPlan(cfg, stateRoot)
	if err != nil || plan.ValidatorEvidence == nil || terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil {
		return nil, errors.Join(errors.New("compact companion capture authority is incomplete"), err)
	}
	journal, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(stateRoot, "journal.jsonl"), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	entries, err := decodeFinalSemanticJournalBytes(journal)
	if err != nil {
		return nil, err
	}
	names := []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID}
	for validatorId := 1; validatorId <= cfg.Config.Topology.Validators; validatorId++ {
		for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
			names = append(names, runtimeEvidenceActivationActionId(validatorId, noId))
		}
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	var files []FinalCollectedFileBundleEntry
	add := func(name string, raw []byte) error {
		if len(raw) == 0 || len(raw) > finalCollectedBundleMaximumRawBytes {
			return errors.New("compact companion source exceeds the unchanged bundle bound")
		}
		files = append(files, FinalCollectedFileBundleEntry{Path: name, ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw)), Data: raw})
		return nil
	}
	if err := add("journal.jsonl", journal); err != nil {
		return nil, err
	}
	fromBlock := uint64(0)
	for _, name := range names {
		action, err := exactPlanActionByID(plan, name)
		if err != nil {
			return nil, err
		}
		var selected *JournalEntry
		for index := range entries {
			entry := &entries[index]
			if entry.PlanHash == plan.PlanHash && entry.ActionID == name && entry.Stage == StageFinalized {
				if !actionAcceptsIntent(action, entry.IntentHash) || entry.BlockNumber == 0 || selected != nil && (selected.TransactionHash != entry.TransactionHash || selected.BlockHash != entry.BlockHash) {
					return nil, errors.New("compact companion fixed action has conflicting finality")
				}
				selected = entry
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("compact companion action %s lacks actual finalized journal evidence", name)
		}
		transaction, pending, err := client.TransactionByHash(ctx, common.HexToHash(selected.TransactionHash))
		if err != nil || transaction == nil || pending || !strings.EqualFold(transaction.Hash().Hex(), selected.TransactionHash) {
			return nil, errors.Join(errors.New("compact companion signed transaction is absent"), err)
		}
		receipt, err := client.TransactionReceipt(ctx, transaction.Hash())
		if err != nil || receipt == nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Uint64() != selected.BlockNumber || !strings.EqualFold(receipt.BlockHash.Hex(), selected.BlockHash) || receipt.TxHash != transaction.Hash() || receipt.Status != types.ReceiptStatusSuccessful {
			return nil, errors.Join(errors.New("compact companion actual receipt differs from finalized journal"), err)
		}
		head, err := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, receipt.BlockNumber)
		if err != nil || head.Number != selected.BlockNumber || !strings.EqualFold(head.Hash, selected.BlockHash) {
			return nil, errors.Join(errors.New("compact companion receipt block is not canonical"), err)
		}
		signed, err := transaction.MarshalBinary()
		if err != nil {
			return nil, err
		}
		if err := add("transactions/"+name+".rlp", signed); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return nil, err
		}
		if err := add("receipts/"+name+".json", raw); err != nil {
			return nil, err
		}
		if name == validatorEvidenceDeployActionID {
			if receipt.ContractAddress != plan.ValidatorEvidence.Address || transaction.To() != nil {
				return nil, errors.New("compact companion creation receipt identifies another contract")
			}
			fromBlock = selected.BlockNumber
		}
	}
	head := terminal.Status.Contracts.FinalizedHead
	if fromBlock == 0 || head.Number < fromBlock {
		return nil, errors.New("compact companion has no finalized capture interval")
	}
	logs, transactions, err := captureFinalEVMLogs(ctx, cfg, fromBlock, []common.Address{plan.ValidatorEvidence.Address}, head)
	if err != nil {
		return nil, err
	}
	logBytes, err := json.Marshal(logs)
	if err != nil {
		return nil, err
	}
	if err := add("logs.json", logBytes); err != nil {
		return nil, err
	}
	txBytes, err := json.Marshal(transactions)
	if err != nil {
		return nil, err
	}
	if err := add("log-transactions.json", txBytes); err != nil {
		return nil, err
	}
	selector := map[string]any{"blockHash": common.HexToHash(head.Hash), "requireCanonical": true}
	var code hexutil.Bytes
	if err := client.Client().CallContext(ctx, &code, "eth_getCode", plan.ValidatorEvidence.Address, selector); err != nil {
		return nil, err
	}
	if crypto.Keccak256Hash(code) != plan.ValidatorEvidence.RuntimeCodeHash {
		return nil, errors.New("compact companion runtime differs from approved executable")
	}
	if err := add("runtime.bin", code); err != nil {
		return nil, err
	}
	views := validatorEvidenceDeploymentGetters(*plan.ValidatorEvidence)
	for index, view := range views {
		var raw hexutil.Bytes
		if err := client.Client().CallContext(ctx, &raw, "eth_call", map[string]any{"to": view.address, "data": hexutil.Bytes(view.data)}, selector); err != nil {
			return nil, err
		}
		if !bytes.Equal(raw, view.expected) {
			return nil, errors.New("compact companion getter differs from the approved fixed domain")
		}
		if err := add(fmt.Sprintf("views/getter-%03d.bin", index), raw); err != nil {
			return nil, err
		}
	}
	var anchor hexutil.Bytes
	if err := client.Client().CallContext(ctx, &anchor, "eth_call", map[string]any{"to": plan.ValidatorEvidence.Coordinator, "data": hexutil.Bytes(stabi.NewSTCoordinator().PackValidatorEvidence())}, selector); err != nil {
		return nil, err
	}
	if !bytes.Equal(anchor, abiWordAddress(plan.ValidatorEvidence.Address)) {
		return nil, errors.New("compact companion is not the actual coordinator anchor")
	}
	if err := add("views/coordinator-anchor.bin", anchor); err != nil {
		return nil, err
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateRoot)
	if err != nil {
		return nil, err
	}
	chain, err := validatorpkg.DialReleaseChainContext(ctx, []string{cfg.OperationalEVM}, plan.ValidatorEvidence.Coordinator)
	if err != nil {
		return nil, err
	}
	defer chain.Close()
	headHash, err := decodeHex32("compact companion terminal hash", head.Hash)
	if err != nil {
		return nil, err
	}
	for _, configured := range resolved.Config.ValidatorEvidenceV2 {
		for _, operator := range configured.Evidence.Operators {
			encoded, err := validatorpkg.ReadReleaseEvidenceV2File(ctx, operator.Activation, uint64(protocol.ValidatorEvidenceActivationPayloadSize))
			if err != nil {
				return nil, err
			}
			activation, err := protocol.DecodeValidatorEvidenceActivationPayload(encoded)
			if err != nil {
				return nil, err
			}
			digest, err := activation.Digest()
			if err != nil {
				return nil, err
			}
			var raw hexutil.Bytes
			if err := client.Client().CallContext(ctx, &raw, "eth_call", map[string]any{"to": plan.ValidatorEvidence.Address, "data": hexutil.Bytes(stabi.NewSTValidatorEvidence().PackActivation(digest))}, selector); err != nil {
				return nil, err
			}
			if err := add(fmt.Sprintf("views/validator-%d-no-%d-activation.bin", configured.ValidatorID, operator.NoID), raw); err != nil {
				return nil, err
			}
			publication, err := chain.ValidatorEvidenceActivationAtHashContext(ctx, plan.ValidatorEvidence.Address, [32]byte(plan.ValidatorEvidence.RuntimeCodeHash), activation, head.Number, headHash)
			if err != nil {
				return nil, err
			}
			verified, err := json.Marshal(publication)
			if err != nil {
				return nil, err
			}
			if err := add(fmt.Sprintf("views/validator-%d-no-%d-activation.json", configured.ValidatorID, operator.NoID), verified); err != nil {
				return nil, err
			}
		}
	}
	headBytes, err := json.Marshal(struct {
		Schema    string                      `json:"schema"`
		PlanHash  string                      `json:"plan_hash"`
		Companion ValidatorEvidenceDeployment `json:"companion"`
		FromBlock uint64                      `json:"from_block"`
		Head      ChainHead                   `json:"head"`
	}{Schema: "urnetwork-sim-companion-raw-capture-v2", PlanHash: plan.PlanHash, Companion: *plan.ValidatorEvidence, FromBlock: fromBlock, Head: head})
	if err != nil {
		return nil, err
	}
	if err := add("capture.json", headBytes); err != nil {
		return nil, err
	}
	// Prove the terminal selector has not moved after every actual view.
	recheck, err := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, new(big.Int).SetUint64(head.Number))
	if err != nil || recheck != head {
		return nil, errors.Join(errors.New("compact companion terminal identity changed"), err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return persistFinalCollectedBundleChunks(runRoot, "validator-evidence-companion", files)
}
