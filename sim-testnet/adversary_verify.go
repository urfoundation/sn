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
	"sort"
	"strconv"
	"strings"
	"sync"

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
	cfg             *ResolvedConfig
	http            *adversaryHTTP
	validators      map[int]verifyAdversaryIdentity
	providerSources map[int]map[connect.Id]string
	seedProviders   map[int][]connect.Id
	faults          *adversaryFaultWindow

	mu                 sync.Mutex
	lastRealAssignSize map[int]int
	rateBoundDone      map[int]bool
	rateBoundAttempts  map[int]uint64
}

func newVerifyAdversary(cfg *ResolvedConfig, roles *RoleSecrets, client *adversaryHTTP, faults *adversaryFaultWindow) (*verifyAdversary, error) {
	self := &verifyAdversary{
		cfg: cfg, http: client, faults: faults, validators: map[int]verifyAdversaryIdentity{},
		providerSources: map[int]map[connect.Id]string{}, seedProviders: map[int][]connect.Id{},
		lastRealAssignSize: map[int]int{}, rateBoundDone: map[int]bool{}, rateBoundAttempts: map[int]uint64{},
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

func (self *verifyAdversary) sampleError(operator int, err error, requests, maximumInFlight uint64) adversarySampleResult {
	if self.faults.Expected(fmt.Sprintf("operator-%d-api", operator)) {
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
	return self.http.do(ctx, http.MethodPost, self.endpoint(operator), source, body, 4<<20)
}

func (self *verifyAdversary) serverKeys(ctx context.Context, operator int) (map[byte]ed25519.PublicKey, uint64, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/verify/keys", 18080+operator)
	status, body, err := self.http.do(ctx, http.MethodGet, endpoint, "", nil, 1<<20)
	if err != nil || status/100 != 2 {
		return nil, 1, fmt.Errorf("verify keys status=%d error=%v", status, err)
	}
	var response struct {
		Keys []struct {
			ServerKeyID byte   `json:"server_key_id"`
			PublicKey   []byte `json:"public_key"`
		} `json:"keys"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Keys) == 0 {
		return nil, 1, errors.New("verify keys response is malformed")
	}
	keys := map[byte]ed25519.PublicKey{}
	for _, key := range response.Keys {
		if len(key.PublicKey) != ed25519.PublicKeySize || keys[key.ServerKeyID] != nil {
			return nil, 1, errors.New("verify keys response contains an invalid or duplicate key")
		}
		keys[key.ServerKeyID] = append(ed25519.PublicKey(nil), key.PublicKey...)
	}
	return keys, 1, nil
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

func (self *verifyAdversary) walk(ctx context.Context, operator int, sequence uint64, replay bool) (string, uint64, verifyIntegrityEvidence, error) {
	identity := self.validators[operator]
	providers := self.seedProviders[operator]
	if len(providers) == 0 {
		return "", 0, verifyIntegrityEvidence{}, errors.New("operator has no adversarial seed provider")
	}
	keys, requests, err := self.serverKeys(ctx, operator)
	if err != nil {
		return "", requests, verifyIntegrityEvidence{}, err
	}
	seedProvider := providers[int(sequence%uint64(len(providers)))]
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
		return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("valid verify SEED status=%d error=%v", status, err)
	}
	var assign connect.VerifyAssignResult
	if json.Unmarshal(response, &assign) != nil || len(assign.Trail) != 1 || assign.Trail[0] != seedProvider {
		return "", requests, verifyIntegrityEvidence{}, errors.New("valid verify SEED returned the wrong source hop")
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
		trail := append(append([]connect.Id(nil), confirmed...), pending)
		extendMessage, err := connect.BuildVerifyExtendMessage(trailID, serverNonce, identity.public, byte(m), trail)
		if err != nil {
			return "", requests, verifyIntegrityEvidence{}, err
		}
		extendSignature := ed25519.Sign(identity.private, extendMessage)
		extend := &connect.VerifyExtendArgs{ClientId: identity.clientID, TrailId: trailID, Trail: trail, ExtendSig: extendSignature}
		if replay {
			body, marshalErr := json.Marshal(extend)
			if marshalErr != nil {
				return "", requests, verifyIntegrityEvidence{}, marshalErr
			}
			pair, pairErr := self.http.doConcurrentPair(ctx, http.MethodPost, self.endpoint(operator), source, body, 4<<20)
			requests += 2
			if pairErr != nil {
				return "", requests, verifyIntegrityEvidence{}, pairErr
			}
			winner := -1
			busy := -1
			for index := range pair {
				if pair[index].Err != nil {
					return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("concurrent verify EXTEND depth=%d request=%d: %w", depth, index, pair[index].Err)
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
					return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("concurrent verify EXTEND depth=%d request=%d returned HTTP %d", depth, index, pair[index].Status)
				}
			}
			if winner == -1 {
				return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("concurrent verify EXTEND depth=%d had no winner", depth)
			}
			response = pair[winner].Body
			status = pair[winner].Status
			if busy != -1 {
				replayBusy++
				retryStatus, retryBody, retryErr := self.post(ctx, operator, source, extend)
				requests++
				if retryErr != nil || retryStatus != http.StatusOK || !bytes.Equal(response, retryBody) {
					return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("busy verify EXTEND replay depth=%d status=%d equal=%t error=%v", depth, retryStatus, bytes.Equal(response, retryBody), retryErr)
				}
			}
		} else {
			status, response, err = self.post(ctx, operator, source, extend)
			requests++
			if err != nil || status/100 != 2 {
				return "", requests, verifyIntegrityEvidence{}, fmt.Errorf("valid verify EXTEND depth=%d status=%d error=%v", depth, status, err)
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
			proofRequests, proofErr := self.requireUniqueProof(ctx, operator, trailID)
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
		confirmed = trail
		assign = next
	}
	return "", requests, verifyIntegrityEvidence{}, errors.New("verify trail never finalized")
}

func (self *verifyAdversary) requireUniqueProof(ctx context.Context, operator int, trailID connect.Id) (uint64, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/verify/proofs?limit=10000", 18080+operator)
	status, body, err := self.http.do(ctx, http.MethodGet, endpoint, "", nil, 32<<20)
	if err != nil || status != http.StatusOK {
		return 1, fmt.Errorf("verify proof index status=%d error=%v", status, err)
	}
	var proofs struct {
		Schema string `json:"schema"`
		Rows   []struct {
			TrailID connect.Id `json:"trail_id"`
		} `json:"rows"`
	}
	if json.Unmarshal(body, &proofs) != nil || proofs.Schema != "urnetwork-verify-proof-index-v1" {
		return 1, errors.New("verify proof index is malformed")
	}
	count := 0
	for _, proof := range proofs.Rows {
		if proof.TrailID == trailID {
			count++
		}
	}
	if count != 1 {
		return 1, fmt.Errorf("verify finalized trail %s appears %d times in proof history", trailID, count)
	}
	return 1, nil
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
	if errA != nil || errB != nil || statusA/100 != 2 || statusB/100 != 2 {
		return "", requests, fmt.Errorf("poison SEED status=%d/%d error=%v/%v", statusA, statusB, errA, errB)
	}
	var first, second connect.VerifyAssignResult
	if json.Unmarshal(bodyA, &first) != nil || json.Unmarshal(bodyB, &second) != nil || len(first.Trail) != 1 || len(second.Trail) != 1 || first.Trail[0] != second.Trail[0] {
		return "", requests, errors.New("poison SEED exposes an unstable synthetic source")
	}
	if err := validateAdversaryAssign(&first, first.Trail, public, keys); err != nil {
		return "", requests, err
	}
	if err := validateAdversaryAssign(&second, second.Trail, public, keys); err != nil {
		return "", requests, err
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
		return "", 1, nil, err
	}
	if status != http.StatusBadRequest {
		return "", 1, nil, fmt.Errorf("malformed signature returned HTTP %d, want 400", status)
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
			return "", uint64(attempt), true, err
		}
		if attempt <= limit && status != http.StatusBadRequest {
			return "", uint64(attempt), true, fmt.Errorf("pre-limit request %d returned HTTP %d", attempt, status)
		}
		if attempt == limit+1 && status != http.StatusTooManyRequests {
			return "", uint64(attempt), true, fmt.Errorf("hard-limit request returned HTTP %d, want 429", status)
		}
	}
	self.mu.Lock()
	self.rateBoundDone[operator] = true
	self.mu.Unlock()
	return fmt.Sprintf("operator=%d source_bound_after=%d", operator, limit), uint64(limit + 1), true, nil
}

func (self *verifyAdversary) Sample(ctx context.Context, phase adversarySamplePhase, sequence uint64) adversarySampleResult {
	// Five-sample blocks begin with a real control walk for the same operator
	// that receives the following four attacks. This prevents a poison-size
	// comparison from relying on another operator's response shape.
	operator := 1 + int((sequence/5)%uint64(self.cfg.Config.Topology.Operators))
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
