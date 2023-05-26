// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
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
func (g *Gatekeeper) Upsert(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config apigateway.HelmConfig) error {
	g.Log.Info(fmt.Sprintf("Upsert Gateway Deployment %s/%s", gateway.Namespace, gateway.Name))

	if err := g.upsertRole(ctx, gateway, gcc, config); err != nil {
		return err
	}

	if err := g.upsertServiceAccount(ctx, gateway, config); err != nil {
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
func (g *Gatekeeper) Delete(ctx context.Context, nsname types.NamespacedName) error {
	if err := g.deleteRole(ctx, nsname); err != nil {
		return err
	}

	if err := g.deleteServiceAccount(ctx, nsname); err != nil {
		return err
	}

	if err := g.deleteService(ctx, nsname); err != nil {
		return err
	}

	if err := g.deleteDeployment(ctx, nsname); err != nil {
		return err
	}

	return nil
}

// resourceMutator is passed to create or update functions to mutate Kubernetes resources.
type resourceMutator = func() error

func (g Gatekeeper) namespacedName(gateway gwv1beta1.Gateway) types.NamespacedName {
	return types.NamespacedName{
		Namespace: gateway.Namespace,
		Name:      gateway.Name,
	}
}

func (g Gatekeeper) serviceAccountName(gateway gwv1beta1.Gateway, config apigateway.HelmConfig) string {
	if config.AuthMethod == "" {
		return ""
	}
	return gateway.Name
}
