// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminatinggateway

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const (
	staticClientName         = "static-client"
	staticServerName         = "static-server"
	staticServerLocalAddress = "http://localhost:1234"
)

func AddIntention(t *testing.T, consulClient *api.Client, sourcePeer, sourceNS, sourceService, destinationNS, destinationsService string) {
	t.Helper()

	logger.Log(t, fmt.Sprintf("creating %s => %s intention", sourceService, destinationsService))
	_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind:      api.ServiceIntentions,
		Name:      destinationsService,
		Namespace: destinationNS,
		Sources: []*api.SourceIntention{
			{
				Name:      sourceService,
				Namespace: sourceNS,
				Action:    api.IntentionActionAllow,
				Peer:      sourcePeer,
			},
		},
	}, nil)
	require.NoError(t, err)
}

func CreateTerminatingGatewayFromCRD(t *testing.T, kubectlOptions *k8s.KubectlOptions, noCleanupOnFailure, noCleanup bool, path string) {
	// Create the config entry for the terminating gateway.
	k8s.KubectlApply(t, kubectlOptions, path)

	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		k8s.KubectlDelete(t, kubectlOptions, path)
	})
}

func CreateTerminatingGatewayConfigEntry(t *testing.T, consulClient *api.Client, gwNamespace, serviceNamespace string, serviceNames ...string) {
	t.Helper()

	logger.Log(t, "creating config entry")

	if serviceNamespace != "" {
		logger.Logf(t, "creating the %s namespace in Consul", serviceNamespace)
		_, _, err := consulClient.Namespaces().Create(&api.Namespace{
			Name: serviceNamespace,
		}, nil)
		require.NoError(t, err)
	}

	var gatewayServices []api.LinkedService
	for _, serviceName := range serviceNames {
		linkedService := api.LinkedService{Name: serviceName, Namespace: serviceNamespace}
		gatewayServices = append(gatewayServices, linkedService)
	}

	configEntry := &api.TerminatingGatewayConfigEntry{
		Kind:      api.TerminatingGateway,
		Name:      "terminating-gateway",
		Namespace: gwNamespace,
		Services:  gatewayServices,
	}

	created, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
	require.NoError(t, err)
	require.True(t, created, "failed to create config entry")
}

func UpdateTerminatingGatewayRole(t *testing.T, consulClient *api.Client, rules string) {
	t.Helper()

	logger.Log(t, "creating a write policy for the static-server")
	_, _, err := consulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:  "static-server-write-policy",
		Rules: rules,
	}, nil)
	require.NoError(t, err)

	logger.Log(t, "getting the terminating gateway role")
	roles, _, err := consulClient.ACL().RoleList(nil)
	require.NoError(t, err)
	terminatingGatewayRoleID := ""
	for _, role := range roles {
		if strings.Contains(role.Name, "terminating-gateway") {
			terminatingGatewayRoleID = role.ID
			break
		}
	}

	logger.Log(t, "update role with policy")
	termGwRole, _, err := consulClient.ACL().RoleRead(terminatingGatewayRoleID, nil)
	require.NoError(t, err)
	termGwRole.Policies = append(termGwRole.Policies, &api.ACLTokenPolicyLink{Name: "static-server-write-policy"})
	_, _, err = consulClient.ACL().RoleUpdate(termGwRole, nil)
	require.NoError(t, err)
}

func CreateServiceDefaultDestination(t *testing.T, consulClient *api.Client, serviceNamespace string, name string, protocol string, port int, addresses ...string) {
	t.Helper()

	logger.Log(t, "creating config entry")

	if serviceNamespace != "" {
		logger.Logf(t, "creating the %s namespace in Consul", serviceNamespace)
		_, _, err := consulClient.Namespaces().Create(&api.Namespace{
			Name: serviceNamespace,
		}, nil)
		require.NoError(t, err)
	}

	configEntry := &api.ServiceConfigEntry{
		Kind:      api.ServiceDefaults,
		Name:      name,
		Namespace: serviceNamespace,
		Protocol:  protocol,
		Destination: &api.DestinationConfig{
			Addresses: addresses,
			Port:      port,
		},
	}

	created, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
	require.NoError(t, err)
	require.True(t, created, "failed to create config entry")
}

func CreateMeshConfigEntry(t *testing.T, consulClient *api.Client, namespace string) {
	t.Helper()

	logger.Log(t, "creating mesh config entry to enable MeshDestinationOnly")
	created, _, err := consulClient.ConfigEntries().Set(&api.MeshConfigEntry{
		Namespace: namespace,
		TransparentProxy: api.TransparentProxyMeshConfig{
			MeshDestinationsOnly: true,
		},
	}, nil)
	require.NoError(t, err)
	require.True(t, created, "failed to create config entry")
}
