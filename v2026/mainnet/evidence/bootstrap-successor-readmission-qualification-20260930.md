# Successor admission after provenance verification

**Independent qualification complete for this increment.** Both heavy roots and
all three selected adjacent roots pass normal and race execution. Both causal
controls reproduce their intended assertion in normal and race execution. Source,
module and six local replacement fences are sealed. Public submission remains
unavailable; this qualification supplies no live transaction authority.

Frozen source is `cd4261a8ae67cb326dd38f786cfeba03b26886ee`, tree
`b88238a937b12e6a9cd6fdc6a59bde2774ab2149`, based on qualified `a7186754`.
The author worktree is
`/home/by/urnetwork/sn-successor-canonical-readmission-owner-20260930`.
The earlier [canonical qualification receipt](bootstrap-successor-canonical-qualification-20260930.md)
remains unchanged; its ten focused, twenty-two adjacent and fourteen/six control
counts are evidence for that earlier source, not reruns of this increment.

The change authenticates provenance at the pinned finalized snapshot before
every scoped pending observation and the final canonical/runtime/native-window
checks. A synchronous fixture barrier mutates the actual Safe nonce, relayer
nonce or later runtime during proof; each refresh must reject that changed
authority. Every new observation also invalidates earlier admission at entry.
After a canceled refresh, a previously admitted counted attempt cannot send
until another complete observation succeeds.

The signed provenance policy, public command boundary and missing production
authenticator are unchanged. Public `--submit` remains unavailable before
custody loading, networking or attempt reservation. The real local EVM fixtures
use an explicitly synthetic history capability; these results do not prove a
deployed Safe's complete history or authorize a live transaction.

## Evidence and test scope

Sol medium runs behavioral tests; Astra max implements and independently audits
the evidence. Astra's gofmt, compile-only `go test -c -p 1 -vet=off ./mainnet` and
`go vet -p 1 ./mainnet` pass, including both isolated causal patches. No author
behavioral test is run. Raw independent streams are retained at
`/home/by/urnetwork/temp/canonical-readmission-validation-cd4261a8`.

The separate author handoff is
`/tmp/successor-canonical-readmission-handoff-20260930`. Its thirteen-file
`SHA256SUMS` hashes to
`2b9bd48de9821bdcbdc273d483f5ca6753f8a2a59b650bd3b2d79821bba88d26`;
`CONTROLS.json` hashes to
`2c388db08e493dc8e2b031321a0a0605674fddd109f4c91eb5bcf518e6b517f2`.
The independent `manifest.json` hashes to
`5ee48470d365f3293de29056ed76382b749c3fc663d44f3afeeb9cd3380a6764`.
Its fifteen-file `SHA256SUMS` hashes to
`ddc01b6fb6d8feff784862514fe99da85e8f2c517061b2fe57c27f2383e6e729`;
all checks pass. Astra independently read all ten positive/control streams and
rechecked source, modules and local replacements. That read-only audit is
`/tmp/successor-canonical-readmission-final-seal-audit.json`, SHA-256
`e1d5ee88167ae127c6c2a9ce14c5b519307e65885fc05207a158a2de3dbdf263`.

| Stream | Roots | Result / package duration | SHA-256 |
| --- | --- | --- | --- |
| `adapter-normal.log` | 1/1 | PASS / 47.296s | `a649cdb96b5f13a76992ccf91ca76a008bbe12ca00f6bcc0eb76e5d578bfb6b4` |
| `adapter-race.log` | 1/1 | PASS / 308.905s | `1ab83b26f0130e6537514bfc62bfd1962a99a6a198da37138d29b210eab6c1db` |
| `command-normal.log` | 1/1 | PASS / 51.171s | `39a291bbf885f5ad8b7e488c48d50c84ab6228f71c5ddc8e329e91c6576a6f11` |
| `command-race.log` | 1/1 | PASS / 354.095s | `b3e5b7e9bcb67cf01d2e99945135791a02bb8f4bbfb593bdcf3ffb5aedc8f6c7` |
| `adjacent-normal.log` | 3/3 | PASS / 2.296s | `b1294fa6a38f1b2718a1089479bdcdee02d05812aefc336069cad0fb5925bb60` |
| `adjacent-race.log` | 3/3 | PASS / 13.027s | `5bec4abb727b5ba345f427203571770840bdf98962f648cac551ad781e39d638` |

The two heavy roots run separately:

- `TestBootstrapSuccessorCanonicalAdapterBoundaries`
- `TestBootstrapSuccessorCanonicalCommandExecutesAndRecovers`

Qualified adjacent roots are:

- `TestBootstrapSuccessorCanonicalOrphanAuthorityCannotEnableSubmission`
- `TestBootstrapSuccessorExecutionRechecksAfterReservation`
- `TestBootstrapSuccessorExecutionCancellationAndMissingClaimFailClosed`

The handoff uses `GOMAXPROCS=2 GOPROXY=off`, `-p 1 -count=1 -v`, separate
twenty-minute normal and thirty-minute heavy race package limits, and bounded
process limits. The production read/send deadlines do not change.

Both causal controls target `TestBootstrapSuccessorCanonicalAdapterBoundaries`:

| Control | Restored defect | Required named assertion | Normal / race |
| --- | --- | --- | --- |
| `provenance_after_pending` | Move proof back after pending/runtime/window admission. | `canonical admission reused pending state from before provenance Safe nonce` | Causal / causal |
| `stale_admission_after_failed_refresh` | Invalidate old admission only after a potentially failing checkpoint. | `failed canonical refresh reused earlier send admission` | Causal / causal |

Each patch ran alone and reached its intended root and assertion with process
exit one and root/package FAIL. All four executions have no build error, panic,
timeout or race report. Both author and independent positive worktrees remain
clean at the frozen source. The normal control result index hashes to
`affdf2390fb2b4290a496da266075ea9c1aa433ac3cff04e9372525b308bb005`;
the race index hashes to
`bd3dbe73b3fcdb120033bbaab39e63c49473f53d6c89d61d706e1b975d97eb9b`.

The module identity remains `github.com/urfoundation/sn`. `go.mod` hashes to
`2a948e40658bb403c440ea649140b5ffae53d8b338b0dfc61c90625c708dbc5c`,
`go.sum` to `2a8d74108e1c331b4b9ebdd8595c02f4b8a98e76f51fedf4c7498c7b41d974ca`,
and the offline module graph to
`19db0607a41328b8dc09c13292e40a0ee7024650c80ffc3e85e9a870ae225f9b`.
The manifest retains exact clean commit/tree identities for glog, goidenticons,
proxy, server, userwireguard and warp. Integration preserves every Go, test and
module byte from `cd4261a8`; only documentation differs.

## Limits

This increment addresses the order of current admission around an expensive
history operation. The genuine Safe provenance implementation is still absent;
complete storage authority and independent signer cutover remain live gates.
Additive independently signed runtime revisions preserving all original receipts,
signed bytes, nonce claims, counted attempts and liabilities are the next
separate P0. No live RPC, signing, transaction, installation, validator activation
or native 10/90 acceptance is established.
