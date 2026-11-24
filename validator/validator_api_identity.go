package validator

// Every request this validator makes to an operator's api carries its client
// bearer and, when it holds one, its SN25 validator rate class token (owner
// decision 2026-11-24), so the operator's lb attributes the request to the
// validator and charges an admitted validator's requests to the validator
// rate class. The api mints the class token (POST /sn/validator-class) on a
// VPK class consent while the reserved upload admission admits the
// activation; it lives at most five minutes and is refreshed before it ends.
//
// An identity is installed per api origin once the operator runtime has its
// api session, and every api client's transport is wrapped by
// validatorAPITransport, which adds the headers only to requests for an
// installed origin's host. Requests to anything else (chain RPC, drand, the
// trail /verify requests through provider tunnels) are untouched.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urfoundation/sn/protocol"
)

const (
	// a class token is refreshed when less than this is left of it
	validatorClassRefreshMargin = 2 * time.Minute
	// after a failed mint the next waits this long, doubling up to the maximum
	validatorClassRetryInterval        = 30 * time.Second
	maximumValidatorClassRetryInterval = 4 * time.Minute
	validatorClassMintTimeout          = 10 * time.Second
	// a request with no live token waits at most this long for a mint
	validatorClassMintWait = 2 * time.Second
	// a class token lives at most this long, as the api mints it
	validatorClassMaximumLifetime   = 5*time.Minute + 30*time.Second
	maximumValidatorClassTokenBytes = 1024
	validatorClassMintPath          = "/sn/validator-class"
)

// One api origin's identity: the client bearer, and the rate class tokens when
// this validator has an activation.
type validatorAPIIdentity struct {
	// the live api session's credential, or "" when it has none now
	credential func() string
	// nil without an activation
	class *validatorClassTokens
}

// The installed identities by lowercase api host. Copied on write. Two
// runtimes may name the same api origin; their requests can't be told apart
// by host, so such a host gets no identity until one of them is removed.
var validatorAPIIdentities atomic.Pointer[map[string][]*validatorAPIIdentity]
var validatorAPIIdentitiesLock sync.Mutex

// Installs identity for the api at origin until the returned func runs.
func installValidatorAPIIdentity(origin string, identity *validatorAPIIdentity) (func(), error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || identity == nil || identity.credential == nil {
		return nil, errors.New("validator api identity needs an api origin and a credential")
	}
	host := strings.ToLower(parsed.Host)
	updateValidatorAPIIdentities(func(identities map[string][]*validatorAPIIdentity) {
		identities[host] = append(identities[host], identity)
	})
	var once sync.Once
	return func() {
		once.Do(func() {
			updateValidatorAPIIdentities(func(identities map[string][]*validatorAPIIdentity) {
				kept := []*validatorAPIIdentity{}
				for _, installed := range identities[host] {
					if installed != identity {
						kept = append(kept, installed)
					}
				}
				if len(kept) == 0 {
					delete(identities, host)
				} else {
					identities[host] = kept
				}
			})
			if identity.class != nil {
				identity.class.close()
			}
		})
	}, nil
}

// Replaces the installed identities with update's change to a copy.
func updateValidatorAPIIdentities(update func(map[string][]*validatorAPIIdentity)) {
	validatorAPIIdentitiesLock.Lock()
	defer validatorAPIIdentitiesLock.Unlock()
	next := map[string][]*validatorAPIIdentity{}
	if current := validatorAPIIdentities.Load(); current != nil {
		for host, installed := range *current {
			next[host] = append([]*validatorAPIIdentity{}, installed...)
		}
	}
	update(next)
	validatorAPIIdentities.Store(&next)
}

// The one installed identity of a request's host, or nil.
func validatorAPIIdentityOf(r *http.Request) *validatorAPIIdentity {
	identities := validatorAPIIdentities.Load()
	if identities == nil || r == nil || r.URL == nil {
		return nil
	}
	if installed := (*identities)[strings.ToLower(r.URL.Host)]; len(installed) == 1 {
		return installed[0]
	}
	return nil
}

// Adds an installed api origin's bearer and class token to each request for
// it, leaving any Authorization or class header the request already has.
type validatorAPITransportWrapper struct {
	base http.RoundTripper
}

// Wraps base, or http.DefaultTransport when base is nil, for an api client.
func validatorAPITransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &validatorAPITransportWrapper{base: base}
}

// The transport a wrapper sends with, for tests.
func validatorAPITransportBase(transport http.RoundTripper) http.RoundTripper {
	if wrapper, ok := transport.(*validatorAPITransportWrapper); ok {
		return wrapper.base
	}
	return transport
}

// Whether two client transports send the same way: equal, or each the
// identity wrapper over the same transport.
func sameValidatorAPITransport(a http.RoundTripper, b http.RoundTripper) bool {
	wrapperA, okA := a.(*validatorAPITransportWrapper)
	wrapperB, okB := b.(*validatorAPITransportWrapper)
	if okA != okB {
		return false
	}
	if okA {
		return wrapperA.base == wrapperB.base
	}
	return a == b
}

// Closes base's idle connections, as http.Client.CloseIdleConnections does
// for an unwrapped transport.
func (self *validatorAPITransportWrapper) CloseIdleConnections() {
	if closer, ok := self.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// See http.RoundTripper. The request is cloned before its headers change.
func (self *validatorAPITransportWrapper) RoundTrip(r *http.Request) (*http.Response, error) {
	identity := validatorAPIIdentityOf(r)
	if identity == nil {
		return self.base.RoundTrip(r)
	}
	clone := r.Clone(r.Context())
	if clone.Header.Get("Authorization") == "" {
		if credential := identity.credential(); credential != "" {
			clone.Header.Set("Authorization", "Bearer "+credential)
		}
	}
	if identity.class != nil && clone.Header.Get(protocol.ValidatorClassHeader) == "" && clone.URL.Path != validatorClassMintPath {
		if token := identity.class.current(r.Context()); token != "" {
			clone.Header.Set(protocol.ValidatorClassHeader, token)
		}
	}
	return self.base.RoundTrip(clone)
}

// One api origin's class tokens. A token is used until the refresh margin,
// then minted again in the background while requests keep using it; only a
// request with no live token waits for a mint, and at most
// validatorClassMintWait. A failed mint is retried after 30 s, doubling to
// 4 min while it keeps failing. Until a mint succeeds, requests carry the
// bearer alone, under the identity limit.
type validatorClassTokens struct {
	// the operator runtime's: its end ends a running mint
	ctx    context.Context
	cancel context.CancelFunc
	noID   uint64

	endpoint   string
	credential func() string
	signer     *validatorAttemptUploadSigner
	// unwrapped, so a mint never asks for a class token itself
	client   *http.Client
	now      func() time.Time
	mintWait time.Duration

	lock        sync.Mutex
	token       string
	expires     time.Time
	nextAttempt time.Time
	retry       time.Duration
	// closed when the running mint ends; nil when none runs
	minting chan struct{}
	// the last mint's outcome, so diagnostics report only changes
	ready  bool
	failed bool
	closed bool
}

func newValidatorClassTokens(ctx context.Context, noID uint64, origin string, credential func() string, signer *validatorAttemptUploadSigner) (*validatorClassTokens, error) {
	base, err := url.Parse(origin)
	if err != nil || base.Host == "" || credential == nil || signer == nil {
		return nil, errors.New("validator class tokens need an api origin, a credential and a signer")
	}
	ctx, cancel := context.WithCancel(ctx)
	return &validatorClassTokens{
		ctx:        ctx,
		cancel:     cancel,
		noID:       noID,
		endpoint:   base.ResolveReference(&url.URL{Path: validatorClassMintPath}).String(),
		credential: credential,
		signer:     signer,
		client: &http.Client{Timeout: validatorClassMintTimeout, Transport: releaseAPIHTTPTransport, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		now:      time.Now,
		mintWait: validatorClassMintWait,
	}, nil
}

// The token to send now, or "" when there is none.
func (self *validatorClassTokens) current(ctx context.Context) string {
	now := self.now()
	self.lock.Lock()
	live := self.liveWithLock(now)
	if live != "" && now.Add(validatorClassRefreshMargin).Before(self.expires) {
		self.lock.Unlock()
		return live
	}
	minting := self.minting
	// without a session there is nothing to consent for, so no attempt is spent
	if minting == nil && !self.closed && !now.Before(self.nextAttempt) && self.credential() != "" {
		minting = make(chan struct{})
		self.minting = minting
		go self.refresh(minting)
	}
	self.lock.Unlock()
	if live != "" || minting == nil {
		return live
	}
	wait := time.NewTimer(self.mintWait)
	defer wait.Stop()
	select {
	case <-minting:
	case <-wait.C:
	case <-ctx.Done():
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.liveWithLock(self.now())
}

// The held token if it has not expired.
func (self *validatorClassTokens) liveWithLock(now time.Time) string {
	if self.token != "" && now.Before(self.expires) {
		return self.token
	}
	return ""
}

// One background mint; minting closes when it has been recorded.
func (self *validatorClassTokens) refresh(minting chan struct{}) {
	defer close(minting)
	now := self.now()
	token, expires, cause, err := self.mint(now)
	self.lock.Lock()
	self.minting = nil
	code := ""
	if err != nil {
		self.retry = min(max(2*self.retry, validatorClassRetryInterval), maximumValidatorClassRetryInterval)
		self.nextAttempt = now.Add(self.retry)
		if !self.failed && self.ctx.Err() == nil {
			code = "validator_class_unavailable"
		}
		self.ready, self.failed = false, true
	} else {
		self.token, self.expires, self.nextAttempt, self.retry = token, expires, time.Time{}, 0
		if !self.ready {
			code = "validator_class_ready"
		}
		self.ready, self.failed = true, false
	}
	self.lock.Unlock()
	if code != "" {
		releaseDiagnostic(self.ctx, "operator", code, 0, false, 0, releaseDiagnosticFacts{operatorId: self.noID, operatorKnown: true, cause: cause})
	}
}

// Ends a running mint and waits for it; no mint starts after.
func (self *validatorClassTokens) close() {
	self.lock.Lock()
	self.closed = true
	minting := self.minting
	self.lock.Unlock()
	self.cancel()
	if minting != nil {
		<-minting
	}
}

// One mint: the class consent for the current credential, beside it. The
// cause classifies a failure for the diagnostics only.
func (self *validatorClassTokens) mint(now time.Time) (string, time.Time, releaseDiagnosticCause, error) {
	credential := self.credential()
	if credential == "" {
		return "", time.Time{}, releaseDiagnosticUnavailable, errors.New("validator class mint has no api session")
	}
	header, err := self.signer.classHeaderAt(credential, now)
	if err != nil {
		return "", time.Time{}, releaseDiagnosticHardError, err
	}
	ctx, cancel := context.WithTimeout(self.ctx, validatorClassMintTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.endpoint, nil)
	if err != nil {
		return "", time.Time{}, releaseDiagnosticHardError, err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set(protocol.ValidatorClassIntentHeader, header)
	response, err := self.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", time.Time{}, releaseDiagnosticTimeout, err
		}
		return "", time.Time{}, releaseDiagnosticTransport, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16*1024))
	if err != nil {
		return "", time.Time{}, releaseDiagnosticTransport, err
	}
	switch {
	case response.StatusCode == http.StatusOK:
	case 500 <= response.StatusCode:
		return "", time.Time{}, releaseDiagnosticUnavailable, errors.New("validator class mint answered " + response.Status)
	default:
		// refused: the admission does not admit this validator, or the consent
		return "", time.Time{}, releaseDiagnosticHardError, errors.New("validator class mint answered " + response.Status)
	}
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", time.Time{}, releaseDiagnosticHardError, err
	}
	if result.Token == "" || maximumValidatorClassTokenBytes < len(result.Token) || !now.Before(result.ExpiresAt) || now.Add(validatorClassMaximumLifetime).Before(result.ExpiresAt) {
		return "", time.Time{}, releaseDiagnosticHardError, errors.New("validator class mint answered no usable token")
	}
	return result.Token, result.ExpiresAt, releaseDiagnosticUnknown, nil
}
