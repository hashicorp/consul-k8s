package read

import (
	"fmt"
	"strings"

	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

func formatClusters(clusters []Cluster) *terminal.Table {
	table := terminal.NewTable("Name", "FQDN", "Endpoints", "Type", "Last Updated")
	for _, cluster := range clusters {
		table.AddRow([]string{cluster.Name, cluster.FullyQualifiedDomainName, strings.Join(cluster.Endpoints, ", "),
			cluster.Type, cluster.LastUpdated}, []string{})
	}

	return table
}

func formatEndpoints(endpoints []Endpoint) *terminal.Table {
	table := terminal.NewTable("Address:Port", "Cluster", "Weight", "Status")
	for _, endpoint := range endpoints {
		var statusColor string
		if endpoint.Status == "HEALTHY" {
			statusColor = "green"
		} else {
			statusColor = "red"
		}

		table.AddRow(
			[]string{endpoint.Address, endpoint.Cluster, fmt.Sprintf("%.2f", endpoint.Weight), endpoint.Status},
			[]string{"", "", "", statusColor})
	}

	return table
}

func formatListeners(listeners []Listener) *terminal.Table {
	table := terminal.NewTable("Name", "Address:Port", "Direction", "Filter Chain Match", "Filters", "Last Updated")
	for _, listener := range listeners {
		for index, filter := range listener.FilterChain {
			// Print each element of the filter chain in a separate line
			// without repeating the name, address, etc.
			filters := strings.Join(filter.Filters, "\n")
			if index == 0 {
				table.AddRow(
					[]string{listener.Name, listener.Address, listener.Direction, filter.FilterChainMatch, filters, listener.LastUpdated},
					[]string{})
			} else {
				table.AddRow(
					[]string{"", "", "", filter.FilterChainMatch, filters},
					[]string{})
			}
		}
	}

	return table
}

func formatRoutes(routes []Route) *terminal.Table {
	table := terminal.NewTable("Name", "Destination Cluster", "Last Updated")
	for _, route := range routes {
		table.AddRow([]string{route.Name, route.DestinationCluster, route.LastUpdated}, []string{})
	}

	return table
}

func formatSecrets(secrets []Secret) *terminal.Table {
	table := terminal.NewTable("Name", "Type", "Last Updated")
	for _, secret := range secrets {
		table.AddRow([]string{secret.Name, secret.Type, secret.LastUpdated}, []string{})
	}

	return table
}
