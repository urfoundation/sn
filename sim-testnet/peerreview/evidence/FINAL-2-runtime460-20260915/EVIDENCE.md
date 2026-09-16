# Runtime 460 artifact and compatibility evidence

This bundle supplements the unchanged README.md and its retained plan/doctor
refusals. It contains existing observations and closed probe receipts; packaging
ran no RPC, probe, test, build, or native operation. All chain observations came
from the owned LAN node: **independent_rpc=false**. It establishes artifact
provenance and the reviewed compatibility basis, not a running soak, campaign
acceptance, new transaction finalization, or unchanged economics.

The pinned native observation is block **8014242**,
`0x6ba8842851af6d0d53ca97f867d5075c5d2a4e4580a60300e3d1b975e1d0966d`.
`lan/` preserves exact finalized-head, header, genesis, runtime-version and
code-hash requests/responses, plus the original large-byte requests and artifact
identity. The tuple is node-subtensor **460 / transaction 1 / state 1**, metadata
format **14**. Large code and metadata responses are omitted and hash-identified
in omitted-artifacts.json.

The authoritative [GitHub artifact record](https://api.github.com/repos/RaoFoundation/subtensor/actions/artifacts/10420673699)
identifies runtime-460, [workflow 35029090314](https://github.com/RaoFoundation/subtensor/actions/runs/35029090314),
source commit [8d5f20ec1a5e5d90295d43046dacdefc54aaed06](https://github.com/RaoFoundation/subtensor/tree/8d5f20ec1a5e5d90295d43046dacdefc54aaed06),
and ZIP SHA256 `4c00445deb37516aa9bde60a87d65a461721a864ab342a88a5ffed72e4068cca`.
The retained unauthenticated archive request returned401; the retained mirror
ZIP matched that authoritative digest. The exact srtool receipt records v0.18.3,
Rust1.89.0 and the production profile. Its compressed Wasm identity matches the
LAN artifact: 2525524 bytes, SHA256
`12b9affec176cbb79c7e5db253d3d0e47f4cb575ce4501ef10de6578afbb817f`.

The reviewed 459-to-460 comparison reports one metadata-byte change at position
267231, the spec-version constant; both metadata artifacts are336358 bytes.
Call, storage and event layouts are unchanged. The upstream changes retire stale
share rows when a share-pool denominator transitions either to or from zero,
and allow deposits to reopen pools with shares but no value. These accounting
fixes change behavior; they are not a claim of economic equivalence. The
admission patch retains the original activation and exact historical runtime
identities, including458/459, while460 receives its own exact artifact binding.
The extracted upstream summary identifies all changed files and retains the
three production patches and generated-binding version changes. Its complete
original comparison response hash is explicit.

The existing Terra offline probe ran23:07:06–23:07:53 UTC and was joined23:08:41.
Probe, outer and launcher-join exits are0; preflight, unchanged-input and final
fences aretrue. `offline-probe/` contains the raw command, output, stderr,
terminal ownership, hashes and original capture manifest. That original manifest
SHA256 is `8864ae0f66179e0107e47470f3d721691f3c4eacee6e7b072efe0736331b8429`.
It retains original local paths; this bundle's top-level SHA256SUMS uses portable
relative paths.

For portable inputs, retrieve the exact CI archive using its artifact/workflow
links and digest, or use the recorded source commit and srtool production
inputs. The accompanying qualified SN source provides
`miner/testdata/runtime460-metadata.scale.gz.base64` (base64, then gzip decoding),
`tools/runtime-metadata-probe`, and its locked SDK revision
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`. `source/portable-inputs.sha256` binds the
fixture and existing probe source; the copied source manifests bind the reviewed
upstream paths. `offline-probe/command.txt` gives the exact probe operands after
placing the matching Wasm at runtime460.code.wasm. No new verifier is included.

Doctor hard checks and the upstream delta summary are explicitly labeled
extractions. Other copied receipts remain original bytes. The retained-input
before/after manifests and compare.exit0 record preservation of the README and
both pre-existing refusal directories.
