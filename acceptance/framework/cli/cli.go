package cli

import (
	"os/exec"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
)

// RunCmd compiles and runs the consul-k8s CLI with the given arguments.  
func RunCmd(args ...string) ([]byte, error) {
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	cmd.Dir = config.CLIPath
	return cmd.Output()
}
