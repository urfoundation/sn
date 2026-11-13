// Command trail-probe runs a few validator verification trails exactly as a
// production operator does, with verbose transport logging and the full error
// chain printed, so a trail that fails before reaching the operator server can
// be diagnosed. It reads the validator config, the operator's client JWT and
// client key seed (never printed), and wires the same seed picker, tunnel
// transport and trail engine as `validator run`. It keeps no attempt ledger or
// request journal and writes proofs only to its own scratch state directory,
// so it does not touch the validator's durable state.
//
//	trail-probe -config=/etc/sn-validator/validator.yml [-no_id=1] [-trails=1]
//	    [-hop=<provider client id>] [-state_dir=<dir>] [-timeout=5m] [-v=2]
//
// Run it as the validator's service user so it reads the same credentials:
//
//	sudo setpriv --reuid=sn-validator --regid=sn-validator --init-groups --reset-env -- trail-probe -config=... -v=2
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/glog/v2026"
	"github.com/urnetwork/sdk/v2026"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// Only the fields a trail needs; the full config is validated by `validator run`.
type probeConfig struct {
	DeploymentID    string `yaml:"deployment_id"`
	ChainID         uint64 `yaml:"chain_id"`
	GenesisHash     string `yaml:"genesis_hash"`
	Netuid          uint16 `yaml:"netuid"`
	Coordinator     string `yaml:"coordinator"`
	SettlementVault string `yaml:"settlement_vault"`
	PolicyHash      string `yaml:"policy_hash"`
	TrailDepth      int    `yaml:"trail_depth"`
	Policy          struct {
		Verify struct {
			TrailDepth         int `yaml:"trail_depth"`
			StepTimeoutSeconds int `yaml:"step_timeout_seconds"`
		} `yaml:"verify"`
	} `yaml:"policy"`
	Operators []struct {
		NoID              uint64 `yaml:"no_id"`
		APIURL            string `yaml:"api_url"`
		ConnectURL        string `yaml:"connect_url"`
		ClientJWTFile     string `yaml:"client_jwt_file"`
		ClientKeySeedFile string `yaml:"client_key_seed_file"`
	} `yaml:"operators"`
}

func main() {
	configPath := flag.String("config", "/etc/sn-validator/validator.yml", "validator config")
	noID := flag.Uint64("no_id", 1, "operator no_id whose credentials to use")
	trails := flag.Int("trails", 1, "number of trails to run, one after another")
	hop := flag.String("hop", "", "fixed seed hop client id (default: the validator's find-providers2 seed picker)")
	rankMode := flag.String("rank_mode", "quality", "find-providers2 rank_mode for the seed pick; quality is what the validator sends")
	forceMinimum := flag.Bool("force_minimum", true, "find-providers2 force_minimum for the seed pick; true is what the validator sends")
	locationID := flag.String("location_id", "", "seed pick from this location instead of best_available")
	stateDir := flag.String("state_dir", "", "scratch proof directory (default: a new temporary directory)")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall deadline")
	flag.Parse()
	if err := run(*configPath, *noID, *trails, *hop, *rankMode, *forceMinimum, *locationID, *stateDir, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "trail-probe:", err)
		os.Exit(1)
	}
}

func run(configPath string, noID uint64, trails int, fixedHop string, rankMode string, forceMinimum bool, locationID string, stateDir string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg probeConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	var op *struct {
		NoID              uint64 `yaml:"no_id"`
		APIURL            string `yaml:"api_url"`
		ConnectURL        string `yaml:"connect_url"`
		ClientJWTFile     string `yaml:"client_jwt_file"`
		ClientKeySeedFile string `yaml:"client_key_seed_file"`
	}
	for i := range cfg.Operators {
		if cfg.Operators[i].NoID == noID {
			op = &cfg.Operators[i]
		}
	}
	if op == nil {
		return fmt.Errorf("config has no operator no_id %d", noID)
	}
	depth := cfg.Policy.Verify.TrailDepth
	if depth == 0 {
		depth = cfg.TrailDepth
	}
	stepTimeout := time.Duration(cfg.Policy.Verify.StepTimeoutSeconds) * time.Second

	tokenBytes, err := os.ReadFile(op.ClientJWTFile)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(tokenBytes))
	clientID, err := jwtClientID(token)
	if err != nil {
		return err
	}
	seed, err := crv4.LoadRawOrBareHexSeedFile(op.ClientKeySeedFile)
	if err != nil {
		return err
	}
	key := ed25519.NewKeyFromSeed(seed[:])

	domain := protocol.ClientKeyHistoryDomain{
		ChainID:          cfg.ChainID,
		GenesisHash:      common.HexToHash(cfg.GenesisHash),
		Netuid:           cfg.Netuid,
		Coordinator:      common.HexToAddress(cfg.Coordinator),
		SettlementVault:  common.HexToAddress(cfg.SettlementVault),
		DeploymentIDHash: sha256.Sum256([]byte(cfg.DeploymentID)),
		PolicyHash:       common.HexToHash(cfg.PolicyHash),
		NoID:             noID,
	}
	domainHash, err := domain.Digest()
	if err != nil {
		return fmt.Errorf("close report domain: %w", err)
	}

	if stateDir == "" {
		stateDir, err = os.MkdirTemp("", "trail-probe-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stateDir)
	}

	fmt.Printf("operator no_id=%d client_id=%s api=%s connect=%s depth=%d step_timeout=%s\n", noID, clientID, op.APIURL, op.ConnectURL, depth, stepTimeout)

	// The production strategy settings (validator release_run.go).
	strategySettings := connect.DefaultClientStrategySettings()
	strategySettings.RequestTimeout = 120 * time.Second
	strategySettings.ConnectTimeout = 45 * time.Second
	strategy := connect.NewClientStrategy(ctx, strategySettings)
	api := sdk.NewApi(ctx, strategy, op.APIURL)
	api.SetByJwt(token)

	transport := validator.NewTunnelTransport(ctx, strategy, validator.TunnelTransportConfig{
		CloseReportDomainHash: domainHash,
		ApiUrl:                op.APIURL,
		ConnectUrl:            op.ConnectURL,
		ByClientJwt:           func() string { return token },
		SourceClientId:        clientID,
	})
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Minute)
		defer closeCancel()
		if err := transport.CloseAndWait(closeCtx); err != nil {
			fmt.Println("transport close:", err)
		}
	}()

	picker := validator.NewFindProvidersSeedPicker(api, clientID)
	if rankMode != "quality" || !forceMinimum || locationID != "" {
		picker = seedPicker(api, clientID, rankMode, forceMinimum, locationID)
	}
	if fixedHop != "" {
		hopID, err := connect.ParseId(fixedHop)
		if err != nil {
			return fmt.Errorf("hop: %w", err)
		}
		picker = func(context.Context) (connect.Id, error) { return hopID, nil }
	}
	tracedPicker := func(ctx context.Context) (connect.Id, error) {
		hop, err := picker(ctx)
		if err != nil {
			fmt.Printf("  seed pick failed: %v\n", err)
		} else {
			fmt.Printf("  seed hop %s\n", hop)
		}
		return hop, err
	}

	store, err := validator.NewProofStore(stateDir)
	if err != nil {
		return err
	}
	engine := validator.NewTrailEngine(
		clientID,
		key,
		transport,
		validator.NewApiServerKeyRing(api),
		tracedPicker,
		validator.NewStatsEngine(validator.StatsConfig{}),
		store,
		func() uint64 { return 0 },
		validator.TrailEngineConfig{M: depth, StepTimeout: stepTimeout},
	)

	failed := 0
	for i := 1; i <= trails; i++ {
		fmt.Printf("trail %d/%d\n", i, trails)
		start := time.Now()
		record, err := engine.RunTrail(ctx)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			failed++
			var trailErr *validator.TrailError
			if errors.As(err, &trailErr) {
				fmt.Printf("  FAILED after %s: kind=%s hop=%s\n  error: %v\n", elapsed, trailErrorKind(trailErr.Kind), trailErr.Hop, trailErr.Err)
			} else {
				fmt.Printf("  FAILED after %s: %v\n", elapsed, err)
			}
			continue
		}
		fmt.Printf("  completed after %s: depth=%d epoch=%d\n", elapsed, record.M, record.Epoch)
	}
	glog.Flush()
	if failed > 0 {
		return fmt.Errorf("%d of %d trails failed", failed, trails)
	}
	return nil
}

// The validator's seed request (validator/transport.go NewFindProvidersSeedPicker)
// with a different rank mode or force_minimum, to compare what each returns.
func seedPicker(api *sdk.Api, selfClientID connect.Id, rankMode string, forceMinimum bool, locationID string) validator.SeedPicker {
	return func(ctx context.Context) (connect.Id, error) {
		selfID, err := sdk.ParseId(selfClientID.String())
		if err != nil {
			return connect.Id{}, err
		}
		specs := sdk.NewProviderSpecList()
		if locationID == "" {
			specs.Add(&sdk.ProviderSpec{BestAvailable: true})
		} else {
			location, err := sdk.ParseId(locationID)
			if err != nil {
				return connect.Id{}, fmt.Errorf("location_id: %w", err)
			}
			specs.Add(&sdk.ProviderSpec{LocationId: location})
		}
		exclude := sdk.NewIdList()
		exclude.Add(selfID)
		result, err := api.FindProviders2SyncWithContext(ctx, &sdk.FindProviders2Args{
			Specs:            specs,
			Count:            8,
			ExcludeClientIds: exclude,
			RankMode:         rankMode,
			ForceMinimum:     forceMinimum,
		})
		if err != nil {
			return connect.Id{}, err
		}
		if result.ProviderStats == nil || result.ProviderStats.Len() == 0 {
			return connect.Id{}, errors.New("no seed providers available")
		}
		fmt.Printf("  find-providers2 rank_mode=%q force_minimum=%t location=%q returned %d providers\n", rankMode, forceMinimum, locationID, result.ProviderStats.Len())
		for i := 0; i < result.ProviderStats.Len(); i++ {
			if provider := result.ProviderStats.Get(i); provider != nil && provider.ClientId != nil {
				if id, err := connect.ParseId(provider.ClientId.String()); err == nil && id != selfClientID {
					return id, nil
				}
			}
		}
		return connect.Id{}, errors.New("no seed providers available")
	}
}

// The client id claim only; the token itself is never printed.
func jwtClientID(token string) (connect.Id, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return connect.Id{}, errors.New("client jwt is not a three-part token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return connect.Id{}, fmt.Errorf("client jwt payload: %w", err)
	}
	var claims struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return connect.Id{}, fmt.Errorf("client jwt claims: %w", err)
	}
	if claims.ClientID == "" {
		return connect.Id{}, errors.New("client jwt has no client_id claim")
	}
	return connect.ParseId(claims.ClientID)
}

func trailErrorKind(kind validator.TrailErrorKind) string {
	switch kind {
	case validator.TrailErrorSeed:
		return "seed"
	case validator.TrailErrorHop:
		return "hop"
	case validator.TrailErrorProtocol:
		return "protocol"
	case validator.TrailErrorUnknownOutcome:
		return "unknown_final"
	default:
		return fmt.Sprintf("%d", kind)
	}
}
