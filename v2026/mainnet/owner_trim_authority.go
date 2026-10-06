// Observed runtime predicates are necessary but cannot promise future Root,
// governance or owner actions. An independent enforced window remains mandatory.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// These facts come from one exact current block, not imported proof booleans.
// A passing observation never changes the historical qualifier's false flags.
type ownerTrimCurrentWindow struct {
	Qualification            ownerTrimBoundedQualification `json:"bounded_qualification"`
	AccountNonce             uint32                        `json:"account_nonce"`
	NetworkImmunityPeriod    uint64                        `json:"network_immunity_period"`
	NetworkImmuneUntil       uint64                        `json:"network_immune_until_exclusive"`
	PublicPruningFenced      bool                          `json:"public_pruning_predicate_through_expiry"`
	OwnerProxiesAbsent       bool                          `json:"owner_proxies_absent"`
	ProxyStorageKey          string                        `json:"proxy_storage_key"`
	ProxyStorage             *string                       `json:"proxy_storage"`
	NetworkImmunityStorage   []rootStorageValue            `json:"network_immunity_storage"`
	Blockers                 []string                      `json:"observed_blockers"`
	RequiredEnforcement      []string                      `json:"required_independent_enforcement"`
	CurrentAuthorityVerified bool                          `json:"current_authority_verified"`
}

// This port must enforce its named invariants through the *original* era death,
// including governance, privileged calls, pending owner routes, root generations,
// global coldkey signing exclusivity, provenance and fee exposure. No production
// adapter is supplied: an approved source hash or signed config cannot do that.
type ownerTrimWindowEnforcer interface {
	enforceWindow(context.Context, ownerTrimExecutionConfig, ownerTrimCurrentWindow) error
}

// The production authority implements the observable predicates itself, then
// requires a separately qualified enforcement capability. Nil never means pass.
type ownerTrimCurrentAuthority struct {
	chain    *ownerTrimCanonicalChain
	baseline subnetPreview
	enforcer ownerTrimWindowEnforcer
}

// Recheck exact current facts before both signing and every numbered send.
func (self *ownerTrimCurrentAuthority) authorize(ctx context.Context, config ownerTrimExecutionConfig, evidence ownerTrimActionReconciliation) error {
	if self == nil || self.chain == nil || rootObjectHash(config) != rootObjectHash(self.chain.config) || evidence.Receipt != nil ||
		evidence.Observation.AccountNonce == nil || *evidence.Observation.AccountNonce != config.Action.Nonce ||
		evidence.Observation.FinalizedNumber >= config.Action.BirthBlock+config.Action.Period {
		return errors.New("owner trim current authority has no exact pending action and nonce")
	}
	window, err := self.chain.readCurrentWindow(ctx, config.Action, evidence.Observation)
	if err != nil {
		return err
	}
	census := window.Qualification.Census.Observation
	if !reflect.DeepEqual(self.baseline.RootRegistrations, census.RootRegistrations) {
		window.Blockers = append(window.Blockers, "OWNER_TRIM_ORIGINAL_ROOT_GENERATIONS_CHANGED")
	}
	if len(window.Blockers) != 0 {
		return errors.New("owner trim current runtime/window predicates do not admit execution")
	}
	if self.enforcer == nil {
		return errors.New("owner trim enforced owner/governance/root window, runtime provenance, global coldkey custody and fee exposure remain unresolved")
	}
	return self.enforcer.enforceWindow(ctx, config, window)
}

// Validate the complete Proxy.Proxies shape before treating null/default as no
// delegation. An unknown query layout cannot be interpreted as an empty vector.
func ownerTrimProxyEntry(metadata *types.Metadata) (types.StorageEntryMetadataV14, error) {
	var selected types.StorageEntryMetadataV14
	count := 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "Proxy" {
			continue
		}
		if !pallet.HasStorage || pallet.Storage.Prefix != "Proxy" {
			return selected, errors.New("owner trim Proxy storage prefix changed")
		}
		for _, entry := range pallet.Storage.Items {
			if entry.Name == "Proxies" {
				selected, count = entry, count+1
			}
		}
	}
	if count != 1 || !selected.Type.IsMap || !selected.Modifier.IsDefault || len(selected.Type.AsMap.Hashers) != 1 ||
		!selected.Type.AsMap.Hashers[0].IsTwox64Concat || !rootTypeMatches(metadata, selected.Type.AsMap.Key, "account", 0) || !bytes.Equal(selected.Fallback, make([]byte, 9)) {
		return selected, errors.New("owner trim Proxy.Proxies key, default or query profile changed")
	}
	value := metadata.AsMetadataV14.EfficientLookup[selected.Type.AsMap.Value.Int64()]
	if value == nil || !value.Def.IsTuple || len(value.Def.Tuple) != 2 || !rootTypeMatches(metadata, value.Def.Tuple[1], "u64", 0) {
		return selected, errors.New("owner trim Proxy.Proxies tuple changed")
	}
	sequence := metadata.AsMetadataV14.EfficientLookup[value.Def.Tuple[0].Int64()]
	// BoundedVec is a single-field wrapper in some metadata versions.
	for depth := 0; sequence != nil && sequence.Def.IsComposite && len(sequence.Def.Composite.Fields) == 1 && depth < 16; depth++ {
		sequence = metadata.AsMetadataV14.EfficientLookup[sequence.Def.Composite.Fields[0].Type.Int64()]
	}
	if sequence == nil || !sequence.Def.IsSequence {
		return selected, errors.New("owner trim Proxy.Proxies vector changed")
	}
	definition := metadata.AsMetadataV14.EfficientLookup[sequence.Def.Sequence.Type.Int64()]
	if definition == nil || !definition.Def.IsComposite || len(definition.Def.Composite.Fields) != 3 {
		return selected, errors.New("owner trim proxy definition changed")
	}
	fields := definition.Def.Composite.Fields
	proxyType := metadata.AsMetadataV14.EfficientLookup[fields[1].Type.Int64()]
	if fields[0].Name != "delegate" || fields[1].Name != "proxy_type" || fields[2].Name != "delay" ||
		!rootTypeMatches(metadata, fields[0].Type, "account", 0) || !rootTypeMatches(metadata, fields[2].Type, "u32", 0) || proxyType == nil || !proxyType.Def.IsVariant {
		return selected, errors.New("owner trim proxy delegate/type/delay encoding changed")
	}
	seen := map[uint8]bool{}
	for _, variant := range proxyType.Def.Variant.Variants {
		if len(variant.Fields) != 0 || seen[uint8(variant.Index)] {
			return selected, errors.New("owner trim proxy type variants changed")
		}
		seen[uint8(variant.Index)] = true
	}
	return selected, nil
}

// Requalification at a later head conservatively checks a full period beyond
// that head. This can refuse an otherwise safe shorter remaining window; it
// never renews the original action's birth, expiry, nonce or allowance.
func (self *ownerTrimCanonicalChain) readCurrentWindow(ctx context.Context, action ownerTrimAction, observation ownerTrimObservation) (ownerTrimCurrentWindow, error) {
	var result ownerTrimCurrentWindow
	if ctx == nil || observation.FinalizedNumber < action.BirthBlock || observation.FinalizedNumber >= action.BirthBlock+action.Period ||
		observation.AccountNonce == nil || *observation.AccountNonce != action.Nonce || observation.Issue != "" {
		return result, errors.New("owner trim current window has no exact live nonce/era observation")
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	window := ownerTrimWindow{Schema: ownerTrimWindowSchema, PolicyHash: action.PolicyHash, FinalizedHash: observation.FinalizedHash,
		FinalizedNumber: observation.FinalizedNumber, MortalPeriod: action.Period, SelectionRule: action.SelectionRule}
	qualified, err := self.client.readOwnerTrimBoundedTarget(operationCtx, self.policy, action.PolicyHash, window, rootObjectHash(window), action.MaximumUids)
	if err != nil {
		return result, err
	}
	result.Qualification, result.AccountNonce = qualified, *observation.AccountNonce
	if observation.Census == nil {
		return ownerTrimCurrentWindow{}, errors.New("owner trim current window lacks the original canonical census")
	}
	previous := observation.Census.Observation
	sealed, _ := sealSubnetPreview(previous)
	if sealed.ContentHash != observation.Census.ContentHash {
		return ownerTrimCurrentWindow{}, errors.New("owner trim prior census seal differs")
	}
	previous.Identity.ObservedAt = qualified.Census.Observation.Identity.ObservedAt
	if rootObjectHash(previous) != rootObjectHash(qualified.Census.Observation) {
		return ownerTrimCurrentWindow{}, errors.New("owner trim same-block census observations contradict")
	}
	result.Blockers = append([]string{}, qualified.QualificationBlockers...)
	result.RequiredEnforcement = []string{"OWNER_NONCE_PROXY_MULTISIG_AND_PENDING_ACTIONS_FENCED_THROUGH_ORIGINAL_EXPIRY", "GOVERNANCE_PRIVILEGED_RUNTIME_AND_ROOT_GENERATIONS_FENCED_THROUGH_ORIGINAL_EXPIRY", "INDEPENDENT_SOURCE_TO_WASM_AND_COLLATERAL_STAKE_CLAIM_HISTORY_APPROVAL", "GLOBAL_COLDKEY_CUSTODY_AND_ENFORCEABLE_FEE_EXPOSURE"}
	return self.readCurrentPredicates(operationCtx, action, observation, result)
}

// Observable nonce, proxy, call and public-pruning predicates are shared by
// both approval domains. They never assert future privileged-action enforcement.
func (self *ownerTrimCanonicalChain) readCurrentPredicates(operationCtx context.Context, action ownerTrimAction, observation ownerTrimObservation, result ownerTrimCurrentWindow) (ownerTrimCurrentWindow, error) {
	runtime, err := self.nativeRuntimeAt(operationCtx, observation.FinalizedHash)
	if err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	call, err := subnetOwnerTrimCall(runtime.metadata)
	if err != nil || action.CallIndex != [2]byte{call.PalletIndex, call.CallIndex} {
		return ownerTrimCurrentWindow{}, errors.Join(errors.New("owner trim current call index differs from authenticated metadata"), err)
	}
	if _, err := ownerTrimProxyEntry(runtime.metadata); err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	owner, _ := hex.DecodeString(action.Coldkey[2:])
	account, exists, err := self.storage(operationCtx, runtime.metadata, "System", "Account", observation.FinalizedHash, owner)
	if err != nil || exists && len(account) != 56 {
		return ownerTrimCurrentWindow{}, errors.Join(errors.New("owner trim current nonce is unavailable"), err)
	}
	nonce := uint32(0)
	if exists {
		nonce = binary.LittleEndian.Uint32(account[:4])
	}
	if nonce != action.Nonce || nonce != *observation.AccountNonce {
		return ownerTrimCurrentWindow{}, errors.New("owner trim same-block account nonce changed")
	}
	key, err := types.CreateStorageKey(runtime.metadata, "Proxy", "Proxies", owner)
	if err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	// Only an explicit null may select the already authenticated default. The
	// optional-storage reader still rejects a response with no result field.
	if err := self.client.callWithStorageAbsence(operationCtx, "state_getStorage", []any{key.Hex(), observation.FinalizedHash}, &result.ProxyStorage, true); err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	result.ProxyStorageKey = key.Hex()
	proxyData := make([]byte, 9)
	if result.ProxyStorage != nil {
		proxyData, err = rootReceiptHex(*result.ProxyStorage, 65536)
		if err != nil {
			return ownerTrimCurrentWindow{}, err
		}
	}
	result.OwnerProxiesAbsent = bytes.Equal(proxyData, make([]byte, 9))
	if !result.OwnerProxiesAbsent {
		result.Blockers = append(result.Blockers, "OWNER_TRIM_ACTIVE_OR_UNRESOLVED_PROXY_DELEGATION")
	}
	specs := []rootStorageSpec{{name: "NetworkImmunityPeriod", value: "u64"}}
	entries, err := observationStorageProfile(runtime.metadata, specs)
	if err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	reader := rootStorageReader{client: self.client, metadata: runtime.metadata, entries: entries, specs: specs, block: observation.FinalizedHash, valueKVs: map[string]rootStorageValue{}}
	value, err := reader.read(operationCtx, "NetworkImmunityPeriod")
	if err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	result.NetworkImmunityPeriod, result.NetworkImmunityStorage = binary.LittleEndian.Uint64(value.data), reader.evidence()
	result.NetworkImmuneUntil = action.SubnetRegistrationBlock + result.NetworkImmunityPeriod
	if result.NetworkImmuneUntil < action.SubnetRegistrationBlock {
		result.NetworkImmuneUntil = math.MaxUint64
	}
	result.PublicPruningFenced = action.BirthBlock+action.Period-1 < result.NetworkImmuneUntil
	if !result.PublicPruningFenced {
		result.Blockers = append(result.Blockers, "OWNER_TRIM_PUBLIC_SUBNET_PRUNING_NOT_FENCED_THROUGH_EXPIRY")
	}
	var canonical, latest string
	if err := self.client.call(operationCtx, "chain_getBlockHash", []any{observation.FinalizedNumber}, &canonical); err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	if err := self.client.call(operationCtx, "chain_getFinalizedHead", []any{}, &latest); err != nil {
		return ownerTrimCurrentWindow{}, err
	}
	if canonical != observation.FinalizedHash || latest != observation.FinalizedHash {
		return ownerTrimCurrentWindow{}, errors.New("owner trim current window changed finalized mapping or became stale")
	}
	result.Blockers = ownerTrimUniqueBlockers(result.Blockers)
	return result, operationCtx.Err()
}
