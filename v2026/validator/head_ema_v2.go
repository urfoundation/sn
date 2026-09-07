package validator

// Bounded restart admission is separate from live preview and durable commit.
// It accepts the existing finite EMA schemas without repairing persisted state.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// The caller chooses all limits from trusted configuration, never file data.
// File bytes and control payload are independent allowances. Control includes
// decoded records, retained indexes and a conservative rational-work reserve;
// it is not a bound on Go allocator overhead or process RSS.
type HeadEMAStoreV2Limits struct {
	MaxFileBytes    uint64
	MaxEntries      uint64
	MaxControlBytes uint64
}

const maxHeadEMAStoreV2Bytes = 64 * 1024 * 1024

// Both the decoded file and returned store coexist during verification.
func headEMAStoreV2FixedControlBytes() uint64 {
	return uint64(reflect.TypeFor[headEMAFile]().Size()) + uint64(reflect.TypeFor[HeadEMAStore]().Size())
}

// Refuse invalid limits before any native acquisition or length conversion.
func (self HeadEMAStoreV2Limits) validate() error {
	if self.MaxFileBytes == 0 || self.MaxFileBytes > maxHeadEMAStoreV2Bytes || self.MaxEntries == 0 || self.MaxControlBytes == 0 || self.MaxControlBytes > maxHeadEMAStoreV2Bytes {
		return errors.New("bounded head EMA limits must be explicit and finite")
	}
	return nil
}

// One call owns the tokenizer and budget; no candidate-selected schema,
// recursive interface field, normalization, or external callback is involved.
type headEMAStoreV2WireAdmission struct {
	ctx       context.Context
	decoder   *json.Decoder
	limits    HeadEMAStoreV2Limits
	remaining uint64
}

// Division before multiplication prevents budget overflow on hostile lengths.
func (self *headEMAStoreV2WireAdmission) charge(count, width uint64) error {
	if width == 0 || count > self.remaining/width {
		return errors.New("bounded head EMA control allowance exceeded")
	}
	self.remaining -= count * width
	return nil
}

// A schema-limited walk rejects duplicate/unknown keys, wrong primitive types,
// malformed fixed arrays and excessive row counts before the typed allocation.
// Recursion follows only the fixed local Go schema, with a hard depth guard.
func (self *headEMAStoreV2WireAdmission) value(valueType reflect.Type, depth uint, rationalText bool) error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if depth > 8 {
		return errors.New("bounded head EMA JSON exceeds its fixed schema depth")
	}
	token, err := self.decoder.Token()
	if err != nil {
		return err
	}
	if valueType.Kind() == reflect.Pointer {
		if token == nil {
			return nil
		}
		valueType = valueType.Elem()
		if err := self.charge(1, uint64(valueType.Size())); err != nil {
			return err
		}
	}
	switch valueType.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') || valueType.NumField() > 64 {
			return errors.New("bounded head EMA object differs from its schema")
		}
		var seen uint64
		for self.decoder.More() {
			if err := self.ctx.Err(); err != nil {
				return err
			}
			fieldToken, err := self.decoder.Token()
			if err != nil {
				return err
			}
			fieldName, ok := fieldToken.(string)
			if !ok {
				return errors.New("bounded head EMA object key is not a string")
			}
			index := -1
			for candidate := 0; candidate < valueType.NumField(); candidate++ {
				field := valueType.Field(candidate)
				name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
				if name == "" {
					name = field.Name
				}
				if name == fieldName {
					index = candidate
					break
				}
			}
			if index < 0 || seen&(uint64(1)<<uint(index)) != 0 {
				return errors.New("bounded head EMA object has an unknown or duplicate field")
			}
			seen |= uint64(1) << uint(index)
			field := valueType.Field(index)
			isRationalText := field.Type.Kind() == reflect.String && (valueType == reflect.TypeFor[headEMAEntry]() || valueType == reflect.TypeFor[RationalJSON]())
			if err := self.value(field.Type, depth+1, isRationalText); err != nil {
				return err
			}
		}
		end, err := self.decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.Join(errors.New("bounded head EMA object is incomplete"), err)
		}
	case reflect.Slice, reflect.Array:
		if token == nil && valueType.Kind() == reflect.Slice {
			return nil
		}
		if token != json.Delim('[') {
			return errors.New("bounded head EMA array differs from its schema")
		}
		count := uint64(0)
		for self.decoder.More() {
			if err := self.ctx.Err(); err != nil {
				return err
			}
			if valueType.Kind() == reflect.Array {
				if count >= uint64(valueType.Len()) {
					return errors.New("bounded head EMA fixed array is too long")
				}
			} else {
				if count >= self.limits.MaxEntries {
					return errors.New("bounded head EMA row census exceeds its bound")
				}
				if err := self.charge(1, uint64(valueType.Elem().Size())); err != nil {
					return err
				}
				// The longest formatted fleet key is 156 bytes. Reserve the
				// retained entry or consistency index before constructing it.
				indexBytes := uint64(156) + uint64(reflect.TypeFor[string]().Size())
				if valueType.Elem() == reflect.TypeFor[headEMAEntry]() {
					indexBytes += uint64(reflect.TypeFor[headEMAEntry]().Size())
				} else if valueType.Elem() == reflect.TypeFor[HeadEMAMeasurement]() {
					indexBytes += uint64(reflect.TypeFor[RationalJSON]().Size())
				} else {
					return errors.New("bounded head EMA has an unsupported row schema")
				}
				// Fixed rational objects and arithmetic temporaries are reserved
				// per row; decimal limbs receive an additional reserve below.
				if err := self.charge(1, indexBytes+2048); err != nil {
					return err
				}
			}
			if err := self.value(valueType.Elem(), depth+1, false); err != nil {
				return err
			}
			count++
		}
		end, err := self.decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.Join(errors.New("bounded head EMA array is incomplete"), err)
		}
		if valueType.Kind() == reflect.Array && count != uint64(valueType.Len()) {
			return errors.New("bounded head EMA fixed array is too short")
		}
	case reflect.String:
		value, ok := token.(string)
		if !ok {
			return errors.New("bounded head EMA string differs from its schema")
		}
		width := uint64(1)
		// A decimal digit needs fewer than four bits. This conservative
		// reserve covers decoded text and the per-row rational operations
		// before SetString, normalization or exact fold verification runs.
		if rationalText {
			width += 32
		}
		if err := self.charge(uint64(len(value)), width); err != nil {
			return err
		}
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return errors.New("bounded head EMA boolean differs from its schema")
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint64:
		number, ok := token.(json.Number)
		if !ok {
			return errors.New("bounded head EMA integer differs from its schema")
		}
		if _, err := strconv.ParseUint(string(number), 10, valueType.Bits()); err != nil {
			return err
		}
	default:
		return errors.New("bounded head EMA has an unsupported wire type")
	}
	return self.ctx.Err()
}

// Admission runs before json.Unmarshal can allocate the complete record
// slices and before any decimal text reaches a big integer parser.
func admitHeadEMAStoreV2Wire(ctx context.Context, encoded []byte, limits HeadEMAStoreV2Limits) error {
	if ctx == nil {
		return errors.New("bounded head EMA context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := limits.validate(); err != nil {
		return err
	}
	if uint64(len(encoded)) > limits.MaxFileBytes {
		return errors.New("bounded head EMA file exceeds its byte allowance")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	admission := &headEMAStoreV2WireAdmission{ctx: ctx, decoder: decoder, limits: limits, remaining: limits.MaxControlBytes}
	if err := admission.charge(1, headEMAStoreV2FixedControlBytes()); err != nil {
		return err
	}
	if err := admission.value(reflect.TypeFor[headEMAFile](), 0, false); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.Join(errors.New("bounded head EMA file has trailing JSON"), err)
	}
	return ctx.Err()
}

// The hook marks the real arithmetic boundary and cannot supply decoded
// state or a verification verdict. Returned state owns all of its input data.
func decodeHeadEMAStoreV2(ctx context.Context, path string, encoded []byte, limits HeadEMAStoreV2Limits, beforeRational func() error) (result *HeadEMAStore, resultErr error) {
	if ctx == nil {
		return nil, errors.New("bounded head EMA context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if uint64(len(path)) >= limits.MaxControlBytes {
		return nil, errors.New("bounded head EMA path exceeds its control allowance")
	}
	limits.MaxControlBytes -= uint64(len(path))
	if err := admitHeadEMAStoreV2Wire(ctx, encoded, limits); err != nil {
		return nil, err
	}
	var file headEMAFile
	if err := json.Unmarshal(encoded, &file); err != nil {
		return nil, err
	}
	if file.Schema != headEMASchemaV1 && file.Schema != headEMASchemaV2 {
		return nil, errors.New("unsupported bounded head EMA schema")
	}
	if file.Schema == headEMASchemaV2 && ((file.LastSubnetEpoch == nil) != (file.LastAlpha == nil) || (file.LastSubnetEpoch != nil && file.LastFold == nil)) {
		return nil, errors.New("persisted head EMA fold metadata is partial")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if beforeRational != nil {
		if err := beforeRational(); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := &HeadEMAStore{path: path, values: make(map[string]headEMAEntry, len(file.Entries))}
	priorKey := ""
	for _, entry := range file.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := entry.Key.String()
		if priorKey != "" && key <= priorKey {
			return nil, errors.New("persisted head EMA entries are not strictly ordered")
		}
		value, err := decodeRationalJSON(RationalJSON{Numerator: entry.Numerator, Denominator: entry.Denominator})
		if err != nil || value.Sign() <= 0 {
			return nil, errors.Join(errors.New("persisted head EMA entry is not canonical positive state"), err)
		}
		store.values[key] = entry
		priorKey = key
	}
	if file.Schema == headEMASchemaV2 && file.LastSubnetEpoch != nil {
		if err := verifyHeadEMAFold(file.LastFold, *file.LastAlpha); err != nil {
			return nil, fmt.Errorf("persisted head EMA fold: %w", err)
		}
		lastValues := make(map[string]RationalJSON, len(file.LastFold))
		for _, record := range file.LastFold {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// Full verification above proves the unique canonical zero form.
			if record.Next.Numerator != "0" {
				lastValues[record.Key.String()] = record.Next
			}
		}
		if len(lastValues) != len(store.values) {
			return nil, errors.New("persisted head EMA entries differ from the last fold")
		}
		for key, entry := range store.values {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if lastValues[key] != (RationalJSON{Numerator: entry.Numerator, Denominator: entry.Denominator}) {
				return nil, errors.New("persisted head EMA entry differs from the last fold")
			}
		}
		store.lastSubnetEpoch, store.lastAlpha, store.lastFold = file.LastSubnetEpoch, file.LastAlpha, file.LastFold
	}
	return store, nil
}
