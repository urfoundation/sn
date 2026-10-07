# Current root participation and offline capabilities

The reviewed v470 source represents a netuid-0 participant as a registered
root hotkey with root stake and a basket fund. Dividends accumulate where they
were earned. The source retires `set_root_weights`; unchanged accumulation
requires no periodic root-hotkey call. The `RootWeights` proxy allows no calls.
This corrects the historical assumption that completing the netuid-0 role must
include a recurring native root-weight signer. It does not make a running
observer evidence of actual registration, retained stake or earnings.

The standard UR `sn/validator` majority role and the required secondary role
keep their separate configurations, signatures and admissions. Netuid 0 cannot
count toward either UR role. The root hotkey retains its separately specified
other-hardware custody. Its device and signature interface remain unspecified.
Current root lifecycle operations need independently established root-owning
or staker coldkey custody, or an explicitly permitted proxy. None of these
identities inherits the subnet-owner Ledger, which remains owner-local setup
custody with no Snow access.

Unknown native devices do not block observation or unchanged participation of
an already registered and staked fund. They remain unknown in the report and
become execution prerequisites only for a separately approved mutation. The
participant still needs actual finalized membership, stake and accounting
evidence; removing an inapplicable device gate supplies none of that evidence.

## Exact evidence and its scope

This review uses official source commit
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d` and the already retained
[runtime470 artifact review](../docs/spec/runtime-470-audit.md). Its public
metadata is 354,056 bytes, SHA-256
`ccad189c41970e1d33fe665b697b4bc763bece0212e81e15a3035d4091c79b1d`
and BLAKE2b-256
`8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`.
The audited official Wasm has BLAKE2b-256
`5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`.
Executing that artifact's metadata API reproduced those metadata bytes in the
retained audit; no new execution is claimed by this source review.

Deterministic source-to-Wasm equality failed in that audit. The owner accepted
the exact official artifact and documented single-function randomness exception
for launch planning only. The report preserves that distinction: source proof,
current runtime verification, signing, deployment and activation are all false.
A supplied source commit identifies the behavior being reviewed; it cannot
prove that any supplied metadata was produced by that source or a current chain.

## Supported interfaces

The command checks the following exact `SubtensorModule` pallet-7 call indices,
field order, names and SCALE wire shapes. Signed origins and behavioral rules
come from the pinned source, since metadata does not encode origin permissions.
All listed mutations remain planning interfaces, not implemented current-root
signing or submission adapters.

| Call | Index | Reviewed role and effect |
| --- | ---: | --- |
| `root_register(hotkey)` | 62 | Owning coldkey registers the root hotkey and pays the current burn. |
| `add_stake(hotkey, netuid, amount_staked)` | 2 | Staker coldkey adds principal; the root plan must explicitly select netuid 0. |
| `remove_stake(hotkey, netuid, amount_unstaked)` | 3 | Staker coldkey removes principal subject to current unlocking and other admission rules. |
| `claim_root(subnets)` | 121 | Staker coldkey or admitted RootClaim proxy claims fund-level entitlements across its selected validator funds; `subnets` is ignored. |
| `claim_root_with_hotkey(hotkey)` | 148 | Staker coldkey or admitted RootClaim proxy claims one fund's entitlement into root stake. |
| `stake_into_basket(hotkey, amount_staked)` | 147 | Depositor coldkey buys basket entitlement; this is distinct from adding root principal. |
| `swap_basket(hotkey, origin_netuid, destination_netuid, amount, min_amount_out)` | 150 | Owning coldkey or admitted BasketTrading proxy changes holdings under runtime trade guardrails. |
| `swap_basket_many(hotkey, legs)` | 151 | Same trading authority; bounded sequence of `(u16, u16, u64, u64)` legs. |
| `set_auto_parent_delegation_enabled(hotkey, enabled)` | 135 | Owning coldkey changes future automatic parent delegation. |
| `decrease_take(hotkey, take)` / `increase_take(hotkey, take)` | 65 / 66 | Owning coldkey changes delegate take; increase has separate rate limits. |
| `set_children(hotkey, netuid, children)` | 67 | Owning coldkey changes delegation on a nonroot subnet, not netuid 0. |

Calls and signatures are defined in the pinned
[dispatch source][dispatches]; the exact
[proxy filters][proxy-filters] and [call groups][proxy-groups] determine permitted
proxy operations. The report rejects metadata containing retired
`set_root_weights`, `set_root_claim_type` or `sudo_set_num_root_claims`, instead
of mixing historical and current call families. It reports exact call-shape
compatibility; it does not validate signed extensions, a device, current proxy
permissions, economic bounds, or permission to execute a call.

## Registration, stake and earning conditions

Root capacity comes from `MaxAllowedUids(0)`, not a fixed guide example.
The [registration implementation][root-source] admits a free seat without a
displacement stake comparison. At full capacity it chooses the lowest-staked
nonimmune member and requires applicant root stake at least equal to that
member's root stake. If all seats are immune, registration fails. The
[pruning selector][pruning] resolves equal stake by older registration block,
then lower UID. Other registration limits, ownership and balance checks still
apply in both cases.

The upstream [Root Reborn guide][root-guide] says no prior stake is needed for
registration; the full-capacity implementation qualifies that statement.
Likewise, `add_stake` [requires an existing hotkey account][stake-validation].
A plan cannot assume a fresh, unregistered hotkey can always register first and
stake later, or invent an account-creation route to meet the full-capacity rule.
Finalized account, ownership, capacity, immunity and complete stake evidence
must select the actual admissible path.

`root_register` has no maximum-burn parameter. A read of the current burn is a
quote, not an on-chain cost cap. Finite fees, burn exposure and re-registration
need separately reviewed economic bounds and custody admission. Registration
makes an undelegated hotkey a delegate with default take 11,796/65,535 (about
18%); an existing delegate keeps its take. The registration path also sets
automatic child relations to existing subnet owners unless the root hotkey
already opted out. Changing the automatic-delegation flag affects future
automatic setup; it does not remove current delegation edges.

Root principal, the fund's holdings, staker entitlements and claimed proceeds
are separate quantities. The [root fund guide][root-guide] and
[claim implementation][claim-source] describe automatic in-place accrual and
explicit redemption. A claim sells the owed fraction subject to current
thresholds and dust rules, stakes the proceeds back on root and can refresh
the unlock interval. There is no current automatic-claim scheduler or required
trade cadence. Buying basket entitlement, claiming, changing take, changing
delegation and trading are separate economic choices; this review authorizes
none of them. Actual retained stake and accrued earnings must be shown by
current finalized evidence, not inferred from a fund guide or a process PID.

## Concrete completion plan

1. Bind the independently approved current chain, finality checkpoint, runtime
   artifact and metadata to an observation. Preserve the already documented
   planning-only artifact exception; do not upgrade it to runtime or deployment
   authority.
2. For an existing root participant, bind both UID mapping directions, hotkey,
   owning coldkey and exact registration generation. Read complete capacity,
   stake and immunity state at that same finalized hash. Establish current
   delegation and take, fund holdings, entitled stakers, relevant queued credits
   and observed accrual under the selected accounting rules. The existing
   passive preview contributes a seat/stake/delegation census but is not a
   complete basket entitlement or earning proof.
3. If a new seat, stake change or other mutation is actually needed, choose
   only the corresponding current call above. Establish its separate owner or
   staker coldkey/device or explicit proxy before implementing and admitting
   a finite original-request custody and receipt path. Require exact runtime,
   account, nonce, era, policy, fees, amounts and call-specific bounds. Do not
   send the historical root-weight payload to any device. Required mutation
   adapters are conditional on that approved transition; they are not a
   periodic-signer prerequisite for an unchanged existing participant.
4. Preserve exact signed bytes and durable custody through uncertain submission
   and reconcile finalized call-specific outcomes before any successor request.
   Require the post-state seat generation, stake, delegation and fund state
   matching the approved transition; a successful extrinsic alone is not the
   combined participant completion result.
5. Bind continuous observation and bounded, independently approved process
   recovery to that actual participant state. Detect seat replacement, low
   retention margin, stalled/forked finality, changed runtime, missing data and
   accounting gaps. The finite passive observer and its signed process repair
   do not themselves establish indefinitely supervised operation. Never
   turn process recovery into automatic re-registration, staking, claim or
   trading authority.
6. Join this root evidence with independently admitted majority and secondary
   UR validator evidence. Declare the user's combined outcome only after both
   protocol roles are actually evidenced. A metadata report closes none of
   those live outcome gates.

## Offline command

The finite `root-capabilities` command accepts one bounded local raw SCALE14
file, an independently supplied BLAKE2b-256 pin and the exact reviewed source.
It checks the hash before SCALE decoding. It opens no RPC, signer, device,
custody journal or durable-volume owner. For example, against the already
retained public artifact:

```sh
sn-mainnet root-capabilities \
  --metadata /path/to/observed-470-metadata.scale \
  --metadata-hash 0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34 \
  --runtime-source-commit 923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d
```

Exit 0 means the checked call interface is compatible. It does not mean a
participant is registered, staked, earning, supervised or ready to activate.
An independently pinned projection can be shape-compatible while
`audited_metadata_matched` is false. Exit 3 means a pin/profile refusal or an
incompatible call report; exit 2 means invalid arguments; exit 1 means local
input/output failure. Membership, root stake, basket accrual and custody stay
`unknown`; current capacity and both applicant/candidate stakes stay JSON
`null`. Unimplemented mutation adapters remain explicit and conditional. No
sign, RPC, approval, device or broadcast option exists.

The deterministic tests cover the retained identity-free metadata projection,
call index/name/type/order changes, basket-leg widths and recursion, missing and
retired calls, ambiguous registries, typed capacity/stake unknowns, the public
read-only route, hash-before-decode refusal, mutation-option rejection and
cancellation/source refusal. Tests are authored for independent execution; this
source review does not claim compilation or test-body qualification.

[dispatches]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/macros/dispatches.rs
[proxy-filters]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/runtime/src/proxy_filters/mod.rs
[proxy-groups]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/runtime/src/proxy_filters/call_groups.rs
[root-source]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/root.rs
[pruning]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/subnets/registration.rs
[stake-validation]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/staking/stake_utils.rs
[root-guide]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/docs/guides/root-reborn.mdx
[claim-source]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/staking/claim_root.rs
