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
	// Use "consul"/"consul" as release/namespace so the Helm ownership labels
	// on the pre-installed CRDs match what `consul-k8s install` expects.
	// HelmCluster tests are unaffected because they set installCRDs=false and
	// never attempt to claim CRD ownership via Helm.
	helmArgs := []string{
		"template", "consul", config.HelmChartPath,
		"--namespace", "consul",
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

	// Apply CRDs to every configured cluster context so that multi-cluster
	// tests (partitions, peering, wan-federation) have CRDs available on all
	// secondary clusters. The gateway-resources post-upgrade hook job runs on
	// whichever cluster helm installs to, so each cluster needs the CRDs.
	for _, env := range cfg.KubeEnvs {
		ctxArgs := []string{}
		if env.KubeConfig != "" {
			ctxArgs = append(ctxArgs, "--kubeconfig", env.KubeConfig)
		}
		if env.KubeContext != "" {
			ctxArgs = append(ctxArgs, "--context", env.KubeContext)
		}

		kubectl := exec.Command("kubectl", append(ctxArgs, "apply", "--server-side", "--force-conflicts", "-f", "-")...)
		kubectl.Stdin = bytes.NewReader(crdOnly)
		if out, err := kubectl.CombinedOutput(); err != nil {
			return fmt.Errorf("kubectl apply CRDs to context %s: %w\n%s", env.KubeContext, err, out)
		}

		// helm template does not emit app.kubernetes.io/managed-by or the
		// meta.helm.sh/* annotations. Add them so that consul-k8s install
		// (CLI tests) and helm install/upgrade can adopt the pre-installed CRDs
		// without "invalid ownership metadata" errors.
		sel := "release=consul,component=crd"
		label := exec.Command("kubectl", append(ctxArgs,
			"label", "--overwrite", "crds", "-l", sel,
			"app.kubernetes.io/managed-by=Helm")...)
		if out, err := label.CombinedOutput(); err != nil {
			return fmt.Errorf("kubectl label CRDs in context %s: %w\n%s", env.KubeContext, err, out)
		}

		annotate := exec.Command("kubectl", append(ctxArgs,
			"annotate", "--overwrite", "crds", "-l", sel,
			"meta.helm.sh/release-name=consul",
			"meta.helm.sh/release-namespace=consul")...)
		if out, err := annotate.CombinedOutput(); err != nil {
			return fmt.Errorf("kubectl annotate CRDs in context %s: %w\n%s", env.KubeContext, err, out)
		}
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
