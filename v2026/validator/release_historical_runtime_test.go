//go:build linux || darwin

package validator

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The metadata is the reviewed protocol artifact; all blocks, keys and state
// values come from the synthetic native reader fixture.
func releaseHistoricalTestMetadata(t *testing.T) string {
	t.Helper()
	encoded, err := os.ReadFile("../miner/testdata/runtime455-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err := errors.Join(err, reader.Close()); err != nil {
		t.Fatal(err)
	}
	metadata := hexutil.Encode(raw)
	_, hash, err := crv4.DecodeRuntimeMetadata(metadata)
	if err != nil || hash != "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc" {
		t.Fatalf("reviewed historical metadata differs: %s %v", hash, err)
	}
	return metadata
}

func releaseHistoricalTestArtifact() crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1},
		CodeHash: "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a", MetadataHash: "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"}
}

func releaseHistoricalTestCurrentArtifact() crv4.RuntimeArtifactIdentity {
	cfg := runtime461ValidatorTestConfig()
	return crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
}

// Reproduces the transport's JSON assignment, including nullable storage and
// the capture reader's bounded custom destination.
func setReleaseHistoricalTestResult(result any, value any) error {
	wire, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(wire, result)
}

func installReleaseHistoricalTestNative(t *testing.T, native *releaseNativeValidatorTestFixture) *string {
	t.Helper()
	metadata := releaseHistoricalTestMetadata(t)
	native.expected = releaseHistoricalTestArtifact()
	original := native.chain.API.Client
	native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		native.ctx = ctx
		if method == "state_getMetadata" {
			if len(args) != 1 || args[0] != native.block.Hex() {
				return errors.New("historical metadata changed original block")
			}
			native.calls = append(native.calls, method)
			return setReleaseHistoricalTestResult(result, metadata)
		}
		// The original fixture checks every method and argument. Its header
		// branch owns a typed result; the other branches provide raw JSON.
		if method == "chain_getHeader" {
			var header types.Header
			if err := original.CallContext(ctx, &header, method, args...); err != nil {
				return err
			}
			return setReleaseHistoricalTestResult(result, header)
		}
		var wire json.RawMessage
		if err := original.CallContext(ctx, &wire, method, args...); err != nil {
			return err
		}
		return json.Unmarshal(wire, result)
	}}
	return &metadata
}

// Both independent signatures and actual pinned native/Evm readers must pass
// when the source is455 and the currently approved release is461.
func TestReleaseEvidenceV2HistoricalRuntimeActivationAuthenticatesOriginalConsent(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	installReleaseHistoricalTestNative(t, fixture.native)
	fixture.authority.NativeRuntime = releaseHistoricalTestCurrentArtifact()
	metadata, runtime := fixture.native.chain.Meta, fixture.native.chain.Runtime
	if _, err := fixture.read(t.Context()); err != nil {
		t.Fatalf("original455 activation replay: %v", err)
	}
	if fixture.native.chain.Meta != metadata || fixture.native.chain.Runtime != runtime {
		t.Fatal("historical activation rebound current signing metadata")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeActivationRejectsChangedAuthority(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*releaseActivationV2TestFixture, *string)
	}{
		{name: "code", mutate: func(f *releaseActivationV2TestFixture, _ *string) { f.native.expected.CodeHash = types.Hash{99}.Hex() }},
		{name: "metadata", mutate: func(_ *releaseActivationV2TestFixture, raw *string) { *raw = "0x0102" }},
		{name: "version", mutate: func(f *releaseActivationV2TestFixture, _ *string) { f.native.expected.Version.SpecVersion = 456 }},
		{name: "uid", mutate: func(f *releaseActivationV2TestFixture, _ *string) { f.authority.ValidatorUID = 2 }},
		{name: "permit", mutate: func(f *releaseActivationV2TestFixture, _ *string) { f.native.permit = false }},
		{name: "stake", mutate: func(f *releaseActivationV2TestFixture, _ *string) { f.native.total = f.native.threshold - 1 }},
		{name: "consent", mutate: func(f *releaseActivationV2TestFixture, _ *string) { f.vpkSignature[0] ^= 1 }},
		{name: "runtime-authority", mutate: func(f *releaseActivationV2TestFixture, _ *string) {
			f.authority.NativeRuntime.CodeHash = types.Hash{98}.Hex()
		}},
	} {
		fixture := newReleaseActivationV2TestFixture(t, "")
		raw := installReleaseHistoricalTestNative(t, fixture.native)
		fixture.authority.NativeRuntime = releaseHistoricalTestCurrentArtifact()
		test.mutate(fixture, raw)
		if _, err := fixture.read(t.Context()); err == nil {
			t.Fatalf("changed %s became historical activation authority", test.name)
		}
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeSelectionPreservesExplicitAuthority(t *testing.T) {
	current := releaseHistoricalTestCurrentArtifact()
	want := []crv4.RuntimeArtifactIdentity{current, releaseHistoricalTestArtifact(), releaseHistorical458TestArtifact(), releaseHistorical459TestArtifact(), releaseHistorical460TestArtifact()}
	if got := HistoricalReleaseRuntimeArtifacts(current); !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewed history selection differs: %+v", got)
	}
	for _, test := range []struct {
		name   string
		mutate func(*crv4.RuntimeArtifactIdentity)
	}{
		{name: "future", mutate: func(v *crv4.RuntimeArtifactIdentity) { v.Version.SpecVersion++ }},
		{name: "name", mutate: func(v *crv4.RuntimeArtifactIdentity) { v.Version.SpecName = "foreign" }},
		{name: "transaction", mutate: func(v *crv4.RuntimeArtifactIdentity) { v.Version.TransactionVersion++ }},
		{name: "state", mutate: func(v *crv4.RuntimeArtifactIdentity) { v.Version.StateVersion++ }},
		{name: "code", mutate: func(v *crv4.RuntimeArtifactIdentity) { v.CodeHash = types.Hash{3}.Hex() }},
		{name: "metadata", mutate: func(v *crv4.RuntimeArtifactIdentity) { v.MetadataHash = types.Hash{4}.Hex() }},
		{name: "original", mutate: func(v *crv4.RuntimeArtifactIdentity) { *v = releaseHistoricalTestArtifact() }},
	} {
		input := current
		test.mutate(&input)
		if got := HistoricalReleaseRuntimeArtifacts(input); !reflect.DeepEqual(got, []crv4.RuntimeArtifactIdentity{input}) {
			t.Fatalf("%s gained implicit runtime authority", test.name)
		}
	}
	first := HistoricalReleaseRuntimeArtifacts(current)
	first[1].CodeHash = "changed"
	if !reflect.DeepEqual(HistoricalReleaseRuntimeArtifacts(current), want) {
		t.Fatal("caller mutated shared historical authority")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeKeepsFreshSigningCurrent(t *testing.T) {
	native := newReleaseNativeValidatorTestFixture(t)
	installReleaseHistoricalTestNative(t, native)
	cfg := runtime461ValidatorTestConfig()
	metadata, runtime := native.chain.Meta, native.chain.Runtime
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native.chain, &cfg, native.block); err == nil {
		t.Fatal("fresh signing admitted455")
	}
	if native.chain.Meta != metadata || native.chain.Runtime != runtime {
		t.Fatal("refused signing mutated binding")
	}
	owned := *native.chain
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), &owned, &cfg, native.block); err != nil {
		t.Fatalf("historical private source binding: %v", err)
	}
	if owned.Runtime.SpecVersion != 455 || native.chain.Meta != metadata || native.chain.Runtime != runtime {
		t.Fatal("historical binding escaped its private owner")
	}
}

// There is deliberately no persisted receipt. Neither legacy nor source-v2
// reconciliation may turn an authenticated old prepared block into a new send.
func releaseHistoricalTestPendingReplay(t *testing.T, versionTwo bool) {
	t.Helper()
	native := newReleaseNativeValidatorTestFixture(t)
	installReleaseHistoricalTestNative(t, native)
	original := native.chain.API.Client
	blockReads, sendCalls := 0, 0
	native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if strings.HasPrefix(method, "author_") {
			sendCalls++
			return errors.New("historical replay attempted a write")
		}
		if method == "chain_getBlock" {
			if len(args) != 1 || args[0] != native.block.Hex() {
				return errors.New("pending scan changed original hash")
			}
			blockReads++
			return json.Unmarshal([]byte(`{"block":{"header":{"parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000","number":"0x64","stateRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","extrinsicsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000","digest":{"logs":[]}},"extrinsics":[]},"justifications":null}`), result)
		}
		return original.CallContext(ctx, result, method, args...)
	}}
	prepared := testPreparedSubmission(t, 1, []uint16{1, 2})
	prepared.PreparedAtBlock, prepared.PreparedAtBlockHash = native.blockNumber, native.block.Hex()
	if _, err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg := runtime461ValidatorTestConfig()
	steerer := &ReleaseSteerer{cfg: &cfg, native: native.chain}
	intent := &SteeringIntent{Status: "pending", Prepared: prepared, SubnetEpoch: 1}
	metadata, runtime := native.chain.Meta, native.chain.Runtime
	var err error
	if versionTwo {
		_, err = steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1})
	} else {
		_, err = steerer.reconcilePending(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: 1})
	}
	if err == nil || !strings.Contains(err.Error(), "pending steering replay uses a historical signing runtime") || blockReads != 1 || sendCalls != 0 {
		t.Fatalf("old preparation replay guard: blocks=%d sends=%d error=%v", blockReads, sendCalls, err)
	}
	if native.chain.Meta != metadata || native.chain.Runtime != runtime {
		t.Fatal("pending history changed active signing view")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimePendingLegacyRefusesNewSubmission(t *testing.T) {
	releaseHistoricalTestPendingReplay(t, false)
}
func TestReleaseEvidenceV2HistoricalRuntimePendingV2RefusesNewSubmission(t *testing.T) {
	releaseHistoricalTestPendingReplay(t, true)
}

func TestReleaseEvidenceV2HistoricalRuntimeDecisionRetainsOriginalSources(t *testing.T) {
	fixture := newReleaseDecisionV2EligibilityTestFixture(t, nil)
	installReleaseHistoricalTestNative(t, fixture.native)
	current := releaseHistoricalTestCurrentArtifact()
	fixture.history.cfg.RuntimeSpec, fixture.history.cfg.RuntimeCodeHash, fixture.history.cfg.RuntimeMetadataHash = 461, current.CodeHash, current.MetadataHash
	if err := fixture.history.authenticateIntentChainReference(t.Context(), fixture.decision.chain, fixture.native.chain, current, fixture.intent, fixture.artifact); err != nil {
		t.Fatalf("original455 decision replay under461: %v", err)
	}
	fixture.artifact.NativeSnapshotHash = types.Hash{98}.Hex()
	if err := fixture.history.authenticateIntentChainReference(t.Context(), fixture.decision.chain, fixture.native.chain, current, fixture.intent, fixture.artifact); err == nil {
		t.Fatal("historical decision accepted a changed original block")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeStartupChecksOriginalSchedule(t *testing.T) {
	decision := newReleaseDecisionV2TestFixture(t)
	native := newReleaseDecisionV2NativeTestFixture(t, decision)
	installReleaseHistoricalTestNative(t, native)
	initial := ReleaseEvidenceV2ActivationContext{Activation: protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{GenesisHash: [32]byte(native.genesis), Netuid: 521}, Hotkey: native.hotkey, NativeBlock: native.blockNumber, NativeHash: [32]byte(native.block)}}
	journal := &releaseMeasurementInputJournal{SubnetEpoch: 1, MeasurementInput: ReleaseMeasurementInput{CutNativeBlock: native.blockNumber, CutNativeBlockHash: native.block.Hex()}}
	if err := authenticateReleaseStartupNativeV2Context(t.Context(), native.chain, initial, journal, releaseHistoricalTestCurrentArtifact(), false); err != nil {
		t.Fatalf("original455 startup schedule: %v", err)
	}
	journal.SubnetEpoch++
	if err := authenticateReleaseStartupNativeV2Context(t.Context(), native.chain, initial, journal, releaseHistoricalTestCurrentArtifact(), false); err == nil {
		t.Fatal("historical startup accepted changed subnet epoch")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeAdoptionChecksExactAppliedRow(t *testing.T) {
	decision := newReleaseDecisionV2TestFixture(t)
	native := newReleaseDecisionV2NativeTestFixture(t, decision)
	metadataHex := installReleaseHistoricalTestNative(t, native)
	metadata, _, err := crv4.DecodeRuntimeMetadata(*metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	key, err := types.CreateStorageKey(metadata, "SubtensorModule", "Weights", binary.LittleEndian.AppendUint16(nil, 521), binary.LittleEndian.AppendUint16(nil, native.uid))
	if err != nil {
		t.Fatal(err)
	}
	row, err := codec.Encode([]crv4.WeightPair{{UID: 1, Value: 9}, {UID: 2, Value: 11}})
	if err != nil {
		t.Fatal(err)
	}
	original := native.chain.API.Client
	native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getStorage" && len(args) == 2 && args[0] == key.Hex() {
			if args[1] != native.block.Hex() {
				return errors.New("applied row changed original block")
			}
			return setReleaseHistoricalTestResult(result, hexutil.Encode(row))
		}
		return original.CallContext(ctx, result, method, args...)
	}}
	cfg := runtime461ValidatorTestConfig()
	cfg.Netuid = 521
	intent := &SteeringIntent{Status: "applied", Prepared: &crv4.PreparedSubmission{HotkeyHex: hexutil.Encode(native.hotkey[:])}, SelfUID: native.uid, ApplicationBlock: native.blockNumber, ApplicationBlockHash: native.block.Hex(), RevealBlock: native.blockNumber, UIDs: []uint16{1, 2}, Values: []uint16{9, 11}}
	if err := authenticateAdoptedIntentApplicationV2(t.Context(), native.chain, &cfg, intent); err != nil {
		t.Fatalf("original455 applied receipt: %v", err)
	}
	intent.Values[1]++
	if err := authenticateAdoptedIntentApplicationV2(t.Context(), native.chain, &cfg, intent); err == nil {
		t.Fatal("adopted history accepted changed applied weights")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeArchiveReplaysOriginalConfig(t *testing.T) {
	fixture := newReleaseArchiveV2TestFixtureWithTrails(t, 1)
	original := releaseHistoricalTestArtifact()
	fixture.options.Config.RuntimeSpec = original.Version.SpecVersion
	fixture.options.Config.RuntimeCodeHash, fixture.options.Config.RuntimeMetadataHash = original.CodeHash, original.MetadataHash
	before := mustArchiveV2JSONTest(t, fixture.options.Config)
	if err := fixture.options.Config.Validate(); err == nil {
		t.Fatal("original archive config became fresh launch authority")
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
	if err != nil {
		t.Fatalf("original455 closed archive replay: %v", err)
	}
	defer archive.Close()
	closure, err := archive.TerminalClosure(fixture.last.Epoch)
	if err != nil || !bytes.Equal(mustArchiveV2JSONTest(t, closure), mustArchiveV2JSONTest(t, fixture.last)) {
		t.Fatalf("original archive lost signed terminal closure: %v", err)
	}
	if !bytes.Equal(before, mustArchiveV2JSONTest(t, fixture.options.Config)) {
		t.Fatal("archive rewrote original config bytes")
	}
}

func TestReleaseEvidenceV2HistoricalRuntimeArchiveRejectsChangedOriginalConfig(t *testing.T) {
	fixture := newReleaseArchiveV2TestFixtureWithTrails(t, 1)
	for _, test := range []struct {
		name   string
		mutate func(*ReleaseConfig)
	}{
		{name: "future", mutate: func(c *ReleaseConfig) { c.RuntimeSpec = 456 }},
		{name: "code", mutate: func(c *ReleaseConfig) { c.RuntimeCodeHash = types.Hash{3}.Hex() }},
		{name: "metadata", mutate: func(c *ReleaseConfig) { c.RuntimeMetadataHash = types.Hash{4}.Hex() }},
		{name: "transaction", mutate: func(c *ReleaseConfig) { c.TransactionVersion = 2 }},
		{name: "policy", mutate: func(c *ReleaseConfig) { c.PolicyHash = types.Hash{5}.Hex() }},
	} {
		options, cfg := fixture.options, *fixture.options.Config
		original := releaseHistoricalTestArtifact()
		cfg.RuntimeSpec, cfg.RuntimeCodeHash, cfg.RuntimeMetadataHash = 455, original.CodeHash, original.MetadataHash
		test.mutate(&cfg)
		options.Config = &cfg
		reads := 0
		options.ReadSource = func(context.Context, ReleaseEvidenceV2CaptureSource) ([]byte, error) {
			reads++
			return nil, errors.New("invalid authority reached archive sources")
		}
		if archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), options); err == nil || archive != nil || reads != 0 {
			t.Fatalf("changed %s escaped archive admission: reads=%d error=%v", test.name, reads, err)
		}
	}
}
