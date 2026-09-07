package main

// Startup-loader qualification retains the legacy arithmetic controls and
// exercises the real bounded native/decoder edges, without claiming activation.

import (
	"os"
	"regexp"
	"testing"
)

// Prefix coverage admits every current and adjacent EMA regression, not just
// a hand-maintained subset of the newly authored roots.
func TestProducerGateStateSelectionCoversHeadEMAStoreV2(t *testing.T) {
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	selector, err := releaseConnectPolicySelectorAssignment(string(raw), "producer_tests")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, path := range []string{"../validator/head_ema_test.go", "../validator/head_ema_v2_test.go", "../validator/head_ema_v2_control_test.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(source))
	}
	if err := verifyReleaseSourceTestCoverage(selector, "^TestHeadEMA", sources); err != nil {
		t.Fatal(err)
	}
	capture, err := releaseConnectPolicySelectorAssignment(string(raw), "capture_tests")
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"TestProducerGateStateSelectionCoversHeadEMAStoreV2", "TestProducerGateStateSelectionPinsHeadEMAStoreV2Edges"} {
		matched, err := regexp.MatchString(capture, root)
		if err != nil || !matched {
			t.Fatalf("capture gate omits %s: %v", root, err)
		}
	}
}

// These are real library edges. RunRelease still requires a separately
// authenticated v2 startup configuration and is deliberately not claimed here.
func TestProducerGateStateSelectionPinsHeadEMAStoreV2Edges(t *testing.T) {
	for _, edge := range []struct{ path, function, callee string }{
		{path: "../validator/head_ema_v2_load_unix.go", function: "NewHeadEMAStoreV2", callee: "newHeadEMAStoreV2"},
		{path: "../validator/head_ema_v2_load_unix.go", function: "newHeadEMAStoreV2", callee: "readHeadEMAStoreV2"},
		{path: "../validator/head_ema_v2_load_unix.go", function: "newHeadEMAStoreV2", callee: "decodeHeadEMAStoreV2"},
		{path: "../validator/head_ema_v2_load_unix.go", function: "readHeadEMAStoreV2", callee: "openAttemptPrivateDirectory"},
		{path: "../validator/head_ema_v2_load_unix.go", function: "readHeadEMAStoreV2", callee: "statAttemptPrivateFile"},
		{path: "../validator/head_ema_v2_load_unix.go", function: "readHeadEMAStoreV2", callee: "checkHeadEMAStoreV2Witness"},
		{path: "../validator/head_ema_v2.go", function: "decodeHeadEMAStoreV2", callee: "admitHeadEMAStoreV2Wire"},
		{path: "../validator/head_ema_v2.go", function: "decodeHeadEMAStoreV2", callee: "decodeRationalJSON"},
		{path: "../validator/head_ema_v2.go", function: "decodeHeadEMAStoreV2", callee: "verifyHeadEMAFold"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.function)[edge.callee] {
			t.Errorf("%s omits %s", edge.function, edge.callee)
		}
	}
}
