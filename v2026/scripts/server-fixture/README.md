# Private server suite fixture

Run this Go tool from the SN repository to prepare the authentication resources
needed by isolated server tests. Pass an existing physical mode0700 output
parent and the exact physical server checkout used by the test build:

```sh
go run ./scripts/server-fixture --parent /absolute/private/test-parent --server /absolute/server-checkout
```

The command prints a public JSON report containing the new workspace path,
server input path and signing-key fingerprint. It creates a fresh JWT signing
key and account password pepper; secrets stay in mode0600 files beneath the
new mode0700 workspace. It never reads the host vault/config, modifies server
source, starts services, or creates a testnet wallet.

The original command is authentication-only; its `fixture.json` reports only
that boundary, not admission to the complete server suite. Do not use it as a
production vault. Independent runs get independent credentials.
A failed run retains any created private directory and reports its path for
diagnosis; it does not silently delete it or retry with ambient credentials.

For the complete portable suite, the private service owner supplies the two
distinct daemon-assigned loopback endpoints explicitly:

```sh
go run ./scripts/server-fixture --suite --parent /absolute/private/test-parent --server /absolute/server-checkout --postgres-authority 127.0.0.1:35431 --redis-authority 127.0.0.1:36371 --path-only
```

The illustrated ports are synthetic, not defaults. The gate's existing service
adapter passes its actual private endpoints, enforces its existing finite
startup deadline, and publishes `environment.sh` only after the tool succeeds.
Both complete gates run the tool tests in normal and race mode inside their
existing isolation phase; the full suite-resource guard remains unchanged.

Suite mode admits the exact checked-in resource manifest before writing any
output. It creates dedicated platform and OAuth signing keys, proxy/WireGuard
and observed-egress keys, valid synthetic tls pairs, and typed local resources.
The existing server script's tls aliases determine filenames only: certificate
subjects and names remain `fixture.example`. No source/live certificate, wallet,
account, endpoint or host vault is imported. The only copied configuration
bytes are the frozen local database/Redis pool settings. External-service
credentials remain disabled; empty optional catalogs do not claim real vendor
or product-data coverage. The proxy host is synthetic, while the actual proxy
harness independently owns its listeners.

Tests cover physical ownership, exact manifest drift, endpoint refusal,
synthetic certificates and the actual private adapter. Server child regressions
load the generated keys through the real JWT, OAuth, proxy, WireGuard and proof
settings consumers, including expiration and changed-key refusals. No fixture
test opens a production service or grants release qualification. Run
`go test ./scripts/server-fixture` for the generator checks; the complete gates
also run the actual server adapter roots in both modes.
