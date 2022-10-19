package common

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Finalizer implements the Kubernetes controller-runtime client.Patch interface
// to remove any finalizers on a resource.
// Ref: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client@v0.13.0#Patch
type Finalizer struct{}

// NewFinalizer returns a new Finalizer instance for patching finalizers on
// Kubernetes resources to be an empty list.
func NewFinalizer() Finalizer { return Finalizer{} }

// Type returns the JSON Patch Type
// Ref: https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/#use-a-json-merge-patch-to-update-a-deployment
func (f Finalizer) Type() types.PatchType { return types.JSONPatchType }

func (f Finalizer) Data(obj client.Object) ([]byte, error) {
	return nil, nil
}
