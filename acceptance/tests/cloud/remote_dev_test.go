// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloud

import (
	"crypto/tls"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"

	hcpgnm "github.com/hashicorp/hcp-sdk-go/clients/cloud-global-network-manager-service/preview/2022-02-15/client/global_network_manager_service"
	"github.com/hashicorp/hcp-sdk-go/clients/cloud-global-network-manager-service/preview/2022-02-15/models"
	hcpcfg "github.com/hashicorp/hcp-sdk-go/config"
	"github.com/hashicorp/hcp-sdk-go/httpclient"
	"github.com/hashicorp/hcp-sdk-go/resource"
)

type DevTokenResponse struct {
	Token string `json:"token"`
}

type hcp struct {
	ResourceID   string
	ClientID     string
	ClientSecret string
	AuthURL      string
	APIHostname  string
	ScadaAddress string
}

func TestRemoteDevCloud(t *testing.T) {
	_, rIDok := os.LookupEnv("HCP_RESOURCE_ID")
	_, cIDok := os.LookupEnv("HCP_CLIENT_ID")
	_, cSECok := os.LookupEnv("HCP_CLIENT_SECRET")

	if !rIDok || !cIDok || !cSECok {
		t.Log("Must set HCP_RESOURCE_ID, HCP_CLIENT_ID and HCP_CLIENT_SECRET")
		t.FailNow()
	}

	apiHost := os.Getenv("HCP_AUTH_URL")
	if apiHost == "" {
		apiHost = "https://api.hcp.dev"
	}
	authURL := os.Getenv("HCP_API_HOST")
	if authURL == "" {
		authURL = "https://auth.idp.hcp.dev"
	}
	scadaAddr := os.Getenv("HCP_SCADA_ADDRESS")
	if scadaAddr == "" {
		scadaAddr = "scada.internal.hcp.dev:7224"
	}

	ctx := suite.Environment().DefaultContext(t)

	kubectlOptions := ctx.KubectlOptions(t)
	ns := kubectlOptions.Namespace
	k8sClient := environment.KubernetesClientFromOptions(t, kubectlOptions)

	var (
		resourceSecretName     = "resource-sec-name"
		resourceSecretKey      = "resource-sec-key"
		resourceSecretKeyValue = os.Getenv("HCP_RESOURCE_ID")

		clientIDSecretName     = "clientid-sec-name"
		clientIDSecretKey      = "clientid-sec-key"
		clientIDSecretKeyValue = os.Getenv("HCP_CLIENT_ID")

		clientSecretName     = "client-sec-name"
		clientSecretKey      = "client-sec-key"
		clientSecretKeyValue = os.Getenv("HCP_CLIENT_SECRET")

		apiHostSecretName     = "apihost-sec-name"
		apiHostSecretKey      = "apihost-sec-key"
		apiHostSecretKeyValue = apiHost

		authUrlSecretName     = "authurl-sec-name"
		authUrlSecretKey      = "authurl-sec-key"
		authUrlSecretKeyValue = authURL

		scadaAddressSecretName     = "scadaaddress-sec-name"
		scadaAddressSecretKey      = "scadaaddress-sec-key"
		scadaAddressSecretKeyValue = scadaAddr

		bootstrapTokenSecretName = "bootstrap-token"
		bootstrapTokenSecretKey  = "token"
	)

	hcpCfg := hcp{
		ResourceID:   resourceSecretKeyValue,
		ClientID:     clientIDSecretKeyValue,
		ClientSecret: clientSecretKeyValue,
		AuthURL:      authUrlSecretKeyValue,
		APIHostname:  apiHostSecretKeyValue,
		ScadaAddress: scadaAddressSecretKeyValue,
	}

	aclToken := hcpCfg.fetchAgentBootstrapConfig(t)

	cfg := suite.Config()
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, resourceSecretName, resourceSecretKey, resourceSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientIDSecretName, clientIDSecretKey, clientIDSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientSecretName, clientSecretKey, clientSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, apiHostSecretName, apiHostSecretKey, apiHostSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, authUrlSecretName, authUrlSecretKey, authUrlSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, scadaAddressSecretName, scadaAddressSecretKey, scadaAddressSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, bootstrapTokenSecretName, bootstrapTokenSecretKey, aclToken)

	releaseName := helpers.RandomName()

	helmValues := map[string]string{
		"global.imagePullPolicy":             "IfNotPresent",
		"global.cloud.enabled":               "true",
		"global.cloud.resourceId.secretName": resourceSecretName,
		"global.cloud.resourceId.secretKey":  resourceSecretKey,

		"global.cloud.clientId.secretName": clientIDSecretName,
		"global.cloud.clientId.secretKey":  clientIDSecretKey,

		"global.cloud.clientSecret.secretName": clientSecretName,
		"global.cloud.clientSecret.secretKey":  clientSecretKey,

		"global.cloud.apiHost.secretName": apiHostSecretName,
		"global.cloud.apiHost.secretKey":  apiHostSecretKey,

		"global.cloud.authUrl.secretName": authUrlSecretName,
		"global.cloud.authUrl.secretKey":  authUrlSecretKey,

		"global.cloud.scadaAddress.secretName": scadaAddressSecretName,
		"global.cloud.scadaAddress.secretKey":  scadaAddressSecretKey,
		"connectInject.default":                "true",

		"global.acls.manageSystemACLs":          "true",
		"global.acls.bootstrapToken.secretName": bootstrapTokenSecretName,
		"global.acls.bootstrapToken.secretKey":  bootstrapTokenSecretKey,

		"global.gossipEncryption.autoGenerate": "false",
		"global.tls.enabled":                   "true",
		"global.tls.enableAutoEncrypt":         "true",

		"telemetryCollector.enabled":                   "true",
		"telemetryCollector.cloud.clientId.secretName": clientIDSecretName,
		"telemetryCollector.cloud.clientId.secretKey":  clientIDSecretKey,

		"telemetryCollector.cloud.clientSecret.secretName": clientSecretName,
		"telemetryCollector.cloud.clientSecret.secretKey":  clientSecretKey,
		// Either we set the global.trustedCAs (make sure it's idented exactly) or we
		// set TLS to insecure

		"telemetryCollector.extraEnvironmentVars.HCP_API_ADDRESS": apiHostSecretKeyValue,
	}

	if cfg.ConsulImage != "" {
		helmValues["global.image"] = cfg.ConsulImage
	}
	if cfg.ConsulCollectorImage != "" {
		helmValues["telemetryCollector.image"] = cfg.ConsulCollectorImage
	}

	consulCluster := consul.NewHelmCluster(t, helmValues, suite.Environment().DefaultContext(t), cfg, releaseName)
	consulCluster.Create(t)

	logger.Log(t, "setting acl permissions for collector and services")
	aclDir := "../fixtures/bases/cloud/service-intentions"
	k8s.KubectlApplyK(t, ctx.KubectlOptions(t), aclDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), aclDir)
	})

	logger.Log(t, "creating static-server deployment")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")
	time.Sleep(1 * time.Hour)

	// TODO: add in test assertions here

}

// fetchAgentBootstrapConfig use the resource-id, client-id, and client-secret
// to call to the agent bootstrap config endpoint and parse the response into a
// CloudBootstrapConfig struct.
func (c *hcp) fetchAgentBootstrapConfig(t *testing.T) string {
	cfg, err := c.HCPConfig()
	require.NoError(t, err)
	logger.Log(t, "Fetching Consul cluster configuration from HCP")
	httpClientCfg := httpclient.Config{
		HCPConfig: cfg,
	}
	clientRuntime, err := httpclient.New(httpClientCfg)
	require.NoError(t, err)

	hcpgnmClient := hcpgnm.New(clientRuntime, nil)
	clusterResource, err := resource.FromString(c.ResourceID)
	require.NoError(t, err)

	params := hcpgnm.NewAgentBootstrapConfigParams().
		WithID(clusterResource.ID).
		WithLocationOrganizationID(clusterResource.Organization).
		WithLocationProjectID(clusterResource.Project)

	resp, err := hcpgnmClient.AgentBootstrapConfig(params, nil)
	require.NoError(t, err)

	bootstrapConfig := resp.GetPayload()
	logger.Log(t, "HCP configuration successfully fetched.")

	return c.parseBootstrapConfigResponse(t, bootstrapConfig)
}

// ConsulConfig represents 'cluster.consul_config' in the response
// fetched from the agent bootstrap config endpoint in HCP.
type ConsulConfig struct {
	ACL ACL `json:"acl"`
}

// ACL represents 'cluster.consul_config.acl' in the response
// fetched from the agent bootstrap config endpoint in HCP.
type ACL struct {
	Tokens Tokens `json:"tokens"`
}

// Tokens represents 'cluster.consul_config.acl.tokens' in the
// response fetched from the agent bootstrap config endpoint in HCP.
type Tokens struct {
	Agent             string `json:"agent"`
	InitialManagement string `json:"initial_management"`
}

// parseBootstrapConfigResponse unmarshals the boostrap parseBootstrapConfigResponse
// and also sets the HCPConfig values to return CloudBootstrapConfig struct.
func (c *hcp) parseBootstrapConfigResponse(t *testing.T, bootstrapRepsonse *models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse) string {

	var consulConfig ConsulConfig
	err := json.Unmarshal([]byte(bootstrapRepsonse.Bootstrap.ConsulConfig), &consulConfig)
	require.NoError(t, err)

	return consulConfig.ACL.Tokens.InitialManagement
}

func (c *hcp) HCPConfig(opts ...hcpcfg.HCPConfigOption) (hcpcfg.HCPConfig, error) {
	if c.ClientID != "" && c.ClientSecret != "" {
		opts = append(opts, hcpcfg.WithClientCredentials(c.ClientID, c.ClientSecret))
	}
	if c.AuthURL != "" {
		opts = append(opts, hcpcfg.WithAuth(c.AuthURL, &tls.Config{}))
	}
	if c.APIHostname != "" {
		opts = append(opts, hcpcfg.WithAPI(c.APIHostname, &tls.Config{}))
	}
	if c.ScadaAddress != "" {
		opts = append(opts, hcpcfg.WithSCADA(c.ScadaAddress, &tls.Config{}))
	}
	opts = append(opts, hcpcfg.FromEnv(), hcpcfg.WithoutBrowserLogin())
	return hcpcfg.NewHCPConfig(opts...)
}
