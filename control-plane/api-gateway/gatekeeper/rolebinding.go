// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	rbac "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (g *Gatekeeper) upsertRoleBinding(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) error {
	if config.AuthMethod == "" && !config.EnableOpenShift {
		return g.deleteRole(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name})
	}

	roleBinding := &rbac.RoleBinding{}

	// If the RoleBinding already exists, ensure that we own the RoleBinding
	err := g.Client.Get(ctx, g.namespacedName(gateway), roleBinding)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if !k8serrors.IsNotFound(err) {
		// Ensure we own the Role.
		for _, ref := range roleBinding.GetOwnerReferences() {
			if ref.UID == gateway.GetUID() && ref.Name == gateway.GetName() {
				// We found ourselves!
				return nil
			}
		}
		return errors.New("role not owned by controller")
	}

	// Create or update the RoleBinding
	roleBinding = g.roleBinding(gateway, gcc, config)
	if err := ctrl.SetControllerReference(&gateway, roleBinding, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, roleBinding); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deleteRoleBinding(ctx context.Context, gwName types.NamespacedName) error {
	if err := g.Client.Delete(ctx, &rbac.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: gwName.Name, Namespace: gwName.Namespace}}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) roleBinding(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) *rbac.RoleBinding {
	// Create resources for reference. This avoids bugs if naming patterns change.
	serviceAccount := g.serviceAccount(gateway)
	role := g.role(gateway, gcc, config)

	return &rbac.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    common.LabelsForGateway(&gateway),
		},
		RoleRef: rbac.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role.Name,
		},
		Subjects: []rbac.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount.Name,
				Namespace: serviceAccount.Namespace,
			},
		},
	}
}
