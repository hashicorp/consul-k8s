package terminatinggateway

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const (
	staticClientName         = "static-client"
	staticServerName         = "static-server"
	staticServerLocalAddress = "http://localhost:1234"
)

func addIntention(t *testing.T, consulClient *api.Client, sourceNS, sourceService, destinationNS, destinationsService string) {
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
			},
		},
	}, nil)
	require.NoError(t, err)
}

func createTerminatingGatewayConfigEntry(t *testing.T, consulClient *api.Client, gwNamespace, serviceNamespace string, serviceNames ...string) {
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

func updateTerminatingGatewayRole(t *testing.T, consulClient *api.Client, rules string) {
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
