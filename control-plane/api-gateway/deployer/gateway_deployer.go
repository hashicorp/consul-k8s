package deployer

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type GatewayDeployer struct {
	client.Client
}

func NewGatewayDeployer(client client.Client) *GatewayDeployer {
	return &GatewayDeployer{
		Client: client,
	}
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
	err := d.Client.Delete(ctx, &appsv1.Deployment{ObjectMeta: v1.ObjectMeta{
		Namespace: namespacedName.Namespace,
		Name:      namespacedName.Name,
	}})
	if err != nil {
		return err
	}

	err = d.Client.Delete(ctx, &corev1.Service{ObjectMeta: v1.ObjectMeta{
		Namespace: namespacedName.Namespace,
		Name:      namespacedName.Name,
	}})
	if err != nil {
		return err
	}

	return nil
}
