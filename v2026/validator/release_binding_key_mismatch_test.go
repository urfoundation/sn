//go:build linux || darwin

package validator

// The coordinator accepts any registered hotkey's binding of any client id
// under the binder's own client key, so a binding alone says nothing about the
// measured client. These controls give a measured provider a foreign binding,
// keep a fleet's binding across the client's key rotation, and clear a
// client's key, then require the collector and every verifier to score the
// provider exactly as an unbound one. The binding row records the exclusion:
// its chain key beside the local key from the retained signed capture. A local
// key that capture does not state is refused in either direction, so neither a
// binder nor a dishonest collector can move a provider into or out of a fleet.

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// The binder's own synthetic fleet, hotkey, client key and commitment. Only
// the client id is taken from the measured provider it claims.
var (
	releaseKeyMismatchTestFleetId    = [32]byte{0xf1, 0x01}
	releaseKeyMismatchTestHotkey     = [32]byte{0xf2, 0x02}
	releaseKeyMismatchTestClientKey  = [32]byte{0xf3, 0x03}
	releaseKeyMismatchTestCommitment = [32]byte{0xf4, 0x04}
)

// The binder's hotkey holds its own live native UID.
const releaseKeyMismatchTestUid = uint16(77)

// Selects the first live binding of noId whose chain and local keys match.
func releaseKeyMismatchTestVictim(t testing.TB, artifact *ReleaseMeasurementArtifact, noId uint64) (int, connect.Id) {
	t.Helper()
	for index, binding := range artifact.Bindings {
		if binding.NoID == noId && binding.Active && binding.LiveUIDFound && binding.LiveUID == binding.RecordUID && binding.LocalClientKey == binding.ClientKey {
			clientId, err := connect.ParseId(binding.ClientID)
			if err != nil {
				t.Fatal(err)
			}
			return index, clientId
		}
	}
	t.Fatalf("no_id %d has no live matched binding to claim", noId)
	return 0, connect.Id{}
}

// The row bindingAt yields once the binder has claimed the victim's client id:
// active and live, but naming the binder's fleet, hotkey and client key. The
// victim's own local key is unchanged.
func releaseKeyMismatchTestForeign(victim ReleaseBindingMeasurement) ReleaseBindingMeasurement {
	foreign := victim
	foreign.FleetID, foreign.Hotkey = releaseHex32(releaseKeyMismatchTestFleetId), releaseHex32(releaseKeyMismatchTestHotkey)
	foreign.ClientKey, foreign.CommitmentHash = releaseHex32(releaseKeyMismatchTestClientKey), releaseHex32(releaseKeyMismatchTestCommitment)
	foreign.Generation = victim.Generation + 1
	foreign.RecordUID, foreign.LiveUIDFound, foreign.LiveUID = releaseKeyMismatchTestUid, true, releaseKeyMismatchTestUid
	return foreign
}

// The same genuine measurement with the victim's binding inactive. The v1
// claim path, not the code under test, fixes its head transcript and decision.
func releaseKeyMismatchTestUnboundOracle(t testing.TB, measurement *releaseMeasurementV2TestFixture, index int) *releaseMeasurementV2TestFixture {
	t.Helper()
	oracle := copyReleaseMeasurementTestProvider(t, measurement)
	unbound := &oracle.artifact.Bindings[index]
	unbound.Active, unbound.LiveUIDFound, unbound.LiveUID = false, false, 0
	unbound.LocalClientKey, unbound.ClientKeyObservationHash = releaseHex32([32]byte{}), ""
	oracle.rebuildLegacy(t)
	if oracle.want == nil {
		t.Fatal("the unbound oracle has no positive decision to compare")
	}
	return oracle
}

// Rebuilds every row from the fixture's chain answers and native UIDs, then
// takes each active row's local key only from its retained signed capture.
// No collector output is consulted.
func releaseKeyMismatchTestRetainedRows(t *testing.T, fixture *releaseHeadV2TestFixture) []ReleaseBindingMeasurement {
	t.Helper()
	custody := &releaseEvidenceV2StartupReferences{remaining: fixture.steerer.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	defer func() {
		if err := errors.Join(custody.check(), custody.close()); err != nil {
			t.Fatal(err)
		}
	}()
	rows := make([]ReleaseBindingMeasurement, 0, len(fixture.measurement.artifact.Bindings))
	for _, original := range fixture.measurement.artifact.Bindings {
		clientId, err := connect.ParseId(original.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		chain := fixture.rpc.bindingKVs[clientId]
		row := ReleaseBindingMeasurement{
			NoID: original.NoID, ClientID: original.ClientID, Active: chain.Active,
			FleetID: chain.FleetID, Hotkey: chain.Hotkey, ClientKey: chain.ClientKey, LocalClientKey: releaseHex32([32]byte{}),
			CommitmentHash: chain.CommitmentHash, Generation: chain.Generation, ValidFromEpoch: chain.ValidFromEpoch, ValidToEpoch: chain.ValidToEpoch,
			CleanedAtEpoch: chain.CleanedAtEpoch, RecordUID: chain.RecordUID, Cleaned: chain.Cleaned,
		}
		if chain.Active {
			row.LiveUID, row.LiveUIDFound = fixture.hotkeys[common.HexToHash(chain.Hotkey)]
			domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, row.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientId)
			if err != nil {
				t.Fatal(err)
			}
			path, err := releaseClientKeyCaptureV2Path(fixture.steerer.cfg.StateDir, domain, request)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := custody.read(t.Context(), path, releaseClientKeyHistoryTestResponseBytes, false)
			if err != nil {
				t.Fatal(err)
			}
			registration, err := verifyReleaseClientKeyCaptureV2(t.Context(), fixture.steerer.chain, encoded, releaseClientKeyHistoryTestResponseBytes, domain, request, true)
			if err != nil {
				t.Fatal(err)
			}
			if registration.Present {
				row.LocalClientKey = releaseHex32(registration.PublicKey)
			}
			row.ClientKeyObservationHash = ReleaseMeasurementContentHash(encoded)
		}
		rows = append(rows, row)
	}
	return rows
}

// Pure verifier options over the given rows. The fixture's own current-binding
// rule, not the code under test, derives each operator's replay bindings.
func releaseKeyMismatchTestOptions(t testing.TB, measurement *releaseMeasurementV2TestFixture, rows []ReleaseBindingMeasurement) ReleaseMeasurementV2Options {
	t.Helper()
	original := measurement.artifact.Bindings
	measurement.artifact.Bindings = rows
	defer func() { measurement.artifact.Bindings = original }()
	return measurement.options(t)
}

// The audited crash loop: a registered hotkey binds a measured provider's client
// id under its own client key. Steering must continue, score the provider exactly
// as unbound, keep the matched bindings, and record the foreign row so that the
// sealed artifact replays to the same decision from retained bytes alone.
func TestReleaseHeadV2ForeignKeyBindingScoresProviderAsUnbound(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 15)
	index, victimId := releaseKeyMismatchTestVictim(t, fixture.measurement.artifact, 9)
	oracle := releaseKeyMismatchTestUnboundOracle(t, fixture.measurement, index)
	foreign := releaseKeyMismatchTestForeign(fixture.measurement.artifact.Bindings[index])
	fixture.rpc.bindingKVs[victimId] = foreign
	fixture.hotkeys[releaseKeyMismatchTestHotkey] = releaseKeyMismatchTestUid
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatalf("a foreign binding on a measured provider failed the steering attempt: %v", err)
	}
	want := oracle.want
	if !reflect.DeepEqual(result.Weights, want.SelectedHead) || !reflect.DeepEqual(result.Bound, want.BoundProviders) || !reflect.DeepEqual(result.StaleBindings, want.StaleBindings) || !reflect.DeepEqual(result.HeadEMA, oracle.artifact.HeadEMA) {
		t.Fatal("the foreign-key provider was not scored exactly as an unbound provider")
	}
	if result.Bound[9][victimId] || len(result.Weights) == 0 || slices.Contains(result.EligibleUIDs, releaseKeyMismatchTestUid) {
		t.Fatal("the foreign binding acquired fleet membership or head weight")
	}
	matched := 0
	for _, binding := range result.Bindings {
		clientId, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		if binding.Active && clientId != victimId {
			if !result.Bound[binding.NoID][clientId] {
				t.Fatalf("matched binding %s lost its fleet membership", binding.ClientID)
			}
			matched++
		}
	}
	if matched == 0 {
		t.Fatal("no matched binding remained to show that legitimate bindings still count")
	}
	row := result.Bindings[index]
	if !row.Active || row.ClientKey != foreign.ClientKey || row.Hotkey != foreign.Hotkey || row.LocalClientKey != releaseHex32([32]byte{0x31}) || row.ClientKeyObservationHash == "" || !row.LiveUIDFound || row.LiveUID != releaseKeyMismatchTestUid {
		t.Fatalf("the exclusion is not recorded by its chain and retained local keys: %+v", row)
	}
	// The verifier sees only the chain answers and retained captures; removing
	// the credential proves it performs no new signed observation.
	for _, owner := range fixture.steerer.contexts {
		owner.ClientKeyHistory.byJwt = func() string { return "" }
	}
	rows := releaseKeyMismatchTestRetainedRows(t, fixture)
	if !reflect.DeepEqual(rows, result.Bindings) {
		t.Fatal("collector rows differ from the independently observed chain rows and retained captures")
	}
	artifact := cloneReleaseMeasurementArtifact(t, fixture.measurement.artifact)
	artifact.Inputs, artifact.Bindings, artifact.HeadEMA = result.Inputs, result.Bindings, result.HeadEMA
	encoded, hash, err := SealReleaseMeasurementArtifactV2(t.Context(), artifact, releaseKeyMismatchTestOptions(t, fixture.measurement, rows))
	if err != nil || hash != ReleaseMeasurementContentHash(encoded) {
		t.Fatalf("the collector's artifact did not seal: %v", err)
	}
	_, verified, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, releaseKeyMismatchTestOptions(t, fixture.measurement, rows))
	if err != nil || !reflect.DeepEqual(verified.Decision, want) {
		t.Fatalf("the verifier did not reproduce the collector's unbound decision: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

// A client that rotated its key after its fleet bound it is measured under the
// new key. The old-key binding is scored as unbound until the fleet binds the
// new key, and that new binding counts.
func TestReleaseHeadV2RotatedClientKeyExcludesOldKeyBindingUntilRebound(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	index, victimId := releaseKeyMismatchTestVictim(t, fixture.measurement.artifact, 10)
	rotated := [32]byte{0x35}
	measurement := fixture.steerer.contexts[10]
	measurement.ClientKeyHistory = newReleaseHeadV2ClientKeyHistoryReader(t, fixture.steerer.cfg, 10, func(clientId [16]byte) [][32]byte {
		if connect.Id(clientId) == victimId {
			return [][32]byte{{0x31}, rotated}
		}
		return [][32]byte{{0x31}}
	})
	lookup := measurement.ClientKey
	measurement.ClientKey = func(clientId connect.Id) ([32]byte, bool, error) {
		if clientId == victimId {
			return rotated, true, nil
		}
		return lookup(clientId)
	}
	oracle := releaseKeyMismatchTestUnboundOracle(t, fixture.measurement, index)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatalf("a rotated client key failed the steering attempt: %v", err)
	}
	row := result.Bindings[index]
	if result.Bound[10][victimId] || !row.Active || row.ClientKey != releaseHex32([32]byte{0x31}) || row.LocalClientKey != releaseHex32(rotated) || row.ClientKeyObservationHash == "" {
		t.Fatalf("the old-key binding still counted after rotation: %+v", row)
	}
	if !reflect.DeepEqual(result.Weights, oracle.want.SelectedHead) || !reflect.DeepEqual(result.Bound, oracle.want.BoundProviders) || !reflect.DeepEqual(result.HeadEMA, oracle.artifact.HeadEMA) {
		t.Fatal("the old-key binding was not scored exactly as an unbound provider")
	}
	// The fleet binds the rotated key in a new generation. It counts at once,
	// though signed work from the earlier generation is not inherited.
	rebound := fixture.rpc.bindingKVs[victimId]
	rebound.ClientKey, rebound.Generation = releaseHex32(rotated), rebound.Generation+1
	fixture.rpc.bindingKVs[victimId] = rebound
	again, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil || !again.Bound[10][victimId] || again.Bindings[index].ClientKey != again.Bindings[index].LocalClientKey {
		t.Fatalf("a binding signed for the rotated key did not count: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

// A client that cleared its key has a signed removal and no published key. Any
// binding on it, including its fleet's former one, is scored as unbound with a
// zero local key.
func TestReleaseHeadV2ClearedClientKeyExcludesBinding(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	index, victimId := releaseKeyMismatchTestVictim(t, fixture.measurement.artifact, 9)
	measurement := fixture.steerer.contexts[9]
	measurement.ClientKeyHistory = newReleaseHeadV2ClientKeyHistoryReader(t, fixture.steerer.cfg, 9, func(clientId [16]byte) [][32]byte {
		if connect.Id(clientId) == victimId {
			return [][32]byte{{0x31}, {}}
		}
		return [][32]byte{{0x31}}
	})
	lookup := measurement.ClientKey
	measurement.ClientKey = func(clientId connect.Id) ([32]byte, bool, error) {
		if clientId == victimId {
			return [32]byte{}, false, nil
		}
		return lookup(clientId)
	}
	oracle := releaseKeyMismatchTestUnboundOracle(t, fixture.measurement, index)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatalf("a cleared client key failed the steering attempt: %v", err)
	}
	row := result.Bindings[index]
	if result.Bound[9][victimId] || !row.Active || row.LocalClientKey != releaseHex32([32]byte{}) || row.ClientKeyObservationHash == "" {
		t.Fatalf("a binding on a cleared key still counted or lost its signed removal: %+v", row)
	}
	if !reflect.DeepEqual(result.Weights, oracle.want.SelectedHead) || !reflect.DeepEqual(result.Bound, oracle.want.BoundProviders) || !reflect.DeepEqual(result.HeadEMA, oracle.artifact.HeadEMA) {
		t.Fatal("the cleared-key binding was not scored exactly as an unbound provider")
	}
	fixture.assertNoEMACommit(t)
}

// The collector never records a local key its retained signed capture does not
// state. A live key the capture contradicts fails the attempt closed, so no row
// exists that historical replay would refuse.
func TestReleaseHeadV2RefusesLiveKeyItsSignedCaptureDoesNotState(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	_, victimId := releaseKeyMismatchTestVictim(t, fixture.measurement.artifact, 9)
	fixture.steerer.contexts[9].ClientKeyHistory = newReleaseHeadV2ClientKeyHistoryReader(t, fixture.steerer.cfg, 9, func(clientId [16]byte) [][32]byte {
		if connect.Id(clientId) == victimId {
			return [][32]byte{{0x31}, {0x35}}
		}
		return [][32]byte{{0x31}}
	})
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err == nil || !strings.Contains(err.Error(), "signed current key differs from its independently read local key") || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("the collector recorded a live key its signed capture contradicts: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

// The pure verifier scores a row whose chain key differs from its local key as
// unbound, standalone and in lineage after an artifact written before this rule,
// which itself keeps its decision. The immutable v1 wire retains no signed local
// key, so it still refuses such a row.
func TestReleaseMeasurementV2KeyMismatchedRowReplaysAsUnbound(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	previous, err := canonicalReleaseMeasurementBytes(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	_, old, err := DecodeReleaseMeasurementArtifactV2(t.Context(), previous, fixture.options(t))
	if err != nil || !reflect.DeepEqual(old.Decision, fixture.want) {
		t.Fatalf("an artifact with only matched keys changed its decision: %v", err)
	}
	index, _ := releaseKeyMismatchTestVictim(t, fixture.artifact, 10)
	oracle := releaseKeyMismatchTestUnboundOracle(t, fixture, index)
	current := copyReleaseMeasurementTestProvider(t, fixture)
	current.artifact.Bindings[index] = releaseKeyMismatchTestForeign(fixture.artifact.Bindings[index])
	current.artifact.HeadEMA = slices.Clone(oracle.artifact.HeadEMA)
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), current.artifact, current.options(t))
	if err != nil || !reflect.DeepEqual(result.Decision, oracle.want) {
		t.Fatalf("a key-mismatched row did not verify as an unbound provider: %v", err)
	}
	// A foreign binding whose own hotkey lost its UID is still only excluded;
	// it is never reported as a stale member of a fleet.
	stale := copyReleaseMeasurementTestProvider(t, current)
	stale.artifact.Bindings[index].LiveUIDFound, stale.artifact.Bindings[index].LiveUID = false, 0
	result, err = VerifyReleaseMeasurementArtifactV2(t.Context(), stale.artifact, stale.options(t))
	if err != nil || !reflect.DeepEqual(result.Decision, oracle.want) {
		t.Fatalf("a stale key-mismatched row did not verify as an unbound provider: %v", err)
	}
	// Borrowing a live fleet's id buys the foreign row no membership and costs
	// that fleet nothing: exclusion is per row, and a fleet key names its hotkey.
	borrowed := copyReleaseMeasurementTestProvider(t, current)
	lender := slices.IndexFunc(borrowed.artifact.Bindings, func(binding ReleaseBindingMeasurement) bool {
		return binding.NoID != borrowed.artifact.Bindings[index].NoID && binding.Active && binding.LiveUIDFound && binding.LiveUID == binding.RecordUID && binding.LocalClientKey == binding.ClientKey
	})
	if lender < 0 {
		t.Fatal("no live fleet of another operator remains to lend its id")
	}
	borrowed.artifact.Bindings[index].FleetID = borrowed.artifact.Bindings[lender].FleetID
	result, err = VerifyReleaseMeasurementArtifactV2(t.Context(), borrowed.artifact, borrowed.options(t))
	if err != nil || !reflect.DeepEqual(result.Decision, oracle.want) {
		t.Fatalf("a foreign row borrowing a live fleet's id changed the unbound decision: %v", err)
	}
	current.artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(previous)
	lineage, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, fixture.options(t), current.artifact, current.options(t))
	if err != nil || !reflect.DeepEqual(lineage.Decision, oracle.want) {
		t.Fatalf("lineage from an all-matched artifact refused the exclusion: %v", err)
	}
	legacy := cloneReleaseMeasurementArtifact(t, fixture.legacy)
	legacy.Bindings[index] = current.artifact.Bindings[index]
	if _, err := VerifyReleaseMeasurementArtifact(legacy); err == nil || !strings.Contains(err.Error(), legacy.Bindings[index].ClientID) {
		t.Fatalf("the v1 wire admitted an unauthenticated local-key exclusion: %v", err)
	}
}

// Extends the hash-pinned decision fixture with the views a retained client-key
// capture is checked against: the EVM chain identity, the native genesis and the
// immutable companion's deployment fields, all at the exact decision hash.
type releaseKeyMismatchDecisionRpc struct {
	*releaseDecisionV2TestFixture
	anchor    common.Address
	companion map[string][]byte
}

// The EVM chain identity of the fixture's deployment.
func (self *releaseKeyMismatchDecisionRpc) ChainId() hexutil.Uint64 {
	return hexutil.Uint64(self.query.domain.ChainID)
}

// Companion reads use the same exact canonical hash selector as coordinator views.
func (self *releaseKeyMismatchDecisionRpc) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if common.BytesToAddress(call["to"]) != self.anchor {
		return self.releaseDecisionV2TestFixture.Call(ctx, call, selector)
	}
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical || *selector.BlockHash != common.Hash(self.blocks[self.finalized]) {
		return nil, errors.New("companion read requires the exact canonical hash selector")
	}
	calldata := call["input"]
	if len(calldata) == 0 {
		calldata = call["data"]
	}
	value, found := self.companion[hex.EncodeToString(calldata)]
	if !found {
		return nil, errors.New("companion field is not defined")
	}
	return slices.Clone(value), ctx.Err()
}

// The native genesis served beside the EVM namespace, as subtensor serves it.
type releaseKeyMismatchNativeRpc struct {
	genesis common.Hash
}

func (self *releaseKeyMismatchNativeRpc) GetBlockHash(number uint64) (common.Hash, error) {
	if number != 0 {
		return common.Hash{}, errors.New("only the genesis hash is served")
	}
	return self.genesis, nil
}

// A real geth client over the decision fixture plus its client-key authority.
func newReleaseKeyMismatchDecisionChain(t *testing.T, decision *releaseDecisionV2TestFixture, cfg *ReleaseConfig) *ChainClient {
	t.Helper()
	anchor := common.Address{0x77}
	coordinator := decision.chain.coordinator
	decision.set(t, "settlementVault", coordinator.PackSettlementVault(), common.HexToAddress(cfg.SettlementVault))
	decision.set(t, "validatorEvidence", coordinator.PackValidatorEvidence(), anchor)
	companion := stabi.NewSTValidatorEvidence()
	words := map[string][]byte{}
	for _, field := range []struct {
		data  []byte
		value [32]byte
	}{
		{data: companion.PackCoordinator(), value: common.BytesToHash(common.HexToAddress(cfg.Coordinator).Bytes())},
		{data: companion.PackSettlementVault(), value: common.BytesToHash(common.HexToAddress(cfg.SettlementVault).Bytes())},
		{data: companion.PackChainId(), value: common.BigToHash(new(big.Int).SetUint64(cfg.ChainID))},
		{data: companion.PackNetuid(), value: common.BigToHash(new(big.Int).SetUint64(uint64(cfg.Netuid)))},
		{data: companion.PackGenesisHash(), value: common.HexToHash(cfg.GenesisHash)},
		{data: companion.PackDeploymentIdHash(), value: decision.query.domain.DeploymentIDHash},
	} {
		words[hex.EncodeToString(field.data)] = slices.Clone(field.value[:])
	}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", &releaseKeyMismatchDecisionRpc{releaseDecisionV2TestFixture: decision, anchor: anchor, companion: words}); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterName("chain", &releaseKeyMismatchNativeRpc{genesis: common.HexToHash(cfg.GenesisHash)}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	client, err := ethclient.Dial(httpServer.URL)
	if err != nil {
		httpServer.Close()
		server.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); httpServer.Close(); server.Stop() })
	return &ChainClient{client: client, coordinator: coordinator, contractAddr: decision.chain.contractAddr, chainId: new(big.Int).SetUint64(cfg.ChainID), release: true}
}

// A historical decision whose first operator has a foreign binding on one
// provider and a matched binding on another, both live at the pinned EVM hash.
// The collector's real batch capture retains each client's signed key, and
// the artifact rows take their local keys from those captures.
type releaseKeyMismatchHistoricalTestFixture struct {
	decision  *releaseDecisionV2TestFixture
	native    *releaseNativeValidatorTestFixture
	history   *releaseEvidenceV2StartupHistory
	intent    *SteeringIntent
	artifact  *ReleaseMeasurementArtifact
	chain     *ChainClient
	victimId  connect.Id
	matchedId connect.Id
	positions map[connect.Id]int
}

func newReleaseKeyMismatchHistoricalTestFixture(t *testing.T) *releaseKeyMismatchHistoricalTestFixture {
	t.Helper()
	eligibility := newReleaseDecisionV2EligibilityTestFixture(t, nil)
	decision, history, artifact := eligibility.decision, eligibility.history, eligibility.artifact
	history.cfg.StateDir = newReleaseHeadV2TestStateDir(t)
	coordinator := decision.chain.coordinator
	// The operator root that signs this fixture's client-key statements.
	version := decision.versions[0]
	version.RootSigner = crypto.PubkeyToAddress(releaseClientKeyTestSigner(t, "12").PublicKey)
	decision.set(t, "operatorAt", coordinator.PackOperatorAt(big.NewInt(1), big.NewInt(1)), version)
	victimId, matchedId := decision.query.operators[0].providerIDs[0], decision.query.operators[0].providerIDs[1]
	decision.set(t, "bindingAt", coordinator.PackBindingAt([16]byte(victimId), big.NewInt(1)), true, stabi.STCoordinatorBindingRecord{
		FleetId: releaseKeyMismatchTestFleetId, Hotkey: chainBatchHotkey(1), ClientKey: releaseKeyMismatchTestClientKey,
		CommitmentHash: releaseKeyMismatchTestCommitment, Generation: 5, ValidFromEpoch: 1, ValidToEpoch: 2, Uid: 1,
	})
	decision.set(t, "bindingAt", coordinator.PackBindingAt([16]byte(matchedId), big.NewInt(1)), true, stabi.STCoordinatorBindingRecord{
		FleetId: [32]byte{0xa1}, Hotkey: chainBatchHotkey(2), ClientKey: [32]byte{0x31},
		CommitmentHash: [32]byte{0xa3}, Generation: 4, ValidFromEpoch: 0, ValidToEpoch: 2, Uid: 2,
	})
	chain := newReleaseKeyMismatchDecisionChain(t, decision, &history.cfg)
	observed, err := chain.readReleaseDecisionChainV2Context(t.Context(), decision.query)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Bindings = slices.Clone(observed.bindings)
	positions := map[connect.Id]int{}
	for index, binding := range artifact.Bindings {
		clientId, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		if binding.Active {
			positions[clientId] = index
		}
	}
	if len(positions) != 2 || !artifact.Bindings[positions[victimId]].LiveUIDFound || !artifact.Bindings[positions[matchedId]].LiveUIDFound {
		t.Fatal("the decision fixture lost its live foreign or matched binding")
	}
	hotkey := history.initial[history.participants[0].NoID].InitialCut.Activation.Hotkey
	var requests []protocol.ClientKeyObservationRequest
	var domain protocol.ClientKeyHistoryDomain
	for _, clientId := range []connect.Id{victimId, matchedId} {
		var request protocol.ClientKeyObservationRequest
		domain, request, err = releaseClientKeyDecisionV2(&history.cfg, 1, hotkey, artifact, clientId)
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	reader := newReleaseHeadV2ClientKeyHistoryReader(t, &history.cfg, 1, func([16]byte) [][32]byte { return [][32]byte{{0x31}} })
	budget := releaseHeadV2Budget{limit: history.cfg.EvidenceV2.Bounds.MaxControlBytes}
	keyCtx, keyReads, err := newReleaseClientKeyAuthorityV2Reads(t.Context(), chain, &history.cfg, artifact, hotkey, &budget)
	if err != nil {
		t.Fatal(err)
	}
	captures, err := captureReleaseClientKeysV2(keyCtx, chain, reader, history.cfg.StateDir, domain, requests, protocol.MaxClientKeyHistoryResponseBytes, history.cfg.EvidenceV2.Bounds.MaxHistoryBytes)
	if err := keyReads.finish(err); err != nil || len(captures) != 2 {
		t.Fatalf("real client-key capture prerequisite failed: %v", err)
	}
	for index, clientId := range []connect.Id{victimId, matchedId} {
		row := &artifact.Bindings[positions[clientId]]
		row.LocalClientKey, row.ClientKeyObservationHash = releaseHex32(captures[index].registration.PublicKey), captures[index].contentHash
	}
	if artifact.Bindings[positions[victimId]].LocalClientKey == artifact.Bindings[positions[victimId]].ClientKey {
		t.Fatal("the foreign binding unexpectedly carries the measured client's key")
	}
	return &releaseKeyMismatchHistoricalTestFixture{decision: decision, native: eligibility.native, history: history, intent: eligibility.intent, artifact: artifact, chain: chain, victimId: victimId, matchedId: matchedId, positions: positions}
}

// One complete historical replay at the pinned hash, through the real verifier.
func (self *releaseKeyMismatchHistoricalTestFixture) read(t *testing.T, artifact *ReleaseMeasurementArtifact) (releaseIntentDecisionSourcesV2, error) {
	t.Helper()
	custody := &releaseEvidenceV2StartupReferences{remaining: self.history.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	sources, err := self.history.readIntentDecisionSourcesV2(t.Context(), self.chain, self.native.chain, self.native.expected, self.intent, artifact, custody)
	return sources, errors.Join(err, custody.close())
}

// Startup and archive replay re-read a foreign binding at its pinned EVM hash
// and the retained operator-signed capture of the measured client. The capture,
// not the coordinator row, is the local key; the shared projection then excludes
// the foreign binding and keeps the matched one, as the collector did.
func TestReleaseEvidenceV2DecisionHistoricalForeignBindingUsesRetainedLocalKey(t *testing.T) {
	t.Parallel()
	fixture := newReleaseKeyMismatchHistoricalTestFixture(t)
	sources, err := fixture.read(t, fixture.artifact)
	if err != nil {
		t.Fatalf("historical replay refused a foreign binding beside its retained capture: %v", err)
	}
	if !slices.Equal(sources.bindings, fixture.artifact.Bindings) {
		t.Fatal("historical binding sources differ from the collector's rows")
	}
	providerKVs := map[uint64]map[connect.Id]bool{}
	for _, operator := range fixture.decision.query.operators {
		providerKVs[operator.noID] = map[connect.Id]bool{}
		for _, clientId := range operator.providerIDs {
			providerKVs[operator.noID][clientId] = true
		}
	}
	projection := &ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, SettlementEpoch: fixture.artifact.SettlementEpoch, Policy: fixture.history.cfg.Policy, Bindings: sources.bindings}
	_, bound, membersByUid, stale, current, err := releaseMeasurementBindingObservations(projection, providerKVs)
	if err != nil || bound[1][fixture.victimId] || !bound[1][fixture.matchedId] || len(stale) != 0 || len(current) != 1 || len(membersByUid) != 1 || len(membersByUid[2]) != 1 {
		t.Fatalf("historical sources did not reproduce the collector's exclusion: %v", err)
	}
}

// The retained capture, not the artifact, decides which side of its binding a
// measured client is on. Claiming that a matched member holds another key would
// silently drop a legitimate fleet member, and claiming the binder's key for a
// foreign row would admit the binder; historical replay refuses both.
func TestReleaseEvidenceV2DecisionHistoricalRefusesLocalKeyItsCaptureDoesNotState(t *testing.T) {
	t.Parallel()
	fixture := newReleaseKeyMismatchHistoricalTestFixture(t)
	for _, claim := range []struct {
		clientId connect.Id
		key      [32]byte
	}{
		{clientId: fixture.matchedId, key: [32]byte{0x35}},
		{clientId: fixture.victimId, key: releaseKeyMismatchTestClientKey},
	} {
		artifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
		artifact.Bindings[fixture.positions[claim.clientId]].LocalClientKey = releaseHex32(claim.key)
		if _, err := fixture.read(t, artifact); err == nil || !strings.Contains(err.Error(), "historical local client key differs from its retained operator-signed capture") {
			t.Fatalf("historical replay admitted a local key for %s that its retained capture does not state: %v", claim.clientId, err)
		}
	}
}
