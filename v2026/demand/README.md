# demand

Package `demand` keeps an operator's alpha wallet able to pay demand deposits. The operator has one Bittensor alpha wallet and one Solana USDC wallet. A deposit is paid from the alpha wallet. When that wallet is short, `Funder.Ensure` converts USDC into SN25 alpha over an on-chain route.

## The alpha wallet is the deposit signer

The alpha wallet is an EVM account on Bittensor. It must be the coordinator's deposit signer, on the server's deposit hotkey; the server's `stDemandBinding` refuses anything else. A deposit draws on that signer's stake on that hotkey, so alpha from any other source has to reach that position first. The operator gas policy already covers its gas, and its conversion transactions share the deposit's durable sender, which records each operation before signing, sends it once, and returns after finalized inclusion.

## The route

`Ensure` runs one step at a time until the wallet holds the alpha it needs. Each step first has the durable sender finish the wallet's earlier transactions, so that no balance is read from under one still mining. Then it reads the balances and uses what is furthest along: it unwraps wTAO of at least one bridge unit (1,000 rao), stakes TAO above the gas reserve if that covers the shortfall, bridges TAO on Solana if that covers it, and otherwise swaps USDC. Alpha is read again just before a bridge send or a swap, and a change starts the step over.

| Step | What guards it |
|---|---|
| 1. Jupiter swaps USDC for TAO on Solana. | First the later legs, so USDC does not move only to park TAO partway: the alpha wallet holds at least 0.0021 TAO, the bridge passes the checks of step 2, the USDC wallet holds `min_sol`, and a dry run of the smallest stake says the hotkey can take stake. Then the USDC balance and `max_usdc_per_epoch`. The order for this wallet, being the real route and price, must pay at least the floor: the USDC's worth at Coinbase's public TAO-USD best ask, less `max_price_gap`. Its minimum must too. An order paying enough with too low a minimum is placed once more, with slippage sized from its own output less 10 bps, and that one must clear both. `max_slippage_bps` caps slippage. Before signing, the fee payer must be the wallet or Jupiter's gasless payer, and a simulation must show exactly that USDC leaving, at least the floor of TAO reaching the wallet's own account, no delegate or close authority on either account, the wallet still a plain system-owned account, and at most 0.006 SOL spent. |
| 2. LayerZero OFT sends the TAO to wTAO (wrapped TAO) at the alpha wallet's address on Bittensor EVM. | The alpha wallet holds at least 0.0021 TAO, to unwrap what arrives. The Solana peer config and wTAO's peer must still name each other. `max_bridge_fee_sol` caps the fee, and the wallet must hold that plus 0.001 SOL. The amount is whole units of 1,000 rao (1 rao is 10^-9 TAO), and the minimum delivered equals it. A simulation of the unsigned send must show exactly that TAO leaving, the wallet still a plain system-owned account, and at most the cap plus 0.001 SOL spent. Only then is it signed. |
| 3. `withdraw` unwraps the wallet's wTAO to TAO. | The durable sender, with the amount read after earlier sends settle. Less than one bridge unit, or a wallet they have already funded, sends nothing: anyone can send the wallet wTAO, and dust must not make it spend gas. `AllowedAlphaWalletCall` admits `withdraw` only on wTAO. |
| 4. `addStakeLimit` stakes TAO into alpha on the deposit hotkey. | The durable sender. Alpha and TAO are read again after earlier sends settle: a wallet they have funded stakes nothing, and a stake they would shrink below 0.0021 TAO, about what the chain accepts, is not sent. The limit price is the pool's expected price after the purchase, plus `pool_price_tolerance`. The stake fills whole or not at all and leaves `gas_reserve_tao` unstaked. `AllowedAlphaWalletCall` admits only this hotkey and subnet. |

## demand.yml

```yaml
alpha_wallet:
  address: "0xALPHA_WALLET_EVM_ADDRESS"
  private_key: "ALPHA_WALLET_PRIVATE_KEY_HEX"
  hotkey: "0xDEPOSIT_HOTKEY_32_BYTES_HEX"
usdc_wallet:
  address: "USDC_WALLET_ADDRESS_BASE58"
  private_key: "USDC_WALLET_64_BYTE_SECRET_BASE58"
conversion:
  enabled: true
```

The file holds private keys and belongs in the operator's encrypted vault. Each key must derive its stated address, and unknown fields are refused. The server reads the file once per process, so a change takes effect at restart.

Every `conversion` setting is optional. Amounts must be finite numbers: `.nan` and `.inf` are refused.

| Setting | Default | Allowed | Meaning |
|---|---|---|---|
| `enabled` | `false` | | Must be `true` for USDC to be spent. |
| `max_usdc_per_epoch` | 6000 | above 0 | The most USDC swapped for one epoch's deposit. |
| `max_price_gap` | 0.01 | above 0, below 0.1 | How far above Coinbase's ask a swap may pay. |
| `max_slippage_bps` | 100 | 1 to 1000 | The cap on Jupiter's slippage. |
| `min_sol` | 0.06 | at least `max_bridge_fee_sol` + 0.007 | The least SOL needed to start a swap, enough for the swap and the bridge send after it. |
| `max_bridge_fee_sol` | 0.05 | above 0 | The cap on LayerZero's fee per send. Only the actual fee is charged. |
| `alpha_buffer_bps` | 100 | 0 to 1000 | How much more than the shortfall to buy, in case the price moves before the stake. |
| `gas_reserve_tao` | 0.2 | at least 0.02 | The TAO the alpha wallet keeps unstaked for gas. At the 30 gwei fee cap a deposit needs 0.102 TAO, and a staging and deposit pair 0.186. |
| `pool_price_tolerance` | 0.01 | above 0, below 0.1 | How far over the pool's expected price the stake may fill. |
| `jupiter_api_key` | none | | A key raises Jupiter's rate limit. |

## What the wallets must hold

| Wallet | Asset | How much |
|---|---|---|
| USDC | USDC | The whole purchase, sized from the missing TAO at Coinbase's ask plus 0.5%, and at least 1 USDC. Keep it at a few epochs' worth rather than far more: in samples RFQ pricing worsened about 0.6 bps per $1,000 of the taker's balance, and very large balances lost RFQ quotes entirely. The cause is unconfirmed. |
| USDC | SOL | Budget 0.06 SOL plus 0.025 per conversion: a bridge send spent about 0.023 SOL in a mainnet simulation, and the wallet's first swap pays 1,488,440 lamports of rent for its TAO account. It takes `min_sol` to start a swap, of which a swap may spend up to 0.006 SOL, and `max_bridge_fee_sol` + 0.001 to start a bridge send. A wallet holding only `min_sol` pays for one conversion and refuses the next. |
| Alpha | TAO | `gas_reserve_tao`. Hold it before the first conversion, because the unwrap pays gas from it: below 0.0021 TAO a swap or a bridge send is refused. A conversion also buys back what the reserve lacks. A wallet with alpha enough returns funded without looking at this, and the next conversion refills it, so watch `native_rao` against `gas_reserve_rao` in `Funder.Status`. |
| Alpha | Alpha | Nothing up front. Alpha left over stays for the next deposit. |

## When it refuses

`Ensure` returns an error and the deposit stops for now. The server records the reason as `fund demand deposit: <reason>`, and the deposit task retries. A refused step spends nothing. What earlier steps did stays in the wallets for the next run.

| Error | Meaning | What to do |
|---|---|---|
| `ErrDisabled` | The wallet is short and `enabled` is off. Nothing else runs, not even a stake of TAO already held. | Add alpha, or turn conversion on. |
| `ErrInsufficientUSDC` | The USDC wallet cannot cover the whole purchase, so nothing converts. | Add USDC. |
| `ErrInsufficientSOL` | Below `min_sol` for a swap, or below `max_bridge_fee_sol` + 0.001 for a bridge send. | Add SOL. TAO already on Solana waits there. |
| `ErrInsufficientGas` | The alpha wallet holds less than 0.0021 TAO, too little to unwrap what a swap or a bridge send would bring. | Add TAO to the alpha wallet. TAO already on Solana waits there. |
| `ErrHotkeyNotReady` | A dry run of the smallest stake failed, for example with `HotKeyAccountNotExists`. The error carries what the chain said. | Register the hotkey, or fix what the error names. |
| `ErrEpochBudget` | This swap and the epoch's earlier ones would pass `max_usdc_per_epoch`, or the request names an epoch older than the one the budget has moved on to, which spends nothing. | Wait for the next epoch, which resets the budget, or raise the limit. |
| `ErrPriceGuard` | Jupiter's order for this wallet pays less than the floor, or its minimum stays below the floor after one more order. | Let the retries run: a second order's price moves, and the next try often clears. Raising `max_price_gap` is no fix: it pays more for every swap, and in live probes at 0.015 it cleared only half of these refusals. |
| `ErrBridgeChanged` | A peer changed, or the Solana peer config or LayerZero's lookup table is missing. The TAO stays on Solana. | Find out what changed. Update `bridge/` only after confirming the new configuration. |
| `ErrStale` | A state write lost to another writer. Nothing is sent after a lost write. | A retry re-reads the state. If it persists, check that only one process is converting. |

A failed simulation is a plain error. A stale account list in `bridge/accounts.go`, after LayerZero changes its settings, shows up this way and needs a code update.

After 12 minutes of waiting, or 12 steps that did not fund the wallet, `Ensure` returns a `Result` with `Waiting` set and no error, and the server retries. A landed bridge send has no timeout: nothing more is bought until its wTAO arrives, though TAO the wallet already holds is staked meanwhile if it covers the shortfall. The log warns after 15 minutes, and `FAILED` or `BLOCKED` from LayerZero Scan shows in `Waiting` and the log. The send is never repeated. A delivery Scan reports as `DELIVERED` that the wTAO balance still does not show after 5 minutes counts as arrived, its wTAO having moved on.

## How a crash resumes

- **Balances decide.** `State` holds only the epoch, the USDC spent in it, and the journal below. Each step reads everything else from the chains.
- **Solana transactions are journaled before sending**, with their raw bytes and whichever of a last valid block height or an expiry time they have. On restart the signature's status decides, at confirmed commitment or above only: below that, even an error is one node's view of a block a fork can abandon.
- **A swap with no status waits until it can no longer land.** An AMM order has a last valid height, and expires 30 blocks past it. A JupiterZ RFQ order has none, and its signature exists only in `/execute`'s reply, so it expires 2 minutes past the order's `expireAt`. An order with neither expires 3 minutes after it was signed. Then the balances decide: TAO up by at least the floor means it landed. USDC only corroborates, since the operator may move it at any time. An expired swap whose TAO did not arrive and whose USDC did not leave, or a failed one, is cleared and returns its USDC to the budget.
- **A swap that landed, or whose USDC left, keeps its journal until its TAO shows.** A read behind the swap would otherwise look like a wallet with no TAO, and buying on it would buy twice, and balances cannot tell such a read from a withdrawal. So nothing more is bought, and past the first minute the log says `ALERT` at each look, until 30 minutes after the swap was signed. TAO reaching the floor at any point in that time books the swap as usual. At the end, TAO short of the floor is booked as what it is and logged. No TAO at all closes the swap with no `swapped` event and no refund, and a `swap_needs_person` event records it. The conversion then goes on from the balances, with that USDC still against the epoch's budget.
- **A bridge send with no status** is rebroadcast as the same bytes, which cannot land twice. Past its expiry, its TAO having left shows that it landed, and it is followed like any other. With its TAO still there it is cleared.
- **EVM transactions go through the durable sender**, which settles earlier transactions before preparing the next, so a resumed run sizes from balances they have changed.
- **Events follow the state**: each is recorded after the save that makes it final, so a kill can lose an event but not book one twice. A failure to record, even a panic from the store, is logged and stops nothing.

## Using it from a server

```go
cfg, err := demand.ParseConfig(raw)
funder, err := demand.NewFunder(cfg, netuid, demand.Deps{Store: store, EVM: evm, Sender: sender, Solana: solanaClient})
result, err := funder.Ensure(ctx, demand.Request{Epoch: epoch, NeedRao: needRao})
```

`Store` keeps `State` with a version check and appends an `Event` per step. `EVM` reads Bittensor EVM as the alpha wallet. `Sender` is the durable sender. `Solana` is the RPC (`solana.NewClient`). `Jupiter`, `Market` and `Scan` default when nil to Jupiter's API, Coinbase's public book and LayerZero Scan. `Logf`, `Now` and `Sleep` are optional.

The server calls `Ensure` in the `FUND` step of `StDepositForEpoch`, after the deposit's own limits pass, so USDC is not spent on a deposit they would refuse. It starts no staging transfer or conversion within 150 blocks of the deposit's deadline. `NeedRao` is the deposit plus 3 rao of staging allowance, less what is already staged. A step settles the sender before it reads alpha, so `NeedRao` has to be read after settling too: a staging transaction finishing in between would otherwise read as a shortfall to buy. With no `demand.yml` the step does nothing: the deposit draws on what the wallet holds, or fails. A `demand.yml` that does not parse or match the deposit signer stops the deposit.

The server's `st demand-status` command prints `Funder.Status` as JSON: balances, `gas_reserve_rao`, `conversion_enabled`, `hotkey_error` and the state. It signs nothing.

## Prerequisites

- **Priced deposits.** At zero price nothing is owed: `StDepositForEpoch` records a skipped publish before `FUND`, and nothing happens here. A price entry published after that skip is picked up within about 15 minutes. [`mainnet/DEPOSIT-PRICES.md`](../mainnet/DEPOSIT-PRICES.md) is how the owner turns prices on.
- **The deposit hotkey exists on chain.** Otherwise a swap is refused with `ErrHotkeyNotReady` before any USDC moves, and a stake of TAO already held fails with `HotKeyAccountNotExists`. Before enabling conversion, check `hotkey_error`, the result of the same dry run of the smallest stake. It needs that stake, 0.0021 TAO, in the wallet to speak to the hotkey at all.
- **The statement deposit sizing reads.** The server's sizing reads the operator's signed usage statement when the source epoch was settled by v2, and the signed payout artifact for earlier ones. It retries without reaching `FUND` until the one it needs exists. This limits sizing, not this package: the server's `st deposit --alpha_rao=<rao>` command names the size and skips the read.
- **A coordinator and a gas policy.** `FUND` runs only on a deployment with a coordinator. In production the durable sender denies new sends without an approved operator gas policy.

## Tests

`go test ./demand/...` from the repository root uses fakes and no network. `funder_recovery_test.go` kills a conversion at every store call and external call, with replies lost and landings delayed, on both shapes of Jupiter order, and checks that no resumed run buys or stakes more than once.

The live tests touch mainnet but sign nothing and move nothing, and each skips unless its variables are set. `demand/bridge/live_test.go` compiles a bridge send for a system-owned wallet holding TAO and SOL, and simulates it:

```sh
DEMAND_LIVE_SOLANA_RPC=<solana-rpc-url> DEMAND_LIVE_TAO_HOLDER=<solana-wallet> \
  go test ./demand/bridge -run TestLiveSendSimulates -v
```

`demand/live_test.go` checks the route against mainnet, Jupiter and Coinbase. It holds no key and refuses every call that could spend. Its variables are `DEMAND_LIVE_SOLANA_RPC`, `DEMAND_LIVE_EVM_RPC` and `DEMAND_LIVE_USDC_HOLDER`, with `DEMAND_LIVE_TAO_HOLDER` and `DEMAND_LIVE_SIZES` optional.
