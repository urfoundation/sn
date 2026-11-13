// Synthetic taskworker resources, signed transactions and a real local HTTP
// server exercise the production adapter without a system service or live key.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/server/v2026/controller"
	"github.com/urnetwork/server/v2026/strecovery"
)

// The real collector gets a complete original database image at each read.
// Only the network transport is replaced; validation and archive replay run.
type repairOperatorTestReader struct {
	images map[string]*strecovery.DatabaseSnapshot
	err    error
	calls  int
	before func(context.Context) error
}

// A test read owns a copied image so later fixture mutations cannot edit a
// previously retained observation through shared slice or pointer storage.
func (self *repairOperatorTestReader) Snapshot(ctx context.Context, source strecovery.DatabaseSource, limits strecovery.Limits) (*strecovery.DatabaseSnapshot, error) {
	self.calls++
	if err := errors.Join(ctx.Err(), self.err); err != nil {
		return nil, err
	}
	if self.before != nil {
		if err := self.before(ctx); err != nil {
			return nil, err
		}
	}
	value := self.images[source.Id]
	if value == nil {
		return nil, errors.New("missing synthetic original source")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result strecovery.DatabaseSnapshot
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// The existing validator fixture supplies only physical test transport and
// volume facts. Its signature and protocol source never enter operator authority.
type repairOperatorFixture struct {
	t               *testing.T
	base            *repairValidatorFixture
	envelope        *repairOperatorEnvelope
	reader          *repairOperatorTestReader
	archive         *strecovery.Archive
	private         ed25519.PrivateKey
	key             string
	originalPrivate ed25519.PrivateKey
	originalKey     string
	approvalPath    string
	status          atomic.Value
	statusConfig    atomic.Value
	http            *httptest.Server
}

// Both source and incident signatures use deterministic throwaway test seeds.
func newRepairOperatorFixture(t *testing.T) *repairOperatorFixture {
	t.Helper()
	base := newRepairValidatorFixture(t)
	f := &repairOperatorFixture{t: t, base: base, private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x81}, 32)), originalPrivate: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x82}, 32)), approvalPath: filepath.Join(base.directory, "operator-incident.json")}
	f.status.Store("ok")
	f.key = "0x" + hex.EncodeToString(f.private.Public().(ed25519.PublicKey))
	f.originalKey = "0x" + hex.EncodeToString(f.originalPrivate.Public().(ed25519.PublicKey))
	f.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Error("operator used a non-status route", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		status := f.statusConfig.Load().(repairOperatorStatus)
		status.Status = f.status.Load().(string)
		_ = json.NewEncoder(w).Encode(status)
	}))
	t.Cleanup(f.http.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(f.http.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	p := repairOperatorHostPlan{Role: "synthetic", MachineId: base.approval.Plan.MachineId, BootId: base.approval.Plan.BootId, Unit: planFileReference{Path: filepath.Join(base.directory, "sn-mainnet-operator-synthetic.service")}, Binary: planFileReference{Path: filepath.Join(base.directory, "taskworker")}, Inspector: base.approval.Plan.Unit.Binary, Systemctl: base.approval.Plan.Systemctl, StateDirectory: base.approval.Plan.Unit.StateDirectory, Uid: base.approval.Plan.Unit.Uid, Gid: base.approval.Plan.Unit.Gid, Port: uint16(portNumber), Count: 8, BatchSize: 4, DurableVolumes: *base.approval.Plan.Unit.DurableVolumes, RequiredMounts: base.approval.Plan.RequiredMounts, ActivatedAt: base.now.Add(-time.Hour), Generation: base.approval.Plan.Previous, ExclusiveHostControl: true}
	storageTransport := base.host.storageCommand
	base.host.storageCommand = func(ctx context.Context, command *exec.Cmd) error {
		if command.Path != "/proc/self/fd/3" || len(command.Args) == 0 || command.Args[0] != p.Inspector.Path || len(command.ExtraFiles) != 1 || command.ExtraFiles[0].Name() != p.Inspector.Path {
			return errors.New("operator storage inspection did not execute the separately pinned SN inspector")
		}
		return storageTransport(ctx, command)
	}
	p.Binary.Sha256 = monitorReadDigest([]byte("synthetic actual taskworker release\n"))
	repairValidatorTestWrite(t, p.Binary.Path, []byte("synthetic actual taskworker release\n"), 0755)
	warp := filepath.Join(base.directory, "warp")
	p.Environment = []repairOperatorEnvironment{{"LANG", "C"}, {"URNETWORK_ST_PROFILE", "mainnet"}, {"WARP_BLOCK", "synthetic"}, {"WARP_CONFIG_HOME", filepath.Join(warp, "config")}, {"WARP_CONFIG_VERSION", "1.0.0"}, {"WARP_ENV", "main"}, {"WARP_HOME", warp}, {"WARP_HOST", "synthetic-host"}, {"WARP_SERVICE", "taskworker"}, {"WARP_SITE_HOME", filepath.Join(warp, "site")}, {"WARP_VAULT_HOME", filepath.Join(warp, "vault")}, {"WARP_VERSION", "1.0.0"}}
	for _, name := range []string{"config", "site", "vault"} {
		path := filepath.Join(warp, name)
		if err := os.MkdirAll(filepath.Join(path, "main", "1.0.0"), 0700); err != nil {
			t.Fatal(err)
		}
		repairValidatorTestWrite(t, filepath.Join(path, "main", "1.0.0", "settings.yml"), []byte("all: {}\n"), 0600)
		p.Resources = append(p.Resources, repairOperatorTree{Path: path, MaximumFiles: 128, MaximumBytes: 1024 * 1024})
	}
	slices.SortFunc(p.Resources, func(a, b repairOperatorTree) int { return strings.Compare(a.Path, b.Path) })
	var state syscall.Stat_t
	if err := syscall.Stat(p.StateDirectory, &state); err != nil {
		t.Fatal(err)
	}
	p.WritableRoots = []repairOperatorRoot{{Path: p.StateDirectory, Device: uint64(state.Dev), Inode: state.Ino, Blob: true}}
	p.JournalPaths = []string{filepath.Join(p.StateDirectory, "retained-journal.json")}
	for path, raw := range map[string][]byte{p.JournalPaths[0]: []byte("{\"uncertain\":true,\"original_nonce\":0}\n"), filepath.Join(p.StateDirectory, ".capacity-owner.partial"): nil} {
		repairValidatorTestWrite(t, path, raw, 0600)
		if os.Geteuid() == 0 {
			if err := os.Chown(path, int(p.Uid), int(p.Gid)); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.reader, f.archive = repairOperatorSignedCensus(t, base.directory, base.now)
	p.Census = f.archive.Selection
	stRaw := []byte("profile: mainnet\nenabled: true\nrpc_urls: [http://127.0.0.1:9944]\nchain_id: 964\ngenesis_hash: 0x" + strings.Repeat("a", 64) + "\ndeployment_id: synthetic\npolicy_hash: 0x" + strings.Repeat("c", 64) + "\ncoordinator_address: 0x" + strings.Repeat("b", 40) + "\nsettlement_vault_address: 0x" + strings.Repeat("c", 40) + "\nreserve_sink_address: 0x" + strings.Repeat("d", 40) + "\nnetuid: 7\nno_id: 1\ndeploy_block: 1\ndeposit_hotkey: 0x" + strings.Repeat("4", 64) + "\ndeposit_key: '" + strings.Repeat("1", 64) + "'\nroot_key: '" + strings.Repeat("2", 64) + "'\nartifact_key: '" + strings.Repeat("3", 64) + "'\ndeposit_rate_numerator_rao_per_gib: 1\ndeposit_rate_denominator: 1\ndeposit_epoch_cap_rao: 1000\n")
	p.Source, err = repairOperatorPublicSource(stRaw, p)
	if err != nil {
		t.Fatal(err)
	}
	p.Source.Databases = []repairOperatorDatabaseBinding{{Resource: "pg.yml", Source: "operator-one"}, {Resource: "pg_maintenance.yml", Source: "operator-one"}}
	for name, raw := range map[string][]byte{"st.yml": stRaw, "pg.yml": []byte("authority: 127.0.0.1:5432\ndb: operator-one\nuser: writer\npassword: synthetic\n"), "redis.yml": []byte("authority: 127.0.0.1:6379\npassword: synthetic\ncluster: false\n"), "minio.yml": []byte(fmt.Sprintf("authority: local\npath: %s\nmax_bytes: 1073741824\ndurable_volumes:\n  path: %s\n  sha256: %s\n", p.StateDirectory, p.DurableVolumes.Path, p.DurableVolumes.Sha256))} {
		repairValidatorTestWrite(t, filepath.Join(p.env("WARP_VAULT_HOME"), "main", "1.0.0", name), raw, 0600)
	}
	for _, name := range []string{"db.yml", "redis.yml"} {
		repairValidatorTestWrite(t, filepath.Join(p.env("WARP_CONFIG_HOME"), "main", "1.0.0", name), []byte("min_connections: 0\nmax_connections: 2\n"), 0600)
	}
	repairOperatorTestServiceAccess(t, base.directory, p)
	for index := range p.Resources {
		hash, err := inspectRepairOperatorTree(t.Context(), base.host, p.Resources[index], p)
		if err != nil {
			t.Fatal(err)
		}
		p.Resources[index].Sha256 = hash
	}
	p.Unit.Sha256 = monitorReadDigest(p.render())
	repairValidatorTestWrite(t, p.Unit.Path, p.render(), 0644)
	originalPath := filepath.Join(base.directory, "operator-original-host.json")
	archivePath := filepath.Join(base.directory, "operator-original-census.json")
	archiveRaw, err := json.Marshal(f.archive)
	if err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, archivePath, archiveRaw, 0600)
	incident := repairOperatorPlan{OriginalApproval: planFileReference{Path: originalPath}, OriginalPublicKey: f.originalKey, OriginalCensus: planFileReference{Path: archivePath, Sha256: monitorReadDigest(archiveRaw)}, Previous: p.Generation, IncidentAt: base.now, StatePath: filepath.Join(base.directory, "operator-repair.json"), ValidFrom: base.now, ExpiresAt: base.now.Add(time.Hour), MaximumStarts: 1, MaximumObservations: 16, MaximumSampleAgeSeconds: 120}
	for _, path := range []string{filepath.Join(p.StateDirectory, ".capacity-owner.partial"), p.JournalPaths[0]} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var state syscall.Stat_t
		if err := syscall.Stat(path, &state); err != nil {
			t.Fatal(err)
		}
		incident.OriginalFiles = append(incident.OriginalFiles, repairOperatorRetainedFile{File: planFileReference{Path: path, Sha256: monitorReadDigest(raw)}, Device: uint64(state.Dev), Inode: state.Ino, Quota: filepath.Base(path) == ".capacity-owner.partial"})
	}
	prepareMainnetSnapshotTest(t, incident.StatePath, "mainnet-host-action", 64*1024)
	f.envelope = &repairOperatorEnvelope{approval: repairOperatorApproval{Schema: repairOperatorSchema, Plan: incident}, original: repairOperatorHostApproval{Schema: repairOperatorHostSchema, Plan: p}, reader: f.reader, procRoot: filepath.Join(base.directory, "proc")}
	version, config := p.env("WARP_VERSION"), p.env("WARP_CONFIG_VERSION")
	f.statusConfig.Store(repairOperatorStatus{Version: &version, ConfigVersion: &config, ClientAddress: "127.0.0.1", Host: p.env("WARP_HOST"), Service: "taskworker", Block: p.env("WARP_BLOCK")})
	f.signHost()
	f.sign()
	base.writeProgress = false
	base.approval.Plan.Unit.Name = p.unitName()
	for key, value := range map[string]string{"Id": p.unitName(), "FragmentPath": p.Unit.Path, "Environment": p.environment(), "ControlGroup": "/system.slice/" + p.unitName(), "ExecStart": "{ path=" + p.Binary.Path + " ; argv[]=" + p.Binary.Path + " " + p.arguments() + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=71 ; code=exited ; status=0 }"} {
		base.manager[key] = value
	}
	group := filepath.Join(base.host.cgroupRoot, "system.slice", p.unitName())
	if err := os.MkdirAll(group, 0700); err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, filepath.Join(group, "cgroup.events"), []byte("populated 0\nfrozen 0\n"), 0644)
	repairValidatorTestWrite(t, filepath.Join(group, "cgroup.procs"), nil, 0644)
	f.process()
	return f
}

// The archive includes same-nonce replacements, cancellation and a terminal
// signature. A status-filtered source therefore cannot satisfy this fixture.
func repairOperatorSignedCensus(t *testing.T, directory string, now time.Time) (*repairOperatorTestReader, *strecovery.Archive) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	rootKey, err := crypto.HexToECDSA(strings.Repeat("2", 64))
	if err != nil {
		t.Fatal(err)
	}
	address := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	rootAddress := strings.ToLower(crypto.PubkeyToAddress(rootKey.PublicKey).Hex())
	selection := strecovery.Config{Schema: strecovery.ConfigSchema, ChainId: mainnetEvmChainId, Genesis: "0x" + strings.Repeat("a", 64), Roles: []strecovery.Role{{Id: "deposit", Address: address, FirstNonce: 0, NextNonce: 2}, {Id: "root", Address: rootAddress, FirstNonce: 0, NextNonce: 0}}, Limits: strecovery.Limits{MaximumIntents: 128, MaximumAttempts: 128, MaximumTransactionBytes: 128 * 1024, MaximumTotalBytes: 1024 * 1024}}
	reader := &repairOperatorTestReader{images: map[string]*strecovery.DatabaseSnapshot{}}
	for _, name := range []string{"operator-one", "operator-two"} {
		path := filepath.Join(directory, name+"-db.txt")
		raw := []byte("postgres://observer:synthetic@127.0.0.1:5432/" + name + "?sslmode=disable\n")
		repairValidatorTestWrite(t, path, raw, 0600)
		selection.Databases = append(selection.Databases, strecovery.DatabaseSource{Id: name, Connection: strecovery.FileReference{Path: path, Sha256: monitorReadDigest(raw)}, Roles: []string{"deposit", "root"}})
		reader.images[name] = &strecovery.DatabaseSnapshot{Intents: []strecovery.Intent{}, Attempts: []strecovery.Attempt{}}
	}
	store := filepath.Join(directory, "original-signed-transactions")
	if err := os.Mkdir(store, 0700); err != nil {
		t.Fatal(err)
	}
	selection.Stores = []strecovery.StoreSource{{Id: "original-store", Directory: store, Roles: []string{"deposit", "root"}}}
	image := reader.images["operator-one"]
	to := common.HexToAddress("0x" + strings.Repeat("b", 40))
	for nonce := uint64(0); nonce < 2; nonce++ {
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", nonce+1)
		intent := strecovery.Intent{Id: id, IntentKey: fmt.Sprintf("original/%d", nonce), LogicalKey: fmt.Sprintf("original/%d", nonce), Generation: 0, Profile: "mainnet", DeploymentId: "synthetic", DeploymentKey: "964:" + strings.ToLower(to.Hex()), ChainId: int64(mainnetEvmChainId), Genesis: selection.Genesis, From: address, To: strings.ToLower(to.Hex()), CalldataHash: crypto.Keccak256Hash(nil).Hex(), Nonce: int64(nonce), Status: "uncertain", CreateTime: now.Add(-time.Hour), UpdateTime: now.Add(-time.Minute)}
		count := 3
		if nonce == 1 {
			count = 1
			intent.Status = "finalized"
		}
		for number := 1; number <= count; number++ {
			recipient, kind := to, "execution"
			if number == 3 {
				recipient = crypto.PubkeyToAddress(key.PublicKey)
				kind = "cancellation"
			}
			tx, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: nonce, To: &recipient, Gas: 21000, GasPrice: big.NewInt(int64(number + 1)), Value: new(big.Int)}), types.LatestSignerForChainID(new(big.Int).SetUint64(mainnetEvmChainId)), key)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := tx.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			price := tx.GasPrice().String()
			hash := tx.Hash().Hex()
			attempt := strecovery.Attempt{IntentId: id, Number: number, Kind: kind, Hash: hash, Raw: raw, GasLimit: 21000, GasPrice: &price, Status: "uncertain", CreateTime: now.Add(-time.Hour), UpdateTime: now.Add(-time.Minute)}
			if number == 1 && nonce == 0 {
				attempt.Status = "replaced"
			}
			if number == 2 {
				attempt.Status = "failed"
			}
			if nonce == 1 {
				attempt.Status = "finalized"
			}
			image.Attempts = append(image.Attempts, attempt)
			intent.CurrentHash = &hash
			intent.AttemptCount = number
			repairValidatorTestWrite(t, filepath.Join(store, strings.TrimPrefix(hash, "0x")+".rlp"), raw, 0600)
		}
		image.Intents = append(image.Intents, intent)
	}
	archive, err := strecovery.Collect(t.Context(), selection, reader)
	if err != nil {
		t.Fatal(err)
	}
	return reader, archive
}

// Host changes require their own explicit original authority before an incident
// may refer to them; no production command exposes this synthetic signer.
func (self *repairOperatorFixture) signHost() {
	self.t.Helper()
	p := &self.envelope.original.Plan
	p.Unit.Sha256 = monitorReadDigest(p.render())
	repairValidatorTestWrite(self.t, p.Unit.Path, p.render(), 0644)
	raw, err := self.envelope.original.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.envelope.original.Signature = hex.EncodeToString(ed25519.Sign(self.originalPrivate, raw))
	if err := self.envelope.original.validate(self.originalKey); err != nil {
		self.t.Fatal(err)
	}
	raw, err = json.Marshal(self.envelope.original)
	if err != nil {
		self.t.Fatal(err)
	}
	self.envelope.approval.Plan.OriginalApproval.Sha256 = monitorReadDigest(raw)
	repairValidatorTestWrite(self.t, self.envelope.approval.Plan.OriginalApproval.Path, raw, 0600)
}

// Re-signing a test incident cannot renew its permanent generation marker.
func (self *repairOperatorFixture) sign() {
	self.t.Helper()
	self.envelope.approval.Plan.IncidentId = self.envelope.approval.Plan.incidentId()
	raw, err := self.envelope.approval.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.envelope.approval.Signature = hex.EncodeToString(ed25519.Sign(self.private, raw))
	if err := self.envelope.validate(self.key); err != nil {
		self.t.Fatal(err)
	}
	raw, err = json.Marshal(self.envelope.approval)
	if err != nil {
		self.t.Fatal(err)
	}
	repairValidatorTestWrite(self.t, self.approvalPath, raw, 0600)
}

// The fixture supplies kernel observation files while the status transport is
// a real HTTP server. The production path always selects the actual /proc tree.
func (self *repairOperatorFixture) process() {
	self.t.Helper()
	p := self.envelope.original.Plan
	root := filepath.Join(self.envelope.procRoot, "72")
	for _, name := range []string{"fd", "net", "ns"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			self.t.Fatal(err)
		}
	}
	fields := []string{"S"}
	for len(fields) < 20 {
		fields = append(fields, "0")
	}
	fields[19] = "200"
	repairValidatorTestWrite(self.t, filepath.Join(root, "status"), []byte(fmt.Sprintf("Uid:\t%d\t%d\t%d\t%d\nGid:\t%d\t%d\t%d\t%d\n", p.Uid, p.Uid, p.Uid, p.Uid, p.Gid, p.Gid, p.Gid, p.Gid)), 0600)
	repairValidatorTestWrite(self.t, filepath.Join(root, "stat"), []byte("72 (taskworker) "+strings.Join(fields, " ")+"\n"), 0600)
	repairValidatorTestWrite(self.t, filepath.Join(root, "cmdline"), []byte(strings.Join(append([]string{p.Binary.Path}, strings.Fields(p.arguments())...), "\x00")+"\x00"), 0600)
	words := []string{"INVOCATION_ID=" + strings.Repeat("7", 32)}
	for _, v := range p.Environment {
		words = append(words, v.Name+"="+v.Value)
	}
	repairValidatorTestWrite(self.t, filepath.Join(root, "environ"), []byte(strings.Join(words, "\x00")+"\x00"), 0600)
	observer := filepath.Join(self.envelope.procRoot, "self", "ns")
	if err := os.MkdirAll(observer, 0700); err != nil {
		self.t.Fatal(err)
	}
	repairValidatorTestWrite(self.t, filepath.Join(observer, "net"), []byte("synthetic shared network namespace"), 0600)
	if err := os.Symlink(filepath.Join(observer, "net"), filepath.Join(root, "ns", "net")); err != nil {
		self.t.Fatal(err)
	}
	repairValidatorTestWrite(self.t, filepath.Join(root, "cgroup"), []byte("0::/system.slice/"+p.unitName()+"\n"), 0600)
	if err := os.Symlink(p.Binary.Path, filepath.Join(root, "exe")); err != nil {
		self.t.Fatal(err)
	}
	if err := os.Symlink("socket:[123456]", filepath.Join(root, "fd", "4")); err != nil {
		self.t.Fatal(err)
	}
	repairValidatorTestWrite(self.t, filepath.Join(root, "net", "tcp"), []byte(fmt.Sprintf("sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n0: 0100007F:%04X 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 123456\n", p.Port)), 0600)
	repairValidatorTestWrite(self.t, filepath.Join(root, "net", "tcp6"), []byte("sl local_address rem_address st\n"), 0600)
}

// The operator journal shares the already admitted physical fixture volume.
func (self *repairOperatorFixture) ctx() context.Context { return self.base.storage.Context }

// Each call reopens the actual original process journal and consumed allowance.
func (self *repairOperatorFixture) resume() (string, bool, error) {
	return self.envelope.resume(self.ctx(), self.key, self.base.host, func() time.Time { return self.base.now })
}

// Claiming preserves every original database and local journal byte.
func (self *repairOperatorFixture) claim() {
	self.t.Helper()
	if err := self.envelope.claim(self.ctx(), self.key, self.base.host, func() time.Time { return self.base.now }); err != nil {
		self.t.Fatal(err)
	}
	if self.base.starts != 0 {
		self.t.Fatal("operator claim started a service")
	}
}

// A real signed census and actual taskworker status are both necessary, while
// the original uncertainty stays in the retained archive after local recovery.
func TestRepairOperatorRetainsOriginalAttemptsAndCompletesActualProcess(t *testing.T) {
	f := newRepairOperatorFixture(t)
	original, err := os.ReadFile(f.envelope.approval.Plan.OriginalCensus.Path)
	if err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, f.envelope.original.Plan.StateDirectory)
	f.claim()
	status, complete, err := f.resume()
	if err != nil || !complete || status != "resumed-generation-observed" || f.base.starts != 1 {
		t.Fatal("operator actual process continuation failed", status, complete, err, f.base.starts)
	}
	f.base.now = f.envelope.approval.Plan.ExpiresAt.Add(time.Hour)
	status, complete, err = f.resume()
	if err != nil || !complete || f.base.starts != 1 {
		t.Fatal("completed operator incident replayed start", status, complete, err, f.base.starts)
	}
	after, err := os.ReadFile(f.envelope.approval.Plan.OriginalCensus.Path)
	if err != nil || !bytes.Equal(original, after) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.envelope.original.Plan.StateDirectory)) {
		t.Fatal("operator repair changed original signatures, quota or journals", err)
	}
	if len(f.archive.Transactions) != 4 || f.archive.Databases[0].Snapshot.Intents[0].Status != "uncertain" {
		t.Fatal("fixture lost original complete census or uncertainty")
	}
}

// An uncertain systemctl acknowledgement permanently consumes one start even
// if the unrelated HTTP response and next process observation look healthy.
func TestRepairOperatorUncertainStartNeverAdoptsOrRetries(t *testing.T) {
	f := newRepairOperatorFixture(t)
	f.claim()
	f.base.startError = errors.Join(context.DeadlineExceeded, syscall.EIO)
	status, complete, err := f.resume()
	if status != "uncertain-consumed-start" || complete || !errors.Is(err, syscall.EIO) || f.base.starts != 1 {
		t.Fatal("original operator uncertain result changed", status, complete, err, f.base.starts)
	}
	f.base.startError = nil
	status, complete, err = f.resume()
	if status != "uncertain-consumed-start" || complete || !errors.Is(err, errRepairProcessHeld) || f.base.starts != 1 {
		t.Fatal("operator uncertainty renewed or inferred a start", status, complete, err, f.base.starts)
	}
}

// A second correctly signed file cannot evade the original physical generation
// marker by choosing a different journal or another incident digest.
func TestRepairOperatorAlternateJournalCannotRenewGeneration(t *testing.T) {
	f := newRepairOperatorFixture(t)
	f.claim()
	f.envelope.approval.Plan.StatePath = filepath.Join(f.base.directory, "operator-another.json")
	prepareMainnetSnapshotTest(t, f.envelope.approval.Plan.StatePath, "mainnet-host-action", 64*1024)
	f.sign()
	if err := f.envelope.claim(f.ctx(), f.key, f.base.host, func() time.Time { return f.base.now }); err == nil || f.base.starts != 0 {
		t.Fatal("operator alternate journal renewed generation", err, f.base.starts)
	}
}

// A pending-only projection omits the terminal intent's original signed bytes.
// The real collector must refuse before any service effect or quota mutation.
func TestRepairOperatorRefusesPendingOnlyAndChangedOriginalCensus(t *testing.T) {
	for _, kind := range []string{"pending-only", "raw-bytes", "status-drift"} {
		f := newRepairOperatorFixture(t)
		f.claim()
		image := f.reader.images["operator-one"]
		switch kind {
		case "pending-only":
			image.Intents = image.Intents[:1]
			image.Attempts = image.Attempts[:3]
		case "raw-bytes":
			image.Attempts[0].Raw[0] ^= 1
		case "status-drift":
			image.Intents[0].Status = "failed"
		}
		_, complete, err := f.resume()
		if err == nil || complete || f.base.starts != 0 {
			t.Fatal("operator changed complete original census acquired start", kind, complete, err, f.base.starts)
		}
	}
}

// Original availability causes survive classification; a later exact observation
// can proceed under the same unconsumed allowance without rewriting authority.
func TestRepairOperatorCensusReadFailureRetainsCauseAndAllowance(t *testing.T) {
	f := newRepairOperatorFixture(t)
	f.claim()
	f.reader.err = errors.Join(context.DeadlineExceeded, syscall.EIO)
	status, complete, err := f.resume()
	if status != "source-refused" || complete || !errors.Is(err, syscall.EIO) || errors.Is(err, errRpcIntegrity) || f.base.starts != 0 {
		t.Fatal("operator unavailable census became a mismatch or effect", status, complete, err, f.base.starts)
	}
	f.reader.err = nil
	status, complete, err = f.resume()
	if err != nil || !complete || f.base.starts != 1 {
		t.Fatal("operator original census did not resume", status, complete, err, f.base.starts)
	}
}

// Complete WARP directory coverage includes alternative versions and settings
// redirects, not merely the currently selected st.yml file.
func TestRepairOperatorWarpClosureRejectsVersionAndEnvironmentDrift(t *testing.T) {
	for _, kind := range []string{"new-version", "changed-settings", "symlink", "environment"} {
		f := newRepairOperatorFixture(t)
		f.claim()
		p := f.envelope.original.Plan
		switch kind {
		case "new-version":
			path := filepath.Join(p.env("WARP_VAULT_HOME"), "main", "9.0.0")
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			repairValidatorTestWrite(t, filepath.Join(path, "st.yml"), []byte("enabled: false\n"), 0600)
		case "changed-settings":
			repairValidatorTestWrite(t, filepath.Join(p.env("WARP_CONFIG_HOME"), "main", "1.0.0", "settings.yml"), []byte("all:\n  env_vars:\n    WARP_VAULT_HOME: /unapproved/vault\n"), 0600)
		case "symlink":
			if err := os.Symlink("/unapproved", filepath.Join(p.env("WARP_SITE_HOME"), "other")); err != nil {
				t.Fatal(err)
			}
		case "environment":
			f.base.manager["Environment"] += " WARP_VAULT_HOME=/unapproved/vault"
		}
		_, complete, err := f.resume()
		if err == nil || complete || f.base.starts != 0 {
			t.Fatal("operator changed actual WARP source acquired start", kind, complete, err, f.base.starts)
		}
	}
}

// A recreated empty quota file is not continuity, even when its bytes match.
func TestRepairOperatorMissingAndReplacedQuotaRemainStopped(t *testing.T) {
	for _, replace := range []bool{false, true} {
		f := newRepairOperatorFixture(t)
		f.claim()
		path := filepath.Join(f.envelope.original.Plan.StateDirectory, ".capacity-owner.partial")
		if err := os.Rename(path, path+"-original"); err != nil {
			t.Fatal(err)
		}
		if replace {
			repairValidatorTestWrite(t, path, nil, 0600)
		}
		_, complete, err := f.resume()
		if err == nil || complete || f.base.starts != 0 {
			t.Fatal("operator quota loss or replacement acquired start", complete, err, f.base.starts)
		}
	}
}

// The service must be genuinely stopped with no descendants in its real
// unified cgroup; a quiet process snapshot or empty direct procs list is weaker.
func TestRepairOperatorRequiresStoppedServiceAndEmptyDescendantCgroup(t *testing.T) {
	for _, kind := range []string{"running", "descendant", "filesystem"} {
		f := newRepairOperatorFixture(t)
		switch kind {
		case "running":
			f.base.manager["MainPID"] = "71"
			f.base.manager["ActiveState"] = "active"
			f.base.manager["SubState"] = "running"
		case "descendant":
			repairValidatorTestWrite(t, filepath.Join(f.base.host.cgroupRoot, "system.slice", f.envelope.original.Plan.unitName(), "cgroup.events"), []byte("populated 1\nfrozen 0\n"), 0644)
		case "filesystem":
			f.base.host.cgroupType = func(string) (int64, error) { return 0, nil }
		}
		if err := f.envelope.ready(f.ctx(), f.base.host, f.base.now); err == nil || f.base.starts != 0 {
			t.Fatal("operator stopped-generation proof was bypassed", kind, err, f.base.starts)
		}
	}
}

// Database rows and an unrelated HTTP 200 cannot prove process readiness.
func TestRepairOperatorStatusRequiresOwnedSocketAndExactProcess(t *testing.T) {
	for _, kind := range []string{"socket", "argv", "environment", "invocation", "namespace", "credentials", "exe", "not-ready"} {
		f := newRepairOperatorFixture(t)
		f.claim()
		root := filepath.Join(f.envelope.procRoot, "72")
		switch kind {
		case "socket":
			if err := os.Remove(filepath.Join(root, "fd", "4")); err != nil {
				t.Fatal(err)
			}
		case "argv":
			repairValidatorTestWrite(t, filepath.Join(root, "cmdline"), []byte("taskworker\x00init-tasks\x00"), 0600)
		case "environment":
			repairValidatorTestWrite(t, filepath.Join(root, "environ"), []byte("WARP_SERVICE=taskworker\x00WARP_ENV=local\x00"), 0600)
		case "invocation":
			raw, err := os.ReadFile(filepath.Join(root, "environ"))
			if err != nil {
				t.Fatal(err)
			}
			repairValidatorTestWrite(t, filepath.Join(root, "environ"), bytes.Replace(raw, []byte("INVOCATION_ID="+strings.Repeat("7", 32)), []byte("INVOCATION_ID="+strings.Repeat("8", 32)), 1), 0600)
		case "credentials":
			repairValidatorTestWrite(t, filepath.Join(root, "status"), []byte("Uid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\n"), 0600)
		case "namespace":
			if err := os.Remove(filepath.Join(root, "ns", "net")); err != nil {
				t.Fatal(err)
			}
			repairValidatorTestWrite(t, filepath.Join(root, "ns", "net"), []byte("unrelated namespace"), 0600)
		case "exe":
			if err := os.Remove(filepath.Join(root, "exe")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(f.envelope.original.Plan.Inspector.Path, filepath.Join(root, "exe")); err != nil {
				t.Fatal(err)
			}
		case "not-ready":
			f.status.Store("error not ready: pg unavailable")
		}
		status, complete, err := f.resume()
		if err == nil || complete || status != "waiting-progress" || f.base.starts != 1 {
			t.Fatal("operator generic status bypassed actual generation postcondition", kind, status, complete, err, f.base.starts)
		}
	}
}

// The original host and validator signatures have no incident authority.
func TestRepairOperatorRequiresDistinctOriginalAndIncidentAuthority(t *testing.T) {
	f := newRepairOperatorFixture(t)
	f.envelope.approval.Signature = f.envelope.original.Signature
	if err := f.envelope.validate(f.originalKey); err == nil {
		t.Fatal("original host declaration acquired repair authority")
	}
	f.envelope.approval.Signature = f.base.approval.Signature
	if err := f.envelope.validate(f.base.publicKey); err == nil {
		t.Fatal("validator signature acquired operator authority")
	}
}

// Continued accounting retains original bytes while allowing genuine status
// reconciliation and appended signatures under the unchanged application owner.
func TestRepairOperatorContinuedCensusPreservesEveryOriginalAttempt(t *testing.T) {
	f := newRepairOperatorFixture(t)
	copyArchive := func() *strecovery.Archive {
		raw, err := json.Marshal(f.archive)
		if err != nil {
			t.Fatal(err)
		}
		var v strecovery.Archive
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return &v
	}
	current := copyArchive()
	current.Databases[0].Snapshot.Intents[0].Status = "canceled"
	current.Databases[0].Snapshot.Attempts[2].Status = "canceled"
	if err := retainRepairOperatorCensus(f.archive, current); err != nil {
		t.Fatal("status reconciliation changed immutable custody", err)
	}
	for _, kind := range []string{"terminal", "replacement", "fees", "original-bytes", "store", "nonce-floor", "nonce-ceiling"} {
		v := copyArchive()
		switch kind {
		case "terminal":
			v.Databases[0].Snapshot.Intents = v.Databases[0].Snapshot.Intents[:1]
		case "replacement":
			v.Databases[0].Snapshot.Attempts = v.Databases[0].Snapshot.Attempts[1:]
		case "fees":
			v.Databases[0].Snapshot.Attempts[0].GasLimit++
		case "original-bytes":
			v.Databases[0].Snapshot.Attempts[0].Raw[0] ^= 1
		case "store":
			v.Stores[0].Files = v.Stores[0].Files[1:]
		case "nonce-floor":
			v.Selection.Roles[0].FirstNonce++
		case "nonce-ceiling":
			v.Selection.Roles[0].NextNonce--
		}
		if err := retainRepairOperatorCensus(f.archive, v); err == nil {
			t.Fatal("continued operator census discarded original custody", kind)
		}
	}
}

// The public controller consumes the exact independent manifest and signed
// operator envelope, retaining both intents before the real adapter's effect.
func TestRepairOperatorPublicControllerOwnsDurablePendingAndCompletion(t *testing.T) {
	f := newRepairOperatorFixture(t)
	resource := filepath.Join(f.envelope.original.Plan.env("WARP_VAULT_HOME"), "main", "1.0.0", "provider_work_session.json")
	repairValidatorTestWrite(t, resource, repairOperatorSessionTestSource(t, f.base.now, "valid"), 0600)
	repairOperatorSessionTestSign(t, f)
	f.base.host.operator = &repairOperatorTransports{reader: f.reader, procRoot: f.envelope.procRoot}
	raw, err := os.ReadFile(f.approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := repairControllerManifest{Schema: repairControllerSchema, MaximumParallel: 2, Entries: []repairControllerEntry{{Id: "synthetic-operator-incident", Kind: "operator", Approval: planFileReference{Path: f.approvalPath, Sha256: monitorReadDigest(raw)}, PublicKey: f.key}}}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(f.base.directory, "operator-controller-manifest.json")
	repairValidatorTestWrite(t, manifestPath, manifestRaw, 0600)
	checkpoint := filepath.Join(f.base.directory, "operator-controller.json")
	prepareMainnetSnapshotTest(t, checkpoint, "mainnet-host-action", 64*1024)
	args := []string{"--manifest", manifestPath, "--manifest-sha256", monitorReadDigest(manifestRaw), "--checkpoint", checkpoint, "--metrics-file", filepath.Join(f.base.directory, "operator-controller.prom")}
	execute := f.base.host.execute
	intents := false
	f.base.host.execute = func(ctx context.Context, path string, arguments []string) ([]byte, error) {
		if strings.Contains(strings.Join(arguments, " "), " start -- ") {
			controllerRaw, controllerErr := os.ReadFile(checkpoint)
			processRaw, processErr := os.ReadFile(f.envelope.approval.Plan.StatePath)
			var controller repairControllerRecord
			var process repairProcessRecord
			if err := errors.Join(controllerErr, processErr, json.Unmarshal(controllerRaw, &controller), json.Unmarshal(processRaw, &process)); err != nil {
				return nil, err
			}
			if len(controller.Entries) != 1 || !controller.Entries[0].Attempted || controller.Entries[0].Disposition != "claim-intent-durable" || process.StartAt.IsZero() || process.Status != "start-consumed" || process.Generation != nil {
				return nil, errors.New("operator service effect preceded both durable original intents")
			}
			intents = true
		}
		return execute(ctx, path, arguments)
	}
	run := func() repairControllerRecord {
		t.Helper()
		var out, diagnostic bytes.Buffer
		if code := runRepairControllerCommandWithHost(f.ctx(), args, &out, &diagnostic, func() time.Time { return f.base.now }, f.base.host); code != 0 {
			t.Fatal("public operator controller refused original envelope", code, diagnostic.String())
		}
		var record repairControllerRecord
		if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.validate(manifest, args[3]) != nil {
			t.Fatal("public operator controller lost original record", err, record)
		}
		return record
	}
	f.status.Store("error not ready: retained queue unavailable")
	first := run()
	if !intents || !first.Entries[0].Attempted || first.Entries[0].Status != "pending" || first.Entries[0].Disposition != "waiting-progress" || f.base.starts != 1 {
		t.Fatal("operator public dispatch bypassed original pending custody", intents, first, f.base.starts)
	}
	f.status.Store("ok")
	second := run()
	if second.Entries[0].Status != "completed" || second.Entries[0].Disposition != "resumed-generation-observed" || f.base.starts != 1 {
		t.Fatal("operator public dispatch did not complete the actual original process", second, f.base.starts)
	}
	third := run()
	if third.Entries[0].Status != "completed" || f.base.starts != 1 {
		t.Fatal("operator public controller repeated consumed start", third, f.base.starts)
	}
	metrics, err := os.ReadFile(args[7])
	if err != nil || !bytes.Contains(metrics, []byte("sn_mainnet_repair_controller_completed 1\n")) || bytes.Contains(metrics, []byte(f.key)) {
		t.Fatal("public operator completion metrics differ or leak authority", err, string(metrics))
	}
}

// The public loader reauthenticates original host files; its production reader
// type remains the real complete PostgreSQL census when no test seam exists.
func TestRepairOperatorPublicLoaderRetainsActualSourceAdapter(t *testing.T) {
	f := newRepairOperatorFixture(t)
	raw, err := os.ReadFile(f.approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadRepairOperatorEnvelope(f.ctx(), raw, f.key, f.base.host)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.reader.(strecovery.PostgresReader); !ok || loaded.procRoot != "/proc" {
		t.Fatal("public operator loader selected a generic or pending-only source")
	}
	original, err := os.ReadFile(f.envelope.approval.Plan.OriginalApproval.Path)
	if err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, f.envelope.approval.Plan.OriginalApproval.Path, append(original, '\n'), 0600)
	if _, err := loadRepairOperatorEnvelope(f.ctx(), raw, f.key, f.base.host); !errors.Is(err, errRpcIntegrity) {
		t.Fatal("public operator loader accepted changed original host source", err)
	}
}

// Even a signed full tree census must join the actual selected operator source.
// These fixtures preserve valid signatures and all physical tree checks so the
// public ST/database/blob adapter assertions remain causally necessary.
func TestRepairOperatorActualSelectedSourcesRejectSignedOmissions(t *testing.T) {
	for _, kind := range []string{"disabled-st", "different-signer", "missing-st", "foreign-db", "maintenance-db", "db-route", "db-url", "undeclared-blob"} {
		f := newRepairOperatorFixture(t)
		p := &f.envelope.original.Plan
		root := filepath.Join(p.env("WARP_VAULT_HOME"), "main", "1.0.0")
		path := filepath.Join(root, "st.yml")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "disabled-st":
			repairValidatorTestWrite(t, path, bytes.Replace(raw, []byte("enabled: true"), []byte("enabled: false"), 1), 0600)
		case "different-signer":
			repairValidatorTestWrite(t, path, bytes.Replace(raw, []byte("deposit_key: '"+strings.Repeat("1", 64)), []byte("deposit_key: '"+strings.Repeat("5", 64)), 1), 0600)
		case "missing-st":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "foreign-db":
			repairValidatorTestWrite(t, filepath.Join(root, "pg.yml"), []byte("authority: 127.0.0.1:5432\ndb: different-original\nuser: writer\npassword: synthetic\n"), 0600)
		case "maintenance-db":
			repairValidatorTestWrite(t, filepath.Join(root, "pg_maintenance.yml"), []byte("authority: 127.0.0.1:15432\ndb: operator-one\nuser: writer\npassword: synthetic\n"), 0600)
		case "db-route":
			repairValidatorTestWrite(t, filepath.Join(root, "pg.yml"), []byte("authority: unrelated-runtime:5432\ndb: operator-one\nuser: writer\npassword: synthetic\n"), 0600)
			repairValidatorTestWrite(t, filepath.Join(p.env("WARP_CONFIG_HOME"), "main", "1.0.0", "settings.yml"), []byte("all:\n  routes:\n    unrelated-runtime: 127.0.0.1\n"), 0600)
		case "db-url":
			repairValidatorTestWrite(t, filepath.Join(root, "pg.yml"), []byte("authority: 127.0.0.1:5432\ndb: operator-one\nuser: writer\npassword: 'secret@foreign:5432/other?host=foreign&discard='\n"), 0600)
		case "undeclared-blob":
			raw, err := os.ReadFile(filepath.Join(root, "minio.yml"))
			if err != nil {
				t.Fatal(err)
			}
			repairValidatorTestWrite(t, filepath.Join(root, "minio.yml"), bytes.Replace(raw, []byte("path: "+p.StateDirectory+"\n"), []byte("path: /unapproved/local-blobs\n"), 1), 0600)
		}
		repairOperatorTestServiceAccess(t, f.base.directory, *p)
		for index := range p.Resources {
			hash, err := inspectRepairOperatorTree(t.Context(), f.base.host, p.Resources[index], *p)
			if err != nil {
				t.Fatal(kind, err)
			}
			p.Resources[index].Sha256 = hash
		}
		f.signHost()
		f.sign()
		if err := f.envelope.ready(f.ctx(), f.base.host, f.base.now); err == nil || f.base.starts != 0 {
			t.Fatal("signed generic host census bypassed actual operator source adapter", kind, err, f.base.starts)
		}
	}
}

// Fixtures use the actual parser's digest too; no separately manufactured
// summary can stand in for enabled configuration or a signing-resource join.
func repairOperatorPublicSource(raw []byte, plan repairOperatorHostPlan) (repairOperatorRuntimeSource, error) {
	var result repairOperatorRuntimeSource
	value, err := decodeRepairOperatorResource(raw)
	if err != nil {
		return result, err
	}
	urls, err := repairOperatorRpcUrls(value, plan)
	if err != nil {
		return result, err
	}
	inspection, err := controller.InspectStConfigBytes(bytes.Clone(raw), "mainnet", urls)
	if err != nil || inspection == nil || !inspection.Enabled {
		return result, errors.Join(errors.New("operator original public source is unavailable"), err)
	}
	result = repairOperatorRuntimeSource{PublicConfigSha256: fmt.Sprintf("sha256:%x", inspection.PublicConfigSha256), DeploymentId: inspection.DeploymentId, Netuid: inspection.Netuid, OperatorId: inspection.NoId, DepositSigner: strings.ToLower(inspection.DepositKeyAddress.Hex()), RootSigner: strings.ToLower(inspection.RootKeyAddress.Hex()), ArtifactSigner: strings.ToLower(inspection.ArtifactKeyAddress.Hex())}
	return result, nil
}

// The actual collector owns and joins a canceled snapshot before returning.
// Cancellation cannot consume a start or refill a later resumed allowance.
func TestRepairOperatorCanceledCensusJoinsBeforeReturning(t *testing.T) {
	f := newRepairOperatorFixture(t)
	f.claim()
	entered, canceled, released, exited := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }
	t.Cleanup(release)
	f.reader.before = func(ctx context.Context) error {
		defer close(exited)
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-released
		return errors.Join(ctx.Err(), syscall.EIO)
	}
	ctx, cancel := context.WithCancel(f.ctx())
	defer cancel()
	type outcome struct {
		status         string
		complete       bool
		err            error
		snapshotExited bool
	}
	joined, returned := make(chan outcome, 1), make(chan struct{})
	t.Cleanup(func() {
		cancel()
		release()
		select {
		case <-returned:
		case <-time.After(10 * time.Second):
			t.Error("operator canceled resume did not join during cleanup")
		}
		select {
		case <-entered:
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				t.Error("operator canceled snapshot did not join during cleanup")
			}
		default:
		}
	})
	go func() {
		defer close(returned)
		status, complete, err := f.envelope.resume(ctx, f.key, f.base.host, func() time.Time { return f.base.now })
		result := outcome{status: status, complete: complete, err: err}
		select {
		case <-exited:
			result.snapshotExited = true
		default:
		}
		joined <- result
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("operator actual collector never entered owned snapshot")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(10 * time.Second):
		t.Fatal("operator snapshot did not receive its original cancellation")
	}
	// The barrier keeps the callback alive after it observed cancellation.
	// This timer bounds a negative wait, not the source's work or readiness.
	select {
	case result := <-joined:
		t.Fatal("operator collector returned while its original snapshot was still held", result)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case result := <-joined:
		if !result.snapshotExited || result.complete || !errors.Is(result.err, context.Canceled) || !errors.Is(result.err, syscall.EIO) || f.base.starts != 0 {
			t.Fatal("operator canceled snapshot lost its joined cause or consumed start", result, f.base.starts)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("operator canceled snapshot outlived its caller")
	}
	f.reader.before = nil
	status, complete, err := f.resume()
	if err != nil || !complete || f.base.starts != 1 {
		t.Fatal("operator cancellation reset or lost original unconsumed custody", status, complete, err, f.base.starts)
	}
}

// A later signed incident reaches the original activation through the actual
// previous journal and marker; a merely observed process cannot supply the link.
func TestRepairOperatorSuccessorRetainsAcknowledgedOriginalJournal(t *testing.T) {
	for _, kind := range []string{"acknowledged", "unacknowledged", "missing", "reset-quota", "lost-original-row"} {
		f := newRepairOperatorFixture(t)
		f.claim()
		if kind == "unacknowledged" {
			f.base.startError = syscall.EIO
		}
		_, complete, err := f.resume()
		if kind == "unacknowledged" {
			if complete || !errors.Is(err, syscall.EIO) {
				t.Fatal("operator original uncertainty fixture did not occur", err)
			}
		} else if err != nil || !complete {
			t.Fatal("operator original acknowledged fixture did not complete", err)
		}
		old := f.envelope
		approvalRaw, approvalErr := os.ReadFile(f.approvalPath)
		journalRaw, journalErr := os.ReadFile(old.approval.Plan.StatePath)
		if err := errors.Join(approvalErr, journalErr); err != nil {
			t.Fatal(err)
		}
		f.base.now = f.base.now.Add(time.Minute)
		plan := old.approval.Plan
		if kind == "lost-original-row" {
			image := f.reader.images["operator-one"]
			image.Intents, image.Attempts = image.Intents[:1], image.Attempts[:3]
			// The terminal signature still exists in the store, so the real
			// collector can close its nonce interval while the DB lost custody.
			archive, err := strecovery.Collect(f.ctx(), f.archive.Selection, f.reader)
			if err != nil {
				t.Fatal("successor row-loss fixture cannot form its real census", err)
			}
			raw, err := json.Marshal(archive)
			if err != nil {
				t.Fatal(err)
			}
			plan.OriginalCensus = planFileReference{Path: filepath.Join(f.base.directory, "operator-successor-census.json"), Sha256: monitorReadDigest(raw)}
			repairValidatorTestWrite(t, plan.OriginalCensus.Path, raw, 0600)
		}
		plan.OriginalFiles = slices.Clone(plan.OriginalFiles)
		if kind == "reset-quota" {
			for index, retained := range plan.OriginalFiles {
				if !retained.Quota {
					continue
				}
				if err := os.Rename(retained.File.Path, retained.File.Path+"-predecessor"); err != nil {
					t.Fatal(err)
				}
				repairValidatorTestWrite(t, retained.File.Path, nil, 0600)
				if os.Geteuid() == 0 {
					if err := os.Chown(retained.File.Path, int(old.original.Plan.Uid), int(old.original.Plan.Gid)); err != nil {
						t.Fatal(err)
					}
				}
				var state syscall.Stat_t
				if err := syscall.Stat(retained.File.Path, &state); err != nil {
					t.Fatal(err)
				}
				plan.OriginalFiles[index].Device, plan.OriginalFiles[index].Inode = uint64(state.Dev), state.Ino
			}
		}
		plan.Predecessor = &repairProcessPredecessor{Approval: planFileReference{Path: f.approvalPath, Sha256: monitorReadDigest(approvalRaw)}, PublicKey: f.key, Journal: planFileReference{Path: plan.StatePath, Sha256: monitorReadDigest(journalRaw)}}
		plan.StatePath = filepath.Join(f.base.directory, "operator-successor-repair.json")
		plan.Previous = repairValidatorGeneration{InvocationId: strings.Repeat("7", 32), Pid: 72, StartedUsec: 200}
		plan.IncidentAt, plan.ValidFrom, plan.ExpiresAt = f.base.now, f.base.now, f.base.now.Add(time.Hour)
		f.approvalPath = filepath.Join(f.base.directory, "operator-successor-incident.json")
		f.envelope = &repairOperatorEnvelope{approval: repairOperatorApproval{Schema: repairOperatorSchema, Plan: plan}, original: old.original, reader: f.reader, procRoot: old.procRoot}
		prepareMainnetSnapshotTest(t, plan.StatePath, "mainnet-host-action", 64*1024)
		f.sign()
		f.base.host.operator = &repairOperatorTransports{reader: f.reader, procRoot: old.procRoot}
		raw, err := os.ReadFile(f.approvalPath)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := loadRepairOperatorEnvelope(f.ctx(), raw, f.key, f.base.host)
		if err != nil {
			t.Fatal("operator successor did not load original signed ancestry", kind, err)
		}
		f.base.manager["MainPID"], f.base.manager["ActiveState"], f.base.manager["SubState"] = "0", "inactive", "dead"
		if kind == "missing" {
			if err := os.Remove(old.approval.Plan.StatePath); err != nil {
				t.Fatal(err)
			}
		}
		err = loaded.ready(f.ctx(), f.base.host, f.base.now)
		if (err == nil) != (kind == "acknowledged") || f.base.starts != 1 {
			t.Fatal("operator successor re-created or lost original acknowledgement", kind, err, f.base.starts)
		}
	}
}

// Root fixtures grant the synthetic taskworker read/traverse through its primary
// group. Unselected real vault data has no such requirement in production.
func repairOperatorTestServiceAccess(t *testing.T, directory string, plan repairOperatorHostPlan) {
	t.Helper()
	if os.Geteuid() != 0 {
		return
	}
	paths := []string{directory, plan.env("WARP_HOME")}
	for _, tree := range plan.Resources {
		if err := filepath.WalkDir(tree.Path, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			paths = append(paths, path)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0640)
		if info.IsDir() {
			mode = 0750
		}
		if err := errors.Join(os.Chown(path, os.Getuid(), int(plan.Gid)), os.Chmod(path, mode)); err != nil {
			t.Fatal(err)
		}
	}
}

// The actual selected-resource adapter checks service credentials independently
// of the privileged reader; parsing valid bytes alone cannot supply access.
func TestRepairOperatorSourceReaderRequiresServiceCredentials(t *testing.T) {
	f := newRepairOperatorFixture(t)
	p := f.envelope.original.Plan
	p.Uid, p.Gid = p.Uid+1000, p.Gid+1000
	// Candidate lookup is permitted; only the selected file remains unreadable
	// by this principal, independently of the resolver's directory checks.
	selected := filepath.Join(p.env("WARP_VAULT_HOME"), "main", "1.0.0", "st.yml")
	for path := filepath.Dir(selected); ; path = filepath.Dir(path) {
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatal(err)
		}
		if path == f.base.directory {
			break
		}
	}
	if raw, err := readRepairOperatorResource(f.ctx(), f.base.host, p, p.env("WARP_VAULT_HOME"), "st.yml"); raw != nil || !errors.Is(err, errRpcIntegrity) {
		t.Fatal("privileged resource read replaced actual taskworker access", err)
	}
}

// The actual taskworker maps its CLI service port through the signed WARP host
// inputs. Socket ownership and the real HTTP request must follow that mapping.
func TestRepairOperatorStatusUsesActualWarpHostPort(t *testing.T) {
	f := newRepairOperatorFixture(t)
	p := &f.envelope.original.Plan
	originalPort := p.Port
	p.Port = 80
	p.Environment = append(p.Environment, repairOperatorEnvironment{"WARP_HOST_IPV4", "127.0.0.1"}, repairOperatorEnvironment{"WARP_PORTS", fmt.Sprintf("80:%d", originalPort)})
	slices.SortFunc(p.Environment, func(a, b repairOperatorEnvironment) int { return strings.Compare(a.Name, b.Name) })
	f.signHost()
	f.sign()
	f.base.manager["Environment"] = p.environment()
	f.base.manager["ExecStart"] = strings.Replace(f.base.manager["ExecStart"], fmt.Sprintf("--port=%d", originalPort), "--port=80", 1)
	root := filepath.Join(f.envelope.procRoot, "72")
	repairValidatorTestWrite(t, filepath.Join(root, "cmdline"), []byte(strings.Join(append([]string{p.Binary.Path}, strings.Fields(p.arguments())...), "\x00")+"\x00"), 0600)
	words := []string{"INVOCATION_ID=" + strings.Repeat("7", 32)}
	for _, value := range p.Environment {
		words = append(words, value.Name+"="+value.Value)
	}
	repairValidatorTestWrite(t, filepath.Join(root, "environ"), []byte(strings.Join(words, "\x00")+"\x00"), 0600)
	f.claim()
	status, complete, err := f.resume()
	if err != nil || !complete || f.base.starts != 1 {
		t.Fatal("operator postcondition ignored actual WARP host port selection", status, complete, err, f.base.starts)
	}
}

// A complete multi-source observation has one original monotonic age bound.
// A later fresh observation can close the same acknowledged process only.
func TestRepairOperatorWholeObservationRetainsAgeBound(t *testing.T) {
	f := newRepairOperatorFixture(t)
	f.claim()
	stamp := uint64(150)
	f.base.host.monotonic = func() (uint64, error) {
		if f.base.starts != 0 {
			stamp += uint64(f.envelope.approval.Plan.MaximumSampleAgeSeconds)*1000000 + 1
		}
		return stamp, nil
	}
	status, complete, err := f.resume()
	if status != "waiting-progress" || complete || !errors.Is(err, errRepairProcessPending) || f.base.starts != 1 {
		t.Fatal("operator refreshed a slow original observation or discarded its acknowledged start", status, complete, err, f.base.starts)
	}
	f.base.host.monotonic = func() (uint64, error) { return stamp, nil }
	status, complete, err = f.resume()
	if err != nil || !complete || f.base.starts != 1 {
		t.Fatal("operator fresh observation failed to retain its original consumed start", status, complete, err, f.base.starts)
	}
}
