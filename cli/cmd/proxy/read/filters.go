package read

import (
	"strconv"
	"strings"
)

// FilterFQDN takes a slice of clusters along with a substring
// and filters the clusters to only those with fully qualified
// domain names which contain the given substring.
func FilterFQDN(clusters []Cluster, substring string) []Cluster {
	if substring == "" {
		return clusters
	}

	filtered := make([]Cluster, 0)
	for _, cluster := range clusters {
		if strings.Contains(cluster.FullyQualifiedDomainName, substring) {
			filtered = append(filtered, cluster)
		}
	}

	return filtered
}

// FilterPort takes a slice of clusters along with a port number
// and filters the clusters to only those with endpoints whose
// ports match the given port.
func FilterPort(clusters []Cluster, port int) []Cluster {
	if port == -1 {
		return clusters
	}

	portStr := strconv.Itoa(port)

	filtered := make([]Cluster, 0)
	for _, cluster := range clusters {
		for _, endpoint := range cluster.Endpoints {
			if strings.HasSuffix(endpoint, portStr) {
				filtered = append(filtered, cluster)
			}
		}
	}

	return filtered
}
