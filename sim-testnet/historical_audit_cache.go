package main

// This cache remembers successful immutable historical proofs, never current
// postconditions. Callers must authenticate local evidence and obtain fresh
// canonical/finalized checkpoints before consulting it. Cache failures are
// misses: losing an optimization must neither authorize an action nor add a
// new execution gate.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	historicalAuditCacheSchema          = "urnetwork-sim-historical-audit-cache-v1"
	historicalAuditCacheVerifierVersion = "immutable-history-v1"
	historicalAuditCacheDirectoryName   = "historical-audit-cache-v1"
	historicalAuditCacheMaximumBytes    = 16 * 1024
)

type historicalAuditCacheProof struct {
	Schema           string `json:"schema"`
	VerifierVersion  string `json:"verifier_version"`
	ExecutableSHA256 string `json:"executable_sha256"`
	ContextHash      string `json:"context_hash"`
	Kind             string `json:"kind"`
	InputHash        string `json:"input_hash"`
	Success          bool   `json:"success"`
}

type historicalAuditCacheEnvelope struct {
	Proof historicalAuditCacheProof `json:"proof"`
	MAC   string                    `json:"hmac_sha256"`
}

// A prepared entry lets batched readers look up every action first and save
// each completed proof without replacing their cold RPC batches with calls
// per action. The authentication key never leaves process memory.
type historicalAuditCacheEntry struct {
	stateDir string
	name     string
	proof    historicalAuditCacheProof
	key      [32]byte
}

var historicalAuditExecutableIdentity struct {
	sync.Once
	digest string
	err    error
}

// Hash the running image once. Linux's process image remains the correct
// identity even if another process replaces the executable's original path.
func historicalAuditExecutableSHA256() (string, error) {
	historicalAuditExecutableIdentity.Do(func() {
		path := "/proc/self/exe"
		if runtime.GOOS != "linux" {
			var err error
			path, err = os.Executable()
			if err != nil {
				historicalAuditExecutableIdentity.err = err
				return
			}
		}
		file, err := os.Open(path)
		if err != nil {
			historicalAuditExecutableIdentity.err = err
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = errors.New("running audit executable is not a regular file")
			}
			historicalAuditExecutableIdentity.err = err
			return
		}
		digest := sha256.New()
		if _, err := io.Copy(digest, file); err != nil {
			historicalAuditExecutableIdentity.err = err
			return
		}
		historicalAuditExecutableIdentity.digest = hex.EncodeToString(digest.Sum(nil))
	})
	return historicalAuditExecutableIdentity.digest, historicalAuditExecutableIdentity.err
}

// Bind the authenticated plan identity without reserializing thousands of
// actions on every lookup. Exact action and dependency inputs belong in the
// caller's proof input. A changed lineage, release lock, policy, role identity
// or RPC authorization cannot inherit an earlier success. The
// validated campaign loopback hop is not a new authorized RPC domain; callers
// include the actual observer role/domain in input when a proof is per reader.
func (e *Executor) historicalAuditContextHash(cfg *ResolvedConfig) (string, error) {
	return canonicalHashHex(struct {
		PlanSchema           string
		PlanHash             string
		PlanRelease          string
		PlanReleaseLockHash  string
		PlanDeploymentID     string
		PlanConfigHash       string
		PlanResolvedHash     string
		PlanPolicyHash       string
		PlanChainID          uint64
		PlanGenesisHash      string
		PlanNetuid           uint16
		PriorPlanHashes      []string
		Config               *HarnessConfig
		Public               *PublicManifest
		Release              *ReleaseLock
		Hyperparameters      *Hyperparameters
		Policy               any
		ConfigHash           string
		PolicyHash           string
		ChainID              uint64
		Netuid               uint16
		Authority            string
		OperationalRPCMode   string
		OperationalSubstrate string
		OperationalEVM       string
		IndependentRPC       bool
		WalletPublic         string
		WalletHotkeyPublic   string
	}{
		PlanSchema: e.plan.Schema, PlanHash: e.plan.PlanHash, PlanRelease: e.plan.Release,
		PlanReleaseLockHash: e.plan.ReleaseLockHash, PlanDeploymentID: e.plan.DeploymentID,
		PlanConfigHash: e.plan.ConfigHash, PlanResolvedHash: e.plan.ResolvedInputsHash,
		PlanPolicyHash: e.plan.PolicyHash, PlanChainID: e.plan.ChainID,
		PlanGenesisHash: e.plan.GenesisHash, PlanNetuid: e.plan.Netuid,
		PriorPlanHashes: e.plan.PriorPlanHashes,
		Config:          cfg.Config, Public: cfg.Public, Release: cfg.Release,
		Hyperparameters: cfg.Hyperparameters, Policy: cfg.Policy,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID,
		Netuid: cfg.Netuid, Authority: cfg.Authority, OperationalRPCMode: cfg.OperationalRPCMode,
		OperationalSubstrate: cfg.OperationalSubstrate, OperationalEVM: cfg.OperationalEVM,
		IndependentRPC: independentRPCRequired(cfg), WalletPublic: cfg.WalletPublic,
		WalletHotkeyPublic: cfg.WalletHotkeyPublic,
	})
}

// lookupHistoricalAuditCache performs no verification and creates no files.
// A nonnil miss is a prepared entry whose saveSuccess may be called only once
// all immutable checks required by its exact input have succeeded. In
// particular, dual-observer action proofs require both observers to succeed.
func (e *Executor) lookupHistoricalAuditCache(ctx context.Context, kind string, input any) (*historicalAuditCacheEntry, bool) {
	if ctx == nil || ctx.Err() != nil || e == nil || e.plan == nil || e.plan.PlanHash == "" || e.stateDir == "" || kind == "" || len(kind) > 128 || input == nil {
		return nil, false
	}
	cfg := e.auditAuthorizedConfig
	if cfg == nil {
		cfg = e.cfg
	}
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Release == nil || cfg.WalletMaterial == "" || cfg.Config.Deployment.DeploymentID == "" {
		return nil, false
	}
	executableHash, err := historicalAuditExecutableSHA256()
	if err != nil {
		return nil, false
	}
	contextHash, err := e.historicalAuditContextHash(cfg)
	if err != nil {
		return nil, false
	}
	inputHash, err := canonicalHashHex(input)
	if err != nil {
		return nil, false
	}
	entry := &historicalAuditCacheEntry{
		stateDir: e.stateDir,
		key:      derive32(cfg, "historical-audit-cache/v1"),
		proof: historicalAuditCacheProof{
			Schema: historicalAuditCacheSchema, VerifierVersion: historicalAuditCacheVerifierVersion,
			ExecutableSHA256: executableHash, ContextHash: contextHash, Kind: kind,
			InputHash: inputHash, Success: true,
		},
	}
	nameHash, err := canonicalHashHex(entry.proof)
	if err != nil {
		return nil, false
	}
	entry.name = strings.TrimPrefix(nameHash, "0x") + ".json"
	hit := entry.readSuccess()
	if ctx.Err() != nil {
		return nil, false
	}
	return entry, hit
}

// withHistoricalAuditCache leaves freshness and local evidence validation
// outside its boundary. A success is durable independently of later audits;
// failed or canceled verification never creates an entry.
func (e *Executor) withHistoricalAuditCache(ctx context.Context, kind string, input any, verify func(context.Context) error) (bool, error) {
	if ctx == nil || verify == nil {
		return false, errors.New("historical audit context or verifier is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	entry, hit := e.lookupHistoricalAuditCache(ctx, kind, input)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if hit {
		return true, nil
	}
	if err := verify(ctx); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	entry.saveSuccess(ctx)
	return false, ctx.Err()
}

func (entry *historicalAuditCacheEntry) authenticationTag(proof historicalAuditCacheProof) []byte {
	wire, _ := json.Marshal(proof) // The proof contains only strings and a bool.
	mac := hmac.New(sha256.New, entry.key[:])
	mac.Write([]byte(historicalAuditCacheSchema + "\x00"))
	mac.Write(wire)
	return mac.Sum(nil)
}

// Open every directory relative to an already opened descriptor. No path
// component, including the configured state directory, may be a symlink.
// Only the cache directory may be created, after successful verification.
func openHistoricalAuditCacheDirectory(stateDir string, create bool) (*os.File, error) {
	path, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		next, openErr := unix.Openat(fd, component, flags, 0)
		unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	defer func() { unix.Close(fd) }()
	if err := requireHistoricalAuditPrivateMode(fd, unix.S_IFDIR, 0o700); err != nil {
		return nil, err
	}
	if create {
		if err := unix.Mkdirat(fd, historicalAuditCacheDirectoryName, 0o700); err == nil {
			// Persist the newly created directory's entry in the state root.
			_ = unix.Fsync(fd)
		} else if !errors.Is(err, unix.EEXIST) {
			return nil, err
		}
	}
	cacheFD, err := unix.Openat(fd, historicalAuditCacheDirectoryName, flags, 0)
	if err != nil {
		return nil, err
	}
	if err := requireHistoricalAuditPrivateMode(cacheFD, unix.S_IFDIR, 0o700); err != nil {
		unix.Close(cacheFD)
		return nil, err
	}
	return os.NewFile(uintptr(cacheFD), historicalAuditCacheDirectoryName), nil
}

func requireHistoricalAuditPrivateMode(fd int, kind uint32, permissions uint32) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != kind || stat.Mode&0o7777 != permissions || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("historical audit cache has an unsafe file type, owner or mode")
	}
	return nil
}

func (entry *historicalAuditCacheEntry) readSuccess() bool {
	if entry == nil {
		return false
	}
	directory, err := openHistoricalAuditCacheDirectory(entry.stateDir, false)
	if err != nil {
		return false
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), entry.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(fd), entry.name)
	defer file.Close()
	if err := requireHistoricalAuditPrivateMode(fd, unix.S_IFREG, 0o600); err != nil {
		return false
	}
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > historicalAuditCacheMaximumBytes {
		return false
	}
	wire, err := io.ReadAll(io.LimitReader(file, historicalAuditCacheMaximumBytes+1))
	if err != nil || len(wire) > historicalAuditCacheMaximumBytes || rejectDuplicatePostconditionJSONFields(wire) != nil {
		return false
	}
	var envelope historicalAuditCacheEnvelope
	if err := decodeStrictJSONBytes(wire, &envelope); err != nil || envelope.Proof != entry.proof {
		return false
	}
	tag, err := hex.DecodeString(envelope.MAC)
	return err == nil && hmac.Equal(tag, entry.authenticationTag(envelope.Proof))
}

// saveSuccess is best effort and nil-safe. Descriptor-relative creation and
// rename prevent a substituted directory or leaf from redirecting a write.
// Concurrent successful writers publish the same authenticated proof, with
// private temporary files and an atomic, synced replacement.
func (entry *historicalAuditCacheEntry) saveSuccess(ctx context.Context) {
	if entry == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	envelope := historicalAuditCacheEnvelope{Proof: entry.proof, MAC: hex.EncodeToString(entry.authenticationTag(entry.proof))}
	wire, err := json.Marshal(envelope)
	if err != nil || len(wire) >= historicalAuditCacheMaximumBytes {
		return
	}
	directory, err := openHistoricalAuditCacheDirectory(entry.stateDir, true)
	if err != nil {
		return
	}
	defer directory.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return
	}
	temporary := ".tmp-" + hex.EncodeToString(random[:])
	directoryFD := int(directory.Fd())
	fd, err := unix.Openat(directoryFD, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return
	}
	defer unix.Unlinkat(directoryFD, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	if file.Chmod(0o600) != nil {
		return
	}
	if _, err := file.Write(append(wire, '\n')); err != nil || file.Sync() != nil {
		return
	}
	if file.Close() != nil || ctx.Err() != nil {
		return
	}
	if err := unix.Renameat(directoryFD, temporary, directoryFD, entry.name); err != nil {
		return
	}
	_ = directory.Sync()
}
