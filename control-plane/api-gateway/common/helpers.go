// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func DerefAll[T any](vs []*T) []T {
	e := make([]T, 0, len(vs))
	for _, v := range vs {
		e = append(e, *v)
	}
	return e
}

func EmptyOrEqual(v, check string) bool {
	return v == "" || v == check
}

func NilOrEqual[T ~string](v *T, check string) bool {
	return v == nil || string(*v) == check
}

func FilterIsExternalFilter(filter gwv1beta1.HTTPRouteFilter) bool {
	if filter.Type != gwv1beta1.HTTPRouteFilterExtensionRef {
		return false
	}

	if !DerefEqual(&filter.ExtensionRef.Group, v1alpha1.ConsulHashicorpGroup) {
		return false
	}

	switch filter.ExtensionRef.Kind {
	case v1alpha1.RouteRetryFilterKind, v1alpha1.RouteTimeoutFilterKind, v1alpha1.RouteAuthFilterKind:
		return true
	}

	return false

}

func IndexedNamespacedNameWithDefault[T ~string, U ~string, V ~string](t T, u *U, v V) types.NamespacedName {
	return types.NamespacedName{
		Namespace: DerefStringOr(u, v),
		Name:      string(t),
	}
}

func ResourceReferenceWithDefault[T ~string, U ~string, V ~string](kind string, name T, section string, u *U, v V, partition string) api.ResourceReference {
	return api.ResourceReference{
		Kind:        kind,
		Name:        string(name),
		SectionName: section,
		Namespace:   DerefStringOr(u, v),
		Partition:   partition,
	}
}

func DerefStringOr[T ~string, U ~string](v *T, val U) string {
	if v == nil {
		return string(val)
	}
	return string(*v)
}

func DerefLookup[T comparable, U any](v *T, lookup map[T]U) U {
	var zero U
	if v == nil {
		return zero
	}
	return lookup[*v]
}

func DerefConvertFunc[T any, U any](v *T, fn func(T) U) U {
	var zero U
	if v == nil {
		return zero
	}
	return fn(*v)
}

func DerefEqual[T ~string](v *T, check string) bool {
	if v == nil {
		return false
	}
	return string(*v) == check
}

func DerefIntOr[T ~int | ~int32, U ~int](v *T, val U) int {
	if v == nil {
		return int(val)
	}
	return int(*v)
}

func StringLikeSlice[T ~string](vs []T) []string {
	converted := []string{}
	for _, v := range vs {
		converted = append(converted, string(v))
	}
	return converted
}

func ConvertMapValuesToSlice[T comparable, U any](vs map[T]U) []U {
	converted := []U{}
	for _, v := range vs {
		converted = append(converted, v)
	}
	return converted
}

func ConvertSliceFunc[T any, U any](vs []T, fn func(T) U) []U {
	converted := []U{}
	for _, v := range vs {
		converted = append(converted, fn(v))
	}
	return converted
}

func ConvertSliceFuncIf[T any, U any](vs []T, fn func(T) (U, bool)) []U {
	converted := []U{}
	for _, v := range vs {
		if c, ok := fn(v); ok {
			converted = append(converted, c)
		}
	}
	return converted
}

func Flatten[T any](vs [][]T) []T {
	flattened := []T{}
	for _, v := range vs {
		flattened = append(flattened, v...)
	}
	return flattened
}

func Filter[T any](vs []T, filterFn func(T) bool) []T {
	filtered := []T{}
	for _, v := range vs {
		if !filterFn(v) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func DefaultOrEqual(v, fallback, check string) bool {
	if v == "" {
		return fallback == check
	}
	return v == check
}

// ObjectsToReconcileRequests takes a list of objects and returns a list of
// reconcile Requests.
func ObjectsToReconcileRequests[T metav1.Object](objects []T) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(objects))

	for _, object := range objects {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: object.GetNamespace(),
				Name:      object.GetName(),
			},
		})
	}
	return requests
}

// ParentRefs takes a list of ParentReference objects and returns a list of NamespacedName objects.
func ParentRefs(group, kind, namespace string, refs []gwv1beta1.ParentReference) []types.NamespacedName {
	indexed := make([]types.NamespacedName, 0, len(refs))
	for _, parent := range refs {
		if NilOrEqual(parent.Group, group) && NilOrEqual(parent.Kind, kind) {
			indexed = append(indexed, IndexedNamespacedNameWithDefault(parent.Name, parent.Namespace, namespace))
		}
	}
	return indexed
}

// BothNilOrEqual is used to determine if two pointers to comparable
// object are either nil or both point to the same value.
func BothNilOrEqual[T comparable](one, two *T) bool {
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

// ValueOr checks if a string-like pointer is nil, and if it is,
// returns the given value instead.
func ValueOr[T ~string](v *T, fallback string) string {
	if v == nil {
		return fallback
	}
	return string(*v)
}

// PointerTo is a convenience method for taking a pointer
// of an object without having to declare an intermediate variable.
// It's also useful for making sure we don't accidentally take
// the pointer of a range variable directly.
func PointerTo[T any](v T) *T {
	return &v
}

// ParentsEqual checks for equality between two parent references.
func ParentsEqual(one, two gwv1beta1.ParentReference) bool {
	return BothNilOrEqual(one.Group, two.Group) &&
		BothNilOrEqual(one.Kind, two.Kind) &&
		BothNilOrEqual(one.SectionName, two.SectionName) &&
		BothNilOrEqual(one.Port, two.Port) &&
		one.Name == two.Name
}

func EntryToReference(entry api.ConfigEntry) api.ResourceReference {
	return api.ResourceReference{
		Kind:      entry.GetKind(),
		Name:      entry.GetName(),
		Partition: entry.GetPartition(),
		Namespace: entry.GetNamespace(),
	}
}
