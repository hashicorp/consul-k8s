// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
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
		secure         bool
		enableDNSProxy bool
	}{
		{secure: false, enableDNSProxy: false},
		{secure: false, enableDNSProxy: true},
		{secure: true, enableDNSProxy: false},
		{secure: true, enableDNSProxy: true},
	}

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t / enableDNSProxy: %t", c.secure, c.enableDNSProxy)
		t.Run(name, func(t *testing.T) {
			env := suite.Environment()
			ctx := env.DefaultContext(t)
			releaseName := helpers.RandomName()
			helmValues := map[string]string{
				"dns.enabled":                  "true",
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"dns.proxy.enabled":            strconv.FormatBool(c.enableDNSProxy),
			}

			cluster := consul.NewHelmCluster(t, helmValues, ctx, suite.Config(), releaseName)
			cluster.Create(t)

			updateCoreDNSWithConsulDomain(t, ctx, releaseName, c.enableDNSProxy)
			verifyDNS(t, releaseName, ctx.KubectlOptions(t).Namespace, ctx, ctx, "app=consul,component=server",
				"consul.service.consul", true, 0)
		})
	}
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

	input, err := os.ReadFile("coredns-template.yaml")
	require.NoError(t, err)
	newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsIP, -1)
	err = os.WriteFile(dnsFileName, []byte(newContents), os.FileMode(0644))
	require.NoError(t, err)
}

func updateCoreDNS(t *testing.T, ctx environment.TestContext, coreDNSConfigFile string) {
	coreDNSCommand := []string{
		"replace", "-n", "kube-system", "-f", coreDNSConfigFile,
	}
	logs, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), coreDNSCommand...)
	require.NoError(t, err)
	require.Contains(t, logs, "configmap/coredns replaced")
	restartCoreDNSCommand := []string{"rollout", "restart", "deployment/coredns", "-n", "kube-system"}
	_, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), restartCoreDNSCommand...)
	require.NoError(t, err)
	// Wait for restart to finish.
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "rollout", "status", "--timeout", "1m", "--watch", "deployment/coredns", "-n", "kube-system")
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
