package helm

import (
	"fmt"
	"strings"

	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"helm.sh/helm/v3/pkg/action"
)

// CreateLogger creates a Helm logger from the terminal UI and allows for
// verbosity to be set.
func CreateLogger(ui terminal.UI, verbose bool) action.DebugLog {
	return func(s string, args ...interface{}) {
		msg := fmt.Sprintf(s, args...)

		if verbose {
			ui.Output(msg, terminal.WithLibraryStyle())
		} else {
			if !strings.Contains(msg, "not ready") {
				ui.Output(msg, terminal.WithLibraryStyle())
			}
		}
	}
}
