# Original bootstrap readiness custody qualification

Date: 2026-10-01–02. Qualified source:
`3d1e2ecfadf75e33341830a86b6e1871b050840a` on
`fix/mainnet-bootstrap-readiness-custody-20261001`. The source patch is based on
SN `1d580d5e60e569c346cf2aaba2f7cf12eed8c032`, rebased onto the documentation-only
`31c3e2e4` successor. Server remains
`ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`; Connect remains `e1b5d77b5029`, SDK
`5d37be3876e5`, and the other qualified sibling revisions are unchanged.

This correction covers the original preparation reader's five markers, the
passive v4 subset of three markers, and the consumers that retained those
descriptors without revalidating physical custody. It does not qualify the
separate legacy root writers, owner-trim's sixth exclusive marker, cross-host
signing custody, a hostile filesystem, complete rollback detection, a release
artifact, or live launch acceptance. No live transaction, signature, deployment,
unit start, or database mutation was performed. All issued test effects used
synthetic keys, local EVM fixtures, or an in-process fake system manager.

## Causal failures

A flock protects an opened inode. A replacement lock pathname with identical
bytes is another inode and can admit another owner. The old
`bootstrapChainReadinessState` checked markers and journals only while opening,
then trusted the cached seals through subsequent reads and host decisions.

The unchanged `1d580d5e` baseline, with only the regression test file overlaid,
failed all three selected roots: each of five legacy and three passive marker
replacements produced complete readiness, deletion of two completed child
journals produced complete readiness, and each of three passive markers allowed
a synthetic start both before and after start reservation. The test process
exited 1. The failures are assertions at observed output/effect boundaries,
not compiler or harness failures. The baseline fixture and log remain preserved.

A separate exact-base overlay fails four further roots (`155.864s`): an original
root-service marker replacement still produced canonical receipt readiness;
root-progress marker loss still produced a complete installation;
preparation-marker replacement after attempt reservation still issued the
retained successor transaction; and marker loss after an issued passive
install/start still published unretained completion. Both baseline groups use
only copied tests over unchanged production source, and all seven roots fail
through their intended causal assertions.

The validator current-admission check already ran before its final manager
inspection. Its new original-custody capability must also be checked after that
inspection, immediately before start, and before acknowledging the invocation.
A one-file omission control restores only `validator_activation.go` from the
old source while retaining the new custody capability and unchanged regression.
The omission fails the unchanged regression (`5.164s`) and produces synthetic start counts `[1, 0]`. The fixed regression passes. The omission retains all other correction files; its eight-line source delta is preserved separately. It specifically proves the last host boundary, without attributing the new capability plumbing to the old release.

## Final behavior and custody scope

The reader reuses the existing shared marker owner from the independently
qualified eight-action reader. Every marker retains its private physical parent,
opened and named inode identity, single-link/private regular-file constraints,
and exact expected marker bytes. The original journals retain bounded raw-byte
digests, including every signed transaction/extrinsic, counter and receipt.
Final checks reread those digests and markers before releasing borrowed custody.

An observed replacement, missing completed record, changed journal, or invalid
completed record is an integrity failure. A failed open reader remains failed
even if another actor restores its original files. It does not reconstruct or
rewrite original custody. A canceled read alone does not poison intact custody.
An originally valid interrupted preparation phase remains pending and can be
completed by the original preparation owner; a completed marker with a missing
journal is not such a pending phase. New invocations must revalidate the actual
original files. This is not a durable cross-host quarantine or a rollback proof.

Passive host installation/start checks bracket actual effects and manager
readbacks. Failure before reservation consumes no start; failure after the
durable reservation keeps the consumed allowance. Custody errors suppress
unretained `installed`, generation, observation, and current-running result
fields and report `original-custody-integrity-failure`; they preserve the
consumed-start flag and original journal. Current validator admission retains
the same original cohort through its concrete installation adapter and rechecks
after the final manager read and before acknowledging a start. Independent
approval domains, serialized preparation seals, signed bytes and start budgets
are unchanged. Validator claim now holds original preparation custody until its
claim result has been checked.

The caller census covers all ten production opens and their dependent consumers:

| Consumer | Retained boundary |
| --- | --- |
| `observeBootstrapChainReadiness` | After dependent finalized reads and before successful output; error clears partial current facts. |
| `contract-readiness` | Before releasing original preparation and publishing custody inspection. |
| `contract-successor-proposal` | Before publishing a proposal with original preparation seals. |
| `loadBootstrapSuccessorPreparation` | At reconstruction return, then preparation/preview/review consumers before local publication and result. |
| `loadBootstrapSuccessorExecution` consumers | Before ownership/adapter construction and preview, online readback or execution output. |
| `bootstrapContractReceiptScope` | Both existing receipt checkpoints now include original preparation. |
| `bootstrapSuccessorCanonicalChain` | Owns original preparation itself; its existing authenticate/observe/submit/installation checkpoints include the cohort. |
| `rootPassiveHostStore` | Owns original preparation for store lifetime; load/save and install/start/count/result boundaries recheck it. |
| `ownerTrimStore` / `trim-plan` | Before/after load and publication, plus final planning output. The sixth exclusive marker is separate and remains deferred. |
| Validator activation claim/current production | Claim holds original preparation to final inspection; current production closes over both retained preparation and canonical adapter checkpoints. |

## Qualification

The frozen normal run passes all 44 selected roots (`725.650s`). Selection
closure exactly matches the root manifest, including all eleven new roots;
existing signed readiness, inactive/conflicting owner recovery, passive
installation/start/restart, validator current starts, owner-trim reopen,
successor preparation/review/execution, and the public current-only exact-send
path. The fixed source and sibling fence remain unchanged.

The author race run passes all fourteen selected roots in one terminal package
(`1354.234s`): the eleven new roots plus signed readiness, passive host
install/start/recovery, and the healthy two-validator current-admission starts.
The selected-root closure has no omissions or extra roots. No timeout,
continuation, assertion failure or data race occurred in this final invocation.
Explicit `go vet ./mainnet` passes (exit 0, `20.089s` command elapsed). The
qualification source and dependency fence are clean and unchanged.

The [portable qualification receipt](bootstrap-readiness-custody-qualification-20261001.json)
records the exact commands, root results, source/dependency pins, causal scopes
and raw artifact hashes. Its author normal command is `go test -v ./mainnet`
with the exact 44-root selector, `-count=1 -timeout=40m`; race uses
`go test -race -v ./mainnet` with the exact 14-root selector,
`-count=1 -timeout=45m`. The full selectors are in the linked receipt and the
retained root manifests. Documentation later joins main `0a8f0afb`; that join
does not change the qualified Go source, dependency pins, or executable scope.

Independent qualification uses a separate clean `3d1e2ecf` checkout with the same
sibling fence. All eleven new roots pass normal (`201.983s`) and race
(`1282.421s`) in terminal packages; explicit package vet passes. A separate exact
`1d580d5e` checkout with only two new test files copied in fails all seven causal
roots (`191.444s`). The sealed independent receipt is
`eb083cf71318778ac3fcb4973fa4217f9fe134856a03a8904e1eb916e23ce18e`
(SHA-256), retained at
`/mnt/data/sn-testnet/sol-mainnet-custody-successor-independent-20261001/readiness-custody-receipt.json`.
A verified copy and its command, log and baseline-fence files are also retained
with the author evidence. These results do not expand to the separate author
adjacent scope or release artifacts.

The exact root manifests, command arguments, exit statuses, elapsed times, source
fences and full logs are retained under
`/mnt/data/sn-testnet/bootstrap-readiness-custody-20261001/evidence`.
Intermediate development runs are kept separately; they do not replace the
frozen-source qualification. The initial ten-root development run passed
`199.496s`; the subsequent five-root completion-boundary run also passed.

Every new worktree, test scratch directory and build cache is on `/mnt/data`.
The initial baseline and normal run read the existing immutable root module
cache without an observed source read error. Later controls/race/vet use a
separate data copy of the qualified module cache. The toolchain is Go
`1.26.6 linux/amd64`. Existing qualification-host medium errors remain a separate
MG-02 limitation; no root media recovery was attempted by this work.

## Deferred launch blockers

`rootActionStore` has no production caller outside its definition; public root
service constructs `newRootOwnedSubmission(..., nil)` and omits service
Authority/Submitter ports. Its admission refuses fresh signing/counting without
those ports. The owner-trim public command constructs `ownerTrimExecutor`
without independent authority and `newOwnerTrimCanonicalChain(..., nil)`; its
submit rejects absent authority before transport. This correction does not make
either route fresh-send-capable.

The marker/journal ownership of `root_action_store.go`,
`root_submission_store.go`, `root_service_store.go`,
`root_offline_custody_store.go`, `bootstrap_root_store.go`,
`bootstrap_chain_store.go`, and owner-trim's own sixth marker still needs a
separate causal qualification before wiring any fresh native signing or sends.
The borrowed-five correction in `ownerTrimStore` is not a qualification of that
separate sixth-marker writer. Launch must keep these capabilities unavailable
until their corresponding P0 custody work and authority gates are closed.

The release frozen at `1d580d5e` excludes this source correction. The
[exact readiness artifact successor](release-3d1e2ecf-serverac86-20261002.md) now packages the qualified
`3d1e2ecf` source with server `ac86855d`, matching source/bytecode/OCI repeats
and separate independent artifact readback. This qualified scoped baseline is
superseded for launch by later owner-custody source `21640419`, which is excluded
from those artifacts and requires a new exact release composition. Its unchanged
base receipt must be read with the separate corrected finalization guard. MG-02
remains open for independent compiler provenance and complete production
qualification. Live current admission, installation/host custody acceptance,
operational rehearsal and the other mainnet gates remain open.

The author evidence manifest is `evidence-sha256.txt` under the retained evidence
directory. Its SHA-256 is `d6a958be1a17ce642cfcb0b9b68b6f62c22d9a84ddfd1b52a63930f5840a770a`.
It seals 56 retained artifacts, including both causal groups, the omission control,
intermediate development history, source patch and independent copies.
