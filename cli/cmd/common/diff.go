package common

import (
	"reflect"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Diff returns a string representation of the difference between two maps as YAML.
func Diff(a, b map[string]interface{}) (string, error) {
	if len(a) == 0 && len(b) == 0 {
		return "", nil
	}

	buf := new(strings.Builder)
	err := diffRecursively(a, b, buf)

	return buf.String(), err
}

// diffRecursively recursively iterates over both maps and writes the differences to the given buffer.
func diffRecursively(a, b map[string]interface{}, buf *strings.Builder) error {
	// Get the union of keys in a and b sorted alphabetically.
	keys := collectKeys(a, b)

	for _, key := range keys {
		valueInA, inA := a[key]
		valueInB, inB := b[key]

		aSlice := map[string]interface{}{
			key: valueInA,
		}
		bSlice := map[string]interface{}{
			key: valueInB,
		}

		// If the key is in both a and b, compare the values.
		if inA && inB {
			// If the map slices are the same, write as unchanged YAML.
			if reflect.DeepEqual(aSlice, bSlice) {
				asYaml, err := yaml.Marshal(aSlice)
				if err != nil {
					return err
				}

				lines := strings.Split(strings.TrimSpace(string(asYaml)), "\n")
				writeWithPrepend("  ", lines, buf)
				continue
			}

			// If the map slices are different and there is another level of depth to the map, recurse.
			if len(aSlice) > 1 && len(bSlice) > 1 {
				diffRecursively(aSlice, bSlice, buf)
				continue
			}

			// If the map slices are different and there is no other level of depth to the map, write as changed YAML.
			aSliceAsYaml, err := yaml.Marshal(aSlice)
			if err != nil {
				return err
			}

			bSliceAsYaml, err := yaml.Marshal(bSlice)
			if err != nil {
				return err
			}

			writeWithPrepend("- ", strings.Split(strings.TrimSpace(string(aSliceAsYaml)), "\n"), buf)
			writeWithPrepend("+ ", strings.Split(strings.TrimSpace(string(bSliceAsYaml)), "\n"), buf)
		}

		// If the key is in a but not in b, write as removed.
		if inA && !inB {
			asYaml, err := yaml.Marshal(aSlice)
			if err != nil {
				return err
			}

			lines := strings.Split(strings.TrimSpace(string(asYaml)), "\n")
			writeWithPrepend("- ", lines, buf)
			continue
		}

		// If the key is in b but not in a, write as added.
		if !inA && inB {
			asYaml, err := yaml.Marshal(bSlice)
			if err != nil {
				return err
			}

			lines := strings.Split(strings.TrimSpace(string(asYaml)), "\n")
			writeWithPrepend("+ ", lines, buf)
			continue
		}
	}

	return nil
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

// writeWithPrepend writes each line to the buffer with the given prefix.
func writeWithPrepend(prepend string, lines []string, buf *strings.Builder) {
	for _, line := range lines {
		buf.WriteString(prepend)
		buf.WriteString(line)
		buf.WriteString("\n")
	}
}
