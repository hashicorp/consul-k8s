// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package namespaces handles interaction with Consul namespaces needed across
// commands.
package namespaces

import (
	"fmt"

	capi "github.com/hashicorp/consul/api"
)

const (
	WildcardNamespace = "*"
	DefaultNamespace  = "default"
)

// EnsureExists ensures a Consul namespace with name ns exists. If it doesn't,
// it will create it and set crossNSACLPolicy as a policy default.
// Boolean return value indicates if the namespace was created by this call.
func EnsureExists(client *capi.Client, ns string, crossNSAClPolicy string) (bool, error) {
	if ns == WildcardNamespace || ns == DefaultNamespace {
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

// EnsureDeleted ensures a Consul namespace with name ns is deleted. If it is already not found
// the call to delete will be skipped.
func EnsureDeleted(client *capi.Client, ns string) error {
	if ns == WildcardNamespace || ns == DefaultNamespace {
		return nil
	}
	// Check if the Consul namespace exists.
	namespaceInfo, _, err := client.Namespaces().Read(ns, nil)
	if err != nil {
		return fmt.Errorf("could not read namespace %s: %w", ns, err)
	}
	if namespaceInfo == nil {
		return nil
	}

	// If not empty, delete it.
	_, err = client.Namespaces().Delete(ns, nil)
	if err != nil {
		return fmt.Errorf("could not delete namespace %s: %w", ns, err)
	}
	return nil
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

// NonDefaultConsulNamespace returns the given Consul namespace if it is not default or empty.
// Otherwise, it returns the empty string.
func NonDefaultConsulNamespace(consulNS string) string {
	if consulNS == "" || consulNS == DefaultNamespace {
		return ""
	}
	return consulNS
}
