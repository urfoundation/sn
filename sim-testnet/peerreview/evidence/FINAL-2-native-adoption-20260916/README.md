# Native software revision and setup adoption

Captured on 2026-09-16 UTC. This bundle records the completed matched CLI
build, read-only software plan revision and setup/adoption. It does not claim
that the relay refresh, managed topology or full campaign has completed.
All actual chain RPC used `192.168.1.162:9944`; `independent_rpc=false`.

| Operation | Actual outcome | Evidence |
| --- | --- | --- |
| Matched CLI build | Build, wrapper and joined launcher exited 0; 13 repository input records were unchanged | `final-cli/RESULT-FINAL-CLI-BUILD.json`, `final-cli/logs/`, `final-cli/capture/`, `final-cli/meta/` |
| Read-only plan revision | Body, wrapper and join exited 0 at 04:06:54; watched state, executable and release lock were unchanged | `plan-revision/result.status`, exit files and before/after hashes |
| Setup/adoption | Body, wrapper and join exited 0 at 04:30:51; `ready=true`, `prepare_only=true`, `stopped_before_actions=true` | `setup/stdout.json`, `setup/stderr`, exit files |

The executable is revision `541e13cfbe968704fb74c4001506853d2529bfdd`, SHA-256
`45455c69687d88287a200979ee914677bf0f39e000274e7fc5fb618339c0fc8a`.
The release-lock file SHA-256 is
`d703afe97a7f3299b3d4e43b7feed0d90e330b161384a9f5d8d33541266945b2`.
Focused qualification is retained in
[the reserve-recovery bundle](../FINAL-2-release-reserve-recovery-20260916/README.md).

The adopted plan is
`0x0d24a3f1dfc8ea5bc6a2f59c80a7580a761d9e3410dd4833bda3843304ce6f86`.
Root reviewed the complete raw diff after inserting layout outside quoted
strings without changing numeric tokens. `plan-revision/plan.raw.diff` and
`REVIEW.json` record that review. All 4,733 actions, limits, maximum and
superseded spending, renewals and the retained continuation are unchanged.
Only release identity, ancestry, generation time and advancing finalized
block/alpha observations differ. Full plan bytes remain at the local capture
paths recorded in `private-capture-plan-hashes.sha256`; they are not duplicated
in this compact bundle, so its diff review is local evidence, not an independent
reconstruction of both complete plans.

Setup completed all 4,673 carried-action checks. Only `plan.json` and
`config.redacted.yml` changed. The journal, both supervisor files and public
identities were unchanged. The journal SHA-256 remains
`92adbc53f96c87286b8c22d0f34fdf3663d9d5cca1ed24de3e4bfd8aeee25346`.
No setup transaction was submitted. `state_compare_exit=1` in the setup
receipt records those expected local file changes; it is not a failed command.

The setup report explicitly defers `launch-runtime-inputs` to launch/resume.
Its two owned-node independence warnings are also soft checks, not independent
verification or hard failures. No deferred check is counted as passed here.

The embedded doctor observed native block 8,015,798, hash
`0x04a3c47751c2ead21076d0fdce9c35e334a4259a3251a58620663b6cdb96a4f1`,
runtime 460/1/1, 256 UIDs and `MaxAllowedValidators=64`. These observations
are reported by the native command; this bundle does not add separate raw RPC
reproduction or new on-chain acceptance.

The final CLI binary, private vault and live runtime databases are not included.
Original command records may refer to absolute capture paths. Run
`sha256sum -c SHA256SUMS` from this directory to check the bundled bytes.
