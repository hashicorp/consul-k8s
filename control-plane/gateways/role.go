// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (b *meshGatewayBuilder) Role() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.gateway.Name,
			Namespace: b.gateway.Namespace,
			Labels:    b.Labels(),
		},
		Rules: []rbacv1.PolicyRule{},
	}
}

func (b *meshGatewayBuilder) RoleBinding() *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.gateway.Name,
			Namespace: b.gateway.Namespace,
			Labels:    b.Labels(),
		},
		Subjects: []rbacv1.Subject{
			{
				APIGroup:  "",
				Kind:      rbacv1.ServiceAccountKind,
				Name:      b.gateway.Name,
				Namespace: b.gateway.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     b.Role().Name,
		},
	}
}
