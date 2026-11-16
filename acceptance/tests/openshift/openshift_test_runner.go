package openshift

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool) {
	// Cleanup of old consul secret
	cmd := exec.Command("kubectl", "delete", "secret", "-n", "consul", "consul-ent-license")
	_, _ = cmd.CombinedOutput()

	// Cleanup of old consul helm installtion and namespace
	cmd = exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
	_, _ = cmd.CombinedOutput()

	// Cleanup of old consul namespace. Only bypass finalizers if a normal delete gets stuck.
	ns := "consul"
	cmd = exec.Command("kubectl", "delete", "namespace", ns, "--ignore-not-found", "--wait=true", "--timeout=5m")
	_, _ = cmd.CombinedOutput()
	if exec.Command("kubectl", "get", "namespace", ns).Run() == nil {
		cmd = exec.Command("bash", "-c", `kubectl get ns "`+ns+`" -o json | jq 'del(.spec.finalizers)' | kubectl replace --raw "/api/v1/namespaces/`+ns+`/finalize" -f -`)
		_ = cmd.Run()
		cmd = exec.Command("kubectl", "wait", "--for=delete", "namespace/"+ns, "--timeout=2m")
		_, _ = cmd.CombinedOutput()
	}

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

	chartPath := "../../../charts/consul"
	helmArgs := []string{"upgrade", "--install", "consul", chartPath,
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
	// OpenShift 4.19+ ships and owns the gateway.networking.k8s.io CRDs, so the chart must not render them.
	if openshiftVersionAtLeast(t, 4, 19) {
		helmArgs = append(helmArgs, "--set", "global.openshift.isOcpGreaterthan4_18=true")
	}
	cmd = exec.Command("helm", helmArgs...)

	output, err = cmd.CombinedOutput()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "failed to uninstall consul: %s", string(output))
	})

	require.NoErrorf(t, err, "failed to install consul: %s", string(output))
}

// openshiftVersionAtLeast reports whether the cluster's OpenShift version is at least major.minor.
func openshiftVersionAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()
	out, err := exec.Command("kubectl", "get", "clusterversion", "version", "-o", "jsonpath={.status.desired.version}").CombinedOutput()
	require.NoErrorf(t, err, "failed to get OpenShift cluster version: %s", string(out))
	parts := strings.SplitN(strings.TrimSpace(string(out)), ".", 3)
	require.GreaterOrEqualf(t, len(parts), 2, "unexpected OpenShift version %q", string(out))
	gotMajor, err := strconv.Atoi(parts[0])
	require.NoError(t, err)
	gotMinor, err := strconv.Atoi(parts[1])
	require.NoError(t, err)
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

// waitForTerminatingConsulCRDs waits until consul.hashicorp.com CRDs left Terminating by a previous run are
// deleted, clearing the finalizers of their remaining objects so the next install can recreate them.
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
