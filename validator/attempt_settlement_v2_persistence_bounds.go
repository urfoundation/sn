//go:build linux || darwin

package validator

// Runtime metadata has independent caller-owned byte allowances. Count the
// complete JSON representation before the encoder allocates its output; a
// limited writer alone would not bound encoding/json's internal buffer.

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/urnetwork/connect"
)

// Neither allowance is selected by a snapshot, journal or implicit default.
// The journal allowance includes all base64-encoded original/pre/post images.
type AttemptSettlementRuntimeV2PersistenceBounds struct {
	MaxSnapshotBytes uint64
	MaxJournalBytes uint64
}

// Both size+1 reads and encoder lengths must fit the platform's signed int.
func validateAttemptSettlementV2MetadataLimit(limit uint64) error {
	if limit == 0 || limit > uint64(^uint(0)>>1)-1 || limit > uint64(^uint64(0)>>1)-1 {
		return errors.New("compact persistence byte allowance is zero or overflows size+1")
	}
	return nil
}

// Validate even unused allowances before namespace admission or callbacks.
func (self AttemptSettlementRuntimeV2PersistenceBounds) validate() error {
	return errors.Join(validateAttemptSettlementV2MetadataLimit(self.MaxSnapshotBytes), validateAttemptSettlementV2MetadataLimit(self.MaxJournalBytes))
}

// One count owns its budget and pointer ancestry; it never invokes a candidate
// marshaler or allocates a slice proportional to a map's unadmitted census.
type attemptSettlementV2JSONCount struct {
	ctx context.Context
	limit uint64
	used uint64
	indent bool
	ancestors map[any]bool
}

// Overflow is refused before adding even one byte to the reservation.
func (self *attemptSettlementV2JSONCount) add(size uint64) error {
	if err := self.ctx.Err(); err != nil { return err }
	if size > self.limit-self.used { return errors.New("compact JSON output exceeds its independent byte allowance") }
	self.used += size
	return nil
}

// encoding/json escapes HTML, control characters, invalid UTF-8 and the two
// JavaScript line separators. UUIDs have their separately fixed wire width.
func (self *attemptSettlementV2JSONCount) string(value string) error {
	if err := self.add(2); err != nil { return err }
	for offset := 0; offset < len(value); {
		character, width := utf8.DecodeRuneInString(value[offset:])
		size := uint64(width)
		switch {
		case character == '"' || character == '\\' || character == '\b' || character == '\f' || character == '\n' || character == '\r' || character == '\t': size = 2
		case character < 0x20 || character == '<' || character == '>' || character == '&' || character == '\u2028' || character == '\u2029' || character == utf8.RuneError && width == 1: size = 6
		}
		if err := self.add(size); err != nil { return err }
		offset += width
	}
	return nil
}

// Indentation is counted at the exact parent depth, including closing lines.
func (self *attemptSettlementV2JSONCount) line(depth uint64) error {
	if !self.indent { return nil }
	if depth > (self.limit-self.used)/2 { return errors.New("compact JSON indentation exceeds its byte allowance") }
	return self.add(1+2*depth)
}

// These are encoding/json's omitempty kinds, not Go's broader zero values.
func emptyAttemptSettlementV2JSONValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String: return value.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer: return value.IsZero()
	}
	return false
}

// Only the existing plain metadata schema and fixed UUID marshaler are
// supported. A future custom encoder must add an explicit bounded contract.
func (self *attemptSettlementV2JSONCount) value(value reflect.Value, depth uint64) error {
	if err := self.ctx.Err(); err != nil { return err }
	if !value.IsValid() { return self.add(4) }
	if value.Type() == reflect.TypeFor[connect.Id]() { return self.add(38) }
	if value.Type() == reflect.TypeFor[*connect.Id]() { if value.IsNil() { return self.add(4) }; return self.add(38) }
	if value.Type() == reflect.TypeFor[attemptSettlementV2BorrowedEMA]() || value.Type() == reflect.TypeFor[attemptSettlementV2BorrowedPPM]() || value.Type() == reflect.TypeFor[attemptSettlementV2BorrowedWindow]() { return self.borrowedMap(value, depth, false) }
	if value.Type() == reflect.TypeFor[attemptSettlementV2BorrowedEgress]() { return self.borrowedMap(value, depth, true) }
	marshaler := reflect.TypeFor[json.Marshaler]()
	textMarshaler := reflect.TypeFor[encoding.TextMarshaler]()
	if value.Type() == reflect.TypeFor[json.Number]() || value.Type().Implements(marshaler) || reflect.PointerTo(value.Type()).Implements(marshaler) || value.Type().Implements(textMarshaler) || reflect.PointerTo(value.Type()).Implements(textMarshaler) {
		return fmt.Errorf("compact JSON counting refuses unbounded custom encoder %s", value.Type())
	}
	if value.Kind() == reflect.Pointer && value.IsNil() { return self.add(4) }
	var number [32]byte
	switch value.Kind() {
	case reflect.Pointer:
		pointer := value.Interface()
		if self.ancestors[pointer] { return errors.New("compact JSON metadata contains a pointer cycle") }
		self.ancestors[pointer] = true
		err := self.value(value.Elem(), depth)
		delete(self.ancestors, pointer)
		return err
	case reflect.Bool:
		if value.Bool() { return self.add(4) }; return self.add(5)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return self.add(uint64(len(strconv.AppendInt(number[:0], value.Int(), 10))))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return self.add(uint64(len(strconv.AppendUint(number[:0], value.Uint(), 10))))
	case reflect.Float32, reflect.Float64:
		floating := value.Float()
		if math.IsNaN(floating) || math.IsInf(floating, 0) { return errors.New("compact JSON metadata contains a non-finite number") }
		bits, absolute, format := value.Type().Bits(), math.Abs(floating), byte('f')
		if bits == 32 {
			if absolute != 0 && (float32(absolute) < 1e-6 || float32(absolute) >= 1e21) { format = 'e' }
		} else if absolute != 0 && (absolute < 1e-6 || absolute >= 1e21) { format = 'e' }
		encoded := strconv.AppendFloat(number[:0], floating, format, -1, bits)
		size := len(encoded)
		if format == 'e' && size >= 4 && encoded[size-4] == 'e' && encoded[size-3] == '-' && encoded[size-2] == '0' { size-- }
		return self.add(uint64(size))
	case reflect.String:
		return self.string(value.String())
	case reflect.Slice:
		if value.IsNil() { return self.add(4) }
		if value.Type().Elem().Kind() == reflect.Uint8 {
			element := value.Type().Elem()
			if element.Implements(marshaler) || reflect.PointerTo(element).Implements(marshaler) || element.Implements(textMarshaler) || reflect.PointerTo(element).Implements(textMarshaler) { return errors.New("compact JSON byte slice has an unsupported custom element encoder") }
			length := uint64(value.Len())
			if length > self.limit || length > (^uint64(0)-2)/4 { return errors.New("compact JSON base64 input exceeds its byte allowance") }
			return self.add(2+4*((length+2)/3))
		}
		fallthrough
	case reflect.Array:
		if err := self.add(2); err != nil { return err }
		if uint64(value.Len()) > self.limit-self.used { return errors.New("compact JSON array census exceeds its byte allowance") }
		for index := 0; index < value.Len(); index++ {
			if index != 0 { if err := self.add(1); err != nil { return err } }
			if err := self.line(depth+1); err != nil { return err }
			if err := self.value(value.Index(index), depth+1); err != nil { return err }
		}
		if value.Len() != 0 { return self.line(depth) }; return nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String { return errors.New("compact JSON metadata has an unsupported map key") }
		if value.IsNil() { return self.add(4) }
		if err := self.add(2); err != nil { return err }
		if uint64(value.Len()) > self.limit-self.used { return errors.New("compact JSON map census exceeds its byte allowance") }
		iterator, count := value.MapRange(), 0
		for iterator.Next() {
			if count != 0 { if err := self.add(1); err != nil { return err } }
			if err := self.line(depth+1); err != nil { return err }
			if err := self.string(iterator.Key().String()); err != nil { return err }
			colon := uint64(1); if self.indent { colon++ }
			if err := self.add(colon); err != nil { return err }
			if err := self.value(iterator.Value(), depth+1); err != nil { return err }
			count++
		}
		if count != 0 { return self.line(depth) }; return nil
	case reflect.Struct:
		if err := self.add(2); err != nil { return err }
		count := 0
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if field.PkgPath != "" { continue }
			if field.Anonymous { return errors.New("compact JSON metadata has an unsupported embedded field") }
			tag := field.Tag.Get("json")
			if tag == "-" { continue }
			name, options, _ := strings.Cut(tag, ",")
			if options != "" && options != "omitempty" { return errors.New("compact JSON metadata has an unsupported field option") }
			if options == "omitempty" && emptyAttemptSettlementV2JSONValue(value.Field(index)) { continue }
			if name == "" { name = field.Name }
			if count != 0 { if err := self.add(1); err != nil { return err } }
			if err := self.line(depth+1); err != nil { return err }
			if err := self.string(name); err != nil { return err }
			colon := uint64(1); if self.indent { colon++ }
			if err := self.add(colon); err != nil { return err }
			if err := self.value(value.Field(index), depth+1); err != nil { return err }
			count++
		}
		if count != 0 { return self.line(depth) }; return nil
	default:
		return fmt.Errorf("compact JSON metadata has unsupported kind %s", value.Kind())
	}
}

// Newline, pretty-print expansion and all nested payloads enter this reserve.
func countAttemptSettlementV2JSON(ctx context.Context, value any, limit uint64, indent, newline bool) (uint64, error) {
	if ctx == nil { return 0, errors.New("compact JSON counting context is nil") }
	if err := validateAttemptSettlementV2MetadataLimit(limit); err != nil { return 0, err }
	count := attemptSettlementV2JSONCount{ctx: ctx, limit: limit, indent: indent, ancestors: map[any]bool{}}
	if newline { if err := count.add(1); err != nil { return 0, err } }
	if err := count.value(reflect.ValueOf(value), 0); err != nil { return 0, err }
	return count.used, nil
}

// The ordinary encoder runs only after the complete exact output was admitted.
func marshalAttemptSettlementV2JSON(ctx context.Context, value any, limit uint64, indent, newline bool) ([]byte, error) {
	size, err := countAttemptSettlementV2JSON(ctx, value, limit, indent, newline)
	if err != nil { return nil, err }
	var encoded []byte
	if indent { encoded, err = json.MarshalIndent(value, "", "  ") } else { encoded, err = json.Marshal(value) }
	if err != nil { return nil, err }
	if newline { encoded = append(encoded, '\n') }
	if uint64(len(encoded)) != size { return nil, errors.New("compact JSON encoding differs from its admitted exact size") }
	if err := ctx.Err(); err != nil { return nil, err }
	return encoded, nil
}

// The persisted v1/v6 representation remains byte-for-byte the existing codec.
func encodeAttemptSettlementV2Stats(ctx context.Context, snapshot statsSnapshot, limit uint64) ([]byte, error) {
	size, err := countAttemptSettlementV2JSON(ctx, snapshot, limit, true, true)
	if err != nil { return nil, err }
	encoded, err := encodeStatsSnapshot(snapshot)
	if err != nil { return nil, err }
	if uint64(len(encoded)) != size { return nil, errors.New("compact snapshot encoding differs from its admitted exact size") }
	if err := ctx.Err(); err != nil { return nil, err }
	return encoded, nil
}

// Admit every retained live image before replay, sealer or filesystem hooks.
// Private postimages are admitted separately before their first encoding.
func admitAttemptSettlementV2LiveSnapshots(ctx context.Context, batch *attemptSettlementV2Owners) error {
	if err := batch.persistence.validate(); err != nil { return err }
	for _, stats := range batch.candidates {
		if _, err := countAttemptSettlementV2Engine(ctx, stats, batch.persistence.MaxSnapshotBytes); err != nil { return err }
	}
	return ctx.Err()
}
