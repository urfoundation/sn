package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/clientauth"
	"github.com/urfoundation/sn/crv4"
)

const (
	releaseSnapshotStartupAttempts    = 5
	releaseSnapshotStartupRetryDelay  = 2 * time.Second
	releaseExpectedBlockSeconds       = 12
	releaseNativeAuthenticationBlocks = 10
	releaseNativePollingWindows       = 4
)

// releaseNativeEndpointTimeout leaves room for a complete WebSocket dial,
// metadata download, and exact runtime authentication. The public profile
// supplies a 12-second block target; config polling may request a longer
// recovery window, while a caller's own deadline still clamps this bound.
func releaseNativeEndpointTimeout(cfg *ReleaseConfig) time.Duration {
	blockBudget := time.Duration(releaseExpectedBlockSeconds*releaseNativeAuthenticationBlocks) * time.Second
	if cfg == nil || cfg.PollSeconds <= 0 {
		return blockBudget
	}
	pollBudget := time.Duration(cfg.PollSeconds*releaseNativePollingWindows) * time.Second
	if pollBudget > blockBudget {
		return pollBudget
	}
	return blockBudget
}

// Injectable coherent snapshot read used by deterministic retry tests.
type releaseSnapshotLoader func(context.Context) (*ReleaseSnapshot, error)

// Injectable interruptible delay used by deterministic retry tests.
type releaseSnapshotRetryWait func(context.Context, time.Duration) error

type releaseOperatorRuntime struct {
	measurement   *ReleaseMeasurementContext
	stats         *StatsEngine
	engine        releaseTrailRunner
	close         func() error
	attemptUpload *releaseAttemptUploadV2
	attemptSource *releaseAttemptUploadSourceV2
	attemptLedger *AttemptLedger
}

type releaseTrailRunner interface {
	Run(context.Context, int) error
}

func reportReleaseTrailEngineError(ctx context.Context, runner releaseTrailRunner, noID uint64, concurrency int, output chan<- error) {
	if err := releaseRuntimeError(ctx, runner.Run(ctx, concurrency)); err != nil {
		output <- fmt.Errorf("validator no_id %d trail engine: %w", noID, err)
	}
}

type releaseAttemptState struct {
	stats        *StatsEngine
	ledger       *AttemptLedger
	store        *ProofStore
	uploadSource *releaseAttemptUploadSourceV2
}

// Release clients consume provisioned keys only, with their original raw or
// bare-hex grammar over the shared bounded descriptor custody implementation.
func loadClientSeed(path string) ([]byte, error) {
	seed, err := crv4.LoadRawOrBareHexSeedFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return seed[:], nil
}

// Restricts startup retries to transport, provider-capacity, and timeout
// failures. ABI, policy, and contract errors remain immediate hard failures.
func transientReleaseSnapshotError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	var httpErr gethrpc.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	message := strings.ToLower(err.Error())
	// Immutable publication uses a deliberately narrow error type at the
	// stream boundary. Preserve its exact-body and ETag failures as hard
	// errors, while allowing a bounded startup retry for transient 5xx
	// responses from either configured origin.
	const uploadStatusMarker = "attempt upload response status is "
	if index := strings.Index(message, uploadStatusMarker); index >= 0 {
		var status int
		if _, scanErr := fmt.Sscanf(message[index+len(uploadStatusMarker):], "%d", &status); scanErr == nil && status >= 500 && status <= 599 {
			return true
		}
	}
	for _, marker := range []string{
		"connection refused", "connection reset", "broken pipe", "unexpected eof",
		"upstream overloaded", "temporarily unavailable",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// Retries only the initial terminal publication. A failed stream leaves its
// exact durable closure in releaseRuntimeV2, so the next advance can resume
// the same publication without refolding measurements or changing signed
// inputs. Each retry uses a newly finalized snapshot and remains bounded by
// the same startup retry budget as the initial chain read.
func advanceInitialReleaseWithRetry(ctx context.Context, initial *ReleaseSnapshot, load releaseSnapshotLoader, advance func(context.Context, *ReleaseSnapshot) error, wait releaseSnapshotRetryWait) error {
	if ctx == nil || initial == nil || load == nil || advance == nil || wait == nil {
		return errors.New("initial release publication retry dependencies are incomplete")
	}
	snapshot := initial
	var lastErr error
	for attempt := 1; attempt <= releaseSnapshotStartupAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := advance(ctx, snapshot); err == nil {
			return nil
		} else {
			lastErr = err
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if !transientReleaseSnapshotError(err) {
				return err
			}
		}
		if attempt == releaseSnapshotStartupAttempts {
			break
		}
		if err := wait(ctx, releaseSnapshotStartupRetryDelay); err != nil {
			return err
		}
		fresh, err := load(ctx)
		if err != nil {
			lastErr = err
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if !transientReleaseSnapshotError(err) {
				return err
			}
			continue
		}
		snapshot = fresh
	}
	return fmt.Errorf("initial release publication failed after %d transient attempts: %w", releaseSnapshotStartupAttempts, lastErr)
}

// Waits between bounded startup attempts while remaining interruptible.
func waitReleaseSnapshotRetry(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// Loads the first coherent finalized snapshot without turning a single public
// RPC capacity event into a supervised process restart.
func loadInitialReleaseSnapshot(ctx context.Context, load releaseSnapshotLoader, wait releaseSnapshotRetryWait) (*ReleaseSnapshot, error) {
	if ctx == nil || load == nil || wait == nil {
		return nil, errors.New("initial release snapshot retry dependencies are incomplete")
	}
	var lastErr error
	for attempt := 1; attempt <= releaseSnapshotStartupAttempts; attempt++ {
		snapshot, err := load(ctx)
		if err == nil {
			return snapshot, nil
		}
		lastErr = err
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if !transientReleaseSnapshotError(err) {
			return nil, err
		}
		if attempt < releaseSnapshotStartupAttempts {
			if err := wait(ctx, releaseSnapshotStartupRetryDelay); err != nil {
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("initial release snapshot failed after %d transient attempts: %w", releaseSnapshotStartupAttempts, lastErr)
}

// releaseSeedAttemptInterval leaves 25% headroom below the server's locked
// per-minute hard cap and spaces every attempt (including idempotent retries)
// across all workers. Headroom absorbs fixed-window phase and process-restart
// overlap without weakening the server-side abuse bound.
func releaseSeedAttemptInterval(hardLimit int) (time.Duration, error) {
	if hardLimit <= 0 {
		return 0, errors.New("hard seed rate limit must be positive")
	}
	// floor(3*hardLimit/4), arranged without multiplication overflow.
	safeLimit := (hardLimit/4)*3 + (hardLimit%4)*3/4
	if safeLimit < 1 {
		safeLimit = 1
	}
	if int64(safeLimit) > int64(time.Minute) {
		return 0, errors.New("hard seed rate limit exceeds nanosecond pacing capacity")
	}
	divisor := time.Duration(safeLimit)
	interval := time.Minute / divisor
	if time.Minute%divisor != 0 {
		interval++
	}
	return interval, nil
}

func newReleaseAttemptBoundaryResolver(ctx context.Context, chain *ChainClient, cfg *ReleaseConfig) *cachedAttemptBoundaryResolver {
	return newCachedAttemptBoundaryResolverWithLifecycle(ctx, &chainAttemptBoundaryRPC{chain: chain, netuid: cfg.Netuid}, releaseNativeEndpointTimeout(cfg))
}

func loadReleaseAttemptState(cfg *ReleaseConfig, op OperatorConfig, validatorUID uint16) (*releaseAttemptState, error) {
	return loadReleaseAttemptStateWithObserver(cfg, op, validatorUID, nil)
}

// A call-local observer retains the actual acquired ledger for deterministic
// ownership tests. It cannot skip any production load or recovery operation.
func loadReleaseAttemptStateWithObserver(cfg *ReleaseConfig, op OperatorConfig, validatorUID uint16, ledgerOpened func(*AttemptLedger)) (state *releaseAttemptState, returnErr error) {
	seed, err := loadClientSeed(op.ClientKeySeedFile)
	if err != nil {
		return nil, fmt.Errorf("no_id %d client key: %w", op.NoID, err)
	}
	stats := NewStatsEngine(StatsConfig{
		AMin:             cfg.Policy.Verify.ReliabilityAMin,
		AlphaNumerator:   1,
		AlphaDenominator: 10,
		LatRefMillis:     4000,
	})
	if err := stats.Load(op.StateDir); err != nil {
		return nil, fmt.Errorf("no_id %d stats: %w", op.NoID, err)
	}
	ledger, err := NewAttemptLedger(op.StateDir, AttemptLedgerIdentity{
		DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: strings.ToLower(cfg.GenesisHash),
		Netuid: cfg.Netuid, ValidatorID: cfg.ValidatorID, ValidatorUID: validatorUID, NoID: op.NoID,
	}, ed25519.NewKeyFromSeed(seed))
	if err != nil {
		return nil, fmt.Errorf("no_id %d attempt ledger: %w", op.NoID, err)
	}
	transferred := false
	defer func() {
		if !transferred {
			returnErr = errors.Join(returnErr, releaseStageError(fmt.Sprintf("no_id %d refused attempt ledger shutdown", op.NoID), ledger.Close()))
		}
	}()
	if ledgerOpened != nil {
		ledgerOpened(ledger)
	}
	if err := stats.AttachAttemptLedger(ledger, op.StateDir); err != nil {
		return nil, fmt.Errorf("no_id %d attempt ledger recovery: %w", op.NoID, err)
	}
	store, err := NewProofStore(op.StateDir)
	if err != nil {
		return nil, fmt.Errorf("no_id %d proof projection: %w", op.NoID, err)
	}
	if err := store.ReconcileAttemptProofs(ledger); err != nil {
		return nil, fmt.Errorf("no_id %d proof projection reconciliation: %w", op.NoID, err)
	}
	transferred = true
	return &releaseAttemptState{stats: stats, ledger: ledger, store: store}, nil
}

func releasePriorSettlementBoundary(ctx context.Context, chain *ChainClient, snapshot *ReleaseSnapshot) (AttemptBoundary, error) {
	if ctx == nil || chain == nil || snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() || snapshot.Epoch.Sign() == 0 {
		return AttemptBoundary{}, errors.New("cannot resolve the prior settlement boundary")
	}
	startBlock, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, snapshot.BlockNumber, snapshot.BlockHash, snapshot.Epoch)
	if err != nil || startBlock == 0 {
		return AttemptBoundary{}, fmt.Errorf("current settlement start block: %w", err)
	}
	block := startBlock - 1
	hash, err := chain.BlockHashContext(ctx, block)
	if err != nil {
		return AttemptBoundary{}, fmt.Errorf("prior settlement terminal block: %w", err)
	}
	epoch, err := chainViewAtHashContext(ctx, chain, block, hash, chain.coordinator.PackCurrentEpoch(), chain.coordinator.UnpackCurrentEpoch)
	if err != nil || epoch == nil || !epoch.IsUint64() || epoch.Uint64()+1 != snapshot.Epoch.Uint64() {
		return AttemptBoundary{}, errors.New("prior settlement terminal block has the wrong epoch")
	}
	return AttemptBoundary{SettlementEpoch: epoch.Uint64(), EVMBlock: block, EVMBlockHash: attemptHex32(hash)}, nil
}

// Runtime startup shares one call path with deterministic identity-admission
// tests. Production has no observer and cannot bypass the same admission.
func startReleaseOperator(ctx context.Context, cfg *ReleaseConfig, op OperatorConfig, epochFn func() uint64, attemptResolver AttemptBoundaryResolver, attemptState *releaseAttemptState) (*releaseOperatorRuntime, error) {
	return startReleaseOperatorWithAdmission(ctx, cfg, op, epochFn, attemptResolver, attemptState, nil)
}

// A private synchronous observer exposes only the copied public key and may
// stop tests before API, JWT, strategy or transport ownership begins.
func startReleaseOperatorWithAdmission(ctx context.Context, cfg *ReleaseConfig, op OperatorConfig, epochFn func() uint64, attemptResolver AttemptBoundaryResolver, attemptState *releaseAttemptState, beforeRuntime func([32]byte) error) (*releaseOperatorRuntime, error) {
	if ctx == nil || cfg == nil || epochFn == nil || attemptResolver == nil || attemptState == nil || attemptState.stats == nil || attemptState.ledger == nil || attemptState.store == nil {
		return nil, fmt.Errorf("no_id %d prepared attempt state is incomplete", op.NoID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stats, ledger := attemptState.stats, attemptState.ledger
	store := attemptState.store
	seedAttemptInterval, err := releaseSeedAttemptInterval(cfg.Policy.Verify.HardSeedPerMinutePerSource)
	if err != nil {
		return nil, fmt.Errorf("no_id %d seed pacing: %w", op.NoID, err)
	}
	seed, err := loadClientSeed(op.ClientKeySeedFile)
	if err != nil {
		return nil, fmt.Errorf("no_id %d client key: %w", op.NoID, err)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if !bytes.Equal(publicKey, ledger.vpk) || validateAttemptLedgerIdentity(ledger.identity, publicKey) != nil {
		return nil, fmt.Errorf("no_id %d client key differs from prepared attempt ledger", op.NoID)
	}
	identity := ledger.identity
	if identity.DeploymentID != cfg.DeploymentID || identity.ChainID != cfg.ChainID || identity.GenesisHash != strings.ToLower(cfg.GenesisHash) ||
		identity.Netuid != cfg.Netuid || identity.ValidatorID != cfg.ValidatorID || identity.NoID != op.NoID {
		return nil, fmt.Errorf("no_id %d prepared attempt ledger identity differs from operator configuration", op.NoID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if beforeRuntime != nil {
		var copiedPublicKey [32]byte
		copy(copiedPublicKey[:], publicKey)
		if err := beforeRuntime(copiedPublicKey); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	artifactReader, err := NewHTTPArtifactReader(op.APIURL, cfg.DeploymentID, cfg.Netuid)
	if err != nil {
		return nil, fmt.Errorf("no_id %d artifact reader: %w", op.NoID, err)
	}
	strategySettings := connect.DefaultClientStrategySettings()
	// Processed ClientKey replies wait for a shared chain observation. Match
	// provider registration budgets while retaining each trail's own context.
	strategySettings.RequestTimeout = 120 * time.Second
	strategySettings.ConnectTimeout = 45 * time.Second
	if apiURL, err := url.Parse(op.APIURL); err == nil && apiURL.Scheme == "http" {
		if address := net.ParseIP(apiURL.Hostname()); address != nil && address.IsLoopback() {
			// Local control requests need one budget, without resilient route splits.
			strategySettings.EnableResilient = false
		}
	}
	strategy := connect.NewClientStrategy(ctx, strategySettings)
	api := sdk.NewApi(ctx, strategy, op.APIURL)
	byClientJWT, clientID, err := clientauth.LoadOrCreateClientJwt(ctx, api, op.NetworkJWTFile, op.ClientJWTFile, fmt.Sprintf("validator-%d no-%d release-1.0", cfg.ValidatorID, op.NoID))
	if err != nil {
		closeErr := api.CloseAndWait(context.Background())
		strategy.Close()
		return nil, errors.Join(fmt.Errorf("no_id %d authentication: %w", op.NoID, err), releaseStageError("authentication API shutdown", closeErr))
	}

	upload, err := newReleaseAttemptUploadV2(ctx, op, cfg.EvidenceV2.Bounds, api.GetByJwt)
	if err != nil {
		closeErr := api.CloseAndWait(context.Background())
		strategy.Close()
		return nil, errors.Join(fmt.Errorf("no_id %d attempt upload: %w", op.NoID, err), releaseStageError("upload API shutdown", closeErr))
	}
	cancelled := atomic.Bool{}
	clientSettings := connect.DefaultClientSettings()
	clientSettings.ClientKeySeed = seed
	clientOOB := connect.NewApiOutOfBandControl(ctx, strategy, byClientJWT, op.APIURL)
	identityClient := connect.NewClient(ctx, clientID, clientOOB, clientSettings)
	instanceID := connect.NewId()
	platformTransport := connect.NewPlatformTransportWithDefaults(ctx, strategy, identityClient.RouteManager(), op.ConnectURL, &connect.ClientAuth{
		ByJwt: byClientJWT, InstanceId: instanceID, AppVersion: RequireVersion(),
	})
	transport := NewTunnelTransport(ctx, strategy, TunnelTransportConfig{ApiUrl: op.APIURL, ConnectUrl: op.ConnectURL, ByClientJwt: api.GetByJwt, SourceClientId: clientID})
	refreshSub := api.AddJwtRefreshListener(clientauth.JwtRefreshListenerFunc(func(jwt string) {
		if err := clientauth.WriteToken(op.ClientJWTFile, jwt); err != nil {
			fmt.Printf("validator no_id %d JWT save failed: %v\n", op.NoID, err)
			cancelled.Store(true)
			upload.close()
			transport.Close()
		}
		clientOOB.SetByJwt(jwt)
		platformTransport.SetAuth(&connect.ClientAuth{ByJwt: jwt, InstanceId: instanceID, AppVersion: RequireVersion()})
	}))
	logoutSub := api.AddAuthLogoutListener(clientauth.AuthLogoutListenerFunc(func() {
		upload.close()
		transport.Close()
		_ = clientauth.MarkRejected(op.ClientJWTFile, op.NetworkJWTFile)
		cancelled.Store(true)
	}))
	api.StartJwtRefresh()
	var closeOnce sync.Once
	var closeErr error
	closeResources := func() error {
		closeOnce.Do(func() {
			upload.close()
			closeErr = errors.Join(closeErr, releaseStageError("tunnel transport", transport.CloseAndWait(context.Background())))
			refreshSub.Close()
			logoutSub.Close()
			closeErr = errors.Join(closeErr, releaseStageError("platform transport", platformTransport.CloseAndWait(context.Background())))
			closeErr = errors.Join(closeErr, releaseStageError("identity client", identityClient.CloseAndWait(context.Background())))
			closeErr = errors.Join(closeErr, releaseStageError("out-of-band control", clientOOB.CloseAndWait(context.Background())))
			closeErr = errors.Join(closeErr, releaseStageError("API", api.CloseAndWait(context.Background())))
			strategy.Close()
		})
		return closeErr
	}

	engine := NewTrailEngine(clientID, privateKey, transport, NewApiServerKeyRing(api), NewFindProvidersSeedPicker(api, clientID), stats, store, epochFn, TrailEngineConfig{
		M:                   cfg.Policy.Verify.TrailDepth,
		StepTimeout:         time.Duration(cfg.Policy.Verify.StepTimeoutSeconds) * time.Second,
		SeedAttemptInterval: seedAttemptInterval,
		AttemptLedger:       ledger, AttemptBoundaryResolver: attemptResolver,
	})
	keyHistoryReader, err := NewHTTPClientKeyHistoryReader(op.APIURL, func() string {
		if cancelled.Load() || ctx.Err() != nil {
			return ""
		}
		return api.GetByJwt()
	})
	if err != nil {
		return nil, errors.Join(err, closeResources())
	}
	measurement := &ReleaseMeasurementContext{
		NoID:             op.NoID,
		Stats:            stats,
		Artifacts:        artifactReader,
		ClientKeyHistory: keyHistoryReader,
		ClientKey: func(id connect.Id) ([32]byte, bool, error) {
			if cancelled.Load() {
				return [32]byte{}, false, errors.New("operator authentication is no longer valid")
			}
			sdkID, err := sdk.ParseId(id.String())
			if err != nil {
				return [32]byte{}, false, err
			}
			result, err := api.GetClientKeySyncWithContext(ctx, &sdk.GetClientKeyArgs{ClientId: sdkID})
			if err != nil {
				return [32]byte{}, false, err
			}
			if result == nil || len(result.PublicKey) != ed25519.PublicKeySize {
				return [32]byte{}, false, nil
			}
			var key [32]byte
			copy(key[:], result.PublicKey)
			return key, true, nil
		},
	}
	return &releaseOperatorRuntime{
		measurement:   measurement,
		attemptUpload: upload,
		attemptSource: attemptState.uploadSource,
		attemptLedger: ledger,
		stats:         stats,
		engine:        engine,
		close:         newReleaseOperatorClose(stats, op.StateDir, closeResources),
	}, nil
}

// dialPinnedNative tries ordered release endpoints with an independent bound
// for each context-aware dial and immutable runtime authentication attempt.
func dialPinnedNative(ctx context.Context, cfg *ReleaseConfig) (*crv4.Chain, error) {
	if ctx == nil || cfg == nil {
		return nil, errors.New("pinned native dial context is incomplete")
	}
	var errs []error
	wantGenesis, _ := parseHash32("genesis_hash", cfg.GenesisHash)
	for _, endpoint := range cfg.Substrate {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		endpointCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(cfg))
		chain, err := crv4.DialChainContext(endpointCtx, endpoint)
		if err != nil {
			cancel()
			errs = append(errs, fmt.Errorf("%s: %w", endpoint, err))
			continue
		}
		if chain.GenesisHash != typesHash(wantGenesis) {
			cancel()
			chain.API.Client.Close()
			errs = append(errs, fmt.Errorf("%s: genesis does not match release pin", endpoint))
			continue
		}
		if _, err := authenticatePinnedNativeRuntimeContext(endpointCtx, chain, cfg); err != nil {
			cancel()
			chain.API.Client.Close()
			errs = append(errs, fmt.Errorf("%s: runtime identity does not match release pin: %w", endpoint, err))
			continue
		}
		cancel()
		return chain, nil
	}
	return nil, fmt.Errorf("no pinned Substrate endpoint answered: %w", errors.Join(errs...))
}

func typesHash(value [32]byte) [32]byte { return value }

// RunRelease starts the production validator modules under a caller-owned
// lifecycle. CLIs and integration harnesses share this exact entry point.
func RunRelease(ctx context.Context, configPath string) (returnErr error) {
	return runReleaseWithActivationSetup(ctx, configPath, nil)
}

func runReleaseWithActivationSetup(ctx context.Context, configPath string, retainedSetup *ProvisionalActivationSetupV2) (returnErr error) {
	if ctx == nil {
		return errors.New("release production lifecycle context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := LoadReleaseConfig(configPath)
	if err != nil {
		return err
	}
	if retainedSetup != nil {
		if err := retainedSetup.validate(cfg, configPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "validator: provisional retained activation setup; final_acceptance=false; source_plan=%s handoff=%s\n", retainedSetup.SourcePlanHash, retainedSetup.contentHash)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	hotkeySeed, err := crv4.LoadSeedFile(cfg.HotkeySeedFile)
	if err != nil {
		return fmt.Errorf("production hotkey seed: %w", err)
	}
	hotkey, err := crv4.KeypairFromSeed(hotkeySeed)
	if err != nil {
		return err
	}
	chain, err := DialReleaseChainContext(ctx, cfg.RPC, common.HexToAddress(cfg.Coordinator))
	if err != nil {
		return err
	}
	defer chain.Close()
	native, err := dialPinnedNative(ctx, cfg)
	if err != nil {
		return err
	}
	defer native.API.Client.Close()

	var settlementEpoch atomic.Uint64
	snapshot, err := loadInitialReleaseSnapshot(ctx, chain.ReleaseSnapshotContext, waitReleaseSnapshotRetry)
	if err != nil {
		return err
	}
	settlementEpoch.Store(snapshot.Epoch.Uint64())
	var activationInputs []releaseEvidenceV2ActivationInput
	if retainedSetup != nil {
		activationInputs, err = loadReleaseEvidenceV2ActivationInputsWithRetainedSetup(ctx, cfg, chain, native, hotkey.PublicKey(), retainedSetup)
		if err != nil {
			return fmt.Errorf("reserved upload activation startup: %w", err)
		}
	}
	var validatorUID uint16
	var found bool
	if retainedSetup != nil {
		validatorUID, found, err = findProvisionalValidatorUIDAtHashContext(ctx, chain, snapshot, cfg.Netuid, hotkey.PublicKey(), activationInputs)
	} else {
		validatorUID, found, err = chain.FindUidByHotkeyAtHashContext(ctx, snapshot.BlockNumber, snapshot.BlockHash, cfg.Netuid, hotkey.PublicKey())
	}
	if err != nil || !found {
		return fmt.Errorf("release validator hotkey has no UID at finalized EVM block %d: %w", snapshot.BlockNumber, err)
	}
	if _, err := authenticateReleaseValidatorStakeContext(ctx, native, cfg, hotkey.PublicKey(), validatorUID); err != nil {
		return err
	}
	if retainedSetup == nil {
		activationInputs, err = loadReleaseEvidenceV2ActivationInputsWithRetainedSetup(ctx, cfg, chain, native, hotkey.PublicKey(), nil)
		if err != nil {
			return fmt.Errorf("reserved upload activation startup: %w", err)
		}
	}
	if len(cfg.Operators) < 2 {
		return errors.New("release V2 requires two configured public operator origins")
	}
	origins := [2]string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}
	if _, err := newReleaseEvidenceV2StartupReaders(origins, cfg.EvidenceV2.Bounds.Cut); err != nil {
		return err
	}
	serverKeys, err := readReleaseServerKeysV2(ctx, cfg)
	if err != nil {
		return fmt.Errorf("release V2 server-key startup: %w", err)
	}
	disk, err := openReleaseEvidenceV2DiskState(ctx, cfg, activationInputs, serverKeys)
	if err != nil {
		return fmt.Errorf("release V2 disk startup: %w", err)
	}
	defer func() {
		cancel()
		returnErr = errors.Join(returnErr, disk.close())
	}()
	runtimeV2, err := newReleaseRuntimeV2(ctx, cfg, chain, native, hotkey, activationInputs, serverKeys, origins, disk)
	if err != nil {
		return fmt.Errorf("release V2 semantic startup: %w", err)
	}
	boundaryCtx := ctx
	if retainedSetup != nil {
		// Scope longer reads to shared preparation; trail callers keep their
		// original deadline and never inherit this private owner context.
		boundaryCtx = context.WithValue(ctx, provisionalBoundaryReadBudgetKey{}, true)
	}
	attemptBoundaryResolver := newReleaseAttemptBoundaryResolver(boundaryCtx, chain, cfg)
	defer attemptBoundaryResolver.close()
	runtimeV2.publishEpoch = func(epoch uint64) {
		attemptBoundaryResolver.invalidateLatest()
		settlementEpoch.Store(epoch)
	}

	var runtimes []*releaseOperatorRuntime
	workersOwnResources := false
	defer func() {
		if !workersOwnResources {
			cancel()
			for _, runtime := range runtimes {
				returnErr = errors.Join(returnErr, runtime.close())
			}
		}
	}()
	for _, op := range cfg.Operators {
		runtime, err := startReleaseOperator(ctx, cfg, op, settlementEpoch.Load, attemptBoundaryResolver.Resolve, disk.states[op.NoID])
		if err != nil {
			return err
		}
		runtimes = append(runtimes, runtime)
	}
	if err := runtimeV2.attach(runtimes); err != nil {
		return err
	}
	measurements := make([]*ReleaseMeasurementContext, len(runtimes))
	for index, runtime := range runtimes {
		measurements[index] = runtime.measurement
	}
	steerer, err := newReleaseSteererV2(cfg, chain, native, hotkey, measurements, runtimeV2)
	if err != nil {
		return fmt.Errorf("release V2 native startup: %w", err)
	}
	if err := advanceInitialReleaseWithRetry(ctx, snapshot, chain.ReleaseSnapshotContext, runtimeV2.advance, waitReleaseSnapshotRetry); err != nil {
		return fmt.Errorf("release V2 initial terminal publication: %w", err)
	}
	workersOwnResources = true
	return runReleaseOperatorWorkers(ctx, cancel, cfg, runtimes, releaseRuntimeOperations{
		refresh: func(ctx context.Context) error {
			return runReleaseSettlementRefresh(ctx, time.Duration(cfg.PollSeconds)*time.Second, chain.ReleaseSnapshotContext, runtimeV2.advance, func(*ReleaseSnapshot) {}, waitReleaseSnapshotRetry)
		},
		newSteerer: func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
			return steerer, nil
		},
		running: func() {
			fmt.Printf("validator release 1.0 running: validator=%d netuid=%d hotkey=%s operators=%d\n", cfg.ValidatorID, cfg.Netuid, hotkey.Address(), len(runtimes))
		},
	})
}

func runReleaseConfig(configPath string) {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	if err := RunRelease(event.Ctx(), configPath); err != nil {
		panic(err)
	}
}
