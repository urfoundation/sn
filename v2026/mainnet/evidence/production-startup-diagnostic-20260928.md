# Production startup continuation diagnostic candidate

This is an unqualified source/fixture checkpoint, not an acceptance result.
The prior continuation and native HTTP candidate scopes remain independently
qualified or pending in their own immutable captures.

The actual `RunRelease` path now initializes native observation at the signed
activation block, replays real activation/disk/original intent authority, then
reconciles retained native liability before requesting current preparation.
Fresh UID/stake and real settlement publication still gate every new intent and
trail worker. Local semantic history uses caller cancellation rather than the
five-minute read deadline; actual RPC/HTTP owners retain finite read budgets.

The first diagnostic root is:

```sh
go test ./validator -run '^TestProductionStartupRunReleaseReconcilesBeforeCurrentPreparation$' -count=1 -timeout=300s
```

It builds independently signed zero-price continuation inputs, selects genuine
activation digests before compact sealing, preserves real M8 proof/journal replay,
serves exact native receipt/events/application rows and journal ABI state, and
starts the public binary path with both actual local HTTP/session/upload owners.
An explicit current-EVM request barrier must occur after the original pending
intent reaches its actual applied row. Latest metadata is unavailable throughout.
Exactly one original send is retained; the test does not claim paid capture.

Compile-only `go test ./crv4 ./validator -run '^$'` passed before this checkpoint.
No author-lane test body has run. Terra must diagnose the exact frozen revision.
Remaining checks include full-root outage/restart and fresh-deployment startup,
parallel mixed read-cause composition, cancellation/integrity controls, and normal
and race qualification. Both live operator server-key/public-object/session routes
remain startup dependencies; this candidate does not claim offline operator recovery.
Bounded durable receipt-prefix reuse and historical-only foreign-nonce resolution
remain separate open work.

The first normal diagnostic at `1ffa1f34` failed in fixture construction: its
native route used HTTP, while production native submission uses WebSocket
`author_submitAndWatchExtrinsic`. The corrected fixture serves genuine native
JSON-RPC over an explicitly signed WebSocket route. No config/approval gate was
widened, no route is inferred or converted by production code, and HTTP read
constructor support still grants no subscription/signing authority. Both operator
API routes and the EVM route remain actual HTTP endpoints. This fixture correction
requires its own diagnostic; the original failed capture remains evidence.

The WebSocket diagnostic at `af2a6603` passed the route boundary but refused the
fixture's config signature after YAML loading. Fixture input now follows the
existing production-runtime fixture discipline: round-trip and normalize the
public YAML representation before the independent approval is signed. Production
hashing, signature checking and retained-authority rules remain unchanged.

The normalized checkpoint `a6889441` reached public capacity validation and
rejected the private fixture's header allowance. The next fixture correction
audits the related public gates together: metadata pages admit the declared
header; replay rows cover the real retained disk allowance; durable limits
match the physical ledger; scratch roots are provisioned outside every durable
state and credential namespace. The actual corpus and all production gates
remain unchanged. These diagnostics show why private helper admission cannot
stand in for qualification through the public configuration and startup root.

The `bcf95d28` diagnostic exposed the seal helper's later 32 KiB chunk override.
The public fixture now explicitly admits its real 64 KiB maximum ledger row at
both replay and chunk boundaries; raising the replay row limit alone was not a
complete capacity correction. This adds no production default or dynamic bound.

The `ec28042b` run passed public config admission and reached the actual hotkey
loader, which correctly refused the fixture's umask-dependent TempDir parent.
Startup fixture roots now use the existing explicitly created 0700 identity
fixture directory. The physical sweep confirms native/client key files use 0600
single-link files with owned private immediate parents; operator state and
reference/scratch roots are separately provisioned, and existing ancestry is
never chmodded. Approval references already use their private retained owners.
Production custody checks and Terra's existing captures remain unchanged.

## Physical-owner correction after cd709cf4

Terra preserved the full `282fb402` census and separately ran only the two
public roots on `cd709cf4`. The private seed-parent correction passed its
previous refusal. Both modes then exposed two independent fixture errors:

- Retained source: the measurement-only fixture had generated genuine signed
  M8 rows and an ordinary cursor rotation without executing the initial V2
  statistics activation. Its saved inactive image could not represent the
  authenticated advanced cursor. Normal/race correctly refused
  `startup inactive image is not the independently pinned initial boundary`.
- Fresh source: its synthetic stored/refresh token omitted `device_id`.
  Both modes correctly refused the incomplete session identity.

The correction uses the real `InitializeAttemptSettlementEpochV2` owner for
each independently declared empty source before running its genuine M8 trails.
The same normal signed cut, immutable input journal, production measurement,
sidecar and durable intent pipeline follows. It does not paste an activation
flag into a completed image or waive nonempty activation refusal. Existing
measurement-only callers retain their original source path.

One synthetic token now contains both required identifiers, is checked by
`clientauth.ClientIdFromJwt`, and is reused byte-identically for the stored
credential and actual HTTP refresh response. No session, custody or production
activation guard changed. The exact two public roots must still reach original
receipt/application and fresh readiness, respectively; qualification is pending.

Preserved capture:
`/mnt/data/sn-testnet/qualification/production-startup-continuation-20260928/cd709cf4/`.
Normal: 21.931s package exit 1. Race: 147.681s package exit 1. These are fixture
diagnostics, not causal control successes or a completed public-startup result.

## HTTP fixture cleanup after fca0b16c

At `fca0b16c843601b680d92fbe0a8a3a43dc6365fe`, the real retained-intent public
root passed normal (31.17s) and race (189.39s). Preserve those results. The fresh
root reached runtime readiness but its seed HTTP fixture hung during cleanup;
Terra retained a stack showing `httptest.Close` awaiting the POST handler at
`production_startup_fixture_test.go:219`. The package termination is not a
fresh-root pass. Captures remain under
`/mnt/data/sn-testnet/qualification/production-startup-continuation-20260928/fca0b16c/`.

The seed fixture waited on request cancellation before consuming its POST body.
The correction reads and closes at most 1 MiB plus an overflow byte before
publishing the readiness barrier. Both it and the adjacent EVM outage handler
now have independent fixture cleanup releases; their server owners join after
release. The EVM handler already consumed input, but lacked a bound, physical
body close and independent release. The WebSocket handlers are separate read
loops terminated by their actual client connection owners and contain no
request-context barrier. No production timeout, request or custody gate changed.

Two deterministic fixture tests require complete/closed input before the barrier,
join the handlers with a still-live request context, and reject oversized or
failed-close input before any barrier. The repeated monitor/startup lesson is
also recorded under PH-16. Terra must run the affected fresh public root and
these fixture tests normally and with race detection, then the original public
startup-order causal control using the corrected fixture bytes. Prior retained
root and unaffected full282 scopes remain reusable.
