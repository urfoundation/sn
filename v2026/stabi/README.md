# stabi — STSubnet Go bindings

Generated Go bindings (abigen v2, go-ethereum v1.16.7) for the STSubnet contract. The
source of truth is the Foundry project in `evm/`: `forge build` compiles
`evm/src/STSubnet.sol`, the ABI is exported with
`jq .abi evm/out/STSubnet.sol/STSubnet.json > evm/abi/STSubnet.abi.json`, and
`./generate.sh` (also wired as `go generate ./stabi/...`) re-runs
`abigen --v2 --abi ../evm/abi/STSubnet.abi.json --pkg stabi --type STSubnet --out stsubnet.go`
and copies the same ABI json to `stctl/st_abi.json`, which is generated output — never edit
`stsubnet.go` or `stctl/st_abi.json` by hand. Bindings are ABI-only (no bytecode);
deployment lives in forge. Entry points: `NewSTSubnet()` plus
`(*STSubnet).Instance(backend, addr)` returning a `bind.BoundContract`, with per-method
`Pack*/TryPack*/Unpack*` wrappers, `Unpack*Event(log)` for events, and `Unpack*Error(raw)`
for custom errors.
