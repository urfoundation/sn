# Runtime 461 observed during preparation

This bundle records closed preparation attempts and a finalized LAN observation.
The release-candidate campaign did not start; final acceptance remains false.

The qualified startup and direct campaign handoff were published at
`1860261f524054b3d8132a48f75307ffdb792322`. The matching executable built
successfully with SHA-256
`114bede0b30a9bc9fdb946f3075d1e1f3ff9b3e36daf57d1c084f6144e43bb07`.
[Root's build review](FINAL-CLI-ROOT-REVIEW.json) records the closed build evidence.
The read-only software plan passed with unchanged actions, limits, custody,
renewals and signed history; its [complete comparison](plan-revision/root-full-diff.json)
is retained here.

The first setup invocation exited one before preparation because the physical
SN checkout was detached and executable attestation could not read its upstream.
The [branch repair](branch-repair/RESULT.json) attached the unchanged commit to
an `origin/main` tracking branch. All watched state, executable and lock bytes
remained identical; no rebuild was required for that repair.

The [setup retry](setup-revision-r2/stdout.json) ran **11:43:40–11:57:57 UTC** on
September 16. Body, outer and joined exits are **1**. It completed all **4,673**
carried-action checks and collected **nine failures**, each reporting
`unreviewed identity node-subtensor/461/1/1`. The failed actions are listed in
[ROOT-REVIEW.json](setup-revision-r2/ROOT-REVIEW.json). Eight hard preparation
checks passed, one hard check failed, and launch-runtime inputs remained
explicitly deferred. Doctor reported ready across its 63 checks, with the two
expected soft warnings about using one physical RPC node. Those doctor results
do not establish compatibility with a runtime installed later in the invocation.

The software plan was adopted before the audit failed:
`0xf6e8c46e6a6a79c7c67deb8304e387f4bc9821ad96513c0ab6851d954d0d3bc6`.
Only the saved plan and redacted configuration changed. The journal, supervisor
manifest/state and public identities remained unchanged. The adopted plan's
[lossless comparison](setup-revision-r2/ADOPTED-PLAN-DIFF.json) against the reviewed
planner output finds only generation time and four refreshed finalized-head
facts; its approval hash is identical. A raw-byte equality assertion first
refused these expected observation changes before making any RPC request.
Two error wrappers print `balance=0`; because runtime authentication failed,
these messages do not establish a funding deficit or authorize another payment.

At **12:00:20 UTC**, a separate [raw LAN response](relay-capture/native-head/snapshot.response.json)
confirmed **node-subtensor/461/1/1** at finalized block **8,018,145**:

- Native hash: `0x43093d12230005ca09a38835fb1506e7b018fb52233597e68ad50c440c2d7272`.
- EVM hash: `0xe7570097180720fb03a9d6cf15b7244bdd34e0ec03e9307f7eaaea936b945889`.
- EVM chain ID: **945**, with the original genesis hash.

The [observation](relay-capture/native-head/RUNTIME-CHANGE.json) identifies
`192.168.1.162:9944` and `independent_rpc=false`. The preparer's runtime check
refused this observation before selecting END or launching the native
continuation command. No continuation capture, import or chain submission
occurred. Compatibility with 461 remains separate work.

Four temporary helpers passed local readiness, then stopped with all four
actual child exits and the owner/join exit zero; no forced kill occurred.
[Teardown verification](temporary-services/ROOT-JOIN-REVIEW.json) confirms their
listeners were absent. Their launch happened after setup's body had ended;
the proposed overlap did not occur. Helper health is not campaign acceptance.

[SOURCE-FILES.json](SOURCE-FILES.json) maps byte-identical copies to their closed
sources. Private configuration, signing material and helper logs are omitted.
Command files are inert evidence, not instructions to repeat an operation.
SHA256SUMS covers every payload except itself and its seal.
