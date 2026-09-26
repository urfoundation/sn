# Implementation status — complete whitepaper v1 (2026-07-02)

> **v0.4 (D25–D28) IMPLEMENTED 2026-07-03 — conviction staking, validator-computed weights, IP-breadth
> head, testnet-first.** The full cascade landed and builds GREEN across every repo (sn root + validator +
> snclaim + connect/provider + server). Specced in `WHITEPAPER_DISCUSS.md` D25–D28. Status:
> - **DONE (contract + bindings).** The contract stops weighting/validating deposits: the `DT`/`totalDT`
>   ledger is **dropped** (deposits are **conviction stake** recorded by the 4-arg `Deposited(e, noId,
>   from, amount)` event; validators compute the pool weight `implied_usage × quality` off that log — the
>   contract weighs nothing). **`EpochFinalized` lost `totalDT`** (now `(e)` only); **ABI 62 → 60
>   methods**; **stabi regenerated**; stctl `state`/format output de-DT'd. `deposit()` still full-sinks to
>   the locked reserve (the D23 buyback is kept). Forge **75/75**.
> - **DONE (connect wire).** `connect.VerifyProofHop` gained a per-hop **`EgressIpHash`** folded into the
>   **signed** FINAL message (`BuildVerifyFinalMessage`) so the server attests each hop's egress-IP-prefix;
>   golden vectors recomputed; connect + provider green.
> - **DONE (server).** The `/verify` server computes each confirmed hop's egress-IP-hash at a
>   **subnet-configurable** prefix (default **/29 v4, /48 v6**, `VerifySettings`) → the FINAL proof (poison
>   trails get a stable synthetic hash, no observable branch). **Guardrails-off** (D26): `EligibilityInterval`
>   defaults 0 and `SoftLimitsEnabled` defaults false (the §9 soft→poison paths gated off); the HARD per-IP
>   DoS backstop is kept and re-enablable. `EpochFinalized` decoder de-`totalDT`'d; every `DT`/`EpochDepositRao`
>   read replaced by `SumStDepositedRao` (sums the mirrored `Deposited` log). Serviceless + pg/redis suites green.
> - **DONE (validator).** Pool weight = `implied_usage × Q_n` (`implied_usage = epoch_deposit / rate(tier)`)
>   with deposits/conviction summed from the on-chain `Deposited` log (`DepositedSums`, chunked `eth_getLogs`)
>   and the tier→rate schedule floored above zero; head weight = **split-adjusted routable-IP score**
>   (`score(u) = Σ_{h∈IPs(u)} 1/claim(h)`, EMA-smoothed) from the trail egress-hashes; `chain.DT` removed.
>   `go build/vet/test ./...` green.
> - **Launch: testnet-first (D28)** — reverts the D24 mainnet-direct note below; `docs/LAUNCH.md` and
>   `WHITEPAPER §16.3` are back to the M0→M6 testnet ladder (mainnet = the later Phase-E promotion).
> - **Infra (new).** `xops/main/ansible/run-subtensor.sh` + `playbook-subtensor.yml` deploy a subtensor
>   lite node + plain (LAN-only) nginx RPC gateway onto host **snow**; per-host config in
>   `host_files/snow/subtensor/`.

> **v0.3 (D23) IMPLEMENTED 2026-07-03 — deposits are buybacks; effort bounty deferred.** The full
> cascade landed and is GREEN everywhere:
> - **Contract**: `deposit()` credits `DT` then `moveStake`s the FULL amount to the locked
>   **buyback reserve** (`reserveHotkey` = owner-validator hotkey, set once at initialize, ≠
>   treasury/miner hotkeys; strict move — failed move reverts the credit; `buybackTotal` +
>   `BuybackReserved` event = on-chain audit; **one-way invariant**: no code path sources a transfer
>   from the reserve — sibling of "finalized claims sacrosanct"). `poolTotal = poolEmission + carry`
>   (emission-only). Effort-bounty subsystem REMOVED from v1 (validator/vpk registry, serverKeys,
>   submitTrails/prove/reseed/disputes, claimValidator, feePool/φ/ω/sampleK, fundFeePool) — the
>   hardened v0.2 implementation is **parked byte-for-byte at `docs/parked/`** (with README) for the
>   (X) phase. `trailsWindowBlocks` kept as a reserved dial (epoch API stable). Deposited event is
>   4-arg; EpochFinalized is (e, totalDT). Forge **68/68** (rewritten suites incl. reserve-movement,
>   failed-move rollback, one-way-reserve-under-hostile-upgrade, escrow+reserve conservation).
> - **ABI/stabi**: regenerated (97→62 methods), tests updated, green.
> - **stctl**: submit-trails/claim-validator removed; new `initialize` command (golden-pinned
>   calldata) + reserve-aware `deploy-status`/`state`/event decoding. **snclaim**: fixture swap only.
> - **validator**: effort submission/claim/registration paths removed (trail-proof persistence KEPT
>   as the parked phase's input); steering untouched; 32 tests green.
> - **server**: Deposited/EpochFinalized decoders trimmed, BuybackReserved added, removed-event
>   decoders deleted; zero interface changes; serviceless + **pg/redis integration suites re-run
>   green** (st model 210s + e2e verify).
> - **Deploy.s.sol** (`ST_RESERVE_HOTKEY` required; φ/ω/sampleK gone), **evm/README** rewritten,
>   **mechanism diagram** regenerated, WHITEPAPER v0.3 + DISCUSS D23 + COMPARISON/VALIDATOR
>   touchpoints all synced.
>
> **D24 (2026-07-03): v1 launches DIRECTLY ON MAINNET — no public-testnet phase.** Runbook:
> **`docs/LAUNCH.md`** (replaces docs/TESTNET.md): SP-3 localnet rehearsal (promoted to required) →
> mainnet dust probes (SP-1 battery incl. reserve dividend-compounding/take; SP-2 `check-metadata`
> vs finney) → ONE scripted genesis window (own UIDs first, reserve-hotkey take 0, short rehearsal
> epochs + dust D-3 cap) → ramp to production params. WHITEPAPER §16.3 milestones reframed; PLAN.md
> amended. **SP-1 harness BUILT + CI-green** (2026-07-03): `evm/src/probe/STSubnetProbe.sol`
> (throwaway probe reproducing STSubnet's precompile shapes + `mirror(this)` custody) +
> `evm/script/SP1Conformance.s.sol` (deploy + on-node `cast` battery playbook) +
> `evm/test/SP1Probe.t.sol` (7 tests — proves the battery logic before it touches mainnet). Runs
> on-node via `cast` (subtensor precompiles are runtime-only; forge sim can't execute them).
> **forge 75/75** (68 + 7); production STSubnet ABI byte-identical (probe is additive — no stabi
> regen, no Go changes). `docs/LAUNCH.md` B1 has the invocation sequence + the readBattery matrix.
>
> The v0.2-scoped narrative BELOW is retained as the build history of the pool+head+effort work —
> read it with D23 in mind (the effort machinery it describes is now the parked (X)-phase code).

The full whitepaper v1 — **both miner tiers (pool + head) plus the hardened effort bounty** — is
implemented, integrated, and tested green across all repos. **Nothing is committed** (branches `sn`
exist on connect+server; `urfoundation/sn` on `main`). One decision open before commit (bottom).

## Green as of the final integration sweep
| Repo / module | build | test |
|---|---|---|
| `sn` (crv4, merkle, ss58, stabi, stctl) | ✅ | ✅ `go test ./...` |
| `sn/validator` (own module) | ✅ | ✅ incl. effort + head-steering + HF-4 self-mask tests |
| `sn/snclaim` (own module) | ✅ | ✅ incl. bindHead calldata tests |
| `sn/evm` (Foundry) | ✅ | ✅ **133/133** `forge test` (incl. HF-2 reseed-cap regression) |
| `urnetwork/connect` (+provider) | ✅ | ✅ Verify/Sn/Bind + provider |
| `urnetwork/server` | ✅ | ✅ pure-logic st/sn/verify controller + HF-1 replay model tests |

**pg+redis integration tests: DONE (2026-07-03)** against the local services from `test.sh`
(`local-pg`/`local-redis.bringyour.com`, `/etc/hosts` → 10.211.55.3; per-test fresh DB + full
migrations — which also validates the new st/verify migrations live). New suites, all green:
- `model/st_model_db_test.go` — st wallet/epoch (status monotonicity)/leaves (idempotent
  replace)/publish/event dedup+order/high-water; head-binding upsert guard; the HF-1 SQL leg
  (`GetHeadBoundCkeysInEpoch` from seeded `st_event` rows incl. log_index-order-sensitive
  same-block cases + the unbind-at-close-1 dodge); epoch summary cache; ckey MGET;
  usage window sums; reliability join/window.
- `model/verify_model_db_test.go` — egress bijection (ambiguous downgrade, two-ip fail,
  release hooks), eligibility membership, V3 trail lock (mutual exclusion + ttl self-heal),
  V4/V5 rate meters, eligibility tokens, trail lifecycle, reaper (expire/persist/re-score/
  poison-never-persisted), idempotent stats rollup, next-hop sampling (exclusions, token
  exhaustion, pad).
- `controller/verify_controller_db_test.go` — end-to-end `POST /verify`: a real
  SEED→EXTEND×3→FINAL walk at M=4 with all four Ed25519 sigs verified third-party style
  (incl. the coverage-bound effort-digest FINAL sig), §4.3 idempotent replay, §7.6 stats
  (seed hop excluded), durable proof row; poison V2 stable synthetic seed, V1
  poison/real error equality, V5 hard-limit refusal, expired-row semantics.
Run: `WARP_ENV=local WARP_SERVICE=test WARP_DOMAIN=bringyour.com WARP_BLOCK=test
WARP_VERSION=0.0.0 BRINGYOUR_POSTGRES_HOSTNAME=local-pg.bringyour.com
BRINGYOUR_REDIS_HOSTNAME=local-redis.bringyour.com go test ./model ./controller -run …`
(server root `go test .` also green with services — 81s).

## Two miner tiers + hardened effort (what's new since the pool-tier-only build)

**Effort-bounty hardening (closes review F1/A2 — the mechanism is now sound):**
- **Coverage is server-attested + unforgeable.** The server signs the *effort digest*
  `sha256(finalDigest ‖ uint256_be(coverage))` (coverage = M−1), the validator co-signs it and
  carries coverage in the leaf, and the contract verifies both sigs over it (recomputed on-chain from
  the leaf's `finalDigest`+`coverage`). Single source of truth: `connect.VerifyEffortDigest`, golden
  vectors pinned in connect + evm.
- **Credit is sample-derived, not the free claim.** `proveTrailSamples` credits
  `min((Σ sampled coverage)·leafCount/k, claimedEffort)` — an unbiased estimator the validator can't
  cherry-pick (post-submission seed) or inflate (signed coverage), capped at its own declaration.
  `k=0` credits 0. Replay stays blocked by `pathId` dedup.
- The two exploit PoCs are now **mutation-verified regressions** (`evm/test/EffortHardening.t.sol`):
  the `1e27` inflated claim credits the real estimate (40); a forged-coverage leaf fails the sig check.

**Head tier — top-level miner UIDs (whitepaper §8.4/§11.4), both tiers now in parallel:**
- **On-chain binding registry** (`STSubnet.sol`): `bindHead(hotkey, clientId, clientIdSig)` /
  `unbindHead` — dual proof (Ed25519 `clientId` sig via 0x402 + metagraph `getColdkey(uid)==mirror(sender)`),
  `headBindDigest` domain-separated like `vpkBindDigest`; rebind = dual-proof-gated re-point; getters
  `headClientIdToHotkey`/`headHotkeyToClientId`; events `HeadBound`/`HeadUnbound`. 13 tests.
- **Validator steering** (`sn/validator/steer.go`): reads bindings per-ckey (`eth_call`, cross-tempo
  cache), weights bound **live** UIDs on pure `Q_p` (no deposit term), θ head / (1−θ) pool split, empty
  head cedes to pools; fails closed (no key / unbound / dead UID → skip).
- **Server exclusion** (`st_controller.go`/`st_model.go`): mirrors bindings via event sync into
  `st_head_binding`; drops head-bound providers from the pool `payoutRoot` (no double-pay). **Tier
  exclusivity is symmetric** — the same head-bound ckey set is removed from the pool on both sides.
- **Tooling**: `provider bind-head`/`unbind-head` (stdlib: reads `headBindDigest` via eth_call, signs
  with the client key, prints intent) + `snclaim bind-head`/`unbind-head` (submits; recomputes the
  digest under its own sender and verifies the provider's sig before spending gas).

## Build-time deviations that OVERRIDE the plan text (keep)
- Deposits **push-then-credit** (no approve/transferStakeFrom in StakingV2 v3.2.7).
- Contract UUPS `initialize(...)`, amounts in **rao**.
- CRv4 ciphertext arkworks `TLECiphertext<TinyBLS381>`; extrinsic `commit_timelocked_weights`, version 4.
- Effort leaf = 9-field tuple; **sigs now over the effort digest** (`sha256(finalDigest‖coverage)`),
  binding coverage. Server determines coverage (M−1) and attests it.
- Head binding `clientId` = the provider's **client Ed25519 key (ckey)** — the server/validator bridge
  ckey↔UUID-client_id via `GetClientPublicKey` / `/key/<id>`.

## Adversarial reviews (all applied)
`docs/REVIEW_FINDINGS.md` + `docs/review-artifacts/`. Contract (earlier): fundFeePool onlyOwner,
minerHotkey≠treasuryHotkey, per-epoch window snapshot, MAX_OPERATORS. `/verify`: poison source-check,
deterministic synthetic seed id, per-trail lock, EXTEND meter, nginx-invariant. Effort/head review:
**complete, fixes applied** (HF-1…HF-6, see FIX STATUS in the findings doc) —
- **HF-1 (HIGH, fixed)**: pool exclusion is now epoch-interval, not active-at-close — the close
  replays the HeadBound/HeadUnbound log (`GetHeadBoundCkeysInEpoch`, pure core
  `StHeadBoundCkeysFromEvents` + serviceless tests incl. the unbind-at-close-1 dodge); the
  active-only getter is removed. Kills the bind-all-epoch/unbind-at-close-1 double-pay.
- **HF-2 (MED, fixed)**: `reseedTrailSamples` capped (`MAX_RESEEDS=2`) + validator-only; honest
  residual documented in-code (leafCount-at-submit deferred to permissionless-validator hardening).
- **HF-4 (LOW, fixed)**: validator self-mask fails CLOSED — cached definitive answer on read error,
  no answer → skip the tempo's commit.
- **HF-3/HF-5/HF-6**: documented (HF-3 = the network-granularity caveat below; HF-6's same-block
  ordering made explicit via a (block, log_index) sort in `stApplyHeadBindings`).

## Known v1 caveats (documented, acceptable)
- Server payout exclusion is **network-granularity**: if any contributing provider in a network is
  head-bound, the whole network is dropped from the pool that epoch. Conservative (never double-pays;
  usage isn't per-provider so a finer split would leak head usage into the pool). Under-pays a
  multi-provider network's non-promoted providers — fine where network≈provider (serious miners).
- Binding/close timing: RESOLVED by HF-1 — the server excludes on the epoch-interval head set
  (bound at any block of [E_start, E_close]), which is exactly the set the validator paid per
  tempo; no double-pay window remains. A provider bound mid-epoch forgoes the pool for that whole
  epoch (conservative, matches "head earnings for those tempos").
- HF-5 (self-inflicted): a head binding whose hotkey dies (deregistered) earns neither tier —
  steering skips the dead UID (fail closed) while the pool exclusion still applies — until the
  provider calls `unbindHead`. Public on-chain state; the provider can always self-serve unbind.

## Open decision (before commit)
- **Commit granularity.** Branches `sn` exist (connect+server); sn on `main`. Nothing committed.
  Commit per-repo now, feature-branch first, or hold for review? (The effort-mechanism decision is
  resolved — hardened; the head tier is built.)

## Remaining external gates
- **SP-1** precompile conformance (ABIs vendored from subtensor v3.2.7, unverified live except 0x402) —
  now also gates the head binding's 0x402 `clientId` sig check + `getColdkey`/`uid` reads.
- **SP-2** CRv4 reference vectors (live-verified vs test.finney; keep as the pre-deploy gate).
- ~~Server pg+redis integration tests~~ DONE 2026-07-03 (see the integration-test section above).
