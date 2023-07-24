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

func (g *Gatekeeper) upsertRole(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) error {
	if config.AuthMethod == "" {
		return g.deleteRole(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name})
	}

	role := &rbac.Role{}

	// If the Role already exists, ensure that we own the Role
	err := g.Client.Get(ctx, g.namespacedName(gateway), role)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if !k8serrors.IsNotFound(err) {
		// Ensure we own the Role.
		for _, ref := range role.GetOwnerReferences() {
			if ref.UID == gateway.GetUID() && ref.Name == gateway.GetName() {
				// We found ourselves!
				return nil
			}
		}
		return errors.New("role not owned by controller")
	}

	role = g.role(gateway, gcc)
	if err := ctrl.SetControllerReference(&gateway, role, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, role); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deleteRole(ctx context.Context, gwName types.NamespacedName) error {
	if err := g.Client.Delete(ctx, &rbac.Role{ObjectMeta: metav1.ObjectMeta{Name: gwName.Name, Namespace: gwName.Namespace}}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) role(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig) *rbac.Role {
	role := &rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    common.LabelsForGateway(&gateway),
		},
		Rules: []rbac.PolicyRule{},
	}

	if gcc.Spec.PodSecurityPolicy != "" {
		role.Rules = append(role.Rules, rbac.PolicyRule{
			APIGroups:     []string{"policy"},
			Resources:     []string{"podsecuritypolicies"},
			ResourceNames: []string{gcc.Spec.PodSecurityPolicy},
			Verbs:         []string{"use"},
		})
	}

	return role
}

func (g *Gatekeeper) upsertOpenshiftRole(ctx context.Context, gateway gwv1beta1.Gateway, config common.HelmConfig) error {
	role := &rbac.Role{}

	openshiftRoleName := getOpenshiftName(gateway)

	// If the Role already exists, ensure that we own the Role
	err := g.Client.Get(ctx, types.NamespacedName{
		Namespace: gateway.Namespace,
		Name:      openshiftRoleName,
	}, role)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if !k8serrors.IsNotFound(err) {
		// Ensure we own the Role.
		for _, ref := range role.GetOwnerReferences() {
			if ref.UID == gateway.GetUID() && ref.Name == gateway.Name {
				// We found ourselves!
				g.Log.Info("We own the role")

				return nil
			}

		}
		return errors.New("role not owned by controller")
	}

	role = g.openshiftRole(gateway, openshiftRoleName, config)
	if err := ctrl.SetControllerReference(&gateway, role, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, role); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) openshiftRole(gateway gwv1beta1.Gateway, roleName string, config common.HelmConfig) *rbac.Role {
	role := &rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: gateway.Namespace,
			Labels:    common.LabelsForGateway(&gateway),
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{"security.openshift.io"},
				Resources: []string{"securitycontextconstraints"},
				// TODO(nathancoleman) Consider accepting an explicit SCC name. This will make the code
				//   here less brittle and allow for the user to provide their own SCC if they wish.
				ResourceNames: []string{config.ReleaseName + "-api-gateway"},
				Verbs:         []string{"use"},
			},
		},
	}

	return role
}
