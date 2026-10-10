// Flag-mode measurement retains one original client and joins every user of
// its key custody. Chain reads stamp epochs only; this runner cannot steer.
package validator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

type measurementRunSettings struct {
	apiUrl           string
	connectUrl       string
	stateDir         string
	networkPath      string
	concurrency      int
	m                int
	rpcUrls          []string
	contract         string
	adoptLegacyKey   bool
	strategySettings *connect.ClientStrategySettings
	// the command that writes networkPath, named when its sign-in is rejected;
	// empty is the single-operator `validator auth`
	authCommand string
}

// Test observers stop only after actual authentication or worker completion.
// They cannot replace custody, allocation, HTTP, refresh or measurement logic.
type measurementRunHooks struct {
	afterAttempt          func(error) error
	afterAuthenticated    func(string, connect.Id, []byte) error
	afterWorkersStarted   func()
	afterTrailJoined      func()
	afterStatsJoined      func()
	afterApiJoined        func()
	afterRefreshPersisted func()
	beforeWorkersWait     func()
	beforeApiWait         func()
}
type measurementRunHooksKey struct{}

// The no-config path deliberately does not call LoadIdentity: that helper may
// generate a new key and load unrelated signing keys. This path only measures.
func measurementSettingsFromOpts(opts docopt.Opts) (measurementRunSettings, error) {
	stateDir := optString(opts, "--state_dir", "")
	if stateDir == "" {
		var err error
		stateDir, err = defaultStateDir()
		if err != nil {
			return measurementRunSettings{}, err
		}
	}
	stateDir, err := filepath.Abs(expandHome(stateDir))
	if err != nil {
		return measurementRunSettings{}, err
	}
	networkPath, err := networkJwtPath()
	if err != nil {
		return measurementRunSettings{}, err
	}
	adopt, _ := opts.Bool("--adopt-legacy-measurement-key")
	return measurementRunSettings{apiUrl: optString(opts, "--api_url", DefaultApiUrl), connectUrl: optString(opts, "--connect_url", DefaultConnectUrl), stateDir: filepath.Clean(stateDir), networkPath: networkPath, concurrency: optInt(opts, "--concurrency", 4), m: optInt(opts, "--m", connect.VerifyMDefault), rpcUrls: optStringList(opts, "--rpc"), contract: optString(opts, "--contract", ""), adoptLegacyKey: adopt}, nil
}

// Only typed unavailable reads may replay the exact retained request. Hard
// custody, completed identity/refusal and mixed joined causes remain closed.
func measurementAuthenticationRetryable(err error, depth int) bool {
	remaining := 512
	return releaseErrorGraph(err, func(cause error) bool {
		switch cause.(type) {
		case *sdk.NetworkClientRegistrationUnavailableError, *clientauth.RegistrationRefreshUnavailableError:
			return true
		}
		return false
	}, depth, &remaining)
}

// A registration the server refused for its network sign-in needs a new
// sign-in at networkPath, not measurement custody recovery.
func measurementSignInRejected(networkPath string, authCommand string, err error) error {
	if authCommand == "" {
		authCommand = "validator auth"
	}
	return fmt.Errorf("the network sign-in at %s was rejected or has expired; run `%s` to sign in again: %w", networkPath, authCommand, err)
}

func authenticateMeasurement(ctx context.Context, api *sdk.Api, owner *clientauth.ValidatorMeasurementClientKeyOwner, networkPath string) (string, connect.Id, error) {
	hooks, _ := ctx.Value(measurementRunHooksKey{}).(measurementRunHooks)
	for {
		attempt, cancel := context.WithTimeout(ctx, 300*time.Second)
		token, id, err := owner.LoadOrRegisterClientJwt(attempt, api, networkPath)
		cancel()
		if hooks.afterAttempt != nil {
			if observed := hooks.afterAttempt(err); observed != nil {
				return "", connect.Id{}, errors.Join(err, observed)
			}
		}
		if err == nil {
			return token, id, nil
		}
		if ctx.Err() != nil {
			return "", connect.Id{}, errors.Join(err, ctx.Err())
		}
		if clientauth.IsNetworkCredentialRejected(err) {
			return "", connect.Id{}, err
		}
		if !measurementAuthenticationRetryable(err, 0) {
			return "", connect.Id{}, fmt.Errorf("measurement client identity requires recovery: %w", err)
		}
		select {
		case <-ctx.Done():
			return "", connect.Id{}, errors.Join(err, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// API callbacks may cancel; only the outer runner joins. A failed persist or
// identity check cannot publish a token to the measurement transports.
type measurementAuthenticationCallbacks struct {
	owner        *clientauth.ValidatorMeasurementClientKeyOwner
	original     string
	cancel       context.CancelFunc
	failed       atomic.Bool
	failureLock  sync.Mutex
	cause        error
	publish      func(string)
	afterPersist func()
}

func (self *measurementAuthenticationCallbacks) fail(err error) {
	self.failed.Store(true)
	func() {
		self.failureLock.Lock()
		defer self.failureLock.Unlock()
		self.cause = errors.Join(self.cause, err)
	}()
	self.cancel()
}

func (self *measurementAuthenticationCallbacks) failure() error {
	self.failureLock.Lock()
	defer self.failureLock.Unlock()
	return self.cause
}

func (self *measurementAuthenticationCallbacks) JwtRefreshed(token string) {
	if self.failed.Load() {
		return
	}
	if err := clientauth.ValidateRefreshedClientJwt(self.original, token); err != nil {
		self.fail(err)
		return
	}
	if err := self.owner.PersistClientJwt(token); err != nil {
		self.fail(err)
		return
	}
	if self.afterPersist != nil {
		self.afterPersist()
	}
	if !self.failed.Load() && self.publish != nil {
		self.publish(token)
	}
}

func (self *measurementAuthenticationCallbacks) AuthLogout() {
	self.failed.Store(true)
	self.cancel()
	self.fail(errors.Join(errors.New("measurement client authentication was revoked"), self.owner.RejectClientJwt()))
}

func (self *measurementAuthenticationCallbacks) ClientRefreshInvalid(notice *sdk.ClientRefreshIntegrityNotice) {
	if notice.CloseApiIfCurrent() {
		self.fail(errors.New("measurement client refresh changed its owned identity"))
	}
}

// The key owner is the outer lifetime. API, transports, trail and periodic
// workers terminate before it is released, including errors and cancellation.
func (self measurementRunSettings) run(parent context.Context, output io.Writer) (returnErr error) {
	if parent == nil || output == nil {
		return errors.New("measurement runner requires its context and output")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	hooks, _ := ctx.Value(measurementRunHooksKey{}).(measurementRunHooks)
	owner, err := clientauth.OpenValidatorMeasurementClientKey(ctx, filepath.Join(self.stateDir, ".validator.key"), self.adoptLegacyKey)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, owner.Close()) }()
	strategySettings := self.strategySettings
	if strategySettings == nil {
		strategySettings = connect.DefaultClientStrategySettings()
	}
	strategy := connect.NewClientStrategy(ctx, strategySettings)
	defer strategy.Close()
	api := sdk.NewApi(ctx, strategy, self.apiUrl)
	defer func() {
		returnErr = errors.Join(returnErr, api.CloseAndWait(context.Background()))
		if hooks.afterApiJoined != nil {
			hooks.afterApiJoined()
		}
	}()
	token, id, err := authenticateMeasurement(ctx, api, owner, self.networkPath)
	if err != nil {
		if clientauth.IsNetworkCredentialRejected(err) {
			return measurementSignInRejected(self.networkPath, self.authCommand, err)
		}
		return err
	}
	if hooks.afterAuthenticated != nil {
		if err := hooks.afterAuthenticated(token, id, owner.Seed()); err != nil {
			return err
		}
	}
	settings := connect.DefaultClientSettings()
	settings.ClientKeySeed = owner.Seed()
	outOfBand := connect.NewApiOutOfBandControl(ctx, strategy, token, self.apiUrl)
	identity := connect.NewClient(ctx, id, outOfBand, settings)
	instanceId := connect.NewId()
	platform := connect.NewPlatformTransportWithDefaults(ctx, strategy, identity.RouteManager(), self.connectUrl, &connect.ClientAuth{ByJwt: token, InstanceId: instanceId, AppVersion: RequireVersion()})
	var durableCredential atomic.Value
	durableCredential.Store(token)
	transport := NewTunnelTransport(ctx, strategy, TunnelTransportConfig{ApiUrl: self.apiUrl, ConnectUrl: self.connectUrl, ByClientJwt: func() string { return durableCredential.Load().(string) }, SourceClientId: id})
	callbacks := &measurementAuthenticationCallbacks{owner: owner, original: token, cancel: cancel, afterPersist: hooks.afterRefreshPersisted, publish: func(refreshed string) {
		durableCredential.Store(refreshed)
		outOfBand.SetByJwt(refreshed)
		platform.SetAuth(&connect.ClientAuth{ByJwt: refreshed, InstanceId: instanceId, AppVersion: RequireVersion()})
	}}
	refresh := api.AddJwtRefreshListener(callbacks)
	invalid := api.AddClientRefreshIntegrityListener(callbacks)
	logout := api.AddAuthLogoutListener(callbacks)
	defer func() {
		cancel()
		if hooks.beforeApiWait != nil {
			hooks.beforeApiWait()
		}
		returnErr = errors.Join(returnErr, api.CloseAndWait(context.Background()))
		refresh.Close()
		invalid.Close()
		logout.Close()
		returnErr = errors.Join(returnErr, transport.CloseAndWait(context.Background()), platform.CloseAndWait(context.Background()), identity.CloseAndWait(context.Background()), outOfBand.CloseAndWait(context.Background()), callbacks.failure())
	}()
	api.StartJwtRefresh()

	var chain *ChainClient
	var cachedEpoch atomic.Uint64
	if len(self.rpcUrls) > 0 && self.contract != "" {
		if !common.IsHexAddress(self.contract) {
			return errors.New("invalid measurement epoch contract")
		}
		chain, err = DialChainContext(ctx, self.rpcUrls, common.HexToAddress(self.contract))
		if err != nil {
			return err
		}
		defer chain.Close()
		fmt.Fprintln(output, "chain: epoch reads configured; steering disabled")
	} else {
		fmt.Fprintln(output, "chain: not configured; proofs carry epoch 0; steering disabled")
	}
	refreshEpoch := func() {
		raw, err := chain.ethCallAtContext(ctx, chain.contractAddr, chain.st.PackEpoch(), nil)
		if err == nil {
			if epoch, err := chain.st.UnpackEpoch(raw); err == nil {
				cachedEpoch.Store(epoch.Uint64())
			}
		}
	}
	if chain != nil {
		refreshEpoch()
	}
	stats := NewStatsEngine(StatsConfig{})
	if err := stats.Load(self.stateDir); err != nil {
		fmt.Fprintln(output, "stats load failed; starting fresh")
	}
	store, err := NewProofStore(self.stateDir)
	if err != nil {
		return err
	}
	engine := NewTrailEngine(id, ed25519.NewKeyFromSeed(owner.Seed()), transport, NewApiServerKeyRing(api), NewFindProvidersSeedPicker(api, id), stats, store, func() uint64 { return cachedEpoch.Load() }, TrailEngineConfig{M: self.m})
	var workers sync.WaitGroup
	trailResult := make(chan error, 1)
	workers.Add(2)
	go func() {
		defer workers.Done()
		err := engine.Run(ctx, self.concurrency)
		trailResult <- err
		if err != nil {
			cancel()
		}
		if hooks.afterTrailJoined != nil {
			hooks.afterTrailJoined()
		}
	}()
	go func() {
		defer workers.Done()
		defer func() {
			if hooks.afterStatsJoined != nil {
				hooks.afterStatsJoined()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(60 * time.Second):
				if err := stats.Save(self.stateDir); err != nil {
					fmt.Fprintln(output, "stats save failed")
				}
			}
		}
	}()
	if chain != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(30 * time.Second):
					refreshEpoch()
				}
			}
		}()
	}
	defer func() {
		cancel()
		if hooks.beforeWorkersWait != nil {
			hooks.beforeWorkersWait()
		}
		workers.Wait()
		returnErr = errors.Join(returnErr, <-trailResult, stats.Save(self.stateDir))
	}()
	if hooks.afterWorkersStarted != nil {
		hooks.afterWorkersStarted()
	}
	fmt.Fprintf(output, "validator %s measuring (concurrency %d, M %d); steering disabled\n", RequireVersion(), self.concurrency, self.m)
	<-ctx.Done()
	return nil
}
