// Original Server receipts keep their exact canonical wire grammar. Pure
// signature verification is shared without importing the Server model package.
package protocol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"github.com/urnetwork/connect/v2026"
	"io"
)

const ProviderAttemptReceiptDomain = "urnetwork-verify-original-transition-v1\x00"

// Exact signed bytes remain authoritative; decoded fields are only indexes.
type ProviderAttemptReceipt struct {
	Body      []byte `json:"body"`
	Signature []byte `json:"signature"`
}

// Full next state retains pending exposure and each original request/response.
// Network snapshots are operator observations at assignment, not current joins.
type ProviderAttemptReceiptBody struct {
	Schema           string                       `json:"schema"`
	Scope            *ProviderAttemptReceiptScope `json:"scope,omitempty"`
	PreviousDepth    int                          `json:"previous_depth"`
	RecoveryMs       uint64                       `json:"recovery_ms"`
	Trail            *ProviderAttemptReceiptTrail `json:"trail"`
	RequestMessage   []byte                       `json:"request_message"`
	RequestSignature []byte                       `json:"request_signature"`
	ResponseJson     string                       `json:"response_json"`
}

// Bind new metadata to the selected immutable deployment/policy. A missing
// scope is explicitly unusable as subnet earning authority.
type ProviderAttemptReceiptScope struct {
	Profile       string   `json:"profile" yaml:"profile"`
	GenesisHash   [32]byte `json:"genesis_hash" yaml:"genesis_hash"`
	DeploymentId  string   `json:"deployment_id" yaml:"deployment_id"`
	DeploymentKey string   `json:"deployment_key" yaml:"deployment_key"`
	PolicyHash    [32]byte `json:"policy_hash" yaml:"policy_hash"`
	Netuid        uint64   `json:"netuid" yaml:"netuid"`
	NoId          uint64   `json:"no_id" yaml:"no_id"`
}

// Decode bounded retained bytes without promoting their source to authority.
func DecodeProviderAttemptReceipt(original *ProviderAttemptReceipt) (*ProviderAttemptReceiptBody, error) {
	if original == nil || len(original.Body) == 0 || len(original.Body) > 65536 || len(original.Signature) != ed25519.SignatureSize {
		return nil, errors.New("invalid verification original envelope")
	}
	var body ProviderAttemptReceiptBody
	decoder := json.NewDecoder(bytes.NewReader(original.Body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing verification original bytes")
	}
	if body.Schema != ProviderAttemptReceiptDomain || body.Trail == nil || body.Trail.Poison || body.PreviousDepth < 0 || body.PreviousDepth > 16 || len(body.Trail.Hops) < 1 || len(body.Trail.Hops) > 16 || len(body.RequestSignature) != ed25519.SignatureSize || len(body.RequestMessage) == 0 || body.RecoveryMs < body.Trail.ActivityMs {
		return nil, errors.New("invalid verification original body")
	}
	canonical, err := json.Marshal(&body)
	if err != nil || !bytes.Equal(canonical, original.Body) {
		return nil, errors.New("noncanonical verification original body")
	}
	return &body, nil
}

// A caller supplies its independently pinned historical key, never a key from
// the receipt itself. The receipt signature covers timing and sampling metadata.
func VerifyProviderAttemptReceiptSignature(original *ProviderAttemptReceipt, publicKey ed25519.PublicKey) bool {
	return original != nil && len(publicKey) == ed25519.PublicKeySize && ed25519.Verify(publicKey, append([]byte(ProviderAttemptReceiptDomain), original.Body...), original.Signature)
}

type ProviderAttemptReceiptHop struct {
	ClientId     connect.Id  `json:"client_id"`
	NetworkId    *connect.Id `json:"network_id,omitempty"`
	NetworkIssue string      `json:"network_issue,omitempty"`
	AssignedMs   uint64      `json:"assigned_ms"`
	ConfirmedMs  uint64      `json:"confirmed_ms"`
	AssignN      int         `json:"assign_n"`
	Seed         bool        `json:"seed,omitempty"`
	EgressIpHash [32]byte    `json:"egress_ip_hash"`
}

type ProviderAttemptReceiptTrail struct {
	TrailId     connect.Id
	ClientId    connect.Id
	Vpk         []byte
	ServerNonce []byte
	M           int
	ServerKeyId byte
	Status      string
	Poison      bool
	CreateMs    uint64
	ActivityMs  uint64
	Hops        []*ProviderAttemptReceiptHop
	Pending     *ProviderAttemptReceiptHop
}

// Require one response object; the receipt retains its exact original bytes.
func decodeProviderAttemptReceiptResponse(encoded string, response any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(response); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing original response bytes")
	}
	return nil
}

// Verify both the metadata receipt and unchanged wire domains. A server receipt
// cannot substitute for the original verifier signature or confirm a new path.
func ValidateProviderAttemptReceipt(original *ProviderAttemptReceipt, publicKey ed25519.PublicKey) (*ProviderAttemptReceiptBody, error) {
	body, err := DecodeProviderAttemptReceipt(original)
	if err != nil {
		return nil, err
	}
	invalid := func() (*ProviderAttemptReceiptBody, error) {
		return nil, errors.New("verification original signature or path mismatch")
	}
	if !VerifyProviderAttemptReceiptSignature(original, publicKey) {
		return invalid()
	}
	trail := body.Trail
	if trail.M < connect.VerifyMMin || trail.M > connect.VerifyMMax || len(trail.Hops) != body.PreviousDepth+1 || len(trail.Vpk) != ed25519.PublicKeySize || len(trail.ServerNonce) != connect.VerifyNonceSize {
		return invalid()
	}
	ids := make([]connect.Id, len(trail.Hops))
	seen := map[connect.Id]bool{}
	for index, hop := range trail.Hops {
		if hop == nil || hop.Seed != (index == 0) || hop.ConfirmedMs == 0 || (index > 0 && (hop.AssignedMs == 0 || hop.ConfirmedMs < hop.AssignedMs)) {
			return invalid()
		}
		ids[index] = connect.Id(hop.ClientId)
		if seen[ids[index]] {
			return invalid()
		}
		seen[ids[index]] = true
	}
	if !connect.VerifyVerifyMessageSignature(trail.Vpk, body.RequestMessage, body.RequestSignature) {
		return invalid()
	}
	if body.PreviousDepth == 0 {
		prefix := append([]byte(connect.VerifyCtx), connect.VerifyMsgTypeSeed)
		prefix = append(prefix, trail.Vpk...)
		if len(body.RequestMessage) != len(prefix)+connect.VerifyNonceSize+1 || !bytes.HasPrefix(body.RequestMessage, prefix) {
			return invalid()
		}
		requested := int(body.RequestMessage[len(body.RequestMessage)-1])
		if requested < connect.VerifyMMin {
			requested = connect.VerifyMMin
		}
		if requested > connect.VerifyMMax {
			requested = connect.VerifyMMax
		}
		if requested != trail.M {
			return invalid()
		}
	} else {
		message, err := connect.BuildVerifyExtendMessage(connect.Id(trail.TrailId), trail.ServerNonce, trail.Vpk, byte(trail.M), ids)
		if err != nil || !bytes.Equal(message, body.RequestMessage) {
			return invalid()
		}
	}
	if trail.Status == "expired" {
		if trail.Pending != nil || body.ResponseJson != "" || body.PreviousDepth == 0 {
			return invalid()
		}
		return body, nil
	}
	var response struct {
		Assign *connect.VerifyAssignResult `json:"assign,omitempty"`
		Final  *connect.VerifyFinalResult  `json:"final,omitempty"`
	}
	if err := decodeProviderAttemptReceiptResponse(body.ResponseJson, &response); err != nil {
		return nil, err
	}
	if trail.Status == "active" {
		assign := response.Assign
		if assign == nil || response.Final != nil || trail.Pending == nil || len(ids) >= trail.M || trail.Pending.AssignedMs == 0 || trail.Pending.AssignN < 1 || seen[connect.Id(trail.Pending.ClientId)] || assign.TrailId != connect.Id(trail.TrailId) || !bytes.Equal(assign.ServerNonce, trail.ServerNonce) || assign.M != trail.M || assign.ServerKeyId != trail.ServerKeyId || assign.NextHop != connect.Id(trail.Pending.ClientId) || len(assign.Trail) != len(ids) {
			return invalid()
		}
		for index, id := range ids {
			if assign.Trail[index] != id {
				return invalid()
			}
		}
		message, err := connect.BuildVerifyAssignMessage(trail.ServerKeyId, connect.Id(trail.TrailId), trail.ServerNonce, trail.Vpk, byte(trail.M), append(ids, assign.NextHop))
		if err != nil || !connect.VerifyVerifyMessageSignature(publicKey, message, assign.AssignSig) {
			return invalid()
		}
	} else if trail.Status == "complete" {
		if response.Final == nil || response.Assign != nil || response.Final.Proof == nil || trail.Pending != nil || len(ids) != trail.M {
			return invalid()
		}
		proof := response.Final.Proof
		if response.Final.Status != connect.VerifyStatusComplete || proof.Header.TrailId != connect.Id(trail.TrailId) || !bytes.Equal(proof.Header.ServerNonce, trail.ServerNonce) || !bytes.Equal(proof.Header.Vpk, trail.Vpk) || proof.Header.M != trail.M || proof.ServerKeyId != trail.ServerKeyId || proof.Coverage != uint64(trail.M-1) || !bytes.Equal(proof.VerifierSig, body.RequestSignature) || len(proof.Hops) != len(ids) {
			return invalid()
		}
		for index, hop := range proof.Hops {
			if hop.ClientId != ids[index] || hop.TimeMs != trail.Hops[index].ConfirmedMs || hop.EgressIpHash != trail.Hops[index].EgressIpHash {
				return invalid()
			}
		}
		message, err := connect.BuildVerifyFinalMessage(trail.ServerKeyId, connect.Id(trail.TrailId), trail.ServerNonce, trail.Vpk, byte(trail.M), proof.Hops)
		if err != nil || !connect.VerifyVerifyMessageSignature(publicKey, message, proof.FinalSig) {
			return invalid()
		}
	} else {
		return invalid()
	}
	return body, nil
}
