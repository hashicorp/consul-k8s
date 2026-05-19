package openshift

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool) {
	newOpenshiftClusterWithHelmValues(t, cfg, secure, namespaceMirroring, nil)
}

func newOpenshiftClusterWithHelmValues(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool, extraHelmValues map[string]string) {
	// Cleanup of old consul secret
	cmd := exec.Command("kubectl", "delete", "secret", "-n", "consul", "consul-ent-license")
	_, _ = cmd.CombinedOutput()

	// Cleanup of old consul helm installtion and namespace
	cmd = exec.Command("helm", "uninstall", "consul", "--namespace", "consul", "--no-hooks")
	_, _ = cmd.CombinedOutput()

	// Bypass finalizers for the consul namespace deletion.
	ns := "consul"
	cmd = exec.Command("bash", "-c", `kubectl get ns "`+ns+`" -o json | jq 'del(.spec.finalizers)' | kubectl replace --raw "/api/v1/namespaces/`+ns+`/finalize" -f -`)
	_ = cmd.Run()

	// Cleanup of old consul namespace
	cmd = exec.Command("kubectl", "delete", "namespace", "consul")
	_, _ = cmd.CombinedOutput()

	// Ensure resources from prior runs are not stuck in Terminating.
	helpers.ForceCleanupTerminatingResources(t, []helpers.ResourceToCleanup{
		{Type: "crd", Name: "gatewayclassconfigs.consul.hashicorp.com"},
	}, 180*time.Second)

	var output []byte
	var err error

	// Add the hashicorp helm repo
	cmd = exec.Command("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com")
	output, err = cmd.CombinedOutput()
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

	// Create CRD explicitly and let Helm treat it as externally managed to avoid
	// CRD delete/recreate races across tests.
	cmd = exec.Command("kubectl", "apply", "-f", "../../../control-plane/config/crd/bases/consul.hashicorp.com_gatewayclassconfigs.yaml")
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to apply GatewayClassConfig CRD: %s", string(output))
	cmd = exec.Command("kubectl", "wait", "--for=condition=Established", "--timeout=120s", "crd/gatewayclassconfigs.consul.hashicorp.com")
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed waiting for GatewayClassConfig CRD to be established: %s", string(output))
	cmd = exec.Command("kubectl", "label", "crd", "gatewayclassconfigs.consul.hashicorp.com", "app.kubernetes.io/managed-by=Helm", "--overwrite")
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to label GatewayClassConfig CRD for Helm ownership: %s", string(output))
	cmd = exec.Command("kubectl", "annotate", "crd", "gatewayclassconfigs.consul.hashicorp.com", "meta.helm.sh/release-name=consul", "meta.helm.sh/release-namespace=consul", "--overwrite")
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to annotate GatewayClassConfig CRD for Helm ownership: %s", string(output))
	// Normalize ownership metadata on existing Consul CRDs. Prior local runs may
	// leave these tied to a different Helm release/namespace and block install.
	cmd = exec.Command("bash", "-c", `for crd in $(kubectl get crd -o name | grep '\.consul\.hashicorp\.com$'); do kubectl label "$crd" app.kubernetes.io/managed-by=Helm --overwrite >/dev/null 2>&1 || true; kubectl annotate "$crd" meta.helm.sh/release-name=consul meta.helm.sh/release-namespace=consul --overwrite >/dev/null 2>&1 || true; done`)
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to normalize Consul CRD ownership metadata: %s", string(output))

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
		"--set", "connectInject.apiGateway.manageExternalCRDs=false",
	}

	if len(extraHelmValues) > 0 {
		keys := make([]string, 0, len(extraHelmValues))
		for key := range extraHelmValues {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			helmArgs = append(helmArgs, "--set", key+"="+extraHelmValues[key])
		}
	}

	cmd = exec.Command("helm", helmArgs...)

	output, err = cmd.CombinedOutput()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("helm", "uninstall", "consul", "--namespace", "consul", "--no-hooks")
		output, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(string(output), "release: not found") {
			require.NoErrorf(t, err, "failed to uninstall consul: %s", string(output))
		}
	})

	require.NoErrorf(t, err, "failed to install consul: %s", string(output))
}
