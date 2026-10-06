// Original wallet histories are fetched under one read owner, then checked
// against an independent exact-window head. HTTP never chooses that authority.
// Every chain kind (provider, network, hotkey delegation and global hotkey
// consent) shares the same bounds, retries and not-effective outcome.
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
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urnetwork/connect/v2026"
)

// Fixed response and request lifetimes are independent of remote metadata.
type HttpWalletMappingReader struct {
	endpoint string
	// the network consent history endpoint
	networkEndpoint string
	// the hotkey delegation and global hotkey consent history endpoints
	hotkeyDelegationEndpoint string
	hotkeyConsentEndpoint    string
	client                   *http.Client
	wait                     func(context.Context, time.Duration) error
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
		endpoint:                 base.ResolveReference(&url.URL{Path: "/sn/wallet/consent/history"}).String(),
		networkEndpoint:          base.ResolveReference(&url.URL{Path: "/sn/wallet/network-consent/history"}).String(),
		hotkeyDelegationEndpoint: base.ResolveReference(&url.URL{Path: "/sn/wallet/hotkey-delegation/history"}).String(),
		hotkeyConsentEndpoint:    base.ResolveReference(&url.URL{Path: "/sn/wallet/hotkey-consent/history"}).String(),
		client:                   &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		wait:                     waitReleaseSnapshotRetry,
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
	request := struct {
		Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
		ClientId   string                          `json:"client_id"`
		HeadHash   [32]byte                        `json:"head_hash"`
		Generation uint64                          `json:"generation"`
	}{Domain: expected.Domain, ClientId: connect.Id(expected.ClientId).String(), HeadHash: expected.HeadHash, Generation: expected.Generation}
	return readWalletMappingHistoryBounded(ctx, self, self.endpoint, request, expected.Generation, maximumBytes, func(owner context.Context, originals []protocol.WalletMappingConsent) (*protocol.VerifiedWalletMapping, error) {
		return protocol.VerifyWalletMappingHistory(owner, originals, expected)
	})
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
	request := struct {
		Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
		NetworkId  string                          `json:"network_id"`
		HeadHash   [32]byte                        `json:"head_hash"`
		Generation uint64                          `json:"generation"`
	}{Domain: expected.Domain, NetworkId: connect.Id(expected.NetworkId).String(), HeadHash: expected.HeadHash, Generation: expected.Generation}
	return readWalletMappingHistoryBounded(ctx, self, self.networkEndpoint, request, expected.Generation, maximumBytes, func(owner context.Context, originals []protocol.WalletMappingConsent) (*protocol.VerifiedNetworkWalletMapping, error) {
		return protocol.VerifyNetworkWalletMappingHistory(owner, originals, expected)
	})
}

// The hotkey delegation history through the independently pinned delegation
// head, with the same bounds, retries and not-effective outcome as
// ReadNetworkBounded. The selected delegation names the global consent head
// that ReadHotkeyConsentBounded reads next.
func (self *HttpWalletMappingReader) ReadHotkeyDelegationBounded(ctx context.Context, expected protocol.HotkeyNetworkDelegationHistoryExpectation, maximumBytes uint64) ([]protocol.WalletMappingConsent, *protocol.VerifiedHotkeyNetworkDelegation, error) {
	if ctx == nil || self == nil || self.client == nil || self.wait == nil || expected.Generation == 0 || expected.Generation > protocol.MaxWalletMappingHistory || expected.HeadHash == ([32]byte{}) || expected.NetworkId == ([16]byte{}) {
		return nil, nil, protocol.ErrWalletMappingUnavailable
	}
	if err := errors.Join(evidenceReadContextError(ctx), expected.Domain.Validate()); err != nil {
		return nil, nil, err
	}
	request := struct {
		Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
		NetworkId  string                          `json:"network_id"`
		HeadHash   [32]byte                        `json:"head_hash"`
		Generation uint64                          `json:"generation"`
	}{Domain: expected.Domain, NetworkId: connect.Id(expected.NetworkId).String(), HeadHash: expected.HeadHash, Generation: expected.Generation}
	return readWalletMappingHistoryBounded(ctx, self, self.hotkeyDelegationEndpoint, request, expected.Generation, maximumBytes, func(owner context.Context, originals []protocol.WalletMappingConsent) (*protocol.VerifiedHotkeyNetworkDelegation, error) {
		return protocol.VerifyHotkeyNetworkDelegationHistory(owner, originals, expected)
	})
}

// The global hotkey consent chain from generation 1 through the head that a
// delegation names, with the same bounds, retries and not-effective outcome.
// The operator indexes the chain by hotkey address alone; the expected subnet,
// hotkey and head still select and verify every original.
func (self *HttpWalletMappingReader) ReadHotkeyConsentBounded(ctx context.Context, expected protocol.HotkeyWalletMappingHistoryExpectation, maximumBytes uint64) ([]protocol.HotkeyWalletMappingConsent, *protocol.VerifiedHotkeyWalletMapping, error) {
	if ctx == nil || self == nil || self.client == nil || self.wait == nil || expected.Generation == 0 || expected.Generation > protocol.MaxWalletMappingHistory || expected.HeadHash == ([32]byte{}) || expected.Hotkey == ([32]byte{}) {
		return nil, nil, protocol.ErrWalletMappingUnavailable
	}
	if err := errors.Join(evidenceReadContextError(ctx), expected.Subnet.Validate()); err != nil {
		return nil, nil, err
	}
	hotkeySs58, err := ss58.Encode(expected.Hotkey, ss58.BittensorPrefix)
	if err != nil {
		return nil, nil, errors.Join(protocol.ErrWalletMappingIntegrity, err)
	}
	request := struct {
		HotkeySs58 string   `json:"hotkey_ss58"`
		HeadHash   [32]byte `json:"head_hash"`
		Generation uint64   `json:"generation"`
	}{HotkeySs58: hotkeySs58, HeadHash: expected.HeadHash, Generation: expected.Generation}
	return readWalletMappingHistoryBounded(ctx, self, self.hotkeyConsentEndpoint, request, expected.Generation, maximumBytes, func(owner context.Context, originals []protocol.HotkeyWalletMappingConsent) (*protocol.VerifiedHotkeyWalletMapping, error) {
		return protocol.VerifyHotkeyWalletMappingHistory(owner, originals, expected)
	})
}

// One read owner for one independently pinned head. Every attempt sends the
// identical request; the response reserve is the pinned generation count of
// maximal originals, capped by the caller's remaining allowance. A complete
// chain with no consent effective at the epoch returns its originals with
// protocol.ErrWalletMappingNotEffective, so the caller can retain them as the
// evidence for a fallback to the next mode.
func readWalletMappingHistoryBounded[T any, V any](ctx context.Context, self *HttpWalletMappingReader, endpoint string, request any, generation uint64, maximumBytes uint64, verify func(context.Context, []T) (*V, error)) ([]T, *V, error) {
	owner, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, nil, err
	}
	maximum := int64(generation)*protocol.MaxWalletMappingConsentBytes + 1024
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
		originals, err := readWalletMappingOriginals[T](owner, self.client, endpoint, requestBody, maximum)
		if err == nil {
			verified, err := verify(owner, originals)
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
func readWalletMappingOriginals[T any](ctx context.Context, client *http.Client, endpoint string, body []byte, maximum int64) (originals []T, resultErr error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if err := evidenceReadContextError(ctx); err != nil {
		return nil, err
	}
	response, err := client.Do(request)
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
		Originals []T `json:"originals"`
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
