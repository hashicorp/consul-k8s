// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"fmt"
	"os/exec"

	"github.com/gruntwork-io/terratest/modules/k8s"
)

const (
	cliBinaryName = "consul-k8s"
)

// CLI provides access to compile and execute commands with the `consul-k8s` CLI.
type CLI struct {
	initialized bool
}

// NewCLI returns a handle to execute commands with the consul-k8s binary.
func NewCLI() (*CLI, error) {
	return &CLI{true}, nil
}

// Run runs the CLI with the given args.
func (c *CLI) Run(options *k8s.KubectlOptions, args ...string) ([]byte, error) {
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

	cmd := exec.Command(cliBinaryName, args...)
	return cmd.Output()
}
