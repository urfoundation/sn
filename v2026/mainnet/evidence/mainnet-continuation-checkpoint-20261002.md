# Mainnet continuation checkpoint — October 2

This is a source and qualification checkpoint, not deployment approval. Sim-testnet
remains closed with exceptions. The full 38-requirement implementation backlog
remains active; no live signing, broadcasting, installation or activation is reported.

## Retained observer and successor evidence

Exact observer source `6d398662` now passes 17 normal and 17 race roots plus vet
in both author and independent scopes. The [independent receipt](durable-observer-independent-20261002.json)
retains five intended old-body failures per mode and a separate passing checkpoint
uncertainty control. Earlier RPC-count fixture and private-ancestry setup failures
remain retained. These counts are overlapping scopes, not an aggregate suite.

Test-only `33e57ee7` corrects the stale diagnostic assertion over writer `f3c8a618`.
Its [focused author receipt](durable-successor-diagnostic-20261002.json) records one
normal and one race pass, vet, and the old assertion's intended race failure.
The earlier broad writer run remains 84 passes and one fixture failure.

Frozen composition SN `69f4bbdd` / server `10a8f4d8` has passed all 78 selected
normal roots. All 78 race roots and both vets have also passed. Independent qualification
passes its twelve selected public roots in both modes and affected-package vet.
The [sealed composition](durable-owner-composition-qualification-20261002.md)
retains exact overlapping scopes and root binding readback. The exact 287-file source join was rehashed; a source join
alone does not qualify the dependency graph. This composition still selects
Connect `0a5cda0e`, not the separately qualified `71df099c` constructor fix.

## Miner isolation and missing progress monitoring

Source `c3707376` changes startup so a transient failure of a later miner does
not cancel already healthy miners or prevent admission of subsequent miners.
The [author receipt](provider-startup-isolation-20261002.json) records 37 normal
and 37 race passes, vet and two discriminating old-body failures per mode.
Recovery uses the existing serialized member control; this is not permission
to blindly resend a signed wallet request after an ambiguous result.

A separate callback defect was reproduced: failure to persist a refreshed token
cancels healthy siblings after publication. Successor `3664820` has reported
40 normal and 40 race passes; its final sealed receipt and review remain pending.
The fix quarantines and joins the affected member outside the callback, retaining
hard-error precedence. Actual provider progress and an expected-provider roster
in production monitoring remain separate missing interfaces.

## Member custody and recovery ordering

Source `8d37e7a5` passes its first 16 new normal controls for retained member
census, both nonce domains, exact pre-stage intent recovery, child-process crash,
partial-byte refusal and bounded payload handling. The original `8573f8f9`
compile failure remains separate; it executed no test bodies.

The adjacent recovery gate has exposed a real overreach: generic pending recovery
finalizes retained terminal/policy stages before the existing application
reconciliation path. The correction must materialize only the exact reserved
payload that has not yet been staged, preserving historical terminal ordering.
Those tests must remain intact. Separate assertions that mistake the mutable
census head for an immutable signed member need narrowly scoped fixture updates.
The adjacent normal census is terminal: 20 roots, 14 passes and six failures.
Four failures are positive immutable-protocol fixture maps that include the
legitimately mutable census head; two are the actual ordering regression above.
The original log SHA-256 is
`528aebc3caa805b75a924739ea81b36debc5941c1f1a700720a965d43e5a2b8f`.
Corrected-source qualification, race and vet remain pending.
Production offline preparation and its actual role adapters remain open.

## Qualification and evidence hardening lessons

Go `testing.TempDir` creates its numbered child with mode `0777`; under `umask
002` that becomes `0775`. Even with a private parent, whole-ancestry admission
correctly refuses it. Launchers must use `umask 077`, preflight owned ancestors,
and explicitly make protected fixture roots private. Check the actual cause of
negative tests; a setup refusal is not the intended product failure.

A sealed startup receipt originally referenced physical source reused for the
next callback change. Root rehash caught one changed production file out of 39
bindings. The original exact source was restored in its own detached checkout;
all 39 bindings rehash and the original receipt is unchanged. The next source
now has a separate physical checkout. The sealed source tree is read-only, and
actual owner write/create probes refuse mutation. Future source, runner and
capture paths bound by receipts must be immutable after sealing; Git commit
identity alone does not protect a mutable physical path. Preserve the mismatch
and repair record rather than rewriting historical receipts or replaying already
successful tests merely to replace their evidence.

These are qualification-host protections, not production capacity or restore
proof. The local data volume still requires active-cache headroom monitoring.

## Integrated callback and private-fixture successors

Miner `36648203` and test-only fixtures `28c970db` have now been merged into
main. Their [exact scoped qualification](provider-callback-qualification-20261002.md)
retains 40 author / 12 independent selected miner roots per mode and seven
fixture roots per mode under each of two umasks. Source/module/release authority
is not inherited by unrelated candidates. Current server intake at `cebf154f`
failed before test execution because published Connect `6443417d` lacks the
required durablevolume package; compatible successor intake remains in progress.

Current server successor `22e3c1ba` changes only the Connect replacement to
published `e0d75562` and adds its checksum lines; SCTP and unrelated upstream
module floors are preserved. It is prepared for actual declared-graph testing,
not promoted as a qualified server yet. Member source `2c8017f5` has 38 selected
author passes per mode and vet, with all 86 manifest bindings rehashed by root.
Its independent review retains the initial missing-sibling checkout failure;
the corrected graph preflight passes and selected tests are running. Root's
clean current-main composition `5f1fe123` is a review candidate, not main or a
qualified release. It preserves the newer observer, callback and fixture fixes.
