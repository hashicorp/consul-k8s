package management

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Gatekeeper is used to manage the lifecycle of Gateway deployments and services.
type Gatekeeper struct {
	Log logr.Logger
	client.Client
}

func NewGatekeeper(client client.Client, log logr.Logger) *Gatekeeper {
	return &Gatekeeper{
		Log:    log,
		Client: client,
	}
}

func (g *Gatekeeper) UpsertGateway(ctx context.Context, gateway *Gateway) error {
	return nil
}

func (g *Gatekeeper) DeleteGateway(ctx context.Context, namespacedName types.NamespacedName) error {
	return nil
}
