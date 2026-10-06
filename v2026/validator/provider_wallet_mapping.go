// Original wallet histories are fetched under one read owner, then checked
// against an independent exact-window head. HTTP never chooses that authority.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Fixed response and request lifetimes are independent of remote metadata.
type HttpWalletMappingReader struct {
	endpoint string
	// the network consent history endpoint
	networkEndpoint string
	client          *http.Client
	wait            func(context.Context, time.Duration) error
}

// Redirects cannot change the selected original evidence endpoint.
func NewHttpWalletMappingReader(apiUrl string) (*HttpWalletMappingReader, error) {
	if err := validateEndpoint("wallet mapping api_url", apiUrl, "http", "https"); err != nil {
		return nil, err
	}
	base, err := url.Parse(apiUrl)
	if err != nil {
		return nil, err
	}
	return &HttpWalletMappingReader{
		endpoint:        base.ResolveReference(&url.URL{Path: "/sn/wallet/consent/history"}).String(),
		networkEndpoint: base.ResolveReference(&url.URL{Path: "/sn/wallet/network-consent/history"}).String(),
		client:          &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		wait:            waitReleaseSnapshotRetry,
	}, nil
}

// Only a selected complete original head can authorize acquisition. A missing
// head remains unknown before any request; every attempt owns its body close.
func (self *HttpWalletMappingReader) Read(ctx context.Context, expected protocol.WalletMappingHistoryExpectation) ([]protocol.WalletMappingConsent, *protocol.VerifiedWalletMapping, error) {
	return self.ReadBounded(ctx, expected, uint64(protocol.MaxWalletMappingHistory)*protocol.MaxWalletMappingConsentBytes+1024)
}

// A containing multi-provider owner reserves its remaining original-byte
// allowance before the request; remote length cannot multiply that allowance.
// A complete chain with no consent effective at the epoch returns its
// originals with protocol.ErrWalletMappingNotEffective, so the caller can
// retain them as the evidence for a fallback to the network consent.
func (self *HttpWalletMappingReader) ReadBounded(ctx context.Context, expected protocol.WalletMappingHistoryExpectation, maximumBytes uint64) ([]protocol.WalletMappingConsent, *protocol.VerifiedWalletMapping, error) {
	if ctx == nil || self == nil || self.client == nil || self.wait == nil || expected.Generation == 0 || expected.Generation > protocol.MaxWalletMappingHistory || expected.HeadHash == ([32]byte{}) || expected.ClientId == ([16]byte{}) {
		return nil, nil, protocol.ErrWalletMappingUnavailable
	}
	if err := errors.Join(evidenceReadContextError(ctx), expected.Domain.Validate()); err != nil {
		return nil, nil, err
	}
	owner, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	requestBody, err := json.Marshal(struct {
		Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
		ClientId   string                          `json:"client_id"`
		HeadHash   [32]byte                        `json:"head_hash"`
		Generation uint64                          `json:"generation"`
	}{Domain: expected.Domain, ClientId: connect.Id(expected.ClientId).String(), HeadHash: expected.HeadHash, Generation: expected.Generation})
	if err != nil {
		return nil, nil, err
	}
	maximum := int64(expected.Generation)*protocol.MaxWalletMappingConsentBytes + 1024
	if maximumBytes == 0 {
		return nil, nil, protocol.ErrWalletMappingCapacity
	}
	if maximumBytes < uint64(maximum) {
		maximum = int64(maximumBytes)
	}
	var lastErr error
	for {
		if err := evidenceReadContextError(owner); err != nil {
			return nil, nil, errors.Join(lastErr, err)
		}
		originals, err := self.read(owner, self.endpoint, requestBody, maximum)
		if err == nil {
			verified, err := protocol.VerifyWalletMappingHistory(owner, originals, expected)
			if contextErr := evidenceReadContextError(owner); contextErr != nil {
				return nil, nil, errors.Join(err, contextErr)
			}
			if errors.Is(err, protocol.ErrWalletMappingNotEffective) {
				return originals, nil, err
			}
			if err != nil {
				return nil, nil, err
			}
			return originals, verified, nil
		}
		lastErr = err
		if ownerErr := evidenceReadContextError(owner); ownerErr != nil || !retryableClientKeyObservationHttpError(err) {
			return nil, nil, errors.Join(err, ownerErr)
		}
		if waitErr := self.wait(owner, 5*time.Second); waitErr != nil {
			return nil, nil, errors.Join(err, waitErr, evidenceReadContextError(owner))
		}
	}
}

// The network consent history through the independently pinned network head,
// with the same bounds, retries and not-effective outcome as ReadBounded.
func (self *HttpWalletMappingReader) ReadNetworkBounded(ctx context.Context, expected protocol.NetworkWalletMappingHistoryExpectation, maximumBytes uint64) ([]protocol.WalletMappingConsent, *protocol.VerifiedNetworkWalletMapping, error) {
	if ctx == nil || self == nil || self.client == nil || self.wait == nil || expected.Generation == 0 || expected.Generation > protocol.MaxWalletMappingHistory || expected.HeadHash == ([32]byte{}) || expected.NetworkId == ([16]byte{}) {
		return nil, nil, protocol.ErrWalletMappingUnavailable
	}
	if err := errors.Join(evidenceReadContextError(ctx), expected.Domain.Validate()); err != nil {
		return nil, nil, err
	}
	owner, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	requestBody, err := json.Marshal(struct {
		Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
		NetworkId  string                          `json:"network_id"`
		HeadHash   [32]byte                        `json:"head_hash"`
		Generation uint64                          `json:"generation"`
	}{Domain: expected.Domain, NetworkId: connect.Id(expected.NetworkId).String(), HeadHash: expected.HeadHash, Generation: expected.Generation})
	if err != nil {
		return nil, nil, err
	}
	maximum := int64(expected.Generation)*protocol.MaxWalletMappingConsentBytes + 1024
	if maximumBytes == 0 {
		return nil, nil, protocol.ErrWalletMappingCapacity
	}
	if maximumBytes < uint64(maximum) {
		maximum = int64(maximumBytes)
	}
	var lastErr error
	for {
		if err := evidenceReadContextError(owner); err != nil {
			return nil, nil, errors.Join(lastErr, err)
		}
		originals, err := self.read(owner, self.networkEndpoint, requestBody, maximum)
		if err == nil {
			verified, err := protocol.VerifyNetworkWalletMappingHistory(owner, originals, expected)
			if contextErr := evidenceReadContextError(owner); contextErr != nil {
				return nil, nil, errors.Join(err, contextErr)
			}
			if errors.Is(err, protocol.ErrWalletMappingNotEffective) {
				return originals, nil, err
			}
			if err != nil {
				return nil, nil, err
			}
			return originals, verified, nil
		}
		lastErr = err
		if ownerErr := evidenceReadContextError(owner); ownerErr != nil || !retryableClientKeyObservationHttpError(err) {
			return nil, nil, errors.Join(err, ownerErr)
		}
		if waitErr := self.wait(owner, 5*time.Second); waitErr != nil {
			return nil, nil, errors.Join(err, waitErr, evidenceReadContextError(owner))
		}
	}
}

// A refused or partial response cannot escape before the actual descriptor
// close and owner cancellation check; retries always reuse identical input.
func (self *HttpWalletMappingReader) read(ctx context.Context, endpoint string, body []byte, maximum int64) (originals []protocol.WalletMappingConsent, resultErr error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if err := evidenceReadContextError(ctx); err != nil {
		return nil, err
	}
	response, err := self.client.Do(request)
	if err != nil {
		return nil, errors.Join(err, evidenceReadContextError(ctx))
	}
	defer func() {
		resultErr = errors.Join(resultErr, response.Body.Close(), evidenceReadContextError(ctx))
		if resultErr != nil {
			originals = nil
		}
	}()
	if response.StatusCode != http.StatusOK {
		return nil, &clientKeyObservationHttpStatusError{status: response.StatusCode, operation: "wallet mapping"}
	}
	if response.ContentLength > maximum {
		return nil, protocol.ErrWalletMappingCapacity
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maximum {
		return nil, protocol.ErrWalletMappingCapacity
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, errors.Join(protocol.ErrWalletMappingIntegrity, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result struct {
		Originals []protocol.WalletMappingConsent `json:"originals"`
	}
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.Join(protocol.ErrWalletMappingIntegrity, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, protocol.ErrWalletMappingIntegrity
	}
	return result.Originals, nil
}

// Release idle transport connections after the containing evidence owner joins.
func (self *HttpWalletMappingReader) CloseIdleConnections() {
	if self != nil && self.client != nil {
		self.client.CloseIdleConnections()
	}
}
