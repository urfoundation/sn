//go:build linux || darwin

// Receipt checkpoints are disposable acceleration, never intent or transaction
// authority. One bounded signed file retains complete canonical absence for the
// exact original intent; eviction or semantic changes require only a rescan.
package validator

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/sys/unix"
)

const productionReceiptCheckpointSchema = "urnetwork-validator-native-receipt-absence-v1"
const productionReceiptCheckpointLimit uint64 = 4096
const productionReceiptCheckpointName = "checkpoint.json"

// The signature binds semantics, original custody and canonical coverage. It
// cannot authorize a send, a new intent or a nonce read at any later boundary.
type productionReceiptCheckpoint struct {
	Schema    string     `json:"schema"`
	Scope     [32]byte   `json:"scope"`
	Through   uint64     `json:"through"`
	BlockHash types.Hash `json:"block_hash"`
	Signature []byte     `json:"signature,omitempty"`
}

// Scope is constructed only after actual retained intent admission. No release
// executable, current configuration or mutable lifecycle status selects it.
type productionReceiptCheckpointOwner struct {
	path       string
	key        *crv4.Keypair
	scope      [32]byte
	preparedAt uint64
	txHash     types.Hash
	checkpoint *productionReceiptCheckpoint
}

// The complete original config identity survives an approved current renewal.
func newProductionReceiptCheckpointOwner(self *ReleaseSteerer, cfg *ReleaseConfig, intent *SteeringIntent) (*productionReceiptCheckpointOwner, error) {
	if self == nil || self.cfg == nil || self.hotkey == nil || cfg == nil || cfg.ownerRecycleProduction == nil || intent == nil || intent.Prepared == nil || productionEconomicIntent(intent) == nil {
		return nil, errors.New("receipt checkpoint lacks authenticated original intent ownership")
	}
	if err := validateOwnerRecycleProductionConfig(cfg); err != nil {
		return nil, err
	}
	txHash, err := types.NewHashFromHexString(intent.Prepared.ExtrinsicHash)
	if err != nil {
		return nil, err
	}
	scope, err := json.Marshal(struct {
		Domain       string   `json:"domain"`
		Genesis      string   `json:"genesis"`
		ConfigHash   [32]byte `json:"config_hash"`
		Hotkey       [32]byte `json:"hotkey"`
		VectorHash   string   `json:"vector_hash"`
		EnvelopeHash string   `json:"envelope_hash"`
		Approval     string   `json:"approval"`
		Extrinsic    string   `json:"extrinsic"`
		PreparedAt   uint64   `json:"prepared_at"`
		PreparedHash string   `json:"prepared_hash"`
		CreatedAt    string   `json:"created_at"`
	}{Domain: productionReceiptCheckpointSchema, Genesis: cfg.GenesisHash, ConfigHash: cfg.ownerRecycleProduction.configHash,
		Hotkey: self.hotkey.PublicKey(), VectorHash: intent.VectorHash, EnvelopeHash: intent.MeasurementEnvelopeHash,
		Approval: productionEconomicIntent(intent).Signature, Extrinsic: intent.Prepared.ExtrinsicHash, PreparedAt: intent.Prepared.PreparedAtBlock,
		PreparedHash: intent.Prepared.PreparedAtBlockHash, CreatedAt: intent.CreatedAt})
	if err != nil {
		return nil, err
	}
	return &productionReceiptCheckpointOwner{path: filepath.Join(self.cfg.StateDir, "native-receipt-cache"), key: self.hotkey, scope: sha256.Sum256(scope), preparedAt: intent.Prepared.PreparedAtBlock, txHash: txHash}, nil
}

// A separate signing domain prevents local cache evidence from masquerading as
// a Substrate extrinsic, source approval or economic measurement.
func productionReceiptCheckpointDigest(value productionReceiptCheckpoint) ([32]byte, error) {
	value.Signature = nil
	encoded, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte("urnetwork/local-receipt-checkpoint/v1\x00"), encoded...)), nil
}

// A short descriptor/flock owner covers local file work only; no network call
// or semantic intent replay occurs while holding this independent namespace.
func (self *productionReceiptCheckpointOwner) directory(ctx context.Context, create bool, visit func(*attemptPrivateDirectory) error) (resultErr error) {
	if ctx == nil || self == nil || self.key == nil || visit == nil {
		return errors.New("receipt checkpoint file owner is incomplete")
	}
	var directory *attemptPrivateDirectory
	var err error
	if create {
		directory, err = openReleaseMeasurementInputV2Parents(ctx, self.path)
	} else {
		directory, err = openAttemptPrivateDirectory(self.path, ctx)
		if releaseMeasurementInputV2OnlyMissing(err) {
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.check(), directory.close(), ctx.Err()) }()
	if directory.anchor.mode&0o077 != 0 || directory.anchor.uid != uint32(os.Geteuid()) {
		return errors.New("receipt checkpoint directory is not private and owned")
	}
	if err := unix.Flock(int(directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.Join(errors.New("receipt checkpoint namespace is already owned"), err)
	}
	return visit(directory)
}

// Missing, empty, other-intent or older semantic versions are cache misses.
// A reusable entry must be canonical and authenticate every coverage field.
func (self *productionReceiptCheckpointOwner) read(ctx context.Context, directory *attemptPrivateDirectory) (*productionReceiptCheckpoint, error) {
	encoded, exists, err := readAttemptPrivateMetadata(ctx, directory, productionReceiptCheckpointName, productionReceiptCheckpointLimit, attemptPrivateMetadataReadIO{closeFile: func(file *os.File) error { return file.Close() }})
	if err != nil || !exists || len(encoded) == 0 {
		return nil, err
	}
	var value productionReceiptCheckpoint
	if err := decodeAttemptStreamV2JSON(encoded, productionReceiptCheckpointLimit, &value); err != nil {
		return nil, err
	}
	if value.Schema != productionReceiptCheckpointSchema || value.Scope != self.scope {
		return nil, nil
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("receipt checkpoint encoding is not canonical"), err)
	}
	digest, err := productionReceiptCheckpointDigest(value)
	if err != nil || !self.key.Verify(digest[:], value.Signature) || value.Through < self.preparedAt || value.Through > uint64(^uint32(0)) || value.BlockHash == (types.Hash{}) {
		return nil, errors.Join(errors.New("receipt checkpoint authenticated coverage differs"), err)
	}
	return &value, nil
}

// Every close completes before a cached prefix becomes visible to its caller.
func (self *productionReceiptCheckpointOwner) load(ctx context.Context) (result *productionReceiptCheckpoint, resultErr error) {
	self.checkpoint = nil
	resultErr = self.directory(ctx, false, func(directory *attemptPrivateDirectory) error {
		var err error
		result, err = self.read(ctx, directory)
		return err
	})
	if resultErr != nil {
		result = nil
	} else if result != nil {
		copy := *result
		self.checkpoint = &copy
	}
	return result, resultErr
}

// Only the shared complete-body scanner supplies new absence. Saving this
// small file never rewrites the durable intent, its history, allowance or age.
func (self *productionReceiptCheckpointOwner) accept(scan *crv4.FinalizedExtrinsicScan) (*productionReceiptCheckpoint, error) {
	txHash, first, previous := scan.Request()
	expectedFirst, expectedPrevious := self.preparedAt, types.Hash{}
	if self.checkpoint != nil {
		expectedFirst, expectedPrevious = self.checkpoint.Through+1, self.checkpoint.BlockHash
	}
	if txHash != self.txHash || first != expectedFirst || previous != expectedPrevious {
		return nil, errors.New("receipt checkpoint scan differs from the original transaction or contiguous prefix")
	}
	number, hash, absent := scan.AbsenceBoundary()
	if !absent || number < self.preparedAt || number > uint64(^uint32(0)) || hash == (types.Hash{}) {
		return nil, errors.New("receipt checkpoint requires complete original-attempt absence")
	}
	value := productionReceiptCheckpoint{Schema: productionReceiptCheckpointSchema, Scope: self.scope, Through: number, BlockHash: hash}
	self.checkpoint = &value
	return &value, nil
}

// The direct composition is useful to owners which require durable success.
// Production recovery handles optional persistence separately from admission.
func (self *productionReceiptCheckpointOwner) save(ctx context.Context, scan *crv4.FinalizedExtrinsicScan) error {
	value, err := self.accept(scan)
	if err != nil {
		return err
	}
	return self.persist(ctx, *value)
}

// An accepted memory prefix remains true even if optional local persistence
// fails. Such a failure never grants a durable-success claim to its caller.
func (self *productionReceiptCheckpointOwner) persist(ctx context.Context, value productionReceiptCheckpoint) error {
	digest, err := productionReceiptCheckpointDigest(value)
	if err != nil {
		return err
	}
	value.Signature, err = self.key.Sign(digest[:])
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil || uint64(len(encoded))+1 > productionReceiptCheckpointLimit {
		return errors.Join(errors.New("receipt checkpoint exceeds its bound"), err)
	}
	encoded = append(encoded, '\n')
	err = self.directory(ctx, true, func(directory *attemptPrivateDirectory) (resultErr error) {
		prior, err := self.read(ctx, directory)
		if err != nil {
			return err
		}
		if prior != nil && prior.Through >= value.Through {
			if prior.Through == value.Through && prior.BlockHash != value.BlockHash {
				return errors.New("receipt checkpoint names conflicting canonical coverage")
			}
			return nil
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		name := ".checkpoint-" + hex.EncodeToString(nonce[:])
		file, err := directory.openFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		state, err := statAttemptPrivateFile(file)
		if err != nil {
			return errors.Join(err, file.Close())
		}
		temporaryOwned := true
		defer func() {
			if temporaryOwned {
				current, err := directory.stat(name)
				if err != nil || !sameHeadEMAStoreV2FileOwner(state, current) {
					resultErr = errors.Join(resultErr, errors.New("receipt checkpoint temporary owner changed"), err)
				} else {
					resultErr = errors.Join(resultErr, unix.Unlinkat(int(directory.file.Fd()), name, 0))
				}
			}
		}()
		written, writeErr := file.Write(encoded)
		if written != len(encoded) && writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		if err := errors.Join(writeErr, file.Sync(), file.Close(), directory.check(), ctx.Err()); err != nil {
			return err
		}
		current, err := directory.stat(name)
		if err != nil || !sameHeadEMAStoreV2FileOwner(state, current) {
			return errors.Join(errors.New("receipt checkpoint temporary owner changed before publication"), err)
		}
		if err := unix.Renameat(int(directory.file.Fd()), name, int(directory.file.Fd()), productionReceiptCheckpointName); err != nil {
			return err
		}
		temporaryOwned = false
		return directory.file.Sync()
	})
	return err
}
