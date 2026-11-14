// Public authority publication retries one immutable original under a finite
// owner. The server's idempotent post reconciles an interrupted response.
package payoutroster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urnetwork/connect/v2026"
)

// Configuration and integrity failures are permanent; unavailable publication
// remains safe to retry with exactly the same retained original.
var (
	ErrTransportConfig        = errors.New("invalid authority transport configuration")
	ErrPublicationIntegrity   = errors.New("authority publication integrity failure")
	ErrPublicationRefused     = errors.New("authority publication refused")
	ErrPublicationUnavailable = errors.New("authority publication unavailable")
)

// Independent callers own independent retry budgets and cancellation. A supplied
// client contributes its transport only; redirects and cookies are never used.
type TransportSettings struct {
	ApiBase        string
	HttpClient     *http.Client
	TotalTimeout   time.Duration
	AttemptTimeout time.Duration
	RetryDelay     time.Duration
}

// A constructed transport is safe for concurrent publication. Errors expose no
// endpoint, response body, credential, or underlying network address.
type Transport struct {
	endpoint       string
	client         *http.Client
	totalTimeout   time.Duration
	attemptTimeout time.Duration
	retryDelay     time.Duration
	wait           func(context.Context, time.Duration) error
	ownedTransport *http.Transport
}

// Only authenticated https can acknowledge the exact expected receipt. A base
// path is preserved, but userinfo, selectors, fragments and redirects are refused.
func NewTransport(settings TransportSettings) (*Transport, error) {
	base, err := url.Parse(settings.ApiBase)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawFragment != "" || base.Opaque != "" || base.RawPath != "" {
		return nil, ErrTransportConfig
	}
	if settings.TotalTimeout == 0 {
		settings.TotalTimeout = 300 * time.Second
	}
	if settings.AttemptTimeout == 0 {
		settings.AttemptTimeout = 30 * time.Second
	}
	if settings.RetryDelay == 0 {
		settings.RetryDelay = time.Second
	}
	if settings.TotalTimeout < 60*time.Second || settings.TotalTimeout > 24*time.Hour || settings.AttemptTimeout <= 0 || settings.AttemptTimeout > settings.TotalTimeout || settings.RetryDelay <= 0 || settings.RetryDelay > settings.TotalTimeout {
		return nil, ErrTransportConfig
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/provider-work/v1/authorities"
	client := &http.Client{}
	if settings.HttpClient != nil {
		client.Transport = settings.HttpClient.Transport
	}
	var ownedTransport *http.Transport
	if client.Transport == nil {
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, ErrTransportConfig
		}
		ownedTransport = defaultTransport.Clone()
		client.Transport = ownedTransport
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Transport{endpoint: base.String(), client: client, totalTimeout: settings.TotalTimeout, attemptTimeout: settings.AttemptTimeout, retryDelay: settings.RetryDelay, ownedTransport: ownedTransport}, nil
}

// Only the connection pool created by this instance is released; an explicitly
// supplied transport remains owned by its caller and may serve other clients.
func (self *Transport) Close() {
	if self != nil && self.ownedTransport != nil {
		self.ownedTransport.CloseIdleConnections()
	}
}

// A private copy is verified before transport, then every attempt posts that
// exact copy. A matching receipt over verified tls is the only success condition.
func (self *Transport) Publish(ctx context.Context, raw []byte, expected common.Address) error {
	if ctx == nil || self == nil || self.client == nil {
		return ErrTransportConfig
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(raw) == 0 || len(raw) > payoutartifact.MaxWholeWorkAuthorityBytes {
		return ErrPublicationIntegrity
	}
	original := bytes.Clone(raw)
	if _, err := payoutartifact.DecodeWholeWorkAuthority(ctx, original, expected); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrPublicationIntegrity
	}
	owner, cancel := context.WithTimeout(ctx, self.totalTimeout)
	defer cancel()
	digest := sha256.Sum256(original)
	for {
		if err := owner.Err(); err != nil {
			return errors.Join(ErrPublicationUnavailable, err)
		}
		retry, err := self.attempt(owner, original, digest)
		if err == nil || !retry {
			return err
		}
		if owner.Err() != nil {
			return errors.Join(err, owner.Err())
		}
		if self.wait != nil {
			if waitErr := self.wait(owner, self.retryDelay); waitErr != nil {
				return errors.Join(err, waitErr)
			}
		} else {
			select {
			case <-owner.Done():
				return errors.Join(err, owner.Err())
			case <-connect.NewPacedReconnect(self.retryDelay).After():
			}
		}
	}
}

// Content length and exact content type match the production admission framing.
// No response body or wrapped url error is ever included in a returned error.
func (self *Transport) attempt(owner context.Context, original []byte, digest [32]byte) (bool, error) {
	ctx, cancel := context.WithTimeout(owner, self.attemptTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.endpoint, bytes.NewReader(original))
	if err != nil {
		return false, ErrTransportConfig
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := self.client.Do(request)
	if err != nil {
		var verificationError *tls.CertificateVerificationError
		var unknownAuthority x509.UnknownAuthorityError
		var hostnameError x509.HostnameError
		var certificateError x509.CertificateInvalidError
		if errors.As(err, &verificationError) || errors.As(err, &unknownAuthority) || errors.As(err, &hostnameError) || errors.As(err, &certificateError) {
			return false, ErrPublicationIntegrity
		}
		if owner.Err() != nil {
			return false, errors.Join(ErrPublicationUnavailable, owner.Err())
		}
		return true, ErrPublicationUnavailable
	}
	defer response.Body.Close()
	if response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 {
		return false, ErrPublicationIntegrity
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 && response.StatusCode <= 599 {
		return true, fmt.Errorf("%w (http status %d)", ErrPublicationUnavailable, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w (http status %d)", ErrPublicationRefused, response.StatusCode)
	}
	if response.Header.Get("Content-Type") != "application/json" || len(response.Header.Values("Content-Type")) != 1 || response.Header.Get("Content-Encoding") != "" {
		return false, ErrPublicationIntegrity
	}
	const maximumReceiptBytes = 1024
	if response.ContentLength > maximumReceiptBytes {
		return false, ErrPublicationIntegrity
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumReceiptBytes+1))
	if err != nil {
		if owner.Err() != nil {
			return false, errors.Join(ErrPublicationUnavailable, owner.Err())
		}
		return true, ErrPublicationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return owner.Err() == nil, errors.Join(ErrPublicationUnavailable, err)
	}
	var receipt struct {
		AuthorityHash [32]byte `json:"authority_hash"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > maximumReceiptBytes || decoder.Decode(&receipt) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) || receipt.AuthorityHash != digest {
		return false, ErrPublicationIntegrity
	}
	canonical, _ := json.Marshal(receipt)
	if !bytes.Equal(raw, canonical) {
		return false, ErrPublicationIntegrity
	}
	return false, nil
}
