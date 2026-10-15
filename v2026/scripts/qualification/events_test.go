// Exact attribution controls for interleaved ordinary and causal event streams.
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func testEvent(action, root, output string) map[string]any {
	event := map[string]any{"Action": action, "Package": "example.com/validator"}
	if root != "" {
		event["Test"] = root
	}
	if action == "output" {
		event["Output"] = output
	}
	return event
}

func testEvents() []map[string]any {
	return []map[string]any{testEvent("start", "", ""), testEvent("run", "TestFailure", ""), testEvent("pause", "TestFailure", ""),
		testEvent("run", "TestOther", ""), testEvent("cont", "TestFailure", ""), testEvent("output", "TestFailure", "exact causal "),
		testEvent("output", "TestOther", "unrelated output\n"), testEvent("output", "TestFailure", "assertion\n"), testEvent("fail", "TestFailure", ""),
		testEvent("pass", "TestOther", ""), testEvent("output", "", "FAIL\n"), testEvent("fail", "", "")}
}

func testEncodedEvents(t *testing.T, events []map[string]any) []byte {
	t.Helper()
	var output bytes.Buffer
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(data)
		output.WriteByte('\n')
	}
	return output.Bytes()
}

func testExpectedEvents() expectedSuite {
	return expectedSuite{Roots: []string{"TestFailure", "TestOther"}, Outcomes: map[string]string{"TestFailure": "fail", "TestOther": "pass"}, Markers: map[string]string{"TestFailure": "exact causal assertion"}}
}

func TestQualificationEventsMatchInterleavedRootOwnedFailure(t *testing.T) {
	summary, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, testEvents())), testExpectedEvents(), "example.com/validator", 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Roots != 2 || summary.Passed != 1 || summary.ExpectedFailures != 1 {
		t.Fatalf("wrong census: %+v", summary)
	}
}

func TestQualificationEventsRejectOtherRootAndPackageFailureLiterals(t *testing.T) {
	for _, substitute := range []int{6, 10} {
		events := testEvents()
		events[5]["Output"], events[7]["Output"] = "different failure", "\n"
		events[substitute]["Output"] = "exact causal assertion\n"
		if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), testExpectedEvents(), "example.com/validator", 1); err == nil {
			t.Fatalf("accepted literal from event %d", substitute)
		}
	}
	events := testEvents()
	events[6]["Output"], events[7]["Output"] = "assertion\n", "different tail\n"
	if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), testExpectedEvents(), "example.com/validator", 1); err == nil {
		t.Fatal("combined failure literal across roots")
	}
}

func TestQualificationEventsRejectTimeoutCancellationAndMissingBinaryExit(t *testing.T) {
	for _, status := range []int{-1, 0, 2, 124, 125, 137, 143} {
		if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, testEvents())), testExpectedEvents(), "example.com/validator", status); err == nil {
			t.Fatalf("accepted exit %d as expected assertion failure", status)
		}
	}
}

func TestQualificationEventsRejectMissingUnexpectedAndRepeatedRoots(t *testing.T) {
	for _, mutation := range []string{"duplicate-run", "duplicate-outcome", "skip", "wrong-package", "unknown-root", "missing-end", "after-end"} {
		events := testEvents()
		switch mutation {
		case "duplicate-run":
			events[2] = testEvent("run", "TestFailure", "")
		case "duplicate-outcome":
			events[9] = testEvent("fail", "TestFailure", "")
		case "skip":
			events[9]["Action"] = "skip"
		case "wrong-package":
			events[5]["Package"] = "other/package"
		case "unknown-root":
			events[5]["Test"] = "TestUnexpected"
		case "missing-end":
			events = events[:len(events)-1]
		case "after-end":
			events = append(events, testEvent("output", "", "later"))
		}
		if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), testExpectedEvents(), "example.com/validator", 1); err == nil {
			t.Fatalf("accepted %s", mutation)
		}
	}
}

func TestQualificationEventsRejectDuplicateJSONRoutingKey(t *testing.T) {
	data := testEncodedEvents(t, testEvents())
	data = bytes.Replace(data, []byte(`"Test":"TestFailure"`), []byte(`"Test":"TestOther","Test":"TestFailure"`), 1)
	if _, err := verifyEvents(bytes.NewReader(data), testExpectedEvents(), "example.com/validator", 1); err == nil {
		t.Fatal("accepted duplicate routing key")
	}
}

func TestQualificationEventsBoundOversizedAndTruncatedRecords(t *testing.T) {
	for _, input := range []string{strings.Repeat("x", metadataLimit+1) + "\n", `{"Action":`, ""} {
		if _, err := verifyEvents(strings.NewReader(input), testExpectedEvents(), "example.com/validator", 1); err == nil {
			t.Fatal("accepted malformed stream")
		}
	}
}
