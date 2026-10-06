// Root action encoding is separate from the immortal UR transaction helpers.
// It constructs one mortal hotkey action; neither metadata nor its checksum is
// an approval, an eligibility decision, or permission to use a signing key.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"path/filepath"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/v2026/crv4"
)

const rootActionSchema = "urnetwork-mainnet-root-action-v1"

// Historical action encoding must not follow the observer's future profile.
// Add a separately reviewed codec instead of changing this V1 source identity.
const rootActionV1Source = "67dcf7f791dc495064c293f080a0702cb433e51e"
const rootCallPallet = 7
const rootWeightsCall = 146

// One reviewed artifact permits at most one signature and one nonce. The fee
// reserve is local accounting, not an on-chain maximum-fee argument. A custody
// adapter must independently enforce the approved exposure before signing.
type rootActionScope struct {
	Schema              string                      `json:"schema"`
	Role                string                      `json:"role"`
	Netuid              uint16                      `json:"netuid"`
	NativeChain         string                      `json:"native_chain"`
	GenesisHash         string                      `json:"genesis_hash"`
	EvmChainId          uint64                      `json:"evm_chain_id"`
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
	Hotkey              string                      `json:"hotkey_account_id"`
	Coldkey             string                      `json:"coldkey_account_id"`
	Seat                rootSeatExpectation         `json:"seat"`
	PolicyHash          string                      `json:"policy_hash"`
	ApprovalHash        string                      `json:"approval_hash"`
	CustodyId           string                      `json:"custody_id"`
	StatePath           string                      `json:"state_path"`
	Strategy            string                      `json:"strategy"`
	FeeReserveRao       uint64                      `json:"fee_reserve_rao"`
	ValidUntilBlock     uint64                      `json:"valid_until_block"`
	MaxBroadcasts       uint8                       `json:"max_broadcasts"`
}

// The exact call and mortal anchor are part of the signer request identity.
// No nonce refresh, new era or runtime migration may mutate an existing action.
type rootAction struct {
	Scope       rootActionScope `json:"scope"`
	Dests       []uint16        `json:"dests"`
	Weights     []uint16        `json:"weights"`
	Nonce       uint32          `json:"nonce"`
	BirthBlock  uint64          `json:"birth_block"`
	BirthHash   string          `json:"birth_hash"`
	Period      uint64          `json:"mortal_period"`
	Call        string          `json:"call_scale"`
	Payload     string          `json:"payload_scale"`
	RequestHash string          `json:"request_hash"`
}

// Content hashes bind local artifacts; only an external authority can approve.
func rootObjectHash(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Canonical lower-case hashes prevent equivalent spellings changing ownership.
func rootCanonicalHash(value string) bool {
	return validHash(value) && value == strings.ToLower(value) && value != "0x"+strings.Repeat("0", 64)
}

// Requires independent nonempty authority references and explicit local bounds.
func (self rootActionScope) validate() error {
	if self.Schema != rootActionSchema || self.Role != "bittensor-root-validator" || self.Netuid != 0 || self.EvmChainId != mainnetEvmChainId || strings.TrimSpace(self.NativeChain) == "" || self.RuntimeSourceCommit != rootActionV1Source {
		return errors.New("root action requires its exact root role, mainnet identity and reviewed runtime source")
	}
	for _, value := range []string{self.GenesisHash, self.RuntimeCodeHash, self.RuntimeMetadataHash, self.Hotkey, self.Coldkey} {
		if !rootCanonicalHash(value) {
			return errors.New("root action requires canonical nonzero domain and account hashes")
		}
	}
	for _, value := range []string{self.PolicyHash, self.ApprovalHash} {
		if !strings.HasPrefix(value, "sha256:") || !rootCanonicalHash("0x"+strings.TrimPrefix(value, "sha256:")) {
			return errors.New("root action requires independent policy and approval content hashes")
		}
	}
	if self.RuntimeVersion.SpecName == "" || self.RuntimeVersion.SpecVersion == 0 || self.RuntimeVersion.TransactionVersion == 0 || self.RuntimeVersion.StateVersion == 0 || self.Seat.RegistrationBlock == 0 || self.Seat.RegistrationBlock > math.MaxUint32 {
		return errors.New("root action runtime or existing-seat generation is incomplete")
	}
	if self.Strategy != "explicit_root_weights" || self.CustodyId == "" || len(self.CustodyId) > 128 || !filepath.IsAbs(self.StatePath) || filepath.Clean(self.StatePath) != self.StatePath || filepath.Base(self.StatePath) == "." || self.StatePath == "/" {
		return errors.New("root signing requires a separately approved explicit-weight strategy, custody and canonical state path")
	}
	if self.FeeReserveRao == 0 || self.ValidUntilBlock == 0 || self.ValidUntilBlock > math.MaxUint32 || self.MaxBroadcasts == 0 || self.MaxBroadcasts > 8 {
		return errors.New("root action needs finite fee, block-expiry and broadcast bounds")
	}
	return nil
}

// All supported periods have unit quantization, so the supplied finalized
// anchor is exactly Era::birth(anchor), with no rounded or invented checkpoint.
func rootMortalEra(birth, period uint64) ([]byte, error) {
	if birth == 0 || birth > math.MaxUint32 || period < 4 || period > 256 || period&(period-1) != 0 || birth > math.MaxUint32-period {
		return nil, errors.New("root mortal era requires a finalized u32 anchor and power-of-two period from 4 through 256")
	}
	encoded := uint16(bits.TrailingZeros64(period)-1) | uint16(birth%period)<<4
	return binary.LittleEndian.AppendUint16(nil, encoded), nil
}

// The bounded inputs cannot fail compact SCALE encoding.
func rootCompact(value uint64) []byte {
	raw, _ := codec.Encode(types.NewUCompactFromUInt(value))
	return raw
}

// Recursively authenticates the selected wire shapes; unrecognized extensions
// cannot be omitted just because a library happens to encode an empty field.
func rootSigningType(metadata *types.Metadata, id types.Si1LookupTypeID, shape string, depth int) bool {
	if depth > 16 {
		return false
	}
	entry := metadata.AsMetadataV14.EfficientLookup[id.Int64()]
	if entry == nil {
		return false
	}
	def := entry.Def
	if def.IsComposite && len(def.Composite.Fields) == 1 {
		return rootSigningType(metadata, def.Composite.Fields[0].Type, shape, depth+1)
	}
	switch shape {
	case "unit":
		return def.IsComposite && len(def.Composite.Fields) == 0 || def.IsTuple && len(def.Tuple) == 0
	case "u32":
		return def.IsPrimitive && def.Primitive.Si0TypeDefPrimitive == types.IsU32
	case "compact32", "compact64":
		inner := "u32"
		if shape == "compact64" {
			inner = "u64"
		}
		return def.IsCompact && rootSigningType(metadata, def.Compact.Type, inner, depth+1)
	case "u16s":
		return def.IsSequence && rootTypeMatches(metadata, def.Sequence.Type, "u16", 0)
	case "signature64":
		return def.IsArray && def.Array.Len == 64 && rootTypeMatches(metadata, def.Array.Type, "u8", 0)
	case "era":
		if !def.IsVariant || len(def.Variant.Variants) != 256 {
			return false
		}
		for index, variant := range def.Variant.Variants {
			if int(variant.Index) != index || index == 0 && (variant.Name != "Immortal" || len(variant.Fields) != 0) || index > 0 && (string(variant.Name) != fmt.Sprintf("Mortal%d", index) || len(variant.Fields) != 1 || !rootTypeMatches(metadata, variant.Fields[0].Type, "u8", 0)) {
				return false
			}
		}
		return true
	case "mode", "optional-hash":
		if !def.IsVariant || len(def.Variant.Variants) != 2 {
			return false
		}
		first, second := def.Variant.Variants[0], def.Variant.Variants[1]
		if first.Index != 0 || second.Index != 1 || len(first.Fields) != 0 {
			return false
		}
		if shape == "mode" {
			return first.Name == "Disabled" && second.Name == "Enabled" && len(second.Fields) == 0
		}
		return first.Name == "None" && second.Name == "Some" && len(second.Fields) == 1 && rootTypeMatches(metadata, second.Fields[0].Type, "account", 0)
	}
	return rootTypeMatches(metadata, id, shape, 0)
}

// Admits only the inspected version-4 extension order and root basket setter.
// Shape compatibility does not establish source-to-Wasm semantics or eligibility.
func rootSigningProfile(metadata *types.Metadata) ([]byte, error) {
	if err := nativeSigningProfile(metadata); err != nil {
		return nil, err
	}
	return rootWeightsSigningCall(metadata)
}

// Both native roles use the same reviewed envelope, without borrowing another
// role's call or authority. The caller validates its own exact call separately.
func nativeSigningProfile(metadata *types.Metadata) error {
	return nativeSigningProfileForSignature(metadata, "Sr25519", 1)
}

// Each separately approved native codec selects one exact signature variant.
// A supported alternative never changes a retained root or owner v1 signature.
func nativeSigningProfileForSignature(metadata *types.Metadata, signatureVariant string, signatureIndex uint8) error {
	if metadata == nil || metadata.Version != 14 || metadata.AsMetadataV14.Extrinsic.Version != 4 {
		return errors.New("native signing requires the reviewed metadata14/extrinsic4 profile")
	}
	names := []string{"CheckNonZeroSender", "CheckSpecVersion", "CheckTxVersion", "CheckGenesis", "CheckMortality", "CheckNonce", "CheckWeight", "ChargeTransactionPayment", "SudoTransactionExtension", "CheckShieldedTxValidity", "SubtensorTransactionExtension", "DrandPriority", "CheckMetadataHash"}
	values := []string{"unit", "unit", "unit", "unit", "era", "compact32", "unit", "compact64", "unit", "unit", "unit", "unit", "mode"}
	additional := []string{"unit", "u32", "u32", "account", "account", "unit", "unit", "unit", "unit", "unit", "unit", "unit", "optional-hash"}
	extensions := metadata.AsMetadataV14.Extrinsic.SignedExtensions
	if len(extensions) != len(names) {
		return errors.New("native signed extension count changed")
	}
	for index, extension := range extensions {
		if string(extension.Identifier) != names[index] || !rootSigningType(metadata, extension.Type, values[index], 0) || !rootSigningType(metadata, extension.AdditionalSigned, additional[index], 0) {
			return fmt.Errorf("native signed extension %d name or wire shape changed", index)
		}
	}
	extrinsicType := metadata.AsMetadataV14.EfficientLookup[metadata.AsMetadataV14.Extrinsic.Type.Int64()]
	if extrinsicType == nil {
		return errors.New("native extrinsic type parameters are missing")
	}
	for _, expected := range []struct {
		parameter string
		variant   string
		index     uint8
		shape     string
	}{
		{parameter: "Address", variant: "Id", index: 0, shape: "account"},
		{parameter: "Signature", variant: signatureVariant, index: signatureIndex, shape: "signature64"},
	} {
		matched := 0
		for _, parameter := range extrinsicType.Params {
			if string(parameter.Name) != expected.parameter {
				continue
			}
			entry := metadata.AsMetadataV14.EfficientLookup[parameter.Type.Int64()]
			if !parameter.HasType || entry == nil || !entry.Def.IsVariant {
				return errors.New("native address/signature type shape changed")
			}
			seenIndices := map[uint8]bool{}
			for _, variant := range entry.Def.Variant.Variants {
				if seenIndices[uint8(variant.Index)] {
					return errors.New("native address/signature has duplicate variant indices")
				}
				seenIndices[uint8(variant.Index)] = true
				if string(variant.Name) != expected.variant {
					continue
				}
				if uint8(variant.Index) != expected.index || len(variant.Fields) != 1 || !rootSigningType(metadata, variant.Fields[0].Type, expected.shape, 0) {
					return errors.New("native selected address/signature encoding changed")
				}
				matched++
			}
		}
		if matched != 1 {
			return errors.New("native selected address/signature parameter missing or duplicated")
		}
	}
	return nil
}

// Root weights retain their original independently checked call profile.
func rootWeightsSigningCall(metadata *types.Metadata) ([]byte, error) {
	var callIndex []byte
	palletCount := 0
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		palletCount++
		if palletCount != 1 || !pallet.HasCalls {
			return nil, errors.New("root call pallet missing or duplicated")
		}
		entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		if entry == nil || !entry.Def.IsVariant {
			return nil, errors.New("root call variant metadata absent")
		}
		seenIndices := map[uint8]bool{}
		for _, variant := range entry.Def.Variant.Variants {
			if seenIndices[uint8(variant.Index)] {
				return nil, errors.New("root pallet call indices are duplicated")
			}
			seenIndices[uint8(variant.Index)] = true
			if variant.Name != "set_root_weights" {
				continue
			}
			if callIndex != nil || len(variant.Fields) != 2 || variant.Fields[0].Name != "dests" || variant.Fields[1].Name != "weights" || !rootSigningType(metadata, variant.Fields[0].Type, "u16s", 0) || !rootSigningType(metadata, variant.Fields[1].Type, "u16s", 0) {
				return nil, errors.New("root basket call schema changed")
			}
			callIndex = []byte{byte(pallet.Index), byte(variant.Index)}
		}
	}
	if len(callIndex) != 2 || callIndex[0] != rootCallPallet || callIndex[1] != rootWeightsCall {
		return nil, errors.New("root basket call unavailable")
	}
	return callIndex, nil
}

// Produces a reviewable request from independently pinned raw metadata. No
// signing backend is constructed and no current observer-ready bit is consumed.
func prepareRootAction(action rootAction, metadataHex string) (rootAction, error) {
	if len(metadataHex) > 2+2*maxMetadataRpcReplyBytes {
		return rootAction{}, errors.New("root action metadata exceeds its resource bound")
	}
	metadata, digest, err := nativePinnedMetadata(metadataHex, action.Scope.RuntimeMetadataHash)
	if err != nil || digest != action.Scope.RuntimeMetadataHash {
		return rootAction{}, errors.Join(errors.New("root action metadata differs from approved bytes"), err)
	}
	callIndex, err := rootSigningProfile(metadata)
	if err != nil {
		return rootAction{}, err
	}
	action.Call = "0x" + hex.EncodeToString(callIndex)
	call, payload, err := action.encoding()
	if err != nil {
		return rootAction{}, err
	}
	action.Call, action.Payload = "0x"+hex.EncodeToString(call), "0x"+hex.EncodeToString(payload)
	action.RequestHash = ""
	action.RequestHash = rootObjectHash(action)
	return action, nil
}

// Reconstructs canonical bytes rather than accepting an opaque signer payload.
func (self rootAction) encoding() ([]byte, []byte, error) {
	if err := self.Scope.validate(); err != nil {
		return nil, nil, err
	}
	era, err := rootMortalEra(self.BirthBlock, self.Period)
	if err != nil || self.Nonce == math.MaxUint32 || self.BirthBlock < self.Scope.Seat.RegistrationBlock || self.BirthBlock+self.Period > self.Scope.ValidUntilBlock || !rootCanonicalHash(self.BirthHash) {
		return nil, nil, errors.Join(errors.New("root action anchor or expiry is outside approval"), err)
	}
	if len(self.Dests) == 0 || len(self.Dests) > rootCensusLimit || len(self.Dests) != len(self.Weights) {
		return nil, nil, errors.New("root action requires equally sized bounded destination/weight vectors")
	}
	call, err := hex.DecodeString(strings.TrimPrefix(self.Call, "0x"))
	if err != nil || !strings.HasPrefix(self.Call, "0x") || len(call) < 2 || call[0] != rootCallPallet || call[1] != rootWeightsCall {
		return nil, nil, errors.New("root action call index is missing")
	}
	call = append([]byte(nil), call[:2]...)
	call = append(call, rootCompact(uint64(len(self.Dests)))...)
	for index, dest := range self.Dests {
		if index > 0 && self.Dests[index-1] >= dest || self.Weights[index] == 0 {
			return nil, nil, errors.New("root action requires sorted unique destinations and positive weights")
		}
		call = binary.LittleEndian.AppendUint16(call, dest)
	}
	call = append(call, rootCompact(uint64(len(self.Weights)))...)
	for _, weight := range self.Weights {
		call = binary.LittleEndian.AppendUint16(call, weight)
	}
	payload := append(append([]byte(nil), call...), era...)
	payload = append(payload, rootCompact(uint64(self.Nonce))...)
	payload = append(payload, 0, 0) // zero tip and disabled metadata-hash extension
	payload = binary.LittleEndian.AppendUint32(payload, self.Scope.RuntimeVersion.SpecVersion)
	payload = binary.LittleEndian.AppendUint32(payload, self.Scope.RuntimeVersion.TransactionVersion)
	genesis, _ := hex.DecodeString(self.Scope.GenesisHash[2:])
	birthHash, _ := hex.DecodeString(self.BirthHash[2:])
	payload = append(payload, genesis...)
	payload = append(payload, birthHash...)
	payload = append(payload, 0) // no additional metadata hash
	return call, payload, nil
}

// Validates retained contents without needing current-runtime metadata again.
func (self rootAction) validate() error {
	call, payload, err := self.encoding()
	if err != nil {
		return err
	}
	claimed := self.RequestHash
	self.RequestHash = ""
	if self.Call != "0x"+hex.EncodeToString(call) || self.Payload != "0x"+hex.EncodeToString(payload) || claimed != rootObjectHash(self) {
		return errors.New("root action retained request content differs")
	}
	return nil
}

// Verifies the public signature and assembles only the exact authorized call.
// Substrate hashes payloads longer than 256 bytes before sr25519 signing.
func (self rootAction) signed(signature []byte) ([]byte, error) {
	if err := self.validate(); err != nil {
		return nil, err
	}
	account, _ := hex.DecodeString(self.Scope.Hotkey[2:])
	public, err := (sr25519.Scheme{}).FromPublicKey(account)
	if err != nil {
		return nil, err
	}
	payload, _ := hex.DecodeString(self.Payload[2:])
	if len(payload) > 256 {
		digest := blake2b.Sum256(payload)
		payload = digest[:]
	}
	if len(signature) != 64 || !public.Verify(payload, signature) {
		return nil, errors.New("root signer returned a signature for another key or payload")
	}
	body := append([]byte{0x84, 0}, account...)
	body = append(body, 1) // MultiSignature::Sr25519
	body = append(body, signature...)
	era, _ := rootMortalEra(self.BirthBlock, self.Period)
	body = append(body, era...)
	body = append(body, rootCompact(uint64(self.Nonce))...)
	body = append(body, 0, 0)
	call, _ := hex.DecodeString(self.Call[2:])
	body = append(body, call...)
	return append(rootCompact(uint64(len(body))), body...), nil
}
