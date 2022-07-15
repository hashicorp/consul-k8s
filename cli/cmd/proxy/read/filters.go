package read

import (
	"strconv"
	"strings"
)

// FilterClustersByFQDN takes a slice of clusters along with a substring
// and filters the clusters to only those with fully qualified
// domain names which contain the given substring.
func FilterClustersByFQDN(clusters []Cluster, substring string) []Cluster {
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

// FilterClustersByPort takes a slice of clusters along with a port number
// and filters the clusters to only those with endpoints whose
// ports match the given port.
// If -1 is passed as a port, no filtering will occur.
func FilterClustersByPort(clusters []Cluster, port int) []Cluster {
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

// FilterEndpointsByPort takes a slice of endpoints along with a port number
// and filters the endpoints to only those whose port matches the given port.
// If -1 is passed as a port, no filtering will occur.
func FilterEndpointsByPort(endpoints []Endpoint, port int) []Endpoint {
	if port == -1 {
		return endpoints
	}

	filtered := make([]Endpoint, 0)

	return filtered
}

// FilterListenersByPort takes a slice of listeners along with a port number
// and filters the listeners to only those with an address whose port matches
// the given port.
// If -1 is passed as a port, no filtering will occur.
func FilterListenersByPort(listeners []Listener, port int) []Listener {
	if port == -1 {
		return listeners
	}

	filtered := make([]Listener, 0)

	return filtered
}

// FilterListenersByAddress takes a slice of listeners along with a substring
// and filters the listeners to only those with an address that contains the
// given substring.
func FilterListenersByAddress(listeners []Listener, substring string) []Listener {
	if substring == "" {
		return listeners
	}

	filtered := make([]Listener, 0)

	return filtered
}
