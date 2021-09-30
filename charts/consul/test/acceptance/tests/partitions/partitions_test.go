package partitions

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that Connect works in a default installation.
// i.e. without ACLs because TLS is required for setting up Admin Partitions.
func TestPartitions(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	if !cfg.UseKind {
		t.Skipf("skipping this test because Admin Partition tests are only supported in Kind for now")
	}

	primaryContext := env.DefaultContext(t)
	secondaryContext := env.Context(t, environment.SecondaryContextName)

	ctx := context.Background()

	primaryHelmValues := map[string]string{
		"global.datacenter": "dc1",
		"global.image":      "hashicorp/consul-enterprise:1.11.0-ent-alpha",

		"global.adminPartitions.enabled": "true",
		"global.enableConsulNamespaces":  "true",
		"global.tls.enabled":             "true",

		"server.exposeGossipAndRPCPorts": "true",

		"connectInject.enabled": "true",
	}

	if cfg.UseKind {
		primaryHelmValues["global.adminPartitions.service.type"] = "NodePort"
		primaryHelmValues["global.adminPartitions.service.nodePort.https"] = "30000"
	}

	releaseName := helpers.RandomName()

	// Install the consul cluster with servers in the default kubernetes context.
	primaryConsulCluster := consul.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
	primaryConsulCluster.Create(t)

	// Get the TLS CA certificate and key secret from the primary cluster and apply it to secondary cluster
	tlsCert := fmt.Sprintf("%s-consul-ca-cert", releaseName)
	logger.Logf(t, "retrieving ca cert secret %s from the primary cluster and applying to the secondary", tlsCert)
	caCertSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(ctx, tlsCert, metav1.GetOptions{})
	caCertSecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(ctx, caCertSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	tlsKey := fmt.Sprintf("%s-consul-ca-key", releaseName)
	logger.Logf(t, "retrieving ca key secret %s from the primary cluster and applying to the secondary", tlsKey)
	caKeySecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(ctx, tlsKey, metav1.GetOptions{})
	caKeySecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(ctx, caKeySecret, metav1.CreateOptions{})
	require.NoError(t, err)

	var partitionSvcIP string
	if !cfg.UseKind {
		// Get the IP of the partition service to configure the external server address in the values file for the workload cluster.
		partitionServiceName := fmt.Sprintf("%s-partition-secret", releaseName)
		logger.Logf(t, "retrieving partition service to determine external IP for servers")
		partitionsSvc, err := primaryContext.KubernetesClient(t).CoreV1().Services(primaryContext.KubectlOptions(t).Namespace).Get(ctx, partitionServiceName, metav1.GetOptions{})
		require.NoError(t, err)
		partitionSvcIP = partitionsSvc.Status.LoadBalancer.Ingress[0].IP
	} else {
		nodeList, err := primaryContext.KubernetesClient(t).CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		require.NoError(t, err)
		// Get the address of the (only) node from the Kind cluster.
		partitionSvcIP = nodeList.Items[0].Status.Addresses[0].Address
	}

	// Create secondary cluster
	secondaryHelmValues := map[string]string{
		"global.datacenter": "dc1",
		"global.image":      "hashicorp/consul-enterprise:1.11.0-ent-alpha",
		"global.enabled":    "false",

		"global.adminPartitions.enabled": "true",
		"global.adminPartitions.name":    "secondary",
		"global.enableConsulNamespaces":  "true",

		"global.tls.enabled":           "true",
		"global.tls.caCert.secretName": tlsCert,
		"global.tls.caCert.secretKey":  "tls.crt",
		"global.tls.caKey.secretName":  tlsKey,
		"global.tls.caKey.secretKey":   "tls.key",

		"externalServers.enabled":       "true",
		"externalServers.hosts[0]":      partitionSvcIP,
		"externalServers.tlsServerName": "server.dc1.consul",

		"client.enabled":           "true",
		"client.exposeGossipPorts": "true",
		"client.join[0]":           partitionSvcIP,

		"connectInject.enabled": "true",
	}

	if cfg.UseKind {
		secondaryHelmValues["externalServers.httpsPort"] = "30000"
	}

	// Install the consul cluster without servers in the secondary kubernetes context.
	secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)

	agentPodList, err := secondaryContext.KubernetesClient(t).CoreV1().Pods(secondaryContext.KubectlOptions(t).Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=consul,component=client"})
	require.NoError(t, err)
	require.Len(t, agentPodList.Items, 1)

	output, err := k8s.RunKubectlAndGetOutputE(t, secondaryContext.KubectlOptions(t), "logs", agentPodList.Items[0].Name, "-n", secondaryContext.KubectlOptions(t).Namespace)
	require.NoError(t, err)
	require.Contains(t, output, "Partition: 'secondary'")

	// TODO: These can be enabled once mesh gateways are used for communication between services. Currently we cant setup a flat pod network on Kind.
	// Check that we can connect services over the mesh gateways

	//logger.Log(t, "creating static-server in workload cluster")
	//k8s.DeployKustomize(t, secondaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	//logger.Log(t, "creating static-client in server cluster")
	//k8s.DeployKustomize(t, primaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-partition")

	//logger.Log(t, "checking that connection is successful")
	//k8s.CheckStaticServerConnectionSuccessful(t, primaryContext.KubectlOptions(t), "http://localhost:1234")
}
