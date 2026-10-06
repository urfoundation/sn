// Production pins are synthetic and independent of the retained testnet
// catalog. Tests drive fresh finalized reads and deliberate state changes.
package miner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Each missing or foreign approval coordinate fails independently.
func TestFleetMainnetAuthorityRejectsIncompleteAndForeignScope(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	for _, item := range []struct {
		name   string
		change func(*fleetMainnetRuntimeAuthority)
	}{
		{name: "schema", change: func(value *fleetMainnetRuntimeAuthority) { value.Schema = "observation" }},
		{name: "chain", change: func(value *fleetMainnetRuntimeAuthority) { value.EvmChainId = 945 }},
		{name: "subnet", change: func(value *fleetMainnetRuntimeAuthority) { value.Netuid++ }},
		{name: "coordinator", change: func(value *fleetMainnetRuntimeAuthority) { value.Coordinator = "0x" + strings.Repeat("aa", 20) }},
		{name: "missing native name", change: func(value *fleetMainnetRuntimeAuthority) { value.NativeChain = "" }},
		{name: "testnet genesis", change: func(value *fleetMainnetRuntimeAuthority) { value.GenesisHash = fleetProvisionalTestnetGenesis }},
		{name: "zero genesis", change: func(value *fleetMainnetRuntimeAuthority) { value.GenesisHash = "0x" + strings.Repeat("00", 32) }},
		{name: "missing source", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeSourceCommit = "" }},
		{name: "source tag", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeSourceCommit = "v1.0" }},
		{name: "missing review", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeReviewSha256 = "" }},
		{name: "profile observation", change: func(value *fleetMainnetRuntimeAuthority) {
			value.RuntimeReviewScope = crv4.ProvisionalRuntimeCompatibilityProfile
		}},
		{name: "missing code", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeCodeHash = "" }},
		{name: "missing metadata", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeMetadataHash = "" }},
		{name: "missing spec", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeVersion.SpecVersion = 0 }},
		{name: "missing transaction", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeVersion.TransactionVersion = 0 }},
		{name: "missing state", change: func(value *fleetMainnetRuntimeAuthority) { value.RuntimeVersion.StateVersion = 0 }},
	} {
		value := fixture.authority
		item.change(&value)
		if err := value.validate(fixture.manifest); err == nil {
			t.Errorf("%s authorized", item.name)
		}
	}
	if err := fixture.authority.validate(fixture.manifest); err != nil {
		t.Fatal(err)
	}
}

// A content pin cannot bless ambiguous, oversized or unknown document fields.
func TestFleetMainnetAuthorityFileRequiresExactApprovedBytes(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	raw, err := json.Marshal(fixture.authority)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name         string
		raw          []byte
		matchingHash bool
	}{
		{name: "changed bytes", raw: append(append([]byte(nil), raw...), '\n')},
		{name: "duplicate key", raw: append([]byte(`{"schema":"forged",`), raw[1:]...), matchingHash: true},
		{name: "trailing value", raw: append(append([]byte(nil), raw...), []byte(` {}`)...), matchingHash: true},
		{name: "unknown field", raw: append([]byte(`{"observed_provisional":true,`), raw[1:]...), matchingHash: true},
		{name: "oversized", raw: append(append([]byte(nil), raw...), []byte(strings.Repeat(" ", fleetMainnetAuthorityLimit))...), matchingHash: true},
	} {
		if err := os.WriteFile(fleetOpt(fixture.opts, "--mainnet-runtime-authority"), item.raw, 0600); err != nil {
			t.Fatal(err)
		}
		approved := sha256.Sum256(raw)
		if item.matchingHash {
			approved = sha256.Sum256(item.raw)
		}
		fixture.opts["--mainnet-runtime-authority-sha256"] = hex.EncodeToString(approved[:])
		if _, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest); err == nil {
			t.Errorf("%s accepted", item.name)
		}
	}
}

// Production and testnet approval domains cannot be combined or substituted.
func TestFleetMainnetAuthorityCannotInheritTestnetProvisionalFlags(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--provisional-runtime-compatibility"] = crv4.ProvisionalRuntimeCompatibilityProfile
	fixture.opts["--runtime-observation-dir"] = t.TempDir()
	if _, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest); err == nil {
		t.Fatal("production combined with testnet exception")
	}
	if _, err := loadFleetMainnetRuntimeAuthority(fixture.opts, &protocol.FleetManifest{ChainID: 945}); err == nil {
		t.Fatal("production approval reused for testnet")
	}
	for _, id := range []uint64{0, 1, 964} {
		if _, err := loadFleetMainnetRuntimeAuthority(docopt.Opts{}, &protocol.FleetManifest{ChainID: id}); err == nil {
			t.Errorf("chain%d inherited release authority", id)
		}
	}
	if authority, err := loadFleetMainnetRuntimeAuthority(docopt.Opts{}, &protocol.FleetManifest{ChainID: 945}); err != nil || authority != nil {
		t.Fatal("legacy strict testnet selection changed")
	}
}

// An explicitly approved artifact outside the testnet catalog gets its own view.
func TestFleetMainnetRuntimeUsesIndependentExactArtifact(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if _, err := authenticateFleetRuntimeAtContext(t.Context(), chain, fixture.head); err == nil {
		t.Fatal("legacy release pin admitted independent production runtime")
	}
	previousMeta, previousRuntime := chain.Meta, chain.Runtime
	view, block, err := fixture.authority.finalizedView(t.Context(), chain)
	if err != nil {
		t.Fatal(err)
	}
	if view == chain || block != fixture.head || view.Runtime.SpecVersion != types.U32(fixture.authority.RuntimeVersion.SpecVersion) || view.Meta == nil || chain.Meta != previousMeta || chain.Runtime != previousRuntime {
		t.Fatal("mainnet view mutated the connection or substituted release metadata")
	}
	if chain.ProvisionalRuntimeCompatibilityEnabled() || view.CurrentRuntimeCompatibilityProfile() != "" {
		t.Fatal("mainnet exact approval inherited provisional authority")
	}
	if _, err := view.NewSetFleetCommitmentCall(fixture.manifest.Netuid, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.authority.commitmentFinalized(t.Context(), chain, fixture.manifest.Netuid, fixture.manifest.Hotkey); err != nil {
		t.Fatal(err)
	}
}

// Metadata reuse cannot authorize a changed network identity.
func TestFleetMainnetRuntimeChecksFreshGenesisDespiteCachedMetadata(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if _, _, err := fixture.authority.finalizedView(t.Context(), chain); err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.genesis[0]++
	fixture.stateLock.Unlock()
	if _, err := fixture.authority.commitmentFinalized(t.Context(), chain, 25, fixture.manifest.Hotkey); err == nil {
		t.Fatal("cached metadata authorized changed genesis")
	}
	if fixture.count("state_getStorage") != 0 {
		t.Fatal("changed genesis reached commitment decoding")
	}
}

// Every version and byte-identity coordinate participates in admission.
func TestFleetMainnetRuntimeRejectsEveryArtifactCoordinate(t *testing.T) {
	for _, item := range []struct {
		name   string
		change func(*fleetMainnetTestFixture)
	}{
		{name: "spec name", change: func(value *fleetMainnetTestFixture) { value.version.SpecName += "-changed" }},
		{name: "spec version", change: func(value *fleetMainnetTestFixture) { value.version.SpecVersion++ }},
		{name: "transaction", change: func(value *fleetMainnetTestFixture) { value.version.TransactionVersion++ }},
		{name: "state", change: func(value *fleetMainnetTestFixture) { value.version.StateVersion++ }},
		{name: "code", change: func(value *fleetMainnetTestFixture) { value.code = (types.Hash{0xff}).Hex() }},
		{name: "metadata", change: func(value *fleetMainnetTestFixture) { value.metadata, _ = fleetRuntimeTestMetadata(t) }},
	} {
		fixture := newFleetMainnetTestFixture(t)
		chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
		if err != nil {
			t.Fatal(err)
		}
		fixture.stateLock.Lock()
		item.change(fixture)
		fixture.stateLock.Unlock()
		_, _, err = fixture.authority.finalizedView(t.Context(), chain)
		chain.API.Client.Close()
		if err == nil {
			t.Errorf("%s drift authorized", item.name)
		}
	}
}

// A receipt's runtime is approved before its commitment state is interpreted.
func TestFleetMainnetReceiptRuntimeDriftPrecedesStorageDecoding(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if _, _, err := fixture.authority.finalizedView(t.Context(), chain); err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.version.SpecVersion++
	fixture.stateLock.Unlock()
	hash, _ := fixture.manifest.CommitmentHash()
	receipt := &crv4.FinalizedExtrinsic{BlockHash: fixture.receiptBlock, BlockNumber: 102, ExtrinsicHash: types.Hash{0x61}}
	if _, err := fixture.authority.commitmentWrite(t.Context(), chain, 25, fixture.manifest.Hotkey, hash, receipt); err == nil {
		t.Fatal("receipt upgrade inherited the signing artifact")
	}
	if fixture.count("state_getStorage") != 0 {
		t.Fatal("unapproved receipt runtime reached storage decoding")
	}
}

// Matching old commitment bytes cannot impersonate a later included write.
func TestFleetMainnetReceiptBindsExactWriteBlock(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	hash, _ := fixture.manifest.CommitmentHash()
	receipt := &crv4.FinalizedExtrinsic{BlockHash: fixture.head, BlockNumber: 100, ExtrinsicHash: types.Hash{0x61}}
	verified, err := fixture.authority.commitmentWrite(t.Context(), chain, 25, fixture.manifest.Hotkey, hash, receipt)
	if err != nil || verified.ExtrinsicHash != receipt.ExtrinsicHash {
		t.Fatalf("approved receipt: %v", err)
	}
	receipt.BlockHash, receipt.BlockNumber = fixture.receiptBlock, 102
	if _, err := fixture.authority.commitmentWrite(t.Context(), chain, 25, fixture.manifest.Hotkey, hash, receipt); err == nil {
		t.Fatal("old identical commitment impersonated a new write")
	}
}

// Even an explicitly installed testnet profile cannot enter production admission.
func TestFleetMainnetRuntimeRejectsInstalledProvisionalAuthority(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if err := chain.EnableProvisionalRuntimeCompatibility(fixture.genesis, func(crv4.AuthenticatedRuntimeArtifact) error {
		t.Error("production reached provisional observation")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.authority.finalizedView(t.Context(), chain); err == nil {
		t.Fatal("production reused a connection carrying provisional authority")
	}
}

// A command retains its original approval when the source file is replaced.
func TestFleetMainnetAuthoritySnapshotDoesNotReloadChangedFile(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.authority.RuntimeVersion.SpecVersion++
	fixture.writeAuthority(t)
	if authority.RuntimeVersion.SpecVersion != 8001 || authority.artifactIdentity().Version.SpecVersion != 8001 {
		t.Fatal("authority changed after owner admission")
	}
	chain, _, err := dialFleetNativeAuthorityContext(t.Context(), fixture.opts, fixture.manifest, authority)
	if err != nil {
		t.Fatal(err)
	}
	chain.API.Client.Close()
}
