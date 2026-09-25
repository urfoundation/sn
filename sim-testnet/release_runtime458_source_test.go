// Runtime458 attestation binds exact source bytes and exercises the real shell
// transport with synthetic responses; no test can contact a blockchain node.
package main

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
)

// Supplies fake executables before the host path, retaining actual jq parsing.
func runtime458ArtifactShellFixture(t *testing.T, response string) string {
	t.Helper()
	dir := t.TempDir()
	for _, child := range []string{"bin", "work"} {
		if err := os.Mkdir(filepath.Join(dir, child), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"fixture-response.json": response,
		"bin/curl": `#!/usr/bin/env bash
set -euo pipefail
printf 'call\n' >>"$RUNTIME_TEST_DIR/calls"
printf '%s\n' "$@" >>"$RUNTIME_TEST_DIR/arguments"
for argument in "$@"; do
  case "$argument" in @*) cat -- "${argument#@}" >"$RUNTIME_TEST_DIR/captured-request.json" ;; esac
done
cat "$RUNTIME_TEST_DIR/fixture-response.json"
exit "$RUNTIME_TEST_STATUS"
`,
		"bin/cargo": `#!/usr/bin/env bash
printf 'cargo reached after exact genesis\n' >"$RUNTIME_TEST_DIR/cargo"
exit 73
`,
		"bin/sleep": `#!/usr/bin/env bash
printf 'unexpected pacing\n' >>"$RUNTIME_TEST_DIR/sleep"
exit 0
`,
	}
	for path, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(contents), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Every invocation retains a bounded owner and uses only mocked network tools.
func runRuntime458ArtifactShell(t *testing.T, dir string, status string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "bash", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RUNTIME_TEST_DIR="+dir, "RUNTIME_TEST_STATUS="+status, "RUNTIME_METADATA_PROBE_TARGET_DIR="+filepath.Join(dir, "target"), "TMPDIR="+dir)
	return command.CombinedOutput()
}

// Reads the actual function rather than reproducing its transport decisions.
func runtime458ArtifactRpcFunction(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../scripts/check-runtime-metadata-artifacts.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	start := strings.Index(script, "rpc_call() {\n")
	if start < 0 {
		t.Fatal("artifact checker has no rpc function")
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatal("artifact rpc function has no complete body")
	}
	return script[start : start+end+3]
}

// Confirms one owned-route request, unchanged payload and no redirect/retry path.
func checkRuntime458ArtifactArguments(t *testing.T, dir string, endpoint string) {
	t.Helper()
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || string(calls) != "call\n" {
		t.Fatalf("artifact rpc did not make exactly one request: %q %v", calls, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	arguments := string(raw)
	endpointCount := 0
	for _, argument := range strings.Split(strings.TrimSpace(arguments), "\n") {
		if strings.HasPrefix(argument, "http://") || strings.HasPrefix(argument, "https://") {
			if argument != endpoint {
				t.Fatalf("artifact rpc selected an unapproved endpoint: %q", argument)
			}
			endpointCount++
		}
	}
	if endpointCount != 1 {
		t.Fatal("artifact rpc must have exactly one endpoint operand")
	}
	for _, required := range []string{"--disable\n", "--fail\n", "--proto\n=http\n", "--noproxy\n*\n", "--data-binary\n@", endpoint + "\n"} {
		if !strings.Contains(arguments, required) {
			t.Fatalf("artifact rpc route lacks %q: %s", required, arguments)
		}
	}
	for _, forbidden := range []string{"--location", "--retry", "--proxy", "https://"} {
		if strings.Contains(arguments, forbidden) {
			t.Fatalf("artifact rpc permits an alternate or paced route: %s", arguments)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sleep")); !os.IsNotExist(err) {
		t.Fatal("artifact rpc paced or retried a request")
	}
}

// Mock input and captured requests cannot alias the real function's scratch files.
func checkRuntime458ArtifactFixture(t *testing.T, dir, response string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "fixture-response.json"))
	if err != nil || string(raw) != response {
		t.Fatalf("artifact scratch changed the immutable response fixture: %q %v", raw, err)
	}
	requestRaw, err := os.ReadFile(filepath.Join(dir, "captured-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	writtenRequest, err := os.ReadFile(filepath.Join(dir, "work", "request.json"))
	if err != nil || string(writtenRequest) != string(requestRaw) {
		t.Fatalf("captured request differs from the real request bytes: %q %q %v", requestRaw, writtenRequest, err)
	}
	var request struct {
		Jsonrpc string `json:"jsonrpc"`
		Id      int    `json:"id"`
		Method  string `json:"method"`
		Params  []int  `json:"params"`
	}
	if err := json.Unmarshal(requestRaw, &request); err != nil || request.Jsonrpc != "2.0" || request.Id != 1 || request.Method != "synthetic_method" || len(request.Params) != 1 || request.Params[0] != 7 {
		t.Fatalf("artifact request identity changed: %s %v", requestRaw, err)
	}
}

// Exact valid responses survive the real function without changing the request.
func TestRuntime458ArtifactRpcPreservesExactResponse(t *testing.T) {
	for _, response := range []string{
		`{"jsonrpc":"2.0","id":1,"result":"synthetic-result"}`,
		`{"jsonrpc":"2.0","id":1,"result":null}`,
		`{"jsonrpc":"2.0","id":1,"result":{"synthetic":[7,false]}}`,
	} {
		dir := runtime458ArtifactShellFixture(t, response)
		body := "set -euo pipefail\nwork_dir=\"$1/work\"\nrpc_url='http://192.0.2.42:19944'\nrpc_request_id=1\n" + runtime458ArtifactRpcFunction(t) + "\nrpc_call 'synthetic_method' '[7]' \"$work_dir/output.json\"\n"
		output, err := runRuntime458ArtifactShell(t, dir, "0", "-c", body, "synthetic-rpc", dir)
		if err != nil {
			t.Fatalf("exact artifact response refused: %s %v", output, err)
		}
		checkRuntime458ArtifactArguments(t, dir, "http://192.0.2.42:19944")
		checkRuntime458ArtifactFixture(t, dir, response)
		raw, err := os.ReadFile(filepath.Join(dir, "work", "output.json"))
		if err != nil || string(raw) != response {
			t.Fatalf("artifact response changed: %q %v", raw, err)
		}
	}
}

// Transport and response refusals have no retry, pacing or published output.
func TestRuntime458ArtifactRpcRejectsMalformedAndFailedResponses(t *testing.T) {
	for _, testCase := range []struct{ name, response, status string }{
		{name: "transport", response: `{"jsonrpc":"2.0","id":1,"result":null}`, status: "22"},
		{name: "invalid json", response: "{", status: "0"},
		{name: "non-object", response: `[]`, status: "0"},
		{name: "wrong id", response: `{"jsonrpc":"2.0","id":2,"result":null}`, status: "0"},
		{name: "string id", response: `{"jsonrpc":"2.0","id":"1","result":null}`, status: "0"},
		{name: "missing id", response: `{"jsonrpc":"2.0","result":null}`, status: "0"},
		{name: "wrong protocol", response: `{"jsonrpc":"1.0","id":1,"result":null}`, status: "0"},
		{name: "rpc error", response: `{"jsonrpc":"2.0","id":1,"error":{"code":-1}}`, status: "0"},
		{name: "result and error", response: `{"jsonrpc":"2.0","id":1,"result":null,"error":null}`, status: "0"},
		{name: "missing result", response: `{"jsonrpc":"2.0","id":1}`, status: "0"},
	} {
		dir := runtime458ArtifactShellFixture(t, testCase.response)
		body := "set -euo pipefail\nwork_dir=\"$1/work\"\nrpc_url='http://192.0.2.42:19944'\nrpc_request_id=1\n" + runtime458ArtifactRpcFunction(t) + "\nrpc_call 'synthetic_method' '[7]' \"$work_dir/output.json\"\n"
		if output, err := runRuntime458ArtifactShell(t, dir, testCase.status, "-c", body, "synthetic-rpc", dir); err == nil {
			t.Fatalf("%s response was admitted: %s", testCase.name, output)
		}
		checkRuntime458ArtifactArguments(t, dir, "http://192.0.2.42:19944")
		checkRuntime458ArtifactFixture(t, dir, testCase.response)
		if _, err := os.Stat(filepath.Join(dir, "work", "output.json")); !os.IsNotExist(err) {
			t.Fatalf("%s published an unauthenticated response", testCase.name)
		}
	}
}

// The actual script keeps old provenance while selecting only the current owned
// observation route; mocked cargo stops it before any compilation or bulk reads.
func TestRuntime458ArtifactCheckerKeepsHistoricalProvenanceOffFreshRoute(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-metadata-artifacts.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseRuntimeMetadataArtifactManifest
	if err := decodeStrictJSONBytes(raw, &manifest); err != nil || len(manifest.Artifacts) != len(crv4.ReviewedRuntimeArtifacts()) {
		t.Fatalf("artifact fixture manifest is incomplete: %v", err)
	}
	var observation releaseRuntimeMetadataArtifact
	observations := 0
	for _, artifact := range manifest.Artifacts {
		if artifact.SpecVersion == 458 {
			observation = artifact
			observations++
		}
		if artifact.SpecVersion < 458 && artifact.ObservationRpcUrl != "" {
			t.Fatal("historical observation provenance was rewritten")
		}
	}
	endpoint, err := url.Parse(observation.ObservationRpcUrl)
	if observations != 1 || err != nil || endpoint == nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || !net.ParseIP(endpoint.Hostname()).IsPrivate() || endpoint.Port() == "" || observation.ObservationRpcUrl == manifest.SubstrateRPCURL {
		t.Fatal("runtime458 observation lost its distinct owned-route provenance")
	}
	response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": manifest.GenesisHash})
	if err != nil {
		t.Fatal(err)
	}
	dir := runtime458ArtifactShellFixture(t, string(response))
	for _, relative := range []string{"scripts/check-runtime-metadata-artifacts.sh", "docs/spec/runtime-metadata-artifacts.json", "tools/runtime-metadata-probe/Cargo.toml", "tools/runtime-metadata-probe/Cargo.lock", "tools/runtime-metadata-probe/rust-toolchain.toml"} {
		contents, err := os.ReadFile(filepath.Join("..", relative))
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(dir, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRuntime458ArtifactShell(t, dir, "0", filepath.Join(dir, "scripts/check-runtime-metadata-artifacts.sh"))
	if err == nil {
		t.Fatal("synthetic cargo boundary did not stop the checker")
	}
	if _, err := os.Stat(filepath.Join(dir, "cargo")); err != nil {
		t.Fatalf("actual artifact checker did not reach the post-genesis boundary: %s %v", output, err)
	}
	checkRuntime458ArtifactArguments(t, dir, observation.ObservationRpcUrl)
}

// Keeps all prior precompile/metadata paths and attests every changed upstream
// path in the reviewed455-to458 range, including adjacent accounting guards.
func TestRuntime458SourceAttestationIncludesExactCompatibilityDelta(t *testing.T) {
	previous, err := os.ReadFile("../docs/spec/runtime-v455-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	expectedKVs := map[string]string{}
	for _, row := range strings.Split(strings.TrimSpace(string(previous)), "\n") {
		fields := strings.Fields(row)
		if len(fields) != 2 || expectedKVs[fields[1]] != "" {
			t.Fatalf("malformed retained455 source row %q", row)
		}
		expectedKVs[fields[1]] = fields[0]
	}
	if len(expectedKVs) != 33 {
		t.Fatal("retained455 source scope changed")
	}
	for path, digest := range map[string]string{
		"chain-extensions/src/lib.rs":                            "31de41ce7dd7e14f4634fa03a6e307756ffcacd87637e53268259559424b6b35",
		"chain-extensions/src/tests.rs":                          "ad727e7f5c1a2e3f815737bac7076b3909523eaa56c0e93c4ca2020e6c2be9bb",
		"chain-extensions/src/types.rs":                          "08fe06fef304cc03b8644ef6950bac06fe45c4790a4098316dc7e20bc92b7932",
		"docs/internals/wasm-contracts.mdx":                      "d66509c882423029720229eb824e1e9a8f45f4504a894f30123793c0702793b6",
		"ink-contract/lib.rs":                                    "2d38c6d0d6efbe9a21e47b3fec6ba69dde95897879a1330eb62b145e57142bbc",
		"pallets/crowdloan/src/benchmarking.rs":                  "9e93c3267c3ffd9fb573441eb07fb5d8d8dd9bd683048984fb4df5595572b8f2",
		"pallets/crowdloan/src/lib.rs":                           "d26f355be5fa5074d4da9bc902f7d2cadbc6944221656aac18a736aabdb289af",
		"pallets/crowdloan/src/mock.rs":                          "3ea83a22dbbc630894f329c198f6764d908e33f091b3769ed473038f1e9518ea",
		"pallets/crowdloan/src/tests.rs":                         "9c8b2b74c529ec3aebf3096893468a762cea5c393a74aaa164c1fbc808b86396",
		"pallets/limit-orders/src/lib.rs":                        "dd23c6c4868305fafec40ef2b89df8291300ac1556df837205b457f81d750359",
		"pallets/limit-orders/src/tests/extrinsics.rs":           "002c311d657f7dedd617a67decf499ed2f8853600d97b3b8415208b5bf9ffc96",
		"pallets/limit-orders/src/tests/mock.rs":                 "6cd2c0ceba09f10ea04077155784752a53ada799918686138fbb51cb93ec1f7e",
		"pallets/proxy/src/lib.rs":                               "6c7f5987504468166b1d5b005fc42a2277c70ac34d478915b364a3b637705b12",
		"pallets/proxy/src/tests.rs":                             "994adca8bab7b6ecf096352890b291570a69b24c1dbc4725fca831f758d10b19",
		"pallets/subtensor/src/benchmarks/benchmarks.rs":         "a749723f0c11452c540032bd4369e9945f955b65c4e7547529a609b5aa0017c9",
		"pallets/subtensor/src/benchmarks/helpers.rs":            "0b3167c883b0c7ad1df3c24cc4669073176e3f8368dc7594989f859d149a8e3a",
		"pallets/subtensor/src/coinbase/root.rs":                 "e9687412fa4b44be62647f9556f12775d9dd8cac9dfebe6a46de0ff1129b20dc",
		"pallets/subtensor/src/lib.rs":                           "14d60cfb0ab1361c07f539037469325ecc93aa8677ea6ef3dec121e3a876ac48",
		"pallets/subtensor/src/macros/dispatches.rs":             "cf02750049a04bcb091d725a18b6df442f87900442b6c096bba0bec07b221488",
		"pallets/subtensor/src/staking/claim_root.rs":            "ebb311a8d3a839b59180f0203ba4730a173c3b963ee57dcf957e6b7a62d23ffd",
		"pallets/subtensor/src/staking/lock.rs":                  "9d17513f1d6142588850cd4f7306441eefd7b2817f841810b775655916c88419",
		"pallets/subtensor/src/staking/order_swap.rs":            "2cd41eeabacc6be8c7dccf7558f02c22e19f0b38a758e3a9e9871dcbe5dbcec3",
		"pallets/subtensor/src/staking/stake_utils.rs":           "bfe5dc92474f7ddf7de60e7e8bf351d2d527f50974bfedfb3b1beeffb87472fe",
		"pallets/subtensor/src/subnets/collateral.rs":            "4598de7d7221f238fb2763d12abbd5a842cc8ce97554864604336df0b63ba342",
		"pallets/subtensor/src/subnets/leasing.rs":               "145b8e907857bef18a3205e806861bb8e27efe533c7caec41de06910043f6842",
		"pallets/subtensor/src/swap/swap_coldkey.rs":             "61d80c02a80b7480de6f2330b014ee375055c6bd7dbc37fe1c6ce5f734e3e4bd",
		"pallets/subtensor/src/swap/swap_hotkey.rs":              "567a33deb00626b412a70f7514a5c7e5852e48ebdb87de3f93f7c04684321d4d",
		"pallets/subtensor/src/tests/children.rs":                "ed8b579252335e1589dd3eef148e264d22b4961ff5afbf08829f5f372e891beb",
		"pallets/subtensor/src/tests/claim_root.rs":              "0a1cc251f727c0a9dbb9eb55de1caadbaf4ef57fcb578c94d8a41b34bf30ffbc",
		"pallets/subtensor/src/tests/leasing.rs":                 "9bf9b827468ae084779eefd573920b5a4488f363c90f32a9cc3c6b6779029c18",
		"pallets/subtensor/src/tests/locks.rs":                   "4cfd566a7ab28ea800145ccc4c4ec25984bd847f87e8dc35fcef766a8a2423fc",
		"pallets/subtensor/src/tests/mock.rs":                    "4cc5bcc08c795c751f5d8671524dd1cb309903348d0a81866cf7cea53b3f1560",
		"pallets/subtensor/src/tests/move_stake.rs":              "a391fe0bf917e89e49d9b6e69d57d42d6defed4b285f820f885fb2979f10f3fe",
		"pallets/subtensor/src/tests/registration.rs":            "eb20d7236c03da097b7e0c2f64d8e232119604ad7d4c75d689ea7398d291c0c4",
		"pallets/subtensor/src/tests/stake_into_basket.rs":       "eb32fa348e670f9e0b46d27c8ccd53d4ab45d95583c1f23a20bb0a8d702fd2c2",
		"pallets/subtensor/src/tests/staking.rs":                 "2437510f142f4720010a00afb001767a45b9fe3f1d0cae2ece2e41c647e24de4",
		"pallets/subtensor/src/tests/swap_hotkey.rs":             "fc0463710086a488eae959d9abaec07b74af14af3e1f3784c7aaf994af9c846d",
		"primitives/share-pool/src/lib.rs":                       "822eb71bf3e8c74332a9091be16fc5f0fde8fc2d7b82a971360f1a30cbfe178a",
		"runtime/src/lib.rs":                                     "c1306a98ec4f594f3dacaabe5ea9f26ed77d5987c1ead01cb5dbc98752ac02df",
		"ts-tests/suites/zombienet_evm/03-wasm-contract.test.ts": "6f6e31e703feadaa6da7efb5750bc728c6fb185a5c6adeae2e3ba89be023f08f",
	} {
		expectedKVs[path] = digest
	}
	current, err := os.ReadFile("../docs/spec/runtime-v458-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if len(expectedKVs) != 62 {
		t.Fatal("reviewed458 source scope changed")
	}
	if err := verifyReleaseRuntimeSourceCensus(string(current), expectedKVs); err != nil {
		t.Fatal(err)
	}
	checker, err := os.ReadFile("../scripts/check-runtime-v454-source.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"for current_spec in 455 458", "runtime-v458-source.sha256", "current_commit=\"a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7\"", "expected_current_files=62", "SUBTENSOR_RUNTIME458_SOURCE", "expected_files=29", "expected_metadata_files=30"} {
		if !strings.Contains(string(checker), required) {
			t.Fatalf("runtime458 source checker omits %q", required)
		}
	}
}
