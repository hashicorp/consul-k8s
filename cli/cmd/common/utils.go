package common

import "strings"

// Abort returns true if the raw input string is not equal to "y" or "yes".
func Abort(raw string) bool {
	confirmation := strings.TrimSuffix(raw, "\n")
	if !(strings.ToLower(confirmation) == "y" || strings.ToLower(confirmation) == "yes") {
		return true
	}
	return false
}
