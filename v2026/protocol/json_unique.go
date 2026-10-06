// JSON admission rejects ambiguous keys before an RPC result is decoded.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const maximumJsonAdmissionDepth = 64

// Check every object in one complete JSON document. Callers bound input bytes
// before this parse.
func ValidateUniqueJsonKeys(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	foldKey := func(key string) string {
		var folded strings.Builder
		for _, character := range key {
			minimum := character
			for next := unicode.SimpleFold(character); next != character; next = unicode.SimpleFold(next) {
				if next < minimum {
					minimum = next
				}
			}
			folded.WriteRune(minimum)
		}
		return folded.String()
	}
	var readValue func(int) error
	readValue = func(depth int) error {
		if depth > maximumJsonAdmissionDepth {
			return fmt.Errorf("JSON nesting exceeds %d levels", maximumJsonAdmissionDepth)
		}
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("invalid JSON value: %w", err)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		if delimiter == '{' {
			seenKeys := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("invalid JSON object key: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("JSON object key is not a string")
				}
				// encoding/json's struct matcher accepts case variants of one
				// field, so they are ambiguous at an RPC envelope boundary.
				foldedKey := foldKey(key)
				if _, exists := seenKeys[foldedKey]; exists {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seenKeys[foldedKey] = struct{}{}
				if err := readValue(depth + 1); err != nil {
					return err
				}
			}
		} else if delimiter == '[' {
			for decoder.More() {
				if err := readValue(depth + 1); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
		closing, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("unterminated JSON collection: %w", err)
		}
		if closing != json.Delim('}') && closing != json.Delim(']') {
			return errors.New("invalid JSON collection terminator")
		}
		if delimiter == '{' && closing != json.Delim('}') || delimiter == '[' && closing != json.Delim(']') {
			return errors.New("mismatched JSON collection terminator")
		}
		return nil
	}
	if err := readValue(0); err != nil {
		return err
	}
	_, err := decoder.Token()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("invalid trailing JSON data: %w", err)
	}
	return errors.New("multiple JSON values in one response")
}
