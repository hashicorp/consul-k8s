// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimefake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
)

// Test that if TestConfig has values that need to be provided
// to a helm install, it will respect the helmValues over
// the values from TestConfig.
func TestNewHelmCluster(t *testing.T) {
	tests := []struct {
		name       string
		helmValues map[string]string
		want       map[string]string
	}{
		{
			name:       "defaults are used when no helmValues are set",
			helmValues: map[string]string{},
			want: map[string]string{
				"global.image":    "test-config-image",
				"global.logLevel": "debug",
				"server.replicas": "1",
				"connectInject.transparentProxy.defaultEnabled":                          "false",
				"connectInject.sidecarProxy.lifecycle.defaultShutdownGracePeriodSeconds": "1",
				"dns.enabled":        "false",
				"server.extraConfig": `"{\"log_level\": \"TRACE\"}"`,
				"client.extraConfig": `"{\"log_level\": \"TRACE\"}"`,
			},
		},
		{
			name: "when using helmValues, defaults are overridden",
			helmValues: map[string]string{
				"global.image":           "test-image",
				"global.logLevel":        "debug",
				"server.bootstrapExpect": "3",
				"server.replicas":        "3",
				"connectInject.transparentProxy.defaultEnabled":                          "true",
				"connectInject.sidecarProxy.lifecycle.defaultShutdownGracePeriodSeconds": "3",
				"dns.enabled":        "true",
				"server.extraConfig": `"{\"foo\": \"bar\"}"`,
				"client.extraConfig": `"{\"foo\": \"bar\"}"`,
				"feature.enabled":    "true",
			},
			want: map[string]string{
				"global.image":           "test-image",
				"global.logLevel":        "debug",
				"server.bootstrapExpect": "3",
				"server.replicas":        "3",
				"connectInject.transparentProxy.defaultEnabled":                          "true",
				"connectInject.sidecarProxy.lifecycle.defaultShutdownGracePeriodSeconds": "3",
				"dns.enabled":        "true",
				"server.extraConfig": `"{\"foo\": \"bar\"}"`,
				"client.extraConfig": `"{\"foo\": \"bar\"}"`,
				"feature.enabled":    "true",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := NewHelmCluster(t, tt.helmValues, &ctx{}, &config.TestConfig{ConsulImage: "test-config-image"}, "test")
			require.Equal(t, cluster.helmOptions.SetValues, tt.want)
		})
	}
}

func TestHelmCluster_PrepareCRDs(t *testing.T) {
	for _, openshift := range []bool{false, true} {
		name := "non-OpenShift"
		if openshift {
			name = "OpenShift"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			commandLog := filepath.Join(dir, "commands")
			// No real kubectl or cluster is used. Record any CRD cleanup calls.
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CRD_COMMAND_LOG\"\nprintf '{\"items\":[]}'\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700))
			t.Setenv("PATH", dir)
			t.Setenv("CRD_COMMAND_LOG", commandLog)
			cluster := NewHelmCluster(t, nil, &ctx{}, &config.TestConfig{EnableOpenshift: openshift}, "test")
			cluster.prepareCRDs(t)
			if openshift {
				require.NoFileExists(t, commandLog, "OpenShift startup must not inspect or strip CRD finalizers")
			} else {
				calls, err := os.ReadFile(commandLog)
				require.NoError(t, err)
				require.Equal(t, "get crd -o=json\n", string(calls))
			}
		})
	}
}

func TestNewHelmCluster_OpenShiftCRDValues(t *testing.T) {
	for _, installCRDs := range []string{"", "true"} {
		t.Run("explicit-install="+installCRDs, func(t *testing.T) {
			values := map[string]string{
				"global.tls.enabled":                   "true",
				"global.tls.enableAutoEncrypt":         "true",
				"global.acls.manageSystemACLs":         "true",
				"global.gossipEncryption.autoGenerate": "true",
			}
			if installCRDs != "" {
				values["global.installK8sNetworkingCRDs"] = installCRDs
			}
			cluster := NewHelmCluster(t, values, &ctx{}, &config.TestConfig{EnableOpenshift: true}, "test")
			require.Equal(t, "true", cluster.helmOptions.SetValues["global.openshift.enabled"])
			require.Subset(t, cluster.helmOptions.SetValues, values)
			if installCRDs == "" {
				require.Equal(t, "false", cluster.helmOptions.SetValues["global.installK8sNetworkingCRDs"])
			}
		})
	}
}

func TestHelmCluster_OpenShiftPreflightFailure(t *testing.T) {
	const helperEnv = "CONSUL_K8S_TEST_PREFLIGHT_HELPER"
	if os.Getenv(helperEnv) == "1" {
		cluster := NewHelmCluster(t, nil, &ctx{}, &config.TestConfig{EnableOpenshift: true}, "never-installed")
		// Any accidental cleanup must fail locally rather than load real credentials.
		cluster.helmOptions.KubectlOptions.ConfigPath = filepath.Join(t.TempDir(), "no-kubeconfig")
		cluster.Create(t)
		t.Fatal("expected the prior-installation guard to reject the run")
		return
	}

	dir := t.TempDir()
	commandLog := filepath.Join(dir, "helm-commands")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$PREFLIGHT_COMMAND_LOG"
case "$1" in
  list) printf '[{"name":"existing-release","chart":"consul-helm-1.8"}]\n' ;;
  *) printf 'unexpected Helm command\n' >&2; exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helm"), []byte(script), 0700))
	t.Setenv("PATH", dir)
	t.Setenv("PREFLIGHT_COMMAND_LOG", commandLog)
	t.Setenv(helperEnv, "1")
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestHelmCluster_OpenShiftPreflightFailure$", "-test.v")
	output, err := cmd.CombinedOutput()
	require.Error(t, err, string(output))
	require.NoError(t, ctx.Err(), string(output))
	require.Contains(t, string(output), "detected an existing installation")
	require.Equal(t, 1, strings.Count(string(output), "Error Trace:"), string(output))
	require.NotContains(t, string(output), "cleaning up resources")
	calls, err := os.ReadFile(commandLog)
	require.NoError(t, err)
	require.Contains(t, string(calls), "list")
	require.NotContains(t, string(calls), "upgrade")
	require.NotContains(t, string(calls), "delete")
}

type ctx struct{}

func (c *ctx) Name() string {
	return ""
}

func (c *ctx) KubectlOptions(_ testutil.TestingTB) *k8s.KubectlOptions {
	return &k8s.KubectlOptions{}
}
func (c *ctx) KubectlOptionsForNamespace(ns string) *k8s.KubectlOptions {
	return &k8s.KubectlOptions{}
}
func (c *ctx) KubernetesClient(_ testutil.TestingTB) kubernetes.Interface {
	return fake.NewSimpleClientset()
}
func (c *ctx) ControllerRuntimeClient(_ testutil.TestingTB) client.Client {
	return runtimefake.NewClientBuilder().Build()
}

var _ environment.TestContext = (*ctx)(nil)
