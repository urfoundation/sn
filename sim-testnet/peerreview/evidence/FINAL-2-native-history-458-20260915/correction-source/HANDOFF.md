# Historical companion release-lock admission

Root reviewed the production correction and seven new tests. The final clean
formatted candidate is `3ffc1277d1acd1e21b908c3b2cfd37612602e07b`, in `sn/`
beside this note. Its raw parent is `d11c544f114ee6acf0a36ad5d639c73c1783bc70`,
based on final `c572d993de3116a68b1b2875d91a67c05c839e22`.
Terra owns formatting, compilation, execution and evidence capture. Astra has
run no tests, builds, formatters or RPC for this correction. The canonical and
active physical sources were not edited.

## Cause and scope

The actual native preview failed in
`/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/native-setup-preview-c572d993`
with `persisted setup plan: validator evidence original release lock: release
lock runtime identity is not the reviewed testnet runtime 458 release`.
Its state, binary and release-lock comparisons all passed unchanged.

`validateValidatorEvidenceSource` already authenticates the archived lock's
canonical hash against the source approval before historical admission. Its
historical branch nevertheless called the current-only `validateReleaseLockStatic`.
That validator switched to runtime458 during adoption, orphaning the authentic
runtime455 companion archive. The retained source has the exact reviewed455
commit, Wasm, metadata and empty tag/proposal fields. A read-only census found
81 retained plan files including the active file: 20 v12 files contain that455
archive; 61 v1–v11 files contain no companion source envelope.

The correction admits only exact reviewed455 or exact current458 companion
runtime provenance on this historical branch. Both use the unchanged image,
dependency, section, repository and build checks. Current lock admission,
rendering, final release anchors, current RPC and signing remain458-only.
Existing451–454 runtime artifact and pre-companion plan readers are unchanged.
There is no lock/plan rewrite, new spending allowance or chain operation.

## Frozen execution inputs

`handoff/FORMAT-PATHS.txt` lists exactly four Go paths. Terra completed that
formatting; the only delta was +15/-13 mechanical lines in
`evidence_carry_runtime_history_test.go`, committed as3ffc1277. Its SHA-256 is
`2aaf5d008c4c74a6d0f03c70d41704155d28e0f686560ee52cc021a2889226d4`.
All three production files are byte-identical to the reviewed raw commit.
`handoff/RAW-IDENTITY.json` and `FINAL-IDENTITY.json` retain both source fences.

Use the same 13 dependency snapshots as finalc572, including xops42bfe0. The
simulator package CWD and fixture prerequisites are unchanged: `sn/sim-testnet`,
`testnet.yml`, and the existing `../deploy/testnet` policy/release inputs and
sibling source projections. Use the admitted streaming capture owners with
explicit package CWD, exact compiled listing, verbose body events, numeric
exits, source/dependency/binary pre/post fences and complete owner cleanup.

Normal and race each select exactly **36 top-level roots**, no descendants:
`handoff/SELECTOR.txt`, `roots.expected.txt` and `source-roots.tsv` are the exact
selector, expected set and source declaration map. Preserve the existing
10-minute package / 660-second outer limits. This includes all seven new roots,
the actual carry/archive readers, legacy plan controls, static/render lock
controls, final lock boundaries and current/historical runtime separation.
Checkout-wide source observation is outside this bounded matrix.

The actual source gate owns every new `TestValidatorEvidenceHistoricalLock...`
root through its existing `ValidatorEvidence` prefix and `evidence_carry*_test.go`
source glob. `TestProducerGateStateSelectionPartitionsSimulatorEvidenceExactly`
and `TestProducerGateStateSelectionCoversEvidenceV2ContractAndInstallerSources`
are included to prove actual script/source ownership. No selector or frozen
inventory needed editing; the semantic-integrity selector does not own this
family.

After normal36 passes, the two roots in `handoff/CONFIRMATION-SELECTOR.txt`
(`ReopensOriginalApproval`, `RestartsCurrentCarry`) each need fresh sequential
normal p2 then p3 on the same normal binary/source/dependencies. Matrix p1
counts only if its exact root passed and all fences closed. Race36 is the
affected race qualification; no old completed streak is reset.

## Deterministic causal control

After the positive candidate is frozen, make a separate disposable checkout
from that exact final commit. Apply
`handoff/CAUSAL-current-to-original-runtime-admission.patch` **forward**. The
only dirty path is `sim-testnet/evidence_carry.go`, changing the historical
branch back to `validateReleaseLockStatic(source.ReleaseLock)`. Retain the
exact diff and successful reverse-check; never mutate positive inputs.

This changes production code, so build one separate normal mutant binary using
the existing dirty-mutant capture owner. Its exact four-root selector is
`handoff/CAUSAL-SELECTOR.txt`, with 10-minute/660-second limits and expected body
exit1. `causal.outcomes.expected.tsv` specifies **2 FAIL / 2 PASS** and
`causal.failure-literals.tsv` specifies one exact joined literal per failed
root. Reopening the original archived plan and restarting the current carried
plan must fail at the old current458 runtime check. Static constraint refusal
and current release authority controls must pass. New helper code/tests stay
present; this proves the missing production dispatch rather than merely
changing an expected value. No race mutant or full old-source rerun is needed.

## Adjacent review and source identity

The runtime-specific caller change is only in `evidence_carry.go:834`, after
its original lock approval-hash check at lines807–814. The shared historical
reader in `evidence_carry.go:121` first uses
`decodePersistedPlanBytesForHistory`; `executor.go:796` authenticates the
complete wire hash before budget/source admission. The unchanged current
`loadPersistedPlan` independently compares the current release and resolved
input hashes before validating the carried source.

Reviewed direct entry points are `validatorEvidencePayloadsForPlan`,
`authenticateValidatorEvidenceCarry`, the original source/anchor archive reads,
`writeRunInputs` fallback archival, and fleet-mirror, native-funding and
conviction lineage readers. The new controls exercise the archive reader,
actual payload reconstruction, current persisted-plan reader and all three
lineage loaders. Existing selected tests retain signed-envelope, source
substitution, historical action/commitment recovery and immutable budget checks.

All production `validateReleaseLockStatic` and `canonicalReleaseLockBytes`
callers were searched. Current release observation/update/rendering and
`finalReleaseRuntimeRootsForPlan` / `verifyFinalReleaseLockArtifact` bind the
final candidate and remain current-only; they do not authenticate a predecessor
companion lock. The existing historical artifact readers still accept their
exact451–455 identities. No current client or global historical allowlist changed.

Three production Go paths change, so final source hashes, release lock and
stamped CLI must be updated. The accepted c572 read-only renderer dynamically
hashes tracked production Go and can render this successor without a separate
bootstrap build. Root owns that final render/publication and native admission.
Completed unrelated181-root runtime qualification remains evidence of its
unchanged recorded scope; this matrix qualifies the new historical lock path.
