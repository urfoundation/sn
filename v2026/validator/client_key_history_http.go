// Authenticated live captures own their exact HTTP response through bounded
// reads and the real body Close. This transport never declares signer authority.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Immutable routing and the live session getter are safe concurrently. The
// getter returns empty after the existing API owner loses authentication.
type HTTPClientKeyHistoryReader struct {
	endpoint      string
	batchEndpoint string
	client        *http.Client
	byJwt         func() string
}

// Construction never reads credentials or calls the network. Redirects cannot
// move an authenticated capture to an unapproved origin.
func NewHTTPClientKeyHistoryReader(apiURL string, byJwt func() string) (*HTTPClientKeyHistoryReader, error) {
	if byJwt == nil {
		return nil, errors.New("client-key history has no live authenticated session")
	}
	if err := validateEndpoint("client-key history api_url", apiURL, "http", "https"); err != nil {
		return nil, err
	}
	base, err := url.Parse(apiURL)
	if err != nil {
		return nil, err
	}
	return &HTTPClientKeyHistoryReader{endpoint: base.ResolveReference(&url.URL{Path: "/sn/client-key/observation"}).String(), batchEndpoint: base.ResolveReference(&url.URL{Path: "/sn/client-key/observations"}).String(), byJwt: byJwt, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// A fresh nonce comes from the validator caller before this method. It is
// echoed by an independently signed observation and verified by the consumer.
func (self *HTTPClientKeyHistoryReader) Read(ctx context.Context, observationRequest protocol.ClientKeyObservationRequest, maximum uint64) (result []byte, resultErr error) {
	if ctx == nil || self == nil || self.client == nil || self.byJwt == nil || maximum == 0 || maximum > protocol.MaxClientKeyHistoryResponseBytes {
		return nil, errors.New("client-key history HTTP owner or byte allowance is unavailable")
	}
	if err := errors.Join(ctx.Err(), observationRequest.Validate()); err != nil {
		return nil, err
	}
	client, endpoint, byJwt := self.client, self.endpoint, self.byJwt
	requestBytes, err := json.Marshal(observationRequest)
	if err != nil {
		return nil, err
	}
	args := struct {
		ClientID string `json:"client_id"`
		Request  []byte `json:"request"`
	}{ClientID: connect.Id(observationRequest.ClientID).String(), Request: requestBytes}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	credential := byJwt()
	if credential == "" || len(credential) > 16*1024 || strings.ContainsAny(credential, "\r\n") || ctx.Err() != nil {
		return nil, errors.Join(errors.New("client-key history authenticated session is unavailable"), ctx.Err())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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
		resultErr = errors.Join(resultErr, response.Body.Close(), ctx.Err())
		if byJwt() == "" {
			resultErr = errors.Join(resultErr, errors.New("client-key history session ended during capture"))
		}
		if resultErr != nil {
			result = nil
		}
	}()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client-key history returned HTTP %d", response.StatusCode)
	}
	if contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])); contentType != "application/json" {
		return nil, errors.New("client-key history response is not JSON")
	}
	if response.ContentLength > int64(maximum) {
		return nil, errors.New("client-key history response exceeds its byte allowance")
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, int64(maximum)+1))
	if err != nil || uint64(len(encoded)) > maximum {
		return nil, errors.Join(errors.New("client-key history response is incomplete or excessive"), err)
	}
	// json.Encoder adds one newline; every retained inner wrapper remains exact.
	encoded = bytes.TrimSuffix(encoded, []byte{'\n'})
	if _, err := protocol.DecodeClientKeyHistoryResponse(encoded, maximum); err != nil {
		return nil, err
	}
	return encoded, nil
}
