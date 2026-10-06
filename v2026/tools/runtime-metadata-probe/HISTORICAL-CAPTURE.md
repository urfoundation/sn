The `runtime-historical-capture` worker builds a complete historical replay job
from a retained read-only parent trie. `capture_historical_on_backend` borrows
an archive node's `state.as_trie_backend()` and uses the pinned SDK's
`prove_execution_on_trie_backend`. It never applies the resulting overlay.
The standalone worker accepts a retained directory of raw Blake2-256-addressed
trie nodes as an interchange format. It is not a RocksDB or ParityDB parser.

The public Go command owns the exact executable and request pins, a single
60–900 second deadline (300 seconds by default), and joined bounded process
pipes:

```
sn-mainnet capture-historical-execution \
  --engine /private/runtime-historical-capture \
  --engine-sha256 sha256:EXACT_EXECUTABLE_DIGEST \
  --request /private/request.json \
  --request-sha256 sha256:EXACT_REQUEST_DIGEST \
  --nodes /private/parent-trie-nodes
```

For a native v2 profile, the same command can fill missing nodes from a selected
archive RPC. Use an existing private cache covered by the usual durable-volume
declaration. This mode is unsigned qualification; it does not construct or
enroll a signed production producer:

```
sn-mainnet capture-historical-execution \
  --engine /private/runtime-historical-capture \
  --engine-sha256 sha256:EXACT_EXECUTABLE_DIGEST \
  --request /private/request.json \
  --request-sha256 sha256:EXACT_REQUEST_DIGEST \
  --rpc https://SELECTED_ARCHIVE_RPC \
  --expected-chain SELECTED_NATIVE_CHAIN \
  --expected-genesis 0xEXACT_GENESIS_HASH \
  --expected-evm-chain-id SELECTED_EVM_CHAIN_ID \
  --cache-dir /private/capture-cache \
  --durable-volumes /private/durable-volumes.json \
  --durable-volumes-sha256 sha256:EXACT_DECLARATION_DIGEST \
  --budget 900s
```

`--nodes` and archive mode are mutually exclusive. Before starting the worker,
archive mode re-reads and compares the complete original parent/child/body,
code, any declared metadata pin, and execution-state version with the input. Explicit
principal queries stay in that exact unsigned request. One total deadline
includes admission, RPC reads, missing-node refills, child join and closing
selected-finality checks. The broker uses only `state_getReadProof` and, when
needed, `state_getChildReadProof` at the exact parent; claimed RPC values or page
boundaries do not establish completeness. Missing proof paths remain failures.

The cache defaults to 1 GiB and 475,476 entries. `--cache-max-bytes` and
`--cache-max-entries` select finite explicit ceilings within the existing
producer-file profile; growth never deletes evidence or changes a request.
An exclusive owner reserves the complete native boundary before writes.
Content-addressed files retain the original RPC observation, exact request,
raw nodes and successful `jobs/<job-sha256>.json`. The stdout report still
uses the original capture grammar. Files from an incomplete attempt are not
completed jobs and remain subject to the same capacity and custody checks.

For a cache on the operator's local disk, explicitly add `--owner-local-cache`
and supply its separately scoped `urnetwork-owner-local-volumes-v2` declaration.
This uses the existing owner-local descriptor, identity, capacity and lease
checks. It does not relabel an owner-local declaration as daemon storage or
change the signed production producer's filesystem admission. The default
archive mode continues to require the ordinary daemon-volume declaration.

The request uses schema `urnetwork-historical-execution-capture-v1`. It pins
the SCALE parent and child headers and their hashes, the complete SCALE
extrinsic body, both original runtime-code digests, execution state version,
and an optional original-Wasm observation profile. The runtime code comes
from the parent state. The collector retains code/heap proof paths, executes
the whole original block, materializes write paths, and then invokes the
separate strict proof replay. A partial `state_getReadProof` response is never
accepted merely because its supplied keys were read successfully.

The report retains `job_json` as the exact JSON string whose digest the strict
replay verified. Extract that string without re-encoding its contents to use
it with `verify-historical-execution`. The report also carries the request
digest, actual backend read counts, and the complete replay result. Only a
complete successful capture and replay can emit the envelope. Cancellation,
missing nodes, changed identities, unimplemented hosts and any resource
refusal emit no proof fact.

Raw nodes are read through a retained directory descriptor. Each node must be
a unique regular file named by the lowercase Blake2-256 of its bytes; payload
length and metadata are checked before and after a bounded read. Node count,
individual size, retained proof bytes, cumulative backend I/O, input/output
bytes and Wasm work remain separate finite bounds. A partial or oversized
capture must be retried from the original request with a separately reviewed
resource change; it must not skip state or fabricate zero fees.

This capture does not authorize a runtime, source profile or finalized block.
`runtime_admitted`, `native_fee_withdrawal_refund_observed` and
`production_selection` remain false; the top-level native debit remains null.
Original-callsite observations are candidates, including the distinction
between an absent refund and an actually observed zero. Actual chain use
still requires a complete retained backend or exact-parent archive proofs,
correspondence to the original deployed code, and independent parent/child
finality admission. Complete fee callsites are a separate requirement when
fee authority is selected; a generic metadata pin does not select that
authority. A prior runtime review grants no authority to another deployed
runtime. The deterministic command fixtures are synthetic and do not assert
that these external admissions have happened.
