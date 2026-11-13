# Client authentication source-graph correction — 2026-09-29

This candidate repairs the build graph from SN
`3fcec46e9e0e60e46c6f56040122e941bc338815` (tree
`3cb2eb2e3d3c4c79012b0edc715d585dc77593c2`). It changes module selection,
module metadata, documentation, and three source-graph regression tests. SDK,
Connect, and SN client-authentication implementation bytes are unchanged.
Compilation and static checks are complete; behavioral qualification belongs
to Sol and remains pending at this source handoff. No live actions were run.

## Preserved baseline and cause

The complete original build-failure census remains at
`/mnt/data/sn-testnet/qualification/sn-mainnet-baseline-20260929/SOL-RESULT.md`,
with its original `normal.json` and `normal.stderr`. No baseline test body ran;
the compiler stopped after its missing-SDK-symbol limit. This correction does
not reinterpret that failure as a test assertion or a successful baseline.

SN imported the durable registration and typed refresh APIs, while `go.mod`
unconditionally selected clean sibling SDK
`42241118cd969b954228e825a215957bd34151bd`. The reviewed SDK
`516521fb16da46c9f4bff0b58221e1941694f616` is its descendant through three
registration/refresh commits and defines the missing APIs. Its module files
are unchanged from the older SDK.

Pinning SDK alone would leave an adjacent build failure: reviewed SDK uses
`connect.WithHttpRedirectsDisabled`, absent from clean shared Connect
`358cefaef9b058cdd06ba5e9c4feeadef64ae1fb`. Reviewed Connect
`b163f9dd9ac374942fe97331f26631248a9c1f81` supplies that request-scoped redirect
control and the required request-exhaustion semantics. Its SCTP subtree is
unchanged between those two commits, but retaining the old mutable SCTP
replacement would allow that fork to diverge independently later.

## Durable module selection

| Module | Effective versioned source |
|---|---|
| `github.com/urnetwork/sdk` | `github.com/urnetwork/sdk v0.0.0-20260928100458-516521fb16da` |
| `github.com/urnetwork/connect` | `github.com/urnetwork/connect v0.0.0-20260928101830-b163f9dd9ac3` |
| `github.com/pion/sctp` | `github.com/urnetwork/connect/sctp v0.0.0-20260928101830-b163f9dd9ac3` |

All three selections use versioned replacements in SN's main module, with
downloaded archive and module checksums recorded in `go.sum`. Merely changing
the requirements to pseudo-versions is insufficient: server, proxy, and SDK
still require `v0.0.0` placeholders, which outrank `v0.0.0-...` prereleases.
Dependency modules' own replacement directives are not inherited. No remote
branch name or sibling checkout HEAD participates in these three selections.

Static `go mod tidy -diff` also exposed existing metadata drift. Repeating it
with the original SN module files and the reviewed physical siblings produced
the same drift: the escrow fixture imports `holiman/uint256` directly, and six
existing dependency-test modules needed checksums. The correction records the
direct classification and those checksums without changing their versions.

The remaining local replacements are deliberately retained. Full release
inventory separately requires physical sibling repositories; these pins do
not authorize a release, change a release lock, or qualify a different physical
graph. Use `GOWORK=off` and the checked-in module files for this qualification.
Do not silently override these pins with a qualification modfile.

## Physical source graph and static evidence

Isolated branch: `codex/mainnet-clientauth-source-graph-20260929`.
Physical graph:
`/mnt/data/sn-testnet/worktrees/sn-mainnet-clientauth-source-graph-20260929/source`.
SN is the `sn` checkout. SDK and Connect checkouts are retained for source
review; their actual Go build inputs are the checksum-bound module archives.

| Physical sibling | Exact commit |
|---|---|
| SDK | `516521fb16da46c9f4bff0b58221e1941694f616` |
| Connect, including SCTP | `b163f9dd9ac374942fe97331f26631248a9c1f81` |
| server | `b7c8c74336c98de89e17687861820fb2bfdaf831` |
| proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` |
| userwireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` |
| warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` |
| glog | `892ade4a6be396b32ea82a550f243190b5992180` |
| goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` |

Evidence is in the parent of `source`: downloaded module identities and sums,
the full effective module graph, the original require-only selection probe,
original-module and candidate tidy diffs, and compile-only logs for
`./mainnet`, `./clientauth`, and `./scripts/source-graph`. Final tidy diff and
`git diff --check` are clean. Shared repository working trees and MG06 source
were not edited. The exact frozen SN commit and source/module fences are in
the external `HANDOFF.md` and `SOURCE-FENCE.txt` beside those logs.

## Sol qualification and causal controls

There are exactly three new top-level roots in `./scripts/source-graph`:

- `TestClientauthSourceGraphPinsSdkWithoutSiblingOverride`
- `TestClientauthSourceGraphPinsConnectWithoutSiblingOverride`
- `TestClientauthSourceGraphPinsReviewedSctpFork`

They invoke Go's effective module resolver with `GOWORK=off`, readonly module
files, and network lookup disabled. They import only the standard library,
so restoring the bad SDK override does not prevent the regression package
from compiling. Cache the three pinned modules first with
`GOWORK=off go mod download github.com/urnetwork/sdk github.com/urnetwork/connect github.com/pion/sctp`.
Keep the same module cache for the normal and race invocations.

Run all three roots normally and with `-race`; run the unchanged 16 top-level
`./clientauth` roots in both modes as the adjacent registration/refresh seam.
The baseline `./mainnet` package contains 618 source-level test roots and now
compiles. Sol owns its subsequent normal/race execution and complete failure
census; compilation does not claim any of those behavioral results.

For each causal control, use a separate clean checkout of this candidate,
change only its `go.mod`, and run only the listed root. Restore and fence the
source after each control. The physical siblings must remain available.

| Control | Isolated mutation | Root / intended failure |
|---|---|---|
| G01 | `go mod edit -replace=github.com/urnetwork/sdk=../sdk` | SDK root: `clientauth SDK resolved outside the reviewed registration source` |
| G02 | `go mod edit -replace=github.com/urnetwork/connect=../connect` | Connect root: `clientauth Connect resolved outside the reviewed transport source` |
| G03 | `go mod edit -replace=github.com/pion/sctp=../connect/sctp` | SCTP root: `clientauth SCTP resolved outside the reviewed Connect fork` |

These controls must fail the named identity assertion even when the physical
sibling happens to contain compatible code: a mutable override has lost the
durable pin. A dependency download, compiler error, or package timeout is not
a passing causal result. Restoring the checked-in `go.mod` is the positive
control. The original clean but incompatible SDK failure remains preserved
in the baseline receipt rather than being replaced by these new controls.
