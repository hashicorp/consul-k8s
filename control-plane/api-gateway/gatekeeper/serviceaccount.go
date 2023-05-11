package gatekeeper

import (
	"context"
	"errors"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (g *Gatekeeper) upsertServiceAccount(ctx context.Context) error {
	// We don't create the ServiceAccount if we are not using ManagedGatewayClass.
	if !g.HelmConfig.ManageSystemACLs {
		return nil
	}

	serviceAccount := &corev1.ServiceAccount{}
	exists := false

	// Get ServiceAccount if it exists.
	err := g.Client.Get(ctx, g.namespacedName(), serviceAccount)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else {
		exists = true
	}

	if exists {
		// Ensure we own the ServiceAccount.
		for _, ref := range serviceAccount.GetOwnerReferences() {
			if ref.UID == g.Gateway.GetUID() && ref.Name == g.Gateway.GetName() {
				// We found ourselves!
				return nil
			}
		}
		return errors.New("ServiceAccount not owned by controller")
	}

	// Create the ServiceAccount.
	serviceAccount = g.serviceAccount()
	if err := ctrl.SetControllerReference(&g.Gateway, serviceAccount, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, serviceAccount); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deleteServiceAccount(ctx context.Context) error {
	if err := g.Client.Delete(ctx, g.serviceAccount()); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) serviceAccount() *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.Gateway.Name,
			Namespace: g.Gateway.Namespace,
			Labels:    apigateway.LabelsForGateway(&g.Gateway),
		},
	}
}
