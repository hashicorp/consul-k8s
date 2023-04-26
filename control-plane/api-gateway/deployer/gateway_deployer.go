package deployer

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type GatewayDeployer struct {
	client.Client
}

func (d *GatewayDeployer) Create(ctx context.Context, gateway Gateway) error {
	err := d.Client.Create(ctx, gateway.Deployment())
	if err != nil {
		return err
	}

	err = d.Client.Create(ctx, gateway.Service())
	if err != nil {
		return err
	}

	return nil
}

func (d *GatewayDeployer) Delete(ctx context.Context, namespacedName types.NamespacedName) error {

	return nil
}
