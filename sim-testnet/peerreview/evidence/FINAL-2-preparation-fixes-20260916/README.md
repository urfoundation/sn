# Preparation fixes: complete focused qualification

PF-01 accepts **32 distinct roots per normal/race mode**. PF-02 accepts **30 per mode: 9 SN, 8 server/task and 13 server/taskworker**. PF-01's causal result is 2 expected FAIL / 6 PASS; PF-02's is 7 expected FAIL / 5 PASS across its four package bodies. These results cover the two bounded corrections. Native startup, soak and release-candidate completion are separate; server merge-publication checks are also separate from this qualification.

| Closed capture | Actual selected-root outcomes | Raw body / outer / join exit |
| --- | --- | --- |
| PF-01 original normal and race | Each 31 PASS, 1 FAIL; original failures retained | Each 1 / 127 / 127 |
| PF-01 fixture confirmation normal and race | Each 2 PASS | Each 0 / 0 / 0 |
| PF-01 causal normal | 2 expected FAIL, 6 PASS | 1 / 1 / 1 |
| PF-02 SN normal and race | Each 9 PASS | Each 0 / 0 / 0 |
| PF-02 SN causal normal | 2 expected FAIL, 1 PASS | 1 / 1 / 1 |
| PF-02 server/task normal and race | Each 8 PASS | Each 0 / 0 / 0 |
| PF-02 server/taskworker normal and race | Each 13 PASS | Each 0 / 0 / 0 |
| PF-02 server causal, three bodies | Combined 5 expected FAIL, 4 PASS | Each 1 / 1 / 1 |
| Initial server environment refusals | Two censuses; no selected body started | Each 125 / 127 / 127 |
| First portable server owner refusals | Seven censuses; no selected body started | Each 125 / 127 / 127 |

The successful second server service owner recorded setup 0, cleanup 0 and enclosing exit **1**. That 1 aggregates the three expected causal body failures; all seven body outcome maps and fences are valid. It remains recorded as 1. The earlier owner attempted to wait on non-child launchers, cleaned up prematurely and left seven censuses without their test environment. Those are retained non-admitted attempts, not passing controls or semantic test failures.

PF-01 bounds actual carried preparation with a detached journal snapshot/index and call-local authenticated historical-plan reuse. Full ancestry, latest eligible journal selection, every receipt hash/identity and original RPC labels, current postconditions, cancellation and durable audit-cache behavior remain checked. A final snapshot rejects journal changes before publishing keys. Eight new deterministic roots exercise collected operation counts, multiple source authorities, corrupt source/receipt, changed scope, appended journal and cancellation/retry. The causal source restores real repeated journal/source reads through the same counters: 66 snapshots / 4224 rows and 64 source decodes produce the two intended failures.

PF-01 source `351ece79d9f4dad93888c74c8bdcc699dd4c8dac` is followed by test-only `d52028de6864f7b1c48381fcb7652361d2520932`. The original direct fleet-history fixture warmed its durable cache before expecting another cold verification, giving 5/2/1 requests. The correction clears only that fixture's cache, keeps the exact cold 6/2/2 proof and all receipt/identity negatives, and confirms the unchanged warm-cache behavior separately. Acceptance combines 30 unaffected original passes with those two confirmed roots per mode. Both original failures remain failures. Causal source is `57983498878ba1bc9e84dcc4f5502580ccd3d5f6`.

PF-02 binds the explicit subnet-operator workload into both managed operator taskworker specs and supervisor restarts. The server scopes scheduling and queue admission to required existing tasks, including ST reconciliation and verification; ordinary defaults and strict log classification remain intact. Positive tests cover queue windows, excluded rows, aliases, post wrappers, leases/drain, defaults, spec serialization and classifier behavior. Causal variants restore unfiltered queue claims, unrestricted profile initialization or unbound operator specs. Exact sources, selectors and expected outcome maps are retained in the admitted inputs and fences.

## Evidence layout and verification

`interim/` is the complete earlier sealed PF-01 plus partial PF-02-SN receipt, preserved byte-for-byte. Its original manifest SHA-256 remains `cbdc670db18b265745bbe0162e76967d18b265a3b5c625c6de563155773365fb`; its partial status is historical and is completed by `pf02-server-final/`. That directory contains the seven closed retry-2 server bodies and safe owner/admission metadata. The original server compiler receipts remain in `interim/pf02/server-original/`. The prepared-only capture, rejected chmod-altered PF-01 workspaces and both server refusal batches are retained separately. No compiled binaries, private service state, runtime configs, environment files, credentials or private native logs are included.

`CAPTURE-MAP.tsv` binds every copied artifact to its original path and exact SHA-256. `ACCEPTED-ROOTS.tsv` reconstructs all 124 accepted mode/root rows directly from terminal test2json events. `INVOCATIONS.tsv` records all 24 closed body/census owners and their actual exits. `COMPOSED-ACCEPTANCE.json` records complete composition and expected causal outcomes. Source, dependencies, binary and selector before/after fences were checked while packaging; no compiler or test was rerun. Raw original PF-01 `events.valid=false` is retained because its original all-pass expectations failed.

Terra's closed receipt indexes remain byte-for-byte, including the earlier partial index and original composition. The 200 PF-01, 120 PF-02-SN and 405 final PF-02 artifact references resolve to exact copied files. `pf02-final-qualification-receipt/` preserves the complete final PF-02 authority: RESULT SHA-256 `c593122553e9ce384f190bf21bf320bbc75e9a82f356c60f43df30e239b9d939`, original manifest seal `fb4b2beceeba03967f17d693886f897c5e6f82804fd2e67c9f59a32a6e43e230`. Captured absolute paths and commands document their original invocation and must not be used to overwrite closed captures. The outer `SHA256SUMS` uses portable relative paths and is bound by `SEAL.sha256`; run `sha256sum -c SEAL.sha256` and `sha256sum -c SHA256SUMS` from this directory. Inner original manifests retain their original path scopes. Root controls publication, matched release build and native recovery.
