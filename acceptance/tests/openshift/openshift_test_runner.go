package openshift

import (
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func interruptProcess(t *testing.T) {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Logf("Failed to find process: %v", err)
		return
	}
	err = p.Signal(syscall.SIGINT)
	if err != nil {
		t.Logf("Failed to send interrupt signal: %v", err)
	}
}
func removeNamespaceFinalizer(t *testing.T, namespace string) {
	cmd := exec.Command("kubectl", "patch", "namespace", namespace,
		"--type=json", "-p", `[{"op": "remove", "path": "/spec/finalizers"}]`)

	if output, err := cmd.CombinedOutput(); err != nil {
		t.Logf("Error removing namespace finalizer: %v\nOutput: %s\n", err, output)
	}
	verifyNamespaceDeletion(t, namespace)
}

func verifyNamespaceDeletion(t *testing.T, namespace string) {
	checkCmd := exec.Command("kubectl", "get", "ns", namespace)
	if checkErr := checkCmd.Run(); checkErr != nil {
		t.Logf("Namespace %s deleted successfully\n", namespace)
		return
	}

	t.Logf("Namespace still exists. Additional cleanup might be required")
	interruptProcess(t)
}

func gatewayControllerDiagnostic(t *testing.T, namespace string) {
	// Add diagnostic logging for pods for controller identification
	lsNamespaceCmd := exec.Command("oc", "get", "namespaces", "-o", "wide")
	lsNamespaceOutput, _ := lsNamespaceCmd.CombinedOutput()
	t.Logf("Namespaces in cluster:\n%s", string(lsNamespaceOutput))

	podInfoCmd := exec.Command("kubectl", "get", "pods", "-n", namespace)
	podInfoOutput, _ := podInfoCmd.CombinedOutput()
	t.Logf("Pod status in namespace %s before cleanup:\n%s", namespace, string(podInfoOutput))

	podInfoCmd = exec.Command("kubectl", "get", "pods", "-n", "kube-system")
	podInfoOutput, _ = podInfoCmd.CombinedOutput()
	t.Logf("Pod status in namespace %s before cleanup:\n%s", "kube-system", string(podInfoOutput))
}

func checkAndDeleteNamespace(t *testing.T) {
	// There are some commands in this function
	//which are not being replaced with this variables and need manual replacement of namespace
	namespace := "consul"
	// Check if namespace exists and its status
	nsCheckCmd := exec.Command("kubectl", "get", "namespace", namespace, "-o", "json")
	nsOutput, _ := nsCheckCmd.CombinedOutput()
	t.Logf("Consul namespace status before cleanup:\n%s", string(nsOutput))

	// Add diagnostic logging before attempting cleanup
	logCmd := exec.Command("kubectl", "get", "all", "-n", namespace)
	logOutput, _ := logCmd.CombinedOutput()
	t.Logf("Resources in consul namespace before cleanup:\n%s", string(logOutput))

	// find gateway controller information and logs
	gatewayControllerDiagnostic(t, namespace)
	// Force cleanup of any stuck resources in the namespace (if it still exists)
	t.Log("Checking for any stuck resources...")
	forceCleanupCmd := exec.Command("bash", "-c", `
		# Try to find finalizers on the namespace
		FINALIZERS=$(kubectl get namespace consul -o json 2>/dev/null | jq '.spec.finalizers' 2>/dev/null)
		if [ ! -z "$FINALIZERS" ] && [ "$FINALIZERS" != "null" ]; then
			echo "Found finalizers on namespace consul"
			echo $FINALIZERS
			# Remove finalizers from namespace to force deletion
			# kubectl get namespace consul -o json | jq '.spec.finalizers = []' | kubectl replace --raw "/api/v1/namespaces/consul/finalize" -f -
		fi
		if kubectl get namespace consul > /dev/null 2>&1; then
			# Check for gateway resources
			GATEWAYS=$(kubectl get gateways.gateway.networking.k8s.io -n consul -o json 2>/dev/null || echo "")
			echo $GATEWAYS
		fi
	`)
	forceOutput, _ := forceCleanupCmd.CombinedOutput()
	t.Logf("Force cleanup result:\n%s", string(forceOutput))

	// Get remaining Gateways
	getCmd := exec.Command("kubectl", "get", "gateways.gateway.networking.k8s.io", "-n", namespace,
		"-o=jsonpath={.items[*].metadata.name}")
	output, err := getCmd.CombinedOutput()
	t.Logf("Gateway resource check result:\n%s", string(output))

	if err != nil {
		t.Logf("Error getting gateways: %v\n", err)
		return
	}
	cleanedOutput := strings.TrimSpace(string(output))
	if cleanedOutput == "" {
		t.Logf("No gateways found, removing namespace finalizer")
		removeNamespaceFinalizer(t, namespace)
		return
	}
	if len(cleanedOutput) > 0 {
		// Remove finalizers from each gateway
		patchCmd := exec.Command("kubectl", "patch", "gateways.gateway.networking.k8s.io", string(output), "-n", namespace,
			"--type=json", "-p", `[{"op": "remove", "path": "/metadata/finalizers"}]`)
		patchOutput, patchErr := patchCmd.CombinedOutput()
		if patchErr != nil {
			t.Logf("Error patching gateway: %v\nOutput: %s\n", patchErr, patchOutput)
			return
		}
		t.Logf("Finalizers removed successfully")
		removeNamespaceFinalizer(t, namespace)
	}
	t.Log("Attempting to delete consul namespace if it exists...")
	cleanupCmd := exec.Command("kubectl", "delete", "namespace", "consul", "--ignore-not-found=true")
	cleanupOutput, cleanupErr := cleanupCmd.CombinedOutput()
	// We don't check error here since it's just precautionary cleanup
	t.Logf("Namespace deletion attempt result: %v\nOutput: %s", cleanupErr, string(cleanupOutput))

	// Wait for namespace to be fully deleted before proceeding
	t.Log("Waiting for consul namespace to be fully deleted...")
	waitCmd := exec.Command("kubectl", "wait", "--for=delete", "namespace/consul", "--timeout=30s")
	waitOutput, waitErr := waitCmd.CombinedOutput() // Ignore errors, as this will error if the namespace doesn't exist at all
	t.Logf("Wait result: %v\nOutput: %s", waitErr, string(waitOutput))

	// Verify namespace deletion
	verifyNamespaceDeletion(t, namespace)
}

func newOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool) {
	cmd := exec.Command("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com")
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to add hashicorp helm repo : %s", string(output))

	// Check for any stuck resources in the namespace and force cleanup if necessary
	checkAndDeleteNamespace(t)

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

	require.NoErrorf(t, err, "failed to create namespace: %s", string(output))

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
	)

	output, err = cmd.CombinedOutput()
	t.Logf("Output of the helm install command: %s", string(output))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "failed to uninstall consul: %s", string(output))
	})

	require.NoErrorf(t, err, "failed to install consul: %s", string(output))
}
