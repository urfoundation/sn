# Measurement client custody

The no-config `validator run` path measures trails only. Its primary `.validator.jwt`
authentication cannot create a client identity or generate a replacement key. Only the
first provisioning of a pristine directory can, as described below. The runner cannot
register a native validator or write weights.
The schema-3 production `--config` dispatch and its approved operator scope remain
separate. Earlier nonproduction release-config compatibility is outside this
migration. Existing ephemeral tunnel clients created by `TunnelTransport` through
`NewApiMultiClientGenerator` also remain outside this primary-identity migration;
their allocating API path is unchanged.

The measurement scope contains the exact versioned registration endpoint, the
public key derived from the retained `.validator.key`, the role
`validator-measurement-v1` and the slot `direct`. Deployment, chain, genesis,
subnet, validator and operator fields are empty or zero. The description is
`validator measurement`; build version, optional epoch RPC/contract and path names
do not select another client. Historical provider and production-validator scope
bytes remain unchanged.

An existing legacy key and `.validator.jwt` need one explicit
`--adopt-legacy-measurement-key` assertion that the seed is the original key. JWT
claims do not prove that link. This first adoption writes a public key marker and
the existing-client identity marker before refresh. Missing, malformed, changed
or unproven keys require recovery. The assertion cannot repair missing markers
beside versioned history, and never grants creation authority. A fresh install is
created only by first provisioning (below). A legacy allocation whose reply was lost
must recover its original identity; this command cannot infer it.

A separately recovered original measurement registration can replay its exact
durable operation, including after a lost reply. The command always uses
`allowCreate=false`, retains the first-send anchor and client/device binding, and
never calls the legacy allocating endpoint for that primary identity. The test fixtures explicitly prepare
synthetic retained operations; they do not demonstrate an upstream live
provisioning authorization. Unknown missing credentials and missing original
requests remain closed. Confirmed rejection remains sticky across login-token
rotation.

One exclusive key owner retains the state directory descriptor until API,
transport, trail, epoch and stats workers have joined. Registration and refresh
effects open relative to that owner and reject a renamed or replaced directory.
The global login token is needed only for original-operation replay; its separate
private directory is opened lazily and retained across retries. Ordinary existing
client refresh requires no bootstrap token, and completed primary authentication
releases the bootstrap lock while retaining the key owner. Local custody binds local ownership;
the API remains responsible for signature, membership and revocation authority.

Refresh validates the original stable principal, roles and client/device before
persisting and publishing to measurement transports. Malformed current-generation
SDK notices cancel the owner; stale notices cannot cancel a replacement login.
Callbacks request cancellation without joining their own API worker. The runner
returns after cleanup and final stats save, including on errors; it contains no
inner `os.Exit` and loads no EVM or native signing key. Optional EVM access remains
cancellable epoch reads only.

Qualification must cover the isolated candidate's exact source and module graph.
Compiler/vet results alone do not establish behavior. Deterministic tests use
private synthetic files, local HTTP, real custody/refresh and worker barriers.
The full local lifecycle fixture reaches seed discovery and joined shutdown; it
does not reach ephemeral tunnel-client creation or prove successful live trail
completion or mainnet readiness.

Existing proof and stats storage keeps its separate path ownership and existing
stats-load error behavior. This primary-identity migration does not establish a
new recovery guarantee for corrupted or replaced measurement history.

## First provisioning (`--auto-register`)

The approved provisioning workflow is `--auto-register`. It covers
`validator run --all-operators --auto-register --hotkey_seed_file=<path>` and
`--observe-unpinned-operators`, which provisions with the config's hotkey.
Each operator directory is provisioned through
`clientauth.ProvisionValidatorMeasurementClientKey`. The workflow covers only the first
provisioning of a pristine directory. It never recovers, adopts or replaces an existing
identity, and pinned production operators never use it.

- **Pristine only.**
  - The directory may hold no `.validator*` entry except the key store's own lock,
    `.validator.key.registration.lock`. A refused run of a pristine directory leaves that
    lock behind.
  - Any other directory is refused with
    `measurement_provisioning_requires_pristine_directory` and left unchanged. The run path
    then applies its usual rules to it.
- **Durable order.** Each file is published with the store's write discipline: temporary
  file, fsync, rename, directory fsync. The order is:
  1. the provisioning marker `.validator.key.provisioning`, holding the same public-key
     line as the identity marker;
  2. the identity marker `.validator.key.identity`;
  3. the random nonzero seed `.validator.key`, last.

  A durable key therefore proves that both markers are durable. A crash before the key
  leaves a keyless remnant. Every path refuses it as before, and nothing was sent, so an
  operator may delete it.
- **One creation.** The provisioning owner may make exactly one direct registration
  through `ProvisionClientJwt`. It uses the run path's scope and description, so
  afterwards the unchanged runner opens the key with `OpenValidatorMeasurementClientKey`
  and refreshes that client.
  - The registration guard admits measurement creation only from this owner's explicit
    call, and only in a directory whose provisioning marker names the scope's client key
    and that holds no provisioned marker.
  - The run path, the owner's `LoadOrRegisterClientJwt` and the package-level
    `LoadOrRegisterClientJwt` keep `allowCreate=false`.
- **Resumption.** A restart that finds exactly the key and both matching markers resumes
  that one creation. Nothing else may be present: no provisioned marker, no registration
  record, no `.validator.jwt`, first-send anchor, existing-client marker or rejection, and
  no other `.validator*` file. Only the client credential's lock is allowed.
- **After the first send.** Once a registration record exists, the ordinary replay and
  refresh rules apply: a lost reply replays through the run path, and rejection stays
  sticky. Every other combination of remnants keeps its existing refusal code.
- **Marker lifecycle: provisioning, then provisioned.** The provisioning marker is the
  creation authority. It is spent durably once the registration completes, that is, once
  the original operation is retained and the client JWT persisted.
  - Whichever path completes the registration renames `.validator.key.provisioning` to
    `.validator.key.provisioned`. That may be `ProvisionClientJwt`, or the run path's
    replay or refresh after a crash between registration and rename.
  - The rename keeps the same bytes and happens inside the owned directory descriptor,
    followed by a directory sync.
  - The provisioned marker is provenance only and authorizes nothing. A directory that
    holds it is never pristine or resumable, so losing the registration, its anchor and
    `.validator.jwt` afterwards still creates no second client.
  - A leftover provisioning marker beside a provisioned marker for the same key is
    removed. A provisioning marker that names another key is a custody fault that the
    run path reports.
