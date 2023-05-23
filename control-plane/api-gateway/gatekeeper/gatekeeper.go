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

	Gateway            gwv1beta1.Gateway
	GatewayClassConfig v1alpha1.GatewayClassConfig
	HelmConfig         apigateway.HelmConfig
}

// New creates a new Gatekeeper from the Config.
func New(log logr.Logger, client client.Client, gateway gwv1beta1.Gateway, gatewayClassConfig v1alpha1.GatewayClassConfig, helmConfig apigateway.HelmConfig) *Gatekeeper {
	return &Gatekeeper{
		Log:                log,
		Client:             client,
		Gateway:            gateway,
		GatewayClassConfig: gatewayClassConfig,
		HelmConfig:         helmConfig,
	}
}

// Upsert creates or updates the resources for handling routing of network traffic.
func (g *Gatekeeper) Upsert(ctx context.Context) error {
	g.Log.Info(fmt.Sprintf("Upsert Gateway Deployment %s/%s", g.Gateway.Namespace, g.Gateway.Name))

	if err := g.upsertRole(ctx); err != nil {
		return err
	}

	if err := g.upsertServiceAccount(ctx); err != nil {
		return err
	}

	if err := g.upsertService(ctx); err != nil {
		return err
	}

	if err := g.upsertDeployment(ctx); err != nil {
		return err
	}

	return nil
}

// Delete removes the resources for handling routing of network traffic.
func (g *Gatekeeper) Delete(ctx context.Context) error {
	if err := g.deleteRole(ctx); err != nil {
		return err
	}

	if err := g.deleteServiceAccount(ctx); err != nil {
		return err
	}

	if err := g.deleteService(ctx); err != nil {
		return err
	}

	if err := g.deleteDeployment(ctx); err != nil {
		return err
	}

	return nil
}

// resourceMutator is passed to create or update functions to mutate Kubernetes resources.
type resourceMutator = func() error

func (g Gatekeeper) namespacedName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: g.Gateway.Namespace,
		Name:      g.Gateway.Name,
	}
}

func (g Gatekeeper) serviceAccountName() string {
	authspecaccount := "" // TODO do I need to add this to GatewayClassConfig?
	fmt.Println(authspecaccount)

	return ""
}
