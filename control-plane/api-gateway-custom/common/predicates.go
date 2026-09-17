// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// consulAnnotationSuffix matches every Consul-owned annotation that can change
// how a Gateway is translated into Consul config entries (for example
// AnnotationTLSEnabled and AnnotationExtAuthz).
//
// A substring match is deliberate: these keys appear both bare
// ("consul.hashicorp.com/tls-enabled") and domain-prefixed
// ("api-gateway.consul.hashicorp.com/tls_min_version",
// "api-gateway-custom.consul.hashicorp.com/tls_min_version"), so a plain
// prefix check would silently miss the listener-scoped ones.
const consulAnnotationSuffix = "consul.hashicorp.com/"

// hasConsulAnnotation reports whether the object carries any Consul annotation.
func hasConsulAnnotation(obj client.Object) bool {
	if obj == nil {
		return false
	}
	for k := range obj.GetAnnotations() {
		if strings.Contains(k, consulAnnotationSuffix) {
			return true
		}
	}
	return false
}

// ConsulAnnotationPredicate admits objects that carry a Consul annotation.
//
// It exists so a Gateway watch can stay filtered on the generated-resource
// ComponentLabel while still reconciling user-authored Gateways, which never
// carry that label but do carry annotations such as
// consul.hashicorp.com/tls-enabled.
//
// Update events deliberately inspect BOTH the old and the new object. The
// motivating bug was annotation REMOVAL: after the annotation is deleted the
// new object has no Consul annotation at all, so testing only the new object
// would drop exactly the event that must trigger a reconcile to clear the
// corresponding Consul setting.
func ConsulAnnotationPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasConsulAnnotation(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasConsulAnnotation(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasConsulAnnotation(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasConsulAnnotation(e.ObjectOld) || hasConsulAnnotation(e.ObjectNew)
		},
	}
}
