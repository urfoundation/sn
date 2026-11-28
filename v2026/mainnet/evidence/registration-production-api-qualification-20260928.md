# Actual SDK, production API and database qualification

Server `5dc11761373580b5a0ddd9757cd6e4eb94140e27` is integrated on
`codex/mainnet-composed-hardening-20260927`. Its addition is test-only, atop
the qualified versioned registration transaction, grammar and allocation-attempt
changes. No production model bytes changed after `077fe625` for this fixture.

Terra ran both actual API roots normally and with race detection. All passed.
The fixture constructs the production router and routes, generates genuine
synthetic signed network credentials, and reaches session/JWT validation,
controller and real PostgreSQL allocation through the actual SDK/Connect HTTP
client. It breaks a response only after the handler commits, then proves the
original request recovers the same client/device, including after SDK reopen
and bearer renewal. It also exercises request/principal conflicts and actual
remove-client revocation without allocating a replacement identity.

The single control changes only the production route-table path. Its lost-reply
root reaches `real versioned route did not recover complete SDK identity` in
both modes, confirming that the fixture consumes the real route composition.
It does not replace the separate model/SDK transport and allocation controls.

| Scope | Normal | Race |
| --- | --- | --- |
| Two real API/database roots | Passed; 8.410 s | Passed; 16.674 s |
| Route-table control | Intended failure; 4.109 s | Intended failure; 7.775 s |

Both maintained captures passed four stages with unchanged source, exact
terminal membership and joined bodies. Positive bodies exited 0; control
bodies exited 1. API vet, input seals, before/after content/HEAD/status/module
checks, owned-service cleanup and the overall driver exited 0. PostgreSQL and
Redis were independently owned disposable fixtures. No full model body was
repeated and no live identity or database was changed.

Raw evidence:
`/mnt/data/sn-testnet/evidence/server-registration-sdk-route-20260928`.

| Report | SHA-256 |
| --- | --- |
| `positive-capture/report.json` | `9a6a9187950cd4da883356996d255b9a313acb45a2e0b9672663a113f00b7b91` |
| `control-capture/report.json` | `2fedd7b01abb953370dadc5dcfc1f0ea2431ff97b8d8758fc10873273089de1a` |

The ten-repository graph pins SN `d3ce3e88`, SDK `516521fb`, Connect `b163f9dd`
including SCTP, server `5dc11761`, and physical operator-proxy plus the other
replacements. The eleventh tree is the one-line route control
`784588d171ac07fe64b0edc8e7e1b10cd3907bfd`; it must never be deployed.
All complete revisions, commands and manifests are retained in the handoff.
The previously qualified runner is `cf73edc6`. Production server-first migration,
rollout, final release assembly and mainnet acceptance remain separate gates.
