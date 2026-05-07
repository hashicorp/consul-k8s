// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package suite

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/flags"
)

type suite struct {
	m     *testing.M
	env   *environment.KubernetesEnvironment
	cfg   *config.TestConfig
	flags *flags.TestFlags
}

type Suite interface {
	Run() int
	Environment() environment.TestEnvironment
	Config() *config.TestConfig
}

func NewSuite(m *testing.M) Suite {
	flags := flags.NewTestFlags()

	flag.Parse()

	testConfig := flags.TestConfigFromFlags()

	return &suite{
		m:     m,
		env:   environment.NewKubernetesEnvironmentFromConfig(testConfig),
		cfg:   testConfig,
		flags: flags,
	}
}

func (s *suite) Run() int {
	err := s.flags.Validate()
	if err != nil {
		fmt.Printf("Flag validation failed: %s\n", err)
		return 1
	}

	// Create test debug directory if it doesn't exist
	if s.cfg.DebugDirectory == "" {
		var err error
		s.cfg.DebugDirectory, err = os.MkdirTemp("", "consul-test")
		if err != nil {
			fmt.Printf("Failed to create debug directory: %s\n", err)
			return 1
		}
	}

	// Pre-apply all Consul CRDs once before any test runs. Individual Helm
	// installs use global.installCRDs=false so they never claim CRD ownership,
	// allowing multiple releases to coexist on the same cluster without
	// annotation ownership conflicts. This is what enables t.Parallel().
	if err := installCRDs(s.cfg); err != nil {
		fmt.Printf("Failed to pre-install CRDs: %s\n", err)
		return 1
	}

	return s.m.Run()
}

func (s *suite) Environment() environment.TestEnvironment {
	return s.env
}

func (s *suite) Config() *config.TestConfig {
	return s.cfg
}

// installCRDs renders all Consul CRDs from the local chart via helm template
// and applies them to the cluster using server-side apply. Using --force-conflicts
// ensures idempotency: re-running against an existing cluster updates CRDs cleanly
// regardless of which field manager last touched them.
func installCRDs(cfg *config.TestConfig) error {
	// Enable every feature flag that gates a CRD template. global.tls.enabled and
	// meshGateway.enabled are required by helm's validation when peering is on;
	// they do not affect which CRD documents are emitted.
	helmArgs := []string{
		"template", "crd-pre-install", config.HelmChartPath,
		"--set", "global.installCRDs=true",
		"--set", "connectInject.enabled=true",
		"--set", "global.peering.enabled=true",
		"--set", "global.tls.enabled=true",
		"--set", "meshGateway.enabled=true",
		"--set", "global.installK8sNetworkingCRDs=true",
		"--set", "connectInject.apiGateway.manageExternalCRDs=true",
		"--set", "connectInject.apiGateway.manageNonStandardCRDs=true",
	}

	helmCmd := exec.Command("helm", helmArgs...)
	var helmStderr bytes.Buffer
	helmCmd.Stderr = &helmStderr
	helmOut, err := helmCmd.Output()
	if err != nil {
		return fmt.Errorf("helm template for CRDs: %w\n%s", err, helmStderr.String())
	}

	// Filter to only CustomResourceDefinition documents; the full render includes
	// Deployments, ServiceAccounts, etc. that we must not apply to the cluster.
	crdOnly := filterCRDs(helmOut)

	kubectlArgs := []string{"apply", "--server-side", "--force-conflicts", "-f", "-"}
	primaryEnv := cfg.GetPrimaryKubeEnv()
	if primaryEnv.KubeConfig != "" {
		kubectlArgs = append([]string{"--kubeconfig", primaryEnv.KubeConfig}, kubectlArgs...)
	}
	if primaryEnv.KubeContext != "" {
		kubectlArgs = append([]string{"--context", primaryEnv.KubeContext}, kubectlArgs...)
	}

	kubectl := exec.Command("kubectl", kubectlArgs...)
	kubectl.Stdin = bytes.NewReader(crdOnly)
	if out, err := kubectl.CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl apply CRDs: %w\n%s", err, out)
	}

	return nil
}

// filterCRDs extracts only CustomResourceDefinition documents from a multi-document YAML manifest.
func filterCRDs(manifest []byte) []byte {
	var crds []string
	for _, doc := range strings.Split(string(manifest), "\n---") {
		if strings.Contains(doc, "kind: CustomResourceDefinition") {
			crds = append(crds, doc)
		}
	}
	return []byte(strings.Join(crds, "\n---"))
}
