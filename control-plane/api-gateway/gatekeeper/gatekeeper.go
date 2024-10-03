// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Gatekeeper is used to manage the lifecycle of Gateway deployments and services.
type Gatekeeper struct {
	Log    logr.Logger
	Client client.Client
}

// New creates a new Gatekeeper from the Config.
func New(log logr.Logger, client client.Client) *Gatekeeper {
	return &Gatekeeper{
		Log:    log,
		Client: client,
	}
}

// Upsert creates or updates the resources for handling routing of network traffic.
// This is done in order based on dependencies between resources.
func (g *Gatekeeper) Upsert(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) error {
	g.Log.V(1).Info(fmt.Sprintf("Upsert Gateway Deployment %s/%s", gateway.Namespace, gateway.Name))

	if err := g.upsertRole(ctx, gateway, gcc, config); err != nil {
		return err
	}

	if err := g.upsertServiceAccount(ctx, gateway, config); err != nil {
		return err
	}

	if err := g.upsertRoleBinding(ctx, gateway, gcc, config); err != nil {
		return err
	}

	if err := g.upsertService(ctx, gateway, gcc, config); err != nil {
		return err
	}

	if err := g.upsertDeployment(ctx, gateway, gcc, config); err != nil {
		return err
	}

	return nil
}

// Delete removes the resources for handling routing of network traffic.
// This is done in the reverse order of Upsert due to dependencies between resources.
func (g *Gatekeeper) Delete(ctx context.Context, gatewayName types.NamespacedName) error {
	g.Log.V(1).Info(fmt.Sprintf("Delete Gateway Deployment %s/%s", gatewayName.Namespace, gatewayName.Name))

	if err := g.deleteDeployment(ctx, gatewayName); err != nil {
		return err
	}

	if err := g.deleteService(ctx, gatewayName); err != nil {
		return err
	}

	if err := g.deleteRoleBinding(ctx, gatewayName); err != nil {
		return err
	}

	if err := g.deleteServiceAccount(ctx, gatewayName); err != nil {
		return err
	}

	if err := g.deleteRole(ctx, gatewayName); err != nil {
		return err
	}

	return nil
}

// resourceMutator is passed to create or update functions to mutate Kubernetes resources.
type resourceMutator = func() error

func (g *Gatekeeper) namespacedName(gateway gwv1beta1.Gateway) types.NamespacedName {
	return types.NamespacedName{
		Namespace: gateway.Namespace,
		Name:      gateway.Name,
	}
}

func (g *Gatekeeper) serviceAccountName(gateway gwv1beta1.Gateway, config common.HelmConfig) string {
	// We only create a ServiceAccount if it's needed for RBAC or image pull secrets;
	// otherwise, we clean up if one was previously created.
	if config.AuthMethod == "" && !config.EnableOpenShift && len(config.ImagePullSecrets) == 0 {
		return ""
	}
	return gateway.Name
}
