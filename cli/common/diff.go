package common

import (
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
)

// Diff returns a string representation of the difference between two maps as YAML.
// The returned string is sorted alphabetically by key. If the maps are identical, the returned string is empty.
func Diff(a, b map[string]interface{}) (string, error) {
	if len(a) == 0 && len(b) == 0 {
		return "", nil
	}

	return diffRecursively(a, b, 0)
}

// diffRecursively iterates over maps `a` and `b` and returns a string representation of the difference between them as YAML.
// The returned string is sorted alphabetically by key with `+` and `-` prefixed to "diffed" lines.
// `c` is a map of the default values for the chart. If a key is present in `a`, but not `b` (i.e. removed),
// the value is compared with the default value in `c` to prevent a false positive "removed" line.
func diffRecursively(a, b map[string]interface{}, recurseDepth int) (string, error) {
	buf := new(strings.Builder)

	// Get the union of keys in a and b sorted alphabetically.
	keys := collectKeys(a, b)

	for _, key := range keys {
		valueInA, inA := a[key]
		valueInB, inB := b[key]

		aMapWithKey := map[string]interface{}{
			key: valueInA,
		}
		bMapWithKey := map[string]interface{}{
			key: valueInB,
		}

		// If the key is in both a and b, compare the values.
		if inA && inB {
			// If the map slices are the same, write as unchanged YAML.
			if cmp.Equal(aMapWithKey, bMapWithKey) {
				asYaml, err := yaml.Marshal(aMapWithKey)
				if err != nil {
					return "", err
				}

				writeWithPrepend("  ", string(asYaml), recurseDepth, buf)
				continue
			}

			// If the maps are different and there is another level of depth to the map, recurse.
			if !isMaxDepth(aMapWithKey) && !isMaxDepth(bMapWithKey) {
				writeWithPrepend("  ", key+":", recurseDepth, buf)

				childDiff, err := diffRecursively(valueInA.(map[string]interface{}), valueInB.(map[string]interface{}), recurseDepth+1)
				if err != nil {
					return "", err
				}

				buf.WriteString(childDiff)

				continue
			}

			// If the map slices are different and there is no other level of depth to the map, write as changed YAML.
			aSliceAsYaml, err := yaml.Marshal(aMapWithKey)
			if err != nil {
				return "", err
			}

			bSliceAsYaml, err := yaml.Marshal(bMapWithKey)
			if err != nil {
				return "", err
			}

			writeWithPrepend("- ", string(aSliceAsYaml), recurseDepth, buf)
			writeWithPrepend("+ ", string(bSliceAsYaml), recurseDepth, buf)
		}

		// If the key is in `a` but not in `b`, write as removed unless `a` matches the value in `c`.
		if inA && !inB {
			asYaml, err := yaml.Marshal(aMapWithKey)
			if err != nil {
				return "", err
			}

			writeWithPrepend("- ", string(asYaml), recurseDepth, buf)
			continue
		}

		// If the key is in b but not in a, write as added.
		if !inA && inB {
			asYaml, err := yaml.Marshal(bMapWithKey)
			if err != nil {
				return "", err
			}

			writeWithPrepend("+ ", string(asYaml), recurseDepth, buf)
			continue
		}
	}

	return buf.String(), nil
}

// collectKeys iterates over both maps and collects all keys sorted alphabetically, ignoring duplicates.
func collectKeys(a, b map[string]interface{}) []string {
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)
	return keys
}

// writeWithPrepend writes each line to the buffer with the given prefix and indentation matching the recurse depth.
func writeWithPrepend(prepend, text string, recurseDepth int, buf *strings.Builder) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		buf.WriteString(prepend)
		for i := 0; i < recurseDepth; i++ {
			buf.WriteString("  ")
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
}

// isMaxDepth returns false if any of the values in the map are maps.
func isMaxDepth(m map[string]interface{}) bool {
	for _, value := range m {
		if _, ok := value.(map[string]interface{}); ok {
			return false
		}
	}

	return true
}
