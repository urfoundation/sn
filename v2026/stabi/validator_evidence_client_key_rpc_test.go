// Real Http/ethclient batches exercise exact canonical selectors, actual ABI
// decoding, complete failure discard and separate Http/logical work counters.
package stabi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Faults are changed between joined operations or under the same state lock.
type validatorEvidenceClientKeyRpcFixture struct {
	stateLock      sync.Mutex
	domain         protocol.ClientKeyHistoryDomain
	httpRequests   int
	logicalMethods int
	largestBatch   int
	fault          string
	failAtHttp     int
	entered        chan struct{}
	left           chan struct{}
}

// Snapshot counters use the same lock as every live transport increment.
func (self *validatorEvidenceClientKeyRpcFixture) counts() (int, int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.httpRequests, self.logicalMethods, self.largestBatch
}

// Heights are genuine independent transport values, not reconstructed headers.
func validatorEvidenceClientKeyRpcBoundary(block uint64) protocol.ClientKeyEffectiveBoundary {
	hash := [32]byte{0x71}
	binary.BigEndian.PutUint64(hash[24:], block)
	return protocol.ClientKeyEffectiveBoundary{Epoch: 9, Block: block, Hash: hash}
}

// Each method has a separate work count even when sharing one Http admission.
func (self *validatorEvidenceClientKeyRpcFixture) method(ctx context.Context) (string, error) {
	self.stateLock.Lock()
	self.logicalMethods++
	fault, entered, left := self.fault, self.entered, self.left
	self.entered = nil
	self.stateLock.Unlock()
	if entered != nil {
		close(entered)
		<-ctx.Done()
		close(left)
		return fault, ctx.Err()
	}
	return fault, nil
}

// Native genesis has no Evm block-zero fallback.
func (self *validatorEvidenceClientKeyRpcFixture) GetBlockHash(ctx context.Context, block uint64) (*common.Hash, error) {
	fault, err := self.method(ctx)
	if err != nil || block != 0 || fault == "native-unavailable" {
		return nil, errors.Join(errors.New("native method unavailable"), err)
	}
	if fault == "native-null" {
		return nil, nil
	}
	hash := common.Hash(self.domain.GenesisHash)
	if fault == "native-wrong" {
		hash[0]++
	}
	return &hash, nil
}

// The test endpoint independently supplies the configured Evm chain identity.
func (self *validatorEvidenceClientKeyRpcFixture) ChainId(ctx context.Context) (hexutil.Uint64, error) {
	fault, err := self.method(ctx)
	value := self.domain.ChainID
	if fault == "chain" {
		value++
	}
	return hexutil.Uint64(value), err
}

// Evm genesis deliberately returns null; canonical positive heights are exact.
func (self *validatorEvidenceClientKeyRpcFixture) GetBlockByNumber(ctx context.Context, tag rpc.BlockNumber, full bool) (map[string]any, error) {
	fault, err := self.method(ctx)
	if err != nil || full {
		return nil, errors.Join(errors.New("invalid block request"), err)
	}
	if tag == 0 {
		return nil, nil
	}
	block := uint64(tag)
	if tag == rpc.FinalizedBlockNumber {
		block = 1000
	}
	if block > 1000 {
		return nil, errors.New("unknown canonical block")
	}
	boundary := validatorEvidenceClientKeyRpcBoundary(block)
	if fault == "canonical" && tag != rpc.FinalizedBlockNumber {
		boundary.Hash[0]++
	}
	return map[string]any{"number": hexutil.EncodeUint64(block), "hash": common.Hash(boundary.Hash)}, nil
}

// Real ABI output values and immutable words are returned only at the exact
// requested canonical hash. Operators one and two have distinct root signers.
func (self *validatorEvidenceClientKeyRpcFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	fault, err := self.method(ctx)
	if err != nil {
		return nil, err
	}
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical {
		return nil, errors.New("authority selector is not an exact canonical hash")
	}
	block := binary.BigEndian.Uint64(selector.BlockHash[24:])
	if block == 0 || block > 1000 || *selector.BlockHash != common.Hash(validatorEvidenceClientKeyRpcBoundary(block).Hash) {
		return nil, errors.New("authority selector names a foreign block")
	}
	data := call["data"]
	if len(data) == 0 {
		data = call["input"]
	}
	anchor := common.Address{0x77}
	if common.BytesToAddress(call["to"]) == anchor {
		companion := NewSTValidatorEvidence()
		fields := []struct {
			data  []byte
			value common.Hash
		}{
			{data: companion.PackCoordinator(), value: common.BytesToHash(self.domain.Coordinator[:])},
			{data: companion.PackSettlementVault(), value: common.BytesToHash(self.domain.SettlementVault[:])},
			{data: companion.PackChainId(), value: common.BigToHash(new(big.Int).SetUint64(self.domain.ChainID))},
			{data: companion.PackNetuid(), value: common.BigToHash(new(big.Int).SetUint64(uint64(self.domain.Netuid)))},
			{data: companion.PackGenesisHash(), value: common.Hash(self.domain.GenesisHash)},
			{data: companion.PackDeploymentIdHash(), value: common.Hash(self.domain.DeploymentIDHash)},
		}
		for _, field := range fields {
			if bytes.Equal(data, field.data) {
				if fault == "companion" {
					field.value[0]++
				}
				return field.value[:], nil
			}
		}
		return nil, errors.New("unknown companion getter")
	}
	if common.BytesToAddress(call["to"]) != self.domain.Coordinator || len(data) < 4 {
		return nil, errors.New("wrong coordinator address or calldata")
	}
	parsed, err := STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	method, err := parsed.MethodById(data[:4])
	if err != nil {
		return nil, err
	}
	inputs, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		return nil, err
	}
	var value any
	switch method.Name {
	case "currentEpoch":
		value = big.NewInt(9)
	case "netuid":
		value = self.domain.Netuid
	case "settlementVault":
		value = self.domain.SettlementVault
	case "validatorEvidence":
		value = anchor
	case "policyAt":
		if len(inputs) != 1 || inputs[0].(*big.Int).Uint64() != 9 {
			return nil, errors.New("wrong policy epoch")
		}
		hash := self.domain.PolicyHash
		if fault == "policy" {
			hash[0]++
		}
		value = STCoordinatorPolicySnapshot{PolicyHash: hash, EpochBlocks: 1000, EpochDepositCapRao: big.NewInt(1), CampaignDepositCapRao: big.NewInt(1)}
	case "operatorAt":
		if len(inputs) != 2 || inputs[1].(*big.Int).Uint64() != 9 || !inputs[0].(*big.Int).IsUint64() {
			return nil, errors.New("wrong operator selector")
		}
		noId := inputs[0].(*big.Int).Uint64()
		if noId < 1 || noId > 2 {
			return nil, errors.New("unknown operator")
		}
		value = STCoordinatorOperatorVersion{RootSigner: common.Address{byte(noId + 10)}, Active: fault != "inactive"}
	default:
		return nil, errors.New("unexpected authority getter")
	}
	encoded, err := method.Outputs.Pack(value)
	if fault == "trailing" {
		encoded = append(encoded, make([]byte, 32)...)
	}
	return encoded, err
}

// The actual Http boundary rejects oversized batches rather than hiding work.
func newValidatorEvidenceClientKeyRpcFixture(t testing.TB) (*validatorEvidenceClientKeyRpcFixture, *ethclient.Client, ClientKeyAuthorityRpcLimits) {
	t.Helper()
	fixture := &validatorEvidenceClientKeyRpcFixture{domain: protocol.ClientKeyHistoryDomain{ChainID: 945, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 1}}
	service := rpc.NewServer()
	if err := service.RegisterName("eth", fixture); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterName("chain", fixture); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
		_ = r.Body.Close()
		var calls []json.RawMessage
		if err != nil || len(body) > 1024*1024 || json.Unmarshal(body, &calls) != nil || len(calls) == 0 || len(calls) > MaxClientKeyAuthorityRpcBatchMembers {
			http.Error(w, "invalid actual batch", http.StatusBadRequest)
			return
		}
		fixture.stateLock.Lock()
		fixture.httpRequests++
		fixture.largestBatch = max(fixture.largestBatch, len(calls))
		if fixture.failAtHttp != 0 && fixture.httpRequests == fixture.failAtHttp {
			fixture.fault = "native-wrong"
		}
		fixture.stateLock.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		service.ServeHTTP(w, r)
	}))
	client, err := ethclient.DialContext(t.Context(), endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); endpoint.Close(); service.Stop() })
	return fixture, client, ClientKeyAuthorityRpcLimits{MaximumRequests: protocol.MaxClientKeyObservationBatchRpcRequests, MaximumMethods: protocol.MaxClientKeyObservationBatchRpcMethods, MaximumBytes: protocol.MaxClientKeyObservationBatchControlBytes}
}

// Two operators at two boundaries share immutable views, not root authority.
func TestValidatorEvidenceClientKeyRpcSharesExactBoundariesAcrossOperators(t *testing.T) {
	fixture, client, limits := newValidatorEvidenceClientKeyRpcFixture(t)
	queries := []ClientKeyAuthorityQuery{}
	for _, block := range []uint64{900, 1000} {
		for _, noId := range []uint64{1, 2} {
			domain := fixture.domain
			domain.NoID = noId
			queries = append(queries, ClientKeyAuthorityQuery{Domain: domain, Boundary: validatorEvidenceClientKeyRpcBoundary(block)})
		}
	}
	actual, work, err := ReadClientKeyAuthoritiesContext(t.Context(), client, queries, limits)
	httpRequests, methods, _ := fixture.counts()
	if err != nil || len(actual) != 4 || work.Requests != 4 || work.Methods != 36 || httpRequests != 4 || methods != 36 {
		t.Fatalf("actual shared reads: observations=%d work=%+v Http=%d methods=%d error=%v", len(actual), work, httpRequests, methods, err)
	}
	for index, value := range actual {
		if value.Query != queries[index] || value.Operator.RootSigner != (common.Address{byte(queries[index].Domain.NoID + 10)}) {
			t.Fatal("operator authority was merged")
		}
	}
}

// Current authenticated registration includes the final witness in four Http.
func TestValidatorEvidenceClientKeyRpcCurrentRegistrationUsesFourRequests(t *testing.T) {
	fixture, client, limits := newValidatorEvidenceClientKeyRpcFixture(t)
	actual, work, err := ReadCurrentClientKeyAuthorityContext(t.Context(), client, fixture.domain, limits)
	httpRequests, methods, _ := fixture.counts()
	if err != nil || actual.Query.Boundary != validatorEvidenceClientKeyRpcBoundary(1000) || actual.Operator.RootSigner != (common.Address{11}) || work.Requests != 4 || work.Methods != 20 || httpRequests != 4 || methods != 20 {
		t.Fatalf("current authority: %+v work=%+v error=%v", actual, work, err)
	}
}

// Distinct registration blocks remain real work. No larger hidden batch or
// single logical-work token can make an excessive census appear admissible.
func TestValidatorEvidenceClientKeyRpcDistinctBoundaryAdmissionPrecedesIo(t *testing.T) {
	fixture, client, limits := newValidatorEvidenceClientKeyRpcFixture(t)
	queries := make([]ClientKeyAuthorityQuery, 405)
	for index := range queries {
		queries[index] = ClientKeyAuthorityQuery{Domain: fixture.domain, Boundary: validatorEvidenceClientKeyRpcBoundary(uint64(index + 1))}
	}
	actual, work, err := ReadClientKeyAuthoritiesContext(t.Context(), client, queries, limits)
	httpRequests, _, _ := fixture.counts()
	if !errors.Is(err, ErrClientKeyAuthorityRpcWork) || actual != nil || work.Requests != 0 || httpRequests != 0 {
		t.Fatalf("excessive work escaped preflight: %+v %v", work, err)
	}
	actual, work, err = ReadClientKeyAuthoritiesContext(t.Context(), client, queries[:26], limits)
	httpRequests, _, _ = fixture.counts()
	if !errors.Is(err, ErrClientKeyAuthorityRpcWork) || actual != nil || work.Requests != 0 || httpRequests != 0 {
		t.Fatal("one-over Http batch work was sent before admission")
	}
	actual, work, err = ReadClientKeyAuthoritiesContext(t.Context(), client, queries[:17], limits)
	httpRequests, methods, largest := fixture.counts()
	if err != nil || len(actual) != 17 || work.Requests != 8 || work.Methods != 244 || httpRequests != 8 || methods != 244 || largest != 50 {
		t.Fatalf("separate bounded work units: %+v %v", work, err)
	}
}

// Native null/unavailable, wrong deployment identity and malformed ABI fail
// through actual transport, never through an already-approved test callback.
func TestValidatorEvidenceClientKeyRpcRejectsIdentityAndMemberFailures(t *testing.T) {
	for _, fault := range []string{"native-null", "native-unavailable", "native-wrong", "chain", "canonical", "companion", "policy", "inactive", "trailing"} {
		fixture, client, limits := newValidatorEvidenceClientKeyRpcFixture(t)
		fixture.stateLock.Lock()
		fixture.fault = fault
		fixture.stateLock.Unlock()
		actual, _, err := ReadClientKeyAuthoritiesContext(t.Context(), client, []ClientKeyAuthorityQuery{{Domain: fixture.domain, Boundary: validatorEvidenceClientKeyRpcBoundary(1000)}}, limits)
		if err == nil || actual != nil {
			t.Fatalf("%s acquired authority: %+v %v", fault, actual, err)
		}
		fixture.stateLock.Lock()
		fixture.fault = ""
		fixture.stateLock.Unlock()
		actual, _, err = ReadClientKeyAuthoritiesContext(t.Context(), client, []ClientKeyAuthorityQuery{{Domain: fixture.domain, Boundary: validatorEvidenceClientKeyRpcBoundary(1000)}}, limits)
		if err != nil || len(actual) != 1 {
			t.Fatalf("%s failed owner survived into fresh read: %v", fault, err)
		}
	}
}

// A changed final native witness clears all preceding successful views.
func TestValidatorEvidenceClientKeyRpcFinalWitnessClearsSuccessfulPrefix(t *testing.T) {
	fixture, client, limits := newValidatorEvidenceClientKeyRpcFixture(t)
	fixture.failAtHttp = 4
	actual, work, err := ReadClientKeyAuthoritiesContext(t.Context(), client, []ClientKeyAuthorityQuery{{Domain: fixture.domain, Boundary: validatorEvidenceClientKeyRpcBoundary(1000)}}, limits)
	if err == nil || actual != nil || work.Requests != 4 {
		t.Fatalf("late identity change returned authority: %+v %v", work, err)
	}
}

// The real request is held until caller cancellation; no sleep proves this.
func TestValidatorEvidenceClientKeyRpcCancellationJoinsActualTransport(t *testing.T) {
	fixture, client, limits := newValidatorEvidenceClientKeyRpcFixture(t)
	entered, left := make(chan struct{}), make(chan struct{})
	fixture.entered, fixture.left = entered, left
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		actual, _, err := ReadClientKeyAuthoritiesContext(ctx, client, []ClientKeyAuthorityQuery{{Domain: fixture.domain, Boundary: validatorEvidenceClientKeyRpcBoundary(1000)}}, limits)
		if actual != nil {
			err = errors.Join(err, errors.New("cancelled read returned authority"))
		}
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("actual cancellation was lost: %v", err)
	}
	<-left
}
