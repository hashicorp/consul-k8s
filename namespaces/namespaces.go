// Package namespaces handles interaction with Consul namespaces needed across
// commands.
package namespaces

import (
	"fmt"

	capi "github.com/hashicorp/consul/api"
)

const WildcardNamespace string = "*"

// EnsureExists ensures a Consul namespace with name ns exists. If it doesn't,
// it will create it and set crossNSACLPolicy as a policy default.
// Boolean return value indicates if the namespace was created by this call.
func EnsureExists(client *capi.Client, ns string, crossNSAClPolicy string) (bool, error) {
	if ns == WildcardNamespace {
		return false, nil
	}
	// Check if the Consul namespace exists.
	namespaceInfo, _, err := client.Namespaces().Read(ns, nil)
	if err != nil {
		return false, err
	}
	if namespaceInfo != nil {
		return false, nil
	}

	// If not, create it.
	var aclConfig capi.NamespaceACLConfig
	if crossNSAClPolicy != "" {
		// Create the ACLs config for the cross-Consul-namespace
		// default policy that needs to be attached
		aclConfig = capi.NamespaceACLConfig{
			PolicyDefaults: []capi.ACLLink{
				{Name: crossNSAClPolicy},
			},
		}
	}

	consulNamespace := capi.Namespace{
		Name:        ns,
		Description: "Auto-generated by consul-k8s",
		ACLs:        &aclConfig,
		Meta:        map[string]string{"external-source": "kubernetes"},
	}

	_, _, err = client.Namespaces().Create(&consulNamespace, nil)
	return true, err
}

// ConsulNamespace returns the consul namespace that a service should be
// registered in based on the namespace options. It returns an
// empty string if namespaces aren't enabled.
func ConsulNamespace(kubeNS string, enableConsulNamespaces bool, consulDestNS string, enableMirroring bool, mirroringPrefix string) string {
	if !enableConsulNamespaces {
		return ""
	}

	// Mirroring takes precedence.
	if enableMirroring {
		return fmt.Sprintf("%s%s", mirroringPrefix, kubeNS)
	}

	return consulDestNS
}
