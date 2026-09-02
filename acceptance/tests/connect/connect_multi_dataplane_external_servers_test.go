// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConnectInject_MultiDataplane_ExternalServers tests that connect works when using multiple dataplanes
// authenticating to the same control plane via custom k8s auth methods.
func TestConnectInject_MultiDataplane_ExternalServers(t *testing.T) {
	for _, secure := range []bool{
		false,
		true,
	} {
		caseName := fmt.Sprintf("secure: %t", secure)
		t.Run(caseName, func(t *testing.T) {
			cfg := suite.Config()
			cfg.SkipWhenOpenshiftAndCNI(t)

			// ctx1 represents the Control Plane and Dataplane 1
			ctx1 := suite.Environment().DefaultContext(t)
			// ctx2 represents Dataplane 2
			ctx2 := suite.Environment().Context(t, 1)

			// Setup Control Plane in ctx1
			serverHelmValues := map[string]string{
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
				"global.tls.enabled":           strconv.FormatBool(secure),
				"global.datacenter":            "dc1",
				"server.exposeService.enabled": "true",

				// Install injector in ctx1 as Dataplane 1
				"connectInject.enabled": "true",
			}

			if cfg.UseKind {
				serverHelmValues["server.exposeService.type"] = "NodePort"
				serverHelmValues["server.exposeService.nodePort.http"] = "32500"
				serverHelmValues["server.exposeService.nodePort.https"] = "32501"
				serverHelmValues["server.exposeService.nodePort.grpc"] = "32502"

				// Add the node IP to the server's TLS certificate SANs so that Dataplane 2
				// can verify the certificate when connecting via the NodePort.
				nodeList, err := ctx1.KubernetesClient(t).CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				require.NoError(t, err)
				if len(nodeList.Items) > 0 && len(nodeList.Items[0].Status.Addresses) > 0 {
					serverHelmValues["global.tls.serverAdditionalIPSANs[0]"] = nodeList.Items[0].Status.Addresses[0].Address
				}
			}
			serverReleaseName := helpers.RandomName()
			consulServerCluster := consul.NewHelmCluster(t, serverHelmValues, ctx1, cfg, serverReleaseName)
			consulServerCluster.Create(t)

			// We need to fetch the IP where the server NodePort can be reached from ctx2.
			// Using the standard k8s.ServiceHost helper to find the IP.
			serverSvcAddress := k8s.ServiceHost(t, cfg, ctx1, fmt.Sprintf("%s-consul-expose-servers", serverReleaseName))

			logger.Log(t, "Setting up externalServers config for Dataplane 2 (ctx2)")

			dp2HelmValues := map[string]string{
				"global.datacenter": "dc1", // Must match control plane
				"server.enabled":    "false",

				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
				"global.tls.enabled":           strconv.FormatBool(secure),

				"connectInject.enabled": "true",

				"externalServers.enabled":  "true",
				"externalServers.hosts[0]": serverSvcAddress,
			}

			if cfg.UseKind {

				// Set k8sAuthMethodHost to the ctx2 API server IP so the Consul server in ctx1 can verify tokens
				dp2NodeList, err := ctx2.KubernetesClient(t).CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				require.NoError(t, err)
				if len(dp2NodeList.Items) > 0 && len(dp2NodeList.Items[0].Status.Addresses) > 0 {
					dp2HelmValues["externalServers.k8sAuthMethodHost"] = fmt.Sprintf("https://%s:6443", dp2NodeList.Items[0].Status.Addresses[0].Address)
				}
				dp2HelmValues["externalServers.httpsPort"] = "32500"
				dp2HelmValues["externalServers.grpcPort"] = "32502"
			} else {
				// We need k8sAuthMethodHost so that the control plane auth method can reach ctx2 API server
				// Using standard kubernetes endpoint in a kind multi-cluster setup this typically requires NodePort
				// For this specific test, we can just point it to the local cluster API since we are just proving
				// the authMethod logic is respected by the components.
				dp2HelmValues["externalServers.k8sAuthMethodHost"] = "https://kubernetes.default.svc"
				dp2HelmValues["externalServers.httpsPort"] = "8500"
				dp2HelmValues["externalServers.grpcPort"] = "8502"
			}

			if secure {
				if cfg.UseKind {
					dp2HelmValues["externalServers.httpsPort"] = "32501"
				} else {
					dp2HelmValues["externalServers.httpsPort"] = "8501"
				}
				dp2HelmValues["global.tls.caCert.secretName"] = fmt.Sprintf("%s-consul-ca-cert", serverReleaseName)
				dp2HelmValues["global.tls.caCert.secretKey"] = "tls.crt"
				dp2HelmValues["global.acls.bootstrapToken.secretName"] = fmt.Sprintf("%s-consul-bootstrap-acl-token", serverReleaseName)
				dp2HelmValues["global.acls.bootstrapToken.secretKey"] = "token"

				// Use the new multi-dataplane authentication values only when ACLs are enabled
				dp2HelmValues["global.acls.authMethodName"] = "auth-method-dc2"
				dp2HelmValues["connectInject.overrideAuthMethodName"] = "auth-method-dc2"

				// Copy Secrets from ctx1 to ctx2
				logger.Log(t, "Copying CA Cert and Bootstrap token to ctx2")
				caSecret, err := ctx1.KubernetesClient(t).CoreV1().Secrets(ctx1.KubectlOptions(t).Namespace).Get(context.Background(), fmt.Sprintf("%s-consul-ca-cert", serverReleaseName), metav1.GetOptions{})
				require.NoError(t, err)

				bootstrapSecret, err := ctx1.KubernetesClient(t).CoreV1().Secrets(ctx1.KubectlOptions(t).Namespace).Get(context.Background(), fmt.Sprintf("%s-consul-bootstrap-acl-token", serverReleaseName), metav1.GetOptions{})
				require.NoError(t, err)

				caSecret.ResourceVersion = ""
				caSecret.UID = ""
				caSecret.Namespace = ctx2.KubectlOptions(t).Namespace

				bootstrapSecret.ResourceVersion = ""
				bootstrapSecret.UID = ""
				bootstrapSecret.Namespace = ctx2.KubectlOptions(t).Namespace

				_, err = ctx2.KubernetesClient(t).CoreV1().Secrets(ctx2.KubectlOptions(t).Namespace).Create(context.Background(), caSecret, metav1.CreateOptions{})
				require.NoError(t, err)

				_, err = ctx2.KubernetesClient(t).CoreV1().Secrets(ctx2.KubectlOptions(t).Namespace).Create(context.Background(), bootstrapSecret, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			releaseName2 := helpers.RandomName()
			consulClusterDP2 := consul.NewHelmCluster(t, dp2HelmValues, ctx2, cfg, releaseName2)
			consulClusterDP2.SkipCheckForPreviousInstallations = true

			consulClusterDP2.Create(t)

			logger.Log(t, "deploying static-server and static-client in ctx1 (Dataplane 1)")
			_, _ = k8s.RunKubectlAndGetOutputE(t, ctx1.KubectlOptions(t), "delete", "deploy", "static-server", "static-client", "--ignore-not-found")
			k8s.DeployKustomize(t, ctx1.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, ctx1.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, ctx1.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
			}

			logger.Log(t, "deploying static-server and static-client in ctx2 (Dataplane 2)")
			_, _ = k8s.RunKubectlAndGetOutputE(t, ctx2.KubectlOptions(t), "delete", "deploy", "dp2-static-server", "dp2-static-client", "--ignore-not-found")
			k8s.DeployKustomize(t, ctx2.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/dp2-static-server-inject")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, ctx2.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/dp2-static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, ctx2.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/dp2-static-client-inject")
			}

			// Check that both static-server and static-client have been injected in both clusters.
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				require.Eventually(t, func() bool {
					podList, err := ctx1.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
						LabelSelector: labelSelector,
					})
					if err != nil || len(podList.Items) == 0 {
						return false
					}
					return len(podList.Items[0].Spec.Containers) == 2
				}, 5*time.Minute, 5*time.Second)
			}
			for _, labelSelector := range []string{"app=dp2-static-server", "app=dp2-static-client"} {
				require.Eventually(t, func() bool {
					podList, err := ctx2.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
						LabelSelector: labelSelector,
					})
					if err != nil || len(podList.Items) == 0 {
						return false
					}
					return len(podList.Items[0].Spec.Containers) == 2
				}, 5*time.Minute, 5*time.Second)
			}

			consulClient, _ := consulServerCluster.SetupConsulClient(t, secure)

			logger.Log(t, "creating ServiceDefaults config entry in control plane")
			serviceDefaults := &api.ServiceConfigEntry{
				Kind:     api.ServiceDefaults,
				Name:     connhelper.StaticServerName,
				Protocol: "http",
			}
			_, _, err := consulClient.ConfigEntries().Set(serviceDefaults, nil)
			require.NoError(t, err)

			serviceDefaultsDP2 := &api.ServiceConfigEntry{
				Kind:     api.ServiceDefaults,
				Name:     "dp2-" + connhelper.StaticServerName,
				Protocol: "http",
			}
			_, _, err = consulClient.ConfigEntries().Set(serviceDefaultsDP2, nil)
			require.NoError(t, err)

			if secure {
				logger.Log(t, "creating intention in control plane to allow traffic")
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
				_, _, err = consulClient.ConfigEntries().Set(intention, nil)
				require.NoError(t, err)

				intentionDP2 := &api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: "dp2-" + connhelper.StaticServerName,
					Sources: []*api.SourceIntention{
						{
							Name:   "dp2-" + connhelper.StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}
				_, _, err = consulClient.ConfigEntries().Set(intentionDP2, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful in ctx1 (Dataplane 1)")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx1.KubectlOptions(t), connhelper.StaticClientName, "http://static-server")
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx1.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:1234")
			}

			logger.Log(t, "checking that connection is successful in ctx2 (Dataplane 2)")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx2.KubectlOptions(t), "dp2-static-client", "http://dp2-static-server")
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx2.KubectlOptions(t), "dp2-static-client", "http://localhost:1234")
			}
		})
	}
}
