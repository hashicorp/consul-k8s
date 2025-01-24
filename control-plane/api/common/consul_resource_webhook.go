// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ConsulResourceLister is implemented by CRD-specific webhooks.
type ConsulResourceLister interface {
	// List returns all resources of this type across all namespaces in a
	// Kubernetes cluster.
	List(ctx context.Context) ([]ConsulResource, error)
}

// ValidateConsulResource validates a Consul Resource. It is a generic method that
// can be used by all CRD-specific validators.
// Callers should pass themselves as validator and kind should be the custom
// resource name, e.g. "TrafficPermissions".
func ValidateConsulResource(
	ctx context.Context,
	req admission.Request,
	logger logr.Logger,
	resourceLister ConsulResourceLister,
	resource ConsulResource,
	tenancy ConsulTenancyConfig) admission.Response {

	defaultingPatches, err := ConsulResourceDefaultingPatches(resource, tenancy)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	// On create we need to validate that there isn't already a resource with
	// the same name in a different namespace if we're mapping all Kube
	// resources to a single Consul namespace. The only case where we're not
	// mapping all kube resources to a single Consul namespace is when we
	// are running Consul enterprise with namespace mirroring.
	singleConsulDestNS := !(tenancy.EnableConsulNamespaces && tenancy.EnableNSMirroring)
	if req.Operation == admissionv1.Create && singleConsulDestNS {
		logger.Info("validate create", "name", resource.KubernetesName())

		list, err := resourceLister.List(ctx)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		for _, item := range list {
			if item.KubernetesName() == resource.KubernetesName() {
				return admission.Errored(http.StatusBadRequest,
					fmt.Errorf("%s resource with name %q is already defined – all %s resources must have unique names across namespaces",
						resource.KubeKind(),
						resource.KubernetesName(),
						resource.KubeKind()))
			}
		}
	}
	if err := resource.Validate(tenancy); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Patched(fmt.Sprintf("valid %s request", resource.KubeKind()), defaultingPatches...)
}

// ConsulResourceDefaultingPatches returns the patches needed to set fields to their defaults.
func ConsulResourceDefaultingPatches(resource ConsulResource, tenancy ConsulTenancyConfig) ([]jsonpatch.Operation, error) {
	beforeDefaulting, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("marshalling input: %s", err)
	}
	resource.DefaultNamespaceFields(tenancy)
	afterDefaulting, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("marshalling after defaulting: %s", err)
	}

	defaultingPatches, err := jsonpatch.CreatePatch(beforeDefaulting, afterDefaulting)
	if err != nil {
		return nil, fmt.Errorf("creating patches: %s", err)
	}
	return defaultingPatches, nil
}
