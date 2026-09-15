// Capture ownership checks index the source once, retain exact shell boundaries,
// and reject each changed candidate independently of prior verification.
package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Count the actual complete-script matcher calls, including an invalid late
// owner. Reintroducing a per-owner scan fails without waiting for a timeout.
func TestProducerGateCaptureSelectionBoundsMetadataScriptScans(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	check := func(source string, valid bool) {
		calls, scannedBytes := 0, 0
		err := verifyReleaseGateCaptureMetadataIsolationWithScan(source, func(pattern *regexp.Regexp, input string) [][]int {
			calls++
			scannedBytes += len(input)
			if calls > 1 || scannedBytes > len(source) {
				t.Fatalf("whole-script declaration scan exceeded one input pass: calls=%d bytes=%d input=%d", calls, scannedBytes, len(source))
			}
			return pattern.FindAllStringSubmatchIndex(input, -1)
		})
		if calls != 1 || scannedBytes != len(source) {
			t.Fatalf("capture verification did not scan its exact candidate once: calls=%d bytes=%d input=%d", calls, scannedBytes, len(source))
		}
		if (err == nil) != valid {
			t.Fatalf("capture verification valid=%v: %v", valid, err)
		}
	}
	check(script, true)
	const command = `go test -race ./sim-testnet -run "$capture_metadata_tests" -count=1 -timeout 45m`
	if strings.Count(script, command) != 1 {
		t.Fatal("late metadata mutation lost its unique source")
	}
	check(strings.Replace(script, command, strings.Replace(command, "-count=1", "-count=0", 1), 1), false)
	check(script, true)
}

// Indexing must preserve declaration uniqueness, executable ancestry and the
// requirement that the actual call follows its definition.
func TestProducerGateCaptureSelectionRejectsAmbiguousIndexedDefinitions(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatal(err)
	}
	const declaration = "release_phase_capture_metadata() {\n"
	const start = "release_gate_start capture-metadata release_phase_capture_metadata"
	begin := strings.Index(script, declaration)
	if begin < 0 || strings.Count(script, declaration) != 1 || strings.Count(script, start) != 1 {
		t.Fatal("metadata source lost its unique declaration or call")
	}
	end := strings.Index(script[begin:], "\n}")
	if end < 0 {
		t.Fatal("metadata definition lost its closing line")
	}
	definition := script[begin : begin+end+2]
	for _, change := range []struct {
		name        string
		original    string
		replacement string
	}{
		{name: "duplicate definition", original: definition, replacement: definition + "\n" + definition},
		{name: "definition inside unrelated function", original: definition, replacement: "unused_capture_owner() {\n" + definition + "\n}"},
		{name: "commented declaration", original: declaration, replacement: "# " + declaration},
		{name: "declaration with arguments", original: declaration, replacement: "release_phase_capture_metadata(ignored) {\n"},
		{name: "registration inside unrelated function", original: start, replacement: "unused_capture_registration() {\n" + start + "\n}"},
		{name: "duplicate registration in unrelated function", original: start, replacement: start + "\nunused_capture_registration() {\n" + start + "\n}"},
		{name: "dead unindented call with different indented live call", original: start, replacement: "unused_capture_registration() {\n" + start + "\n}\n\t" + start},
		{name: "indented duplicate registration", original: start, replacement: start + "\n\t" + start},
	} {
		if err := verifyReleaseGateCaptureMetadataIsolation(strings.Replace(script, change.original, change.replacement, 1)); err == nil {
			t.Fatalf("capture index accepted %s", change.name)
		}
	}
	beforeDefinition := start + "\n" + strings.Replace(script, start, "", 1)
	if err := verifyReleaseGateCaptureMetadataIsolation(beforeDefinition); err == nil {
		t.Fatal("capture index accepted registration before its definition")
	}
}

// The bounded index retains harmless whitespace and comments accepted by the
// original grammar, including a call on the final line without a newline.
func TestProducerGateCaptureSelectionKeepsIndexedWhitespaceAndComments(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	const declaration = "release_phase_capture_metadata() {\n"
	const start = "release_gate_start capture-metadata release_phase_capture_metadata"
	if strings.Count(script, declaration) != 1 || strings.Count(script, start) != 1 {
		t.Fatal("metadata whitespace fixture lost its unique source")
	}
	script = strings.Replace(script, declaration, " \t"+declaration+"  # retained metadata boundary\n", 1)
	script = strings.Replace(script, start, start+"\t ", 1)
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatalf("capture index rejected accepted whitespace and comments: %v", err)
	}
	// The last of the ten capture admissions can end the input directly.
	// Unrelated final-fence syntax is outside this registry fixture.
	script = script[:strings.Index(script, start)+len(start)]
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatalf("capture index rejected final registration without newline: %v", err)
	}
}
