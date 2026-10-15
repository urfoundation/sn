# MG-08 unsigned successor proposal: scoped qualification

Sol qualified exact SN `b74dcddb309db88a9e3686d1cfe678791696d262`, tree
`e2766b6a897a09f8f7bd6b15ff21afd7d94c7a77`. Merge
`1e2b2abb285f4f195e6468e5c0fe4956f95dd9ea`, tree
`2ce1e9ee0c120ac63e5bfed946918bb1136b5700`, integrates its unchanged Go
source onto the qualified shared lineage. Only `BOOTSTRAP-CHAIN.md` needed
conflict resolution; the prerequisite receipt and successor description were
both retained. No unqualified Safe verifier or execution owner is included.

All six new successor roots and twelve inherited prerequisite roots passed
normally and with race detection. Each of the four selected package runs had
exact root coverage, zero failures/skips, package PASS and process exit zero.
Six causal patches each failed the intended named assertion in both modes:
twelve expected root/package failures with nonzero exits. The unchanged
candidate, eleven physical source entries and resolved modules stayed identical.
Resolved modules SHA-256 is
`cdf0a7368498ff67df444ba47ae4af97e8705c1882967fad30a525e616b8010b`.
This graph uses isolated Connect `b163f9dd`, SDK `516521fb` and server
`cfcbfcba`, as recorded in the retained physical graph.

The public command now produces an unsigned additive-cap proposal from the
original completed eight-action custody, preserving sealed receipts, attempts,
conservative lifetime exposure and any original ninth reservation. One unfinished
anchor plus approved retry margin determines additional capacity; no eight-action
replay is requested. Safe authority and a durable signed-successor adoption/
execution owner remain absent. The output grants no financial or signing authority.

This original receipt independently covered the completed projection and
preparation reader without a separate successful full-v3 public-command fixture.
That specific gap is now closed by the separately qualified [c294 fixture](bootstrap-contract-successor-full-v3-qualification-20260929.md),
whose exact source, full-graph execution, causal controls and preserved first
attempt are recorded there. The 276-root impact census is not a package-pass
claim. The separate prerequisite battery is partial at 150/270 roots in both
modes, with 120 unrun; it does not expand this original receipt's scope.

| Retained receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg08-contract-successor-sol-20260929/SOL-RESULT.md) | `3bdc26892002d45b347185a82d6fd88f2bea1e2919b0bf5a956e069807879b6b` |
| [Structured result](/mnt/data/sn-testnet/qualification/mg08-contract-successor-sol-20260929/SUCCESSOR-qualification.json) | `f176161428206598d939438b865f59b4cf6dc9c0ebae289192b1bddcc824a608` |
| [88-file manifest](/mnt/data/sn-testnet/qualification/mg08-contract-successor-sol-20260929/SHA256SUMS) | `2a1832ddacc6e5e7bda848692666307f46cabe86d305123271ce1886070c0e74` |

Astra independently verified all manifest entries, exact candidate/tree,
positive root terminals, intended causal failures and unchanged integrated Go
source. The merged `./mainnet` test binary compiled without execution using the
same pinned external module graph (`go test -c`, offline module resolution).
No live RPC, signing, broadcast or behavioral test execution occurred
during this integration review.

## Earlier MG-07 plus prerequisite composition

The separate Sol smoke on exact merge
`79ff2c6e3d75c6d05d702748c2f80b3e050555d0`, tree
`b4bae252859276f58665a6da416d7a3cafb5d80a`, passed fifteen roots normally
and with race detection, with package PASS and exit zero: twelve prerequisite
roots and three MG-07 read-incident roots. It uses integrated server `5ff7bf02`.
Its exact graph is distinct from the successor graph above, and it predates the
successor source. This closes that focused composed smoke only.

| Retained receipt | SHA-256 |
| --- | --- |
| [Composed Sol result](/mnt/data/sn-testnet/qualification/mg08-mg07-composed-sol-79ff2c6e-20260929/SOL-RESULT.md) | `b2557a6fc61b76114c88cb6963fc9bd2e33e8a96ffdcf6f80efbcb1220b9de1e` |
| [18-file manifest](/mnt/data/sn-testnet/qualification/mg08-mg07-composed-sol-79ff2c6e-20260929/SHA256SUMS) | `b7c790db2f7306e7ab0a189585120899dc03584b75a2724976764a2a812a65b4` |

Both manifest and clean exact source were independently verified.

## Separate SDK package coverage and scheduling lesson

The corrected [SDK coverage snapshot](/mnt/data/sn-testnet/qualification/mg07-read-incidents-sol-20260929/sdk-package-gap/sdk-package-coverage-543.json),
SHA-256 `50b4a5a2fb10153cea23076700dfb6821e2e33a2ae0c657cf4b67f4bd3940051`,
records **543/618** roots backed by package-PASS race streams and **75 pending**,
with no root failures. It applies to SDK source
`86ebb8c12e66b085adb3577815e36c993c8870dd`, not the successor candidate.
The earlier 410/618 snapshot and original timeouts remain historical evidence;
618 root-body passes alone still do not close package qualification.

`TestEvmEvidenceCreateFencesSevenPredecessorCheckpoints` runs fourteen serial
fixtures: two refresh modes times seven predecessor checkpoints. It passed in
1658.89 seconds in shard 01 and 1662.23 seconds in its exact-root retry, about
28 minutes each. Shard 01 passed all 25 roots in 3298.139 seconds. Its adjacent
`TestEvmReserveLinkKeepsCumulativeGraphAttempts` root also passed. No product
assertion failure or custody deadlock was established. `-parallel=2` does not
parallelize the serial fixture loop. Give this long root its own package/time
budget, partition other roots by measured duration, and preserve every causal
case, original timeout, source fence and package exit.

The read-only [scheduling review](/mnt/data/sn-testnet/qualification/sdk-package-time-budget-review-20260929/REVIEW.md)
has SHA-256 `00c7057377727d7029f78b5d013b53ebe5781f62caacde1e2823aac616ba1456`;
its [three-file manifest](/mnt/data/sn-testnet/qualification/sdk-package-time-budget-review-20260929/SHA256SUMS)
has SHA-256 `7dd48a31ccf978939185355a2e51b1c1c509fd11022160a15e5f14658ccf2866`.
No live test process was changed for the review.
