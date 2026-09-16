The original compiler captures are retained unchanged. Their source-after
fences recorded only an untracked literal `$capture/` output directory caused
by the capture command quoting error. This recovery removed exactly that
untracked directory through `git clean -fd -- '$capture'` in each disposable
isolated source after all compiler owners joined. No tracked source file,
dependency, selector, or test body was changed or run.
