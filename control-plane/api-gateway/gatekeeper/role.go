package gatekeeper

import (
	"context"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	rbac "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (g *Gatekeeper) upsertRole(ctx context.Context) error {
	if !g.HelmConfig.ManageSystemACLs {
		return nil
	}

	// TODO check and do upsert

	err := g.Client.Create(ctx, g.role())
	if err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deleteRole(ctx context.Context) error {
	if err := g.Client.Delete(ctx, g.role()); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) role() *rbac.Role {
	return &rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.Gateway.Name,
			Namespace: g.Gateway.Namespace,
			Labels:    apigateway.LabelsForGateway(&g.Gateway),
		},
		Rules: []rbac.PolicyRule{{
			APIGroups: []string{"policy"},
			Resources: []string{"podsecuritypolicies"},
			// TODO figure out how to bring this in. Maybe GWCCFG
			// ResourceNames: []string{c.Spec.ConsulSpec.AuthSpec.PodSecurityPolicy},
			Verbs: []string{"use"},
		}},
	}
}
