package gatekeeper

import (
	"context"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	v1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func (g *Gatekeeper) upsertPodDisruptionBudget(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) error {
	if gcc.Spec.PodDisruptionBudgetSpec == nil || !gcc.Spec.PodDisruptionBudgetSpec.Enabled {
		return g.deletePodDisruptionBudget(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name})
	}

	pdb := g.podDisruptionBudget(ctx, gateway, gcc, config)
	mutated := pdb.DeepCopy()
	mutator := newPodDisruptionBudgetMutator(pdb, mutated, gateway, g.Client.Scheme())

	result, err := controllerutil.CreateOrUpdate(ctx, g.Client, mutated, mutator)
	if err != nil {
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		g.Log.V(1).Info("Created Service")
	case controllerutil.OperationResultUpdated:
		g.Log.V(1).Info("Updated Service")
	case controllerutil.OperationResultNone:
		g.Log.V(1).Info("No change to service")
	}

	return nil
}

func mergePDB(from, to *v1.PodDisruptionBudget) *v1.PodDisruptionBudget {
	if arePDBsEqual(from, to) {
		return to
	}

	to.ObjectMeta.Name = from.ObjectMeta.Name
	to.ObjectMeta.Namespace = from.ObjectMeta.Namespace
	to.Spec.Selector = from.Spec.Selector
	to.Spec.MinAvailable = from.Spec.MinAvailable
	to.Spec.MaxUnavailable = from.Spec.MaxUnavailable

	return to
}

func arePDBsEqual(a, b *v1.PodDisruptionBudget) bool {
	if a.ObjectMeta.GetName() != b.ObjectMeta.GetName() ||
		a.ObjectMeta.GetNamespace() != b.ObjectMeta.GetNamespace() ||
		!equality.Semantic.DeepEqual(a.Spec.Selector, b.Spec.Selector) ||
		a.Spec.MinAvailable != b.Spec.MinAvailable ||
		a.Spec.MaxUnavailable != b.Spec.MaxUnavailable {
		return false
	}

	return true
}

func newPodDisruptionBudgetMutator(pdb, mutated *v1.PodDisruptionBudget, gateway gwv1beta1.Gateway, scheme *runtime.Scheme) resourceMutator {
	return func() error {
		mutated = mergePDB(pdb, mutated)
		return ctrl.SetControllerReference(&gateway, mutated, scheme)
	}
}

func (g *Gatekeeper) deletePodDisruptionBudget(ctx context.Context, gwName types.NamespacedName) error {
	if err := g.Client.Delete(ctx, &v1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: gwName.Name, Namespace: gwName.Namespace}}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

// https://kubernetes.io/docs/reference/kubernetes-api/policy-resources/pod-disruption-budget-v1/#PodDisruptionBudgetSpec
func (g *Gatekeeper) podDisruptionBudget(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) *v1.PodDisruptionBudget {
	pdb := &v1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    common.LabelsForGateway(&gateway),
		},
	}

	pdb.Spec = v1.PodDisruptionBudgetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: common.LabelsForGateway(&gateway),
		},
	}

	if gcc.Spec.PodDisruptionBudgetSpec.MinAvailable != nil {
		pdb.Spec.MinAvailable = gcc.Spec.PodDisruptionBudgetSpec.MinAvailable
	}

	if gcc.Spec.PodDisruptionBudgetSpec.MaxUnavailable != nil {
		pdb.Spec.MaxUnavailable = gcc.Spec.PodDisruptionBudgetSpec.MaxUnavailable
	}

	return pdb
}
