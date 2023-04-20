package controllers

import (
	"context"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	GatewayClassConfigFieldIndex = "__gatewayclassconfig"
	GatewayClassFieldIndex       = "__gatewayclass"
)

// RegisterFieldIndexes registers all of the field indexes for the API gateway controllers.
// These indexes are similar to indexes used in databases to speed up queries.
// They allow us to quickly find objects based on a field value.
func RegisterFieldIndexes(ctx context.Context, mgr ctrl.Manager) error {
	for _, index := range indexes {
		if err := mgr.GetFieldIndexer().IndexField(ctx, index.target, index.name, index.indexerFunc); err != nil {
			return err
		}
	}
	return nil
}

type index struct {
	name        string
	target      client.Object
	indexerFunc client.IndexerFunc
}

var indexes = []index{
	{
		name:        GatewayClassConfigFieldIndex,
		target:      &v1alpha1.GatewayClassConfig{},
		indexerFunc: gatewayClassConfigIndexerFunc,
	},
	{
		name:        GatewayClassFieldIndex,
		target:      &gwv1beta1.GatewayClass{},
		indexerFunc: gatewayClassIndexerFunc,
	},
}

// gatewayClassConfigIndexerFunc creates an index of every GatewayClass
// that references the GatewayClassConfig.
func gatewayClassConfigIndexerFunc(o client.Object) []string {
	gc := o.(*gwv1beta1.GatewayClass)

	pr := gc.Spec.ParametersRef
	if pr != nil && pr.Kind == v1alpha1.GatewayClassConfigKind {
		return []string{pr.Name}
	}

	return []string{}
}

// gatewayClassIndexerFunc creates an index of every GatewayClass
func gatewayClassIndexerFunc(o client.Object) []string {
	g := o.(*gwv1beta1.Gateway)
	return []string{string(g.Spec.GatewayClassName)}
}
