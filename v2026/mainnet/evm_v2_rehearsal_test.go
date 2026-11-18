//go:build runtime_rehearsal

// R-1 (VERSION2-FINAL.md §15.8) deploys vault2 and STSettlement2 through the
// settlement v2 phase of bootstrap-contracts, with its readback, on the
// runtime rehearsal node of scripts/runtime-rehearsal/README.md:
//
//	RUNTIME_REHEARSAL_NODE=/path/to/node-subtensor \
//	  go test -tags runtime_rehearsal ./mainnet/ -run TestR1SettlementV2Phase -v
//
// Every command runs through runMain, as the sn-mainnet binary runs it, with
// the reviewed release of sim-testnet/contracts_gen.go. Only the custody
// declaration is the synthetic one of every bootstrap-contracts test, in
// place of the storage-prepare ceremony.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/evmrpc"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
	"golang.org/x/crypto/blake2b"
)

// One TAO in rao, and the rehearsal's escrow burn ceiling, the runtime's
// default MaxBurn.
const (
	r1Tao         = 1_000_000_000
	r1MaximumBurn = 100 * r1Tao
)

// The rehearsal's approved gas limits, the starting values of
// BOOTSTRAP-CONTRACTS.md, the optional claim batcher's last; R-1 reports
// each action's actual use.
var r1GasLimits = []uint64{4_000_000, 2_500_000, 3_000_000, 1_000_000, 150_000, 400_000}

// The approved demand fee of BOOTSTRAP-CONTRACTS.md: R-6's measured answer
// gas, 149,074, at 5 gwei, rounded up to 750,000 rao.
var r1DemandFeeWei = new(big.Int).Mul(big.NewInt(750_000), big.NewInt(1_000_000_000))

// One rehearsal chain: blocks sealed on demand and finalized at once.
type r1Node struct {
	ctx    context.Context
	sub    *crv4.Chain
	eth    *ethclient.Client
	http   string
	source string
}

// Starts scripts/runtime-rehearsal/run-node.sh on free ports in a temporary
// directory and stops the node when the test ends.
func startR1Node(t *testing.T) *r1Node {
	t.Helper()
	binary := os.Getenv("RUNTIME_REHEARSAL_NODE")
	if binary == "" {
		t.Fatal("RUNTIME_REHEARSAL_NODE must name the node-subtensor binary built by scripts/runtime-rehearsal/build.sh")
	}
	launcher, err := filepath.Abs(filepath.Join("..", "scripts", "runtime-rehearsal", "run-node.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "node.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	rpcPort := r1FreePort(t)
	cmd := exec.Command(launcher)
	cmd.Env = append(os.Environ(), "RUNTIME_REHEARSAL_NODE="+binary, "RUNTIME_REHEARSAL_BASE_PATH="+filepath.Join(dir, "chain"),
		"RUNTIME_REHEARSAL_RPC_PORT="+strconv.Itoa(rpcPort), "RUNTIME_REHEARSAL_P2P_PORT="+strconv.Itoa(r1FreePort(t)))
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		_ = log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(logPath)
			if len(raw) > 8192 {
				raw = raw[len(raw)-8192:]
			}
			t.Logf("node log tail:\n%s", bytes.TrimSpace(raw))
		}
	})
	n := &r1Node{ctx: t.Context(), http: "http://127.0.0.1:" + strconv.Itoa(rpcPort)}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		select {
		case err := <-exited:
			t.Fatalf("node exited before serving RPC: %v", err)
		default:
		}
		if n.sub, err = crv4.DialChainContext(n.ctx, "ws://127.0.0.1:"+strconv.Itoa(rpcPort)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node RPC not ready: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Cleanup(n.sub.API.Client.Close)
	// R-1 runs only on a runtime whose source was reviewed; the phase binds
	// that source to the node's own version.
	var reviewed bool
	if n.source, reviewed = crv4.ReviewedNativeOwnerSpecSource(uint32(n.sub.Runtime.SpecVersion)); n.sub.Runtime.SpecName != "node-subtensor" || !reviewed {
		t.Fatalf("node runs %s/%d, which has no reviewed source", n.sub.Runtime.SpecName, n.sub.Runtime.SpecVersion)
	}
	t.Logf("node runs node-subtensor/%d, reviewed source %s", n.sub.Runtime.SpecVersion, n.source)
	if n.eth, err = evmrpc.DialContext(n.ctx, n.http); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.eth.Close)
	return n
}

// A loopback TCP port for the node.
func r1FreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// Produces and finalizes one block holding every pending transaction.
func (self *r1Node) seal(t *testing.T) native.Hash {
	t.Helper()
	var created struct {
		Hash string `json:"hash"`
	}
	if err := self.sub.API.Client.CallContext(self.ctx, &created, "engine_createBlock", true, true, nil); err != nil {
		t.Fatalf("seal block: %v", err)
	}
	hash, err := native.NewHashFromHexString(created.Hash)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// Manual sealing advances the chain one 250 ms slot per block, faster than
// real time, while the node builds its pending block, which every send's
// admission reads, at the wall-clock slot, and Aura refuses a slot that does
// not increase. Waits until real time passes the latest block's timestamp.
func (self *r1Node) catchUp(t *testing.T) {
	t.Helper()
	header, err := self.eth.HeaderByNumber(self.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for time.Now().Unix() <= int64(header.Time)+1 {
		time.Sleep(100 * time.Millisecond)
	}
}

// Signs a call with the repository's signer, seals it into its own block and
// fails the test unless it dispatched.
func (self *r1Node) submit(t *testing.T, signer signature.KeyringPair, name string, args ...any) {
	t.Helper()
	call, err := native.NewCall(self.sub.Meta, name, args...)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	nonce, err := self.sub.AccountNonceContext(self.ctx, signer.Address)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := chain.EncodeSignedCall(self.sub, signer, call, nonce)
	if err != nil {
		t.Fatal(err)
	}
	var submitted string
	if err := self.sub.API.Client.CallContext(self.ctx, &submitted, "author_submitExtrinsic", codec.HexEncodeToString(raw)); err != nil {
		t.Fatalf("submit %s: %v", name, err)
	}
	if err := self.sub.VerifyFinalizedExtrinsicContext(self.ctx, self.seal(t), chain.ExtrinsicHash(raw)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// Dispatches a call as root through Alice's Sudo.sudo.
func (self *r1Node) sudo(t *testing.T, name string, args ...any) {
	t.Helper()
	call, err := native.NewCall(self.sub.Meta, name, args...)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	self.submit(t, signature.TestKeyringPairAlice, "Sudo.sudo", call)
}

// The latest block, which is also finalized, and its number.
func (self *r1Node) head(t *testing.T) (native.Hash, uint64) {
	t.Helper()
	hash, err := crv4.FinalizedHeadContext(self.ctx, self.sub)
	if err != nil {
		t.Fatal(err)
	}
	header, err := self.sub.HeaderAtContext(self.ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	return hash, uint64(header.Number)
}

// Funds an EVM address's mapped account from Alice.
func (self *r1Node) fundEvm(t *testing.T, address common.Address, rao uint64) {
	t.Helper()
	mirror := ss58.EvmMirrorPubkey(address)
	destination, err := native.NewMultiAddressFromAccountID(mirror[:])
	if err != nil {
		t.Fatal(err)
	}
	self.submit(t, signature.TestKeyringPairAlice, "Balances.transfer_keep_alive", destination, native.NewUCompactFromUInt(rao))
}

// Registers subnets owned by Alice until netuid 25 exists, which the phase
// is pinned to, and starts its emission so that registration is open.
func (self *r1Node) createSubnet25(t *testing.T) {
	t.Helper()
	self.sudo(t, "AdminUtils.sudo_set_network_rate_limit", native.NewU64(0))
	self.sudo(t, "AdminUtils.sudo_set_network_min_lock_cost", native.NewU64(r1Tao))
	self.sudo(t, "AdminUtils.sudo_set_lock_reduction_interval", native.NewU64(1))
	for range 32 {
		var hotkey [32]byte
		if _, err := rand.Read(hotkey[:]); err != nil {
			t.Fatal(err)
		}
		account, err := native.NewAccountID(hotkey[:])
		if err != nil {
			t.Fatal(err)
		}
		self.submit(t, signature.TestKeyringPairAlice, "SubtensorModule.register_network", *account)
		head, _ := self.head(t)
		if _, registered, err := chain.UIDAtContext(self.ctx, self.sub, 25, hotkey, head); err != nil || registered {
			if err != nil {
				t.Fatal(err)
			}
			self.submit(t, signature.TestKeyringPairAlice, "SubtensorModule.start_call", native.NewU16(25))
			return
		}
	}
	t.Fatal("subnet 25 was not created")
}

// Sends one legacy EVM transaction, seals it and returns its receipt.
func (self *r1Node) transact(t *testing.T, key *ecdsa.PrivateKey, to *common.Address, data []byte) *types.Receipt {
	t.Helper()
	from := crypto.PubkeyToAddress(key.PublicKey)
	nonce, err := self.eth.NonceAt(self.ctx, from, nil)
	if err != nil {
		t.Fatal(err)
	}
	price, err := self.eth.SuggestGasPrice(self.ctx)
	if err != nil {
		t.Fatal(err)
	}
	gas, err := self.eth.EstimateGas(self.ctx, ethereum.CallMsg{From: from, To: to, Data: data})
	if err != nil {
		t.Fatalf("estimate gas: %v", err)
	}
	tx, err := types.SignNewTx(key, types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId)), &types.LegacyTx{Nonce: nonce, GasPrice: price, Gas: gas, To: to, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if err := self.eth.SendTransaction(self.ctx, tx); err != nil {
		t.Fatalf("send transaction: %v", err)
	}
	self.seal(t)
	deadline := time.Now().Add(30 * time.Second)
	for {
		receipt, err := self.eth.TransactionReceipt(self.ctx, tx.Hash())
		if err == nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				t.Fatalf("transaction %s failed", tx.Hash())
			}
			return receipt
		}
		if !errors.Is(err, ethereum.NotFound) || time.Now().After(deadline) {
			t.Fatalf("receipt of %s: %v", tx.Hash(), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A fresh EVM key.
func r1Key(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// The node's identity as the phase's approval pins it: chain name, genesis,
// the reviewed source of this node's runtime version, the BLAKE2b-256 of its
// code and of its metadata, and the finalized start block.
func (self *r1Node) identity(t *testing.T) (planNetwork, rootReceiptProfile, native.Hash, uint64) {
	t.Helper()
	var name, genesis, code, metadata string
	var version crv4.RuntimeVersionIdentity
	for _, read := range []struct {
		method string
		params []any
		result any
	}{{"system_chain", nil, &name}, {"chain_getBlockHash", []any{0}, &genesis}, {"state_getRuntimeVersion", nil, &version}, {"state_getStorageHash", []any{runtimeCodeStorageKey}, &code}, {"state_getMetadata", nil, &metadata}} {
		if err := self.sub.API.Client.CallContext(self.ctx, read.result, read.method, read.params...); err != nil {
			t.Fatalf("%s: %v", read.method, err)
		}
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(metadata, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	digest := blake2b.Sum256(raw)
	head, number := self.head(t)
	return planNetwork{NativeChain: name, GenesisHash: genesis, EvmChainId: mainnetEvmChainId},
		rootReceiptProfile{RuntimeSourceCommit: self.source, RuntimeVersion: version, RuntimeCodeHash: code, RuntimeMetadataHash: "0x" + hex.EncodeToString(digest[:])}, head, number
}

// One approved phase and its custody, driven through the top-level command.
type r1Phase struct {
	t          *testing.T
	n          *r1Node
	storage    *durablefixture.Fixture
	config     evmPhaseConfig
	configPath string
	deployer   *ecdsa.PrivateKey
}

// Runs one bootstrap-contracts command and decodes its result.
func (self *r1Phase) command(command string, extra ...string) (evmCreateResult, int, string) {
	self.t.Helper()
	args := []string{"bootstrap-contracts", command, "--config", self.configPath, "--run-dir", self.config.Plan.RunDirectory, "--accept-plan-hash", self.config.Plan.hash()}
	if slices.Contains(extra, "--online") {
		self.n.catchUp(self.t)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(self.storage.Context, append(args, extra...), &stdout, &stderr)
	var result evmCreateResult
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			self.t.Fatal(err)
		}
	}
	return result, code, stderr.String()
}

// Runs a command that must end with the given status.
func (self *r1Phase) require(status, command string, extra ...string) evmCreateResult {
	self.t.Helper()
	result, code, diagnostic := self.command(command, extra...)
	if code != 0 || result.Status != status {
		self.t.Fatalf("%s %v: %d %s %+v, want %s", command, extra, code, diagnostic, result, status)
	}
	return result
}

// The deployer's original signature over the envelope a command printed, in
// its own pinned file, as the operator signs apply's unsigned transaction.
func (self *r1Phase) sign(id, printed string) (string, string) {
	self.t.Helper()
	encoded, err := hex.DecodeString(strings.TrimPrefix(printed, "0x"))
	if err != nil {
		self.t.Fatal(err)
	}
	var unsigned types.Transaction
	if err := unsigned.UnmarshalBinary(encoded); err != nil {
		self.t.Fatal(err)
	}
	tx, err := types.SignTx(&unsigned, types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId)), self.deployer)
	if err != nil {
		self.t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		self.t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(self.configPath), id+".signed.bin")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		self.t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return path, "sha256:" + hex.EncodeToString(digest[:])
}

// Completes a sent action online, sealing a block between attempts until
// its receipt is retained after the readback.
func (self *r1Phase) complete(id string) evmCreateResult {
	self.t.Helper()
	completed := evmV2ActionAt(evmV2ActionIndex(id)).completed
	for range 8 {
		result, code, diagnostic := self.command("resume", "--action", id, "--online")
		if code == 0 && result.Status == completed {
			return result
		}
		self.t.Logf("%s: %d %s %s", id, code, result.Status, strings.TrimSpace(diagnostic))
		self.n.seal(self.t)
	}
	self.t.Fatalf("%s did not complete", id)
	return evmCreateResult{}
}

// R-1: on SN25 of the rehearsal chain, with a v1 coordinator in place, one
// approval deploys the five actions and the optional sixth: vault2 and its
// escrow registration broadcast back to back and sealed into one block, the
// STSettlement2 implementation, the initialized proxy, the coordinator
// binding and the claim batcher, each completed only after its readback
// against the real precompiles.
func TestR1SettlementV2Phase(t *testing.T) {
	n := startR1Node(t)
	n.createSubnet25(t)
	release := evmV2TestRelease(t)

	// The initializer's v1 coordinator: the reviewed v1 implementation's code.
	v1Deployer := r1Key(t)
	n.fundEvm(t, crypto.PubkeyToAddress(v1Deployer.PublicKey), 100*r1Tao)
	coordinator, err := hex.DecodeString(evmV2TestArtifactNamed(t, evmTestRelease(t), "Coordinator").Creation)
	if err != nil {
		t.Fatal(err)
	}
	v1 := n.transact(t, v1Deployer, nil, coordinator).ContractAddress

	// The approved graph: one deployer at consecutive nonces, a fee cap of
	// twice the node's gas price and the escrow's burn ceiling as funding.
	deployerKey := r1Key(t)
	deployer := crypto.PubkeyToAddress(deployerKey.PublicKey)
	n.fundEvm(t, deployer, 1_000*r1Tao)
	price, err := n.eth.SuggestGasPrice(n.ctx)
	if err != nil {
		t.Fatal(err)
	}
	feeCap := new(big.Int).Mul(price, big.NewInt(2))
	vault, implementation, proxy := crypto.CreateAddress(deployer, 0), crypto.CreateAddress(deployer, 2), crypto.CreateAddress(deployer, 3)
	var escrowHotkey [32]byte
	if _, err := rand.Read(escrowHotkey[:]); err != nil {
		t.Fatal(err)
	}
	owner, auditor := crypto.PubkeyToAddress(r1Key(t).PublicKey), crypto.PubkeyToAddress(r1Key(t).PublicKey)
	initializer := stabi.NewSTSettlement2().PackInitialize(owner, v1, vault, []common.Address{auditor}, big.NewInt(1))
	funding := new(big.Int).Mul(big.NewInt(r1MaximumBurn), big.NewInt(r1Tao))
	data := []string{
		evmV2TestVaultData(t, release, 25, escrowHotkey, ss58.EvmMirrorPubkey(vault), deployer, r1DemandFeeWei),
		"0x" + hex.EncodeToString(stabi.NewSTSettlementVault2().PackRegisterEscrow(r1MaximumBurn)),
		"0x" + evmV2TestArtifactNamed(t, release, "Settlement2").Creation,
		evmV2TestProxyData(t, release, implementation, initializer),
		"0x" + hex.EncodeToString(stabi.NewSTSettlementVault2().PackSetCoordinatorOnce(proxy)),
		"0x" + evmV2TestArtifactNamed(t, release, "ClaimBatcher").Creation,
	}
	ids := append(evmV2ActionIds(), evmV2BatcherAction.id)
	total := new(big.Int).Set(funding)
	actions := []evmPhaseAction{}
	for index, id := range ids {
		action := evmPhaseAction{Id: id, Sender: deployer, Nonce: uint64(index), Data: data[index], ValueWei: "0", Gas: r1GasLimits[index], FeeCapWei: feeCap.String(), TipCapWei: "0"}
		if index == 1 || index == 4 {
			action.To = &vault
		}
		if index == 1 {
			action.ValueWei = funding.String()
		}
		total.Add(total, new(big.Int).Mul(new(big.Int).SetUint64(action.Gas), feeCap))
		actions = append(actions, action)
	}

	// The release, the custody directory and the independent approval.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	releaseRaw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	releasePath := filepath.Join(dir, "settlement-v2-contracts.json")
	if err := os.WriteFile(releasePath, releaseRaw, 0600); err != nil {
		t.Fatal(err)
	}
	releaseDigest := sha256.Sum256(releaseRaw)
	network, runtime, start, startNumber := n.identity(t)
	p := &r1Phase{t: t, n: n, configPath: filepath.Join(dir, "settlement-v2.json"), deployer: deployerKey}
	p.config = evmPhaseConfig{Schema: evmPhase2ConfigSchema, Plan: evmPhasePlan{Schema: evmPhase2Schema, DeploymentId: "rehearsal-settlement-v2", Network: network, Runtime: runtime,
		Route: ownedSubmissionRoute{RpcUrl: n.http, ReadRetrySeconds: 60, SendTimeoutSeconds: 30}, RunDirectory: dir,
		Artifacts:      planFileReference{Path: releasePath, Sha256: "sha256:" + hex.EncodeToString(releaseDigest[:])},
		SourceLockHash: "sha256:" + strings.Repeat("10", 32), CustodyId: "rehearsal-exclusive-deployer", CustodyFenceHash: "sha256:" + strings.Repeat("11", 32), CutoverEvidenceHash: "sha256:" + strings.Repeat("12", 32),
		StartNativeNumber: startNumber, StartNativeHash: start.Hex(), ValidThroughNative: startNumber + 7200, MaximumAttempts: 8, MaximumTotalWei: total.String(), Netuid: 25, Actions: actions}}
	seed := sha256.Sum256([]byte("rehearsal settlement v2 independent approval"))
	approval := ed25519.NewKeyFromSeed(seed[:])
	p.config.ApprovalPublicKey = "0x" + hex.EncodeToString(approval.Public().(ed25519.PublicKey))
	message, err := p.config.Plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	p.config.Signature = hex.EncodeToString(ed25519.Sign(approval, message))
	configRaw, err := json.Marshal(p.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.configPath, configRaw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range evmV2JournalNames() {
		prepareMainnetSnapshotTest(t, filepath.Join(dir, name), "mainnet-evm-action", 512*1024)
	}
	p.storage = durablefixture.New(t, t.Context(), dir)

	// Vault2 and its escrow registration, back to back (A7): vault2's apply
	// prints both envelopes, and the deployer signs both before any send.
	prepared := p.require("signature-awaiting-import", "apply", "--action", "vault2-create")
	vaultPath, vaultHash := p.sign("vault2-create", prepared.UnsignedTransaction)
	escrowPath, escrowHash := p.sign("escrow2-register", prepared.EscrowUnsignedTransaction)
	p.require("signed-custody-complete", "resume", "--action", "vault2-create", "--signed-transaction", vaultPath, "--signed-transaction-hash", vaultHash, "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash)
	p.require("submitted-with-escrow-registration-awaiting-canonical-receipts", "resume", "--action", "vault2-create", "--online", "--submit")
	n.seal(t)
	results := []evmCreateResult{p.complete("vault2-create")}
	if escrow := p.require("signature-awaiting-import", "apply", "--action", "escrow2-register"); escrow.UnsignedTransaction != prepared.EscrowUnsignedTransaction || escrow.SigningDigest != prepared.EscrowSigningDigest {
		t.Fatalf("escrow2's envelope differs from the one vault2 printed: %+v %+v", escrow, prepared)
	}
	p.require("signed-custody-complete", "resume", "--action", "escrow2-register", "--signed-transaction", escrowPath, "--signed-transaction-hash", escrowHash)
	results = append(results, p.complete("escrow2-register"))
	if results[1].Receipt.BlockHash != results[0].Receipt.BlockHash || results[1].EscrowCollateral == nil || *results[1].EscrowCollateral != contractEscrowCollateralExpected {
		t.Fatalf("escrow registration outside vault2's block or with collateral: %+v %+v", results[0].Receipt, results[1])
	}

	// The implementation, the initialized proxy, the binding and the batcher.
	for _, id := range ids[2:] {
		path, hash := p.sign(id, p.require("signature-awaiting-import", "apply", "--action", id).UnsignedTransaction)
		p.require("signed-custody-complete", "resume", "--action", id, "--signed-transaction", path, "--signed-transaction-hash", hash)
		p.require("submitted-awaiting-canonical-receipt", "resume", "--action", id, "--online", "--submit")
		n.seal(t)
		results = append(results, p.complete(id))
	}
	link, batcher := results[4], results[5]
	if !link.InstallationComplete || link.ActivationReady || link.Vault2Address != vault.Hex() || link.Settlement2Address != proxy.Hex() || link.Settlement2ImplementationAddress != implementation.Hex() {
		t.Fatalf("installation: %+v", link)
	}
	if !batcher.InstallationComplete || batcher.ClaimBatcherAddress != crypto.CreateAddress(deployer, 5).Hex() || batcher.Receipt.ContractAddress != batcher.ClaimBatcherAddress {
		t.Fatalf("claim batcher: %+v", batcher)
	}
	for index, result := range results {
		t.Logf("R-1 %s: block %d, gas used %d of limit %d", ids[index], result.Receipt.BlockNumber, result.Receipt.GasUsed, r1GasLimits[index])
	}
}
