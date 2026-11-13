// Receipt events are traversed with explicit byte, depth and work bounds. A
// corrupt vector length must not allocate an attacker-sized slice or hide
// trailing bytes after an apparently valid dispatch event.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"unicode/utf8"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// One immutable SCALE bundle shares a work budget across all event fields.
type rootScaleReader struct {
	data     []byte
	offset   int
	work     int
	metadata *types.Metadata
}

// Slices reference the bounded original bundle; decoding never reallocates it.
func (self *rootScaleReader) take(count int) ([]byte, error) {
	if count < 0 || count > len(self.data)-self.offset {
		return nil, errors.New("truncated root receipt SCALE data")
	}
	value := self.data[self.offset : self.offset+count]
	self.offset += count
	return value, nil
}

// Canonical SCALE compact integers are bounded to u64 for lengths and balances.
func (self *rootScaleReader) compact() (uint64, error) {
	start := self.offset
	first, err := self.take(1)
	if err != nil {
		return 0, err
	}
	var value uint64
	switch first[0] & 3 {
	case 0:
		value = uint64(first[0] >> 2)
	case 1, 2:
		length := 2
		if first[0]&3 == 2 {
			length = 4
		}
		tail, err := self.take(length - 1)
		if err != nil {
			return 0, err
		}
		value = uint64(first[0])
		for index, part := range tail {
			value |= uint64(part) << (8 * (index + 1))
		}
		value >>= 2
	case 3:
		length := int(first[0]>>2) + 4
		if length > 8 {
			return 0, errors.New("root receipt compact integer exceeds u64")
		}
		tail, err := self.take(length)
		if err != nil {
			return 0, err
		}
		for index, part := range tail {
			value |= uint64(part) << (8 * index)
		}
	}
	if !bytes.Equal(self.data[start:self.offset], rootCompact(value)) {
		return 0, errors.New("noncanonical root receipt compact integer")
	}
	return value, nil
}

// Metadata determines the complete event shape, including unrelated fields.
// Unsupported bit sequences fail explicitly; no generic decoder can silently
// reinterpret them. Runtime admission must qualify actual emitted shapes.
func (self *rootScaleReader) skip(id types.Si1LookupTypeID, depth int) error {
	self.work++
	if depth > 64 || self.work > 1024*1024 || self.metadata == nil {
		return errors.New("root receipt SCALE traversal exceeds resource bound")
	}
	entry := self.metadata.AsMetadataV14.EfficientLookup[id.Int64()]
	if entry == nil {
		return errors.New("root receipt metadata type is absent")
	}
	def := entry.Def
	switch {
	case def.IsComposite:
		for _, field := range def.Composite.Fields {
			if err := self.skip(field.Type, depth+1); err != nil {
				return err
			}
		}
	case def.IsTuple:
		for _, field := range def.Tuple {
			if err := self.skip(field, depth+1); err != nil {
				return err
			}
		}
	case def.IsVariant:
		raw, err := self.take(1)
		if err != nil {
			return err
		}
		var selected *types.Si1Variant
		seen := map[byte]bool{}
		for index := range def.Variant.Variants {
			variant := &def.Variant.Variants[index]
			if seen[byte(variant.Index)] {
				return errors.New("root receipt variant index is duplicated")
			}
			seen[byte(variant.Index)] = true
			if byte(variant.Index) == raw[0] {
				selected = variant
			}
		}
		if selected == nil {
			return errors.New("root receipt variant index is unknown")
		}
		for _, field := range selected.Fields {
			if err := self.skip(field.Type, depth+1); err != nil {
				return err
			}
		}
	case def.IsArray, def.IsSequence:
		count, item := uint64(def.Array.Len), def.Array.Type
		if def.IsSequence {
			var err error
			count, err = self.compact()
			if err != nil {
				return err
			}
			item = def.Sequence.Type
		}
		if count > rootBodyCountLimit {
			return errors.New("root receipt vector count exceeds resource bound")
		}
		for index := uint64(0); index < count; index++ {
			if err := self.skip(item, depth+1); err != nil {
				return err
			}
		}
	case def.IsCompact:
		value, err := self.compact()
		if err != nil {
			return err
		}
		if rootSigningType(self.metadata, def.Compact.Type, "u32", 0) && value > (1<<32)-1 {
			return errors.New("root receipt compact u32 overflow")
		}
		if !rootSigningType(self.metadata, def.Compact.Type, "u32", 0) && !rootSigningType(self.metadata, def.Compact.Type, "u64", 0) && !rootTypeMatches(self.metadata, def.Compact.Type, "u16", 0) && !rootTypeMatches(self.metadata, def.Compact.Type, "u8", 0) {
			return errors.New("unsupported root receipt compact type")
		}
		if rootTypeMatches(self.metadata, def.Compact.Type, "u16", 0) && value > (1<<16)-1 || rootTypeMatches(self.metadata, def.Compact.Type, "u8", 0) && value > 255 {
			return errors.New("root receipt compact narrow integer overflow")
		}
	case def.IsPrimitive:
		primitive := def.Primitive.Si0TypeDefPrimitive
		width := map[types.Si0TypeDefPrimitive]int{types.IsBool: 1, types.IsChar: 4, types.IsU8: 1, types.IsU16: 2, types.IsU32: 4, types.IsU64: 8, types.IsU128: 16, types.IsU256: 32, types.IsI8: 1, types.IsI16: 2, types.IsI32: 4, types.IsI64: 8, types.IsI128: 16, types.IsI256: 32}[primitive]
		if primitive == types.IsStr {
			length, err := self.compact()
			if err != nil || length > uint64(len(self.data)-self.offset) {
				return errors.New("root receipt string length is invalid")
			}
			width = int(length)
		}
		if width == 0 && primitive != types.IsStr {
			return errors.New("root receipt primitive is unsupported")
		}
		raw, err := self.take(width)
		if err != nil {
			return err
		}
		if primitive == types.IsBool && raw[0] > 1 || primitive == types.IsStr && !utf8.Valid(raw) || primitive == types.IsChar && !utf8.ValidRune(rune(binary.LittleEndian.Uint32(raw))) {
			return errors.New("root receipt primitive value is invalid")
		}
	default:
		return errors.New("root receipt SCALE type is unsupported")
	}
	return nil
}
