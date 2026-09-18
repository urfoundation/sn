// One bounded observation operation retains separate request-bound signatures
// for every client. Batching changes transport ownership, never key authority.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
)

const (
	MaxClientKeyObservationBatchClients        = 512
	MaxClientKeyObservationBatchRequestBytes   = 1024 * 1024
	MaxClientKeyObservationBatchResponseBytes  = MaxClientKeyHistoryResponseBytes
	MaxClientKeyObservationBatchControlBytes   = 64 * 1024 * 1024
	MaxClientKeyObservationBatchRpcRequests    = 8
	MaxClientKeyObservationBatchRpcMethods     = 400
	ClientKeyObservationFallbackBatchClients   = 16
	MaxClientKeyObservationReservationAttempts = 2
	MaxClientKeyObservationActiveOperations    = 4
	// This is a finite planning envelope, not a provider latency promise:
	// two operators' four slots, two validators and two other Rpc owners.
	ClientKeyObservationBatchDeadlineContenders        = 2*MaxClientKeyObservationActiveOperations + 4
	ClientKeyObservationBatchDeadlineRequestsPerMinute = 40
	ClientKeyObservationBatchLocalWorkSeconds          = 36
	ClientKeyObservationBatchOperationSeconds          = MaxClientKeyObservationBatchRpcRequests*ClientKeyObservationBatchDeadlineContenders*60/ClientKeyObservationBatchDeadlineRequestsPerMinute + ClientKeyObservationBatchLocalWorkSeconds
)

// Only the explicit pre-read work refusal allows one smaller retry. This is
// transport admission information, never an authenticated source observation.
var ErrClientKeyObservationBatchWork = errors.New("client-key batch work admission refused")

// Complete ordered requests have one actual validator/native/Evm decision.
// Every client keeps its own unpredictable nonce and independently signed head.
type ClientKeyObservationBatchRequest struct {
	Requests             []ClientKeyObservationRequest `json:"requests"`
	MaximumResponseBytes uint64                        `json:"maximum_response_bytes"`
}

// Empty, duplicated or mixed-decision requests cannot acquire one shared owner.
func (self ClientKeyObservationBatchRequest) Validate() error {
	if len(self.Requests) == 0 || len(self.Requests) > MaxClientKeyObservationBatchClients || self.MaximumResponseBytes == 0 || self.MaximumResponseBytes > MaxClientKeyObservationBatchResponseBytes {
		return errors.New("client-key batch request census or response allowance is invalid")
	}
	decision := self.Requests[0]
	decision.ClientID, decision.Nonce = [16]byte{}, [32]byte{}
	for index, request := range self.Requests {
		if err := request.Validate(); err != nil {
			return err
		}
		if index > 0 && bytes.Compare(self.Requests[index-1].ClientID[:], request.ClientID[:]) >= 0 {
			return errors.New("client-key batch client census is not strictly ordered and unique")
		}
		request.ClientID, request.Nonce = [16]byte{}, [32]byte{}
		if request != decision {
			return errors.New("client-key batch members have different native or Evm decisions")
		}
	}
	return nil
}

// A canonical request has neither aliases, unknown fields nor a trailing value.
func DecodeClientKeyObservationBatchRequest(encoded []byte) (ClientKeyObservationBatchRequest, error) {
	var request ClientKeyObservationBatchRequest
	if len(encoded) == 0 || len(encoded) > MaxClientKeyObservationBatchRequestBytes {
		return request, errors.New("client-key batch request exceeds its wire allowance")
	}
	if err := decodeCanonicalClientKeyJSON(encoded, &request); err != nil {
		return ClientKeyObservationBatchRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return ClientKeyObservationBatchRequest{}, err
	}
	return request, nil
}

// Raw inner bodies remain the identical singleton capture format. No extra
// base64 layer or remarshal is needed for the existing content-addressed slot.
type ClientKeyObservationBatchResponse struct {
	Responses []json.RawMessage `json:"responses"`
}

// Count/order binding and operator signatures are separate consumer checks;
// this transport decoder only admits canonical complete bounded response bytes.
func DecodeClientKeyObservationBatchResponse(encoded []byte, maximum uint64, count uint64) (ClientKeyObservationBatchResponse, error) {
	var response ClientKeyObservationBatchResponse
	if maximum == 0 || maximum > MaxClientKeyObservationBatchResponseBytes || len(encoded) == 0 || uint64(len(encoded)) > maximum || count == 0 || count > MaxClientKeyObservationBatchClients {
		return response, errors.New("client-key batch response exceeds its independent allowance")
	}
	if err := decodeCanonicalClientKeyJSON(encoded, &response); err != nil {
		return ClientKeyObservationBatchResponse{}, err
	}
	if uint64(len(response.Responses)) != count {
		return ClientKeyObservationBatchResponse{}, errors.New("client-key batch response census is incomplete or excessive")
	}
	for _, encoded := range response.Responses {
		if _, err := DecodeClientKeyHistoryResponse(encoded, min(maximum, uint64(MaxClientKeyHistoryResponseBytes))); err != nil {
			return ClientKeyObservationBatchResponse{}, err
		}
	}
	return response, nil
}
