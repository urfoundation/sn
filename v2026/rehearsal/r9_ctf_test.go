//go:build runtime_rehearsal

package rehearsal

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/merkle"
)

const (
	// ctfPoolBase is STSettlement2Ctf's CTF_POOL_BASE: operator n's CTF pool
	// is vault2 pool 2^32 + n (CAPTURETHEFLAG.md §3.1).
	ctfPoolBase = 1 << 32
	// ctfCensusSlot is the slot of STSettlement2Ctf's census mapping, keyed by
	// operator, then epoch, after phase C's slots 0 to 50
	// (evm/layout/STSettlement2Ctf.json).
	ctfCensusSlot = 51
	// thirdPartyStakingHotkeys is the runtime's MAX_THIRD_PARTY_STAKING_HOTKEYS:
	// a transfer from another coldkey adds a hotkey to a coldkey's
	// StakingHotkeys list only while the list holds fewer entries.
	thirdPartyStakingHotkeys = 128
	// carrySeed is the TAO, in rao, the carry contract stakes as its residual
	// at creation: 1 TAO, as on mainnet (CAPTURETHEFLAG.md §11.2).
	carrySeed = tao
	// existentialDeposit is the runtime's, which a contract's account keeps:
	// its EVM balance is the balance above it.
	existentialDeposit = 500
)

// R-9. TestR9CaptureTheFlag: the capture the flag upgrade of STSettlement2
// and the carry contract on the real runtime (CAPTURETHEFLAG.md §3.1, §4.6,
// §11.2). As on mainnet, the proxy runs phase C first. Anyone creates
// STSettlement2Ctf through the deterministic deployment proxy's code, and the
// owner, standing in for the timelock, sends the one folded call:
// upgradeToAndCall(implementation, registerCtfPool(1, maximumBurn, 1)), paying
// the burn ceiling. The pool hotkey is derived from the block and its parent's
// hash, which qualifies BLOCKHASH on the runtime. Operator 1's root signer
// commits epoch 1's census once, inside its window.
//
// A guardian coldkey makes vault2's coldkey a hotkey account first, so no
// coldkey swap can merge into it or reserve it, and everything runs with it.
// The CTF pool then runs a carry cycle (§4.6, "Qualifying it"). Its epoch 1
// closes, and its root pays the carry contract's coldkey, which is named
// before the contract exists. The guardian makes that coldkey a hotkey account
// too, the proxy's account gets TAO above its existential deposit, and a third
// party fills the coldkey's staking-hotkey list to 128 entries. Anyone then
// creates STCtfCarry through the same proxy with its seed, which its
// constructor stakes on the escrow hotkey as its residual before pinning the
// pool hotkey as its auto-stake destination and setting the runtime's
// opt-outs. No third party can add an entry after that, nor a coldkey swap
// reach it. A stranger's recycle claims the account to the contract's
// coldkey, moves the stake above the residual onto the pool hotkey and
// transfers it to vault2, and epoch 2's close captures it. Last, the delays
// the residual cannot stop are tried, each until it ends: root's sweep of
// the residual, answered by seed(); a reservation of either coldkey, refused;
// and vault2's own list held full, answered by absorbStake.
func TestR9CaptureTheFlag(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	// Every epoch from 1 has the shortest cadence STSettlement2 accepts for a
	// pool's first epoch, 14,410 blocks, so the CTF pool's first epoch is 1.
	// A root committed in an epoch's window finalizes before the next epoch
	// ends, as on mainnet, so each carry cycle returns before the next close.
	// Operator 2 is an active v1 operator without a provider pool.
	s := deployStack(t, n, stackConfig{operators: 2})
	operator := s.operators[1]
	// The guardian, a coldkey that never swaps the hotkeys it holds, makes
	// vault2's coldkey a hotkey account, as mainnet should before the
	// operation: no coldkey swap can then merge into vault2's coldkey, fill
	// its staking-hotkey list for good, or reserve it. Everything below runs
	// with it.
	guardian := newColdkey(t)
	n.fund(t, guardian.PublicKey(), 10*tao)
	n.submit(t, guardian.Ring, n.call(t, "SubtensorModule.try_associate_hotkey", accountID(t, s.vaultColdkey)))
	if n.owner(t, s.vaultColdkey) != guardian.PublicKey() {
		t.Fatal("vault2's coldkey is not the guardian's hotkey")
	}
	// Epochs stop (tempo 0), so no dividend reaches a position between reads.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(s.netuid), types.NewU16(0)))
	relayer, stranger := newEVMKey(t), newEVMKey(t)
	n.fundEVM(t, relayer.address, 1_000*tao)
	n.fundEVM(t, stranger.address, 1_000*tao)
	phaseC := forgeArtifact(t, "src/STSettlement2PhaseC.sol", "STSettlement2PhaseC")
	ctf := forgeArtifact(t, "src/STSettlement2Ctf.sol", "STSettlement2Ctf")
	carryArtifact := forgeArtifact(t, "src/STCtfCarry.sol", "STCtfCarry")
	const firstEpoch uint64 = 1
	const ctfPool uint64 = ctfPoolBase + 1
	nu := id(uint64(s.netuid))

	var factory common.Address
	var hotkey [32]byte
	t.Run("upgrade", func(t *testing.T) {
		// Phase C, the implementation mainnet runs, installed as R-8 installs it.
		factory = deployCreate2Factory(t, n, s.deployer)
		phaseCImplementation, phaseCCreated := create2(t, n, relayer, factory, phaseC.creation)
		phaseCUpgrade := n.send(t, s.owner, s.settlement, phaseC, nil, "upgradeToAndCall", phaseCImplementation, []byte{})
		requireEvent(t, phaseCUpgrade, phaseC, s.settlement, "Upgraded", map[string]any{"implementation": phaseCImplementation})

		// Anyone creates the CTF implementation through the same proxy. Its
		// own initializers are disabled.
		implementation, created := create2(t, n, relayer, factory, ctf.creation)
		code, err := n.eth.CodeAt(n.ctx, implementation, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := n.refused(t, relayer, implementation, ctf, nil, "initialize", relayer.address, s.v1, s.vault, []common.Address{relayer.address}, big.NewInt(1)); got != "InvalidInitialization" {
			t.Fatalf("initialize on the implementation refused with %s", got)
		}

		// The timelock's one operation (§3.1, §11.2): the upgrade, with the
		// registration as its call and the burn ceiling as its value. Only
		// the owner may send it.
		register := ctf.pack(t, "registerCtfPool", id(1), uint64(maximumBurn), firstEpoch)
		if got := n.refused(t, stranger, s.settlement, ctf, raoWei(maximumBurn), "upgradeToAndCall", implementation, register); got != "OwnableUnauthorizedAccount" {
			t.Fatalf("a stranger's upgrade refused with %s", got)
		}
		before := s.settlementSlots(t)
		economics, err := chain.ReadRegistrationEconomicsAtContext(n.ctx, n.sub, s.netuid, n.head(t))
		if err != nil {
			t.Fatal(err)
		}
		balance, mapped, proxyBalance := n.balance(t, s.owner.address), n.freeBalance(t, s.vaultColdkey), n.balance(t, s.settlement)
		receipt := n.send(t, s.owner, s.settlement, ctf, raoWei(maximumBurn), "upgradeToAndCall", implementation, register)
		requireEvent(t, receipt, ctf, s.settlement, "Upgraded", map[string]any{"implementation": implementation})
		after := s.settlementSlots(t)
		if common.BytesToAddress(after[implementationSlot].Bytes()) != implementation {
			t.Fatalf("implementation slot holds %s", after[implementationSlot])
		}
		for slot, value := range before {
			if slot != implementationSlot && after[slot] != value {
				t.Fatalf("slot %s changed from %s to %s", slot, value, after[slot])
			}
		}

		// The pool hotkey is keccak256(abi.encode(proxy, 2^32 + 1, block,
		// blockhash(block - 1))), with the parent's hash as the node reports it.
		block := receipt.BlockNumber.Uint64()
		parent, _ := n.ethBlock(t, block-1)
		if _, named := n.ethBlock(t, block); named != parent {
			t.Fatalf("block %d names parent %s, but block %d's hash is %s", block, named, block-1, parent)
		}
		var substrateParent string
		if err := n.sub.API.Client.CallContext(n.ctx, &substrateParent, "chain_getBlockHash", block-1); err != nil {
			t.Fatal(err)
		}
		record := s.pool(t, ctfPool)
		hotkey = record.Hotkey
		switch {
		case parent == (common.Hash{}):
			t.Fatalf("the node reports a zero hash for block %d", block-1)
		case hotkey == ctfHotkey(s.settlement, ctfPool, block, parent):
		case hotkey == ctfHotkey(s.settlement, ctfPool, block, common.Hash{}):
			t.Errorf("BLOCKHASH RETURNED ZERO: in block %d, blockhash(block.number - 1) read 0, not %s. The CTF pool hotkey %x "+
				"is keccak256(proxy, pool, block, 0), which anyone can compute for any block before it, so registerCtfPool's "+
				"derivation does not hide the hotkey on this runtime (CAPTURETHEFLAG.md §3.1)", block, parent, hotkey)
		case hotkey == ctfHotkey(s.settlement, ctfPool, block, common.HexToHash(substrateParent)):
			t.Errorf("in block %d, blockhash(block.number - 1) read the Substrate block hash %s, not the Ethereum hash %s that eth_getBlockByNumber reports",
				block, substrateParent, parent)
		default:
			t.Fatalf("vault2 registered hotkey %x, which no parent hash of block %d derives", hotkey, block)
		}
		uid, registered := n.uid(t, s.netuid, hotkey)
		if !record.Registered || record.FirstEpoch != firstEpoch || !registered || n.owner(t, hotkey) != s.vaultColdkey ||
			s.vaultUint(t, "lastFundedEpoch", id(ctfPool)).Uint64() != firstEpoch-1 {
			t.Fatalf("pools(2^32 + 1) = %+v; UID %d registered %v", record, uid, registered)
		}
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "PoolRegistered", map[string]any{
			"noId": id(ctfPool), "hotkey": hotkey, "uid": uid, "firstEpoch": firstEpoch,
		})
		// The burn decays in the registration's own block, so the owner pays
		// at most the burn read before it, and gets the rest of the ceiling
		// back; vault2 and the proxy keep nothing.
		paid := new(big.Int).Sub(balance, n.balance(t, s.owner.address))
		paid.Sub(paid, fee(receipt))
		if paid.Sign() <= 0 || paid.Cmp(raoWei(economics.BurnRao)) > 0 || n.freeBalance(t, s.vaultColdkey) != mapped ||
			n.balance(t, s.settlement).Cmp(proxyBalance) != 0 {
			t.Fatalf("the owner paid %s wei beyond the fee for a burn of at most %d rao; vault2's account went %d -> %d; the proxy's %s -> %s",
				paid, economics.BurnRao, mapped, n.freeBalance(t, s.vaultColdkey), proxyBalance, n.balance(t, s.settlement))
		}

		// Reads through the new implementation.
		resolved := n.read(t, s.settlement, ctf, "ctfSettlementAt", id(1), id(firstEpoch))
		early := n.read(t, s.settlement, ctf, "ctfSettlementAt", id(1), id(firstEpoch-1))
		general := n.read(t, s.settlement, ctf, "settlementAt", id(ctfPool), id(firstEpoch))
		if n.read(t, s.settlement, ctf, "owner")[0].(common.Address) != s.owner.address ||
			n.read(t, s.settlement, ctf, "vault")[0].(common.Address) != s.vault ||
			n.read(t, s.settlement, ctf, "ctfPoolId", id(1))[0].(*big.Int).Uint64() != ctfPool ||
			resolved[0].(common.Address) != s.vault || resolved[1].([32]byte) != hotkey || resolved[2].(uint64) != firstEpoch ||
			early[0].(common.Address) != (common.Address{}) || early[1].([32]byte) != [32]byte{} || early[2].(uint64) != 0 ||
			general[0].(common.Address) != s.vault || general[1].([32]byte) != hotkey || general[2].(uint8) != 2 {
			t.Fatalf("views through the CTF implementation: ctfSettlementAt %v, before its first epoch %v; settlementAt %v", resolved, early, general)
		}

		// After the upgrade: an operator without a provider pool, and
		// operator 1's CTF pool a second time, whatever hotkey a later block
		// derives.
		if got := n.refused(t, s.owner, s.settlement, ctf, raoWei(maximumBurn), "registerCtfPool", id(2), uint64(maximumBurn), firstEpoch); got != "UnknownPool" {
			t.Fatalf("operator 2's CTF pool, without a provider pool, refused with %s", got)
		}
		if got := n.refused(t, s.owner, s.settlement, ctf, raoWei(maximumBurn), "registerCtfPool", id(1), uint64(maximumBurn), firstEpoch); got != "InvalidConfiguration" {
			t.Fatalf("a second registration of operator 1's CTF pool refused with %s", got)
		}
		if s.pool(t, ctfPoolBase+2).Registered || s.pool(t, ctfPool).Hotkey != hotkey {
			t.Fatal("a refused registration changed vault2's pools")
		}
		t.Logf("phase C: CREATE2 gas %d; upgradeToAndCall(implementation, 0x) gas %d", phaseCCreated.GasUsed, phaseCUpgrade.GasUsed)
		t.Logf("STSettlement2Ctf: CREATE2 of %d runtime bytes, gas %d; the folded upgradeToAndCall, %d calldata bytes with a value of %d rao: gas %d",
			len(code), created.GasUsed, len(ctf.pack(t, "upgradeToAndCall", implementation, register)), uint64(maximumBurn), receipt.GasUsed)
		t.Logf("registerCtfPool: pool %d registered from epoch %d under hotkey %x, UID %d, derived in block %d from its parent's hash %s; "+
			"the owner paid a burn of %s wei of the %d rao ceiling and got the rest back", ctfPool, firstEpoch, hotkey, uid, block, parent, paid, uint64(maximumBurn))
	})

	// Epoch 1's emission reaches the CTF pool hotkey. Anyone may create the
	// carry contract through the deterministic deployment proxy (§11.2). Here
	// it is created with the zero salt, so its address, and the coldkey that
	// roots pay, are known before it exists: the worst case for griefing it.
	// On mainnet the salt is private until the creation.
	carryCreation := withArgs(t, carryArtifact, s.vault, id(ctfPool))
	carry := crypto.CreateAddress2(factory, [32]byte{}, crypto.Keccak256(carryCreation))
	carryColdkey := mirror(carry)
	names := map[[32]byte]string{s.escrow: "escrow", hotkey: "CTF pool", operator.pool: "provider pool"}
	n.stakeFor(t, alice, s.vaultColdkey, hotkey, s.netuid, 200*tao)
	settings := forgeArtifact(t, "src/interfaces/stakingAccountSettings.sol", "IStakingAccountSettings")
	donor := newColdkey(t)
	n.fund(t, donor.PublicKey(), 1_000*tao)
	// fill has a third party add hotkeys of its own to coldkey's StakingHotkeys
	// list until it holds `to` entries: each it associates, stakes on and
	// transfers to coldkey. It returns the last hotkey it added, and the
	// runtime's refusal that stopped it, if one did.
	fill := func(t *testing.T, coldkey [32]byte, to int) ([32]byte, error) {
		t.Helper()
		var last [32]byte
		for {
			count := len(n.stakingHotkeys(t, coldkey))
			if count >= to {
				return last, nil
			}
			var calls []types.Call
			var added [32]byte
			for range min(8, to-count) {
				third := newAccount(t)
				calls = append(calls, n.call(t, "SubtensorModule.try_associate_hotkey", accountID(t, third)),
					n.addStakeCall(t, third, s.netuid, 3_000_000), n.transferStakeCall(t, coldkey, third, s.netuid, wholePosition))
				added = third
			}
			if _, err := n.trySubmit(t, donor.Ring, n.batch(t, calls...)); err != nil {
				return last, err
			}
			last = added
		}
	}

	// census stands for the sha256 of epoch 1's census header (§4.3).
	census := sha256.Sum256([]byte(`{"schema":"urnetwork-ctf-census-v1","no_id":1,"pool_id":4294967297,"epoch":1}`))
	other := sha256.Sum256([]byte(`{"schema":"urnetwork-ctf-census-v1","no_id":1,"pool_id":4294967297,"epoch":1,"candidates":1}`))
	end := s.end(t, firstEpoch)

	// R-9: operator 1's root signer commits epoch 1's census hash once,
	// inside [end(1), end(1) + window/2], before the CTF pool's root (§4.3).
	t.Run("census", func(t *testing.T) {
		// Before the window: the eth_call at end(1) - 2 and the transaction in
		// end(1) - 1.
		n.sealUntil(t, end-2)
		if got := n.refused(t, operator.rootSigner, s.settlement, ctf, nil, "commitCtfCensus", id(firstEpoch), id(1), census); got != "InvalidWindow" {
			t.Fatalf("a census before its window refused with %s", got)
		}
		n.sealUntil(t, end)
		receipt, estimate, least := n.sendLeast(t, operator.rootSigner, s.settlement, ctf, nil, "commitCtfCensus", id(firstEpoch), id(1), census)
		committed := receipt.BlockNumber.Uint64()
		requireEvent(t, receipt, ctf, s.settlement, "CtfCensusCommitted", map[string]any{
			"noId": id(1), "epoch": id(firstEpoch), "censusHash": census, "blockNumber": committed,
		})
		if read := n.read(t, s.settlement, ctf, "ctfCensus", id(1), id(firstEpoch)); read[0].([32]byte) != census || read[1].(uint64) != committed {
			t.Fatalf("ctfCensus(1, 1) reads %v, want the hash and block %d", read, committed)
		}
		// The census lands in slot 51's mapping, after phase C's slots.
		entry := ctfCensusEntry(1, firstEpoch)
		stored := n.storageWord(t, s.settlement, entry)
		storedBlock := n.storageWord(t, s.settlement, common.BigToHash(new(big.Int).Add(entry.Big(), big.NewInt(1))))
		if stored != common.Hash(census) || storedBlock.Big().Uint64() != committed {
			t.Fatalf("slot 51's entry for (1, 1) holds %s and %s", stored, storedBlock)
		}
		if got := n.refused(t, operator.rootSigner, s.settlement, ctf, nil, "commitCtfCensus", id(firstEpoch), id(1), other); got != "CensusAlreadyCommitted" {
			t.Fatalf("a second census refused with %s", got)
		}
		if read := n.read(t, s.settlement, ctf, "ctfCensus", id(1), id(firstEpoch)); read[0].([32]byte) != census || read[1].(uint64) != committed {
			t.Fatalf("after a refused second census ctfCensus(1, 1) reads %v", read)
		}
		t.Logf("commitCtfCensus in block %d, end(1) + %d: gas %d, least limit %d, eth_estimateGas limit %d", committed, committed-end, receipt.GasUsed, least, estimate)
	})

	// capture closes epoch for the CTF pool and requires that vault2's movable
	// position on its hotkey move into escrow, which gains it or one rao less,
	// recorded exactly as the epoch's funding.
	capture := func(t *testing.T, epoch uint64) {
		t.Helper()
		lock := s.locked(t, hotkey)
		observed := n.stake(t, hotkey, s.vaultColdkey, s.netuid) - lock
		escrow := n.stake(t, s.escrow, s.vaultColdkey, s.netuid)
		captured := s.vaultUint(t, "capturedTotal", id(ctfPool)).Uint64()
		receipt := s.close(t, epoch, ctfPool)
		gained := n.stake(t, s.escrow, s.vaultColdkey, s.netuid) - escrow
		if left := n.stake(t, hotkey, s.vaultColdkey, s.netuid); left != lock || gained != observed && gained != observed-1 {
			t.Fatalf("epoch %d: the CTF pool hotkey kept %d of vault2's %d movable rao (lock %d), and escrow gained %d", epoch, left-lock, observed, lock, gained)
		}
		funded := id(captured + gained)
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "EmissionCaptured", map[string]any{
			"epoch": id(epoch), "noId": id(ctfPool), "observed": id(observed), "amount": id(gained), "fundedThrough": funded,
		})
		if s.vaultUint(t, "fundedThrough", id(ctfPool), id(epoch)).Cmp(funded) != 0 || s.vaultUint(t, "lastFundedEpoch", id(ctfPool)).Uint64() != epoch {
			t.Fatalf("epoch %d: the CTF pool's funding differs from its capture", epoch)
		}
		s.requireConservation(t)
		t.Logf("epoch %d: closeOperatorEpoch(%d, 2^32 + 1) captured %d of the %d rao on the CTF pool hotkey, gas %d", epoch, epoch, gained, observed, receipt.GasUsed)
	}

	// Epoch 1 closes for the CTF pool after its census, and operator 1's root
	// signer commits its root: the carry contract's account, holding the pots,
	// and a winner's. The auditor approves it.
	capture(t, firstEpoch)
	funded1 := s.vaultUint(t, "fundedThrough", id(ctfPool), id(firstEpoch)).Uint64()
	winner := newAccount(t)
	carried := funded1 - funded1/5
	first := newLedger(t, []merkle.LedgerAccount{ledgerAccount(carryColdkey, carried), ledgerAccount(winner, funded1-carried)}, deepLeafCount)
	commit1 := first.commitment("ctf ledger header epoch 1")
	n.send(t, operator.rootSigner, s.settlement, ctf, nil, "commitDistribution", id(firstEpoch), id(ctfPool), id(0), commit1)
	n.send(t, s.auditor, s.settlement, ctf, nil, "approveDistribution", id(ctfPool), uint64(1), commit1.Root, commit1.LedgerHash)

	// Past end(1) + window/2 the census window has closed, and the refusal
	// comes before the write-once check.
	if head := n.number(t); head < end+window/2+1 {
		n.sealUntil(t, end+window/2+1)
	}
	if got := n.refused(t, operator.rootSigner, s.settlement, ctf, nil, "commitCtfCensus", id(firstEpoch), id(1), other); got != "InvalidWindow" {
		t.Errorf("a census after its window refused with %s", got)
	}

	// R-9: the carry's creation and cycle (§4.6, "Qualifying it"): a griefer
	// fills the carry coldkey's staking-hotkey list before the carry exists;
	// the constructor still stakes its seed on the escrow hotkey by its own
	// addStake and sets the opt-outs; then a claim to the contract coldkey, a
	// moveStake and a transferStake from it, and the capture of the returned
	// position, with their gas, rounding and staking-hotkey lists.
	var residual uint64
	t.Run("carry", func(t *testing.T) {
		// Before the carry exists, the guardian makes its coldkey a hotkey
		// account, which reveals the coldkey but not the address, and the
		// proxy's account gets TAO above its existential deposit, without
		// which the creation's value cannot pass through it (OutOfFund).
		n.submit(t, guardian.Ring, n.call(t, "SubtensorModule.try_associate_hotkey", accountID(t, carryColdkey)))
		if n.owner(t, carryColdkey) != guardian.PublicKey() {
			t.Fatal("the carry's coldkey is not the guardian's hotkey")
		}
		n.fundEVM(t, factory, tao/1_000)
		// A third party fills the list to 128 entries; a 129th is refused.
		if _, err := fill(t, carryColdkey, thirdPartyStakingHotkeys+1); err == nil || !strings.Contains(err.Error(), "TooManyStakingHotkeys") {
			t.Fatalf("a third party's 129th entry on the carry coldkey's list: %v", err)
		}
		prefilled := n.stakingHotkeys(t, carryColdkey)
		if len(prefilled) != thirdPartyStakingHotkeys {
			t.Fatalf("third parties filled the carry coldkey's list to %d entries", len(prefilled))
		}

		// Anyone creates the carry contract at the address root 1 named, with
		// its seed as the call's value. The account keeps its existential
		// deposit, and the constructor stakes the rest, carrySeed rao.
		creator := newEVMKey(t)
		n.fundEVM(t, creator.address, 100*tao)
		created, creation := create2Valued(t, n, creator, factory, raoWei(carrySeed+existentialDeposit), carryCreation)
		if created != carry {
			t.Fatalf("the carry contract was created at %s, predicted %s", created, carry)
		}
		code, err := n.eth.CodeAt(n.ctx, carry, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Its coldkey is the runtime's 0x080c mapping of its address, which
		// its constructor called; the rest it read from vault2.
		if got := n.read(t, carry, carryArtifact, "coldkey")[0].([32]byte); got != carryColdkey {
			t.Fatalf("coldkey() reads %x; the mapping of %s is %x", got, carry, carryColdkey)
		}
		if n.read(t, carry, carryArtifact, "vault")[0].(common.Address) != s.vault ||
			n.read(t, carry, carryArtifact, "poolId")[0].(*big.Int).Uint64() != ctfPool ||
			n.read(t, carry, carryArtifact, "netuid")[0].(uint16) != s.netuid ||
			n.read(t, carry, carryArtifact, "escrowHotkey")[0].([32]byte) != s.escrow ||
			n.read(t, carry, carryArtifact, "poolHotkey")[0].([32]byte) != hotkey ||
			n.read(t, carry, carryArtifact, "vaultColdkey")[0].([32]byte) != s.vaultColdkey {
			t.Fatal("the carry contract read vault2 differently")
		}
		// The residual is what the seed's addStake gained on the escrow hotkey,
		// the escrow hotkey and the pool hotkey, its auto-stake destination,
		// joined the full list, and both opt-outs hold.
		residual = n.read(t, carry, carryArtifact, "residual")[0].(*big.Int).Uint64()
		listed := n.stakingHotkeys(t, carryColdkey)
		requireEvent(t, creation, carryArtifact, carry, "Seeded", map[string]any{"tao": id(carrySeed), "amount": id(residual), "residual": id(residual)})
		if residual == 0 || n.stake(t, s.escrow, carryColdkey, s.netuid) != residual || len(listed) != thirdPartyStakingHotkeys+2 ||
			!onList(listed, s.escrow) || !onList(listed, hotkey) {
			t.Fatalf("the seed staked a residual of %d rao, the carry holds %d on the escrow hotkey, and its list is %s",
				residual, n.stake(t, s.escrow, carryColdkey, s.netuid), hotkeyList(listed, names))
		}
		if destination := n.read(t, stakingAddress, settings, "getAutoStakeDestination", carryColdkey, s.netuid); !destination[0].(bool) || destination[1].([32]byte) != hotkey {
			t.Fatalf("the carry coldkey's auto-stake destination on netuid %d is %v, not the CTF pool hotkey", s.netuid, destination)
		}
		if !n.read(t, stakingAddress, settings, "getRejectStakingHotkeyGrowth", carryColdkey)[0].(bool) ||
			!n.read(t, stakingAddress, settings, "getRejectLockedAlpha", carryColdkey)[0].(bool) {
			t.Fatal("the carry coldkey does not keep its opt-outs")
		}
		if left := n.freeBalance(t, carryColdkey); left > existentialDeposit {
			t.Fatalf("the carry kept %d rao of TAO, more than its existential deposit", left)
		}

		// It recycles only its own account, and nothing while it holds only its residual.
		if got := n.refused(t, stranger, carry, carryArtifact, nil, "recycle", id(0), first.account(1), first.proof(1)); got != "InvalidColdkey" {
			t.Fatalf("a recycle of the winner's account refused with %s", got)
		}
		none := account{Index: id(0), Coldkey: [32]byte{}, Cumulative: id(0)}
		if got := n.refused(t, stranger, carry, carryArtifact, nil, "recycle", id(0), none, []proofNode{}); got != "NothingToRecycle" {
			t.Fatalf("a recycle with nothing above the residual refused with %s", got)
		}

		// No third party can add an entry any more, while a transfer on the
		// escrow hotkey is a donation the next recycle returns. A coldkey swap
		// into the carry's coldkey, a hotkey account, is refused at its
		// announcement.
		if _, err := fill(t, carryColdkey, len(listed)+1); err == nil || !strings.Contains(err.Error(), "AccountRejectsStakingHotkeyGrowth") {
			t.Fatalf("a third party's new entry on the carry coldkey's list after creation: %v", err)
		}
		const donation = 2 * tao
		n.stakeFor(t, alice, carryColdkey, s.escrow, s.netuid, donation)
		donated := n.stake(t, s.escrow, carryColdkey, s.netuid) - residual
		swapper := newColdkey(t)
		n.fund(t, swapper.PublicKey(), 10*tao)
		owned := newAccount(t)
		n.submit(t, swapper.Ring, n.batch(t, n.call(t, "SubtensorModule.try_associate_hotkey", accountID(t, owned)), n.addStakeCall(t, owned, s.netuid, 3_000_000)))
		if _, err := n.trySubmit(t, swapper.Ring, n.call(t, "SubtensorModule.announce_coldkey_swap_v2", accountID(t, carryColdkey))); err == nil ||
			!strings.Contains(err.Error(), "NewColdKeyIsHotkey") {
			t.Fatalf("a coldkey swap announced into the carry coldkey with a new hotkey: %v", err)
		}

		// Root 1 finalizes after its challenge window, before epoch 2 ends.
		n.sealUntil(t, s.pending(t, ctfPool).CommittedBlock+challengeBlocks)
		n.send(t, relayer, s.settlement, ctf, nil, "finalizeDistribution", id(ctfPool))

		// A stranger recycles: the claim pays the carry's coldkey on the escrow
		// hotkey, though third parties filled its list, the carry moves all of
		// it above the residual onto the CTF pool hotkey and transfers it to
		// vault2's position there, recorded for the pool's next epoch.
		a := first.account(0)
		claimed := a.Cumulative.Uint64()
		vaultBefore := n.stakingHotkeys(t, s.vaultColdkey)
		position := n.stake(t, hotkey, s.vaultColdkey, s.netuid)
		next := s.vaultUint(t, "lastFundedEpoch", id(ctfPool)).Uint64() + 1
		receipt, estimate, least := n.sendLeast(t, stranger, carry, carryArtifact, nil, "recycle", id(0), a, first.proof(0))
		recycled := n.stake(t, hotkey, s.vaultColdkey, s.netuid) - position
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "Claimed", map[string]any{
			"noId": id(ctfPool), "rootIndex": id(0), "index": id(0), "coldkey": carryColdkey,
			"cumulative": a.Cumulative, "amount": id(claimed), "caller": carry,
		})
		requireEvent(t, receipt, carryArtifact, carry, "Recycled", map[string]any{"epoch": id(next), "amount": id(recycled)})
		if paid := s.vaultUint(t, "claimed", id(ctfPool), id(0)).Uint64(); paid != claimed {
			t.Fatalf("vault2 records %d of the carry's %d rao as claimed", paid, claimed)
		}
		if got := n.read(t, carry, carryArtifact, "recycledFor", id(next))[0].(*big.Int).Uint64(); got != recycled || recycled > claimed+donated || claimed+donated-recycled > 3 {
			t.Fatalf("claimed %d rao with %d donated; vault2's position grew %d; recycledFor(%d) reads %d", claimed, donated, recycled, next, got)
		}
		// The residual stays, so the escrow hotkey stays on the list.
		escrowLeft, poolLeft := n.stake(t, s.escrow, carryColdkey, s.netuid), n.stake(t, hotkey, carryColdkey, s.netuid)
		kept := n.read(t, carry, carryArtifact, "residual")[0].(*big.Int).Uint64()
		carryAfter := n.stakingHotkeys(t, carryColdkey)
		if poolLeft != 0 || escrowLeft < kept || escrowLeft > kept+1 || kept > residual || residual-kept > 1 || !onList(carryAfter, s.escrow) || !onList(carryAfter, hotkey) {
			t.Fatalf("the carry holds %d rao on the escrow hotkey with a residual of %d (%d at creation) and %d on the CTF pool hotkey; its list is %s",
				escrowLeft, kept, residual, poolLeft, hotkeyList(carryAfter, names))
		}
		residual = kept
		s.requireConservation(t)
		vaultAfter := n.stakingHotkeys(t, s.vaultColdkey)
		t.Logf("STCtfCarry: CREATE2 of %d runtime bytes with a seed of %d rao, gas %d; residual %d rao of alpha", len(code), carrySeed, creation.GasUsed, residual)
		t.Logf("recycle: claimed %d rao to the carry's coldkey with %d donated and returned %d to vault2's position on the CTF pool hotkey (%d rao lost to rounding), recorded for epoch %d; "+
			"gas %d, least limit %d, eth_estimateGas limit %d, with the carry coldkey's list at %s",
			claimed, donated, recycled, claimed+donated-recycled, next, receipt.GasUsed, least, estimate, hotkeyList(carryAfter, names))
		t.Logf("vault2's StakingHotkeys: before %s, after %s", hotkeyList(vaultBefore, names), hotkeyList(vaultAfter, names))

		// Epoch 2's close captures the returned stake with the epoch's emission.
		n.stakeFor(t, alice, s.vaultColdkey, hotkey, s.netuid, 50*tao)
		capture(t, next)
		if growth := s.vaultUint(t, "fundedThrough", id(ctfPool), id(next)).Uint64() - funded1; growth < recycled {
			t.Fatalf("epoch %d's close funded %d rao, less than the %d recycled for it", next, growth, recycled)
		}
	})

	// R-9: what the residual cannot stop, and how each delay ends. Root 2
	// grows the carry account. Root raises the nominator minimum, and its
	// sweep clears the residual: the claim is refused until seed() restores
	// it. Neither the carry's coldkey nor vault2's can be reserved as a coldkey
	// swap's destination. Third parties hold vault2's list at 128 after a
	// capture, and the return is refused until vault2's absorbStake frees an
	// entry. Then the recycle pays.
	t.Run("griefing", func(t *testing.T) {
		funded2 := s.vaultUint(t, "fundedThrough", id(ctfPool), id(firstEpoch+1)).Uint64()
		delta := (funded2 - funded1) / 2
		second := newLedger(t, []merkle.LedgerAccount{ledgerAccount(carryColdkey, carried+delta), ledgerAccount(winner, funded2-carried-delta)}, deepLeafCount)
		commit2 := second.commitment("ctf ledger header epoch 2")
		s.inWindow(t, firstEpoch+1)
		n.send(t, operator.rootSigner, s.settlement, ctf, nil, "commitDistribution", id(firstEpoch+1), id(ctfPool), id(1), commit2)
		n.send(t, s.auditor, s.settlement, ctf, nil, "approveDistribution", id(ctfPool), uint64(2), commit2.Root, commit2.LedgerHash)
		n.sealUntil(t, s.pending(t, ctfPool).CommittedBlock+challengeBlocks)
		n.send(t, relayer, s.settlement, ctf, nil, "finalizeDistribution", id(ctfPool))
		a := second.account(0)
		paid := s.vaultUint(t, "claimed", id(ctfPool), id(0))
		owed := new(big.Int).Sub(a.Cumulative, paid)

		// Root's sweep: a nominator minimum of twice the residual's worth at the
		// subnet's spot price clears the residual back to the carry's TAO and
		// the escrow hotkey from its list. Twice the seed's TAO is not always
		// enough: the seed's own buy lifted the price it is now valued at.
		alpha := forgeArtifact(t, "src/interfaces/alpha.sol", "IAlpha")
		priceQ9 := new(big.Int).Quo(n.read(t, alphaAddress, alpha, "getAlphaPrice", s.netuid)[0].(*big.Int), big.NewInt(tao)).Uint64()
		worth := new(big.Int).Quo(new(big.Int).Mul(new(big.Int).SetUint64(residual), new(big.Int).SetUint64(priceQ9)), big.NewInt(tao)).Uint64()
		nominatorMinimum := max(2*worth+1, 2*tao)
		t.Logf("root's sweep: alpha at %d rao, the residual's %d rao of alpha worth %d rao, the nominator minimum raised to %d rao",
			priceQ9, residual, worth, nominatorMinimum)
		n.fundEVM(t, stranger.address, 10*nominatorMinimum)
		n.sudo(t, n.call(t, "AdminUtils.sudo_set_nominator_min_required_stake", types.NewU64(nominatorMinimum)))
		swept := n.stakingHotkeys(t, carryColdkey)
		if left := n.stake(t, s.escrow, carryColdkey, s.netuid); left != 0 || onList(swept, s.escrow) {
			t.Fatalf("root's sweep left the carry %d rao on the escrow hotkey and its list at %s", left, hotkeyList(swept, names))
		}
		if got := n.refused(t, stranger, carry, carryArtifact, nil, "recycle", id(1), a, second.proof(0)); got != "ResidualMissing" {
			t.Fatalf("a recycle after root's sweep refused with %s", got)
		}
		_, refusal := n.ethCall(s.vault, stakingAddress, n.staking.pack(t, "transferStake", carryColdkey, s.escrow, nu, nu, owed))
		if refusal == nil || !strings.Contains(refusal.Error(), "AccountRejectsStakingHotkeyGrowth") {
			t.Errorf("simulated from vault2, the claim's transfer to the swept carry answers %v", refusal)
		}
		minimum := n.read(t, carry, carryArtifact, "seedMinimum")[0].(*big.Int).Uint64()
		returned := n.freeBalance(t, carryColdkey)
		t.Logf("root's sweep returned %d rao of TAO to the carry, and cleared its list to %s; seedMinimum() is now %d rao",
			returned, hotkeyList(swept, names), minimum)
		if got := n.refused(t, stranger, carry, carryArtifact, nil, "seed"); got != "SeedTooSmall" {
			t.Fatalf("a seed of the swept TAO alone refused with %s", got)
		}
		receipt := n.send(t, stranger, carry, carryArtifact, raoWei(minimum), "seed")
		requireEvent(t, receipt, carryArtifact, carry, "ResidualLowered", map[string]any{"from": id(residual), "to": id(0)})
		residual = n.read(t, carry, carryArtifact, "residual")[0].(*big.Int).Uint64()
		if residual == 0 || n.stake(t, s.escrow, carryColdkey, s.netuid) != residual || !onList(n.stakingHotkeys(t, carryColdkey), s.escrow) {
			t.Fatalf("seed() restored a residual of %d rao; the carry holds %d on the escrow hotkey", residual, n.stake(t, s.escrow, carryColdkey, s.netuid))
		}
		t.Logf("seed(): gas %d, a residual of %d rao", receipt.GasUsed, residual)

		// A blank coldkey can reserve neither the carry's coldkey nor vault2's
		// as a swap's destination, both hotkey accounts: a reservation would
		// refuse every transfer into them, vault2's claim or the carry's return.
		announcer := newColdkey(t)
		n.fund(t, announcer.PublicKey(), 10*tao)
		for _, target := range [][32]byte{carryColdkey, s.vaultColdkey} {
			if _, err := n.trySubmit(t, announcer.Ring, n.call(t, "SubtensorModule.announce_coldkey_swap_v2", accountID(t, target))); err == nil ||
				!strings.Contains(err.Error(), "NewColdKeyIsHotkey") {
				t.Fatalf("a coldkey swap announced to reserve %x: %v", target, err)
			}
		}

		// Third parties hold vault2's list at 128 without the CTF pool hotkey,
		// which epoch 2's capture left empty.
		if onList(n.stakingHotkeys(t, s.vaultColdkey), hotkey) {
			t.Fatal("vault2 still lists the CTF pool hotkey after epoch 2's capture")
		}
		foreign, err := fill(t, s.vaultColdkey, thirdPartyStakingHotkeys)
		if err != nil || foreign == ([32]byte{}) {
			t.Fatalf("third parties filling vault2's list added %x: %v", foreign, err)
		}
		full := n.refused(t, stranger, carry, carryArtifact, nil, "recycle", id(1), a, second.proof(0))
		// The carry holds nothing on the CTF pool hotkey until the recycle's
		// claim and move, and the runtime checks the stake to withdraw before the
		// destination's list. So the return is simulated from a coldkey that holds
		// stake there: the stranger's own, sent to vault2's full list.
		n.send(t, stranger, stakingAddress, n.staking, nil, "addStake", hotkey, id(tao), nu)
		held := n.stake(t, hotkey, mirror(stranger.address), s.netuid)
		_, refusal = n.ethCall(stranger.address, stakingAddress, n.staking.pack(t, "transferStake", s.vaultColdkey, hotkey, nu, nu, id(held)))
		if refusal == nil || !strings.Contains(refusal.Error(), "TooManyStakingHotkeys") {
			t.Errorf("simulated from a coldkey with stake on the CTF pool hotkey, a return to vault2's full list answers %v", refusal)
		}
		absorb := n.send(t, relayer, s.vault, s.vaultArtifact, nil, "absorbStake", foreign)
		t.Logf("vault2's list full refuses the recycle (%s): simulated, a return to it answers %v; absorbStake of one foreign entry leaves it at %s, gas %d",
			full, refusal, hotkeyList(n.stakingHotkeys(t, s.vaultColdkey), names), absorb.GasUsed)

		// The recycle pays.
		position := n.stake(t, hotkey, s.vaultColdkey, s.netuid)
		receipt, estimate, least := n.sendLeast(t, stranger, carry, carryArtifact, nil, "recycle", id(1), a, second.proof(0))
		if now := s.vaultUint(t, "claimed", id(ctfPool), id(0)); now.Cmp(a.Cumulative) != 0 {
			t.Fatalf("the carry's claim paid, but vault2 records %s of %s as claimed", now, a.Cumulative)
		}
		if left := n.stake(t, s.escrow, carryColdkey, s.netuid); left < residual || left > residual+1 {
			t.Fatalf("the carry holds %d rao on the escrow hotkey after the recycle, with a residual of %d", left, residual)
		}
		s.requireConservation(t)
		t.Logf("the carry's claim of %s rao paid after each delay ended: vault2's position on the CTF pool hotkey grew %d; gas %d, least limit %d, eth_estimateGas limit %d",
			owed, n.stake(t, hotkey, s.vaultColdkey, s.netuid)-position, receipt.GasUsed, least, estimate)
	})
}

// deployCreate2Factory places the deterministic deployment proxy's exact
// runtime code (create2FactoryRuntime) at an address of this chain, as R-8
// does.
func deployCreate2Factory(t *testing.T, n *node, from evmKey) common.Address {
	t.Helper()
	runtime := hexutil.MustDecode(create2FactoryRuntime)
	initcode := append([]byte{0x60, byte(len(runtime)), 0x80, 0x60, 0x0b, 0x60, 0x00, 0x39, 0x60, 0x00, 0xf3}, runtime...)
	factory := n.deploy(t, from, initcode)
	if code, err := n.eth.CodeAt(n.ctx, factory, nil); err != nil || !bytes.Equal(code, runtime) {
		t.Fatalf("factory code %x (%v)", code, err)
	}
	return factory
}

// create2 has `from` send initcode, constructor arguments included, to the
// deterministic deployment proxy at factory with a zero salt. It returns the
// address CREATE2 predicts, which must then hold code, and the receipt.
func create2(t *testing.T, n *node, from evmKey, factory common.Address, initcode []byte) (common.Address, *gethtypes.Receipt) {
	t.Helper()
	return create2Valued(t, n, from, factory, nil, initcode)
}

// create2Valued is create2 with value, which the proxy passes to CREATE2,
// and so to the constructor.
func create2Valued(t *testing.T, n *node, from evmKey, factory common.Address, value *big.Int, initcode []byte) (common.Address, *gethtypes.Receipt) {
	t.Helper()
	var salt [32]byte
	address := crypto.CreateAddress2(factory, salt, crypto.Keccak256(initcode))
	receipt := n.transact(t, from, &factory, value, append(salt[:], initcode...), 0)
	if receipt.Status != gethtypes.ReceiptStatusSuccessful {
		t.Fatalf("CREATE2 through the factory failed in block %s", receipt.BlockNumber)
	}
	if code, err := n.eth.CodeAt(n.ctx, address, nil); err != nil || len(code) == 0 {
		t.Fatalf("no code at the predicted address %s (%v)", address, err)
	}
	return address, receipt
}

// ctfHotkey is the hotkey registerCtfPool derives:
// keccak256(abi.encode(proxy, pool, block, parent)).
func ctfHotkey(proxy common.Address, pool, block uint64, parent common.Hash) [32]byte {
	return [32]byte(crypto.Keccak256Hash(
		common.LeftPadBytes(proxy.Bytes(), 32),
		common.BigToHash(new(big.Int).SetUint64(pool)).Bytes(),
		common.BigToHash(new(big.Int).SetUint64(block)).Bytes(),
		parent.Bytes(),
	))
}

// ctfCensusEntry is the first slot of STSettlement2Ctf's census of
// (noId, epoch): its hash, followed by its block.
func ctfCensusEntry(noId, epoch uint64) common.Hash {
	inner := crypto.Keccak256(common.BigToHash(id(noId)).Bytes(), common.BigToHash(big.NewInt(ctfCensusSlot)).Bytes())
	return crypto.Keccak256Hash(common.BigToHash(id(epoch)).Bytes(), inner)
}

// ethBlock returns an EVM block's hash and its parent's, as the node's
// eth_getBlockByNumber reports them.
func (self *node) ethBlock(t *testing.T, number uint64) (hash, parent common.Hash) {
	t.Helper()
	var block *struct {
		Hash       common.Hash `json:"hash"`
		ParentHash common.Hash `json:"parentHash"`
	}
	if err := self.eth.Client().CallContext(self.ctx, &block, "eth_getBlockByNumber", hexutil.EncodeUint64(number), false); err != nil {
		t.Fatalf("eth_getBlockByNumber %d: %v", number, err)
	}
	if block == nil {
		t.Fatalf("the node has no EVM block %d", number)
	}
	return block.Hash, block.ParentHash
}

// storageWord reads one storage slot of the contract at address.
func (self *node) storageWord(t *testing.T, address common.Address, slot common.Hash) common.Hash {
	t.Helper()
	value, err := self.eth.StorageAt(self.ctx, address, slot, nil)
	if err != nil {
		t.Fatal(err)
	}
	return common.BytesToHash(value)
}

// hotkeyList describes a StakingHotkeys list: its length, the hotkeys of
// names on it, and how many others.
func hotkeyList(list []types.AccountID, names map[[32]byte]string) string {
	var held []string
	others := 0
	for _, hotkey := range list {
		if name, ok := names[[32]byte(hotkey)]; ok {
			held = append(held, name)
		} else {
			others++
		}
	}
	if others != 0 {
		held = append(held, fmt.Sprintf("%d others", others))
	}
	return fmt.Sprintf("%d entries [%s]", len(list), strings.Join(held, ", "))
}

// onList reports whether a StakingHotkeys list holds hotkey.
func onList(list []types.AccountID, hotkey [32]byte) bool {
	for _, entry := range list {
		if [32]byte(entry) == hotkey {
			return true
		}
	}
	return false
}
