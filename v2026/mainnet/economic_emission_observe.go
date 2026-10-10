// One bounded read owns its runtime cache and complete contiguous archive
// range. Partial evidence survives errors; no cursor can skip an unread block.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// A finite retained-byte budget includes repeated JSON state and header bytes,
// separately from one bounded metadata artifact. No process-global budget.
type economicEmissionBudget struct {
	used int
}

var errEconomicEmissionEvidenceCapacity = errors.New("native incentive retained evidence exceeds 8 MiB budget")

// Accounting precedes retaining the value; an attempted block remains bounded
// by the independent single-response/event/UID limits when this budget fills.
func (self *economicEmissionBudget) retain(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > economicEmissionBytesLimit-self.used {
		return errEconomicEmissionEvidenceCapacity
	}
	self.used += len(raw)
	return nil
}

// Walk by child-authenticated parent hashes. The owned route's finalized-head
// assertion is retained as such; header linkage is not an independent GRANDPA
// justification or storage proof.
func economicEmissionAncestry(ctx context.Context, chain *rootCanonicalChain, from economicEmissionBoundary, budget *economicEmissionBudget, retain *[]rootReceiptHeader, retained ...economicEmissionBoundary) (economicEmissionBoundary, map[uint64]economicEmissionBlock, error) {
	points := []nativeFinalityPoint{{Number: from.Number, Hash: from.Hash}}
	for _, boundary := range retained {
		points = append(points, nativeFinalityPoint{Number: boundary.Number, Hash: boundary.Hash})
	}
	point, err := chain.client.readNativeFinalityCovering(ctx, points...)
	if err != nil {
		return economicEmissionBoundary{}, nil, err
	}
	hash := point.Hash
	openingHash := hash
	blocks := map[uint64]economicEmissionBlock{}
	var head economicEmissionBoundary
	var previous uint64
	for count := 0; count <= rootAncestryLimit; count++ {
		header, number, err := chain.header(ctx, hash)
		if err != nil {
			return head, blocks, err
		}
		if count == 0 {
			head = economicEmissionBoundary{Number: number, Hash: openingHash}
			if number-from.Number > rootAncestryLimit {
				return head, blocks, errors.New("native incentive finalized ancestry exceeds 4096 blocks")
			}
		} else if number+1 != previous {
			return head, blocks, errors.Join(errRpcIntegrity, errors.New("native incentive finalized ancestry height is discontinuous"))
		}
		if err := budget.retain(header); err != nil {
			return head, blocks, err
		}
		*retain = append(*retain, header)
		boundary := economicEmissionBoundary{Number: number, Hash: hash}
		// Only the policy window needs a lookup copy; all ancestry stays retained.
		if number-from.Number <= economicEmissionBlockLimit {
			blocks[number] = economicEmissionBlock{Boundary: boundary, Header: header}
		}
		if number == from.Number {
			if hash != from.Hash {
				return head, blocks, errors.Join(errRpcIntegrity, errors.New("native incentive finalized ancestry conflicts with the exact boundary"))
			}
			return head, blocks, nil
		}
		previous, hash = number, header.ParentHash
	}
	return head, blocks, errors.New("native incentive finalized ancestry exhausted its bound")
}

// Explicit storage null means the reviewed empty-vector default. Missing JSON
// result, malformed hex, missing metadata or malformed SCALE never means zero.
func readEconomicEmissionEvents(ctx context.Context, client *rpcClient, metadata *types.Metadata, block *economicEmissionBlock) ([]byte, error) {
	key, err := types.CreateStorageKey(metadata, "System", "Events")
	if err != nil {
		return nil, err
	}
	if err := client.callBoundedRead(ctx, "state_getStorage", []any{key.Hex(), block.Boundary.Hash}, &block.RawEventsStorage, true, 2*economicEmissionEventBytesLimit+maxRpcReplyBytes); err != nil {
		return nil, err
	}
	raw := []byte{0}
	if block.RawEventsStorage != nil {
		raw, err = rootReceiptHex(*block.RawEventsStorage, economicEmissionEventBytesLimit)
		if err != nil {
			return nil, err
		}
	}
	block.RawEvents, block.EventsHash = "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw)
	return raw, nil
}

// Range completeness and economic verification are independent. Even a fully
// read range leaves target/Q unset until native denominator and outcome proof.
func observeEconomicEmission(ctx context.Context, client *rpcClient, policy economicEmissionPolicy, policyHash string) (result economicEmissionObservation, resultErr error) {
	return observeEconomicEmissionPage(ctx, client, policy, policyHash, false)
}

// Only an explicitly declared continuous reader uses historical pages. The
// original finite command retains its stronger bounded head-to-window walk.
func observeEconomicEmissionPage(ctx context.Context, client *rpcClient, policy economicEmissionPolicy, policyHash string, historical bool) (economicEmissionObservation, error) {
	return observeEconomicEmissionCatalog(ctx, client, policy, policyHash, historical, nil, false)
}

// Catalog admission is exclusive to the continuous reader. Original finite
// inputs keep their single-artifact and original receipt-envelope requirements.
func observeEconomicEmissionCatalog(ctx context.Context, client *rpcClient, policy economicEmissionPolicy, policyHash string, historical bool, catalog []monitorEconomicRuntimeEntry, renewed bool) (result economicEmissionObservation, resultErr error) {
	result = economicEmissionObservation{
		Schema: economicEmissionSchema, Policy: policy, PolicyHash: policyHash, Status: "unresolved", FinalityAuthority: "owned-rpc-assertion",
		ObservedIncentiveTotalAlpha: "0", Blocks: []economicEmissionBlock{}, Ancestry: []rootReceiptHeader{}, ClosingAncestry: []rootReceiptHeader{},
		Blockers: []string{
			"runtime source-to-Wasm provenance and independent finalized storage proofs are not supplied by this observation",
			"complete pre-withholding miner tranche, zero-incentive fallback and runtime fixed-point/per-UID truncation proof remain unresolved",
			"recipient generations, provider entitlement, collateral capture/claim and actual owner recycling require separate complete native evidence",
			"cross-window exact-once accounting, activation drain, signed economic authority and live acceptance remain separate gates",
		},
	}
	if renewed {
		result.RuntimeCatalog = append([]monitorEconomicRuntimeEntry(nil), catalog...)
	}
	defer func() {
		if resultErr != nil {
			result.Issue = resultErr.Error()
			result.Complete, result.Status = false, "unresolved"
		}
		result.ContentHash = ""
		result.ContentHash = rootObjectHash(result)
	}()
	if err := policy.validate(); err != nil {
		return result, err
	}
	var producer *nativeProducerSession
	if policy.Execution != nil && policy.Execution.Producer != nil {
		var err error
		producer, err = openNativeProducerSession(ctx, policy)
		if err != nil {
			return result, err
		}
		defer func() { resultErr = errors.Join(resultErr, producer.files.close()) }()
		ctx = context.WithValue(ctx, nativeProducerSessionKey{}, producer)
	}
	if !planSha256(policyHash) {
		return result, errors.New("native incentive exact input SHA256 is missing")
	}
	if producer != nil && len(producer.authorities) > 1 {
		if !renewed || len(catalog) == 0 {
			catalog = producer.runtimeCatalog()
		}
		renewed = true
		result.RuntimeCatalog = append([]monitorEconomicRuntimeEntry(nil), catalog...)
	}
	profiles := []rootReceiptProfile{policy.Runtime}
	if renewed && len(catalog) != 0 {
		profiles = nil
		for _, entry := range catalog {
			if !slices.Contains(profiles, entry.Profile) {
				profiles = append(profiles, entry.Profile)
			}
		}
	}
	profileLimit, cacheEntries := 8, 8
	if renewed {
		profileLimit, cacheEntries = 64, 2
	}
	chain, err := newRootCanonicalChainBounded(client, identityExpectation{NativeChain: policy.Network.NativeChain, GenesisHash: policy.Network.GenesisHash, EvmChainId: policy.Network.EvmChainId}, profiles, profileLimit, cacheEntries)
	if err != nil {
		return result, err
	}
	runtimeFor := func(ctx context.Context, block, purpose string) (rootReceiptRuntime, error) {
		if renewed {
			return economicRuntimeFor(ctx, chain, block, policy, catalog, purpose)
		}
		return chain.nativeRuntimeAt(ctx, block)
	}
	readCtx, cancel := context.WithTimeout(ctx, client.retryWindow)
	defer cancel()
	if err := chain.network(readCtx); err != nil {
		return result, err
	}
	budget := economicEmissionBudget{}
	var blocks map[uint64]economicEmissionBlock
	if historical {
		result.HistoricalFinality = "owned-rpc-assertion"
		result.Finalized, result.FinalizedHeader, blocks, err = economicEmissionHistoricalPage(readCtx, chain, policy, &budget, &result.RangeAncestry)
	} else {
		result.Finalized, blocks, err = economicEmissionAncestry(readCtx, chain, policy.From, &budget, &result.Ancestry, policy.Through)
		if len(result.Ancestry) != 0 {
			header := result.Ancestry[0]
			result.FinalizedHeader = &header
		}
	}
	if err != nil {
		return result, err
	}
	through, exists := blocks[policy.Through.Number]
	if !exists || through.Boundary != policy.Through {
		return result, errors.New("native incentive end boundary is not in the observed finalized ancestry")
	}
	runtime, err := runtimeFor(readCtx, policy.From.Hash, economicRuntimeStatePurpose)
	if err != nil {
		return result, err
	}
	if _, err := economicEmissionEventProfile(runtime.metadata); !renewed && err != nil {
		return result, err
	}
	// Retain exact independently pinned bytes, not just a parsed cache object.
	if err := client.call(readCtx, "state_getMetadata", []any{policy.From.Hash}, &result.MetadataHex); err != nil {
		return result, err
	}
	metadataRaw, err := rootReceiptHex(result.MetadataHex, maxMetadataRpcReplyBytes)
	if err != nil || rootExtrinsicHash(metadataRaw) != runtime.profile.RuntimeMetadataHash {
		return result, errors.New("native incentive retained metadata differs from approved artifact")
	}
	initial, err := readEconomicEmissionState(readCtx, client, runtime.metadata, policy, policy.From)
	result.InitialState = &initial
	if err != nil {
		return result, err
	}
	if err := budget.retain(initial); err != nil {
		return result, err
	}
	previous, total := initial, new(big.Int)
	for number := policy.From.Number + 1; number <= policy.Through.Number; number++ {
		block := blocks[number]
		result.AttemptedBlock = &block
		before := previous
		block.Before = &before
		if block.Header.ParentHash != before.Boundary.Hash || before.Boundary.Number+1 != number {
			return result, errors.New("native incentive block range has an unobserved predecessor")
		}
		// Check parent execution runtime even if the current block has new code.
		runtime, err := runtimeFor(readCtx, before.Boundary.Hash, economicRuntimeEventsPurpose)
		if err != nil {
			return result, err
		}
		body, err := chain.body(readCtx, block.Boundary.Hash, block.Header)
		if err != nil {
			return result, err
		}
		block.BodyCount = len(body)
		raw, err := readEconomicEmissionEvents(readCtx, client, runtime.metadata, &block)
		if err != nil {
			return result, err
		}
		block.Events, block.ContextEvents, err = decodeEconomicEmissionEvents(runtime.metadata, raw, block.BodyCount, policy.Netuid, policy.MaximumUids, number)
		if err != nil {
			return result, err
		}
		if len(policy.FeePayers) != 0 {
			if renewed {
				if _, err := runtimeFor(readCtx, before.Boundary.Hash, economicRuntimeFeePurpose); err != nil {
					return result, err
				}
			}
			block.Fees, err = decodeEconomicNativeFees(runtime.metadata, raw, body, policy.FeePayers)
			if err != nil {
				return result, err
			}
		}
		postRuntime, err := runtimeFor(readCtx, block.Boundary.Hash, economicRuntimeStatePurpose)
		if err != nil {
			return result, err
		}
		if renewed {
			execution, post := runtime.profile, postRuntime.profile
			block.ExecutionRuntime, block.PostStateRuntime = &execution, &post
		}
		after, err := readEconomicEmissionState(readCtx, client, postRuntime.metadata, policy, block.Boundary)
		block.After = &after
		if err != nil {
			return result, err
		}
		block.Denominator, err = economicEmissionDenominatorEvidence(before, after, block.Events, block.ContextEvents)
		if err != nil {
			return result, err
		}
		block.ExecutionOutcome, err = observeNativeExecution(readCtx, client, policy, block, runtime.profile, runtime.metadata)
		if err != nil {
			return result, err
		}
		if block.ExecutionOutcome != nil {
			block.Denominator.NativeMinerAllocationAlpha = &block.ExecutionOutcome.MinerAllocation
			block.Denominator.RuntimeTruncationDustAlpha = &block.ExecutionOutcome.FixedPointDust
			block.Denominator.ZeroIncentiveFallback = block.ExecutionOutcome.RedirectedToValidators != "0"
			block.Denominator.Status = "execution-allocation-authenticated"
			block.Denominator.Blockers = []string{"full economic target tolerance includes earlier runtime/u16 quantization and signed activation policy; final normalization proof alone does not grant it"}
		}
		if err := budget.retain(block); err != nil {
			return result, err
		}
		amount, valid := new(big.Int).SetString(block.Denominator.ObservedIncentiveAlpha, 10)
		if !valid || amount.Sign() < 0 {
			return result, errors.New("native incentive aggregate amount is invalid")
		}
		total.Add(total, amount)
		result.ObservedIncentiveTotalAlpha = total.String()
		result.Blocks = append(result.Blocks, block)
		result.AttemptedBlock = nil
		previous = after
	}
	if historical {
		result.ClosingFinalized, result.ClosingFinalizedHeader, err = economicEmissionFinalizedAssertion(readCtx, chain, policy.From, policy.Through, result.Finalized)
	} else {
		result.ClosingFinalized, _, err = economicEmissionAncestry(readCtx, chain, result.Finalized, &budget, &result.ClosingAncestry, policy.From, policy.Through)
	}
	if err != nil {
		return result, err
	}
	for _, boundary := range []economicEmissionBoundary{policy.From, policy.Through, result.Finalized} {
		var canonical string
		if err := client.call(readCtx, "chain_getBlockHash", []any{boundary.Number}, &canonical); err != nil {
			return result, err
		}
		if canonical != boundary.Hash {
			return result, fmt.Errorf("%w: native incentive closing canonical conflict at block %d", errRpcIntegrity, boundary.Number)
		}
	}
	if err := chain.network(readCtx); err != nil {
		return result, err
	}
	result.Status, result.Complete = "observed-economic-outcome-unresolved", true
	if policy.Execution != nil {
		window, err := summarizeNativeExecution(result)
		if err != nil {
			return result, err
		}
		result.ExecutionWindow = window
		if producer != nil {
			value := producer.state
			result.ExecutionProducer = &value
			if len(producer.authorities) > 1 {
				result.runtimeAdmission, err = admitNativeProducerRuntimes(policy, &value, producer.authorities, func(reference planFileReference) ([]byte, error) {
					return producer.files.readReference(reference, nativeProducerCompletionMaximum(policy.Execution.FeeCensus))
				})
				if err != nil {
					return result, err
				}
			}
			result.FinalityAuthority = "independently-approved-anchor-and-verified-grandpa"
		}
		result.NativeMinerAllocationAlpha, result.ProviderEntitlementAlpha, result.OwnerRecycledAlpha = &window.MinerAllocation, &window.ProviderEntitlement, &window.OwnerRecycled
		result.IndependentStorageProof = true
		if producer == nil {
			result.FinalityAuthority = "independently-reviewed-finalized-boundaries"
		}
		result.Status = "observed-execution-amounts-target-unresolved"
		result.Blockers = []string{"execution amounts and final fixed-point casts are authenticated; complete runtime/u16 quantization tolerance and activation accounting remain separate", "vault capture, independent Claim and cross-domain conservation remain separate; native recycling grants no reserve credit"}
	}
	return result, nil
}
