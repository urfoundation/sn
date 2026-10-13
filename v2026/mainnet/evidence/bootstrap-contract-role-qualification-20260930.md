# Contract-to-validator declaration qualification

Sol medium qualified the corrected frozen source with 40 positive root
executions passing normally and under race detection. All three source-only
causal controls reached their assigned assertions in both modes. Astra max
independently audited the sealed raw streams and exact source/dependency fences.
These results qualify declaration admission only, not installation or activation.

The candidate adds `bootstrap-chain contract-role-plan`, a separate offline
admission for two independently signed UR producer configs. Both configs can
pass the original mutual-agreement checks while sharing a foreign coordinator,
vault or policy identifier. The new command reconstructs the approved eight
action projections and requires the predicted coordinator proxy, approved vault
and exact atomic proxy initializer's policy identifier. Direct implementation
substitution is refused even when both producer signatures authorize it.

The sealed report retains the original preparation and contract-plan hashes,
artifact pin, implementation/proxy/vault/evidence addresses, immutable evidence
domain and both signed producer declarations. It changes no original v1/v2/v3
preparation hash, custody journal or recovery scope. Its declaration check does
not advance a durable chain phase.

## Exact source and author checks

- Source: `f4470d0d3a81d0d36b09bd0cc52a6387774a58b0`.
- Tree: `1c53d55f59d4125465a90797e58ae86036d88040`.
- Base: `8cfabc6eeb0321d18a6fef8dd025ee84dc7f6528`.
- Frozen owner checkout:
  `/home/by/urnetwork/sn-bootstrap-contract-role-corrected-owner-20260930`.
- Author `gofmt`, compile-only `go test -c -p 1 ./mainnet` and
  `go vet -p 1 ./mainnet` passed. The same compile-only/vet checks passed for
  each of the three source-only control mutations in a separate checkout;
  every mutation was restored afterward.
- Astra max owns implementation, debugging and fixes. Sol medium owns all
  behavioral normal/race qualification and causal controls. The author has
  not run behavioral tests for this candidate.

The handoff directory is
`/tmp/bootstrap-contract-role-corrected-handoff-20260930`.
Its eleven-file `PAYLOAD.SHA256SUMS` has SHA-256
`e8f7fea13a4a4127984ed40b2543839bbb5d22b713e0d349e767647e7e714aab`.
Its nineteen-file `SHA256SUMS` adds the isolated author-control checks and has
SHA-256 `3dd6d7bc31415b981d2c22abdc1a9fc804d20bc035a85d8637b4d466e4ec6e9a`.
`TEST-PLAN.json` supplies exact selectors; `CONTROLS.json` supplies the patch
hashes and assigned assertions. `SOURCE-FENCE.json` binds all changed files,
Go 1.26.6, unchanged `go.mod`/`go.sum`, the module graph, all six clean local
replacement commits/trees and the unchanged Safe storage proof SDK oracle.

## Independent evidence

The raw evidence directory is
`/home/by/urnetwork/temp/bootstrap-contract-role-corrected-validation-f4470d0d`.
Its `manifest.json` has SHA-256
`9fa07f0c9ef0819ef493ec5efa93e353bae9d8e1f480d6d3b35941a7242381f6`.
Its 17-file `SHA256SUMS` has SHA-256
`2596e54b32874e724a116c44819f2e19b7b5a9f3bb3f9a2566569d558960bc86`;
every entry passed verification. The independent read-only audit is
`/tmp/bootstrap-contract-role-final-seal-audit.json`, SHA-256
`3ab3890baa037d58ef24f01bcc78d19a9fa6ee4500f30fca8218cd507978b3b9`.

| Group | Roots | Normal | Race |
| --- | ---: | --- | --- |
| Contract-role command and rejection boundaries | 8 | PASS, 17.743 s | PASS, 133.286 s |
| Original bootstrap custody, signed roles and approved graph | 12 | PASS, 31.408 s | PASS, 205.635 s |

The positive census is 40 root executions. Commands used `GOPROXY=off`,
`GOMAXPROCS=2`, `-p 1`, `-count=1` and `-v`, with ten-minute normal and
twenty-minute race package timeouts. These are scoped commands, not a claim
that the complete mainnet package has passed.

The success fixture uses real release artifacts and signed synthetic producer
configs without executing deployment transactions. It checks exact sealed
output, all ungranted authority fields, unchanged original custody before and
after offline `apply`/`resume`, and zero chain reads. Separate roots exercise
mutually signed proxy, vault and policy substitution, a signed policy hash
without its preimage, a signed successor with a stale parent, missing evidence
scope, cancellation, output failure and invented online/submission/custody flags.

| Causal mutation | Required existing assertion | Normal | Race |
| --- | --- | --- | --- |
| `proxy_binding` | `mutually signed implementation substituted for coordinator proxy` | CAUSAL | CAUSAL |
| `vault_binding` | `mutually signed foreign vault bypassed contract-role binding` | CAUSAL | CAUSAL |
| `policy_binding` | `mutually signed foreign policy bypassed contract-role binding` | CAUSAL | CAUSAL |

Each mutation bypasses one production comparison without changing test bytes.
All six executions produced their assigned assertion, selected-root and package
failure, exit one, and no build, panic, timeout or race confounder. The positive
streams likewise contain their exact top-level run/pass census and no failure,
skip or race report.

## Preserved preliminary failure

The original `43dcd01f0e1fc284209947bc643536c68734ea21` focused normal run
failed five of six roots during their common fixture setup; only the missing
evidence-graph root passed. Its raw log remains at
`/home/by/urnetwork/temp/bootstrap-contract-role-validation-43dcd01f/focused-normal.log`,
SHA-256 `e4f4f67157467dd91c57c02f4c779823effae5d9d8fbed7653b15aef378eba46`.
The twelve preliminary adjacent roots passed normal and race. Those streams
remain separate diagnostics and do not qualify the corrected source.

The generic EVM fixture's placeholder policy hash had no full policy preimage.
Replacing the producer's configured hash with it correctly failed the existing
producer loader. The corrected fixture reapproves the synthetic proxy
initializer with the real producer policy digest before custody. Its shared
reapproval helper derives both configured and signed successor parent hashes
from actual policy contents before signing the full configuration. The foreign
policy test now changes actual policy contents. Two new roots preserve the
existing refusal of independently signed hash/preimage and stale-parent
mismatches. All production bytes and original control patch bytes are unchanged
from `43dcd01f`; this correction changes only the new test file.

## Integration and remaining authority

The integration fence retains every non-Markdown byte from the frozen source,
including all Go, test, module and fixture bytes. Prior qualification receipts
remain immutable. Documentation composition does not broaden the scoped test
claim or alter the original tested source identity. Integration uses a
fast-forward of the shared branch and a non-force push.

This report supplies declarations only. Canonical installation and anchor
receipts, each declared deployment scan floor, current code/getters, the
anchored evidence journal, operator/role evidence and both UR plus root service
activations remain separate P0 work. The declaration report is not accepted as
production service authority. Public Safe submission remains closed; the
original complete-history statement remains retained and unproven. Current-only
policy approval, a separately qualified release route and live chain identity
remain independent gates. No live RPC, real signer or mainnet action is part
of this qualification.
