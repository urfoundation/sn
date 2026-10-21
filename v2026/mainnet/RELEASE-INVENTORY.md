# Local release candidate inventory

`release-inventory` hashes actual selected files alongside an exact source
lock. It makes no RPC calls, loads no keys and executes no listed artifact.
It emits an **unapproved candidate**, even if every category is populated.

```sh
GOWORK=off sn-mainnet release-inventory --config /secure/ur-mainnet/inventory.json > candidate-inventory.json
```

The strict JSON input has this shape. Replace the incomplete values with real
local paths and the exact source-lock hashes; there are no inferred defaults:

```json
{
  "schema": "urnetwork-mainnet-release-inventory-config-v1",
  "candidate_id": "ur-sn25-candidate",
  "source_sn_dir": "/absolute/clean/candidate/sn",
  "source_lock": {"path": "source-lock.json", "sha256": null},
  "artifacts": [
    {
      "id": "sn-mainnet",
      "category": "executable",
      "path": "/absolute/build/sn-mainnet",
      "source_lock_content_hash": null
    }
  ]
}
```

`source_lock.sha256` is `sha256:` plus the lowercase digest of the exact input
file, including any newline. `source_lock_content_hash` on **each artifact**
must equal the internal source-lock seal. The planner rejects stale per-artifact
bindings when the top-level lock changes. A deliberate new binding causes a
fresh read and a different inventory identity; it does not prove the old binary
was rebuilt from those new sources. Optional `expected_sha256` locks previously
reviewed file bytes and refuses changed output.

The command reuses the source-lock audit before and after initial artifact
collection. Every clean Git repository, local Go replacement, tracked module
file and exact go.mod/go.sum hash must still match. The current inventory
tool/Go version can differ from the tool that originally emitted the source
lock; those differences are not a claim about the candidate's build toolchain.
The retained source lock's `tool_sha256` identifies that **earlier source-lock
binary**, not the new inventory binary. After integration the inventory tool
needs its own clean build and composed source lock; no earlier tool hash is
silently inherited as its identity.
Use `GOWORK=off`, as required by the source-lock audit. The command does not run
a build or download dependencies.

Eight category names are accepted:

| Category | Selected file examples and limits of the claim |
| --- | --- |
| `executable` | Actual SN miner/validator/operator/mainnet, root-service, monitor, server or Connect executables; execute permission is required, but files are not run or parsed. |
| `contract` | Exact Foundry JSON, creation/runtime bytecode or ABI files; selecting one does not establish deployable bytecode, constructor values or complete custody coverage. |
| `config` | Exact service and build configuration; presence does not validate mainnet identities, permissions or safe production settings. |
| `policy` | Exact signed or unsigned policy bytes; this command does not validate signatures or policy semantics. |
| `migration` | Full selected server migration/catalog source and deployment manifests; no database version or writer-cutover safety is inferred. |
| `image-identity` | Retained OCI manifest/inspection file plus explicit `image_reference: "registry.example/image@sha256:..."`; mutable tags are refused. The declared reference and file hash do not prove image availability, provenance or deployment. |
| `dependency` | Exact retained Solidity/library/other build-dependency lock manifests; ignored `evm/lib` contents are outside the Go source lock and still need their own proof. |
| `toolchain` | Compiler/Forge/Go binaries or exact build-tool/settings observation files; no reproducible-build claim is created. |

The real candidate can span a detached `sn` checkout, symlinked pinned
`server`/`connect` siblings, external executable output, and copied Foundry
artifacts. List literal file paths; relative paths resolve against the config
file. There is no directory walk, glob expansion, environment substitution or
URL fetch. This prevents unrelated files from silently changing a release's
scope, but it also means **the operator must enumerate the complete scope**.
Different IDs/categories cannot count the same resolved path or hardlinked
file more than once; parent-directory symlink aliases are checked as well.
For server migration identity, include the relevant `db_migration*.go` and
`monitor/*migration*.go` catalog/implementation set, excluding `*_test.go`, and
the actual deployment manifest. The monitor set includes `signal_migrations.go`,
the `migration_*.go` implementations and the `migrations_*.go` catalogs; renaming
a catalog must not drop it from the release inputs. One file cannot stand for
the entire deployed migration state.

Every file is streamed and hashed twice, including a final exact-byte recheck.
Both passes refuse symlinks at the final path component, special files, empty
files, oversize files and inconsistent size/mode/mtime during reading. Final
rehashing catches same-size edits even if an mtime was restored. No prior
artifact cache or reported hash is accepted instead of reading bytes. Paths,
byte counts, permission modes, hashes and source bindings are retained; file
contents, including configuration secrets, are not printed.

There are hard limits of 256 artifacts, 512 MiB per artifact, 2 GiB total selected
bytes, and a five-minute command deadline. Streaming checks cancellation every
128 KiB buffer read. Config/source-lock input files are bounded to 1 MiB. Unknown
fields, duplicate JSON keys, duplicate IDs, unknown categories and overflow are
rejected. A declared missing file is an input error; omit an unavailable category
to receive explicit `missing` category output. Errors emit no partial inventory.

Output `missing_categories` and per-category counts report presence only.
`release_complete`, `deployment_approved` and `provenance_proven` remain false.
A populated category is `present_unvalidated`; one file per category is not a
complete release. Full role/artifact coverage, exact compiler/library closure,
source-to-Wasm/source-to-bytecode proof, reproducibility, contract semantics,
production qualification and independent approval remain separate work.
Sequential checks detect ordinary drift but are not an atomic filesystem
snapshot; deployment consumers must recheck exact bytes and current authority.

The content seal is SHA256 of `urnetwork-mainnet-release-inventory-v1`, a zero
byte and canonical Go JSON with `content_hash` empty. Ordering is by artifact ID
and a fixed category list. Identical config/source/file bytes produce identical
output. Exit 0 means an inventory was emitted, including partial candidates;
invalid input or capture failure exits 2, and output failure exits 1.

The retained inventory can be supplied to the appropriate `plan` review slot,
where it remains `supplied_unvalidated`. It is not an executable release,
transaction authorization or closure of MG-02.
