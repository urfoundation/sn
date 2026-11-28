# Concurrent registration allocation qualification

Server fixture correction `077fe625372b425135d6f260f4c5830579749f57`, based
on request grammar candidate `736d7b8f`, passes its focused normal and race
qualification. This commit changes tests and their receipt; production
registration transaction behavior is unchanged. Astra authored the fixture;
Terra executed every body on separately owned disposable PostgreSQL/Redis.

The original concurrent-duplicate test proved final identity equality but
did not distinguish ReadCommitted from RepeatableRead. The transaction owner
retries rollback errors for up to 60 seconds, so an extra allocation attempt
could roll back and retry into the already committed identity. The original
control's unexpected passes remain in
`/mnt/data/sn-testnet/evidence/operator-registration-server-20260928/run2/stale-allocation-snapshot-capture`;
they are not causal proof of the isolation fix.

The corrected `TestNetworkClientRegistrationConcurrentDuplicates` holds two
real PostgreSQL advisory-lock waiters at the intended boundary. A disposable
sequence and device-insert trigger scoped to the synthetic network count
allocation attempts even when the enclosing transaction rolls back. The
ReadCommitted candidate allocates once. Restoring RepeatableRead, with the
same fixture and retry owner, deterministically reaches
`concurrent duplicates repeated already committed allocation work: attempts=`.
Neither production hooks nor disabled retries supply this result.

| Scope | Normal | Race | Maintained result |
| --- | --- | --- | --- |
| Corrected concurrent-duplicate root | Pass; 4.960 s | Pass; 7.165 s | 4/4 stages passed |
| Original isolation control | Intended failure; 4.557 s | Intended failure; 6.129 s | 4/4 stages passed |

Both captures report unchanged source; all four bodies joined with exact
one-root membership. Positive bodies exited 0 and controls exited 1. Model
vet, before/after dependency comparison, runner and owned service cleanup all
exited 0. This fixture-only correction does not require restarting the original
full model run, which retains its own graph and results.

The raw plans, source manifests, control diff, commands and results are under
`/mnt/data/sn-testnet/evidence/operator-registration-allocation-attempts-20260928`.
Physical sources are under the corresponding `qualification/` directory.
The graph retains SN `9da4213d`, SDK `42241118`, Connect `358cefae` and the
original registration dependencies; it does not claim the newer composed
SDK/validator release. All ten repositories are fenced by tracked content.

| Report | SHA-256 |
| --- | --- |
| `positive-capture/report.json` | `d68f2d6f69954f022c7b6ac37fa8d6495ea04c1195c76c72856a2ef3dd591d18` |
| `control-capture/report.json` | `222de28a6e3c8e415f0ebf5dec18f508e337a4fe17825fb40f6943dae9d82ec1` |

For future retry and idempotency tests, observe attempted irreversible work
at its actual boundary as well as final identity. A successful retry can hide
unnecessary allocation, duplicate side effects or an ineffective regression
control. This component result does not close full model, composed client
recovery, redirect, migration or deployment gates.
