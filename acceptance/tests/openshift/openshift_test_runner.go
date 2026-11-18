package openshift

import (
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/stretchr/testify/require"
)

func newOpenshiftCluster(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool) {
	newOpenshiftClusterWithHelmValues(t, cfg, secure, namespaceMirroring, nil)
}

func newOpenshiftClusterWithHelmValues(t *testing.T, cfg *config.TestConfig, secure, namespaceMirroring bool, extraHelmValues map[string]string) {
	t.Helper()
	ctx := environment.NewKubernetesEnvironmentFromConfig(cfg).DefaultContext(t)
	require.Equal(t, "consul", ctx.KubectlOptions(t).Namespace, "OpenShift fixtures require the consul test namespace")
	values := map[string]string{
		"global.name":           "consul",
		"connectInject.enabled": "true",
		"connectInject.transparentProxy.defaultEnabled":                            "false",
		"connectInject.apiGateway.managedGatewayClass.mapPrivilegedContainerPorts": "8000",
		"global.acls.manageSystemACLs":                                             strconv.FormatBool(secure),
		"global.tls.enabled":                                                       strconv.FormatBool(secure),
		"global.tls.enableAutoEncrypt":                                             strconv.FormatBool(secure),
		"global.enableConsulNamespaces":                                            strconv.FormatBool(namespaceMirroring),
		"global.consulNamespaces.mirroringK8S":                                     strconv.FormatBool(namespaceMirroring),
	}
	helpers.MergeMaps(values, extraHelmValues)
	cluster := consul.NewHelmCluster(t, values, ctx, cfg, "consul")
	cluster.Create(t)
}
