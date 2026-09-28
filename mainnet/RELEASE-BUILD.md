# Offline executable build candidate

`sn-mainnet release-build --config FILE` builds selected real Go entry packages
twice and retains a role/platform manifest plus the existing release inventory.
It uses current clean physical source repositories. It does not accept old
executable paths or inherit a previous candidate's artifact hashes.

The initial profile is `linux-amd64-static-v1`:

| Role | Module and package | Artifact scope |
| --- | --- | --- |
| `sn-miner` | SN `./cli/miner` | Miner executable |
| `sn-validator` | SN `./cli/validator` | Validator executable |
| `sn-mainnet` | SN `./mainnet` | Mainnet operator tooling |
| `server-api` | server `./cli/api` | API executable |
| `server-taskworker` | server `./cli/taskworker` | Production taskworker executable |
| `server-strecovery` | server `./cli/strecovery` | Offline recovery tooling |

`strecovery` is not a running subnet operator. The production taskworker CLI
uses the production workload default; building it does not select the underlying
service's opt-in `subnet-operator` workload. Runtime profile selection, service
configuration, database schema, credentials and running identity remain separate
gates. Nothing here starts a service or executes a generated artifact.

Prepare an ordinary source lock from the clean graph, then supply its exact
file digest and an output directory that does not yet exist:

```json
{
  "schema": "urnetwork-mainnet-release-build-config-v1",
  "candidate_id": "reviewed-source-candidate",
  "source_sn_dir": "/absolute/physical/source/sn",
  "source_lock": {
    "path": "/absolute/evidence/source-lock.json",
    "sha256": "sha256:<exact-file-digest>"
  },
  "output_dir": "/absolute/evidence/new-candidate",
  "profile": "linux-amd64-static-v1",
  "roles": [
    "sn-miner", "sn-validator", "sn-mainnet",
    "server-api", "server-taskworker", "server-strecovery"
  ]
}
```

The strict parser rejects unknown/duplicate fields, repeated or unknown roles,
relative paths, symlink aliases, existing output, and output inside any locked
source repository. The output parent must already exist. Source and replacement
worktrees must be clean and physical, including nested SCTP and SN npipe modules.
A server replacement resolving a module to a different physical path than the
SN lock is refused even when both repositories are clean.

The builder fixes `GOWORK=off`, `GOTOOLCHAIN=local`, `GOENV=off`,
`GOFLAGS=-mod=readonly`, `CGO_ENABLED=0`, `GOOS=linux`, `GOARCH=amd64`,
`GOAMD64=v1`, and `GOMAXPROCS=2`. Downloads and Go authentication are disabled
with `GOPROXY=off`, `GOSUMDB=off` and `GOAUTH=off`. Required modules must already
exist in the cache. Only process lookup, home and explicit Go cache paths are
inherited. Temporary compiler files stay inside the new output tree.

Each build uses `-p=2 -trimpath -buildvcs=true -buildmode=exe -compiler=gc`.
SN binaries use `-ldflags '-w -s'`; miner and validator additionally stamp
`main.Version=0.0.0-mainnet-candidate.<full-SN-commit>`. Miner and the three server
roles use `GOEXPERIMENT=greenteagc`; validator and mainnet use the default setting.
The recovery CLI explicitly uses the server profile because it has no separate
production image recipe. Exact flags and settings are retained for every role.

Before and after each compile, the builder captures `go list -m -json all`,
`go list -deps -json PACKAGE`, and actual selected source files. Go, assembler,
headers, prebuilt objects and `go:embed` inputs are included. Local compiler
inputs must be tracked by a locked repository; ignored generated files cannot
borrow a clean Git identity. Content hashes catch changes hidden from Git by
`assume-unchanged`. The selected standard-library and downloaded package source
files are also retained. `go mod verify` checks the downloaded module cache
before and after the complete sequence. The Go driver, compiler, linker,
assembler, packer and running builder executable are hashed and rechecked.

Output ELF headers must describe a static amd64 executable with no dynamic
loader. Go build info must match the role's package, module, Go version,
platform, cgo/trimpath settings, experiment and clean Git revision. Linked
dependency identities must agree with the actual compiler module graph. Both
passes must produce identical bytes and build info. A final pass rechecks
compiler inputs and both output copies before publication.

Successful output contains:

- `first/ROLE` and `second/ROLE`: both executable copies.
- `inputs/ROLE.json`: compiler-resolved module and package-file records.
- `source-lock.json`: the copied and verified source lock.
- `release-inventory-config.json` and `release-inventory.json`: the compatible
  partial inventory selecting first-pass executables, compiler input records,
  source lock and Go tool binaries.
- `build-manifest.json`: the final manifest, written after the inventory
  succeeds. The same manifest is printed to stdout.

The manifest binds the exact inventory file digest. Its `content_hash` is
SHA-256 over `urnetwork-mainnet-release-build-v1`, a NUL byte, and canonical Go
JSON with an empty `content_hash`. Paths are part of this identity; relocation
requires a fresh record. No attestation or approval is implied.

Two invocations on one builder using a shared Go cache establish only the
recorded `same_builder_byte_equality`. They do not establish independent,
cache-free or hermetic reproducibility. `independent_rebuild_verified`,
`release_complete` and `deployment_approved` remain false. The inventory retains
false `provenance_proven` and `present_unvalidated` semantics. Other platforms,
proxy/image roles, contracts, migrations, configuration, policy, security
qualification, registry publication and deployed readback are uncovered. Checks
are sequential observations, not an atomic snapshot or a defense against a
hostile builder.

The total timeout defaults to 90 minutes and may be set up to three hours; each
compile is limited to 15 minutes. Metadata commands have shorter deadlines,
32 MiB output bounds and joined process groups. Each compiler closure is limited
to 8,192 packages/modules, 65,536 files and 2 GiB; a single file is bounded by
the existing 512 MiB inventory limit. At most six roles run in two serial passes.
Failed output remains for diagnosis and cannot be resumed or overwritten; use
a new directory for a corrected run. Exit 2 means command syntax refusal,
exit 1 means build/admission/output failure, and exit 0 means only that this
local candidate was captured.
