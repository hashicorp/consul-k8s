package openshift

import (
	"os/exec"
	"sort"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool) {
	newOpenshiftClusterWithHelmValues(t, cfg, secure, namespaceMirroring, nil)
}

// newOpenshiftClusterWithHelmValues installs Consul on OpenShift like newOpenshiftCluster,
// additionally applying extraHelmValues (e.g. to enable the consul.hashicorp.com API Gateway).
func newOpenshiftClusterWithHelmValues(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool, extraHelmValues map[string]string) {
	// Cleanup of old consul secret
	cmd := exec.Command("kubectl", "delete", "secret", "-n", "consul", "consul-ent-license")
	_, _ = cmd.CombinedOutput()

	// Cleanup of old consul helm installtion and namespace
	cmd = exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
	_, _ = cmd.CombinedOutput()

	// Bypass finalizers for the consul namespace deletion.
	ns := "consul"
	cmd = exec.Command("bash", "-c", `kubectl get ns "`+ns+`" -o json | jq 'del(.spec.finalizers)' | kubectl replace --raw "/api/v1/namespaces/`+ns+`/finalize" -f -`)
	_ = cmd.Run()

	// Cleanup of old consul namespace
	cmd = exec.Command("kubectl", "delete", "namespace", "consul")
	_, _ = cmd.CombinedOutput()

	waitForTerminatingConsulCRDs(t)

	// Add the hashicorp helm repo
	cmd = exec.Command("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com")
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to add hashicorp helm repo: %s", string(output))

	// FUTURE for some reason NewHelmCluster creates a consul server pod that runs as root which
	//   isn't allowed in OpenShift. In order to test OpenShift properly, we have to call helm and k8s
	//   directly to bypass. Ideally we would just fix the framework that is running the pod as root.
	cmd = exec.Command("kubectl", "create", "namespace", "consul")
	output, err = cmd.CombinedOutput()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd = exec.Command("kubectl", "delete", "namespace", "consul")
		output, err = cmd.CombinedOutput()
		assert.NoErrorf(t, err, "failed to delete namespace: %s", string(output))
	})

	require.NoErrorf(t, err, "failed to add hashicorp helm repo: %s", string(output))

	cmd = exec.Command("kubectl", "create", "secret", "generic",
		"consul-ent-license",
		"--namespace", "consul",
		`--from-literal=key=`+cfg.EnterpriseLicense)
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to add consul enterprise license: %s", string(output))

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd = exec.Command("kubectl", "delete", "secret", "consul-ent-license", "--namespace", "consul")
		output, err = cmd.CombinedOutput()
		assert.NoErrorf(t, err, "failed to delete secret: %s", string(output))
	})

	cmd = exec.Command("helm", openshiftHelmArgs(cfg, secure, namespaceMirroring, withOpenshiftVersionDefaults(t, extraHelmValues))...)

	output, err = cmd.CombinedOutput()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "failed to uninstall consul: %s", string(output))
	})

	require.NoErrorf(t, err, "failed to install consul: %s", string(output))
}

// upgradeOpenshiftCluster runs `helm upgrade` against the release created by
// newOpenshiftClusterWithHelmValues. The same base values are used so that only
// extraHelmValues differ between the install and the upgrade.
func upgradeOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool, extraHelmValues map[string]string) {
	t.Helper()

	args := append(openshiftHelmArgs(cfg, secure, namespaceMirroring, withOpenshiftVersionDefaults(t, extraHelmValues)), "--wait", "--timeout", "15m")
	output, err := exec.Command("helm", args...).CombinedOutput()
	require.NoErrorf(t, err, "failed to upgrade consul: %s", string(output))
}

// waitForTerminatingConsulCRDs waits until consul.hashicorp.com CRDs left behind by a
// previous test's uninstall have finished deleting. A CRD whose objects still carry a
// finalizer (for example a CustomGatewayClass) stays in Terminating after the
// controller that would clear the finalizer is uninstalled. If the next install's
// Helm release adopts such a CRD it is deleted underneath Helm, which then fails
// with "resource not ready ... NotFound".
//
// Only CRDs that are already being deleted are touched: the finalizers of their
// leftover objects are cleared so the deletion can complete. Nothing is deleted
// that was not already being deleted, and nothing outside consul.hashicorp.com.
func waitForTerminatingConsulCRDs(t *testing.T) {
	t.Helper()
	script := `set -u
for crd in $(kubectl get crd -o jsonpath='{range .items[?(@.metadata.deletionTimestamp)]}{.metadata.name}{"\n"}{end}' | grep '\.consul\.hashicorp\.com$'); do
  echo "waiting for terminating CRD $crd"
  kubectl get "$crd" -A -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{"\n"}{end}' 2>/dev/null |
  while read -r ns name; do
    [ -z "$name" ] && { name=$ns; ns=""; }
    kubectl patch "$crd" "$name" ${ns:+-n "$ns"} --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
  done
  kubectl wait --for=delete "crd/$crd" --timeout=180s
done`
	output, err := exec.Command("bash", "-c", script).CombinedOutput()
	require.NoErrorf(t, err, "consul.hashicorp.com CRDs from a previous run did not finish deleting: %s", string(output))
}

// withOpenshiftVersionDefaults returns extraHelmValues plus the values required by the
// cluster's OpenShift version. OpenShift 4.19+ ships and owns the gateway.networking.k8s.io
// CRDs, so the chart must be told not to render them (global.openshift.isOcpGreaterthan4_18),
// otherwise Helm refuses to install because it cannot adopt the platform-owned CRDs.
// A value set explicitly by the test always wins.
func withOpenshiftVersionDefaults(t *testing.T, extraHelmValues map[string]string) map[string]string {
	t.Helper()
	values := make(map[string]string, len(extraHelmValues)+1)
	for k, v := range extraHelmValues {
		values[k] = v
	}
	if _, ok := values["global.openshift.isOcpGreaterthan4_18"]; !ok && openshiftVersionAtLeast(t, 4, 19) {
		values["global.openshift.isOcpGreaterthan4_18"] = "true"
	}
	return values
}

func openshiftHelmArgs(cfg *config.TestConfig, secure, namespaceMirroring bool, extraHelmValues map[string]string) []string {
	chartPath := "../../../charts/consul"
	args := []string{"upgrade", "--install", "consul", chartPath,
		"--namespace", "consul",
		"--set", "global.name=consul",
		"--set", "connectInject.enabled=true",
		"--set", "connectInject.transparentProxy.defaultEnabled=false",
		"--set", "connectInject.apiGateway.managedGatewayClass.mapPrivilegedContainerPorts=8000",
		"--set", "global.acls.manageSystemACLs=" + strconv.FormatBool(secure),
		"--set", "global.tls.enabled=" + strconv.FormatBool(secure),
		"--set", "global.tls.enableAutoEncrypt=" + strconv.FormatBool(secure),
		"--set", "global.enableConsulNamespaces=" + strconv.FormatBool(namespaceMirroring),
		"--set", "global.consulNamespaces.mirroringK8S=" + strconv.FormatBool(namespaceMirroring),
		"--set", "global.openshift.enabled=true",
		"--set", "global.image=" + cfg.ConsulImage,
		"--set", "global.imageK8S=" + cfg.ConsulK8SImage,
		"--set", "global.imageConsulDataplane=" + cfg.ConsulDataplaneImage,
		"--set", "global.enterpriseLicense.secretName=consul-ent-license",
		"--set", "global.enterpriseLicense.secretKey=key",
		"--set", "connectInject.apiGateway.manageExternalCRDs=true",
	}

	// Sort for deterministic ordering; later --set flags override earlier ones.
	keys := make([]string, 0, len(extraHelmValues))
	for key := range extraHelmValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--set", key+"="+extraHelmValues[key])
	}
	return args
}
