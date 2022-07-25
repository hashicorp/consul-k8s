package cli

import (
	"fmt"
	"os/exec"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
)

// CLI provides access to compile and execute commands with the `consul-k8s` CLI.
type CLI struct {
	initialized bool
}

// NewCLI compiles the `consul-k8s` CLI and returns a handle to execute commands
// with the binary.
func NewCLI() (*CLI, error) {
	cmd := exec.Command("go", "install", ".")
	cmd.Dir = config.CLIPath
	_, err := cmd.Output()
	return &CLI{true}, err
}

// Run runs the CLI with the given args.
func (c *CLI) Run(args ...string) ([]byte, error) {
	if !c.initialized {
		return nil, fmt.Errorf("CLI must be initialized before calling Run, use `cli.NewCLI()` to initialize.")
	}
	cmd := exec.Command("cli", args...)
	return cmd.Output()
}
