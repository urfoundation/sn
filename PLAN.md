# UR Subnet — Implementation Plan

**Code changes across `sn`, `urnetwork/server`, and `urnetwork/connect` to implement `WHITEPAPER.md` v0.2
(pool tier + `/verify`), targeting a subtensor test subnet first.**

> **AMENDMENT (2026-07-03, WHITEPAPER v0.3 / D23 — deposits are buybacks; bounty deferred).** This plan
> was drafted and built against v0.2. D23 changes the v1 contract scope AFTER the build below completed:
> **(1)** `deposit()` now moves the full amount to a **buyback reserve** (`moveStake` →
> `reserveHotkey` = the owner-validator hotkey; one-way invariant; `buybackTotal` accounting;
> reserve/escrow hotkeys must stay distinct or dividend accrual corrupts the push-then-credit check);
> **(2)** `finalizeEpoch`: `poolTotal = poolEmission + carry` **only** (drop `depositNet`); **(3)** the
> whole effort-bounty subsystem leaves v1 (`phiBps`/`omegaBps`/`feePool`/`fundFeePool`,
> `registerValidator`+vpk registry, `submitTrails`/`proveTrailSamples`/`reseedTrailSamples`/effort
> disputes, `claimValidator`) — **parked as the (X)-phase implementation, not discarded** (the hardened
> effort machinery incl. HF-2 and the A2 coverage-signing stays in history/branch for that phase);
> **(4)** ABI changes → `stabi` regen → rebuild stctl/snclaim/server-st/validator binders; `snclaim`
> loses submit-trails, validator loses the effort builder from the v1 path; server `/verify` + payout
> pipeline unchanged (poolTotal is read from chain); steering (`DT` gross) unchanged. See
> `WHITEPAPER.md` §6.3–§6.4, §7.4, §8.3, §12.4 and `WHITEPAPER_DISCUSS.md` D23.

> **AMENDMENT (2026-07-03, D24 — mainnet-direct launch; no public-testnet phase).** The "targeting a
> subtensor test subnet first" framing above and the §3 testnet runbook are superseded: v1 launches
> **directly on finney**. Replacements for what testnet was load-bearing for: **SP-1** → mainnet dust
> probes before the subnet exists (a throwaway probe contract on chain 964 against an *existing*
> netuid; the battery now also covers reserve-hotkey dividend auto-compounding + take. Harness
> **BUILT + CI-green**: `evm/src/probe/STSubnetProbe.sol` + `script/SP1Conformance.s.sol` +
> `test/SP1Probe.t.sol` (7 tests); subtensor precompiles are runtime-only so the battery runs on-node
> via `cast` against the deployed probe); **SP-2** →
> `sp2 check-metadata` re-pinned read-only against the finney runtime (format already verified vs
> test.finney); **SP-3 localnet** → PROMOTED from nice-to-have to REQUIRED (pinned to the live finney
> runtime tag; the full genesis dry-run + failure drills); the **testnet soak** → a mainnet ramp
> (launch with short epochs + a dust `deposit_epoch_cap_rao`, several clean cycles, then
> `setEpochParams(50_400, …)` and raise the cap stepwise — D-11/F2 make this a config call). Genesis
> is ONE scripted window (registration starts the start_call/emission clock — do not register until
> the rehearsal + probes are green); own UIDs registered first under a defensive registration
> posture; the owner-validator α position acquired in the first hours (it IS the first buyback and
> the §9.2 majority seat); reserve-hotkey take = 0 before the first deposit. Runbook:
> **`docs/LAUNCH.md`** (replaces `docs/TESTNET.md`). test.finney remains an optional free scratchpad
> only — its runtime may differ from finney, which is exactly why conformance now runs on mainnet.
> Zero code changes (the 945 profile stays for probes; the localnet inherits testnet defaults).

> **AMENDMENT (2026-07-03, WHITEPAPER v0.4 / D25–D28).** **(a) D28 reverts D24 — v1 is testnet-first
> again.** The SP-1/SP-2/SP-3 harness is endpoint-parameterized, so it **re-targets to testnet with zero
> code change** (`SP1_NETUID` + `--rpc-url testnet`, chain **945** / `wss://test.finney`); **mainnet is
> the later Phase-E promotion** (re-run the probe battery against finney before the real deploy).
> `docs/LAUNCH.md` is back to the testnet-bootstrap-then-mainnet runbook (Phases A–E; mainnet gated
> behind a clean testnet run). **(b) v0.4 (D25–D28) also reshaped the economic core** — conviction-staking
> deposits (the contract drops the `DT`/`totalDT` ledger and does custody + settlement only),
> **validator-computed** pool weights `implied_usage × quality` (implied usage = deposit ÷ conviction-tier
> rate; the contract weighs nothing), and an **IP-breadth head** (top-200 fleets by split-adjusted routable
> egress-IP count). See `WHITEPAPER_DISCUSS.md` D25–D27; contract + `stabi` are done, the
> validator/server/connect cascade is in progress (`IMPLEMENTATION_STATUS.md`).

Drafted 2026-07-01 from: `WHITEPAPER.md` v0.2, `VALIDATOR.md`, `WHITEPAPER_DISCUSS.md` (D1–D21), exploration
of `urnetwork/connect` and `urnetwork/server`, and an adversarially-verified deep-research pass on subtensor
testnet + EVM deployment (findings + open items in §3, §10).

Scope = the five requested workstreams:
1. Validator API bindings in connect + a `validator` binary — home **revised 2026-07-01 to `sn/validator`**
   (still a peer of `connect/provider` in CLI/build conventions; moved with D-5 for gomobile isolation and
   subnet-code consolidation)
2. `/verify` validator API in `server/api` + `server/model`, hot path state in Redis
3. Merkle pool-claim support in the provider (wallet threading + claim)
4. ST contract in `sn/evm` + testnet deployment
5. `server/controller/st_controller.go` + `server/model/st_model.go` chain coordination
   (deposits, payout-root publishing, epoch finalize)

Out of scope for this iteration (explicitly deferred, seams left for them): the **head tier**
(top-level-miner `client_id ⇄ hotkey` binding, §11.4), disputes UI, Pool 1 / mechanism 1, effort-bounty
coverage curve tuning. Commit-reveal v4 weights are **in scope** (D-1 settled 2026-07-01: Go-native from
day one — no CR-off interim, no sidecar).

---

## 0. Repo map — where every piece lands

| Repo | New / changed | Purpose |
|---|---|---|
| `sn/evm/` | **new** Foundry project | ST contract (`WHITEPAPER.md` §6), precompile interfaces, tests, deploy scripts |
| `sn/stabi/` | **new** Go pkg | abigen bindings generated from `evm` artifacts (replaces hand-written `stctl/st_abi.json`) |
| `sn/merkle/` | **new** Go pkg | OZ-compatible keccak Merkle tree/proofs — single implementation shared by server, provider, stctl (test vectors vs Solidity) |
| `sn/stctl/` | **implement** (Makefile exists) | ops CLI: register-operator, deposit, commit, finalize, claim, status (docopt + go-ethereum; go.mod deps already declared) |
| `sn/validator/` | **new binary, own Go module** | the validator: trail engine + stats + steering (CRv4) + effort claims — mirrors `connect/provider` conventions; imports connect via `replace => ../../urnetwork/connect` |
| `sn/snclaim/` | **new tiny module** | claim-submission CLI (go-ethereum): `snclaim submit` signs + sends `claimMiner` (D-6) |
| `server/api` | routes + `handlers/verify_handlers.go`, `handlers/st_handlers.go` | `/verify` (SEED/EXTEND), `/verify/keys`, `/sn/wallet`, `/sn/pool/claim`, `/sn/epoch` |
| `server/model` | `verify_model.go`, `st_model.go` + migrations | trail state (Redis), verify stats, `verify_trail` table; st tables (wallets, epochs, leaves, publishes) |
| `server/controller` | `verify_controller.go`, `st_controller.go` | trail protocol orchestration; **all** subtensor coordination (deposits, roots, finalize, event sync) |
| `server/taskworker/work` | `verify_work.go`, `st_work.go` | reaper + stats jobs; epoch pipeline tasks |
| `server/jwt`-style vault | `verify.yml`, `st.yml` | server Ed25519 trail-signing keys; EVM hot-wallet keys + RPC config |
| `connect` (root pkg) | `api_verify.go` (+ types) | control-plane API bindings + shared wire/canonical-message types (stdlib-only, gomobile-safe) |
| `connect/provider/` | args + subcommands | `--wallet` threading, `provider wallet set`, `provider claim` (stdlib-only verify) |

**Module wiring & workspace layout (D-5, revised 2026-07-01).** Repos stay at their true locations — no
symlinks, no module publishing: cross-repo `replace` directives point at real relative paths. From this
repo, urnetwork modules are `replace github.com/urnetwork/X => ../../urnetwork/X`; from the urnetwork side,
server adds `replace github.com/urnetwork/sn => ../../urfoundation/sn` (for `stabi`/`merkle`). `connect` is
compiled into the gomobile SDK (`sdk` requires it), so heavy chain deps must never enter its module graph —
which is why the subnet binaries live **here**: `sn/validator` and `sn/snclaim` are their own modules
replacing `../../urnetwork/connect` and `../../urnetwork/glog`. (Go `replace` directives do **not** inherit
from a dependency's go.mod, so each module lists its full replace set.) `server` already depends on
`go-ethereum v1.16.7` (`model/auth_model.go:568`, EIP-191 verify) — adding `ethclient`/bindings needs no new
top-level dep. The one build-infra consequence: CI/build contexts must span both checkout roots
(`urnetwork/*` and `urfoundation/sn`) — server's warp/Docker context gains `../../urfoundation/sn`, and
sn-module builds need the urnetwork siblings present.

---

## 1. Build order

Four tracks run in parallel after Phase 0; hard dependencies marked →.

```
Phase 0  SPIKES + SUBNET BOOTSTRAP (sn)          — §3 runbook; SP-1..SP-4 close the research gaps
Phase 1  CONTRACT (sn/evm + stabi + stctl)       — everything on-chain depends on the ABI
Phase 2  /verify (server + connect types)        — independent of chain; testable standalone
Phase 3  VALIDATOR BINARY trail engine (sn/validator) — → Phase 2 (needs /verify live)
Phase 4  POOL CLAIMS (server st_model wallets/leaves + provider) — → Phase 1 (ABI), Phase 2 (reliability stats)
Phase 5  st_controller EPOCH PIPELINE (server)   — → Phase 1; e2e epoch on testnet with short T_epoch
Phase 6  STEERING + EFFORT (CRv4 weights: Go tlock + gsrpc, submitTrails, claims) — → Phases 1,3,5 + SP-2; = WHITEPAPER M1/M2
```

Milestone mapping: Phase 1+0 ≈ whitepaper **M0**; Phase 4+5 ≈ **M1** (minus multi-validator); Phase 6 ≈
**M1/M2**; the +4h/+24h/+48h window automation in Phase 5/6 ≈ **M3**.

---

## 2. `sn/evm` — the ST contract (workstream 4a)

Foundry project (`forge init`), Solidity **0.8.24**, `evm_version = "cancun"` **pinned** in `foundry.toml`
(solc ≥ 0.8.30 defaults to prague — the pin is load-bearing).

```
evm/
  foundry.toml
  src/STSubnet.sol            # custody/settlement core (§6 state + interfaces)
  src/STSubnetPolicy.sol      # (optional split now, per §6.4.3: admission/params module)
  src/interfaces/             # IStakingV2 (0x805), INeuron (0x804), IMetagraph (0x802),
                              # IAlpha (0x808), IEd25519Verify (0x402), IBalanceTransfer (0x800)
  test/                       # unit tests w/ precompile mocks (vm.etch at the precompile addresses)
  script/Deploy.s.sol         # proxy + impl deploy; testnet + mainnet profiles
  abi/                        # exported artifacts -> abigen input for sn/stabi
```

**v1 contract scope** (matches `stctl/st_abi.json`, which becomes generated output):
- Registries: `registerOperator` (owner-gated), `registerValidator(hotkey, vpk, ed25519Sig)` —
  **permissionless** (D-10): verifies `Metagraph.getColdkey(uid(hotkey)) == mirror(msg.sender)` plus the
  `0x402` vpk-binding signature.
- Deposits: `deposit(noId, alphaAmount)` pulling via StakingV2 `transferStakeFrom` (NO pre-`approve`s);
  route `φ` to `feePool`.
- Epoch machine: `epoch`, `epochStartBlock`, **`T_EPOCH` constructor/param-settable** (50,400 mainnet; small
  on testnet so a full epoch e2e runs in minutes — the whitepaper constant becomes a bounded parameter), and
  **governance-settable window params** (D-11): commit window (4h mainnet default, generous on testnet),
  trails (+24h) and finalize (+48h) offsets. A missed commit rolls that pool's total into the next epoch.
- `commitOperator(e, noId, payoutRoot, off)` (+4h window), `submitTrails(e, valId, trailsRoot,
  claimedEffort)` (+24h; random-sample `0x402` checks — sample count `k` a parameter, seeded from
  `blockhash`/`prevrandao`), leaf dispute in +24–48h, `finalizeEpoch(e)` (+48h, permissionless).
- Claims: `claimMiner(e, noId, coldkey, shareBps, proof)` per §11.2 (OZ `MerkleProof`, per-pool
  over-drain cap, dedup key `(noId, coldkey)`), `claimValidator(e, valId)` (pure on-chain arithmetic).
- Custody: all held α staked to `treasuryHotkey` **except** per-NO pool emission accrues on each pool's own
  `minerHotkey` — keep them separate so `poolTotal_n` can be measured as a **stake delta snapshot** on the
  pool hotkey between epoch boundaries (see D-4; more robust than trusting `getEmission` point reads).
- Governance: Phase-0 posture (owner multisig + upgradeable proxy + pause-only guardian), with the §6.4
  invariant enforced structurally: finalize is append-only; claim paths for finalized epochs are
  upgrade-independent (test this with an upgrade-under-fire test).

**Precompile interfaces are a verification deliverable, not a copy-paste.** The research pass confirmed only
`0x402` Ed25519 (`verify(bytes32 msg32, bytes32 pubkey, bytes32 r, bytes32 s) → bool`, message must already
be 32 bytes) and flagged the rest (0x800/0x802/0x804/0x805/0x808 ABIs, contract-as-coldkey custody, rao vs
18-dec units) as **unverified against the live runtime** — and the `opentensor/evm-bittensor` examples repo
is ~10 months stale with dead endpoints. So: vendor interfaces from `opentensor/subtensor`
`precompiles/src/` at a **pinned release tag**, and write **SP-1 conformance tests** (below) that exercise
each call on testnet before any dependent code lands.

**Merkle spec (must match Go exactly):** leaf = `keccak256(bytes.concat(keccak256(abi.encode(bytes32
coldkey, uint256 shareBps))))` (OZ double-hash standard), sorted-pair internal hashing, proofs verified with
OZ `MerkleProof.verify`. `sn/merkle` implements the identical scheme in Go; a shared JSON test-vector file is
consumed by both `forge` tests and `go test`. Same scheme for the `(trail, coverage)` effort leaves.

**`sn/stctl`** (deps already in `sn/go.mod`: docopt, go-ethereum, yaml, x/crypto): subcommands
`deploy-status | register-operator | deposit | commit-root | submit-trails | finalize | claim-miner |
claim-validator | epoch | state` — thin calls through `sn/stabi`, key/RPC config via yaml (mirrors provider
conventions). Deployment itself stays in `forge script`; stctl is runtime ops + inspection.

---

## 3. Testnet deployment runbook (workstream 4b — research-verified)

Verified facts (live-checked 2026-07-01; citations in the research output):

1. **Chain endpoints.** Substrate: `wss://test.finney.opentensor.ai:443` (btcli `--network test`). EVM
   JSON-RPC: **`https://test.chain.opentensor.ai`, chain ID 945** (`0x3b1`, probed live). Explorer:
   `evm-testscan.dev.opentensor.ai`. **Do not** use `evm-testnet.dev.opentensor.ai` (dead; still referenced
   by the stale examples repo). Mainnet later: `https://lite.chain.opentensor.ai`, chain ID 964.
2. **testTAO.** No automated faucet — request in the Bittensor Discord (moderated form). Testnet TAO ≠
   devnet TAO. Localnet uses the pre-funded Alice account instead.
3. **Create the subnet.** `btcli subnet burn-cost --network test` (dynamic: decays over time, doubles per
   creation) → `btcli subnet create --network test`. Testnet creation rate limit is **720 blocks (~2.4h)**
   (vs 14,400 mainnet), so iteration is cheap.
4. **Hyperparameters.** `btcli sudo set --netuid N --param <exact-chain-metadata-name> --value <v>` —
   owner-gated, per-parameter cooldown of `tempo × OwnerHyperparamRateLimit` (default 2 tempos). All four we
   care about (`commit_reveal_weights_enabled`, `max_weight_limit`, `immunity_period`,
   `min_allowed_weights`) are settable by exact name even where btcli doesn't enumerate them. Testnet
   setting: **commit-reveal ON** once the SP-2 CRv4 harness passes (togglable off only for debugging —
   D-1 settled Go-native CRv4 from the start), `max_weight_limit` to a real cap,
   `min_allowed_weights = 1`, high `immunity_period`.
5. **Emissions activation.** New subnets are inactive: after `StartCallDelay` from registration
   (`btcli subnets check-start`), owner runs `btcli subnets start --netuid N`. Plan the delay into the
   testnet schedule (exact testnet value unverified — check on-chain; ~7 days on mainnet, shorter on test).
6. **Fund the EVM deployer.** `H160 → SS58` is `ss58_encode(blake2_256("evm:" ‖ H160), prefix 42)`
   (one-way; no substrate key exists for the mirror). `btcli wallet transfer` testTAO to that mirror → the
   H160 sees the balance; deploy with Foundry/Hardhat pointing at chain 945. Keep a funding helper in
   `stctl` (`stctl evm-address <H160>` prints the mirror SS58).
7. **Compiler.** solc 0.8.24 / Cancun (docs-pinned pairing; newer solc requires explicit
   `evm_version = cancun`).
8. **Mechanisms.** Multi-mechanism is live (`MechanismCount` default 1, `max_uids × mechanism_count ≤ 256`,
   `btcli subnet mechanisms`). We stay at `mechanism_count = 1` per D19.
9. **Commit-reveal v4** uses **drand timelock encryption performed client-side** (`bittensor-drand`,
   `get_encrypted_commit_v2`, SDK v11 stateful epoch schedule). The "chain handles it automatically" reading
   was adversarially refuted. Direct consequence for our Go validator: **D-1**.

**Bootstrap sequence** (one-time; now the MAINNET genesis script — D24, `sn/docs/LAUNCH.md`): localnet
rehearsal + mainnet dust probes FIRST → coldkey + real TAO → create subnet → set hyperparams (defensive
registration posture) → register owner validator UID (= reserve hotkey, take 0) → fund EVM deployer +
ops keys → `forge script` deploy proxy+impl (short rehearsal epochs, dust deposit cap) → `stctl
register-operator` (contract `burnedRegister`s the pool UID; burn denomination pre-pinned by the SP-1
probes) → start call when eligible → first CRv4 commit → short-epoch e2e cycles → ramp to production.

**Spikes (close the research gaps before dependent phases):**
- **SP-1 Precompile conformance (blocks Phases 1/4/5/6).** On testnet, from a scratch contract + Go: pin
  subtensor release; verify each precompile address + ABI; prove **contract-as-coldkey custody** (contract
  `addStake`s, holds, `transferStake`s out); establish **units** (rao `uint64` vs 18-dec wei) for every
  amount-bearing call; measure `0x402` gas (sizes `k` for `submitTrails` sampling); confirm `burnedRegister`
  cost/denomination; confirm Spec-421 `miner_burned` status on current runtime.
- **SP-2 CRv4 conformance harness (blocks Phase 6 — a build track, per settled D-1).** Go-native
  commit-reveal end to end: `drand/tlock` ciphertexts cross-checked against `bittensor-drand` reference
  vectors (generated with the Python/Rust lib), SDK-v11 reveal-round/epoch schedule computation, commit
  extrinsic encoding via go-substrate-rpc-client, sr25519 signing (go-schnorrkel) by the validator hotkey.
  Also verifies Neuron-precompile `burnedRegister` semantics (coldkey = caller's mirror, hotkey = arbitrary
  AccountId32) required by D-10. If truly blocked, D-1 is revisited — the Python sidecar is the recorded
  contingency, not the plan.
- **SP-3 Localnet CI (nice-to-have for Phases 1/5).** EVM-enabled local subtensor (docker; fast blocks,
  pow-faucet, Alice) for `forge`/Go e2e in CI; public testnet stays the integration target. Also pins the
  node's RPC surface for §11.1 (flags/ports — one subtensor node serves both substrate ws and eth JSON-RPC).
- **SP-4 Event indexing.** Confirm `eth_getLogs` reliability/retention on the public testnet RPC (drives
  st_controller's sync strategy: logs vs state polling).

---

## 4. `server` — the `/verify` validator API (workstream 2)

Implements `VALIDATOR.md` §§2–9 exactly; the protocol spec already references the reuse points. Redis holds
**all hot per-trail/per-provider state** (the stated goal: no DB pressure on the trail path); Postgres gets
only completed proofs + periodic stat rollups.

**Routes** (`api/api.go`, handlers in `api/handlers/verify_handlers.go`):
- `POST /verify` — one route, two body shapes (SEED: `vpk, client_nonce, seed_sig, M`; EXTEND: `trail_id,
  trail[], extend_sig`). **`WrapWithInputNoAuth`** — auth is the protocol's own Ed25519 signatures (SEED sig
  under `vpk`), *not* a JWT: the caller's source IP must be the provider egress, and the protocol is
  self-authenticating + poisoned for unknowns (§9). Body carries the validator's `client_id` so the server
  checks `vpk == model.GetClientPublicKey(client_id)` (`network_client_key_model.go:24`) without a reverse
  key index.
- `GET /verify/keys` — published server Ed25519 public keys by `server_key_id` (proof verification;
  unauthenticated, like `GET /key/([^/]+)` at `api.go:96`).
- Source IP via `session.NewClientSessionFromRequest` order (`client_session.go:41`): `X-UR-Forwarded-For` →
  `X-Forwarded-For` → `RemoteAddr`.

**`model/verify_model.go`** — Redis layout (cluster hash-tag rule: everything pipelined for one trail shares
`{vtr_<trail_id>}`):
- `{vtr_<id>}h` header hash (vpk, server_nonce, M, status, times), `{vtr_<id>}hops` list,
  `{vtr_<id>}pending`, `{vtr_<id>}resp` (idempotent retry cache, §4.3); `EXPIRE = TrailTTL`.
- `{velig_<clientId>}` eligibility token bucket (§5.3) — INCR/EXPIRE idiom from
  `connect/transport_rate_limit.go:123-144`.
- `{vstat_<clientId>}` counters: `assignments`, `confirmations`, latency histogram buckets (HINCRBY per
  log-spaced bucket — percentile-recoverable, memory-bounded; simpler than t-digest for v1).
- `verify_egress_<ipv4> → client_id` reverse index (or `HMAC(salt, ip)` variant) with **two feeders behind
  one bijection gate** (D-8 settled: residential included from day one): (1) proxy-allocated egresses from
  `CreateProxyClient` (`network_client_proxy_model.go:583`, WG branch `:666-778`) and the reaper
  `RemoveDisconnectedNetworkClients` (`network_client_model.go:1377`); (2) **observed connection source
  IPs** for residential providers — fed at client connect/auth from the session address, TTL-refreshed
  while connected, dropped on disconnect or IP change. Gate (§8.2): an IP observed backing more than one
  client resolves to nothing; a client without exactly one current egress IP is ineligible; entries expire
  so a reassigned IP is never miscredited (CGNAT/shared egress simply never qualifies).
- Seed rate limits per source IP + per vpk (INCR+EXPIRE), concurrent-trail cap per vpk.
- **Eligible-set sampling** (§5.1): maintain a Redis set of currently-eligible providers (fed by provide-mode
  changes + egress-index membership + token availability), `SRANDMEMBER` + validate, record candidate count
  `n` with each ASSIGN.
- Postgres (migrations appended at tail of `db_migrations.go`): `verify_trail` per `VALIDATOR.md` §6.2;
  `verify_provider_stats(period_start, client_id, assignments, confirmations, latency_pXX…)` rollup.

**`controller/verify_controller.go`** — protocol steps (§4.1/§4.2 checklists verbatim, including poison
trails carried to depth M with indistinguishable timing), canonical message encode/sign for
ASSIGN/FINAL, proof assembly/publication.

**Canonical encodings shared, not duplicated:** the Appendix-A byte layouts (SEED/EXTEND/ASSIGN/FINAL,
`CTX = "urnetwork/verify/v1"`) are implemented once in **`package connect`** (stdlib-only, e.g.
`verify_wire.go`) — server (`replace => ../connect` already present) and the validator binary both import
them; golden test vectors pin the bytes.

**Server signing key:** new vault resource `verify.yml` (peer of `jwt.yml`, loaded with the
`sync.OnceValue` pattern of `jwt/by_jwt.go:38-84`): list of Ed25519 keys with 1-byte `server_key_id`,
newest signs, all published via `/verify/keys`.

**Tasks** (`taskworker/work/verify_work.go`, registered in `taskworker.go`): trail **reaper** (sweep
expired `active` trails into failure stats, §4.4 — modeled on the reliability cleanup tasks); **stats
rollup** Redis→Postgres (model: `UpdateClientReliabilityScores`, `network_client_reliability_model.go:173`);
optional eligibility-set refresher.

---

## 5. `server` — pool claims + wallet threading (workstream 3, server side)

- **`st_wallet`** table (in `st_model.go`): `network_id → coldkey_ss58 (bytes32 pubkey), set_time`. New
  route `POST /sn/wallet` (`WrapWithInputRequireAuth`) validating ss58 format. **Deliberately separate from
  `account_wallet`/`payout_wallet`** so the Circle USDC payout planner (`PlanPayments`) never sees subnet
  wallets (D-2 records the wallet-type decision itself).
- **Payout share computation** (epoch task in Phase 5's pipeline): per provider client, usage from
  `transfer_escrow_sweep` (`payout_byte_count`, written by `settleEscrowInTx`,
  `subscription_model.go:1852-1870`) within the epoch × reliability from `verify_provider_stats`
  (`reliability_{n,p}` — the same §7 signal), summed per **network**, joined to `st_wallet`, then
  **aggregated per coldkey** (the contract dedups claims by `(noId, coldkey)` — one leaf per coldkey is a
  hard requirement), floored to `shareBps` with Σ ≤ 10,000 (remainder rolls over on-chain by design).
- **Leaves + proofs**: `st_payout_leaf(epoch, no_id, coldkey, share_bps, leaf_index)`; root built with
  `sn/merkle`; proofs computed on demand (O(log N) from stored leaves) via
  `GET /sn/pool/claim?epoch=e` → `{no_id, coldkey, share_bps, proof[], payout_root, contract_address,
  chain_id, claim_open_block}` (auth: network JWT; provider fetches its own).
- `GET /sn/epoch` — current epoch index/boundaries/deadlines mirrored from chain (read-through cache in
  Redis) so clients don't need an RPC.

---

## 6. `server` — `st_controller.go` / `st_model.go` (workstream 5)

**`controller/st_controller.go`** — all subtensor coordination, one swappable client behind an interface
(mirroring the `CoinbaseClient`/`CircleClient` var-instance pattern so tests stub it):

```go
type StClient interface {           // impl: ethclient + sn/stabi bindings
    Epoch(ctx) (StEpochState, error)                  // epoch, startBlock, windows
    Deposit(ctx, noId, alphaRao) (txHash, error)      // addStake (TAO→α) as needed, approve, deposit
    CommitPayoutRoot(ctx, e, noId, root, off) (txHash, error)
    FinalizeEpoch(ctx, e) (txHash, error)
    SyncEvents(ctx, fromBlock) ([]StEvent, error)     // Deposit/Commit/Finalize/Claim logs (SP-4)
    PoolState(ctx, e, noId) (StPoolState, error)      // poolTotal, claimed, roots — for reconciliation
}
```

- **Config/keys**: vault `st.yml` — `rpc_urls` (**ordered failover list**, §11.1), `chain_id`,
  `contract_address`, `netuid`, `no_id`, keys (deposit hot wallet, ops/poke wallet — separate). Loaded via
  `sync.OnceValue` like `jwt.yml`. Amount handling in **rao** end-to-end once SP-1 pins the units.
- **Idempotency before send** (tasks retry): every publish checks on-chain state first (`noCommit[e][noId]`
  already set? epoch already finalized?) and records results in `st_publish(epoch, kind, tx_hash, status,
  error)` — precedent: `CompletePayment` recording `tx_hash` (`account_payment_model.go:459`).
- **Deposit policy**: deposit sizing = off-chain reference rate × epoch usage (§7.1; rate is config, not
  oracle), executed as one or few DTs per epoch, hard-capped per epoch in config (custody blast-radius
  control, D-3).

**`model/st_model.go`** — tables (append migrations): `st_wallet`, `st_epoch(epoch, start_block,
commit_deadline, trails_deadline, finalize_block, status)`, `st_payout_leaf`, `st_publish`, `st_event`
(synced log mirror); Redis read-through cache for the hot epoch state.

**Tasks** (`taskworker/work/st_work.go`, modeled on the `SchedulePayout`/`Payout`/`PayoutPost` trio,
`account_payment_work.go:19-79`, `RunOnce` keys per epoch):
- `StSyncChain` (periodic): pull epoch state + events → `st_epoch`/`st_event`; schedules the per-epoch
  chain below with `RunAt` derived from **contract** epoch boundaries (the contract clock is authoritative —
  never wall-clock).
- `StEpochClose` (at boundary): freeze usage+reliability window → compute leaves → build root → store.
- `StCommitRoot` (immediately after close; **must land ≤ +4h**): `CommitPayoutRoot` with confirm + bounded
  retries; alert at T-2h if unconfirmed (this deadline is the ops-critical path — D-11).
- `StDeposit` (per policy above).
- `StFinalizePoke` (at +48h): permissionless `finalizeEpoch`; then mark claims open (claim API starts
  serving proofs for e).
- (later, M2+) `StSpotAudit`: verify a sample of published proofs/effort leaves off-chain to arm disputes.

**`bringyourctl`** subcommands (`st deposit|commit|finalize|status`) for manual ops/recovery, wired to the
same controller.

---

## 7. Client side — connect API bindings, `sn/validator`, provider changes (workstreams 1 + 3-client)

### 7.1 `package connect` additions (gomobile-safe)

- `api_verify.go`: Args/Result/Callback trios + methods on `BringYourApi` per the existing template
  (`HttpPostWithStrategy`, `Bearer` JWT, + `Sync` variants — `api.go:135/251/346` are the patterns):
  `VerifyKeys`, `SnSetWallet`, `SnPoolClaim`, `SnEpoch`. Also the **wire types** for SEED/EXTEND
  request/response and the **canonical message builders + signing helpers** (Appendix A) used by both server
  and validator.
- The `api/bringyour.yml` OpenAPI spec gets the new routes (it's the server API definition file — the
  "connect/api" directory is spec, not Go).
- **Trail HTTP calls do NOT go through `BringYourApi`**: they must egress through a specific provider. The
  validator builds them with a plain `http.Client` whose `Transport.DialContext` = the tunnel (below).

### 7.2 `sn/validator` (new binary, own module — home revised from `connect/validator` with D-5)

Mirrors `provider/main.go` conventions (docopt; `~/.urnetwork/jwt`; glog init; `NewEventWithContext` +
`SetOnSignals` shutdown; Makefile copied from `provider/Makefile`, as `stctl`'s already is), importing the
connect SDK via `replace => ../../urnetwork/connect`:

```
Usage:
    validator auth (...)                         # same as provider auth
    validator run [--no=<domain>]... [--nofile=<path>] [--theta=<θ>]
        [--rpc=<evm_rpc>]... [--substrate=<ws_url>]... [--contract=<addr>] [--netuid=<id>]
        [--evm_key_file=<path>] [--hotkey_seed_file=<path>] [--concurrency=<n>] [--api_url=...] [-v...]
    validator register [--stake=<alpha>]         # UID reg via Neuron precompile + ST registerValidator (vpk bind)
    validator submit-trails [--epoch=<e>]        # effort Merkle root -> contract (+24h window)
    validator claim [--epoch=<e>]                # claimValidator bounty
    validator status
```

- **Identity bundle**: `vpk` = its connect client Ed25519 key (`ClientKeyManager`, seed persisted like
  `.provider.key`; public key auto-registers in-band via the `protocol.ClientKey` control frame →
  `ckey_<clientId>`); network/client JWT for control-plane API; **EVM key** for contract calls and coldkey
  ops (its mirror account IS the validator's on-chain coldkey, per D-10); and a **real sr25519 hotkey**
  (seed persisted beside the vpk) that signs the CRv4 commit extrinsics via go-substrate-rpc-client. UID
  registration: Neuron-precompile `burnedRegister` from the EVM wallet with the sr25519 key as hotkey.
- **Trail engine**: seed pick from `FindProviders2`; per-hop egress pinning with
  `ProviderSpec{ClientId: hop}` → `NewApiMultiClientGenerator` → `RemoteUserNatMultiClient` +
  `connect.Tun.DialContext` (`tun.go:583`) as the `http.Client` transport; SEED/EXTEND signing via the
  shared canonical builders; per-hop `StepTimeout`; idempotent EXTEND retries; bounded concurrent trails;
  eligibility-aware pacing. Completed proofs persisted locally (append-only JSONL + index — enough for
  `submitTrails` leaf building and disputes; no cgo).
- **Stats engine**: per-provider `a/c/f`, Wilson interval, latency percentiles, cross-epoch EMA (α ≈ 0.1),
  `a_min` gating, seed-hop exclusion (§7).
- **Steering loop** (Phase 6): each tempo — read operators/deposits (contract) + metagraph (0x802 reads via
  `eth_call`) → `pool[n] = D_n × Q_n`, head slots empty for now → θ split (config; governance-published) →
  u16 normalize + `max_weight_limit` → **CRv4 commit** (D-1): tlock-encrypt the vector to the reveal round
  (`drand/tlock`), submit the commit extrinsic via gsrpc signed by the sr25519 hotkey; reveal follows the
  SDK-v11 epoch schedule. `Q_n` v1 = usage-weighted mean (D-9).
- **Module deps** (kept out of `connect` entirely by living in sn): connect (`../../urnetwork/connect`) +
  glog (`../../urnetwork/glog`), go-ethereum, go-substrate-rpc-client + go-schnorrkel, drand/tlock, docopt.
- **Effort**: at epoch+24h build `(pathId, coverage)` leaves with `sn/merkle`, `submitTrails`; after
  finalize, `claimValidator`.

### 7.3 `connect/provider` changes

- New arg on `provide`/`auth-provide`: `--wallet=<coldkey_ss58>` → on startup, idempotent
  `SnSetWallet` call with the network JWT (already loaded for `provideAuth`, `provider/main.go:621-685`).
  Also `provider wallet set <coldkey_ss58>` standalone subcommand.
- `provider claim [--epoch=<e>] [--rpc=<url>] [--dry-run]` (**stdlib-only**, per settled D-6): fetch
  `SnPoolClaim` → recompute leaf + verify proof against the **on-chain** root (minimal hand-rolled
  `eth_call` JSON-RPC, not trusting the server) → print status/calldata. Transaction submission lives in
  **`sn/snclaim`** (own module with go-ethereum): `snclaim submit` signs + sends `claimMiner`. Claims are
  permissionless, so a foundation-run relayer can batch-submit later without protocol changes — keep the
  leaf keyed by coldkey only.

---

## 8. Testing & e2e

- **Unit**: forge tests w/ mocked precompiles (`vm.etch`); Go tests for merkle (shared vectors vs
  Solidity), canonical encodings (golden bytes), verify model (repo's existing pg/redis test harness,
  `test.sh`); handler tests per existing api patterns.
- **Protocol e2e without chain** (Phase 2/3 gate): local server + N provider processes + validator walking
  real trails; assert stats vs induced failures (kill a provider mid-trail → failure attributed to pending
  hop; §7 semantics).
- **Chain e2e** (Phase 5/6 gates, testnet or SP-3 localnet): short `T_EPOCH`; full epoch: deposits →
  trails → payout root ≤+4h → submitTrails ≤+24h → finalize +48h → provider `claimMiner` + validator
  `claimValidator`; upgrade-under-fire test (upgrade between finalize and claim must not affect claims);
  weights → emission accrual on the contract-owned pool UID (M1 check).

---

## 9. Decisions — SETTLED 2026-07-01

All thirteen decided with the user (proposals accepted except D-1 and D-8, which were **strengthened**;
D-13 accepted as default). Kickoff: record only — implementation starts in a follow-up session.

| # | Decision | Settled |
|---|---|---|
| **D-1** | Weight submission from Go | **Go-native CRv4 from day one** (override — no CR-off interim, no sidecar): `drand/tlock` ciphertexts + SDK-v11 reveal-round schedule + commit extrinsics via go-substrate-rpc-client, sr25519-signed (go-schnorrkel) by the validator hotkey. SP-2 is the conformance harness against `bittensor-drand` reference vectors; the Python sidecar is recorded only as contingency if SP-2 hits a wall. |
| **D-2** | Provider claim wallet | **ss58 coldkey** (bytes32 pubkey) in the Merkle leaf; claims stay permissionless → relayer-compatible. |
| **D-3** | Deposit execution | **Automated in st_controller** with per-epoch caps in `st.yml`, separate deposit vs ops keys, alerting, `bringyourctl st deposit` manual fallback. |
| **D-4** | `poolTotal_n` measurement | **Stake-delta snapshots** on per-pool hotkeys at epoch boundaries; deposits + fee pool isolated on `treasuryHotkey`; SP-1 verifies `getStake`/custody semantics. |
| **D-5** | Shared code home + workspace layout | **Revised 2026-07-01: no symlink — real-location relative replaces.** From sn: `replace github.com/urnetwork/X => ../../urnetwork/X`; from server: `replace github.com/urnetwork/sn => ../../urfoundation/sn`. Consequence: the subnet binaries **relocate into sn** (`sn/validator`, `sn/snclaim` — revising workstream 1's `connect/validator` home), so connect gains no nested modules. Canonical encodings still live in `package connect` (stdlib-only). Build contexts must span both checkout roots. |
| **D-6** | Claim tooling layout | **`provider claim` verifies (stdlib-only: proof fetch + minimal `eth_call` root check + calldata print); `sn/snclaim` (own module, go-ethereum — home moved with revised D-5) signs and submits.** |
| **D-7** | `/verify` auth | **Signature-only per spec**: `WrapWithInputNoAuth`, Ed25519 protocol signatures + `client_id`-vs-`ckey_` check, poisoning, per-IP/per-vpk rate limits. JWT only on control-plane routes. |
| **D-8** | Trail eligibility scope | **Residential included from day one** (override): two index feeders — proxy allocations + observed connection source IPs — behind one bijection gate (ambiguous IP → resolves to nothing; ≠1 current IP → ineligible; TTL expiry so reassigned IPs never miscredit). |
| **D-9** | `Q_n` aggregation v1 | **Usage-weighted mean** of per-provider `q_p`, EMA-smoothed across epochs; revisit before multi-NO mainnet (resolves the `WHITEPAPER_DISCUSS.md` §3 open item for v1 → D22 there). |
| **D-10** | Validator registration binding | **EVM-registered UID + metagraph check** (permissionless): coldkey = mirror(EVM wallet) via Neuron-precompile `burnedRegister`, hotkey = real sr25519 key (signs CRv4 commits); contract checks `Metagraph.getColdkey(uid(hotkey)) == mirror(msg.sender)` + `0x402` vpk bind. Owner-gated only as SP-1/SP-2 fallback. |
| **D-11** | Epoch windows | **Governance-settable contract params, 4h commit default** (generous on testnet); T-2h alerting + retries + manual fallback; a missed commit rolls the pool total into the next epoch. |
| **D-12** | Contract admin stack | **UUPS proxy + OZ Ownable (EOA → multisig once Safe on subtensor EVM is confirmed) + pause-only guardian**; finalized-claims invariant enforced structurally + upgrade-under-fire test. |
| **D-13** | Routes namespace | **`/sn/*`** control-plane (`/sn/wallet`, `/sn/pool/claim`, `/sn/epoch`); `/verify` + `/verify/keys` per spec. |

Open research items folded into spikes: precompile ABIs/custody/units (SP-1), CRv4 reference vectors +
`burnedRegister` mirror semantics (SP-2), localnet (SP-3), `eth_getLogs` (SP-4), testnet `StartCallDelay` +
burn cost at run time (§3.5), Spec-421 status (SP-1), Ed25519 precompile gas (SP-1).

---

## 10. Risks

- **Precompile drift / unverified ABIs** — the single biggest external risk (ABIs not formally versioned,
  issue #2455; examples repo stale). Mitigation: SP-1 conformance suite pinned to a subtensor release, rerun
  before every deploy.
- **CRv4 from Go (on the critical path since settled D-1)** — mitigations: reference-vector conformance
  first (SP-2), gsrpc + go-schnorrkel + drand/tlock are established libraries, and the ciphertext format is
  pinned by `bittensor-drand`. If genuinely blocked, D-1 is revisited with the sidecar as the recorded
  contingency.
- **Units confusion (rao vs 18-dec)** — SP-1 pins; all internal amounts typed `AlphaRao uint64` to make
  mixing impossible.
- **+4h window misses** — D-11 mitigations (T-2h alert, bounded retries, `bringyourctl st commit` manual
  fallback); rollover-on-miss is the defined contract behavior; on mainnet the self-hosted node (§11.1)
  removes the shared-public-RPC failure mode from this path.
- **Trail-path privacy** — publishing proofs reveals `client_id` sequences (inherent, §9); keep the
  poisoning + rate limits from day one; salted egress index (D-8 variant) if IP-at-rest is a concern.
- **Single-NO bootstrap degeneracy** — with UR as the only NO, pool-tier Yuma weight is trivially 1.0; the
  real consensus signal starts with the head tier / multi-NO. Fine for testnet; sets mainnet sequencing.

---

## 11. Runtime topology — what has to run for the subnet to work

### 11.1 Chain access / node topology

**Principle: every chain consumer takes an ordered endpoint list with failover, from day one.** `st.yml`
carries `rpc_urls: [...]`; the validator takes repeatable `--rpc`/`--substrate` flags. Trivial to build in
Phases 3/5, and it makes the mainnet node decision a config change instead of a refactor.

| Environment | Chain access | Node we run |
|---|---|---|
| Dev/CI | SP-3 localnet (docker; fast blocks; Alice-funded) | ephemeral per test run — tooling, not infrastructure |
| Testnet (Phases 0–6) | public endpoints: `wss://test.finney.opentensor.ai:443` (substrate) + `https://test.chain.opentensor.ai` (EVM, 945) | none — generous D-11 windows absorb public-RPC flakiness |
| Mainnet | **self-hosted subtensor lite node(s)** primary; public `lite.chain.opentensor.ai` / finney entrypoint last in the failover list | 1 primary + 1 warm standby |

Why the mainnet node: the +4h `commitOperator` write is deadline-critical (shared public RPCs mean rate
limits, upgrade-window flakiness, no SLA — exactly the failure mode we can't absorb); owner-run validators
commit CRv4 weights **every tempo**; and self-hosting resolves SP-4 (`eth_getLogs` range/retention limits).
One node serves **both** RPC surfaces — the Frontier EVM layer is inside the subtensor node binary (localnet
exposes both at `:9944`; SP-3 pins the exact flags/ports). **Pruned (lite) is sufficient by design**:
current contract state is the source of truth and event sync is reconstructable from it, so no archive node;
rare deep-history queries go to the public archive endpoint. Independent validators run their **own** node —
a validator-docs requirement, not our infrastructure.

### 11.2 Services inventory

**UR platform — existing warp services, changed:**

| Service | Delta | Chain access | Notes |
|---|---|---|---|
| `cli/api` | + `/verify`, `/verify/keys`, `/sn/*` | none (serves from Redis/PG; chain facts mirrored by st sync) | stateless, horizontal; holds the `verify.yml` Ed25519 signing key |
| `cli/connect` | + observed-IP egress-index feeder (D-8) on client connect/disconnect | none | feeds `verify_egress_*` behind the bijection gate |
| `cli/taskworker` | + `verify_work` (trail reaper, stats rollup) + `st_work` (epoch close → commit ≤+4h → deposits → finalize poke → chain sync) | **read + write** via `st.yml` keys | taskworker already executes Circle payouts, so money-moving credentials here match the existing posture; key-partitioned worker pools are a later hardening |
| Postgres / Redis | new tables / key families | — | existing clusters |

**UR platform — new:**

| Service | What | Chain access |
|---|---|---|
| subtensor lite node ×2 | §11.1 (mainnet only) | is the chain access |
| owner-run validators ×N (launch) | `validator` binary instances — each with its own client identity (vpk), sr25519 hotkey, EVM key (mirror = coldkey), α stake, and local proof store; walks trails through provider egress like any validator | substrate ws (per-tempo CRv4 commits) + EVM (contract calls) |
| monitoring signals | commit-deadline T-2h, per-validator weight-commit liveness each tempo, hot-wallet balances (TAO gas / α), chain-sync lag, node health, epoch pipeline state | — (existing alerting stack, new sources) |

Owner validators run on hosts/keys separate from the platform services (independence posture; sharing the
UR node at launch is acceptable) and hand off to the community as the independent set grows (whitepaper
§9.7).

**Third parties (their infrastructure, our docs):** independent validators (`validator` binary + own lite
node + funded keys); providers (existing `provider` binary + `--wallet`; `snclaim` is an on-demand CLI, not
a service). `finalizeEpoch` is permissionless, so epoch liveness never depends on UR alone.

**One-shot / admin (not services):** `btcli` (subnet create, hyperparams, start call), `forge script`
(deploy/upgrade), `stctl` (ops/inspection), `bringyourctl st` (manual fallbacks).

**Explicitly deferred services** (the subnet functions without them): indexer/explorer (whitepaper §16.1
component 7 — the public audit surface), claim relayer (batch `claimMiner` submission), dispute watcher
(v1: manual via `stctl`).

### 11.3 Cadence map

```
per request      /verify SEED/EXTEND (api) · egress-index lookups
continuous       validator trail engines (bounded concurrency) · connect feeder
per tempo ~72m   each validator: stats → weight vector → CRv4 commit (substrate) → reveal per schedule
per epoch        t0 close+snapshot → leaves+root → commitOperator ≤ +4h (taskworker)
  (7d mainnet)     → validators submitTrails ≤ +24h → dispute window → finalizeEpoch +48h (poke)
                   → claims open (api serves proofs; providers/snclaim claim at leisure)
periodic         stats rollup Redis→PG · trail reaper · st chain sync · deposit task · monitors
```

---

*Companion docs: `WHITEPAPER.md` §16 (component architecture this plan implements), `VALIDATOR.md` Appendix B
(the reuse/new-work checklist §4 follows), `WHITEPAPER_DISCUSS.md` (decision log — settled decisions
recorded there as D22).*
