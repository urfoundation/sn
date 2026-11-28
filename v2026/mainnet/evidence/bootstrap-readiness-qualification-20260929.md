# Bootstrap readiness qualification

`bootstrap-chain readiness` connects the original accepted v3 preparation to
current finalized role prerequisites without creating signing or service
authority. It reloads the original approvals, validates all five retained
journals under shared read-only locks, and observes both UR generations,
activity, permits and signed block windows plus the separate root seat,
mortal window and approved checkpoint. Incomplete custody, RPC failure,
runtime disagreement and a changed finalized mapping remain unresolved;
partial role observations cannot become success.

Astra max implemented frozen SN commit
`6627d15f8946319d256eb560ab626dab3fe6e66c`, tree
`3b4f7b6d9f9df40d1723295c690901fac01ed82c`, from root
`91b1b25629ec70e2e3454d5b64e4399c76eccdcf`. Sol medium qualified that exact
candidate without source edits. The mainnet root fast-forwarded to the same
commit and tree; the subsequent integration update changes documentation only.
No live RPC, native signing, deployment, validator activation or Safe evidence
anchor change occurred.

| Qualified scope | Result |
| --- | --- |
| New `^TestBootstrapChainReadiness` roots | 10/10 normal and 10/10 race; no child tests, failures or skips |
| Adjacent `^Test(BootstrapChain\|BootstrapRoot\|RootOfflineCustody\|RootService\|Subnet\|OwnerTrim)` | 148/148 normal roots with 79 passing child events; exact 148/148 race-root union, with no failures, skips or race reports |
| `go vet ./mainnet` | Passed |
| Four causal controls | Each mutation failed its named behavioral assertion, without a setup or compile failure |
| Source/module/format fences | All nine physical source heads, trees and clean statuses matched; module bytes, gofmt and diff checks passed |

The 148-root adjacent scope includes the ten new roots; these are not 158
distinct tests. At integration, race coverage came from passing root events
in the broad selector and seven completed shards. The redundant whole-selector
process was still running at integration and was later intentionally terminated
after its passing prefix. Its overall exit is not counted as a pass. The exact
148-root disjoint race union remains the qualified coverage; the immutable
integration receipt below preserves the state known at that earlier boundary.

The controls remove the UR birth-block comparison, force a root checkpoint
match, omit the final canonical-hash refusal and ignore marker-content
mismatch. Their expected failures identify stale `majority-birth`, a conflicting
approved checkpoint, the `after-checkpoint` reorg and `incomplete-marker`.
Control changes were confined to a separate worktree and restored afterward.
The positive command test retains original public signatures across repeated
invocations; missing-state coverage checks all five journals and markers.

Qualification used Go `1.26.6 linux/amd64` and an external absolute-replacement
modfile with physical Connect `b163f9dd9ac374942fe97331f26631248a9c1f81`,
SDK `516521fb16da46c9f4bff0b58221e1941694f616` and server
`fbe0c039949618ca9b9c66a7ebd7b160a37e25ce`. Tracked module files were unchanged.
The initial compile against older workspace SDK/Connect failed because those
revisions lack existing SN registration/session APIs; a later output-path
permission failure was also retained in the Astra handoff. Neither setup
failure is counted as qualification.

| Retained artifact | SHA-256 |
| --- | --- |
| [Sol integration receipt snapshot](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/SOL-RESULT.integration-6627d15f.md) | `ffb42f9dc0daac61b89181969f69e662860468879b1f57291e4c5dba5762b1a6` |
| [Exact race census snapshot](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/race-union-census.integration-6627d15f.json) | `e38b53c79da5f42744186c000884dc12b02f1bd364997f13bef42d04b832c094` |
| [Before module graph](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/modules.before.json) and [after module graph](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/modules.after.json) | `b4f06ea9677e5f006461aa9f0a53fc47eca6f47be3aa702b30cab5dd0659b246` |
| [Pinned modfile](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/astra/compile.mod) | `26a542ba0f985908e7a8e28f3fa0c2dc4931ff4c93178e7b1e843c646744f591` |
| [Physical source fence](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/astra/source-fence.json) | `5a4b0fccbd27330c000bf7374a5bd5651a86102fbfa7fe2b364296ac4fb5960a` |
| [Original Astra handoff](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/astra/HANDOFF.md) | `33d4f843f6b75b73c8e1245245355d90bb2f66d2e02bbdeb033e86ef18e58c7a` |

The [Sol working receipt](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/SOL-RESULT.md)
records the intentionally terminated redundant run; the snapshots above remain
the exact integration basis. The later
[composed check](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/COMPOSED-RESULT.md)
(SHA-256 `a68f0b1f755504128733be1954115a9e68e16c7cd4f92e6317ba6868440bda0f`)
qualifies SN `e35771ec` with server `b7c8c743`: all ten focused roots passed
normal/race, vet and exact source/module fences passed. This is composed source
qualification and supplies no live authority or acceptance.
The [command documentation](../BOOTSTRAP-CHAIN.md)
defines the observed prerequisites and the separate activation blockers.

MG-08 remains blocked for live activation. Mainnet genesis and route identity,
effective stake and role eligibility, global custody/device fencing, current
authority, production admission and healthy operators, executable trim,
complete contracts and root service activation remain open. All readiness
results retain `current_authority_verified: false`, `native_signing: false`,
`network_effects: false`, `activation_ready: false` and all five pending chain
phases. No actual 10% provider/90% recycle outcome is established by these tests.
