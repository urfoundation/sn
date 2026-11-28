# MG-08 full-v3 successor command fixture: scoped qualification

Shared SN fast-forwarded the qualified test-only candidate
`c294fefdee5b1c49b5afc1e5065ed20d4a205e5e`, tree
`cfeb56d9cf88e0fe18db7d1b6235b3cdf65360b0`, from `71105825`.
The change adds one public-command fixture and its finite local send budget;
production command, custody and timeout semantics are unchanged. The isolated
Safe release verifier is excluded from this integration.

`TestBootstrapContractSuccessorCommandAdoptsCompleteV3Custody` passed normally
and with race detection, each with exact root PASS, package PASS and exit zero
(38.28 and 298.10 seconds wall time). Three isolated production mutations each
compiled and reached their intended assertion in both modes: six causal
executions, with the expected root/package FAIL and exit one. The controls omit
the public preparation projection, omit its output seal, or ignore output failure.

The fixture prepares all five original v3 child journals, executes the eight
contract actions through their public commands against a local EVM with full
native metadata, and resumes the original parent. It then invokes the public
unsigned successor command twice and exercises output failure. It checks the
complete preparation projection, retained receipt seals, additive caps, original
journal bytes, released locks and absence of additional sends or custody files.
This closes the separately successful full-v3 command-fixture gap recorded in the
[original scoped successor receipt](bootstrap-contract-successor-qualification-20260929.md).
Safe authority, signed successor adoption/execution, live installation and role
activation remain unresolved.

| Retained receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg08-successor-full-v3-budget-sol-20260929/SOL-RESULT.md) | `7123718c5b341148e356ff15b5f1437f02cec87d4842d013f0ca6c823f7c9619` |
| [Structured result](/mnt/data/sn-testnet/qualification/mg08-successor-full-v3-budget-sol-20260929/BUDGET-qualification.json) | `15e07720109eb07d4d8559f620b208507e2b79899af00aadc219db3dd6c50544` |
| [51-file manifest](/mnt/data/sn-testnet/qualification/mg08-successor-full-v3-budget-sol-20260929/SHA256SUMS) | `fd175f047804b75ca5bccde47e0c5b21bb9f1b6a4df8ee60397733b9f5b1e932` |

Astra independently verified every manifest entry, named root and package
terminal, intended control assertion and process exit. All eleven actual source
paths, Git roots, heads, trees and clean states matched the before/after fence.
The graph pins Connect `b163f9dd`, SDK `516521fb` and isolated server `cfcbfcba`;
it does not substitute the shared server `5ff7bf02`. Resolved module files match
byte-for-byte with SHA-256
`5d464a43c12fb3095c0a205b879fcb97665e838404885e4b6c13f89c6186b68b`,
and `go mod verify` passed. No live RPC, signing, broadcast or behavioral test
execution occurred during the integration review.

## Preserved first attempt and fixture scheduling lesson

The earlier candidate `d7c47eb9`, tree `3209110e`, passed its positive root in
both modes and two nearest adjacent roots normally. Five of its six causal
executions reached the intended assertions. The remaining race control stopped
earlier during genuine local evidence creation: its synthetic HTTP POST exceeded
the inherited one-second send budget. The original uncertain-send guard retained
the liability; that attempt is noncausal and establishes no product assertion
failure. Its raw evidence remains immutable and separate from the c294 receipt.

| Preserved first-attempt receipt | SHA-256 |
| --- | --- |
| [Partial d7 result](/mnt/data/sn-testnet/qualification/mg08-successor-full-v3-sol-20260929/D7-RESULT.md) | `cca8189e3917dc75795ddc7f932e0a501c0369603605a7a29d1fd91cce3403e7` |
| [d7 manifest](/mnt/data/sn-testnet/qualification/mg08-successor-full-v3-sol-20260929/D7-SHA256SUMS) | `4a0ccf8aefdf2219d96f70bda26e4abcd4cd09226cdcdb764c0ac795e16f4499` |

The successful full-graph fixture now approves an existing finite 60-second send
maximum before claiming custody. Its full metadata causes synchronous local
inclusion work that the compact EVM fixtures avoid. The adjacent fixture audit
keeps targeted lost-reply cases and production deadlines unchanged. Preserve
every causal case and distinguish an intended assertion from an earlier local
timeout. The separate [SDK scheduling review](bootstrap-contract-successor-qualification-20260929.md#separate-sdk-package-coverage-and-scheduling-lesson)
likewise retains all fourteen serial predecessor fixtures, their observed
approximately 28-minute root duration, and a separate package/time budget.

## Separate composed smoke and partial adjacent coverage

The composed successor smoke on `1e2b2abb285f4f195e6468e5c0fe4956f95dd9ea`,
tree `2ce1e9ee0c120ac63e5bfed946918bb1136b5700`, passed twelve selected roots
normally and with race detection, both packages PASS/exit zero: six successor,
three prerequisite and three MG-07 roots. It predates the new full-v3 fixture and
has its own physical graph. Its nineteen manifest entries were independently
verified.

| Composed receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg08-successor-composed-sol-20260929/SOL-RESULT.md) | `b22fa24f06e135eb5920e2b5fdf7f63953140476ee87b9b6fa99830c15d4e96e` |
| [19-file manifest](/mnt/data/sn-testnet/qualification/mg08-successor-composed-sol-20260929/SHA256SUMS) | `895b0e8e2e22e3e8038bd9ca63d009b37e46c58578fd2fd32fe198ec6bcda176` |

The broad prerequisite battery is **partial** on original source `baa35de8`,
tree `f4cb21a2`: shards 01–05 passed 150 distinct roots normally and with race
detection, with all ten packages PASS/exit zero and no root failures or skips.
The other 120 of 270 declared roots were unrun when the coordinator paused.
These counts were checked against the raw top-level root terminals; subtests
are not additional roots. This partial receipt does not expand the c294 scope.

| Partial adjacent receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg08-contract-prerequisites-sol-20260929/ADJACENT-PARTIAL-RESULT.md) | `055201a4eeca6b47f1091bdc19a6074dd4b41d033581675138cea1a135f5aac6` |
| [52-file manifest](/mnt/data/sn-testnet/qualification/mg08-contract-prerequisites-sol-20260929/ADJACENT-PARTIAL-SHA256SUMS) | `4520902f4ed5ef2c05c1be413b30d10fa4cdcec0c502df3754060d2dedb3a4b0` |

The separate corrected SDK snapshot remains **543/618** roots backed by race
package PASS, with **75 pending** and no root failures. Neither the c294 fixture
nor either composed smoke closes that SDK package gap, the remaining broad
prerequisite battery, MG-08 activation, or actual native 10/90 outcomes.
