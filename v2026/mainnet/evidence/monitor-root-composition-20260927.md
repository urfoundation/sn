# Mainnet observer and root service composition

The complete `mainnet` package at SN `448b6dd8` passed **279 root tests and
41 subtests**, with no failures or skips, in 208.294 seconds. This joins the
[monitor telemetry](monitor-telemetry-20260927.md) with the qualified root
submission adapter. All reads and submissions in these fixtures are synthetic
and local.

The subsequent `4a301250` integration adds the standard validator's approved
production runtime and owner-recycle path. Its 42-file producer source manifest
and runtime owner's manifest match the separately qualified files exactly.
The composed `mainnet`, `crv4` and `validator` packages also compiled. The
producer/runtime behavioral checks remain the normal/race and causal results
in [producer qualification](owner-recycle-production-qualification-20260927.md)
and [runtime qualification](production-runtime-qualification-20260927.md), rather
than treating a compile-only command as another behavioral run.

Commands, logs, source identities and the captured mainnet test binary are in
`/mnt/data/sn-testnet/evidence/mainnet-monitor-root-composed-20260927/RESULT.md`.
These results do not establish deployed alerts, live root custody, mainnet
identity or a completed mainnet acceptance interval.
