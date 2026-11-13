# Signed local successor preparation qualification

Qualified source is `fa4f95adb46f6f3921ce0ae32cb5a05ef71a2747`, tree
`f634e8ad028a1291ad0b1f08d0f3d05dd10a2cb4`, from
`/home/by/urnetwork/temp/mg08-signed-successor-preparation-20260929/sn`.
Its base is qualified `14cd8e1397b8e5bea0ce1d7d021f209b9749d96e`.
The exact candidate merges with shared `a9a30569` at
`93a0a060151ed2b7ccb02086bc7c249315a20ad9`, tree
`cec0389f9e7719cc92d1b121737262b3a89f46e3`. Every candidate Go file is unchanged;
the shared pure Safe files also retain their exact qualified bytes. Only
MAINNET/PRELAUNCH documentation auto-merges. The following documentation child
changes no Go source or module resolution.

Sol independently ran all thirteen new roots and three adjacent roots in normal
and race modes. Each of the four streams has package PASS, exit 0, exact root
coverage and no failed, skipped or missing roots. Sixteen independent causal
patches each compile and fail their exact named assertion in both modes, with
the selected root/package FAIL and exit 1: thirty-two intended executions.
No unexpected failure occurred. This is selected-scope qualification, not a
full-package result. The subsequent merged composition has its own scoped
qualification below.

Evidence is retained at
`/mnt/data/sn-testnet/qualification/mg08-signed-successor-preparation-sol-20260929`:

| Artifact | SHA-256 |
| --- | --- |
| `SOL-RESULT.md` | `1598b4796e70e97361972577c7e6f0df72e013c8fba0559bba4c91d67b20fbda` |
| `SOL-RESULT.json` | `d65ab3260a60edb355ef22faa26ca1e7f50cf3cf43223fc9293b6c18000edac0` |
| `SHA256SUMS` (149 files) | `2d9db3a33f059d3ea9576436e961b9a22e5c324a48e6a9d9ca6a92757ec94feb` |
| Author `HANDOFF.md` | `1b0be73e3e14587d48bfd3cdd8bee847aee6d174dac9190a6cb262588de01006` |
| Author `SHA256SUMS` (34 files) | `b7e7024d8f51ace67690616a12f1eb0e0c2127587d674aab1d7edf3fb7bc2e5b` |
| Author source fence | `ccd5a3f37566512dc759a0d45178b9f071c9d4637498759230752c1aa5626a6d` |
| Sol after source fence | `0955acd57ee2c148fd515382e51e61c2cec22f032e6f00fa46101a97fcab5edd` |
| Exact module bytes | `ad5b50557f4f0eb1c6644f2cbea0c974d5800b68cd67b859db2e1f3834ab5b6c` |
| Author causal controls | `58bbeec26a625bca40c35f133e1e4f9fb7da6b6e45c62826fad3818865bcd78c` |

Before integration, Astra independently verified both manifests, all raw JSON
root/package terminals, process exits and exact named control assertions against
the sealed author lists. Each control retains its original baseline, recorded
patch/diff/module bytes and mode. All eleven actual physical paths, Git roots,
heads and trees remain clean, including ten resolved local modules and the
separately fenced unselected Warp path. Before/after module bytes exactly match
the author graph. That graph pins Connect `b163f9dd`, SDK `516521fb` and server
`cfcbfcba`; the separately qualified shared server finality-capture source is
not silently substituted. Author compile-only and vet checks exited 0; all
behavioral execution belongs to the independent tester.

The commands `contract-successor-preview`, `contract-successor-prepare` and
`contract-successor-resume` reconstruct the proposal from actual original v3
custody and all eight complete action journals. An independently pinned Ed25519
key approves the distinct local preparation domain. The fixed claim and
immutable record use original directory ownership, exact device/inode binding,
bounded input/census checks and staged fsync/no-replace publication. Original
plans, journals and markers retain their exact bytes.

The eight completed actions keep their existing seals, signatures, receipt
records and counted attempts. They are neither signed nor replayed again.
Remaining attempt proposals subtract retained attempts from the additive
cumulative ceiling; lifetime exposure preserves completed maximum-envelope
reservations and any unfinished ninth reservation. For example, eight retained
attempts plus two proposed attempts leave one anchor attempt and one retry.
The immutable preparation record supplies no executable send allowance.

The new public-command root uses genuine local execution of all eight actions
and the original five preparation records. The existing full-v3 successor
command is an explicit adjacent root because its completion loop became a shared
test helper. Its eighth-action mutation proves the helper cannot silently skip
the last local action. The finite sixty-second success-fixture send timeout is
approved before original custody; production deadlines and targeted lost-reply
fixtures are unchanged.

Recovery coverage includes same-approval restart, interruptions before/after
stage fsync and marker publication, conflicting claimants, missing/corrupted
records, copied/replaced roots and no-replace collisions. These process hooks
do not emulate hardware power loss. A legitimate filesystem restore or volume
move to a different physical root requires independently approved migration;
none is implemented. Single ownership assumes a trusted private local filesystem
and cooperating owners. It is not global/cross-host custody or an unerasable
anti-rollback record.

Only preparation approval and local preparation completion may become true.
Execution approval, current chain verification, Safe authority, global signing
custody, executability, signing, network effects, installation and activation
remain false. Executable adoption still needs canonical historical receipt and
postcondition reauthentication, exact current Safe code and authority paths,
Safe inner and relayer outer nonce custody, approved digest/signatures and outer
envelope, funding and durable cumulative send accounting. A new Safe must match
recorded initializerOwner or have separately approved ownership migration.

The separately verified [four-root pure Safe composed smoke](safe-execution-evidence-qualification-20260929.md#separate-composed-smoke)
passes normal/race on exact `c648495f`. It predates and does not qualify this
new preparation source or merge. The preparation merge smoke is recorded below.
SDK coverage remains 543/618 package-backed race roots with 75 pending;
broader MG-08 coverage remains 150/270 normal/race roots with 120 unrun. The
earlier e074 fixture setup anomaly and d7 noncausal one-second send timeout remain
preserved in their own receipts. No live RPC, signing, transaction, installation
or activation was performed by this increment.

## Separate composed smoke

The exact merge `93a0a060151ed2b7ccb02086bc7c249315a20ad9`, tree
`cec0389f9e7719cc92d1b121737262b3a89f46e3`, also passes an independent normal/race
smoke from the detached clean worktree
`/home/by/urnetwork/temp/mg08-signed-preparation-composed-smoke-20260929/sn`.
Both streams have all six selected top-level roots PASS, package PASS, exit 0
and no failed, skipped or missing roots:

- `TestBootstrapSuccessorPreparationCommandRetainsActualV3Prefix`
- `TestBootstrapSuccessorPreparationClaimsOnceAndResumesSameRoot`
- `TestBootstrapContractSuccessorCommandAdoptsCompleteV3Custody`
- `TestSafeExecutionDigestMatchesPinnedProxyAndSingleton`
- `TestSafeExecutionOutcomesFollowPinnedContractAndNonce`
- `TestMonitorReadIncidentCommandRecoveryRecurrenceAndRestart`

Evidence is retained at
`/mnt/data/sn-testnet/qualification/mg08-signed-preparation-composed-sol-20260929`:

| Artifact | SHA-256 |
| --- | --- |
| `SOL-RESULT.md` | `073340e9683c27c4ab70f373ecbc46478a4b1e477acf2875205f4afe527c2aab` |
| `SOL-RESULT.json` | `0ac0acc3ea9a7b05910f09c07c770e48946333c5de9c06fc626143b5abde14b7` |
| `SHA256SUMS` (18 files) | `063a11c811cab826c6ffd58dfff72ab26dddeacf520a4a40e0421b6bf0cb7167` |
| Exact before/after module bytes | `e8c8b9fb3bb14bce0eb6d460343ea377ad3a2967d16968f5dc869f285c3da209` |
| Exact before/after source fence | `b6ba9d32d13fba2fe0fc08d287c744ca3089daf09cb40f70fca16b18b8a75b96` |

Astra independently verified all eighteen manifest files, the raw root/package
terminals and process exits, identical before/after module/fence bytes and all
eleven actual clean physical roots. Ten local modules resolve; Warp remains a
separately fenced unselected path. Connect `b163f9dd`, SDK `516521fb` and server
`cfcbfcba` remain the graph's exact pins. The 572.342-second race stream includes
both genuine full-v3 command fixtures and completed within its bounded package
budget. No causal case was removed or production deadline changed.

This smoke checks the composition of already qualified source increments. It
does not expand the 543/618 SDK or 150/270 broader MG-08 counts, and it supplies
no current mainnet observation, signing custody, Safe authority or permission
to execute the proposed successor.
