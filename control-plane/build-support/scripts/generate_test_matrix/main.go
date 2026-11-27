// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// generate_test_matrix generates a time-weighted sharded test matrix for GitHub Actions.
//
// Usage: generate_test_matrix <cloud>
//
// cloud: kind | gke | eks | aks | openshift (default: kind)
//
// Reads:
//
//	acceptance/ci-inputs/<cloud>_acceptance_test_packages.yaml -- ordered package list
//	acceptance/ci-inputs/test-timings.json                     -- per-test durations (seconds)
//
// For each package, discovers top-level Test* functions, looks up their historical duration
// from test-timings.json, and applies greedy bin-packing to distribute tests across shards
// so that each shard's estimated total is at most targetShardSeconds. Packages with no
// timing data are emitted as a single unfiltered shard.
//
// Outputs a JSON array consumed by the GitHub Actions matrix strategy. Each element has:
//
//	runner        -- unique integer index (used for artifact naming)
//	test-packages -- package directory name under acceptance/tests/
//	run-filter    -- (optional) go test -run regexp; absent means run all tests
//	<extra>       -- any additional fields from the YAML entry (e.g. num-clusters)
package main

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	targetShardSeconds = 20 * 60 // aim for ≤20 min estimated wall time per shard
	defaultTestSeconds = 10 * 60 // assumed duration for tests with no recorded history
)

// testEntry pairs a test function name with its estimated duration in seconds.
type testEntry struct {
	name     string
	duration float64
}

// shardItem is a node in the min-heap used during bin-packing.
type shardItem struct {
	total float64
	idx   int
	tests []string
}

type shardHeap []shardItem

func (h shardHeap) Len() int            { return len(h) }
func (h shardHeap) Less(i, j int) bool  { return h[i].total < h[j].total }
func (h shardHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *shardHeap) Push(x any)         { *h = append(*h, x.(shardItem)) }
func (h *shardHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// discoverTests returns the top-level Test* function names found in acceptance/tests/<pkg>.
// Files are processed in sorted order for deterministic output.
func discoverTests(repoRoot, pkg string) []string {
	pkgDir := filepath.Join(repoRoot, "acceptance", "tests", pkg)
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil
	}

	re := regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\)`)
	var tests []string
	// ReadDir already returns entries sorted by name.
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range re.FindAllSubmatch(data, -1) {
			if name := string(m[1]); name != "TestMain" {
				tests = append(tests, name)
			}
		}
	}
	return tests
}

// binPack distributes tests across shards using a greedy min-heap algorithm.
// Tests are sorted descending by duration and assigned to the lightest shard.
// The number of shards is ceil(total_duration / target).
func binPack(tests []testEntry, target float64) [][]string {
	if len(tests) == 0 {
		return nil
	}

	sort.Slice(tests, func(i, j int) bool {
		return tests[i].duration > tests[j].duration
	})

	var total float64
	for _, t := range tests {
		total += t.duration
	}
	numShards := int(math.Ceil(total / target))
	if numShards < 1 {
		numShards = 1
	}

	h := make(shardHeap, numShards)
	for i := range h {
		h[i] = shardItem{idx: i}
	}
	heap.Init(&h)

	for _, t := range tests {
		item := heap.Pop(&h).(shardItem)
		item.tests = append(item.tests, t.name)
		item.total += t.duration
		heap.Push(&h, item)
	}

	// Restore original index order so output is deterministic.
	sort.Slice(h, func(i, j int) bool { return h[i].idx < h[j].idx })

	var shards [][]string
	for _, item := range h {
		if len(item.tests) > 0 {
			shards = append(shards, item.tests)
		}
	}
	return shards
}

// findRepoRoot walks up from the current directory until it finds the .git directory,
// which marks the repository root. This allows the tool to be invoked from any
// subdirectory within the repo (e.g. `cd control-plane && go run ...`).
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root not found: no .git directory in path from %s", dir)
		}
		dir = parent
	}
}

func main() {
	cloud := "kind"
	if len(os.Args) > 1 {
		cloud = os.Args[1]
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error finding repo root: %v\n", err)
		os.Exit(1)
	}

	// Load the ordered package list.
	packagesPath := filepath.Join(repoRoot, "acceptance", "ci-inputs", cloud+"_acceptance_test_packages.yaml")
	packagesData, err := os.ReadFile(packagesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", packagesPath, err)
		os.Exit(1)
	}
	// Each YAML entry is a map of string fields; all values are treated as strings.
	var packages []map[string]string
	if err := yaml.Unmarshal(packagesData, &packages); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing packages YAML: %v\n", err)
		os.Exit(1)
	}

	// Load per-test timing data. Missing file is not an error; all tests get the default.
	timings := make(map[string]map[string]float64)
	timingsPath := filepath.Join(repoRoot, "acceptance", "ci-inputs", "test-timings.json")
	if raw, err := os.ReadFile(timingsPath); err == nil {
		// The top-level JSON is map[pkg]map[testName]seconds with a "_comment" key to skip.
		var top map[string]json.RawMessage
		if err := json.Unmarshal(raw, &top); err == nil {
			for pkg, v := range top {
				if strings.HasPrefix(pkg, "_") {
					continue
				}
				var pkgTimings map[string]float64
				if err := json.Unmarshal(v, &pkgTimings); err == nil {
					timings[pkg] = pkgTimings
				}
			}
		}
	}

	// canonical fields that the generator controls; everything else is passed through.
	canonical := map[string]bool{"runner": true, "test-packages": true, "run-filter": true}

	var matrix []map[string]any
	runnerIdx := 0

	for _, entry := range packages {
		pkg := entry["test-packages"]

		// Extra fields (e.g. num-clusters) are forwarded to every shard for this package.
		extra := make(map[string]string)
		for k, v := range entry {
			if !canonical[k] {
				extra[k] = v
			}
		}

		discovered := discoverTests(repoRoot, pkg)
		pkgTimings := timings[pkg]

		newRow := func() map[string]any {
			row := map[string]any{"runner": runnerIdx, "test-packages": pkg}
			for k, v := range extra {
				row[k] = v
			}
			return row
		}

		// No timing data → single unfiltered shard; behaviour is unchanged from before sharding.
		if len(pkgTimings) == 0 || len(discovered) == 0 {
			matrix = append(matrix, newRow())
			runnerIdx++
			continue
		}

		tests := make([]testEntry, 0, len(discovered))
		for _, name := range discovered {
			dur, ok := pkgTimings[name]
			if !ok {
				dur = defaultTestSeconds
			}
			tests = append(tests, testEntry{name: name, duration: dur})
		}

		shards := binPack(tests, targetShardSeconds)

		if len(shards) <= 1 {
			matrix = append(matrix, newRow())
			runnerIdx++
		} else {
			for _, shardTests := range shards {
				row := newRow()
				row["run-filter"] = "^(" + strings.Join(shardTests, "|") + ")$"
				matrix = append(matrix, row)
				runnerIdx++
			}
		}
	}

	out, err := json.Marshal(matrix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling matrix: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}
