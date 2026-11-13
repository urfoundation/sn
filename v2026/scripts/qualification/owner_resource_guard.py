"""One phase's existing resource floors around the durable owned-tree guard.

Import one instance per phase. The caller supplies its joined sampler hook and
sets resource_observation_failed on any invalid observation. A guard exception
retains its actual wait through ChildContext without becoming a product pass.
"""
from pathlib import Path
import shutil

import cargo_control


resource_observation_failed = False
before_tree_join = None


def run_process(args, cwd, environment, output, label, timeout,
                log_limit=cargo_control.MAXIMUM_LOG_BYTES, minimum_free=0):
    """Keep the reviewed floors and stop only this guard's original child tree."""
    def observe():
        memory = next(int(line.split()[1]) * 1024
                      for line in Path("/proc/meminfo").read_text().splitlines()
                      if line.startswith("MemAvailable:"))
        cargo_control.require(memory >= 96 * 1024 * 1024 * 1024,
                              "active model crossed memory floor")
        cargo_control.require(shutil.disk_usage("/var/lib/docker").free >= 295833415680,
                              "active model crossed Docker root floor")
        cargo_control.require(not resource_observation_failed,
                              "owned resource observation failed")

    return cargo_control.run_process(args, cwd, environment, output, label, timeout,
        log_limit=log_limit, minimum_free=minimum_free,
        resource_observer=observe, before_tree_join=before_tree_join)
