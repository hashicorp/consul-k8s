package cli

import (
	"fmt"
	"strings"
)

// TranslateListOutput takes the raw output from the proxy list command and
// translates the table into a map.
func TranslateListOutput(raw []byte) map[string]string {
	formatted := make(map[string]string)
	for _, pod := range strings.Split(strings.TrimSpace(string(raw)), "\n")[2:] {
		row := strings.Split(strings.TrimSpace(pod), "\t")

		var name string
		if len(row) == 3 { // Handle the case where namespace is present
			name = fmt.Sprintf("%s/%s", strings.TrimSpace(row[0]), strings.TrimSpace(row[1]))
		} else if len(row) == 2 {
			name = strings.TrimSpace(row[0])
		}
		formatted[name] = row[len(row)-1]
	}

	return formatted
}
