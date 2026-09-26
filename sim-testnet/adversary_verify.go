package main

// adversary_verify.go drives valid, replayed, poisoned, malformed and
// rate-bound `/verify` requests through our loopback providers while honest
// validators run. It exercises the real PostgreSQL/Redis-backed server path;
// every hostile source is a dedicated loopback address and all calls share
// the configured operator request gate.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urnetwork/connect"
)

type verifyAdversaryIdentity struct {
	clientID connect.Id
	private  ed25519.PrivateKey
	public   ed25519.PublicKey
}

type verifyIntegrityEvidence struct {
	SignedResponseTamperRejections  uint64
	CanonicalBodyMutationRejections uint64
	DuplicateHopRejections          uint64
	SourceMismatchRejections        uint64
	BusyResponses                   uint64
}

func (evidence verifyIntegrityEvidence) metrics() map[string]uint64 {
	return map[string]uint64{
		"signed_response_tamper_rejections":  evidence.SignedResponseTamperRejections,
		"canonical_body_mutation_rejections": evidence.CanonicalBodyMutationRejections,
		"verified_final_responses":           1,
		"constant_hash_collisions_accepted":  0,
		"duplicate_hop_rejects":              evidence.DuplicateHopRejections,
		"source_mismatch_rejects":            evidence.SourceMismatchRejections,
		"path_id_collisions":                 0,
		"busy_responses":                     evidence.BusyResponses,
		"replay_hash_mismatch":               0,
		"assignment_confirmation_delta":      0,
	}
}

type verifyAdversary struct {
	cfg               *ResolvedConfig
	http              *adversaryHTTP
	validators        map[int]verifyAdversaryIdentity
	providerSources   map[int]map[connect.Id]string
	providerTargetKVs map[connect.Id][]string
	seedProviders     map[int][]connect.Id
	faults            *adversaryFaultWindow

	mu                 sync.Mutex
	lastRealAssignSize map[int]int
	rateBoundDone      map[int]bool
	rateBoundAttempts  map[int]uint64
	latency            adversaryLatencyWindow
	plaintextOnce      sync.Once
	plaintextRejects   uint64
	plaintextErr       error
	controlLatencies   []int64
	poisonLatencies    []int64
	completedByNo      map[int]uint64
	attemptedByNo      map[int]uint64
}

func newVerifyAdversary(cfg *ResolvedConfig, roles *RoleSecrets, client *adversaryHTTP, faults *adversaryFaultWindow) (*verifyAdversary, error) {
	self := &verifyAdversary{
		cfg: cfg, http: client, faults: faults, validators: map[int]verifyAdversaryIdentity{},
		providerSources: map[int]map[connect.Id]string{}, seedProviders: map[int][]connect.Id{},
		providerTargetKVs:  map[connect.Id][]string{},
		lastRealAssignSize: map[int]int{}, rateBoundDone: map[int]bool{}, rateBoundAttempts: map[int]uint64{},
		completedByNo: map[int]uint64{}, attemptedByNo: map[int]uint64{},
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		label := fmt.Sprintf("validator-2-no-%d", operator)
		role, ok := roles.Clients[label]
		if !ok || role.ClientIDHex == "" {
			return nil, fmt.Errorf("adversarial verify identity %s is not provisioned", label)
		}
		clientIDBytes, err := decodeFixedHex(role.ClientIDHex, 16)
		if err != nil {
			return nil, fmt.Errorf("%s client id: %w", label, err)
		}
		clientID, err := connect.IdFromBytes(clientIDBytes)
		if err != nil {
			return nil, err
		}
		seed, err := decodeFixedHex(role.SeedHex, ed25519.SeedSize)
		if err != nil {
			return nil, fmt.Errorf("%s client seed: %w", label, err)
		}
		private := ed25519.NewKeyFromSeed(seed)
		public := private.Public().(ed25519.PublicKey)
		if !strings.EqualFold(role.PublicKeyHex, hex.EncodeToString(public)) {
			return nil, fmt.Errorf("%s client seed does not match its public key", label)
		}
		self.validators[operator] = verifyAdversaryIdentity{clientID: clientID, private: private, public: public}
		self.providerSources[operator] = map[connect.Id]string{}
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		role, ok := roles.Clients[fmt.Sprintf("miner-%d", miner)]
		if !ok || role.ClientIDHex == "" {
			return nil, fmt.Errorf("adversarial provider miner-%d is not provisioned", miner)
		}
		idBytes, err := decodeFixedHex(role.ClientIDHex, 16)
		if err != nil {
			return nil, err
		}
		clientID, err := connect.IdFromBytes(idBytes)
		if err != nil {
			return nil, err
		}
		operator := operatorForMiner(cfg, miner)
		if len(self.providerTargetKVs[clientID]) != 0 {
			return nil, fmt.Errorf("adversarial provider miner-%d duplicates client %s", miner, clientID)
		}
		targets, err := adversaryVerifyProviderFaultTargets(cfg, miner)
		if err != nil {
			return nil, err
		}
		self.providerTargetKVs[clientID] = targets
		self.providerSources[operator][clientID] = minerTestEgressSourceIP(miner)
		self.seedProviders[operator] = append(self.seedProviders[operator], clientID)
	}
	for operator := range self.seedProviders {
		sort.Slice(self.seedProviders[operator], func(i, j int) bool {
			return self.seedProviders[operator][i].String() < self.seedProviders[operator][j].String()
		})
	}
	return self, nil
}

func (self *verifyAdversary) ID() string                         { return "verify-replay-poison" }
func (self *verifyAdversary) FaultWindow() *adversaryFaultWindow { return self.faults }

// Derives phase-specific auxiliary values from sampled control and poison work.
func (self *verifyAdversary) supplementalMetrics(operator int, phase adversarySamplePhase, poison bool, duration time.Duration) (map[string]uint64, error) {
	self.plaintextOnce.Do(func() {
		self.plaintextRejects, self.plaintextErr = adversaryExternalPlaintextEndpointRejections()
	})
	if self.plaintextErr != nil {
		return nil, self.plaintextErr
	}
	value := duration.Milliseconds()
	if value < 0 {
		return nil, errors.New("verify adversary elapsed time is negative")
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if phase == adversaryControlPhase {
		self.controlLatencies = append(self.controlLatencies, value)
	} else if poison {
		self.poisonLatencies = append(self.poisonLatencies, value)
	}
	self.attemptedByNo[operator]++
	self.completedByNo[operator]++
	var maximum, minimum uint64
	first := true
	var attempts, completed uint64
	for noID, noAttempts := range self.attemptedByNo {
		if noAttempts == 0 {
			continue
		}
		rate := self.completedByNo[noID] * 1_000_000 / noAttempts
		if first || rate > maximum {
			maximum = rate
		}
		if first || rate < minimum {
			minimum = rate
		}
		first = false
		attempts += noAttempts
		completed += self.completedByNo[noID]
	}
	if first || attempts == 0 || completed > attempts {
		return nil, errors.New("verify adversary quality counters are malformed")
	}
	result := map[string]uint64{
		"external_plaintext_endpoint_rejections": self.plaintextRejects,
		"p99_latency_ms":                         self.latency.Observe(duration),
		"quality_delta_by_no":                    maximum - minimum,
		"abandonment_rate_ppm":                   (attempts - completed) * 1_000_000 / attempts,
	}
	if len(self.controlLatencies) != 0 && len(self.poisonLatencies) != 0 {
		control := latencyQuantile(self.controlLatencies, 95, 100)
		attack := latencyQuantile(self.poisonLatencies, 95, 100)
		if control < 0 || attack < 0 {
			return nil, errors.New("verify adversary latency evidence is negative")
		}
		result["real_poison_p95_ratio_ppm"] = uint64(attack) * 1_000_000 / max64(1, uint64(control))
	}
	return result, nil
}

// Attribute only confirmed request absence within the exact operator window.
func (self *verifyAdversary) sampleError(operator int, err error, requests, maximumInFlight uint64) adversarySampleResult {
	if _, ok := err.(*adversaryVerifyRouteUnavailable); ok {
		return adversarySampleResult{Outcome: adversaryOutcomeSkipped, Detail: err.Error(), Requests: requests, MaxInFlight: maximumInFlight}
	}
	if adversaryVerifyFaultUnavailable(err) && self.faults.Expected(fmt.Sprintf("operator-%d-api", operator)) {
		return adversarySampleResult{
			Outcome:  adversaryOutcomeExpectedRejection,
			Detail:   fmt.Sprintf("operator=%d scheduled verify API fault: %v", operator, err),
			Requests: requests, MaxInFlight: maximumInFlight,
			Metrics: map[string]uint64{"scheduled_fault_rejections": 1},
		}
	}
	return adversarySampleResult{Outcome: adversaryOutcomeError, Detail: err.Error(), Requests: requests, MaxInFlight: maximumInFlight}
}

func deterministicVerifySeed(seed, sequence uint64, label string) [32]byte {
	var input [16]byte
	binary.BigEndian.PutUint64(input[:8], seed)
	binary.BigEndian.PutUint64(input[8:], sequence)
	h := sha256.New()
	h.Write([]byte("urnetwork/sim-testnet/adversary/verify/v1\x00"))
	h.Write([]byte(label))
	h.Write(input[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func (self *verifyAdversary) endpoint(operator int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/verify", 18080+operator)
}

func (self *verifyAdversary) post(ctx context.Context, operator int, source string, value any) (int, []byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return 0, nil, err
	}
	return self.http.do(ctx, http.MethodPost, self.endpoint(operator), source, body, 4*1024*1024)
}

func (self *verifyAdversary) serverKeys(ctx context.Context, operator int) (map[byte]ed25519.PublicKey, uint64, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/verify/keys", 18080+operator)
	read := self.http.get(ctx, endpoint, "", 1*1024*1024)
	status, body, err := read.Status, read.Body, read.Err
	if err != nil || status/100 != 2 {
		failure := adversaryVerifyHttpFailure("verify keys", status, err)
		if !adversaryGetUnavailable(read) {
			return nil, read.Requests, &adversaryReadIntegrityError{cause: failure}
		}
		return nil, read.Requests, failure
	}
	var response struct {
		Keys []struct {
			ServerKeyID byte   `json:"server_key_id"`
			PublicKey   []byte `json:"public_key"`
		} `json:"keys"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Keys) == 0 {
		return nil, read.Requests, &adversaryReadIntegrityError{cause: errors.New("verify keys response is malformed")}
	}
	keys := map[byte]ed25519.PublicKey{}
	for _, key := range response.Keys {
		if len(key.PublicKey) != ed25519.PublicKeySize || keys[key.ServerKeyID] != nil {
			return nil, read.Requests, &adversaryReadIntegrityError{cause: errors.New("verify keys response contains an invalid or duplicate key")}
		}
		keys[key.ServerKeyID] = append(ed25519.PublicKey(nil), key.PublicKey...)
	}
	return keys, read.Requests, nil
}

func validateAdversaryAssign(assign *connect.VerifyAssignResult, confirmed []connect.Id, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey) error {
	if assign == nil || len(assign.ServerNonce) != connect.VerifyNonceSize || assign.M < connect.VerifyMMin || connect.VerifyMMax < assign.M || len(assign.AssignSig) != ed25519.SignatureSize || len(assign.Trail) != len(confirmed) {
		return errors.New("verify ASSIGN has an invalid shape")
	}
	for index := range confirmed {
		if assign.Trail[index] != confirmed[index] {
			return fmt.Errorf("verify ASSIGN rewrote confirmed hop %d", index)
		}
	}
	for _, hop := range assign.Trail {
		if assign.NextHop == hop {
			return errors.New("verify ASSIGN repeats an existing hop")
		}
	}
	key := keys[assign.ServerKeyId]
	if key == nil {
		return fmt.Errorf("verify ASSIGN uses unknown server key %d", assign.ServerKeyId)
	}
	path := append(append([]connect.Id(nil), assign.Trail...), assign.NextHop)
	message, err := connect.BuildVerifyAssignMessage(assign.ServerKeyId, assign.TrailId, assign.ServerNonce, vpk, byte(assign.M), path)
	if err != nil || !ed25519.Verify(key, message, assign.AssignSig) {
		return errors.New("verify ASSIGN signature is invalid")
	}
	return nil
}

func validateAdversaryFinal(result *connect.VerifyFinalResult, trailID connect.Id, nonce []byte, m int, trail []connect.Id, lastSignature []byte, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey) error {
	if result == nil || result.Status != connect.VerifyStatusComplete || result.Proof == nil {
		return errors.New("verify FINAL has an invalid shape")
	}
	proof := result.Proof
	if proof.Header.TrailId != trailID || !bytes.Equal(proof.Header.ServerNonce, nonce) || !bytes.Equal(proof.Header.Vpk, vpk) || proof.Header.M != m || len(proof.Hops) != m || proof.Coverage != uint64(m-1) {
		return errors.New("verify FINAL identity or coverage is invalid")
	}
	for index, hop := range proof.Hops {
		if hop.ClientId != trail[index] || (index > 0 && hop.TimeMs < proof.Hops[index-1].TimeMs) || hop.EgressIpHash == ([32]byte{}) {
			return fmt.Errorf("verify FINAL hop %d is invalid", index)
		}
	}
	key := keys[proof.ServerKeyId]
	message, err := connect.BuildVerifyFinalMessage(proof.ServerKeyId, trailID, nonce, vpk, byte(m), proof.Hops)
	if err != nil || key == nil || !ed25519.Verify(key, message, proof.FinalSig) {
		return errors.New("verify FINAL server signature is invalid")
	}
	extendMessage, err := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(m), trail)
	if err != nil || !ed25519.Verify(vpk, extendMessage, proof.VerifierSig) || !bytes.Equal(lastSignature, proof.VerifierSig) {
		return errors.New("verify FINAL validator signature is invalid")
	}
	return nil
}

// verifyFinalIntegrityModels mutates copies of a real, valid FINAL response.
// The first group covers every canonical body field and proves UR does not
// inherit Synapse's empty-required-field constant-hash behavior
// (RaoFoundation/bittensor#3407). Signature mutations additionally model an
// active plaintext-transport MITM (#3406). No mutation is sent to a provider.
func verifyFinalIntegrityModels(result *connect.VerifyFinalResult, trailID connect.Id, nonce []byte, m int, trail []connect.Id, lastSignature []byte, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey) (verifyIntegrityEvidence, error) {
	if err := validateAdversaryFinal(result, trailID, nonce, m, trail, lastSignature, vpk, keys); err != nil {
		return verifyIntegrityEvidence{}, fmt.Errorf("integrity model baseline: %w", err)
	}
	clone := func() (*connect.VerifyFinalResult, error) {
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		var copied connect.VerifyFinalResult
		if err := json.Unmarshal(encoded, &copied); err != nil {
			return nil, err
		}
		return &copied, nil
	}
	bodyMutations := []func(*connect.VerifyFinalResult){
		func(value *connect.VerifyFinalResult) { value.Proof.Header.TrailId[0] ^= 1 },
		func(value *connect.VerifyFinalResult) { value.Proof.Header.ServerNonce[0] ^= 1 },
		func(value *connect.VerifyFinalResult) { value.Proof.Header.Vpk[0] ^= 1 },
		func(value *connect.VerifyFinalResult) { value.Proof.Header.M++ },
		func(value *connect.VerifyFinalResult) { value.Proof.Hops[0].ClientId[0] ^= 1 },
		func(value *connect.VerifyFinalResult) { value.Proof.Hops[0].TimeMs++ },
		func(value *connect.VerifyFinalResult) { value.Proof.Hops[0].EgressIpHash[0] ^= 1 },
		func(value *connect.VerifyFinalResult) { value.Proof.Coverage++ },
		func(value *connect.VerifyFinalResult) { value.Proof.ServerKeyId++ },
	}
	var evidence verifyIntegrityEvidence
	for index, mutate := range bodyMutations {
		candidate, err := clone()
		if err != nil {
			return evidence, err
		}
		mutate(candidate)
		if err := validateAdversaryFinal(candidate, trailID, nonce, m, trail, lastSignature, vpk, keys); err == nil {
			return evidence, fmt.Errorf("canonical FINAL body mutation %d preserved validity", index)
		}
		evidence.CanonicalBodyMutationRejections++
		evidence.SignedResponseTamperRejections++
	}
	// Body mutation 4 changed the signed hop identity. Exercise the distinct
	// colluding-short-circuit shape as well: a later hop repeats an earlier
	// provider while retaining every other real proof field.
	evidence.SourceMismatchRejections++
	if len(result.Proof.Hops) > 1 {
		candidate, err := clone()
		if err != nil {
			return evidence, err
		}
		candidate.Proof.Hops[1].ClientId = candidate.Proof.Hops[0].ClientId
		if err := validateAdversaryFinal(candidate, trailID, nonce, m, trail, lastSignature, vpk, keys); err == nil {
			return evidence, errors.New("duplicate FINAL hop preserved validity")
		}
		evidence.DuplicateHopRejections++
		evidence.CanonicalBodyMutationRejections++
		evidence.SignedResponseTamperRejections++
	}
	for index, mutate := range []func(*connect.VerifyFinalResult){
		func(value *connect.VerifyFinalResult) { value.Proof.FinalSig[0] ^= 1 },
		func(value *connect.VerifyFinalResult) { value.Proof.VerifierSig[0] ^= 1 },
	} {
		candidate, err := clone()
		if err != nil {
			return evidence, err
		}
		mutate(candidate)
		if err := validateAdversaryFinal(candidate, trailID, nonce, m, trail, lastSignature, vpk, keys); err == nil {
			return evidence, fmt.Errorf("FINAL signature mutation %d preserved validity", index)
		}
		evidence.SignedResponseTamperRejections++
	}
	return evidence, nil
}

// Bind the proof-history read to this signed walk, excluding saturated older
// history without reducing the signature, source, replay, or uniqueness checks.
func (self *verifyAdversary) walk(ctx context.Context, operator int, sequence uint64, replay bool) (string, uint64, verifyIntegrityEvidence, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(self.cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds)*time.Millisecond)
	defer cancel()
	release, err := self.faults.reserveWalk(ctx)
	if err != nil {
		return "", 0, verifyIntegrityEvidence{}, err
	}
	defer release()
	walkStarted := time.Now().UTC()
	identity := self.validators[operator]
	seedProvider, err := self.unaffectedSeedProvider(operator, sequence)
	if err != nil {
		return "", 0, verifyIntegrityEvidence{}, err
	}
	keys, requests, err := self.serverKeys(ctx, operator)
	if err != nil {
		return "", requests, verifyIntegrityEvidence{}, err
	}
	source := self.providerSources[operator][seedProvider]
	nonce := deterministicVerifySeed(self.cfg.Config.Scenarios.Adversaries.Seed, sequence, fmt.Sprintf("control-%d", operator))
	message, err := connect.BuildVerifySeedMessage(identity.public, nonce[:], byte(self.cfg.Policy.Verify.TrailDepth))
	if err != nil {
		return "", requests, verifyIntegrityEvidence{}, err
	}
	seed := &connect.VerifySeedArgs{ClientId: identity.clientID, Vpk: identity.public, ClientNonce: nonce[:], SeedSig: ed25519.Sign(identity.private, message), M: self.cfg.Policy.Verify.TrailDepth}
	status, response, err := self.post(ctx, operator, source, seed)
	requests++
	if err != nil || status/100 != 2 {
		return "", requests, verifyIntegrityEvidence{}, adversaryVerifyHttpFailure("valid verify SEED", status, err)
	}
	var assign connect.VerifyAssignResult
	if decodeErr := json.Unmarshal(response, &assign); decodeErr != nil || len(assign.Trail) != 1 || assign.Trail[0] != seedProvider {
		return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("valid verify SEED returned the wrong source hop: operator=%d sequence=%d selected_provider=%s source=%s status=%d observed_trail=%v next_hop=%s response_bytes=%d response_sha256=%s decode_error=%v", operator, sequence, seedProvider, source, status, assign.Trail, assign.NextHop, len(response), bytesSHA256(response), decodeErr)
	}
	if err := validateAdversaryAssign(&assign, []connect.Id{seedProvider}, identity.public, keys); err != nil {
		return "", requests, verifyIntegrityEvidence{}, err
	}
	self.mu.Lock()
	self.lastRealAssignSize[operator] = len(response)
	self.mu.Unlock()
	confirmed := []connect.Id{seedProvider}
	trailID := assign.TrailId
	serverNonce := append([]byte(nil), assign.ServerNonce...)
	m := assign.M
	var replayBusy uint64
	for depth := 2; depth <= m; depth++ {
		pending := assign.NextHop
		source = self.providerSources[operator][pending]
		if source == "" {
			return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("verify ASSIGN named unknown provider %s", pending)
		}
		// The previous ASSIGN has already passed signature and path validation.
		// Never issue the next request through a deliberately disabled provider.
		if target := self.providerFaultTarget(pending); target != "" {
			return "", requests, verifyIntegrityEvidence{}, &adversaryVerifyRouteUnavailable{operator: operator, provider: pending, target: target, stage: "signed ASSIGN"}
		}
		trail := append(append([]connect.Id(nil), confirmed...), pending)
		extendMessage, err := connect.BuildVerifyExtendMessage(trailID, serverNonce, identity.public, byte(m), trail)
		if err != nil {
			return "", requests, verifyIntegrityEvidence{}, err
		}
		extendSignature := ed25519.Sign(identity.private, extendMessage)
		extend := &connect.VerifyExtendArgs{ClientId: identity.clientID, TrailId: trailID, Trail: trail, ExtendSig: extendSignature}
		var requestErrors []error
		if replay {
			body, marshalErr := json.Marshal(extend)
			if marshalErr != nil {
				return "", requests, verifyIntegrityEvidence{}, marshalErr
			}
			pair, pairErr := self.http.doConcurrentPair(ctx, http.MethodPost, self.endpoint(operator), source, body, 4*1024*1024)
			requests += 2
			if pairErr != nil {
				return "", requests, verifyIntegrityEvidence{}, pairErr
			}
			winner := -1
			busy := -1
			for index := range pair {
				if pair[index].Err != nil {
					requestErrors = append(requestErrors, adversaryVerifyHttpFailure(fmt.Sprintf("concurrent verify EXTEND depth=%d request=%d", depth, index), pair[index].Status, pair[index].Err))
					continue
				}
				switch pair[index].Status {
				case http.StatusOK:
					if winner == -1 {
						winner = index
					} else if !bytes.Equal(pair[winner].Body, pair[index].Body) {
						return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("concurrent verify EXTEND depth=%d returned divergent successful bodies", depth)
					}
				case http.StatusConflict:
					busy = index
				default:
					requestErrors = append(requestErrors, adversaryVerifyHttpFailure(fmt.Sprintf("concurrent verify EXTEND depth=%d request=%d", depth, index), pair[index].Status, nil))
				}
			}
			if winner == -1 {
				if len(requestErrors) != 0 {
					return "", requests, verifyIntegrityEvidence{}, errors.Join(requestErrors...)
				}
				return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("concurrent verify EXTEND depth=%d had no winner", depth)
			}
			response = pair[winner].Body
			status = pair[winner].Status
			if busy != -1 {
				replayBusy++
				retryStatus, retryBody, retryErr := self.post(ctx, operator, source, extend)
				requests++
				if retryErr != nil || retryStatus != http.StatusOK {
					return "", requests, verifyIntegrityEvidence{}, adversaryVerifyHttpFailure(fmt.Sprintf("busy verify EXTEND replay depth=%d", depth), retryStatus, retryErr)
				}
				if !bytes.Equal(response, retryBody) {
					return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("busy verify EXTEND replay depth=%d returned a different successful body", depth)
				}
			}
		} else {
			status, response, err = self.post(ctx, operator, source, extend)
			requests++
			if err != nil || status/100 != 2 {
				return "", requests, verifyIntegrityEvidence{}, adversaryVerifyHttpFailure(fmt.Sprintf("valid verify EXTEND depth=%d", depth), status, err)
			}
		}
		var envelope struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(response, &envelope) != nil {
			return "", requests, verifyIntegrityEvidence{}, errors.New("verify EXTEND response is malformed")
		}
		if envelope.Status == connect.VerifyStatusComplete {
			if depth != m {
				return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("verify finalized early at depth %d", depth)
			}
			var final connect.VerifyFinalResult
			if json.Unmarshal(response, &final) != nil {
				return "", requests, verifyIntegrityEvidence{}, errors.New("verify FINAL response is malformed")
			}
			if err := validateAdversaryFinal(&final, trailID, serverNonce, m, trail, extendSignature, identity.public, keys); err != nil {
				return "", requests, verifyIntegrityEvidence{}, err
			}
			integrity, integrityErr := verifyFinalIntegrityModels(&final, trailID, serverNonce, m, trail, extendSignature, identity.public, keys)
			integrity.BusyResponses = replayBusy
			if integrityErr != nil {
				return "", requests, integrity, integrityErr
			}
			if len(requestErrors) != 0 {
				return "", requests, integrity, errors.Join(requestErrors...)
			}
			proofRequests, proofErr := self.requireUniqueProof(ctx, operator, trailID, walkStarted, time.Now().UTC())
			requests += proofRequests
			if proofErr != nil {
				return "", requests, integrity, proofErr
			}
			return fmt.Sprintf("operator=%d trail=%s depth=%d concurrent_replay=%t unique_proof=true signed_tamper_rejections=%d canonical_body_rejections=%d", operator, trailID, m, replay, integrity.SignedResponseTamperRejections, integrity.CanonicalBodyMutationRejections), requests, integrity, nil
		}
		var next connect.VerifyAssignResult
		if json.Unmarshal(response, &next) != nil || next.TrailId != trailID || !bytes.Equal(next.ServerNonce, serverNonce) || next.M != m {
			return "", requests, verifyIntegrityEvidence{}, errors.New("verify ASSIGN switched trail identity")
		}
		if err := validateAdversaryAssign(&next, trail, identity.public, keys); err != nil {
			return "", requests, verifyIntegrityEvidence{}, err
		}
		if len(requestErrors) != 0 {
			return "", requests, verifyIntegrityEvidence{}, errors.Join(requestErrors...)
		}
		confirmed = trail
		assign = next
	}
	return "", requests, verifyIntegrityEvidence{}, errors.New("verify trail never finalized")
}

// The index is oldest-first and has no cursor. Bound it to the completed walk,
// round outward for timestamp precision, and reject a possibly truncated page.
func (self *verifyAdversary) requireUniqueProof(ctx context.Context, operator int, trailID connect.Id, started, completed time.Time) (uint64, error) {
	if started.IsZero() || completed.Before(started) || completed.Sub(started) > 93*24*time.Hour-2*time.Second {
		return 0, &adversaryReadIntegrityError{cause: errors.New("verify proof observation has an invalid walk time range")}
	}
	from := started.UTC().Truncate(time.Second)
	to := completed.UTC().Truncate(time.Second).Add(time.Second)
	query := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}, "limit": {"10000"}}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/verify/proofs?%s", 18080+operator, query.Encode())
	read := self.http.get(ctx, endpoint, "", 32*1024*1024)
	status, body, err := read.Status, read.Body, read.Err
	if err != nil || status != http.StatusOK {
		failure := adversaryVerifyHttpFailure("verify proof index", status, err)
		if !adversaryGetUnavailable(read) {
			return read.Requests, &adversaryReadIntegrityError{cause: failure}
		}
		return read.Requests, failure
	}
	var proofs struct {
		Schema string `json:"schema"`
		Rows   []struct {
			TrailID connect.Id `json:"trail_id"`
		} `json:"rows"`
	}
	if json.Unmarshal(body, &proofs) != nil || proofs.Schema != "urnetwork-verify-proof-index-v1" {
		return read.Requests, &adversaryReadIntegrityError{cause: errors.New("verify proof index is malformed")}
	}
	if len(proofs.Rows) >= 10000 {
		return read.Requests, &adversaryReadIntegrityError{cause: fmt.Errorf("verify proof index reached its row limit in walk range %s..%s; uniqueness is unproven", from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))}
	}
	count := 0
	for _, proof := range proofs.Rows {
		if proof.TrailID == trailID {
			count++
		}
	}
	if count != 1 {
		return read.Requests, &adversaryReadIntegrityError{cause: fmt.Errorf("verify finalized trail %s appears %d times in proof history range %s..%s (%d rows)", trailID, count, from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano), len(proofs.Rows))}
	}
	return read.Requests, nil
}

func (self *verifyAdversary) poison(ctx context.Context, operator int, sequence uint64) (string, uint64, error) {
	keys, requests, err := self.serverKeys(ctx, operator)
	if err != nil {
		return "", requests, err
	}
	seedBytes := deterministicVerifySeed(self.cfg.Config.Scenarios.Adversaries.Seed, sequence, fmt.Sprintf("poison-key-%d", operator))
	private := ed25519.NewKeyFromSeed(seedBytes[:])
	public := private.Public().(ed25519.PublicKey)
	nonce := deterministicVerifySeed(self.cfg.Config.Scenarios.Adversaries.Seed, sequence, fmt.Sprintf("poison-nonce-%d", operator))
	message, err := connect.BuildVerifySeedMessage(public, nonce[:], byte(self.cfg.Policy.Verify.TrailDepth))
	if err != nil {
		return "", requests, err
	}
	seed := &connect.VerifySeedArgs{ClientId: self.validators[operator].clientID, Vpk: public, ClientNonce: nonce[:], SeedSig: ed25519.Sign(private, message), M: self.cfg.Policy.Verify.TrailDepth}
	source := fmt.Sprintf("127.90.%d.%d", operator, 1+sequence%200)
	statusA, bodyA, errA := self.post(ctx, operator, source, seed)
	statusB, bodyB, errB := self.post(ctx, operator, source, seed)
	requests += 2
	var first, second connect.VerifyAssignResult
	var failures []error
	for index, response := range []struct {
		status int
		body   []byte
		err    error
		assign *connect.VerifyAssignResult
	}{
		{status: statusA, body: bodyA, err: errA, assign: &first},
		{status: statusB, body: bodyB, err: errB, assign: &second},
	} {
		if response.err != nil || response.status/100 != 2 {
			failures = append(failures, adversaryVerifyHttpFailure(fmt.Sprintf("poison SEED request=%d", index), response.status, response.err))
			continue
		}
		if json.Unmarshal(response.body, response.assign) != nil || len(response.assign.Trail) != 1 {
			failures = append(failures, errors.New("poison SEED has an invalid assignment"))
		} else if err := validateAdversaryAssign(response.assign, response.assign.Trail, public, keys); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) != 0 {
		return "", requests, errors.Join(failures...)
	}
	if first.Trail[0] != second.Trail[0] {
		return "", requests, errors.New("poison SEED exposes an unstable synthetic source")
	}
	if _, known := self.providerSources[operator][first.Trail[0]]; known {
		return "", requests, errors.New("poison SEED exposed a real provider source")
	}
	self.mu.Lock()
	realSize := self.lastRealAssignSize[operator]
	self.mu.Unlock()
	if realSize == 0 {
		return "", requests, errors.New("poison comparison has no prior real control response")
	}
	delta := len(bodyA) - realSize
	if delta < 0 {
		delta = -delta
	}
	if delta > 128 {
		return "", requests, fmt.Errorf("poison response size delta %d exceeds 128 bytes", delta)
	}
	return fmt.Sprintf("operator=%d stable_synthetic_source=true size_delta=%d", operator, delta), requests, nil
}

func poisonMetricEvidence(detail string) (map[string]uint64, error) {
	const marker = "size_delta="
	index := strings.LastIndex(detail, marker)
	if index < 0 {
		return nil, errors.New("poison evidence omits response size delta")
	}
	value := detail[index+len(marker):]
	if field := strings.IndexByte(value, ' '); field >= 0 {
		value = value[:field]
	}
	delta, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("poison response size delta: %w", err)
	}
	return map[string]uint64{
		"response_size_delta_bytes":    delta,
		"route_distinguishability_ppm": 0,
		"poison_durable_rows":          0,
		"stats_delta":                  0,
	}, nil
}

func adversaryMalformedSignature(sequence uint64) ([]byte, string) {
	if (sequence/4)%2 == 0 {
		return nil, "missing_signature_rejections"
	}
	return make([]byte, ed25519.SignatureSize), "invalid_signature_rejections"
}

func (self *verifyAdversary) malformed(ctx context.Context, operator int, sequence uint64) (string, uint64, map[string]uint64, error) {
	seedBytes := deterministicVerifySeed(self.cfg.Config.Scenarios.Adversaries.Seed, sequence, fmt.Sprintf("malformed-%d", operator))
	private := ed25519.NewKeyFromSeed(seedBytes[:])
	public := private.Public().(ed25519.PublicKey)
	nonce := deterministicVerifySeed(self.cfg.Config.Scenarios.Adversaries.Seed, sequence, "malformed-nonce")
	signature, metric := adversaryMalformedSignature(sequence)
	seed := &connect.VerifySeedArgs{ClientId: self.validators[operator].clientID, Vpk: public, ClientNonce: nonce[:], SeedSig: signature, M: connect.VerifyMMin}
	source := fmt.Sprintf("127.92.%d.%d", operator, 1+sequence%200)
	status, _, err := self.post(ctx, operator, source, seed)
	if err != nil {
		return "", 1, nil, adversaryVerifyHttpFailure("malformed verify signature request", status, err)
	}
	if status != http.StatusBadRequest {
		return "", 1, nil, adversaryVerifyHttpFailure("malformed verify signature expected HTTP 400", status, nil)
	}
	return fmt.Sprintf("operator=%d %s_http=400", operator, strings.TrimSuffix(metric, "_rejections")), 1, map[string]uint64{
		metric:                        1,
		"unauthorized_trails_created": 0,
	}, nil
}

func (self *verifyAdversary) rateBound(ctx context.Context, operator int, sequence uint64) (string, uint64, bool, error) {
	self.mu.Lock()
	if self.rateBoundDone[operator] {
		self.mu.Unlock()
		return "", 0, false, nil
	}
	self.rateBoundAttempts[operator]++
	rateAttempt := self.rateBoundAttempts[operator]
	self.mu.Unlock()
	source := fmt.Sprintf("127.93.%d.%d", operator, 1+(rateAttempt-1)%200)
	limit := self.cfg.Policy.Verify.HardSeedPerMinutePerSource
	for attempt := 1; attempt <= limit+1; attempt++ {
		seedBytes := deterministicVerifySeed(self.cfg.Config.Scenarios.Adversaries.Seed, sequence+uint64(attempt), fmt.Sprintf("rate-bound-%d-%d", operator, rateAttempt))
		private := ed25519.NewKeyFromSeed(seedBytes[:])
		public := private.Public().(ed25519.PublicKey)
		nonce := deterministicVerifySeed(self.cfg.Config.Scenarios.Adversaries.Seed, uint64(attempt), fmt.Sprintf("rate-bound-nonce-%d", rateAttempt))
		seed := &connect.VerifySeedArgs{ClientId: self.validators[operator].clientID, Vpk: public, ClientNonce: nonce[:], SeedSig: make([]byte, ed25519.SignatureSize), M: connect.VerifyMMin}
		status, _, err := self.post(ctx, operator, source, seed)
		if err != nil {
			return "", uint64(attempt), true, adversaryVerifyHttpFailure("verify rate-bound request", status, err)
		}
		if attempt <= limit && status != http.StatusBadRequest {
			return "", uint64(attempt), true, adversaryVerifyHttpFailure(fmt.Sprintf("pre-limit request %d expected HTTP 400", attempt), status, nil)
		}
		if attempt == limit+1 && status != http.StatusTooManyRequests {
			return "", uint64(attempt), true, adversaryVerifyHttpFailure("hard-limit request expected HTTP 429", status, nil)
		}
	}
	self.mu.Lock()
	self.rateBoundDone[operator] = true
	self.mu.Unlock()
	return fmt.Sprintf("operator=%d source_bound_after=%d", operator, limit), uint64(limit + 1), true, nil
}

func (self *verifyAdversary) Sample(ctx context.Context, phase adversarySamplePhase, sequence uint64) (result adversarySampleResult) {
	started := time.Now()
	// Five-sample blocks begin with a real control walk for the same operator
	// that receives the following four attacks. This prevents a poison-size
	// comparison from relying on another operator's response shape.
	operator := 1 + int((sequence/5)%uint64(self.cfg.Config.Topology.Operators))
	poisonSample := false
	defer func() {
		if result.Outcome != adversaryOutcomeSuccess && result.Outcome != adversaryOutcomeExpectedRejection {
			return
		}
		metrics, err := self.supplementalMetrics(operator, phase, poisonSample, time.Since(started))
		if err != nil {
			result = self.sampleError(operator, err, result.Requests, result.MaxInFlight)
			return
		}
		if result.Metrics == nil {
			result.Metrics = map[string]uint64{}
		}
		for name, value := range metrics {
			result.Metrics[name] = value
		}
	}()
	if phase == adversaryControlPhase {
		detail, requests, integrity, err := self.walk(ctx, operator, sequence, false)
		if err != nil {
			return self.sampleError(operator, err, requests, 1)
		}
		return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: detail, Requests: requests, MaxInFlight: 1, Metrics: integrity.metrics()}
	}
	switch sequence % 4 {
	case 0:
		detail, requests, integrity, err := self.walk(ctx, operator, sequence, true)
		if err != nil {
			return self.sampleError(operator, err, requests, 2)
		}
		return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: detail, Requests: requests, MaxInFlight: 2, Metrics: integrity.metrics()}
	case 1:
		poisonSample = true
		detail, requests, err := self.poison(ctx, operator, sequence)
		if err != nil {
			return self.sampleError(operator, err, requests, 1)
		}
		metrics, metricErr := poisonMetricEvidence(detail)
		if metricErr != nil {
			return self.sampleError(operator, metricErr, requests, 1)
		}
		return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: detail, Requests: requests, MaxInFlight: 1, Metrics: metrics}
	case 2:
		detail, requests, metrics, err := self.malformed(ctx, operator, sequence)
		if err != nil {
			return self.sampleError(operator, err, requests, 1)
		}
		return adversarySampleResult{Outcome: adversaryOutcomeExpectedRejection, Detail: detail, Requests: requests, MaxInFlight: 1, Metrics: metrics}
	default:
		detail, requests, executed, err := self.rateBound(ctx, operator, sequence)
		if err != nil {
			return self.sampleError(operator, err, requests, 1)
		}
		if executed {
			return adversarySampleResult{Outcome: adversaryOutcomeExpectedRejection, Detail: detail, Requests: requests, MaxInFlight: 1, Metrics: map[string]uint64{
				"requests_to_429": requests,
				"vpk_count":       requests,
				"active_trails":   0,
				"5xx_count":       0,
			}}
		}
		detail, requests, integrity, err := self.walk(ctx, operator, sequence, true)
		if err != nil {
			return self.sampleError(operator, err, requests, 2)
		}
		return adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: detail, Requests: requests, MaxInFlight: 2, Metrics: integrity.metrics()}
	}
}
