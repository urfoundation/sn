// Retained API binaries can log normal client cancellation as a warning.
// Only their exact structured handler record is lifecycle noise; subsequent
// joined storage/write causes remain independently blocking across scans.
package main

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Admit the old handler's complete typed diagnostic, never a generic context
// substring. Deadlines, mismatched hashes, other handlers and extra causes fail.
func processLogArtifactRequestCancellation(text string) bool {
	if !klogSeverity(text, 'W') {
		return false
	}
	const source = " sn_attempt_artifact_handlers.go:"
	position := strings.Index(text, source)
	if position < 0 {
		return false
	}
	line, message, found := strings.Cut(text[position+len(source):], "] [sn-attempt] artifact stream failed: ")
	number, err := strconv.ParseUint(line, 10, 32)
	if !found || err != nil || number == 0 || strconv.FormatUint(number, 10) != line {
		return false
	}
	fields := strings.SplitN(message, " ", 5)
	if len(fields) != 5 || fields[4] != "context=context canceled error=context canceled" {
		return false
	}
	if fields[0] != "kind=records" && fields[0] != "kind=proofs" && fields[0] != "kind=metadata" {
		return false
	}
	if !strings.HasPrefix(fields[1], "hash=0x") || !strings.HasPrefix(fields[2], "bytes=") {
		return false
	}
	hash := strings.TrimPrefix(fields[1], "hash=0x")
	decoded, err := hex.DecodeString(hash)
	if len(hash) != 64 || err != nil || len(decoded) != 32 || hash != strings.ToLower(hash) || hash == strings.Repeat("0", 64) {
		return false
	}
	count := strings.TrimPrefix(fields[2], "bytes=")
	value, err := strconv.ParseUint(count, 10, 64)
	if err != nil || strconv.FormatUint(value, 10) != count {
		return false
	}
	if !strings.HasPrefix(fields[3], "elapsed=") {
		return false
	}
	elapsed, err := time.ParseDuration(strings.TrimPrefix(fields[3], "elapsed="))
	return err == nil && elapsed >= 0
}

// A new logger-owned header ends the previous multiline error. Arbitrary
// unstructured continuation text cannot hide beneath a cancellation finding.
func processLogStructuredRecord(text string) bool {
	for _, severity := range []byte{'I', 'W', 'E', 'F'} {
		if klogSeverity(text, severity) {
			return true
		}
	}
	for _, severity := range []string{"debug", "info", "warn", "warning", "error", "fatal", "panic"} {
		if structuredLogSeverity(strings.ToLower(text), severity) {
			return true
		}
	}
	return false
}
