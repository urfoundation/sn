# Versioned registration request grammar qualification

Server candidate `736d7b8ffed9a00682fb345e75c398b7973d4672`, based on
`da4621fe9ffdc627bdf40aa8bbc7f78e56485a33`, passes its focused request-decoding
qualification. Astra authored the change; Terra executed the normal and race
bodies. This result does not close the underlying registration transaction,
combined SDK/validator, migration or deployment gates.

The decoder requires the five exact request tags before normal Go struct
decoding. It rejects case and Unicode-fold aliases, duplicate decoded names,
unknown fields, null/non-string/compound values and trailing data without
changing the receiver. Escaped spellings of the canonical tag characters
remain valid. The change does not alter legacy allocation or database behavior.

| Scope | Normal | Race | Maintained result |
| --- | --- | --- | --- |
| `TestNetworkClientRegistrationRejectsUnknownWireIdentity` | Pass; 0.045 s | Pass; 1.201 s | 4/4 stages passed |
| Exact old-parser control | Intended failure; 0.037 s | Intended failure; 0.186 s | 4/4 stages passed |

Both captures record `source_unchanged=true`; all four bodies were joined.
The control, `002fea93a324a67a136618ebd508e50b854c0a13`, restores the exact old
production parser while retaining the corrected test. Both modes reach
`versioned registration accepted noncanonical request grammar` and exit 1.
The candidate bodies exit 0. Author compile-only/model vet and both control
compile/metadata checks passed. This pure JSON scope uses no database or live
endpoint and does not warrant repeating the full model suite.

Raw plans, commands, source manifests, module graphs, outputs and reports are in
`/mnt/data/sn-testnet/evidence/server-registration-request-grammar-20260928`.
Root narrowed each author plan from two workers to one to share capacity with
the independent database and Connect checks; no source, outcome or assertion
changed. The original author metadata refusal for a missing service-helper
manifest remains retained; it occurred before body execution.

| Report | SHA-256 |
| --- | --- |
| `terra-positive/report.json` | `ad366e5d62314f554312d233d638ccb9cc4e275e8f0448fc617831dc099e22a2` |
| `terra-control/report.json` | `b02ecf371065d6eb31ed3dda752bfd9f1ea47cb559eb988c0d5579b816b1544d` |

The component used the original registration dependency graph: SN `9da4213d`,
Connect `358cefae`, SDK `42241118`, proxy `6204ae7d`, glog `892ade4a`,
goidenticons `325750b3` and userwireguard `85fb1ca4`. Full revisions and hashes
are retained in `source-fences.json` and the content manifests. The maintained
runner also declares its separate SN `615a7675` and server `4468a696` script
owners. Its binary SHA-256 is
`cf73edc6abe2ddf42c7dbe5aa3840bd12d3093a6349ea7904e0b19cb3ba70368`.

The newer combined registration graph uses cumulative Connect `bcf9b324` and
SDK `31f9234` and has its own qualification. Do not substitute that graph into
this receipt or infer full release acceptance from a parser-only result.
The causal control is evidence only and must never be deployed.
