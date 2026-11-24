//go:build linux || darwin

// validator setup takes a third party from its own Bittensor wallet hotkey to
// a running SN25 validator. The mainnet defaults are bundled in the binary
// (mainnet-profile.json) and every path is derived from one home directory,
// so the hotkey is the only required input. Each step runs the commands an
// operator could run by hand (configure, storage-prepare, auth, activate)
// with the same checks; setup supplies their paths and remembers its files.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/internal/durablepath"
	"github.com/urnetwork/connect/durablevolume"
)

// The default home, under the account's home directory. --home overrides it
// for every setup step and for validator run.
const defaultValidatorHomeName = "sn25-validator"

// A third party renews by rotation, so setup approves a longer window than
// configure's 30-day default. --approval-valid-for overrides it.
const defaultSetupApprovalValidFor = 365 * 24 * time.Hour

// How far back the endpoints must serve state: one coordinator epoch (seven
// days of blocks). Activation, settlement and the audit read that far back,
// and deposit and settlement logs further.
const setupHistoryProbeBlocks = 50400

// One validator's directory. keys and config stay outside runtime, which is
// the storage root that storage preparation declares; activation writes its
// rendered config under runtime/activation.
type validatorHome struct {
	root string
}

func (self validatorHome) Root() string    { return self.root }
func (self validatorHome) KeysDir() string { return filepath.Join(self.root, "keys") }
func (self validatorHome) ApprovalKey() string {
	return filepath.Join(self.root, "keys", "approval.seed")
}
func (self validatorHome) ClientKey() string {
	return filepath.Join(self.root, "keys", "no-1", "client.key")
}
func (self validatorHome) ConfigDir() string { return filepath.Join(self.root, "config") }
func (self validatorHome) InitialConfig() string {
	return filepath.Join(self.root, "config", "initial.yml")
}
func (self validatorHome) StorageDir() string { return filepath.Join(self.root, "config", "storage") }
func (self validatorHome) StorageRequest() string {
	return filepath.Join(self.root, "config", "storage", "request.json")
}
func (self validatorHome) StoragePlan() string {
	return filepath.Join(self.root, "config", "storage", "plan.json")
}
func (self validatorHome) StorageResult() string {
	return filepath.Join(self.root, "config", "storage", "result.json")
}
func (self validatorHome) Progress() string {
	return filepath.Join(self.root, "config", "progress.json")
}
func (self validatorHome) Runtime() string  { return filepath.Join(self.root, "runtime") }
func (self validatorHome) State() string    { return filepath.Join(self.root, "runtime", "state") }
func (self validatorHome) Evidence() string { return filepath.Join(self.root, "runtime", "evidence") }
func (self validatorHome) Activation() string {
	return filepath.Join(self.root, "runtime", "activation")
}
func (self validatorHome) RunConfig() string {
	return filepath.Join(self.root, "runtime", "activation", "validator.yml")
}
func (self validatorHome) SuccessorApproval() string {
	return filepath.Join(self.root, "runtime", "activation", "activation-approval.json")
}
func (self validatorHome) OriginalAuthority() string {
	return filepath.Join(self.root, "runtime", "activation", "initial-authority.json")
}

// An empty path selects $HOME/.urnetwork/sn25-validator. Configuration paths
// must be absolute and free of symlinks, so the existing part of the path is
// resolved once here and the result is what every step records.
func resolveValidatorHome(path string) (validatorHome, error) {
	if strings.TrimSpace(path) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return validatorHome{}, fmt.Errorf("no --home and no home directory: %w", err)
		}
		path = filepath.Join(home, ".urnetwork", defaultValidatorHomeName)
	}
	resolved, err := resolveExistingPathPrefix(expandHome(path))
	if err != nil {
		return validatorHome{}, err
	}
	return validatorHome{root: resolved}, nil
}

// The absolute, clean path with its longest existing prefix's symlinks
// resolved; the missing remainder is appended unchanged.
func resolveExistingPathPrefix(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing, rest := filepath.Clean(absolute), ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, rest), nil
}

// The external calls setup makes, replaced only by tests.
type setupDependencies struct {
	profile   func() (*releaseMainnetProfile, error)
	preflight func(ctx context.Context, rpc, substrate string, profile *releaseMainnetProfile) error
	snapshot  releaseConfigureSnapshotReader
	status    func(configPath string)
}

func defaultSetupDependencies() setupDependencies {
	return setupDependencies{profile: readReleaseMainnetProfile, preflight: preflightSetupEndpoints, snapshot: readReleaseConfigureNativeSnapshot, status: statusRelease}
}

const setupUsage = `Usage:
  validator setup --wallet.name=<wallet> --wallet.hotkey=<hotkey> [options]
  validator setup --hotkey-file=<path> [options]
  validator setup storage --validator-uid=<uid> --fresh [--mount-path=<dir> --filesystem-uuid=<uuid> --filesystem-type=<type>] [--home=<dir>]
  validator setup storage --apply [--home=<dir>]
  validator setup auth [-f] [--home=<dir>]
  validator setup activate --relayer-key-file=<path> (--dry-run | --apply) [--home=<dir>]

Sets up an SN25 validator from your own Bittensor hotkey with the mainnet
defaults bundled in this binary. Every file lives under one home directory,
by default $HOME/.urnetwork/sn25-validator; validator run uses the same home.

  setup             create or reuse the approval and measurement keys, check
                    the chain endpoints, write the signed initial
                    configuration and report the hotkey's registration.
  setup storage     prepare the runtime directory's durable storage: request
                    and plan with --validator-uid and --fresh (the mount
                    facts are found on Linux), then --apply the shown plan.
  setup auth        sign in to the network operator with the hotkey. This
                    accepts the operator's Terms of Service.
  setup activate    publish the activation with a funded EVM key and render
                    the run configuration; repeat --apply until it completes.
  validator run     start the validator from the home.

Options for setup:
  --home=<dir>                 Validator home [default: $HOME/.urnetwork/sn25-validator].
  --wallet.name=<wallet>       btcli wallet name.
  --wallet.hotkey=<hotkey>     btcli hotkey name in that wallet.
  --wallet.path=<dir>          btcli wallets directory [default: $HOME/.bittensor/wallets].
  --hotkey-file=<path>         A hotkey file instead of a wallet: an unencrypted
                               Bittensor keyfile, or a raw or hex sr25519 seed.
  --validator-id=<n>           Local validator ID [default: derived from the hotkey].
  --rpc=<url>                  EVM JSON-RPC endpoint [default: the bundled one]. It must
                               serve eth_getLogs and a week of history, like an
                               archive subtensor node.
  --substrate=<url>            Subtensor WebSocket endpoint [default: the bundled one].
                               It must serve historical state.
  --approval-valid-for=<d>     Approval window of the signed configuration [default: 8760h].

Flags override these defaults, and the defaults are the bundled mainnet profile.
`

// Dispatches the setup steps; an empty or flag-first argument list is the
// initial step. Exit status 2 is a usage error, 1 a failed step.
func runSetupCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runSetupCommandWith(ctx, args, stdout, stderr, defaultSetupDependencies())
}

func runSetupCommandWith(ctx context.Context, args []string, stdout, stderr io.Writer, dependencies setupDependencies) int {
	step := ""
	if len(args) != 0 && !strings.HasPrefix(args[0], "-") {
		step, args = args[0], args[1:]
	}
	if len(args) != 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, setupUsage)
		return 0
	}
	var err error
	usage := false
	switch step {
	case "":
		usage, err = setupInitial(ctx, args, stdout, stderr, dependencies)
	case "storage":
		usage, err = setupStorage(ctx, args, stdout, stderr)
	case "auth":
		usage, err = setupAuth(ctx, args, stdout, stderr)
	case "activate":
		usage, err = setupActivate(ctx, args, stdout, stderr)
	default:
		usage, err = true, fmt.Errorf("unknown setup step %q; the steps are storage, auth and activate", step)
	}
	if err == nil {
		return 0
	}
	label := "validator setup"
	if step != "" {
		label += " " + step
	}
	fmt.Fprintf(stderr, "%s: %v\n", label, err)
	if usage {
		return 2
	}
	return 1
}

// The hotkey file a wallet name and hotkey select, as btcli lays them out.
func bittensorWalletHotkeyPath(walletPath, name, hotkey string) (string, error) {
	for label, value := range map[string]string{"--wallet.name": name, "--wallet.hotkey": hotkey} {
		if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
			return "", fmt.Errorf("%s %q is not a wallet entry name", label, value)
		}
	}
	if walletPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		walletPath = filepath.Join(home, ".bittensor", "wallets")
	}
	return filepath.Join(expandHome(walletPath), name, "hotkeys", hotkey), nil
}

// A positive local ID that stays the same for the same hotkey, so neither a
// choice nor a record of one is needed. It is not the native UID.
func defaultSetupValidatorID(hotkey [32]byte) uint64 {
	digest := sha256.Sum256(append([]byte("urnetwork-sn25-validator-id-v1"), hotkey[:]...))
	if id := uint64(binary.BigEndian.Uint32(digest[:4])); id != 0 {
		return id
	}
	return 1
}

type setupInitialOptions struct {
	home             string
	walletName       string
	walletHotkey     string
	walletPath       string
	hotkeyFile       string
	validatorID      uint64
	rpc              string
	substrate        string
	approvalValidFor time.Duration
}

func setupInitial(ctx context.Context, args []string, stdout, stderr io.Writer, dependencies setupDependencies) (bool, error) {
	profile, err := dependencies.profile()
	if err != nil {
		return false, fmt.Errorf("bundled mainnet profile: %w", err)
	}
	options := setupInitialOptions{}
	flags := flag.NewFlagSet("validator setup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.home, "home", "", "")
	flags.StringVar(&options.walletName, "wallet.name", "", "")
	flags.StringVar(&options.walletHotkey, "wallet.hotkey", "", "")
	flags.StringVar(&options.walletPath, "wallet.path", "", "")
	flags.StringVar(&options.hotkeyFile, "hotkey-file", "", "")
	flags.Uint64Var(&options.validatorID, "validator-id", 0, "")
	flags.StringVar(&options.rpc, "rpc", profile.Config.RPC[0], "")
	flags.StringVar(&options.substrate, "substrate", profile.Config.Substrate[0], "")
	flags.DurationVar(&options.approvalValidFor, "approval-valid-for", defaultSetupApprovalValidFor, "")
	if err := flags.Parse(args); err != nil {
		return true, err
	}
	if flags.NArg() != 0 {
		return true, fmt.Errorf("unexpected arguments %q", flags.Args())
	}
	explicitValidatorID := false
	var configChoices []string
	flags.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "validator-id":
			explicitValidatorID = true
			configChoices = append(configChoices, "--"+value.Name)
		case "rpc", "substrate", "approval-valid-for":
			configChoices = append(configChoices, "--"+value.Name)
		}
	})
	if explicitValidatorID && options.validatorID == 0 {
		return true, errors.New("--validator-id must be positive")
	}
	wallet := options.walletName != "" || options.walletHotkey != "" || options.walletPath != ""
	switch {
	case wallet && options.hotkeyFile != "":
		return true, errors.New("choose the hotkey once: --wallet.name and --wallet.hotkey, or --hotkey-file")
	case !wallet && options.hotkeyFile == "":
		return true, errors.New("a hotkey is required: --wallet.name=<wallet> --wallet.hotkey=<hotkey> for a btcli wallet (under --wallet.path, default ~/.bittensor/wallets), or --hotkey-file=<path>")
	case wallet && (options.walletName == "" || options.walletHotkey == ""):
		return true, errors.New("a wallet hotkey needs both --wallet.name and --wallet.hotkey")
	}
	hotkeyPath := expandHome(options.hotkeyFile)
	if wallet {
		if hotkeyPath, err = bittensorWalletHotkeyPath(options.walletPath, options.walletName, options.walletHotkey); err != nil {
			return true, err
		}
	}
	if _, err := os.Stat(hotkeyPath); errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("hotkey file %s does not exist; create or import the hotkey with btcli first, or pass --hotkey-file", hotkeyPath)
	}
	if hotkeyPath, err = resolveExistingPathPrefix(hotkeyPath); err != nil {
		return false, err
	}
	seed, err := crv4.LoadSeedFile(hotkeyPath)
	if err != nil {
		return false, fmt.Errorf("hotkey %s: %w (a hotkey file must be owned by this account with mode 0600)", hotkeyPath, err)
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return false, err
	}
	home, err := resolveValidatorHome(options.home)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(stdout, "home: %s\nhotkey: %s (%s)\n", home.Root(), hotkey.Address(), hotkeyPath)
	for _, directory := range []string{home.Root(), home.KeysDir(), filepath.Dir(home.ClientKey()), home.ConfigDir(), home.StorageDir()} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return false, err
		}
	}
	// storage preparation needs the runtime root to exist and stay empty
	// until it declares it.
	if err := os.Mkdir(home.Runtime(), 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return false, err
	}
	if _, _, err := crv4.LoadOrCreateRawSeedFile(home.ApprovalKey()); err != nil {
		return false, fmt.Errorf("approval key %s: %w", home.ApprovalKey(), err)
	}
	approval, err := loadApprovalKeyFile(home.ApprovalKey())
	if err != nil {
		return false, fmt.Errorf("approval key %s: %w", home.ApprovalKey(), err)
	}
	if _, _, err := crv4.LoadOrCreateRawSeedFile(home.ClientKey()); err != nil {
		return false, fmt.Errorf("measurement key %s: %w", home.ClientKey(), err)
	}
	fmt.Fprintf(stdout, "approval signer: 0x%x (%s)\nmeasurement key: %s\n", approval.Public(), home.ApprovalKey(), home.ClientKey())
	if _, err := os.Lstat(home.InitialConfig()); err == nil {
		if err := checkSetupConfigHotkey(home, hotkeyPath, hotkey.PublicKey()); err != nil {
			return false, err
		}
		fmt.Fprintf(stdout, "config: %s (existing)\n", home.InitialConfig())
		if len(configChoices) != 0 {
			fmt.Fprintf(stderr, "validator setup: the existing signed configuration keeps its own values, so %s changed nothing; set up a new --home to change them\n", strings.Join(configChoices, ", "))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	} else {
		if !explicitValidatorID {
			options.validatorID = defaultSetupValidatorID(hotkey.PublicKey())
		}
		if err := dependencies.preflight(ctx, options.rpc, options.substrate, profile); err != nil {
			return false, err
		}
		settings := defaultReleaseConfigureSettings()
		configure := releaseConfigureOptions{StateDir: home.State(), EvidenceDir: home.Evidence(), HotkeySeedFile: hotkeyPath, ClientKeySeedFile: home.ClientKey(),
			ApprovalKeyFile: home.ApprovalKey(), OutputPath: home.InitialConfig(), ValidatorId: options.validatorID, Rpc: options.rpc, Substrate: options.substrate,
			ApprovalValidFor: options.approvalValidFor}
		if err := configure.validate(settings); err != nil {
			return true, err
		}
		if err := configureRelease(ctx, configure, settings, profile, dependencies.snapshot, stdout); err != nil {
			return false, err
		}
	}
	if dependencies.status != nil {
		dependencies.status(home.InitialConfig())
	}
	flagHome := ""
	if options.home != "" {
		flagHome = " --home=" + home.Root()
	}
	fmt.Fprintf(stdout, `
Next steps:
  1. The hotkey needs a validator permit and stake on subnet 25. Look for
     "permit=true, eligible=true" above; the registration line shows its UID.
  2. Prepare durable storage for %[1]s:
       validator setup storage --validator-uid=<UID> --fresh%[2]s
     review the plan, then: validator setup storage --apply%[2]s
  3. Sign in to the network operator with the hotkey:
       validator setup auth%[2]s
  4. Publish the activation with a funded EVM key (gas only), then repeat with
     --apply until it reports completion; it waits for the next coordinator epoch:
       validator setup activate --relayer-key-file=<path> --dry-run%[2]s
  5. Start the validator, for example from the systemd unit in the README:
       validator run%[2]s
`, home.Runtime(), flagHome)
	return false, nil
}

// An existing initial config is reused only for the hotkey it names.
func checkSetupConfigHotkey(home validatorHome, hotkeyPath string, hotkey [32]byte) error {
	cfg, err := loadReleaseBootstrapCommandConfig(home.InitialConfig())
	if err != nil {
		return fmt.Errorf("existing config %s: %w", home.InitialConfig(), err)
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return fmt.Errorf("existing config %s: %w", home.InitialConfig(), err)
	}
	if cfg.HotkeySeedFile != hotkeyPath || approved.Approval.ValidatorHotkey != hotkey {
		return fmt.Errorf("home %s is set up for the hotkey in %s; use another --home for this hotkey", home.Root(), cfg.HotkeySeedFile)
	}
	return nil
}

// The bundled public endpoints answer setup's own reads, but the validator
// also reads EVM logs and a week of history. A pruned node or one that
// refuses eth_getLogs would pass setup and fail days later, after activation,
// so setup refuses it now, before it signs a config naming it.
func preflightSetupEndpoints(ctx context.Context, rpc, substrate string, profile *releaseMainnetProfile) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	evm, err := gethrpc.DialContext(ctx, rpc)
	if err != nil {
		return fmt.Errorf("EVM RPC %s: %w", rpc, err)
	}
	defer evm.Close()
	if err := preflightSetupEVM(ctx, evm, rpc, profile.Config.Coordinator); err != nil {
		return err
	}
	native, err := crv4.DialChainContext(ctx, substrate)
	if err != nil {
		return fmt.Errorf("subtensor endpoint %s: %w", substrate, err)
	}
	defer native.API.Client.Close()
	var header struct {
		Number string `json:"number"`
	}
	if err := native.API.Client.CallContext(ctx, &header, "chain_getHeader"); err != nil {
		return fmt.Errorf("subtensor endpoint %s: %w", substrate, err)
	}
	head, err := hexutil.DecodeUint64(header.Number)
	if err != nil || head <= setupHistoryProbeBlocks {
		return fmt.Errorf("subtensor endpoint %s answered an unusable head %q", substrate, header.Number)
	}
	var old string
	if err := native.API.Client.CallContext(ctx, &old, "chain_getBlockHash", head-setupHistoryProbeBlocks); err != nil || old == "" {
		return errors.Join(fmt.Errorf("subtensor endpoint %s has no block %d", substrate, head-setupHistoryProbeBlocks), err)
	}
	var code string
	if err := native.API.Client.CallContext(ctx, &code, "state_getStorageHash", "0x3a636f6465", old); err != nil || code == "" {
		return errors.Join(fmt.Errorf("subtensor endpoint %s serves no state from a week ago (block %d), which the validator reads; pass --substrate with an archive subtensor node", substrate, head-setupHistoryProbeBlocks), err)
	}
	return nil
}

func preflightSetupEVM(ctx context.Context, evm *gethrpc.Client, rpc, coordinator string) error {
	var encoded string
	if err := evm.CallContext(ctx, &encoded, "eth_blockNumber"); err != nil {
		return fmt.Errorf("EVM RPC %s: %w", rpc, err)
	}
	head, err := hexutil.DecodeUint64(encoded)
	if err != nil || head <= setupHistoryProbeBlocks {
		return fmt.Errorf("EVM RPC %s answered an unusable head %q", rpc, encoded)
	}
	// One chunk of the size the validator's own log reads use: public
	// endpoints refuse larger ranges, and some refuse logs entirely.
	var logs []json.RawMessage
	filter := map[string]any{"address": coordinator, "fromBlock": hexutil.EncodeUint64(head - getLogsChunkBlocks + 1), "toBlock": hexutil.EncodeUint64(head)}
	if err := evm.CallContext(ctx, &logs, "eth_getLogs", filter); err != nil {
		return fmt.Errorf("EVM RPC %s refuses eth_getLogs over %d blocks (%v), which the validator's settlement audit and deposit reads use; pass --rpc with an endpoint that serves logs and history, such as your own archive subtensor node", rpc, getLogsChunkBlocks, err)
	}
	var code hexutil.Bytes
	if err := evm.CallContext(ctx, &code, "eth_getCode", coordinator, hexutil.EncodeBig(new(big.Int).SetUint64(head-setupHistoryProbeBlocks))); err != nil || len(code) == 0 {
		return errors.Join(fmt.Errorf("EVM RPC %s serves no state from a week ago (block %d), which the validator reads; pass --rpc with an archive endpoint", rpc, head-setupHistoryProbeBlocks), err)
	}
	return nil
}

// The storage step: request and plan, then apply the plan whose digest the
// first half printed. Everything is written under config/storage.
func setupStorage(ctx context.Context, args []string, stdout, stderr io.Writer) (bool, error) {
	flags := flag.NewFlagSet("validator setup storage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	homePath := flags.String("home", "", "")
	uid := flags.String("validator-uid", "", "")
	fresh := flags.Bool("fresh", false, "")
	apply := flags.Bool("apply", false, "")
	mount := flags.String("mount-path", "", "")
	uuid := flags.String("filesystem-uuid", "", "")
	filesystem := flags.String("filesystem-type", "", "")
	minimumBytes := flags.String("min-available-bytes", "", "")
	minimumInodes := flags.String("min-available-inodes", "", "")
	if err := flags.Parse(args); err != nil {
		return true, err
	}
	if flags.NArg() != 0 {
		return true, fmt.Errorf("unexpected arguments %q", flags.Args())
	}
	home, err := resolveValidatorHome(*homePath)
	if err != nil {
		return false, err
	}
	adapter := validatorRuntimePreparationAdapter()
	if *apply {
		if *uid != "" || *fresh || *mount != "" || *uuid != "" || *filesystem != "" {
			return true, errors.New("--apply takes only the plan already written; run the request step without it")
		}
		digest, err := setupFileDigest(home.StoragePlan())
		if err != nil {
			return false, fmt.Errorf("no storage plan to apply (run `validator setup storage --validator-uid=<UID> --fresh` first): %w", err)
		}
		result, err := durablepath.ApplyPreparation(ctx, durablevolume.Reference{Path: home.StoragePlan(), Sha256: digest}, adapter)
		if err != nil {
			return false, err
		}
		if err := writeSetupJSON(home.StorageResult(), result); err != nil {
			return false, err
		}
		fmt.Fprintf(stdout, "storage prepared: %s\ndeclaration: %s %s\nnext: validator setup auth\n", home.StorageResult(), result.Declaration.Path, result.Declaration.Sha256)
		return false, nil
	}
	if *uid == "" || !*fresh {
		return true, errors.New("the request step needs --validator-uid=<UID> (status shows it) and --fresh, which asserts that the runtime directory has never been used")
	}
	if *mount == "" && *uuid == "" && *filesystem == "" {
		*mount, *uuid, *filesystem, err = detectRuntimeFilesystem(home.Runtime())
		if err != nil {
			return true, err
		}
		fmt.Fprintf(stdout, "runtime filesystem: %s (UUID %s, %s)\n", *mount, *uuid, *filesystem)
	} else if *mount == "" || *uuid == "" || *filesystem == "" {
		return true, errors.New("give all of --mount-path, --filesystem-uuid and --filesystem-type, or none of them on Linux")
	}
	switch {
	case setupFileExists(home.StorageResult()):
		return false, fmt.Errorf("storage is already prepared (%s); next: validator setup auth", home.StorageResult())
	case setupFileExists(home.StoragePlan()):
		return false, fmt.Errorf("a storage plan is already written (%s); review it and run `validator setup storage --apply`, or remove %s to start over", home.StoragePlan(), home.StorageDir())
	case setupFileExists(home.StorageRequest()):
		return false, fmt.Errorf("an earlier storage request did not finish (%s); fix its cause, remove %s and the files it left in %s, then run this step again", home.StorageRequest(), home.StorageDir(), home.Runtime())
	}
	request := []string{"--config=" + home.InitialConfig(), "--root-dir=" + home.Runtime(), "--validator-uid=" + *uid, "--mount-path=" + *mount,
		"--filesystem-uuid=" + *uuid, "--filesystem-type=" + *filesystem, "--fresh", "--out=" + home.StorageRequest()}
	if *minimumBytes != "" {
		request = append(request, "--min-available-bytes="+*minimumBytes)
	}
	if *minimumInodes != "" {
		request = append(request, "--min-available-inodes="+*minimumInodes)
	}
	reference, err := createValidatorStorageRequest(ctx, request, stderr)
	if err != nil {
		return false, err
	}
	plan, err := durablepath.PlanPreparation(ctx, reference, adapter)
	if err != nil {
		return false, err
	}
	if err := writeSetupJSON(home.StoragePlan(), plan); err != nil {
		return false, err
	}
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return false, err
	}
	fmt.Fprintf(stdout, "%s\nplan: %s\nReview the plan, then apply it: validator setup storage --apply\n", encoded, home.StoragePlan())
	return false, nil
}

func setupFileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// "sha256:<hex>" of a file's exact bytes, the pin storage preparation takes.
func setupFileDigest(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Exactly the bytes the storage-prepare command prints, in a new private file.
func writeSetupJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(value); err != nil {
		return errors.Join(err, file.Close())
	}
	return errors.Join(file.Sync(), file.Close())
}

// The declaration storage preparation published, as validator run and
// activate need it.
func readSetupStorageDeclaration(home validatorHome) (durablevolume.Reference, error) {
	raw, err := os.ReadFile(home.StorageResult())
	if err != nil {
		return durablevolume.Reference{}, fmt.Errorf("no prepared storage in %s (run `validator setup storage`): %w", home.Root(), err)
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(raw, &result); err != nil || result.Declaration.Path == "" || result.Declaration.Sha256 == "" {
		return durablevolume.Reference{}, errors.Join(fmt.Errorf("storage result %s has no declaration", home.StorageResult()), err)
	}
	return result.Declaration, nil
}

// The auth step: validator auth --config=<initial> --no_id=1.
func setupAuth(ctx context.Context, args []string, stdout, stderr io.Writer) (bool, error) {
	flags := flag.NewFlagSet("validator setup auth", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	homePath := flags.String("home", "", "")
	force := flags.Bool("f", false, "")
	if err := flags.Parse(args); err != nil {
		return true, err
	}
	if flags.NArg() != 0 {
		return true, fmt.Errorf("unexpected arguments %q", flags.Args())
	}
	home, err := resolveValidatorHome(*homePath)
	if err != nil {
		return false, err
	}
	// The credential is written under the runtime root, which storage
	// preparation requires to be empty, so sign-in follows preparation.
	if _, err := readSetupStorageDeclaration(home); err != nil {
		return false, err
	}
	argv := []string{"auth", "--config=" + home.InitialConfig(), "--no_id=1"}
	if *force {
		argv = append(argv, "-f")
	}
	opts, err := docopt.ParseArgs(mainUsage(), argv, RequireVersion())
	if err != nil {
		return true, err
	}
	target, proceed, err := confirmAuthTarget(ctx, opts, os.Stdin, stdout)
	if err != nil || !proceed {
		return false, err
	}
	fmt.Fprintln(stdout, "Signing in with the hotkey accepts the network operator's Terms of Service.")
	return false, writeAuthJwt(ctx, opts, target, stdout)
}

// The activate step: validator activate with every path from the home.
func setupActivate(ctx context.Context, args []string, stdout, stderr io.Writer) (bool, error) {
	flags := flag.NewFlagSet("validator setup activate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	homePath := flags.String("home", "", "")
	relayer := flags.String("relayer-key-file", "", "")
	apply := flags.Bool("apply", false, "")
	dryRun := flags.Bool("dry-run", false, "")
	if err := flags.Parse(args); err != nil {
		return true, err
	}
	if flags.NArg() != 0 {
		return true, fmt.Errorf("unexpected arguments %q", flags.Args())
	}
	if *relayer == "" || *apply == *dryRun {
		return true, errors.New("activate needs --relayer-key-file=<funded EVM key> and one of --dry-run or --apply")
	}
	home, err := resolveValidatorHome(*homePath)
	if err != nil {
		return false, err
	}
	declaration, err := readSetupStorageDeclaration(home)
	if err != nil {
		return false, err
	}
	mode := "--dry-run"
	if *apply {
		mode = "--apply"
	}
	argv := []string{"activate", "--config=" + home.InitialConfig(), "--relayer_key_file=" + expandHome(*relayer), mode,
		"--rendered-config=" + home.RunConfig(), "--successor-approval=" + home.SuccessorApproval(), "--original-authority=" + home.OriginalAuthority(),
		"--approval-key-file=" + home.ApprovalKey(), "--durable-volumes=" + declaration.Path, "--durable-volumes-sha256=" + declaration.Sha256}
	opts, err := docopt.ParseArgs(mainUsage(), argv, RequireVersion())
	if err != nil {
		return true, err
	}
	ctx = durablevolume.WithReference(ctx, declaration)
	options := ReleaseActivationOptions{ConfigPath: home.InitialConfig(), RelayerKeyFile: expandHome(*relayer), Apply: *apply, Output: stdout}
	options.Successor = releaseProductionSuccessorOptionsFromOpts(opts)
	if err := RunReleaseActivation(ctx, options); err != nil {
		return false, err
	}
	if _, err := os.Lstat(home.RunConfig()); err == nil {
		fmt.Fprintf(stdout, "run configuration: %s\nnext: validator run (start it promptly; the first decision must be made in the native epoch activation named)\n", home.RunConfig())
	}
	return false, nil
}

// The run configuration the home holds, with the home's defaults written into
// opts for every flag the operator left out: the storage declaration, the
// progress file, verified startup progress and a 60m steering budget. A home
// without a rendered configuration says which setup step is missing.
func validatorHomeRunConfig(opts docopt.Opts, homePath string) (string, error) {
	home, err := resolveValidatorHome(homePath)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(home.RunConfig()); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if _, initial := os.Lstat(home.InitialConfig()); initial == nil {
			return "", fmt.Errorf("the validator in %s is not activated yet: %s does not exist. Finish `validator setup storage`, `validator setup auth` and `validator setup activate --apply` first (see validator/README.md)", home.Root(), home.RunConfig())
		}
		return "", fmt.Errorf("no validator is set up in %s: %s does not exist. Set one up with `validator setup --wallet.name=<wallet> --wallet.hotkey=<hotkey>` and its later steps (see validator/README.md), or pass --config=<path> for a configuration made by hand", home.Root(), home.RunConfig())
	}
	if optString(opts, "--durable-volumes", "") == "" && optString(opts, "--durable-volumes-sha256", "") == "" {
		declaration, err := readSetupStorageDeclaration(home)
		if err != nil {
			return "", err
		}
		opts["--durable-volumes"], opts["--durable-volumes-sha256"] = declaration.Path, declaration.Sha256
	}
	if optString(opts, "--progress-file", "") == "" {
		opts["--progress-file"] = home.Progress()
	}
	if optString(opts, "--steering-operation-timeout", "") == "" {
		opts["--steering-operation-timeout"] = "60m"
	}
	opts["--startup-verified-progress"] = true
	return home.RunConfig(), nil
}
