# Durable claim producer checkpoint — October 2

Frozen SN `347605fd1d01b5404b969602c05cf3e8c5feef9f`, tree
`04199a9573b1e391062b532cf16dca2b08cfb6db`, passes its bounded author
qualification. Its parent is `17cc30994318564b739c047907e51f1ab0d26bcc`.
The [unaltered author receipt](claim-projection-author-20261002.json) has SHA-256
`5a857f31c7b2e3a43fe09062e9e0b9918888cf3d06f130b1603c7b5f8c9a111b`.
Independent qualification and integration are pending. This is not a release
or deployment verdict.

The producer uses the real unsigned finalized-leaf, exact signed-receipt and
queue-save boundaries. It exposes a bounded public claim projection after
durable acknowledgement. Failed or uncertain saves do not advance its sequence
or digest. An unavailable or closed writer cannot continue reporting an active
view. The projection contains no signing material, RPC credentials or local paths.

Evidence domains remain explicit. API no-claim responses are API assertions.
Configured-RPC leaf/receipt observations remain configured-RPC assertions, with
genesis unverified. A Merkle path checked against the advertised root does not
authenticate native finality or the running contract code. An unsigned leaf cannot invent a
transaction receipt or amount. `Claimed` proves accepted liability in the
observed receipt; `ClaimPaymentDeferred` retains credit, and `ClaimPaid` transfers
aggregate coldkey credit that may span epochs and operators. Payment is never
allocated to the current epoch without that additional ledger. Event selectors
come from the consumed ABI. Invalid, ambiguous or too-small payment events
degrade only the payment observation; the original signed claim result remains.

Optional metadata cannot consume the original 16 MiB queue's operational
capacity. A new oversized observation first yields to the retained observation;
if operational growth still needs room, all optional observations yield. Every
entry, signature and operational field remains intact. True operational capacity
exhaustion still refuses the save. The 64-entry public view is a separate bound:
it retains the oldest unresolved epoch, recent outcomes, full counts and omitted
counts. Sequence changes are acknowledged heartbeats, not proof of settlement
progress. Future observations are omitted rather than published as fresh facts.

| Author scope | Terminal result |
| --- | --- |
| Fifteen new miner and four new protocol tests | 19 pass normally and with race detection; zero skips |
| Eighteen affected custody, signed-reconciliation and queue-capacity tests | 18 pass in both modes; zero skips |
| `go vet ./miner ./protocol` | Both packages pass |
| Prior optional-capacity body | Intended failure on retained metadata blocking operational growth; fresh-observation fallback remains positive, both modes |
| Prior incorrect paid-event selector | Intended aggregate-payment failure; deferred-credit positive, both modes |
| Prior newest-first unresolved selection expression | Intended failure in both modes under the current complete-census validator; this is a narrow expression control, not a claim about the whole old wire format |

The original test-only `56c52df0` over unchanged parent production reproduces
both missing actual projection hooks. Intermediate `b68b0845` retains two passes
and the actual canonical paid-event failure. Their original logs and scopes are
preserved. In total, the final causal batch has three intended failures and two
positive controls per mode. No full miner or protocol package pass is inferred.

The receipt binds 127 source, runner, command, result and evidence files. Its
effective graph has 636 modules and seven physical local Git replacements,
including exact Server `10a8f4d8`, Connect `0a5cda0e` and SDK `5d37`. It does not
inherit later current-server, published-module, provider or fleet composition
qualification. Raw logs and the exact source bundle remain under
`/mnt/data/sn-testnet/mainnet-durable-volume-20261002/claim-progress/evidence`.
Two non-product helper attempts are retained separately: an empty bundle
selection refusal and a sealer syntax error before execution. Neither required
a product test rerun.

The [physical freeze](claim-projection-physical-freeze-20261002.json), SHA-256
`f17244f84c5576ea789badbfd2ab44f1096b520e66cb1b74a83e32ab3af2fd05`,
rehashes all bindings and preserves the original detached source and helpers
read-only. The claim monitor is being implemented in a distinct physical
checkout. Its independent expected member/pool census, semantic deadlines,
restart/freshness handling and public role integration require their own tests.
Contract carry/reserve/payment and native economic observers, fee attribution,
proof-cache/fair-work work and the final composed release remain open. Runtime
472 observation supplies no signing or planning approval beyond its own intake.
