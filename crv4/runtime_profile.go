package crv4

// Compatibility is an explicit provisional admission, not a new reviewed
// runtime identity. The baseline is the current reviewed467 metadata. Only
// interfaces consumed by SN participate; unrelated root-basket changes do not.

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const ProvisionalRuntimeCompatibilityProfile = "urnetwork-subtensor-consumed-interface-v1"

//go:embed runtime-profile-v1.scale.gz.base64
var runtimeProfileBaselineEncoded string

var runtimeProfileBaseline = sync.OnceValues(func() (*types.Metadata, error) {
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(runtimeProfileBaselineEncoded))
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 347305))
	if err != nil || len(raw) != 347304 {
		return nil, errors.Join(errors.New("runtime profile baseline size differs"), err)
	}
	metadata, hash, err := DecodeRuntimeMetadata(fmt.Sprintf("0x%x", raw))
	if err != nil || hash != ReviewedRuntimeMetadataHash {
		return nil, errors.Join(errors.New("runtime profile baseline hash differs"), err)
	}
	return metadata, nil
})

var runtimeProfileStorage = map[string]string{
	"System":          "Account Events",
	"Timestamp":       "Now",
	"Ethereum":        "BlockHash",
	"Commitments":     "CommitmentOf LastCommitment MaxSpace UsedSpaceOf",
	"SubtensorModule": "SubnetworkN Keys Uids Owner TotalHotkeyAlpha ValidatorPermit StakeThreshold SubnetOwner SubnetOwnerHotkey SubnetEpochIndex Tempo LastEpochBlock PendingEpochAt BlocksSinceLastStep RevealPeriodEpochs CommitRevealWeightsEnabled CommitRevealWeightsVersion MaxWeightsLimit WeightsVersionKey Weights LastUpdate Emission Incentive Dividends BlockAtRegistration Burn MinBurn MaxBurn BurnHalfLife BurnIncreaseMult StakingHotkeys Lock MinerCollateral ColdkeyMinerCollateral MaxAllowedUids MaxAllowedValidators MechanismCountCurrent LiquidAlphaOn ImmunityPeriod MinAllowedWeights ServingRateLimit TransferToggle SubtokenEnabled NetworkRegisteredAt StartCallDelay FirstEmissionBlockNumber Delegates",
}

var runtimeProfileCalls = map[string]string{
	"Utility":         "batch_all",
	"Commitments":     "set_commitment",
	"Balances":        "transfer_keep_alive",
	"SubtensorModule": "commit_timelocked_weights commit_timelocked_mechanism_weights register_limit add_stake_limit start_call decrease_take transfer_stake_and_hotkey",
	"AdminUtils":      "sudo_set_burn_half_life sudo_set_tempo sudo_set_max_allowed_uids sudo_set_mechanism_count sudo_set_commit_reveal_weights_enabled sudo_set_commit_reveal_weights_interval sudo_set_liquid_alpha_enabled sudo_set_immunity_period sudo_set_min_allowed_weights sudo_set_weights_version_key sudo_set_serving_rate_limit sudo_set_toggle_transfer",
}

var runtimeProfileEvents = map[string]string{
	"System":          "ExtrinsicSuccess ExtrinsicFailed",
	"Commitments":     "Commitment",
	"SubtensorModule": "TimelockedWeightsCommitted",
	"Utility":         "ItemCompleted BatchCompleted BatchInterrupted ItemFailed BatchCompletedWithErrors",
}

// Portable type ids, docs and Rust module paths are not wire identities. Field
// names/order, variant indices, widths and container shapes are. RuntimeCall
// and RuntimeEvent are traversed through the selected calls/events separately.
func runtimeProfileType(metadata *types.Metadata, id types.Si1LookupTypeID, stack map[int64]int) (any, error) {
	if len(stack) > 64 {
		return nil, errors.New("runtime profile type depth exceeded")
	}
	key := id.Int64()
	item := metadata.AsMetadataV14.EfficientLookup[key]
	if item == nil {
		return nil, fmt.Errorf("runtime profile type %d is absent", key)
	}
	if len(item.Path) != 0 {
		name := string(item.Path[len(item.Path)-1])
		if name == "RuntimeCall" || name == "RuntimeEvent" {
			return name, nil
		}
	}
	if depth, exists := stack[key]; exists {
		return []any{"recursive", len(stack) - depth}, nil
	}
	stack[key] = len(stack)
	defer delete(stack, key)
	child := func(id types.Si1LookupTypeID) (any, error) { return runtimeProfileType(metadata, id, stack) }
	fields := func(values []types.Si1Field) ([]any, error) {
		out := make([]any, 0, len(values))
		for _, field := range values {
			value, err := child(field.Type)
			if err != nil {
				return nil, err
			}
			out = append(out, []any{field.HasName, string(field.Name), value})
		}
		return out, nil
	}
	d := item.Def
	switch {
	case d.IsPrimitive:
		return []any{"primitive", uint8(d.Primitive.Si0TypeDefPrimitive)}, nil
	case d.IsComposite:
		value, err := fields(d.Composite.Fields)
		return []any{"composite", value}, err
	case d.IsVariant:
		values := make([]any, 0, len(d.Variant.Variants))
		for _, variant := range d.Variant.Variants {
			value, err := fields(variant.Fields)
			if err != nil {
				return nil, err
			}
			values = append(values, []any{string(variant.Name), uint8(variant.Index), value})
		}
		return []any{"variant", values}, nil
	case d.IsSequence:
		value, err := child(d.Sequence.Type)
		return []any{"sequence", value}, err
	case d.IsArray:
		value, err := child(d.Array.Type)
		return []any{"array", uint32(d.Array.Len), value}, err
	case d.IsTuple:
		values := make([]any, 0, len(d.Tuple))
		for _, item := range d.Tuple {
			value, err := child(item)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return []any{"tuple", values}, nil
	case d.IsCompact:
		value, err := child(d.Compact.Type)
		return []any{"compact", value}, err
	default:
		return nil, errors.New("runtime profile consumed type is unsupported")
	}
}

func runtimeProfileShape(metadata *types.Metadata) (map[string]any, error) {
	if metadata == nil || metadata.Version != 14 {
		return nil, errors.New("runtime compatibility requires metadata14")
	}
	out := map[string]any{}
	shape := func(id types.Si1LookupTypeID) (any, error) { return runtimeProfileType(metadata, id, map[int64]int{}) }
	pallets := map[string]types.PalletMetadataV14{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if _, exists := pallets[string(pallet.Name)]; exists {
			return nil, errors.New("runtime profile duplicate pallet")
		}
		pallets[string(pallet.Name)] = pallet
	}
	for module, names := range runtimeProfileStorage {
		pallet, ok := pallets[module]
		if !ok || !pallet.HasStorage {
			return nil, fmt.Errorf("runtime profile storage pallet %s absent", module)
		}
		for _, name := range strings.Fields(names) {
			var found *types.StorageEntryMetadataV14
			for _, item := range pallet.Storage.Items {
				if string(item.Name) == name {
					if found != nil {
						return nil, errors.New("runtime profile duplicate storage")
					}
					copy := item
					found = &copy
				}
			}
			if found == nil {
				return nil, fmt.Errorf("runtime profile storage %s.%s absent", module, name)
			}
			key := "storage/" + module + "." + name
			var encoding any
			if found.Type.IsPlainType {
				value, err := shape(found.Type.AsPlainType)
				if err != nil {
					return nil, err
				}
				encoding = []any{"plain", value}
			} else if found.Type.IsMap {
				mapKey, err := shape(found.Type.AsMap.Key)
				if err != nil {
					return nil, err
				}
				value, err := shape(found.Type.AsMap.Value)
				if err != nil {
					return nil, err
				}
				encoding = []any{"map", found.Type.AsMap.Hashers, mapKey, value}
			} else {
				return nil, fmt.Errorf("runtime profile %s has invalid storage", key)
			}
			out[key] = []any{string(pallet.Storage.Prefix), found.Modifier, fmt.Sprintf("%x", []byte(found.Fallback)), encoding}
		}
	}
	for kind, selection := range map[string]map[string]string{"call": runtimeProfileCalls, "event": runtimeProfileEvents} {
		for module, names := range selection {
			pallet, ok := pallets[module]
			if !ok {
				return nil, fmt.Errorf("runtime profile %s pallet %s absent", kind, module)
			}
			id, present := pallet.Calls.Type, pallet.HasCalls
			if kind == "event" {
				id, present = pallet.Events.Type, pallet.HasEvents
			}
			if !present {
				return nil, fmt.Errorf("runtime profile %s pallet %s has no variants", kind, module)
			}
			item := metadata.AsMetadataV14.EfficientLookup[id.Int64()]
			if item == nil || !item.Def.IsVariant {
				return nil, errors.New("runtime profile call/event type is invalid")
			}
			for _, name := range strings.Fields(names) {
				key := kind + "/" + module + "." + name
				for _, variant := range item.Def.Variant.Variants {
					if string(variant.Name) != name {
						continue
					}
					if _, exists := out[key]; exists {
						return nil, errors.New("runtime profile duplicate call/event")
					}
					fields := make([]any, 0, len(variant.Fields))
					for _, field := range variant.Fields {
						value, err := shape(field.Type)
						if err != nil {
							return nil, err
						}
						fields = append(fields, []any{field.HasName, string(field.Name), value})
					}
					out[key] = []any{uint8(pallet.Index), uint8(variant.Index), fields}
				}
				if _, exists := out[key]; !exists {
					return nil, fmt.Errorf("runtime profile %s absent", key)
				}
			}
		}
	}
	extensions := make([]any, 0, len(metadata.AsMetadataV14.Extrinsic.SignedExtensions))
	for _, extension := range metadata.AsMetadataV14.Extrinsic.SignedExtensions {
		value, err := shape(extension.Type)
		if err != nil {
			return nil, err
		}
		additional, err := shape(extension.AdditionalSigned)
		if err != nil {
			return nil, err
		}
		extensions = append(extensions, []any{string(extension.Identifier), value, additional})
	}
	out["signed-extensions"] = []any{uint8(metadata.AsMetadataV14.Extrinsic.Version), extensions}
	return out, nil
}

// Only compatibility of the consumed wire interfaces is claimed. The complete
// economic semantics of unknown Wasm remain outside final acceptance.
func ValidateProvisionalRuntimeMetadata(metadata *types.Metadata) error {
	baseline, err := runtimeProfileBaseline()
	if err != nil {
		return err
	}
	expected, err := runtimeProfileShape(baseline)
	if err != nil {
		return fmt.Errorf("runtime profile baseline: %w", err)
	}
	actual, err := runtimeProfileShape(metadata)
	if err != nil {
		return err
	}
	var failures []error
	for name, shape := range expected {
		if !reflect.DeepEqual(actual[name], shape) {
			failures = append(failures, fmt.Errorf("runtime consumed interface %s changed", name))
		}
	}
	return errors.Join(failures...)
}

// Metadata14 does not expose runtime API signatures. Pin the consumed API's
// declared version separately; changes to other APIs do not affect admission.
func validateProvisionalRuntimeApis(raw json.RawMessage) error {
	var version struct {
		Apis []json.RawMessage `json:"apis"`
	}
	if err := json.Unmarshal(raw, &version); err != nil {
		return err
	}
	found := false
	for _, encoded := range version.Apis {
		var pair []json.RawMessage
		if err := json.Unmarshal(encoded, &pair); err != nil || len(pair) != 2 {
			return errors.New("runtime API identity is malformed")
		}
		var id string
		if err := json.Unmarshal(pair[0], &id); err != nil {
			return err
		}
		if id != "0x8375104b299b74c5" {
			continue
		}
		value, err := decodeRuntimeVersionUint("SubnetInfoRuntimeApi", pair[1], 32)
		if found || err != nil || value != 2 {
			return errors.New("consumed SubnetInfoRuntimeApi is not version2")
		}
		found = true
	}
	if !found {
		return errors.New("consumed SubnetInfoRuntimeApi is absent")
	}
	return nil
}
