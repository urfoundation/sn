# Capture-local admission refusal

The first setup command stopped before any compiler, list, or test body because it
incorrectly required byte equality between the supplied minimal forward patch and
the output of git diff. The patch intentionally omits generated diff headers,
index lines, and function-context lines, so that byte comparison is not an
identity criterion. The retained admission criterion is the approved exact
six-path whitelist plus forward/reverse apply checks, stable dirty diff, and
full mutant/dependency manifest checks.
