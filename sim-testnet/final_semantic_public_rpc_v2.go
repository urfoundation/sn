//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// ValidatorSourcesV2 reuses the admitted independent public clients. The local
// HTTP facade performs no network dial: it preserves actual EVM batches through
// the existing quota/retry transport, while native capture wraps that reader's
// existing client. Only the ordinary archive verifier creates observations.
func (self *PublicFinalSemanticChainReader) ValidatorSourcesV2(ctx context.Context, evidence *FinalSemanticEvidence, archive *validatorpkg.ReleaseEvidenceV2Archive) (observations []validatorpkg.ReleaseEvidenceV2DecisionObservation, exchanges []FinalRPCExchange, resultErr error) {
	if ctx == nil || self == nil || evidence == nil || self.evm == nil || self.native == nil || self.native.Client == nil || archive == nil {
		return nil, nil, errors.New("public V2 historical reader owner differs")
	}
	ownedReader, err := self.finalV2InvocationReader(evidence)
	if err != nil {
		return nil, nil, err
	}
	self, evidence = ownedReader, ownedReader.evidence
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			observations, exchanges = nil, nil
		}
	}()
	bounds, err := archive.SourceReadBoundsV2(ctx)
	if err != nil {
		return nil, nil, err
	}
	recorder := newFinalV2RPCRecorder(ctx, self, min(bounds.MaxControlBytes, uint64(maximumCampaignEvidenceRawFileBytes)), min(bounds.MaxHistoryBytes, uint64(maximumCampaignEvidenceRawFileBytes)))
	transport, err := rpc.DialOptions(ctx, "http://validator-evidence-read.invalid", rpc.WithHTTPClient(&http.Client{Transport: &finalV2EVMReadTransport{owner: recorder}}))
	if err != nil {
		return nil, nil, err
	}
	defer transport.Close()
	chain, err := validatorpkg.NewReleaseChainReadRPCContext(ctx, transport, self.canonicalEVMRPC, common.HexToAddress(evidence.Deployment.CoordinatorProxy))
	if err != nil {
		return nil, nil, err
	}
	genesis, err := gsrpctypes.NewHashFromHexString(evidence.GenesisHash)
	if err != nil {
		return nil, nil, err
	}
	// ObserveSources authenticates metadata at each exact historical block. A
	// latest-metadata constructor here would add an unrelated moving snapshot.
	native := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: self.native.Client}, GenesisHash: genesis}
	observations, err = archive.ObserveSourcesWithNativeReadsV2(ctx, chain, native, recorder.retainNative)
	if err != nil {
		return nil, nil, err
	}
	if err := archive.AuthenticatePublicationsV2(ctx, chain, evidence.Window.FirstEpoch, evidence.Window.EpochCount, evidence.EVMTerminalHead.Number, common.HexToHash(evidence.EVMTerminalHead.Hash)); err != nil {
		return nil, nil, err
	}
	exchanges, err = recorder.finish()
	return observations, exchanges, err
}

// Production factories and verifiers deliberately hold detached evidence
// snapshots. Bind their complete stable content, then detach the invocation
// again so a retained factory argument cannot mutate ongoing reads. Only the
// two fields populated by public sealing are excluded from source identity.
func (self *PublicFinalSemanticChainReader) finalV2InvocationReader(evidence *FinalSemanticEvidence) (*PublicFinalSemanticChainReader, error) {
	if self == nil || self.evidence == nil || evidence == nil {
		return nil, errors.New("public V2 evidence source is unavailable")
	}
	source, err := finalSemanticEvidenceDetachedCopy(self.evidence)
	if err != nil {
		return nil, err
	}
	target, err := finalSemanticEvidenceDetachedCopy(evidence)
	if err != nil {
		return nil, err
	}
	source.PublicVerification, target.PublicVerification = nil, nil
	source.EvidenceHash, target.EvidenceHash = "", ""
	sourceHash, err := canonicalHashHex(source)
	if err != nil {
		return nil, err
	}
	targetHash, err := canonicalHashHex(target)
	if err != nil {
		return nil, err
	}
	if sourceHash != targetHash {
		return nil, errors.New("public V2 immutable evidence source differs from the reader factory")
	}
	owned := *self
	owned.evidence = target
	return &owned, nil
}

// A single invocation owns all immutable snapshots. Equal request identities
// must return equal JSON values; later callers never inherit this map. Sorting
// removes scheduling differences between the ordinary concurrent chain reads.
type finalV2RPCRecorder struct {
	ctx                       context.Context
	reader                    *PublicFinalSemanticChainReader
	maximum, remaining        uint64
	mu                        sync.Mutex
	exchanges                 map[string]FinalRPCExchange
	nativeHeads, evmHeads     map[string]ChainHead
	nativeNumbers, evmNumbers map[uint64]string
	liveNativeHeads           map[string]bool
	err                       error
}

func newFinalV2RPCRecorder(ctx context.Context, reader *PublicFinalSemanticChainReader, maximum, total uint64) *finalV2RPCRecorder {
	return &finalV2RPCRecorder{ctx: ctx, reader: reader, maximum: maximum, remaining: total, exchanges: map[string]FinalRPCExchange{}, nativeHeads: map[string]ChainHead{}, evmHeads: map[string]ChainHead{}, nativeNumbers: map[uint64]string{}, evmNumbers: map[uint64]string{}, liveNativeHeads: map[string]bool{}}
}

func (self *finalV2RPCRecorder) fail(err error) {
	if err == nil {
		return
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.err == nil {
		self.err = err
	}
}

func (self *finalV2RPCRecorder) retain(exchange FinalRPCExchange) error {
	if self == nil || self.ctx == nil || self.maximum == 0 {
		return errors.New("public V2 transcript owner is incomplete")
	}
	if err := errors.Join(self.ctx.Err(), verifyFinalHead("V2 source", exchange.PinnedHead)); err != nil {
		return err
	}
	if len(exchange.Params) == 0 || len(exchange.Result) == 0 || uint64(len(exchange.Params)) > self.maximum || uint64(len(exchange.Result)) > self.maximum {
		return errors.New("public V2 exchange exceeds its original source byte bound")
	}
	request, response, err := finalRPCExchangeHashes(exchange)
	if err != nil {
		return err
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.err != nil {
		return self.err
	}
	if prior, found := self.exchanges[request]; found {
		if prior.ResponseHash != response {
			self.err = errors.New("public V2 immutable request returned conflicting results")
		}
		return self.err
	}
	exchange.RequestHash, exchange.ResponseHash = request, response
	encoded, err := json.Marshal(exchange)
	if err != nil || uint64(len(encoded)) > self.remaining {
		self.err = errors.Join(errors.New("public V2 transcript exceeds its existing archive byte bound"), err)
		return self.err
	}
	self.remaining -= uint64(len(encoded))
	exchange.Params, exchange.Result = bytes.Clone(exchange.Params), bytes.Clone(exchange.Result)
	self.exchanges[request] = exchange
	return nil
}

func (self *finalV2RPCRecorder) finish() ([]FinalRPCExchange, error) {
	self.mu.Lock()
	defer self.mu.Unlock()
	if err := errors.Join(self.err, self.ctx.Err()); err != nil {
		return nil, err
	}
	if len(self.exchanges) == 0 {
		return nil, errors.New("public V2 source reader retained no immutable exchanges")
	}
	result := make([]FinalRPCExchange, 0, len(self.exchanges))
	for _, exchange := range self.exchanges {
		exchange.Params, exchange.Result = bytes.Clone(exchange.Params), bytes.Clone(exchange.Result)
		result = append(result, exchange)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Chain != b.Chain {
			return a.Chain < b.Chain
		}
		if a.PinnedHead.Number != b.PinnedHead.Number {
			return a.PinnedHead.Number < b.PinnedHead.Number
		}
		if a.PinnedHead.Hash != b.PinnedHead.Hash {
			return a.PinnedHead.Hash < b.PinnedHead.Hash
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		return a.RequestHash < b.RequestHash
	})
	return result, nil
}

type finalV2EVMReadTransport struct{ owner *finalV2RPCRecorder }

type finalV2RPCRequest struct {
	Version string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type finalV2RPCResponse struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type finalV2RPCResult struct {
	maximum uint64
	raw     []byte
	budget  *finalV2RPCDecodeBudget
}

type finalV2RPCDecodeBudget struct {
	mu        sync.Mutex
	remaining uint64
}

func (self *finalV2RPCDecodeBudget) take(size uint64) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	if size > self.remaining {
		return errors.New("public V2 RPC batch exceeds its source byte bound")
	}
	self.remaining -= size
	return nil
}

func (self *finalV2RPCResult) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || self.maximum == 0 || uint64(len(raw)) > self.maximum {
		return errors.New("public V2 RPC result exceeds its source bound")
	}
	if self.budget != nil {
		if err := self.budget.take(uint64(len(raw))); err != nil {
			return err
		}
	}
	self.raw = bytes.Clone(raw)
	return nil
}

func (self *finalV2EVMReadTransport) RoundTrip(request *http.Request) (response *http.Response, resultErr error) {
	if self == nil || self.owner == nil || request == nil || request.Body == nil || request.Method != http.MethodPost {
		return nil, errors.New("public V2 EVM read request is incomplete")
	}
	defer request.Body.Close()
	owner := self.owner
	defer func() { owner.fail(resultErr) }()
	ctx, cancel := context.WithCancel(request.Context())
	joined := make(chan struct{})
	stop := context.AfterFunc(owner.ctx, func() { defer close(joined); cancel() })
	defer func() {
		if !stop() {
			<-joined
		}
		cancel()
	}()
	if err := errors.Join(ctx.Err(), owner.ctx.Err()); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, int64(owner.maximum)+1))
	if err != nil || uint64(len(raw)) > owner.maximum {
		return nil, errors.Join(errors.New("public V2 RPC request exceeds its source bound"), err)
	}
	batchMode := len(bytes.TrimSpace(raw)) != 0 && bytes.TrimSpace(raw)[0] == '['
	var requests []finalV2RPCRequest
	if batchMode {
		err = json.Unmarshal(raw, &requests)
	} else {
		requests = make([]finalV2RPCRequest, 1)
		err = json.Unmarshal(raw, &requests[0])
	}
	if err != nil || len(requests) == 0 || len(requests) > 50 {
		return nil, errors.Join(errors.New("public V2 RPC request differs from the existing 50-call batch bound"), err)
	}
	values := make([]*finalV2RPCResult, len(requests))
	batch := make([]rpc.BatchElem, len(requests))
	ids := map[string]bool{}
	for index, call := range requests {
		if call.Version != "2.0" || len(call.ID) == 0 || bytes.Equal(call.ID, []byte("null")) || ids[string(call.ID)] {
			return nil, errors.New("public V2 RPC request identity is invalid")
		}
		ids[string(call.ID)] = true
		if err := finalV2EVMReadMethod(call.Method, call.Params); err != nil {
			return nil, err
		}
		args := make([]any, len(call.Params))
		for i, param := range call.Params {
			args[i] = param
		}
		values[index] = &finalV2RPCResult{maximum: owner.maximum}
		batch[index] = rpc.BatchElem{Method: call.Method, Args: args, Result: &values[index]}
	}
	// Keep a real source batch intact, including the existing public transport's
	// aggregate rate gate. No unpaced secondary endpoint is opened here.
	var responseBudget *finalV2RPCDecodeBudget
	err = retryFinalSemanticRPCCall(ctx, nil, owner.reader.evmRetry, func(attempt context.Context) error {
		responseBudget = &finalV2RPCDecodeBudget{remaining: owner.maximum}
		for index := range batch {
			values[index] = &finalV2RPCResult{maximum: owner.maximum, budget: responseBudget}
			batch[index].Error = nil
		}
		if batchMode {
			return owner.reader.evm.BatchCallContext(attempt, batch)
		}
		return owner.reader.evm.CallContext(attempt, &values[0], batch[0].Method, batch[0].Args...)
	})
	if err != nil {
		return nil, err
	}
	responses := make([]finalV2RPCResponse, len(requests))
	for index, call := range requests {
		if batch[index].Error != nil {
			return nil, fmt.Errorf("public V2 batch member %d: %w", index, batch[index].Error)
		}
		value := values[index]
		if value == nil {
			if err := responseBudget.take(4); err != nil {
				return nil, err
			}
			value = &finalV2RPCResult{raw: []byte("null")}
		}
		if len(value.raw) == 0 {
			return nil, errors.New("public V2 RPC transport omitted its result")
		}
		if err := owner.retainEVM(ctx, call.Method, call.Params, value.raw); err != nil {
			return nil, err
		}
		responses[index] = finalV2RPCResponse{Version: "2.0", ID: call.ID, Result: value.raw}
	}
	if err := errors.Join(ctx.Err(), owner.ctx.Err()); err != nil {
		return nil, err
	}
	var encoded []byte
	if batchMode {
		encoded, err = json.Marshal(responses)
	} else {
		encoded, err = json.Marshal(responses[0])
	}
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(encoded)), Request: request}, nil
}

// Additional canonical-header reads use the same original per-response bound
// before allocation, and the already admitted public transport beneath it.
func (self *finalV2RPCRecorder) sourceRaw(ctx context.Context, chain string, head ChainHead, method string, args ...any) (json.RawMessage, FinalRPCExchange, error) {
	params, err := json.Marshal(args)
	if err != nil || uint64(len(params)) > self.maximum {
		return nil, FinalRPCExchange{}, errors.Join(errors.New("public V2 header request exceeds its bound"), err)
	}
	value := &finalV2RPCResult{maximum: self.maximum}
	if chain == "substrate" {
		err = self.reader.native.Client.CallContext(ctx, &value, method, args...)
	} else {
		err = retryFinalSemanticRPCCall(ctx, nil, self.reader.evmRetry, func(attempt context.Context) error {
			value = &finalV2RPCResult{maximum: self.maximum}
			return self.reader.evm.CallContext(attempt, &value, method, args...)
		})
	}
	if err := errors.Join(err, ctx.Err(), self.ctx.Err()); err != nil {
		return nil, FinalRPCExchange{}, err
	}
	if value == nil {
		value = &finalV2RPCResult{raw: []byte("null")}
	}
	if len(value.raw) == 0 {
		return nil, FinalRPCExchange{}, errors.New("public V2 header transport omitted its result")
	}
	return value.raw, FinalRPCExchange{Chain: chain, Method: method, Params: params, PinnedHead: head, Result: value.raw}, nil
}
