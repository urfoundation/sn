//go:build linux || darwin

// With --startup-verified-progress the units a production process verified are
// also kept in the state directory, signed by the validator hotkey, and a later
// process skips what a predecessor already verified. That changes what a start
// re-proves: releaseStartupVerifiedAccepts states exactly which predecessor it
// trusts. Without the flag no record is read or written.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"golang.org/x/sys/unix"
)

const (
	releaseStartupVerifiedSigningDomain = "urnetwork/validator/startup-verified/v1"
	releaseStartupVerifiedFileName      = "startup-verified-v1.json"
	// Raised whenever a remembered unit's verification changes meaning. A
	// record from another verifier is ignored, so everything is verified once
	// again by the first process that runs the new one.
	releaseStartupVerifiedVerifier     = 1
	releaseStartupVerifiedMaximumUnits = 1 << 17
	releaseStartupVerifiedMaximumBytes = 16 << 20
	releaseStartupVerifiedFlushDelay   = time.Minute
)

// The live sealer has just completed the same replay itself.
func (self *releaseStartupVerified) recordStatsCut(measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2StatsOptions) {
	if self == nil {
		return
	}
	if inputs, ok := releaseStartupVerifiedStatsCutInputs(measurement, cut, expected, policy, bounds, options); ok {
		if unit, ok := releaseStartupVerifiedUnit("stats-cut", inputs); ok {
			self.add(unit)
		}
	}
}

// A chain digest commits to every earlier entry, so one match is the prefix.
func (self *releaseStartupVerified) intentVerified(digests [][32]byte, index int) bool {
	if self == nil || index < 0 || index >= len(digests) {
		return false
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return index < len(self.intents) && self.intents[index] == digests[index]
}

type releaseStartupVerifiedPersistenceKey struct{}

// `validator run --startup-verified-progress` asks for the signed record.
func withReleaseStartupVerifiedPersistence(ctx context.Context) context.Context {
	return context.WithValue(ctx, releaseStartupVerifiedPersistenceKey{}, true)
}

func releaseStartupVerifiedPersistenceFrom(ctx context.Context) bool {
	enabled, _ := ctx.Value(releaseStartupVerifiedPersistenceKey{}).(bool)
	return enabled
}

// Everything a remembered verdict assumes that is not in its own digest.
type releaseStartupVerifiedAuthority struct {
	chainID      uint64
	deploymentID string
	validatorID  uint64
	hotkey       [32]byte
	configHash   [32]byte
	// The current signed configuration and every predecessor it names.
	lineage    map[[32]byte]bool
	bridge     []ReleaseNativeGapBridgeEdge
	serverKeys []releaseStartupVerifiedServerKey
}

type releaseStartupVerifiedServerKey struct {
	NoID    uint64 `json:"no_id"`
	Version byte   `json:"version"`
	Key     string `json:"key"`
}

// Only a plain production run keeps a record: no provisional setup, history
// adoption or deferred native input, whose verdicts follow other rules.
func releaseStartupVerifiedAuthorityFor(cfg *ReleaseConfig, hotkey [32]byte, serverKeys map[uint64]map[byte]ed25519.PublicKey) (releaseStartupVerifiedAuthority, error) {
	if !isOwnerRecycleProductionConfig(cfg) || cfg.ownerRecycleProduction == nil || cfg.historyAdoptionV2 != nil || cfg.ProvisionalDeferClosedNativeInput || hotkey == ([32]byte{}) {
		return releaseStartupVerifiedAuthority{}, errors.New("startup verified progress needs a plain production configuration")
	}
	authority := releaseStartupVerifiedAuthority{chainID: cfg.ChainID, deploymentID: cfg.DeploymentID, validatorID: cfg.ValidatorID, hotkey: hotkey,
		configHash: cfg.ownerRecycleProduction.configHash, lineage: map[[32]byte]bool{cfg.ownerRecycleProduction.configHash: true}, bridge: cfg.nativeGapBridge.Edges()}
	if cfg.productionAuthorityHistory != nil {
		for _, entry := range cfg.productionAuthorityHistory.entries {
			if entry.config == nil || entry.config.ownerRecycleProduction == nil {
				return releaseStartupVerifiedAuthority{}, errors.New("startup verified progress found an unloaded authority predecessor")
			}
			authority.lineage[entry.config.ownerRecycleProduction.configHash] = true
		}
	}
	listed, err := releaseStartupVerifiedServerKeysOf(serverKeys)
	if err != nil {
		return releaseStartupVerifiedAuthority{}, err
	}
	authority.serverKeys = listed
	return authority, nil
}

// A server-key census as the record lists it, in canonical order.
func releaseStartupVerifiedServerKeysOf(serverKeys map[uint64]map[byte]ed25519.PublicKey) ([]releaseStartupVerifiedServerKey, error) {
	var listed []releaseStartupVerifiedServerKey
	for noID, keys := range serverKeys {
		for version, key := range keys {
			if len(key) != ed25519.PublicKeySize {
				return nil, errors.New("startup verified progress server key has invalid width")
			}
			listed = append(listed, releaseStartupVerifiedServerKey{NoID: noID, Version: version, Key: hex.EncodeToString(key)})
		}
	}
	sortReleaseStartupVerifiedServerKeys(listed)
	return listed, nil
}

func sortReleaseStartupVerifiedServerKeys(keys []releaseStartupVerifiedServerKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].NoID != keys[j].NoID {
			return keys[i].NoID < keys[j].NoID
		}
		return keys[i].Version < keys[j].Version
	})
}

// The signed record. Units and intents are hex digests; the header names the
// authority under which the writer accepted all of them.
type releaseStartupVerifiedFile struct {
	Schema       string                            `json:"schema"`
	Verifier     uint64                            `json:"verifier"`
	ChainID      uint64                            `json:"chain_id"`
	DeploymentID string                            `json:"deployment_id"`
	ValidatorID  uint64                            `json:"validator_id"`
	Hotkey       string                            `json:"hotkey"`
	ConfigHash   string                            `json:"config_hash"`
	BridgeEdges  []ReleaseNativeGapBridgeEdge      `json:"bridge_edges"`
	ServerKeys   []releaseStartupVerifiedServerKey `json:"server_keys"`
	Intents      []string                          `json:"intents"`
	Units        []string                          `json:"units"`
	Signature    string                            `json:"signature"`
}

// What a later process trusts. A record is used only when its hotkey
// signature verifies and every condition below holds; otherwise it is ignored
// and the start verifies everything, as without the flag.
//   - The same verifier version, chain, deployment, validator and hotkey.
//   - Its configuration is the current one or a signed predecessor the current
//     configuration names. Those keep policy, identities, operators, proof
//     bounds and custody, so an earlier verdict holds under the current one.
//   - Its bridge edges are the current bridge's leading edges; a later bridge
//     only appends edges.
//   - Each server key it names is still the same key; a later key set only
//     adds versions, which cannot make a verified record fail.
//
// What is then not proved again for a remembered unit: the public origin still
// serving that cut, the archive node still serving that historical state, and
// a remembered intent's chain sources and lineage. The retained files are
// still read and matched to the content hashes the intents and journals pin,
// every local structural and successor rule still runs, and everything not
// remembered is verified in full.
func releaseStartupVerifiedAccepts(file *releaseStartupVerifiedFile, authority releaseStartupVerifiedAuthority) error {
	if file.Schema != releaseStartupVerifiedSchema || file.Verifier != releaseStartupVerifiedVerifier {
		return errors.New("record belongs to another verifier")
	}
	if file.ChainID != authority.chainID || file.DeploymentID != authority.deploymentID || file.ValidatorID != authority.validatorID || file.Hotkey != releaseHex32(authority.hotkey) {
		return errors.New("record belongs to another deployment, validator or hotkey")
	}
	configHash, err := canonicalAttemptHex32("startup verified configuration", file.ConfigHash, false)
	if err != nil || !authority.lineage[configHash] {
		return errors.Join(errors.New("record configuration is neither current nor a signed predecessor"), err)
	}
	if len(file.BridgeEdges) > len(authority.bridge) || !slices.Equal(file.BridgeEdges, authority.bridge[:len(file.BridgeEdges)]) {
		return errors.New("record bridge edges are not the current bridge's leading edges")
	}
	current := make(map[[2]uint64]string, len(authority.serverKeys))
	for _, key := range authority.serverKeys {
		current[[2]uint64{key.NoID, uint64(key.Version)}] = key.Key
	}
	for _, key := range file.ServerKeys {
		if current[[2]uint64{key.NoID, uint64(key.Version)}] != key.Key {
			return errors.New("record names a server key that is no longer current")
		}
	}
	return nil
}

// The persisted side of one production run.
type releaseStartupVerifiedRecord struct {
	path      string
	hotkey    *crv4.Keypair
	authority releaseStartupVerifiedAuthority
	writeLock sync.Mutex
	stderr    io.Writer
	full      bool
	// Every server-key version this process admitted: startup's census and each
	// version the live service added since. Guarded by the owner's stateLock;
	// replaced, never modified, so a flush may keep the slice it read.
	serverKeys []releaseStartupVerifiedServerKey
	// A census that could not be listed stops all further writes.
	unlisted bool
}

// Reads and admits the record before the first remembered unit is consulted.
// A missing, unreadable or unaccepted record is reported and ignored.
func (self *releaseStartupVerified) enableRecord(stateDir string, hotkey *crv4.Keypair, authority releaseStartupVerifiedAuthority, stderr io.Writer) {
	if self == nil || hotkey == nil || !filepath.IsAbs(stateDir) {
		return
	}
	record := &releaseStartupVerifiedRecord{path: filepath.Join(stateDir, releaseStartupVerifiedFileName), hotkey: hotkey, authority: authority, stderr: stderr, serverKeys: authority.serverKeys}
	file, err := readReleaseStartupVerifiedFile(record.path)
	if err == nil && file != nil {
		err = releaseStartupVerifiedAccepts(file, authority)
	}
	var units, intents [][32]byte
	if err == nil && file != nil {
		units, err = decodeReleaseStartupVerifiedDigests(file.Units)
		if err == nil {
			intents, err = decodeReleaseStartupVerifiedDigests(file.Intents)
		}
	}
	self.stateLock.Lock()
	self.record = record
	if err == nil && file != nil {
		for _, unit := range units {
			self.units[unit] = struct{}{}
		}
		if len(self.intents) == 0 {
			self.intents = intents
		}
	}
	self.dirty = true
	self.stateLock.Unlock()
	switch {
	case err != nil:
		fmt.Fprintf(stderr, "validator: startup verified progress record ignored; this start verifies the complete history: %v\n", err)
	case file == nil:
		fmt.Fprintf(stderr, "validator: no startup verified progress record yet; this start verifies the complete history\n")
	default:
		fmt.Fprintf(stderr, "validator: startup verified progress record accepted: %d units and %d intents already verified\n", len(units), len(intents))
	}
}

// The live service admits new server-key versions and then verifies cuts under
// them, so each later flush names every version admitted so far: a successor
// keeps those units only while each named key is still current. A listed
// version is never dropped; one whose bytes differ stops all further writes.
func (self *releaseStartupVerified) observeServerKeys(serverKeys map[uint64]map[byte]ed25519.PublicKey) {
	if self == nil {
		return
	}
	admitted, err := releaseStartupVerifiedServerKeysOf(serverKeys)
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	record := self.record
	if record == nil || record.unlisted {
		return
	}
	listed := make(map[[2]uint64]string, len(record.serverKeys))
	for _, key := range record.serverKeys {
		listed[[2]uint64{key.NoID, uint64(key.Version)}] = key.Key
	}
	merged := slices.Clone(record.serverKeys)
	for _, key := range admitted {
		prior, found := listed[[2]uint64{key.NoID, uint64(key.Version)}]
		if found && prior != key.Key {
			err = errors.Join(err, errors.New("a listed server-key version changed"))
		} else if !found {
			merged = append(merged, key)
		}
	}
	if err != nil {
		record.unlisted = true
		fmt.Fprintf(record.stderr, "validator: startup verified progress record is no longer updated: %v\n", err)
		return
	}
	if len(merged) != len(record.serverKeys) {
		sortReleaseStartupVerifiedServerKeys(merged)
		record.serverKeys = merged
		self.dirty = true
	}
}

func decodeReleaseStartupVerifiedDigests(values []string) ([][32]byte, error) {
	result := make([][32]byte, len(values))
	for index, value := range values {
		digest, err := canonicalAttemptHex32("startup verified digest", value, false)
		if err != nil {
			return nil, err
		}
		result[index] = digest
	}
	return result, nil
}

func releaseStartupVerifiedSigningDigest(file *releaseStartupVerifiedFile) ([32]byte, error) {
	unsigned := *file
	unsigned.Signature = ""
	encoded, err := json.Marshal(&unsigned)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append(append([]byte(releaseStartupVerifiedSigningDomain), 0), encoded...)), nil
}

// A private regular file of bounded size whose canonical bytes carry a valid
// hotkey signature. Absence is not an error.
func readReleaseStartupVerifiedFile(path string) (*releaseStartupVerifiedFile, error) {
	handle, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > releaseStartupVerifiedMaximumBytes {
		return nil, errors.New("record is not a private regular file within its byte bound")
	}
	encoded, err := io.ReadAll(io.LimitReader(handle, releaseStartupVerifiedMaximumBytes+1))
	if err != nil {
		return nil, err
	}
	if len(encoded) > releaseStartupVerifiedMaximumBytes {
		return nil, errors.New("record exceeds its byte bound")
	}
	if err := protocol.ValidateUniqueJsonKeys(encoded); err != nil {
		return nil, err
	}
	var file releaseStartupVerifiedFile
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(&file)
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("record is not canonical"), err)
	}
	if len(file.Units) > releaseStartupVerifiedMaximumUnits || len(file.Intents) > releaseStartupVerifiedMaximumUnits {
		return nil, errors.New("record exceeds its digest bound")
	}
	hotkey, err := canonicalAttemptHex32("startup verified hotkey", file.Hotkey, false)
	if err != nil {
		return nil, err
	}
	signature, err := parseReleaseMeasurementEnvelopeSignature(file.Signature)
	if err != nil {
		return nil, err
	}
	digest, err := releaseStartupVerifiedSigningDigest(&file)
	if err != nil {
		return nil, err
	}
	publicKey, err := (sr25519.Scheme{}).FromPublicKey(hotkey[:])
	if err != nil || !publicKey.Verify(digest[:], signature) {
		return nil, errors.Join(errors.New("record signature is invalid"), err)
	}
	return &file, nil
}

// Writes the current units under the current authority, then renames the
// signed bytes into place. Every remembered unit was verified in this process
// or admitted from a record whose authority the current one extends.
func (self *releaseStartupVerified) flush() error {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	record := self.record
	if record == nil || !self.dirty || record.unlisted {
		self.stateLock.Unlock()
		return nil
	}
	units := make([]string, 0, len(self.units))
	for unit := range self.units {
		units = append(units, attemptHex32(unit))
	}
	intents := make([]string, len(self.intents))
	for index, digest := range self.intents {
		intents[index] = attemptHex32(digest)
	}
	// Read with the units: every unit was verified under a key listed here.
	serverKeys := record.serverKeys
	self.dirty = false
	self.stateLock.Unlock()
	record.writeLock.Lock()
	defer record.writeLock.Unlock()
	if len(units) > releaseStartupVerifiedMaximumUnits || len(intents) > releaseStartupVerifiedMaximumUnits {
		if !record.full {
			record.full = true
			fmt.Fprintf(record.stderr, "validator: startup verified progress exceeds its record bound; later starts verify what it omits\n")
		}
		return nil
	}
	sort.Strings(units)
	authority := record.authority
	file := releaseStartupVerifiedFile{Schema: releaseStartupVerifiedSchema, Verifier: releaseStartupVerifiedVerifier, ChainID: authority.chainID, DeploymentID: authority.deploymentID,
		ValidatorID: authority.validatorID, Hotkey: releaseHex32(authority.hotkey), ConfigHash: releaseHex32(authority.configHash), BridgeEdges: authority.bridge,
		ServerKeys: serverKeys, Intents: intents, Units: units}
	if file.BridgeEdges == nil {
		file.BridgeEdges = []ReleaseNativeGapBridgeEdge{}
	}
	if file.ServerKeys == nil {
		file.ServerKeys = []releaseStartupVerifiedServerKey{}
	}
	digest, err := releaseStartupVerifiedSigningDigest(&file)
	if err != nil {
		return err
	}
	signature, err := record.hotkey.Sign(digest[:])
	if err != nil {
		return err
	}
	file.Signature = "0x" + hex.EncodeToString(signature)
	encoded, err := json.Marshal(&file)
	if err != nil {
		return err
	}
	err = writeReleaseStartupVerifiedFile(record.path, append(encoded, '\n'))
	if err != nil {
		self.stateLock.Lock()
		self.dirty = true
		self.stateLock.Unlock()
	}
	return err
}

func writeReleaseStartupVerifiedFile(path string, encoded []byte) (resultErr error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+releaseStartupVerifiedFileName+".*")
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporary.Name())
		}
	}()
	written, err := temporary.Write(encoded)
	if err == nil && written != len(encoded) {
		err = io.ErrShortWrite
	}
	if err := errors.Join(err, temporary.Chmod(0o600), temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return err
	}
	parent, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(parent.Sync(), parent.Close())
}

// Keeps the record current while the service runs, so the next start skips
// what the live service verified. A write failure is reported, not fatal.
func (self *releaseStartupVerified) flushUntilDone(ctx context.Context, stderr io.Writer) {
	if self == nil || ctx == nil {
		return
	}
	ticker := time.NewTicker(releaseStartupVerifiedFlushDelay)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := self.flush(); err != nil {
				fmt.Fprintf(stderr, "validator: startup verified progress record not written: %v\n", err)
			}
		}
	}
}
