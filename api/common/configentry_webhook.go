package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"gomodules.xyz/jsonpatch/v2"
	"k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ConfigEntryLister is implemented by CRD-specific webhooks.
type ConfigEntryLister interface {
	// List returns all resources of this type across all namespaces in a
	// Kubernetes cluster.
	List(ctx context.Context) ([]ConfigEntryResource, error)
}

// ValidateConfigEntry validates cfgEntry. It is a generic method that
// can be used by all CRD-specific validators.
// Callers should pass themselves as validator and kind should be the custom
// resource name, e.g. "ServiceDefaults".
func ValidateConfigEntry(
	ctx context.Context,
	req admission.Request,
	logger logr.Logger,
	configEntryLister ConfigEntryLister,
	cfgEntry ConfigEntryResource,
	enableConsulNamespaces bool,
	nsMirroring bool,
	consulDestinationNamespace string,
	nsMirroringPrefix string) admission.Response {

	defaultingPatches, err := defaultingPatches(cfgEntry, enableConsulNamespaces, nsMirroring, consulDestinationNamespace, nsMirroringPrefix)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	// On create we need to validate that there isn't already a resource with
	// the same name in a different namespace if we're need to mapping all Kube
	// resources to a single Consul namespace. The only case where we're not
	// mapping all kube resources to a single Consul namespace is when we
	// are running Consul enterprise with namespace mirroring.
	singleConsulDestNS := !(enableConsulNamespaces && nsMirroring)
	if req.Operation == v1beta1.Create && singleConsulDestNS {
		logger.Info("validate create", "name", cfgEntry.KubernetesName())

		list, err := configEntryLister.List(ctx)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		for _, item := range list {
			if item.KubernetesName() == cfgEntry.KubernetesName() {
				return admission.Errored(http.StatusBadRequest,
					fmt.Errorf("%s resource with name %q is already defined – all %s resources must have unique names across namespaces",
						cfgEntry.KubeKind(),
						cfgEntry.KubernetesName(),
						cfgEntry.KubeKind()))
			}
		}
	}
	if err := cfgEntry.Validate(enableConsulNamespaces); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Patched(fmt.Sprintf("valid %s request", cfgEntry.KubeKind()), defaultingPatches...)
}

// defaultingPatches returns the patches needed to set fields to their
// defaults.
func defaultingPatches(svcIntentions ConfigEntryResource, enableConsulNamespaces bool, nsMirroring bool, consulDestinationNamespace string, nsMirroringPrefix string) ([]jsonpatch.Operation, error) {
	beforeDefaulting, err := json.Marshal(svcIntentions)
	if err != nil {
		return nil, fmt.Errorf("marshalling input: %s", err)
	}
	svcIntentions.Default(enableConsulNamespaces, consulDestinationNamespace, nsMirroring, nsMirroringPrefix)
	afterDefaulting, err := json.Marshal(svcIntentions)
	if err != nil {
		return nil, fmt.Errorf("marshalling after defaulting: %s", err)
	}

	defaultingPatches, err := jsonpatch.CreatePatch(beforeDefaulting, afterDefaulting)
	if err != nil {
		return nil, fmt.Errorf("creating patches: %s", err)
	}
	return defaultingPatches, nil
}
