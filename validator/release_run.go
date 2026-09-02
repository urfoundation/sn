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
	releaseSnapshotStartupAttempts   = 5
	releaseSnapshotStartupRetryDelay = 2 * time.Second
)

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

func startReleaseOperator(ctx context.Context, cfg *ReleaseConfig, op OperatorConfig, epochFn func() uint64) (*releaseOperatorRuntime, error) {
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

	stats := NewStatsEngine(StatsConfig{
		AMin:             cfg.Policy.Verify.ReliabilityAMin,
		AlphaNumerator:   1,
		AlphaDenominator: 10,
		LatRefMillis:     4000,
	})
	if err := stats.Load(op.StateDir); err != nil {
		closeResources()
		return nil, fmt.Errorf("no_id %d stats: %w", op.NoID, err)
	}
	store, err := NewProofStore(op.StateDir)
	if err != nil {
		closeResources()
		return nil, err
	}
	transport := NewTunnelTransport(ctx, strategy, TunnelTransportConfig{ApiUrl: op.APIURL, ConnectUrl: op.ConnectURL, ByClientJwt: api.GetByJwt, SourceClientId: clientID})
	engine := NewTrailEngine(clientID, ed25519.NewKeyFromSeed(seed), transport, NewApiServerKeyRing(api), NewFindProvidersSeedPicker(api, clientID), stats, store, epochFn, TrailEngineConfig{
		M:                   cfg.Policy.Verify.TrailDepth,
		StepTimeout:         time.Duration(cfg.Policy.Verify.StepTimeoutSeconds) * time.Second,
		SeedAttemptInterval: seedAttemptInterval,
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

func dialPinnedNative(cfg *ReleaseConfig) (*crv4.Chain, error) {
	var errs []error
	wantGenesis, _ := parseHash32("genesis_hash", cfg.GenesisHash)
	for _, endpoint := range cfg.Substrate {
		chain, err := crv4.DialChain(endpoint)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", endpoint, err))
			continue
		}
		if chain.GenesisHash != typesHash(wantGenesis) || uint32(chain.Runtime.SpecVersion) != cfg.RuntimeSpec {
			chain.API.Client.Close()
			errs = append(errs, fmt.Errorf("%s: genesis/runtime does not match release pin", endpoint))
			continue
		}
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
	chain, err := DialReleaseChain(cfg.RPC, common.HexToAddress(cfg.Coordinator))
	if err != nil {
		return err
	}
	defer chain.Close()
	native, err := dialPinnedNative(cfg)
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
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.PollSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if snapshot, err := chain.ReleaseSnapshotContext(ctx); err == nil {
					settlementEpoch.Store(snapshot.Epoch.Uint64())
				} else if ctx.Err() == nil {
					fmt.Printf("validator release snapshot refresh: %v\n", err)
				}
			}
		}
	}()

	var runtimes []*releaseOperatorRuntime
	for _, op := range cfg.Operators {
		runtime, err := startReleaseOperator(ctx, cfg, op, settlementEpoch.Load)
		if err != nil {
			for _, started := range runtimes {
				started.close()
			}
			return err
		}
		runtimes = append(runtimes, runtime)
		go runtime.engine.Run(ctx, op.Concurrency)
	}
	defer func() {
		for _, runtime := range runtimes {
			runtime.close()
		}
	}()
	measurements := make([]*ReleaseMeasurementContext, len(runtimes))
	for i, runtime := range runtimes {
		measurements[i] = runtime.measurement
	}
	steerer, err := NewReleaseSteerer(cfg, chain, native, hotkey, measurements)
	if err != nil {
		return err
	}
	go steerer.Run(ctx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for i, runtime := range runtimes {
					if err := runtime.stats.Save(cfg.Operators[i].StateDir); err != nil {
						fmt.Printf("validator no_id %d stats save: %v\n", cfg.Operators[i].NoID, err)
					}
				}
			}
		}
	}()
	fmt.Printf("validator release 1.0 running: validator=%d netuid=%d hotkey=%s operators=%d\n", cfg.ValidatorID, cfg.Netuid, hotkey.Address(), len(runtimes))
	<-ctx.Done()
	return nil
}

func runReleaseConfig(configPath string) {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	if err := RunRelease(event.Ctx(), configPath); err != nil {
		panic(err)
	}
}
