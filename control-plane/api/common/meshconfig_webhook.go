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

// MeshConfigLister is implemented by CRD-specific webhooks.
type MeshConfigLister interface {
	// List returns all resources of this type across all namespaces in a
	// Kubernetes cluster.
	List(ctx context.Context) ([]MeshConfig, error)
}

// ValidateMeshConfig validates a MeshConfig. It is a generic method that
// can be used by all CRD-specific validators.
// Callers should pass themselves as validator and kind should be the custom
// resource name, e.g. "TrafficPermissions".
func ValidateMeshConfig(
	ctx context.Context,
	req admission.Request,
	logger logr.Logger,
	meshConfigLister MeshConfigLister,
	meshConfig MeshConfig,
	tenancy ConsulTenancyConfig) admission.Response {

	defaultingPatches, err := MeshConfigDefaultingPatches(meshConfig, tenancy)
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
		logger.Info("validate create", "name", meshConfig.KubernetesName())

		list, err := meshConfigLister.List(ctx)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		for _, item := range list {
			if item.KubernetesName() == meshConfig.KubernetesName() {
				return admission.Errored(http.StatusBadRequest,
					fmt.Errorf("%s resource with name %q is already defined – all %s resources must have unique names across namespaces",
						meshConfig.KubeKind(),
						meshConfig.KubernetesName(),
						meshConfig.KubeKind()))
			}
		}
	}
	if err := meshConfig.Validate(tenancy); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Patched(fmt.Sprintf("valid %s request", meshConfig.KubeKind()), defaultingPatches...)
}

// MeshConfigDefaultingPatches returns the patches needed to set fields to their defaults.
func MeshConfigDefaultingPatches(meshConfig MeshConfig, tenancy ConsulTenancyConfig) ([]jsonpatch.Operation, error) {
	beforeDefaulting, err := json.Marshal(meshConfig)
	if err != nil {
		return nil, fmt.Errorf("marshalling input: %s", err)
	}
	meshConfig.DefaultNamespaceFields(tenancy)
	afterDefaulting, err := json.Marshal(meshConfig)
	if err != nil {
		return nil, fmt.Errorf("marshalling after defaulting: %s", err)
	}

	defaultingPatches, err := jsonpatch.CreatePatch(beforeDefaulting, afterDefaulting)
	if err != nil {
		return nil, fmt.Errorf("creating patches: %s", err)
	}
	return defaultingPatches, nil
}
