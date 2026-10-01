// Synthetic mainnet approval history exercises the real config loader and
// outer native admission without keys, network sockets or production evidence.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Mutable transport responses are guarded only while scripted calls execute.
// Tests change them between joined operations, never during an observation.
type mainnetRuntimeTestFixture struct {
	stateLock        sync.Mutex
	cfg              ReleaseConfig
	approvals        []releaseMainnetRuntimeApproval
	path             string
	native           *crv4.Chain
	genesis          types.Hash
	nativeChain      string
	evmChainId       string
	head             uint64
	metadata         string
	versions         []crv4.RuntimeVersionIdentity
	codes            []string
	callKVs          map[string]int
	canonicalHashKVs map[uint64]types.Hash
}

// No spec or genesis here is a shipped production identity.
func newMainnetRuntimeTestFixture(t *testing.T) *mainnetRuntimeTestFixture {
	t.Helper()
	self := &mainnetRuntimeTestFixture{cfg: validReleaseConfig(t), genesis: types.Hash{0x91}, nativeChain: "Synthetic Mainnet", evmChainId: "0x3c4", head: 150,
		metadata: validatorRuntimeIdentityTestMetadata(t), callKVs: map[string]int{}, canonicalHashKVs: map[uint64]types.Hash{}}
	_, metadataHash, err := crv4.DecodeRuntimeMetadata(self.metadata)
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.SchemaVersion = releaseMainnetRuntimeObservationSchemaVersion
	self.cfg.ChainID = 964
	self.cfg.GenesisHash = self.genesis.Hex()
	self.cfg.Policy.NetworkProfile = "mainnet"
	self.cfg.Policy.ProductionCadence.EpochBlocks = 50_400
	self.cfg.PolicyHash, err = self.cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.Substrate = []string{"wss://validator-runtime.test"}
	root := identityTestStateDir(t)
	for index := range 2 {
		approval := releaseMainnetRuntimeApproval{Schema: releaseMainnetRuntimeApprovalSchema, Revision: uint64(index + 1), NativeChain: self.nativeChain,
			GenesisHash: self.cfg.GenesisHash, EvmChainId: 964, DeploymentId: self.cfg.DeploymentID, ValidatorId: self.cfg.ValidatorID,
			Netuid: self.cfg.Netuid, Coordinator: self.cfg.Coordinator, PolicyHash: self.cfg.PolicyHash, ValidFromBlock: uint64(1 + index*100), ValidThroughBlock: uint64((index + 1) * 100),
			RuntimeVersion:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: uint32(9_001 + index), TransactionVersion: 1, StateVersion: 1},
			RuntimeCodeHash: (types.Hash{byte(0xa1 + index)}).Hex(), RuntimeMetadataHash: metadataHash,
			RuntimeSourceCommit: strings.Repeat("12", 20), RuntimeReviewSha256: strings.Repeat("34", 32), RuntimeReviewScope: releaseMainnetRuntimeObservationScope}
		if index > 0 {
			approval.PreviousSha256 = self.cfg.MainnetRuntimeApprovals[index-1].SHA256
		}
		self.approvals = append(self.approvals, approval)
		self.versions = append(self.versions, approval.RuntimeVersion)
		self.codes = append(self.codes, approval.RuntimeCodeHash)
		reference := mainnetRuntimeTestWriteApproval(t, filepath.Join(root, fmt.Sprintf("approval-%d.json", index+1)), approval)
		self.cfg.MainnetRuntimeApprovals = append(self.cfg.MainnetRuntimeApprovals, reference)
	}
	tail := self.approvals[1]
	self.cfg.RuntimeSpec = tail.RuntimeVersion.SpecVersion
	self.cfg.RuntimeCodeHash = tail.RuntimeCodeHash
	self.cfg.RuntimeMetadataHash = tail.RuntimeMetadataHash
	self.path = writeReleaseConfig(t, self.cfg)
	client := &validatorRuntimeIdentityTestClient{callContext: self.callContext}
	self.native = &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, GenesisHash: self.genesis, Meta: types.NewMetadataV14(), Runtime: &types.RuntimeVersion{SpecName: "unbound", SpecVersion: 3}}
	return self
}

// Exact immutable approval references use the production hash/size shape.
func mainnetRuntimeTestWriteApproval(t *testing.T, path string, value any) ReleaseEvidenceV2File {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return mainnetRuntimeTestWriteBytes(t, path, raw)
}

// Replacements are intentional negative fixtures; the production loader reads.
func mainnetRuntimeTestWriteBytes(t *testing.T, path string, raw []byte) ReleaseEvidenceV2File {
	t.Helper()
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(raw)), SHA256: attemptHex32(sha256.Sum256(raw))}
}

// The synthetic block hash encodes its height and cannot alias genesis.
func mainnetRuntimeTestBlock(number uint64) types.Hash {
	var hash types.Hash
	binary.LittleEndian.PutUint64(hash[:8], number)
	hash[31] = 0xdd
	return hash
}

// Only read methods required by identity observation are implemented.
func (self *mainnetRuntimeTestFixture) callContext(ctx context.Context, result any, method string, args ...any) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	self.callKVs[method]++
	switch method {
	case "system_chain":
		return setValidatorRuntimeIdentityTestResult(result, self.nativeChain)
	case "eth_chainId":
		return setValidatorRuntimeIdentityTestResult(result, self.evmChainId)
	case "chain_getFinalizedHead":
		return setValidatorRuntimeIdentityTestResult(result, mainnetRuntimeTestBlock(self.head).Hex())
	case "chain_getBlockHash":
		number, ok := args[0].(uint64)
		if !ok {
			return fmt.Errorf("unexpected block height %T", args[0])
		}
		value := mainnetRuntimeTestBlock(number)
		if number == 0 {
			value = self.genesis
		} else if override, ok := self.canonicalHashKVs[number]; ok {
			value = override
		}
		*result.(*types.Hash) = value
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("unexpected observation Rpc %s", method)
	}
	hash, err := types.NewHashFromHexString(fmt.Sprint(args[len(args)-1]))
	if err != nil {
		return err
	}
	number := binary.LittleEndian.Uint64(hash[:8])
	index := 0
	if number > 100 {
		index = 1
	}
	switch method {
	case "chain_getHeader":
		*result.(*types.Header) = types.Header{Number: types.BlockNumber(number)}
		return nil
	case "state_getRuntimeVersion":
		return setValidatorRuntimeIdentityTestResult(result, self.versions[index])
	case "state_getStorageHash":
		if len(args) != 2 || args[0] != "0x3a636f6465" {
			return fmt.Errorf("unexpected code-hash query %v", args)
		}
		return setValidatorRuntimeIdentityTestResult(result, self.codes[index])
	case "state_getMetadata":
		return setValidatorRuntimeIdentityTestResult(result, self.metadata)
	default:
		return fmt.Errorf("unexpected observation Rpc %s", method)
	}
}

// Compiled release pins no longer block independently approved read identities;
// default producer/bootstrap/archive admission still cannot load this schema.
func TestMainnetRuntimeObservationLoadsWithoutGrantingWriterAuthority(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeSpec != 9_002 || cfg.ProvisionalRuntimeCompatibility != "" {
		t.Fatal("observation changed the independent runtime pin")
	}
	if err := validateReleaseNativeRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseHistoricalNativeRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseNativeRuntimeConfig(&fixture.cfg); err == nil {
		t.Fatal("unloaded public fields forged authenticated approval history")
	}
	for name, load := range map[string]func(string) (*ReleaseConfig, error){"producer": LoadReleaseConfig, "bootstrap": LoadReleaseConfigPreActivation, "provisional": LoadProvisionalActivationObservationConfig} {
		if got, err := load(fixture.path); err == nil || got != nil {
			t.Fatalf("%s admitted read-only authority: %v", name, err)
		}
	}
	for name, validate := range map[string]func() error{"current": cfg.Validate, "archive": cfg.ValidateHistorical} {
		if err := validate(); err == nil {
			t.Fatalf("%s admitted read-only authority", name)
		}
	}
	if err := RunRelease(context.Background(), fixture.path); err == nil {
		t.Fatal("producer lifecycle admitted a runtime observer")
	}
	if _, err := os.Stat(cfg.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only config opened state: %v", err)
	}
}

// History chooses exact original artifacts by block, not numerical precedence.
func TestMainnetRuntimeObservationAuthenticatesSuccessorAndOriginalHistory(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	originalMetadata, originalRuntime := fixture.native.Meta, fixture.native.Runtime
	current, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 2 || current.Runtime != fixture.approvals[1].artifactIdentity() || current.BlockHash != mainnetRuntimeTestBlock(150) || current.ApprovalSha256 != cfg.MainnetRuntimeApprovals[1].SHA256 {
		t.Fatalf("current observation lost exact approval: %+v", current)
	}
	original, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, mainnetRuntimeTestBlock(100))
	if err != nil {
		t.Fatal(err)
	}
	if original.Revision != 1 || original.Runtime != fixture.approvals[0].artifactIdentity() || original.ConfigSha256 != current.ConfigSha256 {
		t.Fatalf("historical observation reinterpreted original authority: %+v", original)
	}
	if fixture.native.Meta != originalMetadata || fixture.native.Runtime != originalRuntime || cfg.RuntimeSpec != 9_002 {
		t.Fatal("read-only observation changed config or mutable signing view")
	}
	// Exercise the actual outer runtime gate on a private read view as well.
	view := *fixture.native
	if err := authenticateHistoricalNativeRuntimeAtContext(context.Background(), &view, cfg, mainnetRuntimeTestBlock(100)); err != nil {
		t.Fatal(err)
	}
	if uint32(view.Runtime.SpecVersion) != 9_001 || fixture.native.Runtime != originalRuntime {
		t.Fatal("historical read view did not retain its own approved artifact")
	}
}

// Later approvals cannot expand an archived config's retained authority.
func TestMainnetRuntimeObservationOriginalConfigCannotInheritSuccessor(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	fixture.cfg.MainnetRuntimeApprovals = fixture.cfg.MainnetRuntimeApprovals[:1]
	fixture.cfg.RuntimeSpec = fixture.approvals[0].RuntimeVersion.SpecVersion
	fixture.cfg.RuntimeCodeHash = fixture.approvals[0].RuntimeCodeHash
	cfg, err := LoadMainnetRuntimeObservationConfig(writeReleaseConfig(t, fixture.cfg))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || got != nil || fixture.callKVs["state_getRuntimeVersion"] != 0 {
		t.Fatalf("original config inherited later approval: observation=%+v err=%v calls=%v", got, err, fixture.callKVs)
	}
	if _, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, mainnetRuntimeTestBlock(50)); err != nil {
		t.Fatal(err)
	}
}

// A hash-pinned path replacement cannot change an already loaded observation;
// reloading the old config rejects it, and config mutation invalidates its seal.
func TestMainnetRuntimeObservationRetainsBytesAndRejectsConfigMutation(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	changed := fixture.approvals[1]
	changed.ValidFromBlock = 1
	mainnetRuntimeTestWriteApproval(t, cfg.MainnetRuntimeApprovals[1].Path, changed)
	if _, err := LoadMainnetRuntimeObservationConfig(fixture.path); err == nil {
		t.Fatal("changed approval path satisfied the original pinned reference")
	}
	if _, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err != nil {
		t.Fatal(err)
	}
	before := fixture.callKVs["system_chain"]
	cfg.Substrate = append(cfg.Substrate, "wss://new.example")
	if _, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || fixture.callKVs["system_chain"] != before {
		t.Fatal("mutated config inherited authenticated history")
	}
}

// Approval documents must append exact lineage and keep all deployment/domain
// and review coordinates intact even when their newly supplied hash is correct.
func TestMainnetRuntimeObservationRejectsInvalidApprovalHistory(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*releaseMainnetRuntimeApproval)
	}{
		{"schema", func(value *releaseMainnetRuntimeApproval) { value.Schema = "unreviewed" }},
		{"revision", func(value *releaseMainnetRuntimeApproval) { value.Revision = 3 }},
		{"lineage", func(value *releaseMainnetRuntimeApproval) { value.PreviousSha256 = "" }},
		{"overlap", func(value *releaseMainnetRuntimeApproval) { value.ValidFromBlock = 100 }},
		{"zero end", func(value *releaseMainnetRuntimeApproval) { value.ValidThroughBlock = 0 }},
		{"name", func(value *releaseMainnetRuntimeApproval) { value.NativeChain = "Other Synthetic Mainnet" }},
		{"genesis", func(value *releaseMainnetRuntimeApproval) { value.GenesisHash = (types.Hash{3}).Hex() }},
		{"chain", func(value *releaseMainnetRuntimeApproval) { value.EvmChainId = 945 }},
		{"deployment", func(value *releaseMainnetRuntimeApproval) { value.DeploymentId += "-other" }},
		{"validator", func(value *releaseMainnetRuntimeApproval) { value.ValidatorId++ }},
		{"subnet", func(value *releaseMainnetRuntimeApproval) { value.Netuid++ }},
		{"coordinator", func(value *releaseMainnetRuntimeApproval) { value.Coordinator = "0x" + strings.Repeat("ab", 20) }},
		{"policy", func(value *releaseMainnetRuntimeApproval) { value.PolicyHash = (types.Hash{5}).Hex() }},
		{"scope", func(value *releaseMainnetRuntimeApproval) { value.RuntimeReviewScope = "production-signing" }},
		{"source", func(value *releaseMainnetRuntimeApproval) { value.RuntimeSourceCommit = "v9002" }},
		{"review", func(value *releaseMainnetRuntimeApproval) { value.RuntimeReviewSha256 = "" }},
		{"runtime name", func(value *releaseMainnetRuntimeApproval) { value.RuntimeVersion.SpecName = "other" }},
		{"zero spec", func(value *releaseMainnetRuntimeApproval) { value.RuntimeVersion.SpecVersion = 0 }},
		{"zero transaction", func(value *releaseMainnetRuntimeApproval) { value.RuntimeVersion.TransactionVersion = 0 }},
		{"zero state", func(value *releaseMainnetRuntimeApproval) { value.RuntimeVersion.StateVersion = 0 }},
		{"code", func(value *releaseMainnetRuntimeApproval) { value.RuntimeCodeHash = "" }},
		{"metadata", func(value *releaseMainnetRuntimeApproval) { value.RuntimeMetadataHash = "" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newMainnetRuntimeTestFixture(t)
			changed := fixture.approvals[1]
			testCase.change(&changed)
			fixture.cfg.MainnetRuntimeApprovals[1] = mainnetRuntimeTestWriteApproval(t, fixture.cfg.MainnetRuntimeApprovals[1].Path, changed)
			if cfg, err := LoadMainnetRuntimeObservationConfig(writeReleaseConfig(t, fixture.cfg)); err == nil || cfg != nil {
				t.Fatalf("invalid approval admitted: %v", err)
			}
		})
	}
}

// Read-only authority cannot be used with an old schema, a testnet domain,
// missing independent history, provisional flags or a mismatched current pin.
func TestMainnetRuntimeObservationRejectsConfigAuthorityDrift(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*ReleaseConfig)
	}{
		{"schema", func(value *ReleaseConfig) { value.SchemaVersion = 1 }},
		{"testnet chain", func(value *ReleaseConfig) { value.ChainID = 945 }},
		{"testnet genesis", func(value *ReleaseConfig) { value.GenesisHash = provisionalRuntimeTestnetGenesis }},
		{"testnet policy", func(value *ReleaseConfig) { value.Policy.NetworkProfile = "testnet" }},
		{"provisional", func(value *ReleaseConfig) {
			value.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
		}},
		{"deferral", func(value *ReleaseConfig) { value.ProvisionalDeferClosedNativeInput = true }},
		{"omitted approvals", func(value *ReleaseConfig) { value.MainnetRuntimeApprovals = nil }},
		{"omitted predecessor", func(value *ReleaseConfig) { value.MainnetRuntimeApprovals = value.MainnetRuntimeApprovals[1:] }},
		{"tail pin", func(value *ReleaseConfig) { value.RuntimeSpec++ }},
		{"size", func(value *ReleaseConfig) { value.MainnetRuntimeApprovals[0].Bytes++ }},
		{"digest", func(value *ReleaseConfig) { value.MainnetRuntimeApprovals[0].SHA256 = (types.Hash{1}).Hex() }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newMainnetRuntimeTestFixture(t)
			testCase.change(&fixture.cfg)
			if cfg, err := LoadMainnetRuntimeObservationConfig(writeReleaseConfig(t, fixture.cfg)); err == nil || cfg != nil {
				t.Fatalf("invalid config admitted: %v", err)
			}
		})
	}
}

// Hash-pinning does not permit ambiguous, extended or concatenated JSON.
func TestMainnetRuntimeObservationRejectsAmbiguousApprovalBytes(t *testing.T) {
	for _, suffix := range []string{`,"revision":2}`, `,"unknown":true}`, `} {}`} {
		fixture := newMainnetRuntimeTestFixture(t)
		raw, err := json.Marshal(fixture.approvals[1])
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw[:len(raw)-1], suffix...)
		fixture.cfg.MainnetRuntimeApprovals[1] = mainnetRuntimeTestWriteBytes(t, fixture.cfg.MainnetRuntimeApprovals[1].Path, raw)
		if cfg, err := LoadMainnetRuntimeObservationConfig(writeReleaseConfig(t, fixture.cfg)); err == nil || cfg != nil {
			t.Fatalf("ambiguous document admitted: suffix=%q err=%v", suffix, err)
		}
	}
}

// Guard the concrete key/journal/sign/replay/submit boundaries, including a
// matching version which previously bypassed the config's admission purpose.
func TestMainnetRuntimeObservationRefusesSigningBeforeIo(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.native.Runtime = &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: types.U32(cfg.RuntimeSpec), TransactionVersion: 1}
	if err := validateReleaseNativeSigningRuntime(fixture.native, cfg); err == nil {
		t.Fatal("matching version laundered observation into signing")
	}
	if err := validatePreparedNativeRuntimeContext(context.Background(), fixture.native, cfg, mainnetRuntimeTestBlock(130), mainnetRuntimeTestBlock(150)); err == nil {
		t.Fatal("observation authorized prepared replay")
	}
	if receipt, submitted, err := submitPreparedNativeRuntimeContext(context.Background(), fixture.native, cfg, &crv4.PreparedSubmission{}); err == nil || submitted || receipt != nil {
		t.Fatalf("observation attempted submission: submitted=%t err=%v", submitted, err)
	}
	if _, err := loadReleaseHotkey(cfg); err == nil || !strings.Contains(err.Error(), "observation approval") {
		t.Fatalf("observation reached key read: %v", err)
	}
	if _, err := openReleaseNativeJournal(cfg); err == nil {
		t.Fatal("observation opened a transaction journal")
	}
	if _, err := authenticateReleaseValidatorStakeContext(context.Background(), fixture.native, cfg, [32]byte{1}, 1); err == nil {
		t.Fatal("runtime identity observation authorized startup stake eligibility")
	}
	if len(fixture.callKVs) != 0 {
		t.Fatalf("writer refusal made Rpc calls: %v", fixture.callKVs)
	}
	if _, err := os.Stat(cfg.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("writer refusal created state: %v", err)
	}
}

// Frozen config/history supports independent concurrent historical readers;
// serialization still names the original runtime rather than observed state.
func TestMainnetRuntimeObservationConcurrentHistory(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	done := make(chan error, 12)
	for index := range 12 {
		go func() {
			<-start
			number := uint64(50)
			if index%2 == 0 {
				number = 150
			}
			got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, mainnetRuntimeTestBlock(number))
			if err == nil && (got.BlockNumber != number || got.Runtime.Version.SpecVersion != uint32(9_001+number/100)) {
				err = fmt.Errorf("wrong historical observation: %+v", got)
			}
			done <- err
		}()
	}
	close(start)
	for range 12 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	after, err := json.Marshal(cfg)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("concurrent observations changed serialized authority: %v", err)
	}
}
