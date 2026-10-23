// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOCPCleanupScript(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required by the OCP cleanup script")
	}
	for _, tc := range []struct {
		name       string
		args       []string
		scenario   string
		wantError  string
		dryRun     bool
		noCommands bool
		dnsBackup  bool
		largeCRDs  bool
	}{
		{name: "scoped cleanup", args: []string{"--context", "test-cluster"}},
		{name: "dry run", args: []string{"--context", "test-cluster", "--dry-run"}, dryRun: true},
		{name: "unlabeled namespace", args: []string{"--context", "test-cluster"}, scenario: "unlabeled", wantError: "must be explicitly labeled"},
		{name: "foreign release", args: []string{"--context", "test-cluster"}, scenario: "foreign", wantError: "non-test Helm releases"},
		{name: "protected namespace", args: []string{"--context", "test-cluster", "--namespace", "openshift-dns"}, wantError: "protected namespace", noCommands: true},
		{name: "missing context", wantError: "explicit Kubernetes context", noCommands: true},
		{name: "flag as namespace", args: []string{"--context", "test-cluster", "--namespace", "--all"}, wantError: "Invalid test namespace", noCommands: true},
		{name: "recover DNS backup", args: []string{"--context", "test-cluster"}, scenario: "dns", dnsBackup: true},
		{name: "refuse changed DNS forwarder", args: []string{"--context", "test-cluster"}, scenario: "dns-drift", dnsBackup: true, wantError: "refusing to overwrite"},
		{name: "owned resources in default namespace", args: []string{"--context", "test-cluster"}, scenario: "default-owned"},
		{name: "large OCP CRD listing", args: []string{"--context", "test-cluster"}, largeCRDs: true},
		{name: "terminating CRD instances keep finalizers", args: []string{"--context", "test-cluster"}, scenario: "stuck-instances"},
		{name: "CRD removed before instance listing", args: []string{"--context", "test-cluster"}, scenario: "vanished-crd"},
		{name: "Consul installed outside the test namespaces", args: []string{"--context", "test-cluster"}, scenario: "foreign-injector"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			calls := filepath.Join(directory, "calls")
			t.Setenv("OCP_TEST_CALLS", calls)
			t.Setenv("OCP_TEST_SCENARIO", tc.scenario)
			crdsPath := filepath.Join(directory, "crds.json")
			crds := ocpCleanupCRDs
			if tc.largeCRDs {
				crds = strings.TrimSuffix(crds, "}") + `,"padding":"` + strings.Repeat("x", 512*1024) + `"}`
			}
			require.NoError(t, os.WriteFile(crdsPath, []byte(crds), 0600))
			t.Setenv("OCP_TEST_CRDS_FILE", crdsPath)
			t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "kubectl"), []byte(ocpCleanupKubectlStub), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "helm"), []byte(ocpCleanupHelmStub), 0700))
			args := append([]string{}, tc.args...)
			backupPath := filepath.Join(directory, "ocp-dns-backup.json")
			if tc.dnsBackup {
				require.NoError(t, os.WriteFile(backupPath, []byte(`{"context":"test-cluster","namespace":"consul","servers":null,"upstream":"10.0.0.1"}`), 0600))
				args = append(args, "--debug-directory", directory)
			}
			command := exec.Command("bash", append([]string{"../../../hack/cleanup-ocp-tests.sh"}, args...)...)
			output, err := command.CombinedOutput()
			if tc.wantError != "" {
				require.Error(t, err, string(output))
				require.Contains(t, string(output), tc.wantError)
			} else {
				require.NoError(t, err, string(output))
			}
			commands, readErr := os.ReadFile(calls)
			if tc.noCommands {
				require.ErrorIs(t, readErr, os.ErrNotExist)
				return
			}
			require.NoError(t, readErr)
			if tc.dnsBackup {
				_, backupErr := os.Stat(backupPath)
				if tc.wantError == "" {
					require.ErrorIs(t, backupErr, os.ErrNotExist)
					require.Contains(t, string(commands), `"resourceVersion":"7"`)
					require.Contains(t, string(commands), `"name":"unrelated"`)
					require.NotContains(t, string(commands), `"name":"consul-acceptance"`)
				} else {
					require.NoError(t, backupErr, "conflicting DNS updates must retain their recovery backup")
				}
			}
			if tc.dryRun || tc.wantError != "" {
				require.NotContains(t, string(commands), " delete ")
				require.NotContains(t, string(commands), " uninstall ")
				return
			}
			require.Contains(t, string(commands), "delete crd meshes.consul.hashicorp.com")
			require.Contains(t, string(commands), "delete namespace test-apps")
			require.Contains(t, string(commands), "delete customgatewayclasses.consul.hashicorp.com stale-managed-class")
			require.NotContains(t, string(commands), "delete customgatewayclasses.consul.hashicorp.com foreign-class")
			require.NotContains(t, string(commands), "delete crd gatewayclasses.gateway.networking.k8s.io")
			require.NotContains(t, string(commands), "delete crd servicedefaults.consul.hashicorp.com")
			require.NotContains(t, string(commands), "delete namespace default")
			if tc.scenario == "default-owned" {
				require.Contains(t, string(commands), "delete meshes.consul.hashicorp.com/test-mesh --namespace default")
			}
			clearFinalizers := `--type=merge --patch {"metadata":{"finalizers":[]}}`
			for _, gatewayClass := range []string{"other-controller-class", "foreign-release-class", "foreign-helm-class"} {
				require.NotContains(t, string(commands), "gatewayclasses.gateway.networking.k8s.io "+gatewayClass)
			}
			if tc.scenario == "foreign-injector" {
				require.NotContains(t, string(commands), "get gatewayclasses.gateway.networking.k8s.io")
				require.Contains(t, string(output), "Consul also runs in: other-consul.")
			} else {
				staleClassPatch := strings.Index(string(commands), "patch gatewayclasses.gateway.networking.k8s.io stale-test-class "+clearFinalizers)
				require.NotEqual(t, -1, staleClassPatch, string(commands))
				require.Less(t, staleClassPatch, strings.Index(string(commands), "delete gatewayclasses.gateway.networking.k8s.io stale-test-class"))
				require.Less(t, strings.Index(string(commands), "helm --kube-context test-cluster uninstall"), staleClassPatch)
			}
			switch tc.scenario {
			case "stuck-instances":
				clusterPatch := strings.Index(string(commands), "patch customgatewayclasses.consul.hashicorp.com global "+clearFinalizers)
				namespacedPatch := strings.Index(string(commands), "patch meshes.consul.hashicorp.com stuck "+clearFinalizers+" --namespace other-apps")
				require.NotEqual(t, -1, clusterPatch, string(commands))
				require.NotEqual(t, -1, namespacedPatch, string(commands))
				require.Less(t, clusterPatch, strings.Index(string(commands), "delete crd customgatewayclasses.consul.hashicorp.com"))
				require.Less(t, namespacedPatch, strings.Index(string(commands), "delete crd meshes.consul.hashicorp.com"))
				require.NotContains(t, string(commands), "patch meshes.consul.hashicorp.com unblocked")
			case "vanished-crd":
				require.Contains(t, string(commands), "get crd customgatewayclasses.consul.hashicorp.com --ignore-not-found -o name")
				require.NotContains(t, string(commands), "delete crd customgatewayclasses.consul.hashicorp.com")
			default:
				require.Contains(t, string(commands), "delete crd customgatewayclasses.consul.hashicorp.com")
			}
			for _, line := range strings.Split(strings.TrimSpace(string(commands)), "\n") {
				if strings.HasPrefix(line, "helm ") {
					require.Contains(t, line, "--kube-context test-cluster")
				} else {
					require.Contains(t, line, "--context test-cluster")
				}
			}
			require.Contains(t, string(output), "stub warning", "successful stderr must not corrupt JSON parsing")
		})
	}
}

func TestOCPAcceptanceRunner(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required by the OCP cleanup script")
	}
	for _, tc := range []struct {
		name             string
		packages         []string
		failTest         bool
		transparentProxy string
		wantError        string
		testCount        int
		cleanupCount     int
	}{
		{name: "serial single-cluster packages", packages: []string{"basic", "connect"}, testCount: 2, cleanupCount: 2},
		{name: "fail fast", packages: []string{"basic", "connect"}, failTest: true, wantError: "stub test failure", testCount: 1, cleanupCount: 1},
		{name: "reject multi-cluster packages", packages: []string{"peering"}, wantError: "Unsupported single-cluster acceptance package"},
		{name: "default package list", testCount: 16, cleanupCount: 16},
		{name: "transparent proxy disabled", packages: []string{"server"}, transparentProxy: "false", testCount: 1, cleanupCount: 1},
		{name: "reject invalid transparent proxy setting", packages: []string{"server"}, transparentProxy: "maybe", wantError: "ENABLE_TRANSPARENT_PROXY must be true or false."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			calls := filepath.Join(directory, "calls")
			t.Setenv("OCP_TEST_CALLS", calls)
			t.Setenv("OCP_TEST_SCENARIO", "")
			crdsPath := filepath.Join(directory, "crds.json")
			require.NoError(t, os.WriteFile(crdsPath, []byte(ocpCleanupCRDs), 0600))
			t.Setenv("OCP_TEST_CRDS_FILE", crdsPath)
			t.Setenv("OCP_TEST_FAIL", "false")
			if tc.failTest {
				t.Setenv("OCP_TEST_FAIL", "true")
			}
			t.Setenv("KUBE_CONTEXT", "test-cluster")
			t.Setenv("KUBE_NAMESPACE", "consul")
			t.Setenv("CONSUL_ENT_LICENSE", "test-license")
			t.Setenv("KUBECONFIG_FILE", "")
			t.Setenv("DEBUG_DIRECTORY", filepath.Join(directory, "debug"))
			t.Setenv("ENABLE_CNI", "false")
			t.Setenv("ENABLE_TRANSPARENT_PROXY", tc.transparentProxy)
			t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "kubectl"), []byte(ocpCleanupKubectlStub), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "helm"), []byte(ocpCleanupHelmStub), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "go"), []byte("#!/bin/sh\necho \"go $*\" >> \"$OCP_TEST_CALLS\"\nif [ \"$OCP_TEST_FAIL\" = true ]; then echo 'stub test failure' >&2; exit 1; fi\n"), 0700))
			output, err := exec.Command("bash", append([]string{"../../../hack/run-ocp-acceptance.sh"}, tc.packages...)...).CombinedOutput()
			if tc.wantError != "" {
				require.Error(t, err, string(output))
				require.Contains(t, string(output), tc.wantError)
			} else {
				require.NoError(t, err, string(output))
			}
			commands, readErr := os.ReadFile(calls)
			if tc.testCount == 0 {
				require.ErrorIs(t, readErr, os.ErrNotExist)
				return
			}
			require.NoError(t, readErr)
			tests := 0
			cleanups := 0
			for _, line := range strings.Split(strings.TrimSpace(string(commands)), "\n") {
				if strings.Contains(line, "delete namespace consul ") {
					cleanups++
				}
				if !strings.HasPrefix(line, "go test ") {
					continue
				}
				tests++
				require.Equal(t, tests, cleanups, "cleanup must precede each test package")
				require.Contains(t, line, " -p 1 ")
				require.Contains(t, line, " -failfast ")
				require.Contains(t, line, "-kube-contexts=test-cluster")
				require.NotContains(t, line, "-enable-multi-cluster")
				require.Equal(t, tc.transparentProxy != "false", slices.Contains(strings.Fields(line), "-enable-transparent-proxy"))
			}
			require.Equal(t, tc.testCount, tests)
			require.Equal(t, tc.cleanupCount, cleanups)
		})
	}
}

const ocpCleanupKubectlStub = `#!/usr/bin/env bash
set -eu
echo "kubectl $*" >> "$OCP_TEST_CALLS"
case "$*" in
  *"get clusterversion "*) echo "4.22.18" ;;
  *"get namespace consul --ignore-not-found -o json"*)
    if [[ "$OCP_TEST_SCENARIO" == "unlabeled" ]]; then
      echo '{"metadata":{"name":"consul"}}'
    else
      echo '{"metadata":{"name":"consul","labels":{"consul.hashicorp.com/acceptance-tests":"true"}}}'
    fi ;;
  *"get namespaces -l "*)
    echo '{"items":[{"metadata":{"name":"test-apps"}}]}' ;;
  *"get dns.operator.openshift.io default -o json"*)
    upstream="10.0.0.1"
    if [[ "$OCP_TEST_SCENARIO" == "dns-drift" ]]; then upstream="10.0.0.9"; fi
    echo '{"metadata":{"resourceVersion":"7"},"spec":{"servers":[{"name":"consul-acceptance","zones":["consul"],"forwardPlugin":{"upstreams":["'"$upstream"'"]}},{"name":"unrelated","zones":["example.com"],"forwardPlugin":{"upstreams":["10.0.0.2"]}}]}}' ;;
  *"api-resources --api-group=consul.hashicorp.com --namespaced=true"*) echo "meshes.consul.hashicorp.com" ;;
  *"api-resources --api-group=consul.hashicorp.com --namespaced=false"*) echo "customgatewayclasses.consul.hashicorp.com" ;;
  *"api-resources "*) ;;
  *"get meshes.consul.hashicorp.com --namespace default -o name --selector consul.hashicorp.com/acceptance-namespace=consul"*)
    if [[ "$OCP_TEST_SCENARIO" == "default-owned" ]]; then echo "meshes.consul.hashicorp.com/test-mesh"; fi ;;
  *"get meshes.consul.hashicorp.com --namespace consul -o name"*|*"get meshes.consul.hashicorp.com --namespace test-apps -o name"*) ;;
  *"get crd -o json"*)
    echo "stub warning" >&2
    cat "$OCP_TEST_CRDS_FILE" ;;
  *"get customgatewayclasses.consul.hashicorp.com -o json"*)
    echo '{"items":[{"metadata":{"name":"stale-managed-class","labels":{"release":"test-old"}}},{"metadata":{"name":"foreign-class","labels":{"release":"another-release"}}}]}' ;;
  *"get customgatewayclasses.consul.hashicorp.com --all-namespaces -o json"*)
    case "$OCP_TEST_SCENARIO" in
      stuck-instances) echo '{"items":[{"metadata":{"name":"global","deletionTimestamp":"2026-01-01T00:00:00Z","finalizers":["finalizers.consul.hashicorp.com"]}}]}' ;;
      vanished-crd) echo 'error: the server doesn'"'"'t have a resource type "customgatewayclasses"' >&2; exit 1 ;;
      *) echo '{"items":[]}' ;;
    esac ;;
  *"get meshes.consul.hashicorp.com --all-namespaces -o json"*)
    if [[ "$OCP_TEST_SCENARIO" == "stuck-instances" ]]; then
      echo '{"items":[{"metadata":{"name":"stuck","namespace":"other-apps","finalizers":["finalizers.consul.hashicorp.com"]}},{"metadata":{"name":"unblocked","namespace":"other-apps"}}]}'
    else
      echo '{"items":[]}'
    fi ;;
  *"get crd customgatewayclasses.consul.hashicorp.com --ignore-not-found -o name"*) ;;
  *"get deployments --all-namespaces -l app=consul,component=connect-injector -o json"*)
    other=""
    if [[ "$OCP_TEST_SCENARIO" == "foreign-injector" ]]; then other=',{"metadata":{"name":"consul-connect-injector","namespace":"other-consul"}}'; fi
    echo '{"items":[{"metadata":{"name":"consul-connect-injector","namespace":"consul"}}'"$other"']}' ;;
  *"get gatewayclasses.gateway.networking.k8s.io -o json"*)
    echo '{"items":[{"metadata":{"name":"stale-test-class","finalizers":["gateway-exists-finalizer.consul.hashicorp.com"]},"spec":{"controllerName":"consul.hashicorp.com/gateway-controller"}},{"metadata":{"name":"other-controller-class"},"spec":{"controllerName":"openshift.io/gateway-controller/v1"}},{"metadata":{"name":"foreign-release-class","labels":{"release":"another-release"}},"spec":{"controllerName":"consul.hashicorp.com/gateway-controller"}},{"metadata":{"name":"foreign-helm-class","annotations":{"meta.helm.sh/release-namespace":"other-consul"}},"spec":{"controllerName":"consul.hashicorp.com/gateway-controller"}}]}' ;;
  *" -o json") echo '{"items":[]}' ;;
  *"delete "*|*"create namespace "*|*"label namespace "*|*"patch dns.operator.openshift.io "*|*"patch meshes.consul.hashicorp.com"*|*"patch customgatewayclasses.consul.hashicorp.com "*|*"patch gatewayclasses.gateway.networking.k8s.io "*) ;;
  *) echo "Unexpected kubectl command: $*" >&2; exit 1 ;;
esac
`

const ocpCleanupCRDs = `{"items":[{"metadata":{"name":"gatewayclasses.gateway.networking.k8s.io","annotations":{"meta.helm.sh/release-namespace":"openshift-ingress"}},"spec":{"group":"gateway.networking.k8s.io"}},{"metadata":{"name":"meshes.consul.hashicorp.com","annotations":{"meta.helm.sh/release-namespace":"consul"}},"spec":{"group":"consul.hashicorp.com"}},{"metadata":{"name":"customgatewayclasses.consul.hashicorp.com","annotations":{"meta.helm.sh/release-namespace":"consul","meta.helm.sh/release-name":"test-old"}},"spec":{"group":"consul.hashicorp.com"}},{"metadata":{"name":"servicedefaults.consul.hashicorp.com","annotations":{"meta.helm.sh/release-namespace":"other-installation"}},"spec":{"group":"consul.hashicorp.com"}}]}`

const ocpCleanupHelmStub = `#!/usr/bin/env bash
set -eu
echo "helm $*" >> "$OCP_TEST_CALLS"
case "$*" in
  *"list "*)
    if [[ "$OCP_TEST_SCENARIO" == "foreign" ]]; then
      echo '[{"name":"database","chart":"postgresql-1.0.0"}]'
    else
      echo '[{"name":"test-consul","chart":"consul-2.1.0"}]'
    fi ;;
  *"uninstall "*) ;;
  *) echo "Unexpected helm command: $*" >&2; exit 1 ;;
esac
`
