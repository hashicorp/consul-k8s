package openshift

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	consulNamespace     = "consul"
	consulReleaseName   = "consul"
	consulLicenseSecret = "consul-ent-license"
	deleteTimeout       = "2m"
)

func newOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool) {
	// Remove anything left over from a previous run before installing.
	uninstallConsul(t)

	// Add the hashicorp helm repo
	cmd := exec.Command("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com")
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to add hashicorp helm repo: %s", string(output))

	// FUTURE for some reason NewHelmCluster creates a consul server pod that runs as root which
	//   isn't allowed in OpenShift. In order to test OpenShift properly, we have to call helm and k8s
	//   directly to bypass. Ideally we would just fix the framework that is running the pod as root.
	cmd = exec.Command("kubectl", "create", "namespace", consulNamespace)
	output, err = cmd.CombinedOutput()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		uninstallConsul(t)
		assert.Error(t, exec.Command("kubectl", "get", "namespace", consulNamespace).Run(),
			"namespace %s still exists after cleanup", consulNamespace)
	})
	require.NoErrorf(t, err, "failed to create namespace: %s", string(output))

	cmd = exec.Command("kubectl", "create", "secret", "generic",
		consulLicenseSecret,
		"--namespace", consulNamespace,
		`--from-literal=key=`+cfg.EnterpriseLicense)
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to add consul enterprise license: %s", string(output))

	chartPath := "../../../charts/consul"
	helmArgs := []string{"upgrade", "--install", consulReleaseName, chartPath,
		"--namespace", consulNamespace,
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
	require.NoErrorf(t, err, "failed to install consul: %s", string(output))
}

// uninstallConsul removes the Consul installation of this release in this order:
//  1. Consul API gateways and custom API gateways
//  2. their GatewayClasses / CustomGatewayClasses, then GatewayClassConfigs
//  3. the Helm release
//  4. the license secret
//  5. the namespace, then any cluster-scoped resources of the release
//
// Gateway resources are deleted while the controller is still running so it can remove
// their finalizers. Each delete has a timeout and falls back to removing finalizers.
// Missing resources are ignored, so it is safe to call before an install.
func uninstallConsul(t *testing.T) {
	gatewayKinds := []struct{ classes, gateways, controller string }{
		{"gatewayclasses.gateway.networking.k8s.io", "gateways.gateway.networking.k8s.io", "consul.hashicorp.com/gateway-controller"},
		{"customgatewayclasses.consul.hashicorp.com", "cgateways.consul.hashicorp.com", "consul.hashicorp.com/gateway-controller-consul"},
	}
	for _, k := range gatewayKinds {
		classes := map[string]bool{}
		for _, line := range kubectlLines(fmt.Sprintf(`{range .items[?(@.spec.controllerName=="%s")]}{.metadata.name}{"\n"}{end}`, k.controller), "get", k.classes, "-l", "release="+consulReleaseName) {
			classes[line] = true
		}
		if len(classes) == 0 {
			continue
		}
		for _, line := range kubectlLines(`{range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{" "}{.spec.gatewayClassName}{"\n"}{end}`, "get", k.gateways, "-A") {
			f := strings.Fields(line)
			if len(f) == 3 && classes[f[2]] {
				deleteWithFinalizerFallback(t, k.gateways, f[0], f[1])
			}
		}
		for class := range classes {
			deleteWithFinalizerFallback(t, k.classes, "", class)
		}
	}
	for _, name := range kubectlLines(`{range .items[*]}{.metadata.name}{"\n"}{end}`, "get", "gatewayclassconfigs.consul.hashicorp.com", "-l", "release="+consulReleaseName) {
		deleteWithFinalizerFallback(t, "gatewayclassconfigs.consul.hashicorp.com", "", name)
	}

	if out, err := exec.Command("helm", "status", consulReleaseName, "--namespace", consulNamespace).CombinedOutput(); err == nil {
		out, err = exec.Command("helm", "uninstall", consulReleaseName, "--namespace", consulNamespace, "--wait", "--timeout", "5m").CombinedOutput()
		if err != nil {
			logger.Logf(t, "helm uninstall failed, continuing with namespace cleanup: %s", string(out))
		}
	} else {
		logger.Logf(t, "no helm release %s found: %s", consulReleaseName, strings.TrimSpace(string(out)))
	}

	_, _ = exec.Command("kubectl", "delete", "secret", consulLicenseSecret, "--namespace", consulNamespace,
		"--ignore-not-found", "--timeout="+deleteTimeout).CombinedOutput()

	deleteNamespace(t, consulNamespace)

	out, err := exec.Command("kubectl", "delete",
		"clusterrole,clusterrolebinding,mutatingwebhookconfiguration,validatingwebhookconfiguration,securitycontextconstraints.security.openshift.io",
		"-l", "release="+consulReleaseName, "--ignore-not-found", "--timeout="+deleteTimeout).CombinedOutput()
	if err != nil {
		logger.Logf(t, "failed to delete cluster-scoped resources of release %s: %s", consulReleaseName, string(out))
	}
}

// deleteNamespace deletes ns with a timeout. If it is stuck terminating, it removes the
// finalizers of the resources still in it and, as a last resort, finalizes the namespace.
func deleteNamespace(t *testing.T, ns string) {
	_, _ = exec.Command("kubectl", "delete", "namespace", ns, "--ignore-not-found", "--wait=true", "--timeout=5m").CombinedOutput()
	if exec.Command("kubectl", "get", "namespace", ns).Run() != nil {
		return
	}

	logger.Logf(t, "namespace %s is stuck terminating, removing finalizers of remaining resources", ns)
	types := kubectlLines("", "api-resources", "--verbs=list", "--namespaced", "-o", "name")
	if len(types) > 0 {
		// kubectl prints the resources it can list on stdout even if some types fail.
		out, _ := exec.Command("kubectl", "get", strings.Join(types, ","), "-n", ns, "-o", "name", "--ignore-not-found").Output()
		for _, res := range strings.Fields(string(out)) {
			logger.Logf(t, "removing finalizers from %s", res)
			_, _ = exec.Command("kubectl", "patch", res, "-n", ns, "--type=merge", "-p", `{"metadata":{"finalizers":null}}`).CombinedOutput()
		}
	}
	if exec.Command("kubectl", "wait", "--for=delete", "namespace/"+ns, "--timeout="+deleteTimeout).Run() == nil {
		return
	}

	logger.Logf(t, "namespace %s is still terminating, finalizing it", ns)
	_ = exec.Command("bash", "-c", `kubectl get ns "`+ns+`" -o json | jq 'del(.spec.finalizers)' | kubectl replace --raw "/api/v1/namespaces/`+ns+`/finalize" -f -`).Run()
	_, _ = exec.Command("kubectl", "wait", "--for=delete", "namespace/"+ns, "--timeout="+deleteTimeout).CombinedOutput()
}

// deleteWithFinalizerFallback deletes a resource with a timeout and removes its
// finalizers if the delete does not finish. An empty namespace means cluster-scoped.
func deleteWithFinalizerFallback(t *testing.T, resource, namespace, name string) {
	args := []string{resource, name}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	out, err := exec.Command("kubectl", append([]string{"delete", "--ignore-not-found", "--timeout=" + deleteTimeout}, args...)...).CombinedOutput()
	if err == nil {
		return
	}
	logger.Logf(t, "delete of %s %s timed out, removing finalizers: %s", resource, name, string(out))
	_, _ = exec.Command("kubectl", append([]string{"patch", "--type=merge", "-p", `{"metadata":{"finalizers":null}}`}, args...)...).CombinedOutput()
	_, _ = exec.Command("kubectl", append([]string{"delete", "--ignore-not-found", "--timeout=" + deleteTimeout}, args...)...).CombinedOutput()
}

// kubectlLines runs kubectl with the given jsonpath (if any) and returns the non-empty
// output lines. Errors, such as a CRD that is not installed, return no lines.
func kubectlLines(jsonpath string, args ...string) []string {
	if jsonpath != "" {
		args = append(args, "-o", "jsonpath="+jsonpath)
	}
	out, err := exec.Command("kubectl", args...).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
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
