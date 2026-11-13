// Replay retained test2json evidence without compiling, running a body,
// filtering events or claiming a new source/binary qualification fence.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Matching is a statement about the supplied retained evidence only.
type replayResult struct {
	Status       string        `json:"status"`
	Package      string        `json:"package,omitempty"`
	Verification *eventSummary `json:"verification,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// Inputs are the original physical event and binary-exit files plus exact
// declared outcomes/literals. The enclosing capture still owns their hashes
// and all original source, binary, converter and process-lifecycle receipts.
func replayEventsFiles(args []string) (replayResult, error) {
	if len(args) != 5 {
		return replayResult{}, errors.New("usage: qualification replay EVENTS OUTCOMES LITERALS PACKAGE BODY_EXIT")
	}
	eventPath, outcomes, literals, packagePath, exitPath := args[0], args[1], args[2], args[3], args[4]
	if len(packagePath) > metadataLimit || !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(packagePath) || strings.HasPrefix(packagePath, "/") || filepath.Clean(packagePath) != packagePath || packagePath == "." || packagePath == ".." || strings.HasPrefix(packagePath, "../") {
		return replayResult{}, errors.New("invalid replay package identity")
	}
	expected, err := expectedInputs(outcomes, literals)
	if err != nil {
		return replayResult{}, err
	}
	encodedExit, err := readBounded(exitPath, 16)
	if err != nil {
		return replayResult{}, err
	}
	actualExit, err := strconv.Atoi(strings.TrimSuffix(string(encodedExit), "\n"))
	if err != nil || actualExit < 0 || actualExit > 255 || string(encodedExit) != strconv.Itoa(actualExit)+"\n" {
		return replayResult{}, errors.New("binary exit receipt is not a canonical bounded decimal")
	}
	events, info, err := openRegular(eventPath)
	if err != nil {
		return replayResult{}, err
	}
	if info.Size() > capturedJSONLimit {
		return replayResult{}, errors.Join(errors.New("replay event file exceeds captured Json bound"), events.Close())
	}
	checked, verifyErr := verifyEvents(events, expected, packagePath, actualExit)
	closeErr := errors.Join(sameRegular(eventPath, events, info), events.Close())
	if err := errors.Join(verifyErr, closeErr); err != nil {
		return replayResult{}, err
	}
	return replayResult{Status: "matched", Package: packagePath, Verification: &checked}, nil
}

// This is the actual standalone command dispatch used by main, with injectable
// output owners so admission/refusal and write failures are tested without a
// test subprocess or a second capture of the original qualification body.
func runReplay(args []string, stdout, stderr io.Writer) int {
	if stdout == nil || stderr == nil {
		return 1
	}
	result, err := replayEventsFiles(args)
	if err != nil {
		result = replayResult{Status: "refused", Error: shortError(err)}
		_, _ = fmt.Fprintln(stderr, result.Error)
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr == nil {
		encoded = append(encoded, '\n')
		count, writeErr := stdout.Write(encoded)
		encodeErr = writeErr
		if encodeErr == nil && count != len(encoded) {
			encodeErr = io.ErrShortWrite
		}
	}
	if encodeErr != nil {
		_, _ = fmt.Fprintln(stderr, encodeErr)
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}
