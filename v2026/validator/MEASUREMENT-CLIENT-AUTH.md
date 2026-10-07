# Measurement client custody

The no-config `validator run` path measures trails only. Its primary `.validator.jwt`
authentication cannot create a client identity or generate a replacement key. The
runner cannot register a native validator or write weights.
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
beside versioned history, and never grants creation authority. A fresh install or
a legacy allocation whose reply was lost must recover its original identity or
use a separately approved provisioning workflow; this command cannot infer it.

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
