# Private server authentication fixture

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

Use the returned workspace with the server's isolated service adapter. Do not
use it as a production vault. Independent runs get independent credentials.
A failed run retains any created private directory and reports its path for
diagnosis; it does not silently delete it or retry with ambient credentials.

The twelve deterministic tests use real signing/decoding and physical files.
Normal and race qualification, including original-source causal controls, are
retained in the finalization handoff; run `go test ./scripts/server-fixture`
for a local check.
