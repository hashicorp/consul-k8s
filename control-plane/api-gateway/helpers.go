package apigateway

import (
	"github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/types"
)

func EmptyOrEqual(v, check string) bool {
	return v == "" || v == check
}

func NilOrEqual[T ~string](v *T, check string) bool {
	return v == nil || string(*v) == check
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
