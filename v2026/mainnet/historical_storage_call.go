// A source-reviewed semantic ancestor selects an exact original storage host
// path. The engine authenticates each body, direct callee and leaf ABI.
package main

import "errors"

type historicalStorageCallsite struct {
	FunctionIndex      uint32                 `json:"function_index"`
	FunctionBodySha256 historicalReplayDigest `json:"function_body_sha256"`
	OffsetStart        uint32                 `json:"offset_start"`
	OffsetEnd          uint32                 `json:"offset_end"`
}
type historicalStorageCall struct {
	Operation string                      `json:"operation"`
	Path      []historicalStorageCallsite `json:"path"`
}

func validateHistoricalStorageCallRule(profile *historicalReplayObservationProfile, rule historicalReplayHookRule) error {
	call := rule.StorageCall
	if call == nil {
		return nil
	}
	if profile.Schema != historicalNativeProfileSchema || rule.HostSnapshot != nil || len(call.Path) == 0 || len(call.Path) > 8 || call.Operation != "get" && call.Operation != "set" && call.Operation != "append" {
		return errors.New("original storage call path exceeds its scope or bound")
	}
	for _, frame := range call.Path {
		if frame.FunctionBodySha256 == (historicalReplayDigest{}) || frame.OffsetStart >= frame.OffsetEnd {
			return errors.New("original storage call frame identity differs")
		}
	}
	last := call.Path[len(call.Path)-1]
	if last.FunctionIndex != rule.FunctionIndex || last.FunctionBodySha256 != rule.FunctionBodySha256 || last.OffsetStart != rule.OffsetStart || last.OffsetEnd != rule.OffsetEnd {
		return errors.New("original storage call semantic ancestor differs")
	}
	return nil
}

func historicalRuleMatchesAt(rule historicalReplayHookRule, stack []historicalReplayFrame, index int) bool {
	frame := stack[index]
	if rule.FunctionIndex != frame.FunctionIndex || frame.FunctionOffset < rule.OffsetStart || frame.FunctionOffset >= rule.OffsetEnd || rule.HostSnapshot != nil && index != 0 {
		return false
	}
	if call := rule.StorageCall; call != nil {
		if len(call.Path) == 0 || len(stack) < len(call.Path) || index != len(call.Path)-1 {
			return false
		}
		for offset, selected := range call.Path {
			actual := stack[offset]
			if selected.FunctionIndex != actual.FunctionIndex || actual.FunctionOffset < selected.OffsetStart || actual.FunctionOffset >= selected.OffsetEnd {
				return false
			}
		}
	}
	return true
}

// Two rules sharing a semantic caller are safe only if their exact stack
// prefixes are disjoint. A shorter common prefix is still ambiguous.
func historicalStoragePathsOverlap(left, right historicalReplayHookRule) bool {
	if left.StorageCall == nil || right.StorageCall == nil {
		return true
	}
	for index := 0; index < min(len(left.StorageCall.Path), len(right.StorageCall.Path)); index++ {
		a, b := left.StorageCall.Path[index], right.StorageCall.Path[index]
		if a.FunctionIndex != b.FunctionIndex || a.OffsetStart >= b.OffsetEnd || b.OffsetStart >= a.OffsetEnd {
			return false
		}
	}
	return true
}
