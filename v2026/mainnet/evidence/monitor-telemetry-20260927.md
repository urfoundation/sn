# Monitor telemetry source qualification

The actual mainnet monitor command now publishes eleven fixed textfile gauges.
Checkpoint v3 retains initial outages, finalized continuity and completed read
time across restart. Existing v1/v2 records remain readable without inventing
missing observations.

Qualification on 2026-09-27 passed **26 selected normal tests, 26 selected race
tests, `go vet ./mainnet`, and the Prometheus alert-rule fixtures**. Six source
overlays reproduced the former initial-outage, severity, startup-publication,
aliased-ownership, completion-time and closed-owner failures before the corrected tests
passed. The alias overlay exercises two distinct root regressions. A first
run also exposed and corrected a permission fixture that depended on the
caller's umask; the failed evidence remains retained.

Commands, source hashes, expected-failure logs and final results are under
`/mnt/data/sn-testnet/evidence/mainnet-monitor-telemetry-20260927/RESULT.md`.
Its `SHA256SUMS` manifest hashes to
`2c25d2ad9e65118a5049179ada8a22b8f96a0011666215568c7c6c2d93ff6d65`.
Tests used only synthetic local HTTP, explicit barriers and an injected clock.
Prometheus 3.5.0 rules were checked with its image pinned to
`sha256:63805ebb8d2b3920190daf1cb14a60871b16fd38bed42b857a3182bc621f4996`.

This qualifies [source and alert examples](../MONITOR-TELEMETRY.md). It does not
prove deployed collection, external alert delivery, expected-host provisioning
or cross-domain health. Those remain MG-07 launch gates.
