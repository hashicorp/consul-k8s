package uninstall

import (
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	crdPath     = "/apis/apiextensions.k8s.io/v1/customresourcedefinitions"
	consulGroup = "consul.hashicorp.com"
)

// crds is used to deserialize JSON returned from the
// `/apis/apiextensions.k8s.io/v1/customresourcedefinitions` endpoint.
type crds struct {
	Items []crd `json:"items"`
}

// crd is used to deserialize the JSON definition of a single CRD.
type crd struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Group string `json:"group"`
		Names struct {
			Kind string `json:"kind"`
		} `json:"names"`
		Versions []struct {
			Name string `json:"name"`
		} `json:"versions"`
	} `json:"spec"`
}

// crs is used to deserialize JSON returned from the
// `/apis/consul.hashicorp.com/<VERSION>/<CRD-NAME>` endpoint.
type crs struct {
	Items []cr `json:"items"`
}

// cr is used to deserialize the JSOn definition of a single CR.
type cr struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Uid       string `json:"uid"`
	} `json:"metadata"`
}

// patchCustomResources removes the finalizers from all custom resources
// managed by Consul. This allows them to be removed from the Kubernetes cluster.
func patchCustomResources(ctx context.Context, restClient rest.Interface, client client.Client) error {
	// Get all custom resource definitions in the Kubernetes cluster.
	raw, err := restClient.Get().AbsPath(crdPath).DoRaw(ctx)
	if err != nil {
		return nil
	}
	var allCRDs crds
	if err := json.Unmarshal(raw, &allCRDs); err != nil {
		return err
	}

	// Filter only to CRDs managed by Consul.
	var consulCRDs crds
	for _, crd := range allCRDs.Items {
		if crd.Spec.Group == consulGroup {
			consulCRDs.Items = append(consulCRDs.Items, crd)
		}
	}
	if len(consulCRDs.Items) == 0 {
		return nil
	}

	// Get all custom resources for each custom resource definition.
	var consulCRs []cr
	for _, crd := range consulCRDs.Items {
		for _, path := range crPaths(crd) {
			raw, err := restClient.Get().AbsPath(path).DoRaw(ctx)
			if err != nil {
				return err
			}
			var crs crs
			if err := json.Unmarshal(raw, &crs); err != nil {
				return err
			}
			consulCRs = append(consulCRs, crs.Items...)
		}
	}

	// Patch the finalizers for each custom resource.
	var target = &unstructured.Unstructured{}
	for _, cr := range consulCRs {
		target.SetNamespace(cr.Metadata.Namespace)
		target.SetName(cr.Metadata.Name)
		target.SetKind(cr.Kind)

		if err := client.Patch(ctx, target, nil); err != nil {
			return err
		}
	}

	return nil
}

// crPaths returns a Kubernetes API path to the custom resources
// for each version of the custom resource definition.
func crPaths(crd crd) []string {
	var paths []string
	for _, version := range crd.Spec.Versions {
		paths = append(paths, fmt.Sprintf("/apis/%s/%s/%s", consulGroup, version, crd.Spec.Names.Kind))
	}
	return paths
}
