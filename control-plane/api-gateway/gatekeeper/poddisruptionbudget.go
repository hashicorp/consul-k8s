package gatekeeper

import (
	"context"
	"errors"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func (g *Gatekeeper) upsertPodDisruptionBudget(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) error {
	if gcc.Spec.DeploymentSpec.PodDisruptionBudgetSpec == nil || !gcc.Spec.DeploymentSpec.PodDisruptionBudgetSpec.Enabled {
		return g.deletePodDisruptionBudget(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name})
	}

	pdb := &policyv1.PodDisruptionBudget{}
	exists := false

	// Get PDB if it exists.
	err := g.Client.Get(ctx, g.namespacedName(gateway), pdb)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if k8serrors.IsNotFound(err) {
		exists = false
	} else {
		exists = true
	}

	if exists {
		// Ensure we own the PDB.
		for _, ref := range pdb.GetOwnerReferences() {
			if ref.UID == gateway.GetUID() && ref.Name == gateway.GetName() {
				// We found ourselves!
				return nil
			}
		}
		return errors.New("PDB not owned by controller")
	}

	// Create the PDB.
	pdb = g.podDisruptionBudget(gateway, gcc)
	if err := ctrl.SetControllerReference(&gateway, pdb, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, pdb); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deletePodDisruptionBudget(ctx context.Context, gwName types.NamespacedName) error {
	if err := g.Client.Delete(ctx, &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: gwName.Name, Namespace: gwName.Namespace}}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

// https://kubernetes.io/docs/reference/kubernetes-api/policy-resources/pod-disruption-budget-v1/#PodDisruptionBudgetSpec
func (g *Gatekeeper) podDisruptionBudget(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig) *policyv1.PodDisruptionBudget {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    common.LabelsForGateway(&gateway),
		},
	}

	pdb.Spec = policyv1.PodDisruptionBudgetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: common.LabelsForGateway(&gateway),
		},
	}

	// if both minAvailable and maxUnavailable are set, minAvailable takes precedence
	if gcc.Spec.DeploymentSpec.PodDisruptionBudgetSpec.MinAvailable != nil {
		pdb.Spec.MinAvailable = gcc.Spec.DeploymentSpec.PodDisruptionBudgetSpec.MinAvailable
	} else if gcc.Spec.DeploymentSpec.PodDisruptionBudgetSpec.MaxUnavailable != nil {
		pdb.Spec.MaxUnavailable = gcc.Spec.DeploymentSpec.PodDisruptionBudgetSpec.MaxUnavailable
	}

	return pdb
}
