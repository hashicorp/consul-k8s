package read

import (
	"strconv"
	"strings"
)

// FilterClusters takes a slice of clusters along with parameters for filtering
// those clusters.
//
//   - `fqdn` filters clusters to only those with fully qualified domain names
//     which contain the given value.
//   - `address` filters clusters to only those with endpoint addresses which
//     contain the given value.
//   - `port` filters clusters to only those with endpoint addresses with ports
//     that match the given value. If -1 is passed, no filtering will occur.
//
// The filters are applied in combination such that a cluster must adhere to
// all of the filtering values which are passed in.
func FilterClusters(clusters []Cluster, fqdn, address string, port int) []Cluster {
	// No filtering no-op.
	if fqdn == "" && address == "" && port == -1 {
		return clusters
	}

	portStr := ":" + strconv.Itoa(port)

	filtered := make([]Cluster, 0)
	for _, cluster := range clusters {
		if !strings.Contains(cluster.FullyQualifiedDomainName, fqdn) {
			continue
		}

		endpoints := strings.Join(cluster.Endpoints, " ")
		if !strings.Contains(endpoints, address) || (port != -1 && !strings.Contains(endpoints, portStr)) {
			continue
		}

		hasFQDN := strings.Contains(cluster.FullyQualifiedDomainName, fqdn)
		hasAddress := strings.Contains(endpoints, address)
		hasPort := port == -1 || strings.Contains(endpoints, portStr)

		if hasFQDN && hasAddress && hasPort {
			filtered = append(filtered, cluster)
		}
	}

	return filtered
}

// FilterEndpoints takes a slice of endpoints along with parameters for filtering
// those endpoints:
//
//   - `address` filters endpoints to only those with an address which contains
//     the given value.
//   - `port` filters endpoints to only those with an address which has a port
//     that matches the given value. If -1 is passed, no filtering will occur.
//
// The filters are applied in combination such that an endpoint must adhere to
// all of the filtering values which are passed in.
func FilterEndpoints(endpoints []Endpoint, address string, port int) []Endpoint {
	if address == "" && port == -1 {
		return endpoints
	}

	portStr := ":" + strconv.Itoa(port)

	filtered := make([]Endpoint, 0)
	for _, endpoint := range endpoints {
		if strings.Contains(endpoint.Address, address) && (port == -1 || strings.Contains(endpoint.Address, portStr)) {
			filtered = append(filtered, endpoint)
		}
	}

	return filtered
}

// FilterListeners takes a slice of listeners along with parameters for filtering
// those endpoints:
//
//   - `address` filters listeners to only those with an address which contains
//     the given value.
//   - `port` filters listeners to only those with an address which has a port
//     that matches the given value. If -1 is passed, no filtering will occur.
//
// The filters are applied in combination such that an listener must adhere to
// all of the filtering values which are passed in.
func FilterListeners(listeners []Listener, address string, port int) []Listener {
	if address == "" && port == -1 {
		return listeners
	}

	portStr := ":" + strconv.Itoa(port)

	filtered := make([]Listener, 0)
	for _, listener := range listeners {
		if strings.Contains(listener.Address, address) && (port == -1 || strings.Contains(listener.Address, portStr)) {
			filtered = append(filtered, listener)
		}
	}

	return filtered
}
