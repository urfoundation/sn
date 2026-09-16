The first compiler invocation for each package/mode completed its Go build with
exit 0, but the source-after fence detected an untracked literal `$capture/`
output directory caused solely by capture-script quoting. Those original
invocations remain preserved as non-admitted outer-126 receipts and no body
ran from them. The exact untracked paths were cleaned only after all owners
joined; their recorded artifact hashes are preserved. The corrected retry used
explicit absolute private output paths, retained unchanged sources,
dependencies and selectors, and reproduced all four binary hashes exactly.

Positive bodies ran the exact 11-root selection once in normal and race modes.
Each causal source ran the same selection once normally and produced its one
pinned failure with the other ten roots passing. No other tests were run.
