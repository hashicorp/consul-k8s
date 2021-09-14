package common

import "strings"

func PrefixLines(prefix, lines string) string {
	var prefixedLines string
	for _, l := range strings.Split(lines, "\n") {
		prefixedLines += prefix + l + "\n"
	}
	return strings.TrimSuffix(prefixedLines, "\n")
}
