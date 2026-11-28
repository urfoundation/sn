#!/usr/bin/env bash
# Pin a build version's copy of sn or server to immutable SDK, Connect, SCTP
# and gVisor modules. A build runs this on its own copy, commits the result
# there and builds it with dependency_mode "pinned"; main keeps its local
# substitutions, and nobody runs this on main.
#
#   scripts/mainnet-release-build/pin.sh [-C <checkout>] <sdk-commit> <connect-commit> [<gvisor-commit>]
#
# <checkout> is the sn copy holding this script (the default) or a server copy,
# clean. Each commit must be checked out in the sibling of that name beside the
# checkout, and pushed to GitHub. <gvisor-commit> defaults to the gvisor
# sibling's HEAD. PIN-PROCEDURE.md beside this script has the whole procedure.
set -euo pipefail

die() {
  printf 'pin: %s\n' "$*" >&2
  exit 1
}
usage() {
  printf 'usage: %s [-C <checkout>] <sdk-commit> <connect-commit> [<gvisor-commit>]\n' "$0" >&2
  exit 2
}

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
checkout="$here/../.."
if [[ "${1:-}" == -C ]]; then
  (( $# >= 2 )) || usage
  checkout="$2"
  shift 2
fi
(( $# == 2 || $# == 3 )) || usage
checkout="$(cd "$checkout" && pwd -P)" || die "no such checkout"
workspace="$(dirname "$checkout")"
for tool in git go python3; do
  command -v "$tool" >/dev/null || die "$tool is required"
done

# Ignore any go.work and ambient flags. GOPROXY, GOSUMDB and GOPRIVATE stay the
# caller's: they choose how the modules are fetched, and the commit message
# records them.
export GOWORK=off GOFLAGS=
# Run every command below with the toolchain that go.mod requires, without
# switching again, as the release builder does (GOTOOLCHAIN=local).
goroot="$(cd "$checkout" && go env GOROOT)" || die "cannot select the Go toolchain that go.mod requires"
export PATH="$goroot/bin:$PATH" GOTOOLCHAIN=local

main_module="$(awk '$1 == "module" { print $2; exit }' "$checkout/go.mod")"
case "$main_module" in
  github.com/urfoundation/sn) repository=sn ;;
  github.com/urnetwork/server) repository=server ;;
  *) die "$checkout is neither sn nor server" ;;
esac
[[ -z "$(git -C "$checkout" status --porcelain=v1 --untracked-files=all)" ]] ||
  die "$checkout has uncommitted or untracked changes; a pin starts from a clean checkout"

# The release gate tests the sibling checkouts themselves, so a pin must name
# exactly what is checked out there.
sibling_commit() {
  local sibling="$1" commit="$2" full head
  [[ -e "$workspace/$sibling/.git" ]] || die "no $sibling checkout beside $checkout"
  full="$(git -C "$workspace/$sibling" rev-parse --verify --quiet "$commit^{commit}")" ||
    die "the $sibling checkout has no commit $commit; fetch it first"
  head="$(git -C "$workspace/$sibling" rev-parse HEAD)"
  [[ "$head" == "$full" ]] ||
    die "the $sibling checkout is at $head, not at $full; check out the frozen commit"
  printf '%s\n' "$full"
}

# Print "version<TAB>Sum<TAB>GoModSum" for a module at a commit. A proxy that
# has not seen a commit yet can answer "not found: fetch timed out" while it
# fetches it, and serve it a little later, so try a few times.
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
module_identity() {
  local module="$1" commit="$2" attempt reply
  for attempt in 1 2 3 4; do
    if reply="$(cd "$scratch" && go mod download -json "$module@$commit" 2>"$scratch/stderr")"; then
      python3 -c '
import json, sys
module, commit = sys.argv[1:]
reply = json.load(sys.stdin)
version, total, mod = reply["Version"], reply["Sum"], reply["GoModSum"]
origin = (reply.get("Origin") or {}).get("Hash", commit)
if reply["Path"] != module or origin != commit or not version.endswith("-" + commit[:12]):
    sys.exit("pin: %s resolved to %s at %s, not to commit %s" % (module, version, origin, commit))
if not (total.startswith("h1:") and mod.startswith("h1:")):
    sys.exit("pin: %s has no h1 checksums" % module)
print(version, total, mod, sep="\t")
' "$module" "$commit" <<<"$reply"
      return
    fi
    printf 'pin: resolving %s@%s failed (attempt %s of 4):\n%s\n' "$module" "$commit" "$attempt" "$reply" >&2
    cat "$scratch/stderr" >&2
    (( attempt == 4 )) || sleep 30
  done
  die "cannot resolve $module@$commit; see PIN-PROCEDURE.md, \"Module resolution\""
}

cd "$checkout"
replaces="$(go mod edit -json | python3 -c '
import json, sys
for replace in json.load(sys.stdin).get("Replace") or []:
    print(replace["Old"]["Path"], replace["New"]["Path"], replace["New"].get("Version", ""))')"
replaced() {
  awk -v module="$1" '$1 == module { found = 1 } END { exit !found }' <<<"$replaces"
}
for module in github.com/urnetwork/sdk github.com/urnetwork/connect github.com/pion/sctp; do
  replaced "$module" || die "go.mod does not replace $module"
done

sdk_commit="$(sibling_commit sdk "$1")"
connect_commit="$(sibling_commit connect "$2")"
# The release builder admits a local module only from its pinned repositories,
# which do not include gvisor, so a local gvisor replacement is pinned as well.
gvisor_commit=""
if (( $# == 3 )); then
  replaced gvisor.dev/gvisor || die "go.mod does not replace gvisor.dev/gvisor"
  gvisor_commit="$(sibling_commit gvisor "$3")"
elif awk '$1 == "gvisor.dev/gvisor" && $3 == "" { found = 1 } END { exit !found }' <<<"$replaces"; then
  [[ -e "$workspace/gvisor/.git" ]] || die "go.mod uses ../gvisor, and there is no gvisor checkout beside $checkout"
  gvisor_commit="$(sibling_commit gvisor "$(git -C "$workspace/gvisor" rev-parse HEAD)")"
fi

identity="$(module_identity github.com/urnetwork/sdk "$sdk_commit")"
IFS=$'\t' read -r sdk_version sdk_sum sdk_mod_sum <<<"$identity"
identity="$(module_identity github.com/urnetwork/connect "$connect_commit")"
IFS=$'\t' read -r connect_version connect_sum connect_mod_sum <<<"$identity"
identity="$(module_identity github.com/urnetwork/connect/sctp "$connect_commit")"
IFS=$'\t' read -r sctp_version sctp_sum sctp_mod_sum <<<"$identity"
gvisor_version="" gvisor_sum="" gvisor_mod_sum=""
if [[ -n "$gvisor_commit" ]]; then
  identity="$(module_identity github.com/urnetwork/gvisor "$gvisor_commit")"
  IFS=$'\t' read -r gvisor_version gvisor_sum gvisor_mod_sum <<<"$identity"
fi

replacements=(
  "-replace=github.com/urnetwork/connect=github.com/urnetwork/connect@$connect_version"
  "-replace=github.com/urnetwork/sdk=github.com/urnetwork/sdk@$sdk_version"
  "-replace=github.com/pion/sctp=github.com/urnetwork/connect/sctp@$sctp_version"
)
if [[ -n "$gvisor_commit" ]]; then
  replacements+=("-replace=gvisor.dev/gvisor=github.com/urnetwork/gvisor@$gvisor_version")
fi
go mod edit "${replacements[@]}"

# Rewrite the go.mod comments that described the sibling checkouts. In sn,
# also record the pins where the release builder's admission table, its
# fixture, the source-graph tests and RELEASE-BUILD.md name them.
REPOSITORY="$repository" \
SDK_COMMIT="$sdk_commit" SDK_VERSION="$sdk_version" SDK_SUM="$sdk_sum" SDK_MOD_SUM="$sdk_mod_sum" \
CONNECT_COMMIT="$connect_commit" CONNECT_VERSION="$connect_version" CONNECT_SUM="$connect_sum" CONNECT_MOD_SUM="$connect_mod_sum" \
SCTP_VERSION="$sctp_version" SCTP_SUM="$sctp_sum" SCTP_MOD_SUM="$sctp_mod_sum" \
python3 - <<'PYTHON'
import os
import re
import sys

E = os.environ
# main module path -> (replacement path, version, Sum, GoModSum)
pins = {
    "github.com/urnetwork/sdk": ("github.com/urnetwork/sdk", E["SDK_VERSION"], E["SDK_SUM"], E["SDK_MOD_SUM"]),
    "github.com/urnetwork/connect": ("github.com/urnetwork/connect", E["CONNECT_VERSION"], E["CONNECT_SUM"], E["CONNECT_MOD_SUM"]),
    "github.com/pion/sctp": ("github.com/urnetwork/connect/sctp", E["SCTP_VERSION"], E["SCTP_SUM"], E["SCTP_MOD_SUM"]),
}


def rewrite(path, edit):
    with open(path, encoding="utf-8") as file:
        before = file.read()
    after = edit(before)
    with open(path, "w", encoding="utf-8") as file:
        file.write(after)


def once(pattern, replacement, text, what):
    text, count = re.subn(pattern, replacement, text, flags=re.MULTILINE)
    if count != 1:
        sys.exit("pin: expected one %s, found %d; fix scripts/mainnet-release-build/pin.sh" % (what, count))
    return text


def admission_table(text):
    for module, (path, version, total, mod) in pins.items():
        row = '{"%s", "%s", "%s", "%s"},' % (path, version, total, mod)
        text = once(r'^(\t\t"%s":\s+)\{"[^"]*", "[^"]*", "h1:[^"]*", "h1:[^"]*"\},$' % re.escape(module),
                    lambda match: match.group(1) + row, text, "admission pin for " + module)
    return text


def admission_fixture(text):
    for module, (path, version, total, mod) in pins.items():
        row = '{Path: "%s", Replace: &goModule{Path: "%s", Version: "%s", Sum: "%s", GoModSum: "%s"}},' % (module, path, version, total, mod)
        text = once(r'^(\t\t)\{Path: "%s", Replace: &goModule\{Path: "[^"]*", Version: "[^"]*", Sum: "h1:[^"]*", GoModSum: "h1:[^"]*"\}\},$' % re.escape(module),
                    lambda match: match.group(1) + row, text, "fixture pin for " + module)
    return text


def source_graph(text):
    for module, (path, version, _, _) in pins.items():
        text = once(r'(requireSiblingOrPin\(t, "[^"]*", "%s", "[^"]*", "%s", ")[^"]*("\))' % (re.escape(module), re.escape(path)),
                    lambda match: match.group(1) + version + match.group(2), text, "source-graph pin for " + module)
    return text


def comment_above(lines, module, comment):
    found = [index for index, line in enumerate(lines) if line.startswith("replace %s => " % module)]
    if len(found) != 1:
        sys.exit("pin: expected one replace directive for %s in go.mod" % module)
    start = found[0]
    while start > 0 and lines[start - 1].startswith("//"):
        start -= 1
    lines[start:found[0]] = comment


def module_comments(text):
    lines = text.split("\n")
    if E["REPOSITORY"] == "sn":
        comment_above(lines, "github.com/urnetwork/connect", [
            "// This build version pins the frozen SDK and Connect commits. Versioned",
            "// replacements also cover the v0.0.0 placeholders that sibling modules require,",
            "// so a ../sdk or ../connect checkout cannot select different source. The",
            "// release builder's pinned admission table",
            "// (scripts/mainnet-release-build/sources.go) binds the same versions to their",
            "// checksums.",
        ])
        comment_above(lines, "github.com/pion/sctp", [
            "// A dependency's replaces are ignored, so pin Connect's SCTP fork here too,",
            "// at the same Connect commit.",
        ])
    else:
        comment_above(lines, "github.com/urnetwork/connect", [
            "// This build version pins the frozen SDK and Connect commits as its SN go.mod",
            "// does; the SN release builder's pinned mode admits this module only with the",
            "// same versioned replacements. The other siblings remain local sources.",
            "// Replacements in imported modules are not inherited by Go.",
        ])
    if any(line.startswith("replace gvisor.dev/gvisor => github.com/urnetwork/gvisor ") for line in lines):
        comment_above(lines, "gvisor.dev/gvisor", [
            "// The published fork, at the commit of the ../gvisor checkout. The SN release",
            "// builder's pinned mode admits a local module only from its census.",
        ])
    return "\n".join(lines)


def release_build_document(text):
    lines = text.split("\n")
    october = [index for index, line in enumerate(lines)
               if line == "| `github.com/pion/sctp` | `github.com/urnetwork/connect/sctp v0.0.0-20261001021459-e1b5d77b5029` |"]
    following = [index for index, line in enumerate(lines) if line.startswith("The [current unsigned preparation]")]
    if len(october) != 1 or len(following) != 1 or october[0] >= following[0]:
        sys.exit("pin: mainnet/RELEASE-BUILD.md no longer has the October 1 pin table before "
                 "\"The [current unsigned preparation]\"; record the pins there by hand")
    block = [
        "",
        "This build version pins the modules below. Its pinned mode admits only these",
        "values, which replace the October 1 values above for this build, and both main",
        "modules select them with versioned replacements in `go.mod`. Each row is the",
        "`Version`, `Sum` and `GoModSum` that `go mod download -json <module>@<commit>`",
        "reports. `validateReleaseModule` (`scripts/mainnet-release-build/sources.go`)",
        "compares all three with each main module's resolution, and",
        "`scripts/source-graph` asserts SN's versions.",
        "",
        "| Module | Commit | Effective immutable version | `Sum` | `GoModSum` |",
        "| --- | --- | --- | --- | --- |",
        "| SDK | `%s` | `github.com/urnetwork/sdk %s` | `%s` | `%s` |" % (E["SDK_COMMIT"], E["SDK_VERSION"], E["SDK_SUM"], E["SDK_MOD_SUM"]),
        "| Connect | `%s` | `github.com/urnetwork/connect %s` | `%s` | `%s` |" % (E["CONNECT_COMMIT"], E["CONNECT_VERSION"], E["CONNECT_SUM"], E["CONNECT_MOD_SUM"]),
        "| `github.com/pion/sctp` | the Connect commit | `github.com/urnetwork/connect/sctp %s` | `%s` | `%s` |" % (E["SCTP_VERSION"], E["SCTP_SUM"], E["SCTP_MOD_SUM"]),
        "",
    ]
    with open("go.mod", encoding="utf-8") as file:
        gvisor = re.search(r"^replace gvisor\.dev/gvisor => (github\.com/urnetwork/gvisor \S+)$", file.read(), flags=re.MULTILINE)
    if gvisor:
        block += [
            "`gvisor.dev/gvisor` resolves to the published fork,",
            "`%s`, because pinned" % gvisor.group(1),
            "mode admits a local module only from its census. `go.sum` binds the fork's",
            "bytes as it binds every remote module's.",
            "",
        ]
    lines[october[0] + 1:following[0]] = block
    return "\n".join(lines)


rewrite("go.mod", module_comments)
if E["REPOSITORY"] == "sn":
    rewrite("scripts/mainnet-release-build/sources.go", admission_table)
    rewrite("scripts/mainnet-release-build/sources_test.go", admission_fixture)
    rewrite("scripts/source-graph/source_graph_test.go", source_graph)
    rewrite("mainnet/RELEASE-BUILD.md", release_build_document)
PYTHON
if [[ "$repository" == sn ]]; then
  go fmt ./scripts/mainnet-release-build ./scripts/source-graph >/dev/null
fi

go mod tidy

# The pinned graph must now resolve offline to exactly the recorded identities,
# as the source-graph tests and the release builder read it.
expect() {
  local module="$1" want="$2 $3 $4 $5" actual
  actual="$(GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off go list -m -json "$module" | python3 -c '
import json, sys
replace = json.load(sys.stdin).get("Replace") or {}
print(replace.get("Path", ""), replace.get("Version", ""), replace.get("Sum", ""), replace.get("GoModSum", ""))')"
  [[ "$actual" == "$want" ]] || die "$module resolves to \"$actual\", not to \"$want\""
}
expect github.com/urnetwork/sdk github.com/urnetwork/sdk "$sdk_version" "$sdk_sum" "$sdk_mod_sum"
expect github.com/urnetwork/connect github.com/urnetwork/connect "$connect_version" "$connect_sum" "$connect_mod_sum"
expect github.com/pion/sctp github.com/urnetwork/connect/sctp "$sctp_version" "$sctp_sum" "$sctp_mod_sum"
if [[ -n "$gvisor_commit" ]]; then
  expect gvisor.dev/gvisor github.com/urnetwork/gvisor "$gvisor_version" "$gvisor_sum" "$gvisor_mod_sum"
fi
go mod tidy -diff >/dev/null || die "go.mod or go.sum is not tidy after the pin"
if [[ "$repository" == sn ]]; then
  go test ./scripts/source-graph ./scripts/mainnet-release-build -count=1
fi

# Beside the checkouts, so the copy stays clean until it is committed.
message="$workspace/pin-$repository.msg"
record() {
  printf '%s\n  commit    %s\n  version   %s\n  Sum       %s\n  GoModSum  %s\n' "$@"
}
sources='SDK, Connect and SCTP'
if [[ -n "$gvisor_commit" ]]; then
  sources='SDK, Connect, SCTP and gVisor'
fi
{
  if [[ "$repository" == sn ]]; then
    printf 'Pin the frozen %s sources for this build version\n\n' "$sources"
    printf 'go.mod selects them with versioned replacements instead of the sibling\n'
    printf 'checkouts. The release builder'"'"'s pinned admission table, its fixture, the\n'
    printf 'source-graph tests and RELEASE-BUILD.md name the same pins.\n\n'
  else
    printf 'Pin the frozen %s sources for this build version\n\n' "$sources"
    printf 'go.mod selects them with versioned replacements instead of the sibling\n'
    printf 'checkouts, as the SN release builder'"'"'s pinned mode requires of this main\n'
    printf 'module.\n\n'
  fi
  record github.com/urnetwork/sdk "$sdk_commit" "$sdk_version" "$sdk_sum" "$sdk_mod_sum"
  record github.com/urnetwork/connect "$connect_commit" "$connect_version" "$connect_sum" "$connect_mod_sum"
  record 'github.com/pion/sctp => github.com/urnetwork/connect/sctp' "$connect_commit" "$sctp_version" "$sctp_sum" "$sctp_mod_sum"
  if [[ -n "$gvisor_commit" ]]; then
    record 'gvisor.dev/gvisor => github.com/urnetwork/gvisor' "$gvisor_commit" "$gvisor_version" "$gvisor_sum" "$gvisor_mod_sum"
  fi
  printf '\nEach version, Sum and GoModSum is what `go mod download -json\n'
  printf '<module>@<commit>` reported with GOPROXY=%s,\n' "$(go env GOPROXY)"
  printf 'GOSUMDB=%s, GONOSUMDB=%s and GOPRIVATE=%s.\n' "$(go env GOSUMDB)" "$(go env GONOSUMDB)" "$(go env GOPRIVATE)"
} > "$message"

printf '\npin: %s is pinned and ready to commit:\n\n' "$checkout"
git status --short
printf '\n'
cat "$message"
printf '\npin: commit with: git -C %s commit -a -F %s\n' "$checkout" "$message"
