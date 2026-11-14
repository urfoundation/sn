# Provider client registration qualification

The provider primary-client registration change is independently qualified and
integrated. Qualification used synthetic offline fixtures under ordinary umask
0002. No live registration, signer use, chain action or production custody
migration has occurred.

The qualified source is SN `60934fb069a812b3f0105df24339a88fc45619db`, tree
`7565ae59a7a24fb448f9d555d934d92a993eae4f`, on shared base
`de480675e20c9010a27d585e37b61165b617d9db`. Its
[implementation handoff](/mnt/data/sn-testnet/qualification/miner-client-registration-20260929/frozen-609/HANDOFF.md)
has SHA256 `17ed81b095d9fc37313f9ae173bd694507c5927ebca9548b9973eac2a985f18e`;
the 105-file manifest has SHA256
`416981b9795f45550ef6e9224ae55ce361f1e6bfdf9e20ea38d0d068c37faefd`.
The child corrects exactly three test files from preserved implementation
`e32becc989a906e88c4a93bc72f0d67f1e779cf6`; every production and source module
byte remains unchanged. The qualified source changes 17 files from the shared
base; integration adds only the three scoped mainnet documentation files above
that source. E32 remains unqualified, with its original evidence preserved.

The independent [Sol receipt](/mnt/data/sn-testnet/qualification/miner-client-registration-sol-20260929/frozen-609/SOL-RESULT.md)
has SHA256 `4531bf252a95d1d2e973a20cc235be6f19eef85fddcddbc536d0a28c200cfc57`;
its 182-file raw manifest has SHA256
`f3318ddaabfe89bbd4474ee4aa0e0b29ca62210a9fc132b9d38604137e538809`.
The independent [integration audit](/mnt/data/sn-testnet/qualification/miner-client-registration-integration-audit-20260929/FINAL-AUDIT.json)
has SHA256 `be24fbee967e7780181b0736ee5dfb5b4280384ed1d23605aab300410bb167bc`.
It rechecks raw events, root/package terminals, process exits, exact causal
assertions and mutant diffs, the original e32 census, and physical source/module
custody against the sealed manifests.

The selected physical graph uses SDK
`516521fb16da46c9f4bff0b58221e1941694f616`, Connect
`b163f9dd9ac374942fe97331f26631248a9c1f81`, and server
`80c0e1b7d9fb48ee6f929bca8158ac925b114fe0`. The handoff pins eight clean Git
roots and ten selected local module entries through an external readonly
modfile. SN's `go.mod` and `go.sum` are unchanged. SDK revision 42241118
does not contain the required versioned registration/integrity API and cannot
replace SDK516 in this qualification.

`provide` and `auth-provide` formerly called the legacy allocating endpoint
before persisting the shared provider key. A committed lost reply or interrupted
credential write could therefore create another client on restart. The change
retains the seed, public identity marker, original request and first-send anchor
before the versioned POST, then durably binds the server identity before exposing
its JWT. Restart and typed transient retries reuse that exact operation.

The opaque provider scope contains the exact SDK-returned registration endpoint,
retained public key, closed `provider-v1` role, and direct or full proxy-slot hash.
Proxy credentials and order do not select identity. Duplicate effective slots or
credential paths are rejected before workers start. Chain, deployment, operator
and validator fields remain absent; prior validator scope bytes are preserved.
The existing versioned server protocol requires no new wire format here.

New work requires `--allow-client-registration`. The mutually exclusive
`--adopt-legacy-provider-key` supports a first-upgrade refresh only when the
operator asserts that the unmarked seed is the original retained key. JWT
claims cannot authenticate that historical link. Neither flag repairs missing
key/marker custody with retained versioned history or authorizes replacement of
a lost/revoked client. Complete deletion of all local custody, or all history
for one slot, remains locally indistinguishable from a fresh installation or
new slot; no recovery proof is claimed.

The global provider key owner spans all worker, device and API joins. Registration
attempts, bootstrap/rejection reads and required refresh/logout writes use its
physical directory descriptors. A replaced pathname cannot select new operation
custody or receive renewed credentials. Closed startup diagnostics explain
refusal without paths, tokens, proxy passwords or raw errors, through the
existing bounded final drain. The public daemon's prior completion exit policy
is retained.

All selected roots pass in both normal and race modes:

| Package | Selected roots per mode | Package census | New roots |
| --- | ---: | ---: | ---: |
| clientauth | 25 | 25 | 9 |
| miner | 79 | 307 | 15 |
| validator | 10 | 1992 | 0 |
| Total | 114 | 2324 | 24 |

Each of the six package streams has every selected root RUN/PASS once, package
PASS and exit zero, with no missing, extra, failed or skipped roots: 114 selected
roots per mode, 228 executions. The validator race stream completed in 1227.309
seconds. This is the full clientauth package and selected miner/validator
adjacency, not full affected-package coverage. All 17 causal variants reach
their specified semantic assertion, root/package FAIL and exit one in both
modes: 34 valid causal executions. No build, fixture, panic, race-detector or
global-timeout failure is counted as causal evidence. Author compilation and
vet pass for all three packages; all 17 variants compile. The independent
source/module checks match before and after behavior. No behavioral test body
was executed by the author.

The original e32 capture is retained under
`/mnt/data/sn-testnet/qualification/miner-client-registration-sol-20260929/frozen-e32`.
Its sealed [anomaly receipt](/mnt/data/sn-testnet/qualification/miner-client-registration-sol-20260929/frozen-e32/SOL-ANOMALY.md)
has SHA256 `05e57bda561145ad61000580221289c8d78db3ab3d118b43ec6ce71d3684db24`;
its manifest has SHA256
`829642da5a728d82d07d0ea4f5eb0190dc9140e052c375f2e1d724efba05af7b`.
E32 is not qualified. Ordinary-umask clientauth normal passed 22/25 roots;
three key fixtures reached the intended refusal of group-writable mode 0775.
Private-umask 077 diagnostic runs passed clientauth 25/25 and validator 10/10
in both modes. Miner passed 77/79 in both modes, with package failure from the
two physical-refresh-count assumptions. The original validator race process
finished without restart after 1232.591 seconds. No e32 causal controls ran,
and none of these diagnostic passes are relabeled as child qualification.

The pinned SDK uses parallel GET transport; the failed e32 streams did not
report its actual counter values. The child explicitly makes fixture parents
private and distinguishes exact client/handoff/allocation identity from physical
refresh attempts. It requires identical original slot records/key, exact
authenticated identities, no additional allocation and an unchanged HTTP-count
baseline after the typed lost-legacy refusal. The ordinary-umask child normal
stream now records three retained clients and 12 physical restart refresh GETs
(four per client), while allocation remains three. Legacy adoption records four
GETs, and the later lost-JWT refusal leaves that baseline unchanged. These
observations support the fixture correction without weakening custody rejection.
The independently sealed child normal/race result covers these corrected
fixtures in the ordinary environment and preserves the failed parent streams.

The daemon fixtures use the actual startup and SDK HTTP path but stop at
authenticated handoff before full serving-device construction. Refresh/logout
callbacks and the actual public CLI refusal are separate fixtures. The synthetic
allocation ledger does not newly qualify production PostgreSQL allocation;
the [existing actual API/database result](registration-production-api-qualification-20260928.md)
remains a separate prerequisite. Qualification is Linux-specific; other supported
platforms are not newly covered.

At this provider qualification, the active no-config measurement validator still
called the legacy loader. Its durable primary identity now has a separately
[qualified existing-custody-only migration](validator-measurement-client-registration-candidate-20260929.md);
that later result does not expand this provider receipt or repair the unchanged
ephemeral tunnel-client allocator. Configured schema-3
production operator authentication has its own qualified owner; the legacy
release and simulation callers are not migrated here. This provider component supplies no
processed-key, traffic/proof, validator membership or serving readiness; no
runtime identity, native finality, actual fee, contract/Safe authority, economic
acceptance or live approval. API/migration deployment and operator-owned custody,
all chain/contract/economic launch gates, and deployed independent monitoring
remain required. MG-04 and PH-13 are not closed by this component.
