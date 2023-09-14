// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	// Naming convention: TARGET_REFERENCE.
	GatewayClass_GatewayClassConfigIndex = "__gatewayclass_referencing_gatewayclassconfig"
	GatewayClass_ControllerNameIndex     = "__gatewayclass_controller_name"

	Gateway_GatewayClassIndex = "__gateway_referencing_gatewayclass"

	HTTPRoute_GatewayIndex            = "__httproute_referencing_gateway"
	HTTPRoute_ServiceIndex            = "__httproute_referencing_service"
	HTTPRoute_MeshServiceIndex        = "__httproute_referencing_mesh_service"
	HTTPRoute_RouteRetryFilterIndex   = "__httproute_referencing_retryfilter"
	HTTPRoute_RouteTimeoutFilterIndex = "__httproute_referencing_timeoutfilter"
	HTTPRoute_RouteAuthFilterIndex    = "__httproute_referencing_routeauthfilter"

	TCPRoute_GatewayIndex     = "__tcproute_referencing_gateway"
	TCPRoute_ServiceIndex     = "__tcproute_referencing_service"
	TCPRoute_MeshServiceIndex = "__tcproute_referencing_mesh_service"

	MeshService_PeerIndex      = "__meshservice_referencing_peer"
	Secret_GatewayIndex        = "__secret_referencing_gateway"
	Gatewaypolicy_GatewayIndex = "__gatewaypolicy_referencing_gateway"
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
		name:        GatewayClass_ControllerNameIndex,
		target:      &gwv1beta1.GatewayClass{},
		indexerFunc: gatewayClassControllerName,
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
	{
		name:        HTTPRoute_GatewayIndex,
		target:      &gwv1beta1.HTTPRoute{},
		indexerFunc: gatewaysForHTTPRoute,
	},
	{
		name:        HTTPRoute_ServiceIndex,
		target:      &gwv1beta1.HTTPRoute{},
		indexerFunc: servicesForHTTPRoute,
	},
	{
		name:        HTTPRoute_MeshServiceIndex,
		target:      &gwv1beta1.HTTPRoute{},
		indexerFunc: meshServicesForHTTPRoute,
	},
	{
		name:        TCPRoute_GatewayIndex,
		target:      &gwv1alpha2.TCPRoute{},
		indexerFunc: gatewaysForTCPRoute,
	},
	{
		name:        TCPRoute_ServiceIndex,
		target:      &gwv1alpha2.TCPRoute{},
		indexerFunc: servicesForTCPRoute,
	},
	{
		name:        TCPRoute_MeshServiceIndex,
		target:      &gwv1alpha2.TCPRoute{},
		indexerFunc: meshServicesForTCPRoute,
	},
	{
		name:        MeshService_PeerIndex,
		target:      &v1alpha1.MeshService{},
		indexerFunc: peersForMeshService,
	},
	{
		name:        HTTPRoute_RouteRetryFilterIndex,
		target:      &gwv1beta1.HTTPRoute{},
		indexerFunc: filtersForHTTPRoute,
	},
	{
		name:        HTTPRoute_RouteTimeoutFilterIndex,
		target:      &gwv1beta1.HTTPRoute{},
		indexerFunc: filtersForHTTPRoute,
	},
	{
		name:        HTTPRoute_RouteAuthFilterIndex,
		target:      &gwv1beta1.HTTPRoute{},
		indexerFunc: filtersForHTTPRoute,
	},
	{
		name:        Gatewaypolicy_GatewayIndex,
		target:      &v1alpha1.GatewayPolicy{},
		indexerFunc: gatewayForGatewayPolicy,
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

func gatewayClassControllerName(o client.Object) []string {
	gc := o.(*gwv1beta1.GatewayClass)

	if gc.Spec.ControllerName != "" {
		return []string{string(gc.Spec.ControllerName)}
	}

	return []string{}
}

// gatewayClassForGateway creates an index of every GatewayClass referenced by a Gateway.
func gatewayClassForGateway(o client.Object) []string {
	g := o.(*gwv1beta1.Gateway)
	return []string{string(g.Spec.GatewayClassName)}
}

func peersForMeshService(o client.Object) []string {
	m := o.(*v1alpha1.MeshService)
	if m.Spec.Peer != nil {
		return []string{string(*m.Spec.Peer)}
	}
	return nil
}

func gatewayForSecret(o client.Object) []string {
	gateway := o.(*gwv1beta1.Gateway)
	var secretReferences []string
	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil || *listener.TLS.Mode != gwv1beta1.TLSModeTerminate {
			continue
		}
		for _, cert := range listener.TLS.CertificateRefs {
			if common.NilOrEqual(cert.Group, "") && common.NilOrEqual(cert.Kind, "Secret") {
				// If an explicit Secret namespace is not provided, use the Gateway namespace to lookup the provided Secret Name.
				secretReferences = append(secretReferences, common.IndexedNamespacedNameWithDefault(cert.Name, cert.Namespace, gateway.Namespace).String())
			}
		}
	}
	return secretReferences
}

func gatewaysForHTTPRoute(o client.Object) []string {
	route := o.(*gwv1beta1.HTTPRoute)
	statusRefs := common.ConvertSliceFunc(route.Status.Parents, func(parentStatus gwv1beta1.RouteParentStatus) gwv1beta1.ParentReference {
		return parentStatus.ParentRef
	})
	return gatewaysForRoute(route.Namespace, route.Spec.ParentRefs, statusRefs)
}

func gatewaysForTCPRoute(o client.Object) []string {
	route := o.(*gwv1alpha2.TCPRoute)
	statusRefs := common.ConvertSliceFunc(route.Status.Parents, func(parentStatus gwv1beta1.RouteParentStatus) gwv1beta1.ParentReference {
		return parentStatus.ParentRef
	})
	return gatewaysForRoute(route.Namespace, route.Spec.ParentRefs, statusRefs)
}

func servicesForHTTPRoute(o client.Object) []string {
	route := o.(*gwv1beta1.HTTPRoute)
	refs := []string{}
	for _, rule := range route.Spec.Rules {
	BACKEND_LOOP:
		for _, ref := range rule.BackendRefs {
			if common.NilOrEqual(ref.Group, "") && common.NilOrEqual(ref.Kind, "Service") {
				backendRef := common.IndexedNamespacedNameWithDefault(ref.Name, ref.Namespace, route.Namespace).String()
				for _, member := range refs {
					if member == backendRef {
						continue BACKEND_LOOP
					}
				}
				refs = append(refs, backendRef)
			}
		}
	}
	return refs
}

func meshServicesForHTTPRoute(o client.Object) []string {
	route := o.(*gwv1beta1.HTTPRoute)
	refs := []string{}
	for _, rule := range route.Spec.Rules {
	BACKEND_LOOP:
		for _, ref := range rule.BackendRefs {
			if common.DerefEqual(ref.Group, v1alpha1.ConsulHashicorpGroup) && common.DerefEqual(ref.Kind, v1alpha1.MeshServiceKind) {
				backendRef := common.IndexedNamespacedNameWithDefault(ref.Name, ref.Namespace, route.Namespace).String()
				for _, member := range refs {
					if member == backendRef {
						continue BACKEND_LOOP
					}
				}
				refs = append(refs, backendRef)
			}
		}
	}
	return refs
}

func servicesForTCPRoute(o client.Object) []string {
	route := o.(*gwv1alpha2.TCPRoute)
	refs := []string{}
	for _, rule := range route.Spec.Rules {
	BACKEND_LOOP:
		for _, ref := range rule.BackendRefs {
			if common.NilOrEqual(ref.Group, "") && common.NilOrEqual(ref.Kind, common.KindService) {
				backendRef := common.IndexedNamespacedNameWithDefault(ref.Name, ref.Namespace, route.Namespace).String()
				for _, member := range refs {
					if member == backendRef {
						continue BACKEND_LOOP
					}
				}
				refs = append(refs, backendRef)
			}
		}
	}
	return refs
}

func meshServicesForTCPRoute(o client.Object) []string {
	route := o.(*gwv1alpha2.TCPRoute)
	refs := []string{}
	for _, rule := range route.Spec.Rules {
	BACKEND_LOOP:
		for _, ref := range rule.BackendRefs {
			if common.DerefEqual(ref.Group, v1alpha1.ConsulHashicorpGroup) && common.DerefEqual(ref.Kind, v1alpha1.MeshServiceKind) {
				backendRef := common.IndexedNamespacedNameWithDefault(ref.Name, ref.Namespace, route.Namespace).String()
				for _, member := range refs {
					if member == backendRef {
						continue BACKEND_LOOP
					}
				}
				refs = append(refs, backendRef)
			}
		}
	}
	return refs
}

func gatewaysForRoute(namespace string, refs []gwv1beta1.ParentReference, statusRefs []gwv1beta1.ParentReference) []string {
	var references []string
	for _, parent := range refs {
		if common.NilOrEqual(parent.Group, common.BetaGroup) && common.NilOrEqual(parent.Kind, common.KindGateway) {
			// If an explicit Gateway namespace is not provided, use the Route namespace to lookup the provided Gateway Namespace.
			references = append(references, common.IndexedNamespacedNameWithDefault(parent.Name, parent.Namespace, namespace).String())
		}
	}
	for _, parent := range statusRefs {
		if common.NilOrEqual(parent.Group, common.BetaGroup) && common.NilOrEqual(parent.Kind, common.KindGateway) {
			// If an explicit Gateway namespace is not provided, use the Route namespace to lookup the provided Gateway Namespace.
			references = append(references, common.IndexedNamespacedNameWithDefault(parent.Name, parent.Namespace, namespace).String())
		}
	}
	return references
}

func filtersForHTTPRoute(o client.Object) []string {
	route := o.(*gwv1beta1.HTTPRoute)
	filters := []string{}
	var nilString *string

	for _, rule := range route.Spec.Rules {
	FILTERS_LOOP:
		for _, filter := range rule.Filters {
			if common.FilterIsExternalFilter(filter) {
				// TODO this seems like its type agnostic, so this might just work without having to make
				// multiple index functions per custom filter type?

				// index external filters
				filter := common.IndexedNamespacedNameWithDefault(string(filter.ExtensionRef.Name), nilString, route.Namespace).String()
				for _, member := range filters {
					if member == filter {
						continue FILTERS_LOOP
					}
				}
				filters = append(filters, filter)
			}
		}

		// same thing but over the backend refs
	BACKEND_LOOP:
		for _, ref := range rule.BackendRefs {
			for _, filter := range ref.Filters {
				if common.FilterIsExternalFilter(filter) {
					filter := common.IndexedNamespacedNameWithDefault(string(filter.ExtensionRef.Name), nilString, route.Namespace).String()
					for _, member := range filters {
						if member == filter {
							continue BACKEND_LOOP
						}
					}
					filters = append(filters, filter)
				}
			}
		}
	}
	return filters
}

func gatewayForGatewayPolicy(o client.Object) []string {
	gatewayPolicy := o.(*v1alpha1.GatewayPolicy)

	targetGateway := gatewayPolicy.Spec.TargetRef
	if targetGateway.Group == gwv1beta1.GroupVersion.String() && targetGateway.Kind == common.KindGateway {
		policyNamespace := gatewayPolicy.Namespace
		if policyNamespace == "" {
			policyNamespace = "default"
		}
		targetNS := targetGateway.Namespace
		if targetNS == "" {
			targetNS = policyNamespace
		}

		namespacedName := types.NamespacedName{Name: targetGateway.Name, Namespace: targetNS}
		return []string{namespacedName.String()}
	}

	return []string{}
}
