# Prepare owner action custody

Request inspection precedes custody preparation. `owner-signing inspect`,
`owner-recycle inspect-request` and their `ledger-plan` modes read only the
explicit portable public inputs. They require neither Snow access nor a durable
declaration. Actual signing requires the owner's independently prepared local
custody. Host reserve, export, import, submission and reconciliation require the
separate host declaration.

The implemented physical preparation profile is Linux. Use the reviewed binary,
filesystem and owner SDK bundle for that platform; these commands do not qualify
a different owner platform, physical Ledger or firmware.

## Exact fresh preparation request

Create an independently reviewed `urnetwork-storage-preparation-request-v1`
request for each new custody root. Its `purpose` is `fresh`; retained or unknown
signing state must continue through original custody rather than being described
as fresh. The request must supply all of:

- `scope`: `owner-local` for the device journal, `daemon` for host action custody.
- The observed `mount_path`, `filesystem_uuid` and `filesystem_type`, with
  explicit `min_available_bytes` and `min_available_inodes`.
- The exact private `root_path`, external `marker_path`, `lease_path`,
  `declaration_path`, `control_path` and `staging_directory`. The root is an
  existing empty directory with mode 0700. Keep portable requests, tools,
  approvals, output reports and the preparation inputs outside this fresh root.
- `former_writer_fence`, a `{ "path": ..., "sha256": ... }` reference to a
  private `urnetwork-storage-preparation-fence-v1` file. It binds `root_path`,
  the observed `root_inode`, `purpose: "fresh"`, `former_writers_stopped: true`,
  `no_previous_owner_state: true`, and the actual external `evidence` for those
  assertions. A local lock cannot establish remote writer cessation.
- Finite `limits`: `max_entries`, `max_bytes`, `max_depth`,
  `max_owner_attributes`, `max_owner_attribute_bytes` and `max_plan_bytes`.
- The fixed `owners` entry below. Its capacity is an exact existing format,
  independent of the selected physical free-space floor or planning limits.

For an owner device journal named `device-state.json`, the complete `owners`
value is:

```json
[
  {
    "kind": "mainnet-owner-signing",
    "relative_path": ".",
    "purpose": "fresh",
    "inputs": {
      "schema": "urnetwork-snapshot-preparation-v1",
      "name": "device-state.json",
      "maximum_bytes": 16384
    }
  }
]
```

Select the format that matches the command and journal being prepared:

| Custody | Preparation command | Scope | Kind | Name | Maximum bytes |
| --- | --- | --- | --- | --- | ---: |
| Owner device, trim or recycle | `storage-owner-prepare` | `owner-local` | `mainnet-owner-signing` | Exact basename of `--owner-state` | 16384 |
| Host recycle action | `storage-prepare` | `daemon` | `mainnet-owner-recycle` | `owner-recycle-action.json` | 34734080 |
| Host trim action | `storage-prepare` | `daemon` | `mainnet-owner-trim` | `owner-trim-action.json` | 67108864 |

Trim custody is fixed inside the original bootstrap run directory. Include its
empty snapshot marker alongside all required bootstrap owners in that root's
original fresh preparation, before `bootstrap-chain apply`. Do not prepare a
different trim root or run a fresh request over retained bootstrap history. A
retained root lacking this prepared member requires an independently reviewed
complete restore/continuation, not an empty-state replacement.

The device uses a new permanent state path for each independently approved
original request. The host action and owner device paths must remain separate.
Neither preparation selects the owner's account, native nonce, era, action,
route, approval key or production authority.

## Public plan and apply

On the owner's computer, with actual independently reviewed file pins:

```sh
sn-mainnet storage-owner-prepare plan \
  --request /owner/recycle/preparation-request.json \
  --request-sha256 sha256:REVIEWED_PREPARATION_REQUEST \
  > /owner/recycle/preparation-plan.json

sn-mainnet storage-owner-prepare apply \
  --plan /owner/recycle/preparation-plan.json \
  --plan-sha256 sha256:INDEPENDENTLY_ACCEPTED_PREPARATION_PLAN \
  > /owner/recycle/preparation-result.json
```

Review and pin the exact emitted plan before `apply`. Retain the request, plan,
staging bytes and preparation journal. If apply output is lost, repeat the same
accepted plan to recover its original result; do not create a replacement plan.
No existing durable declaration is needed to enter either preparation command.

Use the returned `declaration.path` and `declaration.sha256` as the paired
`--durable-volumes` and `--durable-volumes-sha256` flags on every later signing
command. The `--owner-state` must be the prepared `root_path` joined with the
exact snapshot `name`. Preparation writes the original empty marker and
physical checkpoint; creating an empty directory alone is insufficient.

For fresh host action custody, use the independently reviewed daemon request
and the same `plan`/`apply` sequence under `storage-prepare`. Pass that result's
declaration to the host commands. An owner-local declaration cannot open host
action custody, and a daemon declaration cannot open the owner device store.

Preparation returns `restart_authorized: false`. Original action approval,
owner signature, independent submission approval and canonical finality remain
separate steps in [OWNER-SIGNING.md](OWNER-SIGNING.md) and
[OWNER-RECYCLE-EXECUTION.md](OWNER-RECYCLE-EXECUTION.md).
