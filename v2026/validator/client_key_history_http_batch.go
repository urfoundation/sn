// One ordered live census uses the plural authenticated endpoint. A refusal
// before chain work permits only one bounded smaller retry per logical client.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// The operation preserves the same nonces through an admission-only fallback.
// Signature, identity, transport, quota and partial-response failures never retry.
func (self *HTTPClientKeyHistoryReader) ReadBatch(ctx context.Context, requests []protocol.ClientKeyObservationRequest, maximum uint64) (result [][]byte, resultErr error) {
	if ctx == nil || self == nil || self.client == nil || self.byJwt == nil || self.batchEndpoint == "" {
		return nil, errors.New("client-key batch has no actual authenticated Http owner")
	}
	owned := protocol.ClientKeyObservationBatchRequest{Requests: slices.Clone(requests), MaximumResponseBytes: maximum}
	if err := errors.Join(ctx.Err(), owned.Validate()); err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	result, err := self.readBatchAttempt(ctx, owned)
	if !errors.Is(err, protocol.ErrClientKeyObservationBatchWork) || len(owned.Requests) <= protocol.ClientKeyObservationFallbackBatchClients {
		return result, err
	}
	// There is exactly one additional reservation per member, regardless of
	// how many disjoint smaller Http envelopes carry the refused census.
	for attempt := 1; attempt < protocol.MaxClientKeyObservationReservationAttempts; attempt++ {
		result = make([][]byte, 0, len(owned.Requests))
		remaining := maximum
		for start := 0; start < len(owned.Requests); start += protocol.ClientKeyObservationFallbackBatchClients {
			end := min(start+protocol.ClientKeyObservationFallbackBatchClients, len(owned.Requests))
			// Count the actual canonical envelope punctuation and encoder's
			// optional final newline across all smaller transport responses.
			overhead := uint64(len("{\"responses\":[]}")) + uint64(end-start)
			if remaining <= overhead {
				return nil, errors.New("client-key batch fallback exhausted its complete response allowance")
			}
			response, err := self.readBatchAttempt(ctx, protocol.ClientKeyObservationBatchRequest{Requests: owned.Requests[start:end], MaximumResponseBytes: remaining})
			if err != nil {
				return nil, err
			}
			remaining -= overhead
			for _, encoded := range response {
				if uint64(len(encoded)) > remaining {
					return nil, errors.New("client-key fallback response exceeds the original complete allowance")
				}
				remaining -= uint64(len(encoded))
				result = append(result, encoded)
			}
		}
		return result, nil
	}
	return nil, protocol.ErrClientKeyObservationBatchWork
}

// Body Close and the live session remain part of the result's ownership. A
// late failure clears every response, including a seemingly complete prefix.
func (self *HTTPClientKeyHistoryReader) readBatchAttempt(ctx context.Context, batch protocol.ClientKeyObservationBatchRequest) (result [][]byte, resultErr error) {
	if err := errors.Join(ctx.Err(), batch.Validate()); err != nil {
		return nil, err
	}
	// Copy the concrete client so the original singleton remains exactly30s.
	// The caller's earlier deadline/cancellation still wins over this ceiling.
	ownedClient := *self.client
	ownedClient.Timeout = time.Duration(protocol.ClientKeyObservationBatchOperationSeconds) * time.Second
	client, endpoint, byJwt := &ownedClient, self.batchEndpoint, self.byJwt
	encoded, err := json.Marshal(batch)
	if err != nil || len(encoded) > protocol.MaxClientKeyObservationBatchRequestBytes {
		return nil, errors.Join(errors.New("client-key batch request exceeds its wire allowance"), err)
	}
	credential := byJwt()
	if credential == "" || len(credential) > 16*1024 || strings.ContainsAny(credential, "\r\n") || ctx.Err() != nil {
		return nil, errors.Join(errors.New("client-key batch session is unavailable"), ctx.Err())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	defer func() {
		lateErr := errors.Join(response.Body.Close(), ctx.Err())
		if byJwt() == "" {
			lateErr = errors.Join(lateErr, errors.New("client-key batch session ended during capture"))
		}
		if lateErr != nil && errors.Is(resultErr, protocol.ErrClientKeyObservationBatchWork) {
			resultErr = errors.New("client-key batch admission response lost its owner: " + lateErr.Error())
		} else {
			resultErr = errors.Join(resultErr, lateErr)
		}
		if resultErr != nil {
			result = nil
		}
	}()
	if response.StatusCode == http.StatusRequestEntityTooLarge && response.Header.Get("X-Ur-Client-Key-Batch-Admission") == "work" {
		return nil, protocol.ErrClientKeyObservationBatchWork
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client-key batch returned Http %d", response.StatusCode)
	}
	if contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])); contentType != "application/json" || len(response.Header.Values("Content-Type")) != 1 {
		return nil, errors.New("client-key batch response is not unambiguous Json")
	}
	if response.ContentLength > int64(batch.MaximumResponseBytes) {
		return nil, errors.New("client-key batch response exceeds its wire allowance")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(batch.MaximumResponseBytes)+1))
	if err != nil || uint64(len(body)) > batch.MaximumResponseBytes {
		return nil, errors.Join(errors.New("client-key batch response is incomplete or excessive"), err)
	}
	body = bytes.TrimSuffix(body, []byte{'\n'})
	decoded, err := protocol.DecodeClientKeyObservationBatchResponse(body, batch.MaximumResponseBytes, uint64(len(batch.Requests)))
	if err != nil {
		return nil, err
	}
	result = make([][]byte, len(decoded.Responses))
	for index, value := range decoded.Responses {
		result[index] = value
	}
	return result, nil
}
