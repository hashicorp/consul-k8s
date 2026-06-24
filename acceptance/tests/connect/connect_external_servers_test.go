// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConnectInject_ExternalServers tests that connect works when using external servers.
// It sets up an external Consul server in the same cluster but a different Helm installation
// and then treats this server as external.
func TestConnectInject_ExternalServers(t *testing.T) {
	for _, secure := range []bool{
		false,
		true,
	} {
		caseName := fmt.Sprintf("secure: %t", secure)
		t.Run(caseName, func(t *testing.T) {
			cfg := suite.Config()
			cfg.SkipWhenOpenshiftAndCNI(t)

			ctx := suite.Environment().DefaultContext(t)

			serverHelmValues := map[string]string{
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
				"global.tls.enabled":           strconv.FormatBool(secure),

				// Don't install injector, controller and cni on this cluster so that it's not installed twice.
				"connectInject.enabled":     "false",
				"connectInject.cni.enabled": "false",
			}

			if cfg.EnableOpenshift {
				// On ROSA/OCP with OVN-Kubernetes, gRPC connections to the headless consul-server service
				// (which returns pod IPs directly via DNS) are disrupted by the OVN-K8s datapath for ALL
				// long-lived streams — including the xDS stream that consul-dataplane needs to configure
				// envoy. Repeated disruptions prevent consul-dataplane from completing a full xDS sync.
				// A ClusterIP expose-servers service provides a stable virtual IP routed through OVN's
				// load-balancer NAT rules, which are not subject to the same Geneve-tunnel ct timeouts.
				serverHelmValues["server.exposeService.enabled"] = "true"
				serverHelmValues["server.exposeService.type"] = "ClusterIP"
			}

			serverReleaseName := helpers.RandomName()
			consulServerCluster := consul.NewHelmCluster(t, serverHelmValues, ctx, cfg, serverReleaseName)

			consulServerCluster.Create(t)

			helmValues := map[string]string{
				"server.enabled":               "false",
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),

				"global.tls.enabled": strconv.FormatBool(secure),

				"connectInject.enabled": "true",

				"externalServers.enabled":   "true",
				"externalServers.hosts[0]":  fmt.Sprintf("%s-consul-server", serverReleaseName),
				"externalServers.httpsPort": "8500",
			}

			if secure {
				helmValues["global.tls.caCert.secretName"] = fmt.Sprintf("%s-consul-ca-cert", serverReleaseName)
				helmValues["global.tls.caCert.secretKey"] = "tls.crt"
				helmValues["global.acls.bootstrapToken.secretName"] = fmt.Sprintf("%s-consul-bootstrap-acl-token", serverReleaseName)
				helmValues["global.acls.bootstrapToken.secretKey"] = "token"
				helmValues["externalServers.httpsPort"] = "8501"
			}

			if cfg.EnableOpenshift {
				// On ROSA/OCP (OVN-Kubernetes), several issues prevent reliable external-server connect:
				//
				// 1. ALL long-lived gRPC streams to headless-service pod IPs are disrupted by OVN-K8s
				//    Geneve-tunnel ct timeouts — including the xDS stream. consul-dataplane retries but
				//    cannot complete a full xDS sync before the next disruption.
				//    Fix: use the ClusterIP expose-servers service (stable OVN load-balancer NAT path).
				//
				// 2. The TLS cert SANs are generated for the headless service name (e.g.
				//    <rel>-consul-server.*) and do NOT include the expose-servers name.
				//    Fix: set tlsServerName to the headless service DNS name for TLS SNI matching.
				//
				// 3. skipServerWatch avoids opening the idle server-watch stream to pod IPs.
				//
				// 4. On OCP without CNI, transparent proxy is disabled to prevent tproxy iptables from
				//    intercepting the static-server's httpGet startup/liveness probes and routing them
				//    through envoy before xDS is received. Disabling tproxy globally means probes go
				//    directly to the app port and the explicit upstream annotation (localhost:1234) is
				//    used instead — which does not require iptables and works independently of tproxy.
				helmValues["externalServers.hosts[0]"] = fmt.Sprintf("%s-consul-expose-servers", serverReleaseName)
				helmValues["externalServers.tlsServerName"] = fmt.Sprintf("%s-consul-server", serverReleaseName)
				helmValues["externalServers.skipServerWatch"] = "true"
				helmValues["connectInject.transparentProxy.defaultEnabled"] = "false"
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.SkipCheckForPreviousInstallations = true

			consulCluster.Create(t)

			logger.Log(t, "creating static-server and static-client deployments")
			// Use appropriate fixtures based on OpenShift and CNI flags
			staticServerFixture := "../fixtures/cases/static-server-inject"
			staticClientFixture := "../fixtures/cases/static-client-inject"
			if cfg.EnableOpenshift {
				// OpenShift-specific fixtures
				if cfg.EnableCNI {
					// OpenShift WITH CNI
					staticServerFixture = "../fixtures/cases/static-server-openshift-cni"
					if cfg.EnableTransparentProxy {
						staticClientFixture = "../fixtures/cases/static-client-openshift-tproxy-cni"
					} else {
						staticClientFixture = "../fixtures/cases/static-client-openshift-inject-cni"
					}
				} else {
					// OpenShift WITHOUT CNI. Transparent proxy is disabled globally (see
					// connectInject.transparentProxy.defaultEnabled=false above), so kubelet
					// probes go directly to the app port without iptables interception.
					// static-server-openshift provides an exec readiness probe for the health-check
					// sync test (touch /tmp/unhealthy). static-client-openshift-inject explicitly
					// marks tproxy disabled and uses the localhost:1234 explicit upstream.
					staticServerFixture = "../fixtures/cases/static-server-openshift"
					staticClientFixture = "../fixtures/cases/static-client-openshift-inject"
				}
			} else if cfg.EnableTransparentProxy {
				staticClientFixture = "../fixtures/cases/static-client-tproxy"
			}

			// On OCP, tproxy is not enabled (to avoid kubelet probe interception deadlock with envoy).
			// curl checks use localhost:1234 via the explicit upstream annotation on the client.
			// WaitForPodsRunningPhase is sufficient — the curl retry loop in the test provides
			// additional time for consul-dataplane to receive xDS and proxy traffic.
			useTransparentProxy := cfg.EnableTransparentProxy
			if cfg.EnableOpenshift {
				namespace := ctx.KubectlOptions(t).Namespace
				k8s.DeployKustomizeNoWait(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, staticServerFixture)
				k8s.DeployKustomizeNoWait(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, staticClientFixture)
				// Wait until init containers are done and application containers have started.
				// consul-dataplane's readiness probe (TCPSocket port 20000) may still be pending
				// until envoy receives xDS, but the connection check retry loop below covers that.
				k8s.WaitForPodsRunningPhase(t, ctx.KubernetesClient(t), namespace, "app=static-server")
				k8s.WaitForPodsRunningPhase(t, ctx.KubernetesClient(t), namespace, "app=static-client")
			} else {
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, staticServerFixture)
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, staticClientFixture)
			}

			// Check that both static-server and static-client have been injected and now have 2 containers.
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				podList, err := ctx.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
			}

			if secure {
				consulClient, _ := consulServerCluster.SetupConsulClient(t, true)

				logger.Log(t, "checking that the connection is not successful because there's no intention")
				if useTransparentProxy {
					k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://static-server")
				} else {
					k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:1234")
				}

				intention := &api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: connhelper.StaticServerName,
					Sources: []*api.SourceIntention{
						{
							Name:   connhelper.StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}

				logger.Log(t, "creating intention")
				_, _, err := consulClient.ConfigEntries().Set(intention, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful")
			if useTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://static-server")
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:1234")
			}

			// Test that kubernetes readiness status is synced to Consul.
			// Create the file so that the readiness probe of the static-server pod fails.
			logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+connhelper.StaticServerName, "-c", "static-server", "--", "touch", "/tmp/unhealthy")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			logger.Log(t, "checking that connection is unsuccessful")
			if useTransparentProxy {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server port 80"}, "", "http://static-server")
			} else {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
			}
		})
	}
}
