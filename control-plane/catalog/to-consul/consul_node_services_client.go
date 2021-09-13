package catalog

import (
	"fmt"

	"github.com/hashicorp/consul/api"
)

// ConsulService is service registered in Consul.
type ConsulService struct {
	// Namespace is the Consul namespace the service is registered in.
	// If namespaces are disabled this will always be the empty string even
	// though the namespace is technically "default".
	Namespace string
	// Name is the name of the service in Consul.
	Name string
}

// ConsulNodeServicesClient is used to query for node services.
type ConsulNodeServicesClient interface {
	// NodeServices returns consul services with the corresponding tag
	// registered to the Consul node with nodeName. opts is used as the
	// query options in the API call to consul. It returns the list of services
	// (not service instances) and the query meta from the API call.
	NodeServices(tag string, nodeName string, opts api.QueryOptions) ([]ConsulService, *api.QueryMeta, error)
}

// PreNamespacesNodeServicesClient implements ConsulNodeServicesClient
// for Consul < 1.7 which does not support namespaces.
type PreNamespacesNodeServicesClient struct {
	Client *api.Client
}

// NodeServices returns Consul services tagged with
// tag registered on nodeName using a Consul API that is supported in
// Consul versions before 1.7. Consul versions after 1.7 still support
// this API but the API is not namespace-aware.
func (s *PreNamespacesNodeServicesClient) NodeServices(
	tag string,
	nodeName string,
	opts api.QueryOptions) ([]ConsulService, *api.QueryMeta, error) {
	// NOTE: We're not using tag filtering here so we can support Consul
	// < 1.5.
	node, meta, err := s.Client.Catalog().Node(nodeName, &opts)
	if err != nil {
		return nil, nil, err
	}
	if node == nil {
		return nil, meta, nil
	}

	var svcs []ConsulService
	// seenServices is used to ensure the svcs list is unique.
	seenServices := make(map[string]bool)
	for _, svcInstance := range node.Services {
		svcName := svcInstance.Service
		if _, ok := seenServices[svcName]; ok {
			continue
		}
		for _, svcTag := range svcInstance.Tags {
			if svcTag == tag {
				if _, ok := seenServices[svcName]; !ok {
					svcs = append(svcs, ConsulService{
						// If namespaces are not enabled we use empty
						// string.
						Namespace: "",
						Name:      svcName,
					})
					seenServices[svcName] = true
				}
				break
			}
		}
	}
	return svcs, meta, nil
}

// NamespacesNodeServicesClient implements ConsulNodeServicesClient
// for Consul >= 1.7 which supports namespaces.
type NamespacesNodeServicesClient struct {
	Client *api.Client
}

// NodeServices returns Consul services tagged with
// tag registered on nodeName using a Consul API that is supported in
// Consul versions >= 1.7. If opts.Namespace is set to
// "*", services from all namespaces will be returned.
func (s *NamespacesNodeServicesClient) NodeServices(
	tag string,
	nodeName string,
	opts api.QueryOptions) ([]ConsulService, *api.QueryMeta, error) {
	opts.Filter = fmt.Sprintf("\"%s\" in Tags", tag)
	nodeCatalog, meta, err := s.Client.Catalog().NodeServiceList(nodeName, &opts)
	if err != nil {
		return nil, nil, err
	}

	var svcs []ConsulService
	// seenServices is used to ensure the svcs list is unique. Its keys are
	// <namespace>/<service name>.
	seenSvcs := make(map[string]bool)
	for _, svcInstance := range nodeCatalog.Services {
		svcName := svcInstance.Service
		key := fmt.Sprintf("%s/%s", svcInstance.Namespace, svcName)
		if _, ok := seenSvcs[key]; !ok {
			svcs = append(svcs, ConsulService{
				Namespace: svcInstance.Namespace,
				Name:      svcName,
			})
			seenSvcs[key] = true
		}
	}
	return svcs, meta, nil
}
