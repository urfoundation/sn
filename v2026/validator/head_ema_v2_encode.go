package validator

// The existing indented schema has one exact streaming size pass before its
// sole output allocation. This preserves JSON bytes without MarshalIndent's
// unbounded intermediate buffer. The second pass cannot exceed the first.

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Counting and encoding share the same fixed local schema traversal.
type headEMAStoreV2JSONWriter struct {
	ctx       context.Context
	limit     uint64
	used      uint64
	encoded   []byte
	countOnly bool
}

// Every emission checks the original explicit allowance before append.
func (self *headEMAStoreV2JSONWriter) emit(value string) error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if self.used > self.limit || uint64(len(value)) > self.limit-self.used {
		return errors.New("bounded head EMA encoded output exceeds its file allowance")
	}
	self.used += uint64(len(value))
	if !self.countOnly {
		self.encoded = append(self.encoded, value...)
	}
	return nil
}

// Indentation is fixed-depth and cannot allocate a repeated-string buffer.
func (self *headEMAStoreV2JSONWriter) indent(depth int) error {
	if err := self.emit("\n"); err != nil {
		return err
	}
	for range depth {
		if err := self.emit("  "); err != nil {
			return err
		}
	}
	return nil
}

// Match encoding/json's default HTML-safe string grammar, including invalid
// UTF-8 replacement and U+2028/U+2029. Runtime-generated decimals are ASCII,
// but the codec remains exact for existing string fields and independent tests.
func (self *headEMAStoreV2JSONWriter) quoted(value string) error {
	if err := self.emit("\""); err != nil {
		return err
	}
	for index := 0; index < len(value); {
		b := value[index]
		if b < utf8.RuneSelf {
			var escaped string
			switch b {
			case '"':
				escaped = "\\\""
			case '\\':
				escaped = "\\\\"
			case '\b':
				escaped = "\\b"
			case '\f':
				escaped = "\\f"
			case '\n':
				escaped = "\\n"
			case '\r':
				escaped = "\\r"
			case '\t':
				escaped = "\\t"
			case '<':
				escaped = "\\u003c"
			case '>':
				escaped = "\\u003e"
			case '&':
				escaped = "\\u0026"
			default:
				if b < 0x20 {
					const hex = "0123456789abcdef"
					bytes := [6]byte{'\\', 'u', '0', '0', hex[b>>4], hex[b&15]}
					escaped = string(bytes[:])
				} else {
					escaped = value[index : index+1]
				}
			}
			if err := self.emit(escaped); err != nil {
				return err
			}
			index++
			continue
		}
		runeValue, width := utf8.DecodeRuneInString(value[index:])
		if runeValue == utf8.RuneError && width == 1 {
			if err := self.emit("\\ufffd"); err != nil {
				return err
			}
		} else if runeValue == '\u2028' {
			if err := self.emit("\\u2028"); err != nil {
				return err
			}
		} else if runeValue == '\u2029' {
			if err := self.emit("\\u2029"); err != nil {
				return err
			}
		} else if err := self.emit(value[index : index+width]); err != nil {
			return err
		}
		index += width
	}
	return self.emit("\"")
}

// Only the finite existing headEMAFile type graph is supported; no interface,
// map, user-selected marshaler or candidate schema can add unbounded work.
func (self *headEMAStoreV2JSONWriter) value(value reflect.Value, depth int) error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if depth > 8 {
		return errors.New("bounded head EMA output exceeds fixed schema depth")
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return self.emit("null")
		}
		return self.value(value.Elem(), depth)
	}
	switch value.Kind() {
	case reflect.Struct:
		if err := self.emit("{"); err != nil {
			return err
		}
		count := 0
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" {
				name = field.Name
			}
			child := value.Field(index)
			if options == "omitempty" {
				if child.Kind() != reflect.Pointer {
					return errors.New("bounded head EMA output has an unknown omission rule")
				}
				if child.IsNil() {
					continue
				}
			}
			if count != 0 {
				if err := self.emit(","); err != nil {
					return err
				}
			}
			if err := self.indent(depth + 1); err != nil {
				return err
			}
			if err := self.quoted(name); err != nil {
				return err
			}
			if err := self.emit(": "); err != nil {
				return err
			}
			if err := self.value(child, depth+1); err != nil {
				return err
			}
			count++
		}
		if count != 0 {
			if err := self.indent(depth); err != nil {
				return err
			}
		}
		return self.emit("}")
	case reflect.Array, reflect.Slice:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return self.emit("null")
		}
		if err := self.emit("["); err != nil {
			return err
		}
		for index := 0; index < value.Len(); index++ {
			if index != 0 {
				if err := self.emit(","); err != nil {
					return err
				}
			}
			if err := self.indent(depth + 1); err != nil {
				return err
			}
			if err := self.value(value.Index(index), depth+1); err != nil {
				return err
			}
		}
		if value.Len() != 0 {
			if err := self.indent(depth); err != nil {
				return err
			}
		}
		return self.emit("]")
	case reflect.String:
		return self.quoted(value.String())
	case reflect.Bool:
		if value.Bool() {
			return self.emit("true")
		}
		return self.emit("false")
	case reflect.Uint8, reflect.Uint16, reflect.Uint64:
		var bytes [20]byte
		number := strconv.AppendUint(bytes[:0], value.Uint(), 10)
		return self.emit(string(number))
	default:
		return errors.New("bounded head EMA output has an unsupported field type")
	}
}

// Exact file bytes are admitted before allocating them, while all input row
// owners were admitted before this phase. There is no append-newline spare cap.
func marshalHeadEMAStoreV2File(ctx context.Context, file headEMAFile, budget *headEMAStoreV2Budget, limit uint64) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("bounded head EMA context is nil")
	}
	if limit == 0 || limit > maxHeadEMAStoreV2Bytes {
		return nil, errors.New("bounded head EMA output allowance is invalid")
	}
	count := headEMAStoreV2JSONWriter{ctx: ctx, limit: limit, countOnly: true}
	if err := count.value(reflect.ValueOf(file), 0); err != nil {
		return nil, err
	}
	if err := count.emit("\n"); err != nil {
		return nil, err
	}
	if err := budget.charge(count.used, 1); err != nil {
		return nil, err
	}
	writer := headEMAStoreV2JSONWriter{ctx: ctx, limit: count.used, encoded: make([]byte, 0, int(count.used))}
	if err := writer.value(reflect.ValueOf(file), 0); err != nil {
		return nil, err
	}
	if err := writer.emit("\n"); err != nil {
		return nil, err
	}
	if writer.used != count.used || uint64(len(writer.encoded)) != count.used {
		return nil, errors.New("bounded head EMA exact output accounting changed")
	}
	return writer.encoded, ctx.Err()
}

// Sorting and row/header owners were reserved by the operation plan. Empty
// Entries remains null, while an epoch's empty LastFold remains [], exactly as
// in the old saveLocked implementation.
func encodeHeadEMAStoreV2File(ctx context.Context, store *HeadEMAStore, budget *headEMAStoreV2Budget, limit uint64) ([]byte, error) {
	keys := make([]string, 0, len(store.values))
	for key := range store.values {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	file := headEMAFile{Schema: headEMASchemaV2, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if len(keys) != 0 {
		file.Entries = make([]headEMAEntry, 0, len(keys))
	}
	for _, key := range keys {
		file.Entries = append(file.Entries, store.values[key])
	}
	if store.lastSubnetEpoch != nil {
		epoch, alpha := *store.lastSubnetEpoch, *store.lastAlpha
		file.LastSubnetEpoch, file.LastAlpha = &epoch, &alpha
		file.LastFold = append([]HeadEMAMeasurement{}, store.lastFold...)
	}
	return marshalHeadEMAStoreV2File(ctx, file, budget, limit)
}
