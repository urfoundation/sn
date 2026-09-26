//go:build linux || darwin

// Native subnet lifecycle commands for an independent validator: init (seed
// files), register (burned registration), stake add, activate (evidence_v2
// inputs) and the registration/activation portion of status. Every mutating
// command is a dry run unless --apply is given; every chain read and every
// signature happens against the runtime identity the release config pins.
package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

// ed25519PublicKey derives the vpk of a raw 32-byte client key seed.
func ed25519PublicKey(seed []byte) ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
}

// The observed netuid-521 registration fee was 2,131,733 rao; the default
// ceiling leaves headroom without letting a runtime multiplier drain a key.
const defaultNativeFeeLimitRao = uint64(10_000_000)

// releaseNativeRuntimeIdentity is the artifact the configuration pins.
func releaseNativeRuntimeIdentity(cfg *ReleaseConfig) crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{
		Version:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion},
		CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash,
	}
}

// loadReleaseHotkey never creates the production hotkey; init does.
func loadReleaseHotkey(cfg *ReleaseConfig) (*crv4.Keypair, error) {
	seed, err := crv4.LoadSeedFile(cfg.HotkeySeedFile)
	if err != nil {
		return nil, fmt.Errorf("production hotkey seed %s: %w (run `validator init --config=<path>` to create it)", cfg.HotkeySeedFile, err)
	}
	return crv4.KeypairFromSeed(seed)
}

// The native transaction journal lives beside the validator's other state.
func openReleaseNativeJournal(cfg *ReleaseConfig) (*snchain.Journal, error) {
	return snchain.OpenJournal(filepath.Join(cfg.StateDir, "native"))
}

func nativeCommandContext() (context.Context, context.CancelFunc) {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	return context.WithCancel(event.Ctx())
}

func optBool(opts docopt.Opts, key string) bool {
	value, _ := opts.Bool(key)
	return value
}

func exitOnError(command string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", command, err)
		os.Exit(1)
	}
}

// --- init ---

func initCommand(opts docopt.Opts) {
	exitOnError("validator init", runInit(opts, os.Stdout))
}

func runInit(opts docopt.Opts, output *os.File) error {
	if configPath := optString(opts, "--config", ""); configPath != "" {
		cfg, err := LoadReleaseConfigPreActivation(configPath)
		if err != nil {
			return err
		}
		if err := initHotkeySeed(cfg.HotkeySeedFile, output); err != nil {
			return err
		}
		for _, operator := range cfg.Operators {
			if err := initClientKeySeed(operator.NoID, operator.ClientKeySeedFile, output); err != nil {
				return err
			}
		}
		return nil
	}
	stateDir, hotkeySeedFile := initSeedPaths(opts)
	if stateDir == "" {
		return errors.New("--state_dir or --config is required")
	}
	if err := initHotkeySeed(hotkeySeedFile, output); err != nil {
		return err
	}
	noIDs := optStringList(opts, "--no_id")
	if len(noIDs) == 0 {
		seed, created, err := loadOrCreateVpkSeed(stateDir)
		if err != nil {
			return err
		}
		state := "existing"
		if created {
			state = "created"
		}
		key := ed25519PublicKey(seed)
		fmt.Fprintf(output, "client key seed: %s (%s)\nvpk: 0x%s\n", filepath.Join(stateDir, vpkSeedFileName), state, hex.EncodeToString(key))
		return nil
	}
	seen := map[uint64]bool{}
	for _, raw := range noIDs {
		noID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || noID == 0 || seen[noID] {
			return fmt.Errorf("--no_id %q is not a distinct positive operator id", raw)
		}
		seen[noID] = true
		if err := initClientKeySeed(noID, filepath.Join(stateDir, fmt.Sprintf("no-%d", noID), "client.key"), output); err != nil {
			return err
		}
	}
	return nil
}

// initSeedPaths resolves the state-dir form's paths. The docopt defaults
// ("~/.urnetwork/validator", "<state_dir>/…") are placeholders shared with the
// flag-mode commands; the state-dir form requires an explicit --state_dir and
// derives the hotkey seed path from it unless --hotkey_seed_file is given.
func initSeedPaths(opts docopt.Opts) (string, string) {
	stateDir := optString(opts, "--state_dir", "")
	if stateDir == "" || stateDir == "~/.urnetwork/validator" {
		return "", ""
	}
	stateDir = expandHome(stateDir)
	hotkeySeedFile := optString(opts, "--hotkey_seed_file", "")
	if hotkeySeedFile == "" || strings.HasPrefix(hotkeySeedFile, "<state_dir>") {
		return stateDir, filepath.Join(stateDir, defaultHotkeySeedFileName)
	}
	return stateDir, expandHome(hotkeySeedFile)
}

func initHotkeySeed(path string, output *os.File) error {
	seed, created, err := crv4.LoadOrCreateSeedFile(path)
	if err != nil {
		return fmt.Errorf("hotkey seed %s: %w", path, err)
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return err
	}
	state := "existing"
	if created {
		state = "created"
	}
	fmt.Fprintf(output, "hotkey seed: %s (%s, mode 0600)\nhotkey ss58: %s\nhotkey pubkey: 0x%x\n", path, state, hotkey.Address(), hotkey.PublicKey())
	return nil
}

func initClientKeySeed(noID uint64, path string, output *os.File) error {
	seed, created, err := crv4.LoadOrCreateRawSeedFile(path)
	if err != nil {
		return fmt.Errorf("no_id %d client key seed %s: %w", noID, path, err)
	}
	state := "existing"
	if created {
		state = "created"
	}
	fmt.Fprintf(output, "client key seed (no_id %d): %s (%s, mode 0600)\nvpk (no_id %d): 0x%s\n", noID, path, state, noID, hex.EncodeToString(ed25519PublicKey(seed[:])))
	return nil
}

// --- register ---

func registerCommand(opts docopt.Opts) {
	exitOnError("validator register", runRegister(opts))
}

func runRegister(opts docopt.Opts) error {
	cfg, err := LoadReleaseConfigPreActivation(optString(opts, "--config", ""))
	if err != nil {
		return err
	}
	hotkey, err := loadReleaseHotkey(cfg)
	if err != nil {
		return err
	}
	coldkey, err := snchain.LoadKeypairFile(expandHome(optString(opts, "--coldkey_seed_file", "")))
	if err != nil {
		return fmt.Errorf("coldkey seed: %w", err)
	}
	ctx, cancel := nativeCommandContext()
	defer cancel()
	native, err := dialPinnedNative(ctx, cfg)
	if err != nil {
		return err
	}
	defer native.API.Client.Close()
	journal, err := openReleaseNativeJournal(cfg)
	if err != nil {
		return err
	}
	_, err = snchain.RegisterHotkey(ctx, native, snchain.RegisterRequest{
		Command: "validator register", Netuid: cfg.Netuid, Hotkey: hotkey.PublicKey(), Coldkey: coldkey,
		BurnLimitRao: optUint64(opts, "--burn_limit_rao", 0), FeeLimitRao: optUint64(opts, "--fee_limit_rao", defaultNativeFeeLimitRao),
		Allowed: []crv4.RuntimeArtifactIdentity{releaseNativeRuntimeIdentity(cfg)}, Journal: journal, Apply: optBool(opts, "--apply"), Output: os.Stdout,
	})
	return err
}

// --- stake add ---

func stakeAddCommand(opts docopt.Opts) {
	exitOnError("validator stake add", runStakeAdd(opts))
}

func runStakeAdd(opts docopt.Opts) error {
	cfg, err := LoadReleaseConfigPreActivation(optString(opts, "--config", ""))
	if err != nil {
		return err
	}
	hotkey, err := loadReleaseHotkey(cfg)
	if err != nil {
		return err
	}
	coldkey, err := snchain.LoadKeypairFile(expandHome(optString(opts, "--coldkey_seed_file", "")))
	if err != nil {
		return fmt.Errorf("coldkey seed: %w", err)
	}
	amount := optUint64(opts, "--amount_rao", 0)
	if amount == 0 {
		return errors.New("--amount_rao must be a positive TAO amount in rao")
	}
	ctx, cancel := nativeCommandContext()
	defer cancel()
	native, err := dialPinnedNative(ctx, cfg)
	if err != nil {
		return err
	}
	defer native.API.Client.Close()
	journal, err := openReleaseNativeJournal(cfg)
	if err != nil {
		return err
	}
	_, err = snchain.AddStake(ctx, native, snchain.AddStakeRequest{
		Command: "validator stake add", Netuid: cfg.Netuid, Hotkey: hotkey.PublicKey(), Coldkey: coldkey, AmountRao: amount,
		LimitPriceRao: optUint64(opts, "--limit_price_rao", 0), AllowPartial: optBool(opts, "--allow_partial"),
		FeeLimitRao: optUint64(opts, "--fee_limit_rao", defaultNativeFeeLimitRao),
		Allowed:     []crv4.RuntimeArtifactIdentity{releaseNativeRuntimeIdentity(cfg)}, Journal: journal, Apply: optBool(opts, "--apply"), Output: os.Stdout,
	})
	return err
}

// --- activate ---

func activateCommand(opts docopt.Opts) {
	ctx, cancel := nativeCommandContext()
	defer cancel()
	exitOnError("validator activate", RunReleaseActivation(ctx, ReleaseActivationOptions{
		ConfigPath: optString(opts, "--config", ""), RelayerKeyFile: expandHome(optString(opts, "--relayer_key_file", "")),
		Apply: optBool(opts, "--apply"), Output: os.Stdout,
	}))
}

// --- status --config ---

// statusRelease prints the configuration summary, then the hotkey's native
// registration (UID, permit, stake) and the activation setup state. Chain
// failures are reported, not fatal, so status stays usable offline.
func statusRelease(configPath string) {
	cfg, err := LoadReleaseConfigPreActivation(configPath)
	if err != nil {
		panic(err)
	}
	fmt.Printf("release: %s production=%t validator=%d netuid=%d operators=%d\n", cfg.Release, cfg.Production, cfg.ValidatorID, cfg.Netuid, len(cfg.Operators))
	fmt.Printf("coordinator: %s\npolicy: %s\nstate_dir: %s\n", cfg.Coordinator, cfg.PolicyHash, cfg.StateDir)
	rendered := 0
	for _, operator := range cfg.EvidenceV2.Operators {
		if !operator.Unrendered() {
			rendered++
		}
	}
	fmt.Printf("evidence_v2: %d/%d operator inputs rendered\n", rendered, len(cfg.EvidenceV2.Operators))
	printReleaseActivationSetupStatus(cfg, configPath)
	hotkey, err := loadReleaseHotkey(cfg)
	if err != nil {
		fmt.Printf("hotkey: %v\n", err)
		return
	}
	fmt.Printf("hotkey ss58: %s\nhotkey pubkey: 0x%x\n", hotkey.Address(), hotkey.PublicKey())
	ctx, cancel := nativeCommandContext()
	defer cancel()
	native, err := dialPinnedNative(ctx, cfg)
	if err != nil {
		fmt.Printf("native: %v\n", err)
		return
	}
	defer native.API.Client.Close()
	printReleaseRegistration(ctx, native, cfg, hotkey)
}

func printReleaseActivationSetupStatus(cfg *ReleaseConfig, configPath string) {
	ctx := context.Background()
	preparedPath, completedPath := ReleaseActivationSetupPaths(cfg)
	var prepared ReleaseActivationSetupPreparedV2
	if _, err := readReleaseActivationSetupV2(ctx, preparedPath, cfg.EvidenceV2.Bounds.MaxControlBytes, &prepared); err != nil {
		if ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
			fmt.Printf("activation: not prepared (run `validator activate --config=%s`)\n", configPath)
		} else {
			fmt.Printf("activation: %v\n", err)
		}
		return
	}
	fmt.Printf("activation: prepared for epoch %d (%d members, EVM snapshot %d, native snapshot %d, journal %s)\n", prepared.Epoch, len(prepared.Members), prepared.EVM.Number, prepared.Native.Number, prepared.Journal)
	var completed ReleaseActivationSetupCompletedV2
	if _, err := readReleaseActivationSetupV2(ctx, completedPath, cfg.EvidenceV2.Bounds.MaxControlBytes, &completed); err == nil {
		fmt.Printf("activation: completed at boundary block %d (%s)\n", completed.Boundary.Number, completed.Boundary.Hash)
	} else if !ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		fmt.Printf("activation completion: %v\n", err)
	}
}

func printReleaseRegistration(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, hotkey *crv4.Keypair) {
	bound, runtime, err := snchain.AuthenticateFinalizedRuntimeContext(ctx, native, releaseNativeRuntimeIdentity(cfg))
	if err != nil {
		fmt.Printf("native: %v\n", err)
		return
	}
	fmt.Printf("native: finalized block %d (%s) runtime %s/%d\n", runtime.Number, runtime.Hash.Hex(), runtime.Artifact.Version.SpecName, runtime.Artifact.Version.SpecVersion)
	uid, registered, err := snchain.UIDAtContext(ctx, bound, cfg.Netuid, hotkey.PublicKey(), runtime.Hash)
	if err != nil {
		fmt.Printf("registration: %v\n", err)
		return
	}
	if !registered {
		fmt.Printf("registration: NOT REGISTERED on netuid %d; run `validator register --config=<path> --coldkey_seed_file=<path> --apply`\n", cfg.Netuid)
		return
	}
	owner, err := snchain.HotkeyOwnerAtContext(ctx, bound, hotkey.PublicKey(), runtime.Hash)
	if err != nil {
		fmt.Printf("registration: uid %d; owner: %v\n", uid, err)
		return
	}
	alpha, err := snchain.TotalHotkeyAlphaAtContext(ctx, bound, hotkey.PublicKey(), cfg.Netuid, runtime.Hash)
	if err != nil {
		fmt.Printf("registration: uid %d; stake: %v\n", uid, err)
		return
	}
	genesis, _ := parseHash32("genesis_hash", cfg.GenesisHash)
	observation, err := crv4.ReadValidatorStakeAtContext(ctx, native, crv4.ValidatorIdentityQuery{
		GenesisHash: types.Hash(genesis), BlockHash: runtime.Hash, BlockNumber: runtime.Number, Netuid: cfg.Netuid, UID: uid, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs,
	}, releaseNativeRuntimeIdentity(cfg))
	if err != nil {
		fmt.Printf("registration: uid %d, coldkey %s, hotkey alpha %s; permit/stake: %v\n", uid, snchain.Hex32(owner), snchain.FormatRao(alpha), err)
		return
	}
	fmt.Printf("registration: uid %d, coldkey %s, hotkey alpha %s alpha, weighted stake %d rao (threshold %d rao), permit=%t, eligible=%t\n", uid, snchain.Hex32(owner), snchain.FormatRao(alpha), observation.TotalStakeRao, observation.StakeThresholdRao, observation.Identity.ValidatorPermit, observation.MeetsNonSelfStakeAndPermit())
}
