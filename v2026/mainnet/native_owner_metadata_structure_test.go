package main

import (
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// nativeOwnerMetadataItems renders the complete metadata interface by stable
// names, variant indexes and nested types rather than renumbered type IDs.
// It is shared by the exact historical and successor metadata delta reviews.
func nativeOwnerMetadataItems(metadata *types.Metadata) map[string]string {
	v14 := metadata.AsMetadataV14
	typeKVs := map[int64]types.Si1Type{}
	for _, entry := range v14.Lookup.Types {
		typeKVs[entry.ID.Int64()] = entry.Type
	}
	renderedKVs, activeKVs := map[int64]string{}, map[int64]bool{}
	var render func(int64) string
	fields := func(list []types.Si1Field) string {
		renderedFields := []string{}
		for _, field := range list {
			name := ""
			if field.HasName {
				name = string(field.Name) + ":"
			}
			renderedFields = append(renderedFields, name+render(field.Type.Int64()))
		}
		return "{" + strings.Join(renderedFields, ",") + "}"
	}
	render = func(id int64) string {
		if text, ok := renderedKVs[id]; ok {
			return text
		}
		entry := typeKVs[id]
		segments := []string{}
		for _, segment := range entry.Path {
			segments = append(segments, string(segment))
		}
		path := strings.Join(segments, "::")
		// A type already being rendered further up is named, not expanded, so a
		// recursive type terminates.
		if activeKVs[id] {
			return "@" + path
		}
		activeKVs[id] = true
		defer delete(activeKVs, id)
		var text string
		switch def := entry.Def; {
		case def.IsComposite:
			text = path + fields(def.Composite.Fields)
		case def.IsVariant:
			variants := []string{}
			for _, variant := range def.Variant.Variants {
				variants = append(variants, fmt.Sprintf("%d:%s%s", variant.Index, variant.Name, fields(variant.Fields)))
			}
			text = path + "<" + strings.Join(variants, "|") + ">"
		case def.IsSequence:
			text = "Vec<" + render(def.Sequence.Type.Int64()) + ">"
		case def.IsArray:
			text = fmt.Sprintf("[%s;%d]", render(def.Array.Type.Int64()), def.Array.Len)
		case def.IsTuple:
			members := []string{}
			for _, member := range def.Tuple {
				members = append(members, render(member.Int64()))
			}
			text = "(" + strings.Join(members, ",") + ")"
		case def.IsPrimitive:
			text = fmt.Sprintf("prim%d", def.Primitive.Si0TypeDefPrimitive)
		case def.IsCompact:
			text = "Compact<" + render(def.Compact.Type.Int64()) + ">"
		case def.IsBitSequence:
			text = "BitSeq<" + render(def.BitSequence.BitStoreType.Int64()) + "," + render(def.BitSequence.BitOrderType.Int64()) + ">"
		default:
			text = "?" + path
		}
		params := []string{}
		for _, param := range entry.Params {
			if param.HasType {
				params = append(params, string(param.Name)+"="+render(param.Type.Int64()))
			}
		}
		if len(params) != 0 {
			text += "[" + strings.Join(params, ",") + "]"
		}
		renderedKVs[id] = text
		return text
	}
	hasher := func(h types.StorageHasherV10) string {
		switch {
		case h.IsBlake2_128:
			return "Blake2_128"
		case h.IsBlake2_256:
			return "Blake2_256"
		case h.IsBlake2_128Concat:
			return "Blake2_128Concat"
		case h.IsTwox128:
			return "Twox128"
		case h.IsTwox256:
			return "Twox256"
		case h.IsTwox64Concat:
			return "Twox64Concat"
		case h.IsIdentity:
			return "Identity"
		}
		return "?"
	}
	itemKVs := map[string]string{}
	for _, pallet := range v14.Pallets {
		name := string(pallet.Name)
		itemKVs["pallet "+name] = fmt.Sprintf("index %d", pallet.Index)
		if pallet.HasStorage {
			for _, item := range pallet.Storage.Items {
				modifier := "default"
				if item.Modifier.IsOptional {
					modifier = "optional"
				}
				shape := "plain " + render(item.Type.AsPlainType.Int64())
				if item.Type.IsMap {
					hashers := []string{}
					for _, h := range item.Type.AsMap.Hashers {
						hashers = append(hashers, hasher(h))
					}
					shape = "map " + strings.Join(hashers, ",") + " " + render(item.Type.AsMap.Key.Int64()) + " -> " + render(item.Type.AsMap.Value.Int64())
				}
				itemKVs["storage "+name+"."+string(item.Name)] = fmt.Sprintf("prefix %s modifier %s %s fallback %x", pallet.Storage.Prefix, modifier, shape, []byte(item.Fallback))
			}
		}
		for _, kind := range []struct {
			label string
			has   bool
			id    int64
		}{
			{label: "call", has: bool(pallet.HasCalls), id: pallet.Calls.Type.Int64()},
			{label: "event", has: bool(pallet.HasEvents), id: pallet.Events.Type.Int64()},
			{label: "error", has: bool(pallet.HasErrors), id: pallet.Errors.Type.Int64()},
		} {
			if !kind.has {
				continue
			}
			for _, variant := range typeKVs[kind.id].Def.Variant.Variants {
				itemKVs[kind.label+" "+name+"."+string(variant.Name)] = fmt.Sprintf("index %d %s", variant.Index, fields(variant.Fields))
			}
		}
		for _, constant := range pallet.Constants {
			itemKVs["constant "+name+"."+string(constant.Name)] = render(constant.Type.Int64()) + " = " + fmt.Sprintf("%x", []byte(constant.Value))
		}
	}
	extensions := []string{}
	for _, extension := range v14.Extrinsic.SignedExtensions {
		extensions = append(extensions, string(extension.Identifier)+":"+render(extension.Type.Int64())+"/"+render(extension.AdditionalSigned.Int64()))
	}
	itemKVs["extrinsic"] = fmt.Sprintf("version %d type %s ext [%s]", v14.Extrinsic.Version, render(v14.Extrinsic.Type.Int64()), strings.Join(extensions, ";"))
	return itemKVs
}
