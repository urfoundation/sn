//go:build linux || darwin

// Capture the flag end to end (CAPTURETHEFLAG.md §3 to §10, §11.2): UR's CTF
// pool through its whole cycle on the e2e chain, with the real contracts, the
// ctf package, the validator's auditor sender and the real snclaim.
//
// The chain is mainnet's configuration (evm_v2_ctf_upgrade_test.go): pool 1
// registered for v2 from epoch 2, STSettlement2 on phase C behind the
// reviewed 72 h timelock, and vault2's coldkey a hotkey account of the
// guardian. The folded operation upgrades it to STSettlement2Ctf and
// registers pool 2^32 + 1 from epoch 3 under the hotkey derived at execution;
// the carry contract is created through the deterministic deployment proxy
// with its 1 TAO seed, as ctf-carry review checks it before and after, once
// the guardian holds its coldkey too and the proxy's account is funded above
// its existential deposit; the signed amendment entry v2 puts capture the
// flag in force from epoch 3 with that carry, signed by the test's key in
// place of the price sheet's (e2eAmendmentSignerEnv).
//
// Each epoch then runs as the server's driver runs it (server
// controller/st_ctf_driver.go): emission on the CTF pool hotkey, the close,
// the census committed by the root signer in [end(k), end(k) + 600], the
// ledger built by ctf.Build with drand's real quicknet pulse of ρ_k, its
// objects published, header last, and its root committed by the root signer.
// The builder reads the head bindings as the server does, from the
// coordinator's FleetBound events and bindingAt at the epoch's boundaries.
// The validator's CTF audit reads its input through the settlement v2
// audit's own readers (validator.SettlementV2CtfAuditInput: the entry in
// force, the carry's pool and coldkey, recycledFor, ctfCensus, the census
// block's time, the pulse and its head oracle, whose bindings must equal the
// builder's), runs ctf.Verify, and approves through the validator's
// SettlementV2Sender. snclaim v2 check audits the pending root as UR's CTF
// watcher does, then the root finalizes after its window, a winner claims
// through snclaim v2 claim, and anyone recycles the carry account through
// STCtfCarry, which keeps its residual and returns the rest, for the next
// epoch's close to capture.
//
// The runtime is the e2e chain's Go fakes of runtime 475 (stake moves,
// registration, UIDs) with the address mapping, the Ed25519 and sr25519
// checks, runtime 486's staking-hotkey lists, opt-outs and auto-stake
// destinations, addStake, and the existential deposit on the proxy and the
// carry (settlement_v2_e2e_chain_test.go), not a subtensor node: rehearsal
// R-9 runs the upgrade, the census and the carry on the live runtime. The
// guardian's associations, the proxy's funding and root's sweep are native
// steps, as emission is. Every block is final at once, and drand's pulse is
// served by a local relay that answers with the real signature pinned here.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/amendment"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ctf"
	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
	"github.com/urfoundation/sn/validator"
)

const (
	// ctfE2ECensusBlock is B_3, the block that commits epoch 3's census, a
	// fixed block inside its window [end(3), end(3) + 600], so the draw's
	// round is the one pinned below while the fixture's deployment keeps
	// end(3) within 300 blocks of where it is. ctfE2ERound is ρ_3, the first
	// quicknet round at least 60 s after B_3's time, and ctfE2ESignature
	// that round's real signature, as drand's relays serve it
	// (GET https://api.drand.sh/<quicknet chain hash>/public/<round>).
	ctfE2ECensusBlock = 158_705
	ctfE2ERound       = 33_033_719
	ctfE2ESignature   = "980fc88d2c2cdb8ccdb491c1e838c38f40df87e7f3d6ca44c752f24ee88835fe611a54436447176c65263cced115a6e8"
	// The CTF pool's emission in epochs 3 and 4, in rao: epoch 3's slots are
	// 0.01 α each.
	ctfE2EEmission3 = 24_900_000_000
	ctfE2EEmission4 = 2_490_000_000
	// The carry's seed, in rao: 1 TAO, as on mainnet (CAPTURETHEFLAG.md
	// §11.2), above its least of ten times the runtime's nominator minimum.
	ctfE2ESeed             = 1_000_000_000
	ctfE2ENominatorMinimum = 50_000_000
	// Root raises the nominator minimum to 2 TAO, and its sweep clears the
	// 1 TAO residual: the carry's least seed becomes 20 TAO.
	ctfE2ERaisedMinimum = 2_000_000_000
)

var (
	e2eCtfCarryBinding = stabi.NewSTCtfCarry()
	ctfE2EPool         = uint64(ctf.PoolID(e2eNoID))
	// ctfE2EBound is the head-bound client the census lists, a provider in
	// IS, which P5 should have kept out of it.
	ctfE2EBound = ctfE2EClient(12)
)

// ctfE2EClient is census client n, and ctfE2EColdkey its payout coldkey.
func ctfE2EClient(n byte) [16]byte { return [16]byte{0: 0xc7, 15: n} }

func ctfE2EColdkey(client [16]byte) [32]byte { return e2eKey(fmt.Sprintf("ctf-client-%x", client)) }

// ctfE2E is the chain after the capture the flag upgrade, with the carry,
// the signed entry, a drand relay and a head binding.
type ctfE2E struct {
	*ctfTestChain
	ctx context.Context
	// hotkey is the CTF pool's hotkey, derived at the execution.
	hotkey [32]byte
	// carry is the carry contract and carryColdkey its coldkey;
	// carryGuardian is the coldkey whose hotkey accounts the carry's coldkey
	// and vault2's are (not the v1 coordinator's guardian).
	carry         common.Address
	carryColdkey  [32]byte
	carryGuardian [32]byte
	// signer signs the amendments file, which web serves at /amendments.yml
	// beside quicknet's pinned pulse, as a drand relay serves it.
	signer     *ecdsa.PrivateKey
	amendments []byte
	web        *httptest.Server
	// validator is the validator's chain client, and sender its auditor
	// sender.
	validator *validator.ChainClient
	sender    *validator.SettlementV2Sender
	// ledgers are the CTF roots built, by root index once finalized.
	ledgers []*ctf.Ledger
	// short is the rao every stake move credits its destination short of
	// its amount: 0, or 1 once the runtime's shortfall knob is set.
	short int64
}

// newCtfE2E has the guardian make vault2's coldkey a hotkey account,
// upgrades the proxy through the folded timelock operation, creates the
// carry, signs the amendment entry and binds ctfE2EBound to a head fleet from
// epoch 3 to 4.
func newCtfE2E(t *testing.T) *ctfE2E {
	t.Helper()
	f := newCtfTestChain(t)
	e := &ctfE2E{ctfTestChain: f, ctx: context.Background(), carryGuardian: e2eKey("ctf-guardian")}
	// The guardian's first step (§11.2 step 0), sent on the runtime through
	// MevShield.submit_encrypted: vault2's coldkey becomes its hotkey
	// account, so no coldkey swap can merge into vault2 or reserve it.
	if !f.chain.associateHotkey(e.carryGuardian, f.vault2Coldkey) {
		t.Fatal("the guardian does not hold vault2's coldkey")
	}
	f.chain.setNominatorMinimum(ctfE2ENominatorMinimum)
	operation := f.operation()
	f.schedule(operation)
	// The timelock holds exactly the value; a stranger sends the execute, and
	// the burn's surplus returns to the timelock.
	receipt := f.must(f.stranger, f.timelock, nil, operation.execute)
	e.hotkey = f.ctfHotkey(receipt.BlockNumber.Uint64())
	if hotkey, firstEpoch, registered := f.ctfPool(); !registered || hotkey != e.hotkey || firstEpoch != ctfTestFirstEpoch || f.implementation() != operation.implementation {
		t.Fatalf("the execute registered %s from %d (%t), proxy on %s", hotkey, firstEpoch, registered, f.implementation())
	}
	if balance := f.chain.balance(f.timelock); balance.Cmp(new(big.Int).Sub(ctfTestValue, ctfTestBurn)) != 0 {
		t.Fatalf("the timelock holds %s wei after the execute", balance)
	}
	f.publisher.lock.Lock()
	f.publisher.pools = append(f.publisher.pools, ctfE2EPool)
	f.publisher.lock.Unlock()
	e.createCarry()
	e.signEntry()
	e.web = httptest.NewServer(http.HandlerFunc(e.serveWeb))
	t.Cleanup(e.web.Close)
	f.snclaimEnv = []string{e2eAmendmentSignerEnv + "=" + crypto.PubkeyToAddress(e.signer.PublicKey).Hex()}
	var err error
	if e.validator, err = validator.DialReleaseChainContext(e.ctx, []string{f.chain.server.URL}, f.coordinator); err != nil {
		t.Fatal(err)
	}
	if e.sender, err = validator.NewSettlementV2Sender(e.validator, []string{f.chain.server.URL}, e2eChainID, validator.SettlementV2Addresses{Settlement: f.settlement, Vault: f.vault2}, f.auditor.keyFile(t), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	e.bindHead(ctfE2EBound, ctfTestFirstEpoch, ctfTestFirstEpoch+1)
	return e
}

// createCarry creates STCtfCarry(vault2, 2^32 + 1) through the deterministic
// deployment proxy as the owner's steps do (CAPTURETHEFLAG.md §11.2), each
// one checked by ctf-carry review. Review refuses until the guardian holds
// the carry's coldkey as a hotkey account. That makes the coldkey public, and
// a stranger sends it stake on a hotkey of its own, which review reports and
// does not refuse. Review then refuses until the proxy's account holds TAO
// above its existential deposit, without which the creation's value cannot
// pass through it; funded by a native transfer, review preflights the
// creation, which sends the 1 TAO seed and the deposit as its value. The runtime keeps the deposit on the proxy and on the carry, as
// Frontier does, so the carry stakes exactly its seed on vault2's escrow
// hotkey as its residual; it pins the pool hotkey as its auto-stake
// destination and opts out of third-party staking-hotkey growth and of
// locked alpha. Review then checks the carry it made, and every view.
func (e *ctfE2E) createCarry() {
	f, t := e.ctfTestChain, e.t
	artifact := e2eForgeArtifact(t, "STCtfCarry")
	path := filepath.Join(t.TempDir(), "STCtfCarry.json")
	if err := os.WriteFile(path, artifact.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	salt := crypto.Keccak256Hash([]byte("urnetwork-e2e-ctf-carry"))
	initcode := append(slices.Clone(artifact.creation), e2eCtfCarryBinding.PackConstructor(f.vault2, ctfTestPoolId)...)
	createData := append(salt.Bytes(), initcode...)
	value := new(big.Int).Mul(e2eRao(ctfE2ESeed+e2eExistentialDepositRao), big.NewInt(e2eRaoWei))
	e.carry = crypto.CreateAddress2(upgradeCreate2Factory, salt, crypto.Keccak256(initcode))
	e.carryColdkey = ss58.EvmMirrorPubkey(e.carry)
	f.chain.keepExistentialDeposit(upgradeCreate2Factory, e.carry)
	review := func(extra ...string) (ctfCarryReview, int, string) {
		t.Helper()
		args := append([]string{"review", "--rpc", f.rpc, "--settlement2", f.settlement.Hex(), "--no-id", "1", "--artifact", path,
			"--salt", salt.Hex(), "--seed-rao", strconv.Itoa(ctfE2ESeed), "--guardian", common.Hash(e.carryGuardian).Hex()}, extra...)
		var stdout, stderr strings.Builder
		code := runCtfCarryCommand(e.ctx, args, &stdout, &stderr)
		var result ctfCarryReview
		if code == 0 {
			if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
				t.Fatalf("ctf-carry review output %q: %v", stdout.String(), err)
			}
		}
		return result, code, stderr.String()
	}

	// The guardian has not taken the carry's coldkey yet.
	if _, code, stderr := review(); code != 3 || !strings.Contains(stderr, "is not a hotkey account yet") {
		t.Fatalf("review before the carry's coldkey is the guardian's: exit %d\n%s", code, stderr)
	}
	if !f.chain.associateHotkey(e.carryGuardian, e.carryColdkey) {
		t.Fatal("the guardian does not hold the carry's coldkey")
	}
	// The stranger's dust is a third-party entry on the carry's list, which
	// stays: the creation and every move go through it.
	dust := e2eKey("ctf-carry-dust-hotkey")
	f.chain.emit(dust, ss58.EvmMirrorPubkey(f.stranger.address), e2eRao(1_000_000))
	transfer, err := e2eRuntimeABI.Pack("transferStake", e.carryColdkey, dust, big.NewInt(e2eNetuid), big.NewInt(e2eNetuid), big.NewInt(1_000_000))
	if err != nil {
		t.Fatal(err)
	}
	f.must(f.stranger, e2eStaking, nil, transfer)
	// The proxy holds nothing above its deposit: the creation would fail,
	// as review says.
	if _, code, stderr := review(); code != 3 || !strings.Contains(stderr, "holds no TAO above its existential deposit") {
		t.Fatalf("review with the proxy unfunded: exit %d\n%s", code, stderr)
	}
	var unfunded error
	f.chain.do(func() {
		_, _, unfunded = f.chain.callLocked(f.anyone.address, &upgradeCreate2Factory, value, createData, e2eTxGas, f.chain.head)
	})
	if unfunded == nil {
		t.Fatal("the creation passes its value through a proxy that holds only the value")
	}
	f.chain.transferNative(upgradeCreate2Factory, e2eExistentialDepositRao+1)
	before, code, stderr := review("--from", f.anyone.address.Hex())
	if code != 0 || !before.Preflight.Ran || before.Carry.Address != e.carry.Hex() || before.Carry.Coldkey != common.Hash(e.carryColdkey).Hex() ||
		before.Carry.CreateData != hexutil.Encode(createData) || before.Carry.CreateValueWei != value.String() || before.Created != nil ||
		before.Runtime.SeedMinimumRao != strconv.Itoa(10*ctfE2ENominatorMinimum) || before.Runtime.StakingHotkeys != 1 {
		t.Fatalf("review before the creation: exit %d %+v %+v\n%s", code, before.Carry, before.Runtime, stderr)
	}

	receipt := f.must(f.anyone, upgradeCreate2Factory, value, hexutil.MustDecode(before.Carry.CreateData))
	var seeded *stabi.STCtfCarrySeeded
	for _, log := range receipt.Logs {
		if log.Address == e.carry {
			seeded, _ = e2eCtfCarryBinding.UnpackSeededEvent(log)
		}
	}
	if seeded == nil || seeded.Tao.Cmp(e2eRao(ctfE2ESeed)) != 0 || seeded.Amount.Cmp(e2eRao(ctfE2ESeed)) != 0 || seeded.Residual.Cmp(e2eRao(ctfE2ESeed)) != 0 {
		t.Fatalf("the creation seeded %+v", seeded)
	}
	after, code, stderr := review()
	if code != 0 || after.Created == nil || !after.Carry.Exists || after.Created.ResidualRao != strconv.Itoa(ctfE2ESeed) || after.Created.EscrowPositionRao != strconv.Itoa(ctfE2ESeed) ||
		!after.Created.EscrowListed || !after.Created.PoolHotkeyPinned || !after.Created.PoolListed || !after.Created.RejectsGrowth || !after.Created.RejectsLockedAlpha ||
		after.Created.AmendmentCarryEntry != e.carry.Hex() {
		t.Fatalf("review of the created carry: exit %d %+v\n%s", code, after.Created, stderr)
	}

	vault := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackVault(), e2eCtfCarryBinding.UnpackVault)
	pool := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackPoolId(), e2eCtfCarryBinding.UnpackPoolId)
	netuid := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackNetuid(), e2eCtfCarryBinding.UnpackNetuid)
	escrow := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackEscrowHotkey(), e2eCtfCarryBinding.UnpackEscrowHotkey)
	hotkey := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackPoolHotkey(), e2eCtfCarryBinding.UnpackPoolHotkey)
	vaultColdkey := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackVaultColdkey(), e2eCtfCarryBinding.UnpackVaultColdkey)
	coldkey := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackColdkey(), e2eCtfCarryBinding.UnpackColdkey)
	minimum := e2eView(f.e2eFixture, e.carry, e2eCtfCarryBinding.PackSeedMinimum(), e2eCtfCarryBinding.UnpackSeedMinimum)
	if coldkey != e.carryColdkey || vault != f.vault2 || pool.Cmp(ctfTestPoolId) != 0 || netuid != e2eNetuid || escrow != f.escrowV2 || hotkey != e.hotkey ||
		vaultColdkey != f.vault2Coldkey || e.residual().Cmp(e2eRao(ctfE2ESeed)) != 0 || minimum.Cmp(e2eRao(10*ctfE2ENominatorMinimum)) != 0 {
		t.Fatalf("carry %s reads coldkey 0x%x, vault %s pool %s netuid %d, escrow 0x%x, pool hotkey 0x%x, vault coldkey 0x%x, residual %s, least seed %s",
			e.carry, coldkey, vault, pool, netuid, escrow, hotkey, vaultColdkey, e.residual(), minimum)
	}
	// The proxy passed the value on and keeps its funding; the carry keeps
	// the deposit and staked the rest. Its list holds the stranger's dust,
	// the escrow hotkey, by the residual, and the pool hotkey, by the pin,
	// which holds no stake.
	listed := f.chain.stakingHotkeys(e.carryColdkey)
	if f.chain.balance(upgradeCreate2Factory).Cmp(new(big.Int).Mul(e2eRao(e2eExistentialDepositRao+1), big.NewInt(e2eRaoWei))) != 0 ||
		f.chain.balance(e.carry).Cmp(new(big.Int).Mul(e2eRao(e2eExistentialDepositRao), big.NewInt(e2eRaoWei))) != 0 ||
		len(listed) != 3 || !slices.Contains(listed, dust) || !slices.Contains(listed, f.escrowV2) || !slices.Contains(listed, e.hotkey) ||
		e.chain.stake(e.hotkey, e.carryColdkey).Sign() != 0 {
		t.Fatalf("after the creation the proxy holds %s wei, the carry %s wei, its list %x", f.chain.balance(upgradeCreate2Factory), f.chain.balance(e.carry), listed)
	}
}

// signEntry signs the capture the flag entry of operator 1 from epoch 3 with
// the carry, as cli/amendment attach writes it.
func (e *ctfE2E) signEntry() {
	t := e.t
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte("urnetwork-e2e-amendment-signer")))
	if err != nil {
		t.Fatal(err)
	}
	entry := amendment.NewCaptureTheFlag(e2eChainID, e2eNetuid, e2eNoID, ctfTestFirstEpoch, e.carry)
	if err := entry.Sign(key); err != nil {
		t.Fatal(err)
	}
	line, err := entry.Line()
	if err != nil {
		t.Fatal(err)
	}
	e.signer = key
	e.amendments = []byte(fmt.Sprintf("sn: %d\nsigned_amendments:\n  - line: \"%s\"\n    signature: \"0x%x\"\n", e2eNetuid, line, entry.Signature))
	if _, err := amendment.ParseVerified(e.amendments, e2eChainID, e2eNetuid, crypto.PubkeyToAddress(key.PublicKey)); err != nil {
		t.Fatal(err)
	}
}

// serveWeb serves the amendments, as ur.xyz does, and quicknet's pinned
// pulse, as a drand relay does.
func (e *ctfE2E) serveWeb(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/amendments.yml" {
		_, _ = writer.Write(e.amendments)
		return
	}
	if round, ok := strings.CutPrefix(request.URL.Path, "/"+crv4.QuicknetChainHash+"/public/"); ok && round == strconv.Itoa(ctfE2ERound) {
		signature, _ := hex.DecodeString(ctfE2ESignature)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"round":%d,"randomness":"%x","signature":"%s"}`, ctfE2ERound, sha256.Sum256(signature), ctfE2ESignature)
		return
	}
	http.NotFound(writer, request)
}

// pulse is quicknet's pulse of round, which must be the pinned one.
func (e *ctfE2E) pulse(round uint64) *ctf.Pulse {
	e.t.Helper()
	signature, _ := hex.DecodeString(ctfE2ESignature)
	pulse := &ctf.Pulse{Round: ctfE2ERound, Signature: signature}
	if round != ctfE2ERound {
		e.t.Fatalf("the draw needs quicknet round %d, and the test pins round %d: pin that round's signature", round, ctfE2ERound)
	}
	if err := pulse.Verify(); err != nil {
		e.t.Fatal(err)
	}
	return pulse
}

// bindHead binds client to a head fleet for epochs [from, to] through the
// v1 coordinator, as `provider fleet` does: the head hotkey holds a UID, the
// oracle mirrors its commitment, and the client's Ed25519 key and the
// hotkey's sr25519 key both sign the binding.
func (e *ctfE2E) bindHead(client [16]byte, from, to uint64) {
	f, t := e.ctfTestChain, e.t
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw(crypto.Keccak256Hash([]byte("urnetwork-e2e-ctf-head-hotkey")))
	if err != nil {
		t.Fatal(err)
	}
	secret, hotkey := mini.ExpandEd25519(), mini.Public().Encode()
	f.chain.native(func(rt *e2eRuntime) { rt.setWord(e2eNeuron, e2eRuntimeSlot("uid", hotkey), big.NewInt(8)) })
	commitment := crypto.Keccak256Hash([]byte("urnetwork-e2e-ctf-head-commitment"))
	head := f.chain.next() - 1
	f.must(f.oracle, f.coordinator, nil, e2eCoordinatorBinding.PackMirrorCommitment(hotkey, commitment, head, e2eBlockHash(head)))
	clientKey := ed25519.NewKeyFromSeed(crypto.Keccak256([]byte("urnetwork-e2e-ctf-head-client")))
	binding := stabi.STCoordinatorFleetBinding{ChainId: e2eChainID, Netuid: e2eNetuid, Coordinator: f.coordinator, FleetId: crypto.Keccak256Hash([]byte("urnetwork-e2e-ctf-fleet")),
		Hotkey: hotkey, ClientId: client, ClientKey: [32]byte(clientKey.Public().(ed25519.PublicKey)), Generation: 1, ValidFromEpoch: from, ValidToEpoch: to, CommitmentHash: commitment}
	digest := e2eView(f.e2eFixture, f.coordinator, e2eCoordinatorBinding.PackFleetBindingDigest(binding), e2eCoordinatorBinding.UnpackFleetBindingDigest)
	signed, err := secret.Sign(schnorrkel.NewSigningContext([]byte("substrate"), digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature := signed.Encode()
	receipt := f.must(f.anyone, f.coordinator, nil, e2eCoordinatorBinding.PackBindFleetMember(binding, ed25519.Sign(clientKey, digest[:]), hotkeySignature[:]))
	if len(receipt.Logs) != 1 {
		t.Fatalf("the binding logged %d events", len(receipt.Logs))
	}
	if event, err := e2eCoordinatorBinding.UnpackFleetBoundEvent(receipt.Logs[0]); err != nil || event.ClientId != client || event.Uid != 7 || event.ValidFromEpoch != from || event.ValidToEpoch != to {
		t.Fatalf("FleetBound %+v: %v", event, err)
	}
}

// heads is the auditor's head oracle at the head: the v1 coordinator's
// FleetBound events through it, and for each client whose validity covers
// the epoch, bindingAt at the epoch's start and end blocks, as the
// validator reads them (validator/settlement_v2_audit_chain.go).
func (e *ctfE2E) heads() ledger.HeadOracle {
	return ctfE2EHeads{e: e, block: e.chain.next() - 1}
}

type ctfE2EHeads struct {
	e     *ctfE2E
	block uint64
}

func (h ctfE2EHeads) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	f := h.e.ctfTestChain
	var candidates [][16]byte
	var failure error
	f.chain.do(func() {
		for _, log := range f.chain.logs {
			if log.Address != f.coordinator || log.BlockNumber > h.block || len(log.Topics) == 0 || log.Topics[0] != e2eFleetBoundID {
				continue
			}
			event, err := e2eCoordinatorBinding.UnpackFleetBoundEvent(log)
			if err != nil {
				failure = err
				return
			}
			if event.ValidFromEpoch <= epoch && epoch <= event.ValidToEpoch && !slices.Contains(candidates, event.ClientId) {
				candidates = append(candidates, event.ClientId)
			}
		}
	})
	bound := map[[16]byte]bool{}
	if failure != nil || len(candidates) == 0 {
		return bound, failure
	}
	start, end := f.epochStart(epoch), f.end(epoch)
	if start >= end || end > h.block {
		return nil, fmt.Errorf("epoch %d is not closed at block %d", epoch, h.block)
	}
	for _, block := range []uint64{start, end} {
		for _, client := range candidates {
			var out []byte
			var err error
			to := f.coordinator
			f.chain.do(func() {
				out, _, err = f.chain.callLocked(common.Address{}, &to, nil, e2eCoordinatorBinding.PackBindingAt(client, new(big.Int).SetUint64(epoch)), e2eTxGas, block)
			})
			if err != nil {
				return nil, err
			}
			binding, err := e2eCoordinatorBinding.UnpackBindingAt(out)
			if err != nil {
				return nil, err
			}
			bound[client] = bound[client] || binding.Active
		}
	}
	return bound, nil
}

// e2eFleetBoundID is the v1 coordinator's FleetBound topic.
var e2eFleetBoundID = func() common.Hash {
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		panic(err)
	}
	return parsed.Events["FleetBound"].ID
}()

// The CTF pool's vault2 state at the head.
func (e *ctfE2E) pending() stabi.ISTSettlementVault2Pending {
	return e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackPending(ctfTestPoolId), e2eVault2Binding.UnpackPending)
}

func (e *ctfE2E) rootCount() uint64 {
	return e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackRootCount(ctfTestPoolId), e2eVault2Binding.UnpackRootCount).Uint64()
}

func (e *ctfE2E) root(index uint64) stabi.ISTSettlementVault2Root {
	return e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackRoots(ctfTestPoolId, e2eRao(index)), e2eVault2Binding.UnpackRoots)
}

func (e *ctfE2E) fundedThrough(epoch uint64) *big.Int {
	return e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackFundedThrough(ctfTestPoolId, e2eRao(epoch)), e2eVault2Binding.UnpackFundedThrough)
}

func (e *ctfE2E) claimed(index uint64) *big.Int {
	return e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackClaimed(ctfTestPoolId, e2eRao(index)), e2eVault2Binding.UnpackClaimed)
}

func (e *ctfE2E) recycledFor(epoch uint64) *big.Int {
	return e2eView(e.e2eFixture, e.carry, e2eCtfCarryBinding.PackRecycledFor(e2eRao(epoch)), e2eCtfCarryBinding.UnpackRecycledFor)
}

// residual is the carry's own stake on vault2's escrow hotkey, which it
// never returns.
func (e *ctfE2E) residual() *big.Int {
	return e2eView(e.e2eFixture, e.carry, e2eCtfCarryBinding.PackResidual(), e2eCtfCarryBinding.UnpackResidual)
}

func (e *ctfE2E) census(epoch uint64) stabi.CtfCensusOutput {
	return e2eView(e.e2eFixture, e.settlement, e2eSettlementBinding.PackCtfCensus(big.NewInt(e2eNoID), e2eRao(epoch)), e2eSettlementBinding.UnpackCtfCensus)
}

// ctfError names a revert by STSettlement2Ctf's or STCtfCarry's errors.
func (e *ctfE2E) ctfError(revert []byte) string {
	if len(revert) >= 4 {
		for _, contract := range []*e2eArtifact{e.ctf, e2eForgeArtifact(e.t, "STCtfCarry")} {
			if failure, err := contract.abi.ErrorByID([4]byte(revert[:4])); err == nil {
				return failure.Name
			}
		}
	}
	return fmt.Sprintf("0x%x", revert)
}

// refusedCtf sends data from actor and requires the named error of the
// capture the flag contracts.
func (e *ctfE2E) refusedCtf(actor *e2eActor, to common.Address, data []byte, name string) {
	e.t.Helper()
	receipt, revert := e.chain.send(actor.key, &to, nil, data)
	if receipt.Status != types.ReceiptStatusFailed || e.ctfError(revert) != name {
		e.t.Fatalf("%s's call 0x%x: status %d revert %s, want %s", actor.name, data[:4], receipt.Status, e.ctfError(revert), name)
	}
}

// close lets epoch end and closes it for the CTF pool, as anyone may: the
// close moves everything vault2 holds on the CTF pool hotkey to its escrow,
// and funds the epoch with what escrow gained. It returns that funding.
func (e *ctfE2E) close(epoch uint64) *big.Int {
	e.t.Helper()
	if end := e.end(epoch); e.chain.next() < end {
		e.chain.rollTo(end)
	}
	observed := e.chain.stake(e.hotkey, e.vault2Coldkey)
	captured := new(big.Int).Sub(observed, big.NewInt(e.short))
	before := e.fundedThrough(epoch - 1)
	receipt := e.must(e.anyone, e.settlement, nil, e2eSettlementBinding.PackCloseOperatorEpoch(e2eRao(epoch), ctfTestPoolId))
	funded := new(big.Int).Add(before, captured)
	e.requireLogs(receipt, e.vault2Log("EmissionCaptured", e2eRao(epoch), ctfTestPoolId, observed, captured, funded))
	if e.fundedThrough(epoch).Cmp(funded) != 0 || e.chain.stake(e.hotkey, e.vault2Coldkey).Sign() != 0 {
		e.t.Fatalf("epoch %d funded through %s, want %s", epoch, e.fundedThrough(epoch), funded)
	}
	return captured
}

// commitCensus publishes the census's objects and commits its hash as the
// root signer in block B_k.
func (e *ctfE2E) commitCensus(census *ctf.Census, block uint64) *ctf.CensusObjects {
	e.t.Helper()
	objects, err := ctf.EncodeCensus(census)
	if err != nil {
		e.t.Fatal(err)
	}
	e.publisher.publish(objects.Objects, nil, nil)
	e.chain.rollTo(block)
	receipt := e.must(e.operator, e.settlement, nil, e2eSettlementBinding.PackCommitCtfCensus(e2eRao(census.Epoch), big.NewInt(e2eNoID), objects.Hash))
	event, err := e2eSettlementBinding.UnpackCtfCensusCommittedEvent(receipt.Logs[0])
	if len(receipt.Logs) != 1 || err != nil || event.NoId.Uint64() != e2eNoID || event.Epoch.Uint64() != census.Epoch || event.CensusHash != objects.Hash || event.BlockNumber != block || receipt.BlockNumber.Uint64() != block {
		e.t.Fatalf("CtfCensusCommitted %+v: %v", event, err)
	}
	if committed := e.census(census.Epoch); committed.CensusHash != objects.Hash || committed.BlockNumber != block {
		e.t.Fatalf("ctfCensus(1, %d) is %+v", census.Epoch, committed)
	}
	return objects
}

// build builds the CTF root of epoch over the latest finalized CTF root, as
// the server's driver does: each covered epoch's committed census with its
// pulse and the head bindings at its boundaries, recycledFor and vault2's
// funding.
func (e *ctfE2E) build(epoch uint64, censuses map[uint64]*ctf.CensusObjects) *ctf.Ledger {
	e.t.Helper()
	in := ctf.BuildInput{NoID: e2eNoID, ChainID: e2eChainID, Netuid: e2eNetuid, Epoch: epoch, FirstEpoch: ctfTestFirstEpoch, CarryColdkey: e.carryColdkey, FundedThrough: map[uint64]uint256.Int{}}
	p := uint64(ctfTestFirstEpoch - 1)
	if count := e.rootCount(); count > 0 {
		previous := e.ledgers[count-1]
		in.Previous = &ctf.Previous{Root: previous.Commit, Accounts: previous.Accounts, State: previous.State}
		p = previous.Commit.Epoch
	}
	heads := e.heads()
	for k := p + 1; k <= epoch; k++ {
		in.FundedThrough[k] = *uint256.MustFromBig(e.fundedThrough(k))
		input := ctf.EpochInput{Epoch: k, Recycled: *uint256.MustFromBig(e.recycledFor(k))}
		if committed := e.census(k); committed.CensusHash != ([32]byte{}) {
			objects := censuses[k]
			time := e2eBlockTime(committed.BlockNumber)
			bound, err := heads.HeadBindings(e.ctx, k)
			if err != nil {
				e.t.Fatal(err)
			}
			census, err := ctf.DecodeCensus(objects.Header, objects.Objects)
			if err != nil {
				e.t.Fatal(err)
			}
			coldkeys := map[[16]byte][32]byte{}
			for _, candidate := range census.Candidates {
				coldkeys[candidate.ClientID] = ctfE2EColdkey(candidate.ClientID)
			}
			input.Census = &ctf.CensusInput{Hash: committed.CensusHash, Block: committed.BlockNumber, Time: time, Objects: objects.Objects}
			input.Pulse, input.Bound, input.Coldkeys = e.pulse(ctf.DrawRound(time)), bound, coldkeys
		}
		in.Epochs = append(in.Epochs, input)
	}
	built, err := ctf.Build(e.ctx, in)
	if err != nil {
		e.t.Fatalf("CTF root of epoch %d: %v", epoch, err)
	}
	return built
}

// commit publishes the root's objects and commits it as the root signer of
// operator 1, checking DistributionCommitted.
func (e *ctfE2E) commit(built *ctf.Ledger) stabi.ISTSettlementVault2Pending {
	e.t.Helper()
	e.publisher.publish(built.Objects, built.Accounts, built.Tree)
	before, count := e.pending(), e.rootCount()
	commitment := built.Commit
	receipt := e.must(e.operator, e.settlement, nil, e2eSettlementBinding.PackCommitDistribution(e2eRao(commitment.Epoch), ctfTestPoolId, e2eRao(count),
		stabi.ISTSettlementVault2Commitment{Root: commitment.Root, Total: commitment.Total.ToBig(), LeafCount: commitment.LeafCount, LedgerHash: commitment.LedgerHash}))
	after := e.pending()
	e.requireLogs(receipt, e.vault2Log("DistributionCommitted", ctfTestPoolId, after.Seq, commitment.Epoch, commitment.Root, commitment.Total.ToBig(), commitment.LeafCount, commitment.LedgerHash, before.Seq))
	return after
}

// audit runs the validator's CTF audit of the pending root at the head:
// validator.SettlementV2CtfAuditInput reads its input through the
// settlement v2 audit's own readers (the entry in force in the signed file,
// the carry's pool and coldkey, each covered epoch's recycledFor, ctfCensus,
// its block's time and the pulse of its round from the relay, and its head
// oracle over the coordinator's FleetBound events and bindingAt), and
// ctf.Verify judges the published objects. Each drawn epoch's bindings by
// the validator's oracle must equal the builder's (heads). Without
// bindings, the draw leaves out no candidate, as a watcher that read none
// drew.
func (e *ctfE2E) audit(bindings bool) *ctf.Result {
	e.t.Helper()
	file, err := amendment.ParseVerified(e.amendments, e2eChainID, e2eNetuid, crypto.PubkeyToAddress(e.signer.PublicKey))
	if err != nil {
		e.t.Fatal(err)
	}
	in, err := validator.SettlementV2CtfAuditInput(e.ctx, e.validator, validator.SettlementV2Addresses{Settlement: e.settlement, Vault: e.vault2}, e2eChainID, e2eNetuid,
		e.epochStart(0), file, e.web.URL, e2eNoID)
	if err != nil {
		e.t.Fatal(err)
	}
	if in.CarryColdkey != e.carryColdkey || in.FirstEpoch != ctfTestFirstEpoch {
		e.t.Fatalf("the validator reads carry coldkey 0x%x and first epoch %d", in.CarryColdkey, in.FirstEpoch)
	}
	builder := e.heads()
	for k, facts := range in.Epochs {
		if facts.Census == nil {
			continue
		}
		validatorBound, err := in.Heads.HeadBindings(e.ctx, k)
		if err != nil {
			e.t.Fatal(err)
		}
		builderBound, err := builder.HeadBindings(e.ctx, k)
		if err != nil || !maps.Equal(validatorBound, builderBound) {
			e.t.Fatalf("epoch %d: the validator's head bindings %v, the builder's %v (%v)", k, validatorBound, builderBound, err)
		}
	}
	in.Fetcher, in.Custody, in.MaxFetchBytes = e.publisher, ledger.NewMemoryCustody(), ledger.DefaultMaxFetchBytes
	if !bindings {
		in.Heads = nil
	}
	result, err := ctf.Verify(e.ctx, *in)
	if err != nil {
		e.t.Fatal(err)
	}
	return result
}

// approve audits the pending root as the validator does and sends its
// approval through its sender.
func (e *ctfE2E) approve() *ctf.Result {
	e.t.Helper()
	result := e.audit(true)
	if result.Action() != ledger.ActionApprove {
		e.t.Fatalf("the validator refuses the pending CTF root: %v (%v check: %v)", result.Action(), result.Check, result.Err)
	}
	pending := e.pending()
	if err := e.sender.Approve(e.ctx, ctfE2EPool, pending); err != nil {
		e.t.Fatal(err)
	}
	sent := e.chain.sentBy(e.auditor.address)
	e.requireLogs(e.chain.receipt(sent[len(sent)-1].Hash()), e.settlementLog("DistributionApproved", ctfTestPoolId, pending.Seq, e.auditor.address, uint64(1), common.Big1))
	return result
}

// finalize lets the pending root's challenge window pass and finalizes it,
// as anyone may.
func (e *ctfE2E) finalize(built *ctf.Ledger) uint64 {
	e.t.Helper()
	pending := e.pending()
	if at := pending.CommittedBlock + e2eWindow; e.chain.next() < at {
		e.chain.rollTo(at)
	}
	index := e.rootCount()
	receipt := e.must(e.anyone, e.settlement, nil, e2eSettlementBinding.PackFinalizeDistribution(ctfTestPoolId))
	e.requireLogs(receipt, e.vault2Log("DistributionFinalized", ctfTestPoolId, pending.Seq, e2eRao(index), pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash))
	e.ledgers = append(e.ledgers, built)
	return index
}

// check runs snclaim v2 check of the CTF pool, as the CTF watcher does.
func (e *ctfE2E) check(flags ...string) (string, string, int) {
	e.t.Helper()
	return e.snclaim(e.snclaimArgs(append([]string{"v2", "check", "--all", fmt.Sprintf("--no_id=%d", ctfE2EPool),
		"--amendments_url=" + e.web.URL + "/amendments.yml", "--drand_url=" + e.web.URL}, flags...)...)...)
}

// recycle has anyone claim and recycle the carry account of the latest
// finalized CTF root through the carry, and checks what moved: the account
// is claimed in full, the carry keeps exactly its residual on the escrow
// hotkey and holds nothing on the pool hotkey, and vault2's position on the
// CTF pool hotkey grew by what it returned, recorded for the next unfunded
// epoch: the account's unclaimed amount, less the rao each of the claim, the
// carry's move and its transfer may credit short. With claimant set, the
// claimant first claims the carry account from vault2 itself, as anyone may,
// and the recycle claims nothing again.
func (e *ctfE2E) recycle(claimant *e2eActor) (uint64, *big.Int) {
	e.t.Helper()
	index := e.rootCount() - 1
	built := e.ledgers[index]
	carryIndex := e2eAccountOf(e.t, built.Accounts, e.carryColdkey)
	account, proof := e2eProof(e.t, built.Tree, built.Accounts, carryIndex)
	owed := new(big.Int).Sub(account.Cumulative, e.claimed(carryIndex))
	residual := e.residual()
	if residual.Sign() == 0 || e.chain.stake(e.escrowV2, e.carryColdkey).Cmp(residual) != 0 {
		e.t.Fatalf("before the recycle of root %d the carry holds %s on the escrow hotkey, its residual %s", index, e.chain.stake(e.escrowV2, e.carryColdkey), residual)
	}
	if claimant != nil {
		e.must(claimant, e.vault2, nil, e2eVault2Binding.PackClaim(ctfTestPoolId, e2eRao(index), account, proof))
		if paid := new(big.Int).Sub(e.chain.stake(e.escrowV2, e.carryColdkey), residual); e.claimed(carryIndex).Cmp(account.Cumulative) != 0 || paid.Cmp(new(big.Int).Sub(owed, big.NewInt(e.short))) != 0 {
			e.t.Fatalf("%s's claim of the carry account paid the carry %s of %s", claimant.name, paid, owed)
		}
	}
	before := e.chain.stake(e.hotkey, e.vault2Coldkey)
	epoch := e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackLastFundedEpoch(ctfTestPoolId), e2eVault2Binding.UnpackLastFundedEpoch).Uint64() + 1
	recorded := e.recycledFor(epoch)
	receipt := e.must(e.anyone, e.carry, nil, e2eCtfCarryBinding.PackRecycle(e2eRao(index), account, proof))
	var event *stabi.STCtfCarryRecycled
	for _, log := range receipt.Logs {
		if log.Address == e.carry {
			event, _ = e2eCtfCarryBinding.UnpackRecycledEvent(log)
		}
	}
	grown := new(big.Int).Sub(e.chain.stake(e.hotkey, e.vault2Coldkey), before)
	returned := new(big.Int).Sub(owed, big.NewInt(3*e.short))
	if event == nil || event.Epoch.Uint64() != epoch || event.Amount.Cmp(grown) != 0 || grown.Cmp(returned) != 0 || e.claimed(carryIndex).Cmp(account.Cumulative) != 0 ||
		e.recycledFor(epoch).Cmp(new(big.Int).Add(recorded, grown)) != 0 || e.residual().Cmp(residual) != 0 ||
		e.chain.stake(e.escrowV2, e.carryColdkey).Cmp(residual) != 0 || e.chain.stake(e.hotkey, e.carryColdkey).Sign() != 0 {
		e.t.Fatalf("recycle of root %d: event %+v, vault2 grew %s of %s owed, recycledFor(%d) %s, the carry keeps %s of its residual %s on the escrow hotkey",
			index, event, grown, owed, epoch, e.recycledFor(epoch), e.chain.stake(e.escrowV2, e.carryColdkey), residual)
	}
	// Nothing is left above the residual: another recycle, naming no
	// account, reverts.
	e.refusedCtf(e.anyone, e.carry, e2eCtfCarryBinding.PackRecycle(e2eRao(index), stabi.LedgerTreeAccount{Index: new(big.Int), Cumulative: new(big.Int)}, nil), "NothingToRecycle")
	return epoch, grown
}

// sweepResidual has root raise the nominator minimum past the residual's
// worth, and its sweep unstake the residual back to the carry's TAO
// (CAPTURETHEFLAG.md §4.6). Without it the escrow hotkey leaves the carry's
// list, and with third-party growth refused the runtime refuses vault2's
// claim of the carry account, the watcher's included; the recycle that would
// claim it reverts ResidualMissing instead, and the account stays claimable.
// The relay's answer is seed() with at least seedMinimum() less what the
// carry holds: a seed of the swept TAO alone is refused, SeedTooSmall. The
// seed follows the residual down to nothing and restores it, and the escrow
// hotkey with it.
func (e *ctfE2E) sweepResidual() {
	t := e.t
	t.Helper()
	index := e.rootCount() - 1
	built := e.ledgers[index]
	carryIndex := e2eAccountOf(t, built.Accounts, e.carryColdkey)
	account, proof := e2eProof(t, built.Tree, built.Accounts, carryIndex)
	residual, claimed := e.residual(), e.claimed(carryIndex)
	e.chain.setNominatorMinimum(ctfE2ERaisedMinimum)
	swept := e.chain.sweep(e.escrowV2, e.carryColdkey, e.carry)
	if swept.Cmp(residual) != 0 || e.chain.stake(e.escrowV2, e.carryColdkey).Sign() != 0 || slices.Contains(e.chain.stakingHotkeys(e.carryColdkey), e.escrowV2) {
		t.Fatalf("root's sweep returned %s of the residual %s, leaving %s on the escrow hotkey", swept, residual, e.chain.stake(e.escrowV2, e.carryColdkey))
	}
	if receipt, _ := e.chain.send(e.watcher.key, &e.vault2, nil, e2eVault2Binding.PackClaim(ctfTestPoolId, e2eRao(index), account, proof)); receipt.Status != types.ReceiptStatusFailed || e.claimed(carryIndex).Cmp(claimed) != 0 {
		t.Fatalf("vault2's claim of the carry account without its residual: status %d, claimed %s", receipt.Status, e.claimed(carryIndex))
	}
	e.refusedCtf(e.anyone, e.carry, e2eCtfCarryBinding.PackRecycle(e2eRao(index), account, proof), "ResidualMissing")
	minimum := e2eView(e.e2eFixture, e.carry, e2eCtfCarryBinding.PackSeedMinimum(), e2eCtfCarryBinding.UnpackSeedMinimum)
	if minimum.Cmp(e2eRao(10*ctfE2ERaisedMinimum)) != 0 {
		t.Fatalf("the least seed is %s after root raised the nominator minimum", minimum)
	}
	e.refusedCtf(e.anyone, e.carry, e2eCtfCarryBinding.PackSeed(), "SeedTooSmall")
	topUp := new(big.Int).Mul(new(big.Int).Sub(minimum, swept), big.NewInt(e2eRaoWei))
	receipt := e.must(e.anyone, e.carry, topUp, e2eCtfCarryBinding.PackSeed())
	var lowered *stabi.STCtfCarryResidualLowered
	var seeded *stabi.STCtfCarrySeeded
	for _, log := range receipt.Logs {
		if log.Address != e.carry {
			continue
		}
		if event, err := e2eCtfCarryBinding.UnpackResidualLoweredEvent(log); err == nil {
			lowered = event
		}
		if event, err := e2eCtfCarryBinding.UnpackSeededEvent(log); err == nil {
			seeded = event
		}
	}
	if lowered == nil || lowered.From.Cmp(residual) != 0 || lowered.To.Sign() != 0 || seeded == nil || seeded.Tao.Cmp(minimum) != 0 || seeded.Amount.Cmp(minimum) != 0 ||
		seeded.Residual.Cmp(minimum) != 0 || e.residual().Cmp(minimum) != 0 || e.chain.stake(e.escrowV2, e.carryColdkey).Cmp(minimum) != 0 ||
		!slices.Contains(e.chain.stakingHotkeys(e.carryColdkey), e.escrowV2) || e.claimed(carryIndex).Cmp(claimed) != 0 {
		t.Fatalf("seed after the sweep: lowered %+v, seeded %+v, residual %s, escrow position %s", lowered, seeded, e.residual(), e.chain.stake(e.escrowV2, e.carryColdkey))
	}
}

// ctfE2ECensus is epoch 3's census: three provider units in DE, two in IS,
// one of them ctfE2EBound's, and two extender units in US, each client in its
// own unit; a presence near miss in DE and an extender with no location.
func ctfE2ECensus(epoch uint64) *ctf.Census {
	candidate := func(role ctf.Role, country string, client byte) ctf.Candidate {
		code, _ := ctf.ParseCountry(country)
		unit := [32]byte{0: 0x0b, 31: client}
		return ctf.Candidate{Role: role, Country: code, Unit: unit, Family: ctf.FamilyIPv4, ClientID: ctfE2EClient(client)}
	}
	de, _ := ctf.ParseCountry("de")
	return &ctf.Census{NoID: e2eNoID, Epoch: epoch,
		Candidates: []ctf.Candidate{
			candidate(ctf.RoleProvider, "de", 1), candidate(ctf.RoleProvider, "de", 2), candidate(ctf.RoleProvider, "de", 3),
			candidate(ctf.RoleProvider, "is", 11), candidate(ctf.RoleProvider, "is", 12),
			candidate(ctf.RoleExtender, "us", 21), candidate(ctf.RoleExtender, "us", 22),
		},
		Excluded: []ctf.Exclusion{
			{Country: de, Role: ctf.RoleProvider, Reason: ctf.ReasonPresence, Count: 2},
			{Country: ctf.CountryUnknown, Role: ctf.RoleExtender, Reason: ctf.ReasonNoLocation, Count: 1},
		}}
}

// E-8: capture the flag's whole cycle on the e2e chain. The guardian makes
// vault2's coldkey a hotkey account; the proxy is upgraded to
// STSettlement2Ctf and UR's CTF pool registered from epoch 3 by the one
// folded timelock operation, its value paid from the timelock; the carry is
// created with its 1 TAO seed once the guardian holds its coldkey and the
// proxy's account is funded, ctf-carry review checking it before and after,
// and the signed entry names it. In epoch 3 the CTF pool earns
// 24.9 α; its census, which lists a client bound to a head fleet, is refused
// before its window and from anyone but the root signer, committed in it once
// and only once; the root drawn with quicknet's real pulse, leaving the bound
// client out, is approved by the validator's CTF audit, its input read by the
// settlement v2 audit's own readers and its approval sent by its sender, and
// snclaim v2 check approves it too, naming the client it left out, where
// without head bindings it would refuse it. After its window the
// root finalizes; snclaim v2 status lists a winner's CTF account, and the
// winner claims it through snclaim v2 claim; anyone claims and recycles the
// carry account through STCtfCarry, which keeps its residual, recorded for
// epoch 4. Epoch 4's census window passes without one, and a census after it
// is refused: everything carries, and its root returns the carry, matured;
// the audit and snclaim, with the previous root's accounts, approve it, and it
// finalizes. Root's sweep then clears the residual: vault2's claim of the
// carry account is refused, the recycle reverts ResidualMissing, and a seed
// of at least the raised least restores it. Once the watcher has claimed the
// carry account from vault2 itself the carry recycles what it holds above
// the residual for epoch 5. vault2's conservation holds across both pools. The cycle runs with exact stake moves, and again
// with every move from epoch 3 on crediting one rao short, as the runtime's
// share pool can: a recycle returns three rao less than the carry account,
// the next capture one less, and the root still matures within the walk's
// tolerance (CAPTURETHEFLAG.md §4.5, §4.6).
func TestSettlementV2E2ECaptureTheFlag(t *testing.T) {
	for _, short := range []int64{0, 1} {
		t.Run(fmt.Sprintf("moves %d rao short", short), func(t *testing.T) {
			e := newCtfE2E(t)
			e.short = short
			e.chain.do(func() { e.chain.runtime.shortfall = short == 1 })
			e.cycle()
		})
	}
}

// cycle runs E-8 from epoch 3's emission.
func (e *ctfE2E) cycle() {
	t := e.t
	short := big.NewInt(e.short)
	less := func(amount *big.Int, times int64) *big.Int {
		return new(big.Int).Sub(amount, new(big.Int).Mul(short, big.NewInt(times)))
	}

	// Epoch 3: the CTF pool earns, and the census window opens at end(3).
	e.chain.emit(e.hotkey, e.vault2Coldkey, e2eRao(ctfE2EEmission3))
	end := e.end(ctfTestFirstEpoch)
	if ctfE2ECensusBlock < end || ctfE2ECensusBlock > end+e2eCommitWindow/2 {
		t.Fatalf("B_3 %d is outside the census window [%d, %d]: pin another block and its round", ctfE2ECensusBlock, end, end+e2eCommitWindow/2)
	}
	census := ctfE2ECensus(ctfTestFirstEpoch)
	encoded, err := ctf.EncodeCensus(census)
	if err != nil {
		t.Fatal(err)
	}
	commitData := e2eSettlementBinding.PackCommitCtfCensus(e2eRao(ctfTestFirstEpoch), big.NewInt(e2eNoID), encoded.Hash)
	e.chain.rollTo(end - 1)
	e.refusedCtf(e.operator, e.settlement, commitData, "InvalidWindow")
	funded3 := e.close(ctfTestFirstEpoch)
	if funded3.Cmp(less(e2eRao(ctfE2EEmission3), 1)) != 0 {
		t.Fatalf("the close funded %s", funded3)
	}
	e.refusedCtf(e.anyone, e.settlement, commitData, "Unauthorized")
	e.refusedCtf(e.operator, e.settlement, e2eSettlementBinding.PackCommitCtfCensus(e2eRao(ctfTestFirstEpoch), big.NewInt(e2eNoID), [32]byte{}), "InvalidCensusHash")
	objects := e.commitCensus(census, ctfE2ECensusBlock)
	e.refusedCtf(e.operator, e.settlement, commitData, "CensusAlreadyCommitted")

	// The root: drawn with ρ_3's pulse, the bound client left out.
	censuses := map[uint64]*ctf.CensusObjects{ctfTestFirstEpoch: objects}
	first := e.build(ctfTestFirstEpoch, censuses)
	three := first.Epochs[0]
	if !three.Drawn || three.Round != ctfE2ERound || len(three.Bound) != 1 || three.Bound[0].ClientID != ctfE2EBound || len(three.Awards) != 6 ||
		three.SlotBase.ToBig().Cmp(new(big.Int).Div(funded3, big.NewInt(ctf.NumSlots))) != 0 {
		t.Fatalf("epoch 3 drawn %t round %d, bound %v, %d awards, slot %s", three.Drawn, three.Round, three.Bound, len(three.Awards), three.SlotBase.Dec())
	}
	for _, award := range three.Awards {
		if award.ClientID == ctfE2EBound {
			t.Fatal("the bound client won")
		}
	}
	e.commit(first)

	// The validator approves what reproduces with the chain's bindings;
	// without them the draw keeps the bound client and the root is refused,
	// which is what a watcher without bindings would answer.
	if blind := e.audit(false); blind.Action() != ledger.ActionRefuse || blind.Check != ctf.CheckDraw {
		t.Fatalf("an audit without head bindings: %v (%v check: %v)", blind.Action(), blind.Check, blind.Err)
	}
	audited := e.approve()
	if bound := audited.Epochs[0].Bound; len(bound) != 1 || bound[0].ClientID != ctfE2EBound {
		t.Fatalf("the audit left out %v", bound)
	}

	// snclaim agrees with the validator: it reads the same bindings, leaves
	// the client out, names it, and approves.
	stdout, stderr, code := e.check("--challenge", "--demand", "--key_file="+e.watcher.keyFile(t))
	heads := fmt.Sprintf("  head bindings:     1 FleetBound events of v1 coordinator %s from block %d\n", e.coordinator.Hex(), e.epochStart(0))
	left := fmt.Sprintf("                     left out 1 head-bound candidates: 0x%x (provider, is)\n", ctfE2EBound)
	drawn := fmt.Sprintf("  epoch 3:           census %v at block %d, 7 candidates; drand round %d, 6 winners\n", objects.Hash, ctfE2ECensusBlock, ctfE2ERound)
	if code != 0 || !strings.Contains(stdout, heads) || !strings.Contains(stdout, drawn+left) || !strings.HasSuffix(stdout, "  audit:             approve\n") || len(e.chain.sentBy(e.watcher.address)) != 0 {
		t.Fatalf("snclaim v2 check of the first CTF root: exit %d\n%s%s", code, stdout, stderr)
	}
	t.Logf("snclaim v2 check of the first CTF root:\n%s", stdout)

	// After its window the root finalizes, and a winner claims through
	// snclaim, paid on vault2's escrow hotkey.
	e.finalize(first)
	if e.pending().Seq != 0 || e.rootCount() != 1 {
		t.Fatal("the CTF root did not finalize")
	}
	winner := three.Awards[0]
	index := e2eAccountOf(t, first.Accounts, winner.Coldkey)
	cumulative := first.Accounts[index].Cumulative.ToBig()
	stdout, stderr, code = e.snclaim(e.snclaimArgs("v2", "status", fmt.Sprintf("--coldkey=0x%x", winner.Coldkey))...)
	if code != 0 || !strings.Contains(stdout, fmt.Sprintf("  pool %d, account %d:\n", ctfE2EPool, index)) || !strings.Contains(stdout, fmt.Sprintf(" (%s rao) at root 0\n", cumulative)) {
		t.Fatalf("snclaim v2 status of the winner: exit %d\n%s%s", code, stdout, stderr)
	}
	stdout, stderr, code = e.snclaim(e.snclaimArgs("v2", "claim", fmt.Sprintf("--coldkey=0x%x", winner.Coldkey), fmt.Sprintf("--no_id=%d", ctfE2EPool), "--key_file="+e.anyone.keyFile(t))...)
	if code != 0 || e.claimed(index).Cmp(cumulative) != 0 || e.chain.stake(e.escrowV2, winner.Coldkey).Cmp(less(cumulative, 1)) != 0 {
		t.Fatalf("snclaim v2 claim of the winner's CTF account: exit %d, claimed %s of %s\n%s%s", code, e.claimed(index), cumulative, stdout, stderr)
	}

	// Anyone recycles the carry: vault2's next close captures it. It returns
	// what the root owed the pots and pending awards, less the moves'
	// rounding, and keeps its residual.
	returned4, amount := e.recycle(nil)
	if owed := first.State.Obligations(); returned4 != ctfTestFirstEpoch+1 || amount.Cmp(less(owed.ToBig(), 3)) != 0 {
		t.Fatalf("recycled %s for epoch %d, the pots and pending awards are %s", amount, returned4, owed.Dec())
	}

	// Epoch 4: fresh emission and the returned carry. Its census window
	// passes without a census, which is then refused, so there is no draw and
	// everything carries (§4.7). The carry is back within the walk's
	// tolerance: what the moves lost is written off, and the fresh amount is
	// the funding less what was owed.
	e.chain.emit(e.hotkey, e.vault2Coldkey, e2eRao(ctfE2EEmission4))
	funded4 := e.close(ctfTestFirstEpoch + 1)
	e.chain.rollTo(e.end(ctfTestFirstEpoch+1) + e2eCommitWindow/2 + 1)
	e.refusedCtf(e.operator, e.settlement, e2eSettlementBinding.PackCommitCtfCensus(e2eRao(ctfTestFirstEpoch+1), big.NewInt(e2eNoID), encoded.Hash), "InvalidWindow")
	if funded4.Cmp(less(new(big.Int).Add(amount, e2eRao(ctfE2EEmission4)), 1)) != 0 {
		t.Fatalf("the close of epoch 4 funded %s", funded4)
	}
	second := e.build(ctfTestFirstEpoch+1, censuses)
	four, owed := second.Epochs[0], first.State.Obligations()
	if paid := four.Paid(); four.Drawn || four.Census != nil || !four.Matured || four.Owed.Cmp(&owed) != 0 || four.Returned.ToBig().Cmp(amount) != 0 ||
		four.Fresh.ToBig().Cmp(new(big.Int).Sub(funded4, owed.ToBig())) != 0 || !paid.IsZero() {
		t.Fatalf("epoch 4: drawn %t, matured %t, returned %s of %s owed, fresh %s, paid %s", four.Drawn, four.Matured, four.Returned.Dec(), four.Owed.Dec(), four.Fresh.Dec(), paid.Dec())
	}
	if pending := e.commit(second); pending.Seq != 2 {
		t.Fatalf("the second CTF root is seq %d", pending.Seq)
	}
	e.approve()
	stdout, stderr, code = e.check("--challenge", "--demand", "--key_file="+e.watcher.keyFile(t))
	if code != 0 || !strings.Contains(stdout, "  epoch 4:           no census committed: no draw\n") || strings.Contains(stdout, "head bindings") ||
		!strings.HasSuffix(stdout, fmt.Sprintf("  audit:             approve\n  previous accounts: %d of root 0, no fault\n", len(first.Accounts))) {
		t.Fatalf("snclaim v2 check of the second CTF root: exit %d\n%s%s", code, stdout, stderr)
	}
	t.Logf("snclaim v2 check of the second CTF root:\n%s", stdout)
	// Root's sweep clears the residual, which seed() restores. The watcher
	// then claims the carry account from vault2 first; the recycle claims
	// nothing again and returns what the carry holds above its residual.
	e.finalize(second)
	e.sweepResidual()
	if returned5, again := e.recycle(e.watcher); returned5 != ctfTestFirstEpoch+2 || again.Cmp(less(funded4, 3)) != 0 {
		t.Fatalf("recycled %s for epoch %d", again, returned5)
	}
	e.requireConservation()
}

// requireConservation checks G1 on vault2 across both of operator 1's pools:
// what was captured is paid or accounted, each pool's captured less its
// claims is its share of what is accounted, no pool's claims exceed its
// finalized roots or those its captures, and vault2's escrow position covers
// what it accounts.
func (e *ctfE2E) requireConservation() {
	e.t.Helper()
	if !e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackConservationHolds(), e2eVault2Binding.UnpackConservationHolds) {
		e.t.Fatal("vault2 conservation does not hold")
	}
	captured, paid, accounted := e.vault2Counter(e2eVault2Binding.PackTotalCaptured), e.vault2Counter(e2eVault2Binding.PackTotalPaid), e.vault2Counter(e2eVault2Binding.PackEscrowAccounted)
	held := new(big.Int)
	for _, pool := range []*big.Int{big.NewInt(e2eNoID), ctfTestPoolId} {
		poolCaptured := e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackCapturedTotal(pool), e2eVault2Binding.UnpackCapturedTotal)
		poolClaimed := e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackClaimedTotal(pool), e2eVault2Binding.UnpackClaimedTotal)
		finalized := e2eView(e.e2eFixture, e.vault2, e2eVault2Binding.PackFinalizedTotal(pool), e2eVault2Binding.UnpackFinalizedTotal)
		if poolClaimed.Cmp(finalized) > 0 || finalized.Cmp(poolCaptured) > 0 {
			e.t.Fatalf("pool %s: captured %s, finalized %s, claimed %s", pool, poolCaptured, finalized, poolClaimed)
		}
		held.Add(held, new(big.Int).Sub(poolCaptured, poolClaimed))
	}
	if new(big.Int).Add(paid, accounted).Cmp(captured) != 0 || held.Cmp(accounted) != 0 || e.chain.stake(e.escrowV2, e.vault2Coldkey).Cmp(accounted) < 0 {
		e.t.Fatalf("G1: captured %s paid %s accounted %s, the pools hold %s", captured, paid, accounted, held)
	}
}
