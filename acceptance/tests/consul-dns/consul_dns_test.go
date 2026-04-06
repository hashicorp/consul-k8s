// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
		// {tlsEnabled: false, connectInjectEnabled: true, aclsEnabled: false, manageSystemACLs: false, enableDNSProxy: false},
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

			updateCoreDNSWithConsulDomain(t, ctx, releaseName, c.enableDNSProxy)
			verifyDNS(t, releaseName, ctx.KubectlOptions(t).Namespace, ctx, ctx, "app=consul,component=server",
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

func updateCoreDNSWithConsulDomain(t *testing.T, ctx environment.TestContext, releaseName string, enableDNSProxy bool) {
	cfg := suite.Config()
	dnsConfig := clusterDNSConfigFor(cfg)
	if cfg.UseOpenshift || cfg.EnableOpenshift {
		originalServers := backupOpenShiftDNSServers(t, ctx)
		updateOpenShiftDNSWithConsulDomain(t, ctx, releaseName, enableDNSProxy)

		t.Cleanup(func() {
			restoreOpenShiftDNSServers(t, ctx, originalServers)
			time.Sleep(5 * time.Second)
		})
		return
	}

	originalConfigFile := backupDNSConfigMap(t, ctx, dnsConfig)
	customConfigFile := renderDNSConfigMap(t, ctx, releaseName, enableDNSProxy, dnsConfig)

	updateCoreDNS(t, ctx, dnsConfig, customConfigFile)

	t.Cleanup(func() {
		updateCoreDNS(t, ctx, dnsConfig, originalConfigFile)
		time.Sleep(5 * time.Second)
		_ = os.Remove(customConfigFile)
		_ = os.Remove(originalConfigFile)
	})
}

const openShiftConsulDNSServerName = "consul-test"

func backupOpenShiftDNSServers(t *testing.T, ctx environment.TestContext) []interface{} {
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "get", "dns.operator/default", "-o", "json")
	require.NoError(t, err)

	var dns map[string]interface{}
	err = json.Unmarshal([]byte(out), &dns)
	require.NoError(t, err)

	spec, ok := dns["spec"].(map[string]interface{})
	require.True(t, ok)

	servers, ok := spec["servers"].([]interface{})
	if !ok {
		return []interface{}{}
	}

	return servers
}

func updateOpenShiftDNSWithConsulDomain(t *testing.T, ctx environment.TestContext, releaseName string, enableDNSProxy bool) {
	dnsIP, err := getDNSServiceClusterIP(t, ctx, releaseName, enableDNSProxy)
	require.NoError(t, err)

	upstream := dnsIP
	if enableDNSProxy {
		upstream = fmt.Sprintf("%s:8053", dnsIP)
	}

	originalServers := backupOpenShiftDNSServers(t, ctx)
	filteredServers := make([]interface{}, 0, len(originalServers)+1)
	for _, srv := range originalServers {
		srvMap, ok := srv.(map[string]interface{})
		if !ok {
			filteredServers = append(filteredServers, srv)
			continue
		}
		if name, _ := srvMap["name"].(string); name == openShiftConsulDNSServerName {
			continue
		}
		filteredServers = append(filteredServers, srv)
	}

	filteredServers = append(filteredServers, map[string]interface{}{
		"name":  openShiftConsulDNSServerName,
		"zones": []string{"consul"},
		"forwardPlugin": map[string]interface{}{
			"policy":    "Sequential",
			"upstreams": []string{upstream},
		},
	})

	applyOpenShiftDNSServers(t, ctx, filteredServers)
	waitForOpenShiftDNSReconcile(t, ctx)
	waitForOpenShiftDNSRollout(t, ctx)
	waitForOpenShiftCorefileForwarder(t, ctx, upstream)
}

func restoreOpenShiftDNSServers(t *testing.T, ctx environment.TestContext, servers []interface{}) {
	applyOpenShiftDNSServers(t, ctx, servers)
	waitForOpenShiftDNSReconcile(t, ctx)
	waitForOpenShiftDNSRollout(t, ctx)
}

func applyOpenShiftDNSServers(t *testing.T, ctx environment.TestContext, servers []interface{}) {
	serversBytes, err := json.Marshal(servers)
	require.NoError(t, err)

	patch := fmt.Sprintf(`{"spec":{"servers":%s}}`, string(serversBytes))
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "patch", "dns.operator/default", "--type=merge", "-p", patch)
	require.NoError(t, err, out)
}

func waitForOpenShiftDNSReconcile(t *testing.T, ctx environment.TestContext) {
	timer := &retry.Timer{Timeout: 10 * time.Minute, Wait: 10 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "get", "dns.operator/default", "-o", "json")
		require.NoError(r, err)

		var dns map[string]interface{}
		err = json.Unmarshal([]byte(out), &dns)
		require.NoError(r, err)

		status, ok := dns["status"].(map[string]interface{})
		require.True(r, ok)
		conditions, ok := status["conditions"].([]interface{})
		require.True(r, ok)

		availableTrue := false
		progressingFalse := false
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			typeStr, _ := condMap["type"].(string)
			statusStr, _ := condMap["status"].(string)
			if typeStr == "Available" && statusStr == "True" {
				availableTrue = true
			}
			if typeStr == "Progressing" && statusStr == "False" {
				progressingFalse = true
			}
		}

		if !(availableTrue && progressingFalse) {
			r.Errorf("waiting for OpenShift DNS reconcile; available=%t progressingFalse=%t", availableTrue, progressingFalse)
		}
	})
}

func waitForOpenShiftDNSRollout(t *testing.T, ctx environment.TestContext) {
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "rollout", "status", "--timeout", "10m", "--watch", "daemonset/dns-default", "-n", "openshift-dns")
	require.NoError(t, err, out)
}

func waitForOpenShiftCorefileForwarder(t *testing.T, ctx environment.TestContext, upstream string) {
	timer := &retry.Timer{Timeout: 10 * time.Minute, Wait: 10 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "get", "configmap", "dns-default", "-n", "openshift-dns", "-o", "json")
		require.NoError(r, err)

		var cm map[string]interface{}
		err = json.Unmarshal([]byte(out), &cm)
		require.NoError(r, err)

		data, ok := cm["data"].(map[string]interface{})
		require.True(r, ok)
		corefile, _ := data["Corefile"].(string)

		if !strings.Contains(corefile, "consul:5353") || !strings.Contains(corefile, upstream) {
			r.Errorf("waiting for OpenShift DNS Corefile to include consul forwarder to %q", upstream)
		}
	})
}

type clusterDNSConfig struct {
	configMapName string
	namespace     string
	workloadKind  string
	workloadName  string
}

func clusterDNSConfigFor(cfg *config.TestConfig) clusterDNSConfig {
	if cfg.UseOpenshift || cfg.EnableOpenshift {
		return clusterDNSConfig{
			configMapName: "dns-default",
			namespace:     "openshift-dns",
			workloadKind:  "daemonset",
			workloadName:  "dns-default",
		}
	}

	return clusterDNSConfig{
		configMapName: "coredns",
		namespace:     "kube-system",
		workloadKind:  "deployment",
		workloadName:  "coredns",
	}
}

func backupDNSConfigMap(t *testing.T, ctx environment.TestContext, dnsConfig clusterDNSConfig) string {
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "get", "configmap", dnsConfig.configMapName, "-n", dnsConfig.namespace, "-o", "yaml")
	require.NoError(t, err)

	file, err := os.CreateTemp("", "consul-dns-original-*.yaml")
	require.NoError(t, err)

	err = os.WriteFile(file.Name(), []byte(out), os.FileMode(0644))
	require.NoError(t, err)

	return file.Name()
}

func renderDNSConfigMap(t *testing.T, ctx environment.TestContext, releaseName string,
	enableDNSProxy bool, dnsConfig clusterDNSConfig) string {
	dnsIP, err := getDNSServiceClusterIP(t, ctx, releaseName, enableDNSProxy)
	require.NoError(t, err)

	// If we're using the DNS proxy, we need to use port 8053 (non-privileged) in K8s 1.30+
	dnsTarget := dnsIP
	if enableDNSProxy {
		dnsTarget = fmt.Sprintf("%s:8053", dnsIP)
	}

	input, err := os.ReadFile("coredns-template.yaml")
	require.NoError(t, err)
	newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsTarget, -1)
	newContents = strings.Replace(newContents, "name: coredns", fmt.Sprintf("name: %s", dnsConfig.configMapName), 1)
	newContents = strings.Replace(newContents, "namespace: kube-system", fmt.Sprintf("namespace: %s", dnsConfig.namespace), 1)

	file, err := os.CreateTemp("", "consul-dns-custom-*.yaml")
	require.NoError(t, err)

	err = os.WriteFile(file.Name(), []byte(newContents), os.FileMode(0644))
	require.NoError(t, err)

	return file.Name()
}

func updateCoreDNS(t *testing.T, ctx environment.TestContext, dnsConfig clusterDNSConfig, coreDNSConfigFile string) {
	coreDNSCommand := []string{
		"replace", "-n", dnsConfig.namespace, "-f", coreDNSConfigFile,
	}
	var logs string

	timer := &retry.Timer{Timeout: 30 * time.Minute, Wait: 60 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		var err error
		logs, err = k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), coreDNSCommand...)
		require.NoError(r, err)
	})

	require.Contains(t, logs, fmt.Sprintf("configmap/%s replaced", dnsConfig.configMapName))
	restartCoreDNSCommand := []string{"rollout", "restart", fmt.Sprintf("%s/%s", dnsConfig.workloadKind, dnsConfig.workloadName), "-n", dnsConfig.namespace}
	_, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), restartCoreDNSCommand...)
	require.NoError(t, err)

	rolloutTimeout := "1m"
	if dnsConfig.workloadKind == "daemonset" {
		// OpenShift DNS is managed by a daemonset that commonly rolls slowly.
		rolloutTimeout = "10m"
	}
	// Wait for restart to finish.
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "rollout", "status", "--timeout", rolloutTimeout, "--watch", fmt.Sprintf("%s/%s", dnsConfig.workloadKind, dnsConfig.workloadName), "-n", dnsConfig.namespace)
	require.NoError(t, err, out, "rollout status command errored, this likely means the rollout didn't complete in time")
}

func verifyDNS(t *testing.T, releaseName string, svcNamespace string, requestingCtx, svcContext environment.TestContext,
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
	const dnsUtilsImage = "anubhavmishra/tiny-tools"
	var dnsTestPodArgs []string
	if suite.Config().UseOpenshift || suite.Config().EnableOpenshift {
		overrides := fmt.Sprintf(`{"spec":{"securityContext":{"runAsNonRoot":true,"seccompProfile":{"type":"RuntimeDefault"}},"containers":[{"name":"%s","image":"%s","command":["dig","%s"],"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}}]}}`, dnsUtilsPod, dnsUtilsImage, svcName)
		dnsTestPodArgs = []string{
			"run", "-it", dnsUtilsPod, "--restart", "Never", "--image", dnsUtilsImage, "--overrides", overrides,
		}
	} else {
		dnsTestPodArgs = []string{
			"run", "-it", dnsUtilsPod, "--restart", "Never", "--image", dnsUtilsImage, "--", "dig", svcName,
		}
	}

	helpers.Cleanup(t, suite.Config().NoCleanupOnFailure, suite.Config().NoCleanup, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		k8s.RunKubectl(t, requestingCtx.KubectlOptions(t), "delete", "pod", dnsUtilsPod)
	})

	verifyFn := func(r *retry.R) {
		_, _ = k8s.RunKubectlAndGetOutputE(r, requestingCtx.KubectlOptions(r), "delete", "pod", dnsUtilsPod, "--ignore-not-found=true")

		logger.Log(t, "run the dns utilize pod and query DNS for the service.")
		logs, err := k8s.RunKubectlAndGetOutputE(r, requestingCtx.KubectlOptions(r), dnsTestPodArgs...)
		require.NoError(r, err)
		if strings.TrimSpace(logs) == "" {
			logs, err = k8s.RunKubectlAndGetOutputE(r, requestingCtx.KubectlOptions(r), "logs", dnsUtilsPod)
			require.NoError(r, err)
		}

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
	}

	if suite.Config().UseOpenshift || suite.Config().EnableOpenshift {
		// OpenShift DNS/operator updates can converge a bit slower than generic kube-dns.
		timer := &retry.Timer{Timeout: 3 * time.Minute, Wait: 10 * time.Second}
		retry.RunWith(timer, t, verifyFn)
		return
	}

	retry.Run(t, verifyFn)
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
