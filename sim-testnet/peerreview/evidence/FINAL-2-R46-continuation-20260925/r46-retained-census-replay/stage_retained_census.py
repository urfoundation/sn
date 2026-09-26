#!/usr/bin/env python3
"""Copy only sealed diagnostic census inputs; never read chain or live state."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath


def sha256(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--diagnostic", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--source-revision", required=True)
    args = parser.parse_args()
    diagnostic = args.diagnostic.resolve(strict=True)
    output = args.output.absolute()
    if len(args.source_revision) != 40 or any(c not in "0123456789abcdef" for c in args.source_revision):
        raise ValueError("source revision must be an exact commit")
    if output.exists() or output == diagnostic or diagnostic in output.parents:
        raise ValueError("output must be a new external directory")
    report_path = diagnostic / "report.json"
    report_data = report_path.read_bytes()
    report = json.loads(report_data)
    if report.get("schema") != "urnetwork-sim-terminal-diagnostics-v1" or report.get("status") not in ("complete", "complete-with-findings") or not report.get("completed_at") or not report.get("read_only") or report.get("final_acceptance"):
        raise ValueError("diagnostic report is not sealed")
    checks = {}
    for check in report["checks"]:
        if check["id"] in checks:
            raise ValueError("duplicate diagnostic check")
        checks[check["id"]] = check
    for name in ("closed-foundation-receipts-and-topology", "original-scenario-result"):
        if checks.get(name, {}).get("status") != "pass":
            raise ValueError("required sealed check did not pass: " + name)
    closed = checks["closed-foundation-receipts-and-topology"]["evidence"]
    locators = [closed["result"], closed["terminal"]]
    locators.extend(locator for locator in closed["bundles"] if PurePosixPath(locator["uri"]).name.startswith(("launch-foundation", "plan-history", "public")))
    output.mkdir(mode=0o700, parents=False)
    (output / "objects").mkdir(mode=0o700)
    report_copy = output / "diagnostic-report.json"
    report_copy.write_bytes(report_data)
    report_copy.chmod(0o400)
    artifacts = []
    seen = set()
    for locator in locators:
        uri = PurePosixPath(locator["uri"])
        if uri.is_absolute() or ".." in uri.parts or str(uri) != locator["uri"] or ":" in str(uri):
            raise ValueError("artifact is not a canonical local path")
        if locator["uri"] in seen:
            raise ValueError("duplicate selected artifact")
        seen.add(locator["uri"])
        path = diagnostic.joinpath(*uri.parts)
        if path.resolve(strict=True) != path:
            raise ValueError("artifact path traverses a symlink")
        data = path.read_bytes()
        digest = sha256(data)
        if digest != locator["content_sha256"] or len(data) != locator["size_bytes"]:
            raise ValueError("sealed artifact differs: " + str(uri))
        relative = "objects/" + digest.removeprefix("sha256:") + ".bin"
        destination = output / relative
        if destination.exists():
            if destination.read_bytes() != data:
                raise ValueError("copied object collision")
        else:
            destination.write_bytes(data)
            destination.chmod(0o400)
        artifacts.append({"file": {"path": relative, "sha256": digest, "bytes": len(data)}, "locator": locator})
    if report_path.read_bytes() != report_data:
        raise ValueError("diagnostic report changed while staging")
    manifest = {
        "schema": "urnetwork-retained-chain-census-inputs-v1",
        "source_revision": args.source_revision,
        "report": {"path": "diagnostic-report.json", "sha256": sha256(report_data), "bytes": len(report_data)},
        "artifacts": artifacts,
    }
    manifest_path = output / "manifest.json"
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")
    manifest_path.chmod(0o400)
    print(json.dumps({"manifest": str(manifest_path), "manifest_sha256": sha256(manifest_path.read_bytes()), "diagnostic_report_sha256": sha256(report_data), "artifact_count": len(artifacts)}))


if __name__ == "__main__":
    main()
