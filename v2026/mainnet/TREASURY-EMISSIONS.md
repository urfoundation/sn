# Native treasury emissions — October 5, 2026

The selected launch policy is **10% of the native miner allocation for providers
and 90% received by `ur-reserve` for future network improvements**. The user has
specified that `ur-reserve` only receives funds and does not send funds. The
receive-only launch uses the supplied native account and public recipient
hotkeys. It does not require multisig reconstruction, signatory identities,
Ledger device configuration or qualification of reserve spending. This
supersedes the September 27 owner-recycle choice and the earlier mandatory
reserve-signing workflow.

Retain the full miner tranche for distribution instead of deliberately recycling
its remainder. The receive-only descriptor, signed validator routing and native
accounting are distinct from the optional multisig sending tools below. Actual
recipient registration, runtime/profile qualification, signed policy, deployment
and activation remain required. Historical qualification evidence retains its
original scope.

## Native routing and custody

The earlier runtime review covers v470, source
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`. Its owner-directed incentive branch
recycles or burns; it never credits a treasury. Recycling reduces outstanding
alpha and issuance. Changing `RecycleOrBurn` cannot select a wallet.
[Distribution source](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/run_coinbase.rs#L659),
[alpha accounting](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/alpha.rs#L38).
Current launch still requires the actual runtime-473 capture/replay profile and
its independent authority; these retained v470 references do not qualify it.

Use the supplied native treasury account as the coldkey owning at least two
ordinary registered SN25 recipient hotkeys. Native incentive reception requires
no reserve or recipient signature. The account must differ from `SubnetOwner`;
neither recipient may belong to that owner's registered `OwnedHotkeys` or equal
`SubnetOwnerHotkey`. The existing signed `32768/65535` per-recipient cap requires
at least two usable treasury UIDs for a 90% row, initially 45% each. In an epoch
with no provider weight the validator submits the reserve-only row instead: the
recipients share the whole row equally (half each with two, still under the cap)
and providers get nothing. These are explicit treasury recipients, never
invented provider contributions. If they are not already registered to the
supplied account, one-time external registration is a prerequisite. Registration
uses ordinary coldkey authority, subject to current eligibility, capacity, fees
and collateral; a public descriptor cannot register a hotkey or invent its
ownership or generation. No chain-root allocation override is assumed.
[Registration source](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/subnets/registration.rs#L70).

Ordinary miner rewards become native alpha stake controlled by the recipient's
coldkey. Record collateral capture separately from withdrawable stake. This is
not an automatic free-TAO, ERC20 or EVM-wallet payment. Future spending requires
the treasury's authorized native transfer or conversion, with its locks, fees
and receipts. Optional `AutoStakeDestination` changes the staking hotkey while
keeping coldkey ownership. Select and qualify that policy explicitly: growing
stake on treasury miner hotkeys can affect top-k validator permits. Preserve
independent validator participation and account for any additional dividends.
[Autostake authority](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/macros/dispatches.rs#L1949),
[permit selection](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/epoch/run_epoch.rs#L673).

The user selected reserve public account
`5CcHGEqKK3RXeEA2sVycHQAQGrqsyhWaYu9FjGtDVN6nwMwR`, which decodes under
SS58 prefix 42 to native AccountId32
`0x1815103f41a8d1e24c55d380c6f843fb36d715b4322a4e4f02bff36dfe74a410`.
Its checksum and canonical encoding round trip were checked using the same
public Base58/BLAKE2b rule as [the existing SS58 helpers](../ss58/ss58.go).
This exact AccountId32 is the receive-only destination; accepting it does not
depend on deriving it from signatories. The earlier **2-of-3 multisig** statement
is optional sending-custody information. If that workflow is selected later,
its complete actual signer set must derive the same account under the strict
custody contract. No recipient hotkeys or registered UID generations have been
supplied here.

The treasury CLI accepts a **public destination descriptor only**: network,
genesis, account and recipient hotkeys. Its input contains no signatory or
device references, coldkey or hotkey seed, private key, mnemonic or online
signer. If `vault/main/sn.yml` contains root keys or any other private fields,
extract a separate public-only descriptor and pin its exact bytes. Never pass
the combined vault file to a public descriptor reader or output command. The
CLI takes an explicit absolute path; it neither loads a vault resource
implicitly nor extracts a nested descriptor. This documentation does not read
or create the actual vault file or choose missing identities. Validators
receive the public policy projection below.

Direct native treasury custody requires no new Solidity contract.
[`STSettlementVault`](../evm/src/STSettlementVault.sol) continues to secure the
provider pool's claims. [`STReserveSink`](../evm/src/STReserveSink.sol) remains an
immutable one-way deposit sink with no spending operation. Neither its principal
nor unclaimed vault balances become treasury funds. An EVM treasury alternative
would require separately qualified ownership, precompile and withdrawal behavior.

## Public configuration contract

For a destination with no registered recipients, the [runtime-473 setup note](TREASURY-RECEIVE-SETUP.md) describes the selected separately funded source-account setup. The reserve makes no outgoing payments, including setup fees; reserve-funded registration is unselected. A fresh source-coldkey transfer has the observed 36,000-block delay. Preserve its destination eligibility by leaving interim alpha stake deposits out of the setup. Receiving requires no reserve signatory configuration.

The [public destination reader](treasury_destination.go) opens an explicitly selected absolute file path
under an independently accepted SHA-256 pin. Use the mainnet tool's
`treasury describe --destination FILE --destination-sha256 HASH` command, with
`FILE` set to the actual absolute path of the public-only descriptor snapshot.
The CLI has no `Vault.SimpleResource("sn.yml")` lookup or environment fallback.
Describe validates only the public descriptor.

The independent `Config.SimpleResource("sn.yml")` reader in Server parses the
public earnings schedule strictly. Do not insert the following fields into
`config/main/sn.yml`, replace that schedule, or reuse `st.yml`'s legacy
`treasury_hotkey` deposit-staging field.

The receive-only descriptor has this separate schema. The empty genesis is a
placeholder requiring the independently approved native hash. An empty recipient
list is valid for describing the known destination; it does not admit routing.
The descriptor accepts up to 100 recipient hotkeys. When known,
`recipient_hotkeys` contains canonical AccountId32 strings sorted by raw bytes,
not device records or invented UID/generation values.

```yaml
schema: urnetwork-native-treasury-destination-v1
profile: mainnet
netuid: 25
genesis_hash: ""                       # independently approved native hash
account_id: "0x1815103f41a8d1e24c55d380c6f843fb36d715b4322a4e4f02bff36dfe74a410"
recipient_hotkeys: []                  # actual public hotkeys when supplied
```

Account IDs and native hashes use canonical nonzero `0x`-prefixed lowercase
32-byte hex, matching native admission. An SS58 display address must decode to
the same account; a 20-byte EVM address is not a substitute. Recipient hotkeys
must be unique and distinct from the destination account. No signing-custody
fields are accepted in this descriptor.

`treasury observe --destination FILE --destination-sha256 HASH --input INPUT`
uses the explicitly supplied route and runtime/network inputs to authenticate
the account's recipient state at a canonical finalized block. Observation
requires at least two declared public recipient hotkeys.
`treasury policy-plan --destination FILE --destination-sha256 HASH --input INPUT`
requires the complete authenticated roster: at least two registered hotkeys,
their actual `Owner`, UID and registration generation, and exact owner-set
exclusion. Its result remains unsigned with `approved: false`. Missing
registrations require the one-time external registration and a fresh observation;
the receive-only path neither creates registrations nor invokes reserve signing.

The [receive-only input JSON](treasury_destination_command.go) uses schema
`urnetwork-native-treasury-destination-input-v1`, with `policy`, `destination`
and `owned_route`. Policy planning additionally uses the retained `observation`
and exact `runtime_metadata_scale`. The independently pinned destination file
may supply the `destination` field; if both are provided, they must agree.
The input contains no reserve signing action, signer nonce, deposit or fee
allowance. `--destination` and `--custody` cannot be selected together.

The generated economic policy uses
`urnetwork-native-treasury-receive-policy-v1`. Its compatibility carrier
`multisig_account` holds the receiver; `threshold: 0` and `signatories: null`
assert no sending custody. The original strict multisig policy schema keeps its
own derivation and hash rules. The separately signed receive-only policy binds
these public fields; parsing a destination or preparing an unsigned policy does
not authorize emissions:

| Public policy field | Required meaning |
| --- | --- |
| Network/runtime and predecessor policy | Exact approved network, runtime/source, advancing policy/activation boundary and unchanged historical authority |
| Treasury account | Exact supplied native AccountId32, selected for receive-only routing; no signing custody or device paths |
| Treasury recipient roster | At least two exact hotkey/UID/registration generations owned by that account; no provider-role overlap |
| Allocation denominator | Full actual native miner allocation before distribution, excluding other emission tranches and principal |
| Distribution fractions | `1/1` distributed: providers `1/10`, treasury `9/10`; no deliberate owner recycle or deferred provider liability. An epoch with no provider weight submits the reserve-only row instead: treasury `1/1`, providers `0` |
| Per-recipient cap and assurance | Existing `32768/65535` cap and observed-native-target with exact runtime tolerance; theta applies only to providers |
| Auto-stake destination | Omitted means authenticated absence; a supplied public AccountId32 must match original native storage exactly |

Owner-set exclusion, identity uniqueness and cap checks are mandatory validation,
never caller-disableable flags. Reject unknown fields, duplicate keys/identities,
multiple YAML documents and excessive input. Require authenticated native
ownership and registration readback; the supplied address or successful parsing
alone supplies neither. Multisig derivation remains mandatory only for the
separate selected multisig custody/policy mode.

## Optional hardware multisig execution

This workflow is unselected for the receive-only launch. The existing
`urnetwork-native-treasury-custody-v1` schema and
`--custody FILE --custody-sha256 HASH` interface remain strict and separate. They
require the complete native multisig account, threshold, sorted public signatories and
recipient device references; the destination descriptor cannot substitute for
them or supply spending authority. The earlier 2-of-3 confirmation may be used
only with its actual complete signatory set, whose derivation must equal the
supplied account.

Custody device references use the `path`/`bytes`/`sha256` shape: absolute
owner-local path, positive exact byte count and canonical `0x`-prefixed SHA-256.
They identify public Ledger configuration, never commands or keys. The custody
descriptor requires two through 100 recipient hotkeys, distinct from its native
account and every signatory. These requirements apply only when this optional
registration, liquidation or sending workflow is selected.

Pinned v470 installs native `Multisig` at pallet index 13 using SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a` (`pallet-multisig` 41.0.0).
Its deterministic account derives from the sorted complete signatory list and
threshold. Threshold execution dispatches the inner call with the multisig
account as signed origin, so ordinary registration can make that account the
hotkey owner. This is independent of EVM Safe.
[Runtime configuration](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/runtime/src/lib.rs#L555),
[native multisig source](https://github.com/RaoFoundation/polkadot-sdk/blob/cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a/substrate/frame/multisig/src/lib.rs#L618).

Prepare bounded registration calls and explicit multisig approvals offline. Each
owner signs their exact outer extrinsic on their local Ledger; a coordinator may
retain and submit only returned signed bytes. Bind the original call hash,
signatory, threshold, other-signatory order, nonce, mortal era, runtime metadata,
maximum weight, deposit/fee allowances and initial approval timepoint. Preserve
pending approvals and unknown results across restart; cancellation belongs to
the original depositor. An outer successful extrinsic is insufficient: require
the matching `MultisigExecuted` inner result and actual registration/ownership
or spending readback.

The [treasury commands](treasury_command.go) implement this separation:

| Stage | Commands and retained authority |
| --- | --- |
| Read and review | `describe --custody FILE --custody-sha256 HASH` validates the strict custody descriptor. Multisig `observe`, `plan` and `policy-plan` consume an explicit `--input` JSON, optionally binding that descriptor with the same custody flags. Observation pins native finality; plans remain unsigned. `policy-plan` returns `approved: false`. Receive-only `--destination` supports `describe`, `observe` and `policy-plan`, and supplies no action-plan or signing authority. |
| Permanent host custody | `reserve`, `export`, `import-reply`, `status`, `reconcile` and `submit` require the original signed `--config`, independent `--approval-key`, `--accept-action-hash` and `--production-authority-hash`. Export retains original metadata; imported replies require an exact file pin. Unknown outcomes retain the original request and bounded submission allowance. |
| Owner-local Ledger | `inspect-request`, `ledger-plan` and `sign` require the original `--request`, independently accepted request hash, signatory account, genesis and approval key. `sign` additionally requires the descriptor's exact `--device-config` and permanent `--owner-state`; no host private key is accepted. |

The device reference resolves to a public
`urnetwork-native-treasury-ledger-device-v1` JSON containing the expected account,
derivation path, absolute Python/helper/backend paths, helper/backend SHA-256
pins and app version. The supported signing path is Ed25519 at
`m/44'/354'/account'/0'/index'`. The local journal retains a returned signature;
an unresolved device issuance cannot trigger a fresh signing call. A verified
signature alone does not prove Ledger provenance; qualify the actual device
workflow before using these optional custody operations. This qualification is
not a reserve receiving prerequisite.

Supported inner calls are bounded SN25 `register_limit`, `remove_stake_limit`
with partial execution disabled, and `transfer_keep_alive`, wrapped in native
`approve_as_multi`, `as_multi` or `cancel_as_multi`. Liquidation checks both the
selected stake position and coldkey-wide availability, including collateral.
Cancellation retains the original depositor and timepoint. There is no
auto-stake setter in this command path: begin with an absent destination or
separately authorize its native management, then bind its exact observed value
in the economic policy. Source implementation does not establish successful
hardware signing, native execution or launch authority.

## Allocation and evidence

Let `M` be cumulative actual native miner allocation before distribution, in
alpha atomic units, over exact activated intervals. Preserve the cumulative
provider reference `P = floor((M - R) / 10)` and define treasury reference
`T = M - P`, where `R` is the miner allocation of reserve-only epochs (zero when
there are none). The complete allocation witness identifies those epochs by
their actual weight inputs, never by their outcome, and mainnet conformance
reports their sum as `reserve_only_miner_allocation_alpha`.
Theta divides only `P` between provider head and tail; paid/free completed traffic
keeps equal weight. Treasury retention creates no deferred provider entitlement.
Exclude owner cuts, validator/root dividends, deposits and existing principal
from `M`. Separate earned collateral capture, truncation dust and any actual
unplanned recipient or owner-withholding outcome.

The [original native availability query](historical_principal.go) retains the
runtime API's raw SCALE response and coldkey-wide total, locked and available
alpha at both boundaries. `StakeInfo.locked` alone is insufficient on v470.
Captured rewards are already staked and must not be added to principal twice;
available alpha also accounts for collateral. Deduplicate a shared coldkey's
availability across recipient queries. Complete custody reconciliation requires
the retained stake positions and original causes to cover that coldkey's total;
untracked positions leave the custody conclusion unqualified. Later liquidation
and spending are separate native outflows, never negative provider earnings.

When complete principal causes are unavailable, select the observer's explicit
[principal original retention policy](ECONOMIC-CONSERVATION-STATUS.md#principal-original-retention)
to keep exact evidence in bounded durable segments while native observation
continues. This operational selection preserves unknown custody causes and
spendable bounds. It creates no reserve signing action, provider claim collateral
or additional treasury earning authority.

The submitted 90/10 row, like a reserve-only row, is a target. Yuma masks, other
validators, clipping, normalization and integer rounding determine actual
incentive. Preserve the observed-native-target assurance and runtime-derived
tolerance; no hard payout guarantee follows from owner control. Removing deliberate owner withholding
removes that component of `MinerBurned`; subsequent price-share renormalization
and emission gating still determine the subnet's allocation.
[Yuma source](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/epoch/run_epoch.rs#L750),
[emission shares](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/subnet_emissions.rs#L350).

## Implementation and launch consequence

The merged [validator successor](../validator/TREASURY-PRODUCTION.md) uses an
explicit `treasury_approval` selector, distinct signed domains and the complete
ordinary recipient roster through measured preparation, submission and recovery.
It preserves original owner-recycle bytes and read-only historical authority.
The receive-only successor admits the supplied account and authenticated
recipient roster without a reserve signing configuration. The existing strict
multisig parser and custody commands remain optional. Native accounting and
monitoring retain explicit treasury schemas; legacy recycling and residual
amounts retain their historical meaning. The integrated receive-only routing,
accounting and original availability capture/replay path still require execution
qualification together. Existing owner-recycle and multisig results remain
evidence for their original scope; they do not qualify the receive-only successor
or establish a live treasury outcome.

1. Accept the supplied destination account and obtain the public recipient
   hotkeys. If necessary, complete one-time external registration. Bind exact
   coldkey ownership, UID/hotkey generations, owner-set exclusion,
   masks and caps at the applicable native execution boundary. Ordinary treasury
   registrations lack owner immunity: monitor pruning, re-registration,
   ownership changes and permit effects. Missing recipients must fail the
   complete row. Retain prior stake positions through recipient replacement or
   destination rotation; reconcile pending work before replacing its authority.
2. Prove provider and treasury outcomes independently using
   [execution accounting](economic_native_accounting.go), original native
   receipts, collateral and custody reconciliation. A synthetic original
   capture/replay fixture qualifies mechanics only; it does not admit the live
   runtime or demonstrate a mainnet 10/90 outcome.
3. Qualify the changed path, ownership/generation failures, pruning/recovery,
   cap/mask failures, consensus divergence and treasury conservation. The owner
   recycle-mode transition is no longer a prerequisite for the ordinary treasury
   remainder; any actual withholding remains measured. Reassess deployment and
   readiness using the completed successor, then observe the required native
   intervals and settlement/claim cycle after authorized activation.

Keep the existing 38-requirement ledger and retained qualification evidence.
This decision changes the economic outcome those requirements must demonstrate;
it supplies no additional test pass or launch authority. Canonical
`config/main/sn.yml` remains `activation: blocked`. The inclusive October 6
new-earnings boundary and all pre-cutoff USDC obligations remain unchanged.
