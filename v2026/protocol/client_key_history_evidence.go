// The existing public evidence envelope carries exact operator statements.
// Its publisher signature is checked as transport integrity; only the inner
// root signature, joined to independently pinned chain state, is authority.
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const (
	ClientKeyRegistrationEvidenceKind = "client-key-registration-v1"
	ClientKeyObservationEvidenceKind  = "client-key-observation-v1"
	MaxClientKeyEvidenceBytes         = 16 * 1024
	MaxClientKeyHistoryRegistrations  = 1024
	MaxClientKeyHistoryResponseBytes  = 8 * 1024 * 1024
)

// This is the exact server/startifact evidence wire, including optional run ID.
// It lives here to keep validator dependencies independent of the server.
type ClientKeyEvidenceEnvelope struct {
	Schema       string          `json:"schema"`
	DeploymentID string          `json:"deployment_id"`
	ChainID      uint64          `json:"chain_id"`
	GenesisHash  string          `json:"genesis_hash"`
	Netuid       uint16          `json:"netuid"`
	Kind         string          `json:"kind"`
	RunID        string          `json:"run_id,omitempty"`
	CreatedAt    string          `json:"created_at"`
	Payload      json.RawMessage `json:"payload"`
	Signer       common.Address  `json:"signer"`
	ContentHash  string          `json:"content_hash"`
	Signature    string          `json:"signature"`
}

// Body limits must precede decoding. Canonical re-encoding rejects duplicate
// fields and alternate spellings, including signatures and domain hashes.
func DecodeClientKeyEvidence(encoded []byte, domain ClientKeyHistoryDomain, kind string) (ClientKeyEvidenceEnvelope, error) {
	var envelope ClientKeyEvidenceEnvelope
	if err := domain.Validate(); err != nil {
		return envelope, err
	}
	if len(encoded) == 0 || len(encoded) > MaxClientKeyEvidenceBytes || kind != ClientKeyRegistrationEvidenceKind && kind != ClientKeyObservationEvidenceKind {
		return envelope, errors.New("client-key evidence kind or fixed byte allowance is invalid")
	}
	if err := decodeCanonicalClientKeyJSON(encoded, &envelope); err != nil {
		return ClientKeyEvidenceEnvelope{}, err
	}
	if envelope.Schema != "urnetwork-release-evidence-v1" || envelope.RunID != "" || envelope.Kind != kind || envelope.ChainID != domain.ChainID || envelope.Netuid != domain.Netuid || envelope.GenesisHash != fmt.Sprintf("0x%x", domain.GenesisHash) || sha256.Sum256([]byte(envelope.DeploymentID)) != domain.DeploymentIDHash || strings.TrimSpace(envelope.CreatedAt) == "" || len(envelope.Payload) == 0 || len(envelope.Payload) > MaxClientKeyStatementBytes {
		return ClientKeyEvidenceEnvelope{}, errors.New("client-key evidence differs from its independent deployment or kind")
	}
	unsigned := envelope
	unsigned.ContentHash, unsigned.Signature = "", ""
	data, err := json.Marshal(unsigned)
	if err != nil {
		return ClientKeyEvidenceEnvelope{}, err
	}
	hash := sha256.Sum256(data)
	signatureBytes, err := hex.DecodeString(strings.TrimPrefix(envelope.Signature, "0x"))
	if err != nil || len(signatureBytes) != 65 || envelope.Signature != "0x"+hex.EncodeToString(signatureBytes) || envelope.ContentHash != "sha256:"+hex.EncodeToString(hash[:]) {
		return ClientKeyEvidenceEnvelope{}, errors.Join(errors.New("client-key evidence full publisher identity differs"), err)
	}
	if err := verifyClientKeyStatementSignature(hash, [65]byte(signatureBytes), envelope.Signer); err != nil {
		return ClientKeyEvidenceEnvelope{}, err
	}
	return envelope, nil
}

// Authenticated capture owns every complete wrapper; no history listing or
// unsigned current-key response can substitute for this contiguous census.
type ClientKeyHistoryResponse struct {
	History     [][]byte `json:"history"`
	Observation []byte   `json:"observation"`
}

// Allows the HTTP encoder's final newline only at the transport boundary;
// callers retain a canonical body for content identity and crash replay.
func DecodeClientKeyHistoryResponse(encoded []byte, maximum uint64) (ClientKeyHistoryResponse, error) {
	var response ClientKeyHistoryResponse
	if maximum == 0 || maximum > MaxClientKeyHistoryResponseBytes || len(encoded) == 0 || uint64(len(encoded)) > maximum {
		return response, errors.New("client-key history response exceeds its independent byte allowance")
	}
	if err := decodeCanonicalClientKeyJSON(encoded, &response); err != nil {
		return ClientKeyHistoryResponse{}, err
	}
	if len(response.History) == 0 || len(response.History) > MaxClientKeyHistoryRegistrations || len(response.Observation) == 0 || len(response.Observation) > MaxClientKeyEvidenceBytes {
		return ClientKeyHistoryResponse{}, errors.New("client-key history response census is incomplete or excessive")
	}
	for _, registration := range response.History {
		if len(registration) == 0 || len(registration) > MaxClientKeyEvidenceBytes {
			return ClientKeyHistoryResponse{}, errors.New("client-key history registration exceeds its fixed wrapper bound")
		}
	}
	return response, nil
}

// One canonical value has neither unknown fields, aliases nor trailing JSON.
func decodeCanonicalClientKeyJSON(encoded []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("client-key evidence contains trailing JSON")
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return errors.Join(errors.New("client-key evidence bytes are not canonical"), err)
	}
	return nil
}
