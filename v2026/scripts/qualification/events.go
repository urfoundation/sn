// Verify bounded test2json events against exact root-owned expected outcomes.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type eventSummary struct {
	Roots            int `json:"roots"`
	Passed           int `json:"passed"`
	ExpectedFailures int `json:"expected_failures"`
	BinaryExit       int `json:"binary_exit"`
	Events           int `json:"events"`
	Bytes            int `json:"bytes"`
}

func verifyEvents(source io.Reader, expected expectedSuite, packagePath string, actualExit int) (eventSummary, error) {
	result := eventSummary{Roots: len(expected.Roots), ExpectedFailures: len(expected.Markers), BinaryExit: actualExit}
	wantedExit, terminal := 0, "pass"
	if len(expected.Markers) > 0 {
		wantedExit, terminal = 1, "fail"
	}
	if actualExit != wantedExit {
		return result, errors.New("actual binary exit differs from declared outcome")
	}
	records := bufio.NewReaderSize(source, metadataLimit+1)
	states, tails, matched := map[string]string{}, map[string]string{}, map[string]bool{}
	started, ended, outcomes := false, false, 0
	for {
		line, readErr := records.ReadSlice('\n')
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		result.Events++
		result.Bytes += len(line)
		if readErr != nil || len(line) > metadataLimit || result.Events > 256*1024 || result.Bytes > 64*metadataLimit {
			return result, errors.Join(errors.New("event stream exceeds bounds or ends in a partial event"), readErr)
		}
		var event map[string]json.RawMessage
		if err := decodeJSON(line, &event); err != nil {
			return result, err
		}
		text := func(name string) (string, error) {
			raw, ok := event[name]
			if !ok {
				return "", nil
			}
			var value *string
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", err
			}
			if value == nil {
				return "", errors.New("null event text")
			}
			return *value, nil
		}
		action, err := text("Action")
		if err != nil {
			return result, err
		}
		root, err := text("Test")
		if err != nil {
			return result, err
		}
		pkg, err := text("Package")
		if err != nil || pkg != packagePath || ended {
			return result, errors.New("event package or terminal boundary differs")
		}
		if failed, err := text("FailedBuild"); err != nil || failed != "" {
			return result, errors.New("failed or malformed build event")
		}
		if !started {
			if action != "start" || root != "" {
				return result, errors.New("missing package start")
			}
			started = true
			continue
		}
		if action == "start" {
			return result, errors.New("duplicate package start")
		}
		if root != "" && expected.Outcomes[root] == "" {
			return result, fmt.Errorf("unexpected root or subtest %s", root)
		}
		if action == "output" {
			output, err := text("Output")
			if err != nil || event["Output"] == nil {
				return result, errors.New("invalid output event")
			}
			if root != "" {
				if states[root] != "running" && states[root] != "paused" {
					return result, errors.New("output outside live root")
				}
				if literal := expected.Markers[root]; literal != "" && !matched[root] {
					combined := tails[root] + output
					matched[root] = strings.Contains(combined, literal)
					if len(combined) >= len(literal) {
						combined = combined[len(combined)-len(literal)+1:]
					}
					tails[root] = combined
				}
			}
			continue
		}
		if root == "" {
			if action != terminal || outcomes != len(expected.Roots) {
				return result, errors.New("incomplete or unexpected package outcome")
			}
			for name := range expected.Markers {
				if !matched[name] {
					return result, errors.New("missing root-bound failure literal")
				}
			}
			ended = true
			continue
		}
		switch action {
		case "run":
			if states[root] != "" {
				return result, errors.New("duplicate root start")
			}
			states[root] = "running"
		case "pause":
			if states[root] != "running" {
				return result, errors.New("pause outside running root")
			}
			states[root] = "paused"
		case "cont":
			if states[root] != "paused" {
				return result, errors.New("continue outside paused root")
			}
			states[root] = "running"
		case "pass", "fail":
			if states[root] != "running" || expected.Outcomes[root] != action {
				return result, errors.New("root terminal state differs")
			}
			states[root] = "done"
			outcomes++
			if action == "pass" {
				result.Passed++
			}
		default:
			return result, errors.New("unexpected test action")
		}
	}
	if !started || !ended {
		return result, errors.New("missing complete package boundary")
	}
	return result, nil
}
