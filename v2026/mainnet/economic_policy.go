// The owner-recycle precondition authenticates one finalized runtime and mode.
// It grants no signing authority and cannot establish final Yuma allocations.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

const recyclePolicySchema = "urnetwork-mainnet-owner-recycle-policy-v1"
const recycleModeSchema = "urnetwork-mainnet-owner-recycle-mode-v1"
const recycleStorageProfile = "subtensor-recycle-storage-v1"

// Operator-reviewed pins are supplied independently of the observed endpoint.
// The source assertion must already have a reviewed source-to-Wasm binding;
// reading matching hashes does not create that build provenance.
type recyclePolicy struct {
	Schema              string                      `json:"schema"`
	NativeChain         string                      `json:"native_chain"`
	GenesisHash         string                      `json:"genesis_hash"`
	EvmChainId          uint64                      `json:"evm_chain_id"`
	Netuid              uint16                      `json:"netuid"`
	StorageProfile      string                      `json:"storage_profile"`
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
}

// Rejects incomplete authority before a network read. Runtime numbers confer
// no authority; the caller must review the exact source, code and metadata.
func (self recyclePolicy) validate() error {
	if self.Schema != recyclePolicySchema || self.Netuid != 25 || self.EvmChainId != mainnetEvmChainId || strings.TrimSpace(self.NativeChain) == "" {
		return errors.New("owner-recycle policy requires its exact schema, netuid 25, native chain and EVM chain ID 964")
	}
	for _, digest := range []string{self.GenesisHash, self.RuntimeCodeHash, self.RuntimeMetadataHash} {
		if !validHash(digest) || digest == "0x"+strings.Repeat("0", 64) {
			return errors.New("owner-recycle policy requires independently approved nonzero genesis, code and metadata hashes")
		}
	}
	if self.StorageProfile != recycleStorageProfile {
		return errors.New("owner-recycle storage profile is unsupported")
	}
	if len(self.RuntimeSourceCommit) != 40 || self.RuntimeSourceCommit == strings.Repeat("0", 40) {
		return errors.New("owner-recycle policy requires an independently reviewed runtime source commit")
	}
	if _, err := hex.DecodeString(self.RuntimeSourceCommit); err != nil {
		return errors.New("owner-recycle source commit is not hexadecimal")
	}
	if strings.TrimSpace(self.RuntimeVersion.SpecName) == "" || self.RuntimeVersion.SpecVersion == 0 || self.RuntimeVersion.TransactionVersion == 0 || self.RuntimeVersion.StateVersion == 0 {
		return errors.New("owner-recycle policy requires a complete nonzero approved runtime version identity")
	}
	return nil
}

// A successful mode precondition remains distinct from economic activation.
// Raw absence and the authenticated fallback are retained separately.
type recycleModeObservation struct {
	Schema              string                      `json:"schema"`
	PolicyHash          string                      `json:"policy_hash"`
	Identity            chainIdentity               `json:"identity"`
	Netuid              uint16                      `json:"netuid"`
	StorageProfile      string                      `json:"storage_profile"`
	Status              string                      `json:"status"`
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
	StorageKey          string                      `json:"storage_key"`
	RawStorage          *string                     `json:"raw_storage"`
	EffectiveScale      string                      `json:"effective_scale"`
	ValueSource         string                      `json:"value_source"`
	Mode                string                      `json:"mode"`
	ModeGatePassed      bool                        `json:"mode_gate_passed"`
	ActivationReady     bool                        `json:"activation_ready"`
	Blockers            []string                    `json:"activation_blockers"`
}

// The digest binds the observation; it is not a signature or approval.
type recycleModeEnvelope struct {
	Observation recycleModeObservation `json:"observation"`
	ContentHash string                 `json:"content_hash"`
}

// Resolves the exact reviewed Identity/u16 map and two fieldless SCALE variants.
// A changed default, optional query, duplicate entry or trailing enum payload
// cannot inherit the meaning of the reviewed source.
func recycleModeStorage(metadata *types.Metadata, netuid uint16) (types.StorageKey, []byte, error) {
	if metadata == nil || metadata.Version != 14 {
		return nil, nil, errors.New("owner-recycle requires authenticated metadata14")
	}
	var entry *types.StorageEntryMetadataV14
	palletCount := 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		palletCount++
		if !pallet.HasStorage || pallet.Storage.Prefix != "SubtensorModule" {
			return nil, nil, errors.New("owner-recycle storage prefix changed")
		}
		for _, candidate := range pallet.Storage.Items {
			if candidate.Name == "RecycleOrBurn" {
				if entry != nil {
					return nil, nil, errors.New("duplicate RecycleOrBurn storage")
				}
				copy := candidate
				entry = &copy
			}
		}
	}
	if palletCount != 1 || entry == nil || !entry.Modifier.IsDefault || entry.Modifier.IsOptional || !entry.Type.IsMap {
		return nil, nil, errors.New("RecycleOrBurn is not one default-valued storage map")
	}
	mapType := entry.Type.AsMap
	if len(mapType.Hashers) != 1 || !mapType.Hashers[0].IsIdentity {
		return nil, nil, errors.New("RecycleOrBurn key hashing differs from reviewed Identity")
	}
	keyType := metadata.AsMetadataV14.EfficientLookup[mapType.Key.Int64()]
	if keyType != nil && keyType.Def.IsComposite && len(keyType.Def.Composite.Fields) == 1 {
		keyType = metadata.AsMetadataV14.EfficientLookup[keyType.Def.Composite.Fields[0].Type.Int64()]
	}
	if keyType == nil || !keyType.Def.IsPrimitive || keyType.Def.Primitive.Si0TypeDefPrimitive != types.IsU16 {
		return nil, nil, errors.New("RecycleOrBurn key is not a SCALE u16 NetUid")
	}
	valueType := metadata.AsMetadataV14.EfficientLookup[mapType.Value.Int64()]
	if valueType == nil || !valueType.Def.IsVariant || len(valueType.Def.Variant.Variants) != 2 {
		return nil, nil, errors.New("RecycleOrBurn value is not the reviewed two-variant enum")
	}
	seen := map[string]bool{}
	for _, variant := range valueType.Def.Variant.Variants {
		name := string(variant.Name)
		if seen[name] || len(variant.Fields) != 0 || !(name == "Burn" && variant.Index == 0 || name == "Recycle" && variant.Index == 1) {
			return nil, nil, errors.New("RecycleOrBurn enum encoding differs from reviewed Burn=0, Recycle=1")
		}
		seen[name] = true
	}
	if len(entry.Fallback) != 1 || entry.Fallback[0] != 0 {
		return nil, nil, errors.New("RecycleOrBurn metadata fallback differs from reviewed Burn")
	}
	key, err := types.CreateStorageKey(metadata, "SubtensorModule", "RecycleOrBurn", chain.NetuidArg(netuid))
	return key, append([]byte(nil), entry.Fallback...), err
}

// All runtime and storage reads use the same finalized hash and one total
// deadline. No speculative/latest-state fallback or provisional runtime is used.
func (self *rpcClient) readRecycleMode(ctx context.Context, policy recyclePolicy, policyHash string) (recycleModeObservation, error) {
	observation := recycleModeObservation{}
	if err := policy.validate(); err != nil {
		return observation, err
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	identity, err := self.readIdentity(sampleCtx)
	if err != nil {
		return observation, err
	}
	expected := identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}
	if err := expected.match(identity); err != nil {
		return observation, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	var rawVersion json.RawMessage
	if err := self.call(sampleCtx, "state_getRuntimeVersion", []any{identity.FinalizedHash}, &rawVersion); err != nil {
		return observation, err
	}
	version, err := crv4.DecodeRuntimeVersionIdentity(rawVersion)
	if err != nil || version != policy.RuntimeVersion || uint64(version.SpecVersion) != identity.RuntimeSpec || uint64(version.TransactionVersion) != identity.RuntimeTx {
		return observation, fmt.Errorf("%w: finalized runtime version does not match reviewed policy: %v", errRpcIntegrity, err)
	}
	var codeHash string
	if err := self.call(sampleCtx, "state_getStorageHash", []any{"0x3a636f6465", identity.FinalizedHash}, &codeHash); err != nil {
		return observation, err
	}
	if !validHash(codeHash) || !strings.EqualFold(codeHash, policy.RuntimeCodeHash) {
		return observation, fmt.Errorf("%w: finalized runtime code hash differs from reviewed policy", errRpcIntegrity)
	}
	var metadataHex string
	if err := self.call(sampleCtx, "state_getMetadata", []any{identity.FinalizedHash}, &metadataHex); err != nil {
		return observation, err
	}
	if !strings.HasPrefix(metadataHex, "0x") || len(metadataHex) <= 2 || len(metadataHex)%2 != 0 {
		return observation, fmt.Errorf("%w: finalized runtime metadata is not canonical hex", errRpcIntegrity)
	}
	metadataRaw, err := hex.DecodeString(metadataHex[2:])
	if err != nil {
		return observation, fmt.Errorf("%w: finalized runtime metadata is invalid hex", errRpcIntegrity)
	}
	metadataDigest := blake2b.Sum256(metadataRaw)
	metadataHash := "0x" + hex.EncodeToString(metadataDigest[:])
	if !strings.EqualFold(metadataHash, policy.RuntimeMetadataHash) {
		return observation, fmt.Errorf("%w: finalized runtime metadata hash differs from reviewed policy", errRpcIntegrity)
	}
	// Authenticate bytes before the SCALE decoder can use their declared lengths.
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		return observation, fmt.Errorf("%w: finalized runtime metadata differs from reviewed policy: %v", errRpcIntegrity, err)
	}
	key, fallback, err := recycleModeStorage(metadata, policy.Netuid)
	if err != nil {
		return observation, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	var rawStorage *string
	if err := self.callWithStorageAbsence(sampleCtx, "state_getStorage", []any{key.Hex(), identity.FinalizedHash}, &rawStorage, true); err != nil {
		return observation, err
	}
	effective := fallback
	valueSource := "authenticated-metadata-fallback"
	if rawStorage != nil {
		if !strings.HasPrefix(*rawStorage, "0x") {
			return observation, fmt.Errorf("%w: mode storage is not 0x hex", errRpcIntegrity)
		}
		effective, err = hex.DecodeString((*rawStorage)[2:])
		if err != nil {
			return observation, fmt.Errorf("%w: mode storage is invalid hex", errRpcIntegrity)
		}
		valueSource = "finalized-storage"
	}
	if len(effective) != 1 || effective[0] > 1 {
		return observation, fmt.Errorf("%w: mode storage is not exactly one reviewed enum byte", errRpcIntegrity)
	}
	var confirmedHash string
	if err := self.call(sampleCtx, "chain_getBlockHash", []any{identity.FinalizedNumber}, &confirmedHash); err != nil {
		return observation, err
	}
	if !validHash(confirmedHash) || !strings.EqualFold(confirmedHash, identity.FinalizedHash) {
		return observation, fmt.Errorf("%w: finalized block changed during mode read", errRpcIntegrity)
	}
	if err := self.closeSnapshotFinality(sampleCtx, identity); err != nil {
		return observation, err
	}
	mode := "Burn"
	if effective[0] == 1 {
		mode = "Recycle"
	}
	return recycleModeObservation{
		Schema: recycleModeSchema, PolicyHash: policyHash, Identity: identity, Netuid: policy.Netuid,
		StorageProfile: policy.StorageProfile, Status: "mode-storage-verified-under-approved-source-artifact",
		RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: version,
		RuntimeCodeHash: strings.ToLower(codeHash), RuntimeMetadataHash: metadataHash,
		StorageKey: key.Hex(), RawStorage: rawStorage, EffectiveScale: "0x" + hex.EncodeToString(effective),
		ValueSource: valueSource, Mode: mode, ModeGatePassed: mode == "Recycle", ActivationReady: false,
		Blockers: []string{"source-to-code provenance and full runtime recycle semantics require separate review", "runtime-recognized owner-hotkey census and signed validator policy not qualified", "activation drain boundary and native interval allocation not authenticated", "final Yuma provider/recycle outcomes and runtime-derived rounding tolerance not qualified"},
	}, nil
}

// Seals only a complete observation, preserving a failed Burn precondition too.
func sealRecycleMode(observation recycleModeObservation) (recycleModeEnvelope, error) {
	raw, err := json.Marshal(observation)
	if err != nil {
		return recycleModeEnvelope{}, err
	}
	digest := sha256.Sum256(raw)
	return recycleModeEnvelope{Observation: observation, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}
