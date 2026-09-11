package main

// This file batches the exact historical block and contract reads needed to
// authenticate generation-1 fleet state after an approved generation-2
// refresh. Every action retains its own checkpoint and observed-state proof;
// historical eth_call proofs may be reused only after their exact local
// evidence and fresh canonical/finalized checkpoints have been checked.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/stabi"
)

const carriedFleetHistoryBatchTimeout = 10 * time.Minute

type carriedActionAudit struct {
	action Action
	entry  JournalEntry
	record *ActionPostcondition
}

type historicalFleetGenerationOneCall struct {
	action      Action
	entry       JournalEntry
	record      *ActionPostcondition
	address     common.Address
	data        []byte
	expectation any
	observe     func([]byte) (map[string]any, error)
}

const historicalFleetGenerationOneCacheKind = "fleet-generation-one-eth-call-v1"

// Bind each observer's exact request as well as every input used to interpret
// the output. The durable cache also binds the authorized endpoint identities.
type historicalFleetGenerationOneCacheInput struct {
	Action      Action                           `json:"action"`
	Entry       JournalEntry                     `json:"entry"`
	Record      *ActionPostcondition             `json:"record"`
	Expectation any                              `json:"expectation"`
	Observers   []historicalFleetObserverRequest `json:"observers"`
}

type historicalFleetObserverRequest struct {
	Observer   string            `json:"observer"`
	Checkpoint ChainHead         `json:"checkpoint"`
	Request    coordinatorCallAt `json:"request"`
}

// Read numbered EVM-RPC block identities in bounded batches. The embedded
// height remains checked for every element; positional batch success alone is
// never treated as proof of the requested checkpoint.
func batchEVMBlocksByNumber(ctx context.Context, client *ethclient.Client, numbers []uint64) ([]ChainHead, error) {
	if client == nil || len(numbers) == 0 {
		return nil, errors.New("EVM block batch is unavailable")
	}
	heads := make([]ChainHead, len(numbers))
	for start := 0; start < len(numbers); start += maximumEVMRPCBatchCalls {
		end := min(start+maximumEVMRPCBatchCalls, len(numbers))
		blocks := make([]*evmRPCBlock, end-start)
		batch := make([]rpc.BatchElem, end-start)
		for index := start; index < end; index++ {
			if numbers[index] == 0 {
				return nil, fmt.Errorf("EVM block batch element %d has no block", index)
			}
			batch[index-start] = rpc.BatchElem{
				Method: "eth_getBlockByNumber",
				Args:   []any{hexutil.EncodeUint64(numbers[index]), false},
				Result: &blocks[index-start],
			}
		}
		if err := client.Client().BatchCallContext(ctx, batch); err != nil {
			return nil, err
		}
		for index := range batch {
			if batch[index].Error != nil {
				return nil, fmt.Errorf("EVM block batch element %d: %w", start+index, batch[index].Error)
			}
			number := numbers[start+index]
			head, err := decodeEVMRPCBlock(blocks[index], new(big.Int).SetUint64(number))
			if err != nil {
				return nil, fmt.Errorf("EVM block batch element %d: %w", start+index, err)
			}
			heads[start+index] = head
		}
	}
	return heads, nil
}

// Prepare one historical mirror/binding decoder from the same immutable local
// evidence used by the ordinary postcondition functions. Atomic aliases need
// only a canonical block proof because their observed state is derived from
// the authenticated install receipt without an additional contract read.
func (self *Executor) prepareHistoricalFleetGenerationOneCall(ctx context.Context, audit carriedActionAudit, coordinates fleetGenerationOneActionCoordinates) (historicalFleetGenerationOneCall, error) {
	call := historicalFleetGenerationOneCall{action: audit.action, entry: audit.entry, record: audit.record}
	if audit.record == nil || audit.record.EVMFinalized.Number == 0 {
		return call, errors.New("historical fleet checkpoint is unavailable")
	}
	if coordinates.Install {
		return call, errors.New("fleet install batches are not individual historical calls")
	}
	if coordinates.Alias {
		aliasReceiptKind, err := classifyFleetInstallAliasReceipt(audit.record)
		if err != nil {
			return call, err
		}
		if aliasReceiptKind == fleetInstallAliasReceiptDerived {
			observed, err := self.actionPostState(ctx, audit.action, audit.record.EVMFinalized)
			if err != nil {
				return call, err
			}
			call.observe = func([]byte) (map[string]any, error) { return observed, nil }
			return call, nil
		}
		if aliasReceiptKind != fleetInstallAliasReceiptHistoricalRead {
			return call, errors.New("fleet install alias receipt format is unsupported")
		}
	}
	if err := self.ensurePayloads(ctx); err != nil {
		return call, err
	}
	call.address = self.payloads.Manifest.CoordinatorProxy
	contract := stabi.NewSTCoordinator()
	if coordinates.Member == 0 {
		manifest, _, commitmentHash, err := fleetManifest(self.cfg, self.stateDir, self.roles, coordinates.Fleet)
		if err != nil {
			return call, err
		}
		evidence, err := loadFleetCommitmentEvidence(self.stateDir, coordinates.Fleet)
		if err != nil {
			return call, err
		}
		finalizedBlockHash, err := decodeHex32("fleet commitment finalized block", evidence.FinalizedBlockHash)
		if err != nil {
			return call, err
		}
		call.data = contract.PackMirroredCommitments(manifest.Hotkey)
		call.expectation = struct {
			CommitmentHash [32]byte                 `json:"commitment_hash"`
			Evidence       *FleetCommitmentEvidence `json:"evidence"`
		}{CommitmentHash: commitmentHash, Evidence: evidence}
		call.observe = func(output []byte) (map[string]any, error) {
			got, err := contract.UnpackMirroredCommitments(output)
			if err != nil || !fleetMirrorMatches(got, commitmentHash, evidence.CommitmentBlock, finalizedBlockHash) {
				return nil, stateMismatchError(err, "fleet %d historical mirror mismatch", coordinates.Fleet)
			}
			return map[string]any{
				"kind": audit.action.Kind, "target": audit.action.Target, "fleet": coordinates.Fleet,
				"commitment_hash": "0x" + hex.EncodeToString(commitmentHash[:]), "finalized_block": got.FinalizedBlock,
			}, nil
		}
		return call, nil
	}
	var evidence FleetBindingEvidence
	path := filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", coordinates.Fleet, coordinates.Member))
	if err := readJSONFile(path, &evidence); err != nil {
		return call, err
	}
	clientIDBytes, ok := evidenceFixedHex(evidence.ClientID, 16)
	if !ok {
		return call, errors.New("fleet binding evidence has invalid client id")
	}
	var clientID [16]byte
	copy(clientID[:], clientIDBytes)
	call.data = contract.PackBindingAt(clientID, new(big.Int).SetUint64(evidence.ValidFromEpoch))
	call.expectation = evidence
	call.observe = func(output []byte) (map[string]any, error) {
		got, err := contract.UnpackBindingAt(output)
		if err != nil || !got.Active || got.Record.Uid != evidence.UID || got.Record.Generation != evidence.Generation {
			return nil, stateMismatchError(err, "fleet %d member %d historical binding mismatch", coordinates.Fleet, coordinates.Member)
		}
		return map[string]any{
			"kind": audit.action.Kind, "target": audit.action.Target, "fleet": coordinates.Fleet, "member": coordinates.Member,
			"client_id": evidence.ClientID, "uid": evidence.UID,
		}, nil
	}
	return call, nil
}

// Verify one observer's fresh finalized head and every distinct historical
// canonical block, including calls whose immutable replay proof was cached.
func verifyHistoricalFleetGenerationOneCheckpoints(ctx context.Context, client *ethclient.Client, calls []historicalFleetGenerationOneCall, independent bool) error {
	if client == nil || len(calls) == 0 {
		return errors.New("historical fleet EVM client is unavailable")
	}
	for _, call := range calls {
		if call.record == nil || call.observe == nil || (len(call.data) != 0 && call.address == (common.Address{})) {
			return fmt.Errorf("action %s has incomplete historical fleet call evidence", call.action.ID)
		}
	}
	finalized, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return fmt.Errorf("finalized EVM head: %w", err)
	}
	checkpointByNumber := map[uint64]ChainHead{}
	checkpointNumbers := make([]uint64, 0, len(calls))
	for _, call := range calls {
		checkpoint := call.record.EVMFinalized
		if independent {
			checkpoint = call.record.IndependentEVMFinalized
		}
		if checkpoint.Number == 0 || checkpoint.Hash == "" {
			return fmt.Errorf("action %s has an incomplete historical EVM checkpoint", call.action.ID)
		}
		if prior, ok := checkpointByNumber[checkpoint.Number]; ok {
			if !strings.EqualFold(prior.Hash, checkpoint.Hash) {
				return fmt.Errorf("action %s conflicts with historical EVM hash %s at block %d", call.action.ID, prior.Hash, checkpoint.Number)
			}
			continue
		}
		checkpointByNumber[checkpoint.Number] = checkpoint
		checkpointNumbers = append(checkpointNumbers, checkpoint.Number)
	}
	heads, err := batchEVMBlocksByNumber(ctx, client, checkpointNumbers)
	if err != nil {
		return fmt.Errorf("historical EVM checkpoints: %w", err)
	}
	for index, number := range checkpointNumbers {
		checkpoint := checkpointByNumber[number]
		ready, visibilityErr := checkpointVisibility(checkpoint, finalized, heads[index].Hash)
		if visibilityErr != nil {
			return fmt.Errorf("historical EVM block %d: %w", number, visibilityErr)
		}
		if !ready {
			return fmt.Errorf("historical EVM block %d is not finalized (head %d)", number, finalized.Number)
		}
	}
	return nil
}

func historicalFleetGenerationOneRequest(call historicalFleetGenerationOneCall, independent bool) coordinatorCallAt {
	block := call.record.EVMFinalized.Number
	if independent {
		block = call.record.IndependentEVMFinalized.Number
	}
	return coordinatorCallAt{Address: call.address, Data: call.data, Block: block}
}

func verifyHistoricalFleetGenerationOneObservation(call historicalFleetGenerationOneCall, output []byte, independent bool) error {
	observed, err := call.observe(output)
	if err != nil {
		return fmt.Errorf("action %s historical fleet state: %w", call.action.ID, err)
	}
	recorded := call.record.Observed
	if independent {
		recorded = call.record.IndependentObserved
	}
	if err := observedPostconditionMatches(recorded, observed); err != nil {
		return fmt.Errorf("action %s historical fleet state: %w", call.action.ID, err)
	}
	return nil
}

// The ordinary observer verifier remains a complete, uncached proof. The
// carried-action path below may skip only its immutable eth_call component.
func verifyHistoricalFleetGenerationOneClient(ctx context.Context, client *ethclient.Client, calls []historicalFleetGenerationOneCall, independent bool) error {
	if err := verifyHistoricalFleetGenerationOneCheckpoints(ctx, client, calls, independent); err != nil {
		return err
	}
	requests := make([]coordinatorCallAt, 0, len(calls))
	requestCalls := make([]historicalFleetGenerationOneCall, 0, len(calls))
	for _, call := range calls {
		if len(call.data) == 0 {
			if err := verifyHistoricalFleetGenerationOneObservation(call, nil, independent); err != nil {
				return err
			}
			continue
		}
		requests = append(requests, historicalFleetGenerationOneRequest(call, independent))
		requestCalls = append(requestCalls, call)
	}
	if len(requests) == 0 {
		return nil
	}
	outputs, err := rawCoordinatorBatchCallsAt(ctx, client, requests)
	if err != nil {
		return fmt.Errorf("historical fleet contract calls: %w", err)
	}
	for index, output := range outputs {
		if err := verifyHistoricalFleetGenerationOneObservation(requestCalls[index], output, independent); err != nil {
			return err
		}
	}
	return nil
}

type historicalFleetGenerationOneResult struct {
	output hexutil.Bytes
	err    error
}

// Retain individual RPC errors so an earlier action whose two observations
// succeeded can be saved even when a later element in the same batch fails.
func readHistoricalFleetGenerationOneBatch(ctx context.Context, client *ethclient.Client, calls []historicalFleetGenerationOneCall, independent bool) ([]historicalFleetGenerationOneResult, error) {
	if client == nil || len(calls) == 0 || len(calls) > maximumEVMRPCBatchCalls {
		return nil, errors.New("historical fleet contract batch is unavailable or unbounded")
	}
	results := make([]historicalFleetGenerationOneResult, len(calls))
	batch := make([]rpc.BatchElem, len(calls))
	for index, call := range calls {
		request := historicalFleetGenerationOneRequest(call, independent)
		if request.Block == 0 || request.Address == (common.Address{}) || len(request.Data) == 0 {
			return nil, fmt.Errorf("action %s has an incomplete historical fleet request", call.action.ID)
		}
		batch[index] = rpc.BatchElem{
			Method: "eth_call",
			Args: []any{
				map[string]any{"to": request.Address, "data": hexutil.Bytes(request.Data)},
				hexutil.EncodeUint64(request.Block),
			},
			Result: &results[index].output,
		}
	}
	if err := client.Client().BatchCallContext(ctx, batch); err != nil {
		return nil, err
	}
	for index := range batch {
		results[index].err = batch[index].Error
	}
	return results, nil
}

// Only the carried generation-1 path calls this after authenticating every
// source/install/refresh relationship and rebuilding its decoder inputs.
// All checkpoint checks and derived-alias observations remain fresh. Misses
// retain bounded transport batching, and each success is durable immediately
// after both required observers have passed that action's exact comparison.
func (self *Executor) verifyHistoricalFleetGenerationOneCalls(ctx context.Context, calls []historicalFleetGenerationOneCall) (map[string]bool, error) {
	verifiedKeys := map[string]bool{}
	if len(calls) == 0 {
		return verifiedKeys, nil
	}
	if self.deployer == nil || self.deployer.client == nil {
		return nil, errors.New("operational historical fleet EVM client is unavailable")
	}
	if err := verifyHistoricalFleetGenerationOneCheckpoints(ctx, self.deployer.client, calls, false); err != nil {
		return nil, fmt.Errorf("operational historical fleet audit: %w", err)
	}
	independent := independentRPCRequired(self.cfg)
	if independent {
		if err := verifyHistoricalFleetGenerationOneCheckpoints(ctx, self.independentEVM, calls, true); err != nil {
			return nil, fmt.Errorf("independent historical fleet audit: %w", err)
		}
	} else {
		for _, call := range calls {
			if call.record.IndependentEVMFinalized.Number != call.record.EVMFinalized.Number || !strings.EqualFold(call.record.IndependentEVMFinalized.Hash, call.record.EVMFinalized.Hash) {
				return nil, fmt.Errorf("action %s shared-provider historical EVM checkpoints differ", call.action.ID)
			}
			if err := observedPostconditionMatches(call.record.Observed, call.record.IndependentObserved); err != nil {
				return nil, fmt.Errorf("action %s shared-provider historical EVM clone: %w", call.action.ID, err)
			}
		}
	}
	misses := make([]historicalFleetGenerationOneCall, 0, len(calls))
	entries := make([]*historicalAuditCacheEntry, 0, len(calls))
	for _, call := range calls {
		if len(call.data) == 0 {
			if err := verifyHistoricalFleetGenerationOneObservation(call, nil, false); err != nil {
				return nil, fmt.Errorf("operational historical fleet audit: %w", err)
			}
			if independent {
				if err := verifyHistoricalFleetGenerationOneObservation(call, nil, true); err != nil {
					return nil, fmt.Errorf("independent historical fleet audit: %w", err)
				}
			}
			verifiedKeys[carriedVerificationKey(call.entry)] = true
			continue
		}
		if call.expectation == nil {
			return nil, fmt.Errorf("action %s has no historical fleet decoder evidence", call.action.ID)
		}
		observers := []historicalFleetObserverRequest{{
			Observer: "operational", Checkpoint: call.record.EVMFinalized,
			Request: historicalFleetGenerationOneRequest(call, false),
		}}
		if independent {
			observers = append(observers, historicalFleetObserverRequest{
				Observer: "independent", Checkpoint: call.record.IndependentEVMFinalized,
				Request: historicalFleetGenerationOneRequest(call, true),
			})
		}
		entry, hit := self.lookupHistoricalAuditCache(ctx, historicalFleetGenerationOneCacheKind, historicalFleetGenerationOneCacheInput{
			Action: call.action, Entry: call.entry, Record: call.record, Expectation: call.expectation, Observers: observers,
		})
		if hit {
			verifiedKeys[carriedVerificationKey(call.entry)] = true
			continue
		}
		misses = append(misses, call)
		entries = append(entries, entry)
	}
	for start := 0; start < len(misses); start += maximumEVMRPCBatchCalls {
		end := min(start+maximumEVMRPCBatchCalls, len(misses))
		batch := misses[start:end]
		operational, err := readHistoricalFleetGenerationOneBatch(ctx, self.deployer.client, batch, false)
		if err != nil {
			return nil, fmt.Errorf("operational historical fleet contract calls: %w", err)
		}
		var comparison []historicalFleetGenerationOneResult
		if independent {
			comparison, err = readHistoricalFleetGenerationOneBatch(ctx, self.independentEVM, batch, true)
			if err != nil {
				return nil, fmt.Errorf("independent historical fleet contract calls: %w", err)
			}
		}
		for index, call := range batch {
			if operational[index].err != nil {
				return nil, fmt.Errorf("action %s operational historical fleet call: %w", call.action.ID, operational[index].err)
			}
			if err := verifyHistoricalFleetGenerationOneObservation(call, operational[index].output, false); err != nil {
				return nil, fmt.Errorf("operational historical fleet audit: %w", err)
			}
			if independent {
				if comparison[index].err != nil {
					return nil, fmt.Errorf("action %s independent historical fleet call: %w", call.action.ID, comparison[index].err)
				}
				if err := verifyHistoricalFleetGenerationOneObservation(call, comparison[index].output, true); err != nil {
					return nil, fmt.Errorf("independent historical fleet audit: %w", err)
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			entries[start+index].saveSuccess(ctx)
			verifiedKeys[carriedVerificationKey(call.entry)] = true
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return verifiedKeys, nil
}

// Authenticate every superseded per-fleet proof before the ordinary carried
// audit workers begin. The returned keys are installed only after all reads
// and both configured observers succeed, so a partial batch cannot suppress a
// later verifier.
func (self *Executor) verifyCarriedFleetGenerationOneHistory(ctx context.Context, audits []carriedActionAudit) (map[string]bool, error) {
	verifiedKeys := map[string]bool{}
	calls := make([]historicalFleetGenerationOneCall, 0)
	batchCtx, cancel := context.WithTimeout(ctx, carriedFleetHistoryBatchTimeout)
	defer cancel()
	for _, audit := range audits {
		coordinates, applicable, err := fleetGenerationOneCoordinates(self.cfg, audit.action)
		if err != nil {
			return nil, fmt.Errorf("action %s generation-1 coordinates: %w", audit.action.ID, err)
		}
		if !applicable || coordinates.Install {
			continue
		}
		superseded, err := self.fleetGenerationOneActionSuperseded(audit.action, audit.entry, audit.record)
		if err != nil {
			return nil, fmt.Errorf("action %s generation-1 successor: %w", audit.action.ID, err)
		}
		if !superseded {
			continue
		}
		hash, err := canonicalHashHex(audit.record)
		if err != nil || hash != audit.entry.PostconditionHash {
			return nil, stateMismatchError(err, "action %s historical fleet receipt hash %s differs from verified %s", audit.action.ID, hash, audit.entry.PostconditionHash)
		}
		call, err := self.prepareHistoricalFleetGenerationOneCall(batchCtx, audit, coordinates)
		if err != nil {
			return nil, fmt.Errorf("action %s historical fleet preparation: %w", audit.action.ID, err)
		}
		calls = append(calls, call)
	}
	if len(calls) == 0 {
		return verifiedKeys, nil
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: batched historical fleet audit 0/%d\n", len(calls))
	verifiedKeys, err := self.verifyHistoricalFleetGenerationOneCalls(batchCtx, calls)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: batched historical fleet audit %d/%d\n", len(calls), len(calls))
	return verifiedKeys, nil
}
