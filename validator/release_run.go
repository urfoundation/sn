package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	measurement *ReleaseMeasurementContext
	stats       *StatsEngine
	engine      *TrailEngine
	close       func()
}

type releaseTrailRunner interface {
	Run(context.Context, int) error
}

func reportReleaseTrailEngineError(ctx context.Context, runner releaseTrailRunner, noID uint64, concurrency int, output chan<- error) {
	if err := runner.Run(ctx, concurrency); err != nil && ctx.Err() == nil {
		output <- fmt.Errorf("validator no_id %d trail engine: %w", noID, err)
	}
}

type releaseAttemptState struct {
	stats  *StatsEngine
	ledger *AttemptLedger
	store  *ProofStore
}

func loadClientSeed(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) == ed25519.SeedSize {
		return append([]byte(nil), b...), nil
	}
	decoded, decodeErr := hex.DecodeString(string(bytesTrimSpace(b)))
	if decodeErr == nil && len(decoded) == ed25519.SeedSize {
		return decoded, nil
	}
	return nil, fmt.Errorf("%s: expected a raw or hex %d-byte Ed25519 seed", path, ed25519.SeedSize)
}

func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\n' || b[start] == '\r' || b[start] == '\t') {
		start++
	}
	for start < end && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == '\t') {
		end--
	}
	return b[start:end]
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

func newReleaseAttemptBoundaryResolver(chain *ChainClient, cfg *ReleaseConfig) *cachedAttemptBoundaryResolver {
	return newCachedAttemptBoundaryResolver(&chainAttemptBoundaryRPC{chain: chain, netuid: cfg.Netuid})
}

func loadReleaseAttemptState(cfg *ReleaseConfig, op OperatorConfig, validatorUID uint16) (*releaseAttemptState, error) {
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

func startReleaseOperator(ctx context.Context, cfg *ReleaseConfig, op OperatorConfig, epochFn func() uint64, attemptResolver AttemptBoundaryResolver, attemptState *releaseAttemptState) (*releaseOperatorRuntime, error) {
	seedAttemptInterval, err := releaseSeedAttemptInterval(cfg.Policy.Verify.HardSeedPerMinutePerSource)
	if err != nil {
		return nil, fmt.Errorf("no_id %d seed pacing: %w", op.NoID, err)
	}
	seed, err := loadClientSeed(op.ClientKeySeedFile)
	if err != nil {
		return nil, fmt.Errorf("no_id %d client key: %w", op.NoID, err)
	}
	artifactReader, err := NewHTTPArtifactReader(op.APIURL, cfg.DeploymentID, cfg.Netuid)
	if err != nil {
		return nil, fmt.Errorf("no_id %d artifact reader: %w", op.NoID, err)
	}
	strategy := connect.NewClientStrategyWithDefaults(ctx)
	api := sdk.NewApi(ctx, strategy, op.APIURL)
	byClientJWT, clientID, err := clientauth.LoadOrCreateClientJwt(ctx, api, op.NetworkJWTFile, op.ClientJWTFile, fmt.Sprintf("validator-%d no-%d release-1.0", cfg.ValidatorID, op.NoID))
	if err != nil {
		_ = api.CloseAndWait(context.Background())
		strategy.Close()
		return nil, fmt.Errorf("no_id %d authentication: %w", op.NoID, err)
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
	refreshSub := api.AddJwtRefreshListener(clientauth.JwtRefreshListenerFunc(func(jwt string) {
		if err := clientauth.WriteToken(op.ClientJWTFile, jwt); err != nil {
			fmt.Printf("validator no_id %d JWT save failed: %v\n", op.NoID, err)
			cancelled.Store(true)
		}
		clientOOB.SetByJwt(jwt)
		platformTransport.SetAuth(&connect.ClientAuth{ByJwt: jwt, InstanceId: instanceID, AppVersion: RequireVersion()})
	}))
	logoutSub := api.AddAuthLogoutListener(clientauth.AuthLogoutListenerFunc(func() {
		_ = clientauth.MarkRejected(op.ClientJWTFile, op.NetworkJWTFile)
		cancelled.Store(true)
	}))
	api.StartJwtRefresh()
	var closeOnce sync.Once
	closeResources := func() {
		closeOnce.Do(func() {
			refreshSub.Close()
			logoutSub.Close()
			_ = platformTransport.CloseAndWait(context.Background())
			_ = identityClient.CloseAndWait(context.Background())
			_ = clientOOB.CloseAndWait(context.Background())
			_ = api.CloseAndWait(context.Background())
			strategy.Close()
		})
	}

	if epochFn == nil || attemptResolver == nil || attemptState == nil || attemptState.stats == nil || attemptState.ledger == nil || attemptState.store == nil {
		closeResources()
		return nil, fmt.Errorf("no_id %d prepared attempt state is incomplete", op.NoID)
	}
	stats, ledger := attemptState.stats, attemptState.ledger
	store := attemptState.store
	transport := NewTunnelTransport(ctx, strategy, TunnelTransportConfig{ApiUrl: op.APIURL, ConnectUrl: op.ConnectURL, ByClientJwt: api.GetByJwt, SourceClientId: clientID})
	engine := NewTrailEngine(clientID, ed25519.NewKeyFromSeed(seed), transport, NewApiServerKeyRing(api), NewFindProvidersSeedPicker(api, clientID), stats, store, epochFn, TrailEngineConfig{
		M:                   cfg.Policy.Verify.TrailDepth,
		StepTimeout:         time.Duration(cfg.Policy.Verify.StepTimeoutSeconds) * time.Second,
		SeedAttemptInterval: seedAttemptInterval,
		AttemptLedger:       ledger, AttemptBoundaryResolver: attemptResolver,
	})
	measurement := &ReleaseMeasurementContext{
		NoID:      op.NoID,
		Stats:     stats,
		Artifacts: artifactReader,
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
		measurement: measurement,
		stats:       stats,
		engine:      engine,
		close: func() {
			_ = stats.Save(op.StateDir)
			closeResources()
		},
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
func RunRelease(ctx context.Context, configPath string) error {
	cfg, err := LoadReleaseConfig(configPath)
	if err != nil {
		return err
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
	validatorUID, found, err := chain.FindUidByHotkeyAtHashContext(ctx, snapshot.BlockNumber, snapshot.BlockHash, cfg.Netuid, hotkey.PublicKey())
	if err != nil || !found {
		return fmt.Errorf("release validator hotkey has no UID at finalized EVM block %d: %w", snapshot.BlockNumber, err)
	}
	settlementParticipants := make([]AttemptSettlementParticipant, len(cfg.Operators))
	for index, operator := range cfg.Operators {
		settlementParticipants[index] = AttemptSettlementParticipant{NoID: operator.NoID, StateDir: operator.StateDir}
	}
	if err := RecoverAttemptSettlementEpoch(cfg.StateDir, settlementParticipants); err != nil {
		return fmt.Errorf("recover validator settlement transaction: %w", err)
	}
	attemptStates := make(map[uint64]*releaseAttemptState, len(cfg.Operators))
	for index, operator := range cfg.Operators {
		state, err := loadReleaseAttemptState(cfg, operator, validatorUID)
		if err != nil {
			return err
		}
		attemptStates[operator.NoID] = state
		settlementParticipants[index].Stats = state.stats
	}
	advanceSettlement := func(ctx context.Context, snapshot *ReleaseSnapshot) error {
		return advanceReleaseSettlementSnapshot(ctx, cfg.StateDir, snapshot, settlementParticipants, func(ctx context.Context, snapshot *ReleaseSnapshot) (AttemptBoundary, error) {
			return releasePriorSettlementBoundary(ctx, chain, snapshot)
		})
	}
	if err := advanceSettlement(ctx, snapshot); err != nil {
		return fmt.Errorf("advance validator settlement transaction: %w", err)
	}
	attemptBoundaryResolver := newReleaseAttemptBoundaryResolver(chain, cfg)

	var runtimes []*releaseOperatorRuntime
	for _, op := range cfg.Operators {
		runtime, err := startReleaseOperator(ctx, cfg, op, settlementEpoch.Load, attemptBoundaryResolver.Resolve, attemptStates[op.NoID])
		if err != nil {
			for _, started := range runtimes {
				started.close()
			}
			return err
		}
		runtimes = append(runtimes, runtime)
	}
	runtimeErrors := make(chan error, len(runtimes)+3)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
		for _, runtime := range runtimes {
			runtime.close()
		}
	}()
	workers.Go(func() {
		err := runReleaseSettlementRefresh(ctx, time.Duration(cfg.PollSeconds)*time.Second, chain.ReleaseSnapshotContext, func(ctx context.Context, snapshot *ReleaseSnapshot) error {
			return advanceReleaseSettlementSnapshotWithMode(ctx, cfg.StateDir, snapshot, settlementParticipants, func(ctx context.Context, snapshot *ReleaseSnapshot) (AttemptBoundary, error) {
				return releasePriorSettlementBoundary(ctx, chain, snapshot)
			}, false)
		}, func(snapshot *ReleaseSnapshot) {
			if settlementEpoch.Load() < snapshot.Epoch.Uint64() {
				attemptBoundaryResolver.invalidateLatest()
				settlementEpoch.Store(snapshot.Epoch.Uint64())
			}
		}, waitReleaseSnapshotRetry)
		if err != nil && ctx.Err() == nil {
			runtimeErrors <- err
		}
	})
	for index, runtime := range runtimes {
		operatorID := cfg.Operators[index].NoID
		concurrency := cfg.Operators[index].Concurrency
		workers.Go(func() { reportReleaseTrailEngineError(ctx, runtime.engine, operatorID, concurrency, runtimeErrors) })
	}
	measurements := make([]*ReleaseMeasurementContext, len(runtimes))
	for i, runtime := range runtimes {
		measurements[i] = runtime.measurement
	}
	steerer, err := NewReleaseSteerer(cfg, chain, native, hotkey, measurements)
	if err != nil {
		return err
	}
	workers.Go(func() {
		if err := steerer.Run(ctx); err != nil {
			runtimeErrors <- err
		}
	})
	workers.Go(func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for i, runtime := range runtimes {
					if err := runtime.stats.Save(cfg.Operators[i].StateDir); err != nil {
						runtimeErrors <- fmt.Errorf("validator no_id %d stats save: %w", cfg.Operators[i].NoID, err)
						return
					}
				}
			}
		}
	})
	fmt.Printf("validator release 1.0 running: validator=%d netuid=%d hotkey=%s operators=%d\n", cfg.ValidatorID, cfg.Netuid, hotkey.Address(), len(runtimes))
	select {
	case <-ctx.Done():
		return nil
	case err := <-runtimeErrors:
		return err
	}
}

func runReleaseConfig(configPath string) {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	if err := RunRelease(event.Ctx(), configPath); err != nil {
		panic(err)
	}
}
