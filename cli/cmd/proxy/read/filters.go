package read

import "strings"

// FilterFQDN takes a slice of clusters along with a substring
// and filters the clusters to only those with fully qualified
// domain names which contain the given substring.
func FilterFQDN(clusters []Cluster, substring string) []Cluster {
	filtered := make([]Cluster, 0)
	for _, cluster := range clusters {
		if strings.Contains(cluster.FullyQualifiedDomainName, substring) {
			filtered = append(filtered, cluster)
		}
	}

	return filtered
}
