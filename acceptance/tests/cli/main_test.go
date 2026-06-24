// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	// If the consul-k8s binary is not found in PATH, build it from source.
	// This handles CI/CD runners (e.g. OCP) where make cli-dev was not run.
	if _, err := exec.LookPath("consul-k8s"); err != nil {
		gopath, err := exec.Command("go", "env", "GOPATH").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "consul-k8s not found in PATH and cannot determine GOPATH: %v\n", err)
			os.Exit(1)
		}
		binPath := filepath.Join(strings.TrimSpace(string(gopath)), "bin", "consul-k8s")
		fmt.Fprintf(os.Stdout, "consul-k8s not found in PATH, building from source to %s\n", binPath)

		// Tests run from acceptance/tests/cli/; the cli source is at ../../../cli.
		// We must cd into the cli module directory and build "." because
		// acceptance/ is a separate Go module (cross-module paths are rejected).
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get working directory: %v\n", err)
			os.Exit(1)
		}
		cliDir := filepath.Join(cwd, "..", "..", "..", "cli")
		buildCmd := exec.Command("go", "build", "-o", binPath, ".")
		buildCmd.Dir = cliDir
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to build consul-k8s: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "consul-k8s built successfully\n")
	}
	suite = testsuite.NewSuite(m)
	os.Exit(suite.Run())
}
