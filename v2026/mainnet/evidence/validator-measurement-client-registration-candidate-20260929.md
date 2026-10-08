# Measurement primary-client registration qualification

The durable primary-client change for the active no-config validator measurement
path is independently qualified and integrated. It preserves the original key
and client identity, refuses unowned primary creation, and joins users before
releasing custody. Qualification uses private synthetic custody and local HTTP;
no live registration, signer use, chain transaction or deployment has occurred.

The qualified source is SN `e33f64d4132bfaee8f913c7e6b0e698c15a15b43`, tree
`2b428776e2f61fe45cac202ead46ebe2b8883d5d`, at
`/home/by/urnetwork/temp/sn-validator-measurement-cli-fixture-20260929/sn`.
It is a four-commit descendant of integrated provider/docs
`db5e1d21740dec3da39407edb379283487021801`, tree
`facc5a6ef65bd68317812b5e6c3100c67a6de5f6`. Exactly seven planned measurement
files and two adjacent miner CLI test files differ from that base; source module
files and the qualified provider registration fixtures are unchanged. Integration
adds only MAINNET, PRELAUNCH and the two component evidence documents above this
exact source; production, test and module blobs remain byte-identical. The
qualified provider component has its
[separate receipt](provider-client-registration-candidate-20260929.md).

The sealed [child handoff](/home/by/urnetwork/temp/sn-validator-measurement-cli-fixture-20260929/frozen-e33f64d4/HANDOFF.md)
has SHA256 `3852390f7264535a459b3500f9f60f997a0a042ad51878f14e31a9464146d41a`;
its verified 84-file manifest is
`d5fdfba12491cdf36fc1426c690215cb06b212f3a04bf552c3d01e5939cfbe15`.
The preserved [parent handoff](/home/by/urnetwork/temp/sn-validator-measurement-registration-20260929/frozen-c3fe0cf2/HANDOFF.md)
has SHA256 `947ce4259114072e998de8ece504f63df4c80816488d6c1f8cb8e216833591b3`;
its 78-file manifest is
`6e9e0b58bfe7b743bb8033beafacd2457aaa0cf1e0db607e35905262e1d72b67`.
That parent's behavioral anomaly is retained below and does not qualify the child.
The fresh physical graph has ten local module entries and eight clean Git roots,
using an external offline readonly modfile. It pins Connect
`b163f9dd9ac374942fe97331f26631248a9c1f81`, SDK
`516521fb16da46c9f4bff0b58221e1941694f616`, and server
`80c0e1b7d9fb48ee6f929bca8158ac925b114fe0` in isolated sibling checkouts.
SN-owned npipe and Connect-owned sctp are explicitly included in that graph.

The independent [Sol receipt](/mnt/data/sn-testnet/qualification/validator-measurement-sol-20260929/frozen-e33f64d4/SOL-RESULT.md)
has SHA256 `5029a302592114e026cda1989da081e894e091393893181e47b692d5ad8bc157`;
its verified 225-file manifest is
`43b8f71e3a9c31e738eb84f8c003feed3932f36e24efcf3413fa2037721d1888`.
The separate [Astra audit](/mnt/data/sn-testnet/qualification/validator-measurement-preaudit-20260929/FINAL-AUDIT.json)
has SHA256 `7279a2d2fd0399671f01a784304099fa86bc0b784f7889cb396722abe0ae3325`.
It independently rechecks all raw root/package terminals, process exits, named
causal assertions, actual patch bytes, source/module graph and original anomaly.
Its [concise receipt](/mnt/data/sn-testnet/qualification/validator-measurement-preaudit-20260929/FINAL-AUDIT.md)
has SHA256 `8738b3edffcd0884e7921e63a498fce4c1567ff812d593a3fe0876122e8a88e1`;
the five-file final manifest is
`77c759d2515fcb7600f6d85e02b6bc97428fe1de49d23adbefcf1600f9e6ec3b`.
The [integration audit](/mnt/data/sn-testnet/qualification/validator-measurement-integration-20260929/INTEGRATION-AUDIT.json),
SHA256 `3e0a4a865eaee484f48e82e9d2a51de50dd3f6bd0c179ac606401ba19a9a4f47`,
separately rechecks these sealed files and exact source before integration.

The old no-config path could allocate a client before retaining its result and
could regenerate a missing measurement key through `LoadIdentity`. A committed
lost reply could therefore leave an unowned original identity. The change
requires the existing nonzero `.validator.key`; it never generates a replacement
key and always uses `allowCreate=false` for durable primary `.validator.jwt`.
The role is `validator-measurement-v1`, the slot is `direct`, and the scope binds
the exact SDK endpoint and retained public key. Deployment, chain, genesis,
subnet, validator and operator authority fields remain empty or zero. Provider
and configured production-validator scope bytes retain their original encoding.

First legacy adoption needs the explicit `--adopt-legacy-measurement-key`
assertion and an existing client JWT. This assertion does not prove the historical
JWT-to-key link or grant new registration authority. Missing key identity beside
versioned history remains a refusal. An existing JWT can refresh without the
global login token; a separately recovered retained registration may replay only
its original request, first-send anchor and identity binding. Unknown lost work
cannot become a fresh primary allocation. The synthetic replay fixtures prepare
their retained records explicitly and supply no upstream live provisioning proof.

One exclusive key owner spans primary API callbacks, transports and measurement
workers. Registration, bootstrap/rejection reads and refresh/logout persistence
remain bound to the appropriate retained physical directory. The separate global
bootstrap directory is opened only when original replay needs it, retained across
unresolved retries, and released after successful authentication. Another state
can then replay its own original operation without acquiring the first state's
still-held key. Refresh validates stable identity before persistence/publication;
confirmed rejection is sticky, and stale SDK integrity notices cannot cancel a
replacement login.

The runner returns through cleanup without inner `os.Exit` and joins API,
transport, trail, epoch and stats users before releasing key custody. Optional
chain access remains cancellable epoch reads; it grants no native registration or
weight-write path. Schema-3 production `--config` dispatch remains separate.
The implementation's production bytes match the independently reviewed
`b9a9be01361fe3bb6f69a496069f54f942762047`; later children strengthen only test
barriers, parser fixtures and limitation prose. The [static review](/mnt/data/sn-testnet/qualification/validator-measurement-registration-review-20260929/REVIEW.md)
has SHA256 `169832f1899e625fff1d7c03ff779e5a7dc34a1bec666209c52244c2f7e0167b`.
Its four-file manifest is
`1b48530100bb8f178a41ab0ac98e4993a9ec85db16ef5e18eed4ea2378812fa1`.
Static review is separate from the independent behavioral result.

Exact-source `go test -c` for clientauth, miner and validator, three-package
`go vet`, and all nineteen isolated mutation compilations pass on the child.
The author executed no behavioral test. Independent normal/race qualification
passes the following exact selection:

| Package | Focused roots | Adjacent roots | Selected roots per mode |
| --- | ---: | ---: | ---: |
| clientauth | 12 | 7 | 19 |
| miner | 0 | 4 | 4 |
| validator | 12 | 2 | 14 |
| Total | 24 | 13 | 37 |

All 37 selected roots have RUN/PASS in each mode: 74 root executions across
twelve package PASS/exit-zero streams, with no missing, extra, failed or skipped
roots. This is selected coverage, not full-package coverage. All 19 causal
variants compile and reach their exact semantic assertion, root/package FAIL
and exit one in both modes: 38 valid executions. No build, fixture, panic,
race-detector or timeout failure is counted as causal evidence. The adjacent
production-validator roots use separate package time budgets. The ten local
module entries across eight physical Git roots remain clean, and the resolved
module stream is byte-identical before and after qualification.

The parent `c3fe0cf2` is explicitly unqualified. Its [sealed anomaly receipt](/mnt/data/sn-testnet/qualification/validator-measurement-sol-20260929/frozen-c3fe0cf2/SOL-ANOMALY.md)
has SHA256 `0430e4c7375215652f2da470a4ebb63708046e27798c99bee581afae76b01eda`;
the independently verified **62-file** manifest is
`a6e533d3e1e2e1c0b5ce73d23cbeaead8a1202ef72365af9c01ce775767eaabb`.
In both modes, nine focused validator root bodies passed before the CLI fixture
called package-level `docopt.ParseArgs` on deliberate invalid arguments. Its
default handler printed usage and exited the test process, so the CLI root had
no terminal and two following roots were unrun in that package. Those two roots
passed separately. Overall 34/35 selected roots had body PASS per mode, but only
25/35 had package-PASS streams. No parent controls ran. The child changes exactly
three test files to use local non-exiting parsers, checks typed grammar refusals,
and adds the two repaired miner roots plus a real CLI-grammar mutation control.
Production CLI exit behavior and global parser state are unchanged. Parent
results remain separate from the new child qualification.

The fixtures use private synthetic custody, actual local HTTP and explicit
worker/refresh barriers. The shutdown fixture waits for actual startup refresh
persistence before testing key lifetime. It then waits for the periodic stats
worker to stop and removes any earlier snapshot while the trail join remains
held, so only the real final save can satisfy its assertion. This avoids a slow
run's earlier snapshot masking an omitted final save. The correction changes
only the test, not production shutdown semantics. The complete local lifecycle
fixture reaches seed discovery and joined shutdown; it does not complete a live
trail or demonstrate prospective proof readiness.

Existing `TunnelTransport` still allocates ephemeral tunnel clients through
`NewApiMultiClientGenerator`; that path is outside this durable primary-client
repair. Earlier nonproduction release-config compatibility, proof/statistics
pathname ownership and existing stats-load recovery behavior are also unchanged.
Primary registration custody does not establish recovery of corrupted or moved
measurement history. Qualification does not widen these limits.

The API/migration rollout, actual operator key adoption and original identity
recovery, processed-key and trail/proof readiness remain operational gates. Owned
Snow identity and approved mainnet genesis/runtime, current chain and Safe
authority, contract installation, protected native roles, actual fee/custody
authority, observed 10/90 outcomes and independently deployed monitoring also
remain open. The broader SDK result remains 543/618 roots backed by package-PASS
race streams, with 75 pending; the separate MG-08 adjacent scope remains 150/270
roots with 120 unrun. This slice supplies no additional broad-package coverage
or live authority and does not close MG-04, MG-08 or PH-13.
