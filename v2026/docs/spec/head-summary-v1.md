# Validator head summary v1

The backward-compatible decoder also supports [v2](head-summary-v2.md), which
adds exact cut references and a private census scoped to the recipient operator.
Version 1 retains the encoding and signature contract below.

After each native tempo's steering intent is decided, the validator uploads a
signed summary of its head result to each configured operator. The operator
shows the validator's actual ranking of claimed fleets (server `LEADERTOP.md`
§6.1, design question 9) and checks its own replica against it (§6.3).

The summary carries only the result: fleet identities, exact scores, ranks and
the selection. It carries no private inputs: no latencies, trail hops, egress
prefix hashes, client ids, quality statistics or EMA state (raw, prior or
decaying values).

Reference implementation: `github.com/urfoundation/sn/protocol/headsummary`
(encode, sign, decode and verify) on top of `protocol/headscore` (the exact head
arithmetic and order). The golden vector is `protocol/testdata/head-summary-v1.json`.
Go operators should call `headsummary.Decode` rather than re-implementing this
document.

## Envelope

The body is one JSON object. Its members appear in exactly this order:

| # | Member | Type | Value |
|---|---|---|---|
| 1 | `schema` | string | `urnetwork-validator-head-summary-v1` |
| 2 | `netuid` | u16 | the subnet, ≥ 1 (25 on mainnet) |
| 3 | `native_epoch` | u64 | the tempo index: `SubtensorModule::SubnetEpochIndex(netuid)` at `native_snapshot_block`. It is the steering intent's `subnet_epoch`; there is no separate tempo counter |
| 4 | `native_snapshot_block` | u64 | the finalized native block the decision was taken at, ≥ 1 |
| 5 | `native_snapshot_hash` | h256 | that block's hash |
| 6 | `evm_snapshot_block` | u64 | the finalized EVM block whose coordinator binding census was read, ≥ 1 |
| 7 | `evm_snapshot_hash` | h256 | that block's hash |
| 8 | `settlement_epoch` | u64 | the coordinator settlement epoch at `evm_snapshot_block` |
| 9 | `policy_id` | u64 | the signed policy's `policy_id`, ≥ 1 |
| 10 | `policy_hash` | h256 | the signed policy's hash, as pinned by the coordinator |
| 11 | `measurement_artifact_hash` | string | `sha256:` and 64 lowercase hex digits: the content address of the validator's private measurement artifact for this intent. An opaque binding; it reveals nothing |
| 12 | `validator_hotkey` | h256 | the validator's sr25519 public key, nonzero |
| 13 | `validator_uid` | u16 | the validator's UID on `netuid` |
| 14 | `maximum_head_fleets` | u16 | the policy's `maximum_head_fleets`, ≥ 1 (200 on mainnet) |
| 15 | `entries` | array | every eligible head UID, in rank order (below) |
| 16 | `selected_uids` | array of u16 | the UIDs of the selected entries, in rank order |
| 17 | `rejected_uids` | array of u16 | the UIDs of the other entries, in rank order |
| 18 | `signed_at` | string | UTC signing time, RFC 3339 with up to nine fraction digits and no trailing fraction zeros, ending in `Z` (Go `time.RFC3339Nano`) |
| 19 | `signature_scheme` | string | `sr25519` |
| 20 | `signing_hash` | string | `sha256:` and 64 lowercase hex digits (below) |
| 21 | `signature` | string | `0x` and 128 lowercase hex digits: the 64-byte sr25519 signature |

Each entry is one object with these members, in this order:

| # | Member | Type | Value |
|---|---|---|---|
| 1 | `rank` | u32 | 1-based position among the eligible entries |
| 2 | `uid` | u16 | the head UID |
| 3 | `hotkey` | h256 | the hotkey holding `uid` at `evm_snapshot_block`, nonzero |
| 4 | `score` | rational | the UID's exact head score (below), > 0 |
| 5 | `selected` | bool | `true` exactly when `rank` ≤ `maximum_head_fleets` |
| 6 | `fleets` | array | the live fleet identities that make up `score`, at least one |

Each `fleets` item is `{"fleet_id": h256, "generation": u64}`, with a nonzero
`fleet_id` and `generation` ≥ 1. Items are ordered by `fleet_id` bytes, then by
`generation`, ascending, with no duplicates. Together with the entry's `uid` and
`hotkey` each item is one validator fleet identity `(fleet_id, hotkey,
generation, uid)`. A UID normally has exactly one; it has more only while
clients of one fleet are bound under different generations.

A rational is `{"numerator": decimal, "denominator": decimal}`: the unique
reduced form, so `gcd(numerator, denominator) = 1`, with the denominator ≥ 1 and
no sign or leading zeros (the integer zero would be `"0"`, but scores here are
positive). An h256 is `0x` and 64 lowercase hex digits. Integers are plain
decimal JSON numbers, exact to 64 bits; a consumer outside Go must parse them as
64-bit integers, since a generation can exceed 2^53.

## Score, rank and selection

The scores are the validator's exact head arithmetic (`integer_rational_v1`,
implemented by `protocol/headscore`):

- **Raw score per tempo** of a fleet identity: `Σ_h 1/claim(h)` over the
  distinct egress prefix hashes `h` it showed on non-seed hops of complete
  trails, where `claim(h)` is the number of live fleet identities that showed `h`.
- **EMA** per identity: `next = α·raw + (1 − α)·prior` with the policy's
  `head_score_ema` α (1/4 on mainnet). A new identity starts at its raw score. A
  new generation is a new identity and inherits nothing. A live identity with no
  hops is multiplied by `1 − α`. An identity that is not live keeps decaying in
  the validator's private state but gets no score.
- **UID score:** the sum of the positive EMA values of the UID's live
  identities. Only UIDs with a positive score are eligible.
- **Rank:** score descending, then UID ascending. This is a total order: UIDs
  are unique.
- **Selection:** the first `min(n, maximum_head_fleets)` eligible entries.

`entries` therefore equals the intent's `eligible_head_uids` and
`eligible_head_scores`, and `selected_uids` and `rejected_uids` equal its
`selected_head_uids` and `rejected_head_uids`. An empty head is a valid summary
with three empty arrays; the validator still sends it every tempo.

## Canonical encoding and signature

The canonical encoding is the JSON above with the members in the listed order,
no insignificant whitespace, no trailing newline, and no escapes: every string
value is drawn from ASCII digits, lowercase letters, `0x`, `:`, `-`, `.` and
`T`/`Z`, so none is needed. Arrays are `[]` when empty, never `null`. This is
exactly Go's `encoding/json` output for the reference types.

The digest is

    SHA-256( "urnetwork/validator/head-summary/v1" || 0x00 || U )

where `U` is the canonical encoding of the envelope with `signing_hash` and
`signature` set to the empty string `""` (the members stay in place).

- `signing_hash` is `sha256:` and the hex digest.
- `signature` is the validator hotkey's sr25519 (schnorrkel) signature over the
  32-byte digest, in the Substrate signing context `substrate`, as produced by
  `go-subkey` and the validator's CRv4 hotkey.

## Transport

    POST /sn/head-summary
    Authorization: Bearer <the validator's API session JWT for this operator>
    Content-Type: application/json

The body is exactly the canonical envelope bytes, at most 16 MiB. The session
is the one the validator already uses for its `/sn/attempt-artifact` uploads to
that operator; the hotkey signature, not the session, is the summary's
authority.

| Status | Meaning |
|---|---|
| 204 | Stored, or an identical summary was already stored. |
| 400 | Malformed, non-canonical, inconsistent, wrongly signed, or an unknown `schema`. |
| 401 | The session is missing or invalid. |
| 403 | The signature is valid, but the operator does not accept `validator_hotkey` as a validator of `netuid`. |
| 413 | The body exceeds 16 MiB. |
| 429 | Rate limited; `Retry-After` may be set. |
| 5xx | Transient operator failure. |

## Validator behavior

- One summary per steering intent, built after the intent is durably recorded.
  A same-epoch retry after a failed intent sends another summary for the same
  `native_epoch` with a later snapshot. Its entries are normally the same: once
  an epoch's EMA is committed, the validator refuses a same-epoch EMA with
  different inputs.
- Upload is off the steering path: building, signing and sending run in the
  background, and nothing about them can delay, change or fail the intent, the
  EMA commit or the CRv4 commit.
- Each operator has its own bounded queue of 8 summaries; when it is full the
  oldest is dropped.
- Each attempt has a 15-second timeout. A summary gets at most 3 attempts per
  operator, retried only after a transport error or timeout, 408, 429 or 5xx,
  after 5 then 15 seconds (or `Retry-After`, capped at 60 seconds). Redirects
  are never followed.
- Every failure is logged as a validator diagnostic and counted:
  `head_summary_upload_failed` and `head_summary_dropped` in the `operator`
  domain with the operator id, and `head_summary_build_failed` in `steering`;
  a delivery is `head_summary_uploaded`. A failed or dropped summary is not
  retried later; the next tempo's summary supersedes it. A summary queued at a
  restart is lost the same way.
- The upload is on by default whenever the validator runs; there is no switch.

## Operator behavior

The operator MUST, in order:

1. Refuse a body over 16 MiB (413) and an invalid session (401).
2. Decode strictly: one object, no unknown or duplicate members, no trailing
   data; then require the body to equal the canonical re-encoding byte for byte.
3. Check every value against the tables above, and that `entries` is in rank
   order with ranks `1..n`, unique UIDs, `selected` exactly for `rank ≤
   maximum_head_fleets`, and `selected_uids`/`rejected_uids` equal to the
   selected and unselected entries' UIDs in rank order. At most 4,096 entries.
4. Recompute the digest, compare `signing_hash`, and verify `signature` under
   `validator_hotkey`.
5. Decide whether it accepts `validator_hotkey` for `netuid` (403 otherwise).
   Recommended: the hotkey holds `validator_uid` with a validator permit at or
   after `native_snapshot_block`, or it is on the operator's configured validator
   list (UR's validator is UID 1).

Storage is keyed by `(netuid, validator_hotkey, native_epoch)`. A summary with a
greater `native_snapshot_block` replaces the stored one; otherwise the stored
one is kept. Both cases return 204. The operator should rate-limit per hotkey
and may drop summaries older than its board retention.

The stored scores, ranks and selection may be shown on the board. UIDs,
hotkeys, fleet ids and generations are public chain data, but the board still
follows `LEADERTOP.md` §9: an anonymous network's row shows only its UIDs.

## Example

Unsigned values, shortened hashes, for shape only:

```json
{"schema":"urnetwork-validator-head-summary-v1","netuid":25,"native_epoch":51234,"native_snapshot_block":6123456,"native_snapshot_hash":"0x6d…","evm_snapshot_block":6123450,"evm_snapshot_hash":"0x2a…","settlement_epoch":7,"policy_id":1,"policy_hash":"0x9c…","measurement_artifact_hash":"sha256:41…","validator_hotkey":"0x5e…","validator_uid":1,"maximum_head_fleets":200,"entries":[{"rank":1,"uid":42,"hotkey":"0x8f…","score":{"numerator":"7","denominator":"4"},"selected":true,"fleets":[{"fleet_id":"0x11…","generation":3}]}],"selected_uids":[42],"rejected_uids":[],"signed_at":"2026-10-29T18:04:05.123456789Z","signature_scheme":"sr25519","signing_hash":"sha256:…","signature":"0x…"}
```

## Versioning

A new version uses a new `schema`, a new signing domain and its own document.
The operator answers 400 to a schema it does not know.
