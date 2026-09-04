// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"fmt"
	"os"
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
}

// NewCLI returns a handle to execute commands with the consul-k8s binary.
func NewCLI() (*CLI, error) {
	return &CLI{true}, nil
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

	logger.Logf(t, "Running `consul-k8s %s`", strings.Join(args, " "))
	cmd := exec.Command(cliBinaryName, args...)
	// Disable Helm's client-side OpenAPI schema validation for CLI-driven installs
	// and upgrades. On large clusters (for example OpenShift/ROSA) validating the
	// manifest downloads the full OpenAPI schema from the API server, a big transfer
	// that intermittently fails with "connection reset by peer" and flakes the
	// install. The API server still validates resources on apply, so this only
	// removes a flaky client-side step. Honored by consul-k8s that supports the
	// CONSUL_K8S_SKIP_OPENAPI_VALIDATION environment variable.
	cmd.Env = append(os.Environ(), "CONSUL_K8S_SKIP_OPENAPI_VALIDATION=true")
	return cmd.Output()
}
