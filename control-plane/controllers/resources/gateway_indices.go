// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

type indexName string

const (
	// Naming convention: TARGET_REFERENCE.
	GatewayClass_GatewayClassConfigIndex indexName = "__v2_gatewayclass_referencing_gatewayclassconfig"

	APIGateway_GatewayClassIndex  indexName = "__v2_api_gateway_referencing_gatewayclass"
	MeshGateway_GatewayClassIndex indexName = "__v2_mesh_gateway_referencing_gatewayclass"
)

// RegisterGatewayFieldIndexes registers all of the field indexes for the xGateway controllers.
// These indexes are similar to indexes used in databases to speed up queries.
// They allow us to quickly find objects based on a field value.
func RegisterGatewayFieldIndexes(ctx context.Context, mgr ctrl.Manager) error {
	for _, index := range indexes {
		if err := mgr.GetFieldIndexer().IndexField(ctx, index.target, string(index.name), index.indexerFunc); err != nil {
			return err
		}
	}
	return nil
}

type index struct {
	name        indexName
	target      client.Object
	indexerFunc client.IndexerFunc
}

var indexes = []index{
	{
		name:   GatewayClass_GatewayClassConfigIndex,
		target: &meshv2beta1.GatewayClass{},
		indexerFunc: func(o client.Object) []string {
			gc := o.(*meshv2beta1.GatewayClass)

			pr := gc.Spec.ParametersRef
			if pr != nil && pr.Kind == v2beta1.KindGatewayClassConfig {
				return []string{pr.Name}
			}

			return []string{}
		},
	},
	{
		name:   APIGateway_GatewayClassIndex,
		target: &meshv2beta1.APIGateway{},
		indexerFunc: func(o client.Object) []string {
			g := o.(*meshv2beta1.APIGateway)
			return []string{string(g.Spec.GatewayClassName)}
		},
	},
	{
		name:   MeshGateway_GatewayClassIndex,
		target: &meshv2beta1.MeshGateway{},
		indexerFunc: func(o client.Object) []string {
			g := o.(*meshv2beta1.MeshGateway)
			return []string{string(g.Spec.GatewayClassName)}
		},
	},
}
