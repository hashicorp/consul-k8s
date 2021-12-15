package common

import (
	"strings"
)

func Diff(a, b map[string]interface{}) string {
	if len(a) == 0 && len(b) == 0 {
		return ""
	}

	buf := new(strings.Builder)
	diffRecursively(a, b, buf)

	return buf.String()
}

// collectKeys iterates over both maps and collects all keys, ignoring duplicates.
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
	return keys
}

func diffRecursively(a, b map[string]interface{}, buf *strings.Builder) {
	// Get the union of keys in a and b.
	keys := collectKeys(a, b)

	for _, key := range keys {
		valueInA, inA := a[key]
		valueInB, inB := b[key]

		// If the key is in both maps and the values are equal strings, write as unchanged.
		if inA && inB && isType(valueInA, "string") && isType(valueInB, "string") {
			if valueInA.(string) == valueInB.(string) {
				writeUnchanged(key, valueInA.(string), buf)
				continue
			}
		}

		// If the key is in a but not in b, write as removed.
		if inA && !inB {
			writeRemoved(key, valueInA, buf)
			continue
		}

		// If the key is in b but not in a, write as added.
		if !inA && inB {
			writeAdded(key, valueInB, buf)
			continue
		}

		// If the key is in both a and b, compare the values recursively.
		if a[key] != b[key] {
			diffRecursively(a[key].(map[string]interface{}), b[key].(map[string]interface{}), buf)
		}
	}
}

func isType(value interface{}, expectedType string) bool {
	switch value.(type) {
	case string:
		return expectedType == "string"
	case int:
		return expectedType == "int"
	case float64:
		return expectedType == "float64"
	case bool:
		return expectedType == "bool"
	case map[string]interface{}:
		return expectedType == "map"
	case []interface{}:
		return expectedType == "array"
	default:
		return false
	}
}

func writeUnchanged(key, value string, buf *strings.Builder) {
	buf.WriteString("  ")
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(value)
	buf.WriteString("\n")
}

func writeRemoved(key string, value interface{}, buf *strings.Builder) {
	buf.WriteString("- ")
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(value.(string))
	buf.WriteString("\n")
}

func writeAdded(key string, value interface{}, buf *strings.Builder) {
	buf.WriteString("+ ")
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(value.(string))
	buf.WriteString("\n")
}

func coerceToString(value interface{}) string {
	return ""
}
