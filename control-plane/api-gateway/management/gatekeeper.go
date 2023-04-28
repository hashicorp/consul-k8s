package management

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Gatekeeper struct {
	client.Client
}

func (g *Gatekeeper) CreateGateway(ctx context.Context, gateway Gateway) error {
	return nil
}

func (g *Gatekeeper) GetGateway(ctx context.Context, namespacedName types.NamespacedName) (Gateway, error) {
	return Gateway{}, nil
}

func (g *Gateway) UpdateGateway(ctx context.Context, old, gateway Gateway) error {
	return nil
}

func (g *Gatekeeper) DeleteGateway(ctx context.Context, namespacedName types.NamespacedName) error {
	return nil
}
