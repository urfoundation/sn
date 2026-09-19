# stabi — release contract Go bindings

Generated Go bindings (abigen v2, go-ethereum v1.17.0) for the release contracts. The
source of truth is the Foundry project in `evm/`; `./generate.sh` (also wired as
`go generate ./stabi/...`) exports the artifact ABIs and regenerates the bindings.
It resolves abigen from `ABIGEN`, `PATH`, `GOBIN`, or the first `GOPATH/bin`, in
that order, and accepts only `abigen version 1.17.0-stable`. Install it with
`go install github.com/ethereum/go-ethereum/cmd/abigen@v1.17.0`. The script also
copies the legacy STSubnet ABI to `stctl/st_abi.json`; generated bindings and ABIs
must never be edited by hand. Bindings are ABI-only (no bytecode); deployment
lives in forge. Entry points: `NewSTSubnet()` plus
`(*STSubnet).Instance(backend, addr)` returning a `bind.BoundContract`, with per-method
`Pack*/TryPack*/Unpack*` wrappers, `Unpack*Event(log)` for events, and `Unpack*Error(raw)`
for custom errors.

Use `./generate.sh --preflight` to resolve and validate the pinned generator
without reading artifacts or regenerating files. Release gates run this check
before their long test phases, then use `./generate.sh --check` for freshness.
