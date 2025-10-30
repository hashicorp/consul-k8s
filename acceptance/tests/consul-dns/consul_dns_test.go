// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"fmt"
	"net"
	"os"
	"slices"
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
		port                 string
	}{
		{tlsEnabled: false, connectInjectEnabled: true, aclsEnabled: false, manageSystemACLs: false, enableDNSProxy: false, port: privilegedPort},
		{tlsEnabled: false, connectInjectEnabled: true, aclsEnabled: false, manageSystemACLs: false, enableDNSProxy: true, port: privilegedPort},
		{tlsEnabled: true, connectInjectEnabled: true, aclsEnabled: true, manageSystemACLs: true, enableDNSProxy: false, port: privilegedPort},
		{tlsEnabled: true, connectInjectEnabled: true, aclsEnabled: true, manageSystemACLs: true, enableDNSProxy: true, port: privilegedPort},
		{tlsEnabled: true, connectInjectEnabled: false, aclsEnabled: true, manageSystemACLs: false, enableDNSProxy: true, port: privilegedPort},
		{tlsEnabled: false, connectInjectEnabled: true, aclsEnabled: false, manageSystemACLs: false, enableDNSProxy: false, port: nonPrivilegedPort},
		{tlsEnabled: false, connectInjectEnabled: true, aclsEnabled: false, manageSystemACLs: false, enableDNSProxy: true, port: nonPrivilegedPort},
		{tlsEnabled: true, connectInjectEnabled: true, aclsEnabled: true, manageSystemACLs: true, enableDNSProxy: false, port: nonPrivilegedPort},
		{tlsEnabled: true, connectInjectEnabled: true, aclsEnabled: true, manageSystemACLs: true, enableDNSProxy: true, port: nonPrivilegedPort},
		{tlsEnabled: true, connectInjectEnabled: false, aclsEnabled: true, manageSystemACLs: false, enableDNSProxy: true, port: nonPrivilegedPort},
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

			if c.enableDNSProxy {
				helmValues["dns.proxy.port"] = c.port
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

			updateCoreDNSWithConsulDomain(t, ctx, releaseName, c.enableDNSProxy, c.port)
			// Validate DNS proxy privileged port configuration when DNS proxy is enabled
			if c.enableDNSProxy && c.port == privilegedPort {
				validateDNSProxyPrivilegedPort(t, ctx, releaseName)
			}
			verifyDNS(t, cfg, releaseName, ctx.KubectlOptions(t).Namespace, ctx, ctx, "app=consul,component=server",
				"consul.service.consul", true, 0)
		})
	}
}

func createACLTokenWithGivenPolicy(t *testing.T, consulClient *api.Client, policyRules string, initialManagementToken string, configAddress string) (error, *api.ACLToken) {
	_, _, err := consulClient.ACL().TokenCreate(&api.ACLToken{}, &api.WriteOptions{
		Token: initialManagementToken,
	})
	require.NoError(t, err)

	// Create the policy and token _before_ we enable dns proxy and upgrade the cluster.
	require.NoError(t, err)
	policy, _, err := consulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        "dns-proxy-token",
		Description: "DNS Proxy Policy",
		Rules:       policyRules,
	}, nil)
	require.NoError(t, err)
	dnsProxyToken, _, err := consulClient.ACL().TokenCreate(&api.ACLToken{
		Description: fmt.Sprintf("DNS Proxy Token for %s", strings.Split(configAddress, ":")[0]),
		Policies: []*api.ACLTokenPolicyLink{
			{
				Name: policy.Name,
			},
		},
	}, nil)
	require.NoError(t, err)
	logger.Log(t, "created DNS Proxy token", "token", dnsProxyToken)
	return err, dnsProxyToken
}

func updateCoreDNSWithConsulDomain(t *testing.T, ctx environment.TestContext, releaseName string, enableDNSProxy bool, port string) {
	updateCoreDNSFile(t, ctx, releaseName, enableDNSProxy, port, "coredns-custom.yaml")
	updateCoreDNS(t, ctx, "coredns-custom.yaml")

	t.Cleanup(func() {
		updateCoreDNS(t, ctx, "coredns-original.yaml")
		time.Sleep(5 * time.Second)
	})
}

func updateCoreDNSFile(t *testing.T, ctx environment.TestContext, releaseName string,
	enableDNSProxy bool, port string, dnsFileName string) {
	dnsIP, err := getDNSServiceClusterIP(t, ctx, releaseName, enableDNSProxy)
	require.NoError(t, err)

	dnsTarget := dnsIP
	if enableDNSProxy {
		dnsTarget = net.JoinHostPort(dnsIP, port)
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
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "rollout", "status", "--timeout", "1m", "--watch", "deployment/coredns", "-n", "kube-system")
	require.NoError(t, err, out, "rollout status command errored, this likely means the rollout didn't complete in time")
}

func verifyDNS(t *testing.T, cfg *config.TestConfig, releaseName string, svcNamespace string, requestingCtx, svcContext environment.TestContext,
	podLabelSelector, svcName string, shouldResolveDNSRecord bool, dnsUtilsPodIndex int) {
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
		"run", "-it", dnsUtilsPod, "--restart", "Never", "--image", "anubhavmishra/tiny-tools", "--", "dig", svcName,
	}

	helpers.Cleanup(t, suite.Config().NoCleanupOnFailure, suite.Config().NoCleanup, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		k8s.RunKubectl(t, requestingCtx.KubectlOptions(t), "delete", "pod", dnsUtilsPod)
	})

	retry.Run(t, func(r *retry.R) {
		logger.Log(t, "run the dns utilize pod and query DNS for the service.")
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
		// We assert on the existence of the ANSWER SECTION, The consul-server IPs being present in the ANSWER SECTION and the the DNS IP mentioned in the SERVER: field

		logger.Log(t, "verify the DNS results.")
		// strip logs of tabs, newlines and spaces to make it easier to assert on the content when there is a DNS match
		strippedLogs := strings.Replace(logs, "\t", "", -1)
		strippedLogs = strings.Replace(strippedLogs, "\n", "", -1)
		strippedLogs = strings.Replace(strippedLogs, " ", "", -1)
		for _, ip := range servicePodIPs {
			aRecordPattern := "%s.5INA%s"
			aRecord := fmt.Sprintf(aRecordPattern, svcName, ip)
			if shouldResolveDNSRecord {
				require.Contains(r, logs, "ANSWER SECTION:")
				require.Contains(r, strippedLogs, aRecord)
			} else {
				require.NotContains(r, logs, "ANSWER SECTION:")
				require.NotContains(r, strippedLogs, aRecord)
				require.Contains(r, logs, "status: NXDOMAIN")
				require.Contains(r, logs, "AUTHORITY SECTION:\nconsul.\t\t\t5\tIN\tSOA\tns.consul. hostmaster.consul.")
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

// validateDNSProxyPrivilegedPort validates that the consul-dns-proxy pod is correctly configured
// to use privileged port with appropriate command and envoy arguments.
func validateDNSProxyPrivilegedPort(t *testing.T, ctx environment.TestContext, releaseName string) {
	logger.Log(t, "validating DNS proxy pod uses privileged port", privilegedPort)

	var pod corev1.Pod

	// Wait for DNS proxy pod to be created and ready with retry
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 30}, t, func(r *retry.R) {
		pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app=consul,component=dns-proxy,release=%s", releaseName),
		})
		require.NoError(r, err)
		require.NotEmpty(r, pods.Items, "DNS proxy pod should exist")

		pod = pods.Items[0]
		require.Equal(r, corev1.PodRunning, pod.Status.Phase, "DNS proxy pod should be running")
	})

	logger.Log(t, "found DNS proxy pod", "name", pod.Name)

	// Find the consul-dns-proxy container
	var dnsProxyContainer *corev1.Container
	for i, container := range pod.Spec.Containers {
		if container.Name == "dns-proxy" {
			dnsProxyContainer = &pod.Spec.Containers[i]
			break
		}
	}
	require.NotNil(t, dnsProxyContainer, "dns-proxy container should exist")

	// Validate command arguments include privilegedPort
	commandArgs := strings.Join(dnsProxyContainer.Args, " ")
	require.Contains(t, commandArgs, fmt.Sprintf("-consul-dns-bind-port=%s", privilegedPort), fmt.Sprintf("DNS proxy command should include -consul-dns-bind-port=%s argument", privilegedPort))
	logger.Log(t, "validated DNS proxy command includes -consul-dns-bind-port=", privilegedPort, "args", commandArgs)

	// Validate privileged-envoy executable is used
	require.Contains(t, commandArgs, "-envoy-executable-path=/usr/local/bin/privileged-envoy", "Envoy should have admin port configured")
	logger.Log(t, "validated envoy configuration in DNS proxy")

	logger.Log(t, "successfully validated DNS proxy privileged port", privilegedPort)

	// Validate privileged port is configured
	var foundPrivilegedPort bool
	privilegedPortInt, _ := strconv.Atoi(privilegedPort)
	for _, port := range dnsProxyContainer.Ports {
		if port.ContainerPort == int32(privilegedPortInt) {
			foundPrivilegedPort = true
			require.Contains(t, port.Name, "dns")
			logger.Log(t, "validated DNS proxy uses port", privilegedPort, "port", port.ContainerPort, "name", port.Name)
			break
		}
	}
	require.True(t, foundPrivilegedPort, fmt.Sprintf("DNS proxy container should expose port %s", privilegedPort))

	// Validate security context has privileged capabilities
	require.NotNil(t, dnsProxyContainer.SecurityContext, "DNS proxy container should have security context")
	require.NotNil(t, dnsProxyContainer.SecurityContext.Capabilities, "DNS proxy container should have capabilities configured")
	require.NotNil(t, dnsProxyContainer.SecurityContext.Capabilities.Add, "DNS proxy container should have added capabilities")

	// Check for NET_BIND_SERVICE capability (required for privileged ports)
	var hasNetBindService bool
	if slices.Contains(dnsProxyContainer.SecurityContext.Capabilities.Add, "NET_BIND_SERVICE") {
		hasNetBindService = true
		logger.Log(t, "validated DNS proxy has NET_BIND_SERVICE capability")
	}
	require.True(t, hasNetBindService, "DNS proxy container should have NET_BIND_SERVICE capability for privileged port")
}
