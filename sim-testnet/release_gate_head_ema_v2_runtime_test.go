package main

// Source-edge and selector controls retain the actual runtime tests. They
// are not substitutes for native persistence or full arithmetic execution.

import (
	"os"
	"regexp"
	"testing"
)

// All runtime roots and both guard roots must remain selected in real modes.
func TestProducerGateStateSelectionCoversHeadEMAStoreV2Runtime(t *testing.T) {
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil { t.Fatal(err) }
	selector, err := releaseConnectPolicySelectorAssignment(string(raw), "producer_tests")
	if err != nil { t.Fatal(err) }
	var sources []string
	for _, path := range []string{"../validator/head_ema_v2_runtime_test.go", "../validator/head_ema_v2_write_test.go"} {
		source, err := os.ReadFile(path)
		if err != nil { t.Fatal(err) }
		sources = append(sources, string(source))
	}
	if err := verifyReleaseSourceTestCoverage(selector, "^TestHeadEMAStoreV2Runtime", sources); err != nil { t.Fatal(err) }
	capture, err := releaseConnectPolicySelectorAssignment(string(raw), "capture_tests")
	if err != nil { t.Fatal(err) }
	for _, root := range []string{"TestProducerGateStateSelectionCoversHeadEMAStoreV2Runtime", "TestProducerGateStateSelectionPinsHeadEMAStoreV2RuntimeEdges"} {
		matched, err := regexp.MatchString(capture, root)
		if err != nil || !matched { t.Fatalf("capture omits %s: %v", root, err) }
	}
	t.Log("ema_runtime_control_completed: TestProducerGateStateSelectionCoversHeadEMAStoreV2Runtime")
}

// The constructor-selected legacy signatures may not regain pathname writes;
// the explicit context path still performs the real full fold and native calls.
func TestProducerGateStateSelectionPinsHeadEMAStoreV2RuntimeEdges(t *testing.T) {
	for _, edge := range []struct{ path, function, callee string }{
		{path: "../validator/head_ema.go", function: "Fold", callee: "FoldV2"},
		{path: "../validator/head_ema.go", function: "PreviewForEpoch", callee: "PreviewForEpochV2"},
		{path: "../validator/head_ema.go", function: "CommitForEpoch", callee: "CommitForEpochV2"},
		{path: "../validator/head_ema.go", function: "FoldForEpoch", callee: "FoldForEpochV2"},
		{path: "../validator/head_ema_v2_runtime.go", function: "runHeadEMAStoreV2", callee: "admitHeadEMAStoreV2WithLock"},
		{path: "../validator/head_ema_v2_runtime.go", function: "runHeadEMAStoreV2", callee: "foldWithLock"},
		{path: "../validator/head_ema_v2_runtime.go", function: "runHeadEMAStoreV2", callee: "rawHeadEMAInputs"},
		{path: "../validator/head_ema_v2_runtime.go", function: "runHeadEMAStoreV2", callee: "checkHeadEMAStoreV2Completed"},
		{path: "../validator/head_ema_v2_runtime.go", function: "runHeadEMAStoreV2", callee: "encodeHeadEMAStoreV2File"},
		{path: "../validator/head_ema_v2_runtime.go", function: "runHeadEMAStoreV2", callee: "writeHeadEMAStoreV2"},
		{path: "../validator/head_ema_v2_write_unix.go", function: "writeHeadEMAStoreV2", callee: "createHeadEMAStoreV2Marker"},
		{path: "../validator/head_ema_v2_write_unix.go", function: "writeHeadEMAStoreV2", callee: "publishHeadEMAStoreV2File"},
		{path: "../validator/head_ema_v2_write_unix.go", function: "writeHeadEMAStoreV2", callee: "hashHeadEMAStoreV2File"},
		{path: "../validator/head_ema_v2_write_unix.go", function: "writeHeadEMAStoreV2", callee: "finishHeadEMAStoreV2Write"},
		{path: "../validator/head_ema_v2_write_unix.go", function: "finishHeadEMAStoreV2Write", callee: "unlinkHeadEMAStoreV2Owned"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.function)[edge.callee] { t.Errorf("%s omits %s", edge.function, edge.callee) }
	}
	t.Log("ema_runtime_control_completed: TestProducerGateStateSelectionPinsHeadEMAStoreV2RuntimeEdges")
}
