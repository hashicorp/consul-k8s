package common

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
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
	cfgEntry ConfigEntryResource) admission.Response {

	// On create we need to validate that there isn't already a resource with
	// the same name in a different namespace since we need to map all Kube
	// resources to a single Consul namespace.
	if req.Operation == v1beta1.Create {
		logger.Info("validate create", "name", cfgEntry.Name())

		list, err := configEntryLister.List(ctx)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		for _, item := range list {
			if item.Name() == cfgEntry.Name() {
				// todo: If running Consul Ent with mirroring need to change this to respect namespaces.
				return admission.Errored(http.StatusBadRequest,
					fmt.Errorf("%s resource with name %q is already defined – all %s resources must have unique names across namespaces",
						cfgEntry.KubeKind(),
						cfgEntry.Name(),
						cfgEntry.KubeKind()))
			}
		}
	}
	if err := cfgEntry.Validate(); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Allowed(fmt.Sprintf("valid %s request", cfgEntry.KubeKind()))
}
