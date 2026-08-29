package onchain

// api.go — the miner-facing submission API. The stdlib-built `provider claim` /
// `provider bind-head` / `provider unbind-head` commands (package miner) pack
// their calldata with sn/stabi + sn/merkle and, when handed an EVM key, sign and
// broadcast it through these exported wrappers instead of shelling out to the
// snclaim binary. The snclaim CLI handlers (cmdSubmit/cmdUnbindHead) route
// through the same funcs, so there is a single packing + submission path.

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// ClaimIntent is a decoded immutable-vault claim call.
// Exported (as an alias of the internal claimIntent) so external callers can
// construct one for BuildClaimCalldata without reaching into the package.
type ClaimIntent = claimIntent

// BuildClaimCalldata ABI-packs STSettlementVault.claim via the stabi bindings.
func BuildClaimCalldata(intent ClaimIntent) ([]byte, error) {
	return buildClaimCalldata(&intent)
}

// BuildBindHeadCalldata ABI-packs bindHead(hotkey, clientId, clientIdSig) via
// the stabi bindings. sig is the provider's 64-byte Ed25519 R‖S over the
// on-chain headBindDigest.
func BuildBindHeadCalldata(hotkey, clientID [32]byte, sig []byte) ([]byte, error) {
	return legacySTSubnet.TryPackBindHead(hotkey, clientID, sig)
}

// BuildUnbindHeadCalldata ABI-packs unbindHead(hotkey) via the stabi bindings.
func BuildUnbindHeadCalldata(hotkey [32]byte) ([]byte, error) {
	return legacySTSubnet.TryPackUnbindHead(hotkey)
}

// BuildFleetBindingCalldata packs the release-1.0 many-to-one dual-signed
// binding. The protocol package owns the canonical digest; this function is
// only the ABI boundary.
func BuildFleetBindingCalldata(binding protocol.FleetBinding, clientSignature, hotkeySignature []byte) ([]byte, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	if !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
		return nil, fmt.Errorf("fleet binding signatures do not verify")
	}
	return stCoordinator.TryPackBindFleetMember(stabi.STCoordinatorFleetBinding{
		ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: common.Address(binding.Coordinator),
		FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID,
		ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch,
		ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash,
	}, clientSignature, hotkeySignature)
}

// LoadKeyFile reads a hex-encoded 32-byte secp256k1 EVM private key
// (0x-optional, surrounding whitespace ignored).
func LoadKeyFile(path string) (*ecdsa.PrivateKey, error) {
	return loadKeyFile(path)
}

// SubmitParams is the input to Submit: the contract to call, the rpc failover
// list, the signing key, ready-to-send calldata, and the usual chain-id / gas /
// dry-run knobs.
type SubmitParams struct {
	Contract common.Address
	Rpcs     []string
	Key      *ecdsa.PrivateKey
	Calldata []byte
	ChainID  *big.Int // optional; when set, the rpc's chain id must match it
	GasLimit uint64   // 0 = estimate + 20% headroom
	DryRun   bool
}

// SubmitHooks make the signed-transaction durability boundary explicit for
// long-running relayers. Prepared runs after signing and before broadcast;
// Broadcast runs immediately after SendTransaction succeeds.
type SubmitHooks struct {
	Prepared  func(common.Hash, []byte) error
	Broadcast func(common.Hash) error
}

// Submit dials the first reachable rpc (failover), verifies the chain id, and
// runs the shared submit lifecycle: an eth_call preflight (surfacing revert
// reasons before spending gas), a gas estimate, a generic intent block, a stop
// on DryRun, and otherwise sign + send + wait-mined. It returns the mined
// receipt, or nil on a dry run. The snclaim CLI handlers share the same
// lifecycle via the internal submit().
func Submit(ctx context.Context, p SubmitParams) (*types.Receipt, error) {
	return submit(ctx, p, nil, SubmitHooks{})
}

func SubmitWithHooks(ctx context.Context, p SubmitParams, hooks SubmitHooks) (*types.Receipt, error) {
	return submit(ctx, p, nil, hooks)
}

// intentPrinter builds the per-command intent block once the connection is
// resolved: it receives the resolved sender, chain id, and answering rpc url,
// and returns the printIntent callback runTx invokes with the gas estimate. A
// nil intentPrinter selects Submit's generic block.
type intentPrinter func(from common.Address, chainID *big.Int, rpcURL string) func(gasEst uint64, gasErr error)

// submit is the shared dial + chain-id check + runTx path behind Submit and the
// submit/unbind-head CLI handlers. mkPrint may be nil (generic intent block).
func submit(ctx context.Context, p SubmitParams, mkPrint intentPrinter, hooks SubmitHooks) (*types.Receipt, error) {
	from := crypto.PubkeyToAddress(p.Key.PublicKey)
	client, chainID, rpcURL, err := dialFirst(ctx, p.Rpcs)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if p.ChainID != nil && chainID.Cmp(p.ChainID) != 0 {
		return nil, fmt.Errorf("chain id mismatch: --chain_id=%s but %s reports %s", p.ChainID, rpcURL, chainID)
	}

	var printIntent func(gasEst uint64, gasErr error)
	if mkPrint != nil {
		printIntent = mkPrint(from, chainID, rpcURL)
	} else {
		printIntent = func(gasEst uint64, gasErr error) {
			fmt.Printf("submit intent\n")
			fmt.Printf("  contract:   %s (chain id %s, rpc %s)\n", p.Contract.Hex(), chainID, rpcURL)
			fmt.Printf("  from:       %s\n", from.Hex())
			if len(p.Calldata) >= 4 {
				fmt.Printf("  calldata:   %d bytes, selector 0x%x\n", len(p.Calldata), p.Calldata[:4])
			} else {
				fmt.Printf("  calldata:   %d bytes\n", len(p.Calldata))
			}
			if gasErr == nil {
				fmt.Printf("  gas (est):  %d\n", gasEst)
			} else {
				fmt.Printf("  gas (est):  unavailable (%v); using --gas_limit=%d\n", gasErr, p.GasLimit)
			}
		}
	}

	return runTx(ctx, client, chainID, txRequest{
		contract:  p.Contract,
		from:      from,
		key:       p.Key,
		calldata:  p.Calldata,
		gasLimit:  p.GasLimit,
		dryRun:    p.DryRun,
		prepared:  hooks.Prepared,
		broadcast: hooks.Broadcast,
	}, printIntent)
}
