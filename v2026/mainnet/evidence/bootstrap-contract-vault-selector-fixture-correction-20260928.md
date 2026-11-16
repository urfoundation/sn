# Vault checkpoint fixture selector correction

Base: `cdfe07449f86b041fc2aa27343e6d19e168e8ce5`. The containing commit on
`codex/mainnet-vault-selector-fix-20260928` is the isolated correction candidate
at `/home/by/urnetwork/temp/sn-mainnet-vault-selector-fix-20260928`.
It changes one existing test root and this handoff. Production execution,
approval payloads, journal schemas and historical bytes, modules and artifacts
are unchanged. The candidate remains provisional until Sol qualifies it.

Sol's focused 29-root run and isolated reproduction both failed
`TestEvmVaultLinkFencesSixPredecessorCheckpoints`. Preserved raw evidence is in
`/mnt/data/sn-testnet/qualification/sol-vault-link-20260928/focus-normal.log`
and `repro-checkpoint-normal.log`. The callback panicked on the unchecked
`params[2].(map[string]any)` storage selector assertion: current admission sends
the string `"pending"`. The HTTP handler's panic produced EOFs and eventual
retry-window exhaustion. The isolated failure after 102.45 seconds confirms
the same fixture defect; the timeout is not evidence of generic CPU contention
or an unavailable external RPC.

The callback now dispatches the selector by its concrete type. Only a canonical
block map carrying the original reserve-binding inclusion hash can release the
historical proxy-read barrier. A pending string records current-state traffic
without releasing that barrier. Refreshed continuity cases explicitly require
an actual pending proxy storage read before the injected fork is rejected.
Every one of the six predecessor checkpoints is still tested before initial
admission and after current-state admission, followed by restoration and
recovery of the sole original signed binding.

Before starting the HTTP-driven action, the root invokes the same callback
with its real proxy owner-slot query and a pending selector. It requires an
unchanged response, a pending observation and no historical/fork barrier
release, then clears that observation so the action must exercise its own
pending read. Restoring the old unchecked assertion now fails directly in this
probe instead of waiting for swallowed HTTP panics and retry exhaustion.
No sleeps, scheduler assumptions or widened timeout are introduced.

Adjacent review covered the reserve five-checkpoint barrier, the earlier
proxy/escrow checkpoint barriers, the reserve low-gas getter callback, and
canonical selector assertions in vault/reserve/escrow/proxy inclusion tests.
Earlier checkpoint barriers do not cast the mixed block selector; the low-gas
callback branches on pending before reading a map. Inclusion-only callbacks
are installed after mining and only reconcile their retained receipt, so
current admission does not reach those casts. The in-progress index-7 evidence
checkpoint callback copied the faulty pattern; it will receive the same typed
dispatch and direct pending probe before its independent provisional freeze.

Author validation is limited to gofmt, `git diff --check` and compile-only
`go test -c -p=2`; no test binary was executed. The external module files,
compile log and unexecuted binary are under
`/mnt/data/sn-testnet/worktrees/sn-mainnet-vault-selector-fix-20260928/`.
The dependency pins are those in
`bootstrap-contract-vault-link-source-20260928.md` and the inherited read-gas
correction. There were no live signing, RPC, DB, chain or Safe-inner actions.

Sol should first qualify the exact root normally and under race:

```
^TestEvmVaultLinkFencesSixPredecessorCheckpoints$
```

The causal control replaces only the new typed selector dispatch with the
original unchecked map assertion while retaining the direct probe and other
assertions. It must fail immediately at that probe after fixture preparation.
Then qualify all `^TestEvmVaultLink` roots, adjacent predecessor checkpoint
roots, and broader gates at this exact successor. Preserve both original logs;
this compile-only handoff claims neither a behavioral pass nor completion of
the parent vault-link qualification.
