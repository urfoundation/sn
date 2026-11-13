# stabi — release contract Go bindings

Generated Go bindings (abigen v2, go-ethereum v1.17.0) for the release contracts. The
source of truth is the Foundry project in `evm/`; `./generate.sh` (also wired as
`go generate ./stabi/...`) exports the artifact ABIs and regenerates the bindings.
It resolves abigen from `ABIGEN`, `PATH`, `GOBIN`, or the first `GOPATH/bin`, in
that order, and accepts only `abigen version 1.17.0-stable` from a source build or
`abigen version 1.17.0-stable-0cf3d3ba` from the pinned release. Preflight preserves
the observed version line; only the structured release lock records `1.17.0`.
Install the source build with
`go install github.com/ethereum/go-ethereum/cmd/abigen@v1.17.0`. The script also
copies the legacy STSubnet ABI to `stctl/st_abi.json`; generated bindings and ABIs
must never be edited by hand. Bindings are ABI-only (no bytecode); deployment
lives in forge. Entry points: `NewSTSubnet()` plus
`(*STSubnet).Instance(backend, addr)` returning a `bind.BoundContract`, with per-method
`Pack*/TryPack*/Unpack*` wrappers, `Unpack*Event(log)` for events, and `Unpack*Error(raw)`
for custom errors.

The settlement v2 contracts, `STSettlement2`, `STSettlementVault2`,
`STClaimBatcher` and the capture the flag carry contract `STCtfCarry`, share
struct types and are bound in one combined pass, `stsettlement2.go`. The
`STSettlement2` binding is the proxy's surface: it is generated from its newest
implementation, `STSettlement2Ctf` (`CAPTURETHEFLAG.md` §3.1), which keeps every
phase A and phase C function, event and error and adds `registerCtfPool`,
`commitCtfCensus`, `ctfCensus`, `ctfSettlementAt` and `ctfPoolId`. Calls the
proxy's live implementation lacks revert until its upgrade executes.

Use `./generate.sh --preflight` to resolve and validate the pinned generator
without reading artifacts or regenerating files. Release gates run this check
before their long test phases, then use `./generate.sh --check` for freshness.
