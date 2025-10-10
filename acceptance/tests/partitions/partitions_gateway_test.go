// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package partitions

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Test that Gateway works in a default and ACLsEnabled installations for X-Partition and in-partition networking.
func TestPartitions_Gateway(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	const defaultPartition = "default"
	const secondaryPartition = "secondary"

	defaultPartitionClusterContext := env.DefaultContext(t)
	secondaryPartitionClusterContext := env.Context(t, 1)

	commonHelmValues := map[string]string{
		"global.adminPartitions.enabled": "true",
		"global.enableConsulNamespaces":  "true",
		"global.logLevel":                "debug",

		"global.tls.enabled":   "true",
		"global.tls.httpsOnly": "true",

		"global.acls.manageSystemACLs": "true",

		"connectInject.enabled": "true",
		// When mirroringK8S is set, this setting is ignored.
		"connectInject.consulNamespaces.consulDestinationNamespace": staticServerNamespace,
		"connectInject.consulNamespaces.mirroringK8S":               "true",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",

		"dns.enabled":           "true",
		"dns.enableRedirection": strconv.FormatBool(cfg.EnableTransparentProxy),
	}

	defaultPartitionHelmValues := make(map[string]string)

	// On Kind, there are no load balancers but since all clusters
	// share the same node network (docker bridge), we can use
	// a NodePort service so that we can access node(s) in a different Kind cluster.
	if cfg.UseKind {
		defaultPartitionHelmValues["meshGateway.service.type"] = "NodePort"
		defaultPartitionHelmValues["meshGateway.service.nodePort"] = "30200" // todo: do we need to set this port?
		defaultPartitionHelmValues["server.exposeService.type"] = "NodePort"
		defaultPartitionHelmValues["server.exposeService.nodePort.https"] = "30000"
		defaultPartitionHelmValues["server.exposeService.nodePort.grpc"] = "30100"
	}

	releaseName := helpers.RandomName()

	helpers.MergeMaps(defaultPartitionHelmValues, commonHelmValues)

	// Install the consul cluster with servers in the default kubernetes context.
	serverConsulCluster := consul.NewHelmCluster(t, defaultPartitionHelmValues, defaultPartitionClusterContext, cfg, releaseName)
	serverConsulCluster.Create(t)

	// Get the TLS CA certificate and key secret from the server cluster and apply it to the client cluster.
	caCertSecretName := fmt.Sprintf("%s-consul-ca-cert", releaseName)

	logger.Logf(t, "retrieving ca cert secret %s from the server cluster and applying to the client cluster", caCertSecretName)
	k8s.CopySecret(t, defaultPartitionClusterContext, secondaryPartitionClusterContext, caCertSecretName)

	partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
	logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", partitionToken)
	k8s.CopySecret(t, defaultPartitionClusterContext, secondaryPartitionClusterContext, partitionToken)

	partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", releaseName)
	partitionSvcAddress := k8s.ServiceHost(t, cfg, defaultPartitionClusterContext, partitionServiceName)

	k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, secondaryPartitionClusterContext)

	// Create client cluster.
	secondaryPartitionHelmValues := map[string]string{
		"global.enabled": "false",

		"global.adminPartitions.name": secondaryPartition,

		"global.tls.caCert.secretName": caCertSecretName,
		"global.tls.caCert.secretKey":  "tls.crt",

		"externalServers.enabled":       "true",
		"externalServers.hosts[0]":      partitionSvcAddress,
		"externalServers.tlsServerName": "server.dc1.consul",
	}

	// Setup partition token and auth method host since ACLs enabled.
	secondaryPartitionHelmValues["global.acls.bootstrapToken.secretName"] = partitionToken
	secondaryPartitionHelmValues["global.acls.bootstrapToken.secretKey"] = "token"
	secondaryPartitionHelmValues["externalServers.k8sAuthMethodHost"] = k8sAuthMethodHost

	if cfg.UseKind {
		secondaryPartitionHelmValues["externalServers.httpsPort"] = "30000"
		secondaryPartitionHelmValues["externalServers.grpcPort"] = "30100"
		secondaryPartitionHelmValues["meshGateway.service.type"] = "NodePort"
		secondaryPartitionHelmValues["meshGateway.service.nodePort"] = "30200"
	}

	helpers.MergeMaps(secondaryPartitionHelmValues, commonHelmValues)

	// Install the consul cluster without servers in the client cluster kubernetes context.
	clientConsulCluster := consul.NewHelmCluster(t, secondaryPartitionHelmValues, secondaryPartitionClusterContext, cfg, releaseName)
	clientConsulCluster.Create(t)

	defaultPartitionClusterStaticServerOpts := &terratestk8s.KubectlOptions{
		ContextName: defaultPartitionClusterContext.KubectlOptions(t).ContextName,
		ConfigPath:  defaultPartitionClusterContext.KubectlOptions(t).ConfigPath,
		Namespace:   staticServerNamespace,
	}
	secondaryPartitionClusterStaticServerOpts := &terratestk8s.KubectlOptions{
		ContextName: secondaryPartitionClusterContext.KubectlOptions(t).ContextName,
		ConfigPath:  secondaryPartitionClusterContext.KubectlOptions(t).ConfigPath,
		Namespace:   staticServerNamespace,
	}
	secondaryPartitionClusterStaticClientOpts := &terratestk8s.KubectlOptions{
		ContextName: secondaryPartitionClusterContext.KubectlOptions(t).ContextName,
		ConfigPath:  secondaryPartitionClusterContext.KubectlOptions(t).ConfigPath,
		Namespace:   StaticClientNamespace,
	}

	logger.Logf(t, "creating namespaces %s and %s in servers cluster", staticServerNamespace, StaticClientNamespace)
	k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
	k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "create", "ns", StaticClientNamespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace, StaticClientNamespace)
	})

	logger.Logf(t, "creating namespaces %s and %s in clients cluster", staticServerNamespace, StaticClientNamespace)
	k8s.RunKubectl(t, secondaryPartitionClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
	k8s.RunKubectl(t, secondaryPartitionClusterContext.KubectlOptions(t), "create", "ns", StaticClientNamespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, secondaryPartitionClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace, StaticClientNamespace)
	})

	consulClient, _ := serverConsulCluster.SetupConsulClient(t, true)

	serverQueryServerOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: defaultPartition}
	clientQueryServerOpts := &api.QueryOptions{Namespace: StaticClientNamespace, Partition: defaultPartition}

	serverQueryClientOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: secondaryPartition}
	clientQueryClientOpts := &api.QueryOptions{Namespace: StaticClientNamespace, Partition: secondaryPartition}

	// We need to register the cleanup function before we create the deployments
	// because golang will execute them in reverse order i.e. the last registered
	// cleanup function will be executed first.
	t.Cleanup(func() {
		retry.Run(t, func(r *retry.R) {
			tokens, _, err := consulClient.ACL().TokenList(serverQueryServerOpts)
			require.NoError(r, err)
			for _, token := range tokens {
				require.NotContains(r, token.Description, staticServerName)
			}

			tokens, _, err = consulClient.ACL().TokenList(clientQueryServerOpts)
			require.NoError(r, err)
			for _, token := range tokens {
				require.NotContains(r, token.Description, StaticClientName)
			}
			tokens, _, err = consulClient.ACL().TokenList(serverQueryClientOpts)
			require.NoError(r, err)
			for _, token := range tokens {
				require.NotContains(r, token.Description, staticServerName)
			}

			tokens, _, err = consulClient.ACL().TokenList(clientQueryClientOpts)
			require.NoError(r, err)
			for _, token := range tokens {
				require.NotContains(r, token.Description, StaticClientName)
			}
		})
	})

	// Create a ProxyDefaults resource to configure services to use the mesh
	// gateways.
	logger.Log(t, "creating proxy-defaults config")
	kustomizeDir := "../fixtures/cases/api-gateways/mesh"

	k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), kustomizeDir)
	})

	k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), kustomizeDir)
	})

	// We use the static-client pod so that we can make calls to the api gateway
	// via kubectl exec without needing a route into the cluster from the test machine.
	// Since we're deploying the gateway in the secondary cluster, we create the static client
	// in the secondary as well.
	logger.Log(t, "creating static-client pod in secondary partition cluster")
	k8s.DeployKustomize(t, secondaryPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Create certificate secret, we do this separately since
	// applying the secret will make an invalid certificate that breaks other tests
	logger.Log(t, "creating certificate secret")
	out, err := k8s.RunKubectlAndGetOutputE(t, secondaryPartitionClusterStaticServerOpts, "apply", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, secondaryPartitionClusterStaticServerOpts, "delete", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	})

	logger.Log(t, "creating api-gateway resources")
	out, err = k8s.RunKubectlAndGetOutputE(t, secondaryPartitionClusterStaticServerOpts, "apply", "-k", "../fixtures/bases/api-gateway")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, secondaryPartitionClusterStaticServerOpts, "delete", "-k", "../fixtures/bases/api-gateway")
	})

	// Grab a kubernetes client so that we can verify binding
	// behavior prior to issuing requests through the gateway.
	k8sClient := secondaryPartitionClusterContext.ControllerRuntimeClient(t)

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence the 1m timeout here).
	var gatewayAddress string
	counter := &retry.Counter{Count: 600, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: staticServerNamespace}, &gateway)
		require.NoError(r, err)

		// check that we have an address to use
		require.Len(r, gateway.Status.Addresses, 1)
		// now we know we have an address, set it so we can use it
		gatewayAddress = gateway.Status.Addresses[0].Value
	})

	targetAddress := fmt.Sprintf("http://%s/", net.JoinHostPort(gatewayAddress, "8080"))

	// This section of the tests runs the in-partition networking tests.
	t.Run("in-partition", func(t *testing.T) {
		logger.Log(t, "test in-partition networking")
		logger.Log(t, "creating target server in secondary partition cluster")
		k8s.DeployKustomize(t, secondaryPartitionClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

		// Check that static-server injected 2 containers.
		for _, labelSelector := range []string{"app=static-server"} {
			podList, err := secondaryPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)
		}

		logger.Log(t, "patching route to target server")
		k8s.RunKubectl(t, secondaryPartitionClusterStaticServerOpts, "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"group":"consul.hashicorp.com","kind":"MeshService","name":"mesh-service","port":80}]}]}}`, "--type=merge")

		logger.Log(t, "checking that the connection is not successful because there's no intention")
		k8s.CheckStaticServerHTTPConnectionFailing(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, targetAddress)

		intention := &api.ServiceIntentionsConfigEntry{
			Kind:      api.ServiceIntentions,
			Name:      staticServerName,
			Namespace: staticServerNamespace,
			Sources: []*api.SourceIntention{
				{
					Name:      "gateway",
					Namespace: staticServerNamespace,
					Action:    api.IntentionActionAllow,
				},
			},
		}

		logger.Log(t, "creating intention")
		_, _, err = consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: secondaryPartition})
		require.NoError(t, err)
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			_, err = consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: secondaryPartition})
			require.NoError(t, err)
		})

		logger.Log(t, "checking that connection is successful")
		k8s.CheckStaticServerConnectionSuccessful(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, targetAddress)
	})

	// This section of the tests runs the cross-partition networking tests.
	t.Run("cross-partition", func(t *testing.T) {
		logger.Log(t, "test cross-partition networking")

		logger.Log(t, "creating target server in default partition cluster")
		k8s.DeployKustomize(t, defaultPartitionClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

		// Check that static-server injected 2 containers.
		for _, labelSelector := range []string{"app=static-server"} {
			podList, err := defaultPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)
		}

		logger.Log(t, "creating exported services")
		k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-ns1")
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-ns1")
		})

		logger.Log(t, "creating local service resolver")
		k8s.KubectlApplyK(t, secondaryPartitionClusterStaticServerOpts, "../fixtures/cases/api-gateways/resolver")
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			k8s.KubectlDeleteK(t, secondaryPartitionClusterStaticServerOpts, "../fixtures/cases/api-gateways/resolver")
		})

		logger.Log(t, "patching route to target server")
		k8s.RunKubectl(t, secondaryPartitionClusterStaticServerOpts, "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"group":"consul.hashicorp.com","kind":"MeshService","name":"mesh-service","port":80}]}]}}`, "--type=merge")

		logger.Log(t, "checking that the connection is not successful because there's no intention")
		k8s.CheckStaticServerHTTPConnectionFailing(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, targetAddress)

		intention := &api.ServiceIntentionsConfigEntry{
			Kind:      api.ServiceIntentions,
			Name:      staticServerName,
			Namespace: staticServerNamespace,
			Sources: []*api.SourceIntention{
				{
					Name:      "gateway",
					Namespace: staticServerNamespace,
					Action:    api.IntentionActionAllow,
					Partition: secondaryPartition,
				},
			},
		}

		logger.Log(t, "creating intention")
		_, _, err = consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: defaultPartition})
		require.NoError(t, err)
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			_, err = consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: defaultPartition})
			require.NoError(t, err)
		})

		logger.Log(t, "checking that connection is successful")
		k8s.CheckStaticServerConnectionSuccessful(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, targetAddress)
	})
}
