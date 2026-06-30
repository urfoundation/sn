# `/verify` — The Validator's Routing-Verification Protocol

Design document for the `/verify` API route — the **validator's** cryptographic
routing-verification protocol: a competition in which **validators** walk
server-assigned chains of **providers** (`client_id`s), proving in real time that
they can egress from each provider, and producing per-provider liveness and latency
statistics.

This document specifies the three things needed to implement the route correctly:
the **signature mechanism**, the **probabilities / trail selection**, and the
**statistics**. It also fixes the wire format, the server state model, and the
security scope. **§0.5** then frames the validator's *second* job — turning these
statistics into the per-tempo `set_weights` that steers the v0.2 two-tier subnet
(detailed in **§11**).

---

## 0.5 The validator's two jobs

In v0.2 the subnet merges the old "verifier" and "validator" roles into **one** —
*"Validator (was verifier)"* (`WHITEPAPER.md` §3). That one role has **two jobs**,
and this document is mostly about the first:

1. **Measure (this document, §1–§10).** Run `/verify` trails — walk server-assigned
   chains of providers, proving real-time transit (§3–§6) — and aggregate the
   completed/failed trails into **per-provider liveness and latency statistics**
   (§7), the subnet's core signal: *which providers are the weakest links.* This is
   the cryptographic measurement protocol, kept intact below.

2. **Steer (§11).** Each **tempo** (~72 min), turn those per-provider statistics into
   a Bittensor **weight vector** and submit it under commit-reveal, so the
   validators' evaluation — not a fixed formula — drives the 41% miner emission, the
   Bittensor way (`WHITEPAPER.md` §10). The same per-provider numbers feed **both**
   miner tiers (`WHITEPAPER.md` §8.4–§8.5): they aggregate to a **pool scalar `Q_n`**
   for the per-NO **pool** tier (the tail), and are used **directly, per provider, as
   `Q_p`** for the **top-level miners** (the head — no aggregation). For this work the
   validator earns **native Yuma dividends** (∝ stake × vtrust) **plus a fee-funded
   effort bounty** (∝ verified trail volume) — both unchanged by the two-tier
   iteration (`WHITEPAPER.md` §9).

The two jobs are separated by design: §1–§10 produce an attributable measurement
under an open, adversarial validator set; §11 is the thin consuming layer that maps
that measurement onto on-chain UIDs and weights. **Nothing in §1–§10 changes because
of §11** — the steering reads the statistics, it does not perturb them.

> **Terminology & symbols.** "Validator" is the role formerly called "verifier"; the
> route is still `/verify`. The signing key a validator endorses a trail with is
> still its **path key `vpk`** (its `client_id`'s registered Ed25519 key, §2), which
> the ST contract binds to the validator's Bittensor wallet via
> `registerValidator(vpk, sig)` (`WHITEPAPER.md` §3). The proof's validator signature
> keeps its wire name **`verifier_sig`** (`WHITEPAPER.md` §9.1) — a field name, not
> the role.

---

## 1. What this proves (and what it does not)

Read this first; it bounds every design choice below.

A completed trail proves:

- **Key custody** — the holder of the validator path key `vpk` endorsed this exact path.
- **Real-time sequential transit** — the validator egressed from each assigned
  provider, in a server-chosen order revealed one hop at a time, so the path
  cannot be precomputed and the provider egresses were live during the trail.
- **Server attribution** — the server non-repudiably records which provider sat
  at each hop (the *canonical provider*).

A completed trail does **not** prove:

- **Honest relay of real user traffic.** It measures reachability to *one known
  destination* (the `/verify` endpoint) over the *cold path* with tiny packets. A
  provider can prioritize that path and degrade everything else ("teaching to the
  test"). Closing this needs **destination diversity** (§10), not signatures.
- **Non-self-dealing per-hop.** At the server, "provider X relayed the validator's
  packet" and "X originated the packet from its own egress" are the same
  observable. A party that runs both the validator and provider X can fake X's
  *own* measurement. Whole-trail forgery is bounded by §6 (needs majority of
  `client_id`s); per-hop self-dealing is **not** — it needs only X, and is
  defended only statistically, conditional on an independent validator population
  (§7.7, §10).

**Operating assumptions** (the system is only as good as these):

1. No single party controls a majority of provider `client_id`s.
2. The validator population is **independent of the providers it measures.** This is
   an **open competition** — anyone may run a validator — so this is a property we
   *approximate*, not a precondition we can assume. v1 relies on an honest validator
   *majority* providing a clean baseline against which self-dealing and adversarial
   abandonment show up statistically (§7.5, §7.7); the structural defenses that make
   it hold under adversarial validators are the §10 roadmap.
3. Source IP cannot be spoofed (TCP return path) — so a request's source IP is
   evidence the packet truly egressed from that provider.
4. Each eligible provider maps to exactly one egress IP, and that IP resolves to
   exactly that provider (§8) — enforced by eligibility, not assumed.

**v1 scope — best effort as a first step.** This document specifies a shippable
first version for an open competition. v1 includes the affordable defenses
(real-time unpredictable paths §5.1, server-stamped time §3.4, per-trail nonces,
SEED auth + rate limits + poisoning §9, per-step latency §7.5, reputation-weighted
stats and anomaly detection §7.7). v1 **defers** the structural defenses —
**proof-of-routing**, **destination diversity**, and **validator Sybil resistance**
(§10) — and is therefore suitable initially for *internal liveness/latency
monitoring and provisional scoring*, not yet for high-value payouts. §10 is the
explicit path from here to payout-grade.

---

## 2. Roles and identities

Every `/verify` request involves three identities. Keeping them distinct is the
single most important implementation point.

| Role | Identity | How the server learns it |
|------|----------|--------------------------|
| **Validator** | Ed25519 public key `vpk` — the **validator's path key** (a `client_id`'s registered key) | From the request body + signature |
| **Hop / provider** | provider `client_id` | From the **source IP** of the request → egress-IP index (§8.1) |
| **Server** | Ed25519 server key `(server_key_id, server_sk)` | Published out of band |

- The **validator** is whoever is building the trail. Reuse the existing per-client
  Ed25519 key: `model.GetClientPublicKey(ctx, validatorClientId)` already returns a
  raw 32-byte Ed25519 public key (`network_client_key_model.go:24`,
  Redis `ckey_<clientId>`). The validator signs with the matching device private
  key. (A competition-only ephemeral key also works; the protocol only needs an
  Ed25519 `vpk`.) The `vpk` is the validator's **path key**; the ST contract binds it
  to the validator's Bittensor wallet via `registerValidator(vpk, sig)` so
  completed-trail proofs are attributable to that wallet (`WHITEPAPER.md` §3).
- The **hop** is the provider the validator is *currently egressing through*. The
  server derives it from the request source IP — the validator never asserts it.
  This is the anchor of the whole proof.
- The **server** holds a dedicated Ed25519 signing key for assignments and
  finalization. Publish the public half so third parties can verify finished
  trails. Carry `server_key_id` (1 byte) in signed messages for rotation.

---

## 3. Cryptographic mechanism

### 3.1 Primitive

**Ed25519 (EdDSA), everywhere.** Rationale: deterministic nonces (no ECDSA
nonce-reuse footgun), 32-byte keys, 64-byte signatures, fast, in the Go stdlib
(`crypto/ed25519`), and already the per-client key type in this codebase. This is
a **signature** scheme — there is no encryption in this design; confidentiality is
not a goal.

> If a validator signature is ever used as a map key / dedup token, hash it
> (SHA-256) first or enforce RFC 8032 canonical `S`. Do not use a raw signature as
> a unique identifier — Ed25519 signatures are weakly malleable.

### 3.2 The four signatures

Two are produced by the validator, two by the server. Each is computed over a
**canonical binary message** (Appendix A), never over JSON. Each message begins
with the global context string `"urnetwork/verify/v1"` and a 1-byte `msg_type`,
which together provide domain separation from every other Ed25519 use in the
codebase and from each other.

| # | Name | Signed by | `msg_type` | Binds / proves |
|---|------|-----------|-----------|----------------|
| 1 | **SEED** | validator `vsk` | `0x01` | "I, holder of `vpk`, am starting a trail" (key ownership) |
| 2 | **EXTEND** | validator `vsk` | `0x02` | "`vpk` has reached this exact ordered path under this trail nonce" |
| 3 | **ASSIGN** | server `server_sk` | `0x03` | "the server assigned this next hop in this trail" (non-repudiable, unpredictable) |
| 4 | **FINAL** | server `server_sk` | `0x04` | "the server attests `vpk` completed this exact timed path" |

What each one is *for*:

- **SEED** authenticates trail creation to `vpk` from the very first hop, so a
  trail cannot be started or hijacked by anyone not holding `vsk`.
- **EXTEND** is the validator's claim, signed at every hop, over the whole path so
  far *including the hop it is currently claiming to have reached*. Because the
  EXTEND at depth `M` covers all `M` `client_id`s, **the final EXTEND alone
  endorses the entire completed path** — that is the signature published in the
  proof. EXTEND covers `client_id`s and the trail header only; **times are not
  signed by the validator** (the server is authoritative for time, §3.4).
- **ASSIGN** commits the server to each randomly-sampled next hop. Because it is
  bound to the per-trail `server_nonce` and returned only *after* the current hop
  succeeds, it is the anti-precomputation commitment: the validator cannot learn or
  grind future hops.
- **FINAL** is the published attestation. It covers all `M` confirmed hops **with
  their server-stamped times**, so the public proof carries timing.

### 3.3 The published proof

When a trail reaches depth `M`, the server publishes and returns:

```
proof = {
  header:      { trail_id, server_nonce, vpk, M },
  hops:        [ (client_id_1, time_ms_1), …, (client_id_M, time_ms_M) ],
  final_sig:   FINAL signed by server_sk,   // server_key_id included
  verifier_sig: EXTEND at depth M signed by vsk   // the validator's signature (wire field name, WHITEPAPER.md §9.1)
}
```

Anyone with the server's published Ed25519 public key can verify:

- `final_sig` ⇒ the server attests this timed path occurred, and
- `verifier_sig` ⇒ the holder of `vpk` walked exactly this path.

Together they are a non-repudiable completed trail credited to `vpk`. (Recall from
§1: this attests *transit*, not honest relay.)

### 3.4 Time

Every hop time is **server-stamped** with `server.NowUtc()` at the instant the hop
is confirmed, encoded as unix-milliseconds `uint64`. Validator-supplied time is
never trusted and never signed by the validator. This makes the latency signal
(§7) authoritative and unforgeable by the validator.

### 3.5 Server key management

- One active Ed25519 signing key, addressed by `server_key_id` (1 byte), included
  in every ASSIGN/FINAL message so old proofs verify across rotations.
- Store the private key alongside the existing JWT signing material
  (`jwt/by_jwt.go` loads keys from the `jwt.yml` vault resource — add a
  `verify.yml` peer, or an Ed25519 entry).
- Publish all historical public keys (keyed by `server_key_id`) at a stable URL so
  proofs remain verifiable.

---

## 4. Trail lifecycle

```
            routes through A (validator's chosen entry)
validator ─────────────────────────────────────────────►  POST /verify   (SEED: vpk, vsig)
                                                           src IP → hop = A
                                          server: new trail_id, server_nonce,
                                                  trail = [A@t0], sample next = B
            ◄───────────────────────────────────────────  { trail_id, server_nonce,
                                                             trail, next_hop=B, assign_sig }

            routes through B
validator ─────────────────────────────────────────────►  POST /verify   (EXTEND over [A,B], vsig)
                                                           src IP → hop = B
                                          server: verify vsig under vpk,
                                                  check src==B==pending tail,
                                                  stamp t1, record latency(B),
                                                  trail = [A@t0, B@t1], sample next = C
            ◄───────────────────────────────────────────  { …, next_hop=C, assign_sig }

            … repeat until depth == M …

                                          server: depth==M → FINAL, publish proof
            ◄───────────────────────────────────────────  { status: "complete", proof }
```

### 4.1 SEED (depth 0 → 1)

The validator picks an entry provider `A`, routes through it, and calls `/verify`
with no trail.

Request body:
```json
{ "vpk": "<base64 32B>", "client_nonce": "<base64 32B>", "seed_sig": "<base64 64B>", "M": 8 }
```

Server:
1. Resolve source IP → hop `client_id` `A` (§8.1). If unresolved → **poison path**
   (§9), but proceed indistinguishably.
2. Verify `seed_sig` is a valid SEED signature under `vpk` (Appendix A.1).
3. Check `vpk`'s `client_id` is allowed to verify and is **not** `A` (a validator
   may not seed through itself). Apply seed rate limits (§9).
4. Check `A` is an eligible provider: `model.GetProvideModes(ctx, A)` is non-empty
   **and** `A` holds an eligibility token (§5.3).
5. Create trail: `trail_id = server.NewId()`, `server_nonce` = 32 random bytes,
   `M` (clamp to `[MMin, MMax]`), `t0 = server.NowUtc()`, confirmed hops `= [A@t0]`.
   **`A` is the validator-chosen seed — exclude it from statistics (§7.6).**
6. Sample next hop `B` (§5). Append pending `B`. Produce ASSIGN.
7. Persist trail (§8.2). Return `{ trail_id, server_nonce, trail, next_hop: B, assign_sig }`.

### 4.2 EXTEND (depth k → k+1)

The validator routes through the pending hop it was given and calls `/verify` with
the trail.

Request body:
```json
{ "trail_id": "<id>", "trail": ["<A id>","<B id>", …, "<pending hop id>"],
  "extend_sig": "<base64 64B>" }
```

Server (all checks must pass; any failure → §4.4):
1. Resolve source IP → hop `client_id` `C` (§8.1).
2. Load trail by `trail_id`. Require `status == active` and not expired.
3. Verify the submitted `trail` equals the server's recorded confirmed hops **plus
   the single pending hop** — the validator may not rewrite history.
4. Verify `extend_sig` is a valid EXTEND under the trail's `vpk` over the canonical
   path of those `client_id`s (Appendix A.2).
5. **Check `C == pending_hop`** — the request truly egressed from the assigned
   provider. (This is "caller `client_id` matches the tail.")
6. Confirm the hop: stamp `t_k = server.NowUtc()`, move pending → confirmed,
   **record `latency = t_k − assigned_at` for provider `C` (§7)**.
7. If `depth == M`: produce FINAL, mark `complete`, persist & publish proof (§3.3),
   return `{ status: "complete", proof }`.
   Else: sample next hop `D` (§5), append pending `D`, produce ASSIGN, return
   `{ trail_id, server_nonce, trail, next_hop: D, assign_sig }`.

### 4.3 Idempotency / retries

A validator may resend the same EXTEND (packet loss). If the submitted trail matches
a hop already confirmed, return the **same** ASSIGN response (store the last
response per trail). Do not double-count latency or advance depth.

### 4.4 Failure / expiry

- If any EXTEND check fails, or the pending hop is not confirmed within `StepTimeout`
  `T`, the step **fails**. Record the failure against the **pending next hop**
  (the provider that was never reached) — see §7.2 — and move the trail to
  `expired`. Do not publish.
- Failures and expiries are the raw material for the statistics. They are inferred
  from *silence* (an ASSIGN that never produced a matching EXTEND), so they are
  noisy; §7 handles the attribution and confounds.

---

## 5. Trail selection & probabilities

### 5.1 Next-hop sampling

At each assignment step the server draws the next hop **uniformly at random,
without replacement within the trail**, from the eligible set:

```
eligible(trail) = { c : c has an active ProvideMode }
               ∩ { c : c has exactly one known egress IP (§8.1, §8.2) }
               ∩ { c : c holds an eligibility token (§5.3) }
               \ { hops already in trail }  \ { validator's own client_id }
```

Draw with a CSPRNG (`crypto/rand`). Let `n = |eligible(trail)|` at the draw; record
`n` with the assignment so the analysis knows the exact assignment probability
`1/n` (§7.3). Without replacement guarantees each trail exercises `M` *distinct*
providers (path diversity) and lets a failure be attributed to a single provider.

> **Anti-precomputation:** the ASSIGN for hop `k+1` is computed and returned **only
> after** hop `k` is confirmed, and is bound to `server_nonce`. The validator
> therefore cannot see, grind, or pre-position the path beyond the current hop.

### 5.2 Core probabilities

Let `N` = number of eligible providers, `M` = trail depth, and consider a provider
`X` that is not the seed.

- **Per-step assignment:** at a draw with candidate-set size `n`,
  `P(assign X) = 1/n`. With a stable eligible set, `n ≈ N − (hops used)`.
- **Appearance in a trail:** the `M−1` server-assigned positions are a uniform
  random `(M−1)`-subset of the `N−1` non-seed providers, so
  ```
  P(X ∈ trail)  ≈  (M − 1) / (N − 1)  ≈  M / N      (large N)
  ```
- **Exposure rate (sizing):** if trails start at rate `λ` (trails/sec), the total
  assignment rate is `λ(M−1)`/sec, spread over `N` providers, so each provider is
  assigned about
  ```
  ρ  ≈  λ (M − 1) / N      assignments per second.
  ```
  Use `ρ` to size the eligibility interval (§5.3) and to know how long until a
  provider accumulates the minimum sample `a_min` (§7.3): `≈ a_min / ρ` seconds.

### 5.3 Eligibility (rate / coverage control)

Each provider carries a token bucket that refills **one token every
`EligibilityInterval`** seconds (the original "a token every N seconds for each
ip"), capped at a small burst. A provider is assignable only if it has a token; on
assignment, spend one. This:

- caps each provider's measurement rate (spreads coverage over time), and
- bounds how often any one provider can be measured — which also bounds how often a
  self-dealer can harvest its *own* node's measurements (§7.7).

**Design note (deliberate change from the original sketch):** the server *samples
among eligible providers and pushes* the assignment, rather than granting the token
to "the first request after the eligible time." A first-come grant is a latency
race that biases assignment toward fast-polling validators and well-connected
providers — i.e. it biases by network proximity, not provider quality, which would
invalidate the equal-probability baseline the statistics depend on. Server-push
sampling keeps the baseline clean.

Implement the bucket in Redis: key `{velig_<clientId>}` (own hash-tag slot),
`INCR`+`EXPIRE` style as in `connect/transport_rate_limit.go:124`, or a token count
with a refill timestamp.

### 5.4 Resistance to whole-trail forgery (the "needs majority" result)

If one party owns `k` of the `N` providers (fraction `f = k/N`), the probability
that *every* randomly-assigned hop of a trail lands on owned providers is
```
P(all M−1 assigned hops owned) = C(k, M−1) / C(N−1, M−1)  ≈  f^(M−1).
```
Example: `N = 10000`, `M = 8`, `k = 1000` (10%) → `0.1^7 = 1e-7`. To complete a
meaningful fraction of trails purely through owned nodes, `f` must approach 1 —
i.e. whole-trail integrity reduces to "no party owns a majority of `client_id`s,"
an existing network assumption. **This does not bound per-hop self-dealing**, which
needs only `k = 1` (§7.7).

### 5.5 Parameters

| Symbol | Name | Default | Notes |
|--------|------|---------|-------|
| `M` | trail depth | 8 | `f^(M−1)` forgery resistance vs. completion difficulty |
| `MMin/MMax` | clamp on requested `M` | 4 / 16 | reject out-of-range |
| `T` | `StepTimeout` | 30 s | defines a step "failure" |
| `EligibilityInterval` | token refill | 60 s | size from `ρ` (§5.2) and target sample rate |
| `TrailTTL` | trail lifetime | `M·T` + 60 s | GC abandoned trails |
| `a_min` | min exposure to report a provider | 30 | §7.3 |
| seed limits | per-source-IP, per-`vpk` seed rate | tune | §9 |

---

## 6. Trail state model

### 6.1 Records (Redis, with cluster-safe hash tags)

> **Cluster rule:** keys touched in one pipeline/transaction must share a `{…}`
> hash tag (see the Redis pipeline slot rule the codebase already follows, e.g.
> `{pm_<clientId>}`). Tag every key for one trail with `{vtr_<trail_id>}` so a
> single `TxPipelined` over that trail stays in one slot.

Per trail (`{vtr_<trail_id>}`):
```
header        : vpk(32B), server_nonce(32B), M, status, created_at, last_activity
hops          : ordered [ (client_id, assigned_at, confirmed_at|null, assign_n) ]
pending       : (client_id, assigned_at, assign_n)         // the tail awaiting arrival
last_response : cached JSON for idempotent retries (§4.3)
```
Set `EXPIRE = TrailTTL`; an expired key whose status was `active` is an abandoned
trail and should be swept into the failure stats (§7) by a reaper before/at expiry.

Per provider, for statistics (`{vstat_<clientId>}`):
```
assignments, confirmations          // counters
latency samples (reservoir or t-digest for percentiles)
```

### 6.2 Published proofs (Postgres)

Persist completed proofs durably for audit and point accounting:
```sql
CREATE TABLE verify_trail (
    trail_id      uuid PRIMARY KEY,
    vpk           bytea NOT NULL,         -- validator Ed25519 path key
    server_key_id smallint NOT NULL,
    server_nonce  bytea NOT NULL,
    depth         smallint NOT NULL,      -- = M for complete
    status        smallint NOT NULL,      -- complete | expired
    hops_json     text NOT NULL,          -- [(client_id, time_ms), …]
    final_sig     bytea,                  -- server FINAL (null if expired)
    verifier_sig  bytea,                  -- validator EXTEND@M signature (null if expired)
    create_time   timestamp NOT NULL,
    complete_time timestamp
);
```
Use `server.Tx` / `server.Db` (`db.go:540` / `:376`). Points per proof are computed
from this table; the scoring function is out of scope here.

---

## 7. Statistics — finding the weakest provider

Goal: per-provider quality, comparable across providers. The naive "incomplete
trails per `client_id` / completed trails per `client_id`" is **wrong** because a
trail is a path — it confounds which hop failed, censors providers downstream of a
failure, and mixes validator abandonment with provider failure. Use the following.

### 7.1 Why rewarding *complete* trails yields the cleanest *incomplete*-trail data

The data we want is the failures; the thing we pay for is completions. That is not a
contradiction — it is the mechanism. Rewarding completion is exactly what makes a
failure *attributable to a provider* instead of noise:

- **Completion reward ⇒ maximum validator effort.** A validator paid only when a trail
  finishes will push every trail as hard as it can. So a hop that fails *despite* a
  validator straining to complete it is strong evidence the **provider** was the
  blocker — not that the validator was lazy or lost interest. The failure signal is a
  *byproduct* of honest maximal effort, and it gets cleaner as the reward grows.

- **The unpredictable reveal makes "tried hardest" structural, not just rational.**
  Each next hop is revealed only after the current one is confirmed (§5.1), so a
  validator cannot see the path ahead and cannot route around a provider it suspects
  is weak. At the assigned hop its only choices are *attempt it* or *abandon the
  whole trail* and forfeit all accumulated progress. The mechanism thus forces the
  validator to exercise the very providers we sampled, in the proportion we sampled
  them — it cannot cherry-pick the easy ones, so every provider's failures get
  observed under the same maximal-effort conditions.

- **Incentivizing failure *directly* would be the real mismatch.** If we paid for
  reported failures we would get *manufactured* ones — validators inducing or faking
  failures for reward. Completion is the only incentive that aligns the validator with
  *trying*, rather than with the outcome we are trying to measure. We never pay for a
  failure; we pay for the effort, and read the failures that effort could not
  overcome.

- **The reward sorts marginal providers onto the right axis — a feature.** A validator
  straining to finish will retry a marginal hop rather than quit, turning a
  near-failure into a *high-latency completion* instead of an incomplete. So the
  binary incomplete signal collapses toward "**truly dead / unreachable**," while the
  graded quality of slow-but-working providers moves into the **latency tail** (§7.3).
  Read both axes: latency percentiles for degradation, incompletes for outright
  failure.

This is why the rest of §7 is built the way it is. **Per-transition attribution**
(§7.2) blames a failure on the one provider that was never reached, so maximal effort
points the signal at a single hop. **Per-step latency recording** (§7.5) ensures that
even when a validator rationally restarts a slow trail, the slow measurement it already
produced is kept rather than censored. The honest residual — that rewarding completion
*purifies* but does not *eliminate* abandonment (a validator can still crash, or
adversarially abandon to grief a competitor) — is what reputation weighting and the
independence assumption (§7.7, §1) are for, and is the boundary v1 accepts (§10).

### 7.2 Attribution: per-transition, not per-trail

A trail fails at exactly one hop: it reached confirmed tail `Z`, was assigned next
hop `Y`, and never produced a valid EXTEND from `Y`. **Attribute the failure to the
pending next hop `Y`** (the provider that was never reached), never to the whole
path. Concretely, per provider `Y`:
```
a_Y = # times Y was a (server-assigned, non-seed) next hop      // exposure
c_Y = # times an EXTEND from Y confirmed within T               // success
f_Y = a_Y − c_Y                                                 // failures
```
This is a hazard/survival framing: each provider is judged only on the trails where
it was actually assigned, which automatically handles **right-censoring** (a
provider downstream of an earlier failure simply has no assignment from that trail,
so it is neither credited nor blamed).

### 7.3 Two signals

- **Liveness / step-completion rate** `r_Y = c_Y / a_Y`. Report with a **Wilson
  score** confidence interval, not a point estimate. Flag `Y` whose interval lies
  below the global rate. This captures *dead / unreachable* providers.
- **Latency distribution** (primary graded signal). For each *confirmed* step into
  `Y`, `latency = confirmed_at − assigned_at`. Report per-provider median / p95 /
  p99. Because the completion incentive (§7.1) converts marginal providers'
  near-failures into *slow successes*, the quality signal lives in the latency
  **tail** — read p95/p99, and treat `r_Y` as a separate liveness flag.

### 7.4 Exposure, baseline, and known-not-equal weighting

- **Minimum exposure:** do not report `Y` until `a_Y ≥ a_min`. Sparse providers
  have meaningless ratios.
- **Baseline:** under the null "all providers equal," `r_Y` and the latency
  distribution are equal across providers up to sampling noise. Test outliers with
  non-overlapping Wilson intervals (liveness) or a peer-percentile threshold on p95
  (latency).
- **Uniform is not required — *known* is.** Eligibility (§5.3) can make the draw
  non-uniform. Because the candidate-set size `n` is recorded at each assignment,
  the assignment probability `1/n` is known, so estimates can be inverse-probability
  (Horvitz–Thompson) weighted if needed. Equal probability only equalizes *power*
  across providers, not correctness.

### 7.5 Record latency **per step**, not per trail

Write the latency/confirmation sample at the instant a hop confirms (§4.2 step 6),
**not** at trail finalization. Otherwise a validator that abandons a slow trail to
start a fresh one (rational under per-completion rewards, §7.1) removes the slow
provider's measurement entirely — **survivorship bias** that makes slow providers
look fast. Per-step recording captures the slow hop that *triggered* the
abandonment.

> Tension to accept: per-step recording lets a self-dealer (validator = provider `Y`)
> bank a flattering measurement for `Y` without completing the trail. Mitigate by
> weighting samples by validator reputation and by §7.7's anomaly detection — do not
> rely on per-step recording alone where validators are untrusted.

### 7.6 Exclude the seed hop

Only the `M−1` **server-assigned** positions are unbiased random samples. The seed
hop (§4.1) is validator-*chosen* (they pick a working entry), so it is
selection-biased upward. Never count the seed position in `a_Y` / `c_Y` / latency.
**This rule is load-bearing twice over:** it keeps the statistics clean *and* —
because the same server-assigned-only sampling underlies both the effort bounty
**and** the per-provider head weight `Q_p` (§11) — it is what stops a top-level miner
from *farming its own measurement* (it cannot seed itself, §4.1, and the assigned
hops it appears in are drawn at random by the server, §5.1). See `WHITEPAPER.md` §9.3.

### 7.7 Known biases the numbers still carry

- **Incomplete = silence.** A failure can be the provider *or* a validator that
  crashed, was rate-limited, or rationally quit. Rewarding completion (§7.1) makes
  engaged validators try hard, which purifies this signal, but cannot remove
  abandonment entirely.
- **Adversarial abandonment.** A competitor can inflate victim `Y`'s `f_Y` by
  abandoning specifically when assigned `Y`. This needs no `client_id`s and is not
  caught by §5.4. It is bounded only by validator independence / reputation.
- **Per-hop self-dealing.** `Y`'s operator can run a validator and self-originate
  `Y`'s step (needs only `Y`). Detect statistically: with enough *independent*
  validator volume, self-dealt samples appear as a too-perfect / bimodal cluster, or
  as good numbers that only ever come from one validator identity. This detection
  exists **only to the degree the validator population is independent** (§1
  assumption 2).
- **Correlated outages.** Providers in one ASN/region fail together; model time and
  region as covariates before declaring a provider individually bad.

For anything beyond ranking (e.g. payouts), fit a per-hop logistic/hazard model
with covariates {assigned provider, position-in-trail, validator (random effect),
time/region} rather than reading marginal ratios.

---

## 8. Source-IP → provider mapping

### 8.1 The mapping (partly new work)

The hop `client_id` comes from the request **source IP**, which is the egress IP of
the provider the validator routed through. Extract the source IP exactly as
`session/client_session.go:41` does — headers in order **`X-UR-Forwarded-For`**, then
**`X-Forwarded-For`** (+`X-Forwarded-Source-Port`), then `r.RemoteAddr`.

The egress IPs exist in `proxy_client.client_ipv4` / `proxy_client_ipv4`
(`db_migrations.go`), but there is **no reverse index** `egress_ipv4 → client_id`
today — **this is new work**:

- Maintain a Redis hash `verify_egress:<ipv4>` → `client_id`, written when a
  `proxy_client` egress is allocated/released
  (`network_client_proxy_model.go:583`). Look up by encoding the source IPv4 as the
  same `bigint` used in `proxy_client`.
- Or store **salted hashes** of egress IPs (`HMAC(server_salt, ipv4)`), matching
  the original "known ip hashes from providers" so the table does not hold raw
  provider IPs at rest. Look up by hashing the source IP.

Only providers satisfying the one-provider-⇄-one-egress-IP invariant (§8.2) are
present in this index; everyone else is absent and therefore ineligible. Do the
lookup in **constant-ish time** regardless of hit/miss (§9).

### 8.2 Eligibility invariant: one provider ⇄ one egress IP

The hop lookup must be unambiguous, so eligibility enforces a **bijection** between
eligible providers and egress IPs. We do not attempt to disambiguate shared egress —
we exclude.

- **Exclude any `client_id` that currently has more than one egress IP.** A
  multi-egress provider is simply not eligible to be sampled (§5.1) until it is back
  to a single egress IP. (CGNAT-style ambiguity is explicitly a non-goal.)
- **The reverse index must be unique.** If an egress IP is ever observed backing
  more than one `client_id`, that IP resolves to *no* provider — drop it, never guess.
- Maintain the invariant where `proxy_client` egresses are allocated/released
  (`network_client_proxy_model.go:583`): on a provider gaining a second egress IP,
  remove it from the eligible index; on returning to a single egress IP, re-add it.
- **IPv6 / churn:** index both families; expire entries when a `proxy_client` is
  released so a reassigned IP is not miscredited to the previous holder.

---

## 9. Anti-deanonymization (poisoning) & DoS

`/verify` must not become an oracle that tells anyone "is IP X a provider, and which
`client_id`?"

- **Poison unknown seeds.** If a SEED's source IP does not resolve to a provider,
  still create a normal-looking trail with valid ASSIGNs and carry it **all the way
  to depth `M`**, then silently decline to publish (mark `poison`, never write
  `verify_trail`). The caller cannot distinguish a real trail from a poisoned one
  until proofs publish.
- **Indistinguishable responses & timing.** Real-vs-poison must match in payload and
  latency. Resolve the IP mapping (§8.1) in constant-ish time; issue ASSIGNs for
  poison trails identically.
- **This is delay+cost, not a wall.** A routing-capable attacker who completes a
  trail still learns the truth via their own `vpk` when proofs publish; poisoning
  only raises the cost to "complete a full trail." It also does **not** address the
  separate leak that *published* proofs reveal `client_id` sequences and timing —
  that is inherent to publishing the canonical provider.
- **DoS bound.** Carrying poison trails to depth `M` costs server state, so rate-
  limit SEEDs per source IP and per `vpk` (Redis `INCR`+`EXPIRE`,
  `transport_rate_limit.go:124`), and optionally require a small proof-of-work on
  SEED. Cap concurrent active trails per `vpk`.

---

## 10. Roadmap to payout-grade (deferred from v1)

Because this is an **open competition** (§1), the following structural defenses are
what v1 does not yet include. They are required before per-provider stats drive
high-value payouts or public rankings; until then, treat v1 output as
liveness/latency monitoring and provisional scoring.

- **Proof-of-routing (provider co-attestation).** Have each hop cryptographically
  attest it forwarded the validator's packet, chained through neighbors, to raise the
  bar on per-hop self-dealing (§7.7). Note: a hop signing its *own* attestation is
  forgeable by its operator; the value comes from *neighbor* attestation, which
  again leans on `client_id` independence.
- **Destination diversity.** Have the validator fetch an unpredictable, server-chosen
  *external* resource through each hop instead of only hitting `/verify`, so a
  provider cannot pre-optimize the single measured path ("teaching to the test",
  §1). This is the only thing that converts the measurement from
  *reachability-to-us* into *routing-quality*.
- **Validator Sybil resistance.** If validators are open and points have value, bind
  validator identity to stake / a permissioned set; otherwise per-hop self-dealing
  and adversarial abandonment (§7.7) are cheap. (In the subnet this is exactly what
  the validator's α stake + Bittensor permit and a broad **independent** validator
  set provide, `WHITEPAPER.md` §9.7 — the primary v1 lever toward this roadmap.)
- **Hierarchical stats model.** The logistic/hazard model of §7.7 for payout-grade
  attribution.

---

## 11. Feeding the subnet — steering the two miner tiers

§1–§10 produce the measurement; this section is the **validator's second job** (§0.5):
each **tempo** (~72 min), turn the per-provider statistics into a Bittensor **weight
vector** and submit it under commit-reveal, so the validators' evaluation — not a
fixed formula — drives the 41% miner emission. This is the consuming layer; it reads
§7's output and maps it onto on-chain UIDs. The canonical on-chain treatment is
`WHITEPAPER.md` §10 (setting weights), §8.4–§8.5 (the two tiers and the split θ), and
§9 (the validator's rewards). Nothing here changes §1–§10.

### 11.1 Per-provider quality, EMA-smoothed — and its dual use

For every provider with enough exposure (`a_Y ≥ a_min`, §7.4), compute the §7 quality
signals — the **Wilson-score step-completion (liveness)** interval (§7.3) and the
**latency percentiles** (median / p95 / p99, §7.3) — over the **server-assigned,
non-seed** hops only (§7.6). **EMA-smooth** each provider's signal across epochs
(template default α ≈ 0.1) so a single noisy epoch does not thrash emission or
deregistration.

These per-provider numbers have a **dual use** across the two miner tiers
(`WHITEPAPER.md` §8.4–§8.5), and that is the key point:

- **TAIL — pool tier (`Q_n`).** For a per-NO **pool**, aggregate the per-provider
  stats over the NO's *tail* providers into a single **pool scalar `Q_n`**. The pool's
  weight is `deposit_n × Q_n` (demand-coupled, the unchanged v0.1 mechanism). The
  aggregation rule itself is the open question that lives **only on the tail**
  (`WHITEPAPER.md` §8.4).
- **HEAD — top-level miners (`Q_p`).** For a **top-level miner**, the **same
  per-provider statistic is used directly, per provider, as its weight `Q_p` — no
  aggregation at all.** Per-provider `Q_p` *is* the head weight (pure quality, no
  `deposit` term). This is why the head sidesteps the pool-quality-aggregation problem
  entirely.

```
for each measured provider p (a_p ≥ a_min, server-assigned hops only):
    q_p = EMA_e( Wilson_liveness(p), latency_percentiles(p) )    # §7.3, §7.6
# TAIL: roll up to a pool scalar
Q_n  = aggregate{ q_p : p is a TAIL provider of NO n }            # WHITEPAPER §8.4 (tail only)
# HEAD: use per-provider, no roll-up
Q_p  = q_p                                                       # the top-level miner's weight, directly
```

### 11.2 Mapping a measured `client_id` to a head UID — the binding

A validator measures providers by **`client_id`** (the proof's hops, server-derived
from the unspoofable source IP, §8.1) — it never learns a provider's on-chain UID from
a trail. To steer the **head** it must resolve `client_id → UID`, and it does so by
reading the **dual-signed `client_id ⇄ hotkey` binding** (`WHITEPAPER.md` §11.4):

```
msg        = "urnetwork/bind/v1" ‖ client_id(16) ‖ hotkey_ss58(32)
sig_client = Ed25519.Sign(client_sk, msg)   // client_sk = the provider's per-client key (the ckey/vpk keying of §2) → proves client_id ownership
sig_hotkey = sr25519.Sign(hotkey_sk, msg)   // proves UID / hotkey ownership
```

The binding is published in the **commitments pallet** (free, `Pays::No`, keyed
`(netuid, hotkey)`) and anchored in the **ST contract** for disputes. Before crediting
any `Q_p` to a UID, the validator **verifies**:

1. **Both signatures.** `sig_client` under the provider's per-client Ed25519 key,
   `sig_hotkey` under the hotkey's sr25519 key. A *single* signature is not enough — it
   would let a miner claim a `client_id` it does not operate and **steal another
   provider's measured quality** (the dual signature is SN51 Celium's
   `associate_evm_key` anti-theft shape).
2. **The hotkey is a live UID** in the current metagraph snapshot — **fail closed** on
   a stale snapshot (the standard Epistula / ORO-AI `bittensor-auth` "signed proof →
   *registered* hotkey" rule). An unverifiable or stale binding contributes **zero**
   head weight; it is never guessed.

**Binding ⊥ quality.** The binding proves *ownership* only; the trail (§1–§7) proves
*quality*. They are kept strictly separate — identity says *whose* UID this is, the
measurement says *how good* it is — and composed, never merged (as every comparable
subnet does; cf. Targon keeping the hotkey out of its attestation). A contested binding
is adjudicated on-chain via the `0x402` Ed25519 precompile (`WHITEPAPER.md` §11.4); a
bad one is slashable.

**Privacy.** Publishing `client_id → hotkey` (→ egress IP via §8.1) **does**
deanonymize the provider — exactly what §9 works to prevent for the general population.
So the binding is **opt-in self-deanonymization**, only for providers claiming a public
top-level slot; claiming the public UID *is* the consent. The long tail stays
`client_id`-pseudonymous inside the pools.

### 11.3 The two-channel weight vector

Each tempo the validator builds **one** `u16` weight vector over all miner UIDs, with
the two tiers combined and divided by the **governance head share θ** (`WHITEPAPER.md`
§8.5 — SN13-style weight reservation, started ≈ 0.3 and ramped):

```
# HEAD — top-level miners, pure measured quality (no deposit)
for each top-level-miner UID u (with bound, verified client_id c, §11.2):
    head[u] = Q_p(c)                       # §11.1; = 0 for the validator's own UID (self-mask)
normalize head so Σ head = θ

# TAIL — NO pools, unchanged deposit × quality
for each NO-pool UID n:
    pool[n] = deposit_n × Q_n              # §11.1; = 0 if this validator operates NO n (self-mask)
normalize pool so Σ pool = 1 − θ

w = head ⊕ pool                            # ONE vector over all miner UIDs
apply max_weight_limit                     # MUST set — chain default is NO cap; else one UID dominates a tier
commit / reveal w                          # commit-reveal ON: the Q_p / θ signal is subjective, anti-copy
```

Because Yuma clips each validator to the κ-stake-weighted median, **θ and the scoring
rule are a validator-software convention a stake-majority must run in common** — a
published governance parameter, not per-validator discretion (`WHITEPAPER.md` §8.5,
§10). Both shares go to **real recipients** (top miners; contract-owned pool UIDs), so
the June-2026 `(1 − miner_burned)` penalty does not apply — never reserve a share by
burning to an owner UID.

For this work the validator earns its two reward streams **unchanged** by the two-tier
iteration (`WHITEPAPER.md` §9.2): **native Yuma dividends** (∝ stake × vtrust —
accurate, consensus-aligned scoring) **plus the fee-funded effort bounty** (∝ verified,
coverage-weighted completed trails — the engine that keeps the failure data flowing,
`WHITEPAPER.md` §9.3). Steering both tiers is one extra sub-vector, not a new reward.

### 11.4 Why a top miner cannot farm its own head weight

The head channel pays a provider **directly** on its own `Q_p`, which sharpens the
§7.7 per-hop self-dealing concern to a single UID. The same anti-farming rules that
protect the effort bounty protect `Q_p`:

- **Server-assigned hops only, seed excluded (§7.6).** A provider cannot place itself
  on a trail: it cannot seed through itself (§4.1), and the hops it appears in are
  drawn **uniformly at random by the server** after the previous hop confirms (§5.1).
  So a top-level miner cannot steer trails onto itself to manufacture a flattering
  `Q_p` — the same property that stops a validator farming the bounty through favored
  providers (`WHITEPAPER.md` §9.3).
- **Self-weight mask + independent baseline.** A validator's head/tail vector zeroes
  its own UID and its own NO (§11.3); with an independent validator majority (§1, §7.7)
  the κ-median tracks ground truth, and a self-dealt `Q_p` shows up as the §7.7
  too-perfect / single-source anomaly.
- **Provisional until §10.** As for every other use of these statistics, head rewards
  remain **provisional** until the §10 structural defenses (proof-of-routing,
  destination diversity, validator Sybil resistance) land. The `WHITEPAPER.md` §8.4
  quality-dip protections (high `immunity_period`, the `Q_p` EMA of §11.1, θ headroom)
  keep native deregistration churn from evicting a good top miner on one bad stretch.

---

## Appendix A — Canonical signed-message encoding

All signatures are over these exact byte strings (not JSON). All integers
big-endian. Fixed widths: `Id`/`client_id` = 16 B, Ed25519 pubkey = 32 B, signature
= 64 B, `server_nonce` = 32 B, `time_ms` = `uint64`. The global context
`CTX = "urnetwork/verify/v1"` is the 19 ASCII bytes, written verbatim. Variable hop
lists are prefixed by a 1-byte count (`= depth`, ≤ `MMax`).

### A.1 SEED (`msg_type = 0x01`, signed by `vsk`)
```
CTX(19) ‖ 0x01 ‖ vpk(32) ‖ client_nonce(32) ‖ M(1)
```
`seed_sig = ed25519.Sign(vsk, that)`. Server verifies with `vpk`.

### A.2 EXTEND (`msg_type = 0x02`, signed by `vsk`, at depth `k`)
```
CTX(19) ‖ 0x02 ‖ trail_id(16) ‖ server_nonce(32) ‖ vpk(32) ‖ M(1)
        ‖ k(1) ‖ client_id_1(16) ‖ … ‖ client_id_k(16)
```
where `client_id_1..k` are the confirmed hops `1..k−1` **plus** the pending hop the
validator is claiming at position `k`. **No times.** `extend_sig = ed25519.Sign(vsk,…)`.
The depth-`M` EXTEND is the validator's signature (the `verifier_sig` field) in the
published proof (§3.3).

### A.3 ASSIGN (`msg_type = 0x03`, signed by `server_sk`)
```
CTX(19) ‖ 0x03 ‖ server_key_id(1) ‖ trail_id(16) ‖ server_nonce(32) ‖ vpk(32)
        ‖ M(1) ‖ depth(1) ‖ client_id_1(16) ‖ … ‖ client_id_depth(16)
```
where the last `client_id` is the newly assigned (pending) next hop. Returned with
each non-final response so the validator (and later auditors) hold the server's
commitment to the path.

### A.4 FINAL (`msg_type = 0x04`, signed by `server_sk`, at depth `M`)
```
CTX(19) ‖ 0x04 ‖ server_key_id(1) ‖ trail_id(16) ‖ server_nonce(32) ‖ vpk(32)
        ‖ M(1) ‖ (client_id_1(16) ‖ time_ms_1(8)) ‖ … ‖ (client_id_M(16) ‖ time_ms_M(8))
```
**Includes** server-stamped times. `final_sig = ed25519.Sign(server_sk, that)`;
verified with the published server public key for `server_key_id`.

---

## Appendix B — Implementation checklist

**Reuse (exists today):**
- `server.Id`, `server.NewId()`, `server.ParseId()` — ids (`id.go`).
- `server.NowUtc()` — server-authoritative time (`util.go:53`).
- `model.GetClientPublicKey(ctx, clientId)` — validator Ed25519 path key (`network_client_key_model.go:24`).
- `model.GetProvideModes(ctx, clientId)` — provider eligibility (`network_client_model.go:920`).
- `server.Db` / `server.Tx` / `server.Redis` — storage (`db.go`, `redis.go`).
- `router.NewRoute("POST", "/verify", handlers.Verify)` + `handler(w, r)` (`api/api.go`, `auth_handlers.go:22`).
- Source-IP extraction order from `session/client_session.go:41`.
- Redis `INCR`+`EXPIRE` rate-limit pattern (`connect/transport_rate_limit.go:124`); `{…}` hash-tag rule.

**New work:**
- `egress_ipv4 → client_id` reverse index (§8.1), kept in sync with `proxy_client`.
- Server Ed25519 verify keypair + `server_key_id` rotation + published public keys (§3.5).
- Trail state in Redis under `{vtr_<trail_id>}` (§6.1); per-provider stats under `{vstat_<clientId>}`.
- Eligibility token buckets `{velig_<clientId>}` (§5.3).
- `verify_trail` Postgres table + proof persistence (§6.2).
- `/verify` handler implementing SEED / EXTEND / finalize, poisoning, idempotency,
  and the four signatures (Appendix A).
- Reaper that sweeps expired `active` trails into failure stats (§4.4, §6.1).
- Stats job: Wilson intervals, latency percentiles, per-transition attribution,
  `a_min` gating (§7).
- **Steering (validator-client, §11):** EMA per-provider quality; read & verify the
  `client_id ⇄ hotkey` binding (`WHITEPAPER.md` §11.4); build the head/tail weight
  vector split by θ; commit-reveal `set_weights`. (The trail/measurement server above
  is run by the NO; the steering loop is the validator's own Bittensor software,
  `WHITEPAPER.md` §16.)
