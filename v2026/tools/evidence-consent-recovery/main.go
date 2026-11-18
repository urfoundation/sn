//go:build linux || darwin

// Explicit recovery changes one lost consent envelope, never signed history.
// All private material stays behind the ordinary managed release loaders.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/sdk/v2026"
	"golang.org/x/sys/unix"
)

type recoveryCommandSettings struct {
	// Timeout bounds the complete replay, managed upload and durable adoption.
	Timeout time.Duration
	// MaximumPublicKeys bounds the separately pinned public key response.
	MaximumPublicKeys int
}

func defaultRecoveryCommandSettings() recoveryCommandSettings {
	return recoveryCommandSettings{Timeout: 30 * time.Minute, MaximumPublicKeys: 256}
}

type recoveryCommand struct {
	ConfigPath, ConfigHash         string
	ArtifactPath, ArtifactHash     string
	HeaderPath, HeaderHash         string
	ActivationPath, ActivationHash string
	ServerKeysPath, ServerKeysHash string
	ExpectedManifestHash           string
	ExpectedManifestBytes          uint64
	Scratch                        string
	ExportPreparedConsent          string
	RecoverSameHeaderConsent       bool
	Timeout                        time.Duration
}

func parseRecoveryCommand(arguments []string, settings recoveryCommandSettings) (recoveryCommand, error) {
	var command recoveryCommand
	flags := flag.NewFlagSet("evidence-consent-recovery", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&command.ConfigPath, "config", "", "ordinary managed validator configuration")
	flags.StringVar(&command.ConfigHash, "config-sha256", "", "independent complete configuration SHA-256")
	flags.StringVar(&command.ArtifactPath, "artifact", "", "original public measurement containing the signed closed transition")
	flags.StringVar(&command.ArtifactHash, "artifact-sha256", "", "independent original public artifact SHA-256")
	flags.StringVar(&command.HeaderPath, "expected-header", "", "independently reviewed complete original protocol header JSON")
	flags.StringVar(&command.HeaderHash, "expected-header-sha256", "", "independent complete expected-header file SHA-256")
	flags.StringVar(&command.ActivationPath, "activation-context", "", "original public managed activation context")
	flags.StringVar(&command.ActivationHash, "activation-context-sha256", "", "independent original activation context SHA-256")
	flags.StringVar(&command.ServerKeysPath, "server-keys", "", "pinned public verify server key response")
	flags.StringVar(&command.ServerKeysHash, "server-keys-sha256", "", "independent public verify key response SHA-256")
	flags.StringVar(&command.ExpectedManifestHash, "expected-manifest-sha256", "", "independent exact original fixed-epoch locator SHA-256")
	flags.Uint64Var(&command.ExpectedManifestBytes, "expected-manifest-bytes", 0, "independent exact original locator length")
	flags.StringVar(&command.Scratch, "scratch", "", "fresh absolute replay scratch path")
	flags.StringVar(&command.ExportPreparedConsent, "export-prepared-consent", "", "export only durable public consent to this fresh file; do not sign or repair")
	flags.BoolVar(&command.RecoverSameHeaderConsent, "recover-same-header-consent", false, "explicitly authorize fresh consent for this unchanged header after stopping the validator")
	flags.DurationVar(&command.Timeout, "timeout", settings.Timeout, "complete recovery deadline")
	if err := flags.Parse(arguments); err != nil {
		return command, err
	}
	if !command.RecoverSameHeaderConsent || flags.NArg() != 0 || command.Timeout <= 0 || settings.MaximumPublicKeys <= 0 || command.ExpectedManifestBytes == 0 {
		return command, errors.New("explicit same-header recovery, independent locator pins and finite deadline are required")
	}
	for _, pin := range []string{command.ConfigHash, command.ArtifactHash, command.HeaderHash, command.ActivationHash, command.ServerKeysHash, command.ExpectedManifestHash} {
		if _, err := decodeRecoveryHash(pin); err != nil {
			return command, err
		}
	}
	for _, path := range []string{command.ConfigPath, command.ArtifactPath, command.HeaderPath, command.ActivationPath, command.ServerKeysPath, command.Scratch} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
			return command, errors.New("recovery input and scratch paths must be canonical and absolute")
		}
	}
	if command.ExportPreparedConsent != "" && (!filepath.IsAbs(command.ExportPreparedConsent) || filepath.Clean(command.ExportPreparedConsent) != command.ExportPreparedConsent || filepath.Dir(command.ExportPreparedConsent) == command.ExportPreparedConsent) {
		return command, errors.New("public consent export requires a canonical absolute fresh file")
	}
	return command, nil
}

func decodeRecoveryHash(value string) ([32]byte, error) {
	var hash [32]byte
	raw, err := hex.DecodeString(value)
	if err != nil || len(value) != 64 || value != strings.ToLower(value) || len(raw) != len(hash) {
		return hash, errors.New("independent SHA-256 pins require exactly 64 lowercase hex digits without prefix")
	}
	copy(hash[:], raw)
	if hash == ([32]byte{}) {
		return hash, errors.New("independent recovery pin is empty")
	}
	return hash, nil
}

// Public inputs are bounded and hash-checked before any managed signer loads.
func readRecoveryInput(ctx context.Context, path, pin string, maximum uint64) (result []byte, resultErr error) {
	hash, err := decodeRecoveryHash(pin)
	if err != nil {
		return nil, err
	}
	if maximum == 0 || maximum >= math.MaxInt64 {
		return nil, errors.New("public recovery input has no finite byte allowance")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || uint64(before.Size()) > maximum {
		return nil, errors.Join(errors.New("public recovery input is aliased, missing or oversized"), err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, errors.Join(errors.New("public recovery input changed during open"), err)
	}
	if state, ok := opened.Sys().(*syscall.Stat_t); !ok || state.Nlink != 1 {
		return nil, errors.New("public recovery input has a hardlink alias")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || uint64(len(raw)) > maximum || sha256.Sum256(raw) != hash {
		return nil, errors.Join(errors.New("public recovery input differs from independent content pin"), err)
	}
	return raw, nil
}

func decodeRecoveryJson(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("public recovery input has trailing JSON")
	}
	return nil
}

// Production admissions, source replay, signers and publication cannot be supplied
// by a flag or callback. No relay key, transaction send or history reset exists.
func runRecoveryCommand(ctx context.Context, command recoveryCommand, output io.Writer, settings recoveryCommandSettings) (resultErr error) {
	if !command.RecoverSameHeaderConsent {
		return errors.New("same-header consent recovery requires explicit operator authorization")
	}
	cfg, err := validator.LoadValidatorEvidenceConsentRecoveryConfig(command.ConfigPath, command.ConfigHash)
	if err != nil {
		return err
	}
	headerRaw, err := readRecoveryInput(ctx, command.HeaderPath, command.HeaderHash, cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
	if err != nil {
		return err
	}
	var header protocol.ValidatorEvidenceHeader
	if err := decodeRecoveryJson(headerRaw, &header); err != nil {
		return err
	}
	if err := header.Validate(); err != nil {
		return err
	}
	if header.Kind != protocol.ValidatorEvidenceClosedCensus || header.Subject != (protocol.ValidatorEvidenceSubject{}) {
		return errors.New("recovery accepts only the exact original closed-census header")
	}
	activationRaw, err := readRecoveryInput(ctx, command.ActivationPath, command.ActivationHash, cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil {
		return err
	}
	var activation validator.ReleaseEvidenceV2ActivationContext
	if err := decodeRecoveryJson(activationRaw, &activation); err != nil {
		return err
	}
	canonical, err := activation.CanonicalJSON(cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil || !bytes.Equal(activationRaw, canonical) {
		return errors.Join(errors.New("original activation context is not canonical"), err)
	}
	artifactRaw, err := readRecoveryInput(ctx, command.ArtifactPath, command.ArtifactHash, cfg.EvidenceV2.Bounds.MaxArtifactBytes)
	if err != nil {
		return err
	}
	var artifact validator.ReleaseMeasurementArtifact
	if err := decodeRecoveryJson(artifactRaw, &artifact); err != nil {
		return err
	}
	closure := artifact.SettlementClosureV2
	if artifact.DeploymentID != cfg.DeploymentID || artifact.ChainID != cfg.ChainID || artifact.GenesisHash != cfg.GenesisHash || artifact.Coordinator != cfg.Coordinator || artifact.ValidatorID != cfg.ValidatorID || artifact.Netuid != cfg.Netuid || closure == nil || closure.Epoch != header.Epoch || len(closure.Transitions) != 1 || closure.Transitions[0] == nil || closure.Transitions[0].Identity.NoID != header.NoID {
		return errors.New("original public artifact differs from independent complete recovery identity")
	}
	keysRaw, err := readRecoveryInput(ctx, command.ServerKeysPath, command.ServerKeysHash, cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil {
		return err
	}
	var response sdk.VerifyKeysResult
	if err := decodeRecoveryJson(keysRaw, &response); err != nil {
		return err
	}
	if len(response.Keys) == 0 || len(response.Keys) > settings.MaximumPublicKeys {
		return errors.New("public verify key census is incomplete or oversized")
	}
	keys := map[byte]ed25519.PublicKey{}
	for _, key := range response.Keys {
		if key == nil || key.ServerKeyId < 0 || key.ServerKeyId > 255 || len(key.PublicKey) != ed25519.PublicKeySize || keys[byte(key.ServerKeyId)] != nil {
			return errors.New("public verify key response has duplicate or malformed members")
		}
		keys[byte(key.ServerKeyId)] = bytes.Clone(key.PublicKey)
	}
	manifestPath, err := validator.ValidatorEvidencePublicationV2ManifestPath(cfg.StateDir, header.Epoch)
	if err != nil {
		return err
	}
	manifestRaw, err := validator.ReadReleaseEvidenceV2SetupFile(ctx, manifestPath, cfg.EvidenceV2.Bounds.MaxClosureBytes)
	if err != nil {
		return err
	}
	// A completed retry may already select the prepared replacement. Core recovery
	// owns that recognition; the byte pin still bounds the independent old locator.
	if command.ExpectedManifestBytes > cfg.EvidenceV2.Bounds.MaxClosureBytes || len(manifestRaw) == 0 {
		return errors.New("independent original manifest length exceeds recovery allowance")
	}
	manifestHash, err := decodeRecoveryHash(command.ExpectedManifestHash)
	if err != nil {
		return err
	}
	if sha256.Sum256(manifestRaw) == manifestHash && uint64(len(manifestRaw)) != command.ExpectedManifestBytes {
		return errors.New("original manifest differs from independently pinned exact length")
	}
	if command.ExportPreparedConsent != "" {
		for _, operator := range cfg.Operators {
			if recoveryPathWithin(command.ExportPreparedConsent, operator.StateDir) {
				return errors.New("public consent export overlaps retained managed state")
			}
		}
		if recoveryPathWithin(command.ExportPreparedConsent, cfg.StateDir) || len(cfg.Operators) != 1 || cfg.Operators[0].NoID != header.NoID {
			return errors.New("public consent export differs from the complete independent managed owner")
		}
		projection, err := validator.ReadPreparedValidatorEvidenceConsentRecoveryV2(ctx, validator.ValidatorEvidenceConsentRecoveryV2Options{
			StateDir: cfg.StateDir, OperatorStateDir: cfg.Operators[0].StateDir, ExpectedManifestHash: manifestHash, ExpectedManifestBytes: command.ExpectedManifestBytes, ExpectedHeader: header,
			Census: validator.ValidatorEvidenceCensusV2Options{Settlement: validator.AttemptSettlementV2Options{MaxClosureBytes: cfg.EvidenceV2.Bounds.MaxClosureBytes}},
		})
		if err != nil {
			return err
		}
		return writeRecoveryProjection(ctx, command.ExportPreparedConsent, projection, output)
	}
	options, closeManaged, err := validator.OpenValidatorEvidenceConsentRecoveryV2(ctx, cfg, header, activation, closure.Transitions[0].Cut.Context, command.Scratch, keys)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeManaged(), ctx.Err()) }()
	options.ExpectedManifestHash = manifestHash
	options.ExpectedManifestBytes = command.ExpectedManifestBytes
	options.OriginalConsentUnavailable = command.RecoverSameHeaderConsent
	result, err := validator.RepairValidatorEvidenceConsentV2(ctx, closure, options)
	if err != nil {
		return err
	}
	if err := errors.Join(closeManaged(), ctx.Err()); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func recoveryPathWithin(path, directory string) bool {
	return path == directory || strings.HasPrefix(path, directory+string(filepath.Separator))
}

// A public-only projection never overwrites any path and reports success only
// after the new file and its private staging directory have synced and closed.
func writeRecoveryProjection(ctx context.Context, path string, raw []byte, output io.Writer) (resultErr error) {
	return writeRecoveryProjectionWithHooks(ctx, path, raw, output, recoveryProjectionHooks{})
}

// Observers bracket actual physical operations without replacing their results.
type recoveryProjectionHooks struct {
	afterDirectoryOpen  func() error
	afterDirectoryClose func() error
}

func writeRecoveryProjectionWithHooks(ctx context.Context, path string, raw []byte, output io.Writer, hooks recoveryProjectionHooks) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(raw) == 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("public consent projection or fresh output path is absent")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.Join(errors.New("public consent projection requires a pre-existing private staging directory"), err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() {
		if directory != nil {
			resultErr = errors.Join(resultErr, directory.Close())
		}
		resultErr = errors.Join(resultErr, ctx.Err())
	}()
	opened, err := directory.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.Join(errors.New("public consent staging directory changed during open"), err)
	}
	if hooks.afterDirectoryOpen != nil {
		if err := hooks.afterDirectoryOpen(); err != nil {
			return err
		}
	}
	fd, err := unix.Openat(int(directory.Fd()), filepath.Base(path), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	count, writeErr := file.Write(raw)
	if count != len(raw) {
		writeErr = errors.Join(writeErr, io.ErrShortWrite)
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := errors.Join(directory.Sync(), ctx.Err()); err != nil {
		return err
	}
	if err := directory.Close(); err != nil {
		return err
	}
	// The descriptor has joined before the receipt; its second deferred Close
	// is deliberately disabled rather than turning a successful export into error.
	directory = nil
	if hooks.afterDirectoryClose != nil {
		if err := hooks.afterDirectoryClose(); err != nil {
			return err
		}
	}
	after, err := os.Lstat(filepath.Dir(path))
	if err != nil || !after.IsDir() || !os.SameFile(info, after) || after.Mode().Perm() != info.Mode().Perm() {
		return errors.Join(errors.New("public consent staging directory changed before receipt"), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		PublicProjection bool   `json:"public_projection"`
		RecoveryComplete bool   `json:"recovery_completed"`
		NewSignatures    bool   `json:"new_signatures"`
		Hash             string `json:"metadata_hash"`
		Bytes            uint64 `json:"metadata_bytes"`
		Path             string `json:"public_path"`
	}{PublicProjection: true, Hash: fmt.Sprintf("0x%x", sha256.Sum256(raw)), Bytes: uint64(len(raw)), Path: path})
}

func main() {
	settings := defaultRecoveryCommandSettings()
	command, err := parseRecoveryCommand(os.Args[1:], settings)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), command.Timeout)
		defer cancel()
		err = runRecoveryCommand(ctx, command, os.Stdout, settings)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
