// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConsulDNS configures CoreDNS to use configure consul domain queries to
// be forwarded to the Consul DNS Service or the Consul DNS Proxy depending on
// the test case.  The test validates that the DNS queries are resolved when querying
// for .consul services in secure and non-secure modes.
func TestConsulDNS_Privileged(t *testing.T) {
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

			updateCoreDNSWithConsulDomain_Privileged(t, ctx, releaseName, c.enableDNSProxy)
			verifyDNS(t, cfg, releaseName, ctx.KubectlOptions(t).Namespace, ctx, ctx, "app=consul,component=server",
				"consul.service.consul", true, 0)
		})
	}
}

func updateCoreDNSWithConsulDomain_Privileged(t *testing.T, ctx environment.TestContext, releaseName string, enableDNSProxy bool) {
	updateCoreDNSFile_Privileged(t, ctx, releaseName, enableDNSProxy, "coredns-custom.yaml")
	updateCoreDNS(t, ctx, "coredns-custom.yaml")

	t.Cleanup(func() {
		updateCoreDNS(t, ctx, "coredns-original.yaml")
		time.Sleep(5 * time.Second)
	})
}

func updateCoreDNSFile_Privileged(t *testing.T, ctx environment.TestContext, releaseName string,
	enableDNSProxy bool, dnsFileName string) {
	dnsIP, err := getDNSServiceClusterIP(t, ctx, releaseName, enableDNSProxy)
	require.NoError(t, err)

	// For privileged test, we use the default port 53 (not 8053 like in non-privileged)
	dnsTarget := dnsIP
	if enableDNSProxy {
		dnsTarget = net.JoinHostPort(dnsIP, "53")
	}

	input, err := os.ReadFile("coredns-template.yaml")
	require.NoError(t, err)
	newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsTarget, -1)
	err = os.WriteFile(dnsFileName, []byte(newContents), os.FileMode(0644))
	require.NoError(t, err)
}
