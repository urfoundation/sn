package onchain

// withdraw.go — `snclaim withdraw --vault v1` (VERSION2-FINAL.md §10.2, §12.1
// step 8). The v1 vault keeps an accepted claim whose payment was deferred
// (below the transfer minimum, no alpha price, or a failed runtime transfer)
// as claimCredit[coldkey], and anyone may retry it forever with
// withdrawClaimCredit(coldkey). Payment always lands on the coldkey; the
// relayer only pays gas, through the same runTx lifecycle as claims.

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"

	docopt "github.com/docopt/docopt-go"
)

// cmdWithdraw implements `snclaim withdraw`. It reads the coldkey's credit at
// the latest block and refuses a zero credit before anything is signed. A
// withdrawal the vault would revert (TransferBelowMinimum,
// RuntimePriceUnavailable, RuntimeTransferFailed, RuntimeAccountingMismatch,
// NothingToWithdraw) fails runTx's preflight with the decoded reason; a paid
// one reports the amount from its ClaimPaid event.
func cmdWithdraw(ctx context.Context, opts docopt.Opts) error {
	// Only the immutable v1 vault keeps credit: vault2 pays each claim in
	// full or reverts it (VERSION2-FINAL.md §7.1).
	if vault := strOpt(opts, "--vault"); vault != "v1" {
		return fmt.Errorf("--vault: %q holds no claim credit (want v1)", vault)
	}
	coldkey, err := parseColdkey(strOpt(opts, "--coldkey"))
	if err != nil {
		return err
	}
	contract, err := parseContract(strOpt(opts, "--contract"))
	if err != nil {
		return err
	}
	wantChainID, gasLimit, err := parseChainAndGas(opts)
	if err != nil {
		return err
	}
	key, err := loadKeyFile(strOpt(opts, "--key_file"))
	if err != nil {
		return err
	}

	client, chainID, rpcURL, err := dialFirst(ctx, strsOpt(opts, "--rpc"))
	if err != nil {
		return err
	}
	defer client.Close()
	if wantChainID != nil && chainID.Cmp(wantChainID) != 0 {
		return fmt.Errorf("chain id mismatch: --chain_id=%s but %s reports %s", wantChainID, rpcURL, chainID)
	}
	ret, err := ethCall(ctx, client, contract, stSettlementVault.PackClaimCredit(coldkey))
	if err != nil {
		return fmt.Errorf("claimCredit(0x%x): %w", coldkey, err)
	}
	credit, err := stSettlementVault.UnpackClaimCredit(ret)
	if err != nil {
		return fmt.Errorf("claimCredit(0x%x): %w", coldkey, err)
	}
	if credit.Sign() == 0 {
		return fmt.Errorf("coldkey %s has no v1 claim credit at the latest block; nothing sent", renderColdkey(coldkey))
	}

	from := crypto.PubkeyToAddress(key.PublicKey)
	calldata := stSettlementVault.PackWithdrawClaimCredit(coldkey)
	receipt, err := runTx(ctx, client, chainID, txRequest{
		contract: contract,
		from:     from,
		key:      key,
		calldata: calldata,
		gasLimit: gasLimit,
		dryRun:   boolOpt(opts, "--dry-run"),
	}, func(gasEst uint64, gasErr error) {
		fmt.Printf("settlement-vault v1 credit withdrawal intent\n")
		fmt.Printf("  contract:   %s (chain id %s, rpc %s)\n", contract.Hex(), chainID, rpcURL)
		fmt.Printf("  from:       %s\n", from.Hex())
		fmt.Printf("  coldkey:    %s\n", renderColdkey(coldkey))
		fmt.Printf("  credit:     %s\n", formatAlpha(credit))
		fmt.Printf("  calldata:   %d bytes, selector 0x%x\n", len(calldata), calldata[:4])
		if gasErr == nil {
			fmt.Printf("  gas (est):  %d\n", gasEst)
		} else {
			fmt.Printf("  gas (est):  unavailable (%v); using --gas_limit=%d\n", gasErr, gasLimit)
		}
	})
	if err != nil {
		return err
	}
	if receipt == nil {
		return nil // dry run
	}
	for _, lg := range receipt.Logs {
		if lg.Address != contract {
			continue
		}
		ev, uerr := stSettlementVault.UnpackClaimPaidEvent(lg)
		if uerr != nil || ev.Coldkey != coldkey {
			continue
		}
		fmt.Printf("ClaimPaid: coldkey %s\n", renderColdkey(ev.Coldkey))
		fmt.Printf("  paid:     %s\n", formatAlpha(ev.Amount))
		fmt.Printf("  relayer:  %s\n", ev.Relayer.Hex())
		return nil
	}
	return fmt.Errorf("tx %s succeeded but %s emitted no ClaimPaid for coldkey %s", receipt.TxHash, contract.Hex(), renderColdkey(coldkey))
}
