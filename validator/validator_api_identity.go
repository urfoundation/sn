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
	// after a failed mint, the next is tried no sooner than this
	validatorClassRetryInterval = 30 * time.Second
	validatorClassMintTimeout   = 10 * time.Second
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

// The installed identities by lowercase api host. Copied on write.
var validatorAPIIdentities atomic.Pointer[map[string]*validatorAPIIdentity]
var validatorAPIIdentitiesLock sync.Mutex

// Installs identity for the api at origin until the returned func runs.
func installValidatorAPIIdentity(origin string, identity *validatorAPIIdentity) (func(), error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || identity == nil || identity.credential == nil {
		return nil, errors.New("validator api identity needs an api origin and a credential")
	}
	host := strings.ToLower(parsed.Host)
	validatorAPIIdentitiesLock.Lock()
	defer validatorAPIIdentitiesLock.Unlock()
	next := map[string]*validatorAPIIdentity{}
	if current := validatorAPIIdentities.Load(); current != nil {
		for key, value := range *current {
			next[key] = value
		}
	}
	next[host] = identity
	validatorAPIIdentities.Store(&next)
	var once sync.Once
	return func() {
		once.Do(func() {
			validatorAPIIdentitiesLock.Lock()
			defer validatorAPIIdentitiesLock.Unlock()
			current := validatorAPIIdentities.Load()
			if current == nil || (*current)[host] != identity {
				return
			}
			next := map[string]*validatorAPIIdentity{}
			for key, value := range *current {
				if key != host {
					next[key] = value
				}
			}
			validatorAPIIdentities.Store(&next)
		})
	}, nil
}

// The installed identity of a request's host, or nil.
func validatorAPIIdentityOf(r *http.Request) *validatorAPIIdentity {
	identities := validatorAPIIdentities.Load()
	if identities == nil || r == nil || r.URL == nil {
		return nil
	}
	return (*identities)[strings.ToLower(r.URL.Host)]
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

// One api origin's class tokens: the current one, minted again when less than
// the refresh margin is left. A request never waits on a mint another
// request started, and a failed mint is not tried again for a while; until a
// mint succeeds, requests carry the bearer alone, under the identity limit.
type validatorClassTokens struct {
	endpoint   string
	credential func() string
	signer     *validatorAttemptUploadSigner
	// unwrapped, so a mint never asks for a class token itself
	client *http.Client
	now    func() time.Time

	lock        sync.Mutex
	token       string
	expires     time.Time
	nextAttempt time.Time
	minting     bool
}

func newValidatorClassTokens(origin string, credential func() string, signer *validatorAttemptUploadSigner) (*validatorClassTokens, error) {
	base, err := url.Parse(origin)
	if err != nil || base.Host == "" || credential == nil || signer == nil {
		return nil, errors.New("validator class tokens need an api origin, a credential and a signer")
	}
	return &validatorClassTokens{
		endpoint:   base.ResolveReference(&url.URL{Path: validatorClassMintPath}).String(),
		credential: credential,
		signer:     signer,
		client: &http.Client{Timeout: validatorClassMintTimeout, Transport: releaseAPIHTTPTransport, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		now: time.Now,
	}, nil
}

// The token to send now, or "" when there is none.
func (self *validatorClassTokens) current(ctx context.Context) string {
	now := self.now()
	self.lock.Lock()
	if self.token != "" && now.Add(validatorClassRefreshMargin).Before(self.expires) {
		token := self.token
		self.lock.Unlock()
		return token
	}
	if self.minting || now.Before(self.nextAttempt) {
		token := self.liveWithLock(now)
		self.lock.Unlock()
		return token
	}
	self.minting = true
	self.lock.Unlock()

	token, expires, err := self.mint(ctx, now)

	self.lock.Lock()
	defer self.lock.Unlock()
	self.minting = false
	if err != nil {
		self.nextAttempt = now.Add(validatorClassRetryInterval)
		return self.liveWithLock(now)
	}
	self.token, self.expires, self.nextAttempt = token, expires, time.Time{}
	return token
}

// The held token if it has not expired.
func (self *validatorClassTokens) liveWithLock(now time.Time) string {
	if self.token != "" && now.Before(self.expires) {
		return self.token
	}
	return ""
}

// One mint: the class consent for the current credential, beside it.
func (self *validatorClassTokens) mint(ctx context.Context, now time.Time) (string, time.Time, error) {
	credential := self.credential()
	if credential == "" {
		return "", time.Time{}, errors.New("validator class mint has no api session")
	}
	header, err := self.signer.classHeaderAt(credential, now)
	if err != nil {
		return "", time.Time{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, validatorClassMintTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.endpoint, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set(protocol.ValidatorClassIntentHeader, header)
	response, err := self.client.Do(request)
	if err != nil {
		return "", time.Time{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16*1024))
	if err != nil {
		return "", time.Time{}, err
	}
	if response.StatusCode != http.StatusOK {
		return "", time.Time{}, errors.New("validator class mint answered " + response.Status)
	}
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", time.Time{}, err
	}
	if result.Token == "" || maximumValidatorClassTokenBytes < len(result.Token) || !now.Before(result.ExpiresAt) || now.Add(validatorClassMaximumLifetime).Before(result.ExpiresAt) {
		return "", time.Time{}, errors.New("validator class mint answered no usable token")
	}
	return result.Token, result.ExpiresAt, nil
}
