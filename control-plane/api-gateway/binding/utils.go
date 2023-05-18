package binding

import (
	"reflect"

	"github.com/hashicorp/consul/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func bothNilOrEqual[T comparable](one, two *T) bool {
	if one == nil && two == nil {
		return true
	}
	if one == nil {
		return false
	}
	if two == nil {
		return false
	}
	return *one == *two
}

func toNamespaceSet(name string, labels map[string]string) klabels.Labels {
	// If namespace label is not set, implicitly insert it to support older Kubernetes versions
	if labels[NamespaceNameLabel] == name {
		// Already set, avoid copies
		return klabels.Set(labels)
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[NamespaceNameLabel] = name
	return klabels.Set(ret)
}

func valueOr[T ~string](v *T, fallback string) string {
	if v == nil {
		return fallback
	}
	return string(*v)
}

func nilOrEqual[T ~string](v *T, check string) bool {
	return v == nil || string(*v) == check
}

func filterParentRefs(gateway types.NamespacedName, namespace string, refs []gwv1beta1.ParentReference) []gwv1beta1.ParentReference {
	references := []gwv1beta1.ParentReference{}
	for _, ref := range refs {
		if nilOrEqual(ref.Group, betaGroup) &&
			nilOrEqual(ref.Kind, kindGateway) &&
			gateway.Namespace == valueOr(ref.Namespace, namespace) &&
			gateway.Name == string(ref.Name) {
			references = append(references, ref)
		}
	}

	return references
}

func stringPointer[T ~string](v T) *string {
	x := string(v)
	return &x
}

func objectsToMeta[T metav1.Object](objects []T) []types.NamespacedName {
	var meta []types.NamespacedName
	for _, object := range objects {
		meta = append(meta, types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		})
	}
	return meta
}

func objectToMeta[T metav1.Object](object T) types.NamespacedName {
	return types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
}

func isDeleted(object client.Object) bool {
	return !object.GetDeletionTimestamp().IsZero()
}

func ensureFinalizer(object client.Object) bool {
	if !object.GetDeletionTimestamp().IsZero() {
		return false
	}

	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == gatewayFinalizer {
			return false
		}
	}

	object.SetFinalizers(append(finalizers, gatewayFinalizer))
	return true
}

func removeFinalizer(object client.Object) bool {
	found := false
	filtered := []string{}
	for _, f := range object.GetFinalizers() {
		if f == gatewayFinalizer {
			found = true
			continue
		}
		filtered = append(filtered, f)
	}

	object.SetFinalizers(filtered)
	return found
}

func pointerTo[T any](v T) *T {
	return &v
}

func isNil(arg interface{}) bool {
	return arg == nil || reflect.ValueOf(arg).IsNil()
}

func routeMatchesListener(listenerName gwv1beta1.SectionName, routeSectionName *gwv1alpha2.SectionName) (can bool, must bool) {
	if routeSectionName == nil {
		return true, false
	}
	return string(listenerName) == string(*routeSectionName), true
}

func listenersFor(gateway *gwv1beta1.Gateway, name *gwv1beta1.SectionName) []gwv1beta1.Listener {
	listeners := []gwv1beta1.Listener{}
	for _, listener := range gateway.Spec.Listeners {
		if name == nil {
			listeners = append(listeners, listener)
			continue
		}
		if listener.Name == *name {
			listeners = append(listeners, listener)
		}
	}
	return listeners
}

func serviceMap(services []api.CatalogService) map[types.NamespacedName]api.CatalogService {
	smap := make(map[types.NamespacedName]api.CatalogService)
	for _, service := range services {
		smap[serviceToNamespacedName(&service)] = service
	}
	return smap
}

func serviceToNamespacedName(s *api.CatalogService) types.NamespacedName {
	var (
		metaKeyKubeNS          = "k8s-namespace"
		metaKeyKubeServiceName = "k8s-service-name"
	)
	return types.NamespacedName{
		Namespace: s.ServiceMeta[metaKeyKubeNS],
		Name:      s.ServiceMeta[metaKeyKubeServiceName],
	}
}
