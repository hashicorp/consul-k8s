package gatekeeper

import (
	"context"

	"github.com/go-logr/logr"
	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Gatekeeper is used to manage the lifecycle of Gateway deployments and services.
type Gatekeeper struct {
	Config
}

// Config is the configuration for the Gatekeeper.
type Config struct {
	Log    logr.Logger
	Client client.Client

	Gateway            gwv1beta1.Gateway
	GatewayClassConfig v1alpha1.GatewayClassConfig
	HelmConfig         apigateway.HelmConfig
}

// New creates a new Gatekeeper from the Config.
func New(cfg Config) *Gatekeeper {
	return &Gatekeeper{cfg}
}

func (g *Gatekeeper) Upsert(ctx context.Context) error {
	if g.HelmConfig.ConsulNamespaceMirroring {
		// TODO ensure namespace exists
	}

	if err := g.upsertServiceAccount(ctx); err != nil {
		return err
	}

	if err := g.upsertDeployment(ctx); err != nil {
		return err
	}

	if err := g.upsertService(ctx); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) Delete(ctx context.Context) error {
	if err := g.deleteServiceAccount(ctx); err != nil {
		return err
	}

	return nil
}

func (g Gatekeeper) namespacedName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: g.Gateway.Namespace,
		Name:      g.Gateway.Name,
	}
}
