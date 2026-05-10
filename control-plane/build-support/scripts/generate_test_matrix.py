#!/usr/bin/env python3
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
"""
Generates a time-weighted sharded test matrix for GitHub Actions.

Usage: generate_test_matrix.py <cloud>
  cloud: kind | gke | eks | aks (default: kind)

Reads:
  acceptance/ci-inputs/<cloud>_acceptance_test_packages.yaml  -- ordered package list
  acceptance/ci-inputs/test-timings.json                      -- per-test durations (seconds)

For each package:
  - Discovers top-level Test* functions from source.
  - Looks up historical duration from test-timings.json.
    Unknown tests get DEFAULT_TEST_SECONDS as a conservative estimate.
  - Applies greedy bin-packing to distribute tests across shards so that
    each shard's estimated total is at most TARGET_SHARD_SECONDS.
  - Packages with no timing data at all are not sharded (single runner,
    no -run filter), so they remain unaffected until timing data exists.

Outputs a JSON array consumed by the GitHub Actions matrix strategy.
Each element has:
  runner       -- unique integer index (used for artifact naming)
  test-packages -- package directory name under acceptance/tests/
  run-filter   -- (optional) go test -run regexp; absent means run all tests
"""

import heapq
import json
import re
import sys
from pathlib import Path

TARGET_SHARD_SECONDS = 20 * 60   # aim for ≤20 min estimated wall time per shard
DEFAULT_TEST_SECONDS = 10 * 60   # assumed duration for tests with no recorded history

REPO_ROOT = Path(__file__).resolve().parent.parent.parent.parent


def discover_tests(pkg: str) -> list[str]:
    """Return top-level Test function names found in acceptance/tests/<pkg>."""
    pkg_dir = REPO_ROOT / "acceptance" / "tests" / pkg
    tests = []
    for f in sorted(pkg_dir.glob("*_test.go")):
        for m in re.finditer(
            r"^func (Test\w+)\(t \*testing\.T\)", f.read_text(), re.MULTILINE
        ):
            name = m.group(1)
            if name != "TestMain":
                tests.append(name)
    return tests


def bin_pack(tests_with_times: list[tuple[str, float]], target: float) -> list[list[str]]:
    """
    Greedy bin-packing: sort tests descending by duration, assign each to
    the current lightest shard. Returns a list of shards (each a list of
    test names), omitting empty shards.
    """
    if not tests_with_times:
        return []

    sorted_tests = sorted(tests_with_times, key=lambda x: -x[1])
    total = sum(t for _, t in sorted_tests)
    num_shards = max(1, -(-int(total) // int(target)))  # ceil division

    # min-heap entries: (current_total, stable_index, test_names)
    heap = [(0.0, i, []) for i in range(num_shards)]
    heapq.heapify(heap)

    for name, duration in sorted_tests:
        total, idx, names = heapq.heappop(heap)
        heapq.heappush(heap, (total + duration, idx, names + [name]))

    return [names for _, _, names in sorted(heap, key=lambda x: x[1]) if names]


def load_packages(path: Path) -> list[dict]:
    """Parse the simple list-of-dicts YAML used for acceptance package matrices."""
    items = []
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("-"):
            line = line[1:].strip().strip("{}")
            item = {k: v.strip('"').strip() for k, v in re.findall(r"([\w-]+):\s*\"?([^,}\"]+)\"?", line)}
            items.append(item)
    return items


def main() -> None:
    cloud = sys.argv[1] if len(sys.argv) > 1 else "kind"

    packages_yaml = REPO_ROOT / f"acceptance/ci-inputs/{cloud}_acceptance_test_packages.yaml"
    timings_json = REPO_ROOT / "acceptance/ci-inputs/test-timings.json"

    packages = load_packages(packages_yaml)
    raw_timings = json.loads(timings_json.read_text()) if timings_json.exists() else {}
    # Strip the metadata comment key
    timings = {k: v for k, v in raw_timings.items() if not k.startswith("_")}

    matrix = []
    runner_idx = 0

    for entry in packages:
        pkg = entry["test-packages"]
        pkg_timings = timings.get(pkg)  # None if package has no recorded data

        # Pass through any extra fields (e.g. num-clusters) to every shard for this package.
        # Excludes the canonical fields that the generator controls itself.
        extra = {k: v for k, v in entry.items() if k not in ("runner", "test-packages", "run-filter")}

        discovered = discover_tests(pkg)

        # No timing data at all → single unfiltered shard, no change in behaviour
        if not pkg_timings or not discovered:
            matrix.append({"runner": runner_idx, "test-packages": pkg, **extra})
            runner_idx += 1
            continue

        # Build (test, duration) pairs; use DEFAULT for tests missing from timing file
        tests_with_times = [
            (t, pkg_timings.get(t, DEFAULT_TEST_SECONDS)) for t in discovered
        ]

        shards = bin_pack(tests_with_times, TARGET_SHARD_SECONDS)

        if len(shards) <= 1:
            # Bin-packing produced a single shard — no filter needed
            matrix.append({"runner": runner_idx, "test-packages": pkg, **extra})
            runner_idx += 1
        else:
            for shard_tests in shards:
                run_filter = "^(" + "|".join(shard_tests) + ")$"
                matrix.append(
                    {
                        "runner": runner_idx,
                        "test-packages": pkg,
                        "run-filter": run_filter,
                        **extra,
                    }
                )
                runner_idx += 1

    print(json.dumps(matrix))


if __name__ == "__main__":
    main()
