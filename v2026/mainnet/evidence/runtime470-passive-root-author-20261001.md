# Runtime470 and passive root author handoff

Source candidate: `e45f81a7087d55de637173cbf003fad364e57e02`, based on
`7777667fc77e7ba97cc94f20bb6d50a4c543e78f`. Isolated checkout:
`/mnt/data/sn-testnet/mainnet-runtime470-astra-20261001/sn`.
This author receipt is not independent qualification or mainnet approval.

The additive [bootstrap v4 passive root service](../ROOT-PASSIVE-SERVICE.md)
retains two UR roles and a distinct netuid-0 role. It requires exact private
configuration bytes, a new independent config-signature domain, current runtime
and registration identity, finite observation scope and completed original local
preparation. It never constructs or imports native signatures. Old signed v3
explicit-root-weight domains, bytes and custody are retained unchanged.

The [runtime470 review](../../docs/spec/runtime-470-audit.md) records official
tag/source/release identity, exact on-chain artifact equality, executed metadata
and 87 unchanged CRv4 interfaces plus 20 mainnet consumer/negative checks.
The source rebuild completed with a precise reproducibility exception: 22
`i64.const` operands in Wasmi hash-table seed initialization differ; all other
instructions/functions/sections match. Exact rebuild equality failed. Accepting
the exact official artifact with that exception requires independent review;
the local rebuilt artifact is not a replacement deployment candidate.

Author test streams are under
`/mnt/data/sn-testnet/mainnet-runtime470-astra-20261001/evidence/`:

| Stream | Result and exact scope |
| --- | --- |
| `passive-final-normal-r2.log` | Final source: 30 package-PASS roots, including all 14 new passive roots and 16 existing successor roots. |
| `validator-source-normal.log`, `validator-source-race.log` | Final validator source: 30 package-PASS roots each, exact runtime/source binding, unchanged proposal rows and refusal coverage. |
| `passive-final-race.log` | 12 package-PASS new roots before the final passive root-seal projection. |
| `mainnet-focused-normal.log` | 201 package-PASS legacy/new root and bootstrap roots before that final projection. |
| `mainnet-legacy-race.log` | 39 package-PASS legacy bootstrap signature/domain, restart, root policy/preview/monitor and owner-trim roots before that final projection. |
| `passive-downstream-race.log` | Final source: 18 package-PASS roots, including both new downstream roots and 16 existing successor roots, with the actual public v3 retained-prefix command. |
| `go-vet-final.log` | Final `go vet ./mainnet ./validator` passes. |

The final projection explicitly labels passive root seals and leaves native
root custody/extrinsic hashes empty. Both independent UR admission and contract
successor validation accept the real preparation while rejecting an unlabelled
missing legacy custody hash or invented native liability. Existing wire fields
are unchanged; the new strategy field is omitted for legacy observations.

The tests use synthetic accounts, keys, metadata and local HTTP servers. They
exercise the real public passive CLI and durable checkpoint, composed readiness
at one finalized hash, original marker loss/corruption, repinned forged approval,
schema conversion refusal, checkpoint namespace collisions, absent retired
runtime capabilities, changed code/metadata/window/registration/owner and
downstream root-seal admission. Earlier fixture failures remain retained and are
not counted as successful runs.

Independent Sol review should run the exact frozen source, all 14 new roots
normal/race, the owner-recycle source suite, and focused existing v3 signature,
custody, readiness and successor roots. It should independently reproduce the
official/on-chain Wasm equality, section/function/instruction difference, crate
checksum/feature provenance and metadata consumer checks. Review the final
passive root-seal field and cross-schema approval boundaries specifically.

No public chain transaction, live signing, service installation or shared-tree
edit occurred. Native Sr25519 and owner Ed25519 metadata shapes pass; native
ECDSA remains unqualified by the 64-byte adapter. Independent runtime/genesis/
checkpoint approval, externally held signed commitments, owned custody, the
exact mainnet mapping, existing root stake/registration, real UR production
admission, deployment and economic observations remain open.
