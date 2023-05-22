package gatekeeper

import (
	"context"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	rbac "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (g *Gatekeeper) upsertRole(ctx context.Context, gateway gwv1beta1.Gateway, config apigateway.HelmConfig) error {
	if !config.ManageSystemACLs {
		return nil
	}

	role := &rbac.Role{}
	exists := false

	// Get ServiceAccount
	err := g.Client.Get(ctx, g.namespacedName(gateway), role)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if k8serrors.IsNotFound(err) {
		exists = false
	} else {
		exists = true
	}

	if exists {
		// Ensure we own the Role.
		for _, ref := range role.GetOwnerReferences() {
			if ref.UID == gateway.GetUID() && ref.Name == gateway.GetName() {
				// We found ourselves!
				return nil
			}
		}
		return errors.New("Role not owned by controller")
	}

	role = g.role(gateway)
	if err := ctrl.SetControllerReference(&gateway, role, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, role); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deleteRole(ctx context.Context, nsname types.NamespacedName) error {
	if err := g.Client.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: nsname.Name, Namespace: nsname.Namespace}}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) role(gateway gwv1beta1.Gateway) *rbac.Role {
	return &rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    apigateway.LabelsForGateway(&gateway),
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{"policy"},
				Resources: []string{"podsecuritypolicies"},
				// TODO figure out how to bring this in. Maybe GWCCFG
				// ResourceNames: []string{c.Spec.ConsulSpec.AuthSpec.PodSecurityPolicy},
				Verbs: []string{"use"},
			},
			{
				APIGroups:     []string{"security.openshift.io"},
				Resources:     []string{"securitycontextconstraints"},
				ResourceNames: []string{"name-of-the-security-context-constraints"},
				Verbs:         []string{"use"},
			},
		},
	}
}
