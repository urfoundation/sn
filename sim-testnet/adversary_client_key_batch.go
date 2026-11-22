// Operator pressure is budgeted in logical quota items. This is intentionally
// different from the public Rpc gate's unchanged actual Http request unit.
package main

import (
	"net/http"
	"net/url"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Only the actual plural route can expand one body into multiple reservations.
// Invalid batches never reach the live service through the campaign actor.
func adversaryOperatorRequestSlots(method, endpoint string, body []byte) (int, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return 0, err
	}
	if method != http.MethodPost || parsed.Path != "/sn/client-key/observations" {
		return 1, nil
	}
	request, err := protocol.DecodeClientKeyObservationBatchRequest(body)
	if err != nil {
		return 0, err
	}
	return len(request.Requests), nil
}
