//go:build linux || darwin

// Genuine signed censuses, private locators and both public Http replicas
// exercise interruption and replay through the actual phase reader.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Test mutation and transport reads share one lock. An exact object barrier
// waits for request cancellation, never a scheduler delay or a verdict hook.
type evidenceRelayColdCensusTestFixture struct {
	runtime       *evidenceRelayRuntime
	native        *runtimeEvidenceActivationRpcV2TestFixture
	closed        []validatorcomponent.ValidatorEvidencePublicationV2Manifest
	audit         validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest
	stateLock     sync.Mutex
	objectKVs     [2]map[string][]byte
	readKVs       [2]map[string]uint64
	viewKVs       map[string]string
	blockHash     string
	blockStarted  chan struct{}
	blockRelease  chan struct{}
	blockOnce     sync.Once
	originStatus  int
	finalized     uint64
	finalizedHash common.Hash
	epochReads    uint64
}

// The native authority and plan ancestry are the existing real provisional
// fixture. The public payloads exercise transport and consent, not trail truth.
func newEvidenceRelayColdCensusTestFixture(t *testing.T) *evidenceRelayColdCensusTestFixture {
	t.Helper()
	native, runtime := provisionalRelayContinuationRuntimeTest(t)
	self := &evidenceRelayColdCensusTestFixture{runtime: runtime, native: native, finalized: 850, finalizedHash: common.Hash{0x33},
		objectKVs: [2]map[string][]byte{{}, {}}, readKVs: [2]map[string]uint64{{}, {}}, viewKVs: map[string]string{}}
	for index := range self.objectKVs {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || request.URL.Query().Get("kind") != "metadata" {
				http.Error(writer, "unexpected test artifact request", http.StatusBadRequest)
				return
			}
			hash := request.URL.Query().Get("hash")
			raw, block, release, status := func() ([]byte, bool, <-chan struct{}, int) {
				self.stateLock.Lock()
				defer self.stateLock.Unlock()
				self.readKVs[index][hash]++
				block := index == 1 && self.blockHash == hash
				if block {
					self.blockOnce.Do(func() { close(self.blockStarted) })
				}
				status := 0
				if index == 1 {
					status = self.originStatus
				}
				return bytes.Clone(self.objectKVs[index][hash]), block, self.blockRelease, status
			}()
			if block {
				select {
				case <-request.Context().Done():
					return
				case <-release:
				}
			}
			if status != 0 {
				http.Error(writer, "test origin request failed", status)
				return
			}
			if raw == nil {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(raw)
		}))
		t.Cleanup(server.Close)
		runtime.origins[index] = server.URL
	}
	chainServer := httptest.NewServer(http.HandlerFunc(self.serveChain))
	t.Cleanup(chainServer.Close)
	var err error
	runtime.chain, err = validatorcomponent.DialReleaseChainContext(t.Context(), []string{chainServer.URL}, runtime.executor.plan.ValidatorEvidence.Coordinator)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.chain.Close)
	source := &runtime.sources[0]
	epoch := source.activations[0].Domain.Epoch
	for offset := uint64(0); offset < 2; offset++ {
		self.addEpoch(t, epoch+offset, 210+300*offset, 510+300*offset)
		self.closed = append(self.closed, self.publication(t, epoch+offset, 509+300*offset, protocol.ValidatorEvidenceSubject{}).(validatorcomponent.ValidatorEvidencePublicationV2Manifest))
	}
	self.audit = self.publication(t, epoch, 830, protocol.ValidatorEvidenceSubject{ObservationEpoch: epoch + 2, NativeEpoch: 78}).(validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest)
	return self
}

// Encode immutable coordinator boundaries through the actual contract abi.
func (self *evidenceRelayColdCensusTestFixture) addEpoch(t *testing.T, epoch, start, end uint64) {
	t.Helper()
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	for _, view := range []struct {
		method string
		input  []byte
		value  uint64
	}{
		{method: "epochStartBlock", input: coordinator.PackEpochStartBlock(new(big.Int).SetUint64(epoch)), value: start},
		{method: "epochEndBlock", input: coordinator.PackEpochEndBlock(new(big.Int).SetUint64(epoch)), value: end},
	} {
		raw, err := parsed.Methods[view.method].Outputs.Pack(new(big.Int).SetUint64(view.value))
		if err != nil {
			t.Fatal(err)
		}
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.viewKVs[hexutil.Encode(view.input)] = hexutil.Encode(raw)
		}()
	}
}

// Every exact-hash epoch query and fresh finalized head crosses real Http.
func (self *evidenceRelayColdCensusTestFixture) serveChain(writer http.ResponseWriter, request *http.Request) {
	var call evidenceRelayRpcRequest
	if json.NewDecoder(io.LimitReader(request.Body, 32*1024)).Decode(&call) != nil {
		http.Error(writer, "invalid test chain request", http.StatusBadRequest)
		return
	}
	result, err := func() (any, error) {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		switch call.Method {
		case "eth_chainId":
			return hexutil.EncodeUint64(testnetChainID), nil
		case "eth_getBlockByHash":
			var hash common.Hash
			if len(call.Params) == 2 && json.Unmarshal(call.Params[0], &hash) == nil && hash == self.finalizedHash {
				return map[string]any{"number": hexutil.EncodeUint64(self.finalized), "hash": hash}, nil
			}
		case "eth_getBlockByNumber":
			if len(call.Params) != 2 {
				break
			}
			var selector string
			if json.Unmarshal(call.Params[0], &selector) != nil {
				break
			}
			if selector == "finalized" || selector == hexutil.EncodeUint64(self.finalized) {
				return map[string]any{"number": hexutil.EncodeUint64(self.finalized), "hash": self.finalizedHash}, nil
			}
			anchor := self.runtime.sources[0].activations[0]
			if selector == hexutil.EncodeUint64(anchor.EVMBlock) {
				return map[string]any{"number": selector, "hash": common.Hash(anchor.EVMHash)}, nil
			}
		case "eth_call":
			if len(call.Params) != 2 {
				break
			}
			var selector struct {
				BlockHash        common.Hash `json:"blockHash"`
				RequireCanonical bool        `json:"requireCanonical"`
			}
			var input struct {
				To    common.Address `json:"to"`
				Input hexutil.Bytes  `json:"input"`
			}
			if json.Unmarshal(call.Params[0], &input) != nil || json.Unmarshal(call.Params[1], &selector) != nil || input.To != self.runtime.executor.plan.ValidatorEvidence.Coordinator || selector.BlockHash != self.finalizedHash || !selector.RequireCanonical {
				break
			}
			self.epochReads++
			if raw, found := self.viewKVs[hexutil.Encode(input.Input)]; found {
				return raw, nil
			}
		}
		return nil, errors.New("test chain request lost exact coordinates")
	}()
	response := map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}
	if err != nil {
		delete(response, "result")
		response["error"] = map[string]any{"code": -32000, "message": err.Error()}
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response)
}

// Build canonical synthetic public objects with the original configured keys.
// This does not manufacture a retained receipt or a startup cache verdict.
func (self *evidenceRelayColdCensusTestFixture) publication(t *testing.T, epoch, boundary uint64, subject protocol.ValidatorEvidenceSubject, generationKeys ...map[uint64]ed25519.PrivateKey) any {
	t.Helper()
	source := self.runtime.sources[0].forEpoch(epoch)
	store := func(value any) ([]byte, [32]byte) {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, '\n')
		hash := sha256.Sum256(raw)
		for index := range self.objectKVs {
			self.objectKVs[index][fmt.Sprintf("0x%x", hash)] = bytes.Clone(raw)
		}
		return raw, hash
	}
	decision := validatorcomponent.ReleaseMeasurementV2Decision{DeploymentID: self.runtime.executor.plan.DeploymentID, ChainID: self.runtime.executor.plan.ChainID,
		GenesisHash: self.runtime.executor.plan.GenesisHash, Coordinator: common.Address(source.activations[0].Domain.Coordinator).Hex(),
		SettlementVault: common.Address(source.activations[0].Domain.SettlementVault).Hex(), ValidatorID: source.validatorId, Netuid: self.runtime.executor.plan.Netuid,
		SubnetEpoch: subject.NativeEpoch, NativeSnapshotBlock: 101, NativeSnapshotHash: common.Hash{0x71}.Hex(), EVMSnapshotBlock: boundary,
		EVMSnapshotHash: common.Hash{0x72}.Hex(), SettlementEpoch: subject.ObservationEpoch, PolicyHash: common.Hash(source.activations[0].Domain.PolicyHash).Hex()}
	closedCensus := validatorcomponent.ValidatorEvidenceCensusV2{Schema: validatorcomponent.ValidatorEvidenceCensusV2Schema, Hotkey: source.activations[0].Hotkey,
		Boundary: validatorcomponent.AttemptBoundary{SettlementEpoch: epoch, EVMBlock: boundary, EVMBlockHash: decision.EVMSnapshotHash}}
	auditCensus := validatorcomponent.ValidatorEvidenceDepositAuditV2Census{Schema: validatorcomponent.ValidatorEvidenceDepositAuditV2CensusSchema, Epoch: epoch,
		Subject: subject, Decision: decision, Hotkey: source.activations[0].Hotkey}
	for _, activation := range source.activations {
		domain, err := activation.EvidenceDomain()
		if err != nil {
			t.Fatal(err)
		}
		var payload any = struct {
			Epoch uint64
			NoId  uint64
		}{Epoch: epoch, NoId: activation.NoID}
		if subject != (protocol.ValidatorEvidenceSubject{}) {
			payload = validatorcomponent.ValidatorEvidenceDepositAuditV2Payload{Schema: validatorcomponent.ValidatorEvidenceDepositAuditV2Schema, Decision: decision,
				Audit: validatorcomponent.DepositAudit{NoID: activation.NoID, Epoch: subject.ObservationEpoch, SourceEpoch: epoch, ObservedAtBlock: boundary}}
		}
		raw, hash := store(payload)
		closedCensus.Members = append(closedCensus.Members, validatorcomponent.ValidatorEvidenceCensusV2Member{Domain: domain, NoID: activation.NoID, VPK: activation.VPK, PayloadHash: hash, PayloadBytes: uint64(len(raw))})
		auditCensus.Members = append(auditCensus.Members, validatorcomponent.ValidatorEvidenceDepositAuditV2Member{Domain: domain, NoId: activation.NoID, Vpk: activation.VPK, PayloadHash: hash, PayloadBytes: uint64(len(raw))})
	}
	var census any = closedCensus
	kind := protocol.ValidatorEvidenceClosedCensus
	if subject != (protocol.ValidatorEvidenceSubject{}) {
		census, kind = auditCensus, protocol.ValidatorEvidenceDepositAudit
	}
	censusRaw, censusHash := store(census)
	var references []validatorcomponent.ValidatorEvidencePublicationV2MemberReference
	for index, activation := range source.activations {
		member := closedCensus.Members[index]
		header := protocol.ValidatorEvidenceHeader{Domain: member.Domain, Hotkey: activation.Hotkey, NoID: activation.NoID, Epoch: epoch, Kind: kind, Subject: subject,
			VPK: activation.VPK, BoundaryBlock: boundary, BoundaryHash: [32]byte{0x72}, CensusHash: censusHash, PayloadHash: member.PayloadHash, PayloadBytes: member.PayloadBytes}
		hotkey, key, err := runtimeEvidenceActivationKeysV2(self.runtime.executor.roles, source.validatorId, activation.NoID)
		if err != nil {
			t.Fatal(err)
		}
		if len(generationKeys) == 1 {
			key = generationKeys[0][activation.NoID]
		}
		vpkSignature, err := header.SignVPK(key)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := header.Digest()
		if err != nil {
			t.Fatal(err)
		}
		hotkeySignature, err := hotkey.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		raw, hash := store(validatorcomponent.ValidatorEvidenceSignedV2{Schema: validatorcomponent.ValidatorEvidenceSignedV2Schema, Header: header, VPKSignature: vpkSignature, HotkeySignature: hotkeySignature})
		references = append(references, validatorcomponent.ValidatorEvidencePublicationV2MemberReference{NoId: activation.NoID, SignedArtifactHash: hash, SignedArtifactBytes: uint64(len(raw))})
	}
	var manifest any = validatorcomponent.ValidatorEvidencePublicationV2Manifest{Schema: validatorcomponent.ValidatorEvidencePublicationV2Schema, Kind: kind,
		Epoch: epoch, Origins: self.runtime.origins, CensusHash: censusHash, CensusBytes: uint64(len(censusRaw)), Members: references}
	path, err := validatorcomponent.ValidatorEvidencePublicationV2ManifestPath(source.stateDir, epoch)
	if kind == protocol.ValidatorEvidenceDepositAudit {
		manifest = validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest{Schema: validatorcomponent.ValidatorEvidenceDepositAuditV2ManifestSchema, Kind: kind,
			Epoch: epoch, Subject: subject, Decision: decision, Origins: self.runtime.origins, CensusHash: censusHash, CensusBytes: uint64(len(censusRaw)), Members: references}
		path, err = validatorcomponent.ValidatorEvidenceDepositAuditV2ManifestPath(source.stateDir, epoch, subject)
	}
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// New sessions use the exact production gate and only replace log transport.
func (self *evidenceRelayColdCensusTestFixture) census(t *testing.T, total uint64) *evidenceRelayColdCensusSource {
	t.Helper()
	session, err := self.runtime.newEvidenceRelayColdCensusSession(t.Context())
	if err != nil || session == nil {
		t.Fatal("cold census session is absent", err)
	}
	session.output = new(bytes.Buffer)
	return session.beginSource(&self.runtime.sources[0], total)
}

// Cumulative real public requests remain stable after the caller joins reads.
func (self *evidenceRelayColdCensusTestFixture) counts() [2]uint64 {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var result [2]uint64
	for index, reads := range self.readKVs {
		for _, count := range reads {
			result[index] += count
		}
	}
	return result
}

// Complete public verification follows provisional admission in a separate
// owner. Tests join that actual verifier before inspecting its retained work.
func (self *evidenceRelayColdCensusTestFixture) prepareAndAudit() error {
	if err := self.runtime.prepareHorizon(); err != nil {
		return err
	}
	return self.runtime.pendingPublicCensus.verify(self.runtime.ctx)
}

// A cancellation after the first durable object and during the next actual
// public request preserves completed work without granting audit success or sends.
func TestEvidenceRelayColdCensusCancellationResumesExactlyCompletedPublications(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	fixture.runtime.ctx = ctx
	fixture.blockHash = fmt.Sprintf("0x%x", fixture.closed[1].CensusHash)
	fixture.blockStarted = make(chan struct{})
	result := make(chan error, 1)
	go func() { result <- fixture.prepareAndAudit() }()
	select {
	case <-fixture.blockStarted:
	case err := <-result:
		t.Fatal("preparation failed before the second real public read", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	paths, err := filepath.Glob(filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("the complete first publication was not durable before its successor", len(paths), err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) || fixture.runtime.horizon == nil || len(fixture.runtime.horizon.headerKVs) != 0 {
		t.Fatal("canceled audit lost provisional readiness or granted live slot admission", err)
	}
	before := fixture.counts()
	fixture.stateLock.Lock()
	fixture.blockHash = ""
	fixture.stateLock.Unlock()
	fixture.runtime.ctx = t.Context()
	if err := fixture.prepareAndAudit(); err != nil {
		t.Fatal("restart could not resume authenticated progress", err)
	}
	after := fixture.counts()
	for index := range before {
		if after[index]-before[index] != 10 {
			t.Fatalf("origin %d replayed completed data or skipped its incomplete suffix: before=%d after=%d", index, before[index], after[index])
		}
	}
	paths, err = filepath.Glob(filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, "*.json"))
	if err != nil || len(paths) != 3 || fixture.runtime.horizon == nil || len(fixture.runtime.horizon.headerKVs) != 0 || len(fixture.runtime.pendingPublicCensus.horizon.headerKVs) != 6 || len(fixture.runtime.executor.journal.Entries()) != 1 {
		t.Fatal("resumed census lost exact slots or created transaction completion", err)
	}
	for _, source := range fixture.runtime.sources {
		if source.nextEpoch != 0 || fixture.runtime.completed[source.validatorId] {
			t.Fatal("public authentication became relay completion")
		}
	}
}

// Warm preflight must still read every epoch window, original journal debit
// and native permit. Actual relay input readers always revisit both origins.
func TestEvidenceRelayColdCensusWarmEntryKeepsFreshAdmissionAndActualPublicationReads(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	if err := fixture.prepareAndAudit(); err != nil {
		t.Fatal(err)
	}
	before := fixture.counts()
	beforeNative := fixture.native.callCount("state_call")
	fixture.stateLock.Lock()
	beforeEpoch := fixture.epochReads
	fixture.finalized++
	fixture.finalizedHash = common.Hash{0x34}
	fixture.stateLock.Unlock()
	fixture.runtime.horizon = nil
	if err := fixture.prepareAndAudit(); err != nil || fixture.runtime.horizon == nil {
		t.Fatal("later finalized head discarded immutable progress", err)
	}
	fixture.stateLock.Lock()
	epochReads := fixture.epochReads - beforeEpoch
	fixture.stateLock.Unlock()
	if fixture.counts() != before || fixture.native.callCount("state_call") != beforeNative+4 || epochReads != 6 {
		t.Fatal("warm preflight skipped live clocks/geometry or repeated public payload reads")
	}
	if _, err := fixture.runtime.readClosedPublication(t.Context(), &fixture.runtime.sources[0], &fixture.closed[0], fixture.finalized, fixture.finalizedHash); err != nil {
		t.Fatal(err)
	}
	if after := fixture.counts(); after[0]-before[0] != 5 || after[1]-before[1] != 5 {
		t.Fatal("actual relay reader inherited a provisional authentication checkpoint", after, before)
	}
	fixture.native.stateLock.Lock()
	fixture.native.permits[1] = false
	fixture.native.stateLock.Unlock()
	fixture.runtime.horizon = nil
	if err := fixture.runtime.prepareHorizon(); err == nil || !strings.Contains(err.Error(), "independent finalized eligibility/schedule") || fixture.runtime.horizon != nil {
		t.Fatal("warm checkpoint granted native eligibility", err)
	}
	fixture.native.stateLock.Lock()
	fixture.native.permits[1] = true
	fixture.native.stateLock.Unlock()
	entry := JournalEntry{DeploymentID: fixture.runtime.executor.plan.DeploymentID, PlanHash: fixture.runtime.executor.plan.PlanHash,
		ActionID: evidenceRelayActionPrefix + strings.Repeat("ab", 32), IntentHash: common.Hash{0x55}.Hex(), Stage: StageIntent}
	if err := fixture.runtime.executor.journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.prepareHorizon(); err == nil || fixture.runtime.horizon != nil {
		t.Fatal("warm checkpoint waived a fresh unmatched journal debit", err)
	}
}

// Cold progress counts only the fully read two-origin objects. Missing final
// consents, payload corruption and cancellation remain incomplete on disk.
func TestEvidenceRelayColdCensusRejectsIncompleteReplicaWithoutCheckpoint(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	manifest := &fixture.closed[0]
	for _, fault := range []string{"missing consent", "corrupt payload", "second origin timeout"} {
		census := fixture.census(t, 1)
		key := fmt.Sprintf("0x%x", manifest.Members[len(manifest.Members)-1].SignedArtifactHash)
		fixture.stateLock.Lock()
		if fault == "corrupt payload" {
			var signed validatorcomponent.ValidatorEvidenceSignedV2
			if err := json.Unmarshal(fixture.objectKVs[1][key], &signed); err != nil {
				fixture.stateLock.Unlock()
				t.Fatal(err)
			}
			key = fmt.Sprintf("0x%x", signed.Header.PayloadHash)
		}
		original := fixture.objectKVs[1][key]
		if fault == "second origin timeout" {
			fixture.originStatus = http.StatusGatewayTimeout
		} else if fault == "missing consent" {
			delete(fixture.objectKVs[1], key)
		} else {
			fixture.objectKVs[1][key] = bytes.Repeat([]byte{'x'}, len(original))
		}
		fixture.stateLock.Unlock()
		requests, err := fixture.runtime.readClosedPublication(t.Context(), &fixture.runtime.sources[0], manifest, fixture.finalized, fixture.finalizedHash, census)
		if err == nil || requests != nil || census.completed != 0 {
			t.Fatalf("%s completed a partial census: %v", fault, err)
		}
		fixture.stateLock.Lock()
		fixture.objectKVs[1][key] = original
		fixture.originStatus = 0
		fixture.stateLock.Unlock()
		paths, err := filepath.Glob(filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, "*.json"))
		if err != nil || len(paths) != 0 {
			t.Fatal("failed replica authentication left a checkpoint", err)
		}
	}
}

// Both closed and audit counters survive source-by-source continuation without
// borrowing a result prefix. The log distinguishes resumed work from persistence.
func TestEvidenceRelayColdCensusProgressReportsPerSourceCompletedAndTotal(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	for pass := uint64(0); pass < 2; pass++ {
		census := fixture.census(t, 2)
		if _, err := fixture.runtime.readClosedPublication(t.Context(), &fixture.runtime.sources[0], &fixture.closed[0], fixture.finalized, fixture.finalizedHash, census); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.runtime.readAuditPublication(t.Context(), &fixture.runtime.sources[0], &fixture.audit, fixture.finalized, fixture.finalizedHash, census); err != nil {
			t.Fatal(err)
		}
		if census.total != 2 || census.completed != 2 || census.resumed != 2*pass || census.checkpointed != 2 || census.checkpointFailures != 0 {
			t.Fatal("progress misrepresented complete two-origin authentication", census)
		}
		output := census.session.output.(*bytes.Buffer).String()
		want := fmt.Sprintf("validator_id=1 total=2 completed=2 resumed=%d checkpointed=2 checkpoint_failures=0 final_acceptance=false", 2*pass)
		if !strings.Contains(output, "total=2 completed=0") || !strings.Contains(output, want) {
			t.Fatal("public progress telemetry omitted exact counters", output)
		}
	}
}
