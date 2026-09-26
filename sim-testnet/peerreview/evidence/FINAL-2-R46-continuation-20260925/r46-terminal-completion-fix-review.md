# Future failed provisional interval completion

Base: exact R46 driver ad5c05eca00a73bada9fdc35dfa516487a616177. Live owner, pinned image, setup/chain/config and main checkout are unchanged.

Confirmed controller defect: release observation stops only after assertionsPass and Adversaries.Ready, except a narrow lifecycle-specific escape. Closed-epoch failures and permanent retained process findings therefore cause repeated reads after terminal and cleanup. R46 NO2 epoch634 is authenticated RootMissed (status3, zero root/hash/commit); low-rate readiness itself is a price-dependent margin and is deliberately not a stopping proof. The missing consensus actor samples independently keep Ready false.

The new rule applies only to a provisional release, an exact current signed window/checkpoint/session/plan/definition, complete finalized per-operator epoch census, complete fault census with all restores observed, and completed configured lifecycle. It ends failed observation for an immutable in-window missed root, chain-bound invalid tier artifact, or repeated exact-scope TLS/exit-gap findings still blocking under existing isolated-event rules. It never changes any original assertion. Missing artifacts, low-rate margin, uncertain claims, unknown/recovering process classes and ordinary/strict/production runs keep their prior behavior.

After this stopping decision the ordinary bounded adversary Stop/join, failed sample assertions, final strict process scan, lifecycle handoff capture, result publication and signed failure invalidation still run. No successful completion marker or strict acceptance is created. Existing production handoff integrity and restoration gates remain separate. The failed result adds only provisional_failed_interval_complete with the reason and final_acceptance=false.

Deterministic tests use real signed request/runtime checkpoints plus synthetic chain rows. A full production scheduler fixture runs every declared fault and fails on any next snapshot after complete terminal cleanup; it requires failed payout and sample assertions, one Stop, durable failed result, and no complete.json. No elapsed sleep proves the fix. Negatives cover incomplete interval/epoch, foreign session/hash, missing/pending/active fault, incomplete lifecycle, absent adversary owner, read-only/strict/other phase, unfinalized settlement, missing artifact, mutable margin, foreign artifact/epoch, isolated/foreign/recovering/unknown logs.

Normal focused: PASS 6.738s. Adjacent normal: PASS 27.924s. Combined focused/adjacent race: PASS 222.208s, exit 0, no race findings. Old-controller causal: expected RED 5.487s, exit 1, exact extra-snapshot barrier after complete interval and fault cleanup. All four frozen source files rechecked unchanged after tests; git diff --check passed. Exact source fence: source.sha256. Causal overlay restores only original scenario.go; controller must fail by deterministic extra-snapshot barrier. This patch does not claim to fix R46 validator/native eligibility, actor failures, evidence retrieval or traffic distribution.


Exact commands (working directory `/mnt/data/sn-testnet/worktrees/astra-r46-terminal-failure-completion-20260926`):

```sh
TMPDIR=/mnt/data/sn-testnet/qualification/r46-terminal-failure-completion-20260926/tmp go test -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod ./sim-testnet -run '^TestScenarioFailedTerminal' -count=1 -timeout=15m
TMPDIR=/mnt/data/sn-testnet/qualification/r46-terminal-failure-completion-20260926/tmp go test -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod ./sim-testnet -run '^(TestScenarioInterval|TestLifecycleTerminalCleanup|TestScenarioClaimWindow|TestScenarioPayout)' -count=1 -timeout=20m
TMPDIR=/mnt/data/sn-testnet/qualification/r46-terminal-failure-completion-20260926/tmp go test -race -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod ./sim-testnet -run '^(TestScenarioFailedTerminal|TestScenarioInterval|TestLifecycleTerminalCleanup|TestScenarioClaimWindow|TestScenarioPayout)' -count=1 -timeout=30m
TMPDIR=/mnt/data/sn-testnet/qualification/r46-terminal-failure-completion-20260926/tmp go test -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod -overlay=/mnt/data/sn-testnet/qualification/r46-terminal-failure-completion-20260926/old-controller.overlay.json ./sim-testnet -run '^TestScenarioFailedTerminalController' -count=1 -timeout=15m
```

Logs in the same directory: `final-focused-normal.log`, `adjacent-normal.log`, `focused-adjacent-race.log`, `causal-old-controller.log`. The causal changes only original scenario.go from ad5c05ec; it retains the new deterministic fixture and compiles before the expected behavioral failure. The separate cumulative-cleanup reader repair is not included in this commit.

Qualification toolchain: Go 1.26.6 linux/amd64. Shared external pinned modfile `r45-final.mod` SHA256 c38fba12a5d6fe522eaebd02aea699c834565aa5ce76a8f8b3e0775b0821f010 (unchanged since 2026-09-25 11:15 UTC); it selects the previously frozen R45 dependency checkout.
