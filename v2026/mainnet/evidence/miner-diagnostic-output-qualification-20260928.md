# Miner diagnostic output qualification

The miner output and shutdown correction is integrated and component qualified.
Astra authored `1b671769ddf801117ef0e197ff97e4bb2cd59b8d` and
`dc84ec47dfb2393f03d1941422cee65a0cb9ce44`; their SN integration commits are
`e01aca0d` and `ea2b0495`. The integrated miner files match the frozen candidate
byte-for-byte. Terra executed all normal, race and causal-control bodies.

The actual provide owner now has four bounded diagnostic domains, a joined
output owner, and a status listener that closes admission and joins every
admitted HTTP handler before output closes. Real token/rejection file effects
retain their cancellation policy and original causes. Panic recovery preserves
cleanup causes. Optional status counters describe local output delivery;
`status: "ok"` remains process liveness. See the
[source scope and rollout contract](miner-diagnostic-output-source-20260928.md).

| Scope | Normal | Race | Maintained result |
| --- | --- | --- | --- |
| 18 affected miner roots | 18 passed; 0.994 s | 18 passed; 3.600 s | Positive capture 8/8 stages |
| Actual unchanged Warp status reader | One passed; 0.012 s | One passed; 1.040 s | Included in positive capture |
| A: synchronous callback output | Intended failure; 45.073 s | Intended failure; 45.306 s | 4/4 stages |
| B: omitted output-owner join | Intended failure; 0.059 s | Intended failure; 0.254 s | 4/4 stages |
| C: raw formatting and lost cancellation | Two intended failures; 0.115 s | Two intended failures; 0.249 s | 4/4 stages |

Every capture reports unchanged source, exact terminal membership and joined
processes. Positive bodies exited 0; the four control roots exited 1 at their
declared assertions. Candidate/reader/control compile-only checks, candidate
and reader vet, module/package inspection and maintained metadata admission
passed. The complete Terra batch exited 0. No new alert rule was introduced.

The physical full-stdout test invokes the actual provide run owner with owned
synthetic state and endpoints; finite CLI parsing and home proxy discovery are
outside that scope. It proves required callback completion before stdout is
drained. The HTTP shutdown root holds a real admitted loopback request until
network cancellation, then checks handler and owner closure. An unchanged
Warp `sampleStatusVersions` reader fixture verifies additive JSON compatibility.
Its separate test-only commit is
`d3c690a38b283087ae89bc7ac53c4ce261d837f3`, parent `7498864c`.

Raw evidence is in
`/mnt/data/sn-testnet/evidence/mainnet-miner-output-20260928`. The handoff,
selected roots, exact control literals, content manifests and actual Go graphs
are retained there. Root narrowed only jobs from two to one in separately
named plans to share capacity with ongoing registration/model checks.

| Report | SHA-256 |
| --- | --- |
| `terra-positive/report.json` | `a0d4d314d2a612dcd5f5fb24a47c0b226318ee6b11ee8831af7a0433cea3731b` |
| `terra-control-a/report.json` | `c40eba927691a5d02f4f6d0269a42287b29d65b7c7dab087830be9c86aae3f39` |
| `terra-control-b/report.json` | `b3979f5992d36390144dde04278ba5a8b115aed415bd10d3d63bb29a1ba72781` |
| `terra-control-c/report.json` | `1bb398b679325480f8085e9a1af41132322bf78a3a40a9294fa2417754b2d3fb` |

The component graph uses Connect `c68689c4`, server `4468a696`, SDK `42241118`,
proxy `6204ae7d`, glog `892ade4a`, goidenticons `325750b3` and userwireguard
`85fb1ca4`. The actual physical module graph and complete revisions are in the
handoff. The separately declared maintained runner is SN `615a7675`, binary
SHA-256 `cf73edc6abe2ddf42c7dbe5aa3840bd12d3093a6349ea7904e0b19cb3ba70368`.
This does not qualify a later combined registration dependency graph.

Review found a separate optional cause-classification gap: custom `Unwrap`
methods can run synchronously before an output offer. That follow-up has its
own [qualified immutable successor](diagnostic-cause-isolation-qualification-20260928.md);
these original results are not relabeled as proof against arbitrary error
callbacks. SDK/internal logging, actual consumer
rollout, protocol readiness, delivered alerts, production custody and mainnet
activation also remain separate work. No live deployment or chain action was
performed by this qualification.
