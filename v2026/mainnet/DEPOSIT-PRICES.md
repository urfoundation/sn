# Turning on demand deposit prices

How the owner puts a price on SN25's demand deposits, or changes it, on the sn side. The rules are in
[WHITEPAPER.md](../WHITEPAPER.md) §7.1: rates and the per-epoch cap come from entries of the signed price sheet at
<https://ur.xyz/price.yml>, the coordinator's own caps must allow them, and the signed policy document does not change.
The first signed entry, from epoch 7, is in [`price.yml`](../price.yml).

Choose the rates for every tier, the per-epoch cap `C` in α, and the first priced epoch `E`. Leave time for every step
below to finish before `E` begins, and take them in this order.

## 1. Run the build that reads signed entries

Every validator, auditor and archive reader must run the build that understands signed price sheet entries before the
first entry takes effect. Its decoders are strict, and once it has retained a priced decision it cannot be rolled back
to an earlier build.

## 2. Check that usage statements are publishing

Required: the first priced epoch `E` prices epoch `E − 1`, so the usage statement for `E − 1` must be published during
`E`'s own root window, in the first hours of `E`, after `E − 1`'s root is committed. The server's statement step must
already be running when `E − 1` ends, because it captures `E − 1`'s user count at that boundary. The server builds the
statement from the provider chunks the committed header lists and sizes the deposit from it, so the deposit follows the
commit.

Recommended: have it running an epoch earlier, so that the statement for `E − 2` appears and shows the step works before
it matters.

## 3. Raise the coordinator's caps

The governance Safe calls `schedulePolicy`, as it did at launch: a direct `execTransaction` from the owner
([as launched](LAUNCH.md#as-launched--october-8); [the policy and its schedule](LAUNCH.md#produce-the-protocol-policy-hash)).
Build the new snapshot from the one in force, `policyAt(currentEpoch())`, and change only:

- `effectiveEpoch`, to any epoch up to `E`. The coordinator refuses an epoch at or before the current one or the last one
  scheduled, and computes `effectiveBlock` itself.
- `epochDepositCapRao`, to at least `C` in rao (1 α is 10^9 rao).
- `campaignDepositCapRao`, the lifetime limit on all deposits and conviction locks ever made, by every operator together:
  196 α at launch. Raise it to cover many epochs of deposits: at least `campaignReserved()` plus the new epoch cap for
  each operator in each epoch until the next raise, plus any conviction locks expected in that time. It may not be below
  the epoch cap. A deposit past it reverts.

Everything else, `policyHash` included, stays as in force. Validators stop unless the coordinator's epoch cap is at least
the cap in force, the entry's or the signed policy's while no entry is in force, and its campaign cap is at least the
larger of the signed policy's campaign cap and that epoch cap. So a raise may take effect at any epoch up to `E`, but not
after it. Read `policyAt(E)` back and check both caps. The coordinator keeps at most 64 policy versions in all, so make
each raise last.

## 4. Sign and publish the entry, well ahead of `E`

In this repository:

1. In [`price.yml`](../price.yml), set `tiered_per_block_demand_deposits` to the new rates and update the comments to
   match. The tool builds the line to sign from the rates it finds there.
2. Print the line to sign. The owner supplies the signing key's derivation path:

   ```bash
   go run ./cli/pricesheet message --in=price.yml --epoch=E --cap=C --rpc="$RPC" \
     --ledger-path="<the owner key's derivation path>"
   ```

   `$RPC` is an EVM RPC endpoint for chain 964. The tool refuses an `E` at or below the newest entry, or not after the
   current epoch.
3. Run the `cast wallet sign --ledger` command it prints, on the Ledger that holds the pinned signer, the governance
   Safe's owner key. Approve only if the device shows exactly the printed line.
4. Attach the signature, then check the whole file:

   ```bash
   go run ./cli/pricesheet attach --in=price.yml --epoch=E --cap=C --signature=0x... --rpc="$RPC"
   go run ./cli/pricesheet verify --in=price.yml --rpc="$RPC"
   ```

5. Merge `price.yml` to `main`. The site serves its own copy, refreshed from this file outside this repository
   ([OPERATOR-DISCOVERY.md](../docs/OPERATOR-DISCOVERY.md#8-urxyz) describes the same model for `operators.yml`).

A signed entry is permanent. Readers refuse a file that drops or changes one, or adds one before an entry they hold, so
a mistake can be corrected only by a later entry.

## 5. Verify the live file

```bash
go run ./cli/pricesheet verify --url=https://ur.xyz/price.yml --rpc="$RPC"
```

It reads the published file as validators and operators do: every entry must verify under the pinned key and the
top-level rates must equal the newest entry, and it says which entry applies now. Check that the new entry is there
well before `E` begins, so that every validator and operator has read it when it takes effect.

## What to expect

Every priced epoch starts with no pool weight until the source epoch's root is committed (within the 1,200-block root
window, about four hours on mainnet), its usage statement is published and the exact deposit has landed.

Validators read the sheet at most every five minutes. An entry published late, after the server has already skipped an
epoch's deposit at zero price, is picked up within about 15 minutes.

A bad or missing statement, ledger object or deposit costs only that operator's pool its weight on a steering pass:
nothing an operator controls can stop steering.

## Later changes

- A new price repeats step 4 with a later `E`. Raise the caps first, to take effect no later than that `E`, only if the
  new cap exceeds the coordinator's epoch cap or the campaign cap would not last.
- To stop charging, sign a later entry whose rates are all zero. It returns to the zero-price mode. Its cap must still be
  nonzero and within the coordinator's epoch cap, so leave the caps as they are.
