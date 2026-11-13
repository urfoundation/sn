//go:build runtime_rehearsal

package rehearsal

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/chain"
)

// buildSnMainnet builds this repository's sn-mainnet, as the operator runs
// it, in a private directory.
func buildSnMainnet(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "sn-mainnet")
	build := exec.Command("go", "build", "-o", binary, "./mainnet")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./mainnet: %v\n%s", err, output)
	}
	return binary
}

// runSnMainnet runs one sn-mainnet command and returns its output, its
// diagnostics and its exit code.
func runSnMainnet(t *testing.T, binary string, args ...string) (string, string, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil && command.ProcessState == nil {
		t.Fatalf("sn-mainnet %v: %v", args, err)
	}
	return stdout.String(), stderr.String(), command.ProcessState.ExitCode()
}

// snMainnetResult runs one sn-mainnet command that must succeed and decodes
// its JSON result.
func snMainnetResult(t *testing.T, binary string, result any, args ...string) {
	t.Helper()
	stdout, stderr, code := runSnMainnet(t, binary, args...)
	if code != 0 {
		t.Fatalf("sn-mainnet %v: exit %d\n%s", args, code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), result); err != nil {
		t.Fatalf("sn-mainnet %v: %v\n%s", args, err, stdout)
	}
}

// R-4. TestR4Registration: pool registration through STSettlement2, reviewed,
// preflighted and read back by sn-mainnet register-operator-pool; the escrow
// and a pool hotkey pruned, their stake kept and still captured;
// re-registration through registerPoolUid; absorbStake of vault2's stake on a
// foreign hotkey, the v1 escrow hotkey (VERSION2-FINAL.md §15.8).
func TestR4Registration(t *testing.T) {
	t.Parallel()
	binary := buildSnMainnet(t)
	n := startNode(t)
	s := deployStack(t, n, stackConfig{operators: 3, shortFrom: 2})
	pool1, pool2 := s.operators[1].pool, s.operators[2].pool

	// R-4: registerOperatorPool forwards the burn ceiling to vault2, which
	// registers the hotkey under its mapped coldkey and refunds the rest to
	// the owner through STSettlement2. sn-mainnet's review prints the owner's
	// exact call after its eth_call preflight from the owner, the owner sends
	// exactly that call, and readback confirms the registration.
	t.Run("registration", func(t *testing.T) {
		economics, err := chain.ReadRegistrationEconomicsAtContext(n.ctx, n.sub, s.netuid, n.head(t))
		if err != nil {
			t.Fatal(err)
		}
		before := n.balance(t, s.owner.address)
		mapped := n.freeBalance(t, s.vaultColdkey)
		inputs := []string{"--rpc", n.http, "--settlement2", s.settlement.Hex(), "--no-id", "2", "--pool-hotkey", hexutil.Encode(pool2[:]), "--first-epoch", "1"}
		var review struct {
			To           common.Address `json:"to"`
			ValueWei     string         `json:"value_wei"`
			Data         hexutil.Bytes  `json:"data"`
			PreflightUid uint16         `json:"preflight_uid"`
		}
		snMainnetResult(t, binary, &review, append([]string{"register-operator-pool", "review", "--safe", s.owner.address.Hex(), "--maximum-burn-rao", strconv.FormatUint(maximumBurn, 10), "--value-wei", raoWei(maximumBurn).String()}, inputs...)...)
		value, _ := new(big.Int).SetString(review.ValueWei, 10)
		if review.To != s.settlement || value == nil || value.Cmp(raoWei(maximumBurn)) != 0 || !bytes.Equal(review.Data, s.settlementArtifact.pack(t, "registerOperatorPool", id(2), pool2, uint64(maximumBurn), uint64(1))) {
			t.Fatalf("reviewed call: %+v", review)
		}
		receipt := n.transact(t, s.owner, &review.To, value, review.Data, 0)
		if receipt.Status != gethtypes.ReceiptStatusSuccessful {
			t.Fatalf("the reviewed registerOperatorPool failed in block %s", receipt.BlockNumber)
		}
		var readback struct {
			Uid uint16 `json:"uid"`
		}
		snMainnetResult(t, binary, &readback, append([]string{"register-operator-pool", "readback", "--transaction-hash", receipt.TxHash.Hex()}, inputs...)...)
		uid, registered := n.uid(t, s.netuid, pool2)
		if !registered || n.owner(t, pool2) != s.vaultColdkey || review.PreflightUid != uid || readback.Uid != uid {
			t.Fatalf("pool 2's hotkey: registered %v, owner %x, UID %d, preflight %d, readback %d", registered, n.owner(t, pool2), uid, review.PreflightUid, readback.Uid)
		}
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "PoolRegistered", map[string]any{
			"noId": id(2), "hotkey": pool2, "uid": uid, "firstEpoch": uint64(1),
		})
		// The burn decays in the registration's own block, so the owner pays
		// at most the burn read before it, and vault2 keeps nothing.
		paid := new(big.Int).Sub(before, n.balance(t, s.owner.address))
		paid.Sub(paid, fee(receipt))
		if paid.Sign() <= 0 || paid.Cmp(raoWei(economics.BurnRao)) > 0 || n.freeBalance(t, s.vaultColdkey) != mapped {
			t.Fatalf("owner paid %s wei beyond the fee for a burn of at most %d rao; vault2's account went %d -> %d",
				paid, economics.BurnRao, mapped, n.freeBalance(t, s.vaultColdkey))
		}
		pool := s.pool(t, 2)
		if pool.Hotkey != pool2 || pool.FirstEpoch != 1 || !pool.Registered || s.vaultUint(t, "lastFundedEpoch", id(2)).Sign() != 0 {
			t.Fatalf("pools(2) = %+v", pool)
		}
		t.Logf("registerOperatorPool: UID %d, burn %s wei of the %d rao ceiling, the rest refunded to the owner, gas %d", uid, paid, uint64(maximumBurn), receipt.GasUsed)

		// An already registered pool, and a first epoch whose cadence is too short.
		if got := n.refused(t, s.owner, s.settlement, s.settlementArtifact, raoWei(maximumBurn), "registerOperatorPool",
			id(2), newAccount(t), uint64(maximumBurn), uint64(1)); got != "InvalidConfiguration" {
			t.Fatalf("second registration refused with %s", got)
		}
		if got := n.refused(t, s.owner, s.settlement, s.settlementArtifact, raoWei(maximumBurn), "registerOperatorPool",
			id(3), s.operators[3].pool, uint64(maximumBurn), uint64(2)); got != "CadenceTooShort" {
			t.Fatalf("short first epoch refused with %s", got)
		}
	})

	// R-4: pruning takes a hotkey's UID but keeps its stake and owner, and the
	// stake still moves. Epochs stop first (tempo 0), so every UID's emission
	// stays 0 and pruning takes the oldest UID: the escrow before pool 1.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_tempo", types.NewU16(s.netuid), types.NewU16(0)))
	n.stakeFor(t, alice, s.vaultColdkey, pool1, s.netuid, 100*tao)
	staked := n.stake(t, pool1, s.vaultColdkey, s.netuid)
	t.Run("pruning", func(t *testing.T) {
		size, err := chain.SubnetworkNAtContext(n.ctx, n.sub, s.netuid, n.head(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range []types.Call{
			n.call(t, "AdminUtils.sudo_set_min_allowed_uids", types.NewU16(s.netuid), types.NewU16(1)),
			n.call(t, "AdminUtils.sudo_set_max_allowed_uids", types.NewU16(s.netuid), types.NewU16(size)),
			n.call(t, "AdminUtils.sudo_set_immunity_period", types.NewU16(s.netuid), types.NewU16(0)),
			n.call(t, "AdminUtils.sudo_set_min_non_immune_uids", types.NewU16(s.netuid), types.NewU16(0)),
		} {
			n.sudo(t, call)
		}
		registrar := newColdkey(t)
		n.fund(t, registrar.PublicKey(), 10_000*tao)
		for i := 0; ; i++ {
			if _, registered := n.uid(t, s.netuid, pool1); !registered {
				break
			}
			if i == int(size) {
				t.Fatalf("pool 1 still registered after %d registrations", size)
			}
			n.registerHotkey(t, registrar.Ring, s.netuid, newAccount(t))
		}
		if _, registered := n.uid(t, s.netuid, s.escrow); registered {
			t.Fatal("the escrow hotkey, registered before pool 1, kept its UID")
		}
		if n.stake(t, pool1, s.vaultColdkey, s.netuid) != staked || n.owner(t, pool1) != s.vaultColdkey || n.owner(t, s.escrow) != s.vaultColdkey {
			t.Fatal("pruning changed a stake or an owner")
		}

		// The pruned pool's stake moves into the pruned escrow at epoch 1's close.
		receipt := s.close(t, 1, 1)
		gained := n.stake(t, s.escrow, s.vaultColdkey, s.netuid)
		if n.stake(t, pool1, s.vaultColdkey, s.netuid) != 0 || gained != staked && gained != staked-1 {
			t.Fatalf("capture from the pruned pool: pool left %d, escrow gained %d of %d", n.stake(t, pool1, s.vaultColdkey, s.netuid), gained, staked)
		}
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "EmissionCaptured", map[string]any{
			"epoch": id(1), "noId": id(1), "observed": id(staked), "amount": id(gained), "fundedThrough": id(gained),
		})
		s.requireConservation(t)
	})

	// R-4: anyone re-registers a pruned pool hotkey, paying the burn.
	t.Run("registerPoolUid", func(t *testing.T) {
		payer := newEVMKey(t)
		n.fundEVM(t, payer.address, 1_000*tao)
		if got := n.refused(t, payer, s.vault, s.vaultArtifact, raoWei(maximumBurn), "registerPoolUid", id(9), uint64(maximumBurn)); got != "UnknownPool" {
			t.Fatalf("unknown pool refused with %s", got)
		}
		economics, err := chain.ReadRegistrationEconomicsAtContext(n.ctx, n.sub, s.netuid, n.head(t))
		if err != nil {
			t.Fatal(err)
		}
		before := n.balance(t, payer.address)
		receipt := n.send(t, payer, s.vault, s.vaultArtifact, raoWei(maximumBurn), "registerPoolUid", id(1), uint64(maximumBurn))
		uid, registered := n.uid(t, s.netuid, pool1)
		if !registered {
			t.Fatal("pool 1 not re-registered")
		}
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "PoolUidRegistered", map[string]any{
			"noId": id(1), "hotkey": pool1, "uid": uid, "payer": payer.address,
		})
		paid := new(big.Int).Sub(before, n.balance(t, payer.address))
		if paid.Sub(paid, fee(receipt)).Sign() <= 0 || paid.Cmp(raoWei(economics.BurnRao)) > 0 {
			t.Fatalf("payer paid %s wei beyond the fee for a burn of at most %d rao", paid, economics.BurnRao)
		}
		if got := n.refused(t, payer, s.vault, s.vaultArtifact, raoWei(maximumBurn), "registerPoolUid", id(1), uint64(maximumBurn)); got != "UidExists" {
			t.Fatalf("registered pool refused with %s", got)
		}
		t.Logf("registerPoolUid: UID %d, burn %s wei, gas %d", uid, paid, receipt.GasUsed)
	})

	// R-4: stake vault2's coldkey holds on a foreign hotkey, here the v1
	// escrow hotkey, moves into escrow as surplus.
	t.Run("absorbStake", func(t *testing.T) {
		donor := newColdkey(t)
		n.fund(t, donor.PublicKey(), 1_000*tao)
		n.stakeFor(t, donor.Ring, s.vaultColdkey, s.v1Escrow, s.netuid, 50*tao)
		held := n.stake(t, s.v1Escrow, s.vaultColdkey, s.netuid)
		accounted, live := s.vaultUint(t, "escrowAccounted"), s.vaultUint(t, "liveEscrowStake")
		caller := newEVMKey(t)
		n.fundEVM(t, caller.address, 100*tao)
		receipt := n.send(t, caller, s.vault, s.vaultArtifact, nil, "absorbStake", s.v1Escrow)
		gained := s.vaultUint(t, "liveEscrowStake").Uint64() - live.Uint64()
		if n.stake(t, s.v1Escrow, s.vaultColdkey, s.netuid) != 0 || gained != held && gained != held-1 ||
			s.vaultUint(t, "escrowAccounted").Cmp(accounted) != 0 {
			t.Fatalf("absorbStake of %d rao: left %d, escrow gained %d", held, n.stake(t, s.v1Escrow, s.vaultColdkey, s.netuid), gained)
		}
		requireEvent(t, receipt, s.vaultArtifact, s.vault, "StakeAbsorbed", map[string]any{
			"hotkey": s.v1Escrow, "observed": id(held), "amount": id(gained), "caller": caller.address,
		})
		s.requireConservation(t)
		for _, refusal := range []struct {
			hotkey [32]byte
			want   string
		}{{[32]byte{}, "InvalidHotkey"}, {s.escrow, "InvalidHotkey"}, {pool1, "InvalidHotkey"}, {s.v1Escrow, "NothingToAbsorb"}} {
			if got := n.refused(t, caller, s.vault, s.vaultArtifact, nil, "absorbStake", refusal.hotkey); got != refusal.want {
				t.Fatalf("absorbStake(%x) refused with %s, want %s", refusal.hotkey, got, refusal.want)
			}
		}
		t.Logf("absorbStake: %d rao from the v1 escrow hotkey became %d rao of surplus, gas %d", held, gained, receipt.GasUsed)
	})
}

// R-4, PR-6. TestR4RegistrationRefusals: sn-mainnet register-operator-pool
// names what the node answers for a registration the chain refuses, and exits
// 3, never 1. The node answers a revert with code -32603 and the revert's
// bytes as data, a runtime refusal such as a burn above the cap as a revert
// with no data, and a sender short of the value as the EVM error OutOfFund.
// Readback names a reverted view the same way (VERSION2-FINAL.md §15.8).
func TestR4RegistrationRefusals(t *testing.T) {
	t.Parallel()
	binary := buildSnMainnet(t)
	n := startNode(t)
	s := deployStack(t, n, stackConfig{operators: 3, shortFrom: 2})
	pool2, pool3 := hexutil.Encode(s.operators[2].pool[:]), hexutil.Encode(s.operators[3].pool[:])
	fresh := newAccount(t)
	ceiling, value := strconv.FormatUint(maximumBurn, 10), raoWei(maximumBurn).String()
	review := func(safe common.Address, noId, hotkey, burn, wei, firstEpoch string) (string, string, int) {
		return runSnMainnet(t, binary, "register-operator-pool", "review", "--rpc", n.http, "--settlement2", s.settlement.Hex(), "--safe", safe.Hex(),
			"--no-id", noId, "--pool-hotkey", hotkey, "--maximum-burn-rao", burn, "--value-wei", wei, "--first-epoch", firstEpoch)
	}
	if stdout, stderr, code := review(s.owner.address, "2", pool2, ceiling, value, "1"); code != 0 || stdout == "" {
		t.Fatalf("the owner's review of pool 2: exit %d\n%s", code, stderr)
	}
	for _, refusal := range []struct {
		name                                string
		safe                                common.Address
		noId, hotkey, burn, wei, firstEpoch string
		want                                string
	}{
		{"a Safe that does not own STSettlement2", s.auditor.address, "2", pool2, ceiling, value, "1", "OwnableUnauthorizedAccount: the Safe does not own STSettlement2"},
		{"a registered pool", s.owner.address, "1", hexutil.Encode(fresh[:]), ceiling, value, "1", "InvalidConfiguration: vault2 refuses the pool"},
		{"a zero burn cap", s.owner.address, "2", pool2, "0", "0", "1", "InvalidConfiguration: vault2 refuses the pool"},
		{"a first epoch with a short cadence", s.owner.address, "3", pool3, ceiling, value, "2", "CadenceTooShort: the first epoch's v1 policy is shorter than its commit window plus vault2's challenge window"},
		{"a no_id that is no v1 operator", s.owner.address, "9", hexutil.Encode(fresh[:]), ceiling, value, "1", "UnknownOperator: no_id is not a v1 operator"},
		{"an unfunded Safe", newEVMKey(t).address, "2", pool2, ceiling, value, "1", "OutOfFund: the Safe holds less than the value"},
		{"an owner short of the value", s.owner.address, "2", pool2, strconv.FormatUint(1_000_000*tao, 10), raoWei(1_000_000 * tao).String(), "1", "OutOfFund: the Safe holds less than the value"},
		{"a burn above the cap", s.owner.address, "2", pool2, "1", raoWei(1).String(), "1", `the call failed with "VM Exception while processing transaction: revert" and data 0x`},
	} {
		stdout, stderr, code := review(refusal.safe, refusal.noId, refusal.hotkey, refusal.burn, refusal.wei, refusal.firstEpoch)
		if code != 3 || stdout != "" || !strings.Contains(stderr, "registerOperatorPool refused: "+refusal.want) {
			t.Fatalf("%s: exit %d, want 3 and %q\n%s", refusal.name, code, refusal.want, stderr)
		}
	}
	// settlementAt reverts for a no_id that has neither a v2 pool nor a v1
	// operator; readback reports it as a difference.
	stdout, stderr, code := runSnMainnet(t, binary, "register-operator-pool", "readback", "--rpc", n.http, "--settlement2", s.settlement.Hex(),
		"--no-id", "9", "--pool-hotkey", hexutil.Encode(fresh[:]), "--first-epoch", "1", "--transaction-hash", common.Hash{9}.Hex())
	if code != 3 || stdout != "" || !strings.Contains(stderr, "registerOperatorPool readback differs: UnknownOperator: no_id is not a v1 operator") {
		t.Fatalf("readback of no v1 operator: exit %d\n%s", code, stderr)
	}
}

// balance returns an EVM account's balance in wei.
func (self *node) balance(t *testing.T, address common.Address) *big.Int {
	t.Helper()
	balance, err := self.eth.BalanceAt(self.ctx, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	return balance
}
