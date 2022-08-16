package cli

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
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
func (c *CLI) Run(t *testing.T, options *k8s.KubectlOptions, args ...string) ([]byte, error) {
	if !c.initialized {
		return nil, fmt.Errorf("CLI must be initialized before calling Run, use `cli.NewCLI()` to initialize.")
	}

	// Append configuration from `options` to the command.
	if options.ConfigPath != "" {
		args = append(args, "-kubeconfig", options.ConfigPath)
	}
	if options.ContextName != "" {
		args = append(args, "-context", options.ContextName)
	}

	logger.Logf(t, "Running `consul-k8s %s`", strings.Join(args, " "))
	cmd := exec.Command("cli", args...)
	return cmd.Output()
}
