# Read-only upload runtime history qualification

The [upload projection](../VALIDATOR-UPLOAD-RUNTIME.md) closes the schema-3
tuple-only activation-history gap and enforces the signed current runtime's
native block interval. It retains immutable runtime/route/scope values without
retaining a producer capsule or opening custody. Legacy configuration JSON
continues to omit an absent production pin.

Source base: `cbbd7e89f7a47870b4316d2ca4983a5735d4382d`.
Isolated worktree:
`/mnt/data/sn-testnet/worktrees/mg08-upload-runtime-20260927/sn`.
Complete [evidence and commands](/mnt/data/sn-testnet/evidence/mainnet-upload-runtime-20260927/RESULT.md)
include the final commit in `COMMIT.txt`, source manifests, exact captured test
executables, Go build metadata, JSON events and terminal test membership.

With Go 1.26.6, `GOWORK=off`, `GOMAXPROCS=2` and
`TMPDIR=/mnt/data/sn-testnet/evidence/mainnet-upload-runtime-20260927/tmp`:

```sh
go test -json -exec /mnt/data/sn-testnet/evidence/mainnet-upload-runtime-20260927/capture-normal.sh ./validator -run '^TestValidatorUpload' -count=1 -timeout=15m
go test -race -json -exec /mnt/data/sn-testnet/evidence/mainnet-upload-runtime-20260927/capture-race.sh ./validator -run '^TestValidatorUpload' -count=1 -timeout=15m
go vet ./validator
go test -c -o /mnt/data/sn-testnet/evidence/mainnet-upload-runtime-20260927/server-controller.test github.com/urnetwork/server/controller
go test -c -o /mnt/data/sn-testnet/evidence/mainnet-upload-runtime-20260927/server-handlers.test github.com/urnetwork/server/api/handlers
```

Normal and race each passed **27 roots plus 7 existing subtests**, with no
failures, skips or race reports. Package times were **7.627s** and **59.003s**.
Vet passed. Both server consumers compiled; this is not a server database,
remote upload or deployment qualification. The actual ordinary upload signer,
public admission constructor, refresh, eligibility decoding and upload lease
meet in a deterministic local producer/consumer fixture.

Three separate overlays restore former decisions. Each exits 1 at its intended
regression assertion: identity-only history rejects the approved old-runtime
upload, a current-tuple observer accepts block 201 outside its signed interval,
and `omitempty` on the value reference adds an absent field to legacy JSON.
Corrected cases also cover current permit loss, changed pins/config/deployment,
network and route substitution, expired current authority canceling a lease,
unapproved historical artifacts and interrupted loading. New tests are top-level
roots with synthetic identities; no live key or network service is used.

The final source manifest SHA-256 is
`21d189e5f892181e162aa20b4b9c15c6e30a8a1be388aa16d12b30b872e50682`.
Normal executable SHA-256:
`d7c234fa83da1f0ca1740ac25210d0e0992bb8dede6e8170902bda111a3f1a70`.
Race executable SHA-256:
`6e2e0ac6d337ce17ac9625dba8ce475b5a9a0119b3722e5132c978dd1db2c78a`.

An earlier 26-root qualification is retained under `pre-omitzero/`; the final
run includes the additional wire-compatibility regression. No Go changes
followed the final test captures. Server and connect sources were clean at
`0633780cb5e97d29be423adfb19a8748a28e4895` and
`358cefaef9b058cdd06ba5e9c4feeadef64ae1fb`, respectively.

Actual mainnet approvals, live owned-node/archive trust, remote staging delivery
and activation remain external gates. Original economic/config authority
migration is separate from this runtime-only projection. Cached request
freshness retains its existing bounds between observations; expiry is checked
at each observation, not by adding request-time RPCs. This is staging admission,
not measurement, settlement or weight authority. No live signing, submission,
deployment, merge or push was performed.
