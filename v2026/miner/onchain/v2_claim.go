package onchain

// v2_claim.go — `snclaim v2 status` and `snclaim v2 claim` (VERSION2-FINAL.md
// §10.2): a coldkey's accounts as the ledger endpoint lists them, with the
// claimed watermark read from vault2; one claim per call, or with --all
// every account, one by one or through the claim batcher (v2_batch.go). A
// claim pays cumulative − claimed to the account's coldkey whoever sends
// it, so a third party having claimed first is success (SPEC A6).

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	docopt "github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

// cmdV2Status implements `snclaim v2 status`: per pool, the account index,
// the finalized cumulative, claimable, claimed (read from vault2) and the
// pending cumulative. An endpoint that cannot answer shows "unavailable",
// never zero.
func cmdV2Status(ctx context.Context, opts docopt.Opts) error {
	coldkey, err := parseColdkey(strOpt(opts, "--coldkey"))
	if err != nil {
		return err
	}
	vault, err := parseVault2(strOpt(opts, "--vault"))
	if err != nil {
		return err
	}
	api, err := parseAPIURL(strOpt(opts, "--api_url"))
	if err != nil {
		return err
	}
	wantChainID, _, err := parseChainAndGas(opts)
	if err != nil {
		return err
	}
	client, chainID, rpcURL, err := v2Connect(ctx, opts, wantChainID)
	if err != nil {
		return err
	}
	defer client.Close()

	fmt.Printf("settlement v2 account status — vault %s (chain id %s, rpc %s)\n", vault.Hex(), chainID, rpcURL)
	fmt.Printf("  coldkey:      %s\n", renderColdkey(coldkey))
	accounts, err := fetchV2Accounts(ctx, api, coldkey)
	if err != nil {
		fmt.Printf("  accounts:     unavailable (%v)\n", err)
		return nil
	}
	if len(accounts) == 0 {
		fmt.Printf("  accounts:     none\n")
		return nil
	}
	reader := &v2Reader{client: client, vault: vault}
	for i := range accounts {
		account := &accounts[i]
		if err := account.check(vault); err != nil {
			return err
		}
		claimed, err := v2View(ctx, reader, vault2.PackClaimed(new(big.Int).SetUint64(account.NoID), new(big.Int).SetUint64(account.Index)), vault2.UnpackClaimed)
		if err != nil {
			return fmt.Errorf("claimed(%d,%d): %w", account.NoID, account.Index, err)
		}
		finalized, claimable := "none (the account is only in the pending root)", formatAlpha(new(big.Int))
		if account.RootIndex != nil {
			cumulative, err := v2Amount("cumulative", account.Cumulative)
			if err != nil {
				return err
			}
			finalized = fmt.Sprintf("%s at root %d", v2Alpha(cumulative), *account.RootIndex)
			if remaining := new(big.Int).Sub(cumulative.ToBig(), claimed); remaining.Sign() > 0 {
				claimable = formatAlpha(remaining)
			}
		}
		pending := "none"
		if account.PendingCumulative != nil {
			value, err := v2Amount("pending_cumulative", *account.PendingCumulative)
			if err != nil {
				return err
			}
			pending = v2Alpha(value)
		}
		fmt.Printf("  pool %d, account %d:\n", account.NoID, account.Index)
		fmt.Printf("    finalized:  %s\n", finalized)
		fmt.Printf("    claimed:    %s\n", formatAlpha(claimed))
		fmt.Printf("    claimable:  %s\n", claimable)
		fmt.Printf("    pending:    %s\n", pending)
	}
	return nil
}

// cmdV2Claim implements `snclaim v2 claim`: it takes the coldkey's one
// finalized account, or the one --no_id and --index pick, verifies the
// endpoint's proof against the root vault2 holds, refuses a mismatch, and
// sends claim. An account whose claimed watermark already reaches its
// cumulative, before the send or after a refused one, is reported as done.
// With --all it verifies every finalized account of the coldkey first, then
// claims each one with anything claimable, or with --batch two or more of
// them through the claim batcher.
func cmdV2Claim(ctx context.Context, opts docopt.Opts) error {
	coldkey, err := parseColdkey(strOpt(opts, "--coldkey"))
	if err != nil {
		return err
	}
	vault, err := parseVault2(strOpt(opts, "--vault"))
	if err != nil {
		return err
	}
	api, err := parseAPIURL(strOpt(opts, "--api_url"))
	if err != nil {
		return err
	}
	noID, pickNoID, err := parseUint64Opt(opts, "--no_id")
	if err != nil {
		return err
	}
	index, pickIndex, err := parseUint64Opt(opts, "--index")
	if err != nil {
		return err
	}
	all := boolOpt(opts, "--all")
	var batcher *common.Address
	if hasOpt(opts, "--batch") {
		text := strOpt(opts, "--batch")
		if !common.IsHexAddress(text) {
			return fmt.Errorf("--batch: %q is not a 20-byte hex address", text)
		}
		batcher = new(common.Address)
		*batcher = common.HexToAddress(text)
	}
	wantChainID, gasLimit, err := parseChainAndGas(opts)
	if err != nil {
		return err
	}
	key, err := loadKeyFile(strOpt(opts, "--key_file"))
	if err != nil {
		return err
	}
	client, chainID, rpcURL, err := v2Connect(ctx, opts, wantChainID)
	if err != nil {
		return err
	}
	defer client.Close()
	if batcher != nil {
		if err := VerifyClaimBatcher(ctx, client, *batcher); err != nil {
			return fmt.Errorf("--batch: %w", err)
		}
	}

	accounts, err := fetchV2Accounts(ctx, api, coldkey)
	if err != nil {
		return fmt.Errorf("ledger endpoint: %w", err)
	}
	var picked []*v2Account
	for i := range accounts {
		account := &accounts[i]
		if account.RootIndex != nil && (all || (!pickNoID || account.NoID == noID) && (!pickIndex || account.Index == index)) {
			picked = append(picked, account)
		}
	}
	if !all && len(picked) != 1 {
		names := make([]string, len(picked))
		for i, account := range picked {
			names[i] = fmt.Sprintf("--no_id=%d --index=%d", account.NoID, account.Index)
		}
		return fmt.Errorf("coldkey %s has %d finalized accounts matching the selection [%s]; claim exactly one", renderColdkey(coldkey), len(picked), strings.Join(names, ", "))
	}
	reader := &v2Reader{client: client, vault: vault}
	var claims []*v2Claim
	for _, account := range picked {
		claim, err := prepareV2Claim(ctx, reader, coldkey, account)
		if err != nil {
			return err
		}
		if claim != nil {
			claims = append(claims, claim)
		}
	}
	if all && len(picked) == 0 {
		fmt.Printf("coldkey %s has no finalized account; nothing to claim\n", renderColdkey(coldkey))
	}

	sender := &v2Sender{client: client, chainID: chainID, rpcURL: rpcURL, vault: vault, from: crypto.PubkeyToAddress(key.PublicKey), key: key, gasLimit: gasLimit, dryRun: boolOpt(opts, "--dry-run")}
	if batcher != nil && len(claims) > 1 {
		return sendV2Batch(ctx, sender, reader, *batcher, claims)
	}
	var failures []error
	for _, claim := range claims {
		if err := sendV2Claim(ctx, sender, reader, claim); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// v2Claim is one finalized account whose proof verified against vault2's root
// and whose claimed watermark is below its cumulative.
type v2Claim struct {
	account    *v2Account
	coldkey    [32]byte
	cumulative *uint256.Int
	claimed    *big.Int
	proof      []merkle.LedgerNode
	root       [32]byte
}

func (c *v2Claim) noID() *big.Int      { return new(big.Int).SetUint64(c.account.NoID) }
func (c *v2Claim) index() *big.Int     { return new(big.Int).SetUint64(c.account.Index) }
func (c *v2Claim) rootIndex() *big.Int { return new(big.Int).SetUint64(*c.account.RootIndex) }

// item is the claim's arguments, for vault2 or the batcher.
func (c *v2Claim) item() stabi.STClaimBatcherClaim {
	return stabi.STClaimBatcherClaim{NoId: c.noID(), RootIndex: c.rootIndex(), Account: v2LedgerAccount(c.account.Index, &merkle.LedgerAccount{Coldkey: c.coldkey, Cumulative: *c.cumulative}), Proof: v2ProofNodes(c.proof)}
}

// readClaimed reads the account's claimed watermark from vault2.
func (c *v2Claim) readClaimed(ctx context.Context, reader *v2Reader) (*big.Int, error) {
	claimed, err := v2View(ctx, reader, vault2.PackClaimed(c.noID(), c.index()), vault2.UnpackClaimed)
	if err != nil {
		return nil, fmt.Errorf("claimed(%d,%d): %w", c.account.NoID, c.account.Index, err)
	}
	return claimed, nil
}

// prepareV2Claim verifies the endpoint's account against the root vault2
// holds and reads its claimed watermark. It returns nil, after saying so, for
// an account with nothing to claim.
func prepareV2Claim(ctx context.Context, reader *v2Reader, coldkey [32]byte, account *v2Account) (*v2Claim, error) {
	if err := account.check(reader.vault); err != nil {
		return nil, err
	}
	cumulative, err := v2Amount("cumulative", account.Cumulative)
	if err != nil {
		return nil, err
	}
	proof, err := account.proof()
	if err != nil {
		return nil, err
	}
	claim := &v2Claim{account: account, coldkey: coldkey, cumulative: cumulative, proof: proof}
	root, err := v2View(ctx, reader, vault2.PackRoots(claim.noID(), claim.rootIndex()), vault2.UnpackRoots)
	if err != nil {
		return nil, fmt.Errorf("roots(%d,%d): %w", account.NoID, *account.RootIndex, err)
	}
	if !merkle.VerifyLedgerProof(root.Root, uint256.MustFromBig(root.Total), root.LeafCount, account.Index, coldkey, cumulative, proof) {
		return nil, fmt.Errorf("the ledger endpoint's proof for pool %d account %d does not verify against root %d (0x%x) in vault2; nothing sent", account.NoID, account.Index, *account.RootIndex, root.Root)
	}
	claim.root = root.Root
	if claim.claimed, err = claim.readClaimed(ctx, reader); err != nil {
		return nil, err
	}
	if claim.claimed.Cmp(cumulative.ToBig()) >= 0 {
		fmt.Printf("pool %d account %d: claimed %s already reaches the cumulative %s; nothing to claim\n", account.NoID, account.Index, formatAlpha(claim.claimed), v2Alpha(cumulative))
		return nil, nil
	}
	return claim, nil
}

// describe prints the claim's account lines of an intent.
func (c *v2Claim) describe() {
	fmt.Printf("  account:     pool %d, account %d, root %d\n", c.account.NoID, c.account.Index, *c.account.RootIndex)
	fmt.Printf("  coldkey:     %s\n", renderColdkey(c.coldkey))
	fmt.Printf("  cumulative:  %s\n", v2Alpha(c.cumulative))
	fmt.Printf("  claimed:     %s\n", formatAlpha(c.claimed))
	fmt.Printf("  claimable:   %s\n", formatAlpha(new(big.Int).Sub(c.cumulative.ToBig(), c.claimed)))
	fmt.Printf("  proof:       %d siblings, verified against root 0x%x\n", len(c.proof), c.root)
}

// done reports whether a refused claim is done: the re-read watermark covers
// its cumulative, because another sender claimed it first (SPEC A6).
func (c *v2Claim) done(ctx context.Context, reader *v2Reader) (bool, error) {
	now, err := c.readClaimed(ctx, reader)
	if err != nil {
		return false, err
	}
	if now.Cmp(c.cumulative.ToBig()) < 0 {
		return false, nil
	}
	fmt.Printf("pool %d account %d: another sender claimed it first (claimed %s); done\n", c.account.NoID, c.account.Index, formatAlpha(now))
	return true, nil
}

// paid finds and prints the claim's Claimed event from vault2 in receipt,
// whose caller must be caller.
func (c *v2Claim) paid(receipt *types.Receipt, vault, caller common.Address) bool {
	for _, log := range receipt.Logs {
		if log.Address != vault {
			continue
		}
		event, err := vault2.UnpackClaimedEvent(log)
		if err != nil || event.NoId.Cmp(c.noID()) != 0 || event.Index.Cmp(c.index()) != 0 || event.Caller != caller {
			continue
		}
		fmt.Printf("Claimed: pool %s, account %s, root %s\n", event.NoId, event.Index, event.RootIndex)
		fmt.Printf("  coldkey:     %s\n", renderColdkey(event.Coldkey))
		fmt.Printf("  cumulative:  %s\n", formatAlpha(event.Cumulative))
		fmt.Printf("  paid:        %s\n", formatAlpha(event.Amount))
		fmt.Printf("  caller:      %s\n", event.Caller.Hex())
		return true
	}
	return false
}

// sendV2Claim sends one claim to vault2 and reports its Claimed event.
func sendV2Claim(ctx context.Context, sender *v2Sender, reader *v2Reader, claim *v2Claim) error {
	item := claim.item()
	receipt, err := sender.send(ctx, "claim", vault2.PackClaim(item.NoId, item.RootIndex, item.Account, item.Proof), nil, ClaimBatchGasPerClaim, claim.describe)
	if err != nil {
		if !v2Reverted(err) {
			return err
		}
		done, rerr := claim.done(ctx, reader)
		if rerr != nil {
			return errors.Join(err, rerr)
		}
		if !done {
			return err
		}
		return nil
	}
	if receipt == nil {
		return nil // dry run
	}
	if !claim.paid(receipt, sender.vault, sender.from) {
		return fmt.Errorf("tx %s succeeded but %s emitted no Claimed for pool %d account %d", receipt.TxHash, sender.vault.Hex(), claim.account.NoID, claim.account.Index)
	}
	return nil
}

// sendV2Batch claims two or more accounts through the claim batcher, in
// rounds of at most ClaimBatchMaxClaims claims. Each round preflights its
// claims alone (PrepareClaimBatch): a refusal is done when another sender
// claimed first and otherwise fails that account alone. A claim the batch did
// not attempt, or that did not fit, goes in a later round; a last one goes
// alone to vault2.
func sendV2Batch(ctx context.Context, sender *v2Sender, reader *v2Reader, batcher common.Address, claims []*v2Claim) error {
	var failures []error
	for len(claims) > 1 {
		round, later := claims[:min(len(claims), ClaimBatchMaxClaims)], claims[min(len(claims), ClaimBatchMaxClaims):]
		items := make([]stabi.STClaimBatcherClaim, len(round))
		for i, claim := range round {
			items[i] = claim.item()
		}
		refusals, batch, err := PrepareClaimBatch(ctx, sender.client, sender.from, batcher, sender.vault, items)
		var passed []*v2Claim
		for i, refusal := range refusals {
			if refusal == nil {
				passed = append(passed, round[i])
				continue
			}
			refusal = fmt.Errorf("pool %d account %d: preflight: %w", round[i].account.NoID, round[i].account.Index, refusal)
			if v2Reverted(refusal) {
				done, rerr := round[i].done(ctx, reader)
				if rerr == nil && done {
					continue
				}
				refusal = errors.Join(refusal, rerr)
			}
			failures = append(failures, refusal)
		}
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if batch == nil {
			claims = append(passed, later...)
			if len(later) == 0 {
				break
			}
			continue
		}
		inBatch := make([]*v2Claim, len(batch.Items))
		for i, index := range batch.Items {
			inBatch[i] = round[index]
		}
		receipt, err := runTx(ctx, sender.client, sender.chainID, txRequest{contract: batcher, from: sender.from, key: sender.key, calldata: batch.Calldata, gasLimit: batch.GasLimit, dryRun: sender.dryRun}, func(uint64, error) {
			fmt.Printf("settlement v2 batch claim intent\n")
			fmt.Printf("  vault:       %s (chain id %s, rpc %s)\n", sender.vault.Hex(), sender.chainID, sender.rpcURL)
			fmt.Printf("  batcher:     %s\n", batcher.Hex())
			fmt.Printf("  from:        %s\n", sender.from.Hex())
			for _, claim := range inBatch {
				claim.describe()
			}
			fmt.Printf("  claims:      %d of %d, each capped at %d gas\n", len(inBatch), len(passed), batch.GasPerClaim)
			fmt.Printf("  gas limit:   %d (the least that attempts every claim, by eth_call, + 20%%)\n", batch.GasLimit)
		})
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if receipt == nil {
			return errors.Join(failures...) // dry run
		}
		outcomes, err := ClaimBatchOutcomes(receipt, batcher, len(inBatch))
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		var rest []*v2Claim
		for i, outcome := range outcomes {
			claim := inBatch[i]
			switch {
			case !outcome.Attempted:
				rest = append(rest, claim)
			case outcome.Amount == nil:
				done, rerr := claim.done(ctx, reader)
				if rerr != nil || !done {
					failures = append(failures, errors.Join(fmt.Errorf("pool %d account %d: tx %s refused its claim: %s", claim.account.NoID, claim.account.Index, receipt.TxHash, v2RefusalName(outcome.Selector)), rerr))
				}
			case !claim.paid(receipt, sender.vault, batcher):
				failures = append(failures, fmt.Errorf("tx %s paid pool %d account %d but %s emitted no Claimed for it", receipt.TxHash, claim.account.NoID, claim.account.Index, sender.vault.Hex()))
			}
		}
		if len(rest) == len(inBatch) {
			return errors.Join(append(failures, fmt.Errorf("tx %s attempted none of its %d claims", receipt.TxHash, len(inBatch)))...)
		}
		claims = append(append(rest, passed[len(inBatch):]...), later...)
	}
	for _, claim := range claims {
		if err := sendV2Claim(ctx, sender, reader, claim); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// v2RefusalName names a batched claim's revert selector: one of vault2's
// errors, or the bytes. A claim without revert data ran out of its gas, as a
// runtime refusal leaves it.
func v2RefusalName(selector [4]byte) string {
	if selector == ([4]byte{}) {
		return "no revert data (out of gas, as a runtime refusal leaves a claim)"
	}
	if parsed, err := parsedVault2ABI(); err == nil {
		if custom, err := parsed.ErrorByID(selector); err == nil {
			return custom.Name
		}
	}
	return fmt.Sprintf("0x%x", selector[:])
}
