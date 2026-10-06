# Current-runtime passive root service

The current root role is `passive_accumulate_in_place`: observe an independently
approved existing netuid-0 seat while its dividend basket accumulates. It runs
alongside both UR subnet validators. It never constructs, signs or broadcasts a
root transaction. Registration, root staking, claims, take changes, delegation
changes and optional basket trades need separately approved capabilities.

The [runtime470 review](../docs/spec/runtime-470-audit.md) verifies the exact
observed artifact and current source, retaining the documented Wasmi build-seed
exception and separate independent runtime approval. That runtime has neither `set_root_weights`
nor its old enable/cap storage. The official [migration][migration] removes the
root weight vector; [Root Reborn][root-reborn] describes dividend accumulation
without a target vector and optional coldkey/proxy basket trades. There is no
heartbeat transaction required for the selected passive strategy.

## Additive approval domains

A fresh unsigned launch composition uses these explicit versions:

| Input | Required value |
| --- | --- |
| Bootstrap chain config | `urnetwork-mainnet-bootstrap-chain-config-v4` |
| Root child config | `urnetwork-mainnet-bootstrap-root-config-v2` |
| Root child plan | `urnetwork-mainnet-bootstrap-root-passive-plan-v2` |
| Root service | `urnetwork-mainnet-root-passive-service-v1` |
| Embedded observation policy | `urnetwork-mainnet-root-observer-policy-v2` |
| Observation storage profile | `subtensor-root-passive-read-only-v2` |
| Root role implementation | `sn/mainnet/root-passive-service` |
| Root role strategy | `passive_accumulate_in_place` |
| Independent config approval | `urnetwork-mainnet-root-passive-service-approval-v2` |
| Service invocation config | `urnetwork-mainnet-root-passive-runtime-v1` |

The root child still references exact private service bytes by path and SHA-256.
Its `passive_service` contains the embedded policy, exact HTTP(S) `rpc_url`,
`read_retry_seconds` (60–900), private `checkpoint_path`, `maximum_samples`
(1–10,000), `interval_seconds` (1–3,600), and `stall_after_seconds` (120–3,600).
The policy requires the independently selected hotkey, coldkey, UID and nonzero
registration block; full approved genesis/runtime/source/code/metadata identity;
positive minimum root stake; expected delegate take; `accumulate_in_place`;
`observe_existing`; and positive finite native `valid_from_native_block` and
`valid_through_native_block`. All runtime source fields name immutable commit
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`. Observation does not change existing
automatic, current or pending delegation.

The root role has explicit netuid 0, its own identity/generation and independent
Ed25519 config approver. Its `action_approval_public_key_ed25519` is empty.
The approval binds deployment, complete root plan hash and complete service
config hash. Its signing message is the approval schema plus a NUL byte plus
canonical Go JSON with an empty signature. This is config approval only; no
native key or nonce is present. The two separately approved UR production
configs remain required, and the root hotkey cannot count toward their quorum.

Legacy bootstrap v1/v2/v3, root config v1, `explicit_root_weights`, original
signature domains and retained custody remain unchanged. They cannot acquire
passive authority by adding a field or renaming a strategy. Keep any already
signed legacy action and its liabilities; no conversion or new submission is
implied. Check for any externally held signed commitment before selecting a
fresh launch plan.

## Preparation and operation

Run the existing `bootstrap-chain plan`, then the independently accepted local
`apply`/`resume` workflow. A v4 preparation retains three real journals: chain,
contracts and passive root preparation. It creates no root signing-custody
journal, native signature, transaction or observer checkpoint. Contract and UR
admission paths accept this explicit v4 scope and preserve their own approvals.

The service invocation JSON has `schema`, the same exact `root_config` file
reference, and the complete independently selected `root_validator` role from
the v4 chain config. Review its content hash with:

```sh
mainnet root-passive-service plan --config /private/passive-runtime.json
mainnet root-passive-service run --config /private/passive-runtime.json \
  --accept-runtime-sha256 sha256:<exact-reviewed-runtime-config-digest>
```

Every invocation rereads and verifies all exact input bytes and the independent
service approval before opening the route or checkpoint. It then holds a shared
lock on complete original local preparation; missing/corrupt records or an
interrupted claim are refused, never repaired by the service. The existing
bounded root monitor owns the checkpoint and emits finalized observations.
The approved policy is handed to it in memory, avoiding a pathname reopen.
There are no signer, submission, signature import or cadence override flags.

The passive metadata profile checks both that required current read shapes
match and that the retired root call/gates are absent. The observer authenticates
the full runtime tuple and current canonical finalized state, ownership,
registration generation, stake/retention, delegate take, delegation and basket
storage. Any runtime or generation mismatch fails closed. `bootstrap-chain
readiness` observes UR and passive root state at the same finalized hash and
reports the root policy's finite approval window. `ready` remains observation
readiness; `activation_ready` stays false.

No production service has been installed or started by this change. Deployment
must pin the qualified binary and exact approvals, supervise the finite command,
retain its private checkpoint, and verify real observations and alerts. A new
window/config needs a new independent approval and preparation, without deleting
old records. Existing root registration and sufficient approved stake are launch
prerequisites; passive observation does not create them. Actual UR production
admission, source/runtime approval, owned custody, contract installation and
mainnet economic outcomes remain independent launch gates.

[migration]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/migrations/migrate_remove_root_weights.rs
[root-reborn]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/docs/guides/root-reborn.mdx

## Independently approved static host owner

`activate-root-passive` owns one initial passive process start on one approved
Linux host/boot. Its [qualification](evidence/passive-root-host-20261001.md)
binds original v4 preparation, both UR config inspections, exact passive runtime
bytes, the independent passive config signature, and a separate signed host
approval. It does not consume either UR process allowance. No production unit
was installed or started during qualification.

Fresh host approvals must select
`<bootstrap-run-directory>/root-passive-observation/<checkpoint-name>` in the
signed passive service config. Provision this dedicated directory as root-owned,
group `0`, mode `0700`; its only permitted files are the selected checkpoint
and its `.lock`. Existing flat checkpoint approvals keep their original meaning
and can still run through `root-passive-service`; their shared preparation
directory is unsuitable for this sandbox. A new checkpoint path requires new
exact service/config and host approvals. Never rewrite a retained signed plan.

Original configs, approvals and preparation records stay private and root-owned.
The static process uses `User=0` and `Group=0` to read those exact inputs, with
no runtime copies or changed permissions. Its fixed unit has an empty capability
set, `NoNewPrivileges=yes`, `ProtectSystem=strict`, private devices, protected
kernel/control-group settings and one `ReadWritePaths` entry: the dedicated
checkpoint directory. The controller rejects authority/custody aliases, symlinks,
hard links and unrelated checkpoint files. It verifies the loaded manager's
sandbox, exact command, empty hooks/environment, dependencies and cgroup.
The Linux host and system manager remain trusted; these checks do not attest
host integrity independently. See the [systemd 255 execution specification][systemd-exec].

The independently supplied Ed25519 public key verifies schema
`urnetwork-mainnet-root-passive-host-v1`. Sign the literal domain
`urnetwork-mainnet-root-passive-host-approval-v1`, a NUL byte, and canonical Go
JSON of the complete envelope with `signature_ed25519` empty. The signed `plan`
contains:

| Field | Exact approved value |
| --- | --- |
| `bootstrap_config`, `bootstrap_plan_hash` | Original v4 file reference and preparation content hash |
| `runtime_config`, `root_plan_hash` | Exact private invocation file reference and passive child hash |
| `unit_file` | `/etc/systemd/system/sn-mainnet-root-passive.service` and hash of the fixed rendered unit |
| `mainnet_binary`, `systemctl` | Protected absolute file paths and exact binary hashes |
| `machine_id`, `boot_id` | Current selected host and boot; a reboot does not renew authority |
| `checkpoint_directory` | The dedicated private child described above |
| `required_mounts` | Sorted, unique mount-unit names; every mount and `system.slice` must already be active without jobs |
| `state_path` | Separate permanent private host journal, outside worker-writable state |
| `valid_from`, `expires_at` | Explicit start/install window of at most 24 hours |
| `maximum_operations` | 1–128 counted install, admission, start and recovery operations |
| `command_timeout_seconds` | 1–60 seconds per joined manager command |
| `maximum_sample_age_seconds` | 1–600 seconds for the current pre-start policy observation |
| `install_static_unit`, `authorize_one_passive_start` | Both explicitly `true` |

The exact renderer is `rootPassiveHostPlan.render` in
[root_passive_host_authority.go](root_passive_host_authority.go). The sole command
is `mainnet root-passive-service run --config=<runtime path>
--accept-runtime-sha256=<exact digest>`, with `Restart=no` and no install target.
There is no enable, restart, stop, native signer or transaction operation.

Every operation requires `--approval`, `--accept-approval-hash` and the independent
`--independent-public-key`. The sequence is:

1. `claim` retains the complete signed envelope, approver key and original
   preparation seals under a permanent marker. It neither repairs preparation
   nor opens a start allowance when a marker already exists.
2. `install` records intent, creates only the exact unit without overwriting
   another file, reloads the system manager, and proves the loaded profile and
   unused stopped generation. A crash after publication can reconcile those
   same bytes on a later `install`.
3. `admit` performs a fresh finalized read of the approved root policy. It
   records read-only prerequisites and never starts a process.
4. `start --execute-approved-start` repeats fresh policy/host admission, durably
   consumes the single start, rechecks custody/window/source, issues one bounded
   start, and retains the exact acknowledged invocation ID, PID and monotonic
   start time. Missing acknowledgment remains consumed even when a process exists.
5. `resume` observes only that retained invocation, including its stopped finite
   completion. Repeated recovery spends the same operation allowance and never
   starts another process. A changed boot/generation or uncertain start requires
   explicit external reconciliation with all original records retained.
6. `status` reads historical journal facts only; `current_process_running` stays
   false. Successful current `start`/`resume` may report manager liveness, while
   `root_service_ready`, `activation_ready`, native signing and network submission
   remain false. Inspect real monitor observations/checkpoint age and alerts
   separately to establish ongoing health.

Expiry and exhausted operation allowance close this host owner; `status` remains
historical. The finite service still owns its original signed sample/block
window. A new run or policy needs independently reviewed successor authority and
explicit disposition of the prior invocation; removing state or changing paths
is not recovery.

Mainnet remains gated on independently approved runtime/source provenance and
its recorded reproducibility exception, an actual existing root seat and stake,
the fresh v4 passive configuration/signature, this exact independent host
signature and acceptance, the qualified successor binary, protected host paths,
owned RPC access and observed live service health. Both UR current-admission
approvals, original contract anchor/history, signer exclusion and operator
readiness retain their separate gates. The release fenced at SN `6c801a25` does
not include this later host implementation and must not be relabeled as covering it.

[systemd-exec]: https://github.com/systemd/systemd/blob/v255/man/systemd.exec.xml
