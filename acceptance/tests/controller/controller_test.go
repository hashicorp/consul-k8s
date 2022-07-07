package controller

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-uuid"
	"github.com/stretchr/testify/require"
)

const (
	KubernetesAuthMethodPath = "kubernetes"
	ManageSystemACLsRole     = "server-acl-init"
	ClientRole               = "client"
	ServerRole               = "server"
)

func TestController(t *testing.T) {
	cfg := suite.Config()

	cases := []struct {
		secure   bool
		useVault bool
	}{
		{false, false},
		{true, false},
		{false, true},
		{true, true},
	}

	// The name of a service intention in consul is
	// the name of the destination service and is not
	// the same as the kube name of the resource.
	const IntentionName = "svc1"

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t, vault: %t", c.secure, c.useVault)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"controller.enabled":           "true",
				"connectInject.enabled":        "true",
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()

			var bootstrapToken string
			var helmConsulValues map[string]string
			if c.useVault {
				helmConsulValues, bootstrapToken = configureAndGetVaultHelmValues(t, ctx, cfg, releaseName, c.secure)
				helpers.MergeMaps(helmConsulValues, helmValues)
			}
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			if c.useVault {
				consulCluster.ACLToken = bootstrapToken
			}

			// Test creation.
			{
				logger.Log(t, "creating custom resources")
				retry.Run(t, func(r *retry.R) {
					// Retry the kubectl apply because we've seen sporadic
					// "connection refused" errors where the mutating webhook
					// endpoint fails initially.
					out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/bases/crds-oss")
					require.NoError(r, err, out)
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						// Ignore errors here because if the test ran as expected
						// the custom resources will have been deleted.
						k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/bases/crds-oss")
					})
				})

				// On startup, the controller can take upwards of 1m to perform
				// leader election so we may need to wait a long time for
				// the reconcile loop to run (hence the 1m timeout here).
				counter := &retry.Counter{Count: 60, Wait: 1 * time.Second}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", nil)
					require.NoError(r, err)
					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, "http", svcDefaultEntry.Protocol)

					// service-resolver
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", nil)
					require.NoError(r, err)
					svcResolverEntry, ok := entry.(*api.ServiceResolverConfigEntry)
					require.True(r, ok, "could not cast to ServiceResolverConfigEntry")
					require.Equal(r, "bar", svcResolverEntry.Redirect.Service)

					// proxy-defaults
					entry, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", nil)
					require.NoError(r, err)
					proxyDefaultEntry, ok := entry.(*api.ProxyConfigEntry)
					require.True(r, ok, "could not cast to ProxyConfigEntry")
					require.Equal(r, api.MeshGatewayModeLocal, proxyDefaultEntry.MeshGateway.Mode)
					require.Equal(r, "tcp", proxyDefaultEntry.Config["protocol"])
					require.Equal(r, float64(3), proxyDefaultEntry.Config["number"])
					require.Equal(r, true, proxyDefaultEntry.Config["bool"])
					require.Equal(r, []interface{}{"item1", "item2"}, proxyDefaultEntry.Config["array"])
					require.Equal(r, map[string]interface{}{"key": "value"}, proxyDefaultEntry.Config["map"])
					require.Equal(r, "/health", proxyDefaultEntry.Expose.Paths[0].Path)
					require.Equal(r, 22000, proxyDefaultEntry.Expose.Paths[0].ListenerPort)
					require.Equal(r, 8080, proxyDefaultEntry.Expose.Paths[0].LocalPathPort)

					// mesh
					entry, _, err = consulClient.ConfigEntries().Get(api.MeshConfig, "mesh", nil)
					require.NoError(r, err)
					meshConfigEntry, ok := entry.(*api.MeshConfigEntry)
					require.True(r, ok, "could not cast to MeshConfigEntry")
					require.True(r, meshConfigEntry.TransparentProxy.MeshDestinationsOnly)

					// service-router
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", nil)
					require.NoError(r, err)
					svcRouterEntry, ok := entry.(*api.ServiceRouterConfigEntry)
					require.True(r, ok, "could not cast to ServiceRouterConfigEntry")
					require.Equal(r, "/foo", svcRouterEntry.Routes[0].Match.HTTP.PathPrefix)

					// service-splitter
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", nil)
					require.NoError(r, err)
					svcSplitterEntry, ok := entry.(*api.ServiceSplitterConfigEntry)
					require.True(r, ok, "could not cast to ServiceSplitterConfigEntry")
					require.Equal(r, float32(100), svcSplitterEntry.Splits[0].Weight)

					// service-intentions
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, nil)
					require.NoError(r, err)
					svcIntentionsEntry, ok := entry.(*api.ServiceIntentionsConfigEntry)
					require.True(r, ok, "could not cast to ServiceIntentionsConfigEntry")
					require.Equal(r, api.IntentionActionAllow, svcIntentionsEntry.Sources[0].Action)
					require.Equal(r, api.IntentionActionAllow, svcIntentionsEntry.Sources[1].Permissions[0].Action)

					// ingress-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "ingress-gateway", nil)
					require.NoError(r, err)
					ingressGatewayEntry, ok := entry.(*api.IngressGatewayConfigEntry)
					require.True(r, ok, "could not cast to IngressGatewayConfigEntry")
					require.Len(r, ingressGatewayEntry.Listeners, 1)
					require.Equal(r, "tcp", ingressGatewayEntry.Listeners[0].Protocol)
					require.Equal(r, 8080, ingressGatewayEntry.Listeners[0].Port)
					require.Len(r, ingressGatewayEntry.Listeners[0].Services, 1)
					require.Equal(r, "foo", ingressGatewayEntry.Listeners[0].Services[0].Name)

					// terminating-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.TerminatingGateway, "terminating-gateway", nil)
					require.NoError(r, err)
					terminatingGatewayEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
					require.True(r, ok, "could not cast to TerminatingGatewayConfigEntry")
					require.Len(r, terminatingGatewayEntry.Services, 1)
					require.Equal(r, "name", terminatingGatewayEntry.Services[0].Name)
					require.Equal(r, "caFile", terminatingGatewayEntry.Services[0].CAFile)
					require.Equal(r, "certFile", terminatingGatewayEntry.Services[0].CertFile)
					require.Equal(r, "keyFile", terminatingGatewayEntry.Services[0].KeyFile)
					require.Equal(r, "sni", terminatingGatewayEntry.Services[0].SNI)
				})
			}

			// Test updates.
			{
				logger.Log(t, "patching service-defaults custom resource")
				patchProtocol := "tcp"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicedefaults", "defaults", "-p", fmt.Sprintf(`{"spec":{"protocol":"%s"}}`, patchProtocol), "--type=merge")

				logger.Log(t, "patching service-resolver custom resource")
				patchRedirectSvc := "baz"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "serviceresolver", "resolver", "-p", fmt.Sprintf(`{"spec":{"redirect":{"service": "%s"}}}`, patchRedirectSvc), "--type=merge")

				logger.Log(t, "patching proxy-defaults custom resource")
				patchMeshGatewayMode := "remote"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "proxydefaults", "global", "-p", fmt.Sprintf(`{"spec":{"meshGateway":{"mode": "%s"}}}`, patchMeshGatewayMode), "--type=merge")

				logger.Log(t, "patching mesh custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "mesh", "mesh", "-p", fmt.Sprintf(`{"spec":{"transparentProxy":{"meshDestinationsOnly": %t}}}`, false), "--type=merge")

				logger.Log(t, "patching service-router custom resource")
				patchPathPrefix := "/baz"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicerouter", "router", "-p", fmt.Sprintf(`{"spec":{"routes":[{"match":{"http":{"pathPrefix":"%s"}}}]}}`, patchPathPrefix), "--type=merge")

				logger.Log(t, "patching service-splitter custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicesplitter", "splitter", "-p", `{"spec": {"splits": [{"weight": 50}, {"weight": 50, "service": "other-splitter"}]}}`, "--type=merge")

				logger.Log(t, "patching service-intentions custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "serviceintentions", "intentions", "-p", `{"spec": {"sources": [{"name": "svc2", "action": "deny"}, {"name": "svc3", "permissions": [{"action": "deny", "http": {"pathExact": "/foo", "methods": ["GET", "PUT"]}}]}]}}`, "--type=merge")

				logger.Log(t, "patching ingress-gateway custom resource")
				patchPort := 9090
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "ingressgateway", "ingress-gateway", "-p", fmt.Sprintf(`{"spec": {"listeners": [{"port": %d, "protocol": "tcp", "services": [{"name": "foo"}]}]}}`, patchPort), "--type=merge")

				logger.Log(t, "patching terminating-gateway custom resource")
				patchSNI := "patch-sni"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "terminatinggateway", "terminating-gateway", "-p", fmt.Sprintf(`{"spec": {"services": [{"name":"name","caFile":"caFile","certFile":"certFile","keyFile":"keyFile","sni":"%s"}]}}`, patchSNI), "--type=merge")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", nil)
					require.NoError(r, err)
					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, patchProtocol, svcDefaultEntry.Protocol)

					// service-resolver
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", nil)
					require.NoError(r, err)
					svcResolverEntry, ok := entry.(*api.ServiceResolverConfigEntry)
					require.True(r, ok, "could not cast to ServiceResolverConfigEntry")
					require.Equal(r, patchRedirectSvc, svcResolverEntry.Redirect.Service)

					// proxy-defaults
					entry, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", nil)
					require.NoError(r, err)
					proxyDefaultsEntry, ok := entry.(*api.ProxyConfigEntry)
					require.True(r, ok, "could not cast to ProxyConfigEntry")
					require.Equal(r, api.MeshGatewayModeRemote, proxyDefaultsEntry.MeshGateway.Mode)

					// mesh
					entry, _, err = consulClient.ConfigEntries().Get(api.MeshConfig, "mesh", nil)
					require.NoError(r, err)
					meshEntry, ok := entry.(*api.MeshConfigEntry)
					require.True(r, ok, "could not cast to MeshConfigEntry")
					require.False(r, meshEntry.TransparentProxy.MeshDestinationsOnly)

					// service-router
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", nil)
					require.NoError(r, err)
					svcRouterEntry, ok := entry.(*api.ServiceRouterConfigEntry)
					require.True(r, ok, "could not cast to ServiceRouterConfigEntry")
					require.Equal(r, patchPathPrefix, svcRouterEntry.Routes[0].Match.HTTP.PathPrefix)

					// service-splitter
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", nil)
					require.NoError(r, err)
					svcSplitter, ok := entry.(*api.ServiceSplitterConfigEntry)
					require.True(r, ok, "could not cast to ServiceSplitterConfigEntry")
					require.Equal(r, float32(50), svcSplitter.Splits[0].Weight)
					require.Equal(r, float32(50), svcSplitter.Splits[1].Weight)
					require.Equal(r, "other-splitter", svcSplitter.Splits[1].Service)

					// service-intentions
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, nil)
					require.NoError(r, err)
					svcIntentions, ok := entry.(*api.ServiceIntentionsConfigEntry)
					require.True(r, ok, "could not cast to ServiceIntentionsConfigEntry")
					require.Equal(r, api.IntentionActionDeny, svcIntentions.Sources[0].Action)
					require.Equal(r, api.IntentionActionDeny, svcIntentions.Sources[1].Permissions[0].Action)

					// ingress-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "ingress-gateway", nil)
					require.NoError(r, err)
					ingressGatewayEntry, ok := entry.(*api.IngressGatewayConfigEntry)
					require.True(r, ok, "could not cast to IngressGatewayConfigEntry")
					require.Equal(r, patchPort, ingressGatewayEntry.Listeners[0].Port)

					// terminating-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.TerminatingGateway, "terminating-gateway", nil)
					require.NoError(r, err)
					terminatingGatewayEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
					require.True(r, ok, "could not cast to TerminatingGatewayConfigEntry")
					require.Equal(r, patchSNI, terminatingGatewayEntry.Services[0].SNI)
				})
			}

			// Test a delete.
			{
				logger.Log(t, "deleting service-defaults custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicedefaults", "defaults")

				logger.Log(t, "deleting service-resolver custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "serviceresolver", "resolver")

				logger.Log(t, "deleting proxy-defaults custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "proxydefaults", "global")

				logger.Log(t, "deleting mesh custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "mesh", "mesh")

				logger.Log(t, "deleting service-router custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicerouter", "router")

				logger.Log(t, "deleting service-splitter custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicesplitter", "splitter")

				logger.Log(t, "deleting service-intentions custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "serviceintentions", "intentions")

				logger.Log(t, "deleting ingress-gateway custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ingressgateway", "ingress-gateway")

				logger.Log(t, "deleting terminating-gateway custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "terminatinggateway", "terminating-gateway")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					_, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-resolver
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// proxy-defaults
					_, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// mesh
					_, _, err = consulClient.ConfigEntries().Get(api.MeshConfig, "mesh", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-router
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-splitter
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-intentions
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// ingress-gateway
					_, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "ingress-gateway", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// terminating-gateway
					_, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "terminating-gateway", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")
				})
			}
		})
	}
}

func configureAndGetVaultHelmValues(t *testing.T, ctx environment.TestContext,
	cfg *config.TestConfig, consulReleaseName string, secure bool) (map[string]string, string) {
	vaultReleaseName := helpers.RandomName()
	ns := ctx.KubectlOptions(t).Namespace

	vaultCluster := vault.NewVaultCluster(t, ctx, cfg, vaultReleaseName, nil)
	vaultCluster.Create(t, ctx, "")
	// Vault is now installed in the cluster.

	// Now fetch the Vault client so we can create the policies and secrets.
	vaultClient := vaultCluster.VaultClient(t)

	// -------------------------
	// PKI
	// -------------------------
	// Configure Service Mesh CA
	connectCAPolicy := "connect-ca-dc1"
	connectCARootPath := "connect_root"
	connectCAIntermediatePath := "dc1/connect_inter"
	// Configure Policy for Connect CA
	vault.CreateConnectCARootAndIntermediatePKIPolicy(t, vaultClient, connectCAPolicy, connectCARootPath, connectCAIntermediatePath)

	// Configure Server PKI
	serverPKIConfig := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "pki",
		PolicyName:          "consul-ca-policy",
		RoleName:            "consul-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		MaxTTL:              "1h",
		AuthMethodPath:      "kubernetes",
		CommonName:          "Consul CA",
	}
	serverPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	webhookCertTtl := 25 * time.Second
	// Configure controller webhook PKI
	controllerWebhookPKIConfig := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "controller",
		PolicyName:          "controller-ca-policy",
		RoleName:            "controller-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "controller"),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "controller-webhook"),
		MaxTTL:              webhookCertTtl.String(),
		AuthMethodPath:      "kubernetes",
	}
	controllerWebhookPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	// Configure controller webhook PKI
	connectInjectorWebhookPKIConfig := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "connect",
		PolicyName:          "connect-ca-policy",
		RoleName:            "connect-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "connect-injector"),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "connect-injector"),
		MaxTTL:              webhookCertTtl.String(),
		AuthMethodPath:      "kubernetes",
	}
	connectInjectorWebhookPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	// -------------------------
	// KV2 secrets
	// -------------------------
	// Gossip key
	gossipKey, err := vault.GenerateGossipSecret()
	require.NoError(t, err)
	gossipSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/gossip",
		Key:        "gossip",
		Value:      gossipKey,
		PolicyName: "gossip",
	}
	gossipSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// License
	licenseSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/license",
		Key:        "license",
		Value:      cfg.EnterpriseLicense,
		PolicyName: "license",
	}
	if cfg.EnableEnterprise {
		licenseSecret.SaveSecretAndAddReadPolicy(t, vaultClient)
	}

	// Bootstrap Token
	bootstrapToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	bootstrapTokenSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/bootstrap",
		Key:        "token",
		Value:      bootstrapToken,
		PolicyName: "bootstrap",
	}
	bootstrapTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// -------------------------
	// Additional Auth Roles
	// -------------------------
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", gossipSecret.PolicyName, connectCAPolicy, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName)
	if cfg.EnableEnterprise {
		serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
	}

	// server
	consulServerRole := ServerRole
	srvAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            consulServerRole,
		PolicyNames:         serverPolicies,
	}
	srvAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// client
	consulClientRole := ClientRole
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, ClientRole)
	clientAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            consulClientRole,
		PolicyNames:         gossipSecret.PolicyName,
	}
	clientAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// manageSystemACLs
	manageSystemACLsRole := ManageSystemACLsRole
	manageSystemACLsServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, ManageSystemACLsRole)
	aclAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            manageSystemACLsRole,
		PolicyNames:         bootstrapTokenSecret.PolicyName,
	}
	aclAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// allow all components to access server ca
	srvCAAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	}
	srvCAAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	consulHelmValues := map[string]string{
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecret,
		"server.extraVolumes[0].load": "false",

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulServerRole":     consulServerRole,
		"global.secretsBackend.vault.consulClientRole":     consulClientRole,
		"global.secretsBackend.vault.consulCARole":         serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole": manageSystemACLsRole,

		"global.secretsBackend.vault.ca.secretName": vaultCASecret,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",
	}

	if cfg.EnableEnterprise {
		consulHelmValues["global.enterpriseLicense.secretName"] = licenseSecret.Path
		consulHelmValues["global.enterpriseLicense.secretKey"] = licenseSecret.Key
	}

	if secure {
		consulHelmValues["server.serverCert.secretName"] = serverPKIConfig.CertPath
		consulHelmValues["global.tls.caCert.secretName"] = serverPKIConfig.CAPath
		consulHelmValues["global.secretsBackend.vault.connectInject.tlsCert.secretName"] = connectInjectorWebhookPKIConfig.CertPath
		consulHelmValues["global.secretsBackend.vault.connectInject.caCert.secretName"] = connectInjectorWebhookPKIConfig.CAPath
		consulHelmValues["global.secretsBackend.vault.controller.tlsCert.secretName"] = controllerWebhookPKIConfig.CertPath
		consulHelmValues["global.secretsBackend.vault.controller.caCert.secretName"] = controllerWebhookPKIConfig.CAPath
		consulHelmValues["global.secretsBackend.vault.connectInjectRole"] = connectInjectorWebhookPKIConfig.RoleName
		consulHelmValues["global.secretsBackend.vault.controllerRole"] = controllerWebhookPKIConfig.RoleName
		consulHelmValues["global.acls.bootstrapToken.secretName"] = bootstrapTokenSecret.Path
		consulHelmValues["global.acls.bootstrapToken.secretKey"] = bootstrapTokenSecret.Key
		consulHelmValues["global.gossipEncryption.secretName"] = gossipSecret.Path
		consulHelmValues["global.gossipEncryption.secretKey"] = gossipSecret.Key
	}

	return consulHelmValues, bootstrapToken
}
