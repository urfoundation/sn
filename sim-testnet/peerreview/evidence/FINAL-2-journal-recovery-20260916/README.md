# Journal validation-index recovery receipts

The preceding native resume expired after completing configuration verification
and migrations. Its failed outcome and retained checkpoint are preserved in
[the startup evidence](native-resume/README.md). No soak acceptance is claimed.

The exact 15 selected simulator roots passed in normal and race modes. The
source-specific causal control passed 13 compatibility roots and failed the
two work-bound roots as expected after restoring the original full-scan
comparison.

This is a portable review subset. It retains terminal test events, compiler
and body exits, compiled censuses, selectors, source/binary/selector fences,
and result maps. Binaries, caches, temporary trees, command scripts, and
host-specific configuration probes are omitted.

The deterministic load covers 44,048 synthetic rows: 6,000 actions with seven
stages each plus 2,048 same-action retries. The two work-bound checks assert
no more than four historical comparisons per input row and no more than four
following reopen.

[Source, qualification and syntax-only followup](COMPOSITION-INDEX.md) identify
the exact inputs. The detailed local captures remain at
`/mnt/data/sn-testnet/qualification/journal-validation-index-20260916-r1/terra`.
