# Root daemon output source candidate, 2026-09-28

[Component qualification is complete](root-output-qualification-20260928.md):
41 roots have scoped normal/race passes after retained fixture corrections,
six causal families reproduce their intended failures, and vet plus seven
offline alert rules pass. The following text records the authored source scope;
the linked receipt supplies executed results and remaining rollout work.

This slice follows bounded diagnostic source `74b827ab864650a8eb5c66b8e512d6719ed3149a`
and lifecycle fixture `ba6a7ec356738cd8afbe5e4183d501fcd4fbe70f`. Its physical source
is `/mnt/data/sn-testnet/worktrees/sn-mainnet-root-output-20260928/sn`;
author investigation/qualification inputs are under
`/mnt/data/sn-testnet/evidence/mainnet-root-output-20260928`.

The actual `root-monitor` command owns bounded stdout/stderr from admission
through joined cleanup. Compact v2 diagnostics replace its large census event;
finite `root-preview` retains complete v1 output. Optional atomic metrics use
explicit bounded role labels and preserve current read, retained finalized
progress and prior output acknowledgment separately. Metrics admission/write
failure cannot stop independent read-only observation. A refused or changed path
is left untouched and disables that optional publisher; absence requires an
external expected-source alert. Required checkpoint and integrity failures remain
hard, with the original 60-second minimum/300-second default read retry policy.

The private root service `Run` API consumes a concrete scalar exporter instead
of calling an arbitrary publisher. It closes that owner on early return and
preserves original poisoned-journal causes, including simultaneous cancellation.
Its actual action ports and durable journal formats are unchanged. Tests author
blocked/full/disconnected output across real intent/signature/attempt retention
and restart without extra action allowance, unformattable read causes, both sides
of required commit failure, physical full-pipe command cancellation, real
checkpoint/metrics writes, ambiguous publication, refused/replaced targets and
unknown/stale observation separation. Positive tests use explicit barriers;
bounded completion deadlines only detect failure to join physical I/O.

Source review and compile/vet investigation precede the separate Terra normal,
race and causal executions. This source receipt **does not claim those bodies
passed**. The maintained qualification owner will retain actual package/module
resolution, source manifests, binary identities, root census, terminal membership
and exact causal failure literals. Causal variants are separately frozen and
must never be integrated. Provisional root alert rules need offline rule tests
and independent deployment review; no notification or service was installed.

The consumed graph retains the existing bounded-output candidate's separate
receipt-prefix dependencies (`22aca4cc`/`fbaa71fe`, fixture `093272b6`/`06da5cbe`).
This focused qualification does not repeat or supersede those families, nor the
separate diagnostic wire fixture correction `bc1aefcd`. No blocked contract/Safe
correction, provider-payment implementation, live key, chain write or deployment
is part of this slice.
