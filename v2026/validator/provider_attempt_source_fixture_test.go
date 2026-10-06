//go:build linux

// The complete fixture executes actual M8 request custody, failed/idle cuts,
// dual public replay and canonical Server originals under synthetic keys only.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Real public handlers return originals only; no returned verified flags exist.
type providerAttemptSourceTestFixture struct {
	base         *providerAttemptWindowTestFixture
	source       *ProviderAttemptAuthoritySource
	response     ProviderAttemptWindowResponse
	artifact     *payoutartifact.Artifact
	windowKey    ed25519.PrivateKey
	authorityRaw []byte
	stateLock    sync.Mutex
	closed       bool
	reads        int
}

// The transport returns a raw ASSIGN/FINAL, while the actual Server retains
// its typed cached-response envelope inside the signed original receipt.
func providerAttemptSourceTestReceipt(t *testing.T, seal *attemptCutV2SealTestFixture, scope protocol.ProviderAttemptReceiptScope, request ProviderAttemptRequestRecord, response []byte) protocol.ProviderAttemptReceipt {
	t.Helper()
	var assign connect.VerifyAssignResult
	var final connect.VerifyFinalResult
	if err := json.Unmarshal(response, &assign); err != nil {
		t.Fatal(err)
	}
	var trailId connect.Id
	var cached any
	if assign.TrailId != (connect.Id{}) {
		trailId = assign.TrailId
		cached = struct {
			Assign *connect.VerifyAssignResult `json:"assign,omitempty"`
		}{Assign: &assign}
	} else {
		if err := json.Unmarshal(response, &final); err != nil || final.Proof == nil {
			t.Fatal("actual final response", err)
		}
		trailId = final.Proof.Header.TrailId
		cached = struct {
			Final *connect.VerifyFinalResult `json:"final,omitempty"`
		}{Final: &final}
	}
	cachedRaw, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	seal.server.mu.Lock()
	state := seal.server.trails[trailId]
	network := connect.Id{71}
	trail := &protocol.ProviderAttemptReceiptTrail{TrailId: trailId, ClientId: seal.engine.clientId, Vpk: bytes.Clone(state.vpk), ServerNonce: bytes.Clone(state.serverNonce), M: state.m, ServerKeyId: seal.server.serverKeyId, Status: "active", CreateMs: state.confirmedAt[0], ActivityMs: state.confirmedAt[len(state.confirmedAt)-1]}
	for index, id := range state.confirmed {
		hop := &protocol.ProviderAttemptReceiptHop{ClientId: id, NetworkId: &network, ConfirmedMs: state.confirmedAt[index], Seed: index == 0}
		copy(hop.EgressIpHash[:], id[:])
		if index > 0 {
			hop.AssignedMs = state.confirmedAt[index-1]
			hop.AssignN = len(seal.server.providers) - index
		}
		trail.Hops = append(trail.Hops, hop)
	}
	if state.complete {
		trail.Status = "complete"
	} else {
		trail.Pending = &protocol.ProviderAttemptReceiptHop{ClientId: state.pending, NetworkId: &network, AssignedMs: trail.ActivityMs, AssignN: len(seal.server.providers) - len(state.confirmed)}
	}
	seal.server.mu.Unlock()
	body := protocol.ProviderAttemptReceiptBody{Schema: protocol.ProviderAttemptReceiptDomain, Scope: &scope, PreviousDepth: len(trail.Hops) - 1, RecoveryMs: trail.ActivityMs, RequestMessage: request.Message, RequestSignature: request.RequestSignature, ResponseJson: string(cachedRaw), Trail: trail}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	receipt := protocol.ProviderAttemptReceipt{Body: raw, Signature: ed25519.Sign(seal.server.serverKey, append([]byte(protocol.ProviderAttemptReceiptDomain), raw...))}
	if _, err := protocol.ValidateProviderAttemptReceipt(&receipt, seal.server.serverKey.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal("actual original cached response", err)
	}
	return receipt
}

// Select all authorities before producing any cuts or serving witness bytes.
func newProviderAttemptSourceTestFixture(t *testing.T, failed bool) *providerAttemptSourceTestFixture {
	t.Helper()
	return newProviderAttemptSourceTestFixtureWithCompleted(t, failed, false)
}

// Complete M8 originals and a failed lane share the same independently pinned
// window. Existing empty/failed callers retain their original scenario.
func newProviderAttemptSourceTestFixtureWithCompleted(t *testing.T, failed, completed bool) *providerAttemptSourceTestFixture {
	t.Helper()
	self := &providerAttemptSourceTestFixture{windowKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{83}, 32))}
	firstBlock := uint64(1)
	if completed {
		// Actual wallet consent needs a positive effective block before earning.
		// Select this clock before any request, terminal or publication exists.
		firstBlock = 2
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: 42, StartBlock: firstBlock, EndBlock: firstBlock + 100, FinalizedBlock: firstBlock + 100}
	start := &types.Header{Number: new(big.Int).SetUint64(window.StartBlock), Difficulty: new(big.Int), Time: 1700000000, GasLimit: 30000000, Extra: []byte("synthetic-provider-start")}
	end := &types.Header{Number: new(big.Int).SetUint64(window.EndBlock), Difficulty: new(big.Int), Time: 1700001000, GasLimit: 30000000, Extra: []byte("synthetic-provider-end")}
	startRaw, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	endRaw, err := json.Marshal(end)
	if err != nil {
		t.Fatal(err)
	}
	type requestFixture struct {
		preparation ProviderAttemptRequestPreparation
		cut         ProviderAttemptRequestWindow
		scope       protocol.ProviderAttemptReceiptScope
		endpoint    string
	}
	requests := map[*attemptCutV2SealTestFixture]requestFixture{}
	activeUsed := false
	completeUsed := false
	before := func(seal *attemptCutV2SealTestFixture) {
		if completed {
			head, err := seal.ledger.Head()
			if err != nil || head.LastSequence != 0 {
				t.Fatal("prospective fixture cannot relabel existing original records", err)
			}
			terminal := &types.Header{Number: new(big.Int).SetUint64(window.EndBlock - 1), Difficulty: new(big.Int), Time: 1700000999, GasLimit: 30000000, Extra: []byte("synthetic-provider-terminal")}
			boundary := AttemptBoundary{SettlementEpoch: window.Epoch, EVMBlock: window.EndBlock - 1, EVMBlockHash: terminal.Hash().Hex()}
			seal.expected.Boundary = boundary
			seal.engine.cfg.AttemptBoundaryResolver = func(ctx context.Context, pinned *AttemptBoundary, clients []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
				if err := ctx.Err(); err != nil {
					return AttemptBoundary{}, nil, err
				}
				if pinned != nil && *pinned != boundary {
					return AttemptBoundary{}, nil, errors.New("prospective fixture original boundary changed")
				}
				bindings := make([]AttemptBinding, len(clients))
				for index, client := range clients {
					bindings[index] = attemptLedgerTestBinding(client, 1)
				}
				return boundary, bindings, nil
			}
			// Construction snapshots the resolver separately from config. Select
			// the live resolver while the ledger is still empty, before any send.
			seal.engine.resolve = seal.engine.cfg.AttemptBoundaryResolver
		}
		identity := ProviderAttemptRequestIdentity{Ledger: seal.ledger.identity, Coordinator: fmt.Sprintf("0x%x", seal.expected.Activation.Domain.Coordinator), ClientId: seal.engine.clientId, PolicyHash: seal.expected.Activation.Domain.PolicyHash}
		preparation := ProviderAttemptRequestPreparation{Identity: identity, Limits: ProviderAttemptRequestLimits{MaxRecords: 1000, MaxRecordBytes: 8192, MaxJournalBytes: 8 * 1024 * 1024}, Birth: AttemptBoundary{SettlementEpoch: window.Epoch, EVMBlock: window.StartBlock, EVMBlockHash: start.Hash().Hex()}}
		directory := t.TempDir()
		if err := os.Chmod(directory, 0700); err != nil {
			t.Fatal(err)
		}
		prepareProviderRequestTestOwner(t, directory, preparation)
		journal, err := OpenProviderAttemptRequestJournal(t.Context(), directory, preparation, seal.key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := journal.Close(); err != nil {
				t.Error(err)
			}
		})
		seal.engine.cfg.RequestJournal = journal
		seal.engine.cfg.StepTimeout = time.Minute
		scope := protocol.ProviderAttemptReceiptScope{Profile: "synthetic", GenesisHash: seal.expected.Activation.Domain.GenesisHash, DeploymentId: seal.ledger.identity.DeploymentID, DeploymentKey: fmt.Sprintf("%d:%s", seal.ledger.identity.ChainID, identity.Coordinator), PolicyHash: identity.PolicyHash, Netuid: uint64(seal.ledger.identity.Netuid), NoId: seal.ledger.identity.NoID}
		receipts := map[[32]byte]protocol.ProviderAttemptReceipt{}
		runComplete := completed && !completeUsed && seal.ledger.identity.NoID == 9
		runFailed := failed && !activeUsed && !runComplete && (!completed || seal.ledger.identity.NoID == 9)
		if runComplete || runFailed {
			if runComplete {
				completeUsed = true
			} else {
				activeUsed = true
			}
			if runComplete {
				seal.server.providers = seal.server.providers[:8]
			}
			seal.engine.transport = attemptCutV2SealTestTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
				var args struct {
					TrailId *connect.Id `json:"trail_id"`
				}
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, err
				}
				if args.TrailId != nil && runFailed {
					return nil, errors.New("synthetic original extend never delivered")
				}
				response, err := seal.server.PostVerify(ctx, hop, raw)
				if err != nil {
					return nil, err
				}
				var request ProviderAttemptRequestRecord
				if err := journal.Walk(ctx, func(value ProviderAttemptRequestRecord) error { request = value; return nil }); err != nil {
					return nil, err
				}
				receipt := providerAttemptSourceTestReceipt(t, seal, scope, request, response)
				wire := sha256.Sum256(append(bytes.Clone(request.Message), request.RequestSignature...))
				receipts[wire] = receipt
				return response, nil
			})
			if runComplete {
				for index := uint64(0); index < seal.policy.Verify.ReliabilityAMin; index++ {
					if proof, err := seal.engine.RunTrail(t.Context()); err != nil || proof == nil || len(proof.Hops) != 8 {
						t.Fatal("actual completed M8 original", err)
					}
				}
			} else if proof, err := seal.engine.RunTrail(t.Context()); err == nil || proof != nil {
				t.Fatal("actual failed request fixture unexpectedly completed", err)
			}
			seal.engine.transport = seal.server
		}
		cut, err := journal.SealWindow(t.Context(), window, 16*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.CloseRequests(t.Context(), cut, scope); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			self.stateLock.Lock()
			self.reads++
			closed := self.closed
			self.stateLock.Unlock()
			if closed {
				http.Error(writer, "synthetic unavailable", http.StatusNotFound)
				return
			}
			if request.Method != http.MethodPost || request.URL.Path != "/verify/original/close" {
				http.Error(writer, "unexpected request", 400)
				return
			}
			var closure protocol.ProviderAttemptRequestClosure
			if err := json.NewDecoder(request.Body).Decode(&closure); err != nil {
				http.Error(writer, err.Error(), 400)
				return
			}
			if err := protocol.VerifyProviderAttemptRequestClosure(request.Context(), closure, scope); err != nil {
				http.Error(writer, err.Error(), 400)
				return
			}
			wire := sha256.Sum256(append(bytes.Clone(closure.Message), closure.RequestSignature...))
			result := providerAttemptLookupResult{}
			if receipt, exists := receipts[wire]; exists {
				value := receipt
				result.Original = &value
			} else {
				value, err := protocol.SealProviderAttemptClosedUnreceived(request.Context(), closure, seal.server.serverKeyId, seal.server.serverKey)
				if err != nil {
					http.Error(writer, err.Error(), 400)
					return
				}
				result.ClosedUnreceived = value
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(result)
		}))
		t.Cleanup(server.Close)
		requests[seal] = requestFixture{preparation: preparation, cut: *cut, scope: scope, endpoint: server.URL + "/verify/original"}
	}
	self.base = &providerAttemptWindowTestFixture{owners: []*providerAttemptWindowTestOwner{newProviderAttemptWindowTestOwnerForWindow(t, 51, 0, 0, before, &window), newProviderAttemptWindowTestOwnerForWindow(t, 52, 0, 0, before, &window)}}
	sort.Slice(self.base.owners, func(i, j int) bool {
		a, b := self.base.owners[i].fixture.hotkey.PublicKey(), self.base.owners[j].fixture.hotkey.PublicKey()
		return bytes.Compare(a[:], b[:]) < 0
	})
	domain := self.base.owners[0].read.Activations[0].Domain
	registryKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{82}, 32))
	expectation := protocol.ProviderAttemptRegistryExpectation{Domain: protocol.ProviderAttemptDomain{ChainId: domain.ChainID, GenesisHash: domain.GenesisHash, Netuid: domain.Netuid, Coordinator: domain.Coordinator, SettlementVault: domain.SettlementVault, DeploymentIdHash: domain.DeploymentIDHash, PolicyHash: domain.PolicyHash}, Signer: [32]byte(registryKey.Public().(ed25519.PublicKey)), MaxRevisions: 4, MaxOwners: 8, MaxOperatorLanes: 16, MaxBytes: 64 * 1024}
	registry := protocol.ProviderAttemptRegistry{Schema: protocol.ProviderAttemptRegistrySchema, Domain: expectation.Domain, EffectiveEpoch: 42}
	self.response.Window.Schema = ProviderAttemptWindowSchema
	for _, owner := range self.base.owners {
		hotkey := owner.fixture.hotkey.PublicKey()
		registry.Owners = append(registry.Owners, protocol.ProviderAttemptOwner{Hotkey: hotkey, NoIds: []uint64{9, 11}})
		self.response.Window.Members = append(self.response.Window.Members, ProviderAttemptWindowMember{Hotkey: hotkey, Manifest: *owner.manifest})
	}
	signed, err := protocol.SealProviderAttemptRegistry(t.Context(), registry, expectation, registryKey)
	if err != nil {
		t.Fatal(err)
	}
	expectation.RootHash, err = signed.Hash()
	if err != nil {
		t.Fatal(err)
	}
	expectation.RequiredHeadHash = expectation.RootHash
	self.base.expectation = expectation
	self.response.Window.RegistryHistory = []protocol.ProviderAttemptRegistry{*signed}
	authority := ProviderAttemptWindowAuthority{Schema: ProviderAttemptWindowAuthoritySchema, Domain: expectation.Domain, RegistryHead: expectation.RootHash, Window: self.base.owners[0].read.Window, StartHeader: startRaw, EndHeader: endRaw}
	for _, owner := range self.base.owners {
		options := owner.fixture.options(t).Settlement
		value := ProviderAttemptValidatorAuthority{Hotkey: owner.fixture.hotkey.PublicKey(), Publication: owner.read, MaxParticipants: options.MaxParticipants, MaxTransitionBytes: options.MaxTransitionBytes, MaxClosureBytes: options.MaxClosureBytes}
		for _, fixture := range owner.fixture.operators {
			seal := fixture.seal
			option := options.Operators[seal.ledger.identity.NoID]
			request := requests[seal]
			operator := ProviderAttemptOperatorAuthority{Expected: option.Expected, Policy: option.Policy, Bounds: option.Bounds, Stats: option.Measurement.ExpectedConfig, MaxProviders: option.Measurement.MaxProviders, MaxEgressHashes: option.Measurement.MaxEgressHashes, MaxFleetPrefixes: option.Measurement.MaxFleetPrefixes, ReplayBounds: option.Measurement.Replay.Bounds, Requests: request.preparation, ReceiptScope: request.scope, ReceiptEndpoint: request.endpoint}
			for id, binding := range option.Measurement.CurrentBindingKVs {
				operator.Bindings = append(operator.Bindings, ProviderAttemptBindingAuthority{ClientId: id, Binding: binding})
			}
			for id, key := range option.Measurement.Replay.ServerKeys {
				operator.ServerKeys = append(operator.ServerKeys, ProviderAttemptServerKey{Id: id, Key: [32]byte(key)})
			}
			value.Operators = append(value.Operators, operator)
			self.response.Requests = append(self.response.Requests, ProviderAttemptRequestLaneOriginal{Hotkey: value.Hotkey, NoId: seal.ledger.identity.NoID, Windows: []ProviderAttemptRequestWindow{request.cut}})
		}
		authority.Validators = append(authority.Validators, value)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.reads++
		if self.closed {
			http.Error(writer, "synthetic unavailable", 404)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(self.response)
	}))
	t.Cleanup(endpoint.Close)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	scratch := root
	if output := os.Getenv("URNETWORK_PROVIDER_ATTEMPT_FIXTURE_DIR"); output != "" {
		if !filepath.IsAbs(output) || filepath.Clean(output) != output {
			t.Fatal("export root is not absolute")
		}
		if err := os.Mkdir(output, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		info, err := os.Lstat(output)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatal("export root is not protected", err)
		}
		scratch = filepath.Join(output, "scratch")
		if err := os.Mkdir(scratch, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
	}
	config := ProviderAttemptAuthority{Schema: ProviderAttemptAuthoritySchema, Registry: expectation, WindowSigner: [32]byte(self.windowKey.Public().(ed25519.PublicKey)), WindowEndpoint: endpoint.URL + "/windows", ScratchRoot: scratch, MaxWindowBytes: 16 * 1024 * 1024, MaxOriginalBytes: 64 * 1024 * 1024, MaxObjects: 4096, MaxProviders: 128, MaxMetadataBytes: 16 * 1024 * 1024, MaxRecordBytes: 64 * 1024 * 1024, MaxRecords: 100000}
	self.authorityRaw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(self.authorityRaw)
	authority.AuthorityHash = hash
	self.response.Authority = authority
	self.resign(t)
	path := filepath.Join(root, "authority.json")
	if err := os.WriteFile(path, self.authorityRaw, 0600); err != nil {
		t.Fatal(err)
	}
	self.source, err = NewProviderAttemptAuthoritySource(t.Context(), ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(self.authorityRaw)), SHA256: attemptHex32(hash)})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(stringsRepeatSyntheticScalar())
	if err != nil {
		t.Fatal(err)
	}
	providers := []payoutartifact.ProviderInput{}
	if completed {
		rows := map[connect.Id]*payoutartifact.ProviderInput{}
		for _, owner := range self.base.owners {
			for _, fixture := range owner.fixture.operators {
				if fixture.seal.ledger.identity.NoID != 9 {
					continue
				}
				for _, id := range fixture.seal.server.providers {
					if rows[id] == nil {
						rows[id] = &payoutartifact.ProviderInput{ClientID: [16]byte(id), NetworkID: [16]byte{71}, Coldkey: [32]byte{73}, BindingGeneration: 1}
					}
				}
				for _, state := range fixture.seal.server.trails {
					for _, id := range state.confirmed[1:] {
						rows[id].Assignments++
						rows[id].Confirmations++
					}
					if !state.complete {
						rows[state.pending].Assignments++
					}
				}
			}
		}
		aMin := authority.Validators[0].Operators[0].Policy.Verify.ReliabilityAMin
		for _, row := range rows {
			row.Eligible = row.Assignments >= aMin
			if row.Eligible {
				row.UsageBytes = 100
			}
			providers = append(providers, *row)
		}
	} else {
		for _, owner := range self.base.owners {
			for _, fixture := range owner.fixture.operators {
				for _, record := range requests[fixture.seal].cut.Records {
					if record.Message[len(connect.VerifyCtx)] == connect.VerifyMsgTypeSeed {
						for _, state := range fixture.seal.server.trails {
							providers = append(providers, payoutartifact.ProviderInput{ClientID: [16]byte(state.pending), NetworkID: [16]byte{71}, Coldkey: [32]byte{73}, UsageBytes: 100, Assignments: 1, Eligible: true})
						}
					}
				}
			}
		}
	}
	self.artifact, err = payoutartifact.Build(payoutartifact.BuildInput{DeploymentID: self.base.owners[0].fixture.operators[0].seal.ledger.identity.DeploymentID, GenesisHash: attemptHex32(domain.GenesisHash), PolicyHash: attemptHex32(domain.PolicyHash), ChainID: domain.ChainID, Netuid: domain.Netuid, Coordinator: common.Address(domain.Coordinator), SettlementVault: common.Address(domain.SettlementVault), Epoch: window.Epoch, NoID: 9, Start: payoutartifact.Boundary{Number: window.StartBlock, Hash: start.Hash().Hex()}, End: payoutartifact.Boundary{Number: window.EndBlock, Hash: end.Hash().Hex()}, OperatorSnapshotHash: "sha256:" + hex.EncodeToString(bytes.Repeat([]byte{1}, 32)), FleetSnapshotHash: "sha256:" + hex.EncodeToString(bytes.Repeat([]byte{2}, 32)), ReliabilityAMin: authority.Validators[0].Operators[0].Policy.Verify.ReliabilityAMin, Providers: providers, CreatedAt: time.Unix(1700001000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(self.artifact, key); err != nil {
		t.Fatal(err)
	}
	return self
}

// A visibly synthetic scalar makes an exported artifact reproducible without
// copying any real account, endpoint or private key into source fixtures.
func stringsRepeatSyntheticScalar() string {
	return "0000000000000000000000000000000000000000000000000000000000000055"
}

// A mutation control deliberately re-signs only its independent input change;
// it cannot rewrite original wire or request-window signatures.
func (self *providerAttemptSourceTestFixture) resign(t *testing.T) {
	t.Helper()
	signed, err := SealProviderAttemptWindowAuthority(t.Context(), self.response.Authority, self.windowKey)
	if err != nil {
		t.Fatal(err)
	}
	self.response.Authority = *signed
}
