# Independent schema 750 and mixed-writer check

On 2026-10-01, an independent test run used clean server
`720e7c61182983dd2cd6de667787bb5b52f4d8a4` and frozen SN
`b8dc332aeb2d934a1aaa3de02ba2bbffc7f98627` with a disposable local
PostgreSQL 18/Redis 8 fixture. No production database, live service, or source
was changed. The exact [receipt](/mnt/data/sn-testnet/sol-server750-qualification-20261001/receipt.json)
has SHA-256 `0516b853be2549285039efb403dde64f4e3ffa0ec478243f1bc5cdbdd8234b45`;
its `SHA256SUMS` seal has SHA-256
`7317308c36d1c9960b8d26aac410b5ea8757bbb8a5c98ae78aad64a7f35daffb`.
All retained entries verified. Four owned fixture containers were removed and
verified absent.

The selected server normal packages, serial router race rerun, server/model
race packages, SN seed-picker normal/race, and affected vet checks passed.
A test-only readiness overlay proved that a 749 database is refused and the
migrated 750 database is accepted. The initial router race stream failed during
a short fixture connection preflight; its corrected serial host-network rerun
passed. That initial stream is retained as a harness diagnostic, not a product
test verdict.

The mixed-writer control intentionally **failed** and found a real v2 policy
activation blocker. A new writer inserted a connection row with
`arin_quality_verified=true` and favorable `arin_risk=false` /
`arin_non_quality=false`. An old server `0b8e758d`-shape `ON CONFLICT` update
changed the location and wrote favorable risk/non-quality values but omitted
the new verified column. PostgreSQL retained `arin_quality_verified=true`;
the strict subscriber guard's row expression still evaluated eligible. The
[raw control](/mnt/data/sn-testnet/sol-server750-qualification-20261001/mixed-writer-host.jsonl)
and test-only overlay are in the same sealed evidence directory. This control
evaluated the guard expression on the row; it did not rerun the complete live
provider-connection join.

Keep `subscriber_quality_policy_version` absent or `0`, and keep the candidate
MMDB out of active launch inputs. Before any v2 activation, drain all older
writers and re-attest affected rows, or implement and qualify a fail-closed
mixed-update correction with deterministic old/new interleaving tests. Schema
750 migration is required for server `720e7c61` even while v2 stays off. The
tests do not approve deployment, subscriber v2, or the current release image.
