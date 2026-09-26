package miner

// sn.go — subnet (bittensor) subcommands for the provider
// (sn/PLAN.md 7.3): `provider wallet set` registers the claim coldkey
// with the platform (decision D-2; the signed set lives in sn_wallet.go),
// and `provider claim` fetches and verifies this network's pool payout
// claim for an epoch (decision D-6). Claim recomputes the merkle leaf and
// checks the inclusion proof with sn/merkle, cross-checks the payout root
// on-chain via a minimal eth_call when --rpc is given, and builds the
// settlement vault's claim(uint256,uint256,bytes32,uint256,bytes32[])
// calldata with the shared sn/stabi packer. With a --key_file it signs and
// submits the transaction through sn/miner/onchain (go-ethereum); without
// one it prints the ready-to-submit calldata for the offline/air-gapped
// snclaim path. Head-tier membership is `provider fleet` (fleet.go):
// register_limit on the native chain, then the coordinator's dual-signed
// fleet bindings. The ABI encoding, keccak, merkle and ss58 all come from
// the shared sn packages — this file owns only the flow and the stdlib
// read-side eth_call transport (sn_rpc.go).

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/docopt/docopt-go"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Release bindings. Pool claims always target the immutable settlement vault;
// fleet membership always targets the coordinator.
var stSettlementVault = stabi.NewSTSettlementVault()
var stCoordinator = stabi.NewSTCoordinator()

// readNetworkJwt loads the network jwt written by `provider auth` from
// ~/.urnetwork/jwt — the same bootstrap credential `provider provide`
// uses to mint its client JWT (clientauth.LoadOrCreateClientJwt).
func readNetworkJwt() (string, error) {
	jwtPath, err := providerStatePath("jwt")
	if err != nil {
		return "", err
	}
	byJwtBytes, err := os.ReadFile(jwtPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("jwt does not exist at %s. Run `provider auth` first", jwtPath)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(byJwtBytes)), nil
}

// claim implements `provider claim [--epoch=<epoch>] [--rpc=<rpc_url>]...
// [--key_file=<key_file>] [--dry-run]`.
//
// Default epoch: the platform reports the current epoch e; the payout
// root for e is only committed and finalized after e ends
// (sn/WHITEPAPER.md 5.2), so the default target is e-1 — the most
// recent epoch that can have a committed root. During the first ~48h of
// e that root may still be inside its dispute window; `claim_open_block`
// in the output says when the claim becomes submittable.
//
// Verification requires the payout roots to agree: the inclusion proof
// walked locally from the recomputed leaf must authenticate the leaf
// against the server-provided root, and (when --rpc is given) that root
// must equal the root read from the contract with eth_call — so a
// verified claim does not rest on trusting the platform (decision D-6).
// Exits nonzero on any mismatch. When a --key_file (and --rpc) is given,
// a verified claim is signed and submitted via sn/miner/onchain;
// otherwise the ready-to-submit calldata is printed for snclaim.
func claim(opts docopt.Opts) {
	apiUrl, err := resolveApiUrl(opts)
	if err != nil {
		fmt.Printf("network config error: %s\n", err)
		os.Exit(1)
	}

	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(event.Ctx())
	defer cancel()

	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	defer clientStrategy.Close()

	dryRun, _ := opts.Bool("--dry-run")
	keyFile, _ := opts.String("--key_file")
	if dryRun && keyFile == "" {
		fmt.Printf("note: --dry-run has no effect without --key_file; claim only verifies\n")
	}

	byJwt, err := readNetworkJwt()
	if err != nil {
		panic(err)
	}
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer func() {
		_ = api.CloseAndWait(context.Background())
	}()
	api.SetByJwt(byJwt)

	var rpcUrls []string
	if rpcAny, ok := opts["--rpc"]; ok && rpcAny != nil {
		rpcUrls = append(rpcUrls, rpcAny.([]string)...)
	}
	// submitting needs an rpc endpoint to broadcast through
	if keyFile != "" && len(rpcUrls) == 0 {
		fmt.Printf("claim: --key_file needs --rpc to submit\n")
		os.Exit(1)
	}

	epoch := int64(0)
	epochNote := ""
	if epochStr, epochErr := opts.String("--epoch"); epochErr == nil && epochStr != "" {
		epoch, err = strconv.ParseInt(epochStr, 10, 64)
		if err != nil {
			panic(fmt.Errorf("bad --epoch %q: %s", epochStr, err))
		}
		if epoch < 0 {
			panic(fmt.Errorf("bad --epoch %q: must be non-negative", epochStr))
		}
	} else {
		epochResult, err := api.SnEpochSync()
		if err != nil {
			panic(err)
		}
		if epochResult.Epoch == 0 {
			panic(fmt.Errorf("current epoch is 0; no finalized epoch to claim yet"))
		}
		epoch = epochResult.Epoch - 1
		epochNote = fmt.Sprintf(" (last finalized; current epoch is %d. Use --epoch to override)", epochResult.Epoch)
	}

	poolClaim, err := api.SnPoolClaimSync(&sdk.SnPoolClaimArgs{
		Epoch: epoch,
	})
	if err != nil {
		panic(err)
	}

	// decode and sanity-check the claim fields
	if len(poolClaim.NoId) == 0 {
		panic(fmt.Errorf("claim has no no_id"))
	}
	if 32 < len(poolClaim.NoId) {
		panic(fmt.Errorf("bad no_id length %d; expected <= 32", len(poolClaim.NoId)))
	}
	noId := new(big.Int).SetBytes(poolClaim.NoId)
	if len(poolClaim.Coldkey) != 32 {
		panic(fmt.Errorf("bad coldkey length %d; expected 32", len(poolClaim.Coldkey)))
	}
	var coldkey [32]byte
	copy(coldkey[:], poolClaim.Coldkey)
	if len(poolClaim.PayoutRoot) != 32 {
		panic(fmt.Errorf("bad payout root length %d; expected 32", len(poolClaim.PayoutRoot)))
	}
	var serverRoot [32]byte
	copy(serverRoot[:], poolClaim.PayoutRoot)
	if poolClaim.ShareBps < 0 {
		panic(fmt.Errorf("bad share_bps %d", poolClaim.ShareBps))
	}
	shareBps := uint64(poolClaim.ShareBps)
	shareBpsBig := new(big.Int).SetUint64(shareBps)
	proof := make([][32]byte, len(poolClaim.Proof))
	for i, proofElement := range poolClaim.Proof {
		if len(proofElement) != 32 {
			panic(fmt.Errorf("bad proof element %d length %d; expected 32", i, len(proofElement)))
		}
		copy(proof[i][:], proofElement)
	}

	// recompute the leaf and check the inclusion proof against the server
	// root with sn/merkle — the server root is never trusted blindly
	leaf := merkle.PayoutLeaf(coldkey, shareBpsBig)
	proofVerifiesServer := merkle.Verify(serverRoot, leaf, proof)

	// read the on-chain root, trying each --rpc endpoint in order until one
	// answers both eth_chainId and eth_call. The noCommit read calldata is
	// built with sn/stabi; only the http transport is hand-rolled (sn_rpc.go).
	epochBig := big.NewInt(epoch)
	chainChecked := false
	var chainRoot [32]byte
	var chainId uint64
	var chainRpcUrl string
	if 0 < len(rpcUrls) {
		entitlementCalldata := stSettlementVault.PackEntitlement(epochBig, noId)
		for _, rpcUrl := range rpcUrls {
			chainIdHex, rpcErr := ethRpcHexResult(ctx, rpcUrl, "eth_chainId", []any{})
			if rpcErr != nil {
				fmt.Printf("rpc %s: %s\n", rpcUrl, rpcErr)
				continue
			}
			rpcChainId, rpcErr := parseEthHexQuantity(chainIdHex)
			if rpcErr != nil {
				fmt.Printf("rpc %s: bad eth_chainId %q\n", rpcUrl, chainIdHex)
				continue
			}
			callHex, rpcErr := ethRpcHexResult(ctx, rpcUrl, "eth_call", []any{
				map[string]any{
					"to":   poolClaim.ContractAddress,
					"data": fmt.Sprintf("0x%x", entitlementCalldata),
				},
				"latest",
			})
			if rpcErr != nil {
				fmt.Printf("rpc %s: %s\n", rpcUrl, rpcErr)
				continue
			}
			returnData, rpcErr := parseEthHexBytes(callHex)
			if rpcErr != nil || len(returnData) < 32 {
				fmt.Printf("rpc %s: entitlement returned %d bytes; expected >= 32 (wrong vault address?)\n", rpcUrl, len(returnData))
				continue
			}
			// entitlement returns the tuple with payoutRoot as its first word.
			copy(chainRoot[:], returnData[:32])
			chainId = rpcChainId
			chainRpcUrl = rpcUrl
			chainChecked = true
			break
		}
		if !chainChecked {
			fmt.Printf("status: UNVERIFIED — no --rpc endpoint answered\n")
			os.Exit(1)
		}
	}

	// all roots must agree: the proof must authenticate the leaf against the
	// server root, and (when --rpc is given) the on-chain root must equal the
	// server root and the chain ids must match
	mismatches := []string{}
	if !proofVerifiesServer {
		mismatches = append(mismatches, "the proof does not verify against the server payout root")
	}
	if chainChecked {
		if chainRoot == ([32]byte{}) {
			mismatches = append(mismatches, "the on-chain payout root is zero (epoch not committed on-chain yet?)")
		} else if chainRoot != serverRoot {
			mismatches = append(mismatches, "the server payout root does not match the on-chain root")
		}
		if poolClaim.ChainId < 0 || chainId != uint64(poolClaim.ChainId) {
			mismatches = append(mismatches, fmt.Sprintf("chain id mismatch: rpc says %d, server says %d", chainId, poolClaim.ChainId))
		}
	}

	fmt.Printf("epoch: %d%s\n", epoch, epochNote)
	fmt.Printf("no_id: 0x%x\n", poolClaim.NoId)
	fmt.Printf("coldkey: 0x%x\n", coldkey)
	fmt.Printf("share_bps: %d (%.2f%%)\n", shareBps, float64(shareBps)/100.0)
	fmt.Printf("payout_root (server): 0x%x\n", serverRoot)
	if proofVerifiesServer {
		fmt.Printf("payout_root (proof): verifies against the server root (%d-element proof)\n", len(proof))
	} else {
		fmt.Printf("payout_root (proof): DOES NOT verify against the server root (%d-element proof)\n", len(proof))
	}
	if chainChecked {
		fmt.Printf("payout_root (chain): 0x%x (via %s, chain id %d)\n", chainRoot, chainRpcUrl, chainId)
	} else {
		fmt.Printf("payout_root (chain): not checked. Pass --rpc=<rpc_url> to verify against the contract\n")
	}
	fmt.Printf("contract: %s (chain id %d)\n", poolClaim.ContractAddress, poolClaim.ChainId)
	fmt.Printf("claim_open_block: %d\n", poolClaim.ClaimOpenBlock)

	// build the ready-to-submit immutable-vault claim calldata with the shared stabi
	// packer — byte-identical to snclaim's own structured path
	claimCalldata, err := onchain.BuildClaimCalldata(onchain.ClaimIntent{
		E:        epochBig,
		NoID:     noId,
		Coldkey:  coldkey,
		ShareBps: shareBpsBig,
		Proof:    proof,
	})
	if err != nil {
		panic(fmt.Errorf("pack settlement-vault claim: %s", err))
	}

	if 0 < len(mismatches) {
		fmt.Printf("claim calldata:\n0x%x\n", claimCalldata)
		for _, mismatch := range mismatches {
			fmt.Printf("mismatch: %s\n", mismatch)
		}
		fmt.Printf("status: MISMATCH — do not submit\n")
		os.Exit(1)
	}

	// verified. With an EVM key, sign+send through onchain.Submit; otherwise
	// print the calldata for the offline/air-gapped snclaim path.
	if keyFile != "" {
		if !common.IsHexAddress(poolClaim.ContractAddress) {
			fmt.Printf("claim: server contract address %q is not a valid EVM address\n", poolClaim.ContractAddress)
			os.Exit(1)
		}
		if poolClaim.ChainId < 0 {
			fmt.Printf("claim: server chain id %d is invalid\n", poolClaim.ChainId)
			os.Exit(1)
		}
		contract := common.HexToAddress(poolClaim.ContractAddress)
		key, err := onchain.LoadKeyFile(keyFile)
		if err != nil {
			fmt.Printf("claim: %s\n", err)
			os.Exit(1)
		}
		receipt, err := onchain.Submit(ctx, onchain.SubmitParams{
			Contract: contract,
			Rpcs:     rpcUrls,
			Key:      key,
			Calldata: claimCalldata,
			ChainID:  new(big.Int).SetUint64(uint64(poolClaim.ChainId)),
			DryRun:   dryRun,
		})
		if err != nil {
			fmt.Printf("claim submit failed: %s\n", err)
			os.Exit(1)
		}
		if receipt == nil {
			return // dry run; onchain.Submit printed the preflight
		}
		printMinerClaimed(receipt, contract)
		return
	}

	fmt.Printf("claim calldata:\n0x%x\n", claimCalldata)
	fmt.Printf("submit with: snclaim submit --calldata=0x%x --contract=%s --rpc=<rpc_url> --key_file=<evm_key_file>\n", claimCalldata, poolClaim.ContractAddress)
	if chainChecked {
		fmt.Printf("status: VERIFIED (proof, server, and on-chain roots agree)\n")
	} else {
		fmt.Printf("status: VERIFIED against the server root only\n")
	}
}

type minerClaimReceiptEvents struct {
	Claimed  []*stabi.STSettlementVaultClaimed
	Paid     []*stabi.STSettlementVaultClaimPaid
	Deferred []*stabi.STSettlementVaultClaimPaymentDeferred
}

func decodeMinerClaimReceipt(receipt *types.Receipt, contract common.Address) minerClaimReceiptEvents {
	result := minerClaimReceiptEvents{}
	if receipt == nil {
		return result
	}
	for _, lg := range receipt.Logs {
		if lg.Address != contract {
			continue
		}
		if ev, err := stSettlementVault.UnpackClaimedEvent(lg); err == nil {
			result.Claimed = append(result.Claimed, ev)
			continue
		}
		if ev, err := stSettlementVault.UnpackClaimPaidEvent(lg); err == nil {
			result.Paid = append(result.Paid, ev)
			continue
		}
		if ev, err := stSettlementVault.UnpackClaimPaymentDeferredEvent(lg); err == nil {
			result.Deferred = append(result.Deferred, ev)
		}
	}
	return result
}

// printMinerClaimed distinguishes acceptance of a Merkle entitlement from the
// runtime transfer. Small entitlements remain durable claim credit until they
// aggregate above Subtensor's DefaultMinTransfer floor.
func printMinerClaimed(receipt *types.Receipt, contract common.Address) {
	events := decodeMinerClaimReceipt(receipt, contract)
	for _, ev := range events.Claimed {
		fmt.Printf("Claimed: epoch %s, noId %s\n", ev.Epoch, ev.NoId)
		fmt.Printf("  coldkey:  0x%x\n", ev.Coldkey)
		fmt.Printf("  shareBps: %s\n", ev.ShareBps)
		fmt.Printf("  credited: %s rao\n", ev.Amount)
	}
	for _, ev := range events.Paid {
		fmt.Printf("ClaimPaid: coldkey 0x%x, paid %s rao\n", ev.Coldkey, ev.Amount)
	}
	for _, ev := range events.Deferred {
		fmt.Printf("ClaimPaymentDeferred: coldkey 0x%x, credit %s rao, TAO equivalent %s rao, minimum %d rao, reason %d\n", ev.Coldkey, ev.CreditAlphaRao, ev.TaoEquivalentRao, ev.MinimumTransferTaoRao, ev.Reason)
	}
	if len(events.Claimed) == 0 {
		fmt.Printf("warning: no Claimed event decoded from the receipt\n")
	} else if len(events.Paid) == 0 && len(events.Deferred) == 0 {
		fmt.Printf("claim accepted with zero credit; no runtime transfer was required\n")
	}
}
