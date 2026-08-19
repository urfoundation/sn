package onchain

import (
	"context"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"

	docopt "github.com/docopt/docopt-go"
)

// cmdSubmit implements `snclaim submit`, in either raw-calldata mode
// (--calldata from `provider claim`, sent byte-for-byte) or structured mode
// (--epoch/--no_id/--coldkey/--share_bps/--proof packed via stabi).
func cmdSubmit(opts docopt.Opts) error {
	contract, err := parseContract(strOpt(opts, "--contract"))
	if err != nil {
		return err
	}
	rpcs := strsOpt(opts, "--rpc")
	if len(rpcs) == 0 {
		return fmt.Errorf("--rpc: at least one endpoint required")
	}

	var wantChainID *big.Int
	if s := strOpt(opts, "--chain_id"); s != "" {
		wantChainID, err = parseBig("--chain_id", s)
		if err != nil {
			return err
		}
	}
	var gasLimit uint64
	if s := strOpt(opts, "--gas_limit"); s != "" {
		gasLimit, err = strconv.ParseUint(s, 0, 64)
		if err != nil {
			return fmt.Errorf("--gas_limit: %w", err)
		}
	}

	// Build or validate the calldata.
	var (
		calldata []byte
		intent   *claimIntent
	)
	if hasOpt(opts, "--calldata") {
		calldata, intent, err = parseClaimCalldata(strOpt(opts, "--calldata"))
		if err != nil {
			return err
		}
	} else {
		intent = &claimIntent{}
		if intent.E, err = parseBig("--epoch", strOpt(opts, "--epoch")); err != nil {
			return err
		}
		if intent.NoID, err = parseBig("--no_id", strOpt(opts, "--no_id")); err != nil {
			return err
		}
		if intent.Coldkey, err = parseColdkey(strOpt(opts, "--coldkey")); err != nil {
			return err
		}
		if intent.ShareBps, err = parseBig("--share_bps", strOpt(opts, "--share_bps")); err != nil {
			return err
		}
		if intent.ShareBps.Sign() <= 0 || intent.ShareBps.Cmp(big.NewInt(10_000)) > 0 {
			return fmt.Errorf("--share_bps: %s out of range 1..10000", intent.ShareBps)
		}
		if intent.Proof, err = parseProof(strOpt(opts, "--proof")); err != nil {
			return err
		}
		if calldata, err = buildClaimCalldata(intent); err != nil {
			return fmt.Errorf("pack claimMiner: %w", err)
		}
	}

	key, err := loadKeyFile(strOpt(opts, "--key_file"))
	if err != nil {
		return err
	}

	receipt, err := submit(context.Background(), SubmitParams{
		Contract: contract,
		Rpcs:     rpcs,
		Key:      key,
		Calldata: calldata,
		ChainID:  wantChainID,
		GasLimit: gasLimit,
		DryRun:   boolOpt(opts, "--dry-run"),
	}, func(from common.Address, chainID *big.Int, rpcURL string) func(uint64, error) {
		return func(gasEst uint64, gasErr error) {
			fmt.Printf("claimMiner intent\n")
			fmt.Printf("  contract:   %s (chain id %s, rpc %s)\n", contract.Hex(), chainID, rpcURL)
			fmt.Printf("  from:       %s\n", from.Hex())
			fmt.Printf("  epoch:      %s\n", intent.E)
			fmt.Printf("  noId:       %s\n", intent.NoID)
			fmt.Printf("  coldkey:    %s\n", renderColdkey(intent.Coldkey))
			fmt.Printf("  shareBps:   %s\n", intent.ShareBps)
			fmt.Printf("  proof:      %d nodes\n", len(intent.Proof))
			fmt.Printf("  calldata:   %d bytes, selector 0x%x\n", len(calldata), calldata[:4])
			if gasErr == nil {
				fmt.Printf("  gas (est):  %d\n", gasEst)
			} else {
				fmt.Printf("  gas (est):  unavailable (%v); using --gas_limit=%d\n", gasErr, gasLimit)
			}
		}
	})
	if err != nil {
		return err
	}
	if receipt == nil {
		return nil // dry run
	}
	decoded := false
	for _, lg := range receipt.Logs {
		if lg.Address != contract {
			continue
		}
		ev, uerr := stSubnet.UnpackMinerClaimedEvent(lg)
		if uerr != nil {
			continue
		}
		fmt.Printf("MinerClaimed: epoch %s, noId %s\n", ev.E, ev.NoId)
		fmt.Printf("  coldkey:  %s\n", renderColdkey(ev.Coldkey))
		fmt.Printf("  shareBps: %s\n", ev.ShareBps)
		fmt.Printf("  paid:     %s\n", formatAlpha(ev.Amount))
		decoded = true
	}
	if !decoded {
		fmt.Println("warning: no MinerClaimed event decoded from the receipt")
	}
	return nil
}

// cmdStatus implements `snclaim status`: read-only views of the claim state
// for (epoch, noId) and optionally whether a coldkey has already claimed.
func cmdStatus(opts docopt.Opts) error {
	e, err := parseBig("--epoch", strOpt(opts, "--epoch"))
	if err != nil {
		return err
	}
	noID, err := parseBig("--no_id", strOpt(opts, "--no_id"))
	if err != nil {
		return err
	}
	contract, err := parseContract(strOpt(opts, "--contract"))
	if err != nil {
		return err
	}
	rpcs := strsOpt(opts, "--rpc")
	if len(rpcs) == 0 {
		return fmt.Errorf("--rpc: at least one endpoint required")
	}
	var coldkey *[32]byte
	if s := strOpt(opts, "--coldkey"); s != "" {
		ck, err := parseColdkey(s)
		if err != nil {
			return err
		}
		coldkey = &ck
	}

	ctx := context.Background()
	client, chainID, rpcURL, err := dialFirst(ctx, rpcs)
	if err != nil {
		return err
	}
	defer client.Close()

	finalizedRet, err := ethCall(ctx, client, contract, stSubnet.PackFinalized(e))
	if err != nil {
		return fmt.Errorf("finalized(%s): %w", e, err)
	}
	finalized, err := stSubnet.UnpackFinalized(finalizedRet)
	if err != nil {
		return fmt.Errorf("finalized(%s): %w", e, err)
	}
	commitRet, err := ethCall(ctx, client, contract, stSubnet.PackNoCommit(e, noID))
	if err != nil {
		return fmt.Errorf("noCommit(%s,%s): %w", e, noID, err)
	}
	commit, err := stSubnet.UnpackNoCommit(commitRet)
	if err != nil {
		return fmt.Errorf("noCommit(%s,%s): %w", e, noID, err)
	}
	totalRet, err := ethCall(ctx, client, contract, stSubnet.PackPoolTotal(e, noID))
	if err != nil {
		return fmt.Errorf("poolTotal(%s,%s): %w", e, noID, err)
	}
	total, err := stSubnet.UnpackPoolTotal(totalRet)
	if err != nil {
		return fmt.Errorf("poolTotal(%s,%s): %w", e, noID, err)
	}
	claimedRet, err := ethCall(ctx, client, contract, stSubnet.PackClaimedMiner(e, noID))
	if err != nil {
		return fmt.Errorf("claimedMiner(%s,%s): %w", e, noID, err)
	}
	claimed, err := stSubnet.UnpackClaimedMiner(claimedRet)
	if err != nil {
		return fmt.Errorf("claimedMiner(%s,%s): %w", e, noID, err)
	}

	fmt.Printf("STSubnet claim status — contract %s (chain id %s, rpc %s)\n", contract.Hex(), chainID, rpcURL)
	if finalized {
		fmt.Printf("  epoch:        %s (finalized — claims open)\n", e)
	} else {
		fmt.Printf("  epoch:        %s (NOT finalized — claims open only after finalizeEpoch)\n", e)
	}
	fmt.Printf("  pool (noId):  %s\n", noID)
	if commit.PayoutRoot == ([32]byte{}) {
		fmt.Printf("  payout root:  none (no operator commit for this epoch/pool)\n")
	} else {
		fmt.Printf("  payout root:  0x%x\n", commit.PayoutRoot)
	}
	fmt.Printf("  pool total:   %s\n", formatAlpha(total))
	fmt.Printf("  claimed:      %s%s\n", formatAlpha(claimed), shareOfPool(claimed, total))
	fmt.Printf("  remaining:    %s\n", formatAlpha(new(big.Int).Sub(total, claimed)))

	if coldkey != nil {
		key := minerClaimedByKey(noID, *coldkey)
		ret, err := ethCall(ctx, client, contract, stSubnet.PackMinerClaimedBy(e, key))
		if err != nil {
			return fmt.Errorf("minerClaimedBy(%s,0x%x): %w", e, key, err)
		}
		done, err := stSubnet.UnpackMinerClaimedBy(ret)
		if err != nil {
			return fmt.Errorf("minerClaimedBy(%s,0x%x): %w", e, key, err)
		}
		fmt.Printf("  coldkey:      %s\n", renderColdkey(*coldkey))
		if done {
			fmt.Printf("    claimed:    yes — already claimed for this (epoch, pool)\n")
		} else {
			fmt.Printf("    claimed:    no\n")
		}
	}
	return nil
}

// shareOfPool renders claimed/total in percent when total > 0.
func shareOfPool(claimed, total *big.Int) string {
	if total.Sign() <= 0 {
		return ""
	}
	bps := new(big.Int).Div(new(big.Int).Mul(claimed, big.NewInt(10_000)), total)
	return fmt.Sprintf(" — %d.%02d%% of pool", bps.Int64()/100, bps.Int64()%100)
}
