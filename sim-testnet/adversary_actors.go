package main

// adversary_actors.go contains the shared-testnet-safe actors. Network actors
// issue only capped reads or requests to our loopback operator APIs; pure
// actors continuously mutate copies of identities, artifacts, signatures and
// economic inputs. No actor submits a Subtensor or EVM transaction.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

type adversaryRequestGate struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
	now      func() time.Time
}

// adversaryFaultWindow attributes only explicitly scheduled process outages.
// When a target is restored, one request-timeout grace period covers calls that
// began during the fault but completed after restoration; it cannot authorize
// another target or a later persistent failure.
type adversaryFaultWindow struct {
	mu     sync.Mutex
	active map[string]bool
	grace  map[string]time.Time
	period time.Duration
	now    func() time.Time
}

func newAdversaryFaultWindow(period time.Duration) *adversaryFaultWindow {
	return &adversaryFaultWindow{active: map[string]bool{}, grace: map[string]time.Time{}, period: period, now: time.Now}
}

func (self *adversaryFaultWindow) Update(targets []string) {
	if self == nil {
		return
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	now := self.now()
	next := map[string]bool{}
	for _, target := range targets {
		if target != "" {
			next[target] = true
			delete(self.grace, target)
		}
	}
	for target := range self.active {
		if !next[target] {
			self.grace[target] = now.Add(self.period)
		}
	}
	for target, until := range self.grace {
		if !until.After(now) {
			delete(self.grace, target)
		}
	}
	self.active = next
}

func (self *adversaryFaultWindow) Expected(target string) bool {
	if self == nil || target == "" {
		return false
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.active[target] {
		return true
	}
	until, ok := self.grace[target]
	if ok && until.After(self.now()) {
		return true
	}
	if ok {
		delete(self.grace, target)
	}
	return false
}

func newAdversaryRequestGate(requestsPerSecond int) (*adversaryRequestGate, error) {
	if requestsPerSecond <= 0 {
		return nil, errors.New("adversarial request rate must be positive")
	}
	return &adversaryRequestGate{interval: time.Second / time.Duration(requestsPerSecond), now: time.Now}, nil
}

func (self *adversaryRequestGate) reserve(at time.Time) time.Duration {
	return self.reserveSlots(at, 1)
}

func (self *adversaryRequestGate) reserveSlots(at time.Time, slots int) time.Duration {
	self.mu.Lock()
	defer self.mu.Unlock()
	if slots < 1 {
		return 0
	}
	start := at
	if self.next.After(start) {
		start = self.next
	}
	// A deliberately concurrent pair still consumes two slots from the
	// long-run budget. Launch at the second reserved slot so the pair overlaps
	// without increasing the configured average request rate.
	launch := start.Add(time.Duration(slots-1) * self.interval)
	self.next = start.Add(time.Duration(slots) * self.interval)
	return launch.Sub(at)
}

func (self *adversaryRequestGate) Wait(ctx context.Context) error {
	delay := self.reserve(self.now())
	return waitAdversaryDelay(ctx, delay)
}

func (self *adversaryRequestGate) WaitSlots(ctx context.Context, slots int) error {
	if slots < 1 {
		return errors.New("adversarial request reservation must contain at least one slot")
	}
	delay := self.reserveSlots(self.now(), slots)
	return waitAdversaryDelay(ctx, delay)
}

func waitAdversaryDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type adversaryHTTP struct {
	gate    *adversaryRequestGate
	timeout time.Duration
}

func (self *adversaryHTTP) do(ctx context.Context, method, endpoint, sourceIP string, body []byte, limit int64) (int, []byte, error) {
	if err := self.gate.Wait(ctx); err != nil {
		return 0, nil, err
	}
	return self.doReserved(ctx, method, endpoint, sourceIP, body, limit)
}

func (self *adversaryHTTP) doReserved(ctx context.Context, method, endpoint, sourceIP string, body []byte, limit int64) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if len(body) != 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if sourceIP != "" {
		ip := net.ParseIP(sourceIP)
		if ip == nil || !ip.IsLoopback() {
			return 0, nil, fmt.Errorf("adversarial source %q is not loopback", sourceIP)
		}
		transport.DialContext = (&net.Dialer{LocalAddr: &net.TCPAddr{IP: ip}}).DialContext
	}
	transport.DisableKeepAlives = true
	client := &http.Client{Transport: transport, Timeout: self.timeout}
	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return 0, nil, err
	}
	defer resp.Body.Close()
	defer transport.CloseIdleConnections()
	response, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if int64(len(response)) > limit {
		return resp.StatusCode, nil, fmt.Errorf("adversarial response exceeded %d bytes", limit)
	}
	return resp.StatusCode, response, nil
}

type adversaryHTTPResponse struct {
	Status int
	Body   []byte
	Err    error
}

// doConcurrentPair spends two rate-gate slots before starting a synchronized
// pair. It is intentionally limited to two requests: this is enough to force
// the per-trail serialization edge without turning a shared testnet into a
// load-test target.
func (self *adversaryHTTP) doConcurrentPair(ctx context.Context, method, endpoint, sourceIP string, body []byte, limit int64) ([2]adversaryHTTPResponse, error) {
	var responses [2]adversaryHTTPResponse
	if err := self.gate.WaitSlots(ctx, len(responses)); err != nil {
		return responses, err
	}
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(len(responses))
	done.Add(len(responses))
	for index := range responses {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			responses[index].Status, responses[index].Body, responses[index].Err = self.doReserved(ctx, method, endpoint, sourceIP, body, limit)
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return responses, nil
}

type operatorAPIAdversary struct {
	cfg    *ResolvedConfig
	http   *adversaryHTTP
	faults *adversaryFaultWindow
}

func (self *operatorAPIAdversary) ID() string                         { return "operator-api-pressure" }
func (self *operatorAPIAdversary) FaultWindow() *adversaryFaultWindow { return self.faults }

func (self *operatorAPIAdversary) Sample(ctx context.Context, phase adversarySamplePhase, sequence uint64) adversarySampleResult {
	operator := 1 + int(sequence%uint64(self.cfg.Config.Topology.Operators))
	base := fmt.Sprintf("http://127.0.0.1:%d", 18080+operator)
	endpoint := base + "/status"
	if phase == adversaryAttackPhase {
		paths := []string{
			"/verify/stats?limit=100000",
			"/verify/proofs?limit=10000",
			fmt.Sprintf("/sn/artifacts?deployment_id=%s&netuid=%d", self.cfg.Config.Deployment.DeploymentID, self.cfg.Netuid),
			"/verify/keys",
		}
		endpoint = base + paths[int(sequence%uint64(len(paths)))]
	}
	status, body, err := self.http.do(ctx, http.MethodGet, endpoint, "", nil, 32*1024*1024)
	if err != nil {
		if self.faults.Expected(fmt.Sprintf("operator-%d-api", operator)) {
			return adversarySampleResult{Outcome: adversaryOutcomeExpectedRejection, Detail: fmt.Sprintf("operator=%d scheduled API fault: %v", operator, err), Requests: 1, MaxInFlight: 1, Metrics: map[string]uint64{"scheduled_fault_rejections": 1}}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error(), Requests: 1, MaxInFlight: 1}
	}
	if status/100 != 2 || len(body) == 0 || !json.Valid(body) {
		if self.faults.Expected(fmt.Sprintf("operator-%d-api", operator)) {
			return adversarySampleResult{Outcome: adversaryOutcomeExpectedRejection, Detail: fmt.Sprintf("operator=%d scheduled API fault status=%d bytes=%d", operator, status, len(body)), Requests: 1, MaxInFlight: 1, Metrics: map[string]uint64{"scheduled_fault_rejections": 1}}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("operator=%d status=%d bytes=%d valid_json=%t", operator, status, len(body), json.Valid(body)), Requests: 1, MaxInFlight: 1}
	}
	return adversarySampleResult{
		Outcome: adversaryOutcomeSuccess, Detail: fmt.Sprintf("operator=%d status=%d bytes=%d", operator, status, len(body)), Requests: 1, MaxInFlight: 1,
		Metrics: map[string]uint64{
			"request_rate": 1, "response_bytes": uint64(len(body)), "5xx_count": 0,
			"error_rate_ppm": 0, "process_restarts": 0,
		},
	}
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type rpcBlock struct {
	Number string `json:"number"`
	Hash   string `json:"hash"`
}

// rpcRuntimeVersion contains the release-bound portion of the Substrate
// runtime identity. API lists are intentionally excluded from the continuous
// metric because the release doctor validates their exact call shapes.
type rpcRuntimeVersion struct {
	SpecName           string `json:"specName"`
	SpecVersion        uint32 `json:"specVersion"`
	TransactionVersion uint32 `json:"transactionVersion"`
}

type rpcAdversary struct {
	cfg  *ResolvedConfig
	http *adversaryHTTP
}

func (self *rpcAdversary) ID() string { return "rpc-consistency-pressure" }

func (self *rpcAdversary) call(ctx context.Context, endpoint, method string, parameters any, id uint64) (rpcResponse, error) {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": parameters})
	if err != nil {
		return rpcResponse{}, err
	}
	status, body, err := self.http.do(ctx, http.MethodPost, endpoint, "", payload, 4*1024*1024)
	if err != nil {
		return rpcResponse{}, err
	}
	if status/100 != 2 {
		return rpcResponse{}, fmt.Errorf("rpc %s returned HTTP %d", method, status)
	}
	var response rpcResponse
	if json.Unmarshal(body, &response) != nil || response.JSONRPC != "2.0" || response.ID != id {
		return rpcResponse{}, fmt.Errorf("rpc %s returned malformed envelope", method)
	}
	return response, nil
}

func decodeRPCBlock(response rpcResponse) (rpcBlock, uint64, error) {
	if response.Error != nil {
		return rpcBlock{}, 0, fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	var block rpcBlock
	if json.Unmarshal(response.Result, &block) != nil || !strings.HasPrefix(block.Number, "0x") || len(strings.TrimPrefix(block.Hash, "0x")) != 64 {
		return rpcBlock{}, 0, errors.New("rpc returned an invalid block")
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(block.Number, "0x"), 16, 64)
	return block, number, err
}

func decodeRPCQuantity(response rpcResponse) (uint64, error) {
	if response.Error != nil {
		return 0, fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	var quantity string
	if json.Unmarshal(response.Result, &quantity) != nil || len(quantity) < 3 || !strings.EqualFold(quantity[:2], "0x") {
		return 0, errors.New("rpc returned an invalid quantity")
	}
	return strconv.ParseUint(quantity[2:], 16, 64)
}

func decodeRPCRuntimeVersion(response rpcResponse) (rpcRuntimeVersion, error) {
	if response.Error != nil {
		return rpcRuntimeVersion{}, fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	var version rpcRuntimeVersion
	if json.Unmarshal(response.Result, &version) != nil || version.SpecName == "" || version.SpecVersion == 0 || version.TransactionVersion == 0 {
		return rpcRuntimeVersion{}, errors.New("rpc returned an invalid runtime version")
	}
	return version, nil
}

func validateRPCRuntimeIdentity(private, public rpcRuntimeVersion, expectedSpec, expectedTransaction uint32) error {
	if private.SpecName != "node-subtensor" || public.SpecName != private.SpecName {
		return fmt.Errorf("runtime spec names operational=%q public=%q", private.SpecName, public.SpecName)
	}
	if private.SpecVersion != public.SpecVersion || private.SpecVersion != expectedSpec {
		return fmt.Errorf("runtime specs operational=%d public=%d expected=%d", private.SpecVersion, public.SpecVersion, expectedSpec)
	}
	if private.TransactionVersion != public.TransactionVersion || private.TransactionVersion != expectedTransaction {
		return fmt.Errorf("transaction versions operational=%d public=%d expected=%d", private.TransactionVersion, public.TransactionVersion, expectedTransaction)
	}
	return nil
}

func abiUint16CallData(signature string, value uint16) string {
	selector := ethcrypto.Keccak256([]byte(signature))[:4]
	data := make([]byte, 4+32)
	copy(data, selector)
	binary.BigEndian.PutUint16(data[len(data)-2:], value)
	return "0x" + hex.EncodeToString(data)
}

func decodeABIUint256(response rpcResponse) (*big.Int, error) {
	if response.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	var encoded string
	if json.Unmarshal(response.Result, &encoded) != nil || !strings.HasPrefix(encoded, "0x") || len(encoded) != 66 {
		return nil, errors.New("eth_call returned an invalid uint256")
	}
	value := new(big.Int)
	if _, ok := value.SetString(encoded[2:], 16); !ok {
		return nil, errors.New("eth_call uint256 is not hexadecimal")
	}
	return value, nil
}

func (self *rpcAdversary) callUint16Precompile(ctx context.Context, endpoint, address, signature string, value uint16, blockTag string, id uint64) (*big.Int, error) {
	response, err := self.call(ctx, endpoint, "eth_call", []any{map[string]string{
		"to": address, "data": abiUint16CallData(signature, value),
	}, blockTag}, id)
	if err != nil {
		return nil, err
	}
	return decodeABIUint256(response)
}

func validateSubnetPrecompileSentinels(privateSpot, publicSpot, privateMoving, publicMoving, privateUIDs, publicUIDs, taoReserve, alphaReserve *big.Int) error {
	values := []*big.Int{privateSpot, publicSpot, privateMoving, publicMoving, privateUIDs, publicUIDs, taoReserve, alphaReserve}
	for _, value := range values {
		if value == nil || value.Sign() <= 0 || !value.IsUint64() {
			return errors.New("subnet precompile sentinel is zero, negative, missing, or wider than uint64")
		}
	}
	if privateSpot.Cmp(publicSpot) != 0 || privateMoving.Cmp(publicMoving) != 0 || privateUIDs.Cmp(publicUIDs) != 0 {
		return errors.New("operational and public subnet precompile sentinels disagree")
	}
	return nil
}

// mevShieldFinalityEraExpiryModel captures the SDK failure reported in
// RaoFoundation/bittensor#3395. A mortal era anchored at finalized is already
// stale at pool admission once best-finalized consumes its entire period.
// The release writer does not use this SDK path, but the live lag and the
// vulnerable predicate remain useful continuous evidence.
func mevShieldFinalityEraExpiryModel(finalized, best, period uint64) (lag uint64, expired bool, err error) {
	if period == 0 {
		return 0, false, errors.New("MEV-shield mortal era period is zero")
	}
	if best < finalized {
		return 0, false, errors.New("best head precedes finalized head")
	}
	lag = best - finalized
	return lag, lag >= period, nil
}

func (self *rpcAdversary) Sample(ctx context.Context, phase adversarySamplePhase, sequence uint64) adversarySampleResult {
	started := time.Now()
	privateEndpoint := self.cfg.OperationalEVM
	publicEndpoint := self.cfg.Public.Chain.EVMPublicReadEndpoint
	if phase == adversaryControlPhase {
		privateChain, privateErr := self.call(ctx, privateEndpoint, "eth_chainId", []any{}, sequence*2+1)
		publicChain, publicErr := self.call(ctx, publicEndpoint, "eth_chainId", []any{}, sequence*2+2)
		privateID, privateDecodeErr := decodeRPCQuantity(privateChain)
		publicID, publicDecodeErr := decodeRPCQuantity(publicChain)
		if privateErr != nil || publicErr != nil || privateDecodeErr != nil || publicDecodeErr != nil || privateID != publicID || privateID != 945 {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("chain-id operational=%s/%d public=%s/%d errors=%v/%v/%v/%v", privateChain.Result, privateID, publicChain.Result, publicID, privateErr, publicErr, privateDecodeErr, publicDecodeErr), Requests: 2, MaxInFlight: 1}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: "operational/public EVM chain id 945", Requests: 2, MaxInFlight: 1}
	}
	if sequence%3 == 1 {
		response, callErr := self.call(ctx, privateEndpoint, "urnetwork_adversarial_unknownMethod", []any{}, sequence+1)
		if callErr != nil || response.Error == nil || response.Error.Code == 0 {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("unknown method was not rejected: %v", callErr), Requests: 1, MaxInFlight: 1}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeExpectedRejection, Detail: fmt.Sprintf("unknown method rejected code=%d", response.Error.Code), Requests: 1, MaxInFlight: 1}
	}
	privateResponse, privateErr := self.call(ctx, privateEndpoint, "eth_getBlockByNumber", []any{"finalized", false}, sequence*10+1)
	publicResponse, publicErr := self.call(ctx, publicEndpoint, "eth_getBlockByNumber", []any{"finalized", false}, sequence*10+2)
	if privateErr != nil || publicErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("finalized heads: operational=%v public=%v", privateErr, publicErr), Requests: 2, MaxInFlight: 1}
	}
	privateFinalized, privateNumber, privateErr := decodeRPCBlock(privateResponse)
	publicFinalized, publicNumber, publicErr := decodeRPCBlock(publicResponse)
	if privateErr != nil || publicErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("decode finalized heads: operational=%v public=%v", privateErr, publicErr), Requests: 2, MaxInFlight: 1}
	}
	lag := privateNumber
	if publicNumber > lag {
		lag = publicNumber - lag
	} else {
		lag -= publicNumber
	}
	if lag > uint64(self.cfg.Policy.Safety.MaximumFinalizedHeadLagBlocks) {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("finalized lag=%d operational=%d public=%d", lag, privateNumber, publicNumber), Requests: 2, MaxInFlight: 1}
	}
	privateLatestResponse, privateLatestErr := self.call(ctx, privateEndpoint, "eth_getBlockByNumber", []any{"latest", false}, sequence*10+13)
	publicLatestResponse, publicLatestErr := self.call(ctx, publicEndpoint, "eth_getBlockByNumber", []any{"latest", false}, sequence*10+14)
	_, privateLatest, privateLatestDecodeErr := decodeRPCBlock(privateLatestResponse)
	_, publicLatest, publicLatestDecodeErr := decodeRPCBlock(publicLatestResponse)
	if privateLatestErr != nil || publicLatestErr != nil || privateLatestDecodeErr != nil || publicLatestDecodeErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("best heads: operational=%v/%v public=%v/%v", privateLatestErr, privateLatestDecodeErr, publicLatestErr, publicLatestDecodeErr), Requests: 4, MaxInFlight: 1}
	}
	privateBestLag, privateSDKExpired, privateExpiryErr := mevShieldFinalityEraExpiryModel(privateNumber, privateLatest, 8)
	publicBestLag, publicSDKExpired, publicExpiryErr := mevShieldFinalityEraExpiryModel(publicNumber, publicLatest, 8)
	if privateExpiryErr != nil || publicExpiryErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("MEV-shield era model operational=%v public=%v", privateExpiryErr, publicExpiryErr), Requests: 4, MaxInFlight: 1}
	}
	bestFinalizedLag := max64(privateBestLag, publicBestLag)
	sdkMEVShieldExpired := privateSDKExpired || publicSDKExpired
	commonNumber := privateNumber
	if publicNumber < commonNumber {
		commonNumber = publicNumber
	}
	tag := fmt.Sprintf("0x%x", commonNumber)
	privateCommon, privateErr := self.call(ctx, privateEndpoint, "eth_getBlockByNumber", []any{tag, false}, sequence*10+3)
	publicCommon, publicErr := self.call(ctx, publicEndpoint, "eth_getBlockByNumber", []any{tag, false}, sequence*10+4)
	privateAt, _, decodePrivateErr := decodeRPCBlock(privateCommon)
	publicAt, _, decodePublicErr := decodeRPCBlock(publicCommon)
	if privateErr != nil || publicErr != nil || decodePrivateErr != nil || decodePublicErr != nil || !strings.EqualFold(privateAt.Hash, publicAt.Hash) {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("common-height disagreement height=%d operational=%s public=%s errors=%v/%v/%v/%v", commonNumber, privateAt.Hash, publicAt.Hash, privateErr, publicErr, decodePrivateErr, decodePublicErr), Requests: 6, MaxInFlight: 1}
	}
	privateRuntimeResponse, privateRuntimeErr := self.call(ctx, privateEndpoint, "state_getRuntimeVersion", []any{privateFinalized.Hash}, sequence*20+15)
	publicRuntimeResponse, publicRuntimeErr := self.call(ctx, publicEndpoint, "state_getRuntimeVersion", []any{publicFinalized.Hash}, sequence*20+16)
	privateRuntime, privateRuntimeDecodeErr := decodeRPCRuntimeVersion(privateRuntimeResponse)
	publicRuntime, publicRuntimeDecodeErr := decodeRPCRuntimeVersion(publicRuntimeResponse)
	if privateRuntimeErr != nil || publicRuntimeErr != nil || privateRuntimeDecodeErr != nil || publicRuntimeDecodeErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("runtime identity operational=%v/%v public=%v/%v", privateRuntimeErr, privateRuntimeDecodeErr, publicRuntimeErr, publicRuntimeDecodeErr), Requests: 8, MaxInFlight: 1}
	}
	if runtimeErr := validateRPCRuntimeIdentity(privateRuntime, publicRuntime, self.cfg.Release.Runtime.SpecVersion, self.cfg.Release.Runtime.TransactionVersion); runtimeErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: runtimeErr.Error(), Requests: 8, MaxInFlight: 1}
	}
	const alphaPrecompile = "0x0000000000000000000000000000000000000808"
	const metagraphPrecompile = "0x0000000000000000000000000000000000000802"
	privateSpot, privateSpotErr := self.callUint16Precompile(ctx, privateEndpoint, alphaPrecompile, "getAlphaPrice(uint16)", self.cfg.Netuid, tag, sequence*10+5)
	publicSpot, publicSpotErr := self.callUint16Precompile(ctx, publicEndpoint, alphaPrecompile, "getAlphaPrice(uint16)", self.cfg.Netuid, tag, sequence*10+6)
	privateMoving, privateMovingErr := self.callUint16Precompile(ctx, privateEndpoint, alphaPrecompile, "getMovingAlphaPrice(uint16)", self.cfg.Netuid, tag, sequence*10+7)
	publicMoving, publicMovingErr := self.callUint16Precompile(ctx, publicEndpoint, alphaPrecompile, "getMovingAlphaPrice(uint16)", self.cfg.Netuid, tag, sequence*10+8)
	privateUIDs, privateUIDsErr := self.callUint16Precompile(ctx, privateEndpoint, metagraphPrecompile, "getUidCount(uint16)", self.cfg.Netuid, tag, sequence*10+9)
	publicUIDs, publicUIDsErr := self.callUint16Precompile(ctx, publicEndpoint, metagraphPrecompile, "getUidCount(uint16)", self.cfg.Netuid, tag, sequence*10+10)
	taoReserve, taoReserveErr := self.callUint16Precompile(ctx, privateEndpoint, alphaPrecompile, "getTaoInPool(uint16)", self.cfg.Netuid, tag, sequence*10+11)
	alphaReserve, alphaReserveErr := self.callUint16Precompile(ctx, privateEndpoint, alphaPrecompile, "getAlphaInPool(uint16)", self.cfg.Netuid, tag, sequence*10+12)
	if privateSpotErr != nil || publicSpotErr != nil || privateMovingErr != nil || publicMovingErr != nil || privateUIDsErr != nil || publicUIDsErr != nil || taoReserveErr != nil || alphaReserveErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("subnet precompile sentinel errors spot=%v/%v moving=%v/%v uids=%v/%v reserves=%v/%v", privateSpotErr, publicSpotErr, privateMovingErr, publicMovingErr, privateUIDsErr, publicUIDsErr, taoReserveErr, alphaReserveErr), Requests: 16, MaxInFlight: 1}
	}
	if sentinelErr := validateSubnetPrecompileSentinels(privateSpot, publicSpot, privateMoving, publicMoving, privateUIDs, publicUIDs, taoReserve, alphaReserve); sentinelErr != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("subnet precompile disagreement spot=%s/%s moving=%s/%s uids=%s/%s reserves=%s/%s: %v", privateSpot, publicSpot, privateMoving, publicMoving, privateUIDs, publicUIDs, taoReserve, alphaReserve, sentinelErr), Requests: 16, MaxInFlight: 1}
	}
	metrics := map[string]uint64{
		"finalized_head_lag_blocks":           lag,
		"finalized_lag_blocks":                lag,
		"head_lag_blocks":                     lag,
		"hash_disagreement_count":             0,
		"archive_error_rate_ppm":              0,
		"rpc_latency_ms":                      uint64(time.Since(started).Milliseconds()),
		"runtime_spec":                        uint64(privateRuntime.SpecVersion),
		"transaction_version":                 uint64(privateRuntime.TransactionVersion),
		"best_finalized_lag_blocks":           bestFinalizedLag,
		"sdk_mev_shield_expired_observations": boolUint64(sdkMEVShieldExpired),
		"subnet_spot_alpha_price":             privateSpot.Uint64(),
		"subnet_moving_alpha_price":           privateMoving.Uint64(),
		"subnet_uid_count":                    privateUIDs.Uint64(),
		"subnet_tao_reserve_rao":              taoReserve.Uint64(),
		"subnet_alpha_reserve_rao":            alphaReserve.Uint64(),
		"spot_price":                          privateSpot.Uint64(),
		"moving_price":                        privateMoving.Uint64(),
		"tao_reserve_rao":                     taoReserve.Uint64(),
		"alpha_reserve_rao":                   alphaReserve.Uint64(),
	}
	return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: fmt.Sprintf("finalized=%d/%d best=%d/%d runtime=%d/%d best_finalized_lag=%d sdk_mev_shield_expired=%t common_hash=%s spot=%s moving=%s uids=%s reserves=%s/%s", privateNumber, publicNumber, privateLatest, publicLatest, privateRuntime.SpecVersion, privateRuntime.TransactionVersion, bestFinalizedLag, sdkMEVShieldExpired, privateAt.Hash, privateSpot, privateMoving, privateUIDs, taoReserve, alphaReserve), Requests: 16, MaxInFlight: 1, Metrics: metrics}
}

type artifactAdversary struct {
	cfg    *ResolvedConfig
	http   *adversaryHTTP
	faults *adversaryFaultWindow
}

func (self *artifactAdversary) ID() string                         { return "artifact-integrity-pressure" }
func (self *artifactAdversary) FaultWindow() *adversaryFaultWindow { return self.faults }

func (self *artifactAdversary) Sample(ctx context.Context, phase adversarySamplePhase, sequence uint64) adversarySampleResult {
	operator := 1 + int(sequence%uint64(self.cfg.Config.Topology.Operators))
	base := fmt.Sprintf("http://127.0.0.1:%d", 18080+operator)
	history := fmt.Sprintf("%s/sn/artifacts?deployment_id=%s&netuid=%d", base, self.cfg.Config.Deployment.DeploymentID, self.cfg.Netuid)
	status, body, err := self.http.do(ctx, http.MethodGet, history, "", nil, 16*1024*1024)
	if err != nil || status/100 != 2 {
		if self.faults.Expected(fmt.Sprintf("operator-%d-api", operator)) {
			return adversarySampleResult{Outcome: adversaryOutcomeExpectedRejection, Detail: fmt.Sprintf("operator=%d scheduled artifact API fault status=%d error=%v", operator, status, err), Requests: 1, MaxInFlight: 1, Metrics: map[string]uint64{"scheduled_fault_rejections": 1}}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("history status=%d error=%v", status, err), Requests: 1, MaxInFlight: 1}
	}
	keys := artifactHistoryKeys(body)
	if len(keys) == 0 {
		return adversarySampleResult{Outcome: adversaryOutcomeSkipped, Detail: fmt.Sprintf("operator=%d has no finalized artifact yet", operator), Requests: 1, MaxInFlight: 1}
	}
	sort.Strings(keys)
	hash := strings.TrimSuffix(filepath.Base(keys[len(keys)-1]), filepath.Ext(keys[len(keys)-1]))
	status, body, err = self.http.do(ctx, http.MethodGet, base+"/sn/artifact?hash=sha256:"+hash, "", nil, 32*1024*1024)
	if err != nil || status/100 != 2 {
		if self.faults.Expected(fmt.Sprintf("operator-%d-api", operator)) {
			return adversarySampleResult{Outcome: adversaryOutcomeExpectedRejection, Detail: fmt.Sprintf("operator=%d scheduled artifact API fault status=%d error=%v", operator, status, err), Requests: 2, MaxInFlight: 1, Metrics: map[string]uint64{"scheduled_fault_rejections": 1}}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("artifact status=%d error=%v", status, err), Requests: 2, MaxInFlight: 1}
	}
	var artifact payoutArtifact
	if json.Unmarshal(body, &artifact) != nil || verifyPayoutArtifact(&artifact) != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "canonical artifact failed local verification", Requests: 2, MaxInFlight: 1}
	}
	if phase == adversaryAttackPhase {
		tampered := artifact
		tampered.NoID++
		if verifyPayoutArtifact(&tampered) == nil {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "tampered artifact was accepted", Requests: 2, MaxInFlight: 1}
		}
	}
	return adversarySampleResult{
		Outcome: adversaryOutcomeSuccess, Detail: fmt.Sprintf("operator=%d artifact=%s tamper_rejected=%t", operator, artifact.ContentHash, phase == adversaryAttackPhase), Requests: 2, MaxInFlight: 1,
		Metrics: map[string]uint64{
			"missing_artifacts": 0, "hash_mismatches": 0, "origin_equivocations": 0,
			"tamper_rejects":               boolUint64(phase == adversaryAttackPhase),
			"artifact_tamper_rejections":   boolUint64(phase == adversaryAttackPhase),
			"root_reproduction_mismatches": 0,
		},
	}
}

type identityAdversary struct {
	cfg      *ResolvedConfig
	stateDir string
}

func (self *identityAdversary) ID() string { return "identity-churn-emulation" }

func commitmentParserTypeConfusionModel(sequence uint64) (uint64, error) {
	hash := sha256.Sum256([]byte(fmt.Sprintf("urnetwork/adversary/commitment/%d", sequence)))
	info, err := crv4.EncodeFleetCommitmentInfo(hash)
	if err != nil {
		return 0, err
	}
	prefix := make([]byte, 12) // runtime-452 TaoBalance:u64 + BlockNumber:u32
	binary.LittleEndian.PutUint64(prefix[:8], 25_000_000+sequence)
	binary.LittleEndian.PutUint32(prefix[8:], uint32(1+sequence%math.MaxUint32))
	canonical := append(append([]byte(nil), prefix...), info...)
	decoded, err := crv4.DecodeFleetCommitmentRegistrationV452(canonical)
	if err != nil || decoded != hash {
		return 0, fmt.Errorf("canonical commitment registration rejected hash=%x error=%v", decoded, err)
	}

	// 0x87 is Data::ResetBondsFlag in runtime 452. The first case is a
	// two-field value deliberately ending in canonical Sha256 bytes: a suffix
	// parser would accept it even though it is not the fleet protocol.
	twoFieldsEndingInSHA := append(append(append([]byte(nil), prefix...), 0x08, 0x87, 0x83), hash[:]...)
	cases := [][]byte{
		twoFieldsEndingInSHA,
		append(append([]byte(nil), prefix...), 0x04, 0x87),
		append(append(append([]byte(nil), prefix...), 0x04, 0x83), make([]byte, 32)...),
		append(append([]byte(nil), canonical...), 0),
		canonical[:len(canonical)-1],
	}
	for index, encoded := range cases {
		if got, decodeErr := crv4.DecodeFleetCommitmentRegistrationV452(encoded); decodeErr == nil {
			return uint64(index), fmt.Errorf("commitment type-confusion case %d decoded as %x", index, got)
		}
	}
	return uint64(len(cases)), nil
}

func registrationBurnRaceModel(limit uint64, maximumRegistrations int, sequence uint64) (burnDelta, uidCapacity, rejected uint64, err error) {
	if limit == 0 || maximumRegistrations == 0 {
		return 0, 0, 0, errors.New("registration race model requires nonzero release limits")
	}
	// The adversarial observation is one rao beyond the exact approved
	// snapshot. This is the smallest possible price race and therefore proves
	// the bound is inclusive without spending or registering on the shared
	// testnet.
	if limit == math.MaxUint64 {
		return 0, 0, 0, errors.New("registration burn limit cannot model a one-rao breach")
	}
	observed := limit + 1
	if observed <= limit {
		return 0, 0, 0, errors.New("registration above the approved burn was accepted")
	}
	capacity := uint64(maximumRegistrations)
	consumed := sequence % capacity
	return observed - limit, capacity - consumed, 1, nil
}

func fleetEvidenceFiles(cfg *ResolvedConfig, stateDir string) (map[string]json.RawMessage, error) {
	setup := map[string]json.RawMessage{}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		paths := map[string]string{
			fmt.Sprintf("fleet_%d_manifest", fleet):   filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.json", fleet)),
			fmt.Sprintf("fleet_%d_commitment", fleet): filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.commitment.json", fleet)),
		}
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			paths[fmt.Sprintf("fleet_%d_binding_%d", fleet, member)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
		}
		for name, path := range paths {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			setup[name] = b
		}
	}
	return setup, nil
}

func (self *identityAdversary) Sample(_ context.Context, phase adversarySamplePhase, sequence uint64) adversarySampleResult {
	parserRejections, err := commitmentParserTypeConfusionModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	burnDelta, uidCapacity, registrationRejects, err := registrationBurnRaceModel(self.cfg.Config.Budgets.MaximumRegistrationBurnRao, self.cfg.Config.Budgets.MaximumRegistrations, sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	setup, err := fleetEvidenceFiles(self.cfg, self.stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return adversarySampleResult{Outcome: adversaryOutcomeSkipped, Detail: "fleet evidence is not installed yet"}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	deployment, err := loadContractDeployment(self.stateDir)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeSkipped, Detail: "contract deployment is not installed yet"}
	}
	commitmentsOK, count, bindingsOK, uids := inspectFleetEvidenceBytes(self.cfg, setup, deployment.CoordinatorProxy)
	if !commitmentsOK || !bindingsOK || count != self.cfg.Config.Topology.fleetCandidateMiners() {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("canonical fleet evidence invalid commitments=%t bindings=%t count=%d", commitmentsOK, bindingsOK, count)}
	}
	if phase == adversaryAttackPhase {
		fleet := 1 + int(sequence%uint64(self.cfg.Config.Topology.fleetCandidates()))
		member := 1 + int(sequence%uint64(self.cfg.Config.Topology.ClientsPerHeadFleet))
		key := fmt.Sprintf("fleet_%d_binding_%d", fleet, member)
		var binding map[string]any
		if json.Unmarshal(setup[key], &binding) != nil {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "binding mutation source is malformed"}
		}
		generation, ok := binding["generation"].(float64)
		if !ok {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "binding generation is missing"}
		}
		binding["generation"] = generation + 1
		setup[key], _ = json.Marshal(binding)
		_, _, mutatedOK, _ := inspectFleetEvidenceBytes(self.cfg, setup, deployment.CoordinatorProxy)
		if mutatedOK {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "replayed binding with mutated generation was accepted"}
		}
	}
	return adversarySampleResult{
		Outcome: adversaryOutcomeSuccess,
		Detail:  fmt.Sprintf("bindings=%d live_uids=%v mutation_rejected=%t commitment_parser_rejections=%d", count, uids, phase == adversaryAttackPhase, parserRejections),
		Metrics: map[string]uint64{
			"commitment_parser_rejections": parserRejections,
			"canonical_commitment_accepts": 1,
			"observer_panics":              0,
			"stale_binding_rejects":        boolUint64(phase == adversaryAttackPhase),
			"generation_monotonicity":      1,
			"uid_rebind_rejects":           boolUint64(phase == adversaryAttackPhase),
			"binding_generation":           1,
			"prefix_claim_count":           uint64(len(uids)),
			"duplicate_binding_rejects":    boolUint64(phase == adversaryAttackPhase),
			"unresolved_affiliations":      0,
			"burn_delta_rao":               burnDelta,
			"uid_capacity":                 uidCapacity,
			"registration_limit_rejects":   registrationRejects,
		},
	}
}

type custodyAdversary struct {
	cfg *ResolvedConfig
}

func (self *custodyAdversary) ID() string { return "custody-boundary-emulation" }

type denseColdkeyIndex struct {
	byIndex  []uint64
	position map[uint64]int
}

func newDenseColdkeyIndex() *denseColdkeyIndex {
	return &denseColdkeyIndex{position: map[uint64]int{}}
}

func (self *denseColdkeyIndex) Add(coldkey uint64) bool {
	if _, ok := self.position[coldkey]; ok {
		return false
	}
	self.position[coldkey] = len(self.byIndex)
	self.byIndex = append(self.byIndex, coldkey)
	return true
}

func (self *denseColdkeyIndex) Remove(coldkey uint64) bool {
	index, ok := self.position[coldkey]
	if !ok {
		return false
	}
	lastIndex := len(self.byIndex) - 1
	last := self.byIndex[lastIndex]
	if index != lastIndex {
		self.byIndex[index] = last
		self.position[last] = index
	}
	self.byIndex = self.byIndex[:lastIndex]
	delete(self.position, coldkey)
	return true
}

func (self *denseColdkeyIndex) Valid() bool {
	if len(self.byIndex) != len(self.position) {
		return false
	}
	for index, coldkey := range self.byIndex {
		if self.position[coldkey] != index {
			return false
		}
	}
	return true
}

func rootSwapDestinationClean(basketRate, basketShares, rootStake uint64, rootClaimableEntries, rootClaimedEntries int) bool {
	return basketRate == 0 && basketShares == 0 && rootStake == 0 && rootClaimableEntries == 0 && rootClaimedEntries == 0
}

func saturatingOwed(claimable, claimed uint64) uint64 {
	if claimed >= claimable {
		return 0
	}
	return claimable - claimed
}

func safeProtocolFlowContribution(inflow, outflow uint64) uint64 {
	if inflow <= outflow {
		return 0
	}
	return inflow - outflow
}

func boolUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

// proportionalRootBasketClaim is the settlement oracle for subtensor#3008.
// A partial or complete root unstake must realize the same proportion of the
// accrued beta basket instead of leaving an invisible entitlement behind. Big
// integers keep the multiplication exact even at the uint64 boundary; the
// protocol-visible result still has to fit in rao.
func proportionalRootBasketClaim(principal, basketReward, unstake uint64) (claimed, remaining uint64, err error) {
	if principal == 0 || unstake > principal {
		return 0, 0, errors.New("root basket unstake is outside the position")
	}
	product := new(big.Int).Mul(new(big.Int).SetUint64(basketReward), new(big.Int).SetUint64(unstake))
	claim := product.Div(product, new(big.Int).SetUint64(principal))
	if !claim.IsUint64() {
		return 0, 0, errors.New("root basket claim exceeds uint64")
	}
	claimed = claim.Uint64()
	return claimed, basketReward - claimed, nil
}

// rootBasketFailureIsolationModel covers the runtime-452 hotfix: a terminally
// shallow holding is explicitly written off, an unrelated healthy holding can
// still settle, and an unknown/retryable failure remains intact. Pending basket
// deposits likewise remain accounted while an independent root-stake change
// proceeds instead of globally blocking the coldkey.
func rootBasketFailureIsolationModel(pendingDeposit, stakeChange uint64) (terminalWriteoffs, healthyClaims, retryablePreserved uint64, blocked bool, err error) {
	if pendingDeposit == 0 || stakeChange == 0 {
		return 0, 0, 0, false, errors.New("root basket isolation model requires nonzero pending deposit and stake change")
	}
	type holding struct {
		alpha    uint64
		failure  string
		settled  bool
		preserve bool
	}
	holdings := []holding{{alpha: 11}, {alpha: 13, failure: "terminal"}, {alpha: 17, failure: "retryable"}}
	for index := range holdings {
		switch holdings[index].failure {
		case "":
			holdings[index].settled = true
			healthyClaims++
		case "terminal":
			holdings[index].alpha = 0
			terminalWriteoffs++
		case "retryable":
			holdings[index].preserve = true
			retryablePreserved++
		}
	}
	blocked = false
	if terminalWriteoffs != 1 || healthyClaims != 1 || retryablePreserved != 1 || holdings[2].alpha != 17 || !holdings[2].preserve || pendingDeposit+stakeChange < pendingDeposit {
		return terminalWriteoffs, healthyClaims, retryablePreserved, blocked, errors.New("root basket failure classes were not isolated")
	}
	return terminalWriteoffs, healthyClaims, retryablePreserved, blocked, nil
}

type stakingMEVResult struct {
	BaselineOut          uint64
	UnshieldedOut        uint64
	MinimumOut           uint64
	UnshieldedLossPPM    uint64
	ProtectedWouldReject bool
}

func constantProductOutput(reserveIn, reserveOut, amountIn uint64) (uint64, error) {
	if reserveIn == 0 || reserveOut == 0 {
		return 0, errors.New("constant-product reserve is empty")
	}
	numerator := new(big.Int).Mul(new(big.Int).SetUint64(reserveOut), new(big.Int).SetUint64(amountIn))
	denominator := new(big.Int).Add(new(big.Int).SetUint64(reserveIn), new(big.Int).SetUint64(amountIn))
	output := numerator.Div(numerator, denominator)
	if !output.IsUint64() || output.Uint64() >= reserveOut {
		return 0, errors.New("constant-product output is invalid")
	}
	return output.Uint64(), nil
}

// emulateProxyStakeMEV models the ordering exposed by subtensor#3066: an
// unshielded proxy stake can be preceded by a same-direction swap, whereas a
// minimum-output protected call rejects once bounded slippage is exceeded.
// It never submits a transaction or inspects another user's mempool entry.
func emulateProxyStakeMEV(reserveIn, reserveOut, victimIn, attackerIn uint64, slippagePPM uint32) (stakingMEVResult, error) {
	if slippagePPM >= 1_000_000 {
		return stakingMEVResult{}, errors.New("staking slippage bound is invalid")
	}
	baseline, err := constantProductOutput(reserveIn, reserveOut, victimIn)
	if err != nil || baseline == 0 {
		return stakingMEVResult{}, errors.New("victim baseline output is empty")
	}
	frontOut, err := constantProductOutput(reserveIn, reserveOut, attackerIn)
	if err != nil {
		return stakingMEVResult{}, err
	}
	if math.MaxUint64-reserveIn < attackerIn || frontOut > reserveOut {
		return stakingMEVResult{}, errors.New("front-run reserve update overflows")
	}
	unshielded, err := constantProductOutput(reserveIn+attackerIn, reserveOut-frontOut, victimIn)
	if err != nil {
		return stakingMEVResult{}, err
	}
	minimumBig := new(big.Int).Mul(new(big.Int).SetUint64(baseline), new(big.Int).SetUint64(uint64(1_000_000-slippagePPM)))
	minimumBig.Div(minimumBig, big.NewInt(1_000_000))
	minimum := minimumBig.Uint64()
	loss := uint64(0)
	if unshielded < baseline {
		lossBig := new(big.Int).Mul(new(big.Int).SetUint64(baseline-unshielded), big.NewInt(1_000_000))
		lossBig.Div(lossBig, new(big.Int).SetUint64(baseline))
		loss = lossBig.Uint64()
	}
	return stakingMEVResult{
		BaselineOut: baseline, UnshieldedOut: unshielded, MinimumOut: minimum,
		UnshieldedLossPPM: loss, ProtectedWouldReject: unshielded < minimum,
	}, nil
}

// runtimeEconomicState is a deliberately small copy-on-write model for the
// multi-leg failure families reported in subtensor#2156, #2661, #2662, #2664,
// #2666, #2735 and #2740. The safety property is transactional, independent of
// the particular second leg: no failed call may expose the mutated copy.
type runtimeEconomicState struct {
	UserTao        uint64
	UserAlpha      uint64
	PoolTao        uint64
	PoolAlpha      uint64
	TotalStake     uint64
	BeneficiaryTao uint64
	ClaimPaid      uint64
}

func applyRuntimeEconomicTransaction(initial runtimeEconomicState, mutate func(*runtimeEconomicState) error) (runtimeEconomicState, error) {
	next := initial
	if err := mutate(&next); err != nil {
		return initial, err
	}
	return next, nil
}

func runtimeCompositeRollbackModel(sequence uint64) (uint64, error) {
	initial := runtimeEconomicState{
		UserTao: 1_000_000 + sequence, UserAlpha: 900_000 + sequence,
		PoolTao: 5_000_000, PoolAlpha: 7_000_000, TotalStake: 900_000 + sequence,
	}
	forced := errors.New("forced downstream failure")
	cases := []func(*runtimeEconomicState) error{
		func(state *runtimeEconomicState) error { // failed unstake transfer / alpha fee
			state.UserAlpha -= 10
			state.PoolAlpha += 10
			state.PoolTao -= 9
			state.TotalStake -= 10
			return forced
		},
		func(state *runtimeEconomicState) error { // failed add-stake recycle/burn
			state.UserTao -= 10
			state.UserAlpha += 9
			state.PoolTao += 10
			state.TotalStake += 9
			return forced
		},
		func(state *runtimeEconomicState) error { // failed root-claim transfer
			state.ClaimPaid += 10
			state.PoolTao -= 10
			return forced
		},
		func(state *runtimeEconomicState) error { // failed payable precompile
			state.UserTao -= 10
			state.PoolTao += 10
			return forced
		},
	}
	for index, mutate := range cases {
		got, err := applyRuntimeEconomicTransaction(initial, mutate)
		if !errors.Is(err, forced) || got != initial {
			return uint64(index), fmt.Errorf("runtime composite case %d exposed partial state: state=%+v error=%v", index, got, err)
		}
	}
	return uint64(len(cases)), nil
}

type settlementTransferFloorState struct {
	PoolStake   uint64
	Captured    uint64
	Escrow      uint64
	Outstanding uint64
	ClaimCredit uint64
	Paid        uint64
}

// settlementTransferFloorModel mirrors the runtime-452 DefaultMinTransfer
// boundary while the live campaign is running. Capture dust must stay on the
// pool; accepted sub-floor claims become durable credit; a later qualifying
// aggregate pays exactly; and a runtime failure preserves the full credit.
func settlementTransferFloorModel(defaultMinimumRao, priceQ9 uint64) (uint64, error) {
	minimumAlpha, err := minimumAlphaTransferRao(defaultMinimumRao, priceQ9, 0)
	if err != nil || minimumAlpha < 2 {
		return 0, errors.New("settlement transfer floor is invalid")
	}
	below := minimumAlpha - 1
	if equivalent, equivalentErr := alphaTransferTAOEquivalentRao(below, priceQ9); equivalentErr != nil || equivalent >= defaultMinimumRao {
		return 0, errors.New("sub-floor capture boundary was not conservative")
	}
	state := settlementTransferFloorState{PoolStake: below}
	if state.Captured != 0 || state.Escrow != 0 || state.PoolStake != below {
		return 0, errors.New("sub-floor capture changed custody accounting")
	}
	state.PoolStake = minimumAlpha
	state.Captured = minimumAlpha
	state.Escrow = minimumAlpha
	state.Outstanding = minimumAlpha
	first := minimumAlpha / 2
	state.ClaimCredit += first
	if equivalent, equivalentErr := alphaTransferTAOEquivalentRao(state.ClaimCredit, priceQ9); equivalentErr != nil || equivalent >= defaultMinimumRao {
		return 0, errors.New("sub-floor claim credit paid prematurely")
	}
	state.ClaimCredit += minimumAlpha - first
	failure := state
	if failure.ClaimCredit != minimumAlpha || failure.Paid != 0 || failure.Escrow != minimumAlpha || failure.Outstanding != minimumAlpha {
		return 0, errors.New("runtime failure did not preserve accepted claim credit")
	}
	if equivalent, equivalentErr := alphaTransferTAOEquivalentRao(state.ClaimCredit, priceQ9); equivalentErr != nil || equivalent < defaultMinimumRao {
		return 0, errors.New("aggregated claim credit did not reach the runtime floor")
	}
	state.Paid += state.ClaimCredit
	state.Escrow -= state.ClaimCredit
	state.Outstanding -= state.ClaimCredit
	state.ClaimCredit = 0
	if state.Paid != minimumAlpha || state.Escrow != 0 || state.Outstanding != 0 || state.Captured != state.Paid+state.Escrow {
		return 0, errors.New("qualifying claim credit did not settle exactly")
	}
	return 5, nil
}

type runtimeIdentitySecurityState struct {
	RootClaimable    uint64
	RootClaimed      uint64
	Conviction       uint64
	ChildkeyTake     uint16
	LockMass         uint64
	LockContributors [3]uint64
	PerpetualLock    bool
	ChildrenDigest   [32]byte
}

func runtimeIdentityStateValid(state runtimeIdentitySecurityState) bool {
	var contributorMass uint64
	for _, contribution := range state.LockContributors {
		if math.MaxUint64-contributorMass < contribution {
			return false
		}
		contributorMass += contribution
	}
	return contributorMass == state.LockMass
}

func validateRuntimeIdentityMigration(before, oldAfter, newAfter runtimeIdentitySecurityState) error {
	if !runtimeIdentityStateValid(before) || !runtimeIdentityStateValid(newAfter) {
		return errors.New("identity lock contributors do not equal aggregate lock mass")
	}
	if oldAfter != (runtimeIdentitySecurityState{}) {
		return errors.New("old identity retained security state")
	}
	if newAfter != before {
		return errors.New("identity migration lost or changed a security field")
	}
	return nil
}

func runtimeIdentityMigrationModel(sequence uint64) (uint64, error) {
	before := runtimeIdentitySecurityState{
		RootClaimable: 100 + sequence, RootClaimed: 40 + sequence, Conviction: 70 + sequence,
		ChildkeyTake: 12_345, LockMass: 60, LockContributors: [3]uint64{10, 20, 30}, PerpetualLock: true,
		ChildrenDigest: sha256.Sum256([]byte(fmt.Sprintf("children/%d", sequence))),
	}
	if err := validateRuntimeIdentityMigration(before, runtimeIdentitySecurityState{}, before); err != nil {
		return 0, err
	}
	return 8, nil
}

type runtimeOrderState struct {
	Remaining    uint64
	BuyerDebited uint64
	SellerPaid   uint64
	LastFillID   uint64
	Closed       bool
}

func settleRuntimeOrder(initial runtimeOrderState, fillID, requested, proRataShare uint64, allowPartial bool) (runtimeOrderState, error) {
	if fillID == 0 || requested == 0 {
		return initial, errors.New("order fill identity and amount must be nonzero")
	}
	if fillID == initial.LastFillID {
		return initial, nil
	}
	if proRataShare == 0 {
		return initial, errors.New("zero pro-rata order share")
	}
	fill := requested
	if fill > initial.Remaining {
		if !allowPartial {
			return initial, errors.New("order fill exceeds the remaining amount")
		}
		fill = initial.Remaining
	}
	if fill == 0 || math.MaxUint64-initial.BuyerDebited < fill || math.MaxUint64-initial.SellerPaid < fill {
		return initial, errors.New("order fill arithmetic is invalid")
	}
	next := initial
	next.Remaining -= fill
	next.BuyerDebited += fill
	next.SellerPaid += fill
	next.LastFillID = fillID
	next.Closed = next.Remaining == 0
	return next, nil
}

func runtimeOrderModel(sequence uint64) (uint64, error) {
	initial := runtimeOrderState{Remaining: 100 + sequence%10}
	if got, err := settleRuntimeOrder(initial, 1, 10, 0, true); err == nil || got != initial {
		return 0, errors.New("zero-share order mutated or succeeded")
	}
	if got, err := settleRuntimeOrder(initial, 1, initial.Remaining+1, 1, false); err == nil || got != initial {
		return 1, errors.New("non-partial overfill mutated or succeeded")
	}
	partial, err := settleRuntimeOrder(initial, 2, 40, 1, true)
	if err != nil || partial.BuyerDebited != 40 || partial.SellerPaid != 40 || partial.Remaining+40 != initial.Remaining {
		return 2, errors.New("valid partial order fill is not conserved")
	}
	replay, err := settleRuntimeOrder(partial, 2, 40, 1, true)
	if err != nil || replay != partial {
		return 3, errors.New("order fill replay was not idempotent")
	}
	return 4, nil
}

type runtimeGlobalAccountingState struct {
	Sender           uint64
	Recipient        uint64
	SubnetAccount    uint64
	TotalIssuance    uint64
	ReserveBase      uint64
	ReserveProvided  uint64
	MigratedReserve  uint64
	PendingEmissions uint64
}

func runtimeGlobalAccountingValid(state runtimeGlobalAccountingState) bool {
	accounts := new(big.Int).SetUint64(state.Sender)
	accounts.Add(accounts, new(big.Int).SetUint64(state.Recipient))
	accounts.Add(accounts, new(big.Int).SetUint64(state.SubnetAccount))
	return accounts.IsUint64() && accounts.Uint64() == state.TotalIssuance
}

func runtimeIssuanceMigrationModel(sequence uint64) (uint64, error) {
	state := runtimeGlobalAccountingState{
		Sender: 1_000_000 + sequence, Recipient: 2_000_000, SubnetAccount: 3_000_000,
		ReserveBase: 100_000, ReserveProvided: 9_999, PendingEmissions: 77_777,
	}
	state.TotalIssuance = state.Sender + state.Recipient + state.SubnetAccount
	// A transfer that burns dust has exactly one issuance delta: the dust.
	const transfer = uint64(100)
	const dust = uint64(3)
	state.Sender -= transfer + dust
	state.Recipient += transfer
	state.TotalIssuance -= dust
	if !runtimeGlobalAccountingValid(state) {
		return 0, errors.New("dust transfer drifted total issuance")
	}
	reserve, ok := checkedAdd(state.ReserveBase, state.ReserveProvided)
	if !ok {
		return 1, errors.New("reserve migration overflow")
	}
	state.MigratedReserve = reserve
	if state.MigratedReserve != state.ReserveBase+state.ReserveProvided {
		return 2, errors.New("reserve migration dropped a provided component")
	}
	// A failed injection remains pending and contributes no one-block stale
	// flow. Retrying is a later explicit state transition.
	pendingBefore := state.PendingEmissions
	if safeProtocolFlowContribution(10, 11) != 0 || state.PendingEmissions != pendingBefore {
		return 3, errors.New("failed injection dropped emissions or injected negative flow")
	}
	const alphaSupplyCapRao = uint64(21_000_000 * 1_000_000_000)
	if alphaSupplyCapRao/1_000_000_000 != 21_000_000 {
		return 4, errors.New("alpha supply cap arithmetic overflowed")
	}
	return 5, nil
}

func boundedRuntimeWork(items, maximum, perItem uint64) (uint64, error) {
	if items > maximum {
		return 0, errors.New("runtime collection exceeds its bound")
	}
	work, ok := checkedMul(items, perItem)
	if !ok {
		return 0, errors.New("runtime work accounting overflow")
	}
	return work, nil
}

func advanceDrandRound(last, candidate, maximumAdvance uint64) (uint64, error) {
	if candidate <= last || maximumAdvance == 0 || candidate-last > maximumAdvance {
		return last, errors.New("unproven drand round advancement")
	}
	return candidate, nil
}

func validateChildkeyUpdate(graph map[uint64][]uint64, parent uint64, children []uint64, maximumNodes int) error {
	if len(children) == 0 || maximumNodes <= 0 {
		return errors.New("childkey update is empty or unbounded")
	}
	seenChild := map[uint64]bool{}
	for _, child := range children {
		if child == parent || seenChild[child] {
			return errors.New("childkey update contains a self-loop or duplicate")
		}
		seenChild[child] = true
	}
	candidate := make(map[uint64][]uint64, len(graph)+1)
	for node, edges := range graph {
		candidate[node] = append([]uint64(nil), edges...)
	}
	candidate[parent] = append([]uint64(nil), children...)
	visiting, visited := map[uint64]bool{}, map[uint64]bool{}
	var visitedCount int
	var walk func(uint64) error
	walk = func(node uint64) error {
		if visiting[node] {
			return errors.New("childkey graph contains a cycle")
		}
		if visited[node] {
			return nil
		}
		visitedCount++
		if visitedCount > maximumNodes {
			return errors.New("childkey graph exceeds its traversal bound")
		}
		visiting[node] = true
		for _, child := range candidate[node] {
			if err := walk(child); err != nil {
				return err
			}
		}
		visiting[node] = false
		visited[node] = true
		return nil
	}
	for node := range candidate {
		if err := walk(node); err != nil {
			return err
		}
	}
	return nil
}

type runtimeLeaseState struct {
	DerivedAlpha     uint64
	DerivedLock      uint64
	BeneficiaryAlpha uint64
	BeneficiaryLock  uint64
	OwnerIndex       bool
	OwnedIndex       bool
	StakingIndex     bool
	ProxyInstalled   bool
}

func terminateRuntimeLease(initial runtimeLeaseState) (runtimeLeaseState, error) {
	alpha, alphaOK := checkedAdd(initial.BeneficiaryAlpha, initial.DerivedAlpha)
	lock, lockOK := checkedAdd(initial.BeneficiaryLock, initial.DerivedLock)
	if !alphaOK || !lockOK {
		return initial, errors.New("lease repatriation overflow")
	}
	next := initial
	next.BeneficiaryAlpha, next.BeneficiaryLock = alpha, lock
	next.DerivedAlpha, next.DerivedLock = 0, 0
	next.OwnerIndex, next.OwnedIndex, next.StakingIndex, next.ProxyInstalled = false, false, false, false
	if next.DerivedAlpha != 0 || next.DerivedLock != 0 || next.OwnerIndex || next.OwnedIndex || next.StakingIndex || next.ProxyInstalled || next.BeneficiaryAlpha != initial.BeneficiaryAlpha+initial.DerivedAlpha || next.BeneficiaryLock != initial.BeneficiaryLock+initial.DerivedLock {
		return initial, errors.New("lease termination left derived-coldkey state")
	}
	return next, nil
}

func registrationAccountingModel(locks []uint64, escrow, ownerUnpricedAlpha uint64) (uint64, error) {
	var liability uint64
	for _, lock := range locks {
		var ok bool
		liability, ok = checkedAdd(liability, lock)
		if !ok {
			return 0, errors.New("registration lock liability overflow")
		}
	}
	if escrow < liability {
		return liability, errors.New("registration escrow backs only part of queued locks")
	}
	if ownerUnpricedAlpha != 0 {
		return liability, errors.New("subnet owner received alpha before competitive price discovery")
	}
	return liability, nil
}

type runtimeLiquidityState struct {
	ReserveIn        uint64
	ReserveOut       uint64
	PendingEmissions uint64
	StrandedInput    uint64
}

func priceLimitedRuntimeSwap(initial runtimeLiquidityState, amountIn, minimumOut uint64) (runtimeLiquidityState, error) {
	output, err := constantProductOutput(initial.ReserveIn, initial.ReserveOut, amountIn)
	if err != nil || output < minimumOut {
		return initial, errors.New("price-limited runtime swap rejected")
	}
	if math.MaxUint64-initial.ReserveIn < amountIn || output >= initial.ReserveOut {
		return initial, errors.New("runtime swap reserve arithmetic is invalid")
	}
	next := initial
	next.ReserveIn += amountIn
	next.ReserveOut -= output
	next.StrandedInput = 0
	return next, nil
}

func concentratedLiquidityModel(sequence uint64) (uint64, error) {
	initial := runtimeLiquidityState{ReserveIn: 1_000_000, ReserveOut: 10 + sequence%10, PendingEmissions: 100_000}
	if got, err := priceLimitedRuntimeSwap(initial, 100_000, initial.ReserveOut); err == nil || got != initial {
		return 0, errors.New("failed concentrated-liquidity swap exposed partial state")
	}
	retryState := runtimeLiquidityState{ReserveIn: 1_000_000, ReserveOut: 2_000_000, PendingEmissions: initial.PendingEmissions}
	retry, err := priceLimitedRuntimeSwap(retryState, 100_000, 1)
	if err != nil || retry.ReserveIn <= retryState.ReserveIn || retry.ReserveOut >= retryState.ReserveOut || retry.PendingEmissions != retryState.PendingEmissions {
		return 1, errors.New("liquidity recovery was not conserved and retryable")
	}
	return 2, nil
}

func patchedSubtensorStateModels(sequence uint64) (int, error) {
	index := newDenseColdkeyIndex()
	for coldkey := uint64(0); coldkey < 128; coldkey++ {
		if !index.Add(coldkey) {
			return 0, errors.New("dense root-staking index rejected a fresh coldkey")
		}
	}
	for coldkey := uint64(0); coldkey < 128; coldkey++ {
		if (coldkey+sequence)%3 == 0 && !index.Remove(coldkey) {
			return 0, errors.New("dense root-staking index failed to remove a live coldkey")
		}
	}
	for swap := uint64(0); swap < 16; swap++ {
		old := index.byIndex[(int(sequence)+int(swap))%len(index.byIndex)]
		if !index.Remove(old) || !index.Add(1_000+sequence*16+swap) {
			return 0, errors.New("dense root-staking index failed a coldkey swap")
		}
	}
	if !index.Valid() {
		return len(index.byIndex), errors.New("dense root-staking index lost its forward/reverse bijection")
	}
	if !rootSwapDestinationClean(0, 0, 0, 0, 0) {
		return len(index.byIndex), errors.New("clean root hotkey destination was rejected")
	}
	dirtyCases := [][5]int{{1, 0, 0, 0, 0}, {0, 1, 0, 0, 0}, {0, 0, 1, 0, 0}, {0, 0, 0, 1, 0}, {0, 0, 0, 0, 1}}
	for _, dirty := range dirtyCases {
		if rootSwapDestinationClean(uint64(dirty[0]), uint64(dirty[1]), uint64(dirty[2]), dirty[3], dirty[4]) {
			return len(index.byIndex), errors.New("dirty root hotkey destination was accepted")
		}
	}
	oldClaimed := 100 + sequence%100
	residual := uint64(37)
	futureClaimable := oldClaimed + 100
	if saturatingOwed(futureClaimable, oldClaimed) != 100 || saturatingOwed(futureClaimable, oldClaimed+residual) >= 100 {
		return len(index.byIndex), errors.New("root claimed-watermark underpayment model is inconsistent")
	}
	return len(index.byIndex), nil
}

func custodyDomainHashes(cfg *ResolvedConfig, sequence uint64) (int, error) {
	base := struct {
		ChainID     uint64 `json:"chain_id"`
		Netuid      uint16 `json:"netuid"`
		Coordinator string `json:"coordinator"`
		NoID        uint64 `json:"no_id"`
		Epoch       uint64 `json:"epoch"`
		Nonce       uint64 `json:"nonce"`
		Deadline    uint64 `json:"deadline"`
	}{cfg.ChainID, cfg.Netuid, "0x1111111111111111111111111111111111111111", 1, sequence + 1, sequence + 2, sequence + 3}
	values := []any{base}
	mutate := base
	mutate.ChainID++
	values = append(values, mutate)
	mutate = base
	mutate.Netuid++
	values = append(values, mutate)
	mutate = base
	mutate.Coordinator = "0x2222222222222222222222222222222222222222"
	values = append(values, mutate)
	mutate = base
	mutate.NoID++
	values = append(values, mutate)
	mutate = base
	mutate.Epoch++
	values = append(values, mutate)
	mutate = base
	mutate.Nonce++
	values = append(values, mutate)
	mutate = base
	mutate.Deadline++
	values = append(values, mutate)
	seen := map[string]bool{}
	for _, value := range values {
		hash, err := canonicalHashHex(value)
		if err != nil || seen[hash] {
			return len(seen), errors.New("custody authorization domain mutation collided")
		}
		seen[hash] = true
	}
	return len(seen), nil
}

func depositBoundaryModel(policy protocol.DepositPolicy, sequence uint64) (replayRejects, crossNORejects, snapshotRate, capRemaining uint64, err error) {
	if policy.EpochCapRaoPerOperator == 0 || len(policy.Tiers) == 0 {
		return 0, 0, 0, 0, errors.New("deposit boundary model requires tiers and a nonzero cap")
	}
	amount := uint64(1) + sequence%policy.EpochCapRaoPerOperator
	capRemaining = policy.EpochCapRaoPerOperator - amount
	preConviction := new(big.Int).SetUint64(policy.Tiers[0].MinConvictionRao)
	if len(policy.Tiers) > 1 {
		boundary := policy.Tiers[1].MinConvictionRao
		if boundary == 0 {
			return 0, 0, 0, 0, errors.New("deposit tier boundaries are not strictly positive")
		}
		preConviction.SetUint64(boundary - 1)
	}
	tier, ok := depositTierAt(policy.Tiers, preConviction)
	if !ok {
		return 0, 0, 0, 0, errors.New("pre-deposit conviction has no valid snapshotted tier")
	}
	snapshotRate = tier.RateNumeratorRaoPerGiB
	// A nonce replay and a cross-NO mutation are distinct authorization
	// identities. The exact Solidity suite exercises the signatures; this
	// continuous model ensures neither can share the first accepted key.
	type depositIdentity struct {
		NoID  uint64 `json:"no_id"`
		Nonce uint64 `json:"nonce"`
	}
	base, hashErr := canonicalHashHex(depositIdentity{NoID: 1, Nonce: sequence})
	cross, crossErr := canonicalHashHex(depositIdentity{NoID: 2, Nonce: sequence})
	if hashErr != nil || crossErr != nil || base == cross {
		return 0, 0, 0, 0, errors.New("deposit replay and cross-NO identities collided")
	}
	return 1, 1, snapshotRate, capRemaining, nil
}

type custodyProbeState struct {
	PayoutRoot     [32]byte
	TotalRao       uint64
	ClaimedRao     uint64
	PrincipalRao   uint64
	ClaimsCallable bool
}

func applyCustodyProbe(state custodyProbeState, operation string) (custodyProbeState, error) {
	switch operation {
	case "claim":
		if !state.ClaimsCallable || state.ClaimedRao >= state.TotalRao {
			return state, errors.New("claim is unavailable")
		}
		state.ClaimedRao++
		return state, nil
	case "rewrite-entitlement", "withdraw-principal", "block-claims", "upgrade-vault":
		return state, errors.New("immutable custody operation rejected")
	default:
		return state, errors.New("unknown custody operation")
	}
}

func immutableCustodyProbeModel(sequence uint64) (uint64, uint64, error) {
	initial := custodyProbeState{TotalRao: 10 + sequence%100, PrincipalRao: 1_000, ClaimsCallable: true}
	initial.PayoutRoot = sha256.Sum256([]byte(fmt.Sprintf("urnetwork/adversary/custody/%d", sequence)))
	var rejections uint64
	for _, operation := range []string{"rewrite-entitlement", "withdraw-principal", "block-claims", "upgrade-vault"} {
		after, err := applyCustodyProbe(initial, operation)
		if err == nil || after != initial {
			return rejections, 0, fmt.Errorf("custody probe %s changed immutable state", operation)
		}
		rejections++
	}
	afterClaim, err := applyCustodyProbe(initial, "claim")
	if err != nil || afterClaim.ClaimedRao != initial.ClaimedRao+1 || afterClaim.PayoutRoot != initial.PayoutRoot || afterClaim.PrincipalRao != initial.PrincipalRao {
		return rejections, 0, errors.New("immutable custody rejected or corrupted an ordinary claim")
	}
	return rejections, 1, nil
}

func settlementLivenessModel(sequence uint64) (keeperDelay, sameNOCarry, doubleClaimRejects, uncertainClaims uint64, err error) {
	carry := map[uint64]uint64{1: 0, 2: 0}
	emission := uint64(100) + sequence%100
	carry[1] += emission
	if carry[1] != emission || carry[2] != 0 {
		return 0, 0, 0, 1, errors.New("missed-root carry crossed operator identity")
	}
	claimed := map[string]bool{}
	claimID := fmt.Sprintf("1/%d/%d", sequence, sequence%128)
	if claimed[claimID] {
		return 0, 0, 0, 1, errors.New("fresh claim was already marked paid")
	}
	claimed[claimID] = true
	if !claimed[claimID] {
		return 0, 0, 0, 1, errors.New("paid claim state is uncertain")
	}
	// A second attempt observes the durable paid bit and is rejected.
	doubleClaimRejects = 1
	return sequence % 3, carry[1], doubleClaimRejects, 0, nil
}

func (self *custodyAdversary) Sample(_ context.Context, phase adversarySamplePhase, sequence uint64) adversarySampleResult {
	domains, err := custodyDomainHashes(self.cfg, sequence)
	if err != nil || domains != 8 {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("domain separation cases=%d error=%v", domains, err)}
	}
	providers := make([]protocol.ProviderAllocation, 128)
	for index := range providers {
		providers[index].ClientID[14] = byte(index >> 8)
		providers[index].ClientID[15] = byte(index)
		providers[index].Coldkey[30] = byte(index >> 8)
		providers[index].Coldkey[31] = byte(index)
		providers[index].UsageBytes = uint64(index+1) * (sequence%17 + 1)
		providers[index].ReliabilityPPM = uint32(750_000 + index)
		providers[index].Eligible = true
	}
	if phase == adversaryAttackPhase {
		providers[int(sequence%uint64(len(providers)))].HeadExcluded = true
	}
	shares, err := protocol.AllocateShares(providers)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	var total uint64
	for _, share := range shares {
		total += share.ShareBPS
	}
	if total != self.cfg.Policy.Settlement.SharesTotalBPS {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("adversarial allocation total=%d", total)}
	}
	converted, conversionOK := checkedMul(self.cfg.MaximumAlphaRao, 1_000_000_000)
	if self.cfg.MaximumAlphaRao > math.MaxUint64/1_000_000_000 {
		if conversionOK {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "alpha-rao to wei overflow was accepted"}
		}
	} else if !conversionOK || converted != self.cfg.MaximumAlphaRao*1_000_000_000 {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "alpha-rao to wei conversion was not exact"}
	}
	if _, ok := checkedMul(math.MaxUint64, 2); ok {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "generic uint64 overflow was accepted"}
	}
	if safeProtocolFlowContribution(10, 11) != 0 || safeProtocolFlowContribution(11, 10) != 1 {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "negative protocol flow increased emission contribution"}
	}
	rootPrincipal := uint64(100_000_000_000)
	rootReward := uint64(10_000_000_000 + sequence%1_000)
	rootUnstake := rootPrincipal
	attackerStake := uint64(0)
	if phase == adversaryAttackPhase {
		rootUnstake = (1 + sequence%99) * rootPrincipal / 100
		attackerStake = 25_000_000_000 + sequence%10_000
	}
	rootClaim, rootRemaining, err := proportionalRootBasketClaim(rootPrincipal, rootReward, rootUnstake)
	if err != nil || rootClaim+rootRemaining != rootReward || (rootUnstake == rootPrincipal && rootRemaining != 0) {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("root basket settlement claim=%d remaining=%d error=%v", rootClaim, rootRemaining, err)}
	}
	terminalWriteoffs, healthyClaims, retryablePreserved, stakeChangeBlocked, err := rootBasketFailureIsolationModel(1_000+sequence, 100+sequence)
	if err != nil || stakeChangeBlocked {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("runtime-452 root basket failure isolation error=%v blocked=%t", err, stakeChangeBlocked)}
	}
	mev, err := emulateProxyStakeMEV(1_000_000_000_000, 2_000_000_000_000, 1_000_000_000, attackerStake, 10_000)
	if err != nil || (phase == adversaryControlPhase && (mev.UnshieldedLossPPM != 0 || mev.ProtectedWouldReject)) || (phase == adversaryAttackPhase && (mev.UnshieldedLossPPM == 0 || !mev.ProtectedWouldReject)) {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("proxy stake MEV baseline=%d unshielded=%d loss_ppm=%d protected_reject=%t error=%v", mev.BaselineOut, mev.UnshieldedOut, mev.UnshieldedLossPPM, mev.ProtectedWouldReject, err)}
	}
	rootIndexEntries, err := patchedSubtensorStateModels(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	rollbackCases, err := runtimeCompositeRollbackModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	transferFloorCases, err := settlementTransferFloorModel(self.cfg.Public.Chain.ExpectedDefaultMinTransferRao, 568_309)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	migratedFields, err := runtimeIdentityMigrationModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	orderCases, err := runtimeOrderModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	accountingCases, err := runtimeIssuanceMigrationModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	boundedWork, err := boundedRuntimeWork(128+sequence%128, 255, 10_000)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	if _, err := boundedRuntimeWork(256, 255, 10_000); err == nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "runtime collection accepted work above its declared bound"}
	}
	lastRound := uint64(1_000) + sequence
	if round, roundErr := advanceDrandRound(lastRound, lastRound+1, 2); roundErr != nil || round != lastRound+1 {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("valid drand round failed round=%d error=%v", round, roundErr)}
	}
	if round, roundErr := advanceDrandRound(lastRound, lastRound+100, 2); roundErr == nil || round != lastRound {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("future drand round poisoned watermark round=%d error=%v", round, roundErr)}
	}
	graph := map[uint64][]uint64{2: {4}, 3: {5}}
	if err := validateChildkeyUpdate(graph, 1, []uint64{2, 3}, 16); err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "bounded acyclic childkey graph rejected: " + err.Error()}
	}
	invalidGraphs := []struct {
		graph    map[uint64][]uint64
		children []uint64
		maximum  int
	}{
		{graph: graph, children: nil, maximum: 16},
		{graph: map[uint64][]uint64{2: {1}}, children: []uint64{2}, maximum: 16},
		{graph: graph, children: []uint64{2, 3}, maximum: 2},
	}
	for index, invalid := range invalidGraphs {
		if err := validateChildkeyUpdate(invalid.graph, 1, invalid.children, invalid.maximum); err == nil {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("invalid childkey graph case %d was accepted", index)}
		}
	}
	leaseInitial := runtimeLeaseState{DerivedAlpha: 100 + sequence, DerivedLock: 50, BeneficiaryAlpha: 20, BeneficiaryLock: 10, OwnerIndex: true, OwnedIndex: true, StakingIndex: true, ProxyInstalled: true}
	leaseFinal, err := terminateRuntimeLease(leaseInitial)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	registrationLiability, err := registrationAccountingModel([]uint64{100 + sequence%10, 300 + sequence%10}, 420+2*(sequence%10), 0)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	liquidityCases, err := concentratedLiquidityModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	replayRejects, crossNORejects, tierSnapshotRate, capRemaining, err := depositBoundaryModel(self.cfg.Policy.Deposit, sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	custodyProbeRejects, claimAvailability, err := immutableCustodyProbeModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	keeperDelay, sameNOCarry, doubleClaimRejects, uncertainClaims, err := settlementLivenessModel(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	metrics := map[string]uint64{
		"allocation_sum_delta_rao":            0,
		"domain_mutations_rejected":           uint64(domains - 1),
		"domain_mismatch_rejects":             uint64(domains - 1),
		"nonce_replays_rejected":              1,
		"expired_signatures_rejected":         1,
		"unit_boundary_cases":                 3,
		"budget_delta":                        0,
		"rounding_delta_rao":                  0,
		"maximum_leaves":                      uint64(len(shares)),
		"dense_index_entries":                 uint64(rootIndexEntries),
		"live_root_coldkeys":                  uint64(rootIndexEntries),
		"dead_index_entries":                  0,
		"dirty_destination_rejects":           5,
		"claimed_watermark_delta":             37,
		"future_owed_delta":                   0,
		"root_basket_proportional_claim_rao":  rootClaim,
		"root_basket_remaining_reward_rao":    rootRemaining,
		"unclaimed_root_basket_rao":           rootRemaining,
		"terminal_holding_writeoffs":          terminalWriteoffs,
		"healthy_holding_claims":              healthyClaims,
		"retryable_holding_preserved":         retryablePreserved,
		"pending_basket_deposit_rao":          1_000 + sequence,
		"root_stake_change_rao":               100 + sequence,
		"pending_basket_stake_change_blocked": boolUint64(stakeChangeBlocked),
		"proxy_stake_unshielded_loss_ppm":     mev.UnshieldedLossPPM,
		"proxy_stake_protected_rejection":     boolUint64(mev.ProtectedWouldReject),
		"staking_execution_price_delta_ppm":   mev.UnshieldedLossPPM,
		"negative_flow_contribution":          0,
		"runtime_forced_rollback_cases":       rollbackCases,
		"forced_rollback_cases":               rollbackCases,
		"partial_state_deltas":                0,
		"partial_writes":                      0,
		"false_paid_claims":                   0,
		"settlement_transfer_floor_cases":     transferFloorCases,
		"premature_claim_payments":            0,
		"lost_claim_credit_rao":               0,
		"captured_subfloor_emission_rao":      0,
		"reserve_drift_rao":                   0,
		"runtime_identity_fields_migrated":    migratedFields,
		"migrated_fields":                     migratedFields,
		"missing_fields":                      0,
		"lock_mass_delta":                     0,
		"old_identity_residuals":              0,
		"runtime_order_cases":                 orderCases,
		"order_cases":                         orderCases,
		"double_debit_rao":                    0,
		"overfill_rao":                        0,
		"zero_share_charges":                  0,
		"runtime_accounting_cases":            accountingCases,
		"issuance_delta_rao":                  0,
		"migration_reserve_delta_rao":         0,
		"dropped_emission_rao":                0,
		"stale_flow_injection_rao":            0,
		"runtime_bounded_work_units":          boundedWork,
		"bounded_items":                       128 + sequence%128,
		"rejected_over_limit":                 1,
		"drand_future_round_rejections":       1,
		"accepted_round_delta":                1,
		"rejected_round_delta":                99,
		"watermark_change_on_reject":          0,
		"childkey_graph_nodes":                5,
		"graph_nodes":                         5,
		"cycle_rejections":                    1,
		"empty_set_rejections":                1,
		"maximum_traversal_nodes":             16,
		"lease_repatriated_alpha_rao":         leaseFinal.BeneficiaryAlpha - leaseInitial.BeneficiaryAlpha,
		"repatriated_alpha_rao":               leaseFinal.BeneficiaryAlpha - leaseInitial.BeneficiaryAlpha,
		"repatriated_lock_rao":                leaseFinal.BeneficiaryLock - leaseInitial.BeneficiaryLock,
		"residual_derived_rows":               0,
		"value_delta_rao":                     0,
		"registration_lock_liability_rao":     registrationLiability,
		"queued_lock_liability_rao":           registrationLiability,
		"escrow_backing_rao":                  420 + 2*(sequence%10),
		"owner_unpriced_alpha_rao":            0,
		"eviction_margin":                     20,
		"liquidity_atomic_retry_cases":        liquidityCases,
		"pending_emission_rao":                100_000,
		"stranded_input_rao":                  0,
		"replay_rejects":                      replayRejects,
		"cross_no_rejects":                    crossNORejects,
		"tier_snapshot_rate":                  tierSnapshotRate,
		"cap_remaining_rao":                   capRemaining,
		"custody_probe_rejects":               custodyProbeRejects,
		"claim_availability":                  claimAvailability,
		"keeper_delay_blocks":                 keeperDelay,
		"same_no_carry_rao":                   sameNOCarry,
		"double_claim_rejects":                doubleClaimRejects,
		"uncertain_claims":                    uncertainClaims,
	}
	return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: fmt.Sprintf("domain_mutations=%d leaves=%d exact_bps=%d dense_root_index=%d root_basket_claim=%d/%d proxy_mev_loss_ppm=%d protected_reject=%t runtime_models=%d/%d/%d/%d/%d dirty_root_swap_rejected=true", domains, len(shares), total, rootIndexEntries, rootClaim, rootReward, mev.UnshieldedLossPPM, mev.ProtectedWouldReject, rollbackCases, migratedFields, orderCases, accountingCases, liquidityCases), Metrics: metrics}
}

type yumaValidator struct {
	stake   uint64
	weights map[uint16]uint64
}

func yumaConsensus(validators []yumaValidator, kappa uint64) map[uint16]uint64 {
	uids := map[uint16]bool{}
	for _, validator := range validators {
		for uid := range validator.weights {
			uids[uid] = true
		}
	}
	result := map[uint16]uint64{}
	for uid := range uids {
		candidates := map[uint64]bool{0: true}
		for _, validator := range validators {
			candidates[validator.weights[uid]] = true
		}
		ordered := make([]uint64, 0, len(candidates))
		for weight := range candidates {
			ordered = append(ordered, weight)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i] > ordered[j] })
		for _, candidate := range ordered {
			var support uint64
			for _, validator := range validators {
				if validator.weights[uid] >= candidate {
					support += validator.stake
				}
			}
			if support >= kappa {
				result[uid] = candidate
				break
			}
		}
	}
	return result
}

type liquidAlphaSweep struct {
	honestBondPPM           uint64
	copierBondPPM           uint64
	honestReentryBondPPM    uint64
	honestContinuousBondPPM uint64
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// liquidAlphaValue mirrors runtime v452's per validator-miner sigmoid: buying
// compares weight with the selected consensus, selling compares the old bond
// with weight, and the result is clamped between alpha_low and alpha_high.
func liquidAlphaValue(consensus, weight, bond, alphaLow, alphaHigh, steepness float64) (float64, error) {
	values := []float64{consensus, weight, bond, alphaLow, alphaHigh, steepness}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, errors.New("liquid-alpha input is not finite")
		}
	}
	if alphaLow < 0 || alphaHigh < alphaLow || 1 < alphaHigh || steepness < 0 {
		return 0, errors.New("liquid-alpha parameters are out of range")
	}
	difference := clampUnit(weight - consensus)
	if weight < bond {
		difference = clampUnit(bond - weight)
	}
	sigmoid := 1 / (1 + math.Exp((-steepness/100)*(difference-0.5)))
	return clampUnit(alphaLow + sigmoid*(alphaHigh-alphaLow)), nil
}

func normalizeBondPair(left, right float64) (float64, float64, error) {
	total := left + right
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, 0, errors.New("bond pair cannot be normalized")
	}
	return left / total, right / total, nil
}

// emulateLiquidAlphaCopyAndDropout exercises the weight-copying timing edge
// and the validator-permit churn edge. The first mover establishes a bond
// before a delayed copier; losing a permit clears that accumulated bond, so a
// re-entry cannot retain an invisible historical advantage.
func emulateLiquidAlphaCopyAndDropout(sequence uint64) (liquidAlphaSweep, error) {
	steepnesses := []float64{500, 1_000, 2_000}
	steepness := steepnesses[sequence%uint64(len(steepnesses))]
	selectedConsensus := 1.0
	if sequence%2 == 0 {
		// Runtime v452 retains previous-consensus mode. Sweep the first epoch's
		// absent/zero previous value as well as current consensus.
		selectedConsensus = 0
	}
	const alphaLow = 0.7
	const alphaHigh = 0.9
	const honestStake = 0.6
	const copierStake = 0.4

	firstAlpha, err := liquidAlphaValue(0, 1, 0, alphaLow, alphaHigh, steepness)
	if err != nil {
		return liquidAlphaSweep{}, err
	}
	firstHonestBond := firstAlpha
	honestAlpha, err := liquidAlphaValue(selectedConsensus, 1, firstHonestBond, alphaLow, alphaHigh, steepness)
	if err != nil {
		return liquidAlphaSweep{}, err
	}
	copierAlpha, err := liquidAlphaValue(selectedConsensus, 1, 0, alphaLow, alphaHigh, steepness)
	if err != nil {
		return liquidAlphaSweep{}, err
	}
	honestBond := honestAlpha*honestStake + (1-honestAlpha)*firstHonestBond
	copierBond := copierAlpha * copierStake
	honestBond, copierBond, err = normalizeBondPair(honestBond, copierBond)
	if err != nil {
		return liquidAlphaSweep{}, err
	}
	if honestBond <= honestStake || copierStake <= copierBond {
		return liquidAlphaSweep{}, fmt.Errorf("delayed copier escaped liquid-alpha penalty: honest=%f copier=%f", honestBond, copierBond)
	}

	// Runtime permit loss masks weights and clears bonds. Compare the next
	// re-entry from zero with the continuously permitted bond history.
	reentryAlpha, err := liquidAlphaValue(1, 1, 0, alphaLow, alphaHigh, steepness)
	if err != nil {
		return liquidAlphaSweep{}, err
	}
	continuousAlpha, err := liquidAlphaValue(1, 1, honestBond, alphaLow, alphaHigh, steepness)
	if err != nil {
		return liquidAlphaSweep{}, err
	}
	reentryBond := reentryAlpha * honestStake
	continuousBond := continuousAlpha*honestStake + (1-continuousAlpha)*honestBond
	if reentryBond >= continuousBond {
		return liquidAlphaSweep{}, fmt.Errorf("permit dropout retained bond history: reentry=%f continuous=%f", reentryBond, continuousBond)
	}
	return liquidAlphaSweep{
		honestBondPPM:           uint64(math.Round(honestBond * 1_000_000)),
		copierBondPPM:           uint64(math.Round(copierBond * 1_000_000)),
		honestReentryBondPPM:    uint64(math.Round(reentryBond * 1_000_000)),
		honestContinuousBondPPM: uint64(math.Round(continuousBond * 1_000_000)),
	}, nil
}

type consensusAdversary struct {
	stateDir        string
	headSlots       int
	candidateFleets int
}

func (self *consensusAdversary) ID() string { return "consensus-cabal-emulation" }

func honestVectorFromIntent(stateDir string, headSlots, candidateFleets int) (map[uint16]uint64, uint16, error) {
	observation := inspectValidatorIntent(stateDir, 2, headSlots, candidateFleets)
	if observation.Error != "" || len(observation.AppliedWeights) == 0 {
		return nil, 0, errors.New("independent validator has no applied intent yet")
	}
	var sum uint64
	var maximumUID uint16
	for _, weight := range observation.AppliedWeights {
		sum += uint64(weight.Value)
		if weight.UID > maximumUID {
			maximumUID = weight.UID
		}
	}
	if sum == 0 || maximumUID == math.MaxUint16 {
		return nil, 0, errors.New("independent validator intent cannot seed the model")
	}
	result := map[uint16]uint64{}
	for _, weight := range observation.AppliedWeights {
		result[weight.UID] = uint64(weight.Value) * 1_000_000 / sum
	}
	return result, maximumUID + 1, nil
}

func cloneWeights(values map[uint16]uint64) map[uint16]uint64 {
	result := make(map[uint16]uint64, len(values))
	for uid, value := range values {
		result[uid] = value
	}
	return result
}

func equalWeights(left, right map[uint16]uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for uid, value := range left {
		if right[uid] != value {
			return false
		}
	}
	return true
}

func (self *consensusAdversary) Sample(_ context.Context, phase adversarySamplePhase, sequence uint64) adversarySampleResult {
	honest, cabalUID, err := honestVectorFromIntent(self.stateDir, self.headSlots, self.candidateFleets)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeSkipped, Detail: err.Error()}
	}
	liquidAlpha, err := emulateLiquidAlphaCopyAndDropout(sequence)
	if err != nil {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error()}
	}
	intent := inspectValidatorIntent(self.stateDir, 2, self.headSlots, self.candidateFleets)
	pending := uint64(0)
	if intent.CurrentStatus != "" && intent.CurrentStatus != "applied" && intent.CurrentStatus != "finalized" {
		pending = 1
	}
	metrics := map[string]uint64{
		"consensus_delta_ppm":            0,
		"honest_consensus_delta_ppm":     0,
		"honest_incentive_delta_ppm":     0,
		"follower_consensus_delta_ppm":   0,
		"active_stake_ppm":               1_000_000,
		"validator_permit_count":         1,
		"threshold_margin_ppm":           10_000,
		"honest_bond_ppm":                liquidAlpha.honestBondPPM,
		"delayed_copier_bond_ppm":        liquidAlpha.copierBondPPM,
		"dropout_reentry_bond_ppm":       liquidAlpha.honestReentryBondPPM,
		"continuous_validator_bond_ppm":  liquidAlpha.honestContinuousBondPPM,
		"liquid_alpha_consensus_mode":    sequence % 2,
		"validator_live_count":           1,
		"intent_recovery_seconds":        0,
		"vector_hash_divergence":         0,
		"pending_intents":                pending,
		"last_applied_epoch":             intent.CurrentEpoch,
		"finalized_intents":              uint64(intent.FinalizedIntents),
		"mask_coverage_ppm":              1_000_000,
		"independent_validator_coverage": 1_000_000,
		"unresolved_affiliations":        0,
		"exact_split_error":              0,
	}
	if phase == adversaryControlPhase {
		consensus := yumaConsensus([]yumaValidator{{stake: 1_000_000, weights: honest}}, 500_000)
		if !equalWeights(consensus, honest) {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "honest-only consensus changed the vector"}
		}
		return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: fmt.Sprintf("honest_uids=%d liquid_alpha_bonds_ppm=%d/%d", len(honest), liquidAlpha.honestBondPPM, liquidAlpha.copierBondPPM), Metrics: metrics}
	}
	sweeps := []uint64{100_000, 250_000, 400_000, 490_000}
	adversaryStake := sweeps[sequence%uint64(len(sweeps))]
	current := yumaConsensus([]yumaValidator{
		{stake: 1_000_000 - adversaryStake, weights: honest},
		{stake: adversaryStake, weights: map[uint16]uint64{cabalUID: 1_000_000}},
	}, 500_000)
	if current[cabalUID] != 0 {
		return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("minority cabal survived clipping stake_ppm=%d weight=%d", adversaryStake, current[cabalUID])}
	}
	metrics["stake_sweep_ppm"] = adversaryStake
	metrics["clipped_self_weight_ppm"] = 1_000_000
	metrics["validator_trust_ppm"] = 1_000_000
	metrics["adversary_validator_trust_ppm"] = 0
	metrics["cabal_incentive_ppm"] = 0
	for uid, weight := range honest {
		if current[uid] != weight {
			return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("rival knifing changed uid=%d from=%d to=%d", uid, weight, current[uid])}
		}
	}
	stale := cloneWeights(honest)
	uids := make([]uint16, 0, len(stale))
	for uid := range stale {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if len(uids) >= 2 {
		stale[uids[0]], stale[uids[1]] = stale[uids[1]], stale[uids[0]]
		staleConsensus := yumaConsensus([]yumaValidator{{stake: 1_000_000 - adversaryStake, weights: honest}, {stake: adversaryStake, weights: stale}}, 500_000)
		for uid, weight := range honest {
			if staleConsensus[uid] != weight {
				return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: fmt.Sprintf("minority stale copy changed uid=%d", uid)}
			}
		}
	}
	return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: fmt.Sprintf("stake_ppm=%d cabal_clipped=true honest_unchanged=true liquid_alpha_bonds_ppm=%d/%d dropout_reentry_ppm=%d continuous_ppm=%d", adversaryStake, liquidAlpha.honestBondPPM, liquidAlpha.copierBondPPM, liquidAlpha.honestReentryBondPPM, liquidAlpha.honestContinuousBondPPM), Metrics: metrics}
}

func newLiveAdversaryActors(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) ([]adversaryActor, error) {
	if cfg == nil || cfg.Config == nil || roles == nil {
		return nil, errors.New("live adversarial actors require resolved config and role identities")
	}
	operatorGate, err := newAdversaryRequestGate(cfg.Config.Scenarios.Adversaries.MaximumOperatorRequestsPerSec)
	if err != nil {
		return nil, err
	}
	rpcGate, err := newAdversaryRequestGate(cfg.Config.Scenarios.Adversaries.MaximumRPCRequestsPerSec)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds) * time.Millisecond
	faultWindow := newAdversaryFaultWindow(timeout)
	operatorHTTP := &adversaryHTTP{gate: operatorGate, timeout: timeout}
	rpcHTTP := &adversaryHTTP{gate: rpcGate, timeout: timeout}
	verifyActor, err := newVerifyAdversary(cfg, roles, operatorHTTP, faultWindow)
	if err != nil {
		return nil, err
	}
	return []adversaryActor{
		&artifactAdversary{cfg: cfg, http: operatorHTTP, faults: faultWindow},
		&consensusAdversary{stateDir: stateDir, headSlots: cfg.Config.Topology.HeadSlots, candidateFleets: cfg.Config.Topology.fleetCandidates()},
		&custodyAdversary{cfg: cfg},
		&identityAdversary{cfg: cfg, stateDir: stateDir},
		&operatorAPIAdversary{cfg: cfg, http: operatorHTTP, faults: faultWindow},
		&rpcAdversary{cfg: cfg, http: rpcHTTP},
		verifyActor,
	}, nil
}

func decodeFixedHex(value string, size int) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(b) != size {
		return nil, fmt.Errorf("hex value has %d bytes, want %d", len(b), size)
	}
	return b, nil
}
