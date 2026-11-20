// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package segments

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// TestSegments_MeshWithAgentfulClients is a simple test that verifies that
// the Consul service mesh can be configured to use segments with:
// - one cluster with an alpha segment configured on the servers.
// - clients enabled and joining the alpha segment.
// - static client can communicate with static server.
func TestSegments_MeshWithAgentfulClients(t *testing.T) {
	cases := map[string]struct {
		secure bool
	}{
		"not-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			if !cfg.EnableEnterprise {
				t.Skipf("skipping this test because -enable-enterprise is not set")
			}
			ctx := suite.Environment().DefaultContext(t)

			releaseName := helpers.RandomName()
			addr := "0.0.0.0"
			helmValues := map[string]string{
				"connectInject.enabled": "true",

				"server.replicas":    "3",
				"server.extraConfig": fmt.Sprintf(`"{\"segments\": [{\"name\":\"alpha1\"\,\"bind\":\"%s\"\,\"port\":8303}]}"`, addr),

				"client.enabled": "true",
				// need to configure clients to connect to port 8303 that the alpha segment was configured on rather than
				// the standard serf LAN port.
				"client.join[0]":     "${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[1]":     "${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[2]":     "${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.extraConfig": `"{\"segment\": \"alpha1\"}"`,
			}

			connHelper := connhelper.ConnectHelper{
				ClusterKind:     consul.Helm,
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             ctx,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
				HelmValues:      helmValues,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			if c.secure {
				connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}

// TestSegments_MeshWithAgentfulClientsMultiCluster verifies that client agents in one Kubernetes cluster
// can join a network segment served by Consul servers in another Kubernetes cluster:
// - the server cluster runs a server with an "alpha1" segment and exposes their RPC, gossip
// and segment ports on the node IPs, plus their HTTP(S) and gRPC APIs on a NodePort service;
// - the client cluster runs client agents only, joins the servers' node IPs on the segment port
// and reaches the servers' APIs through the NodePort service;
// - the client cluster's agents show up as alive members of the "alpha1" segment.
//
// The two clusters' node networks must be routable to each other (as on Kind, or peered VPCs).
func TestSegments_MeshWithAgentfulClientsMultiCluster(t *testing.T) {
	if cfg := suite.Config(); (cfg.EnableOpenshift || cfg.UseOpenshift) && (!cfg.EnableMultiCluster || !cfg.IsExpectedClusterCount(2)) {
		t.Skip("requires at least two clusters and -enable-multi-cluster")
	}

	const segmentName = "alpha1"
	const segmentPort = 8303
	// serf.StatusAlive: the member status Consul reports for a live agent.
	const memberStatusAlive = 1

	cases := map[string]struct {
		secure bool
	}{
		"not-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			if !cfg.EnableEnterprise {
				t.Skipf("skipping this test because -enable-enterprise is not set")
			}
			releaseName := helpers.RandomName()

			// One server: with host ports, servers would gossip with each other through node IPs, which
			// the cloud firewall between nodes may not allow, and a single Kind node can only hold the host
			// ports once.
			serverReplicas := 1

			// deploy server cluster
			serverClusterContext := suite.Environment().DefaultContext(t)
			serverClusterHelmValues := map[string]string{
				"connectInject.enabled": "true",

				"global.tls.enableAutoEncrypt": "true",

				"server.replicas":    fmt.Sprintf("%d", serverReplicas),
				"server.extraConfig": fmt.Sprintf(`"{\"segments\": [{\"name\":\"%s\"\,\"bind\":\"0.0.0.0\"\,\"port\":%d}]}"`, segmentName, segmentPort),
				// Servers advertise their node IP, and their RPC and gossip ports are host ports,
				// so agents in the other cluster can reach them.
				"server.exposeGossipAndRPCPorts": "true",
				// The servers' HTTP(S) and gRPC APIs for the other cluster's injector and ACL setup.
				"server.exposeService.enabled": "true",
				"server.exposeService.type":    "NodePort",

				// No client agents here: they take host ports 8500/8502 on every node, which clash with the
				// server's host ports. In-cluster segment clients are covered by TestSegments_MeshWithAgentfulClients.
				"client.enabled": "false",
			}

			serverConnHelper := connhelper.ConnectHelper{
				ClusterKind:     consul.Helm,
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             serverClusterContext,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
				HelmValues:      serverClusterHelmValues,
			}

			serverConnHelper.Setup(t)
			serverConnHelper.Install(t)
			serverConnHelper.DeployServer(t)

			serverOpts := serverClusterContext.KubectlOptions(t)
			serverK8s := serverClusterContext.KubernetesClient(t)

			// The chart has no setting to publish a segment port as a host port, so add it to the
			// server pods here. Segment members gossip with the servers' advertised (node) address.
			statefulSet := fmt.Sprintf("%s-consul-server", releaseName)
			patch := fmt.Sprintf(`[{"op":"add","path":"/spec/template/spec/containers/0/ports/-","value":{"name":"segment-tcp","containerPort":%[1]d,"hostPort":%[1]d,"protocol":"TCP"}},`+
				`{"op":"add","path":"/spec/template/spec/containers/0/ports/-","value":{"name":"segment-udp","containerPort":%[1]d,"hostPort":%[1]d,"protocol":"UDP"}}]`, segmentPort)
			k8s.RunKubectl(t, serverOpts, "patch", "statefulset", statefulSet, "--type=json", "-p", patch)
			k8s.RunKubectl(t, serverOpts, "rollout", "status", "statefulset/"+statefulSet, "--timeout=15m")

			// The node IPs of the server pods, for the client agents to join on the segment port.
			serverPods, err := serverK8s.CoreV1().Pods(serverOpts.Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=consul,component=server,release=%s", releaseName),
			})
			require.NoError(t, err)
			require.Len(t, serverPods.Items, serverReplicas)

			// The NodePorts of the servers' APIs.
			exposeSvc, err := serverK8s.CoreV1().Services(serverOpts.Namespace).Get(context.Background(), releaseName+"-consul-expose-servers", metav1.GetOptions{})
			require.NoError(t, err)
			nodePorts := map[string]int32{}
			for _, port := range exposeSvc.Spec.Ports {
				nodePorts[port.Name] = port.NodePort
			}
			apiPortName := "http"
			if c.secure {
				apiPortName = "https"
			}
			require.NotZero(t, nodePorts[apiPortName], "expose-servers service has no %s NodePort", apiPortName)
			require.NotZero(t, nodePorts["grpc"], "expose-servers service has no grpc NodePort")

			// deploy client cluster
			clientClusterContext := suite.Environment().Context(t, 1)
			clientClusterHelmValues := map[string]string{
				"global.enabled":        "false",
				"connectInject.enabled": "true",
				"dns.enabled":           "true",
				"dns.enableRedirection": "true",

				"global.tls.enabled":           fmt.Sprintf("%t", c.secure),
				"global.tls.enableAutoEncrypt": "true",
				"global.acls.manageSystemACLs": fmt.Sprintf("%t", c.secure),

				"externalServers.enabled":   "true",
				"externalServers.hosts[0]":  serverPods.Items[0].Status.HostIP,
				"externalServers.httpsPort": fmt.Sprintf("%d", nodePorts[apiPortName]),
				"externalServers.grpcPort":  fmt.Sprintf("%d", nodePorts["grpc"]),

				"client.enabled": "true",
				// Client agents advertise their node IP and expose their gossip port, so the servers can reach them.
				"client.exposeGossipPorts": "true",
				"client.extraConfig":       fmt.Sprintf(`"{\"segment\": \"%s\"}"`, segmentName),
			}
			for i, pod := range serverPods.Items {
				clientClusterHelmValues[fmt.Sprintf("client.join[%d]", i)] = fmt.Sprintf("%s:%d", pod.Status.HostIP, segmentPort)
			}

			if c.secure {
				// The client cluster trusts the servers' CA and uses the bootstrap token to create its own
				// ACL tokens. With auto-encrypt the CA private key stays in the server cluster.
				caCertSecretName := releaseName + "-consul-ca-cert"
				bootstrapTokenSecretName := releaseName + "-consul-bootstrap-acl-token"
				for _, secretName := range []string{caCertSecretName, bootstrapTokenSecretName} {
					logger.Logf(t, "copying secret %s from the server cluster to the client cluster", secretName)
					k8s.CopySecret(t, serverClusterContext, clientClusterContext, secretName)
					secretName := secretName
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
						_ = clientClusterContext.KubernetesClient(t).CoreV1().Secrets(clientClusterContext.KubectlOptions(t).Namespace).
							Delete(context.Background(), secretName, metav1.DeleteOptions{})
					})
				}
				clientClusterHelmValues["global.tls.caCert.secretName"] = caCertSecretName
				clientClusterHelmValues["global.tls.caCert.secretKey"] = "tls.crt"
				clientClusterHelmValues["global.acls.bootstrapToken.secretName"] = bootstrapTokenSecretName
				clientClusterHelmValues["global.acls.bootstrapToken.secretKey"] = "token"
				clientClusterHelmValues["externalServers.tlsServerName"] = "server.dc1.consul"
				clientClusterHelmValues["externalServers.k8sAuthMethodHost"] = k8s.KubernetesAPIServerHost(t, cfg, clientClusterContext)
			}

			// The client cluster has no servers, so install it directly rather than through a ConnectHelper,
			// which would try to reach a local server.
			clientCluster := consul.NewHelmCluster(t, clientClusterHelmValues, clientClusterContext, cfg, releaseName)
			clientCluster.Create(t)

			// The client cluster's agents must be alive members of the segment, as seen by the servers.
			clientNodes := map[string]bool{}
			clientPods, err := clientClusterContext.KubernetesClient(t).CoreV1().Pods(clientClusterContext.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=consul,component=client,release=%s", releaseName),
			})
			require.NoError(t, err)
			require.NotEmpty(t, clientPods.Items)
			for _, pod := range clientPods.Items {
				clientNodes[pod.Spec.NodeName] = true
			}
			retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
				members, err := serverConnHelper.ConsulClient.Agent().MembersOpts(api.MembersOpts{Segment: segmentName})
				require.NoError(r, err)
				alive := map[string]bool{}
				for _, m := range members {
					if m.Status == memberStatusAlive {
						alive[m.Name] = true
					}
				}
				for node := range clientNodes {
					require.Truef(r, alive[node], "client agent on %s is not an alive member of segment %s", node, segmentName)
				}
			})
			logger.Logf(t, "all %d client agents from the client cluster are alive members of segment %s", len(clientNodes), segmentName)

			logger.Log(t, "creating static-client deployments in client cluster")
			opts := clientClusterContext.KubectlOptions(t)
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, opts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, opts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
			}

			// Check that the static-client has been injected and now have 2 containers in client cluster.
			for _, labelSelector := range []string{"app=static-client"} {
				podList, err := clientClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
			}
		})
	}
}
