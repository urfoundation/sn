//go:build linux || darwin

// The settlement v2 block is an addition: today's signed configs keep their
// hashes, and an adopted block is spelled exactly and confirmed by the chain.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"gopkg.in/yaml.v3"
)

// The block every settlement v2 fixture adopts.
var settlementV2TestBlock = ReleaseSettlementV2Config{Settlement: "0x5e77000000000000000000000000000000000001", Vault: "0x5e77000000000000000000000000000000000002"}

// Computed by the binary before settlement_v2 existed (f92aec6e), when it
// signed the fixture in testdata/settlement-v2-today: the owner-recycle
// original inside the history bundle, and today's treasury successor that
// retains it. The bundle is the history entry today's config gives its
// successor, as snv2 1530b0bc (also without the block) builds it. Never
// regenerate these with a later binary.
const (
	settlementV2TodayConfigHash         = "0xbaaaef11cada1538e70bcfef4a4746813bfbf4a864ebd27393bc76dd0e088b78"
	settlementV2TodayOriginalConfigHash = "0x8b430b102c5f689f259c62afd6d6676cdf2fead245611c0f840bf9631d971d22"
	settlementV2TodayConfigSeal         = "0xe69e1dbeb34472dddbe9534c673324fb8d6dd98e68cdb9885b5496679170f8a4"
	settlementV2TodayBundle             = "0x02d029d47eae8f4fef405b05fc4a2fd296489a200cdd474ca4e263fddd4b6067"
)

// VA-6: today's validator.yml and its history bundle, written and signed by
// the binary before settlement_v2 existed, load unchanged through the server
// staging loader. Each signature names the hash that earlier binary computed,
// and the bundle an adoption appends is the one that binary would build.
func TestSettlementV2TodayConfigAndHistoryHashUnchanged(t *testing.T) {
	staging := identityTestStateDir(t)
	source := filepath.Join("testdata", "settlement-v2-today")
	entries, err := os.ReadDir(source)
	if err != nil || len(entries) != 3 {
		t.Fatal("today's fixture is incomplete", err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(staging, entry.Name()), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(staging, "validator.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeReleaseConfigBytesMode(path, raw, releaseConfigLoadMode{stagingConfigDirectory: staging})
	if err != nil {
		t.Fatal("today's signed config or history bundle no longer loads", err)
	}
	if cfg.SettlementV2 != nil || cfg.TreasuryApproval == nil || len(cfg.productionAuthorityHistory.entries) != 1 {
		t.Fatal("fixture is not today's treasury config with one original")
	}
	original := cfg.productionAuthorityHistory.entries[0].config
	current, currentErr := OwnerRecycleConfigHash(cfg)
	prior, priorErr := OwnerRecycleConfigHash(original)
	seal, sealErr := json.Marshal(cfg)
	bundle, bundleErr := BuildOwnerRecycleProductionAuthority(t.Context(), cfg)
	if err := errors.Join(currentErr, priorErr, sealErr, bundleErr); err != nil {
		t.Fatal(err)
	}
	if attemptHex32(current) != settlementV2TodayConfigHash || attemptHex32(prior) != settlementV2TodayOriginalConfigHash ||
		attemptHex32(sha256.Sum256(seal)) != settlementV2TodayConfigSeal || bytes.Contains(seal, []byte("settlement_v2")) ||
		attemptHex32(sha256.Sum256(bundle)) != settlementV2TodayBundle {
		t.Fatal("today's config, its original, its history seal or its successor's history bundle hashes differently")
	}
}

// A signed schema-3 config carrying block, loaded by the producer loader.
func settlementV2TestLoad(t *testing.T, block *ReleaseSettlementV2Config) (*ReleaseConfig, error) {
	t.Helper()
	runtime := newProductionRuntimeTestFixture(t, false)
	cfg, approval := productionPendingTestClone(t, *runtime.cfg), productionPendingTestClone(t, runtime.approval)
	productionPendingTestTreasury(&cfg, &approval)
	cfg.TreasuryApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
	cfg.SettlementV2 = block
	path, _ := productionPendingTestSign(t, cfg, approval, runtime.private)
	return LoadReleaseConfig(path)
}

// VA-6: an adopted block loads with its exact spelling, appears in both wire
// forms and is what steering, the production row, replay and the startup
// identity check read through releaseSettlementV2Addresses.
func TestSettlementV2ConfigLoadsAndNamesAddresses(t *testing.T) {
	block := settlementV2TestBlock
	cfg, err := settlementV2TestLoad(t, &block)
	if err != nil {
		t.Fatal(err)
	}
	want := SettlementV2Addresses{Settlement: common.HexToAddress(block.Settlement), Vault: common.HexToAddress(block.Vault)}
	if releaseSettlementV2Addresses(cfg) != want || *cfg.SettlementV2 != block {
		t.Fatal("loaded config does not name its settlement_v2 addresses")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil || !bytes.Contains(encoded, []byte(`"settlement_v2":{"settlement":"`+block.Settlement+`","vault":"`+block.Vault+`"}`)) {
		t.Fatal("JSON omits the adopted block", err)
	}
	document, err := yaml.Marshal(cfg)
	if err != nil || !bytes.Contains(document, []byte("settlement_v2:\n    settlement: "+block.Settlement+"\n    vault: "+block.Vault+"\n")) {
		t.Fatal("YAML omits the adopted block", err, string(document))
	}
	if releaseSettlementV2Addresses(&ReleaseConfig{}).configured() {
		t.Fatal("a config without the block named v2 addresses")
	}
}

// VA-6: a non-lowercase, zero or malformed address is refused at load, even
// when the approval key signed it.
func TestSettlementV2ConfigRefusesMalformedAddresses(t *testing.T) {
	checksum := common.HexToAddress(settlementV2TestBlock.Settlement).Hex()
	if checksum == settlementV2TestBlock.Settlement {
		t.Fatal("control address has no checksum case")
	}
	for name, item := range map[string]struct {
		block ReleaseSettlementV2Config
		field string
	}{
		"settlement checksum case": {block: ReleaseSettlementV2Config{Settlement: checksum, Vault: settlementV2TestBlock.Vault}, field: "settlement"},
		"vault uppercase":          {block: ReleaseSettlementV2Config{Settlement: settlementV2TestBlock.Settlement, Vault: "0x" + strings.ToUpper(settlementV2TestBlock.Vault[2:])}, field: "vault"},
		"vault 0X prefix":          {block: ReleaseSettlementV2Config{Settlement: settlementV2TestBlock.Settlement, Vault: "0X" + settlementV2TestBlock.Vault[2:]}, field: "vault"},
		"settlement zero":          {block: ReleaseSettlementV2Config{Settlement: "0x" + strings.Repeat("0", 40), Vault: settlementV2TestBlock.Vault}, field: "settlement"},
		"vault zero":               {block: ReleaseSettlementV2Config{Settlement: settlementV2TestBlock.Settlement, Vault: "0x" + strings.Repeat("0", 40)}, field: "vault"},
		"settlement without 0x":    {block: ReleaseSettlementV2Config{Settlement: settlementV2TestBlock.Settlement[2:], Vault: settlementV2TestBlock.Vault}, field: "settlement"},
		"vault short":              {block: ReleaseSettlementV2Config{Settlement: settlementV2TestBlock.Settlement, Vault: settlementV2TestBlock.Vault[:41]}, field: "vault"},
		"settlement not hex":       {block: ReleaseSettlementV2Config{Settlement: settlementV2TestBlock.Settlement[:41] + "g", Vault: settlementV2TestBlock.Vault}, field: "settlement"},
		"vault absent":             {block: ReleaseSettlementV2Config{Settlement: settlementV2TestBlock.Settlement}, field: "vault"},
	} {
		block := item.block
		if _, err := settlementV2TestLoad(t, &block); err == nil || !strings.Contains(err.Error(), "settlement_v2."+item.field+" must be a nonzero lowercase 0x address") {
			t.Errorf("%s: malformed address admitted or refused for another cause: %v", name, err)
		}
	}
}

// VA-6: an unsigned legacy config carries the block under the same spelling
// rule; only production's signed lineage adds the successor class.
func TestSettlementV2ConfigLoadsInLegacySchema(t *testing.T) {
	cfg := validReleaseConfig(t)
	block := settlementV2TestBlock
	cfg.SettlementV2 = &block
	loaded, err := LoadReleaseConfig(writeReleaseConfig(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != ReleaseValidatorSchemaVersion || releaseSettlementV2Addresses(loaded) != (SettlementV2Addresses{Settlement: common.HexToAddress(block.Settlement), Vault: common.HexToAddress(block.Vault)}) {
		t.Fatal("legacy config does not name its settlement_v2 addresses")
	}
	cfg.SettlementV2 = &ReleaseSettlementV2Config{Settlement: common.HexToAddress(block.Settlement).Hex(), Vault: block.Vault}
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil || !strings.Contains(err.Error(), "settlement_v2.settlement must be a nonzero lowercase 0x address") {
		t.Fatal("legacy config admitted a checksum-case address", err)
	}
}

// A getter of one target, keyed as the fake chains below answer it; the
// selector is go-ethereum's Keccak-256 of the pinned signature.
func settlementV2TestKey(target, signature string) string {
	return target + "/" + common.Bytes2Hex(crypto.Keccak256([]byte(signature))[:4])
}

// One ABI address word.
func settlementV2TestWord(address string) []byte {
	return common.LeftPadBytes(common.HexToAddress(address).Bytes(), 32)
}

// The chain's honest answers for settlementV2TestBlock under coordinator.
func settlementV2TestAnswers(coordinator string) map[string][]byte {
	settlement, vault := settlementV2TestBlock.Settlement, settlementV2TestBlock.Vault
	return map[string][]byte{
		settlementV2TestKey(settlement, "vault()"):         settlementV2TestWord(vault),
		settlementV2TestKey(settlement, "v1Coordinator()"): settlementV2TestWord(coordinator),
		settlementV2TestKey(vault, "coordinator()"):        settlementV2TestWord(settlement),
	}
}

// Records each exact-block eth_call and answers it by target and calldata.
type settlementV2TestReader struct {
	answers map[string][]byte
	err     error
	calls   []string
}

func (self *settlementV2TestReader) ethCallAtHashContext(_ context.Context, to common.Address, calldata []byte, block uint64, blockHash [32]byte) ([]byte, error) {
	if block != 77 || blockHash != ([32]byte{0x77}) {
		return nil, errors.New("settlement v2 read escaped its pinned block")
	}
	key := strings.ToLower(to.Hex()) + "/" + common.Bytes2Hex(calldata)
	self.calls = append(self.calls, key)
	if self.err != nil {
		return nil, self.err
	}
	return self.answers[key], nil
}

// VA-6: the three pinned getters agree at the pinned block; a config without
// the block reads nothing.
func TestSettlementV2IdentityCheckReadsThreePinnedGetters(t *testing.T) {
	cfg := &ReleaseConfig{Coordinator: "0x1111111111111111111111111111111111111111"}
	reader := &settlementV2TestReader{answers: settlementV2TestAnswers(cfg.Coordinator)}
	if err := verifyReleaseSettlementV2Context(t.Context(), reader, cfg, 77, [32]byte{0x77}); err != nil || len(reader.calls) != 0 {
		t.Fatal("a config without settlement_v2 read the chain", err)
	}
	block := settlementV2TestBlock
	cfg.SettlementV2 = &block
	if err := verifyReleaseSettlementV2Context(t.Context(), reader, cfg, 77, [32]byte{0x77}); err != nil {
		t.Fatal(err)
	}
	if len(reader.calls) != 3 {
		t.Fatal("identity check did not read exactly the three getters", reader.calls)
	}
	for _, pinned := range [][2][]byte{
		{settlementV2VaultSelector[:], stabi.NewSTSettlement2().PackVault()},
		{settlementV2V1CoordinatorSelector[:], stabi.NewSTSettlement2().PackV1Coordinator()},
		{settlementV2CoordinatorSelector[:], stabi.NewSTSettlementVault2().PackCoordinator()},
	} {
		if !bytes.Equal(pinned[0], pinned[1]) {
			t.Fatal("a pinned getter differs from the contract ABI", pinned)
		}
	}
	for _, call := range reader.calls {
		if _, exists := reader.answers[call]; !exists {
			t.Fatal("identity check read an unpinned getter", call)
		}
	}
}

// VA-6: each getter's mismatch is refused with its own cause, and a read
// failure is never mistaken for agreement.
func TestSettlementV2IdentityCheckRefusesEachMismatch(t *testing.T) {
	coordinator := "0x1111111111111111111111111111111111111111"
	block := settlementV2TestBlock
	cfg := &ReleaseConfig{Coordinator: coordinator, SettlementV2: &block}
	for getter, key := range map[string]string{
		"STSettlement2.vault()":            settlementV2TestKey(block.Settlement, "vault()"),
		"STSettlement2.v1Coordinator()":    settlementV2TestKey(block.Settlement, "v1Coordinator()"),
		"STSettlementVault2.coordinator()": settlementV2TestKey(block.Vault, "coordinator()"),
	} {
		reader := &settlementV2TestReader{answers: settlementV2TestAnswers(coordinator)}
		reader.answers[key] = settlementV2TestWord("0x5e77000000000000000000000000000000000003")
		if err := verifyReleaseSettlementV2Context(t.Context(), reader, cfg, 77, [32]byte{0x77}); err == nil || !strings.Contains(err.Error(), "settlement_v2 "+getter+" at block 77 returned") {
			t.Errorf("%s mismatch admitted or refused for another cause: %v", getter, err)
		}
	}
	reader := &settlementV2TestReader{answers: settlementV2TestAnswers(coordinator)}
	reader.answers[settlementV2TestKey(block.Settlement, "vault()")] = common.HexToAddress(block.Vault).Bytes()
	if err := verifyReleaseSettlementV2Context(t.Context(), reader, cfg, 77, [32]byte{0x77}); err == nil || !strings.Contains(err.Error(), "settlement_v2 STSettlement2.vault() at block 77 returned") {
		t.Fatal("an unpadded address word was admitted", err)
	}
	canary := errors.New("synthetic settlement v2 read unavailable")
	reader = &settlementV2TestReader{answers: settlementV2TestAnswers(coordinator), err: canary}
	if err := verifyReleaseSettlementV2Context(t.Context(), reader, cfg, 77, [32]byte{0x77}); !errors.Is(err, canary) || len(reader.calls) != 1 {
		t.Fatal("a failed read was not returned at the first getter", err)
	}
}

// VA-6: the V2 steerer that both runtime paths construct takes the block's
// addresses from its config through releaseSettlementV2Addresses, the helper
// replay and the production row also call.
func TestSettlementV2SteererTakesConfiguredAddresses(t *testing.T) {
	block := settlementV2TestBlock
	_, steerer := newReleaseStartupOwnerV2TestFixture(t, func(cfg *ReleaseConfig) { cfg.SettlementV2 = &block })
	if steerer.settlementV2 != (SettlementV2Addresses{Settlement: common.HexToAddress(block.Settlement), Vault: common.HexToAddress(block.Vault)}) {
		t.Fatal("the steerer did not take the configured settlement v2 addresses", steerer.settlementV2)
	}
}

// Replaces the settlement v2 views the runtime fixture's chain serves.
func (self *releaseRuntimeV2ChainTestFixture) serveSettlementV2(views map[string][]byte) {
	self.settlementV2Lock.Lock()
	defer self.settlementV2Lock.Unlock()
	self.settlementV2 = views
}

// The settlement v2 views the runtime fixture's chain has served.
func (self *releaseRuntimeV2ChainTestFixture) settlementV2ReadCount() int {
	self.settlementV2Lock.Lock()
	defer self.settlementV2Lock.Unlock()
	return self.settlementV2Reads
}

// VA-6: the runtime's first advance confirms the block at its finalized
// snapshot before it closes anything. A mismatch stops it with no epoch closed
// and nothing published; the same runtime checks again at its next advance,
// and once the check passes it is not repeated.
func TestSettlementV2RuntimeConfirmsIdentityBeforeClosing(t *testing.T) {
	block := settlementV2TestBlock
	fixture := newReleaseRuntimeV2TestFixtureWithBounds(t, nil, func(cfg *ReleaseConfig) { cfg.SettlementV2 = &block })
	honest := settlementV2TestAnswers(fixture.startup.cfg.Coordinator)
	other := maps.Clone(honest)
	other[settlementV2TestKey(block.Settlement, "vault()")] = settlementV2TestWord("0x5e77000000000000000000000000000000000003")
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	before := fixture.runtime.history.current[fixture.runtime.history.participants[0].NoID].epoch
	fixture.evm.serveSettlementV2(other)
	if err := fixture.runtime.advance(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "settlement_v2 STSettlement2.vault() at block 2001 returned") {
		t.Fatalf("runtime admitted another vault or stopped for another cause: %v", err)
	}
	for _, participant := range fixture.runtime.history.participants {
		if fixture.runtime.history.current[participant.NoID].epoch != before {
			t.Fatal("a refused identity closed an epoch")
		}
	}
	for _, store := range fixture.stores {
		if len(store.counts()) != 0 {
			t.Fatal("a refused identity published evidence")
		}
	}
	fixture.evm.serveSettlementV2(honest)
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if fixture.runtime.history.current[fixture.runtime.history.participants[0].NoID].epoch != 9 || fixture.evm.settlementV2ReadCount() != 4 {
		t.Fatal("the confirmed runtime did not close its epochs after one failed and three good reads", fixture.evm.settlementV2ReadCount())
	}
	fixture.evm.serveSettlementV2(other)
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil || fixture.evm.settlementV2ReadCount() != 4 {
		t.Fatal("a confirmed runtime checked its identity again", err)
	}
}

// VA-9: CLOSED_CENSUS continues across adoption. Epoch 7 closes and publishes
// under the predecessor; the runtime restarts on the same disk with the block,
// confirms it, closes epoch 8 and publishes it, and both censuses read back
// under the original activations. Production runs this same census path.
func TestSettlementV2ClosedCensusContinuesAcrossAdoption(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	closed := map[uint64]uint64{7: 1501, 8: 2001}
	advance := func(epoch uint64) {
		t.Helper()
		block := closed[epoch]
		if err := fixture.runtime.advance(t.Context(), &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(epoch + 1), BlockNumber: block, BlockHash: fixture.startup.blocks[block]}); err != nil {
			t.Fatal(err)
		}
	}
	advance(7)
	fixture.startup.reopen(t)
	block := settlementV2TestBlock
	fixture.startup.cfg.SettlementV2 = &block
	fixture.evm.serveSettlementV2(settlementV2TestAnswers(fixture.startup.cfg.Coordinator))
	fixture.start(t)
	advance(8)
	if fixture.evm.settlementV2ReadCount() != 3 {
		t.Fatal("the adopted runtime did not confirm its block once", fixture.evm.settlementV2ReadCount())
	}
	for epoch, finalized := range closed {
		path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, epoch)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, fixture.startup.cfg.EvidenceV2.Bounds.MaxClosureBytes, 2)
		if err != nil {
			t.Fatal(err)
		}
		options := ValidatorEvidencePublicationV2ReadOptions{Origins: fixture.origins, Bounds: fixture.startup.cfg.EvidenceV2.Bounds, Window: protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: 1001 + 500*(epoch-7), EndBlock: 1501 + 500*(epoch-7), FinalizedBlock: finalized}}
		for _, input := range fixture.startup.inputs {
			options.Activations = append(options.Activations, input.Context.Activation)
		}
		if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), manifest, options); err != nil || len(publication.Members) != 2 {
			t.Fatalf("epoch %d closed census across adoption: %v", epoch, err)
		}
	}
}
