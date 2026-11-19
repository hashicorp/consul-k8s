// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil"
)

const (
	cliBinaryName = "consul-k8s"
)

// CLI provides access to compile and execute commands with the `consul-k8s` CLI.
type CLI struct {
	initialized bool
	// OpenShift passes the KubectlOptions namespace to namespaced commands and
	// includes stderr in the output of failed commands.
	OpenShift bool
}

// NewCLI returns a handle to execute commands with the consul-k8s binary.
func NewCLI() (*CLI, error) {
	return &CLI{initialized: true}, nil
}

// Run runs the CLI with the given args.
func (c *CLI) Run(t testutil.TestingTB, options *k8s.KubectlOptions, args ...string) ([]byte, error) {
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
	if c.OpenShift {
		args = appendNamespace(args, options.Namespace)
	}

	logger.Logf(t, "Running `consul-k8s %s`", strings.Join(args, " "))
	cmd := exec.Command(cliBinaryName, args...)
	if !c.OpenShift {
		return cmd.Output()
	}
	output, err := cmd.Output()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		output = append(output, exitError.Stderr...)
	}
	return output, err
}

// appendNamespace passes namespace to commands that accept -namespace unless
// the caller already set it. OpenShift test contexts use a non-default namespace.
func appendNamespace(args []string, namespace string) []string {
	if namespace == "" || len(args) == 0 {
		return args
	}
	switch args[0] {
	case "install", "uninstall", "proxy", "troubleshoot":
	default:
		return args
	}
	for _, arg := range args {
		if arg == "-namespace" || arg == "--namespace" || strings.HasPrefix(arg, "-namespace=") || strings.HasPrefix(arg, "--namespace=") {
			return args
		}
	}
	return append(args, "-namespace", namespace)
}
