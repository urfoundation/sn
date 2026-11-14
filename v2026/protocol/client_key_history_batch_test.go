// Canonical transport census is finite and separate from signer authority.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

// All values describe one concrete decision but each client owns a nonce.
func clientKeyHistoryBatchTestRequest(count int) ClientKeyObservationBatchRequest {
	result := ClientKeyObservationBatchRequest{MaximumResponseBytes: MaxClientKeyObservationBatchResponseBytes}
	for index := 0; index < count; index++ {
		request := ClientKeyObservationRequest{ValidatorHotkey: [32]byte{1}, NativeBlock: 101, NativeHash: [32]byte{2}, NativeEpoch: 3, DecisionBoundary: ClientKeyEffectiveBoundary{Epoch: 4, Block: 100, Hash: [32]byte{5}}, Nonce: [32]byte{6}}
		binary.BigEndian.PutUint64(request.ClientID[8:], uint64(index+1))
		binary.BigEndian.PutUint64(request.Nonce[24:], uint64(index+1))
		result.Requests = append(result.Requests, request)
	}
	return result
}

// The full supported census fits the explicit wire budget without aliases.
func TestClientKeyHistoryBatchMaximumCanonicalCensus(t *testing.T) {
	request := clientKeyHistoryBatchTestRequest(MaxClientKeyObservationBatchClients)
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeClientKeyObservationBatchRequest(encoded)
	if err != nil || len(decoded.Requests) != MaxClientKeyObservationBatchClients || len(encoded) > MaxClientKeyObservationBatchRequestBytes {
		t.Fatalf("full census: bytes=%d error=%v", len(encoded), err)
	}
	if MaxClientKeyObservationReservationAttempts != 2 || ClientKeyObservationFallbackBatchClients <= 0 || ClientKeyObservationFallbackBatchClients >= MaxClientKeyObservationBatchClients {
		t.Fatal("finite retry contract changed")
	}
}

// A single shared owner cannot erase per-client identity or merge decisions.
func TestClientKeyHistoryBatchRejectsDuplicateUnorderedMixedAndExcessiveRequests(t *testing.T) {
	for _, kind := range []string{"duplicate", "unordered", "validator", "native", "evm", "nonce", "excessive", "empty", "response"} {
		request := clientKeyHistoryBatchTestRequest(2)
		switch kind {
		case "duplicate":
			request.Requests[1].ClientID = request.Requests[0].ClientID
		case "unordered":
			request.Requests[0], request.Requests[1] = request.Requests[1], request.Requests[0]
		case "validator":
			request.Requests[1].ValidatorHotkey[0]++
		case "native":
			request.Requests[1].NativeHash[0]++
		case "evm":
			request.Requests[1].DecisionBoundary.Hash[0]++
		case "nonce":
			request.Requests[1].Nonce = [32]byte{}
		case "excessive":
			request = clientKeyHistoryBatchTestRequest(MaxClientKeyObservationBatchClients + 1)
		case "empty":
			request.Requests = nil
		case "response":
			request.MaximumResponseBytes++
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if decoded, err := DecodeClientKeyObservationBatchRequest(encoded); err == nil || len(decoded.Requests) != 0 {
			t.Fatalf("%s was admitted: %v", kind, err)
		}
	}
}

// Outer canonical framing preserves exact inner singleton response bodies.
func TestClientKeyHistoryBatchResponsePreservesExactBodiesAndCount(t *testing.T) {
	inner, err := json.Marshal(ClientKeyHistoryResponse{History: [][]byte{[]byte("retained-wrapper")}, Observation: []byte("retained-observation")})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(ClientKeyObservationBatchResponse{Responses: []json.RawMessage{inner, inner}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeClientKeyObservationBatchResponse(encoded, uint64(len(encoded)), 2)
	if err != nil || string(decoded.Responses[0]) != string(inner) {
		t.Fatal("canonical inner bytes changed", err)
	}
	for _, count := range []uint64{0, 1, 3, MaxClientKeyObservationBatchClients + 1} {
		if _, err := DecodeClientKeyObservationBatchResponse(encoded, uint64(len(encoded)), count); err == nil {
			t.Fatalf("wrong count %d accepted", count)
		}
	}
	if _, err := DecodeClientKeyObservationBatchResponse(encoded, uint64(len(encoded)-1), 2); err == nil {
		t.Fatal("oversized complete response accepted")
	}
}
