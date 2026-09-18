// Immutable fleet signatures survive runner restarts. Reuse is local to the
// exact verifier/configuration and unchanged source files; epoch validity,
// descriptor selection, cross-fleet uniqueness and chain reads remain fresh.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
)

const (
	fleetCensusCacheSchema        = "urnetwork-sim-fleet-census-cache-v1"
	fleetCensusCacheDirectoryName = "fleet-census-cache-v1"
	fleetCensusCacheMaximumBytes  = 16 * 1024 * 1024
	// Bump whenever inspectOneFleetEvidenceBytes changes its acceptance rules.
	fleetCensusVerifierVersion = "signed-fleet-evidence-v1"
)

// Ctime prevents an in-place rewrite from inheriting a proof after restoring
// the original mtime. Inode/device also distinguish atomic replacements.
type fleetCensusFileWitness struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Mode               uint32 `json:"mode"`
	Device             uint64 `json:"device"`
	Inode              uint64 `json:"inode"`
	ModifiedNanosecond int64  `json:"modified_nanosecond"`
	ChangedNanosecond  int64  `json:"changed_nanosecond"`
}

// Only a fully verified individual fleet enters durable storage. The caller
// still checks every cached fleet against the other candidates and this epoch.
type fleetCensusProof struct {
	Fleet          int                      `json:"fleet"`
	DescriptorHash string                   `json:"descriptor_hash"`
	Files          []fleetCensusFileWitness `json:"files"`
	Uid            uint16                   `json:"uid"`
	Clients        [][16]byte               `json:"clients"`
	Hotkey         string                   `json:"hotkey"`
	ValidFromEpoch uint64                   `json:"valid_from_epoch"`
	ValidToEpoch   uint64                   `json:"valid_to_epoch"`
}

type fleetCensusCacheProof struct {
	Schema          string                   `json:"schema"`
	VerifierVersion string                   `json:"verifier_version"`
	ContextHash     string                   `json:"context_hash"`
	Fleets          map[int]fleetCensusProof `json:"fleets"`
}

type fleetCensusCacheEnvelope struct {
	Proof fleetCensusCacheProof `json:"proof"`
	Mac   string                `json:"hmac_sha256"`
}

// One census invocation owns this object; concurrent snapshots use independent
// objects and atomically publish authenticated successes. Hooks let regression
// tests count real cold reads and force mutation before the final recheck.
type fleetCensusCache struct {
	stateDir      string
	name          string
	key           [32]byte
	proof         fleetCensusCacheProof
	dirty         bool
	usedFleetKVs  map[int]fleetCensusProof
	readFile      func(string) ([]byte, error)
	beforeRecheck func()
}

// Runtime/executable versions do not change a signed fleet's identity. The
// explicit verifier version invalidates proofs when verification rules change.
func newFleetCensusCache(cfg *ResolvedConfig, stateDir string, coordinator common.Address) *fleetCensusCache {
	self := &fleetCensusCache{stateDir: stateDir, readFile: os.ReadFile, usedFleetKVs: map[int]fleetCensusProof{}}
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || cfg.WalletMaterial == "" {
		return self
	}
	contextHash, err := canonicalHashHex(struct {
		DeploymentId  string
		ConfigHash    string
		PolicyHash    string
		ChainId       uint64
		Netuid        uint16
		Coordinator   common.Address
		Topology      TopologyConfig
		MaximumEpochs uint64
	}{DeploymentId: cfg.Config.Deployment.DeploymentID, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash,
		ChainId: cfg.ChainID, Netuid: cfg.Netuid, Coordinator: coordinator, Topology: cfg.Config.Topology,
		MaximumEpochs: cfg.Policy.Binding.MaximumValidityEpochs})
	if err != nil {
		return self
	}
	self.key = derive32(cfg, "fleet-census-cache/v1")
	self.name = strings.TrimPrefix(contextHash, "0x") + ".json"
	self.proof = fleetCensusCacheProof{Schema: fleetCensusCacheSchema, VerifierVersion: fleetCensusVerifierVersion,
		ContextHash: contextHash, Fleets: map[int]fleetCensusProof{}}
	self.load()
	return self
}

// Descriptor selection is authenticated by the caller at the current epoch.
// Rebuild the aggregate on every observation, even when each fleet is cached.
func inspectFleetEvidenceCensus(cfg *ResolvedConfig, coordinator common.Address, epoch uint64, descriptors []fleetLifecycleEvidenceDescriptor, cache *fleetCensusCache) (bool, int, bool, []uint16, []string, [][]int) {
	if cache == nil || cfg == nil || cfg.Config == nil || len(descriptors) != cfg.Config.Topology.fleetCandidates() {
		return false, 0, false, nil, nil, nil
	}
	commitments, historicalBindings, currentBindings, count := true, true, true, 0
	uids := make([]uint16, 0, len(descriptors))
	hotkeys := make([]string, 0, len(descriptors))
	minerGroups := make([][]int, 0, len(descriptors))
	seenUids := map[uint16]bool{}
	seenClients := map[[16]byte]bool{}
	for index, descriptor := range descriptors {
		committed, members, bound, proof, err := cache.inspect(cfg, coordinator, index+1, descriptor)
		if err != nil {
			return false, 0, false, nil, nil, nil
		}
		commitments = commitments && committed
		historicalBindings = historicalBindings && bound
		currentBindings = currentBindings && bound && proof.ValidFromEpoch <= epoch && epoch <= proof.ValidToEpoch
		count += members
		if bound {
			if seenUids[proof.Uid] {
				historicalBindings = false
			}
			seenUids[proof.Uid] = true
			uids = append(uids, proof.Uid)
		}
		for _, client := range proof.Clients {
			if seenClients[client] {
				historicalBindings = false
			}
			seenClients[client] = true
		}
		hotkeys = append(hotkeys, proof.Hotkey)
		minerGroups = append(minerGroups, append([]int(nil), descriptor.MinerIDs...))
	}
	if !cache.revalidate(descriptors) {
		return false, 0, false, nil, nil, nil
	}
	cache.save()
	if !historicalBindings || !currentBindings || len(uids) != len(descriptors) {
		return commitments, count, false, uids, nil, nil
	}
	return commitments, count, true, uids, hotkeys, minerGroups
}

// Fixed descriptor names are the entire per-fleet source census, in stable
// order. No directory enumeration can silently omit a configured member.
func fleetCensusSourceNames(descriptor fleetLifecycleEvidenceDescriptor) []string {
	names := []string{descriptor.ManifestName, descriptor.CommitmentName}
	return append(names, descriptor.BindingNames...)
}

// Public source files may be readable by others, but writable aliases and
// non-regular files never qualify for reuse. Failure simply forces a cold read.
func (self *fleetCensusCache) witnesses(descriptor fleetLifecycleEvidenceDescriptor) ([]fleetCensusFileWitness, bool) {
	files := make([]fleetCensusFileWitness, 0, len(descriptor.BindingNames)+2)
	for _, name := range fleetCensusSourceNames(descriptor) {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return nil, false
		}
		info, err := os.Lstat(filepath.Join(self.stateDir, "public", name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 {
			return nil, false
		}
		device, inode, uid, changed, err := evidenceRelayStartupFileIdentity(info)
		if err != nil || uid != uint32(os.Geteuid()) {
			return nil, false
		}
		files = append(files, fleetCensusFileWitness{Name: name, Size: info.Size(), Mode: uint32(info.Mode()),
			Device: device, Inode: inode, ModifiedNanosecond: info.ModTime().UnixNano(), ChangedNanosecond: changed})
	}
	return files, true
}

// Exact metadata is compared element by element without allocation or hashing.
func equalFleetCensusWitnesses(left, right []fleetCensusFileWitness) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// Warm results never skip current-window or census checks. Only the individual
// immutable signature verification and its unchanged local bytes are reused.
func (self *fleetCensusCache) inspect(cfg *ResolvedConfig, coordinator common.Address, fleet int, descriptor fleetLifecycleEvidenceDescriptor) (bool, int, bool, fleetCensusProof, error) {
	descriptorHash, err := canonicalHashHex(descriptor)
	if err != nil {
		return false, 0, false, fleetCensusProof{}, err
	}
	files, witnessed := self.witnesses(descriptor)
	if cached, found := self.proof.Fleets[fleet]; found && witnessed && cached.Fleet == fleet && cached.DescriptorHash == descriptorHash &&
		len(cached.Clients) == cfg.Config.Topology.ClientsPerHeadFleet && cached.ValidFromEpoch != 0 && cached.ValidToEpoch >= cached.ValidFromEpoch &&
		equalFleetCensusWitnesses(cached.Files, files) {
		self.usedFleetKVs[fleet] = cached
		return true, len(cached.Clients), true, cached, nil
	}
	if _, found := self.proof.Fleets[fleet]; found {
		delete(self.proof.Fleets, fleet)
		self.dirty = true
	}
	setup := map[string]json.RawMessage{}
	for index, name := range fleetCensusSourceNames(descriptor) {
		raw, err := self.readFile(filepath.Join(self.stateDir, "public", name))
		if err != nil {
			return false, 0, false, fleetCensusProof{}, err
		}
		key := fmt.Sprintf("fleet_%d_binding_%d", fleet, index-1)
		if index == 0 {
			key = fmt.Sprintf("fleet_%d_manifest", fleet)
		} else if index == 1 {
			key = fmt.Sprintf("fleet_%d_commitment", fleet)
		}
		setup[key] = raw
	}
	commitments, count, bindings, uid, clients := inspectOneFleetEvidenceBytes(cfg, setup, coordinator, fleet)
	result := fleetCensusProof{Fleet: fleet, DescriptorHash: descriptorHash, Files: files, Uid: uid, Clients: clients}
	if !bindings {
		return commitments, count, false, result, nil
	}
	manifest, err := protocol.ParseFleetManifest(setup[fmt.Sprintf("fleet_%d_manifest", fleet)])
	if err != nil {
		return commitments, count, false, result, err
	}
	result.Hotkey = fleetLifecycleHex(manifest.Hotkey)
	result.ValidToEpoch = ^uint64(0)
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		var binding FleetBindingEvidence
		if err := json.Unmarshal(setup[fmt.Sprintf("fleet_%d_binding_%d", fleet, member)], &binding); err != nil {
			return commitments, count, false, result, err
		}
		result.ValidFromEpoch = max(result.ValidFromEpoch, binding.ValidFromEpoch)
		result.ValidToEpoch = min(result.ValidToEpoch, binding.ValidToEpoch)
	}
	sort.Slice(result.Clients, func(i, j int) bool { return bytes.Compare(result.Clients[i][:], result.Clients[j][:]) < 0 })
	if witnessed {
		current, ok := self.witnesses(descriptor)
		if !ok || !equalFleetCensusWitnesses(files, current) {
			return false, 0, false, result, errors.New("fleet evidence changed during verification")
		}
		self.usedFleetKVs[fleet] = result
		if self.proof.Fleets != nil && result.ValidToEpoch >= result.ValidFromEpoch {
			self.proof.Fleets[fleet] = result
			self.dirty = true
		}
	}
	return commitments, count, bindings, result, nil
}

// A later fleet's read cannot hide replacement of an earlier cached source.
// Recheck before returning the observation and before publishing any successes.
func (self *fleetCensusCache) revalidate(descriptors []fleetLifecycleEvidenceDescriptor) bool {
	if self.beforeRecheck != nil {
		self.beforeRecheck()
	}
	for fleet, proof := range self.usedFleetKVs {
		if fleet < 1 || fleet > len(descriptors) {
			return false
		}
		files, ok := self.witnesses(descriptors[fleet-1])
		if !ok || !equalFleetCensusWitnesses(files, proof.Files) {
			return false
		}
	}
	return true
}

// The authentication key derives from private deployment custody and never
// appears in the cache. The verifier version is covered by the tag as well.
func (self *fleetCensusCache) authenticationTag(proof fleetCensusCacheProof) []byte {
	wire, _ := json.Marshal(proof)
	mac := hmac.New(sha256.New, self.key[:])
	mac.Write([]byte(fleetCensusCacheSchema + "\x00"))
	mac.Write(wire)
	return mac.Sum(nil)
}

// Corruption, unsafe permissions, unsupported versions and key changes are
// misses. An optimization never becomes an additional prerequisite to run.
func (self *fleetCensusCache) load() {
	if self.name == "" {
		return
	}
	directory, err := openHistoricalAuditNamedCacheDirectory(self.stateDir, fleetCensusCacheDirectoryName, false)
	if err != nil {
		return
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), self.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	file := os.NewFile(uintptr(fd), self.name)
	defer file.Close()
	if requireHistoricalAuditPrivateMode(fd, unix.S_IFREG, 0o600) != nil {
		return
	}
	wire, err := io.ReadAll(io.LimitReader(file, fleetCensusCacheMaximumBytes+1))
	if err != nil || len(wire) > fleetCensusCacheMaximumBytes || rejectDuplicatePostconditionJSONFields(wire) != nil {
		return
	}
	var envelope fleetCensusCacheEnvelope
	if decodeStrictJSONBytes(wire, &envelope) != nil || envelope.Proof.Schema != self.proof.Schema || envelope.Proof.VerifierVersion != self.proof.VerifierVersion ||
		envelope.Proof.ContextHash != self.proof.ContextHash || envelope.Proof.Fleets == nil {
		return
	}
	tag, err := hex.DecodeString(envelope.Mac)
	if err != nil || !hmac.Equal(tag, self.authenticationTag(envelope.Proof)) {
		return
	}
	self.proof = envelope.Proof
}

// Descriptor-relative creation and rename prevent substituted cache paths
// from redirecting a write; concurrent writers can only lose an optimization.
func (self *fleetCensusCache) save() {
	if !self.dirty || self.name == "" {
		return
	}
	envelope := fleetCensusCacheEnvelope{Proof: self.proof, Mac: hex.EncodeToString(self.authenticationTag(self.proof))}
	wire, err := json.Marshal(envelope)
	if err != nil || len(wire) >= fleetCensusCacheMaximumBytes {
		return
	}
	directory, err := openHistoricalAuditNamedCacheDirectory(self.stateDir, fleetCensusCacheDirectoryName, true)
	if err != nil {
		return
	}
	defer directory.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return
	}
	temporary := ".tmp-" + hex.EncodeToString(random[:])
	directoryFd := int(directory.Fd())
	fd, err := unix.Openat(directoryFd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return
	}
	defer unix.Unlinkat(directoryFd, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	if file.Chmod(0o600) != nil {
		return
	}
	if _, err := file.Write(append(wire, '\n')); err != nil || file.Sync() != nil || file.Close() != nil {
		return
	}
	if unix.Renameat(directoryFd, temporary, directoryFd, self.name) == nil {
		_ = directory.Sync()
	}
}
