// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// TestConsulDNSProxy_PrivilegedPort tests that the Consul DNS proxy works correctly with privileged ports.
// It configures CoreDNS to use Consul domain queries forwarded to the Consul DNS Proxy
// using a privileged port (53, the standard DNS port). The test validates that the DNS queries
// are resolved correctly in both secure and non-secure modes.
// TestConsulDNS_PrivilegedPort configures CoreDNS to use configure consul domain queries to
// be forwarded to the Consul DNS Service or the Consul DNS Proxy depending on
// the test case, using a privileged port (53). The test validates that the DNS queries are
// resolved when querying for .consul services in secure and non-secure modes.
func TestConsulDNS_PrivilegedPort(t *testing.T) {
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
		name := fmt.Sprintf("privileged-port / tlsEnabled: %t / aclsEnabled: %t / manageSystemACLs: %t, enableDNSProxy: %t",
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

			// Configure DNS proxy to use a privileged port
			if c.enableDNSProxy {
				helmValues["dns.proxy.enabled"] = "true"
				helmValues["dns.proxy.port"] = "53"
			}

			// If ACLs are enabled and we are not managing system ACLs, we need to
			// set the initial management token in the server.extraConfig.
			const initialManagementToken = "b1gs33cr3t"
			if c.aclsEnabled && !c.manageSystemACLs {
				helmValues["server.extraConfig"] = fmt.Sprintf(`"{\"acl\": {\"enabled\": true\, \"default_policy\": \"deny\"\, \"tokens\": {\"initial_management\": \"%s\"}}}"`,
					initialManagementToken)
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

			// Wait specifically for DNS proxy pods if enabled
			if c.enableDNSProxy {
				k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), ctx.KubectlOptions(t).Namespace,
					fmt.Sprintf("app=consul,component=dns-proxy,release=%s", releaseName))
			}

			updateCoreDNSWithConsulDomainPrivilegedPort(t, ctx, releaseName, c.enableDNSProxy)
			verifyDNSWithPrivilegedPort(t, releaseName, ctx.KubectlOptions(t).Namespace, ctx, ctx, "app=consul,component=server",
				"consul.service.consul", true, 0)

			// For DNS proxy with privileged port, verify the privileged container is used
			if c.enableDNSProxy {
				verifyDNSProxyUsesPrivilegedCommand(t, ctx, releaseName)
			}
		})
	}
}

func TestConsulDNSProxy_PrivilegedPort(t *testing.T) {
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
		aclsEnabled          bool
		manageSystemACLs     bool
		name                 string
	}{
		{
			name:                 "not secure - ACLs and auto-encrypt not enabled",
			tlsEnabled:           false,
			connectInjectEnabled: true,
			aclsEnabled:          false,
			manageSystemACLs:     false,
		},
		{
			name:                 "secure - ACLs and auto-encrypt enabled",
			tlsEnabled:           true,
			connectInjectEnabled: true,
			aclsEnabled:          true,
			manageSystemACLs:     true,
		},
		{
			name:                 "TLS enabled but connect-inject disabled",
			tlsEnabled:           true,
			connectInjectEnabled: false,
			aclsEnabled:          true,
			manageSystemACLs:     false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := suite.Environment()
			ctx := env.DefaultContext(t)

			releaseName := helpers.RandomName()

			// Configure privileged port 53 (standard DNS port) for the DNS proxy
			helmValues := map[string]string{
				"connectInject.enabled":        strconv.FormatBool(c.connectInjectEnabled),
				"dns.enabled":                  "true",
				"dns.proxy.enabled":            "true",
				"dns.proxy.port":               "53", // Use privileged port
				"global.tls.enabled":           strconv.FormatBool(c.tlsEnabled),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.manageSystemACLs),
				"global.logLevel":              "debug",
			}

			// If ACLs are enabled and we are not managing system ACLs, we need to
			// set the initial management token in the server.extraConfig.
			const initialManagementToken = "b1gs33cr3t"
			if c.aclsEnabled && !c.manageSystemACLs {
				helmValues["server.extraConfig"] = fmt.Sprintf(`"{\"acl\": {\"enabled\": true\, \"default_policy\": \"deny\"\, \"tokens\": {\"initial_management\": \"%s\"}}}"`,
					initialManagementToken)
			}

			cluster := consul.NewHelmCluster(t, helmValues, ctx, suite.Config(), releaseName)

			// Attach the initial management token to the cluster so it is tied to the client requests
			// when minting a dns proxy ACL token.
			if c.aclsEnabled && !c.manageSystemACLs {
				cluster.ACLToken = initialManagementToken
			}

			cluster.Create(t)

			// If ACLs are enabled and we are not managing system ACLs, we need to
			// create a policy and token for the DNS proxy that need to be in
			// place before the DNS proxy is started.
			if c.aclsEnabled && !c.manageSystemACLs {
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

				// Upgrade the cluster to apply the changes created above.
				cluster.Upgrade(t, helmValues)

				// Wait for DNS proxy to become ready
				logger.Log(t, "waiting for DNS proxy pod to become ready")
				k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), ctx.KubectlOptions(t).Namespace,
					fmt.Sprintf("app=consul,component=dns-proxy,release=%s", releaseName))

				// Wait for DNS proxy to become ready
				logger.Log(t, "waiting for DNS proxy pod to become ready")
				k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), ctx.KubectlOptions(t).Namespace,
					fmt.Sprintf("app=consul,component=dns-proxy,release=%s", releaseName))
			}

			// Update CoreDNS to use Consul DNS
			updateCoreDNSWithConsulDomainPrivilegedPort(t, ctx, releaseName, true) // DNS proxy is enabled in TestConsulDNSProxy_PrivilegedPort

			// Verify that the Consul service can be resolved through DNS
			verifyDNSWithPrivilegedPort(t, releaseName, ctx.KubectlOptions(t).Namespace, ctx, ctx, "app=consul,component=server", "consul.service.consul", true, 0)

			// Additionally, check that the command in DNS proxy pod is using privileged-consul-dataplane
			// as it's running on a privileged port
			verifyDNSProxyUsesPrivilegedCommand(t, ctx, releaseName)
		})
	}
}

func updateCoreDNSWithConsulDomainPrivilegedPort(t *testing.T, ctx environment.TestContext, releaseName string, enableDNSProxy bool) {
	// For privileged port, don't need to specify port number, using default DNS port 53
	updateCoreDNSFileForPrivilegedPort(t, ctx, releaseName, "coredns-custom.yaml", enableDNSProxy)
	updateCoreDNS(t, ctx, "coredns-custom.yaml")

	t.Cleanup(func() {
		updateCoreDNS(t, ctx, "coredns-original.yaml")
		time.Sleep(5 * time.Second)
	})
}

func updateCoreDNSFileForPrivilegedPort(t *testing.T, ctx environment.TestContext, releaseName string, dnsFileName string, enableDNSProxy bool) {
	dnsIP, err := getDNSServiceOrProxyIP(t, ctx, releaseName, enableDNSProxy)
	require.NoError(t, err)

	// When using a privileged port (53), we don't need to specify the port in the CoreDNS config
	input, err := os.ReadFile("coredns-template.yaml")
	require.NoError(t, err)

	// Replace the template placeholder with the DNS IP (no port needed for standard DNS port 53)
	newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsIP, -1)
	err = os.WriteFile(dnsFileName, []byte(newContents), 0644)
	require.NoError(t, err)
}

func verifyDNSWithPrivilegedPort(t *testing.T, releaseName string, svcNamespace string, requestingCtx, svcContext environment.TestContext,
	podLabelSelector, svcName string, shouldResolveDNSRecord bool, dnsUtilsPodIndex int) {
	podList, err := svcContext.KubernetesClient(t).CoreV1().Pods(svcNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: podLabelSelector,
	})
	require.NoError(t, err)

	servicePodIPs := make([]string, len(podList.Items))
	for i, serverPod := range podList.Items {
		servicePodIPs[i] = serverPod.Status.PodIP
	}

	logger.Log(t, "launch a pod to test the dns resolution with privileged port.")
	dnsUtilsPod := fmt.Sprintf("%s-dns-utils-privileged-pod-%d", releaseName, dnsUtilsPodIndex)
	dnsTestPodArgs := []string{
		"run", "-it", dnsUtilsPod, "--restart", "Never", "--image", "anubhavmishra/tiny-tools", "--", "dig", svcName,
	}

	helpers.Cleanup(t, suite.Config().NoCleanupOnFailure, suite.Config().NoCleanup, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		k8s.RunKubectl(t, requestingCtx.KubectlOptions(t), "delete", "pod", dnsUtilsPod)
	})

	retry.Run(t, func(r *retry.R) {
		logger.Log(t, "run the dns utilize pod and query DNS for the service with privileged port.")
		logs, err := k8s.RunKubectlAndGetOutputE(r, requestingCtx.KubectlOptions(r), dnsTestPodArgs...)
		require.NoError(r, err)

		// When the `dig` request is successful, a section of it's response looks like the following:
		//
		// ;; ANSWER SECTION:
		// consul.service.consul.	0	IN	A	<consul-server-pod-ip>
		//
		// ;; Query time: 2 msec
		// ;; SERVER: <dns-ip>#<dns-port>(<dns-ip>)
		// ;; WHEN: Mon Aug 10 15:02:40 UTC 2020
		// ;; MSG SIZE  rcvd: 98
		//
		// We assert on the existence of the ANSWER SECTION, The consul-server IPs being present
		// in the ANSWER SECTION and the the DNS IP mentioned in the SERVER: field

		logger.Log(t, "verify the DNS results for privileged port.")
		// Strip logs of tabs, newlines and spaces to make it easier to assert on the content when there is a DNS match
		strippedLogs := strings.Replace(logs, "\t", "", -1)
		strippedLogs = strings.Replace(strippedLogs, "\n", "", -1)
		strippedLogs = strings.Replace(strippedLogs, " ", "", -1)

		for _, ip := range servicePodIPs {
			aRecordPattern := "%s.5INA%s"
			aRecord := fmt.Sprintf(aRecordPattern, svcName, ip)
			if shouldResolveDNSRecord {
				require.Contains(r, logs, "ANSWER SECTION:")
				require.Contains(r, strippedLogs, aRecord)
				// Check that the server is responding on port 53 (the privileged port)
				require.Contains(r, logs, "SERVER:")
				// When using privileged port 53, the log should have #53 in the SERVER line
				serverLine := getServerLineFromDigOutput(logs)
				require.Contains(r, serverLine, "#53")
			} else {
				require.NotContains(r, logs, "ANSWER SECTION:")
				require.NotContains(r, strippedLogs, aRecord)
				require.Contains(r, logs, "status: NXDOMAIN")
				require.Contains(r, logs, "AUTHORITY SECTION:\nconsul.\t\t\t5\tIN\tSOA\tns.consul. hostmaster.consul.")
			}
		}
	})
}

// getServerLineFromDigOutput extracts the SERVER line from dig command output
func getServerLineFromDigOutput(digOutput string) string {
	lines := strings.Split(digOutput, "\n")
	for _, line := range lines {
		if strings.Contains(line, "SERVER:") {
			return line
		}
	}
	return ""
}

// verifyDNSProxyUsesPrivilegedCommand verifies that the DNS proxy is using the privileged command
// when configured with a privileged port
func verifyDNSProxyUsesPrivilegedCommand(t *testing.T, ctx environment.TestContext, releaseName string) {

	// Get the DNS proxy pod
	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=consul,component=dns-proxy,release=%s", releaseName),
	})
	require.NoError(t, err)
	require.NotEmpty(t, podList.Items, "No DNS proxy pods found")

	dnsProxyPod := podList.Items[0].Name

	// Check the container command
	describeCmd := []string{
		"describe", "pod", dnsProxyPod,
	}

	logger.Log(t, "checking DNS proxy pod configuration")
	output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), describeCmd...)
	require.NoError(t, err)

	// For privileged port, the command should be privileged-consul-dataplane
	require.Contains(t, output, "privileged-consul-dataplane")

	// Also check for the privileged-envoy flag
	require.Contains(t, output, "-envoy-executable-path=/usr/local/bin/privileged-envoy")

	logger.Log(t, "verified DNS proxy is using privileged executable for port 53")
}
