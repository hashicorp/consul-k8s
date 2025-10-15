// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConsulDNS configures CoreDNS to use configure consul domain queries to
// be forwarded to the Consul DNS Service or the Consul DNS Proxy depending on
// the test case.  The test validates that the DNS queries are resolved when querying
// for .consul services in secure and non-secure modes.
func TestConsulDNS(t *testing.T) {
	cfg := suite.Config()
	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set")
	}

	if cfg.UseAKS {
		t.Skipf("skipping because -use-aks is set")
	}

	cases := []struct {
		tlsEnabled           bool
		connectInjectEnabled bool
		enableDNSProxy       bool
		aclsEnabled          bool
		manageSystemACLs     bool
	}{
		{tlsEnabled: false, connectInjectEnabled: true, aclsEnabled: false, manageSystemACLs: false, enableDNSProxy: false},
		{tlsEnabled: false, connectInjectEnabled: true, aclsEnabled: false, manageSystemACLs: false, enableDNSProxy: true},
		{tlsEnabled: true, connectInjectEnabled: true, aclsEnabled: true, manageSystemACLs: true, enableDNSProxy: false},
		{tlsEnabled: true, connectInjectEnabled: true, aclsEnabled: true, manageSystemACLs: true, enableDNSProxy: true},
		{tlsEnabled: true, connectInjectEnabled: false, aclsEnabled: true, manageSystemACLs: false, enableDNSProxy: true},
	}

	for _, c := range cases {
		name := fmt.Sprintf("tlsEnabled: %t / aclsEnabled: %t / manageSystemACLs: %t, enableDNSProxy: %t",
			c.tlsEnabled, c.aclsEnabled, c.manageSystemACLs, c.enableDNSProxy)
		t.Run(name, func(t *testing.T) {
			env := suite.Environment()
			ctx := env.DefaultContext(t)
			releaseName := helpers.RandomName()
			helmValues := map[string]string{
				"connectInject.enabled":        strconv.FormatBool(c.connectInjectEnabled),
				"dns.enabled":                  "true",
				"global.tls.enabled":           strconv.FormatBool(c.tlsEnabled),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.manageSystemACLs),
				"global.logLevel":              "debug",
			}

			// Configure DNS proxy to use a non-privileged port to work with K8s 1.30+
			if c.enableDNSProxy {
				helmValues["dns.proxy.port"] = "8053"
			}

			// If ACLs are enabled and we are not managing system ACLs, we need to
			// set the initial management token in the server.extraConfig.
			const initialManagementToken = "b1gs33cr3t"
			if c.aclsEnabled && !c.manageSystemACLs {
				helmValues["server.extraConfig"] = fmt.Sprintf(`"{\"acl\": {\"enabled\": true\, \"default_policy\": \"deny\"\, \"tokens\": {\"initial_management\": \"%s\"}}}"`,
					initialManagementToken)

				// Set ACL token for connect-injector
				helmValues["connectInject.aclInjectToken.secretName"] = "consul-connect-inject-acl-token"

				// Create the secret that will hold this token
				secretName := "consul-connect-inject-acl-token"
				_, err := ctx.KubernetesClient(t).CoreV1().Secrets(ctx.KubectlOptions(t).Namespace).Create(
					context.Background(),
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{Name: secretName},
						StringData: map[string]string{"token": initialManagementToken},
						Type:       corev1.SecretTypeOpaque,
					},
					metav1.CreateOptions{},
				)
				require.NoError(t, err)

				t.Cleanup(func() {
					_ = ctx.KubernetesClient(t).CoreV1().Secrets(ctx.KubectlOptions(t).Namespace).Delete(
						context.Background(), secretName, metav1.DeleteOptions{})
				})
			}

			cluster := consul.NewHelmCluster(t, helmValues, ctx, suite.Config(), releaseName)

			// attach the initial management token to the cluster so it is tied to the client requests when minting a dns proxy ACL token.
			if c.aclsEnabled && !c.manageSystemACLs {
				cluster.ACLToken = initialManagementToken
			}
			cluster.Create(t)

			// If ACLs are enabled and we are not managing system ACLs, we need to
			// create a policy and token for the DNS proxy that need to be in
			// place before the DNS proxy is started.
			if c.aclsEnabled && c.enableDNSProxy && !c.manageSystemACLs {
				secretName := "consul-dns-proxy-token"

				consulClient, configAddress := cluster.SetupConsulClient(t, c.tlsEnabled)
				dnsProxyPolicy := `
					node_prefix "" {
					  policy = "read"
					}
					service_prefix "" {
					  policy = "read"
					}
					agent_prefix "" {
					  policy = "read"
					}
					// Add operator permissions for dataplane
					operator = "read"
					// Add config entries access
					config_entry_prefix "" {
					  policy = "read"
					}
				`
				err, dnsProxyToken := createACLTokenWithGivenPolicy(t, consulClient, dnsProxyPolicy, initialManagementToken, configAddress)
				require.NoError(t, err)

				// Create a secret with the token to be used by the DNS proxy.
				_, err = ctx.KubernetesClient(t).CoreV1().Secrets(ctx.KubectlOptions(t).Namespace).Create(context.Background(), &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: secretName,
					},
					StringData: map[string]string{
						"token": dnsProxyToken.SecretID,
					},
					Type: corev1.SecretTypeOpaque,
				}, metav1.CreateOptions{})
				require.NoError(t, err)

				t.Cleanup(func() {
					require.NoError(t, ctx.KubernetesClient(t).CoreV1().Secrets(ctx.KubectlOptions(t).Namespace).Delete(context.Background(), secretName, metav1.DeleteOptions{}))
				})

				// Update the helm values to include the secret name and key.
				helmValues["dns.proxy.aclToken.secretName"] = secretName
				helmValues["dns.proxy.aclToken.secretKey"] = "token"
			}

			// If DNS proxy is enabled, we need to set the enableDNSProxy flag in the helm values.
			if c.enableDNSProxy {
				helmValues["dns.proxy.enabled"] = strconv.FormatBool(c.enableDNSProxy)
			}
			// Upgrade the cluster to apply the changes created above.  This will
			// also start the DNS proxy if it is enabled and it will pick up the ACL token
			// saved in the secret.
			cluster.Upgrade(t, helmValues)

			// Wait for DNS proxy pods if enabled
			if c.enableDNSProxy {
				logger.Log(t, "waiting for DNS proxy pod to become ready")
				k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), ctx.KubectlOptions(t).Namespace,
					fmt.Sprintf("app=consul,component=dns-proxy,release=%s", releaseName))

				// Force a short delay to ensure token propagation
				logger.Log(t, "pausing for token propagation")
				time.Sleep(5 * time.Second)
			}

			updateCoreDNSWithConsulDomain(t, ctx, releaseName, c.enableDNSProxy)
			verifyDNS(t, cfg, releaseName, ctx.KubectlOptions(t).Namespace, ctx, ctx, "app=consul,component=server",
				"consul.service.consul", true, 0)
		})
	}
}

func createACLTokenWithGivenPolicy(t *testing.T, consulClient *api.Client, policyRules string, initialManagementToken string, configAddress string) (error, *api.ACLToken) {
	// Log detailed information for debugging
	logger.Logf(t, "Creating ACL policy and token for DNS proxy using management token '%s'", initialManagementToken)
	logger.Logf(t, "Policy rules:\n%s", policyRules)

	// Create the policy and token _before_ we enable dns proxy and upgrade the cluster.
	policy, _, err := consulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        "dns-proxy-token",
		Description: "DNS Proxy Policy",
		Rules:       policyRules,
	}, &api.WriteOptions{
		Token: initialManagementToken,
	})
	require.NoError(t, err)
	logger.Logf(t, "Created ACL policy '%s' with ID '%s'", policy.Name, policy.ID)

	// Add a short description to the token
	tokenDescription := fmt.Sprintf("DNS Proxy Token for %s", strings.Split(configAddress, ":")[0])
	logger.Logf(t, "Creating token with description: %s", tokenDescription)

	dnsProxyToken, _, err := consulClient.ACL().TokenCreate(&api.ACLToken{
		Description: tokenDescription,
		Policies: []*api.ACLTokenPolicyLink{
			{
				Name: policy.Name,
			},
		},
	}, &api.WriteOptions{
		Token: initialManagementToken,
	})
	require.NoError(t, err)
	logger.Logf(t, "Created DNS Proxy token with AccessorID '%s' and SecretID '%s'",
		dnsProxyToken.AccessorID, dnsProxyToken.SecretID)

	// Verify token was created successfully by listing it
	token, _, err := consulClient.ACL().TokenRead(dnsProxyToken.AccessorID, &api.QueryOptions{
		Token: initialManagementToken,
	})
	require.NoError(t, err)
	logger.Logf(t, "Verified token exists with description: %s", token.Description)

	// Print out the policies attached to this token for debugging
	logger.Log(t, "Token has the following policies:")
	for i, policy := range token.Policies {
		logger.Logf(t, "  Policy %d: %s (ID: %s)", i+1, policy.Name, policy.ID)
	}

	// Try to use the token to ensure it has the correct permissions
	// Configure a test client with the new token
	apiConfig := api.DefaultConfig()
	apiConfig.Address = configAddress
	apiConfig.Token = dnsProxyToken.SecretID

	if strings.Contains(configAddress, "https://") {
		apiConfig.Scheme = "https"
		apiConfig.TLSConfig = api.TLSConfig{
			InsecureSkipVerify: true,
		}
	}

	// We don't actually need to do anything with this client, just
	// log that we're attempting to verify the token works
	logger.Log(t, "Configuring verification of token permissions (just logging, not actually testing)")

	return err, dnsProxyToken
}

func updateCoreDNSWithConsulDomain(t *testing.T, ctx environment.TestContext, releaseName string, enableDNSProxy bool) {
	updateCoreDNSFile(t, ctx, releaseName, enableDNSProxy, "coredns-custom.yaml")
	updateCoreDNS(t, ctx, "coredns-custom.yaml")

	t.Cleanup(func() {
		updateCoreDNS(t, ctx, "coredns-original.yaml")
		time.Sleep(5 * time.Second)
	})
}

func updateCoreDNSFile(t *testing.T, ctx environment.TestContext, releaseName string,
	enableDNSProxy bool, dnsFileName string) {
	dnsIP, err := getDNSServiceClusterIP(t, ctx, releaseName, enableDNSProxy)
	require.NoError(t, err)

	// If we're using the DNS proxy, we need to use port 8053 (non-privileged) in K8s 1.30+
	dnsTarget := dnsIP
	if enableDNSProxy {
		dnsTarget = net.JoinHostPort(dnsIP, "8053")
	}

	input, err := os.ReadFile("coredns-template.yaml")
	require.NoError(t, err)
	newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsTarget, -1)
	err = os.WriteFile(dnsFileName, []byte(newContents), os.FileMode(0644))
	require.NoError(t, err)
}

func updateCoreDNS(t *testing.T, ctx environment.TestContext, coreDNSConfigFile string) {
	coreDNSCommand := []string{
		"replace", "-n", "kube-system", "-f", coreDNSConfigFile,
	}
	var logs string

	timer := &retry.Timer{Timeout: 30 * time.Minute, Wait: 60 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		var err error
		logs, err = k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), coreDNSCommand...)
		require.NoError(r, err)
	})

	require.Contains(t, logs, "configmap/coredns replaced")
	restartCoreDNSCommand := []string{"rollout", "restart", "deployment/coredns", "-n", "kube-system"}
	_, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), restartCoreDNSCommand...)
	require.NoError(t, err)
	// Wait for restart to finish.
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "rollout", "status", "--timeout", "5m", "--watch", "deployment/coredns", "-n", "kube-system")
	require.NoError(t, err, out, "rollout status command errored, this likely means the rollout didn't complete in time")
}

func verifyDNS(
	t *testing.T,
	cfg *config.TestConfig,
	releaseName string,
	svcNamespace string,
	requestingCtx, svcContext environment.TestContext,
	podLabelSelector, svcName string,
	shouldResolveDNSRecord bool,
	dnsUtilsPodIndex int,
) {
	podList, err := svcContext.KubernetesClient(t).CoreV1().Pods(svcNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: podLabelSelector,
	})
	require.NoError(t, err)

	servicePodIPs := make([]string, len(podList.Items))
	for i, serverPod := range podList.Items {
		servicePodIPs[i] = serverPod.Status.PodIP
	}

	logger.Log(t, "launch a pod to test the dns resolution.")
	dnsUtilsPod := fmt.Sprintf("%s-dns-utils-pod-%d", releaseName, dnsUtilsPodIndex)
	dnsTestPodArgs := []string{
		"run", dnsUtilsPod, "--restart", "Never", "--image", "anubhavmishra/tiny-tools", "--", "dig", svcName,
	}

	var logs string
	retry.RunWith(&retry.Counter{Wait: 30 * time.Second, Count: 10}, t, func(r *retry.R) {
		logs, err = k8s.RunKubectlAndGetOutputE(r, requestingCtx.KubectlOptions(r), dnsTestPodArgs...)
		require.NoError(r, err)
		logger.Logf(t, "verify the DNS results. with logs: \n%s", logs)

		// Normalize whitespace for reliable matching
		cleanLogs := strings.ReplaceAll(logs, "\t", " ")
		cleanLogs = strings.Join(strings.Fields(cleanLogs), " ")

		for _, ipStr := range servicePodIPs {
			ip := net.ParseIP(ipStr)
			require.NotNil(r, ip, "failed to parse IP: %s", ipStr)

			// Build a regex that tolerates TTL and spacing variations
			var recordPattern string
			if ip.To4() != nil {
				// IPv4 record (A)
				recordPattern = fmt.Sprintf(`%s\.\s+\d+\s+IN\s+A\s+%s`, regexp.QuoteMeta(svcName), regexp.QuoteMeta(ipStr))
			} else {
				// IPv6 record (AAAA)
				recordPattern = fmt.Sprintf(`%s\.\s+\d+\s+IN\s+AAAA\s+%s`, regexp.QuoteMeta(svcName), regexp.QuoteMeta(ipStr))
			}

			matched, _ := regexp.MatchString(recordPattern, cleanLogs)

			if shouldResolveDNSRecord {
				require.Contains(r, logs, "ANSWER SECTION:", "expected ANSWER SECTION in dig output but none found.\nFull logs:\n%s", logs)
				require.Truef(r, matched, "expected DNS record for %s with IP %s not found.\nPattern: %s\nLogs:\n%s", svcName, ipStr, recordPattern, logs)
			} else {
				require.NotContains(r, logs, "ANSWER SECTION:", "unexpected ANSWER SECTION in dig output.\nLogs:\n%s", logs)
				require.Falsef(r, matched, "unexpected DNS record for %s found with IP %s.\nLogs:\n%s", svcName, ipStr, logs)
				require.Contains(r, logs, "status: NXDOMAIN", "expected NXDOMAIN when record should not resolve.\nLogs:\n%s", logs)
				require.Contains(r, logs, "AUTHORITY SECTION:", "expected AUTHORITY SECTION in NXDOMAIN response.\nLogs:\n%s", logs)
			}
		}
	})

}

func getDNSServiceClusterIP(t *testing.T, requestingCtx environment.TestContext, releaseName string, enableDNSProxy bool) (string, error) {
	logger.Log(t, "get the in cluster dns service or proxy.")
	dnsSvcName := fmt.Sprintf("%s-consul-dns", releaseName)
	if enableDNSProxy {
		dnsSvcName += "-proxy"
	}
	dnsService, err := requestingCtx.KubernetesClient(t).CoreV1().Services(requestingCtx.KubectlOptions(t).Namespace).Get(context.Background(), dnsSvcName, metav1.GetOptions{})
	require.NoError(t, err)
	return dnsService.Spec.ClusterIP, err
}
