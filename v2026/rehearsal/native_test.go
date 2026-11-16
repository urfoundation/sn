//go:build runtime_rehearsal

package rehearsal

import (
	"crypto/rand"
	"errors"
	"reflect"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

const (
	// tao is one TAO, or one alpha, in rao.
	tao = 1_000_000_000
	// wholePosition is AlphaBalance::MAX, which move_stake and transfer_stake
	// read as the live origin position.
	wholePosition = ^uint64(0)
)

// alice is the dev chain's sudo key and holds 1,000,000 TAO at genesis.
var alice = signature.TestKeyringPairAlice

// newColdkey returns a fresh sr25519 keypair that signs extrinsics.
func newColdkey(t *testing.T) *crv4.Keypair {
	t.Helper()
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		t.Fatal(err)
	}
	keypair, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return keypair
}

// newAccount returns a fresh account ID, for hotkeys and coldkeys that never sign.
func newAccount(t *testing.T) [32]byte {
	t.Helper()
	var account [32]byte
	if _, err := rand.Read(account[:]); err != nil {
		t.Fatal(err)
	}
	return account
}

// call builds a runtime call from the node's metadata.
func (self *node) call(t *testing.T, name string, args ...any) types.Call {
	t.Helper()
	call, err := types.NewCall(self.sub.Meta, name, args...)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return call
}

// submit seals call into its own block and fails the test unless it dispatched.
func (self *node) submit(t *testing.T, signer signature.KeyringPair, call types.Call) types.Hash {
	t.Helper()
	block, err := self.trySubmit(t, signer, call)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

// trySubmit signs call with the repository's signer (chain.EncodeSignedCall),
// seals it into its own block and returns the block with the runtime's
// refusal, a *crv4.FinalizedDispatchError, if the call failed.
func (self *node) trySubmit(t *testing.T, signer signature.KeyringPair, call types.Call) (types.Hash, error) {
	t.Helper()
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
		t.Fatalf("submit extrinsic: %v (%v)", err, errorData(err))
	}
	block := self.seal(t)
	err = self.sub.VerifyFinalizedExtrinsicContext(self.ctx, block, chain.ExtrinsicHash(raw))
	var refused *crv4.FinalizedDispatchError
	if err != nil && !errors.As(err, &refused) {
		t.Fatalf("extrinsic receipt: %v", err)
	}
	return block, err
}

// sudo dispatches call as root through Alice's Sudo.sudo. The extrinsic
// succeeds even when the inner call fails, so callers check its effect.
func (self *node) sudo(t *testing.T, call types.Call) {
	t.Helper()
	self.submit(t, alice, self.call(t, "Sudo.sudo", call))
}

// fund transfers rao from Alice to account.
func (self *node) fund(t *testing.T, account [32]byte, rao uint64) {
	t.Helper()
	destination, err := types.NewMultiAddressFromAccountID(account[:])
	if err != nil {
		t.Fatal(err)
	}
	self.submit(t, alice, self.call(t, "Balances.transfer_keep_alive", destination, types.NewUCompactFromUInt(rao)))
}

// createSubnet registers a subnet owned by owner, whose hotkey takes UID 0,
// starts its emission and returns its netuid.
func (self *node) createSubnet(t *testing.T, owner signature.KeyringPair, ownerHotkey [32]byte) uint16 {
	t.Helper()
	self.submit(t, owner, self.call(t, "SubtensorModule.register_network", accountID(t, ownerHotkey)))
	for netuid := uint16(1); netuid < 1024; netuid++ {
		if _, registered := self.uid(t, netuid, ownerHotkey); registered {
			self.submit(t, owner, self.call(t, "SubtensorModule.start_call", types.NewU16(netuid)))
			return netuid
		}
	}
	t.Fatal("the new subnet's owner hotkey has no UID")
	return 0
}

// registerHotkey burn-registers hotkey on netuid under coldkey and returns its UID.
func (self *node) registerHotkey(t *testing.T, coldkey signature.KeyringPair, netuid uint16, hotkey [32]byte) uint16 {
	t.Helper()
	call, err := chain.BurnRegisterLimitCall(self.sub.Meta, netuid, hotkey, ^uint64(0))
	if err != nil {
		t.Fatal(err)
	}
	self.submit(t, coldkey, call)
	uid, registered := self.uid(t, netuid, hotkey)
	if !registered {
		t.Fatalf("hotkey %x has no UID on netuid %d after registration", hotkey, netuid)
	}
	return uid
}

// stakeFor stakes taoRao of funder's balance on hotkey and transfers the
// funder's whole position there to coldkey, which may be a contract's mapped
// coldkey. The add_stake swap prices the alpha; the transfer does not.
func (self *node) stakeFor(t *testing.T, funder signature.KeyringPair, coldkey, hotkey [32]byte, netuid uint16, taoRao uint64) {
	t.Helper()
	add, err := chain.AddStakeCall(self.sub.Meta, netuid, hotkey, taoRao)
	if err != nil {
		t.Fatal(err)
	}
	self.submit(t, funder, add)
	self.submit(t, funder, self.call(t, "SubtensorModule.transfer_stake", accountID(t, coldkey), accountID(t, hotkey),
		types.NewU16(netuid), types.NewU16(netuid), types.NewU64(wholePosition)))
}

// hotkeyAlpha reads TotalHotkeyAlpha from storage: all alpha on hotkey on
// netuid, across coldkeys, without the share-pool read of getStake.
func (self *node) hotkeyAlpha(t *testing.T, hotkey [32]byte, netuid uint16) uint64 {
	t.Helper()
	alpha, err := chain.TotalHotkeyAlphaAtContext(self.ctx, self.sub, hotkey, netuid, self.head(t))
	if err != nil {
		t.Fatal(err)
	}
	return alpha
}

// freeBalance reads an account's free balance in rao from storage.
func (self *node) freeBalance(t *testing.T, account [32]byte) uint64 {
	t.Helper()
	balance, err := chain.FreeBalanceAtContext(self.ctx, self.sub, account, self.head(t))
	if err != nil {
		t.Fatal(err)
	}
	return balance
}

// uid reads a hotkey's UID on netuid from storage.
func (self *node) uid(t *testing.T, netuid uint16, hotkey [32]byte) (uint16, bool) {
	t.Helper()
	uid, registered, err := chain.UIDAtContext(self.ctx, self.sub, netuid, hotkey, self.head(t))
	if err != nil {
		t.Fatal(err)
	}
	return uid, registered
}

// owner reads the coldkey that owns hotkey from storage.
func (self *node) owner(t *testing.T, hotkey [32]byte) [32]byte {
	t.Helper()
	owner, err := chain.HotkeyOwnerAtContext(self.ctx, self.sub, hotkey, self.head(t))
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func accountID(t *testing.T, account [32]byte) types.AccountID {
	t.Helper()
	id, err := types.NewAccountID(account[:])
	if err != nil {
		t.Fatal(err)
	}
	return *id
}

// batch is Utility.batch_all: calls dispatched in order, all or none.
func (self *node) batch(t *testing.T, calls ...types.Call) types.Call {
	t.Helper()
	return self.call(t, "Utility.batch_all", calls)
}

// addStakeCall stakes rao of the signer's TAO on hotkey.
func (self *node) addStakeCall(t *testing.T, hotkey [32]byte, netuid uint16, rao uint64) types.Call {
	t.Helper()
	call, err := chain.AddStakeCall(self.sub.Meta, netuid, hotkey, rao)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

// transferStakeCall transfers alpha of the signer's stake on hotkey to coldkey.
func (self *node) transferStakeCall(t *testing.T, coldkey, hotkey [32]byte, netuid uint16, alpha uint64) types.Call {
	t.Helper()
	return self.call(t, "SubtensorModule.transfer_stake", accountID(t, coldkey), accountID(t, hotkey),
		types.NewU16(netuid), types.NewU16(netuid), types.NewU64(alpha))
}

// stakingHotkeys reads a coldkey's StakingHotkeys list from storage.
func (self *node) stakingHotkeys(t *testing.T, coldkey [32]byte) []types.AccountID {
	t.Helper()
	key, err := types.CreateStorageKey(self.sub.Meta, crv4.PalletName, "StakingHotkeys", coldkey[:])
	if err != nil {
		t.Fatal(err)
	}
	var hotkeys []types.AccountID
	if _, err := chain.ReadStorageAtContext(self.ctx, self.sub, key, crv4.PalletName, "StakingHotkeys", &hotkeys, self.head(t)); err != nil {
		t.Fatal(err)
	}
	return hotkeys
}

// errorData returns the data field of a JSON-RPC error, where the node
// explains a refused transaction.
func errorData(err error) any {
	value := reflect.ValueOf(err)
	if value.Kind() != reflect.Pointer || value.Elem().Kind() != reflect.Struct {
		return nil
	}
	if data := value.Elem().FieldByName("Data"); data.IsValid() && data.CanInterface() {
		return data.Interface()
	}
	return nil
}
