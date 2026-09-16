# Release lock for incremental startup recovery

The qualified PF-01 preparation-index fix and PF-02 operator workload profile
are composed in SN `dc90e4cff6895860cd5b9590dbffc965ce63efed`, with exact server
`6752a8df246c0ee7e1c5a38cbd26b1e849b702ca`. Their affected qualification is
retained in the [preparation-fix bundle](../FINAL-2-preparation-fixes-20260916/README.md).

The read-only source renderer completed at **07:39:57 UTC on September 16**.
Its body, outer and observed tool join all exited zero. The original
[request](preview/REQUEST.json), [command](preview/command.sh), timestamps and
[result](preview/result.status) are retained. All watched deployment-state,
executable and existing-lock hashes remained unchanged. No chain transaction
was submitted.

The renderer used the existing admitted `0fd7ffc0` executable; its renderer
and configuration code are unchanged by these fixes. It observed clean physical
SN/server checkouts containing the qualified changes. This preview does not
claim that the old executable can launch the revised deployment.

The candidate changes exactly two lock fields:

| Field | New value |
| --- | --- |
| `repositories.sn_go_source_hash` | `sha256:2ac35d3ed59965e06ba68c5c756139c7b77c43bc11d0c5ec3ba93dc5f0823c20` |
| `repositories.server_go_source_hash` | `sha256:ee3f1007b006fe19ce21e1927f070fd29a2879bb839efdeed7eaf4d86f418a2f` |

Runtime 460, contract artifacts, all other repositories, configuration,
interfaces, dependencies and infrastructure lock fields are unchanged. Root
reviewed the complete two-field diff and installed byte-identical
[candidate YAML](preview/stdout.yml) in `deploy/testnet/release.lock.yml`.
Its SHA-256 is
`bf417189d4c0a62f8116606f84f9c5509b3afe2dab611429c9b781998b3198fc`;
the prior lock was
`d703afe97a7f3299b3d4e43b7feed0d90e330b161384a9f5d8d33541266945b2`.

Publication of the complete source/evidence/lock batch and a matching CLI build
must precede native plan revision and startup. No successful deployment,
campaign or soak is claimed by this source-only receipt. The later build and
native commands retain their own results.

Verify copied bytes with `sha256sum -c SHA256SUMS` from this directory. Captured
absolute paths are provenance for the original invocation, not portable commands
to overwrite its evidence. The files contain source and public state hashes;
private configurations and signer material are excluded.
