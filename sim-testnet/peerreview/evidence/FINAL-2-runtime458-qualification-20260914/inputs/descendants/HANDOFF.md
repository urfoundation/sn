# Retained CRv4 descendant replay

This is capture metadata and supported verifier reuse only. Candidate df98472,
its four package binaries, the original event stream, all source manifests,
and the original checker refusal stay unchanged. Astra did not run a test,
build, formatter, converter or verifier.

The Python checker is deliberately root-only: its ROOT expression rejects
slash names before event verification (scripts/check-qualification-events.py
lines 22 and 46), and line 115 rejects any undeclared identity. Adding child
rows or an invented flag cannot configure that checker for this stream.

The already built Go tool supports exact descendant declarations and immutable
replay. See sim-testnet/README.md:296 and :308,
scripts/qualification/replay.go:28, config.go:417 and events.go:108.
It validates every declared ancestor, transition and terminal outcome without
filtering descendants, rerunning a binary, reconverting output or rereading
large source manifests. Its source is byte-identical from 907d186 to df98472.

Reuse this existing executable without rebuilding it:

`/home/by/urnetwork/temp/sn-prior-carrier-capacity-correction-20260914/terra-runtime/qualification-typed-prior-907d186/tools/qualification`

SHA256: `2002a97d85c373b3d17f634f56525863e34bad06929a59f838c2eed86fdf1309`.
Its successful build and reconciled record-schema refusal are retained in
that capture's runner-build directory. The later MMDB bound refusal belongs
to its full `run` path; `replay` does not perform that source admission.

Exact replay operands, in order:

1. `replay`
2. Original `normal/body-crv4-retry2/events.json`
3. This directory's `crv4.outcomes.tsv`
4. This directory's zero-byte `empty-literals.tsv`
5. `github.com/urfoundation/sn/crv4`
6. Original `normal/body-crv4-retry2/body.exit`

The original stage is under
`/home/by/urnetwork/temp/sn-runtime458-history-capacity-20260914/terra-runtime/qualification-runtime458-df98472`.
The last operand is the immutable decimal exit-receipt FILE, unlike the
Python checker's numeric --binary-exit operand. Expected replay exit is zero,
with 100 roots passed and 19 subtests passed. Record its actual output and
exit separately, and hash the tool/source/inputs before and after. Retain the
original body0/converter0/checker1 and original completed source/binary fences;
successful replay attributes those original tests and earns no new body run.

The 19 child names are derived from three source tables, not inferred from
observed success. Five checkpoint names at evm_checkpoint_test.go:77 have
spaces rewritten to underscores by Go testing; six schedule and eight foreign
authority names in validator_stake_runtime455_test.go are already literal.
All names and source pointers are in crv4.descendants.sources.tsv. The full
outcomes file adds these PASS declarations to the unchanged 100 frozen roots.
The retained stream independently contains exactly those 19 child terminal
passes; this read-only census is not a substitute for Terra's verifier run.

For the upcoming CRv4 race matrix, preserve the existing 100-root selector,
root-only compiled-list metadata and preflight helper. Use the full 119-row
outcomes ONLY for the Go replay verifier after the captured body. Replacing
the root-only Python event-check command with this existing replay command
in a new static owner is sufficient; preserve source/binary fences, list,
test.v, package CWD, 600/660 limits, and actual body/converter/replay exits.
Do not feed child rows to the binary's top-level -test.list comparison.

Adjacent source review covered every file listed by the four frozen
*.sources.tsv inventories. CRv4 has only those three selected t.Run loops;
miner and validator selected sources have none. The simulator source files
also contain t.Run in four unselected tests (strict YAML, UDP limits, missing
storage batch and subnet activation); no selected simulator root calls those
tests. The selected 41 simulator roots have no descendants. The normal p2/p3
confirmation and three causal selectors select none of the three CRv4 parent
roots above, so their existing root-only inventories remain correct.
