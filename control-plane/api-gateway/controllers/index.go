package controllers

import (
	"context"
	"fmt"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	// Naming convention: TARGET_REFERENCE.
	GatewayClass_GatewayClassConfigIndex = "__gatewayclass_referencing_gatewayclassconfig"
	Gateway_GatewayClassIndex            = "__gateway_referencing_gatewayclass"
	HTTPRoute_GatewayIndex               = "__httproute_referencing_gateway"
	HTTPRoute_ServiceIndex               = "__httproute_referencing_service"
	TCPRoute_GatewayIndex                = "__tcproute_referencing_gateway"
	TCPRoute_ServiceIndex                = "__tcproute_referencing_service"
	Secret_GatewayIndex                  = "__secret_referencing_gateway"
)

// RegisterFieldIndexes registers all of the field indexes for the API gateway controllers.
// These indexes are similar to indexes used in databases to speed up queries.
// They allow us to quickly find objects based on a field value.
func RegisterFieldIndexes(ctx context.Context, mgr ctrl.Manager) error {
	for _, index := range indexes {
		fmt.Println("############Index: ", index)
		if err := mgr.GetFieldIndexer().IndexField(ctx, index.target, index.name, index.indexerFunc); err != nil {
			fmt.Println("RRRrrrrrrrrrrrrrrrrrrrrrrrRegistering Index ERror")
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
		name:        GatewayClass_GatewayClassConfigIndex,
		target:      &gwv1beta1.GatewayClass{},
		indexerFunc: gatewayClassConfigForGatewayClass,
	},
	{
		name:        Gateway_GatewayClassIndex,
		target:      &gwv1beta1.Gateway{},
		indexerFunc: gatewayClassForGateway,
	},
	{
		name:        HTTPRoute_GatewayIndex,
		target:      &gwv1beta1.HTTPRoute{},
		indexerFunc: gatewayForHTTPRoute,
	},
	{
		name:        TCPRoute_GatewayIndex,
		target:      &gwv1alpha2.TCPRoute{},
		indexerFunc: gatewayForTCPRoute,
	},
}

// gatewayClassConfigForGatewayClass creates an index of every GatewayClassConfig referenced by a GatewayClass.
func gatewayClassConfigForGatewayClass(o client.Object) []string {
	gc := o.(*gwv1beta1.GatewayClass)

	pr := gc.Spec.ParametersRef
	if pr != nil && pr.Kind == v1alpha1.GatewayClassConfigKind {
		return []string{pr.Name}
	}

	return []string{}
}

// gatewayClassForGateway creates an index of every GatewayClass referenced by a Gateway.
func gatewayClassForGateway(o client.Object) []string {
	g := o.(*gwv1beta1.Gateway)
	return []string{string(g.Spec.GatewayClassName)}
}

// gatewayForHTTPRoute creates an index of every Gateway referenced by an HTTPRoute.
func gatewayForHTTPRoute(o client.Object) []string {
	g := o.(*gwv1beta1.HTTPRoute)
	parents := make([]string, 0, len(g.Spec.ParentRefs))
	for _, p := range g.Spec.ParentRefs {
		parents = append(parents, string(p.Name))
	}
	return parents
}

// gatewayForTCPRoute creates an index of every Gateway referenced by a TCPRoute.
func gatewayForTCPRoute(o client.Object) []string {
	g := o.(*gwv1alpha2.TCPRoute)
	parents := make([]string, 0, len(g.Spec.ParentRefs))
	for _, p := range g.Spec.ParentRefs {
		parents = append(parents, string(p.Name))
	}
	return parents
}
