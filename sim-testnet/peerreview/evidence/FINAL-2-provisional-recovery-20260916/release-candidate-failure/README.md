# First provisional release-candidate failure

The command ran from **16:53:00 through 17:01:52 UTC on September 16, 2026**
using plan
`0xcba026fa05f500c6ee120a19c94b1f3f0d1362bf4e6cab0db21aa4306d8b4d0f`,
driver SHA-256
`5ff1130c0f1b7ef3404c2d7a8c37f388854bed8299b3ae4bb152ac3ebf220348`
and the owned RPC at `192.168.1.162:9944`. It authenticated all **4,674**
retained local receipts, then stopped before a measured campaign phase with:

```text
open durable release-1.0 attempt: campaign succession requires the strict approved deployment owner
```

Body and outer exits are both one. The runtime binary and installed release
lock are unchanged. The only watched-state difference is
`supervisor.state.json`, which records continued process-health observations
and validator restarts. `CLOSED-RESULT.json` and `CLOSED-SHA256SUMS` are the
owner's sealed result and raw-capture manifest.

The live topology remained present after the command. At closure it had 33
processes and three validator restarts: validator 1 had restarted twice and
validator 2 once. Both validators' initial terminal publication logged HTTP
403 with `validator staging complete discovery exceeds its explicit range
budget`. `validator-staging-observation.json` records a bounded observation of
the generated staging configuration: each operator had zero activation
contexts, both provisional authority flags were absent/false, and the ordinary
discovery span was 1,024 × 16 = 16,384 blocks from deployment block 7,973,237.
Validator process health is PID presence and does not prove terminal
publication.

This is local failure evidence. It makes no new on-chain acceptance claim, no
campaign phase completed, and `final_acceptance=false`.
