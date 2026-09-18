# Mainnet bootstrap program design

Track required prelaunch corrections in
[PRELAUNCH-FIXES.md](../mainnnet/PRELAUNCH-FIXES.md). Its first workstream replaces
the version-by-version runtime migration requirement below with automatic
compatible-upgrade handling. Those fixes are proposed and remain unimplemented.

Status: design only, 2026-09-14. This document specifies a future Go entry point at `mainnet/main.go`; that program does not exist yet. No mainnet transaction, node query, deployment, UID removal, or validator startup was performed for this design.

The design is based on SN commit `a59294e98ea02d05125015ae02cf32f2c0059c8a`. The separately running testnet campaign remains the prerequisite for promotion. Its successful local checks alone do not establish mainnet readiness or completion of `FINAL-2.md`.

## Requested outcome and decisions

The bootstrap must deliver all four requested outcomes:

1. Reset the existing miner registrations on our UR subnet, with an exact census and an explicit meaning of reset.
2. Install and initialize the production contract set with the approved custody and governance identities.
3. Begin provider rewards at **10% of the native miner allocation**. This is not 10% of all subnet emission, not a validator take, and not the head/tail steering parameter.
4. Operate both an owned **root validator on netuid 0** and an owned **validator on the UR subnet**.

The target UR mainnet netuid, owned mainnet node, keys, spend ceilings, reset mechanism, and treatment of the other 90% remain inputs to the future plan. None has a default mainnet address or financial allowance. Existing testnet spend approvals do not authorize mainnet spend.

Two constraints determine the implementation. There is no demonstrated subnet-owner call that arbitrarily clears every miner registration while retaining an arbitrary list of validators. Also, the current UR contracts and validator policy do not provide a standalone switch that changes the native miner allocation to 10%. The planner must expose these as capability decisions, not claim that lowering UID capacity or setting `theta: 0.1` fulfills them. The requested 10% target allows the exact runtime's explicitly established quantization tolerance; a stronger enforceable hard cap is a separate assurance choice, not an additional user requirement.

The initial draft policy is therefore `reset.mode: unresolved` and `emissions.remainder: unresolved`. A complete preview may be produced with those fields unresolved, but it must be visibly non-executable. Resolve the economic mechanism before installing an immutable vault or removing existing registrations. No further clarification is needed to complete this design document.

## Source and runtime boundary

The current published Subtensor source inspected here is [commit `67dcf7f791dc495064c293f080a0702cb433e51e`][subtensor-commit], dated 2026-09-07, following the release-455 merge. The official repository now resolves to `RaoFoundation/subtensor`; official documentation is at `bittensor.com/docs`. These are source observations, **not an attestation that mainnet is running that Wasm**.

The future `inspect` command must authenticate one finalized native block and its corresponding canonical EVM block using a separately verified, operator-owned mainnet node. Require an independently approved genesis hash, EVM chain ID 964, native chain identity, complete runtime version, `:code` hash, metadata hash, node build identity, and the reviewed runtime source/artifact mapping. Verify signed extensions, call argument types, storage layouts, and relevant precompile behavior. A matching `specVersion` alone is insufficient; [the existing runtime authenticator](../crv4/runtime_identity.go) already binds more than that number.

The authorized endpoint `http://192.168.1.162:9944` currently serves the verified **testnet** identity and must therefore be rejected for mainnet now. This is not a permanent blacklist of that numeric address: a later owned mainnet deployment at the same address can be admitted only after separate authorization and a fresh mainnet genesis/runtime attestation. Chain identity is authoritative. Do not infer a mainnet address by changing a port, switch to a named public network, inherit a library's default provider, or use a public fallback. RPC URLs, resolved upstreams, TLS identities where applicable, and local proxy routes are part of the plan. A loopback proxy is acceptable only when its sole upstream is the approved owned mainnet node and its process configuration is authenticated. Preserve the configured zero artificial RPC pacing; bound concurrency and cancel failed work instead of adding delays or alternate providers. Protocol block windows and on-chain rate limits still apply.

At admission, record native and EVM clocks separately. Verify their mapping; do not assume equal height or treat an EVM receipt as native finality. Historical reads must remain at the receipt's authenticated block. A runtime change stops new signing until a newly qualified adapter and a newly reviewed plan are installed.

Current source changes matter to this design:

| Subject | Source-backed observation | Bootstrap consequence |
| --- | --- | --- |
| Subnet emission allocation | The inspected `get_shares` uses price EMA, a `1 - MinerBurned` adjustment, then an emission gate. A flow-based helper also exists but is not the selected `get_shares` path. [Source][subtensor-shares] | Do not assume an older Taoflow formula or a root-validator vote controls our subnet's allocation. Attest the actual runtime path. |
| Root weights | Root Reborn uses a validator's root weights for its own dividend basket. This differs from historical global subnet-allocation voting. [Official guide][root-reborn] | Implement root operation separately from UR miner scoring; do not send UR UID weights to netuid 0. |
| Miner collateral | Registration collateral can survive deregistration; later earnings can affect release and capture. [Official collateral guide, pinned source][collateral-guide] | A UID reset is not a balance, lock, or stake reset. Pool capture must distinguish emission, locked collateral, and principal. |
| Native versus signed limits | The local whitepaper records runtime-dependent weight-limit behavior and requires a signed policy cap. [Local specification](../WHITEPAPER.md#15-concrete-parameters) | Observe runtime getters and enforce the signed cap independently. Do not assume a successful setter changed native enforcement. |

When documentation and the exact runtime source disagree, record the discrepancy and resolve it against the authenticated runtime. For example, the inspected emission-enable implementation affects pool-side injection while retaining participant-side emission; it cannot serve as an owner-controlled miner payout pause. [Storage contract][subtensor-storage]

## Authority and capability matrix

The word “root” identifies three different things here: the Substrate `Root` origin, a netuid-0 validator, and a UR settlement Merkle root. None grants either of the other authorities.

| Action | Required authority | Real limit and admission check |
| --- | --- | --- |
| Inspect finalized chain state | Read access to the owned node | No signer; authenticate chain and block before interpreting storage. |
| Change supported subnet parameters or request trimming | Subnet-owner coldkey, or a genuinely authorized chain `Root` origin | Owner calls have individual permissions, rate limits and administrative windows. A call name beginning with `sudo_` does not itself mean the owner has chain Sudo. |
| Change maximum validator permits or the global subnet-owner cut | Chain `Root` in the inspected implementation | Root-validator registration and the UR owner wallet do not satisfy `ensure_root`. No automatic governance proposal or Sudo attempt. |
| Remove every selected miner | Depends on an actually supported mechanism | No general owner-authorized arbitrary bulk removal has been established. See the reset alternatives below. |
| Register a UR hotkey and acquire stake | Its coldkey or a proven permitted proxy/contract origin | Registration, burn, collateral, pool price, capacity and eligibility are independent checks. |
| Register the root-validator hotkey | Its coldkey through the root registration path | Does not confer administrative authority. The native call's burn-price limitation needs special handling below. |
| Submit UR consensus weights | The registered UR validator hotkey | Correct permit/eligibility, stake, activity, mechanism and CRv4 timing are required. |
| Manage a root dividend basket | The registered root-validator hotkey | Root-specific stake, enablement, diversity, concentration and timing constraints apply. |
| Deploy EVM contracts | Dedicated EVM deployment signer | Exact nonce, creation bytecode, constructor data, gas and value envelopes. |
| Govern the coordinator | Approved EVM 2-of-3 Safe | Safe authorization does not authorize native subnet-owner calls. |
| Pause permitted coordinator actions | Configured guardian under contract rules | Cannot claw back reserve principal, rewrite earned claims or pause valid vault claims. |
| Withdraw immutable reserve or upgrade the settlement vault | No such release-1.0 authority | Reject any proposed action requiring this capability. |

The inspected admin implementation permits owner-limited trimming and selected parameters; maximum validators and owner-cut setters require chain `Root`. The emission-enable setter also requires `Root` and is not a percentage setter. [Pinned admin implementation][subtensor-admin]

Every planned transaction records its actual origin: native account or proxy real account, EVM sender, Safe address and threshold, and the exact role it exercises. Prove ownership and proxy filters from chain state. Do not manufacture an authority assumption from possession of a similarly named key file.

## Exact UID census and reset

### What is being reset

Scope is the approved UR mainnet netuid only. Netuid 0 and other subnets are excluded. A UID is a mutable slot, not a permanent miner identity, and a neuron can perform more than one role. “All miners” must become a signed list of **hotkey identities and registration generations**, not a range such as `1..255` or “all UIDs without a validator permit.”

At finalized block `B`, write `census.json` containing every UID and both directions of its UID/hotkey mapping; coldkey ownership; registration block; owner identity; role classification; permits and activity; native and mechanism-specific emission/weights; immune status and expiry; collateral and other locks; stake positions relevant to custody; commitments and associated EVM identity. Include the block hash and runtime identity for every decoded field. Reconcile the complete cardinality against `SubnetworkN`; missing entries or ambiguous ownership block planning.

Construct disjoint `remove`, `preserve`, and `unresolved` sets. Preserve explicit owner and validator hotkeys, including a validator currently lacking a permit, and any reserve, pool or escrow identity whose existing custody or earned claims require continuity. Membership in both a requested removal scope and a protected custody/validator role is an explicit conflict requiring a reviewed resolution; it is not silently omitted from “all.” Third-party validator identities receive the same explicit classification. Snapshot netuid-0 membership independently to prove it was untouched.

The literal default completion criterion is: every hotkey registration generation in `remove` is absent from the UR membership mappings, every identity in `preserve` retains its required ownership and role, and every survivor has a verified old-to-new UID mapping. If a removed miner later re-registers, record a new generation; never let reuse of the numeric UID satisfy the old identity's postcondition. Registration-open policy and the cutover window must specify whether re-entry is allowed.

Deletion of a registration does not delete historical events, refund registration cost, erase coldkey assets, or extinguish collateral and claims. Historical UR bindings continue to use their original block-specific mapping. Invalidate or renew only future bindings that reference displaced UID generations; preserve proof and claim history.

### Supported paths and their limits

| Mode | What it accomplishes | Condition for selection |
| --- | --- | --- |
| `owner-trim` | Lowers capacity, removes runtime-selected low emitters and compresses surviving UIDs. | A pinned simulation proves its exact removed set equals the approved set and preserves every required identity. Otherwise this is only a partial trim and does not satisfy the request. |
| `bounded-replacement` | New registrations replace runtime-selected existing neurons as capacity fills. | A finite, budgeted sequence proves every intended replacement and no protected loss, including competing registrations and changed pruning inputs. If the runtime cannot enforce the approved selection at execution, do not automate the destructive sequence. |
| `new-subnet` | Starts a separate metagraph and contract deployment on a new netuid. | Explicitly selected alternative with its own subnet-registration allowance and migration plan. It leaves the old subnet and its registrations in existence; it is not a reset of that subnet. |
| `chain-root-migration` | Can implement the literal removal policy if the chain's authorized governance adopts a suitable migration. | Separately reviewed runtime/call and authentic governance execution. The bootstrap verifies its finalized result; it never pretends the UR owner can grant itself this authority. |
| `ur-generation-only` | Resets UR application admission/scoring/bindings prospectively. | Explicitly accepted narrower outcome. It makes no claim to remove native UIDs. |

The inspected trim implementation enforces minimum and maximum capacity, protects owner-immune and temporarily immune entries, and requires the immune percentage to remain strictly below its runtime threshold. It removes according to emission rank and migrates the survivors' slot-indexed state. The ordinary `set_max_allowed_uids` path cannot set capacity below the occupied count. [Trim implementation][subtensor-uids], [capacity documentation][max-uids]

Ordinary registration pruning is a different algorithm. In the inspected source it excludes owner-protected identities but can fall back to temporally immune candidates in some capacity conditions. Do not use the trim immunity model to justify a replacement sequence. `clear_neuron` is an internal implementation routine, not evidence of an owner-callable reset endpoint. [Registration implementation][subtensor-registration]

The reset planner must derive the live minimum, timing restrictions, immunity threshold and pruning order, including ties, rather than use the documentation's nominal 64-slot minimum or 30-day trim interval as constants. An external actor can change the candidate set between preview and execution. A fresh preflight reduces that race but does not eliminate it; destructive automatic apply requires an execution-time guard or a demonstrated invariant under all allowed intervening changes. Otherwise export the unsupported action and report `RESET_CAPABILITY_BLOCKED`.

After a finalized reset, repeat the entire census and compare identities, not just counts. Reconcile commitments, balances and locks separately. Re-establish approved capacity and permitted registration settings before new pool/head registrations; no “temporary” parameter change may remain unreported. Existing settlement/claim service must stay available throughout any migration.

## Ten percent of native miner allocation

### Denominator and accounting

Use alpha's atomic units for reward accounting, distinct from TAO rao and EVM wei. The reference denominator is the UR subnet's **native miner tranche before the chosen 90% withholding mechanism**, over explicitly identified native emission intervals after activation. It excludes the owner cut, validator/root dividends, TAO pool injection, deposits, reserve principal, and collateral principal. A miner's newly earned reward captured into collateral still counts as that miner's economic reward; it is not a way to hide payments from the 10% calculation.

Let `M_i` be the authenticated miner allocation for native interval `i`, using the exact runtime's accumulation, mechanism split, drain and rounding rules. Let `M_total(k) = sum(M_i, i <= k)`. Define the integer reference for the cumulative provider target:

```text
P_reference(k) = floor(M_total(k) / 10)
P_interval(k) = P_reference(k) - P_reference(k - 1)
remainder_reference(k) = M_total(k) - P_reference(k)
```

This conserves reference rounding across intervals; independent floating-point `0.1 * amount` calculations are prohibited. Use checked integer/rational arithmetic. Record `Q(k)`, the absolute alpha-unit tolerance derived from the selected runtime's actual u16, fixed-point and per-recipient rounding over the observation window. The requested target is met when actual provider entitlement differs from `P_reference(k)` by no more than that explicitly reviewed tolerance. Do not invent a large percentage allowance for consensus disagreement or require zero dust when the chain cannot represent the fraction exactly. Track native truncation dust separately from the policy remainder. Allocation at a later UR settlement epoch must reference its underlying native intervals exactly once. Align activation to a proven drain boundary with no unexplained pre-activation `PendingServerEmission`; never relabel old rewards as a new 10% budget.

Under the nominal 18% owner / 41% miner / 41% validator split, this request corresponds to approximately **4.1% of participant-side subnet emission**. The actual owner-cut fraction and fixed-point rounding must be read and reproduced, not hardcoded as exactly 41%. The inspected implementation accumulates a miner half after the owner cut, and zero incentive can redirect the miner allocation to validators. [Participant distribution implementation][subtensor-coinbase]

The existing UR `theta` divides provider distribution between direct head miners and tail pools. Once the 10% mechanism is selected, construct reference allocations `H = floor(theta * P_reference)` and `T = P_reference - H`, then reconcile their actual native outcomes against the approved quantization tolerance. A separately selected hard-cap mode additionally requires actual cumulative entitlement not to exceed its signed ceiling. Changing theta alone leaves the native miner pot intact; row normalization also makes multiplying every validator weight by 0.1 ineffective. [UR steering specification](../WHITEPAPER.md), [existing steering tests](../validator/steer_test.go)

Count entitlement once: native rewards to provider-owned head coldkeys, and provider entitlement funded through tail pools, are the two payment channels. A tail capture followed by a claim is one reward, not two. Target measurement, and any separately selected cap, covers earned entitlement and locked miner reward, not just liquid claims completed so far. Existing valid claims retain their original terms.

### Explicit policies for the other 90%

| Policy | Feasibility and consequences | Design disposition |
| --- | --- | --- |
| `owner-recycle` | If final Yuma incentive is directed to the runtime-recognized owner-hotkey set, that portion is recycled. It is not placed in our reserve. | Candidate path closest to existing custody architecture, but requires a changed economic policy and a proven final-incentive split. |
| `owner-burn` | The same owner-directed withholding path can burn instead. Burn/recycle supply effects differ. | Same proof requirements; never choose implicitly from an unset runtime value. |
| `reserve-custody` | Routes the remainder into an explicitly defined reserve position, with separately enforced accounting and no provider claim on it. | Requires a demonstrated origin/custody route and new economic design. The current vault has no “send 90% to reserve” operation. |
| `deferred-provider-liability` | Pays 10% now while preserving 90% as future provider claims. | This is payment deferral, not a 10% reward allocation; it does not satisfy the clarified request unless explicitly redefined. |
| `native-runtime-cap` | A runtime-enforced miner sub-allocation could provide the strongest whole-subnet boundary. | No suitable owner-callable primitive was established in the inspected source. Requires an actually deployed, qualified capability. |

The existing implementation withholds owner-directed incentive in both burn and recycle modes and records the withheld ratio. Choosing recycle does not avoid that accounting. [Owner-directed distribution][subtensor-coinbase] The ratio reduces the subnet's demand share before the emission gate; a 90% withholding policy is economically consequential and does not imply an exact 90% reduction in final TAO allocation after renormalization and gating. [Allocation formula][subtensor-shares]

Release 1.0 explicitly rejected owner-directed burning as its head/tail steering strategy. Selecting either withholding path here therefore requires an explicit replacement of that economic policy, not a hidden bootstrap flag. Preserve the independent-validator objective and signed weight caps: do not raise a cap, create arbitrary owner recipients, or displace validators merely to force a 90% weight destination. [Whitepaper, head/tail decision](../WHITEPAPER.md#138-headtail-split-θ-in-one-mechanism-chosen-not-two-mechanisms-not-owner-burn)

A weight proposal is not an enforceable payout fraction. Independent validator weights, Yuma clipping, bonds, activity, permits, normalization and u16 rounding affect final incentive. For either owner-withholding path, qualify the complete runtime outcome against the admitted validator set and review adjacent/adversarial weight states. The draft assurance mode is `observed-native-target`: demonstrate the actual 10% allocation within `Q(k)`, disclose sensitivity to other validators, and monitor subsequent deviation. It does not promise that other validators can never change the outcome. If a stronger `enforced-cap` mode is selected, prove that ceiling under all admitted conditions or report `EMISSION_CAP_UNENFORCEABLE`; an after-the-fact monitor is not enforcement. Halting our validator does not revoke other validators' weights or stop already queued native emission. [Consensus implementation][subtensor-epoch]

Claim “started at 10%” only after the observed native outcomes meet the target and its runtime-derived tolerance. Report deviations and dust explicitly; a low payout below a hard ceiling alone does not establish that the target was reached. If only a proposed weight target can be shown and the actual outcome cannot yet be established, report `ACTIVATED_AWAITING_EMISSION_OBSERVATION`. A material miss is not relabeled as quantization. Future changes to the target or assurance level require a new reviewed policy.

### Immutable vault implications

[STSettlementVault](../evm/src/STSettlementVault.sol) captures the eligible pool emission through the configured staking precompile and keeps immutable claim accounting. It has no upgrade or treasury sweep. Its conservation checks distinguish total captured, total paid, escrow accounting, pending funding and outstanding liabilities. Publishing payout roots at 10% while leaving 90% in this vault does not make the remainder an owner reserve or erase its accounting obligations.

A reserve design must specify a different enforceable custody path before deployment: who owns each head and pool registration, which precompile call transfers each tranche, which principal and collateral are excluded, who can authorize the transfer, and how cumulative provider liabilities are bounded. Moving all head miners behind a new contract would change release 1.0's provider-owned direct-payment model. Such a design requires its own contract/policy qualification and migration plan; a coordinator upgrade cannot retrofit withdrawal authority into an old vault.

Recommended planning order: assess owner-recycle and owner-burn against the exact runtime and signed policy first, compare their economic effects, then assess a new metered custody design only if the user selects retention of the remainder. Leave `remainder: unresolved` until one complete, reviewable policy is chosen. Do not deploy an immutable contract set while this decision can still invalidate its custody layout.

## Contract deployment, custody and initialization

Reuse the release-1.0 contracts and reviewed ABI/artifact generation, with mainnet-specific inputs. The existing [Deploy script](../evm/script/Deploy.s.sol) is the ordering reference, not a command the bootstrap blindly shells out to. The Go planner must build exact transaction payloads and independently read back their results.

| Identity | Custody/authority |
| --- | --- |
| Subnet owner coldkey | Native subnet administration; offline or explicitly qualified native multisig/proxy. |
| EVM deployer | Limited bootstrap gas/value; no ongoing governance custody. |
| Coordinator owner | Actual 2-of-3 Safe with three distinct approved owners. |
| Guardian | Separate limited operational authority. |
| Commitment oracle | Separate reviewed signer/service with original and any scheduled route authenticated. |
| Root validator coldkey/hotkey | Root stake custody and root service signing; separate from UR scoring by default. |
| UR validator hotkey and stake coldkey | UR scoring; may be the reviewed reserve target when explicitly selected. |
| Vault mapped coldkey | Immutable tail-pool and escrow custody. No human holds its private key. |
| Reserve mapped coldkey | Permanent reserve stake under the immutable sink. |

Role equivalence must be deliberate. In particular, naming a UR validator “owner validator” does not prove that it is the native `SubnetOwnerHotkey`. The planner checks the actual mapping and does not assume UID 0. Require the existing deployer's distinct owner/guardian/oracle constraints. Inspect Safe singleton bytecode, owners, threshold, enabled modules, guards, fallback handler and pending transactions; an address merely implementing `getOwners` and `getThreshold` is insufficient.

Freeze compiler and dependency versions, creation and deployed bytecode, link/immutable locations, constructor encodings, source identities and storage layout. For a dedicated deployer starting at nonce `n`, the existing core sequence is:

| Nonce | Action | Required postcondition before its dependants |
| --- | --- | --- |
| `n` | CREATE `STReserveSink` | Exact predicted address, bytecode, netuid, reserve hotkey, bootstrap, mapped coldkey. |
| `n+1` | CREATE `STSettlementVault` | Exact custody identities, claim horizon, minimum transfer and bootstrap. |
| `n+2` | CREATE `STCoordinator` implementation | Exact implementation bytecode; implementation initializer disabled. |
| `n+3` | `vault.registerEscrow(maxBurnRao)` | Escrow registration under the vault's mapped coldkey, correct UID/ownership and bounded debit/refund. |
| `n+4` | CREATE ERC1967 proxy with initialization calldata | Initialization occurs in the constructor; approved Safe, guardian, oracle, custody links and initial policy are set atomically. |
| `n+5` | `reserve.setRecorderOnce(proxy)` | Exact one-shot recorder. |
| `n+6` | `vault.setCoordinatorOnce(proxy)` | Exact one-shot coordinator. |

The escrow registration deliberately consumes a nonce before proxy creation. Predict all addresses before construction because the mapped coldkeys are immutable. Native rao-to-EVM-value conversion uses `1 rao = 10^9 wei` here; bounds and conversions must reject overflow. Authenticate `blake2_256("evm:" || H160)` against the runtime mapping before custody is funded.

Deploy and anchor [STValidatorEvidence](../evm/src/STValidatorEvidence.sol) as a separately planned additive contract with its genesis/deployment domain and coordinator/vault identities. The current release uses the coordinator's one-shot `fixValidatorEvidence`; an existing foreign anchor is a hard conflict. Use the fresh mainnet nonce graph, not the sim-testnet graph's extra upgrade, fleet-helper or adversarial contracts. Mainnet artifacts must not include those test fixtures by default. [Evidence deployment reference](../sim-testnet/evidence_deployment.go), [readback reference](../sim-testnet/evidence_deployment_runtime.go)

Only then register the approved operator pool hotkeys under vault custody, establish reserve-target eligibility, activate evidence identities and future bindings, and fund reviewed stake/deposit positions. Provider-owned head miners register through their own authorized identities; bootstrap cannot sign for unrelated miners. Every registration is present in the spend/count plan. Reconcile pool/escrow collateral and minimum-transfer semantics before the first production capture; immutable custody must not become stranded by an unqualified runtime change.

Use the actual [mainnet policy validation](../protocol/policy.go): a UR settlement epoch is **50,400 native blocks**, with the reviewed production root-commit/finalization/close windows and claim retention. The deploy script's mainnet reference windows are 1,200 / 14,400 / 120 blocks and 8 claim epochs plus 1 grace epoch. Encode all fields explicitly in the signed mainnet policy; do not inherit accelerated 300- or 360-block testnet settings. Mainnet economic caps, deposit tiers, theta, minimum operator/validator counts and binding horizons need independent review.

Preserve the current guarantees: the coordinator owns neither custody position, the sink has no outbound path, and valid earned vault claims survive coordinator pause or upgrade. Pausing new application activity is not a native emission kill switch. Initial contracts establish their epoch clock at deployment, so the plan must include sufficient time to finish setup and a future activation boundary; it cannot assume a dormant deployment has no running clock.

## Running both validators

### Root validator on netuid 0

Implement a separate root service/config schema. The current [UR validator config](../validator/config.go) rejects netuid 0 and is not a root-validator implementation.

For an existing root seat, verify hotkey/coldkey ownership, current membership and registration generation, stake, immunity, delegate take, children/parents, basket configuration and accrued rights before adoption. For a new seat, the inspected runtime uses burn-priced root registration without a prior-stake admission condition; a full root network prunes a lowest-staked eligible seat. Registration alone does not provide enough stake to retain a seat or submit basket weights. [Root registration implementation][subtensor-root]

One specific budget gap must not be hidden: native `root_register(hotkey)` has no maximum-burn argument, while `register_limit` rejects netuid 0. A fresh quote is not an atomic price ceiling. The inspected Neuron precompile also exposes `rootRegister(bytes32)` without a limit. [Native call definitions][subtensor-dispatches], [registration limits][subtensor-registration], [Neuron interface][neuron-interface]

The future adapter therefore needs one of these explicit admission paths:

- Adopt an already registered, approved root identity and prove its finalized receipt and current ownership.
- Qualify a bounded root registration mechanism. A possible EVM path is an approved root custody account that invokes `rootRegister` and atomically rejects a mapped-balance debit above the signed ceiling. This changes the coldkey custody model and requires proof of EVM/native rollback, fee separation, hotkey ownership, staking/withdrawal authority and child-delegation controls; no such helper is supplied by this design or the existing UR contracts.
- Record a separately authorized native root registration as an external prerequisite with its actual receipt and spend. Do not label an uncapped native call “max-burn protected” or silently replace its intended coldkey with a contract mirror.

Without a proven bounded path or retained seat, full automatic bootstrap is `ROOT_REGISTRATION_CAPABILITY_BLOCKED`. This is a concrete implementation requirement, not permission to omit the requested root validator.

Root registration can automatically delegate child weight to every existing subnet owner unless the identity opts out first. The proposed root policy explicitly disables automatic parent delegation before registration, then permits only a separately selected UR delegation if required. For a fresh hotkey, first establish its approved coldkey association through the supported `try_associate_hotkey` path: the opt-out call already requires that ownership. Verify the resulting child/parent maps, including pending changes. Existing roots with custom weights require an explicit preservation/reset decision; omitting a new write must not be mistaken for clearing old weights. [Root delegation dispatch notes][subtensor-dispatches]

Default the draft basket policy to **accumulate in place with no custom weight vector**. This is a real operating strategy, not simulated validation or a claim to control subnet issuance. Setting custom root weights turns on basket allocation behavior and must be separately plan-bound. The inspected call checks root membership, minimum stake, enablement, timing, distinct existing destinations, diversity and concentration. Read their live values; do not hardcode a 64-seat network, an 8-destination floor or a 1/16 cap. [Root weight implementation][subtensor-weights], [basket behavior][root-reborn]

Stake the root seat from its own explicit TAO allowance, retain fees/ED, and observe the actual retention margin. Stake or basket top-ups, re-registration, claims, take changes and weight changes are bounded planned actions, not an unlimited watchdog loop. The service monitors finality, seat ownership, stake rank, delegation, basket state and runtime identity. Claiming root yield and unstaking principal are distinct operations with runtime-dependent windows; neither is enabled automatically by “run a root validator.”

### UR subnet validator

Run the production [validator entry point](../cli/validator/main.go), using complete deployment, policy, runtime, genesis, operator API, evidence and signing inputs. It must acquire current UR eligibility and validate finalized usage/evidence before emitting native CRv4 weights. Preserve the signed weight cap, head/tail rules, deposit/quality calculation and full prefix/history admission. Reject testnet provisional-input deferrals on mainnet.

Observe effective alpha/root-stake contribution, child attribution, `TaoWeight`, stake threshold, permits, activity, CRv4 version, reveal schedule and mechanism state. Do not transplant the testnet stake target or an old 0.18/0.018 TAO multiplier. The inspected production epoch path gives the owner UID special eligibility treatment; another owned hotkey still needs its own proper eligibility. A configured process being alive does not prove it has a permit or that its weight row was revealed and applied. [Permit calculation][subtensor-epoch]

Both requested validators do **not** count as two UR validators: a seat on netuid 0 alone does not validate the UR subnet. The current UR safety policy requires at least two live validators and two healthy operators. Include a separately admitted second UR validator before production readiness. Do not generate synthetic peers to satisfy the count.

Capacity is computed from the union of actual UR hotkeys: head miners, one pool per operator, distinct UR validator identities, escrow and owner/other protected identities. Root-only membership consumes no UR slot. A validator-permit limit is not a reserved partition of UID space. Keep the release's one-mechanism requirement and approximately 200-head target only if the live capacity and all additional identities fit. Do not assume “200 miners + 56 validators” leaves space for pools and escrow.

Manage both services with independent state directories, signer permissions, logs, executable/config hashes, and one writer per hotkey. Coldkeys and Safe owners stay out of online validator processes. Supervision has bounded restart policy; ownership transfers require the old process and every child to be joined. Read-only health includes finalized lag, evidence/index gaps, permit activity, revealed weight rows, pool/escrow capture, signed cap violations and root seat/basket drift.

## Go CLI and action model

`mainnet/main.go` should contain argument parsing, signal handling and exit-status mapping. Keep the plan builder pure after its authenticated snapshot inputs are supplied. Separate chain adapters, signer interfaces, state storage and supervisors so preview cannot reach a transaction submission path.

Suggested commands:

| Command | Behavior |
| --- | --- |
| `inspect` | Read-only owned-node identity, authority, census, capability, balances, custody and validator observation; emit a hashed snapshot. |
| `plan` | Build canonical plan/actions and a readable review from config, pinned snapshot, artifacts and testnet acceptance. No signing or submission. |
| `apply --accept-plan HASH` | Execute only the exactly reviewed plan with matching signed authorization, prerequisites and ceilings. |
| `status` / `verify` | Read-only journal reconciliation and current/finalized postcondition verification. |
| `resume --accept-plan HASH` | Recover in-flight actions, verify retained receipts and continue the same approved graph without duplicate spend. |
| `services start` / `services stop` | Run or join the plan's admitted services; starting write-capable validators is an explicit authorized phase. |
| `report` | Produce a complete acceptance or incomplete/blocked report with evidence references and realized spend. |

No implicit apply, automatic subnet creation, private-key CLI flags, “force” bypass, mutable `latest` artifact, or inherited network defaults. Every mutating command takes an explicit run directory and accepted plan hash. `inspect` may be run before testnet acceptance using the future authorized mainnet read endpoint; irreversible apply cannot.

The canonical plan binds schema and action-format versions; exact config/policy bytes; resolved configuration roots and runtime routes; source/dependency/artifact/binary identities; owned-node and runtime identities; native/EVM snapshot hashes; all public roles; census and reset classifications; actual transaction payloads/origins; expected CREATE addresses and nonces; phase dependencies; validity windows; spend/count caps; and the chosen emission-denominator/remainder policy. Hash canonical bytes with domain separation. The signed authorization names that hash, network, expiry, allowed phases and ceilings. Reject duplicate fields, unknown schema versions, overflow, unexpanded substitutions and ambiguous addresses.

Future operator examples, shown only as interface design:

```sh
sn-mainnet inspect --config /secure/ur-mainnet/bootstrap.yml --out /secure/ur-mainnet/inspection
sn-mainnet plan --config /secure/ur-mainnet/bootstrap.yml --snapshot /secure/ur-mainnet/inspection/snapshot.json --phase full --out /secure/ur-mainnet/review
sn-mainnet apply --plan /secure/ur-mainnet/review/plan.json --accept-plan "$REVIEWED_MAINNET_PLAN_HASH" --authorization /secure/ur-mainnet/authorization.json --run-dir /secure/ur-mainnet/run
sn-mainnet status --run-dir /secure/ur-mainnet/run
sn-mainnet resume --run-dir /secure/ur-mainnet/run --accept-plan "$REVIEWED_MAINNET_PLAN_HASH" --authorization /secure/ur-mainnet/authorization.json
sn-mainnet verify --run-dir /secure/ur-mainnet/run --out /secure/ur-mainnet/verification
```

This incomplete config sketch intentionally contains `null` for unapproved identities and monetary values. A real executable plan must reject them. Values represent required fields, not suggested budgets or fake addresses; secret material is supplied through local signer references rather than embedded here.

```yaml
schema: urnetwork-mainnet-bootstrap-v1
network: mainnet
deployment_id: null
netuid: null
owned_node:
  substrate_url: null
  evm_url: null
  ownership_attestation: null
  expected_genesis_hash: null
  expected_evm_chain_id: 964
  runtime_artifact_manifest: null
  rpc_pacing: none
  fallback_urls: []
release:
  source_lock: null
  contract_manifest: null
  binary_manifest: null
  policy_file: null
  accepted_testnet_final2: null
roles:
  subnet_owner: null
  evm_deployer: null
  coordinator_safe: null
  guardian: null
  commitment_oracle: null
  root_validator: null
  ur_validator: null
  independent_ur_validators: []
  reserve_hotkey: null
  escrow_hotkey: null
reset:
  mode: unresolved
  census_file: null
  remove_generations_file: null
  preserve_identities_file: null
  registration_during_cutover: null
emissions:
  denominator: native_miner_allocation_before_withholding
  provider_fraction: {numerator: 1, denominator: 10}
  assurance: observed-native-target
  quantization_tolerance_manifest: null
  remainder: unresolved
  mechanism_manifest: null
  activation_boundary: null
root_validator:
  registration_mode: null
  auto_parent_delegation: false
  basket_strategy: accumulate_in_place
  custom_weights: []
  stake_rao: null
  delegate_take: null
ur_validator:
  release_config: null
  stake_plan: null
  independent_validator_evidence: null
limits:
  total_tao_debit_rao: null
  total_alpha_commitment_units: null
  evm_fee_wei: null
  native_fee_rao: null
  per_registration_burn_rao: null
  root_registration_debit_rao: null
  maximum_registrations: null
  maximum_subnet_creations: 0
  maximum_transactions: null
  stake_price_limits: null
  expiry_finalized_block: null
operations:
  run_dir: /secure/ur-mainnet/run
  signer_manifest: null
  service_manifest: null
  worker_limit: null
```

The complete schema also requires action-level value/gas/fee bounds, collateral exposure, swap price/minimum-output limits, claim/deposit policy caps and fee reserves. Totals aggregate economic debits once across EVM and native representations. Refunds are recorded separately; they do not replenish lifetime authorization unless the plan explicitly defines that rule. Reverted transactions consume fee budget. New attempts, repairs and replacements retain the same lifetime ledger.

## Phases, finality and recovery

| Phase | Admission and work | Completion evidence |
| --- | --- | --- |
| 0. Testnet acceptance | Authenticate actual `FINAL-2.md`, its source lock, local gate transcripts, chain/run evidence and finalized reconciliation. | All required testnet outcomes passed; no substituted read-only preview or incomplete run. |
| 1. Mainnet inspect and review | Verify node/runtime, complete census, authorities, capabilities, keys, artifacts, reset method, 90% policy and budgets. | Canonical feasible plan and exact operator authorization; no unresolved full-scope action. |
| 2. Cutover/reset | Execute the selected supported reset and configuration changes within their native windows. | Complete before/after census, preserved identities and accounted removals/locks. |
| 3. Contracts and registration | Deploy exact custody graph, anchor evidence, register approved pool/escrow/head/validator identities. | Canonical finalized receipts, code/getter proofs and registration ownership. |
| 4. Stake and service readiness | Apply bounded stake/deposit plans; admit both services and the second UR validator/operator safety set. | Current root membership, UR eligibility, authenticated runtime configs and service ownership. |
| 5. Emission activation | At the approved native boundary, activate the qualified 10% mechanism and corresponding signed UR policy. | Native incentive outcome, remainder destination, weights, stake/collateral deltas and policy epoch agree. |
| 6. Acceptance and operations | Observe the specified production interval, settle/claim genuine accrued emission and reconcile all funds. | Self-contained final report; all four requested outcomes satisfied with no open cap/custody exceptions. |

Phases form a dependency graph, not a best-effort list. The final activation boundary may need a new snapshot and plan revision after lengthy setup; revisions authenticate the prior plan and completed receipts, preserve original immutable identities and lifetime caps, and explicitly authorize changed future actions. They do not rewrite the prior plan, retroactively approve execution, or reset spend.

Persist a write-ahead, hash-linked action journal with states such as `planned`, `intent-recorded`, `signed`, `submitted`, `included`, `finalized`, `postcondition-verified`, `failed` and `canceled`. Append and fsync the exact intent and signed payload before broadcast, with mode-0600 protection for recoverable signed bytes. Public evidence contains no secrets. Journal records include parent plan/action hashes, full origin/domain, payload hash, nonce, value/fees, transaction hash, actual block/receipt/event positions and the authenticated postcondition snapshot.

Use atomic manifest/config writes and one exclusive run owner. Native account and EVM/Safe nonce ownership must be explicit, including whether two representations share an underlying account. Serialize one-shot setup dependencies. Pipeline independent operations only after adapter-specific evidence shows nonce, finality and spend recovery remain correct. A timeout, canceled watch or lost RPC response is not proof that a transaction failed; search for the exact signed transaction before deciding to rebroadcast or replace it.

Native success requires finalized inclusion and successful dispatch at the correct extrinsic index. EVM success requires a canonical receipt and the corresponding native finality mapping, plus exact event/getter/code readback. Decode events with that block's authenticated metadata. Where batched calls can partially succeed, record every child result; prefer an actually atomic batch when the desired action requires all-or-nothing behavior. Never infer child success from the outer batch alone.

Recovery rules:

- For CREATE, find the original nonce transaction and compare exact address, code and immutable parameters; do not redeploy because a local marker is missing.
- For registration, compare hotkey ownership and generation, not only UID presence. A foreign occupant is a conflict, not an idempotent success.
- For one-shot links and initialization, an exact existing value is reusable only with authentic receipt/precondition history. A conflicting initialized value stops recovery.
- For stake, deposits and capture, reconcile actual deltas and retained-source balances before any retry. Never repeat an economic action because its terminal response was lost.
- For local rendered configuration, bind format, resolved route, paths and authority/capacity inputs in the action intent. An approved new local render can converge stopped services; it cannot pretend an old receipt already proves new bytes.
- For superseded actions, authenticate the historical postcondition at its source and the specific finalized successor that authorizes current state. Do not demand obsolete live equality after an approved successor, and do not globally waive current checks.
- On cancellation, stop new signing, join submitted transaction owners and service children, persist the actual terminal state, and report `CANCELED`, not `PASS`. An incomplete deployment or native inclusion remains recoverable work.

There is no automatic rollback of a finalized UID removal, registration burn or immutable deployment. Recovery uses a newly reviewed bounded forward action where supported. Old custody contracts and claim artifacts remain served until their obligations have actually ended. An emergency service stop does not imply that native emission stopped or that the vault may stop honoring claims.

## Integration with this repository

Reuse importable production packages: [crv4](../crv4) for authenticated runtime/native reads and transaction evidence; [stabi](../stabi) for the release ABI surface; [protocol](../protocol) for policy and domain encoding; [validator](../validator) for UR validation and evidence; and existing cryptographic/address/Merkle primitives where their units and domains match.

Do not import `sim-testnet` as a production dependency: it is a `package main` campaign with fixture, finance-repair, historical migration and adversarial machinery. Extract only a required, qualified generic facility into a neutral internal package when implementation begins. Keep mainnet capability adapters explicit, and keep the new root-validator implementation separate from UR scoring. The old [stctl configuration](../stctl/config.go) identifies itself as legacy pre-1.0 and rejects the current deployment domain; it is not the release-1.0 mainnet control plane.

Suggested future package boundary:

```text
mainnet/main.go                  argument parsing and command dispatch
internal/mainnetbootstrap/       config, snapshots, capabilities, plans, executor, reports
internal/rootvalidator/          root membership/basket observer and approved action loop
internal/chainactions/           only extracted, proven transaction/journal primitives
validator/                      existing production UR validator
```

Use interfaces for `FinalizedReader`, `NativeSigner`, `EvmSigner`, `SafeSigner`, `Submitter`, `Journal` and `ServiceSupervisor`. Follow the [UR Go style guide](../../connect/CODESTYLE.md): owned Go identifiers use `Evm`, `Rpc`, `Uid` and `Id` casing, `self` receivers and the prescribed field naming. Preserve externally required, generated and wire-format names. Packages may import a parent or peer, never their own child; the proposed internal packages are peers of the command package, not a bypass for that rule. A signer returns an identity-bound signed payload; it does not decide policy or fall back to another account. Mainnet configuration decoding must finish before any durable worker or RPC connection starts. Keep testnet provisional admission flags, deterministic fixture keys, fabricated identities, accelerated epochs, simulator routes and faucet/funding assumptions outside this interface.

## Acceptance evidence and implementation qualification

Promotion is gated on the actual full testnet campaign: 33 roles, 1,000 providers, 202 candidates, 200 head positions and two UR validators; five accelerated epochs; the scheduled 360/60/180/6 testnet production policy; and three fully observed production epochs with native/economic/claim reconciliation, as required by that campaign. Authenticate its final report and referenced evidence on the final qualified source. Do not substitute the producer or aggregate local gate alone, a canceled setup, an approved plan, or a partially observed epoch. The testnet report must explicitly close remaining repair/renewal work and finality. The mainnet build has its own release identity and the 50,400-block production policy.

The future mainnet acceptance bundle contains:

| Outcome | Evidence required |
| --- | --- |
| Reset | Signed target/preserve census, exact finalized removal mechanism, before/after hotkey generations and UID mapping, unchanged required custody/validator ownership, residual stake/lock report. |
| Contracts | Source/toolchain/artifact hashes, predicted/actual addresses and nonces, creation receipts, runtime bytecode and immutable getters, Safe authority, one-shot links and evidence anchor, preserved custody invariants. |
| 10% miner rewards | Explicit denominator and activation boundary; exact native interval accounting; finalized incentive outcomes including collateral; direct-head and tail entitlement reconciliation; identified 90% disposition; runtime-derived quantization tolerance and actual target result; proof of a stronger hard cap only if that assurance was selected. |
| Root validator | Real netuid-0 membership and owner mapping, bounded admission receipt, stake/retention observation, child policy, actual basket strategy, live owned service and runtime identity. |
| UR validator | Real UR eligibility and live applied/revealed CRv4 rows, authenticated evidence/usage, service signer, second UR validator and operator safety minima. |
| Financial/finality closure | Actual spend including failures, remaining allowance, all transaction owners joined, native/EVM finality mapping, open liabilities and future operations clearly reported. |

Measure activation across at least three complete native emission intervals, and observe a full mainnet UR settlement/claim cycle before declaring settlement acceptance. The 50,400-block cycle cannot be replaced by accelerated testnet timing. Bootstrap may report `DEPLOYED_AWAITING_SETTLEMENT` while that observation is pending; it must not call the whole requested program accepted early.

Implementation qualification is owned by **Terra max for tests, race runs, builds and formatting; Astra max diagnoses and authors fixes**, following the [Go style guide's bug-fix and testing policy](../../connect/CODESTYLE.md). Every actual fix needs a regression that deterministically reproduces the pre-fix failure and verifies corrected behavior at the observable failing layer. Use explicit barriers, hooks or state transitions for ordering; sleeps, negative timeouts, queue polling and scheduler luck are not the primary proof. Review surrounding code, sibling call sites and similar patterns before declaring the root cause fixed, and record any affected adjacent paths.

Keep regression data visibly synthetic: generated test-only identities, `.example` hosts and reserved documentation addresses; sanitize captures before turning them into fixtures and retain necessary raw evidence outside source. Tests are top-level `func TestXxx(t *testing.T)` declarations. Use separate top-level tests or plain table loops for ordinary cases; use `t.Run` only when isolating and asserting a deliberately failing subtest is itself the subject. This document does not execute or request execution of new tests. When implementation is authorized, cover meaningful boundaries and recovery:

1. Command-level read-only preview cannot acquire a signer or submit, including malformed/duplicate config, wrong genesis/chain, testnet identity, route substitution, stale snapshots and runtime upgrades. Also cover a separately attested mainnet identity subsequently hosted at a previously testnet address.
2. Real pinned metadata/call encoding proves owner versus Root origins and both root/UR call domains. Negative cases include unsupported trim, wrong signer, root `register_limit`, missing price protection and native/EVM rollback failure for any new registration wrapper.
3. Reset census and actual pruning/trim replay cover dual-role neurons, validator-without-permit preservation, custody identities, immunity boundary, tie ordering, concurrent registration, minimum capacity, UID renumbering and surviving collateral. A smaller UID count alone cannot pass.
4. Full emission-path tests demonstrate why scaling weights or theta does not impose a cap; then prove the selected 10% target and runtime-derived tolerance through quantization, consensus, independent-validator rows, no-incentive fallback, multiple mechanisms, denominator-boundary accrual, locked rewards and cumulative dust. Distinguish actual target observation from any selected hard-cap guarantee. Cover burn and recycle accounting separately and refuse an unsupported reserve transfer.
5. Deployment tests use real creation payloads and contract execution for nonce `n+3` escrow registration, atomic proxy initialization, mapped coldkeys, Safe checks, refund/fee bounds, one-shot conflicts and evidence anchoring. A changed custody contract requires its own conservation/claim and adversarial review.
6. Crash/restart tests inject failure before and after signing, submission, finality and fsync; prove no duplicate burn/deposit/stake and no lost finalized success. Cover nonce collision, reorg before finality, failed batch children, canceled owners and authenticated successor history.
7. Process tests prove both distinct validator roles, no silent endpoint fallback, single signer ownership, complete child joining, current UR permit/reveal checks, root custom/default strategy handling and no unbounded restaking/re-registration loop.

Run bounded normal tests on the frozen implementation, then appropriate race tests for shared state, journal ownership and supervisors. Retain exact source, binary, selector, package working directory and terminal evidence. Diagnose any actual failure on that capture before retry; preserve failed evidence and use the established confirmation protocol. Reuse unaffected qualification only with an explicit source/dependency mapping; changed custody/runtime economics require their relevant full tests and owned-node rehearsal. There are no mainnet tests against public RPC and no broadcast hidden in a test command.

## Open inputs before an executable mainnet plan

The concrete next work is to finish testnet acceptance, then obtain the owned mainnet node/runtime attestation and a full UR census. That establishes whether literal miner removal is possible with the available authority. In parallel, choose the remaining-90% policy and qualify the actual 10% native allocation with its explicit runtime granularity and assurance level. Select root custody/registration protection, supply actual identities and budgets, and qualify the implementation and any changed economic contracts. Each unresolved item remains visible in the preview and final report; none is converted into an assumed successful outcome.

[subtensor-commit]: https://github.com/RaoFoundation/subtensor/commit/67dcf7f791dc495064c293f080a0702cb433e51e
[subtensor-admin]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs
[subtensor-storage]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/lib.rs#L1661
[subtensor-uids]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171
[subtensor-registration]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/registration.rs
[subtensor-root]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/root.rs#L88
[subtensor-weights]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/weights.rs#L875
[subtensor-dispatches]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/macros/dispatches.rs
[subtensor-coinbase]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/run_coinbase.rs
[subtensor-shares]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/subnet_emissions.rs#L354
[subtensor-epoch]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/epoch/run_epoch.rs
[neuron-interface]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/precompiles/src/solidity/neuron.sol#L206
[root-reborn]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/guides/root-reborn.mdx
[collateral-guide]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/guides/mining/collateral.mdx
[max-uids]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/hyperparameters/max-allowed-uids.mdx
