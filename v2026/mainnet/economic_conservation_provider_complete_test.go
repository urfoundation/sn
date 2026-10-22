// Complete provider measurements replay actual exported M8 trials and complete
// original work. Wallet consents and hash-pinned binding replies are produced
// here through their actual signature, ABI and HTTP paths, never verified flags.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urfoundation/sn/v2026/validator"
)

// Independent qualification exports are mandatory for these selected roots.
// An absent producer result is a setup refusal, never a skipped passing body.
func economicProviderCompleteOriginal(t *testing.T, variable, name string) []byte {
	t.Helper()
	directory := os.Getenv(variable)
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		t.Fatal("complete provider consumer requires actual original export", variable)
	}
	file, err := os.Open(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 32*1024*1024+1))
	if err := errors.Join(readErr, file.Close()); err != nil || len(raw) > 32*1024*1024 {
		t.Fatal("bounded complete provider original", name, err)
	}
	return raw
}

// The exported trial artifact supplies every provider identity. Only its
// synthetic wallet/binding placeholders are replaced by real original proofs.
func economicProviderCompleteFixture(t *testing.T) (economicConservationPolicy, economicConservationEntitlement) {
	t.Helper()
	artifactRaw := economicProviderCompleteOriginal(t, "URNETWORK_PROVIDER_WORK_FIXTURE_DIR", "artifact.json")
	artifact, err := payoutartifact.DecodeWithContext(t.Context(), artifactRaw)
	if err != nil || artifact.Epoch != 42 || artifact.Start.Number <= 1 || artifact.End.Number <= artifact.Start.Number {
		t.Fatal("original completed trial window cannot precede a prospective wallet", err)
	}
	var work payoutartifact.WholeWorkInventory
	workRaw := economicProviderCompleteOriginal(t, "URNETWORK_PROVIDER_WORK_FIXTURE_DIR", "inventory.json")
	if err := json.Unmarshal(workRaw, &work); err != nil {
		t.Fatal(err)
	}
	if work.Clock == nil || work.Clock.Start != artifact.Start || work.Clock.End != artifact.End {
		t.Fatal("work original clock differs from actual trials")
	}
	rootKey, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	authorityKey, err := crypto.HexToECDSA(strings.Repeat("39", 32))
	if err != nil {
		t.Fatal(err)
	}
	authoritySigner := crypto.PubkeyToAddress(authorityKey.PublicKey)
	authority, err := payoutartifact.DecodeWholeWorkAuthority(t.Context(), work.Authority, authoritySigner)
	if err != nil || len(authority.ExpectedProviders) != len(artifact.Providers) {
		t.Fatal("independent full provider roster", err)
	}
	providers := append([]payoutartifact.ProviderInput(nil), artifact.Providers...)
	originals := &economicProviderOriginals{Work: &work, Wallets: make([]economicProviderWalletOriginal, len(providers)), Attempts: economicProviderCompleteOriginal(t, "URNETWORK_PROVIDER_ATTEMPT_COMPLETED_FIXTURE_DIR", "original.json")}
	for index := range providers {
		provider := &providers[index]
		if provider.ClientID != authority.ExpectedProviders[index].ClientId || provider.NetworkID != authority.ExpectedProviders[index].NetworkId {
			t.Fatal("original complete provider ordering differs")
		}
		seed := sha256.Sum256(append([]byte("synthetic-economic-original-coldkey:"), provider.ClientID[:]...))
		key, err := schnorrkel.NewMiniSecretKeyFromRaw(seed)
		if err != nil {
			t.Fatal(err)
		}
		provider.Coldkey, provider.BindingGeneration = key.Public().Encode(), 0
		statement := protocol.WalletMappingStatement{Domain: authority.Domain, UserId: [16]byte{0x71, byte(index + 1)}, ClientId: provider.ClientID, NetworkId: provider.NetworkID, Coldkey: provider.Coldkey, Generation: 1, Nonce: seed, IssuedAt: work.Clock.StartTime.Unix() - 600, ExpiresAt: work.Clock.StartTime.Unix() - 300, FromEpoch: artifact.Epoch, ThroughEpoch: artifact.Epoch + 1}
		boundary := protocol.ClientKeyEffectiveBoundary{Epoch: artifact.Epoch - 1, Block: artifact.Start.Number - 1, Hash: sha256.Sum256([]byte("synthetic-prior-finalized-wallet-boundary"))}
		if err := protocol.SignProspectiveWalletMapping(&statement, boundary, rootKey); err != nil {
			t.Fatal(err)
		}
		message, err := statement.Message()
		if err != nil {
			t.Fatal(err)
		}
		signature, err := key.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
		if err != nil {
			t.Fatal(err)
		}
		consent := protocol.WalletMappingConsent{Message: message, Signature: signature.Encode()}
		_, hash, err := protocol.VerifyWalletMappingConsent(t.Context(), consent)
		if err != nil {
			t.Fatal(err)
		}
		originals.Wallets[index] = economicProviderWalletOriginal{ClientId: provider.ClientID, Originals: []protocol.WalletMappingConsent{consent}}
		authority.ExpectedProviders[index].WalletHeadHash, authority.ExpectedProviders[index].WalletGeneration = hex.EncodeToString(hash[:]), 1
	}
	authority, err = payoutartifact.SignWholeWorkAuthority(t.Context(), authority, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	work.Authority, err = authority.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	created, err := time.Parse(time.RFC3339Nano, artifact.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err = payoutartifact.BuildWithContext(t.Context(), payoutartifact.BuildInput{ClosedWork: artifact.ClosedWork, DeploymentID: artifact.DeploymentID, GenesisHash: artifact.GenesisHash, PolicyHash: artifact.PolicyHash, ChainID: artifact.ChainID, Netuid: artifact.Netuid, Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, Epoch: artifact.Epoch, NoID: artifact.NoID, Start: artifact.Start, End: artifact.End, OperatorSnapshotHash: artifact.OperatorSnapshotHash, FleetSnapshotHash: artifact.FleetSnapshotHash, Providers: providers, TotalUsers: artifact.TotalUsers, ReliabilityAMin: artifact.ReliabilityAMin, CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := crypto.HexToECDSA(strings.Repeat("0", 62) + "55")
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, publisher); err != nil {
		t.Fatal(err)
	}
	attemptRaw := economicProviderCompleteOriginal(t, "URNETWORK_PROVIDER_ATTEMPT_COMPLETED_FIXTURE_DIR", "authority.json")
	selected := &economicProviderMeasurementPolicy{Schema: economicProviderMeasurementPolicySchema, AttributionSigner: authoritySigner.Hex(), WholeWorkAuthoritySigner: authoritySigner.Hex(), WalletEndpoint: "https://synthetic-wallet.example", AttemptAuthority: validator.ReleaseEvidenceV2File{Path: filepath.Join(os.Getenv("URNETWORK_PROVIDER_ATTEMPT_COMPLETED_FIXTURE_DIR"), "authority.json"), Bytes: uint64(len(attemptRaw)), SHA256: monitorReadDigest(attemptRaw)}, MaximumOriginalBytes: 32 * 1024 * 1024}
	pool := strconv.FormatUint(artifact.NoID, 10)
	policy := economicConservationPolicy{EntitlementSources: &economicConservationEntitlementPolicy{Sources: []economicConservationEntitlementSource{{PoolId: pool, DeploymentId: artifact.DeploymentID, ProviderMeasurements: selected}}}}
	census := &economicConservationEntitlementCensus{Schema: economicEntitlementCensusSchema, Start: artifact.Start, End: artifact.End, RootSigner: crypto.PubkeyToAddress(rootKey.PublicKey).Hex(), Artifact: *artifact, ProviderOriginals: originals}
	originals.Bindings = economicProviderCompleteBindingRead(t, census, &authority)
	census.ContentHash = census.hash()
	return policy, economicConservationEntitlement{Id: economicConservationEntitlementId("42", pool), Epoch: "42", PoolId: pool, Census: census}
}

// Actual hash-pinned HTTP calls capture both canonical boundary headers, code
// and every generated ABI tuple. The fixture supplies no verified binding rows.
func economicProviderCompleteBindingRead(t *testing.T, census *economicConservationEntitlementCensus, authority *payoutartifact.WholeWorkAuthority) *validator.ProviderAttemptBindingOriginal {
	t.Helper()
	contract, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	code := []byte{0x60, 0x00, 0x50, 0x00}
	census.CoordinatorCodeHash = crypto.Keccak256Hash(code).Hex()
	inactive, err := contract.Methods["bindingAt"].Outputs.Pack(false, stabi.STCoordinatorBindingRecord{})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			JsonRpc string            `json:"jsonrpc"`
			Id      int               `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(io.LimitReader(request.Body, 8192)).Decode(&input) != nil {
			http.Error(writer, "synthetic bounded request", http.StatusBadRequest)
			return
		}
		var result any
		clock := census.ProviderOriginals.Work.Clock
		switch input.Method {
		case "eth_getBlockByNumber":
			var number string
			if len(input.Params) == 2 && json.Unmarshal(input.Params[0], &number) == nil {
				for _, boundary := range []payoutartifact.Boundary{clock.Start, clock.End} {
					if number == hexutil.EncodeUint64(boundary.Number) {
						result = map[string]string{"number": number, "hash": boundary.Hash}
					}
				}
			}
		case "debug_getRawHeader", "eth_getCode", "eth_call":
			position := 0
			if input.Method != "debug_getRawHeader" {
				position = 1
			}
			var selector struct {
				BlockHash        string `json:"blockHash"`
				RequireCanonical bool   `json:"requireCanonical"`
			}
			if len(input.Params) > position && json.Unmarshal(input.Params[position], &selector) == nil && selector.RequireCanonical && (selector.BlockHash == clock.Start.Hash || selector.BlockHash == clock.End.Hash) {
				switch input.Method {
				case "debug_getRawHeader":
					raw := clock.StartHeader
					if selector.BlockHash == clock.End.Hash {
						raw = clock.EndHeader
					}
					result = hexutil.Encode(raw)
				case "eth_getCode":
					var address string
					if json.Unmarshal(input.Params[0], &address) == nil && address == census.Artifact.Coordinator.Hex() {
						result = hexutil.Encode(code)
					}
				case "eth_call":
					var call struct {
						To   string `json:"to"`
						Data string `json:"data"`
					}
					if json.Unmarshal(input.Params[0], &call) == nil && call.To == census.Artifact.Coordinator.Hex() {
						for _, provider := range authority.ExpectedProviders {
							data, err := contract.Pack("bindingAt", provider.ClientId, new(big.Int).SetUint64(census.Artifact.Epoch))
							if err == nil && call.Data == hexutil.Encode(data) {
								result = hexutil.Encode(inactive)
								calls.Add(1)
								break
							}
						}
					}
				}
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		if result == nil {
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": input.Id, "error": map[string]any{"code": -32602, "message": "synthetic original binding request differs"}})
		} else {
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": input.Id, "result": result})
		}
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.httpClient.CloseIdleConnections()
	reader := &monitorEvmReader{client: client, contract: contract, policy: monitorEconomicEvmPolicy{Address: census.Artifact.Coordinator.Hex()}}
	result, err := readEconomicProviderBindings(t.Context(), reader, census, authority)
	if err != nil || result == nil || calls.Load() != uint64(2*len(authority.ExpectedProviders)) {
		t.Fatal("actual complete binding HTTP census", calls.Load(), err)
	}
	return result
}

// A fresh private admission starts without trusted counters or reused results.
func economicProviderCompleteView(ctx context.Context) *economicConservationArchiveView {
	view := newEconomicConservationArchiveView(economicConservationResources{IndexEntries: 8192, IndexBytes: 64 * 1024 * 1024})
	view.admission = ctx
	return view
}

// Actual complete work, failed/idle trials, consent and original binding tuples
// pass both first admission and a cold JSON replay with no detached result cache.
func TestEconomicProviderCompleteOriginalsSurviveColdReplay(t *testing.T) {
	policy, record := economicProviderCompleteFixture(t)
	view := economicProviderCompleteView(t.Context())
	if err := view.sealProviderOriginals(t.Context(), policy, &economicConservationState{}, &record); err != nil {
		t.Fatal("complete original producer census could not join", err)
	}
	measurement := record.Census.ProviderMeasurements
	if measurement == nil || measurement.CompletedBytes != 700 || measurement.Assignments != 57 || measurement.Confirmations != 56 || measurement.Providers < 9 || len(view.providerContracts) != 7 || record.Census.providerAttempts != nil {
		t.Fatal("complete original producer measurements differ", measurement)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var reopened economicConservationEntitlement
	if err := json.Unmarshal(raw, &reopened); err != nil {
		t.Fatal(err)
	}
	cold := economicProviderCompleteView(t.Context())
	value, err := cold.verifyProviderOriginals(t.Context(), policy, &economicConservationState{}, reopened)
	if err != nil || value == nil || value.Measurement != *measurement || value.ClosedWork != *record.Census.ClosedWork || reopened.Census.providerAttempts != nil {
		t.Fatal("cold original replay borrowed counters or changed identity", value, err)
	}
	before, _ := json.Marshal(reopened)
	reopened.Census.ProviderMeasurements.Confirmations++
	if err := cold.sealProviderOriginals(t.Context(), policy, &economicConservationState{}, &reopened); !errors.Is(err, errRpcIntegrity) || len(cold.providerCensuses) != 0 {
		t.Fatal("forged retained projection acquired original authority", err)
	}
	if bytes.Equal(before, raw) == false {
		t.Fatal("original cold serialization changed")
	}
}

// One unavailable idle mapping prevents a partial positive, while a supplied
// invalid original stays a hard contradiction; neither writes the private index.
func TestEconomicProviderCompleteOriginalsRefuseMissingIdleAndForgedConsent(t *testing.T) {
	policy, record := economicProviderCompleteFixture(t)
	idle := -1
	for index, provider := range record.Census.Artifact.Providers {
		if provider.Assignments == 0 {
			idle = index
			break
		}
	}
	if idle < 0 {
		t.Fatal("actual completed fixture omitted idle provider")
	}
	original := record.Census.ProviderOriginals.Wallets[idle].Originals
	record.Census.ProviderOriginals.Wallets[idle].Originals = nil
	view := economicProviderCompleteView(t.Context())
	if err := view.sealProviderOriginals(t.Context(), policy, &economicConservationState{}, &record); !errors.Is(err, protocol.ErrWalletMappingUnavailable) || record.Census.ProviderMeasurements != nil || len(view.providerCensuses) != 0 {
		t.Fatal("missing idle original became a complete subset", err)
	}
	record.Census.ProviderOriginals.Wallets[idle].Originals = append([]protocol.WalletMappingConsent(nil), original...)
	record.Census.ProviderOriginals.Wallets[idle].Originals[0].Signature[0] ^= 1
	if err := view.sealProviderOriginals(t.Context(), policy, &economicConservationState{}, &record); !errors.Is(err, errRpcIntegrity) || record.Census.ProviderMeasurements != nil || len(view.providerCensuses) != 0 {
		t.Fatal("forged original consent became a retryable absence or complete subset", err)
	}
}

// Canceling after originals exist still prevents their admission; the identical
// complete original may then be retried under a new owner without new evidence.
func TestEconomicProviderCompleteOriginalsJoinCancellationBeforePublication(t *testing.T) {
	policy, record := economicProviderCompleteFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	view := economicProviderCompleteView(ctx)
	if err := view.sealProviderOriginals(ctx, policy, &economicConservationState{}, &record); !errors.Is(err, context.Canceled) || record.Census.ProviderMeasurements != nil || len(view.providerCensuses) != 0 {
		t.Fatal("canceled complete original acquired publication", err)
	}
	view = economicProviderCompleteView(t.Context())
	if err := view.sealProviderOriginals(t.Context(), policy, &economicConservationState{}, &record); err != nil || record.Census.ProviderMeasurements == nil {
		t.Fatal("identical complete original could not resume", err)
	}
}
