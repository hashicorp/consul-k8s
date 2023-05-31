// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloud

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/serf/testutil/retry"
	"github.com/stretchr/testify/require"
)

type TokenResponse struct {
	Token string `json:"token"`
}

var (
	resourceSecretName     = "resource-sec-name"
	resourceSecretKey      = "resource-sec-key"
	resourceSecretKeyValue = "organization/11eb1a35-aac0-f7c7-8fe1-0242ac110008/project/11eb1a35-ab64-d576-8fe1-0242ac110008/hashicorp.consul.global-network-manager.cluster/TEST"

	clientIDSecretName     = "clientid-sec-name"
	clientIDSecretKey      = "clientid-sec-key"
	clientIDSecretKeyValue = "clientid"

	clientSecretName     = "client-sec-name"
	clientSecretKey      = "client-sec-key"
	clientSecretKeyValue = "client-secret"

	apiHostSecretName     = "apihost-sec-name"
	apiHostSecretKey      = "apihost-sec-key"
	apiHostSecretKeyValue = "fake-server:443"

	authUrlSecretName     = "authurl-sec-name"
	authUrlSecretKey      = "authurl-sec-key"
	authUrlSecretKeyValue = "https://fake-server:443"

	scadaAddressSecretName     = "scadaaddress-sec-name"
	scadaAddressSecretKey      = "scadaaddress-sec-key"
	scadaAddressSecretKeyValue = "fake-server:443"
)

// The fake-server has a requestToken endpoint to retrieve the token.
func requestToken(endpoint string) (string, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: tr}
	url := fmt.Sprintf("https://%s/token", endpoint)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return "", errors.New("error creating request")
	}

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return "", errors.New("error making request")
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return "", errors.New("error reading body")
	}

	var tokenResponse TokenResponse
	err = json.Unmarshal(body, &tokenResponse)
	if err != nil {
		fmt.Println("Error parsing response:", err)
		return "", errors.New("error parsing body")
	}

	return tokenResponse.Token, nil

}

func TestBasicCloud(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)

	kubectlOptions := ctx.KubectlOptions(t)
	ns := kubectlOptions.Namespace
	k8sClient := environment.KubernetesClientFromOptions(t, kubectlOptions)

	cfg := suite.Config()

	if cfg.HCPResourceID != "" {
		resourceSecretKeyValue = cfg.HCPResourceID
	}
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, resourceSecretName, resourceSecretKey, resourceSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientIDSecretName, clientIDSecretKey, clientIDSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientSecretName, clientSecretKey, clientSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, apiHostSecretName, apiHostSecretKey, apiHostSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, authUrlSecretName, authUrlSecretKey, authUrlSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, scadaAddressSecretName, scadaAddressSecretKey, scadaAddressSecretKeyValue)

	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/cloud/hcp-mock")
	podName, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "get", "pod", "-l", "app=fake-server", "-o", `jsonpath="{.items[0].metadata.name}"`)
	podName = strings.ReplaceAll(podName, "\"", "")
	if err != nil {
		logger.Log(t, "error finding pod name")
		return
	}
	logger.Log(t, "fake-server pod name:"+podName)
	localPort := terratestk8s.GetAvailablePort(t)
	tunnel := terratestk8s.NewTunnelWithLogger(
		ctx.KubectlOptions(t),
		terratestk8s.ResourceTypePod,
		podName,
		localPort,
		443,
		logger.TestLogger{})

	// Retry creating the port forward since it can fail occasionally.
	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
		// NOTE: It's okay to pass in `t` to ForwardPortE despite being in a retry
		// because we're using ForwardPortE (not ForwardPort) so the `t` won't
		// get used to fail the test, just for logging.
		require.NoError(r, tunnel.ForwardPortE(t))
	})

	logger.Log(t, "fake-server addr:"+tunnel.Endpoint())
	consulToken, err := requestToken(tunnel.Endpoint())
	if err != nil {
		logger.Log(t, "error finding consul token")
		return
	}
	tunnel.Close()
	logger.Log(t, "consul test token :"+consulToken)

	releaseName := helpers.RandomName()

	helmValues := map[string]string{
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

		// TODO: Follow up with this bug
		"global.acls.manageSystemACLs":         "false",
		"global.gossipEncryption.autoGenerate": "false",
		"global.tls.enabled":                   "true",
		"global.tls.enableAutoEncrypt":         "true",
		// TODO: Take this out

		"telemetryCollector.enabled":                   "true",
		"telemetryCollector.image":                     cfg.ConsulCollectorImage,
		"telemetryCollector.cloud.clientId.secretName": clientIDSecretName,
		"telemetryCollector.cloud.clientId.secretKey":  clientIDSecretKey,

		"telemetryCollector.cloud.clientSecret.secretName": clientSecretName,
		"telemetryCollector.cloud.clientSecret.secretKey":  clientSecretKey,
		// Either we set the global.trustedCAs (make sure it's idented exactly) or we
		// set TLS to insecure

		"telemetryCollector.extraEnvironmentVars.HCP_API_TLS":       "insecure",
		"telemetryCollector.extraEnvironmentVars.HCP_AUTH_TLS":      "insecure",
		"telemetryCollector.extraEnvironmentVars.HCP_SCADA_TLS":     "insecure",
		"telemetryCollector.extraEnvironmentVars.OTLP_EXPORTER_TLS": "insecure",

		"server.extraEnvironmentVars.HCP_API_TLS":   "insecure",
		"server.extraEnvironmentVars.HCP_AUTH_TLS":  "insecure",
		"server.extraEnvironmentVars.HCP_SCADA_TLS": "insecure",

		// This is pregenerated CA used for testing. It can be replaced at any time and isn't
		// meant for anything other than testing
		// 		"global.trustedCAs[0]": `-----BEGIN CERTIFICATE-----
		// MIICrjCCAZYCCQD5LxMcnMY8rDANBgkqhkiG9w0BAQsFADAZMRcwFQYDVQQDDA5m
		// YWtlLXNlcnZlci1jYTAeFw0yMzA1MTkxMjIwMzhaFw0zMzA1MTYxMjIwMzhaMBkx
		// FzAVBgNVBAMMDmZha2Utc2VydmVyLWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
		// MIIBCgKCAQEAwhbiII7sMultedFzQVhVZz5Ti+9lWrpZb8y0ZR6NaNvoxDPX151t
		// Adh5NegSeH/+351iDBGZHhmKECtBuk8FJgk88O7y8A7Yg+/lyeZd0SJTEeiYUe7d
		// sSaBTYSmixyn6s15Y5MVp9gM7t2YXrocRkFxDtdhLMWf0zwzJEwDouFMMiFZw5II
		// yDbI6UfwKyB8C8ln10+TcczbheaOMQ1jGn35YWAG/LEdutU6DO2Y/GZYQ41nyLF1
		// klqh34USQPVQSQW7R7GiDxyhh1fGaDF6RAzH4RerzQSNvvTHmBXIGurB/Hnu1n3p
		// CwWeatWMU5POy1es73S/EPM0NpWD5RabSwIDAQABMA0GCSqGSIb3DQEBCwUAA4IB
		// AQBayoTltSW55PvKVp9cmqGOBMlkIMKPd6Ny4bCb/3UF+3bzQmIblh3O3kEt7WoY
		// fA9vp+6cSRGVqgBfR2bi40RrerLNA79yywIZjfBMteNuRoul5VeD+mLyFCo4197r
		// Atl2TEx2kl2V8rjCsEBcTqKqetVOMLYEZ2tbCeUt1A/K7OzaJfHgelEYcsVt68Q9
		// /BLoo2UXfOpRrcsx7u7s5HPVbG3bx+1MvGJZ2C3i0B6agnkGDzEpoM4KZGxEefB9
		// DOHIJfie9d9BQD52nZh3SGHz0b3vfJ430XrQmaNZ26fuIEyIYrpvyAhBXckj2iTD
		// 1TXpqr/1D7EUbddktyhXTK9e
		// -----END CERTIFICATE-----`,
	}
	if cfg.ConsulImage != "" {
		helmValues["global.image"] = cfg.ConsulImage
	}
	if cfg.ConsulCollectorImage != "" {
		helmValues["telemetryCollector.image"] = cfg.ConsulCollectorImage
	}

	consulCluster := consul.NewHelmCluster(t, helmValues, suite.Environment().DefaultContext(t), suite.Config(), releaseName)
	consulCluster.Create(t)

	logger.Log(t, "creating static-server deployment")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server")
	// time.Sleep(1 * time.Hour)
	// TODO: add in test assertions here
}
