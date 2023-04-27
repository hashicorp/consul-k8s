package controllers

import (
	"context"
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
		name:        Secret_GatewayIndex,
		target:      &gwv1beta1.Gateway{},
		indexerFunc: gatewayForSecret,
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

func gatewayForSecret(o client.Object) []string {
	gateway := o.(*gwv1alpha2.Gateway)
	var secretReferences []string
	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil || *listener.TLS.Mode != gwv1beta1.TLSModeTerminate {
			continue
		}
		for _, cert := range listener.TLS.CertificateRefs {
			if nilOrEqual(cert.Group, "") && nilOrEqual(cert.Kind, "Secret") {
				// If an explicit Secret namespace is not provided, use the Gateway namespace to lookup the provided Secret Name.
				secretReferences = append(secretReferences, indexedNamespacedNameWithDefault(cert.Name, cert.Namespace, gateway.Namespace).String())
			}
		}
	}
	return secretReferences
}
