// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (b *gatewayBuilder[T]) Role() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.gateway.GetName(),
			Namespace:   b.gateway.GetNamespace(),
			Labels:      b.labelsForRole(),
			Annotations: b.annotationsForRole(),
		},
		Rules: []rbacv1.PolicyRule{},
	}
}

func (b *gatewayBuilder[T]) RoleBinding() *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.gateway.GetName(),
			Namespace:   b.gateway.GetNamespace(),
			Labels:      b.labelsForRoleBinding(),
			Annotations: b.annotationsForRoleBinding(),
		},
		Subjects: []rbacv1.Subject{
			{
				APIGroup:  "",
				Kind:      rbacv1.ServiceAccountKind,
				Name:      b.gateway.GetName(),
				Namespace: b.gateway.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     b.Role().Name,
		},
	}
}
