//go:build linux || darwin

// Configure combines shipped public mainnet terms with this validator's own
// identity and approval key. It writes only a signed activation-pending config
// and its approval; normal bootstrap, activation and producer checks still run.
package validator

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/urfoundation/sn/crv4"
	"gopkg.in/yaml.v3"
)

// Operational setup defaults are finite. Zero durations, concurrency or poll
// interval are invalid; callers never receive an implicit unlimited window.
type releaseConfigureSettings struct {
	ApprovalValidFor time.Duration
	NativeBlockTime  time.Duration
	ReadTimeout      time.Duration
	Concurrency      int
	PollSeconds      int
}

// Mainnet's twelve-second nominal block time converts the requested approval
// duration to an explicit block ceiling; the approval stores blocks and epochs,
// not a promise that wall-clock validity survives future cadence changes.
func defaultReleaseConfigureSettings() releaseConfigureSettings {
	return releaseConfigureSettings{
		ApprovalValidFor: 30 * 24 * time.Hour,
		NativeBlockTime:  12 * time.Second,
		ReadTimeout:      productionSteeringReadTimeout,
		Concurrency:      4,
		PollSeconds:      2,
	}
}

// Identity and path choices are explicit CLI inputs; evidence and the client
// key may be relocated before signing, never by editing the resulting config.
type releaseConfigureOptions struct {
	StateDir          string
	EvidenceDir       string
	HotkeySeedFile    string
	ClientKeySeedFile string
	ApprovalKeyFile   string
	OutputPath        string
	ValidatorId       uint64
	Rpc               string
	Substrate         string
	ApprovalValidFor  time.Duration
}

// The existing activation renderer reselects and re-approves a later drained
// native boundary if onboarding outlives this initial one.
type releaseConfigureNativeSnapshot struct {
	Boundary productionSuccessorBoundary
	Tempo    uint16
}

// Separates the public chain read from local generation for deterministic
// composed tests. The production callback performs no signing or broadcast.
type releaseConfigureSnapshotReader func(context.Context, *releaseMainnetProfile, string) (releaseConfigureNativeSnapshot, error)

// A separate setup parser keeps these identity inputs out of run's serialized
// configuration and out of unrelated measurement-mode command grammars.
func runConfigureCommand(ctx context.Context, args []string, output, errorOutput io.Writer) int {
	profile, err := readReleaseMainnetProfile()
	if err != nil {
		fmt.Fprintln(errorOutput, "validator configure:", err)
		return 1
	}
	return runConfigureCommandWithProfile(ctx, args, output, errorOutput, profile, readReleaseConfigureNativeSnapshot)
}

// Parse before opening keys or contacting a node, including malformed and
// explicitly empty option values. Help needs neither credentials nor RPC.
func runConfigureCommandWithProfile(ctx context.Context, args []string, output, errorOutput io.Writer, profile *releaseMainnetProfile, readSnapshot releaseConfigureSnapshotReader) int {
	settings := defaultReleaseConfigureSettings()
	options := releaseConfigureOptions{}
	flags := flag.NewFlagSet("validator configure", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	flags.StringVar(&options.StateDir, "state-dir", "", "absolute runtime state directory")
	flags.StringVar(&options.EvidenceDir, "evidence-dir", "", "absolute evidence root outside state (default: <state-dir>.evidence)")
	flags.StringVar(&options.HotkeySeedFile, "hotkey-seed-file", "", "existing private sr25519 hotkey seed file")
	flags.StringVar(&options.ClientKeySeedFile, "client-key-seed-file", "", "client seed (default: <state-dir>/no-1/client.key); put outside state for fresh storage preparation")
	flags.StringVar(&options.ApprovalKeyFile, "approval-key-file", "", "existing private Ed25519 approval seed: raw32 or bare64hex")
	flags.StringVar(&options.OutputPath, "out", "", "new absolute config path; also writes <out>.treasury-approval.json")
	flags.Uint64Var(&options.ValidatorId, "validator-id", 0, "positive validator ID, distinct from subnet UID")
	flags.StringVar(&options.Rpc, "rpc", profile.Config.RPC[0], "public EVM HTTP(S) endpoint")
	flags.StringVar(&options.Substrate, "substrate", profile.Config.Substrate[0], "native WS(S) endpoint")
	flags.DurationVar(&options.ApprovalValidFor, "approval-valid-for", settings.ApprovalValidFor, "finite initial approval duration, converted to native block and epoch ceilings")
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, "Usage: validator configure --state-dir=<absolute> --hotkey-seed-file=<absolute> --validator-id=<positive> --approval-key-file=<absolute> --out=<new-absolute.yml> [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return 0
	} else if err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errorOutput, "validator configure: unexpected positional arguments")
		return 2
	}
	var emptyOption string
	flags.Visit(func(value *flag.Flag) {
		if value.Value.String() == "" {
			emptyOption = value.Name
		}
	})
	if emptyOption != "" {
		fmt.Fprintf(errorOutput, "validator configure: --%s must not be empty\n", emptyOption)
		return 2
	}
	if err := options.validate(settings); err != nil {
		fmt.Fprintln(errorOutput, "validator configure:", err)
		return 2
	}
	if err := configureRelease(ctx, options, settings, profile, readSnapshot, output); err != nil {
		fmt.Fprintln(errorOutput, "validator configure:", err)
		return 1
	}
	return 0
}

// All generated paths use canonical absolute spellings before approval. Setup
// must not overwrite an earlier signed config or place it under mutable state.
func (self *releaseConfigureOptions) validate(settings releaseConfigureSettings) error {
	if self.ValidatorId == 0 || self.ApprovalValidFor < settings.NativeBlockTime || settings.NativeBlockTime <= 0 || settings.ReadTimeout <= 0 {
		return errors.New("--validator-id must be positive and --approval-valid-for must be at least one native block")
	}
	for label, path := range map[string]string{"--state-dir": self.StateDir, "--hotkey-seed-file": self.HotkeySeedFile, "--approval-key-file": self.ApprovalKeyFile, "--out": self.OutputPath} {
		if err := ValidateReleaseEvidenceV2Path(path); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	if self.EvidenceDir == "" {
		self.EvidenceDir = self.StateDir + ".evidence"
	}
	if self.ClientKeySeedFile == "" {
		self.ClientKeySeedFile = filepath.Join(self.StateDir, "no-1", "client.key")
	}
	for label, path := range map[string]string{"--evidence-dir": self.EvidenceDir, "--client-key-seed-file": self.ClientKeySeedFile} {
		if err := ValidateReleaseEvidenceV2Path(path); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	if err := validateEndpoint("--rpc", self.Rpc, "http", "https"); err != nil {
		return err
	}
	if err := validateEndpoint("--substrate", self.Substrate, "ws", "wss"); err != nil {
		return err
	}
	paths := []string{self.OutputPath, self.OutputPath + ".treasury-approval.json", self.StateDir, self.EvidenceDir, self.HotkeySeedFile, self.ApprovalKeyFile}
	for index, path := range paths {
		for _, prior := range paths[:index] {
			if productionOriginalRequestPathsOverlap(path, prior) {
				return errors.New("config, approval, state, evidence and signing-key paths must be distinct and unnested")
			}
		}
	}
	for _, path := range paths[:2] {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(fmt.Errorf("output must not already exist: %s", path), err)
		}
	}
	for _, path := range append(slices.Clone(paths[:2]), self.EvidenceDir, self.HotkeySeedFile, self.ApprovalKeyFile) {
		if productionOriginalRequestPathsOverlap(self.ClientKeySeedFile, path) {
			return errors.New("client key must not overlap config, approval, evidence or signing keys")
		}
	}
	return nil
}

// The only private operations are local signatures over the exact generated
// approval. No state directory, client credential or native transaction is made.
func configureRelease(ctx context.Context, options releaseConfigureOptions, settings releaseConfigureSettings, profile *releaseMainnetProfile, readSnapshot releaseConfigureSnapshotReader, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	private, err := loadApprovalKeyFile(options.ApprovalKeyFile)
	if err != nil {
		return fmt.Errorf("approval key: %w", err)
	}
	seed, err := crv4.LoadSeedFile(options.HotkeySeedFile)
	if err != nil {
		return fmt.Errorf("hotkey: %w", err)
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return err
	}
	readCtx, cancel := context.WithTimeout(ctx, settings.ReadTimeout)
	defer cancel()
	snapshot, err := readSnapshot(readCtx, profile, options.Substrate)
	if err != nil {
		return fmt.Errorf("read initial native boundary: %w", err)
	}
	cfg, approval, err := buildConfiguredRelease(options, settings, profile, snapshot, hotkey.PublicKey(), private)
	if err != nil {
		return err
	}
	approvalReference := ReleaseEvidenceV2File{Path: cfg.TreasuryApproval.Approval.Path, Bytes: uint64(len(approval)), SHA256: attemptHex32(sha256.Sum256(approval))}
	cfg.TreasuryApproval.Approval = approvalReference
	if err := cfg.normalize(filepath.Dir(options.OutputPath)); err != nil {
		return err
	}
	if err := loadOwnerRecycleProductionConfigBytes(cfg, approval); err != nil {
		return err
	}
	if err := loadReleaseProductionRuntimeHistory(cfg); err != nil {
		return err
	}
	if err := loadReleaseProductionAuthorityHistory(cfg); err != nil {
		return err
	}
	if err := cfg.validateProductionPreActivation(); err != nil {
		return fmt.Errorf("generated production config: %w", err)
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if _, err := WriteReleaseEvidenceV2File(ctx, approvalReference.Path, approval, MaximumTreasuryApprovalBytes); err != nil {
		return err
	}
	if _, err := WriteReleaseEvidenceV2File(ctx, options.OutputPath, encoded, maximumReleaseConfigBytes); err != nil {
		return err
	}
	fmt.Fprintf(output, "config: %s\napproval: %s\napproval signer: %s\nvalidator hotkey: %s\nstate: %s\nevidence: %s\nactivation pending: prepare declared durable storage; register/stake as needed; activate to a separately approved rendered config; authenticate with that config; then run\n",
		options.OutputPath, approvalReference.Path, cfg.TreasuryApproval.Signer, attemptHex32(hotkey.PublicKey()), cfg.StateDir, options.EvidenceDir)
	return nil
}

// Build and sign through the existing treasury draft/envelope domains. An
// initial authority has only this validator in its census and no prior history.
func buildConfiguredRelease(options releaseConfigureOptions, settings releaseConfigureSettings, profile *releaseMainnetProfile, snapshot releaseConfigureNativeSnapshot, hotkey [32]byte, private ed25519.PrivateKey) (*ReleaseConfig, []byte, error) {
	boundary := snapshot.Boundary
	blocks := uint64(options.ApprovalValidFor / settings.NativeBlockTime)
	if blocks == 0 || boundary.Block == 0 || boundary.Epoch == 0 || boundary.Hash == ([32]byte{}) || snapshot.Tempo == 0 ||
		boundary.Block > math.MaxUint32 || blocks > math.MaxUint32-boundary.Block || len(private) != ed25519.PrivateKeySize {
		return nil, nil, errors.New("initial native boundary or finite approval window is invalid")
	}
	// At most one native epoch can run per block; this ceiling remains finite
	// without promising that today's tempo cannot change during the window.
	if boundary.Epoch > math.MaxUint64-blocks {
		return nil, nil, errors.New("initial approval epoch window overflows")
	}
	cfg := profile.Config
	cfg.ValidatorID, cfg.StateDir, cfg.HotkeySeedFile = options.ValidatorId, options.StateDir, options.HotkeySeedFile
	cfg.RPC, cfg.Substrate = []string{options.Rpc}, []string{options.Substrate}
	cfg.PollSeconds = settings.PollSeconds
	cfg.ControlledNOIDs = []uint64{}
	cfg.Operators = slices.Clone(profile.Config.Operators)
	cfg.EvidenceV2.Operators = make([]ReleaseEvidenceV2OperatorConfig, 0, len(cfg.Operators))
	for index := range cfg.Operators {
		operator := &cfg.Operators[index]
		operator.StateDir = filepath.Join(cfg.StateDir, fmt.Sprintf("no-%d", operator.NoID))
		operator.NetworkJWTFile, operator.ClientJWTFile = filepath.Join(operator.StateDir, "network.jwt"), filepath.Join(operator.StateDir, "client.jwt")
		operator.ClientKeySeedFile = options.ClientKeySeedFile
		operator.AllowClientRegistration, operator.Concurrency = true, settings.Concurrency
		paths := DefaultReleaseEvidenceV2OperatorPaths(options.EvidenceDir, operator.NoID)
		cfg.EvidenceV2.Operators = append(cfg.EvidenceV2.Operators, ReleaseEvidenceV2OperatorConfig{
			NoID:       operator.NoID,
			Activation: ReleaseEvidenceV2File{Path: paths.Files[0]}, VPKSignature: ReleaseEvidenceV2File{Path: paths.Files[1]},
			HotkeySignature: ReleaseEvidenceV2File{Path: paths.Files[2]}, Context: ReleaseEvidenceV2File{Path: paths.Files[3]},
			History: ReleaseEvidenceV2File{Path: paths.Files[4]}, ReplayScratchRoot: paths.ReplayScratchRoot, SealScratchRoot: paths.SealScratchRoot,
		})
	}
	signer := [32]byte(private.Public().(ed25519.PublicKey))
	cfg.TreasuryApproval = &ReleaseTreasuryApprovalConfig{Signer: attemptHex32(signer), Approval: ReleaseEvidenceV2File{Path: options.OutputPath + ".treasury-approval.json"}}
	approval := TreasuryApproval{
		Schema: TreasuryApprovalSchema, Proposal: profile.Proposal, NativeChain: profile.NativeChain, RuntimeReviewHash: profile.reviewHash,
		ValidatorHotkey: hotkey, SubnetOwner: profile.SubnetOwner, OwnerHotkeys: slices.Clone(profile.OwnerHotkeys),
		FirstNativeEpoch: boundary.Epoch, ValidFromNativeBlock: boundary.Block, ValidThroughNativeBlock: boundary.Block + blocks,
		MaximumSubnetUids: profile.MaximumSubnetUids, MaximumOwnedHotkeys: profile.MaximumOwnedHotkeys,
		Production: &OwnerRecycleProductionApproval{Schema: TreasuryProductionScope, RuntimeCapability: crv4.ValidatorProducerRuntimeProfile,
			EpochScheduleProfile: crv4.TempoDriftEpochScheduleProfile, ValidatorHotkeys: [][32]byte{hotkey},
			MaximumLastUpdateAge: profile.MaximumLastUpdateAge, ValidThroughNativeEpoch: boundary.Epoch + blocks, ActivationNativeHash: boundary.Hash},
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	draft, err := DraftTreasuryApproval(options.OutputPath, raw, signer, approval)
	if err != nil {
		return nil, nil, err
	}
	envelope, _, err := AssembleTreasuryApprovalEnvelope(draft.Encoded, signer, hex.EncodeToString(ed25519.Sign(private, draft.Digest[:])))
	return &cfg, envelope, err
}
