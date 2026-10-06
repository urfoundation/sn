// Production owns local evidence and native liabilities independently of an
// operator's live API session. Only the authenticated session enables new work.
package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// The real worker alone installs engine and closes admitted, after durable
// credentials and complete transport ownership. The channel publishes engine.
type productionOperatorAuthentication struct {
	ready            atomic.Bool
	recoveryRequired atomic.Bool
	admitted         chan struct{}
	engine           releaseTrailRunner
	run              func(context.Context) error
	trailContext     context.Context
	cancelTrails     context.CancelFunc
}

func (self *productionOperatorAuthentication) Run(ctx context.Context, concurrency int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-self.admitted:
	}
	if !self.ready.Load() {
		return nil // The owned authentication wait remains visible separately.
	}
	if self.engine == nil || self.trailContext == nil {
		return errors.New("operator authentication omitted the admitted trail owner")
	}
	return releaseRuntimeError(self.trailContext, self.engine.Run(copyTrailDiagnosticContext(self.trailContext, ctx), concurrency))
}

// This typed wait cannot authorize a send or erase a hard joined cause.
type productionOperatorAuthenticationPending struct {
	noId        uint64
	nativeEpoch uint64
	epochKnown  bool
	hard        bool
}

func (self *productionOperatorAuthenticationPending) Error() string {
	return "production API work waits for the original operator authentication"
}

func (self *releaseRuntimeV2) authenticationPending(intent *SteeringIntent) error {
	if self == nil {
		return nil // Historical read-only helpers do not own live destinations.
	}
	for _, runtime := range self.runtimes {
		if runtime.authentication != nil && !runtime.authentication.ready.Load() {
			wait := &productionOperatorAuthenticationPending{noId: runtime.measurement.NoID, hard: runtime.authentication.recoveryRequired.Load()}
			if intent != nil {
				wait.nativeEpoch, wait.epochKnown = intent.SubnetEpoch, true
			}
			return wait
		}
	}
	return nil
}

// These are completed application refusals or the dedicated idempotent route's
// unresolved physical reply. Arbitrary decoder/custody errors remain hard.
func productionRegistrationWait(err error) (bool, bool) {
	remaining := 512
	blocked := false
	wait := releaseErrorGraph(err, func(cause error) bool {
		switch value := cause.(type) {
		case *sdk.NetworkClientRegistrationUnsupportedError:
			blocked = true
			return true
		case *clientauth.RegistrationRefusedError:
			blocked = true
			return value.Code == "network_authentication_missing" || value.Code == "legacy_identity_requires_explicit_recovery"
		case *sdk.NetworkClientRegistrationUnavailableError, *clientauth.RegistrationRefreshUnavailableError:
			return true
		}
		return false
	}, 0, &remaining)
	return wait, wait && blocked
}

// Only API-local causes may latch this operator while native observation stays
// alive. A local descriptor/ledger/intent error joined to them remains shared
// failure; no arbitrary error text or generic hard leaf is downgraded here.
func productionRegistrationLocalFailure(err error) bool {
	remaining := 512
	return releaseErrorGraph(err, func(cause error) bool {
		if cause == context.DeadlineExceeded {
			return true
		}
		switch value := cause.(type) {
		case *sdk.ClientControlResponseError, *clientauth.RegistrationResponseIdentityError, *clientauth.RegistrationRefusedError, *sdk.NetworkClientRegistrationUnsupportedError:
			return true
		case *sdk.NetworkClientRegistrationUnavailableError, *clientauth.RegistrationRefreshUnavailableError:
			return true // In a mixed local verdict, this never makes it retryable.
		case *connect.HttpStatusError:
			return 400 <= value.StatusCode && value.StatusCode <= 599
		}
		return false
	}, 0, &remaining)
}

func observeProductionAuthenticationWait(ctx context.Context, progress *releaseProgress, wait *productionOperatorAuthenticationPending) {
	outcome, code, cause := "read_wait", "authentication_pending", releaseDiagnosticUnknown
	if wait.hard {
		outcome, cause = "hard_error", releaseDiagnosticHardError
	}
	progress.observeSteering(wait.nativeEpoch, wait.epochKnown, outcome, false)
	releaseDiagnostic(ctx, "operator", code, wait.nativeEpoch, wait.epochKnown, 0, releaseDiagnosticFacts{operatorId: wait.noId, operatorKnown: true, cause: cause})
}

// The optional package-private clock seam controls only the actual operation
// deadline. Tests can expire it after physical I/O while keeping service
// lifetime independent; no result, identity, or readiness can be supplied.
type productionAuthenticationReadHooksKey struct{}
type productionAuthenticationReadHooks struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	// Optional tests observe copied completed transitions without granting
	// readiness. The observer must return immediately and owns no runtime.
	observe func(productionAuthenticationObservation)
}

type productionAuthenticationObservation struct {
	noId             uint64
	ready            bool
	recoveryRequired bool
}

// Called after the real atomic/session transition, outside locks. Optional
// diagnostic delivery is lossy and therefore cannot order recovery fixtures.
func observeProductionAuthentication(ctx context.Context, noId uint64, owner *productionOperatorAuthentication) {
	if hooks, ok := ctx.Value(productionAuthenticationReadHooksKey{}).(productionAuthenticationReadHooks); ok && hooks.observe != nil {
		hooks.observe(productionAuthenticationObservation{noId: noId, ready: owner.ready.Load(), recoveryRequired: owner.recoveryRequired.Load()})
	}
}

func productionAuthenticationAttemptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if hooks, ok := ctx.Value(productionAuthenticationReadHooksKey{}).(productionAuthenticationReadHooks); ok && hooks.withTimeout != nil {
		return hooks.withTimeout(ctx, productionSteeringReadTimeout)
	}
	return context.WithTimeout(ctx, productionSteeringReadTimeout)
}

// All evidence owners below are real, and their constructors are local. A
// credential getter never exposes the temporary network bootstrap credential.
func newProductionReleaseOperator(ctx context.Context, cfg *ReleaseConfig, op OperatorConfig, epochFn func() uint64, resolver AttemptBoundaryResolver, state *releaseAttemptState, seed []byte, key ed25519.PrivateKey, artifacts *HTTPArtifactReader, seedInterval time.Duration, strategy *connect.ClientStrategy, api *sdk.Api) (_ *releaseOperatorRuntime, returnErr error) {
	owner := &productionOperatorAuthentication{admitted: make(chan struct{})}
	owner.trailContext, owner.cancelTrails = context.WithCancel(ctx)
	var durableCredential atomic.Value
	credential := func() string {
		if !owner.ready.Load() || ctx.Err() != nil {
			return ""
		}
		value, _ := durableCredential.Load().(string)
		return value
	}
	upload, err := newReleaseAttemptUploadV2(ctx, op, cfg.EvidenceV2.Bounds, credential)
	if err != nil {
		owner.cancelTrails()
		strategy.Close()
		return nil, errors.Join(err, api.CloseAndWait(context.Background()))
	}
	var closeConnected func() error
	var closeOnce sync.Once
	var closeErr error
	closeResources := func() error {
		closeOnce.Do(func() {
			owner.ready.Store(false)
			owner.cancelTrails()
			upload.close()
			if closeConnected != nil {
				closeErr = closeConnected()
			}
			closeErr = errors.Join(closeErr, releaseStageError("API", api.CloseAndWait(context.Background())))
			strategy.Close()
		})
		return closeErr
	}
	transferred := false
	defer func() {
		if !transferred {
			returnErr = errors.Join(returnErr, closeResources())
		}
	}()
	keyHistory, err := NewHTTPClientKeyHistoryReader(op.APIURL, credential)
	if err != nil {
		return nil, err
	}
	measurement := &ReleaseMeasurementContext{NoID: op.NoID, Stats: state.stats, Artifacts: artifacts, ClientKeyHistory: keyHistory,
		ClientKey: func(id connect.Id) ([32]byte, bool, error) {
			if credential() == "" {
				return [32]byte{}, false, &productionOperatorAuthenticationPending{noId: op.NoID}
			}
			sdkId, err := sdk.ParseId(id.String())
			if err != nil {
				return [32]byte{}, false, err
			}
			result, err := api.GetClientKeySyncWithContext(ctx, &sdk.GetClientKeyArgs{ClientId: sdkId})
			if err != nil {
				return [32]byte{}, false, err
			}
			if result == nil || len(result.PublicKey) != ed25519.PublicKeySize {
				return [32]byte{}, false, nil
			}
			var public [32]byte
			copy(public[:], result.PublicKey)
			return public, true, nil
		},
	}
	endpoint, err := api.NetworkClientRegistrationEndpoint()
	if err != nil {
		return nil, err
	}
	scope := clientauth.RegistrationScope{Endpoint: endpoint, DeploymentId: cfg.DeploymentID, ChainId: cfg.ChainID, GenesisHash: strings.ToLower(cfg.GenesisHash), Netuid: cfg.Netuid, ValidatorId: cfg.ValidatorID, OperatorNoId: op.NoID, ClientKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))}
	owner.run = func(service context.Context) error {
		var token string
		var clientId connect.Id
		for {
			// One operation owns a finite 300s ceiling. A later poll replays
			// only the same durable request; service lifetime is independent.
			attempt, cancel := productionAuthenticationAttemptContext(service)
			var err error
			token, clientId, err = clientauth.LoadOrRegisterClientJwt(attempt, api, op.NetworkJWTFile, op.ClientJWTFile, fmt.Sprintf("validator-%d no-%d", cfg.ValidatorID, op.NoID), scope, op.AllowClientRegistration)
			cancel()
			if err == nil {
				break
			}
			if service.Err() != nil {
				return errors.Join(err, service.Err())
			}
			wait, blocked := productionRegistrationWait(err)
			if !wait {
				if productionRegistrationLocalFailure(err) {
					owner.recoveryRequired.Store(true)
					owner.cancelTrails()
					api.Close()
					observeProductionAuthentication(ctx, op.NoID, owner)
					releaseDiagnostic(ctx, "operator", "authentication_recovery_required", 0, false, 0, releaseDiagnosticFacts{operatorId: op.NoID, operatorKnown: true, cause: releaseDiagnosticHardError})
					return nil // Latched; no automatic API retry or replacement.
				}
				return err
			}
			code := "authentication_unavailable"
			if blocked {
				code = "authentication_recovery_required"
			}
			releaseDiagnostic(ctx, "operator", code, 0, false, 0, releaseDiagnosticFacts{operatorId: op.NoID, operatorKnown: true})
			if err := waitReleaseSnapshotRetry(service, time.Duration(cfg.PollSeconds)*time.Second); err != nil {
				return err
			}
		}
		if err := service.Err(); err != nil {
			return err
		}
		settings := connect.DefaultClientSettings()
		settings.ClientKeySeed = seed
		domainHash := releaseCloseReportDomain(cfg, op.NoID)
		settings.ContractManagerSettings.CloseReportDomainHash = domainHash
		outOfBand := connect.NewApiOutOfBandControl(ctx, strategy, token, op.APIURL)
		identity := connect.NewClient(ctx, clientId, outOfBand, settings)
		instanceId := connect.NewId()
		platform := connect.NewPlatformTransportWithDefaults(ctx, strategy, identity.RouteManager(), op.ConnectURL, &connect.ClientAuth{ByJwt: token, InstanceId: instanceId, AppVersion: RequireVersion()})
		transport := NewTunnelTransport(ctx, strategy, TunnelTransportConfig{ApiUrl: op.APIURL, ConnectUrl: op.ConnectURL, ByClientJwt: credential, SourceClientId: clientId, CloseReportDomainHash: domainHash})
		withdraw := func(code string) {
			owner.recoveryRequired.Store(true)
			owner.ready.Store(false)
			owner.cancelTrails()
			upload.close()
			transport.Close()
			api.Close()
			observeProductionAuthentication(ctx, op.NoID, owner)
			releaseDiagnostic(ctx, "operator", code, 0, false, 0, releaseDiagnosticFacts{operatorId: op.NoID, operatorKnown: true, cause: releaseDiagnosticHardError})
		}
		refresh := api.AddJwtRefreshListener(clientauth.JwtRefreshListenerFunc(func(refreshed string) {
			if owner.recoveryRequired.Load() {
				return
			}
			if err := clientauth.ValidateRefreshedClientJwt(token, refreshed); err != nil {
				withdraw("authentication_recovery_required")
				return
			}
			if err := clientauth.WriteToken(op.ClientJWTFile, refreshed); err != nil {
				withdraw("jwt_save_failed")
				return
			}
			durableCredential.Store(refreshed)
			outOfBand.SetByJwt(refreshed)
			platform.SetAuth(&connect.ClientAuth{ByJwt: refreshed, InstanceId: instanceId, AppVersion: RequireVersion()})
		}))
		invalid := api.AddClientRefreshIntegrityListener(clientauth.ClientRefreshIntegrityListenerFunc(func(notice *sdk.ClientRefreshIntegrityNotice) {
			if notice.CloseApiIfCurrent() {
				withdraw("authentication_recovery_required")
			}
		}))
		logout := api.AddAuthLogoutListener(clientauth.AuthLogoutListenerFunc(func() {
			owner.recoveryRequired.Store(true)
			owner.ready.Store(false)
			owner.cancelTrails()
			upload.close()
			transport.Close()
			api.Close()
			code := "authentication_revoked"
			if err := clientauth.MarkRejected(op.ClientJWTFile, op.NetworkJWTFile); err != nil {
				code = "jwt_rejection_save_failed"
			}
			observeProductionAuthentication(ctx, op.NoID, owner)
			releaseDiagnostic(ctx, "operator", code, 0, false, 0, releaseDiagnosticFacts{operatorId: op.NoID, operatorKnown: true, cause: releaseDiagnosticHardError})
		}))
		closeConnected = func() error {
			refresh.Close()
			logout.Close()
			invalid.Close()
			return errors.Join(releaseStageError("tunnel transport", transport.CloseAndWait(context.Background())), releaseStageError("platform transport", platform.CloseAndWait(context.Background())), releaseStageError("identity client", identity.CloseAndWait(context.Background())), releaseStageError("out-of-band control", outOfBand.CloseAndWait(context.Background())))
		}
		requests, err := openReleaseProviderAttemptRequests(service, cfg, op, state.ledger, clientId, key)
		if err != nil {
			return err
		}
		closeConnection := closeConnected
		closeConnected = func() error { return errors.Join(closeConnection(), requests.Close()) }
		owner.engine = NewTrailEngine(clientId, key, transport, NewApiServerKeyRing(api), NewFindProvidersSeedPicker(api, clientId), state.stats, state.store, epochFn, TrailEngineConfig{M: cfg.Policy.Verify.TrailDepth, StepTimeout: time.Duration(cfg.Policy.Verify.StepTimeoutSeconds) * time.Second, SeedAttemptInterval: seedInterval, AttemptLedger: state.ledger, AttemptBoundaryResolver: resolver, RequestJournal: requests})
		durableCredential.Store(token)
		owner.ready.Store(true)
		close(owner.admitted)
		observeProductionAuthentication(ctx, op.NoID, owner)
		releaseDiagnostic(ctx, "operator", "authentication_ready", 0, false, 0, releaseDiagnosticFacts{operatorId: op.NoID, operatorKnown: true})
		api.StartJwtRefresh()
		return nil
	}
	transferred = true
	return &releaseOperatorRuntime{measurement: measurement, stats: state.stats, engine: owner, close: newReleaseOperatorClose(state.stats, op.StateDir, closeResources), attemptUpload: upload, attemptSource: state.uploadSource, attemptLedger: state.ledger, authentication: owner}, nil
}
