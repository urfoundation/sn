// The current-runtime codec fixture contains protocol types only. Every chain,
// account, state row, code artifact and HTTP observation below is synthetic.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

// Documentation and unrelated constants/pallets were removed from the public
// v470 wire schema. This digest identifies that projection, not deployed code.
func subnetRuntime470TestMetadata(t *testing.T) []byte {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime470-subnet-codec.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, maxMetadataRpcReplyBytes))
	if err != nil {
		t.Fatal(err)
	}
	digest := blake2b.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "cdb975f33cf23ba0df2279208feeacdcf0e629f4cd0d0b3e972ee63d1ebdc0a4" {
		t.Fatal("runtime470 protocol projection changed")
	}
	return raw
}

// Unchanged key encodings let the new protocol schema read the same synthetic
// state; synthetic Wasm bytes supply no actual runtime or production identity.
func newSubnetDiscoveryFixture(t *testing.T) (*rpcClient, *rootRpcFixture, subnetCensusPolicy, runtimeSnapshot) {
	t.Helper()
	client, fixture, policy := newSubnetFixture(t)
	fixture.metadataHex = "0x" + hex.EncodeToString(subnetRuntime470TestMetadata(t))
	var err error
	fixture.metadata, policy.RuntimeMetadataHash, err = crv4.DecodeRuntimeMetadata(fixture.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	policy.RuntimeSourceCommit = rootPassiveSource
	policy.RuntimeVersion = crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1}
	code := []byte("synthetic-code-not-a-deployed-runtime")
	digest := blake2b.Sum256(code)
	policy.RuntimeCodeHash = "0x" + hex.EncodeToString(digest[:])
	fixture.policy.RuntimeMetadataHash, fixture.policy.RuntimeCodeHash, fixture.policy.RuntimeVersion = policy.RuntimeMetadataHash, policy.RuntimeCodeHash, policy.RuntimeVersion
	fixture.version = policy.RuntimeVersion
	identity := chainIdentity{Schema: identitySchema, ObservedAt: "2000-01-01T00:00:00Z", RpcUrl: "http://retained-rpc.example", NativeChain: policy.NativeChain,
		GenesisHash: policy.GenesisHash, EvmChainId: 964, NodeVersion: "synthetic-node", FinalizedHash: testFinalizedHash, FinalizedNumber: 100, RuntimeSpec: 470, RuntimeTx: 1}
	snapshot := runtimeSnapshot{Schema: runtimeSnapshotSchema, Admission: "unapproved_observation", Identity: identity, Version: policy.RuntimeVersion,
		CodeHash: policy.RuntimeCodeHash, MetadataHash: policy.RuntimeMetadataHash, CodeHex: "0x" + hex.EncodeToString(code), MetadataHex: fixture.metadataHex}
	return client, fixture, policy, snapshot
}

// Exact synthetic current-runtime pins preserve both removal generations and
// the excluded root baseline, without widening owner authority after v470.
func TestSubnetPreviewRuntime470CodecPreservesScope(t *testing.T) {
	client, fixture, policy, _ := newSubnetDiscoveryFixture(t)
	preview, err := client.readSubnetPreview(t.Context(), policy, "sha256:synthetic-policy")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.CensusComplete || preview.ResetReady || preview.RuntimeVersion.SpecVersion != 470 || preview.RuntimeSourceCommit != rootPassiveSource ||
		len(preview.Seats) != 6 || len(preview.RootRegistrations) != 2 || len(preview.Remove) != 2 || len(preview.Preserve) != 4 || len(preview.Unresolved) != 0 ||
		!preview.Trim.MatchesRequestedRemovalSet || !preview.Trim.CandidateComplete || preview.Trim.Call == nil || len(preview.Trim.Blockers) != 0 {
		t.Fatalf("current runtime changed exact read-only scope: %+v", preview)
	}
	plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || plan.Best == nil || !plan.Best.RuntimeReady || plan.ResetReady || plan.ApplyAuthority || plan.FullResetCompleted {
		t.Fatalf("current runtime trim planning broadened authority: %+v %v", plan, err)
	}
	if fixture.count("state_queryStorageAt") != 0 {
		t.Fatal("discovery batching changed the approved-policy preview path")
	}
}

// One helper writes only test-created JSON and gives public CLI tests a real
// local input boundary rather than injecting a prevalidated in-memory value.
func subnetDiscoveryTestInput(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "synthetic-observation.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The public route completes observations before an owner/removal policy
// exists, leaves every seat unclassified, and exports no executable artifact.
func TestSubnetDiscoveryCommandRuntime470ExportsUnapprovedMembership(t *testing.T) {
	_, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
	server := rootFixtureServer(t, fixture)
	sealed, err := sealRuntimeSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := subnetDiscoveryTestInput(t, sealed)
	var stdout, stderr bytes.Buffer
	if code := runMain(t.Context(), []string{"subnet-discover", "--rpc", server.URL, "--snapshot", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("discovery command failed: exit=%d %s", code, &stderr)
	}
	var envelope subnetDiscoveryEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	discovery := envelope.Discovery
	if !discovery.MembershipComplete || discovery.ResetReady || discovery.ApplyAuthority || discovery.RuntimeSourceProven || discovery.Admission != "unapproved_observation" ||
		discovery.FinalityAuthority != "rpc-assertion" || discovery.SubnetGeneration != 3 || discovery.SubnetRegistrationBlock != 10 || discovery.MinimumUids != 2 || discovery.MaximumUids != 8 ||
		len(discovery.Seats) != 6 || len(discovery.RootRegistrations) != 2 || len(discovery.Blockers) != 4 || discovery.Seats[0].EmissionAlphaRao != "100" || !discovery.Seats[2].ValidatorPermit || discovery.Seats[3].ValidatorPermit ||
		fixture.count("state_getKeysPaged") != 8 || fixture.count("state_queryStorageAt") != 4 || fixture.count("chain_getFinalizedHead") != 4 || len(discovery.Storage) == 0 {
		t.Fatalf("discovery omitted membership evidence or claimed authority: %+v", discovery)
	}
	for _, seat := range discovery.Seats {
		if seat.Disposition != "unclassified" {
			t.Fatal("discovery inferred a role or removal approval", seat)
		}
	}
	first, err := sealSubnetDiscovery(discovery)
	if err != nil || first.ContentHash != envelope.ContentHash {
		t.Fatal("discovery did not seal its exact output", err)
	}
	discovery.Storage[0].ValueSource = "changed"
	second, _ := sealSubnetDiscovery(discovery)
	if first.ContentHash == second.ContentHash {
		t.Fatal("discovery seal omitted raw storage evidence")
	}
	outputPath := filepath.Join(t.TempDir(), "synthetic-discovery.json")
	if err := os.WriteFile(outputPath, stdout.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	reads := fixture.count("system_chain")
	for _, command := range []string{"subnet-preview", "owner-trim-plan"} {
		stdout.Reset()
		stderr.Reset()
		if code := runMain(t.Context(), []string{command, "--rpc", server.URL, "--policy", outputPath}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("system_chain") != reads {
			t.Fatalf("discovery became an approved policy: %s exit=%d %s", command, code, &stderr)
		}
	}
}

// A real combined-format observation supplies only its checked runtime pins.
// A valid seal cannot join runtime and mapping components from different cuts.
func TestSubnetDiscoveryAcceptsCombinedSnapshotAndRejectsSplitIdentity(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	fixture.runtimeMetadata = subnetRuntime470TestMetadata(t)
	fixture.fault = func(method string, _ []any, _ int) (any, bool) {
		return crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1}, method == "state_getRuntimeVersion"
	}
	snapshot, err := client.readFinalizedSnapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealFinalizedSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	runtime, hash, err := readSubnetDiscoveryInput(subnetDiscoveryTestInput(t, sealed))
	if err != nil || hash == "" || runtime.CodeHash != snapshot.Runtime.CodeHash || runtime.MetadataHex != snapshot.Runtime.MetadataHex || runtime.Identity.FinalizedHash != snapshot.FinalizedHash {
		t.Fatalf("combined snapshot could not supply observation pins: %v", err)
	}
	sealed.Runtime.Identity.FinalizedNumber++
	if _, _, err := readSubnetDiscoveryInput(subnetDiscoveryTestInput(t, sealed)); err == nil {
		t.Fatal("split-identity combined snapshot reached discovery")
	}
}

// Malformed, falsely admitted and internally tampered snapshots fail before
// RPC; recomputing an envelope seal cannot bless mismatching artifact bytes.
func TestSubnetDiscoveryRejectsInvalidSnapshotBeforeRpc(t *testing.T) {
	for _, change := range []string{"seal", "code", "metadata", "admission", "version", "chain", "height-alias", "unknown", "duplicate", "trailing"} {
		_, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
		switch change {
		case "code":
			snapshot.CodeHex = "0x00"
		case "metadata":
			snapshot.MetadataHex += "00"
		case "admission":
			snapshot.Admission = "approved"
		case "version":
			snapshot.Version.SpecVersion++
		case "chain":
			snapshot.Identity.EvmChainId = 945
		case "height-alias":
			snapshot.Identity.RuntimeSpec++
		}
		sealed, _ := sealRuntimeSnapshot(snapshot)
		if change == "seal" {
			sealed.ContentHash = "sha256:" + strings.Repeat("00", 32)
		}
		path := subnetDiscoveryTestInput(t, sealed)
		raw, _ := os.ReadFile(path)
		switch change {
		case "unknown":
			raw = append([]byte(`{"apply":true,`), raw[1:]...)
		case "duplicate":
			raw = append([]byte(`{"content_hash":"ignored",`), raw[1:]...)
		case "trailing":
			raw = append(raw, []byte(` {}`)...)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		server := rootFixtureServer(t, fixture)
		var stdout, stderr bytes.Buffer
		if code := runMain(t.Context(), []string{"subnet-discover", "--rpc", server.URL, "--snapshot", path}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("system_chain") != 0 {
			t.Fatalf("%s snapshot reached RPC/output: exit=%d %s", change, code, &stderr)
		}
	}
}

// Retained observation identity is matched again against current RPC evidence,
// including the authenticated retained height, before any account-state read.
func TestSubnetDiscoveryRejectsRuntimeAndHeightDriftBeforeStorage(t *testing.T) {
	for _, change := range []string{"version", "code", "metadata", "chain", "height"} {
		client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
		switch change {
		case "version":
			fixture.version.TransactionVersion++
		case "code":
			fixture.policy.RuntimeCodeHash = "0x" + strings.Repeat("a5", 32)
		case "metadata":
			fixture.metadataHex += "00"
		case "chain":
			fixture.evmChainHex = "0x3b1"
		case "height":
			snapshot.Identity.FinalizedNumber--
		}
		result, err := client.readSubnetDiscovery(t.Context(), snapshot, "sha256:synthetic-snapshot")
		if err == nil || result.Schema != "" || fixture.count("state_getStorage") != 0 || fixture.count("state_getKeysPaged") != 0 || fixture.count("state_queryStorageAt") != 0 {
			t.Fatalf("%s drift reached storage or published evidence: %+v %v", change, result, err)
		}
	}
}

// Both map directions, root exclusion, vector coverage and recorded generation
// remain mandatory without a policy. An error never exports a partial census.
func TestSubnetDiscoveryRejectsIncompleteMembership(t *testing.T) {
	for _, change := range []string{"forward", "reverse", "root-reverse", "membership", "future", "owner", "subnet-birth", "predates", "capacity", "active", "permits", "emission"} {
		client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
		netuidArg, uidArg := []byte{25, 0}, []byte{0, 0}
		hotkey := bytes.Repeat([]byte{0x41}, 32)
		switch change {
		case "forward":
			delete(fixture.storageKVs, fixture.set(t, "Keys", hotkey, netuidArg, uidArg))
		case "reverse":
			fixture.set(t, "Uids", uidArg, netuidArg, bytes.Repeat([]byte{0xee}, 32))
		case "root-reverse":
			fixture.set(t, "Uids", uidArg, []byte{0, 0}, bytes.Repeat([]byte{0xee}, 32))
		case "membership":
			fixture.set(t, "IsNetworkMember", []byte{0}, hotkey, netuidArg)
		case "future":
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 101), netuidArg, uidArg)
		case "owner":
			fixture.set(t, "SubnetOwner", make([]byte, 32), netuidArg)
		case "subnet-birth":
			delete(fixture.storageKVs, fixture.set(t, "NetworkRegisteredAt", make([]byte, 8), netuidArg))
		case "predates":
			fixture.set(t, "NetworkRegisteredAt", binary.LittleEndian.AppendUint64(nil, 41), netuidArg)
		case "capacity":
			fixture.set(t, "MinAllowedUids", []byte{9, 0}, netuidArg)
		case "active":
			fixture.set(t, "Active", []byte{0}, netuidArg)
		case "permits":
			fixture.set(t, "ValidatorPermit", []byte{0}, netuidArg)
		case "emission":
			fixture.set(t, "Emission", []byte{0}, netuidArg)
		}
		result, err := client.readSubnetDiscovery(t.Context(), snapshot, "sha256:synthetic-snapshot")
		if !errors.Is(err, errRpcIntegrity) || result.Schema != "" || result.MembershipComplete || len(result.Seats) != 0 {
			t.Fatalf("%s corruption published membership: %+v %v", change, result, err)
		}
	}
}

// Explicit cancellation on the final response forces the late-publication
// boundary without sleep, scheduler timing or a short negative timeout.
func TestSubnetDiscoveryCancellationOnClosingReadPublishesNothing(t *testing.T) {
	client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if fixture.count("eth_chainId") == 2 {
			closed = true
			cancel()
		}
		return response, err
	})
	result, err := client.readSubnetDiscovery(ctx, snapshot, "sha256:synthetic-snapshot")
	if !closed || !errors.Is(err, context.Canceled) || result.Schema != "" || result.MembershipComplete || len(result.Storage) != 0 {
		t.Fatalf("closing cancellation published evidence: %+v %v", result, err)
	}
}

// The retained block must remain canonical after all account reads, even when
// its header, runtime and genesis were valid during the initial checks.
func TestSubnetDiscoveryRejectsClosingCanonicalDrift(t *testing.T) {
	client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
	fixture.forkAfterStorage = true
	result, err := client.readSubnetDiscovery(t.Context(), snapshot, "sha256:synthetic-snapshot")
	if !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "closing chain_getBlockHash") || result.Schema != "" || fixture.count("state_getKeysPaged") != 8 {
		t.Fatalf("closing fork published evidence: %+v %v", result, err)
	}
}

// Closing checks retain exact chain-name spelling as well as genesis, code
// and EVM identity. Rewriting a reply after membership reads forces this cut.
func TestSubnetDiscoveryRejectsClosingNetworkAndCodeDrift(t *testing.T) {
	for _, method := range []string{"system_chain", "state_getStorageHash", "eth_chainId", "chain_getBlockHash"} {
		client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
		changed := false
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			var call struct {
				Method string `json:"method"`
				Params []any  `json:"params"`
			}
			if err := json.Unmarshal(raw, &call); err != nil {
				return nil, err
			}
			response, err := fixture.roundTrip(request)
			if err != nil || fixture.count("state_getStorage") == 0 || call.Method != method || method == "chain_getBlockHash" && call.Params[0] != float64(0) {
				return response, err
			}
			changed = true
			response.Body.Close()
			value := any("0x" + strings.Repeat("ae", 32))
			if method == "system_chain" {
				value = strings.ToUpper(snapshot.Identity.NativeChain)
			} else if method == "eth_chainId" {
				value = "0x3b1"
			}
			body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
			response.Body = io.NopCloser(bytes.NewReader(body))
			return response, err
		})
		result, err := client.readSubnetDiscovery(t.Context(), snapshot, "sha256:synthetic-snapshot")
		if !changed || !errors.Is(err, errRpcIntegrity) || result.Schema != "" || result.MembershipComplete {
			t.Fatalf("closing %s drift published evidence: %+v %v", method, result, err)
		}
	}
}

// Local artifact input has its own wire-size bound before parsing or RPC.
func TestSubnetDiscoveryRejectsOversizedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-oversized.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maximumSubnetDiscoveryInputBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if _, _, err := readSubnetDiscoveryInput(path); err == nil || !strings.Contains(err.Error(), "exceeds bounded input") {
		t.Fatalf("oversized discovery input reached parsing: %v", err)
	}
}

// Mutation flags, missing policy substitutes and unbounded budgets are rejected
// by the public parser before even opening the input or contacting an endpoint.
func TestSubnetDiscoveryRejectsMutationFlagsAndUnboundedWindows(t *testing.T) {
	for _, extra := range [][]string{{"--apply"}, {"--policy", "synthetic-policy.json"}, {"--retry-window", "59s"}, {"--retry-window", "16m"}, {"unexpected"}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"subnet-discover", "--rpc", "http://rpc.example", "--snapshot", "synthetic-absent-snapshot.json"}, extra...)
		if code := runMain(t.Context(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "subnet-discover requires") {
			t.Fatalf("unexpected parser capability: %v exit=%d %s", extra, code, &stderr)
		}
	}
}
