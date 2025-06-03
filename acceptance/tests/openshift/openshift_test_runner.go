package openshift

import (
	"os/exec"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool) {
	// Cleanup of old consul secret
	cmd := exec.Command("kubectl", "delete", "secret", "-n", "consul", "consul-ent-license")
	cmd.CombinedOutput()

	// Cleanup of old consul helm installtion and namespace
	cmd = exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
	cmd.CombinedOutput()

	// Bypass finalizers for the consul namespace deletion.
	ns := "consul"
	cmd = exec.Command("bash", "-c", `kubectl get ns "`+ns+`" -o json | jq 'del(.spec.finalizers)' | kubectl replace --raw "/api/v1/namespaces/`+ns+`/finalize" -f -`)
	cmd.Run()

	// Cleanup of old consul namespace
	cmd = exec.Command("kubectl", "delete", "namespace", "consul")
	cmd.CombinedOutput()

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
	cmd = exec.Command("helm", "upgrade", "--install", "consul", chartPath,
		"--namespace", "consul",
		"--set", "global.name=consul",
		"--set", "connectInject.enabled=true",
		"--set", "connectInject.transparentProxy.defaultEnabled=false",
		"--set", "connectInject.apiGateway.managedGatewayClass.mapPrivilegedContainerPorts=8000",
		"--set", "global.acls.manageSystemACLs="+strconv.FormatBool(secure),
		"--set", "global.tls.enabled="+strconv.FormatBool(secure),
		"--set", "global.tls.enableAutoEncrypt="+strconv.FormatBool(secure),
		"--set", "global.enableConsulNamespaces="+strconv.FormatBool(namespaceMirroring),
		"--set", "global.consulNamespaces.mirroringK8S="+strconv.FormatBool(namespaceMirroring),
		"--set", "global.openshift.enabled=true",
		"--set", "global.image="+cfg.ConsulImage,
		"--set", "global.imageK8S="+cfg.ConsulK8SImage,
		"--set", "global.imageConsulDataplane="+cfg.ConsulDataplaneImage,
		"--set", "global.enterpriseLicense.secretName=consul-ent-license",
		"--set", "global.enterpriseLicense.secretKey=key",
		"--set", "connectInject.apiGateway.manageExternalCRDs=true",
	)

	output, err = cmd.CombinedOutput()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "failed to uninstall consul: %s", string(output))
	})

	require.NoErrorf(t, err, "failed to install consul: %s", string(output))
}
