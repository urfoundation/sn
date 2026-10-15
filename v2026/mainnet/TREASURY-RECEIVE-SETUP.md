# Receive-only reserve: recipient setup on runtime 473

## Selected path (owner decision, October 6): `ur-reserve` registers its recipients through its multisig

The owner changed the receive-only constraint. `ur-reserve`
(`5CcHGEqKK3RXeEA2sVycHQAQGrqsyhWaYu9FjGtDVN6nwMwR`) is the btcli multisig preset `ur-reserve`, 2-of-3 over these
signatories:
- `brien-ur-reserve`, `5FYohgZfJQqHgPDQzF8SZ2jxn8dveW5JEXoTYugjrTuHDKGW`, on Brien's Ledger at `m/44'/354'/9'/0'/0'`;
- `jack-ur`, `5GeoGiGEvUEQqTfTaUsvXqMQD4zVMNeYtYqMN8JLaMfiDp4J`;
- `keith-ur`, `5DFCNQmzRedo6hbZci4PTFMRuQJQ5PX6f6WrJBxDCDU3yBzS`.

It signs through that multisig and pays its own SN25 registration burns. There is no separate source account, no coldkey
swap and no 36,000-block wait.

How the registration works:
- **The call.** Each recipient is registered by `SubtensorModule.register_limit(25, hotkey, limit_price)` dispatched
  through `Multisig`. Threshold execution makes the multisig the signed origin, so `ur-reserve` pays the burn and becomes
  the hotkey's `Owner` directly.
- **Deposits and fees.**
  - The proposing signatory reserves the multisig deposit (DepositBase 0.132 TAO + 2 × DepositFactor 0.032 TAO = 0.196
    TAO), which is refunded when the call executes.
  - Each signatory pays its own fee.
- **Burn cap.** `limit_price` caps the burn; at the call below it is 0.01 TAO, against a 0.0005 TAO burn on October 6.
- **The recipients.** Their keys live in the hotkey-only btcli wallet `root/subtensor/wallets/ur-reserve`:
  - `recipient-1`: `5E4cTwgTGqRkEiiZZNJ6c5gKydg5oCWNQu6H3bufPCZhnvgv`
  - `recipient-2`: `5EoH9PUVrzAQ3vXaAGQXvhxWGGd3DkTEf9mno2YnGWJ8JHPH`

  Neither hotkey ever needs to sign or serve.

```bash
btcli call SubtensorModule.register_limit \
  --args '{"netuid": 25, "hotkey": "<recipient ss58>", "limit_price": 10000000}' \
  --multisig ur-reserve --ledger --ledger-account 9 --network finney    # Brien proposes
btcli call SubtensorModule.register_limit \
  --args '{"netuid":25,"hotkey":"<recipient ss58>","limit_price":10000000}' --multisig ur-reserve -w jack-ur   # second approval
btcli multisig pending --multisig ur-reserve --network finney
```

On October 6, at finalized block 9,227,258, `ur-reserve` held 0.1 TAO and `brien-ur-reserve` held 0.5 TAO, funded for
this. Brien's first approvals are on chain:
- `recipient-1`: extrinsic `9227287-0026`, call hash `0x70944ff0e05ce7ab605599c88fa2a6d9a5b92d5846449a229fc8a0b1d8bc8253`;
- `recipient-2`: extrinsic `9227291-0016`, call hash `0x1f64e8610b3d9777f8d66324a428caf314183ef684b0445a077206ddf9fa95cf`.

Both await a second signatory.

After both execute, authenticate everything before admitting the signed 10/90 policy:
- the reserve `Owner` of each recipient;
- their UIDs, reverse keys and registration generations;
- that they are outside the subnet-owner hotkey set;
- the auto-stake state.

**Register them close to launch.** The launch performs no owner trim. The recipients have zero emission until UR's
validators weight them, and only the 21,600-block immunity protects a new UID from ordinary pruning on the full subnet.
So hold the second approvals until the validators will weight the recipients within that window.

**SN25 ownership never moves.** `SubnetOwner(25)` is the `ur-owner` multisig
(`5HTeZ5168DjjGWZgbvzGfysEAj9fnexF24gYFKJHENU5cc8a`), and it stays there. No setup step may announce or perform a coldkey
swap from `ur-owner`: a coldkey swap is account-wide and would move subnet ownership with it. On October 6, at block
9,227,170, `ColdkeySwapAnnouncements` and `ColdkeySwapDisputes` held no entry for `ur-owner`.

The rest of this note records the earlier separate-source-plus-coldkey-swap design. It is superseded and not selected.

This is an unsigned setup note, not authority to sign, fund, register or swap any account. The selected policy remains 10% of native miner allocation for providers and 90% received by `ur-reserve`. Its public destination is `5CcHGEqKK3RXeEA2sVycHQAQGrqsyhWaYu9FjGtDVN6nwMwR`, AccountId32 `0x1815103f41a8d1e24c55d380c6f843fb36d715b4322a4e4f02bff36dfe74a410`. Receiving and monitoring require no reserve signatory list or device configuration. The user's receive-only instruction excludes reserve-funded setup fees as well as withdrawals: do not prepare reserve-origin registration or spending as the selected launch path.

The retained observation at block 9,219,009 found no reserve-owned hotkeys. Before routing, at least two actual SN25 recipients must be registered and owned by the reserve. Their `Owner`, UID/reverse key, registration generation and owner-set exclusion must pass the existing [treasury admission](TREASURY-EMISSIONS.md). The address alone supplies none of those facts.

At finalized block **9,219,288**, observed October 5 at 21:00 UTC, the effective coldkey-swap announcement delay is **36,000 blocks**: the storage value is absent and the exact runtime-473 metadata supplies that default. This is about five days at 12 seconds per block. The same metadata defines `KeySwapCost` as **100,000,000 rao (0.1 TAO)**; it is a runtime constant, not another storage entry. It excludes transaction fees and recipient registration costs. The [bounded observation receipt](/home/by/sn-mainnet-receive-setup473-20261005/20261005T210049Z/OBSERVATION.json), SHA256 `113ce19516d1610fe8adb7684682bd2e7029ff512febc90e6bee38dcd194a86e`, retains the selected block, current metadata and exact response bytes. It is endpoint-finalized evidence; no independent trie/GRANDPA verification is claimed.

| Setup choice | Signer and funds | Timing and required decision |
| --- | --- | --- |
| Separate coldkey registers both recipients, then transfers its ownership to the reserve | The separate coldkey signs and pays. The reserve only receives ownership/assets and signs nothing. | A fresh announcement waits 36,000 blocks and cannot support October 6 readiness. A previously announced transfer may differ, but no source account or announcement has been supplied or queried. |
| Excluded comparison: registration from the reserve account | A reserve-origin `register_limit(25, hotkey, limit_price)` call pays registration charges from the reserve even when another account sponsors transaction fees. | Excluded by the selected no-outgoing requirement, including setup fees. |

The first choice is supported by official source commit `f87cada631f81d11683e715a9f059f693992e64a`. After registering both recipients, the separate coldkey calls `announce_coldkey_swap` with the runtime hash of the reserve AccountId32. Once the announced block is reached, the same old coldkey calls `swap_coldkey_announced` with the reserve AccountId32. The announcement charges the old coldkey. No destination signature appears in either call. [Exact dispatch source](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/macros/dispatches.rs).

This is an account-wide transfer, not a transfer of two selected hotkeys. It moves owned hotkeys, stakes, remaining balance, subnet ownership and associated state. Use a dedicated source account whose entire transfer is intended; using the SN25 owner's coldkey would also transfer subnet ownership and violate the selected owner exclusion. The source must never be `ur-owner`, the chain's `SubnetOwner(25)`: SN25 ownership stays with it ([hard rule](LAUNCH.md#hard-rule-sn25-ownership-stays-with-ur-owner)). Admission requires the destination's `StakingHotkeys` to be empty, the destination not to be a hotkey, migration to be idle and the source to fit the runtime work bounds. Empty `OwnedHotkeys` alone does not establish these conditions. The swap rewrites `Owner` and `OwnedHotkeys`; actual SN25 membership and generations still require readback afterward. [Exact swap implementation](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/swap/swap_coldkey.rs).

**Preserve the setup order: do not make an interim `transfer_stake` or `transfer_stake_and_hotkey` deposit into the reserve before this coldkey swap.** Such a deposit can be signed and funded by the source and gives the reserve an alpha stake position, but [stake credit appends the destination's `StakingHotkeys`](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/staking/stake_utils.rs). The swap's `do_swap_coldkey_tracked` rejects a nonempty destination list. Clearing that position by reserve-origin transfer or unstaking would conflict with the selected no-outgoing rule. The earlier empty `OwnedHotkeys` observation does not establish an empty `StakingHotkeys` list. A balance deposit must not be presented as completed recipient setup.

The [same-subnet stake-transfer calls](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/staking/move_stake.rs) move an alpha position between coldkeys; they do not assign the miner hotkey's `Owner`. `transfer_stake_and_hotkey` also changes the position's staking hotkey, not the ownership of that hotkey. `AutoStakeDestination` retains the miner's original coldkey when [crediting emissions](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/coinbase/run_coinbase.rs), and [hotkey swaps](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/swap/swap_hotkey.rs) preserve the signing coldkey. None supplies a faster recipient-ownership transfer in the reviewed ordinary-call paths.

A source-funded alpha deposit is therefore distinct from the selected 90% ordinary native miner credit. Current [recipient observation](treasury_observation.go) requires actual reserve `Owner` values, and [treasury accounting](economic_native_treasury.go) admits the original `native-miner-credit` effect only when its recipient coldkey is the reserve. Do not relax either check or relabel a deposit as mining income. A future forwarding design would require its own explicit policy, source authority and executable transfer/accounting evidence; it is not an implemented substitute for this setup.

Complete registration before announcing: the [coldkey-swap guard](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/guards/check_coldkey_swap.rs) blocks ordinary calls from an announced coldkey and blocks all its signed calls while disputed. The [swap-cost accessor](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/utils/misc.rs) uses the runtime `KeySwapCost` constant reported above.

For the excluded reserve-funded path, runtime 473 derives the coldkey from the signed registration origin. It checks that account's TAO balance, pays the burn/collateral charge and registers the hotkey; `register_limit` bounds the current registration price before this shared path. Actual registration prices, collateral requirements and transaction fees have not been read or budgeted here. These one-time costs are separate from the later 90% alpha receipts, but remain outgoing reserve-funded activity if the reserve is the origin. [Exact registration implementation](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/subnets/registration.rs).

There is no legacy PoW shortcut: runtime 473 ignores the old `register` coldkey/work arguments and uses that same signed, funded registration path. `schedule_swap_coldkey` returns `Deprecated`; `swap_coldkey` is restricted to the chain's Root origin. Owning a root-validator key does not grant that origin. [Exact dispatch source](https://github.com/RaoFoundation/subtensor/blob/f87cada631f81d11683e715a9f059f693992e64a/pallets/subtensor/src/macros/dispatches.rs).

The selected constraint is **no outgoing reserve funds**. The reserve-funded registration alternative above is retained for comparison only and is not selected. Recipient setup must use a separately funded source account and a runtime-supported transfer that leaves the reserve receiving only. A fresh transfer has the observed delay above; no existing mature announcement, source coldkey, recipient keys or completed transfer has been supplied. Do not represent the destination address alone as completed native recipient setup.

The finite setup sequence is:

1. Reuse actual eligible reserve-owned SN25 recipients or an already eligible source announcement only after exact readback. The known reserve address needs no further confirmation.
2. Otherwise prepare a separately funded, dedicated source coldkey with both registered recipients. Read the reserve's actual destination eligibility, including `StakingHotkeys`, before any proposal to announce a swap; leave interim alpha deposits out of that proposal.
3. The operator's separately authorized setup must identify the complete account-wide transfer, source-funded costs and exact recipients. Register before announcing; execute the announced swap only when its actual eligible block is reached. A new announcement retains the observed 36,000-block delay.
4. Authenticate the resulting reserve `Owner`, registered UID/reverse keys and generations, subnet-owner exclusion and auto-stake state before admitting the signed 10/90 policy. No extra completed emission epoch is required to perform this setup.

After the selected setup completes, use the existing receive-only `treasury describe`, `observe` and `policy-plan` commands with the public destination and actual recipients. Preserve native ownership/eligibility readback and the independently signed runtime/economic approval. The October 6 new-earnings boundary remains an attribution rule; it does not supply registration, deployment or launch authority, and postactivation economic acceptance is not a pre-signing requirement.
